package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
	pb "MassSpectraWorker/src/protobuf"
)

// fakePython - подмена Python-сервиса: запоминает запросы и отдаёт
// картинку и маленькую таблицу с пропуском (NaN) во второй строке.
type fakePython struct {
	pb.UnimplementedMassListServiceServer
	mu    sync.Mutex
	reqs  []*pb.MassListRequest
	delay time.Duration
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
		if err := db.Get(&v, "PRAGMA user_version"); err != nil || v != 2 {
			t.Fatalf("user_version = %d, %v", v, err)
		}
		db.Close()
	}
}
