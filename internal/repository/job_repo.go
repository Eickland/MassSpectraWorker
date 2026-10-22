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

// ErrNotFound - задача или элемент не найдены
var ErrNotFound = errors.New("not found")

const jobColumns = `id, status, source_folder, params_json, total_items,
	done_items, failed_items, created_at, finished_at`

const itemColumns = `id, job_id, spectra_name, spectra_path, status, result_path,
	error, file_hash, created_at, started_at, finished_at,
	spectrum_path, spectrum_rows, spectrum_cols, spectrum_size`

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
			id, job_id, spectra_name, spectra_path, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?)`)
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
			items[i].Status, items[i].CreatedAt,
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

// UpdateItemResult - сохраняет итог обработки элемента
func (r *JobRepository) UpdateItemResult(item model.JobItem) error {
	_, err := r.db.Exec(`
		UPDATE batch_job_items
		SET status        = ?,
		    result_path   = ?,
		    spectrum_path = ?,
		    spectrum_rows = ?,
		    spectrum_cols = ?,
		    spectrum_size = ?,
		    error         = ?,
		    finished_at   = ?
		WHERE id = ?`,
		item.Status,
		item.ResultPath,
		item.SpectrumPath,
		item.SpectrumRows,
		item.SpectrumCols,
		item.SpectrumSize,
		item.Error,
		now(),
		item.ID,
	)
	return err
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

// RecoverInterrupted - вызывается при старте: возвращает в очередь элементы,
// которые обрабатывались в момент остановки сервера, и отдаёт ID незавершённых
// задач, чтобы их снова отправить в пул воркеров.
func (r *JobRepository) RecoverInterrupted() ([]uuid.UUID, error) {
	if _, err := r.db.Exec(`
		UPDATE batch_job_items SET status = ?, started_at = NULL WHERE status = ?`,
		model.ItemPending, model.ItemProcessing); err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	err := r.db.Select(&ids, `
		SELECT id FROM batch_jobs WHERE status IN (?, ?, ?) ORDER BY created_at`,
		model.JobPending, model.JobRunning, model.JobCancelling)
	return ids, err
}
