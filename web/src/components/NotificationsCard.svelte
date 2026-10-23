<script lang="ts">
  // Settings > Notifications: turn push notifications on for this device,
  // send a test, pick what to be told about, and see the other devices.
  import { onMount } from 'svelte';
  import { push, deviceLabel } from '../lib/push.svelte';
  import { settings } from '../lib/settings.svelte';
  import { toasts } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';
  import Rich from './Rich.svelte';
  import { t, date } from '../lib/i18n.svelte';

  let testing = $state(false);
  let tested = $state<{ ok: boolean; text: string } | null>(null);
  const label = deviceLabel();

  onMount(() => {
    push.load();
  });

  const others = $derived(push.devices.filter((d) => d.endpoint !== push.endpoint));

  const status = $derived.by((): { text: string; cls: string } => {
    if (push.support === 'needs-home-screen') return { text: t('notify.status.homeScreen'), cls: 'neutral' };
    if (push.support !== 'ok') return { text: t('notify.status.unavailable'), cls: 'neutral' };
    if (push.permission === 'denied') return { text: t('notify.status.blocked'), cls: 'bad' };
    if (push.endpoint) return { text: t('notify.status.on'), cls: 'ok' };
    return { text: t('notify.status.off'), cls: 'neutral' };
  });

  async function test() {
    if (!push.endpoint) return;
    testing = true;
    tested = null;
    try {
      await push.test(push.endpoint);
      tested = { ok: true, text: t('notify.sent') };
    } catch (e) {
      tested = { ok: false, text: (e as Error).message };
    } finally {
      testing = false;
    }
  }

  async function remove(endpoint: string, name: string) {
    try {
      await push.remove(endpoint);
      toasts.show(t('notify.removed', { name }), { cat: 'device', kind: 'removed' });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  const added = (ms: number) => date(ms, { day: 'numeric', month: 'short', year: 'numeric' });
</script>

<section class="card">
  <div class="card-head">
    <h2>{t('notify.title')}</h2>
    <p class="hint">{t('notify.hint')}</p>
  </div>

  <article class="device">
    <header>
      <div class="title">
        <h3>{t('notify.thisDevice')}</h3>
        <p class="meta">{label}</p>
      </div>
      <span class="status {status.cls}">{status.text}</span>
    </header>

    {#if push.support === 'needs-home-screen'}
      <p class="hint"><Rich text={t('notify.homeScreen')} /></p>
    {:else if push.support === 'unsupported'}
      <p class="hint">{t('notify.unsupported')}</p>
    {:else if push.permission === 'denied'}
      <p class="hint">{t('notify.blocked')}</p>
    {:else if !push.loaded}
      <p class="hint">{t('notify.checking')}</p>
    {:else if push.endpoint}
      <div class="actions">
        <button onclick={test} disabled={testing || push.busy}>{testing ? t('notify.sending') : t('notify.test')}</button>
        <button class="quiet" onclick={() => push.disable()} disabled={push.busy}>{t('notify.turnOff')}</button>
      </div>
    {:else}
      <div class="actions">
        <button onclick={() => push.enable()} disabled={push.busy}>
          <Icon name="bell" size={16} />
          {push.busy ? t('notify.turningOn') : t('notify.turnOn')}
        </button>
      </div>
    {/if}

    {#if push.error}
      <p class="result" role="status"><Icon name="close" size={16} /><span>{push.error}</span></p>
    {:else if tested}
      <p class="result" class:ok={tested.ok} role="status">
        <Icon name={tested.ok ? 'check' : 'close'} size={16} /><span>{tested.text}</span>
      </p>
    {/if}
  </article>

  <fieldset class="kinds">
    <legend class="field-label">{t('notify.kinds')}</legend>
    <label class="check">
      <input type="checkbox" bind:checked={settings.notifyNew} />
      <span class="field">
        <span class="field-label">{t('notify.new.label')}</span>
        <span class="hint">{t('notify.new.hint')}</span>
      </span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={settings.notifyDone} />
      <span class="field">
        <span class="field-label">{t('notify.done.label')}</span>
        <span class="hint">{t('notify.done.hint')}</span>
      </span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={settings.notifyFailed} />
      <span class="field">
        <span class="field-label">{t('notify.failed.label')}</span>
        <span class="hint">{t('notify.failed.hint')}</span>
      </span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={settings.notifyBlacklisted} />
      <span class="field">
        <span class="field-label">{t('notify.blacklisted')}</span>
      </span>
    </label>
  </fieldset>

  {#if others.length}
    <div class="others">
      <span class="field-label">{t('notify.others')}</span>
      <ul>
        {#each others as d (d.endpoint)}
          <li>
            <span class="who">
              <span>{d.label}</span>
              <span class="meta">{t('notify.added', { date: added(d.created_at) })}</span>
            </span>
            <button
              class="icon"
              aria-label={t('notify.stop', { name: d.label })}
              title={t('notify.stopThis')}
              onclick={() => remove(d.endpoint, d.label)}><Icon name="close" size={18} /></button>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</section>

<style>
  p {
    margin: 0;
  }
  .device {
    display: grid;
    gap: 12px;
    padding: 14px;
    border-radius: 12px;
    background: var(--crust);
    border: 1px solid var(--surface0);
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }
  .title {
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--font);
    font-weight: 700;
  }
  .meta {
    font-size: var(--font-small);
    color: var(--subtext);
  }
  .title .meta {
    margin-top: 2px;
  }
  .status {
    flex: none;
    font-size: var(--font-small);
    font-weight: 600;
    padding: 2px 9px;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--subtext);
  }
  .status.ok {
    color: var(--green);
    background: color-mix(in srgb, var(--green) 14%, transparent);
  }
  .status.bad {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 14%, transparent);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
  }
  .result {
    display: flex;
    gap: 8px;
    align-items: flex-start;
    padding: 9px 12px;
    border-radius: 10px;
    font-size: var(--font-small);
    color: var(--red);
    background: color-mix(in srgb, var(--red) 10%, transparent);
    overflow-wrap: anywhere;
  }
  .result.ok {
    color: var(--green);
    background: color-mix(in srgb, var(--green) 10%, transparent);
  }
  .result :global(svg) {
    flex: none;
    margin-top: 1px;
  }
  .kinds {
    display: grid;
    gap: 12px;
    margin: 0;
    padding: 0;
    border: 0;
  }
  .kinds legend {
    padding: 0;
    margin-bottom: 12px;
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
  .check .field {
    gap: 2px;
  }
  .others {
    display: grid;
    gap: 8px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 6px 6px 6px 12px;
    border-radius: 10px;
    background: var(--crust);
    border: 1px solid var(--surface0);
  }
  .who {
    display: grid;
    min-width: 0;
  }
  li button {
    color: var(--subtext);
  }
</style>
