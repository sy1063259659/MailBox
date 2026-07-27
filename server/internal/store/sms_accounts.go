package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"gptbox-server/internal/secure"
)

type SMSAccount struct {
	ID                   int64      `json:"id"`
	Phone                string     `json:"phone"`
	ProviderHost         string     `json:"providerHost"`
	Remark               string     `json:"remark"`
	LinkedMailboxType    string     `json:"linkedMailboxType"`
	LinkedMailboxEmail   string     `json:"linkedMailboxEmail"`
	LastCheckedAt        *time.Time `json:"lastCheckedAt,omitempty"`
	LastError            string     `json:"lastError,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	ReceiveURLConfigured bool       `json:"receiveUrlConfigured"`
}

type SMSAccountInput struct {
	Phone        string
	ReceiveURL   string
	ProviderHost string
}

type SMSMailboxReference struct {
	Type  string `json:"type"`
	Email string `json:"email"`
}

func (s *Store) ListSMSAccounts(ctx context.Context) ([]SMSAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, phone, provider_host, remark, linked_mailbox_type, linked_mailbox_email,
		       last_checked_at, last_error, created_at, updated_at, receive_url_encrypted <> ''
		FROM sms_accounts
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list SMS accounts: %w", err)
	}
	defer rows.Close()

	accounts := []SMSAccount{}
	for rows.Next() {
		var account SMSAccount
		if err := rows.Scan(
			&account.ID, &account.Phone, &account.ProviderHost, &account.Remark,
			&account.LinkedMailboxType, &account.LinkedMailboxEmail,
			&account.LastCheckedAt, &account.LastError, &account.CreatedAt,
			&account.UpdatedAt, &account.ReceiveURLConfigured,
		); err != nil {
			return nil, fmt.Errorf("store: scan SMS account: %w", err)
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) ImportSMSAccounts(ctx context.Context, inputs []SMSAccountInput, overwrite bool) (ImportResult, error) {
	if len(inputs) == 0 {
		return ImportResult{}, errors.New("没有可导入的接码账号")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ImportResult{}, fmt.Errorf("store: begin SMS import: %w", err)
	}
	defer tx.Rollback(ctx)

	if overwrite {
		if _, err := tx.Exec(ctx, `DELETE FROM sms_accounts`); err != nil {
			return ImportResult{}, fmt.Errorf("store: clear SMS accounts: %w", err)
		}
	}

	result := ImportResult{}
	for _, input := range inputs {
		encryptedURL, err := secure.EncryptString(s.tokenKey, input.ReceiveURL)
		if err != nil {
			return ImportResult{}, fmt.Errorf("store: encrypt SMS receive URL: %w", err)
		}
		var inserted bool
		err = tx.QueryRow(ctx, `
			INSERT INTO sms_accounts (phone, receive_url_encrypted, provider_host)
			VALUES ($1, $2, $3)
			ON CONFLICT (phone) DO UPDATE SET
				receive_url_encrypted = EXCLUDED.receive_url_encrypted,
				provider_host = EXCLUDED.provider_host,
				last_error = '',
				updated_at = now()
			RETURNING (xmax = 0)
		`, input.Phone, encryptedURL, input.ProviderHost).Scan(&inserted)
		if err != nil {
			return ImportResult{}, fmt.Errorf("store: import SMS account %s: %w", input.Phone, err)
		}
		if inserted {
			result.Imported++
		} else {
			result.Updated++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ImportResult{}, fmt.Errorf("store: commit SMS import: %w", err)
	}
	return result, nil
}

func (s *Store) UpdateSMSRemark(ctx context.Context, phone string, remark string) (SMSAccount, error) {
	var account SMSAccount
	err := s.pool.QueryRow(ctx, `
		UPDATE sms_accounts SET remark = $2, updated_at = now()
		WHERE phone = $1
		RETURNING id, phone, provider_host, remark, linked_mailbox_type, linked_mailbox_email,
		          last_checked_at, last_error, created_at, updated_at, receive_url_encrypted <> ''
	`, phone, remark).Scan(
		&account.ID, &account.Phone, &account.ProviderHost, &account.Remark,
		&account.LinkedMailboxType, &account.LinkedMailboxEmail,
		&account.LastCheckedAt, &account.LastError, &account.CreatedAt,
		&account.UpdatedAt, &account.ReceiveURLConfigured,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SMSAccount{}, errors.New("接码账号不存在")
	}
	if err != nil {
		return SMSAccount{}, fmt.Errorf("store: update SMS remark: %w", err)
	}
	return account, nil
}

func (s *Store) BindSMSMailbox(ctx context.Context, phone string, mailboxType string, email string) (SMSAccount, error) {
	mailboxType = strings.TrimSpace(mailboxType)
	email = strings.ToLower(strings.TrimSpace(email))
	if mailboxType == "" && email == "" {
		return s.updateSMSMailboxBinding(ctx, phone, "", "")
	}
	if !isSMSMailboxType(mailboxType) || email == "" {
		return SMSAccount{}, errors.New("邮箱绑定信息不完整")
	}
	exists, err := s.smsMailboxExists(ctx, mailboxType, email)
	if err != nil {
		return SMSAccount{}, err
	}
	if !exists {
		return SMSAccount{}, errors.New("绑定的邮箱账号不存在")
	}
	return s.updateSMSMailboxBinding(ctx, phone, mailboxType, email)
}

func (s *Store) updateSMSMailboxBinding(ctx context.Context, phone string, mailboxType string, email string) (SMSAccount, error) {
	var account SMSAccount
	err := s.pool.QueryRow(ctx, `
		UPDATE sms_accounts
		SET linked_mailbox_type = $2, linked_mailbox_email = $3, updated_at = now()
		WHERE phone = $1
		RETURNING id, phone, provider_host, remark, linked_mailbox_type, linked_mailbox_email,
		          last_checked_at, last_error, created_at, updated_at, receive_url_encrypted <> ''
	`, phone, mailboxType, email).Scan(
		&account.ID, &account.Phone, &account.ProviderHost, &account.Remark,
		&account.LinkedMailboxType, &account.LinkedMailboxEmail,
		&account.LastCheckedAt, &account.LastError, &account.CreatedAt,
		&account.UpdatedAt, &account.ReceiveURLConfigured,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SMSAccount{}, errors.New("接码账号不存在")
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "idx_sms_accounts_linked_mailbox") {
			return SMSAccount{}, errors.New("该邮箱已绑定其他接码账号")
		}
		return SMSAccount{}, fmt.Errorf("store: bind SMS mailbox: %w", err)
	}
	return account, nil
}

func (s *Store) DeleteSMSAccount(ctx context.Context, phone string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM sms_accounts WHERE phone = $1`, phone)
	if err != nil {
		return fmt.Errorf("store: delete SMS account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("接码账号不存在")
	}
	return nil
}

func (s *Store) GetSMSReceiveURL(ctx context.Context, phone string) (string, error) {
	var encrypted string
	if err := s.pool.QueryRow(ctx, `SELECT receive_url_encrypted FROM sms_accounts WHERE phone = $1`, phone).Scan(&encrypted); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("接码账号不存在")
		}
		return "", fmt.Errorf("store: get SMS receive URL: %w", err)
	}
	value, err := secure.DecryptString(s.tokenKey, encrypted)
	if err != nil {
		return "", errors.New("接码 URL 解密失败")
	}
	return value, nil
}

func (s *Store) UpdateSMSCheckResult(ctx context.Context, phone string, checkErr string) {
	_, _ = s.pool.Exec(ctx, `
		UPDATE sms_accounts SET last_checked_at = now(), last_error = $2, updated_at = now()
		WHERE phone = $1
	`, phone, checkErr)
}

func (s *Store) ListSMSMailboxReferences(ctx context.Context) ([]SMSMailboxReference, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT mailbox_type, email FROM (
			SELECT 'outlook'::text AS mailbox_type, email FROM mail_accounts
			UNION ALL
			SELECT 'icloud'::text AS mailbox_type, email FROM icloud_accounts
			UNION ALL
			SELECT 'icloud_hme'::text AS mailbox_type, email FROM icloud_hme_aliases
		) mailboxes
		ORDER BY mailbox_type, email
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list SMS mailbox references: %w", err)
	}
	defer rows.Close()
	references := []SMSMailboxReference{}
	for rows.Next() {
		var reference SMSMailboxReference
		if err := rows.Scan(&reference.Type, &reference.Email); err != nil {
			return nil, fmt.Errorf("store: scan SMS mailbox reference: %w", err)
		}
		references = append(references, reference)
	}
	return references, rows.Err()
}

func (s *Store) smsMailboxExists(ctx context.Context, mailboxType string, email string) (bool, error) {
	var query string
	switch mailboxType {
	case "outlook":
		query = `SELECT EXISTS(SELECT 1 FROM mail_accounts WHERE lower(email) = $1)`
	case "icloud":
		query = `SELECT EXISTS(SELECT 1 FROM icloud_accounts WHERE lower(email) = $1)`
	case "icloud_hme":
		query = `SELECT EXISTS(SELECT 1 FROM icloud_hme_aliases WHERE lower(email) = $1)`
	default:
		return false, errors.New("不支持的邮箱类型")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, query, email).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: check SMS mailbox: %w", err)
	}
	return exists, nil
}

func isSMSMailboxType(value string) bool {
	return value == "outlook" || value == "icloud" || value == "icloud_hme"
}
