package forecasting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/culinary"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/qty"
	"mise.tt/api/internal/production"
)

const day = "2006-01-02"

// NotReady is the answer whenever the gate is not passed.
const NotReady = "Not enough verified historical demand to generate a reliable forecast."

type Service struct {
	pool     *pgxpool.Pool
	Location *time.Location
	Now      func() time.Time
}

func NewService(pool *pgxpool.Pool, loc *time.Location) *Service {
	return &Service{pool: pool, Location: loc, Now: time.Now}
}

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// thisWeek is the Monday of the current local week; history stops before it
// (an unfinished week is never treated as complete demand).
func (s *Service) thisWeek() time.Time {
	t := s.Now().In(s.Location)
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// ---------------------------------------------------------------- settings

// Settings are the activation criteria. They ship unset: the business
// decides them, and without them no forecast is produced.
type Settings struct {
	MinWeeks             *int      `json:"min_weeks"`
	MaxMissingShare      *float64  `json:"max_missing_share"`
	MinImprovement       *float64  `json:"min_improvement"`
	MinIntervalResiduals *int      `json:"min_interval_residuals"`
	SeasonWeeks          *int      `json:"season_weeks"`
	HorizonWeeks         int       `json:"horizon_weeks"`
	ApprovalRef          string    `json:"approval_ref"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (s *Service) Settings(ctx context.Context, q db.Querier) (*Settings, error) {
	var x Settings
	err := q.QueryRow(ctx, `SELECT min_weeks, max_missing_share::float8, min_improvement::float8, min_interval_residuals, season_weeks, horizon_weeks,
		approval_ref, updated_at FROM forecast_settings`).Scan(&x.MinWeeks, &x.MaxMissingShare, &x.MinImprovement, &x.MinIntervalResiduals,
		&x.SeasonWeeks, &x.HorizonWeeks, &x.ApprovalRef, &x.UpdatedAt)
	return &x, err
}

type SettingsInput struct {
	MinWeeks             *int     `json:"min_weeks"`
	MaxMissingShare      *float64 `json:"max_missing_share"`
	MinImprovement       *float64 `json:"min_improvement"`
	MinIntervalResiduals *int     `json:"min_interval_residuals"`
	SeasonWeeks          *int     `json:"season_weeks"`
	ClearSeason          bool     `json:"clear_season"`
	HorizonWeeks         *int     `json:"horizon_weeks"`
	ApprovalRef          *string  `json:"approval_ref"`
}

func (s *Service) SaveSettings(ctx context.Context, actor string, in SettingsInput) (*Settings, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		cur, err := s.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if in.MinWeeks != nil {
			if *in.MinWeeks < 4 || *in.MinWeeks > 520 {
				return fieldErr("min_weeks", "Use 4 to 520 weeks.")
			}
			cur.MinWeeks = in.MinWeeks
		}
		if in.MaxMissingShare != nil {
			if *in.MaxMissingShare < 0 || *in.MaxMissingShare > 1 {
				return fieldErr("max_missing_share", "Use a share from 0 to 1.")
			}
			cur.MaxMissingShare = in.MaxMissingShare
		}
		if in.MinImprovement != nil {
			if *in.MinImprovement < 0 || *in.MinImprovement > 1 {
				return fieldErr("min_improvement", "Use a share from 0 to 1.")
			}
			cur.MinImprovement = in.MinImprovement
		}
		if in.MinIntervalResiduals != nil {
			if *in.MinIntervalResiduals < 5 || *in.MinIntervalResiduals > 1000 {
				return fieldErr("min_interval_residuals", "Use 5 to 1,000.")
			}
			cur.MinIntervalResiduals = in.MinIntervalResiduals
		}
		if in.ClearSeason {
			cur.SeasonWeeks = nil
		} else if in.SeasonWeeks != nil {
			if *in.SeasonWeeks < 2 || *in.SeasonWeeks > 52 {
				return fieldErr("season_weeks", "Use 2 to 52 weeks.")
			}
			cur.SeasonWeeks = in.SeasonWeeks
		}
		if in.HorizonWeeks != nil {
			if *in.HorizonWeeks < 1 || *in.HorizonWeeks > 12 {
				return fieldErr("horizon_weeks", "Use 1 to 12 weeks.")
			}
			cur.HorizonWeeks = *in.HorizonWeeks
		}
		if in.ApprovalRef != nil {
			cur.ApprovalRef = strings.TrimSpace(*in.ApprovalRef)
			if len([]rune(cur.ApprovalRef)) > 200 {
				return fieldErr("approval_ref", "Keep it under 200 characters.")
			}
		}
		if cur.MinWeeks != nil && cur.ApprovalRef == "" {
			return fieldErr("approval_ref", "Name the decision that approved these criteria.")
		}
		if _, err := tx.Exec(ctx, `UPDATE forecast_settings SET min_weeks = $1, max_missing_share = $2, min_improvement = $3, min_interval_residuals = $4,
			season_weeks = $5, horizon_weeks = $6, approval_ref = $7, updated_by = $8, updated_at = now()`, cur.MinWeeks, cur.MaxMissingShare,
			cur.MinImprovement, cur.MinIntervalResiduals, cur.SeasonWeeks, cur.HorizonWeeks, cur.ApprovalRef, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "forecasting.settings_saved", TargetType: "forecast_settings",
			Metadata: map[string]any{"min_weeks": cur.MinWeeks, "max_missing_share": cur.MaxMissingShare, "min_improvement": cur.MinImprovement,
				"season_weeks": cur.SeasonWeeks, "horizon_weeks": cur.HorizonWeeks, "approval_ref": cur.ApprovalRef}})
	})
	if err != nil {
		return nil, err
	}
	return s.Settings(ctx, s.pool)
}

// ---------------------------------------------------------------- history

// series is one meal's verified weekly demand, on the weeks it was offered.
type series struct {
	MealID, MealName string
	Weeks            []time.Time
	Values           []float64
}

// verified is the definition of usable demand: a confirmed (not pending or
// cancelled) order, paid through a real (non-test) payment provider, for a
// meal that is not demo content.
const verified = `o.status NOT IN ('pending', 'cancelled') AND m.is_demo = false
	AND EXISTS (SELECT 1 FROM payments p WHERE p.id = o.payment_id AND p.provider <> 'testpay')`

func (s *Service) history(ctx context.Context, q db.Querier, before time.Time) ([]series, error) {
	// Weeks each meal was on a published menu, with its verified kits (0 when offered but not ordered).
	rows, err := q.Query(ctx, `
		WITH offered AS (
			SELECT DISTINCT mm.meal_id, wm.week_start FROM weekly_menu_meals mm JOIN weekly_menus wm ON wm.id = mm.menu_id
			WHERE wm.status IN ('published', 'closed', 'archived') AND wm.week_start < $1),
		demand AS (
			SELECT i.meal_id, o.week_start, sum(i.quantity)::float8 AS kits FROM order_items i JOIN orders o ON o.id = i.order_id JOIN meals m ON m.id = i.meal_id
			WHERE `+verified+` AND o.week_start < $1 GROUP BY 1, 2),
		first AS (SELECT meal_id, min(week_start) AS w FROM demand GROUP BY 1)
		SELECT f.meal_id::text, m.name, f.week_start, coalesce(d.kits, 0)
		FROM offered f JOIN first ON first.meal_id = f.meal_id AND f.week_start >= first.w
		JOIN meals m ON m.id = f.meal_id
		LEFT JOIN demand d ON d.meal_id = f.meal_id AND d.week_start = f.week_start
		ORDER BY m.name, f.meal_id, f.week_start`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []series
	for rows.Next() {
		var id, name string
		var w time.Time
		var v float64
		if err := rows.Scan(&id, &name, &w, &v); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].MealID != id {
			out = append(out, series{MealID: id, MealName: name})
		}
		cur := &out[len(out)-1]
		cur.Weeks, cur.Values = append(cur.Weeks, w), append(cur.Values, v)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- readiness

type Criterion struct {
	Name     string  `json:"name"`
	Required *string `json:"required"` // nil: not decided
	Actual   string  `json:"actual"`
	Met      bool    `json:"met"`
}

type Readiness struct {
	Ready            bool        `json:"ready"`
	Message          string      `json:"message"`
	Reasons          []string    `json:"reasons"`
	Criteria         []Criterion `json:"criteria"`
	HistoryFrom      *string     `json:"history_from"`
	HistoryTo        *string     `json:"history_to"`
	VerifiedWeeks    int         `json:"verified_weeks"`
	SpanWeeks        int         `json:"span_weeks"`
	MissingShare     *float64    `json:"missing_share"`
	VerifiedKits     float64     `json:"verified_kits"`
	MealsWithHistory int         `json:"meals_with_history"`
	TestPaidKits     float64     `json:"test_paid_kits"`
	UnpaidKits       float64     `json:"unpaid_kits"`
	DemoMealKits     float64     `json:"demo_meal_kits"`
	CancelledOrders  int         `json:"cancelled_orders"`
	ConfirmedOrders  int         `json:"confirmed_orders"`
	OutlierWeeks     int         `json:"outlier_weeks"`
	NewMealsLast8    int         `json:"new_meals_last_8_weeks"`
	SeasonalCoverage *string     `json:"seasonal_coverage"`
	Settings         *Settings   `json:"settings"`
}

func f2(v float64) string { return fmt.Sprintf("%.2f", v) }

// Readiness explains whether history is good enough to forecast. With no
// criteria configured the answer is always no.
func (s *Service) Readiness(ctx context.Context) (*Readiness, error) {
	return s.readiness(ctx, s.pool)
}

func (s *Service) readiness(ctx context.Context, q db.Querier) (*Readiness, error) {
	before := s.thisWeek()
	set, err := s.Settings(ctx, q)
	if err != nil {
		return nil, err
	}
	r := &Readiness{Settings: set, Reasons: []string{}, Criteria: []Criterion{}}
	var from, to *time.Time
	if err := q.QueryRow(ctx, `
		SELECT min(o.week_start), max(o.week_start), count(DISTINCT o.week_start)::int, coalesce(sum(i.quantity), 0)::float8, count(DISTINCT i.meal_id)::int
		FROM order_items i JOIN orders o ON o.id = i.order_id JOIN meals m ON m.id = i.meal_id WHERE `+verified+` AND o.week_start < $1`, before).
		Scan(&from, &to, &r.VerifiedWeeks, &r.VerifiedKits, &r.MealsWithHistory); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT coalesce(sum(i.quantity) FILTER (WHERE p.provider = 'testpay' AND NOT m.is_demo), 0)::float8,
		       coalesce(sum(i.quantity) FILTER (WHERE o.payment_id IS NULL AND NOT m.is_demo), 0)::float8,
		       coalesce(sum(i.quantity) FILTER (WHERE m.is_demo), 0)::float8
		FROM order_items i JOIN orders o ON o.id = i.order_id JOIN meals m ON m.id = i.meal_id LEFT JOIN payments p ON p.id = o.payment_id
		WHERE o.status NOT IN ('pending', 'cancelled') AND o.week_start < $1`, before).Scan(&r.TestPaidKits, &r.UnpaidKits, &r.DemoMealKits); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'cancelled')::int, count(*) FILTER (WHERE status NOT IN ('pending', 'cancelled'))::int
		FROM orders WHERE week_start < $1`, before).Scan(&r.CancelledOrders, &r.ConfirmedOrders); err != nil {
		return nil, err
	}
	hist, err := s.history(ctx, q, before)
	if err != nil {
		return nil, err
	}
	for _, h := range hist {
		r.OutlierWeeks += outliers(h.Values)
		if len(h.Weeks) > 0 && !h.Weeks[0].Before(before.AddDate(0, 0, -56)) {
			r.NewMealsLast8++
		}
	}
	if from != nil {
		a, b := from.Format(day), to.Format(day)
		r.HistoryFrom, r.HistoryTo = &a, &b
		r.SpanWeeks = int(to.Sub(*from).Hours()/24/7) + 1
		m := 1 - float64(r.VerifiedWeeks)/float64(r.SpanWeeks)
		r.MissingShare = &m
	}
	if set.SeasonWeeks != nil {
		c := fmt.Sprintf("%d of %d weeks (two full seasons needed)", r.SpanWeeks, 2**set.SeasonWeeks)
		r.SeasonalCoverage = &c
	}

	add := func(name string, required *string, actual string, met bool) {
		r.Criteria = append(r.Criteria, Criterion{Name: name, Required: required, Actual: actual, Met: met})
	}
	str := func(s string) *string { return &s }
	if set.MinWeeks == nil {
		add("Weeks of verified history", nil, fmt.Sprint(r.VerifiedWeeks), false)
	} else {
		add("Weeks of verified history", str(fmt.Sprint(*set.MinWeeks)), fmt.Sprint(r.VerifiedWeeks), r.VerifiedWeeks >= *set.MinWeeks)
	}
	missing := "n/a"
	if r.MissingShare != nil {
		missing = f2(*r.MissingShare)
	}
	if set.MaxMissingShare == nil {
		add("Share of missing weeks", nil, missing, false)
	} else {
		add("Share of missing weeks", str(f2(*set.MaxMissingShare)+" or less"), missing, r.MissingShare != nil && *r.MissingShare <= *set.MaxMissingShare)
	}
	if set.MinImprovement == nil {
		add("Required improvement over the naive forecast", nil, "measured at run time", false)
	} else {
		add("Required improvement over the naive forecast", str(f2(*set.MinImprovement)), "measured at run time", true)
	}
	if set.SeasonWeeks != nil {
		add("Seasonal coverage", str(fmt.Sprint(2**set.SeasonWeeks)+" weeks"), fmt.Sprint(r.SpanWeeks), r.SpanWeeks >= 2**set.SeasonWeeks)
	}
	if set.ApprovalRef == "" {
		r.Reasons = append(r.Reasons, "Forecast activation criteria have not been approved by the business.")
	}
	for _, c := range r.Criteria {
		switch {
		case c.Required == nil:
			r.Reasons = append(r.Reasons, c.Name+": no threshold has been decided.")
		case !c.Met:
			r.Reasons = append(r.Reasons, c.Name+": "+c.Actual+", needs "+*c.Required+".")
		}
	}
	if r.VerifiedWeeks == 0 {
		r.Reasons = append(r.Reasons, "There is no verified demand: every order so far was paid through the test provider, was free, or was for demo meals.")
	}
	r.Ready = len(r.Reasons) == 0
	r.Message = "Ready: verified history meets the approved criteria."
	if !r.Ready {
		r.Message = NotReady
	}
	return r, nil
}

// outliers counts weeks beyond 3 interquartile ranges from the quartiles.
func outliers(x []float64) int {
	if len(x) < 8 {
		return 0
	}
	s := append([]float64(nil), x...)
	sort.Float64s(s)
	q1, q3 := quantile(s, 0.25), quantile(s, 0.75)
	iqr := q3 - q1
	n := 0
	for _, v := range x {
		if v < q1-3*iqr || v > q3+3*iqr {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------- runs

type Value struct {
	MealID   string   `json:"meal_id"`
	MealName string   `json:"meal_name"`
	Week     string   `json:"week"`
	Expected float64  `json:"expected"`
	Lower    *float64 `json:"lower"`
	Upper    *float64 `json:"upper"`
}

type BacktestRow struct {
	Method   string   `json:"method"`
	Folds    int      `json:"folds"`
	MAE      *float64 `json:"mae"`
	WAPE     *float64 `json:"wape"`
	Bias     *float64 `json:"bias"`
	MASE     *float64 `json:"mase"`
	Selected bool     `json:"selected"`
}

type Run struct {
	ID            string          `json:"id"`
	Trigger       string          `json:"trigger"`
	ActorName     *string         `json:"actor_name"`
	Status        string          `json:"status"`
	InputFrom     *string         `json:"input_from"`
	InputTo       *string         `json:"input_to"`
	Method        *string         `json:"method"`
	MethodVersion *string         `json:"method_version"`
	HorizonWeeks  int             `json:"horizon_weeks"`
	Readiness     json.RawMessage `json:"readiness"`
	Notes         string          `json:"notes"`
	CreatedAt     time.Time       `json:"created_at"`
	Values        []Value         `json:"values,omitempty"`
	Backtests     []BacktestRow   `json:"backtests,omitempty"`
}

// Run records a forecast attempt. When the gate is not passed it records a
// refused run (with the readiness that refused it) and produces nothing.
func (s *Service) Run(ctx context.Context, actor, trigger string) (*Run, error) {
	var id string
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('forecast-run', 8))`); err != nil {
			return err
		}
		rd, err := s.readiness(ctx, tx)
		if err != nil {
			return err
		}
		set := rd.Settings
		readiness, _ := json.Marshal(rd)
		if !rd.Ready {
			if err := tx.QueryRow(ctx, `INSERT INTO forecast_runs (trigger, actor_user_id, status, horizon_weeks, readiness, notes)
				VALUES ($1, $2, 'refused', $3, $4, $5) RETURNING id::text`, trigger, nilIfEmpty(actor), set.HorizonWeeks, readiness, NotReady).Scan(&id); err != nil {
				return err
			}
			return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "forecasting.run_refused", TargetType: "forecast_run", TargetID: id})
		}
		before := s.thisWeek()
		hist, err := s.history(ctx, tx, before)
		if err != nil {
			return err
		}
		season := 0
		if set.SeasonWeeks != nil {
			season = *set.SeasonWeeks
		}
		minTrain := 4
		if season > minTrain {
			minTrain = season
		}
		// Only meals with at least the approved minimum of weeks are forecast.
		var usable []series
		skipped := []string{}
		for _, h := range hist {
			if len(h.Values) >= *set.MinWeeks && len(h.Values) > minTrain {
				usable = append(usable, h)
			} else {
				skipped = append(skipped, h.MealName)
			}
		}
		cands := Candidates(season)
		scores := make([]Score, 0, len(cands))
		for _, m := range cands {
			parts := make([]Score, 0, len(usable))
			for _, h := range usable {
				parts = append(parts, Backtest(m, h.Values, minTrain, set.HorizonWeeks))
			}
			scores = append(scores, Combine(m.Name(), parts))
		}
		best, ok := Select(scores, *set.MinImprovement)
		notes := ""
		if len(skipped) > 0 {
			notes = "Not forecast (too little history): " + strings.Join(skipped, ", ") + "."
		}
		if !ok || len(usable) == 0 {
			rd.Ready, rd.Message = false, NotReady
			rd.Reasons = append(rd.Reasons, "No meal has enough weeks of verified history to backtest.")
			readiness, _ = json.Marshal(rd)
			if err := tx.QueryRow(ctx, `INSERT INTO forecast_runs (trigger, actor_user_id, status, horizon_weeks, readiness, notes)
				VALUES ($1, $2, 'refused', $3, $4, $5) RETURNING id::text`, trigger, nilIfEmpty(actor), set.HorizonWeeks, readiness, notes).Scan(&id); err != nil {
				return err
			}
			return nil
		}
		var method Method
		for _, m := range cands {
			if m.Name() == best.Method {
				method = m
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO forecast_runs (trigger, actor_user_id, status, input_from, input_to, method, method_version, horizon_weeks, readiness, notes)
			VALUES ($1, $2, 'completed', $3, $4, $5, $6, $7, $8, $9) RETURNING id::text`, trigger, nilIfEmpty(actor), rd.HistoryFrom, rd.HistoryTo,
			method.Name(), method.Version(), set.HorizonWeeks, readiness, notes).Scan(&id); err != nil {
			return err
		}
		for _, sc := range scores {
			if _, err := tx.Exec(ctx, `INSERT INTO forecast_backtests (run_id, method, folds, mae, wape, bias, mase, selected) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				id, sc.Method, sc.Folds, sc.MAE, sc.WAPE, sc.Bias, sc.MASE, sc.Method == best.Method); err != nil {
				return err
			}
		}
		minRes := 0
		if set.MinIntervalResiduals != nil {
			minRes = *set.MinIntervalResiduals
		}
		lo, hi, haveInterval := Interval(best.Residuals, minRes)
		haveInterval = haveInterval && set.MinIntervalResiduals != nil
		for _, h := range usable {
			f := method.Forecast(h.Values, set.HorizonWeeks)
			for i, v := range f {
				v = math.Max(v, 0)
				var lower, upper *float64
				if haveInterval {
					l, u := math.Max(v+lo, 0), math.Max(v+hi, 0)
					lower, upper = &l, &u
				}
				if _, err := tx.Exec(ctx, `INSERT INTO forecast_values (run_id, meal_id, week, expected, lower, upper) VALUES ($1, $2, $3, round($4::numeric, 2), round($5::numeric, 2), round($6::numeric, 2))`,
					id, h.MealID, before.AddDate(0, 0, 7*i), v, lower, upper); err != nil {
					return err
				}
			}
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "forecasting.run_completed", TargetType: "forecast_run", TargetID: id,
			Metadata: map[string]any{"method": method.Name(), "meals": len(usable)}})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Runs(ctx context.Context) ([]Run, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id::text, r.trigger, NULLIF(trim(coalesce(u.first_name, '') || ' ' || coalesce(u.last_name, '')), ''), r.status,
		to_char(r.input_from, 'YYYY-MM-DD'), to_char(r.input_to, 'YYYY-MM-DD'), r.method, r.method_version, r.horizon_weeks, r.readiness, r.notes, r.created_at
		FROM forecast_runs r LEFT JOIN users u ON u.id = r.actor_user_id ORDER BY r.created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Run, error) {
		var x Run
		err := row.Scan(&x.ID, &x.Trigger, &x.ActorName, &x.Status, &x.InputFrom, &x.InputTo, &x.Method, &x.MethodVersion, &x.HorizonWeeks, &x.Readiness,
			&x.Notes, &x.CreatedAt)
		return x, err
	})
}

func (s *Service) Get(ctx context.Context, id string) (*Run, error) {
	var x Run
	err := s.pool.QueryRow(ctx, `SELECT r.id::text, r.trigger, NULLIF(trim(coalesce(u.first_name, '') || ' ' || coalesce(u.last_name, '')), ''), r.status,
		to_char(r.input_from, 'YYYY-MM-DD'), to_char(r.input_to, 'YYYY-MM-DD'), r.method, r.method_version, r.horizon_weeks, r.readiness, r.notes, r.created_at
		FROM forecast_runs r LEFT JOIN users u ON u.id = r.actor_user_id WHERE r.id::text = $1`, id).
		Scan(&x.ID, &x.Trigger, &x.ActorName, &x.Status, &x.InputFrom, &x.InputTo, &x.Method, &x.MethodVersion, &x.HorizonWeeks, &x.Readiness, &x.Notes, &x.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT v.meal_id::text, m.name, to_char(v.week, 'YYYY-MM-DD'), v.expected::float8, v.lower::float8, v.upper::float8
		FROM forecast_values v JOIN meals m ON m.id = v.meal_id WHERE v.run_id = $1 ORDER BY v.week, m.name`, x.ID)
	if err != nil {
		return nil, err
	}
	if x.Values, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Value]); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT method, folds, mae::float8, wape::float8, bias::float8, mase::float8, selected FROM forecast_backtests
		WHERE run_id = $1 ORDER BY mae NULLS LAST, method`, x.ID)
	if err != nil {
		return nil, err
	}
	x.Backtests, err = pgx.CollectRows(rows, pgx.RowToStructByPos[BacktestRow])
	return &x, err
}

// ---------------------------------------------------------------- ingredients

type IngredientNeed struct {
	IngredientID   string  `json:"ingredient_id"`
	IngredientName string  `json:"ingredient_name"`
	Preparation    string  `json:"preparation"`
	Quantity       *string `json:"quantity"`
	Unit           string  `json:"unit"`
	Status         string  `json:"status"`
	Approximate    bool    `json:"approximate"`
	Note           string  `json:"note"`
}

type IngredientProjection struct {
	RunID    string           `json:"run_id"`
	Week     string           `json:"week"`
	Label    string           `json:"label"`
	Needs    []IngredientNeed `json:"needs"`
	NoRecipe []string         `json:"meals_without_recipe"`
}

// Ingredients projects a completed run's expected kits for one week through
// the production requirement engine (the same one production plans use),
// with each meal's published recipe for that week. It is computed on
// request and never stored in, or written to, a plan or purchase order.
func (s *Service) Ingredients(ctx context.Context, runID, week string) (*IngredientProjection, error) {
	w, err := time.Parse(day, week)
	if err != nil {
		return nil, fieldErr("week", "Choose a forecast week.")
	}
	run, err := s.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != "completed" {
		return nil, httpx.NewError(http.StatusConflict, "no_forecast", NotReady)
	}
	out := &IngredientProjection{RunID: run.ID, Week: week, Needs: []IngredientNeed{}, NoRecipe: []string{},
		Label: "Forecast, not demand: expected kits from run " + run.CreatedAt.Format("2 Jan 2006 15:04") + ". Not a plan or an order."}
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET TRANSACTION READ ONLY`); err != nil {
			return err
		}
		conv, err := culinary.NewConverter(ctx, tx)
		if err != nil {
			return err
		}
		var lines []production.RequirementLine
		for _, v := range run.Values {
			if v.Week != week || v.Expected <= 0 {
				continue
			}
			version, ok, err := culinary.RecipeVersionForMeal(ctx, tx, v.MealID, w)
			if err != nil {
				return err
			}
			if !ok {
				out.NoRecipe = append(out.NoRecipe, v.MealName)
				continue
			}
			kits, err := qty.Parse(fmt.Sprintf("%.2f", v.Expected))
			if err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				SELECT v.id, v.yield_servings, m.servings, l.id, l.ingredient_id, trim_scale(l.quantity)::text, l.unit, l.preparation,
				       trim_scale(l.yield_percent)::text, ing.base_unit, ing.active, ing.name
				FROM recipe_versions v JOIN recipes r ON r.id = v.recipe_id JOIN meals m ON m.id = r.meal_id
				JOIN recipe_version_ingredients l ON l.version_id = v.id JOIN ingredients ing ON ing.id = l.ingredient_id
				WHERE v.id = $1 AND v.status = 'published'`, version)
			if err != nil {
				return err
			}
			for rows.Next() {
				var r production.RequirementLine
				var yield, servings int32
				if err := rows.Scan(&r.VersionID, &yield, &servings, &r.LineID, &r.IngredientID, &r.Amount, &r.Unit, &r.Preparation, &r.YieldPercent,
					&r.BaseUnit, &r.Active, &r.IngredientName); err != nil {
					rows.Close()
					return err
				}
				r.Ref, r.Kits, r.YieldServings, r.ServingsPerKit = v.MealID, kits, int(yield), int(servings)
				lines = append(lines, r)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		needs, err := production.AggregateRequirements(ctx, conv, lines)
		if err != nil {
			return err
		}
		for _, n := range needs {
			out.Needs = append(out.Needs, IngredientNeed{IngredientID: n.IngredientID, IngredientName: n.IngredientName, Preparation: n.Preparation,
				Quantity: n.Quantity, Unit: n.Unit, Status: n.Status, Approximate: n.Approximate, Note: n.Note})
		}
		return nil
	})
	return out, err
}
