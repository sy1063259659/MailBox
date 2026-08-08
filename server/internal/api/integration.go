package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"gptbox-server/internal/store"
)

type integrationAPI struct {
	store *store.Store
	hme   *iCloudHMEAPI
	sms   smsAPI
	keys  *integrationKeyState
}

type integrationKeyState struct {
	mu  sync.RWMutex
	key string
}

func newIntegrationKeyState(key string) *integrationKeyState {
	return &integrationKeyState{key: strings.TrimSpace(key)}
}

func (state *integrationKeyState) current() string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.key
}

func (state *integrationKeyState) replace(key string) {
	state.mu.Lock()
	state.key = strings.TrimSpace(key)
	state.mu.Unlock()
}

func (api integrationAPI) acquireSMS(w http.ResponseWriter, r *http.Request) {
	var request struct {
		EmailLeaseID string `json:"emailLeaseId"`
		OwnerID      string `json:"ownerId"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	item, err := api.store.AcquireIntegrationSMS(r.Context(), request.EmailLeaseID, request.OwnerID)
	if err != nil {
		if errors.Is(err, store.ErrIntegrationLeaseLost) {
			WriteError(w, http.StatusConflict, "lease_lost", "邮箱租约已释放或过期")
			return
		}
		if errors.Is(err, store.ErrIntegrationResourceUnavailable) {
			WriteError(w, http.StatusConflict, "sms_unavailable", err.Error())
			return
		}
		WriteError(w, http.StatusBadRequest, "sms_acquire_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "sms": item})
}

func (api integrationAPI) latestSMS(w http.ResponseWriter, r *http.Request) {
	leaseID := strings.TrimSpace(r.URL.Query().Get("leaseId"))
	receiveURL, err := api.store.GetIntegrationSMSReceiveURL(r.Context(), leaseID)
	if err != nil {
		if errors.Is(err, store.ErrIntegrationLeaseLost) {
			WriteError(w, http.StatusConflict, "lease_lost", "接码租约已释放或过期")
			return
		}
		WriteError(w, http.StatusBadRequest, "sms_unavailable", err.Error())
		return
	}
	message, err := api.sms.fetcher.Fetch(r.Context(), receiveURL)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "sms_fetch_failed", "接码接口暂时不可用")
		return
	}
	code := extractSMSCode(message)
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message, "code": code, "available": code != "", "checkedAt": time.Now()})
}

func integrationAuthRequired(keys *integrationKeyState, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := ""
		if keys != nil {
			key = keys.current()
		}
		if strings.TrimSpace(key) == "" {
			WriteError(w, http.StatusServiceUnavailable, "integration_disabled", "集成 API 尚未配置")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if len(provided) != len(key) || subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
			WriteError(w, http.StatusUnauthorized, "integration_unauthorized", "集成 API Key 无效")
			return
		}
		handler(w, r.WithContext(context.WithValue(r.Context(), requestActorKey{}, "integration")))
	}
}

func (api integrationAPI) integrationAPIKeySettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "apiKey": api.keys.current()})
	case http.MethodPost:
		if api.store == nil {
			WriteError(w, http.StatusServiceUnavailable, "settings_unavailable", "设置存储不可用")
			return
		}
		key, err := api.store.ResetIntegrationAPIKey(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "integration_key_reset_failed", "重置 Integration API Key 失败")
			return
		}
		api.keys.replace(key)
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "apiKey": key})
	default:
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (api integrationAPI) groups(w http.ResponseWriter, r *http.Request) {
	groups, err := api.store.ListIntegrationGroups(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "integration_groups_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "groups": groups})
}

func (api integrationAPI) acquire(w http.ResponseWriter, r *http.Request) {
	var request struct {
		QueueID string `json:"queueId"`
		GroupID int64  `json:"groupId"`
		Count   int    `json:"count"`
		Mode    string `json:"mode"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	items, err := api.store.AcquireIntegrationResources(r.Context(), store.AcquireIntegrationInput{QueueID: request.QueueID, GroupID: request.GroupID, Count: request.Count, Mode: request.Mode})
	if err != nil {
		if errors.Is(err, store.ErrIntegrationResourceUnavailable) {
			WriteError(w, http.StatusConflict, "resource_unavailable", err.Error())
			return
		}
		WriteError(w, http.StatusBadRequest, "resource_acquire_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (api integrationAPI) queueLease(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/integration/v1/queues/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "队列租约路径错误")
		return
	}
	queueID, action := parts[0], parts[1]
	if action == "release" {
		if r.Method != http.MethodDelete {
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if err := api.store.ReleaseIntegrationQueue(r.Context(), queueID); err != nil {
			WriteError(w, http.StatusInternalServerError, "lease_release_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	expires, err := api.store.UpdateIntegrationQueueLease(r.Context(), queueID, action)
	if err != nil {
		if errors.Is(err, store.ErrIntegrationLeaseLost) {
			WriteError(w, http.StatusConflict, "lease_lost", "资源租约已释放或过期")
			return
		}
		WriteError(w, http.StatusBadRequest, "lease_update_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "expiresAt": expires})
}

func (api integrationAPI) latestMail(w http.ResponseWriter, r *http.Request) {
	leaseID := strings.TrimSpace(r.URL.Query().Get("leaseId"))
	credentials, err := api.store.GetIntegrationMailCredentials(r.Context(), leaseID)
	if err != nil {
		if errors.Is(err, store.ErrIntegrationLeaseLost) {
			WriteError(w, http.StatusConflict, "lease_lost", "邮箱租约已释放或过期")
			return
		}
		writeICloudHMEMailError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := api.hme.mailClient.ListMessageDetailsByRecipient(ctx, credentials.ICloudEmail, credentials.AppPassword, credentials.AliasEmail, 1, "")
	if err != nil {
		writeICloudHMEPublicMailServiceError(w, err)
		return
	}
	if len(result.Messages) == 0 {
		WriteError(w, http.StatusNotFound, "mail_not_found", "暂未收到邮件")
		return
	}
	WriteJSON(w, http.StatusOK, publicLatestMessage(credentials.AliasEmail, result.Messages[0]))
}

func (api integrationAPI) cardCredentials(w http.ResponseWriter, r *http.Request) {
	item, err := api.store.GetIntegrationCardCredentials(r.Context(), strings.TrimSpace(r.URL.Query().Get("leaseId")))
	if err != nil {
		if errors.Is(err, store.ErrIntegrationLeaseLost) {
			WriteError(w, http.StatusConflict, "lease_lost", "支付卡租约已释放或过期")
			return
		}
		WriteError(w, http.StatusBadRequest, "card_unavailable", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "card": item})
}

func (api integrationAPI) authResult(w http.ResponseWriter, r *http.Request) {
	var request struct {
		LeaseID     string `json:"leaseId"`
		AccessToken string `json:"accessToken"`
		AuthFlow    string `json:"authFlow"`
		CodexAuth   string `json:"codexAuth"`
		Sub2API     string `json:"sub2api"`
		Status      string `json:"status"`
		Error       string `json:"error"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := api.store.SaveIntegrationAuthResult(r.Context(), request.LeaseID, request.AccessToken, request.AuthFlow, request.CodexAuth, request.Sub2API, request.Status, request.Error); err != nil {
		if errors.Is(err, store.ErrIntegrationLeaseLost) {
			WriteError(w, http.StatusConflict, "lease_lost", "邮箱租约已释放或过期")
			return
		}
		WriteError(w, http.StatusBadRequest, "result_save_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api integrationAPI) paymentResult(w http.ResponseWriter, r *http.Request) {
	var request struct {
		QueueID     string   `json:"queueId"`
		Emails      []string `json:"emails"`
		CardLeaseID string   `json:"cardLeaseId"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := api.store.CompleteIntegrationPayment(r.Context(), request.QueueID, request.Emails, request.CardLeaseID); err != nil {
		if errors.Is(err, store.ErrIntegrationLeaseLost) {
			WriteError(w, http.StatusConflict, "lease_lost", "邮箱或卡租约已释放或过期")
			return
		}
		WriteError(w, http.StatusBadRequest, "payment_result_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api integrationAPI) forceRelease(w http.ResponseWriter, r *http.Request) {
	var request struct {
		LeaseID string `json:"leaseId"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := api.store.ForceReleaseIntegrationLease(r.Context(), request.LeaseID); err != nil {
		WriteError(w, http.StatusNotFound, "lease_not_found", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api integrationAPI) activeLeases(w http.ResponseWriter, r *http.Request) {
	items, err := api.store.ListIntegrationLeases(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "lease_list_failed", "加载任务占用失败")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "leases": items})
}
