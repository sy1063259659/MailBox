package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gptbox-server/internal/store"
)

const (
	iCloudLatestMailURL   = "https://icloud.52moyu.net/api/mail/latest"
	iCloudMailMaxBodySize = 2 << 20
)

var iCloudEmailPattern = regexp.MustCompile(`(?i)^[^\s@]+@icloud\.com$`)

type iCloudLatestFetcher interface {
	Latest(context.Context, string, string) (iCloudLatestMailResponse, error)
}

type iCloudAPI struct {
	store       *store.Store
	latestFetch iCloudLatestFetcher
}

type iCloudAccountsResponse struct {
	OK       bool                  `json:"ok"`
	Accounts []store.ICloudAccount `json:"accounts"`
}

type iCloudGroupsResponse struct {
	OK     bool                `json:"ok"`
	Groups []store.ICloudGroup `json:"groups"`
}

type importICloudAccountsRequest struct {
	Text      string `json:"text"`
	Overwrite bool   `json:"overwrite"`
	Group     string `json:"group"`
}

type moveICloudAccountsRequest struct {
	Emails []string `json:"emails"`
	Group  string   `json:"group"`
}

type updateICloudRemarkRequest struct {
	Email  string `json:"email"`
	Remark string `json:"remark"`
}

type latestICloudMailRequest struct {
	Email string `json:"email"`
}

type iCloudMailboxInfo struct {
	ID      int64  `json:"id"`
	Address string `json:"address"`
	Active  bool   `json:"active"`
}

type iCloudLatestEmail struct {
	ID               int64  `json:"id"`
	To               string `json:"to"`
	From             string `json:"from"`
	Subject          string `json:"subject"`
	Text             string `json:"text"`
	ReceivedAt       string `json:"received_at"`
	CreatedAt        string `json:"created_at"`
	VerificationCode string `json:"verification_code"`
	MailType         string `json:"mail_type"`
	InviteLink       string `json:"invite_link"`
	ProcessStatus    string `json:"process_status"`
}

type iCloudLatestMailResponse struct {
	OK      bool               `json:"ok"`
	Mailbox *iCloudMailboxInfo `json:"mailbox,omitempty"`
	Email   *iCloudLatestEmail `json:"email,omitempty"`
	Error   string             `json:"error,omitempty"`
	Message string             `json:"message,omitempty"`
}

type iCloudLatestClient struct {
	baseURL    string
	httpClient *http.Client
}

func newICloudLatestClient() *iCloudLatestClient {
	return &iCloudLatestClient{
		baseURL: iCloudLatestMailURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (client *iCloudLatestClient) Latest(ctx context.Context, email string, key string) (iCloudLatestMailResponse, error) {
	endpoint, err := url.Parse(client.baseURL)
	if err != nil {
		return iCloudLatestMailResponse{}, errors.New("iCloud 收件接口配置错误")
	}
	query := endpoint.Query()
	query.Set("address", email)
	query.Set("key", key)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return iCloudLatestMailResponse{}, errors.New("创建 iCloud 收件请求失败")
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return iCloudLatestMailResponse{}, errors.New("iCloud 收件接口请求失败")
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return iCloudLatestMailResponse{}, fmt.Errorf("iCloud 收件接口返回 HTTP %d", response.StatusCode)
	}

	var payload iCloudLatestMailResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, iCloudMailMaxBodySize))
	if err := decoder.Decode(&payload); err != nil {
		return iCloudLatestMailResponse{}, errors.New("iCloud 收件接口返回格式异常")
	}
	if !payload.OK {
		message := strings.TrimSpace(payload.Message)
		if message == "" {
			message = strings.TrimSpace(payload.Error)
		}
		if message == "" {
			message = "iCloud 收件接口返回失败"
		}
		if key != "" {
			message = strings.ReplaceAll(message, key, "[redacted]")
		}
		return iCloudLatestMailResponse{}, errors.New(message)
	}
	return payload, nil
}

func (api iCloudAPI) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := api.store.ListICloudAccounts(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, iCloudAccountsResponse{OK: true, Accounts: accounts})
}

func (api iCloudAPI) importAccounts(w http.ResponseWriter, r *http.Request) {
	var req importICloudAccountsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	inputs, parseErrors := parseICloudImportText(req.Text)
	applyICloudImportGroup(inputs, req.Group)
	if len(inputs) == 0 {
		if len(parseErrors) == 0 {
			parseErrors = append(parseErrors, "没有可导入的 iCloud 账号")
		}
		WriteJSON(w, http.StatusOK, store.ImportResult{Errors: parseErrors})
		return
	}

	result, err := api.store.ImportICloudAccounts(r.Context(), inputs, req.Overwrite)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	result.Errors = append(parseErrors, result.Errors...)
	WriteJSON(w, http.StatusOK, result)
}

func (api iCloudAPI) latestMail(w http.ResponseWriter, r *http.Request) {
	var req latestICloudMailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !iCloudEmailPattern.MatchString(email) {
		WriteError(w, http.StatusBadRequest, "bad_request", "只支持 @icloud.com 邮箱")
		return
	}
	credentials, err := api.store.GetICloudCredentials(r.Context(), email)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	payload, err := api.latestFetch.Latest(r.Context(), credentials.Email, credentials.Key)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "icloud_mail_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, payload)
}

func (api iCloudAPI) updateRemark(w http.ResponseWriter, r *http.Request) {
	var req updateICloudRemarkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "email is required")
		return
	}
	remark := strings.TrimSpace(req.Remark)
	if len([]rune(remark)) > maxAccountRemarkLength {
		WriteError(w, http.StatusBadRequest, "bad_request", "备注最多 500 个字符")
		return
	}
	account, err := api.store.UpdateICloudAccountRemark(r.Context(), email, remark)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "account": account})
}

func (api iCloudAPI) moveAccounts(w http.ResponseWriter, r *http.Request) {
	var req moveICloudAccountsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := api.store.MoveICloudAccountsToGroup(r.Context(), req.Emails, req.Group); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (api iCloudAPI) deleteAccount(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimPrefix(r.URL.Path, "/api/icloud-accounts/")
	if email == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "email is required")
		return
	}
	if err := api.store.DeleteICloudAccount(r.Context(), email); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (api iCloudAPI) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := api.store.ListICloudGroups(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, iCloudGroupsResponse{OK: true, Groups: groups})
}

func (api iCloudAPI) createGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	group, err := api.store.CreateICloudGroup(r.Context(), req.Name)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "group": group})
}

func (api iCloudAPI) reorderGroups(w http.ResponseWriter, r *http.Request) {
	var req reorderGroupsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	groups, err := api.store.ReorderICloudGroups(r.Context(), req.IDs)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, iCloudGroupsResponse{OK: true, Groups: groups})
}

func (api iCloudAPI) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseICloudGroupID(w, r.URL.Path)
	if !ok {
		return
	}
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	group, err := api.store.RenameICloudGroup(r.Context(), id, req.Name)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "group": group})
}

func (api iCloudAPI) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseICloudGroupID(w, r.URL.Path)
	if !ok {
		return
	}
	if err := api.store.DeleteICloudGroup(r.Context(), id); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func parseICloudGroupID(w http.ResponseWriter, path string) (int64, bool) {
	raw := strings.TrimPrefix(path, "/api/icloud-groups/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if raw == "" || err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "group id is required")
		return 0, false
	}
	return id, true
}

func parseICloudImportText(text string) ([]store.ICloudAccountInput, []string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	inputs := []store.ICloudAccountInput{}
	parseErrors := []string{}

	for index, rawLine := range lines {
		lineNumber := index + 1
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "----")
		if len(parts) != 2 {
			parseErrors = append(parseErrors, "第 "+strconv.Itoa(lineNumber)+" 行格式必须为：邮箱----密钥")
			continue
		}
		email := strings.ToLower(strings.TrimSpace(parts[0]))
		key := strings.TrimSpace(parts[1])
		if email == "" {
			parseErrors = append(parseErrors, "第 "+strconv.Itoa(lineNumber)+" 行邮箱不能为空")
			continue
		}
		if !iCloudEmailPattern.MatchString(email) {
			parseErrors = append(parseErrors, "第 "+strconv.Itoa(lineNumber)+" 行只支持 @icloud.com："+email)
			continue
		}
		if key == "" {
			parseErrors = append(parseErrors, "第 "+strconv.Itoa(lineNumber)+" 行密钥不能为空："+email)
			continue
		}
		inputs = append(inputs, store.ICloudAccountInput{
			Email: email,
			Key:   key,
			Group: store.DefaultICloudGroupName,
		})
	}
	return inputs, parseErrors
}

func applyICloudImportGroup(inputs []store.ICloudAccountInput, group string) {
	group = strings.TrimSpace(group)
	if group == "" {
		group = store.DefaultICloudGroupName
	}
	for index := range inputs {
		inputs[index].Group = group
	}
}
