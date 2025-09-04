package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"codex-companion/internal/account"
	logpkg "codex-companion/internal/log"
	"codex-companion/internal/logger"
)

//go:embed static/*
var staticFiles embed.FS

// AdminHandler registers routes on /admin.
func AdminHandler(am *account.Manager, ls *logpkg.Store) http.Handler {
	mux := http.NewServeMux()
	// Static files
	fsys, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", http.FileServer(http.FS(fsys)))

	// API
	mux.HandleFunc("/api/accounts", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		switch r.Method {
		case http.MethodGet:
			accounts, err := am.List(ctx)
			if err != nil {
				logger.Errorf("list accounts failed: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(accounts)
		case http.MethodPost:
			var req struct {
				Type         string `json:"type"`
				Name         string `json:"name"`
				APIKey       string `json:"api_key"`
				BaseURL      string `json:"base_url"`
				RefreshToken string `json:"refresh_token"`
				AccountID    string `json:"account_id"`
				Priority     int    `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				logger.Warnf("bad add account request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Determine priority if not provided
			priority := req.Priority
			if priority == 0 {
				accounts, err := am.List(ctx)
				if err != nil {
					logger.Errorf("list accounts failed: %v", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if len(accounts) > 0 {
					priority = accounts[len(accounts)-1].Priority + 1
				}
			}

			var a *account.Account
			var err error
			if req.Type == "api_key" {
				a, err = am.AddAPIKey(ctx, req.Name, req.APIKey, req.BaseURL, priority)
			} else if req.Type == "chatgpt" {
				a, err = am.AddChatGPT(ctx, req.Name, req.RefreshToken, req.AccountID, priority)
			} else {
				logger.Warnf("unknown account type %s", req.Type)
				http.Error(w, "unknown type", http.StatusBadRequest)
				return
			}
			if err != nil {
				if errors.Is(err, account.ErrDuplicate) {
					http.Error(w, err.Error(), http.StatusConflict)
				} else {
					logger.Errorf("add account failed: %v", err)
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			json.NewEncoder(w).Encode(a)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/accounts/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		a, err := ImportAuth(r.Context(), am)
		if err != nil {
			logger.Errorf("import auth failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(a)
	})

	mux.HandleFunc("/api/accounts/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		idStr := path.Base(r.URL.Path)
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			logger.Warnf("bad account id %s", idStr)
			http.Error(w, "bad id", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodPut:
			var a account.Account
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				logger.Warnf("bad account update request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			a.ID = id
			if err := am.Update(ctx, &a); err != nil {
				logger.Errorf("update account %d failed: %v", id, err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if err := am.Delete(ctx, id); err != nil {
				logger.Errorf("delete account %d failed: %v", id, err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q := r.URL.Query()
		page, _ := strconv.Atoi(q.Get("page"))
		if page < 1 {
			page = 1
		}
		size, _ := strconv.Atoi(q.Get("size"))
		if size <= 0 {
			size = 100
		}
		offset := (page - 1) * size
		logs, err := ls.List(ctx, size+1, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hasMore := false
		if len(logs) > size {
			hasMore = true
			logs = logs[:size]
		}
		json.NewEncoder(w).Encode(struct {
			Logs    []*logpkg.RequestLog `json:"logs"`
			Page    int                  `json:"page"`
			HasMore bool                 `json:"has_more"`
		}{logs, page, hasMore})
	})

	return http.StripPrefix("/admin", mux)
}

// ImportAuth reads auth.json from CODEX_HOME.
func ImportAuth(ctx context.Context, am *account.Manager) (*account.Account, error) {
	logger.Debugf("reading auth.json")
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		usr, err := os.UserHomeDir()
		if err != nil {
			logger.Errorf("user home dir: %v", err)
			return nil, err
		}
		home = filepath.Join(usr, ".codex")
	}
	data, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		logger.Errorf("read auth.json: %v", err)
		return nil, err
	}
	var cfg struct {
		Tokens struct {
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		logger.Errorf("unmarshal auth.json: %v", err)
		return nil, err
	}
	if cfg.Tokens.RefreshToken == "" {
		logger.Warnf("refresh token not found")
		return nil, errors.New("refresh token not found")
	}
	accounts, err := am.List(ctx)
	if err != nil {
		return nil, err
	}
	priority := 0
	if len(accounts) > 0 {
		priority = accounts[len(accounts)-1].Priority + 1
	}
	return am.AddChatGPT(ctx, "imported", cfg.Tokens.RefreshToken, cfg.Tokens.AccountID, priority)
}
