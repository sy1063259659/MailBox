package codexquota

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	UsageURL        = "https://chatgpt.com/backend-api/wham/usage"
	AccountCheckURL = "https://chatgpt.com/backend-api/wham/accounts/check"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Client struct {
	HTTPClient HTTPClient
}

type Window struct {
	Percentage    *int
	ResetTime     *time.Time
	WindowMinutes *int
	Present       bool
}

type Usage struct {
	PlanType string
	Hourly   Window
	Weekly   Window
	RawJSON  []byte
}

type Profile struct {
	AccountID        string
	AccountName      string
	AccountStructure string
	OrganizationID   string
}

type usageResponse struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		PrimaryWindow   *windowInfo `json:"primary_window"`
		SecondaryWindow *windowInfo `json:"secondary_window"`
	} `json:"rate_limit"`
}

type windowInfo struct {
	UsedPercent        *int   `json:"used_percent"`
	LimitWindowSeconds *int64 `json:"limit_window_seconds"`
	ResetAfterSeconds  *int64 `json:"reset_after_seconds"`
	ResetAt            *int64 `json:"reset_at"`
}

func (c Client) FetchUsage(ctx context.Context, accessToken string, accountID string) (Usage, error) {
	body, err := c.getJSON(ctx, UsageURL, accessToken, accountID)
	if err != nil {
		return Usage{}, err
	}
	var response usageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Usage{}, fmt.Errorf("codex quota: decode usage: %w", err)
	}
	var primary, secondary *windowInfo
	if response.RateLimit != nil {
		primary = response.RateLimit.PrimaryWindow
		secondary = response.RateLimit.SecondaryWindow
	}
	return Usage{
		PlanType: response.PlanType,
		Hourly:   normalizeWindow(primary, "hourly"),
		Weekly:   normalizeWindow(secondary, "weekly"),
		RawJSON:  body,
	}, nil
}

func (c Client) FetchProfile(ctx context.Context, accessToken string, accountID string, organizationID string) (Profile, error) {
	body, err := c.getJSON(ctx, AccountCheckURL, accessToken, accountID)
	if err != nil {
		return Profile{}, err
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Profile{}, fmt.Errorf("codex quota: decode account profile: %w", err)
	}
	return parseProfile(payload, accountID, organizationID), nil
}

func (c Client) getJSON(ctx context.Context, endpoint string, accessToken string, accountID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(accountID) != "" {
		req.Header.Set("ChatGPT-Account-Id", strings.TrimSpace(accountID))
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := extractErrorCode(body)
		message := fmt.Sprintf("API 返回错误 %d", resp.StatusCode)
		if code != "" {
			message += " [error_code:" + code + "]"
		}
		message += fmt.Sprintf(" [body_len:%d]", len(body))
		return nil, ServiceError{StatusCode: resp.StatusCode, Code: code, Message: message}
	}
	return body, nil
}

type ServiceError struct {
	StatusCode int
	Code       string
	Message    string
}

func (err ServiceError) Error() string {
	return err.Message
}

func normalizeWindow(window *windowInfo, fallback string) Window {
	if window == nil {
		percentage := 100
		present := false
		return Window{Percentage: &percentage, Present: present}
	}
	used := 0
	if window.UsedPercent != nil {
		used = clamp(*window.UsedPercent, 0, 100)
	}
	percentage := 100 - used
	var resetTime *time.Time
	if window.ResetAt != nil {
		reset := time.Unix(*window.ResetAt, 0).UTC()
		resetTime = &reset
	} else if window.ResetAfterSeconds != nil && *window.ResetAfterSeconds >= 0 {
		reset := time.Now().UTC().Add(time.Duration(*window.ResetAfterSeconds) * time.Second)
		resetTime = &reset
	}
	var minutes *int
	if window.LimitWindowSeconds != nil && *window.LimitWindowSeconds > 0 {
		value := int((*window.LimitWindowSeconds + 59) / 60)
		minutes = &value
	}
	_ = fallback
	return Window{Percentage: &percentage, ResetTime: resetTime, WindowMinutes: minutes, Present: true}
}

func parseProfile(payload any, expectedAccountID string, expectedOrganizationID string) Profile {
	root := asMap(payload)
	records := collectAccountRecords(payload)
	if len(records) == 0 {
		return Profile{}
	}
	selected := map[string]any(nil)
	if expectedAccountID != "" {
		for _, record := range records {
			if firstString(record["id"], record["account_id"], record["chatgpt_account_id"], record["workspace_id"]) == expectedAccountID {
				selected = record
				break
			}
		}
	}
	if selected == nil {
		ordering := asSlice(root["account_ordering"])
		if len(ordering) > 0 {
			firstID := stringValue(ordering[0])
			for _, record := range records {
				if firstString(record["id"], record["account_id"], record["chatgpt_account_id"], record["workspace_id"]) == firstID {
					selected = record
					break
				}
			}
		}
	}
	if selected == nil && expectedOrganizationID != "" {
		for _, record := range records {
			if firstString(record["organization_id"], record["org_id"], record["workspace_id"]) == expectedOrganizationID {
				selected = record
				break
			}
		}
	}
	if selected == nil {
		selected = records[0]
	}
	return Profile{
		AccountID:        firstString(selected["id"], selected["account_id"], selected["chatgpt_account_id"], selected["workspace_id"]),
		AccountName:      firstString(selected["name"], selected["display_name"], selected["account_name"], selected["organization_name"], selected["workspace_name"], selected["title"]),
		AccountStructure: firstString(selected["structure"], selected["account_structure"], selected["kind"], selected["type"], selected["account_type"]),
		OrganizationID:   firstString(selected["organization_id"], selected["org_id"], selected["workspace_id"]),
	}
}

func collectAccountRecords(payload any) []map[string]any {
	root := asMap(payload)
	records := []map[string]any{}
	if accounts, ok := root["accounts"]; ok {
		if array := asSlice(accounts); len(array) > 0 {
			for _, item := range array {
				if record := asMap(item); len(record) > 0 {
					records = append(records, record)
				}
			}
		} else if object := asMap(accounts); len(object) > 0 {
			for _, item := range object {
				if record := asMap(item); len(record) > 0 {
					records = append(records, record)
				}
			}
		}
	}
	if len(records) == 0 {
		for _, item := range asSlice(payload) {
			if record := asMap(item); len(record) > 0 {
				records = append(records, record)
			}
		}
	}
	return records
}

func extractErrorCode(body []byte) string {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	record := asMap(payload)
	if detail := asMap(record["detail"]); len(detail) > 0 {
		if code := stringValue(detail["code"]); code != "" {
			return code
		}
	}
	if errorRecord := asMap(record["error"]); len(errorRecord) > 0 {
		if code := stringValue(errorRecord["code"]); code != "" {
			return code
		}
	}
	return stringValue(record["code"])
}

func IsBannedOrDisabled(message string, statusCode int) bool {
	normalized := strings.ToLower(message)
	return statusCode == http.StatusForbidden ||
		strings.Contains(normalized, "suspended") ||
		strings.Contains(normalized, "disabled") ||
		strings.Contains(normalized, "deactivated") ||
		strings.Contains(normalized, "banned") ||
		strings.Contains(normalized, "account not active")
}

func IsQuotaLimited(message string) bool {
	normalized := strings.ToLower(message)
	return strings.Contains(normalized, "limit_reached") ||
		strings.Contains(normalized, "rate limit") ||
		strings.Contains(normalized, "quota")
}

func asMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func asSlice(value any) []any {
	if result, ok := value.([]any); ok {
		return result
	}
	return nil
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func firstString(values ...any) string {
	for _, value := range values {
		if text := stringValue(value); text != "" {
			return text
		}
	}
	return ""
}

func clamp(value int, min int, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
