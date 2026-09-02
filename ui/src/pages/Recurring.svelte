<script lang="ts">
  import { loadRecurring, loadCategories, loadTags, categoryName, tagNames, type Recurring, type TxType, type Category, type Tag } from '../lib/pb'
  import { signed, isoDate } from '../lib/format'
  import { nextOccurrence, intervalLabel } from '../lib/recurring'
  import RecurringForm from './RecurringForm.svelte'

  let templates = $state<Recurring[]>([])
  let categories = $state<Category[]>([]), tags = $state<Tag[]>([])
  let editing = $state<Recurring | null | undefined>(undefined) // undefined = closed, null = new
  let newType = $state<TxType>('expense')
  let error = $state('')

  async function refresh() {
    try { [templates, categories, tags] = await Promise.all([loadRecurring(), loadCategories(), loadTags()]) }
    catch (e: any) { error = e.message }
  }
  refresh()

  function open(t: Recurring | null, tp: TxType = 'expense') { newType = tp; editing = t }
  async function closed(changed: boolean) {
    editing = undefined
    if (changed) await refresh()
  }
  const ended = (t: Recurring) => !!t.end && t.end.slice(0, 10) < new Date().toISOString().slice(0, 10)
</script>

<div class="row toolbar" style="justify-content:space-between; margin-bottom:.8rem">
  <h1 style="margin:0">Recurring</h1>
  <div class="row actions" style="margin-left:auto">
    <button class="primary" onclick={() => open(null, 'income')}>+ Income</button>
    <button class="primary" onclick={() => open(null, 'expense')}>+ Expense</button>
  </div>
</div>
<p class="muted small">Templates that automatically create a transaction every interval (rent, insurance, salary…).
  Changes only affect future occurrences; already created transactions stay as they are.</p>

{#if error}<div class="error">{error}</div>{/if}

<div class="panel table-wrap">
  <table class="tx">
    <thead><tr><th>Interval</th><th>Area</th><th>Category</th><th>Tags</th><th>Note</th><th>Next</th><th class="num">Amount</th></tr></thead>
    <tbody>
      {#each templates as t (t.id)}
        <tr class="clickable" onclick={() => open(t)} style={t.active ? '' : 'opacity:.55'}>
          <td class="date">{intervalLabel(t.interval)} since {isoDate(t.start)}{#if t.weekdays_only}<span class="tag" title="Sat/Sun shift to Monday">weekdays</span>{/if}
            {#if !t.active}<span class="tag">paused</span>{:else if ended(t)}<span class="tag">ended {isoDate(t.end)}</span>{:else if t.end}<span class="muted small">until {isoDate(t.end)}</span>{/if}</td>
          <td class="area"><span class="tag">{t.area}</span></td>
          <td class="category">{categoryName(t)}</td>
          <td class="tags">{#if t.tags.length}<div class="tags-cell">{#each tagNames(t) as n}<span class="tag chip">{n}</span>{/each}</div>{/if}</td>
          <td class="muted small note" style="max-width:20rem; overflow:hidden; text-overflow:ellipsis; white-space:nowrap">{t.note}</td>
          <td class="muted small files">{nextOccurrence(t) ? 'next ' + nextOccurrence(t) : ''}</td>
          <td class="num amount {t.type}">{signed(t.amount, t.type)}</td>
        </tr>
      {:else}
        <tr><td colspan="7" class="muted">No recurring transactions yet.</td></tr>
      {/each}
    </tbody>
  </table>
</div>

{#if editing !== undefined}
  <RecurringForm rec={editing} defaultType={newType} {categories} {tags} onclose={closed} />
{/if}
