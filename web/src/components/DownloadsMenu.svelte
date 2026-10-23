<script lang="ts">
  // The downloads in the top bar (the library): its counter says how many are running or waiting, and the arrow bobs
  // while one downloads. A press opens New download and the downloads going on now - running, then waiting, then
  // paused - each with its progress; one opens the Downloads page.
  import { cubicOut } from 'svelte/easing';
  import { flip } from 'svelte/animate';
  import { backClose } from '../lib/backClose';
  import { swallowPress, leaving } from '../lib/outside';
  import { activeDownloads as active } from '../lib/activeDownloads.svelte';
  import { live } from '../lib/live.svelte';
  import { measurable, progress } from '../lib/downloadProgress';
  import { openNewDownload } from '../lib/newDownload.svelte';
  import { sites as siteInfo } from '../lib/sites.svelte';
  import { router } from '../lib/router.svelte';
  import type { DownloadSearch } from '../lib/api';
  import { t, num } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';
  import SiteIcon from './SiteIcon.svelte';

  // query: the search in the bar (New download offers it first).
  let { query }: { query: string } = $props();

  const SHOWN = 6; // more: "N more", to the Downloads page

  let open = $state(false);
  let button = $state<HTMLButtonElement>();
  let panel = $state<HTMLElement>();

  $effect(() => active.use());

  const running = $derived(active.list.some((s) => shownState(s) === 'running'));

  function close() {
    open = false;
  }
  function newDownload() {
    close();
    openNewDownload(query);
  }
  function toDownloads() {
    close();
    router.go('downloads');
  }

  // (a running one on a paused site waits for it: shown as waiting, as on the Downloads page)
  const shownState = (s: DownloadSearch) => (s.state === 'running' && active.sitesPaused[s.site] ? 'queued' : s.state);
  const gallery = (s: DownloadSearch) => siteInfo.engines[s.site] === 'ehentai';
  const named = (s: DownloadSearch) => s.label || s.query || t('push.everything');
  const nameOf = (s: DownloadSearch) => (s.subscription_id ? t('downloads.checkOf', { query: named(s) }) : named(s));
  /** "Downloading · 12 files", "Waiting", "Paused". */
  function detail(s: DownloadSearch): string {
    const st = shownState(s);
    const files = s.downloaded + s.already;
    return st === 'running' && files ? `${t('downloads.state.running')} · ${t('common.files', { n: files })}` : t(`downloads.state.${st}`);
  }

  // A press outside closes it (the press goes no further: nothing opens by accident underneath).
  $effect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      const el = e.target as Node;
      if (panel?.contains(el) || button?.contains(el) || leaving(panel)) return;
      swallowPress(e);
      close();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) {
        e.preventDefault();
        close();
        button?.focus();
      }
    };
    window.addEventListener('pointerdown', down, true);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('pointerdown', down, true);
      window.removeEventListener('keydown', key);
    };
  });

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  // The panel grows out of the button (like the bell's); the downloads slide in one after another.
  function grow(node: Element, { out = false } = {}) {
    if (reduced()) return { duration: 0 };
    return {
      duration: out ? 200 : 240,
      easing: cubicOut,
      css: (k: number) => `opacity: ${k}; transform: translateY(${(1 - k) * -8}px) scale(${0.94 + 0.06 * k})`,
    };
  }
  function slideIn(node: Element, { i = 0 } = {}) {
    if (reduced()) return { duration: 0 };
    return { delay: 60 + Math.min(i, 8) * 28, duration: 260, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateX(${(1 - k) * 14}px)` };
  }
  function slideOut(node: Element) {
    if (reduced()) return { duration: 0 };
    return { duration: 180, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateX(${(1 - k) * 24}px)` };
  }
</script>

<div class="dl-wrap">
  <button
    bind:this={button}
    class="icon dl"
    class:has={active.count > 0}
    class:running
    aria-label={active.count ? t('dlMenu.labelActive', { n: active.count }) : t('dlMenu.label')}
    title={t('dlMenu.label')}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={() => (open = !open)}>
    <span class="arrow"><Icon name="download" size={22} /></span>
    {#if active.count}
      {#key active.count}
        <span class="counter" aria-hidden="true">{active.count > 99 ? '99+' : num(active.count)}</span>
      {/key}
    {/if}
  </button>

  {#if open}
    <div class="panel" bind:this={panel} role="dialog" aria-label={t('dlMenu.label')} {@attach backClose(close)} in:grow out:grow={{ out: true }}>
      <header>
        <h2>{t('dlMenu.label')}</h2>
        <button class="quiet link new" onclick={newDownload} aria-label={t('newDl.open')}><Icon name="plus" size={15} /> {t('dlMenu.new')}</button>
        <button class="quiet link" onclick={toDownloads}>{t('dlMenu.all')} <Icon name="right" size={15} /></button>
      </header>
      {#if active.list.length}
        <ul>
          {#each active.list.slice(0, SHOWN) as s, i (s.id)}
            {@const st = shownState(s)}
            <li animate:flip={{ duration: 220 }} in:slideIn={{ i }} out:slideOut>
              <button class="item" onclick={toDownloads}>
                <SiteIcon host={siteInfo.hosts[s.site] ?? ''} />
                <span class="text">
                  <span class="name">{nameOf(s)}</span>
                  <span class="meta">{detail(s)}</span>
                  {#if st === 'running' && !measurable(s, live.files)}
                    <span class="bar striped" role="progressbar" aria-label={t('downloads.noEstimate')}></span>
                  {:else}
                    <span class="bar"><span class="fill {st}" style:width="{progress(s, live.files, gallery(s)) * 100}%"></span></span>
                  {/if}
                </span>
              </button>
            </li>
          {/each}
        </ul>
        {#if active.list.length > SHOWN}
          <button class="quiet more" onclick={toDownloads}>{t('dlMenu.more', { n: active.list.length - SHOWN })}</button>
        {/if}
      {:else}
        <!-- (nothing: a quiet check, no words - like the bell's) -->
        <div class="empty" role="img" aria-label={t('dlMenu.empty')} in:slideIn><Icon name="check" size={22} /></div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .dl-wrap {
    position: relative;
  }
  .dl {
    position: relative;
    color: var(--subtext);
  }
  .dl.has {
    color: var(--text);
  }
  .arrow {
    display: grid;
  }
  /* Something downloading: the arrow bobs down, again and again. */
  .dl.running .arrow {
    animation: bob 1.6s ease-in-out infinite;
  }
  @keyframes bob {
    0%,
    100% {
      transform: translateY(-1px);
    }
    50% {
      transform: translateY(2px);
    }
  }
  .counter {
    position: absolute;
    top: 3px;
    right: 1px;
    min-width: 17px;
    height: 17px;
    padding: 0 4px;
    border-radius: 999px;
    background: var(--peach);
    color: var(--crust);
    font-size: 10px;
    font-weight: 800;
    line-height: 17px;
    text-align: center;
    box-shadow: 0 0 0 2px var(--mantle);
    animation: pop 0.38s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  @keyframes pop {
    from {
      transform: scale(0.4);
    }
  }
  .panel {
    position: absolute;
    top: calc(100% + 8px);
    right: -48px; /* (lined up with the bell's panel: the bell is right of this button) */
    z-index: 60;
    width: min(380px, calc(100vw - 16px));
    max-height: min(70vh, 560px);
    display: flex;
    flex-direction: column;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 18px 44px rgb(0 0 0 / 0.55);
    transform-origin: calc(100% - 70px) 0;
    overflow: hidden;
  }
  /* Phones: across the top bar, the same margin on both sides (the bar holds it, not the button). */
  @media (max-width: 639px) {
    .dl-wrap {
      position: static;
    }
    .panel {
      left: 8px;
      right: 8px;
      width: auto;
    }
  }
  header {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 44px;
    padding: 4px 6px 4px 14px;
    border-bottom: 1px solid var(--surface0);
  }
  h2 {
    flex: 1;
    margin: 0;
    font-size: 14px;
    font-weight: 700;
  }
  /* New download and All: small links in the heading. */
  .link {
    min-height: 32px;
    padding: 0 8px;
    border: 0;
    font-size: var(--font-small);
    color: var(--mauve);
    gap: 2px;
  }
  .link.new {
    gap: 4px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 4px;
    overflow-y: auto;
    overscroll-behavior: contain;
    display: grid;
    gap: 2px;
  }
  .item {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 0;
    height: auto;
    padding: 8px 10px 8px 8px;
    background: none;
    border: 0;
    border-radius: 10px;
    color: var(--text);
    font: inherit;
    text-align: left;
    font-weight: 400;
  }
  @media (hover: hover) {
    .item:hover {
      background: var(--surface0);
      filter: none;
    }
  }
  .text {
    min-width: 0;
    display: grid;
    gap: 3px;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }
  .meta {
    font-size: 11px;
    color: var(--overlay1);
  }
  .bar {
    display: block;
    height: 4px;
    margin-top: 2px;
    border-radius: 2px;
    background: var(--surface0);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    background: var(--peach);
    transition: width 0.4s;
  }
  .fill.paused,
  .fill.queued {
    background: var(--overlay0);
  }
  .bar.striped {
    background: repeating-linear-gradient(-45deg, var(--peach) 0 6px, color-mix(in srgb, var(--peach) 45%, var(--surface0)) 6px 12px);
    background-size: 17px 17px;
    animation: stripes 0.8s linear infinite;
  }
  @keyframes stripes {
    to {
      background-position: 17px 0;
    }
  }
  .more {
    margin: 0 8px 8px;
    min-height: 34px;
    border: 0;
    font-size: var(--font-small);
    color: var(--subtext);
  }
  .empty {
    display: grid;
    place-items: center;
    margin: 0;
    padding: 18px 16px;
    border: 0; /* (not the dashed box of an empty list elsewhere) */
    border-radius: 0;
    background: none;
    color: var(--overlay0);
  }
  @media (prefers-reduced-motion: reduce) {
    .dl.running .arrow,
    .counter,
    .bar.striped {
      animation: none;
    }
  }
</style>
