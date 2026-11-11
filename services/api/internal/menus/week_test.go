package menus

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, Location())
	if err != nil {
		panic(err)
	}
	return t
}

func TestCutoffIsThursdayBeforeTheWeek(t *testing.T) {
	p := DefaultPolicy()
	week := Date(2026, time.November, 23)
	got := p.CutoffFor(week)
	want := at("2026-11-19 23:59")
	if !got.Equal(want) {
		t.Fatalf("cutoff = %v, want %v", got.In(Location()), want)
	}
	// Stored in UTC: 23:59 in Trinidad (UTC-4) is 03:59 the next day UTC.
	if got.Location() != time.UTC || got.Hour() != 3 || got.Day() != 20 {
		t.Errorf("cutoff in UTC = %v", got)
	}
}

func TestNextOpenWeek(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct{ now, want string }{
		{"2026-11-11 09:00", "2026-11-16"}, // Wednesday: this Thursday's cutoff is for next week
		{"2026-11-12 23:58", "2026-11-16"}, // a minute before the cutoff
		{"2026-11-12 23:59", "2026-11-23"}, // at the cutoff, it has closed
		{"2026-11-13 08:00", "2026-11-23"}, // Friday
		{"2026-11-16 00:00", "2026-11-23"}, // Monday of the delivery week
		{"2026-12-31 12:00", "2027-01-04"}, // over the new year
	}
	for _, c := range cases {
		got := FormatWeek(p.NextOpenWeek(at(c.now)))
		if got != c.want {
			t.Errorf("NextOpenWeek(%s) = %s, want %s", c.now, got, c.want)
		}
	}
}

func TestWeekOfUsesLocalTime(t *testing.T) {
	p := DefaultPolicy()
	// 02:00 UTC on Monday 16 November is still Sunday 22:00 in Trinidad.
	utc := time.Date(2026, time.November, 16, 2, 0, 0, 0, time.UTC)
	if got := FormatWeek(p.WeekOf(utc)); got != "2026-11-09" {
		t.Errorf("WeekOf = %s, want 2026-11-09", got)
	}
}

func TestParsePolicyAndWeek(t *testing.T) {
	p, err := ParsePolicy("Wednesday", "12:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.CutoffFor(Date(2026, time.November, 23)); !got.Equal(at("2026-11-18 12:00")) {
		t.Errorf("Wednesday noon cutoff = %v", got.In(Location()))
	}
	if _, err := ParsePolicy("someday", "12:00"); err == nil {
		t.Error("bad weekday accepted")
	}
	if _, err := ParsePolicy("monday", "25:00"); err == nil {
		t.Error("bad time accepted")
	}
	if _, ok := ParseWeek("2026-11-17"); ok {
		t.Error("a Tuesday was accepted as a week")
	}
	if _, ok := ParseWeek("2026-11-16"); !ok {
		t.Error("a Monday was refused")
	}
	if got := WeekLabel(Date(2026, time.November, 30)); got != "30 November – 6 December 2026" {
		t.Errorf("label = %q", got)
	}
}
