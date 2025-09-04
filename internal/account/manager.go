package account

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"codex-companion/internal/logger"
)

// AccountType distinguishes how credentials are handled.
type AccountType int

const (
	APIKeyAccount AccountType = iota
	ChatGPTAccount
)

// Account represents an upstream Codex account.
type Account struct {
	ID             int64       `json:"id"`
	AccountID      string      `json:"account_id"`
	Name           string      `json:"name"`
	Type           AccountType `json:"type"`
	APIKey         string      `json:"api_key"`
	BaseURL        string      `json:"base_url"`
	RefreshToken   string      `json:"refresh_token"`
	AccessToken    string      `json:"access_token"`
	TokenExpiresAt time.Time   `json:"token_expires_at"`
	Priority       int         `json:"priority"`
	Exhausted      bool        `json:"exhausted"`
	ResetAt        time.Time   `json:"reset_at"`
}

// Manager handles CRUD operations on accounts stored in SQLite.
type Manager struct {
	db *sql.DB
}

// ErrDuplicate indicates the account already exists.
var ErrDuplicate = errors.New("duplicate account")

// NewManager creates a new Manager and ensures the accounts table exists.
func NewManager(db *sql.DB) (*Manager, error) {
	m := &Manager{db: db}
	if err := m.init(); err != nil {
		logger.Errorf("init accounts table failed: %v", err)
		return nil, err
	}
	return m, nil
}

func (m *Manager) init() error {
	query := `CREATE TABLE IF NOT EXISTS accounts (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       name TEXT,
       type INTEGER,
       api_key TEXT,
       refresh_token TEXT,
       access_token TEXT,
       token_expires_at TIMESTAMP,
       account_id TEXT,
       base_url TEXT,
       priority INTEGER,
       exhausted BOOLEAN,
       reset_at TIMESTAMP
   )`
	if _, err := m.db.Exec(query); err != nil {
		logger.Errorf("create accounts table failed: %v", err)
		return err
	}
	// Add new column for existing tables; ignore error if already exists.
	m.db.Exec(`ALTER TABLE accounts ADD COLUMN account_id TEXT`)
	m.db.Exec(`ALTER TABLE accounts ADD COLUMN base_url TEXT`)
	return nil
}

// List returns all accounts ordered by priority.
func (m *Manager) List(ctx context.Context) ([]*Account, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, account_id, name, type, api_key, refresh_token, access_token, token_expires_at, base_url, priority, exhausted, reset_at FROM accounts ORDER BY priority`)
	if err != nil {
		logger.Errorf("query accounts failed: %v", err)
		return nil, err
	}
	defer rows.Close()
	var res []*Account
	for rows.Next() {
		var a Account
		var apiKey, refreshToken, accessToken, accountID, baseURL sql.NullString
		var tokenExpiresAt sql.NullTime
		var resetAt sql.NullTime
		if err := rows.Scan(&a.ID, &accountID, &a.Name, &a.Type, &apiKey, &refreshToken, &accessToken, &tokenExpiresAt, &baseURL, &a.Priority, &a.Exhausted, &resetAt); err != nil {
			logger.Errorf("scan account row failed: %v", err)
			return nil, err
		}
		if apiKey.Valid {
			a.APIKey = apiKey.String
		}
		if baseURL.Valid {
			a.BaseURL = baseURL.String
		}
		if refreshToken.Valid {
			a.RefreshToken = refreshToken.String
		}
		if accessToken.Valid {
			a.AccessToken = accessToken.String
		}
		if accountID.Valid {
			a.AccountID = accountID.String
		}
		if tokenExpiresAt.Valid {
			a.TokenExpiresAt = tokenExpiresAt.Time
		}
		if resetAt.Valid {
			a.ResetAt = resetAt.Time
		}
		res = append(res, &a)
	}
	if err := rows.Err(); err != nil {
		logger.Errorf("iterate account rows failed: %v", err)
		return nil, err
	}
	return res, nil
}

// AddAPIKey adds a new API key account.
func (m *Manager) AddAPIKey(ctx context.Context, name, key, baseURL string, priority int) (*Account, error) {
	logger.Debugf("adding API key account %s priority %d", name, priority)
	var id int64
	err := m.db.QueryRowContext(ctx, `SELECT id FROM accounts WHERE api_key=?`, key).Scan(&id)
	if err == nil {
		logger.Warnf("duplicate API key account %s", key)
		return nil, ErrDuplicate
	} else if err != nil && err != sql.ErrNoRows {
		logger.Errorf("check duplicate api key failed: %v", err)
		return nil, err
	}

	res, err := m.db.ExecContext(ctx, `INSERT INTO accounts(name, type, api_key, base_url, priority, exhausted) VALUES(?, ?, ?, ?, ?, 0)`, name, APIKeyAccount, key, baseURL, priority)
	if err != nil {
		logger.Errorf("add API key account failed: %v", err)
		return nil, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		logger.Errorf("get last insert id failed: %v", err)
		return nil, err
	}
	logger.Infof("added API key account %d", id)
	return &Account{ID: id, Name: name, Type: APIKeyAccount, APIKey: key, BaseURL: baseURL, Priority: priority}, nil
}

// AddChatGPT adds a new ChatGPT account using refresh token.
func (m *Manager) AddChatGPT(ctx context.Context, name, refreshToken, accountID string, priority int) (*Account, error) {
	logger.Debugf("adding ChatGPT account %s priority %d", name, priority)
	var id int64
	err := m.db.QueryRowContext(ctx, `SELECT id FROM accounts WHERE refresh_token=?`, refreshToken).Scan(&id)
	if err == nil {
		logger.Warnf("duplicate ChatGPT account")
		return nil, ErrDuplicate
	} else if err != nil && err != sql.ErrNoRows {
		logger.Errorf("check duplicate chatgpt failed: %v", err)
		return nil, err
	}

	res, err := m.db.ExecContext(ctx, `INSERT INTO accounts(name, type, refresh_token, account_id, priority, exhausted) VALUES(?, ?, ?, ?, ?, 0)`, name, ChatGPTAccount, refreshToken, accountID, priority)
	if err != nil {
		logger.Errorf("add ChatGPT account failed: %v", err)
		return nil, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		logger.Errorf("get last insert id failed: %v", err)
		return nil, err
	}
	logger.Infof("added ChatGPT account %d", id)
	return &Account{ID: id, Name: name, Type: ChatGPTAccount, RefreshToken: refreshToken, AccountID: accountID, Priority: priority}, nil
}

// Update updates an existing account.
func (m *Manager) Update(ctx context.Context, a *Account) error {
	logger.Debugf("updating account %d", a.ID)
	_, err := m.db.ExecContext(ctx, `UPDATE accounts SET name=?, type=?, api_key=?, refresh_token=?, access_token=?, token_expires_at=?, account_id=?, base_url=?, priority=?, exhausted=?, reset_at=? WHERE id=?`,
		a.Name, a.Type, a.APIKey, a.RefreshToken, a.AccessToken, a.TokenExpiresAt, a.AccountID, a.BaseURL, a.Priority, a.Exhausted, a.ResetAt, a.ID)
	if err != nil {
		logger.Errorf("update account %d failed: %v", a.ID, err)
		return err
	}
	logger.Infof("updated account %d", a.ID)
	return nil
}

// Delete removes an account by id.
func (m *Manager) Delete(ctx context.Context, id int64) error {
	logger.Debugf("deleting account %d", id)
	_, err := m.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
	if err != nil {
		logger.Errorf("delete account %d failed: %v", id, err)
	} else {
		logger.Infof("deleted account %d", id)
	}
	return err
}

// MarkExhausted marks account exhausted until resetAt.
func (m *Manager) MarkExhausted(ctx context.Context, id int64, resetAt time.Time) error {
	logger.Warnf("marking account %d exhausted until %v", id, resetAt)
	_, err := m.db.ExecContext(ctx, `UPDATE accounts SET exhausted=1, reset_at=? WHERE id=?`, resetAt, id)
	if err != nil {
		logger.Errorf("mark account %d exhausted failed: %v", id, err)
	}
	return err
}

// Reactivate clears exhaustion flag.
func (m *Manager) Reactivate(ctx context.Context, id int64) error {
	logger.Infof("reactivating account %d", id)
	_, err := m.db.ExecContext(ctx, `UPDATE accounts SET exhausted=0, reset_at=NULL WHERE id=?`, id)
	if err != nil {
		logger.Errorf("reactivate account %d failed: %v", id, err)
	}
	return err
}

// Get retrieves account by id.
func (m *Manager) Get(ctx context.Context, id int64) (*Account, error) {
	logger.Debugf("getting account %d", id)
	row := m.db.QueryRowContext(ctx, `SELECT id, account_id, name, type, api_key, refresh_token, access_token, token_expires_at, base_url, priority, exhausted, reset_at FROM accounts WHERE id=?`, id)
	var a Account
	var apiKey, refreshToken, accessToken, accountID, baseURL sql.NullString
	var tokenExpiresAt sql.NullTime
	var resetAt sql.NullTime
	if err := row.Scan(&a.ID, &accountID, &a.Name, &a.Type, &apiKey, &refreshToken, &accessToken, &tokenExpiresAt, &baseURL, &a.Priority, &a.Exhausted, &resetAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logger.Warnf("account %d not found", id)
			return nil, nil
		}
		logger.Errorf("get account %d failed: %v", id, err)
		return nil, err
	}
	if apiKey.Valid {
		a.APIKey = apiKey.String
	}
	if baseURL.Valid {
		a.BaseURL = baseURL.String
	}
	if refreshToken.Valid {
		a.RefreshToken = refreshToken.String
	}
	if accessToken.Valid {
		a.AccessToken = accessToken.String
	}
	if accountID.Valid {
		a.AccountID = accountID.String
	}
	if tokenExpiresAt.Valid {
		a.TokenExpiresAt = tokenExpiresAt.Time
	}
	if resetAt.Valid {
		a.ResetAt = resetAt.Time
	}
	return &a, nil
}
