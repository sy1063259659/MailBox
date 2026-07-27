package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseSMSImportText(t *testing.T) {
	inputs, errors := parseSMSImportText(`
+12025550123----http://qk.sms777.top/sms/msg/USA/example-token-one
+12025550124----https://qk.sms777.top/sms/msg/USA/example-token-two
`)
	if len(errors) != 0 {
		t.Fatalf("errors = %v, want none", errors)
	}
	if len(inputs) != 2 {
		t.Fatalf("len(inputs) = %d, want 2", len(inputs))
	}
	if inputs[0].Phone != "+12025550123" || inputs[0].ProviderHost != smsReceiveHost {
		t.Fatalf("first input = %+v", inputs[0])
	}
}

func TestParseSMSImportRejectsUnsafeURLsAndDuplicates(t *testing.T) {
	inputs, errors := parseSMSImportText(`
+12025550123----http://127.0.0.1/admin
+12025550124----http://qk.sms777.top/sms/msg/USA/token
+12025550124----http://qk.sms777.top/sms/msg/USA/token-two
bad-phone----http://qk.sms777.top/sms/msg/USA/token
`)
	if len(inputs) != 1 {
		t.Fatalf("inputs = %+v, want one valid input", inputs)
	}
	joined := strings.Join(errors, "\n")
	for _, want := range []string{"只支持 sms777", "手机号重复", "手机号格式错误"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %v, missing %q", errors, want)
		}
	}
}

func TestExtractSMSCode(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{"Your OpenAI verification code is: 428193", "428193"},
		{"【ChatGPT】验证码：A7K92Q，请勿泄露。", "A7K92Q"},
		{"【2026-7-27 9:33】您的 OpenAI 验证代码是：236998", "236998"},
		{"【OpenAI/ChatGPT】暂无短信，到期时间：2026-7-28 12:00", ""},
		{"Message without a verification code", ""},
	}
	for _, test := range tests {
		if got := extractSMSCode(test.message); got != test.want {
			t.Fatalf("extractSMSCode(%q) = %q, want %q", test.message, got, test.want)
		}
	}
}

func TestValidateSMSReceiveURL(t *testing.T) {
	if _, err := validateSMSReceiveURL("http://qk.sms777.top/sms/msg/USA/token"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		"http://localhost/sms/msg/USA/token",
		"http://qk.sms777.top/private",
		"http://qk.sms777.top:8080/sms/msg/USA/token",
		"file:///etc/passwd",
		"http://user:pass@qk.sms777.top/sms/msg/USA/token",
	} {
		if _, err := validateSMSReceiveURL(value); err == nil {
			t.Fatalf("validateSMSReceiveURL(%q) should fail", value)
		}
	}
}

func TestSMSBindingRejectsNonHiddenMailbox(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/sms-accounts/binding",
		strings.NewReader(`{"phone":"+12025550123","mailboxType":"outlook","email":"user@outlook.com"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	(smsAPI{}).bindMailbox(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "只能绑定 iCloud 隐藏邮箱") {
		t.Fatalf("body = %s", response.Body.String())
	}
}
