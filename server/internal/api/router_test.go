package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gptbox-server/internal/codexauth"
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

func TestGPTAccountsRejectMissingSession(t *testing.T) {
	handler := authRequired(session.NewManager([]byte("test-secret"), false), methodHandler(http.MethodGet, newGPTAccountAPI(nil).list))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/gpt-accounts", nil)

	handler(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestGPTAccountOAuthStartRejectsMissingMailAccountEmail(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/gpt-accounts/oauth/start", strings.NewReader(`{}`))

	newGPTAccountAPI(nil).oauthStart(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "mailAccountEmail is required") {
		t.Fatalf("body = %s, want mailAccountEmail error", recorder.Body.String())
	}
}

func TestGPTAccountOAuthCompleteRejectsMissingState(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/gpt-accounts/oauth/complete", strings.NewReader(`{"mailAccountEmail":"user@example.com","loginId":"missing","callbackUrl":"http://localhost/callback?code=abc&state=state"}`))

	newGPTAccountAPI(nil).oauthComplete(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "OAuth 登录会话不存在或已过期") {
		t.Fatalf("body = %s, want missing state error", recorder.Body.String())
	}
}

func TestGPTAccountOAuthCompleteRejectsExpiredState(t *testing.T) {
	api := newGPTAccountAPI(nil)
	api.oauthState["login-1"] = codexauth.OAuthState{
		LoginID:      "login-1",
		State:        "state-1",
		RedirectURI:  "http://localhost/callback",
		CodeVerifier: "verifier",
		ExpiresAt:    time.Now().Add(-time.Minute).Unix(),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/gpt-accounts/oauth/complete", strings.NewReader(`{"mailAccountEmail":"user@example.com","loginId":"login-1","callbackUrl":"http://localhost/callback?code=abc&state=state-1"}`))

	api.oauthComplete(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "OAuth 登录已过期") {
		t.Fatalf("body = %s, want expired state error", recorder.Body.String())
	}
}

func TestGPTAccountOAuthCompleteRejectsWrongCallbackState(t *testing.T) {
	api := newGPTAccountAPI(nil)
	api.oauthState["login-1"] = codexauth.OAuthState{
		LoginID:      "login-1",
		State:        "state-1",
		RedirectURI:  "http://localhost/callback",
		CodeVerifier: "verifier",
		ExpiresAt:    time.Now().Add(time.Minute).Unix(),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/gpt-accounts/oauth/complete", strings.NewReader(`{"mailAccountEmail":"user@example.com","loginId":"login-1","callbackUrl":"http://localhost/callback?code=abc&state=wrong"}`))

	api.oauthComplete(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "OAuth state 校验失败") {
		t.Fatalf("body = %s, want wrong state error", recorder.Body.String())
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

func TestGPTAccountJSONDoesNotExposeTokensOrRawQuota(t *testing.T) {
	payload, err := json.Marshal(store.GPTAccount{
		MailAccountEmail: "user@example.com",
		GPTEmail:         "user@example.com",
		QuotaRawJSON:     json.RawMessage(`{"detail":"server-only"}`),
		Status:           "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"idToken", "accessToken", "refreshToken", "quotaRawJson", "server-only"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("payload = %s, want %q omitted", text, forbidden)
		}
	}
}
