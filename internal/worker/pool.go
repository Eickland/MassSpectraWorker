package worker

import (
	"MassSpectraWorker/internal/client"
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/repository"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	pb "MassSpectraWorker/src/protobuf"
)

type WorkerPool struct {
	repo         *repository.JobRepository
	pythonClient *client.GRPCClient
	workers      int
	jobs         chan uuid.UUID
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
}

func NewWorkerPool(repo *repository.JobRepository,
	pythonClient *client.GRPCClient, workers int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		repo:         repo,
		pythonClient: pythonClient,
		workers:      workers,
		jobs:         make(chan uuid.UUID, 100),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start - запуск воркеров
func (p *WorkerPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	log.Printf("Worker pool started with %d workers", p.workers)
}

// Stop - остановка воркеров
func (p *WorkerPool) Stop() {
	p.cancel()
	close(p.jobs)
	p.wg.Wait()
	log.Println("Worker pool stopped")
}

// Submit - отправка задачи на обработку
func (p *WorkerPool) Submit(jobID uuid.UUID) {
	p.jobs <- jobID
}

// worker - основной цикл воркера
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()
	log.Printf("Worker %d started", id)

	for {
		select {
		case <-p.ctx.Done():
			log.Printf("Worker %d stopped", id)
			return

		case jobID, ok := <-p.jobs:
			if !ok {
				return
			}
			log.Printf("Worker %d processing job %s", id, jobID)
			p.processJob(jobID)
		}
	}
}

// processJob - обработка одной задачи
func (p *WorkerPool) processJob(jobID uuid.UUID) {
	// Обновляем статус задачи на running
	if err := p.repo.UpdateJobStatus(jobID, "running"); err != nil {
		log.Printf("Failed to update job status to running: %v", err)
		return
	}

	var doneItems, failedItems int

	// Обрабатываем элементы по одному
	for {
		// Проверяем, не отменена ли задача
		cancelled, err := p.repo.IsJobCancelled(jobID)
		if err != nil {
			log.Printf("Failed to check cancellation: %v", err)
			break
		}
		if cancelled {
			p.repo.UpdateJobStatus(jobID, "cancelled")
			log.Printf("Job %s cancelled", jobID)
			return
		}

		// Забираем следующий элемент для обработки
		items, err := p.repo.GetPendingItems(jobID, 1)
		if err != nil {
			log.Printf("Failed to get pending items: %v", err)
			time.Sleep(time.Second)
			continue
		}

		if len(items) == 0 {
			// Нет больше элементов
			break
		}

		item := items[0]

		// Обрабатываем элемент
		err = p.processItem(&item)

		// Обновляем статус элемента
		var status string
		var resultPath, errMsg *string
		if err != nil {
			status = "failed"
			errMsg = new(err.Error())
			failedItems++
		} else {
			status = "done"
			resultPath = new(fmt.Sprintf("/results/%s/%s.png", jobID, item.SpectraName))
			doneItems++
		}

		if updateErr := p.repo.UpdateItemStatus(item.ID, status, resultPath, errMsg); updateErr != nil {
			log.Printf("Failed to update item status: %v", updateErr)
		}

		// Обновляем счетчики задачи
		if err := p.repo.UpdateJobCounters(jobID, doneItems, failedItems); err != nil {
			log.Printf("Failed to update job counters: %v", err)
		}

		// Небольшая задержка, чтобы не перегружать БД
		time.Sleep(100 * time.Millisecond)
	}

	// Определяем финальный статус
	//totalItems := doneItems + failedItems
	if failedItems > 0 && doneItems == 0 {
		p.repo.UpdateJobStatus(jobID, "failed")
	} else if failedItems > 0 && doneItems > 0 {
		p.repo.UpdateJobStatus(jobID, "failed") // или "partial" если нужно
	} else {
		p.repo.UpdateJobStatus(jobID, "done")
	}

	log.Printf("Job %s finished: done=%d, failed=%d", jobID, doneItems, failedItems)
}

// processItem - обработка одного элемента (спектра)
func (p *WorkerPool) processItem(item *model.JobItem) error {
	log.Printf("Processing item %s (%s)", item.ID, item.SpectraName)

	cancelled, err := p.repo.IsJobCancelled(item.JobID)
	if err != nil {
		return err
	}
	if cancelled {
		return fmt.Errorf("job cancelled before processing")
	}

	// 1. Получаем параметры задачи (через JOIN)
	job, err := p.repo.GetJobByID(item.JobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	// 2. Формируем запрос к Python
	req := &pb.MassListRequest{
		SpectraPath:    item.SpectraPath,
		SpectraName:    item.SpectraName,
		LowPercentile:  job.Params.LowPercentile,
		HighPercentile: job.Params.HighPercentile,
		RelError:       job.Params.RelError,
		Dpi:            job.Params.Dpi,
		Width:          job.Params.Width,
		Height:         job.Params.Height,
		Format:         job.Params.OutputFormat,
	}
	log.Printf("Item %s name", req.SpectraName)
	// 3. Вызываем Python-сервис
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := p.pythonClient.ProcessMassList(ctx, req)
	if err != nil {
		return fmt.Errorf("python processing failed: %w", err)
	}

	// 4. Сохраняем результат
	resultPath := fmt.Sprintf("/results/%s/%s.png", item.JobID, item.SpectraName)
	if err := p.saveResult(resultPath, resp.ImageData); err != nil {
		return fmt.Errorf("failed to save result: %w", err)
	}

	log.Printf("Item %s processed successfully, size: %d bytes", item.ID, resp.SizeBytes)
	return nil
}

// saveResult - сохраняет бинарные данные в файл
func (p *WorkerPool) saveResult(path string, data []byte) error {
	// Создаём директорию если её нет
	dir := path[:len(path)-len("/"+path[strings.LastIndex(path, "/")+1:])]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
