package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gptbox-server/internal/session"
	"gptbox-server/internal/store"
)

func TestAuthRequiredRejectsMissingSession(t *testing.T) {
	handler := authRequired(session.NewManager([]byte("test-secret"), false), func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without a valid session")
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)

	handler(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(recorder.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("body = %s, want unauthorized error", recorder.Body.String())
	}
}

func TestAccountPathHandlerRejectsUnauthenticatedRemarkPatch(t *testing.T) {
	handler := authRequired(session.NewManager([]byte("test-secret"), false), accountPathHandler(accountAPI{}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/accounts/user%40example.com/remark", strings.NewReader(`{"remark":"x"}`))

	handler(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestGroupOrderRejectsMissingSession(t *testing.T) {
	handler := authRequired(session.NewManager([]byte("test-secret"), false), groupIDHandler(accountAPI{}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/groups/order", strings.NewReader(`{"ids":[1,2]}`))

	handler(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRemovedGPTAccountRoutesReturnNotFound(t *testing.T) {
	handler := NewRouter(nil, session.NewManager([]byte("test-secret"), false))
	for _, path := range []string{
		"/api/gpt-accounts",
		"/api/gpt-accounts/import-token",
		"/api/gpt-accounts/oauth/start",
		"/api/gpt-accounts/oauth/complete",
		"/api/gpt-accounts/refresh-all",
		"/api/gpt-accounts/user%40example.com/refresh",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
			}
		})
	}
}

func TestReorderGroupsRejectsEmptyIDs(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/groups/order", strings.NewReader(`{"ids":[]}`))

	accountAPI{}.reorderGroups(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestUpdateAccountRemarkRejectsTooLongRemark(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/accounts/user%40example.com/remark",
		strings.NewReader(`{"remark":"`+strings.Repeat("好", maxAccountRemarkLength+1)+`"}`),
	)

	accountAPI{}.updateAccountRemark(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "备注最多 500 个字符") {
		t.Fatalf("body = %s, want remark length error", recorder.Body.String())
	}
}

func TestRemarkPathKeepsEncodedPlusAlias(t *testing.T) {
	request := httptest.NewRequest(http.MethodPatch, "/api/accounts/user%2Babc%40hotmail.com/remark", nil)
	email := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/api/accounts/"), "/remark")

	if email != "user+abc@hotmail.com" {
		t.Fatalf("email = %q, want plus alias", email)
	}
}

func TestUpdateAccountRemarkRequiresEmailOnDedicatedRoute(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/api/accounts/remark", strings.NewReader(`{"remark":"x"}`))

	accountAPI{}.updateAccountRemark(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "email is required") {
		t.Fatalf("body = %s, want email required error", recorder.Body.String())
	}
}

func TestWriteServiceErrorMapsTimeout(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeServiceError(recorder, contextDeadlineExceeded{})

	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
	if !strings.Contains(recorder.Body.String(), `"error":"imap_timeout"`) {
		t.Fatalf("body = %s, want imap_timeout", recorder.Body.String())
	}
}

func TestWriteServiceErrorMapsAuthFailure(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeServiceError(recorder, authFailureError{})

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(recorder.Body.String(), `"error":"imap_auth_error"`) {
		t.Fatalf("body = %s, want imap_auth_error", recorder.Body.String())
	}
}

type contextDeadlineExceeded struct{}

func (contextDeadlineExceeded) Error() string {
	return "context deadline exceeded"
}

type authFailureError struct{}

func (authFailureError) Error() string {
	return "AUTHENTICATE failed"
}

func TestParseAccountImportTextRejectsOptionalRemark(t *testing.T) {
	inputs, errors := parseAccountImportText("user@example.com----pass----client----refresh----  VIP 客户  ")
	if len(inputs) != 0 {
		t.Fatalf("len(inputs) = %d, want 0", len(inputs))
	}
	if len(errors) != 1 {
		t.Fatalf("len(errors) = %d, want 1", len(errors))
	}
	if !strings.Contains(errors[0], "格式必须为") {
		t.Fatalf("error = %q, want format error", errors[0])
	}
}

func TestParseAccountImportTextKeepsOldFormatWithoutRemark(t *testing.T) {
	inputs, errors := parseAccountImportText("user@example.com----pass----client----refresh")
	if len(errors) > 0 {
		t.Fatalf("errors = %v, want none", errors)
	}
	if len(inputs) != 1 {
		t.Fatalf("len(inputs) = %d, want 1", len(inputs))
	}
	if inputs[0].RemarkSet {
		t.Fatal("RemarkSet = true, want false")
	}
	if inputs[0].Remark != "" {
		t.Fatalf("Remark = %q, want empty", inputs[0].Remark)
	}
}

func TestParseAccountImportTextRejectsSpaceSeparatedFormat(t *testing.T) {
	inputs, errors := parseAccountImportText("user@example.com pass client refresh")
	if len(inputs) != 0 {
		t.Fatalf("len(inputs) = %d, want 0", len(inputs))
	}
	if len(errors) != 1 {
		t.Fatalf("len(errors) = %d, want 1", len(errors))
	}
	if !strings.Contains(errors[0], "格式必须为") {
		t.Fatalf("error = %q, want format error", errors[0])
	}
}

func TestParseAccountImportTextSplitsDashSeparatedRecordsOnOneLine(t *testing.T) {
	inputs, errors := parseAccountImportText(
		"one@outlook.com----pass1----client1----refresh1 two@outlook.com----pass2----client2----refresh2",
	)
	if len(errors) > 0 {
		t.Fatalf("errors = %v, want none", errors)
	}
	if len(inputs) != 2 {
		t.Fatalf("len(inputs) = %d, want 2", len(inputs))
	}
	if inputs[0].Email != "one@outlook.com" || inputs[1].Email != "two@outlook.com" {
		t.Fatalf("emails = %q, %q; want split records", inputs[0].Email, inputs[1].Email)
	}
}

func TestApplyImportGroupUsesRequestGroup(t *testing.T) {
	inputs, errors := parseAccountImportText("user@example.com----pass----client----refresh")
	if len(errors) > 0 {
		t.Fatalf("errors = %v, want none", errors)
	}

	applyImportGroup(inputs, "  客户A  ")

	if inputs[0].Group != "客户A" {
		t.Fatalf("Group = %q, want %q", inputs[0].Group, "客户A")
	}
}

func TestApplyImportGroupDefaultsEmptyGroup(t *testing.T) {
	inputs, errors := parseAccountImportText("user@example.com----pass----client----refresh")
	if len(errors) > 0 {
		t.Fatalf("errors = %v, want none", errors)
	}

	applyImportGroup(inputs, "  ")

	if inputs[0].Group != store.DefaultGroupName {
		t.Fatalf("Group = %q, want %q", inputs[0].Group, store.DefaultGroupName)
	}
}

func TestMailAccountJSONOmitsEmptyRefreshToken(t *testing.T) {
	payload, err := json.Marshal(store.MailAccount{
		Email:    "user@example.com",
		Password: "password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "refreshToken") {
		t.Fatalf("payload = %s, want refreshToken omitted", string(payload))
	}
}
func TestICloudRoutesRejectMissingSession(t *testing.T) {
	handler := NewRouter(nil, session.NewManager([]byte("test-secret"), false))
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/icloud-accounts", ""},
		{http.MethodPost, "/api/icloud-accounts/import", `{}`},
		{http.MethodPost, "/api/icloud-accounts/latest", `{}`},
		{http.MethodPatch, "/api/icloud-accounts/remark", `{}`},
		{http.MethodPost, "/api/icloud-accounts/move-group", `{}`},
		{http.MethodDelete, "/api/icloud-accounts/user%40icloud.com", ""},
		{http.MethodGet, "/api/icloud-groups", ""},
		{http.MethodPost, "/api/icloud-groups", `{}`},
		{http.MethodPatch, "/api/icloud-groups/order", `{}`},
		{http.MethodPatch, "/api/icloud-groups/1", `{}`},
		{http.MethodDelete, "/api/icloud-groups/1", ""},
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

func TestParseICloudImportText(t *testing.T) {
	inputs, parseErrors := parseICloudImportText(`
 Example-Alias@iCloud.com ---- first-key
second@icloud.com----second-key
`)
	if len(parseErrors) != 0 {
		t.Fatalf("errors = %v, want none", parseErrors)
	}
	if len(inputs) != 2 {
		t.Fatalf("len(inputs) = %d, want 2", len(inputs))
	}
	if inputs[0].Email != "example-alias@icloud.com" || inputs[0].Key != "first-key" {
		t.Fatalf("first input = %+v", inputs[0])
	}
	if inputs[1].Email != "second@icloud.com" || inputs[1].Key != "second-key" {
		t.Fatalf("second input = %+v", inputs[1])
	}
}

func TestParseICloudImportRejectsInvalidRecords(t *testing.T) {
	inputs, parseErrors := parseICloudImportText(`
user@example.com----key
missing-separator
empty-key@icloud.com----
----missing-email
too@icloud.com----many----parts
`)
	if len(inputs) != 0 {
		t.Fatalf("len(inputs) = %d, want 0", len(inputs))
	}
	if len(parseErrors) != 5 {
		t.Fatalf("errors = %v, want 5 errors", parseErrors)
	}
	joined := strings.Join(parseErrors, "\n")
	for _, want := range []string{"只支持 @icloud.com", "格式必须为", "密钥不能为空", "邮箱不能为空"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %v, want %q", parseErrors, want)
		}
	}
}

func TestICloudOverwriteWithNoValidAccountsDoesNotUseStore(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/icloud-accounts/import",
		strings.NewReader(`{"overwrite":true,"text":"invalid@example.com----key"}`),
	)

	iCloudAPI{}.importAccounts(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), "只支持 @icloud.com") {
		t.Fatalf("body = %s, want validation error", recorder.Body.String())
	}
}

func TestUpdateICloudRemarkRejectsTooLongRemark(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/icloud-accounts/remark",
		strings.NewReader(`{"email":"user@icloud.com","remark":"`+strings.Repeat("好", maxAccountRemarkLength+1)+`"}`),
	)

	iCloudAPI{}.updateRemark(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestICloudLatestClientParsesResponseAndForwardsCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("address"); got != "alias@icloud.com" {
			t.Fatalf("address = %q", got)
		}
		if got := r.URL.Query().Get("key"); got != "test-key" {
			t.Fatalf("key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"mailbox": {"id": 1, "address": "alias@icloud.com", "active": true},
			"email": {
				"id": 2,
				"from": "sender@example.com",
				"to": "alias@icloud.com",
				"subject": "Verification",
				"text": "Your code is 123456",
				"html": "<b>ignored</b>",
				"body": "raw ignored",
				"received_at": "2026-07-20T10:00:00+08:00",
				"verification_code": "123456",
				"mail_type": "verification",
				"invite_link": "",
				"process_status": "pending"
			}
		}`))
	}))
	defer server.Close()

	client := &iCloudLatestClient{baseURL: server.URL, httpClient: server.Client()}
	payload, err := client.Latest(t.Context(), "alias@icloud.com", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if payload.Email == nil || payload.Email.VerificationCode != "123456" || payload.Email.Text != "Your code is 123456" {
		t.Fatalf("payload = %+v", payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "<b>ignored</b>") || strings.Contains(string(encoded), "raw ignored") {
		t.Fatalf("payload exposes raw mail content: %s", encoded)
	}
}

func TestICloudLatestClientDoesNotLeakKeyInHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failure", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &iCloudLatestClient{baseURL: server.URL, httpClient: server.Client()}
	_, err := client.Latest(t.Context(), "alias@icloud.com", "secret-key-value")
	if err == nil {
		t.Fatal("want upstream error")
	}
	if strings.Contains(err.Error(), "secret-key-value") {
		t.Fatalf("error leaks key: %v", err)
	}
}
func TestICloudLatestClientRedactsKeyFromUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"message":"invalid key: secret-key-value"}`))
	}))
	defer server.Close()

	client := &iCloudLatestClient{baseURL: server.URL, httpClient: server.Client()}
	_, err := client.Latest(t.Context(), "alias@icloud.com", "secret-key-value")
	if err == nil {
		t.Fatal("want upstream error")
	}
	if strings.Contains(err.Error(), "secret-key-value") {
		t.Fatalf("error leaks key: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error = %v, want redaction marker", err)
	}
}
