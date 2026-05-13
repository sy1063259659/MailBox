package main

import (
	"context"
	"log"
	"net/http"

	"mailbox-server/internal/api"
	"mailbox-server/internal/config"
	"mailbox-server/internal/session"
	"mailbox-server/internal/staticfiles"
	"mailbox-server/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config failed: %v", err)
	}

	ctx := context.Background()
	database, err := store.New(ctx, cfg.DatabaseURL, cfg.TokenKey)
	if err != nil {
		log.Fatalf("database failed: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	if err := database.EnsureAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("admin init failed: %v", err)
	}

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: staticfiles.Handler(
			cfg.StaticDir,
			api.NewRouter(database, session.NewManager(cfg.SessionSecret)),
		),
	}

	log.Printf("mailbox-imap-server listening on http://%s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server failed: %v", err)
	}
}
