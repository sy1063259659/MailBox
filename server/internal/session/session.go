package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const (
	CookieName       = "gptbox_session"
	LegacyCookieName = "mailbox_session"
)

type Manager struct {
	secret []byte
	secure bool
}

func NewManager(secret []byte, secure bool) Manager {
	return Manager{secret: secret, secure: secure}
}

func (m Manager) Set(w http.ResponseWriter, username string) {
	value := m.sign(username)
	http.SetCookie(w, m.cookie(CookieName, value, int((24*time.Hour).Seconds())))
	http.SetCookie(w, m.cookie(LegacyCookieName, "", -1))
}

func (m Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, m.cookie(CookieName, "", -1))
	http.SetCookie(w, m.cookie(LegacyCookieName, "", -1))
}

func (m Manager) Username(r *http.Request) (string, bool) {
	for _, cookieName := range []string{CookieName, LegacyCookieName} {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			continue
		}
		if username, ok := m.usernameFromCookieValue(cookie.Value); ok {
			return username, true
		}
	}
	return "", false
}

func (m Manager) usernameFromCookieValue(value string) (string, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", false
	}
	usernameBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	username := string(usernameBytes)
	if !hmac.Equal([]byte(value), []byte(m.sign(username))) {
		return "", false
	}
	return username, true
}

func (m Manager) cookie(name string, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
	}
}

func (m Manager) sign(username string) string {
	usernamePart := base64.RawURLEncoding.EncodeToString([]byte(username))
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(usernamePart))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return usernamePart + "." + signature
}
