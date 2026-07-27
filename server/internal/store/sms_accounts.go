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
	LinkedMailboxEmails  []string   `json:"linkedMailboxEmails"`
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
		SELECT account.id, account.phone, account.provider_host, account.remark,
		       account.last_checked_at, account.last_error, account.created_at, account.updated_at,
		       account.receive_url_encrypted <> '',
		       COALESCE((
		         SELECT array_agg(binding.mailbox_email ORDER BY binding.created_at, binding.mailbox_email)
		         FROM sms_account_bindings binding
		         WHERE binding.sms_account_id = account.id
		       ), ARRAY[]::text[])
		FROM sms_accounts account
		ORDER BY account.created_at DESC, account.id DESC
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
			&account.LastCheckedAt, &account.LastError, &account.CreatedAt,
			&account.UpdatedAt, &account.ReceiveURLConfigured, &account.LinkedMailboxEmails,
		); err != nil {
			return nil, fmt.Errorf("store: scan SMS account: %w", err)
		}
		setSMSLegacyBindingFields(&account)
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
	result, err := s.pool.Exec(ctx, `
		UPDATE sms_accounts SET remark = $2, updated_at = now()
		WHERE phone = $1
	`, phone, remark)
	if err != nil {
		return SMSAccount{}, fmt.Errorf("store: update SMS remark: %w", err)
	}
	if result.RowsAffected() == 0 {
		return SMSAccount{}, errors.New("接码账号不存在")
	}
	return s.getSMSAccount(ctx, phone)
}

func (s *Store) BindSMSMailboxes(ctx context.Context, phone string, emails []string) (SMSAccount, error) {
	normalized, err := normalizeSMSMailboxEmails(emails)
	if err != nil {
		return SMSAccount{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SMSAccount{}, fmt.Errorf("store: begin SMS binding: %w", err)
	}
	defer tx.Rollback(ctx)

	var accountID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM sms_accounts WHERE phone = $1 FOR UPDATE`, phone).Scan(&accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SMSAccount{}, errors.New("接码账号不存在")
		}
		return SMSAccount{}, fmt.Errorf("store: find SMS account: %w", err)
	}
	if len(normalized) > 0 {
		var existing int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM icloud_hme_aliases WHERE lower(email) = ANY($1::text[])
		`, normalized).Scan(&existing); err != nil {
			return SMSAccount{}, fmt.Errorf("store: check hidden mailboxes: %w", err)
		}
		if existing != len(normalized) {
			return SMSAccount{}, errors.New("绑定的 iCloud 隐藏邮箱不存在")
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sms_account_bindings WHERE sms_account_id = $1`, accountID); err != nil {
		return SMSAccount{}, fmt.Errorf("store: clear SMS bindings: %w", err)
	}
	for _, email := range normalized {
		if _, err := tx.Exec(ctx, `
			INSERT INTO sms_account_bindings (sms_account_id, mailbox_email) VALUES ($1, $2)
		`, accountID, email); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "sms_account_bindings_mailbox_email_key") {
				return SMSAccount{}, fmt.Errorf("隐藏邮箱 %s 已绑定其他接码账号", email)
			}
			return SMSAccount{}, fmt.Errorf("store: bind hidden mailbox: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE sms_accounts
		SET linked_mailbox_type = '', linked_mailbox_email = '', updated_at = now()
		WHERE id = $1
	`, accountID); err != nil {
		return SMSAccount{}, fmt.Errorf("store: update SMS account: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SMSAccount{}, fmt.Errorf("store: commit SMS binding: %w", err)
	}
	return s.getSMSAccount(ctx, phone)
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
		SELECT 'icloud_hme'::text, email
		FROM icloud_hme_aliases
		WHERE apple_status <> 'deleted'
		ORDER BY email
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

func (s *Store) getSMSAccount(ctx context.Context, phone string) (SMSAccount, error) {
	var account SMSAccount
	err := s.pool.QueryRow(ctx, `
		SELECT account.id, account.phone, account.provider_host, account.remark,
		       account.last_checked_at, account.last_error, account.created_at, account.updated_at,
		       account.receive_url_encrypted <> '',
		       COALESCE((
		         SELECT array_agg(binding.mailbox_email ORDER BY binding.created_at, binding.mailbox_email)
		         FROM sms_account_bindings binding
		         WHERE binding.sms_account_id = account.id
		       ), ARRAY[]::text[])
		FROM sms_accounts account
		WHERE account.phone = $1
	`, phone).Scan(
		&account.ID, &account.Phone, &account.ProviderHost, &account.Remark,
		&account.LastCheckedAt, &account.LastError, &account.CreatedAt,
		&account.UpdatedAt, &account.ReceiveURLConfigured, &account.LinkedMailboxEmails,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SMSAccount{}, errors.New("接码账号不存在")
	}
	if err != nil {
		return SMSAccount{}, fmt.Errorf("store: get SMS account: %w", err)
	}
	setSMSLegacyBindingFields(&account)
	return account, nil
}

func normalizeSMSMailboxEmails(emails []string) ([]string, error) {
	if len(emails) > 3 {
		return nil, errors.New("一个接码账号最多绑定 3 个 iCloud 隐藏邮箱")
	}
	normalized := make([]string, 0, len(emails))
	seen := make(map[string]struct{}, len(emails))
	for _, value := range emails {
		email := strings.ToLower(strings.TrimSpace(value))
		if email == "" {
			return nil, errors.New("隐藏邮箱不能为空")
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		normalized = append(normalized, email)
	}
	if len(normalized) > 3 {
		return nil, errors.New("一个接码账号最多绑定 3 个 iCloud 隐藏邮箱")
	}
	return normalized, nil
}

func setSMSLegacyBindingFields(account *SMSAccount) {
	account.LinkedMailboxType = ""
	account.LinkedMailboxEmail = ""
	if len(account.LinkedMailboxEmails) > 0 {
		account.LinkedMailboxType = "icloud_hme"
		account.LinkedMailboxEmail = account.LinkedMailboxEmails[0]
	}
}
