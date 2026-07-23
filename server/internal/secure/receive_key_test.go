package secure

import (
	"strings"
	"testing"
)

func TestGenerateReceiveKeyAndVerifyDigest(t *testing.T) {
	key, err := GenerateReceiveKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "hme_") || ValidateReceiveKey(key) != nil {
		t.Fatalf("invalid generated key %q", key)
	}
	digest := ReceiveKeyDigest([]byte("01234567890123456789012345678901"), key)
	if !VerifyReceiveKey([]byte("01234567890123456789012345678901"), key, digest) {
		t.Fatal("generated key did not verify")
	}
	if VerifyReceiveKey([]byte("01234567890123456789012345678901"), key+"x", digest) {
		t.Fatal("modified key unexpectedly verified")
	}
}

func TestValidateReceiveKeyRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "secret", "hme_short", "hme_***"} {
		if ValidateReceiveKey(value) == nil {
			t.Fatalf("ValidateReceiveKey(%q) succeeded", value)
		}
	}
}
