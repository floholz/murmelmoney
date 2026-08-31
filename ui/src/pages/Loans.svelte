<script lang="ts">
  import { loadLoans, loadLoanPayments, type Loan, type Transaction } from '../lib/pb'
  import { money } from '../lib/format'
  import { loanStats } from '../lib/loans'
  import LoanForm from './LoanForm.svelte'
  import LoanDetail from './LoanDetail.svelte'

  let loans = $state<Loan[]>([])
  let payments = $state<Transaction[]>([])
  let editing = $state<Loan | null | undefined>(undefined) // undefined = closed, null = new
  let viewing = $state<Loan | null>(null)
  let error = $state('')

  async function refresh() {
    try { [loans, payments] = await Promise.all([loadLoans(), loadLoanPayments()]) }
    catch (e: any) { error = e.message }
  }
  refresh()

  const stats = (l: Loan) => loanStats(l, payments)

  async function closedForm(changed: boolean) {
    editing = undefined
    if (changed) { viewing = null; await refresh() }
  }
</script>

<div class="row toolbar" style="justify-content:space-between; margin-bottom:.8rem">
  <h1 style="margin:0">Loans</h1>
  <div class="row actions" style="margin-left:auto">
    <button class="primary" onclick={() => (editing = null)}>+ Loan</button>
  </div>
</div>
<p class="muted small">Loans aren't bound to a year. Payments are normal expense transactions linked to the loan
  (via the transaction form); the part marked as interest doesn't reduce the balance.</p>

{#if error}<div class="error">{error}</div>{/if}

<div class="panel table-wrap">
  <table class="tx">
    <thead><tr><th>Loan</th><th class="num">Principal</th><th class="num">Repaid</th><th class="num">Interest paid</th><th class="num">Remaining</th><th style="width:9rem">Progress</th></tr></thead>
    <tbody>
      {#each loans as l (l.id)}
        {@const s = stats(l)}
        <tr class="clickable" onclick={() => (viewing = l)} style={l.closed ? 'opacity:.55' : ''}>
          <td class="category">{l.name} {#if l.closed}<span class="tag">closed</span>{:else if s.remaining === 0 && l.principal}<span class="tag">paid off</span>{/if}</td>
          <td class="num date">{money(l.principal)}</td>
          <td class="num area income">{money(s.principalPaid)}</td>
          <td class="num tags expense">{money(s.interestPaid)}</td>
          <td class="num note" style="font-weight:600">{money(s.remaining)}</td>
          <td class="files"><div class="progress"><div style="width:{s.progress * 100}%"></div></div></td>
        </tr>
      {:else}
        <tr><td colspan="6" class="muted">No loans yet.</td></tr>
      {/each}
    </tbody>
  </table>
</div>

{#if viewing}
  <LoanDetail loan={viewing} stats={stats(viewing)} payments={payments.filter(p => p.loan === viewing!.id)}
    onclose={() => (viewing = null)} onedit={() => (editing = viewing)} onchanged={refresh} />
{/if}
{#if editing !== undefined}
  <LoanForm loan={editing} onclose={closedForm} />
{/if}
