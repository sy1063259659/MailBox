package api

import (
	"testing"

	"gptbox-server/internal/imapmail"
)

func TestClassifyICloudHMEGPTPlusRequiresSubjectAndBody(t *testing.T) {
	message := imapmail.MessageDetail{
		Subject: "ChatGPT - Your new plan",
		Content: `<p>You've successfully subscribed to ChatGPT Plus.</p>
		          <span>ChatGPT Plus Subscription</span>`,
	}
	if got := classifyICloudHMEGPTMail(message); got != iCloudHMEGPTMailPlus {
		t.Fatalf("classification = %q, want plus", got)
	}
	message.Subject = "Your ChatGPT receipt"
	if got := classifyICloudHMEGPTMail(message); got != iCloudHMEGPTMailUnknown {
		t.Fatalf("receipt classification = %q, want unknown", got)
	}
}

func TestClassifyICloudHMEGPTDeactivatedIgnoresAPIDeactivation(t *testing.T) {
	body := `<p>Your account has been deactivated because recent activity violated our policies.</p>
	         <p>This means your account can no longer be used.</p>`
	message := imapmail.MessageDetail{
		Subject: "OpenAI - Access Deactivated\n\n[C-hhwWxQX28GFM]",
		Content: body,
	}
	if got := classifyICloudHMEGPTMail(message); got != iCloudHMEGPTMailDeactivated {
		t.Fatalf("classification = %q, want deactivated", got)
	}
	message.Subject = "OpenAI API - Access Deactivated [C-NSSwBb7bxmmN]"
	if got := classifyICloudHMEGPTMail(message); got != iCloudHMEGPTMailUnknown {
		t.Fatalf("API deactivation classification = %q, want unknown", got)
	}
}

func TestClassifyICloudHMEGPTDeactivatedRequiresBodyConfirmation(t *testing.T) {
	message := imapmail.MessageDetail{
		Subject: "OpenAI - Access Deactivated [C-test]",
		Content: "This is a test message without the deactivation explanation.",
	}
	if got := classifyICloudHMEGPTMail(message); got != iCloudHMEGPTMailUnknown {
		t.Fatalf("classification = %q, want unknown", got)
	}
}
