<script lang="ts">
  // Rankings: the viewer's "I came" button counted (opt-in, Settings >
  // Viewing) - for a week, month, year or all time: the total, the days, a
  // streak, the most counted files, creators, characters, series and tags. A file opens its
  // ranking in the library (sorted by count); a creator or tag, its search.
  import { api, type Rankings, type FileInfo } from '../lib/api';
  import { router } from '../lib/router.svelte';
  import { overlayScroll } from '../lib/overlayScroll';
  import { splitTag, nsColor } from '../lib/namespaces.svelte';
  import { toasts } from '../lib/toast.svelte';
  import Icon from '../components/Icon.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import Rich from '../components/Rich.svelte';
  import { t, num, date } from '../lib/i18n.svelte';

  const periods = ['week', 'month', 'year', 'all'];
  let period = $state(loadPeriod());
  let data = $state<Rankings | null>(null);
  let files = $state<Map<number, FileInfo>>(new Map());

  function loadPeriod(): string {
    try {
      return localStorage.getItem('vitrine.rankingsPeriod') || 'week';
    } catch {
      return 'week';
    }
  }

  /** A comic opened on the page counted (?page= is 1-based). */
  function openPage(comic: number, page: number) {
    router.go('comics', String(comic));
    router.setParams({ page: String(page + 1) });
  }

  async function load(p: string) {
    try {
      const d = await api.rankings(p);
      if (p !== period) return;
      data = d;
      const want = [...d.files.map((f) => f.id), ...(d.pages ?? []).map((p) => p.file)].filter((id) => !files.has(id));
      if (want.length) {
        const res = await api.files(want);
        const next = new Map(files);
        for (const f of res.files) next.set(f.id, f);
        files = next;
      }
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  $effect(() => {
    const p = period;
    try {
      localStorage.setItem('vitrine.rankingsPeriod', p);
    } catch {
      /* not remembered */
    }
    load(p);
  });

  const n = num;
  let maxDay = $derived(data ? Math.max(1, ...data.days) : 1);
  let pick = $state<number | null>(null);
  const dayLabel = (i: number) => {
    if (!data) return '';
    const d = new Date();
    d.setDate(d.getDate() - (data.days.length - 1 - i));
    return date(d, { day: 'numeric', month: 'short', year: data.days.length > 60 ? 'numeric' : undefined });
  };
  const search = (q: string) => router.openSearch(q);
  async function clearAll() {
    const ok = await confirmAction({
      title: t('rankings.clearTitle'),
      body: t('rankings.clearBody'),
      action: t('rankings.clearAll'),
      icon: 'trash',
    });
    if (!ok) return;
    try {
      const { removed } = await api.clearClimaxes();
      toasts.show(t('rankings.cleared', { n: removed }), { cat: 'tagEdits', kind: 'removed' });
      files = new Map();
      await load(period);
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  /** "20 Oct" this year, "20 Oct 2025" before. */
  function firstDate(ms: number): string {
    const d = new Date(ms);
    return date(d, { day: 'numeric', month: 'short', year: d.getFullYear() === new Date().getFullYear() ? undefined : 'numeric' });
  }
  const tagName = (tag: string) => splitTag(tag).name.replace(/_/g, ' ');
</script>

<div class="page" {@attach overlayScroll}>
  <div class="page-inner">
    <div class="title-row">
      <h1>{t('rankings.title')}</h1>
      <div class="seg" role="radiogroup" aria-label={t('rankings.period')}>
        {#each periods as p (p)}
          <button role="radio" aria-checked={period === p} class:on={period === p} onclick={() => (period = p)}>{t(`rankings.periods.${p}`)}</button>
        {/each}
      </div>
    </div>

    {#if !data}
      <p class="hint">{t('rankings.counting')}</p>
    {:else if data.first === 0}
      <section class="card empty">
        <Icon name="drop" size={28} />
        <p><strong>{t('rankings.none')}</strong></p>
        <p class="hint">{t('rankings.noneHint')}</p>
      </section>
    {:else}
      <section class="card">
        <div class="tiles">
          <div class="tile"><b>{n(data.total)}</b><span>{t(`rankings.in.${period}`)}</span></div>
          <div class="tile"><b>{n(data.streak)}</b><span>{t('rankings.streak', { n: data.streak })}</span></div>
          <div class="tile">
            <b>{firstDate(data.first)}</b><span>{t('rankings.first')}</span>
          </div>
        </div>
        <p class="hint readout" aria-live="polite">
          {#if pick !== null}<Rich text={t('rankings.onDay', { n: data.days[pick], day: dayLabel(pick) })} />{:else}{period === 'all' ? t('rankings.perDayYear') : t('rankings.perDay')}{/if}
        </p>
        <div class="chart" class:dense={data.days.length > 60} role="img" aria-label={t('rankings.perDay')} onpointerleave={() => (pick = null)}>
          {#each data.days as c, i (i)}
            <span class="col" class:on={pick === i} onpointerenter={() => (pick = i)} role="presentation">
              <span class="fill" class:none={!c} style:height="{c ? Math.max(6, (c / maxDay) * 100) : 0}%"></span>
            </span>
          {/each}
        </div>
      </section>

      {#if data.files.length}
        <section class="card">
          <div class="head-row">
            <h2>{t('rankings.files')}</h2>
            <button class="quiet" onclick={() => search('system:came>0 sort:came')}>{t('rankings.seeAll')}</button>
          </div>
          <ol class="files">
            {#each data.files as f, i (f.id)}
              {@const info = files.get(f.id)}
              <li>
                <button class="file" onclick={() => search('system:came>0 sort:came')} aria-label={t('rankings.fileLabel', { rank: i + 1, n: f.count })}>
                  <span class="thumb" style:background-image={info?.thumb ? `url(${info.thumb})` : undefined}></span>
                  <span class="rank">{i + 1}</span>
                  <span class="cnt">{n(f.count)}</span>
                </button>
              </li>
            {/each}
          </ol>
        </section>
      {/if}

      {#if data.pages?.length}
        <!-- The comic reader's presses: the exact page, opening the comic there. -->
        <section class="card">
          <div class="card-head"><h2>{t('rankings.pages')}</h2></div>
          <ol class="files">
            {#each data.pages as pg, i (pg.comic + ':' + pg.page)}
              {@const info = files.get(pg.file)}
              <li>
                <button class="file page" onclick={() => openPage(pg.comic, pg.page)} aria-label={t('rankings.pageLabel', { rank: i + 1, n: pg.count, title: pg.title, page: pg.page + 1 })} title={pg.title}>
                  <span class="thumb" style:background-image={info?.thumb ? `url(${info.thumb})` : undefined}></span>
                  <span class="rank">{i + 1}</span>
                  <span class="cnt">{n(pg.count)}</span>
                  <span class="page-of"><span class="page-title">{pg.title}</span><span class="page-n">{t('rankings.pageN', { page: pg.page + 1 })}</span></span>
                </button>
              </li>
            {/each}
          </ol>
        </section>
      {/if}

      {#each [{ id: 'creators', list: data.creators }, { id: 'characters', list: data.characters ?? [] }, { id: 'series', list: data.series ?? [] }, { id: 'tags', list: data.tags }] as group (group.id)}
        {#if group.list.length}
          {@const top = Math.max(1, ...group.list.map((row) => row.count))}
          <section class="card">
            <div class="card-head"><h2>{t(`rankings.group.${group.id}`)}</h2></div>
            <ol class="bars">
              {#each group.list as row, i (row.tag)}
                <li>
                  <button class="bar-row" onclick={() => search(row.tag)}>
                    <span class="rank-n">{i + 1}</span>
                    <span class="name" style:color={nsColor(splitTag(row.tag).ns)}>{tagName(row.tag)}</span>
                    <span class="bar"><span style:width="{(row.count / top) * 100}%"></span></span>
                    <span class="cnt-n">{n(row.count)}</span>
                  </button>
                </li>
              {/each}
            </ol>
          </section>
        {/if}
      {/each}
      <div class="clear-row">
        <button class="quiet danger" onclick={clearAll}><Icon name="trash" size={16} /> {t('rankings.clearAll')}</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .title-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  /* The period: a segmented control. */
  .seg {
    display: inline-flex;
    padding: 3px;
    gap: 2px;
    border-radius: 999px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
  }
  .seg button {
    min-height: calc(var(--control-h) - 8px);
    padding: 0 12px;
    border-radius: 999px;
    background: transparent;
    color: var(--subtext);
    font-weight: 600;
  }
  .seg button.on {
    background: var(--surface0);
    color: var(--text);
  }
  @media (hover: hover) {
    .seg button:not(.on):hover {
      color: var(--text);
    }
  }
  .clear-row {
    display: flex;
    justify-content: center;
    padding-top: 4px;
  }
  .head-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .empty {
    justify-items: center;
    text-align: center;
    color: var(--subtext);
  }
  .empty p {
    margin: 0;
  }
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
  }
  .tile b {
    font-size: 20px;
    font-weight: 650;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tile span {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .readout :global(strong) {
    color: var(--text);
  }
  .chart {
    display: flex;
    align-items: flex-end;
    height: 72px;
    gap: 3px;
    margin-top: -6px;
  }
  .chart.dense {
    gap: 0;
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
  }
  .dense .fill {
    border-radius: 0;
  }
  .fill.none {
    height: 2px !important;
    background: var(--surface0);
  }
  .col.on .fill {
    background: var(--mauve);
  }
  /* The most counted files: thumbnails with their place and count. */
  .files {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: 6px;
  }
  .file {
    position: relative;
    display: block;
    width: 100%;
    aspect-ratio: 1;
    padding: 0;
    border-radius: 8px;
    overflow: hidden;
    background: var(--surface0);
  }
  .thumb {
    position: absolute;
    inset: 0;
    background: center / cover no-repeat;
  }
  .rank,
  .cnt {
    position: absolute;
    top: 4px;
    min-width: 20px;
    height: 20px;
    padding: 0 6px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 800;
    line-height: 20px;
    text-align: center;
  }
  .rank {
    left: 4px;
    background: rgb(17 17 27 / 0.75);
    color: var(--text);
  }
  .cnt {
    right: 4px;
    background: var(--mauve);
    color: var(--crust);
  }
  /* A comic page: its comic and page number along the bottom. */
  .file.page {
    aspect-ratio: 3 / 4;
  }
  .page-of {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    display: grid;
    padding: 14px 6px 5px;
    background: linear-gradient(to top, rgb(17 17 27 / 0.85), transparent);
    color: var(--text);
    font-size: 11px;
    line-height: 1.3;
    text-align: left;
  }
  .page-title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 700;
  }
  .page-n {
    color: var(--subtext);
  }
  .bars {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 2px;
  }
  .bar-row {
    width: 100%;
    display: grid;
    grid-template-columns: 1.6em minmax(0, 11em) minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    min-height: 34px;
    padding: 0 8px;
    border-radius: 8px;
    background: transparent;
    color: var(--text);
    font-weight: 500;
    text-align: left;
  }
  @media (hover: hover) {
    .bar-row:hover {
      background: var(--surface0);
    }
  }
  .rank-n,
  .cnt-n {
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
    font-size: var(--font-small);
  }
  .cnt-n {
    text-align: right;
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bar {
    height: 6px;
    border-radius: 999px;
    background: var(--surface0);
    overflow: hidden;
  }
  .bar > span {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: var(--mauve);
  }
  @media (max-width: 480px) {
    .bar-row {
      grid-template-columns: 1.6em minmax(0, 1fr) 4em auto;
    }
    .tile b {
      font-size: 16px;
    }
  }
</style>
