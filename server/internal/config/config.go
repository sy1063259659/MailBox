package config

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"strings"
)

const DefaultAddr = "127.0.0.1:8787"

type Config struct {
	Addr          string
	SQLitePath    string
	AdminUsername string
	AdminPassword string
	SessionSecret []byte
	TokenKey      []byte
	StaticDir     string
	CookieSecure  bool
}

func Load() (Config, error) {
	cfg := Config{
		Addr:          getEnvAny("GPTBOX_SERVER_ADDR", "MAILBOX_SERVER_ADDR", DefaultAddr),
		SQLitePath:    getEnvAny("GPTBOX_SQLITE_PATH", "MAILBOX_SQLITE_PATH", "./data/mailbox.db"),
		AdminUsername: strings.TrimSpace(getEnvAny("GPTBOX_ADMIN_USERNAME", "MAILBOX_ADMIN_USERNAME", "admin")),
		AdminPassword: getEnvAny("GPTBOX_ADMIN_PASSWORD", "MAILBOX_ADMIN_PASSWORD", ""),
		StaticDir:     getEnvAny("GPTBOX_STATIC_DIR", "MAILBOX_STATIC_DIR", "./dist"),
		CookieSecure:  getBoolEnvAny("GPTBOX_COOKIE_SECURE", "MAILBOX_COOKIE_SECURE", false),
	}

	if strings.TrimSpace(cfg.AdminPassword) == "" {
		return Config{}, errors.New("GPTBOX_ADMIN_PASSWORD or MAILBOX_ADMIN_PASSWORD is required")
	}

	sessionSecret, err := deriveKey(getEnvAny("GPTBOX_SESSION_SECRET", "MAILBOX_SESSION_SECRET", ""))
	if err != nil {
		return Config{}, err
	}
	tokenKey, err := deriveKey(getEnvAny("GPTBOX_TOKEN_KEY", "MAILBOX_TOKEN_KEY", ""))
	if err != nil {
		return Config{}, err
	}

	cfg.SessionSecret = sessionSecret
	cfg.TokenKey = tokenKey
	return cfg, nil
}

func getEnvAny(primaryKey string, legacyKey string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(primaryKey)); value != "" {
		return value
	}
	return getEnv(legacyKey, fallback)
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getBoolEnvAny(primaryKey string, legacyKey string, fallback bool) bool {
	if strings.TrimSpace(os.Getenv(primaryKey)) != "" {
		return getBoolEnv(primaryKey, fallback)
	}
	return getBoolEnv(legacyKey, fallback)
}

func getBoolEnv(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func deriveKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("GPTBOX_SESSION_SECRET/GPTBOX_TOKEN_KEY or legacy MAILBOX_SESSION_SECRET/MAILBOX_TOKEN_KEY are required")
	}

	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}

	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}
