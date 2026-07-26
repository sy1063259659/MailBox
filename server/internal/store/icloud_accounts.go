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

const DefaultICloudGroupName = "默认分组"

type ICloudGroup struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ICloudAccount struct {
	Email     string    `json:"email"`
	Key       string    `json:"key"`
	Group     string    `json:"group"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ICloudAccountInput struct {
	Email string
	Key   string
	Group string
}

func (s *Store) ListICloudGroups(ctx context.Context) ([]ICloudGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, sort_order, created_at, updated_at FROM icloud_groups ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud groups: %w", err)
	}
	defer rows.Close()

	groups := []ICloudGroup{}
	for rows.Next() {
		var group ICloudGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan iCloud group: %w", err)
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *Store) CreateICloudGroup(ctx context.Context, name string) (ICloudGroup, error) {
	name = normalizeICloudGroup(name)
	if _, err := s.ensureICloudGroup(ctx, name); err != nil {
		return ICloudGroup{}, fmt.Errorf("store: create iCloud group: %w", err)
	}
	var group ICloudGroup
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, sort_order, created_at, updated_at FROM icloud_groups WHERE name = ?
	`, name).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return ICloudGroup{}, fmt.Errorf("store: create iCloud group: %w", err)
	}
	return group, nil
}

func (s *Store) RenameICloudGroup(ctx context.Context, id int64, name string) (ICloudGroup, error) {
	name = normalizeICloudGroup(name)
	tag, err := s.pool.Exec(ctx, `
		UPDATE icloud_groups SET name = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND name <> ?
	`, name, id, DefaultICloudGroupName)
	if err != nil {
		return ICloudGroup{}, fmt.Errorf("store: rename iCloud group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ICloudGroup{}, errors.New("分组不存在")
	}
	var group ICloudGroup
	err = s.pool.QueryRow(ctx, `
		SELECT id, name, sort_order, created_at, updated_at FROM icloud_groups WHERE id = ?
	`, id).Scan(&group.ID, &group.Name, &group.SortOrder, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return ICloudGroup{}, fmt.Errorf("store: rename iCloud group: %w", err)
	}
	return group, nil
}

func (s *Store) ReorderICloudGroups(ctx context.Context, ids []int64) ([]ICloudGroup, error) {
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
		return nil, fmt.Errorf("store: begin reorder iCloud groups: %w", err)
	}
	defer transaction.Rollback(ctx)

	for index, id := range ids {
		result, err := transaction.Exec(ctx, `UPDATE icloud_groups SET sort_order = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, index, id)
		if err != nil {
			return nil, fmt.Errorf("store: reorder iCloud group: %w", err)
		}
		if result.RowsAffected() == 0 {
			return nil, errors.New("分组不存在")
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit reorder iCloud groups: %w", err)
	}
	return s.ListICloudGroups(ctx)
}

func (s *Store) DeleteICloudGroup(ctx context.Context, id int64) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM icloud_accounts WHERE group_id = ?`, id).Scan(&count); err != nil {
		return fmt.Errorf("store: count iCloud group accounts: %w", err)
	}
	if count > 0 {
		return errors.New("只能删除空分组")
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM icloud_groups WHERE id = ? AND name <> ?`, id, DefaultICloudGroupName)
	if err != nil {
		return fmt.Errorf("store: delete iCloud group: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("分组不存在或默认分组不可删除")
	}
	return nil
}

func (s *Store) ListICloudAccounts(ctx context.Context) ([]ICloudAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.email, a.access_key_encrypted, g.name, a.remark, a.created_at, a.updated_at
		FROM icloud_accounts a
		JOIN icloud_groups g ON g.id = a.group_id
		ORDER BY a.created_at DESC, a.email ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list iCloud accounts: %w", err)
	}
	defer rows.Close()

	accounts := []ICloudAccount{}
	for rows.Next() {
		var account ICloudAccount
		var encryptedKey string
		if err := rows.Scan(
			&account.Email,
			&encryptedKey,
			&account.Group,
			&account.Remark,
			&account.CreatedAt,
			&account.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan iCloud account: %w", err)
		}
		key, err := secure.DecryptString(s.tokenKey, encryptedKey)
		if err != nil {
			return nil, fmt.Errorf("store: decrypt iCloud key: %w", err)
		}
		account.Key = key
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) ImportICloudAccounts(ctx context.Context, inputs []ICloudAccountInput, overwrite bool) (ImportResult, error) {
	result := ImportResult{Errors: []string{}}
	if len(inputs) == 0 {
		return result, nil
	}

	transaction, err := s.pool.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("store: begin iCloud import: %w", err)
	}
	defer transaction.Rollback(ctx)

	if overwrite {
		if _, err := transaction.Exec(ctx, `DELETE FROM icloud_accounts`); err != nil {
			return result, fmt.Errorf("store: clear iCloud accounts: %w", err)
		}
	}

	for _, input := range inputs {
		var existed bool
		if err := transaction.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM icloud_accounts WHERE lower(email) = ?)`,
			normalizeEmail(input.Email),
		).Scan(&existed); err != nil {
			return result, fmt.Errorf("store: check iCloud account: %w", err)
		}

		groupName := normalizeICloudGroup(input.Group)
		groupID, err := ensureICloudGroupTx(ctx, transaction, groupName)
		if err != nil {
			return result, err
		}

		encryptedKey, err := secure.EncryptString(s.tokenKey, input.Key)
		if err != nil {
			return result, fmt.Errorf("store: encrypt iCloud key: %w", err)
		}

		if _, err := transaction.Exec(ctx, `
			INSERT INTO icloud_accounts (email, access_key_encrypted, group_id, remark)
			VALUES (?, ?, ?, '')
			ON CONFLICT (email) DO UPDATE SET
				access_key_encrypted = excluded.access_key_encrypted,
				group_id = excluded.group_id,
				updated_at = CURRENT_TIMESTAMP
		`, normalizeEmail(input.Email), encryptedKey, groupID); err != nil {
			return result, fmt.Errorf("store: import iCloud account: %w", err)
		}
		if existed {
			result.Updated++
		} else {
			result.Imported++
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return result, fmt.Errorf("store: commit iCloud import: %w", err)
	}
	return result, nil
}

func (s *Store) MoveICloudAccountsToGroup(ctx context.Context, emails []string, group string) error {
	normalizedEmails := uniqueNormalizedEmails(emails)
	if len(normalizedEmails) == 0 {
		return errors.New("请选择账号")
	}
	groupID, err := s.ensureICloudGroup(ctx, normalizeICloudGroup(group))
	if err != nil {
		return err
	}
	args := append([]any{groupID}, stringArgs(normalizedEmails)...)
	result, err := s.pool.Exec(ctx, `
		UPDATE icloud_accounts
		SET group_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) IN (`+sqlInPlaceholders(len(normalizedEmails))+`)
	`, args...)
	if err != nil {
		return fmt.Errorf("store: move iCloud accounts: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("账号不存在")
	}
	return nil
}

func (s *Store) UpdateICloudAccountRemark(ctx context.Context, email string, remark string) (ICloudAccount, error) {
	normalized := normalizeEmail(email)
	tag, err := s.pool.Exec(ctx, `
		UPDATE icloud_accounts
		SET remark = ?, updated_at = CURRENT_TIMESTAMP
		WHERE lower(email) = ?
	`, strings.TrimSpace(remark), normalized)
	if err != nil {
		return ICloudAccount{}, fmt.Errorf("store: update iCloud remark: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ICloudAccount{}, errors.New("账号不存在")
	}
	var account ICloudAccount
	var encryptedKey string
	err = s.pool.QueryRow(ctx, `
		SELECT a.email, a.access_key_encrypted, g.name, a.remark, a.created_at, a.updated_at
		FROM icloud_accounts a
		JOIN icloud_groups g ON g.id = a.group_id
		WHERE lower(a.email) = ?
	`, normalized).Scan(
		&account.Email,
		&encryptedKey,
		&account.Group,
		&account.Remark,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if err != nil {
		return ICloudAccount{}, fmt.Errorf("store: reload iCloud account: %w", err)
	}
	key, err := secure.DecryptString(s.tokenKey, encryptedKey)
	if err != nil {
		return ICloudAccount{}, fmt.Errorf("store: decrypt iCloud key: %w", err)
	}
	account.Key = key
	return account, nil
}

type ICloudCredentials struct {
	Email string
	Key   string
}

func (s *Store) GetICloudCredentials(ctx context.Context, email string) (ICloudCredentials, error) {
	var credentials ICloudCredentials
	var encryptedKey string
	err := s.pool.QueryRow(ctx, `
		SELECT email, access_key_encrypted
		FROM icloud_accounts
		WHERE lower(email) = ?
	`, normalizeEmail(email)).Scan(&credentials.Email, &encryptedKey)
	if errors.Is(err, sql.ErrNoRows) {
		return ICloudCredentials{}, errors.New("账号不存在")
	}
	if err != nil {
		return ICloudCredentials{}, fmt.Errorf("store: get iCloud credentials: %w", err)
	}
	key, err := secure.DecryptString(s.tokenKey, encryptedKey)
	if err != nil {
		return ICloudCredentials{}, fmt.Errorf("store: decrypt iCloud key: %w", err)
	}
	credentials.Key = key
	return credentials, nil
}

func (s *Store) DeleteICloudAccount(ctx context.Context, email string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM icloud_accounts WHERE lower(email) = ?`, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("store: delete iCloud account: %w", err)
	}
	if result.RowsAffected() == 0 {
		return errors.New("账号不存在")
	}
	return nil
}

func (s *Store) ensureICloudGroup(ctx context.Context, name string) (int64, error) {
	name = normalizeICloudGroup(name)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO icloud_groups (name, sort_order)
		VALUES (?, COALESCE((SELECT max(sort_order) + 1 FROM icloud_groups), 0))
		ON CONFLICT (name) DO NOTHING
	`, name); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud group: %w", err)
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM icloud_groups WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud group: %w", err)
	}
	return id, nil
}

func ensureICloudGroupTx(ctx context.Context, tx *dbTx, name string) (int64, error) {
	name = normalizeICloudGroup(name)
	if _, err := tx.Exec(ctx, `
		INSERT INTO icloud_groups (name, sort_order)
		VALUES (?, COALESCE((SELECT max(sort_order) + 1 FROM icloud_groups), 0))
		ON CONFLICT (name) DO NOTHING
	`, name); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud group: %w", err)
	}
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM icloud_groups WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: ensure iCloud group: %w", err)
	}
	return id, nil
}

func normalizeICloudGroup(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return DefaultICloudGroupName
	}
	return group
}
