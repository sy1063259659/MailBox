package codexauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ClientID           = "app_EMoamEEZ73f0CkXaXp7hrann"
	AuthEndpoint       = "https://auth.openai.com/oauth/authorize"
	TokenEndpoint      = "https://auth.openai.com/oauth/token"
	Scopes             = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	Originator         = "codex_vscode"
	TokenRefreshSkew   = 300 * time.Second
	defaultRedirectURI = "http://localhost:1455/auth/callback"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	HTTPClient HTTPClient
}

type Tokens struct {
	IDToken      string
	AccessToken  string
	RefreshToken string
	AccountID    string
}

type OAuthState struct {
	LoginID      string `json:"loginId"`
	AuthURL      string `json:"authUrl"`
	RedirectURI  string `json:"redirectUri"`
	CodeVerifier string `json:"codeVerifier"`
	State        string `json:"state"`
	ExpiresAt    int64  `json:"expiresAt"`
}

type TokenProfile struct {
	Email                   string
	UserID                  string
	PlanType                string
	AuthFilePlanType        string
	SubscriptionActiveUntil *time.Time
	AccountID               string
	OrganizationID          string
}

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        any    `json:"error"`
	ErrorDesc    string `json:"error_description"`
	Code         string `json:"code"`
}

func StartOAuth() (OAuthState, error) {
	codeVerifier, err := randomBase64URL()
	if err != nil {
		return OAuthState{}, err
	}
	stateToken, err := randomBase64URL()
	if err != nil {
		return OAuthState{}, err
	}
	loginID, err := randomBase64URL()
	if err != nil {
		return OAuthState{}, err
	}
	challenge := codeChallenge(codeVerifier)
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", ClientID)
	values.Set("redirect_uri", defaultRedirectURI)
	values.Set("scope", Scopes)
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("id_token_add_organizations", "true")
	values.Set("codex_cli_simplified_flow", "true")
	values.Set("state", stateToken)
	values.Set("originator", Originator)
	return OAuthState{
		LoginID:      loginID,
		AuthURL:      AuthEndpoint + "?" + values.Encode(),
		RedirectURI:  defaultRedirectURI,
		CodeVerifier: codeVerifier,
		State:        stateToken,
		ExpiresAt:    time.Now().Add(5 * time.Minute).Unix(),
	}, nil
}

func (c Client) ExchangeCode(ctx context.Context, code string, redirectURI string, codeVerifier string) (Tokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", strings.TrimSpace(redirectURI))
	form.Set("client_id", ClientID)
	form.Set("code_verifier", strings.TrimSpace(codeVerifier))
	body, err := c.postToken(ctx, form)
	if err != nil {
		return Tokens{}, err
	}
	if strings.TrimSpace(body.IDToken) == "" || strings.TrimSpace(body.AccessToken) == "" {
		return Tokens{}, errors.New("Codex OAuth 响应缺少 id_token 或 access_token")
	}
	return Tokens{
		IDToken:      body.IDToken,
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
	}, nil
}

func (c Client) Refresh(ctx context.Context, refreshToken string, currentIDToken string) (Tokens, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Tokens{}, errors.New("missing refresh token")
	}
	body, err := c.postTokenJSON(ctx, map[string]string{
		"client_id":     ClientID,
		"grant_type":    "refresh_token",
		"refresh_token": strings.TrimSpace(refreshToken),
	})
	if err != nil {
		return Tokens{}, err
	}
	if strings.TrimSpace(body.AccessToken) == "" {
		return Tokens{}, errors.New("Codex OAuth 刷新响应缺少 access_token")
	}
	idToken := strings.TrimSpace(body.IDToken)
	if idToken == "" {
		idToken = strings.TrimSpace(currentIDToken)
	}
	if idToken == "" {
		return Tokens{}, errors.New("Codex OAuth 刷新响应缺少 id_token")
	}
	nextRefresh := strings.TrimSpace(body.RefreshToken)
	if nextRefresh == "" {
		nextRefresh = refreshToken
	}
	return Tokens{
		IDToken:      idToken,
		AccessToken:  body.AccessToken,
		RefreshToken: nextRefresh,
	}, nil
}

func (c Client) postTokenJSON(ctx context.Context, payload map[string]string) (tokenResponse, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return tokenResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenEndpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.doTokenRequest(req)
}

func (c Client) postToken(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.doTokenRequest(req)
}

func (c Client) doTokenRequest(req *http.Request) (tokenResponse, error) {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return tokenResponse{}, err
	}
	var body tokenResponse
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &body)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := body.ErrorDesc
		if message == "" {
			message = extractErrorString(body.Error)
		}
		if message == "" {
			message = body.Code
		}
		if message == "" {
			message = resp.Status
		}
		return tokenResponse{}, fmt.Errorf("codex auth: token request failed: %s", message)
	}
	return body, nil
}

func ParseCallbackURL(callbackURL string, expectedState string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil {
		return "", fmt.Errorf("回调 URL 格式无效: %w", err)
	}
	values := parsed.Query()
	if values.Get("state") != expectedState {
		return "", errors.New("OAuth state 校验失败")
	}
	code := strings.TrimSpace(values.Get("code"))
	if code == "" {
		return "", errors.New("回调 URL 缺少 code")
	}
	return code, nil
}

func ParseTokenJSON(text string) (Tokens, error) {
	var payload any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &payload); err != nil {
		return Tokens{}, fmt.Errorf("Token JSON 格式无效: %w", err)
	}
	record := asMap(payload)
	tokensRecord := asMap(record["tokens"])
	source := record
	if len(tokensRecord) > 0 {
		source = tokensRecord
	}
	tokens := Tokens{
		IDToken:      stringValue(source["id_token"]),
		AccessToken:  stringValue(source["access_token"]),
		RefreshToken: stringValue(source["refresh_token"]),
		AccountID:    stringValue(source["account_id"]),
	}
	if tokens.AccountID == "" {
		tokens.AccountID = stringValue(record["account_id"])
	}
	if tokens.IDToken == "" || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return Tokens{}, errors.New("Token JSON 必须包含 id_token、access_token、refresh_token")
	}
	return tokens, nil
}

func ParseTokenProfile(tokens Tokens) TokenProfile {
	idPayload := DecodeJWTPayload(tokens.IDToken)
	accessPayload := DecodeJWTPayload(tokens.AccessToken)
	idAuth := asMap(idPayload["https://api.openai.com/auth"])
	accessAuth := asMap(accessPayload["https://api.openai.com/auth"])
	profile := TokenProfile{
		Email:            firstString(idPayload["email"], asMap(idPayload["https://api.openai.com/profile"])["email"]),
		UserID:           firstString(idAuth["chatgpt_user_id"], accessAuth["chatgpt_user_id"], idAuth["user_id"], accessAuth["user_id"], idPayload["sub"]),
		PlanType:         firstString(idAuth["chatgpt_plan_type"], accessAuth["chatgpt_plan_type"]),
		AuthFilePlanType: firstString(idAuth["chatgpt_plan_type"], accessAuth["chatgpt_plan_type"]),
		AccountID:        firstString(tokens.AccountID, idAuth["chatgpt_account_id"], accessAuth["chatgpt_account_id"], idAuth["account_id"], accessAuth["account_id"]),
		OrganizationID: firstString(
			idAuth["organization_id"], accessAuth["organization_id"],
			idAuth["chatgpt_organization_id"], accessAuth["chatgpt_organization_id"],
			idAuth["org_id"], accessAuth["org_id"], idAuth["poid"], accessAuth["poid"],
		),
	}
	profile.SubscriptionActiveUntil = parseSubscriptionTime(firstString(
		idAuth["chatgpt_subscription_active_until"],
		accessAuth["chatgpt_subscription_active_until"],
		idAuth["subscription_active_until"],
		accessAuth["subscription_active_until"],
	))
	return profile
}

func AccessTokenExpiresAt(accessToken string) *time.Time {
	payload := DecodeJWTPayload(accessToken)
	expValue, ok := numberValue(payload["exp"])
	if !ok || expValue <= 0 {
		return nil
	}
	expiresAt := time.Unix(int64(expValue), 0).UTC()
	return &expiresAt
}

func AccessTokenNeedsRefresh(accessToken string, now time.Time) bool {
	expiresAt := AccessTokenExpiresAt(accessToken)
	if expiresAt == nil {
		return true
	}
	return expiresAt.Before(now.Add(TokenRefreshSkew))
}

func DecodeJWTPayload(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return map[string]any{}
	}
	return result
}

func IsReauthError(message string) bool {
	normalized := strings.ToLower(message)
	return strings.Contains(normalized, "refresh_token_reused") ||
		strings.Contains(normalized, "refresh_token_expired") ||
		strings.Contains(normalized, "refresh_token_invalidated") ||
		strings.Contains(normalized, "token_invalidated") ||
		strings.Contains(normalized, "invalid_grant") ||
		strings.Contains(normalized, "invalid_token") ||
		strings.Contains(normalized, "invalid refresh token") ||
		strings.Contains(normalized, "missing refresh token")
}

func randomBase64URL() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func asMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case json.Number:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func firstString(values ...any) string {
	for _, value := range values {
		if text := stringValue(value); text != "" {
			return text
		}
	}
	return ""
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func parseSubscriptionTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		if timestamp > 1_000_000_000_000 {
			timestamp = timestamp / 1000
		}
		parsed := time.Unix(timestamp, 0).UTC()
		return &parsed
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		parsed = parsed.UTC()
		return &parsed
	}
	if parsed, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		parsed = parsed.UTC()
		return &parsed
	}
	return nil
}

func extractErrorString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		return firstString(typed["message"], typed["code"])
	default:
		return ""
	}
}
