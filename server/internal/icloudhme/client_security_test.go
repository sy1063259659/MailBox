package icloudhme

import (
	"strings"
	"testing"
)

func TestHTTPStatusErrorDoesNotIncludeResponseBodyContract(t *testing.T) {
	err := httpStatusError(421)
	if strings.Contains(err.Error(), "{") || err.Error() != "HTTP 421" {
		t.Fatalf("error = %q", err.Error())
	}
}
