package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"mailbox-server/internal/codexauth"
	"mailbox-server/internal/codexquota"
	"mailbox-server/internal/store"
)

type gptAccountAPI struct {
	store      *store.Store
	authClient codexauth.Client
	quota      codexquota.Client
	oauthMu    sync.Mutex
	oauthState map[string]codexauth.OAuthState
}

type gptAccountsResponse struct {
	OK       bool               `json:"ok"`
	Accounts []store.GPTAccount `json:"accounts"`
}

type gptAccountResponse struct {
	OK      bool             `json:"ok"`
	Account store.GPTAccount `json:"account"`
}

type importGPTTokenRequest struct {
	MailAccountEmail string `json:"mailAccountEmail"`
	TokenJSON        string `json:"tokenJson"`
}

type oauthStartRequest struct {
	MailAccountEmail string `json:"mailAccountEmail"`
}

type oauthStartResponse struct {
	OK      bool   `json:"ok"`
	LoginID string `json:"loginId"`
	AuthURL string `json:"authUrl"`
}

type oauthCompleteRequest struct {
	MailAccountEmail string `json:"mailAccountEmail"`
	LoginID          string `json:"loginId"`
	CallbackURL      string `json:"callbackUrl"`
}

func newGPTAccountAPI(store *store.Store) *gptAccountAPI {
	return &gptAccountAPI{
		store:      store,
		authClient: codexauth.Client{},
		quota:      codexquota.Client{},
		oauthState: map[string]codexauth.OAuthState{},
	}
}

func (api *gptAccountAPI) list(w http.ResponseWriter, r *http.Request) {
	accounts, err := api.store.ListGPTAccounts(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, gptAccountsResponse{OK: true, Accounts: accounts})
}

func (api *gptAccountAPI) importToken(w http.ResponseWriter, r *http.Request) {
	var req importGPTTokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tokens, err := codexauth.ParseTokenJSON(req.TokenJSON)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	account, err := api.upsertTokens(r.Context(), req.MailAccountEmail, tokens)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, gptAccountResponse{OK: true, Account: account})
}

func (api *gptAccountAPI) oauthStart(w http.ResponseWriter, r *http.Request) {
	var req oauthStartRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.MailAccountEmail) == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "mailAccountEmail is required")
		return
	}
	state, err := codexauth.StartOAuth()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	api.oauthMu.Lock()
	api.oauthState[state.LoginID] = state
	api.oauthMu.Unlock()
	WriteJSON(w, http.StatusOK, oauthStartResponse{OK: true, LoginID: state.LoginID, AuthURL: state.AuthURL})
}

func (api *gptAccountAPI) oauthComplete(w http.ResponseWriter, r *http.Request) {
	var req oauthCompleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	api.oauthMu.Lock()
	state, ok := api.oauthState[req.LoginID]
	if ok {
		delete(api.oauthState, req.LoginID)
	}
	api.oauthMu.Unlock()
	if !ok {
		WriteError(w, http.StatusBadRequest, "bad_request", "OAuth 登录会话不存在或已过期")
		return
	}
	if state.ExpiresAt <= time.Now().Unix() {
		WriteError(w, http.StatusBadRequest, "bad_request", "OAuth 登录已过期，请重新发起授权")
		return
	}
	code, err := codexauth.ParseCallbackURL(req.CallbackURL, state.State)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	tokens, err := api.authClient.ExchangeCode(r.Context(), code, state.RedirectURI, state.CodeVerifier)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	account, err := api.upsertTokens(r.Context(), req.MailAccountEmail, tokens)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, gptAccountResponse{OK: true, Account: account})
}

func (api *gptAccountAPI) refresh(w http.ResponseWriter, r *http.Request, mailAccountEmail string) {
	account, err := api.refreshAccount(r.Context(), mailAccountEmail)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, gptAccountResponse{OK: true, Account: account})
}

func (api *gptAccountAPI) refreshAll(w http.ResponseWriter, r *http.Request) {
	accounts, err := api.store.ListGPTAccounts(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	refreshed := make([]store.GPTAccount, 0, len(accounts))
	for _, account := range accounts {
		next, err := api.refreshAccount(r.Context(), account.MailAccountEmail)
		if err != nil {
			if current, currentErr := api.store.GetGPTAccountCredentials(r.Context(), account.MailAccountEmail); currentErr == nil {
				refreshed = append(refreshed, current.Account)
			}
			continue
		}
		refreshed = append(refreshed, next)
	}
	WriteJSON(w, http.StatusOK, gptAccountsResponse{OK: true, Accounts: refreshed})
}

func (api *gptAccountAPI) delete(w http.ResponseWriter, r *http.Request, mailAccountEmail string) {
	if err := api.store.DeleteGPTAccount(r.Context(), mailAccountEmail); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (api *gptAccountAPI) upsertTokens(ctx context.Context, mailAccountEmail string, tokens codexauth.Tokens) (store.GPTAccount, error) {
	profile := codexauth.ParseTokenProfile(tokens)
	if profile.Email == "" {
		return store.GPTAccount{}, errors.New("Token JSON 无法解析 GPT 邮箱")
	}
	input := store.GPTAccountInput{
		MailAccountEmail:        mailAccountEmail,
		GPTEmail:                profile.Email,
		AccountID:               firstNonEmpty(profile.AccountID, tokens.AccountID),
		OrganizationID:          profile.OrganizationID,
		PlanType:                profile.PlanType,
		AuthFilePlanType:        profile.AuthFilePlanType,
		SubscriptionActiveUntil: profile.SubscriptionActiveUntil,
		Tokens: store.GPTTokens{
			IDToken:      tokens.IDToken,
			AccessToken:  tokens.AccessToken,
			RefreshToken: tokens.RefreshToken,
		},
		TokenExpiresAt: codexauth.AccessTokenExpiresAt(tokens.AccessToken),
	}
	return api.store.UpsertGPTAccount(ctx, input)
}

func (api *gptAccountAPI) refreshAccount(ctx context.Context, mailAccountEmail string) (store.GPTAccount, error) {
	credentials, err := api.store.GetGPTAccountCredentials(ctx, mailAccountEmail)
	if err != nil {
		return store.GPTAccount{}, err
	}
	tokens := credentials.Tokens
	if codexauth.AccessTokenNeedsRefresh(tokens.AccessToken, time.Now().UTC()) {
		refreshed, err := api.authClient.Refresh(ctx, tokens.RefreshToken, tokens.IDToken)
		if err != nil {
			reason := err.Error()
			if codexauth.IsReauthError(reason) {
				return api.store.MarkGPTAccountReauthRequired(ctx, mailAccountEmail, reason, "invalid_grant")
			}
			return store.GPTAccount{}, err
		}
		tokens = store.GPTTokens{
			IDToken:      refreshed.IDToken,
			AccessToken:  refreshed.AccessToken,
			RefreshToken: refreshed.RefreshToken,
		}
		if err := api.store.UpdateGPTTokens(ctx, mailAccountEmail, tokens, codexauth.AccessTokenExpiresAt(tokens.AccessToken)); err != nil {
			return store.GPTAccount{}, err
		}
	}
	profile := codexauth.ParseTokenProfile(codexauth.Tokens{
		IDToken:      tokens.IDToken,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
	accountID := firstNonEmpty(credentials.Account.AccountID, profile.AccountID)
	usage, usageErr := api.quota.FetchUsage(ctx, tokens.AccessToken, accountID)
	if usageErr != nil {
		return api.updateGPTError(ctx, mailAccountEmail, usageErr)
	}
	remoteProfile, _ := api.quota.FetchProfile(ctx, tokens.AccessToken, firstNonEmpty(accountID, profile.AccountID), profile.OrganizationID)
	status := "active"
	statusReason := ""
	if profile.SubscriptionActiveUntil != nil && profile.SubscriptionActiveUntil.Before(time.Now()) {
		status = "expired"
		statusReason = "订阅已过期"
	}
	if isZeroPercentage(usage.Hourly.Percentage) || isZeroPercentage(usage.Weekly.Percentage) {
		status = "quota_limited"
		statusReason = "额度不足"
	}
	now := time.Now().UTC()
	return api.store.UpdateGPTQuota(ctx, mailAccountEmail, store.GPTQuotaUpdate{
		AccountID:           firstNonEmpty(remoteProfile.AccountID, accountID, profile.AccountID),
		OrganizationID:      firstNonEmpty(remoteProfile.OrganizationID, profile.OrganizationID),
		AccountName:         remoteProfile.AccountName,
		AccountStructure:    remoteProfile.AccountStructure,
		PlanType:            firstNonEmpty(usage.PlanType, profile.PlanType),
		AuthFilePlanType:    profile.AuthFilePlanType,
		HourlyPercentage:    usage.Hourly.Percentage,
		HourlyResetTime:     usage.Hourly.ResetTime,
		HourlyWindowMinutes: usage.Hourly.WindowMinutes,
		HourlyWindowPresent: &usage.Hourly.Present,
		WeeklyPercentage:    usage.Weekly.Percentage,
		WeeklyResetTime:     usage.Weekly.ResetTime,
		WeeklyWindowMinutes: usage.Weekly.WindowMinutes,
		WeeklyWindowPresent: &usage.Weekly.Present,
		QuotaRawJSON:        usage.RawJSON,
		Status:              status,
		StatusReason:        statusReason,
		LastRefreshAt:       &now,
	})
}

func (api *gptAccountAPI) updateGPTError(ctx context.Context, mailAccountEmail string, source error) (store.GPTAccount, error) {
	now := time.Now().UTC()
	status := "error"
	reason := source.Error()
	code := ""
	var serviceErr codexquota.ServiceError
	if errors.As(source, &serviceErr) {
		code = serviceErr.Code
		if serviceErr.StatusCode == http.StatusUnauthorized || codexauth.IsReauthError(reason) {
			status = "reauth_required"
		} else if codexquota.IsBannedOrDisabled(reason, serviceErr.StatusCode) {
			status = "banned_or_disabled"
		} else if codexquota.IsQuotaLimited(reason) {
			status = "quota_limited"
		}
	}
	if status == "reauth_required" {
		return api.store.MarkGPTAccountReauthRequired(ctx, mailAccountEmail, reason, firstNonEmpty(code, "unauthorized"))
	}
	return api.store.UpdateGPTQuota(ctx, mailAccountEmail, store.GPTQuotaUpdate{
		Status:            status,
		StatusReason:      reason,
		QuotaErrorCode:    code,
		QuotaErrorMessage: reason,
		QuotaErrorAt:      &now,
		LastRefreshAt:     &now,
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isZeroPercentage(value *int) bool {
	return value != nil && *value <= 0
}
