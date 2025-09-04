package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"codex-companion/internal/account"
	_ "modernc.org/sqlite"
)

func setupScheduler(t *testing.T) (*Scheduler, *account.Manager) {
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
	s := New(mgr)
	return s, mgr
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func swap(rt http.RoundTripper) func() {
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: rt}
	return func() { http.DefaultClient = old }
}

func TestNextSelectsHighestPriority(t *testing.T) {
	s, mgr := setupScheduler(t)
	ctx := context.Background()
	a1, _ := mgr.AddAPIKey(ctx, "a1", "k1", 1)
	_, _ = mgr.AddAPIKey(ctx, "a2", "k2", 2)
	got, err := s.Next(ctx)
	if err != nil || got.ID != a1.ID {
		t.Fatalf("unexpected: %+v %v", got, err)
	}
}

func TestNextSkipsExhausted(t *testing.T) {
	s, mgr := setupScheduler(t)
	ctx := context.Background()
	a1, _ := mgr.AddAPIKey(ctx, "a1", "k1", 1)
	a2, _ := mgr.AddAPIKey(ctx, "a2", "k2", 2)
	mgr.MarkExhausted(ctx, a1.ID, time.Now().Add(time.Hour))
	got, err := s.Next(ctx)
	if err != nil || got.ID != a2.ID {
		t.Fatalf("expected a2, got %+v %v", got, err)
	}
}

func TestNextRefreshFailureFallback(t *testing.T) {
	s, mgr := setupScheduler(t)
	ctx := context.Background()
	cg, _ := mgr.AddChatGPT(ctx, "cg", "rt", 1)
	cg.TokenExpiresAt = time.Now().Add(-time.Minute)
	mgr.Update(ctx, cg)
	ak, _ := mgr.AddAPIKey(ctx, "a", "k", 2)
	defer swap(rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	}))()
	got, err := s.Next(ctx)
	if err != nil || got.ID != ak.ID {
		t.Fatalf("expected fallback, got %+v %v", got, err)
	}
}

func TestReactivate(t *testing.T) {
	s, mgr := setupScheduler(t)
	ctx := context.Background()
	a, _ := mgr.AddAPIKey(ctx, "a", "k", 1)
	mgr.MarkExhausted(ctx, a.ID, time.Now().Add(-time.Minute))
	s.reactivate(ctx)
	got, _ := mgr.Get(ctx, a.ID)
	if got.Exhausted {
		t.Fatalf("not reactivated")
	}
}

func TestMarkExhausted(t *testing.T) {
	s, mgr := setupScheduler(t)
	ctx := context.Background()
	a, _ := mgr.AddAPIKey(ctx, "a", "k", 1)
	reset := time.Now().Add(time.Hour)
	s.MarkExhausted(ctx, a.ID, reset)
	got, _ := mgr.Get(ctx, a.ID)
	if !got.Exhausted || got.ResetAt.Before(reset.Add(-time.Minute)) {
		t.Fatalf("mark failed: %+v", got)
	}
}

func TestStartReactivator(t *testing.T) {
	s, mgr := setupScheduler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _ := mgr.AddAPIKey(ctx, "a", "k", 1)
	mgr.MarkExhausted(ctx, a.ID, time.Now().Add(-time.Minute))
	s.StartReactivator(ctx, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	got, _ := mgr.Get(ctx, a.ID)
	if got.Exhausted {
		t.Fatalf("account not reactivated")
	}
}
