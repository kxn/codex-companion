package log

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupLogDB(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:log?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInsertList(t *testing.T) {
	s := setupLogDB(t)
	ctx := context.Background()
	now := time.Now()
	rl1 := &RequestLog{
		Time:       now,
		AccountID:  1,
		Method:     "GET",
		URL:        "u1",
		ReqHeader:  http.Header{"A": {"1"}},
		ReqBody:    "req1",
		ReqSize:    4,
		RespHeader: http.Header{"X": {"1"}},
		RespBody:   "resp1",
		RespSize:   5,
		Status:     200,
		DurationMs: 10,
	}
	rl2 := &RequestLog{
		Time:       now.Add(time.Second),
		AccountID:  2,
		Method:     "POST",
		URL:        "u2",
		ReqHeader:  http.Header{"B": {"2"}},
		ReqBody:    "req2",
		ReqSize:    4,
		RespHeader: http.Header{"Y": {"2"}},
		RespBody:   "resp2",
		RespSize:   5,
		Status:     500,
		DurationMs: 20,
	}
	rl3 := &RequestLog{
		Time:       now.Add(2 * time.Second),
		AccountID:  3,
		Method:     "DELETE",
		URL:        "u3",
		ReqHeader:  http.Header{"C": {"3"}},
		ReqBody:    "req3",
		ReqSize:    4,
		RespHeader: http.Header{"Z": {"3"}},
		RespBody:   "resp3",
		RespSize:   5,
		Status:     201,
		DurationMs: 30,
	}
	for i, rl := range []*RequestLog{rl1, rl2, rl3} {
		if err := s.Insert(ctx, rl); err != nil {
			t.Fatalf("insert %d: %v", i+1, err)
		}
	}
	logs, err := s.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 2 || logs[0].ID <= logs[1].ID {
		t.Fatalf("unexpected order: %+v", logs)
	}
	if logs[0].ReqHeader.Get("C") != "3" || logs[0].ReqBody != "req3" || logs[0].ReqSize != 4 || logs[0].RespHeader.Get("Z") != "3" || logs[0].RespBody != "resp3" || logs[0].RespSize != 5 || logs[0].DurationMs != 30 {
		t.Fatalf("log fields not restored: %+v", logs[0])
	}

	// test offset
	logs, err = s.List(ctx, 1, 1)
	if err != nil || len(logs) != 1 || logs[0].ID == rl3.ID {
		t.Fatalf("offset failed: %+v %v", logs, err)
	}
}

func TestStoreMigrateDurationMs(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        time TIMESTAMP,
        account_id INTEGER,
        method TEXT,
        url TEXT,
        req_header BLOB,
        req_body TEXT,
        resp_header BLOB,
        resp_body TEXT,
        status INTEGER,
        error TEXT
    )`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO logs(time, account_id, method, url, req_header, req_body, resp_header, resp_body, status, error) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		time.Now(), int64(1), "GET", "u", nil, "", nil, "", 200, "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()
	logs, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].DurationMs != 0 {
		t.Fatalf("unexpected duration: %d", logs[0].DurationMs)
	}
	rl := &RequestLog{Time: time.Now(), AccountID: 2, Method: "POST", URL: "u2", DurationMs: 5}
	if err := s.Insert(ctx, rl); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	logs, err = s.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List after insert: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
}

func TestStoreMigrateReqSize(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        time TIMESTAMP,
        account_id INTEGER,
        method TEXT,
        url TEXT,
        req_header BLOB,
        req_body TEXT,
        resp_header BLOB,
        resp_body TEXT,
        status INTEGER,
        duration_ms INTEGER,
        error TEXT
    )`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO logs(time, account_id, method, url, req_header, req_body, resp_header, resp_body, status, duration_ms, error) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		time.Now(), int64(1), "GET", "u", nil, "", nil, "", 200, int64(10), "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()
	logs, err := s.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].ReqSize != 0 || logs[0].RespSize != 0 {
		t.Fatalf("unexpected sizes: %+v", logs[0])
	}
	rl := &RequestLog{Time: time.Now(), AccountID: 2, Method: "POST", URL: "u2"}
	if err := s.Insert(ctx, rl); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	logs, err = s.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List after insert: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
}
