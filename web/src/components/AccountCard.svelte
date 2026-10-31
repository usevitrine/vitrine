<script lang="ts">
  // Settings > Account (2026-10-31, the user: "give the user the option to change his username and password in
  // settings" ... "add passkey support with a passkey only login option"): who's logged in, logging out (this device,
  // the others), the name and the password (the current password first; a new password logs the other devices out),
  // the passkeys and logging in with them only. The server: internal/server/account.go.
  import { onMount } from 'svelte';
  import { reveal, pop } from '../lib/motion';
  import { api, type PasskeyList } from '../lib/api';
  import { settings } from '../lib/settings.svelte';
  import { passkeysWork, addPasskey, deviceName, cancelled } from '../lib/passkeys';
  import { confirmAction } from '../lib/confirm.svelte';
  import { toasts } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';
  import Rich from './Rich.svelte';
  import { t, date } from '../lib/i18n.svelte';

  let { user, onlogout, onrenamed }: { user: string; onlogout: () => void; onrenamed: (name: string) => void } = $props();

  // ---- the name and the password: one form open at a time
  let open = $state<'' | 'name' | 'password'>('');
  let current = $state('');
  let newName = $state('');
  let newPassword = $state('');
  let repeat = $state('');
  let busy = $state(false);
  let formError = $state('');
  function toggle(which: 'name' | 'password') {
    open = open === which ? '' : which;
    current = newPassword = repeat = formError = '';
    newName = which === 'name' ? user : '';
  }
  async function save(e: SubmitEvent) {
    e.preventDefault();
    formError = '';
    if (open === 'password' && newPassword !== repeat) {
      formError = t('settings.account.mismatch');
      return;
    }
    busy = true;
    try {
      if (open === 'name') {
        const res = await api.changeName(current, newName.trim());
        onrenamed(res.user);
        toasts.show(t('settings.account.nameChanged'), { cat: 'account', kind: 'info' });
      } else {
        await api.changePassword(current, newPassword);
        toasts.show(t('settings.account.passwordChanged'), { cat: 'account', kind: 'info' });
      }
      toggle(open as 'name' | 'password'); // (closed, emptied)
    } catch (err) {
      formError = (err as Error).message;
    } finally {
      busy = false;
    }
  }
  async function logoutOthers() {
    try {
      const { ended } = await api.logoutOthers();
      toasts.show(ended ? t('settings.account.loggedOutOthers', { n: ended }) : t('settings.account.noOthers'), { cat: 'account', kind: 'info' });
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }

  // ---- passkeys
  let keys = $state<PasskeyList | null>(null);
  const canAdd = passkeysWork();
  let adding = $state(false);
  let renaming = $state<number | null>(null);
  let renameTo = $state('');
  onMount(async () => {
    try {
      keys = await api.passkeys();
    } catch (err) {
      toasts.error((err as Error).message);
    }
  });
  const host = (origin: string) => origin.replace(/^https?:\/\//, '');
  const day = (ms: number) => date(new Date(ms), { day: 'numeric', month: 'short', year: 'numeric' });
  async function add() {
    adding = true;
    try {
      await addPasskey(deviceName());
      keys = await api.passkeys();
      toasts.show(t('settings.account.added'), { cat: 'account', kind: 'info' });
    } catch (err) {
      if (!cancelled(err)) toasts.error((err as Error).message);
    } finally {
      adding = false;
    }
  }
  async function remove(id: number, name: string) {
    const last = keys?.passkeys.length === 1 && keys.passkey_only;
    const ok = await confirmAction({
      title: t('settings.account.removeTitle', { name }),
      body: t('settings.account.removeBody') + (last ? ' ' + t('settings.account.removeLast') : ''),
      action: t('settings.account.remove'),
      icon: 'trash',
    });
    if (!ok) return;
    try {
      keys = await api.passkeyRemove(id);
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }
  async function rename(e: SubmitEvent, id: number) {
    e.preventDefault();
    try {
      keys = await api.passkeyRename(id, renameTo);
      renaming = null;
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }
  async function setOnly(on: boolean) {
    try {
      keys = await api.passkeyOnly(on);
    } catch (err) {
      toasts.error((err as Error).message);
      keys = keys ? { ...keys } : keys; // (the box back as it was)
    }
  }
</script>

<section class="card">
  <div class="card-head"><h2>{t('settings.section.account.label')}</h2></div>
  <div class="row">
    <p><Rich text={t('settings.loggedInAsBold', { user })} /></p>
    <button class="quiet" onclick={onlogout}>{t('settings.section.account.hint')}</button>
  </div>
  <div class="actions">
    <button class="quiet" aria-expanded={open === 'name'} onclick={() => toggle('name')}><Icon name="user" size={16} /> {t('settings.account.changeName')}</button>
    <button class="quiet" aria-expanded={open === 'password'} onclick={() => toggle('password')}><Icon name="key" size={16} /> {t('settings.account.changePassword')}</button>
    <button class="quiet" onclick={logoutOthers}><Icon name="device" size={16} /> {t('settings.account.logOutOthers')}</button>
  </div>
  {#if open}
    <form class="change" onsubmit={save} transition:reveal>
      {#if open === 'name'}
        <label class="field">
          <span class="field-label">{t('settings.account.newName')}</span>
          <input bind:value={newName} autocomplete="username" autocapitalize="off" maxlength="100" required />
        </label>
      {:else}
        <label class="field">
          <span class="field-label">{t('settings.account.newPassword')}</span>
          <input bind:value={newPassword} type="password" autocomplete="new-password" minlength="8" maxlength="1024" required />
        </label>
        <label class="field">
          <span class="field-label">{t('settings.account.repeat')}</span>
          <input bind:value={repeat} type="password" autocomplete="new-password" minlength="8" maxlength="1024" required />
        </label>
      {/if}
      <label class="field">
        <span class="field-label">{t('settings.account.current')}</span>
        <input bind:value={current} type="password" autocomplete="current-password" maxlength="1024" required />
      </label>
      {#if formError}<p class="error" role="alert" transition:reveal>{formError}</p>{/if}
      <div class="foot">
        <button type="button" class="quiet" onclick={() => (open = '')}>{t('common.cancel')}</button>
        <button type="submit" disabled={busy}>{t('settings.account.save')}</button>
      </div>
    </form>
  {/if}
  <label class="check">
    <input type="checkbox" bind:checked={settings.anonymousLogin} />
    <span class="field">
      <span class="field-label">{t('settings.anonymous.label')}</span>
      <span class="hint">{t('settings.anonymous.hint')}</span>
    </span>
  </label>
</section>

<section class="card" data-card="passkeys">
  <div class="card-head">
    <h2>{t('settings.account.passkeys')}</h2>
    <p class="hint">{t('settings.account.passkeysHint')}</p>
  </div>
  {#if keys}
    {#if keys.passkeys.length}
      <ul class="keys">
        {#each keys.passkeys as k (k.id)}
          <li transition:reveal>
            <Icon name="key" size={18} />
            {#if renaming === k.id}
              <form class="rename" onsubmit={(e) => rename(e, k.id)}>
                <!-- svelte-ignore a11y_autofocus -->
                <input bind:value={renameTo} maxlength="60" aria-label={t('settings.account.rename')} autofocus required />
                <button type="submit" class="quiet">{t('settings.account.save')}</button>
              </form>
            {:else}
              <span class="what">
                <span class="name">{k.name}</span>
                <span class="hint">
                  {t('settings.account.made', { date: day(k.created_at) })}{#if k.last_used} · {t('settings.account.used', { date: day(k.last_used) })}{/if}{#if k.origin !== keys.here} · {t('settings.account.elsewhere', { host: host(k.origin) })}{/if}
                </span>
              </span>
              <button class="icon" aria-label={`${t('settings.account.rename')}: ${k.name}`} title={t('settings.account.rename')} onclick={() => ((renaming = k.id), (renameTo = k.name))}><Icon name="pencil" size={16} /></button>
              <button class="icon" aria-label={`${t('settings.account.remove')}: ${k.name}`} title={t('settings.account.remove')} onclick={() => remove(k.id, k.name)}><Icon name="trash" size={16} /></button>
            {/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="hint" transition:reveal>{t('settings.account.none')}</p>
    {/if}
    {#if canAdd}
      <button class="add" disabled={adding} onclick={add}><Icon name="plus" size={16} /> {t('settings.account.add')}</button>
    {:else}
      <p class="hint" transition:reveal>{t('settings.account.unsupported')}</p>
    {/if}
    <label class="check">
      <input type="checkbox" checked={keys.passkey_only} disabled={!keys.passkeys.length} onchange={(e) => setOnly((e.currentTarget as HTMLInputElement).checked)} />
      <span class="field">
        <span class="field-label">{t('settings.account.only')}</span>
        <span class="hint">{keys.passkeys.length ? t('settings.account.onlyHint') : t('settings.account.onlyNeeds')}</span>
      </span>
    </label>
  {:else}
    <p class="hint" transition:pop>{t('common.loading')}</p>
  {/if}
</section>

<style>
  p {
    margin: 0;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .actions button,
  .add {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .add {
    justify-self: start;
  }
  .change {
    display: grid;
    gap: 10px;
    max-width: 360px;
  }
  .field {
    display: grid;
    gap: 4px;
    min-width: 0;
  }
  .field-label {
    font-weight: 500;
    color: var(--text);
  }
  .hint {
    font-size: var(--font-small);
    color: var(--overlay1);
  }
  .error {
    color: var(--red);
    font-size: var(--font-small);
  }
  .foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .check {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    cursor: pointer;
  }
  .check input {
    margin-top: 2px;
  }
  .keys {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 2px;
  }
  .keys li {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 4px;
    color: var(--subtext);
  }
  .what {
    flex: 1;
    min-width: 0;
    display: grid;
  }
  .name {
    color: var(--text);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rename {
    flex: 1;
    display: flex;
    gap: 8px;
  }
  .rename input {
    flex: 1;
    min-width: 0;
  }
</style>
