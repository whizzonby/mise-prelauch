package customers

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/customers/store"
	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// Service is the customer's own records. Every method takes the signed-in
// principal and works on that person's customer row only; no method accepts a
// customer id from the caller.
type Service struct {
	Pool     *pgxpool.Pool
	Identity *identity.Service
	Delivery delivery.Eligibility
	now      func() time.Time
}

func NewService(pool *pgxpool.Pool, ids *identity.Service) *Service {
	return &Service{Pool: pool, Identity: ids, Delivery: delivery.Zones{DB: pool}, now: time.Now}
}

// customer loads the caller's customer row. Every customer account has one,
// created at registration.
func customer(ctx context.Context, q db.Querier, p *identity.Principal) (store.Customer, error) {
	c, err := store.New(q).GetByUserID(ctx, p.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httpx.ErrNotFound
	}
	return c, err
}

// CustomerID returns the caller's customer id, for other modules
// (favourites, addresses) that key their rows by it.
func (s *Service) CustomerID(ctx context.Context, p *identity.Principal) (string, error) {
	c, err := customer(ctx, s.Pool, p)
	return c.ID, err
}

// Profile is the account as its owner sees it.
type Profile struct {
	Email                string                `json:"email"`
	PendingEmail         *string               `json:"pending_email"`
	FirstName            string                `json:"first_name"`
	LastName             string                `json:"last_name"`
	EmailVerified        bool                  `json:"email_verified"`
	Phone                *string               `json:"phone"`
	ContactChannel       string                `json:"contact_channel"`
	Language             string                `json:"language"`
	DeliveryCountry      *string               `json:"delivery_country"`
	DeliveryRegion       *string               `json:"delivery_region"`
	DeliveryAvailability delivery.Availability `json:"delivery_availability"`
	WaitlistLinked       bool                  `json:"waitlist_linked"`
	MemberSince          time.Time             `json:"member_since"`
}

func (s *Service) GetProfile(ctx context.Context, p *identity.Principal) (*Profile, error) {
	c, err := customer(ctx, s.Pool, p)
	if err != nil {
		return nil, err
	}
	return s.profile(ctx, s.Pool, p, c, p.FirstName, p.LastName)
}

func (s *Service) profile(ctx context.Context, q db.Querier, p *identity.Principal, c store.Customer, first, last string) (*Profile, error) {
	out := &Profile{
		Email: p.Email, FirstName: first, LastName: last, EmailVerified: p.EmailVerified,
		Phone: c.Phone, ContactChannel: c.ContactChannel, Language: c.Language,
		DeliveryCountry: c.DeliveryCountry, DeliveryRegion: c.DeliveryRegion,
		DeliveryAvailability: delivery.Unknown, WaitlistLinked: c.LeadID != nil, MemberSince: c.CreatedAt,
	}
	pending, err := s.Identity.PendingEmailChange(ctx, q, p.UserID)
	if err != nil {
		return nil, err
	}
	if pending != "" {
		out.PendingEmail = &pending
	}
	if c.DeliveryCountry != nil && c.DeliveryRegion != nil {
		if out.DeliveryAvailability, err = s.Delivery.Check(ctx, *c.DeliveryCountry, *c.DeliveryRegion); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ProfileInput changes the profile. Fields left out are unchanged; an empty
// string clears phone and the delivery area.
type ProfileInput struct {
	FirstName       *string `json:"first_name"`
	LastName        *string `json:"last_name"`
	Phone           *string `json:"phone"`
	ContactChannel  *string `json:"contact_channel"`
	Language        *string `json:"language"`
	DeliveryCountry *string `json:"delivery_country"`
	DeliveryRegion  *string `json:"delivery_region"`
}

var phoneChars = regexp.MustCompile(`^\+?[0-9 ()\-]{7,20}$`)

// NormalizePhone keeps a leading + and the digits. It accepts 7 to 15 digits,
// the ITU limit, so local Trinidad and Tobago numbers and international ones
// both pass.
func NormalizePhone(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !phoneChars.MatchString(raw) {
		return "", false
	}
	var b strings.Builder
	if strings.HasPrefix(raw, "+") {
		b.WriteByte('+')
	}
	digits := 0
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			digits++
		}
	}
	return b.String(), digits >= 7 && digits <= 15
}

func (s *Service) UpdateProfile(ctx context.Context, p *identity.Principal, in ProfileInput) (*Profile, error) {
	var out *Profile
	err := db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		c, err := customer(ctx, tx, p)
		if err != nil {
			return err
		}
		var fields []httpx.FieldError
		bad := func(field, code, msg string) {
			fields = append(fields, httpx.FieldError{Field: field, Code: code, Message: msg})
		}

		params := store.UpdateProfileParams{
			ID: c.ID, Phone: c.Phone, ContactChannel: c.ContactChannel, Language: c.Language,
			DeliveryCountry: c.DeliveryCountry, DeliveryRegion: c.DeliveryRegion,
		}
		if in.Phone != nil {
			if strings.TrimSpace(*in.Phone) == "" {
				params.Phone = nil
			} else if phone, ok := NormalizePhone(*in.Phone); ok {
				params.Phone = &phone
			} else {
				bad("phone", "invalid", "Enter a phone number with 7 to 15 digits, for example +1 868 555 0123.")
			}
		}
		if in.ContactChannel != nil {
			switch *in.ContactChannel {
			case "email", "sms", "whatsapp":
				params.ContactChannel = *in.ContactChannel
			default:
				bad("contact_channel", "invalid", "Choose email, SMS or WhatsApp.")
			}
		}
		if params.ContactChannel != "email" && params.Phone == nil {
			bad("contact_channel", "needs_phone", "Add a phone number to be contacted by SMS or WhatsApp.")
		}
		if in.Language != nil {
			if *in.Language != "en" {
				bad("language", "invalid", "English is the only language available for now.")
			} else {
				params.Language = *in.Language
			}
		}
		if in.DeliveryCountry != nil {
			params.DeliveryCountry = emptyToNil(*in.DeliveryCountry)
			if params.DeliveryCountry != nil && !vocab.Countries.Has(*params.DeliveryCountry) {
				bad("delivery_country", "invalid", "Mise is starting in Trinidad and Tobago.")
			}
		}
		if in.DeliveryRegion != nil {
			params.DeliveryRegion = emptyToNil(*in.DeliveryRegion)
			if params.DeliveryRegion != nil && !vocab.Regions.Has(*params.DeliveryRegion) {
				bad("delivery_region", "invalid", "Choose a region from the list.")
			}
		}
		if params.DeliveryRegion != nil && params.DeliveryCountry == nil {
			bad("delivery_country", "required", "Choose the country first.")
		}

		first, last := p.FirstName, p.LastName
		if in.FirstName != nil {
			first = *in.FirstName
		}
		if in.LastName != nil {
			last = *in.LastName
		}
		if in.FirstName != nil || in.LastName != nil {
			if err := identity.SetName(ctx, tx, p.UserID, first, last); err != nil {
				var apiErr *httpx.Error
				if !errors.As(err, &apiErr) {
					return err
				}
				fields = append(fields, apiErr.Fields...)
			}
		}
		if len(fields) > 0 {
			return httpx.ValidationError(fields)
		}
		c, err = store.New(tx).UpdateProfile(ctx, params)
		if err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "customer.profile_updated", TargetType: "customer", TargetID: c.ID,
			Metadata: map[string]any{"fields": changedFields(in)}}); err != nil {
			return err
		}
		first, last = strings.Join(strings.Fields(first), " "), strings.Join(strings.Fields(last), " ")
		out, err = s.profile(ctx, tx, p, c, first, last)
		return err
	})
	return out, err
}

// changedFields names the fields a request touched, never their values.
func changedFields(in ProfileInput) []string {
	var out []string
	add := func(name string, set bool) {
		if set {
			out = append(out, name)
		}
	}
	add("first_name", in.FirstName != nil)
	add("last_name", in.LastName != nil)
	add("phone", in.Phone != nil)
	add("contact_channel", in.ContactChannel != nil)
	add("language", in.Language != nil)
	add("delivery_country", in.DeliveryCountry != nil)
	add("delivery_region", in.DeliveryRegion != nil)
	return out
}

func emptyToNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// UserIDOf returns the identity user behind a customer, for work done
// without a signed-in person (emails sent by the scheduler).
func UserIDOf(ctx context.Context, q db.Querier, customerID string) (string, error) {
	c, err := store.New(q).GetByID(ctx, customerID)
	if err != nil {
		return "", err
	}
	return c.UserID, nil
}
