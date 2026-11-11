package menus

import (
	"fmt"
	"strings"
	"time"
)

// TimeZone is the business time zone: weeks, cutoffs and reminders are
// reckoned in Trinidad and Tobago local time. Times are stored in UTC.
const TimeZone = "America/Port_of_Spain"

// Policy is when selection closes for a delivery week. A week is identified
// by its Monday (a local date). The cutoff is the last CutoffWeekday before
// that Monday, at CutoffHour:CutoffMinute local time.
//
// Owner's decision (11 November 2026): Thursday 23:59.
type Policy struct {
	Location      *time.Location
	CutoffWeekday time.Weekday
	CutoffHour    int
	CutoffMinute  int
}

// DefaultPolicy is Thursday 23:59 in Trinidad and Tobago.
func DefaultPolicy() Policy {
	return Policy{Location: Location(), CutoffWeekday: time.Thursday, CutoffHour: 23, CutoffMinute: 59}
}

// Location is the business time zone. Trinidad and Tobago has no daylight
// saving time; the fixed offset is the fallback if tzdata is missing.
func Location() *time.Location {
	if loc, err := time.LoadLocation(TimeZone); err == nil {
		return loc
	}
	return time.FixedZone("AST", -4*3600)
}

// ParsePolicy reads "thursday" and "23:59".
func ParsePolicy(weekday, clock string) (Policy, error) {
	p := DefaultPolicy()
	found := false
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), strings.TrimSpace(weekday)) {
			p.CutoffWeekday, found = d, true
		}
	}
	if !found {
		return p, fmt.Errorf("%q is not a weekday", weekday)
	}
	t, err := time.Parse("15:04", strings.TrimSpace(clock))
	if err != nil {
		return p, fmt.Errorf("%q is not a time like 23:59", clock)
	}
	p.CutoffHour, p.CutoffMinute = t.Hour(), t.Minute()
	return p, nil
}

// Date returns the calendar date of d as a date value (midnight UTC), which
// is how dates travel to and from PostgreSQL.
func Date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// WeekOf returns the Monday of the local week containing t.
func (p Policy) WeekOf(t time.Time) time.Time {
	local := t.In(p.Location)
	offset := (int(local.Weekday()) + 6) % 7 // Monday = 0
	return Date(local.Year(), local.Month(), local.Day()-offset)
}

// IsWeekStart reports whether d is a Monday date.
func IsWeekStart(d time.Time) bool { return d.Weekday() == time.Monday }

// CutoffFor returns the moment selection closes for the week starting on
// Monday week.
func (p Policy) CutoffFor(week time.Time) time.Time {
	back := (int(time.Monday) - int(p.CutoffWeekday) + 7) % 7
	if back == 0 {
		back = 7
	}
	return time.Date(week.Year(), week.Month(), week.Day()-back, p.CutoffHour, p.CutoffMinute, 0, 0, p.Location).UTC()
}

// NextOpenWeek is the earliest week whose cutoff is still ahead of now: the
// week a new subscriber starts with, and the week customers are choosing for.
func (p Policy) NextOpenWeek(now time.Time) time.Time {
	week := p.WeekOf(now)
	for !p.CutoffFor(week).After(now) {
		week = week.AddDate(0, 0, 7)
	}
	return week
}

// ParseWeek reads "2026-11-16" and checks it is a Monday.
func ParseWeek(s string) (time.Time, bool) {
	d, err := time.Parse("2006-01-02", s)
	if err != nil || !IsWeekStart(d) {
		return time.Time{}, false
	}
	return d, true
}

// FormatWeek writes a week as "2026-11-16".
func FormatWeek(d time.Time) string { return d.Format("2006-01-02") }

// WeekLabel is "16 – 22 November 2026" for humans.
func WeekLabel(week time.Time) string {
	end := week.AddDate(0, 0, 6)
	switch {
	case week.Year() != end.Year():
		return week.Format("2 January 2006") + " – " + end.Format("2 January 2006")
	case week.Month() != end.Month():
		return week.Format("2 January") + " – " + end.Format("2 January 2006")
	}
	return week.Format("2") + " – " + end.Format("2 January 2006")
}
