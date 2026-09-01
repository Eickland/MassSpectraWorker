package handlers

import (
    "archive/zip"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "time"
    "github.com/go-chi/chi/v5"
    "github.com/google/uuid"
    "MassSpectraWorker/internal/model"
    "MassSpectraWorker/internal/service"
)

type BatchHandler struct {
    service *service.BatchService
}

func NewBatchHandler(service *service.BatchService) *BatchHandler {
    return &BatchHandler{service: service}
}

// CreateJobRequest - запрос на создание задачи
type CreateJobRequest struct {
    FolderPath *string              `json:"folder_path,omitempty"`
    Items      []CreateJobItem      `json:"items,omitempty"`
    Params     model.JobParams     `json:"params"`
}

type CreateJobItem struct {
    Name string `json:"name"`
    Path string `json:"path"`
}

// CreateJob - POST /api/batch
func (h *BatchHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
    var req CreateJobRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
        return
    }

    // Валидация параметров
    if err := req.Params.Validate(); err != nil {
        http.Error(w, "Invalid params: "+err.Error(), http.StatusBadRequest)
        return
    }

    var job *model.Job
    var err error

    // Если передана папка
    if req.FolderPath != nil && *req.FolderPath != "" {
        job, err = h.service.CreateJobFromFolder(*req.FolderPath, req.Params)
        if err != nil {
            http.Error(w, "Failed to create job from folder: "+err.Error(), http.StatusInternalServerError)
            return
        }
    } else if len(req.Items) > 0 {
        // Если передан список файлов
        items := make([]model.JobItem, len(req.Items))
        for i, item := range req.Items {
            items[i] = model.JobItem{
                SpectraName: item.Name,
                SpectraPath: item.Path,
            }
        }
        job, err = h.service.CreateJobFromItems(items, req.Params)
        if err != nil {
            http.Error(w, "Failed to create job from items: "+err.Error(), http.StatusInternalServerError)
            return
        }
    } else {
        http.Error(w, "Either folder_path or items must be provided", http.StatusBadRequest)
        return
    }

    // Ответ 202 Accepted
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusAccepted)
    json.NewEncoder(w).Encode(map[string]interface{}{
        "job_id": job.ID,
        "status": "accepted",
        "message": "Job created and queued for processing",
    })
}

// GetJobStatus - GET /api/batch/{job_id}
func (h *BatchHandler) GetJobStatus(w http.ResponseWriter, r *http.Request) {
    jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
    if err != nil {
        http.Error(w, "Invalid job ID", http.StatusBadRequest)
        return
    }

    progress, err := h.service.GetJobProgress(jobID)
    if err != nil {
        http.Error(w, "Job not found", http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(progress)
}

// GetJobItems - GET /api/batch/{job_id}/items
func (h *BatchHandler) GetJobItems(w http.ResponseWriter, r *http.Request) {
    jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
    if err != nil {
        http.Error(w, "Invalid job ID", http.StatusBadRequest)
        return
    }

    // Поддержка пагинации
    page, _ := strconv.Atoi(r.URL.Query().Get("page"))
    if page < 1 {
        page = 1
    }
    limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
    if limit < 1 || limit > 100 {
        limit = 20
    }

    items, err := h.service.GetJobItems(jobID)
    if err != nil {
        http.Error(w, "Job not found", http.StatusNotFound)
        return
    }

    // Пагинация в памяти (для простоты, лучше делать в БД)
    total := len(items)
    start := (page - 1) * limit
    end := start + limit
    if end > total {
        end = total
    }

    if start >= total {
        items = []model.JobItem{}
    } else {
        items = items[start:end]
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "items": items,
        "pagination": map[string]int{
            "page": page,
            "limit": limit,
            "total": total,
            "pages": (total + limit - 1) / limit,
        },
    })
}

// GetJobEvents - GET /api/batch/{job_id}/events (SSE)
func (h *BatchHandler) GetJobEvents(w http.ResponseWriter, r *http.Request) {
    jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
    if err != nil {
        http.Error(w, "Invalid job ID", http.StatusBadRequest)
        return
    }

    // Проверяем, существует ли задача
    _, err = h.service.GetJob(jobID)
    if err != nil {
        http.Error(w, "Job not found", http.StatusNotFound)
        return
    }

    // Настройка SSE
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("Access-Control-Allow-Origin", "*")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    // Канал для событий (в реальном проекте используйте pub/sub)
    eventChan := make(chan string, 100)
    
    // Подписываемся на события (в реальном проекте - через Redis или каналы)
    // Здесь для примера используем простой polling
    go h.observeJobEvents(jobID, eventChan, r.Context().Done())

    // Отправляем начальное состояние
    progress, _ := h.service.GetJobProgress(jobID)
    initialEvent := map[string]interface{}{
        "event": "init",
        "data": progress,
    }
    data, _ := json.Marshal(initialEvent)
    fmt.Fprintf(w, "data: %s\n\n", data)
    flusher.Flush()

    // Слушаем события
    for {
        select {
        case event := <-eventChan:
            fmt.Fprintf(w, "%s\n\n", event)
            flusher.Flush()

        case <-r.Context().Done():
            // Клиент отключился
            return

        case <-time.After(30 * time.Second):
            // Отправляем heartbeat
            fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
            flusher.Flush()
        }
    }
}

// observeJobEvents - наблюдатель за событиями (упрощенная версия)
func (h *BatchHandler) observeJobEvents(jobID uuid.UUID, eventChan chan<- string, done <-chan struct{}) {
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()

    lastDone := 0
    lastFailed := 0
    lastStatus := ""

    for {
        select {
        case <-done:
            return
        case <-ticker.C:
            progress, err := h.service.GetJobProgress(jobID)
            if err != nil {
                continue
            }

            // Проверяем изменения
            if progress.DoneItems > lastDone {
                event := map[string]interface{}{
                    "event": "item_done",
                    "data": map[string]interface{}{
                        "job_id": jobID,
                        "done":   progress.DoneItems,
                        "total":  progress.TotalItems,
                    },
                }
                data, _ := json.Marshal(event)
                eventChan <- fmt.Sprintf("event: item_done\ndata: %s", data)
                lastDone = progress.DoneItems
            }

            if progress.FailedItems > lastFailed {
                event := map[string]interface{}{
                    "event": "item_failed",
                    "data": map[string]interface{}{
                        "job_id": jobID,
                        "failed": progress.FailedItems,
                        "total":  progress.TotalItems,
                    },
                }
                data, _ := json.Marshal(event)
                eventChan <- fmt.Sprintf("event: item_failed\ndata: %s", data)
                lastFailed = progress.FailedItems
            }

            if progress.Status != lastStatus && (progress.Status == "done" || progress.Status == "failed" || progress.Status == "cancelled") {
                event := map[string]interface{}{
                    "event": "job_done",
                    "data": map[string]interface{}{
                        "job_id": jobID,
                        "status": progress.Status,
                        "total":  progress.TotalItems,
                        "done":   progress.DoneItems,
                        "failed": progress.FailedItems,
                    },
                }
                data, _ := json.Marshal(event)
                eventChan <- fmt.Sprintf("event: job_done\ndata: %s", data)
                lastStatus = progress.Status
                return
            }

            if progress.Status != lastStatus {
                lastStatus = progress.Status
            }
        }
    }
}

// GetJobResultsZip - GET /api/batch/{job_id}/results.zip
func (h *BatchHandler) GetJobResultsZip(w http.ResponseWriter, r *http.Request) {
    jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
    if err != nil {
        http.Error(w, "Invalid job ID", http.StatusBadRequest)
        return
    }

    // Получаем пути к результатам
    resultPaths, err := h.service.GetJobResults(jobID)
    if err != nil {
        http.Error(w, "Job not found", http.StatusNotFound)
        return
    }

    if len(resultPaths) == 0 {
        http.Error(w, "No results found for this job", http.StatusNotFound)
        return
    }

    // Создаем ZIP на лету
    w.Header().Set("Content-Type", "application/zip")
    w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=job_%s_results.zip", jobID))

    zipWriter := zip.NewWriter(w)
    defer zipWriter.Close()

    for _, filePath := range resultPaths {
        // Проверяем, существует ли файл
        _, err := os.Stat(filePath)
        if err != nil {
            continue // Пропускаем отсутствующие файлы
        }

        // Открываем файл
        file, err := os.Open(filePath)
        if err != nil {
            continue
        }
        defer file.Close()

        // Создаем запись в ZIP
        fileName := filepath.Base(filePath)
        writer, err := zipWriter.Create(fileName)
        if err != nil {
            continue
        }

        // Копируем содержимое
        if _, err := io.Copy(writer, file); err != nil {
            continue
        }
    }

    // Закрываем ZIP
    if err := zipWriter.Close(); err != nil {
        http.Error(w, "Failed to create ZIP", http.StatusInternalServerError)
        return
    }
}

// CancelJob - POST /api/batch/{job_id}/cancel
func (h *BatchHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
    jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
    if err != nil {
        http.Error(w, "Invalid job ID", http.StatusBadRequest)
        return
    }

    if err := h.service.CancelJob(jobID); err != nil {
        http.Error(w, "Failed to cancel job: "+err.Error(), http.StatusBadRequest)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "job_id": jobID,
        "status": "cancelling",
        "message": "Job cancellation requested",
    })
}