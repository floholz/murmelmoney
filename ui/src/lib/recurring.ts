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
//   yearly   2024-02-29 n=1 → 2025-02-28, n=4 → 2028-02-29
//   weekly   2025-01-03 n=1 → 2025-01-10 (Friday stays Friday)
import { AREAS, type Area, type Interval, type Recurring } from './pb'

const iso = (t: number) => new Date(t).toISOString().slice(0, 10)

export function nthDate(start: string, interval: Interval, n: number): string {
  const s = new Date(start.slice(0, 10) + 'T00:00:00Z')
  if (interval === 'weekly') return iso(s.getTime() + n * 7 * 86400_000)
  const months = { monthly: 1, quarterly: 3, yearly: 12 }[interval] * n
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
  const horizon = nthDate(today, 'yearly', 2) // look ahead 2 years max
  return occurrences(t, today, horizon)[0] ?? ''
}
