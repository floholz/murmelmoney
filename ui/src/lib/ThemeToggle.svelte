<script lang="ts">
  import { current, toggle } from './theme'
  let theme = $state(current())
</script>

<button class="theme-toggle" type="button" title="Switch to {theme === 'dark' ? 'light' : 'dark'} theme"
  aria-label="Toggle theme" onclick={() => (theme = toggle())}>{theme === 'dark' ? '☀' : '☾'}</button>
