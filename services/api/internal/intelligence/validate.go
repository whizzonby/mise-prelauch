package intelligence

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Numeric validation: every number the model writes (in its answer, claims
// and tables) must be a value from the tool results of this run, shown as
// is or rounded to the decimals the model wrote. Money facts (minor units)
// may also be written in major units, ratios as percentages. Dates are
// ignored; numbers the user wrote in the question are allowed.

// Claim is one factual statement tied to a tool result.
type Claim struct {
	Text  string   `json:"text"`
	Ref   string   `json:"ref"`
	Fact  string   `json:"fact"`
	Value *float64 `json:"value"`
}

// AnswerTable is a table the model built from tool results.
type AnswerTable struct {
	Title   string     `json:"title"`
	Ref     string     `json:"ref"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// Chart is a chart over one of the answer's tables.
type Chart struct {
	Type        string `json:"type"` // bar or line
	Title       string `json:"title"`
	Table       int    `json:"table"`
	LabelColumn int    `json:"label_column"`
	ValueColumn int    `json:"value_column"`
}

// Answer is the assistant's structured answer.
type Answer struct {
	Answer      string        `json:"answer"`
	Claims      []Claim       `json:"claims"`
	Tables      []AnswerTable `json:"tables"`
	Charts      []Chart       `json:"charts"`
	Assumptions []string      `json:"assumptions"`
	Limitations []string      `json:"limitations"`
	FollowUps   []string      `json:"follow_ups"`
}

// Unsupported is one number or citation that the tool results do not support.
type Unsupported struct {
	Where  string `json:"where"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

// Validation records what was checked.
type Validation struct {
	ClaimsChecked  int           `json:"claims_checked"`
	NumbersChecked int           `json:"numbers_checked"`
	Unsupported    []Unsupported `json:"unsupported"`
	Regenerated    bool          `json:"regenerated"`
	Removed        int           `json:"removed"`
}

var (
	dateRe   = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(T[0-9:.+Z-]+)?\b`)
	timeRe   = regexp.MustCompile(`\b\d{1,2}:\d{2}(:\d{2})?\b`)
	refRe    = regexp.MustCompile(`\bE\d+\b`)
	numberRe = regexp.MustCompile(`-?\d{1,3}(?:,\d{3})+(?:\.\d+)?|-?\d+(?:\.\d+)?`)
)

type numberToken struct {
	text  string
	value float64
	dec   int
	start int
	end   int
}

// numbers finds the numbers in text, skipping dates, times and evidence refs.
func numbers(text string) []numberToken {
	masked := []byte(text)
	for _, re := range []*regexp.Regexp{dateRe, timeRe, refRe} {
		for _, m := range re.FindAllStringIndex(text, -1) {
			for i := m[0]; i < m[1]; i++ {
				masked[i] = ' '
			}
		}
	}
	var out []numberToken
	for _, m := range numberRe.FindAllStringIndex(string(masked), -1) {
		// Part of a word (e.g. "Q3", "v2", "abc123"): not a quantity.
		if m[0] > 0 && isLetter(masked[m[0]-1]) || m[1] < len(masked) && isLetter(masked[m[1]]) && !unitSuffix(string(masked[m[1]:])) {
			continue
		}
		// "10-20" is a range, not minus twenty.
		if masked[m[0]] == '-' && m[0] > 0 && masked[m[0]-1] != ' ' && masked[m[0]-1] != '(' && masked[m[0]-1] != '\n' {
			m[0]++
		}
		raw := text[m[0]:m[1]]
		clean := strings.ReplaceAll(raw, ",", "")
		v, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			continue
		}
		dec := 0
		if i := strings.IndexByte(clean, '.'); i >= 0 {
			dec = len(clean) - i - 1
		}
		out = append(out, numberToken{text: raw, value: v, dec: dec, start: m[0], end: m[1]})
	}
	return out
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_' }

// unitSuffix: "12h", "3d", "45x" read as numbers with a unit.
func unitSuffix(rest string) bool {
	if rest == "" {
		return false
	}
	end := 1
	for end < len(rest) && isLetter(rest[end]) {
		end++
	}
	switch strings.ToLower(rest[:end]) {
	case "h", "hr", "hrs", "d", "x", "k", "kg", "g", "ml", "l", "pp":
		return true
	}
	return false
}

// support is the set of values a run's tool results allow.
type support struct {
	values []float64
	facts  map[string]map[string]Fact // ref -> key -> fact
	tables map[string]bool            // refs that exist
}

func newSupport(results map[string]*Result, question string) *support {
	s := &support{facts: map[string]map[string]Fact{}, tables: map[string]bool{}}
	add := func(v float64) { s.values = append(s.values, v) }
	for ref, r := range results {
		s.tables[ref] = true
		s.facts[ref] = map[string]Fact{}
		for _, f := range r.Facts {
			s.facts[ref][f.Key] = f
			if f.Value == nil {
				continue
			}
			for _, v := range factForms(f) {
				add(v)
			}
		}
		for _, t := range r.Tables {
			add(float64(len(t.Rows)))
			for _, row := range t.Rows {
				for _, cell := range row {
					for _, n := range numbers(cell) {
						add(n.value)
					}
				}
			}
		}
		for _, txt := range append(append([]string{}, r.Limitations...), r.Notes...) {
			for _, n := range numbers(txt) {
				add(n.value)
			}
		}
		add(float64(len(r.Facts)))
	}
	for _, n := range numbers(question) {
		add(n.value)
	}
	return s
}

// factForms are the ways a fact's value may be written.
func factForms(f Fact) []float64 {
	v := *f.Value
	forms := []float64{v}
	switch f.Unit {
	case "money_minor":
		forms = append(forms, v/100)
	case "ratio":
		forms = append(forms, v*100)
	}
	for _, x := range forms {
		if x < 0 {
			forms = append(forms, -x)
		}
	}
	return forms
}

// matches: x, written with dec decimals, is v rounded to dec decimals.
func matches(x float64, dec int, v float64) bool {
	p := math.Pow(10, float64(min(dec, 6)))
	return math.Abs(math.Round(v*p)/p-x) <= 1e-9*math.Max(1, math.Abs(x))
}

func (s *support) has(n numberToken) bool {
	for _, v := range s.values {
		if matches(n.value, n.dec, v) {
			return true
		}
	}
	return false
}

func decimalsOf(x float64) int {
	str := strconv.FormatFloat(x, 'f', -1, 64)
	if i := strings.IndexByte(str, '.'); i >= 0 {
		return len(str) - i - 1
	}
	return 0
}

// check validates an answer. It returns every unsupported number or citation.
func (s *support) check(a *Answer) Validation {
	v := Validation{Unsupported: []Unsupported{}}
	text := func(where, t string) {
		for _, n := range numbers(t) {
			v.NumbersChecked++
			if !s.has(n) {
				v.Unsupported = append(v.Unsupported, Unsupported{Where: where, Value: n.text, Reason: "not in the tool results"})
			}
		}
	}
	text("answer", a.Answer)
	for i, c := range a.Claims {
		v.ClaimsChecked++
		where := fmt.Sprintf("claims[%d]", i)
		facts, ok := s.facts[c.Ref]
		if !ok {
			v.Unsupported = append(v.Unsupported, Unsupported{Where: where, Value: c.Ref, Reason: "cites a result this answer did not retrieve"})
			continue
		}
		if c.Fact != "" || c.Value != nil {
			f, ok := facts[c.Fact]
			switch {
			case !ok:
				v.Unsupported = append(v.Unsupported, Unsupported{Where: where, Value: c.Fact, Reason: "cites a fact the result does not contain"})
				continue
			case f.Value == nil && c.Value != nil:
				v.Unsupported = append(v.Unsupported, Unsupported{Where: where, Value: strconv.FormatFloat(*c.Value, 'f', -1, 64), Reason: "gives a value for a fact that is not available"})
				continue
			case c.Value != nil:
				v.NumbersChecked++
				ok := false
				for _, form := range factForms(f) {
					if matches(*c.Value, decimalsOf(*c.Value), form) {
						ok = true
					}
				}
				if !ok {
					v.Unsupported = append(v.Unsupported, Unsupported{Where: where, Value: strconv.FormatFloat(*c.Value, 'f', -1, 64), Reason: "does not equal " + c.Fact})
					continue
				}
			}
		}
		text(where, c.Text)
	}
	for i, t := range a.Tables {
		where := fmt.Sprintf("tables[%d]", i)
		if !s.tables[t.Ref] {
			v.Unsupported = append(v.Unsupported, Unsupported{Where: where, Value: t.Ref, Reason: "cites a result this answer did not retrieve"})
			continue
		}
		for _, row := range t.Rows {
			for _, cell := range row {
				text(where, cell)
			}
		}
	}
	for i, t := range a.Assumptions {
		text(fmt.Sprintf("assumptions[%d]", i), t)
	}
	for i, t := range a.Limitations {
		text(fmt.Sprintf("limitations[%d]", i), t)
	}
	return v
}

const unverified = "[unverified]"

// strip removes what validation rejected: claims and tables with bad
// citations or values, and unsupported numbers inside text.
func (s *support) strip(a *Answer) int {
	removed := 0
	redact := func(t string) string {
		ns := numbers(t)
		for i := len(ns) - 1; i >= 0; i-- {
			if !s.has(ns[i]) {
				t = t[:ns[i].start] + unverified + t[ns[i].end:]
				removed++
			}
		}
		return t
	}
	a.Answer = redact(a.Answer)
	claims := a.Claims[:0]
	for _, c := range a.Claims {
		one := &Answer{Claims: []Claim{c}}
		if len(s.check(one).Unsupported) > 0 {
			removed++
			continue
		}
		claims = append(claims, c)
	}
	a.Claims = claims
	tables := a.Tables[:0]
	keep := map[int]int{}
	for i, t := range a.Tables {
		if !s.tables[t.Ref] {
			removed++
			continue
		}
		for r := range t.Rows {
			for c := range t.Rows[r] {
				t.Rows[r][c] = redact(t.Rows[r][c])
			}
		}
		keep[i] = len(tables)
		tables = append(tables, t)
	}
	a.Tables = tables
	charts := a.Charts[:0]
	for _, c := range a.Charts {
		if j, ok := keep[c.Table]; ok {
			c.Table = j
			charts = append(charts, c)
		}
	}
	a.Charts = charts
	for i := range a.Assumptions {
		a.Assumptions[i] = redact(a.Assumptions[i])
	}
	for i := range a.Limitations {
		a.Limitations[i] = redact(a.Limitations[i])
	}
	return removed
}

// tidy drops charts that do not fit their table (shape only; numbers are
// already checked as table cells).
func tidy(a *Answer) {
	charts := a.Charts[:0]
	for _, c := range a.Charts {
		if c.Type != "bar" && c.Type != "line" || c.Table < 0 || c.Table >= len(a.Tables) {
			continue
		}
		cols := len(a.Tables[c.Table].Columns)
		if c.LabelColumn < 0 || c.LabelColumn >= cols || c.ValueColumn < 0 || c.ValueColumn >= cols || c.LabelColumn == c.ValueColumn {
			continue
		}
		charts = append(charts, c)
	}
	a.Charts = charts
	for _, p := range []*[]string{&a.Assumptions, &a.Limitations, &a.FollowUps} {
		if *p == nil {
			*p = []string{}
		}
	}
	if a.Claims == nil {
		a.Claims = []Claim{}
	}
	if a.Tables == nil {
		a.Tables = []AnswerTable{}
	}
	if a.Charts == nil {
		a.Charts = []Chart{}
	}
}
