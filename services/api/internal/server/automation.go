package server

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/automation"
	"mise.tt/api/internal/backoffice"
	"mise.tt/api/internal/forecasting"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/mail"
)

// NewAutomation builds the Sprint 8 automation engine and scheduled
// reports. Reports are emailed only through a real mail service: the "log"
// driver (development) is treated as no email service, so nothing is ever
// recorded as sent when it was only logged.
func NewAutomation(cfg config.Config, pool *pgxpool.Pool, com *Commerce, bo *backoffice.Service, ids *identity.Service, an *analytics.Service, mailer automation.Mailer) (*automation.Service, *automation.Reports) {
	a := automation.NewService(pool)
	a.Alerts, a.Now = bo, func() time.Time { return com.Service.Now() }
	if mailer == nil && cfg.Mail.Driver != "" && cfg.Mail.Driver != "log" {
		if m, err := mail.New(cfg.Mail); err == nil {
			mailer = m
		}
	}
	r := &automation.Reports{S: a, Dashboards: an, Contacts: staffContacts{pool: pool, ids: ids}, Location: menus.Location()}
	if mailer != nil {
		r.Mailer = mailer
	}
	return a, r
}

type staffContacts struct {
	pool *pgxpool.Pool
	ids  *identity.Service
}

func (c staffContacts) StaffEmail(ctx context.Context, userID string) (string, bool, error) {
	who, err := c.ids.ContactOf(ctx, userID)
	if err != nil {
		return "", false, err
	}
	return who.Email, who.Active, nil
}

func (c staffContacts) HasPermission(ctx context.Context, userID, permission string) (bool, error) {
	var ok bool
	err := c.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key = ur.role_key
		WHERE ur.user_id = $1 AND rp.permission_key = $2)`, userID, permission).Scan(&ok)
	return ok, err
}

// NewForecasting builds the Sprint 8 forecasting service on the commerce clock.
func NewForecasting(pool *pgxpool.Pool, com *Commerce) *forecasting.Service {
	f := forecasting.NewService(pool, menus.Location())
	f.Now = func() time.Time { return com.Service.Now() }
	return f
}
