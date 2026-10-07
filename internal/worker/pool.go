package worker

import (
	"MassSpectraWorker/internal/client"
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/repository"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"MassSpectraWorker/internal/config"

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
	cfg          config.Config
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
		cfg:          config.LoadConfig(),
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

func (p *WorkerPool) spectrumPath(jobID uuid.UUID, spectraName string) (string, error) {
	base := strings.TrimSuffix(spectraName, filepath.Ext(spectraName))
	// формат хранения спектра — CSV или Parquet
	ext := ".csv" // или ".parquet" — см. п.4
	return filepath.Join(p.cfg.ResultsDir, jobID.String(), base+"_spectrum"+ext), nil
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

		err = p.processItem(&item)

		if err != nil {
			item.Status = "failed"
			item.Error = new(err.Error())
			failedItems++
		} else {
			item.Status = "done"
			doneItems++
		}

		if updateErr := p.repo.UpdateItemResult(item); updateErr != nil {
			log.Printf("Failed to update item: %v", updateErr)
		}

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

	job, err := p.repo.GetJobByID(item.JobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	kind := detectInputKind(item.SpectraPath) // "mzml" | "d" | "unknown"

	var mzmlPath string
	switch kind {
	case "mzml":
		mzmlPath = item.SpectraPath
	case "d":
		// 2. Конвертируем .d → .mzML (с кэшем)
		mzmlPath = p.mzmlCachePath(item.JobID, item.SpectraName)
		if !fileExists(mzmlPath) {
			if err := p.repo.UpdateItemStatusSimple(item.ID, "converting"); err != nil {
				return err
			}
			if err := p.convertDToMzML(item.SpectraPath, mzmlPath); err != nil {
				return fmt.Errorf("convert .d → .mzML: %w", err)
			}
			if err := p.repo.UpdateItemStatusSimple(item.ID, "processing"); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported input: %s", item.SpectraPath)
	}

	req := &pb.MassListRequest{
		SpectraPath:    mzmlPath,
		SpectraName:    item.SpectraName,
		LowPercentile:  job.Params.LowPercentile,
		HighPercentile: job.Params.HighPercentile,
		RelError:       job.Params.RelError,
		Dpi:            job.Params.Dpi,
		Width:          job.Params.Width,
		Height:         job.Params.Height,
		Format:         job.Params.OutputFormat,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := p.pythonClient.ProcessMassList(ctx, req)
	if err != nil {
		return fmt.Errorf("python processing failed: %w", err)
	}

	// 4a. Сохраняем картинку
	plotPath := p.resultPath(item.JobID, item.SpectraName)
	if err := p.saveResult(plotPath, resp.ImageData); err != nil {
		return fmt.Errorf("failed to save plot: %w", err)
	}

	// 4b. Сохраняем спектр (если пришёл)
	var spectrumPath *string
	var spectrumRows, spectrumCols *int
	var spectrumSize *int64

	if resp.Spectrum != nil && resp.Spectrum.NRows > 0 {
		sp, err := p.spectrumPath(item.JobID, item.SpectraName)
		if err != nil {
			return fmt.Errorf("failed to save spectrum: %w", err)
		}

		if err := p.saveSpectrumCSV(sp, resp.Spectrum); err != nil {
			return fmt.Errorf("failed to save spectrum: %w", err)
		}
		info, err := os.Stat(sp)
		if err != nil {
			return fmt.Errorf("failed to stat spectrum: %w", err)
		}
		size := info.Size()

		rows := int(resp.Spectrum.NRows)
		cols := int(resp.Spectrum.NCols)
		spectrumPath = &sp
		spectrumRows = &rows
		spectrumCols = &cols
		spectrumSize = &size
	}

	// 5. Одним вызовом обновляем item: статус + пути + метаданные
	item.Status = "done"
	item.ResultPath = &plotPath
	item.SpectrumPath = spectrumPath
	item.SpectrumRows = spectrumRows
	item.SpectrumCols = spectrumCols
	item.SpectrumSize = spectrumSize

	log.Printf("Item %s processed: plot=%d bytes, spectrum_rows=%v",
		item.ID, len(resp.ImageData), resp.Spectrum.GetNRows())
	return nil
}

// saveResult - сохраняет бинарные данные в файл
func (p *WorkerPool) saveResult(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (p *WorkerPool) resultPath(jobID uuid.UUID, spectraName string) string {
	base := strings.TrimSuffix(spectraName, filepath.Ext(spectraName))
	return filepath.Join(p.cfg.ResultsDir, jobID.String(), base+".png")
}

func (p *WorkerPool) mzmlCachePath(jobID uuid.UUID, spectraName string) string {
	base := strings.TrimSuffix(spectraName, filepath.Ext(spectraName))
	return filepath.Join(p.cfg.WorkDir, jobID.String(), base+".mzML")
}

func (p *WorkerPool) saveSpectrumCSV(path string, t *pb.MassSpectrumTable) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	// заголовок
	header := make([]string, 0, len(t.Columns))
	for _, c := range t.Columns {
		header = append(header, c.Name)
	}
	if err := w.Write(header); err != nil {
		return err
	}

	// строки
	rows := int(t.NRows)
	for i := 0; i < rows; i++ {
		row := make([]string, len(t.Columns))
		for j, c := range t.Columns {
			row[j] = columnValueAt(c, i)
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func columnValueAt(c *pb.Column, i int) string {
	switch c.Dtype {
	case "float64":
		if len(c.DoubleData) > i {
			return strconv.FormatFloat(c.DoubleData[i], 'f', -1, 64)
		}
	case "int64":
		if len(c.IntData) > i {
			return strconv.FormatInt(c.IntData[i], 10)
		}
	case "bool":
		if len(c.BoolData) > i {
			return strconv.FormatBool(c.BoolData[i])
		}
	case "string":
		if len(c.StrData) > i {
			return c.StrData[i]
		}
	}
	return ""
}

func detectInputKind(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "unknown"
	}
	if info.IsDir() {
		// .d — это директория, обычно с расширением .d на конце
		if strings.HasSuffix(strings.ToLower(path), ".d") {
			return "d"
		}
		return "unknown"
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mzml":
		return "mzml"
	case ".mzxml":
		return "mzxml" // ProteoWizard тоже умеет
	}
	return "unknown"
}

func (p *WorkerPool) convertDToMzML(src, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.cfg.MsConvert,
		src, "-o", filepath.Dir(dst),
		"--mzML", "--filter", "zeroSamples removeExtra", "--verbose")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("msconvert: %w", err)
	}
	if !fileExists(dst) {
		return fmt.Errorf("msconvert finished but %s not found (name mismatch?)", dst)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || errors.Is(err, os.ErrNotExist)
}
