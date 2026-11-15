package analytics

import (
	"testing"
	"time"
)

// Local dates become UTC bounds once: 1–2 Nov in Trinidad and Tobago (UTC−4)
// runs from 04:00 UTC on 1 Nov to 04:00 UTC on 3 Nov. A payment at 02:30 UTC
// on 3 Nov is still 2 Nov locally and inside the range; one at 04:30 is not.
func TestRangesUseLocalDays(t *testing.T) {
	loc, err := time.LoadLocation("America/Port_of_Spain")
	if err != nil {
		t.Skip("no time zone data")
	}
	s := &Service{Location: loc, Now: func() time.Time { return time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC) }}
	r, err := s.ParseRange("2026-11-01", "2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	f, to := s.bounds(r)
	if !f.Equal(time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 11, 3, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("bounds = %v .. %v", f, to)
	}
	in, out := time.Date(2026, 11, 3, 2, 30, 0, 0, time.UTC), time.Date(2026, 11, 3, 4, 30, 0, 0, time.UTC)
	if !(in.Before(to) && !out.Before(to)) {
		t.Error("boundary misplaced")
	}
	if p := r.Previous(); p.From.Format(day) != "2026-10-30" || p.To.Format(day) != "2026-10-31" {
		t.Errorf("previous = %v..%v", p.From, p.To)
	}
	if _, err := s.ParseRange("2026-11-05", "2026-11-01"); err == nil {
		t.Error("backwards range accepted")
	}
	if d, _ := s.ParseRange("", ""); d.Days() != 28 {
		t.Errorf("default range = %d days", d.Days())
	}
}
