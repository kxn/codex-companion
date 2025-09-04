package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-companion/internal/account"
	logpkg "codex-companion/internal/log"
	"codex-companion/internal/scheduler"
	_ "modernc.org/sqlite"
)

func setupProxy(t *testing.T, upstream http.HandlerFunc) (*Handler, *account.Manager, *logpkg.Store) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := account.NewManager(db)
	if err != nil {
		t.Fatal(err)
	}
	ls, err := logpkg.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	s := scheduler.New(mgr)
	srv := httptest.NewServer(upstream)
	t.Cleanup(srv.Close)
	h := New(s, ls, srv.URL, srv.URL)
	return h, mgr, ls
}

func TestServeHTTPForwardAndLog(t *testing.T) {
	h, mgr, ls := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, "ok")
	})
	ctx := context.Background()
	mgr.AddAPIKey(ctx, "a", "k", "", 1)
	req := httptest.NewRequest("GET", "http://localhost/v1/responses", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("bad resp %d %s", rec.Code, rec.Body.String())
	}
	logs, err := ls.List(ctx, 10, 0)
	if err != nil || len(logs) != 1 || logs[0].Status != 200 {
		t.Fatalf("logs %v %v", logs, err)
	}
}

func TestServeHTTP429(t *testing.T) {
	h, mgr, _ := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	})
	ctx := context.Background()
	a, _ := mgr.AddAPIKey(ctx, "a", "k", "", 1)
	req := httptest.NewRequest("GET", "http://localhost/v1/responses", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	got, _ := mgr.Get(ctx, a.ID)
	if !got.Exhausted {
		t.Fatalf("account not marked exhausted")
	}
}

func TestServeHTTPRetryNextAccount(t *testing.T) {
	calls := 0
	h, mgr, _ := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") == "Bearer k1" {
			w.WriteHeader(429)
			return
		}
		io.WriteString(w, "ok")
	})
	ctx := context.Background()
	a1, _ := mgr.AddAPIKey(ctx, "a1", "k1", "", 1)
	mgr.AddAPIKey(ctx, "a2", "k2", "", 2)
	req := httptest.NewRequest("GET", "http://localhost/v1/responses", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("unexpected resp %d %s", rec.Code, rec.Body.String())
	}
	if calls != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", calls)
	}
	got, _ := mgr.Get(ctx, a1.ID)
	if !got.Exhausted {
		t.Fatalf("first account not exhausted")
	}
}

func TestServeHTTPChatGPTAccount(t *testing.T) {
	h, mgr, _ := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("chatgpt-account-id") != "aid" {
			t.Fatalf("missing chatgpt-account-id header")
		}
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		if v, ok := m["store"].(bool); !ok || v {
			t.Fatalf("store not false: %v", m["store"])
		}
		inc, ok := m["include"].([]any)
		if !ok || len(inc) != 1 || inc[0] != "reasoning.encrypted_content" {
			t.Fatalf("include not normalized: %v", m["include"])
		}
		io.WriteString(w, "ok")
	})
	ctx := context.Background()
	a, _ := mgr.AddChatGPT(ctx, "cg", "rt", "aid", 1)
	a.AccessToken = "at"
	a.TokenExpiresAt = time.Now().Add(time.Hour)
	if err := mgr.Update(ctx, a); err != nil {
		t.Fatalf("update: %v", err)
	}
	req := httptest.NewRequest("POST", "http://localhost/v1/responses", strings.NewReader(`{"store":true}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("unexpected resp %d %s", rec.Code, rec.Body.String())
	}
}

func TestServeHTTPAPIKeyNormalize(t *testing.T) {
	h, mgr, _ := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("chatgpt-account-id") != "" {
			t.Fatalf("chatgpt-account-id should be empty")
		}
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		if v, ok := m["store"].(bool); !ok || !v {
			t.Fatalf("store not true: %v", m["store"])
		}
		if _, ok := m["include"]; ok {
			t.Fatalf("include should be removed")
		}
		io.WriteString(w, "ok")
	})
	ctx := context.Background()
	mgr.AddAPIKey(ctx, "a", "k", "", 1)
	req := httptest.NewRequest("POST", "http://localhost/v1/responses", strings.NewReader(`{"store":false,"include":["x"]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("unexpected resp %d %s", rec.Code, rec.Body.String())
	}
}

func TestServeHTTPDisallowedPath(t *testing.T) {
	h, _, _ := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not be called")
	})
	req := httptest.NewRequest("GET", "http://localhost/other", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestServeHTTPAccountBaseURL(t *testing.T) {
	badCalls := 0
	h, mgr, _ := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		badCalls++
		w.WriteHeader(500)
	})
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer goodSrv.Close()
	ctx := context.Background()
	mgr.AddAPIKey(ctx, "a", "k", goodSrv.URL, 1)
	req := httptest.NewRequest("GET", "http://localhost/v1/responses", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("unexpected resp %d %s", rec.Code, rec.Body.String())
	}
	if badCalls != 0 {
		t.Fatalf("default upstream was called")
	}
}
