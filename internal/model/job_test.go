package model

import (
	"encoding/json"
	"testing"
)

func TestJobParamsAcceptsFormPayloads(t *testing.T) {
	cases := map[string]string{
		// старый формат пакетной формы: format вместо output_format, brutto строкой
		"legacy": `{"rel_error":0.5,"protocol":"non_tmds","format":"pdf",
			"brutto_dict":"{\"C\": [4, 50], \"S\": [0, 3]}"}`,
		// новый формат
		"arrays":  `{"output_format":"pdf","brutto_dict":{"C":[4,50],"S":[0,3]}}`,
		"objects": `{"output_format":"pdf","brutto_dict":{"C":{"min":4,"max":50},"S":{"min":0,"max":3}}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			var p JobParams
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				t.Fatal(err)
			}
			p.ApplyDefaults()
			if err := p.Validate(); err != nil {
				t.Fatal(err)
			}
			if p.OutputFormat != "pdf" {
				t.Fatalf("format = %q", p.OutputFormat)
			}
			if p.BruttoDict["C"] != (BruttoRange{4, 50}) || p.BruttoDict["S"] != (BruttoRange{0, 3}) {
				t.Fatalf("brutto = %+v", p.BruttoDict)
			}
		})
	}
}

func TestJobParamsRoundTripThroughDB(t *testing.T) {
	in := JobParams{RelError: 2, ChargeMax: 2, Protocol: "tmds",
		BruttoDict: BruttoDict{"C": {1, 9}}, Options: map[string]string{"recalibrate": "false"}}
	in.ApplyDefaults()
	v, err := in.Value()
	if err != nil {
		t.Fatal(err)
	}
	var out JobParams
	if err := out.Scan(v); err != nil { // SQLite возвращает TEXT как string
		t.Fatal(err)
	}
	if err := out.Scan([]byte(v.(string))); err != nil {
		t.Fatal(err)
	}
	if out.Protocol != "tmds" || out.BruttoDict["C"] != (BruttoRange{1, 9}) || out.Options["recalibrate"] != "false" {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestJobParamsValidation(t *testing.T) {
	bad := []JobParams{
		{LowPercentile: 99.9, HighPercentile: 99.5},
		{RelError: -1},
		{Protocol: "unknown"},
		{BruttoDict: BruttoDict{"C": {10, 2}}},
		{OutputFormat: "gif"},
		{ChargeMax: 50},
	}
	for i, p := range bad {
		p.ApplyDefaults()
		if err := p.Validate(); err == nil {
			t.Errorf("case %d: expected validation error for %+v", i, p)
		}
	}
	// rel_error в ppm: значения > 1 допустимы
	ok := JobParams{RelError: 3}
	ok.ApplyDefaults()
	if err := ok.Validate(); err != nil {
		t.Fatalf("rel_error=3 ppm rejected: %v", err)
	}
}

func TestMergeParams(t *testing.T) {
	base := JobParams{RelError: 0.5, ChargeMax: 2, Protocol: "non_tmds",
		BruttoDict: BruttoDict{"C": {4, 50}, "O": {0, 30}}, Options: map[string]string{"recalibrate": "false"}}
	base.ApplyDefaults()

	// Частичное изменение: остальное берётся из base
	got, err := MergeParams(base, json.RawMessage(`{"rel_error": 1.2, "format": "svg"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.RelError != 1.2 || got.ChargeMax != 2 || got.OutputFormat != "svg" ||
		got.BruttoDict["O"] != (BruttoRange{0, 30}) || got.Options["recalibrate"] != "false" {
		t.Fatalf("partial merge = %+v", got)
	}

	// Заданный словарь заменяется целиком, null — сбрасывает к умолчанию Python
	got, _ = MergeParams(base, json.RawMessage(`{"brutto_dict": {"C": [1, 9]}}`))
	if len(got.BruttoDict) != 1 || got.BruttoDict["C"] != (BruttoRange{1, 9}) {
		t.Fatalf("brutto replace = %+v", got.BruttoDict)
	}
	got, _ = MergeParams(base, json.RawMessage(`{"brutto_dict": null}`))
	if got.BruttoDict != nil {
		t.Fatalf("brutto null = %+v", got.BruttoDict)
	}

	// Пустое переопределение = base
	got, err = MergeParams(base, nil)
	if err != nil || got.RelError != 0.5 {
		t.Fatalf("empty override = %+v, %v", got, err)
	}

	for _, bad := range []string{`[1,2]`, `{"rel_error": "x"}`, `{"protocol": "nope"}`} {
		if _, err := MergeParams(base, json.RawMessage(bad)); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}
