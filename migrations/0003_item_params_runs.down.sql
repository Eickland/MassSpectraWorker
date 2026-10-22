DROP TABLE IF EXISTS item_runs;
ALTER TABLE batch_job_items DROP COLUMN current_run_id;
ALTER TABLE batch_job_items DROP COLUMN params_json;
