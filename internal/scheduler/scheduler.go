package scheduler

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"codex-companion/internal/account"
	"codex-companion/internal/auth"
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
	accounts, err := s.mgr.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Priority < accounts[j].Priority })
	now := time.Now()
	for _, a := range accounts {
		if a.Exhausted && now.Before(a.ResetAt) {
			continue
		}
		if a.Type == account.ChatGPTAccount {
			if err := auth.Refresh(ctx, s.mgr, a); err != nil {
				continue
			}
		}
		return a, nil
	}
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
		return
	}
	now := time.Now()
	for _, a := range accounts {
		if a.Exhausted && now.After(a.ResetAt) {
			s.mgr.Reactivate(ctx, a.ID)
		}
	}
}

// MarkExhausted marks an account as exhausted until resetAt.
func (s *Scheduler) MarkExhausted(ctx context.Context, id int64, resetAt time.Time) {
	s.mgr.MarkExhausted(ctx, id, resetAt)
}
