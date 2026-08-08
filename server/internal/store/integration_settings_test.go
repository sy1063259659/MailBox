package store

import (
	"encoding/hex"
	"testing"
)

func TestGenerateIntegrationAPIKey(t *testing.T) {
	first, err := generateIntegrationAPIKey()
	if err != nil {
		t.Fatalf("generate first key: %v", err)
	}
	second, err := generateIntegrationAPIKey()
	if err != nil {
		t.Fatalf("generate second key: %v", err)
	}
	if len(first) != 64 {
		t.Fatalf("key length = %d, want 64", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("key is not hexadecimal: %v", err)
	}
	if first == second {
		t.Fatal("two generated API keys must differ")
	}
}
