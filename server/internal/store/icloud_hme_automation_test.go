package store

import (
	"strings"
	"testing"
	"time"
)

func TestICloudHMEAutomationCooldownSchedule(t *testing.T) {
	want := []time.Duration{
		5 * time.Minute,
		8 * time.Minute,
		12 * time.Minute,
		20 * time.Minute,
		30 * time.Minute,
		45 * time.Minute,
	}
	for index, expected := range want {
		if got := iCloudHMEAutomationCooldownDelay(index + 1); got != expected {
			t.Fatalf("level %d delay = %v, want %v", index+1, got, expected)
		}
	}
	if got := iCloudHMEAutomationCooldownDelay(99); got != 45*time.Minute {
		t.Fatalf("capped delay = %v, want 45m", got)
	}
}

func TestNormalizePublicMailOrigin(t *testing.T) {
	valid := map[string]string{
		"":                                "",
		"   ":                             "",
		"inbox.example.com":               "https://inbox.example.com",
		"https://inbox.example.com":       "https://inbox.example.com",
		"https://inbox.example.com/":      "https://inbox.example.com",
		"http://127.0.0.1:8787":           "http://127.0.0.1:8787",
		" https://inbox.example.com:443 ": "https://inbox.example.com:443",
		// A path is dropped: callers append the API path themselves.
		"https://inbox.example.com/ignored": "https://inbox.example.com",
	}
	for input, want := range valid {
		got, err := NormalizePublicMailOrigin(input)
		if err != nil {
			t.Fatalf("NormalizePublicMailOrigin(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizePublicMailOrigin(%q) = %q, want %q", input, got, want)
		}
	}

	invalid := []string{
		"ftp://inbox.example.com",
		"https://",
		"https://inbox.example.com?a=1",
		"https://inbox.example.com#frag",
		"https://" + strings.Repeat("a", 300),
	}
	for _, input := range invalid {
		if _, err := NormalizePublicMailOrigin(input); err == nil {
			t.Fatalf("NormalizePublicMailOrigin(%q) expected an error", input)
		}
	}
}
