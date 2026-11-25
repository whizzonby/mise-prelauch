package server

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/backoffice"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/fulfillment"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/production"
)

// Fulfilment holds the Sprint 7 services: fulfillment and delivery. They
// share the commerce clock (tests move it) and the business time zone.
type Fulfilment struct {
	Fulfillment *fulfillment.Service
	Delivery    *delivery.Service
}

// NewFulfilment builds the fulfillment and delivery services.
func NewFulfilment(cfg config.Config, pool *pgxpool.Pool, com *Commerce, bo *backoffice.Service, prod *production.Service) *Fulfilment {
	now := func() time.Time { return com.Service.Now() }
	dl := delivery.NewService(pool)
	dl.Orders, dl.Alerts, dl.Cases, dl.Now, dl.Location = com.Orders, bo, bo.Support, now, menus.Location()
	// Confirmation codes are derived from the token signing key; they are
	// never stored.
	dl.CodeKey = cfg.TokenSigningKey
	dl.Metrics = NewAnalytics(pool, com)
	fl := fulfillment.NewService(pool)
	fl.Orders, fl.Production, fl.Delivery, fl.Now, fl.Location = com.Orders, prod, dl, now, menus.Location()
	dl.Fulfillment = fl
	return &Fulfilment{Fulfillment: fl, Delivery: dl}
}
