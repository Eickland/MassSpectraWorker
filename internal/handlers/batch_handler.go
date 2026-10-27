package handlers

import (
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/repository"
	"MassSpectraWorker/internal/service"
	"archive/zip"
	"encoding/json"
	"errors"
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
	// Необязательные параметры этого спектра поверх общих (частичный JSON)
	Params json.RawMessage `json:"params,omitempty"`
}

// CreateJob - POST /api/batch
func (h *BatchHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Валидация параметров
	req.Params.ApplyDefaults()
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
			http.Error(w, "Failed to create job from folder: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else if len(req.Items) > 0 {
		// Если передан список файлов (у каждого могут быть свои параметры)
		list := make([]service.ItemOverride, len(req.Items))
		for i, item := range req.Items {
			list[i] = service.ItemOverride{Name: item.Name, Path: item.Path, Params: item.Params}
		}
		job, err = h.service.CreateJobWithOverrides(list, req.Params)
		if err != nil {
			http.Error(w, "Failed to create job from items: "+err.Error(), http.StatusBadRequest)
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
		writeLookupError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(progress)
}

// writeLookupError - 404 для отсутствующей задачи, 500 для прочих ошибок
func writeLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	log.Printf("batch: %v", err)
	http.Error(w, "Internal error: "+err.Error(), http.StatusInternalServerError)
}

// resultURL - URL картинки результата для галереи
func resultURL(it model.JobItem) string {
	return fileURL(it.JobID, it.ResultPath)
}

func fileURL(jobID uuid.UUID, p *string) string {
	if p == nil || *p == "" {
		return ""
	}
	return "/results/" + path.Join(jobID.String(), filepath.Base(*p))
}

// sseKey - элемент заново попадает в поток событий, когда у него
// появляется новый итог (новый прогон или новая ошибка)
func sseKey(it model.JobItem) string {
	run := ""
	if it.CurrentRunID != nil {
		run = it.CurrentRunID.String()
	}
	finished := ""
	if it.FinishedAt != nil {
		finished = it.FinishedAt.Format(time.RFC3339Nano)
	}
	return it.ID.String() + "|" + it.Status + "|" + run + "|" + finished
}

func parseItemIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
	if err != nil {
		http.Error(w, "Invalid job ID", http.StatusBadRequest)
		return uuid.Nil, uuid.Nil, false
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "item_id"))
	if err != nil {
		http.Error(w, "Invalid item ID", http.StatusBadRequest)
		return uuid.Nil, uuid.Nil, false
	}
	return jobID, itemID, true
}

// writeItemError - коды ответов для операций над спектром
func writeItemError(w http.ResponseWriter, err error) {
	var verr *service.ValidationError
	switch {
	case errors.As(err, &verr):
		http.Error(w, "Invalid params: "+verr.Error(), http.StatusBadRequest)
	case errors.Is(err, repository.ErrNotFound):
		http.Error(w, "Not found", http.StatusNotFound)
	case errors.Is(err, repository.ErrItemBusy):
		http.Error(w, "Спектр уже в очереди или обрабатывается", http.StatusConflict)
	case errors.Is(err, repository.ErrJobBusy):
		http.Error(w, "Задача отменяется — дождитесь завершения", http.StatusConflict)
	case errors.Is(err, repository.ErrRunNotUsable):
		http.Error(w, "Выбрать можно только успешный прогон", http.StatusConflict)
	default:
		log.Printf("batch item: %v", err)
		http.Error(w, "Internal error: "+err.Error(), http.StatusInternalServerError)
	}
}

// GetItem - GET /api/batch/{job_id}/items/{item_id}
// Спектр, его текущие параметры и история прогонов.
func (h *BatchHandler) GetItem(w http.ResponseWriter, r *http.Request) {
	jobID, itemID, ok := parseItemIDs(w, r)
	if !ok {
		return
	}
	d, err := h.service.GetItemDetail(jobID, itemID)
	if err != nil {
		writeItemError(w, err)
		return
	}
	if d.Item.Status == model.ItemDone {
		d.Item.ResultURL = resultURL(d.Item)
	}
	for i := range d.Runs {
		d.Runs[i].ResultURL = fileURL(jobID, d.Runs[i].ResultPath)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d)
}

// ReprocessRequest - тело POST .../reprocess
type ReprocessRequest struct {
	// Полные или частичные параметры поверх текущих параметров спектра
	Params json.RawMessage `json:"params"`
	// true — взять за основу параметры задачи (сбросить индивидуальные)
	ResetToJob bool `json:"reset_to_job"`
}

// ReprocessItem - POST /api/batch/{job_id}/items/{item_id}/reprocess
func (h *BatchHandler) ReprocessItem(w http.ResponseWriter, r *http.Request) {
	jobID, itemID, ok := parseItemIDs(w, r)
	if !ok {
		return
	}
	var req ReprocessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.service.ReprocessItem(jobID, itemID, req.Params, req.ResetToJob); err != nil {
		writeItemError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":  jobID,
		"item_id": itemID,
		"status":  model.ItemPending,
	})
}

// SelectRun - POST /api/batch/{job_id}/items/{item_id}/runs/{run_id}/select
func (h *BatchHandler) SelectRun(w http.ResponseWriter, r *http.Request) {
	jobID, itemID, ok := parseItemIDs(w, r)
	if !ok {
		return
	}
	runID, err := uuid.Parse(chi.URLParam(r, "run_id"))
	if err != nil {
		http.Error(w, "Invalid run ID", http.StatusBadRequest)
		return
	}
	if err := h.service.SelectRun(jobID, itemID, runID); err != nil {
		writeItemError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"item_id": itemID, "current_run_id": runID})
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

	if _, err := h.service.GetJob(jobID); err != nil {
		writeLookupError(w, err)
		return
	}
	items, err := h.service.GetJobItems(jobID)
	if err != nil {
		writeLookupError(w, err)
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
	for i := range items {
		if items[i].Status == model.ItemDone {
			items[i].ResultURL = resultURL(items[i])
		}
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
		writeLookupError(w, err)
		return
	}

	// SSE-соединение живёт дольше WriteTimeout сервера — снимаем дедлайн
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

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
	progress, err := h.service.GetJobProgress(jobID)
	if err != nil {
		writeLookupError(w, err)
		return
	}
	items, _ := h.service.GetJobItems(jobID)

	type initItem struct {
		ID          string `json:"id"`
		SpectraName string `json:"spectra_name"`
		ResultURL   string `json:"result_url"`
	}
	initial := make([]initItem, 0, len(items))
	for _, it := range items {
		if url := resultURL(it); it.Status == model.ItemDone && url != "" {
			initial = append(initial, initItem{
				ID:          it.ID.String(),
				SpectraName: it.SpectraName,
				ResultURL:   url,
			})
		}
	}

	// Поля совпадают с JobProgress, чтобы фронтенд мог передать их в applyProgress
	data, _ := json.Marshal(map[string]interface{}{
		"event": "init",
		"data": struct {
			*model.JobProgress
			Results []initItem `json:"results"`
		}{progress, initial},
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

	seen := make(map[string]struct{}) // итоги элементов, о которых уже сообщили

	// Готовые к моменту подключения элементы уже ушли в init
	if items, err := h.service.GetJobItems(jobID); err == nil {
		for _, it := range items {
			if it.Status == model.ItemDone || it.Status == model.ItemFailed {
				seen[sseKey(it)] = struct{}{}
			}
		}
	}

	send := func(event string, payload interface{}) bool {
		data, _ := json.Marshal(payload)
		select {
		case eventChan <- fmt.Sprintf("event: %s\ndata: %s", event, data):
			return true
		case <-done:
			return false
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
				if it.Status != model.ItemDone && it.Status != model.ItemFailed {
					continue
				}
				key := sseKey(it)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}

				if it.Status == model.ItemFailed {
					errMsg := ""
					if it.Error != nil {
						errMsg = *it.Error
					}
					if !send("item_failed", map[string]interface{}{
						"item_id":      it.ID.String(),
						"spectra_name": it.SpectraName,
						"error":        errMsg,
					}) {
						return
					}
					continue
				}

				if !send("item_done", map[string]interface{}{
					"item_id":      it.ID.String(),
					"spectra_name": it.SpectraName,
					"result_url":   resultURL(it),
				}) {
					return
				}
			}

			progress, err := h.service.GetJobProgress(jobID)
			if err != nil {
				continue
			}
			if progress.Status == model.JobDone || progress.Status == model.JobFailed ||
				progress.Status == model.JobCancelled {
				// Фронтенд ждёт {data: JobProgress}
				send("job_done", map[string]interface{}{"data": progress})
				return
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

	if _, err := h.service.GetJob(jobID); err != nil {
		writeLookupError(w, err)
		return
	}
	files, err := h.service.GetJobResults(jobID)
	if err != nil {
		writeLookupError(w, err)
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

	// itemID -> папка; usedFolders защищает от совпадений вроде
	// Sample.mzML и Sample.csv, которые без расширения дают одно имя
	itemFolders := make(map[uuid.UUID]string)
	usedFolders := make(map[string]bool)

	for _, f := range files {
		// Папка на каждый спектр, чтобы png и спектр были рядом
		folder, ok := itemFolders[f.ItemID]
		if !ok {
			folder = sanitizeZipName(trimSpectrumExt(f.SpectraName))
			if folder == "" {
				folder = f.ItemID.String()
			}
			for base, n := folder, 2; usedFolders[strings.ToLower(folder)]; n++ {
				folder = fmt.Sprintf("%s_%d", base, n)
			}
			usedFolders[strings.ToLower(folder)] = true
			itemFolders[f.ItemID] = folder
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

// trimSpectrumExt убирает расширение файла спектра (Sample.mzML -> Sample).
// Режем только известные форматы, чтобы не обрезать имена вроде "Sample_0.5ppm".
func trimSpectrumExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mzml", ".mzxml", ".csv", ".txt", ".tsv", ".parquet", ".d", ".raw":
		return strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
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
		switch {
		case errors.Is(err, repository.ErrNotFound):
			http.Error(w, "Job not found", http.StatusNotFound)
		case errors.Is(err, service.ErrJobFinished):
			http.Error(w, "Job already finished", http.StatusConflict)
		default:
			http.Error(w, "Failed to cancel job: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":  jobID,
		"status":  "cancelling",
		"message": "Job cancellation requested",
	})
}
