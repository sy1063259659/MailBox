package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mailbox-server/internal/secure"
)

type GPTAccount struct {
	ID                      int64           `json:"id"`
	MailAccountEmail        string          `json:"mailAccountEmail"`
	GPTEmail                string          `json:"gptEmail"`
	AccountID               string          `json:"accountId"`
	OrganizationID          string          `json:"organizationId"`
	AccountName             string          `json:"accountName"`
	AccountStructure        string          `json:"accountStructure"`
	PlanType                string          `json:"planType"`
	AuthFilePlanType        string          `json:"authFilePlanType"`
	SubscriptionActiveUntil *time.Time      `json:"subscriptionActiveUntil,omitempty"`
	HourlyPercentage        *int            `json:"hourlyPercentage,omitempty"`
	HourlyResetTime         *time.Time      `json:"hourlyResetTime,omitempty"`
	HourlyWindowMinutes     *int            `json:"hourlyWindowMinutes,omitempty"`
	HourlyWindowPresent     *bool           `json:"hourlyWindowPresent,omitempty"`
	WeeklyPercentage        *int            `json:"weeklyPercentage,omitempty"`
	WeeklyResetTime         *time.Time      `json:"weeklyResetTime,omitempty"`
	WeeklyWindowMinutes     *int            `json:"weeklyWindowMinutes,omitempty"`
	WeeklyWindowPresent     *bool           `json:"weeklyWindowPresent,omitempty"`
	QuotaRawJSON            json.RawMessage `json:"-"`
	Status                  string          `json:"status"`
	StatusReason            string          `json:"statusReason"`
	RequiresReauth          bool            `json:"requiresReauth"`
	ReauthReason            string          `json:"reauthReason,omitempty"`
	QuotaErrorCode          string          `json:"quotaErrorCode,omitempty"`
	QuotaErrorMessage       string          `json:"quotaErrorMessage,omitempty"`
	QuotaErrorAt            *time.Time      `json:"quotaErrorAt,omitempty"`
	TokenExpiresAt          *time.Time      `json:"tokenExpiresAt,omitempty"`
	TokenUpdatedAt          *time.Time      `json:"tokenUpdatedAt,omitempty"`
	LastRefreshAt           *time.Time      `json:"lastRefreshAt,omitempty"`
	CreatedAt               time.Time       `json:"createdAt"`
	UpdatedAt               time.Time       `json:"updatedAt"`
}

type GPTTokens struct {
	IDToken      string
	AccessToken  string
	RefreshToken string
}

type GPTAccountInput struct {
	MailAccountEmail        string
	GPTEmail                string
	AccountID               string
	OrganizationID          string
	AccountName             string
	AccountStructure        string
	PlanType                string
	AuthFilePlanType        string
	SubscriptionActiveUntil *time.Time
	Tokens                  GPTTokens
	TokenExpiresAt          *time.Time
}

type GPTAccountCredentials struct {
	Account GPTAccount
	Tokens  GPTTokens
}

type GPTQuotaUpdate struct {
	AccountID           string
	OrganizationID      string
	AccountName         string
	AccountStructure    string
	PlanType            string
	AuthFilePlanType    string
	HourlyPercentage    *int
	HourlyResetTime     *time.Time
	HourlyWindowMinutes *int
	HourlyWindowPresent *bool
	WeeklyPercentage    *int
	WeeklyResetTime     *time.Time
	WeeklyWindowMinutes *int
	WeeklyWindowPresent *bool
	QuotaRawJSON        []byte
	Status              string
	StatusReason        string
	RequiresReauth      bool
	ReauthReason        string
	QuotaErrorCode      string
	QuotaErrorMessage   string
	QuotaErrorAt        *time.Time
	LastRefreshAt       *time.Time
}

func (s *Store) ListGPTAccounts(ctx context.Context) ([]GPTAccount, error) {
	rows, err := s.pool.Query(ctx, gptAccountSelectSQL()+` ORDER BY mail_account_email ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list gpt accounts: %w", err)
	}
	defer rows.Close()

	accounts := []GPTAccount{}
	for rows.Next() {
		account, err := scanGPTAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) UpsertGPTAccount(ctx context.Context, input GPTAccountInput) (GPTAccount, error) {
	mailEmail := normalizeEmail(input.MailAccountEmail)
	if mailEmail == "" {
		return GPTAccount{}, errors.New("邮箱账号不能为空")
	}
	if ok, err := s.accountExists(ctx, mailEmail); err != nil {
		return GPTAccount{}, err
	} else if !ok {
		return GPTAccount{}, errors.New("邮箱账号不存在")
	}
	gptEmail := normalizeEmail(input.GPTEmail)
	if gptEmail == "" {
		gptEmail = mailEmail
	}
	if gptEmail != mailEmail {
		return GPTAccount{}, errors.New("GPT 账号邮箱与当前邮箱不一致")
	}

	idEncrypted, err := secure.EncryptString(s.tokenKey, strings.TrimSpace(input.Tokens.IDToken))
	if err != nil {
		return GPTAccount{}, fmt.Errorf("store: encrypt gpt id token: %w", err)
	}
	accessEncrypted, err := secure.EncryptString(s.tokenKey, strings.TrimSpace(input.Tokens.AccessToken))
	if err != nil {
		return GPTAccount{}, fmt.Errorf("store: encrypt gpt access token: %w", err)
	}
	refreshEncrypted, err := secure.EncryptString(s.tokenKey, strings.TrimSpace(input.Tokens.RefreshToken))
	if err != nil {
		return GPTAccount{}, fmt.Errorf("store: encrypt gpt refresh token: %w", err)
	}

	var account GPTAccount
	err = s.pool.QueryRow(ctx, `
		INSERT INTO gpt_accounts (
			mail_account_email, gpt_email, account_id, organization_id, account_name, account_structure,
			plan_type, auth_file_plan_type, subscription_active_until,
			status, status_reason, requires_reauth, reauth_reason,
			id_token_encrypted, access_token_encrypted, refresh_token_encrypted,
			token_expires_at, token_updated_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'unknown', '', false, '', $10, $11, $12, $13, now(), now())
		ON CONFLICT (mail_account_email) DO UPDATE SET
			gpt_email = EXCLUDED.gpt_email,
			account_id = EXCLUDED.account_id,
			organization_id = EXCLUDED.organization_id,
			account_name = EXCLUDED.account_name,
			account_structure = EXCLUDED.account_structure,
			plan_type = EXCLUDED.plan_type,
			auth_file_plan_type = EXCLUDED.auth_file_plan_type,
			subscription_active_until = EXCLUDED.subscription_active_until,
			status = 'unknown',
			status_reason = '',
			requires_reauth = false,
			reauth_reason = '',
			quota_error_code = '',
			quota_error_message = '',
			quota_error_at = NULL,
			id_token_encrypted = EXCLUDED.id_token_encrypted,
			access_token_encrypted = EXCLUDED.access_token_encrypted,
			refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
			token_expires_at = EXCLUDED.token_expires_at,
			token_updated_at = now(),
			updated_at = now()
		RETURNING `+gptAccountColumns(),
		mailEmail, gptEmail, strings.TrimSpace(input.AccountID), strings.TrimSpace(input.OrganizationID),
		strings.TrimSpace(input.AccountName), strings.TrimSpace(input.AccountStructure),
		strings.TrimSpace(input.PlanType), strings.TrimSpace(input.AuthFilePlanType), input.SubscriptionActiveUntil,
		idEncrypted, accessEncrypted, refreshEncrypted, input.TokenExpiresAt,
	).Scan(gptAccountScanDest(&account)...)
	if err != nil {
		return GPTAccount{}, fmt.Errorf("store: upsert gpt account: %w", err)
	}
	return account, nil
}

func (s *Store) GetGPTAccountCredentials(ctx context.Context, mailAccountEmail string) (GPTAccountCredentials, error) {
	var account GPTAccount
	var idEncrypted, accessEncrypted, refreshEncrypted string
	err := s.pool.QueryRow(ctx, `
		SELECT `+gptAccountColumns()+`, id_token_encrypted, access_token_encrypted, refresh_token_encrypted
		FROM gpt_accounts
		WHERE mail_account_email = $1
	`, normalizeEmail(mailAccountEmail)).Scan(append(gptAccountScanDest(&account), &idEncrypted, &accessEncrypted, &refreshEncrypted)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return GPTAccountCredentials{}, errors.New("GPT 账号未绑定")
	}
	if err != nil {
		return GPTAccountCredentials{}, fmt.Errorf("store: get gpt credentials: %w", err)
	}
	tokens, err := s.decryptGPTTokens(idEncrypted, accessEncrypted, refreshEncrypted)
	if err != nil {
		return GPTAccountCredentials{}, err
	}
	return GPTAccountCredentials{Account: account, Tokens: tokens}, nil
}

func (s *Store) UpdateGPTTokens(ctx context.Context, mailAccountEmail string, tokens GPTTokens, tokenExpiresAt *time.Time) error {
	idEncrypted, err := secure.EncryptString(s.tokenKey, strings.TrimSpace(tokens.IDToken))
	if err != nil {
		return fmt.Errorf("store: encrypt gpt id token: %w", err)
	}
	accessEncrypted, err := secure.EncryptString(s.tokenKey, strings.TrimSpace(tokens.AccessToken))
	if err != nil {
		return fmt.Errorf("store: encrypt gpt access token: %w", err)
	}
	refreshEncrypted, err := secure.EncryptString(s.tokenKey, strings.TrimSpace(tokens.RefreshToken))
	if err != nil {
		return fmt.Errorf("store: encrypt gpt refresh token: %w", err)
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE gpt_accounts
		SET id_token_encrypted = $1,
			access_token_encrypted = $2,
			refresh_token_encrypted = $3,
			token_expires_at = $4,
			token_updated_at = now(),
			requires_reauth = false,
			reauth_reason = '',
			updated_at = now()
		WHERE mail_account_email = $5
	`, idEncrypted, accessEncrypted, refreshEncrypted, tokenExpiresAt, normalizeEmail(mailAccountEmail))
	if err != nil {
		return fmt.Errorf("store: update gpt tokens: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("GPT 账号未绑定")
	}
	return nil
}

func (s *Store) UpdateGPTQuota(ctx context.Context, mailAccountEmail string, update GPTQuotaUpdate) (GPTAccount, error) {
	status := strings.TrimSpace(update.Status)
	if status == "" {
		status = "active"
	}
	lastRefreshAt := update.LastRefreshAt
	if lastRefreshAt == nil {
		now := time.Now().UTC()
		lastRefreshAt = &now
	}
	var account GPTAccount
	err := s.pool.QueryRow(ctx, `
		UPDATE gpt_accounts
		SET account_id = COALESCE(NULLIF($2, ''), account_id),
			organization_id = COALESCE(NULLIF($3, ''), organization_id),
			account_name = COALESCE(NULLIF($4, ''), account_name),
			account_structure = COALESCE(NULLIF($5, ''), account_structure),
			plan_type = COALESCE(NULLIF($6, ''), plan_type),
			auth_file_plan_type = COALESCE(NULLIF($7, ''), auth_file_plan_type),
			hourly_percentage = $8,
			hourly_reset_time = $9,
			hourly_window_minutes = $10,
			hourly_window_present = $11,
			weekly_percentage = $12,
			weekly_reset_time = $13,
			weekly_window_minutes = $14,
			weekly_window_present = $15,
			quota_raw_json = $16,
			status = $17,
			status_reason = $18,
			requires_reauth = $19,
			reauth_reason = $20,
			quota_error_code = $21,
			quota_error_message = $22,
			quota_error_at = $23,
			last_refresh_at = $24,
			updated_at = now()
		WHERE mail_account_email = $1
		RETURNING `+gptAccountColumns(),
		normalizeEmail(mailAccountEmail), strings.TrimSpace(update.AccountID), strings.TrimSpace(update.OrganizationID),
		strings.TrimSpace(update.AccountName), strings.TrimSpace(update.AccountStructure), strings.TrimSpace(update.PlanType),
		strings.TrimSpace(update.AuthFilePlanType), update.HourlyPercentage, update.HourlyResetTime,
		update.HourlyWindowMinutes, update.HourlyWindowPresent, update.WeeklyPercentage, update.WeeklyResetTime,
		update.WeeklyWindowMinutes, update.WeeklyWindowPresent, rawJSONOrNil(update.QuotaRawJSON), status,
		strings.TrimSpace(update.StatusReason), update.RequiresReauth, strings.TrimSpace(update.ReauthReason),
		strings.TrimSpace(update.QuotaErrorCode), strings.TrimSpace(update.QuotaErrorMessage), update.QuotaErrorAt, lastRefreshAt,
	).Scan(gptAccountScanDest(&account)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return GPTAccount{}, errors.New("GPT 账号未绑定")
	}
	if err != nil {
		return GPTAccount{}, fmt.Errorf("store: update gpt quota: %w", err)
	}
	return account, nil
}

func (s *Store) MarkGPTAccountReauthRequired(ctx context.Context, mailAccountEmail string, reason string, code string) (GPTAccount, error) {
	now := time.Now().UTC()
	return s.UpdateGPTQuota(ctx, mailAccountEmail, GPTQuotaUpdate{
		Status:            "reauth_required",
		StatusReason:      reason,
		RequiresReauth:    true,
		ReauthReason:      reason,
		QuotaErrorCode:    code,
		QuotaErrorMessage: reason,
		QuotaErrorAt:      &now,
		LastRefreshAt:     &now,
	})
}

func (s *Store) DeleteGPTAccount(ctx context.Context, mailAccountEmail string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM gpt_accounts WHERE mail_account_email = $1`, normalizeEmail(mailAccountEmail))
	if err != nil {
		return fmt.Errorf("store: delete gpt account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("GPT 账号未绑定")
	}
	return nil
}

func (s *Store) decryptGPTTokens(idEncrypted string, accessEncrypted string, refreshEncrypted string) (GPTTokens, error) {
	idToken, err := secure.DecryptString(s.tokenKey, idEncrypted)
	if err != nil {
		return GPTTokens{}, fmt.Errorf("store: decrypt gpt id token: %w", err)
	}
	accessToken, err := secure.DecryptString(s.tokenKey, accessEncrypted)
	if err != nil {
		return GPTTokens{}, fmt.Errorf("store: decrypt gpt access token: %w", err)
	}
	refreshToken, err := secure.DecryptString(s.tokenKey, refreshEncrypted)
	if err != nil {
		return GPTTokens{}, fmt.Errorf("store: decrypt gpt refresh token: %w", err)
	}
	return GPTTokens{IDToken: idToken, AccessToken: accessToken, RefreshToken: refreshToken}, nil
}

func gptAccountSelectSQL() string {
	return `SELECT ` + gptAccountColumns() + ` FROM gpt_accounts`
}

func gptAccountColumns() string {
	return `id, mail_account_email, gpt_email, account_id, organization_id, account_name, account_structure,
		plan_type, auth_file_plan_type, subscription_active_until,
		hourly_percentage, hourly_reset_time, hourly_window_minutes, hourly_window_present,
		weekly_percentage, weekly_reset_time, weekly_window_minutes, weekly_window_present,
		quota_raw_json, status, status_reason, requires_reauth, reauth_reason,
		quota_error_code, quota_error_message, quota_error_at,
		token_expires_at, token_updated_at, last_refresh_at, created_at, updated_at`
}

type gptAccountScanner interface {
	Scan(dest ...any) error
}

func scanGPTAccount(scanner gptAccountScanner) (GPTAccount, error) {
	var account GPTAccount
	if err := scanner.Scan(gptAccountScanDest(&account)...); err != nil {
		return GPTAccount{}, fmt.Errorf("store: scan gpt account: %w", err)
	}
	return account, nil
}

func gptAccountScanDest(account *GPTAccount) []any {
	return []any{
		&account.ID,
		&account.MailAccountEmail,
		&account.GPTEmail,
		&account.AccountID,
		&account.OrganizationID,
		&account.AccountName,
		&account.AccountStructure,
		&account.PlanType,
		&account.AuthFilePlanType,
		&account.SubscriptionActiveUntil,
		&account.HourlyPercentage,
		&account.HourlyResetTime,
		&account.HourlyWindowMinutes,
		&account.HourlyWindowPresent,
		&account.WeeklyPercentage,
		&account.WeeklyResetTime,
		&account.WeeklyWindowMinutes,
		&account.WeeklyWindowPresent,
		&account.QuotaRawJSON,
		&account.Status,
		&account.StatusReason,
		&account.RequiresReauth,
		&account.ReauthReason,
		&account.QuotaErrorCode,
		&account.QuotaErrorMessage,
		&account.QuotaErrorAt,
		&account.TokenExpiresAt,
		&account.TokenUpdatedAt,
		&account.LastRefreshAt,
		&account.CreatedAt,
		&account.UpdatedAt,
	}
}

func rawJSONOrNil(value []byte) any {
	if len(value) == 0 || strings.TrimSpace(string(value)) == "" {
		return nil
	}
	return value
}
