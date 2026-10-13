ALTER TABLE batch_job_items
  ADD COLUMN spectrum_path TEXT,
  ADD COLUMN spectrum_rows INT,
  ADD COLUMN spectrum_cols INT,
  ADD COLUMN spectrum_size BIGINT;