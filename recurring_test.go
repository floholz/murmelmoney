package main

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// These vectors are mirrored in ui/src/lib/recurring.ts — keep both in sync.
func TestNthOccurrence(t *testing.T) {
	cases := []struct {
		start, interval string
		n               int
		want            string
	}{
		// monthly from Jan 31: clamps to month end, no drift (n from start, not previous)
		{"2025-01-31", "monthly", 1, "2025-02-28"},
		{"2024-01-31", "monthly", 1, "2024-02-29"}, // leap year
		{"2025-01-31", "monthly", 2, "2025-03-31"},
		{"2025-01-31", "monthly", 3, "2025-04-30"},
		{"2025-01-15", "monthly", 12, "2026-01-15"},
		// quarterly
		{"2024-11-30", "quarterly", 1, "2025-02-28"},
		{"2024-11-30", "quarterly", 2, "2025-05-30"},
		// yearly
		{"2024-02-29", "yearly", 1, "2025-02-28"},
		{"2024-02-29", "yearly", 4, "2028-02-29"},
		// half-yearly
		{"2024-08-31", "half-yearly", 1, "2025-02-28"},
		{"2024-08-31", "half-yearly", 2, "2025-08-31"},
		// weekly keeps the weekday
		{"2025-01-03", "weekly", 1, "2025-01-10"}, // Friday → Friday
		{"2025-01-03", "weekly", 5, "2025-02-07"},
		// generic "<n> unit" syntax
		{"2025-01-03", "2 weeks", 3, "2025-02-14"},
		{"2025-01-31", "2 months", 1, "2025-03-31"},
		{"2025-01-31", "every 2 months", 1, "2025-03-31"},
		{"2024-02-29", "2 years", 1, "2026-02-28"},
		{"2025-01-31", "18 months", 1, "2026-07-31"},
		// n = 0 is the start itself
		{"2025-06-01", "monthly", 0, "2025-06-01"},
	}
	for _, c := range cases {
		got := nthOccurrence(d(c.start), c.interval, c.n)
		if got.Format("2006-01-02") != c.want {
			t.Errorf("nthOccurrence(%s, %s, %d) = %s, want %s", c.start, c.interval, c.n, got.Format("2006-01-02"), c.want)
		}
		if got.Weekday() != d(c.want).Weekday() && (c.interval == "weekly" || c.interval == "2 weeks") {
			t.Errorf("weekly occurrence changed weekday: %s", got)
		}
	}
}

func TestShiftToWeekday(t *testing.T) {
	cases := map[string]string{
		"2026-08-01": "2026-08-03", // Sat → Mon
		"2026-11-01": "2026-11-02", // Sun → Mon
		"2026-09-01": "2026-09-01", // Tue stays
		"2026-08-28": "2026-08-28", // Fri stays
	}
	for in, want := range cases {
		if got := shiftToWeekday(d(in)).Format("2006-01-02"); got != want {
			t.Errorf("shiftToWeekday(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestIntervalSyntax(t *testing.T) {
	canon := map[string]string{
		"weekly": "weekly", "monthly": "monthly", "quarterly": "quarterly", "half-yearly": "half-yearly", "yearly": "yearly",
		"1 week": "weekly", "1 month": "monthly", "3 months": "quarterly", "6 months": "half-yearly",
		"12 months": "yearly", "1 year": "yearly", "24 months": "2 years",
		"2 weeks": "2 weeks", "2 week": "2 weeks", "every 2 weeks": "2 weeks", " Every 18 Months ": "18 months",
		"5 years": "5 years",
	}
	for in, want := range canon {
		got, ok := canonicalInterval(in)
		if !ok || got != want {
			t.Errorf("canonicalInterval(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "daily", "3 days", "0 weeks", "-1 month", "1000 months", "two weeks", "weeks", "monthly monthly", "every"} {
		if _, ok := canonicalInterval(bad); ok {
			t.Errorf("canonicalInterval(%q) accepted", bad)
		}
	}
	if got := nthOccurrence(d("2025-01-01"), "daily", 3).Format("2006-01-02"); got != "2025-01-01" {
		t.Errorf("unknown interval should not advance: %s", got)
	}
}
