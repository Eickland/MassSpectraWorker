package service

import (
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/repository"
	"MassSpectraWorker/internal/worker"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrJobFinished - попытка отменить уже завершённую задачу
var ErrJobFinished = errors.New("job already finished")

type BatchService struct {
	repo *repository.JobRepository
	pool *worker.WorkerPool
}

type JobResultFile struct {
	ItemID       uuid.UUID
	SpectraName  string
	PlotPath     string
	SpectrumPath string
}

func NewBatchService(repo *repository.JobRepository, pool *worker.WorkerPool) *BatchService {
	return &BatchService{
		repo: repo,
		pool: pool,
	}
}

// CreateJobFromFolder - создает задачу из папки
func (s *BatchService) CreateJobFromFolder(folderPath string, params model.JobParams) (*model.Job, error) {
	files, err := scanFolder(folderPath)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no supported files (.mzML, .mzXML, .d, .raw) found in folder")
	}

	items := make([]model.JobItem, len(files))
	for i, file := range files {
		items[i] = model.JobItem{
			SpectraName: filepath.Base(file),
			SpectraPath: file,
		}
	}
	return s.createJob(&folderPath, items, params)
}

// CreateJobFromItems - создает задачу из списка файлов
func (s *BatchService) CreateJobFromItems(items []model.JobItem, params model.JobParams) (*model.Job, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no items provided")
	}
	for i := range items {
		if items[i].SpectraName == "" {
			items[i].SpectraName = filepath.Base(items[i].SpectraPath)
		}
	}
	return s.createJob(nil, items, params)
}

func (s *BatchService) createJob(sourceFolder *string, items []model.JobItem, params model.JobParams) (*model.Job, error) {
	params.ApplyDefaults()
	if err := params.Validate(); err != nil {
		return nil, err
	}

	job := &model.Job{
		ID:           uuid.New(),
		Status:       model.JobPending,
		SourceFolder: sourceFolder,
		Params:       params,
		TotalItems:   len(items),
		CreatedAt:    time.Now().UTC(),
	}
	for i := range items {
		items[i].ID = uuid.New()
		items[i].JobID = job.ID
		items[i].Status = model.ItemPending
	}

	if err := s.repo.CreateJobWithItems(job, items); err != nil {
		return nil, err
	}

	log.Printf("📤 Submitting job %s (%d items) to worker pool", job.ID, len(items))
	s.pool.Submit(job.ID)
	return job, nil
}

// supportedExts - форматы, которые умеет обрабатывать воркер:
// .mzML напрямую, остальные через msconvert
var supportedExts = map[string]bool{
	".mzml":  true,
	".mzxml": true,
	".raw":   true,
	".d":     true,
}

// scanFolder - сканирует папку и возвращает пути к поддерживаемым файлам
func scanFolder(folderPath string) ([]string, error) {
	// Выбрана сама папка-спектр (sample.d) — это один элемент
	if ext := strings.ToLower(filepath.Ext(folderPath)); ext == ".d" || ext == ".raw" {
		return []string{folderPath}, nil
	}

	var files []string

	err := filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(path))

		if d.IsDir() {
			// .d (Bruker/Agilent) и .raw (Waters) — это папки-спектры:
			// добавляем целиком и не заходим внутрь
			if path != folderPath && (ext == ".d" || ext == ".raw") {
				files = append(files, path)
				return fs.SkipDir
			}
			return nil
		}

		if supportedExts[ext] && ext != ".d" {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

// GetJob - получение задачи
func (s *BatchService) GetJob(id uuid.UUID) (*model.Job, error) {
	return s.repo.GetJobByID(id)
}

// GetJobItems - получение элементов задачи
func (s *BatchService) GetJobItems(id uuid.UUID) ([]model.JobItem, error) {
	return s.repo.GetJobItems(id)
}

// GetJobProgress - получение прогресса
func (s *BatchService) GetJobProgress(id uuid.UUID) (*model.JobProgress, error) {
	return s.repo.GetJobProgress(id)
}

// CancelJob - отмена задачи. Воркер увидит статус cancelling между спектрами,
// пометит оставшиеся элементы как cancelled и завершит задачу.
func (s *BatchService) CancelJob(id uuid.UUID) error {
	if _, err := s.repo.GetJobByID(id); err != nil {
		return err
	}
	ok, err := s.repo.RequestCancel(id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrJobFinished
	}
	// Если задача ждёт в очереди или воркер уже вышел, этот вызов
	// гарантирует, что отмена будет доведена до конца
	s.pool.Submit(id)
	return nil
}

// GetJobResults - получение путей к результатам для ZIP
func (s *BatchService) GetJobResults(id uuid.UUID) ([]JobResultFile, error) {
	items, err := s.repo.GetJobItems(id)
	if err != nil {
		return nil, err
	}

	var files []JobResultFile
	for _, item := range items {
		if item.Status != model.ItemDone {
			continue
		}
		var plot, spectrum string
		if item.ResultPath != nil {
			plot = *item.ResultPath
		}
		if item.SpectrumPath != nil {
			spectrum = *item.SpectrumPath
		}
		if plot == "" && spectrum == "" {
			continue
		}
		files = append(files, JobResultFile{
			ItemID:       item.ID,
			SpectraName:  item.SpectraName,
			PlotPath:     plot,
			SpectrumPath: spectrum,
		})
	}
	return files, nil
}
