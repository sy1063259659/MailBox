package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ICloudHMEGPTScanTarget struct {
	Email           string
	SourceAccountID int64
	Status          string
	PlusActivatedAt *time.Time
	LastScannedAt   *time.Time
	CreatedAt       time.Time
}

func (s *Store) ListICloudHMEGPTScanTargets(ctx context.Context, dueBefore time.Time) ([]ICloudHMEGPTScanTarget, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT alias.email, alias.source_account_id, alias.gpt_status,
		       alias.gpt_plus_activated_at, alias.gpt_last_scanned_at, alias.created_at
		FROM icloud_hme_aliases alias
		JOIN icloud_hme_source_accounts source ON source.id = alias.source_account_id
		WHERE alias.apple_status = 'active'
		  AND alias.gpt_status <> 'deactivated'
		  AND source.app_password_encrypted <> ''
		  AND (alias.gpt_last_scanned_at IS NULL OR alias.gpt_last_scanned_at <= ?)
		ORDER BY alias.source_account_id, alias.created_at, alias.email
	`, dueBefore)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud HME GPT scan targets: %w", err)
	}
	defer rows.Close()

	targets := []ICloudHMEGPTScanTarget{}
	for rows.Next() {
		var target ICloudHMEGPTScanTarget
		if err := rows.Scan(
			&target.Email, &target.SourceAccountID, &target.Status,
			&target.PlusActivatedAt, &target.LastScannedAt, &target.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan iCloud HME GPT target: %w", err)
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (s *Store) MarkICloudHMEGPTScanned(ctx context.Context, emails []string, scannedAt time.Time, scanErr error) error {
	normalized := uniqueNormalizedEmails(emails)
	if len(normalized) == 0 {
		return nil
	}
	message := ""
	if scanErr != nil {
		message = truncateICloudHMEAuditMessage(scanErr.Error())
	}
	args := append([]any{scannedAt, message}, stringArgs(normalized)...)
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET gpt_last_scanned_at = ?, gpt_scan_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) IN (`+sqlInPlaceholders(len(normalized))+`)
	`, args...)
	return err
}

func (s *Store) RecordICloudHMEGPTPlus(ctx context.Context, email, messageUID string, activatedAt time.Time) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET gpt_status = CASE WHEN gpt_status = 'deactivated' THEN gpt_status ELSE 'plus' END,
		    gpt_plus_activated_at = ?,
		    gpt_plan_message_uid = ?,
		    gpt_scan_error = '',
		    updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
		  AND gpt_status <> 'deactivated'
		  AND (gpt_plus_activated_at IS NULL OR ? < gpt_plus_activated_at)
	`, activatedAt, strings.TrimSpace(messageUID), strings.ToLower(strings.TrimSpace(email)), activatedAt)
	if err != nil {
		return false, fmt.Errorf("store: record iCloud HME GPT Plus: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (s *Store) RecordICloudHMEGPTDeactivated(ctx context.Context, email, messageUID string, deactivatedAt time.Time) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET gpt_status = 'deactivated',
		    gpt_deactivated_at = ?,
		    gpt_deactivation_message_uid = ?,
		    gpt_scan_error = '',
		    updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
		  AND gpt_plus_activated_at IS NOT NULL
		  AND ? >= gpt_plus_activated_at
		  AND (gpt_deactivated_at IS NULL OR ? < gpt_deactivated_at)
	`, deactivatedAt, strings.TrimSpace(messageUID), strings.ToLower(strings.TrimSpace(email)), deactivatedAt, deactivatedAt)
	if err != nil {
		return false, fmt.Errorf("store: record iCloud HME GPT deactivation: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
