// Package server assembles the modules into one HTTP handler.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/addresses"
	"mise.tt/api/internal/admin"
	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/automation"
	"mise.tt/api/internal/backoffice"
	"mise.tt/api/internal/behavior"
	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/catalog"
	"mise.tt/api/internal/commerce"
	"mise.tt/api/internal/crm"
	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/events"
	"mise.tt/api/internal/favorites"
	"mise.tt/api/internal/finance"
	"mise.tt/api/internal/finance/scenario"
	"mise.tt/api/internal/forecasting"
	"mise.tt/api/internal/fulfillment"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/intelligence"
	"mise.tt/api/internal/intelligence/ai"
	"mise.tt/api/internal/inventory"
	"mise.tt/api/internal/launch"
	"mise.tt/api/internal/leads"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/payments/paypal"
	"mise.tt/api/internal/payments/scotiabank"
	"mise.tt/api/internal/payments/stripe"
	"mise.tt/api/internal/payments/testpay"
	"mise.tt/api/internal/plans"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/platform/crypto"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/ratelimit"
	"mise.tt/api/internal/platform/token"
	"mise.tt/api/internal/procurement"
	"mise.tt/api/internal/production"
	"mise.tt/api/internal/referrals"
	"mise.tt/api/internal/reporting"
	"mise.tt/api/internal/subscriptions"
	"mise.tt/api/internal/suppliers"
	"mise.tt/api/internal/support"
	"mise.tt/api/internal/supportchat"
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
		AdminAppURL:            cfg.AdminAppURL,
	}), nil
}

// NewCatalogService builds the catalogue from configuration. misectl uses it
// too, to load demo content.
func NewCatalogService(cfg config.Config, pool *pgxpool.Pool) *catalog.Service {
	return catalog.NewService(pool, catalog.Options{AllowDemo: cfg.CatalogDemoEnabled, ImageHosts: cfg.CatalogImageHosts})
}

// Commerce is the subscription-commerce services, wired together. The API,
// the worker and misectl build it the same way.
type Commerce struct {
	Plans    *plans.Service
	Menus    *menus.Service
	Subs     *subscriptions.Service
	Billing  *billing.Service
	Payments *payments.Service
	Orders   *orders.Service
	Service  *commerce.Service
	// Testpay is the payment simulator, when PAYMENTS_TEST_PROVIDER is on.
	Testpay *testpay.Provider
}

// NewBehavior builds the customer behaviour service on the commerce clock.
func NewBehavior(pool *pgxpool.Pool, com *Commerce) *behavior.Service {
	b := behavior.NewService(pool)
	b.Now = func() time.Time { return com.Service.Now() }
	return b
}

// NewFinance builds the management finance service on the commerce clock.
func NewFinance(pool *pgxpool.Pool, com *Commerce) *finance.Service {
	f := finance.NewService(pool)
	f.Now = func() time.Time { return com.Service.Now() }
	return f
}

// NewLaunch builds the launch readiness service from configuration.
// NewAssistant is Ask Mise AI over the configured provider (none: AI unavailable).
func NewAssistant(cfg config.Config, box *intelligence.Toolbox) *intelligence.Assistant {
	a := &intelligence.Assistant{Box: box, Model: cfg.AI.Model}
	if cfg.AI.Provider == "openai" {
		a.Provider = ai.Retrying{P: &ai.OpenAI{APIKey: cfg.AI.APIKey, Model: cfg.AI.Model, BaseURL: cfg.AI.APIBase, Timeout: cfg.AI.Timeout},
			Attempts: cfg.AI.Retries + 1, Backoff: time.Second}
	}
	return a
}

func NewLaunch(cfg config.Config, pool *pgxpool.Pool, com *Commerce) *launch.Service {
	name, live, _, ok := com.Payments.Available()
	if !ok {
		name = ""
	}
	l := launch.NewService(pool, launch.Facts{Env: cfg.Env, PaymentProvider: name, PaymentsLive: live, MailDriver: cfg.Mail.Driver,
		MailHost: cfg.Mail.Host, CustomerAppURL: cfg.CustomerAppURL, PublicSiteURL: cfg.PublicSiteURL, DemoContentShown: cfg.CatalogDemoEnabled,
		Payments: com.Payments.Taking})
	l.Now = func() time.Time { return com.Service.Now() }
	return l
}

// NewCommerce builds the commerce services from configuration.
func NewCommerce(cfg config.Config, pool *pgxpool.Pool, ids *identity.Service) (*Commerce, error) {
	policy, err := menus.ParsePolicy(cfg.Commerce.CutoffWeekday, cfg.Commerce.CutoffTime)
	if err != nil {
		return nil, fmt.Errorf("MENU_CUTOFF_WEEKDAY/MENU_CUTOFF_TIME: %w", err)
	}
	billingPolicy := billing.Policy{
		TaxMode: cfg.Commerce.TaxMode, TaxRateBps: cfg.Commerce.TaxRateBps, TaxLabel: cfg.Commerce.TaxLabel,
		TaxRegistration: cfg.Commerce.TaxRegistration, DeliveryFeeMinor: cfg.Commerce.DeliveryFeeMinor,
		BusinessName: cfg.Commerce.BusinessName, BusinessAddress: cfg.Commerce.BusinessAddress,
		RenewalLead: cfg.Commerce.RenewalLead, RetryLimit: cfg.Commerce.RenewalRetries, RetryAfter: cfg.Commerce.RenewalRetryAfter,
	}
	if err := billingPolicy.Validate(); err != nil {
		return nil, err
	}
	catalogService := NewCatalogService(cfg, pool)
	c := &Commerce{
		Plans:   plans.NewService(pool, plans.Options{AllowDemo: cfg.CatalogDemoEnabled}),
		Menus:   menus.NewService(pool, catalogService, policy),
		Subs:    subscriptions.NewService(pool, policy, subscriptions.Options{RenewalLead: cfg.Commerce.RenewalLead, MaxPauseWeeks: cfg.Commerce.MaxPauseWeeks}),
		Billing: billing.NewService(pool, billingPolicy),
		Orders:  orders.NewService(pool),
	}
	var providers []payments.Provider
	if cfg.Commerce.TestProvider {
		if cfg.IsProduction() {
			return nil, errors.New("the test payment provider cannot run in production")
		}
		c.Testpay = testpay.New(pool, cfg.Commerce.TestProviderSecret, cfg.CustomerAppURL+"/test-checkout")
		providers = append(providers, c.Testpay)
	}
	// Sprint 9.1: real providers, registered only when configured; Scotiabank
	// is always listed so staff can see that it is blocked and why.
	if cfg.Commerce.Stripe.SecretKey != "" {
		providers = append(providers, stripe.New(cfg.Commerce.Stripe, nil))
	}
	if cfg.Commerce.PayPal.ClientID != "" {
		providers = append(providers, paypal.New(cfg.Commerce.PayPal, nil))
	}
	providers = append(providers, scotiabank.New(cfg.Commerce.Scotiabank))
	c.Payments = payments.NewService(pool, providers...)
	c.Payments.Production = cfg.IsProduction()
	if c.Testpay != nil {
		p := c.Payments
		// The simulator "sends" its webhooks straight into the same code a
		// real provider's HTTP request reaches.
		c.Testpay.Deliver = func(ctx context.Context, header http.Header, body []byte) error {
			_, err := p.ReceiveWebhook(ctx, testpay.Name, header, body)
			return err
		}
	}
	svc := commerce.New(pool, commerce.Options{CustomerAppURL: cfg.CustomerAppURL, PendingTTL: cfg.Commerce.PendingTTL})
	svc.Plans, svc.Menus, svc.Subs, svc.Billing, svc.Payments, svc.Orders = c.Plans, c.Menus, c.Subs, c.Billing, c.Payments, c.Orders
	svc.Catalog, svc.Customers, svc.Identity, svc.Delivery = catalogService, customers.NewService(pool, ids), ids, delivery.Zones{DB: pool}
	c.Service = svc
	c.Menus.Usage = c.Subs
	c.Menus.Hooks = commerce.MenuHooks{S: svc}
	c.Billing.Issuer = c.Subs
	c.Payments.Hooks = commerce.Hooks{S: svc}
	return c, nil
}

// SeedDemoMenus creates and publishes menus for the next two open weeks
// from the visible demo meals, unless those weeks already have a menu. It
// returns the weeks it published.
func SeedDemoMenus(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, c *Commerce) ([]string, error) {
	page, err := NewCatalogService(cfg, pool).List(ctx, catalog.Filter{PageSize: 24})
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, errors.New("no published meals: run misectl catalog seed-demo first")
	}
	var meals []menus.MealInput
	for _, m := range page.Items {
		meals = append(meals, menus.MealInput{MealID: m.ID})
	}
	var done []string
	week := c.Menus.Policy.NextOpenWeek(c.Menus.Now())
	for i := 0; i < 2; i++ {
		w := week.AddDate(0, 0, 7*i)
		if info, err := c.Menus.ForWeek(ctx, pool, w); err != nil {
			return done, err
		} else if info != nil {
			continue
		}
		m, err := c.Menus.Create(ctx, "", menus.Input{Week: menus.FormatWeek(w), Title: "Demo menu", Meals: meals})
		if err != nil {
			return done, err
		}
		if _, err := c.Menus.Publish(ctx, "", m.ID, nil); err != nil {
			return done, err
		}
		done = append(done, menus.FormatWeek(w))
	}
	return done, nil
}

// NewBackoffice builds the Sprint 4 administration services.
func NewBackoffice(cfg config.Config, pool *pgxpool.Pool, ids *identity.Service, com *Commerce) *backoffice.Service {
	b := backoffice.New(pool)
	b.Reporting = reporting.New(pool)
	// One clock for commerce and its reports (tests move it).
	b.Now = func() time.Time { return com.Service.Now() }
	b.Reporting.Now = b.Now
	b.CRM = crm.NewService(pool)
	b.Support = support.NewService(pool)
	b.Support.Alerts = b
	b.Commerce, b.Subs, b.Orders, b.Billing, b.Payments = com.Service, com.Subs, com.Orders, com.Billing, com.Payments
	b.Identity, b.Delivery = ids, delivery.Zones{DB: pool}
	provider, live, _, _ := com.Payments.Available()
	b.Integ = backoffice.Integrations{Env: cfg.Env, MailDriver: cfg.Mail.Driver, MailHost: cfg.Mail.Host, PaymentsProvider: provider, PaymentsLive: live}
	mfa := "Required for every staff account"
	if !cfg.AdminMFARequired {
		mfa = "Not required (local development only)"
	}
	payments := "None chosen"
	if provider != "" {
		payments = provider
		if !live {
			payments += " (test mode, no real money)"
		}
	}
	b.Policies = []backoffice.Policy{
		{Key: "currency", Label: "Currency", Value: "TTD", Owner: "Code"},
		{Key: "time_zone", Label: "Business time zone", Value: menus.TimeZone, Owner: "Code"},
		{Key: "cutoff", Label: "Meal selection cutoff", Value: cfg.Commerce.CutoffWeekday + " " + cfg.Commerce.CutoffTime + ", Trinidad and Tobago time", Owner: "Configuration (MENU_CUTOFF_*)"},
		{Key: "renewal", Label: "Weekly billing", Value: "Charged " + cfg.Commerce.RenewalLead.String() + " before each week's cutoff", Owner: "Configuration (BILLING_RENEWAL_LEAD)"},
		{Key: "tax", Label: "Tax", Value: cfg.Commerce.TaxMode, Owner: "Configuration (BILLING_TAX_*), not decided"},
		{Key: "delivery_fee", Label: "Delivery fee (cents per week)", Value: strconv.FormatInt(cfg.Commerce.DeliveryFeeMinor, 10), Owner: "Configuration, not decided"},
		{Key: "pause", Label: "Longest pause", Value: strconv.Itoa(cfg.Commerce.MaxPauseWeeks) + " weeks", Owner: "Configuration"},
		{Key: "payments", Label: "Payment provider", Value: payments, Owner: "Configuration"},
		{Key: "mfa", Label: "Staff two-factor sign-in", Value: mfa, Owner: "Configuration (ADMIN_MFA_REQUIRED)"},
		{Key: "registration", Label: "Customer registration", Value: map[bool]string{true: "Open", false: "Closed"}[cfg.CustomerRegistrationEnabled], Owner: "Configuration"},
		{Key: "delivery_zones", Label: "Delivery zones", Value: "Managed in the database (delivery_zones); all 15 regions active", Owner: "Database"},
	}
	return b
}

// NewKitchen builds the Sprint 5 culinary and production services. They
// share the commerce clock (tests move it) and the business calendar.
func NewKitchen(cfg config.Config, pool *pgxpool.Pool, com *Commerce, alerts production.Alerts) (*culinary.Service, *production.Service) {
	now := func() time.Time { return com.Service.Now() }
	cul := culinary.NewService(pool)
	cul.Catalog, cul.Weeks, cul.Now = NewCatalogService(cfg, pool), com.Menus.Policy, now
	prod := production.NewService(pool)
	prod.Culinary, prod.Orders, prod.Alerts, prod.Weeks, prod.Location, prod.Now = cul, com.Orders, alerts, com.Menus.Policy, menus.Location(), now
	return cul, prod
}

// RegisterCommerceJobs wires the commerce and payment job kinds.
func RegisterCommerceJobs(r *jobs.Runner, c *Commerce) {
	r.Register(payments.JobProcessWebhook, c.Payments.ProcessWebhook)
	r.Register(payments.JobCancelCheckout, c.Payments.CancelCheckoutJob)
	r.Register(commerce.JobCharge, c.Service.HandleCharge)
}

// SetClock makes every commerce service read the time from now (tests).
func (c *Commerce) SetClock(now func() time.Time) {
	c.Plans.Now, c.Menus.Now, c.Subs.Now, c.Billing.Now, c.Payments.Now, c.Orders.Now, c.Service.Now = now, now, now, now, now, now, now
	if c.Testpay != nil {
		c.Testpay.Now = now
	}
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
	com, err := NewCommerce(cfg, pool, ids)
	if err != nil {
		return nil, nil, err
	}
	return BuildWith(cfg, pool, limits, ids, com)
}

// BuildWith is Build with services made by the caller (tests share them
// with the job runner and control their clock).
func BuildWith(cfg config.Config, pool *pgxpool.Pool, limits Limits, ids *identity.Service, com *Commerce) (http.Handler, []string, error) {
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
	identityHandler.StaffRoutes(mux, limits.AdminLogin.ByIP)
	adminHandler.Routes(mux)
	(&customers.Handler{Service: customerService}).Routes(mux)
	(&addresses.Handler{Pool: pool, Identity: ids, Customers: customerService, Delivery: delivery.Zones{DB: pool}, InUse: com.Subs}).Routes(mux)
	(&catalog.Handler{Service: catalogService, Identity: ids, Tastes: customerService}).Routes(mux)
	(&favorites.Handler{Pool: pool, Identity: ids, Customers: customerService, Catalog: catalogService}).Routes(mux)
	(&plans.Handler{Service: com.Plans, Identity: ids}).Routes(mux)
	(&menus.Handler{Service: com.Menus, Identity: ids}).Routes(mux)
	(&payments.Handler{Service: com.Payments, Identity: ids}).Routes(mux)
	(&commerce.Handler{S: com.Service, Limit: limits.Token.ByIP}).Routes(mux)
	if com.Testpay != nil {
		com.Testpay.Routes(mux, limits.Token.ByIP)
	}
	bo := NewBackoffice(cfg, pool, ids, com)
	(&backoffice.Handler{S: bo}).Routes(mux)
	cul, prod := NewKitchen(cfg, pool, com, bo)
	(&culinary.Handler{S: cul, Identity: ids}).Routes(mux)
	(&production.Handler{S: prod, Identity: ids}).Routes(mux)
	sc := NewSupplyChain(cfg, pool, com, bo, prod)
	(&suppliers.Handler{S: sc.Suppliers, Identity: ids}).Routes(mux)
	(&procurement.Handler{S: sc.Procurement, Identity: ids}).Routes(mux)
	(&inventory.Handler{S: sc.Inventory, Identity: ids}).Routes(mux)
	ff := NewFulfilment(cfg, pool, com, bo, prod)
	(&fulfillment.Handler{S: ff.Fulfillment, Identity: ids}).Routes(mux)
	(&delivery.Handler{S: ff.Delivery, Identity: ids}).Routes(mux)
	(&delivery.CustomerHandler{S: ff.Delivery, Identity: ids, Customers: customerService, Orders: com.Orders}).Routes(mux)
	an := NewAnalytics(pool, com)
	(&analytics.Handler{S: an, Identity: ids}).Routes(mux)
	au, reports := NewAutomation(cfg, pool, com, bo, ids, an, nil)
	(&automation.Handler{S: au, R: reports, Identity: ids}).Routes(mux)
	fc := NewForecasting(pool, com)
	(&forecasting.Handler{S: fc, Identity: ids}).Routes(mux)
	(&launch.Handler{S: NewLaunch(cfg, pool, com), Identity: ids}).Routes(mux)
	chat := supportchat.NewService(pool)
	chat.Alerts, chat.Cases = bo, bo.Support
	if cfg.RateLimitsDisabled {
		chat.SendLimit = nil // local automated test runs only (config refuses it elsewhere)
	}
	(&supportchat.Handler{S: chat, Identity: ids, Customers: customerService, StartLimit: limits.Signup.ByIP}).Routes(mux)
	fin, bx := NewFinance(pool, com), NewBehavior(pool, com)
	(&finance.Handler{S: fin, Identity: ids}).Routes(mux)
	box := &intelligence.Toolbox{Pool: pool, Analytics: an, Finance: fin, Behavior: bx, Forecasting: fc,
		Scenarios: &scenario.Service{Pool: pool, Finance: fin, Now: func() time.Time { return com.Service.Now() }},
		Now:       func() time.Time { return com.Service.Now() }}
	asst := NewAssistant(cfg, box)
	box.AI = asst.Status
	(&intelligence.Handler{Box: box, Assistant: asst, Identity: ids}).Routes(mux)
	(&behavior.Handler{S: bx, Identity: ids, Customers: customerService, Limit: limits.Events.ByIP}).Routes(mux)

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
