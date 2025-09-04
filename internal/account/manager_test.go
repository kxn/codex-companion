package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func TestAddAndGet(t *testing.T) {
	db := setupTestDB(t)
	mgr, err := NewManager(db)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	ctx := context.Background()
	a1, err := mgr.AddAPIKey(ctx, "a1", "k1", "", 1)
	if err != nil {
		t.Fatalf("add api: %v", err)
	}
	got, err := mgr.Get(ctx, a1.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.APIKey != "k1" || got.Name != "a1" {
		t.Fatalf("unexpected account: %+v", got)
	}

	a2, err := mgr.AddChatGPT(ctx, "a2", "rt", "", 2)
	if err != nil {
		t.Fatalf("add chatgpt: %v", err)
	}
	got2, err := mgr.Get(ctx, a2.ID)
	if err != nil || got2 == nil {
		t.Fatalf("get2: %v %v", got2, err)
	}
	if got2.RefreshToken != "rt" || got2.Type != ChatGPTAccount {
		t.Fatalf("unexpected: %+v", got2)
	}
}

func TestListOrderUpdateDelete(t *testing.T) {
	db := setupTestDB(t)
	mgr, _ := NewManager(db)
	ctx := context.Background()
	a1, _ := mgr.AddAPIKey(ctx, "a1", "k1", "", 2)
	a2, _ := mgr.AddAPIKey(ctx, "a2", "k2", "", 1)
	list, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != a2.ID {
		t.Fatalf("order wrong: %+v", list)
	}

	a1.Name = "new"
	if err := mgr.Update(ctx, a1); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := mgr.Get(ctx, a1.ID)
	if got.Name != "new" {
		t.Fatalf("update failed: %+v", got)
	}

	if err := mgr.Delete(ctx, a1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone, err := mgr.Get(ctx, a1.ID)
	if err != nil || gone != nil {
		t.Fatalf("delete not effective: %v %v", gone, err)
	}
}

func TestDuplicate(t *testing.T) {
	db := setupTestDB(t)
	mgr, _ := NewManager(db)
	ctx := context.Background()

	if _, err := mgr.AddAPIKey(ctx, "a1", "k1", "", 0); err != nil {
		t.Fatalf("add api: %v", err)
	}
	if _, err := mgr.AddAPIKey(ctx, "a2", "k1", "", 0); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate api key error, got %v", err)
	}

	if _, err := mgr.AddChatGPT(ctx, "c1", "rt1", "", 0); err != nil {
		t.Fatalf("add chatgpt: %v", err)
	}
	if _, err := mgr.AddChatGPT(ctx, "c2", "rt1", "", 0); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate chatgpt error, got %v", err)
	}
}

func TestExhaustReactivate(t *testing.T) {
	db := setupTestDB(t)
	mgr, _ := NewManager(db)
	ctx := context.Background()
	a, _ := mgr.AddAPIKey(ctx, "a", "k", "", 1)
	reset := time.Now().Add(time.Hour)
	if err := mgr.MarkExhausted(ctx, a.ID, reset); err != nil {
		t.Fatalf("mark: %v", err)
	}
	got, _ := mgr.Get(ctx, a.ID)
	if !got.Exhausted || got.ResetAt.IsZero() {
		t.Fatalf("mark failed: %+v", got)
	}
	if err := mgr.Reactivate(ctx, a.ID); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	got, _ = mgr.Get(ctx, a.ID)
	if got.Exhausted || !got.ResetAt.IsZero() {
		t.Fatalf("reactivate failed: %+v", got)
	}
}
