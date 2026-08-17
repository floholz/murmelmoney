<script lang="ts">
  import { pb, status } from '../lib/pb'
  let mode = $state<'login' | 'register'>('login')
  let canRegister = $state(false)
  status().then(s => (canRegister = s.registration)).catch(() => {})
  let email = $state(''), password = $state(''), confirm = $state(''), name = $state('')
  let error = $state(''), busy = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault(); busy = true; error = ''
    try {
      if (mode === 'register') {
        if (password !== confirm) throw new Error('Passwords do not match')
        await pb.collection('users').create({ email, password, passwordConfirm: confirm, name })
      }
      await pb.collection('users').authWithPassword(email, password)
    } catch (err: any) {
      const data = err?.data?.data
      error = data ? Object.entries(data).map(([k, v]: any) => `${k}: ${v.message}`).join('\n') : (err?.message ?? String(err))
    } finally { busy = false }
  }
</script>

<div class="panel login">
  <h1 class="brand"><img src="/logo.svg" alt="" /> murmelmoney</h1>
  {#if canRegister}
    <div class="row seg" style="margin-bottom:.8rem">
      <button type="button" class:on={mode === 'login'} onclick={() => (mode = 'login')}>Sign in</button>
      <button type="button" class:on={mode === 'register'} onclick={() => (mode = 'register')}>Register</button>
    </div>
  {/if}
  <form onsubmit={submit} class="row" style="flex-direction:column; align-items:stretch">
    {#if mode === 'register'}
      <label class="field">Name (optional) <input bind:value={name} autocomplete="name" /></label>
    {/if}
    <label class="field">Email <input type="email" bind:value={email} required autocomplete="username" /></label>
    <label class="field">Password <input type="password" bind:value={password} required minlength="8" autocomplete={mode === 'login' ? 'current-password' : 'new-password'} /></label>
    {#if mode === 'register'}
      <label class="field">Confirm password <input type="password" bind:value={confirm} required autocomplete="new-password" /></label>
    {/if}
    {#if error}<div class="error small">{error}</div>{/if}
    <button class="primary" disabled={busy}>{mode === 'login' ? 'Sign in' : 'Create account'}</button>
  </form>
</div>
