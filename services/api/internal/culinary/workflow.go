package culinary

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/outbox"
	"mise.tt/api/internal/platform/qty"
)

// The recipe version state machine. Each action names the statuses it
// starts from; anything else is refused.
//
//	draft | changes_requested --submit--> submitted
//	submitted --culinary approve--> under_review
//	submitted | under_review --request changes / reject--> changes_requested
//	under_review --nutrition sign-off--> (stays under_review, signed off)
//	under_review --final approve--> approved
//	approved --publish--> published --archive--> archived
type action struct {
	name string
	from []string
}

var (
	actSubmit    = action{"submit", []string{"draft", "changes_requested"}}
	actCulinary  = action{"culinary review", []string{"submitted"}}
	actNutrition = action{"nutrition review", []string{"under_review"}}
	actChanges   = action{"request changes", []string{"submitted", "under_review"}}
	actApprove   = action{"approve", []string{"under_review"}}
	actPublish   = action{"publish", []string{"approved"}}
	actArchive   = action{"archive", []string{"published"}}
)

func (a action) allowed(status string) bool {
	for _, s := range a.from {
		if s == status {
			return true
		}
	}
	return false
}

func invalidTransition(a action, status string) error {
	return conflict("invalid_transition", "A version that is "+strings.ReplaceAll(status, "_", " ")+" cannot "+a.name+".")
}

var errOwnWork = httpx.NewError(403, "own_recipe", "You wrote this version, so someone else must review or approve it.")

// open locks the recipe's latest version for a workflow step.
func (s *Service) open(ctx context.Context, tx pgx.Tx, recipeID string, a action) (*Version, error) {
	if !uuidPattern.MatchString(recipeID) {
		return nil, httpx.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM recipe_versions WHERE recipe_id = $1 FOR UPDATE`, recipeID); err != nil {
		return nil, err
	}
	v, err := s.version(ctx, tx, recipeID, 0)
	if err != nil {
		return nil, err
	}
	if !a.allowed(v.Status) {
		return nil, invalidTransition(a, v.Status)
	}
	return v, nil
}

func (s *Service) setStatus(ctx context.Context, tx pgx.Tx, v *Version, status string, extra string, args ...any) error {
	sql := `UPDATE recipe_versions SET status = $2, updated_at = now()` + extra + ` WHERE id = $1`
	_, err := tx.Exec(ctx, sql, append([]any{v.ID, status}, args...)...)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE recipes SET updated_at = now() WHERE id = $1`, v.RecipeID)
	return err
}

func (s *Service) review(ctx context.Context, tx pgx.Tx, v *Version, actor, stage, decision, notes string) error {
	_, err := tx.Exec(ctx, `INSERT INTO recipe_reviews (version_id, stage, decision, notes, reviewer_id) VALUES ($1, $2, $3, $4, $5)`,
		v.ID, stage, decision, notes, nilIfEmpty(actor))
	return err
}

func event(ctx context.Context, tx pgx.Tx, typ string, v *Version, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["recipe_id"], payload["version"] = v.RecipeID, v.Number
	return outbox.Record(ctx, tx, outbox.Event{Type: typ, AggregateType: "recipe_version", AggregateID: v.ID,
		DedupeKey: fmt.Sprint(time.Now().UnixNano()), Payload: payload})
}

func cleanNotes(notes string, required bool) (string, error) {
	notes = strings.TrimSpace(notes)
	if runes(notes) > 2000 {
		return "", fieldErr("notes", "Keep the notes under 2,000 characters.")
	}
	if required && runes(notes) < 3 {
		return "", fieldErr("notes", "Say what needs to change.")
	}
	return notes, nil
}

// Submit sends the open draft for review.
func (s *Service) Submit(ctx context.Context, p *identity.Principal, recipeID, notes string) (*Version, error) {
	notes, err := cleanNotes(notes, false)
	if err != nil {
		return nil, err
	}
	var number int
	err = s.tx(ctx, func(tx pgx.Tx) error {
		v, err := s.open(ctx, tx, recipeID, actSubmit)
		if err != nil {
			return err
		}
		number = v.Number
		if ok, err := s.canEdit(ctx, tx, p, recipeID); err != nil {
			return err
		} else if !ok {
			return errNotYours
		}
		if probs := submitProblems(v); len(probs) > 0 {
			return &httpx.Error{Status: 422, Code: "incomplete", Message: strings.Join(probs, " ")}
		}
		if err := s.setStatus(ctx, tx, v, "submitted", `, submitted_at = now()`); err != nil {
			return err
		}
		if notes != "" {
			// A chef's note to the reviewers rides on the change summary.
			if _, err := tx.Exec(ctx, `UPDATE recipe_versions SET change_summary = $2 WHERE id = $1 AND change_summary = ''`, v.ID, notes); err != nil {
				return err
			}
		}
		if err := event(ctx, tx, "recipe.submitted", v, nil); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.submitted", TargetType: "recipe_version", TargetID: v.ID,
			Metadata: map[string]any{"version": v.Number}})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

// ReviewInput is a reviewer's decision.
type ReviewInput struct {
	// approved, changes_requested or rejected.
	Decision string `json:"decision"`
	Notes    string `json:"notes"`
}

// CulinaryReview is the culinary director's (recipes.review) decision on a
// submitted version.
func (s *Service) CulinaryReview(ctx context.Context, p *identity.Principal, recipeID string, in ReviewInput) (*Version, error) {
	if in.Decision != "approved" {
		return s.RequestChanges(ctx, p, recipeID, "culinary", in)
	}
	notes, err := cleanNotes(in.Notes, false)
	if err != nil {
		return nil, err
	}
	var number int
	err = s.tx(ctx, func(tx pgx.Tx) error {
		v, err := s.open(ctx, tx, recipeID, actCulinary)
		if err != nil {
			return err
		}
		number = v.Number
		if v.AuthorID != nil && *v.AuthorID == p.UserID {
			return errOwnWork
		}
		if err := s.review(ctx, tx, v, p.UserID, "culinary", "approved", notes); err != nil {
			return err
		}
		if err := s.setStatus(ctx, tx, v, "under_review", ""); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.culinary_approved", TargetType: "recipe_version", TargetID: v.ID})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

// RequestChanges sends a version back to its chef. stage is culinary or
// nutrition; a rejection is recorded as such and also returns the version
// for changes (nothing is deleted).
func (s *Service) RequestChanges(ctx context.Context, p *identity.Principal, recipeID, stage string, in ReviewInput) (*Version, error) {
	if in.Decision == "" {
		in.Decision = "changes_requested"
	}
	if in.Decision != "changes_requested" && in.Decision != "rejected" {
		return nil, fieldErr("decision", "Decision is approved, changes_requested or rejected.")
	}
	notes, err := cleanNotes(in.Notes, true)
	if err != nil {
		return nil, err
	}
	var number int
	err = s.tx(ctx, func(tx pgx.Tx) error {
		v, err := s.open(ctx, tx, recipeID, actChanges)
		if err != nil {
			return err
		}
		number = v.Number
		if stage == "nutrition" && v.Status != "under_review" {
			return conflict("invalid_transition", "Nutrition review starts after the culinary review.")
		}
		if v.AuthorID != nil && *v.AuthorID == p.UserID {
			return errOwnWork
		}
		if err := s.review(ctx, tx, v, p.UserID, stage, in.Decision, notes); err != nil {
			return err
		}
		if err := s.setStatus(ctx, tx, v, "changes_requested", ""); err != nil {
			return err
		}
		if stage == "nutrition" {
			if err := event(ctx, tx, "nutrition.review_completed", v, map[string]any{"decision": in.Decision}); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe." + in.Decision, TargetType: "recipe_version", TargetID: v.ID,
			Metadata: map[string]any{"stage": stage}})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

// NutritionApprove is a nutritionist's sign-off of the version's nutrition
// and allergen information. It needs complete nutrition with a source, and
// every ingredient allergen declared.
func (s *Service) NutritionApprove(ctx context.Context, p *identity.Principal, recipeID, notes string) (*Version, error) {
	notes, err := cleanNotes(notes, false)
	if err != nil {
		return nil, err
	}
	var number int
	err = s.tx(ctx, func(tx pgx.Tx) error {
		v, err := s.open(ctx, tx, recipeID, actNutrition)
		if err != nil {
			return err
		}
		number = v.Number
		if v.AuthorID != nil && *v.AuthorID == p.UserID {
			return errOwnWork
		}
		if v.Signoff.Nutrition {
			return conflict("already_signed", "Nutrition is already signed off for this version.")
		}
		var probs []string
		if !v.Nutrition.Complete {
			probs = append(probs, "Nutrition per serving is incomplete: every figure and its source are needed before sign-off.")
		}
		if len(v.MissingAllergens) > 0 {
			probs = append(probs, "Allergens of the ingredients are not all declared: "+strings.Join(v.MissingAllergens, ", ")+".")
		}
		if len(probs) > 0 {
			return &httpx.Error{Status: 422, Code: "incomplete", Message: strings.Join(probs, " ")}
		}
		if err := s.review(ctx, tx, v, p.UserID, "nutrition", "approved", notes); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE recipe_versions SET updated_at = now() WHERE id = $1`, v.ID); err != nil {
			return err
		}
		if err := event(ctx, tx, "nutrition.review_completed", v, map[string]any{"decision": "approved"}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.nutrition_approved", TargetType: "recipe_version", TargetID: v.ID})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

// Approve gives final approval. The version becomes immutable.
func (s *Service) Approve(ctx context.Context, p *identity.Principal, recipeID, notes string) (*Version, error) {
	notes, err := cleanNotes(notes, false)
	if err != nil {
		return nil, err
	}
	var number int
	err = s.tx(ctx, func(tx pgx.Tx) error {
		v, err := s.open(ctx, tx, recipeID, actApprove)
		if err != nil {
			return err
		}
		number = v.Number
		if v.AuthorID != nil && *v.AuthorID == p.UserID {
			return errOwnWork
		}
		probs := problems(v)
		if !v.Signoff.Culinary {
			probs = append(probs, "The culinary review has not approved this version.")
		}
		if !v.Signoff.Nutrition {
			probs = append(probs, "A nutritionist has not signed off this version's nutrition and allergens.")
		}
		if len(probs) > 0 {
			return &httpx.Error{Status: 422, Code: "incomplete", Message: strings.Join(probs, " ")}
		}
		if err := s.review(ctx, tx, v, p.UserID, "final", "approved", notes); err != nil {
			return err
		}
		if err := s.setStatus(ctx, tx, v, "approved", `, approved_by = $3, approved_at = now()`, p.UserID); err != nil {
			return err
		}
		if err := event(ctx, tx, "recipe.approved", v, nil); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.approved", TargetType: "recipe_version", TargetID: v.ID,
			Metadata: map[string]any{"version": v.Number}})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

// PublishInput chooses the first delivery week the version is used for.
type PublishInput struct {
	EffectiveFrom string `json:"effective_from"`
}

// Publish makes an approved version eligible for production from a
// delivery week whose selection has not closed, and, if the recipe is
// linked to a meal, sets the meal's allergen declaration and verified
// nutrition from it.
func (s *Service) Publish(ctx context.Context, p *identity.Principal, recipeID string, in PublishInput) (*Version, error) {
	now := s.Now()
	week := s.currentWeek()
	if in.EffectiveFrom != "" {
		w, err := time.ParseInLocation("2006-01-02", in.EffectiveFrom, time.UTC)
		if err != nil || w.Weekday() != time.Monday {
			return nil, fieldErr("effective_from", "Choose a delivery week (a Monday).")
		}
		week = w
	}
	if s.Weeks != nil && !now.Before(s.Weeks.CutoffFor(week)) {
		return nil, fieldErr("effective_from", "Selections for that week have closed; choose the next open week or later.")
	}
	var number int
	err := s.tx(ctx, func(tx pgx.Tx) error {
		v, err := s.open(ctx, tx, recipeID, actPublish)
		if err != nil {
			return err
		}
		number = v.Number
		if err := s.review(ctx, tx, v, p.UserID, "publish", "published", ""); err != nil {
			return err
		}
		if err := s.setStatus(ctx, tx, v, "published", `, published_by = $3, published_at = now(), effective_from = $4`, p.UserID, week.Format("2006-01-02")); err != nil {
			return err
		}
		var mealID *string
		if err := tx.QueryRow(ctx, `SELECT meal_id FROM recipes WHERE id = $1`, recipeID).Scan(&mealID); err != nil {
			return err
		}
		if mealID != nil && s.Catalog != nil {
			var nutritionist string
			_ = tx.QueryRow(ctx, `SELECT reviewer_id FROM recipe_reviews WHERE version_id = $1 AND stage = 'nutrition' AND decision = 'approved'
				AND invalidated_at IS NULL ORDER BY created_at DESC LIMIT 1`, v.ID).Scan(&nutritionist)
			if err := s.Catalog.ApplyRecipeClaims(ctx, tx, p.UserID, claims(*mealID, nutritionist, v)); err != nil {
				return err
			}
		}
		if err := event(ctx, tx, "recipe.published", v, map[string]any{"effective_from": week.Format("2006-01-02"), "meal_id": mealID}); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.published", TargetType: "recipe_version", TargetID: v.ID,
			Metadata: map[string]any{"version": v.Number, "effective_from": week.Format("2006-01-02")}})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

func claims(mealID, nutritionist string, v *Version) MealClaims {
	n := v.Nutrition
	round := func(s *string) string { return qty.MustParse(*s).Format(1) }
	c := MealClaims{MealID: mealID, Allergens: v.Contains, NutritionByUser: nutritionist,
		ProteinG: round(n.ProteinG), CarbsG: round(n.CarbsG), FatG: round(n.FatG)}
	e, _ := qty.MustParse(*n.EnergyKcal).Round(0).Rat().Float64()
	c.EnergyKcal = int(e)
	fibre := round(n.FibreG)
	c.FibreG = &fibre
	sodium, _ := qty.MustParse(*n.SodiumMg).Round(0).Rat().Float64()
	si := int(sodium)
	c.SodiumMg = &si
	return c
}

// Archive withdraws a published version from future production planning.
// Batches already made from it keep it.
func (s *Service) Archive(ctx context.Context, p *identity.Principal, recipeID string, number int, reason string) (*Version, error) {
	reason = strings.TrimSpace(reason)
	if n := runes(reason); n < 3 || n > 500 {
		return nil, fieldErr("reason", "Say why the version is withdrawn.")
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM recipe_versions WHERE recipe_id = $1 FOR UPDATE`, recipeID); err != nil {
			return err
		}
		v, err := s.version(ctx, tx, recipeID, number)
		if err != nil {
			return err
		}
		if !actArchive.allowed(v.Status) {
			return invalidTransition(actArchive, v.Status)
		}
		if err := s.setStatus(ctx, tx, v, "archived", `, archived_at = now()`); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.archived", TargetType: "recipe_version", TargetID: v.ID,
			Reason: reason, Metadata: map[string]any{"version": v.Number}})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

// Revise starts a new draft from the latest version. Content is copied;
// reviews and approvals are not.
func (s *Service) Revise(ctx context.Context, p *identity.Principal, recipeID, summary string) (*Version, error) {
	summary = strings.TrimSpace(summary)
	if runes(summary) > 500 {
		return nil, fieldErr("change_summary", "Keep the summary under 500 characters.")
	}
	var number int
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if !uuidPattern.MatchString(recipeID) {
			return httpx.ErrNotFound
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM recipe_versions WHERE recipe_id = $1 FOR UPDATE`, recipeID); err != nil {
			return err
		}
		ok, err := s.canEdit(ctx, tx, p, recipeID)
		if err != nil {
			return err
		}
		if !ok {
			return errNotYours
		}
		var from string
		var status string
		if err := tx.QueryRow(ctx, `SELECT id, number, status FROM recipe_versions WHERE recipe_id = $1 ORDER BY number DESC LIMIT 1`, recipeID).Scan(&from, &number, &status); err != nil {
			return notFoundIf(err)
		}
		if status != "approved" && status != "published" && status != "archived" {
			return conflict("revision_open", "Version "+fmt.Sprint(number)+" is still open ("+strings.ReplaceAll(status, "_", " ")+"). Finish or edit it instead.")
		}
		number++
		var vid string
		if err := tx.QueryRow(ctx, `
			INSERT INTO recipe_versions (recipe_id, number, yield_servings, serving_size, serving_size_g, kitchen_prep_minutes, customer_cook_minutes,
			       storage_instructions, packaging_instructions, customer_instructions, dietary_tags, cross_contact_note, change_summary, author_id, based_on)
			SELECT recipe_id, $2, yield_servings, serving_size, serving_size_g, kitchen_prep_minutes, customer_cook_minutes,
			       storage_instructions, packaging_instructions, customer_instructions, dietary_tags, cross_contact_note, $3, $4, id
			FROM recipe_versions WHERE id = $1 RETURNING id`, from, number, summary, p.UserID).Scan(&vid); err != nil {
			return err
		}
		for _, sql := range []string{
			`INSERT INTO recipe_version_ingredients (version_id, position, ingredient_id, quantity, unit, preparation, yield_percent, optional, substitution, note)
			 SELECT $2, position, ingredient_id, quantity, unit, preparation, yield_percent, optional, substitution, note FROM recipe_version_ingredients WHERE version_id = $1`,
			`INSERT INTO recipe_steps (version_id, kind, position, body) SELECT $2, kind, position, body FROM recipe_steps WHERE version_id = $1`,
			`INSERT INTO recipe_allergens (version_id, allergen, kind) SELECT $2, allergen, kind FROM recipe_allergens WHERE version_id = $1`,
			`INSERT INTO recipe_nutrition (version_id, energy_kcal, protein_g, carbs_g, fat_g, sat_fat_g, fibre_g, sugar_g, sodium_mg, source, method)
			 SELECT $2, energy_kcal, protein_g, carbs_g, fat_g, sat_fat_g, fibre_g, sugar_g, sodium_mg, source, method FROM recipe_nutrition WHERE version_id = $1`,
		} {
			if _, err := tx.Exec(ctx, sql, from, vid); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE recipes SET updated_at = now() WHERE id = $1`, recipeID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "recipe.revised", TargetType: "recipe_version", TargetID: vid,
			Metadata: map[string]any{"version": number}})
	})
	if err != nil {
		return nil, err
	}
	return s.version(ctx, s.pool, recipeID, number)
}

// ReviewQueue lists versions waiting at a stage: culinary (submitted),
// nutrition (under review without a valid nutrition sign-off) or final
// (under review with both sign-offs).
func (s *Service) ReviewQueue(ctx context.Context, stage string) ([]QueueItem, error) {
	cond := map[string]string{
		"culinary":  `v.status = 'submitted'`,
		"nutrition": `v.status = 'under_review' AND NOT EXISTS (SELECT 1 FROM recipe_reviews x WHERE x.version_id = v.id AND x.stage = 'nutrition' AND x.decision = 'approved' AND x.invalidated_at IS NULL)`,
		"final":     `v.status = 'under_review' AND EXISTS (SELECT 1 FROM recipe_reviews x WHERE x.version_id = v.id AND x.stage = 'nutrition' AND x.decision = 'approved' AND x.invalidated_at IS NULL)`,
		"publish":   `v.status = 'approved'`,
	}[stage]
	if cond == "" {
		return nil, fieldErr("stage", "Stage is culinary, nutrition, final or publish.")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.code, r.name, v.number, v.status, COALESCE(c.display_name, ''), COALESCE(btrim(a.first_name || ' ' || a.last_name), ''),
		       v.submitted_at, v.updated_at, m.name
		FROM recipe_versions v JOIN recipes r ON r.id = v.recipe_id
		LEFT JOIN chef_profiles c ON c.id = r.chef_id LEFT JOIN users a ON a.id = v.author_id LEFT JOIN meals m ON m.id = r.meal_id
		WHERE `+cond+` ORDER BY v.submitted_at NULLS LAST, v.updated_at LIMIT 200`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[QueueItem])
}

type QueueItem struct {
	RecipeID    string     `json:"recipe_id"`
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	ChefName    string     `json:"chef_name"`
	AuthorName  string     `json:"author_name"`
	SubmittedAt *time.Time `json:"submitted_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	MealName    *string    `json:"meal_name"`
}
