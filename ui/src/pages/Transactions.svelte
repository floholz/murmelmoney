<script lang="ts">
  import { loadYear, availableYears, loadCategories, loadTags, categoryName, tagNames, AREAS, type Transaction, type TxType, type Area, type Category, type Tag } from '../lib/pb'
  import { signed, isoDate } from '../lib/format'
  import TransactionForm from './TransactionForm.svelte'
  import TransactionDetail from './TransactionDetail.svelte'

  let years = $state<number[]>([new Date().getFullYear()])
  let year = $state(new Date().getFullYear())
  let txs = $state<Transaction[]>([])
  let categories = $state<Category[]>([]), tags = $state<Tag[]>([])
  let type = $state<TxType | ''>(''), area = $state<Area | ''>(''), cat = $state(''), tag = $state(''), q = $state('')
  let editing = $state<Transaction | null | undefined>(undefined) // undefined = closed, null = new
  let viewing = $state<Transaction | null>(null)
  let newType = $state<TxType>('expense')
  let error = $state('')

  async function refresh() {
    try { [txs, categories, tags] = await Promise.all([loadYear(year), loadCategories(), loadTags()]) }
    catch (e: any) { error = e.message }
  }
  $effect(() => { year; refresh() })
  availableYears().then(y => (years = y))

  const shown = $derived(txs.filter(t =>
    (!type || t.type === type) && (!area || t.area === area) &&
    (!cat || t.category === cat) && (!tag || t.tags.includes(tag)) &&
    (!q || (categoryName(t) + ' ' + tagNames(t).join(' ') + ' ' + t.note).toLowerCase().includes(q.toLowerCase()))))
  const sum = $derived(shown.reduce((s, t) => s + (t.type === 'income' ? t.amount : -t.amount), 0))

  function open(t: Transaction | null, tp: TxType = 'expense') { newType = tp; viewing = null; editing = t }
  async function closed(changed: boolean) {
    editing = undefined
    if (changed) { await refresh(); years = await availableYears() }
  }
  async function deleted() { viewing = null; await refresh(); years = await availableYears() }
</script>

<div class="row toolbar" style="justify-content:space-between; margin-bottom:.8rem">
  <div class="row filters" style="flex:1">
    <select bind:value={year}>{#each years as y}<option value={y}>{y}</option>{/each}</select>
    <select bind:value={type}><option value="">all types</option><option value="income">income</option><option value="expense">expense</option></select>
    <select bind:value={area}><option value="">all areas</option>{#each AREAS as a}<option value={a}>{a}</option>{/each}</select>
    <select bind:value={cat}><option value="">all categories</option>{#each categories as c}<option value={c.id}>{c.name}</option>{/each}</select>
    <select bind:value={tag}><option value="">all tags</option>{#each tags as t}<option value={t.id}>{t.name}</option>{/each}</select>
    <input placeholder="search" bind:value={q} style="width:9rem" />
  </div>
  <div class="row actions" style="margin-left:auto">
    <button class="primary" onclick={() => open(null, 'income')}>+ Income</button>
    <button class="primary" onclick={() => open(null, 'expense')}>+ Expense</button>
  </div>
</div>

{#if error}<div class="error">{error}</div>{/if}

<div class="panel table-wrap">
  <table class="tx">
    <thead><tr><th>Date</th><th>Area</th><th>Category</th><th>Tags</th><th>Note</th><th></th><th class="num">Amount</th></tr></thead>
    <tbody>
      {#each shown as t (t.id)}
        <tr class="clickable" onclick={() => (viewing = t)}>
          <td class="num date">{isoDate(t.date)}</td>
          <td class="area"><span class="tag">{t.area}</span></td>
          <td class="category">{categoryName(t)}</td>
          <td class="tags">{#if t.tags.length}<div class="tags-cell">{#each tagNames(t) as n}<span class="tag chip">{n}</span>{/each}</div>{/if}</td>
          <td class="muted small note" style="max-width:20rem; overflow:hidden; text-overflow:ellipsis; white-space:nowrap">{t.note}</td>
          <td class="muted small files">{t.attachments.length ? '📎 ' + t.attachments.length : ''}</td>
          <td class="num amount {t.type}">{signed(t.amount, t.type)}</td>
        </tr>
      {:else}
        <tr><td colspan="7" class="muted">No transactions{txs.length ? ' match the filter' : ' in ' + year}.</td></tr>
      {/each}
    </tbody>
    {#if shown.length}
      <tfoot><tr><th colspan="6">{shown.length} transactions</th><th class="num" class:income={sum >= 0} class:expense={sum < 0}>{signed(sum, sum >= 0 ? 'income' : 'expense')}</th></tr></tfoot>
    {/if}
  </table>
</div>

{#if viewing}
  <TransactionDetail tx={viewing} onclose={() => (viewing = null)} onedit={() => open(viewing)} ondeleted={deleted} />
{/if}
{#if editing !== undefined}
  <TransactionForm tx={editing} defaultType={newType} {categories} {tags} onclose={closed} />
{/if}
