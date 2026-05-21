package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeHTTPClient struct {
	do func(*http.Request) (*http.Response, error)
}

func (client fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return client.do(req)
}

func TestStartOAuthBuildsExpectedAuthorizationURL(t *testing.T) {
	state, err := StartOAuth()
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := url.Parse(state.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	want := map[string]string{
		"response_type":              "code",
		"client_id":                  ClientID,
		"redirect_uri":               defaultRedirectURI,
		"scope":                      Scopes,
		"code_challenge_method":      "S256",
		"id_token_add_organizations": "true",
		"codex_cli_simplified_flow":  "true",
		"state":                      state.State,
		"originator":                 Originator,
	}
	for key, expected := range want {
		if values.Get(key) != expected {
			t.Fatalf("%s = %q, want %q", key, values.Get(key), expected)
		}
	}
	if parsed.Scheme+"://"+parsed.Host+parsed.Path != AuthEndpoint {
		t.Fatalf("endpoint = %s, want %s", parsed.Scheme+"://"+parsed.Host+parsed.Path, AuthEndpoint)
	}
	if values.Get("code_challenge") == "" {
		t.Fatal("code_challenge is empty")
	}
	if state.LoginID == "" || state.CodeVerifier == "" || state.State == "" {
		t.Fatalf("state has empty generated fields: %+v", state)
	}
}

func TestParseCallbackURLValidatesState(t *testing.T) {
	code, err := ParseCallbackURL("http://localhost:1455/auth/callback?code=abc&state=expected", "expected")
	if err != nil {
		t.Fatal(err)
	}
	if code != "abc" {
		t.Fatalf("code = %q, want abc", code)
	}

	if _, err := ParseCallbackURL("http://localhost:1455/auth/callback?code=abc&state=wrong", "expected"); err == nil {
		t.Fatal("ParseCallbackURL succeeded with wrong state")
	}
}

func TestParseTokenJSONSupportsRootAndNestedTokens(t *testing.T) {
	root, err := ParseTokenJSON(`{"id_token":"id","access_token":"access","refresh_token":"refresh","account_id":"acct_root"}`)
	if err != nil {
		t.Fatal(err)
	}
	if root.IDToken != "id" || root.AccessToken != "access" || root.RefreshToken != "refresh" || root.AccountID != "acct_root" {
		t.Fatalf("root tokens = %+v", root)
	}

	nested, err := ParseTokenJSON(`{"account_id":"acct_root","tokens":{"id_token":"id2","access_token":"access2","refresh_token":"refresh2","account_id":"acct_nested"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if nested.IDToken != "id2" || nested.AccessToken != "access2" || nested.RefreshToken != "refresh2" || nested.AccountID != "acct_nested" {
		t.Fatalf("nested tokens = %+v", nested)
	}
}

func TestParseTokenProfileReadsJWTProfileAndSubscriptionTime(t *testing.T) {
	subscription := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	idToken := testJWT(map[string]any{
		"email": "user@example.com",
		"sub":   "fallback-user",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_user_id":                   "user_123",
			"chatgpt_plan_type":                 "plus",
			"chatgpt_account_id":                "acct_123",
			"chatgpt_organization_id":           "org_123",
			"chatgpt_subscription_active_until": subscription.UnixMilli(),
		},
	})
	accessToken := testJWT(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_user_id": "access_user",
		},
	})

	profile := ParseTokenProfile(Tokens{IDToken: idToken, AccessToken: accessToken})

	if profile.Email != "user@example.com" || profile.UserID != "user_123" || profile.PlanType != "plus" {
		t.Fatalf("profile basic fields = %+v", profile)
	}
	if profile.AccountID != "acct_123" || profile.OrganizationID != "org_123" {
		t.Fatalf("profile account fields = %+v", profile)
	}
	if profile.SubscriptionActiveUntil == nil || !profile.SubscriptionActiveUntil.Equal(subscription) {
		t.Fatalf("SubscriptionActiveUntil = %v, want %v", profile.SubscriptionActiveUntil, subscription)
	}
}

func TestAccessTokenExpiresAtAndNeedsRefresh(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	farFuture := testJWT(map[string]any{"exp": now.Add(10 * time.Minute).Unix()})
	expiresAt := AccessTokenExpiresAt(farFuture)
	if expiresAt == nil || !expiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, now.Add(10*time.Minute))
	}
	if AccessTokenNeedsRefresh(farFuture, now) {
		t.Fatal("far future token needs refresh")
	}

	nearFuture := testJWT(map[string]any{"exp": now.Add(time.Minute).Unix()})
	if !AccessTokenNeedsRefresh(nearFuture, now) {
		t.Fatal("near future token does not need refresh")
	}
	if !AccessTokenNeedsRefresh("not-a-jwt", now) {
		t.Fatal("invalid token does not need refresh")
	}
}

func TestExchangeCodeSendsFormRequest(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != TokenEndpoint {
			t.Fatalf("request = %s %s", req.Method, req.URL.String())
		}
		if req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q", req.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"grant_type":    "authorization_code",
			"code":          "code-123",
			"redirect_uri":  "http://localhost/callback",
			"client_id":     ClientID,
			"code_verifier": "verifier-123",
		}
		for key, expected := range want {
			if values.Get(key) != expected {
				t.Fatalf("%s = %q, want %q", key, values.Get(key), expected)
			}
		}
		return jsonResponse(http.StatusOK, `{"id_token":"id","access_token":"access","refresh_token":"refresh"}`), nil
	}}}

	tokens, err := client.ExchangeCode(context.Background(), " code-123 ", " http://localhost/callback ", " verifier-123 ")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.IDToken != "id" || tokens.AccessToken != "access" || tokens.RefreshToken != "refresh" {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestExchangeCodeErrorUsesFallbackCode(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{"error":{"code":"bad_code"}}`), nil
	}}}

	_, err := client.ExchangeCode(context.Background(), "code", "redirect", "verifier")
	if err == nil || !strings.Contains(err.Error(), "bad_code") {
		t.Fatalf("err = %v, want bad_code", err)
	}
}

func TestRefreshSendsJSONRequestAndKeepsFallbackTokens(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != TokenEndpoint {
			t.Fatalf("request = %s %s", req.Method, req.URL.String())
		}
		if req.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q", req.Header.Get("Content-Type"))
		}
		var payload map[string]string
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["grant_type"] != "refresh_token" || payload["client_id"] != ClientID || payload["refresh_token"] != "refresh-old" {
			t.Fatalf("payload = %+v", payload)
		}
		return jsonResponse(http.StatusOK, `{"access_token":"access-new"}`), nil
	}}}

	tokens, err := client.Refresh(context.Background(), " refresh-old ", " id-current ")
	if err != nil {
		t.Fatal(err)
	}
	if tokens.IDToken != "id-current" || tokens.AccessToken != "access-new" || tokens.RefreshToken != " refresh-old " {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestIsReauthErrorIncludesInvalidGrant(t *testing.T) {
	if !IsReauthError("oauth failed: invalid_grant") {
		t.Fatal("invalid_grant was not treated as reauth error")
	}
}

func testJWT(payload map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payloadBytes) + ".signature"
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
