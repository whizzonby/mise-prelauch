// Package server assembles the modules into one HTTP handler.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/addresses"
	"mise.tt/api/internal/admin"
	"mise.tt/api/internal/catalog"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/events"
	"mise.tt/api/internal/favorites"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/crypto"
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

// NewIdentityService builds the identity service from configuration. The
// worker and misectl use it too.
func NewIdentityService(cfg config.Config, pool *pgxpool.Pool) (*identity.Service, error) {
	sealer, err := crypto.NewSealer(cfg.DataEncryptionKey)
	if err != nil {
		return nil, err
	}
	signer := token.NewSigner(cfg.TokenSigningKey)
	return identity.NewService(pool, sealer, signer.HashIP, customers.Hooks{}, identity.Options{
		AdminSessionTTL:        cfg.AdminSessionTTL,
		AdminSessionIdleTTL:    cfg.AdminSessionIdleTTL,
		CustomerSessionTTL:     cfg.CustomerSessionTTL,
		CustomerSessionIdleTTL: cfg.CustomerSessionIdleTTL,
		AdminMFARequired:       cfg.AdminMFARequired,
		RegistrationEnabled:    cfg.CustomerRegistrationEnabled,
		TermsVersion:           cfg.TermsVersion,
		CustomerAppURL:         cfg.CustomerAppURL,
	}), nil
}

// NewCatalogService builds the catalogue from configuration. misectl uses it
// too, to load demo content.
func NewCatalogService(cfg config.Config, pool *pgxpool.Pool) *catalog.Service {
	return catalog.NewService(pool, catalog.Options{AllowDemo: cfg.CatalogDemoEnabled, ImageHosts: cfg.CatalogImageHosts})
}

// Limits are per client IP. Signup is the expensive, abusable endpoint and
// gets the tightest budget. AdminLogin is kept as the name of the budget for
// every password and registration endpoint (customer and admin).
type Limits struct {
	Signup, Token, Lookup, Events, AdminLogin, General *ratelimit.Limiter
}

// LimitsFor returns the production limits, or effectively none when the local
// environment has asked for them to be off.
func LimitsFor(cfg config.Config) Limits {
	if !cfg.RateLimitsDisabled {
		return DefaultLimits()
	}
	open := func() *ratelimit.Limiter { return ratelimit.New(1_000_000, time.Minute, 1_000_000) }
	return Limits{Signup: open(), Token: open(), Lookup: open(), Events: open(), AdminLogin: open(), General: open()}
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

// recorder registers routes on a ServeMux and remembers their patterns.
type recorder struct {
	mux      *http.ServeMux
	patterns []string
}

func (r *recorder) Handle(pattern string, h http.Handler) {
	r.patterns = append(r.patterns, pattern)
	r.mux.Handle(pattern, h)
}

func (r *recorder) HandleFunc(pattern string, h http.HandlerFunc) { r.Handle(pattern, h) }

// New builds the API's HTTP handler.
func New(cfg config.Config, pool *pgxpool.Pool, limits Limits) (http.Handler, error) {
	h, _, err := Build(cfg, pool, limits)
	return h, err
}

// Build is New, also returning every registered route pattern ("METHOD /path").
func Build(cfg config.Config, pool *pgxpool.Pool, limits Limits) (http.Handler, []string, error) {
	ids, err := NewIdentityService(cfg, pool)
	if err != nil {
		return nil, nil, err
	}
	leadService := NewLeadService(cfg, pool)
	leadHandler := &leads.Handler{Service: leadService}
	referralHandler := &referrals.Handler{DB: pool}
	eventHandler := &events.Handler{DB: pool, ResolveLead: leadService.Authenticate, BearerToken: leads.BearerToken}
	identityHandler := identity.NewHandler(ids)
	if cfg.RateLimitsDisabled {
		identityHandler.OpenAccountLimits()
	}
	adminHandler := admin.NewHandler(pool, ids)
	customerService := customers.NewService(pool, ids)
	catalogService := NewCatalogService(cfg, pool)

	mux := &recorder{mux: http.NewServeMux()}

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

	identityHandler.Routes(mux, identity.Limits{Auth: limits.AdminLogin.ByIP, Token: limits.Token.ByIP})
	adminHandler.Routes(mux)
	(&customers.Handler{Service: customerService}).Routes(mux)
	(&addresses.Handler{Pool: pool, Identity: ids, Customers: customerService, Delivery: delivery.Zones{DB: pool}}).Routes(mux)
	(&catalog.Handler{Service: catalogService, Identity: ids, Tastes: customerService}).Routes(mux)
	(&favorites.Handler{Pool: pool, Identity: ids, Customers: customerService, Catalog: catalogService}).Routes(mux)

	mux.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.ErrNotFound)
	})

	return httpx.Chain(mux.mux,
		httpx.WithRequestID,
		httpx.WithClientIP(cfg.TrustedProxyHops, cfg.BFFSharedKey),
		httpx.WithAccessLog,
		httpx.WithRecover,
		httpx.WithSecurityHeaders(cfg.IsProduction()),
		httpx.WithCORS(cfg.CORSOrigins),
		limits.General.ByIP,
	), mux.patterns, nil
}
