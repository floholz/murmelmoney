<script lang="ts">
  import { fileUrl, fileToken, loadCategories, loadTags, type Loan, type Transaction, type TxType, type Category, type Tag } from '../lib/pb'
  import { money, isoDate } from '../lib/format'
  import type { LoanStats } from '../lib/loans'
  import TransactionForm from './TransactionForm.svelte'

  let { loan, stats, payments, onclose, onedit, onchanged }: {
    loan: Loan
    stats: LoanStats
    payments: Transaction[]
    onclose: () => void
    onedit: () => void
    onchanged: () => void
  } = $props()

  let token = $state(''), error = $state('')
  let paying = $state(false)
  let categories = $state<Category[]>([]), tags = $state<Tag[]>([])
  if (loan.attachments.length) fileToken().then(t => (token = t)).catch(e => (error = e.message))
  loadCategories().then(c => (categories = c)).catch(() => {})
  loadTags().then(t => (tags = t)).catch(() => {})

  const isImage = (f: string) => /\.(png|jpe?g|gif|webp|avif|svg)$/i.test(f)
  const ext = (f: string) => (f.match(/\.([a-z0-9]+)$/i)?.[1] ?? 'file').toUpperCase()

  function paymentClosed(changed: boolean) {
    paying = false
    if (changed) onchanged()
  }
</script>

<div class="modal-bg" onclick={(e) => e.target === e.currentTarget && onclose()} role="presentation">
  <div class="modal detail">
    <div class="row" style="justify-content:space-between; align-items:flex-start; margin-bottom:.8rem">
      <div>
        <div class="muted small">
          {#if loan.start}since {isoDate(loan.start)} · {/if}{loan.interest_rate ? loan.interest_rate + '% p.a.' : 'interest-free'}
          {#if loan.closed}· <span class="tag">closed</span>{/if}
        </div>
        <div style="font-weight:600; font-size:1.1rem">{loan.name}</div>
        <div class="amount">{money(stats.remaining)} <span class="muted small" style="font-weight:400">of {money(loan.principal)} remaining</span></div>
      </div>
      <button type="button" class="link" onclick={onclose} aria-label="Close" style="font-size:1.4rem; line-height:1">×</button>
    </div>

    <div class="progress" style="margin-bottom:.4rem"><div style="width:{stats.progress * 100}%"></div></div>
    <div class="muted small" style="margin-bottom:.8rem">
      {money(stats.principalPaid)} repaid{#if stats.interestPaid} + {money(stats.interestPaid)} interest paid{/if}
    </div>

    {#if loan.note}
      <h3>Note</h3>
      <p class="note-text">{loan.note}</p>
    {/if}

    {#if loan.attachments.length}
      <h3>Attachments <span class="muted">({loan.attachments.length})</span></h3>
      {#if !token && !error}
        <p class="muted small">Loading…</p>
      {:else}
        <div class="attachments">
          {#each loan.attachments as f}
            {@const url = fileUrl(loan, f, token)}
            <a class="attachment" href={url} target="_blank" rel="noopener" title={f}>
              {#if isImage(f)}<img src={url} alt={f} loading="lazy" />{:else}<div class="filebox">{ext(f)}</div>{/if}
              <span class="small">{f}</span>
            </a>
          {/each}
        </div>
      {/if}
    {/if}

    <h3>Payments {#if payments.length}<span class="muted">({payments.length})</span>{/if}</h3>
    {#if !payments.length}
      <p class="muted small">None yet.</p>
    {:else}
      <table>
        <thead><tr><th>Date</th><th class="num">Amount</th><th class="num">Interest</th></tr></thead>
        <tbody>
          {#each payments as p (p.id)}
            <tr><td class="num" style="text-align:left">{isoDate(p.date)}</td>
              <td class="num {p.type}">{money(p.amount)}</td>
              <td class="num muted">{p.loan_interest ? money(p.loan_interest) : ''}</td></tr>
          {/each}
        </tbody>
      </table>
    {/if}

    {#if error}<div class="error small" style="margin-top:.6rem">{error}</div>{/if}

    <div class="row" style="justify-content:space-between; margin-top:1.2rem">
      <button type="button" class="primary" onclick={() => (paying = true)}>+ Payment</button>
      <div class="row">
        <button type="button" onclick={onclose}>Close</button>
        <button type="button" onclick={onedit}>Edit</button>
      </div>
    </div>
  </div>
</div>

{#if paying}
  <TransactionForm tx={null} defaultType={'expense' as TxType} defaultLoan={loan.id} {categories} {tags} onclose={paymentClosed} />
{/if}
