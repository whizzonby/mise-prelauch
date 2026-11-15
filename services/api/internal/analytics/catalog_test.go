package analytics

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const catalogPath = "../../../../docs/analytics/METRIC_CATALOG.md"

// The catalogue document is generated from the registry. Run with
// UPDATE_CATALOG=1 to rewrite it; otherwise a stale file fails the build.
func TestCatalogIsUpToDate(t *testing.T) {
	want := Catalog()
	if os.Getenv("UPDATE_CATALOG") == "1" {
		if err := os.WriteFile(filepath.Clean(catalogPath), []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Clean(catalogPath))
	if err != nil {
		t.Fatalf("read catalogue: %v", err)
	}
	if strings.ReplaceAll(string(got), "\r\n", "\n") != want {
		t.Fatal("docs/analytics/METRIC_CATALOG.md is out of date: run the analytics tests with UPDATE_CATALOG=1")
	}
}

// Every metric is complete, uses only known placeholders and declares the
// filters its SQL uses (so a filter is never silently ignored).
func TestRegistryIsComplete(t *testing.T) {
	used := regexp.MustCompile(`\{(plan|meal|zone|supplier)\}`)
	for _, m := range Metrics() {
		if m.Name == "" || m.Definition == "" || m.Calculation == "" || m.Missing == "" || m.Owner == "" || m.Limitations == "" || m.Permission == "" || len(m.Sources) == 0 {
			t.Errorf("%s is missing catalogue fields", m.ID)
		}
		if m.Kind == Money && m.Currency == "" {
			t.Errorf("%s is money without a currency rule", m.ID)
		}
		for _, f := range used.FindAllStringSubmatch(m.SQL, -1) {
			ok := false
			for _, d := range m.Filters {
				ok = ok || d == f[1]
			}
			if !ok {
				t.Errorf("%s uses filter %s without declaring it", m.ID, f[1])
			}
		}
		sql, args := bind(m.SQL, map[string]any{})
		if strings.Contains(sql, "{") || len(args) == 0 && strings.Contains(m.SQL, "{") {
			t.Errorf("%s: unbound placeholder in %q", m.ID, sql)
		}
	}
	for kind, d := range dashboards {
		for _, s := range d.sections {
			for _, id := range s.metrics {
				if _, ok := Get(id); !ok {
					t.Errorf("dashboard %s uses unknown metric %s", kind, id)
				}
			}
		}
	}
}

func TestSafeCSV(t *testing.T) {
	for in, want := range map[string]string{"=HYPERLINK(1)": "'=HYPERLINK(1)", "+1": "'+1", "@x": "'@x", "-12.5": "-12.5", "-cmd": "'-cmd", "plain": "plain", "": ""} {
		if got := SafeCSV(in); got != want {
			t.Errorf("SafeCSV(%q) = %q, want %q", in, got, want)
		}
	}
}
