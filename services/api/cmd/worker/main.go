// Command worker runs background jobs (transactional email, payment webhook
// processing, renewal charges) and the commerce scheduler.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"mise.tt/api/internal/backoffice"
	"mise.tt/api/internal/notifications"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
	"mise.tt/api/internal/fulfillment"
	"mise.tt/api/internal/procurement"
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

	ids, err := server.NewIdentityService(cfg, pool)
	if err != nil {
		return err
	}
	(&notifications.AccountSender{Accounts: ids, Sender: sender}).Register(runner)
	(&notifications.StaffInviteSender{Staff: ids, Sender: sender}).Register(runner)

	com, err := server.NewCommerce(cfg, pool, ids)
	if err != nil {
		return err
	}
	(&notifications.CommerceSender{DB: pool, Identity: ids, Sender: sender}).Register(runner)
	server.RegisterCommerceJobs(runner, com)
	// The commerce scheduler: weekly cycles, renewals, cutoffs, reminders.
	// Safe on several workers (it takes a database advisory lock).
	go com.Service.RunScheduler(ctx, cfg.Commerce.SchedulerInterval)
	// Staff alerts and CSV exports (Sprint 4).
	bo := server.NewBackoffice(cfg, pool, ids, com)
	runner.Register(backoffice.JobExport, bo.HandleExport)
	go bo.RunAlerts(ctx, cfg.Commerce.SchedulerInterval)
	// Sprint 6: released production plans become procurement runs. The
	// dispatcher hands production.plan_released events to the job queue; the
	// handler is idempotent and retried, and failures stay visible.
	_, prod := server.NewKitchen(cfg, pool, com, bo)
	sc := server.NewSupplyChain(cfg, pool, com, bo, prod)
	runner.Register(procurement.JobPlanReleased, sc.Procurement.HandlePlanReleased)
	go sc.Procurement.RunDispatcher(ctx, cfg.Commerce.SchedulerInterval)
	// Sprint 7: production.ready_for_fulfillment events become fulfillment
	// obligations (idempotent per order, retried, failures visible).
	ff := server.NewFulfilment(cfg, pool, com, bo, prod)
	runner.Register(fulfillment.JobBatchReady, ff.Fulfillment.HandleBatchReady)
	(&notifications.DeliverySender{DB: pool, Identity: ids, Sender: sender, AppURL: cfg.CustomerAppURL}).Register(runner)
	go ff.Fulfillment.RunDispatcher(ctx, cfg.Commerce.SchedulerInterval)

	slog.Info("worker started", "mail_driver", cfg.Mail.Driver, "test_payments", cfg.Commerce.TestProvider)
	err = runner.Run(ctx)
	slog.Info("worker stopped")
	return err
}
