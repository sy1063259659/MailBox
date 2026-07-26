package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type ICloudHMEAutomationSettings struct {
	Enabled              bool       `json:"enabled"`
	TargetAvailableCount int        `json:"targetAvailableCount"`
	TargetGroup          string     `json:"targetGroup"`
	LabelPrefix          string     `json:"labelPrefix"`
	PublicMailOrigin     string     `json:"publicMailOrigin"`
	AvailableCount       int        `json:"availableCount"`
	ReservedCount        int        `json:"reservedCount"`
	SoldCount            int        `json:"soldCount"`
	PendingCount         int        `json:"pendingCount"`
	NextCreateAt         *time.Time `json:"nextCreateAt,omitempty"`
	LastSuccessAt        *time.Time `json:"lastSuccessAt,omitempty"`
	LastLimitAt          *time.Time `json:"lastLimitAt,omitempty"`
	UpdatedBy            string     `json:"updatedBy"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type ICloudHMEAutomationEvent struct {
	ID              int64      `json:"id"`
	SourceAccountID *int64     `json:"sourceAccountId,omitempty"`
	SourceName      string     `json:"sourceName,omitempty"`
	EventType       string     `json:"eventType"`
	Result          string     `json:"result"`
	ErrorCode       string     `json:"errorCode,omitempty"`
	Message         string     `json:"message,omitempty"`
	NextAttemptAt   *time.Time `json:"nextAttemptAt,omitempty"`
	RetryCount      int        `json:"retryCount"`
	ProbeStage      int        `json:"probeStage"`
	IntervalSeconds int        `json:"intervalSeconds"`
	RecoverySeconds int        `json:"recoverySeconds"`
	TargetMinSecs   int        `json:"targetIntervalMinSeconds"`
	TargetMaxSecs   int        `json:"targetIntervalMaxSeconds"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type ICloudHMEProbeState struct {
	Stage               int
	SuccessStreak       int
	SuccessTarget       int
	StableStage         int
	RecoveryMode        bool
	LimitStartedAt      *time.Time
	LastCreatedAt       *time.Time
	LastLimitStage      int
	LastIntervalSeconds int
	LastRecoverySeconds int
}

type ICloudHMEProbeTransition struct {
	AttemptStage    int
	Stage           int
	SuccessStreak   int
	SuccessTarget   int
	StableStage     int
	RecoveryMode    bool
	IntervalSeconds int
	RecoverySeconds int
}

type ICloudHMEProbeEventMetrics struct {
	Stage           int
	IntervalSeconds int
	RecoverySeconds int
	TargetMinSecs   int
	TargetMaxSecs   int
}

type ICloudHMEProbeLimit struct {
	Next            time.Time
	CooldownLevel   int
	Stage           int
	IntervalSeconds int
}

func iCloudHMEAutomationCooldownDelay(level int) time.Duration {
	delays := [...]time.Duration{
		5 * time.Minute,
		8 * time.Minute,
		12 * time.Minute,
		20 * time.Minute,
		30 * time.Minute,
		45 * time.Minute,
	}
	if level < 1 {
		level = 1
	}
	if level > len(delays) {
		level = len(delays)
	}
	return delays[level-1]
}

func (s *Store) GetICloudHMEAutomation(ctx context.Context) (ICloudHMEAutomationSettings, error) {
	var out ICloudHMEAutomationSettings
	var nextCreateAt, lastSuccessAt, lastLimitAt sqliteTime
	err := s.pool.QueryRow(ctx, `
		SELECT enabled, target_available_count, target_group, label_prefix,
		       public_mail_origin, updated_by, updated_at,
		       (SELECT count(*) FROM icloud_hme_aliases
		        WHERE apple_status = 'active' AND inventory_status = 'available'
		          AND receive_key_encrypted <> ''),
		       (SELECT count(*) FROM icloud_hme_aliases WHERE inventory_status = 'reserved'),
		       (SELECT count(*) FROM icloud_hme_aliases WHERE inventory_status = 'sold'),
		       (SELECT count(*) FROM icloud_hme_create_job_items item
		        JOIN icloud_hme_create_jobs job ON job.id = item.job_id
		        WHERE job.origin = 'automation' AND item.status IN ('pending', 'running')),
		       (SELECT min(next_create_at) FROM icloud_hme_source_accounts
		        WHERE automation_enabled AND status IN ('active', 'cooldown')),
		       (SELECT max(created_at) FROM icloud_hme_automation_events
		        WHERE event_type = 'create' AND result = 'success'),
		       (SELECT max(created_at) FROM icloud_hme_automation_events
		        WHERE error_code = 'icloud_alias_rate_limited')
		FROM icloud_hme_automation_settings WHERE id = 1
	`).Scan(
		&out.Enabled, &out.TargetAvailableCount, &out.TargetGroup, &out.LabelPrefix,
		&out.PublicMailOrigin, &out.UpdatedBy, &out.UpdatedAt,
		&out.AvailableCount, &out.ReservedCount,
		&out.SoldCount, &out.PendingCount, &nextCreateAt, &lastSuccessAt, &lastLimitAt,
	)
	if err != nil {
		return out, err
	}
	out.NextCreateAt = nextCreateAt.value
	out.LastSuccessAt = lastSuccessAt.value
	out.LastLimitAt = lastLimitAt.value
	return out, nil
}

func (s *Store) UpdateICloudHMEAutomation(
	ctx context.Context,
	enabled bool,
	target int,
	group, prefix, publicMailOrigin, actor string,
) (ICloudHMEAutomationSettings, error) {
	if target < 0 || target > 10000 {
		return ICloudHMEAutomationSettings{}, errors.New("目标库存必须在 0 到 10000 之间")
	}
	group = normalizeICloudHMEGroup(group)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "MailBox"
	}
	if len([]rune(prefix)) > 80 {
		return ICloudHMEAutomationSettings{}, errors.New("标签前缀最多 80 个字符")
	}
	publicMailOrigin, err := NormalizePublicMailOrigin(publicMailOrigin)
	if err != nil {
		return ICloudHMEAutomationSettings{}, err
	}
	if _, err := s.CreateICloudHMEGroup(ctx, group); err != nil {
		return ICloudHMEAutomationSettings{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_automation_settings
		SET enabled = ?, target_available_count = ?, target_group = ?,
		    label_prefix = ?, public_mail_origin = ?,
		    updated_by = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`, enabled, target, group, prefix, publicMailOrigin, strings.TrimSpace(actor)); err != nil {
		return ICloudHMEAutomationSettings{}, fmt.Errorf("store: update iCloud HME automation: %w", err)
	}
	return s.GetICloudHMEAutomation(ctx)
}

// NormalizePublicMailOrigin validates the operator-supplied receive URL origin.
// An empty value is allowed and means "use whatever origin serves the admin UI",
// so a deployment that never sets it keeps working.
func NormalizePublicMailOrigin(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 255 {
		return "", errors.New("取件 URL 域名最多 255 个字符")
	}
	// Accept a bare host as a convenience and default it to https.
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("取件 URL 域名格式不正确")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("取件 URL 域名必须以 http:// 或 https:// 开头")
	}
	if parsed.Host == "" {
		return "", errors.New("取件 URL 域名缺少主机名")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("取件 URL 域名不能包含查询参数或片段")
	}
	// Keep only scheme://host[:port]; the path is supplied by the caller.
	return strings.TrimSuffix((&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String(), "/"), nil
}

func (s *Store) ListICloudHMEAutomationEvents(ctx context.Context, limit int) ([]ICloudHMEAutomationEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT event.id, event.source_account_id, COALESCE(source.name, ''), event.event_type,
		       event.result, event.error_code, event.message, event.next_attempt_at,
		       event.retry_count, event.probe_stage, event.interval_seconds,
		       event.recovery_seconds, event.target_interval_min_seconds,
		       event.target_interval_max_seconds, event.created_at
		FROM icloud_hme_automation_events event
		LEFT JOIN icloud_hme_source_accounts source ON source.id = event.source_account_id
		ORDER BY event.created_at DESC, event.id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ICloudHMEAutomationEvent{}
	for rows.Next() {
		var event ICloudHMEAutomationEvent
		if err := rows.Scan(&event.ID, &event.SourceAccountID, &event.SourceName, &event.EventType,
			&event.Result, &event.ErrorCode, &event.Message, &event.NextAttemptAt,
			&event.RetryCount, &event.ProbeStage, &event.IntervalSeconds,
			&event.RecoverySeconds, &event.TargetMinSecs, &event.TargetMaxSecs,
			&event.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (s *Store) AddICloudHMEAutomationEvent(
	ctx context.Context,
	sourceID *int64,
	itemID *int64,
	eventType, result, code, message string,
	next *time.Time,
	retry int,
	metrics ICloudHMEProbeEventMetrics,
) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO icloud_hme_automation_events
			(source_account_id, job_item_id, event_type, result, error_code, message,
			 next_attempt_at, retry_count, probe_stage, interval_seconds,
			 recovery_seconds, target_interval_min_seconds, target_interval_max_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sourceID, itemID, eventType, result, strings.TrimSpace(code),
		truncateICloudHMEAuditMessage(message), next, retry, metrics.Stage,
		metrics.IntervalSeconds, metrics.RecoverySeconds, metrics.TargetMinSecs,
		metrics.TargetMaxSecs)
	return err
}

func (s *Store) UpdateICloudHMEInventoryStatus(ctx context.Context, emails []string, status string) error {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "available" && status != "reserved" && status != "sold" {
		return errors.New("库存状态必须为 available、reserved 或 sold")
	}
	normalized := uniqueNormalizedEmails(emails)
	if len(normalized) == 0 {
		return errors.New("未选择隐藏邮箱")
	}
	args := append([]any{status, status}, stringArgs(normalized)...)
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET inventory_status = ?,
		    sold_at = CASE WHEN ? = 'sold' THEN COALESCE(sold_at, CURRENT_TIMESTAMP) ELSE NULL END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) IN (`+sqlInPlaceholders(len(normalized))+`)
	`, args...)
	if err != nil {
		return err
	}
	if int(result.RowsAffected()) != len(normalized) {
		return errors.New("部分隐藏邮箱不存在")
	}
	return nil
}

func (s *Store) CreateICloudHMEAutomationJob(ctx context.Context, settings ICloudHMEAutomationSettings, count int) (ICloudHMECreateJob, error) {
	if count > 20 {
		count = 20
	}
	return s.CreateICloudHMEJob(ctx, ICloudHMECreateJobInput{
		Mode: ICloudHMEJobModePool, LabelPrefix: settings.LabelPrefix,
		GroupName: settings.TargetGroup, Count: count, CreatedBy: "automation",
		Origin: "automation",
	})
}

func (s *Store) DeferICloudHMEJobItem(ctx context.Context, itemID int64, sourceID *int64, retryClass, code, message string, next time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_create_job_items
		SET source_account_id = COALESCE(?, source_account_id), status = 'pending',
		    retry_class = ?, error_code = ?, error_message = ?,
		    next_attempt_at = ?, started_at = NULL, finished_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, sourceID, retryClass, code, truncateICloudHMEAuditMessage(message), next, itemID)
	return err
}

func (s *Store) EligibleICloudHMEAutomationSources(ctx context.Context) ([]ICloudHMESourceCredentials, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM icloud_hme_source_accounts
		WHERE automation_enabled
		  AND cookies_encrypted <> ''
		  AND status IN ('active', 'cooldown')
		  AND (next_create_at IS NULL OR next_create_at <= CURRENT_TIMESTAMP)
		ORDER BY alias_total ASC, COALESCE(last_created_at, '1970-01-01 00:00:00') ASC, id ASC
	`)
	if err != nil {
		return nil, err
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
		source, err := s.GetICloudHMESourceCredentials(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	return result, nil
}

func (s *Store) MarkICloudHMEAutomationAttempt(ctx context.Context, sourceID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE icloud_hme_source_accounts SET last_auto_attempt_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, sourceID)
	return err
}

func (s *Store) DelayICloudHMESourceAutomation(ctx context.Context, sourceID int64, next time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET next_create_at = ?, last_error_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, next, sourceID)
	return err
}

func (s *Store) GetICloudHMEProbeState(ctx context.Context, sourceID int64) (ICloudHMEProbeState, error) {
	var state ICloudHMEProbeState
	err := s.pool.QueryRow(ctx, `
		SELECT probe_stage, probe_success_streak, probe_success_target,
		       probe_stable_stage, probe_recovery_mode, probe_limit_started_at,
		       last_created_at, probe_last_limit_stage,
		       probe_last_interval_seconds, probe_last_recovery_seconds
		FROM icloud_hme_source_accounts WHERE id = ?
	`, sourceID).Scan(
		&state.Stage, &state.SuccessStreak, &state.SuccessTarget,
		&state.StableStage, &state.RecoveryMode, &state.LimitStartedAt,
		&state.LastCreatedAt, &state.LastLimitStage,
		&state.LastIntervalSeconds, &state.LastRecoverySeconds,
	)
	return state, err
}

func (s *Store) MarkICloudHMEAutomationSuccess(
	ctx context.Context,
	sourceID int64,
	next time.Time,
	transition ICloudHMEProbeTransition,
) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET status = 'active', status_reason = '', next_create_at = ?,
		    cooldown_level = 0, consecutive_limit_count = 0, last_created_at = CURRENT_TIMESTAMP,
		    probe_stage = ?, probe_success_streak = ?, probe_success_target = ?,
		    probe_stable_stage = ?, probe_recovery_mode = ?,
		    probe_limit_started_at = NULL, probe_last_interval_seconds = ?,
		    probe_last_recovery_seconds = ?,
		    create_window_started_at = CASE
		      WHEN create_window_started_at IS NULL OR create_window_started_at < datetime('now', '-24 hours')
		      THEN CURRENT_TIMESTAMP ELSE create_window_started_at END,
		    create_window_success_count = CASE
		      WHEN create_window_started_at IS NULL OR create_window_started_at < datetime('now', '-24 hours')
		      THEN 1 ELSE create_window_success_count + 1 END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, next, transition.Stage, transition.SuccessStreak,
		transition.SuccessTarget, transition.StableStage, transition.RecoveryMode,
		transition.IntervalSeconds, transition.RecoverySeconds, sourceID)
	return err
}

func (s *Store) MarkICloudHMEAutomationRecovered(ctx context.Context, sourceID int64, next time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET status = 'active', status_reason = '', next_create_at = ?,
		    cooldown_level = 0, consecutive_limit_count = 0,
		    probe_limit_started_at = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, next, sourceID)
	return err
}

func (s *Store) MarkICloudHMEAutomationLimited(ctx context.Context, sourceID int64) (ICloudHMEProbeLimit, error) {
	var result ICloudHMEProbeLimit
	err := s.pool.QueryRow(ctx, `
		UPDATE icloud_hme_source_accounts
		SET cooldown_level = min(cooldown_level + 1, 6),
		    consecutive_limit_count = consecutive_limit_count + 1,
		    last_limit_at = CURRENT_TIMESTAMP, status = 'cooldown',
		    status_reason = 'Apple 暂时限制创建，系统将在冷却后自动探测',
		    probe_last_limit_stage = probe_stage,
		    probe_limit_started_at = COALESCE(probe_limit_started_at, CURRENT_TIMESTAMP),
		    probe_success_streak = 0,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		RETURNING cooldown_level, probe_stage,
		          CASE WHEN last_created_at IS NULL THEN 0
		               ELSE max(0, CAST(strftime('%s', 'now') AS INTEGER) - CAST(strftime('%s', last_created_at) AS INTEGER))
		          END
	`, sourceID).Scan(&result.CooldownLevel, &result.Stage, &result.IntervalSeconds)
	if err != nil {
		return ICloudHMEProbeLimit{}, err
	}
	result.Next = time.Now().Add(iCloudHMEAutomationCooldownDelay(result.CooldownLevel))
	_, err = s.pool.Exec(ctx, `UPDATE icloud_hme_source_accounts SET next_create_at = ? WHERE id = ?`, result.Next, sourceID)
	return result, err
}

func (s *Store) PauseICloudHMESourceAutomation(ctx context.Context, sourceID int64, status, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET automation_enabled = false, status = ?, status_reason = ?,
		    last_error_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, status, truncateICloudHMEAuditMessage(reason), sourceID)
	return err
}

func (s *Store) RecordICloudHMEAutomationProtocolFailure(ctx context.Context) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		UPDATE icloud_hme_automation_settings
		SET error_window_started_at = CASE
		      WHEN error_window_started_at IS NULL OR error_window_started_at < datetime('now', '-1 hour')
		      THEN CURRENT_TIMESTAMP ELSE error_window_started_at END,
		    error_window_count = CASE
		      WHEN error_window_started_at IS NULL OR error_window_started_at < datetime('now', '-1 hour')
		      THEN 1 ELSE error_window_count + 1 END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = 1 RETURNING error_window_count
	`).Scan(&count)
	if err != nil {
		return false, err
	}
	if count < 3 {
		return false, nil
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE icloud_hme_automation_settings
		SET enabled = false, updated_by = 'safety_guard', updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`)
	return err == nil, err
}
