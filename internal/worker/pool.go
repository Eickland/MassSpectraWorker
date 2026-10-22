package worker

import (
	"MassSpectraWorker/internal/client"
	"MassSpectraWorker/internal/config"
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/repository"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	cfg          config.Config
}

func NewWorkerPool(repo *repository.JobRepository, pythonClient *client.GRPCClient,
	workers int, cfg config.Config) *WorkerPool {

	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		repo:         repo,
		pythonClient: pythonClient,
		workers:      workers,
		jobs:         make(chan uuid.UUID, 100),
		ctx:          ctx,
		cancel:       cancel,
		cfg:          cfg,
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

// Stop - остановка воркеров. Текущие элементы прерываются и возвращаются
// в очередь; при следующем запуске RecoverInterrupted их подхватит.
func (p *WorkerPool) Stop() {
	p.cancel()
	p.wg.Wait()
	log.Println("Worker pool stopped")
}

// Submit - отправка задачи на обработку. Не блокирует HTTP-хендлер,
// даже если очередь заполнена.
func (p *WorkerPool) Submit(jobID uuid.UUID) {
	select {
	case p.jobs <- jobID:
	case <-p.ctx.Done():
	default:
		go func() {
			select {
			case p.jobs <- jobID:
			case <-p.ctx.Done():
			}
		}()
	}
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
		case jobID := <-p.jobs:
			log.Printf("Worker %d processing job %s", id, jobID)
			p.processJob(jobID)
		}
	}
}

// processJob - обработка одной задачи
func (p *WorkerPool) processJob(jobID uuid.UUID) {
	job, err := p.repo.GetJobByID(jobID)
	if err != nil {
		log.Printf("job %s: load failed: %v", jobID, err)
		return
	}
	if job.IsTerminal() {
		return
	}
	if job.Status == model.JobCancelling {
		p.finishCancelled(jobID)
		return
	}
	if err := p.repo.StartJob(jobID); err != nil {
		log.Printf("job %s: failed to set running: %v", jobID, err)
		return
	}

	for {
		if p.ctx.Err() != nil {
			// Сервер останавливается: задача остаётся running и будет
			// продолжена при следующем запуске
			return
		}

		cancelled, err := p.repo.IsJobCancelled(jobID)
		if err != nil {
			log.Printf("job %s: failed to check cancellation: %v", jobID, err)
			return
		}
		if cancelled {
			p.finishCancelled(jobID)
			return
		}

		item, err := p.repo.ClaimNextItem(jobID)
		if err != nil {
			log.Printf("job %s: failed to claim item: %v", jobID, err)
			if !p.sleep(time.Second) {
				return
			}
			continue
		}
		if item == nil {
			break // элементов больше нет
		}

		procErr := p.processItem(job, item)

		if procErr != nil && p.ctx.Err() != nil {
			// Прервано остановкой сервера, а не ошибкой обработки
			if err := p.repo.ResetItemToPending(item.ID); err != nil {
				log.Printf("item %s: reset failed: %v", item.ID, err)
			}
			return
		}

		if procErr != nil {
			log.Printf("item %s (%s) FAILED: %v", item.ID, item.SpectraName, procErr)
			msg := procErr.Error()
			item.Status = model.ItemFailed
			item.Error = &msg
		} else {
			item.Status = model.ItemDone
			item.Error = nil
		}

		if err := p.repo.UpdateItemResult(*item); err != nil {
			log.Printf("item %s: failed to save result: %v", item.ID, err)
		}
		if _, err := p.repo.RecountJob(jobID); err != nil {
			log.Printf("job %s: failed to update counters: %v", jobID, err)
		}
	}

	p.finishJob(jobID)
}

// finishJob - выставляет итоговый статус, если все элементы обработаны
func (p *WorkerPool) finishJob(jobID uuid.UUID) {
	c, err := p.repo.RecountJob(jobID)
	if err != nil {
		log.Printf("job %s: recount failed: %v", jobID, err)
		return
	}
	if c.Pending > 0 || c.Processing > 0 {
		// Элементы ещё обрабатывает другой воркер — он и завершит задачу
		return
	}

	// Отмену могли запросить, пока обрабатывался последний элемент
	if cancelled, _ := p.repo.IsJobCancelled(jobID); cancelled {
		p.finishCancelled(jobID)
		return
	}

	status := model.JobDone
	if c.Failed > 0 {
		// Фронтенд показывает failed как «Завершено с ошибками»
		status = model.JobFailed
	}
	if err := p.repo.UpdateJobStatus(jobID, status); err != nil {
		log.Printf("job %s: failed to set final status: %v", jobID, err)
	}
	log.Printf("Job %s finished: done=%d, failed=%d", jobID, c.Done, c.Failed)
}

func (p *WorkerPool) finishCancelled(jobID uuid.UUID) {
	if err := p.repo.CancelPendingItems(jobID); err != nil {
		log.Printf("job %s: failed to cancel items: %v", jobID, err)
	}
	c, err := p.repo.RecountJob(jobID)
	if err != nil {
		log.Printf("job %s: recount failed: %v", jobID, err)
		return
	}
	if c.Processing > 0 {
		// Другой воркер ещё дорабатывает спектр: задача остаётся cancelling,
		// он сам завершит её после сохранения результата
		return
	}
	if err := p.repo.UpdateJobStatus(jobID, model.JobCancelled); err != nil {
		log.Printf("job %s: failed to set cancelled: %v", jobID, err)
	}
	log.Printf("Job %s cancelled", jobID)
}

// sleep - пауза, прерываемая остановкой пула. false — пул остановлен.
func (p *WorkerPool) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-p.ctx.Done():
		return false
	}
}

// processItem - обработка одного элемента (спектра)
func (p *WorkerPool) processItem(job *model.Job, item *model.JobItem) error {
	kind := detectInputKind(item.SpectraPath)
	log.Printf("Processing item %s (%s), kind=%s", item.ID, item.SpectraName, kind)

	var mzmlPath string
	switch kind {
	case "mzml":
		mzmlPath = item.SpectraPath
	case "d", "raw", "mzxml":
		mzmlPath = p.mzmlCachePath(item.JobID, item.SpectraName)
		if !fileExists(mzmlPath) {
			if err := p.convertToMzML(item.SpectraPath, mzmlPath); err != nil {
				return fmt.Errorf("convert %s → .mzML: %w", kind, err)
			}
		}
	case "missing":
		return fmt.Errorf("file not found: %s", item.SpectraPath)
	default:
		return fmt.Errorf("unsupported input: %s", item.SpectraPath)
	}

	params := job.Params
	params.ApplyDefaults()

	req := &pb.MassListRequest{
		SpectraPath:    mzmlPath,
		SpectraName:    item.SpectraName,
		LowPercentile:  params.LowPercentile,
		HighPercentile: params.HighPercentile,
		RelError:       params.RelError,
		ChargeMax:      params.ChargeMax,
		BruttoDict:     params.BruttoDict.ToProto(),
		Protocole:      params.Protocol,
		Options:        params.Options,
		Dpi:            params.Dpi,
		Width:          params.Width,
		Height:         params.Height,
		Format:         params.OutputFormat,
	}

	ctx, cancel := context.WithTimeout(p.ctx, p.cfg.GRPCTimeout)
	defer cancel()

	resp, err := p.pythonClient.ProcessMassList(ctx, req)
	if err != nil {
		return fmt.Errorf("python processing failed: %w", err)
	}

	// Сохраняем картинку (расширение — по фактическому формату ответа)
	format := resp.GetFormat()
	if format == "" {
		format = params.OutputFormat
	}
	plotPath := p.resultPath(item.JobID, item.SpectraName, format)
	if err := saveResult(plotPath, resp.ImageData); err != nil {
		return fmt.Errorf("failed to save plot: %w", err)
	}
	item.ResultPath = &plotPath

	// Сохраняем таблицу спектра (если пришла)
	if resp.Spectrum != nil && resp.Spectrum.NRows > 0 {
		sp := p.spectrumPath(item.JobID, item.SpectraName)
		if err := saveSpectrumCSV(sp, resp.Spectrum); err != nil {
			return fmt.Errorf("failed to save spectrum: %w", err)
		}
		info, err := os.Stat(sp)
		if err != nil {
			return fmt.Errorf("failed to stat spectrum: %w", err)
		}
		size := info.Size()
		rows := int(resp.Spectrum.NRows)
		cols := int(resp.Spectrum.NCols)
		item.SpectrumPath = &sp
		item.SpectrumRows = &rows
		item.SpectrumCols = &cols
		item.SpectrumSize = &size
	}

	log.Printf("Item %s processed: plot=%d bytes, spectrum_rows=%d",
		item.ID, len(resp.ImageData), resp.GetSpectrum().GetNRows())
	return nil
}

// saveResult - сохраняет бинарные данные в файл
func saveResult(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func baseName(spectraName string) string {
	return strings.TrimSuffix(spectraName, filepath.Ext(spectraName))
}

func (p *WorkerPool) resultPath(jobID uuid.UUID, spectraName, format string) string {
	ext := strings.ToLower(format)
	if ext == "jpeg" {
		ext = "jpg"
	}
	return filepath.Join(p.cfg.ResultsDir, jobID.String(), baseName(spectraName)+"."+ext)
}

func (p *WorkerPool) spectrumPath(jobID uuid.UUID, spectraName string) string {
	return filepath.Join(p.cfg.ResultsDir, jobID.String(), baseName(spectraName)+"_spectrum.csv")
}

func (p *WorkerPool) mzmlCachePath(jobID uuid.UUID, spectraName string) string {
	return filepath.Join(p.cfg.WorkDir, jobID.String(), baseName(spectraName)+".mzML")
}

func saveSpectrumCSV(path string, t *pb.MassSpectrumTable) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	w := csv.NewWriter(f)

	header := make([]string, 0, len(t.Columns))
	nulls := make([]map[int]bool, len(t.Columns))
	for j, c := range t.Columns {
		header = append(header, c.Name)
		if len(c.NullMask) > 0 {
			m := make(map[int]bool, len(c.NullMask))
			for _, idx := range c.NullMask {
				m[int(idx)] = true
			}
			nulls[j] = m
		}
	}
	if err := w.Write(header); err != nil {
		return err
	}

	rows := int(t.NRows)
	row := make([]string, len(t.Columns))
	for i := 0; i < rows; i++ {
		for j, c := range t.Columns {
			if nulls[j][i] {
				row[j] = "" // NaN/None: Python кладёт 0 в данные и индекс в null_mask
				continue
			}
			row[j] = columnValueAt(c, i)
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
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

// detectInputKind - "mzml" | "mzxml" | "d" | "raw" | "missing" | "unknown"
func detectInputKind(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	ext := strings.ToLower(filepath.Ext(path))
	if info.IsDir() {
		switch ext {
		case ".d":
			return "d" // Bruker/Agilent
		case ".raw":
			return "raw" // Waters
		}
		return "unknown"
	}
	switch ext {
	case ".mzml":
		return "mzml"
	case ".mzxml":
		return "mzxml"
	case ".raw":
		return "raw" // Thermo
	}
	return "unknown"
}

func (p *WorkerPool) convertToMzML(src, dst string) error {
	if p.cfg.MsConvert == "" {
		return fmt.Errorf("MSCONVERT_PATH is not set")
	}
	ctx, cancel := context.WithTimeout(p.ctx, p.cfg.ConvertTimeout)
	defer cancel()

	// Конвертируем во временную папку и только потом переносим результат:
	// прерванная конвертация не оставит недописанный .mzML в кэше,
	// а имя выходного файла msconvert (по имени исходника) может не совпадать
	// с именем элемента, заданным пользователем.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	outDir, err := os.MkdirTemp(filepath.Dir(dst), ".converting-")
	if err != nil {
		return fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(outDir)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, p.cfg.MsConvert,
		src, "-o", outDir,
		"--mzML", "--filter", "zeroSamples removeExtra", "--verbose")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("msconvert: running %s %v", p.cfg.MsConvert, cmd.Args[1:])

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("msconvert exit: %w\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}

	entries, _ := os.ReadDir(outDir)
	var produced []string
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".mzml") {
			produced = append(produced, filepath.Join(outDir, e.Name()))
		}
	}
	if len(produced) != 1 {
		return fmt.Errorf(
			"msconvert exit 0 but expected exactly one .mzML, files in %q: %v\nstdout:\n%s\nstderr:\n%s",
			outDir, names, stdout.String(), stderr.String())
	}
	if err := os.Rename(produced[0], dst); err != nil {
		return fmt.Errorf("move converted file: %w", err)
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
