package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/delivery"
	"mise.tt/api/internal/platform/config"
)

// DemoDelivery is what SeedDemoDelivery set up.
type DemoDelivery struct {
	Zone        string   `json:"zone"`
	ServiceDays []string `json:"service_days"`
	Partner     string   `json:"partner"`
	Courier     string   `json:"courier"`
	Completed   []string `json:"completed_batches"`
	Obligations int      `json:"fulfillments_created"`
}

const demoMark = "LOCAL DEMO ONLY: not an approved business decision"

// SeedDemoDelivery configures local demo delivery data so the delivery
// screens can be exercised: a demo zone covering the active regions, two
// weeks of demo service days with one demo window and demo capacity, a demo
// partner (marked demo, never a real company) with a demo courier login, and
// a demo proof policy if none is set. With ready, it also takes the most
// recent released demo production plan through the kitchen (start, output,
// quality checks, packing, tasks, complete) using the real API, so real
// readiness events create the fulfillments. Local only, demo content on.
func SeedDemoDelivery(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, ready bool) (*DemoDelivery, error) {
	if cfg.Env != "local" {
		return nil, errors.New("the delivery demo is only allowed when MISE_ENV=local")
	}
	if !cfg.CatalogDemoEnabled {
		return nil, errors.New("demo content is off (CATALOG_DEMO_ENABLED=false)")
	}
	cfg.RateLimitsDisabled = true
	ids, err := NewIdentityService(cfg, pool)
	if err != nil {
		return nil, err
	}
	com, err := NewCommerce(cfg, pool, ids)
	if err != nil {
		return nil, err
	}
	handler, _, err := BuildWith(cfg, pool, LimitsFor(cfg), ids, com)
	if err != nil {
		return nil, err
	}
	api := &inproc{h: handler}
	bo := NewBackoffice(cfg, pool, ids, com)
	_, prod := NewKitchen(cfg, pool, com, bo)
	ff := NewFulfilment(cfg, pool, com, bo, prod)
	dl := ff.Delivery

	staff := map[string]string{}
	for _, s := range []struct{ key, email, name, role string }{
		{"dispatch", "demo.dispatch@example.com", "Demo Dispatch Manager", "dispatch_manager"},
		{"kitchen", "demo.kitchen@example.com", "Demo Kitchen Staff", "kitchen_staff"},
		{"inspector", "demo.qa@example.com", "Demo Quality Inspector", "quality_inspector"},
		{"fulfillment", "demo.fulfillment@example.com", "Demo Fulfillment Manager", "fulfillment_manager"},
		{"courier", "demo.courier@example.com", "Demo Courier", "courier"},
	} {
		pw := randomSecret()
		if err := ids.UpsertStaff(ctx, s.email, s.name, s.role, pw); err != nil {
			return nil, err
		}
		res, err := api.do("POST", "/api/v1/admin/auth/login", map[string]any{"email": s.email, "password": pw}, "")
		if err != nil {
			return nil, fmt.Errorf("demo %s sign-in: %w", s.key, err)
		}
		staff[s.key] = str(res["token"])
	}
	var actor string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email_canonical = 'demo.dispatch@example.com'`).Scan(&actor); err != nil {
		return nil, err
	}
	out := &DemoDelivery{}

	// A demo zone over every active region not already covered.
	var zoneID string
	_ = pool.QueryRow(ctx, `SELECT id::text FROM delivery_service_zones WHERE code = 'DEMO'`).Scan(&zoneID)
	if zoneID == "" {
		cov, err := dl.RegionCoverage(ctx)
		if err != nil {
			return nil, err
		}
		regions := []string{}
		for _, c := range cov {
			if c.Status == "active" && c.ZoneID == nil {
				regions = append(regions, c.Region)
			}
		}
		z, err := dl.SaveZone(ctx, actor, "", delivery.ZoneInput{Code: "DEMO", Name: "Demo zone (local)", Status: "active", Notes: demoMark, Regions: regions})
		if err != nil {
			return nil, err
		}
		zoneID = z.ID
	}
	out.Zone = "DEMO"

	// Two weeks of demo service days, one demo window, demo capacity.
	today := time.Now().In(dl.Location)
	for i := 1; i <= 14; i++ {
		d := today.AddDate(0, 0, i).Format("2006-01-02")
		if _, err := dl.SaveServiceDay(ctx, actor, delivery.ServiceDayInput{Date: d, Status: "open", Notes: demoMark,
			Windows: []delivery.WindowInput{{Label: "Demo window", StartsAt: "09:00", EndsAt: "17:00", Status: "approved"}}}); err != nil {
			return nil, err
		}
		n := 30
		if _, err := dl.SetCapacity(ctx, actor, delivery.CapacityInput{Date: d, Capacity: &n, Notes: demoMark}); err != nil {
			return nil, err
		}
		out.ServiceDays = append(out.ServiceDays, d)
	}

	// A demo partner, qualified and approved, marked demo throughout.
	var partnerID string
	_ = pool.QueryRow(ctx, `SELECT id::text FROM delivery_partners WHERE demo AND status = 'approved' ORDER BY created_at LIMIT 1`).Scan(&partnerID)
	if partnerID == "" {
		p, err := dl.SavePartner(ctx, actor, "", delivery.PartnerInput{Name: "Demo Courier Co (local demo, not a real partner)", IntegrationMode: "manual",
			AgreementStatus: "signed", AgreementRef: "LOCAL-DEMO (no real agreement)", Notes: demoMark, ZoneIDs: []string{zoneID}, Demo: true})
		if err != nil {
			return nil, err
		}
		partnerID = p.ID
		for _, c := range delivery.Criteria {
			if _, err := dl.RecordCriterion(ctx, actor, partnerID, delivery.CriterionInput{Criterion: c, Met: true, Evidence: demoMark}); err != nil {
				return nil, err
			}
		}
		for _, st := range []string{"pending_approval", "approved"} {
			if _, err := dl.SetPartnerStatus(ctx, actor, partnerID, delivery.PartnerStatusInput{Status: st, Reason: demoMark}); err != nil {
				return nil, err
			}
		}
	}
	out.Partner = partnerID
	var courierID string
	_ = pool.QueryRow(ctx, `SELECT c.id::text FROM delivery_couriers c JOIN users u ON u.id = c.user_id WHERE u.email_canonical = 'demo.courier@example.com'`).Scan(&courierID)
	if courierID == "" {
		c, err := dl.SaveCourier(ctx, actor, "", delivery.CourierInput{PartnerID: partnerID, Name: "Demo Rider", Phone: "", UserEmail: "demo.courier@example.com",
			ZoneIDs: []string{zoneID}, Notes: demoMark, Demo: true})
		if err != nil {
			return nil, err
		}
		courierID = c.ID
	}
	out.Courier = courierID

	st, err := dl.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if len(st.ProofMethods) == 0 {
		methods := []string{"confirmation_code", "recipient_acknowledgment"}
		ref := demoMark
		if _, err := dl.PutSettings(ctx, actor, delivery.SettingsInput{ProofMethods: &methods, ProofPolicyRef: &ref}); err != nil {
			return nil, err
		}
	}
	if !ready {
		return out, nil
	}

	// Take the newest released demo plan through the kitchen.
	var planID string
	if err := pool.QueryRow(ctx, `SELECT p.id::text FROM production_plans p WHERE p.status = 'released'
		AND EXISTS (SELECT 1 FROM production_batches b WHERE b.plan_id = p.id AND b.status = 'released')
		AND NOT EXISTS (SELECT 1 FROM production_batches b WHERE b.plan_id = p.id AND b.status NOT IN ('released', 'cancelled'))
		ORDER BY p.released_at DESC LIMIT 1`).Scan(&planID); err != nil {
		return nil, errors.New("no released production plan with untouched batches: run `misectl production seed-demo` first")
	}
	plan, err := api.do("GET", "/api/v1/admin/production/plans/"+planID, nil, staff["kitchen"])
	if err != nil {
		return nil, err
	}
	templates, err := api.do("GET", "/api/v1/admin/quality/templates", nil, staff["inspector"])
	if err != nil {
		return nil, err
	}
	for _, b := range list(plan["batches"]) {
		bt := obj(b)
		if bt["status"] != "released" {
			continue
		}
		id := str(bt["id"])
		kits := int(num(bt["planned_kits"]))
		k, qa := staff["kitchen"], staff["inspector"]
		if _, err := api.do("POST", "/api/v1/admin/production/batches/"+id+"/start", map[string]any{"assigned_team": "Demo line"}, k); err != nil {
			return nil, err
		}
		if _, err := api.do("POST", "/api/v1/admin/production/batches/"+id+"/output", map[string]any{"produced_kits": kits}, k); err != nil {
			return nil, err
		}
		for _, t := range items(templates) {
			if t["active"] == true {
				if _, err := api.do("POST", "/api/v1/admin/quality/checks", map[string]any{"batch_id": id, "template_id": str(t["id"]), "result": "pass"}, qa); err != nil {
					return nil, err
				}
			}
		}
		if _, err := api.do("PUT", "/api/v1/admin/production/batches/"+id+"/packaging", map[string]any{"packaging_type": "Demo insulated bag",
			"packed_kits": kits, "labels_verified": true, "complete": true}, k); err != nil {
			return nil, err
		}
		tasks, err := api.do("GET", "/api/v1/admin/production/tasks?batch_id="+id, nil, k)
		if err != nil {
			return nil, err
		}
		for _, t := range items(tasks) {
			if t["status"] != "done" {
				if _, err := api.do("PATCH", "/api/v1/admin/production/tasks/"+str(t["id"]), map[string]any{"assigned_to": "me", "status": "done"}, k); err != nil {
					return nil, err
				}
			}
		}
		if _, err := api.do("POST", "/api/v1/admin/production/batches/"+id+"/complete", nil, k); err != nil {
			return nil, err
		}
		out.Completed = append(out.Completed, str(bt["code"]))
	}
	// The readiness events are also consumed by the worker; sync makes the
	// result immediate (it is idempotent with the consumer).
	res, err := api.do("POST", "/api/v1/admin/fulfillment/sync", nil, staff["fulfillment"])
	if err != nil {
		return nil, err
	}
	for _, o := range list(res["items"]) {
		if obj(o)["result"] == "created" {
			out.Obligations++
		}
	}
	return out, nil
}
