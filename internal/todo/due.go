package todo

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DateLayout is how a due date is written back to the file: ISO, so the token
// sorts, greps and reads the same everywhere.
const DateLayout = "2006-01-02"

// Day truncates t to local midnight. Due dates are days, not instants, and
// comparing them as instants makes "due today" true only until midday.
func Day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// ParseDue reads the shorthands a person types into the add box — "today",
// "fri", "+3d", "2026-09-01" — relative to now.
//
// It is deliberately forgiving about what it accepts and exact about what it
// writes: whatever is typed ends up in the file as one ISO date.
func ParseDue(s string, now time.Time) (time.Time, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return time.Time{}, false
	}
	today := Day(now)

	switch s {
	case "today", "tod":
		return today, true
	case "tomorrow", "tmr", "tom":
		return today.AddDate(0, 0, 1), true
	case "yesterday":
		return today.AddDate(0, 0, -1), true
	case "none", "never", "clear", "-":
		// An explicit "no date", so "due:none" can take one off again.
		return time.Time{}, true
	}

	// "+3d" or "3d": that many days out.
	if n, ok := parseOffset(s); ok {
		return today.AddDate(0, 0, n), true
	}

	// A weekday name means its next occurrence, never today: "due:mon" on a
	// Monday is a plan for next week, not for the day already underway.
	if wd, ok := parseWeekday(s); ok {
		delta := (int(wd) - int(today.Weekday()) + 7) % 7
		if delta == 0 {
			delta = 7
		}
		return today.AddDate(0, 0, delta), true
	}

	if t, err := time.ParseInLocation(DateLayout, s, now.Location()); err == nil {
		return Day(t), true
	}
	// "09-01" is this year, or next year if that date has already gone.
	if t, err := time.ParseInLocation("01-02", s, now.Location()); err == nil {
		d := time.Date(today.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		if d.Before(today) {
			d = d.AddDate(1, 0, 0)
		}
		return d, true
	}
	return time.Time{}, false
}

// parseOffset reads "+3d", "3d" or "+3" as a number of days.
func parseOffset(s string) (int, bool) {
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimSuffix(s, "d")
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

var weekdays = []time.Weekday{
	time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
	time.Thursday, time.Friday, time.Saturday,
}

// parseWeekday matches a full weekday name or any prefix of one that is not
// ambiguous, so "tue", "thu" and "thursday" all work but "t" does not.
func parseWeekday(s string) (time.Weekday, bool) {
	var found time.Weekday
	matches := 0
	for _, wd := range weekdays {
		if strings.HasPrefix(strings.ToLower(wd.String()), s) {
			found, matches = wd, matches+1
		}
	}
	return found, matches == 1
}

// DueLabel is how a date reads in the list: the nearer it is, the more it says
// in words, because "today" and "2d ago" are what a person acts on and a date
// two months out is only there for reference.
func DueLabel(due, now time.Time) string {
	if due.IsZero() {
		return ""
	}
	today := Day(now)
	days := int(Day(due).Sub(today).Hours() / 24)

	switch {
	case days == 0:
		return "today"
	case days == 1:
		return "tomorrow"
	case days == -1:
		return "yesterday"
	case days < 0:
		return fmt.Sprintf("%dd ago", -days)
	case days < 7:
		return due.Format("Mon")
	case due.Year() == today.Year():
		return due.Format("2 Jan")
	default:
		return due.Format("2 Jan 06")
	}
}
