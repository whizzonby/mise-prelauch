// Command worker runs background jobs: today, transactional email.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"mise.tt/api/internal/notifications"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
	"mise.tt/api/internal/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(httpx.NewLogger(os.Stdout, cfg.LogLevel, "worker", cfg.Env))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	mailer, err := mail.New(cfg.Mail)
	if err != nil {
		return err
	}

	runner := jobs.NewRunner(pool)
	sender := &notifications.Sender{
		DB:      pool,
		Mailer:  mailer,
		Links:   server.NewLeadService(cfg, pool),
		SiteURL: cfg.PublicSiteURL,
	}
	sender.Register(runner)

	slog.Info("worker started", "mail_driver", cfg.Mail.Driver)
	err = runner.Run(ctx)
	slog.Info("worker stopped")
	return err
}
