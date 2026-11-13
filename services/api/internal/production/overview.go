package production

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/audit"
)

// Overview is the kitchen's day at a glance.
type Overview struct {
	Date     string       `json:"date"`
	TimeZone string       `json:"time_zone"`
	Counts   []Count      `json:"counts"`
	Today    []Batch      `json:"today"`
	Overdue  []Batch      `json:"overdue"`
	Holds    []Hold       `json:"holds"`
	Weeks    []WeekStatus `json:"weeks"`
	Setup    []string     `json:"setup"`
}

type Count struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Value      int    `json:"value"`
	Definition string `json:"definition"`
	Link       string `json:"link"`
}

// WeekStatus says where each nearby delivery week is in planning.
type WeekStatus struct {
	Week       string    `json:"week"`
	CutoffAt   time.Time `json:"cutoff_at"`
	Final      bool      `json:"final"`
	Orders     int       `json:"orders"`
	Kits       int       `json:"kits"`
	PlanID     *string   `json:"plan_id"`
	PlanNumber *string   `json:"plan_number"`
	PlanStatus *string   `json:"plan_status"`
}

func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	today := s.today()
	o := &Overview{Date: today.Format(day), TimeZone: s.Location.String(), Setup: []string{}}
	counts := []struct {
		Count
		sql string
	}{
		{Count{Key: "plans_draft", Label: "Plans awaiting approval", Definition: "Production plans in draft.", Link: "/production/plans?status=draft"},
			`SELECT count(*) FROM production_plans WHERE status = 'draft' AND $1::date IS NOT NULL`},
		{Count{Key: "due_today", Label: "Batches due today", Definition: "Batches with today's production date that are not completed or cancelled.", Link: "/production/batches?date=today"},
			`SELECT count(*) FROM production_batches WHERE production_date = $1 AND status NOT IN ('completed', 'cancelled')`},
		{Count{Key: "in_progress", Label: "In progress", Definition: "Batches started and not yet sent to quality checks.", Link: "/production/batches?status=in_progress"},
			`SELECT count(*) FROM production_batches WHERE status = 'in_progress' AND $1::date IS NOT NULL`},
		{Count{Key: "holds", Label: "Quality holds", Definition: "Open quality holds. Held batches cannot be released for fulfilment.", Link: "/production/quality"},
			`SELECT count(*) FROM quality_holds WHERE status = 'open' AND $1::date IS NOT NULL`},
		{Count{Key: "packing", Label: "Waiting to be packed", Definition: "Batches with output recorded whose packaging is not complete.", Link: "/production/packaging"},
			`SELECT count(*) FROM production_batches WHERE status = 'awaiting_quality_check' AND packaging_status <> 'completed' AND $1::date IS NOT NULL`},
		{Count{Key: "delayed", Label: "Delayed", Definition: "Batches past their production date and not completed or cancelled.", Link: "/production/batches?open=true"},
			`SELECT count(*) FROM production_batches WHERE production_date < $1 AND status NOT IN ('completed', 'cancelled')`},
		{Count{Key: "tasks_blocked", Label: "Blocked tasks", Definition: "Kitchen tasks marked blocked, with an exception noted.", Link: "/production/tasks?status=blocked"},
			`SELECT count(*) FROM production_tasks WHERE status = 'blocked' AND $1::date IS NOT NULL`},
	}
	for _, c := range counts {
		x := c.Count
		if err := s.pool.QueryRow(ctx, c.sql, today).Scan(&x.Value); err != nil {
			return nil, err
		}
		o.Counts = append(o.Counts, x)
	}
	var err error
	if o.Today, err = s.batches(ctx, s.pool, BatchFilter{From: today.Format(day), To: today.Format(day)}); err != nil {
		return nil, err
	}
	all, err := s.batches(ctx, s.pool, BatchFilter{To: today.AddDate(0, 0, -1).Format(day), Open: true})
	if err != nil {
		return nil, err
	}
	o.Overdue = all
	if o.Holds, err = s.holds(ctx, s.pool, "", true); err != nil {
		return nil, err
	}
	// The last delivery week, the one whose selection just closed, and the open one.
	open := s.Weeks.NextOpenWeek(s.Now())
	for _, w := range []time.Time{open.AddDate(0, 0, -14), open.AddDate(0, 0, -7), open} {
		ws := WeekStatus{Week: w.Format(day), CutoffAt: s.Weeks.CutoffFor(w)}
		ws.Final = !s.Now().Before(ws.CutoffAt)
		if err := s.pool.QueryRow(ctx, `SELECT count(DISTINCT o.id), coalesce(sum(i.quantity), 0) FROM orders o JOIN order_items i ON i.order_id = o.id
			WHERE o.week_start = $1 AND o.status NOT IN ('pending', 'cancelled')`, w).Scan(&ws.Orders, &ws.Kits); err != nil {
			return nil, err
		}
		var id, number, status string
		if err := s.pool.QueryRow(ctx, `SELECT id, number, status FROM production_plans WHERE week_start = $1 AND status <> 'cancelled'`, w).Scan(&id, &number, &status); err == nil {
			ws.PlanID, ws.PlanNumber, ws.PlanStatus = &id, &number, &status
		} else if err != pgx.ErrNoRows {
			return nil, err
		}
		o.Weeks = append(o.Weeks, ws)
	}
	var tasks, checks, recipes int
	var capacity *int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM production_task_templates WHERE active), (SELECT count(*) FROM quality_check_templates WHERE active),
		(SELECT count(*) FROM recipe_versions WHERE status = 'published'), (SELECT capacity_kits_per_day FROM production_settings)`).Scan(&tasks, &checks, &recipes, &capacity); err != nil {
		return nil, err
	}
	if checks == 0 {
		o.Setup = append(o.Setup, "No quality checklist is configured. Batches cannot complete until one is added from an approved procedure.")
	}
	if tasks == 0 {
		o.Setup = append(o.Setup, "No kitchen task templates are configured. Released batches will have no tasks.")
	}
	if recipes == 0 {
		o.Setup = append(o.Setup, "No recipe is published yet. Meals without a published recipe cannot be planned.")
	}
	if capacity == nil {
		o.Setup = append(o.Setup, "Kitchen capacity is not recorded, so plans are not checked against it.")
	}
	return o, nil
}

// CalendarDay is one day of the production calendar.
type CalendarDay struct {
	Date    string  `json:"date"`
	Weekday string  `json:"weekday"`
	Batches []Batch `json:"batches"`
	Plans   []Plan  `json:"plans"`
	// Proposed or approved schedule slots falling on this day.
	Slots    []string `json:"slots"`
	Cutoffs  []string `json:"cutoffs"`
	Warnings []string `json:"warnings"`
}

type Calendar struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	TimeZone string        `json:"time_zone"`
	Days     []CalendarDay `json:"days"`
	Schedule []Slot        `json:"schedule"`
}

type Slot struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	PrepWeekday     int    `json:"prep_weekday"`
	DeliveryWeekday int    `json:"delivery_weekday"`
	Status          string `json:"status"`
}

func (s *Service) slots(ctx context.Context) ([]Slot, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, label, prep_weekday, delivery_weekday, status FROM production_schedule_slots ORDER BY prep_weekday`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Slot])
}

// Calendar covers days days from from (a week from this Monday by default).
func (s *Service) Calendar(ctx context.Context, from string, days int) (*Calendar, error) {
	start, ok := parseDay(from)
	if !ok {
		t := s.today()
		start = t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
	}
	if days != 1 && days != 7 && days != 14 {
		days = 7
	}
	end := start.AddDate(0, 0, days-1)
	c := &Calendar{From: start.Format(day), To: end.Format(day), TimeZone: s.Location.String()}
	var err error
	if c.Schedule, err = s.slots(ctx); err != nil {
		return nil, err
	}
	batches, err := s.batches(ctx, s.pool, BatchFilter{From: c.From, To: c.To})
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+planCols+planFrom+` WHERE p.production_date BETWEEN $1 AND $2 AND p.status <> 'cancelled' ORDER BY p.production_date`, start, end)
	if err != nil {
		return nil, err
	}
	var plans []Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		plans = append(plans, p)
	}
	rows.Close()
	today := s.today()
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		cd := CalendarDay{Date: d.Format(day), Weekday: d.Weekday().String(), Batches: []Batch{}, Plans: []Plan{}, Slots: []string{}, Cutoffs: []string{}, Warnings: []string{}}
		iso := (int(d.Weekday())+6)%7 + 1
		for _, sl := range c.Schedule {
			if sl.Status == "retired" {
				continue
			}
			suffix := ""
			if sl.Status == "proposed" {
				suffix = " (proposed, not approved)"
			}
			if sl.PrepWeekday == iso {
				cd.Slots = append(cd.Slots, "Preparation: "+sl.Label+suffix)
			}
			if sl.DeliveryWeekday == iso {
				cd.Slots = append(cd.Slots, "Delivery: "+sl.Label+suffix)
			}
		}
		// The selection cutoff of the week that starts after this day.
		next := d.AddDate(0, 0, 7-((int(d.Weekday())+6)%7))
		if co := s.Weeks.CutoffFor(next).In(s.Location); co.Format(day) == d.Format(day) {
			cd.Cutoffs = append(cd.Cutoffs, "Meal selection closes "+co.Format("15:04")+" for the week of "+next.Format("2 Jan"))
		}
		for _, b := range batches {
			if b.ProductionDate == cd.Date {
				cd.Batches = append(cd.Batches, b)
				if b.Status == "quality_hold" {
					cd.Warnings = append(cd.Warnings, b.Code+" is on quality hold.")
				}
				if d.Before(today) && b.Status != "completed" && b.Status != "cancelled" {
					cd.Warnings = append(cd.Warnings, b.Code+" is past its production date.")
				}
			}
		}
		for _, p := range plans {
			if p.ProductionDate == cd.Date {
				cd.Plans = append(cd.Plans, p)
				if p.Status == "draft" {
					cd.Warnings = append(cd.Warnings, "Plan "+p.Number+" is not approved yet.")
				}
			}
		}
		c.Days = append(c.Days, cd)
	}
	return c, nil
}

// Settings are the kitchen's configuration.
type Settings struct {
	CapacityKitsPerDay *int   `json:"capacity_kits_per_day"`
	CapacityNote       string `json:"capacity_note"`
	Schedule           []Slot `json:"schedule"`
}

func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	var st Settings
	if err := s.pool.QueryRow(ctx, `SELECT capacity_kits_per_day, capacity_note FROM production_settings`).Scan(&st.CapacityKitsPerDay, &st.CapacityNote); err != nil {
		return nil, err
	}
	var err error
	st.Schedule, err = s.slots(ctx)
	return &st, err
}

type SettingsInput struct {
	// 0 or less clears it.
	CapacityKitsPerDay *int    `json:"capacity_kits_per_day"`
	CapacityNote       *string `json:"capacity_note"`
	// Slot id to status (proposed, approved, retired).
	SlotStatus map[string]string `json:"slot_status"`
}

func (s *Service) SaveSettings(ctx context.Context, actor string, in SettingsInput) (*Settings, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if in.CapacityKitsPerDay != nil {
			var v *int
			if *in.CapacityKitsPerDay > 0 {
				if *in.CapacityKitsPerDay > 100000 {
					return fieldErr("capacity_kits_per_day", "Enter kits per day.")
				}
				v = in.CapacityKitsPerDay
			}
			if _, err := tx.Exec(ctx, `UPDATE production_settings SET capacity_kits_per_day = $1, updated_by = $2, updated_at = now()`, v, nilIfEmpty(actor)); err != nil {
				return err
			}
		}
		if in.CapacityNote != nil {
			note := strings.TrimSpace(*in.CapacityNote)
			if runes(note) > 500 {
				return fieldErr("capacity_note", "Keep the note under 500 characters.")
			}
			if _, err := tx.Exec(ctx, `UPDATE production_settings SET capacity_note = $1, updated_by = $2, updated_at = now()`, note, nilIfEmpty(actor)); err != nil {
				return err
			}
		}
		for id, st := range in.SlotStatus {
			if !uuidPattern.MatchString(id) || (st != "proposed" && st != "approved" && st != "retired") {
				return fieldErr("slot_status", "Each slot is proposed, approved or retired.")
			}
			if _, err := tx.Exec(ctx, `UPDATE production_schedule_slots SET status = $2 WHERE id = $1`, id, st); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "production.settings_saved", TargetType: "production_settings"})
	})
	if err != nil {
		return nil, err
	}
	return s.Settings(ctx)
}
