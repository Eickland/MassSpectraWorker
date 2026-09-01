package model

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
    "time"
    "github.com/google/uuid"
)

// JobParams - параметры задачи
type JobParams struct {
    Percentile int     `json:"percentile"`
    RelError   float64 `json:"rel_error"`
    Protocol   string  `json:"protocol"`
    BruttoDict string  `json:"brutto_dict"`
    Width      int     `json:"width"`
    Height     int     `json:"height"`
    DPI        int     `json:"dpi"`
    Format     string  `json:"format"`
}

func (p JobParams) Validate() error {
    if p.Percentile < 0 || p.Percentile > 100 {
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

// Job - основная задача
type Job struct {
    ID          uuid.UUID  `db:"id" json:"id"`
    Status      string     `db:"status" json:"status"` // pending | running | done | failed | cancelled
    SourceFolder *string   `db:"source_folder" json:"source_folder,omitempty"`
    Params      JobParams  `db:"params_json" json:"params"`
    TotalItems  int        `db:"total_items" json:"total_items"`
    DoneItems   int        `db:"done_items" json:"done_items"`
    FailedItems int        `db:"failed_items" json:"failed_items"`
    CreatedAt   time.Time  `db:"created_at" json:"created_at"`
    FinishedAt  *time.Time `db:"finished_at" json:"finished_at,omitempty"`
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
    StartedAt   *time.Time `db:"started_at" json:"started_at,omitempty"`
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