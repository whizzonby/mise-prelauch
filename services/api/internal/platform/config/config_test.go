package config

import (
	"strings"
	"testing"
)

// setEnv sets the minimum valid configuration, then the overrides.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	base := map[string]string{
		"MISE_ENV":          "local",
		"DATABASE_URL":      "postgres://mise:mise@localhost:5460/mise",
		"TOKEN_SIGNING_KEY": "0123456789abcdef0123456789abcdef",
	}
	for _, key := range []string{"DB_HOST", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_PORT", "DB_SSLMODE",
		"MAIL_DRIVER", "RATE_LIMITS_DISABLED", "REFERRAL_MILESTONES", "LOG_LEVEL",
		"ADMIN_MFA_REQUIRED", "CUSTOMER_REGISTRATION_ENABLED", "DATA_ENCRYPTION_KEY", "BFF_SHARED_KEY",
		"ADMIN_SESSION_TTL", "ADMIN_SESSION_IDLE_TTL", "CUSTOMER_SESSION_TTL", "CUSTOMER_SESSION_IDLE_TTL"} {
		t.Setenv(key, "")
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
	for k, v := range overrides {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, nil)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.EmailVerificationRequired {
		t.Error("email verification should be required by default")
	}
	if cfg.RateLimitsDisabled {
		t.Error("rate limits should be on by default")
	}
	if len(cfg.ReferralMilestones) != 0 {
		t.Errorf("milestone emails should be off by default, got %v", cfg.ReferralMilestones)
	}
}

func TestLoadRejectsUnsafeConfiguration(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"missing signing key":            {map[string]string{"TOKEN_SIGNING_KEY": ""}, "TOKEN_SIGNING_KEY is required"},
		"short signing key":              {map[string]string{"TOKEN_SIGNING_KEY": "too-short"}, "at least 32 bytes"},
		"missing database":               {map[string]string{"DATABASE_URL": ""}, "DATABASE_URL is required"},
		"unknown environment":            {map[string]string{"MISE_ENV": "prod"}, "MISE_ENV must be"},
		"log mailer in production":       {map[string]string{"MISE_ENV": "production", "MAIL_DRIVER": "log"}, "MAIL_DRIVER=log is only allowed"},
		"rate limits off in staging":     {map[string]string{"MISE_ENV": "staging", "MAIL_DRIVER": "smtp", "RATE_LIMITS_DISABLED": "true"}, "RATE_LIMITS_DISABLED is only allowed"},
		"milestones that are not counts": {map[string]string{"REFERRAL_MILESTONES": "3,many"}, "REFERRAL_MILESTONES"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, c.env)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Load error = %v; want one containing %q", err, c.want)
			}
		})
	}
}

func TestDatabaseURLFromParts(t *testing.T) {
	setEnv(t, map[string]string{
		"DATABASE_URL": "",
		"DB_HOST":      "db.internal",
		"DB_USER":      "mise",
		"DB_PASSWORD":  "p@ss/word:with?odd#chars",
		"DB_NAME":      "mise",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The password is escaped, and TLS is required unless stated otherwise.
	want := "postgres://mise:p%40ss%2Fword%3Awith%3Fodd%23chars@db.internal:5432/mise?sslmode=require"
	if cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL =\n  %s\nwant\n  %s", cfg.DatabaseURL, want)
	}
}

func TestIdentityDefaultsDependOnEnvironment(t *testing.T) {
	setEnv(t, map[string]string{"MISE_ENV": "local"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminMFARequired || !cfg.CustomerRegistrationEnabled {
		t.Errorf("local: mfa %v registration %v, want false, true", cfg.AdminMFARequired, cfg.CustomerRegistrationEnabled)
	}
	if len(cfg.DataEncryptionKey) != 32 {
		t.Errorf("derived data key is %d bytes", len(cfg.DataEncryptionKey))
	}

	setEnv(t, map[string]string{"MISE_ENV": "staging", "MAIL_DRIVER": "smtp"})
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AdminMFARequired || cfg.CustomerRegistrationEnabled {
		t.Errorf("staging: mfa %v registration %v, want true, false", cfg.AdminMFARequired, cfg.CustomerRegistrationEnabled)
	}
}

func TestIdentityConfigurationIsChecked(t *testing.T) {
	cases := map[string]map[string]string{
		"mfa off in production":  {"MISE_ENV": "production", "MAIL_DRIVER": "smtp", "ADMIN_MFA_REQUIRED": "false"},
		"short data key":         {"DATA_ENCRYPTION_KEY": "c2hvcnQ="},
		"short bff key":          {"BFF_SHARED_KEY": "too-short"},
		"idle longer than ttl":   {"ADMIN_SESSION_TTL": "1h", "ADMIN_SESSION_IDLE_TTL": "2h"},
		"customer idle too long": {"CUSTOMER_SESSION_TTL": "24h", "CUSTOMER_SESSION_IDLE_TTL": "48h"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, env)
			if _, err := Load(); err == nil {
				t.Error("unsafe configuration accepted")
			}
		})
	}

	setEnv(t, map[string]string{"DATA_ENCRYPTION_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="})
	cfg, err := Load()
	if err != nil || string(cfg.DataEncryptionKey) != "0123456789abcdef0123456789abcdef" {
		t.Errorf("explicit data key: %q, %v", cfg.DataEncryptionKey, err)
	}
}

func TestCommercePaymentSafety(t *testing.T) {
	// Locally the test provider is on by default; nothing else is.
	setEnv(t, map[string]string{"PAYMENTS_TEST_PROVIDER": ""})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Commerce.TestProvider || cfg.Commerce.TaxMode != "none" || cfg.Commerce.DeliveryFeeMinor != 0 {
		t.Errorf("local commerce defaults = %+v", cfg.Commerce)
	}
	if len(cfg.Commerce.TestProviderSecret) != 32 {
		t.Error("the test provider has no derived webhook secret")
	}
	// Staging: off unless asked for.
	setEnv(t, map[string]string{"MISE_ENV": "staging", "MAIL_DRIVER": "smtp", "PAYMENTS_TEST_PROVIDER": ""})
	if cfg, err = Load(); err != nil || cfg.Commerce.TestProvider {
		t.Errorf("staging test provider = %v (%v)", cfg.Commerce.TestProvider, err)
	}
	// Production: the test provider is refused outright.
	setEnv(t, map[string]string{"MISE_ENV": "production", "MAIL_DRIVER": "smtp", "PAYMENTS_TEST_PROVIDER": "true"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PAYMENTS_TEST_PROVIDER cannot be true in production") {
		t.Fatalf("production with the test provider: %v", err)
	}
}

// Provider credentials (Sprint 9.1) are checked at start-up: malformed keys,
// missing webhook secrets, unsupported PayPal currencies, live credentials
// outside production and test-only API bases in production are all refused.
func TestPaymentProviderConfigurationIsChecked(t *testing.T) {
	clear := map[string]string{"STRIPE_SECRET_KEY": "", "STRIPE_WEBHOOK_SECRET": "", "STRIPE_CURRENCIES": "", "STRIPE_API_BASE": "",
		"PAYPAL_CLIENT_ID": "", "PAYPAL_CLIENT_SECRET": "", "PAYPAL_ENV": "", "PAYPAL_WEBHOOK_ID": "", "PAYPAL_CURRENCIES": "", "PAYPAL_API_BASE": ""}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range clear {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	// Nothing configured: fine, and no provider credentials.
	setEnv(t, with(nil))
	cfg, err := Load()
	if err != nil || cfg.Commerce.Stripe.SecretKey != "" || cfg.Commerce.PayPal.ClientID != "" {
		t.Fatalf("empty provider config: %+v %v", cfg.Commerce.Stripe, err)
	}
	// A valid test setup loads.
	setEnv(t, with(map[string]string{"STRIPE_SECRET_KEY": "sk_test_abc123", "STRIPE_WEBHOOK_SECRET": "whsec_x", "STRIPE_CURRENCIES": "usd, TTD",
		"PAYPAL_CLIENT_ID": "id", "PAYPAL_CLIENT_SECRET": "secret", "PAYPAL_ENV": "sandbox", "PAYPAL_WEBHOOK_ID": "WH", "PAYPAL_CURRENCIES": "USD"}))
	if cfg, err = Load(); err != nil {
		t.Fatalf("valid test providers: %v", err)
	}
	if strings.Join(cfg.Commerce.Stripe.Currencies, ",") != "USD,TTD" {
		t.Errorf("Stripe currencies = %v", cfg.Commerce.Stripe.Currencies)
	}
	refused := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"not a Stripe key", map[string]string{"STRIPE_SECRET_KEY": "pk_test_abc", "STRIPE_WEBHOOK_SECRET": "whsec_x"}, "STRIPE_SECRET_KEY"},
		{"Stripe without webhook secret", map[string]string{"STRIPE_SECRET_KEY": "sk_test_abc"}, "STRIPE_WEBHOOK_SECRET"},
		{"bad currency code", map[string]string{"STRIPE_CURRENCIES": "US"}, "STRIPE_CURRENCIES"},
		{"live Stripe key locally", map[string]string{"STRIPE_SECRET_KEY": "sk_live_abc", "STRIPE_WEBHOOK_SECRET": "whsec_x"}, "only allowed when MISE_ENV=production"},
		{"PayPal cannot charge TTD", map[string]string{"PAYPAL_CLIENT_ID": "id", "PAYPAL_CLIENT_SECRET": "s", "PAYPAL_ENV": "sandbox", "PAYPAL_WEBHOOK_ID": "W", "PAYPAL_CURRENCIES": "TTD"}, "PayPal does not support TTD"},
		{"PayPal id without secret", map[string]string{"PAYPAL_CLIENT_ID": "id", "PAYPAL_ENV": "sandbox"}, "PAYPAL_CLIENT_SECRET"},
		{"PayPal without webhook id", map[string]string{"PAYPAL_CLIENT_ID": "id", "PAYPAL_CLIENT_SECRET": "s", "PAYPAL_ENV": "sandbox"}, "PAYPAL_WEBHOOK_ID"},
		{"PayPal unknown env", map[string]string{"PAYPAL_ENV": "staging"}, "PAYPAL_ENV must be sandbox or live"},
		{"PayPal live locally", map[string]string{"PAYPAL_CLIENT_ID": "id", "PAYPAL_CLIENT_SECRET": "s", "PAYPAL_ENV": "live", "PAYPAL_WEBHOOK_ID": "W"}, "only allowed when MISE_ENV=production"},
	}
	for _, c := range refused {
		setEnv(t, with(c.env))
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", c.name, err, c.want)
		}
	}
	// Test-only API bases are refused in production.
	setEnv(t, with(map[string]string{"MISE_ENV": "production", "MAIL_DRIVER": "smtp", "PAYMENTS_TEST_PROVIDER": "", "STRIPE_API_BASE": "http://fake"}))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "for tests only") {
		t.Errorf("API base in production: %v", err)
	}
}
