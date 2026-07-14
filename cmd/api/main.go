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

	grpcclient "MassSpectraWorker/internal/client"
	"MassSpectraWorker/internal/handlers"
)

func main() {
	fmt.Println("=== MassSpectraWorker HTTP API Server ===")
	fmt.Println("Starting HTTP server with gRPC client...")

	// 1. Инициализируем gRPC клиент
	grpcClient, err := grpcclient.GetClient()
	if err != nil {
		log.Fatalf("❌ Failed to initialize gRPC client: %v", err)
	}
	defer grpcClient.Close()

	// 2. Создаем обработчики
	plotHandler, err := handlers.NewPlotHandler()
	if err != nil {
		log.Fatalf("❌ Failed to create handler: %v", err)
	}

	// 3. Регистрируем маршруты
	http.HandleFunc("POST /api/plot", plotHandler.GeneratePlot)
	http.HandleFunc("POST /api/plot/stream", plotHandler.StreamPlot)
	http.HandleFunc("GET /api/health", plotHandler.HealthCheck)
	http.HandleFunc("GET /api/info", plotHandler.GetInfo)

	// 4. Настраиваем HTTP сервер
	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 5. Запускаем сервер в горутине
	go func() {
		fmt.Println("✅ HTTP server started on http://localhost:8080")
		fmt.Println("📌 Endpoints:")
		fmt.Println("   POST /api/plot - Generate plot")
		fmt.Println("   POST /api/plot/stream - Generate plot with streaming")
		fmt.Println("   GET  /api/health - Health check")
		fmt.Println("   GET  /api/info - Service info")
		fmt.Println()
		fmt.Println("Press Ctrl+C to stop...")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server error: %v", err)
		}
	}()

	// 6. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n🛑 Shutting down server...")

	// Таймаут для завершения
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("❌ Server shutdown failed: %v", err)
	}

	fmt.Println("✅ Server stopped gracefully")
}
