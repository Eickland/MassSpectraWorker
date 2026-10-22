// cmd/api/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	grpcclient "MassSpectraWorker/internal/client"
	"MassSpectraWorker/internal/config"
	"MassSpectraWorker/internal/handlers"
	"MassSpectraWorker/internal/repository"
	"MassSpectraWorker/internal/service"
	"MassSpectraWorker/internal/storage"
	"MassSpectraWorker/internal/worker"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}
	cfg := config.LoadConfig()

	// 1. База SQLite (файл создаётся и мигрируется автоматически)
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("❌ Failed to open database %s: %v", cfg.DBPath, err)
	}
	defer db.Close()
	log.Printf("✅ SQLite database: %s", cfg.DBPath)

	fmt.Println("=== MassSpectraWorker HTTP API Server ===")

	// 2. gRPC клиент к Python-сервису
	grpcClient, err := grpcclient.GetClient()
	if err != nil {
		log.Fatalf("❌ Failed to initialize gRPC client: %v", err)
	}
	defer grpcClient.Close()

	// 3. Обработчики
	plotHandler, err := handlers.NewMassListHandler(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to create handler: %v", err)
	}
	webHandler := handlers.NewWebHandler()

	// 4. Репозиторий, воркеры, сервис пакетной обработки
	repo := repository.NewJobRepository(db)

	pool := worker.NewWorkerPool(repo, grpcClient, cfg.Workers, cfg)
	pool.Start()

	batchService := service.NewBatchService(repo, pool)
	batchHandler := handlers.NewBatchHandler(batchService)

	// Задачи, прерванные прошлой остановкой сервера, продолжаем
	if ids, err := repo.RecoverInterrupted(); err != nil {
		log.Printf("⚠️ Failed to recover interrupted jobs: %v", err)
	} else {
		for _, id := range ids {
			log.Printf("🔁 Resuming job %s", id)
			pool.Submit(id)
		}
	}

	// 5. Роутер
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Глобальный middleware.Timeout не используем: он обрывал SSE через 60 с
	// и укорачивал GRPC_TIMEOUT для /api/plot. Таймауты задаются в хендлерах.

	fileServer := http.FileServer(http.Dir("./static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mass_list", http.StatusFound)
	})
	browseHandler := handlers.NewBrowseHandler()
	r.Get("/api/browse", browseHandler.Browse)

	// Web страницы
	r.Get("/index", webHandler.IndexPage)
	r.Get("/batch_mass_list", webHandler.BatchMassListPage)
	r.Get("/mass_list", webHandler.MassListPage)
	r.Get("/health", webHandler.HealthPage)

	// API: одиночный спектр
	r.Post("/api/plot", plotHandler.ProcessMassList)
	r.Get("/api/health", plotHandler.HealthCheck)
	r.Get("/api/info", plotHandler.GetInfo)
	r.Get("/results/{jobID}/{name}", plotHandler.HandleResultImage)

	// API: пакетная обработка
	r.Route("/api/batch", func(r chi.Router) {
		r.Post("/", batchHandler.CreateJob)
		r.Get("/{job_id}", batchHandler.GetJobStatus)
		r.Get("/{job_id}/items", batchHandler.GetJobItems)
		r.Get("/{job_id}/events", batchHandler.GetJobEvents)
		r.Get("/{job_id}/results.zip", batchHandler.GetJobResultsZip)
		r.Post("/{job_id}/cancel", batchHandler.CancelJob)

		// Параметры и пересчёт отдельного спектра
		r.Get("/{job_id}/items/{item_id}", batchHandler.GetItem)
		r.Post("/{job_id}/items/{item_id}/reprocess", batchHandler.ReprocessItem)
		r.Post("/{job_id}/items/{item_id}/runs/{run_id}/select", batchHandler.SelectRun)
	})

	// Базовый контекст запросов отменяется при остановке — открытые
	// SSE-соединения закрываются сразу, а не держат Shutdown до таймаута
	baseCtx, stopBase := context.WithCancel(context.Background())
	defer stopBase()

	server := &http.Server{
		Addr:        cfg.HTTPAddr,
		BaseContext: func(net.Listener) context.Context { return baseCtx },
		Handler:     r,
		ReadTimeout: 15 * time.Second,
		// /api/plot ждёт Python до GRPC_TIMEOUT; SSE снимает дедлайн сам
		WriteTimeout: cfg.GRPCTimeout + 30*time.Second,
		IdleTimeout:  120 * time.Second,
	}
	server.RegisterOnShutdown(stopBase)

	go func() {
		addr := cfg.HTTPAddr
		if strings.HasPrefix(addr, ":") {
			addr = "localhost" + addr
		}
		fmt.Printf("✅ HTTP server started on http://%s\n", addr)
		fmt.Println("Press Ctrl+C to stop...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n🛑 Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("❌ Server shutdown failed: %v", err)
	}

	// Останавливаем воркеры до закрытия БД: незавершённые элементы
	// вернутся в очередь и продолжатся при следующем запуске
	pool.Stop()
	fmt.Println("✅ Server stopped gracefully")
}
