package api

import (
	"strings"
	"testing"

	"gptbox-server/internal/icloudhme"
)

type panicICloudHMEClient struct{}

func (panicICloudHMEClient) ValidateSession() error                  { return nil }
func (panicICloudHMEClient) AccountInfo() *icloudhme.AccountInfo     { return nil }
func (panicICloudHMEClient) ListAliases() ([]icloudhme.Alias, error) { return nil, nil }
func (panicICloudHMEClient) CreateAlias(string, int) (*icloudhme.CreateResult, error) {
	return nil, nil
}
func (panicICloudHMEClient) DeactivateHME(string) (bool, error) { return true, nil }
func (panicICloudHMEClient) ReactivateHME(string) (bool, error) { return true, nil }
func (panicICloudHMEClient) Delete(string) error                { return nil }
func (panicICloudHMEClient) Login(string, string, icloudhme.OTPProvider) error {
	panic("invalid SRP challenge")
}
func (panicICloudHMEClient) GetCookies() map[string]string { return nil }

func TestSafeICloudHMELoginRecoversProtocolPanic(t *testing.T) {
	err := safeICloudHMELogin(panicICloudHMEClient{}, "user@example.com", "password", nil)
	if err == nil || !strings.Contains(err.Error(), "协议异常") {
		t.Fatalf("err = %v", err)
	}
}
