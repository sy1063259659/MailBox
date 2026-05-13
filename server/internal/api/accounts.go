package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"mailbox-server/internal/store"
)

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

type accountAPI struct {
	store *store.Store
}

type accountsResponse struct {
	OK       bool                `json:"ok"`
	Accounts []store.MailAccount `json:"accounts"`
}

type groupsResponse struct {
	OK     bool          `json:"ok"`
	Groups []store.Group `json:"groups"`
}

type importAccountsRequest struct {
	Text      string `json:"text"`
	Overwrite bool   `json:"overwrite"`
}

type moveAccountsRequest struct {
	Emails []string `json:"emails"`
	Group  string   `json:"group"`
}

type groupRequest struct {
	Name string `json:"name"`
}

func (api accountAPI) listAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := api.store.ListAccounts(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, accountsResponse{OK: true, Accounts: accounts})
}

func (api accountAPI) importAccounts(w http.ResponseWriter, r *http.Request) {
	var req importAccountsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	inputs, errors := parseAccountImportText(req.Text)
	if len(inputs) == 0 && len(errors) == 0 {
		errors = append(errors, "没有可导入的账号")
	}
	if req.Overwrite {
		if err := api.store.ClearAccounts(r.Context()); err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	result, err := api.store.ImportAccounts(r.Context(), inputs)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	result.Errors = append(errors, result.Errors...)
	WriteJSON(w, http.StatusOK, result)
}

func (api accountAPI) deleteAccount(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
	if email == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "email is required")
		return
	}
	if err := api.store.DeleteAccount(r.Context(), email); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (api accountAPI) moveAccounts(w http.ResponseWriter, r *http.Request) {
	var req moveAccountsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := api.store.MoveAccountsToGroup(r.Context(), req.Emails, req.Group); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (api accountAPI) exportAccounts(w http.ResponseWriter, r *http.Request) {
	text, err := api.store.ExportAccounts(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "text": text})
}

func (api accountAPI) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := api.store.ListGroups(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, groupsResponse{OK: true, Groups: groups})
}

func (api accountAPI) createGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	group, err := api.store.CreateGroup(r.Context(), req.Name)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "group": group})
}

func (api accountAPI) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r.URL.Path)
	if !ok {
		return
	}
	var req groupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	group, err := api.store.RenameGroup(r.Context(), id, req.Name)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "group": group})
}

func (api accountAPI) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r.URL.Path)
	if !ok {
		return
	}
	if err := api.store.DeleteGroup(r.Context(), id); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func parseGroupID(w http.ResponseWriter, path string) (int64, bool) {
	raw := strings.TrimPrefix(path, "/api/groups/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if raw == "" || err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "group id is required")
		return 0, false
	}
	return id, true
}

func parseAccountImportText(text string) ([]store.AccountInput, []string) {
	lines := strings.Split(text, "\n")
	inputs := []store.AccountInput{}
	errors := []string{}

	for index, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if strings.Contains(line, "----") {
			fields = strings.Split(line, "----")
			for index, field := range fields {
				fields[index] = strings.TrimSpace(field)
			}
		}
		if len(fields) < 4 {
			errors = append(errors, "第 "+strconv.Itoa(index+1)+" 行字段不足")
			continue
		}
		email := strings.ToLower(strings.TrimSpace(fields[0]))
		if !emailPattern.MatchString(email) {
			errors = append(errors, "第 "+strconv.Itoa(index+1)+" 行邮箱格式非法："+fields[0])
			continue
		}
		if strings.TrimSpace(fields[1]) == "" || strings.TrimSpace(fields[2]) == "" || strings.TrimSpace(fields[3]) == "" {
			errors = append(errors, "第 "+strconv.Itoa(index+1)+" 行必填字段为空："+fields[0])
			continue
		}
		inputs = append(inputs, store.AccountInput{
			Email:        email,
			Password:     strings.TrimSpace(fields[1]),
			ClientID:     strings.TrimSpace(fields[2]),
			RefreshToken: strings.TrimSpace(fields[3]),
			Group:        store.DefaultGroupName,
		})
	}

	return inputs, errors
}
