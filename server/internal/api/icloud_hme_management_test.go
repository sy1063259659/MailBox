package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gptbox-server/internal/imapmail"
	"gptbox-server/internal/store"
)

func TestRandomICloudHMEDelayStaysInsideSafetyWindow(t *testing.T) {
	for i := 0; i < 200; i++ {
		delay := randomICloudHMEDelay()
		if delay < iCloudHMEJobMinDelay || delay > iCloudHMEJobMaxDelay {
			t.Fatalf("delay = %v, want %v..%v", delay, iCloudHMEJobMinDelay, iCloudHMEJobMaxDelay)
		}
	}
}

func TestRandomAutomationCreateDelayStaysInsideSafetyWindow(t *testing.T) {
	for stage, target := range automationProbeRanges {
		for i := 0; i < 200; i++ {
			delay := randomAutomationCreateDelay(stage)
			if delay < target.min || delay > target.max {
				t.Fatalf("stage %d delay = %v, want %v..%v", stage, delay, target.min, target.max)
			}
		}
	}
}

func TestAutomationProbeAdvancesAfterThreeSuccesses(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	state := store.ICloudHMEProbeState{Stage: 0, SuccessTarget: 3, StableStage: -1}
	for index := 0; index < 3; index++ {
		transition := nextAutomationProbeSuccess(state, now.Add(time.Duration(index)*10*time.Minute))
		state.Stage = transition.Stage
		state.SuccessStreak = transition.SuccessStreak
		state.SuccessTarget = transition.SuccessTarget
		state.StableStage = transition.StableStage
		state.RecoveryMode = transition.RecoveryMode
	}
	if state.Stage != 1 || state.SuccessStreak != 0 || state.SuccessTarget != 3 {
		t.Fatalf("state after three successes = %+v, want stage 1 reset for next probe", state)
	}
	for index := 0; index < 3; index++ {
		transition := nextAutomationProbeSuccess(state, now.Add(time.Hour+time.Duration(index)*7*time.Minute))
		state.Stage = transition.Stage
		state.SuccessStreak = transition.SuccessStreak
		state.SuccessTarget = transition.SuccessTarget
		state.StableStage = transition.StableStage
		state.RecoveryMode = transition.RecoveryMode
	}
	if state.Stage != 2 || state.SuccessStreak != 0 || state.SuccessTarget != 5 {
		t.Fatalf("state after second stage = %+v, want stage 2 with five-success target", state)
	}
}

func TestAutomationProbeRecoveryBacksOffAndRequiresFiveSuccesses(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	limitedAt := now.Add(-30 * time.Minute)
	lastCreated := now.Add(-45 * time.Minute)
	state := store.ICloudHMEProbeState{
		Stage: 2, SuccessTarget: 5, StableStage: 0, LimitStartedAt: &limitedAt,
		LastCreatedAt: &lastCreated, LastLimitStage: 2,
	}
	recovered := nextAutomationProbeSuccess(state, now)
	if recovered.Stage != 1 || !recovered.RecoveryMode || recovered.SuccessTarget != 5 ||
		recovered.SuccessStreak != 0 || recovered.RecoverySeconds != int((30*time.Minute)/time.Second) {
		t.Fatalf("recovery transition = %+v", recovered)
	}

	state = store.ICloudHMEProbeState{
		Stage: recovered.Stage, SuccessTarget: recovered.SuccessTarget,
		StableStage: recovered.StableStage, RecoveryMode: recovered.RecoveryMode,
	}
	for index := 1; index <= 5; index++ {
		transition := nextAutomationProbeSuccess(state, now.Add(time.Duration(index)*7*time.Minute))
		state.Stage = transition.Stage
		state.SuccessStreak = transition.SuccessStreak
		state.SuccessTarget = transition.SuccessTarget
		state.StableStage = transition.StableStage
		state.RecoveryMode = transition.RecoveryMode
	}
	if state.Stage != 2 || state.StableStage != 1 || state.RecoveryMode {
		t.Fatalf("state after recovery proof = %+v, want stable stage 1 and retry stage 2", state)
	}
}

func TestAutomationRetryClassification(t *testing.T) {
	tests := []struct {
		message string
		want    string
	}{
		{"You have reached the limit of addresses you can create right now. Please try again later.", "rate_limit"},
		{"HTTP 401", "session"},
		{"未开通 iCloud+", "icloud_plus"},
		{"request timeout", "network"},
		{"unexpected reserve payload", "protocol"},
	}
	for _, test := range tests {
		if got := classifyAutomationRetry(assertError(test.message)); got != test.want {
			t.Fatalf("classifyAutomationRetry(%q) = %q, want %q", test.message, got, test.want)
		}
	}
}

func TestNetworkRetryDelay(t *testing.T) {
	if networkRetryDelay(1) != 15*time.Minute || networkRetryDelay(2) != time.Hour || networkRetryDelay(3) != 6*time.Hour {
		t.Fatal("network retry schedule changed")
	}
}

func TestAutomatedFixedJobStillUsesAutomationSourceSelection(t *testing.T) {
	sourceID := int64(7)
	manual := store.ICloudHMECreateJob{
		Mode: store.ICloudHMEJobModeFixed, SourceAccountID: &sourceID, Origin: "manual",
	}
	automated := manual
	automated.Origin = "automation"
	if !usesFixedICloudHMESource(manual) {
		t.Fatal("manual fixed job should use its configured source")
	}
	if usesFixedICloudHMESource(automated) {
		t.Fatal("automated job must pass through cooldown-aware source selection")
	}
}

func TestExtractICloudHMEVerificationCode(t *testing.T) {
	tests := []struct {
		name    string
		message imapmail.MessageDetail
		want    string
	}{
		{name: "labelled digit", message: imapmail.MessageDetail{Content: "Your verification code is 482913"}, want: "482913"},
		{name: "html digit", message: imapmail.MessageDetail{Content: "<strong>验证码：7712</strong>"}, want: "7712"},
		{name: "subject fallback", message: imapmail.MessageDetail{Subject: "OTP 456789"}, want: "456789"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := extractICloudHMEVerificationCode(test.message); got != test.want {
				t.Fatalf("code = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDeleteAppleAliasRequiresExactConfirmationBeforeStoreLookup(t *testing.T) {
	payload, err := json.Marshal(map[string]string{"confirmEmail": "other@icloud.com"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/icloud-hme/aliases/alias%40icloud.com/delete-apple", bytes.NewReader(payload))
	(&iCloudHMEAPI{}).deleteAppleAlias(recorder, request, "alias@icloud.com")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if !strings.Contains(recorder.Body.String(), "confirmation_mismatch") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestICloudHMELoginChallengeExpiry(t *testing.T) {
	challenge := iCloudHMELoginChallenge{ExpiresAt: time.Now().Add(-time.Second)}
	if !time.Now().After(challenge.ExpiresAt) {
		t.Fatal("challenge should be expired")
	}
}

func TestCreateICloudHMEJobRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	database := &store.Store{}
	if _, err := database.CreateICloudHMEJob(t.Context(), store.ICloudHMECreateJobInput{Mode: store.ICloudHMEJobModePool, Count: 21}); err == nil {
		t.Fatal("count above 20 should be rejected")
	}
	if _, err := database.CreateICloudHMEJob(t.Context(), store.ICloudHMECreateJobInput{Mode: "unsupported", Count: 1}); err == nil {
		t.Fatal("unsupported mode should be rejected")
	}
	if _, err := database.CreateICloudHMEJob(t.Context(), store.ICloudHMECreateJobInput{Mode: store.ICloudHMEJobModeFixed, Count: 1}); err == nil {
		t.Fatal("fixed mode without a source should be rejected")
	}
}

func TestLifecycleAliasesRejectsUnsupportedActionBeforeStoreLookup(t *testing.T) {
	payload, err := json.Marshal(map[string]any{"emails": []string{"alias@icloud.com"}, "action": "delete"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/icloud-hme/aliases/lifecycle", bytes.NewReader(payload))
	(&iCloudHMEAPI{}).lifecycleAliases(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestLoginCompleteKeepsChallengeWhenOTPIsEmpty(t *testing.T) {
	api := &iCloudHMEAPI{}
	challenge := &iCloudHMELoginChallenge{ID: "challenge", SourceID: 7, ExpiresAt: time.Now().Add(time.Minute)}
	api.challenges.Store(challenge.ID, challenge)
	payload, _ := json.Marshal(map[string]string{"challengeId": challenge.ID, "otp": ""})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/icloud-hme/source-accounts/7/login/complete", bytes.NewReader(payload))
	api.loginComplete(recorder, request, 7)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if _, ok := api.challenges.Load(challenge.ID); !ok {
		t.Fatal("empty OTP must not consume the login challenge")
	}
}

func TestLoginCompleteRemovesExpiredChallenge(t *testing.T) {
	api := &iCloudHMEAPI{}
	challenge := &iCloudHMELoginChallenge{ID: "expired", SourceID: 7, ExpiresAt: time.Now().Add(-time.Second)}
	api.challenges.Store(challenge.ID, challenge)
	payload, _ := json.Marshal(map[string]string{"challengeId": challenge.ID, "otp": "123456"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/icloud-hme/source-accounts/7/login/complete", bytes.NewReader(payload))
	api.loginComplete(recorder, request, 7)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if _, ok := api.challenges.Load(challenge.ID); ok {
		t.Fatal("expired challenge should be removed")
	}
}

func TestUniqueICloudHMEEmailsNormalizesAndDeduplicates(t *testing.T) {
	got := uniqueICloudHMEEmails([]string{" Alias@iCloud.com ", "alias@icloud.com", "second@icloud.com", ""})
	if len(got) != 2 || got[0] != "alias@icloud.com" || got[1] != "second@icloud.com" {
		t.Fatalf("emails = %#v", got)
	}
}

func TestICloudHMEJobRunningStatuses(t *testing.T) {
	for _, status := range []string{"pending", "running", "cancel_requested"} {
		if !isRunningICloudHMEJob(status) {
			t.Fatalf("status %q should be running", status)
		}
	}
	for _, status := range []string{"completed", "partial_failed", "cancelled"} {
		if isRunningICloudHMEJob(status) {
			t.Fatalf("status %q should be terminal", status)
		}
	}
}
