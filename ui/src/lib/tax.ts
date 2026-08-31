import { AREAS, categoryName, tagNames, type Area, type Recurring, type Transaction } from './pb'
import { projectRecurring, type Projection } from './recurring'

export interface Bucket { income: number; expenses: number; net: number }
export interface TxLite { date: string; type: 'income' | 'expense'; area: Area; category: string; tags: string[]; amount: number }
export interface Aggregate {
  year: number
  income: number
  expenses: number
  net: number
  area: Record<Area, Bucket>
  category: Record<string, Bucket>
  tag: Record<string, Bucket>
  transactions: TxLite[]
  /** Planned recurring amounts for the rest of the year (only when templates were passed in). */
  projected?: Projection
}
export interface RuleLine { label: string; value: number | string; hint?: string }

const bucket = (): Bucket => ({ income: 0, expenses: 0, net: 0 })
const add = (b: Bucket, t: { type: 'income' | 'expense'; amount: number }) => {
  if (t.type === 'income') b.income += t.amount
  else b.expenses += t.amount
  b.net = b.income - b.expenses
}

export function aggregate(year: number, txs: Transaction[], opts?: { recurring?: Recurring[] }): Aggregate {
  const a: Aggregate = {
    year, income: 0, expenses: 0, net: 0,
    area: Object.fromEntries(AREAS.map(x => [x, bucket()])) as Record<Area, Bucket>,
    category: {}, tag: {},
    transactions: [],
  }
  const total = bucket()
  for (const t of txs) {
    const lite: TxLite = { date: t.date, type: t.type, area: t.area, category: categoryName(t), tags: tagNames(t), amount: t.amount }
    add(total, lite)
    add(a.area[t.area], lite)
    add((a.category[lite.category || '(none)'] ??= bucket()), lite)
    for (const name of lite.tags) add((a.tag[name] ??= bucket()), lite)
    a.transactions.push(lite)
  }
  Object.assign(a, { income: total.income, expenses: total.expenses, net: total.net })
  if (opts?.recurring) a.projected = projectRecurring(opts.recurring, year)
  return a
}

/** Runs a user rule script (a JS function body receiving `d`) against an aggregate. */
export function runRule(script: string, d: Aggregate): RuleLine[] {
  // The script is the user's own code running in their own browser, so plain eval is acceptable.
  const fn = new Function('d', script) as (d: Aggregate) => unknown
  const out = fn(structuredClone(d))
  if (!Array.isArray(out)) throw new Error('rule must return an array of {label, value} lines')
  return out.map((l, i) => {
    if (!l || typeof l !== 'object' || !('label' in l)) throw new Error(`line ${i} has no label`)
    return l as RuleLine
  })
}
