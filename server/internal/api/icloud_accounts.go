package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gptbox-server/internal/store"
)

const (
	iCloudMailBaseURL     = "https://icloud.52moyu.net"
	iCloudMailListPath    = "/web/query-mails"
	iCloudMailDetailPath  = "/web/query-mail-detail"
	iCloudMailListLimit   = 20
	iCloudMailMaxBodySize = 2 << 20
)

var iCloudEmailPattern = regexp.MustCompile(`(?i)^[^\s@]+@icloud\.com$`)

type iCloudLatestFetcher interface {
	Messages(context.Context, string, string) (iCloudMailListResponse, error)
	Message(context.Context, string, string, int64) (iCloudMailDetailResponse, error)
	Latest(context.Context, string, string) (iCloudLatestMailResponse, error)
}

type iCloudAPI struct {
	store       *store.Store
	latestFetch iCloudLatestFetcher
	gptScanMu   sync.Mutex
}

func newICloudAPI(database *store.Store) *iCloudAPI {
	api := &iCloudAPI{store: database, latestFetch: newICloudLatestClient()}
	if database != nil {
		api.startGPTStatusWorker()
	}
	return api
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

type iCloudMailDetailRequest struct {
	Email string `json:"email"`
	ID    int64  `json:"id"`
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
	HTML             string `json:"html,omitempty"`
	ReceivedAt       string `json:"received_at"`
	CreatedAt        string `json:"created_at"`
	VerificationCode string `json:"verification_code"`
	MailType         string `json:"mail_type"`
	InviteLink       string `json:"invite_link"`
	ProcessStatus    string `json:"process_status"`
}

type iCloudMailSummary struct {
	ID               int64  `json:"id"`
	To               string `json:"to"`
	From             string `json:"from"`
	Subject          string `json:"subject"`
	ReceivedAt       string `json:"received_at"`
	VerificationCode string `json:"verification_code"`
	MailType         string `json:"mail_type"`
	InviteLink       string `json:"invite_link"`
}

type iCloudMailListResponse struct {
	OK      bool                `json:"ok"`
	Emails  []iCloudMailSummary `json:"emails"`
	Total   int                 `json:"total"`
	Error   string              `json:"error,omitempty"`
	Message string              `json:"message,omitempty"`
}

type iCloudMailDetailResponse struct {
	OK      bool               `json:"ok"`
	Email   *iCloudLatestEmail `json:"email,omitempty"`
	Error   string             `json:"error,omitempty"`
	Message string             `json:"message,omitempty"`
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
		baseURL: iCloudMailBaseURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (client *iCloudLatestClient) Messages(ctx context.Context, email string, key string) (iCloudMailListResponse, error) {
	var payload iCloudMailListResponse
	err := client.postJSON(ctx, iCloudMailListPath, map[string]string{
		"credential": email + "----" + key,
	}, key, &payload)
	if err != nil {
		return iCloudMailListResponse{}, err
	}
	if len(payload.Emails) > iCloudMailListLimit {
		payload.Emails = payload.Emails[:iCloudMailListLimit]
	}
	return payload, nil
}

func (client *iCloudLatestClient) Message(ctx context.Context, email string, key string, id int64) (iCloudMailDetailResponse, error) {
	var upstream struct {
		OK    bool `json:"ok"`
		Email *struct {
			iCloudLatestEmail
			Body string `json:"body"`
		} `json:"email"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	err := client.postJSON(ctx, iCloudMailDetailPath, map[string]any{
		"address": email,
		"key":     key,
		"id":      id,
	}, key, &upstream)
	if err != nil {
		return iCloudMailDetailResponse{}, err
	}
	payload := iCloudMailDetailResponse{OK: upstream.OK, Error: upstream.Error, Message: upstream.Message}
	if upstream.Email != nil {
		detail := upstream.Email.iCloudLatestEmail
		if strings.TrimSpace(detail.Text) == "" {
			detail.Text = upstream.Email.Body
		}
		payload.Email = &detail
	}
	return payload, nil
}

func (client *iCloudLatestClient) Latest(ctx context.Context, email string, key string) (iCloudLatestMailResponse, error) {
	list, err := client.Messages(ctx, email, key)
	if err != nil {
		return iCloudLatestMailResponse{}, err
	}
	if len(list.Emails) == 0 {
		return iCloudLatestMailResponse{OK: true}, nil
	}
	detail, err := client.Message(ctx, email, key, list.Emails[0].ID)
	if err != nil {
		return iCloudLatestMailResponse{}, err
	}
	return iCloudLatestMailResponse{OK: true, Email: detail.Email}, nil
}

func (client *iCloudLatestClient) postJSON(ctx context.Context, path string, body any, key string, target any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return errors.New("创建 iCloud 收件请求失败")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(client.baseURL, "/")+path, bytes.NewReader(encoded))
	if err != nil {
		return errors.New("创建 iCloud 收件请求失败")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return errors.New("iCloud 收件接口请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var failure struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure)
		if failure.Error != "" || failure.Message != "" {
			return iCloudMailFailure(failure.Error, failure.Message, key)
		}
		return fmt.Errorf("iCloud 收件接口返回 HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, iCloudMailMaxBodySize)).Decode(target); err != nil {
		return errors.New("iCloud 收件接口返回格式异常")
	}

	var result struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	resultBytes, _ := json.Marshal(target)
	_ = json.Unmarshal(resultBytes, &result)
	if result.OK {
		return nil
	}
	return iCloudMailFailure(result.Error, result.Message, key)
}

func iCloudMailFailure(errorCode string, message string, key string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		switch strings.TrimSpace(errorCode) {
		case "invalid_credential":
			message = "iCloud 邮箱或密钥无效"
		case "mailbox_inactive":
			message = "该 iCloud 邮箱已停用"
		case "mail_not_found":
			message = "邮件不存在或已被删除"
		default:
			message = strings.TrimSpace(errorCode)
		}
	}
	if message == "" {
		message = "iCloud 收件接口返回失败"
	}
	if key != "" {
		message = strings.ReplaceAll(message, key, "[redacted]")
	}
	return errors.New(message)
}

func (api *iCloudAPI) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := api.store.ListICloudAccounts(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, iCloudAccountsResponse{OK: true, Accounts: accounts})
}

func (api *iCloudAPI) importAccounts(w http.ResponseWriter, r *http.Request) {
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

func (api *iCloudAPI) latestMail(w http.ResponseWriter, r *http.Request) {
	var req latestICloudMailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	credentials, ok := api.iCloudCredentials(w, r, req.Email)
	if !ok {
		return
	}
	payload, err := api.latestFetch.Latest(r.Context(), credentials.Email, credentials.Key)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "icloud_mail_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, payload)
}

func (api *iCloudAPI) listMail(w http.ResponseWriter, r *http.Request) {
	var req latestICloudMailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	credentials, ok := api.iCloudCredentials(w, r, req.Email)
	if !ok {
		return
	}
	payload, err := api.latestFetch.Messages(r.Context(), credentials.Email, credentials.Key)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "icloud_mail_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, payload)
}

func (api *iCloudAPI) mailDetail(w http.ResponseWriter, r *http.Request) {
	var req iCloudMailDetailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ID <= 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "邮件 ID 无效")
		return
	}
	credentials, ok := api.iCloudCredentials(w, r, req.Email)
	if !ok {
		return
	}
	payload, err := api.latestFetch.Message(r.Context(), credentials.Email, credentials.Key, req.ID)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "icloud_mail_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, payload)
}

func (api *iCloudAPI) iCloudCredentials(w http.ResponseWriter, r *http.Request, rawEmail string) (store.ICloudCredentials, bool) {
	email := strings.ToLower(strings.TrimSpace(rawEmail))
	if !iCloudEmailPattern.MatchString(email) {
		WriteError(w, http.StatusBadRequest, "bad_request", "只支持 @icloud.com 邮箱")
		return store.ICloudCredentials{}, false
	}
	credentials, err := api.store.GetICloudCredentials(r.Context(), email)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return store.ICloudCredentials{}, false
	}
	return credentials, true
}

func (api *iCloudAPI) updateRemark(w http.ResponseWriter, r *http.Request) {
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

func (api *iCloudAPI) moveAccounts(w http.ResponseWriter, r *http.Request) {
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

func (api *iCloudAPI) deleteAccount(w http.ResponseWriter, r *http.Request) {
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

func (api *iCloudAPI) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := api.store.ListICloudGroups(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, iCloudGroupsResponse{OK: true, Groups: groups})
}

func (api *iCloudAPI) createGroup(w http.ResponseWriter, r *http.Request) {
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

func (api *iCloudAPI) reorderGroups(w http.ResponseWriter, r *http.Request) {
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

func (api *iCloudAPI) updateGroup(w http.ResponseWriter, r *http.Request) {
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

func (api *iCloudAPI) deleteGroup(w http.ResponseWriter, r *http.Request) {
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
