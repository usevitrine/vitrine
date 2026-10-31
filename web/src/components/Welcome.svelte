<script lang="ts">
  // The empty library's welcome: the ways to fill it (a download, more sites,
  // files you have), a note while the first download runs, and on iPhone
  // Safari the tip to add Vitrine to the Home Screen.
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import { router } from '../lib/router.svelte';
  import { sites } from '../lib/sites.svelte';
  import { live } from '../lib/live.svelte';
  import { pushSupport } from '../lib/push.svelte';
  import { toasts } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';
  import Rich from './Rich.svelte';
  import { t } from '../lib/i18n.svelte';

  let downloading = $state(0); // searches waiting or running
  const homeScreenTip = pushSupport() === 'needs-home-screen';
  const siteNames = $derived(Object.values(sites.names).sort());
  const importCmd = 'vitrine import /path/to/folder';

  async function check() {
    try {
      const res = await api.activeDownloads();
      downloading = res.searches.filter((s) => s.state === 'queued' || s.state === 'running').length;
    } catch {
      /* the welcome works without it */
    }
  }
  onMount(() => {
    check();
    return live.onDownloads(check);
  });

  function startDownload() {
    router.focus = 'new-download';
    router.go('downloads');
  }
  function openSites() {
    router.go('settings', 'sites');
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(importCmd);
      toasts.show(t('common.copied'), { cat: 'copied' });
    } catch {
      toasts.error(t('welcome.copyFailed'));
    }
  }
</script>

<div class="welcome">
  {#if downloading}
    <div class="running" role="status">
      <Icon name="download" size={18} />
      <span>{t('welcome.running')}</span>
      <button class="quiet" onclick={() => router.go('downloads')}>{t('welcome.seeProgress')}</button>
    </div>
  {/if}

  <div class="intro">
    <h2>{t('welcome.title')}</h2>
    <p class="muted">{t('welcome.empty')}</p>
  </div>

  <ol class="ways">
    <li>
      <span class="badge"><Icon name="download" size={20} /></span>
      <div class="text">
        <h3>{t('welcome.download.title')}</h3>
        <p class="muted">{t('welcome.download.body')}</p>
      </div>
      <button onclick={startDownload}>{t('welcome.download.button')}</button>
    </li>
    <li>
      <span class="badge"><Icon name="plus" size={20} /></span>
      <div class="text">
        <h3>{t('welcome.sites.title')}</h3>
        <p class="muted">
          {#if siteNames.length}{t('welcome.sites.have', { sites: siteNames.join(', ') })} {/if}{t('welcome.sites.body')}
        </p>
      </div>
      <button class="quiet" onclick={openSites}>{t('welcome.sites.button')}</button>
    </li>
    <li>
      <span class="badge"><Icon name="folder" size={20} /></span>
      <div class="text">
        <h3>{t('welcome.import.title')}</h3>
        <p class="muted">{t('welcome.import.body')}</p>
        <code>{importCmd}</code>
      </div>
      <button class="quiet" onclick={copy}>{t('welcome.import.button')}</button>
    </li>
  </ol>

  {#if homeScreenTip}
    <p class="tip">
      <Icon name="info" size={18} />
      <span><Rich text={t('welcome.homeScreen')} /></span>
    </p>
  {/if}
</div>

<style>
  .welcome {
    display: grid;
    gap: 16px;
    width: min(680px, 100%);
    margin: 8px auto 24px;
    padding: 0 12px;
  }
  .intro h2 {
    margin: 4px 0 2px;
    font-size: 22px;
  }
  .intro p,
  .text p {
    margin: 0;
  }
  .ways {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 10px;
  }
  .ways li {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 6px 14px;
    align-items: start;
    padding: 16px;
    border-radius: 14px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
  }
  .ways li > button {
    grid-column: 2;
    justify-self: start;
    margin-top: 6px;
  }
  @media (min-width: 640px) {
    .ways li {
      grid-template-columns: auto 1fr auto;
      align-items: center;
    }
    .ways li > button {
      grid-column: 3;
      margin-top: 0;
    }
  }
  .badge {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 12px;
    color: var(--mauve);
    background: color-mix(in srgb, var(--mauve) 14%, var(--surface0));
  }
  .text {
    display: grid;
    gap: 4px;
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--font);
  }
  code {
    justify-self: start;
    max-width: 100%;
    margin-top: 4px;
    padding: 4px 8px;
    border-radius: 8px;
    background: var(--crust);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: var(--font-small);
    overflow-wrap: anywhere;
  }
  .running,
  .tip {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0;
    padding: 12px 14px;
    border-radius: 12px;
    font-size: var(--font-small);
  }
  .running {
    flex-wrap: wrap;
    color: var(--green);
    background: color-mix(in srgb, var(--green) 10%, transparent);
  }
  .running span {
    flex: 1 1 14em;
  }
  .tip {
    color: var(--subtext);
    background: var(--mantle);
    border: 1px solid var(--surface0);
  }
  .running :global(svg),
  .tip :global(svg) {
    flex: none;
  }
</style>
