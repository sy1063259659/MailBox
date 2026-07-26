package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"gptbox-server/internal/secure"
)

const DefaultGroupName = "默认分组"

type Store struct {
	pool     *dbPool
	tokenKey []byte
}

type Admin struct {
	Username     string
	PasswordHash string
}

type Group struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
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

// commandTag mirrors the subset of pgconn.CommandTag the store layer relied on,
// so call sites can keep using result.RowsAffected() without an error return.
type commandTag struct {
	rowsAffected int64
}

func (tag commandTag) RowsAffected() int64 {
	return tag.rowsAffected
}

// dbPool is a thin wrapper over *sql.DB that keeps the pgx-style call shape
// (context-first Exec/Query/QueryRow/Begin) used across the store package.
type dbPool struct {
	db *sql.DB
}

// normalizeArgs converts every time.Time argument to UTC before it reaches the
// driver. All SQL in this package compares timestamps as text against
// CURRENT_TIMESTAMP / datetime('now'), which are UTC, so stored values must be
// UTC as well.
func normalizeArgs(args []any) []any {
	for index, arg := range args {
		switch value := arg.(type) {
		case time.Time:
			args[index] = value.UTC()
		case *time.Time:
			if value != nil {
				utc := value.UTC()
				args[index] = &utc
			}
		}
	}
	return args
}

func (p *dbPool) Exec(ctx context.Context, query string, args ...any) (commandTag, error) {
	result, err := p.db.ExecContext(ctx, query, normalizeArgs(args)...)
	if err != nil {
		return commandTag{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return commandTag{}, err
	}
	return commandTag{rowsAffected: affected}, nil
}

func (p *dbPool) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return p.db.QueryContext(ctx, query, normalizeArgs(args)...)
}

func (p *dbPool) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return p.db.QueryRowContext(ctx, query, normalizeArgs(args)...)
}

func (p *dbPool) Begin(ctx context.Context) (*dbTx, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &dbTx{tx: tx}, nil
}

func (p *dbPool) Close() error {
	return p.db.Close()
}

// dbTx wraps *sql.Tx with the pgx-style context-first signatures.
type dbTx struct {
	tx *sql.Tx
}

func (t *dbTx) Exec(ctx context.Context, query string, args ...any) (commandTag, error) {
	result, err := t.tx.ExecContext(ctx, query, normalizeArgs(args)...)
	if err != nil {
		return commandTag{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return commandTag{}, err
	}
	return commandTag{rowsAffected: affected}, nil
}

func (t *dbTx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, normalizeArgs(args)...)
}

func (t *dbTx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, normalizeArgs(args)...)
}

func (t *dbTx) Commit(_ context.Context) error {
	return t.tx.Commit()
}

func (t *dbTx) Rollback(_ context.Context) error {
	return t.tx.Rollback()
}

// sqliteTime scans timestamp values that come back from SQL expressions
// (COALESCE, min, max, ...). For plain table columns the driver converts to
// time.Time via the declared column type, but expressions lose the declared
// type, so the driver hands us the raw TEXT value instead.
type sqliteTime struct {
	value *time.Time
}

var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02",
}

func (t *sqliteTime) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		t.value = nil
		return nil
	case time.Time:
		parsed := value.UTC()
		t.value = &parsed
		return nil
	case string:
		return t.parse(value)
	case []byte:
		return t.parse(string(value))
	default:
		return fmt.Errorf("store: unsupported timestamp value of type %T", src)
	}
}

func (t *sqliteTime) parse(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		t.value = nil
		return nil
	}
	for _, layout := range sqliteTimeLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			parsed = parsed.UTC()
			t.value = &parsed
			return nil
		}
	}
	return fmt.Errorf("store: cannot parse timestamp %q", raw)
}

func (t *sqliteTime) Time() time.Time {
	if t.value == nil {
		return time.Time{}
	}
	return *t.value
}

// sqlInPlaceholders returns "?,?,...,?" with count placeholders. count may be
// zero, producing an empty list: SQLite accepts "IN ()" and evaluates it as
// false, matching PostgreSQL's "= ANY('{}')".
func sqlInPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat("?,", count-1) + "?"
}

func stringArgs(values []string) []any {
	args := make([]any, len(values))
	for index, value := range values {
		args[index] = value
	}
	return args
}

func New(ctx context.Context, databasePath string, tokenKey []byte) (*Store, error) {
	databasePath = strings.TrimSpace(databasePath)
	if databasePath == "" {
		return nil, errors.New("store: sqlite database path is required")
	}
	if dir := filepath.Dir(databasePath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: create sqlite directory: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(databasePath) +
		"?_txlock=immediate" +
		"&_time_format=sqlite" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(10000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite: %w", err)
	}
	// SQLite allows a single writer; WAL mode lets readers proceed
	// concurrently. A small pool is plenty and keeps lock contention low.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping sqlite: %w", err)
	}
	return &Store{pool: &dbPool{db: db}, tokenKey: tokenKey}, nil
}

func (s *Store) Close() {
	_ = s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	for _, statement := range migrationCreateStatements() {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	for _, statement := range migrationColumnStatements() {
		if _, err := s.pool.Exec(ctx, statement); err != nil && !isDuplicateColumnError(err) {
			return fmt.Errorf("store: migrate column: %w", err)
		}
	}
	for _, statement := range migrationIndexStatements() {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("store: migrate index: %w", err)
		}
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO icloud_hme_automation_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING`); err != nil {
		return fmt.Errorf("store: seed automation settings: %w", err)
	}

	if _, err := s.ensureGroup(ctx, DefaultGroupName); err != nil {
		return err
	}
	if _, err := s.ensureICloudGroup(ctx, DefaultICloudGroupName); err != nil {
		return err
	}
	_, err := s.ensureICloudHMEGroup(ctx, DefaultICloudHMEGroupName)
	return err
}

func migrationCreateStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS admins (
			username TEXT PRIMARY KEY,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS mail_accounts (
			email TEXT PRIMARY KEY,
			auth_email TEXT NOT NULL DEFAULT '',
			parent_email TEXT,
			split_index INTEGER,
			split_generated_at TIMESTAMP,
			password TEXT NOT NULL,
			password_encrypted TEXT NOT NULL DEFAULT '',
			client_id TEXT NOT NULL,
			refresh_token_encrypted TEXT NOT NULL,
			group_id INTEGER NOT NULL REFERENCES groups(id),
			remark TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'idle',
			error_message TEXT NOT NULL DEFAULT '',
			last_sync_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_accounts (
			email TEXT PRIMARY KEY,
			access_key_encrypted TEXT NOT NULL,
			group_id INTEGER NOT NULL REFERENCES icloud_groups(id),
			remark TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_source_accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			apple_id_email TEXT NOT NULL UNIQUE,
			icloud_email TEXT NOT NULL,
			host TEXT NOT NULL DEFAULT 'icloud.com',
			cookies_encrypted TEXT NOT NULL DEFAULT '',
			app_password_encrypted TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			status_reason TEXT NOT NULL DEFAULT '',
			alias_total INTEGER NOT NULL DEFAULT 0,
			last_validated_at TIMESTAMP,
			last_synced_at TIMESTAMP,
			last_created_at TIMESTAMP,
			last_error_at TIMESTAMP,
			automation_enabled BOOLEAN NOT NULL DEFAULT true,
			next_create_at TIMESTAMP,
			create_window_started_at TIMESTAMP,
			create_window_success_count INTEGER NOT NULL DEFAULT 0,
			cooldown_level INTEGER NOT NULL DEFAULT 0,
			consecutive_limit_count INTEGER NOT NULL DEFAULT 0,
			last_limit_at TIMESTAMP,
			last_auto_attempt_at TIMESTAMP,
			probe_policy_version INTEGER NOT NULL DEFAULT 2,
			probe_stage INTEGER NOT NULL DEFAULT 0,
			probe_success_streak INTEGER NOT NULL DEFAULT 0,
			probe_success_target INTEGER NOT NULL DEFAULT 3,
			probe_stable_stage INTEGER NOT NULL DEFAULT -1,
			probe_recovery_mode BOOLEAN NOT NULL DEFAULT false,
			probe_limit_started_at TIMESTAMP,
			probe_last_interval_seconds INTEGER NOT NULL DEFAULT 0,
			probe_last_recovery_seconds INTEGER NOT NULL DEFAULT 0,
			probe_last_limit_stage INTEGER NOT NULL DEFAULT -1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_aliases (
			email TEXT PRIMARY KEY,
			source_account_id INTEGER NOT NULL REFERENCES icloud_hme_source_accounts(id),
			anonymous_id TEXT NOT NULL DEFAULT '',
			label TEXT NOT NULL DEFAULT '',
			active BOOLEAN NOT NULL DEFAULT true,
			apple_status TEXT NOT NULL DEFAULT 'active',
			deactivated_at TIMESTAMP,
			deleted_at TIMESTAMP,
			last_synced_at TIMESTAMP,
			group_id INTEGER NOT NULL REFERENCES icloud_hme_groups(id),
			remark TEXT NOT NULL DEFAULT '',
			receive_key_encrypted TEXT NOT NULL DEFAULT '',
			receive_key_digest TEXT NOT NULL DEFAULT '',
			receive_key_updated_at TIMESTAMP,
			inventory_status TEXT NOT NULL DEFAULT 'available',
			sold_at TIMESTAMP,
			gpt_status TEXT NOT NULL DEFAULT 'unregistered',
			gpt_plus_activated_at TIMESTAMP,
			gpt_deactivated_at TIMESTAMP,
			gpt_plan_message_uid TEXT NOT NULL DEFAULT '',
			gpt_deactivation_message_uid TEXT NOT NULL DEFAULT '',
			gpt_last_scanned_at TIMESTAMP,
			gpt_scan_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_create_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mode TEXT NOT NULL,
			source_account_id INTEGER REFERENCES icloud_hme_source_accounts(id) ON DELETE SET NULL,
			label_prefix TEXT NOT NULL,
			group_name TEXT NOT NULL DEFAULT '默认分组',
			requested_count INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			completed_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			cancelled_count INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT '',
			origin TEXT NOT NULL DEFAULT 'manual',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMP,
			finished_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_create_job_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id INTEGER NOT NULL REFERENCES icloud_hme_create_jobs(id) ON DELETE CASCADE,
			sequence INTEGER NOT NULL,
			source_account_id INTEGER REFERENCES icloud_hme_source_accounts(id) ON DELETE SET NULL,
			label TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMP,
			finished_at TIMESTAMP,
			next_attempt_at TIMESTAMP,
			retry_class TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(job_id, sequence)
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target TEXT NOT NULL,
			result TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_automation_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			enabled BOOLEAN NOT NULL DEFAULT false,
			target_available_count INTEGER NOT NULL DEFAULT 20,
			target_group TEXT NOT NULL DEFAULT '默认分组',
			label_prefix TEXT NOT NULL DEFAULT 'MailBox',
			-- Origin used to build the public receive URLs handed to buyers.
			-- Empty means "use the origin the admin UI is served from".
			public_mail_origin TEXT NOT NULL DEFAULT '',
			error_window_started_at TIMESTAMP,
			error_window_count INTEGER NOT NULL DEFAULT 0,
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_automation_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_account_id INTEGER REFERENCES icloud_hme_source_accounts(id) ON DELETE SET NULL,
			job_item_id INTEGER REFERENCES icloud_hme_create_job_items(id) ON DELETE SET NULL,
			event_type TEXT NOT NULL,
			result TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			next_attempt_at TIMESTAMP,
			retry_count INTEGER NOT NULL DEFAULT 0,
			probe_stage INTEGER NOT NULL DEFAULT -1,
			interval_seconds INTEGER NOT NULL DEFAULT 0,
			recovery_seconds INTEGER NOT NULL DEFAULT 0,
			target_interval_min_seconds INTEGER NOT NULL DEFAULT 0,
			target_interval_max_seconds INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`}
}

// migrationColumnStatements holds ALTER TABLE steps for columns that were added
// to an already-released schema. The CREATE TABLE statements above always carry
// the current shape, so a fresh database gets the column immediately and these
// statements fail with "duplicate column name" — which isDuplicateColumnError
// tolerates. SQLite has no ADD COLUMN IF NOT EXISTS, hence this pattern.
//
// Note: SQLite only allows ADD COLUMN with a NOT NULL constraint when a default
// is supplied, so every entry here must carry a DEFAULT.
func migrationColumnStatements() []string {
	return []string{
		`ALTER TABLE icloud_hme_automation_settings ADD COLUMN public_mail_origin TEXT NOT NULL DEFAULT ''`,
	}
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func migrationIndexStatements() []string {
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_group_id ON mail_accounts(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_parent_email ON mail_accounts(parent_email)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_created_at ON mail_accounts(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_email_lower ON mail_accounts(lower(email))`,
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_parent_email_lower ON mail_accounts(lower(parent_email))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_accounts_parent_split_index
			ON mail_accounts(parent_email, split_index)
			WHERE parent_email IS NOT NULL AND parent_email <> '' AND split_index IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_accounts_group_id ON icloud_accounts(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_accounts_created_at ON icloud_accounts(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_accounts_email_lower ON icloud_accounts(lower(email))`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_source_account_id ON icloud_hme_aliases(source_account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_group_id ON icloud_hme_aliases(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_created_at ON icloud_hme_aliases(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_email_lower ON icloud_hme_aliases(lower(email))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_icloud_hme_aliases_source_anonymous_id
			ON icloud_hme_aliases(source_account_id, anonymous_id)
			WHERE anonymous_id <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_status ON icloud_hme_aliases(apple_status)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_jobs_status ON icloud_hme_create_jobs(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_job_items_status ON icloud_hme_create_job_items(job_id, status, sequence)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_job_items_due ON icloud_hme_create_job_items(status, next_attempt_at)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_inventory ON icloud_hme_aliases(inventory_status, apple_status)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_gpt_scan
			ON icloud_hme_aliases(source_account_id, gpt_status, gpt_last_scanned_at)
			WHERE gpt_status <> 'deactivated'`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_icloud_hme_active_job_item_label
			ON icloud_hme_create_job_items(label)
			WHERE status IN ('pending', 'running')`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_automation_events_created ON icloud_hme_automation_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_audit_created_at ON icloud_hme_audit_logs(created_at DESC)`}
}

func (s *Store) EnsureAdmin(ctx context.Context, username string, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("store: hash admin password: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO admins (username, password_hash)
		VALUES (?, ?)
		ON CONFLICT (username) DO UPDATE SET
			password_hash = excluded.password_hash,
			updated_at = CURRENT_TIMESTAMP
	`, username, string(hash))
	if err != nil {
		return fmt.Errorf("store: ensure admin: %w", err)
	}
	return nil
}

func (s *Store) ValidateAdmin(ctx context.Context, username string, password string) error {
	var passwordHash string
	err := s.pool.QueryRow(ctx, `SELECT password_hash FROM admins WHERE username = ?`, username).Scan(&passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
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
	rows, err := s.pool.Query(ctx, `SELECT id, name, sort_order, created_at, updated_at FROM groups ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list groups: %w", err)
	}
	defer rows.Close()

	groups := []Group{}
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan group: %w", err)
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, name string) (Group, error) {
	name = normalizeGroup(name)
	if _, err := s.ensureGroup(ctx, name); err != nil {
		return Group{}, fmt.Errorf("store: create group: %w", err)
	}
	var group Group
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, sort_order, created_at, updated_at FROM groups WHERE name = ?
	`, name).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return Group{}, fmt.Errorf("store: create group: %w", err)
	}
	return group, nil
}

func (s *Store) RenameGroup(ctx context.Context, id int64, name string) (Group, error) {
	name = normalizeGroup(name)
	tag, err := s.pool.Exec(ctx, `
		UPDATE groups SET name = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND name <> ?
	`, name, id, DefaultGroupName)
	if err != nil {
		return Group{}, fmt.Errorf("store: rename group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Group{}, errors.New("分组不存在")
	}
	var group Group
	err = s.pool.QueryRow(ctx, `
		SELECT id, name, sort_order, created_at, updated_at FROM groups WHERE id = ?
	`, id).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return Group{}, fmt.Errorf("store: rename group: %w", err)
	}
	return group, nil
}

func (s *Store) ReorderGroups(ctx context.Context, ids []int64) ([]Group, error) {
	if len(ids) == 0 {
		return nil, errors.New("分组排序不能为空")
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("分组 id 非法")
		}
		if seen[id] {
			return nil, errors.New("分组 id 不能重复")
		}
		seen[id] = true
	}

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin reorder groups: %w", err)
	}
	defer transaction.Rollback(ctx)

	for index, id := range ids {
		result, err := transaction.Exec(ctx, `UPDATE groups SET sort_order = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, index, id)
		if err != nil {
			return nil, fmt.Errorf("store: reorder group: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, errors.New("分组不存在")
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit reorder groups: %w", err)
	}
	return s.ListGroups(ctx)
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM mail_accounts WHERE group_id = ?`, id).Scan(&count); err != nil {
		return fmt.Errorf("store: count group accounts: %w", err)
	}
	if count > 0 {
		return errors.New("只能删除空分组")
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id = ? AND name <> ?`, id, DefaultGroupName)
	if err != nil {
		return fmt.Errorf("store: delete group: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("分组不存在或默认分组不可删除")
	}
	return nil
}

const mailAccountSelectColumns = `
	SELECT a.email, a.password, a.password_encrypted, a.client_id, g.name, a.remark, a.status, a.error_message,
		COALESCE(a.parent_email, ''), a.split_index, a.split_generated_at,
		a.last_sync_at, a.created_at, a.updated_at
	FROM mail_accounts a
	JOIN groups g ON g.id = a.group_id
`

func (s *Store) scanMailAccount(row interface{ Scan(...any) error }) (MailAccount, error) {
	var account MailAccount
	var legacyPassword, encryptedPassword string
	if err := row.Scan(
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
		return MailAccount{}, err
	}
	password, err := s.decryptAccountPassword(encryptedPassword, legacyPassword)
	if err != nil {
		return MailAccount{}, fmt.Errorf("store: decrypt account password: %w", err)
	}
	account.Password = password
	account.DisplayName = account.Email
	return account, nil
}

func (s *Store) ListAccounts(ctx context.Context) ([]MailAccount, error) {
	rows, err := s.pool.Query(ctx, mailAccountSelectColumns+`
		ORDER BY COALESCE(a.parent_email, a.email) ASC, a.parent_email NULLS FIRST, a.split_index NULLS FIRST, a.created_at ASC, a.email ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	defer rows.Close()

	accounts := []MailAccount{}
	for rows.Next() {
		account, err := s.scanMailAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan account: %w", err)
		}
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
			VALUES (?, ?, '', ?, ?, ?, ?, ?, 'idle', '')
			ON CONFLICT (email) DO UPDATE SET
				auth_email = excluded.auth_email,
				password = '',
				password_encrypted = excluded.password_encrypted,
				client_id = excluded.client_id,
				refresh_token_encrypted = excluded.refresh_token_encrypted,
				group_id = excluded.group_id,
				remark = CASE WHEN ? THEN excluded.remark ELSE remark END,
				updated_at = CURRENT_TIMESTAMP
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

func (s *Store) ExportAccounts(ctx context.Context, emails []string) (string, error) {
	normalizedEmails := uniqueNormalizedEmails(emails)
	query := `
		SELECT email, password, password_encrypted, client_id, refresh_token_encrypted
		FROM mail_accounts
	`
	args := []any{}
	if len(normalizedEmails) > 0 {
		query += `WHERE lower(email) IN (` + sqlInPlaceholders(len(normalizedEmails)) + `) `
		args = append(args, stringArgs(normalizedEmails)...)
	}
	query += `ORDER BY created_at ASC, email ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return "", fmt.Errorf("store: export accounts: %w", err)
	}
	defer rows.Close()

	lines := []string{}
	for rows.Next() {
		var email, legacyPassword, encryptedPassword, clientID, encrypted string
		if err := rows.Scan(&email, &legacyPassword, &encryptedPassword, &clientID, &encrypted); err != nil {
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
		lines = append(lines, strings.Join([]string{email, password, clientID, refreshToken}, "----"))
	}
	return strings.Join(lines, "\n"), rows.Err()
}

func uniqueNormalizedEmails(emails []string) []string {
	seen := make(map[string]bool, len(emails))
	normalized := []string{}
	for _, email := range emails {
		value := normalizeEmail(email)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	return normalized
}

func (s *Store) UpdateAccountRemark(ctx context.Context, email string, remark string) (MailAccount, error) {
	normalized := normalizeEmail(email)
	tag, err := s.pool.Exec(ctx, `
		UPDATE mail_accounts
		SET remark = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
	`, strings.TrimSpace(remark), normalized)
	if err != nil {
		return MailAccount{}, fmt.Errorf("store: update account remark: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return MailAccount{}, errors.New("账号不存在")
	}
	account, err := s.scanMailAccount(s.pool.QueryRow(ctx, mailAccountSelectColumns+`WHERE lower(a.email) = ?`, normalized))
	if err != nil {
		return MailAccount{}, fmt.Errorf("store: reload account after remark update: %w", err)
	}
	return account, nil
}

func (s *Store) DeleteAccount(ctx context.Context, email string) ([]string, error) {
	normalized := normalizeEmail(email)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin delete account: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `SELECT email FROM mail_accounts WHERE lower(email) = ? OR lower(parent_email) = ?`, normalized, normalized)
	if err != nil {
		return nil, fmt.Errorf("store: delete account: %w", err)
	}
	deletedEmails := []string{}
	for rows.Next() {
		var deletedEmail string
		if err := rows.Scan(&deletedEmail); err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: scan deleted account: %w", err)
		}
		deletedEmails = append(deletedEmails, deletedEmail)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("store: read deleted accounts: %w", err)
	}
	rows.Close()
	if len(deletedEmails) == 0 {
		return nil, errors.New("账号不存在")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM mail_accounts WHERE lower(email) = ? OR lower(parent_email) = ?`, normalized, normalized); err != nil {
		return nil, fmt.Errorf("store: delete account: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit delete account: %w", err)
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
		WHERE lower(a.email) = ? AND COALESCE(a.parent_email, '') = ''
	`, parentEmail).Scan(&legacyPassword, &encryptedPassword, &clientID, &encrypted, &groupID, &groupName, &remark)
	if errors.Is(err, sql.ErrNoRows) {
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
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM mail_accounts WHERE parent_email = ?`, parentEmail).Scan(&childCount); err != nil {
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO mail_accounts (
				email, auth_email, parent_email, split_index, split_generated_at,
				password, password_encrypted, client_id, refresh_token_encrypted, group_id, remark, status, error_message
			)
			VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, 'idle', '')
		`, alias, parentEmail, parentEmail, splitIndex, generatedAt, encryptedPassword, clientID, encrypted, groupID, remark); err != nil {
			return SplitResult{}, fmt.Errorf("store: insert split child: %w", err)
		}
		var account MailAccount
		account.Email = alias
		account.Password = password
		account.ClientID = clientID
		account.Remark = remark
		account.Status = "idle"
		account.ParentEmail = parentEmail
		account.SplitIndex = &splitIndex
		account.Group = groupName
		account.DisplayName = alias
		splitGeneratedAt := generatedAt
		account.SplitGeneratedAt = &splitGeneratedAt
		if err := tx.QueryRow(ctx, `
			SELECT created_at, updated_at FROM mail_accounts WHERE email = ?
		`, alias).Scan(&account.CreatedAt, &account.UpdatedAt); err != nil {
			return SplitResult{}, fmt.Errorf("store: read split child: %w", err)
		}
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
	args := append([]any{groupID}, stringArgs(normalized)...)
	_, err = s.pool.Exec(ctx, `
		UPDATE mail_accounts SET group_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) IN (`+sqlInPlaceholders(len(normalized))+`)
	`, args...)
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
		WHERE lower(email) = ?
	`, normalizeEmail(email)).Scan(&credentials.Email, &credentials.AuthEmail, &credentials.ClientID, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
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
		UPDATE mail_accounts SET refresh_token_encrypted = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
	`, encrypted, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("store: update refresh token: %w", err)
	}
	return nil
}

func (s *Store) UpdateAccountStatus(ctx context.Context, email string, status string, errorMessage string, synced bool) error {
	query := `
		UPDATE mail_accounts
		SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP,
			last_sync_at = CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE last_sync_at END
		WHERE lower(email) = ?
	`
	_, err := s.pool.Exec(ctx, query, status, errorMessage, synced, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	return nil
}

func (s *Store) ensureGroup(ctx context.Context, name string) (int64, error) {
	name = normalizeGroup(name)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO groups (name, sort_order)
		VALUES (?, COALESCE((SELECT max(sort_order) + 1 FROM groups), 0))
		ON CONFLICT (name) DO NOTHING
	`, name); err != nil {
		return 0, fmt.Errorf("store: ensure group: %w", err)
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM groups WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: ensure group: %w", err)
	}
	return id, nil
}

func (s *Store) accountExists(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM mail_accounts WHERE lower(email) = ?)
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

func (s *Store) uniqueHotmailAlias(ctx context.Context, tx *dbTx, parentEmail string) (string, error) {
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
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM mail_accounts WHERE email = ?)`, alias).Scan(&exists); err != nil {
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
