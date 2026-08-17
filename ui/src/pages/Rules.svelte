<script lang="ts">
  import { pb, uid, loadYear, availableYears, type Rule } from '../lib/pb'
  import { money } from '../lib/format'
  import { aggregate, runRule, type RuleLine } from '../lib/tax'

  let rules = $state<Rule[]>([])
  let sel = $state<Rule | null>(null)
  let name = $state(''), script = $state(''), active = $state(false)
  let years = $state<number[]>([new Date().getFullYear()]), year = $state(new Date().getFullYear())
  let preview = $state<RuleLine[]>([]), previewError = $state(''), error = $state(''), saved = $state('')

  async function load(selectId?: string) {
    rules = await pb.collection<Rule>('rules').getFullList({ sort: '-active,name' })
    pick(rules.find(r => r.id === selectId) ?? rules.find(r => r.active) ?? rules[0] ?? null)
  }
  function pick(r: Rule | null) { sel = r; name = r?.name ?? ''; script = r?.script ?? ''; active = r?.active ?? false; preview = []; saved = '' }
  load(); availableYears().then(y => (years = y))

  function fresh() { pick(null); name = 'New rule'; script = "return [\n  { label: 'Net', value: d.net },\n];" }

  async function save() {
    error = ''
    try {
      let id = sel?.id
      const data = { name, script, active, user: uid() }
      if (sel) await pb.collection('rules').update(sel.id, data)
      else id = (await pb.collection('rules').create(data)).id
      if (active) for (const r of rules) if (r.id !== id && r.active) await pb.collection('rules').update(r.id, { active: false })
      await load(id); saved = 'Saved.'
    } catch (e: any) { error = e?.data?.message ?? e.message }
  }
  async function del() {
    if (!sel || !confirm(`Delete rule "${sel.name}"?`)) return
    await pb.collection('rules').delete(sel.id); await load()
  }
  async function run() {
    previewError = ''
    try { preview = runRule(script, aggregate(year, await loadYear(year))) }
    catch (e: any) { preview = []; previewError = e.message }
  }
</script>

<h1>Tax rules</h1>
<p class="muted small">A rule is the body of a JavaScript function that receives the yearly aggregate <code>d</code>
  (<code>d.income, d.expenses, d.net, d.area.business|rental|private.{'{income,expenses,net}'}, d.category[name], d.tag[name], d.transactions[]</code>)
  and returns an array of <code>{'{ label, value, hint? }'}</code> lines. Numeric values are shown as €. The <b>active</b> rule is used on the overview.</p>

<div class="row" style="align-items:flex-start; gap:1rem">
  <div class="panel" style="min-width:220px">
    {#each rules as r}
      <div><button class="link" style="color:{sel?.id === r.id ? 'var(--fg)' : ''}; font-weight:{sel?.id === r.id ? 600 : 400}" onclick={() => pick(r)}>{r.name}</button>
        {#if r.active}<span class="tag">active</span>{/if}</div>
    {/each}
    <button style="margin-top:.8rem" onclick={fresh}>+ New rule</button>
  </div>

  <div class="panel" style="flex:1; min-width:320px">
    <div class="row" style="margin-bottom:.6rem">
      <label class="field" style="flex:1">Name <input bind:value={name} /></label>
      <label class="row small" style="align-self:flex-end"><input type="checkbox" bind:checked={active} /> active</label>
    </div>
    <textarea class="code" bind:value={script} spellcheck="false"></textarea>
    {#if error}<div class="error small">{error}</div>{/if}
    <div class="row" style="justify-content:space-between; margin-top:.6rem">
      <div class="row">
        <button class="primary" onclick={save}>Save</button>
        {#if sel}<button class="danger" onclick={del}>Delete</button>{/if}
        <span class="muted small">{saved}</span>
      </div>
      <div class="row">
        <select bind:value={year}>{#each years as y}<option value={y}>{y}</option>{/each}</select>
        <button onclick={run}>Run preview</button>
      </div>
    </div>
    {#if previewError}<div class="error small" style="margin-top:.6rem">{previewError}</div>{/if}
    {#if preview.length}
      <table style="margin-top:.8rem"><tbody>
        {#each preview as l}<tr><td>{l.label}{#if l.hint}<div class="muted small">{l.hint}</div>{/if}</td>
          <td class="num">{typeof l.value === 'number' ? money(l.value) : l.value}</td></tr>{/each}
      </tbody></table>
    {/if}
  </div>
</div>
