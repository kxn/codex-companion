package auth

import (
	"bytes"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func swapClient(rt http.RoundTripper) func() {
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: rt}
	return func() { http.DefaultClient = old }
}

func TestExchangeRefreshToken(t *testing.T) {
	defer swapClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != tokenURL {
			t.Fatalf("unexpected url: %s", r.URL)
		}
		body := `{"access_token":"tok","refresh_token":"nrt","expires_in":60}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}))()
	tok, rt, dur, err := ExchangeRefreshToken(context.Background(), "rt")
	if err != nil || tok != "tok" || rt != "nrt" || dur != 60*time.Second {
		t.Fatalf("got %v %v %v %v", tok, rt, dur, err)
	}
}

func TestExchangeRefreshTokenError(t *testing.T) {
	defer swapClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
	}))()
	if _, _, _, err := ExchangeRefreshToken(context.Background(), "rt"); err == nil {
		t.Fatalf("expected error")
	}
}

func setupAuthTestMgr(t *testing.T) (*account.Manager, *account.Account) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := account.NewManager(db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, err := mgr.AddChatGPT(ctx, "c", "rt", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	return mgr, a
}

func TestRefreshUpdates(t *testing.T) {
	mgr, a := setupAuthTestMgr(t)
	a.TokenExpiresAt = time.Now().Add(-time.Minute)
	if err := mgr.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	defer swapClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"new","refresh_token":"rt2","expires_in":120}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	}))()
	if err := Refresh(context.Background(), mgr, a); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if a.AccessToken != "new" || a.RefreshToken != "rt2" {
		t.Fatalf("token not set: %+v", a)
	}
	got, _ := mgr.Get(context.Background(), a.ID)
	if got.AccessToken != "new" || got.RefreshToken != "rt2" {
		t.Fatalf("db not updated: %+v", got)
	}
}

func TestRefreshNoNeed(t *testing.T) {
	mgr, a := setupAuthTestMgr(t)
	a.TokenExpiresAt = time.Now().Add(2 * time.Minute)
	if err := mgr.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	defer swapClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request")
		return nil, nil
	}))()
	if err := Refresh(context.Background(), mgr, a); err != nil {
		t.Fatalf("refresh: %v", err)
	}
}

func TestRefreshAPIKey(t *testing.T) {
	db, _ := sql.Open("sqlite", "file:auth2?mode=memory&cache=shared")
	mgr, _ := account.NewManager(db)
	ctx := context.Background()
	a, _ := mgr.AddAPIKey(ctx, "a", "k", "", 1)
	defer swapClient(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("should not call")
		return nil, nil
	}))()
	if err := Refresh(ctx, mgr, a); err != nil {
		t.Fatalf("refresh: %v", err)
	}
}
