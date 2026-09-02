package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Job - основная задача
type Job struct {
	ID     uuid.UUID `db:"id" json:"id"`
	Status string    `db:"status" json:"status"`

	// 📌 Все параметры в одном JSON-поле
	Params JobParams `db:"params_json" json:"params"` // уже есть в вашем коде!

	SourceFolder *string    `db:"source_folder" json:"source_folder,omitempty"`
	TotalItems   int        `db:"total_items" json:"total_items"`
	DoneItems    int        `db:"done_items" json:"done_items"`
	FailedItems  int        `db:"failed_items" json:"failed_items"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	FinishedAt   *time.Time `db:"finished_at" json:"finished_at,omitempty"`
}

// JobParams - параметры обработки спектров
type JobParams struct {
	LowPercentile  float32 `json:"low_percentile"`
	HighPercentile float32 `json:"high_percentile"`
	Dpi            int32   `json:"dpi"`
	Width          int32   `json:"width"`
	Height         int32   `json:"height"`
	OutputFormat   string  `json:"output_format"` // png, svg, pdf, jpeg
	RelError       float32 `json:"rel_error"`
	Protocol       string  `json:"protocol"`

	// Дополнительные параметры, которые могут понадобиться
	Normalize          bool    `json:"normalize,omitempty"`
	BaselineCorrection bool    `json:"baseline_correction,omitempty"`
	PeakThreshold      float64 `json:"peak_threshold,omitempty"`
}

// JobItem - элемент задачи (один спектр)
type JobItem struct {
	ID          uuid.UUID  `db:"id" json:"id"`
	JobID       uuid.UUID  `db:"job_id" json:"job_id"`
	SpectraName string     `db:"spectra_name" json:"spectra_name"`
	SpectraPath string     `db:"spectra_path" json:"spectra_path"`
	Status      string     `db:"status" json:"status"` // pending | processing | done | failed
	ResultPath  *string    `db:"result_path" json:"result_path,omitempty"`
	Error       *string    `db:"error" json:"error,omitempty"`
	FileHash    *string    `db:"file_hash" json:"file_hash,omitempty"`
	StartedAt   *time.Time `db:"created_at" json:"created_at,omitempty"`
	FinishedAt  *time.Time `db:"finished_at" json:"finished_at,omitempty"`
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

func (p JobParams) Validate() error {
	if p.LowPercentile < 0 || p.LowPercentile > 100 {
		return fmt.Errorf("percentile must be 0-100")
	}
	if p.RelError < 0 || p.RelError > 1 {
		return fmt.Errorf("rel_error must be 0-1")
	}
	if p.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}
	if p.Width <= 0 || p.Height <= 0 {
		return fmt.Errorf("width and height must be positive")
	}
	return nil
}

func (p JobParams) Value() (driver.Value, error) {
	return json.Marshal(p)
}

func (p *JobParams) Scan(value interface{}) error {
	if value == nil {
		*p = JobParams{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("expected []byte, got %T", value)
	}
	return json.Unmarshal(bytes, p)
}
