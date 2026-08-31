<script lang="ts">
  import { pb } from './lib/pb'
  import Login from './pages/Login.svelte'
  import Overview from './pages/Overview.svelte'
  import Transactions from './pages/Transactions.svelte'
  import Recurring from './pages/Recurring.svelte'
  import Loans from './pages/Loans.svelte'
  import Labels from './pages/Labels.svelte'
  import Rules from './pages/Rules.svelte'
  import ThemeToggle from './lib/ThemeToggle.svelte'
  import Icon from './lib/Icon.svelte'
  import './lib/theme'

  let authed = $state(pb.authStore.isValid)
  pb.authStore.onChange(() => (authed = pb.authStore.isValid))

  let route = $state(location.hash.slice(1) || '/')
  window.addEventListener('hashchange', () => (route = location.hash.slice(1) || '/'))

  const pages = [
    ['/', 'Overview', Overview, 'overview', 'Overview'],
    ['/transactions', 'Transactions', Transactions, 'transactions', 'Transactions'],
    ['/recurring', 'Recurring', Recurring, 'recurring', 'Repeat'],
    ['/loans', 'Loans', Loans, 'loans', 'Loans'],
    ['/labels', 'Categories & tags', Labels, 'labels', 'Labels'],
    ['/rules', 'Rules', Rules, 'rules', 'Rules'],
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
  <nav class="tabbar" aria-label="Main">
    {#each pages as [path, , , icon, short]}
      <a href={'#' + path} class:active={route === path} aria-current={route === path ? 'page' : undefined}>
        <Icon name={icon} /><span>{short}</span>
      </a>
    {/each}
  </nav>
{/if}
