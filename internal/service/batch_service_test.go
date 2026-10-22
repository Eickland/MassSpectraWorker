package service

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestScanFolder(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	mk("a.mzML")
	mk("b.mzXML")
	mk("thermo.raw")
	mk("notes.csv")
	mk("bruker.d/analysis.tdf") // папка-спектр
	mk("bruker.d/inner/x.mzML") // внутрь .d заходить нельзя
	mk("nested/c.mzml")

	got, err := scanFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i], _ = filepath.Rel(dir, got[i])
		got[i] = filepath.ToSlash(got[i])
	}
	sort.Strings(got)
	want := []string{"a.mzML", "b.mzXML", "bruker.d", "nested/c.mzml", "thermo.raw"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	// Выбрана сама папка .d
	one, err := scanFolder(filepath.Join(dir, "bruker.d"))
	if err != nil || len(one) != 1 {
		t.Fatalf("scan .d itself = %v, %v", one, err)
	}
}
