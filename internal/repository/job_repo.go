package repository

import (
	"MassSpectraWorker/internal/model"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type JobRepository struct {
	db *sqlx.DB
}

func NewJobRepository(db *sqlx.DB) *JobRepository {
	return &JobRepository{db: db}
}

// CreateJob - создает задачу и возвращает ID
func (r *JobRepository) CreateJob(job *model.Job) error {
	query := `
        INSERT INTO batch_jobs (
            id, status, source_folder, params_json, 
            total_items, done_items, failed_items, created_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `
	_, err := r.db.Exec(query,
		job.ID, job.Status, job.SourceFolder, job.Params,
		job.TotalItems, job.DoneItems, job.FailedItems, job.CreatedAt,
	)
	return err
}

// CreateJobItems - массовое создание элементов
func (r *JobRepository) CreateJobItems(items []model.JobItem) error {
	if len(items) == 0 {
		return nil
	}

	query := `
        INSERT INTO batch_job_items (
            id, job_id, spectra_name, spectra_path, status, created_at
        ) VALUES ($1, $2, $3, $4, $5, $6)
    `

	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, item := range items {
		_, err := tx.Exec(query,
			item.ID, item.JobID, item.SpectraName, item.SpectraPath,
			item.Status, time.Now(),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetJobByID - получение задачи по ID
func (r *JobRepository) GetJobByID(id uuid.UUID) (*model.Job, error) {
	var job model.Job
	err := r.db.Get(&job, "SELECT * FROM batch_jobs WHERE id = $1", id)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job not found")
	}
	return &job, err
}

// GetJobItems - получение элементов задачи
func (r *JobRepository) GetJobItems(jobID uuid.UUID) ([]model.JobItem, error) {
	var items []model.JobItem
	err := r.db.Select(&items,
		"SELECT * FROM batch_job_items WHERE job_id = $1 ORDER BY created_at",
		jobID,
	)
	return items, err
}

// GetPendingItems - получение элементов для обработки (с блокировкой)
func (r *JobRepository) GetPendingItems(jobID uuid.UUID, limit int) ([]model.JobItem, error) {
	var items []model.JobItem
	err := r.db.Select(&items, `
        SELECT * FROM batch_job_items 
        WHERE job_id = $1 AND status = 'pending'
        ORDER BY created_at
        LIMIT $2
        FOR UPDATE SKIP LOCKED
    `, jobID, limit)
	return items, err
}

// UpdateItemStatus - обновление статуса элемента
func (r *JobRepository) UpdateItemStatus(itemID uuid.UUID, status string, resultPath, errMsg *string) error {
	query := `
        UPDATE batch_job_items 
        SET status = $1, result_path = $2, error = $3, 
            created_at = COALESCE(created_at, NOW()), 
            finished_at = NOW()
        WHERE id = $4
    `
	_, err := r.db.Exec(query, status, resultPath, errMsg, itemID)
	return err
}

// UpdateJobCounters - обновление счетчиков задачи
func (r *JobRepository) UpdateJobCounters(jobID uuid.UUID, doneItems, failedItems int) error {
	query := `
        UPDATE batch_jobs 
        SET done_items = $1, failed_items = $2
        WHERE id = $3
    `
	_, err := r.db.Exec(query, doneItems, failedItems, jobID)
	return err
}

// UpdateJobStatus - обновление статуса задачи
func (r *JobRepository) UpdateJobStatus(jobID uuid.UUID, status string) error {
	query := `
        UPDATE batch_jobs 
        SET status = $1, finished_at = CASE WHEN $1 IN ('done', 'failed', 'cancelled') THEN NOW() ELSE NULL END
        WHERE id = $2
    `
	_, err := r.db.Exec(query, status, jobID)
	return err
}

// GetJobProgress - получение прогресса для SSE
func (r *JobRepository) GetJobProgress(jobID uuid.UUID) (*model.JobProgress, error) {
	var job model.Job
	err := r.db.Get(&job, "SELECT * FROM batch_jobs WHERE id = $1", jobID)
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
		elapsed := time.Since(job.CreatedAt)
		progress.Elapsed = elapsed.String()
	}

	return progress, nil
}

// MarkJobAsCancelled - отметка задачи как отмененной
func (r *JobRepository) MarkJobAsCancelled(jobID uuid.UUID) error {
	return r.UpdateJobStatus(jobID, "cancelled")
}

// IsJobCancelled - проверка, отменена ли задача
func (r *JobRepository) IsJobCancelled(jobID uuid.UUID) (bool, error) {
	var status string
	err := r.db.Get(&status, "SELECT status FROM batch_jobs WHERE id = $1", jobID)
	if err != nil {
		return false, err
	}
	return status == "cancelling" || status == "cancelled", nil
}
