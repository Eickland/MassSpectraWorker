-- Индивидуальные параметры спектра и история прогонов обработки.

-- Параметры конкретного спектра (JSON model.JobParams). NULL = параметры задачи.
ALTER TABLE batch_job_items ADD COLUMN params_json TEXT;
-- Прогон, результаты которого сейчас считаются результатом спектра (идут в ZIP).
ALTER TABLE batch_job_items ADD COLUMN current_run_id TEXT;

-- Каждая обработка спектра = отдельный прогон со своими параметрами и файлами.
CREATE TABLE item_runs (
  id            TEXT PRIMARY KEY,
  item_id       TEXT NOT NULL REFERENCES batch_job_items(id) ON DELETE CASCADE,
  run_no        INTEGER NOT NULL,              -- 1, 2, 3... в пределах спектра
  params_json   TEXT NOT NULL,                 -- снимок параметров, с которыми считали
  status        TEXT NOT NULL,                 -- processing | done | failed
  result_path   TEXT,
  spectrum_path TEXT,
  spectrum_rows INTEGER,
  spectrum_cols INTEGER,
  spectrum_size INTEGER,
  error         TEXT,
  created_at    DATETIME NOT NULL,
  finished_at   DATETIME,
  UNIQUE (item_id, run_no)
);
CREATE INDEX idx_item_runs_item_id ON item_runs(item_id);

-- Уже обработанные спектры переносим в историю как прогон №1
INSERT INTO item_runs (
  id, item_id, run_no, params_json, status,
  result_path, spectrum_path, spectrum_rows, spectrum_cols, spectrum_size,
  error, created_at, finished_at
)
SELECT
  lower(
    hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(2)) || '-' ||
    hex(randomblob(2)) || '-' || hex(randomblob(6))
  ),
  i.id, 1, j.params_json, i.status,
  i.result_path, i.spectrum_path, i.spectrum_rows, i.spectrum_cols, i.spectrum_size,
  i.error, COALESCE(i.started_at, i.created_at), i.finished_at
FROM batch_job_items i
JOIN batch_jobs j ON j.id = i.job_id
WHERE i.status IN ('done', 'failed');

UPDATE batch_job_items
SET current_run_id = (SELECT r.id FROM item_runs r WHERE r.item_id = batch_job_items.id AND r.run_no = 1)
WHERE status = 'done';
