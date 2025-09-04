package account

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AccountType distinguishes how credentials are handled.
type AccountType int

const (
	APIKeyAccount AccountType = iota
	ChatGPTAccount
)

// Account represents an upstream Codex account.
type Account struct {
	ID             int64
	Name           string
	Type           AccountType
	APIKey         string
	RefreshToken   string
	AccessToken    string
	TokenExpiresAt time.Time
	Priority       int
	Exhausted      bool
	ResetAt        time.Time
}

// Manager handles CRUD operations on accounts stored in SQLite.
type Manager struct {
	db *sql.DB
}

// NewManager creates a new Manager and ensures the accounts table exists.
func NewManager(db *sql.DB) (*Manager, error) {
	m := &Manager{db: db}
	if err := m.init(); err != nil {
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
        priority INTEGER,
        exhausted BOOLEAN,
        reset_at TIMESTAMP
    )`
	_, err := m.db.Exec(query)
	return err
}

// List returns all accounts ordered by priority.
func (m *Manager) List(ctx context.Context) ([]*Account, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, name, type, api_key, refresh_token, access_token, token_expires_at, priority, exhausted, reset_at FROM accounts ORDER BY priority`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*Account
	for rows.Next() {
		var a Account
		var tokenExpiresAt sql.NullTime
		var resetAt sql.NullTime
		if err := rows.Scan(&a.ID, &a.Name, &a.Type, &a.APIKey, &a.RefreshToken, &a.AccessToken, &tokenExpiresAt, &a.Priority, &a.Exhausted, &resetAt); err != nil {
			return nil, err
		}
		if tokenExpiresAt.Valid {
			a.TokenExpiresAt = tokenExpiresAt.Time
		}
		if resetAt.Valid {
			a.ResetAt = resetAt.Time
		}
		res = append(res, &a)
	}
	return res, rows.Err()
}

// AddAPIKey adds a new API key account.
func (m *Manager) AddAPIKey(ctx context.Context, name, key string, priority int) (*Account, error) {
	res, err := m.db.ExecContext(ctx, `INSERT INTO accounts(name, type, api_key, priority, exhausted) VALUES(?, ?, ?, ?, 0)`, name, APIKeyAccount, key, priority)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Account{ID: id, Name: name, Type: APIKeyAccount, APIKey: key, Priority: priority}, nil
}

// AddChatGPT adds a new ChatGPT account using refresh token.
func (m *Manager) AddChatGPT(ctx context.Context, name, refreshToken string, priority int) (*Account, error) {
	res, err := m.db.ExecContext(ctx, `INSERT INTO accounts(name, type, refresh_token, priority, exhausted) VALUES(?, ?, ?, ?, 0)`, name, ChatGPTAccount, refreshToken, priority)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Account{ID: id, Name: name, Type: ChatGPTAccount, RefreshToken: refreshToken, Priority: priority}, nil
}

// Update updates an existing account.
func (m *Manager) Update(ctx context.Context, a *Account) error {
	_, err := m.db.ExecContext(ctx, `UPDATE accounts SET name=?, type=?, api_key=?, refresh_token=?, access_token=?, token_expires_at=?, priority=?, exhausted=?, reset_at=? WHERE id=?`,
		a.Name, a.Type, a.APIKey, a.RefreshToken, a.AccessToken, a.TokenExpiresAt, a.Priority, a.Exhausted, a.ResetAt, a.ID)
	return err
}

// Delete removes an account by id.
func (m *Manager) Delete(ctx context.Context, id int64) error {
	_, err := m.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
	return err
}

// MarkExhausted marks account exhausted until resetAt.
func (m *Manager) MarkExhausted(ctx context.Context, id int64, resetAt time.Time) error {
	_, err := m.db.ExecContext(ctx, `UPDATE accounts SET exhausted=1, reset_at=? WHERE id=?`, resetAt, id)
	return err
}

// Reactivate clears exhaustion flag.
func (m *Manager) Reactivate(ctx context.Context, id int64) error {
	_, err := m.db.ExecContext(ctx, `UPDATE accounts SET exhausted=0, reset_at=NULL WHERE id=?`, id)
	return err
}

// Get retrieves account by id.
func (m *Manager) Get(ctx context.Context, id int64) (*Account, error) {
	row := m.db.QueryRowContext(ctx, `SELECT id, name, type, api_key, refresh_token, access_token, token_expires_at, priority, exhausted, reset_at FROM accounts WHERE id=?`, id)
	var a Account
	var tokenExpiresAt sql.NullTime
	var resetAt sql.NullTime
	if err := row.Scan(&a.ID, &a.Name, &a.Type, &a.APIKey, &a.RefreshToken, &a.AccessToken, &tokenExpiresAt, &a.Priority, &a.Exhausted, &resetAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if tokenExpiresAt.Valid {
		a.TokenExpiresAt = tokenExpiresAt.Time
	}
	if resetAt.Valid {
		a.ResetAt = resetAt.Time
	}
	return &a, nil
}
