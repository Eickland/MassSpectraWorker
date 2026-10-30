package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"MassSpectraWorker/internal/client"
	"MassSpectraWorker/internal/config"
	"MassSpectraWorker/internal/model"
	"MassSpectraWorker/internal/repository"
	"MassSpectraWorker/internal/service"
	"MassSpectraWorker/internal/storage"
	"MassSpectraWorker/internal/worker"
	"MassSpectraWorker/migrations"
	pb "MassSpectraWorker/src/protobuf"
)

// fakePython - подмена Python-сервиса: запоминает запросы и отдаёт
// картинку и маленькую таблицу с пропуском (NaN) во второй строке.
type fakePython struct {
	pb.UnimplementedMassListServiceServer
	mu           sync.Mutex
	reqs         []*pb.MassListRequest
	delay        time.Duration
	failProtocol string // запросы с этим протоколом завершаются ошибкой
}

func (f *fakePython) ProcessMassList(ctx context.Context, req *pb.MassListRequest) (*pb.MassListResponse, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	failProto := f.failProtocol
	f.mu.Unlock()
	if failProto != "" && req.Protocole == failProto {
		return nil, status.Error(codes.Internal, "assign failed")
	}
	if strings.Contains(req.SpectraName, "bad") {
		return nil, status.Error(codes.InvalidArgument, "empty mass list")
	}
	return &pb.MassListResponse{
		ImageData: []byte("fake-image"),
		Format:    req.Format,
		Spectrum: &pb.MassSpectrumTable{
			NRows: 2,
			NCols: 2,
			Columns: []*pb.Column{
				{Name: "mass", Dtype: "float64", DoubleData: []float64{200.5, 300.25}},
				{Name: "C", Dtype: "float64", DoubleData: []float64{10, 0}, NullMask: []uint32{1}},
			},
		},
	}, nil
}

func (f *fakePython) requests() []*pb.MassListRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*pb.MassListRequest(nil), f.reqs...)
}

type env struct {
	repo *repository.JobRepository
	pool *worker.WorkerPool
	svc  *service.BatchService
	fake *fakePython
	cfg  config.Config
	dir  string
}

func setup(t *testing.T, delay time.Duration) *env {
	t.Helper()
	return setupWith(t, delay, nil)
}

// setupWith - как setup, но позволяет поправить конфигурацию пула (например, MsConvert)
func setupWith(t *testing.T, delay time.Duration, tweak func(*config.Config)) *env {
	t.Helper()
	dir := t.TempDir()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakePython{delay: delay}
	srv := grpc.NewServer()
	pb.RegisterMassListServiceServer(srv, fake)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	cl, err := client.NewClient(lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })

	db, err := storage.Open(filepath.Join(dir, "data", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := config.Config{
		ResultsDir:     filepath.Join(dir, "results"),
		WorkDir:        filepath.Join(dir, "work"),
		ConvertTimeout: time.Minute,
		GRPCTimeout:    10 * time.Second,
		Workers:        2,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	repo := repository.NewJobRepository(db)
	pool := worker.NewWorkerPool(repo, cl, cfg.Workers, cfg)
	pool.Start()
	t.Cleanup(pool.Stop)

	return &env{repo: repo, pool: pool, svc: service.NewBatchService(repo, pool),
		fake: fake, cfg: cfg, dir: dir}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func waitTerminal(t *testing.T, repo *repository.JobRepository, id uuid.UUID) *model.Job {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetJobByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.IsTerminal() {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish in time", id)
	return nil
}

// Параметры из формы (в том виде, в каком их шлёт batch_mass_list.js)
// должны дойти до gRPC-запроса без потерь.
func TestBatchJobPassesParamsAndSavesResults(t *testing.T) {
	e := setup(t, 0)

	in := filepath.Join(e.dir, "in")
	touch(t, filepath.Join(in, "a.mzML"))
	touch(t, filepath.Join(in, "sub", "b.mzml"))
	touch(t, filepath.Join(in, "bad_one.mzML"))
	touch(t, filepath.Join(in, "res.csv")) // не спектр — не должен попасть в задачу

	var params model.JobParams
	payload := `{
		"low_percentile": 99.5, "high_percentile": 99.9, "rel_error": 1.5,
		"charge_max": 3, "protocol": "tmds",
		"brutto_dict": {"C": {"min": 4, "max": 40}, "N": [0, 2]},
		"options": {"recalibrate": "false"},
		"width": 8, "height": 5, "dpi": 120, "output_format": "svg"
	}`
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		t.Fatal(err)
	}

	job, err := e.svc.CreateJobFromFolder(in, params)
	if err != nil {
		t.Fatal(err)
	}
	if job.TotalItems != 3 {
		t.Fatalf("total items = %d, want 3 (csv must be skipped)", job.TotalItems)
	}

	final := waitTerminal(t, e.repo, job.ID)
	if final.Status != model.JobFailed || final.DoneItems != 2 || final.FailedItems != 1 {
		t.Fatalf("final job = %s done=%d failed=%d, want failed/2/1",
			final.Status, final.DoneItems, final.FailedItems)
	}
	if final.FinishedAt == nil {
		t.Fatal("finished_at not set")
	}

	reqs := e.fake.requests()
	if len(reqs) != 3 {
		t.Fatalf("python called %d times, want 3", len(reqs))
	}
	r := reqs[0]
	if r.ChargeMax != 3 || r.RelError != 1.5 || r.Protocole != "tmds" || r.Format != "svg" ||
		r.Dpi != 120 || r.Width != 8 || r.Height != 5 {
		t.Fatalf("params lost: %+v", r)
	}
	if c := r.BruttoDict["C"]; c == nil || c.Min != 4 || c.Max != 40 {
		t.Fatalf("brutto C = %+v", c)
	}
	if n := r.BruttoDict["N"]; n == nil || n.Min != 0 || n.Max != 2 {
		t.Fatalf("brutto N = %+v", n)
	}
	if r.Options["recalibrate"] != "false" {
		t.Fatalf("options lost: %v", r.Options)
	}

	items, err := e.repo.GetJobItems(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		switch it.Status {
		case model.ItemDone:
			if it.ResultPath == nil || !strings.HasSuffix(*it.ResultPath, ".svg") {
				t.Fatalf("plot path = %v, want .svg", it.ResultPath)
			}
			if it.StartedAt == nil || it.FinishedAt == nil {
				t.Fatal("timestamps not set")
			}
			csv, err := os.ReadFile(*it.SpectrumPath)
			if err != nil {
				t.Fatal(err)
			}
			want := "mass,C\n200.5,10\n300.25,\n"
			if string(csv) != want {
				t.Fatalf("csv = %q, want %q (NaN must be empty, not 0)", csv, want)
			}
		case model.ItemFailed:
			if it.Error == nil || !strings.Contains(*it.Error, "empty mass list") {
				t.Fatalf("error = %v", it.Error)
			}
		default:
			t.Fatalf("item %s left in status %s", it.SpectraName, it.Status)
		}
	}
}

func TestCancelJob(t *testing.T) {
	e := setup(t, 150*time.Millisecond)

	var items []model.JobItem
	for i := 0; i < 6; i++ {
		p := filepath.Join(e.dir, "in", fmt.Sprintf("s%d.mzML", i))
		touch(t, p)
		items = append(items, model.JobItem{SpectraPath: p})
	}
	job, err := e.svc.CreateJobFromItems(items, model.JobParams{Protocol: "non_tmds"})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	if err := e.svc.CancelJob(job.ID); err != nil {
		t.Fatal(err)
	}

	final := waitTerminal(t, e.repo, job.ID)
	if final.Status != model.JobCancelled {
		t.Fatalf("status = %s, want cancelled", final.Status)
	}
	got, _ := e.repo.GetJobItems(job.ID)
	var cancelled int
	for _, it := range got {
		if it.Status == model.ItemPending || it.Status == model.ItemProcessing {
			t.Fatalf("item %s left %s after cancel", it.SpectraName, it.Status)
		}
		if it.Status == model.ItemCancelled {
			cancelled++
		}
	}
	if cancelled == 0 {
		t.Fatal("expected some items to be cancelled")
	}
	if err := e.svc.CancelJob(job.ID); err != service.ErrJobFinished {
		t.Fatalf("second cancel err = %v, want ErrJobFinished", err)
	}
}

func TestRecoverInterruptedAndResume(t *testing.T) {
	e := setup(t, 0)

	p := filepath.Join(e.dir, "in", "x.mzML")
	touch(t, p)
	job := &model.Job{ID: uuid.New(), Status: model.JobRunning,
		Params: model.JobParams{Protocol: "non_tmds"}, TotalItems: 1}
	item := model.JobItem{ID: uuid.New(), JobID: job.ID, SpectraName: "x.mzML",
		SpectraPath: p, Status: model.ItemPending}
	if err := e.repo.CreateJobWithItems(job, []model.JobItem{item}); err != nil {
		t.Fatal(err)
	}
	// Имитируем падение сервера посреди обработки
	if claimed, err := e.repo.ClaimNextItem(job.ID); err != nil || claimed == nil {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	if again, _ := e.repo.ClaimNextItem(job.ID); again != nil {
		t.Fatal("the same item was claimed twice")
	}

	ids, err := e.repo.RecoverInterrupted()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != job.ID {
		t.Fatalf("recovered = %v", ids)
	}
	e.pool.Submit(job.ID)
	final := waitTerminal(t, e.repo, job.ID)
	if final.Status != model.JobDone || final.DoneItems != 1 {
		t.Fatalf("after resume: %s done=%d", final.Status, final.DoneItems)
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	for i := 0; i < 2; i++ {
		db, err := storage.Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		var v int
		if err := db.Get(&v, "PRAGMA user_version"); err != nil || v != 3 {
			t.Fatalf("user_version = %d, %v", v, err)
		}
		db.Close()
	}
}

func (f *fakePython) lastFor(name string) *pb.MassListRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.reqs) - 1; i >= 0; i-- {
		if f.reqs[i].SpectraName == name {
			return f.reqs[i]
		}
	}
	return nil
}

// Спектр пересчитывается со своими параметрами, история прогонов сохраняется,
// а любой успешный прогон можно снова сделать текущим.
func TestReprocessItemAndSelectRun(t *testing.T) {
	e := setup(t, 0)

	var items []model.JobItem
	for _, n := range []string{"one.mzML", "two.mzML"} {
		p := filepath.Join(e.dir, "in", n)
		touch(t, p)
		items = append(items, model.JobItem{SpectraPath: p})
	}
	job, err := e.svc.CreateJobFromItems(items, model.JobParams{Protocol: "non_tmds", RelError: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if final := waitTerminal(t, e.repo, job.ID); final.Status != model.JobDone {
		t.Fatalf("first pass: %s", final.Status)
	}

	all, _ := e.repo.GetJobItems(job.ID)
	one := all[0]
	firstResult := *one.ResultPath

	// Пересчитываем только первый спектр: другая ошибка и свой брутто-словарь
	override := json.RawMessage(`{"rel_error": 2.5, "brutto_dict": {"C": [5, 30]}, "options": {"recalibrate": "false"}}`)
	if err := e.svc.ReprocessItem(job.ID, one.ID, override, false); err != nil {
		t.Fatal(err)
	}
	// Повторный запрос, пока спектр в очереди, отклоняется
	if err := e.svc.ReprocessItem(job.ID, one.ID, override, false); !errors.Is(err, repository.ErrItemBusy) {
		// спектр мог успеть обработаться — тогда это тоже корректно
		if err != nil {
			t.Fatalf("second reprocess err = %v", err)
		}
	}

	final := waitTerminal(t, e.repo, job.ID)
	if final.Status != model.JobDone || final.DoneItems != 2 {
		t.Fatalf("after reprocess: %s done=%d", final.Status, final.DoneItems)
	}

	req := e.fake.lastFor("one.mzML")
	if req.RelError != 2.5 || req.BruttoDict["C"].GetMin() != 5 || req.BruttoDict["C"].GetMax() != 30 ||
		req.Options["recalibrate"] != "false" {
		t.Fatalf("reprocess params not passed: %+v", req)
	}
	if two := e.fake.lastFor("two.mzML"); two.RelError != 0.5 {
		t.Fatalf("other spectrum must keep job params, rel_error = %v", two.RelError)
	}

	d, err := e.svc.GetItemDetail(job.ID, one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !d.CustomParams || d.EffectiveParams.RelError != 2.5 || d.JobParams.RelError != 0.5 {
		t.Fatalf("detail params: custom=%v eff=%v job=%v", d.CustomParams, d.EffectiveParams.RelError, d.JobParams.RelError)
	}
	if len(d.Runs) != 2 || d.Runs[0].RunNo != 2 || !d.Runs[0].Current || d.Runs[1].Current {
		t.Fatalf("runs = %+v", d.Runs)
	}
	newResult := *d.Item.ResultPath
	if newResult == firstResult || !strings.Contains(filepath.Base(newResult), "__r2") {
		t.Fatalf("run 2 must not overwrite run 1: %s vs %s", newResult, firstResult)
	}
	for _, p := range []string{firstResult, newResult} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("result file missing: %v", err)
		}
	}

	// Возвращаем первый прогон
	if err := e.svc.SelectRun(job.ID, one.ID, d.Runs[1].ID); err != nil {
		t.Fatal(err)
	}
	d, _ = e.svc.GetItemDetail(job.ID, one.ID)
	if *d.Item.ResultPath != firstResult || !d.Runs[1].Current || d.EffectiveParams.RelError != 0.5 {
		t.Fatalf("select run 1: path=%s current=%v rel=%v", *d.Item.ResultPath, d.Runs[1].Current, d.EffectiveParams.RelError)
	}
	files, _ := e.svc.GetJobResults(job.ID)
	for _, f := range files {
		if f.ItemID == one.ID && f.PlotPath != firstResult {
			t.Fatalf("zip must use selected run: %s", f.PlotPath)
		}
	}

	// Неверные параметры → ошибка валидации, спектр не трогаем
	var verr *service.ValidationError
	if err := e.svc.ReprocessItem(job.ID, one.ID, json.RawMessage(`{"rel_error": -1}`), false); !errors.As(err, &verr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	// Сброс к параметрам задачи
	if err := e.svc.ReprocessItem(job.ID, one.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, e.repo, job.ID)
	if r := e.fake.lastFor("one.mzML"); r.RelError != 0.5 || r.BruttoDict != nil {
		t.Fatalf("reset to job params failed: %+v", r)
	}
}

// Неудачный пересчёт не теряет прошлый удачный результат
func TestFailedReprocessKeepsPreviousRun(t *testing.T) {
	e := setup(t, 0)
	p := filepath.Join(e.dir, "in", "s.mzML")
	touch(t, p)
	job, err := e.svc.CreateJobFromItems([]model.JobItem{{SpectraPath: p}}, model.JobParams{Protocol: "non_tmds"})
	if err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, e.repo, job.ID)
	items, _ := e.repo.GetJobItems(job.ID)
	it := items[0]

	// Фейковый сервис падает на протоколе custom
	e.fake.mu.Lock()
	e.fake.failProtocol = "custom"
	e.fake.mu.Unlock()
	if err := e.svc.ReprocessItem(job.ID, it.ID, json.RawMessage(`{"protocol":"custom"}`), false); err != nil {
		t.Fatal(err)
	}
	final := waitTerminal(t, e.repo, job.ID)
	if final.Status != model.JobFailed || final.FailedItems != 1 {
		t.Fatalf("status %s failed=%d", final.Status, final.FailedItems)
	}
	d, _ := e.svc.GetItemDetail(job.ID, it.ID)
	if len(d.Runs) != 2 || d.Runs[0].Status != model.RunFailed || !d.Runs[1].Current {
		t.Fatalf("runs = %+v", d.Runs)
	}
	// Возвращаем удачный прогон — задача снова done
	if err := e.svc.SelectRun(job.ID, it.ID, d.Runs[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SelectRun(job.ID, it.ID, d.Runs[0].ID); !errors.Is(err, repository.ErrRunNotUsable) {
		t.Fatalf("selecting failed run: %v", err)
	}
	if j, _ := e.repo.GetJobByID(job.ID); j.Status != model.JobDone || j.FailedItems != 0 {
		t.Fatalf("job after select: %s failed=%d", j.Status, j.FailedItems)
	}
}

// Параметры отдельных спектров можно задать прямо при создании задачи
func TestCreateJobWithPerItemParams(t *testing.T) {
	e := setup(t, 0)
	a := filepath.Join(e.dir, "in", "a.mzML")
	b := filepath.Join(e.dir, "in", "b.mzML")
	touch(t, a)
	touch(t, b)
	job, err := e.svc.CreateJobWithOverrides([]service.ItemOverride{
		{Path: a, Params: json.RawMessage(`{"charge_max": 3, "protocol": "tmds"}`)},
		{Path: b},
	}, model.JobParams{Protocol: "non_tmds"})
	if err != nil {
		t.Fatal(err)
	}
	waitTerminal(t, e.repo, job.ID)
	if r := e.fake.lastFor("a.mzML"); r.ChargeMax != 3 || r.Protocole != "tmds" {
		t.Fatalf("a: %+v", r)
	}
	if r := e.fake.lastFor("b.mzML"); r.ChargeMax != 1 || r.Protocole != "non_tmds" {
		t.Fatalf("b: %+v", r)
	}
	_, err = e.svc.CreateJobWithOverrides([]service.ItemOverride{
		{Path: a, Params: json.RawMessage(`{"protocol": "nope"}`)},
	}, model.JobParams{Protocol: "non_tmds"})
	if err == nil {
		t.Fatal("invalid per-item params must be rejected")
	}
}

// Результаты, посчитанные до обновления, попадают в историю как прогон №1
func TestMigrationBackfillsRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Откатываемся к схеме v2 и кладём «старый» результат
	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DROP TABLE item_runs",
		"ALTER TABLE batch_job_items DROP COLUMN current_run_id",
		"ALTER TABLE batch_job_items DROP COLUMN params_json",
		"PRAGMA user_version = 2",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	jobID, itemID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	db.MustExec(`INSERT INTO batch_jobs (id, status, params_json, total_items, done_items, created_at)
		VALUES (?, 'done', '{"rel_error":0.7,"protocol":"non_tmds"}', 1, 1, ?)`, jobID, now)
	db.MustExec(`INSERT INTO batch_job_items (id, job_id, spectra_name, spectra_path, status, result_path, created_at, finished_at)
		VALUES (?, ?, 'old.mzML', '/x/old.mzML', 'done', '/r/old.png', ?, ?)`, itemID, jobID, now, now)

	sub := fstest.MapFS{}
	all, _ := fs.Glob(migrations.FS, "*.sql")
	for _, n := range all {
		b, _ := fs.ReadFile(migrations.FS, n)
		sub[n] = &fstest.MapFile{Data: b}
	}
	if err := storage.Migrate(db, sub); err != nil {
		t.Fatal(err)
	}

	repo := repository.NewJobRepository(db)
	runs, err := repo.ListRuns(itemID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs = %v, %v", runs, err)
	}
	if runs[0].RunNo != 1 || runs[0].Params.RelError != 0.7 || *runs[0].ResultPath != "/r/old.png" {
		t.Fatalf("backfilled run = %+v", runs[0])
	}
	item, err := repo.GetItem(jobID, itemID)
	if err != nil || item.CurrentRunID == nil || *item.CurrentRunID != runs[0].ID || item.Params != nil {
		t.Fatalf("item after backfill = %+v, %v", item, err)
	}
	db.Close()
}
