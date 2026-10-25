<script lang="ts">
  import { cubicOut } from 'svelte/easing';
  import { slide } from 'svelte/transition';
  import { reveal, swap } from '../lib/motion';
  import { live } from '../lib/live.svelte';
  import { connection } from '../lib/offline.svelte';
  import Welcome from '../components/Welcome.svelte';
  import { onMount, tick, untrack } from 'svelte';
  import Icon from '../components/Icon.svelte';
  import { fitContain, snap, hero, rectOf, reducedMotion } from '../lib/hero';
  import { results } from '../lib/results.svelte';
  import { router } from '../lib/router.svelte';
  import { settings } from '../lib/settings.svelte';
  import SearchBar from '../components/SearchBar.svelte';
  import Grid from '../components/Grid.svelte';
  import Viewer from '../components/Viewer.svelte';
  import SelectionBar from '../components/SelectionBar.svelte';
  import { selection } from '../lib/selection.svelte';
  import { extractSort } from '../lib/query';
  import { topbar } from '../lib/topbar.svelte';
  import { updateBadge } from '../lib/badge';
  import { api } from '../lib/api';
  import { saved } from '../lib/saved.svelte';
  import { t, short } from '../lib/i18n.svelte';
  import { trash, emptyTrash } from '../lib/trash.svelte';

  // The search and its order are kept apart: the order is in the sort menu and
  // the URL's ?sort= (only when it differs from the default sort setting).
  // The library has three pages: everything (/), Favorites (/favorites) and Trash (/trash) - the same screen, its
  // searches limited to favorites (no chip: the search bar holds only what was typed) or run on the deleted files
  // (lib/trash), each page with its own search, order and default order (Settings; the trash: the last deleted
  // first), kept in its URL.
  type Page = '' | 'favorites' | 'trash';
  const pageNow = (): Page => (router.screen === 'library' && (router.sub === 'favorites' || router.sub === 'trash') ? router.sub : '');
  let page = $state<Page>(pageNow());
  const fav = $derived(page === 'favorites');
  const inTrash = $derived(page === 'trash');
  results.scope = pageNow(); // (the first search's page)
  if (pageNow() === 'trash') trash.load();
  const defaultSort = () => (page === 'favorites' ? settings.favoritesSort : page === 'trash' ? 'modified' : settings.defaultSort);
  const fromNotification = router.param('new') === '1'; // a "new files" notification: everything, new first
  const fromURL = extractSort(router.param('q'));
  let query = $state(fromNotification ? '' : fromURL.q);
  let sort = $state(router.param('sort') || fromURL.sort || defaultSort());
  const sortParam = () => (sort === defaultSort() ? undefined : sort);
  // Read the open file from the URL now: the effect below rewrites those parameters.
  const urlIndex = router.param('i');
  const urlId = Number(router.param('f'));
  let viewing = $state<number | null>(null);
  let viewInfo = $state(false);
  let grid = $state<Grid>();
  // The search bar's height: with the top bar's, the room the grid keeps at its top (both float over it).
  let headH = $state(0);
  let pad = $derived(topbar.h + headH);
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

  // A search started from inside the viewer (a tag tapped) waits until closing
  // the viewer has stepped back in history, so the URL isn't overwritten.
  let pendingSearch: string | null = null;
  function onPop() {
    if (pendingSearch !== null) {
      const q = pendingSearch;
      pendingSearch = null;
      search(q);
    }
  }

  // The app opens on this device's copy of the settings; when the server's copy
  // turns out different (changed on another device), the first screen follows
  // it - unless a search or an order was picked meanwhile.
  async function search(q: string, since: number | null = null) {
    results.sinceOverride = since; // (another search: NEW since "Mark all as seen" again)
    if (viewing !== null && history.state?.viewer) {
      pendingSearch = q;
      viewing = null; // the viewer closes and steps back; onPop runs the search
      return;
    }
    // A sort typed into the search goes to the sort menu, not into the chips.
    const typed = extractSort(q);
    if (typed.sort) sort = typed.sort;
    query = typed.q;
    viewing = null;
    router.setParams({ q: query, sort: sortParam(), i: undefined, f: undefined });
    await results.search(query, sort);
  }

  // A search asked for by another screen (e.g. the blacklist's "Show them").
  $effect(() => {
    const q = router.pendingSearch;
    if (q === null) return;
    router.pendingSearch = null;
    const top = router.focus === 'library-top'; // a tapped notification: from the top
    if (top) router.focus = null;
    search(q).then(() => top && grid?.toTop(true));
  });

  // Switching between the library and Favorites (the menu, Back): the page's own
  // search and order come back from its URL; its default order otherwise.
  $effect(() => {
    const now = pageNow();
    if (now === page || router.screen !== 'library') return;
    untrack(() => {
      page = now;
      results.scope = now;
      if (now === 'trash') trash.load();
      viewing = null;
      selection.clear();
      const typed = extractSort(router.param('q'));
      query = typed.q;
      sort = router.param('sort') || typed.sort || defaultSort();
      results.search(query, sort);
    });
  });

  // A saved search or group tag from the menu: its search and order together;
  // its NEW files are those since it was last opened from there (and it's marked opened now).
  async function runSaved(q: string, s: string, key?: string) {
    let since: number | null = null;
    if (key) {
      try {
        since = (await api.seen(key)).previous || null;
      } catch {
        /* offline: the usual NEW files */
      }
    }
    sort = s;
    await search(q, since);
    saved.refreshNews();
  }

  function setSort(s: string) {
    sort = s;
    search(query);
  }

  // Keep the URL on the open file (position + ID), so a reload comes back to it.
  // Written once swiping pauses: Safari limits how often the address may change
  // (and each change costs time on a fast swipe).
  // Opening a file writes it at once; moving between files, once swiping pauses
  // (and before the app goes to the background).
  let urlTimer = 0;
  let writeURL: (() => void) | null = null;
  let wasOpen = false;
  $effect(() => {
    clearTimeout(urlTimer);
    if (viewing === null) {
      wasOpen = false;
      writeURL = null;
      router.setParams({ i: undefined, f: undefined });
      return;
    }
    results.version;
    const i = viewing;
    const id = results.idAt(i);
    writeURL = () => {
      writeURL = null;
      router.setParams({ i: String(i), f: id === undefined ? undefined : String(id) });
    };
    if (!wasOpen) writeURL();
    else urlTimer = window.setTimeout(() => writeURL?.(), 350);
    wasOpen = true;
  });
  $effect(() => {
    const flush = () => writeURL?.();
    const hidden = () => document.visibilityState === 'hidden' && flush();
    addEventListener('pagehide', flush);
    document.addEventListener('visibilitychange', hidden);
    return () => {
      removeEventListener('pagehide', flush);
      document.removeEventListener('visibilitychange', hidden);
    };
  });

  // ---- Files that arrive (downloads): checked every 15 s while the app is on
  // screen and soon after a download adds files, and taken into the list at
  // once - without moving anything: the grid adds them as whole rows above
  // what's on screen, the viewer stays on its file and a tap opens the file it
  // was on.
  let merging = false;
  async function takeArrivals() {
    if (merging) return;
    merging = true;
    try {
      const shownId = viewing !== null ? results.idAt(viewing) : undefined;
      // The list, the viewer's position and the grid's rows change in one step.
      await results.merge(viewing, (shift) => {
        if (shift <= 0) return;
        if (selection.anchor !== null) selection.anchor += shift;
        if (viewing !== null) {
          const at = shownId !== undefined ? results.indexOf(shownId) : -1;
          viewing = at >= 0 ? at : Math.min(viewing + shift, results.total - 1);
        }
        grid?.addAtTop(shift);
      });
    } finally {
      merging = false;
    }
  }
  $effect(() => {
    const check = async () => {
      if (document.visibilityState !== 'visible') return;
      await results.checkNew();
      if (results.arrived > 0) await takeArrivals();
      updateBadge();
      saved.refreshNews(); // the saved searches menu's NEW pills (and the star's dot)
    };
    const t = setInterval(check, 15_000);
    document.addEventListener('visibilitychange', check);
    // Live: a download added files - look a moment later (several posts land together).
    let soon = 0;
    const off = live.onDownloads(() => {
      clearTimeout(soon);
      soon = setTimeout(check, 1500);
    });
    return () => {
      off();
      clearTimeout(soon);
      clearInterval(t);
      document.removeEventListener('visibilitychange', check);
    };
  });

  // The New section: mark everything as seen (up to this list's time), fold it away.
  async function markSeen() {
    if (results.sinceOverride !== null) {
      // A search opened from the menu: its own NEW files are cleared, not everything's.
      results.sinceOverride = results.snapshot;
      await results.refresh();
      return;
    }
    settings.seenAt = Math.max(settings.seenAt, results.snapshot);
    await results.refresh();
    updateBadge();
  }

  // ---- opening and closing the viewer, animated between the tile and the
  // image's place in the viewer (lib/hero), like iOS Photos.
  let heroing = $state(false);
  // The file in the viewer has no tile while it's open (a swipe down fades the viewer: the grid shows an empty place
  // for the picture to shrink into), nor while it flies back to it on closing.
  let closingId = $state<number | undefined>();
  const hideId = $derived((viewing !== null ? results.idAt(viewing) : undefined) ?? closingId);
  // The grid behind the viewer follows the file being viewed: swiping down to
  // close then shows it already in place (no jump), its thumbnails loaded.
  $effect(() => {
    if (viewing !== null) grid?.reveal(viewing);
  });

  async function open(i: number, details = false) {
    const f = results.fileAt(i);
    const from = grid?.tileRect(i);
    const round = grid?.tileRadius(i) ?? 0;
    viewInfo = details;
    if (details || !from || !f?.thumb || !f.width || reducedMotion()) {
      viewing = i;
      return;
    }
    heroing = true;
    viewing = i;
    await tick();
    const slide = document.querySelector('.viewer .slide[data-slot="1"]');
    if (slide) await hero({ src: f.thumb, aspect: f.width / f.height, from, to: snap(fitContain(f.width, f.height, rectOf(slide))), backdrop: [0, 1], radius: [round, 0] });
    heroing = false;
  }

  function close() {
    const i = viewing;
    // Where the image is now (also mid swipe-down, shrunk and moved), unless zoomed.
    const f = i === null ? undefined : results.fileAt(i);
    const viewer = document.querySelector<HTMLElement>('.viewer');
    const slide = viewer?.querySelector('.slide[data-slot="1"]');
    const zoomed = !!slide?.querySelector('img[style*="scale"]');
    const from = slide && f?.width && !zoomed ? fitContain(f.width, f.height, rectOf(slide)) : null;
    const fade = viewer ? Number(getComputedStyle(viewer).getPropertyValue('--fade')) || 1 : 1;
    closingId = f?.id;
    viewing = null;
    if (i === null) return;
    queueMicrotask(async () => {
      grid?.reveal(i);
      await tick();
      const to = grid?.tileRect(i);
      if (from && to && f?.thumb && !reducedMotion()) await hero({ src: f.thumb, aspect: f.width / f.height, from, to, backdrop: [fade, 0], duration: 260, radius: [0, grid?.tileRadius(i) ?? 0] });
      if (closingId === f?.id) closingId = undefined; // (landed: the tile again)
    });
  }

  $effect(() => {
    topbar.query = query; // (for the Downloads button in the top bar)
  });
  onMount(async () => {
    topbar.library = countControl;
    // The screen shows at once; its first search uses the shared settings
    // (default search and order, what counts as new), read a moment ago.
    await settings.whenSynced();
    if (!router.param('sort') && !fromURL.sort) sort = defaultSort();
    if (!query && settings.defaultSearch && !fromNotification && !fav) query = extractSort(settings.defaultSearch).q;
    if (fromNotification) router.setParams({ new: undefined });
    updateBadge();
    saved.refreshNews();
    router.setParams({ q: query, sort: sortParam() }); // tidies old links with sort: in the search
    await results.search(query, sort);
    // Reopen the file from the URL if it's still at that position.
    const wantIndex = Number(urlIndex);
    if (urlIndex !== '' && wantIndex < results.total) {
      await results.ensure(wantIndex, wantIndex + 1);
      if (results.idAt(wantIndex) === urlId) {
        viewing = wantIndex;
        grid?.reveal(wantIndex);
      }
    }
  });
</script>

<svelte:window onpopstate={onPop} />

<!-- Shown in the app's top bar (see lib/topbar): how many files the search found. -->
{#snippet countControl()}
  <!-- (phones: shorter from 10,000 on - "159K files" - the top bar is crowded; the full number stays in the title and for screen readers) -->
  <span class="count" aria-live="polite" title={results.total ? t('common.files', { n: results.total }) : undefined}
    >{#if (results.busy || !results.searches) && !results.total}{t('library.searching')}{:else if !results.error && results.total < 10_000}{t(
        'common.files',
        { n: results.total },
      )}{:else if !results.error}<span class="sr-only">{t('common.files', { n: results.total })}</span><span class="long" aria-hidden="true"
        >{t('common.files', { n: results.total })}</span
      ><span class="brief" aria-hidden="true">{t('common.filesShort', { n: results.total, x: short(results.total) })}</span>{/if}</span
  >
{/snippet}

<div class="library" style:--lib-pad="{pad}px">
  <header bind:offsetHeight={headH}>
    <div class="search-row">
      <!-- The trash: Empty trash as a red button before the search bar (the user's choice: an icon, no label). -->
      <!-- New files: an eye that marks them seen (just the icon - the user; how many only in its label, for screen readers). Both slide
           in and out sideways, their gap with them. -->
      {#if results.fresh > 0 && !inTrash}
        <div class="slot" transition:slide={{ axis: 'x', duration: reduced() ? 0 : 260, easing: cubicOut }}>
          <button class="new-chip" onclick={markSeen} aria-label={t('library.markSeen', { n: results.fresh })}
            ><Icon name="eye" size={20} /></button>
        </div>
      {/if}
      {#if inTrash && trash.files > 0}
        <div class="slot" transition:slide={{ axis: 'x', duration: reduced() ? 0 : 260, easing: cubicOut }}>
          <button class="empty-trash" aria-label={t('trash.empty')} title={t('trash.empty')} onclick={() => emptyTrash(results)}><Icon name="trash" size={20} /></button>
        </div>
      {/if}
      <div class="search-grow">
        <SearchBar {query} {sort} placeholder={fav ? t('library.favSearch') : inTrash ? t('library.trashSearch') : t('library.search')} defaultSort={defaultSort()} onsearch={search} onsaved={runSaved} onsort={setSort} />
      </div>
    </div>
    {#if inTrash && trash.files > 0}
      <!-- What the trash holds, and how long it keeps it. -->
      <p class="trash-facts" transition:reveal>{t('common.files', { n: trash.files })} · {trash.size} · {trash.days ? t('trash.keepDays', { n: trash.days }) : t('trash.keep')}</p>
    {/if}
    <!-- (no "No tag ..." line: the user - an empty result says enough) -->
    {#if connection.offline || results.error}
    <div class="status" aria-live="polite" transition:reveal>
      {#if connection.offline}
        <span class="offline"><Icon name="clock" size={14} /> {t('library.offline')}</span>
      {:else}
        <span class="error">{results.error}</span>
      {/if}
    </div>
    {/if}
  </header>
  <div class="grid">
    <!-- (only once a search has finished: before the first one - the app starting - the welcome flashed) -->
    {#if results.total === 0 && !results.busy && !results.error && results.searches > 0}
      {#if query}
        <div class="empty" in:swap><p>{t('library.noMatch')}</p></div>
      {:else if fav}
        <div class="empty" in:swap><p>{t('library.noFavorites')}</p></div>
      {:else if inTrash}
        <div class="empty" in:swap><p>{t('library.trashEmpty')}</p></div>
      {:else}
        <div class="welcome-scroll" in:swap><Welcome /></div>
      {/if}
    {:else}
      <Grid
        bind:this={grid}
        {pad}
        {hideId}
        inset={pad - topbar.off}
        onopen={(i, details) => open(i, details)} />
    {/if}
  </div>
  {#if selection.active}<SelectionBar />{/if}
</div>

{#if viewing !== null}
  <Viewer bind:index={viewing} startInfo={viewInfo} hidden={heroing} onclose={close} onsearch={(q) => search(q)} />
{/if}

<style>
  .library {
    height: 100%;
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr); /* never wider than the screen */
    grid-template-rows: minmax(0, 1fr) auto; /* grid, selection bar (the search bar floats) */
  }
  .grid {
    position: relative;
    min-height: 0;
  }
  /* The search bar floats over the grid under the top bar, sliding away with it
     (the grid scrolls under both - nothing moves when they go). */
  header {
    padding: 8px 10px 8px; /* the same gap above the grid with or without the status line */
    position: absolute;
    /* (by its top, not a transform: that would make it the frame of the suggestions
       list, which is fixed to the screen) */
    top: calc(var(--bar-h, 0px) + var(--bar-off, 0px));
    left: 0;
    right: 0;
    z-index: 5;
    background: var(--base);
    /* (and 2px up under the top bar: on screens with fractional pixels - Android - the two edges were rounded apart
       and the scrolling grid showed through a 1px line between them) */
    box-shadow: 0 -2px 0 var(--base);
  }
  /* (only while the bars slide: not when their heights are first measured) */
  :global(.bar-sliding) header {
    transition: top 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.bar-sliding) header {
      transition: none;
    }
  }
  /* Wide screens, and any width with a mouse (a narrow desktop window kept its gap at the sides). */
  @media (min-width: 900px), (pointer: fine) {
    /* The search bar exactly as wide as the grid (the same side gaps). */
    header {
      padding: 10px 12px 10px;
    }
    .grid {
      padding: 0 12px;
      --grid-gutter: 12px; /* (the fast scroller sits past it, at the page's edge) */
    }
  }
  /* The file count in the top bar. */
  .count {
    padding: 0 6px;
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .offline {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--yellow);
  }
  .welcome-scroll {
    height: 100%;
    padding-top: var(--lib-pad);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* The top bar's numbers: in full, or shorter on phones (the bar holds the menu, logo, these, downloads and the bell). */
  .brief {
    display: none;
  }
  @media (max-width: 639px) {
    .long {
      display: none;
    }
    .brief {
      display: inline;
    }
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  /* The same square as Empty trash, in the accent. */
  .new-chip {
    display: grid;
    place-items: center;
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: 12px;
    background: var(--mauve);
    color: var(--crust);
  }
  @media (hover: hover) {
    .new-chip:hover {
      background: color-mix(in srgb, var(--mauve) 85%, white);
    }
  }
  .status {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0 4px;
    margin: 6px 4px 0;
    font-size: var(--font-small);
    font-variant-numeric: tabular-nums;
  }
  /* As wide as the search bar above it (the header's side padding). */
  .empty {
    margin: 0 10px;
    margin-top: calc(var(--lib-pad) + 12px); /* (its border below the search bar, not under it) */
    padding: 40px 16px;
    text-align: center;
  }
  @media (min-width: 900px), (pointer: fine) {
    .empty {
      margin-left: 4px; /* the grid's own 12px + 4px = the header's 16px */
      margin-right: 4px;
    }
  }
  .empty p {
    margin: 4px 0;
  }
  /* The search bar, and on the Trash page a red Empty trash button before it, as tall as the bar. */
  .search-row {
    display: flex;
  }
  /* What sits left of the search bar: its gap inside it, so sliding in and out moves the bar smoothly. */
  .slot {
    flex: none;
    display: flex;
    align-items: flex-start;
    padding-right: 8px;
    white-space: nowrap;
  }
  .search-grow {
    flex: 1;
    min-width: 0;
  }
  /* Square, the search bar's one-line height (its border too), whatever the bar grows to. */
  .empty-trash,
  .new-chip {
    --sq: max(var(--control-h), 38px); /* (44 px on phones; 38 with a mouse, as the bar with its input) */
    width: var(--sq);
    height: var(--sq);
  }
  .empty-trash {
    flex: none;
    min-height: 0;
    padding: 0;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 12px;
    background: var(--red);
    color: var(--crust);
  }
  @media (hover: hover) {
    .empty-trash:hover {
      background: color-mix(in srgb, var(--red) 85%, white);
    }
  }
  /* The trash's line under the search bar: how many, how big, how long kept. */
  .trash-facts {
    margin: 8px 0 0;
    padding: 0 4px;
    color: var(--subtext);
    font-size: var(--font-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
