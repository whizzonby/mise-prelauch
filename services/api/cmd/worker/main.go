// Command worker runs background jobs (transactional email, payment webhook
// processing, renewal charges) and the commerce scheduler.
package main

import (
	"mise.tt/api/internal/appnotify"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/automation"
	"mise.tt/api/internal/backoffice"
	"mise.tt/api/internal/fulfillment"
	"mise.tt/api/internal/intelligence"
	"mise.tt/api/internal/launch"
	"mise.tt/api/internal/notifications"
	"mise.tt/api/internal/platform/buildinfo"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
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
	// Sprint 10: the app's notification centre and push. Push stays off until
	// a Firebase service account is configured (FCM_SERVICE_ACCOUNT_FILE).
	inbox := appnotify.New(pool, nil)
	if fcm, err := appnotify.LoadFCM(cfg.FCMServiceAccountFile); err != nil {
		return err
	} else if fcm != nil {
		inbox.Pusher = fcm
	}
	inbox.Register(runner)
	(&notifications.CommerceSender{DB: pool, Identity: ids, Sender: sender, Inbox: inbox}).Register(runner)
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
	(&notifications.DeliverySender{DB: pool, Identity: ids, Sender: sender, AppURL: cfg.CustomerAppURL, Inbox: inbox}).Register(runner)
	// Sprint 9.1: "you have a reply" chat emails (one per conversation per window).
	(&notifications.ChatSender{DB: pool, Identity: ids, Sender: sender, AppURL: cfg.CustomerAppURL, Inbox: inbox}).Register(runner)
	go ff.Fulfillment.RunDispatcher(ctx, cfg.Commerce.SchedulerInterval)
	// Sprint 8: analytics snapshots (hourly, on events, idempotent).
	an := server.NewAnalytics(pool, com)
	runner.Register(analytics.JobRefresh, an.HandleRefresh)
	runner.Register(analytics.JobEvent, an.HandleEvent)
	// Sprint 8: automation rules (scheduled and on events) and scheduled
	// reports, in the same loop; every action runs as a retried job.
	var reportMailer automation.Mailer
	if cfg.Mail.Driver != "log" {
		reportMailer = mailer
	}
	au, reports := server.NewAutomation(cfg, pool, com, bo, ids, an, reportMailer)
	// Sprint 9.2: intelligence runs past the retention setting are deleted.
	runsRetention := &intelligence.Assistant{Box: &intelligence.Toolbox{Pool: pool, Now: time.Now}}
	// Sprint 9.2: executive briefings on approved schedules (disabled until configured).
	briefings := server.NewBriefings(cfg, pool, com, an, ids, reportMailer)
	runner.Register(intelligence.JobBriefingEmail, briefings.Handle)
	runner.Register(automation.JobExecute, au.HandleExecute)
	runner.Register(automation.JobEvent, au.HandleEvent)
	runner.Register(automation.JobReportEmail, reports.HandleEmail)
	go func() {
		t := time.NewTicker(cfg.Commerce.SchedulerInterval)
		defer t.Stop()
		for {
			if err := an.Tick(ctx, 30*time.Second); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "analytics tick failed", "error", err.Error())
			}
			if err := au.Tick(ctx, 30*time.Second); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "automation tick failed", "error", err.Error())
			}
			if _, err := reports.Due(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "scheduled reports failed", "error", err.Error())
			}
			if _, err := briefings.Box.DueBriefings(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "scheduled briefings failed", "error", err.Error())
			}
			if n, err := runsRetention.Purge(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "intelligence retention failed", "error", err.Error())
			} else if n > 0 {
				slog.InfoContext(ctx, "intelligence runs past retention deleted", "count", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	// Liveness for the system status page: a worker that stops beating is shown as stopped.
	host, _ := os.Hostname()
	go launch.RunHeartbeat(ctx, pool, "worker@"+host)

	slog.Info("worker started", "mail_driver", cfg.Mail.Driver, "test_payments", cfg.Commerce.TestProvider, "version", buildinfo.Version)
	err = runner.Run(ctx)
	slog.Info("worker stopped")
	return err
}
