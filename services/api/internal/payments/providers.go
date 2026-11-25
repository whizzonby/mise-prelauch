package payments

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Several providers sit behind this one service (Sprint 9.1). A provider can
// take a payment only when all of these hold:
//   - its adapter is registered and reports itself ready (credentials
//     configured, a usable specification);
//   - staff have enabled it (payment_provider_settings.enabled);
//   - it can charge the bill's exact currency for this merchant account;
//   - its mode matches its credentials: test credentials only in test mode,
//     live credentials only in live mode, and live mode needs a recorded
//     approval (and, in production, the provider must actually be live).
// Anything else is shown to staff with the reason, and never to customers.

// Setting is one row of payment_provider_settings.
type Setting struct {
	Provider              string     `json:"provider"`
	Enabled               bool       `json:"enabled"`
	Mode                  string     `json:"mode"`
	MerchantStatus        string     `json:"merchant_status"`
	MerchantReference     string     `json:"merchant_reference"`
	LiveApprovedBy        *string    `json:"live_approved_by"`
	LiveApprovedAt        *time.Time `json:"live_approved_at"`
	LiveApprovalReference string     `json:"live_approval_reference"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func (s *Service) settings(ctx context.Context, q db.Querier) (map[string]Setting, error) {
	rows, err := q.Query(ctx, `SELECT provider, enabled, mode, merchant_status, merchant_reference, live_approved_by::text,
		live_approved_at, live_approval_reference, updated_at FROM payment_provider_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Setting{}
	for rows.Next() {
		var st Setting
		if err := rows.Scan(&st.Provider, &st.Enabled, &st.Mode, &st.MerchantStatus, &st.MerchantReference, &st.LiveApprovedBy,
			&st.LiveApprovedAt, &st.LiveApprovalReference, &st.UpdatedAt); err != nil {
			return nil, err
		}
		out[st.Provider] = st
	}
	return out, rows.Err()
}

func readiness(p Provider) Readiness {
	if d, ok := p.(Describer); ok {
		return d.Readiness()
	}
	return Readiness{Ready: true}
}

func displayName(p Provider) string {
	if d, ok := p.(Describer); ok {
		return d.DisplayName()
	}
	return p.Name()
}

// eligible says whether provider p may take a payment in currency now, and
// if not, why (for staff; customers only ever see eligible providers).
func (s *Service) eligible(p Provider, st Setting, hasSetting bool, currency string) (bool, string) {
	r := readiness(p)
	switch {
	case !r.Ready:
		return false, r.Reason
	case !hasSetting || !st.Enabled:
		return false, "Not enabled by staff."
	case currency != "" && !p.Capabilities().Supports(currency):
		return false, "Cannot charge " + currency + " for this merchant account."
	case st.Mode == "live" && !p.Live():
		return false, "Live mode is set but the credentials are test credentials."
	case st.Mode == "test" && p.Live():
		return false, "Live credentials are configured but the provider is in test mode."
	case st.Mode == "live" && (st.LiveApprovedBy == nil || st.LiveApprovedAt == nil):
		return false, "Live mode has no recorded approval."
	case s.Production && !p.Live():
		return false, "Production takes live payments only."
	}
	return true, ""
}

// Option is a way the customer can pay a bill.
type Option struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Method   string `json:"method"`
	Test     bool   `json:"test"`
	// Recurring: the method can be saved and charged for later weeks.
	Recurring bool `json:"recurring"`
}

// Options lists the providers that can take a payment in currency right
// now, in a stable order (the order providers were registered in).
func (s *Service) Options(ctx context.Context, currency string) ([]Option, error) {
	sets, err := s.settings(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	var out []Option
	for _, name := range s.order {
		p := s.providers[name]
		st, has := sets[name]
		if ok, _ := s.eligible(p, st, has, currency); !ok {
			continue
		}
		caps := p.Capabilities()
		out = append(out, Option{Provider: name, Label: displayName(p), Method: caps.Method, Test: !p.Live(), Recurring: caps.OffSessionCharges})
	}
	return out, nil
}

// choose returns the provider to use for a new checkout: the one asked for
// if it is eligible, or the first eligible one when none is named.
func (s *Service) choose(ctx context.Context, name, currency string) (Provider, error) {
	opts, err := s.Options(ctx, currency)
	if err != nil {
		return nil, err
	}
	if len(opts) == 0 {
		return nil, ErrNoProvider
	}
	if name == "" {
		return s.providers[opts[0].Provider], nil
	}
	for _, o := range opts {
		if o.Provider == name {
			return s.providers[name], nil
		}
	}
	return nil, httpx.NewError(http.StatusUnprocessableEntity, "payment_method_unavailable",
		"That payment method is not available for this payment. Choose another.")
}

// ---------------------------------------------------------------- admin

// ProviderStatus is a provider as staff see it.
type ProviderStatus struct {
	Provider          string       `json:"provider"`
	Name              string       `json:"name"`
	Registered        bool         `json:"registered"`
	Ready             bool         `json:"ready"`
	ReadinessReason   string       `json:"readiness_reason"`
	CredentialRef     string       `json:"credential_ref"`
	Live              bool         `json:"live"`
	Currencies        []string     `json:"currencies"`
	Method            string       `json:"method"`
	RecurringBilling  bool         `json:"recurring_billing"`
	Refunds           bool         `json:"refunds"`
	Setting           Setting      `json:"setting"`
	TakingPayments    bool         `json:"taking_payments"`
	NotTakingReason   string       `json:"not_taking_reason"`
	LastReconciledAt  *time.Time   `json:"last_reconciled_at"`
	LastSucceededAt   *time.Time   `json:"last_succeeded_at"`
	Failures24h       int          `json:"failures_24h"`
	WebhooksFailed    int          `json:"webhooks_failed"`
	WebhooksReceived  int          `json:"webhooks_received_24h"`
	LastWebhookAt     *time.Time   `json:"last_webhook_at"`
	RecentEvents      []ProviderEv `json:"recent_events"`
}

// ProviderEv is one audited change to a provider's settings.
type ProviderEv struct {
	Action    string    `json:"action"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Note      string    `json:"note"`
	ActorID   *string   `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Statuses lists every known provider (registered adapters and settings
// rows), with health figures for the admin page.
func (s *Service) Statuses(ctx context.Context) ([]ProviderStatus, error) {
	sets, err := s.settings(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	names := slices.Clone(s.order)
	for name := range sets {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	out := make([]ProviderStatus, 0, len(names))
	for _, name := range names {
		st, has := sets[name]
		ps := ProviderStatus{Provider: name, Name: name, Setting: st, Currencies: []string{}, RecentEvents: []ProviderEv{}}
		if p, ok := s.providers[name]; ok {
			r := readiness(p)
			caps := p.Capabilities()
			ps.Registered, ps.Name, ps.Ready, ps.ReadinessReason, ps.CredentialRef = true, displayName(p), r.Ready, r.Reason, r.CredentialRef
			ps.Live, ps.Method, ps.RecurringBilling, ps.Refunds = p.Live(), caps.Method, caps.OffSessionCharges, caps.Refunds
			if caps.Currencies != nil {
				ps.Currencies = caps.Currencies
			}
			ps.TakingPayments, ps.NotTakingReason = s.eligible(p, st, has, "")
		} else {
			ps.ReadinessReason, ps.NotTakingReason = "No adapter is configured for this provider.", "No adapter is configured for this provider."
		}
		err := s.pool.QueryRow(ctx, `SELECT
				(SELECT max(h.created_at) FROM payment_status_history h JOIN payments p ON p.id = h.payment_id
				  WHERE p.provider = $1 AND h.source IN ('reconcile', 'webhook', 'expiry')),
				(SELECT max(succeeded_at) FROM payments WHERE provider = $1),
				(SELECT count(*) FROM payments WHERE provider = $1 AND status = 'failed' AND updated_at > now() - interval '24 hours'),
				(SELECT count(*) FROM payment_webhook_events WHERE provider = $1 AND status = 'failed'),
				(SELECT count(*) FROM payment_webhook_events WHERE provider = $1 AND received_at > now() - interval '24 hours'),
				(SELECT max(received_at) FROM payment_webhook_events WHERE provider = $1)`, name).
			Scan(&ps.LastReconciledAt, &ps.LastSucceededAt, &ps.Failures24h, &ps.WebhooksFailed, &ps.WebhooksReceived, &ps.LastWebhookAt)
		if err != nil {
			return nil, err
		}
		rows, err := s.pool.Query(ctx, `SELECT action, from_value, to_value, note, actor_id::text, created_at
			FROM payment_provider_events WHERE provider = $1 ORDER BY id DESC LIMIT 10`, name)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e ProviderEv
			if err := rows.Scan(&e.Action, &e.From, &e.To, &e.Note, &e.ActorID, &e.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			ps.RecentEvents = append(ps.RecentEvents, e)
		}
		rows.Close()
		out = append(out, ps)
	}
	return out, nil
}

// SettingsInput changes what staff control in test mode.
type SettingsInput struct {
	Enabled           *bool   `json:"enabled"`
	MerchantStatus    *string `json:"merchant_status"`
	MerchantReference *string `json:"merchant_reference"`
	Note              string  `json:"note"`
}

var errUnknownProvider = httpx.NewError(http.StatusNotFound, "not_found", "No such payment provider.")

func (s *Service) recordProviderEvent(ctx context.Context, tx pgx.Tx, actor, provider, action, from, to, note string) error {
	var actorID *string
	if actor != "" {
		actorID = &actor
	}
	if _, err := tx.Exec(ctx, `INSERT INTO payment_provider_events (provider, action, from_value, to_value, note, actor_id, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))`, provider, action, from, to, note, actorID, httpx.RequestID(ctx)); err != nil {
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "payment_provider." + action, TargetType: "payment_provider",
		TargetID: provider, Metadata: map[string]any{"from": from, "to": to, "note": note}})
}

// UpdateSettings enables or disables a provider and records merchant status.
// It never switches a provider to live mode (that is Activate).
func (s *Service) UpdateSettings(ctx context.Context, actor, provider string, in SettingsInput) (*ProviderStatus, error) {
	if in.MerchantStatus != nil && !slices.Contains([]string{"not_started", "applied", "verified", "rejected"}, *in.MerchantStatus) {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "merchant_status", Code: "invalid", Message: "Choose a merchant status."}})
	}
	if in.MerchantReference != nil && len(*in.MerchantReference) > 200 {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "merchant_reference", Code: "too_long", Message: "At most 200 characters."}})
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var st Setting
		err := tx.QueryRow(ctx, `SELECT enabled, merchant_status, merchant_reference FROM payment_provider_settings WHERE provider = $1 FOR UPDATE`,
			provider).Scan(&st.Enabled, &st.MerchantStatus, &st.MerchantReference)
		if errors.Is(err, pgx.ErrNoRows) {
			return errUnknownProvider
		}
		if err != nil {
			return err
		}
		if in.Enabled != nil && *in.Enabled != st.Enabled {
			if *in.Enabled {
				p, ok := s.providers[provider]
				if !ok {
					return httpx.NewError(http.StatusConflict, "provider_not_configured", "This provider has no adapter configured, so it cannot be enabled.")
				}
				if r := readiness(p); !r.Ready {
					return httpx.NewError(http.StatusConflict, "provider_not_ready", "This provider cannot be enabled: "+r.Reason)
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE payment_provider_settings SET enabled = $2, updated_by = $3, updated_at = now() WHERE provider = $1`,
				provider, *in.Enabled, actor); err != nil {
				return err
			}
			if err := s.recordProviderEvent(ctx, tx, actor, provider, map[bool]string{true: "enabled", false: "disabled"}[*in.Enabled],
				boolWord(st.Enabled), boolWord(*in.Enabled), in.Note); err != nil {
				return err
			}
		}
		if in.MerchantStatus != nil && *in.MerchantStatus != st.MerchantStatus {
			if _, err := tx.Exec(ctx, `UPDATE payment_provider_settings SET merchant_status = $2, updated_by = $3, updated_at = now() WHERE provider = $1`,
				provider, *in.MerchantStatus, actor); err != nil {
				return err
			}
			if err := s.recordProviderEvent(ctx, tx, actor, provider, "merchant_status", st.MerchantStatus, *in.MerchantStatus, in.Note); err != nil {
				return err
			}
		}
		if in.MerchantReference != nil && strings.TrimSpace(*in.MerchantReference) != st.MerchantReference {
			ref := strings.TrimSpace(*in.MerchantReference)
			if _, err := tx.Exec(ctx, `UPDATE payment_provider_settings SET merchant_reference = $2, updated_by = $3, updated_at = now() WHERE provider = $1`,
				provider, ref, actor); err != nil {
				return err
			}
			if err := s.recordProviderEvent(ctx, tx, actor, provider, "merchant_reference", st.MerchantReference, ref, in.Note); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.statusOf(ctx, provider)
}

// ActivateInput approves a provider for live payments, or returns it to test.
type ActivateInput struct {
	Mode      string `json:"mode"`
	Reference string `json:"reference"`
	Note      string `json:"note"`
}

// SetMode switches a provider between test and live. Live needs the adapter
// to hold live credentials, a verified merchant account and a written
// approval reference; it is recorded with who approved it.
func (s *Service) SetMode(ctx context.Context, actor, provider string, in ActivateInput) (*ProviderStatus, error) {
	if in.Mode != "test" && in.Mode != "live" {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "mode", Code: "invalid", Message: "Choose test or live."}})
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var mode, merchant string
		err := tx.QueryRow(ctx, `SELECT mode, merchant_status FROM payment_provider_settings WHERE provider = $1 FOR UPDATE`, provider).Scan(&mode, &merchant)
		if errors.Is(err, pgx.ErrNoRows) {
			return errUnknownProvider
		}
		if err != nil {
			return err
		}
		if mode == in.Mode {
			return nil
		}
		if in.Mode == "test" {
			if _, err := tx.Exec(ctx, `UPDATE payment_provider_settings SET mode = 'test', live_approved_by = NULL, live_approved_at = NULL,
				live_approval_reference = '', updated_by = $2, updated_at = now() WHERE provider = $1`, provider, actor); err != nil {
				return err
			}
			return s.recordProviderEvent(ctx, tx, actor, provider, "mode", mode, "test", in.Note)
		}
		p, ok := s.providers[provider]
		switch {
		case !ok:
			return httpx.NewError(http.StatusConflict, "provider_not_configured", "This provider has no adapter configured.")
		case !readiness(p).Ready:
			return httpx.NewError(http.StatusConflict, "provider_not_ready", "This provider is not ready: "+readiness(p).Reason)
		case !p.Live():
			return httpx.NewError(http.StatusConflict, "test_credentials", "The configured credentials are test credentials. Live mode needs live credentials in the secret store first.")
		case merchant != "verified":
			return httpx.NewError(http.StatusConflict, "merchant_not_verified", "Record the merchant account as verified before going live.")
		case len(strings.TrimSpace(in.Reference)) < 3:
			return httpx.ValidationError([]httpx.FieldError{{Field: "reference", Code: "required", Message: "Give the written approval reference."}})
		}
		if _, err := tx.Exec(ctx, `UPDATE payment_provider_settings SET mode = 'live', live_approved_by = $2, live_approved_at = now(),
			live_approval_reference = $3, updated_by = $2, updated_at = now() WHERE provider = $1`, provider, actor, strings.TrimSpace(in.Reference)); err != nil {
			return err
		}
		return s.recordProviderEvent(ctx, tx, actor, provider, "mode", mode, "live", strings.TrimSpace(in.Reference+" "+in.Note))
	})
	if err != nil {
		return nil, err
	}
	return s.statusOf(ctx, provider)
}

func (s *Service) statusOf(ctx context.Context, provider string) (*ProviderStatus, error) {
	all, err := s.Statuses(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Provider == provider {
			return &all[i], nil
		}
	}
	return nil, errUnknownProvider
}

func boolWord(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// ---------------------------------------------------------------- one attempt at a time

var (
	errAttemptInProgress = httpx.NewError(http.StatusConflict, "payment_in_progress",
		"A payment for this week is still being processed with another payment method. Finish it, or try again in a few minutes.")
	errOutcomeUnknown = httpx.NewError(http.StatusConflict, "payment_status_unknown",
		"We could not confirm an earlier payment attempt for this week with its provider. Please try again in a few minutes.")
)

// settleOtherAttempts makes sure no other provider could still take money
// for the bill before a new checkout opens with chosen. An open checkout at
// another provider is checked with that provider (the customer may have just
// paid) and, if unpaid, closed there; a payment still pending or processing
// elsewhere blocks. Nothing is retried through a different provider while an
// outcome is unknown.
func (s *Service) settleOtherAttempts(ctx context.Context, billingCycleID, chosen string) error {
	var openID, openProvider string
	var openRef *string
	err := s.pool.QueryRow(ctx, `SELECT id, provider, provider_ref FROM checkout_sessions
		WHERE billing_cycle_id = $1 AND status = 'open' AND provider <> $2`, billingCycleID, chosen).Scan(&openID, &openProvider, &openRef)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return err
	case openRef == nil || *openRef == "":
		// The provider never confirmed creating it: nothing could have been paid there.
		if _, err := s.pool.Exec(ctx, `UPDATE checkout_sessions SET status = 'cancelled', updated_at = now() WHERE id = $1 AND status = 'open'`, openID); err != nil {
			return err
		}
	default:
		if err := s.ReconcileCheckout(ctx, openProvider, *openRef, "reconcile", ""); err != nil && !errors.Is(err, errUnknownRef) {
			return errOutcomeUnknown
		}
		var still bool
		if err := s.pool.QueryRow(ctx, `SELECT status = 'open' FROM checkout_sessions WHERE id = $1`, openID).Scan(&still); err != nil {
			return err
		}
		if still {
			c, ok := s.providers[openProvider].(CheckoutCanceller)
			if !ok {
				return errAttemptInProgress // it can only lapse at its expiry time
			}
			if err := c.CancelCheckout(ctx, *openRef); err != nil {
				return errAttemptInProgress
			}
			if _, err := s.pool.Exec(ctx, `UPDATE checkout_sessions SET status = 'cancelled', updated_at = now() WHERE id = $1 AND status = 'open'`, openID); err != nil {
				return err
			}
		}
	}
	// Payments in an uncertain state at another provider: ask the provider.
	rows, err := s.pool.Query(ctx, `SELECT p.provider, p.kind, p.provider_ref, cs.provider_ref FROM payments p
		LEFT JOIN checkout_sessions cs ON cs.id = p.checkout_session_id
		WHERE p.billing_cycle_id = $1 AND p.provider <> $2 AND p.status IN ('pending', 'requires_action', 'processing')`, billingCycleID, chosen)
	if err != nil {
		return err
	}
	type attempt struct {
		provider, kind string
		txRef, sessRef *string
	}
	var pending []attempt
	for rows.Next() {
		var a attempt
		if err := rows.Scan(&a.provider, &a.kind, &a.txRef, &a.sessRef); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, a)
	}
	rows.Close()
	for _, a := range pending {
		var rerr error
		switch {
		case a.kind == "checkout" && a.sessRef != nil:
			rerr = s.ReconcileCheckout(ctx, a.provider, *a.sessRef, "reconcile", "")
		case a.txRef != nil:
			rerr = s.ReconcileTransaction(ctx, a.provider, *a.txRef, "reconcile", "")
		default:
			return errAttemptInProgress
		}
		if rerr != nil {
			return errOutcomeUnknown
		}
	}
	return nil
}
