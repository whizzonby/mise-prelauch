// Package procurement turns released production plans into purchasing:
// requirement snapshots (runs), the net requirement calculation, explained
// recommendations, purchasing decisions, RFQs and quotations, and purchase
// orders with approvals and revisions.
//
// Demand comes only from released production plans, through the production
// module's read contract. Stock counts toward a plan only once it is reserved
// for that plan, and inbound only through purchase order lines bought for
// that planning line, so neither is ever counted twice. Nothing here picks a
// supplier on its own, invents a threshold, or sends a message.
package procurement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/consumer"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/outbox"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/production"
	"mise.tt/api/internal/suppliers"
	"mise.tt/api/internal/support"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Stock is what procurement asks of inventory (internal/inventory
// implements it): per ingredient, the stock already reserved for a plan and
// the usable stock nobody has reserved.
type Stock interface {
	PlanPosition(ctx context.Context, q db.Querier, planID string, ingredientIDs []string) (map[string]StockPosition, error)
}

// StockPosition is in the ingredient's base unit. Issued is stock already
// issued to the plan's batches, net of returns: it left the reservation but
// still serves the plan.
type StockPosition struct {
	Reserved  qty.Q
	Issued    qty.Q
	Available qty.Q
}

// Alerts raises operational alerts (internal/backoffice implements it).
type Alerts interface {
	Raise(ctx context.Context, q db.Querier, a support.Alert) error
}

type Service struct {
	pool       *pgxpool.Pool
	Suppliers  *suppliers.Service
	Production *production.Service
	Stock      Stock
	Alerts     Alerts
	Now        func() time.Time
	Location   *time.Location
	// How old an event must be before it is dispatched (see Dispatch).
	SettleDelay time.Duration
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, Now: time.Now, Location: time.UTC, SettleDelay: 30 * time.Second}
}

func (s *Service) today() time.Time {
	t := s.Now().In(s.Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return db.InTx(ctx, s.pool, fn)
}

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}
func conflict(code, msg string) error { return httpx.NewError(http.StatusConflict, code, msg) }
func unprocessable(code, msg string) error {
	return httpx.NewError(http.StatusUnprocessableEntity, code, msg)
}
func forbidden(code, msg string) error { return httpx.NewError(http.StatusForbidden, code, msg) }
func clean(s string) string            { return strings.Join(strings.Fields(s), " ") }
func runes(s string) int               { return len([]rune(s)) }
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func notFoundIf(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	return err
}
func human(s string) string { return strings.ReplaceAll(s, "_", " ") }

func event(ctx context.Context, q db.Querier, typ, aggType, id, dedupe string, payload map[string]any) error {
	return outbox.Record(ctx, q, outbox.Event{Type: typ, AggregateType: aggType, AggregateID: id, DedupeKey: dedupe, Payload: payload})
}

func (s *Service) raise(ctx context.Context, q db.Querier, a support.Alert) error {
	if s.Alerts == nil {
		return nil
	}
	return s.Alerts.Raise(ctx, q, a)
}

func lock(ctx context.Context, q db.Querier, key string) error {
	_, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 6))`, key)
	return err
}

// ceil rounds a quantity up to a whole number.
func ceil(a qty.Q) qty.Q {
	r := a.Rat()
	n := new(big.Int).Quo(r.Num(), r.Denom())
	if new(big.Rat).SetInt(n).Cmp(r) < 0 {
		n.Add(n, big.NewInt(1))
	}
	return qty.Int(n.Int64())
}

func maxQ(a, b qty.Q) qty.Q {
	if a.Cmp(b) >= 0 {
		return a
	}
	return b
}

func parseQ(s *string) *qty.Q {
	if s == nil {
		return nil
	}
	q, err := qty.Parse(*s)
	if err != nil {
		return nil
	}
	return &q
}

// ---------------------------------------------------------------- settings

// Settings are the procurement policies. Every value starts unset.
type Settings struct {
	ApprovalLimitMinor        *int64    `json:"approval_limit_minor"`
	ReceivingTolerancePercent *string   `json:"receiving_tolerance_percent"`
	Currencies                []string  `json:"currencies"`
	AllowUnapprovedSuppliers  bool      `json:"allow_unapproved_suppliers"`
	UnapprovedPolicyRef       string    `json:"unapproved_policy_ref"`
	UpdatedAt                 time.Time `json:"updated_at"`
	Notes                     []string  `json:"notes"`
}

func (s *Service) Settings(ctx context.Context) (*Settings, error) { return s.settings(ctx, s.pool) }

func (s *Service) settings(ctx context.Context, q db.Querier) (*Settings, error) {
	var x Settings
	err := q.QueryRow(ctx, `SELECT approval_limit_minor, trim_scale(receiving_tolerance_percent)::text, currencies, allow_unapproved_suppliers,
		unapproved_policy_ref, updated_at FROM procurement_settings`).Scan(&x.ApprovalLimitMinor, &x.ReceivingTolerancePercent, &x.Currencies,
		&x.AllowUnapprovedSuppliers, &x.UnapprovedPolicyRef, &x.UpdatedAt)
	if err != nil {
		return nil, err
	}
	x.Notes = []string{}
	if x.ApprovalLimitMinor == nil {
		x.Notes = append(x.Notes, "No approval limit is configured: every purchase order needs one approval by someone other than its buyer.")
	}
	if x.ReceivingTolerancePercent == nil {
		x.Notes = append(x.Notes, "No receiving tolerance is configured: nothing above the ordered quantity can be received.")
	}
	if !x.AllowUnapprovedSuppliers {
		x.Notes = append(x.Notes, "Unapproved suppliers cannot be ordered from: no exception policy is recorded.")
	}
	return &x, nil
}

// SettingsInput changes procurement policies, as the business decided them.
type SettingsInput struct {
	ApprovalLimitMinor        *int64    `json:"approval_limit_minor"`
	ClearApprovalLimit        bool      `json:"clear_approval_limit"`
	ReceivingTolerancePercent *string   `json:"receiving_tolerance_percent"`
	ClearTolerance            bool      `json:"clear_tolerance"`
	Currencies                *[]string `json:"currencies"`
	AllowUnapprovedSuppliers  *bool     `json:"allow_unapproved_suppliers"`
	UnapprovedPolicyRef       *string   `json:"unapproved_policy_ref"`
}

func (s *Service) SaveSettings(ctx context.Context, actor string, in SettingsInput) (*Settings, error) {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.ApprovalLimitMinor != nil && *in.ApprovalLimitMinor < 0 {
		bad("approval_limit_minor", "Enter the limit in cents.")
	}
	var tol *string
	if in.ReceivingTolerancePercent != nil {
		t, err := qty.Parse(*in.ReceivingTolerancePercent)
		if err != nil || t.Sign() < 0 || t.Cmp(qty.Int(100)) > 0 {
			bad("receiving_tolerance_percent", "A percentage between 0 and 100.")
		} else {
			v := t.Format(2)
			tol = &v
		}
	}
	if in.Currencies != nil {
		list := []string{}
		for _, c := range *in.Currencies {
			c = strings.ToUpper(strings.TrimSpace(c))
			if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(c) {
				bad("currencies", "Three-letter currency codes.")
				break
			}
			if !slices.Contains(list, c) {
				list = append(list, c)
			}
		}
		if len(list) == 0 {
			bad("currencies", "At least one currency.")
		}
		*in.Currencies = list
	}
	if in.UnapprovedPolicyRef != nil {
		*in.UnapprovedPolicyRef = clean(*in.UnapprovedPolicyRef)
		if runes(*in.UnapprovedPolicyRef) > 200 {
			bad("unapproved_policy_ref", "At most 200 characters.")
		}
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE procurement_settings SET
			approval_limit_minor = CASE WHEN $1 THEN NULL ELSE COALESCE($2, approval_limit_minor) END,
			receiving_tolerance_percent = CASE WHEN $3 THEN NULL ELSE COALESCE($4::numeric, receiving_tolerance_percent) END,
			currencies = COALESCE($5, currencies), allow_unapproved_suppliers = COALESCE($6, allow_unapproved_suppliers),
			unapproved_policy_ref = COALESCE($7, unapproved_policy_ref), updated_by = $8, updated_at = now()`,
			in.ClearApprovalLimit, in.ApprovalLimitMinor, in.ClearTolerance, tol, in.Currencies, in.AllowUnapprovedSuppliers, in.UnapprovedPolicyRef, nilIfEmpty(actor))
		if err != nil {
			if strings.Contains(err.Error(), "procurement_settings_exception") {
				return fieldErr("unapproved_policy_ref", "Name the approved policy that allows ordering from unapproved suppliers.")
			}
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "procurement.settings_saved", TargetType: "procurement_settings", TargetID: "00000000-0000-0000-0000-000000000000"})
	})
	if err != nil {
		return nil, err
	}
	return s.Settings(ctx)
}

// ---------------------------------------------------------------- runs

// Run is the procurement view of one released production plan.
type Run struct {
	ID               string     `json:"id"`
	Number           string     `json:"number"`
	PlanID           string     `json:"plan_id"`
	PlanNumber       string     `json:"plan_number"`
	SnapshotRevision int        `json:"snapshot_revision"`
	Week             string     `json:"week"`
	RequiredBy       string     `json:"required_by"`
	SourceEventID    *int64     `json:"source_event_id"`
	Status           string     `json:"status"`
	ReviewReason     *string    `json:"review_reason"`
	ApprovedByName   *string    `json:"approved_by_name"`
	ApprovedAt       *time.Time `json:"approved_at"`
	CreatedByName    string     `json:"created_by_name"`
	CreatedAt        time.Time  `json:"created_at"`
	// Summary figures for lists.
	Lines     int `json:"lines"`
	Shortages int `json:"shortages"`
	Pending   int `json:"pending"`

	Requirements []RunRequirement `json:"requirements,omitempty"`
	PlanLines    []Line           `json:"plan_lines,omitempty"`
	Warnings     []string         `json:"warnings"`
	// A newer demand revision exists for the plan's week.
	NewerRevision *int `json:"newer_revision"`
}

// RunRequirement is a gross requirement as consumed. Never edited.
type RunRequirement struct {
	IngredientID   string   `json:"ingredient_id"`
	IngredientName string   `json:"ingredient_name"`
	Preparation    string   `json:"preparation"`
	Quantity       *string  `json:"quantity"`
	Unit           string   `json:"unit"`
	Status         string   `json:"status"`
	Approximate    bool     `json:"approximate"`
	BatchIDs       []string `json:"batch_ids"`
}

// Line is one ingredient's net calculation, options and decision.
type Line struct {
	ID             string  `json:"id"`
	IngredientID   string  `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	Unit           string  `json:"unit"`
	Gross          *string `json:"gross"`
	Buffer         string  `json:"buffer"`
	Reserved       string  `json:"reserved"`
	// Already issued to the plan's batches, net of returns.
	Issued string `json:"issued"`
	// Usable stock nobody has reserved: shown, and reservable, but not counted.
	Available string `json:"available"`
	// Still to receive on PO lines bought for this line, expected by the required-by date.
	Inbound     string `json:"inbound"`
	InboundLate string `json:"inbound_late"`
	// gross + buffer − reserved − issued − inbound, never below zero. Nil when incomplete.
	Shortage   *string  `json:"shortage"`
	Incomplete bool     `json:"incomplete"`
	Explain    []string `json:"explain"`

	Options []Option `json:"options"`
	// The product pre-selected because it is the only option, if any.
	Suggested *string `json:"suggested_product_id"`

	Decision       string     `json:"decision"`
	ProductID      *string    `json:"product_id"`
	Packs          *string    `json:"packs"`
	DecisionNote   string     `json:"decision_note"`
	DecidedByName  *string    `json:"decided_by_name"`
	DecidedAt      *time.Time `json:"decided_at"`
	PurchaseOrders []LinePO   `json:"purchase_orders"`
}

// Option is a purchasable product for a line, with the facts to choose by.
type Option struct {
	ProductID     string `json:"product_id"`
	ProductName   string `json:"product_name"`
	SupplierID    string `json:"supplier_id"`
	SupplierName  string `json:"supplier_name"`
	PackQuantity  string `json:"pack_quantity"`
	PackUnit      string `json:"pack_unit"`
	PackBase      string `json:"pack_base"`
	MinOrderPacks string `json:"min_order_packs"`
	// Whole packs to cover the shortage, at least the minimum order.
	Packs    string `json:"packs"`
	Quantity string `json:"quantity"`
	Surplus  string `json:"surplus"`
	// Per pack, in force today; nil when no price is recorded.
	PriceMinor   *int64  `json:"price_minor"`
	Currency     *string `json:"currency"`
	CostMinor    *int64  `json:"cost_minor"`
	LeadTimeDays *int    `json:"lead_time_days"`
	// Earliest delivery if ordered today, when the lead time is known.
	EarliestDelivery *string  `json:"earliest_delivery"`
	LeadTimeRisk     bool     `json:"lead_time_risk"`
	Availability     string   `json:"availability"`
	Notes            []string `json:"notes"`
}

// LinePO is a purchase order line bought for a planning line.
type LinePO struct {
	POID     string  `json:"po_id"`
	Number   string  `json:"number"`
	Status   string  `json:"status"`
	Packs    string  `json:"packs"`
	Quantity *string `json:"quantity"`
	Expected *string `json:"expected_delivery"`
}

const runCols = `r.id, r.number, r.plan_id, r.plan_number, r.snapshot_revision, r.week_start::text, r.required_by::text, r.source_event_id,
	r.status, r.review_reason,
	CASE WHEN r.approved_by IS NULL THEN NULL ELSE COALESCE(btrim(a.first_name || ' ' || a.last_name), '') END, r.approved_at,
	COALESCE(btrim(c.first_name || ' ' || c.last_name), 'Automatic, from the plan release'), r.created_at,
	(SELECT count(*) FROM procurement_lines l WHERE l.run_id = r.id)::int, 0,
	(SELECT count(*) FROM procurement_lines l WHERE l.run_id = r.id AND l.decision = 'pending')::int`

const runFrom = ` FROM procurement_runs r LEFT JOIN users a ON a.id = r.approved_by LEFT JOIN users c ON c.id = r.created_by`

func scanRun(r pgx.Row) (Run, error) {
	var x Run
	err := r.Scan(&x.ID, &x.Number, &x.PlanID, &x.PlanNumber, &x.SnapshotRevision, &x.Week, &x.RequiredBy, &x.SourceEventID,
		&x.Status, &x.ReviewReason, &x.ApprovedByName, &x.ApprovedAt, &x.CreatedByName, &x.CreatedAt, &x.Lines, &x.Shortages, &x.Pending)
	x.Warnings = []string{}
	return x, err
}

// Runs lists procurement runs, newest week first.
func (s *Service) Runs(ctx context.Context, status string) ([]Run, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+runCols+runFrom+` WHERE ($1 = '' OR r.status = $1) ORDER BY r.week_start DESC, r.number DESC LIMIT 200`, status)
	if err != nil {
		return nil, err
	}
	runs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Run, error) { return scanRun(r) })
	if err != nil {
		return nil, err
	}
	for i := range runs {
		runs[i].Warnings = []string{}
		lines, err := s.lines(ctx, s.pool, &runs[i], false)
		if err != nil {
			return nil, err
		}
		for _, l := range lines {
			if l.Incomplete || (l.Shortage != nil && qty.MustParse(*l.Shortage).Sign() > 0) {
				runs[i].Shortages++
			}
		}
	}
	return runs, nil
}

// PendingPlans lists released plans with no procurement run yet: the safety
// net when a release event has not been consumed.
func (s *Service) PendingPlans(ctx context.Context) ([]production.PlanRef, error) {
	from := s.today().AddDate(0, 0, -28).Format(time.DateOnly)
	plans, err := s.Production.ReleasedPlans(ctx, s.pool, from)
	if err != nil {
		return nil, err
	}
	out := []production.PlanRef{}
	for _, p := range plans {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM procurement_runs WHERE plan_id = $1 AND status <> 'cancelled')`, p.ID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			out = append(out, p)
		}
	}
	return out, nil
}

// CreateRun snapshots a released plan's requirements. However often it is
// asked (an event delivered twice, a planner clicking as the event arrives),
// a plan has one live run: the existing one is returned with created false.
func (s *Service) CreateRun(ctx context.Context, actor, planID string, sourceEvent *int64) (*Run, bool, error) {
	if !uuidPattern.MatchString(planID) {
		return nil, false, fieldErr("plan_id", "Choose a released production plan.")
	}
	var id string
	created := false
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, "procurement-run:"+planID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM procurement_runs WHERE plan_id = $1 AND status <> 'cancelled'`, planID).Scan(&id); err == nil {
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		p, err := s.Production.ProcurementRequirements(ctx, tx, planID)
		if err != nil {
			if errors.Is(err, httpx.ErrNotFound) {
				return fieldErr("plan_id", "Production plan not found.")
			}
			return err
		}
		if p.Status != "released" {
			return conflict("plan_not_released", "Only a released production plan is purchased for; plan "+p.Number+" is "+human(p.Status)+".")
		}
		n, err := suppliers.NextNumber(ctx, tx, "PR", "")
		if err != nil {
			return err
		}
		number := fmt.Sprintf("PR-%04d", n)
		if err := tx.QueryRow(ctx, `INSERT INTO procurement_runs (number, plan_id, plan_number, snapshot_id, snapshot_revision, week_start, required_by,
			source_event_id, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
			number, p.ID, p.Number, p.SnapshotID, p.SnapshotRevision, p.Week, p.ProductionDate, sourceEvent, nilIfEmpty(actor)).Scan(&id); err != nil {
			return err
		}
		// Gross per ingredient, all preparations together: purchasing buys
		// the ingredient, the kitchen prepares it.
		type agg struct {
			unit     string
			total    qty.Q
			complete bool
		}
		byIng, order := map[string]*agg{}, []string{}
		for _, r := range p.Requirements {
			if _, err := tx.Exec(ctx, `INSERT INTO procurement_requirements (run_id, requirement_id, ingredient_id, preparation, quantity, unit, status, approximate, batch_ids)
				VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8, $9)`, id, r.ID, r.IngredientID, r.Preparation, r.Quantity, r.Unit, r.Status, r.Approximate, r.BatchIDs); err != nil {
				return err
			}
			a := byIng[r.IngredientID]
			if a == nil {
				a = &agg{unit: r.Unit, complete: true}
				byIng[r.IngredientID] = a
				order = append(order, r.IngredientID)
			}
			if q := parseQ(r.Quantity); q != nil && r.Status == "ok" {
				a.total = a.total.Add(*q)
			} else {
				a.complete = false
			}
		}
		for _, ing := range order {
			a := byIng[ing]
			var gross *string
			if a.complete {
				v := a.total.String()
				gross = &v
			}
			if _, err := tx.Exec(ctx, `INSERT INTO procurement_lines (run_id, ingredient_id, unit, gross) VALUES ($1, $2, $3, $4::numeric)`,
				id, ing, a.unit, gross); err != nil {
				return err
			}
		}
		created = true
		run, err := s.run(ctx, tx, id)
		if err != nil {
			return err
		}
		short := []map[string]any{}
		for _, l := range run.PlanLines {
			if l.Incomplete || (l.Shortage != nil && qty.MustParse(*l.Shortage).Sign() > 0) {
				short = append(short, map[string]any{"ingredient_id": l.IngredientID, "shortage": l.Shortage, "unit": l.Unit, "incomplete": l.Incomplete})
			}
		}
		if len(short) > 0 {
			if err := event(ctx, tx, "procurement.shortage_identified", "procurement_run", id, "", map[string]any{
				"plan_id": p.ID, "required_by": p.ProductionDate, "shortages": short}); err != nil {
				return err
			}
			if err := s.raise(ctx, tx, support.Alert{DedupeKey: "procurement_shortage:" + id, Kind: "ingredient_shortage", Severity: "warning",
				Title:        fmt.Sprintf("Plan %s needs purchasing: %d ingredient(s) short", p.Number, len(short)),
				Body:         "Ingredients are needed by " + p.ProductionDate + ". Review the procurement run and decide what to buy.",
				ResourceType: "procurement_run", ResourceID: id, Permission: "procurement.plan"}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "procurement.run_created", TargetType: "procurement_run", TargetID: id,
			Metadata: map[string]any{"plan_id": planID, "lines": len(order), "event_id": sourceEvent}})
	})
	if err != nil {
		return nil, false, err
	}
	r, err := s.Run(ctx, id)
	return r, created, err
}

// Run returns a run with its requirements and calculated lines.
func (s *Service) Run(ctx context.Context, id string) (*Run, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	return s.run(ctx, s.pool, id)
}

func (s *Service) run(ctx context.Context, q db.Querier, id string) (*Run, error) {
	r, err := scanRun(q.QueryRow(ctx, `SELECT `+runCols+runFrom+` WHERE r.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	rows, err := q.Query(ctx, `SELECT pr.ingredient_id, i.name, pr.preparation, trim_scale(pr.quantity)::text, pr.unit, pr.status, pr.approximate, pr.batch_ids::text[]
		FROM procurement_requirements pr JOIN ingredients i ON i.id = pr.ingredient_id WHERE pr.run_id = $1 ORDER BY i.name, pr.preparation`, id)
	if err != nil {
		return nil, err
	}
	if r.Requirements, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RunRequirement]); err != nil {
		return nil, err
	}
	if r.PlanLines, err = s.lines(ctx, q, &r, true); err != nil {
		return nil, err
	}
	for _, l := range r.PlanLines {
		if l.Incomplete || (l.Shortage != nil && qty.MustParse(*l.Shortage).Sign() > 0) {
			r.Shortages++
		}
	}
	// A newer demand revision is flagged for review, never applied silently.
	if s.Production != nil {
		if p, err := s.Production.ProcurementRequirements(ctx, q, r.PlanID); err == nil && p.CurrentRevision > r.SnapshotRevision {
			n := p.CurrentRevision
			r.NewerRevision = &n
			r.Warnings = append(r.Warnings, fmt.Sprintf("Orders for this week changed after the plan was made (demand revision %d; this run uses %d). The released plan is unchanged; review whether purchasing should change.",
				n, r.SnapshotRevision))
		}
	}
	r.Warnings = append(r.Warnings, "No approved safety stock is configured: every buffer is zero.")
	if s.Stock == nil {
		r.Warnings = append(r.Warnings, "Inventory is not connected: reserved and available stock are shown as zero.")
	}
	return &r, nil
}

// lines calculates each ingredient line. withOptions adds supplier options.
func (s *Service) lines(ctx context.Context, q db.Querier, r *Run, withOptions bool) ([]Line, error) {
	rows, err := q.Query(ctx, `SELECT l.id, l.ingredient_id, i.name, l.unit, trim_scale(l.gross)::text, trim_scale(l.buffer)::text, l.decision,
		l.product_id::text, trim_scale(l.packs)::text, l.decision_note,
		CASE WHEN l.decided_by IS NULL THEN NULL ELSE COALESCE(btrim(u.first_name || ' ' || u.last_name), '') END, l.decided_at
		FROM procurement_lines l JOIN ingredients i ON i.id = l.ingredient_id LEFT JOIN users u ON u.id = l.decided_by
		WHERE l.run_id = $1 ORDER BY i.name`, r.ID)
	if err != nil {
		return nil, err
	}
	lines, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Line, error) {
		var l Line
		err := row.Scan(&l.ID, &l.IngredientID, &l.IngredientName, &l.Unit, &l.Gross, &l.Buffer, &l.Decision, &l.ProductID, &l.Packs,
			&l.DecisionNote, &l.DecidedByName, &l.DecidedAt)
		return l, err
	})
	if err != nil || len(lines) == 0 {
		return lines, err
	}
	ids := make([]string, len(lines))
	for i, l := range lines {
		ids[i] = l.IngredientID
	}
	pos := map[string]StockPosition{}
	if s.Stock != nil {
		if pos, err = s.Stock.PlanPosition(ctx, q, r.PlanID, ids); err != nil {
			return nil, err
		}
	}
	requiredBy, _ := time.Parse(time.DateOnly, r.RequiredBy)
	for i := range lines {
		l := &lines[i]
		l.Options, l.PurchaseOrders, l.Explain = []Option{}, []LinePO{}, []string{}
		p := pos[l.IngredientID]
		l.Reserved, l.Issued, l.Available = p.Reserved.String(), p.Issued.String(), p.Available.String()
		inbound, late, pos2, err := s.inbound(ctx, q, l.ID, requiredBy)
		if err != nil {
			return nil, err
		}
		l.Inbound, l.InboundLate, l.PurchaseOrders = inbound.String(), late.String(), pos2
		gross := parseQ(l.Gross)
		buffer := qty.MustParse(l.Buffer)
		if gross == nil {
			l.Incomplete = true
			l.Explain = append(l.Explain, "Incomplete: at least one requirement for this ingredient could not be converted to its base unit, so the gross quantity is unknown. Fix the recipe or conversion; nothing is assumed.")
		} else {
			short := gross.Add(buffer).Sub(p.Reserved).Sub(p.Issued).Sub(inbound)
			if short.Sign() < 0 {
				short = qty.Q{}
			}
			v := short.String()
			l.Shortage = &v
			l.Explain = append(l.Explain,
				fmt.Sprintf("Gross requirement %s %s: the plan's requirements for every preparation of this ingredient.", gross.String(), l.Unit),
				fmt.Sprintf("Buffer %s %s: no approved safety stock is configured.", buffer.String(), l.Unit),
				fmt.Sprintf("Reserved for this plan %s %s: stock already held for it; it counts once.", p.Reserved.String(), l.Unit),
				fmt.Sprintf("Issued to this plan's batches %s %s: stock already given to the kitchen for it, net of returns.", p.Issued.String(), l.Unit),
				fmt.Sprintf("Inbound %s %s: still to receive on purchase orders bought for this line, expected by %s.", inbound.String(), l.Unit, r.RequiredBy))
			if late.Sign() > 0 {
				l.Explain = append(l.Explain, fmt.Sprintf("Not counted: %s %s ordered for this line but expected after %s.", late.String(), l.Unit, r.RequiredBy))
			}
			if p.Available.Sign() > 0 {
				l.Explain = append(l.Explain, fmt.Sprintf("Not counted until reserved: %s %s of usable stock nobody has reserved. Reserve it to use it.", p.Available.String(), l.Unit))
			}
			l.Explain = append(l.Explain, fmt.Sprintf("Shortage %s %s = gross + buffer − reserved − issued − inbound, never below zero.", v, l.Unit))
		}
		if withOptions && s.Suppliers != nil && !l.Incomplete && qty.MustParse(*l.Shortage).Sign() > 0 {
			if l.Options, err = s.options(ctx, q, l.IngredientID, qty.MustParse(*l.Shortage), requiredBy); err != nil {
				return nil, err
			}
			if len(l.Options) == 1 {
				l.Suggested = &l.Options[0].ProductID
			}
		}
	}
	return lines, nil
}

// inbound sums what is still to receive on approved or issued PO lines bought
// for this planning line, split by whether it is expected in time.
func (s *Service) inbound(ctx context.Context, q db.Querier, lineID string, requiredBy time.Time) (inTime, late qty.Q, list []LinePO, err error) {
	rows, err := q.Query(ctx, `SELECT po.id, po.number, po.status, trim_scale(pl.packs)::text, trim_scale(pl.quantity_base)::text,
		trim_scale(pl.received_packs)::text, po.expected_delivery::text
		FROM purchase_order_lines pl JOIN purchase_orders po ON po.id = pl.po_id
		WHERE pl.procurement_line_id = $1 AND po.status <> 'cancelled' ORDER BY po.number`, lineID)
	if err != nil {
		return
	}
	defer rows.Close()
	list = []LinePO{}
	for rows.Next() {
		var x LinePO
		var received string
		if err = rows.Scan(&x.POID, &x.Number, &x.Status, &x.Packs, &x.Quantity, &received, &x.Expected); err != nil {
			return
		}
		list = append(list, x)
		if !slices.Contains([]string{"approved", "issued", "partially_received"}, x.Status) || x.Quantity == nil {
			continue
		}
		packs, recv := qty.MustParse(x.Packs), qty.MustParse(received)
		remaining := packs.Sub(recv)
		if remaining.Sign() <= 0 {
			continue
		}
		base := qty.MustParse(*x.Quantity).Mul(remaining).Div(packs)
		if x.Expected != nil {
			if d, e := time.Parse(time.DateOnly, *x.Expected); e == nil && !d.After(requiredBy) {
				inTime = inTime.Add(base)
				continue
			}
		}
		late = late.Add(base)
	}
	err = rows.Err()
	return
}

// options lists every purchasable product for the ingredient with the facts
// to choose by. They are sorted by supplier name, not by price: nothing is
// chosen automatically.
func (s *Service) options(ctx context.Context, q db.Querier, ingredientID string, shortage qty.Q, requiredBy time.Time) ([]Option, error) {
	products, err := s.Suppliers.ProductsIn(ctx, q, suppliers.ProductFilter{IngredientID: ingredientID, Purchasable: true})
	if err != nil {
		return nil, err
	}
	out := []Option{}
	for _, p := range products {
		if p.PackBase == nil {
			continue
		}
		packBase := qty.MustParse(*p.PackBase)
		packs := maxQ(ceil(shortage.Div(packBase)), ceil(qty.MustParse(p.MinOrderPacks)))
		quantity := packs.Mul(packBase)
		o := Option{ProductID: p.ID, ProductName: p.Name, SupplierID: p.SupplierID, SupplierName: p.SupplierName, PackQuantity: p.PackQuantity,
			PackUnit: p.PackUnit, PackBase: *p.PackBase, MinOrderPacks: p.MinOrderPacks, Packs: packs.String(), Quantity: quantity.String(),
			Surplus: quantity.Sub(shortage).String(), LeadTimeDays: p.LeadTimeDays, Availability: p.Availability, Notes: []string{}}
		if p.PackApproximate {
			o.Notes = append(o.Notes, "The pack converts through an average weight: weigh on receipt.")
		}
		if p.Price != nil {
			price, cur := p.Price.AmountMinor, p.Price.Currency
			cost := qty.Int(price).Mul(packs).Round(0)
			c := cost.Rat().Num().Int64()
			o.PriceMinor, o.Currency, o.CostMinor = &price, &cur, &c
		} else {
			o.Notes = append(o.Notes, "No price in force: ask for a quotation.")
		}
		if p.LeadTimeDays != nil {
			d := s.today().AddDate(0, 0, *p.LeadTimeDays)
			v := d.Format(time.DateOnly)
			o.EarliestDelivery = &v
			if d.After(requiredBy) {
				o.LeadTimeRisk = true
				o.Notes = append(o.Notes, "Ordered today it would arrive after the ingredients are needed.")
			}
		} else {
			o.Notes = append(o.Notes, "Lead time not recorded.")
		}
		if p.Availability == "unavailable" || p.Availability == "seasonal" || p.Availability == "limited" {
			o.Notes = append(o.Notes, "The supplier reports this product as "+p.Availability+".")
		}
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SupplierName < out[j].SupplierName })
	return out, nil
}

// DecisionInput records a purchasing decision for a line.
type DecisionInput struct {
	Decision  string `json:"decision"`
	ProductID string `json:"product_id"`
	Packs     string `json:"packs"`
	Note      string `json:"note"`
}

// Decide records what will be bought for a line (or why nothing will).
func (s *Service) Decide(ctx context.Context, actor, runID, lineID string, in DecisionInput) (*Run, error) {
	in.Note = strings.TrimSpace(in.Note)
	if !slices.Contains([]string{"buy", "no_purchase", "deferred", "pending"}, in.Decision) {
		return nil, fieldErr("decision", "Buy, no purchase or deferred.")
	}
	if (in.Decision == "no_purchase" || in.Decision == "deferred") && runes(in.Note) < 3 {
		return nil, fieldErr("note", "Say why nothing is bought now.")
	}
	if runes(in.Note) > 1000 {
		return nil, fieldErr("note", "At most 1000 characters.")
	}
	if !uuidPattern.MatchString(runID) || !uuidPattern.MatchString(lineID) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var status, ingredient string
		if err := tx.QueryRow(ctx, `SELECT r.status, l.ingredient_id FROM procurement_lines l JOIN procurement_runs r ON r.id = l.run_id
			WHERE l.id = $1 AND l.run_id = $2 FOR UPDATE OF r, l`, lineID, runID).Scan(&status, &ingredient); err != nil {
			return notFoundIf(err)
		}
		if status != "open" {
			return conflict("run_locked", "Decisions on an approved run do not change silently. Reopen it, with a reason, to change them.")
		}
		var product, packs *string
		if in.Decision == "buy" {
			p, err := s.Suppliers.ProductIn(ctx, tx, in.ProductID)
			if err != nil {
				return fieldErr("product_id", "Choose one of the supplier options.")
			}
			if p.IngredientID == nil || *p.IngredientID != ingredient || p.MappingStatus != "confirmed" || !p.Active {
				return fieldErr("product_id", "That product is not a confirmed option for this ingredient.")
			}
			if !p.SupplierQualified {
				return unprocessable("supplier_not_approved", p.SupplierName+" is not an approved supplier.")
			}
			n, err := qty.Parse(in.Packs)
			if err != nil || n.Sign() <= 0 || n.Cmp(ceil(n)) != 0 {
				return fieldErr("packs", "Enter a whole number of packs.")
			}
			if n.Cmp(qty.MustParse(p.MinOrderPacks)) < 0 {
				return fieldErr("packs", "Below the supplier's minimum order of "+p.MinOrderPacks+" packs.")
			}
			v := n.String()
			product, packs = &p.ID, &v
		}
		if _, err := tx.Exec(ctx, `UPDATE procurement_lines SET decision = $2, product_id = $3, packs = $4::numeric, decision_note = $5,
			decided_by = $6, decided_at = now() WHERE id = $1`, lineID, in.Decision, product, packs, in.Note, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "procurement.decision_recorded", TargetType: "procurement_run", TargetID: runID,
			Metadata: map[string]any{"line_id": lineID, "decision": in.Decision, "product_id": product, "packs": packs}})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, runID)
}

// ApproveRun approves the purchasing decisions for a plan. Every line with a
// shortage, and every incomplete line, needs a decision first.
func (s *Service) ApproveRun(ctx context.Context, actor, id string) (*Run, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, "procurement-run-approve:"+id); err != nil {
			return err
		}
		r, err := s.run(ctx, tx, id)
		if err != nil {
			return err
		}
		if r.Status != "open" {
			return conflict("invalid_transition", "This run is "+human(r.Status)+".")
		}
		var missing []string
		for _, l := range r.PlanLines {
			short := l.Incomplete || (l.Shortage != nil && qty.MustParse(*l.Shortage).Sign() > 0)
			if short && l.Decision == "pending" {
				missing = append(missing, l.IngredientName)
			}
		}
		if len(missing) > 0 {
			return unprocessable("decisions_pending", "Decide what to do for: "+strings.Join(missing, ", ")+".")
		}
		if _, err := tx.Exec(ctx, `UPDATE procurement_runs SET status = 'approved', approved_by = $2, approved_at = now(), updated_at = now() WHERE id = $1`,
			id, nilIfEmpty(actor)); err != nil {
			return err
		}
		decisions := []map[string]any{}
		for _, l := range r.PlanLines {
			decisions = append(decisions, map[string]any{"ingredient_id": l.IngredientID, "decision": l.Decision, "product_id": l.ProductID, "packs": l.Packs})
		}
		var approvals int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'procurement.run_approved' AND target_id = $1`, id).Scan(&approvals); err != nil {
			return err
		}
		if err := event(ctx, tx, "procurement.plan_approved", "procurement_run", id, fmt.Sprint(approvals+1), map[string]any{
			"plan_id": r.PlanID, "required_by": r.RequiredBy, "decisions": decisions}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "procurement.run_approved", TargetType: "procurement_run", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, id)
}

// ReopenRun reopens an approved run so its decisions can change, with a reason.
func (s *Service) ReopenRun(ctx context.Context, actor, id, reason string) (*Run, error) {
	reason = clean(reason)
	if runes(reason) < 3 || runes(reason) > 500 {
		return nil, fieldErr("reason", "Say why the approved decisions need to change.")
	}
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE procurement_runs SET status = 'open', review_reason = $2, approved_by = NULL, approved_at = NULL, updated_at = now()
			WHERE id = $1 AND status = 'approved'`, id, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return conflict("invalid_transition", "Only an approved run can be reopened.")
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "procurement.run_reopened", TargetType: "procurement_run", TargetID: id,
			Metadata: map[string]any{"reason": reason}})
	})
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, id)
}

// ---------------------------------------------------------------- event consumption

// JobPlanReleased is the job kind that turns a production.plan_released
// event into a procurement run.
const JobPlanReleased = "procurement.plan_released"

// consumerName is this module's row in event_consumer_offsets.
const consumerName = "procurement"

type releasedPayload struct {
	EventID int64  `json:"event_id"`
	PlanID  string `json:"plan_id"`
}

// dispatcher hands production.plan_released events to the job queue
// (platform/consumer: durable offset, settling delay, at-least-once). The
// handler is idempotent; the planning screen also lists released plans
// without a run, in case anything is ever missed.
func (s *Service) dispatcher() *consumer.Dispatcher {
	return &consumer.Dispatcher{Pool: s.pool, Name: consumerName, SettleDelay: s.SettleDelay, Routes: map[string]consumer.Route{
		"production.plan_released": {Kind: JobPlanReleased, Payload: func(id int64, agg string) any { return releasedPayload{EventID: id, PlanID: agg} }},
	}}
}

// Dispatch hands newly released plans to the job queue.
func (s *Service) Dispatch(ctx context.Context) (int, error) { return s.dispatcher().Dispatch(ctx) }

// HandlePlanReleased is the job handler. Retried with backoff by the job
// queue; after its attempts it stays failed (visible) for someone to replay.
func (s *Service) HandlePlanReleased(ctx context.Context, payload json.RawMessage) error {
	var p releasedPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("procurement: bad payload: %w", err)
	}
	ev := p.EventID
	_, _, err := s.CreateRun(ctx, "", p.PlanID, &ev)
	return err
}

// RunDispatcher dispatches every interval until ctx ends (the worker runs it).
func (s *Service) RunDispatcher(ctx context.Context, interval time.Duration) {
	s.dispatcher().Run(ctx, interval)
}

// Replay moves the consumer back so events after fromID are delivered again.
func Replay(ctx context.Context, q db.Querier, fromID int64) error {
	return consumer.Replay(ctx, q, consumerName, fromID)
}

// Deliveries reports the consumer's state for the overview: the offset and
// any failed or waiting jobs.
type Deliveries = consumer.Status

// Failed is a delivery that ran out of attempts.
type Failed = consumer.Failed

func (s *Service) Deliveries(ctx context.Context) (*Deliveries, error) {
	return consumer.StatusOf(ctx, s.pool, consumerName, JobPlanReleased)
}
