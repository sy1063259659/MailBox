package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gptbox-server/internal/imapmail"
	"gptbox-server/internal/store"
)

const (
	publicMailIPLimit        = 120
	publicMailAliasLimit     = 30
	publicMailFailureLimit   = 10
	publicMailRequestWindow  = time.Minute
	publicMailFailureWindow  = 15 * time.Minute
	publicMailLimiterMaxKeys = 10000
)

type publicMailBucket struct {
	StartedAt time.Time
	Count     int
}

type iCloudHMEPublicLimiter struct {
	mu       sync.Mutex
	ip       map[string]publicMailBucket
	alias    map[string]publicMailBucket
	failures map[string]publicMailBucket
}

type publicICloudHMEMessage struct {
	imapmail.MessageDetail
	VerificationCode string `json:"verificationCode"`
}

func newICloudHMEPublicLimiter() *iCloudHMEPublicLimiter {
	return &iCloudHMEPublicLimiter{
		ip: make(map[string]publicMailBucket), alias: make(map[string]publicMailBucket),
		failures: make(map[string]publicMailBucket),
	}
}

func (limiter *iCloudHMEPublicLimiter) allow(bucketMap map[string]publicMailBucket, key string, limit int, window time.Duration, now time.Time) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.pruneLocked(now)
	bucket := bucketMap[key]
	if bucket.StartedAt.IsZero() || now.Sub(bucket.StartedAt) >= window {
		bucket = publicMailBucket{StartedAt: now}
	}
	if bucket.Count >= limit {
		return false, window - now.Sub(bucket.StartedAt)
	}
	bucket.Count++
	bucketMap[key] = bucket
	return true, 0
}

func (limiter *iCloudHMEPublicLimiter) allowIP(ip string, now time.Time) (bool, time.Duration) {
	return limiter.allow(limiter.ip, ip, publicMailIPLimit, publicMailRequestWindow, now)
}

func (limiter *iCloudHMEPublicLimiter) allowAlias(alias string, now time.Time) (bool, time.Duration) {
	return limiter.allow(limiter.alias, alias, publicMailAliasLimit, publicMailRequestWindow, now)
}

func (limiter *iCloudHMEPublicLimiter) allowAuthentication(key string, now time.Time) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.pruneLocked(now)
	bucket := limiter.failures[key]
	if bucket.StartedAt.IsZero() || now.Sub(bucket.StartedAt) >= publicMailFailureWindow {
		return true, 0
	}
	if bucket.Count >= publicMailFailureLimit {
		return false, publicMailFailureWindow - now.Sub(bucket.StartedAt)
	}
	return true, 0
}

func (limiter *iCloudHMEPublicLimiter) recordAuthenticationFailure(key string, now time.Time) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.pruneLocked(now)
	if _, exists := limiter.failures[key]; !exists && len(limiter.failures) >= publicMailLimiterMaxKeys {
		return
	}
	bucket := limiter.failures[key]
	if bucket.StartedAt.IsZero() || now.Sub(bucket.StartedAt) >= publicMailFailureWindow {
		bucket = publicMailBucket{StartedAt: now}
	}
	bucket.Count++
	limiter.failures[key] = bucket
}

func (limiter *iCloudHMEPublicLimiter) pruneLocked(now time.Time) {
	for key, bucket := range limiter.ip {
		if now.Sub(bucket.StartedAt) >= publicMailRequestWindow {
			delete(limiter.ip, key)
		}
	}
	for key, bucket := range limiter.alias {
		if now.Sub(bucket.StartedAt) >= publicMailRequestWindow {
			delete(limiter.alias, key)
		}
	}
	for key, bucket := range limiter.failures {
		if now.Sub(bucket.StartedAt) >= publicMailFailureWindow {
			delete(limiter.failures, key)
		}
	}
}

func (limiter *iCloudHMEPublicLimiter) clearAuthenticationFailures(key string) {
	limiter.mu.Lock()
	delete(limiter.failures, key)
	limiter.mu.Unlock()
}

func (api *iCloudHMEAPI) publicLatestMail(w http.ResponseWriter, r *http.Request) {
	credentials, ok := api.authenticatePublicMail(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := api.mailClient.ListMessageDetailsByRecipient(
		ctx, credentials.ICloudEmail, credentials.AppPassword, credentials.AliasEmail, 1, "",
	)
	if err != nil {
		writeICloudHMEPublicMailServiceError(w, err)
		return
	}
	if len(result.Messages) == 0 {
		WriteError(w, http.StatusNotFound, "mail_not_found", "暂未收到邮件")
		return
	}
	message := publicMessage(result.Messages[0])
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "address": credentials.AliasEmail, "message": message})
}

func (api *iCloudHMEAPI) publicMailHistory(w http.ResponseWriter, r *http.Request) {
	credentials, ok := api.authenticatePublicMail(w, r)
	if !ok {
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 20 {
			WriteError(w, http.StatusBadRequest, "bad_request", "limit 必须在 1 到 20 之间")
			return
		}
		limit = value
	}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if cursor != "" {
		if _, err := strconv.ParseUint(cursor, 10, 32); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "cursor 格式不正确")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, err := api.mailClient.ListMessageDetailsByRecipient(
		ctx, credentials.ICloudEmail, credentials.AppPassword, credentials.AliasEmail, limit, cursor,
	)
	if err != nil {
		writeICloudHMEPublicMailServiceError(w, err)
		return
	}
	messages := make([]publicICloudHMEMessage, 0, len(result.Messages))
	for _, message := range result.Messages {
		messages = append(messages, publicMessage(message))
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "address": credentials.AliasEmail,
		"messages": messages, "nextCursor": result.NextCursor,
	})
}

func (api *iCloudHMEAPI) authenticatePublicMail(w http.ResponseWriter, r *http.Request) (store.ICloudHMEMailCredentials, bool) {
	setPublicMailHeaders(w)
	now := time.Now()
	ip := publicMailClientIP(r)
	if allowed, retry := api.publicLimiter.allowIP(ip, now); !allowed {
		writePublicMailRateLimit(w, retry)
		return store.ICloudHMEMailCredentials{}, false
	}
	address := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("address")))
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	failureKey := ip + "\x00" + address
	if allowed, retry := api.publicLimiter.allowAuthentication(failureKey, now); !allowed {
		writePublicMailRateLimit(w, retry)
		return store.ICloudHMEMailCredentials{}, false
	}
	if address == "" || key == "" || api.store == nil {
		api.publicLimiter.recordAuthenticationFailure(failureKey, now)
		WriteError(w, http.StatusUnauthorized, "invalid_mail_credentials", "邮箱地址或收件密钥无效")
		return store.ICloudHMEMailCredentials{}, false
	}
	credentials, err := api.store.AuthenticateICloudHMEPublicMail(r.Context(), address, key)
	if errors.Is(err, store.ErrInvalidICloudHMEReceiveCredentials) {
		api.publicLimiter.recordAuthenticationFailure(failureKey, now)
		WriteError(w, http.StatusUnauthorized, "invalid_mail_credentials", "邮箱地址或收件密钥无效")
		return store.ICloudHMEMailCredentials{}, false
	}
	if errors.Is(err, store.ErrICloudHMEMailboxUnavailable) {
		WriteError(w, http.StatusForbidden, "mailbox_unavailable", "该隐藏邮箱当前不可用")
		return store.ICloudHMEMailCredentials{}, false
	}
	if errors.Is(err, store.ErrICloudHMEMailServiceUnavailable) {
		WriteError(w, http.StatusServiceUnavailable, "mail_service_unavailable", "收件服务暂不可用")
		return store.ICloudHMEMailCredentials{}, false
	}
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, "mail_service_unavailable", "收件服务暂不可用")
		return store.ICloudHMEMailCredentials{}, false
	}
	api.publicLimiter.clearAuthenticationFailures(failureKey)
	if allowed, retry := api.publicLimiter.allowAlias(credentials.AliasEmail, now); !allowed {
		writePublicMailRateLimit(w, retry)
		return store.ICloudHMEMailCredentials{}, false
	}
	return credentials, true
}

func publicMessage(message imapmail.MessageDetail) publicICloudHMEMessage {
	return publicICloudHMEMessage{
		MessageDetail: message, VerificationCode: extractICloudHMEVerificationCode(message),
	}
}

func setPublicMailHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func publicMailMethodHandler(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setPublicMailHeaders(w)
		if r.Method != method {
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		handler(w, r)
	}
}

func writePublicMailRateLimit(w http.ResponseWriter, retry time.Duration) {
	seconds := int(retry.Round(time.Second).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	WriteError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后重试")
}

func writeICloudHMEPublicMailServiceError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "icloud_mail_not_found") {
		WriteError(w, http.StatusNotFound, "mail_not_found", "暂未收到邮件")
		return
	}
	WriteError(w, http.StatusServiceUnavailable, "mail_service_unavailable", "收件服务暂不可用")
}

func publicMailClientIP(r *http.Request) string {
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
