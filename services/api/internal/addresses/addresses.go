// Package addresses keeps a customer's delivery addresses. Trinidad and
// Tobago is the first market, so regions come from its municipal
// corporations and Tobago. An address is never a promise of delivery: each
// one carries its delivery availability, which is "unknown" until
// operations configures delivery zones.
package addresses

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/addresses/store"
	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/customers"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

const maxAddresses = 10

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	errTooMany   = httpx.NewError(http.StatusConflict, "too_many_addresses", "You can save up to 10 addresses. Remove one to add another.")
	controlChars = regexp.MustCompile(`[\x00-\x1f\x7f<>]`)
)

// Customers resolves the signed-in customer's id.
type Customers interface {
	CustomerID(ctx context.Context, p *identity.Principal) (string, error)
}

type Handler struct {
	Pool      *pgxpool.Pool
	Identity  *identity.Service
	Customers Customers
	Delivery  delivery.Eligibility
}

func (h *Handler) Routes(mux httpx.Router) {
	req := h.Identity.RequireCustomer
	mux.Handle("GET /api/v1/me/addresses", req(h.list))
	mux.Handle("POST /api/v1/me/addresses", req(h.create))
	mux.Handle("PATCH /api/v1/me/addresses/{id}", req(h.update))
	mux.Handle("DELETE /api/v1/me/addresses/{id}", req(h.remove))
}

type Address struct {
	ID                   string                `json:"id"`
	Label                *string               `json:"label"`
	RecipientName        string                `json:"recipient_name"`
	Phone                *string               `json:"phone"`
	Line1                string                `json:"line1"`
	Line2                *string               `json:"line2"`
	Area                 *string               `json:"area"`
	City                 string                `json:"city"`
	Region               string                `json:"region"`
	RegionLabel          string                `json:"region_label"`
	Country              string                `json:"country"`
	Instructions         *string               `json:"instructions"`
	IsDefault            bool                  `json:"is_default"`
	DeliveryAvailability delivery.Availability `json:"delivery_availability"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

func (h *Handler) view(ctx context.Context, a store.CustomerAddress) (Address, error) {
	avail, err := h.Delivery.Check(ctx, a.Country, a.Region)
	return Address{ID: a.ID, Label: a.Label, RecipientName: a.RecipientName, Phone: a.Phone, Line1: a.Line1, Line2: a.Line2,
		Area: a.Area, City: a.City, Region: a.Region, RegionLabel: vocab.Regions.Label(a.Region), Country: a.Country,
		Instructions: a.Instructions, IsDefault: a.IsDefault, DeliveryAvailability: avail, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}, err
}

// Input is an address as the customer sends it. On PATCH, fields left out
// keep their value; an empty string clears an optional field.
type Input struct {
	Label         *string `json:"label"`
	RecipientName *string `json:"recipient_name"`
	Phone         *string `json:"phone"`
	Line1         *string `json:"line1"`
	Line2         *string `json:"line2"`
	Area          *string `json:"area"`
	City          *string `json:"city"`
	Region        *string `json:"region"`
	Country       *string `json:"country"`
	Instructions  *string `json:"instructions"`
	IsDefault     *bool   `json:"is_default"`
}

type fields struct {
	label, phone, line2, area, instructions *string
	recipient, line1, city, region, country string
	isDefault                               bool
}

func fromRow(a store.CustomerAddress) fields {
	return fields{label: a.Label, phone: a.Phone, line2: a.Line2, area: a.Area, instructions: a.Instructions,
		recipient: a.RecipientName, line1: a.Line1, city: a.City, region: a.Region, country: a.Country, isDefault: a.IsDefault}
}

// apply merges input into f and validates the result.
func apply(f *fields, in Input) []httpx.FieldError {
	var errs []httpx.FieldError
	bad := func(field, code, msg string) {
		errs = append(errs, httpx.FieldError{Field: field, Code: code, Message: msg})
	}
	text := func(v *string) string { return strings.Join(strings.Fields(*v), " ") }
	required := func(dst *string, v *string, field, label string, max int) {
		if v == nil {
			return
		}
		*dst = text(v)
	}
	optional := func(dst **string, v *string) {
		if v == nil {
			return
		}
		t := text(v)
		if t == "" {
			*dst = nil
			return
		}
		*dst = &t
	}
	required(&f.recipient, in.RecipientName, "recipient_name", "", 120)
	required(&f.line1, in.Line1, "line1", "", 120)
	required(&f.city, in.City, "city", "", 80)
	required(&f.region, in.Region, "region", "", 60)
	required(&f.country, in.Country, "country", "", 2)
	optional(&f.label, in.Label)
	optional(&f.line2, in.Line2)
	optional(&f.area, in.Area)
	if in.Instructions != nil {
		t := strings.TrimSpace(*in.Instructions)
		f.instructions = &t
		if t == "" {
			f.instructions = nil
		}
	}
	if in.Phone != nil {
		if strings.TrimSpace(*in.Phone) == "" {
			f.phone = nil
		} else if p, ok := customers.NormalizePhone(*in.Phone); ok {
			f.phone = &p
		} else {
			bad("phone", "invalid", "Enter a phone number with 7 to 15 digits.")
		}
	}
	if in.IsDefault != nil {
		f.isDefault = *in.IsDefault
	}
	check := func(field, v string, min, max int, msg string) {
		if n := len([]rune(v)); n < min || n > max || controlChars.MatchString(v) {
			bad(field, "invalid", msg)
		}
	}
	check("recipient_name", f.recipient, 1, 120, "Enter who will receive deliveries (up to 120 characters).")
	check("line1", f.line1, 1, 120, "Enter the street address (up to 120 characters).")
	check("city", f.city, 1, 80, "Enter the town, city or community (up to 80 characters).")
	if f.label != nil {
		check("label", *f.label, 1, 40, "Keep the label to 40 characters.")
	}
	if f.line2 != nil {
		check("line2", *f.line2, 1, 120, "Keep the second line to 120 characters.")
	}
	if f.area != nil {
		check("area", *f.area, 1, 80, "Keep the area to 80 characters.")
	}
	if f.instructions != nil && (len([]rune(*f.instructions)) > 300 || strings.ContainsAny(*f.instructions, "<>")) {
		bad("instructions", "invalid", "Keep delivery instructions to 300 characters.")
	}
	if f.country == "" {
		f.country = "TT"
	}
	if !vocab.Countries.Has(f.country) {
		bad("country", "invalid", "Mise is starting in Trinidad and Tobago.")
	}
	if !vocab.Regions.Has(f.region) {
		bad("region", "invalid", "Choose a region from the list.")
	}
	return errs
}

func (h *Handler) customerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, err := h.Customers.CustomerID(r.Context(), identity.PrincipalFrom(r.Context()))
	if err != nil {
		httpx.Fail(w, r, err)
		return "", false
	}
	return id, true
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	cid, ok := h.customerID(w, r)
	if !ok {
		return
	}
	rows, err := store.New(h.Pool).ListAddresses(r.Context(), cid)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := []Address{}
	for _, a := range rows {
		v, err := h.view(r.Context(), a)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		out = append(out, v)
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"items": out})
}

func decode(w http.ResponseWriter, r *http.Request) (Input, bool) {
	var in Input
	if err := httpx.Decode(w, r, &in, 8<<10); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	return in, true
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	in, ok := decode(w, r)
	if !ok {
		return
	}
	cid, ok := h.customerID(w, r)
	if !ok {
		return
	}
	var f fields
	for _, req := range []struct {
		v     *string
		field string
	}{{in.RecipientName, "recipient_name"}, {in.Line1, "line1"}, {in.City, "city"}, {in.Region, "region"}} {
		if req.v == nil {
			httpx.Fail(w, r, httpx.ValidationError([]httpx.FieldError{{Field: req.field, Code: "required", Message: "This field is required."}}))
			return
		}
	}
	if errs := apply(&f, in); len(errs) > 0 {
		httpx.Fail(w, r, httpx.ValidationError(errs))
		return
	}
	var row store.CustomerAddress
	err := db.InTx(r.Context(), h.Pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		n, err := q.CountAddresses(r.Context(), cid)
		if err != nil {
			return err
		}
		if n >= maxAddresses {
			return errTooMany
		}
		// The first address is the default, whatever was asked.
		if n == 0 {
			f.isDefault = true
		}
		row, err = q.CreateAddress(r.Context(), store.CreateAddressParams{CustomerID: cid, Label: f.label, RecipientName: f.recipient,
			Phone: f.phone, Line1: f.line1, Line2: f.line2, Area: f.area, City: f.city, Region: f.region, Country: f.country,
			Instructions: f.instructions, IsDefault: false})
		if err != nil {
			return err
		}
		if f.isDefault {
			if row, err = h.makeDefault(r.Context(), q, cid, row, f); err != nil {
				return err
			}
		}
		return audit.Record(r.Context(), tx, audit.Entry{ActorUserID: identity.PrincipalFrom(r.Context()).UserID,
			Action: "customer.address_added", TargetType: "customer_address", TargetID: row.ID})
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, err := h.view(r.Context(), row)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusCreated, v)
}

// makeDefault clears the old default first, so the one-default index holds.
func (h *Handler) makeDefault(ctx context.Context, q *store.Queries, cid string, row store.CustomerAddress, f fields) (store.CustomerAddress, error) {
	if err := q.ClearDefault(ctx, store.ClearDefaultParams{CustomerID: cid, KeepID: row.ID}); err != nil {
		return row, err
	}
	return q.UpdateAddress(ctx, store.UpdateAddressParams{ID: row.ID, CustomerID: cid, Label: f.label, RecipientName: f.recipient,
		Phone: f.phone, Line1: f.line1, Line2: f.line2, Area: f.area, City: f.city, Region: f.region, Country: f.country,
		Instructions: f.instructions, IsDefault: true})
}

func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", false
	}
	return id, true
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := decode(w, r)
	if !ok {
		return
	}
	cid, ok := h.customerID(w, r)
	if !ok {
		return
	}
	var row store.CustomerAddress
	err := db.InTx(r.Context(), h.Pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		cur, err := q.GetAddress(r.Context(), store.GetAddressParams{ID: id, CustomerID: cid})
		if errors.Is(err, pgx.ErrNoRows) {
			// Someone else's address and no address at all look the same.
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		f := fromRow(cur)
		if errs := apply(&f, in); len(errs) > 0 {
			return httpx.ValidationError(errs)
		}
		// The default can move to another address, not simply be switched off.
		if cur.IsDefault && !f.isDefault {
			return httpx.ValidationError([]httpx.FieldError{{Field: "is_default", Code: "keep_default",
				Message: "Choose another address as the default instead."}})
		}
		if f.isDefault && !cur.IsDefault {
			row, err = h.makeDefault(r.Context(), q, cid, cur, f)
		} else {
			row, err = q.UpdateAddress(r.Context(), store.UpdateAddressParams{ID: id, CustomerID: cid, Label: f.label, RecipientName: f.recipient,
				Phone: f.phone, Line1: f.line1, Line2: f.line2, Area: f.area, City: f.city, Region: f.region, Country: f.country,
				Instructions: f.instructions, IsDefault: f.isDefault})
		}
		if err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{ActorUserID: identity.PrincipalFrom(r.Context()).UserID,
			Action: "customer.address_updated", TargetType: "customer_address", TargetID: id})
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, err := h.view(r.Context(), row)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, v)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	cid, ok := h.customerID(w, r)
	if !ok {
		return
	}
	err := db.InTx(r.Context(), h.Pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		wasDefault, err := q.DeleteAddress(r.Context(), store.DeleteAddressParams{ID: id, CustomerID: cid})
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if wasDefault {
			if err := q.PromoteOldestToDefault(r.Context(), cid); err != nil {
				return err
			}
		}
		return audit.Record(r.Context(), tx, audit.Entry{ActorUserID: identity.PrincipalFrom(r.Context()).UserID,
			Action: "customer.address_removed", TargetType: "customer_address", TargetID: id})
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"removed": true})
}
