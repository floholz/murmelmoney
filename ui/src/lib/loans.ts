import type { Loan, Transaction } from './pb'

export interface LoanStats { paid: number; principalPaid: number; interestPaid: number; remaining: number; progress: number }

/** Balance math over a loan's linked payment transactions. The interest portion
 *  of a payment doesn't reduce the principal. */
export function loanStats(l: Loan, payments: Transaction[]): LoanStats {
  const ps = payments.filter(p => p.loan === l.id)
  // sign-defensive: an income linked to a loan would increase the balance
  const paid = ps.reduce((s, p) => s + (p.type === 'expense' ? p.amount : -p.amount), 0)
  const interestPaid = ps.reduce((s, p) => s + (p.loan_interest || 0), 0)
  const principalPaid = paid - interestPaid
  const remaining = Math.max(0, l.principal - principalPaid)
  return { paid, principalPaid, interestPaid, remaining, progress: l.principal ? Math.min(1, Math.max(0, principalPaid / l.principal)) : 1 }
}
