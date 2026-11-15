package delivery

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// seen reports whether a tracking idempotency key was already used: a
// repeated update (a retried tap, a partner's duplicate message) answers
// with the current state instead of failing or recording twice.
func seen(ctx context.Context, q db.Querier, deliveryID, key string) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM delivery_tracking_events WHERE delivery_id = $1 AND idempotency_key = $2`, deliveryID, key).Scan(&n)
	return n > 0, err
}

func cleanKey(k string) (string, error) {
	k = strings.TrimSpace(k)
	if k == "" || len(k) > 100 {
		return "", fieldErr("idempotency_key", "Missing request key.")
	}
	return "req:" + k, nil
}

// StatusInput records a progress update: in transit or arriving. Source
// says who reported it; a location is kept only when marked verified.
// This is milestone tracking, not live GPS.
type StatusInput struct {
	Status           string     `json:"status"` // in_transit, arriving, or "note" for a note without a status change
	Source           string     `json:"source"` // staff or partner
	OccurredAt       *time.Time `json:"occurred_at"`
	Location         string     `json:"location"`
	LocationVerified bool       `json:"location_verified"`
	Notes            string     `json:"notes"`
	PartnerReference string     `json:"partner_reference"`
	IdempotencyKey   string     `json:"idempotency_key"`
	Version          *int       `json:"version"`
}

func (s *Service) occurred(at *time.Time) (time.Time, error) {
	if at == nil {
		return s.Now(), nil
	}
	if at.After(s.Now().Add(5 * time.Minute)) {
		return time.Time{}, fieldErr("occurred_at", "An update cannot be in the future.")
	}
	return *at, nil
}

func (s *Service) UpdateStatus(ctx context.Context, sc Scope, actor, id string, in StatusInput) (*Delivery, error) {
	key, err := cleanKey(in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if in.Source == "" {
		in.Source = "staff"
	}
	if in.Source != "staff" && in.Source != "partner" {
		return nil, fieldErr("source", "Choose staff or partner.")
	}
	if !slices.Contains([]string{"in_transit", "arriving", "note"}, in.Status) {
		return nil, fieldErr("status", "Record in transit, arriving or a note. Use the delivery or failure actions for outcomes.")
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if runes(in.Notes) > 1000 || runes(in.Location) > 200 || runes(in.PartnerReference) > 120 {
		return nil, fieldErr("notes", "A field is too long.")
	}
	if in.Status == "note" && in.Notes == "" {
		return nil, fieldErr("notes", "Write the note.")
	}
	at, err := s.occurred(in.OccurredAt)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lockScoped(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if dup, err := seen(ctx, tx, id, key); err != nil || dup {
			return err
		}
		if err := expect(r, in.Version); err != nil {
			return err
		}
		partner := s.runPartner(ctx, tx, id)
		details := map[string]any{}
		if in.PartnerReference != "" {
			details["partner_reference"] = in.PartnerReference
		}
		t := trackInput{Source: in.Source, Actor: actor, PartnerID: partner, OccurredAt: at, Location: in.Location,
			Verified: in.LocationVerified, Notes: in.Notes, Details: details, Key: key}
		if in.Status == "note" {
			t.DeliveryID, t.Type, t.Attempt = id, "note", r.Attempt
			return s.track(ctx, tx, t)
		}
		if err := s.move(ctx, tx, r, in.Status, t, ""); err != nil {
			return err
		}
		if in.Status == "in_transit" {
			if err := event(ctx, tx, "delivery.in_transit", "delivery", id, itoa(r.Attempt), map[string]any{"source": in.Source}); err != nil {
				return err
			}
		}
		return record(ctx, tx, actor, "delivery.status_recorded", "delivery", id, map[string]any{"status": in.Status, "source": in.Source})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, sc, id)
}

// lockScoped locks a delivery the principal may act on. Outside the scope
// it answers not found.
func (s *Service) lockScoped(ctx context.Context, tx pgx.Tx, sc Scope, id string) (*row, error) {
	r, err := s.lock(ctx, tx, id)
	if err != nil || sc.All {
		return r, err
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dispatch_stops s JOIN dispatch_runs r ON r.id = s.run_id
		WHERE s.delivery_id = $1 AND s.status IN ('active', 'done') AND r.courier_id = $2)`, id, sc.CourierID).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.ErrNotFound
	}
	return r, nil
}

func (s *Service) runPartner(ctx context.Context, q db.Querier, deliveryID string) string {
	var p string
	_ = q.QueryRow(ctx, `SELECT r.partner_id::text FROM dispatch_stops s JOIN dispatch_runs r ON r.id = s.run_id
		WHERE s.delivery_id = $1 AND s.status IN ('active', 'done') ORDER BY s.added_at DESC LIMIT 1`, deliveryID).Scan(&p)
	return p
}

// ---------------------------------------------------------------- proof

type Proof struct {
	ID               string    `json:"id"`
	Attempt          int       `json:"attempt"`
	Method           string    `json:"method"`
	Status           string    `json:"status"`
	RecipientName    *string   `json:"recipient_name"`
	PartnerReference *string   `json:"partner_reference"`
	Notes            *string   `json:"notes"`
	RejectionReason  *string   `json:"rejection_reason"`
	RecordedBy       *string   `json:"recorded_by"`
	CreatedAt        time.Time `json:"created_at"`
}

func (s *Service) proofs(ctx context.Context, id string) ([]Proof, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.id::text, p.attempt::int, p.method, p.status, p.recipient_name, p.partner_reference, p.notes,
		p.rejection_reason, NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), p.created_at
		FROM delivery_proofs p LEFT JOIN users u ON u.id = p.recorded_by WHERE p.delivery_id = $1 ORDER BY p.created_at`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Proof])
}

type ProofInput struct {
	Method           string `json:"method"`
	Code             string `json:"code"`
	RecipientName    string `json:"recipient_name"`
	PartnerReference string `json:"partner_reference"`
	Notes            string `json:"notes"`
	IdempotencyKey   string `json:"idempotency_key"`
}

// RecordProof records proof for the current attempt. It is accepted only if
// the method is in the approved policy and its own check passes (a code
// must match this delivery). A wrong code is kept as a rejected attempt.
// Submitting the same proof twice returns the first.
func (s *Service) RecordProof(ctx context.Context, sc Scope, actor, id string, in ProofInput) (*Delivery, error) {
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.IdempotencyKey == "" || len(in.IdempotencyKey) > 100 {
		return nil, fieldErr("idempotency_key", "Missing request key.")
	}
	in.RecipientName, in.PartnerReference, in.Notes = strings.TrimSpace(in.RecipientName), strings.TrimSpace(in.PartnerReference), strings.TrimSpace(in.Notes)
	if runes(in.RecipientName) > 120 || runes(in.PartnerReference) > 120 || runes(in.Notes) > 1000 {
		return nil, fieldErr("notes", "A field is too long.")
	}
	if in.Method == "photo" {
		return nil, unprocessable("photo_unavailable", "Photo proof is not available: there is no secure evidence storage. Use another approved method.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lockScoped(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		var dup int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_proofs WHERE delivery_id = $1 AND idempotency_key = $2`, id, in.IdempotencyKey).Scan(&dup); err != nil {
			return err
		}
		if dup > 0 {
			return nil
		}
		if !slices.Contains([]string{"picked_up", "in_transit", "arriving"}, r.Status) {
			return conflict("invalid_transition", "Proof can be recorded only while a delivery is out; this one is "+Human(r.Status)+".")
		}
		st, err := s.settings(ctx, tx)
		if err != nil {
			return err
		}
		if len(st.ProofMethods) == 0 {
			return unprocessable("proof_policy_missing", "No proof-of-delivery policy is configured, so no delivery can be completed yet.")
		}
		if !slices.Contains(st.ProofMethods, in.Method) {
			return fieldErr("method", "Use one of the approved methods: "+strings.Join(st.ProofMethods, ", ")+".")
		}
		status, rejection := "accepted", ""
		switch in.Method {
		case "confirmation_code":
			if !s.checkCode(id, r.Attempt, in.Code) {
				status, rejection = "rejected", "The confirmation code does not match this delivery."
			}
		case "recipient_acknowledgment":
			if runes(in.RecipientName) < 2 {
				return fieldErr("recipient_name", "Name the person who received the package.")
			}
		case "partner_confirmation":
			if in.PartnerReference == "" {
				return fieldErr("partner_reference", "Give the partner's confirmation reference.")
			}
		case "staff_confirmation":
			if runes(in.Notes) < 5 {
				return fieldErr("notes", "Say how you confirmed the delivery.")
			}
		}
		if status == "accepted" {
			var already int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_proofs WHERE delivery_id = $1 AND attempt = $2 AND status = 'accepted'`, id, r.Attempt).Scan(&already); err != nil {
				return err
			}
			if already > 0 {
				return nil // proof is already in place; a second one adds nothing
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO delivery_proofs (delivery_id, attempt, method, status, recipient_name, partner_reference, notes,
			rejection_reason, recorded_by, idempotency_key) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, r.Attempt, in.Method, status, nilIfEmpty(in.RecipientName), nilIfEmpty(in.PartnerReference), nilIfEmpty(in.Notes),
			nilIfEmpty(rejection), nilIfEmpty(actor), in.IdempotencyKey); err != nil {
			if db.IsUniqueViolation(err, "delivery_proofs_accepted_once") || db.IsUniqueViolation(err, "delivery_proofs_idem") {
				return nil
			}
			return err
		}
		if err := s.track(ctx, tx, trackInput{DeliveryID: id, Type: "proof_" + status, Attempt: r.Attempt, Source: "staff", Actor: actor,
			Details: map[string]any{"method": in.Method}, Notes: rejection, Key: "proof:" + in.IdempotencyKey}); err != nil && !errors.Is(err, errDuplicate) {
			return err
		}
		return record(ctx, tx, actor, "delivery.proof_recorded", "delivery", id, map[string]any{"method": in.Method, "status": status})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, sc, id)
}

type OutcomeInput struct {
	Reason         string     `json:"reason"` // failure reason key
	Notes          string     `json:"notes"`
	Source         string     `json:"source"`
	OccurredAt     *time.Time `json:"occurred_at"`
	IdempotencyKey string     `json:"idempotency_key"`
	Version        *int       `json:"version"`
}

// Deliver completes a delivery. It needs accepted proof for the current
// attempt: a "delivered" status alone never completes anything.
func (s *Service) Deliver(ctx context.Context, sc Scope, actor, id string, in OutcomeInput) (*Delivery, error) {
	key, err := cleanKey(in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	at, err := s.occurred(in.OccurredAt)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lockScoped(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if dup, err := seen(ctx, tx, id, key); err != nil || dup {
			return err
		}
		if err := expect(r, in.Version); err != nil {
			return err
		}
		if !CanMove(r.Status, "delivered") {
			return conflict("invalid_transition", "A delivery that is "+Human(r.Status)+" cannot be completed.")
		}
		var method string
		err = tx.QueryRow(ctx, `SELECT method FROM delivery_proofs WHERE delivery_id = $1 AND attempt = $2 AND status = 'accepted'`, id, r.Attempt).Scan(&method)
		if errors.Is(err, pgx.ErrNoRows) {
			return unprocessable("proof_required", "Record accepted proof of delivery first.")
		}
		if err != nil {
			return err
		}
		if err := s.move(ctx, tx, r, "delivered", trackInput{Source: orDefault(in.Source, "staff"), Actor: actor, PartnerID: s.runPartner(ctx, tx, id),
			OccurredAt: at, Notes: strings.TrimSpace(in.Notes), Details: map[string]any{"proof_method": method}, Key: key},
			`, delivered_at = $3`, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dispatch_stops SET status = 'done' WHERE delivery_id = $1 AND status = 'active'`, id); err != nil {
			return err
		}
		if err := s.Fulfillment.Fulfilled(ctx, tx, r.ObligationID, actor); err != nil {
			return err
		}
		if _, err := s.Orders.Advance(ctx, tx, []string{r.OrderID}, []string{"ready_for_fulfillment", "out_for_delivery"}, "delivered",
			"Delivered with "+Human(method), actor); err != nil {
			return err
		}
		if err := event(ctx, tx, "delivery.delivered", "delivery", id, itoa(r.Attempt), map[string]any{"proof_method": method}); err != nil {
			return err
		}
		if err := s.notify(ctx, tx, r, "delivered", itoa(r.Attempt)); err != nil {
			return err
		}
		if err := s.maybeCompleteRun(ctx, tx, id); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.delivered", "delivery", id, map[string]any{"proof_method": method})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, sc, id)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// Fail records a failed attempt with a configured reason. It never assumes
// fault, and it always raises an exception for someone to decide the next
// action (reattempt, reschedule, return, escalate or refund review).
func (s *Service) Fail(ctx context.Context, sc Scope, actor, id string, in OutcomeInput) (*Delivery, error) {
	key, err := cleanKey(in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	at, err := s.occurred(in.OccurredAt)
	if err != nil {
		return nil, err
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if runes(in.Notes) > 1000 {
		return nil, fieldErr("notes", "Keep notes under 1,000 characters.")
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lockScoped(ctx, tx, sc, id)
		if err != nil {
			return err
		}
		if dup, err := seen(ctx, tx, id, key); err != nil || dup {
			return err
		}
		if err := expect(r, in.Version); err != nil {
			return err
		}
		st, err := s.settings(ctx, tx)
		if err != nil {
			return err
		}
		if !slices.Contains(st.FailureReasons, in.Reason) {
			return fieldErr("reason", "Choose a failure reason from the list.")
		}
		src := orDefault(in.Source, "staff")
		if src != "staff" && src != "partner" {
			return fieldErr("source", "Choose staff or partner.")
		}
		if err := s.move(ctx, tx, r, "delivery_failed", trackInput{Source: src, Actor: actor, PartnerID: s.runPartner(ctx, tx, id), OccurredAt: at,
			Notes: in.Notes, Details: map[string]any{"reason": in.Reason}, Key: key}, `, failed_at = $3`, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE dispatch_stops SET status = 'done' WHERE delivery_id = $1 AND status = 'active'`, id); err != nil {
			return err
		}
		if _, err := s.RaiseException(ctx, tx, ExceptionInput{Stage: "delivery", Type: "delivery_failure", Severity: "high",
			Summary: "Delivery " + r.Number + " failed: " + Human(in.Reason), Notes: in.Notes, DeliveryID: id, NextAction: "none",
			DedupeKey: "failed:" + id + ":" + itoa(r.Attempt), Actor: actor}); err != nil {
			return err
		}
		if err := event(ctx, tx, "delivery.failed", "delivery", id, itoa(r.Attempt), map[string]any{"reason": in.Reason}); err != nil {
			return err
		}
		if err := s.notify(ctx, tx, r, "failed", itoa(r.Attempt)); err != nil {
			return err
		}
		if err := s.maybeCompleteRun(ctx, tx, id); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.failed", "delivery", id, map[string]any{"reason": in.Reason})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, sc, id)
}

// maybeCompleteRun marks a released run completed once none of its
// deliveries is still out.
func (s *Service) maybeCompleteRun(ctx context.Context, tx pgx.Tx, deliveryID string) error {
	_, err := tx.Exec(ctx, `UPDATE dispatch_runs r SET status = 'completed', completed_at = now(), version = version + 1, updated_at = now()
		WHERE r.status = 'released' AND r.id IN (SELECT run_id FROM dispatch_stops WHERE delivery_id = $1)
		AND NOT EXISTS (SELECT 1 FROM dispatch_stops s WHERE s.run_id = r.id AND s.status = 'active')`, deliveryID)
	return err
}

// Reattempt sends a failed delivery back to planning as a new attempt. Its
// packages need a new handoff; the failure stays in the history.
func (s *Service) Reattempt(ctx context.Context, actor, id, why string) (*Delivery, error) {
	why, err := reason(why, 3, 500)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		return s.reattempt(ctx, tx, actor, r, why, nil, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, id)
}

func (s *Service) reattempt(ctx context.Context, tx pgx.Tx, actor string, r *row, why string, date *time.Time, window *string) error {
	if r.Status != "delivery_failed" {
		return conflict("invalid_transition", "Only a failed delivery can be attempted again.")
	}
	prev := r.Attempt
	if err := s.move(ctx, tx, r, "pending_assignment", trackInput{Type: "reattempt", Source: "staff", Actor: actor, Notes: why,
		Details: map[string]any{"previous_attempt": prev}, Key: "reattempt:" + itoa(prev)},
		`, attempt = attempt + 1, service_date = $3, window_id = $4, schedule_confirmed_at = CASE WHEN $3::date IS NULL THEN NULL ELSE now() END`,
		date, window); err != nil {
		return err
	}
	r.Attempt++
	pkgs, err := s.Fulfillment.Packages(ctx, tx, r.ObligationID)
	if err != nil {
		return err
	}
	ids := make([]string, len(pkgs))
	for i, p := range pkgs {
		ids[i] = p.ID
	}
	return s.Fulfillment.SetPackageHandoff(ctx, tx, ids, "pending")
}

// Cancel stops a delivery that has not been picked up. The order and its
// payment are not touched: refunds go through commerce.
func (s *Service) Cancel(ctx context.Context, actor, id, why string) (*Delivery, error) {
	why, err := reason(why, 5, 500)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if !CanMove(r.Status, "cancelled") {
			return conflict("invalid_transition", "A delivery that is "+Human(r.Status)+" cannot be cancelled.")
		}
		var runID *string
		_ = tx.QueryRow(ctx, `SELECT run_id::text FROM dispatch_stops WHERE delivery_id = $1 AND status = 'active'`, id).Scan(&runID)
		if runID != nil {
			// Lock the run so a manifest cannot be written while the stop goes.
			run, err := s.lockRun(ctx, tx, *runID)
			if err != nil {
				return err
			}
			if run.Status == "finalized" {
				if _, err := tx.Exec(ctx, `UPDATE dispatch_manifests SET status = 'superseded' WHERE run_id = $1 AND status = 'current'`, run.ID); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE dispatch_runs SET status = 'draft', finalized_at = NULL WHERE id = $1`, run.ID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE dispatch_stops SET status = 'removed', removed_by = $2, removed_at = now(), removal_reason = $3
				WHERE delivery_id = $1 AND status = 'active'`, id, nilIfEmpty(actor), "Cancelled: "+why); err != nil {
				return err
			}
			if err := s.bumpRun(ctx, tx, run.ID); err != nil {
				return err
			}
		}
		if err := s.move(ctx, tx, r, "cancelled", trackInput{Source: "staff", Actor: actor, Notes: why, Key: "cancelled"}, `, cancelled_at = now()`); err != nil {
			return err
		}
		if err := s.Fulfillment.Cancel(ctx, tx, r.ObligationID, actor, "Delivery cancelled: "+why); err != nil {
			return err
		}
		if _, err := s.RaiseException(ctx, tx, ExceptionInput{Stage: "delivery", Type: "other", Severity: "medium",
			Summary: "Delivery " + r.Number + " cancelled", Notes: why + " The order and payment are unchanged; review a refund in payments if one is due.",
			DeliveryID: id, NextAction: "refund_review", DedupeKey: "cancelled:" + id, Actor: actor}); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.cancelled", "delivery", id, map[string]any{"reason": why})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, id)
}

// Correct fixes a recorded outcome that was wrong. It is the only way a
// delivered delivery can become undelivered (or a failure be withdrawn),
// and it is explicit, permissioned, reasoned and in the history.
func (s *Service) Correct(ctx context.Context, actor, id, to, why string) (*Delivery, error) {
	why, err := reason(why, 10, 1000)
	if err != nil {
		return nil, err
	}
	allowed := map[string]string{"delivered": "delivery_failed", "delivery_failed": "in_transit"}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if allowed[r.Status] != to {
			return conflict("invalid_correction", "A "+Human(r.Status)+" delivery cannot be corrected to "+Human(to)+".")
		}
		from := r.Status
		if err := s.track(ctx, tx, trackInput{DeliveryID: id, Type: "correction", From: from, To: to, Attempt: r.Attempt, Source: "staff",
			Actor: actor, Notes: why, Key: "correction:" + itoa(r.Version)}); err != nil {
			return err
		}
		extra := ``
		if from == "delivered" {
			extra = `, delivered_at = NULL, failed_at = now()`
			// Proofs are immutable and stay as history of what was recorded.
			if err := s.Fulfillment.Interrupt(ctx, tx, r.ObligationID, actor, "Delivery outcome corrected: "+why); err != nil {
				return err
			}
		} else {
			extra = `, failed_at = NULL`
			if _, err := tx.Exec(ctx, `UPDATE dispatch_stops SET status = 'active' WHERE id = (SELECT id FROM dispatch_stops
				WHERE delivery_id = $1 AND status = 'done' ORDER BY added_at DESC LIMIT 1)`, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE deliveries SET status = $2, version = version + 1, updated_at = now()`+extra+` WHERE id = $1`, id, to); err != nil {
			return err
		}
		if _, err := s.RaiseException(ctx, tx, ExceptionInput{Stage: "delivery", Type: "other", Severity: "critical",
			Summary: "Outcome of " + r.Number + " corrected", Notes: Human(from) + " → " + Human(to) + ": " + why +
				" Commerce keeps its order status (it never moves an order backwards); review the order.",
			DeliveryID: id, NextAction: "escalate", Actor: actor}); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.corrected", "delivery", id, map[string]any{"from": from, "to": to, "reason": why})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, id)
}

// ---------------------------------------------------------------- returns

type Return struct {
	ID              string     `json:"id"`
	DeliveryID      string     `json:"delivery_id"`
	DeliveryNumber  string     `json:"delivery_number"`
	OrderNumber     string     `json:"order_number"`
	Attempt         int        `json:"attempt"`
	Reason          string     `json:"reason"`
	ReturnedAt      time.Time  `json:"returned_at"`
	ReceivedBy      *string    `json:"received_by"`
	Condition       string     `json:"condition"`
	Disposition     string     `json:"disposition"`
	DispositionNote *string    `json:"disposition_note"`
	Status          string     `json:"status"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	Packages        []string   `json:"packages"`
}

func (s *Service) returns(ctx context.Context, cond string, args ...any) ([]Return, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id::text, r.delivery_id::text, d.number, o.number, r.attempt::int, r.reason, r.returned_at,
		NULLIF(trim(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), r.condition, r.disposition, r.disposition_note, r.status,
		r.reviewed_at, COALESCE((SELECT array_agg(p.code ORDER BY p.code) FROM delivery_return_packages rp JOIN fulfillment_packages p ON p.id = rp.package_id
			WHERE rp.return_id = r.id), '{}')
		FROM delivery_returns r JOIN deliveries d ON d.id = r.delivery_id JOIN orders o ON o.id = d.order_id
		LEFT JOIN users u ON u.id = r.received_by WHERE `+cond+` ORDER BY r.returned_at DESC LIMIT 300`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Return])
}

func (s *Service) Returns(ctx context.Context, status string) ([]Return, error) {
	switch status {
	case "", "awaiting_review":
		return s.returns(ctx, "r.status = 'awaiting_review'")
	case "all":
		return s.returns(ctx, "true")
	}
	return s.returns(ctx, "r.status = $1", status)
}

type ReturnInput struct {
	Reason     string     `json:"reason"`
	Condition  string     `json:"condition"`
	ReturnedAt *time.Time `json:"returned_at"`
}

// RecordReturn records packages coming back after a failed attempt. They
// are quarantined and need review; they never return to food inventory.
func (s *Service) RecordReturn(ctx context.Context, actor, id string, in ReturnInput) (*Delivery, error) {
	why, err := reason(in.Reason, 3, 500)
	if err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"intact", "damaged", "temperature_concern", "unknown"}, in.Condition) {
		return nil, fieldErr("condition", "Choose the condition the packages came back in.")
	}
	at, err := s.occurred(in.ReturnedAt)
	if err != nil {
		return nil, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Status != "delivery_failed" {
			return conflict("invalid_transition", "Only a failed delivery can be returned; this one is "+Human(r.Status)+".")
		}
		var rid string
		if err := tx.QueryRow(ctx, `INSERT INTO delivery_returns (delivery_id, attempt, reason, returned_at, received_by, condition)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id::text`, id, r.Attempt, why, at, nilIfEmpty(actor), in.Condition).Scan(&rid); err != nil {
			return err
		}
		pkgs, err := s.Fulfillment.Packages(ctx, tx, r.ObligationID)
		if err != nil {
			return err
		}
		ids := []string{}
		for _, p := range pkgs {
			ids = append(ids, p.ID)
			if _, err := tx.Exec(ctx, `INSERT INTO delivery_return_packages (return_id, package_id) VALUES ($1, $2)`, rid, p.ID); err != nil {
				return err
			}
		}
		if err := s.Fulfillment.SetPackageHandoff(ctx, tx, ids, "returned"); err != nil {
			return err
		}
		if err := s.move(ctx, tx, r, "returned", trackInput{Source: "staff", Actor: actor, OccurredAt: at, Notes: why,
			Details: map[string]any{"condition": in.Condition, "disposition": "quarantined"}, Key: "returned:" + itoa(r.Attempt)}, ""); err != nil {
			return err
		}
		if err := s.Fulfillment.Interrupt(ctx, tx, r.ObligationID, actor, "Packages returned: "+why); err != nil {
			return err
		}
		if err := event(ctx, tx, "delivery.returned", "delivery", id, itoa(r.Attempt), map[string]any{"return_id": rid, "condition": in.Condition}); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.returned", "delivery", id, map[string]any{"return_id": rid, "condition": in.Condition})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, id)
}

type ReviewInput struct {
	Disposition string `json:"disposition"` // discarded or other_approved
	Note        string `json:"note"`
}

// ReviewReturn records what happened to quarantined packages. Without an
// approved food-handling procedure the only safe choices are to discard
// them or to name the approved procedure followed.
func (s *Service) ReviewReturn(ctx context.Context, actor, id string, in ReviewInput) ([]Return, error) {
	in.Note = strings.TrimSpace(in.Note)
	if in.Disposition != "discarded" && in.Disposition != "other_approved" {
		return nil, fieldErr("disposition", "Choose discarded, or other approved procedure.")
	}
	if n := runes(in.Note); n < 5 || n > 1000 {
		return nil, fieldErr("note", "Record what was done and under which procedure.")
	}
	if !validID(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE delivery_returns SET disposition = $2, disposition_note = $3, status = 'reviewed', reviewed_by = $4,
			reviewed_at = now() WHERE id = $1 AND status = 'awaiting_review'`, id, in.Disposition, in.Note, nilIfEmpty(actor))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return conflict("invalid_transition", "This return is already reviewed (or does not exist).")
		}
		return record(ctx, tx, actor, "delivery.return_reviewed", "delivery_return", id, map[string]any{"disposition": in.Disposition})
	})
	if err != nil {
		return nil, err
	}
	return s.returns(ctx, "r.id = $1", id)
}
