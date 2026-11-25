package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/support"
)

// ---------------------------------------------------------------- reschedule requests

type Reschedule struct {
	ID            string     `json:"id"`
	RequestedBy   string     `json:"requested_by"`
	PreferredDate *string    `json:"preferred_date"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	NewDate       *string    `json:"new_date"`
	DecisionNote  *string    `json:"decision_note"`
	CreatedAt     time.Time  `json:"created_at"`
	DecidedAt     *time.Time `json:"decided_at"`
}

func (s *Service) reschedules(ctx context.Context, id string) ([]Reschedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, requested_by, preferred_date::text, reason, status, new_date::text, decision_note, created_at, decided_at
		FROM delivery_reschedule_requests WHERE delivery_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Reschedule])
}

// PendingReschedules lists open requests for the planning queue.
type PendingReschedule struct {
	Reschedule
	DeliveryID     string  `json:"delivery_id"`
	DeliveryNumber string  `json:"delivery_number"`
	DeliveryStatus string  `json:"delivery_status"`
	ServiceDate    *string `json:"service_date"`
}

func (s *Service) PendingReschedules(ctx context.Context) ([]PendingReschedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT q.id::text, q.requested_by, q.preferred_date::text, q.reason, q.status, q.new_date::text, q.decision_note,
		q.created_at, q.decided_at, d.id::text, d.number, d.status, d.service_date::text
		FROM delivery_reschedule_requests q JOIN deliveries d ON d.id = q.delivery_id WHERE q.status = 'requested' ORDER BY q.created_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PendingReschedule, error) {
		var p PendingReschedule
		err := r.Scan(&p.ID, &p.RequestedBy, &p.PreferredDate, &p.Reason, &p.Status, &p.NewDate, &p.DecisionNote, &p.CreatedAt, &p.DecidedAt,
			&p.DeliveryID, &p.DeliveryNumber, &p.DeliveryStatus, &p.ServiceDate)
		return p, err
	})
}

type RescheduleInput struct {
	PreferredDate string `json:"preferred_date"`
	Reason        string `json:"reason"`
}

// requestReschedule records a request. No new date is promised: a person
// decides, against the calendar and capacity.
func (s *Service) requestReschedule(ctx context.Context, tx pgx.Tx, by, actor string, r *row, in RescheduleInput) error {
	why, err := reason(in.Reason, 3, 500)
	if err != nil {
		return err
	}
	var pref any
	if in.PreferredDate != "" {
		d, ok := parseDay(in.PreferredDate)
		if !ok || d.Before(s.today()) {
			return fieldErr("preferred_date", "Choose a date from today on.")
		}
		pref = d
	}
	if !oneOf(r.Status, "pending_assignment", "assigned", "ready_for_pickup", "delivery_failed") {
		return conflict("too_late", "This delivery can no longer be rescheduled ("+Human(r.Status)+").")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO delivery_reschedule_requests (delivery_id, requested_by, requester_user, preferred_date, reason)
		VALUES ($1, $2, $3, $4, $5)`, r.ID, by, nilIfEmpty(actor), pref, why); err != nil {
		if strings.Contains(err.Error(), "delivery_reschedule_open") {
			return conflict("already_requested", "A reschedule request is already open for this delivery.")
		}
		return err
	}
	return s.track(ctx, tx, trackInput{DeliveryID: r.ID, Type: "reschedule_requested", Attempt: r.Attempt, Source: by_(by), Actor: actor,
		Notes: why, Details: map[string]any{"preferred_date": in.PreferredDate}, Key: "reschedule:" + itoa(r.Version) + ":" + by})
}

func by_(by string) string {
	if by == "customer" {
		return "customer"
	}
	return "staff"
}

func oneOf(v string, in ...string) bool {
	for _, x := range in {
		if v == x {
			return true
		}
	}
	return false
}

func (s *Service) RequestReschedule(ctx context.Context, actor, id string, in RescheduleInput) (*Delivery, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		return s.requestReschedule(ctx, tx, "staff", actor, r, in)
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, id)
}

type DecisionInput struct {
	Decision string `json:"decision"` // approve or decline
	NewDate  string `json:"new_date"`
	WindowID string `json:"window_id"`
	Note     string `json:"note"`
}

// DecideReschedule approves (new date checked against the calendar and
// known capacity) or declines a request. A delivery already with the
// partner is never moved: it can only fail and be reattempted.
func (s *Service) DecideReschedule(ctx context.Context, actor, requestID string, in DecisionInput) (*Delivery, error) {
	if !validID(requestID) {
		return nil, httpx.ErrNotFound
	}
	in.Note = strings.TrimSpace(in.Note)
	if runes(in.Note) > 500 {
		return nil, fieldErr("note", "Keep the note under 500 characters.")
	}
	var deliveryID string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT delivery_id::text, status FROM delivery_reschedule_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(&deliveryID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "requested" {
			return conflict("decided", "This request was already decided.")
		}
		r, err := s.lock(ctx, tx, deliveryID)
		if err != nil {
			return err
		}
		switch in.Decision {
		case "decline":
			if runes(in.Note) < 3 {
				return fieldErr("note", "Say why the request is declined.")
			}
			if _, err := tx.Exec(ctx, `UPDATE delivery_reschedule_requests SET status = 'declined', decision_note = $2, decided_by = $3, decided_at = now()
				WHERE id = $1`, requestID, in.Note, nilIfEmpty(actor)); err != nil {
				return err
			}
			return record(ctx, tx, actor, "delivery.reschedule_declined", "delivery", deliveryID, map[string]any{"request": requestID})
		case "approve":
		default:
			return fieldErr("decision", "Choose approve or decline.")
		}
		date, ok := parseDay(in.NewDate)
		if !ok {
			return fieldErr("new_date", "Choose the new date.")
		}
		if date.Before(s.today()) {
			return fieldErr("new_date", "Choose a date from today on.")
		}
		if in.WindowID != "" && !validID(in.WindowID) {
			return fieldErr("window_id", "Choose a window.")
		}
		if !oneOf(r.Status, "pending_assignment", "assigned", "delivery_failed") {
			return conflict("too_late", "A delivery that is "+Human(r.Status)+" cannot be moved. Record the outcome first.")
		}
		if r.ZoneID == nil {
			return unprocessable("no_zone", "Assign a zone before rescheduling.")
		}
		st, err := s.settings(ctx, tx)
		if err != nil {
			return err
		}
		if err := eligibleDate(st, r.OrderWeek, date); err != nil {
			return err
		}
		open, why, err := serviceDayOpen(ctx, tx, date, r.ZoneID)
		if err != nil {
			return err
		}
		if !open {
			return unprocessable("not_a_service_day", why)
		}
		// Capacity across all partners for that date and zone: a known,
		// full scope is refused; the stop check applies again at assignment.
		if known, full, err := checkCapacity(ctx, tx, date, r.ZoneID, nilIfEmpty(in.WindowID), "00000000-0000-0000-0000-000000000000", nil); err != nil {
			return err
		} else if known && full != "" {
			return unprocessable("capacity", full)
		}
		if r.Status == "assigned" {
			var runID string
			if err := tx.QueryRow(ctx, `SELECT run_id::text FROM dispatch_stops WHERE delivery_id = $1 AND status = 'active'`, r.ID).Scan(&runID); err != nil {
				return err
			}
			run, err := s.lockRun(ctx, tx, runID, "draft")
			if err != nil {
				return conflict("run_finalized", "The delivery's run is finalized; reopen it before moving the delivery.")
			}
			if err := s.unassign(ctx, tx, actor, run, r, "Rescheduled on request"); err != nil {
				return err
			}
		}
		var window *string
		if in.WindowID != "" {
			window = &in.WindowID
		}
		if r.Status == "delivery_failed" {
			if err := s.reattempt(ctx, tx, actor, r, "Rescheduled on request", &date, window); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE deliveries SET service_date = $2, window_id = $3, schedule_confirmed_at = now(), version = version + 1,
			updated_at = now() WHERE id = $1`, r.ID, date, window); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE delivery_reschedule_requests SET status = 'approved', new_date = $2, new_window_id = $3, decision_note = $4,
			decided_by = $5, decided_at = now() WHERE id = $1`, requestID, date, window, nilIfEmpty(in.Note), nilIfEmpty(actor)); err != nil {
			return err
		}
		if err := s.track(ctx, tx, trackInput{DeliveryID: r.ID, Type: "rescheduled", Attempt: r.Attempt, Source: "staff", Actor: actor,
			Notes: in.Note, Details: map[string]any{"new_date": in.NewDate, "window_id": in.WindowID}, Key: "rescheduled:" + requestID}); err != nil {
			return err
		}
		if err := event(ctx, tx, "delivery.rescheduled", "delivery", r.ID, requestID, map[string]any{"new_date": in.NewDate}); err != nil {
			return err
		}
		if err := s.notify(ctx, tx, r, "rescheduled", requestID); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.rescheduled", "delivery", r.ID, map[string]any{"request": requestID, "new_date": in.NewDate})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, deliveryID)
}

// ---------------------------------------------------------------- support links

type CaseLink struct {
	CaseID  string    `json:"case_id"`
	Number  int64     `json:"number"`
	Subject string    `json:"subject"`
	Status  string    `json:"status"`
	Linked  time.Time `json:"linked_at"`
}

func (s *Service) caseLinks(ctx context.Context, id string) ([]CaseLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id::text, c.number, c.subject, c.status, l.created_at FROM delivery_case_links l
		JOIN support_cases c ON c.id = l.support_case_id WHERE l.delivery_id = $1 ORDER BY l.created_at`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[CaseLink])
}

// LinkCase links an existing support case to a delivery.
func (s *Service) LinkCase(ctx context.Context, actor, deliveryID, caseID string) (*Delivery, error) {
	if !validID(deliveryID) || !validID(caseID) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var customer, caseCustomer *string
		if err := tx.QueryRow(ctx, `SELECT customer_id::text FROM deliveries WHERE id = $1`, deliveryID).Scan(&customer); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT customer_id::text FROM support_cases WHERE id = $1`, caseID).Scan(&caseCustomer); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fieldErr("case_id", "Unknown support case.")
			}
			return err
		}
		if caseCustomer != nil && customer != nil && *caseCustomer != *customer {
			return fieldErr("case_id", "That case belongs to another customer.")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO delivery_case_links (support_case_id, delivery_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, caseID, deliveryID); err != nil {
			return err
		}
		return record(ctx, tx, actor, "delivery.case_linked", "delivery", deliveryID, map[string]any{"case_id": caseID})
	})
	if err != nil {
		return nil, err
	}
	return s.Delivery(ctx, Scope{All: true}, deliveryID)
}

// ---------------------------------------------------------------- customer view

// CustomerDelivery is what a customer sees about their order's delivery:
// friendly milestones from real records, the confirmed date and window,
// their address, and their confirmation code while it is out. No partner,
// courier, run or internal status names.
type CustomerDelivery struct {
	OrderID          string            `json:"order_id"`
	Stage            string            `json:"stage"`
	Headline         string            `json:"headline"`
	Detail           string            `json:"detail"`
	ServiceDate      *string           `json:"service_date"`
	Window           *string           `json:"window"`
	Address          json.RawMessage   `json:"address"`
	ConfirmationCode *string           `json:"confirmation_code"`
	CodeExplanation  string            `json:"code_explanation,omitempty"`
	Timeline         []Milestone       `json:"timeline"`
	Issue            *string           `json:"issue"`
	CanReschedule    bool              `json:"can_reschedule"`
	ReschedulePolicy string            `json:"reschedule_policy"`
	OpenRequest      *Reschedule       `json:"open_request"`
	History          []CustomerAttempt `json:"history"`
	LiveTracking     bool              `json:"live_tracking"`
}

type Milestone struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Done  bool       `json:"done"`
	At    *time.Time `json:"at"`
}

type CustomerAttempt struct {
	Attempt int       `json:"attempt"`
	Outcome string    `json:"outcome"`
	At      time.Time `json:"at"`
}

// OrderFacts is what the customer view needs from commerce (passed in, so
// delivery does not read orders on its own).
type OrderFacts struct {
	OrderID   string
	Status    string
	CreatedAt time.Time
	Address   json.RawMessage
}

func (s *Service) ForCustomer(ctx context.Context, customerID string, o OrderFacts) (*CustomerDelivery, error) {
	out := &CustomerDelivery{OrderID: o.OrderID, Address: o.Address, Timeline: []Milestone{}, History: []CustomerAttempt{}, LiveTracking: false}
	created := o.CreatedAt
	ms := func(key, label string, at *time.Time) {
		out.Timeline = append(out.Timeline, Milestone{Key: key, Label: label, Done: at != nil, At: at})
	}
	ms("confirmed", "Order confirmed", &created)

	// Kitchen milestones from the order's own status history.
	var kitchenAt, readyAt *time.Time
	_ = s.pool.QueryRow(ctx, `SELECT min(created_at) FILTER (WHERE to_status IN ('awaiting_production', 'in_production')),
		min(created_at) FILTER (WHERE to_status = 'ready_for_fulfillment') FROM order_status_history WHERE order_id = $1`, o.OrderID).Scan(&kitchenAt, &readyAt)
	ms("kitchen", "Kitchen preparing", kitchenAt)

	var d struct {
		ID, Status          string
		Attempt             int
		ServiceDate, Window *string
		Confirmed           *time.Time
		Address             json.RawMessage
		Delivered           *time.Time
	}
	var verifiedAt *time.Time
	_ = s.pool.QueryRow(ctx, `SELECT verified_at FROM fulfillment_obligations WHERE order_id = $1`, o.OrderID).Scan(&verifiedAt)
	ms("packed", "Packed and checked", verifiedAt)
	err := s.pool.QueryRow(ctx, `SELECT d.id::text, d.status, d.attempt, d.service_date::text,
		CASE WHEN w.status = 'approved' THEN w.label || ' (' || to_char(w.starts_at, 'HH24:MI') || '–' || to_char(w.ends_at, 'HH24:MI') || ')' END,
		d.schedule_confirmed_at, d.address, d.delivered_at
		FROM deliveries d LEFT JOIN delivery_windows w ON w.id = d.window_id WHERE d.order_id = $1 AND d.customer_id = $2`, o.OrderID, customerID).
		Scan(&d.ID, &d.Status, &d.Attempt, &d.ServiceDate, &d.Window, &d.Confirmed, &d.Address, &d.Delivered)
	if errors.Is(err, pgx.ErrNoRows) {
		ms("ready", "Ready for delivery", nil)
		ms("out", "Out for delivery", nil)
		ms("delivered", "Delivered", nil)
		out.Stage = "preparing"
		out.Headline, out.Detail = "We're preparing your order", "We'll confirm your delivery date here once it's scheduled."
		if o.Status == "confirmed" {
			out.Headline = "Order confirmed"
		}
		out.ReschedulePolicy = "Delivery details appear here once your order is packed."
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.Address = d.Address
	if d.Confirmed != nil {
		out.ServiceDate, out.Window = d.ServiceDate, d.Window
	}
	var pickedAt *time.Time
	_ = s.pool.QueryRow(ctx, `SELECT max(occurred_at) FROM delivery_tracking_events WHERE delivery_id = $1 AND to_status = 'picked_up' AND attempt = $2`,
		d.ID, d.Attempt).Scan(&pickedAt)
	ms("ready", "Ready for delivery", d.Confirmed)
	ms("out", "Out for delivery", pickedAt)
	ms("delivered", "Delivered", d.Delivered)

	rows, err := s.pool.Query(ctx, `SELECT attempt, to_status, occurred_at FROM delivery_tracking_events WHERE delivery_id = $1
		AND to_status IN ('delivered', 'delivery_failed', 'returned') ORDER BY id`, d.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a CustomerAttempt
		var st string
		var attempt int16
		if err := rows.Scan(&attempt, &st, &a.At); err != nil {
			rows.Close()
			return nil, err
		}
		a.Attempt = int(attempt)
		a.Outcome = map[string]string{"delivered": "Delivered", "delivery_failed": "Delivery attempt unsuccessful", "returned": "Returned to Mise"}[st]
		out.History = append(out.History, a)
	}
	rows.Close()

	switch d.Status {
	case "pending_assignment", "assigned", "ready_for_pickup":
		out.Stage = "scheduled"
		if d.Confirmed != nil {
			out.Headline, out.Detail = "Delivery scheduled", "Your order is packed and scheduled for delivery."
		} else {
			out.Stage = "packed"
			out.Headline, out.Detail = "Packed and checked", "Your delivery date will appear here once it's confirmed."
		}
	case "picked_up", "in_transit", "arriving":
		out.Stage = "out_for_delivery"
		out.Headline, out.Detail = "Out for delivery", "Your order is with our delivery team. This shows milestones, not live location."
		code := s.ConfirmationCode(d.ID, d.Attempt)
		out.ConfirmationCode = &code
		out.CodeExplanation = "If you're asked for a delivery code, give this one. Mise will never ask for it by phone or message."
	case "delivered":
		out.Stage = "delivered"
		out.Headline, out.Detail = "Delivered", "Enjoy your meal kits. If anything isn't right, let us know."
	case "delivery_failed":
		out.Stage = "issue"
		out.Headline, out.Detail = "We couldn't complete your delivery", "Our team will contact you about the next step."
	case "returned":
		out.Stage = "issue"
		out.Headline, out.Detail = "Your delivery came back to us", "Our team will contact you about what happens next."
	case "cancelled":
		out.Stage = "cancelled"
		out.Headline, out.Detail = "Delivery cancelled", "Contact us if you have any questions."
	}
	var issue string
	if err := s.pool.QueryRow(ctx, `SELECT type FROM delivery_exceptions WHERE delivery_id = $1 AND status IN ('open', 'in_progress')
		AND type IN ('delivery_failure', 'address_problem', 'damaged_package', 'missing_package') ORDER BY created_at DESC LIMIT 1`, d.ID).Scan(&issue); err == nil {
		msg := map[string]string{"delivery_failure": "We're looking into your delivery.", "address_problem": "We're checking your delivery address.",
			"damaged_package": "We're looking into a problem with your package.", "missing_package": "We're looking into a problem with your package."}[issue]
		out.Issue = &msg
	}
	open := []Reschedule{}
	if all, err := s.reschedules(ctx, d.ID); err == nil {
		for _, r := range all {
			if r.Status == "requested" {
				open = append(open, r)
			}
		}
	}
	if len(open) > 0 {
		out.OpenRequest = &open[0]
	}
	cut, err := s.settings(ctx, s.pool)
	if err != nil {
		return nil, err
	}
	out.CanReschedule = out.OpenRequest == nil && oneOf(d.Status, "pending_assignment", "assigned", "ready_for_pickup")
	if cut.RescheduleCutoffHours == nil {
		out.ReschedulePolicy = "You can ask to change your delivery date. We'll reply as soon as we can; a new date isn't confirmed until we do."
	} else {
		out.ReschedulePolicy = "You can ask to change your delivery date up to " + itoa(*cut.RescheduleCutoffHours) + " hours before it. We'll confirm the new date."
		if d.ServiceDate != nil && d.Confirmed != nil {
			if sd, ok := parseDay(*d.ServiceDate); ok {
				start := time.Date(sd.Year(), sd.Month(), sd.Day(), 0, 0, 0, 0, s.Location)
				if s.Now().Add(time.Duration(*cut.RescheduleCutoffHours) * time.Hour).After(start) {
					out.CanReschedule = false
				}
			}
		}
	}
	return out, nil
}

// CustomerReschedule records a customer's request, within the policy.
func (s *Service) CustomerReschedule(ctx context.Context, customerID, orderID, actor string, in RescheduleInput) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `SELECT id::text FROM deliveries WHERE order_id = $1 AND customer_id = $2`, orderID, customerID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return conflict("not_scheduled", "Your order isn't ready for delivery planning yet.")
		}
		if err != nil {
			return err
		}
		r, err := s.lock(ctx, tx, id)
		if err != nil {
			return err
		}
		st, err := s.settings(ctx, tx)
		if err != nil {
			return err
		}
		if !oneOf(r.Status, "pending_assignment", "assigned", "ready_for_pickup") {
			return conflict("too_late", "Your delivery can no longer be changed online. Please contact us.")
		}
		if st.RescheduleCutoffHours != nil && r.ServiceDate != nil {
			start := time.Date(r.ServiceDate.Year(), r.ServiceDate.Month(), r.ServiceDate.Day(), 0, 0, 0, 0, s.Location)
			if s.Now().Add(time.Duration(*st.RescheduleCutoffHours) * time.Hour).After(start) {
				return conflict("too_late", "It's too close to your delivery date to change it online. Please contact us.")
			}
		}
		return s.requestReschedule(ctx, tx, "customer", actor, r, in)
	})
}

// CustomerSupport opens a support case about the order's delivery, linked
// to it, so staff see the full delivery history with the case.
func (s *Service) CustomerSupport(ctx context.Context, customerID, orderID, orderNumber, message string) (*support.Case, error) {
	message = strings.TrimSpace(message)
	if n := runes(message); n < 5 || n > 2000 {
		return nil, fieldErr("message", "Tell us what's wrong (5 to 2,000 characters).")
	}
	var deliveryID string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM deliveries WHERE order_id = $1 AND customer_id = $2`, orderID, customerID).Scan(&deliveryID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	c, err := s.Cases.Create(ctx, "", support.CreateInput{CustomerID: customerID, Subject: "Delivery help: order " + orderNumber,
		Category: "delivery", Priority: "normal", Note: message})
	if err != nil {
		return nil, err
	}
	if deliveryID != "" {
		if _, err := s.pool.Exec(ctx, `INSERT INTO delivery_case_links (support_case_id, delivery_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, c.ID, deliveryID); err != nil {
			return nil, err
		}
	}
	return c, nil
}
