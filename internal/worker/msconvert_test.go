package worker_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"MassSpectraWorker/internal/config"
	"MassSpectraWorker/internal/model"
)

// fakeMsConvert - скрипт вместо msconvert: пишет свои аргументы в журнал и создаёт
// .mzML с меткой centroid (если есть peakPicking) или profile.
func fakeMsConvert(t *testing.T, dir string) (exe, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake msconvert is a shell script")
	}
	logPath = filepath.Join(dir, "msconvert.log")
	exe = filepath.Join(dir, "msconvert")
	script := `#!/bin/sh
printf '%s\n' "$*" >> '` + logPath + `'
name=$(basename "$1"); name="${name%.*}"
case "$*" in *peakPicking*) acc='MS:1000127';; *) acc='MS:1000128';; esac
printf '<mzML><cvParam accession="%s"/></mzML>\n' "$acc" > "$3/$name.mzML"
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe, logPath
}

func msconvertCalls(t *testing.T, logPath string) []string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func waitItemIdle(t *testing.T, e *env, jobID, itemID uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		d, err := e.svc.GetItemDetail(jobID, itemID)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Item.IsBusy() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("item did not finish in time")
}

// Параметры конвертации из задачи и из пересчёта спектра доходят до msconvert,
// а кэш .mzML различает наборы фильтров.
func TestConvertParamsReachMsConvert(t *testing.T) {
	tools := t.TempDir()
	exe, logPath := fakeMsConvert(t, tools)
	e := setupWith(t, 0, func(c *config.Config) { c.MsConvert = exe })

	src := filepath.Join(e.dir, "in", "LST1.d")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	var params model.JobParams
	if err := json.Unmarshal([]byte(`{"protocol": "non_tmds", "rel_error": 1,
		"convert": {"peak_picking": "cwt", "cwt_snr": 3, "threshold_type": "absolute", "threshold": 2e7}}`), &params); err != nil {
		t.Fatal(err)
	}
	job, err := e.svc.CreateJobFromItems([]model.JobItem{{SpectraPath: src}}, params)
	if err != nil {
		t.Fatal(err)
	}
	if final := waitTerminal(t, e.repo, job.ID); final.Status != model.JobDone {
		t.Fatalf("job status = %s", final.Status)
	}

	calls := msconvertCalls(t, logPath)
	if len(calls) != 1 ||
		!strings.Contains(calls[0], "peakPicking cwt snr=3 peakSpace=0.001 msLevel=1-") ||
		!strings.Contains(calls[0], "threshold absolute 2e+07 most-intense 1-") {
		t.Fatalf("msconvert calls = %q", calls)
	}
	first := e.fake.lastFor("LST1.d").SpectraPath
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("converted file missing: %v", err)
	}

	items, _ := e.repo.GetJobItems(job.ID)
	it := items[0]

	// Пересчёт с другим алгоритмом — новая конвертация и другой файл кэша
	if err := e.svc.ReprocessItem(job.ID, it.ID, json.RawMessage(`{"convert": {"peak_picking": "vendor"}}`), false); err != nil {
		t.Fatal(err)
	}
	waitItemIdle(t, e, job.ID, it.ID)
	calls = msconvertCalls(t, logPath)
	if len(calls) != 2 || !strings.Contains(calls[1], "peakPicking vendor msLevel=1-") || strings.Contains(calls[1], "threshold") {
		t.Fatalf("msconvert calls after reprocess = %q", calls)
	}
	second := e.fake.lastFor("LST1.d").SpectraPath
	if second == first {
		t.Fatal("different convert params must use a different cached .mzML")
	}

	// Те же параметры конвертации, другая ошибка — msconvert не запускается
	if err := e.svc.ReprocessItem(job.ID, it.ID, json.RawMessage(`{"rel_error": 2}`), false); err != nil {
		t.Fatal(err)
	}
	waitItemIdle(t, e, job.ID, it.ID)
	if calls = msconvertCalls(t, logPath); len(calls) != 2 {
		t.Fatalf("cached .mzML must be reused, calls = %q", calls)
	}
	if r := e.fake.lastFor("LST1.d"); r.SpectraPath != second || r.RelError != 2 {
		t.Fatalf("reuse: path=%s rel=%v", r.SpectraPath, r.RelError)
	}

	// Профиль без центроидирования допустим (метка profile в файле)
	if err := e.svc.ReprocessItem(job.ID, it.ID, json.RawMessage(`{"convert": {"peak_picking": "none"}}`), false); err != nil {
		t.Fatal(err)
	}
	waitItemIdle(t, e, job.ID, it.ID)
	d, _ := e.svc.GetItemDetail(job.ID, it.ID)
	if d.Item.Status != model.ItemDone || d.EffectiveParams.Convert.PeakPicking != "none" {
		t.Fatalf("profile run: status=%s convert=%+v", d.Item.Status, d.EffectiveParams.Convert)
	}
	if calls = msconvertCalls(t, logPath); !strings.Contains(calls[len(calls)-1], "zeroSamples removeExtra") {
		t.Fatalf("profile filters = %q", calls[len(calls)-1])
	}
	// В истории прогонов видно, с какой конвертацией сделан каждый
	if len(d.Runs) != 4 || d.Runs[3].Params.Convert.PeakPicking != "cwt" || d.Runs[2].Params.Convert.PeakPicking != "vendor" {
		t.Fatalf("runs keep convert params: %+v", d.Runs)
	}
}
