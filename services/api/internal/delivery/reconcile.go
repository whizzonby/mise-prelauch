package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/httpx"
)

// ---------------------------------------------------------------- costs

type Cost struct {
	ID             string    `json:"id"`
	PartnerID      string    `json:"partner_id"`
	PartnerName    string    `json:"partner_name"`
	DeliveryID     *string   `json:"delivery_id"`
	DeliveryNumber *string   `json:"delivery_number"`
	RunID          *string   `json:"run_id"`
	RunNumber      *string   `json:"run_number"`
	Kind           string    `json:"kind"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	RateReference  string    `json:"rate_reference"`
	Status         string    `json:"status"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *Service) Costs(ctx context.Context, runID string) ([]Cost, error) {
	cond, args := "true", []any{}
	if runID != "" {
		if !validID(runID) {
			return []Cost{}, nil
		}
		cond, args = "(c.run_id = $1 OR c.delivery_id IN (SELECT delivery_id FROM dispatch_stops WHERE run_id = $1))", []any{runID}
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id::text, c.partner_id::text, p.name, c.delivery_id::text, d.number, c.run_id::text, r.number, c.kind,
		c.amount_minor, c.currency, c.rate_reference, c.status, c.notes, c.created_at
		FROM delivery_cost_records c JOIN delivery_partners p ON p.id = c.partner_id LEFT JOIN deliveries d ON d.id = c.delivery_id
		LEFT JOIN dispatch_runs r ON r.id = c.run_id WHERE `+cond+` ORDER BY c.created_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Cost])
}

type CostInput struct {
	PartnerID     string `json:"partner_id"`
	DeliveryID    string `json:"delivery_id"`
	RunID         string `json:"run_id"`
	Kind          string `json:"kind"`
	AmountMinor   *int64 `json:"amount_minor"`
	Currency      string `json:"currency"`
	RateReference string `json:"rate_reference"`
	Notes         string `json:"notes"`
}

// RecordCost records a factual cost: estimated, contracted, actual or an
// adjustment. Nothing is computed from an assumed rate.
func (s *Service) RecordCost(ctx context.Context, actor string, in CostInput) (*Cost, error) {
	in.RateReference, in.Notes = strings.TrimSpace(in.RateReference), strings.TrimSpace(in.Notes)
	if in.Currency == "" {
		in.Currency = "TTD"
	}
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if !validID(in.PartnerID) {
		bad("partner_id", "Choose the partner.")
	}
	if in.DeliveryID == "" && in.RunID == "" {
		bad("delivery_id", "Record the cost against a delivery or a run.")
	}
	for k, v := range map[string]string{"delivery_id": in.DeliveryID, "run_id": in.RunID} {
		if v != "" && !validID(v) {
			bad(k, "Not a valid id.")
		}
	}
	if !slices.Contains([]string{"estimated", "contracted", "actual", "adjustment"}, in.Kind) {
		bad("kind", "Choose estimated, contracted, actual or adjustment.")
	}
	if in.AmountMinor == nil || (*in.AmountMinor < 0 && in.Kind != "adjustment") || *in.AmountMinor > 100_000_000 || *in.AmountMinor < -100_000_000 {
		bad("amount_minor", "Enter the amount in cents.")
	}
	if in.Kind == "contracted" && in.RateReference == "" {
		bad("rate_reference", "A contracted cost needs the rate reference from the signed agreement.")
	}
	if len(in.Currency) != 3 || strings.ToUpper(in.Currency) != in.Currency {
		bad("currency", "Use a currency code like TTD.")
	}
	if runes(in.Notes) > 1000 || runes(in.RateReference) > 200 {
		bad("notes", "A field is too long.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO delivery_cost_records (partner_id, delivery_id, run_id, kind, amount_minor, currency, rate_reference, notes, recorded_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id::text`, in.PartnerID, nilIfEmpty(in.DeliveryID), nilIfEmpty(in.RunID), in.Kind,
			*in.AmountMinor, in.Currency, in.RateReference, in.Notes, nilIfEmpty(actor)).Scan(&id)
		if err != nil {
			if strings.Contains(err.Error(), "violates foreign key") {
				return fieldErr("partner_id", "Unknown partner, delivery or run.")
			}
			return err
		}
		return record(ctx, tx, actor, "delivery.cost_recorded", "delivery_cost", id, map[string]any{"kind": in.Kind, "amount_minor": *in.AmountMinor})
	})
	if err != nil {
		return nil, err
	}
	all, err := s.Costs(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, httpx.ErrNotFound
}

// ---------------------------------------------------------------- reconciliation

// Reconciliation compares what a run planned with what happened.
type Reconciliation struct {
	RunID         string        `json:"run_id"`
	RunNumber     string        `json:"run_number"`
	RunStatus     string        `json:"run_status"`
	Status        string        `json:"status"` // open or reconciled
	Summary       ReconSummary  `json:"summary"`
	Discrepancies []Discrepancy `json:"discrepancies"`
	Explanation   *string       `json:"explanation"`
	ClosedAt      *time.Time    `json:"closed_at"`
}

type ReconSummary struct {
	Planned           int `json:"planned_deliveries"`
	PlannedPackages   int `json:"planned_packages"`
	HandedOver        int `json:"handed_over_packages"`
	Delivered         int `json:"delivered"`
	DeliveredPackages int `json:"delivered_packages"`
	Failed            int `json:"failed"`
	Returned          int `json:"returned"`
	ReturnedPackages  int `json:"returned_packages"`
	StillOut          int `json:"still_out"`
	MissingProof      int `json:"missing_proof"`
	OpenExceptions    int `json:"open_exceptions"`
	ReturnsUnreviewed int `json:"returns_awaiting_review"`
}

type Discrepancy struct {
	Kind       string `json:"kind"`
	DeliveryID string `json:"delivery_id,omitempty"`
	Delivery   string `json:"delivery,omitempty"`
	Detail     string `json:"detail"`
}

func (s *Service) Reconciliation(ctx context.Context, runID string) (*Reconciliation, error) {
	return s.reconcile(ctx, s.pool, runID)
}

func (s *Service) reconcile(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}, runID string) (*Reconciliation, error) {
	if !validID(runID) {
		return nil, httpx.ErrNotFound
	}
	rec := &Reconciliation{RunID: runID, Status: "open", Discrepancies: []Discrepancy{}}
	err := q.QueryRow(ctx, `SELECT r.number, r.status, COALESCE(x.status, 'open'), x.explanation, x.closed_at
		FROM dispatch_runs r LEFT JOIN delivery_reconciliations x ON x.run_id = r.id WHERE r.id = $1`, runID).
		Scan(&rec.RunNumber, &rec.RunStatus, &rec.Status, &rec.Explanation, &rec.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT d.id::text, d.number, s.attempt, d.attempt, d.status, d.package_count,
		  (SELECT count(*) FROM delivery_handoff_packages hp JOIN delivery_handoffs h ON h.id = hp.handoff_id
		    WHERE h.run_id = s.run_id AND hp.delivery_id = d.id AND hp.attempt = s.attempt AND hp.outcome = 'accepted')::int,
		  EXISTS (SELECT 1 FROM delivery_proofs p WHERE p.delivery_id = d.id AND p.attempt = s.attempt AND p.status = 'accepted'),
		  (SELECT count(*) FROM delivery_tracking_events t WHERE t.delivery_id = d.id AND t.attempt = s.attempt AND t.to_status = 'delivered')::int,
		  (SELECT count(*) FROM delivery_tracking_events t WHERE t.delivery_id = d.id AND t.attempt = s.attempt AND t.to_status = 'delivery_failed')::int,
		  (SELECT count(*) FROM delivery_returns rt WHERE rt.delivery_id = d.id AND rt.attempt = s.attempt)::int,
		  (SELECT count(*) FROM delivery_return_packages rp JOIN delivery_returns rt ON rt.id = rp.return_id WHERE rt.delivery_id = d.id AND rt.attempt = s.attempt)::int,
		  (SELECT count(*) FROM delivery_returns rt WHERE rt.delivery_id = d.id AND rt.attempt = s.attempt AND rt.status = 'awaiting_review')::int,
		  (SELECT count(*) FROM delivery_exceptions e WHERE e.delivery_id = d.id AND e.status IN ('open', 'in_progress'))::int
		FROM dispatch_stops s JOIN deliveries d ON d.id = s.delivery_id
		WHERE s.run_id = $1 AND s.status IN ('active', 'done') ORDER BY s.sequence`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sm := &rec.Summary
	for rows.Next() {
		var id, number, status string
		var stopAttempt, attempt int16
		var pkgs int16
		var handed, delivered, failed, returned, returnedPkgs, unreviewed, open int
		var proof bool
		if err := rows.Scan(&id, &number, &stopAttempt, &attempt, &status, &pkgs, &handed, &proof, &delivered, &failed, &returned, &returnedPkgs,
			&unreviewed, &open); err != nil {
			return nil, err
		}
		sm.Planned++
		sm.PlannedPackages += int(pkgs)
		sm.HandedOver += handed
		sm.ReturnedPackages += returnedPkgs
		sm.OpenExceptions += open
		sm.ReturnsUnreviewed += unreviewed
		add := func(kind, detail string) {
			rec.Discrepancies = append(rec.Discrepancies, Discrepancy{Kind: kind, DeliveryID: id, Delivery: number, Detail: detail})
		}
		current := stopAttempt == attempt
		switch {
		case delivered > 0 && (!current || status == "delivered"):
			sm.Delivered++
			sm.DeliveredPackages += handed
			if !proof {
				sm.MissingProof++
				add("missing_proof", "Delivered without accepted proof.")
			}
		case returned > 0:
			sm.Returned++
		case failed > 0:
			sm.Failed++
			if current && status == "delivery_failed" {
				add("failed_unresolved", "Failed with no return or new attempt recorded.")
			}
		case status == "cancelled":
		default:
			sm.StillOut++
			add("still_out", "Still "+Human(status)+".")
		}
		if handed < int(pkgs) && status != "cancelled" {
			add("not_handed_over", itoa(int(pkgs)-handed)+" package(s) never accepted into custody.")
		}
		if open > 0 {
			add("open_exception", itoa(open)+" open exception(s).")
		}
		if unreviewed > 0 {
			add("return_unreviewed", "Returned packages are still awaiting review.")
		}
	}
	return rec, rows.Err()
}

// CloseRun reconciles a run. It is refused while any discrepancy remains
// unexplained: each must first be resolved where it lives (an exception, a
// return review, an outcome).
func (s *Service) CloseRun(ctx context.Context, actor, runID, explanation string) (*Reconciliation, error) {
	explanation = strings.TrimSpace(explanation)
	if runes(explanation) > 2000 {
		return nil, fieldErr("explanation", "Keep the note under 2,000 characters.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		run, err := s.lockRun(ctx, tx, runID, "released", "completed")
		if err != nil {
			return err
		}
		rec, err := s.reconcile(ctx, tx, runID)
		if err != nil {
			return err
		}
		if len(rec.Discrepancies) > 0 {
			parts := []string{}
			for _, d := range rec.Discrepancies {
				parts = append(parts, strings.TrimSpace(d.Delivery+" "+d.Detail))
			}
			return &httpx.Error{Status: 422, Code: "discrepancies", Message: "Resolve before reconciling: " + strings.Join(parts, " ")}
		}
		sum, _ := json.Marshal(rec.Summary)
		if _, err := tx.Exec(ctx, `INSERT INTO delivery_reconciliations (run_id, status, summary, discrepancies, explanation, closed_by, closed_at)
			VALUES ($1, 'reconciled', $2, '[]', $3, $4, now())
			ON CONFLICT (run_id) DO UPDATE SET status = 'reconciled', summary = EXCLUDED.summary, discrepancies = '[]',
				explanation = EXCLUDED.explanation, closed_by = EXCLUDED.closed_by, closed_at = now(), updated_at = now()`,
			runID, sum, nilIfEmpty(explanation), nilIfEmpty(actor)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dispatch_runs SET status = 'reconciled', completed_at = COALESCE(completed_at, now()), version = version + 1,
			updated_at = now() WHERE id = $1`, runID); err != nil {
			return err
		}
		if err := event(ctx, tx, "delivery.reconciled", "dispatch_run", runID, "", map[string]any{"summary": rec.Summary}); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.run_reconciled", "dispatch_run", runID, map[string]any{"run": run.Number})
	})
	if err != nil {
		return nil, err
	}
	return s.Reconciliation(ctx, runID)
}

// ---------------------------------------------------------------- overview

type Overview struct {
	Date               string   `json:"date"`
	DueToday           int      `json:"due_today"`
	AwaitingVerify     int      `json:"awaiting_verification"`
	ReadyForDispatch   int      `json:"ready_for_dispatch"`
	Unscheduled        int      `json:"unscheduled"`
	Unassigned         int      `json:"unassigned"`
	InTransit          int      `json:"in_transit"`
	DeliveredToday     int      `json:"delivered_today"`
	FailedOpen         int      `json:"failed_open"`
	Delayed            int      `json:"delayed"`
	MissingProof       int      `json:"missing_proof"`
	OpenExceptions     int      `json:"open_exceptions"`
	ReturnsToReview    int      `json:"returns_to_review"`
	RescheduleRequests int      `json:"reschedule_requests"`
	Blockers           []string `json:"blockers"`
	ConsumerFailed     int      `json:"consumer_failed_jobs"`
}

// OverviewFor counts real records for a date. "Delayed": a delivery whose
// confirmed service date has passed without an outcome.
func (s *Service) OverviewFor(ctx context.Context, date string) (*Overview, error) {
	d := s.today()
	if date != "" {
		t, ok := parseDay(date)
		if !ok {
			return nil, fieldErr("date", "Use a date like 2026-11-19.")
		}
		d = t
	}
	o := &Overview{Date: d.Format(day)}
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM deliveries WHERE service_date = $1 AND status <> 'cancelled'),
		(SELECT count(*) FROM fulfillment_obligations WHERE status IN ('ready_for_verification', 'verification_in_progress')),
		(SELECT count(*) FROM fulfillment_obligations WHERE status = 'verified'),
		(SELECT count(*) FROM deliveries WHERE status = 'pending_assignment' AND schedule_confirmed_at IS NULL),
		(SELECT count(*) FROM deliveries WHERE status = 'pending_assignment' AND schedule_confirmed_at IS NOT NULL),
		(SELECT count(*) FROM deliveries WHERE status IN ('picked_up', 'in_transit', 'arriving')),
		(SELECT count(*) FROM deliveries WHERE status = 'delivered' AND (delivered_at AT TIME ZONE $2)::date = $1),
		(SELECT count(*) FROM deliveries WHERE status = 'delivery_failed'),
		(SELECT count(*) FROM deliveries WHERE service_date < $1 AND schedule_confirmed_at IS NOT NULL
			AND status IN ('pending_assignment', 'assigned', 'ready_for_pickup', 'picked_up', 'in_transit', 'arriving')),
		(SELECT count(*) FROM deliveries d WHERE d.status = 'delivered' AND NOT EXISTS (SELECT 1 FROM delivery_proofs p
			WHERE p.delivery_id = d.id AND p.attempt = d.attempt AND p.status = 'accepted')),
		(SELECT count(*) FROM delivery_exceptions WHERE status IN ('open', 'in_progress')),
		(SELECT count(*) FROM delivery_returns WHERE status = 'awaiting_review'),
		(SELECT count(*) FROM delivery_reschedule_requests WHERE status = 'requested'),
		(SELECT count(*) FROM jobs WHERE kind = 'fulfillment.batch_ready' AND status = 'failed')`, d, s.Location.String()).
		Scan(&o.DueToday, &o.AwaitingVerify, &o.ReadyForDispatch, &o.Unscheduled, &o.Unassigned, &o.InTransit, &o.DeliveredToday, &o.FailedOpen,
			&o.Delayed, &o.MissingProof, &o.OpenExceptions, &o.ReturnsToReview, &o.RescheduleRequests, &o.ConsumerFailed)
	if err != nil {
		return nil, err
	}
	st, err := s.settings(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	o.Blockers = st.Blockers
	var zones, days, partners int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM delivery_service_zones WHERE status = 'active'),
		(SELECT count(*) FROM delivery_service_days WHERE status = 'open' AND service_date >= $1),
		(SELECT count(*) FROM delivery_partners WHERE status = 'approved' AND NOT demo)`, d).Scan(&zones, &days, &partners); err != nil {
		return nil, err
	}
	if zones == 0 {
		o.Blockers = append([]string{"No active delivery zones: deliveries cannot be scheduled."}, o.Blockers...)
	}
	if days == 0 {
		o.Blockers = append([]string{"No upcoming approved delivery dates."}, o.Blockers...)
	}
	if partners == 0 {
		o.Blockers = append([]string{"No approved delivery partner with a real agreement."}, o.Blockers...)
	}
	return o, nil
}

// ---------------------------------------------------------------- report

// Report is the delivery performance report for deliveries scheduled in a
// date range. Every figure is counted from records, with its definition.
type Report struct {
	From        string            `json:"from"`
	To          string            `json:"to"`
	TimeZone    string            `json:"time_zone"`
	Totals      map[string]any    `json:"totals"`
	ByPartner   []Breakdown       `json:"by_partner"`
	ByZone      []Breakdown       `json:"by_zone"`
	Costs       []CostTotal       `json:"costs"`
	Definitions map[string]string `json:"definitions"`
}

type Breakdown struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Deliveries   int      `json:"deliveries"`
	Delivered    int      `json:"delivered"`
	FirstAttempt int      `json:"first_attempt_delivered"`
	Failed       int      `json:"failed_attempts"`
	OnTimeKnown  int      `json:"on_time_measurable"`
	OnTime       int      `json:"on_time"`
	OnTimeRate   *float64 `json:"on_time_rate"`
}

type CostTotal struct {
	Kind        string `json:"kind"`
	Currency    string `json:"currency"`
	AmountMinor int64  `json:"amount_minor"`
	Records     int    `json:"records"`
}

func (s *Service) Report(ctx context.Context, from, to string) (*Report, error) {
	f, ok1 := parseDay(from)
	t, ok2 := parseDay(to)
	if !ok1 || !ok2 || t.Before(f) || t.Sub(f) > 731*24*time.Hour {
		return nil, fieldErr("from", "Choose a range of 1 day to 2 years, with from before to (YYYY-MM-DD).")
	}
	tz := s.Location.String()
	rep := &Report{From: from, To: to, TimeZone: tz, Totals: map[string]any{}, Definitions: map[string]string{
		"deliveries":              "Deliveries with a confirmed service date in the range (cancelled excluded).",
		"delivered":               "Of those, deliveries completed with accepted proof.",
		"first_attempt_delivered": "Delivered on attempt 1.",
		"failed_attempts":         "Recorded failed attempts (a delivery can fail more than once).",
		"reattempts":              "Deliveries that needed more than one attempt.",
		"returned":                "Deliveries whose packages came back to Mise.",
		"on_time":                 "Delivered on the confirmed date and, where an approved window was promised, inside it. Deliveries without a confirmed date are not measurable and are not counted as on time.",
		"proof_completeness":      "Delivered deliveries that have accepted proof (should always be all of them).",
		"unresolved_exceptions":   "Exceptions on these deliveries still open or in progress.",
		"costs":                   "Recorded partner cost records by kind; estimated, contracted and actual are never mixed.",
	}}
	base := `FROM deliveries d LEFT JOIN delivery_windows w ON w.id = d.window_id
		WHERE d.schedule_confirmed_at IS NOT NULL AND d.service_date BETWEEN $1 AND $2 AND d.status <> 'cancelled'`
	var deliveries, delivered, first, reattempts, returned, failedAttempts, measurable, onTime, withProof, openExc int
	if err := s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE d.status = 'delivered'),
		count(*) FILTER (WHERE d.status = 'delivered' AND d.attempt = 1), count(*) FILTER (WHERE d.attempt > 1),
		count(*) FILTER (WHERE d.status = 'returned'),
		COALESCE(sum((SELECT count(*) FROM delivery_tracking_events t WHERE t.delivery_id = d.id AND t.to_status = 'delivery_failed')), 0)::int,
		count(*) FILTER (WHERE d.status = 'delivered'),
		count(*) FILTER (WHERE d.status = 'delivered' AND (d.delivered_at AT TIME ZONE $3)::date = d.service_date
			AND (w.id IS NULL OR (d.delivered_at AT TIME ZONE $3)::time BETWEEN w.starts_at AND w.ends_at)),
		count(*) FILTER (WHERE d.status = 'delivered' AND EXISTS (SELECT 1 FROM delivery_proofs p WHERE p.delivery_id = d.id
			AND p.attempt = d.attempt AND p.status = 'accepted')),
		COALESCE(sum((SELECT count(*) FROM delivery_exceptions e WHERE e.delivery_id = d.id AND e.status IN ('open', 'in_progress'))), 0)::int `+base, f, t, tz).
		Scan(&deliveries, &delivered, &first, &reattempts, &returned, &failedAttempts, &measurable, &onTime, &withProof, &openExc); err != nil {
		return nil, err
	}
	rate := func(n, d int) any {
		if d == 0 {
			return nil
		}
		return float64(n) / float64(d)
	}
	rep.Totals = map[string]any{"deliveries": deliveries, "delivered": delivered, "first_attempt_delivered": first,
		"first_attempt_rate": rate(first, deliveries), "failed_attempts": failedAttempts, "reattempts": reattempts, "returned": returned,
		"on_time_measurable": measurable, "on_time": onTime, "on_time_rate": rate(onTime, measurable),
		"proof_completeness": rate(withProof, delivered), "unresolved_exceptions": openExc}
	breakdown := func(keyCol, labelCol, join string) ([]Breakdown, error) {
		rows, err := s.pool.Query(ctx, `SELECT `+keyCol+`, `+labelCol+`, count(*), count(*) FILTER (WHERE d.status = 'delivered'),
			count(*) FILTER (WHERE d.status = 'delivered' AND d.attempt = 1),
			COALESCE(sum((SELECT count(*) FROM delivery_tracking_events t WHERE t.delivery_id = d.id AND t.to_status = 'delivery_failed')), 0)::int,
			count(*) FILTER (WHERE d.status = 'delivered'),
			count(*) FILTER (WHERE d.status = 'delivered' AND (d.delivered_at AT TIME ZONE $3)::date = d.service_date
				AND (w.id IS NULL OR (d.delivered_at AT TIME ZONE $3)::time BETWEEN w.starts_at AND w.ends_at)), NULL::float8 `+
			strings.Replace(base, "WHERE", join+" WHERE", 1)+` GROUP BY 1, 2 ORDER BY 2`, f, t, tz)
		if err != nil {
			return nil, err
		}
		out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Breakdown])
		for i := range out {
			if out[i].OnTimeKnown > 0 {
				r := float64(out[i].OnTime) / float64(out[i].OnTimeKnown)
				out[i].OnTimeRate = &r
			}
		}
		return out, err
	}
	var err error
	if rep.ByZone, err = breakdown(`COALESCE(z.code, 'none')`, `COALESCE(z.name, 'No zone')`, `LEFT JOIN delivery_service_zones z ON z.id = d.zone_id`); err != nil {
		return nil, err
	}
	if rep.ByPartner, err = breakdown(`COALESCE(p.id::text, 'none')`, `COALESCE(p.name, 'Not assigned')`,
		`LEFT JOIN LATERAL (SELECT r.partner_id FROM dispatch_stops s JOIN dispatch_runs r ON r.id = s.run_id WHERE s.delivery_id = d.id
			AND s.status IN ('active', 'done') ORDER BY s.added_at DESC LIMIT 1) lp ON true LEFT JOIN delivery_partners p ON p.id = lp.partner_id`); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT c.kind, c.currency, sum(c.amount_minor)::bigint, count(*)::int FROM delivery_cost_records c
		LEFT JOIN deliveries d ON d.id = c.delivery_id LEFT JOIN dispatch_runs r ON r.id = c.run_id
		WHERE COALESCE(d.service_date, r.service_date) BETWEEN $1 AND $2 GROUP BY 1, 2 ORDER BY 1, 2`, f, t)
	if err != nil {
		return nil, err
	}
	rep.Costs, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CostTotal])
	return rep, err
}
