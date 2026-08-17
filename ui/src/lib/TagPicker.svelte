<script lang="ts">
  import type { Tag } from './pb'
  /** Multi-select of tag *names*; unknown names are created on save by the parent. */
  let { selected = $bindable([]), options = [] }: { selected: string[]; options: Tag[] } = $props()
  let input = $state('')

  function add(name: string) {
    name = name.trim()
    if (name && !selected.includes(name)) selected = [...selected, name]
    input = ''
  }
  function onkey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); add(input) }
    else if (e.key === 'Backspace' && !input && selected.length) selected = selected.slice(0, -1)
  }
</script>

<div class="row" style="gap:.3rem">
  {#each selected as name}
    <span class="tag chip">{name} <button type="button" class="link" onclick={() => (selected = selected.filter(x => x !== name))} aria-label="remove {name}">×</button></span>
  {/each}
  <input list="tag-options" bind:value={input} onkeydown={onkey} onchange={() => add(input)}
    placeholder={selected.length ? '' : 'client, project, cost point… (Enter adds)'} style="flex:1; min-width:10rem" />
  <datalist id="tag-options">{#each options.filter(o => !selected.includes(o.name)) as o}<option value={o.name}></option>{/each}</datalist>
</div>
