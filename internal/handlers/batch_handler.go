package handlers

import (
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/service"
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type BatchHandler struct {
	service *service.BatchService
}

func NewBatchHandler(service *service.BatchService) *BatchHandler {
	return &BatchHandler{service: service}
}

// CreateJobRequest - запрос на создание задачи
type CreateJobRequest struct {
	FolderPath *string         `json:"folder_path,omitempty"`
	Items      []CreateJobItem `json:"items,omitempty"`
	Params     model.JobParams `json:"params"`
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
		"job_id":  job.ID,
		"status":  "accepted",
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
			"page":  page,
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

	if _, err := h.service.GetJob(jobID); err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // для nginx

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	eventChan := make(chan string, 100)
	go h.observeJobEvents(jobID, eventChan, r.Context().Done())

	// ---- init ----
	progress, _ := h.service.GetJobProgress(jobID)
	items, _ := h.service.GetJobItems(jobID)

	type initItem struct {
		ID          string `json:"id"`
		SpectraName string `json:"spectra_name"`
		ResultURL   string `json:"result_url"`
	}
	initial := make([]initItem, 0, len(items))
	for _, it := range items {

		if it.Status == "done" && it.ResultPath != nil {
			initial = append(initial, initItem{
				ID:          it.ID.String(),
				SpectraName: it.SpectraName,
				ResultURL:   "/results/" + path.Join(it.JobID.String(), path.Base(*it.ResultPath)),
			})
		}
	}

	initPayload := map[string]interface{}{
		"status":  progress.Status,
		"total":   progress.TotalItems,
		"done":    progress.DoneItems,
		"failed":  progress.FailedItems,
		"results": initial,
	}
	data, _ := json.Marshal(map[string]interface{}{
		"event": "init",
		"data":  initPayload,
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	// ---- основной цикл ----
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case event := <-eventChan:
			fmt.Fprintf(w, "%s\n\n", event)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(w, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// observeJobEvents - наблюдатель за событиями (упрощенная версия)
func (h *BatchHandler) observeJobEvents(jobID uuid.UUID, eventChan chan<- string, done <-chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	seen := make(map[uuid.UUID]struct{}) // уже отправленные items
	lastStatus := ""

	// Отправляем всё, что уже было done к моменту подключения
	if items, err := h.service.GetJobItems(jobID); err == nil {
		for _, it := range items {
			if it.Status != "done" || it.ResultPath == nil {
				continue
			}
			if it.Status == "done" {
				seen[it.ID] = struct{}{}
			}
		}
	}

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			items, err := h.service.GetJobItems(jobID)
			if err != nil {
				continue
			}

			for _, it := range items {
				if it.Status != "done" {
					continue
				}
				if _, ok := seen[it.ID]; ok {
					continue
				}
				seen[it.ID] = struct{}{}

				url := "/results/" + path.Join(jobID.String(), path.Base(*it.ResultPath))
				payload := map[string]interface{}{
					"item_id":      it.ID.String(),
					"spectra_name": it.SpectraName,
					"result_url":   url,
				}
				data, _ := json.Marshal(payload)
				select {
				case eventChan <- fmt.Sprintf("event: item_done\ndata: %s", data):
				case <-done:
					return
				}
			}

			// Финальный статус — как было, но добавим список результатов
			progress, err := h.service.GetJobProgress(jobID)
			if err != nil {
				continue
			}

			// Проверяем изменения
			if progress.Status != lastStatus {
				lastStatus = progress.Status
				if progress.Status == "done" || progress.Status == "failed" || progress.Status == "cancelled" {
					payload := map[string]interface{}{
						"job_id": jobID.String(),
						"status": progress.Status,
						"total":  progress.TotalItems,
						"done":   progress.DoneItems,
						"failed": progress.FailedItems,
					}
					data, _ := json.Marshal(payload)
					select {
					case eventChan <- fmt.Sprintf("event: job_done\ndata: %s", data):
					case <-done:
						return
					}
					return
				}
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

	files, err := h.service.GetJobResults(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	if len(files) == 0 {
		http.Error(w, "No results found for this job", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=job_%s_results.zip", jobID))

	zw := zip.NewWriter(w)
	// ВАЖНО: не defer-им Close, закроем явно в конце.
	// И defer-им на случай паники в середине — но тогда w уже частично записан,
	// это неизбежно для streaming-подхода.

	for _, f := range files {
		// Папка на каждый спектр, чтобы png и спектр были рядом
		folder := sanitizeZipName(f.SpectraName)
		if folder == "" {
			folder = f.ItemID.String()
		}

		if err := addFileToZip(zw, f.PlotPath, folder); err != nil {
			log.Printf("zip: job=%s item=%s plot: %v", jobID, f.ItemID, err)
		}
		if err := addFileToZip(zw, f.SpectrumPath, folder); err != nil {
			log.Printf("zip: job=%s item=%s spectrum: %v", jobID, f.ItemID, err)
		}
	}

	if err := zw.Close(); err != nil {
		// Заголовки уже отправлены — только логируем
		log.Printf("zip: job=%s final close: %v", jobID, err)
		return
	}
}

func addFileToZip(zw *zip.Writer, path, folder string) error {
	if path == "" {
		return nil
	}
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	name := filepath.Base(path)
	if folder != "" {
		name = folder + "/" + name
	}

	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = name
	hdr.Method = zip.Deflate // иначе png/parquet лягут "как есть" без сжатия

	dst, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	return nil
}

func sanitizeZipName(s string) string {
	// Убираем слэши, двоеточия, ведущие точки и прочее,
	// что может сломать структуру ZIP или выйти за пределы (zip-slip)
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.Trim(s, ".")
	return s
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
		"job_id":  jobID,
		"status":  "cancelling",
		"message": "Job cancellation requested",
	})
}
