package store

import (
	"strings"
	"testing"
)

func TestSanitizeICloudHMEStoredMessageRedactsSensitiveUpstreamData(t *testing.T) {
	values := []string{
		`HTTP 421: {"trustTokens":["secret"]}`,
		`X-APPLE-WEBAUTH-TOKEN=secret`,
		`Set-Cookie: session_id=secret`,
		`Authorization: Bearer secret`,
	}
	for _, value := range values {
		result := sanitizeICloudHMEStoredMessage(value)
		if strings.Contains(result, "secret") || strings.Contains(result, "trustTokens") {
			t.Fatalf("sensitive value was not redacted: %q", result)
		}
	}
}

func TestSanitizeICloudHMEStoredMessageKeepsSafeReason(t *testing.T) {
	const value = "隐藏邮箱创建失败"
	if got := sanitizeICloudHMEStoredMessage(value); got != value {
		t.Fatalf("sanitize = %q, want %q", got, value)
	}
}
