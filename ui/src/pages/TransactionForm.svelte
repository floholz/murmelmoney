<script lang="ts">
  import { pb, uid, AREAS, INTERVALS, fileUrl, fileToken, ensureLabel, categoryName, tagNames, loadLoans, loadLoanPayments, type Transaction, type TxType, type Area, type Interval, type Category, type Tag, type Loan, type Recurring } from '../lib/pb'
  import { today, isoDate, money } from '../lib/format'
  import { pendingCount } from '../lib/recurring'
  import TagPicker from '../lib/TagPicker.svelte'
  import LoanForm from './LoanForm.svelte'

  let { tx = null, defaultType = 'expense', defaultLoan = '', categories = [], tags = [], onclose }: {
    tx?: Transaction | null
    defaultType?: TxType
    defaultLoan?: string
    categories?: Category[]
    tags?: Tag[]
    onclose: (changed: boolean) => void
  } = $props()

  let type = $state<TxType>(tx?.type ?? defaultType)
  let date = $state(tx ? isoDate(tx.date) : today())
  let amount = $state<number | ''>(tx?.amount ?? '')
  let area = $state<Area>(tx?.area ?? 'business')
  let category = $state(tx ? categoryName(tx) : '')
  let tagList = $state<string[]>(tx ? tagNames(tx) : [])
  let note = $state(tx?.note ?? '')
  let existing = $state<string[]>(tx?.attachments ?? [])
  let removed = $state<string[]>([])
  let newFiles = $state<File[]>([])
  let loan = $state(tx?.loan ?? defaultLoan)
  let loanInterest = $state<number | ''>(tx?.loan_interest || '')
  let loans = $state<Loan[]>([])
  let interestHint = $state('')
  let repeat = $state<Interval | ''>('')  // '' = one-off; otherwise saves a recurring template
  let repeatEnd = $state('')
  let repeatWeekdays = $state(false)
  let newLoan = $state(false)
  let over = $state(false), busy = $state(false), error = $state('')
  let token = $state('')
  if (existing.length) fileToken().then(t => (token = t)).catch(() => {})
  loadLoans().then(l => (loans = l.filter(x => !x.closed || x.id === loan))).catch(() => {})
  $effect(() => { if (loan === '__new') { loan = ''; newLoan = true } })

  // Backfill preview when saving as recurring (template starts at the chosen date).
  const pending = $derived(!tx && repeat ? pendingCount({
    interval: repeat, start: date, end: repeatEnd, weekdays_only: repeatWeekdays, active: true, last_generated: '',
  } as Recurring) : 0)

  // Suggest an interest portion of ≈ remaining balance × annual rate / 12.
  $effect(() => {
    interestHint = ''
    const l = loans.find(x => x.id === loan)
    if (!l || !l.interest_rate) return
    loadLoanPayments(l.id).then(ps => {
      const repaid = ps.filter(p => p.id !== tx?.id)
        .reduce((s, p) => s + (p.type === 'expense' ? 1 : -1) * (p.amount - (p.loan_interest || 0)), 0)
      const remaining = l.principal - repaid
      if (remaining > 0) interestHint = `suggested ≈ ${money(remaining)} × ${l.interest_rate}% / 12 = ${money(remaining * l.interest_rate / 100 / 12)}`
    }).catch(() => {})
  })

  function addFiles(list: FileList | null) {
    if (list) newFiles = [...newFiles, ...Array.from(list)]
  }

  async function save(e: SubmitEvent) {
    e.preventDefault(); busy = true; error = ''
    try {
      if (loan && loanInterest !== '' && Number(loanInterest) > Number(amount)) {
        throw new Error('Interest portion cannot exceed the amount.')
      }
      const catId = category.trim() ? (await ensureLabel('categories', category)).id : ''
      const tagIds = await Promise.all(tagList.map(async n => (await ensureLabel('tags', n)).id))
      if (!tx && repeat) {
        // Saves a recurring template instead; the server backfills occurrences
        // from the chosen date up to today right away.
        await pb.collection('recurring').create({
          user: uid(), type, amount, area, category: catId, tags: tagIds, note,
          interval: repeat, start: date, end: repeatEnd, weekdays_only: repeatWeekdays, active: true,
        })
        onclose(true)
        return
      }
      const fd = new FormData()
      fd.set('user', uid()); fd.set('type', type); fd.set('date', date); fd.set('amount', String(amount))
      fd.set('area', area); fd.set('category', catId); fd.set('note', note)
      fd.set('loan', type === 'expense' ? loan : '')
      fd.set('loan_interest', type === 'expense' && loan && loanInterest !== '' ? String(loanInterest) : '0')
      if (tagIds.length) for (const id of tagIds) fd.append('tags', id); else fd.set('tags', '')
      for (const f of newFiles) fd.append('attachments', f)
      for (const f of removed) fd.append('attachments-', f)
      if (tx) await pb.collection('transactions').update(tx.id, fd)
      else await pb.collection('transactions').create(fd)
      onclose(true)
    } catch (err: any) { error = err?.data?.message ?? err?.message ?? String(err) }
    finally { busy = false }
  }

  async function del() {
    if (!tx || !confirm('Delete this transaction?')) return
    busy = true
    try { await pb.collection('transactions').delete(tx.id); onclose(true) }
    catch (err: any) { error = err?.message; busy = false }
  }
</script>

<div class="modal-bg" onclick={(e) => e.target === e.currentTarget && onclose(false)} role="presentation">
  <form class="modal" onsubmit={save}>
    <div class="row" style="justify-content:space-between; margin-bottom:.8rem">
      <h2 style="margin:0">{tx ? 'Edit' : 'New'} {repeat ? 'recurring ' : ''}transaction</h2>
      <div class="row seg">
        <button type="button" class:on={type === 'income'} onclick={() => (type = 'income')}>Income</button>
        <button type="button" class:on={type === 'expense'} onclick={() => (type = 'expense')}>Expense</button>
      </div>
    </div>

    <div class="grid" style="grid-template-columns: repeat(auto-fit, minmax(140px, 1fr))">
      <label class="field">Date <input type="date" bind:value={date} required /></label>
      <label class="field">Amount (€) <input type="number" step="0.01" min="0" bind:value={amount} required /></label>
      <label class="field">Area
        <select bind:value={area}>{#each AREAS as a}<option value={a}>{a}</option>{/each}</select>
      </label>
      <label class="field">Category <input list="cats" bind:value={category} placeholder="e.g. Honorarnote" /></label>
      <datalist id="cats">{#each categories as c}<option value={c.name}></option>{/each}</datalist>
      {#if !tx}
        <label class="field">Repeats
          <select bind:value={repeat}><option value="">never (one-off)</option>{#each INTERVALS as i}<option value={i}>{i}</option>{/each}</select>
        </label>
        {#if repeat}
          <label class="field">Until (optional) <input type="date" bind:value={repeatEnd} /></label>
        {/if}
      {/if}
    </div>
    {#if repeat}
      <label class="row small" style="margin-top:.6rem"><input type="checkbox" bind:checked={repeatWeekdays} />
        only on weekdays — Sat/Sun dates shift to the following Monday</label>
      <p class="muted small" style="margin:.4rem 0 0">Saved as a recurring template (editable under
        <a href="#/recurring">Recurring</a>), repeating from the date above.
        {#if pending > 0}⚠ Saving will immediately create <b>{pending}</b> transaction{pending === 1 ? '' : 's'} up to today.{/if}</p>
    {/if}

    {#if type === 'expense' && !repeat}
      <div class="grid" style="grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); margin-top:.8rem">
        <label class="field">Loan payment
          <select bind:value={loan}><option value="">(no loan)</option>{#each loans as l}<option value={l.id}>{l.name}</option>{/each}<option value="__new">+ new loan…</option></select>
        </label>
        {#if loan}
          <label class="field">Interest portion (€) <input type="number" step="0.01" min="0" bind:value={loanInterest} /></label>
        {/if}
      </div>
      {#if loan}
        <p class="muted small" style="margin:.3rem 0 0">The interest part doesn't reduce the loan balance.{#if interestHint}&nbsp;{interestHint}{/if}</p>
      {/if}
    {/if}

    <div class="field" style="margin-top:.8rem"><span class="small muted">Tags</span><TagPicker bind:selected={tagList} options={tags} /></div>

    <label class="field" style="margin-top:.8rem">Note <textarea rows="3" bind:value={note}></textarea></label>

    <div style="margin-top:.8rem" hidden={!!repeat}>
      <div class="small muted" style="margin-bottom:.25rem">Attachments</div>
      <label class="dropzone" class:over
        ondragover={(e) => { e.preventDefault(); over = true }}
        ondragleave={() => (over = false)}
        ondrop={(e) => { e.preventDefault(); over = false; addFiles(e.dataTransfer?.files ?? null) }}>
        Drop files here or click to choose
        <input type="file" multiple hidden onchange={(e) => addFiles(e.currentTarget.files)} />
      </label>
      <ul class="files">
        {#each existing as f}
          <li class="row"><a href={token ? fileUrl(tx!, f, token) : undefined} target="_blank" rel="noopener" aria-disabled={!token}>{f}</a>
            <button type="button" class="link danger" onclick={() => { existing = existing.filter(x => x !== f); removed = [...removed, f] }}>remove</button></li>
        {/each}
        {#each newFiles as f, i}
          <li class="row"><span>{f.name} <span class="muted">({(f.size / 1024).toFixed(0)} KB, new)</span></span>
            <button type="button" class="link danger" onclick={() => (newFiles = newFiles.filter((_, j) => j !== i))}>remove</button></li>
        {/each}
      </ul>
    </div>

    {#if error}<div class="error small" style="margin-top:.6rem">{error}</div>{/if}

    <div class="row" style="justify-content:space-between; margin-top:1rem">
      <div>{#if tx}<button type="button" class="danger" onclick={del} disabled={busy}>Delete</button>{/if}</div>
      <div class="row">
        <button type="button" onclick={() => onclose(false)}>Cancel</button>
        <button class="primary" disabled={busy}>Save</button>
      </div>
    </div>
  </form>
</div>

{#if newLoan}
  <LoanForm loan={null} onclose={(changed, l) => { newLoan = false; if (changed && l) { loans = [...loans, l]; loan = l.id } }} />
{/if}
