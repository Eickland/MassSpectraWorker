package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"

	pb "Mass_spectra_worker/src/protobuf/plot"
)

type SpectraCache struct {
	mu    sync.RWMutex
	cache map[string]*CachedResult
}

type CachedResult struct {
	Data           [][]float64 `json:"data"`
	NumPeaks       int         `json:"num_peaks"`
	NumAssignments int         `json:"num_assignments"`
	CreatedAt      time.Time   `json:"created_at"`
	VKPlot         string      `json:"vk_plot"` // base64 или plotly JSON
	SpectrumPlot   string      `json:"spectrum_plot"`
}

var (
	cache      = &SpectraCache{cache: make(map[string]*CachedResult)}
	grpcClient pb.MassSpectraServiceClient
)

func initGRPC() {
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	grpcClient = pb.NewMassSpectraServiceClient(conn)
}

func generateCacheKey(params map[string]interface{}) string {
	// Генерируем ключ на основе всех параметров
	data, _ := json.Marshal(params)
	return string(data)
}

// Быстрый рендер графиков в Go (используем go-plotly или gonum)
func renderPlots(data [][]float64) (string, string) {
	// Здесь вы можете использовать:
	// 1. Gonum/plot для быстрой генерации PNG
	// 2. Plotly JSON для интерактивных графиков в браузере
	// 3. Или просто возвращать данные для frontend

	// Пример: готовим данные для Plotly
	vkPlot := plotlyScatter(data)
	spectrumPlot := plotlySpectrum(data)

	return vkPlot, spectrumPlot
}

func processHandler(c *gin.Context) {
	var req struct {
		FilePath       string  `json:"file_path"`
		LowPercentile  float64 `json:"low_percentile"`
		HighPercentile float64 `json:"high_percentile"`
		RelError       float64 `json:"rel_error"`
		CMin           int32   `json:"c_min"`
		CMax           int32   `json:"c_max"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Создаем ключ кеша
	cacheKey := generateCacheKey(map[string]interface{}{
		"file":  req.FilePath,
		"low":   req.LowPercentile,
		"high":  req.HighPercentile,
		"error": req.RelError,
	})

	// Проверяем кеш
	cache.mu.RLock()
	if cached, ok := cache.cache[cacheKey]; ok {
		cache.mu.RUnlock()
		c.JSON(200, gin.H{
			"cached": true,
			"result": cached,
		})
		return
	}
	cache.mu.RUnlock()

	// Если нет в кеше - отправляем в Python
	grpcReq := &pb.ProcessRequest{
		FilePath:       req.FilePath,
		LowPercentile:  req.LowPercentile,
		HighPercentile: req.HighPercentile,
		RelError:       req.RelError,
		CMin:           req.CMin,
		CMax:           req.CMax,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := grpcClient.ProcessSpectra(ctx, grpcReq)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Парсим результат
	var data map[string]interface{}
	json.Unmarshal([]byte(resp.ResultJson), &data)

	// Готовим результат
	result := &CachedResult{
		NumPeaks:       int(resp.NumPeaks),
		NumAssignments: int(resp.NumAssignments),
		CreatedAt:      time.Now(),
	}

	// Преобразуем данные в формат для графиков
	// Здесь можно сделать быструю обработку на Go
	result.VKPlot, result.SpectrumPlot = renderPlots(data["data"].([]interface{}))

	// Сохраняем в кеш
	cache.mu.Lock()
	cache.cache[cacheKey] = result
	cache.mu.Unlock()

	c.JSON(200, gin.H{
		"cached": false,
		"result": result,
	})
}

// WebSocket для реального времени
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer conn.Close()

	for {
		// Получаем параметры от клиента
		var params map[string]interface{}
		err := conn.ReadJSON(&params)
		if err != nil {
			break
		}

		// Отправляем прогресс
		conn.WriteJSON(gin.H{"type": "progress", "value": 10})

		// Запускаем обработку
		// ...

		conn.WriteJSON(gin.H{"type": "result", "data": result})
	}
}

func main() {
	initGRPC()

	r := gin.Default()

	// Статика
	r.Static("/static", "./static")
	r.LoadHTMLFiles("templates/index.html")

	// API
	r.POST("/api/process", processHandler)
	r.GET("/ws", gin.WrapH(http.HandlerFunc(wsHandler)))

	r.Run(":8080")
}
