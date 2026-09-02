// Recurring transaction generator: materializes due occurrences of the
// `recurring` templates as real transactions.
//
// Occurrence n of a template is ALWAYS computed from the original start date
// (start + n*interval, day-of-month clamped to the target month's length),
// never from the previous occurrence — otherwise Jan 31 → Feb 28 → Mar 28
// would drift. All date math is date-only UTC midnight.
// Keep in sync with ui/src/lib/recurring.ts (client-side year projection).
package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// maxPerRun caps how many occurrences a single template may generate in one
// run — the loop is naturally bounded by today, this only guards against a
// corrupted start date flooding the database.
const maxPerRun = 5000

// Interval syntax (the `interval` field of a template):
//
//	weekly | monthly | quarterly | half-yearly | yearly   named presets
//	<n> week(s) | <n> month(s) | <n> year(s)             e.g. "2 weeks", "18 months"
//
// An optional "every " prefix is accepted on input. canonicalInterval reduces
// equivalent spellings to one stored form ("1 month" → "monthly", "6 months"
// → "half-yearly", "12 months" → "yearly"), so what is stored is always the
// shortest form. The smallest interval is a week on purpose: weekdays_only
// shifts dates by up to 2 days and relies on occurrences staying ordered.
var intervalRe = regexp.MustCompile(`^(?:every )?([1-9][0-9]{0,2}) (week|month|year)s?$`)

var intervalPresets = map[string]struct {
	count int
	unit  string
}{
	"weekly": {1, "week"}, "monthly": {1, "month"}, "quarterly": {3, "month"},
	"half-yearly": {6, "month"}, "yearly": {1, "year"},
}

// parseInterval returns the step of an interval as (count, unit) with unit
// being "week", "month" or "year"; ok is false for anything else.
func parseInterval(s string) (count int, unit string, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if p, isPreset := intervalPresets[strings.TrimPrefix(s, "every ")]; isPreset {
		return p.count, p.unit, true
	}
	m := intervalRe.FindStringSubmatch(s)
	if m == nil {
		return 0, "", false
	}
	count, _ = strconv.Atoi(m[1])
	return count, m[2], true
}

// canonicalInterval normalizes a valid interval to its stored form; ok is
// false when it doesn't parse.
func canonicalInterval(s string) (string, bool) {
	count, unit, ok := parseInterval(s)
	if !ok {
		return "", false
	}
	if unit == "month" && count%12 == 0 {
		count, unit = count/12, "year"
	}
	for name, p := range intervalPresets {
		if p.count == count && p.unit == unit {
			return name, true
		}
	}
	if count == 1 {
		return "1 " + unit, true
	}
	return strconv.Itoa(count) + " " + unit + "s", true
}

func nthOccurrence(start time.Time, interval string, n int) time.Time {
	count, unit, ok := parseInterval(interval)
	if !ok {
		return start
	}
	switch unit {
	case "week":
		return start.AddDate(0, 0, 7*count*n)
	case "month":
		return addMonthsClamped(start, count*n)
	default: // year
		return addMonthsClamped(start, 12*count*n)
	}
}

// shiftToWeekday moves Saturday/Sunday dates forward to the following Monday
// (how rent and similar payments are issued). Shifts are at most 2 days and
// intervals at least 7, so occurrence order is preserved.
func shiftToWeekday(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, 2)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	}
	return t
}

func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	// first day of the target month; time.Date normalizes month overflow
	first := time.Date(y, m+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	if last := first.AddDate(0, 1, -1).Day(); d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, time.UTC)
}

// generateMu serializes generation across the cron job, the boot catch-up and
// the record hooks so a template is never processed concurrently.
var generateMu sync.Mutex

// generateRecurring materializes due occurrences for all active templates.
// Errors are logged per template so one broken template can't block the rest.
func generateRecurring(app core.App) {
	generateMu.Lock()
	defer generateMu.Unlock()

	templates, err := app.FindRecordsByFilter("recurring", "active = true", "", 0, 0)
	if err != nil {
		app.Logger().Error("recurring: could not load templates", "err", err)
		return
	}
	total := 0
	for _, rec := range templates {
		n, err := generateForTemplate(app, rec)
		if err != nil {
			app.Logger().Error("recurring: generation failed", "template", rec.Id, "err", err)
		}
		total += n
	}
	if total > 0 {
		app.Logger().Info("recurring: generated transactions", "count", total, "templates", len(templates))
	}
}

// generateTemplate is the single-template variant used by the create/update
// hooks so backfill is visible immediately after saving in the UI.
func generateTemplate(app core.App, rec *core.Record) {
	generateMu.Lock()
	defer generateMu.Unlock()
	if n, err := generateForTemplate(app, rec); err != nil {
		app.Logger().Error("recurring: generation failed", "template", rec.Id, "err", err)
	} else if n > 0 {
		app.Logger().Info("recurring: generated transactions", "count", n, "template", rec.Id)
	}
}

// generateForTemplate creates every due occurrence of one template and advances
// its last_generated watermark, atomically. Returns the number created.
func generateForTemplate(app core.App, rec *core.Record) (int, error) {
	if !rec.GetBool("active") {
		return 0, nil
	}
	start := rec.GetDateTime("start").Time().UTC().Truncate(24 * time.Hour)
	if start.IsZero() {
		return 0, fmt.Errorf("template has no start date")
	}
	interval := rec.GetString("interval")
	if _, _, ok := parseInterval(interval); !ok {
		return 0, fmt.Errorf("invalid interval %q", interval)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	limit := today
	if end := rec.GetDateTime("end").Time().UTC(); !end.IsZero() && end.Before(limit) {
		limit = end
	}
	watermark := rec.GetDateTime("last_generated").Time().UTC()

	created := 0
	err := app.RunInTransaction(func(txApp core.App) error {
		txCol, err := txApp.FindCollectionByNameOrId("transactions")
		if err != nil {
			return err
		}
		weekdaysOnly := rec.GetBool("weekdays_only")
		var newest time.Time
		for n := 0; ; n++ {
			d := nthOccurrence(start, interval, n)
			if weekdaysOnly {
				d = shiftToWeekday(d)
			}
			if d.After(limit) {
				break
			}
			if !d.After(watermark) {
				continue // already generated
			}
			if created >= maxPerRun {
				txApp.Logger().Warn("recurring: per-run cap reached", "template", rec.Id, "cap", maxPerRun)
				break
			}
			t := core.NewRecord(txCol)
			t.Set("user", rec.GetString("user"))
			t.Set("type", rec.GetString("type"))
			t.Set("date", d)
			t.Set("amount", rec.GetFloat("amount"))
			t.Set("area", rec.GetString("area"))
			t.Set("category", rec.GetString("category"))
			t.Set("tags", rec.GetStringSlice("tags"))
			t.Set("note", rec.GetString("note"))
			t.Set("recurring", rec.Id)
			if err := txApp.Save(t); err != nil {
				return err
			}
			newest = d
			created++
		}
		if created > 0 {
			// Raw update on purpose: txApp.Save(rec) would re-fire the
			// recurring update hook (which calls back into this generator)
			// while generateMu is held.
			dt, err := types.ParseDateTime(newest)
			if err != nil {
				return err
			}
			_, err = txApp.DB().Update("recurring",
				dbx.Params{"last_generated": dt.String()},
				dbx.HashExp{"id": rec.Id}).Execute()
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return created, nil
}
