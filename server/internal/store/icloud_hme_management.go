package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	ICloudHMEJobModeFixed = "fixed"
	ICloudHMEJobModePool  = "pool"
)

type ICloudHMECreateJob struct {
	ID              int64                    `json:"id"`
	Mode            string                   `json:"mode"`
	SourceAccountID *int64                   `json:"sourceAccountId,omitempty"`
	LabelPrefix     string                   `json:"labelPrefix"`
	GroupName       string                   `json:"groupName"`
	RequestedCount  int                      `json:"requestedCount"`
	Status          string                   `json:"status"`
	CompletedCount  int                      `json:"completedCount"`
	FailedCount     int                      `json:"failedCount"`
	CancelledCount  int                      `json:"cancelledCount"`
	CreatedBy       string                   `json:"createdBy"`
	Origin          string                   `json:"origin"`
	ErrorMessage    string                   `json:"errorMessage,omitempty"`
	StartedAt       *time.Time               `json:"startedAt,omitempty"`
	FinishedAt      *time.Time               `json:"finishedAt,omitempty"`
	CreatedAt       time.Time                `json:"createdAt"`
	UpdatedAt       time.Time                `json:"updatedAt"`
	Items           []ICloudHMECreateJobItem `json:"items,omitempty"`
}

type ICloudHMECreateJobItem struct {
	ID              int64      `json:"id"`
	JobID           int64      `json:"jobId"`
	Sequence        int        `json:"sequence"`
	SourceAccountID *int64     `json:"sourceAccountId,omitempty"`
	Label           string     `json:"label"`
	Email           string     `json:"email,omitempty"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	ErrorCode       string     `json:"errorCode,omitempty"`
	ErrorMessage    string     `json:"errorMessage,omitempty"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	NextAttemptAt   *time.Time `json:"nextAttemptAt,omitempty"`
	RetryClass      string     `json:"retryClass,omitempty"`
}

type ICloudHMECreateJobInput struct {
	Mode            string
	SourceAccountID *int64
	LabelPrefix     string
	GroupName       string
	Count           int
	CreatedBy       string
	Origin          string
}

type ICloudHMEAuditLog struct {
	ID         int64     `json:"id"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	TargetType string    `json:"targetType"`
	Target     string    `json:"target"`
	Result     string    `json:"result"`
	ErrorCode  string    `json:"errorCode,omitempty"`
	Message    string    `json:"message,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (s *Store) CreateICloudHMEJob(ctx context.Context, input ICloudHMECreateJobInput) (ICloudHMECreateJob, error) {
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode != ICloudHMEJobModeFixed && input.Mode != ICloudHMEJobModePool {
		return ICloudHMECreateJob{}, errors.New("创建模式必须为 fixed 或 pool")
	}
	if input.Count < 1 || input.Count > 20 {
		return ICloudHMECreateJob{}, errors.New("单次最多创建 20 个隐藏邮箱")
	}
	if input.Mode == ICloudHMEJobModeFixed && (input.SourceAccountID == nil || *input.SourceAccountID <= 0) {
		return ICloudHMECreateJob{}, errors.New("固定主账号模式必须选择主账号")
	}
	if input.Mode == ICloudHMEJobModePool {
		input.SourceAccountID = nil
	}
	input.LabelPrefix = strings.TrimSpace(input.LabelPrefix)
	if input.LabelPrefix == "" {
		input.LabelPrefix = "MailBox"
	}
	if len([]rune(input.LabelPrefix)) > 80 {
		return ICloudHMECreateJob{}, errors.New("标签前缀最多 80 个字符")
	}
	input.GroupName = normalizeICloudHMEGroup(input.GroupName)
	if input.Origin != "automation" {
		input.Origin = "manual"
	}

	// The transaction starts as a write transaction (txlock=immediate), so the
	// label sequence allocation below is serialized without an advisory lock.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: begin create iCloud HME job: %w", err)
	}
	defer tx.Rollback(ctx)

	var sequenceStart int
	if err := tx.QueryRow(ctx, `
		WITH labels AS (
			SELECT substr(label, length(?) + 3) AS suffix
			FROM icloud_hme_aliases
			WHERE substr(label, 1, length(?) + 2) = ? || ' #'
			UNION ALL
			SELECT substr(item.label, length(?) + 3) AS suffix
			FROM icloud_hme_create_job_items item
			WHERE substr(item.label, 1, length(?) + 2) = ? || ' #'
		)
		SELECT COALESCE(max(CAST(suffix AS INTEGER)), 0)
		FROM labels WHERE suffix <> '' AND suffix NOT GLOB '*[^0-9]*'
	`, input.LabelPrefix, input.LabelPrefix, input.LabelPrefix,
		input.LabelPrefix, input.LabelPrefix, input.LabelPrefix).Scan(&sequenceStart); err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: allocate iCloud HME label sequence: %w", err)
	}

	var healthySourceExists bool
	if input.Mode == ICloudHMEJobModeFixed {
		err = tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM icloud_hme_source_accounts
				WHERE id = ? AND status = 'active' AND cookies_encrypted <> ''
			)
		`, *input.SourceAccountID).Scan(&healthySourceExists)
	} else {
		if input.Origin == "automation" {
			err = tx.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM icloud_hme_source_accounts
					WHERE automation_enabled AND status IN ('active', 'cooldown')
					  AND cookies_encrypted <> ''
				)
			`).Scan(&healthySourceExists)
		} else {
			err = tx.QueryRow(ctx, `
				SELECT EXISTS(
					SELECT 1 FROM icloud_hme_source_accounts
					WHERE status = 'active' AND cookies_encrypted <> ''
				)
			`).Scan(&healthySourceExists)
		}
	}
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: validate iCloud HME job source: %w", err)
	}
	if !healthySourceExists {
		return ICloudHMECreateJob{}, errors.New("没有可用于创建任务的健康 Apple 主账号")
	}

	var jobID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO icloud_hme_create_jobs (
			mode, source_account_id, label_prefix, group_name, requested_count, created_by, origin
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`, input.Mode, input.SourceAccountID, input.LabelPrefix, input.GroupName, input.Count, strings.TrimSpace(input.CreatedBy), input.Origin).Scan(&jobID)
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: create iCloud HME job: %w", err)
	}
	for sequence := 1; sequence <= input.Count; sequence++ {
		label := fmt.Sprintf("%s #%d", input.LabelPrefix, sequenceStart+sequence)
		if _, err := tx.Exec(ctx, `
			INSERT INTO icloud_hme_create_job_items (job_id, sequence, source_account_id, label)
			VALUES (?, ?, ?, ?)
		`, jobID, sequence, input.SourceAccountID, label); err != nil {
			return ICloudHMECreateJob{}, fmt.Errorf("store: create iCloud HME job item: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: commit iCloud HME job: %w", err)
	}
	return s.GetICloudHMEJob(ctx, jobID)
}

func (s *Store) ListICloudHMEJobs(ctx context.Context, limit int) ([]ICloudHMECreateJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, mode, source_account_id, label_prefix, group_name, requested_count,
		       status, completed_count, failed_count, cancelled_count, created_by,
		       origin, error_message, started_at, finished_at, created_at, updated_at
		FROM icloud_hme_create_jobs
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud HME jobs: %w", err)
	}
	defer rows.Close()

	jobs := []ICloudHMECreateJob{}
	for rows.Next() {
		var job ICloudHMECreateJob
		if err := scanICloudHMEJob(rows, &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) GetICloudHMEJob(ctx context.Context, id int64) (ICloudHMECreateJob, error) {
	var job ICloudHMECreateJob
	err := scanICloudHMEJob(s.pool.QueryRow(ctx, `
		SELECT id, mode, source_account_id, label_prefix, group_name, requested_count,
		       status, completed_count, failed_count, cancelled_count, created_by,
		       origin, error_message, started_at, finished_at, created_at, updated_at
		FROM icloud_hme_create_jobs WHERE id = ?
	`, id), &job)
	if errors.Is(err, sql.ErrNoRows) {
		return ICloudHMECreateJob{}, errors.New("创建任务不存在")
	}
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: get iCloud HME job: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, job_id, sequence, source_account_id, label, email, status, attempts,
		       error_code, error_message, started_at, finished_at, created_at, updated_at,
		       next_attempt_at, retry_class
		FROM icloud_hme_create_job_items WHERE job_id = ? ORDER BY sequence ASC
	`, id)
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: list iCloud HME job items: %w", err)
	}
	defer rows.Close()
	job.Items = []ICloudHMECreateJobItem{}
	for rows.Next() {
		var item ICloudHMECreateJobItem
		if err := rows.Scan(
			&item.ID, &item.JobID, &item.Sequence, &item.SourceAccountID, &item.Label,
			&item.Email, &item.Status, &item.Attempts, &item.ErrorCode, &item.ErrorMessage,
			&item.StartedAt, &item.FinishedAt, &item.CreatedAt, &item.UpdatedAt,
			&item.NextAttemptAt, &item.RetryClass,
		); err != nil {
			return ICloudHMECreateJob{}, fmt.Errorf("store: scan iCloud HME job item: %w", err)
		}
		job.Items = append(job.Items, item)
	}
	return job, rows.Err()
}

func scanICloudHMEJob(row interface{ Scan(...any) error }, job *ICloudHMECreateJob) error {
	if err := row.Scan(
		&job.ID, &job.Mode, &job.SourceAccountID, &job.LabelPrefix, &job.GroupName, &job.RequestedCount,
		&job.Status, &job.CompletedCount, &job.FailedCount, &job.CancelledCount, &job.CreatedBy,
		&job.Origin, &job.ErrorMessage, &job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		return fmt.Errorf("store: scan iCloud HME job: %w", err)
	}
	return nil
}

func (s *Store) NextRunnableICloudHMEJob(ctx context.Context) (ICloudHMECreateJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: begin next iCloud HME job: %w", err)
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM icloud_hme_create_jobs
		WHERE status IN ('pending', 'running')
		  AND (origin <> 'automation' OR EXISTS (
		        SELECT 1 FROM icloud_hme_automation_settings WHERE id = 1 AND enabled
		      ))
		  AND EXISTS (SELECT 1 FROM icloud_hme_create_job_items item
		              WHERE item.job_id = icloud_hme_create_jobs.id
		                AND item.status = 'pending'
		                AND (item.next_attempt_at IS NULL OR item.next_attempt_at <= CURRENT_TIMESTAMP))
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ICloudHMECreateJob{}, sql.ErrNoRows
	}
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: next iCloud HME job: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE icloud_hme_create_jobs
		SET status = 'running', started_at = COALESCE(started_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, id); err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: start iCloud HME job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: commit next iCloud HME job: %w", err)
	}
	return s.GetICloudHMEJob(ctx, id)
}

func (s *Store) ClaimNextICloudHMEJobItem(ctx context.Context, jobID int64) (ICloudHMECreateJobItem, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ICloudHMECreateJobItem{}, fmt.Errorf("store: begin claim iCloud HME item: %w", err)
	}
	defer tx.Rollback(ctx)

	var itemID int64
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM icloud_hme_create_job_items
		WHERE job_id = ? AND status = 'pending'
		  AND (next_attempt_at IS NULL OR next_attempt_at <= CURRENT_TIMESTAMP)
		ORDER BY sequence ASC
		LIMIT 1
	`, jobID).Scan(&itemID)
	if err != nil {
		return ICloudHMECreateJobItem{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE icloud_hme_create_job_items
		SET status = 'running', attempts = attempts + 1, started_at = CURRENT_TIMESTAMP,
		    finished_at = NULL, error_code = '', error_message = '', updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, itemID); err != nil {
		return ICloudHMECreateJobItem{}, fmt.Errorf("store: claim iCloud HME item: %w", err)
	}
	var item ICloudHMECreateJobItem
	err = tx.QueryRow(ctx, `
		SELECT id, job_id, sequence, source_account_id, label, email, status, attempts,
		       error_code, error_message, started_at, finished_at, created_at, updated_at,
		       next_attempt_at, retry_class
		FROM icloud_hme_create_job_items
		WHERE id = ?
	`, itemID).Scan(
		&item.ID, &item.JobID, &item.Sequence, &item.SourceAccountID, &item.Label,
		&item.Email, &item.Status, &item.Attempts, &item.ErrorCode, &item.ErrorMessage,
		&item.StartedAt, &item.FinishedAt, &item.CreatedAt, &item.UpdatedAt,
		&item.NextAttemptAt, &item.RetryClass,
	)
	if err != nil {
		return ICloudHMECreateJobItem{}, fmt.Errorf("store: claim iCloud HME item: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ICloudHMECreateJobItem{}, fmt.Errorf("store: commit claim iCloud HME item: %w", err)
	}
	return item, nil
}

func (s *Store) CompleteICloudHMEJobItem(ctx context.Context, itemID, sourceID int64, email string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_job_items
		SET source_account_id = ?, email = ?, status = 'completed',
		    finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, sourceID, normalizeEmail(email), itemID)
	if err != nil {
		return fmt.Errorf("store: complete iCloud HME job item: %w", err)
	}
	return nil
}

func (s *Store) FailICloudHMEJobItem(ctx context.Context, itemID int64, sourceID *int64, code, message string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_job_items
		SET source_account_id = COALESCE(?, source_account_id), status = 'failed',
		    error_code = ?, error_message = ?, finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, sourceID, strings.TrimSpace(code), truncateICloudHMEAuditMessage(message), itemID)
	if err != nil {
		return fmt.Errorf("store: fail iCloud HME job item: %w", err)
	}
	return nil
}

func (s *Store) RefreshICloudHMEJobProgress(ctx context.Context, jobID int64) (ICloudHMECreateJob, error) {
	var completed, failed, cancelled, pending, running int
	var currentStatus string
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'completed'),
			count(*) FILTER (WHERE status = 'failed'),
			count(*) FILTER (WHERE status = 'cancelled'),
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'running'),
			(SELECT status FROM icloud_hme_create_jobs WHERE id = ?)
		FROM icloud_hme_create_job_items WHERE job_id = ?
	`, jobID, jobID).Scan(&completed, &failed, &cancelled, &pending, &running, &currentStatus)
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: aggregate iCloud HME job items: %w", err)
	}
	status, finished := iCloudHMEJobAggregateStatus(currentStatus, completed, failed, cancelled, pending, running)
	_, err = s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_jobs
		SET status = ?, completed_count = ?, failed_count = ?, cancelled_count = ?,
		    finished_at = CASE WHEN ? THEN COALESCE(finished_at, CURRENT_TIMESTAMP) ELSE NULL END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, completed, failed, cancelled, finished, jobID)
	if err != nil {
		return ICloudHMECreateJob{}, fmt.Errorf("store: refresh iCloud HME job: %w", err)
	}
	return s.GetICloudHMEJob(ctx, jobID)
}

func iCloudHMEJobAggregateStatus(current string, completed, failed, cancelled, pending, running int) (string, bool) {
	if pending > 0 || running > 0 {
		if current == "cancel_requested" {
			return "cancel_requested", false
		}
		return "running", false
	}
	switch {
	case cancelled > 0 && completed == 0 && failed == 0:
		return "cancelled", true
	case failed > 0 || cancelled > 0:
		return "partial_failed", true
	default:
		return "completed", true
	}
}

func (s *Store) CancelICloudHMEJob(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_jobs SET status = 'cancel_requested', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status IN ('pending', 'running')
	`, id)
	if err != nil {
		return fmt.Errorf("store: cancel iCloud HME job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("任务不存在或已结束")
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_job_items
		SET status = 'cancelled', finished_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND status = 'pending'
	`, id)
	return err
}

func (s *Store) RetryICloudHMEJob(ctx context.Context, id int64) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_job_items
		SET status = 'pending', source_account_id = NULL, email = '', error_code = '',
		    error_message = '', started_at = NULL, finished_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND status = 'failed'
	`, id)
	if err != nil {
		return fmt.Errorf("store: retry iCloud HME job: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("没有可重试的失败项")
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_jobs
		SET status = 'pending', failed_count = 0, error_message = '', finished_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, id)
	return err
}

func (s *Store) RecoverICloudHMEJobs(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_job_items
		SET status = 'pending', started_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE status = 'running'
	`); err != nil {
		return fmt.Errorf("store: recover iCloud HME job items: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_job_items AS item
		SET status = 'cancelled', finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
		FROM icloud_hme_create_jobs AS job
		WHERE item.job_id = job.id AND job.status = 'cancel_requested'
		  AND item.status = 'pending'
	`); err != nil {
		return fmt.Errorf("store: recover cancelled iCloud HME job items: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_jobs
		SET status = 'pending', updated_at = CURRENT_TIMESTAMP
		WHERE status = 'running'
	`); err != nil {
		return fmt.Errorf("store: recover iCloud HME jobs: %w", err)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_jobs AS job
		SET status = 'cancelled',
		    completed_count = (SELECT count(*) FROM icloud_hme_create_job_items WHERE job_id = job.id AND status = 'completed'),
		    failed_count = (SELECT count(*) FROM icloud_hme_create_job_items WHERE job_id = job.id AND status = 'failed'),
		    cancelled_count = (SELECT count(*) FROM icloud_hme_create_job_items WHERE job_id = job.id AND status = 'cancelled'),
		    finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP), updated_at = CURRENT_TIMESTAMP
		WHERE status = 'cancel_requested'
	`)
	return err
}

func (s *Store) IsICloudHMEJobCancelRequested(ctx context.Context, id int64) (bool, error) {
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM icloud_hme_create_jobs WHERE id = ?`, id).Scan(&status)
	return status == "cancel_requested" || status == "cancelled", err
}

func (s *Store) HealthyICloudHMESources(ctx context.Context) ([]ICloudHMESourceCredentials, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM icloud_hme_source_accounts
		WHERE status = 'active' AND cookies_encrypted <> ''
		ORDER BY alias_total ASC, COALESCE(last_created_at, '1970-01-01 00:00:00') ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list healthy iCloud HME sources: %w", err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var result []ICloudHMESourceCredentials
	for _, id := range ids {
		credentials, err := s.GetICloudHMESourceCredentials(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, credentials)
	}
	return result, nil
}

func (s *Store) MarkICloudHMESourceActivity(ctx context.Context, id int64, activity string, errValue error) error {
	field := "last_created_at"
	if activity == "sync" {
		field = "last_synced_at"
	}
	if errValue != nil {
		_, err := s.pool.Exec(ctx, `
			UPDATE icloud_hme_source_accounts
			SET status = 'error', status_reason = ?, last_error_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, truncateICloudHMEAuditMessage(errValue.Error()), id)
		return err
	}
	query := fmt.Sprintf(`
		UPDATE icloud_hme_source_accounts
		SET %s = CURRENT_TIMESTAMP, status = 'active', status_reason = '', updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, field)
	_, err := s.pool.Exec(ctx, query, id)
	return err
}

func (s *Store) GetICloudHMEAlias(ctx context.Context, email string) (ICloudHMEAlias, error) {
	var alias ICloudHMEAlias
	err := s.pool.QueryRow(ctx, `
		SELECT a.email, a.source_account_id, s.name, a.anonymous_id, a.label, a.active,
		       a.apple_status, a.deactivated_at, a.deleted_at, a.last_synced_at,
		       g.name, a.remark, s.app_password_encrypted <> '',
		       a.receive_key_encrypted <> '', a.receive_key_updated_at,
		       a.inventory_status, a.sold_at,
		       a.gpt_status, a.gpt_plus_activated_at, a.gpt_deactivated_at,
		       a.gpt_last_scanned_at, a.gpt_scan_error,
		       a.created_at, a.updated_at
		FROM icloud_hme_aliases a
		JOIN icloud_hme_source_accounts s ON s.id = a.source_account_id
		JOIN icloud_hme_groups g ON g.id = a.group_id
		WHERE lower(a.email) = ?
	`, normalizeEmail(email)).Scan(
		&alias.Email, &alias.SourceAccountID, &alias.SourceAccountName, &alias.AnonymousID,
		&alias.Label, &alias.Active, &alias.AppleStatus, &alias.DeactivatedAt, &alias.DeletedAt,
		&alias.LastSyncedAt, &alias.Group, &alias.Remark, &alias.MailReady,
		&alias.ReceiveKeyConfigured, &alias.ReceiveKeyUpdatedAt, &alias.InventoryStatus,
		&alias.SoldAt, &alias.GPTStatus, &alias.GPTPlusActivatedAt,
		&alias.GPTDeactivatedAt, &alias.GPTLastScannedAt, &alias.GPTScanError,
		&alias.CreatedAt, &alias.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ICloudHMEAlias{}, errors.New("隐藏邮箱不存在")
	}
	if err != nil {
		return ICloudHMEAlias{}, fmt.Errorf("store: get iCloud HME alias: %w", err)
	}
	return alias, nil
}

func (s *Store) FindICloudHMEAliasByLabel(ctx context.Context, sourceID int64, label string) (ICloudHMEAlias, error) {
	var email string
	err := s.pool.QueryRow(ctx, `
		SELECT email FROM icloud_hme_aliases
		WHERE source_account_id = ? AND label = ? AND apple_status <> 'deleted'
		ORDER BY created_at DESC LIMIT 1
	`, sourceID, strings.TrimSpace(label)).Scan(&email)
	if err != nil {
		return ICloudHMEAlias{}, err
	}
	return s.GetICloudHMEAlias(ctx, email)
}

func (s *Store) UpdateICloudHMEAliasLifecycle(ctx context.Context, email, status string) error {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "active" && status != "inactive" && status != "deleted" && status != "unknown" {
		return errors.New("隐藏邮箱状态非法")
	}
	active := status == "active"
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET active = ?, apple_status = ?,
		    deactivated_at = CASE WHEN ? = 'inactive' THEN COALESCE(deactivated_at, CURRENT_TIMESTAMP) WHEN ? = 'active' THEN NULL ELSE deactivated_at END,
		    deleted_at = CASE WHEN ? = 'deleted' THEN COALESCE(deleted_at, CURRENT_TIMESTAMP) ELSE deleted_at END,
		    last_synced_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
	`, active, status, status, status, status, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("store: update iCloud HME lifecycle: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("隐藏邮箱不存在")
	}
	return nil
}

func (s *Store) AddICloudHMEAudit(ctx context.Context, entry ICloudHMEAuditLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO icloud_hme_audit_logs (actor, action, target_type, target, result, error_code, message)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, strings.TrimSpace(entry.Actor), strings.TrimSpace(entry.Action), strings.TrimSpace(entry.TargetType),
		strings.TrimSpace(entry.Target), strings.TrimSpace(entry.Result), strings.TrimSpace(entry.ErrorCode),
		truncateICloudHMEAuditMessage(entry.Message))
	if err != nil {
		return fmt.Errorf("store: add iCloud HME audit: %w", err)
	}
	return nil
}

func truncateICloudHMEAuditMessage(value string) string {
	value = sanitizeICloudHMEStoredMessage(value)
	if len([]rune(value)) <= 500 {
		return value
	}
	return string([]rune(value)[:500])
}

var iCloudHMESensitiveErrorRE = regexp.MustCompile(`(?i)(trusttokens|x-apple|webauth|set-cookie|authorization|bearer|cookie|scnt|session[_-]?id|dsid|password|验证码|otp["'=:\s])`)

func sanitizeICloudHMEStoredMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if iCloudHMESensitiveErrorRE.MatchString(value) || (strings.Contains(value, "HTTP ") && strings.Contains(value, "{")) {
		return "Apple 会话异常，请重新验证"
	}
	return value
}
