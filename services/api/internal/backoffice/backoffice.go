// Package backoffice is the staff administration layer (Sprint 4): the
// Customer 360, staff alerts, exports, business settings, integration
// status and the audit log viewer. It owns admin_alerts, admin_alert_reads,
// report_exports and business_settings; everything else it shows comes from
// the owning modules (customers, subscriptions, orders, billing, payments,
// crm, support) or the read-only reporting package.
package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/billing"
	"mise.tt/api/internal/commerce"
	"mise.tt/api/internal/crm"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/orders"
	"mise.tt/api/internal/payments"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/reporting"
	"mise.tt/api/internal/subscriptions"
	"mise.tt/api/internal/support"
)

// JobExport builds one CSV export.
const JobExport = "backoffice.export"

// Integrations is what configuration says about outside services. It never
// holds a secret.
type Integrations struct {
	Env              string
	MailDriver       string
	MailHost         string
	PaymentsProvider string
	PaymentsLive     bool
}

// Policy is a setting owned by configuration or another module, shown
// read-only so staff know what is in force.
type Policy struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Owner string `json:"owner"`
}

type Service struct {
	pool      *pgxpool.Pool
	Reporting *reporting.Service
	CRM       *crm.Service
	Support   *support.Service
	Commerce  *commerce.Service
	Subs      *subscriptions.Service
	Orders    *orders.Service
	Billing   *billing.Service
	Payments  *payments.Service
	Identity  *identity.Service
	Delivery  delivery.Eligibility
	Integ     Integrations
	Policies  []Policy
	Now       func() time.Time
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

// ---------------------------------------------------------------- alerts

// Raise records an alert once per dedupe key. It implements support.Alerts.
func (s *Service) Raise(ctx context.Context, q db.Querier, a support.Alert) error {
	_, err := q.Exec(ctx, `
		INSERT INTO admin_alerts (dedupe_key, kind, severity, title, body, resource_type, resource_id, permission)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8)
		ON CONFLICT (dedupe_key) DO NOTHING`,
		a.DedupeKey, a.Kind, a.Severity, a.Title, a.Body, a.ResourceType, a.ResourceID, a.Permission)
	return err
}

// Alert is an alert as one staff member sees it.
type Alert struct {
	ID           int64      `json:"id"`
	Kind         string     `json:"kind"`
	Severity     string     `json:"severity"`
	Title        string     `json:"title"`
	Body         string     `json:"body"`
	ResourceType *string    `json:"resource_type"`
	ResourceID   *string    `json:"resource_id"`
	CreatedAt    time.Time  `json:"created_at"`
	ResolvedAt   *time.Time `json:"resolved_at"`
	Read         bool       `json:"read"`
}

// Alerts lists the alerts this person may see (by permission), newest
// first, with their own read state.
func (s *Service) Alerts(ctx context.Context, p *identity.Principal, unreadOnly bool) ([]Alert, int, error) {
	perms := make([]string, len(p.Permissions))
	for i, x := range p.Permissions {
		perms[i] = string(x)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.kind, a.severity, a.title, a.body, a.resource_type, a.resource_id, a.created_at, a.resolved_at, r.alert_id IS NOT NULL
		FROM admin_alerts a LEFT JOIN admin_alert_reads r ON r.alert_id = a.id AND r.user_id = $1
		WHERE a.permission = ANY($2::text[]) AND a.created_at > now() - interval '30 days'
		  AND (NOT $3::bool OR r.alert_id IS NULL)
		ORDER BY a.created_at DESC LIMIT 100`, p.UserID, perms, unreadOnly)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Alert{}
	unread := 0
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.Kind, &a.Severity, &a.Title, &a.Body, &a.ResourceType, &a.ResourceID, &a.CreatedAt, &a.ResolvedAt, &a.Read); err != nil {
			return nil, 0, err
		}
		if !a.Read {
			unread++
		}
		out = append(out, a)
	}
	return out, unread, rows.Err()
}

// MarkRead marks alerts read for one person (all visible ones when ids is empty).
func (s *Service) MarkRead(ctx context.Context, p *identity.Principal, ids []int64) error {
	perms := make([]string, len(p.Permissions))
	for i, x := range p.Permissions {
		perms[i] = string(x)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO admin_alert_reads (alert_id, user_id)
		SELECT a.id, $1 FROM admin_alerts a
		WHERE a.permission = ANY($2::text[]) AND (cardinality($3::bigint[]) = 0 OR a.id = ANY($3::bigint[]))
		ON CONFLICT DO NOTHING`, p.UserID, perms, ids)
	return err
}

// ScanAlerts raises alerts for conditions that need someone. Each alert has
// a stable dedupe key, so running it every minute raises each one once.
func (s *Service) ScanAlerts(ctx context.Context, now time.Time) (int, error) {
	type found struct{ key, kind, sev, title, body, rtype, rid, perm string }
	var list []found
	collect := func(sql string, build func(rows pgx.Rows) (found, error), args ...any) error {
		rows, err := s.pool.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := build(rows)
			if err != nil {
				return err
			}
			list = append(list, f)
		}
		return rows.Err()
	}
	steps := []error{
		collect(`SELECT p.id, p.amount_minor, p.currency, p.customer_id FROM payments p WHERE p.status = 'failed' AND p.updated_at > $1::timestamptz - interval '7 days'`,
			func(r pgx.Rows) (found, error) {
				var id, cur, cid string
				var amt int64
				err := r.Scan(&id, &amt, &cur, &cid)
				return found{"payment_failed:" + id, "payment_failed", "warning", "A payment failed",
					fmt.Sprintf("A %s payment of %d.%02d did not go through.", cur, amt/100, amt%100), "customer", cid, "payments.read"}, err
			}, now),
		collect(`SELECT id, coalesce(attention_reason, '') FROM payments WHERE needs_attention`,
			func(r pgx.Rows) (found, error) {
				var id, why string
				err := r.Scan(&id, &why)
				return found{"payment_attention:" + id, "payment_attention", "critical", "A payment needs review", why, "payment", id, "payments.read"}, err
			}),
		collect(`SELECT id, customer_id, coalesce(state_reason, ''), week_start FROM subscription_cycles WHERE state = 'needs_resolution' AND week_start > $1::date - 14`,
			func(r pgx.Rows) (found, error) {
				var id, cid, why string
				var week time.Time
				err := r.Scan(&id, &cid, &why, &week)
				return found{"week_resolution:" + id, "subscription_exception", "warning", "A paid week needs a decision",
					"Week of " + week.Format("2 Jan") + ": " + strings.ReplaceAll(why, "_", " ") + ".", "customer", cid, "subscriptions.read"}, err
			}, now),
		collect(`SELECT id, kind, coalesce(last_error, '') FROM jobs WHERE status = 'failed' AND finished_at > $1::timestamptz - interval '7 days'`,
			func(r pgx.Rows) (found, error) {
				var id int64
				var kind, msg string
				err := r.Scan(&id, &kind, &msg)
				if len(msg) > 300 {
					msg = msg[:300]
				}
				return found{fmt.Sprintf("job_failed:%d", id), "job_failed", "warning", "A background job failed: " + kind, msg, "", "", "payments.read"}, err
			}, now),
		collect(`SELECT sc.week_start, count(*) FROM subscription_cycles sc LEFT JOIN meal_selections ms ON ms.subscription_cycle_id = sc.id
		         WHERE sc.state = 'scheduled' AND sc.cutoff_at > $1::timestamptz AND sc.cutoff_at <= $1::timestamptz + interval '24 hours'
		           AND (ms.id IS NULL OR ms.status = 'draft') GROUP BY sc.week_start`,
			func(r pgx.Rows) (found, error) {
				var week time.Time
				var n int64
				err := r.Scan(&week, &n)
				return found{"cutoff_unconfirmed:" + week.Format("2006-01-02"), "upcoming_cutoff", "info",
					fmt.Sprintf("%d customers haven't confirmed meals; cutoff within 24 hours", n),
					"Week of " + week.Format("2 Jan") + ".", "", "", "subscriptions.read"}, err
			}, now),
	}
	// Supply chain (Sprint 6). Business dates in Trinidad and Tobago.
	today := `($1::timestamptz AT TIME ZONE 'America/Port_of_Spain')::date`
	steps = append(steps,
		collect(`SELECT po.id, po.number, s.name, po.expected_delivery FROM purchase_orders po JOIN suppliers s ON s.id = po.supplier_id
		         WHERE po.status IN ('approved', 'issued', 'partially_received') AND po.expected_delivery < `+today,
			func(r pgx.Rows) (found, error) {
				var id, number, supplier string
				var expected time.Time
				err := r.Scan(&id, &number, &supplier, &expected)
				return found{"po_overdue:" + id + ":" + expected.Format("2006-01-02"), "purchase_order_overdue", "warning",
					number + " from " + supplier + " is overdue", "Expected " + expected.Format("2 Jan") + " and not fully received.",
					"purchase_order", id, "purchase_orders.read"}, err
			}, now),
		collect(`SELECT l.id, l.code, i.name, l.expiry_date FROM inventory_lots l JOIN ingredients i ON i.id = l.ingredient_id
		         WHERE l.on_hand > 0 AND l.status IN ('available', 'on_hold') AND l.expiry_status = 'recorded' AND l.expiry_date < `+today,
			func(r pgx.Rows) (found, error) {
				var id, code, name string
				var expiry time.Time
				err := r.Scan(&id, &code, &name, &expiry)
				return found{"lot_expired:" + id, "lot_expired", "warning", name + " lot " + code + " has expired",
					"Expired " + expiry.Format("2 Jan") + ". It is no longer allocated; record waste or a return.", "inventory_lot", id, "inventory.read"}, err
			}, now),
		collect(`SELECT l.id, l.code, i.name FROM inventory_lots l JOIN ingredients i ON i.id = l.ingredient_id
		         WHERE l.on_hand > 0 AND l.status = 'available' AND l.expiry_status = 'not_recorded' AND $1::timestamptz IS NOT NULL`,
			func(r pgx.Rows) (found, error) {
				var id, code, name string
				err := r.Scan(&id, &code, &name)
				return found{"lot_expiry_review:" + id, "lot_expiry_review", "info", name + " lot " + code + " needs an expiry review",
					"No expiry date was recorded on receipt, so it is not used until someone records one or confirms none applies.",
					"inventory_lot", id, "receiving.inspect"}, err
			}, now),
		collect(`SELECT p.id, p.number FROM production_plans p WHERE p.status = 'released' AND p.released_at < $1::timestamptz - interval '1 hour'
		         AND p.week_start >= `+today+` - 7 AND NOT EXISTS (SELECT 1 FROM procurement_runs r WHERE r.plan_id = p.id AND r.status <> 'cancelled')`,
			func(r pgx.Rows) (found, error) {
				var id, number string
				err := r.Scan(&id, &number)
				return found{"procurement_missing:" + id, "procurement_missing", "warning", "Plan " + number + " has no procurement run",
					"It was released over an hour ago and purchasing has not picked it up. Create the run from Procurement planning.",
					"production_plan", id, "procurement.plan"}, err
			}, now),
	)
	if stale, err := s.Support.Stale(ctx, s.pool, now.Add(-72*time.Hour)); err == nil {
		for _, c := range stale {
			list = append(list, found{fmt.Sprintf("case_stale:%s:%s", c.ID, now.Format("2006-01-02")), "support_case", "warning",
				fmt.Sprintf("Case #%d has had no update for 3 days", c.Number), c.Subject, "support_case", c.ID, "cases.read"})
		}
	} else {
		steps = append(steps, err)
	}
	for _, err := range steps {
		if err != nil {
			return 0, err
		}
	}
	raised := 0
	for _, f := range list {
		tag, err := s.pool.Exec(ctx, `
			INSERT INTO admin_alerts (dedupe_key, kind, severity, title, body, resource_type, resource_id, permission)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8) ON CONFLICT (dedupe_key) DO NOTHING`,
			f.key, f.kind, f.sev, f.title, f.body, f.rtype, f.rid, f.perm)
		if err != nil {
			return raised, err
		}
		raised += int(tag.RowsAffected())
	}
	// Payments reviewed or weeks resolved since: mark their alerts resolved.
	_, err := s.pool.Exec(ctx, `
		UPDATE admin_alerts a SET resolved_at = now()
		WHERE a.resolved_at IS NULL AND a.kind = 'payment_attention'
		  AND NOT EXISTS (SELECT 1 FROM payments p WHERE p.id::text = a.resource_id AND p.needs_attention)`)
	return raised, err
}

// RunAlerts scans every interval until ctx ends.
func (s *Service) RunAlerts(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := s.ScanAlerts(ctx, s.Now()); err != nil {
			slog.ErrorContext(ctx, "alert scan failed", "error", err.Error())
		} else if n > 0 {
			slog.InfoContext(ctx, "alerts raised", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---------------------------------------------------------------- settings

// Setting keys staff can change, with labels.
var SettingKeys = []struct{ Key, Label string }{
	{"business.display_name", "Business name"},
	{"business.support_email", "Support email"},
	{"business.support_phone", "Support phone"},
	{"business.whatsapp", "WhatsApp number"},
	{"business.address", "Business address"},
}

type Setting struct {
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	Value     string     `json:"value"`
	UpdatedAt *time.Time `json:"updated_at"`
}

type Settings struct {
	Editable []Setting `json:"editable"`
	Policies []Policy  `json:"policies"`
}

func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value, updated_at FROM business_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stored := map[string]Setting{}
	for rows.Next() {
		var st Setting
		var at time.Time
		if err := rows.Scan(&st.Key, &st.Value, &at); err != nil {
			return nil, err
		}
		st.UpdatedAt = &at
		stored[st.Key] = st
	}
	out := &Settings{Editable: []Setting{}, Policies: s.Policies}
	for _, k := range SettingKeys {
		st := stored[k.Key]
		st.Key, st.Label = k.Key, k.Label
		out.Editable = append(out.Editable, st)
	}
	if out.Policies == nil {
		out.Policies = []Policy{}
	}
	return out, rows.Err()
}

// PutSettings saves contact settings. Each change is audited with the old
// and new value (they are business contact details, not personal data).
func (s *Service) PutSettings(ctx context.Context, actor string, values map[string]string) (*Settings, error) {
	var f []httpx.FieldError
	for k, v := range values {
		known := slices.ContainsFunc(SettingKeys, func(x struct{ Key, Label string }) bool { return x.Key == k })
		v = strings.TrimSpace(v)
		switch {
		case !known:
			f = append(f, httpx.FieldError{Field: k, Code: "unknown", Message: "This setting can't be changed here."})
		case len([]rune(v)) > 500:
			f = append(f, httpx.FieldError{Field: k, Code: "length", Message: "Keep it under 500 characters."})
		case k == "business.support_email" && v != "" && !strings.Contains(v, "@"):
			f = append(f, httpx.FieldError{Field: k, Code: "invalid", Message: "Enter an email address."})
		}
		values[k] = v
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		for k, v := range values {
			var old *string
			_ = tx.QueryRow(ctx, `SELECT value FROM business_settings WHERE key = $1`, k).Scan(&old)
			if old != nil && *old == v {
				continue
			}
			if v == "" {
				if _, err := tx.Exec(ctx, `DELETE FROM business_settings WHERE key = $1`, k); err != nil {
					return err
				}
			} else if _, err := tx.Exec(ctx, `
				INSERT INTO business_settings (key, value, updated_by) VALUES ($1, $2, $3)
				ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`, k, v, actor); err != nil {
				return err
			}
			if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "setting.changed", TargetType: "setting", TargetID: k,
				Metadata: map[string]any{"before": old, "after": v}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Settings(ctx)
}

// ---------------------------------------------------------------- exports

// Export is an export request as its requester sees it.
type Export struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Params      json.RawMessage `json:"params"`
	Status      string          `json:"status"`
	RowCount    *int32          `json:"row_count"`
	Truncated   bool            `json:"truncated"`
	Error       *string         `json:"error"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

// ExportInput asks for an export.
type ExportInput struct {
	Kind string `json:"kind"`
	reporting.ExportParams
}

// RequestExport checks the permission for the kind, records the request and
// queues the job. The CSV is built in the background.
func (s *Service) RequestExport(ctx context.Context, p *identity.Principal, in ExportInput) (*Export, error) {
	perm, ok := reporting.ExportKinds[in.Kind]
	if !ok {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "kind", Code: "invalid", Message: "Choose what to export."}})
	}
	if !p.Can(identity.Permission(perm)) {
		return nil, httpx.ErrForbidden
	}
	for _, d := range []string{in.From, in.To} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return nil, httpx.ValidationError([]httpx.FieldError{{Field: "from", Code: "invalid", Message: "Dates are YYYY-MM-DD."}})
			}
		}
	}
	if in.Status != "" && (len(in.Status) > 30 || strings.ContainsAny(in.Status, " ;'\"")) {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "status", Code: "invalid", Message: "Unknown status."}})
	}
	if in.SegmentID != "" {
		if in.Kind != "customers" {
			return nil, httpx.ValidationError([]httpx.FieldError{{Field: "segment_id", Code: "invalid", Message: "Segments apply to customer exports."}})
		}
		if _, err := s.CRM.Segment(ctx, in.SegmentID); err != nil {
			return nil, err
		}
	}
	params, _ := json.Marshal(in.ExportParams)
	var e Export
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM report_exports WHERE requested_by = $1 AND created_at > now() - interval '1 hour'`, p.UserID).Scan(&recent); err != nil {
			return err
		}
		if recent >= 20 {
			return httpx.ErrRateLimited
		}
		if err := tx.QueryRow(ctx, `INSERT INTO report_exports (kind, params, requested_by) VALUES ($1, $2, $3)
			RETURNING id, kind, params, status, row_count, truncated, error, created_at, completed_at, expires_at`, in.Kind, params, p.UserID).
			Scan(&e.ID, &e.Kind, &e.Params, &e.Status, &e.RowCount, &e.Truncated, &e.Error, &e.CreatedAt, &e.CompletedAt, &e.ExpiresAt); err != nil {
			return err
		}
		if err := jobs.Enqueue(ctx, tx, JobExport, map[string]string{"export_id": e.ID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "export.requested", TargetType: "export", TargetID: e.ID,
			Metadata: map[string]any{"kind": in.Kind, "params": in.ExportParams}})
	})
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// HandleExport is the JobExport handler.
func (s *Service) HandleExport(ctx context.Context, payload json.RawMessage) error {
	var job struct {
		ExportID string `json:"export_id"`
	}
	if err := json.Unmarshal(payload, &job); err != nil {
		return err
	}
	var kind string
	var params reporting.ExportParams
	var raw []byte
	err := s.pool.QueryRow(ctx, `UPDATE report_exports SET status = 'running' WHERE id = $1 AND status IN ('queued', 'running')
		RETURNING kind, params`, job.ExportID).Scan(&kind, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = json.Unmarshal(raw, &params)
	if params.SegmentID != "" {
		seg, err := s.CRM.Segment(ctx, params.SegmentID)
		if err != nil {
			return s.failExport(ctx, job.ExportID, "The segment no longer exists.")
		}
		params.Rules = &seg.Rules
	}
	content, n, truncated, err := s.Reporting.BuildExport(ctx, kind, params)
	if err != nil {
		slog.ErrorContext(ctx, "export failed", "export_id", job.ExportID, "error", err.Error())
		return s.failExport(ctx, job.ExportID, "The export could not be built.")
	}
	_, err = s.pool.Exec(ctx, `UPDATE report_exports SET status = 'ready', content = $2, row_count = $3, truncated = $4, completed_at = now(),
		expires_at = now() + interval '24 hours' WHERE id = $1`, job.ExportID, content, n, truncated)
	return err
}

func (s *Service) failExport(ctx context.Context, id, msg string) error {
	_, err := s.pool.Exec(ctx, `UPDATE report_exports SET status = 'failed', error = $2, completed_at = now() WHERE id = $1`, id, msg)
	return err
}

// Exports lists the requester's own exports.
func (s *Service) Exports(ctx context.Context, p *identity.Principal) ([]Export, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, params, CASE WHEN status = 'ready' AND expires_at < now() THEN 'expired' ELSE status END,
		       row_count, truncated, error, created_at, completed_at, expires_at
		FROM report_exports WHERE requested_by = $1 ORDER BY created_at DESC LIMIT 50`, p.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Export{}
	for rows.Next() {
		var e Export
		if err := rows.Scan(&e.ID, &e.Kind, &e.Params, &e.Status, &e.RowCount, &e.Truncated, &e.Error, &e.CreatedAt, &e.CompletedAt, &e.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

var errExportGone = httpx.NewError(http.StatusGone, "export_expired", "This export has expired or is not ready. Request a new one.")

// DownloadExport returns a ready export's CSV to the person who asked for
// it (and only them, and only before it expires). Each download is audited.
func (s *Service) DownloadExport(ctx context.Context, p *identity.Principal, id string) (string, []byte, error) {
	var kind, status string
	var content []byte
	var expires time.Time
	var requester string
	err := s.pool.QueryRow(ctx, `SELECT kind, status, content, expires_at, requested_by FROM report_exports WHERE id = $1`, id).
		Scan(&kind, &status, &content, &expires, &requester)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && requester != p.UserID) {
		return "", nil, httpx.ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	// The permission is checked again: a role may have been removed since.
	if !p.Can(identity.Permission(reporting.ExportKinds[kind])) {
		return "", nil, httpx.ErrForbidden
	}
	if status != "ready" || !s.Now().Before(expires) {
		return "", nil, errExportGone
	}
	if err := audit.Record(ctx, s.pool, audit.Entry{ActorUserID: p.UserID, Action: "export.downloaded", TargetType: "export", TargetID: id,
		Metadata: map[string]any{"kind": kind}}); err != nil {
		return "", nil, err
	}
	return kind, content, nil
}

// ---------------------------------------------------------------- integrations

// IntegrationStatus is what is known about one outside service. Status is
// one of not_configured, test_mode, configured (set up but unverified) and
// working (with evidence: a recent success).
type IntegrationStatus struct {
	Key           string     `json:"key"`
	Name          string     `json:"name"`
	Purpose       string     `json:"purpose"`
	Status        string     `json:"status"`
	Detail        string     `json:"detail"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     *string    `json:"last_error"`
	LastErrorAt   *time.Time `json:"last_error_at"`
}

func (s *Service) IntegrationStatuses(ctx context.Context) ([]IntegrationStatus, error) {
	out := []IntegrationStatus{}
	// Email: evidence is the last email job that completed or failed.
	email := IntegrationStatus{Key: "email", Name: "Transactional email", Purpose: "Account, order and payment emails"}
	var lastOK, lastErrAt *time.Time
	var lastErr *string
	_ = s.pool.QueryRow(ctx, `SELECT max(finished_at) FROM jobs WHERE status = 'done' AND (kind LIKE 'account.%' OR kind LIKE 'email.%' OR kind IN ('commerce.email', 'staff.invite'))`).Scan(&lastOK)
	_ = s.pool.QueryRow(ctx, `SELECT finished_at, last_error FROM jobs WHERE status = 'failed' AND (kind LIKE 'account.%' OR kind LIKE 'email.%' OR kind IN ('commerce.email', 'staff.invite'))
		ORDER BY finished_at DESC LIMIT 1`).Scan(&lastErrAt, &lastErr)
	email.LastSuccessAt, email.LastError, email.LastErrorAt = lastOK, lastErr, lastErrAt
	switch {
	case s.Integ.MailDriver == "log":
		email.Status, email.Detail = "test_mode", "Emails are written to the log, not sent (local development)."
	case lastOK != nil && lastOK.After(s.Now().Add(-7*24*time.Hour)):
		email.Status, email.Detail = "working", "Sending through SMTP ("+s.Integ.MailHost+")."
	default:
		email.Status, email.Detail = "configured", "SMTP is set up ("+s.Integ.MailHost+"); no email has been sent in the last 7 days."
	}
	out = append(out, email)

	pay := IntegrationStatus{Key: "payments", Name: "Payment provider", Purpose: "Checkout, weekly charges and refunds"}
	_ = s.pool.QueryRow(ctx, `SELECT max(processed_at) FROM payment_webhook_events WHERE status = 'processed'`).Scan(&pay.LastSuccessAt)
	switch {
	case s.Integ.PaymentsProvider == "":
		pay.Status, pay.Detail = "not_configured", "No payment provider has been chosen. Customers cannot pay."
	case !s.Integ.PaymentsLive:
		pay.Status, pay.Detail = "test_mode", "Using the "+s.Integ.PaymentsProvider+" simulator. No real money moves."
	default:
		pay.Status, pay.Detail = "configured", s.Integ.PaymentsProvider
	}
	out = append(out, pay)

	analytics := IntegrationStatus{Key: "analytics", Name: "First-party analytics", Purpose: "Site events for acquisition reporting"}
	_ = s.pool.QueryRow(ctx, `SELECT max(created_at) FROM lead_events`).Scan(&analytics.LastSuccessAt)
	analytics.Status, analytics.Detail = "configured", "Built in. Events are stored by Mise."
	if analytics.LastSuccessAt != nil && analytics.LastSuccessAt.After(s.Now().Add(-7*24*time.Hour)) {
		analytics.Status = "working"
	}
	out = append(out, analytics)

	for _, x := range []struct{ key, name, purpose string }{
		{"food_drop", "Food Drop", "Delivery partner"},
		{"hubspot", "HubSpot", "CRM sync"},
		{"klaviyo", "Klaviyo", "Marketing email"},
		{"sms_whatsapp", "SMS and WhatsApp", "Customer messages by phone"},
	} {
		out = append(out, IntegrationStatus{Key: x.key, Name: x.name, Purpose: x.purpose, Status: "not_configured",
			Detail: "Not set up: no provider approved and no credentials."})
	}
	return out, nil
}

// ---------------------------------------------------------------- audit log

type AuditFilter struct {
	Actor      string
	Action     string // a prefix, e.g. "staff." or "subscription."
	TargetType string
	TargetID   string
	From, To   string
	Page, Size int
}

type AuditEntry struct {
	ID         int64          `json:"id"`
	At         time.Time      `json:"at"`
	ActorID    *string        `json:"actor_id"`
	ActorName  *string        `json:"actor_name"`
	Action     string         `json:"action"`
	TargetType *string        `json:"target_type"`
	TargetID   *string        `json:"target_id"`
	Reason     *string        `json:"reason"`
	RequestID  *string        `json:"request_id"`
	Metadata   map[string]any `json:"metadata"`
}

func (s *Service) AuditLog(ctx context.Context, f AuditFilter) ([]AuditEntry, int, error) {
	if f.Size < 1 || f.Size > 100 {
		f.Size = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.created_at, a.actor_user_id, nullif(trim(u.first_name || ' ' || u.last_name), ''), a.action, a.target_type, a.target_id,
		       a.reason, a.request_id, a.metadata, count(*) OVER ()
		FROM audit_log a LEFT JOIN users u ON u.id = a.actor_user_id
		WHERE ($1 = '' OR a.actor_user_id::text = $1)
		  AND ($2 = '' OR a.action LIKE $2 || '%')
		  AND ($3 = '' OR a.target_type = $3)
		  AND ($4 = '' OR a.target_id = $4)
		  AND ($5 = '' OR a.created_at >= (nullif($5, '')::date)::timestamp AT TIME ZONE 'America/Port_of_Spain')
		  AND ($6 = '' OR a.created_at < ((nullif($6, '')::date) + 1)::timestamp AT TIME ZONE 'America/Port_of_Spain')
		ORDER BY a.id DESC LIMIT $7 OFFSET $8`,
		f.Actor, f.Action, f.TargetType, f.TargetID, f.From, f.To, f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	total := 0
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorName, &e.Action, &e.TargetType, &e.TargetID, &e.Reason, &e.RequestID, &e.Metadata, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
