<script lang="ts">
  // Vitrine's updates, at the top of Settings > Updates (its state shared with the page: bind:info): up to date or the newer version (its page on GitHub), Check
  // now, and with Watchtower set up (or the Windows installer's Vitrine) an Update button - a backup, then Watchtower recreates the container on the new
  // image (or the new installer runs), followed full screen (lib/updater, components/Updater). Without Watchtower: how to update by hand.
  // The daily check can be switched off (shared: the server does it).
  import { onMount } from 'svelte';
  import { api, type UpdateState } from '../lib/api';
  import { settings } from '../lib/settings.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { updater } from '../lib/updater.svelte';
  import { reveal, swap } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';

  let { info = $bindable(null) }: { info?: UpdateState | null } = $props();
  let checking = $state(false);

  onMount(async () => {
    try {
      info = await api.updateState();
    } catch {
      /* no update API (an older server): nothing shown */
    }
  });

  async function checkNow() {
    checking = true;
    try {
      info = await api.checkForUpdate();
    } catch (e) {
      toasts.error(t('updates.checkFailed', { error: (e as Error).message }));
    } finally {
      checking = false;
    }
  }

  async function update() {
    if (!info) return;
    if (!(await confirmAction({ title: t('updates.confirmTitle', { version: info.latest }), body: t('updates.confirmBody'), action: t('updates.update'), icon: 'download' }))) return;
    updater.start(info); // (full screen: lib/updater)
  }
</script>

{#if info}
  <div class="update" transition:reveal>
    {#if info.newer}
      <div class="status newer" in:swap>
        <span class="badge"><Icon name="download" size={15} /> {t('updates.available', { version: info.latest })}</span>
        {#if info.page}<a href={info.page} target="_blank" rel="noopener noreferrer">{t('updates.whatsNew')} <Icon name="external" size={14} /></a>{/if}
      </div>
      {#if info.can_update}
        <button class="primary go" onclick={update} in:swap><Icon name="download" size={18} /> {t('updates.updateTo', { version: info.latest })}</button>
      {:else}
        <p class="hint" in:swap>{t('updates.howTo')}</p>
      {/if}
    {:else}
      <p class="status" in:swap>
        {#if info.error}<span class="error-text">{t('updates.checkFailed', { error: info.error })}</span>{:else if info.latest}<span class="muted"><Icon name="check" size={15} /> {t('updates.upToDate')}</span>{/if}
        <button class="link" onclick={checkNow} disabled={checking}>{checking ? t('updates.checking') : t('updates.checkNow')}</button>
      </p>
    {/if}
    <label class="check">
      <input type="checkbox" bind:checked={settings.updateCheck} />
      <span class="field">
        <span class="field-label">{t('updates.check')}</span>
        <span class="hint">{t('updates.checkHint')}</span>
      </span>
    </label>
  </div>
{/if}

<style>
  .update {
    display: grid;
    gap: 10px;
  }
  .status {
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 14px;
  }
  .status .muted,
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .badge {
    font-weight: 600;
    color: var(--green);
  }
  .error-text {
    color: var(--red);
  }
  /* (links as the About card's) */
  .newer a,
  .status .link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--mauve);
    font-weight: 600;
    text-decoration: none;
  }
  .status .link {
    min-height: 0;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  /* A checkbox beside its label and hint (like the other settings). */
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
  .go {
    justify-self: start;
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
</style>
