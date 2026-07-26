package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gptbox-server/internal/secure"
)

const DefaultICloudHMEGroupName = "默认分组"

type ICloudHMEGroup struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ICloudHMESourceAccount struct {
	ID                    int64      `json:"id"`
	Name                  string     `json:"name"`
	AppleIDEmail          string     `json:"appleIdEmail"`
	ICloudEmail           string     `json:"icloudEmail"`
	Host                  string     `json:"host"`
	CookieConfigured      bool       `json:"cookieConfigured"`
	AppPasswordConfigured bool       `json:"appPasswordConfigured"`
	Status                string     `json:"status"`
	StatusReason          string     `json:"statusReason,omitempty"`
	AliasTotal            int        `json:"aliasTotal"`
	LastValidatedAt       *time.Time `json:"lastValidatedAt,omitempty"`
	LastSyncedAt          *time.Time `json:"lastSyncedAt,omitempty"`
	LastCreatedAt         *time.Time `json:"lastCreatedAt,omitempty"`
	LastErrorAt           *time.Time `json:"lastErrorAt,omitempty"`
	AutomationEnabled     bool       `json:"automationEnabled"`
	NextCreateAt          *time.Time `json:"nextCreateAt,omitempty"`
	CooldownLevel         int        `json:"cooldownLevel"`
	ConsecutiveLimitCount int        `json:"consecutiveLimitCount"`
	LastLimitAt           *time.Time `json:"lastLimitAt,omitempty"`
	LastAutoAttemptAt     *time.Time `json:"lastAutoAttemptAt,omitempty"`
	ProbeStage            int        `json:"probeStage"`
	ProbeSuccessStreak    int        `json:"probeSuccessStreak"`
	ProbeSuccessTarget    int        `json:"probeSuccessTarget"`
	ProbeStableStage      int        `json:"probeStableStage"`
	ProbeRecoveryMode     bool       `json:"probeRecoveryMode"`
	ProbeLastIntervalSecs int        `json:"probeLastIntervalSeconds"`
	ProbeLastRecoverySecs int        `json:"probeLastRecoverySeconds"`
	ProbeLastLimitStage   int        `json:"probeLastLimitStage"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type ICloudHMESourceCredentials struct {
	ICloudHMESourceAccount
	CookiesJSON string `json:"-"`
	AppPassword string `json:"-"`
}

type ICloudHMEAlias struct {
	Email                string     `json:"email"`
	SourceAccountID      int64      `json:"sourceAccountId"`
	SourceAccountName    string     `json:"sourceAccountName"`
	AnonymousID          string     `json:"anonymousId"`
	Label                string     `json:"label"`
	Active               bool       `json:"active"`
	AppleStatus          string     `json:"appleStatus"`
	DeactivatedAt        *time.Time `json:"deactivatedAt,omitempty"`
	DeletedAt            *time.Time `json:"deletedAt,omitempty"`
	LastSyncedAt         *time.Time `json:"lastSyncedAt,omitempty"`
	Group                string     `json:"group"`
	Remark               string     `json:"remark"`
	MailReady            bool       `json:"mailReady"`
	ReceiveKeyConfigured bool       `json:"receiveKeyConfigured"`
	ReceiveKeyUpdatedAt  *time.Time `json:"receiveKeyUpdatedAt,omitempty"`
	InventoryStatus      string     `json:"inventoryStatus"`
	SoldAt               *time.Time `json:"soldAt,omitempty"`
	GPTStatus            string     `json:"gptStatus"`
	GPTPlusActivatedAt   *time.Time `json:"gptPlusActivatedAt,omitempty"`
	GPTDeactivatedAt     *time.Time `json:"gptDeactivatedAt,omitempty"`
	GPTLastScannedAt     *time.Time `json:"gptLastScannedAt,omitempty"`
	GPTScanError         string     `json:"gptScanError,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type ICloudHMEAliasInput struct {
	Email       string
	AnonymousID string
	Label       string
	Active      bool
	CreatedAt   time.Time
}

type ICloudHMEMailCredentials struct {
	AliasEmail  string
	ICloudEmail string
	AppPassword string `json:"-"`
}

func (s *Store) ListICloudHMEGroups(ctx context.Context) ([]ICloudHMEGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, sort_order, created_at, updated_at FROM icloud_hme_groups ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud HME groups: %w", err)
	}
	defer rows.Close()

	groups := []ICloudHMEGroup{}
	for rows.Next() {
		var group ICloudHMEGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan iCloud HME group: %w", err)
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *Store) CreateICloudHMEGroup(ctx context.Context, name string) (ICloudHMEGroup, error) {
	name = normalizeICloudHMEGroup(name)
	if _, err := s.ensureICloudHMEGroup(ctx, name); err != nil {
		return ICloudHMEGroup{}, fmt.Errorf("store: create iCloud HME group: %w", err)
	}
	var group ICloudHMEGroup
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, sort_order, created_at, updated_at FROM icloud_hme_groups WHERE name = ?
	`, name).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return ICloudHMEGroup{}, fmt.Errorf("store: create iCloud HME group: %w", err)
	}
	return group, nil
}

func (s *Store) RenameICloudHMEGroup(ctx context.Context, id int64, name string) (ICloudHMEGroup, error) {
	name = normalizeICloudHMEGroup(name)
	tag, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_groups SET name = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND name <> ?
	`, name, id, DefaultICloudHMEGroupName)
	if err != nil {
		return ICloudHMEGroup{}, fmt.Errorf("store: rename iCloud HME group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ICloudHMEGroup{}, errors.New("分组不存在或默认分组不允许重命名")
	}
	var group ICloudHMEGroup
	err = s.pool.QueryRow(ctx, `
		SELECT id, name, sort_order, created_at, updated_at FROM icloud_hme_groups WHERE id = ?
	`, id).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return ICloudHMEGroup{}, fmt.Errorf("store: rename iCloud HME group: %w", err)
	}
	return group, nil
}

func (s *Store) ReorderICloudHMEGroups(ctx context.Context, ids []int64) ([]ICloudHMEGroup, error) {
	if len(ids) == 0 {
		return nil, errors.New("分组顺序不能为空")
	}
	seen := make(map[int64]bool, len(ids))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin reorder iCloud HME groups: %w", err)
	}
	defer tx.Rollback(ctx)

	for index, id := range ids {
		if id <= 0 || seen[id] {
			return nil, errors.New("分组 id 非法或重复")
		}
		seen[id] = true
		result, err := tx.Exec(ctx, `UPDATE icloud_hme_groups SET sort_order = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, index, id)
		if err != nil {
			return nil, fmt.Errorf("store: reorder iCloud HME group: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, errors.New("分组不存在")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit reorder iCloud HME groups: %w", err)
	}
	return s.ListICloudHMEGroups(ctx)
}

func (s *Store) DeleteICloudHMEGroup(ctx context.Context, id int64) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM icloud_hme_aliases WHERE group_id = ?`, id).Scan(&count); err != nil {
		return fmt.Errorf("store: count iCloud HME group aliases: %w", err)
	}
	if count > 0 {
		return errors.New("只能删除空分组")
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM icloud_hme_groups WHERE id = ? AND name <> ?`, id, DefaultICloudHMEGroupName)
	if err != nil {
		return fmt.Errorf("store: delete iCloud HME group: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("分组不存在或默认分组不能删除")
	}
	return nil
}

const iCloudHMESourceAccountColumns = `
	id, name, apple_id_email, icloud_email, host,
	cookies_encrypted <> '', app_password_encrypted <> '',
	status, status_reason, alias_total, last_validated_at,
	last_synced_at, last_created_at, last_error_at,
	automation_enabled, next_create_at, cooldown_level,
	consecutive_limit_count, last_limit_at, last_auto_attempt_at,
	probe_stage, probe_success_streak, probe_success_target,
	probe_stable_stage, probe_recovery_mode,
	probe_last_interval_seconds, probe_last_recovery_seconds,
	probe_last_limit_stage,
	created_at, updated_at
`

func scanICloudHMESourceAccount(row interface{ Scan(...any) error }, account *ICloudHMESourceAccount) error {
	return row.Scan(
		&account.ID, &account.Name, &account.AppleIDEmail, &account.ICloudEmail, &account.Host,
		&account.CookieConfigured, &account.AppPasswordConfigured,
		&account.Status, &account.StatusReason, &account.AliasTotal,
		&account.LastValidatedAt, &account.LastSyncedAt, &account.LastCreatedAt,
		&account.LastErrorAt, &account.AutomationEnabled, &account.NextCreateAt,
		&account.CooldownLevel, &account.ConsecutiveLimitCount, &account.LastLimitAt,
		&account.LastAutoAttemptAt, &account.ProbeStage, &account.ProbeSuccessStreak,
		&account.ProbeSuccessTarget, &account.ProbeStableStage, &account.ProbeRecoveryMode,
		&account.ProbeLastIntervalSecs, &account.ProbeLastRecoverySecs,
		&account.ProbeLastLimitStage, &account.CreatedAt, &account.UpdatedAt,
	)
}

func (s *Store) ListICloudHMESourceAccounts(ctx context.Context) ([]ICloudHMESourceAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+iCloudHMESourceAccountColumns+`
		FROM icloud_hme_source_accounts
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud HME source accounts: %w", err)
	}
	defer rows.Close()

	accounts := []ICloudHMESourceAccount{}
	for rows.Next() {
		var account ICloudHMESourceAccount
		if err := scanICloudHMESourceAccount(rows, &account); err != nil {
			return nil, fmt.Errorf("store: scan iCloud HME source account: %w", err)
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) getICloudHMESourceAccount(ctx context.Context, id int64) (ICloudHMESourceAccount, error) {
	var account ICloudHMESourceAccount
	err := scanICloudHMESourceAccount(s.pool.QueryRow(ctx, `
		SELECT `+iCloudHMESourceAccountColumns+`
		FROM icloud_hme_source_accounts
		WHERE id = ?
	`, id), &account)
	if errors.Is(err, sql.ErrNoRows) {
		return ICloudHMESourceAccount{}, errors.New("iCloud 主账号不存在")
	}
	if err != nil {
		return ICloudHMESourceAccount{}, fmt.Errorf("store: get iCloud HME source account: %w", err)
	}
	return account, nil
}

func (s *Store) CreateICloudHMESourceAccount(ctx context.Context, name, appleIDEmail, iCloudEmail, host string) (ICloudHMESourceAccount, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ICloudHMESourceAccount{}, errors.New("主账号名称不能为空")
	}
	appleIDEmail = normalizeEmail(appleIDEmail)
	iCloudEmail = normalizeEmail(iCloudEmail)
	if appleIDEmail == "" || iCloudEmail == "" {
		return ICloudHMESourceAccount{}, errors.New("Apple ID 和 iCloud 邮箱不能为空")
	}
	host = normalizeICloudHMEHost(host)

	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO icloud_hme_source_accounts (name, apple_id_email, icloud_email, host)
		VALUES (?, ?, ?, ?)
		RETURNING id
	`, name, appleIDEmail, iCloudEmail, host).Scan(&id)
	if err != nil {
		return ICloudHMESourceAccount{}, fmt.Errorf("store: create iCloud HME source account: %w", err)
	}
	return s.getICloudHMESourceAccount(ctx, id)
}

func (s *Store) GetICloudHMESourceCredentials(ctx context.Context, id int64) (ICloudHMESourceCredentials, error) {
	var credentials ICloudHMESourceCredentials
	var encryptedCookies, encryptedAppPassword string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, apple_id_email, icloud_email, host,
		       cookies_encrypted, app_password_encrypted,
		       status, status_reason, alias_total, last_validated_at,
		       last_synced_at, last_created_at, last_error_at,
		       automation_enabled, next_create_at, cooldown_level,
		       consecutive_limit_count, last_limit_at, last_auto_attempt_at,
		       probe_stage, probe_success_streak, probe_success_target,
		       probe_stable_stage, probe_recovery_mode,
		       probe_last_interval_seconds, probe_last_recovery_seconds,
		       probe_last_limit_stage,
		       created_at, updated_at
		FROM icloud_hme_source_accounts
		WHERE id = ?
	`, id).Scan(
		&credentials.ID, &credentials.Name, &credentials.AppleIDEmail, &credentials.ICloudEmail, &credentials.Host,
		&encryptedCookies, &encryptedAppPassword,
		&credentials.Status, &credentials.StatusReason, &credentials.AliasTotal,
		&credentials.LastValidatedAt, &credentials.LastSyncedAt, &credentials.LastCreatedAt,
		&credentials.LastErrorAt, &credentials.AutomationEnabled, &credentials.NextCreateAt,
		&credentials.CooldownLevel, &credentials.ConsecutiveLimitCount, &credentials.LastLimitAt,
		&credentials.LastAutoAttemptAt, &credentials.ProbeStage, &credentials.ProbeSuccessStreak,
		&credentials.ProbeSuccessTarget, &credentials.ProbeStableStage, &credentials.ProbeRecoveryMode,
		&credentials.ProbeLastIntervalSecs, &credentials.ProbeLastRecoverySecs,
		&credentials.ProbeLastLimitStage, &credentials.CreatedAt, &credentials.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ICloudHMESourceCredentials{}, errors.New("iCloud 主账号不存在")
	}
	if err != nil {
		return ICloudHMESourceCredentials{}, fmt.Errorf("store: get iCloud HME source credentials: %w", err)
	}
	credentials.CookieConfigured = encryptedCookies != ""
	credentials.AppPasswordConfigured = encryptedAppPassword != ""
	if encryptedCookies != "" {
		credentials.CookiesJSON, err = secure.DecryptString(s.tokenKey, encryptedCookies)
		if err != nil {
			return ICloudHMESourceCredentials{}, fmt.Errorf("store: decrypt iCloud HME cookies: %w", err)
		}
	}
	if encryptedAppPassword != "" {
		credentials.AppPassword, err = secure.DecryptString(s.tokenKey, encryptedAppPassword)
		if err != nil {
			return ICloudHMESourceCredentials{}, fmt.Errorf("store: decrypt iCloud HME app password: %w", err)
		}
	}
	return credentials, nil
}

func (s *Store) SaveICloudHMECookies(ctx context.Context, id int64, cookies map[string]string, status, reason string) error {
	payload, err := json.Marshal(cookies)
	if err != nil {
		return fmt.Errorf("store: marshal iCloud HME cookies: %w", err)
	}
	encrypted, err := secure.EncryptString(s.tokenKey, string(payload))
	if err != nil {
		return fmt.Errorf("store: encrypt iCloud HME cookies: %w", err)
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET cookies_encrypted = ?, status = ?, status_reason = ?,
		    automation_enabled = CASE WHEN ? = 'active' THEN true ELSE automation_enabled END,
		    next_create_at = CASE WHEN ? = 'active' THEN COALESCE(next_create_at, CURRENT_TIMESTAMP) ELSE next_create_at END,
		    last_validated_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, encrypted, status, sanitizeICloudHMEStoredMessage(reason), status, status, id)
	if err != nil {
		return fmt.Errorf("store: save iCloud HME cookies: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("iCloud 主账号不存在")
	}
	return nil
}

func (s *Store) SaveICloudHMEAppPassword(ctx context.Context, id int64, password string) error {
	password = strings.TrimSpace(password)
	if password == "" {
		return errors.New("App 专用密码不能为空")
	}
	encrypted, err := secure.EncryptString(s.tokenKey, password)
	if err != nil {
		return fmt.Errorf("store: encrypt iCloud HME app password: %w", err)
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET app_password_encrypted = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, encrypted, id)
	if err != nil {
		return fmt.Errorf("store: save iCloud HME app password: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("iCloud 主账号不存在")
	}
	return nil
}

func (s *Store) UpdateICloudHMESourceStatus(ctx context.Context, id int64, status, reason string, aliasTotal *int, validated bool) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET status = ?,
		    status_reason = ?,
		    alias_total = COALESCE(?, alias_total),
		    last_validated_at = CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE last_validated_at END,
		    last_error_at = CASE WHEN ? IN ('error', 'reauth_required') THEN CURRENT_TIMESTAMP ELSE last_error_at END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, sanitizeICloudHMEStoredMessage(reason), aliasTotal, validated, status, id)
	if err != nil {
		return fmt.Errorf("store: update iCloud HME source status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("iCloud 主账号不存在")
	}
	return nil
}

func (s *Store) DeleteICloudHMESourceAccount(ctx context.Context, id int64) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM icloud_hme_aliases WHERE source_account_id = ?`, id).Scan(&count); err != nil {
		return fmt.Errorf("store: count iCloud HME aliases: %w", err)
	}
	if count > 0 {
		return errors.New("主账号下仍有隐藏邮箱，不能删除")
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM icloud_hme_create_jobs
		WHERE source_account_id = ? AND status IN ('pending', 'running', 'cancel_requested')
	`, id).Scan(&count); err != nil {
		return fmt.Errorf("store: count active iCloud HME jobs: %w", err)
	}
	if count > 0 {
		return errors.New("主账号仍有进行中的创建任务，不能删除")
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM icloud_hme_source_accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete iCloud HME source account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("iCloud 主账号不存在")
	}
	return nil
}

func (s *Store) ListICloudHMEAliases(ctx context.Context) ([]ICloudHMEAlias, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.email, a.source_account_id, s.name, a.anonymous_id, a.label, a.active,
		       a.apple_status, a.deactivated_at, a.deleted_at, a.last_synced_at,
		       g.name, a.remark, s.app_password_encrypted <> '',
		       a.receive_key_encrypted <> '', a.receive_key_updated_at,
		       a.inventory_status, a.sold_at,
		       a.gpt_status, a.gpt_plus_activated_at, a.gpt_deactivated_at,
		       a.gpt_last_scanned_at, a.gpt_scan_error,
		       a.created_at, a.updated_at
		FROM icloud_hme_aliases a
		JOIN icloud_hme_source_accounts s ON s.id = a.source_account_id
		JOIN icloud_hme_groups g ON g.id = a.group_id
		ORDER BY a.created_at DESC, a.email ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud HME aliases: %w", err)
	}
	defer rows.Close()

	aliases := []ICloudHMEAlias{}
	for rows.Next() {
		var alias ICloudHMEAlias
		if err := rows.Scan(
			&alias.Email, &alias.SourceAccountID, &alias.SourceAccountName,
			&alias.AnonymousID, &alias.Label, &alias.Active, &alias.AppleStatus,
			&alias.DeactivatedAt, &alias.DeletedAt, &alias.LastSyncedAt, &alias.Group,
			&alias.Remark, &alias.MailReady, &alias.ReceiveKeyConfigured,
			&alias.ReceiveKeyUpdatedAt, &alias.InventoryStatus, &alias.SoldAt,
			&alias.GPTStatus, &alias.GPTPlusActivatedAt, &alias.GPTDeactivatedAt,
			&alias.GPTLastScannedAt, &alias.GPTScanError,
			&alias.CreatedAt, &alias.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan iCloud HME alias: %w", err)
		}
		aliases = append(aliases, alias)
	}
	return aliases, rows.Err()
}

func (s *Store) SyncICloudHMEAliases(ctx context.Context, sourceID int64, inputs []ICloudHMEAliasInput, group string) (int, int, error) {
	return s.saveICloudHMEAliases(ctx, sourceID, inputs, group, true)
}

func (s *Store) UpsertICloudHMEAliases(ctx context.Context, sourceID int64, inputs []ICloudHMEAliasInput, group string) (int, int, error) {
	return s.saveICloudHMEAliases(ctx, sourceID, inputs, group, false)
}

func (s *Store) saveICloudHMEAliases(ctx context.Context, sourceID int64, inputs []ICloudHMEAliasInput, group string, markMissing bool) (int, int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("store: begin sync iCloud HME aliases: %w", err)
	}
	defer tx.Rollback(ctx)

	groupID, err := ensureICloudHMEGroupTx(ctx, tx, normalizeICloudHMEGroup(group))
	if err != nil {
		return 0, 0, err
	}
	imported, updated := 0, 0
	syncedEmails := make([]string, 0, len(inputs))
	for _, input := range inputs {
		email := normalizeEmail(input.Email)
		if email == "" {
			continue
		}
		var existed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM icloud_hme_aliases WHERE email = ?)`, email).Scan(&existed); err != nil {
			return 0, 0, fmt.Errorf("store: check iCloud HME alias: %w", err)
		}
		createdAt := input.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		receiveKeyEncrypted, receiveKeyDigest := "", ""
		if !existed {
			receiveKey, err := secure.GenerateReceiveKey()
			if err != nil {
				return 0, 0, fmt.Errorf("store: generate iCloud HME receive key: %w", err)
			}
			receiveKeyEncrypted, err = secure.EncryptString(s.tokenKey, receiveKey)
			if err != nil {
				return 0, 0, fmt.Errorf("store: encrypt iCloud HME receive key: %w", err)
			}
			receiveKeyDigest = secure.ReceiveKeyDigest(s.tokenKey, receiveKey)
		}
		active := input.Active
		if _, err := tx.Exec(ctx, `
			INSERT INTO icloud_hme_aliases (
				email, source_account_id, anonymous_id, label, active, apple_status, last_synced_at,
				group_id, receive_key_encrypted, receive_key_digest, receive_key_updated_at, created_at
			)
			VALUES (?, ?, ?, ?, ?, CASE WHEN ? THEN 'active' ELSE 'inactive' END, CURRENT_TIMESTAMP,
			        ?, ?, ?, CASE WHEN ? <> '' THEN CURRENT_TIMESTAMP ELSE NULL END, ?)
			ON CONFLICT (email) DO UPDATE SET
				source_account_id = excluded.source_account_id,
				anonymous_id = excluded.anonymous_id,
				label = excluded.label,
				active = excluded.active,
				apple_status = excluded.apple_status,
				deactivated_at = CASE WHEN excluded.active THEN NULL ELSE COALESCE(deactivated_at, CURRENT_TIMESTAMP) END,
				deleted_at = CASE WHEN apple_status = 'deleted' THEN deleted_at ELSE NULL END,
				last_synced_at = CURRENT_TIMESTAMP,
				updated_at = CURRENT_TIMESTAMP
		`, email, sourceID, strings.TrimSpace(input.AnonymousID), strings.TrimSpace(input.Label), active, active,
			groupID, receiveKeyEncrypted, receiveKeyDigest, receiveKeyEncrypted, createdAt); err != nil {
			return 0, 0, fmt.Errorf("store: upsert iCloud HME alias: %w", err)
		}
		syncedEmails = append(syncedEmails, email)
		if existed {
			updated++
		} else {
			imported++
		}
	}
	if markMissing {
		args := append([]any{sourceID}, stringArgs(syncedEmails)...)
		if _, err := tx.Exec(ctx, `
			UPDATE icloud_hme_aliases
			SET apple_status = 'unknown', active = false, last_synced_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
			WHERE source_account_id = ?
			  AND apple_status <> 'deleted'
			  AND lower(email) NOT IN (`+sqlInPlaceholders(len(syncedEmails))+`)
		`, args...); err != nil {
			return 0, 0, fmt.Errorf("store: mark missing iCloud HME aliases unknown: %w", err)
		}
	}
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM icloud_hme_aliases WHERE source_account_id = ?`, sourceID).Scan(&total); err != nil {
		return 0, 0, fmt.Errorf("store: count synced iCloud HME aliases: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET alias_total = ?, status = 'active', status_reason = '',
		    last_validated_at = CURRENT_TIMESTAMP,
		    last_synced_at = CASE WHEN ? THEN CURRENT_TIMESTAMP ELSE last_synced_at END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, total, markMissing, sourceID); err != nil {
		return 0, 0, fmt.Errorf("store: update iCloud HME alias total: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("store: commit sync iCloud HME aliases: %w", err)
	}
	return imported, updated, nil
}

func (s *Store) UpdateICloudHMEAliasRemark(ctx context.Context, email, remark string) (ICloudHMEAlias, error) {
	normalized := normalizeEmail(email)
	tag, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET remark = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
	`, strings.TrimSpace(remark), normalized)
	if err != nil {
		return ICloudHMEAlias{}, fmt.Errorf("store: update iCloud HME alias remark: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ICloudHMEAlias{}, errors.New("隐藏邮箱不存在")
	}
	return s.GetICloudHMEAlias(ctx, normalized)
}

func (s *Store) MoveICloudHMEAliasesToGroup(ctx context.Context, emails []string, group string) error {
	normalizedEmails := uniqueNormalizedEmails(emails)
	if len(normalizedEmails) == 0 {
		return errors.New("未选择隐藏邮箱")
	}
	groupID, err := s.ensureICloudHMEGroup(ctx, normalizeICloudHMEGroup(group))
	if err != nil {
		return err
	}
	args := append([]any{groupID}, stringArgs(normalizedEmails)...)
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_hme_aliases
		SET group_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) IN (`+sqlInPlaceholders(len(normalizedEmails))+`)
	`, args...)
	if err != nil {
		return fmt.Errorf("store: move iCloud HME aliases: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("隐藏邮箱不存在")
	}
	return nil
}

func (s *Store) DeleteICloudHMEAlias(ctx context.Context, email string) error {
	normalized := normalizeEmail(email)
	var sourceID int64
	err := s.pool.QueryRow(ctx, `SELECT source_account_id FROM icloud_hme_aliases WHERE lower(email) = ?`, normalized).Scan(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("隐藏邮箱不存在")
	}
	if err != nil {
		return fmt.Errorf("store: delete iCloud HME alias: %w", err)
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM icloud_hme_aliases WHERE lower(email) = ?`, normalized)
	if err != nil {
		return fmt.Errorf("store: delete iCloud HME alias: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("隐藏邮箱不存在")
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE icloud_hme_source_accounts
		SET alias_total = (SELECT count(*) FROM icloud_hme_aliases WHERE source_account_id = ?),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, sourceID, sourceID)
	if err != nil {
		return fmt.Errorf("store: update iCloud HME alias total after delete: %w", err)
	}
	return nil
}

func (s *Store) GetICloudHMEMailCredentials(ctx context.Context, email string) (ICloudHMEMailCredentials, error) {
	var credentials ICloudHMEMailCredentials
	var encrypted string
	err := s.pool.QueryRow(ctx, `
		SELECT a.email, s.icloud_email, s.app_password_encrypted
		FROM icloud_hme_aliases a
		JOIN icloud_hme_source_accounts s ON s.id = a.source_account_id
		WHERE lower(a.email) = ?
	`, normalizeEmail(email)).Scan(&credentials.AliasEmail, &credentials.ICloudEmail, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return ICloudHMEMailCredentials{}, errors.New("隐藏邮箱不存在")
	}
	if err != nil {
		return ICloudHMEMailCredentials{}, fmt.Errorf("store: get iCloud HME mail credentials: %w", err)
	}
	if encrypted == "" {
		return ICloudHMEMailCredentials{}, errors.New("icloud_app_password_required")
	}
	credentials.AppPassword, err = secure.DecryptString(s.tokenKey, encrypted)
	if err != nil {
		return ICloudHMEMailCredentials{}, fmt.Errorf("store: decrypt iCloud HME app password: %w", err)
	}
	return credentials, nil
}

func (s *Store) ensureICloudHMEGroup(ctx context.Context, name string) (int64, error) {
	name = normalizeICloudHMEGroup(name)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO icloud_hme_groups (name, sort_order)
		VALUES (?, COALESCE((SELECT max(sort_order) + 1 FROM icloud_hme_groups), 0))
		ON CONFLICT (name) DO NOTHING
	`, name); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud HME group: %w", err)
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM icloud_hme_groups WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud HME group: %w", err)
	}
	return id, nil
}

func ensureICloudHMEGroupTx(ctx context.Context, tx *dbTx, name string) (int64, error) {
	name = normalizeICloudHMEGroup(name)
	if _, err := tx.Exec(ctx, `
		INSERT INTO icloud_hme_groups (name, sort_order)
		VALUES (?, COALESCE((SELECT max(sort_order) + 1 FROM icloud_hme_groups), 0))
		ON CONFLICT (name) DO NOTHING
	`, name); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud HME group: %w", err)
	}
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM icloud_hme_groups WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud HME group: %w", err)
	}
	return id, nil
}

func normalizeICloudHMEGroup(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return DefaultICloudHMEGroupName
	}
	return group
}

func normalizeICloudHMEHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "icloud.com.cn" {
		return host
	}
	return "icloud.com"
}
