package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gptbox-server/internal/secure"
)

var (
	ErrInvalidICloudHMEReceiveCredentials = errors.New("invalid iCloud HME receive credentials")
	ErrICloudHMEMailboxUnavailable        = errors.New("iCloud HME mailbox unavailable")
	ErrICloudHMEMailServiceUnavailable    = errors.New("iCloud HME mail service unavailable")
)

type ICloudHMEReceiveKeyRecord struct {
	Email     string    `json:"email"`
	Key       string    `json:"key"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Store) GenerateMissingICloudHMEReceiveKeys(ctx context.Context, emails []string) (int, error) {
	normalized := uniqueNormalizedEmails(emails)
	if len(normalized) == 0 {
		return 0, errors.New("未选择隐藏邮箱")
	}
	if len(normalized) > 500 {
		return 0, errors.New("单次最多处理 500 个隐藏邮箱")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin generate receive keys: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT email, receive_key_encrypted
		FROM icloud_hme_aliases
		WHERE lower(email) IN (`+sqlInPlaceholders(len(normalized))+`)
	`, stringArgs(normalized)...)
	if err != nil {
		return 0, fmt.Errorf("store: select aliases for receive keys: %w", err)
	}
	type currentKey struct{ email, encrypted string }
	current := make([]currentKey, 0, len(normalized))
	for rows.Next() {
		var item currentKey
		if err := rows.Scan(&item.email, &item.encrypted); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: scan alias receive key: %w", err)
		}
		current = append(current, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("store: list alias receive keys: %w", err)
	}
	rows.Close()
	if len(current) != len(normalized) {
		return 0, errors.New("部分隐藏邮箱不存在")
	}

	generated := 0
	for _, item := range current {
		if strings.TrimSpace(item.encrypted) != "" {
			continue
		}
		key, encrypted, digest, err := s.newICloudHMEReceiveKey()
		if err != nil {
			return 0, err
		}
		_ = key
		if _, err := tx.Exec(ctx, `
			UPDATE icloud_hme_aliases
			SET receive_key_encrypted = ?, receive_key_digest = ?,
			    receive_key_updated_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			WHERE lower(email) = ?
		`, encrypted, digest, normalizeEmail(item.email)); err != nil {
			return 0, fmt.Errorf("store: save generated receive key: %w", err)
		}
		generated++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit generated receive keys: %w", err)
	}
	return generated, nil
}

func (s *Store) RevealICloudHMEReceiveKey(ctx context.Context, email string) (ICloudHMEReceiveKeyRecord, error) {
	var record ICloudHMEReceiveKeyRecord
	var encrypted string
	var updatedAt sqliteTime
	err := s.pool.QueryRow(ctx, `
		SELECT email, receive_key_encrypted, COALESCE(receive_key_updated_at, updated_at)
		FROM icloud_hme_aliases
		WHERE lower(email) = ?
	`, normalizeEmail(email)).Scan(&record.Email, &encrypted, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return record, errors.New("隐藏邮箱不存在")
	}
	if err != nil {
		return record, fmt.Errorf("store: reveal receive key: %w", err)
	}
	record.UpdatedAt = updatedAt.Time()
	if strings.TrimSpace(encrypted) == "" {
		return record, errors.New("该隐藏邮箱尚未生成收件密钥")
	}
	record.Key, err = secure.DecryptString(s.tokenKey, encrypted)
	if err != nil {
		return ICloudHMEReceiveKeyRecord{}, fmt.Errorf("store: decrypt receive key: %w", err)
	}
	return record, nil
}

func (s *Store) ExportICloudHMEReceiveKeys(ctx context.Context, emails []string) ([]ICloudHMEReceiveKeyRecord, error) {
	normalized := uniqueNormalizedEmails(emails)
	if len(normalized) == 0 {
		return nil, errors.New("未选择隐藏邮箱")
	}
	if len(normalized) > 500 {
		return nil, errors.New("单次最多导出 500 个隐藏邮箱")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT email, receive_key_encrypted, COALESCE(receive_key_updated_at, updated_at)
		FROM icloud_hme_aliases
		WHERE lower(email) IN (`+sqlInPlaceholders(len(normalized))+`)
		ORDER BY created_at DESC, email ASC
	`, stringArgs(normalized)...)
	if err != nil {
		return nil, fmt.Errorf("store: export receive keys: %w", err)
	}
	defer rows.Close()
	records := make([]ICloudHMEReceiveKeyRecord, 0, len(normalized))
	for rows.Next() {
		var record ICloudHMEReceiveKeyRecord
		var encrypted string
		var updatedAt sqliteTime
		if err := rows.Scan(&record.Email, &encrypted, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: scan exported receive key: %w", err)
		}
		record.UpdatedAt = updatedAt.Time()
		if strings.TrimSpace(encrypted) == "" {
			continue
		}
		record.Key, err = secure.DecryptString(s.tokenKey, encrypted)
		if err != nil {
			return nil, fmt.Errorf("store: decrypt exported receive key: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) ResetICloudHMEReceiveKey(ctx context.Context, email string) (ICloudHMEReceiveKeyRecord, error) {
	key, encrypted, digest, err := s.newICloudHMEReceiveKey()
	if err != nil {
		return ICloudHMEReceiveKeyRecord{}, err
	}
	normalized := normalizeEmail(email)
	tag, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET receive_key_encrypted = ?, receive_key_digest = ?,
		    receive_key_updated_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
	`, encrypted, digest, normalized)
	if err != nil {
		return ICloudHMEReceiveKeyRecord{}, fmt.Errorf("store: reset receive key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ICloudHMEReceiveKeyRecord{}, errors.New("隐藏邮箱不存在")
	}
	var record ICloudHMEReceiveKeyRecord
	err = s.pool.QueryRow(ctx, `
		SELECT email, receive_key_updated_at FROM icloud_hme_aliases WHERE lower(email) = ?
	`, normalized).Scan(&record.Email, &record.UpdatedAt)
	if err != nil {
		return ICloudHMEReceiveKeyRecord{}, fmt.Errorf("store: reset receive key: %w", err)
	}
	record.Key = key
	return record, nil
}

func (s *Store) AuthenticateICloudHMEPublicMail(ctx context.Context, email, receiveKey string) (ICloudHMEMailCredentials, error) {
	var credentials ICloudHMEMailCredentials
	var digest, encryptedPassword, status string
	var active bool
	err := s.pool.QueryRow(ctx, `
		SELECT a.email, s.icloud_email, s.app_password_encrypted,
		       a.receive_key_digest, a.apple_status, a.active
		FROM icloud_hme_aliases a
		JOIN icloud_hme_source_accounts s ON s.id = a.source_account_id
		WHERE lower(a.email) = ?
	`, normalizeEmail(email)).Scan(
		&credentials.AliasEmail, &credentials.ICloudEmail, &encryptedPassword,
		&digest, &status, &active,
	)
	if errors.Is(err, sql.ErrNoRows) {
		_ = secure.VerifyReceiveKey(s.tokenKey, receiveKey, strings.Repeat("0", 64))
		return ICloudHMEMailCredentials{}, ErrInvalidICloudHMEReceiveCredentials
	}
	if err != nil {
		return ICloudHMEMailCredentials{}, fmt.Errorf("store: authenticate public HME mail: %w", err)
	}
	if digest == "" {
		digest = strings.Repeat("0", 64)
	}
	validFormat := secure.ValidateReceiveKey(receiveKey) == nil
	validDigest := secure.VerifyReceiveKey(s.tokenKey, receiveKey, digest)
	if !validFormat || !validDigest {
		return ICloudHMEMailCredentials{}, ErrInvalidICloudHMEReceiveCredentials
	}
	if !active || status != "active" {
		return ICloudHMEMailCredentials{}, ErrICloudHMEMailboxUnavailable
	}
	if strings.TrimSpace(encryptedPassword) == "" {
		return ICloudHMEMailCredentials{}, ErrICloudHMEMailServiceUnavailable
	}
	credentials.AppPassword, err = secure.DecryptString(s.tokenKey, encryptedPassword)
	if err != nil {
		return ICloudHMEMailCredentials{}, ErrICloudHMEMailServiceUnavailable
	}
	return credentials, nil
}

func (s *Store) newICloudHMEReceiveKey() (key, encrypted, digest string, err error) {
	key, err = secure.GenerateReceiveKey()
	if err != nil {
		return "", "", "", fmt.Errorf("store: generate receive key: %w", err)
	}
	encrypted, err = secure.EncryptString(s.tokenKey, key)
	if err != nil {
		return "", "", "", fmt.Errorf("store: encrypt receive key: %w", err)
	}
	digest = secure.ReceiveKeyDigest(s.tokenKey, key)
	return key, encrypted, digest, nil
}
