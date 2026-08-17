<script lang="ts">
  import { pb } from './lib/pb'
  import Login from './pages/Login.svelte'
  import Overview from './pages/Overview.svelte'
  import Transactions from './pages/Transactions.svelte'
  import Labels from './pages/Labels.svelte'
  import Rules from './pages/Rules.svelte'
  import ThemeToggle from './lib/ThemeToggle.svelte'
  import './lib/theme'

  let authed = $state(pb.authStore.isValid)
  pb.authStore.onChange(() => (authed = pb.authStore.isValid))

  let route = $state(location.hash.slice(1) || '/')
  window.addEventListener('hashchange', () => (route = location.hash.slice(1) || '/'))

  const pages = [
    ['/', 'Overview', Overview],
    ['/transactions', 'Transactions', Transactions],
    ['/labels', 'Categories & tags', Labels],
    ['/rules', 'Rules', Rules],
  ] as const
  const Page = $derived(pages.find(p => p[0] === route)?.[2] ?? Overview)
</script>

{#if !authed}
  <div class="login-wrap"><ThemeToggle /><Login /></div>
{:else}
  <header class="top">
    <a class="brand" href="#/"><img src="/logo.svg" alt="murmelmoney" /><span>murmelmoney</span></a>
    <nav>
      {#each pages as [path, label]}
        <a href={'#' + path} class:active={route === path}>{label}</a>
      {/each}
    </nav>
    <span class="spacer"></span>
    <span class="muted small user">{pb.authStore.record?.name || pb.authStore.record?.email}</span>
    <ThemeToggle />
    <button onclick={() => pb.authStore.clear()}>Logout</button>
  </header>
  <main>{#key authed}<Page />{/key}</main>
{/if}
