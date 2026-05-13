package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"mailbox-server/internal/secure"
)

const DefaultGroupName = "默认分组"

type Store struct {
	pool     *pgxpool.Pool
	tokenKey []byte
}

type Admin struct {
	Username     string
	PasswordHash string
}

type Group struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type MailAccount struct {
	Email        string     `json:"email"`
	Password     string     `json:"password"`
	ClientID     string     `json:"clientId"`
	RefreshToken string     `json:"refreshToken,omitempty"`
	Group        string     `json:"group"`
	DisplayName  string     `json:"displayName"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	LastSyncAt   *time.Time `json:"lastSyncAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type AccountInput struct {
	Email        string
	Password     string
	ClientID     string
	RefreshToken string
	Group        string
}

type AccountCredentials struct {
	Email        string
	ClientID     string
	RefreshToken string
}

type ImportResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Errors   []string `json:"errors"`
}

func New(ctx context.Context, databaseURL string, tokenKey []byte) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping postgres: %w", err)
	}
	return &Store{pool: pool, tokenKey: tokenKey}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS admins (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS mail_accounts (
			email TEXT PRIMARY KEY,
			password TEXT NOT NULL,
			client_id TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL,
			group_id BIGINT NOT NULL REFERENCES groups(id),
			status TEXT NOT NULL DEFAULT 'idle',
			error_message TEXT NOT NULL DEFAULT '',
			last_sync_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}

	for _, statement := range statements {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}

	_, err := s.ensureGroup(ctx, DefaultGroupName)
	return err
}

func (s *Store) EnsureAdmin(ctx context.Context, username string, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("store: hash admin password: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO admins (username, password_hash)
		VALUES ($1, $2)
		ON CONFLICT (username) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			updated_at = now()
	`, username, string(hash))
	if err != nil {
		return fmt.Errorf("store: ensure admin: %w", err)
	}
	return nil
}

func (s *Store) ValidateAdmin(ctx context.Context, username string, password string) error {
	var passwordHash string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM admins WHERE username = $1`, username).Scan(&passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("用户名或密码错误")
	}
	if err != nil {
		return fmt.Errorf("store: query admin: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return errors.New("用户名或密码错误")
	}
	return nil
}

func (s *Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, created_at, updated_at FROM groups ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list groups: %w", err)
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan group: %w", err)
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, name string) (Group, error) {
	name = normalizeGroup(name)
	var group Group
	err := s.pool.QueryRow(ctx, `
		INSERT INTO groups (name)
		VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET updated_at = groups.updated_at
		RETURNING id, name, created_at, updated_at
	`, name).Scan(&group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return Group{}, fmt.Errorf("store: create group: %w", err)
	}
	return group, nil
}

func (s *Store) RenameGroup(ctx context.Context, id int64, name string) (Group, error) {
	name = normalizeGroup(name)
	var group Group
	err := s.pool.QueryRow(ctx, `
		UPDATE groups SET name = $1, updated_at = now()
		WHERE id = $2
		RETURNING id, name, created_at, updated_at
	`, name, id).Scan(&group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, errors.New("分组不存在")
	}
	if err != nil {
		return Group{}, fmt.Errorf("store: rename group: %w", err)
	}
	return group, nil
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mail_accounts WHERE group_id = $1`, id).Scan(&count); err != nil {
		return fmt.Errorf("store: count group accounts: %w", err)
	}
	if count > 0 {
		return errors.New("只能删除空分组")
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1 AND name <> $2`, id, DefaultGroupName)
	if err != nil {
		return fmt.Errorf("store: delete group: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("分组不存在或默认分组不可删除")
	}
	return nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]MailAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.email, a.password, a.client_id, g.name, a.status, a.error_message,
			a.last_sync_at, a.created_at, a.updated_at
		FROM mail_accounts a
		JOIN groups g ON g.id = a.group_id
		ORDER BY a.created_at ASC, a.email ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	defer rows.Close()

	accounts := []MailAccount{}
	for rows.Next() {
		var account MailAccount
		if err := rows.Scan(
			&account.Email,
			&account.Password,
			&account.ClientID,
			&account.Group,
			&account.Status,
			&account.ErrorMessage,
			&account.LastSyncAt,
			&account.CreatedAt,
			&account.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan account: %w", err)
		}
		account.DisplayName = account.Email
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) ImportAccounts(ctx context.Context, inputs []AccountInput) (ImportResult, error) {
	result := ImportResult{Errors: []string{}}
	for _, input := range inputs {
		existed, err := s.accountExists(ctx, input.Email)
		if err != nil {
			return result, err
		}
		groupID, err := s.ensureGroup(ctx, normalizeGroup(input.Group))
		if err != nil {
			return result, err
		}
		encrypted, err := secure.EncryptString(s.tokenKey, input.RefreshToken)
		if err != nil {
			return result, err
		}
		tag, err := s.pool.Exec(ctx, `
			INSERT INTO mail_accounts (
				email, password, client_id, refresh_token_encrypted, group_id, status, error_message
			)
			VALUES ($1, $2, $3, $4, $5, 'idle', '')
			ON CONFLICT (email) DO UPDATE SET
				password = EXCLUDED.password,
				client_id = EXCLUDED.client_id,
				refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
				group_id = EXCLUDED.group_id,
				updated_at = now()
		`, input.Email, input.Password, input.ClientID, encrypted, groupID)
		if err != nil {
			return result, fmt.Errorf("store: import account: %w", err)
		}
		if tag.RowsAffected() > 0 {
			if existed {
				result.Updated++
			} else {
				result.Imported++
			}
		}
	}
	return result, nil
}

func (s *Store) ClearAccounts(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM mail_accounts`)
	if err != nil {
		return fmt.Errorf("store: clear accounts: %w", err)
	}
	return nil
}

func (s *Store) ExportAccounts(ctx context.Context) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT email, password, client_id, refresh_token_encrypted
		FROM mail_accounts
		ORDER BY created_at ASC, email ASC
	`)
	if err != nil {
		return "", fmt.Errorf("store: export accounts: %w", err)
	}
	defer rows.Close()

	lines := []string{}
	for rows.Next() {
		var email, password, clientID, encrypted string
		if err := rows.Scan(&email, &password, &clientID, &encrypted); err != nil {
			return "", fmt.Errorf("store: scan export account: %w", err)
		}
		refreshToken, err := secure.DecryptString(s.tokenKey, encrypted)
		if err != nil {
			return "", err
		}
		lines = append(lines, strings.Join([]string{email, password, clientID, refreshToken}, "----"))
	}
	return strings.Join(lines, "\n"), rows.Err()
}

func (s *Store) DeleteAccount(ctx context.Context, email string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM mail_accounts WHERE email = $1`, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("store: delete account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("账号不存在")
	}
	return nil
}

func (s *Store) MoveAccountsToGroup(ctx context.Context, emails []string, group string) error {
	groupID, err := s.ensureGroup(ctx, normalizeGroup(group))
	if err != nil {
		return err
	}
	normalized := make([]string, 0, len(emails))
	for _, email := range emails {
		if value := normalizeEmail(email); value != "" {
			normalized = append(normalized, value)
		}
	}
	if len(normalized) == 0 {
		return errors.New("请选择账号")
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE mail_accounts SET group_id = $1, updated_at = now()
		WHERE email = ANY($2)
	`, groupID, normalized)
	if err != nil {
		return fmt.Errorf("store: move accounts: %w", err)
	}
	return nil
}

func (s *Store) GetCredentials(ctx context.Context, email string) (AccountCredentials, error) {
	var credentials AccountCredentials
	var encrypted string
	err := s.pool.QueryRow(ctx, `
		SELECT email, client_id, refresh_token_encrypted
		FROM mail_accounts
		WHERE email = $1
	`, normalizeEmail(email)).Scan(&credentials.Email, &credentials.ClientID, &encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountCredentials{}, errors.New("账号不存在")
	}
	if err != nil {
		return AccountCredentials{}, fmt.Errorf("store: get credentials: %w", err)
	}
	refreshToken, err := secure.DecryptString(s.tokenKey, encrypted)
	if err != nil {
		return AccountCredentials{}, err
	}
	credentials.RefreshToken = refreshToken
	return credentials, nil
}

func (s *Store) UpdateRefreshToken(ctx context.Context, email string, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	encrypted, err := secure.EncryptString(s.tokenKey, refreshToken)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE mail_accounts SET refresh_token_encrypted = $1, updated_at = now()
		WHERE email = $2
	`, encrypted, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("store: update refresh token: %w", err)
	}
	return nil
}

func (s *Store) UpdateAccountStatus(ctx context.Context, email string, status string, errorMessage string, synced bool) error {
	query := `
		UPDATE mail_accounts
		SET status = $1, error_message = $2, updated_at = now(), last_sync_at = CASE WHEN $3 THEN now() ELSE last_sync_at END
		WHERE email = $4
	`
	_, err := s.pool.Exec(ctx, query, status, errorMessage, synced, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	return nil
}

func (s *Store) ensureGroup(ctx context.Context, name string) (int64, error) {
	name = normalizeGroup(name)
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO groups (name)
		VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET updated_at = groups.updated_at
		RETURNING id
	`, name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: ensure group: %w", err)
	}
	return id, nil
}

func (s *Store) accountExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM mail_accounts WHERE email = $1)
	`, normalizeEmail(email)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: check account exists: %w", err)
	}
	return exists, nil
}

func normalizeGroup(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return DefaultGroupName
	}
	return group
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
