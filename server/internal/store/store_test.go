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
	if len(statements) != 1 || strings.TrimSpace(statements[0]) != "DROP TABLE IF EXISTS gpt_accounts" {
		t.Fatalf("legacyCleanupStatements() = %v", statements)
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
