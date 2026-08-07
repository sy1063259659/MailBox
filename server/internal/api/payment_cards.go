package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"gptbox-server/internal/store"
)

type paymentCardAPI struct{ store *store.Store }

func (api paymentCardAPI) cards(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cards, err := api.store.ListPaymentCards(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "card_list_failed", "加载支付卡失败")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "cards": cards})
	case http.MethodPost:
		var request struct {
			Text string `json:"text"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		cards, parseErrors, err := api.store.ImportPaymentCards(r.Context(), request.Text)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "card_import_failed", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "cards": cards, "errors": parseErrors})
	default:
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (api paymentCardAPI) card(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/payment-cards/"), "/")
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "卡 ID 格式错误")
		return
	}
	if len(parts) == 2 && parts[1] == "reset" && r.Method == http.MethodPost {
		if err := api.store.SetPaymentCardStatus(r.Context(), id, "active", ""); err != nil {
			writeCardConflict(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var request struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if err := api.store.SetPaymentCardStatus(r.Context(), id, request.Status, request.Reason); err != nil {
			writeCardConflict(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	case http.MethodDelete:
		if err := api.store.DeletePaymentCard(r.Context(), id); err != nil {
			writeCardConflict(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func writeCardConflict(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrIntegrationResourceUnavailable) {
		WriteError(w, http.StatusConflict, "resource_busy", "支付卡正在使用、已关联邮箱或不存在")
		return
	}
	WriteError(w, http.StatusBadRequest, "card_operation_failed", err.Error())
}

func (api paymentCardAPI) cardLink(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email  string `json:"email"`
		CardID int64  `json:"cardId"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	switch r.Method {
	case http.MethodPost:
		if err := api.store.LinkPaymentCard(r.Context(), request.Email, request.CardID, "manual"); err != nil {
			writeCardConflict(w, err)
			return
		}
	case http.MethodDelete:
		if err := api.store.UnlinkPaymentCard(r.Context(), request.Email, "manual_unlink"); err != nil {
			writeCardConflict(w, err)
			return
		}
	default:
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
