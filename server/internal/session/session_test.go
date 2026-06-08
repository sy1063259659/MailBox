package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionCookieSecureFlag(t *testing.T) {
	manager := NewManager([]byte("test-secret"), true)
	recorder := httptest.NewRecorder()

	manager.Set(recorder, "admin")

	cookies := recorder.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("len(cookies) = %d, want 2", len(cookies))
	}
	cookie := findCookie(t, cookies, CookieName)
	if cookie.Name != CookieName {
		t.Fatalf("cookie.Name = %q, want %q", cookie.Name, CookieName)
	}
	if !cookie.HttpOnly {
		t.Fatal("session cookie should be HttpOnly")
	}
	if !cookie.Secure {
		t.Fatal("session cookie should be Secure when enabled")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie.SameSite = %v, want Lax", cookie.SameSite)
	}

	legacyCookie := findCookie(t, cookies, LegacyCookieName)
	if legacyCookie.MaxAge >= 0 {
		t.Fatalf("legacy cookie MaxAge = %d, want negative clear cookie", legacyCookie.MaxAge)
	}
}

func TestSessionUsernameRejectsTamperedCookie(t *testing.T) {
	manager := NewManager([]byte("test-secret"), false)
	recorder := httptest.NewRecorder()
	manager.Set(recorder, "admin")

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range recorder.Result().Cookies() {
		cookie.Value += "tampered"
		request.AddCookie(cookie)
	}

	if username, ok := manager.Username(request); ok {
		t.Fatalf("Username() = (%q, true), want false", username)
	}
}

func TestSessionUsernameAcceptsLegacyCookie(t *testing.T) {
	manager := NewManager([]byte("test-secret"), false)
	recorder := httptest.NewRecorder()
	manager.Set(recorder, "admin")

	issuedCookie := findCookie(t, recorder.Result().Cookies(), CookieName)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{
		Name:  LegacyCookieName,
		Value: issuedCookie.Value,
	})

	username, ok := manager.Username(request)
	if !ok {
		t.Fatal("Username() did not accept legacy cookie")
	}
	if username != "admin" {
		t.Fatalf("Username() = %q, want admin", username)
	}
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}
