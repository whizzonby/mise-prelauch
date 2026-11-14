package suppliers

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
)

// Product is a supplier's offering, mapped to a catalogue ingredient (or a
// packaging item), sold in packs.
type Product struct {
	ID                  string     `json:"id"`
	SupplierID          string     `json:"supplier_id"`
	SupplierName        string     `json:"supplier_name"`
	SupplierStatus      string     `json:"supplier_status"`
	SupplierQualified   bool       `json:"supplier_qualified"`
	Kind                string     `json:"kind"`
	IngredientID        *string    `json:"ingredient_id"`
	IngredientName      *string    `json:"ingredient_name"`
	BaseUnit            *string    `json:"base_unit"`
	SKU                 string     `json:"sku"`
	Name                string     `json:"name"`
	Description         string     `json:"description"`
	Grade               string     `json:"grade"`
	Specification       string     `json:"specification"`
	PackQuantity        string     `json:"pack_quantity"`
	PackUnit            string     `json:"pack_unit"`
	PackDescription     string     `json:"pack_description"`
	MinOrderPacks       string     `json:"min_order_packs"`
	LeadTimeDays        *int       `json:"lead_time_days"`
	Availability        string     `json:"availability"`
	DeliveryConstraints string     `json:"delivery_constraints"`
	MappingStatus       string     `json:"mapping_status"`
	MappingNote         string     `json:"mapping_note"`
	MappingReviewerName *string    `json:"mapping_reviewer_name"`
	MappingReviewedAt   *time.Time `json:"mapping_reviewed_at"`
	Active              bool       `json:"active"`
	// The pack in the ingredient's base unit (NULL for packaging).
	PackBase        *string `json:"pack_base"`
	PackApproximate bool    `json:"pack_approximate"`
	// The price in force today, per pack.
	Price     *Price    `json:"price"`
	Prices    []Price   `json:"prices,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Price is one price record, per pack.
type Price struct {
	ID             string    `json:"id"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	ValidFrom      string    `json:"valid_from"`
	ValidUntil     *string   `json:"valid_until"`
	Origin         string    `json:"origin"`
	QuotationID    *string   `json:"quotation_id"`
	Note           string    `json:"note"`
	RecordedByName string    `json:"recorded_by_name"`
	CreatedAt      time.Time `json:"created_at"`
}

const productCols = `p.id, p.supplier_id, s.name, s.status, s.review_expires_on::text, p.kind, p.ingredient_id, i.name, i.base_unit,
	p.sku, p.name, p.description, p.grade, p.specification, trim_scale(p.pack_quantity)::text, p.pack_unit, p.pack_description,
	trim_scale(p.min_order_packs)::text, p.lead_time_days, p.availability, p.delivery_constraints, p.mapping_status, p.mapping_note,
	CASE WHEN p.mapping_reviewed_by IS NULL THEN NULL ELSE COALESCE(btrim(u.first_name || ' ' || u.last_name), '') END,
	p.mapping_reviewed_at, p.active, p.created_at, p.updated_at`

const productFrom = ` FROM supplier_products p JOIN suppliers s ON s.id = p.supplier_id LEFT JOIN ingredients i ON i.id = p.ingredient_id
	LEFT JOIN users u ON u.id = p.mapping_reviewed_by`

func (s *Service) scanProduct(r pgx.Row) (Product, error) {
	var p Product
	var expires *string
	var lead *int16
	err := r.Scan(&p.ID, &p.SupplierID, &p.SupplierName, &p.SupplierStatus, &expires, &p.Kind, &p.IngredientID, &p.IngredientName, &p.BaseUnit,
		&p.SKU, &p.Name, &p.Description, &p.Grade, &p.Specification, &p.PackQuantity, &p.PackUnit, &p.PackDescription,
		&p.MinOrderPacks, &lead, &p.Availability, &p.DeliveryConstraints, &p.MappingStatus, &p.MappingNote,
		&p.MappingReviewerName, &p.MappingReviewedAt, &p.Active, &p.CreatedAt, &p.UpdatedAt)
	if lead != nil {
		n := int(*lead)
		p.LeadTimeDays = &n
	}
	p.SupplierQualified = s.qualified(p.SupplierStatus, expires)
	return p, err
}

// decorate adds the pack in base units and the price in force.
func (s *Service) decorate(ctx context.Context, q db.Querier, items []Product, withHistory bool) error {
	conv, err := culinary.NewConverter(ctx, q)
	if err != nil {
		return err
	}
	for i := range items {
		p := &items[i]
		if p.IngredientID != nil && p.BaseUnit != nil {
			amount := qty.MustParse(p.PackQuantity)
			if base, approx, err := conv.Convert(ctx, *p.IngredientID, amount, p.PackUnit, *p.BaseUnit); err == nil {
				str := base.String()
				p.PackBase, p.PackApproximate = &str, approx
			}
		}
		prices, err := s.prices(ctx, q, p.ID)
		if err != nil {
			return err
		}
		today := s.today().Format(time.DateOnly)
		for j := range prices {
			pr := prices[j]
			if pr.ValidFrom <= today && (pr.ValidUntil == nil || *pr.ValidUntil >= today) {
				p.Price = &pr
				break
			}
		}
		if withHistory {
			p.Prices = prices
		}
	}
	return nil
}

func (s *Service) prices(ctx context.Context, q db.Querier, productID string) ([]Price, error) {
	rows, err := q.Query(ctx, `SELECT pr.id, pr.amount_minor, pr.currency, pr.valid_from::text, pr.valid_until::text, pr.origin, pr.quotation_id::text,
		pr.note, COALESCE(btrim(u.first_name || ' ' || u.last_name), ''), pr.created_at
		FROM supplier_prices pr LEFT JOIN users u ON u.id = pr.recorded_by WHERE pr.product_id = $1
		ORDER BY pr.valid_from DESC, pr.created_at DESC`, productID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Price])
}

// ProductFilter narrows the product list.
type ProductFilter struct {
	SupplierID, IngredientID, Mapping, Query string
	// Purchasable: confirmed, active products of qualified suppliers.
	Purchasable bool
}

func (s *Service) Products(ctx context.Context, f ProductFilter) ([]Product, error) {
	return s.products(ctx, s.pool, f)
}

// ProductsIn lists products in the caller's transaction (procurement's options).
func (s *Service) ProductsIn(ctx context.Context, q db.Querier, f ProductFilter) ([]Product, error) {
	return s.products(ctx, q, f)
}

func (s *Service) products(ctx context.Context, q db.Querier, f ProductFilter) ([]Product, error) {
	rows, err := q.Query(ctx, `SELECT `+productCols+productFrom+`
		WHERE ($1 = '' OR p.supplier_id::text = $1) AND ($2 = '' OR p.ingredient_id::text = $2) AND ($3 = '' OR p.mapping_status = $3)
		AND ($4 = '' OR p.name ILIKE '%' || $4 || '%' OR p.sku ILIKE $4 || '%' OR i.name ILIKE '%' || $4 || '%' OR s.name ILIKE '%' || $4 || '%')
		AND (NOT $5 OR (p.active AND p.mapping_status = 'confirmed' AND s.status = 'approved'))
		ORDER BY i.name NULLS LAST, s.name, p.name LIMIT 500`,
		f.SupplierID, f.IngredientID, f.Mapping, strings.TrimSpace(f.Query), f.Purchasable)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Product, error) { return s.scanProduct(r) })
	if err != nil {
		return nil, err
	}
	if f.Purchasable {
		// An approval that has lapsed is not an approval.
		items = slices.DeleteFunc(items, func(p Product) bool { return !p.SupplierQualified })
	}
	return items, s.decorate(ctx, q, items, false)
}

// Product returns one product with its full price history.
func (s *Service) Product(ctx context.Context, id string) (*Product, error) {
	return s.product(ctx, s.pool, id)
}

// ProductIn reads a product in the caller's transaction (procurement uses it
// when it prices a purchase order line).
func (s *Service) ProductIn(ctx context.Context, q db.Querier, id string) (*Product, error) {
	return s.product(ctx, q, id)
}

func (s *Service) product(ctx context.Context, q db.Querier, id string) (*Product, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	p, err := s.scanProduct(q.QueryRow(ctx, `SELECT `+productCols+productFrom+` WHERE p.id = $1`, id))
	if err != nil {
		return nil, notFoundIf(err)
	}
	items := []Product{p}
	if err := s.decorate(ctx, q, items, true); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// ProductInput adds or edits a product.
type ProductInput struct {
	Kind                *string `json:"kind"`
	IngredientID        *string `json:"ingredient_id"`
	SKU                 *string `json:"sku"`
	Name                *string `json:"name"`
	Description         *string `json:"description"`
	Grade               *string `json:"grade"`
	Specification       *string `json:"specification"`
	PackQuantity        *string `json:"pack_quantity"`
	PackUnit            *string `json:"pack_unit"`
	PackDescription     *string `json:"pack_description"`
	MinOrderPacks       *string `json:"min_order_packs"`
	LeadTimeDays        *int    `json:"lead_time_days"`
	Availability        *string `json:"availability"`
	DeliveryConstraints *string `json:"delivery_constraints"`
	Active              *bool   `json:"active"`
}

var errIncompatible = unprocessable("incompatible_mapping",
	"This pack cannot be converted to the ingredient's base unit. Use a unit of the same kind (mass or volume), or ask the culinary team to record a conversion for the ingredient.")

// checkMapping verifies a pack converts to the ingredient's base unit.
func checkMapping(ctx context.Context, q db.Querier, ingredientID, packQty, packUnit string) error {
	var base string
	var active bool
	if err := q.QueryRow(ctx, `SELECT base_unit, active FROM ingredients WHERE id = $1`, ingredientID).Scan(&base, &active); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fieldErr("ingredient_id", "Ingredient not found.")
		}
		return err
	}
	if !active {
		return fieldErr("ingredient_id", "That ingredient is inactive.")
	}
	conv, err := culinary.NewConverter(ctx, q)
	if err != nil {
		return err
	}
	if _, _, err := conv.Convert(ctx, ingredientID, qty.MustParse(packQty), packUnit, base); err != nil {
		if errors.Is(err, culinary.ErrIncompatible) {
			return errIncompatible
		}
		return err
	}
	return nil
}

func (s *Service) CreateProduct(ctx context.Context, actor, supplierID string, in ProductInput) (*Product, error) {
	var id string
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.lockSupplier(ctx, tx, supplierID); err != nil {
			return err
		}
		if in.Kind == nil {
			k := "ingredient"
			in.Kind = &k
		}
		if in.PackQuantity == nil || in.PackUnit == nil || in.Name == nil {
			return httpx.ValidationError([]httpx.FieldError{{Field: "name", Code: "invalid", Message: "Name the product and its pack size."}})
		}
		if err := s.validateProduct(ctx, tx, &in, true); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO supplier_products (supplier_id, kind, ingredient_id, name, pack_quantity, pack_unit, created_by)
			VALUES ($1, $2, $3, $4, $5::numeric, $6, $7) RETURNING id`,
			supplierID, *in.Kind, in.IngredientID, *in.Name, *in.PackQuantity, *in.PackUnit, nilIfEmpty(actor)).Scan(&id); err != nil {
			return err
		}
		in.Kind, in.IngredientID, in.Name, in.PackQuantity, in.PackUnit = nil, nil, nil, nil, nil
		if err := s.applyProduct(ctx, tx, id, in); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier_product.created", TargetType: "supplier_product", TargetID: id,
			Metadata: map[string]any{"supplier_id": supplierID}})
	})
	if err != nil {
		return nil, err
	}
	return s.Product(ctx, id)
}

// UpdateProduct edits a product. Changing what it maps to (the ingredient,
// the pack) sends the mapping back for review.
func (s *Service) UpdateProduct(ctx context.Context, actor, id string, in ProductInput) (*Product, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var kind, packQty, packUnit string
		var ingredient *string
		if err := tx.QueryRow(ctx, `SELECT kind, ingredient_id::text, trim_scale(pack_quantity)::text, pack_unit FROM supplier_products WHERE id = $1 FOR UPDATE`, id).
			Scan(&kind, &ingredient, &packQty, &packUnit); err != nil {
			return notFoundIf(err)
		}
		if in.Kind == nil {
			in.Kind = &kind
		}
		remap := (in.IngredientID != nil && (ingredient == nil || *in.IngredientID != *ingredient)) ||
			(in.PackQuantity != nil && *in.PackQuantity != packQty) || (in.PackUnit != nil && *in.PackUnit != packUnit) || *in.Kind != kind
		if in.IngredientID == nil {
			in.IngredientID = ingredient
		}
		if in.PackQuantity == nil {
			in.PackQuantity = &packQty
		}
		if in.PackUnit == nil {
			in.PackUnit = &packUnit
		}
		if err := s.validateProduct(ctx, tx, &in, false); err != nil {
			return err
		}
		if err := s.applyProduct(ctx, tx, id, in); err != nil {
			return err
		}
		if remap {
			if _, err := tx.Exec(ctx, `UPDATE supplier_products SET kind = $2, ingredient_id = $3, pack_quantity = $4::numeric, pack_unit = $5,
				mapping_status = 'proposed', mapping_note = 'Changed after review; needs a new review.', mapping_reviewed_by = NULL, mapping_reviewed_at = NULL
				WHERE id = $1`, id, *in.Kind, in.IngredientID, *in.PackQuantity, *in.PackUnit); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier_product.updated", TargetType: "supplier_product", TargetID: id,
			Metadata: map[string]any{"remapped": remap}})
	})
	if err != nil {
		return nil, err
	}
	return s.Product(ctx, id)
}

func (s *Service) validateProduct(ctx context.Context, q db.Querier, in *ProductInput, create bool) error {
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	text := func(p *string, field string, max int) {
		if p == nil {
			return
		}
		*p = strings.TrimSpace(*p)
		if runes(*p) > max {
			bad(field, "Too long.")
		}
	}
	if *in.Kind != "ingredient" && *in.Kind != "packaging" {
		bad("kind", "An ingredient or a packaging item.")
	}
	if *in.Kind == "packaging" {
		in.IngredientID = nil
	} else if in.IngredientID == nil || !uuidPattern.MatchString(*in.IngredientID) {
		bad("ingredient_id", "Choose the catalogue ingredient this product is.")
	}
	if in.Name != nil {
		*in.Name = clean(*in.Name)
		if n := runes(*in.Name); n < 1 || n > 160 {
			bad("name", "Name the product as the supplier calls it.")
		}
	}
	text(in.SKU, "sku", 60)
	text(in.Description, "description", 1000)
	text(in.Grade, "grade", 120)
	text(in.Specification, "specification", 1000)
	text(in.PackDescription, "pack_description", 120)
	text(in.DeliveryConstraints, "delivery_constraints", 500)
	if in.PackQuantity != nil {
		if p, err := qty.Parse(*in.PackQuantity); err != nil || p.Sign() <= 0 {
			bad("pack_quantity", "Enter how much is in one pack.")
		} else {
			v := p.String()
			in.PackQuantity = &v
		}
	}
	if in.MinOrderPacks != nil {
		if p, err := qty.Parse(*in.MinOrderPacks); err != nil || p.Sign() <= 0 {
			bad("min_order_packs", "Enter the minimum number of packs (1 if none).")
		}
	}
	if in.LeadTimeDays != nil && (*in.LeadTimeDays < 0 || *in.LeadTimeDays > 365) {
		bad("lead_time_days", "Between 0 and 365 days.")
	}
	if in.Availability != nil && !slices.Contains([]string{"unknown", "available", "limited", "seasonal", "unavailable"}, *in.Availability) {
		bad("availability", "Choose from the list.")
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM units_of_measure WHERE code = $1)`, *in.PackUnit).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fieldErr("pack_unit", "Choose a unit.")
	}
	if *in.Kind == "ingredient" {
		return checkMapping(ctx, q, *in.IngredientID, *in.PackQuantity, *in.PackUnit)
	}
	return nil
}

func (s *Service) applyProduct(ctx context.Context, tx pgx.Tx, id string, in ProductInput) error {
	var lead any
	if in.LeadTimeDays != nil {
		lead = *in.LeadTimeDays
	}
	_, err := tx.Exec(ctx, `UPDATE supplier_products SET
		name = COALESCE($2, name), sku = COALESCE($3, sku), description = COALESCE($4, description), grade = COALESCE($5, grade),
		specification = COALESCE($6, specification), pack_description = COALESCE($7, pack_description),
		min_order_packs = COALESCE($8::numeric, min_order_packs), lead_time_days = CASE WHEN $9::boolean THEN $10::smallint ELSE lead_time_days END,
		availability = COALESCE($11, availability), delivery_constraints = COALESCE($12, delivery_constraints), active = COALESCE($13, active),
		updated_at = now() WHERE id = $1`,
		id, in.Name, in.SKU, in.Description, in.Grade, in.Specification, in.PackDescription, in.MinOrderPacks,
		in.LeadTimeDays != nil, lead, in.Availability, in.DeliveryConstraints, in.Active)
	if db.IsUniqueViolation(err, "") {
		return conflict("duplicate_sku", "This supplier already has a product with that SKU.")
	}
	return err
}

// MappingInput confirms or rejects a product's ingredient mapping.
type MappingInput struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

// ReviewMapping records that someone checked the product really is the
// ingredient (same item, not merely similar). Rejected products are never
// purchasing options.
func (s *Service) ReviewMapping(ctx context.Context, actor, id string, in MappingInput) (*Product, error) {
	in.Note = strings.TrimSpace(in.Note)
	if in.Decision != "confirmed" && in.Decision != "rejected" {
		return nil, fieldErr("decision", "Confirm or reject the mapping.")
	}
	if in.Decision == "rejected" && in.Note == "" {
		return nil, fieldErr("note", "Say why it is not the same ingredient.")
	}
	if runes(in.Note) > 500 {
		return nil, fieldErr("note", "At most 500 characters.")
	}
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var kind, packQty, packUnit string
		var ingredient *string
		if err := tx.QueryRow(ctx, `SELECT kind, ingredient_id::text, trim_scale(pack_quantity)::text, pack_unit FROM supplier_products WHERE id = $1 FOR UPDATE`, id).
			Scan(&kind, &ingredient, &packQty, &packUnit); err != nil {
			return notFoundIf(err)
		}
		if in.Decision == "confirmed" && kind == "ingredient" {
			if err := checkMapping(ctx, tx, *ingredient, packQty, packUnit); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE supplier_products SET mapping_status = $2, mapping_note = $3, mapping_reviewed_by = $4,
			mapping_reviewed_at = now(), updated_at = now() WHERE id = $1`, id, in.Decision, in.Note, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier_product.mapping_" + in.Decision, TargetType: "supplier_product", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Product(ctx, id)
}

// PriceInput records a price, per pack.
type PriceInput struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	ValidFrom   string `json:"valid_from"`
	ValidUntil  string `json:"valid_until"`
	Origin      string `json:"origin"`
	Note        string `json:"note"`
}

// AddPrice records a new price. Earlier prices are kept: purchase orders
// and receipts keep the price they used.
func (s *Service) AddPrice(ctx context.Context, actor, id string, in PriceInput) (*Product, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		return RecordPrice(ctx, tx, actor, id, in, "")
	})
	if err != nil {
		return nil, err
	}
	return s.Product(ctx, id)
}

// RecordPrice writes a price in the caller's transaction, and the matching
// supply cost on the ingredient (origin supplier_quote). quotationID links a
// price taken from a recorded quotation.
func RecordPrice(ctx context.Context, q db.Querier, actor, productID string, in PriceInput, quotationID string) error {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Note = strings.TrimSpace(in.Note)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if in.AmountMinor < 0 || in.AmountMinor > 10_000_000_000 {
		bad("amount_minor", "Enter the price in cents.")
	}
	if len(in.Currency) != 3 {
		bad("currency", "Enter a three-letter currency code.")
	}
	from, err := time.Parse(time.DateOnly, in.ValidFrom)
	if err != nil {
		bad("valid_from", "Enter the date the price applies from.")
	}
	var until *time.Time
	if in.ValidUntil != "" {
		d, err := time.Parse(time.DateOnly, in.ValidUntil)
		if err != nil || d.Before(from) {
			bad("valid_until", "The end date must be on or after the start date.")
		}
		until = &d
	}
	if in.Origin == "" {
		in.Origin = "price_list"
	}
	if !slices.Contains([]string{"price_list", "quotation", "verbal", "other"}, in.Origin) {
		bad("origin", "Say where the price comes from.")
	}
	if runes(in.Note) > 300 {
		bad("note", "At most 300 characters.")
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	var ingredient *string
	var packQty, packUnit, supplier string
	if err := q.QueryRow(ctx, `SELECT p.ingredient_id::text, trim_scale(p.pack_quantity)::text, p.pack_unit, s.name
		FROM supplier_products p JOIN suppliers s ON s.id = p.supplier_id WHERE p.id = $1`, productID).Scan(&ingredient, &packQty, &packUnit, &supplier); err != nil {
		return notFoundIf(err)
	}
	var priceID string
	if err := q.QueryRow(ctx, `INSERT INTO supplier_prices (product_id, amount_minor, currency, valid_from, valid_until, origin, quotation_id, note, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		productID, in.AmountMinor, in.Currency, from, until, in.Origin, nilIfEmpty(quotationID), in.Note, nilIfEmpty(actor)).Scan(&priceID); err != nil {
		return err
	}
	if ingredient != nil {
		if err := culinary.RecordSupplyCost(ctx, q, actor, culinary.SupplyCost{IngredientID: *ingredient, AmountMinor: in.AmountMinor, Currency: in.Currency,
			PerQuantity: qty.MustParse(packQty), Unit: packUnit, Origin: "supplier_quote", OriginID: priceID,
			Source: "Supplier price from " + supplier + " (" + strings.ReplaceAll(in.Origin, "_", " ") + ")", EffectiveFrom: from}); err != nil {
			return err
		}
	}
	return audit.Record(ctx, q, audit.Entry{ActorUserID: actor, Action: "supplier_product.price_recorded", TargetType: "supplier_product", TargetID: productID,
		Metadata: map[string]any{"amount_minor": in.AmountMinor, "currency": in.Currency, "origin": in.Origin}})
}

// ---------------------------------------------------------------- availability

// Availability is what a supplier says they could supply. Not stock.
type Availability struct {
	ID                string    `json:"id"`
	SupplierID        string    `json:"supplier_id"`
	SupplierName      string    `json:"supplier_name"`
	IngredientID      string    `json:"ingredient_id"`
	IngredientName    string    `json:"ingredient_name"`
	Status            string    `json:"status"`
	EstimatedQuantity *string   `json:"estimated_quantity"`
	Unit              *string   `json:"unit"`
	AvailableFrom     *string   `json:"available_from"`
	AvailableUntil    *string   `json:"available_until"`
	Confidence        string    `json:"confidence"`
	Source            string    `json:"source"`
	ConfirmedAt       time.Time `json:"confirmed_at"`
	RecordedByName    string    `json:"recorded_by_name"`
}

// AvailabilityFilter narrows the list; Current keeps the latest record per
// supplier and ingredient whose window has not ended.
type AvailabilityFilter struct {
	SupplierID, IngredientID string
	Current                  bool
}

func (s *Service) Availability(ctx context.Context, f AvailabilityFilter) ([]Availability, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT * FROM (
			SELECT DISTINCT ON (CASE WHEN $3 THEN a.supplier_id::text || a.ingredient_id::text ELSE a.id::text END)
				a.id, a.supplier_id, s.name, a.ingredient_id, i.name, a.status, trim_scale(a.estimated_quantity)::text, a.unit,
				a.available_from::text, a.available_until::text, a.confidence, a.source, a.confirmed_at,
				COALESCE(btrim(u.first_name || ' ' || u.last_name), '')
			FROM supplier_availability a JOIN suppliers s ON s.id = a.supplier_id JOIN ingredients i ON i.id = a.ingredient_id
			LEFT JOIN users u ON u.id = a.recorded_by
			WHERE ($1 = '' OR a.supplier_id::text = $1) AND ($2 = '' OR a.ingredient_id::text = $2)
			ORDER BY CASE WHEN $3 THEN a.supplier_id::text || a.ingredient_id::text ELSE a.id::text END, a.confirmed_at DESC
		) x WHERE NOT $3 OR x.available_until IS NULL OR x.available_until::date >= $4
		ORDER BY x.confirmed_at DESC LIMIT 500`, f.SupplierID, f.IngredientID, f.Current, s.today())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Availability])
}

// AvailabilityInput records availability as the supplier described it.
type AvailabilityInput struct {
	SupplierID        string `json:"supplier_id"`
	IngredientID      string `json:"ingredient_id"`
	Status            string `json:"status"`
	EstimatedQuantity string `json:"estimated_quantity"`
	Unit              string `json:"unit"`
	AvailableFrom     string `json:"available_from"`
	AvailableUntil    string `json:"available_until"`
	Confidence        string `json:"confidence"`
	Source            string `json:"source"`
}

func (s *Service) RecordAvailability(ctx context.Context, actor string, in AvailabilityInput) ([]Availability, error) {
	in.Source = strings.TrimSpace(in.Source)
	var f []httpx.FieldError
	bad := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	if !uuidPattern.MatchString(in.SupplierID) {
		bad("supplier_id", "Choose the supplier.")
	}
	if !uuidPattern.MatchString(in.IngredientID) {
		bad("ingredient_id", "Choose the ingredient.")
	}
	if !slices.Contains([]string{"available_now", "expected_harvest", "limited", "unavailable", "seasonal"}, in.Status) {
		bad("status", "Choose the availability.")
	}
	if !slices.Contains([]string{"confirmed_by_supplier", "supplier_estimate", "staff_estimate"}, in.Confidence) {
		bad("confidence", "Say how sure this is.")
	}
	var est, unit *string
	if in.EstimatedQuantity != "" {
		if p, err := qty.Parse(in.EstimatedQuantity); err != nil || p.Sign() <= 0 {
			bad("estimated_quantity", "Enter a quantity, or leave it blank.")
		} else {
			v := p.String()
			est = &v
		}
		if in.Unit == "" {
			bad("unit", "Give the unit of the estimate.")
		}
		unit = &in.Unit
	}
	date := func(v, field string) *time.Time {
		if v == "" {
			return nil
		}
		d, err := time.Parse(time.DateOnly, v)
		if err != nil {
			bad(field, "Enter a date, or leave it blank.")
			return nil
		}
		return &d
	}
	from, until := date(in.AvailableFrom, "available_from"), date(in.AvailableUntil, "available_until")
	if from != nil && until != nil && until.Before(*from) {
		bad("available_until", "The end must be on or after the start.")
	}
	if runes(in.Source) > 300 {
		bad("source", "At most 300 characters.")
	}
	if len(f) > 0 {
		return nil, httpx.ValidationError(f)
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := s.lockSupplier(ctx, tx, in.SupplierID); err != nil {
			return err
		}
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO supplier_availability (supplier_id, ingredient_id, status, estimated_quantity, unit, available_from,
			available_until, confidence, source, recorded_by) VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, $8, $9, $10) RETURNING id`,
			in.SupplierID, in.IngredientID, in.Status, est, unit, from, until, in.Confidence, in.Source, nilIfEmpty(actor)).Scan(&id)
		if db.IsForeignKeyViolation(err) {
			return fieldErr("ingredient_id", "Ingredient or unit not found.")
		}
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "supplier.availability_recorded", TargetType: "supplier", TargetID: in.SupplierID,
			Metadata: map[string]any{"ingredient_id": in.IngredientID, "status": in.Status}})
	})
	if err != nil {
		return nil, err
	}
	return s.Availability(ctx, AvailabilityFilter{SupplierID: in.SupplierID, Current: true})
}
