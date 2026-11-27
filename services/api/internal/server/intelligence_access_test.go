package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Sprint 9.2: Mise Intelligence is admin only. Every intelligence and
// management-finance endpoint lives under /api/v1/admin and refuses guests,
// customers and staff without the intelligence permissions; the customer
// app, the marketing site and the support chat never call it.

func intelligenceRoutes(h *harness) []string {
	var out []string
	for _, r := range h.routes {
		if strings.Contains(r, "/intelligence/") || strings.Contains(r, "/finance/") || strings.Contains(r, "/behavior") {
			out = append(out, r)
		}
	}
	return out
}

func TestMiseIntelligenceIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	routes := intelligenceRoutes(h)
	if len(routes) < 55 {
		t.Fatalf("found %d intelligence and finance routes; the filter no longer matches", len(routes))
	}
	cust := bearer(h.customer("curious-customer@example.com"))
	support := bearer(h.adminLogin("support-only@mise.test", "support"))
	const fakeID = "00000000-0000-4000-8000-000000000000"
	param := regexp.MustCompile(`\{[^}]+\}`)
	for _, rt := range routes {
		method, path, _ := strings.Cut(rt, " ")
		if path == "/api/v1/behavior/events" {
			continue // the customer app's own event relay (identifiers only), not intelligence
		}
		if !strings.HasPrefix(path, "/api/v1/admin/") {
			t.Errorf("%s is not an admin route", rt)
			continue
		}
		url := param.ReplaceAllString(path, fakeID)
		var body any
		if method != http.MethodGet {
			body = map[string]any{}
		}
		if st := h.do(method, url, body).Status; st != http.StatusUnauthorized {
			t.Errorf("guest: %s = %d, want 401", rt, st)
		}
		if st := h.do(method, url, body, cust).Status; st != http.StatusUnauthorized && st != http.StatusForbidden {
			t.Errorf("customer: %s = %d, want 401/403", rt, st)
		}
		if st := h.do(method, url, body, support).Status; st != http.StatusForbidden {
			t.Errorf("support staff: %s = %d, want 403", rt, st)
		}
	}
}

// No customer-facing code reaches intelligence: the customer app, marketing
// site and chat widget never reference the intelligence or finance API.
func TestCustomerFacingAppsNeverCallIntelligence(t *testing.T) {
	root := "../../../../"
	dirs := []string{"apps/web/src", "apps/marketing/src", "packages/chat/src"}
	bad := regexp.MustCompile(`/admin/intelligence|/admin/finance|\.mi\.|api\.finance\.`)
	checked := 0
	for _, d := range dirs {
		_ = filepath.WalkDir(filepath.Join(root, d), func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || !(strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx")) {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			checked++
			if bad.Match(b) {
				t.Errorf("%s references Mise Intelligence", p)
			}
			return nil
		})
	}
	if checked < 50 {
		t.Fatalf("only %d customer-facing source files found; the paths moved", checked)
	}
}
