<script lang="ts">
  import { pb, loadYear, loadRecurring, loadLoans, loadLoanPayments, availableYears, AREAS, type Transaction, type Recurring, type Rule, type Loan } from '../lib/pb'
  import { money } from '../lib/format'
  import { aggregate, runRule, type Bucket, type RuleLine } from '../lib/tax'
  import { loanStats } from '../lib/loans'

  let years = $state<number[]>([new Date().getFullYear()])
  let year = $state(new Date().getFullYear())
  let txs = $state<Transaction[]>([])
  let recurring = $state<Recurring[]>([])
  let rule = $state<Rule | null>(null)
  let error = $state('')

  $effect(() => { year; loadYear(year).then(t => (txs = t)).catch(e => (error = e.message)) })
  availableYears().then(y => (years = y))
  loadRecurring().then(r => (recurring = r)).catch(() => {})
  let loans = $state<Loan[]>([]), loanPayments = $state<Transaction[]>([])
  Promise.all([loadLoans(), loadLoanPayments()]).then(([l, p]) => { loans = l; loanPayments = p }).catch(() => {})
  const openLoans = $derived(loans.filter(l => !l.closed).map(l => ({ loan: l, s: loanStats(l, loanPayments) })))
  const totalDebt = $derived(openLoans.reduce((sum, x) => sum + x.s.remaining, 0))
  pb.collection<Rule>('rules').getFirstListItem('active = true').then(r => (rule = r)).catch(() => (rule = null))

  const agg = $derived(aggregate(year, txs, { recurring }))
  const proj = $derived(agg.projected && (agg.projected.income || agg.projected.expenses) ? agg.projected : null)
  const byNet = (o: Record<string, Bucket>) => Object.entries(o).sort((a, b) => a[1].net - b[1].net)

  let lines = $state<RuleLine[]>([]), ruleError = $state('')
  $effect(() => {
    ruleError = ''; lines = []
    if (!rule) return
    try { lines = runRule(rule.script, agg) } catch (e: any) { ruleError = e.message }
  })
</script>

{#snippet bucketTable(title: string, rows: [string, Bucket][], empty: string)}
  <div class="panel table-wrap">
    <h3>{title}</h3>
    <table>
      <thead><tr><th>{title.replace('By ', '')}</th><th class="num">Income</th><th class="num">Expenses</th><th class="num">Net</th></tr></thead>
      <tbody>{#each rows as [name, b]}
        <tr><td>{name}</td><td class="num income">{money(b.income)}</td><td class="num expense">{money(b.expenses)}</td><td class="num">{money(b.net)}</td></tr>
      {:else}<tr><td colspan="4" class="muted">{empty}</td></tr>{/each}</tbody>
    </table>
  </div>
{/snippet}

<div class="row" style="justify-content:space-between; margin-bottom:.8rem">
  <h1>Overview <select bind:value={year}>{#each years as y}<option value={y}>{y}</option>{/each}</select></h1>
</div>
{#if error}<div class="error">{error}</div>{/if}

<div class="grid" style="margin-bottom:1rem">
  <div class="tile"><div class="label">Income</div><div class="value income">{money(agg.income)}</div></div>
  <div class="tile"><div class="label">Expenses</div><div class="value expense">{money(agg.expenses)}</div></div>
  <div class="tile"><div class="label">Net</div><div class="value">{money(agg.net)}</div></div>
</div>

{#if proj}
  <div class="grid" style="margin-bottom:1rem">
    <div class="tile"><div class="label">Projected income</div><div class="value income">{money(agg.income + proj.income)}</div>
      <div class="muted small">of which planned: {money(proj.income)}</div></div>
    <div class="tile"><div class="label">Projected expenses</div><div class="value expense">{money(agg.expenses + proj.expenses)}</div>
      <div class="muted small">of which planned: {money(proj.expenses)}</div></div>
    <div class="tile"><div class="label">Projected net</div><div class="value">{money(agg.net + proj.net)}</div>
      <div class="muted small">year end, incl. <a href="#/recurring">recurring</a></div></div>
  </div>
{/if}

{@render bucketTable('By area', AREAS.map(a => [a, agg.area[a]]), '')}

{#if openLoans.length}
  <div class="panel table-wrap">
    <h3>Loans <span class="muted" style="text-transform:none; letter-spacing:0">— current state, not bound to {year}</span></h3>
    <table>
      <thead><tr><th>Loan</th><th class="num">Repaid</th><th class="num">Interest paid</th><th class="num">Remaining</th><th style="width:9rem">Progress</th></tr></thead>
      <tbody>
        {#each openLoans as { loan: l, s } (l.id)}
          <tr><td>{l.name} <span class="muted small">{l.interest_rate ? l.interest_rate + '% p.a.' : ''}</span></td>
            <td class="num income">{money(s.principalPaid)}</td>
            <td class="num expense">{money(s.interestPaid)}</td>
            <td class="num">{money(s.remaining)}</td>
            <td><div class="progress"><div style="width:{s.progress * 100}%"></div></div></td></tr>
        {/each}
      </tbody>
      <tfoot><tr><th>Total debt</th><th></th><th></th><th class="num">{money(totalDebt)}</th><th></th></tr></tfoot>
    </table>
    <p class="muted small" style="margin:.6rem 0 0"><a href="#/loans">Manage loans</a></p>
  </div>
{/if}

<div class="panel table-wrap">
  <h3>Tax estimate {#if rule}<span class="muted" style="text-transform:none; letter-spacing:0">— {rule.name}</span>{/if}</h3>
  {#if !rule}
    <p class="muted small">No active rule. <a href="#/rules">Create one</a>.</p>
  {:else if ruleError}
    <div class="error small">Rule error: {ruleError}</div>
  {:else}
    <table><tbody>
      {#each lines as l}
        <tr><td>{l.label}{#if l.hint}<div class="muted small">{l.hint}</div>{/if}</td>
          <td class="num val">{typeof l.value === 'number' ? money(l.value) : l.value}</td></tr>
      {/each}
    </tbody></table>
    <p class="muted small" style="margin:.6rem 0 0">Rough estimate only — see <a href="#/rules">Rules</a> to adjust the assumptions.</p>
  {/if}
</div>

{@render bucketTable('By category', byNet(agg.category), 'Nothing yet.')}
{@render bucketTable('By tag', byNet(agg.tag), 'No tagged transactions.')}
