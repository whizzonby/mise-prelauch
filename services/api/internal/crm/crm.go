// Package crm holds what the business adds about people, and nothing that
// another module already owns: a lead's CRM stage and assignee, internal
// notes on leads and customers, saved customer segments and marketing
// campaign records. Leads, customers, subscriptions and orders are read
// from their own modules (internal/reporting builds the lists).
//
// A CRM stage is not the lead's lifecycle. Verification, unsubscribing and
// conversion stay in leads.status and the customer link; "converted" and
// "unsubscribed" are derived from those, never set here, and a stage never
// creates a customer account.
package crm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/crm/store"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/vocab"
)

// Stages a lead can be put in by staff.
var Stages = []string{"new", "contacted", "qualified", "unqualified"}

type Service struct {
	pool *pgxpool.Pool
	Now  func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------- lead stage

// LeadCRM is a lead's stage and assignee.
type LeadCRM struct {
	Stage          string     `json:"stage"`
	AssignedTo     *string    `json:"assigned_to"`
	StageChangedAt *time.Time `json:"stage_changed_at"`
}

func (s *Service) LeadCRM(ctx context.Context, leadID string) (*LeadCRM, error) {
	p, err := store.New(s.pool).GetLeadProfile(ctx, leadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &LeadCRM{Stage: "new"}, nil
	}
	if err != nil {
		return nil, err
	}
	return &LeadCRM{Stage: p.Stage, AssignedTo: p.AssignedTo, StageChangedAt: &p.StageChangedAt}, nil
}

type LeadCRMInput struct {
	Stage      string  `json:"stage"`
	AssignedTo *string `json:"assigned_to"`
}

func (s *Service) SetLeadCRM(ctx context.Context, actor, leadID string, in LeadCRMInput) (*LeadCRM, error) {
	if !slices.Contains(Stages, in.Stage) {
		return nil, httpx.ValidationError([]httpx.FieldError{{Field: "stage", Code: "invalid", Message: "Choose a stage from the list."}})
	}
	var out LeadCRM
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if ok, err := q.LeadExists(ctx, leadID); err != nil || !ok {
			if err == nil {
				err = httpx.ErrNotFound
			}
			return err
		}
		var assignee *string
		if in.AssignedTo != nil && *in.AssignedTo != "" {
			id := strings.ToLower(*in.AssignedTo)
			ok, err := q.StaffUserExists(ctx, id)
			if err != nil {
				return err
			}
			if !uuidPattern.MatchString(id) || !ok {
				return httpx.ValidationError([]httpx.FieldError{{Field: "assigned_to", Code: "invalid", Message: "Choose an active staff member."}})
			}
			assignee = &id
		}
		before, err := q.GetLeadProfile(ctx, leadID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		p, err := q.UpsertLeadProfile(ctx, store.UpsertLeadProfileParams{LeadID: leadID, Stage: in.Stage, AssignedTo: assignee, UpdatedBy: nilIfEmpty(actor)})
		if err != nil {
			return err
		}
		out = LeadCRM{Stage: p.Stage, AssignedTo: p.AssignedTo, StageChangedAt: &p.StageChangedAt}
		prev := "new"
		if before.LeadID != "" {
			prev = before.Stage
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "lead.crm_updated", TargetType: "lead", TargetID: leadID,
			Metadata: map[string]any{"stage_from": prev, "stage_to": p.Stage, "assigned_to": p.AssignedTo}})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------- notes

// Note is an internal note. It is never shown to the person it is about.
type Note struct {
	ID         string    `json:"id"`
	Body       string    `json:"body"`
	AuthorID   *string   `json:"author_id"`
	AuthorName string    `json:"author_name"`
	CreatedAt  time.Time `json:"created_at"`
}

func cleanNote(body string) (string, error) {
	body = strings.TrimSpace(body)
	if n := len([]rune(body)); n < 1 || n > 2000 {
		return "", httpx.ValidationError([]httpx.FieldError{{Field: "body", Code: "length", Message: "Write a note of up to 2,000 characters."}})
	}
	return body, nil
}

// AddNote adds a note to a lead (subject "lead") or a customer ("customer").
func (s *Service) AddNote(ctx context.Context, actor, subject, id, body string) (*Note, error) {
	body, err := cleanNote(body)
	if err != nil {
		return nil, err
	}
	var out Note
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		var n store.CrmNote
		switch subject {
		case "lead":
			if ok, err := q.LeadExists(ctx, id); err != nil || !ok {
				return orNotFound(err)
			}
			n, err = q.AddLeadNote(ctx, store.AddLeadNoteParams{LeadID: &id, Body: body, AuthorID: nilIfEmpty(actor)})
		case "customer":
			if ok, err := q.CustomerExists(ctx, id); err != nil || !ok {
				return orNotFound(err)
			}
			n, err = q.AddCustomerNote(ctx, store.AddCustomerNoteParams{CustomerID: &id, Body: body, AuthorID: nilIfEmpty(actor)})
		default:
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		out = Note{ID: n.ID, Body: n.Body, AuthorID: n.AuthorID, CreatedAt: n.CreatedAt}
		// The audit entry records that a note was added, not what it says.
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: subject + ".note_added", TargetType: subject, TargetID: id,
			Metadata: map[string]any{"note_id": n.ID}})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func orNotFound(err error) error {
	if err != nil {
		return err
	}
	return httpx.ErrNotFound
}

func (s *Service) Notes(ctx context.Context, subject, id string) ([]Note, error) {
	out := []Note{}
	q := store.New(s.pool)
	switch subject {
	case "lead":
		rows, err := q.LeadNotes(ctx, &id)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, Note{ID: r.ID, Body: r.Body, AuthorID: r.AuthorID, AuthorName: r.AuthorName, CreatedAt: r.CreatedAt})
		}
	case "customer":
		rows, err := q.CustomerNotes(ctx, &id)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, Note{ID: r.ID, Body: r.Body, AuthorID: r.AuthorID, AuthorName: r.AuthorName, CreatedAt: r.CreatedAt})
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- segments

// Rules are a segment's filters. Every rule is a plain condition on stored
// data, so anyone can read why a customer is in a segment. Empty rules
// match every customer. Allergies are never a segment rule.
type Rules struct {
	PlanIDs              []string `json:"plan_ids,omitempty"`
	SubscriptionStatuses []string `json:"subscription_statuses,omitempty"`
	// Lifecycle: prospect (never subscribed), subscriber (a live
	// subscription), lapsed (subscribed before, nothing live now).
	Lifecycle      []string `json:"lifecycle,omitempty"`
	JoinedFrom     string   `json:"joined_from,omitempty"` // YYYY-MM-DD
	JoinedTo       string   `json:"joined_to,omitempty"`
	Regions        []string `json:"regions,omitempty"`
	Dietary        []string `json:"dietary,omitempty"` // any of
	Sources        []string `json:"sources,omitempty"` // first-touch utm_source of the linked lead; "direct" for none
	MinOrders      *int     `json:"min_orders,omitempty"`
	MinTenureWeeks *int     `json:"min_tenure_weeks,omitempty"`
	// ConsentTopic: only customers who have this optional email topic on.
	ConsentTopic string `json:"consent_topic,omitempty"`
}

var (
	subscriptionStatuses = []string{"pending_payment", "active", "pause_scheduled", "paused", "past_due", "cancel_scheduled", "cancelled", "expired"}
	lifecycles           = []string{"prospect", "subscriber", "lapsed"}
	sourcePattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,59}$`)
)

// Validate checks every rule against the known values.
func (r *Rules) Validate() []httpx.FieldError {
	var f []httpx.FieldError
	bad := func(field, msg string) {
		f = append(f, httpx.FieldError{Field: "rules." + field, Code: "invalid", Message: msg})
	}
	for _, id := range r.PlanIDs {
		if !uuidPattern.MatchString(id) {
			bad("plan_ids", "Choose plans from the list.")
			break
		}
	}
	for _, s := range r.SubscriptionStatuses {
		if !slices.Contains(subscriptionStatuses, s) {
			bad("subscription_statuses", "Unknown subscription status.")
			break
		}
	}
	for _, s := range r.Lifecycle {
		if !slices.Contains(lifecycles, s) {
			bad("lifecycle", "Lifecycle is prospect, subscriber or lapsed.")
			break
		}
	}
	for _, d := range []string{r.JoinedFrom, r.JoinedTo} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				bad("joined", "Dates are YYYY-MM-DD.")
			}
		}
	}
	for _, reg := range r.Regions {
		if !vocab.Regions.Has(reg) {
			bad("regions", "Choose regions from the list.")
			break
		}
	}
	for _, d := range r.Dietary {
		if !vocab.Dietary.Has(d) {
			bad("dietary", "Choose dietary preferences from the list.")
			break
		}
	}
	for _, s := range r.Sources {
		if !sourcePattern.MatchString(s) {
			bad("sources", "Sources are lowercase words such as instagram or direct.")
			break
		}
	}
	if r.MinOrders != nil && (*r.MinOrders < 0 || *r.MinOrders > 1000) {
		bad("min_orders", "Minimum orders must be between 0 and 1,000.")
	}
	if r.MinTenureWeeks != nil && (*r.MinTenureWeeks < 0 || *r.MinTenureWeeks > 520) {
		bad("min_tenure_weeks", "Tenure must be between 0 and 520 weeks.")
	}
	if r.ConsentTopic != "" && (!vocab.Topics.Has(r.ConsentTopic) || r.ConsentTopic == "account_security") {
		bad("consent_topic", "Choose an optional email topic.")
	}
	return f
}

// Segment is a saved segment.
type Segment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Rules       Rules     `json:"rules"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func segmentView(s store.CrmSegment) Segment {
	v := Segment{ID: s.ID, Name: s.Name, Description: s.Description, UpdatedAt: s.UpdatedAt}
	_ = json.Unmarshal(s.Rules, &v.Rules)
	return v
}

func (s *Service) Segments(ctx context.Context) ([]Segment, error) {
	rows, err := store.New(s.pool).ListSegments(ctx)
	if err != nil {
		return nil, err
	}
	out := []Segment{}
	for _, r := range rows {
		out = append(out, segmentView(r))
	}
	return out, nil
}

func (s *Service) Segment(ctx context.Context, id string) (*Segment, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	r, err := store.New(s.pool).GetSegment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v := segmentView(r)
	return &v, nil
}

type SegmentInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Rules       Rules  `json:"rules"`
}

func (in *SegmentInput) clean() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	f := in.Rules.Validate()
	if n := len([]rune(in.Name)); n < 1 || n > 80 {
		f = append(f, httpx.FieldError{Field: "name", Code: "length", Message: "Name the segment (up to 80 characters)."})
	}
	if len([]rune(in.Description)) > 300 {
		f = append(f, httpx.FieldError{Field: "description", Code: "length", Message: "Keep the description under 300 characters."})
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	return nil
}

func (s *Service) SaveSegment(ctx context.Context, actor, id string, in SegmentInput) (*Segment, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	rules, _ := json.Marshal(in.Rules)
	var out Segment
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		var row store.CrmSegment
		var err error
		if id == "" {
			row, err = q.CreateSegment(ctx, store.CreateSegmentParams{Name: in.Name, Description: in.Description, Rules: rules, Actor: nilIfEmpty(actor)})
		} else {
			row, err = q.UpdateSegment(ctx, store.UpdateSegmentParams{ID: id, Name: in.Name, Description: in.Description, Rules: rules, Actor: nilIfEmpty(actor)})
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
		}
		if db.IsUniqueViolation(err, "crm_segments_name_key") {
			return httpx.ValidationError([]httpx.FieldError{{Field: "name", Code: "taken", Message: "Another segment has this name."}})
		}
		if err != nil {
			return err
		}
		out = segmentView(row)
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "segment.saved", TargetType: "segment", TargetID: row.ID})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) DeleteSegment(ctx context.Context, actor, id string) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		n, err := store.New(tx).DeleteSegment(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return httpx.ErrNotFound
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "segment.deleted", TargetType: "segment", TargetID: id})
	})
}

// ---------------------------------------------------------------- campaigns

var (
	Objectives       = []string{"acquisition", "activation", "retention", "win_back", "referral", "awareness"}
	Channels         = []string{"email", "social", "paid_social", "search", "partnership", "events", "other"}
	utmPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,59}$`)
	campaignStatuses = map[string][]string{
		"draft":     {"scheduled", "active", "cancelled"},
		"scheduled": {"draft", "active", "cancelled"},
		"active":    {"completed", "cancelled"},
		"completed": {},
		"cancelled": {},
	}
)

// Campaign is a campaign record. Nothing is sent from Mise: sending is for
// an approved marketing provider, and none is connected.
type Campaign struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Objective   string     `json:"objective"`
	Channel     string     `json:"channel"`
	Status      string     `json:"status"`
	UTMCampaign *string    `json:"utm_campaign"`
	SegmentID   *string    `json:"segment_id"`
	PromotionID *string    `json:"promotion_id"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Notes       string     `json:"notes"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func campaignView(c store.MarketingCampaign) Campaign {
	return Campaign{ID: c.ID, Name: c.Name, Objective: c.Objective, Channel: c.Channel, Status: c.Status, UTMCampaign: c.UtmCampaign,
		SegmentID: c.SegmentID, PromotionID: c.PromotionID, StartsAt: c.StartsAt, EndsAt: c.EndsAt, Notes: c.Notes,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func (s *Service) Campaigns(ctx context.Context) ([]Campaign, error) {
	rows, err := store.New(s.pool).ListCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	out := []Campaign{}
	for _, r := range rows {
		out = append(out, campaignView(r))
	}
	return out, nil
}

func (s *Service) Campaign(ctx context.Context, id string) (*Campaign, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	r, err := store.New(s.pool).GetCampaign(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v := campaignView(r)
	return &v, nil
}

type CampaignInput struct {
	Name        string     `json:"name"`
	Objective   string     `json:"objective"`
	Channel     string     `json:"channel"`
	UTMCampaign string     `json:"utm_campaign"`
	SegmentID   string     `json:"segment_id"`
	PromotionID string     `json:"promotion_id"`
	StartsAt    *time.Time `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at"`
	Notes       string     `json:"notes"`
}

func (in *CampaignInput) clean() error {
	var f []httpx.FieldError
	add := func(field, msg string) { f = append(f, httpx.FieldError{Field: field, Code: "invalid", Message: msg}) }
	in.Name, in.Notes = strings.TrimSpace(in.Name), strings.TrimSpace(in.Notes)
	in.UTMCampaign = strings.ToLower(strings.TrimSpace(in.UTMCampaign))
	if n := len([]rune(in.Name)); n < 1 || n > 100 {
		add("name", "Name the campaign (up to 100 characters).")
	}
	if !slices.Contains(Objectives, in.Objective) {
		add("objective", "Choose an objective.")
	}
	if !slices.Contains(Channels, in.Channel) {
		add("channel", "Choose a channel.")
	}
	if in.UTMCampaign != "" && !utmPattern.MatchString(in.UTMCampaign) {
		add("utm_campaign", "Use lowercase letters, numbers, hyphens and underscores.")
	}
	for field, id := range map[string]string{"segment_id": in.SegmentID, "promotion_id": in.PromotionID} {
		if id != "" && !uuidPattern.MatchString(id) {
			add(field, "Choose from the list.")
		}
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		add("ends_at", "The end must be after the start.")
	}
	if len([]rune(in.Notes)) > 2000 {
		add("notes", "Keep notes under 2,000 characters.")
	}
	if len(f) > 0 {
		return httpx.ValidationError(f)
	}
	return nil
}

func (s *Service) SaveCampaign(ctx context.Context, actor, id string, in CampaignInput) (*Campaign, error) {
	if err := in.clean(); err != nil {
		return nil, err
	}
	var out Campaign
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		var row store.MarketingCampaign
		var err error
		if id == "" {
			row, err = q.CreateCampaign(ctx, store.CreateCampaignParams{Name: in.Name, Objective: in.Objective, Channel: in.Channel,
				UtmCampaign: nilIfEmpty(in.UTMCampaign), SegmentID: nilIfEmpty(in.SegmentID), PromotionID: nilIfEmpty(in.PromotionID),
				StartsAt: in.StartsAt, EndsAt: in.EndsAt, Notes: in.Notes, Actor: nilIfEmpty(actor)})
		} else {
			cur, gerr := q.GetCampaign(ctx, id)
			if errors.Is(gerr, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			if gerr != nil {
				return gerr
			}
			if cur.Status == "completed" || cur.Status == "cancelled" {
				return httpx.NewError(http.StatusConflict, "campaign_closed", "A finished campaign can't be changed.")
			}
			row, err = q.UpdateCampaign(ctx, store.UpdateCampaignParams{ID: id, Name: in.Name, Objective: in.Objective, Channel: in.Channel,
				UtmCampaign: nilIfEmpty(in.UTMCampaign), SegmentID: nilIfEmpty(in.SegmentID), PromotionID: nilIfEmpty(in.PromotionID),
				StartsAt: in.StartsAt, EndsAt: in.EndsAt, Notes: in.Notes, Actor: nilIfEmpty(actor)})
		}
		if db.IsUniqueViolation(err, "marketing_campaigns_utm_key") {
			return httpx.ValidationError([]httpx.FieldError{{Field: "utm_campaign", Code: "taken", Message: "Another campaign uses this UTM value."}})
		}
		if db.IsForeignKeyViolation(err) {
			return httpx.ValidationError([]httpx.FieldError{{Field: "segment_id", Code: "invalid", Message: "The segment or promotion no longer exists."}})
		}
		if err != nil {
			return err
		}
		out = campaignView(row)
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "campaign.saved", TargetType: "campaign", TargetID: row.ID})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SetCampaignStatus moves a campaign along draft, scheduled, active,
// completed or cancelled. Activating sends nothing.
func (s *Service) SetCampaignStatus(ctx context.Context, actor, id, status string) (*Campaign, error) {
	var out Campaign
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		cur, err := q.GetCampaign(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if !slices.Contains(campaignStatuses[cur.Status], status) {
			return httpx.NewError(http.StatusConflict, "invalid_transition", "A "+cur.Status+" campaign can't become "+status+".")
		}
		row, err := q.SetCampaignStatus(ctx, store.SetCampaignStatusParams{ID: id, Status: status, Actor: nilIfEmpty(actor)})
		if err != nil {
			return err
		}
		out = campaignView(row)
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "campaign." + status, TargetType: "campaign", TargetID: id,
			Metadata: map[string]any{"from": cur.Status}})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// MarketingProvider is where approved marketing tools (HubSpot, Klaviyo)
// will plug in to sync segments and campaigns. No implementation exists:
// no provider has been approved and no credentials exist.
type MarketingProvider interface {
	Name() string
	SyncSegment(ctx context.Context, segment Segment, customerIDs []string) error
}
