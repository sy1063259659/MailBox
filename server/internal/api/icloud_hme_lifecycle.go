package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"gptbox-server/internal/store"
)

type iCloudHMELifecycleRequest struct {
	Emails []string
	Action string
}

type iCloudHMEDeleteAppleRequest struct {
	ConfirmEmail string
}

func (api *iCloudHMEAPI) validateAllSources(w http.ResponseWriter, r *http.Request) {
	sources, err := api.store.ListICloudHMESourceAccounts(r.Context())
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	results := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		item := map[string]any{"id": source.ID, "ok": false}
		if !source.CookieConfigured {
			item["message"] = "未配置 Cookie"
			results = append(results, item)
			continue
		}
		client, _, err := api.clientForSource(r.Context(), source.ID)
		if err == nil {
			err = client.ValidateSession()
		}
		if err != nil {
			api.markSourceError(r.Context(), source.ID, err)
			item["message"] = friendlyICloudHMEError(err)
		} else {
			_ = api.store.SaveICloudHMECookies(r.Context(), source.ID, client.GetCookies(), "active", "")
			item["ok"] = true
		}
		results = append(results, item)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

func (api *iCloudHMEAPI) syncAllSources(w http.ResponseWriter, r *http.Request) {
	sources, err := api.store.ListICloudHMESourceAccounts(r.Context())
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	results := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		item := map[string]any{"id": source.ID, "imported": 0, "updated": 0}
		if !source.CookieConfigured {
			item["error"] = "未配置 Cookie"
			results = append(results, item)
			continue
		}
		imported, updated, err := api.syncSource(r.Context(), source.ID, store.DefaultICloudHMEGroupName)
		item["imported"] = imported
		item["updated"] = updated
		if err != nil {
			item["error"] = friendlyICloudHMEError(err)
		}
		results = append(results, item)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

func (api *iCloudHMEAPI) lifecycleAliases(w http.ResponseWriter, r *http.Request) {
	var req iCloudHMELifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "deactivate" && action != "reactivate" {
		WriteError(w, http.StatusBadRequest, "bad_request", "生命周期操作只支持 deactivate 或 reactivate")
		return
	}
	emails := uniqueICloudHMEEmails(req.Emails)
	if len(emails) == 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "请选择隐藏邮箱")
		return
	}
	if len(emails) > 100 {
		WriteError(w, http.StatusBadRequest, "bad_request", "单次最多操作 100 个隐藏邮箱")
		return
	}
	results := make([]map[string]any, 0, len(emails))
	for _, email := range emails {
		item := map[string]any{"email": email, "ok": false}
		if err := api.lifecycleAlias(r.Context(), requestActor(r), email, action); err != nil {
			item["error"] = friendlyICloudHMEError(err)
		} else {
			item["ok"] = true
		}
		results = append(results, item)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

func (api *iCloudHMEAPI) lifecycleAlias(ctx context.Context, actor, email, action string) error {
	alias, err := api.store.GetICloudHMEAlias(ctx, email)
	if err != nil {
		return err
	}
	if alias.AnonymousID == "" {
		return errors.New("隐藏邮箱缺少 Apple anonymousId，请先同步")
	}
	lock := api.lockForSource(alias.SourceAccountID)
	lock.Lock()
	defer lock.Unlock()

	client, _, err := api.clientForSource(ctx, alias.SourceAccountID)
	if err == nil {
		var success bool
		if action == "deactivate" {
			success, err = client.DeactivateHME(alias.AnonymousID)
		} else {
			success, err = client.ReactivateHME(alias.AnonymousID)
		}
		if err == nil && !success {
			err = errors.New("Apple 未确认生命周期操作")
		}
	}
	if err != nil {
		api.markSourceError(ctx, alias.SourceAccountID, err)
		_ = api.auditLifecycle(ctx, actor, action, alias.Email, "failed", classifyICloudHMECode(err), err.Error())
		return err
	}
	status := "inactive"
	if action == "reactivate" {
		status = "active"
	}
	if err := api.store.UpdateICloudHMEAliasLifecycle(ctx, alias.Email, status); err != nil {
		return err
	}
	_ = api.auditLifecycle(ctx, actor, action, alias.Email, "success", "", "")
	return nil
}

func (api *iCloudHMEAPI) deleteAppleAlias(w http.ResponseWriter, r *http.Request, email string) {
	var req iCloudHMEDeleteAppleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if normalizeICloudHMEEmail(req.ConfirmEmail) != normalizeICloudHMEEmail(email) {
		WriteError(w, http.StatusBadRequest, "confirmation_mismatch", "必须输入完整隐藏邮箱进行确认")
		return
	}
	alias, err := api.store.GetICloudHMEAlias(r.Context(), email)
	if err != nil {
		writeICloudHMEError(w, err)
		return
	}
	if alias.AppleStatus != "deleted" && alias.AnonymousID == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "隐藏邮箱缺少 Apple anonymousId，请先同步")
		return
	}
	if alias.AppleStatus != "deleted" {
		lock := api.lockForSource(alias.SourceAccountID)
		lock.Lock()
		defer lock.Unlock()
		client, _, err := api.clientForSource(r.Context(), alias.SourceAccountID)
		if err == nil {
			err = client.Delete(alias.AnonymousID)
		}
		if err != nil {
			api.markSourceError(r.Context(), alias.SourceAccountID, err)
			_ = api.auditLifecycle(r.Context(), requestActor(r), "delete_alias", alias.Email, "failed", classifyICloudHMECode(err), err.Error())
			writeICloudHMEError(w, err)
			return
		}
		if err := api.store.UpdateICloudHMEAliasLifecycle(r.Context(), alias.Email, "deleted"); err != nil {
			writeICloudHMEError(w, err)
			return
		}
	}
	if err := api.store.DeleteICloudHMEAlias(r.Context(), alias.Email); err != nil {
		_ = api.auditLifecycle(r.Context(), requestActor(r), "delete_alias", alias.Email, "failed", "local_delete_failed", err.Error())
		writeICloudHMEError(w, err)
		return
	}
	_ = api.auditLifecycle(r.Context(), requestActor(r), "delete_alias", alias.Email, "success", "", "")
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *iCloudHMEAPI) auditLifecycle(ctx context.Context, actor, action, target, result, code, message string) error {
	return api.store.AddICloudHMEAudit(ctx, store.ICloudHMEAuditLog{
		Actor: actor, Action: action, TargetType: "alias", Target: target,
		Result: result, ErrorCode: code, Message: message,
	})
}

func uniqueICloudHMEEmails(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeICloudHMEEmail(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeICloudHMEEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
