package service

import (
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/repository"
	"MassSpectraWorker/internal/worker"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type BatchService struct {
	repo *repository.JobRepository
	pool *worker.WorkerPool
}

func NewBatchService(repo *repository.JobRepository, pool *worker.WorkerPool) *BatchService {
	return &BatchService{
		repo: repo,
		pool: pool,
	}
}

// CreateJobFromFolder - создает задачу из папки
func (s *BatchService) CreateJobFromFolder(folderPath string, params model.JobParams) (*model.Job, error) {
	// Валидация параметров
	if err := params.Validate(); err != nil {
		return nil, err
	}

	// Сканируем папку
	files, err := s.scanFolder(folderPath)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no supported files found in folder")
	}

	// Создаем задачу
	job := &model.Job{
		ID:           uuid.New(),
		Status:       "pending",
		SourceFolder: &folderPath,
		Params:       params,
		TotalItems:   len(files),
		DoneItems:    0,
		FailedItems:  0,
		CreatedAt:    time.Now(),
	}

	if err := s.repo.CreateJob(job); err != nil {
		return nil, err
	}

	// Создаем элементы
	items := make([]model.JobItem, len(files))
	for i, file := range files {
		items[i] = model.JobItem{
			ID:          uuid.New(),
			JobID:       job.ID,
			SpectraName: filepath.Base(file),
			SpectraPath: file,
			Status:      "pending",
		}
	}

	if err := s.repo.CreateJobItems(items); err != nil {
		return nil, err
	}

	// ✅ КЛЮЧЕВОЙ МОМЕНТ: Отправляем задачу в воркер!
	log.Printf("📤 Submitting job %s to worker pool", job.ID)
	s.pool.Submit(job.ID)
	log.Printf("✅ Job %s submitted to worker pool", job.ID)

	return job, nil
}

// CreateJobFromItems - создает задачу из списка файлов
func (s *BatchService) CreateJobFromItems(items []model.JobItem, params model.JobParams) (*model.Job, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("no items provided")
	}

	// Создаем задачу
	job := &model.Job{
		ID:          uuid.New(),
		Status:      "pending",
		Params:      params,
		TotalItems:  len(items),
		DoneItems:   0,
		FailedItems: 0,
		CreatedAt:   time.Now(),
	}

	if err := s.repo.CreateJob(job); err != nil {
		return nil, err
	}

	// Добавляем job_id в элементы
	for i := range items {
		items[i].ID = uuid.New()
		items[i].JobID = job.ID
		items[i].Status = "pending"
	}

	if err := s.repo.CreateJobItems(items); err != nil {
		return nil, err
	}

	return job, nil
}

// scanFolder - сканирует папку и возвращает пути к файлам с поддерживаемыми расширениями
func (s *BatchService) scanFolder(folderPath string) ([]string, error) {
	supportedExts := map[string]bool{
		".csv":   true,
		".mzml":  true,
		".mzxml": true,
		".raw":   true,
		".d":     true, // папка Thermo
	}

	var files []string

	err := filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// Пропускаем папки, но если это .d - добавляем как файл
			if strings.HasSuffix(path, ".d") {
				files = append(files, path)
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if supportedExts[ext] {
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

// CancelJob - отмена задачи
func (s *BatchService) CancelJob(id uuid.UUID) error {
	job, err := s.repo.GetJobByID(id)
	if err != nil {
		return err
	}

	if job.Status == "done" || job.Status == "failed" || job.Status == "cancelled" {
		return fmt.Errorf("job already finished")
	}

	return s.repo.UpdateJobStatus(id, "cancelling")
}

// GetJobResults - получение путей к результатам для ZIP
func (s *BatchService) GetJobResults(id uuid.UUID) ([]string, error) {
	items, err := s.repo.GetJobItems(id)
	if err != nil {
		return nil, err
	}

	var resultPaths []string
	for _, item := range items {
		if item.Status == "done" && item.ResultPath != nil {
			resultPaths = append(resultPaths, *item.ResultPath)
		}
	}
	return resultPaths, nil
}
