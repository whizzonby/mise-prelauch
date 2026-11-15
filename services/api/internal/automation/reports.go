package automation

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/analytics"
	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/mail"
)

// Dashboards is the analytics registry's dashboards (the same figures as
// on screen; reports never calculate anything themselves).
type Dashboards interface {
	Dashboard(ctx context.Context, kind string, r analytics.Range, f analytics.Filters) (*analytics.Dashboard, error)
}

// Mailer sends email (nil: no email service configured).
type Mailer interface {
	Send(ctx context.Context, m mail.Message) error
}

// Contacts resolves a staff account's email and whether it may receive a report.
type Contacts interface {
	StaffEmail(ctx context.Context, userID string) (email string, active bool, err error)
	HasPermission(ctx context.Context, userID, permission string) (bool, error)
}

type reportKind struct {
	dashboards []string
	permission string
	period     func(today time.Time) (time.Time, time.Time) // the last complete period
}

func lastWeek(today time.Time) (time.Time, time.Time) {
	wd := (int(today.Weekday()) + 6) % 7 // Monday = 0
	thisMon := today.AddDate(0, 0, -wd)
	return thisMon.AddDate(0, 0, -7), thisMon.AddDate(0, 0, -1)
}

var reportKinds = map[string]reportKind{
	"daily_operations": {[]string{"overview", "delivery"}, "analytics_executive.read", func(t time.Time) (time.Time, time.Time) {
		y := t.AddDate(0, 0, -1)
		return y, y
	}},
	"weekly_business":    {[]string{"overview", "revenue"}, "analytics_revenue.read", lastWeek},
	"weekly_production":  {[]string{"production", "meals"}, "analytics_production.read", lastWeek},
	"weekly_procurement": {[]string{"procurement", "inventory"}, "analytics_procurement.read", lastWeek},
	"monthly_executive": {[]string{"overview", "revenue", "customers"}, "analytics_executive.read", func(t time.Time) (time.Time, time.Time) {
		first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return first.AddDate(0, -1, 0), first.AddDate(0, 0, -1)
	}},
}

// Reports runs and lists scheduled reports.
type Reports struct {
	S          *Service
	Dashboards Dashboards
	Mailer     Mailer
	Contacts   Contacts
	Location   *time.Location
}

type Schedule struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	Enabled     bool       `json:"enabled"`
	Recipients  []string   `json:"recipients"`
	Emails      []string   `json:"recipient_emails"`
	ApprovalRef string     `json:"approval_ref"`
	Permission  string     `json:"permission"`
	Dashboards  []string   `json:"dashboards"`
	NextPeriod  string     `json:"next_period"`
	LastRun     *time.Time `json:"last_run"`
	Missing     []string   `json:"missing"`
}

func (r *Reports) today() time.Time {
	t := r.S.Now().In(r.Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (r *Reports) Schedules(ctx context.Context) ([]Schedule, error) {
	rows, err := r.S.pool.Query(ctx, `SELECT s.id::text, s.kind, s.name, s.enabled, coalesce((SELECT array_agg(x::text) FROM unnest(s.recipients) x), '{}'),
		coalesce((SELECT array_agg(u.email ORDER BY u.email) FROM users u WHERE u.id = ANY(s.recipients)), '{}'),
		s.approval_ref, (SELECT max(generated_at) FROM report_runs rr WHERE rr.schedule_id = s.id) FROM scheduled_reports s ORDER BY s.kind`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Schedule, error) {
		var s Schedule
		err := row.Scan(&s.ID, &s.Kind, &s.Name, &s.Enabled, &s.Recipients, &s.Emails, &s.ApprovalRef, &s.LastRun)
		return s, err
	})
	if err != nil {
		return nil, err
	}
	today := r.today()
	for i := range out {
		k := reportKinds[out[i].Kind]
		out[i].Permission, out[i].Dashboards = k.permission, k.dashboards
		f, t := k.period(today)
		out[i].NextPeriod = f.Format("2006-01-02") + " to " + t.Format("2006-01-02")
		out[i].Missing = []string{}
		if out[i].ApprovalRef == "" {
			out[i].Missing = append(out[i].Missing, "Not approved: no approval reference.")
		}
		if len(out[i].Recipients) == 0 {
			out[i].Missing = append(out[i].Missing, "No recipients: reports are generated and kept here, not emailed.")
		}
	}
	return out, nil
}

type ScheduleInput struct {
	Enabled     *bool     `json:"enabled"`
	Recipients  *[]string `json:"recipients"` // staff emails
	ApprovalRef *string   `json:"approval_ref"`
}

// SaveSchedule sets recipients and enabling. Every recipient must be an
// active staff account holding the report's permission.
func (r *Reports) SaveSchedule(ctx context.Context, actor, id string, in ScheduleInput) error {
	return db.InTx(ctx, r.S.pool, func(tx pgx.Tx) error {
		var kind, ref string
		var enabled bool
		var recips []string
		err := tx.QueryRow(ctx, `SELECT kind, enabled, approval_ref, coalesce((SELECT array_agg(x::text) FROM unnest(recipients) x), '{}') FROM scheduled_reports
			WHERE id::text = $1 FOR UPDATE`, id).Scan(&kind, &enabled, &ref, &recips)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if in.Recipients != nil {
			recips = []string{}
			for _, e := range *in.Recipients {
				e = strings.ToLower(strings.TrimSpace(e))
				if e == "" {
					continue
				}
				var uid string
				if err := tx.QueryRow(ctx, `SELECT u.id::text FROM users u WHERE u.email_canonical = $1 AND u.status = 'active'
					AND EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key = ur.role_key WHERE ur.user_id = u.id
					AND rp.permission_key = $2)`, e, reportKinds[kind].permission).Scan(&uid); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return fieldErr("recipients", e+" is not an active staff account allowed to see this report.")
					}
					return err
				}
				recips = append(recips, uid)
			}
		}
		if in.ApprovalRef != nil {
			ref = strings.TrimSpace(*in.ApprovalRef)
		}
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		if enabled && ref == "" {
			return fieldErr("approval_ref", "Name the decision that approved this report before enabling it.")
		}
		if _, err := tx.Exec(ctx, `UPDATE scheduled_reports SET enabled = $2, recipients = $3::uuid[], approval_ref = $4, updated_by = $5, updated_at = now() WHERE id::text = $1`,
			id, enabled, recips, ref, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "automation.report_schedule_saved", TargetType: "scheduled_report", TargetID: id,
			Metadata: map[string]any{"kind": kind, "enabled": enabled, "recipients": len(recips)}})
	})
}

type Run struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	PeriodStart string          `json:"period_start"`
	PeriodEnd   string          `json:"period_end"`
	Status      string          `json:"status"`
	Trigger     string          `json:"trigger"`
	Error       *string         `json:"error"`
	StartedAt   time.Time       `json:"started_at"`
	GeneratedAt *time.Time      `json:"generated_at"`
	Deliveries  []RunDelivery   `json:"deliveries"`
	Content     json.RawMessage `json:"content,omitempty"`
}

type RunDelivery struct {
	Email  string  `json:"email"`
	Status string  `json:"status"`
	Reason *string `json:"reason"`
}

// Generate makes the report for one occurrence. A second call for the same
// schedule and period is refused by the database key, so a schedule runs
// once per occurrence however many workers tick.
func (r *Reports) Generate(ctx context.Context, actor, scheduleID, trigger string, from, to time.Time) (*Run, bool, error) {
	var kind, name string
	var recips []string
	if err := r.S.pool.QueryRow(ctx, `SELECT kind, name, coalesce((SELECT array_agg(x::text) FROM unnest(recipients) x), '{}') FROM scheduled_reports WHERE id::text = $1`,
		scheduleID).Scan(&kind, &name, &recips); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, httpx.ErrNotFound
		}
		return nil, false, err
	}
	var runID string
	err := r.S.pool.QueryRow(ctx, `INSERT INTO report_runs (schedule_id, period_start, period_end, trigger, actor_user_id) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (schedule_id, period_start) DO NOTHING RETURNING id::text`, scheduleID, from, to, trigger, nilIfEmpty(actor)).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil // already generated for this occurrence
	}
	if err != nil {
		return nil, false, err
	}
	k := reportKinds[kind]
	var boards []*analytics.Dashboard
	var genErr error
	for _, d := range k.dashboards {
		b, err := r.Dashboards.Dashboard(ctx, d, analytics.Range{From: from, To: to}, analytics.Filters{})
		if err != nil {
			genErr = err
			break
		}
		boards = append(boards, b)
	}
	if genErr != nil {
		msg := genErr.Error()
		_, _ = r.S.pool.Exec(context.WithoutCancel(ctx), `UPDATE report_runs SET status = 'failed', error = $2 WHERE id::text = $1`, runID, clip(msg, 1000))
		return nil, true, genErr
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	for _, b := range boards {
		for _, row := range b.CSV() {
			out := append([]string{b.Kind}, row...)
			for i := range out {
				out[i] = analytics.SafeCSV(out[i])
			}
			_ = cw.Write(out)
		}
	}
	cw.Flush()
	content, _ := json.Marshal(map[string]any{"name": name, "period_start": from.Format("2006-01-02"), "period_end": to.Format("2006-01-02"),
		"generated_at": r.S.Now().UTC(), "dashboards": boards})
	err = db.InTx(ctx, r.S.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE report_runs SET status = 'succeeded', content = $2, csv = $3, generated_at = now() WHERE id::text = $1`,
			runID, content, buf.String()); err != nil {
			return err
		}
		for _, u := range recips {
			// Queued only when an email service exists; otherwise recorded as skipped.
			status, reason := "queued", (*string)(nil)
			if r.Mailer == nil {
				s := "No email service is configured; the report is kept here."
				status, reason = "skipped", &s
			}
			if _, err := tx.Exec(ctx, `INSERT INTO report_run_deliveries (run_id, user_id, status, reason) VALUES ($1::uuid, $2::uuid, $3, $4)`,
				runID, u, status, reason); err != nil {
				return err
			}
			if status == "queued" {
				if err := jobs.Enqueue(ctx, tx, JobReportEmail, map[string]string{"run_id": runID, "user_id": u}); err != nil {
					return err
				}
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "automation.report_generated", TargetType: "report_run", TargetID: runID,
			Metadata: map[string]any{"kind": kind, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02"), "trigger": trigger}})
	})
	if err != nil {
		return nil, true, err
	}
	run, err := r.Run(ctx, runID, false)
	return run, true, err
}

// JobReportEmail emails one recipient a generated report.
const JobReportEmail = "automation.report_email"

// HandleEmail sends the report and records the provider's real outcome.
func (r *Reports) HandleEmail(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		RunID  string `json:"run_id"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("report email: bad payload: %w", err)
	}
	mark := func(status, reason string) error {
		_, err := r.S.pool.Exec(ctx, `UPDATE report_run_deliveries SET status = $3, reason = $4, updated_at = now() WHERE run_id::text = $1 AND user_id::text = $2`,
			p.RunID, p.UserID, status, nilIfEmpty(clip(reason, 300)))
		return err
	}
	var kind, name, csvText string
	var from, to time.Time
	if err := r.S.pool.QueryRow(ctx, `SELECT s.kind, s.name, rr.period_start, rr.period_end, coalesce(rr.csv, '') FROM report_runs rr
		JOIN scheduled_reports s ON s.id = rr.schedule_id WHERE rr.id::text = $1`, p.RunID).Scan(&kind, &name, &from, &to, &csvText); err != nil {
		return err
	}
	email, active, err := r.Contacts.StaffEmail(ctx, p.UserID)
	if err != nil {
		return err
	}
	ok, err := r.Contacts.HasPermission(ctx, p.UserID, reportKinds[kind].permission)
	if err != nil {
		return err
	}
	if !active || !ok {
		return mark("skipped", "The recipient is no longer an active account allowed to see this report.")
	}
	period := from.Format("2 Jan 2006") + " to " + to.Format("2 Jan 2006")
	body := name + " for " + period + ".\n\nThe figures are attached as CSV (aggregates only). Definitions: docs/analytics/METRIC_CATALOG.md.\n"
	msg := mail.Message{To: email, Subject: "Mise " + name + ": " + period, Text: body, HTML: "<p>" + body + "</p><pre>" + htmlEscape(clip(csvText, 20000)) + "</pre>"}
	if err := r.Mailer.Send(ctx, msg); err != nil {
		_ = mark("failed", err.Error())
		return err
	}
	return mark("sent", "")
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func (r *Reports) Run(ctx context.Context, id string, withContent bool) (*Run, error) {
	var x Run
	var from, to time.Time
	var content []byte
	err := r.S.pool.QueryRow(ctx, `SELECT rr.id::text, s.kind, s.name, rr.period_start, rr.period_end, rr.status, rr.trigger, rr.error, rr.started_at, rr.generated_at, rr.content
		FROM report_runs rr JOIN scheduled_reports s ON s.id = rr.schedule_id WHERE rr.id::text = $1`, id).
		Scan(&x.ID, &x.Kind, &x.Name, &from, &to, &x.Status, &x.Trigger, &x.Error, &x.StartedAt, &x.GeneratedAt, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	x.PeriodStart, x.PeriodEnd = from.Format("2006-01-02"), to.Format("2006-01-02")
	if withContent {
		x.Content = content
	}
	rows, err := r.S.pool.Query(ctx, `SELECT u.email, d.status, d.reason FROM report_run_deliveries d JOIN users u ON u.id = d.user_id WHERE d.run_id::text = $1 ORDER BY u.email`, id)
	if err != nil {
		return nil, err
	}
	x.Deliveries, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RunDelivery])
	return &x, err
}

func (r *Reports) Runs(ctx context.Context) ([]Run, error) {
	rows, err := r.S.pool.Query(ctx, `SELECT id::text FROM report_runs ORDER BY started_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := []Run{}
	for _, id := range ids {
		x, err := r.Run(ctx, id, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, nil
}

// CSV returns a generated report's CSV.
func (r *Reports) CSV(ctx context.Context, id string) (string, string, error) {
	var name, csvText string
	var from time.Time
	err := r.S.pool.QueryRow(ctx, `SELECT s.kind, rr.period_start, coalesce(rr.csv, '') FROM report_runs rr JOIN scheduled_reports s ON s.id = rr.schedule_id
		WHERE rr.id::text = $1 AND rr.status = 'succeeded'`, id).Scan(&name, &from, &csvText)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", httpx.ErrNotFound
	}
	return fmt.Sprintf("mise-%s-%s.csv", name, from.Format("2006-01-02")), csvText, err
}

// Due generates every enabled schedule's last complete period (worker tick).
func (r *Reports) Due(ctx context.Context) (int, error) {
	rows, err := r.S.pool.Query(ctx, `SELECT id::text, kind FROM scheduled_reports WHERE enabled`)
	if err != nil {
		return 0, err
	}
	type sk struct{ id, kind string }
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (sk, error) {
		var x sk
		err := row.Scan(&x.id, &x.kind)
		return x, err
	})
	if err != nil {
		return 0, err
	}
	n := 0
	today := r.today()
	for _, s := range list {
		f, t := reportKinds[s.kind].period(today)
		_, made, err := r.Generate(ctx, "", s.id, "schedule", f, t)
		if err != nil {
			return n, err
		}
		if made {
			n++
		}
	}
	return n, nil
}

// RunNow generates a schedule's report for its last complete period on
// request (once per occurrence, like the schedule).
func (r *Reports) RunNow(ctx context.Context, actor, scheduleID string) (*Run, error) {
	var kind string
	if err := r.S.pool.QueryRow(ctx, `SELECT kind FROM scheduled_reports WHERE id::text = $1`, scheduleID).Scan(&kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	f, t := reportKinds[kind].period(r.today())
	run, made, err := r.Generate(ctx, actor, scheduleID, "manual", f, t)
	if err != nil {
		return nil, err
	}
	if !made {
		return nil, conflict("already_generated", "This report was already generated for "+f.Format("2006-01-02")+" to "+t.Format("2006-01-02")+".")
	}
	return run, nil
}
