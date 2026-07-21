package imapmail

import (
	"context"
	"strings"
	"testing"
)

func TestGetLatestMessageByRecipientValidatesCredentials(t *testing.T) {
	_, err := (Client{}).GetLatestMessageByRecipient(context.Background(), "", "", "alias@icloud.com")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err = %v", err)
	}
}
