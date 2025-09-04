package webui

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"codex-companion/internal/account"
	logpkg "codex-companion/internal/log"
	_ "modernc.org/sqlite"
)

func setupWebUI(t *testing.T) (*account.Manager, *logpkg.Store, http.Handler) {
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
	h := AdminHandler(mgr, ls)
	return mgr, ls, h
}

func TestStaticIndex(t *testing.T) {
	_, _, h := setupWebUI(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Codex Companion") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

func TestImportAuth(t *testing.T) {
	_, _, h := setupWebUI(t)
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	data := `{"tokens":{"refresh_token":"rt"}}`
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/api/accounts/import", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var a account.Account
	if err := json.NewDecoder(rec.Body).Decode(&a); err != nil || a.RefreshToken != "rt" {
		t.Fatalf("decode: %v %+v", err, a)
	}

	t.Setenv("CODEX_HOME", filepath.Join(dir, "missing"))
	req = httptest.NewRequest(http.MethodPost, "/admin/api/accounts/import", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected error for missing file")
	}
}

func TestAccountsAPI(t *testing.T) {
	mgr, _, h := setupWebUI(t)

	body := `{"type":"chatgpt","name":"cg","refresh_token":"rt"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/api/accounts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post chatgpt: %d", rec.Code)
	}

	body = `{"type":"api_key","name":"ak","api_key":"k"}`
	req = httptest.NewRequest(http.MethodPost, "/admin/api/accounts", strings.NewReader(body))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post api: %d", rec.Code)
	}
	var a account.Account
	if err := json.NewDecoder(rec.Body).Decode(&a); err != nil {
		t.Fatal(err)
	}

	// duplicate API key should be rejected
	req = httptest.NewRequest(http.MethodPost, "/admin/api/accounts", strings.NewReader(body))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected conflict for duplicate api key, got %d", rec.Code)
	}

	a.Name = "new"
	buf, _ := json.Marshal(&a)
	req = httptest.NewRequest(http.MethodPut, "/admin/api/accounts/"+strconv.FormatInt(a.ID, 10), bytes.NewReader(buf))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("put: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/api/accounts", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var list []account.Account
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil || len(list) != 2 {
		t.Fatalf("list decode: %v %v", err, list)
	}
	if list[0].Name != "cg" || list[1].Name != "new" {
		t.Fatalf("unexpected list: %+v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, "/admin/api/accounts/"+strconv.FormatInt(list[0].ID, 10), nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/api/accounts", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 1 || list[0].Name != "new" {
		t.Fatalf("after delete: %+v", list)
	}

	got, _ := mgr.Get(context.Background(), list[0].ID)
	if got.Name != "new" {
		t.Fatalf("manager not updated: %+v", got)
	}
}

func TestLogsAPI(t *testing.T) {
	_, ls, h := setupWebUI(t)
	ctx := context.Background()
	rl := &logpkg.RequestLog{Time: time.Now(), AccountID: 1, Method: "GET", URL: "u"}
	if err := ls.Insert(ctx, rl); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/api/logs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logs status: %d", rec.Code)
	}
	var logs []logpkg.RequestLog
	if err := json.NewDecoder(rec.Body).Decode(&logs); err != nil || len(logs) != 1 || logs[0].Method != "GET" {
		t.Fatalf("logs decode: %v %+v", err, logs)
	}
}
