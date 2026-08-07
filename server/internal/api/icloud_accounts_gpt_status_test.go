package api

import (
	"context"
	"testing"
	"time"

	"gptbox-server/internal/store"
)

type fakeICloudGPTFetcher struct {
	details   map[int64]*iCloudLatestEmail
	requested []int64
}

func (fetcher *fakeICloudGPTFetcher) Messages(context.Context, string, string) (iCloudMailListResponse, error) {
	return iCloudMailListResponse{OK: true}, nil
}

func (fetcher *fakeICloudGPTFetcher) Message(_ context.Context, _ string, _ string, id int64) (iCloudMailDetailResponse, error) {
	fetcher.requested = append(fetcher.requested, id)
	return iCloudMailDetailResponse{OK: true, Email: fetcher.details[id]}, nil
}

func (fetcher *fakeICloudGPTFetcher) Latest(context.Context, string, string) (iCloudLatestMailResponse, error) {
	return iCloudLatestMailResponse{OK: true}, nil
}

func TestClassifyICloudGPTMailUsesStrictLifecycleRules(t *testing.T) {
	plusBody := `<p>You've successfully subscribed to ChatGPT Plus.</p><p>ChatGPT Plus Subscription</p>`
	if got := classifyICloudGPTMail("ChatGPT - Your new plan", plusBody); got != iCloudHMEGPTMailPlus {
		t.Fatalf("plus classification = %q", got)
	}
	if got := classifyICloudGPTMail("ChatGPT - Your new plan", "Your payment receipt"); got != iCloudHMEGPTMailUnknown {
		t.Fatalf("incomplete plus classification = %q", got)
	}

	deactivatedBody := "Your account has been deactivated and can no longer be used."
	if got := classifyICloudGPTMail("OpenAI - Access Deactivated [C-test]", deactivatedBody); got != iCloudHMEGPTMailDeactivated {
		t.Fatalf("deactivated classification = %q", got)
	}
	if got := classifyICloudGPTMail("OpenAI API - Access Deactivated [C-test]", deactivatedBody); got != iCloudHMEGPTMailUnknown {
		t.Fatalf("API deactivation classification = %q", got)
	}
}

func TestICloudGPTCandidateSubject(t *testing.T) {
	for _, subject := range []string{"ChatGPT - Your new plan", "OpenAI - Access Deactivated [C-test]"} {
		if !isICloudGPTCandidateSubject(subject) {
			t.Fatalf("subject %q should be a candidate", subject)
		}
	}
	if isICloudGPTCandidateSubject("OpenAI API - Access Deactivated") {
		t.Fatal("API deactivation must not be a candidate")
	}
}

func TestLoadICloudGPTPlanMailsDoesNotReadDeactivationBody(t *testing.T) {
	plusBody := "You've successfully subscribed to ChatGPT Plus. ChatGPT Plus Subscription"
	fetcher := &fakeICloudGPTFetcher{details: map[int64]*iCloudLatestEmail{
		1: {ID: 1, Subject: "ChatGPT - Your new plan", Text: plusBody, ReceivedAt: "2026-08-07T09:30:00+08:00"},
		2: {ID: 2, Subject: "OpenAI - Access Deactivated [C-test]", Text: "Your account has been deactivated and can no longer be used.", ReceivedAt: "2026-08-07T10:30:00+08:00"},
	}}
	api := &iCloudAPI{latestFetch: fetcher}
	mails, err := api.loadICloudGPTStatusMails(t.Context(), store.ICloudGPTScanTarget{Email: "alias@icloud.com", Key: "key"}, []iCloudMailSummary{
		{ID: 1, Subject: "ChatGPT - Your new plan"},
		{ID: 2, Subject: "OpenAI - Access Deactivated [C-test]"},
	}, iCloudHMEGPTMailPlus)
	if err != nil {
		t.Fatal(err)
	}
	if len(mails) != 1 || mails[0].id != 1 {
		t.Fatalf("mails = %#v", mails)
	}
	if len(fetcher.requested) != 1 || fetcher.requested[0] != 1 {
		t.Fatalf("requested details = %#v, want only plan message", fetcher.requested)
	}
}

func TestParseICloudGPTMailTime(t *testing.T) {
	want := time.Date(2026, 8, 7, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	got := parseICloudGPTMailTime("2026-08-07T09:30:00+08:00")
	if !got.Equal(want) {
		t.Fatalf("parsed time = %v, want %v", got, want)
	}
	if !parseICloudGPTMailTime("invalid").IsZero() {
		t.Fatal("invalid time should return zero")
	}
}
