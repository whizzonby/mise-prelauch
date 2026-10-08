// Package leads owns the waitlist: signup, email verification, preferences,
// unsubscribe and erasure. A lead is the pre-launch form of a future customer.
package leads

import (
	"time"

	"mise.tt/api/internal/referrals"
)

type Status string

const (
	StatusPending      Status = "PENDING"      // signed up, email not yet confirmed
	StatusVerified     Status = "VERIFIED"     // on the list
	StatusQualified    Status = "QUALIFIED"    // marked by the team as a strong launch candidate
	StatusConverted    Status = "CONVERTED"    // became a customer (post-launch migration)
	StatusUnsubscribed Status = "UNSUBSCRIBED" // asked not to be contacted
	StatusBlocked      Status = "BLOCKED"      // abuse; excluded from everything
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusVerified, StatusQualified, StatusConverted, StatusUnsubscribed, StatusBlocked:
		return true
	}
	return false
}

// Job kinds enqueued by this package and handled by notifications.
const (
	JobEmailVerify            = "email.verify"
	JobEmailWelcome           = "email.welcome"
	JobEmailAlreadyOnList     = "email.already_on_list"
	JobEmailReferralMilestone = "email.referral_milestone"
)

// EmailJob is the payload for every email job. It carries ids only; the worker
// loads current data, so queued jobs never hold personal information.
type EmailJob struct {
	LeadID string `json:"lead_id"`
	Count  int    `json:"count,omitempty"`
}

type Lead struct {
	ID              string
	FirstName       string
	LastName        string
	Email           string
	Phone           *string
	Location        string
	Status          Status
	ReferralCode    string
	ReferredBy      *string
	EmailVerifiedAt *time.Time
	ConsentAt       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Summary is what a lead may see about themselves.
type Summary struct {
	FirstName            string           `json:"first_name"`
	Status               Status           `json:"status"`
	VerificationRequired bool             `json:"verification_required"`
	ReferralCode         string           `json:"referral_code"`
	ReferralURL          string           `json:"referral_url"`
	Referrals            referrals.Counts `json:"referrals"`
	ProfileCompleted     bool             `json:"profile_completed"`
}

type Touch struct {
	UTMSource   string `json:"utm_source"`
	UTMMedium   string `json:"utm_medium"`
	UTMCampaign string `json:"utm_campaign"`
	UTMContent  string `json:"utm_content"`
	UTMTerm     string `json:"utm_term"`
	LandingPage string `json:"landing_page"`
	ReferrerURL string `json:"referrer_url"`
}

func (t Touch) empty() bool { return t == Touch{} }

type Attribution struct {
	First  *Touch `json:"first"`
	Latest *Touch `json:"latest"`
}

type SignupInput struct {
	FirstName        string   `json:"first_name"`
	LastName         string   `json:"last_name"`
	Email            string   `json:"email"`
	Phone            string   `json:"phone"`
	Location         string   `json:"location"`
	DietaryInterests []string `json:"dietary_interests"`
	HouseholdSize    *int     `json:"household_size"`
	// PackagingPreference is optional: the packaging the lead would like kits to come in.
	PackagingPreference string       `json:"packaging_preference"`
	Consent             bool         `json:"consent"`
	ReferralCode        string       `json:"referral_code"`
	Attribution         *Attribution `json:"attribution"`
	AnonymousID         string       `json:"anonymous_id"`

	// Bot checks. Website is a honeypot field hidden from people; ElapsedMS is
	// how long the form was open before it was submitted.
	Website   string `json:"website"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

type SignupOutcome string

const (
	OutcomeCreated       SignupOutcome = "created"
	OutcomeAlreadyOnList SignupOutcome = "already_on_list"
)

type SignupResult struct {
	Outcome      SignupOutcome `json:"outcome"`
	Lead         *Summary      `json:"lead,omitempty"`
	ProfileToken string        `json:"profile_token,omitempty"`
}

type PreferencesInput struct {
	HouseholdSize       *int      `json:"household_size"`
	MealsPerWeek        *int      `json:"meals_per_week"`
	DietaryPreferences  *[]string `json:"dietary_preferences"`
	PackagingPreference *string   `json:"packaging_preference"`
	MealInterests       *[]string `json:"meal_interests"`
	CookingFrequency    *string   `json:"cooking_frequency"`
	DeliveryArea        *string   `json:"delivery_area"`
	HouseholdType       *string   `json:"household_type"`
	FitnessGoal         *string   `json:"fitness_goal"`
	Usage               *string   `json:"usage"`
}
