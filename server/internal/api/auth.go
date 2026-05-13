package api

import (
	"net/http"
	"strings"

	"mailbox-server/internal/session"
	"mailbox-server/internal/store"
)

type authAPI struct {
	store    *store.Store
	sessions session.Manager
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authUserResponse struct {
	OK       bool   `json:"ok"`
	Username string `json:"username"`
}

func (api authAPI) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	username := strings.TrimSpace(req.Username)
	password := req.Password
	if username == "" || password == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "用户名和密码不能为空")
		return
	}
	if err := api.store.ValidateAdmin(r.Context(), username, password); err != nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	api.sessions.Set(w, username)
	WriteJSON(w, http.StatusOK, authUserResponse{OK: true, Username: username})
}

func (api authAPI) logout(w http.ResponseWriter, r *http.Request) {
	api.sessions.Clear(w)
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (api authAPI) me(w http.ResponseWriter, r *http.Request) {
	username, ok := api.sessions.Username(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	WriteJSON(w, http.StatusOK, authUserResponse{OK: true, Username: username})
}
