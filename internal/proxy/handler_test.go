package proxy

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	h := New(s, ls, srv.URL)
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
	mgr.AddAPIKey(ctx, "a", "k", 1)
	req := httptest.NewRequest("GET", "http://localhost/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("bad resp %d %s", rec.Code, rec.Body.String())
	}
	logs, err := ls.List(ctx, 10)
	if err != nil || len(logs) != 1 || logs[0].Status != 200 {
		t.Fatalf("logs %v %v", logs, err)
	}
}

func TestServeHTTP429(t *testing.T) {
	h, mgr, _ := setupProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	})
	ctx := context.Background()
	a, _ := mgr.AddAPIKey(ctx, "a", "k", 1)
	req := httptest.NewRequest("GET", "http://localhost/test", nil)
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
	a1, _ := mgr.AddAPIKey(ctx, "a1", "k1", 1)
	mgr.AddAPIKey(ctx, "a2", "k2", 2)
	req := httptest.NewRequest("GET", "http://localhost/test", nil)
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
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, "ok")
	})
	ctx := context.Background()
	a, _ := mgr.AddChatGPT(ctx, "cg", "rt", "", 1)
	a.AccessToken = "at"
	a.TokenExpiresAt = time.Now().Add(time.Hour)
	if err := mgr.Update(ctx, a); err != nil {
		t.Fatalf("update: %v", err)
	}
	req := httptest.NewRequest("GET", "http://localhost/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("unexpected resp %d %s", rec.Code, rec.Body.String())
	}
}
