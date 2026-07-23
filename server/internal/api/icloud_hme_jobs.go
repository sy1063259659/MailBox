package api

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"gptbox-server/internal/icloudhme"
	"gptbox-server/internal/store"
)

const (
	iCloudHMEJobMinDelay = 3 * time.Second
	iCloudHMEJobMaxDelay = 6 * time.Second
)

type createICloudHMEJobRequest struct {
	Mode            string
	SourceAccountID *int64
	LabelPrefix     string
	Group           string
	Count           int
}

func (api *iCloudHMEAPI) createJob(w http.ResponseWriter, r *http.Request) {
	var req createICloudHMEJobRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	job, err := api.store.CreateICloudHMEJob(r.Context(), store.ICloudHMECreateJobInput{
		Mode: req.Mode, SourceAccountID: req.SourceAccountID, LabelPrefix: req.LabelPrefix,
		GroupName: req.Group, Count: req.Count, CreatedBy: requestActor(r),
	})
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "create_job", TargetType: "job",
		Target: strconv.FormatInt(job.ID, 10), Result: "success",
	})
	api.wakeJobWorker()
	WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "job": job})
}

func (api *iCloudHMEAPI) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := api.store.ListICloudHMEJobs(r.Context(), 30)
	if err != nil {
		WriteError(w, 500, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "jobs": jobs})
}

func (api *iCloudHMEAPI) getJob(w http.ResponseWriter, r *http.Request, id int64) {
	job, err := api.store.GetICloudHMEJob(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
}

func (api *iCloudHMEAPI) cancelJob(w http.ResponseWriter, r *http.Request, id int64) {
	if err := api.store.CancelICloudHMEJob(r.Context(), id); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_, _ = api.store.RefreshICloudHMEJobProgress(r.Context(), id)
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "cancel_job", TargetType: "job",
		Target: strconv.FormatInt(id, 10), Result: "success",
	})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *iCloudHMEAPI) retryJob(w http.ResponseWriter, r *http.Request, id int64) {
	if err := api.store.RetryICloudHMEJob(r.Context(), id); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "retry_job", TargetType: "job",
		Target: strconv.FormatInt(id, 10), Result: "success",
	})
	api.wakeJobWorker()
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (api *iCloudHMEAPI) startJobWorker() {
	_ = api.store.RecoverICloudHMEJobs(context.Background())
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			api.ensureAutomationInventory(context.Background())
			api.runAvailableJobs()
			select {
			case <-api.jobWake:
			case <-ticker.C:
			}
		}
	}()
}

func (api *iCloudHMEAPI) wakeJobWorker() {
	select {
	case api.jobWake <- struct{}{}:
	default:
	}
}

func (api *iCloudHMEAPI) runAvailableJobs() {
	for {
		job, err := api.store.NextRunnableICloudHMEJob(context.Background())
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			return
		}
		api.runJob(job)
	}
}

func (api *iCloudHMEAPI) runJob(job store.ICloudHMECreateJob) {
	ctx := context.Background()
	for {
		cancelled, err := api.store.IsICloudHMEJobCancelRequested(ctx, job.ID)
		if err != nil || cancelled {
			_, _ = api.store.RefreshICloudHMEJobProgress(ctx, job.ID)
			return
		}
		if !api.waitForICloudHMEJobSlot(ctx, job.ID) {
			_, _ = api.store.RefreshICloudHMEJobProgress(ctx, job.ID)
			return
		}
		item, err := api.store.ClaimNextICloudHMEJobItem(ctx, job.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			_, _ = api.store.RefreshICloudHMEJobProgress(ctx, job.ID)
			return
		}
		if err != nil {
			return
		}
		sourceID, email, code, processErr := api.processJobItem(ctx, job, item)
		if processErr != nil {
			if job.Origin == "automation" && api.handleAutomationFailure(ctx, job, item, sourceID, code, processErr) {
				_, _ = api.store.RefreshICloudHMEJobProgress(ctx, job.ID)
				return
			}
			var sourcePointer *int64
			if sourceID > 0 {
				sourcePointer = &sourceID
			}
			_ = api.store.FailICloudHMEJobItem(ctx, item.ID, sourcePointer, code, processErr.Error())
			_ = api.store.AddICloudHMEAudit(ctx, store.ICloudHMEAuditLog{
				Actor: job.CreatedBy, Action: "create_alias", TargetType: "job_item",
				Target: item.Label, Result: "failed", ErrorCode: code, Message: processErr.Error(),
			})
		} else {
			_ = api.store.CompleteICloudHMEJobItem(ctx, item.ID, sourceID, email)
			if job.Origin == "automation" {
				next := time.Now().Add(randomAutomationCreateDelay())
				_ = api.store.MarkICloudHMEAutomationSuccess(ctx, sourceID, next)
				_ = api.store.AddICloudHMEAutomationEvent(ctx, &sourceID, &item.ID, "create", "success", "",
					"隐藏邮箱创建成功", &next, item.Attempts)
			}
			_ = api.store.AddICloudHMEAudit(ctx, store.ICloudHMEAuditLog{
				Actor: job.CreatedBy, Action: "create_alias", TargetType: "alias",
				Target: email, Result: "success",
			})
		}
		progress, err := api.store.RefreshICloudHMEJobProgress(ctx, job.ID)
		if err != nil || !isRunningICloudHMEJob(progress.Status) {
			return
		}
	}
}

func isRunningICloudHMEJob(status string) bool {
	return status == "pending" || status == "running" || status == "cancel_requested"
}

func (api *iCloudHMEAPI) waitForICloudHMEJobSlot(ctx context.Context, jobID int64) bool {
	api.jobPaceMu.Lock()
	defer api.jobPaceMu.Unlock()
	if !api.lastJobItem.IsZero() {
		remaining := randomICloudHMEDelay() - time.Since(api.lastJobItem)
		if remaining > 0 && !api.waitJobDelay(ctx, jobID, remaining) {
			return false
		}
	}
	api.lastJobItem = time.Now()
	return true
}
func (api *iCloudHMEAPI) waitJobDelay(ctx context.Context, jobID int64, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		case <-ticker.C:
			cancelled, err := api.store.IsICloudHMEJobCancelRequested(ctx, jobID)
			if err != nil || cancelled {
				return false
			}
		}
	}
}

func randomICloudHMEDelay() time.Duration {
	span := int64(iCloudHMEJobMaxDelay - iCloudHMEJobMinDelay)
	if span <= 0 {
		return iCloudHMEJobMinDelay
	}
	return iCloudHMEJobMinDelay + time.Duration(rand.Int63n(span+1))
}

func (api *iCloudHMEAPI) processJobItem(ctx context.Context, job store.ICloudHMECreateJob, item store.ICloudHMECreateJobItem) (int64, string, string, error) {
	if usesFixedICloudHMESource(job) {
		email, err := api.createOrRecoverJobAlias(ctx, *job.SourceAccountID, item.Label, job.GroupName)
		if err != nil {
			return *job.SourceAccountID, "", classifyICloudHMECode(err), err
		}
		return *job.SourceAccountID, email, "", nil
	}
	var sources []store.ICloudHMESourceCredentials
	var err error
	if job.Origin == "automation" {
		sources, err = api.store.EligibleICloudHMEAutomationSources(ctx)
	} else {
		sources, err = api.store.HealthyICloudHMESources(ctx)
	}
	if err != nil {
		return 0, "", "internal_error", err
	}
	if len(sources) == 0 {
		if job.Origin == "automation" {
			return 0, "", "icloud_no_eligible_source", errNoEligibleAutomationSource
		}
		return 0, "", "icloud_no_healthy_source", errors.New("没有可用的健康 Apple 主账号")
	}
	var lastErr error
	var lastSourceID int64
	for _, source := range sources {
		lastSourceID = source.ID
		if job.Origin == "automation" {
			_ = api.store.MarkICloudHMEAutomationAttempt(ctx, source.ID)
		}
		email, err := api.createOrRecoverJobAlias(ctx, source.ID, item.Label, job.GroupName)
		if err == nil {
			return source.ID, email, "", nil
		}
		lastErr = err
		if job.Origin != "automation" {
			_ = api.store.MarkICloudHMESourceActivity(ctx, source.ID, "create", err)
		}
		if job.Origin == "automation" {
			break
		}
	}
	return lastSourceID, "", classifyICloudHMECode(lastErr), lastErr
}

func usesFixedICloudHMESource(job store.ICloudHMECreateJob) bool {
	return job.Origin != "automation" &&
		job.Mode == store.ICloudHMEJobModeFixed &&
		job.SourceAccountID != nil
}

func (api *iCloudHMEAPI) createOrRecoverJobAlias(ctx context.Context, sourceID int64, label, group string) (string, error) {
	api.aliasCreateMu.Lock()
	defer api.aliasCreateMu.Unlock()
	lock := api.lockForSource(sourceID)
	lock.Lock()
	defer lock.Unlock()

	client, _, err := api.clientForSource(ctx, sourceID)
	if err != nil {
		return "", err
	}
	if err := client.ValidateSession(); err != nil {
		api.markSourceError(ctx, sourceID, err)
		return "", err
	}
	aliases, err := client.ListAliases()
	if err != nil {
		api.markSourceError(ctx, sourceID, err)
		return "", err
	}
	if _, _, err := api.syncAliasList(ctx, sourceID, aliases, group); err != nil {
		return "", err
	}
	for _, alias := range aliases {
		if strings.TrimSpace(alias.Label) == strings.TrimSpace(label) {
			return alias.Email, nil
		}
	}
	created, err := client.CreateAlias(label, 5)
	if err != nil {
		api.markSourceError(ctx, sourceID, err)
		return "", err
	}
	_, _, err = api.store.UpsertICloudHMEAliases(ctx, sourceID, []store.ICloudHMEAliasInput{{
		Email: created.Email, Label: created.Label, Active: true, CreatedAt: parseICloudHMETime(created.CreatedAt),
	}}, group)
	if err != nil {
		return "", err
	}
	_ = api.store.SaveICloudHMECookies(ctx, sourceID, client.GetCookies(), "active", "")
	_ = api.store.MarkICloudHMESourceActivity(ctx, sourceID, "create", nil)
	return created.Email, nil
}

func (api *iCloudHMEAPI) syncSource(ctx context.Context, sourceID int64, group string) (int, int, error) {
	api.aliasCreateMu.Lock()
	defer api.aliasCreateMu.Unlock()
	lock := api.lockForSource(sourceID)
	lock.Lock()
	defer lock.Unlock()
	client, _, err := api.clientForSource(ctx, sourceID)
	if err != nil {
		return 0, 0, err
	}
	aliases, err := client.ListAliases()
	if err != nil {
		api.markSourceError(ctx, sourceID, err)
		_ = api.store.MarkICloudHMESourceActivity(ctx, sourceID, "sync", err)
		return 0, 0, err
	}
	imported, updated, err := api.syncAliasList(ctx, sourceID, aliases, group)
	if err == nil {
		_ = api.store.SaveICloudHMECookies(ctx, sourceID, client.GetCookies(), "active", "")
		_ = api.store.MarkICloudHMESourceActivity(ctx, sourceID, "sync", nil)
	}
	return imported, updated, err
}

func (api *iCloudHMEAPI) syncAliasList(ctx context.Context, sourceID int64, aliases []icloudhme.Alias, group string) (int, int, error) {
	inputs := make([]store.ICloudHMEAliasInput, 0, len(aliases))
	for _, alias := range aliases {
		inputs = append(inputs, store.ICloudHMEAliasInput{
			Email: alias.Email, AnonymousID: alias.AnonymousID, Label: alias.Label,
			Active: alias.Active, CreatedAt: parseICloudHMETime(alias.CreatedAt),
		})
	}
	return api.store.SyncICloudHMEAliases(ctx, sourceID, inputs, group)
}

func parseICloudHMETime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return parsed
}

func classifyICloudHMECode(err error) string {
	if err == nil {
		return ""
	}
	code, _ := classifyICloudHMEError(err)
	return code
}
func (api *iCloudHMEAPI) routeJob(w http.ResponseWriter, r *http.Request) {
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/icloud-hme/jobs/"), "/")
	parts := strings.Split(raw, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "任务 id 非法")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case r.Method == http.MethodGet && action == "":
		api.getJob(w, r, id)
	case r.Method == http.MethodPost && action == "cancel":
		api.cancelJob(w, r, id)
	case r.Method == http.MethodPost && action == "retry":
		api.retryJob(w, r, id)
	default:
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}
