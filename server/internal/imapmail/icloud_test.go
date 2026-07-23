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

func TestListMessageDetailsByRecipientValidatesCredentials(t *testing.T) {
	_, err := (Client{}).ListMessageDetailsByRecipient(context.Background(), "", "", "alias@icloud.com", 20, "")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("error = %v, want required credentials", err)
	}
}

func TestICloudHMEExtendedMethodsValidateInput(t *testing.T) {
	client := Client{}
	if _, err := client.ListMessagesByRecipient(context.Background(), "", "", "alias@icloud.com", 20, ""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("list err = %v", err)
	}
	if _, err := client.GetMessageByRecipient(context.Background(), "user@icloud.com", "password", "alias@icloud.com", "bad"); err == nil || !strings.Contains(err.Error(), "invalid uid") {
		t.Fatalf("message err = %v", err)
	}
}

func TestICloudLoginUsernamesPrefersLocalPart(t *testing.T) {
	usernames := iCloudLoginUsernames(" shenyu4748@icloud.com ")
	if len(usernames) != 2 || usernames[0] != "shenyu4748" || usernames[1] != "shenyu4748@icloud.com" {
		t.Fatalf("usernames = %#v", usernames)
	}
}

func TestICloudLoginUsernamesKeepsUsernameWithoutDomain(t *testing.T) {
	usernames := iCloudLoginUsernames("shenyu4748")
	if len(usernames) != 1 || usernames[0] != "shenyu4748" {
		t.Fatalf("usernames = %#v", usernames)
	}
}
