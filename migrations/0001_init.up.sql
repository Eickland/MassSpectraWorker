-- SQLite: UUID хранятся как TEXT, JSON как TEXT, время как DATETIME (ISO-8601).
CREATE TABLE batch_jobs (
  id            TEXT PRIMARY KEY,
  status        TEXT NOT NULL DEFAULT 'pending', -- pending | running | cancelling | cancelled | done | failed
  source_folder TEXT,
  params_json   TEXT NOT NULL,                   -- JSON с параметрами обработки (model.JobParams)
  total_items   INTEGER NOT NULL DEFAULT 0,
  done_items    INTEGER NOT NULL DEFAULT 0,
  failed_items  INTEGER NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL,
  finished_at   DATETIME
);

CREATE TABLE batch_job_items (
  id            TEXT PRIMARY KEY,
  job_id        TEXT NOT NULL REFERENCES batch_jobs(id) ON DELETE CASCADE,
  spectra_name  TEXT NOT NULL,
  spectra_path  TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending', -- pending | processing | done | failed | cancelled
  result_path   TEXT,                            -- путь к картинке
  error         TEXT,
  file_hash     TEXT,
  created_at    DATETIME NOT NULL,
  started_at    DATETIME,
  finished_at   DATETIME
);

CREATE INDEX idx_batch_job_items_job_id ON batch_job_items(job_id, status);
CREATE INDEX idx_batch_jobs_status ON batch_jobs(status);
