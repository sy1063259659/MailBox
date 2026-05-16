package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mailbox-server/internal/session"
	"mailbox-server/internal/store"
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

func TestParseAccountImportTextSupportsOptionalRemark(t *testing.T) {
	inputs, errors := parseAccountImportText("user@example.com----pass----client----refresh----  VIP 客户  ")
	if len(errors) > 0 {
		t.Fatalf("errors = %v, want none", errors)
	}
	if len(inputs) != 1 {
		t.Fatalf("len(inputs) = %d, want 1", len(inputs))
	}
	if !inputs[0].RemarkSet {
		t.Fatal("RemarkSet = false, want true")
	}
	if inputs[0].Remark != "VIP 客户" {
		t.Fatalf("Remark = %q, want %q", inputs[0].Remark, "VIP 客户")
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
