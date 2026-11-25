// Package launch answers two questions for the people deciding go-live:
// is the system healthy right now (SystemStatus), and is the business ready
// to launch (Gates). Every gate shows the evidence the system can see for
// itself and the sign-off a person recorded. A configuration record alone is
// never "verified", and the overall answer is never "ready" while a critical
// gate is not verified or explicitly not applicable.
package launch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/buildinfo"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// Facts are the deployment facts configuration knows and the database does not.
type Facts struct {
	Env             string
	PaymentProvider string // "" when none is configured
	PaymentsLive    bool
	// Payments, when set, is asked at check time (staff enable providers and
	// approve live mode while the system runs): the providers taking
	// payments now, and which of them are live.
	Payments         func(ctx context.Context) (taking []string, live []string, err error)
	MailDriver       string
	MailHost         string
	CustomerAppURL   string
	PublicSiteURL    string
	DemoContentShown bool
}

type Service struct {
	pool  *pgxpool.Pool
	Facts Facts
	Now   func() time.Time
}

func NewService(pool *pgxpool.Pool, f Facts) *Service {
	return &Service{pool: pool, Facts: f, Now: time.Now}
}

// ---------------------------------------------------------------- heartbeat

// HeartbeatInterval is how often a worker records that it is alive.
const HeartbeatInterval = 30 * time.Second

// WorkerStaleAfter: a worker silent for longer is reported as stopped.
const WorkerStaleAfter = 3 * HeartbeatInterval

// Beat records that a worker is alive.
func Beat(ctx context.Context, q db.Querier, worker string, started time.Time) error {
	_, err := q.Exec(ctx, `INSERT INTO worker_heartbeats (worker, version, started_at, beat_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (worker) DO UPDATE SET version = EXCLUDED.version, started_at = EXCLUDED.started_at, beat_at = now()`,
		worker, buildinfo.Version, started)
	return err
}

// RunHeartbeat beats until ctx ends.
func RunHeartbeat(ctx context.Context, pool *pgxpool.Pool, worker string) {
	started := time.Now()
	t := time.NewTicker(HeartbeatInterval)
	defer t.Stop()
	for {
		if err := Beat(ctx, pool, worker, started); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "worker heartbeat failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---------------------------------------------------------------- system status

type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func BuildVersion() Version { return Version{Version: buildinfo.Version, Commit: buildinfo.Commit} }

type Worker struct {
	Worker    string    `json:"worker"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
	BeatAt    time.Time `json:"beat_at"`
	Stale     bool      `json:"stale"`
}

type JobKind struct {
	Kind          string     `json:"kind"`
	Waiting       int        `json:"waiting"`
	Failed24h     int        `json:"failed_24h"`
	OldestWaiting *time.Time `json:"oldest_waiting"`
}

type Consumer struct {
	Consumer    string    `json:"consumer"`
	LastEventID int64     `json:"last_event_id"`
	Lag         int64     `json:"lag"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SystemStatus struct {
	Version          Version    `json:"api"`
	Environment      string     `json:"environment"`
	DatabaseMillis   float64    `json:"database_ms"`
	Workers          []Worker   `json:"workers"`
	WorkerHealthy    bool       `json:"worker_healthy"`
	Jobs             []JobKind  `json:"jobs"`
	FailedJobs24h    int        `json:"failed_jobs_24h"`
	EmailFailures24h int        `json:"email_failures_24h"`
	Consumers        []Consumer `json:"consumers"`
	LatestEventID    int64      `json:"latest_event_id"`
	WebhookFailures  int        `json:"payment_webhook_failures"`
	WebhooksPending  int        `json:"payment_webhooks_pending"`
	CheckedAt        time.Time  `json:"checked_at"`
}

// emailKinds are the job kinds that send email.
const emailKinds = `kind LIKE '%email%' OR kind LIKE 'account.%' OR kind = 'staff.invite'`

func (s *Service) SystemStatus(ctx context.Context) (*SystemStatus, error) {
	st := &SystemStatus{Version: BuildVersion(), Environment: s.Facts.Env, Workers: []Worker{}, Jobs: []JobKind{}, Consumers: []Consumer{}, CheckedAt: s.Now()}
	t0 := time.Now()
	if err := s.pool.Ping(ctx); err != nil {
		return nil, err
	}
	st.DatabaseMillis = float64(time.Since(t0).Microseconds()) / 1000
	rows, err := s.pool.Query(ctx, `SELECT worker, version, started_at, beat_at FROM worker_heartbeats ORDER BY worker`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var w Worker
		if err := rows.Scan(&w.Worker, &w.Version, &w.StartedAt, &w.BeatAt); err != nil {
			rows.Close()
			return nil, err
		}
		w.Stale = s.Now().Sub(w.BeatAt) > WorkerStaleAfter
		if !w.Stale {
			st.WorkerHealthy = true
		}
		st.Workers = append(st.Workers, w)
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT kind, count(*) FILTER (WHERE status IN ('queued', 'running'))::int,
		count(*) FILTER (WHERE status = 'failed' AND coalesce(finished_at, created_at) > now() - interval '24 hours')::int,
		min(run_at) FILTER (WHERE status = 'queued')
		FROM jobs GROUP BY kind HAVING count(*) FILTER (WHERE status IN ('queued', 'running', 'failed')) > 0 ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var j JobKind
		if err := rows.Scan(&j.Kind, &j.Waiting, &j.Failed24h, &j.OldestWaiting); err != nil {
			rows.Close()
			return nil, err
		}
		st.FailedJobs24h += j.Failed24h
		st.Jobs = append(st.Jobs, j)
	}
	rows.Close()
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE status = 'failed' AND coalesce(finished_at, created_at) > now() - interval '24 hours'
		AND (`+emailKinds+`)`).Scan(&st.EmailFailures24h); err != nil {
		return nil, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(max(id), 0) FROM domain_events`).Scan(&st.LatestEventID); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT consumer, last_event_id, updated_at FROM event_consumer_offsets ORDER BY consumer`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Consumer
		if err := rows.Scan(&c.Consumer, &c.LastEventID, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		// Lag counts events after the offset; a consumer only routes the types
		// it listens to, so this is an upper bound on its outstanding work.
		c.Lag = max(st.LatestEventID-c.LastEventID, 0)
		st.Consumers = append(st.Consumers, c)
	}
	rows.Close()
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'failed')::int, count(*) FILTER (WHERE status = 'received')::int
		FROM payment_webhook_events`).Scan(&st.WebhookFailures, &st.WebhooksPending); err != nil {
		return nil, err
	}
	return st, nil
}

// ---------------------------------------------------------------- gates

type Category string

const (
	Business       Category = "business"
	Commercial     Category = "commercial"
	Operations     Category = "operations"
	Content        Category = "content"
	Engineering    Category = "engineering"
	Security       Category = "security"
	Infrastructure Category = "infrastructure"
)

// Status of a gate.
const (
	StatusVerified      = "verified"       // evidence holds and a person signed it off
	StatusConfigured    = "configured"     // evidence holds; no sign-off yet
	StatusBlocked       = "blocked"        // evidence missing or contradicts launch
	StatusNotApplicable = "not_applicable" // signed off as not applicable, with a reason
)

type gateDef struct {
	key, name, owner string
	category         Category
	critical         bool
	// check returns whether the system's own evidence holds, and what it saw.
	// Gates the system cannot observe (legal review, a restore test in
	// production) have no check and rest on sign-off with a reference.
	check func(ctx context.Context, s *Service) (bool, string, error)
}

func count(ctx context.Context, s *Service, sql string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, sql).Scan(&n)
	return n, err
}

func countCheck(sql string, describe func(n int) (bool, string)) func(ctx context.Context, s *Service) (bool, string, error) {
	return func(ctx context.Context, s *Service) (bool, string, error) {
		n, err := count(ctx, s, sql)
		if err != nil {
			return false, "", err
		}
		ok, text := describe(n)
		return ok, text, nil
	}
}

var gates = []gateDef{
	{key: "payment_provider", name: "Payment provider", owner: "Finance", category: Commercial, critical: true,
		check: func(ctx context.Context, s *Service) (bool, string, error) {
			if s.Facts.Payments != nil {
				taking, live, err := s.Facts.Payments(ctx)
				switch {
				case err != nil:
					return false, "", err
				case len(taking) == 0:
					return false, "No payment provider is taking payments.", nil
				case len(live) == 0:
					return false, fmt.Sprintf("Only test-mode providers are taking payments (%s): no real money can be collected.", strings.Join(taking, ", ")), nil
				}
				return true, fmt.Sprintf("Taking live payments: %s.", strings.Join(live, ", ")), nil
			}
			switch {
			case s.Facts.PaymentProvider == "":
				return false, "No payment provider is configured.", nil
			case !s.Facts.PaymentsLive:
				return false, fmt.Sprintf("Only the %s simulator is configured: no real payments can be taken.", s.Facts.PaymentProvider), nil
			}
			return true, fmt.Sprintf("%s is configured for live payments.", s.Facts.PaymentProvider), nil
		}},
	{key: "subscription_pricing", name: "Subscription plans and prices", owner: "Business", category: Commercial, critical: true,
		check: countCheck(`SELECT count(*) FROM subscription_plans p WHERE p.status = 'active' AND NOT p.is_demo
			AND EXISTS (SELECT 1 FROM subscription_plan_prices x WHERE x.plan_id = p.id AND x.effective_from <= now())`,
			func(n int) (bool, string) {
				return n > 0, fmt.Sprintf("%d active, non-demo plan(s) with a current price.", n)
			})},
	{key: "published_menu", name: "Published meals with approved recipes", owner: "Culinary director", category: Business, critical: true,
		check: countCheck(`SELECT count(*) FROM meals m WHERE m.status = 'published' AND NOT m.is_demo
			AND EXISTS (SELECT 1 FROM recipes r JOIN recipe_versions v ON v.recipe_id = r.id WHERE r.meal_id = m.id AND v.status = 'published')`,
			func(n int) (bool, string) {
				return n > 0, fmt.Sprintf("%d published, non-demo meal(s) backed by a published recipe version.", n)
			})},
	{key: "allergen_declarations", name: "Allergens declared for every published meal", owner: "Culinary director", category: Business, critical: true,
		check: countCheck(`SELECT count(*) FROM meals WHERE status = 'published' AND NOT is_demo AND allergen_status <> 'declared'`,
			func(n int) (bool, string) {
				return n == 0, fmt.Sprintf("%d published meal(s) without declared allergens.", n)
			})},
	{key: "meal_photography", name: "Meal photography (licensed, real)", owner: "Marketing", category: Content, critical: true,
		check: countCheck(`SELECT count(*) FROM meals m WHERE m.status = 'published' AND NOT m.is_demo
			AND NOT EXISTS (SELECT 1 FROM meal_images i WHERE i.meal_id = m.id)`,
			func(n int) (bool, string) {
				return n == 0, fmt.Sprintf("%d published meal(s) without a photograph. Licences are confirmed by sign-off.", n)
			})},
	{key: "weekly_menu", name: "A published weekly menu open for ordering", owner: "Culinary director", category: Business, critical: true,
		check: countCheck(`SELECT count(*) FROM weekly_menus WHERE status = 'published' AND cutoff_at > now()`,
			func(n int) (bool, string) {
				return n > 0, fmt.Sprintf("%d published menu(s) still open for selection.", n)
			})},
	{key: "delivery_zones", name: "Delivery zones", owner: "Operations", category: Operations, critical: true,
		check: countCheck(`SELECT count(*) FROM delivery_service_zones WHERE status = 'active'`,
			func(n int) (bool, string) { return n > 0, fmt.Sprintf("%d active delivery zone(s).", n) })},
	{key: "delivery_windows", name: "Delivery windows", owner: "Operations", category: Operations, critical: true,
		check: countCheck(`SELECT count(*) FROM delivery_windows WHERE status = 'approved'`,
			func(n int) (bool, string) { return n > 0, fmt.Sprintf("%d approved delivery window(s).", n) })},
	{key: "delivery_capacity", name: "Delivery capacity", owner: "Operations", category: Operations, critical: true,
		check: countCheck(`SELECT count(*) FROM delivery_capacity WHERE service_date >= current_date`,
			func(n int) (bool, string) { return n > 0, fmt.Sprintf("%d capacity record(s) for today or later.", n) })},
	{key: "delivery_partners", name: "Delivery partner agreements", owner: "Operations", category: Operations, critical: true,
		check: countCheck(`SELECT count(*) FROM delivery_partners WHERE status = 'approved' AND NOT demo AND agreement_status = 'signed'`,
			func(n int) (bool, string) {
				return n > 0, fmt.Sprintf("%d approved, non-demo partner(s) with a signed agreement.", n)
			})},
	{key: "proof_of_delivery_policy", name: "Proof-of-delivery policy", owner: "Operations", category: Operations, critical: true,
		check: countCheck(`SELECT count(*) FROM delivery_settings WHERE coalesce(proof_policy_ref, '') <> ''`,
			func(n int) (bool, string) {
				return n > 0, map[bool]string{true: "A proof policy reference is recorded.", false: "No proof policy reference is recorded."}[n > 0]
			})},
	{key: "cold_chain", name: "Food handling and cold-chain requirements", owner: "Quality", category: Operations, critical: true},
	{key: "production_schedule", name: "Production capacity and schedule", owner: "Production manager", category: Operations, critical: true,
		check: countCheck(`SELECT count(*) FROM production_settings WHERE capacity_kits_per_day IS NOT NULL`,
			func(n int) (bool, string) {
				return n > 0, map[bool]string{true: "Kitchen capacity is configured.", false: "Kitchen capacity is not configured."}[n > 0]
			})},
	{key: "supplier_approvals", name: "Approved suppliers", owner: "Procurement manager", category: Operations, critical: true,
		check: countCheck(`SELECT count(*) FROM suppliers WHERE status = 'approved'`,
			func(n int) (bool, string) { return n > 0, fmt.Sprintf("%d approved supplier(s).", n) })},
	{key: "inventory_readiness", name: "Opening stock counted", owner: "Inventory manager", category: Operations, critical: false,
		check: countCheck(`SELECT count(*) FROM stock_counts WHERE status = 'posted'`,
			func(n int) (bool, string) { return n > 0, fmt.Sprintf("%d posted stock count(s).", n) })},
	{key: "support_contacts", name: "Customer support contacts and hours", owner: "Support", category: Content, critical: true},
	{key: "legal_policies", name: "Privacy, terms, subscription and refund terms reviewed", owner: "Legal", category: Content, critical: true},
	{key: "email_provider", name: "Production email provider", owner: "Engineering", category: Infrastructure, critical: true,
		check: func(_ context.Context, s *Service) (bool, string, error) {
			h := strings.ToLower(s.Facts.MailHost)
			switch {
			case s.Facts.MailDriver != "smtp":
				return false, "Mail is only logged, not sent.", nil
			case h == "" || h == "localhost" || h == "mailpit" || strings.HasPrefix(h, "127."):
				return false, "Mail goes to a local test inbox (" + s.Facts.MailHost + ").", nil
			}
			return true, "Mail is sent through " + s.Facts.MailHost + ". Delivery is confirmed by sign-off with provider evidence.", nil
		}},
	{key: "domain_tls", name: "Approved domain and TLS", owner: "Engineering", category: Infrastructure, critical: true,
		check: func(_ context.Context, s *Service) (bool, string, error) {
			for _, u := range []string{s.Facts.CustomerAppURL, s.Facts.PublicSiteURL} {
				if !strings.HasPrefix(u, "https://") || strings.Contains(u, "localhost") || strings.Contains(u, "example.") {
					return false, "Site addresses are not a real HTTPS domain (" + u + ").", nil
				}
			}
			return true, "Sites are served over HTTPS at " + s.Facts.PublicSiteURL + " and " + s.Facts.CustomerAppURL + ".", nil
		}},
	{key: "demo_content_hidden", name: "Demo content hidden from customers", owner: "Engineering", category: Engineering, critical: true,
		check: func(_ context.Context, s *Service) (bool, string, error) {
			if s.Facts.DemoContentShown {
				return false, "CATALOG_DEMO_ENABLED is on: demo meals and plans are shown to customers.", nil
			}
			return true, "Demo meals and plans are hidden from customers.", nil
		}},
	{key: "backups_restore", name: "Production backups and a tested restore", owner: "Engineering", category: Infrastructure, critical: true},
	{key: "monitoring", name: "Monitoring, alerting and incident contacts", owner: "Engineering", category: Infrastructure, critical: true},
	{key: "staging_verified", name: "Staging deployment verified", owner: "Engineering", category: Infrastructure, critical: true},
	{key: "security_review", name: "Security review findings addressed", owner: "Engineering", category: Security, critical: true},
	{key: "performance", name: "Performance and load tests", owner: "Engineering", category: Engineering, critical: false},
	{key: "cross_browser", name: "Cross-browser and real-device checks", owner: "Engineering", category: Engineering, critical: false},
	{key: "accessibility", name: "Accessibility checks (automated and manual)", owner: "Engineering", category: Engineering, critical: false},
	{key: "rollback_plan", name: "Rollback plan rehearsed", owner: "Engineering", category: Infrastructure, critical: true},
	{key: "business_signoff", name: "Business go-live sign-off", owner: "Owner", category: Business, critical: true},
}

type Signoff struct {
	ID        int64     `json:"id"`
	Decision  string    `json:"decision"`
	Reference string    `json:"reference"`
	Note      string    `json:"note"`
	By        string    `json:"by"`
	At        time.Time `json:"at"`
}

type Gate struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Category Category `json:"category"`
	Owner    string   `json:"owner"`
	Critical bool     `json:"critical"`
	// Evidence is what the system observed; nil when the gate cannot be observed.
	Evidence   *string  `json:"evidence"`
	EvidenceOK *bool    `json:"evidence_ok"`
	Signoff    *Signoff `json:"signoff"`
	Status     string   `json:"status"`
}

type Overview struct {
	Ready     bool      `json:"ready"`
	Summary   string    `json:"summary"`
	Blockers  int       `json:"critical_blockers"`
	Gates     []Gate    `json:"gates"`
	CheckedAt time.Time `json:"checked_at"`
}

func (s *Service) latestSignoffs(ctx context.Context) (map[string]Signoff, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (gate) gate, id, decision, reference, note, decided_by_name, decided_at
		FROM launch_signoffs ORDER BY gate, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Signoff{}
	for rows.Next() {
		var g string
		var x Signoff
		if err := rows.Scan(&g, &x.ID, &x.Decision, &x.Reference, &x.Note, &x.By, &x.At); err != nil {
			return nil, err
		}
		if x.Decision != "revoked" {
			out[g] = x
		}
	}
	return out, rows.Err()
}

// status combines evidence and sign-off. Evidence that contradicts launch
// blocks the gate whatever was signed; a sign-off cannot override the data.
func status(def gateDef, evidenceOK *bool, so *Signoff) string {
	if so != nil && so.Decision == "not_applicable" {
		return StatusNotApplicable
	}
	if evidenceOK != nil && !*evidenceOK {
		return StatusBlocked
	}
	if so != nil && so.Decision == "approved" {
		return StatusVerified
	}
	if evidenceOK != nil {
		return StatusConfigured
	}
	return StatusBlocked // nothing observable and nobody has signed it off
}

func (s *Service) Gates(ctx context.Context) (*Overview, error) {
	signed, err := s.latestSignoffs(ctx)
	if err != nil {
		return nil, err
	}
	ov := &Overview{Gates: make([]Gate, 0, len(gates)), CheckedAt: s.Now()}
	for _, def := range gates {
		g := Gate{Key: def.key, Name: def.name, Category: def.category, Owner: def.owner, Critical: def.critical}
		if def.check != nil {
			ok, text, err := def.check(ctx, s)
			if err != nil {
				return nil, fmt.Errorf("gate %s: %w", def.key, err)
			}
			g.Evidence, g.EvidenceOK = &text, &ok
		}
		if so, found := signed[def.key]; found {
			g.Signoff = &so
		}
		g.Status = status(def, g.EvidenceOK, g.Signoff)
		if def.critical && g.Status != StatusVerified && g.Status != StatusNotApplicable {
			ov.Blockers++
		}
		ov.Gates = append(ov.Gates, g)
	}
	ov.Ready = ov.Blockers == 0
	if ov.Ready {
		ov.Summary = "Every critical gate is verified or signed off as not applicable."
	} else {
		ov.Summary = fmt.Sprintf("Not ready to launch: %d critical gate(s) are not verified.", ov.Blockers)
	}
	return ov, nil
}

type SignoffInput struct {
	Decision  string `json:"decision"`
	Reference string `json:"reference"`
	Note      string `json:"note"`
}

// SignOff records a decision on a gate. Approving needs the gate's own
// evidence to hold (where it has any): a sign-off records a person's
// judgement on top of the data, it does not replace the data.
func (s *Service) SignOff(ctx context.Context, p *identity.Principal, gate string, in SignoffInput) (*Gate, error) {
	var def *gateDef
	for i := range gates {
		if gates[i].key == gate {
			def = &gates[i]
		}
	}
	if def == nil {
		return nil, httpx.ErrNotFound
	}
	in.Reference, in.Note = strings.TrimSpace(in.Reference), strings.TrimSpace(in.Note)
	var f []httpx.FieldError
	if in.Decision != "approved" && in.Decision != "not_applicable" && in.Decision != "revoked" {
		f = append(f, httpx.FieldError{Field: "decision", Code: "invalid", Message: "Choose approved, not applicable or revoked."})
	}
	if n := len([]rune(in.Reference)); n < 3 || n > 300 {
		f = append(f, httpx.FieldError{Field: "reference", Code: "length", Message: "Say where the evidence or approval is recorded (3 to 300 characters)."})
	}
	if in.Decision == "not_applicable" && len([]rune(in.Note)) < 10 {
		f = append(f, httpx.FieldError{Field: "note", Code: "required", Message: "Explain why this gate does not apply (at least 10 characters)."})
	}
	if len([]rune(in.Note)) > 2000 {
		f = append(f, httpx.FieldError{Field: "note", Code: "length", Message: "Keep the note under 2,000 characters."})
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	if in.Decision == "approved" && def.check != nil {
		ok, text, err := def.check(ctx, s)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, httpx.NewError(http.StatusConflict, "evidence_missing", "This gate cannot be approved while the system shows: "+text)
		}
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO launch_signoffs (gate, decision, reference, note, decided_by, decided_by_name) VALUES ($1, $2, $3, $4, $5, $6)`,
			gate, in.Decision, in.Reference, in.Note, p.UserID, p.Name()); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "launch.signoff", TargetType: "launch_gate", TargetID: gate,
			Metadata: map[string]any{"decision": in.Decision, "reference": in.Reference}})
	})
	if err != nil {
		return nil, err
	}
	ov, err := s.Gates(ctx)
	if err != nil {
		return nil, err
	}
	for i := range ov.Gates {
		if ov.Gates[i].Key == gate {
			return &ov.Gates[i], nil
		}
	}
	return nil, errors.New("launch: gate vanished")
}
