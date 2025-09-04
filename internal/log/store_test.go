package log

import (
	"context"
	"database/sql"
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
		ReqBody:    []byte("req1"),
		RespHeader: http.Header{"X": {"1"}},
		RespBody:   []byte("resp1"),
		Status:     200,
	}
	rl2 := &RequestLog{
		Time:       now.Add(time.Second),
		AccountID:  2,
		Method:     "POST",
		URL:        "u2",
		ReqHeader:  http.Header{"B": {"2"}},
		ReqBody:    []byte("req2"),
		RespHeader: http.Header{"Y": {"2"}},
		RespBody:   []byte("resp2"),
		Status:     500,
	}
	rl3 := &RequestLog{
		Time:       now.Add(2 * time.Second),
		AccountID:  3,
		Method:     "DELETE",
		URL:        "u3",
		ReqHeader:  http.Header{"C": {"3"}},
		ReqBody:    []byte("req3"),
		RespHeader: http.Header{"Z": {"3"}},
		RespBody:   []byte("resp3"),
		Status:     201,
	}
	for i, rl := range []*RequestLog{rl1, rl2, rl3} {
		if err := s.Insert(ctx, rl); err != nil {
			t.Fatalf("insert %d: %v", i+1, err)
		}
	}
	logs, err := s.List(ctx, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(logs) != 2 || logs[0].ID <= logs[1].ID {
		t.Fatalf("unexpected order: %+v", logs)
	}
	if logs[0].ReqHeader.Get("C") != "3" || string(logs[0].ReqBody) != "req3" || logs[0].RespHeader.Get("Z") != "3" || string(logs[0].RespBody) != "resp3" {
		t.Fatalf("log fields not restored: %+v", logs[0])
	}
}
