<script lang="ts">
  import { pb, uid, AREAS, INTERVALS, ensureLabel, categoryName, tagNames, type Recurring, type TxType, type Area, type Interval, type Category, type Tag } from '../lib/pb'
  import { today, isoDate } from '../lib/format'
  import { pendingCount } from '../lib/recurring'
  import TagPicker from '../lib/TagPicker.svelte'

  let { rec = null, defaultType = 'expense', categories = [], tags = [], onclose }: {
    rec?: Recurring | null
    defaultType?: TxType
    categories?: Category[]
    tags?: Tag[]
    onclose: (changed: boolean) => void
  } = $props()

  let type = $state<TxType>(rec?.type ?? defaultType)
  let amount = $state<number | ''>(rec?.amount ?? '')
  let area = $state<Area>(rec?.area ?? 'business')
  let category = $state(rec ? categoryName(rec) : '')
  let tagList = $state<string[]>(rec ? tagNames(rec) : [])
  let note = $state(rec?.note ?? '')
  let interval = $state<Interval>(rec?.interval ?? 'monthly')
  let start = $state(rec ? isoDate(rec.start) : today())
  let end = $state(rec?.end ? isoDate(rec.end) : '')
  let weekdaysOnly = $state(rec?.weekdays_only ?? false)
  let active = $state(rec?.active ?? true)
  let busy = $state(false), error = $state('')

  // How many past occurrences the server will create right after saving.
  const pending = $derived(pendingCount({
    ...(rec ?? {}), interval, start, end, active, weekdays_only: weekdaysOnly,
    last_generated: rec?.last_generated ?? '',
  } as Recurring))

  async function save(e: SubmitEvent) {
    e.preventDefault(); busy = true; error = ''
    try {
      const catId = category.trim() ? (await ensureLabel('categories', category)).id : ''
      const tagIds = await Promise.all(tagList.map(async n => (await ensureLabel('tags', n)).id))
      const data = { user: uid(), type, amount, area, category: catId, tags: tagIds, note, interval, start, end, weekdays_only: weekdaysOnly, active }
      if (rec) await pb.collection('recurring').update(rec.id, data)
      else await pb.collection('recurring').create(data)
      onclose(true)
    } catch (err: any) { error = err?.data?.message ?? err?.message ?? String(err) }
    finally { busy = false }
  }

  async function del() {
    if (!rec || !confirm('Delete this recurring transaction? Already generated transactions are kept.')) return
    busy = true
    try { await pb.collection('recurring').delete(rec.id); onclose(true) }
    catch (err: any) { error = err?.message; busy = false }
  }
</script>

<div class="modal-bg" onclick={(e) => e.target === e.currentTarget && onclose(false)} role="presentation">
  <form class="modal" onsubmit={save}>
    <div class="row" style="justify-content:space-between; margin-bottom:.8rem">
      <h2 style="margin:0">{rec ? 'Edit' : 'New'} recurring transaction</h2>
      <div class="row seg">
        <button type="button" class:on={type === 'income'} onclick={() => (type = 'income')}>Income</button>
        <button type="button" class:on={type === 'expense'} onclick={() => (type = 'expense')}>Expense</button>
      </div>
    </div>

    <div class="grid" style="grid-template-columns: repeat(auto-fit, minmax(140px, 1fr))">
      <label class="field">Amount (€) <input type="number" step="0.01" min="0" bind:value={amount} required /></label>
      <label class="field">Interval
        <select bind:value={interval}>{#each INTERVALS as i}<option value={i}>{i}</option>{/each}</select>
      </label>
      <label class="field">Area
        <select bind:value={area}>{#each AREAS as a}<option value={a}>{a}</option>{/each}</select>
      </label>
      <label class="field">Category <input list="cats" bind:value={category} placeholder="e.g. Rent" /></label>
      <datalist id="cats">{#each categories as c}<option value={c.name}></option>{/each}</datalist>
      <label class="field">First on <input type="date" bind:value={start} required /></label>
      <label class="field">Until (optional) <input type="date" bind:value={end} /></label>
    </div>
    <label class="row small" style="margin-top:.6rem"><input type="checkbox" bind:checked={weekdaysOnly} />
      only on weekdays — a date falling on Sat/Sun shifts to the following Monday (e.g. rent on the first weekday of the month: first date on the 1st + this)</label>
    <p class="muted small" style="margin:.5rem 0 0">Repeats from the first date: monthly/quarterly/yearly keep its
      day of the month (clamped to shorter months), weekly keeps its weekday.
      {#if rec?.last_generated}Moving the first date further back does not create occurrences before {isoDate(rec.last_generated)}.{/if}</p>

    <div class="field" style="margin-top:.8rem"><span class="small muted">Tags</span><TagPicker bind:selected={tagList} options={tags} /></div>

    <label class="field" style="margin-top:.8rem">Note <textarea rows="3" bind:value={note}></textarea></label>

    <label class="row small" style="margin-top:.8rem"><input type="checkbox" bind:checked={active} /> active (paused templates create nothing and are not projected)</label>

    {#if pending > 0}
      <div class="small" style="margin-top:.6rem">⚠ Saving will immediately create <b>{pending}</b> past transaction{pending === 1 ? '' : 's'}.</div>
    {/if}

    {#if error}<div class="error small" style="margin-top:.6rem">{error}</div>{/if}

    <div class="row" style="justify-content:space-between; margin-top:1rem">
      <div>{#if rec}<button type="button" class="danger" onclick={del} disabled={busy}>Delete</button>{/if}</div>
      <div class="row">
        <button type="button" onclick={() => onclose(false)}>Cancel</button>
        <button class="primary" disabled={busy}>Save</button>
      </div>
    </div>
  </form>
</div>
