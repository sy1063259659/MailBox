package icloudhme

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	http "github.com/bogdanfinn/fhttp"
	"golang.org/x/crypto/pbkdf2"
)

func TestAuthUsesChinaMainlandEndpoints(t *testing.T) {
	client := &Client{Host: "icloud.com.cn"}
	state := &authState{frameId: "frame", clientId: OAuthClientID}

	if got := client.accountLoginURL(); got != "https://setup.icloud.com.cn/setup/ws/1/accountLogin" {
		t.Fatalf("accountLoginURL = %q", got)
	}
	if got := client.accountCountryCode(state); got != "CHN" {
		t.Fatalf("accountCountryCode = %q", got)
	}
	if got := client.authStartURL(state); !strings.Contains(got, "redirect_uri=https%3A%2F%2Fwww.icloud.com.cn") {
		t.Fatalf("authStartURL = %q", got)
	}
}

func TestAuthKeepsInternationalEndpoints(t *testing.T) {
	client := &Client{Host: "icloud.com"}
	state := &authState{frameId: "frame", clientId: OAuthClientID}

	if got := client.accountLoginURL(); got != "https://setup.icloud.com/setup/ws/1/accountLogin" {
		t.Fatalf("accountLoginURL = %q", got)
	}
	if got := client.accountCountryCode(state); got != "USA" {
		t.Fatalf("accountCountryCode = %q", got)
	}
	if got := client.authStartURL(state); !strings.Contains(got, "redirect_uri=https%3A%2F%2Fwww.icloud.com") {
		t.Fatalf("authStartURL = %q", got)
	}
}

func TestAuthUsesAppleCountryHeaderWhenAvailable(t *testing.T) {
	client := &Client{Host: "icloud.com.cn"}
	state := &authState{}
	headers := make(http.Header)
	headers.Set("X-Apple-ID-Account-Country", "USA")
	headers.Set("X-Apple-ID-Session-Id", "session-id")
	headers.Set("scnt", "scnt-value")
	headers.Set("X-Apple-Session-Token", "session-token")
	headers.Set("X-Apple-TwoSV-Trust-Token", "trust-token")

	captureAuthResponseHeaders(state, headers)

	if state.accountCountry != "USA" || state.sessionID != "session-id" || state.scnt != "scnt-value" || state.authToken != "session-token" || state.trustToken != "trust-token" {
		t.Fatalf("state = %+v", state)
	}
	if got := client.accountCountryCode(state); got != "USA" {
		t.Fatalf("accountCountryCode = %q", got)
	}
}

func TestAuthHeadersIncludeRegionalOAuthContext(t *testing.T) {
	client := &Client{Host: "icloud.com.cn"}
	state := &authState{frameId: "frame", authAttr: "attributes"}
	headers := client.updateAuthHeaders(make(http.Header), state)

	if headers.Get("X-Apple-OAuth-Redirect-URI") != "https://www.icloud.com.cn" {
		t.Fatalf("redirect = %q", headers.Get("X-Apple-OAuth-Redirect-URI"))
	}
	if headers.Get("X-Apple-Auth-Attributes") != "attributes" || headers.Get("X-Apple-Widget-Key") != OAuthClientID {
		t.Fatalf("headers = %+v", headers)
	}
}

func TestDeriveAuthPasswordKeySupportsS2KFO(t *testing.T) {
	password := "test-password"
	salt := []byte("test-salt")
	digest := sha256.Sum256([]byte(password))
	want := pbkdf2.Key([]byte(fmt.Sprintf("%x", digest)), salt, 1000, 32, sha256.New)

	got, err := deriveAuthPasswordKey(password, "s2k_fo", salt, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("derived key does not use s2k_fo password encoding")
	}
}
