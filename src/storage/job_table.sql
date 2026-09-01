CREATE TABLE batch_jobs (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  status        TEXT NOT NULL DEFAULT 'pending', -- pending | running | done | failed | cancelled
  source_folder TEXT,
  params_json   JSONB NOT NULL,   -- общие параметры формы: percentile, rel_error, protocol, brutto_dict, width/height/dpi, format
  total_items   INT NOT NULL DEFAULT 0,
  done_items    INT NOT NULL DEFAULT 0,
  failed_items  INT NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at   TIMESTAMPTZ
);

CREATE TABLE batch_job_items (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id        UUID NOT NULL REFERENCES batch_jobs(id) ON DELETE CASCADE,
  spectra_name  TEXT NOT NULL,
  spectra_path  TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending', -- pending | processing | done | failed
  result_path   TEXT,          -- куда записана картинка
  error         TEXT,
  file_hash     TEXT,          -- см. раздел про кэш
  started_at    TIMESTAMPTZ,
  finished_at   TIMESTAMPTZ
);

CREATE INDEX idx_batch_job_items_job_id ON batch_job_items(job_id);