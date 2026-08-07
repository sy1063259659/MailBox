package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"gptbox-server/internal/store"
)

const (
	iCloudGPTScanInterval = 4 * time.Hour
	iCloudGPTScanPoll     = 30 * time.Minute
	iCloudGPTScanLimit    = 20
)

type iCloudGPTScanResult struct {
	Scanned     int `json:"scanned"`
	PlusFound   int `json:"plusFound"`
	BannedFound int `json:"bannedFound"`
	Errors      int `json:"errors"`
}

type iCloudGPTStatusMail struct {
	id         int64
	kind       iCloudHMEGPTMailKind
	receivedAt time.Time
}

func (api *iCloudAPI) startGPTStatusWorker() {
	go func() {
		timer := time.NewTimer(40 * time.Second)
		defer timer.Stop()
		<-timer.C
		_, _ = api.runGPTStatusScan(context.Background(), false)

		ticker := time.NewTicker(iCloudGPTScanPoll)
		defer ticker.Stop()
		for range ticker.C {
			_, _ = api.runGPTStatusScan(context.Background(), false)
		}
	}()
}

func (api *iCloudAPI) scanGPTStatus(w http.ResponseWriter, r *http.Request) {
	result, err := api.runGPTStatusScan(r.Context(), true)
	if err != nil {
		WriteError(w, http.StatusConflict, "icloud_gpt_scan_busy", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (api *iCloudAPI) runGPTStatusScan(ctx context.Context, force bool) (iCloudGPTScanResult, error) {
	if !api.gptScanMu.TryLock() {
		return iCloudGPTScanResult{}, errors.New("iCloud GPT 状态扫描正在运行")
	}
	defer api.gptScanMu.Unlock()

	now := time.Now()
	dueBefore := now.Add(-iCloudGPTScanInterval)
	if force {
		dueBefore = now
	}
	targets, err := api.store.ListICloudGPTScanTargets(ctx, dueBefore, iCloudGPTScanLimit)
	if err != nil {
		return iCloudGPTScanResult{}, err
	}
	result := iCloudGPTScanResult{Scanned: len(targets)}
	for _, target := range targets {
		if err := api.scanICloudGPTTarget(ctx, target, &result); err != nil {
			result.Errors++
			_ = api.store.MarkICloudGPTScanned(ctx, target.Email, now.Add(-3*time.Hour), err)
			continue
		}
		_ = api.store.MarkICloudGPTScanned(ctx, target.Email, now, nil)
	}
	return result, nil
}

func (api *iCloudAPI) scanICloudGPTTarget(ctx context.Context, target store.ICloudGPTScanTarget, result *iCloudGPTScanResult) error {
	list, err := api.latestFetch.Messages(ctx, target.Email, target.Key)
	if err != nil {
		return err
	}
	plusActivatedAt := target.PlusActivatedAt
	if plusActivatedAt == nil {
		planMails, loadErr := api.loadICloudGPTStatusMails(ctx, target, list.Emails, iCloudHMEGPTMailPlus)
		if loadErr != nil {
			return loadErr
		}
		for _, message := range planMails {
			changed, recordErr := api.store.RecordICloudGPTPlus(ctx, target.Email, message.id, message.receivedAt)
			if recordErr != nil {
				return recordErr
			}
			if changed {
				activatedAt := message.receivedAt
				plusActivatedAt = &activatedAt
				result.PlusFound++
				break
			}
		}
	}
	if plusActivatedAt == nil {
		return nil
	}

	deactivationMails, err := api.loadICloudGPTStatusMails(ctx, target, list.Emails, iCloudHMEGPTMailDeactivated)
	if err != nil {
		return err
	}
	for _, message := range deactivationMails {
		if message.receivedAt.Before(*plusActivatedAt) {
			continue
		}
		changed, recordErr := api.store.RecordICloudGPTDeactivated(ctx, target.Email, message.id, message.receivedAt)
		if recordErr != nil {
			return recordErr
		}
		if changed {
			result.BannedFound++
			return nil
		}
	}
	return nil
}

func (api *iCloudAPI) loadICloudGPTStatusMails(
	ctx context.Context,
	target store.ICloudGPTScanTarget,
	summaries []iCloudMailSummary,
	expected iCloudHMEGPTMailKind,
) ([]iCloudGPTStatusMail, error) {
	statusMails := make([]iCloudGPTStatusMail, 0, len(summaries))
	for _, summary := range summaries {
		if classifyICloudGPTSubject(summary.Subject) != expected {
			continue
		}
		detailResponse, detailErr := api.latestFetch.Message(ctx, target.Email, target.Key, summary.ID)
		if detailErr != nil {
			return nil, detailErr
		}
		if detailResponse.Email == nil {
			continue
		}
		detail := detailResponse.Email
		subject := detail.Subject
		if strings.TrimSpace(subject) == "" {
			subject = summary.Subject
		}
		kind := classifyICloudGPTMail(subject, strings.TrimSpace(detail.Text+" "+detail.HTML))
		if kind != expected {
			continue
		}
		receivedAt := parseICloudGPTMailTime(detail.ReceivedAt, detail.CreatedAt, summary.ReceivedAt)
		if receivedAt.IsZero() {
			continue
		}
		messageID := detail.ID
		if messageID <= 0 {
			messageID = summary.ID
		}
		statusMails = append(statusMails, iCloudGPTStatusMail{id: messageID, kind: kind, receivedAt: receivedAt})
	}
	sort.Slice(statusMails, func(left, right int) bool {
		if statusMails[left].receivedAt.Equal(statusMails[right].receivedAt) {
			return statusMails[left].kind == iCloudHMEGPTMailPlus && statusMails[right].kind != iCloudHMEGPTMailPlus
		}
		return statusMails[left].receivedAt.Before(statusMails[right].receivedAt)
	})
	return statusMails, nil
}

func isICloudGPTCandidateSubject(subject string) bool {
	return classifyICloudGPTSubject(subject) != iCloudHMEGPTMailUnknown
}

func classifyICloudGPTSubject(subject string) iCloudHMEGPTMailKind {
	normalized := normalizeICloudHMEGPTSubject(subject)
	if strings.EqualFold(normalized, "ChatGPT - Your new plan") {
		return iCloudHMEGPTMailPlus
	}
	if iCloudHMEGPTDeactivatedSubj.MatchString(normalized) {
		return iCloudHMEGPTMailDeactivated
	}
	return iCloudHMEGPTMailUnknown
}

func parseICloudGPTMailTime(values ...string) time.Time {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05",
		time.RFC1123Z,
		time.RFC1123,
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}
