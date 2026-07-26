package store

import (
	"errors"
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

func TestMailAccountsSchemaContainsRemarkColumn(t *testing.T) {
	statements := strings.Join(migrationCreateStatements(), "\n")
	if !strings.Contains(statements, "remark TEXT NOT NULL DEFAULT ''") {
		t.Fatal("migrationCreateStatements() missing remark column")
	}
}

func TestMailAccountsSchemaContainsEncryptedPasswordColumn(t *testing.T) {
	statements := strings.Join(migrationCreateStatements(), "\n")
	if !strings.Contains(statements, "password_encrypted TEXT NOT NULL DEFAULT ''") {
		t.Fatal("migrationCreateStatements() missing password_encrypted column")
	}
}

func TestMigrationsDoNotCreateLegacyGPTAccountsTable(t *testing.T) {
	statements := strings.Join(append(migrationCreateStatements(), migrationIndexStatements()...), "\n")
	if strings.Contains(statements, "gpt_accounts") {
		t.Fatal("active migrations must not create or alter gpt_accounts")
	}
}

// Columns added after the first release must appear both in CREATE TABLE (for
// fresh databases) and in an ALTER TABLE step (for databases already created by
// an older build).
func TestPublicMailOriginColumnIsCreatedAndBackfilled(t *testing.T) {
	creates := strings.Join(migrationCreateStatements(), "\n")
	if !strings.Contains(creates, "public_mail_origin TEXT NOT NULL DEFAULT ''") {
		t.Fatal("migrationCreateStatements() missing public_mail_origin column")
	}
	columns := strings.Join(migrationColumnStatements(), "\n")
	if !strings.Contains(columns, "ADD COLUMN public_mail_origin TEXT NOT NULL DEFAULT ''") {
		t.Fatal("migrationColumnStatements() missing public_mail_origin upgrade step")
	}
}

func TestMigrationColumnStatementsAlwaysCarryDefaults(t *testing.T) {
	// SQLite rejects ADD COLUMN ... NOT NULL without a DEFAULT.
	for _, statement := range migrationColumnStatements() {
		if strings.Contains(statement, "NOT NULL") && !strings.Contains(statement, "DEFAULT") {
			t.Fatalf("ADD COLUMN with NOT NULL needs a DEFAULT: %s", statement)
		}
	}
}

func TestIsDuplicateColumnError(t *testing.T) {
	if !isDuplicateColumnError(errors.New(`SQL logic error: duplicate column name: public_mail_origin (1)`)) {
		t.Fatal("expected duplicate column name to be tolerated")
	}
	if isDuplicateColumnError(errors.New("no such table: icloud_hme_automation_settings")) {
		t.Fatal("unrelated errors must not be treated as duplicate columns")
	}
	if isDuplicateColumnError(nil) {
		t.Fatal("nil error must not be treated as a duplicate column")
	}
}

func TestGroupsSchemaContainsSortOrderColumn(t *testing.T) {
	statements := strings.Join(migrationCreateStatements(), "\n")
	if !strings.Contains(statements, "sort_order INTEGER NOT NULL DEFAULT 0") {
		t.Fatal("migrationCreateStatements() missing group sort_order column")
	}
}

func TestGroupListOrdersBySortOrder(t *testing.T) {
	query := `SELECT id, name, sort_order, created_at, updated_at FROM groups ORDER BY sort_order ASC, name ASC`
	if !strings.Contains(query, "ORDER BY sort_order ASC, name ASC") {
		t.Fatal("group list query must order by sort_order then name")
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

func TestSQLInPlaceholders(t *testing.T) {
	if got := sqlInPlaceholders(0); got != "" {
		t.Fatalf("sqlInPlaceholders(0) = %q", got)
	}
	if got := sqlInPlaceholders(1); got != "?" {
		t.Fatalf("sqlInPlaceholders(1) = %q", got)
	}
	if got := sqlInPlaceholders(3); got != "?,?,?" {
		t.Fatalf("sqlInPlaceholders(3) = %q", got)
	}
}

func TestSQLiteTimeScan(t *testing.T) {
	var value sqliteTime
	if err := value.Scan("2026-07-26 02:00:00"); err != nil {
		t.Fatal(err)
	}
	if value.value == nil || value.value.Year() != 2026 {
		t.Fatalf("sqliteTime.Scan seconds format = %v", value.value)
	}
	if err := value.Scan("2026-07-26 02:00:00.123456789+00:00"); err != nil {
		t.Fatal(err)
	}
	if value.value == nil || value.value.Nanosecond() != 123456789 {
		t.Fatalf("sqliteTime.Scan fractional format = %v", value.value)
	}
	if err := value.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if value.value != nil {
		t.Fatalf("sqliteTime.Scan(nil) = %v", value.value)
	}
	if !value.Time().IsZero() {
		t.Fatalf("sqliteTime.Time() for nil = %v", value.Time())
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
		iCloudEnd := strings.Index(iCloudStatements, "CREATE TABLE IF NOT EXISTS icloud_hme_groups")
		if iCloudEnd > 0 {
			iCloudStatements = iCloudStatements[:iCloudEnd]
		}
		if strings.Contains(iCloudStatements, "client_id") || strings.Contains(iCloudStatements, "refresh_token") {
			t.Fatal("icloud_accounts must only contain the encrypted access key and account metadata")
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

func TestLowerEmailExpressionIndexes(t *testing.T) {
	statements := strings.Join(migrationIndexStatements(), "\n")
	for _, want := range []string{
		"idx_mail_accounts_email_lower ON mail_accounts(lower(email))",
		"idx_icloud_accounts_email_lower ON icloud_accounts(lower(email))",
		"idx_icloud_hme_aliases_email_lower ON icloud_hme_aliases(lower(email))",
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
		"last_synced_at TIMESTAMP",
		"last_created_at TIMESTAMP",
		"last_error_at TIMESTAMP",
		"apple_status TEXT NOT NULL DEFAULT 'active'",
		"inventory_status TEXT NOT NULL DEFAULT 'available'",
		"gpt_status TEXT NOT NULL DEFAULT 'unregistered'",
		"gpt_plus_activated_at TIMESTAMP",
		"gpt_deactivated_at TIMESTAMP",
		"gpt_last_scanned_at TIMESTAMP",
		"next_create_at TIMESTAMP",
		"next_attempt_at TIMESTAMP",
		"retry_class TEXT NOT NULL DEFAULT ''",
		"origin TEXT NOT NULL DEFAULT 'manual'",
		"probe_stage INTEGER NOT NULL DEFAULT 0",
		"probe_success_streak INTEGER NOT NULL DEFAULT 0",
		"probe_stable_stage INTEGER NOT NULL DEFAULT -1",
		"interval_seconds INTEGER NOT NULL DEFAULT 0",
		"recovery_seconds INTEGER NOT NULL DEFAULT 0",
	} {
		if !strings.Contains(creates, want) {
			t.Fatalf("migrationCreateStatements() missing %q", want)
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
		"idx_icloud_hme_active_job_item_label",
	} {
		if !strings.Contains(indexes, want) {
			t.Fatalf("migrationIndexStatements() missing %q", want)
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
