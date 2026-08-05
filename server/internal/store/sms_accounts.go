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

const maxSMSMailboxSlots = 3

type SMSAccount struct {
	ID                   int64               `json:"id"`
	Phone                string              `json:"phone"`
	ProviderHost         string              `json:"providerHost"`
	Remark               string              `json:"remark"`
	Status               string              `json:"status"`
	InvalidAt            *time.Time          `json:"invalidAt,omitempty"`
	LinkedMailboxType    string              `json:"linkedMailboxType"`
	LinkedMailboxEmail   string              `json:"linkedMailboxEmail"`
	LinkedMailboxEmails  []string            `json:"linkedMailboxEmails"`
	LinkedMailboxes      []SMSMailboxBinding `json:"linkedMailboxes"`
	BindingHistory       []SMSMailboxBinding `json:"bindingHistory"`
	DeletedMailboxSlots  int                 `json:"deletedMailboxSlots"`
	OccupiedMailboxSlots int                 `json:"occupiedMailboxSlots"`
	LastCheckedAt        *time.Time          `json:"lastCheckedAt,omitempty"`
	LastError            string              `json:"lastError,omitempty"`
	CreatedAt            time.Time           `json:"createdAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
	ReceiveURLConfigured bool                `json:"receiveUrlConfigured"`
}

type SMSMailboxBinding struct {
	Email     string     `json:"email"`
	BoundAt   time.Time  `json:"boundAt"`
	UnboundAt *time.Time `json:"unboundAt,omitempty"`
	EndReason string     `json:"endReason,omitempty"`
	Active    bool       `json:"active"`
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
		       account.status, account.invalid_at, account.last_checked_at, account.last_error,
		       account.created_at, account.updated_at,
		       account.receive_url_encrypted <> ''
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
			&account.Status, &account.InvalidAt, &account.LastCheckedAt, &account.LastError, &account.CreatedAt,
			&account.UpdatedAt, &account.ReceiveURLConfigured,
		); err != nil {
			return nil, fmt.Errorf("store: scan SMS account: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.populateSMSBindings(ctx, accounts); err != nil {
		return nil, err
	}
	return accounts, nil
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
	var status string
	if err := tx.QueryRow(ctx, `SELECT id, status FROM sms_accounts WHERE phone = $1 FOR UPDATE`, phone).Scan(&accountID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SMSAccount{}, errors.New("接码账号不存在")
		}
		return SMSAccount{}, fmt.Errorf("store: find SMS account: %w", err)
	}
	if len(normalized) > 0 && status != "active" {
		return SMSAccount{}, errors.New("失效接码账号不能绑定隐藏邮箱")
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
	var deletedSlots int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM sms_account_binding_history
		WHERE sms_account_id = $1 AND end_reason = 'mailbox_deleted'
	`, accountID).Scan(&deletedSlots); err != nil {
		return SMSAccount{}, fmt.Errorf("store: count deleted SMS mailbox slots: %w", err)
	}
	remainingSlots := remainingSMSMailboxSlots(0, deletedSlots)
	if len(normalized) > remainingSlots {
		return SMSAccount{}, fmt.Errorf("该接码账号已有 %d 个邮箱删除快照，最多还能绑定 %d 个隐藏邮箱", deletedSlots, remainingSlots)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE sms_account_binding_history
		SET unbound_at = COALESCE(unbound_at, now()), end_reason = 'manual_unbind'
		WHERE sms_account_id = $1 AND unbound_at IS NULL
		  AND NOT (mailbox_email = ANY($2::text[]))
	`, accountID, normalized); err != nil {
		return SMSAccount{}, fmt.Errorf("store: snapshot cleared SMS bindings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM sms_account_bindings
		WHERE sms_account_id = $1
		  AND NOT (mailbox_email = ANY($2::text[]))
	`, accountID, normalized); err != nil {
		return SMSAccount{}, fmt.Errorf("store: clear SMS bindings: %w", err)
	}
	for _, email := range normalized {
		if _, err := tx.Exec(ctx, `
			INSERT INTO sms_account_bindings (sms_account_id, mailbox_email) VALUES ($1, $2)
			ON CONFLICT (sms_account_id, mailbox_email) DO NOTHING
		`, accountID, email); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "sms_account_bindings_mailbox_email_key") {
				return SMSAccount{}, fmt.Errorf("隐藏邮箱 %s 已绑定其他接码账号", email)
			}
			return SMSAccount{}, fmt.Errorf("store: bind hidden mailbox: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO sms_account_binding_history (sms_account_id, mailbox_email, bound_at)
			SELECT binding.sms_account_id, binding.mailbox_email, binding.created_at
			FROM sms_account_bindings binding
			WHERE binding.sms_account_id = $1 AND binding.mailbox_email = $2
			  AND NOT EXISTS (
			    SELECT 1 FROM sms_account_binding_history history
			    WHERE history.sms_account_id = binding.sms_account_id
			      AND lower(history.mailbox_email) = lower(binding.mailbox_email)
			      AND history.unbound_at IS NULL
			  )
		`, accountID, email); err != nil {
			return SMSAccount{}, fmt.Errorf("store: snapshot SMS binding: %w", err)
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

func (s *Store) AssignSMSMailbox(ctx context.Context, email string, phone string) ([]SMSAccount, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	phone = strings.TrimSpace(phone)
	if email == "" {
		return nil, errors.New("隐藏邮箱不能为空")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin SMS mailbox assignment: %w", err)
	}
	defer tx.Rollback(ctx)

	var canonicalEmail string
	if err := tx.QueryRow(ctx, `
		SELECT email FROM icloud_hme_aliases
		WHERE lower(email) = $1 AND apple_status <> 'deleted'
		FOR UPDATE
	`, email).Scan(&canonicalEmail); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("iCloud 隐藏邮箱不存在")
		}
		return nil, fmt.Errorf("store: find hidden mailbox: %w", err)
	}

	if phone == "" {
		if _, err := tx.Exec(ctx, `
			UPDATE sms_account_binding_history history
			SET unbound_at = COALESCE(history.unbound_at, now()), end_reason = 'manual_unbind'
			FROM sms_account_bindings binding
			WHERE binding.mailbox_email = $1
			  AND history.sms_account_id = binding.sms_account_id
			  AND lower(history.mailbox_email) = lower(binding.mailbox_email)
			  AND history.unbound_at IS NULL
		`, canonicalEmail); err != nil {
			return nil, fmt.Errorf("store: snapshot unbound hidden mailbox: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sms_account_bindings WHERE mailbox_email = $1`, canonicalEmail); err != nil {
			return nil, fmt.Errorf("store: unbind hidden mailbox: %w", err)
		}
	} else {
		var accountID int64
		var status string
		if err := tx.QueryRow(ctx, `
			SELECT id, status FROM sms_accounts WHERE phone = $1 FOR UPDATE
		`, phone).Scan(&accountID, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errors.New("接码账号不存在")
			}
			return nil, fmt.Errorf("store: find SMS account: %w", err)
		}
		if status != "active" {
			return nil, errors.New("失效接码账号不能绑定隐藏邮箱")
		}

		var currentAccountID int64
		if err := tx.QueryRow(ctx, `
			SELECT sms_account_id FROM sms_account_bindings WHERE mailbox_email = $1
		`, canonicalEmail).Scan(&currentAccountID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("store: find current SMS binding: %w", err)
		}
		if currentAccountID != accountID {
			var occupiedSlots int
			if err := tx.QueryRow(ctx, `
				SELECT
				  (SELECT count(*) FROM sms_account_bindings WHERE sms_account_id = $1) +
				  (SELECT count(*) FROM sms_account_binding_history WHERE sms_account_id = $1 AND end_reason = 'mailbox_deleted')
			`, accountID).Scan(&occupiedSlots); err != nil {
				return nil, fmt.Errorf("store: count occupied SMS mailbox slots: %w", err)
			}
			if occupiedSlots >= maxSMSMailboxSlots {
				return nil, fmt.Errorf("该接码账号的 %d 个邮箱名额已占满", maxSMSMailboxSlots)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE sms_account_binding_history history
				SET unbound_at = COALESCE(history.unbound_at, now()), end_reason = 'reassigned'
				FROM sms_account_bindings binding
				WHERE binding.mailbox_email = $1
				  AND history.sms_account_id = binding.sms_account_id
				  AND lower(history.mailbox_email) = lower(binding.mailbox_email)
				  AND history.unbound_at IS NULL
			`, canonicalEmail); err != nil {
				return nil, fmt.Errorf("store: snapshot replaced SMS binding: %w", err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM sms_account_bindings WHERE mailbox_email = $1`, canonicalEmail); err != nil {
				return nil, fmt.Errorf("store: replace SMS binding: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO sms_account_bindings (sms_account_id, mailbox_email) VALUES ($1, $2)
			`, accountID, canonicalEmail); err != nil {
				return nil, fmt.Errorf("store: assign hidden mailbox: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO sms_account_binding_history (sms_account_id, mailbox_email, bound_at)
				SELECT sms_account_id, mailbox_email, created_at
				FROM sms_account_bindings
				WHERE sms_account_id = $1 AND mailbox_email = $2
			`, accountID, canonicalEmail); err != nil {
				return nil, fmt.Errorf("store: snapshot assigned hidden mailbox: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit SMS mailbox assignment: %w", err)
	}
	return s.ListSMSAccounts(ctx)
}

func (s *Store) UpdateSMSStatus(ctx context.Context, phone string, status string) (SMSAccount, error) {
	if status != "active" && status != "invalid" {
		return SMSAccount{}, errors.New("接码状态无效")
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE sms_accounts
		SET status = $2,
		    invalid_at = CASE WHEN $2 = 'invalid' THEN COALESCE(invalid_at, now()) ELSE NULL END,
		    updated_at = now()
		WHERE phone = $1
	`, phone, status)
	if err != nil {
		return SMSAccount{}, fmt.Errorf("store: update SMS status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return SMSAccount{}, errors.New("接码账号不存在")
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
	var status string
	if err := s.pool.QueryRow(ctx, `
		SELECT receive_url_encrypted, status FROM sms_accounts WHERE phone = $1
	`, phone).Scan(&encrypted, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("接码账号不存在")
		}
		return "", fmt.Errorf("store: get SMS receive URL: %w", err)
	}
	if status != "active" {
		return "", errors.New("接码账号已失效")
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
		       account.status, account.invalid_at, account.last_checked_at, account.last_error,
		       account.created_at, account.updated_at,
		       account.receive_url_encrypted <> ''
		FROM sms_accounts account
		WHERE account.phone = $1
	`, phone).Scan(
		&account.ID, &account.Phone, &account.ProviderHost, &account.Remark,
		&account.Status, &account.InvalidAt, &account.LastCheckedAt, &account.LastError, &account.CreatedAt,
		&account.UpdatedAt, &account.ReceiveURLConfigured,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SMSAccount{}, errors.New("接码账号不存在")
	}
	if err != nil {
		return SMSAccount{}, fmt.Errorf("store: get SMS account: %w", err)
	}
	accounts := []SMSAccount{account}
	if err := s.populateSMSBindings(ctx, accounts); err != nil {
		return SMSAccount{}, err
	}
	account = accounts[0]
	return account, nil
}

func (s *Store) populateSMSBindings(ctx context.Context, accounts []SMSAccount) error {
	if len(accounts) == 0 {
		return nil
	}
	indexByID := make(map[int64]int, len(accounts))
	ids := make([]int64, 0, len(accounts))
	for index := range accounts {
		accounts[index].LinkedMailboxEmails = []string{}
		accounts[index].LinkedMailboxes = []SMSMailboxBinding{}
		accounts[index].BindingHistory = []SMSMailboxBinding{}
		indexByID[accounts[index].ID] = index
		ids = append(ids, accounts[index].ID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT sms_account_id, mailbox_email, created_at
		FROM sms_account_bindings
		WHERE sms_account_id = ANY($1::bigint[])
		ORDER BY sms_account_id, created_at, mailbox_email
	`, ids)
	if err != nil {
		return fmt.Errorf("store: list SMS bindings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var accountID int64
		var binding SMSMailboxBinding
		if err := rows.Scan(&accountID, &binding.Email, &binding.BoundAt); err != nil {
			return fmt.Errorf("store: scan SMS binding: %w", err)
		}
		index, exists := indexByID[accountID]
		if !exists {
			continue
		}
		accounts[index].LinkedMailboxEmails = append(accounts[index].LinkedMailboxEmails, binding.Email)
		binding.Active = true
		accounts[index].LinkedMailboxes = append(accounts[index].LinkedMailboxes, binding)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: read SMS bindings: %w", err)
	}
	historyRows, err := s.pool.Query(ctx, `
		SELECT sms_account_id, mailbox_email, bound_at, unbound_at, end_reason
		FROM sms_account_binding_history
		WHERE sms_account_id = ANY($1::bigint[])
		ORDER BY sms_account_id, bound_at DESC, id DESC
	`, ids)
	if err != nil {
		return fmt.Errorf("store: list SMS binding history: %w", err)
	}
	defer historyRows.Close()
	for historyRows.Next() {
		var accountID int64
		var binding SMSMailboxBinding
		if err := historyRows.Scan(&accountID, &binding.Email, &binding.BoundAt, &binding.UnboundAt, &binding.EndReason); err != nil {
			return fmt.Errorf("store: scan SMS binding history: %w", err)
		}
		index, exists := indexByID[accountID]
		if !exists {
			continue
		}
		binding.Active = binding.UnboundAt == nil
		accounts[index].BindingHistory = append(accounts[index].BindingHistory, binding)
		if binding.EndReason == "mailbox_deleted" {
			accounts[index].DeletedMailboxSlots++
		}
	}
	if err := historyRows.Err(); err != nil {
		return fmt.Errorf("store: read SMS binding history: %w", err)
	}
	for index := range accounts {
		accounts[index].OccupiedMailboxSlots = len(accounts[index].LinkedMailboxes) + accounts[index].DeletedMailboxSlots
		setSMSLegacyBindingFields(&accounts[index])
	}
	return nil
}

func normalizeSMSMailboxEmails(emails []string) ([]string, error) {
	if len(emails) > maxSMSMailboxSlots {
		return nil, fmt.Errorf("一个接码账号最多绑定 %d 个 iCloud 隐藏邮箱", maxSMSMailboxSlots)
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
	if len(normalized) > maxSMSMailboxSlots {
		return nil, fmt.Errorf("一个接码账号最多绑定 %d 个 iCloud 隐藏邮箱", maxSMSMailboxSlots)
	}
	return normalized, nil
}

func remainingSMSMailboxSlots(active, deleted int) int {
	remaining := maxSMSMailboxSlots - active - deleted
	if remaining < 0 {
		return 0
	}
	return remaining
}

func setSMSLegacyBindingFields(account *SMSAccount) {
	account.LinkedMailboxType = ""
	account.LinkedMailboxEmail = ""
	if len(account.LinkedMailboxEmails) > 0 {
		account.LinkedMailboxType = "icloud_hme"
		account.LinkedMailboxEmail = account.LinkedMailboxEmails[0]
	}
}
