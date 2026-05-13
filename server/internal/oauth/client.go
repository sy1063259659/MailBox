package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	tokenEndpoint = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	refreshScope  = "https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	HTTPClient HTTPClient
}

type TokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (c Client) Refresh(ctx context.Context, clientID, refreshToken string) (TokenResult, error) {
	if strings.TrimSpace(clientID) == "" {
		return TokenResult{}, errors.New("oauth: clientID is required")
	}
	if strings.TrimSpace(refreshToken) == "" {
		return TokenResult{}, errors.New("oauth: refreshToken is required")
	}

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("scope", refreshScope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return TokenResult{}, err
	}
	defer resp.Body.Close()

	var body tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return TokenResult{}, fmt.Errorf("oauth: decode token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := body.ErrorDesc
		if msg == "" {
			msg = body.Error
		}
		if msg == "" {
			msg = resp.Status
		}
		return TokenResult{}, fmt.Errorf("oauth: refresh failed: %s", msg)
	}
	if body.AccessToken == "" {
		return TokenResult{}, errors.New("oauth: token response missing access_token")
	}

	return TokenResult{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresIn:    body.ExpiresIn,
	}, nil
}
