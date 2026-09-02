import PocketBase, { type RecordModel } from 'pocketbase'

export const pb = new PocketBase(window.location.origin)
pb.autoCancellation(false)

export const uid = () => pb.authStore.record?.id ?? ''

export type TxType = 'income' | 'expense'
export type Area = 'business' | 'rental' | 'private'
export const AREAS: Area[] = ['business', 'rental', 'private']

export interface Category extends RecordModel { name: string }
export interface Tag extends RecordModel { name: string }

export interface Transaction extends RecordModel {
  type: TxType
  date: string
  amount: number
  area: Area
  category: string        // category id ('' if none)
  tags: string[]          // tag ids
  note: string
  attachments: string[]
  recurring: string       // id of the generating template ('' if entered by hand)
  loan: string            // loan id ('' if not a loan payment)
  loan_interest: number   // interest portion of a loan payment (doesn't reduce principal)
  expand?: { category?: Category; tags?: Tag[]; loan?: Loan }
}

/** Interval syntax: a preset name or "<n> weeks|months|years" — see lib/recurring.ts. */
export type Interval = string

export interface Recurring extends RecordModel {
  type: TxType
  amount: number
  area: Area
  category: string
  tags: string[]
  note: string
  interval: Interval
  start: string
  end: string             // '' = open-ended
  weekdays_only: boolean  // shift Sat/Sun occurrences to the following Monday
  active: boolean
  last_generated: string  // server-maintained watermark ('' before first run)
  expand?: { category?: Category; tags?: Tag[] }
}

export interface Loan extends RecordModel {
  name: string
  principal: number
  interest_rate: number   // annual %, 0 = interest-free
  start: string
  note: string
  attachments: string[]
  closed: boolean
}

export interface Rule extends RecordModel {
  name: string
  script: string
  active: boolean
}

/** Attachments are protected: URLs need a short-lived file token of the owning user. */
export const fileUrl = (rec: RecordModel, name: string, token: string) => pb.files.getURL(rec, name, { token })
export const fileToken = () => pb.files.getToken()

export interface Status { registration: boolean; version: string }
export const status = (): Promise<Status> => pb.send('/api/murmel/status', { method: 'GET' })
export const categoryName = (t: { expand?: { category?: Category } }) => t.expand?.category?.name ?? ''
export const tagNames = (t: { expand?: { tags?: Tag[] } }) => t.expand?.tags?.map(x => x.name) ?? []

/** All transactions of a year (of the current user), newest first, with category/tags expanded. */
export const loadYear = (year: number) =>
  pb.collection<Transaction>('transactions').getFullList({
    filter: pb.filter('date >= {:from} && date < {:to}', { from: `${year}-01-01 00:00:00`, to: `${year + 1}-01-01 00:00:00` }),
    sort: '-date,-created',
    expand: 'category,tags,loan',
  })

export const loadCategories = () => pb.collection<Category>('categories').getFullList({ sort: 'name' })
export const loadTags = () => pb.collection<Tag>('tags').getFullList({ sort: 'name' })

export const loadRecurring = () =>
  pb.collection<Recurring>('recurring').getFullList({ sort: '-active,start', expand: 'category,tags' })

export const loadLoans = () => pb.collection<Loan>('loans').getFullList({ sort: 'closed,name' })

/** All loan payments (optionally of one loan), across all years, newest first. */
export const loadLoanPayments = (loanId?: string) =>
  pb.collection<Transaction>('transactions').getFullList({
    filter: loanId ? pb.filter('loan = {:id}', { id: loanId }) : "loan != ''",
    sort: '-date,-created',
  })

/** Find-or-create a category/tag by name for the current user. */
export async function ensureLabel(col: 'categories' | 'tags', name: string): Promise<RecordModel> {
  name = name.trim()
  try { return await pb.collection(col).getFirstListItem(pb.filter('name = {:name}', { name })) }
  catch { return await pb.collection(col).create({ name, user: uid() }) }
}

/** Years from the oldest transaction up to max(current year, newest transaction). */
export async function availableYears(): Promise<number[]> {
  const now = new Date().getFullYear()
  const [oldest, newest] = await Promise.all([
    pb.collection<Transaction>('transactions').getList(1, 1, { sort: 'date' }),
    pb.collection<Transaction>('transactions').getList(1, 1, { sort: '-date' }),
  ])
  const from = oldest.items[0] ? new Date(oldest.items[0].date).getFullYear() : now
  const to = Math.max(now, newest.items[0] ? new Date(newest.items[0].date).getFullYear() : now)
  return Array.from({ length: to - from + 1 }, (_, i) => to - i)
}
