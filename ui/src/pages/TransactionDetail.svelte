<script lang="ts">
  import { pb, fileUrl, fileToken, categoryName, tagNames, type Transaction } from '../lib/pb'
  import { money, isoDate } from '../lib/format'

  let { tx, onclose, onedit, ondeleted }: {
    tx: Transaction
    onclose: () => void
    onedit: () => void
    ondeleted: () => void
  } = $props()

  let token = $state(''), error = $state(''), busy = $state(false)
  if (tx.attachments.length) fileToken().then(t => (token = t)).catch(e => (error = e.message))

  const isImage = (f: string) => /\.(png|jpe?g|gif|webp|avif|svg)$/i.test(f)
  const ext = (f: string) => (f.match(/\.([a-z0-9]+)$/i)?.[1] ?? 'file').toUpperCase()

  async function del() {
    if (!confirm('Delete this transaction?')) return
    busy = true
    try { await pb.collection('transactions').delete(tx.id); ondeleted() }
    catch (e: any) { error = e.message; busy = false }
  }
</script>

<div class="modal-bg" onclick={(e) => e.target === e.currentTarget && onclose()} role="presentation">
  <div class="modal detail">
    <div class="row" style="justify-content:space-between; align-items:flex-start; margin-bottom:.8rem">
      <div>
        <div class="muted small">{isoDate(tx.date)} · <span class="tag">{tx.area}</span>
          {#if tx.recurring}<span class="tag">recurring</span>{/if}</div>
        <div class="amount {tx.type}">{tx.type === 'expense' ? '−' : '+'}{money(tx.amount)}</div>
        {#if categoryName(tx)}<div style="font-weight:600">{categoryName(tx)}</div>{/if}
        {#if tx.loan}<div class="muted small">Loan payment{#if tx.expand?.loan} — {tx.expand.loan.name}{/if}{#if tx.loan_interest} · {money(tx.loan_interest)} interest{/if}</div>{/if}
      </div>
      <button type="button" class="link" onclick={onclose} aria-label="Close" style="font-size:1.4rem; line-height:1">×</button>
    </div>

    {#if tx.tags.length}
      <div class="tags-cell" style="margin-bottom:.8rem">{#each tagNames(tx) as n}<span class="tag chip">{n}</span>{/each}</div>
    {/if}

    {#if tx.note}
      <h3>Note</h3>
      <p class="note-text">{tx.note}</p>
    {/if}

    <h3>Attachments {#if tx.attachments.length}<span class="muted">({tx.attachments.length})</span>{/if}</h3>
    {#if !tx.attachments.length}
      <p class="muted small">None.</p>
    {:else if !token && !error}
      <p class="muted small">Loading…</p>
    {:else}
      <div class="attachments">
        {#each tx.attachments as f}
          {@const url = fileUrl(tx, f, token)}
          <a class="attachment" href={url} target="_blank" rel="noopener" title={f}>
            {#if isImage(f)}
              <img src={url} alt={f} loading="lazy" />
            {:else}
              <div class="filebox">{ext(f)}</div>
            {/if}
            <span class="small">{f}</span>
          </a>
        {/each}
      </div>
    {/if}

    {#if error}<div class="error small" style="margin-top:.6rem">{error}</div>{/if}

    <div class="row" style="justify-content:space-between; margin-top:1.2rem">
      <button type="button" class="danger" onclick={del} disabled={busy}>Delete</button>
      <div class="row">
        <button type="button" onclick={onclose}>Close</button>
        <button type="button" class="primary" onclick={onedit}>Edit</button>
      </div>
    </div>
  </div>
</div>
