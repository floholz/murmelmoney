<script lang="ts">
  import { pb, loadCategories, loadTags, uid, type Category, type Tag } from '../lib/pb'

  type Col = 'categories' | 'tags'
  let categories = $state<Category[]>([]), tags = $state<Tag[]>([])
  let counts = $state<Record<string, number>>({})
  let error = $state('')

  async function refresh() {
    try {
      ;[categories, tags] = await Promise.all([loadCategories(), loadTags()])
      // usage counts (one cheap query per label is fine at this scale)
      const c: Record<string, number> = {}
      await Promise.all([
        ...categories.map(async x => { c[x.id] = (await pb.collection('transactions').getList(1, 1, { filter: pb.filter('category = {:id}', { id: x.id }) })).totalItems }),
        ...tags.map(async x => { c[x.id] = (await pb.collection('transactions').getList(1, 1, { filter: pb.filter('tags ~ {:id}', { id: x.id }) })).totalItems }),
      ])
      counts = c
    } catch (e: any) { error = e.message }
  }
  refresh()

  async function add(col: Col, e: SubmitEvent) {
    e.preventDefault()
    const form = e.currentTarget as HTMLFormElement
    const name = (new FormData(form).get('name') as string).trim()
    if (!name) return
    try { await pb.collection(col).create({ name, user: uid() }); form.reset(); await refresh() }
    catch (err: any) { error = err?.data?.data?.name?.message ?? err.message }
  }
  async function rename(col: Col, r: Category | Tag) {
    const name = prompt('Rename', r.name)?.trim()
    if (!name || name === r.name) return
    try { await pb.collection(col).update(r.id, { name }); await refresh() } catch (err: any) { error = err.message }
  }
  async function del(col: Col, r: Category | Tag) {
    const n = counts[r.id] ?? 0
    if (!confirm(`Delete "${r.name}"?${n ? ` It is used by ${n} transaction(s); they will keep existing without it.` : ''}`)) return
    try { await pb.collection(col).delete(r.id); await refresh() } catch (err: any) { error = err.message }
  }
</script>

<h1>Categories & tags</h1>
<p class="muted small">A transaction has one <b>category</b> (what kind: Honorarnote, Software, Repairs…) and any number of
  <b>tags</b> (what it belongs to: a client, a project, a cost point, the house…). Both are created on the fly in the
  transaction form; here you can rename or delete them. Usage counts cover transactions only, not recurring templates.</p>
{#if error}<div class="error small">{error}</div>{/if}

<div class="row" style="align-items:flex-start; gap:1rem">
  {#each [['categories', 'Categories', 'category', categories], ['tags', 'Tags', 'tag', tags]] as [col, title, singular, items]}
    <div class="panel" style="flex:1; min-width:280px">
      <h3>{title}</h3>
      <table>
        <tbody>
          {#each items as r (r.id)}
            <tr><td>{r.name}</td><td class="muted small num">{counts[r.id] ?? ''}</td>
              <td class="num"><button class="link" onclick={() => rename(col as Col, r)}>rename</button>
                &nbsp; <button class="link danger" onclick={() => del(col as Col, r)}>delete</button></td></tr>
          {:else}<tr><td class="muted" colspan="3">None yet.</td></tr>{/each}
        </tbody>
      </table>
      <form class="row" style="margin-top:.6rem" onsubmit={(e) => add(col as Col, e)}>
        <input name="name" placeholder="new {singular}" style="flex:1" />
        <button>Add</button>
      </form>
    </div>
  {/each}
</div>
