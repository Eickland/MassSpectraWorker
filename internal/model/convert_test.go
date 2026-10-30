package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestConvertDefaultsKeepVendorBehaviour(t *testing.T) {
	var p JobParams // старая задача: в params_json нет ключа "convert"
	if err := json.Unmarshal([]byte(`{"rel_error": 1}`), &p); err != nil {
		t.Fatal(err)
	}
	p.ApplyDefaults()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{"--mzML", "--filter", "peakPicking vendor msLevel=1-", "--verbose"}
	if got := p.Convert.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestConvertFiltersOrderAndSyntax(t *testing.T) {
	c := ConvertParams{
		PeakPicking: "CWT", CwtSNR: 3, CwtPeakSpace: 0.001,
		ThresholdType: "absolute", Threshold: 2e7,
		MzMin: 150, MzMax: 1000,
		ExtraFilters: []string{"  ", "scanTime [0,600]"},
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"peakPicking cwt snr=3 peakSpace=0.001 msLevel=1-", // центроидирование первым
		"mzWindow [150,1000]",
		"threshold absolute 2e+07 most-intense 1-",
		"scanTime [0,600]",
	}
	if got := c.Filters(); !reflect.DeepEqual(got, want) {
		t.Fatalf("filters = %q\nwant %q", got, want)
	}
}

func TestConvertProfileRemovesZeroSamples(t *testing.T) {
	c := ConvertParams{PeakPicking: "none"}
	c.ApplyDefaults()
	if got := c.Filters(); !reflect.DeepEqual(got, []string{"zeroSamples removeExtra"}) {
		t.Fatalf("filters = %q", got)
	}
	if c.Centroided() {
		t.Fatal("profile must not be expected to be centroided")
	}
	c.KeepZeroSamples = true
	if got := c.Filters(); len(got) != 0 {
		t.Fatalf("filters = %q, want none", got)
	}
}

func TestConvertTagDependsOnFilters(t *testing.T) {
	a := ConvertParams{PeakPicking: "vendor"}
	b := ConvertParams{PeakPicking: "cwt"}
	a.ApplyDefaults()
	b.ApplyDefaults()
	if a.Tag() == b.Tag() {
		t.Fatal("different filters must give different cache tags")
	}
	a2 := ConvertParams{PeakPicking: "vendor", CwtSNR: 5} // cwt_snr не влияет на vendor
	a2.ApplyDefaults()
	if a.Tag() != a2.Tag() {
		t.Fatal("unused fields must not change the tag")
	}
}

func TestConvertValidate(t *testing.T) {
	bad := map[string]ConvertParams{
		"algorithm":       {PeakPicking: "magic"},
		"ms levels":       {MsLevels: "1;rm"},
		"snr":             {PeakPicking: "cwt", CwtSNR: -1},
		"threshold type":  {ThresholdType: "percent", Threshold: 1},
		"threshold value": {ThresholdType: "absolute"},
		"relative > 1":    {ThresholdType: "bpi-relative", Threshold: 2},
		"count fraction":  {ThresholdType: "count", Threshold: 2.5},
		"mz one side":     {MzMin: 100},
		"mz reversed":     {MzMin: 900, MzMax: 100},
		"extra flag":      {ExtraFilters: []string{"--64"}},
		"extra picking":   {ExtraFilters: []string{"peakPicking true 1-"}},
	}
	for name, c := range bad {
		c.ApplyDefaults()
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error for %+v", name, c)
		}
	}
	ok := ConvertParams{ThresholdType: "bpi-relative", Threshold: 0.002, MsLevels: "1-2"}
	ok.ApplyDefaults()
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMergeParamsReplacesConvert(t *testing.T) {
	base := JobParams{Convert: ConvertParams{PeakPicking: "cwt", CwtSNR: 3}}
	base.ApplyDefaults()
	got, err := MergeParams(base, json.RawMessage(`{"convert": {"peak_picking": "vendor"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Convert.PeakPicking != "vendor" {
		t.Fatalf("peak_picking = %q", got.Convert.PeakPicking)
	}
	// без ключа convert — параметры конвертации остаются прежними
	got, err = MergeParams(base, json.RawMessage(`{"rel_error": 2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Convert.PeakPicking != "cwt" || got.Convert.CwtSNR != 3 {
		t.Fatalf("convert lost: %+v", got.Convert)
	}
	if _, err := MergeParams(base, json.RawMessage(`{"convert": {"peak_picking": "x"}}`)); err == nil {
		t.Fatal("invalid convert must be rejected")
	}
}

func TestSetDefaultConvert(t *testing.T) {
	saved := DefaultConvert()
	t.Cleanup(func() { defaultConvert = saved })

	if err := SetDefaultConvert(ConvertParams{PeakPicking: "bogus"}); err == nil {
		t.Fatal("invalid default must be rejected")
	}
	if err := SetDefaultConvert(ConvertParams{PeakPicking: "CWT", CwtSNR: 3}); err != nil {
		t.Fatal(err)
	}
	var c ConvertParams
	c.ApplyDefaults()
	if c.PeakPicking != "cwt" || c.CwtSNR != 3 || c.CwtPeakSpace != 0.001 {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestCommandLineQuotesFilters(t *testing.T) {
	c := ConvertParams{}
	c.ApplyDefaults()
	got := CommandLine("/home/u/bin/msconvert", "<спектр>.d", "/tmp/out", c)
	if !strings.Contains(got, `--filter "peakPicking vendor msLevel=1-"`) {
		t.Fatalf("command = %s", got)
	}
}
