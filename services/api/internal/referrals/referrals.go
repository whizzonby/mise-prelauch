// Package referrals owns referral codes and the referrer → referred
// relationship: who invited whom, and whether that invitation counts yet.
package referrals

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

// codeAlphabet omits characters that are easy to confuse when read aloud or
// typed from a screenshot (0/O, 1/I/L).
const (
	codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLength   = 7
)

var codePattern = regexp.MustCompile(`^[A-Z0-9]{6,12}$`)

// NewCode returns a random referral code. Uniqueness is enforced by the
// database; callers retry on a collision.
func NewCode() (string, error) {
	buf := make([]byte, codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		// 31 symbols: the modulo bias over a byte is negligible for a non-secret code.
		buf[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(buf), nil
}

// NormalizeCode upper-cases and trims a code typed or pasted by a person.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func ValidCode(s string) bool { return codePattern.MatchString(s) }

type Referrer struct {
	ID           string
	FirstName    string
	SignupIPHash *string
}

// FindReferrer returns the lead that owns code, or nil if the code is unknown
// or its owner has been blocked.
func FindReferrer(ctx context.Context, q db.Querier, code string) (*Referrer, error) {
	if !ValidCode(code) {
		return nil, nil
	}
	var r Referrer
	err := q.QueryRow(ctx,
		`SELECT id, first_name, signup_ip_hash FROM leads WHERE referral_code = $1 AND status <> 'BLOCKED'`, code,
	).Scan(&r.ID, &r.FirstName, &r.SignupIPHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Create records that referrer invited referred. A flagged referral is kept
// for admins to see but is never converted or counted.
func Create(ctx context.Context, q db.Querier, referrerID, referredID, code string, flagged bool) error {
	status := "pending"
	if flagged {
		status = "flagged"
	}
	_, err := q.Exec(ctx,
		`INSERT INTO referrals (referrer_lead_id, referred_lead_id, referral_code, status) VALUES ($1, $2, $3, $4)`,
		referrerID, referredID, code, status)
	return err
}

// Convert marks the pending referral for a newly verified lead as converted.
// It returns the referrer and their new converted total, or ok=false when
// there was nothing to convert.
func Convert(ctx context.Context, q db.Querier, referredLeadID string) (referrerID string, converted int, ok bool, err error) {
	err = q.QueryRow(ctx,
		`UPDATE referrals SET status = 'converted', converted_at = now()
		 WHERE referred_lead_id = $1 AND status = 'pending'
		 RETURNING referrer_lead_id`, referredLeadID,
	).Scan(&referrerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	counts, err := CountsFor(ctx, q, referrerID)
	if err != nil {
		return "", 0, false, err
	}
	return referrerID, counts.Converted, true, nil
}

type Counts struct {
	Pending   int `json:"pending"`
	Converted int `json:"converted"`
}

func CountsFor(ctx context.Context, q db.Querier, referrerID string) (Counts, error) {
	var c Counts
	err := q.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE status = 'pending'), count(*) FILTER (WHERE status = 'converted')
		 FROM referrals WHERE referrer_lead_id = $1`, referrerID,
	).Scan(&c.Pending, &c.Converted)
	return c, err
}

type Handler struct {
	DB db.Querier
}

// Lookup serves GET /api/v1/referrals/{code}. It lets the invitation page
// greet the visitor with their friend's first name.
func (h *Handler) Lookup(w http.ResponseWriter, r *http.Request) {
	code := NormalizeCode(r.PathValue("code"))
	ref, err := FindReferrer(r.Context(), h.DB, code)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if ref == nil {
		httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "referral_not_found", "This invitation link is not valid."))
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]string{
		"code":                code,
		"referrer_first_name": ref.FirstName,
	})
}
