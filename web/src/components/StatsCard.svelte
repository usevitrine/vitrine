<script lang="ts">
  // Settings > Stats: the library, downloads, storage and the server at a
  // glance - read when the section opens and again every half minute while
  // it's on screen.
  import { onMount } from 'svelte';
  import { reveal, moved } from '../lib/motion';
  import { api, type LibraryStats } from '../lib/api';
  import { formatSize } from '../lib/results.svelte';
  import { siteLabel } from '../lib/sites.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { copyText } from '../lib/clipboard';
  import SiteIcon from './SiteIcon.svelte';
  import Icon from './Icon.svelte';
  import Rich from './Rich.svelte';
  import { t, num, date } from '../lib/i18n.svelte';

  const DAYS = 30;
  let st = $state<LibraryStats | null>(null);

  async function load() {
    try {
      st = await api.stats(DAYS);
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  onMount(() => {
    load();
    const timer = setInterval(() => document.visibilityState === 'visible' && load(), 30_000);
    return () => clearInterval(timer);
  });

  const n = num;
  const kindLabel = (k: 'image' | 'gif' | 'video') => t(`stats.kind.${k}`);
  const kindColor = { image: 'var(--blue)', gif: 'var(--peach)', video: 'var(--mauve)' } as const;
  const diskLabel = (d: 'files' | 'data' | 'both') => t(`stats.disk.${d}.label`);
  const diskHint = (d: 'files' | 'data' | 'both') => t(`stats.disk.${d}.hint`);

  /** "3 days 4 h", "5 h 12 min", "12 min". */
  function duration(s: number): string {
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    if (d) return t('stats.uptime.days', { n: d, h });
    if (h) return t('stats.uptime.hours', { h, m });
    return t('stats.uptime.minutes', { m: Math.max(1, m) });
  }
  const day = (offset: number) => {
    const d = new Date();
    d.setDate(d.getDate() - offset);
    return date(d, { day: 'numeric', month: 'short' });
  };

  // ---- The problem report: made on the server, private values replaced.
  let report = $state('');
  let showReport = $state(false);
  let making = $state(false);
  async function getReport(): Promise<string> {
    making = true;
    try {
      report = await api.report();
      return report;
    } finally {
      making = false;
    }
  }
  async function reportAction(what: 'show' | 'copy' | 'download') {
    if (what === 'show' && showReport) {
      showReport = false;
      return;
    }
    let text: string;
    try {
      text = await getReport();
    } catch (e) {
      toasts.error((e as Error).message);
      return;
    }
    if (what === 'show') showReport = true;
    else if (what === 'copy') copyText(text, t('stats.report.what'));
    else {
      const a = document.createElement('a');
      a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
      a.download = `vitrine-report-${new Date().toISOString().slice(0, 10)}.txt`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
    }
  }

  // The imports chart: the day under the pointer is read out above it.
  let pick = $state<number | null>(null);
  let importsTotal = $derived(st ? st.imports.reduce((a, b) => a + b, 0) : 0);
  let importsMax = $derived(st ? Math.max(1, ...st.imports) : 1);
</script>

{#if !st}
  <section class="card" aria-busy="true">
    <div class="card-head"><h2>{t('settings.section.stats.label')}</h2></div>
    <p class="hint">{t('rankings.counting')}</p>
  </section>
{:else}
  <section class="card" transition:reveal>
    <div class="card-head"><h2>{t('settings.section.library.label')}</h2></div>
    <div class="tiles">
      <div class="tile"><b>{n(st.files)}</b><span>{t('stats.tile.files', { n: st.files })}</span></div>
      <div class="tile"><b>{formatSize(st.bytes)}</b><span>{t('stats.tile.inAll')}</span></div>
      <div class="tile"><b>{n(st.tags)}</b><span>{t('stats.tile.tags', { n: st.tags })}</span></div>
    </div>
    {#if st.files > 0}
      <div class="kinds">
        <div class="bar" role="img" aria-label={st.kinds.map((k) => `${kindLabel(k.kind)}: ${n(k.files)}`).join(', ')}>
          {#each st.kinds as k (k.kind)}
            <span style:flex-grow={k.files} style:background={kindColor[k.kind]}></span>
          {/each}
        </div>
        <ul class="legend">
          {#each st.kinds as k (k.kind)}
            <li>
              <span class="dot" style:background={kindColor[k.kind]}></span>
              <span class="what">{kindLabel(k.kind)}</span>
              <span class="num">{n(k.files)}</span>
              <span class="size">{formatSize(k.bytes)}</span>
            </li>
          {/each}
        </ul>
      </div>
    {/if}
    <dl class="rows">
      <div><dt>{t('stats.favorites')}</dt><dd>{n(st.favorites)}</dd></div>
      <div><dt>{t('stats.tagLinks')}</dt><dd>{n(st.tag_links)}{#if st.user_links}<small>&nbsp;· {t('stats.yours', { n: st.user_links })}</small>{/if}</dd></div>
      <div><dt>{t('stats.relations')}</dt><dd>{n(st.parents)} · {n(st.siblings)}</dd></div>
      <div><dt>{t('stats.untagged')}</dt><dd>{n(st.untagged)}</dd></div>
      <div><dt>{t('stats.trash')}</dt><dd>{n(st.trash.files)}{#if st.trash.files}<small>&nbsp;· {formatSize(st.trash.bytes)}</small>{/if}</dd></div>
      <div><dt>{t('stats.removed')}</dt><dd>{n(st.removed)}<small>&nbsp;· {t('stats.removedHint')}</small></dd></div>
    </dl>
  </section>

  <section class="card">
    <div class="card-head">
      <h2>{t('stats.imports')}</h2>
      <p class="hint readout" aria-live="polite">
        {#if pick !== null}
          <Rich text={t('rankings.onDay', { n: st.imports[pick], day: day(DAYS - 1 - pick) })} />
        {:else}
          <Rich text={t('stats.lastDays', { n: importsTotal, days: DAYS })} />{#if st.since}&nbsp;· {t('stats.first', { date: date(st.since * 1000, { day: 'numeric', month: 'short', year: 'numeric' }) })}{/if}
        {/if}
      </p>
    </div>
    <div class="chart" role="img" aria-label={t('stats.chart', { days: DAYS })} onpointerleave={() => (pick = null)}>
      {#each st.imports as c, i (i)}
        <span class="col" class:on={pick === i} onpointerenter={() => (pick = i)} role="presentation">
          <span class="fill" class:none={!c} style:height="{c ? Math.max(4, (c / importsMax) * 100) : 0}%"></span>
        </span>
      {/each}
    </div>
    <div class="axis"><span>{day(DAYS - 1)}</span><span>{t('stats.today')}</span></div>
  </section>

  <section class="card">
    <div class="card-head"><h2>{t('stats.downloads')}</h2></div>
    {#if st.sites.length}
      <ul class="sites" transition:reveal>
        {#each st.sites as s (s.site)}
          <li animate:moved transition:reveal>
            <SiteIcon host={siteLabel(s.site)} />
            <span class="what">{siteLabel(s.site)}</span>
            <span class="num">{n(s.files)} <small>{t('stats.tile.files', { n: s.files })}</small></span>
            <span class="size">{n(s.posts)} <small>{t('stats.postsSeen', { n: s.posts })}</small></span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="hint" transition:reveal>{t('stats.nothingDownloaded')}</p>
    {/if}
    <dl class="rows">
      <div>
        <dt>{t('stats.subscriptions')}</dt>
        <dd>
          {n(st.subscriptions.total)}{#if st.subscriptions.paused}<small>&nbsp;· {t('stats.paused', { n: st.subscriptions.paused })}</small>{/if}{#if st.subscriptions.failing}<small class="bad">&nbsp;· {t('stats.failing', { n: st.subscriptions.failing })}</small>{/if}
        </dd>
      </div>
    </dl>
  </section>

  <section class="card">
    <div class="card-head"><h2>{t('stats.storage')}</h2></div>
    {#each st.storage.disks as d (d.for)}
      {@const used = d.total - d.free}
      <div class="disk">
        <div class="disk-head">
          <span class="what">{diskLabel(d.for)} <small>{diskHint(d.for)}</small></span>
          <span class="num">{formatSize(d.free)} <small>{t('stats.freeOf', { total: formatSize(d.total) })}</small></span>
        </div>
        <div class="bar meter" class:low={d.free < d.total * 0.1} role="meter" aria-label={t('stats.used', { disk: diskLabel(d.for) })} aria-valuemin={0} aria-valuemax={d.total} aria-valuenow={used}>
          <span style:width="{(used / Math.max(1, d.total)) * 100}%"></span>
        </div>
      </div>
    {/each}
    <dl class="rows">
      <div><dt>{t('stats.originals')}</dt><dd>{formatSize(st.storage.originals)}</dd></div>
      <div><dt>{t('stats.database')}</dt><dd>{formatSize(st.storage.database)}</dd></div>
      <div><dt>{t('stats.thumbnails')}</dt><dd>{formatSize(st.storage.thumbs)}</dd></div>
      <div><dt>{t('stats.previews')}</dt><dd>{formatSize(st.storage.previews)}<small>&nbsp;· {t('stats.previewsHint')}</small></dd></div>
      <div><dt>{t('backups.title')}</dt><dd>{formatSize(st.storage.backups)}</dd></div>
    </dl>
  </section>

  <section class="card">
    <div class="card-head"><h2>{t('stats.server')}</h2></div>
    <dl class="rows">
      <div><dt>{t('stats.version')}</dt><dd>{st.server.version}</dd></div>
      <div><dt>{t('stats.uptime.label')}</dt><dd>{duration(st.server.uptime_s)}</dd></div>
      <div><dt>{t('stats.memory')}</dt><dd>{formatSize(st.server.memory)}<small>&nbsp;· {t('stats.inUse', { size: formatSize(st.server.heap) })}</small></dd></div>
      <div><dt>{t('stats.system')}</dt><dd>{st.server.os}<small>&nbsp;· {t('stats.cpus', { n: st.server.cpus })}{st.server.in_docker ? ' · Docker' : ''}</small></dd></div>
      <div><dt>ffmpeg</dt><dd class:bad={!st.server.ffmpeg}>{st.server.ffmpeg || t('stats.notFound')}</dd></div>
      <div><dt>libvips</dt><dd>{st.server.vips || t('stats.notFound')}{#if !st.server.vips}<small>&nbsp;· {t('stats.imagesUseFfmpeg')}</small>{/if}</dd></div>
      <div><dt>Go</dt><dd>{st.server.go.replace(/^go/, '')}</dd></div>
    </dl>
    <p class="hint">{t('stats.took', { ms: st.took_ms < 1 ? '<1' : num(Math.round(st.took_ms)) })}</p>
  </section>

  <section class="card">
    <div class="card-head">
      <h2>{t('stats.report.title')}</h2>
      <p class="hint">{t('stats.report.hint')}</p>
    </div>
    <div class="actions">
      <button onclick={() => reportAction('download')} disabled={making}><Icon name="download" size={16} /> {t('backups.download')}</button>
      <button class="quiet" onclick={() => reportAction('copy')} disabled={making}><Icon name="copy" size={16} /> {t('tagMenu.copy')}</button>
      <button class="quiet" onclick={() => reportAction('show')} disabled={making} aria-expanded={showReport}><Icon name={showReport ? 'eyeoff' : 'eye'} size={16} /> {showReport ? t('stats.report.hide') : t('stats.report.show')}</button>
    </div>
    {#if showReport}
      <pre class="report" aria-label={t('stats.report.label')} transition:reveal>{report}</pre>
    {/if}
  </section>
{/if}

<style>
  .tiles {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
  .tile {
    display: grid;
    gap: 2px;
    padding: 12px;
    border-radius: 10px;
    background: var(--base);
    min-width: 0;
    container-type: inline-size; /* (its number sized to it: "495.2 GB" whole on a narrow phone) */
  }
  .tile b {
    font-size: min(22px, 18cqi);
    font-weight: 650;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.01em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tile span {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .kinds {
    display: grid;
    gap: 10px;
  }
  .bar {
    display: flex;
    height: 8px;
    border-radius: 999px;
    overflow: hidden;
    background: var(--surface0);
    gap: 2px;
  }
  .bar > span {
    min-width: 4px;
  }
  .meter > span {
    background: var(--mauve);
    border-radius: 999px;
    transition: width 0.4s ease;
  }
  .meter.low > span {
    background: var(--red);
  }
  .legend,
  .sites {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
  }
  .legend li,
  .sites li {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto 5.5em;
    align-items: center;
    gap: 8px;
  }
  .sites li {
    grid-template-columns: auto minmax(0, 1fr) auto 8.5em;
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
  }
  .what {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .num,
  .size,
  dd {
    font-variant-numeric: tabular-nums;
    text-align: right;
    white-space: nowrap;
  }
  .size {
    color: var(--subtext);
  }
  small {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .bad,
  small.bad {
    color: var(--red);
  }
  .rows {
    margin: 0;
    display: grid;
  }
  .rows > div {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 12px;
    padding: 7px 0;
    border-top: 1px solid var(--surface0);
  }
  .rows > div:first-child {
    border-top: 0;
    padding-top: 0;
  }
  .rows > div:last-child {
    padding-bottom: 0;
  }
  dt {
    color: var(--subtext);
  }
  dd {
    margin: 0;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .readout :global(strong) {
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  .chart {
    display: flex;
    align-items: flex-end;
    height: 72px;
    gap: 3px;
  }
  .col {
    flex: 1;
    height: 100%;
    display: flex;
    align-items: flex-end;
  }
  .fill {
    width: 100%;
    border-radius: 3px 3px 1px 1px;
    background: color-mix(in srgb, var(--mauve) 70%, transparent);
    transition: height 0.4s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .fill.none {
    height: 2px !important;
    background: var(--surface0);
  }
  .col.on .fill {
    background: var(--mauve);
  }
  .col.on .fill.none {
    background: var(--surface2);
  }
  .axis {
    display: flex;
    justify-content: space-between;
    color: var(--overlay1);
    font-size: var(--font-small);
    margin-top: -6px;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .report {
    margin: 0;
    max-height: min(60vh, 520px);
    overflow: auto;
    padding: 12px;
    border-radius: 10px;
    background: var(--crust);
    border: 1px solid var(--surface0);
    font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    color: var(--subtext);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    tab-size: 2;
  }
  .disk {
    display: grid;
    gap: 8px;
  }
  /* A disk's name and hint, then its free space - on a line of its own when they don't fit side by side. */
  .disk-head {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: baseline;
    gap: 2px 12px;
  }
  .disk-head .what {
    min-width: 0;
  }
  .disk-head .num {
    margin-left: auto;
    white-space: nowrap;
  }
  .disk-head small {
    margin-left: 4px;
  }
  @media (max-width: 420px) {
    .disk-head .what small {
      display: none;
    }
  }
</style>
