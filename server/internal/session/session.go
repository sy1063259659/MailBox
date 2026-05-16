package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const CookieName = "mailbox_session"

type Manager struct {
	secret []byte
	secure bool
}

func NewManager(secret []byte, secure bool) Manager {
	return Manager{secret: secret, secure: secure}
}

func (m Manager) Set(w http.ResponseWriter, username string) {
	value := m.sign(username)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int((24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
	})
}

func (m Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   m.secure,
	})
}

func (m Manager) Username(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return "", false
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return "", false
	}
	usernameBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	username := string(usernameBytes)
	if !hmac.Equal([]byte(cookie.Value), []byte(m.sign(username))) {
		return "", false
	}
	return username, true
}

func (m Manager) sign(username string) string {
	usernamePart := base64.RawURLEncoding.EncodeToString([]byte(username))
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(usernamePart))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return usernamePart + "." + signature
}
