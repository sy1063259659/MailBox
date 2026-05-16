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
