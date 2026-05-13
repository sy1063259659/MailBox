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
	DatabaseURL   string
	AdminUsername string
	AdminPassword string
	SessionSecret []byte
	TokenKey      []byte
	StaticDir     string
}

func Load() (Config, error) {
	cfg := Config{
		Addr:          getEnv("MAILBOX_SERVER_ADDR", DefaultAddr),
		DatabaseURL:   strings.TrimSpace(os.Getenv("DATABASE_URL")),
		AdminUsername: strings.TrimSpace(getEnv("MAILBOX_ADMIN_USERNAME", "admin")),
		AdminPassword: os.Getenv("MAILBOX_ADMIN_PASSWORD"),
		StaticDir:     getEnv("MAILBOX_STATIC_DIR", "./dist"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if strings.TrimSpace(cfg.AdminPassword) == "" {
		return Config{}, errors.New("MAILBOX_ADMIN_PASSWORD is required")
	}

	sessionSecret, err := deriveKey(os.Getenv("MAILBOX_SESSION_SECRET"))
	if err != nil {
		return Config{}, err
	}
	tokenKey, err := deriveKey(os.Getenv("MAILBOX_TOKEN_KEY"))
	if err != nil {
		return Config{}, err
	}

	cfg.SessionSecret = sessionSecret
	cfg.TokenKey = tokenKey
	return cfg, nil
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func deriveKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("MAILBOX_SESSION_SECRET and MAILBOX_TOKEN_KEY are required")
	}

	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}

	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}
