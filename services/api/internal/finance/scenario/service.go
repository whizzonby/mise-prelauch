package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/finance"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// Service reads observed inputs and stores scenarios.
type Service struct {
	Pool    *pgxpool.Pool
	Finance *finance.Service
	Now     func() time.Time
}

// ObservedValue is one input computed from records.
type ObservedValue struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Unit    string   `json:"unit"`
	Value   *int64   `json:"value"`
	Basis   string   `json:"basis"`
	Sources []string `json:"sources"`
}

// ObservedSet is the observed starting point.
type ObservedSet struct {
	From        string          `json:"from"`
	To          string          `json:"to"`
	Months      int             `json:"months"`
	Values      []ObservedValue `json:"values"`
	Notes       []string        `json:"notes"`
	GeneratedAt time.Time       `json:"generated_at"`
}

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

func loc() *time.Location {
	if l, err := time.LoadLocation("America/Port_of_Spain"); err == nil {
		return l
	}
	return time.FixedZone("AST", -4*3600)
}

const day = "2006-01-02"

// Observe computes the observed inputs over the last `months` complete
// calendar months. Anything the records cannot support is nil with the
// reason; it is never zero by default. Test-provider payments are not sales.
func (s *Service) Observe(ctx context.Context, months int) (*ObservedSet, error) {
	if months == 0 {
		months = 3
	}
	if months < 1 || months > 12 {
		return nil, fieldErr("observed_months", "Between 1 and 12 months.")
	}
	now := s.Now().In(loc())
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	from, to := first.AddDate(0, -months, 0), first.AddDate(0, 0, -1)
	fromS, toS := from.Format(day), to.Format(day)
	days := int64(to.Sub(from).Hours()/24) + 1
	weeks := big.NewRat(days, 7)
	out := &ObservedSet{From: fromS, To: toS, Months: months, Values: []ObservedValue{}, Notes: []string{}, GeneratedAt: s.Now()}
	add := func(key string, v *int64, basis string, sources ...string) {
		def, _ := Find(key)
		if sources == nil {
			sources = []string{}
		}
		out.Values = append(out.Values, ObservedValue{Key: key, Label: def.Label, Unit: def.Unit, Value: v, Basis: basis, Sources: sources})
	}
	ratio := func(num, den int64, scale int64) *int64 {
		if den <= 0 {
			return nil
		}
		v := Round(big.NewRat(num*scale, den))
		return &v
	}
	perWeek := func(n int64) *int64 { v := Round(new(big.Rat).Quo(big.NewRat(n, 1), weeks)); return &v }
	period := fmt.Sprintf("%s to %s", fromS, toS)
	q := s.Pool

	// Real paid orders: kits and value (one kit per paid subscription-week).
	const realPaid = `JOIN payments p ON p.id = o.payment_id AND p.status = 'succeeded' AND p.provider <> 'testpay'`
	var realKits, realValue, allKits int64
	if err := q.QueryRow(ctx, `SELECT count(*), coalesce(sum(o.total_minor - o.tax_minor), 0) FROM orders o `+realPaid+`
		WHERE o.status <> 'cancelled' AND o.currency = 'TTD' AND o.week_start BETWEEN $1 AND $2`, fromS, toS).Scan(&realKits, &realValue); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM orders o JOIN payments p ON p.id = o.payment_id AND p.status = 'succeeded'
		WHERE o.status <> 'cancelled' AND o.week_start BETWEEN $1 AND $2`, fromS, toS).Scan(&allKits); err != nil {
		return nil, err
	}
	const liveSub = `s.status IN ('active', 'pause_scheduled', 'cancel_scheduled', 'past_due')`
	const realCycle = `FROM billing_cycles bc JOIN payments p ON p.billing_cycle_id = bc.id AND p.status = 'succeeded' AND p.provider <> 'testpay'`
	const realSub = `EXISTS (SELECT 1 ` + realCycle + ` WHERE bc.subscription_id = s.id)`
	var active, newSubs, cancelled int64
	if err := q.QueryRow(ctx, `SELECT count(*) FROM subscriptions s WHERE `+liveSub+` AND `+realSub).Scan(&active); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM (SELECT bc.subscription_id, min(bc.period_start) AS first `+realCycle+`
		GROUP BY bc.subscription_id) f WHERE f.first BETWEEN $1 AND $2`, fromS, toS).Scan(&newSubs); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `SELECT count(*) FROM subscriptions s WHERE s.cancelled_at IS NOT NULL
		AND (s.cancelled_at AT TIME ZONE 'America/Port_of_Spain')::date BETWEEN $1 AND $2 AND `+realSub, fromS, toS).Scan(&cancelled); err != nil {
		return nil, err
	}
	var renewOK, renewFail int64
	if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'succeeded'), count(*) FILTER (WHERE status = 'failed') FROM payments
		WHERE kind = 'off_session' AND provider <> 'testpay' AND (created_at AT TIME ZONE 'America/Port_of_Spain')::date BETWEEN $1 AND $2`, fromS, toS).
		Scan(&renewOK, &renewFail); err != nil {
		return nil, err
	}

	subSrc := []string{"subscriptions", "billing cycles with a succeeded real (non-test) payment"}
	add("active_subscriptions", &active, "Live subscriptions today (active, pause or cancel scheduled, past due) with at least one real paid billing cycle.", subSrc...)
	add("new_subscriptions_per_week", perWeek(newSubs), fmt.Sprintf("%d subscriptions had their first real paid week in %s, per week.", newSubs, period), subSrc...)
	if active > 0 {
		v := Round(new(big.Rat).Quo(big.NewRat(cancelled*10_000, active), weeks))
		add("weekly_churn_bps", &v, fmt.Sprintf("%d cancellations in %s per week, over today's %d live subscriptions.", cancelled, period, active), "subscriptions.cancelled_at")
	} else {
		add("weekly_churn_bps", nil, "No live subscriptions with real payments: churn cannot be observed.")
	}
	if n := renewOK + renewFail; n > 0 {
		add("renewal_success_bps", ratio(renewOK, n, 10_000), fmt.Sprintf("%d of %d real renewal charges in %s succeeded.", renewOK, n, period), "payments (off_session)")
	} else {
		add("renewal_success_bps", nil, "No real renewal charges in "+period+".")
	}
	if realKits > 0 {
		add("weekly_value_minor", ratio(realValue, realKits, 1), fmt.Sprintf("%d real paid orders in %s, total before tax divided by orders.", realKits, period), "orders.total_minor − tax_minor")
	} else {
		add("weekly_value_minor", nil, "No real paid orders in "+period+": the order value cannot be observed.")
	}

	// Costs from the management summary: only complete lines are used.
	sum, err := s.Finance.Summary(ctx, fromS, toS)
	if err != nil {
		return nil, err
	}
	line := func(key string) *finance.Line {
		for i := range sum.Lines {
			if sum.Lines[i].Key == key {
				return &sum.Lines[i]
			}
		}
		return nil
	}
	perKit := func(key, lineKey, label string, kits int64, kitsBasis string) {
		l := line(lineKey)
		switch {
		case l == nil || l.AmountMinor == nil || l.Coverage != finance.Complete:
			add(key, nil, label+" is not complete for "+period+".")
		case kits == 0:
			add(key, nil, "No kits in "+period+" to divide "+strings.ToLower(label)+" by.")
		default:
			add(key, ratio(*l.AmountMinor, kits, 1), fmt.Sprintf("%s in %s divided by %d %s.", label, period, kits, kitsBasis), l.Sources...)
		}
	}
	perKit("ingredient_cost_per_kit_minor", "ingredient_consumption", "Ingredient consumption", allKits, "paid kits (test orders included: they used real ingredients)")
	var delivered int64
	if err := q.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE status = 'delivered' AND service_date BETWEEN $1 AND $2`, fromS, toS).Scan(&delivered); err != nil {
		return nil, err
	}
	perKit("delivery_cost_per_kit_minor", "delivery_costs", "Delivery costs", delivered, "delivered deliveries")
	if fl := line("provider_fees"); fl != nil && fl.Coverage == finance.Complete && sum.RealPayments > 0 {
		g := line("gross_collections")
		add("provider_fee_bps", ratio(*fl.AmountMinor, *g.AmountMinor, 10_000), "Provider-reported fees over real gross collections in "+period+".", "payments.fee_minor")
	} else {
		add("provider_fee_bps", nil, "No complete provider fee records for real payments in "+period+".")
	}

	// Expense categories: a class is observed only when every category in it is recorded or confirmed for every month.
	class := func(key string, classes []string, names string) {
		var total int64
		var gaps []string
		est := false
		for _, c := range sum.Categories {
			for _, cl := range classes {
				if c.CostClass != cl {
					continue
				}
				if c.Coverage == finance.Missing || c.Coverage == finance.Partial {
					gaps = append(gaps, c.Name)
				}
				est = est || c.EstimatedMinor > 0
				total += c.ActualMinor + c.EstimatedMinor
			}
		}
		if len(gaps) > 0 {
			add(key, nil, fmt.Sprintf("Not recorded or confirmed for every month of %s: %s.", period, strings.Join(gaps, ", ")))
			return
		}
		basis := fmt.Sprintf("%s recorded in %s, per month.", names, period)
		if est {
			basis += " Includes estimates."
		}
		add(key, ratio(total, int64(months), 1), basis, "finance_expenses (approved)", "finance_coverage_confirmations")
	}
	class("fixed_costs_monthly_minor", []string{"fixed"}, "Fixed expense categories")
	class("labour_monthly_minor", []string{"labor"}, "Salaries and wages")
	class("marketing_monthly_minor", []string{"marketing"}, "Marketing")
	packCat := (*finance.CategoryCoverage)(nil)
	for i := range sum.Categories {
		if sum.Categories[i].Key == "packaging" {
			packCat = &sum.Categories[i]
		}
	}
	switch {
	case packCat == nil || packCat.Coverage == finance.Missing || packCat.Coverage == finance.Partial:
		add("packaging_cost_per_kit_minor", nil, "Packaging is not recorded or confirmed for every month of "+period+".")
	case allKits == 0:
		add("packaging_cost_per_kit_minor", nil, "No kits in "+period+".")
	default:
		add("packaging_cost_per_kit_minor", ratio(packCat.ActualMinor+packCat.EstimatedMinor, allKits, 1), fmt.Sprintf("Packaging in %s divided by %d paid kits.", period, allKits), "finance_expenses (packaging)")
	}
	add("capacity_kits_per_week", nil, "Production capacity is not recorded in the platform: enter it as an assumption.")
	if sum.Cash.BalanceMinor != nil {
		add("opening_cash_minor", sum.Cash.BalanceMinor, "Approved recorded cash balance as of "+strPtr(sum.Cash.BalanceAsOf)+".", "finance_cash_balances (approved)")
	} else {
		add("opening_cash_minor", nil, "No approved cash balance is recorded.")
	}
	if sum.TestPayments > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d test-provider payments in the period are not sales and are excluded.", sum.TestPayments))
	}
	return out, nil
}

func strPtr(s *string) string {
	if s == nil {
		return "unknown"
	}
	return *s
}

// Request is a scenario to run.
type Request struct {
	HorizonDays    int               `json:"horizon_days"`
	Preset         string            `json:"preset"`
	ObservedMonths int               `json:"observed_months"`
	Values         map[string]*int64 `json:"values"`
	Investments    []Investment      `json:"investments"`
	Changes        []Change          `json:"changes"`
}

// InputView is one resolved input for display.
type InputView struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`
	Value  *int64 `json:"value"`
	Source string `json:"source"`
	Basis  string `json:"basis"`
}

// Projection is a run: inputs with sources, assumptions, the output.
type Projection struct {
	Preset      string       `json:"preset"`
	HorizonDays int          `json:"horizon_days"`
	Inputs      []InputView  `json:"inputs"`
	Investments []Investment `json:"investments"`
	Changes     []Change     `json:"changes"`
	Assumptions []string     `json:"assumptions"`
	PresetNote  string       `json:"preset_note"`
	Observed    *ObservedSet `json:"observed"`
	Output      *Output      `json:"output"`
}

func view(in Inputs) []InputView {
	out := []InputView{}
	for _, v := range Variables {
		x := in.Values[v.Key]
		out = append(out, InputView{Key: v.Key, Label: v.Label, Unit: v.Unit, Value: x.Value, Source: x.Source, Basis: x.Basis})
	}
	return out
}

// Resolve builds inputs: observed values, then the preset, then entered
// assumptions, then what-if changes.
func (s *Service) Resolve(ctx context.Context, r Request) (*Projection, Inputs, error) {
	if r.Preset == "" {
		r.Preset = Base
	}
	obs, err := s.Observe(ctx, r.ObservedMonths)
	if err != nil {
		return nil, Inputs{}, err
	}
	in := Inputs{HorizonDays: r.HorizonDays, Values: map[string]Value{}, Investments: []Investment{}, Assumptions: []string{}}
	for _, o := range obs.Values {
		if o.Value != nil {
			in.Values[o.Key] = Value{Value: o.Value, Source: Observed, Basis: o.Basis}
		}
	}
	if err := ApplyPreset(&in, r.Preset); err != nil {
		return nil, in, fieldErr("preset", err.Error())
	}
	for k, v := range r.Values {
		def, ok := Find(k)
		if !ok {
			return nil, in, fieldErr("values."+k, "Unknown input.")
		}
		if v == nil {
			continue
		}
		val := *v
		in.Values[k] = Value{Value: &val, Source: Assumption, Basis: "Entered as an assumption."}
		in.Assumptions = append(in.Assumptions, fmt.Sprintf("%s set to %d (%s), entered.", def.Label, val, def.Unit))
	}
	for _, x := range r.Investments {
		in.Investments = append(in.Investments, x)
		in.Assumptions = append(in.Assumptions, fmt.Sprintf("One-off %s in week %d (%s).", money(x.AmountMinor), x.Week, x.Label))
	}
	if err := Validate(in); err != nil {
		return nil, in, fieldErr("inputs", err.Error())
	}
	if r.Changes == nil {
		r.Changes = []Change{}
	}
	if err := ApplyChanges(&in, r.Changes); err != nil {
		return nil, in, fieldErr("changes", err.Error())
	}
	if err := Validate(in); err != nil {
		return nil, in, fieldErr("changes", err.Error())
	}
	return &Projection{Preset: r.Preset, HorizonDays: r.HorizonDays, Inputs: view(in), Investments: in.Investments, Changes: r.Changes,
		Assumptions: in.Assumptions, PresetNote: PresetNote, Observed: obs, Output: Run(in)}, in, nil
}

// Scenario is a saved scenario with its versions.
type Scenario struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Preset      string     `json:"preset"`
	Archived    bool       `json:"archived"`
	CreatedAt   time.Time  `json:"created_at"`
	Versions    []*Version `json:"versions"`
}

// Version is a stored, immutable run.
type Version struct {
	ID            string       `json:"id"`
	ScenarioID    string       `json:"scenario_id"`
	ScenarioName  string       `json:"scenario_name"`
	Version       int          `json:"version"`
	HorizonDays   int          `json:"horizon_days"`
	EngineVersion string       `json:"engine_version"`
	InputHash     string       `json:"input_hash"`
	Note          string       `json:"note"`
	Projection    *Projection  `json:"projection"`
	Created       bool         `json:"created"`
	CreatedAt     time.Time    `json:"created_at"`
	stored        storedInputs `json:"-"`
}

type storedInputs struct {
	Values      map[string]Value `json:"values"`
	Investments []Investment     `json:"investments"`
	Assumptions []string         `json:"assumptions"`
	Preset      string           `json:"preset"`
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Create saves a new scenario with its first version.
func (s *Service) Create(ctx context.Context, p *identity.Principal, name, desc string, r Request) (*Scenario, error) {
	name, desc = strings.TrimSpace(name), strings.TrimSpace(desc)
	if name == "" || len([]rune(name)) > 120 {
		return nil, fieldErr("name", "Name the scenario (at most 120 characters).")
	}
	if len([]rune(desc)) > 1000 {
		return nil, fieldErr("description", "At most 1,000 characters.")
	}
	if r.Preset == "" {
		r.Preset = Base
	}
	proj, in, err := s.Resolve(ctx, r)
	if err != nil {
		return nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO finance_scenarios (name, description, preset, created_by, created_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		name, desc, r.Preset, p.UserID, s.Now()).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := s.insertVersion(ctx, tx, p, id, proj, in, ""); err != nil {
		return nil, err
	}
	if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "intelligence.scenario_created", TargetType: "finance_scenario", TargetID: id,
		Metadata: map[string]any{"preset": r.Preset, "horizon_days": r.HorizonDays}}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) insertVersion(ctx context.Context, tx pgx.Tx, p *identity.Principal, scenarioID string, proj *Projection, in Inputs, note string) (*Version, error) {
	if len([]rune(note)) > 500 {
		return nil, fieldErr("note", "At most 500 characters.")
	}
	inputs, _ := json.Marshal(storedInputs{Values: in.Values, Investments: in.Investments, Assumptions: in.Assumptions, Preset: proj.Preset})
	changes, _ := json.Marshal(proj.Changes)
	observed, _ := json.Marshal(proj.Observed)
	output, _ := json.Marshal(proj.Output)
	var v Version
	err := tx.QueryRow(ctx, `INSERT INTO finance_scenario_versions (scenario_id, version, horizon_days, inputs, changes, observed, engine_version, input_hash, output, note, created_by, created_at)
		SELECT $1::uuid, coalesce(max(version), 0) + 1, $2::int, $3::jsonb, $4::jsonb, $5::jsonb, $6::text, $7::text, $8::jsonb, $9::text, $10::uuid, $11::timestamptz
		FROM finance_scenario_versions WHERE scenario_id = $1::uuid
		ON CONFLICT (scenario_id, input_hash) DO NOTHING RETURNING id, version`,
		scenarioID, proj.HorizonDays, string(inputs), string(changes), string(observed), EngineVersion, proj.Output.InputHash, string(output), note, p.UserID, s.Now()).Scan(&v.ID, &v.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		// Identical inputs: the existing version stands.
		if err := tx.QueryRow(ctx, `SELECT id, version FROM finance_scenario_versions WHERE scenario_id = $1 AND input_hash = $2`,
			scenarioID, proj.Output.InputHash).Scan(&v.ID, &v.Version); err != nil {
			return nil, err
		}
		return &v, nil
	}
	if err != nil {
		return nil, err
	}
	v.Created = true
	return &v, nil
}

// AddVersion re-resolves the scenario with new inputs (observed values are
// read again) and stores a version unless the inputs are unchanged.
func (s *Service) AddVersion(ctx context.Context, p *identity.Principal, id string, r Request, note string) (*Version, error) {
	sc, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sc.Archived {
		return nil, httpx.NewError(http.StatusConflict, "scenario_archived", "This scenario is archived.")
	}
	if r.Preset == "" {
		r.Preset = sc.Preset
	}
	proj, in, err := s.Resolve(ctx, r)
	if err != nil {
		return nil, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	v, err := s.insertVersion(ctx, tx, p, id, proj, in, strings.TrimSpace(note))
	if err != nil {
		return nil, err
	}
	if v.Created {
		if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "intelligence.scenario_version_saved", TargetType: "finance_scenario", TargetID: id,
			Metadata: map[string]any{"version": v.Version}}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	out, err := s.GetVersion(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	out.Created = v.Created
	return out, nil
}

const versionCols = `v.id, v.scenario_id, sc.name, v.version, v.horizon_days, v.engine_version, v.input_hash, v.note, v.inputs, v.changes, v.observed, v.output, v.created_at`

func scanVersion(row pgx.Row) (*Version, error) {
	var v Version
	var inputs, changes, observed, output []byte
	if err := row.Scan(&v.ID, &v.ScenarioID, &v.ScenarioName, &v.Version, &v.HorizonDays, &v.EngineVersion, &v.InputHash, &v.Note,
		&inputs, &changes, &observed, &output, &v.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpx.ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(inputs, &v.stored)
	p := &Projection{Preset: v.stored.Preset, HorizonDays: v.HorizonDays, Investments: v.stored.Investments, Assumptions: v.stored.Assumptions,
		PresetNote: PresetNote, Output: &Output{}, Observed: &ObservedSet{}}
	_ = json.Unmarshal(changes, &p.Changes)
	_ = json.Unmarshal(observed, p.Observed)
	_ = json.Unmarshal(output, p.Output)
	p.Inputs = view(Inputs{Values: v.stored.Values})
	for _, x := range []*[]string{&p.Assumptions} {
		if *x == nil {
			*x = []string{}
		}
	}
	if p.Investments == nil {
		p.Investments = []Investment{}
	}
	if p.Changes == nil {
		p.Changes = []Change{}
	}
	v.Projection = p
	return &v, nil
}

// GetVersion returns one stored version.
func (s *Service) GetVersion(ctx context.Context, id string) (*Version, error) {
	if !uuidRe.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	return scanVersion(s.Pool.QueryRow(ctx, `SELECT `+versionCols+` FROM finance_scenario_versions v JOIN finance_scenarios sc ON sc.id = v.scenario_id WHERE v.id = $1`, id))
}

// Rerun recomputes a stored version from its stored inputs; the engine is
// deterministic, so the result equals the stored output (same engine version).
func (s *Service) Rerun(v *Version) *Output {
	return Run(Inputs{HorizonDays: v.HorizonDays, Values: v.stored.Values, Investments: v.stored.Investments})
}

// Get returns a scenario with all its versions (newest first).
func (s *Service) Get(ctx context.Context, id string) (*Scenario, error) {
	if !uuidRe.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	var sc Scenario
	var archived *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT id, name, description, preset, archived_at, created_at FROM finance_scenarios WHERE id = $1`, id).
		Scan(&sc.ID, &sc.Name, &sc.Description, &sc.Preset, &archived, &sc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sc.Archived = archived != nil
	rows, err := s.Pool.Query(ctx, `SELECT `+versionCols+` FROM finance_scenario_versions v JOIN finance_scenarios sc ON sc.id = v.scenario_id
		WHERE v.scenario_id = $1 ORDER BY v.version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sc.Versions = []*Version{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		sc.Versions = append(sc.Versions, v)
	}
	return &sc, rows.Err()
}

// Summary is a scenario in a list.
type Summary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Preset        string    `json:"preset"`
	Archived      bool      `json:"archived"`
	Versions      int       `json:"versions"`
	LatestVersion string    `json:"latest_version_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// List lists scenarios (archived ones only when asked).
func (s *Service) List(ctx context.Context, archived bool) ([]Summary, error) {
	rows, err := s.Pool.Query(ctx, `SELECT sc.id, sc.name, sc.description, sc.preset, sc.archived_at IS NOT NULL, count(v.id),
		coalesce((SELECT v2.id::text FROM finance_scenario_versions v2 WHERE v2.scenario_id = sc.id ORDER BY v2.version DESC LIMIT 1), ''), sc.created_at
		FROM finance_scenarios sc LEFT JOIN finance_scenario_versions v ON v.scenario_id = sc.id
		WHERE ($1 OR sc.archived_at IS NULL) GROUP BY sc.id ORDER BY sc.created_at DESC LIMIT 200`, archived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var x Summary
		if err := rows.Scan(&x.ID, &x.Name, &x.Description, &x.Preset, &x.Archived, &x.Versions, &x.LatestVersion, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Archive hides a scenario from the list; its versions are kept.
func (s *Service) Archive(ctx context.Context, p *identity.Principal, id string) (*Scenario, error) {
	if !uuidRe.MatchString(id) {
		return nil, httpx.ErrNotFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE finance_scenarios SET archived_at = coalesce(archived_at, now()) WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound
	}
	if err := audit.Record(ctx, tx, audit.Entry{ActorUserID: p.UserID, Action: "intelligence.scenario_archived", TargetType: "finance_scenario", TargetID: id}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Column is one version in a comparison.
type Column struct {
	VersionID   string      `json:"version_id"`
	Scenario    string      `json:"scenario"`
	Version     int         `json:"version"`
	Preset      string      `json:"preset"`
	HorizonDays int         `json:"horizon_days"`
	Complete    bool        `json:"complete"`
	Totals      *Totals     `json:"totals"`
	BreakEven   *BreakEven  `json:"break_even"`
	Cash        CashView    `json:"cash"`
	Inputs      []InputView `json:"inputs"`
	Assumptions []string    `json:"assumptions"`
}

// Comparison puts versions side by side.
type Comparison struct {
	Columns   []Column `json:"columns"`
	Note      string   `json:"note"`
	Statement string   `json:"statement"`
}

// Compare compares 2 to 4 stored versions.
func (s *Service) Compare(ctx context.Context, ids []string) (*Comparison, error) {
	if len(ids) < 2 || len(ids) > 4 {
		return nil, fieldErr("version_ids", "Compare 2 to 4 versions.")
	}
	out := &Comparison{Columns: []Column{}, Statement: PresetNote}
	horizons := map[int]bool{}
	for _, id := range ids {
		v, err := s.GetVersion(ctx, id)
		if err != nil {
			return nil, err
		}
		p := v.Projection
		horizons[v.HorizonDays] = true
		out.Columns = append(out.Columns, Column{VersionID: v.ID, Scenario: v.ScenarioName, Version: v.Version, Preset: p.Preset,
			HorizonDays: v.HorizonDays, Complete: p.Output.Complete, Totals: p.Output.Totals, BreakEven: p.Output.BreakEven,
			Cash: p.Output.Cash, Inputs: p.Inputs, Assumptions: p.Assumptions})
	}
	if len(horizons) > 1 {
		out.Note = "These versions use different horizons: compare totals with care."
	}
	return out, nil
}
