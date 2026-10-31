<script lang="ts">
  // Settings > Stats: the library, downloads, storage and the server at a
  // glance - read when the section opens and again every half minute while
  // it's on screen. (The problem report: ReportCard, in Help & about.)
  // Every number in a tile, two to a row (four on a wide screen), with a few
  // words under it; only the long versions are rows - never a number beside a
  // long hint, which ran off the card on phones.
  import { onMount } from 'svelte';
  import { reveal, moved } from '../lib/motion';
  import { api, type LibraryStats } from '../lib/api';
  import { formatSize } from '../lib/results.svelte';
  import { siteLabel } from '../lib/sites.svelte';
  import { toasts } from '../lib/toast.svelte';
  import SiteIcon from './SiteIcon.svelte';
  import Rich from './Rich.svelte';
  import { t, num, short, date } from '../lib/i18n.svelte';

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
  const shortDate = (ms: number) => date(new Date(ms), { day: 'numeric', month: 'short' });
  const day = (offset: number) => {
    const d = new Date();
    d.setDate(d.getDate() - offset);
    return date(d, { day: 'numeric', month: 'short' });
  };
  /** "VA-API (/dev/dri/renderD128)" -> the name, and the device under it. */
  function decoder(s = ''): [string, string] {
    const m = s.match(/^(.*?) \((.*)\)$/);
    return m ? [m[1], m[2]] : [s, ''];
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
  {@const s = st.server}
  {@const [dec, device] = decoder(s.hwaccel)}
  <section class="card" transition:reveal>
    <div class="card-head"><h2>{t('menu.library')}</h2></div>
    <div class="tiles">
      <div class="tile"><b>{n(st.files)}</b><span>{t('stats.tile.filesSize', { n: st.files, size: formatSize(st.bytes) })}</span></div>
      <div class="tile"><b>{n(st.tags)}</b><span>{t('stats.tile.tags', { n: st.tags })}</span></div>
    </div>
    {#if st.files > 0}
      <div class="bar" role="img" aria-label={st.kinds.map((k) => `${kindLabel(k.kind)}: ${n(k.files)}`).join(', ')}>
        {#each st.kinds as k (k.kind)}
          <span style:flex-grow={k.files} style:background={kindColor[k.kind]}></span>
        {/each}
      </div>
      <!-- Under the bar: the first kind at its left end, the last at its right, one between in the middle. -->
      <ul class="legend">
        {#each st.kinds as k (k.kind)}
          <li>
            <span class="what"><span class="dot" style:background={kindColor[k.kind]}></span>{kindLabel(k.kind)}</span>
            <span class="num">{n(k.files)}</span>
            <span class="size">{formatSize(k.bytes)}</span>
          </li>
        {/each}
      </ul>
    {/if}
    <div class="tiles three">
      <div class="tile"><b>{short(st.tag_links)}</b><span>{st.user_links ? t('stats.tile.tagLinksYours', { n: n(st.user_links) }) : t('stats.tile.tagLinks')}</span></div>
      <div class="tile"><b>{n(st.favorites)}</b><span>{t('stats.tile.favorites')}</span></div>
      <div class="tile"><b>{n(st.trash.files)}</b><span>{st.trash.files ? t('stats.tile.trashSize', { size: formatSize(st.trash.bytes) }) : t('stats.tile.trash')}</span></div>
      <div class="tile"><b>{n(st.removed)}</b><span>{t('stats.tile.removed')}</span></div>
      <div class="tile"><b>{n(st.untagged)}</b><span>{t('stats.tile.untagged')}</span></div>
      <div class="tile"><b>{n(st.parents)} · {n(st.siblings)}</b><span>{t('stats.tile.relations')}</span></div>
    </div>
    {#if st.health}
      {@const h = st.health}
      {@const bad = !!(h.missing_count || h.damaged_count || h.db_problem)}
      <div class="tile state">
        <div class="said">
          <span class="name">{t('stats.health.label')}</span>
          {#if bad}
            <span class="bad">{[h.missing_count && t('stats.health.missing', { n: h.missing_count }), h.damaged_count && t('stats.health.damaged', { n: h.damaged_count }), h.db_problem && t('stats.health.database')].filter(Boolean).join(' · ')}</span>
          {:else if h.db_checked_at}
            <span>{h.cursor || !h.last_pass_at ? t('stats.health.reading', { pct: Math.min(99, Math.floor((h.verified / Math.max(1, st.files)) * 100)) }) : t('stats.health.allRead', { date: shortDate(h.last_pass_at) })}</span>
          {/if}
        </div>
        <span class="pill" class:ok={h.db_checked_at && !bad} class:red={bad}>{!h.db_checked_at ? t('stats.health.never') : bad ? t('stats.health.problems') : t('stats.health.fine')}</span>
      </div>
    {/if}
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
    {#if !st.sites.length}
      <p class="hint" transition:reveal>{t('stats.nothingDownloaded')}</p>
    {/if}
    <div class="tiles four">
      {#each st.sites as s (s.site)}
        <div class="tile" animate:moved transition:reveal>
          <span class="site"><SiteIcon host={siteLabel(s.site)} /><span class="name">{siteLabel(s.site)}</span></span>
          <b>{short(s.files)}</b>
          <span>{t('stats.tile.filesSeen', { n: s.files, seen: short(s.posts) })}</span>
        </div>
      {/each}
      <div class="tile full">
        <b>{n(st.subscriptions.total)}</b>
        <span>
          {t('stats.tile.subscriptions', { n: st.subscriptions.total })}{#if st.subscriptions.paused}&nbsp;· {t('stats.paused', { n: st.subscriptions.paused })}{/if}{#if st.subscriptions.failing}&nbsp;· <span class="bad">{t('stats.failing', { n: st.subscriptions.failing })}</span>{/if}
        </span>
      </div>
    </div>
  </section>

  <section class="card">
    <div class="card-head"><h2>{t('stats.storage')}</h2></div>
    {#each st.storage.disks as d (d.for)}
      {@const used = d.total - d.free}
      <div class="disk">
        <div class="disk-head"><span class="what">{diskLabel(d.for)}</span><span class="num">{t('stats.free', { size: formatSize(d.free) })}</span></div>
        <div class="bar meter" class:low={d.free < d.total * 0.1} role="meter" aria-label={t('stats.used', { disk: diskLabel(d.for) })} aria-valuemin={0} aria-valuemax={d.total} aria-valuenow={used}>
          <span style:width="{(used / Math.max(1, d.total)) * 100}%"></span>
        </div>
        <span class="sub">{t('stats.ofTotal', { total: formatSize(d.total), what: diskHint(d.for) })}</span>
      </div>
    {/each}
    <div class="tiles five">
      <div class="tile"><b>{formatSize(st.storage.originals)}</b><span>{t('stats.tile.originals')}</span></div>
      <div class="tile"><b>{formatSize(st.storage.thumbs)}</b><span>{t('stats.tile.thumbnails')}</span></div>
      <div class="tile"><b>{formatSize(st.storage.previews)}</b><span>{t('stats.tile.previews')}</span></div>
      <div class="tile"><b>{formatSize(st.storage.database)}</b><span>{t('stats.tile.database')}</span></div>
      <div class="tile"><b>{formatSize(st.storage.backups)}</b><span>{t('stats.tile.backups')}</span></div>
    </div>
  </section>

  <section class="card">
    <div class="card-head"><h2>{t('stats.server')}</h2></div>
    <div class="tiles four">
      <div class="tile"><b>{s.version}</b><span>{t('stats.tile.version')}</span></div>
      <div class="tile"><b>{duration(s.uptime_s)}</b><span>{t('stats.tile.uptime')}</span></div>
      <div class="tile"><b>{formatSize(s.memory)}</b><span>{t('stats.tile.memory', { size: formatSize(s.heap) })}</span></div>
      <div class="tile"><b>{t('stats.cpus', { n: s.cpus })}</b><span>{s.os}{s.in_docker ? ' · Docker' : ''}</span></div>
    </div>
    <dl class="rows">
      <div>
        <dt>{t('stats.hwaccel')}{#if device || !s.hwaccel}<small>{device || t('stats.hwaccelNoneHint')}</small>{/if}</dt>
        <dd>{s.hwaccel ? dec : t('stats.hwaccelNone')}</dd>
      </div>
      <div><dt>ffmpeg</dt><dd class:bad={!s.ffmpeg}>{s.ffmpeg || t('stats.notFound')}</dd></div>
      <div><dt>libvips{#if !s.vips}<small>{t('stats.imagesUseFfmpeg')}</small>{/if}</dt><dd>{s.vips || t('stats.notFound')}</dd></div>
      <div><dt>Go</dt><dd>{s.go.replace(/^go/, '')}</dd></div>
    </dl>
  </section>
{/if}

<style>
  .tiles {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }
  /* Four to a row where the card has room (Settings' column on a computer). */
  @media (min-width: 720px) {
    .tiles.four {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
    .tiles.three {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .tiles.five {
      grid-template-columns: repeat(5, minmax(0, 1fr));
    }
    .tiles.five > .tile {
      grid-column: auto !important;
    }
  }
  .tile {
    display: grid;
    align-content: start;
    gap: 1px;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--base);
    min-width: 0;
  }
  .tile b {
    font-size: 20px;
    font-weight: 650;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.01em;
    overflow-wrap: anywhere;
  }
  .tile > span,
  .sub,
  small {
    color: var(--subtext);
    font-size: var(--font-small);
    overflow-wrap: anywhere;
  }
  .tile.full,
  .tiles:not(.four, .three) > .tile:last-child:nth-child(odd) {
    grid-column: 1 / -1;
  }
  .site {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    margin-bottom: 4px;
  }
  .site .name {
    min-width: 0;
    color: var(--subtext);
    font-size: var(--font-small);
    overflow-wrap: anywhere;
  }
  /* The health check: what it found, and its state as a pill. */
  .tile.state {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .said {
    display: grid;
    gap: 1px;
    min-width: 0;
    flex: 1;
  }
  .said .name {
    font-weight: 600;
  }
  .said > span:not(.name) {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .pill {
    flex: none;
    padding: 2px 9px;
    border-radius: 999px;
    font-size: var(--font-small);
    font-weight: 600;
    white-space: nowrap;
    background: var(--surface0);
    color: var(--subtext);
  }
  .pill.ok {
    background: color-mix(in srgb, var(--green) 16%, transparent);
    color: var(--green);
  }
  .pill.red {
    background: color-mix(in srgb, var(--red) 16%, transparent);
    color: var(--red);
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
  .legend {
    list-style: none;
    margin: -4px 0 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
  .legend li {
    display: grid;
    gap: 1px;
    min-width: 0;
    align-content: start;
  }
  .legend li:nth-child(2):not(:last-child) {
    justify-items: center;
    text-align: center;
  }
  .legend li:last-child:not(:first-child) {
    grid-column: 3;
    justify-items: end;
    text-align: right;
  }
  .legend .what {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    flex: none;
  }
  .num,
  .size,
  dd {
    font-variant-numeric: tabular-nums;
  }
  .size {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .bad {
    color: var(--red);
  }
  /* Rows for what's long (versions, the decoder): its name and any hint on the left, the value
     on the right, in the middle of the two lines. */
  .rows {
    margin: 0;
    display: grid;
  }
  .rows > div {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 8px 0;
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
    display: grid;
    gap: 1px;
    min-width: 0;
    color: var(--subtext);
  }
  dd {
    margin: 0;
    min-width: 0;
    text-align: right;
    overflow-wrap: anywhere;
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
  .disk {
    display: grid;
    gap: 6px;
  }
  .disk-head {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 12px;
  }
  .disk-head .what {
    min-width: 0;
    color: var(--subtext);
  }
  .disk-head .num {
    white-space: nowrap;
  }
</style>
