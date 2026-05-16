package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"mailbox-server/internal/imapmail"
	"mailbox-server/internal/oauth"
	"mailbox-server/internal/store"
)

const requestTimeout = 90 * time.Second

type mailAPI struct {
	oauth oauth.Client
	imap  imapmail.Client
	store *store.Store
}

type accountRequest struct {
	Email string `json:"email"`
}

type folderRequest struct {
	accountRequest
}

type messagesRequest struct {
	accountRequest
	Folder string `json:"folder"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

type messageRequest struct {
	accountRequest
	Folder    string `json:"folder"`
	MessageID string `json:"messageId"`
	UID       string `json:"uid"`
}

type checkResponse struct {
	OK    bool   `json:"ok"`
	Email string `json:"email"`
}

type foldersResponse struct {
	OK      bool               `json:"ok"`
	Folders imapmail.FolderMap `json:"folders"`
}

type messagesResponse struct {
	OK         bool                      `json:"ok"`
	Folder     string                    `json:"folder"`
	Messages   []imapmail.MessageSummary `json:"messages"`
	NextCursor string                    `json:"nextCursor,omitempty"`
}

type messageResponse struct {
	OK      bool                   `json:"ok"`
	Message imapmail.MessageDetail `json:"message"`
	Body    messageBody            `json:"body"`
}

type messageBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

func newMailAPI(store *store.Store) mailAPI {
	return mailAPI{
		oauth: oauth.Client{},
		imap:  imapmail.Client{},
		store: store,
	}
}

func (api mailAPI) check(w http.ResponseWriter, r *http.Request) {
	var req accountRequest
	if !decodeJSON(w, r, &req) || !validateAccountRequest(w, req) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	credentials, token, ok := api.refreshToken(w, ctx, req)
	if !ok {
		return
	}
	_ = api.store.UpdateRefreshToken(ctx, credentials.Email, token.RefreshToken)
	_ = api.store.UpdateAccountStatus(ctx, credentials.Email, "success", "", false)

	WriteJSON(w, http.StatusOK, checkResponse{
		OK:    true,
		Email: strings.TrimSpace(credentials.Email),
	})
}

func (api mailAPI) folders(w http.ResponseWriter, r *http.Request) {
	var req folderRequest
	if !decodeJSON(w, r, &req) || !validateAccountRequest(w, req.accountRequest) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	credentials, token, ok := api.refreshToken(w, ctx, req.accountRequest)
	if !ok {
		return
	}
	_ = api.store.UpdateRefreshToken(ctx, credentials.Email, token.RefreshToken)
	folders, err := api.imap.ListFolders(ctx, credentials.AuthEmail, token.AccessToken)
	if err != nil {
		_ = api.store.UpdateAccountStatus(ctx, credentials.Email, statusForServiceError(err), err.Error(), false)
		writeServiceError(w, err)
		return
	}
	_ = api.store.UpdateAccountStatus(ctx, credentials.Email, "success", "", false)

	WriteJSON(w, http.StatusOK, foldersResponse{
		OK:      true,
		Folders: folders,
	})
}

func (api mailAPI) messages(w http.ResponseWriter, r *http.Request) {
	var req messagesRequest
	if !decodeJSON(w, r, &req) || !validateAccountRequest(w, req.accountRequest) {
		return
	}
	folder := normalizeFolder(req.Folder)
	if folder == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "folder must be inbox or junkemail")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	credentials, token, ok := api.refreshToken(w, ctx, req.accountRequest)
	if !ok {
		return
	}
	_ = api.store.UpdateRefreshToken(ctx, credentials.Email, token.RefreshToken)
	result, err := api.imap.ListMessages(ctx, credentials.AuthEmail, token.AccessToken, folder, req.Limit, req.Cursor)
	if err != nil {
		_ = api.store.UpdateAccountStatus(ctx, credentials.Email, statusForServiceError(err), err.Error(), false)
		writeServiceError(w, err)
		return
	}
	_ = api.store.UpdateAccountStatus(ctx, credentials.Email, "success", "", true)

	WriteJSON(w, http.StatusOK, messagesResponse{
		OK:         true,
		Folder:     folder,
		Messages:   result.Messages,
		NextCursor: result.NextCursor,
	})
}

func (api mailAPI) message(w http.ResponseWriter, r *http.Request) {
	var req messageRequest
	if !decodeJSON(w, r, &req) || !validateAccountRequest(w, req.accountRequest) {
		return
	}
	folder := normalizeFolder(req.Folder)
	if folder == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "folder must be inbox or junkemail")
		return
	}
	uid := strings.TrimSpace(req.UID)
	if uid == "" {
		uid = strings.TrimSpace(req.MessageID)
	}
	if uid == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "messageId is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	credentials, token, ok := api.refreshToken(w, ctx, req.accountRequest)
	if !ok {
		return
	}
	_ = api.store.UpdateRefreshToken(ctx, credentials.Email, token.RefreshToken)
	detail, err := api.imap.GetMessage(ctx, credentials.AuthEmail, token.AccessToken, folder, uid)
	if err != nil {
		_ = api.store.UpdateAccountStatus(ctx, credentials.Email, statusForServiceError(err), err.Error(), false)
		writeServiceError(w, err)
		return
	}
	_ = api.store.UpdateAccountStatus(ctx, credentials.Email, "success", "", false)

	WriteJSON(w, http.StatusOK, messageResponse{
		OK:      true,
		Message: detail,
		Body: messageBody{
			ContentType: detail.ContentType,
			Content:     detail.Content,
		},
	})
}

func (api mailAPI) refreshToken(w http.ResponseWriter, ctx context.Context, req accountRequest) (store.AccountCredentials, oauth.TokenResult, bool) {
	credentials, err := api.store.GetCredentials(ctx, req.Email)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return store.AccountCredentials{}, oauth.TokenResult{}, false
	}
	token, err := api.oauth.Refresh(ctx, credentials.ClientID, credentials.RefreshToken)
	if err != nil {
		_ = api.store.UpdateAccountStatus(ctx, credentials.Email, "token_expired", err.Error(), false)
		WriteError(w, http.StatusUnauthorized, "oauth_error", err.Error())
		return store.AccountCredentials{}, oauth.TokenResult{}, false
	}
	return credentials, token, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	if r.Body == nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "request body is required")
		return false
	}
	defer r.Body.Close()
	if err := readJSON(r, target); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}

func validateAccountRequest(w http.ResponseWriter, req accountRequest) bool {
	if strings.TrimSpace(req.Email) == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "email is required")
		return false
	}
	return true
}

func normalizeFolder(folder string) string {
	switch strings.ToLower(strings.TrimSpace(folder)) {
	case "", "inbox":
		return "inbox"
	case "junk", "junkemail":
		return "junkemail"
	default:
		return ""
	}
}

func writeServiceError(w http.ResponseWriter, err error) {
	message := err.Error()
	status := http.StatusBadGateway
	code := "imap_error"
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(message, "context deadline exceeded") {
		status = http.StatusGatewayTimeout
		code = "imap_timeout"
	}
	if strings.Contains(strings.ToLower(message), "authenticate") || strings.Contains(strings.ToLower(message), "auth") {
		status = http.StatusUnauthorized
		code = "imap_auth_error"
	}
	WriteError(w, status, code, message)
}

func statusForServiceError(err error) string {
	message := strings.ToLower(err.Error())
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(message, "context deadline exceeded") {
		return "error"
	}
	if strings.Contains(message, "authenticate") || strings.Contains(message, "auth") {
		return "token_expired"
	}
	return "error"
}
