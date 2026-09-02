// cmd/http-server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	grpcclient "MassSpectraWorker/internal/client"
	"MassSpectraWorker/internal/handlers"
	"MassSpectraWorker/internal/repository"
	"MassSpectraWorker/internal/service"
	"MassSpectraWorker/internal/worker"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_SSLMODE"),
	)

	db, err := sqlx.Connect("postgres", dbURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.Close()

	log.Println("Connected to massspectraworkerdb!")
	fmt.Println("=== MassSpectraWorker HTTP API Server ===")
	fmt.Println("Starting HTTP server with gRPC client...")

	// 1. Инициализируем gRPC клиент
	grpcClient, err := grpcclient.GetClient()
	if err != nil {
		log.Fatalf("❌ Failed to initialize gRPC client: %v", err)
	}
	defer grpcClient.Close()

	// 2. Создаем обработчики
	plotHandler, err := handlers.NewMassListHandler()
	if err != nil {
		log.Fatalf("❌ Failed to create handler: %v", err)
	}

	webHandler := handlers.NewWebHandler()

	// 3. Инициализация репозитория, сервиса, хендлера для batch
	repo := repository.NewJobRepository(db)

	pool := worker.NewWorkerPool(repo, grpcClient, 5) // 5 воркеров
	pool.Start()
	defer pool.Stop()

	service := service.NewBatchService(repo, pool)
	batchHandler := handlers.NewBatchHandler(service)

	// 4. Запуск воркер-пула

	// ============================================
	// 5. ЕДИНЫЙ РОУТЕР НА БАЗЕ CHI
	// ============================================
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// === Статика ===
	// Обслуживаем статические файлы из папки ./static
	fileServer := http.FileServer(http.Dir("./static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// === Корневой маршрут ===
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mass_list", http.StatusFound)
	})
	browseHandler := handlers.NewBrowseHandler()
	r.Get("/api/browse", browseHandler.Browse)
	// === Web маршруты (HTML) ===
	r.Get("/index", webHandler.IndexPage)
	r.Get("/batch_mass_list", webHandler.BatchMassListPage)
	r.Get("/mass_list", webHandler.MassListPage)
	r.Get("/health", webHandler.HealthPage)

	// === API маршруты для plot ===
	r.Post("/api/plot", plotHandler.ProcessMassList)
	r.Get("/api/health", plotHandler.HealthCheck)
	r.Get("/api/info", plotHandler.GetInfo)

	// === API маршруты для batch ===
	r.Route("/api/batch", func(r chi.Router) {
		r.Post("/", batchHandler.CreateJob)
		r.Get("/{job_id}", batchHandler.GetJobStatus)
		r.Get("/{job_id}/items", batchHandler.GetJobItems)
		r.Get("/{job_id}/events", batchHandler.GetJobEvents)
		r.Get("/{job_id}/results.zip", batchHandler.GetJobResultsZip)
		r.Post("/{job_id}/cancel", batchHandler.CancelJob)
	})

	// 6. Настраиваем HTTP сервер
	server := &http.Server{
		Addr:         ":8080",
		Handler:      r, // ВАЖНО: используем chi роутер
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 7. Запускаем сервер в горутине
	go func() {
		fmt.Println("✅ HTTP server started on http://localhost:8080")
		fmt.Println("📌 Endpoints:")
		fmt.Println("   📄 Web страницы:")
		fmt.Println("      GET /                 - Redirect to /index")
		fmt.Println("      GET /index            - Главная страница")
		fmt.Println("      GET /batch_mass_list  - Batch mass list")
		fmt.Println("      GET /mass_list        - Mass list")
		fmt.Println("      GET /health           - Health page")
		fmt.Println()
		fmt.Println("   🔧 API endpoints:")
		fmt.Println("      POST /api/plot        - Generate plot")
		fmt.Println("      GET  /api/health      - Health check")
		fmt.Println("      GET  /api/info        - Service info")
		fmt.Println("      POST /api/batch/      - Create batch job")
		fmt.Println("      GET  /api/batch/{id}  - Get job status")
		fmt.Println("      GET  /api/batch/{id}/items - Get job items")
		fmt.Println("      GET  /api/batch/{id}/events - Get job events")
		fmt.Println("      GET  /api/batch/{id}/results.zip - Download results")
		fmt.Println("      POST /api/batch/{id}/cancel - Cancel job")
		fmt.Println()
		fmt.Println("Press Ctrl+C to stop...")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server error: %v", err)
		}
	}()

	// 8. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n🛑 Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("❌ Server shutdown failed: %v", err)
	}

	fmt.Println("✅ Server stopped gracefully")
}
