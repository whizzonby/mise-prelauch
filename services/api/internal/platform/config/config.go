// Package config loads all runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
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
		DatabaseURL:   need("DATABASE_URL"),
		PublicSiteURL: strings.TrimRight(get("PUBLIC_SITE_URL", "http://localhost:3100"), "/"),
		CORSOrigins:   splitList(get("CORS_ORIGINS", "http://localhost:3100")),

		TokenSigningKey: []byte(need("TOKEN_SIGNING_KEY")),

		ConsentVersion: get("CONSENT_VERSION", "2026-09"),

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
	if cfg.AdminSessionTTL, err = getDuration("ADMIN_SESSION_TTL", 12*time.Hour); err != nil {
		errs = append(errs, err)
	}
	for _, s := range splitList(os.Getenv("REFERRAL_MILESTONES")) {
		n, convErr := strconv.Atoi(s)
		if convErr != nil || n < 1 {
			errs = append(errs, fmt.Errorf("REFERRAL_MILESTONES: %q is not a positive integer", s))
			continue
		}
		cfg.ReferralMilestones = append(cfg.ReferralMilestones, n)
	}

	if len(cfg.TokenSigningKey) > 0 && len(cfg.TokenSigningKey) < 32 {
		errs = append(errs, errors.New("TOKEN_SIGNING_KEY must be at least 32 bytes"))
	}
	if cfg.Mail.Driver != "smtp" && cfg.Mail.Driver != "log" {
		errs = append(errs, fmt.Errorf("MAIL_DRIVER must be smtp or log, got %q", cfg.Mail.Driver))
	}
	if cfg.Env != "local" && cfg.Mail.Driver == "log" {
		errs = append(errs, errors.New("MAIL_DRIVER=log is only allowed when MISE_ENV=local"))
	}

	return cfg, errors.Join(errs...)
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
