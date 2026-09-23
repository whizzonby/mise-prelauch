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
		"MAIL_DRIVER", "RATE_LIMITS_DISABLED", "REFERRAL_MILESTONES", "LOG_LEVEL"} {
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
