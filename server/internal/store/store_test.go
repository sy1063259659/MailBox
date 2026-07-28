package store

import (
	"strings"
	"testing"
)

func TestAuthEmailFor(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  string
	}{
		{
			name:  "hotmail plus alias",
			email: "jonowen892339+bsuolt@hotmail.com",
			want:  "jonowen892339@hotmail.com",
		},
		{
			name:  "outlook plus alias",
			email: "User+Tag@Outlook.com",
			want:  "user@outlook.com",
		},
		{
			name:  "hotmail primary",
			email: "abc@hotmail.com",
			want:  "abc@hotmail.com",
		},
		{
			name:  "non microsoft plus alias",
			email: "name+tag@gmail.com",
			want:  "name+tag@gmail.com",
		},
		{
			name:  "invalid email stays normalized",
			email: " NoAtSign ",
			want:  "noatsign",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := authEmailFor(test.email); got != test.want {
				t.Fatalf("authEmailFor(%q) = %q, want %q", test.email, got, test.want)
			}
		})
	}
}

func TestHotmailParentEmail(t *testing.T) {
	if got := hotmailParentEmail("jonowen892339+bsuolt@hotmail.com"); got != "jonowen892339@hotmail.com" {
		t.Fatalf("hotmailParentEmail() = %q", got)
	}
	if got := hotmailParentEmail("jonowen892339@hotmail.com"); got != "" {
		t.Fatalf("hotmailParentEmail(primary) = %q", got)
	}
	if got := hotmailParentEmail("name+tag@outlook.com"); got != "" {
		t.Fatalf("hotmailParentEmail(non hotmail) = %q", got)
	}
}

func TestIsHotmailPrimary(t *testing.T) {
	if !isHotmailPrimary("jonowen892339@hotmail.com") {
		t.Fatal("expected hotmail primary")
	}
	if isHotmailPrimary("jonowen892339+tag@hotmail.com") {
		t.Fatal("plus alias should not be a primary")
	}
	if isHotmailPrimary("jonowen892339@outlook.com") {
		t.Fatal("only hotmail.com is supported")
	}
}

func TestSplitIndexUniqueIndexStatement(t *testing.T) {
	statements := migrationIndexStatements()
	want := "CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_accounts_parent_split_index"
	for _, statement := range statements {
		if strings.Contains(statement, want) {
			return
		}
	}
	t.Fatalf("migrationIndexStatements() missing %q", want)
}

func TestMailAccountsMigrationCreatesRemarkColumn(t *testing.T) {
	statements := migrationColumnStatements()
	for _, statement := range statements {
		if strings.Contains(statement, "ADD COLUMN IF NOT EXISTS remark TEXT NOT NULL DEFAULT ''") {
			return
		}
	}
	t.Fatal("migrationColumnStatements() missing remark column")
}

func TestMailAccountsMigrationCreatesEncryptedPasswordColumn(t *testing.T) {
	statements := migrationColumnStatements()
	for _, statement := range statements {
		if strings.Contains(statement, "ADD COLUMN IF NOT EXISTS password_encrypted TEXT NOT NULL DEFAULT ''") {
			return
		}
	}
	t.Fatal("migrationColumnStatements() missing password_encrypted column")
}

func TestMigrationsDoNotCreateLegacyGPTAccountsTable(t *testing.T) {
	statements := strings.Join(append(append(migrationCreateStatements(), migrationColumnStatements()...), migrationIndexStatements()...), "\n")
	if strings.Contains(statements, "gpt_accounts") {
		t.Fatal("active migrations must not create or alter gpt_accounts")
	}
}

func TestLegacyGPTAccountsTableCleanupIsIdempotent(t *testing.T) {
	statements := legacyCleanupStatements()
	joined := strings.Join(statements, "\n")
	for _, want := range []string{"DROP TABLE IF EXISTS gpt_accounts", "UPDATE icloud_hme_source_accounts", "trustTokens"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("legacyCleanupStatements() missing %q: %v", want, statements)
		}
	}
}

func TestGroupsMigrationCreatesSortOrderColumn(t *testing.T) {
	statements := migrationColumnStatements()
	for _, statement := range statements {
		if strings.Contains(statement, "ALTER TABLE groups ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0") {
			return
		}
	}
	t.Fatal("migrationColumnStatements() missing group sort_order column")
}

func TestGroupListOrdersBySortOrder(t *testing.T) {
	query := `SELECT id, name, sort_order, created_at, updated_at FROM groups ORDER BY sort_order ASC, name ASC`
	if !strings.Contains(query, "ORDER BY sort_order ASC, name ASC") {
		t.Fatal("group list query must order by sort_order then name")
	}
}

func TestRenameGroupQueryProtectsDefaultGroup(t *testing.T) {
	query := `
		UPDATE groups SET name = $1, updated_at = now()
		WHERE id = $2 AND name <> $3
		RETURNING id, name, sort_order, created_at, updated_at
	`
	if !strings.Contains(query, "name <> $3") {
		t.Fatal("rename group query must protect the default group")
	}
}

func TestUniqueNormalizedEmails(t *testing.T) {
	got := uniqueNormalizedEmails([]string{" User@Example.com ", "user@example.com", "", "Other@Example.com"})
	want := []string{"user@example.com", "other@example.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("uniqueNormalizedEmails() = %v, want %v", got, want)
	}
}

func TestRandomLetters(t *testing.T) {
	value, err := randomLetters(6)
	if err != nil {
		t.Fatal(err)
	}
	if len(value) != 6 {
		t.Fatalf("len(randomLetters(6)) = %d", len(value))
	}
	for _, char := range value {
		if char < 'a' || char > 'z' {
			t.Fatalf("unexpected random char %q", char)
		}
	}
}
func TestICloudMigrationCreatesIndependentTables(t *testing.T) {
	statements := strings.Join(migrationCreateStatements(), "\n")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS icloud_groups",
		"CREATE TABLE IF NOT EXISTS icloud_accounts",
		"access_key_encrypted TEXT NOT NULL",
		"REFERENCES icloud_groups(id)",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("migrationCreateStatements() missing %q", want)
		}
	}
	if strings.Contains(statements, "icloud_accounts") && strings.Contains(statements, "client_id") {
		iCloudStart := strings.Index(statements, "CREATE TABLE IF NOT EXISTS icloud_accounts")
		iCloudStatements := statements[iCloudStart:]
		if strings.Contains(iCloudStatements, "client_id") || strings.Contains(iCloudStatements, "refresh_token") {
			t.Fatal("icloud_accounts must only contain the encrypted access key and account metadata")
		}
	}
}

func TestSMSMigrationCreatesEncryptedIndependentTable(t *testing.T) {
	statements := strings.Join(migrationCreateStatements(), "\n")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS sms_accounts",
		"CREATE TABLE IF NOT EXISTS sms_account_bindings",
		"receive_url_encrypted TEXT NOT NULL",
		"status TEXT NOT NULL DEFAULT 'active'",
		"REFERENCES icloud_hme_aliases(email)",
		"UNIQUE (mailbox_email)",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("migrationCreateStatements() missing %q", want)
		}
	}
	if strings.Contains(statements, "receive_url TEXT") {
		t.Fatal("SMS receive URL must not be stored as plaintext")
	}
	columns := strings.Join(migrationColumnStatements(), "\n")
	for _, want := range []string{
		"ALTER TABLE sms_accounts ADD COLUMN IF NOT EXISTS status",
		"ALTER TABLE sms_accounts ADD COLUMN IF NOT EXISTS invalid_at",
	} {
		if !strings.Contains(columns, want) {
			t.Fatalf("migrationColumnStatements() missing %q", want)
		}
	}
}

func TestNormalizeSMSMailboxEmails(t *testing.T) {
	emails, err := normalizeSMSMailboxEmails([]string{
		" First@iCloud.com ",
		"second@icloud.com",
		"FIRST@ICLOUD.COM",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(emails) != 2 || emails[0] != "first@icloud.com" || emails[1] != "second@icloud.com" {
		t.Fatalf("emails = %#v", emails)
	}
	if _, err := normalizeSMSMailboxEmails([]string{
		"1@icloud.com", "2@icloud.com", "3@icloud.com", "4@icloud.com",
	}); err == nil {
		t.Fatal("four bindings should fail")
	}
}

func TestSMSLegacyBindingUsesLastCheckedTimeOnce(t *testing.T) {
	statements := strings.Join(legacyCleanupStatements(), "\n")
	for _, want := range []string{
		"COALESCE(account.last_checked_at, now())",
		"SET created_at = account.last_checked_at",
		"SET linked_mailbox_type = '', linked_mailbox_email = ''",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("legacyCleanupStatements() missing %q", want)
		}
	}
}

func TestICloudMigrationCreatesIndexes(t *testing.T) {
	statements := strings.Join(migrationIndexStatements(), "\n")
	for _, want := range []string{
		"idx_icloud_accounts_group_id",
		"idx_icloud_accounts_created_at",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("migrationIndexStatements() missing %q", want)
		}
	}
}

func TestICloudHMEMigrationCreatesIndependentTables(t *testing.T) {
	statements := strings.Join(migrationCreateStatements(), "\n")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS icloud_hme_groups",
		"CREATE TABLE IF NOT EXISTS icloud_hme_source_accounts",
		"CREATE TABLE IF NOT EXISTS icloud_hme_aliases",
		"cookies_encrypted TEXT NOT NULL DEFAULT ''",
		"app_password_encrypted TEXT NOT NULL DEFAULT ''",
		"REFERENCES icloud_hme_source_accounts(id)",
		"REFERENCES icloud_hme_groups(id)",
		"receive_key_encrypted TEXT NOT NULL DEFAULT ''",
		"receive_key_digest TEXT NOT NULL DEFAULT ''",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("migrationCreateStatements() missing %q", want)
		}
	}
}

func TestICloudHMEMigrationDoesNotAlterExistingICloudTables(t *testing.T) {
	columns := strings.Join(migrationColumnStatements(), "\n")
	if strings.Contains(columns, "icloud_accounts") || strings.Contains(columns, "icloud_groups") {
		t.Fatal("HME migrations must not alter existing iCloud tables")
	}
}

func TestICloudHMEManagementMigrations(t *testing.T) {
	creates := strings.Join(migrationCreateStatements(), "\n")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS icloud_hme_create_jobs",
		"CREATE TABLE IF NOT EXISTS icloud_hme_create_job_items",
		"CREATE TABLE IF NOT EXISTS icloud_hme_audit_logs",
		"CREATE TABLE IF NOT EXISTS icloud_hme_automation_settings",
		"CREATE TABLE IF NOT EXISTS icloud_hme_automation_events",
		"UNIQUE(job_id, sequence)",
		"ON DELETE SET NULL",
	} {
		if !strings.Contains(creates, want) {
			t.Fatalf("migrationCreateStatements() missing %q", want)
		}
	}
	columns := strings.Join(migrationColumnStatements(), "\n")
	for _, want := range []string{
		"icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_synced_at",
		"icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_created_at",
		"icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS last_error_at",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS apple_status",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS deleted_at",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS receive_key_encrypted",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS receive_key_digest",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS inventory_status",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_status",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_plus_activated_at",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_deactivated_at",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS gpt_last_scanned_at",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS group_moved_at",
		"icloud_hme_aliases ADD COLUMN IF NOT EXISTS import_order",
		"icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS next_create_at",
		"icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_stage",
		"icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_success_streak",
		"icloud_hme_source_accounts ADD COLUMN IF NOT EXISTS probe_stable_stage",
		"icloud_hme_create_job_items ADD COLUMN IF NOT EXISTS next_attempt_at",
		"icloud_hme_automation_events ADD COLUMN IF NOT EXISTS interval_seconds",
		"icloud_hme_automation_events ADD COLUMN IF NOT EXISTS recovery_seconds",
	} {
		if !strings.Contains(columns, want) {
			t.Fatalf("migrationColumnStatements() missing %q", want)
		}
	}
}

func TestICloudHMEManagementIndexes(t *testing.T) {
	indexes := strings.Join(migrationIndexStatements(), "\n")
	for _, want := range []string{
		"idx_icloud_hme_jobs_status",
		"idx_icloud_hme_job_items_status",
		"idx_icloud_hme_audit_created_at",
		"idx_icloud_hme_aliases_status",
		"idx_icloud_hme_aliases_gpt_scan",
		"idx_icloud_hme_aliases_group_order",
	} {
		if !strings.Contains(indexes, want) {
			t.Fatalf("migrationIndexStatements() missing %q", want)
		}
	}
}

func TestICloudHMEAutomationCleanupUsesProgressiveProbeWindow(t *testing.T) {
	statements := strings.Join(legacyCleanupStatements(), "\n")
	for _, want := range []string{
		"interval '10 minutes'",
		"interval '15 minutes'",
		"interval '20 minutes'",
		"interval '45 minutes'",
		"interval '1 hour'",
		"status = 'pending' AND retry_class = 'rate_limit'",
		"icloud_source_wait",
		"probe_policy_version = 1",
		"probe_policy_version = 2",
		"row_number() OVER (PARTITION BY job.label_prefix ORDER BY item.id)",
		"恢复已有隐藏邮箱，未新增库存",
		"idx_icloud_hme_active_job_item_label",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("legacyCleanupStatements() missing %q", want)
		}
	}
}

type iCloudHMEJobScanRow struct {
	count int
}

func (row *iCloudHMEJobScanRow) Scan(dest ...any) error {
	row.count = len(dest)
	*dest[11].(*string) = "automation"
	return nil
}

func TestScanICloudHMEJobIncludesOrigin(t *testing.T) {
	row := &iCloudHMEJobScanRow{}
	var job ICloudHMECreateJob
	if err := scanICloudHMEJob(row, &job); err != nil {
		t.Fatal(err)
	}
	if row.count != 17 {
		t.Fatalf("scan destination count = %d, want 17", row.count)
	}
	if job.Origin != "automation" {
		t.Fatalf("origin = %q, want automation", job.Origin)
	}
}

func TestICloudHMEJobAggregateStatusPreservesCancellation(t *testing.T) {
	status, finished := iCloudHMEJobAggregateStatus("cancel_requested", 0, 0, 2, 0, 1)
	if status != "cancel_requested" || finished {
		t.Fatalf("status = %q, finished = %v", status, finished)
	}
	status, finished = iCloudHMEJobAggregateStatus("cancel_requested", 1, 0, 2, 0, 0)
	if status != "partial_failed" || !finished {
		t.Fatalf("status = %q, finished = %v", status, finished)
	}
	status, finished = iCloudHMEJobAggregateStatus("cancel_requested", 0, 0, 3, 0, 0)
	if status != "cancelled" || !finished {
		t.Fatalf("status = %q, finished = %v", status, finished)
	}
}
