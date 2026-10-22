package repository

import (
	"MassSpectraWorker/internal/model"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// ErrNotFound - задача, спектр или прогон не найдены
var ErrNotFound = errors.New("not found")

// ErrItemBusy - спектр уже в очереди или обрабатывается
var ErrItemBusy = errors.New("spectrum is queued or being processed")

// ErrJobBusy - задача отменяется, менять её спектры сейчас нельзя
var ErrJobBusy = errors.New("job is being cancelled")

// ErrRunNotUsable - выбрать можно только успешный прогон
var ErrRunNotUsable = errors.New("only a successful run can be selected")

const jobColumns = `id, status, source_folder, params_json, total_items,
	done_items, failed_items, created_at, finished_at`

const itemColumns = `id, job_id, spectra_name, spectra_path, status, result_path,
	error, file_hash, created_at, started_at, finished_at,
	spectrum_path, spectrum_rows, spectrum_cols, spectrum_size,
	params_json, current_run_id`

const runColumns = `id, item_id, run_no, params_json, status, result_path,
	spectrum_path, spectrum_rows, spectrum_cols, spectrum_size,
	error, created_at, finished_at`

type JobRepository struct {
	db *sqlx.DB
}

func NewJobRepository(db *sqlx.DB) *JobRepository {
	return &JobRepository{db: db}
}

func now() time.Time { return time.Now().UTC() }

// CreateJobWithItems - создаёт задачу и её элементы в одной транзакции
func (r *JobRepository) CreateJobWithItems(job *model.Job, items []model.JobItem) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if job.CreatedAt.IsZero() {
		job.CreatedAt = now()
	}
	_, err = tx.Exec(`
		INSERT INTO batch_jobs (
			id, status, source_folder, params_json,
			total_items, done_items, failed_items, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.Status, job.SourceFolder, job.Params,
		job.TotalItems, job.DoneItems, job.FailedItems, job.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}

	stmt, err := tx.Preparex(`
		INSERT INTO batch_job_items (
			id, job_id, spectra_name, spectra_path, status, created_at, params_json
		) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Небольшой сдвиг времени сохраняет порядок элементов при ORDER BY created_at
	base := now()
	for i := range items {
		items[i].CreatedAt = base.Add(time.Duration(i) * time.Microsecond)
		if _, err := stmt.Exec(
			items[i].ID, items[i].JobID, items[i].SpectraName, items[i].SpectraPath,
			items[i].Status, items[i].CreatedAt, items[i].Params,
		); err != nil {
			return fmt.Errorf("insert item %s: %w", items[i].SpectraName, err)
		}
	}
	return tx.Commit()
}

// GetJobByID - получение задачи по ID
func (r *JobRepository) GetJobByID(id uuid.UUID) (*model.Job, error) {
	var job model.Job
	err := r.db.Get(&job, "SELECT "+jobColumns+" FROM batch_jobs WHERE id = ?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// GetJobItems - получение элементов задачи
func (r *JobRepository) GetJobItems(jobID uuid.UUID) ([]model.JobItem, error) {
	items := []model.JobItem{}
	err := r.db.Select(&items,
		"SELECT "+itemColumns+" FROM batch_job_items WHERE job_id = ? ORDER BY created_at, rowid",
		jobID,
	)
	return items, err
}

// ClaimNextItem - атомарно забирает следующий pending-элемент и переводит его
// в processing. Возвращает (nil, nil), если элементов не осталось.
// Заменяет FOR UPDATE SKIP LOCKED из Postgres: UPDATE ... RETURNING в SQLite
// выполняется одной операцией под блокировкой записи.
func (r *JobRepository) ClaimNextItem(jobID uuid.UUID) (*model.JobItem, error) {
	var item model.JobItem
	err := r.db.Get(&item, `
		UPDATE batch_job_items
		SET status = ?, started_at = ?, error = NULL
		WHERE id = (
			SELECT id FROM batch_job_items
			WHERE job_id = ? AND status = ?
			ORDER BY created_at, rowid
			LIMIT 1
		)
		RETURNING `+itemColumns,
		model.ItemProcessing, now(), jobID, model.ItemPending,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ResetItemToPending - возвращает элемент в очередь (например, при остановке сервера)
func (r *JobRepository) ResetItemToPending(itemID uuid.UUID) error {
	_, err := r.db.Exec(`
		UPDATE batch_job_items
		SET status = ?, started_at = NULL
		WHERE id = ? AND status = ?`,
		model.ItemPending, itemID, model.ItemProcessing,
	)
	return err
}

// CancelPendingItems - помечает ещё не начатые элементы как отменённые
func (r *JobRepository) CancelPendingItems(jobID uuid.UUID) error {
	_, err := r.db.Exec(`
		UPDATE batch_job_items
		SET status = ?, finished_at = ?
		WHERE job_id = ? AND status = ?`,
		model.ItemCancelled, now(), jobID, model.ItemPending,
	)
	return err
}

// RecountJob - пересчитывает счётчики задачи по фактическим статусам элементов.
// Счётчики всегда берутся из БД, поэтому корректны и после перезапуска,
// и при обработке одной задачи несколькими воркерами.
func (r *JobRepository) RecountJob(jobID uuid.UUID) (model.ItemCounts, error) {
	var c model.ItemCounts
	err := r.db.Get(&c, `
		SELECT
			COALESCE(SUM(status = 'pending'), 0)    AS pending,
			COALESCE(SUM(status = 'processing'), 0) AS processing,
			COALESCE(SUM(status = 'done'), 0)       AS done,
			COALESCE(SUM(status = 'failed'), 0)     AS failed,
			COALESCE(SUM(status = 'cancelled'), 0)  AS cancelled
		FROM batch_job_items
		WHERE job_id = ?`, jobID)
	if err != nil {
		return c, err
	}
	_, err = r.db.Exec(`
		UPDATE batch_jobs SET done_items = ?, failed_items = ? WHERE id = ?`,
		c.Done, c.Failed, jobID)
	return c, err
}

// UpdateJobStatus - обновление статуса задачи
func (r *JobRepository) UpdateJobStatus(jobID uuid.UUID, status string) error {
	var finished *time.Time
	switch status {
	case model.JobDone, model.JobFailed, model.JobCancelled:
		t := now()
		finished = &t
	}
	_, err := r.db.Exec(`
		UPDATE batch_jobs SET status = ?, finished_at = ? WHERE id = ?`,
		status, finished, jobID)
	return err
}

// StartJob - переводит задачу в running, если она ещё не начата/не завершена
func (r *JobRepository) StartJob(jobID uuid.UUID) error {
	_, err := r.db.Exec(`
		UPDATE batch_jobs SET status = ?
		WHERE id = ? AND status IN (?, ?)`,
		model.JobRunning, jobID, model.JobPending, model.JobRunning)
	return err
}

// RequestCancel - помечает задачу как отменяемую, если она ещё не завершена.
// Возвращает false, если задача уже в конечном статусе.
func (r *JobRepository) RequestCancel(jobID uuid.UUID) (bool, error) {
	res, err := r.db.Exec(`
		UPDATE batch_jobs SET status = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		model.JobCancelling, jobID, model.JobPending, model.JobRunning, model.JobCancelling)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetJobProgress - получение прогресса для SSE
func (r *JobRepository) GetJobProgress(jobID uuid.UUID) (*model.JobProgress, error) {
	job, err := r.GetJobByID(jobID)
	if err != nil {
		return nil, err
	}

	progress := &model.JobProgress{
		JobID:       job.ID,
		Status:      job.Status,
		TotalItems:  job.TotalItems,
		DoneItems:   job.DoneItems,
		FailedItems: job.FailedItems,
	}

	if job.TotalItems > 0 {
		progress.Progress = float64(job.DoneItems+job.FailedItems) / float64(job.TotalItems) * 100
	}

	if !job.CreatedAt.IsZero() {
		end := time.Now()
		if job.FinishedAt != nil {
			end = *job.FinishedAt
		}
		progress.Elapsed = end.Sub(job.CreatedAt).Round(time.Second).String()
	}

	return progress, nil
}

// IsJobCancelled - проверка, запрошена ли отмена задачи
func (r *JobRepository) IsJobCancelled(jobID uuid.UUID) (bool, error) {
	var status string
	err := r.db.Get(&status, "SELECT status FROM batch_jobs WHERE id = ?", jobID)
	if err != nil {
		return false, err
	}
	return status == model.JobCancelling || status == model.JobCancelled, nil
}

// GetItem - спектр задачи
func (r *JobRepository) GetItem(jobID, itemID uuid.UUID) (*model.JobItem, error) {
	var item model.JobItem
	err := r.db.Get(&item,
		"SELECT "+itemColumns+" FROM batch_job_items WHERE id = ? AND job_id = ?",
		itemID, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// StartRun - заводит новый прогон спектра (номер = предыдущий + 1)
func (r *JobRepository) StartRun(itemID uuid.UUID, params model.JobParams) (*model.ItemRun, error) {
	var run model.ItemRun
	err := r.db.Get(&run, `
		INSERT INTO item_runs (id, item_id, run_no, params_json, status, created_at)
		VALUES (?, ?,
			(SELECT COALESCE(MAX(run_no), 0) + 1 FROM item_runs WHERE item_id = ?),
			?, ?, ?)
		RETURNING `+runColumns,
		uuid.New(), itemID, itemID, params, model.RunProcessing, now())
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// CompleteRun - сохраняет итог прогона и обновляет спектр одной транзакцией.
// Успешный прогон становится текущим результатом спектра. При ошибке спектр
// получает статус failed, но ссылки на прошлый удачный результат сохраняются,
// чтобы его можно было вернуть из истории.
func (r *JobRepository) CompleteRun(item model.JobItem, run model.ItemRun) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	finished := now()
	if _, err := tx.Exec(`
		UPDATE item_runs
		SET status = ?, result_path = ?, spectrum_path = ?, spectrum_rows = ?,
		    spectrum_cols = ?, spectrum_size = ?, error = ?, finished_at = ?
		WHERE id = ?`,
		run.Status, run.ResultPath, run.SpectrumPath, run.SpectrumRows,
		run.SpectrumCols, run.SpectrumSize, run.Error, finished, run.ID,
	); err != nil {
		return err
	}

	if run.Status == model.RunDone {
		_, err = tx.Exec(`
			UPDATE batch_job_items
			SET status = ?, error = NULL, finished_at = ?, current_run_id = ?,
			    result_path = ?, spectrum_path = ?, spectrum_rows = ?,
			    spectrum_cols = ?, spectrum_size = ?
			WHERE id = ?`,
			model.ItemDone, finished, run.ID,
			run.ResultPath, run.SpectrumPath, run.SpectrumRows,
			run.SpectrumCols, run.SpectrumSize, item.ID)
	} else {
		_, err = tx.Exec(`
			UPDATE batch_job_items SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
			model.ItemFailed, run.Error, finished, item.ID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteRun - удаляет незавершённый прогон (обработка прервана остановкой сервера)
func (r *JobRepository) DeleteRun(runID uuid.UUID) error {
	_, err := r.db.Exec(`DELETE FROM item_runs WHERE id = ? AND status = ?`, runID, model.RunProcessing)
	return err
}

// ListRuns - история прогонов спектра, новые сверху
func (r *JobRepository) ListRuns(itemID uuid.UUID) ([]model.ItemRun, error) {
	runs := []model.ItemRun{}
	err := r.db.Select(&runs,
		"SELECT "+runColumns+" FROM item_runs WHERE item_id = ? ORDER BY run_no DESC", itemID)
	return runs, err
}

// ReprocessItem - сохраняет новые параметры спектра и ставит его в очередь.
// Завершённая задача снова переходит в running.
func (r *JobRepository) ReprocessItem(jobID, itemID uuid.UUID, params model.JobParams) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var jobStatus, itemStatus string
	err = tx.QueryRowx(`
		SELECT j.status, i.status FROM batch_job_items i
		JOIN batch_jobs j ON j.id = i.job_id
		WHERE i.id = ? AND i.job_id = ?`, itemID, jobID).Scan(&jobStatus, &itemStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if jobStatus == model.JobCancelling {
		return ErrJobBusy
	}
	if itemStatus == model.ItemPending || itemStatus == model.ItemProcessing {
		return ErrItemBusy
	}

	if _, err := tx.Exec(`
		UPDATE batch_job_items
		SET params_json = ?, status = ?, error = NULL, started_at = NULL, finished_at = NULL
		WHERE id = ?`, params, model.ItemPending, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE batch_jobs
		SET status = CASE WHEN status IN (?, ?, ?) THEN ? ELSE status END,
		    finished_at = NULL
		WHERE id = ?`,
		model.JobDone, model.JobFailed, model.JobCancelled, model.JobRunning, jobID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = r.RecountJob(jobID)
	return err
}

// SelectRun - делает успешный прогон текущим результатом спектра.
// Параметры прогона становятся параметрами спектра.
func (r *JobRepository) SelectRun(jobID, itemID, runID uuid.UUID) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var itemStatus string
	err = tx.Get(&itemStatus,
		`SELECT status FROM batch_job_items WHERE id = ? AND job_id = ?`, itemID, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if itemStatus == model.ItemPending || itemStatus == model.ItemProcessing {
		return ErrItemBusy
	}

	var run model.ItemRun
	err = tx.Get(&run, "SELECT "+runColumns+" FROM item_runs WHERE id = ? AND item_id = ?", runID, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if run.Status != model.RunDone {
		return ErrRunNotUsable
	}

	if _, err := tx.Exec(`
		UPDATE batch_job_items
		SET status = ?, error = NULL, current_run_id = ?, params_json = ?,
		    result_path = ?, spectrum_path = ?, spectrum_rows = ?,
		    spectrum_cols = ?, spectrum_size = ?, finished_at = ?
		WHERE id = ?`,
		model.ItemDone, run.ID, run.Params,
		run.ResultPath, run.SpectrumPath, run.SpectrumRows,
		run.SpectrumCols, run.SpectrumSize, run.FinishedAt, itemID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return r.RefreshFinalStatus(jobID)
}

// RefreshFinalStatus - пересчитывает счётчики и, если задача уже завершена,
// её итоговый статус (done/failed) — например, после выбора другого прогона.
func (r *JobRepository) RefreshFinalStatus(jobID uuid.UUID) error {
	c, err := r.RecountJob(jobID)
	if err != nil {
		return err
	}
	if c.Pending > 0 || c.Processing > 0 {
		return nil
	}
	status := model.JobDone
	if c.Failed > 0 {
		status = model.JobFailed
	}
	_, err = r.db.Exec(`
		UPDATE batch_jobs SET status = ? WHERE id = ? AND status IN (?, ?)`,
		status, jobID, model.JobDone, model.JobFailed)
	return err
}

// RecoverInterrupted - вызывается при старте: возвращает в очередь элементы,
// которые обрабатывались в момент остановки сервера, и отдаёт ID незавершённых
// задач, чтобы их снова отправить в пул воркеров.
func (r *JobRepository) RecoverInterrupted() ([]uuid.UUID, error) {
	if _, err := r.db.Exec(`
		UPDATE batch_job_items SET status = ?, started_at = NULL WHERE status = ?`,
		model.ItemPending, model.ItemProcessing); err != nil {
		return nil, err
	}
	// Прогоны, прерванные на середине, не несут результата
	if _, err := r.db.Exec(`DELETE FROM item_runs WHERE status = ?`, model.RunProcessing); err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	err := r.db.Select(&ids, `
		SELECT id FROM batch_jobs WHERE status IN (?, ?, ?) ORDER BY created_at`,
		model.JobPending, model.JobRunning, model.JobCancelling)
	return ids, err
}
