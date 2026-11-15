// Package automation is Mise's rules-based operational automation (Sprint 8).
//
// Rules are typed kinds with validated parameters, never user code.
// Evaluation (find the subjects that need attention) is separate from
// execution (perform one action for one subject, as a retried job). The only
// actions are an admin alert, an internal task and a request for approval:
// automation never refunds, charges, approves suppliers or orders, overrides
// a quality hold, dispatches, or changes a subscription. Actions write no
// domain events, so one automation cannot trigger another.
package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/platform/consumer"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
	"mise.tt/api/internal/platform/jobs"
	"mise.tt/api/internal/support"
)

// Alerts raises staff alerts (internal/backoffice implements it).
type Alerts interface {
	Raise(ctx context.Context, q db.Querier, a support.Alert) error
}

type Service struct {
	pool   *pgxpool.Pool
	Alerts Alerts
	Now    func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, Now: time.Now} }

func fieldErr(field, msg string) error {
	return httpx.ValidationError([]httpx.FieldError{{Field: field, Code: "invalid", Message: msg}})
}

func conflict(code, msg string) error { return httpx.NewError(http.StatusConflict, code, msg) }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------- kinds

// Param is one validated rule parameter.
type Param struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Unit     string `json:"unit"`
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	Required bool   `json:"required"`
}

// Subject is a record that needs attention.
type Subject struct {
	Type   string
	ID     string
	Label  string
	Detail string
	Link   string
}

type kind struct {
	params   []Param
	events   []string // outbox events that trigger an evaluation
	subjects func(ctx context.Context, q db.Querier, r *Rule, now time.Time) ([]Subject, error)
}

func hours(r *Rule, name string) time.Duration {
	return time.Duration(r.Params[name]) * time.Hour
}

// kinds are the six recipes. Their thresholds ship unset: the business
// approves them. A rule cannot be enabled without its required parameters.
var kinds = map[string]kind{
	"procurement_shortage": {
		events: []string{"procurement.shortage_identified"},
		subjects: func(ctx context.Context, q db.Querier, r *Rule, _ time.Time) ([]Subject, error) {
			return collect(ctx, q, `SELECT 'procurement_run', pr.id::text, pr.number || ' (plan ' || pr.plan_number || ')',
				(SELECT count(*) FROM procurement_lines l WHERE l.run_id = pr.id AND l.decision = 'pending')::text || ' line(s) still undecided', '/procurement/' || pr.id
				FROM procurement_runs pr WHERE pr.status = 'open'
				AND EXISTS (SELECT 1 FROM domain_events e WHERE e.type = 'procurement.shortage_identified' AND e.aggregate_id = pr.id)
				AND EXISTS (SELECT 1 FROM procurement_lines l WHERE l.run_id = pr.id AND l.decision = 'pending')`)
		}},
	"overdue_purchase_order": {
		params: []Param{{Name: "grace_hours", Label: "Hours after the expected date", Unit: "hours", Min: 0, Max: 720, Required: true}},
		events: []string{},
		subjects: func(ctx context.Context, q db.Querier, r *Rule, now time.Time) ([]Subject, error) {
			return collect(ctx, q, `SELECT 'purchase_order', p.id::text, p.number || ' · ' || s.name, 'Expected ' || to_char(p.expected_delivery, 'DD Mon') || ', not fully received',
				'/purchase-orders/' || p.id FROM purchase_orders p JOIN suppliers s ON s.id = p.supplier_id
				WHERE p.status IN ('issued', 'partially_received') AND p.expected_delivery IS NOT NULL
				AND (p.expected_delivery::timestamp AT TIME ZONE 'America/Port_of_Spain') + interval '1 day' + $1 * interval '1 hour' < $2`,
				r.Params["grace_hours"], now)
		}},
	"production_hold_unresolved": {
		params: []Param{{Name: "max_hours", Label: "Hours a hold may stay open", Unit: "hours", Min: 1, Max: 720, Required: true}},
		events: []string{},
		subjects: func(ctx context.Context, q db.Querier, r *Rule, now time.Time) ([]Subject, error) {
			return collect(ctx, q, `SELECT 'quality_hold', h.id::text, 'Hold on batch ' || b.code, left(h.reason, 200), '/production/batches/' || b.id
				FROM quality_holds h JOIN production_batches b ON b.id = h.batch_id WHERE h.status = 'open' AND h.created_at < $1`, now.Add(-hours(r, "max_hours")))
		}},
	"delivery_failure_unresolved": {
		params: []Param{{Name: "max_hours", Label: "Hours a failed delivery may wait for a next step", Unit: "hours", Min: 0, Max: 720, Required: true}},
		events: []string{"delivery.failed"},
		subjects: func(ctx context.Context, q db.Querier, r *Rule, now time.Time) ([]Subject, error) {
			return collect(ctx, q, `SELECT 'delivery', d.id::text, 'Delivery ' || d.number, 'Failed with no next step yet', '/delivery/deliveries/' || d.id
				FROM deliveries d WHERE d.status = 'delivery_failed' AND d.failed_at <= $1`, now.Add(-hours(r, "max_hours")))
		}},
	"run_unreconciled": {
		params: []Param{{Name: "max_hours", Label: "Hours a completed run may stay unreconciled", Unit: "hours", Min: 0, Max: 720, Required: true}},
		events: []string{"delivery.delivered", "delivery.failed"},
		subjects: func(ctx context.Context, q db.Querier, r *Rule, now time.Time) ([]Subject, error) {
			return collect(ctx, q, `SELECT 'dispatch_run', r.id::text, 'Run ' || r.number, 'Completed, not reconciled', '/delivery/runs/' || r.id
				FROM dispatch_runs r WHERE r.status = 'completed' AND r.completed_at <= $1`, now.Add(-hours(r, "max_hours")))
		}},
	"payment_failed": {
		events: []string{},
		subjects: func(ctx context.Context, q db.Querier, r *Rule, _ time.Time) ([]Subject, error) {
			// Only failures after the rule was enabled: enabling never floods
			// the team with history.
			return collect(ctx, q, `SELECT 'payment', p.id::text, 'Payment for the week of ' || to_char(b.period_start, 'DD Mon'),
				coalesce(p.failure_message, 'Payment failed'), '/payments/' || p.id
				FROM payments p JOIN billing_cycles b ON b.id = p.billing_cycle_id
				WHERE p.status = 'failed' AND p.updated_at >= $1`, r.EnabledSince())
		}},
}

func collect(ctx context.Context, q db.Querier, sql string, args ...any) ([]Subject, error) {
	rows, err := q.Query(ctx, sql+` LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Subject, error) {
		var s Subject
		err := r.Scan(&s.Type, &s.ID, &s.Label, &s.Detail, &s.Link)
		return s, err
	})
}

// ---------------------------------------------------------------- rules

type Rule struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Enabled         bool           `json:"enabled"`
	Params          map[string]int `json:"params"`
	ParamSpecs      []Param        `json:"param_specs"`
	Action          string         `json:"action"`
	OwnerPermission string         `json:"owner_permission"`
	Priority        string         `json:"priority"`
	CooldownMinutes *int           `json:"cooldown_minutes"`
	ApprovalRef     string         `json:"approval_ref"`
	Version         int            `json:"version"`
	UpdatedAt       time.Time      `json:"updated_at"`
	LastEvaluatedAt *time.Time     `json:"last_evaluated_at"`
	EnabledAt       *time.Time     `json:"enabled_at"`
	EventTriggers   []string       `json:"event_triggers"`
	Missing         []string       `json:"missing"`
	LastSuccess     *time.Time     `json:"last_success"`
	Failed          int            `json:"failed_executions"`
}

// EnabledSince is when the rule was last switched on.
func (r *Rule) EnabledSince() time.Time {
	if r.EnabledAt != nil {
		return *r.EnabledAt
	}
	return r.UpdatedAt
}

const ruleCols = `r.id::text, r.kind, r.name, r.description, r.enabled, r.params, r.action, r.owner_permission, r.priority, r.cooldown_minutes,
	r.approval_ref, r.version, r.updated_at, r.last_evaluated_at, r.enabled_at,
	(SELECT max(finished_at) FROM automation_executions e WHERE e.rule_id = r.id AND e.status IN ('succeeded', 'approved')),
	(SELECT count(*) FROM automation_executions e WHERE e.rule_id = r.id AND e.status = 'failed')::int`

func scanRule(row pgx.Row) (*Rule, error) {
	var r Rule
	var params []byte
	if err := row.Scan(&r.ID, &r.Kind, &r.Name, &r.Description, &r.Enabled, &params, &r.Action, &r.OwnerPermission, &r.Priority, &r.CooldownMinutes,
		&r.ApprovalRef, &r.Version, &r.UpdatedAt, &r.LastEvaluatedAt, &r.EnabledAt, &r.LastSuccess, &r.Failed); err != nil {
		return nil, err
	}
	r.Params = map[string]int{}
	_ = json.Unmarshal(params, &r.Params)
	k := kinds[r.Kind]
	r.ParamSpecs, r.EventTriggers = k.params, k.events
	if r.ParamSpecs == nil {
		r.ParamSpecs = []Param{}
	}
	if r.EventTriggers == nil {
		r.EventTriggers = []string{}
	}
	r.Missing = missing(&r)
	return &r, nil
}

// missing lists what must be decided before the rule can run.
func missing(r *Rule) []string {
	out := []string{}
	for _, p := range kinds[r.Kind].params {
		if _, ok := r.Params[p.Name]; !ok && p.Required {
			out = append(out, p.Label+" is not set.")
		}
	}
	if r.CooldownMinutes == nil {
		out = append(out, "No cooldown is set (how long before the same record can trigger again).")
	}
	if r.ApprovalRef == "" {
		out = append(out, "No approval reference: the business has not approved this automation.")
	}
	return out
}

func (s *Service) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+ruleCols+` FROM automation_rules r ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *Service) Rule(ctx context.Context, q db.Querier, id string) (*Rule, error) {
	r, err := scanRule(q.QueryRow(ctx, `SELECT `+ruleCols+` FROM automation_rules r WHERE r.id::text = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	return r, err
}

type RuleInput struct {
	Enabled         *bool          `json:"enabled"`
	Params          map[string]int `json:"params"`
	Action          *string        `json:"action"`
	Priority        *string        `json:"priority"`
	CooldownMinutes *int           `json:"cooldown_minutes"`
	ApprovalRef     *string        `json:"approval_ref"`
}

// Actions automation may take. Nothing financial, no approvals of
// suppliers or orders, no quality, dispatch or subscription changes.
var Actions = []string{"alert", "task", "request_approval"}

// SaveRule validates and stores a new configuration version.
func (s *Service) SaveRule(ctx context.Context, actor, id string, in RuleInput) (*Rule, error) {
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := s.Rule(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM automation_rules WHERE id = $1 FOR UPDATE`, r.ID); err != nil {
			return err
		}
		k := kinds[r.Kind]
		if in.Params != nil {
			next := map[string]int{}
			for name, v := range in.Params {
				var spec *Param
				for i := range k.params {
					if k.params[i].Name == name {
						spec = &k.params[i]
					}
				}
				if spec == nil {
					return fieldErr("params", "Unknown parameter "+name+" for this rule.")
				}
				if v < spec.Min || v > spec.Max {
					return fieldErr("params", fmt.Sprintf("%s must be %d to %d %s.", spec.Label, spec.Min, spec.Max, spec.Unit))
				}
				next[name] = v
			}
			r.Params = next
		}
		if in.Action != nil {
			if !slices.Contains(Actions, *in.Action) {
				return fieldErr("action", "Choose alert, task or request approval. Automation cannot refund, charge, approve, override or dispatch.")
			}
			r.Action = *in.Action
		}
		if in.Priority != nil {
			if !slices.Contains([]string{"low", "normal", "high", "critical"}, *in.Priority) {
				return fieldErr("priority", "Choose low, normal, high or critical.")
			}
			r.Priority = *in.Priority
		}
		if in.CooldownMinutes != nil {
			if *in.CooldownMinutes < 1 || *in.CooldownMinutes > 43200 {
				return fieldErr("cooldown_minutes", "Use 1 minute to 30 days.")
			}
			r.CooldownMinutes = in.CooldownMinutes
		}
		if in.ApprovalRef != nil {
			r.ApprovalRef = strings.TrimSpace(*in.ApprovalRef)
		}
		if in.Enabled != nil {
			r.Enabled = *in.Enabled
		}
		if r.Enabled {
			if m := missing(r); len(m) > 0 {
				return &httpx.Error{Status: 422, Code: "not_approved", Message: "This rule cannot be enabled yet: " + strings.Join(m, " ")}
			}
		}
		params, _ := json.Marshal(r.Params)
		var version int
		if err := tx.QueryRow(ctx, `UPDATE automation_rules SET enabled = $2, params = $3, action = $4, priority = $5, cooldown_minutes = $6, approval_ref = $7,
			enabled_at = CASE WHEN NOT $2 THEN NULL WHEN enabled THEN enabled_at ELSE now() END, version = version + 1, updated_by = $8, updated_at = now() WHERE id = $1 RETURNING version`,
			r.ID, r.Enabled, params, r.Action, r.Priority, r.CooldownMinutes, r.ApprovalRef, nilIfEmpty(actor)).Scan(&version); err != nil {
			return err
		}
		config, _ := json.Marshal(map[string]any{"enabled": r.Enabled, "params": r.Params, "action": r.Action, "priority": r.Priority,
			"cooldown_minutes": r.CooldownMinutes, "approval_ref": r.ApprovalRef})
		if _, err := tx.Exec(ctx, `INSERT INTO automation_rule_versions (rule_id, version, config, actor_user_id) VALUES ($1, $2, $3, $4)`,
			r.ID, version, config, nilIfEmpty(actor)); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{ActorUserID: actor, Action: "automation.rule_saved", TargetType: "automation_rule", TargetID: r.ID,
			Metadata: map[string]any{"kind": r.Kind, "enabled": r.Enabled, "version": version}})
	})
	if err != nil {
		return nil, err
	}
	return s.Rule(ctx, s.pool, id)
}

// ---------------------------------------------------------------- evaluation

// JobExecute performs one execution's action (retried with backoff).
const JobExecute = "automation.execute"

// JobEvent evaluates rules triggered by one outbox event.
const JobEvent = "automation.event"

// ConsumerName is automation's row in event_consumer_offsets.
const ConsumerName = "automation"

type executePayload struct {
	ExecutionID string `json:"execution_id"`
}

// Evaluate finds a rule's subjects and records one execution per subject
// not seen within the cooldown. The dedupe key (rule, subject, cooldown
// window) makes a second worker or a replayed trigger a no-op.
func (s *Service) Evaluate(ctx context.Context, ruleID, trigger, triggerRef string) (int, error) {
	created := 0
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := s.Rule(ctx, tx, ruleID)
		if err != nil {
			return err
		}
		if !r.Enabled {
			return nil
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('automation:' || $1, 9))`, r.ID); err != nil {
			return err
		}
		now := s.Now()
		subjects, err := kinds[r.Kind].subjects(ctx, tx, r, now)
		if err != nil {
			return err
		}
		cool := time.Duration(*r.CooldownMinutes) * time.Minute
		window := now.UTC().Truncate(cool).Format(time.RFC3339)
		for _, sub := range subjects {
			var recent int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM automation_executions WHERE rule_id = $1 AND subject_type = $2 AND subject_id = $3 AND created_at > $4`,
				r.ID, sub.Type, sub.ID, now.Add(-cool)).Scan(&recent); err != nil {
				return err
			}
			if recent > 0 {
				continue // cooldown
			}
			var id string
			err := tx.QueryRow(ctx, `INSERT INTO automation_executions (rule_id, rule_version, trigger, trigger_ref, subject_type, subject_id, subject_label, dedupe_key, action, result, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) ON CONFLICT (dedupe_key) DO NOTHING RETURNING id::text`,
				r.ID, r.Version, trigger, triggerRef, sub.Type, sub.ID, sub.Label, r.ID+"|"+sub.Type+"|"+sub.ID+"|"+window, r.Action,
				map[string]any{"detail": sub.Detail, "link": sub.Link}, now).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO automation_execution_events (execution_id, type, body) VALUES ($1, 'created', $2)`, id, trigger+" "+triggerRef); err != nil {
				return err
			}
			if err := jobs.Enqueue(ctx, tx, JobExecute, executePayload{ExecutionID: id}); err != nil {
				return err
			}
			created++
		}
		_, err = tx.Exec(ctx, `UPDATE automation_rules SET last_evaluated_at = $2 WHERE id = $1`, r.ID, now)
		return err
	})
	return created, err
}

// HandleExecute performs the action. It is idempotent: an alert is keyed by
// the execution, a task is created once per execution, an approval request
// is a state of the execution itself.
func (s *Service) HandleExecute(ctx context.Context, payload json.RawMessage) error {
	var p executePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("automation: bad payload: %w", err)
	}
	var execErr error
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var ruleID, action, status, label, perm, priority, name string
		var result []byte
		err := tx.QueryRow(ctx, `SELECT e.rule_id::text, e.action, e.status, e.subject_label, e.result, r.owner_permission, r.priority, r.name
			FROM automation_executions e JOIN automation_rules r ON r.id = e.rule_id WHERE e.id = $1 FOR UPDATE OF e`, p.ExecutionID).
			Scan(&ruleID, &action, &status, &label, &result, &perm, &priority, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if status != "pending" && status != "failed" {
			return nil // already done
		}
		var res struct{ Detail, Link string }
		_ = json.Unmarshal(result, &res)
		if _, err := tx.Exec(ctx, `UPDATE automation_executions SET attempts = attempts + 1 WHERE id = $1`, p.ExecutionID); err != nil {
			return err
		}
		execErr = s.act(ctx, tx, p.ExecutionID, action, name, label, res.Detail, res.Link, perm, priority)
		if execErr != nil {
			msg := execErr.Error()
			if len(msg) > 1000 {
				msg = msg[:1000]
			}
			if _, err := tx.Exec(ctx, `UPDATE automation_executions SET status = 'failed', last_error = $2 WHERE id = $1`, p.ExecutionID, msg); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO automation_execution_events (execution_id, type, body) VALUES ($1, 'failed', $2)`, p.ExecutionID, msg)
			return err
		}
		next := "succeeded"
		if action == "request_approval" {
			next = "awaiting_approval"
		}
		if _, err := tx.Exec(ctx, `UPDATE automation_executions SET status = $2, last_error = NULL, finished_at = now() WHERE id = $1`, p.ExecutionID, next); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO automation_execution_events (execution_id, type) VALUES ($1, 'succeeded')`, p.ExecutionID)
		return err
	})
	if err != nil {
		return err
	}
	return execErr // returned so the queue retries with backoff
}

func severity(priority string) string {
	switch priority {
	case "critical":
		return "critical"
	case "high", "normal":
		return "warning"
	}
	return "info"
}

// FailNext makes the next action fail (tests only: proves failures stay
// visible and retries are safe).
var FailNext func(action string) error

func (s *Service) act(ctx context.Context, tx pgx.Tx, execID, action, rule, label, detail, link, perm, priority string) error {
	if FailNext != nil {
		if err := FailNext(action); err != nil {
			return err
		}
	}
	title := clip(rule+": "+label, 200)
	switch action {
	case "alert":
		if s.Alerts == nil {
			return errors.New("no alert service")
		}
		return s.Alerts.Raise(ctx, tx, support.Alert{DedupeKey: "automation:" + execID, Kind: "automation", Severity: severity(priority), Title: title,
			Body: clip(detail, 1000), ResourceType: "automation_execution", ResourceID: execID, Permission: perm})
	case "task":
		_, err := tx.Exec(ctx, `INSERT INTO automation_tasks (execution_id, title, detail, permission, link)
			SELECT $1, $2, $3, $4, $5 WHERE NOT EXISTS (SELECT 1 FROM automation_tasks WHERE execution_id = $1)`, execID, title, clip(detail, 2000), perm, clip(link, 300))
		return err
	case "request_approval":
		if s.Alerts == nil {
			return errors.New("no alert service")
		}
		return s.Alerts.Raise(ctx, tx, support.Alert{DedupeKey: "automation-approval:" + execID, Kind: "automation_approval", Severity: "info",
			Title: clip("Approval requested: "+title, 200), Body: clip(detail, 1000), ResourceType: "automation_execution", ResourceID: execID,
			Permission: "automation.approve"})
	}
	return fmt.Errorf("unknown action %s", action)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// EvaluateAll evaluates every enabled rule (the scheduled trigger).
func (s *Service) EvaluateAll(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM automation_rules WHERE enabled ORDER BY kind`)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	total := 0
	for _, id := range ids {
		n, err := s.Evaluate(ctx, id, "schedule", "")
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// Dispatcher routes the outbox events rules listen to.
func (s *Service) Dispatcher(settle time.Duration) *consumer.Dispatcher {
	routes := map[string]consumer.Route{}
	for _, k := range kinds {
		for _, e := range k.events {
			routes[e] = consumer.Route{Kind: JobEvent, Payload: func(id int64, agg string) any { return map[string]any{"event_id": id} }}
		}
	}
	return &consumer.Dispatcher{Pool: s.pool, Name: ConsumerName, SettleDelay: settle, Routes: routes}
}

// HandleEvent evaluates the enabled rules that listen to the event's type.
func (s *Service) HandleEvent(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		EventID int64 `json:"event_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("automation: bad payload: %w", err)
	}
	var typ string
	if err := s.pool.QueryRow(ctx, `SELECT type FROM domain_events WHERE id = $1`, p.EventID).Scan(&typ); err != nil {
		return err
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, kind FROM automation_rules WHERE enabled`)
	if err != nil {
		return err
	}
	type rk struct{ id, kind string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (rk, error) {
		var x rk
		err := r.Scan(&x.id, &x.kind)
		return x, err
	})
	if err != nil {
		return err
	}
	for _, r := range list {
		if slices.Contains(kinds[r.kind].events, typ) {
			if _, err := s.Evaluate(ctx, r.id, "event", fmt.Sprintf("%s#%d", typ, p.EventID)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Tick runs the scheduled evaluation and dispatches events (worker loop).
func (s *Service) Tick(ctx context.Context, settle time.Duration) error {
	if _, err := s.Dispatcher(settle).Dispatch(ctx); err != nil {
		return err
	}
	_, err := s.EvaluateAll(ctx)
	return err
}
