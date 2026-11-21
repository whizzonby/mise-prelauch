// Package config loads all runtime configuration from environment variables.
package config

import (
	"regexp"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env      string // local, staging, production
	HTTPAddr string
	LogLevel slog.Level

	DatabaseURL string

	// PublicSiteURL is the marketing site origin, used to build links in emails.
	PublicSiteURL string
	CORSOrigins   []string
	// TrustedProxyHops is how many proxies sit in front of the API and append to
	// X-Forwarded-For. 0 means use the socket address.
	TrustedProxyHops int

	// TokenSigningKey signs lead tokens and keys the IP hash. At least 32 bytes.
	TokenSigningKey []byte

	EmailVerificationRequired bool
	// ReferralMilestones lists converted-referral counts that trigger an email.
	// Empty disables milestone emails.
	ReferralMilestones []int
	ConsentVersion     string

	Mail MailConfig

	AdminSessionTTL time.Duration
	// AdminSessionIdleTTL ends an admin session that has not been used for this long.
	AdminSessionIdleTTL time.Duration
	// AdminMFARequired makes every account holding admin.access present a TOTP
	// code. It cannot be turned off in production.
	AdminMFARequired bool

	// CustomerAppURL is the customer web app origin, used in account emails.
	CustomerAppURL string
	// AdminAppURL is the admin console origin, used in staff invitations.
	AdminAppURL string
	// MobilePaymentReturnURL is where hosted checkout sends the customer back
	// to the Mise app (Sprint 10), e.g. mise://checkout/return. Set by the
	// server only; the app never supplies a return URL. Empty: checkout from
	// the app is unavailable.
	MobilePaymentReturnURL string
	// FCMServiceAccountFile is the path of a Google service account JSON used
	// by the worker to send push notifications. Server-side only; empty: no push.
	FCMServiceAccountFile string
	// CustomerRegistrationEnabled opens POST /api/v1/auth/register. Off by
	// default outside local: opening sign-ups is a business decision.
	CustomerRegistrationEnabled bool
	CustomerSessionTTL          time.Duration
	CustomerSessionIdleTTL      time.Duration
	// TermsVersion is recorded against each customer at registration.
	TermsVersion string

	// CatalogDemoEnabled shows demo meals to customers and lets staff publish
	// them. On by default in local only: demo content must never reach real
	// customers by accident.
	CatalogDemoEnabled bool
	// CatalogImageHosts are https hosts meal photographs may come from, besides
	// the customer app's own /catalog/ folder.
	CatalogImageHosts []string

	// BFFSharedKey authenticates the Next.js servers when they pass on the end
	// user's address and browser (httpx.HeaderBFFKey). Empty disables that.
	BFFSharedKey string

	// DataEncryptionKey seals secrets stored in the database (TOTP secrets).
	// Optional: when unset it is derived from TokenSigningKey (see Load).
	DataEncryptionKey []byte

	// RateLimitsDisabled turns off per-IP and per-account limits so end-to-end tests can sign up
	// repeatedly. Refused outside the local environment.
	RateLimitsDisabled bool

	// Sprint 3: subscription commerce.
	Commerce CommerceConfig

	// Sprint 9.2: the language-model provider for Mise Intelligence.
	AI AIConfig
}

// AIConfig: the language-model provider. Credentials come only from the
// environment. Unset provider = AI unavailable; every deterministic view
// still works. Limits, budget and prices are owner settings in the database.
type AIConfig struct {
	Provider string // "" (none) or "openai"
	Model    string // required with a provider; never hardcoded
	APIKey   string
	APIBase  string // tests only; default https://api.openai.com/v1
	Timeout  time.Duration
	Retries  int
}

// CommerceConfig holds the subscription, billing and payment settings.
// Several of these encode business decisions that are still open; each is
// documented in .env.example.
// StripeConfig: Stripe Checkout and off-session charges.
type StripeConfig struct {
	SecretKey     string // sk_test_… or sk_live_…
	WebhookSecret string // whsec_…
	// Currencies this merchant account may charge, confirmed with Stripe.
	// Empty: Stripe is never offered.
	Currencies []string
	APIBase    string // tests only; default https://api.stripe.com
}

// PayPalConfig: PayPal Orders v2 (one-time payments).
type PayPalConfig struct {
	ClientID     string
	ClientSecret string
	Env          string // sandbox or live
	WebhookID    string
	// Currencies this merchant account may charge, from PayPal's supported list.
	Currencies []string
	APIBase    string // tests only
}

// ScotiabankConfig: registration only. No public integration specification
// exists, so the adapter never processes payments (see docs/integrations/SCOTIABANK.md).
type ScotiabankConfig struct {
	MerchantID string
}

type CommerceConfig struct {
	// Selection cutoff: weekday and local time (Trinidad and Tobago).
	CutoffWeekday string
	CutoffTime    string
	// Tax: none, inclusive or exclusive; rate in basis points.
	TaxMode         string
	TaxRateBps      int64
	TaxLabel        string
	TaxRegistration string
	// Delivery fee per week, in minor units.
	DeliveryFeeMinor  int64
	BusinessName      string
	BusinessAddress   string
	RenewalLead       time.Duration
	RenewalRetries    int
	RenewalRetryAfter time.Duration
	MaxPauseWeeks     int
	PendingTTL        time.Duration
	// TestProvider enables testpay, the payment simulator. Refused in production.
	TestProvider bool
	// TestProviderSecret signs testpay webhooks.
	TestProviderSecret []byte

	// Sprint 9.1 providers. Credentials come only from the environment (or
	// the secret store that fills it); they are never stored in the database.
	Stripe     StripeConfig
	PayPal     PayPalConfig
	Scotiabank ScotiabankConfig
	// SchedulerInterval: how often the worker runs the commerce scheduler.
	SchedulerInterval time.Duration
}

type MailConfig struct {
	Driver   string // smtp or log
	Host     string
	Port     int
	Username string
	Password string
	StartTLS bool
	From     string
}

// A custom app scheme with the fixed checkout path, or an https URL.
var mobileReturnRe = regexp.MustCompile(`^([a-z][a-z0-9+.-]{1,30}://checkout/return|https://[A-Za-z0-9.-]+(:[0-9]+)?/[A-Za-z0-9/_-]*)$`)

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration and fails loudly on anything missing or unsafe, so a
// misconfigured deployment never starts.
func Load() (Config, error) {
	var errs []error
	need := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", key))
		}
		return v
	}

	cfg := Config{
		Env:           get("MISE_ENV", "local"),
		HTTPAddr:      get("HTTP_ADDR", ":8090"),
		DatabaseURL:   databaseURL(need),
		PublicSiteURL: strings.TrimRight(get("PUBLIC_SITE_URL", "http://localhost:3100"), "/"),
		CORSOrigins:   splitList(get("CORS_ORIGINS", "http://localhost:3100")),

		TokenSigningKey: []byte(need("TOKEN_SIGNING_KEY")),

		ConsentVersion: get("CONSENT_VERSION", "2026-09"),
		TermsVersion:   get("TERMS_VERSION", "draft-2026-11"),
		BFFSharedKey:   strings.TrimSpace(os.Getenv("BFF_SHARED_KEY")),
		CustomerAppURL: strings.TrimRight(get("CUSTOMER_APP_URL", "http://localhost:3102"), "/"),
		AdminAppURL:    strings.TrimRight(get("ADMIN_APP_URL", "http://localhost:3101"), "/"),
		MobilePaymentReturnURL: get("MOBILE_PAYMENT_RETURN_URL", ""),
		FCMServiceAccountFile:  get("FCM_SERVICE_ACCOUNT_FILE", ""),

		Mail: MailConfig{
			Driver:   get("MAIL_DRIVER", "log"),
			Host:     get("SMTP_HOST", "localhost"),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     get("MAIL_FROM", "Mise <hello@mise.tt>"),
		},
	}

	switch cfg.Env {
	case "local", "staging", "production":
	default:
		errs = append(errs, fmt.Errorf("MISE_ENV must be local, staging or production, got %q", cfg.Env))
	}

	if err := cfg.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	var err error
	if cfg.TrustedProxyHops, err = getInt("TRUSTED_PROXY_HOPS", 0); err != nil {
		errs = append(errs, err)
	}
	if cfg.Mail.Port, err = getInt("SMTP_PORT", 1026); err != nil {
		errs = append(errs, err)
	}
	if cfg.EmailVerificationRequired, err = getBool("EMAIL_VERIFICATION_REQUIRED", true); err != nil {
		errs = append(errs, err)
	}
	if cfg.Mail.StartTLS, err = getBool("SMTP_STARTTLS", false); err != nil {
		errs = append(errs, err)
	}
	if cfg.RateLimitsDisabled, err = getBool("RATE_LIMITS_DISABLED", false); err != nil {
		errs = append(errs, err)
	}
	if cfg.AdminSessionTTL, err = getDuration("ADMIN_SESSION_TTL", 12*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.AdminSessionIdleTTL, err = getDuration("ADMIN_SESSION_IDLE_TTL", 2*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.CustomerSessionTTL, err = getDuration("CUSTOMER_SESSION_TTL", 30*24*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.CustomerSessionIdleTTL, err = getDuration("CUSTOMER_SESSION_IDLE_TTL", 7*24*time.Hour); err != nil {
		errs = append(errs, err)
	}
	// Safe defaults differ by environment: MFA on and registration closed
	// everywhere except a developer's machine.
	isLocal := cfg.Env == "local"
	if cfg.AdminMFARequired, err = getBool("ADMIN_MFA_REQUIRED", !isLocal); err != nil {
		errs = append(errs, err)
	}
	if cfg.CustomerRegistrationEnabled, err = getBool("CUSTOMER_REGISTRATION_ENABLED", isLocal); err != nil {
		errs = append(errs, err)
	}
	if cfg.CatalogDemoEnabled, err = getBool("CATALOG_DEMO_ENABLED", isLocal); err != nil {
		errs = append(errs, err)
	}
	cfg.CatalogImageHosts = splitList(os.Getenv("CATALOG_IMAGE_HOSTS"))
	if v := strings.TrimSpace(os.Getenv("DATA_ENCRYPTION_KEY")); v != "" {
		key, decErr := base64.StdEncoding.DecodeString(v)
		if decErr != nil || len(key) != 32 {
			errs = append(errs, errors.New("DATA_ENCRYPTION_KEY must be 32 bytes, base64-encoded (openssl rand -base64 32)"))
		}
		cfg.DataEncryptionKey = key
	}
	for _, s := range splitList(os.Getenv("REFERRAL_MILESTONES")) {
		n, convErr := strconv.Atoi(s)
		if convErr != nil || n < 1 {
			errs = append(errs, fmt.Errorf("REFERRAL_MILESTONES: %q is not a positive integer", s))
			continue
		}
		cfg.ReferralMilestones = append(cfg.ReferralMilestones, n)
	}

	cfg.Commerce = CommerceConfig{
		CutoffWeekday:   get("MENU_CUTOFF_WEEKDAY", "thursday"),
		CutoffTime:      get("MENU_CUTOFF_TIME", "23:59"),
		TaxMode:         get("BILLING_TAX_MODE", "none"),
		TaxLabel:        get("BILLING_TAX_LABEL", "VAT"),
		TaxRegistration: strings.TrimSpace(os.Getenv("BILLING_TAX_REGISTRATION")),
		BusinessName:    get("BILLING_BUSINESS_NAME", "Mise"),
		// Lines separated by "\n" in the environment value.
		BusinessAddress: strings.ReplaceAll(strings.TrimSpace(os.Getenv("BILLING_BUSINESS_ADDRESS")), `\n`, "\n"),
	}
	var n int
	if n, err = getInt("BILLING_TAX_RATE_BPS", 0); err != nil {
		errs = append(errs, err)
	}
	cfg.Commerce.TaxRateBps = int64(n)
	if n, err = getInt("BILLING_DELIVERY_FEE_MINOR", 0); err != nil {
		errs = append(errs, err)
	}
	cfg.Commerce.DeliveryFeeMinor = int64(n)
	if cfg.Commerce.RenewalLead, err = getDuration("BILLING_RENEWAL_LEAD", 48*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.Commerce.RenewalRetries, err = getInt("BILLING_RENEWAL_RETRIES", 0); err != nil {
		errs = append(errs, err)
	}
	if cfg.Commerce.RenewalRetryAfter, err = getDuration("BILLING_RENEWAL_RETRY_AFTER", 24*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.Commerce.MaxPauseWeeks, err = getInt("SUBSCRIPTION_MAX_PAUSE_WEEKS", 4); err != nil {
		errs = append(errs, err)
	}
	if cfg.Commerce.PendingTTL, err = getDuration("SUBSCRIPTION_PENDING_TTL", 24*time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.Commerce.SchedulerInterval, err = getDuration("COMMERCE_SCHEDULER_INTERVAL", time.Minute); err != nil {
		errs = append(errs, err)
	}
	if cfg.Commerce.TestProvider, err = getBool("PAYMENTS_TEST_PROVIDER", isLocal); err != nil {
		errs = append(errs, err)
	}
	if cfg.IsProduction() && cfg.Commerce.TestProvider {
		errs = append(errs, errors.New("PAYMENTS_TEST_PROVIDER cannot be true in production: the test provider never takes real money"))
	}
	errs = append(errs, loadProviders(&cfg.Commerce, cfg.IsProduction())...)
	errs = append(errs, loadAI(&cfg.AI)...)
	if v := strings.TrimSpace(os.Getenv("PAYMENTS_TEST_PROVIDER_SECRET")); v != "" {
		if len(v) < 32 {
			errs = append(errs, errors.New("PAYMENTS_TEST_PROVIDER_SECRET must be at least 32 characters"))
		}
		cfg.Commerce.TestProviderSecret = []byte(v)
	}
	if cfg.Commerce.MaxPauseWeeks < 1 || cfg.Commerce.MaxPauseWeeks > 12 {
		errs = append(errs, errors.New("SUBSCRIPTION_MAX_PAUSE_WEEKS must be between 1 and 12"))
	}
	if cfg.Commerce.SchedulerInterval < 5*time.Second {
		errs = append(errs, errors.New("COMMERCE_SCHEDULER_INTERVAL must be at least 5s"))
	}

	if len(cfg.TokenSigningKey) > 0 && len(cfg.TokenSigningKey) < 32 {
		errs = append(errs, errors.New("TOKEN_SIGNING_KEY must be at least 32 bytes"))
	}
	if cfg.Mail.Driver != "smtp" && cfg.Mail.Driver != "log" {
		errs = append(errs, fmt.Errorf("MAIL_DRIVER must be smtp or log, got %q", cfg.Mail.Driver))
	}
	if cfg.MobilePaymentReturnURL == "" && cfg.Env == "local" {
		cfg.MobilePaymentReturnURL = "mise-local://checkout/return"
	}
	if u := cfg.MobilePaymentReturnURL; u != "" {
		if !mobileReturnRe.MatchString(u) {
			errs = append(errs, errors.New("MOBILE_PAYMENT_RETURN_URL must be <app-scheme>://checkout/return or an https URL"))
		} else if cfg.IsProduction() && !strings.HasPrefix(u, "mise://") && !strings.HasPrefix(u, "https://") {
			errs = append(errs, errors.New("MOBILE_PAYMENT_RETURN_URL in production must use the production app scheme (mise://) or https"))
		}
	}
	if cfg.Env != "local" && cfg.RateLimitsDisabled {
		errs = append(errs, errors.New("RATE_LIMITS_DISABLED is only allowed when MISE_ENV=local"))
	}
	if cfg.Env != "local" && cfg.Mail.Driver == "log" {
		errs = append(errs, errors.New("MAIL_DRIVER=log is only allowed when MISE_ENV=local"))
	}
	if cfg.BFFSharedKey != "" && len(cfg.BFFSharedKey) < 32 {
		errs = append(errs, errors.New("BFF_SHARED_KEY must be at least 32 characters"))
	}
	if cfg.IsProduction() && !cfg.AdminMFARequired {
		errs = append(errs, errors.New("ADMIN_MFA_REQUIRED cannot be false in production"))
	}
	if cfg.AdminSessionIdleTTL <= 0 || cfg.AdminSessionIdleTTL > cfg.AdminSessionTTL {
		errs = append(errs, errors.New("ADMIN_SESSION_IDLE_TTL must be positive and no longer than ADMIN_SESSION_TTL"))
	}
	if cfg.CustomerSessionIdleTTL <= 0 || cfg.CustomerSessionIdleTTL > cfg.CustomerSessionTTL {
		errs = append(errs, errors.New("CUSTOMER_SESSION_IDLE_TTL must be positive and no longer than CUSTOMER_SESSION_TTL"))
	}

	// Without an explicit key, derive one from the signing key with HKDF so
	// existing deployments start without a new secret. Rotating
	// TOKEN_SIGNING_KEY would then make stored TOTP secrets unreadable, so
	// staging and production should set DATA_ENCRYPTION_KEY.
	if cfg.DataEncryptionKey == nil && len(cfg.TokenSigningKey) >= 32 {
		key, hkdfErr := hkdf.Key(sha256.New, cfg.TokenSigningKey, nil, "mise/data-encryption/v1", 32)
		if hkdfErr != nil {
			errs = append(errs, hkdfErr)
		}
		cfg.DataEncryptionKey = key
	}

	if cfg.Commerce.TestProviderSecret == nil && len(cfg.TokenSigningKey) >= 32 {
		key, hkdfErr := hkdf.Key(sha256.New, cfg.TokenSigningKey, nil, "mise/testpay-webhook/v1", 32)
		if hkdfErr != nil {
			errs = append(errs, hkdfErr)
		}
		cfg.Commerce.TestProviderSecret = key
	}

	return cfg, errors.Join(errs...)
}

// databaseURL returns DATABASE_URL, or builds it from DB_HOST, DB_USER,
// DB_PASSWORD and DB_NAME. The parts form exists for AWS, where RDS keeps the
// password in Secrets Manager as its own value rather than inside a URL.
func databaseURL(need func(string) string) string {
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		return v
	}
	if os.Getenv("DB_HOST") == "" {
		return need("DATABASE_URL")
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(need("DB_USER"), need("DB_PASSWORD")),
		Host:     net.JoinHostPort(os.Getenv("DB_HOST"), get("DB_PORT", "5432")),
		Path:     "/" + need("DB_NAME"),
		RawQuery: "sslmode=" + get("DB_SSLMODE", "require"),
	}
	return u.String()
}

func get(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not an integer", key, v)
	}
	return n, nil
}

func getBool(key string, fallback bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %q is not a boolean", key, v)
	}
	return b, nil
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration", key, v)
	}
	return d, nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func currencyList(name string) ([]string, error) {
	var out []string
	for _, c := range strings.Split(os.Getenv(name), ",") {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if len(c) != 3 || strings.Trim(c, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			return nil, fmt.Errorf("%s: %q is not a three-letter currency code", name, c)
		}
		out = append(out, c)
	}
	return out, nil
}

// paypalCurrencies is PayPal's list of supported currency codes, read from
// developer.paypal.com/reference/currency-codes on 25 November 2026. TTD is
// not on it. BRL, CNY and MYR work only for in-country accounts.
var paypalCurrencies = map[string]bool{"AUD": true, "BRL": true, "CAD": true, "CNY": true, "CZK": true, "DKK": true, "EUR": true,
	"HKD": true, "HUF": true, "ILS": true, "JPY": true, "MYR": true, "MXN": true, "TWD": true, "NZD": true, "NOK": true, "PHP": true,
	"PLN": true, "GBP": true, "SGD": true, "SEK": true, "CHF": true, "THB": true, "USD": true}

func loadProviders(c *CommerceConfig, production bool) []error {
	var errs []error
	var err error
	c.Stripe = StripeConfig{
		SecretKey:     strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")),
		WebhookSecret: strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET")),
		APIBase:       strings.TrimSpace(os.Getenv("STRIPE_API_BASE")),
	}
	if c.Stripe.Currencies, err = currencyList("STRIPE_CURRENCIES"); err != nil {
		errs = append(errs, err)
	}
	if k := c.Stripe.SecretKey; k != "" && !strings.HasPrefix(k, "sk_test_") && !strings.HasPrefix(k, "sk_live_") && !strings.HasPrefix(k, "rk_") {
		errs = append(errs, errors.New("STRIPE_SECRET_KEY must be a Stripe secret key (sk_test_…, sk_live_… or a restricted rk_ key)"))
	}
	if c.Stripe.SecretKey != "" && !strings.HasPrefix(c.Stripe.WebhookSecret, "whsec_") {
		errs = append(errs, errors.New("STRIPE_WEBHOOK_SECRET (whsec_…) is required with STRIPE_SECRET_KEY: payments are confirmed only by verified webhooks"))
	}
	c.PayPal = PayPalConfig{
		ClientID:     strings.TrimSpace(os.Getenv("PAYPAL_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("PAYPAL_CLIENT_SECRET")),
		Env:          strings.ToLower(strings.TrimSpace(os.Getenv("PAYPAL_ENV"))),
		WebhookID:    strings.TrimSpace(os.Getenv("PAYPAL_WEBHOOK_ID")),
		APIBase:      strings.TrimSpace(os.Getenv("PAYPAL_API_BASE")),
	}
	if c.PayPal.Env == "" {
		c.PayPal.Env = "sandbox"
	}
	if c.PayPal.Env != "sandbox" && c.PayPal.Env != "live" {
		errs = append(errs, errors.New("PAYPAL_ENV must be sandbox or live"))
	}
	if c.PayPal.Currencies, err = currencyList("PAYPAL_CURRENCIES"); err != nil {
		errs = append(errs, err)
	}
	for _, cur := range c.PayPal.Currencies {
		if !paypalCurrencies[cur] {
			errs = append(errs, fmt.Errorf("PAYPAL_CURRENCIES: PayPal does not support %s (see docs/integrations/PAYPAL.md)", cur))
		}
	}
	if (c.PayPal.ClientID == "") != (c.PayPal.ClientSecret == "") {
		errs = append(errs, errors.New("set both PAYPAL_CLIENT_ID and PAYPAL_CLIENT_SECRET, or neither"))
	}
	if c.PayPal.ClientID != "" && c.PayPal.WebhookID == "" {
		errs = append(errs, errors.New("PAYPAL_WEBHOOK_ID is required with PayPal credentials: payments are confirmed only by verified webhooks"))
	}
	if !production && c.PayPal.Env == "live" {
		errs = append(errs, errors.New("PAYPAL_ENV=live is only allowed when MISE_ENV=production"))
	}
	if !production && strings.HasPrefix(c.Stripe.SecretKey, "sk_live_") {
		errs = append(errs, errors.New("a live Stripe key (sk_live_) is only allowed when MISE_ENV=production"))
	}
	if (c.Stripe.APIBase != "" || c.PayPal.APIBase != "") && production {
		errs = append(errs, errors.New("STRIPE_API_BASE and PAYPAL_API_BASE are for tests only"))
	}
	c.Scotiabank = ScotiabankConfig{MerchantID: strings.TrimSpace(os.Getenv("SCOTIABANK_MERCHANT_ID"))}
	return errs
}

func loadAI(c *AIConfig) []error {
	var errs []error
	c.Provider = strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER")))
	c.Model = strings.TrimSpace(os.Getenv("AI_MODEL"))
	c.APIKey = strings.TrimSpace(os.Getenv("AI_API_KEY"))
	c.APIBase = strings.TrimSpace(os.Getenv("AI_API_BASE"))
	var err error
	if c.Timeout, err = getDuration("AI_TIMEOUT", 45*time.Second); err != nil {
		errs = append(errs, err)
	} else if c.Timeout < time.Second || c.Timeout > 5*time.Minute {
		errs = append(errs, errors.New("AI_TIMEOUT must be between 1s and 5m"))
	}
	if c.Retries, err = getInt("AI_RETRIES", 2); err != nil {
		errs = append(errs, err)
	} else if c.Retries < 0 || c.Retries > 5 {
		errs = append(errs, errors.New("AI_RETRIES must be between 0 and 5"))
	}
	switch c.Provider {
	case "":
	case "openai":
		if c.Model == "" || c.APIKey == "" {
			errs = append(errs, errors.New("AI_PROVIDER=openai needs AI_MODEL and AI_API_KEY"))
		}
	default:
		errs = append(errs, fmt.Errorf("AI_PROVIDER %q is not supported (use openai, or leave it empty)", c.Provider))
	}
	if c.APIBase != "" {
		if u, err := url.Parse(c.APIBase); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, errors.New("AI_API_BASE must be an http(s) URL"))
		}
	}
	return errs
}
