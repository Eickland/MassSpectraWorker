package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	pb "MassSpectraWorker/src/protobuf"
)

// Статусы задачи
const (
	JobPending    = "pending"
	JobRunning    = "running"
	JobCancelling = "cancelling"
	JobCancelled  = "cancelled"
	JobDone       = "done"
	JobFailed     = "failed" // завершена, но есть ошибки (или все элементы упали)
)

// Статусы элемента
const (
	ItemPending    = "pending"
	ItemProcessing = "processing"
	ItemDone       = "done"
	ItemFailed     = "failed"
	ItemCancelled  = "cancelled"
)

// Протоколы обработки, которые понимает Python-сервис
var KnownProtocols = map[string]bool{
	"non_tmds": true,
	"tmds":     true,
	"custom":   true,
}

var KnownFormats = map[string]bool{
	"png":  true,
	"svg":  true,
	"pdf":  true,
	"jpeg": true,
	"jpg":  true,
}

// Job - основная задача
type Job struct {
	ID     uuid.UUID `db:"id" json:"id"`
	Status string    `db:"status" json:"status"`

	// Все параметры обработки в одном JSON-поле
	Params JobParams `db:"params_json" json:"params"`

	SourceFolder *string    `db:"source_folder" json:"source_folder,omitempty"`
	TotalItems   int        `db:"total_items" json:"total_items"`
	DoneItems    int        `db:"done_items" json:"done_items"`
	FailedItems  int        `db:"failed_items" json:"failed_items"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	FinishedAt   *time.Time `db:"finished_at" json:"finished_at,omitempty"`
}

// IsTerminal - задача завершена (успешно, с ошибками или отменена)
func (j *Job) IsTerminal() bool {
	return j.Status == JobDone || j.Status == JobFailed || j.Status == JobCancelled
}

// BruttoRange - диапазон числа атомов элемента
type BruttoRange struct {
	Min int32 `json:"min"`
	Max int32 `json:"max"`
}

// UnmarshalJSON принимает и {"min": 4, "max": 50}, и [4, 50]
func (r *BruttoRange) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		var arr []int32
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		if len(arr) != 2 {
			return fmt.Errorf("brutto range must be [min, max], got %d values", len(arr))
		}
		r.Min, r.Max = arr[0], arr[1]
		return nil
	}
	type plain BruttoRange
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*r = BruttoRange(p)
	return nil
}

// BruttoDict - диапазоны элементов для приписывания формул,
// например {"C": [4, 50], "H": [4, 80], "O": [0, 50]}
type BruttoDict map[string]BruttoRange

// UnmarshalJSON дополнительно принимает JSON, упакованный в строку
// (старый формат формы пакетной обработки).
func (d *BruttoDict) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if len(bytes.TrimSpace([]byte(s))) == 0 {
			*d = nil
			return nil
		}
		data = []byte(s)
	}
	if string(data) == "null" {
		*d = nil
		return nil
	}
	m := map[string]BruttoRange{}
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("brutto_dict: %w", err)
	}
	*d = m
	return nil
}

func (d BruttoDict) Validate() error {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := d[k]
		if k == "" {
			return fmt.Errorf("brutto_dict: empty element name")
		}
		if r.Min > r.Max {
			return fmt.Errorf("brutto_dict: %s min (%d) > max (%d)", k, r.Min, r.Max)
		}
	}
	return nil
}

// ToProto - конвертация в map для gRPC-запроса
func (d BruttoDict) ToProto() map[string]*pb.BruttoRange {
	if len(d) == 0 {
		return nil
	}
	out := make(map[string]*pb.BruttoRange, len(d))
	for k, r := range d {
		out[k] = &pb.BruttoRange{Min: r.Min, Max: r.Max}
	}
	return out
}

// JobParams - параметры обработки спектров
type JobParams struct {
	LowPercentile  float32    `json:"low_percentile"`
	HighPercentile float32    `json:"high_percentile"`
	RelError       float32    `json:"rel_error"` // ppm
	ChargeMax      int32      `json:"charge_max"`
	Protocol       string     `json:"protocol"` // non_tmds | tmds | custom
	BruttoDict     BruttoDict `json:"brutto_dict,omitempty"`

	// Дополнительные параметры обработки (передаются в Python как есть):
	// sulfur_precision_factor, nitrogen_precision_factor, recalibrate,
	// tmds_rel_error, tmds_max_num, tmds_brutto_dict
	Options map[string]string `json:"options,omitempty"`

	// Параметры графика
	Dpi          int32  `json:"dpi"`
	Width        int32  `json:"width"`
	Height       int32  `json:"height"`
	OutputFormat string `json:"output_format"` // png, svg, pdf, jpeg
}

// UnmarshalJSON принимает и "output_format", и "format" (так поле называется в форме)
func (p *JobParams) UnmarshalJSON(data []byte) error {
	type plain JobParams
	aux := struct {
		*plain
		Format string `json:"format"`
	}{plain: (*plain)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if p.OutputFormat == "" && aux.Format != "" {
		p.OutputFormat = aux.Format
	}
	return nil
}

// ApplyDefaults - заполняет незаданные поля значениями по умолчанию
func (p *JobParams) ApplyDefaults() {
	if p.RelError == 0 {
		p.RelError = 0.5
	}
	if p.ChargeMax == 0 {
		p.ChargeMax = 1
	}
	if p.Protocol == "" {
		p.Protocol = "non_tmds"
	}
	if p.Dpi == 0 {
		p.Dpi = 100
	}
	if p.Width == 0 {
		p.Width = 10
	}
	if p.Height == 0 {
		p.Height = 6
	}
	if p.OutputFormat == "" {
		p.OutputFormat = "png"
	}
	if p.OutputFormat == "jpg" {
		p.OutputFormat = "jpeg"
	}
}

func (p JobParams) Validate() error {
	if p.LowPercentile < 0 || p.LowPercentile > 100 {
		return fmt.Errorf("low_percentile must be 0-100")
	}
	if p.HighPercentile < 0 || p.HighPercentile > 100 {
		return fmt.Errorf("high_percentile must be 0-100")
	}
	if p.HighPercentile > 0 && p.LowPercentile >= p.HighPercentile {
		return fmt.Errorf("low_percentile must be less than high_percentile")
	}
	if p.RelError <= 0 || p.RelError > 100 {
		return fmt.Errorf("rel_error must be in (0, 100] ppm")
	}
	if p.ChargeMax < 1 || p.ChargeMax > 10 {
		return fmt.Errorf("charge_max must be 1-10")
	}
	if !KnownProtocols[p.Protocol] {
		return fmt.Errorf("unknown protocol %q", p.Protocol)
	}
	if err := p.BruttoDict.Validate(); err != nil {
		return err
	}
	if p.Width <= 0 || p.Height <= 0 {
		return fmt.Errorf("width and height must be positive")
	}
	if p.Dpi < 10 || p.Dpi > 1200 {
		return fmt.Errorf("dpi must be 10-1200")
	}
	if !KnownFormats[p.OutputFormat] {
		return fmt.Errorf("unknown output format %q", p.OutputFormat)
	}
	return nil
}

// Value - сериализация в TEXT-колонку params_json
func (p JobParams) Value() (driver.Value, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan - SQLite может вернуть и string, и []byte
func (p *JobParams) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		*p = JobParams{}
		return nil
	case []byte:
		return json.Unmarshal(v, p)
	case string:
		return json.Unmarshal([]byte(v), p)
	default:
		return fmt.Errorf("params_json: unexpected type %T", value)
	}
}

// JobItem - элемент задачи (один спектр)
type JobItem struct {
	ID           uuid.UUID  `db:"id" json:"id"`
	JobID        uuid.UUID  `db:"job_id" json:"job_id"`
	SpectraName  string     `db:"spectra_name" json:"spectra_name"`
	SpectraPath  string     `db:"spectra_path" json:"spectra_path"`
	Status       string     `db:"status" json:"status"` // pending | processing | done | failed | cancelled
	ResultPath   *string    `db:"result_path" json:"result_path,omitempty"`
	Error        *string    `db:"error" json:"error,omitempty"`
	FileHash     *string    `db:"file_hash" json:"file_hash,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	StartedAt    *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt   *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	SpectrumPath *string    `db:"spectrum_path" json:"spectrum_path"` // путь к таблице
	SpectrumRows *int       `db:"spectrum_rows" json:"spectrum_rows"`
	SpectrumCols *int       `db:"spectrum_cols" json:"spectrum_cols"`
	SpectrumSize *int64     `db:"spectrum_size" json:"spectrum_size"`

	// Вычисляется в хендлере, в БД не хранится
	ResultURL string `db:"-" json:"result_url,omitempty"`
}

// JobProgress - прогресс задачи (для SSE)
type JobProgress struct {
	JobID       uuid.UUID `json:"job_id"`
	Status      string    `json:"status"`
	TotalItems  int       `json:"total_items"`
	DoneItems   int       `json:"done_items"`
	FailedItems int       `json:"failed_items"`
	Progress    float64   `json:"progress"` // 0-100
	Elapsed     string    `json:"elapsed"`  // human-readable
}

// ItemCounts - текущее распределение элементов задачи по статусам
type ItemCounts struct {
	Pending    int `db:"pending"`
	Processing int `db:"processing"`
	Done       int `db:"done"`
	Failed     int `db:"failed"`
	Cancelled  int `db:"cancelled"`
}
