package log

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// RequestLog records a proxied request.
type RequestLog struct {
	ID         int64
	Time       time.Time
	AccountID  int64
	Method     string
	URL        string
	ReqHeader  http.Header
	ReqBody    string
	ReqSize    int
	RespHeader http.Header
	RespBody   string
	RespSize   int
	Status     int
	DurationMs int64
	Error      string
}

// Store persists RequestLogs in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates log store and ensures table exists.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	query := `CREATE TABLE IF NOT EXISTS logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        time TIMESTAMP,
        account_id INTEGER,
        method TEXT,
        url TEXT,
        req_header BLOB,
       req_body TEXT,
       req_size INTEGER,
       resp_header BLOB,
       resp_body TEXT,
       resp_size INTEGER,
        status INTEGER,
        duration_ms INTEGER,
        error TEXT
    )`
	_, err := s.db.Exec(query)
	return err
}

// Insert saves a RequestLog.
func (s *Store) Insert(ctx context.Context, rl *RequestLog) error {
	reqHeader, _ := json.Marshal(rl.ReqHeader)
	respHeader, _ := json.Marshal(rl.RespHeader)
	_, err := s.db.ExecContext(ctx, `INSERT INTO logs(time, account_id, method, url, req_header, req_body, req_size, resp_header, resp_body, resp_size, status, duration_ms, error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rl.Time, rl.AccountID, rl.Method, rl.URL, reqHeader, rl.ReqBody, rl.ReqSize, respHeader, rl.RespBody, rl.RespSize, rl.Status, rl.DurationMs, rl.Error)
	return err
}

// List returns latest logs limited by n with offset.
func (s *Store) List(ctx context.Context, n, offset int) ([]*RequestLog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, time, account_id, method, url, req_header, req_body, req_size, resp_header, resp_body, resp_size, status, duration_ms, error FROM logs ORDER BY id DESC LIMIT ? OFFSET ?`, n, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*RequestLog
	for rows.Next() {
		var rl RequestLog
		var reqHeader, respHeader []byte
		if err := rows.Scan(&rl.ID, &rl.Time, &rl.AccountID, &rl.Method, &rl.URL, &reqHeader, &rl.ReqBody, &rl.ReqSize, &respHeader, &rl.RespBody, &rl.RespSize, &rl.Status, &rl.DurationMs, &rl.Error); err != nil {
			return nil, err
		}
		json.Unmarshal(reqHeader, &rl.ReqHeader)
		json.Unmarshal(respHeader, &rl.RespHeader)
		res = append(res, &rl)
	}
	return res, rows.Err()
}
