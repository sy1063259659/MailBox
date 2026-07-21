package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"gptbox-server/internal/store"
)

const iCloudHMEChallengeTTL = 5 * time.Minute

type iCloudHMELoginChallenge struct {
	ID        string
	SourceID  int64
	Client    iCloudHMEClient
	OTP       chan string
	Result    chan error
	ExpiresAt time.Time
	Actor     string
	once      sync.Once
}

type iCloudHMELoginStartRequest struct {
	Password string
}

type iCloudHMELoginCompleteRequest struct {
	ChallengeID string
	OTP         string
}

func (api *iCloudHMEAPI) loginStart(w http.ResponseWriter, r *http.Request, sourceID int64) {
	var req iCloudHMELoginStartRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "Apple 密码不能为空")
		return
	}
	credentials, err := api.store.GetICloudHMESourceCredentials(r.Context(), sourceID)
	if err != nil {
		writeICloudHMEError(w, err)
		return
	}
	client, err := api.newClient(nil, credentials.Host)
	if err != nil {
		writeICloudHMEError(w, err)
		return
	}

	challenge := &iCloudHMELoginChallenge{
		ID: uuid.NewString(), SourceID: sourceID, Client: client,
		OTP: make(chan string, 1), Result: make(chan error, 1),
		ExpiresAt: time.Now().Add(iCloudHMEChallengeTTL), Actor: requestActor(r),
	}
	otpRequired := make(chan struct{}, 1)
	go func(password string) {
		err := safeICloudHMELogin(client, credentials.AppleIDEmail, password, func() (string, error) {
			otpRequired <- struct{}{}
			select {
			case otp := <-challenge.OTP:
				return otp, nil
			case <-time.After(iCloudHMEChallengeTTL):
				return "", errors.New("验证码挑战已过期")
			}
		})
		challenge.Result <- err
	}(req.Password)

	select {
	case <-otpRequired:
		api.challenges.Store(challenge.ID, challenge)
		time.AfterFunc(iCloudHMEChallengeTTL, func() { api.challenges.Delete(challenge.ID) })
		WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "otpRequired": true, "challengeId": challenge.ID, "expiresAt": challenge.ExpiresAt,
		})
	case err := <-challenge.Result:
		if err != nil {
			api.markSourceError(r.Context(), sourceID, err)
			writeICloudHMEError(w, err)
			return
		}
		if err := api.persistLoggedInClient(r.Context(), sourceID, client, requestActor(r)); err != nil {
			writeICloudHMEError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "otpRequired": false})
	case <-time.After(45 * time.Second):
		WriteError(w, http.StatusGatewayTimeout, "icloud_login_timeout", "Apple 登录响应超时，请稍后重试")
	}
}

func (api *iCloudHMEAPI) loginComplete(w http.ResponseWriter, r *http.Request, sourceID int64) {
	var req iCloudHMELoginCompleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	otp := strings.TrimSpace(req.OTP)
	if otp == "" {
		WriteError(w, http.StatusBadRequest, "icloud_otp_required", "请输入 Apple 双重认证验证码")
		return
	}
	challengeID := strings.TrimSpace(req.ChallengeID)
	value, ok := api.challenges.Load(challengeID)
	if !ok {
		WriteError(w, http.StatusBadRequest, "icloud_otp_expired", "验证码挑战不存在或已失效")
		return
	}
	challenge, ok := value.(*iCloudHMELoginChallenge)
	if !ok || challenge.SourceID != sourceID || time.Now().After(challenge.ExpiresAt) {
		if ok && time.Now().After(challenge.ExpiresAt) {
			api.challenges.Delete(challengeID)
		}
		WriteError(w, http.StatusBadRequest, "icloud_otp_expired", "验证码挑战不存在或已失效")
		return
	}
	value, ok = api.challenges.LoadAndDelete(challengeID)
	if !ok || value != challenge {
		WriteError(w, http.StatusBadRequest, "icloud_otp_expired", "验证码挑战已被使用")
		return
	}
	challenge.once.Do(func() { challenge.OTP <- otp })
	select {
	case err := <-challenge.Result:
		if err != nil {
			api.markSourceError(r.Context(), sourceID, err)
			_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
				Actor: challenge.Actor, Action: "source_login", TargetType: "source_account",
				Target: strconv.FormatInt(sourceID, 10), Result: "failed",
				ErrorCode: classifyICloudHMECode(err), Message: err.Error(),
			})
			writeICloudHMEError(w, err)
			return
		}
		if err := api.persistLoggedInClient(r.Context(), sourceID, challenge.Client, challenge.Actor); err != nil {
			writeICloudHMEError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	case <-time.After(90 * time.Second):
		WriteError(w, http.StatusGatewayTimeout, "icloud_login_timeout", "Apple 验证响应超时，请重新登录")
	}
}
func (api *iCloudHMEAPI) persistLoggedInClient(ctx context.Context, sourceID int64, client iCloudHMEClient, actor string) error {
	if err := client.ValidateSession(); err != nil {
		api.markSourceError(ctx, sourceID, err)
		return err
	}
	if err := api.store.SaveICloudHMECookies(ctx, sourceID, client.GetCookies(), "active", ""); err != nil {
		return err
	}
	return api.store.AddICloudHMEAudit(ctx, store.ICloudHMEAuditLog{
		Actor: actor, Action: "source_login", TargetType: "source_account",
		Target: strconv.FormatInt(sourceID, 10), Result: "success",
	})
}
