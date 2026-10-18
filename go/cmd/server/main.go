// Command server runs the Cribby API.
//
// It applies pending database migrations on boot (the old Render start command
// ran `alembic upgrade head` before uvicorn), then serves on $PORT.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"campus-backend/internal/api"
	"campus-backend/internal/config"
	"campus-backend/internal/db"
	"campus-backend/internal/email"
	"campus-backend/internal/storage"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply migrations and exit")
	skipMigrate := flag.Bool("skip-migrate", false, "don't apply migrations on boot")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg := config.Load()

	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if !*skipMigrate {
		if err := database.Migrate(context.Background()); err != nil {
			log.Error("migrations failed", "err", err)
			os.Exit(1)
		}
		log.Info("database at head", "revision", db.HeadRevision())
	}
	if *migrateOnly {
		return
	}

	if cfg.SecretKey == "change-me-in-production" {
		log.Warn("SECRET_KEY is the insecure default — set it before deploying")
	}
	srv := api.New(cfg, database,
		email.NewResend(cfg.ResendAPIKey, cfg.EmailFrom, cfg.EmailVerifyExpireMinutes, cfg.PasswordResetExpireMinutes),
		storage.NewCloudinary(cfg.CloudinaryCloudName, cfg.CloudinaryAPIKey, cfg.CloudinaryAPISecret),
		log)

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Info("listening", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
}
