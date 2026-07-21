package api

import (
	"html"
	"net/http"
	"regexp"
	"strings"

	"gptbox-server/internal/imapmail"
)

type iCloudHMEMailMessagesRequest struct {
	Email  string
	Limit  int
	Cursor string
}

type iCloudHMEMailMessageRequest struct {
	Email string
	UID   string
}

var (
	iCloudHMEDigitCodeRE  = regexp.MustCompile(`(?i)(?:verification\s*code|security\s*code|passcode|code|验证码|otp)[^0-9]{0,24}([0-9]{4,8})`)
	iCloudHMEPlainDigitRE = regexp.MustCompile(`\b([0-9]{4,8})\b`)
	iCloudHMEAlphaCodeRE  = regexp.MustCompile(`\b([A-Z0-9]{4,8})\b`)
	iCloudHMEHTMLTagRE    = regexp.MustCompile(`(?is)<[^>]*>`)
)

func (api *iCloudHMEAPI) mailMessages(w http.ResponseWriter, r *http.Request) {
	var req iCloudHMEMailMessagesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	credentials, err := api.store.GetICloudHMEMailCredentials(r.Context(), req.Email)
	if err != nil {
		writeICloudHMEMailError(w, err)
		return
	}
	result, err := api.mailClient.ListMessagesByRecipient(
		r.Context(), credentials.ICloudEmail, credentials.AppPassword,
		credentials.AliasEmail, req.Limit, req.Cursor,
	)
	if err != nil {
		writeICloudHMEMailError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "messages": result.Messages, "nextCursor": result.NextCursor})
}

func (api *iCloudHMEAPI) mailMessage(w http.ResponseWriter, r *http.Request) {
	var req iCloudHMEMailMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	credentials, err := api.store.GetICloudHMEMailCredentials(r.Context(), req.Email)
	if err != nil {
		writeICloudHMEMailError(w, err)
		return
	}
	message, err := api.mailClient.GetMessageByRecipient(
		r.Context(), credentials.ICloudEmail, credentials.AppPassword, credentials.AliasEmail, req.UID,
	)
	if err != nil {
		writeICloudHMEMailError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "message": message, "verificationCode": extractICloudHMEVerificationCode(message),
	})
}

func (api *iCloudHMEAPI) mailCode(w http.ResponseWriter, r *http.Request) {
	var req iCloudHMEMailMessagesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	credentials, err := api.store.GetICloudHMEMailCredentials(r.Context(), req.Email)
	if err != nil {
		writeICloudHMEMailError(w, err)
		return
	}
	result, err := api.mailClient.ListMessagesByRecipient(
		r.Context(), credentials.ICloudEmail, credentials.AppPassword, credentials.AliasEmail, 5, "",
	)
	if err != nil {
		writeICloudHMEMailError(w, err)
		return
	}
	for _, summary := range result.Messages {
		message, err := api.mailClient.GetMessageByRecipient(
			r.Context(), credentials.ICloudEmail, credentials.AppPassword, credentials.AliasEmail, summary.ID,
		)
		if err != nil {
			continue
		}
		if code := extractICloudHMEVerificationCode(message); code != "" {
			WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "code": code, "message": message})
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "code": "", "message": nil})
}

func writeICloudHMEMailError(w http.ResponseWriter, err error) {
	code, status := "icloud_mail_error", http.StatusBadGateway
	message := friendlyICloudHMEError(err)
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "app password"):
		code, status = "icloud_app_password_required", http.StatusBadRequest
	case strings.Contains(lower, "login") || strings.Contains(lower, "authentication"):
		code, status = "icloud_app_password_invalid", http.StatusBadRequest
	case strings.Contains(lower, "not found") || strings.Contains(lower, "icloud_mail_not_found"):
		code, status = "icloud_mail_not_found", http.StatusNotFound
	}
	WriteError(w, status, code, message)
}

func extractICloudHMEVerificationCode(message imapmail.MessageDetail) string {
	value := html.UnescapeString(message.Subject + " " + iCloudHMEHTMLTagRE.ReplaceAllString(message.Content, " "))
	if match := iCloudHMEDigitCodeRE.FindStringSubmatch(value); len(match) > 1 {
		return strings.ToUpper(match[1])
	}
	if match := iCloudHMEPlainDigitRE.FindStringSubmatch(value); len(match) > 1 {
		return match[1]
	}
	for _, match := range iCloudHMEAlphaCodeRE.FindAllStringSubmatch(strings.ToUpper(value), -1) {
		if len(match) > 1 && strings.IndexFunc(match[1], func(char rune) bool { return char >= '0' && char <= '9' }) >= 0 {
			return match[1]
		}
	}
	return ""
}
