package api

import (
	"net/http"

	"mailbox-server/internal/session"
	"mailbox-server/internal/store"
)

var allowedOrigins = map[string]struct{}{
	"http://127.0.0.1:5173": {},
	"http://localhost:5173": {},
}

func NewRouter(store *store.Store, sessions session.Manager) http.Handler {
	mux := http.NewServeMux()
	authAPI := authAPI{store: store, sessions: sessions}
	accountAPI := accountAPI{store: store}
	mailAPI := newMailAPI(store)
	mux.HandleFunc("/api/health", methodHandler(http.MethodGet, healthHandler))

	mux.HandleFunc("/api/auth/login", methodHandler(http.MethodPost, authAPI.login))
	mux.HandleFunc("/api/auth/logout", methodHandler(http.MethodPost, authAPI.logout))
	mux.HandleFunc("/api/auth/me", methodHandler(http.MethodGet, authAPI.me))

	mux.HandleFunc("/api/accounts", authRequired(sessions, methodHandler(http.MethodGet, accountAPI.listAccounts)))
	mux.HandleFunc("/api/accounts/import", authRequired(sessions, methodHandler(http.MethodPost, accountAPI.importAccounts)))
	mux.HandleFunc("/api/accounts/move-group", authRequired(sessions, methodHandler(http.MethodPost, accountAPI.moveAccounts)))
	mux.HandleFunc("/api/accounts/export", authRequired(sessions, methodHandler(http.MethodGet, accountAPI.exportAccounts)))
	mux.HandleFunc("/api/accounts/", authRequired(sessions, methodHandler(http.MethodDelete, accountAPI.deleteAccount)))
	mux.HandleFunc("/api/groups", authRequired(sessions, groupsHandler(accountAPI)))
	mux.HandleFunc("/api/groups/", authRequired(sessions, groupIDHandler(accountAPI)))

	mux.HandleFunc("/api/mail/check", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.check)))
	mux.HandleFunc("/api/mail/folders", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.folders)))
	mux.HandleFunc("/api/mail/messages", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.messages)))
	mux.HandleFunc("/api/mail/message", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.message)))

	return withCORS(mux)
}

func methodHandler(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		handler(w, r)
	}
}

func authRequired(sessions session.Manager, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := sessions.Username(r); !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "未登录")
			return
		}
		handler(w, r)
	}
}

func groupsHandler(api accountAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.listGroups(w, r)
		case http.MethodPost:
			api.createGroup(w, r)
		default:
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}
}

func groupIDHandler(api accountAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			api.updateGroup(w, r)
		case http.MethodDelete:
			api.deleteGroup(w, r)
		default:
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowedOrigins[r.Header.Get("Origin")]; ok {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type healthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, healthResponse{
		OK:      true,
		Service: "mailbox-imap-server",
	})
}
