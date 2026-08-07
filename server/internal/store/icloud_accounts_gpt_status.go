package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gptbox-server/internal/secure"
)

type ICloudGPTScanTarget struct {
	Email           string
	Key             string
	Status          string
	PlusActivatedAt *time.Time
	LastScannedAt   *time.Time
	CreatedAt       time.Time
}

func (s *Store) ListICloudGPTScanTargets(ctx context.Context, dueBefore time.Time, limit int) ([]ICloudGPTScanTarget, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT email, access_key_encrypted, gpt_status, gpt_plus_activated_at,
		       gpt_last_scanned_at, created_at
		FROM icloud_accounts
		WHERE gpt_status <> 'deactivated'
		  AND (gpt_last_scanned_at IS NULL OR gpt_last_scanned_at <= $1)
		ORDER BY gpt_last_scanned_at ASC NULLS FIRST, created_at, email
		LIMIT $2
	`, dueBefore, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud GPT scan targets: %w", err)
	}
	defer rows.Close()
	targets := []ICloudGPTScanTarget{}
	for rows.Next() {
		var target ICloudGPTScanTarget
		var encryptedKey string
		if err := rows.Scan(
			&target.Email, &encryptedKey, &target.Status, &target.PlusActivatedAt,
			&target.LastScannedAt, &target.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan iCloud GPT target: %w", err)
		}
		key, err := secure.DecryptString(s.tokenKey, encryptedKey)
		if err != nil {
			return nil, fmt.Errorf("store: decrypt iCloud GPT key: %w", err)
		}
		target.Key = key
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (s *Store) MarkICloudGPTScanned(ctx context.Context, email string, scannedAt time.Time, scanErr error) error {
	message := ""
	if scanErr != nil {
		message = truncateICloudHMEAuditMessage(scanErr.Error())
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE icloud_accounts
		SET gpt_last_scanned_at = $2, gpt_scan_error = $3, updated_at = now()
		WHERE lower(email) = $1
	`, normalizeEmail(email), scannedAt, message)
	return err
}

func (s *Store) RecordICloudGPTPlus(ctx context.Context, email string, messageID int64, activatedAt time.Time) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_accounts
		SET gpt_status = CASE WHEN gpt_status = 'deactivated' THEN gpt_status ELSE 'plus' END,
		    gpt_plus_activated_at = $2,
		    gpt_plan_message_id = $3,
		    gpt_scan_error = '',
		    updated_at = now()
		WHERE lower(email) = $1
		  AND gpt_status <> 'deactivated'
		  AND (gpt_plus_activated_at IS NULL OR $2 < gpt_plus_activated_at)
	`, normalizeEmail(email), activatedAt, messageID)
	if err != nil {
		return false, fmt.Errorf("store: record iCloud GPT Plus: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

func (s *Store) RecordICloudGPTDeactivated(ctx context.Context, email string, messageID int64, deactivatedAt time.Time) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_accounts
		SET gpt_status = 'deactivated',
		    gpt_deactivated_at = $2,
		    gpt_deactivation_message_id = $3,
		    gpt_scan_error = '',
		    updated_at = now()
		WHERE lower(email) = $1
		  AND gpt_plus_activated_at IS NOT NULL
		  AND $2 >= gpt_plus_activated_at
		  AND (gpt_deactivated_at IS NULL OR $2 < gpt_deactivated_at)
	`, strings.ToLower(strings.TrimSpace(email)), deactivatedAt, messageID)
	if err != nil {
		return false, fmt.Errorf("store: record iCloud GPT deactivation: %w", err)
	}
	return result.RowsAffected() > 0, nil
}
