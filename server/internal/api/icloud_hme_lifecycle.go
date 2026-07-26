package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gptbox-server/internal/store"
)

type iCloudHMELifecycleRequest struct {
	Emails []string
	Action string
}

type iCloudHMELifecycleResult struct {
	Email string `json:"email"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// iCloudHMELifecycleTask tracks one batch deactivate/reactivate run. Tasks live
// in memory only: a run takes at most a few minutes and the per-alias outcome
// is also written to the audit log, so surviving a restart is not worth a table.
type iCloudHMELifecycleTask struct {
	mu         sync.Mutex
	id         string
	action     string
	total      int
	results    []iCloudHMELifecycleResult
	finishedAt time.Time
}

func (task *iCloudHMELifecycleTask) record(email string, err error) {
	task.mu.Lock()
	defer task.mu.Unlock()
	result := iCloudHMELifecycleResult{Email: email, OK: err == nil}
	if err != nil {
		result.Error = friendlyICloudHMEError(err)
	}
	task.results = append(task.results, result)
}

func (task *iCloudHMELifecycleTask) finish() {
	task.mu.Lock()
	defer task.mu.Unlock()
	task.finishedAt = time.Now()
}

func (task *iCloudHMELifecycleTask) snapshot() map[string]any {
	task.mu.Lock()
	defer task.mu.Unlock()
	status := "running"
	if !task.finishedAt.IsZero() {
		status = "completed"
	}
	results := make([]iCloudHMELifecycleResult, len(task.results))
	copy(results, task.results)
	return map[string]any{
		"id": task.id, "action": task.action, "status": status,
		"total": task.total, "done": len(results), "results": results,
	}
}

func (task *iCloudHMELifecycleTask) expired(now time.Time) bool {
	task.mu.Lock()
	defer task.mu.Unlock()
	return !task.finishedAt.IsZero() && now.Sub(task.finishedAt) > time.Hour
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
	task := api.newLifecycleTask(action, len(emails))
	actor := requestActor(r)
	if isAsyncLifecycleRequest(r) {
		// New clients poll the task for progress instead of holding one request
		// open while every Apple operation finishes.
		go api.runLifecycleTask(context.Background(), task, actor, action, emails)
		WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true, "taskId": task.id, "total": len(emails)})
		return
	}

	// Keep the original synchronous response contract for browser tabs that
	// were already open during a deployment. Those clients expect results and
	// would otherwise attempt to call filter on an absent response field.
	api.runLifecycleTask(r.Context(), task, actor, action, emails)
	snapshot := task.snapshot()
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "results": snapshot["results"]})
}

func isAsyncLifecycleRequest(r *http.Request) bool {
	return r.URL.Query().Get("async") == "1"
}

func (api *iCloudHMEAPI) newLifecycleTask(action string, total int) *iCloudHMELifecycleTask {
	now := time.Now()
	api.lifecycleTasks.Range(func(key, value any) bool {
		if stored, ok := value.(*iCloudHMELifecycleTask); ok && stored.expired(now) {
			api.lifecycleTasks.Delete(key)
		}
		return true
	})
	task := &iCloudHMELifecycleTask{
		id:     "lt-" + strconv.FormatInt(api.lifecycleTaskSeq.Add(1), 10),
		action: action, total: total,
	}
	api.lifecycleTasks.Store(task.id, task)
	return task
}

func (api *iCloudHMEAPI) getLifecycleTask(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/icloud-hme/lifecycle-tasks/"), "/")
	value, ok := api.lifecycleTasks.Load(id)
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "生命周期任务不存在或已过期")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "task": value.(*iCloudHMELifecycleTask).snapshot()})
}

func (api *iCloudHMEAPI) runLifecycleTask(ctx context.Context, task *iCloudHMELifecycleTask, actor, action string, emails []string) {
	defer task.finish()
	bySource := map[int64][]store.ICloudHMEAlias{}
	for _, email := range emails {
		alias, err := api.store.GetICloudHMEAlias(ctx, email)
		if err == nil && alias.AnonymousID == "" {
			err = errors.New("隐藏邮箱缺少 Apple anonymousId，请先同步")
		}
		if err != nil {
			task.record(email, err)
			_ = api.auditLifecycle(ctx, actor, action, email, "failed", classifyICloudHMECode(err), err.Error())
			continue
		}
		bySource[alias.SourceAccountID] = append(bySource[alias.SourceAccountID], alias)
	}
	// One goroutine per source account: a single tls-client and Apple session
	// handles all aliases of that source, and different sources proceed in
	// parallel. lockForSource still serializes against other Apple operations.
	var wg sync.WaitGroup
	for sourceID, aliases := range bySource {
		wg.Add(1)
		go func(sourceID int64, aliases []store.ICloudHMEAlias) {
			defer wg.Done()
			api.runLifecycleSourceBatch(ctx, task, actor, action, sourceID, aliases)
		}(sourceID, aliases)
	}
	wg.Wait()
}

func (api *iCloudHMEAPI) runLifecycleSourceBatch(
	ctx context.Context,
	task *iCloudHMELifecycleTask,
	actor, action string,
	sourceID int64,
	aliases []store.ICloudHMEAlias,
) {
	lock := api.lockForSource(sourceID)
	lock.Lock()
	defer lock.Unlock()

	failRemaining := func(from int, err error) {
		for _, alias := range aliases[from:] {
			task.record(alias.Email, err)
			_ = api.auditLifecycle(ctx, actor, action, alias.Email, "failed", classifyICloudHMECode(err), err.Error())
		}
	}
	client, _, err := api.clientForSource(ctx, sourceID)
	if err != nil {
		api.markSourceAuthError(ctx, sourceID, err)
		failRemaining(0, err)
		return
	}
	succeeded := 0
	for index, alias := range aliases {
		var success bool
		var opErr error
		if action == "deactivate" {
			success, opErr = client.DeactivateHME(alias.AnonymousID)
		} else {
			success, opErr = client.ReactivateHME(alias.AnonymousID)
		}
		if opErr == nil && !success {
			opErr = errors.New("Apple 未确认生命周期操作")
		}
		if opErr != nil {
			api.markSourceAuthError(ctx, sourceID, opErr)
			task.record(alias.Email, opErr)
			_ = api.auditLifecycle(ctx, actor, action, alias.Email, "failed", classifyICloudHMECode(opErr), opErr.Error())
			if isICloudHMEAuthError(opErr) {
				// The session is gone; hammering Apple with the rest would
				// just repeat the same failure.
				failRemaining(index+1, opErr)
				return
			}
			continue
		}
		status := "inactive"
		if action == "reactivate" {
			status = "active"
		}
		if err := api.store.UpdateICloudHMEAliasLifecycle(ctx, alias.Email, status); err != nil {
			task.record(alias.Email, err)
			continue
		}
		succeeded++
		task.record(alias.Email, nil)
		_ = api.auditLifecycle(ctx, actor, action, alias.Email, "success", "", "")
	}
	if succeeded > 0 {
		_ = api.store.SaveICloudHMECookies(ctx, sourceID, client.GetCookies(), "active", "")
	}
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
	if alias.AnonymousID == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "隐藏邮箱缺少 Apple anonymousId，请先同步")
		return
	}
	lock := api.lockForSource(alias.SourceAccountID)
	lock.Lock()
	defer lock.Unlock()
	client, _, err := api.clientForSource(r.Context(), alias.SourceAccountID)
	if err == nil {
		err = client.Delete(alias.AnonymousID)
	}
	if err != nil {
		api.markSourceAuthError(r.Context(), alias.SourceAccountID, err)
		_ = api.auditLifecycle(r.Context(), requestActor(r), "delete_apple", alias.Email, "failed", classifyICloudHMECode(err), err.Error())
		writeICloudHMEError(w, err)
		return
	}
	if err := api.store.UpdateICloudHMEAliasLifecycle(r.Context(), alias.Email, "deleted"); err != nil {
		writeICloudHMEError(w, err)
		return
	}
	_ = api.auditLifecycle(r.Context(), requestActor(r), "delete_apple", alias.Email, "success", "", "")
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
