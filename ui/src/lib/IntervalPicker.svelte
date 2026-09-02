<script lang="ts">
  // Interval select with the named presets plus a "custom" mode ("every N
  // weeks/months/years"). The bound value is the interval string as stored.
  import { INTERVAL_PRESETS, INTERVAL_UNITS, parseInterval, canonicalInterval, type IntervalUnit } from './recurring'

  let { value = $bindable(''), none = '' }: {
    value: string
    /** Label of an extra empty option ('' = value ''), e.g. "never (one-off)". */
    none?: string
  } = $props()

  const CUSTOM = '__custom'
  const initial = parseInterval(value)
  let mode = $state<string>(!value ? '' : INTERVAL_PRESETS[value] ? value : CUSTOM)
  let count = $state<number>(initial && !INTERVAL_PRESETS[value] ? initial.count : 2)
  let unit = $state<IntervalUnit>(initial && !INTERVAL_PRESETS[value] ? initial.unit : 'week')

  function pick(m: string) {
    mode = m
    value = m === CUSTOM ? canonicalInterval(count || 1, unit) : m
  }
  function custom() {
    if (mode === CUSTOM) value = canonicalInterval(Math.min(999, Math.max(1, Math.floor(count || 1))), unit)
  }
</script>

<select value={mode} onchange={(e) => pick(e.currentTarget.value)}>
  {#if none}<option value="">{none}</option>{/if}
  {#each Object.keys(INTERVAL_PRESETS) as p}<option value={p}>{p}</option>{/each}
  <option value={CUSTOM}>every…</option>
</select>
{#if mode === CUSTOM}
  <span class="row" style="gap:.3rem; margin-top:.3rem">
    <span class="small muted">every</span>
    <input type="number" min="1" max="999" step="1" bind:value={count} oninput={custom} required style="width:4.5rem" />
    <select bind:value={unit} onchange={custom}>
      {#each INTERVAL_UNITS as u}<option value={u}>{u}{count === 1 ? '' : 's'}</option>{/each}
    </select>
  </span>
{/if}
