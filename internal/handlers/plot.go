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

// MassListHandler - обработчик для запросов графиков
type MassListHandler struct {
	grpcClient *grpcclient.GRPCClient
}

// NewPlotHandler - создает новый обработчик
func NewMassListHandler() (*MassListHandler, error) {
	client, err := grpcclient.GetClient()
	if err != nil {
		return nil, err
	}
	return &MassListHandler{
		grpcClient: client,
	}, nil
}

type BruttoRange struct {
	Min int32 `json:"min"`
	Max int32 `json:"max"`
}

// MassListRequest - структура HTTP запроса
type MassListRequest struct {
	SpectraName    string                     `json:"spectra_name"`
	LowPercentile  float32                    `json:"low_percentile"`
	HighPercentile float32                    `json:"high_percentile"`
	RelError       float32                    `json:"rel_error"`
	ChargeMax      int32                      `json:"charge_max"`
	BruttoDict     map[string]*pb.BruttoRange `json:"brutto_dict"`
	Protocole      string                     `json:"protocole"`
	SpectraPath    string                     `json:"spectra_path"`
	Width          int32                      `json:"width"`
	Height         int32                      `json:"height"`
	Dpi            int32                      `json:"dpi"`
	Format         string                     `json:"format"`
	Options        map[string]string          `json:"options"`
}

// MassListResponse - структура HTTP ответа
type MassListResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	Format      string `json:"format"`
	MimeType    string `json:"mime_type"`
	GeneratedAt int64  `json:"generated_at"`
	Error       string `json:"error,omitempty"`
}

// ProcessMassList - HTTP эндпоинт для генерации графика
func (h *MassListHandler) ProcessMassList(w http.ResponseWriter, r *http.Request) {
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
	var req MassListRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("❌ JSON parsing error: %v", err)
		log.Printf("Raw body: %s", string(body))
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 4. Валидация
	if req.SpectraName == "" {
		http.Error(w, "spectra_name is required", http.StatusBadRequest)
		return
	}

	// 5. Устанавливаем значения по умолчанию
	if req.ChargeMax == 0 {
		req.ChargeMax = 1
	}
	if req.RelError == 0 {
		req.RelError = 0.5
	}
	if req.Format == "" {
		req.Format = "png"
	}
	if req.Protocole == "" {
		req.Protocole = "non_tmds"
	}
	if req.Width == 0 {
		req.Width = 10
	}
	if req.Height == 0 {
		req.Height = 6
	}
	if req.Dpi == 0 {
		req.Dpi = 100
	}

	// 6. Создаем gRPC запрос
	grpcReq := &pb.MassListRequest{
		SpectraName:    req.SpectraName,
		SpectraPath:    req.SpectraPath,
		LowPercentile:  req.LowPercentile,
		HighPercentile: req.HighPercentile,
		RelError:       req.RelError,
		ChargeMax:      req.ChargeMax,
		BruttoDict:     req.BruttoDict, // ✅ Теперь типы совпадают!
		Protocole:      req.Protocole,
		Width:          req.Width,
		Height:         req.Height,
		Dpi:            req.Dpi,
		Format:         req.Format,
		Options:        req.Options,
	}

	// 7. Создаем контекст с таймаутом
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// 8. Вызываем gRPC сервер
	log.Printf("📊 Generating mass list: %s", req.SpectraName)
	start := time.Now()

	resp, err := h.grpcClient.ProcessMassList(ctx, grpcReq)
	if err != nil {
		log.Printf("❌ Error generating mass list: %v", err)
		http.Error(w, "Failed to generate mass list: "+err.Error(), http.StatusInternalServerError)
		return
	}

	elapsed := time.Since(start)
	log.Printf("✅ Mass list generated in %v, size: %.2f KB", elapsed, float64(resp.SizeBytes)/1024)

	// 9. Отправляем ответ
	format := strings.ToLower(req.Format)

	if format == "json" || r.Header.Get("Accept") == "application/json" {
		// Возвращаем JSON с base64
		jsonResp := MassListResponse{
			Success:     true,
			Message:     "Mass list generated successfully",
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
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=mass_list_%d.%s",
			time.Now().Unix(), resp.Format))

		w.WriteHeader(http.StatusOK)
		w.Write(resp.ImageData)
	}
}

// HealthCheck - проверка здоровья сервиса
func (h *MassListHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
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
func (h *MassListHandler) GetInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"service": "MassSpectraWorker HTTP API",
		"version": "1.0.0",
		"endpoints": []string{
			"POST /api/plot - Generate plot",
			"GET /api/health - Health check",
			"GET /api/info - Service info",
		},
		"grpc_server": "localhost:50051",
		"status":      "running",
	})
}
