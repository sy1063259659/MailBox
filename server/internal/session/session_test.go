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
	if len(cookies) != 1 {
		t.Fatalf("len(cookies) = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
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
