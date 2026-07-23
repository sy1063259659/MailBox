package api

import (
	"net/http"
	"strconv"

	"gptbox-server/internal/store"
)

type iCloudHMEReceiveKeysRequest struct {
	Emails []string `json:"emails"`
}

func (api *iCloudHMEAPI) generateReceiveKeys(w http.ResponseWriter, r *http.Request) {
	setReceiveKeyAdminHeaders(w)
	var req iCloudHMEReceiveKeysRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	generated, err := api.store.GenerateMissingICloudHMEReceiveKeys(r.Context(), req.Emails)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "generate_receive_keys", TargetType: "alias_batch",
		Target: "selected_aliases", Result: "success", Message: "generated=" + strconv.Itoa(generated),
	})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "generated": generated})
}

func (api *iCloudHMEAPI) exportReceiveKeys(w http.ResponseWriter, r *http.Request) {
	setReceiveKeyAdminHeaders(w)
	var req iCloudHMEReceiveKeysRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	records, err := api.store.ExportICloudHMEReceiveKeys(r.Context(), req.Emails)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "export_receive_keys", TargetType: "alias_batch",
		Target: "selected_aliases", Result: "success", Message: "exported=" + strconv.Itoa(len(records)),
	})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "records": records})
}

func (api *iCloudHMEAPI) revealReceiveKey(w http.ResponseWriter, r *http.Request, email string) {
	setReceiveKeyAdminHeaders(w)
	record, err := api.store.RevealICloudHMEReceiveKey(r.Context(), email)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "reveal_receive_key", TargetType: "alias",
		Target: record.Email, Result: "success",
	})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "record": record})
}

func (api *iCloudHMEAPI) resetReceiveKey(w http.ResponseWriter, r *http.Request, email string) {
	setReceiveKeyAdminHeaders(w)
	record, err := api.store.ResetICloudHMEReceiveKey(r.Context(), email)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_ = api.store.AddICloudHMEAudit(r.Context(), store.ICloudHMEAuditLog{
		Actor: requestActor(r), Action: "reset_receive_key", TargetType: "alias",
		Target: record.Email, Result: "success",
	})
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "record": record})
}

func setReceiveKeyAdminHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}
