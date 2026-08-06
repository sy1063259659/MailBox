package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"gptbox-server/internal/store"
)

type createICloudHMEDeleteJobRequest struct {
	Emails      []string `json:"emails"`
	ConfirmText string   `json:"confirmText"`
}

func (api *iCloudHMEAPI) createDeleteJob(w http.ResponseWriter, r *http.Request) {
	var req createICloudHMEDeleteJobRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	expected := fmt.Sprintf("永久删除 %d 个", len(req.Emails))
	if strings.TrimSpace(req.ConfirmText) != expected {
		WriteError(w, http.StatusBadRequest, "confirmation_mismatch", "批量永久删除确认文字不匹配")
		return
	}
	job, err := api.store.CreateICloudHMEDeleteJob(r.Context(), req.Emails, requestActor(r))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "create_delete_job", TargetType: "delete_job",
		Target: strconv.FormatInt(job.ID, 10), Result: "success",
	})
	api.wakeDeleteJobWorker()
	WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "job": job})
}

func (api *iCloudHMEAPI) getDeleteJob(w http.ResponseWriter, r *http.Request) {
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/icloud-hme/delete-jobs/"), "/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "永久删除任务 ID 无效")
		return
	}
	job, err := api.store.GetICloudHMEDeleteJob(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
}

func (api *iCloudHMEAPI) startDeleteJobWorker() {
	_ = api.store.RecoverICloudHMEDeleteJobs(context.Background())
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			api.runAvailableDeleteJobs()
			select {
			case <-api.deleteJobWake:
			case <-ticker.C:
			}
		}
	}()
}

func (api *iCloudHMEAPI) wakeDeleteJobWorker() {
	select {
	case api.deleteJobWake <- struct{}{}:
	default:
	}
}

func (api *iCloudHMEAPI) runAvailableDeleteJobs() {
	for {
		job, err := api.store.NextRunnableICloudHMEDeleteJob(context.Background())
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			return
		}
		api.runDeleteJob(job)
	}
}

func (api *iCloudHMEAPI) runDeleteJob(job store.ICloudHMEDeleteJob) {
	ctx := context.Background()
	for {
		item, err := api.store.ClaimNextICloudHMEDeleteJobItem(ctx, job.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			_, _ = api.store.RefreshICloudHMEDeleteJob(ctx, job.ID)
			return
		}
		if err != nil {
			return
		}
		if err := api.permanentlyDeleteAlias(ctx, job.CreatedBy, item.Email); err != nil {
			_ = api.store.FailICloudHMEDeleteJobItem(ctx, item.ID, classifyICloudHMECode(err), friendlyICloudHMEError(err))
		} else {
			_ = api.store.CompleteICloudHMEDeleteJobItem(ctx, item.ID)
		}
		progress, err := api.store.RefreshICloudHMEDeleteJob(ctx, job.ID)
		if err != nil || (progress.Status != "pending" && progress.Status != "running") {
			return
		}
		time.Sleep(time.Second)
	}
}
