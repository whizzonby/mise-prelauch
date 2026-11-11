package billing

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/billing/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/money"
)

// Promotions are created as drafts and only apply once someone with
// promotions.manage activates them. None exist by default: the business
// plan's early-bird offer is not created until it is approved.

var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,29}$`)

// Promotion is a promotion as staff see it.
type Promotion struct {
	ID             string     `json:"id"`
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	PercentBps     *int32     `json:"percent_bps"`
	AmountMinor    *int64     `json:"amount_minor"`
	Currency       string     `json:"currency"`
	Cycles         int        `json:"cycles"`
	MaxRedemptions *int32     `json:"max_redemptions"`
	Redemptions    int        `json:"redemptions"`
	ValidFrom      *time.Time `json:"valid_from"`
	ValidUntil     *time.Time `json:"valid_until"`
	PlanIDs        []string   `json:"plan_ids"`
	Status         string     `json:"status"`
	ActivatedAt    *time.Time `json:"activated_at"`
	CreatedAt      time.Time  `json:"created_at"`
	Description    string     `json:"description"`
}

func promotionView(p store.Promotion) Promotion {
	v := Promotion{ID: p.ID, Code: p.Code, Name: p.Name, Kind: p.Kind, PercentBps: p.PercentBps, AmountMinor: p.AmountMinor,
		Currency: p.Currency, Cycles: int(p.Cycles), MaxRedemptions: p.MaxRedemptions, Redemptions: int(p.Redemptions),
		ValidFrom: p.ValidFrom, ValidUntil: p.ValidUntil, PlanIDs: p.PlanIds, Status: p.Status, ActivatedAt: p.ActivatedAt,
		CreatedAt: p.CreatedAt}
	v.Description = describe(snapshotOf(p), int(p.Cycles))
	return v
}

func snapshotOf(p store.Promotion) DiscountSnapshot {
	d := DiscountSnapshot{PromotionID: p.ID, Code: p.Code, Name: p.Name, Kind: p.Kind}
	if p.PercentBps != nil {
		d.PercentBps = int64(*p.PercentBps)
	}
	if p.AmountMinor != nil {
		d.AmountMinor = *p.AmountMinor
	}
	return d
}

// describe is "15% off your first 2 weeks".
func describe(d DiscountSnapshot, cycles int) string {
	off := money.Format(d.AmountMinor, "TTD") + " off"
	if d.Kind == "percent" {
		off = percent(d.PercentBps) + " off"
	}
	switch cycles {
	case 1:
		return off + " your first week"
	default:
		return off + " each of your first " + itoa(cycles) + " weeks"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// NormalizeCode upper-cases and trims a code as typed.
func NormalizeCode(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

var errBadCode = httpx.ValidationError([]httpx.FieldError{{Field: "promotion_code", Code: "invalid",
	Message: "This code is not valid."}})

// Applicable is a promotion a customer may use for a plan now.
type Applicable struct {
	ID          string
	Snapshot    DiscountSnapshot
	Cycles      int
	Description string
}

// Check finds an active promotion by code and checks it applies to planID
// for this customer now. It does not use it up.
func (s *Service) Check(ctx context.Context, q db.Querier, code, planID, customerID string) (*Applicable, error) {
	code = NormalizeCode(code)
	if !codePattern.MatchString(code) {
		return nil, errBadCode
	}
	p, err := store.New(q).PromotionByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errBadCode
	}
	if err != nil {
		return nil, err
	}
	now := s.Now()
	fail := func(msg string) error {
		return httpx.ValidationError([]httpx.FieldError{{Field: "promotion_code", Code: "not_applicable", Message: msg}})
	}
	switch {
	case p.Status != "active":
		return nil, errBadCode
	case p.ValidFrom != nil && now.Before(*p.ValidFrom):
		return nil, fail("This code is not active yet.")
	case p.ValidUntil != nil && !now.Before(*p.ValidUntil):
		return nil, fail("This code has expired.")
	case p.MaxRedemptions != nil && p.Redemptions >= *p.MaxRedemptions:
		return nil, fail("This code has been fully used.")
	case len(p.PlanIds) > 0 && !slices.Contains(p.PlanIds, planID):
		return nil, fail("This code does not apply to this plan.")
	case p.Kind == "fixed" && p.Currency != money.Currency:
		return nil, fail("This code does not apply here.")
	}
	used, err := store.New(q).CustomerUsedPromotion(ctx, store.CustomerUsedPromotionParams{PromotionID: p.ID, CustomerID: customerID})
	if err != nil {
		return nil, err
	}
	if used {
		return nil, fail("You have already used this code.")
	}
	return &Applicable{ID: p.ID, Snapshot: snapshotOf(p), Cycles: int(p.Cycles), Description: describe(snapshotOf(p), int(p.Cycles))}, nil
}

// Reserve counts one use of the promotion for a new subscription. It fails
// if the last use was taken in the meantime.
func (s *Service) Reserve(ctx context.Context, q db.Querier, a *Applicable, subscriptionID, customerID string) error {
	if _, err := store.New(q).TakeRedemption(ctx, a.ID); errors.Is(err, pgx.ErrNoRows) {
		return httpx.ValidationError([]httpx.FieldError{{Field: "promotion_code", Code: "not_applicable", Message: "This code has been fully used."}})
	} else if err != nil {
		return err
	}
	_, err := store.New(q).CreateRedemption(ctx, store.CreateRedemptionParams{PromotionID: a.ID, SubscriptionID: subscriptionID,
		CustomerID: customerID, CyclesTotal: int16(a.Cycles)})
	if db.IsUniqueViolation(err, "promotion_redemptions_once") {
		return httpx.ValidationError([]httpx.FieldError{{Field: "promotion_code", Code: "not_applicable", Message: "You have already used this code."}})
	}
	return err
}

// Release gives back a reserved use when a subscription never started.
func (s *Service) Release(ctx context.Context, q db.Querier, subscriptionID string) error {
	promoID, err := store.New(q).ReleaseRedemption(ctx, subscriptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return store.New(q).GiveBackRedemption(ctx, promoID)
}

// discountFor is the discount a subscription's next billing cycle gets.
func (s *Service) discountFor(ctx context.Context, q db.Querier, subscriptionID string) (*DiscountSnapshot, error) {
	r, err := store.New(q).RedemptionForSubscription(ctx, subscriptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.Status == "released" || r.CyclesUsed >= r.CyclesTotal {
		return nil, nil
	}
	d := &DiscountSnapshot{PromotionID: r.PromotionID, Code: r.Code, Name: r.Name, Kind: r.Kind}
	if r.PercentBps != nil {
		d.PercentBps = int64(*r.PercentBps)
	}
	if r.AmountMinor != nil {
		d.AmountMinor = *r.AmountMinor
	}
	return d, nil
}

// ---------------------------------------------------------------- admin

func (s *Service) Promotions(ctx context.Context) ([]Promotion, error) {
	rows, err := store.New(s.pool).ListPromotions(ctx)
	if err != nil {
		return nil, err
	}
	out := []Promotion{}
	for _, r := range rows {
		out = append(out, promotionView(r))
	}
	return out, nil
}

// PromotionInput is a new promotion.
type PromotionInput struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	PercentBps     *int32     `json:"percent_bps"`
	AmountMinor    *int64     `json:"amount_minor"`
	Cycles         int        `json:"cycles"`
	MaxRedemptions *int32     `json:"max_redemptions"`
	ValidFrom      *time.Time `json:"valid_from"`
	ValidUntil     *time.Time `json:"valid_until"`
	PlanIDs        []string   `json:"plan_ids"`
}

func (s *Service) CreatePromotion(ctx context.Context, actor string, in PromotionInput) (*Promotion, error) {
	var f []httpx.FieldError
	add := func(field, code, msg string) { f = append(f, httpx.FieldError{Field: field, Code: code, Message: msg}) }
	in.Code = NormalizeCode(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	if !codePattern.MatchString(in.Code) {
		add("code", "format", "Use 3 to 30 capital letters, numbers and hyphens.")
	}
	if n := len([]rune(in.Name)); n < 1 || n > 80 {
		add("name", "length", "Give the promotion a name of up to 80 characters.")
	}
	switch in.Kind {
	case "percent":
		in.AmountMinor = nil
		if in.PercentBps == nil || *in.PercentBps < 1 || *in.PercentBps > 10000 {
			add("percent_bps", "range", "A percentage must be between 0.01% and 100%.")
		}
	case "fixed":
		in.PercentBps = nil
		if in.AmountMinor == nil || *in.AmountMinor < 1 {
			add("amount_minor", "range", "Enter an amount above zero.")
		}
	default:
		add("kind", "invalid", "Choose a percentage or a fixed amount.")
	}
	if in.Cycles == 0 {
		in.Cycles = 1
	}
	if in.Cycles < 1 || in.Cycles > 52 {
		add("cycles", "range", "A promotion can apply to 1 to 52 weeks.")
	}
	if in.MaxRedemptions != nil && *in.MaxRedemptions < 1 {
		add("max_redemptions", "range", "The limit must be at least 1.")
	}
	if in.ValidFrom != nil && in.ValidUntil != nil && !in.ValidUntil.After(*in.ValidFrom) {
		add("valid_until", "order", "The end must be after the start.")
	}
	if in.PlanIDs == nil {
		in.PlanIDs = []string{}
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	var out Promotion
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		p, err := store.New(tx).CreatePromotion(ctx, store.CreatePromotionParams{
			Code: in.Code, Name: in.Name, Kind: in.Kind, PercentBps: in.PercentBps, AmountMinor: in.AmountMinor,
			Currency: money.Currency, Cycles: int16(in.Cycles), MaxRedemptions: in.MaxRedemptions,
			ValidFrom: in.ValidFrom, ValidUntil: in.ValidUntil, PlanIds: in.PlanIDs, Actor: nilIfEmpty(actor),
		})
		if db.IsUniqueViolation(err, "promotions_code_key") {
			return httpx.ValidationError([]httpx.FieldError{{Field: "code", Code: "taken", Message: "This code is already in use."}})
		}
		if err != nil {
			return err
		}
		out = promotionView(p)
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "promotion.created", TargetType: "promotion", TargetID: p.ID,
			Metadata: map[string]any{"code": p.Code}})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SetPromotionStatus activates or archives a promotion.
func (s *Service) SetPromotionStatus(ctx context.Context, actor, id, status string) (*Promotion, error) {
	var out Promotion
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		cur, err := store.New(tx).GetPromotion(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Status == "archived" {
			return httpx.NewError(http.StatusConflict, "archived", "An archived promotion cannot be changed.")
		}
		p, err := store.New(tx).SetPromotionStatus(ctx, store.SetPromotionStatusParams{ID: id, Status: status, Actor: nilIfEmpty(actor)})
		if err != nil {
			return err
		}
		out = promotionView(p)
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "promotion." + status, TargetType: "promotion", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
