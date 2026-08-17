export type Theme = 'light' | 'dark'
const KEY = 'murmel_theme'

const system = (): Theme => (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
export const current = (): Theme => (localStorage.getItem(KEY) as Theme | null) ?? system()

export function apply(t: Theme | null) {
  if (t) { document.documentElement.dataset.theme = t; localStorage.setItem(KEY, t) }
  else { delete document.documentElement.dataset.theme; localStorage.removeItem(KEY) }
}
export const toggle = () => { const next: Theme = current() === 'dark' ? 'light' : 'dark'; apply(next); return next }

// apply persisted choice as early as possible
apply(localStorage.getItem(KEY) as Theme | null)
