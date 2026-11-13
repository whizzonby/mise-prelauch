// Package culinary owns ingredients, units, chefs and recipes: the
// operational source of truth for what goes into a Mise kit.
//
// A recipe has versions. A version is edited only while it is a draft (or
// has changes requested), goes through a culinary review, a nutrition
// sign-off and a final approval, and is then published for production from
// a given delivery week. Approved versions never change (database triggers
// enforce it); a change is a new revision with no approvals carried over.
//
// Quantities are exact decimals (platform/qty) stored as numeric. Nothing
// here invents ingredients, quantities, prices or nutrition.
package culinary

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Catalog is what culinary asks of the customer catalogue (internal/catalog
// implements it): when a recipe version linked to a meal is published, the
// meal's allergen declaration and verified nutrition are set from it.
type Catalog interface {
	ApplyRecipeClaims(ctx context.Context, q db.Querier, actor string, c MealClaims) error
	MealName(ctx context.Context, q db.Querier, id string) (string, bool, error)
}

// MealClaims are the customer-facing facts taken from an approved version.
type MealClaims struct {
	MealID          string
	Allergens       []string
	NutritionByUser string
	EnergyKcal      int
	ProteinG        string
	CarbsG          string
	FatG            string
	FibreG          *string
	SodiumMg        *int
}

// Weeks gives the business calendar (internal/menus Policy implements it).
type Weeks interface {
	NextOpenWeek(now time.Time) time.Time
	CutoffFor(week time.Time) time.Time
}

type Service struct {
	pool    *pgxpool.Pool
	Catalog Catalog
	Weeks   Weeks
	Now     func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

// Pool is for the production module, which runs culinary reads inside its
// own transactions.
func (s *Service) Pool() *pgxpool.Pool { return s.pool }

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

func conflict(code, msg string) error { return httpx.NewError(http.StatusConflict, code, msg) }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

func runes(s string) int { return len([]rune(s)) }

func notFoundIf(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------- units

// Unit is a unit of measure.
type Unit struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	ToBase    string `json:"to_base"`
}

func (s *Service) Units(ctx context.Context) ([]Unit, error) {
	rows, err := s.pool.Query(ctx, `SELECT code, name, dimension, trim_scale(to_base)::text FROM units_of_measure ORDER BY position, code`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Unit, error) {
		var u Unit
		err := r.Scan(&u.Code, &u.Name, &u.Dimension, &u.ToBase)
		return u, err
	})
}

// Conversion is one ingredient-specific cross-dimension conversion:
// 1 From = Factor To.
type Conversion struct {
	ID          string    `json:"id"`
	From        string    `json:"from_unit"`
	To          string    `json:"to_unit"`
	Factor      string    `json:"factor"`
	Approximate bool      `json:"approximate"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

// Converter converts quantities between units, for one transaction's view
// of the unit table and conversions. Build it with NewConverter.
type Converter struct {
	units map[string]unitInfo
	// conversions[ingredient] lists that ingredient's cross-dimension rules.
	conversions map[string][]Conversion
	q           db.Querier
}

type unitInfo struct {
	dimension string
	toBase    qty.Q
}

// ErrIncompatible means no rule converts between the two units: they measure
// different things and the ingredient has no recorded conversion.
var ErrIncompatible = errors.New("culinary: no conversion between these units")

func NewConverter(ctx context.Context, q db.Querier) (*Converter, error) {
	rows, err := q.Query(ctx, `SELECT code, dimension, trim_scale(to_base)::text FROM units_of_measure`)
	if err != nil {
		return nil, err
	}
	c := &Converter{units: map[string]unitInfo{}, conversions: map[string][]Conversion{}, q: q}
	for rows.Next() {
		var code, dim, base string
		if err := rows.Scan(&code, &dim, &base); err != nil {
			rows.Close()
			return nil, err
		}
		c.units[code] = unitInfo{dimension: dim, toBase: qty.MustParse(base)}
	}
	rows.Close()
	return c, rows.Err()
}

// Dimension returns the unit's dimension, or "" for an unknown unit.
func (c *Converter) Dimension(unit string) string { return c.units[unit].dimension }

func (c *Converter) load(ctx context.Context, ingredientID string) ([]Conversion, error) {
	if list, ok := c.conversions[ingredientID]; ok {
		return list, nil
	}
	rows, err := c.q.Query(ctx, `
		SELECT id, from_unit, to_unit, trim_scale(factor)::text, approximate, source, created_at
		FROM ingredient_unit_conversions WHERE ingredient_id = $1 ORDER BY created_at`, ingredientID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Conversion, error) {
		var v Conversion
		err := r.Scan(&v.ID, &v.From, &v.To, &v.Factor, &v.Approximate, &v.Source, &v.CreatedAt)
		return v, err
	})
	if err != nil {
		return nil, err
	}
	c.conversions[ingredientID] = list
	return list, nil
}

// Convert converts amount of an ingredient from one unit to another.
// Within a dimension it uses the exact unit factors. Across dimensions it
// needs a conversion recorded for that ingredient; approximate reports
// whether that conversion is an average (pieces). Volume is never turned
// into mass, nor pieces into anything, without one.
func (c *Converter) Convert(ctx context.Context, ingredientID string, amount qty.Q, from, to string) (out qty.Q, approximate bool, err error) {
	fu, ok1 := c.units[from]
	tu, ok2 := c.units[to]
	if !ok1 || !ok2 {
		return qty.Q{}, false, ErrIncompatible
	}
	if fu.dimension == tu.dimension {
		return amount.Mul(fu.toBase).Div(tu.toBase), false, nil
	}
	list, err := c.load(ctx, ingredientID)
	if err != nil {
		return qty.Q{}, false, err
	}
	for _, rule := range list {
		ru, rt := c.units[rule.From], c.units[rule.To]
		factor := qty.MustParse(rule.Factor)
		switch {
		case ru.dimension == fu.dimension && rt.dimension == tu.dimension:
			// amount(from) -> rule.From -> ×factor -> rule.To -> to
			inRuleFrom := amount.Mul(fu.toBase).Div(ru.toBase)
			inRuleTo := inRuleFrom.Mul(factor)
			return inRuleTo.Mul(rt.toBase).Div(tu.toBase), rule.Approximate, nil
		case rt.dimension == fu.dimension && ru.dimension == tu.dimension:
			// The rule read backwards.
			inRuleTo := amount.Mul(fu.toBase).Div(rt.toBase)
			inRuleFrom := inRuleTo.Div(factor)
			return inRuleFrom.Mul(ru.toBase).Div(tu.toBase), rule.Approximate, nil
		}
	}
	return qty.Q{}, false, ErrIncompatible
}

// tx runs fn in a transaction.
func (s *Service) tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return db.InTx(ctx, s.pool, fn)
}
