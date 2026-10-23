<script lang="ts">
  import Logo from '../components/Logo.svelte';
  import Icon from '../components/Icon.svelte';
  import { api, type Session } from '../lib/api';
  import { t } from '../lib/i18n.svelte';

  let { setup, onDone }: { setup: boolean; onDone: (s: Session) => void } = $props();

  let name = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);
  let show = $state(false); // the password in plain text (to check a new one for typos)

  // "Forgot it? Run {command} on the server." around the command.
  const forgot = $derived(t('login.forgot').split('{command}'));

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
</script>

<div class="wrap">
  <form class="card" onsubmit={submit}>
    <h1><Logo size={36} /></h1>
    {#if setup}
      <p class="muted">{t('login.setupHint')}</p>
    {/if}
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
