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
		// weekly keeps the weekday
		{"2025-01-03", "weekly", 1, "2025-01-10"}, // Friday → Friday
		{"2025-01-03", "weekly", 5, "2025-02-07"},
		// n = 0 is the start itself
		{"2025-06-01", "monthly", 0, "2025-06-01"},
	}
	for _, c := range cases {
		got := nthOccurrence(d(c.start), c.interval, c.n)
		if got.Format("2006-01-02") != c.want {
			t.Errorf("nthOccurrence(%s, %s, %d) = %s, want %s", c.start, c.interval, c.n, got.Format("2006-01-02"), c.want)
		}
		if got.Weekday() != d(c.want).Weekday() && c.interval == "weekly" {
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
