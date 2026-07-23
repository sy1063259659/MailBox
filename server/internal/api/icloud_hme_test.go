package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gptbox-server/internal/session"
)

func TestICloudHMERoutesRejectMissingSession(t *testing.T) {
	handler := NewRouter(nil, session.NewManager([]byte("test-secret"), false))
	tests := []struct{ method, path, body string }{
		{http.MethodGet, "/api/icloud-hme/source-accounts", ""},
		{http.MethodPost, "/api/icloud-hme/source-accounts", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/1/cookies", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/1/login", `{}`},
		{http.MethodPut, "/api/icloud-hme/source-accounts/1/app-password", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/1/validate", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/validate-all", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/sync-all", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/1/login/start", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/1/login/complete", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/1/aliases", `{}`},
		{http.MethodPost, "/api/icloud-hme/source-accounts/1/aliases/sync", `{}`},
		{http.MethodGet, "/api/icloud-hme/aliases", ""},
		{http.MethodPatch, "/api/icloud-hme/aliases/remark", `{}`},
		{http.MethodPost, "/api/icloud-hme/aliases/move-group", `{}`},
		{http.MethodPost, "/api/icloud-hme/aliases/lifecycle", `{}`},
		{http.MethodPost, "/api/icloud-hme/aliases/receive-keys/generate", `{}`},
		{http.MethodPost, "/api/icloud-hme/aliases/receive-keys/export", `{}`},
		{http.MethodPost, "/api/icloud-hme/aliases/test%40icloud.com/receive-key/reveal", `{}`},
		{http.MethodPost, "/api/icloud-hme/aliases/test%40icloud.com/receive-key/reset", `{}`},
		{http.MethodPost, "/api/icloud-hme/aliases/test%40icloud.com/delete-apple", `{}`},
		{http.MethodGet, "/api/icloud-hme/jobs", ""},
		{http.MethodPost, "/api/icloud-hme/jobs", `{}`},
		{http.MethodGet, "/api/icloud-hme/jobs/1", ""},
		{http.MethodPost, "/api/icloud-hme/jobs/1/cancel", `{}`},
		{http.MethodPost, "/api/icloud-hme/jobs/1/retry", `{}`},
		{http.MethodGet, "/api/icloud-hme/automation", ""},
		{http.MethodPut, "/api/icloud-hme/automation", `{}`},
		{http.MethodGet, "/api/icloud-hme/automation/events", ""},
		{http.MethodPost, "/api/icloud-hme/aliases/inventory-status", `{}`},
		{http.MethodDelete, "/api/icloud-hme/aliases/test%40icloud.com", ""},
		{http.MethodPost, "/api/icloud-hme/mail/latest", `{}`},
		{http.MethodPost, "/api/icloud-hme/mail/messages", `{}`},
		{http.MethodPost, "/api/icloud-hme/mail/message", `{}`},
		{http.MethodPost, "/api/icloud-hme/mail/code", `{}`},
		{http.MethodGet, "/api/icloud-hme/groups", ""},
		{http.MethodPost, "/api/icloud-hme/groups", `{}`},
		{http.MethodPatch, "/api/icloud-hme/groups/order", `{}`},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestParseICloudHMECookies(t *testing.T) {
	header, err := parseICloudHMECookies(`X-APPLE-WEBAUTH-TOKEN="abc"; scnt=def`)
	if err != nil {
		t.Fatal(err)
	}
	if header["X-APPLE-WEBAUTH-TOKEN"] != "abc" || header["scnt"] != "def" {
		t.Fatalf("header cookies = %#v", header)
	}
	object, err := parseICloudHMECookies(`{"session":"one","trust":"two"}`)
	if err != nil {
		t.Fatal(err)
	}
	if object["session"] != "one" || object["trust"] != "two" {
		t.Fatalf("json cookies = %#v", object)
	}
}

func TestClassifyICloudHMEError(t *testing.T) {
	tests := []struct{ message, code string }{
		{"账号启用了双重认证,需要提供 OTP", "icloud_otp_required"},
		{"未开通 iCloud+ 订阅", "icloud_plus_required"},
		{"HTTP 401", "icloud_session_expired"},
		{"创建别名失败: reserve", "icloud_alias_create_failed"},
	}
	for _, test := range tests {
		code, _ := classifyICloudHMEError(assertError(test.message))
		if code != test.code {
			t.Fatalf("classify(%q) = %q, want %q", test.message, code, test.code)
		}
	}
}

type assertError string

func (err assertError) Error() string { return string(err) }
