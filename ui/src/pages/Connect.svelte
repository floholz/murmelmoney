<script lang="ts">
  import { pb } from '../lib/pb'

  const endpoint = `${window.location.origin}/api/murmel/mcp`
  const validity = [[30, '30 days'], [365, '1 year'], [1825, '5 years']] as const

  let days = $state<number>(365)
  let scope = $state<'write' | 'read'>('write')
  let token = $state('')
  let expires = $state('')
  let tokenScope = $state('')
  let error = $state('')
  let busy = $state(false)
  let copied = $state('')

  async function create() {
    busy = true; error = ''
    try {
      const r = await pb.send('/api/murmel/tokens', { method: 'POST', body: { days, scope } })
      token = r.token; tokenScope = r.scope; expires = new Date(r.expires).toLocaleDateString()
    } catch (e: any) { error = e.message } finally { busy = false }
  }

  async function revoke() {
    if (!confirm('Revoke every token ever issued for your account? Connected agents stop working and you will be signed out of the web app on all devices.')) return
    busy = true; error = ''
    try { await pb.send('/api/murmel/tokens/revoke', { method: 'POST' }); pb.authStore.clear() }
    catch (e: any) { error = e.message; busy = false }
  }

  async function copy(text: string, what: string) {
    try { await navigator.clipboard.writeText(text); copied = what; setTimeout(() => (copied = ''), 1500) }
    catch { error = 'Could not copy — select the text and copy it by hand.' }
  }

  const shown = $derived(token || '<TOKEN>')
  const claudeCode = $derived(`claude mcp add --transport http murmelmoney ${endpoint} --header "Authorization: Bearer ${shown}"`)
  const jsonConfig = $derived(JSON.stringify({ mcpServers: { murmelmoney: { type: 'http', url: endpoint, headers: { Authorization: `Bearer ${shown}` } } } }, null, 2))
  const bridgeConfig = $derived(JSON.stringify({ mcpServers: { murmelmoney: { command: 'npx', args: ['-y', 'mcp-remote', endpoint, '--header', `Authorization:Bearer ${shown}`] } } }, null, 2))

  const tools: [string, string, boolean][] = [
    ['list_transactions · get_transaction', 'filter by year / date range, type, area, category, tag, note text or loan', true],
    ['list_categories · list_tags', 'with usage counts', true],
    ['year_summary', 'totals per area, category and tag plus the projected recurring amounts — the overview page as data', true],
    ['get_tax_rule', 'your active tax script, so the agent can estimate what to put aside', true],
    ['list_recurring · list_loans', 'templates with their next occurrence; balance, repaid and interest per loan', true],
    ['create_transaction · update_transaction · delete_transaction', 'categories and tags by name, created on the fly', false],
    ['create_recurring · update_recurring · delete_recurring', 'rent, subscriptions, retainers', false],
    ['create_loan', 'payments are then expense transactions linked to the loan', false],
  ]
</script>

<h1>AI agents &amp; API</h1>
<p class="muted small">murmelmoney speaks the <a href="https://modelcontextprotocol.io" target="_blank" rel="noreferrer">Model Context
  Protocol</a>, so an AI assistant (Claude Code, Claude Desktop, Cursor, …) can log expenses, look things up and
  summarize a year for you — in your data only. Create a token, hand it to the agent, done.</p>
{#if error}<div class="error small">{error}</div>{/if}

<div class="panel">
  <h3>1 · Access token</h3>
  <p class="small">A token is a long-lived login for your account. Treat it like a password: it is shown once and not stored anywhere readable.
    A <b>read-only</b> token lets the agent look things up and summarize but never create, change or delete anything — the agent is
    told so when it connects and will ask you for a read &amp; write token if you want changes made.</p>
  <div class="row">
    <select bind:value={scope}>
      <option value="write">Read &amp; write</option>
      <option value="read">Read-only</option>
    </select>
    <select bind:value={days}>
      {#each validity as [d, label]}<option value={d}>{label}</option>{/each}
    </select>
    <button class="primary" onclick={create} disabled={busy}>Create token</button>
  </div>
  {#if token}
    <div class="row" style="margin-top:.8rem; align-items:flex-start">
      <pre class="snippet" style="flex:1">{token}</pre>
      <button onclick={() => copy(token, 'token')}>{copied === 'token' ? 'Copied' : 'Copy'}</button>
    </div>
    <p class="muted small">{tokenScope === 'read' ? 'Read-only' : 'Read & write'}, valid until {expires}. Copy it now — it will not be shown again.</p>
  {/if}
</div>

<div class="panel">
  <h3>2 · Connect an agent</h3>
  <p class="small">Endpoint: <code>{endpoint}</code>{#if !token} (create a token above to fill in the snippets){/if}</p>

  <p class="small" style="margin-bottom:.3rem"><b>Claude Code</b></p>
  <div class="row" style="align-items:flex-start">
    <pre class="snippet" style="flex:1">{claudeCode}</pre>
    <button onclick={() => copy(claudeCode, 'cc')}>{copied === 'cc' ? 'Copied' : 'Copy'}</button>
  </div>

  <p class="small" style="margin:.8rem 0 .3rem"><b>Clients with HTTP + headers</b> (Cursor, Claude Desktop via <i>Settings → Developer</i>, most others)</p>
  <div class="row" style="align-items:flex-start">
    <pre class="snippet" style="flex:1">{jsonConfig}</pre>
    <button onclick={() => copy(jsonConfig, 'json')}>{copied === 'json' ? 'Copied' : 'Copy'}</button>
  </div>

  <p class="small" style="margin:.8rem 0 .3rem"><b>Clients that only run local (stdio) servers</b> — bridge it with <code>mcp-remote</code></p>
  <div class="row" style="align-items:flex-start">
    <pre class="snippet" style="flex:1">{bridgeConfig}</pre>
    <button onclick={() => copy(bridgeConfig, 'bridge')}>{copied === 'bridge' ? 'Copied' : 'Copy'}</button>
  </div>
</div>

<div class="panel">
  <h3>What the agent can do</h3>
  <table>
    <tbody>
      {#each tools as [name, what, read]}
        <tr><td><code>{name}</code></td><td class="muted small">{what}</td><td class="num"><span class="tag">{read ? 'read' : 'write'}</span></td></tr>
      {/each}
    </tbody>
  </table>
  <p class="muted small" style="margin:.6rem 0 0">Everything is scoped to your account. A read-only token only sees the <i>read</i> tools (and is refused on
    every write of the regular API as well). Attachments can be listed but not uploaded through the agent.</p>
</div>

<div class="panel">
  <h3>Revoke</h3>
  <p class="small">Lost a token or want to cut off every agent? Revoking invalidates <b>all</b> tokens of your account at once — including this browser session, so you will have to log in again.</p>
  <button class="danger" onclick={revoke} disabled={busy}>Revoke all tokens</button>
</div>
