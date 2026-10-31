<script lang="ts">
  import { onMount } from 'svelte';
  import Logo from '../components/Logo.svelte';
  import Icon from '../components/Icon.svelte';
  import { api, type Session } from '../lib/api';
  import { passkeysWork, signInWithPasskey, cancelled } from '../lib/passkeys';
  import { t } from '../lib/i18n.svelte';

  // Logging in: a name and password, or a passkey (2026-10-31: offered whenever the browser can; with passkey-only
  // sign-in on, the passkey alone - the server refuses every password then).
  let { setup, passkeyOnly: given = false, onDone }: { setup: boolean; passkeyOnly?: boolean; onDone: (s: Session) => void } = $props();
  // (asked again here: turned on since the app started, then logged out - the start's answer is old)
  // svelte-ignore state_referenced_locally
  let passkeyOnly = $state(given);
  onMount(async () => {
    if (setup) return;
    try {
      passkeyOnly = !!(await api.session()).passkey_only;
    } catch {
      /* as the app last heard */
    }
  });

  let name = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);
  let show = $state(false); // the password in plain text (to check a new one for typos)
  const canPasskey = passkeysWork();

  // "Forgot it? Run {command} on the server." around the command.
  const forgot = $derived(t(passkeyOnly ? 'login.lostPasskeys' : 'login.forgot').split('{command}'));

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    busy = true;
    try {
      const s = setup ? await api.setup(name, password) : await api.login(name, password);
      password = '';
      onDone({ ...s, setup_needed: false, logged_in: true });
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }

  async function passkey() {
    error = '';
    busy = true;
    try {
      const s = await signInWithPasskey();
      onDone({ ...s, setup_needed: false, logged_in: true });
    } catch (err) {
      if (!cancelled(err)) error = t('login.passkeyFailed', { error: (err as Error).message });
    } finally {
      busy = false;
    }
  }
</script>

<div class="wrap">
  <form class="card" onsubmit={submit}>
    <h1><Logo size={36} /></h1>
    {#if setup}
      <p class="muted">{t('login.setupHint')}</p>
    {:else if passkeyOnly}
      <p class="muted">{t('login.passkeyOnly')}</p>
    {/if}
    {#if !passkeyOnly || setup}
      <label>
        {t('login.name')}
        <input bind:value={name} autocomplete="username" autocapitalize="off" required />
      </label>
      <label>
        {t('login.password')}
        <span class="secret">
          <input
            bind:value={password}
            type={show ? 'text' : 'password'}
            autocomplete={setup ? 'new-password' : 'current-password'}
            minlength={setup ? 8 : undefined}
            maxlength={1024}
            autocapitalize="off"
            spellcheck="false"
            required />
          <button
            type="button"
            class="icon eye"
            aria-label={show ? t('login.hidePassword') : t('login.showPassword')}
            aria-pressed={show}
            title={show ? t('login.hidePassword') : t('login.showPassword')}
            onclick={() => (show = !show)}><Icon name={show ? 'eyeoff' : 'eye'} size={18} /></button>
        </span>
      </label>
      <button type="submit" disabled={busy}>{setup ? t('login.create') : t('login.logIn')}</button>
    {/if}
    {#if !setup && (canPasskey || passkeyOnly)}
      {#if !passkeyOnly}<p class="or" aria-hidden="true"><span>{t('login.or')}</span></p>{/if}
      <button type="button" class:quiet={!passkeyOnly} class="passkey" disabled={busy || !canPasskey} onclick={passkey}>
        <Icon name="key" size={18} /> {t('login.passkey')}
      </button>
    {/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    {#if !setup}
      <p class="hint forgot">{forgot[0]}<code>vitrine passwd</code>{forgot[1]}</p>
    {/if}
  </form>
</div>

<style>
  .wrap {
    min-height: 100%;
    display: grid;
    place-items: center;
    padding: calc(16px + var(--safe-top)) 16px 16px;
  }
  .card {
    width: min(360px, 100%);
    display: grid;
    gap: 14px;
    padding: 28px 24px 24px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    border-radius: 16px;
    box-shadow: 0 24px 60px rgb(0 0 0 / 0.35);
  }
  h1 {
    margin: 0 0 6px;
    font-size: inherit;
  }
  h1 :global(.name) {
    font-size: 24px;
  }
  p {
    margin: 0;
  }
  label {
    display: grid;
    gap: 6px;
    font-weight: 500;
  }
  button[type='submit'] {
    margin-top: 4px;
  }
  .passkey {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }
  /* "or" between the password and the passkey: a line each side. */
  .or {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--overlay1);
    font-size: var(--font-small);
  }
  .or::before,
  .or::after {
    content: '';
    flex: 1;
    height: 1px;
    background: var(--surface0);
  }
  .secret {
    position: relative;
    display: block;
  }
  .secret input {
    width: 100%;
    padding-right: 44px;
  }
  .eye {
    position: absolute;
    right: 2px;
    top: 50%;
    translate: 0 -50%;
    color: var(--subtext);
  }
  .forgot {
    margin-top: 2px;
  }
  .forgot code {
    font-size: 0.95em;
  }
</style>
