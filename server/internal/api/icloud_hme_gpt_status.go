package api

import (
	"context"
	"errors"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gptbox-server/internal/imapmail"
	"gptbox-server/internal/store"
)

const (
	iCloudHMEGPTScanInterval = 4 * time.Hour
	iCloudHMEGPTScanPoll     = 30 * time.Minute
	iCloudHMEGPTScanLimit    = 500
)

var (
	iCloudHMEGPTHTMLTagRE       = regexp.MustCompile(`(?is)<[^>]*>`)
	iCloudHMEGPTDeactivatedSubj = regexp.MustCompile(`(?i)^openai - access deactivated(?:\s+\[[^\]]+\])?$`)
)

type iCloudHMEGPTMailKind string

const (
	iCloudHMEGPTMailUnknown     iCloudHMEGPTMailKind = ""
	iCloudHMEGPTMailPlus        iCloudHMEGPTMailKind = "plus"
	iCloudHMEGPTMailDeactivated iCloudHMEGPTMailKind = "deactivated"
)

type iCloudHMEGPTScanResult struct {
	Scanned     int `json:"scanned"`
	PlusFound   int `json:"plusFound"`
	BannedFound int `json:"bannedFound"`
	Errors      int `json:"errors"`
}

func normalizeICloudHMEGPTSubject(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeICloudHMEGPTBody(value string) string {
	value = html.UnescapeString(iCloudHMEGPTHTMLTagRE.ReplaceAllString(value, " "))
	value = strings.ReplaceAll(value, "’", "'")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func classifyICloudHMEGPTMail(message imapmail.MessageDetail) iCloudHMEGPTMailKind {
	return classifyICloudGPTMail(message.Subject, message.Content)
}

func classifyICloudGPTMail(rawSubject, rawBody string) iCloudHMEGPTMailKind {
	subject := normalizeICloudHMEGPTSubject(rawSubject)
	body := normalizeICloudHMEGPTBody(rawBody)
	if strings.EqualFold(subject, "ChatGPT - Your new plan") &&
		strings.Contains(body, "you've successfully subscribed to chatgpt plus") &&
		strings.Contains(body, "chatgpt plus subscription") {
		return iCloudHMEGPTMailPlus
	}
	if iCloudHMEGPTDeactivatedSubj.MatchString(subject) &&
		strings.Contains(body, "your account has been deactivated") &&
		strings.Contains(body, "can no longer be used") {
		return iCloudHMEGPTMailDeactivated
	}
	return iCloudHMEGPTMailUnknown
}

func (api *iCloudHMEAPI) startGPTStatusWorker() {
	go func() {
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		<-timer.C
		_, _ = api.runGPTStatusScan(context.Background(), false)

		ticker := time.NewTicker(iCloudHMEGPTScanPoll)
		defer ticker.Stop()
		for range ticker.C {
			_, _ = api.runGPTStatusScan(context.Background(), false)
		}
	}()
}

func (api *iCloudHMEAPI) scanGPTStatus(w http.ResponseWriter, r *http.Request) {
	result, err := api.runGPTStatusScan(r.Context(), true)
	if err != nil {
		WriteError(w, http.StatusConflict, "icloud_gpt_scan_busy", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (api *iCloudHMEAPI) runGPTStatusScan(ctx context.Context, force bool) (iCloudHMEGPTScanResult, error) {
	if !api.gptScanMu.TryLock() {
		return iCloudHMEGPTScanResult{}, errors.New("GPT 状态扫描正在运行")
	}
	defer api.gptScanMu.Unlock()

	now := time.Now()
	dueBefore := now.Add(-iCloudHMEGPTScanInterval)
	if force {
		dueBefore = now
	}
	targets, err := api.store.ListICloudHMEGPTScanTargets(ctx, dueBefore)
	if err != nil {
		return iCloudHMEGPTScanResult{}, err
	}
	grouped := make(map[int64][]store.ICloudHMEGPTScanTarget)
	for _, target := range targets {
		grouped[target.SourceAccountID] = append(grouped[target.SourceAccountID], target)
	}
	result := iCloudHMEGPTScanResult{}
	for sourceID, sourceTargets := range grouped {
		result.Scanned += len(sourceTargets)
		emails := make([]string, 0, len(sourceTargets))
		targetByEmail := make(map[string]*store.ICloudHMEGPTScanTarget, len(sourceTargets))
		since := now
		for index := range sourceTargets {
			target := &sourceTargets[index]
			email := strings.ToLower(strings.TrimSpace(target.Email))
			emails = append(emails, email)
			targetByEmail[email] = target
			start := target.CreatedAt.Add(-24 * time.Hour)
			if target.PlusActivatedAt != nil {
				start = target.PlusActivatedAt.Add(-24 * time.Hour)
			}
			if start.Before(since) {
				since = start
			}
		}

		source, sourceErr := api.store.GetICloudHMESourceCredentials(ctx, sourceID)
		if sourceErr != nil {
			result.Errors++
			_ = api.store.MarkICloudHMEGPTScanned(ctx, emails, now.Add(-3*time.Hour), sourceErr)
			continue
		}
		messages, scanErr := api.mailClient.ListICloudOpenAIStatusMessages(
			ctx, source.ICloudEmail, source.AppPassword, since, iCloudHMEGPTScanLimit,
		)
		if scanErr != nil {
			result.Errors++
			_ = api.store.MarkICloudHMEGPTScanned(ctx, emails, now.Add(-3*time.Hour), scanErr)
			continue
		}

		for _, message := range messages {
			if classifyICloudHMEGPTMail(message) != iCloudHMEGPTMailPlus {
				continue
			}
			for _, address := range message.To {
				email := strings.ToLower(strings.TrimSpace(address.Email))
				target := targetByEmail[email]
				if target == nil || target.PlusActivatedAt != nil {
					continue
				}
				changed, recordErr := api.store.RecordICloudHMEGPTPlus(ctx, email, message.ID, message.ReceivedAt)
				if recordErr == nil && changed {
					activatedAt := message.ReceivedAt
					target.PlusActivatedAt = &activatedAt
					target.Status = "plus"
					result.PlusFound++
				}
			}
		}
		for _, message := range messages {
			if classifyICloudHMEGPTMail(message) != iCloudHMEGPTMailDeactivated {
				continue
			}
			for _, address := range message.To {
				email := strings.ToLower(strings.TrimSpace(address.Email))
				target := targetByEmail[email]
				if target == nil || target.PlusActivatedAt == nil || message.ReceivedAt.Before(*target.PlusActivatedAt) {
					continue
				}
				changed, recordErr := api.store.RecordICloudHMEGPTDeactivated(ctx, email, message.ID, message.ReceivedAt)
				if recordErr == nil && changed {
					target.Status = "deactivated"
					result.BannedFound++
				}
			}
		}
		_ = api.store.MarkICloudHMEGPTScanned(ctx, emails, now, nil)
	}
	return result, nil
}
