package api

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func WriteError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("X-Mailbox-Error-Code", code)
	WriteJSON(w, status, ErrorResponse{
		OK:      false,
		Error:   code,
		Message: message,
	})
}

func readJSON(r *http.Request, target interface{}) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
