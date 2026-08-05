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

	"gptbox-server/internal/secure"
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
	statements := migrationCreateStatements()
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
	for _, statement := range legacyCleanupStatements() {
		if _, err := s.pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("store: cleanup legacy schema: %w", err)
		}
	}
	if err := s.backfillICloudHMEAliasOrdering(ctx); err != nil {
		return err
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
	if err := s.backfillGroupSortOrder(ctx); err != nil {
		return err
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
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
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
		`CREATE TABLE IF NOT EXISTS icloud_groups (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_accounts (
			email TEXT PRIMARY KEY,
			access_key_encrypted TEXT NOT NULL,
			group_id BIGINT NOT NULL REFERENCES icloud_groups(id),
			remark TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS sms_accounts (
			id BIGSERIAL PRIMARY KEY,
			phone TEXT NOT NULL UNIQUE,
			receive_url_encrypted TEXT NOT NULL,
			provider_host TEXT NOT NULL DEFAULT '',
			remark TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			invalid_at TIMESTAMPTZ,
			linked_mailbox_type TEXT NOT NULL DEFAULT '',
			linked_mailbox_email TEXT NOT NULL DEFAULT '',
			last_checked_at TIMESTAMPTZ,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_groups (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_source_accounts (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			apple_id_email TEXT NOT NULL UNIQUE,
			icloud_email TEXT NOT NULL,
			host TEXT NOT NULL DEFAULT 'icloud.com',
			cookies_encrypted TEXT NOT NULL DEFAULT '',
			app_password_encrypted TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			status_reason TEXT NOT NULL DEFAULT '',
			alias_total INTEGER NOT NULL DEFAULT 0,
			last_validated_at TIMESTAMPTZ,
			probe_policy_version INTEGER NOT NULL DEFAULT 2,
			probe_stage INTEGER NOT NULL DEFAULT 0,
			probe_success_streak INTEGER NOT NULL DEFAULT 0,
			probe_success_target INTEGER NOT NULL DEFAULT 3,
			probe_stable_stage INTEGER NOT NULL DEFAULT -1,
			probe_recovery_mode BOOLEAN NOT NULL DEFAULT false,
			probe_limit_started_at TIMESTAMPTZ,
			probe_last_interval_seconds INTEGER NOT NULL DEFAULT 0,
			probe_last_recovery_seconds INTEGER NOT NULL DEFAULT 0,
			probe_last_limit_stage INTEGER NOT NULL DEFAULT -1,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE SEQUENCE IF NOT EXISTS icloud_hme_alias_import_order_seq`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_aliases (
			email TEXT PRIMARY KEY,
			source_account_id BIGINT NOT NULL REFERENCES icloud_hme_source_accounts(id),
			anonymous_id TEXT NOT NULL DEFAULT '',
			label TEXT NOT NULL DEFAULT '',
			active BOOLEAN NOT NULL DEFAULT true,
			apple_status TEXT NOT NULL DEFAULT 'active',
			deactivated_at TIMESTAMPTZ,
			deleted_at TIMESTAMPTZ,
			last_synced_at TIMESTAMPTZ,
			group_id BIGINT NOT NULL REFERENCES icloud_hme_groups(id),
			remark TEXT NOT NULL DEFAULT '',
			receive_key_encrypted TEXT NOT NULL DEFAULT '',
			receive_key_digest TEXT NOT NULL DEFAULT '',
			receive_key_updated_at TIMESTAMPTZ,
			gpt_status TEXT NOT NULL DEFAULT 'unregistered',
			gpt_plus_activated_at TIMESTAMPTZ,
			gpt_deactivated_at TIMESTAMPTZ,
			gpt_plan_message_uid TEXT NOT NULL DEFAULT '',
			gpt_deactivation_message_uid TEXT NOT NULL DEFAULT '',
			gpt_last_scanned_at TIMESTAMPTZ,
			gpt_scan_error TEXT NOT NULL DEFAULT '',
			group_moved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			import_order BIGINT NOT NULL DEFAULT nextval('icloud_hme_alias_import_order_seq'),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS sms_account_bindings (
			sms_account_id BIGINT NOT NULL REFERENCES sms_accounts(id) ON DELETE CASCADE,
			mailbox_email TEXT NOT NULL REFERENCES icloud_hme_aliases(email) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (sms_account_id, mailbox_email),
			UNIQUE (mailbox_email)
		)`,
		`CREATE TABLE IF NOT EXISTS sms_account_binding_history (
			id BIGSERIAL PRIMARY KEY,
			sms_account_id BIGINT NOT NULL REFERENCES sms_accounts(id) ON DELETE CASCADE,
			mailbox_email TEXT NOT NULL,
			bound_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			unbound_at TIMESTAMPTZ,
			end_reason TEXT NOT NULL DEFAULT '',
			UNIQUE (sms_account_id, mailbox_email, bound_at)
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_create_jobs (
			id BIGSERIAL PRIMARY KEY,
			mode TEXT NOT NULL,
			source_account_id BIGINT REFERENCES icloud_hme_source_accounts(id) ON DELETE SET NULL,
			label_prefix TEXT NOT NULL,
			group_name TEXT NOT NULL DEFAULT '默认分组',
			requested_count INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			completed_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			cancelled_count INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_create_job_items (
			id BIGSERIAL PRIMARY KEY,
			job_id BIGINT NOT NULL REFERENCES icloud_hme_create_jobs(id) ON DELETE CASCADE,
			sequence INTEGER NOT NULL,
			source_account_id BIGINT REFERENCES icloud_hme_source_accounts(id) ON DELETE SET NULL,
			label TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(job_id, sequence)
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_audit_logs (
			id BIGSERIAL PRIMARY KEY,
			actor TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target TEXT NOT NULL,
			result TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_automation_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			enabled BOOLEAN NOT NULL DEFAULT false,
			target_available_count INTEGER NOT NULL DEFAULT 20,
			target_group TEXT NOT NULL DEFAULT '默认分组',
			label_prefix TEXT NOT NULL DEFAULT 'MailBox',
			error_window_started_at TIMESTAMPTZ,
			error_window_count INTEGER NOT NULL DEFAULT 0,
			updated_by TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS icloud_hme_automation_events (
			id BIGSERIAL PRIMARY KEY,
			source_account_id BIGINT REFERENCES icloud_hme_source_accounts(id) ON DELETE SET NULL,
			job_item_id BIGINT REFERENCES icloud_hme_create_job_items(id) ON DELETE SET NULL,
			event_type TEXT NOT NULL,
			result TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			next_attempt_at TIMESTAMPTZ,
			retry_count INTEGER NOT NULL DEFAULT 0,
			probe_stage INTEGER NOT NULL DEFAULT -1,
			interval_seconds INTEGER NOT NULL DEFAULT 0,
			recovery_seconds INTEGER NOT NULL DEFAULT 0,
			target_interval_min_seconds INTEGER NOT NULL DEFAULT 0,
			target_interval_max_seconds INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`}
}

func migrationColumnStatements() []string {
	return []string{
		`ALTER TABLE groups ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS auth_email TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS parent_email TEXT`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS split_index INTEGER`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS split_generated_at TIMESTAMPTZ`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS remark TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mail_accounts ADD COLUMN IF NOT EXISTS password_encrypted TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sms_accounts ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE sms_accounts ADD COLUMN IF NOT EXISTS invalid_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_created_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS automation_enabled BOOLEAN NOT NULL DEFAULT true`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS next_create_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS create_window_started_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS create_window_success_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS cooldown_level INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS consecutive_limit_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_limit_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_auto_attempt_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_policy_version INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ALTER COLUMN probe_policy_version SET DEFAULT 2`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_stage INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_success_streak INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_success_target INTEGER NOT NULL DEFAULT 3`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_stable_stage INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_recovery_mode BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_limit_started_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_last_interval_seconds INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_last_recovery_seconds INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_last_limit_stage INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS apple_status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS deactivated_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS receive_key_encrypted TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS receive_key_digest TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS receive_key_updated_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS inventory_status TEXT NOT NULL DEFAULT 'available'`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS sold_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_status TEXT NOT NULL DEFAULT 'unregistered'`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_plus_activated_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_deactivated_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_plan_message_uid TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_deactivation_message_uid TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_last_scanned_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_scan_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS group_moved_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_aliases ADD COLUMN IF NOT EXISTS import_order BIGINT`,
		`ALTER TABLE icloud_hme_create_jobs ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'manual'`,
		`ALTER TABLE icloud_hme_create_job_items ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ`,
		`ALTER TABLE icloud_hme_create_job_items ADD COLUMN IF NOT EXISTS retry_class TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE icloud_hme_automation_events ADD COLUMN IF NOT EXISTS probe_stage INTEGER NOT NULL DEFAULT -1`,
		`ALTER TABLE icloud_hme_automation_events ADD COLUMN IF NOT EXISTS interval_seconds INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_automation_events ADD COLUMN IF NOT EXISTS recovery_seconds INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_automation_events ADD COLUMN IF NOT EXISTS target_interval_min_seconds INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE icloud_hme_automation_events ADD COLUMN IF NOT EXISTS target_interval_max_seconds INTEGER NOT NULL DEFAULT 0`,
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
		`CREATE INDEX IF NOT EXISTS idx_icloud_accounts_group_id ON icloud_accounts(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_accounts_created_at ON icloud_accounts(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sms_accounts_created_at ON sms_accounts(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sms_accounts_status ON sms_accounts(status, created_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_sms_accounts_linked_mailbox
			ON sms_accounts(linked_mailbox_type, linked_mailbox_email)
			WHERE linked_mailbox_type <> '' AND linked_mailbox_email <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_sms_account_bindings_account
			ON sms_account_bindings(sms_account_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sms_account_binding_history_account
			ON sms_account_binding_history(sms_account_id, bound_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sms_account_binding_history_mailbox
			ON sms_account_binding_history(lower(mailbox_email), bound_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_source_account_id ON icloud_hme_aliases(source_account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_group_id ON icloud_hme_aliases(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_created_at ON icloud_hme_aliases(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_aliases_group_order
			ON icloud_hme_aliases(group_id, group_moved_at DESC, import_order ASC)`,
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
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_automation_events_created ON icloud_hme_automation_events(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_icloud_hme_audit_created_at ON icloud_hme_audit_logs(created_at DESC)`}
}

func legacyCleanupStatements() []string {
	return []string{
		`DROP TABLE IF EXISTS gpt_accounts`,
		`UPDATE icloud_hme_source_accounts
		 SET status_reason = 'Apple 会话异常，请重新验证', updated_at = now()
		 WHERE status_reason ~* '(trustTokens|X-APPLE|Bearer|Set-Cookie|webauth-token|scnt)'`,
		`INSERT INTO icloud_hme_automation_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO sms_account_bindings (sms_account_id, mailbox_email, created_at)
		 SELECT account.id, alias.email, COALESCE(account.last_checked_at, now())
		 FROM sms_accounts account
		 JOIN icloud_hme_aliases alias ON lower(alias.email) = lower(account.linked_mailbox_email)
		 WHERE account.linked_mailbox_type = 'icloud_hme'
		   AND account.linked_mailbox_email <> ''
		 ON CONFLICT DO NOTHING`,
		`UPDATE sms_account_bindings binding
		 SET created_at = account.last_checked_at
		 FROM sms_accounts account
		 WHERE binding.sms_account_id = account.id
		   AND account.linked_mailbox_type = 'icloud_hme'
		   AND lower(account.linked_mailbox_email) = lower(binding.mailbox_email)
		   AND account.last_checked_at IS NOT NULL`,
		`UPDATE sms_accounts account
		 SET linked_mailbox_type = '', linked_mailbox_email = ''
		 WHERE account.linked_mailbox_type = 'icloud_hme'
		   AND EXISTS (
		     SELECT 1 FROM sms_account_bindings binding
		     WHERE binding.sms_account_id = account.id
		       AND lower(binding.mailbox_email) = lower(account.linked_mailbox_email)
		   )`,
		`INSERT INTO sms_account_binding_history (sms_account_id, mailbox_email, bound_at)
		 SELECT binding.sms_account_id, binding.mailbox_email, binding.created_at
		 FROM sms_account_bindings binding
		 ON CONFLICT (sms_account_id, mailbox_email, bound_at) DO NOTHING`,
		`UPDATE icloud_hme_create_job_items
		 SET status = 'pending', retry_class = 'rate_limit',
		     error_code = 'icloud_alias_rate_limited',
		     next_attempt_at = COALESCE(finished_at, updated_at, now()) + interval '5 minutes',
		     finished_at = NULL, updated_at = now()
		 WHERE status = 'failed'
		   AND (error_code = 'icloud_alias_rate_limited'
		        OR error_message ILIKE '%reached the limit of addresses%')`,
		`UPDATE icloud_hme_create_jobs job
		 SET status = 'pending', origin = 'automation', finished_at = NULL, updated_at = now()
		 WHERE EXISTS (
		   SELECT 1 FROM icloud_hme_create_job_items item
		   WHERE item.job_id = job.id AND item.status = 'pending'
		 ) AND job.status IN ('partial_failed', 'failed')`,
		`UPDATE icloud_hme_source_accounts source
		 SET status = 'cooldown', status_reason = 'Apple 暂时限制创建，系统将在冷却后自动探测',
		     next_create_at = latest.next_attempt_at,
		     cooldown_level = GREATEST(cooldown_level, 1),
		     consecutive_limit_count = GREATEST(consecutive_limit_count, 1),
		     last_limit_at = COALESCE(last_limit_at, now()), updated_at = now()
		 FROM (
		   SELECT source_account_id, max(next_attempt_at) AS next_attempt_at
		   FROM icloud_hme_create_job_items
		   WHERE status = 'pending' AND retry_class = 'rate_limit' AND source_account_id IS NOT NULL
		   GROUP BY source_account_id
		 ) latest
		 WHERE source.id = latest.source_account_id`,
		`UPDATE icloud_hme_source_accounts source
		 SET next_create_at = GREATEST(now(), COALESCE(last_limit_at, now()) +
		       CASE
		         WHEN cooldown_level <= 1 THEN interval '10 minutes'
		         WHEN cooldown_level = 2 THEN interval '15 minutes'
		         WHEN cooldown_level = 3 THEN interval '20 minutes'
		         WHEN cooldown_level = 4 THEN interval '30 minutes'
		         WHEN cooldown_level = 5 THEN interval '45 minutes'
		         ELSE interval '1 hour'
		       END),
		     updated_at = now()
		 WHERE cooldown_level > 0 AND last_limit_at IS NOT NULL`,
		`UPDATE icloud_hme_create_job_items item
		 SET next_attempt_at = source.next_create_at, updated_at = now()
		 FROM icloud_hme_source_accounts source
		 WHERE item.source_account_id = source.id
		   AND item.status = 'pending' AND item.retry_class = 'rate_limit'`,
		`UPDATE icloud_hme_automation_events
		 SET result = 'waiting', error_code = 'icloud_source_wait',
		     message = '主账号正在等待下一次单项探测，队列将自动继续'
		 WHERE error_code IN ('icloud_no_healthy_source', 'icloud_source_wait')
		   AND event_type = 'queue'`,
		`UPDATE icloud_hme_create_job_items item
		 SET next_attempt_at = LEAST(
		       COALESCE(item.next_attempt_at, now() + interval '8 minutes'),
		       now() + interval '8 minutes'
		     ),
		     updated_at = now()
		 FROM icloud_hme_create_jobs job
		 WHERE item.job_id = job.id
		   AND job.origin = 'automation'
		   AND item.status = 'pending'
		   AND item.retry_class = 'source_wait'
		   AND EXISTS (
		     SELECT 1 FROM icloud_hme_source_accounts source
		     WHERE source.probe_policy_version = 0
		   )`,
		`UPDATE icloud_hme_source_accounts
		 SET probe_policy_version = 1,
		     probe_stage = 0,
		     probe_success_streak = 0,
		     probe_success_target = 3,
		     probe_stable_stage = -1,
		     probe_recovery_mode = cooldown_level > 0,
		     probe_limit_started_at = CASE
		       WHEN cooldown_level > 0 THEN COALESCE(last_limit_at, now())
		       ELSE NULL
		     END,
		     probe_last_limit_stage = CASE WHEN cooldown_level > 0 THEN 0 ELSE -1 END,
		     next_create_at = CASE
		       WHEN automation_enabled AND status = 'active'
		       THEN LEAST(COALESCE(next_create_at, now() + interval '8 minutes'), now() + interval '8 minutes')
		       ELSE next_create_at
		     END,
		     updated_at = now()
		 WHERE probe_policy_version = 0`,
		`WITH pending AS (
		   SELECT item.id, job.label_prefix,
		          row_number() OVER (PARTITION BY job.label_prefix ORDER BY item.id) AS row_number_value
		   FROM icloud_hme_create_job_items item
		   JOIN icloud_hme_create_jobs job ON job.id = item.job_id
		   WHERE job.origin = 'automation' AND item.status IN ('pending', 'running')
		 ),
		 prefixes AS (
		   SELECT DISTINCT label_prefix FROM pending
		 ),
		 max_sequences AS (
		   SELECT prefix.label_prefix, COALESCE((
		     SELECT max(suffix::integer)
		     FROM (
		       SELECT substring(alias.label FROM char_length(prefix.label_prefix) + 3) AS suffix
		       FROM icloud_hme_aliases alias
		       WHERE left(alias.label, char_length(prefix.label_prefix) + 2) = prefix.label_prefix || ' #'
		       UNION ALL
		       SELECT substring(item.label FROM char_length(prefix.label_prefix) + 3) AS suffix
		       FROM icloud_hme_create_job_items item
		       WHERE left(item.label, char_length(prefix.label_prefix) + 2) = prefix.label_prefix || ' #'
		     ) labels
		     WHERE suffix ~ '^[0-9]+$'
		   ), 0) AS max_sequence
		   FROM prefixes prefix
		 ),
		 assignments AS (
		   SELECT pending.id,
		          pending.label_prefix || ' #' ||
		          (max_sequences.max_sequence + pending.row_number_value)::text AS label
		   FROM pending
		   JOIN max_sequences USING (label_prefix)
		 )
		 UPDATE icloud_hme_create_job_items item
		 SET label = assignments.label, updated_at = now()
		 FROM assignments
		 WHERE item.id = assignments.id
		   AND EXISTS (
		     SELECT 1 FROM icloud_hme_source_accounts source
		     WHERE source.probe_policy_version < 2
		   )`,
		`WITH ranked AS (
		   SELECT item.id,
		          row_number() OVER (
		            PARTITION BY lower(item.email)
		            ORDER BY COALESCE(item.finished_at, item.updated_at), item.id
		          ) AS occurrence
		   FROM icloud_hme_create_job_items item
		   JOIN icloud_hme_create_jobs job ON job.id = item.job_id
		   WHERE job.origin = 'automation'
		     AND item.status = 'completed' AND item.email <> ''
		 )
		 UPDATE icloud_hme_automation_events event
		 SET result = 'recovered',
		     message = '恢复已有隐藏邮箱，未新增库存'
		 FROM ranked
		 WHERE event.job_item_id = ranked.id
		   AND ranked.occurrence > 1
		   AND event.event_type = 'create' AND event.result = 'success'
		   AND EXISTS (
		     SELECT 1 FROM icloud_hme_source_accounts source
		     WHERE source.probe_policy_version < 2
		   )`,
		`UPDATE icloud_hme_source_accounts source
		 SET probe_policy_version = 2,
		     probe_stage = 0,
		     probe_success_streak = 0,
		     probe_success_target = 3,
		     probe_stable_stage = -1,
		     probe_recovery_mode = cooldown_level > 0,
		     probe_limit_started_at = CASE
		       WHEN cooldown_level > 0 THEN COALESCE(last_limit_at, now())
		       ELSE NULL
		     END,
		     last_created_at = (
		       SELECT max(alias.created_at)
		       FROM icloud_hme_aliases alias
		       WHERE alias.source_account_id = source.id
		     ),
		     next_create_at = CASE
		       WHEN automation_enabled AND status = 'active' THEN now() + interval '8 minutes'
		       ELSE next_create_at
		     END,
		     updated_at = now()
		 WHERE probe_policy_version < 2`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_icloud_hme_active_job_item_label
		 ON icloud_hme_create_job_items(label)
		 WHERE status IN ('pending', 'running')`,
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
	var group Group
	err := s.pool.QueryRow(ctx, `
		INSERT INTO groups (name, sort_order)
		VALUES ($1, COALESCE((SELECT max(sort_order) + 1 FROM groups), 0))
		ON CONFLICT (name) DO UPDATE SET updated_at = groups.updated_at
		RETURNING id, name, sort_order, created_at, updated_at
	`, name).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
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
		WHERE id = $2 AND name <> $3
		RETURNING id, name, sort_order, created_at, updated_at
	`, name, id, DefaultGroupName).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, errors.New("分组不存在")
	}
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
		result, err := transaction.Exec(ctx, `UPDATE groups SET sort_order = $1, updated_at = now() WHERE id = $2`, index, id)
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

func (s *Store) ExportAccounts(ctx context.Context, emails []string) (string, error) {
	normalizedEmails := uniqueNormalizedEmails(emails)
	query := `
		SELECT email, password, password_encrypted, client_id, refresh_token_encrypted
		FROM mail_accounts
	`
	args := []any{}
	if len(normalizedEmails) > 0 {
		query += `WHERE lower(email) = ANY($1) `
		args = append(args, normalizedEmails)
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
		INSERT INTO groups (name, sort_order)
		VALUES ($1, COALESCE((SELECT max(sort_order) + 1 FROM groups), 0))
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

func (s *Store) backfillGroupSortOrder(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		WITH ordered AS (
			SELECT id, row_number() OVER (ORDER BY name ASC) - 1 AS next_order
			FROM groups
		)
		UPDATE groups
		SET sort_order = ordered.next_order
		FROM ordered
		WHERE groups.id = ordered.id
			AND groups.sort_order = 0
			AND NOT EXISTS (SELECT 1 FROM groups WHERE sort_order <> 0)
	`)
	if err != nil {
		return fmt.Errorf("store: backfill group sort order: %w", err)
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
