<script lang="ts">
  import { pb, uid, fileUrl, fileToken, type Loan } from '../lib/pb'
  import { isoDate, today } from '../lib/format'

  let { loan = null, onclose }: {
    loan?: Loan | null
    onclose: (changed: boolean, saved?: Loan) => void
  } = $props()

  let name = $state(loan?.name ?? '')
  let principal = $state<number | ''>(loan?.principal ?? '')
  let rate = $state<number | ''>(loan?.interest_rate || '')
  let start = $state(loan?.start ? isoDate(loan.start) : today())
  let note = $state(loan?.note ?? '')
  let closed = $state(loan?.closed ?? false)
  let existing = $state<string[]>(loan?.attachments ?? [])
  let removed = $state<string[]>([])
  let newFiles = $state<File[]>([])
  let over = $state(false), busy = $state(false), error = $state('')
  let token = $state('')
  if (existing.length) fileToken().then(t => (token = t)).catch(() => {})

  function addFiles(list: FileList | null) {
    if (list) newFiles = [...newFiles, ...Array.from(list)]
  }

  async function save(e: SubmitEvent) {
    e.preventDefault(); busy = true; error = ''
    try {
      const fd = new FormData()
      fd.set('user', uid()); fd.set('name', name); fd.set('principal', String(principal))
      fd.set('interest_rate', String(rate || 0)); fd.set('start', start); fd.set('note', note)
      fd.set('closed', closed ? 'true' : 'false')
      for (const f of newFiles) fd.append('attachments', f)
      for (const f of removed) fd.append('attachments-', f)
      const saved = loan
        ? await pb.collection<Loan>('loans').update(loan.id, fd)
        : await pb.collection<Loan>('loans').create(fd)
      onclose(true, saved)
    } catch (err: any) { error = err?.data?.message ?? err?.message ?? String(err) }
    finally { busy = false }
  }

  async function del() {
    if (!loan || !confirm('Delete this loan? Its payments stay as normal transactions.')) return
    busy = true
    try { await pb.collection('loans').delete(loan.id); onclose(true) }
    catch (err: any) { error = err?.message; busy = false }
  }
</script>

<div class="modal-bg" onclick={(e) => e.target === e.currentTarget && onclose(false)} role="presentation">
  <form class="modal" onsubmit={save}>
    <h2 style="margin:0 0 .8rem">{loan ? 'Edit' : 'New'} loan</h2>

    <div class="grid" style="grid-template-columns: repeat(auto-fit, minmax(140px, 1fr))">
      <label class="field">Name <input bind:value={name} required placeholder="e.g. Car loan" /></label>
      <label class="field">Principal (€) <input type="number" step="0.01" min="0" bind:value={principal} required /></label>
      <label class="field">Interest rate (% p.a.) <input type="number" step="0.01" min="0" bind:value={rate} placeholder="0" /></label>
      <label class="field">Start <input type="date" bind:value={start} /></label>
    </div>

    <label class="field" style="margin-top:.8rem">Note <textarea rows="3" bind:value={note}></textarea></label>

    <div style="margin-top:.8rem">
      <div class="small muted" style="margin-bottom:.25rem">Attachments (contract, statements…)</div>
      <label class="dropzone" class:over
        ondragover={(e) => { e.preventDefault(); over = true }}
        ondragleave={() => (over = false)}
        ondrop={(e) => { e.preventDefault(); over = false; addFiles(e.dataTransfer?.files ?? null) }}>
        Drop files here or click to choose
        <input type="file" multiple hidden onchange={(e) => addFiles(e.currentTarget.files)} />
      </label>
      <ul class="files">
        {#each existing as f}
          <li class="row"><a href={token ? fileUrl(loan!, f, token) : undefined} target="_blank" rel="noopener" aria-disabled={!token}>{f}</a>
            <button type="button" class="link danger" onclick={() => { existing = existing.filter(x => x !== f); removed = [...removed, f] }}>remove</button></li>
        {/each}
        {#each newFiles as f, i}
          <li class="row"><span>{f.name} <span class="muted">({(f.size / 1024).toFixed(0)} KB, new)</span></span>
            <button type="button" class="link danger" onclick={() => (newFiles = newFiles.filter((_, j) => j !== i))}>remove</button></li>
        {/each}
      </ul>
    </div>

    <label class="row small" style="margin-top:.8rem"><input type="checkbox" bind:checked={closed} /> closed (archived; hidden in the transaction form)</label>

    {#if error}<div class="error small" style="margin-top:.6rem">{error}</div>{/if}

    <div class="row" style="justify-content:space-between; margin-top:1rem">
      <div>{#if loan}<button type="button" class="danger" onclick={del} disabled={busy}>Delete</button>{/if}</div>
      <div class="row">
        <button type="button" onclick={() => onclose(false)}>Cancel</button>
        <button class="primary" disabled={busy}>Save</button>
      </div>
    </div>
  </form>
</div>
