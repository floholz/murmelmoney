// Client-side occurrence math for recurring templates.
//
// Occurrence n of a template is ALWAYS computed from the original start date
// (start + n*interval, day-of-month clamped to the target month's length),
// never from the previous occurrence — otherwise Jan 31 → Feb 28 → Mar 28
// would drift. All math is UTC; dates compare as YYYY-MM-DD strings.
// Keep in sync with recurring.go (server-side generator); shared test vectors
// live in recurring_test.go:
//   monthly  2025-01-31 n=1 → 2025-02-28, n=2 → 2025-03-31, n=3 → 2025-04-30
//   monthly  2024-01-31 n=1 → 2024-02-29 (leap)
//   quarterly 2024-11-30 n=1 → 2025-02-28
//   half-yearly 2024-08-31 n=1 → 2025-02-28
//   yearly   2024-02-29 n=1 → 2025-02-28, n=4 → 2028-02-29
//   weekly   2025-01-03 n=1 → 2025-01-10 (Friday stays Friday)
//   2 weeks  2025-01-03 n=3 → 2025-02-14;  2 years 2024-02-29 n=1 → 2026-02-28
import { AREAS, type Area, type Interval, type Recurring } from './pb'

const iso = (t: number) => new Date(t).toISOString().slice(0, 10)

// --- interval syntax ----------------------------------------------------------
// A preset name or "<n> week(s)|month(s)|year(s)" (the server also accepts an
// "every " prefix and stores everything in canonical form — see recurring.go).
export type IntervalUnit = 'week' | 'month' | 'year'
export const INTERVAL_UNITS: IntervalUnit[] = ['week', 'month', 'year']
export const INTERVAL_PRESETS: Record<string, { count: number; unit: IntervalUnit }> = {
  weekly: { count: 1, unit: 'week' }, monthly: { count: 1, unit: 'month' }, quarterly: { count: 3, unit: 'month' },
  'half-yearly': { count: 6, unit: 'month' }, yearly: { count: 1, unit: 'year' },
}

export function parseInterval(s: Interval): { count: number; unit: IntervalUnit } | null {
  s = s.trim().toLowerCase().replace(/^every /, '')
  if (INTERVAL_PRESETS[s]) return INTERVAL_PRESETS[s]
  const m = /^([1-9][0-9]{0,2}) (week|month|year)s?$/.exec(s)
  return m ? { count: Number(m[1]), unit: m[2] as IntervalUnit } : null
}

/** Shortest spelling of an interval ("1 month" → "monthly", "12 months" → "yearly", "2 week" → "2 weeks"). */
export function canonicalInterval(count: number, unit: IntervalUnit): Interval {
  if (unit === 'month' && count % 12 === 0) { count /= 12; unit = 'year' }
  for (const [name, p] of Object.entries(INTERVAL_PRESETS)) if (p.count === count && p.unit === unit) return name
  return `${count} ${unit}${count === 1 ? '' : 's'}`
}

/** Human label: presets as-is, everything else "every 2 weeks". */
export const intervalLabel = (s: Interval) =>
  INTERVAL_PRESETS[s] ? s : parseInterval(s) ? 'every ' + canonicalInterval(parseInterval(s)!.count, parseInterval(s)!.unit) : s

export function nthDate(start: string, interval: Interval, n: number): string {
  const s = new Date(start.slice(0, 10) + 'T00:00:00Z')
  const p = parseInterval(interval)
  if (!p) return start.slice(0, 10)
  if (p.unit === 'week') return iso(s.getTime() + n * p.count * 7 * 86400_000)
  const months = (p.unit === 'year' ? 12 : 1) * p.count * n
  const y = s.getUTCFullYear(), m = s.getUTCMonth() + months
  const last = new Date(Date.UTC(y, m + 1, 0)).getUTCDate() // day 0 of next month = last of target
  return iso(Date.UTC(y, m, Math.min(s.getUTCDate(), last)))
}

/** Saturday/Sunday → following Monday (rent-style "next weekday" payments). */
export function shiftToWeekday(date: string): string {
  const wd = new Date(date + 'T00:00:00Z').getUTCDay()
  if (wd === 6) return iso(Date.parse(date + 'T00:00:00Z') + 2 * 86400_000)
  if (wd === 0) return iso(Date.parse(date + 'T00:00:00Z') + 86400_000)
  return date
}

/** Occurrence dates of a template in (afterExcl, toIncl], honoring its end date. */
export function occurrences(t: Recurring, afterExcl: string, toIncl: string): string[] {
  const start = t.start.slice(0, 10)
  const end = t.end ? t.end.slice(0, 10) : ''
  const limit = end && end < toIncl ? end : toIncl
  const out: string[] = []
  if (!parseInterval(t.interval)) return out
  for (let n = 0; ; n++) {
    let d = nthDate(start, t.interval, n)
    if (t.weekdays_only) d = shiftToWeekday(d)
    if (d > limit) break
    if (d > afterExcl) out.push(d)
  }
  return out
}

const todayIso = () => new Date().toISOString().slice(0, 10)

/** How many past occurrences saving this template would create right away. */
export const pendingCount = (t: Recurring, today = todayIso()) =>
  t.active ? occurrences(t, t.last_generated?.slice(0, 10) ?? '', today).length : 0

export interface Projection { income: number; expenses: number; net: number; area: Record<Area, { income: number; expenses: number; net: number }> }

/**
 * Planned (not yet materialized) recurring amounts of a year. The server only
 * generates occurrences <= today; we only project > max(today, last_generated),
 * so actual + projected never double-counts.
 */
export function projectRecurring(templates: Recurring[], year: number, today = todayIso()): Projection {
  const p: Projection = {
    income: 0, expenses: 0, net: 0,
    area: Object.fromEntries(AREAS.map(a => [a, { income: 0, expenses: 0, net: 0 }])) as Projection['area'],
  }
  const yearEnd = `${year}-12-31`
  for (const t of templates) {
    if (!t.active) continue
    const wm = t.last_generated ? t.last_generated.slice(0, 10) : ''
    const after = wm > today ? wm : today
    for (const d of occurrences(t, after, yearEnd)) {
      if (d < `${year}-01-01`) continue
      const buckets = [p, p.area[t.area]]
      for (const b of buckets) {
        if (t.type === 'income') b.income += t.amount
        else b.expenses += t.amount
        b.net = b.income - b.expenses
      }
    }
  }
  return p
}

/** Next occurrence after today ('' if the template is done or paused). */
export function nextOccurrence(t: Recurring, today = todayIso()): string {
  if (!t.active) return ''
  // The next occurrence lies within one step after max(today, start) (+ at
  // most 2 days of weekday shift); two steps is a safe horizon.
  const start = t.start.slice(0, 10)
  const horizon = nthDate(start > today ? start : today, t.interval, 2)
  return occurrences(t, today, horizon)[0] ?? ''
}
