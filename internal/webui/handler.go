package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

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
	fsys, err := fs.Sub(staticFiles, "static")
	if err != nil {
		logger.Errorf("load static files: %v", err)
	}
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
			if err := json.NewEncoder(w).Encode(accounts); err != nil {
				logger.Errorf("encode accounts failed: %v", err)
			}
		case http.MethodPost:
			var req struct {
				Type         string `json:"type"`
				Name         string `json:"name"`
				APIKey       string `json:"api_key"`
				BaseURL      string `json:"base_url"`
				RefreshToken string `json:"refresh_token"`
				AccessToken  string `json:"access_token"`
				AccountID    string `json:"account_id"`
				Priority     int    `json:"priority"`
				LastRefresh  string `json:"last_refresh"`
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
				if err == nil && req.AccessToken != "" {
					a.AccessToken = req.AccessToken
					if req.LastRefresh != "" {
						if t, err := time.Parse(time.RFC3339, req.LastRefresh); err == nil {
							a.TokenExpiresAt = t.Add(28 * 24 * time.Hour)
						}
					}
					if a.TokenExpiresAt.IsZero() {
						a.TokenExpiresAt = time.Now().Add(28 * 24 * time.Hour)
					}
					if err := am.Update(ctx, a); err != nil {
						logger.Errorf("update account token: %v", err)
					}
				}
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
			if err := json.NewEncoder(w).Encode(a); err != nil {
				logger.Errorf("encode account failed: %v", err)
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/accounts/import/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			logger.Warnf("import auth upload file: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			logger.Errorf("read uploaded auth.json: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a, err := ImportAuthData(r.Context(), am, data)
		if err != nil {
			logger.Errorf("import auth from upload failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(a); err != nil {
			logger.Errorf("encode account failed: %v", err)
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
		if err := json.NewEncoder(w).Encode(a); err != nil {
			logger.Errorf("encode account failed: %v", err)
		}
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
			logger.Errorf("list logs failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hasMore := false
		if len(logs) > size {
			hasMore = true
			logs = logs[:size]
		}
		if err := json.NewEncoder(w).Encode(struct {
			Logs    []*logpkg.RequestLog `json:"logs"`
			Page    int                  `json:"page"`
			HasMore bool                 `json:"has_more"`
		}{logs, page, hasMore}); err != nil {
			logger.Errorf("encode logs failed: %v", err)
		}
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
	return ImportAuthData(ctx, am, data)
}

// ImportAuthData imports a ChatGPT account from the provided auth.json data.
func ImportAuthData(ctx context.Context, am *account.Manager, data []byte) (*account.Account, error) {
	var cfg struct {
		Tokens struct {
			RefreshToken string `json:"refresh_token"`
			AccessToken  string `json:"access_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
		LastRefresh string `json:"last_refresh"`
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
		logger.Errorf("list accounts failed: %v", err)
		return nil, err
	}
	priority := 0
	if len(accounts) > 0 {
		priority = accounts[len(accounts)-1].Priority + 1
	}
	name := cfg.Tokens.AccountID
	if len(name) > 8 {
		name = name[:8]
	}
	logger.Infof("importing ChatGPT account %s", name)
	a, err := am.AddChatGPT(ctx, name, cfg.Tokens.RefreshToken, cfg.Tokens.AccountID, priority)
	if err != nil {
		return nil, err
	}
	a.AccessToken = cfg.Tokens.AccessToken
	if cfg.LastRefresh != "" {
		if t, err := time.Parse(time.RFC3339, cfg.LastRefresh); err == nil {
			a.TokenExpiresAt = t.Add(28 * 24 * time.Hour)
		}
	}
	if a.TokenExpiresAt.IsZero() {
		a.TokenExpiresAt = time.Now().Add(28 * 24 * time.Hour)
	}
	if err := am.Update(ctx, a); err != nil {
		logger.Errorf("update account after import: %v", err)
		return nil, err
	}
	return a, nil
}
