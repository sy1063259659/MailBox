package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxICloudHMEDeleteJobItems = 100

type ICloudHMEDeleteJob struct {
	ID             int64                    `json:"id"`
	RequestedCount int                      `json:"requestedCount"`
	Status         string                   `json:"status"`
	CompletedCount int                      `json:"completedCount"`
	FailedCount    int                      `json:"failedCount"`
	CreatedBy      string                   `json:"createdBy"`
	StartedAt      *time.Time               `json:"startedAt,omitempty"`
	FinishedAt     *time.Time               `json:"finishedAt,omitempty"`
	CreatedAt      time.Time                `json:"createdAt"`
	UpdatedAt      time.Time                `json:"updatedAt"`
	Items          []ICloudHMEDeleteJobItem `json:"items,omitempty"`
}

type ICloudHMEDeleteJobItem struct {
	ID           int64      `json:"id"`
	JobID        int64      `json:"jobId"`
	Sequence     int        `json:"sequence"`
	Email        string     `json:"email"`
	Status       string     `json:"status"`
	Attempts     int        `json:"attempts"`
	ErrorCode    string     `json:"errorCode,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func normalizeICloudHMEDeleteEmails(emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, errors.New("请至少选择一个隐藏邮箱")
	}
	if len(emails) > maxICloudHMEDeleteJobItems {
		return nil, fmt.Errorf("单次最多永久删除 %d 个隐藏邮箱", maxICloudHMEDeleteJobItems)
	}
	result := make([]string, 0, len(emails))
	seen := make(map[string]struct{}, len(emails))
	for _, value := range emails {
		email := normalizeEmail(value)
		if email == "" {
			return nil, errors.New("隐藏邮箱不能为空")
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		result = append(result, email)
	}
	if len(result) > maxICloudHMEDeleteJobItems {
		return nil, fmt.Errorf("单次最多永久删除 %d 个隐藏邮箱", maxICloudHMEDeleteJobItems)
	}
	return result, nil
}

func (s *Store) CreateICloudHMEDeleteJob(ctx context.Context, emails []string, createdBy string) (ICloudHMEDeleteJob, error) {
	normalized, err := normalizeICloudHMEDeleteEmails(emails)
	if err != nil {
		return ICloudHMEDeleteJob{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: begin iCloud HME delete job: %w", err)
	}
	defer tx.Rollback(ctx)

	var existing int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM icloud_hme_aliases WHERE lower(email) = ANY($1::text[])
	`, normalized).Scan(&existing); err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: validate iCloud HME delete job: %w", err)
	}
	if existing != len(normalized) {
		return ICloudHMEDeleteJob{}, errors.New("部分隐藏邮箱不存在，请刷新列表后重试")
	}

	var job ICloudHMEDeleteJob
	err = tx.QueryRow(ctx, `
		INSERT INTO icloud_hme_delete_jobs (requested_count, created_by)
		VALUES ($1, $2)
		RETURNING id, requested_count, status, completed_count, failed_count, created_by,
		          started_at, finished_at, created_at, updated_at
	`, len(normalized), strings.TrimSpace(createdBy)).Scan(
		&job.ID, &job.RequestedCount, &job.Status, &job.CompletedCount, &job.FailedCount,
		&job.CreatedBy, &job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: create iCloud HME delete job: %w", err)
	}
	for index, email := range normalized {
		if _, err := tx.Exec(ctx, `
			INSERT INTO icloud_hme_delete_job_items (job_id, sequence, email)
			VALUES ($1, $2, $3)
		`, job.ID, index+1, email); err != nil {
			return ICloudHMEDeleteJob{}, fmt.Errorf("store: create iCloud HME delete job item: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: commit iCloud HME delete job: %w", err)
	}
	return s.GetICloudHMEDeleteJob(ctx, job.ID)
}

func (s *Store) GetICloudHMEDeleteJob(ctx context.Context, id int64) (ICloudHMEDeleteJob, error) {
	var job ICloudHMEDeleteJob
	err := s.pool.QueryRow(ctx, `
		SELECT id, requested_count, status, completed_count, failed_count, created_by,
		       started_at, finished_at, created_at, updated_at
		FROM icloud_hme_delete_jobs WHERE id = $1
	`, id).Scan(
		&job.ID, &job.RequestedCount, &job.Status, &job.CompletedCount, &job.FailedCount,
		&job.CreatedBy, &job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ICloudHMEDeleteJob{}, errors.New("永久删除任务不存在")
	}
	if err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: get iCloud HME delete job: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, job_id, sequence, email, status, attempts, error_code, error_message,
		       started_at, finished_at, created_at, updated_at
		FROM icloud_hme_delete_job_items WHERE job_id = $1 ORDER BY sequence
	`, id)
	if err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: list iCloud HME delete job items: %w", err)
	}
	defer rows.Close()
	job.Items = []ICloudHMEDeleteJobItem{}
	for rows.Next() {
		var item ICloudHMEDeleteJobItem
		if err := rows.Scan(
			&item.ID, &item.JobID, &item.Sequence, &item.Email, &item.Status, &item.Attempts,
			&item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.FinishedAt,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return ICloudHMEDeleteJob{}, fmt.Errorf("store: scan iCloud HME delete job item: %w", err)
		}
		job.Items = append(job.Items, item)
	}
	return job, rows.Err()
}

func (s *Store) NextRunnableICloudHMEDeleteJob(ctx context.Context) (ICloudHMEDeleteJob, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		UPDATE icloud_hme_delete_jobs job
		SET status = 'running', started_at = COALESCE(started_at, now()), updated_at = now()
		WHERE job.id = (
			SELECT candidate.id FROM icloud_hme_delete_jobs candidate
			WHERE candidate.status IN ('pending', 'running')
			  AND EXISTS (SELECT 1 FROM icloud_hme_delete_job_items item WHERE item.job_id = candidate.id AND item.status = 'pending')
			ORDER BY candidate.created_at, candidate.id
			FOR UPDATE SKIP LOCKED LIMIT 1
		)
		RETURNING job.id
	`).Scan(&id)
	if err != nil {
		return ICloudHMEDeleteJob{}, err
	}
	return s.GetICloudHMEDeleteJob(ctx, id)
}

func (s *Store) ClaimNextICloudHMEDeleteJobItem(ctx context.Context, jobID int64) (ICloudHMEDeleteJobItem, error) {
	var item ICloudHMEDeleteJobItem
	err := s.pool.QueryRow(ctx, `
		UPDATE icloud_hme_delete_job_items item
		SET status = 'running', attempts = attempts + 1, started_at = now(), finished_at = NULL,
		    error_code = '', error_message = '', updated_at = now()
		WHERE item.id = (
			SELECT candidate.id FROM icloud_hme_delete_job_items candidate
			WHERE candidate.job_id = $1 AND candidate.status = 'pending'
			ORDER BY candidate.sequence
			FOR UPDATE SKIP LOCKED LIMIT 1
		)
		RETURNING id, job_id, sequence, email, status, attempts, error_code, error_message,
		          started_at, finished_at, created_at, updated_at
	`, jobID).Scan(
		&item.ID, &item.JobID, &item.Sequence, &item.Email, &item.Status, &item.Attempts,
		&item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.FinishedAt,
		&item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (s *Store) CompleteICloudHMEDeleteJobItem(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_delete_job_items
		SET status = 'completed', finished_at = now(), updated_at = now()
		WHERE id = $1
	`, id)
	return err
}

func (s *Store) FailICloudHMEDeleteJobItem(ctx context.Context, id int64, code, message string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_delete_job_items
		SET status = 'failed', error_code = $2, error_message = $3,
		    finished_at = now(), updated_at = now()
		WHERE id = $1
	`, id, strings.TrimSpace(code), truncateICloudHMEAuditMessage(message))
	return err
}

func (s *Store) RefreshICloudHMEDeleteJob(ctx context.Context, id int64) (ICloudHMEDeleteJob, error) {
	var completed, failed, pending, running int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'completed'),
		       count(*) FILTER (WHERE status = 'failed'),
		       count(*) FILTER (WHERE status = 'pending'),
		       count(*) FILTER (WHERE status = 'running')
		FROM icloud_hme_delete_job_items WHERE job_id = $1
	`, id).Scan(&completed, &failed, &pending, &running); err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: aggregate iCloud HME delete job: %w", err)
	}
	status := "running"
	finished := false
	if pending == 0 && running == 0 {
		finished = true
		if failed > 0 {
			status = "partial_failed"
		} else {
			status = "completed"
		}
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_delete_jobs
		SET status = $2, completed_count = $3, failed_count = $4,
		    finished_at = CASE WHEN $5 THEN COALESCE(finished_at, now()) ELSE NULL END,
		    updated_at = now()
		WHERE id = $1
	`, id, status, completed, failed, finished); err != nil {
		return ICloudHMEDeleteJob{}, fmt.Errorf("store: refresh iCloud HME delete job: %w", err)
	}
	return s.GetICloudHMEDeleteJob(ctx, id)
}

func (s *Store) RecoverICloudHMEDeleteJobs(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_delete_job_items
		SET status = 'pending', started_at = NULL, updated_at = now()
		WHERE status = 'running'
	`); err != nil {
		return fmt.Errorf("store: recover iCloud HME delete job items: %w", err)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_delete_jobs SET status = 'pending', updated_at = now()
		WHERE status = 'running'
	`)
	return err
}
