package log

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"codex-companion/internal/logger"
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
		logger.Errorf("init logs table failed: %v", err)
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
	if err != nil {
		logger.Errorf("create logs table failed: %v", err)
	}
	return err
}

// Insert saves a RequestLog.
func (s *Store) Insert(ctx context.Context, rl *RequestLog) error {
	reqHeader, err := json.Marshal(rl.ReqHeader)
	if err != nil {
		logger.Warnf("marshal req header failed: %v", err)
	}
	respHeader, err := json.Marshal(rl.RespHeader)
	if err != nil {
		logger.Warnf("marshal resp header failed: %v", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO logs(time, account_id, method, url, req_header, req_body, req_size, resp_header, resp_body, resp_size, status, duration_ms, error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rl.Time, rl.AccountID, rl.Method, rl.URL, reqHeader, rl.ReqBody, rl.ReqSize, respHeader, rl.RespBody, rl.RespSize, rl.Status, rl.DurationMs, rl.Error)
	if err != nil {
		logger.Errorf("insert request log failed: %v", err)
		return err
	}
	logger.Debugf("logged request account %d status %d", rl.AccountID, rl.Status)
	return nil
}

// List returns latest logs limited by n with offset.
func (s *Store) List(ctx context.Context, n, offset int) ([]*RequestLog, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, time, account_id, method, url, req_header, req_body, req_size, resp_header, resp_body, resp_size, status, duration_ms, error FROM logs ORDER BY id DESC LIMIT ? OFFSET ?`, n, offset)
	if err != nil {
		logger.Errorf("query logs failed: %v", err)
		return nil, err
	}
	defer rows.Close()
	var res []*RequestLog
	for rows.Next() {
		var rl RequestLog
		var reqHeader, respHeader []byte
		if err := rows.Scan(&rl.ID, &rl.Time, &rl.AccountID, &rl.Method, &rl.URL, &reqHeader, &rl.ReqBody, &rl.ReqSize, &respHeader, &rl.RespBody, &rl.RespSize, &rl.Status, &rl.DurationMs, &rl.Error); err != nil {
			logger.Errorf("scan log row failed: %v", err)
			return nil, err
		}
		if err := json.Unmarshal(reqHeader, &rl.ReqHeader); err != nil {
			logger.Warnf("unmarshal req header failed: %v", err)
		}
		if err := json.Unmarshal(respHeader, &rl.RespHeader); err != nil {
			logger.Warnf("unmarshal resp header failed: %v", err)
		}
		res = append(res, &rl)
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("iterate logs failed: %v", err)
		return nil, err
	}
	logger.Debugf("retrieved %d logs", len(res))
	return res, nil
}
