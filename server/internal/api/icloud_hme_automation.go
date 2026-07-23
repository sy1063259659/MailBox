package api

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"gptbox-server/internal/store"
)

type updateICloudHMEAutomationRequest struct {
	Enabled              bool   `json:"enabled"`
	TargetAvailableCount int    `json:"targetAvailableCount"`
	TargetGroup          string `json:"targetGroup"`
	LabelPrefix          string `json:"labelPrefix"`
}

type updateICloudHMEInventoryRequest struct {
	Emails []string `json:"emails"`
	Status string   `json:"status"`
}

func (api *iCloudHMEAPI) automationSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := api.store.GetICloudHMEAutomation(r.Context())
		if err != nil {
			WriteError(w, 500, "internal_error", err.Error())
			return
		}
		WriteJSON(w, 200, map[string]any{"ok": true, "automation": settings})
	case http.MethodPut:
		var req updateICloudHMEAutomationRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		settings, err := api.store.UpdateICloudHMEAutomation(
			r.Context(), req.Enabled, req.TargetAvailableCount, req.TargetGroup,
			req.LabelPrefix, requestActor(r),
		)
		if err != nil {
			WriteError(w, 400, "bad_request", err.Error())
			return
		}
		_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
			Actor: requestActor(r), Action: "update_automation", TargetType: "automation",
			Target: "global", Result: "success",
		})
		api.wakeJobWorker()
		WriteJSON(w, 200, map[string]any{"ok": true, "automation": settings})
	default:
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (api *iCloudHMEAPI) automationEvents(w http.ResponseWriter, r *http.Request) {
	events, err := api.store.ListICloudHMEAutomationEvents(r.Context(), 100)
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, 200, map[string]any{"ok": true, "events": events})
}

func (api *iCloudHMEAPI) updateInventoryStatus(w http.ResponseWriter, r *http.Request) {
	var req updateICloudHMEInventoryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := api.store.UpdateICloudHMEInventoryStatus(r.Context(), req.Emails, req.Status); err != nil {
		WriteError(w, 400, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "update_inventory", TargetType: "alias",
		Target: strings.Join(req.Emails, ","), Result: "success",
	})
	api.wakeJobWorker()
	WriteJSON(w, 200, map[string]bool{"ok": true})
}

func (api *iCloudHMEAPI) ensureAutomationInventory(ctx context.Context) {
	settings, err := api.store.GetICloudHMEAutomation(ctx)
	if err != nil || !settings.Enabled {
		return
	}
	shortage := settings.TargetAvailableCount - settings.AvailableCount - settings.PendingCount
	if shortage <= 0 {
		return
	}
	if shortage > 20 {
		shortage = 20
	}
	job, err := api.store.CreateICloudHMEAutomationJob(ctx, settings, shortage)
	if err != nil {
		_ = api.store.AddICloudHMEAutomationEvent(ctx, nil, nil, "queue", "waiting",
			"icloud_source_wait", "主账号正在等待下一次单项探测或需要重新验证",
			settings.NextCreateAt, 0, noProbeEventMetrics())
		return
	}
	_ = api.store.AddICloudHMEAutomationEvent(ctx, nil, nil, "queue", "scheduled", "",
		"已按库存缺口加入自动创建队列", nil, 0, noProbeEventMetrics())
	_ = job
}

type automationProbeRange struct {
	min time.Duration
	max time.Duration
}

var automationProbeRanges = [...]automationProbeRange{
	{min: 8 * time.Minute, max: 12 * time.Minute},
	{min: 6 * time.Minute, max: 8 * time.Minute},
	{min: 4 * time.Minute, max: 6 * time.Minute},
}

func clampAutomationProbeStage(stage int) int {
	if stage < 0 {
		return 0
	}
	if stage >= len(automationProbeRanges) {
		return len(automationProbeRanges) - 1
	}
	return stage
}

func automationProbeRangeForStage(stage int) automationProbeRange {
	return automationProbeRanges[clampAutomationProbeStage(stage)]
}

func randomAutomationCreateDelay(stage int) time.Duration {
	target := automationProbeRangeForStage(stage)
	span := int64(target.max - target.min)
	if span <= 0 {
		return target.min
	}
	return target.min + time.Duration(rand.Int63n(span+1))
}

func automationProbeEventMetrics(stage, intervalSeconds, recoverySeconds int) store.ICloudHMEProbeEventMetrics {
	target := automationProbeRangeForStage(stage)
	return store.ICloudHMEProbeEventMetrics{
		Stage:           clampAutomationProbeStage(stage),
		IntervalSeconds: intervalSeconds,
		RecoverySeconds: recoverySeconds,
		TargetMinSecs:   int(target.min / time.Second),
		TargetMaxSecs:   int(target.max / time.Second),
	}
}

func noProbeEventMetrics() store.ICloudHMEProbeEventMetrics {
	return store.ICloudHMEProbeEventMetrics{Stage: -1}
}

func nextAutomationProbeSuccess(
	state store.ICloudHMEProbeState,
	now time.Time,
) store.ICloudHMEProbeTransition {
	stage := clampAutomationProbeStage(state.Stage)
	stableStage := state.StableStage
	if stableStage < -1 || stableStage >= len(automationProbeRanges) {
		stableStage = -1
	}
	intervalSeconds := 0
	if state.LastCreatedAt != nil && now.After(*state.LastCreatedAt) {
		intervalSeconds = int(now.Sub(*state.LastCreatedAt) / time.Second)
	}

	if state.LimitStartedAt != nil {
		failedStage := clampAutomationProbeStage(state.LastLimitStage)
		if failedStage > 0 {
			stage = failedStage - 1
		} else {
			stage = 0
		}
		recoverySeconds := 0
		if now.After(*state.LimitStartedAt) {
			recoverySeconds = int(now.Sub(*state.LimitStartedAt) / time.Second)
		}
		return store.ICloudHMEProbeTransition{
			AttemptStage:    clampAutomationProbeStage(state.Stage),
			Stage:           stage,
			SuccessStreak:   0,
			SuccessTarget:   5,
			StableStage:     stableStage,
			RecoveryMode:    true,
			IntervalSeconds: intervalSeconds,
			RecoverySeconds: recoverySeconds,
		}
	}

	target := state.SuccessTarget
	if target < 1 {
		target = 3
	}
	if stage == len(automationProbeRanges)-1 && !state.RecoveryMode {
		target = 5
	}
	streak := state.SuccessStreak + 1
	recoveryMode := state.RecoveryMode
	if streak >= target {
		if recoveryMode || stage == len(automationProbeRanges)-1 {
			stableStage = stage
			recoveryMode = false
		}
		if stage < len(automationProbeRanges)-1 {
			stage++
			streak = 0
			if stage == len(automationProbeRanges)-1 {
				target = 5
			} else {
				target = 3
			}
		} else {
			streak = target
			target = 5
		}
	}
	return store.ICloudHMEProbeTransition{
		AttemptStage:    clampAutomationProbeStage(state.Stage),
		Stage:           stage,
		SuccessStreak:   streak,
		SuccessTarget:   target,
		StableStage:     stableStage,
		RecoveryMode:    recoveryMode,
		IntervalSeconds: intervalSeconds,
		RecoverySeconds: state.LastRecoverySeconds,
	}
}

func classifyAutomationRetry(err error) string {
	if err == nil {
		return ""
	}
	code := classifyICloudHMECode(err)
	if code == "icloud_alias_rate_limited" {
		return "rate_limit"
	}
	if code == "icloud_session_expired" {
		return "session"
	}
	if code == "icloud_plus_required" {
		return "icloud_plus"
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") ||
		strings.Contains(lower, "connection") || strings.Contains(lower, "http 5") {
		return "network"
	}
	return "protocol"
}

func networkRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 15 * time.Minute
	case 2:
		return time.Hour
	default:
		return 6 * time.Hour
	}
}

func (api *iCloudHMEAPI) handleAutomationFailure(
	ctx context.Context,
	job store.ICloudHMECreateJob,
	item store.ICloudHMECreateJobItem,
	sourceID int64,
	code string,
	processErr error,
) bool {
	retryClass := classifyAutomationRetry(processErr)
	var sourcePointer *int64
	if sourceID > 0 {
		sourcePointer = &sourceID
	}
	if code == "icloud_no_eligible_source" {
		next := time.Now().Add(time.Minute)
		if settings, err := api.store.GetICloudHMEAutomation(ctx); err == nil &&
			settings.NextCreateAt != nil && settings.NextCreateAt.After(next) {
			next = *settings.NextCreateAt
		}
		_ = api.store.DeferICloudHMEJobItem(ctx, item.ID, nil, "source_wait", code,
			"暂无到期可用的主账号", next)
		return true
	}
	switch retryClass {
	case "rate_limit":
		limited, err := api.store.MarkICloudHMEAutomationLimited(ctx, sourceID)
		if err == nil {
			_ = api.store.DeferICloudHMEJobItem(ctx, item.ID, sourcePointer, retryClass, code, processErr.Error(), limited.Next)
			_ = api.store.AddICloudHMEAutomationEvent(ctx, sourcePointer, &item.ID, "create", "deferred", code,
				"Apple 暂时限制创建，冷却后将单次探测", &limited.Next, item.Attempts,
				automationProbeEventMetrics(limited.Stage, limited.IntervalSeconds, 0))
			return true
		}
	case "network":
		if item.Attempts <= 3 {
			next := time.Now().Add(networkRetryDelay(item.Attempts))
			_ = api.store.DelayICloudHMESourceAutomation(ctx, sourceID, next)
			_ = api.store.DeferICloudHMEJobItem(ctx, item.ID, sourcePointer, retryClass, code,
				"Apple 网络服务暂时不可用，已安排重试", next)
			_ = api.store.AddICloudHMEAutomationEvent(ctx, sourcePointer, &item.ID, "create", "deferred", code,
				"网络错误，已安排有限重试", &next, item.Attempts, noProbeEventMetrics())
			return true
		}
	case "session", "icloud_plus":
		status := "reauth_required"
		if retryClass == "icloud_plus" {
			status = "icloud_plus_required"
		}
		_ = api.store.PauseICloudHMESourceAutomation(ctx, sourceID, status, processErr.Error())
		next := time.Now().Add(time.Minute)
		_ = api.store.DeferICloudHMEJobItem(ctx, item.ID, sourcePointer, retryClass, code,
			"主账号已暂停，等待其他健康账号接管", next)
		_ = api.store.AddICloudHMEAutomationEvent(ctx, sourcePointer, &item.ID, "source", "paused", code,
			"主账号自动补货已暂停", nil, item.Attempts, noProbeEventMetrics())
		return true
	case "protocol":
		paused, _ := api.store.RecordICloudHMEAutomationProtocolFailure(ctx)
		if paused {
			_ = api.store.FailICloudHMEJobItem(ctx, item.ID, sourcePointer, code, processErr.Error())
			_ = api.store.AddICloudHMEAutomationEvent(ctx, sourcePointer, &item.ID, "safety", "paused", code,
				"同类协议异常一小时内连续出现 3 次，自动补货已暂停",
				nil, item.Attempts, noProbeEventMetrics())
			return true
		}
	}
	return false
}

var errNoEligibleAutomationSource = errors.New("暂无到期可用的自动补货主账号")
