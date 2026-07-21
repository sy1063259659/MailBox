package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestICloudHMECredentialsDoNotSerializeSecrets(t *testing.T) {
	payload, err := json.Marshal(ICloudHMESourceCredentials{CookiesJSON: `{"session":"secret"}`, AppPassword: "app-secret"})
	if err != nil {
		t.Fatal(err)
	}
	value := string(payload)
	if strings.Contains(value, "secret") || strings.Contains(value, "CookiesJSON") || strings.Contains(value, "AppPassword") {
		t.Fatalf("credentials leaked into JSON: %s", value)
	}
	mailPayload, err := json.Marshal(ICloudHMEMailCredentials{AliasEmail: "alias@icloud.com", AppPassword: "app-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mailPayload), "app-secret") {
		t.Fatalf("mail credentials leaked into JSON: %s", mailPayload)
	}
}
