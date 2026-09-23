package leads

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/platform/token"
	"mise.tt/api/internal/referrals"
)

const (
	verifyTokenTTL  = 7 * 24 * time.Hour
	profileTokenTTL = 30 * 24 * time.Hour
)

var (
	errSignupRejected = httpx.NewError(http.StatusUnprocessableEntity, "signup_rejected",
		"We could not process this signup. Reload the page and try again.")
	errTokenInvalid = httpx.NewError(http.StatusBadRequest, "token_invalid",
		"This link is not valid. Use the most recent email we sent you.")
	errTokenExpired = httpx.NewError(http.StatusGone, "token_expired",
		"This link has expired. Join the list again and we will send a new one.")
	errLeadUnavailable = httpx.NewError(http.StatusForbidden, "lead_unavailable",
		"This signup is not available.")
)

type Options struct {
	PublicSiteURL        string
	VerificationRequired bool
	ConsentVersion       string
	ReferralMilestones   []int
}

type Service struct {
	pool   *pgxpool.Pool
	signer *token.Signer
	opts   Options
}

func NewService(pool *pgxpool.Pool, signer *token.Signer, opts Options) *Service {
	return &Service{pool: pool, signer: signer, opts: opts}
}

// ReferralURL is the link a lead shares.
func (s *Service) ReferralURL(code string) string { return s.opts.PublicSiteURL + "/join/" + code }

// VerifyURL is the confirmation link sent by email.
func (s *Service) VerifyURL(leadID string) string {
	return s.opts.PublicSiteURL + "/verify?token=" + s.signer.Sign(token.PurposeVerifyEmail, leadID, verifyTokenTTL)
}

// UnsubscribeURL is included in every email.
func (s *Service) UnsubscribeURL(leadID string) string {
	return s.opts.PublicSiteURL + "/unsubscribe?token=" + s.signer.Sign(token.PurposeUnsubscribe, leadID, 0)
}

// WelcomeURL takes a lead back to their referral page from an email.
func (s *Service) WelcomeURL(leadID string) string {
	return s.opts.PublicSiteURL + "/welcome?token=" + s.signer.Sign(token.PurposeProfile, leadID, profileTokenTTL)
}

// Signup adds a person to the waitlist. Submitting an address that is already
// on the list is not an error: the existing lead is emailed their link and the
// caller is told only that the address is already listed.
func (s *Service) Signup(ctx context.Context, in SignupInput, clientIP string) (SignupResult, error) {
	norm, err := validateSignup(in)
	if err != nil {
		return SignupResult{}, err
	}
	if looksAutomated(in) {
		slog.InfoContext(ctx, "signup rejected by bot checks")
		return SignupResult{}, errSignupRejected
	}

	ipHash := s.signer.HashIP(clientIP)

	// A referral-code collision or a concurrent signup for the same address
	// aborts the transaction; both are resolved by running it again.
	for attempt := 0; ; attempt++ {
		var result SignupResult
		err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			var txErr error
			result, txErr = s.signupTx(ctx, tx, norm, ipHash)
			return txErr
		})
		if err != nil && attempt < 3 && db.IsUniqueViolation(err, "") {
			continue
		}
		if err != nil {
			return SignupResult{}, err
		}
		slog.InfoContext(ctx, "signup handled", "event", "lead."+string(result.Outcome))
		return result, nil
	}
}

func (s *Service) signupTx(ctx context.Context, tx pgx.Tx, norm normalizedSignup, ipHash string) (SignupResult, error) {
	existing, err := getByCanonicalEmailForUpdate(ctx, tx, norm.EmailCanonical)
	if err != nil {
		return SignupResult{}, err
	}
	if existing != nil {
		return SignupResult{Outcome: OutcomeAlreadyOnList}, s.handleDuplicate(ctx, tx, existing)
	}

	code, err := referrals.NewCode()
	if err != nil {
		return SignupResult{}, err
	}

	var referrer *referrals.Referrer
	if norm.ReferralCode != "" {
		if referrer, err = referrals.FindReferrer(ctx, tx, norm.ReferralCode); err != nil {
			return SignupResult{}, err
		}
	}

	status := StatusPending
	if !s.opts.VerificationRequired {
		status = StatusVerified
	}
	in := insertLead{
		normalizedSignup: norm,
		Status:           status,
		Code:             code,
		ConsentVersion:   s.opts.ConsentVersion,
		IPHash:           ipHash,
	}
	if referrer != nil {
		in.ReferredBy = &referrer.ID
	}
	lead, err := insert(ctx, tx, in)
	if err != nil {
		return SignupResult{}, err
	}

	if referrer != nil {
		// Same network as the referrer usually means someone inviting themselves.
		flagged := ipHash != "" && referrer.SignupIPHash != nil && *referrer.SignupIPHash == ipHash
		if err := referrals.Create(ctx, tx, referrer.ID, lead.ID, norm.ReferralCode, flagged); err != nil {
			return SignupResult{}, err
		}
	}

	if s.opts.VerificationRequired {
		err = jobs.Enqueue(ctx, tx, JobEmailVerify, EmailJob{LeadID: lead.ID})
	} else {
		if err = s.convertReferral(ctx, tx, lead.ID); err == nil {
			err = jobs.Enqueue(ctx, tx, JobEmailWelcome, EmailJob{LeadID: lead.ID})
		}
	}
	if err != nil {
		return SignupResult{}, err
	}

	summary, err := s.summarize(ctx, tx, lead)
	if err != nil {
		return SignupResult{}, err
	}
	return SignupResult{
		Outcome:      OutcomeCreated,
		Lead:         summary,
		ProfileToken: s.signer.Sign(token.PurposeProfile, lead.ID, profileTokenTTL),
	}, nil
}

// handleDuplicate decides what an existing lead should receive when their
// address is submitted again.
func (s *Service) handleDuplicate(ctx context.Context, tx pgx.Tx, lead *Lead) error {
	kind := ""
	switch lead.Status {
	case StatusPending:
		kind = JobEmailVerify
	case StatusVerified, StatusQualified:
		kind = JobEmailAlreadyOnList
	case StatusUnsubscribed:
		// Signing up again with the consent box ticked is fresh consent.
		status, next := StatusPending, JobEmailVerify
		if lead.EmailVerifiedAt != nil || !s.opts.VerificationRequired {
			status, next = StatusVerified, JobEmailAlreadyOnList
		}
		if err := renewConsent(ctx, tx, lead.ID, status, s.opts.ConsentVersion); err != nil {
			return err
		}
		kind = next
	default:
		// Blocked and converted leads get nothing.
		return nil
	}
	recent, err := recentlyEmailed(ctx, tx, lead.ID, kind)
	if err != nil || recent {
		return err
	}
	return jobs.Enqueue(ctx, tx, kind, EmailJob{LeadID: lead.ID})
}

// convertReferral counts a newly verified lead for whoever invited them and
// queues a milestone email if the referrer just reached one.
func (s *Service) convertReferral(ctx context.Context, tx pgx.Tx, leadID string) error {
	referrerID, total, ok, err := referrals.Convert(ctx, tx, leadID)
	if err != nil || !ok {
		return err
	}
	if slices.Contains(s.opts.ReferralMilestones, total) {
		return jobs.Enqueue(ctx, tx, JobEmailReferralMilestone, EmailJob{LeadID: referrerID, Count: total})
	}
	return nil
}

func (s *Service) summarize(ctx context.Context, q db.Querier, lead *Lead) (*Summary, error) {
	counts, err := referrals.CountsFor(ctx, q, lead.ID)
	if err != nil {
		return nil, err
	}
	done, err := profileCompleted(ctx, q, lead.ID)
	if err != nil {
		return nil, err
	}
	return &Summary{
		FirstName:            lead.FirstName,
		Status:               lead.Status,
		VerificationRequired: lead.Status == StatusPending && s.opts.VerificationRequired,
		ReferralCode:         lead.ReferralCode,
		ReferralURL:          s.ReferralURL(lead.ReferralCode),
		Referrals:            counts,
		ProfileCompleted:     done,
	}, nil
}

type VerifyResult struct {
	Lead         *Summary `json:"lead"`
	ProfileToken string   `json:"profile_token"`
}

// Verify confirms a lead's email address. It is idempotent: following the link
// twice succeeds twice.
func (s *Service) Verify(ctx context.Context, tok string) (VerifyResult, error) {
	leadID, err := s.signer.Verify(token.PurposeVerifyEmail, tok)
	if err != nil {
		return VerifyResult{}, tokenError(err)
	}

	var result VerifyResult
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		lead, err := getForUpdate(ctx, tx, leadID)
		if err != nil {
			return err
		}
		if lead == nil {
			return errTokenInvalid
		}
		switch lead.Status {
		case StatusBlocked:
			return errLeadUnavailable
		case StatusPending:
			if err := markVerified(ctx, tx, lead.ID, StatusVerified); err != nil {
				return err
			}
			lead.Status = StatusVerified
			if err := s.convertReferral(ctx, tx, lead.ID); err != nil {
				return err
			}
			if err := jobs.Enqueue(ctx, tx, JobEmailWelcome, EmailJob{LeadID: lead.ID}); err != nil {
				return err
			}
			slog.InfoContext(ctx, "lead verified", "event", "lead.verified")
		case StatusUnsubscribed:
			// The address is confirmed, but the person still does not want email.
			if err := markVerified(ctx, tx, lead.ID, StatusUnsubscribed); err != nil {
				return err
			}
		}
		summary, err := s.summarize(ctx, tx, lead)
		if err != nil {
			return err
		}
		result = VerifyResult{Lead: summary, ProfileToken: s.signer.Sign(token.PurposeProfile, lead.ID, profileTokenTTL)}
		return nil
	})
	return result, err
}

// Authenticate resolves a profile token to a lead id.
func (s *Service) Authenticate(tok string) (string, error) {
	leadID, err := s.signer.Verify(token.PurposeProfile, tok)
	if err != nil {
		return "", httpx.ErrUnauthorized
	}
	return leadID, nil
}

func (s *Service) Me(ctx context.Context, leadID string) (*Summary, error) {
	lead, err := s.activeLead(ctx, s.pool, leadID)
	if err != nil {
		return nil, err
	}
	return s.summarize(ctx, s.pool, lead)
}

func (s *Service) UpdatePreferences(ctx context.Context, leadID string, in PreferencesInput) (*Summary, error) {
	prefs, err := validatePreferences(in)
	if err != nil {
		return nil, err
	}
	lead, err := s.activeLead(ctx, s.pool, leadID)
	if err != nil {
		return nil, err
	}
	if err := upsertPreferences(ctx, s.pool, lead.ID, prefs); err != nil {
		return nil, err
	}
	return s.summarize(ctx, s.pool, lead)
}

func (s *Service) activeLead(ctx context.Context, q db.Querier, leadID string) (*Lead, error) {
	lead, err := GetByID(ctx, q, leadID)
	if err != nil {
		return nil, err
	}
	if lead == nil {
		return nil, httpx.ErrUnauthorized
	}
	if lead.Status == StatusBlocked {
		return nil, errLeadUnavailable
	}
	return lead, nil
}

// Unsubscribe stops all email to a lead. It is idempotent and never reveals
// whether the lead still exists.
func (s *Service) Unsubscribe(ctx context.Context, tok string) error {
	leadID, err := s.signer.Verify(token.PurposeUnsubscribe, tok)
	if err != nil {
		return tokenError(err)
	}
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		lead, err := getForUpdate(ctx, tx, leadID)
		if err != nil || lead == nil {
			return err
		}
		switch lead.Status {
		case StatusPending, StatusVerified, StatusQualified:
			slog.InfoContext(ctx, "lead unsubscribed", "event", "lead.unsubscribed")
			return setStatus(ctx, tx, lead.ID, StatusUnsubscribed)
		}
		return nil
	})
}

func tokenError(err error) error {
	if errors.Is(err, token.ErrExpired) {
		return errTokenExpired
	}
	return errTokenInvalid
}
