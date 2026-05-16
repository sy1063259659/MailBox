package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
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
	Email            string     `json:"email"`
	Password         string     `json:"password"`
	ClientID         string     `json:"clientId"`
	RefreshToken     string     `json:"refreshToken,omitempty"`
	Group            string     `json:"group"`
	Remark           string     `json:"remark"`
	DisplayName      string     `json:"displayName"`
	Status           string     `json:"status"`
	ErrorMessage     string     `json:"errorMessage,omitempty"`
	ParentEmail      string     `json:"parentEmail,omitempty"`
	SplitIndex       *int       `json:"splitIndex,omitempty"`
	SplitGeneratedAt *time.Time `json:"splitGeneratedAt,omitempty"`
	LastSyncAt       *time.Time `json:"lastSyncAt,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type AccountInput struct {
	Email        string
	Password     string
	ClientID     string
	RefreshToken string
	Group        string
	Remark       string
	RemarkSet    bool
}

type AccountCredentials struct {
	Email        string
	AuthEmail    string
	ClientID     string
	RefreshToken string
}

type ImportResult struct {
	Imported int      `json:"imported"`
	Updated  int      `json:"updated"`
	Errors   []string `json:"errors"`
}

type SplitResult struct {
	ParentEmail string        `json:"parentEmail"`
	Accounts    []MailAccount `json:"accounts"`
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
			auth_email TEXT NOT NULL DEFAULT '',
			parent_email TEXT,
			split_index INTEGER,
			split_generated_at TIMESTAMPTZ,
			password TEXT NOT NULL,
			password_encrypted TEXT NOT NULL DEFAULT '',
			client_id TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL,
			group_id BIGINT NOT NULL REFERENCES groups(id),
			remark TEXT NOT NULL DEFAULT '',
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

	for _, statement := range migrationColumnStatements() {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("store: migrate column: %w", err)
		}
	}
	for _, statement := range migrationIndexStatements() {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("store: migrate index: %w", err)
		}
	}
	if err := s.backfillAuthEmails(ctx); err != nil {
		return err
	}
	if err := s.backfillEncryptedPasswords(ctx); err != nil {
		return err
	}
	if err := s.backfillSplitParents(ctx); err != nil {
		return err
	}

	_, err := s.ensureGroup(ctx, DefaultGroupName)
	return err
}

func migrationColumnStatements() []string {
	return []string{
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS auth_email TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS parent_email TEXT`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS split_index INTEGER`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS split_generated_at TIMESTAMPTZ`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS remark TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS password_encrypted TEXT NOT NULL DEFAULT ''`,
	}
}

func migrationIndexStatements() []string {
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_group_id ON mail_accounts(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_parent_email ON mail_accounts(parent_email)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_created_at ON mail_accounts(created_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_accounts_parent_split_index
			ON mail_accounts(parent_email, split_index)
			WHERE parent_email IS NOT NULL AND parent_email <> '' AND split_index IS NOT NULL`,
	}
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
		SELECT a.email, a.password, a.password_encrypted, a.client_id, g.name, a.remark, a.status, a.error_message,
			COALESCE(a.parent_email, ''), a.split_index, a.split_generated_at,
			a.last_sync_at, a.created_at, a.updated_at
		FROM mail_accounts a
		JOIN groups g ON g.id = a.group_id
		ORDER BY COALESCE(a.parent_email, a.email) ASC, a.parent_email NULLS FIRST, a.split_index NULLS FIRST, a.created_at ASC, a.email ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	defer rows.Close()

	accounts := []MailAccount{}
	for rows.Next() {
		var account MailAccount
		var legacyPassword, encryptedPassword string
		if err := rows.Scan(
			&account.Email,
			&legacyPassword,
			&encryptedPassword,
			&account.ClientID,
			&account.Group,
			&account.Remark,
			&account.Status,
			&account.ErrorMessage,
			&account.ParentEmail,
			&account.SplitIndex,
			&account.SplitGeneratedAt,
			&account.LastSyncAt,
			&account.CreatedAt,
			&account.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan account: %w", err)
		}
		password, err := s.decryptAccountPassword(encryptedPassword, legacyPassword)
		if err != nil {
			return nil, fmt.Errorf("store: decrypt account password: %w", err)
		}
		account.Password = password
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
		encryptedPassword, err := secure.EncryptString(s.tokenKey, input.Password)
		if err != nil {
			return result, err
		}
		authEmail := authEmailFor(input.Email)
		tag, err := s.pool.Exec(ctx, `
			INSERT INTO mail_accounts (
				email, auth_email, password, password_encrypted, client_id, refresh_token_encrypted, group_id, remark, status, error_message
			)
			VALUES ($1, $2, '', $3, $4, $5, $6, $7, 'idle', '')
			ON CONFLICT (email) DO UPDATE SET
				auth_email = EXCLUDED.auth_email,
				password = '',
				password_encrypted = EXCLUDED.password_encrypted,
				client_id = EXCLUDED.client_id,
				refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
				group_id = EXCLUDED.group_id,
				remark = CASE WHEN $8 THEN EXCLUDED.remark ELSE mail_accounts.remark END,
				updated_at = now()
		`, input.Email, authEmail, encryptedPassword, input.ClientID, encrypted, groupID, input.Remark, input.RemarkSet)
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
		SELECT email, password, password_encrypted, client_id, refresh_token_encrypted, remark
		FROM mail_accounts
		ORDER BY created_at ASC, email ASC
	`)
	if err != nil {
		return "", fmt.Errorf("store: export accounts: %w", err)
	}
	defer rows.Close()

	lines := []string{}
	for rows.Next() {
		var email, legacyPassword, encryptedPassword, clientID, encrypted, remark string
		if err := rows.Scan(&email, &legacyPassword, &encryptedPassword, &clientID, &encrypted, &remark); err != nil {
			return "", fmt.Errorf("store: scan export account: %w", err)
		}
		password, err := s.decryptAccountPassword(encryptedPassword, legacyPassword)
		if err != nil {
			return "", fmt.Errorf("store: decrypt export password: %w", err)
		}
		refreshToken, err := secure.DecryptString(s.tokenKey, encrypted)
		if err != nil {
			return "", err
		}
		fields := []string{email, password, clientID, refreshToken}
		if strings.TrimSpace(remark) != "" {
			fields = append(fields, remark)
		}
		lines = append(lines, strings.Join(fields, "----"))
	}
	return strings.Join(lines, "\n"), rows.Err()
}

func (s *Store) UpdateAccountRemark(ctx context.Context, email string, remark string) (MailAccount, error) {
	normalized := normalizeEmail(email)
	var account MailAccount
	var legacyPassword, encryptedPassword string
	err := s.pool.QueryRow(ctx, `
		UPDATE mail_accounts a
		SET remark = $2, updated_at = now()
		FROM groups g
		WHERE a.group_id = g.id AND lower(a.email) = $1
		RETURNING a.email, a.password, a.password_encrypted, a.client_id, g.name, a.remark, a.status, a.error_message,
			COALESCE(a.parent_email, ''), a.split_index, a.split_generated_at,
			a.last_sync_at, a.created_at, a.updated_at
	`, normalized, strings.TrimSpace(remark)).Scan(
		&account.Email,
		&legacyPassword,
		&encryptedPassword,
		&account.ClientID,
		&account.Group,
		&account.Remark,
		&account.Status,
		&account.ErrorMessage,
		&account.ParentEmail,
		&account.SplitIndex,
		&account.SplitGeneratedAt,
		&account.LastSyncAt,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MailAccount{}, errors.New("账号不存在")
	}
	if err != nil {
		return MailAccount{}, fmt.Errorf("store: update account remark: %w", err)
	}
	password, err := s.decryptAccountPassword(encryptedPassword, legacyPassword)
	if err != nil {
		return MailAccount{}, fmt.Errorf("store: decrypt account password: %w", err)
	}
	account.Password = password
	account.DisplayName = account.Email
	return account, nil
}

func (s *Store) DeleteAccount(ctx context.Context, email string) ([]string, error) {
	normalized := normalizeEmail(email)
	rows, err := s.pool.Query(ctx, `DELETE FROM mail_accounts WHERE lower(email) = $1 OR lower(parent_email) = $1 RETURNING email`, normalized)
	if err != nil {
		return nil, fmt.Errorf("store: delete account: %w", err)
	}
	defer rows.Close()

	deletedEmails := []string{}
	for rows.Next() {
		var deletedEmail string
		if err := rows.Scan(&deletedEmail); err != nil {
			return nil, fmt.Errorf("store: scan deleted account: %w", err)
		}
		deletedEmails = append(deletedEmails, deletedEmail)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read deleted accounts: %w", err)
	}
	if len(deletedEmails) == 0 {
		return nil, errors.New("账号不存在")
	}
	return deletedEmails, nil
}

func (s *Store) SplitHotmailAccount(ctx context.Context, email string) (SplitResult, error) {
	parentEmail := normalizeEmail(email)
	if !isHotmailPrimary(parentEmail) {
		return SplitResult{}, errors.New("只有 hotmail.com 主账号可以分裂")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SplitResult{}, fmt.Errorf("store: begin split account: %w", err)
	}
	defer tx.Rollback(ctx)

	var legacyPassword, encryptedPassword, clientID, encrypted, groupName, remark string
	var groupID int64
	err = tx.QueryRow(ctx, `
		SELECT a.password, a.password_encrypted, a.client_id, a.refresh_token_encrypted, a.group_id, g.name, a.remark
		FROM mail_accounts a
		JOIN groups g ON g.id = a.group_id
		WHERE lower(a.email) = $1 AND COALESCE(a.parent_email, '') = ''
		FOR UPDATE
	`, parentEmail).Scan(&legacyPassword, &encryptedPassword, &clientID, &encrypted, &groupID, &groupName, &remark)
	if errors.Is(err, pgx.ErrNoRows) {
		return SplitResult{}, errors.New("主账号不存在或不是主账号")
	}
	if err != nil {
		return SplitResult{}, fmt.Errorf("store: get split parent: %w", err)
	}
	password, err := s.decryptAccountPassword(encryptedPassword, legacyPassword)
	if err != nil {
		return SplitResult{}, fmt.Errorf("store: decrypt split parent password: %w", err)
	}
	if strings.TrimSpace(encryptedPassword) == "" {
		encryptedPassword, err = secure.EncryptString(s.tokenKey, password)
		if err != nil {
			return SplitResult{}, fmt.Errorf("store: encrypt split parent password: %w", err)
		}
	}

	var childCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM mail_accounts WHERE parent_email = $1`, parentEmail).Scan(&childCount); err != nil {
		return SplitResult{}, fmt.Errorf("store: count split children: %w", err)
	}
	if childCount > 0 {
		return SplitResult{}, errors.New("该账号已分裂，不能重复生成")
	}

	accounts := make([]MailAccount, 0, 5)
	generatedAt := time.Now().UTC()
	for index := 1; index <= 5; index++ {
		alias, err := s.uniqueHotmailAlias(ctx, tx, parentEmail)
		if err != nil {
			return SplitResult{}, err
		}
		splitIndex := index
		var account MailAccount
		err = tx.QueryRow(ctx, `
			INSERT INTO mail_accounts (
				email, auth_email, parent_email, split_index, split_generated_at,
				password, password_encrypted, client_id, refresh_token_encrypted, group_id, remark, status, error_message
			)
			VALUES ($1, $2, $3, $4, $5, '', $6, $7, $8, $9, $10, 'idle', '')
			RETURNING email, password, password_encrypted, client_id, remark, status, error_message, parent_email, split_index,
				split_generated_at, last_sync_at, created_at, updated_at
		`, alias, parentEmail, parentEmail, splitIndex, generatedAt, encryptedPassword, clientID, encrypted, groupID, remark).Scan(
			&account.Email,
			&legacyPassword,
			&encryptedPassword,
			&account.ClientID,
			&account.Remark,
			&account.Status,
			&account.ErrorMessage,
			&account.ParentEmail,
			&account.SplitIndex,
			&account.SplitGeneratedAt,
			&account.LastSyncAt,
			&account.CreatedAt,
			&account.UpdatedAt,
		)
		if err != nil {
			return SplitResult{}, fmt.Errorf("store: insert split child: %w", err)
		}
		password, err := s.decryptAccountPassword(encryptedPassword, legacyPassword)
		if err != nil {
			return SplitResult{}, fmt.Errorf("store: decrypt split child password: %w", err)
		}
		account.Password = password
		account.Group = groupName
		account.DisplayName = account.Email
		accounts = append(accounts, account)
	}

	if err := tx.Commit(ctx); err != nil {
		return SplitResult{}, fmt.Errorf("store: commit split account: %w", err)
	}
	return SplitResult{ParentEmail: parentEmail, Accounts: accounts}, nil
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
		WHERE lower(email) = ANY($2)
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
		SELECT email, COALESCE(NULLIF(auth_email, ''), email), client_id, refresh_token_encrypted
		FROM mail_accounts
		WHERE lower(email) = $1
	`, normalizeEmail(email)).Scan(&credentials.Email, &credentials.AuthEmail, &credentials.ClientID, &encrypted)
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
		WHERE lower(email) = $2
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
		WHERE lower(email) = $4
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
		SELECT EXISTS(SELECT 1 FROM mail_accounts WHERE lower(email) = $1)
	`, normalizeEmail(email)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: check account exists: %w", err)
	}
	return exists, nil
}

func (s *Store) decryptAccountPassword(encrypted string, legacy string) (string, error) {
	if strings.TrimSpace(encrypted) == "" {
		return legacy, nil
	}
	password, err := secure.DecryptString(s.tokenKey, encrypted)
	if err != nil {
		return "", err
	}
	return password, nil
}

func (s *Store) backfillAuthEmails(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT email
		FROM mail_accounts
		WHERE auth_email = '' OR auth_email IS NULL
	`)
	if err != nil {
		return fmt.Errorf("store: query auth email backfill: %w", err)
	}
	defer rows.Close()

	emails := []string{}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return fmt.Errorf("store: scan auth email backfill: %w", err)
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: read auth email backfill: %w", err)
	}

	for _, email := range emails {
		if _, err := s.pool.Exec(ctx, `
			UPDATE mail_accounts
			SET auth_email = $1, updated_at = now()
			WHERE email = $2
		`, authEmailFor(email), normalizeEmail(email)); err != nil {
			return fmt.Errorf("store: backfill auth email: %w", err)
		}
	}
	return nil
}

func (s *Store) backfillEncryptedPasswords(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT email, password
		FROM mail_accounts
		WHERE password <> '' AND (password_encrypted = '' OR password_encrypted IS NULL)
	`)
	if err != nil {
		return fmt.Errorf("store: query password backfill: %w", err)
	}
	defer rows.Close()

	type accountPassword struct {
		email    string
		password string
	}
	passwords := []accountPassword{}
	for rows.Next() {
		var item accountPassword
		if err := rows.Scan(&item.email, &item.password); err != nil {
			return fmt.Errorf("store: scan password backfill: %w", err)
		}
		passwords = append(passwords, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: read password backfill: %w", err)
	}

	for _, item := range passwords {
		encrypted, err := secure.EncryptString(s.tokenKey, item.password)
		if err != nil {
			return fmt.Errorf("store: encrypt password backfill: %w", err)
		}
		if _, err := s.pool.Exec(ctx, `
			UPDATE mail_accounts
			SET password_encrypted = $1,
				password = '',
				updated_at = now()
			WHERE email = $2
		`, encrypted, item.email); err != nil {
			return fmt.Errorf("store: update password backfill: %w", err)
		}
	}
	return nil
}

func (s *Store) backfillSplitParents(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT email
		FROM mail_accounts
		WHERE (parent_email IS NULL OR parent_email = '')
			AND lower(email) LIKE '%+%@hotmail.com'
	`)
	if err != nil {
		return fmt.Errorf("store: query split parent backfill: %w", err)
	}
	defer rows.Close()

	type splitChild struct {
		email  string
		parent string
	}
	children := []splitChild{}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return fmt.Errorf("store: scan split parent backfill: %w", err)
		}
		parent := hotmailParentEmail(email)
		if parent != "" && parent != normalizeEmail(email) {
			children = append(children, splitChild{email: normalizeEmail(email), parent: parent})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: read split parent backfill: %w", err)
	}

	parentCounts := map[string]int{}
	for _, child := range children {
		parentCounts[child.parent]++
		splitIndex := parentCounts[child.parent]
		if _, err := s.pool.Exec(ctx, `
			UPDATE mail_accounts
			SET parent_email = $1,
				split_index = COALESCE(split_index, $2),
				split_generated_at = COALESCE(split_generated_at, created_at),
				auth_email = $1,
				updated_at = now()
			WHERE email = $3
		`, child.parent, splitIndex, child.email); err != nil {
			return fmt.Errorf("store: backfill split parent: %w", err)
		}
	}
	return nil
}

func authEmailFor(email string) string {
	email = normalizeEmail(email)
	local, domain, ok := strings.Cut(email, "@")
	if !ok {
		return email
	}
	if !isMicrosoftPersonalDomain(domain) {
		return email
	}
	if base, _, found := strings.Cut(local, "+"); found && base != "" {
		return base + "@" + domain
	}
	return email
}

func isMicrosoftPersonalDomain(domain string) bool {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "hotmail.com", "outlook.com", "live.com", "msn.com":
		return true
	default:
		return false
	}
}

func (s *Store) uniqueHotmailAlias(ctx context.Context, tx pgx.Tx, parentEmail string) (string, error) {
	local, domain, ok := strings.Cut(parentEmail, "@")
	if !ok {
		return "", errors.New("主账号格式错误")
	}
	for tries := 0; tries < 80; tries++ {
		suffix, err := randomLetters(6)
		if err != nil {
			return "", err
		}
		alias := strings.ToLower(local + "+" + suffix + "@" + domain)
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mail_accounts WHERE email = $1)`, alias).Scan(&exists); err != nil {
			return "", fmt.Errorf("store: check split alias: %w", err)
		}
		if !exists {
			return alias, nil
		}
	}
	return "", errors.New("生成别名失败，请重试")
}

func randomLetters(length int) (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	var builder strings.Builder
	builder.Grow(length)
	for index := 0; index < length; index++ {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", fmt.Errorf("store: random alias suffix: %w", err)
		}
		builder.WriteByte(letters[value.Int64()])
	}
	return builder.String(), nil
}

func isHotmailPrimary(email string) bool {
	local, domain, ok := strings.Cut(normalizeEmail(email), "@")
	return ok && domain == "hotmail.com" && local != "" && !strings.Contains(local, "+")
}

func hotmailParentEmail(email string) string {
	email = normalizeEmail(email)
	local, domain, ok := strings.Cut(email, "@")
	if !ok || domain != "hotmail.com" {
		return ""
	}
	base, _, found := strings.Cut(local, "+")
	if !found || base == "" {
		return ""
	}
	return base + "@" + domain
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
