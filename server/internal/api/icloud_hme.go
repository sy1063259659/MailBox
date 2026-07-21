package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gptbox-server/internal/icloudhme"
	"gptbox-server/internal/imapmail"
	"gptbox-server/internal/store"
)

type iCloudHMEClient interface {
	ValidateSession() error
	AccountInfo() *icloudhme.AccountInfo
	ListAliases() ([]icloudhme.Alias, error)
	CreateAlias(string, int) (*icloudhme.CreateResult, error)
	Login(string, string, icloudhme.OTPProvider) error
	GetCookies() map[string]string
}

type iCloudHMEClientFactory func(map[string]string, string) (iCloudHMEClient, error)

type iCloudHMEAPI struct {
	store       *store.Store
	newClient   iCloudHMEClientFactory
	mailClient  imapmail.Client
	createLocks sync.Map
}

func newICloudHMEAPI(database *store.Store) *iCloudHMEAPI {
	return &iCloudHMEAPI{
		store: database,
		newClient: func(cookies map[string]string, host string) (iCloudHMEClient, error) {
			return icloudhme.NewClient(cookies, host, "", false)
		},
	}
}

type createICloudHMESourceRequest struct {
	Name         string `json:"name"`
	AppleIDEmail string `json:"appleIdEmail"`
	ICloudEmail  string `json:"icloudEmail"`
	Host         string `json:"host"`
}

type iCloudHMECookiesRequest struct {
	Cookies string `json:"cookies"`
}
type iCloudHMELoginRequest struct {
	Password string `json:"password"`
	OTP      string `json:"otp"`
}
type iCloudHMEAppPasswordRequest struct {
	AppPassword string `json:"appPassword"`
}
type createICloudHMEAliasRequest struct {
	Label string `json:"label"`
	Group string `json:"group"`
}
type iCloudHMERemarkRequest struct {
	Email  string `json:"email"`
	Remark string `json:"remark"`
}
type iCloudHMEMoveRequest struct {
	Emails []string `json:"emails"`
	Group  string   `json:"group"`
}
type iCloudHMEMailRequest struct {
	Email string `json:"email"`
}

func (api *iCloudHMEAPI) listSourceAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := api.store.ListICloudHMESourceAccounts(r.Context())
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "accounts": accounts})
}

func (api *iCloudHMEAPI) createSourceAccount(w http.ResponseWriter, r *http.Request) {
	var req createICloudHMESourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	account, err := api.store.CreateICloudHMESourceAccount(r.Context(), req.Name, req.AppleIDEmail, req.ICloudEmail, req.Host)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "account": account})
}

func (api *iCloudHMEAPI) saveCookies(w http.ResponseWriter, r *http.Request, id int64) {
	var req iCloudHMECookiesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	cookies, err := parseICloudHMECookies(req.Cookies)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	credentials, err := api.store.GetICloudHMESourceCredentials(r.Context(), id)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	client, err := api.newClient(cookies, credentials.Host)
	if err != nil {
		WriteError(w, 500, "internal_error", "创建 Apple 客户端失败")
		return
	}
	if err := client.ValidateSession(); err != nil {
		api.markSourceError(r.Context(), id, err)
		writeICloudHMEError(w, err)
		return
	}
	if err := api.store.SaveICloudHMECookies(r.Context(), id, client.GetCookies(), "active", ""); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "accountInfo": client.AccountInfo()})
}

func (api *iCloudHMEAPI) login(w http.ResponseWriter, r *http.Request, id int64) {
	var req iCloudHMELoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	credentials, err := api.store.GetICloudHMESourceCredentials(r.Context(), id)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		WriteError(w, 400, "bad_request", "Apple 密码不能为空")
		return
	}
	client, err := api.newClient(nil, credentials.Host)
	if err != nil {
		WriteError(w, 500, "internal_error", "创建 Apple 客户端失败")
		return
	}
	var otpProvider icloudhme.OTPProvider
	if strings.TrimSpace(req.OTP) != "" {
		otp := strings.TrimSpace(req.OTP)
		otpProvider = func() (string, error) { return otp, nil }
	}
	if err := safeICloudHMELogin(client, credentials.AppleIDEmail, req.Password, otpProvider); err != nil {
		api.markSourceError(r.Context(), id, err)
		writeICloudHMEError(w, err)
		return
	}
	if err := client.ValidateSession(); err != nil {
		api.markSourceError(r.Context(), id, err)
		writeICloudHMEError(w, err)
		return
	}
	if err := api.store.SaveICloudHMECookies(r.Context(), id, client.GetCookies(), "active", ""); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "accountInfo": client.AccountInfo()})
}

func (api *iCloudHMEAPI) saveAppPassword(w http.ResponseWriter, r *http.Request, id int64) {
	var req iCloudHMEAppPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := api.store.SaveICloudHMEAppPassword(r.Context(), id, req.AppPassword); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (api *iCloudHMEAPI) validateSource(w http.ResponseWriter, r *http.Request, id int64) {
	client, _, err := api.clientForSource(r.Context(), id)
	if err != nil {
		writeICloudHMEError(w, err)
		return
	}
	if err := client.ValidateSession(); err != nil {
		api.markSourceError(r.Context(), id, err)
		writeICloudHMEError(w, err)
		return
	}
	if err := api.store.SaveICloudHMECookies(r.Context(), id, client.GetCookies(), "active", ""); err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "accountInfo": client.AccountInfo()})
}

func (api *iCloudHMEAPI) deleteSource(w http.ResponseWriter, r *http.Request, id int64) {
	if err := api.store.DeleteICloudHMESourceAccount(r.Context(), id); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (api *iCloudHMEAPI) listAliases(w http.ResponseWriter, r *http.Request) {
	aliases, err := api.store.ListICloudHMEAliases(r.Context())
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "aliases": aliases})
}

func (api *iCloudHMEAPI) createAlias(w http.ResponseWriter, r *http.Request, sourceID int64) {
	var req createICloudHMEAliasRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	lock := api.lockForSource(sourceID)
	if !lock.TryLock() {
		WriteError(w, 409, "icloud_alias_create_busy", "该主账号正在创建隐藏邮箱")
		return
	}
	defer lock.Unlock()

	client, _, err := api.clientForSource(r.Context(), sourceID)
	if err != nil {
		writeICloudHMEError(w, err)
		return
	}
	result, err := client.CreateAlias(strings.TrimSpace(req.Label), 3)
	if err != nil {
		api.markSourceError(r.Context(), sourceID, err)
		writeICloudHMEError(w, err)
		return
	}
	createdAt, _ := time.Parse(time.RFC3339, result.CreatedAt)
	imported, _, err := api.store.SyncICloudHMEAliases(r.Context(), sourceID, []store.ICloudHMEAliasInput{{
		Email: result.Email, Label: result.Label, Active: true, CreatedAt: createdAt,
	}}, req.Group)
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	if err := api.store.SaveICloudHMECookies(r.Context(), sourceID, client.GetCookies(), "active", ""); err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "email": result.Email, "imported": imported})
}

func (api *iCloudHMEAPI) syncAliases(w http.ResponseWriter, r *http.Request, sourceID int64) {
	client, _, err := api.clientForSource(r.Context(), sourceID)
	if err != nil {
		writeICloudHMEError(w, err)
		return
	}
	aliases, err := client.ListAliases()
	if err != nil {
		api.markSourceError(r.Context(), sourceID, err)
		writeICloudHMEError(w, err)
		return
	}
	inputs := make([]store.ICloudHMEAliasInput, 0, len(aliases))
	for _, alias := range aliases {
		createdAt, _ := time.Parse(time.RFC3339, alias.CreatedAt)
		inputs = append(inputs, store.ICloudHMEAliasInput{Email: alias.Email, AnonymousID: alias.AnonymousID, Label: alias.Label, Active: alias.Active, CreatedAt: createdAt})
	}
	imported, updated, err := api.store.SyncICloudHMEAliases(r.Context(), sourceID, inputs, store.DefaultICloudHMEGroupName)
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	if err := api.store.SaveICloudHMECookies(r.Context(), sourceID, client.GetCookies(), "active", ""); err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "imported": imported, "updated": updated, "total": len(inputs)})
}

func (api *iCloudHMEAPI) updateAliasRemark(w http.ResponseWriter, r *http.Request) {
	var req iCloudHMERemarkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len([]rune(strings.TrimSpace(req.Remark))) > maxAccountRemarkLength {
		WriteError(w, 400, "bad_request", "备注最多 500 个字符")
		return
	}
	alias, err := api.store.UpdateICloudHMEAliasRemark(r.Context(), req.Email, req.Remark)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "alias": alias})
}

func (api *iCloudHMEAPI) moveAliases(w http.ResponseWriter, r *http.Request) {
	var req iCloudHMEMoveRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := api.store.MoveICloudHMEAliasesToGroup(r.Context(), req.Emails, req.Group); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (api *iCloudHMEAPI) deleteAlias(w http.ResponseWriter, r *http.Request, email string) {
	if err := api.store.DeleteICloudHMEAlias(r.Context(), email); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (api *iCloudHMEAPI) latestMail(w http.ResponseWriter, r *http.Request) {
	var req iCloudHMEMailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	credentials, err := api.store.GetICloudHMEMailCredentials(r.Context(), req.Email)
	if err != nil {
		code := "bad_request"
		if err.Error() == "icloud_app_password_required" {
			code = "icloud_app_password_required"
		}
		WriteError(w, 400, code, friendlyICloudHMEError(err))
		return
	}
	message, err := api.mailClient.GetLatestMessageByRecipient(r.Context(), credentials.ICloudEmail, credentials.AppPassword, credentials.AliasEmail)
	if err != nil {
		code := "icloud_mail_error"
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			code = "icloud_mail_timeout"
		}
		if strings.Contains(err.Error(), "icloud_mail_not_found") {
			code = "icloud_mail_not_found"
			status = http.StatusNotFound
		}
		WriteError(w, status, code, friendlyICloudHMEError(err))
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "email": message})
}

func (api *iCloudHMEAPI) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := api.store.ListICloudHMEGroups(r.Context())
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "groups": groups})
}
func (api *iCloudHMEAPI) createGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	group, err := api.store.CreateICloudHMEGroup(r.Context(), req.Name)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "group": group})
}
func (api *iCloudHMEAPI) reorderGroups(w http.ResponseWriter, r *http.Request) {
	var req reorderGroupsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	groups, err := api.store.ReorderICloudHMEGroups(r.Context(), req.IDs)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "groups": groups})
}
func (api *iCloudHMEAPI) updateGroup(w http.ResponseWriter, r *http.Request, id int64) {
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	group, err := api.store.RenameICloudHMEGroup(r.Context(), id, req.Name)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "group": group})
}
func (api *iCloudHMEAPI) deleteGroup(w http.ResponseWriter, r *http.Request, id int64) {
	if err := api.store.DeleteICloudHMEGroup(r.Context(), id); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (api *iCloudHMEAPI) clientForSource(ctx context.Context, id int64) (iCloudHMEClient, store.ICloudHMESourceCredentials, error) {
	credentials, err := api.store.GetICloudHMESourceCredentials(ctx, id)
	if err != nil {
		return nil, credentials, err
	}
	if strings.TrimSpace(credentials.CookiesJSON) == "" {
		return nil, credentials, errors.New("icloud_session_expired")
	}
	cookies := map[string]string{}
	if err := json.Unmarshal([]byte(credentials.CookiesJSON), &cookies); err != nil {
		return nil, credentials, errors.New("icloud_session_expired")
	}
	client, err := api.newClient(cookies, credentials.Host)
	return client, credentials, err
}

func (api *iCloudHMEAPI) markSourceError(ctx context.Context, id int64, err error) {
	code, message := classifyICloudHMEError(err)
	status := "error"
	if code == "icloud_session_expired" || code == "icloud_otp_required" {
		status = "reauth_required"
	}
	if code == "icloud_plus_required" {
		status = "icloud_plus_required"
	}
	_ = api.store.UpdateICloudHMESourceStatus(ctx, id, status, message, nil, false)
}

func (api *iCloudHMEAPI) lockForSource(id int64) *sync.Mutex {
	value, _ := api.createLocks.LoadOrStore(id, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func safeICloudHMELogin(client iCloudHMEClient, username, password string, otpProvider icloudhme.OTPProvider) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("Apple 登录协议异常: %v", recovered)
		}
	}()
	return client.Login(username, password, otpProvider)
}

func parseICloudHMECookies(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("Cookie 不能为空")
	}
	cookies := map[string]string{}
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &cookies); err != nil {
			return nil, errors.New("Cookie JSON 格式不正确")
		}
	} else {
		for _, item := range strings.Split(raw, ";") {
			parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
				continue
			}
			cookies[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), "\"")
		}
	}
	if len(cookies) == 0 {
		return nil, errors.New("未识别到有效 Cookie")
	}
	return cookies, nil
}

func classifyICloudHMEError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	message := err.Error()
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "需要提供 otp"), strings.Contains(lower, "需要 otp"), strings.Contains(lower, "双重认证"):
		return "icloud_otp_required", "Apple 账号需要双重认证验证码"
	case strings.Contains(lower, "icloud+") || strings.Contains(lower, "premiummailsettings"):
		return "icloud_plus_required", "该 Apple 账号未开通 iCloud+ 或无隐藏邮箱权限"
	case strings.Contains(lower, "用户名或密码错误"), strings.Contains(lower, "2fa 验证失败"), strings.Contains(lower, "http 401"), strings.Contains(lower, "http 403"), strings.Contains(lower, "cookie"), strings.Contains(lower, "session"):
		if strings.Contains(lower, "用户名或密码错误") {
			return "icloud_session_expired", "Apple ID 或密码错误"
		}
		return "icloud_session_expired", "Apple 会话已失效，请重新登录或导入 Cookie"
	case strings.Contains(lower, "创建别名"), strings.Contains(lower, "generate"), strings.Contains(lower, "reserve"):
		return "icloud_alias_create_failed", "隐藏邮箱创建失败：" + message
	default:
		return "icloud_alias_create_failed", message
	}
}

func writeICloudHMEError(w http.ResponseWriter, err error) {
	code, message := classifyICloudHMEError(err)
	status := http.StatusBadGateway
	if code == "icloud_otp_required" || code == "icloud_session_expired" || code == "icloud_plus_required" {
		status = http.StatusConflict
	}
	WriteError(w, status, code, message)
}

func friendlyICloudHMEError(err error) string {
	if err == nil {
		return ""
	}
	if err.Error() == "icloud_app_password_required" {
		return "请先为所属主账号配置 App 专用密码"
	}
	if strings.Contains(err.Error(), "icloud_mail_not_found") {
		return "未找到发给该隐藏邮箱的邮件"
	}
	if strings.Contains(strings.ToLower(err.Error()), "login") {
		return "iCloud IMAP 登录失败，请检查 App 专用密码"
	}
	return err.Error()
}

func parseICloudHMEID(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.Trim(raw, "/"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("id is required")
	}
	return id, nil
}

func decodePathEmail(raw string) string {
	decoded, err := url.PathUnescape(strings.Trim(raw, "/"))
	if err != nil {
		return strings.Trim(raw, "/")
	}
	return decoded
}

func (api *iCloudHMEAPI) routeSourceAccount(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/icloud-hme/source-accounts/")
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) == 0 {
		WriteError(w, 400, "bad_request", "id is required")
		return
	}
	id, err := parseICloudHMEID(parts[0])
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	action := ""
	if len(parts) > 1 {
		action = strings.Join(parts[1:], "/")
	}
	switch {
	case r.Method == http.MethodPost && action == "cookies":
		api.saveCookies(w, r, id)
	case r.Method == http.MethodPost && action == "login":
		api.login(w, r, id)
	case r.Method == http.MethodPut && action == "app-password":
		api.saveAppPassword(w, r, id)
	case r.Method == http.MethodPost && action == "validate":
		api.validateSource(w, r, id)
	case r.Method == http.MethodPost && action == "aliases":
		api.createAlias(w, r, id)
	case r.Method == http.MethodPost && action == "aliases/sync":
		api.syncAliases(w, r, id)
	case r.Method == http.MethodDelete && action == "":
		api.deleteSource(w, r, id)
	default:
		WriteError(w, 405, "method_not_allowed", "method not allowed")
	}
}

func (api *iCloudHMEAPI) routeAlias(w http.ResponseWriter, r *http.Request) {
	email := decodePathEmail(strings.TrimPrefix(r.URL.Path, "/api/icloud-hme/aliases/"))
	if r.Method != http.MethodDelete {
		WriteError(w, 405, "method_not_allowed", "method not allowed")
		return
	}
	api.deleteAlias(w, r, email)
}

func (api *iCloudHMEAPI) routeGroup(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/icloud-hme/groups/")
	if strings.Trim(raw, "/") == "order" {
		if r.Method == http.MethodPatch {
			api.reorderGroups(w, r)
		} else {
			WriteError(w, 405, "method_not_allowed", "method not allowed")
		}
		return
	}
	id, err := parseICloudHMEID(raw)
	if err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	if r.Method == http.MethodPatch {
		api.updateGroup(w, r, id)
		return
	}
	if r.Method == http.MethodDelete {
		api.deleteGroup(w, r, id)
		return
	}
	WriteError(w, 405, "method_not_allowed", "method not allowed")
}
