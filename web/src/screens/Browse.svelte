<script lang="ts">
  // Comics > Browse (2026-10-22): a gallery site's own search (e-hentai, ExHentai) in the library's own search bar and
  // grid - the same tiles, hover zoom, rows and motion as the other pages (lib/browse: the site's galleries dressed as
  // comics, marked when the library has them or their download waits, page after page as the end comes near). A tile
  // opens its details in the app's details panel (GalleryPanel: like a comic's), with Download in the panel's header.
  import { onMount } from 'svelte';
  import { reveal, pop, glide, swap } from '../lib/motion';
  import { fade } from 'svelte/transition';
  import { cubicOut } from 'svelte/easing';
  import { api, type BrowseGallery } from '../lib/api';
  import { openSiteDownload } from '../lib/newDownload.svelte';
  import { browseScope, provideScope } from '../lib/scope.svelte';
  import type { BrowseResults } from '../lib/browse.svelte';
  import { siteLabel, sites as siteInfo } from '../lib/sites.svelte';
  import { router } from '../lib/router.svelte';
  import { topbar } from '../lib/topbar.svelte';
  import { t, num } from '../lib/i18n.svelte';
  import { namespaces } from '../lib/namespaces.svelte';
  import SearchBar from '../components/SearchBar.svelte';
  import Grid from '../components/Grid.svelte';
  import DetailsPanel from '../components/DetailsPanel.svelte';
  import GalleryPanel from '../components/GalleryPanel.svelte';
  import Icon from '../components/Icon.svelte';
  import SelectionBar from '../components/SelectionBar.svelte';

  provideScope(browseScope);
  const results = browseScope.results as BrowseResults;
  const selection = browseScope.selection;

  let sites = $state<string[]>([]);
  let sorts = $state<Record<string, string[]>>({}); // each site's other orders (its own lists: popular, toplists)
  let loaded = $state(false);
  let query = $state('');
  let headH = $state(0);
  let pad = $derived(topbar.h + headH);

  function lastSite(): string {
    try {
      return localStorage.getItem('vitrine.browseSite') ?? '';
    } catch {
      return '';
    }
  }
  function pickSite(id: string) {
    results.site = id;
    selection.ids.clear();
    try {
      localStorage.setItem('vitrine.browseSite', id);
    } catch {
      /* not remembered - fine */
    }
    results.search(query);
  }
  function search(q: string) {
    if (q.trim()) results.sort = ''; // (the site's own lists can't be searched: a search is its newest first)
    query = q;
    open = null;
    selection.ids.clear(); // (another search: other galleries - picking goes on)
    results.search(q);
  }
  // An order from the filter menu: one of the site's own lists clears the search (it has none).
  function pickSort(s: string) {
    results.sort = s === 'newest' ? '' : s;
    if (results.sort) query = '';
    open = null;
    selection.ids.clear();
    results.search(query);
  }

  onMount(() => () => selection.clear()); // (leaving Browse: nothing stays picked)
  onMount(async () => {
    topbar.browse = topControl;
    siteInfo.load();
    namespaces.loadComics();
    try {
      const res = await api.browseSites();
      sites = res.sites;
      sorts = res.sorts ?? {};
    } catch (e) {
      results.error = (e as Error).message;
    }
    loaded = true;
    if (!sites.length) return;
    results.site = sites.includes(lastSite()) ? lastSite() : sites[0];
    if (results.searches === 0) search('');
  });

  // The site's own tags while typing (not only those the library knows), grouped as a download will group them.
  async function suggestTags(word: string, signal: AbortSignal) {
    const res = await api.browseTags(results.site, word, signal);
    namespaces.addComics(res.suggestions.map((s) => (s.tag.includes(':') ? s.tag.slice(0, s.tag.indexOf(':')) : '')));
    return res;
  }

  // ---- a gallery's details
  let open = $state<BrowseGallery | null>(null);
  function details(i: number) {
    const id = results.idAt(i);
    open = (id === undefined ? undefined : results.galleries.get(id)) ?? null;
  }
  // Download: once, or a subscription (an ongoing gallery's newer versions come in) - the New download dialog.
  function download(g: BrowseGallery) {
    openSiteDownload(results.site, g.url, g.title, () => {
      results.queued(g.id);
      if (open?.id === g.id) open = { ...g, queued: true };
    });
  }
</script>

<!-- In the app's top bar: how many galleries - or that it's searching. (The site: the filter menu's From row.) -->
{#snippet topControl()}
  <span class="count" aria-live="polite">
    {#if results.busy}<span class="spinner small" aria-hidden="true"></span> {t('library.searching')}
    {:else if !results.error && results.searches}{results.comics === 1 ? t('browse.one') : t('browse.found', { n: num(results.comics) })}{/if}
  </span>
  {#if results.total > 0 && !selection.active}
    <!-- Several galleries at once: Select, then tap them (with a mouse: Ctrl/Shift-click works without it). -->
    <button class="icon" aria-label={t('common.select')} title={t('browse.selectHint')} onclick={() => (selection.picking = true)} in:fade={{ duration: 150 }}><Icon name="checks" /></button>
  {/if}
{/snippet}

<!-- The details panel's header: Download (or Read: in the library already). -->
{#snippet panelActions()}
  {#if open}
    {@const g = open}
    {#if g.comic}
      <button class="icon" aria-label={t('browse.read')} title={t('browse.read')} onclick={() => router.go('comics', String(g.comic))} transition:pop><Icon name="book" /></button>
    {:else if g.queued}
      <span class="icon-state" role="img" aria-label={t('browse.queued')} title={t('browse.queued')} transition:pop><Icon name="download" /></span>
    {:else}
      <button class="icon accent" aria-label={t('backups.download')} title={t('backups.download')} onclick={() => download(g)} transition:pop><Icon name="download" /></button>
    {/if}
  {/if}
{/snippet}

<div class="browse" style:--lib-pad="{pad}px">
  <header bind:offsetHeight={headH}>
    {#if sites.length}
      <div in:glide={{ y: -8, duration: 260, easing: cubicOut }}>
        <SearchBar
          {query}
          sort={results.sort || 'newest'}
          defaultSort="newest"
          sorts={sorts[results.site] ?? []}
          onsort={pickSort}
          suggest={suggestTags}
          browseSites={sites}
          site={results.site}
          onsite={pickSite}
          placeholder={siteInfo.hints[results.site] ?? t('browse.searchOn', { site: siteLabel(results.site) })} onsearch={search} />
      </div>
    {/if}
    {#if results.error}<div class="status" aria-live="polite" transition:reveal><span class="error">{results.error}</span></div>{/if}
  </header>
  <div class="grid">
    {#if loaded && !sites.length}
      <div class="empty-shelf" in:swap>
        <span class="badge"><Icon name="book" size={28} /></span>
        <p class="muted">{t('browse.noSites')}</p>
      </div>
    {:else if results.total === 0 && !results.busy && !results.error && results.searches > 0}
      <div class="empty" in:swap><p>{t('browse.none')}</p></div>
    {:else if results.total === 0 && results.busy}
      <div class="first-load" role="status" aria-label={t('library.searching')} in:swap><span class="spinner"></span></div>
    {:else}
      <Grid {pad} inset={topbar.h + headH} onopen={details} />
    {/if}
    {#if results.loadingMore}
      <!-- The site's next page on its way. -->
      <div class="more" role="status" aria-label={t('browse.loadingMore')} transition:fade={{ duration: 150 }}><span class="spinner small"></span></div>
    {/if}
  </div>
  {#if selection.active}<SelectionBar />{/if}
</div>

{#if open}
  <DetailsPanel title={t('fileMenu.details')} onclose={() => (open = null)} actions={panelActions} over>
    <GalleryPanel gallery={open} site={results.site} image={(u) => results.image(u)} onsearch={search} />
  </DetailsPanel>
{/if}

<style>
  .browse {
    height: 100%;
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr) auto; /* grid, selection bar */
  }
  .grid {
    position: relative;
    min-height: 0;
  }
  /* The search bar floats over the grid under the top bar, sliding away with it (as in the library and Comics). */
  header {
    padding: 8px 10px 8px;
    position: absolute;
    top: var(--bar-h, 0px);
    left: 0;
    right: 0;
    z-index: 5;
    background: var(--base);
    /* (and 2px up under the top bar: on screens with fractional pixels - Android - the two edges were rounded apart
       and the scrolling grid showed through a 1px line between them) */
    box-shadow: 0 -2px 0 var(--base);
  }
  :global(.bar-sliding) .browse header {
    transition: top 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.bar-sliding) .browse header {
      transition: none;
    }
  }
  @media (min-width: 900px), (pointer: fine) {
    header {
      padding: 10px 12px 10px;
    }
    .grid {
      padding: 0 12px;
      --grid-gutter: 12px;
    }
  }
  .count {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px;
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .status {
    margin: 6px 4px 0;
    font-size: var(--font-small);
  }
  .first-load {
    display: flex;
    justify-content: center;
    padding: calc(var(--lib-pad) + 64px) 16px 48px;
  }
  .more {
    position: absolute;
    left: 50%;
    bottom: calc(16px + env(safe-area-inset-bottom, 0px));
    translate: -50% 0;
    z-index: 6;
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 999px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    box-shadow: 0 8px 20px rgb(0 0 0 / 0.45);
  }
  .icon-state {
    display: grid;
    place-items: center;
    width: var(--control-h);
    height: var(--control-h);
    color: var(--peach);
  }
  .accent {
    color: var(--mauve);
  }
  .empty {
    margin: 0 10px;
    margin-top: calc(var(--lib-pad) + 12px); /* (its border below the search bar, not under it) */
    padding: 40px 16px;
    text-align: center;
    color: var(--subtext);
  }
  .empty-shelf {
    display: grid;
    justify-items: center;
    gap: 10px;
    padding: calc(var(--lib-pad) + 48px) 24px 48px;
    text-align: center;
  }
  .empty-shelf .badge {
    display: grid;
    place-items: center;
    width: 56px;
    height: 56px;
    border-radius: 16px;
    background: var(--surface0);
    color: var(--mauve);
  }
  .muted {
    margin: 0;
    color: var(--subtext);
  }
</style>
