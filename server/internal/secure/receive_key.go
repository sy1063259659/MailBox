package secure

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

const receiveKeyPrefix = "hme_"

func GenerateReceiveKey() (string, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", err
	}
	return receiveKeyPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func ReceiveKeyDigest(secret []byte, receiveKey string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strings.TrimSpace(receiveKey)))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyReceiveKey(secret []byte, receiveKey, encodedDigest string) bool {
	digest, err := hex.DecodeString(strings.TrimSpace(encodedDigest))
	if err != nil || len(digest) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strings.TrimSpace(receiveKey)))
	return hmac.Equal(mac.Sum(nil), digest)
}

func ValidateReceiveKey(receiveKey string) error {
	value := strings.TrimSpace(receiveKey)
	if !strings.HasPrefix(value, receiveKeyPrefix) {
		return errors.New("invalid receive key")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, receiveKeyPrefix))
	if err != nil || len(payload) != 32 {
		return errors.New("invalid receive key")
	}
	return nil
}
