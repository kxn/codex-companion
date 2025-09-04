package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	acct "codex-companion/internal/account"
	"codex-companion/internal/log"
	"codex-companion/internal/scheduler"
)

// Handler implements reverse proxy logic.
type Handler struct {
	Scheduler       *scheduler.Scheduler
	Log             *log.Store
	UpstreamAPI     string
	UpstreamChatGPT string
	Client          *http.Client
}

// New creates a new proxy Handler.
func New(s *scheduler.Scheduler, l *log.Store, apiUpstream, chatgptUpstream string) *Handler {
	return &Handler{
		Scheduler:       s,
		Log:             l,
		UpstreamAPI:     apiUpstream,
		UpstreamChatGPT: chatgptUpstream,
		Client:          &http.Client{Timeout: 60 * time.Second},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/admin") {
		http.NotFound(w, r)
		return
	}
	allowed := false
	for _, p := range []string{"/v1/responses", "/v1/chat/completions", "/v1/models"} {
		if strings.HasPrefix(r.URL.Path, p) {
			allowed = true
			break
		}
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	// read request body for logging and forwarding
	var reqBody []byte
	if r.Body != nil {
		reqBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
	}

	for attempts := 0; attempts < 3; attempts++ {
		account, err := h.Scheduler.Next(ctx)
		if err != nil {
			http.Error(w, "no accounts available", http.StatusServiceUnavailable)
			return
		}

		base := h.UpstreamAPI
		path := r.URL.Path
		if account.Type == acct.APIKeyAccount {
			if account.BaseURL != "" {
				base = account.BaseURL
			}
		} else {
			base = h.UpstreamChatGPT
			path = strings.TrimPrefix(path, "/v1")
		}
		upstreamURL := base + path
		if r.URL.RawQuery != "" {
			upstreamURL += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL, bytes.NewReader(reqBody))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		req.Header = r.Header.Clone()
		if account.Type == acct.APIKeyAccount {
			req.Header.Set("Authorization", "Bearer "+account.APIKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+account.AccessToken)
		}
		resp, err := h.Client.Do(req)
		if err != nil {
			h.Log.Insert(ctx, &log.RequestLog{
				Time:      time.Now(),
				AccountID: account.ID,
				Method:    r.Method,
				URL:       r.URL.String(),
				ReqHeader: r.Header.Clone(),
				ReqBody:   string(reqBody),
				Error:     err.Error(),
			})
			if attempts == 2 {
				http.Error(w, "upstream error", http.StatusBadGateway)
				return
			}
			continue
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		// log
		h.Log.Insert(ctx, &log.RequestLog{
			Time:       time.Now(),
			AccountID:  account.ID,
			Method:     r.Method,
			URL:        r.URL.String(),
			ReqHeader:  r.Header.Clone(),
			ReqBody:    string(reqBody),
			RespHeader: resp.Header.Clone(),
			RespBody:   string(respBody),
			Status:     resp.StatusCode,
		})

		if resp.StatusCode == http.StatusTooManyRequests {
			h.Scheduler.MarkExhausted(ctx, account.ID, time.Now().Add(time.Hour))
			if attempts < 2 {
				continue
			}
		}

		for k, v := range resp.Header {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}
}
