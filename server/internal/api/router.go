package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"gptbox-server/internal/session"
	"gptbox-server/internal/store"
)

var allowedOrigins = map[string]struct{}{
	"http://127.0.0.1:5173": {},
	"http://localhost:5173": {},
}

func NewRouter(store *store.Store, sessions session.Manager, integrationKeys ...string) http.Handler {
	mux := http.NewServeMux()
	authAPI := authAPI{store: store, sessions: sessions}
	accountAPI := accountAPI{store: store}
	iCloudAPI := newICloudAPI(store)
	iCloudHMEAPI := newICloudHMEAPI(store)
	smsAPI := newSMSAPI(store)
	cardAPI := paymentCardAPI{store: store}
	integrationKey := ""
	if len(integrationKeys) > 0 {
		integrationKey = strings.TrimSpace(integrationKeys[0])
	}
	integration := integrationAPI{store: store, hme: iCloudHMEAPI, sms: smsAPI, key: integrationKey}
	mailAPI := newMailAPI(store)
	mux.HandleFunc("/api/health", methodHandler(http.MethodGet, healthHandler))

	mux.HandleFunc("/api/auth/login", methodHandler(http.MethodPost, authAPI.login))
	mux.HandleFunc("/api/auth/logout", methodHandler(http.MethodPost, authAPI.logout))
	mux.HandleFunc("/api/auth/me", methodHandler(http.MethodGet, authAPI.me))
	mux.HandleFunc("/api/public/icloud-hme/mail/latest", publicMailMethodHandler(http.MethodGet, iCloudHMEAPI.publicLatestMail))
	mux.HandleFunc("/api/public/icloud-hme/mail/history", publicMailMethodHandler(http.MethodGet, iCloudHMEAPI.publicMailHistory))

	mux.HandleFunc("/api/accounts", authRequired(sessions, methodHandler(http.MethodGet, accountAPI.listAccounts)))
	mux.HandleFunc("/api/accounts/import", authRequired(sessions, methodHandler(http.MethodPost, accountAPI.importAccounts)))
	mux.HandleFunc("/api/accounts/move-group", authRequired(sessions, methodHandler(http.MethodPost, accountAPI.moveAccounts)))
	mux.HandleFunc("/api/accounts/export", authRequired(sessions, exportAccountsHandler(accountAPI)))
	mux.HandleFunc("/api/accounts/remark", authRequired(sessions, methodHandler(http.MethodPatch, accountAPI.updateAccountRemark)))
	mux.HandleFunc("/api/accounts/", authRequired(sessions, accountPathHandler(accountAPI)))
	mux.HandleFunc("/api/groups", authRequired(sessions, groupsHandler(accountAPI)))
	mux.HandleFunc("/api/groups/", authRequired(sessions, groupIDHandler(accountAPI)))

	mux.HandleFunc("/api/icloud-accounts", authRequired(sessions, methodHandler(http.MethodGet, iCloudAPI.listAccounts)))
	mux.HandleFunc("/api/icloud-accounts/import", authRequired(sessions, methodHandler(http.MethodPost, iCloudAPI.importAccounts)))
	mux.HandleFunc("/api/icloud-accounts/latest", authRequired(sessions, methodHandler(http.MethodPost, iCloudAPI.latestMail)))
	mux.HandleFunc("/api/icloud-accounts/messages", authRequired(sessions, methodHandler(http.MethodPost, iCloudAPI.listMail)))
	mux.HandleFunc("/api/icloud-accounts/message", authRequired(sessions, methodHandler(http.MethodPost, iCloudAPI.mailDetail)))
	mux.HandleFunc("/api/icloud-accounts/gpt-status/scan", authRequired(sessions, methodHandler(http.MethodPost, iCloudAPI.scanGPTStatus)))
	mux.HandleFunc("/api/icloud-accounts/remark", authRequired(sessions, methodHandler(http.MethodPatch, iCloudAPI.updateRemark)))
	mux.HandleFunc("/api/icloud-accounts/move-group", authRequired(sessions, methodHandler(http.MethodPost, iCloudAPI.moveAccounts)))
	mux.HandleFunc("/api/icloud-accounts/", authRequired(sessions, iCloudAccountPathHandler(iCloudAPI)))
	mux.HandleFunc("/api/icloud-groups", authRequired(sessions, iCloudGroupsHandler(iCloudAPI)))
	mux.HandleFunc("/api/icloud-groups/", authRequired(sessions, iCloudGroupIDHandler(iCloudAPI)))

	mux.HandleFunc("/api/sms-accounts", authRequired(sessions, methodHandler(http.MethodGet, smsAPI.listAccounts)))
	mux.HandleFunc("/api/sms-accounts/import", authRequired(sessions, methodHandler(http.MethodPost, smsAPI.importAccounts)))
	mux.HandleFunc("/api/sms-accounts/remark", authRequired(sessions, methodHandler(http.MethodPatch, smsAPI.updateRemark)))
	mux.HandleFunc("/api/sms-accounts/status", authRequired(sessions, methodHandler(http.MethodPatch, smsAPI.updateStatus)))
	mux.HandleFunc("/api/sms-accounts/binding", authRequired(sessions, methodHandler(http.MethodPatch, smsAPI.bindMailbox)))
	mux.HandleFunc("/api/sms-accounts/mailbox-binding", authRequired(sessions, methodHandler(http.MethodPatch, smsAPI.assignMailbox)))
	mux.HandleFunc("/api/sms-accounts/mailboxes", authRequired(sessions, methodHandler(http.MethodGet, smsAPI.listMailboxes)))
	mux.HandleFunc("/api/sms-accounts/latest", authRequired(sessions, methodHandler(http.MethodPost, smsAPI.latestMessage)))
	mux.HandleFunc("/api/sms-accounts/", authRequired(sessions, smsAPI.routeAccount))
	mux.HandleFunc("/api/payment-cards", authRequired(sessions, cardAPI.cards))
	mux.HandleFunc("/api/payment-cards/", authRequired(sessions, cardAPI.card))
	mux.HandleFunc("/api/icloud-hme/card-link", authRequired(sessions, cardAPI.cardLink))
	mux.HandleFunc("/api/integration/v1/groups", integrationAuthRequired(integrationKey, methodHandler(http.MethodGet, integration.groups)))
	mux.HandleFunc("/api/integration/v1/resources/acquire", integrationAuthRequired(integrationKey, methodHandler(http.MethodPost, integration.acquire)))
	mux.HandleFunc("/api/integration/v1/queues/", integrationAuthRequired(integrationKey, integration.queueLease))
	mux.HandleFunc("/api/integration/v1/mail/latest", integrationAuthRequired(integrationKey, methodHandler(http.MethodGet, integration.latestMail)))
	mux.HandleFunc("/api/integration/v1/cards/credentials", integrationAuthRequired(integrationKey, methodHandler(http.MethodGet, integration.cardCredentials)))
	mux.HandleFunc("/api/integration/v1/sms/acquire", integrationAuthRequired(integrationKey, methodHandler(http.MethodPost, integration.acquireSMS)))
	mux.HandleFunc("/api/integration/v1/sms/latest", integrationAuthRequired(integrationKey, methodHandler(http.MethodGet, integration.latestSMS)))
	mux.HandleFunc("/api/integration/v1/results/auth", integrationAuthRequired(integrationKey, methodHandler(http.MethodPost, integration.authResult)))
	mux.HandleFunc("/api/integration/v1/results/payment", integrationAuthRequired(integrationKey, methodHandler(http.MethodPost, integration.paymentResult)))
	mux.HandleFunc("/api/integration/v1/leases", authRequired(sessions, methodHandler(http.MethodGet, integration.activeLeases)))
	mux.HandleFunc("/api/integration/v1/leases/force-release", authRequired(sessions, methodHandler(http.MethodPost, integration.forceRelease)))

	mux.HandleFunc("/api/icloud-hme/source-accounts", authRequired(sessions, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			iCloudHMEAPI.listSourceAccounts(w, r)
		case http.MethodPost:
			iCloudHMEAPI.createSourceAccount(w, r)
		default:
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))
	mux.HandleFunc("/api/icloud-hme/source-accounts/validate-all", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.validateAllSources)))
	mux.HandleFunc("/api/icloud-hme/source-accounts/sync-all", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.syncAllSources)))
	mux.HandleFunc("/api/icloud-hme/source-accounts/", authRequired(sessions, iCloudHMEAPI.routeSourceAccount))
	mux.HandleFunc("/api/icloud-hme/jobs", authRequired(sessions, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			iCloudHMEAPI.listJobs(w, r)
		case http.MethodPost:
			iCloudHMEAPI.createJob(w, r)
		default:
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))
	mux.HandleFunc("/api/icloud-hme/jobs/", authRequired(sessions, iCloudHMEAPI.routeJob))
	mux.HandleFunc("/api/icloud-hme/delete-jobs", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.createDeleteJob)))
	mux.HandleFunc("/api/icloud-hme/delete-jobs/", authRequired(sessions, methodHandler(http.MethodGet, iCloudHMEAPI.getDeleteJob)))
	mux.HandleFunc("/api/icloud-hme/automation", authRequired(sessions, iCloudHMEAPI.automationSettings))
	mux.HandleFunc("/api/icloud-hme/automation/events", authRequired(sessions, methodHandler(http.MethodGet, iCloudHMEAPI.automationEvents)))
	mux.HandleFunc("/api/icloud-hme/gpt-status/scan", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.scanGPTStatus)))
	mux.HandleFunc("/api/icloud-hme/aliases", authRequired(sessions, methodHandler(http.MethodGet, iCloudHMEAPI.listAliases)))
	mux.HandleFunc("/api/icloud-hme/aliases/inventory-status", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.updateInventoryStatus)))
	mux.HandleFunc("/api/icloud-hme/aliases/lifecycle", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.lifecycleAliases)))
	mux.HandleFunc("/api/icloud-hme/aliases/remark", authRequired(sessions, methodHandler(http.MethodPatch, iCloudHMEAPI.updateAliasRemark)))
	mux.HandleFunc("/api/icloud-hme/aliases/move-group", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.moveAliases)))
	mux.HandleFunc("/api/icloud-hme/aliases/receive-keys/generate", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.generateReceiveKeys)))
	mux.HandleFunc("/api/icloud-hme/aliases/receive-keys/export", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.exportReceiveKeys)))
	mux.HandleFunc("/api/icloud-hme/aliases/", authRequired(sessions, iCloudHMEAPI.routeAlias))
	mux.HandleFunc("/api/icloud-hme/mail/latest", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.latestMail)))
	mux.HandleFunc("/api/icloud-hme/mail/messages", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.mailMessages)))
	mux.HandleFunc("/api/icloud-hme/mail/message", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.mailMessage)))
	mux.HandleFunc("/api/icloud-hme/mail/code", authRequired(sessions, methodHandler(http.MethodPost, iCloudHMEAPI.mailCode)))
	mux.HandleFunc("/api/icloud-hme/groups", authRequired(sessions, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			iCloudHMEAPI.listGroups(w, r)
		case http.MethodPost:
			iCloudHMEAPI.createGroup(w, r)
		default:
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))
	mux.HandleFunc("/api/icloud-hme/groups/", authRequired(sessions, iCloudHMEAPI.routeGroup))
	mux.HandleFunc("/api/mail/check", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.check)))
	mux.HandleFunc("/api/mail/folders", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.folders)))
	mux.HandleFunc("/api/mail/messages", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.messages)))
	mux.HandleFunc("/api/mail/message", authRequired(sessions, methodHandler(http.MethodPost, mailAPI.message)))

	return withRequestLogging(withCORS(mux))
}

func iCloudAccountPathHandler(api *iCloudAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			api.deleteAccount(w, r)
			return
		}
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func iCloudGroupsHandler(api *iCloudAPI) http.HandlerFunc {
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

func iCloudGroupIDHandler(api *iCloudAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSuffix(r.URL.Path, "/") == "/api/icloud-groups/order" {
			if r.Method != http.MethodPatch {
				WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
				return
			}
			api.reorderGroups(w, r)
			return
		}
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
func accountPathHandler(api accountAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/split-hotmail") {
			api.splitHotmail(w, r)
			return
		}
		if r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/remark") {
			api.updateAccountRemark(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			api.deleteAccount(w, r)
			return
		}
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
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

type requestActorKey struct{}

func requestActor(r *http.Request) string {
	actor, _ := r.Context().Value(requestActorKey{}).(string)
	return actor
}

func authRequired(sessions session.Manager, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, ok := sessions.Username(r)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "未登录")
			return
		}
		handler(w, r.WithContext(context.WithValue(r.Context(), requestActorKey{}, username)))
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

func exportAccountsHandler(api accountAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			api.exportAccounts(w, r)
		default:
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}
}

func groupIDHandler(api accountAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSuffix(r.URL.Path, "/") == "/api/groups/order" {
			if r.Method != http.MethodPatch {
				WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
				return
			}
			api.reorderGroups(w, r)
			return
		}
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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(body []byte) (int, error) {
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	return recorder.ResponseWriter.Write(body)
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		errorCode := w.Header().Get("X-Mailbox-Error-Code")
		if errorCode == "" {
			errorCode = "-"
		}
		log.Printf(
			"api method=%s path=%s status=%d duration_ms=%d error=%s",
			r.Method,
			r.URL.Path,
			status,
			time.Since(startedAt).Milliseconds(),
			errorCode,
		)
	})
}

type healthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, healthResponse{
		OK:      true,
		Service: "gptbox-server",
	})
}
