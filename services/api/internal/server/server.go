// Package server assembles the modules into one HTTP handler.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/admin"
	"mise.tt/api/internal/events"
	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/ratelimit"
	"mise.tt/api/internal/platform/token"
	"mise.tt/api/internal/referrals"
)

// NewLeadService builds the lead service from configuration. The worker uses
// it too, for the links that go into emails.
func NewLeadService(cfg config.Config, pool *pgxpool.Pool) *leads.Service {
	return leads.NewService(pool, token.NewSigner(cfg.TokenSigningKey), leads.Options{
		PublicSiteURL:        cfg.PublicSiteURL,
		VerificationRequired: cfg.EmailVerificationRequired,
		ConsentVersion:       cfg.ConsentVersion,
		ReferralMilestones:   cfg.ReferralMilestones,
	})
}

// Limits are per client IP. Signup is the expensive, abusable endpoint and
// gets the tightest budget.
type Limits struct {
	Signup, Token, Lookup, Events, AdminLogin, General *ratelimit.Limiter
}

func DefaultLimits() Limits {
	return Limits{
		Signup:     ratelimit.New(10, time.Hour, 5),
		Token:      ratelimit.New(30, time.Hour, 10),
		Lookup:     ratelimit.New(60, time.Minute, 20),
		Events:     ratelimit.New(120, time.Minute, 40),
		AdminLogin: ratelimit.New(20, 15*time.Minute, 10),
		General:    ratelimit.New(600, time.Minute, 200),
	}
}

func New(cfg config.Config, pool *pgxpool.Pool, limits Limits) http.Handler {
	leadService := NewLeadService(cfg, pool)
	leadHandler := &leads.Handler{Service: leadService}
	referralHandler := &referrals.Handler{DB: pool}
	eventHandler := &events.Handler{DB: pool, ResolveLead: leadService.Authenticate, BearerToken: leads.BearerToken}
	adminHandler := admin.NewHandler(pool, admin.NewAuth(pool, cfg.AdminSessionTTL))

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			httpx.Fail(w, r, httpx.NewError(http.StatusServiceUnavailable, "not_ready", "Database is not reachable."))
			return
		}
		httpx.JSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
	})

	mux.Handle("POST /api/v1/leads", limits.Signup.ByIP(http.HandlerFunc(leadHandler.Create)))
	mux.Handle("POST /api/v1/leads/verify", limits.Token.ByIP(http.HandlerFunc(leadHandler.Verify)))
	mux.Handle("POST /api/v1/leads/unsubscribe", limits.Token.ByIP(http.HandlerFunc(leadHandler.Unsubscribe)))
	mux.HandleFunc("GET /api/v1/leads/me", leadHandler.Me)
	mux.HandleFunc("PATCH /api/v1/leads/preferences", leadHandler.UpdatePreferences)
	mux.Handle("GET /api/v1/referrals/{code}", limits.Lookup.ByIP(http.HandlerFunc(referralHandler.Lookup)))
	mux.Handle("POST /api/v1/events", limits.Events.ByIP(http.HandlerFunc(eventHandler.Ingest)))

	adminHandler.Routes(mux, limits.AdminLogin.ByIP)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.ErrNotFound)
	})

	return httpx.Chain(mux,
		httpx.WithRequestID,
		httpx.WithClientIP(cfg.TrustedProxyHops),
		httpx.WithAccessLog,
		httpx.WithRecover,
		httpx.WithSecurityHeaders(cfg.IsProduction()),
		httpx.WithCORS(cfg.CORSOrigins),
		limits.General.ByIP,
	)
}
