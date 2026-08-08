package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"gptbox-server/internal/secure"
)

const integrationAPIKeySetting = "integration_api_key"

func (s *Store) EnsureIntegrationAPIKey(ctx context.Context, fallback string) (string, error) {
	key, err := s.integrationAPIKey(ctx)
	if err == nil && key != "" {
		return key, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	key = strings.TrimSpace(fallback)
	if key == "" {
		key, err = generateIntegrationAPIKey()
		if err != nil {
			return "", err
		}
	}
	encrypted, err := secure.EncryptString(s.tokenKey, key)
	if err != nil {
		return "", err
	}
	result, err := s.pool.Exec(ctx, `
		INSERT INTO application_settings (setting_key,value_encrypted,updated_at)
		VALUES ($1,$2,now()) ON CONFLICT (setting_key) DO NOTHING`,
		integrationAPIKeySetting, encrypted)
	if err != nil {
		return "", fmt.Errorf("store: initialize integration API key: %w", err)
	}
	if result.RowsAffected() == 1 {
		return key, nil
	}
	return s.integrationAPIKey(ctx)
}

func (s *Store) ResetIntegrationAPIKey(ctx context.Context) (string, error) {
	key, err := generateIntegrationAPIKey()
	if err != nil {
		return "", err
	}
	encrypted, err := secure.EncryptString(s.tokenKey, key)
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO application_settings (setting_key,value_encrypted,updated_at)
		VALUES ($1,$2,now())
		ON CONFLICT (setting_key) DO UPDATE
		SET value_encrypted=EXCLUDED.value_encrypted,updated_at=now()`,
		integrationAPIKeySetting, encrypted); err != nil {
		return "", fmt.Errorf("store: reset integration API key: %w", err)
	}
	return key, nil
}

func (s *Store) integrationAPIKey(ctx context.Context) (string, error) {
	var encrypted string
	if err := s.pool.QueryRow(ctx, `
		SELECT value_encrypted FROM application_settings WHERE setting_key=$1`,
		integrationAPIKeySetting).Scan(&encrypted); err != nil {
		return "", err
	}
	key, err := secure.DecryptString(s.tokenKey, encrypted)
	if err != nil {
		return "", fmt.Errorf("store: decrypt integration API key: %w", err)
	}
	return strings.TrimSpace(key), nil
}

func generateIntegrationAPIKey() (string, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("store: generate integration API key: %w", err)
	}
	return hex.EncodeToString(payload), nil
}
