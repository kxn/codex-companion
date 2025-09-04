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
			logger.Debugf("list accounts")
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
				RefreshToken string `json:"refresh_token"`
				Priority     int    `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				logger.Warnf("bad add account request: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var a *account.Account
			var err error
			if req.Type == "api_key" {
				a, err = am.AddAPIKey(ctx, req.Name, req.APIKey, req.Priority)
			} else if req.Type == "chatgpt" {
				a, err = am.AddChatGPT(ctx, req.Name, req.RefreshToken, req.Priority)
			} else {
				logger.Warnf("unknown account type %s", req.Type)
				http.Error(w, "unknown type", http.StatusBadRequest)
				return
			}
			if err != nil {
				logger.Errorf("add account failed: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
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
		logger.Infof("importing auth.json")
		a, err := ImportAuth(r.Context(), am)
		if err != nil {
			logger.Errorf("import auth failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Infof("imported account %d", a.ID)
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
		logs, err := ls.List(ctx, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(logs)
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
	return am.AddChatGPT(ctx, "imported", cfg.Tokens.RefreshToken, 0)
}
