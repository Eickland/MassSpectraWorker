// internal/handlers/plot.go
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	grpcclient "MassSpectraWorker/internal/client"
	pb "MassSpectraWorker/src/protobuf"
)

// PlotHandler - обработчик для запросов графиков
type PlotHandler struct {
	grpcClient *grpcclient.GRPCClient
}

// NewPlotHandler - создает новый обработчик
func NewPlotHandler() (*PlotHandler, error) {
	client, err := grpcclient.GetClient()
	if err != nil {
		return nil, err
	}
	return &PlotHandler{
		grpcClient: client,
	}, nil
}

// PlotRequest - структура HTTP запроса
type PlotRequest struct {
	XValues  []float64 `json:"x_values"`
	YValues  []float64 `json:"y_values"`
	Title    string    `json:"title"`
	XLabel   string    `json:"x_label"`
	YLabel   string    `json:"y_label"`
	PlotType string    `json:"plot_type"` // line, scatter, bar, hist
	Color    string    `json:"color"`
	Grid     bool      `json:"grid"`
	Width    int32     `json:"width"`
	Height   int32     `json:"height"`
	Dpi      int32     `json:"dpi"`
	Format   string    `json:"format"` // png, svg, pdf, jpeg
}

// PlotResponse - структура HTTP ответа
type PlotResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	Format      string `json:"format"`
	MimeType    string `json:"mime_type"`
	GeneratedAt int64  `json:"generated_at"`
	Error       string `json:"error,omitempty"`
}

// GeneratePlot - HTTP эндпоинт для генерации графика
func (h *PlotHandler) GeneratePlot(w http.ResponseWriter, r *http.Request) {
	// 1. Проверяем метод
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	// 2. Читаем тело запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 3. Парсим JSON
	var req PlotRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 4. Валидация
	if len(req.XValues) == 0 || len(req.YValues) == 0 {
		http.Error(w, "x_values and y_values are required", http.StatusBadRequest)
		return
	}

	if len(req.XValues) != len(req.YValues) {
		http.Error(w, "x_values and y_values must have same length", http.StatusBadRequest)
		return
	}

	// 5. Преобразуем в gRPC запрос
	grpcReq := &pb.PlotRequest{
		XValues:  req.XValues,
		YValues:  req.YValues,
		Title:    req.Title,
		XLabel:   req.XLabel,
		YLabel:   req.YLabel,
		PlotType: req.PlotType,
		Color:    req.Color,
		Grid:     req.Grid,
		Width:    req.Width,
		Height:   req.Height,
		Dpi:      req.Dpi,
		Format:   req.Format,
	}

	// Устанавливаем значения по умолчанию
	if grpcReq.PlotType == "" {
		grpcReq.PlotType = "line"
	}
	if grpcReq.Format == "" {
		grpcReq.Format = "png"
	}
	if grpcReq.Color == "" {
		grpcReq.Color = "blue"
	}
	if grpcReq.Width == 0 {
		grpcReq.Width = 10
	}
	if grpcReq.Height == 0 {
		grpcReq.Height = 6
	}
	if grpcReq.Dpi == 0 {
		grpcReq.Dpi = 100
	}

	// 6. Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 7. Вызываем gRPC сервер
	log.Printf("📊 Generating plot: %s (type: %s)", req.Title, req.PlotType)
	start := time.Now()

	resp, err := h.grpcClient.GeneratePlot(ctx, grpcReq)
	if err != nil {
		log.Printf("❌ Error generating plot: %v", err)
		http.Error(w, "Failed to generate plot: "+err.Error(), http.StatusInternalServerError)
		return
	}

	elapsed := time.Since(start)
	log.Printf("✅ Plot generated in %v, size: %.2f KB", elapsed, float64(resp.SizeBytes)/1024)

	// 8. Отправляем ответ в зависимости от формата
	format := strings.ToLower(grpcReq.Format)

	if format == "json" || r.Header.Get("Accept") == "application/json" {
		// Возвращаем JSON с base64
		jsonResp := PlotResponse{
			Success:     true,
			Message:     "Plot generated successfully",
			SizeBytes:   resp.SizeBytes,
			Format:      resp.Format,
			MimeType:    resp.MimeType,
			GeneratedAt: resp.GeneratedAt,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(jsonResp)
	} else {
		// Возвращаем изображение
		w.Header().Set("Content-Type", resp.MimeType)
		w.Header().Set("Content-Length", strconv.FormatInt(resp.SizeBytes, 10))
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=plot_%d.%s",
			time.Now().Unix(), resp.Format))

		w.WriteHeader(http.StatusOK)
		w.Write(resp.ImageData)
	}
}

// StreamPlot - HTTP эндпоинт для стриминга
func (h *PlotHandler) StreamPlot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req PlotRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(req.XValues) == 0 || len(req.YValues) == 0 {
		http.Error(w, "x_values and y_values are required", http.StatusBadRequest)
		return
	}

	// Преобразуем в gRPC запрос
	grpcReq := &pb.PlotRequest{
		XValues:  req.XValues,
		YValues:  req.YValues,
		Title:    req.Title,
		XLabel:   req.XLabel,
		YLabel:   req.YLabel,
		PlotType: req.PlotType,
		Color:    req.Color,
		Grid:     req.Grid,
		Width:    req.Width,
		Height:   req.Height,
		Dpi:      req.Dpi,
		Format:   req.Format,
	}

	// Устанавливаем значения по умолчанию
	if grpcReq.PlotType == "" {
		grpcReq.PlotType = "line"
	}
	if grpcReq.Format == "" {
		grpcReq.Format = "png"
	}
	if grpcReq.Color == "" {
		grpcReq.Color = "blue"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	log.Printf("📊 Streaming plot: %s", req.Title)
	start := time.Now()

	// Вызываем gRPC стриминг
	stream, err := h.grpcClient.StreamPlot(ctx, grpcReq)
	if err != nil {
		log.Printf("❌ Error starting stream: %v", err)
		http.Error(w, "Failed to start stream: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Собираем все чанки
	var chunks [][]byte
	totalSize := int64(0)
	chunkCount := 0

	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.Printf("❌ Stream error: %v", err)
			http.Error(w, "Stream error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		chunks = append(chunks, chunk.ChunkData)
		totalSize += int64(len(chunk.ChunkData))
		chunkCount++
	}

	// Собираем изображение
	imageData := make([]byte, 0, totalSize)
	for _, chunk := range chunks {
		imageData = append(imageData, chunk...)
	}

	elapsed := time.Since(start)
	log.Printf("✅ Stream completed in %v, chunks: %d, size: %.2f KB",
		elapsed, chunkCount, float64(totalSize)/1024)

	// Отправляем ответ
	format := strings.ToLower(grpcReq.Format)
	mimeTypes := map[string]string{
		"png":  "image/png",
		"svg":  "image/svg+xml",
		"pdf":  "application/pdf",
		"jpeg": "image/jpeg",
		"jpg":  "image/jpeg",
	}

	w.Header().Set("Content-Type", mimeTypes[format])
	w.Header().Set("Content-Length", strconv.FormatInt(totalSize, 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=streamed_plot_%d.%s",
		time.Now().Unix(), format))

	w.WriteHeader(http.StatusOK)
	w.Write(imageData)
}

// HealthCheck - проверка здоровья сервиса
func (h *PlotHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	httpStatus := http.StatusOK
	grpcHealthy := h.grpcClient.IsHealthy()

	if !grpcHealthy {
		status = "unhealthy - gRPC connection failed"
		httpStatus = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": status,
		"grpc":   grpcHealthy,
		"time":   time.Now().Unix(),
	})
}

// GetInfo - информация о сервисе
func (h *PlotHandler) GetInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"service": "MassSpectraWorker HTTP API",
		"version": "1.0.0",
		"endpoints": []string{
			"POST /api/plot - Generate plot",
			"POST /api/plot/stream - Generate plot with streaming",
			"GET /api/health - Health check",
			"GET /api/info - Service info",
		},
		"grpc_server": "localhost:50051",
		"status":      "running",
	})
}
