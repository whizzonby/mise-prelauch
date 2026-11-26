package server_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"mise.tt/api/internal/payments/testpay"
)

// adminRoute is one admin endpoint and the permission its router requires.
type adminRoute struct {
	method, path, perm string
}

var (
	routeRe = regexp.MustCompile(`mux\.Handle\("([A-Z]+) (/api/v1/admin/[^"]+)",\s*(?:adm|req|s\.RequireAdmin|h\.Identity\.RequireAdmin)\((identity\.Perm\w+|Perm\w+|P\("[a-z_.]+"\)|any)`)
	constRe = regexp.MustCompile(`(Perm\w+)\s+Permission\s*=\s*"([a-z_.]+)"`)
	anyRe   = regexp.MustCompile(`any\s*:=\s*identity\.(Perm\w+)`)
	paramRe = regexp.MustCompile(`\{[^}]+\}`)
)

// adminRoutes reads every admin route and its permission from the handler
// source, so a route added later is covered without editing this test.
func adminRoutes(t *testing.T) []adminRoute {
	t.Helper()
	files, err := filepath.Glob("../*/*.go")
	if err != nil {
		t.Fatal(err)
	}
	consts := map[string]string{}
	var sources []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, m := range constRe.FindAllStringSubmatch(src, -1) {
			consts[m[1]] = m[2]
		}
		sources = append(sources, src)
	}
	var out []adminRoute
	for _, src := range sources {
		local := ""
		if m := anyRe.FindStringSubmatch(src); m != nil {
			local = m[1]
		}
		for _, m := range routeRe.FindAllStringSubmatch(src, -1) {
			raw := strings.TrimPrefix(m[3], "identity.")
			var perm string
			switch {
			case strings.HasPrefix(raw, `P("`):
				perm = strings.TrimSuffix(strings.TrimPrefix(raw, `P("`), `")`)
			case raw == "any":
				perm = consts[local]
			default:
				perm = consts[raw]
			}
			if perm == "" {
				t.Fatalf("cannot resolve the permission of %s %s (%s)", m[1], m[2], m[3])
			}
			out = append(out, adminRoute{method: m[1], path: m[2], perm: perm})
		}
	}
	return out
}

// scopedInHandler are endpoints whose handler narrows access further, by
// record, after the router's permission check: a holder of the permission can
// still be refused for a particular record. Each entry says why.
var scopedInHandler = map[string]string{
	"POST /api/v1/admin/intelligence/tools/{name}":           "each tool also needs its domain permission and the permission of the data it reads",
	"GET /api/v1/admin/intelligence/segments/{key}/members":  "names customers, so customers.read is also required",
	"GET /api/v1/admin/delivery/deliveries":                  "delivery scope: dispatch roles see all, a courier only assigned deliveries",
	"GET /api/v1/admin/delivery/deliveries/{id}":             "delivery scope",
	"GET /api/v1/admin/delivery/dispatch-runs":               "delivery scope",
	"GET /api/v1/admin/delivery/dispatch-runs/{id}":          "delivery scope",
	"GET /api/v1/admin/delivery/dispatch-runs/{id}/manifest": "delivery scope",
	"POST /api/v1/admin/delivery/deliveries/{id}/deliver":    "a courier acts only on deliveries assigned to them",
	"POST /api/v1/admin/delivery/deliveries/{id}/fail":       "a courier acts only on deliveries assigned to them",
	"POST /api/v1/admin/delivery/deliveries/{id}/events":     "a courier acts only on deliveries assigned to them",
	"POST /api/v1/admin/delivery/deliveries/{id}/proof":      "a courier acts only on deliveries assigned to them",
	"POST /api/v1/admin/purchase-orders/{id}/approve":        "approval limits: the approver needs a limit covering the order",
	"PATCH /api/v1/admin/recipes/{id}":                       "a chef edits only recipes they author",
	"POST /api/v1/admin/recipes/{id}/revise":                 "a chef revises only recipes they author",
}

// Every role, against every admin endpoint, by direct API call: a role
// reaches an endpoint exactly when it holds the endpoint's permission. A
// customer session reaches none, and no session reaches none.
func TestEveryRoleReachesExactlyTheAdminEndpointsItIsGranted(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	routes := adminRoutes(t)
	if len(routes) < 300 {
		t.Fatalf("found only %d admin routes; the route pattern no longer matches the handlers", len(routes))
	}
	// Every documented admin route must be in the matrix.
	covered := map[string]bool{}
	for _, r := range routes {
		covered[r.method+" "+r.path] = true
	}
	for _, rt := range h.routes {
		if strings.Contains(rt, " /api/v1/admin/") && !strings.Contains(rt, "/admin/auth/") && !covered[rt] {
			t.Errorf("route %s is not in the RBAC matrix (permission not found by the parser)", rt)
		}
	}

	rows, err := h.pool.Query(ctx, `SELECT key FROM roles ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		roles = append(roles, k)
	}
	rows.Close()
	if len(roles) < 11 {
		t.Fatalf("only %d roles", len(roles))
	}

	const fakeID = "00000000-0000-4000-8000-000000000000"
	call := func(r adminRoute, opts ...reqOpt) int {
		path := paramRe.ReplaceAllString(r.path, fakeID)
		var body any
		if r.method != http.MethodGet && r.method != http.MethodDelete {
			body = map[string]any{}
		}
		if strings.HasSuffix(path, "/stream") {
			// A permitted stream stays open by design: end it shortly after it starts.
			opts = append(opts, func(req *http.Request) {
				ctx, cancel := context.WithTimeout(req.Context(), 300*time.Millisecond)
				t.Cleanup(cancel)
				*req = *req.WithContext(ctx)
			})
		}
		return h.do(r.method, path, body, opts...).Status
	}

	// No session, and a customer session: refused everywhere.
	cust := bearer(h.customer("matrix-customer@example.com"))
	for _, r := range routes {
		if st := call(r); st != http.StatusUnauthorized {
			t.Errorf("no session: %s %s = %d, want 401", r.method, r.path, st)
		}
		if st := call(r, cust); st != http.StatusUnauthorized && st != http.StatusForbidden {
			t.Errorf("customer session: %s %s = %d, want 401/403", r.method, r.path, st)
		}
	}

	granted := map[string]map[string]bool{}
	rows, err = h.pool.Query(ctx, `SELECT role_key, permission_key FROM role_permissions`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var role, perm string
		if err := rows.Scan(&role, &perm); err != nil {
			t.Fatal(err)
		}
		if granted[role] == nil {
			granted[role] = map[string]bool{}
		}
		granted[role][perm] = true
	}
	rows.Close()

	checked := 0
	for _, role := range roles {
		if !granted[role]["admin.access"] {
			// A role without console access cannot sign in to the console at all.
			email := "matrix-" + role + "@mise.test"
			if err := h.ids.UpsertStaff(ctx, email, "No Console", role, "correct horse battery staple"); err != nil {
				t.Fatalf("create %s: %v", role, err)
			}
			res := h.do("POST", "/api/v1/admin/auth/login", map[string]string{"email": email, "password": "correct horse battery staple"})
			if res.Status == http.StatusOK {
				t.Errorf("role %s has no admin.access but signed in to the console", role)
			}
			continue
		}
		tok := bearer(h.adminLogin("matrix-"+role+"@mise.test", role))
		var wrongly []string
		for _, r := range routes {
			st := call(r, tok)
			allowed := granted[role][r.perm]
			switch {
			case !allowed && st != http.StatusForbidden:
				wrongly = append(wrongly, "reached "+r.method+" "+r.path+" ("+r.perm+")")
			case allowed && st == http.StatusForbidden && scopedInHandler[r.method+" "+r.path] != "":
				// Permission held, refused for this (made-up) record by the handler's own scope.
			case allowed && (st == http.StatusForbidden || st == http.StatusUnauthorized):
				wrongly = append(wrongly, "refused "+r.method+" "+r.path+" ("+r.perm+")")
			}
			checked++
		}
		sort.Strings(wrongly)
		for _, w := range wrongly {
			t.Errorf("role %s %s", role, w)
		}
	}
	t.Logf("RBAC matrix: %d roles × %d admin routes = %d direct API calls checked", len(roles), len(routes), checked)
}

// A customer can never read or change another customer's records by id.
func TestCustomersCannotReachEachOthersRecords(t *testing.T) {
	s := newShop(t)
	h := s.h
	ctx := context.Background()
	tokA, subA := s.active("owner@example.com", testpay.CardApprove)
	week := s.weekCycle(tokA, "2026-11-16")["id"].(string)
	h.wantStatus(h.do("POST", "/api/v1/me/meal-selections/"+week+"/finalize", map[string]any{"items": items(s.meals[0], 3)}, tokA), http.StatusOK)
	s.tick("2026-11-13 00:30")
	tokB, _ := s.customer("intruder@example.com")

	var order, invoice, checkout, address string
	for q, dst := range map[string]*string{
		`SELECT id::text FROM orders LIMIT 1`:            &order,
		`SELECT id::text FROM invoices LIMIT 1`:          &invoice,
		`SELECT id::text FROM checkout_sessions LIMIT 1`: &checkout,
		`SELECT id::text FROM customer_addresses a WHERE a.customer_id = (SELECT customer_id FROM subscriptions WHERE id = $1) LIMIT 1`: &address,
	} {
		var err error
		if strings.Contains(q, "$1") {
			err = h.pool.QueryRow(ctx, q, subA).Scan(dst)
		} else {
			err = h.pool.QueryRow(ctx, q).Scan(dst)
		}
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	attempts := []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/me/orders/" + order, nil},
		{"GET", "/api/v1/me/orders/" + order + "/delivery", nil},
		{"POST", "/api/v1/me/orders/" + order + "/delivery/support", map[string]any{"message": "Where is it?"}},
		{"POST", "/api/v1/me/orders/" + order + "/delivery/reschedule-requests", map[string]any{"reason": "Away"}},
		{"GET", "/api/v1/me/invoices/" + invoice, nil},
		{"GET", "/api/v1/me/invoices/" + invoice + "/pdf", nil},
		{"GET", "/api/v1/checkout/sessions/" + checkout, nil},
		{"GET", "/api/v1/me/meal-selections/" + week, nil},
		{"PUT", "/api/v1/me/meal-selections/" + week, map[string]any{"items": items(s.meals[1], 3)}},
		{"POST", "/api/v1/me/meal-selections/" + week + "/finalize", map[string]any{"items": items(s.meals[1], 3)}},
		{"POST", "/api/v1/me/subscriptions/" + subA + "/pause", map[string]any{"weeks": 1}},
		{"POST", "/api/v1/me/subscriptions/" + subA + "/resume", nil},
		{"POST", "/api/v1/me/subscriptions/" + subA + "/cancel", map[string]any{"reason": "other"}},
		{"POST", "/api/v1/me/subscriptions/" + subA + "/change-plan", map[string]any{"plan_id": s.family}},
		{"PUT", "/api/v1/me/subscriptions/" + subA + "/address", map[string]any{"address_id": address}},
		{"PATCH", "/api/v1/me/addresses/" + address, map[string]any{"label": "Mine now"}},
		{"DELETE", "/api/v1/me/addresses/" + address, nil},
	}
	for _, a := range attempts {
		res := h.do(a.method, a.path, a.body, tokB)
		if res.Status < 400 || (res.Status != http.StatusNotFound && res.Status != http.StatusForbidden) {
			t.Errorf("customer B %s %s = %d, want 404 (or 403)", a.method, a.path, res.Status)
		}
	}
	// Customer A's records are untouched.
	if st := s.subscription(tokA)["status"]; st != "active" {
		t.Errorf("A's subscription = %v", st)
	}
	if n := h.count(`SELECT count(*) FROM customer_addresses WHERE id = $1 AND label = 'Mine now'`, address); n != 0 {
		t.Error("B changed A's address")
	}
	// B's own lists do not include A's records.
	for _, path := range []string{"/api/v1/me/orders", "/api/v1/me/invoices"} {
		res := h.do("GET", path, nil, tokB)
		h.wantStatus(res, http.StatusOK)
		if n := len(list(t, res.Data["items"])); n != 0 {
			t.Errorf("B's %s lists %d records", path, n)
		}
	}
}
