package main

import (
	"context"
	"database/sql"
	stdlog "log"
	"net/http"
	"os"
	"time"

	"codex-companion/internal/account"
	logstore "codex-companion/internal/log"
	"codex-companion/internal/proxy"
	"codex-companion/internal/scheduler"
	"codex-companion/internal/webui"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "companion.db")
	if err != nil {
		stdlog.Fatalf("open db: %v", err)
	}
	defer db.Close()

	am, err := account.NewManager(db)
	if err != nil {
		stdlog.Fatalf("account manager: %v", err)
	}
	ls, err := logstore.NewStore(db)
	if err != nil {
		stdlog.Fatalf("log store: %v", err)
	}
	sched := scheduler.New(am)
	ctx := context.Background()
	sched.StartReactivator(ctx, time.Minute)

	adminHandler := webui.AdminHandler(am, ls)
	proxyHandler := proxy.New(sched, ls, "https://api.openai.com", "https://chatgpt.com/backend-api/codex")

	mux := http.NewServeMux()
	mux.Handle("/admin/", adminHandler)
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})
	mux.Handle("/", proxyHandler)

	addr := "127.0.0.1:8080"
	if v := os.Getenv("CODEX_COMPANION_ADDR"); v != "" {
		addr = v
	}
	stdlog.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		stdlog.Fatal(err)
	}
}
