package codexquota

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
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

func TestFetchUsageSendsExpectedHeaders(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != UsageURL {
			t.Fatalf("request = %s %s", req.Method, req.URL.String())
		}
		if req.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("Authorization = %q", req.Header.Get("Authorization"))
		}
		if req.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q", req.Header.Get("Accept"))
		}
		if req.Header.Get("ChatGPT-Account-Id") != "acct_123" {
			t.Fatalf("ChatGPT-Account-Id = %q", req.Header.Get("ChatGPT-Account-Id"))
		}
		return jsonResponse(http.StatusOK, `{"plan_type":"plus","rate_limit":{}}`), nil
	}}}

	usage, err := client.FetchUsage(context.Background(), " access-token ", " acct_123 ")
	if err != nil {
		t.Fatal(err)
	}
	if usage.PlanType != "plus" {
		t.Fatalf("PlanType = %q, want plus", usage.PlanType)
	}
}

func TestFetchUsageConvertsUsedPercentToRemainingAndClamps(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"rate_limit": {
				"primary_window": {"used_percent":125},
				"secondary_window": {"used_percent":-20}
			}
		}`), nil
	}}}

	usage, err := client.FetchUsage(context.Background(), "access", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := *usage.Hourly.Percentage; got != 0 {
		t.Fatalf("Hourly.Percentage = %d, want 0", got)
	}
	if got := *usage.Weekly.Percentage; got != 100 {
		t.Fatalf("Weekly.Percentage = %d, want 100", got)
	}
}

func TestFetchUsageParsesResetAtResetAfterAndWindowMinutes(t *testing.T) {
	resetAt := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"rate_limit": {
				"primary_window": {"used_percent":40,"reset_at":`+unixString(resetAt)+`,"limit_window_seconds":3600},
				"secondary_window": {"used_percent":10,"reset_after_seconds":120,"limit_window_seconds":604799}
			}
		}`), nil
	}}}

	before := time.Now().UTC()
	usage, err := client.FetchUsage(context.Background(), "access", "")
	after := time.Now().UTC()
	if err != nil {
		t.Fatal(err)
	}
	if usage.Hourly.ResetTime == nil || !usage.Hourly.ResetTime.Equal(resetAt) {
		t.Fatalf("Hourly.ResetTime = %v, want %v", usage.Hourly.ResetTime, resetAt)
	}
	if usage.Hourly.WindowMinutes == nil || *usage.Hourly.WindowMinutes != 60 {
		t.Fatalf("Hourly.WindowMinutes = %v, want 60", usage.Hourly.WindowMinutes)
	}
	if usage.Weekly.ResetTime == nil {
		t.Fatal("Weekly.ResetTime is nil")
	}
	minReset := before.Add(120 * time.Second)
	maxReset := after.Add(120 * time.Second)
	if usage.Weekly.ResetTime.Before(minReset) || usage.Weekly.ResetTime.After(maxReset) {
		t.Fatalf("Weekly.ResetTime = %v, want between %v and %v", usage.Weekly.ResetTime, minReset, maxReset)
	}
	if usage.Weekly.WindowMinutes == nil || *usage.Weekly.WindowMinutes != 10080 {
		t.Fatalf("Weekly.WindowMinutes = %v, want 10080", usage.Weekly.WindowMinutes)
	}
}

func TestFetchUsageDefaultsMissingWindows(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{}`), nil
	}}}

	usage, err := client.FetchUsage(context.Background(), "access", "")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Hourly.Present || usage.Weekly.Present {
		t.Fatalf("Present = hourly %v weekly %v, want false", usage.Hourly.Present, usage.Weekly.Present)
	}
	if *usage.Hourly.Percentage != 100 || *usage.Weekly.Percentage != 100 {
		t.Fatalf("percentages = %d/%d, want 100/100", *usage.Hourly.Percentage, *usage.Weekly.Percentage)
	}
}

func TestFetchProfileSelectsExpectedAccount(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"accounts": [
				{"id":"acct_a","name":"A","structure":"personal","organization_id":"org_a"},
				{"id":"acct_b","display_name":"B","account_structure":"workspace","organization_id":"org_b"}
			],
			"account_ordering": ["acct_a"]
		}`), nil
	}}}

	profile, err := client.FetchProfile(context.Background(), "access", "acct_b", "")
	if err != nil {
		t.Fatal(err)
	}
	if profile.AccountID != "acct_b" || profile.AccountName != "B" || profile.AccountStructure != "workspace" || profile.OrganizationID != "org_b" {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestFetchProfileFallsBackToAccountOrderingAndOrganization(t *testing.T) {
	orderingClient := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{
			"accounts": {
				"one": {"id":"acct_1","name":"One"},
				"two": {"id":"acct_2","name":"Two"}
			},
			"account_ordering": ["acct_2"]
		}`), nil
	}}}
	ordered, err := orderingClient.FetchProfile(context.Background(), "access", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if ordered.AccountID != "acct_2" || ordered.AccountName != "Two" {
		t.Fatalf("ordered profile = %+v", ordered)
	}

	orgClient := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `[
			{"id":"acct_1","name":"One","organization_id":"org_1"},
			{"id":"acct_2","name":"Two","organization_id":"org_2"}
		]`), nil
	}}}
	byOrg, err := orgClient.FetchProfile(context.Background(), "access", "", "org_2")
	if err != nil {
		t.Fatal(err)
	}
	if byOrg.AccountID != "acct_2" || byOrg.OrganizationID != "org_2" {
		t.Fatalf("org profile = %+v", byOrg)
	}
}

func TestFetchUsageReturnsServiceErrorWithExtractedCode(t *testing.T) {
	client := Client{HTTPClient: fakeHTTPClient{do: func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusTooManyRequests, `{"detail":{"code":"limit_reached"}}`), nil
	}}}

	_, err := client.FetchUsage(context.Background(), "access", "")
	var serviceErr ServiceError
	if !errors.As(err, &serviceErr) {
		t.Fatalf("err = %T %v, want ServiceError", err, err)
	}
	if serviceErr.StatusCode != http.StatusTooManyRequests || serviceErr.Code != "limit_reached" {
		t.Fatalf("serviceErr = %+v", serviceErr)
	}
	if !strings.Contains(serviceErr.Error(), "error_code:limit_reached") {
		t.Fatalf("message = %q, want extracted code", serviceErr.Error())
	}
}

func TestIsBannedOrDisabledAndQuotaLimited(t *testing.T) {
	if !IsBannedOrDisabled("account disabled", http.StatusOK) {
		t.Fatal("disabled message was not banned/disabled")
	}
	if !IsBannedOrDisabled("anything", http.StatusForbidden) {
		t.Fatal("403 was not banned/disabled")
	}
	if IsBannedOrDisabled("active", http.StatusOK) {
		t.Fatal("active account was banned/disabled")
	}
	if !IsQuotaLimited("rate limit reached") || !IsQuotaLimited("quota exhausted") || !IsQuotaLimited("limit_reached") {
		t.Fatal("quota limited message was not detected")
	}
	if IsQuotaLimited("account disabled") {
		t.Fatal("disabled account was quota limited")
	}
}

func unixString(value time.Time) string {
	return strconv.FormatInt(value.Unix(), 10)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
