package scheduler

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"codex-companion/internal/account"
	"codex-companion/internal/auth"
	"codex-companion/internal/logger"
)

// Scheduler selects which account to use.
type Scheduler struct {
	mgr *account.Manager
	mu  sync.Mutex
}

func New(mgr *account.Manager) *Scheduler {
	return &Scheduler{mgr: mgr}
}

// Next returns the next available account.
func (s *Scheduler) Next(ctx context.Context) (*account.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	logger.Debugf("scheduler selecting next account")
	accounts, err := s.mgr.List(ctx)
	if err != nil {
		logger.Errorf("list accounts failed: %v", err)
		return nil, err
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Priority < accounts[j].Priority })
	now := time.Now()
	for _, a := range accounts {
		if a.Exhausted && now.Before(a.ResetAt) {
			logger.Debugf("account %d exhausted until %v", a.ID, a.ResetAt)
			continue
		}
		if a.Type == account.ChatGPTAccount {
			if err := auth.Refresh(ctx, s.mgr, a); err != nil {
				logger.Warnf("refresh account %d failed: %v", a.ID, err)
				continue
			}
		}
		logger.Debugf("selected account %d", a.ID)
		return a, nil
	}
	logger.Warnf("no accounts available")
	return nil, errors.New("no accounts available")
}

// StartReactivator starts background goroutine to reactivate exhausted accounts.
func (s *Scheduler) StartReactivator(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reactivate(ctx)
			}
		}
	}()
}

func (s *Scheduler) reactivate(ctx context.Context) {
	accounts, err := s.mgr.List(ctx)
	if err != nil {
		logger.Errorf("reactivate list accounts: %v", err)
		return
	}
	now := time.Now()
	for _, a := range accounts {
		if a.Exhausted && now.After(a.ResetAt) {
			logger.Infof("reactivating account %d", a.ID)
			if err := s.mgr.Reactivate(ctx, a.ID); err != nil {
				logger.Errorf("reactivate account %d failed: %v", a.ID, err)
			}
		}
	}
}

// MarkExhausted marks an account as exhausted until resetAt.
func (s *Scheduler) MarkExhausted(ctx context.Context, id int64, resetAt time.Time) {
	logger.Warnf("marking account %d exhausted until %v", id, resetAt)
	if err := s.mgr.MarkExhausted(ctx, id, resetAt); err != nil {
		logger.Errorf("mark exhausted %d failed: %v", id, err)
	}
}
