// Package orders owns orders: what is to be cooked and delivered for one
// subscription week, made once at the selection cutoff from a paid week with
// confirmed meals.
//
// An order is a snapshot. It copies each meal's name, version and declared
// allergens, the quantities, the amounts paid and the delivery address, so
// it never has to be rebuilt from records that may since have changed.
// Sprint 3 creates orders as "confirmed"; the production and delivery
// modules will own the later statuses. Nothing here invents those events.
package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/orders/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

type Service struct {
	pool *pgxpool.Pool
	Now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

// Address is the delivery address copied onto an order.
type Address struct {
	RecipientName string  `json:"recipient_name"`
	Phone         *string `json:"phone,omitempty"`
	Line1         string  `json:"line1"`
	Line2         *string `json:"line2,omitempty"`
	Area          *string `json:"area,omitempty"`
	City          string  `json:"city"`
	Region        string  `json:"region"`
	RegionLabel   string  `json:"region_label"`
	Country       string  `json:"country"`
	Instructions  *string `json:"instructions,omitempty"`
}

// ItemInput is one meal of a new order, as recorded from the catalogue.
type ItemInput struct {
	MealID         string
	MealVersion    int
	MealName       string
	MealSlug       string
	Quantity       int
	Servings       int
	Allergens      []string
	AllergenStatus string
	DietaryTags    []string
}

// NewOrder is everything an order is made from.
type NewOrder struct {
	CustomerID          string
	SubscriptionID      string
	SubscriptionCycleID string
	BillingCycleID      string
	// PaymentID is empty when the week was free (nothing collected).
	PaymentID         string
	MenuID            string
	Week              time.Time
	PlanName          string
	Currency          string
	SubtotalMinor     int64
	DiscountMinor     int64
	DeliveryFeeMinor  int64
	TaxMinor          int64
	TotalMinor        int64
	Address           Address
	CustomerAllergens []string
	Items             []ItemInput
}

var location = func() *time.Location {
	if loc, err := time.LoadLocation("America/Port_of_Spain"); err == nil {
		return loc
	}
	return time.FixedZone("AST", -4*3600)
}()

// Create makes the order for a cycle, once. If the cycle already has an
// order (the scheduler ran twice), that order is returned and created=false.
func (s *Service) Create(ctx context.Context, q db.Querier, in NewOrder) (*Order, bool, error) {
	st := store.New(q)
	if existing, err := st.OrderForCycle(ctx, in.SubscriptionCycleID); err == nil {
		o, err := s.view(ctx, q, existing)
		return o, false, err
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	if len(in.Items) == 0 {
		return nil, false, errors.New("orders: an order needs at least one meal")
	}
	count := 0
	for _, it := range in.Items {
		count += it.Quantity
	}
	year := s.Now().In(location).Year()
	n, err := st.NextOrderNumber(ctx, int32(year))
	if err != nil {
		return nil, false, err
	}
	addr, _ := json.Marshal(in.Address)
	if in.CustomerAllergens == nil {
		in.CustomerAllergens = []string{}
	}
	row, err := st.CreateOrder(ctx, store.CreateOrderParams{
		Number: fmt.Sprintf("MO-%d-%06d", year, n), CustomerID: in.CustomerID, SubscriptionID: in.SubscriptionID,
		SubscriptionCycleID: in.SubscriptionCycleID, BillingCycleID: in.BillingCycleID, PaymentID: nilIfEmpty(in.PaymentID), MenuID: in.MenuID,
		WeekStart: in.Week, Currency: in.Currency, SubtotalMinor: in.SubtotalMinor, DiscountMinor: in.DiscountMinor,
		DeliveryFeeMinor: in.DeliveryFeeMinor, TaxMinor: in.TaxMinor, TotalMinor: in.TotalMinor, MealsCount: int16(count),
		PlanName: in.PlanName, AddressSnapshot: addr, CustomerAllergens: in.CustomerAllergens,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent run made it between the read and the insert.
		existing, err := st.OrderForCycle(ctx, in.SubscriptionCycleID)
		if err != nil {
			return nil, false, err
		}
		o, err := s.view(ctx, q, existing)
		return o, false, err
	}
	if err != nil {
		return nil, false, err
	}
	for _, it := range in.Items {
		if err := st.AddOrderItem(ctx, store.AddOrderItemParams{OrderID: row.ID, MealID: it.MealID, MealVersion: int32(it.MealVersion),
			MealName: it.MealName, MealSlug: it.MealSlug, Quantity: int16(it.Quantity), Servings: int16(it.Servings),
			Allergens: nonNil(it.Allergens), AllergenStatus: it.AllergenStatus, DietaryTags: nonNil(it.DietaryTags)}); err != nil {
			return nil, false, err
		}
	}
	if err := st.AddOrderStatus(ctx, store.AddOrderStatusParams{OrderID: row.ID, ToStatus: "confirmed", Reason: ptr("selection finalised at cutoff")}); err != nil {
		return nil, false, err
	}
	o, err := s.view(ctx, q, row)
	return o, true, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------- views

type Item struct {
	MealID         string   `json:"meal_id"`
	MealVersion    int      `json:"meal_version"`
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	Quantity       int      `json:"quantity"`
	Servings       int      `json:"servings"`
	Allergens      []string `json:"allergens"`
	AllergenStatus string   `json:"allergen_status"`
	DietaryTags    []string `json:"dietary_tags"`
}

type StatusChange struct {
	From      *string   `json:"from"`
	To        string    `json:"to"`
	Reason    *string   `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// Order is an order with its items.
type Order struct {
	ID                  string          `json:"id"`
	Number              string          `json:"number"`
	Status              string          `json:"status"`
	Week                string          `json:"week"`
	PlanName            string          `json:"plan_name"`
	MealsCount          int             `json:"meals_count"`
	Currency            string          `json:"currency"`
	SubtotalMinor       int64           `json:"subtotal_minor"`
	DiscountMinor       int64           `json:"discount_minor"`
	DeliveryFeeMinor    int64           `json:"delivery_fee_minor"`
	TaxMinor            int64           `json:"tax_minor"`
	TotalMinor          int64           `json:"total_minor"`
	Address             Address         `json:"address"`
	DeliveryWindow      json.RawMessage `json:"delivery_window"`
	CreatedAt           time.Time       `json:"created_at"`
	Items               []Item          `json:"items"`
	History             []StatusChange  `json:"history,omitempty"`
	CustomerID          string          `json:"customer_id,omitempty"`
	SubscriptionID      string          `json:"subscription_id"`
	BillingCycleID      string          `json:"billing_cycle_id"`
	PaymentID           *string         `json:"payment_id"`
	SubscriptionCycleID string          `json:"subscription_cycle_id"`
	// Set when delivery corrected a delivered outcome (see FlagDeliveryOutcome).
	DeliveryOutcome *DeliveryOutcome `json:"delivery_outcome"`
}

func (s *Service) view(ctx context.Context, q db.Querier, o store.Order) (*Order, error) {
	out := &Order{ID: o.ID, Number: o.Number, Status: o.Status, Week: o.WeekStart.Format("2006-01-02"), PlanName: o.PlanName,
		MealsCount: int(o.MealsCount), Currency: o.Currency, SubtotalMinor: o.SubtotalMinor, DiscountMinor: o.DiscountMinor,
		DeliveryFeeMinor: o.DeliveryFeeMinor, TaxMinor: o.TaxMinor, TotalMinor: o.TotalMinor, CreatedAt: o.CreatedAt,
		Items: []Item{}, SubscriptionID: o.SubscriptionID, BillingCycleID: o.BillingCycleID, PaymentID: o.PaymentID,
		SubscriptionCycleID: o.SubscriptionCycleID, DeliveryWindow: o.DeliveryWindow}
	_ = json.Unmarshal(o.AddressSnapshot, &out.Address)
	oc, err := latestOutcome(ctx, q, o.ID)
	if err != nil {
		return nil, err
	}
	out.DeliveryOutcome = oc
	items, err := store.New(q).OrderItems(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		out.Items = append(out.Items, Item{MealID: it.MealID, MealVersion: int(it.MealVersion), Name: it.MealName, Slug: it.MealSlug,
			Quantity: int(it.Quantity), Servings: int(it.Servings), Allergens: it.Allergens, AllergenStatus: it.AllergenStatus,
			DietaryTags: it.DietaryTags})
	}
	return out, nil
}

// ForCustomer returns one of the customer's orders. Another customer's
// order id answers not found, exactly like an unknown one.
func (s *Service) ForCustomer(ctx context.Context, customerID, id string) (*Order, error) {
	o, err := store.New(s.pool).GetOrderForCustomer(ctx, store.GetOrderForCustomerParams{ID: id, CustomerID: customerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.pool, o)
}

// Summary is an order in a list.
type Summary struct {
	ID         string    `json:"id"`
	Number     string    `json:"number"`
	Status     string    `json:"status"`
	Week       string    `json:"week"`
	PlanName   string    `json:"plan_name"`
	MealsCount int       `json:"meals_count"`
	TotalMinor int64     `json:"total_minor"`
	Currency   string    `json:"currency"`
	CreatedAt  time.Time `json:"created_at"`
	CustomerID string    `json:"customer_id,omitempty"`
	// "disputed" while a corrected delivered outcome is unresolved.
	DeliveryOutcome *string `json:"delivery_outcome"`
}

func summary(o store.Order) Summary {
	return Summary{ID: o.ID, Number: o.Number, Status: o.Status, Week: o.WeekStart.Format("2006-01-02"), PlanName: o.PlanName,
		MealsCount: int(o.MealsCount), TotalMinor: o.TotalMinor, Currency: o.Currency, CreatedAt: o.CreatedAt}
}

func (s *Service) ListForCustomer(ctx context.Context, customerID string, page, size int) ([]Summary, error) {
	if size < 1 || size > 50 {
		size = 20
	}
	if page < 1 {
		page = 1
	}
	rows, err := store.New(s.pool).ListOrdersForCustomer(ctx, store.ListOrdersForCustomerParams{CustomerID: customerID,
		Lim: int32(size), Off: int32((page - 1) * size)})
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, r := range rows {
		out = append(out, summary(r))
	}
	return out, s.outcomes(ctx, out)
}

// ---------------------------------------------------------------- staff

type Filter struct {
	Week       *time.Time
	Status     string
	CustomerID string
}

func (s *Service) List(ctx context.Context, f Filter) ([]Summary, error) {
	params := store.ListOrdersParams{WeekStart: f.Week}
	if f.Status != "" {
		params.Status = &f.Status
	}
	if f.CustomerID != "" {
		params.CustomerID = &f.CustomerID
	}
	rows, err := store.New(s.pool).ListOrders(ctx, params)
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, r := range rows {
		v := summary(r)
		v.CustomerID = r.CustomerID
		out = append(out, v)
	}
	return out, s.outcomes(ctx, out)
}

func (s *Service) Get(ctx context.Context, id string) (*Order, error) {
	o, err := store.New(s.pool).GetOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out, err := s.view(ctx, s.pool, o)
	if err != nil {
		return nil, err
	}
	out.CustomerID = o.CustomerID
	hist, err := store.New(s.pool).OrderHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, h := range hist {
		out.History = append(out.History, StatusChange{From: h.FromStatus, To: h.ToStatus, Reason: h.Reason, CreatedAt: h.CreatedAt})
	}
	return out, nil
}

// DemandLine is how many kits of one meal version a week needs.
type DemandLine struct {
	MealID       string         `json:"meal_id"`
	MealVersion  int            `json:"meal_version"`
	MealName     string         `json:"meal_name"`
	Servings     int            `json:"servings"`
	Quantity     int            `json:"quantity"`
	Orders       int            `json:"orders"`
	OrderNumbers []string       `json:"order_numbers"`
	Allergies    map[string]int `json:"customer_allergies"`
}

// Demand is the finalised demand of a delivery week. It is the interface
// later Chef ERP and production modules read: it counts kits per meal
// version from confirmed orders and does not plan anything.
type Demand struct {
	Week   string       `json:"week"`
	Status string       `json:"status"`
	Lines  []DemandLine `json:"lines"`
	Kits   int          `json:"kits"`
	Orders int          `json:"orders"`
}

func (s *Service) Demand(ctx context.Context, week time.Time, finalised bool) (*Demand, error) {
	rows, err := store.New(s.pool).Demand(ctx, week)
	if err != nil {
		return nil, err
	}
	allergies, err := store.New(s.pool).DemandAllergens(ctx, week)
	if err != nil {
		return nil, err
	}
	byMeal := map[string]map[string]int{}
	for _, a := range allergies {
		if byMeal[a.MealID] == nil {
			byMeal[a.MealID] = map[string]int{}
		}
		byMeal[a.MealID][a.Allergen] = int(a.Orders)
	}
	d := &Demand{Week: week.Format("2006-01-02"), Status: "provisional", Lines: []DemandLine{}}
	if finalised {
		d.Status = "final"
	}
	seen := map[string]bool{}
	for _, r := range rows {
		al := byMeal[r.MealID]
		if al == nil {
			al = map[string]int{}
		}
		d.Lines = append(d.Lines, DemandLine{MealID: r.MealID, MealVersion: int(r.MealVersion), MealName: r.MealName,
			Servings: int(r.Servings), Quantity: int(r.Quantity), Orders: int(r.Orders), OrderNumbers: r.OrderNumbers, Allergies: al})
		d.Kits += int(r.Quantity)
		for _, n := range r.OrderNumbers {
			if !seen[n] {
				seen[n] = true
				d.Orders++
			}
		}
	}
	return d, nil
}

// IDForCycle returns the id of a cycle's order, if it has one.
func (s *Service) IDForCycle(ctx context.Context, q db.Querier, cycleID string) (string, bool) {
	o, err := store.New(q).OrderForCycle(ctx, cycleID)
	if err != nil {
		return "", false
	}
	return o.ID, true
}

// SourceLine is one meal of one order, as production demand snapshots read
// it. Only orders that exist are counted: an order is made at the cutoff
// only for a paid (or promotion-free) week with confirmed meals, so unpaid
// weeks, carts and draft selections never appear here.
type SourceLine struct {
	OrderID           string
	OrderNumber       string
	MealID            string
	MealVersion       int
	MealName          string
	Quantity          int
	Servings          int
	DietaryTags       []string
	CustomerAllergens []string
}

// DemandLines returns every meal line of the week's live orders (not
// pending, not cancelled), in a stable order.
func (s *Service) DemandLines(ctx context.Context, q db.Querier, week time.Time) ([]SourceLine, error) {
	rows, err := q.Query(ctx, `
		SELECT o.id, o.number, i.meal_id, i.meal_version, i.meal_name, i.quantity, i.servings, i.dietary_tags, o.customer_allergens
		FROM order_items i JOIN orders o ON o.id = i.order_id
		WHERE o.week_start = $1 AND o.status NOT IN ('pending', 'cancelled')
		ORDER BY o.number, i.meal_id`, week)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceLine{}
	for rows.Next() {
		var l SourceLine
		var v, qn, sv int32
		if err := rows.Scan(&l.OrderID, &l.OrderNumber, &l.MealID, &v, &l.MealName, &qn, &sv, &l.DietaryTags, &l.CustomerAllergens); err != nil {
			return nil, err
		}
		l.MealVersion, l.Quantity, l.Servings = int(v), int(qn), int(sv)
		out = append(out, l)
	}
	return out, rows.Err()
}

// Advance moves orders to a production status, only from the statuses
// given (so it never moves an order backwards or out of cancelled), and
// records each change in the order's history. It returns how many moved.
func (s *Service) Advance(ctx context.Context, q db.Querier, orderIDs []string, from []string, to, reason, actor string) (int, error) {
	if len(orderIDs) == 0 {
		return 0, nil
	}
	rows, err := q.Query(ctx, `
		WITH moved AS (
			SELECT id, status FROM orders WHERE id = ANY($1::uuid[]) AND status = ANY($2::text[]) FOR UPDATE
		), upd AS (
			UPDATE orders o SET status = $3, updated_at = now() FROM moved WHERE o.id = moved.id RETURNING o.id, moved.status AS from_status
		)
		INSERT INTO order_status_history (order_id, from_status, to_status, reason, actor_user_id)
		SELECT id, from_status, $3, $4, $5::uuid FROM upd RETURNING order_id`, orderIDs, from, to, reason, nilIfEmpty(actor))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}

// FulfillmentFacts is what fulfillment needs to decide whether an order may
// be fulfilled: its status, who it is for, what was paid and the address
// copied when it was made. Fulfillment reads this; it never edits an order.
type FulfillmentFacts struct {
	OrderID       string
	Number        string
	CustomerID    string
	Status        string
	Week          time.Time
	TotalMinor    int64
	PaymentStatus string // "" when the week was free (no payment)
	Address       json.RawMessage
	Kits          int
}

// PaymentEligible: the week was paid and not refunded, or nothing was owed.
func (f FulfillmentFacts) PaymentEligible() (bool, string) {
	switch {
	case f.PaymentStatus == "" && f.TotalMinor == 0:
		return true, ""
	case f.PaymentStatus == "succeeded":
		return true, ""
	case f.PaymentStatus == "":
		return false, "The order has no payment but a total above zero."
	}
	return false, "The order's payment is " + strings.ReplaceAll(f.PaymentStatus, "_", " ") + "."
}

// Facts returns an order's FulfillmentFacts, or httpx.ErrNotFound.
func (s *Service) Facts(ctx context.Context, q db.Querier, orderID string) (*FulfillmentFacts, error) {
	var f FulfillmentFacts
	err := q.QueryRow(ctx, `
		SELECT o.id, o.number, o.customer_id, o.status, o.week_start, o.total_minor, COALESCE(p.status, ''), o.address_snapshot,
		       (SELECT COALESCE(sum(quantity), 0)::int FROM order_items WHERE order_id = o.id)
		FROM orders o LEFT JOIN payments p ON p.id = o.payment_id WHERE o.id = $1`, orderID).
		Scan(&f.OrderID, &f.Number, &f.CustomerID, &f.Status, &f.Week, &f.TotalMinor, &f.PaymentStatus, &f.Address, &f.Kits)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return &f, err
}

// ReadyOrderIDs lists every order at ready_for_fulfillment (the fulfillment
// "sync" action uses it to catch anything a missed event left behind).
func (s *Service) ReadyOrderIDs(ctx context.Context, q db.Querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT id::text FROM orders WHERE status = 'ready_for_fulfillment' ORDER BY week_start, number`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// ---------------------------------------------------------------- delivery outcomes

// DeliveryOutcome is the commerce record of a corrected delivery outcome.
// An order's status and payment history are never rewritten: when delivery
// corrects a "delivered" outcome, commerce records here that the outcome is
// disputed, and records "confirmed" once a later attempt is delivered.
type DeliveryOutcome struct {
	State  string    `json:"state"` // disputed or confirmed
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// FlagDeliveryOutcome appends an outcome entry in q's transaction. It is the
// contract delivery uses; nothing else changes on the order.
func (s *Service) FlagDeliveryOutcome(ctx context.Context, q db.Querier, orderID, state, reason, actor string) error {
	if state != "disputed" && state != "confirmed" {
		return errors.New("orders: outcome must be disputed or confirmed")
	}
	reason = strings.TrimSpace(reason)
	if r := []rune(reason); len(r) > 1000 {
		reason = string(r[:1000])
	}
	_, err := q.Exec(ctx, `INSERT INTO order_delivery_outcomes (order_id, state, reason, actor_user_id) VALUES ($1, $2, $3, $4)`,
		orderID, state, reason, nilIfEmpty(actor))
	return err
}

// OutcomeDisputed reports whether the order's latest outcome entry is a dispute.
func (s *Service) OutcomeDisputed(ctx context.Context, q db.Querier, orderID string) (bool, error) {
	o, err := latestOutcome(ctx, q, orderID)
	return o != nil && o.State == "disputed", err
}

func latestOutcome(ctx context.Context, q db.Querier, orderID string) (*DeliveryOutcome, error) {
	var o DeliveryOutcome
	err := q.QueryRow(ctx, `SELECT state, reason, created_at FROM order_delivery_outcomes WHERE order_id = $1 ORDER BY id DESC LIMIT 1`,
		orderID).Scan(&o.State, &o.Reason, &o.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// outcomes fills each summary's latest outcome state in one query.
func (s *Service) outcomes(ctx context.Context, list []Summary) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]string, len(list))
	for i, o := range list {
		ids[i] = o.ID
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (order_id) order_id::text, state FROM order_delivery_outcomes
		WHERE order_id = ANY($1::uuid[]) ORDER BY order_id, id DESC`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	states := map[string]string{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			return err
		}
		states[id] = st
	}
	for i := range list {
		if st, ok := states[list[i].ID]; ok {
			list[i].DeliveryOutcome = &st
		}
	}
	return rows.Err()
}
