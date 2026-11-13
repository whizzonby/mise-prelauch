package culinary

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// Chef is a chef as the culinary team sees them: the catalogue chef card
// (Sprint 2) plus the staff account it is linked to, specialities, working
// status and recipe counts. Contact details come from the linked staff
// account and are shown only to staff who may read staff accounts.
type Chef struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	DisplayName  string   `json:"display_name"`
	RoleTitle    *string  `json:"role_title"`
	CardStatus   string   `json:"card_status"`
	WorkStatus   string   `json:"work_status"`
	Specialities []string `json:"specialities"`
	UserID       *string  `json:"user_id"`
	UserName     *string  `json:"user_name"`
	Email        *string  `json:"email"`
	Recipes      int      `json:"recipes"`
	InReview     int      `json:"in_review"`
	Published    int      `json:"published"`
}

func (s *Service) Chefs(ctx context.Context, p *identity.Principal) ([]Chef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.slug, c.display_name, c.role_title, c.status, c.work_status, c.specialities, c.user_id,
		       NULLIF(btrim(u.first_name || ' ' || u.last_name), ''), u.email,
		       (SELECT count(*) FROM recipes r WHERE r.chef_id = c.id)::int,
		       (SELECT count(*) FROM recipes r JOIN recipe_versions v ON v.recipe_id = r.id WHERE r.chef_id = c.id AND v.status IN ('submitted', 'under_review'))::int,
		       (SELECT count(DISTINCT r.id) FROM recipes r JOIN recipe_versions v ON v.recipe_id = r.id WHERE r.chef_id = c.id AND v.status = 'published')::int
		FROM chef_profiles c LEFT JOIN users u ON u.id = c.user_id
		WHERE NOT c.is_demo OR c.user_id IS NOT NULL
		ORDER BY c.work_status, c.display_name`)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Chef])
	if err != nil {
		return nil, err
	}
	if !p.Can(identity.PermUsersRead) {
		for i := range list {
			list[i].Email = nil
		}
	}
	return list, nil
}

// ChefInput links a chef card to a staff account and sets team details.
type ChefInput struct {
	// "" unlinks.
	UserID       *string   `json:"user_id"`
	Specialities *[]string `json:"specialities"`
	WorkStatus   *string   `json:"work_status"`
}

func (s *Service) UpdateChef(ctx context.Context, p *identity.Principal, id string, in ChefInput) ([]Chef, error) {
	if !uuidPattern.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chef_profiles WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpx.ErrNotFound
		}
		if in.UserID != nil {
			var user *string
			if *in.UserID != "" {
				var staff bool
				if !uuidPattern.MatchString(*in.UserID) {
					return fieldErr("user_id", "Choose a staff member.")
				}
				// Linked accounts must be staff holding recipe permissions:
				// the chef role, or a culinary role.
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_key = ur.role_key
					WHERE ur.user_id = $1 AND rp.permission_key = 'recipes.create')`, *in.UserID).Scan(&staff); err != nil {
					return err
				}
				if !staff {
					return fieldErr("user_id", "Link a staff account that holds the chef role.")
				}
				user = in.UserID
			}
			if _, err := tx.Exec(ctx, `UPDATE chef_profiles SET user_id = $2, updated_at = now() WHERE id = $1`, id, user); err != nil {
				if strings.Contains(err.Error(), "chef_profiles_user_key") {
					return fieldErr("user_id", "That staff account is already linked to another chef.")
				}
				return err
			}
		}
		if in.Specialities != nil {
			list := []string{}
			for _, sp := range *in.Specialities {
				sp = clean(sp)
				if sp == "" {
					continue
				}
				if runes(sp) > 60 || len(list) >= 12 {
					return fieldErr("specialities", "Up to 12 specialities of 60 characters each.")
				}
				list = append(list, sp)
			}
			if _, err := tx.Exec(ctx, `UPDATE chef_profiles SET specialities = $2, updated_at = now() WHERE id = $1`, id, list); err != nil {
				return err
			}
		}
		if in.WorkStatus != nil {
			if *in.WorkStatus != "active" && *in.WorkStatus != "inactive" {
				return fieldErr("work_status", "Status is active or inactive.")
			}
			if _, err := tx.Exec(ctx, `UPDATE chef_profiles SET work_status = $2, updated_at = now() WHERE id = $1`, id, *in.WorkStatus); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "chef.updated", TargetType: "chef_profile", TargetID: id})
	})
	if err != nil {
		return nil, err
	}
	return s.Chefs(ctx, p)
}
