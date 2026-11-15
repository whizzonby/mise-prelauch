package server

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/menus"
)

// NewAnalytics builds the Sprint 8 analytics service on the commerce clock.
func NewAnalytics(pool *pgxpool.Pool, com *Commerce) *analytics.Service {
	a := analytics.NewService(pool)
	a.Now, a.Location = func() time.Time { return com.Service.Now() }, menus.Location()
	return a
}
