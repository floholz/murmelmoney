<script lang="ts">
  import { pb, uid, AREAS, fileUrl, fileToken, ensureLabel, categoryName, tagNames, type Transaction, type TxType, type Area, type Category, type Tag } from '../lib/pb'
  import { today, isoDate } from '../lib/format'
  import TagPicker from '../lib/TagPicker.svelte'

  let { tx = null, defaultType = 'expense', categories = [], tags = [], onclose }: {
    tx?: Transaction | null
    defaultType?: TxType
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
  let over = $state(false), busy = $state(false), error = $state('')
  let token = $state('')
  if (existing.length) fileToken().then(t => (token = t)).catch(() => {})

  function addFiles(list: FileList | null) {
    if (list) newFiles = [...newFiles, ...Array.from(list)]
  }

  async function save(e: SubmitEvent) {
    e.preventDefault(); busy = true; error = ''
    try {
      const catId = category.trim() ? (await ensureLabel('categories', category)).id : ''
      const tagIds = await Promise.all(tagList.map(async n => (await ensureLabel('tags', n)).id))
      const fd = new FormData()
      fd.set('user', uid()); fd.set('type', type); fd.set('date', date); fd.set('amount', String(amount))
      fd.set('area', area); fd.set('category', catId); fd.set('note', note)
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
      <h2 style="margin:0">{tx ? 'Edit' : 'New'} transaction</h2>
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
    </div>

    <div class="field" style="margin-top:.8rem"><span class="small muted">Tags</span><TagPicker bind:selected={tagList} options={tags} /></div>

    <label class="field" style="margin-top:.8rem">Note <textarea rows="3" bind:value={note}></textarea></label>

    <div style="margin-top:.8rem">
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
