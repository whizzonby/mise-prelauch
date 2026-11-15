package customers

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/customers/store"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// ---------------------------------------------------------------- onboarding

// OnboardingSteps in order. Each step's answers are saved through the normal
// endpoints (household, preferences, profile); this records only where the
// customer is, so onboarding can be resumed on any device.
var OnboardingSteps = []string{"welcome", "household", "interests", "dietary", "allergies", "location", "complete"}

// skippableSteps may be skipped. Skipping allergies is allowed but leaves the
// question open, and the app keeps asking.
var skippableSteps = []string{"household", "interests", "dietary", "allergies", "location"}

type Onboarding struct {
	Step        string     `json:"step"`
	Skipped     []string   `json:"skipped"`
	CompletedAt *time.Time `json:"completed_at"`
	Steps       []string   `json:"steps"`
}

func (s *Service) GetOnboarding(ctx context.Context, p *identity.Principal) (*Onboarding, error) {
	c, err := customer(ctx, s.Pool, p)
	if err != nil {
		return nil, err
	}
	row, err := store.New(s.Pool).GetOnboarding(ctx, c.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &Onboarding{Step: "welcome", Skipped: []string{}, Steps: OnboardingSteps}, nil
	}
	if err != nil {
		return nil, err
	}
	return &Onboarding{Step: row.Step, Skipped: row.Skipped, CompletedAt: row.CompletedAt, Steps: OnboardingSteps}, nil
}

type OnboardingInput struct {
	Step    string   `json:"step"`
	Skipped []string `json:"skipped"`
}

func (s *Service) PutOnboarding(ctx context.Context, p *identity.Principal, in OnboardingInput) (*Onboarding, error) {
	var fields []httpx.FieldError
	if !slices.Contains(OnboardingSteps, in.Step) {
		fields = append(fields, httpx.FieldError{Field: "step", Code: "invalid", Message: "Unknown onboarding step."})
	}
	skipped := []string{}
	for _, k := range in.Skipped {
		if !slices.Contains(skippableSteps, k) {
			fields = append(fields, httpx.FieldError{Field: "skipped", Code: "invalid", Message: "Only optional questions can be skipped."})
			break
		}
		if !slices.Contains(skipped, k) {
			skipped = append(skipped, k)
		}
	}
	if len(fields) > 0 {
		return nil, httpx.ValidationError(fields)
	}
	var out *Onboarding
	err := db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		c, err := customer(ctx, tx, p)
		if err != nil {
			return err
		}
		var completed *time.Time
		if in.Step == "complete" {
			now := s.now().UTC()
			completed = &now
		}
		row, err := store.New(tx).UpsertOnboarding(ctx, store.UpsertOnboardingParams{CustomerID: c.ID, Step: in.Step, Skipped: skipped, CompletedAt: completed})
		if err != nil {
			return err
		}
		if in.Step == "complete" {
			if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "customer.onboarding_completed", TargetType: "customer", TargetID: c.ID,
				Metadata: map[string]any{"skipped": skipped}}); err != nil {
				return err
			}
		}
		out = &Onboarding{Step: row.Step, Skipped: row.Skipped, CompletedAt: row.CompletedAt, Steps: OnboardingSteps}
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------- notifications

type Topic struct {
	Topic     string     `json:"topic"`
	Label     string     `json:"label"`
	Enabled   bool       `json:"enabled"`
	Source    *string    `json:"source"`
	UpdatedAt *time.Time `json:"updated_at"`
}

type NotificationPreferences struct {
	// AccountSecurity is always on: sign-in, password and email-change notices
	// are part of keeping the account safe, not marketing.
	AccountSecurity struct {
		Enabled  bool `json:"enabled"`
		Required bool `json:"required"`
	} `json:"account_security"`
	Topics   []Topic      `json:"topics"`
	Upcoming []vocab.Term `json:"upcoming"`
}

func (s *Service) GetNotificationPreferences(ctx context.Context, p *identity.Principal) (*NotificationPreferences, error) {
	c, err := customer(ctx, s.Pool, p)
	if err != nil {
		return nil, err
	}
	return loadNotifications(ctx, s.Pool, c.ID)
}

func loadNotifications(ctx context.Context, q db.Querier, customerID string) (*NotificationPreferences, error) {
	rows, err := store.New(q).ListNotificationPreferences(ctx, customerID)
	if err != nil {
		return nil, err
	}
	out := &NotificationPreferences{Upcoming: vocab.UpcomingTopics}
	out.AccountSecurity.Enabled, out.AccountSecurity.Required = true, true
	for _, t := range vocab.Topics {
		topic := Topic{Topic: t.Key, Label: t.Label, Enabled: vocab.ServiceTopics[t.Key]}
		for _, r := range rows {
			if r.Topic == t.Key {
				topic.Enabled, topic.Source, topic.UpdatedAt = r.Enabled, &r.Source, &r.UpdatedAt
			}
		}
		out.Topics = append(out.Topics, topic)
	}
	return out, nil
}

type NotificationInput struct {
	Topics map[string]bool `json:"topics"`
}

// PutNotificationPreferences records the customer's choices. Each change is an
// audited consent record: topic, new value, when, and that the customer made it.
func (s *Service) PutNotificationPreferences(ctx context.Context, p *identity.Principal, in NotificationInput) (*NotificationPreferences, error) {
	for topic := range in.Topics {
		switch {
		case topic == "account_security":
			return nil, httpx.ValidationError([]httpx.FieldError{{Field: "topics.account_security", Code: "required", Message: "Account and security email cannot be turned off."}})
		case vocab.UpcomingTopics.Has(topic):
			return nil, httpx.ValidationError([]httpx.FieldError{{Field: "topics." + topic, Code: "not_available", Message: "This kind of email is not available yet."}})
		case !vocab.Topics.Has(topic):
			return nil, httpx.ValidationError([]httpx.FieldError{{Field: "topics." + topic, Code: "invalid", Message: "Unknown email topic."}})
		}
	}
	var out *NotificationPreferences
	err := db.InTx(ctx, s.Pool, func(tx pgx.Tx) error {
		c, err := customer(ctx, tx, p)
		if err != nil {
			return err
		}
		st := store.New(tx)
		before, err := loadNotifications(ctx, tx, c.ID)
		if err != nil {
			return err
		}
		changes := map[string]bool{}
		for _, t := range before.Topics {
			want, ok := in.Topics[t.Topic]
			if !ok || (want == t.Enabled && t.Source != nil) {
				continue
			}
			if err := st.SetNotificationPreference(ctx, store.SetNotificationPreferenceParams{CustomerID: c.ID, Topic: t.Topic, Enabled: want}); err != nil {
				return err
			}
			changes[t.Topic] = want
		}
		if len(changes) > 0 {
			if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "customer.consent_updated", TargetType: "customer", TargetID: c.ID,
				Metadata: map[string]any{"changes": changes}}); err != nil {
				return err
			}
		}
		out, err = loadNotifications(ctx, tx, c.ID)
		return err
	})
	return out, err
}
