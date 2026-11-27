package intelligence

import (
	"strings"
	"testing"

	"mise.tt/api/internal/analytics"
)

func fv(v float64) *float64 { return &v }

func sampleResults() map[string]*Result {
	return map[string]*Result{
		"E1": {Tool: "financial_summary", Facts: []Fact{
			{Key: "finance.net_collections", Value: fv(1234567), Unit: "money_minor", Currency: "TTD"},
			{Key: "finance.margin", Value: fv(0.31415), Unit: "ratio"},
			{Key: "finance.operating_result", Value: nil, Unit: "money_minor"},
			{Key: "finance.loss", Value: fv(-2500), Unit: "money_minor"},
		}},
		"E2": {Tool: "meal_performance", Tables: []analytics.Table{{Columns: []string{"Meal", "Kits"}, Rows: [][]string{{"Pelau", "1,204"}, {"Curry", "88"}}}}},
	}
}

func TestNumbersSkipDatesRefsAndWords(t *testing.T) {
	got := []string{}
	for _, n := range numbers("From 2026-11-01 to 2026-11-30 at 10:30, E1 shows TT$12,345.67 and 31% (Q3, v2, 10-20 kits, 3rd).") {
		got = append(got, n.text)
	}
	if strings.Join(got, "|") != "12,345.67|31|10|20" {
		t.Fatalf("numbers = %v", got)
	}
}

func TestValidationAcceptsRoundingAndUnitForms(t *testing.T) {
	s := newSupport(sampleResults(), "What about the top 5 meals?")
	a := &Answer{
		Answer: "Net collections were TT$12,345.67 (about 12346), margin 31.4% (0.31), a loss of 25.00. Pelau sold 1,204 kits; 2 meals listed; top 5.",
		Claims: []Claim{{Text: "Net collections", Ref: "E1", Fact: "finance.net_collections", Value: fv(12345.67)},
			{Text: "Margin", Ref: "E1", Fact: "finance.margin", Value: fv(31.4)},
			{Text: "Operating result is withheld", Ref: "E1", Fact: "finance.operating_result"}},
		Tables: []AnswerTable{{Ref: "E2", Rows: [][]string{{"Pelau", "1204"}}}},
	}
	if v := s.check(a); len(v.Unsupported) != 0 {
		t.Fatalf("unsupported = %+v", v.Unsupported)
	}
}

func TestValidationRejectsInventedNumbersAndCitations(t *testing.T) {
	s := newSupport(sampleResults(), "")
	a := &Answer{
		Answer: "Net collections were 12,400 and margin 35%.",
		Claims: []Claim{
			{Text: "Revenue", Ref: "E7", Fact: "finance.net_collections", Value: fv(12345.67)},         // unknown ref
			{Text: "Result", Ref: "E1", Fact: "finance.operating_result", Value: fv(0)},                // withheld given a value
			{Text: "Margin", Ref: "E1", Fact: "finance.margin", Value: fv(0.4)},                        // wrong value
			{Text: "Made up", Ref: "E1", Fact: "finance.profit"},                                       // no such fact
			{Text: "Net collections", Ref: "E1", Fact: "finance.net_collections", Value: fv(12345.67)}, // fine
		},
		Tables: []AnswerTable{{Ref: "E9", Rows: [][]string{{"x", "1"}}}, {Ref: "E2", Rows: [][]string{{"Pelau", "1300"}}}},
		Charts: []Chart{{Type: "bar", Table: 0, LabelColumn: 0, ValueColumn: 1}, {Type: "bar", Table: 1, LabelColumn: 0, ValueColumn: 1}},
	}
	v := s.check(a)
	where := []string{}
	for _, u := range v.Unsupported {
		where = append(where, u.Where+"="+u.Value)
	}
	want := "answer=12,400|answer=35|claims[0]=E7|claims[1]=0|claims[2]=0.4|claims[3]=finance.profit|tables[0]=E9|tables[1]=1300"
	if strings.Join(where, "|") != want {
		t.Fatalf("unsupported = %s", strings.Join(where, "|"))
	}
	removed := s.strip(a)
	if len(a.Claims) != 1 || a.Claims[0].Fact != "finance.net_collections" || len(a.Tables) != 1 || a.Tables[0].Rows[0][1] != unverified ||
		len(a.Charts) != 1 || a.Charts[0].Table != 0 || !strings.Contains(a.Answer, "were [unverified] and margin [unverified]%") || removed != 8 {
		t.Fatalf("after strip (%d removed): %+v", removed, a)
	}
	if v := s.check(a); len(v.Unsupported) != 0 {
		t.Fatalf("still unsupported after strip: %+v", v.Unsupported)
	}
}

func TestScrubRemovesContactDetails(t *testing.T) {
	got := scrub(`{"note":"Contact kavita@example.com or +1 (868) 555-0123 by 2026-11-30"}`)
	if strings.Contains(got, "kavita") || strings.Contains(got, "555-0123") || !strings.Contains(got, "2026-11-30") {
		t.Fatalf("scrub = %s", got)
	}
}

func TestForModelBoundsRows(t *testing.T) {
	r := &Result{Tool: "x", Tables: []analytics.Table{{Columns: []string{"a"}}}}
	for i := 0; i < 100; i++ {
		r.Tables[0].Rows = append(r.Tables[0].Rows, []string{strings.Repeat("y", 500)})
	}
	out := forModel("E1", r)
	if !strings.Contains(out, `"rows_omitted":60`) || strings.Contains(out, strings.Repeat("y", 301)) {
		t.Fatalf("forModel did not bound the result (%d bytes)", len(out))
	}
}
