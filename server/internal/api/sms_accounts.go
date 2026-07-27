package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gptbox-server/internal/store"
)

const (
	smsReceiveHost     = "qk.sms777.top"
	smsMaxResponseSize = 128 * 1024
	maxSMSRemarkLength = 500
)

var (
	smsPhonePattern = regexp.MustCompile(`^\+?[0-9]{6,20}$`)
	smsCodePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:verification\s*code|security\s*code|one[- ]time\s*(?:password|code)|验证码|验证代码|校验码|动态码|otp|code)\s*(?:is|为|是|[:：\-])?\s*([A-Z0-9]{4,8})`),
		regexp.MustCompile(`(?i)\b([0-9]{6,8})\b`),
	}
)

type smsLatestFetcher interface {
	Fetch(context.Context, string) (string, error)
}

type smsHTTPClient struct {
	httpClient *http.Client
}

type smsAPI struct {
	store   *store.Store
	fetcher smsLatestFetcher
}

type smsImportRequest struct {
	Text      string `json:"text"`
	Overwrite bool   `json:"overwrite"`
}

type smsPhoneRequest struct {
	Phone string `json:"phone"`
}

type smsRemarkRequest struct {
	Phone  string `json:"phone"`
	Remark string `json:"remark"`
}

type smsStatusRequest struct {
	Phone  string `json:"phone"`
	Status string `json:"status"`
}

type smsBindingRequest struct {
	Phone       string   `json:"phone"`
	MailboxType string   `json:"mailboxType"`
	Email       string   `json:"email"`
	Emails      []string `json:"emails"`
}

type smsMailboxAssignmentRequest struct {
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type smsLatestResponse struct {
	OK        bool      `json:"ok"`
	Phone     string    `json:"phone"`
	Message   string    `json:"message"`
	Code      string    `json:"code"`
	Available bool      `json:"available"`
	CheckedAt time.Time `json:"checkedAt"`
}

func newSMSAPI(dataStore *store.Store) smsAPI {
	return smsAPI{
		store: dataStore,
		fetcher: &smsHTTPClient{httpClient: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 2 {
					return errors.New("接码接口重定向次数过多")
				}
				if !strings.EqualFold(request.URL.Hostname(), smsReceiveHost) {
					return errors.New("接码接口重定向地址不受信任")
				}
				return nil
			},
		}},
	}
}

func (api smsAPI) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := api.store.ListSMSAccounts(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "sms_list_failed", "加载接码账号失败")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "accounts": accounts})
}

func (api smsAPI) importAccounts(w http.ResponseWriter, r *http.Request) {
	var request smsImportRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	inputs, parseErrors := parseSMSImportText(request.Text)
	if len(inputs) == 0 {
		WriteJSON(w, http.StatusOK, store.ImportResult{Errors: parseErrors})
		return
	}
	result, err := api.store.ImportSMSAccounts(r.Context(), inputs, request.Overwrite)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "sms_import_failed", err.Error())
		return
	}
	result.Errors = parseErrors
	WriteJSON(w, http.StatusOK, result)
}

func (api smsAPI) updateRemark(w http.ResponseWriter, r *http.Request) {
	var request smsRemarkRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	phone := normalizeSMSPhone(request.Phone)
	remark := strings.TrimSpace(request.Remark)
	if phone == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "手机号不能为空")
		return
	}
	if len([]rune(remark)) > maxSMSRemarkLength {
		WriteError(w, http.StatusBadRequest, "bad_request", "备注最多 500 个字符")
		return
	}
	account, err := api.store.UpdateSMSRemark(r.Context(), phone, remark)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "sms_remark_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "account": account})
}

func (api smsAPI) updateStatus(w http.ResponseWriter, r *http.Request) {
	var request smsStatusRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	phone := normalizeSMSPhone(request.Phone)
	status := strings.ToLower(strings.TrimSpace(request.Status))
	if phone == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "手机号不能为空")
		return
	}
	account, err := api.store.UpdateSMSStatus(r.Context(), phone, status)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "sms_status_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "account": account})
}

func (api smsAPI) bindMailbox(w http.ResponseWriter, r *http.Request) {
	var request smsBindingRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	phone := normalizeSMSPhone(request.Phone)
	if phone == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "手机号不能为空")
		return
	}
	emails := request.Emails
	if len(emails) == 0 && strings.TrimSpace(request.Email) != "" {
		if request.MailboxType != "icloud_hme" {
			WriteError(w, http.StatusBadRequest, "sms_binding_failed", "接码账号只能绑定 iCloud 隐藏邮箱")
			return
		}
		emails = []string{request.Email}
	}
	account, err := api.store.BindSMSMailboxes(r.Context(), phone, emails)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "sms_binding_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "account": account})
}

func (api smsAPI) listMailboxes(w http.ResponseWriter, r *http.Request) {
	references, err := api.store.ListSMSMailboxReferences(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "sms_mailboxes_failed", "加载邮箱账号失败")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "mailboxes": references})
}

func (api smsAPI) assignMailbox(w http.ResponseWriter, r *http.Request) {
	var request smsMailboxAssignmentRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	phone := ""
	if strings.TrimSpace(request.Phone) != "" {
		phone = normalizeSMSPhone(request.Phone)
		if phone == "" {
			WriteError(w, http.StatusBadRequest, "bad_request", "手机号格式错误")
			return
		}
	}
	accounts, err := api.store.AssignSMSMailbox(r.Context(), email, phone)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "sms_binding_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "accounts": accounts})
}

func (api smsAPI) latestMessage(w http.ResponseWriter, r *http.Request) {
	var request smsPhoneRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	phone := normalizeSMSPhone(request.Phone)
	if phone == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "手机号不能为空")
		return
	}
	receiveURL, err := api.store.GetSMSReceiveURL(r.Context(), phone)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "sms_account_unavailable", err.Error())
		return
	}
	message, err := api.fetcher.Fetch(r.Context(), receiveURL)
	if err != nil {
		api.store.UpdateSMSCheckResult(r.Context(), phone, "接码接口暂时不可用")
		WriteError(w, http.StatusBadGateway, "sms_fetch_failed", "接码接口暂时不可用")
		return
	}
	api.store.UpdateSMSCheckResult(r.Context(), phone, "")
	code := extractSMSCode(message)
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, smsLatestResponse{
		OK: true, Phone: phone, Message: message, Code: code,
		Available: code != "", CheckedAt: time.Now(),
	})
}

func (api smsAPI) routeAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	phone, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/sms-accounts/"))
	if err != nil || normalizeSMSPhone(phone) == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "手机号格式错误")
		return
	}
	if err := api.store.DeleteSMSAccount(r.Context(), normalizeSMSPhone(phone)); err != nil {
		WriteError(w, http.StatusBadRequest, "sms_delete_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (client *smsHTTPClient) Fetch(ctx context.Context, rawURL string) (string, error) {
	parsed, err := validateSMSReceiveURL(rawURL)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", errors.New("创建接码请求失败")
	}
	request.Header.Set("Accept", "text/plain")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", errors.New("接码接口请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("接码接口返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, smsMaxResponseSize+1))
	if err != nil || len(body) > smsMaxResponseSize {
		return "", errors.New("接码接口响应读取失败")
	}
	return strings.TrimSpace(string(body)), nil
}

func parseSMSImportText(text string) ([]store.SMSAccountInput, []string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	inputs := make([]store.SMSAccountInput, 0, len(lines))
	parseErrors := []string{}
	seen := map[string]bool{}
	for index, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "----")
		if len(parts) != 2 {
			parseErrors = append(parseErrors, fmt.Sprintf("第 %d 行：格式必须为 手机号----取件URL", index+1))
			continue
		}
		phone := normalizeSMSPhone(parts[0])
		if phone == "" {
			parseErrors = append(parseErrors, fmt.Sprintf("第 %d 行：手机号格式错误", index+1))
			continue
		}
		receiveURL, err := validateSMSReceiveURL(parts[1])
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("第 %d 行：%s", index+1, err.Error()))
			continue
		}
		if seen[phone] {
			parseErrors = append(parseErrors, fmt.Sprintf("第 %d 行：手机号重复", index+1))
			continue
		}
		seen[phone] = true
		inputs = append(inputs, store.SMSAccountInput{
			Phone: phone, ReceiveURL: receiveURL.String(), ProviderHost: receiveURL.Hostname(),
		})
	}
	return inputs, parseErrors
}

func normalizeSMSPhone(value string) string {
	value = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(value))
	if !smsPhonePattern.MatchString(value) {
		return ""
	}
	return value
}

func validateSMSReceiveURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("取件 URL 格式错误")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("取件 URL 必须使用 HTTP 或 HTTPS")
	}
	if port := parsed.Port(); port != "" && !((parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443")) {
		return nil, errors.New("取件 URL 端口不受支持")
	}
	if !strings.EqualFold(parsed.Hostname(), smsReceiveHost) || !strings.HasPrefix(parsed.EscapedPath(), "/sms/msg/") {
		return nil, errors.New("只支持 sms777 接码 URL")
	}
	return parsed, nil
}

func extractSMSCode(message string) string {
	if strings.Contains(message, "暂无短信") {
		return ""
	}
	for _, pattern := range smsCodePatterns {
		match := pattern.FindStringSubmatch(message)
		if len(match) > 1 {
			return strings.ToUpper(match[1])
		}
	}
	return ""
}
