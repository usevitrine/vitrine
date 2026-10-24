<script lang="ts">
  // The Comics page: looks and works like the library - the same search bar
  // (chips, OR groups, filters, every order, its own saved searches), the
  // same masonry grid (each cover in its own shape, the fast scroller),
  // selection and its actions - over comics instead of files (a second search
  // index on the server). The comics being read come first (a badge says so); a
  // tap opens the reader at /comics/<id>. Comics live only here - their pages are
  // never in the library's grid.
  import { onMount, tick, untrack } from 'svelte';
  import { reveal, glide, swap, nudge } from '../lib/motion';
  import { collections } from '../lib/collections.svelte';
  import ActionMenu from '../components/ActionMenu.svelte';
  import { cubicOut } from 'svelte/easing';
  import { fitContain, hero, reducedMotion, type HeroPicture, type Rect } from '../lib/hero';
  import { isRead, isReading } from '../lib/comics';
  import type { Comic } from '../lib/api';
  import { router } from '../lib/router.svelte';
  import { itemName, t, num } from '../lib/i18n.svelte';
  import { live } from '../lib/live.svelte';
  import { comicsScope, provideScope } from '../lib/scope.svelte';
  import { topbar } from '../lib/topbar.svelte';
  import { extractSort } from '../lib/query';
  import { connection } from '../lib/offline.svelte';
  import { namespaces } from '../lib/namespaces.svelte';
  import Icon from '../components/Icon.svelte';
  import Rich from '../components/Rich.svelte';
  import SearchBar from '../components/SearchBar.svelte';
  import Grid from '../components/Grid.svelte';
  import SelectionBar from '../components/SelectionBar.svelte';
  import CollectionOrderDialog from '../components/CollectionOrderDialog.svelte';
  import ComicReader from '../components/ComicReader.svelte';
  import { trash, emptyTrash } from '../lib/trash.svelte';
  import DetailsPanel from '../components/DetailsPanel.svelte';
  import ComicPanel from '../components/ComicPanel.svelte';
  import CropDialog from '../components/CropDialog.svelte';
  import { api } from '../lib/api';
  import { setFavorite } from '../lib/favorites';
  import { confirmAction } from '../lib/confirm.svelte';
  import { toasts } from '../lib/toast.svelte';

  provideScope(comicsScope);
  const { results, selection } = comicsScope;
  const DEFAULT_SORT = 'imported'; // newest added first

  const here = () => router.screen === 'comics';
  const fromURL = extractSort(router.param('q'));
  let query = $state(fromURL.q);
  let sort = $state(router.param('sort') || fromURL.sort || (here() && router.sub === 'trash' ? 'modified' : loadSort())); // (the trash: the last deleted first)
  let grid = $state<Grid>();
  let headH = $state(0);
  let details = $state<Comic | null>(null);
  // Two pages like the library's: all comics (/comics) and Favorites (/comics/favorites, only favorites - no chip).
  const favAtStart = here() && router.sub === 'favorites';
  let fav = $state(favAtStart);
  // The comics trash (/comics/trash, Menu > Trash > Comics): the deleted comics (lib/trash).
  const trashAtStart = here() && router.sub === 'trash';
  let inTrash = $state(trashAtStart);
  results.scope = favAtStart ? 'favorites' : trashAtStart ? 'trash' : '';
  if (trashAtStart) trash.load();
  // A collection's own page (/comics/collection/<id>): its comics, by title (elsewhere a collection is one tile).
  const COLLECTION_TILE = 2 ** 31; // (a collection's tile in the results: its ID plus this - the server's collectionTile)
  const collectionOf = (sub: string) => Number(sub.match(/^collection\/(\d+)$/)?.[1] ?? 0);
  const collectionAtStart = here() ? collectionOf(router.sub) : 0;
  let inCollection = $state(collectionAtStart);
  results.collection = collectionAtStart;
  if (collectionAtStart) sort = 'title_asc';
  let current = $derived(collections.list.find((k) => k.id === inCollection));
  let queryOutside = ''; // (the search on the page a collection was opened from: back on leaving it)
  let cameFromGrid = false; // (opened from its tile: Back steps back)
  $effect(() => {
    if (!here() || /^\d+$/.test(router.sub)) return; // (reading: the page under the reader stays)
    const now = router.sub === 'favorites';
    const nowTrash = router.sub === 'trash';
    const nowCollection = collectionOf(router.sub);
    if (now === fav && nowTrash === inTrash && nowCollection === inCollection) return;
    untrack(() => {
      const was = inCollection;
      const wasTrash = inTrash;
      fav = now;
      inTrash = nowTrash;
      inCollection = nowCollection;
      results.scope = fav ? 'favorites' : nowTrash ? 'trash' : '';
      if (nowTrash) {
        trash.load();
        sort = 'modified'; // (the last deleted first)
      } else if (wasTrash) sort = loadSort();
      results.collection = inCollection;
      selection.clear();
      if (inCollection && !was) {
        results.morph = { kind: 'open', id: COLLECTION_TILE + inCollection }; // (its comics fly out of its tile)
        queryOutside = query;
        query = '';
        sort = 'title_asc';
      } else if (!inCollection && was) {
        results.morph = { kind: 'close', id: COLLECTION_TILE + was }; // (and back into it)
        query = queryOutside;
        sort = loadSort();
        cameFromGrid = false;
      }
      search(query);
    });
  });
  function leaveCollection() {
    if (cameFromGrid) history.back();
    else router.go('comics');
  }

  function loadSort(): string {
    try {
      return localStorage.getItem('vitrine.comicSort') || DEFAULT_SORT;
    } catch {
      return DEFAULT_SORT;
    }
  }
  function keepSort() {
    if (inCollection) return; // (a collection's page has its own order: by title)
    try {
      localStorage.setItem('vitrine.comicSort', sort);
    } catch {
      /* not remembered */
    }
  }
  const setURL = () => here() && router.setParams({ q: query || undefined, sort: sort === (inTrash ? 'modified' : DEFAULT_SORT) ? undefined : sort });

  async function search(q: string) {
    const typed = extractSort(q); // a sort typed into the search goes to the order, not the chips
    if (typed.sort) sort = typed.sort;
    query = typed.q;
    keepSort();
    setURL();
    await results.search(query, sort);
  }
  function setSort(s: string) {
    sort = s;
    search(query);
  }

  // (The comics being read come first in the list - the server puts them there.)
  let pad = $derived(topbar.h + headH);

  // ---- the reader (/comics/<id>) and the details panel.
  const openID = $derived(here() && /^\d+$/.test(router.sub) ? Number(router.sub) : null);
  let openedHere = false;
  const startAt = new Map<number, number>(); // "Read from the start"
  /** A page asked for in the address (?page=N, 1-based - the I-came page's comic pages), 0-based. */
  const pageParam = () => {
    const p = Number(router.param('page'));
    return p >= 1 ? p - 1 : undefined;
  };
  let readerKey = $state(0); // (a new reader: the same comic again from another page)
  // The end page's Up next: the collection's next comic in the reader's place (Back still closes it); from its
  // start if it was read to the end, else where reading stopped.
  function readNext(c: Comic) {
    if (isRead(c)) startAt.set(c.id, 0);
    else startAt.delete(c.id);
    details = null;
    router.replace('comics', String(c.id));
    readerKey++;
  }
  function read(id: number, fromStart = false) {
    if (fromStart) startAt.set(id, 0);
    else startAt.delete(id); // (where reading stopped)
    closing = false;
    if (openID === id) {
      readerKey++;
      return;
    }
    openedHere = true;
    router.go('comics', String(id));
  }
  // ---- opening and closing the reader, animated between the tile and the page (lib/hero), like the
  // library's viewer: the cover grows from its tile to where the reader shows it; closing, the page shown
  // goes back into its comic's tile.
  let heroing = $state(false);
  let heroId = $state<number | undefined>(); // (the comic flying out of its tile or back: its tile is hidden meanwhile)
  let readerReady: (() => void) | null = null; // (the reader says its page is there)
  // The opened comic's pages, fetched as its opening animation starts (it flies to the page's shape; the reader takes them).
  let preload = $state.raw<{ id: number; p: ReturnType<typeof api.comic> } | null>(null);
  let opening = false; // (its page being looked up: a second tap does nothing)
  async function openAnimated(i: number, c: Comic) {
    if (opening || openID !== null) return;
    // (a comic being read opens where it stopped - the cover flies, then fades into that page; else at the cover)
    const fromStart = !isReading(c);
    if (!c.thumb || !c.cover_w || !c.cover_h || reducedMotion() || !grid?.tileRect(i, true)) return read(c.id, fromStart);
    preload = { id: c.id, p: api.comic(c.id) }; // (the reader takes it: one fetch)
    // The page it opens at (being read: where it stopped - its size and thumbnail from that fetch; not waited for
    // long - the tile stays as it is meanwhile: else the cover's shape, and it fades into the page at the end).
    let page = { w: c.cover_w, h: c.cover_h, thumb: c.thumb };
    if (!fromStart) {
      opening = true;
      const got = await Promise.race([preload.p.catch(() => null), new Promise<null>((r) => setTimeout(() => r(null), 150))]);
      const f = got?.pages[Math.min(c.read_page, got.pages.length - 1)];
      if (f?.width && f.height && f.thumb) page = { w: f.width, h: f.height, thumb: f.thumb };
      opening = false;
    }
    // From the tile as it is now (zoomed by a hover: the card) - the tile itself is what flies: it's hidden meanwhile.
    const from = grid?.tileRect(i);
    if (!from) return read(c.id, fromStart);
    heroing = true;
    heroId = c.id;
    const ready = new Promise<void>((done) => (readerReady = done));
    read(c.id, fromStart);
    // Where the reader puts the page: fitted to the screen, or (scrolling down) across the top at most 900 px wide.
    let mode = 'single';
    try {
      mode = JSON.parse(localStorage.getItem('vitrine.readerMode') ?? '"single"');
    } catch {
      /* the usual */
    }
    const W = innerWidth;
    const H = innerHeight;
    let to: Rect = fitContain(page.w, page.h, { x: 0, y: 0, w: W, h: H });
    if (mode === 'scroll') {
      const w = Math.min(W, 900);
      to = { x: (W - w) / 2, y: 0, w, h: (w * page.h) / page.w };
    }
    // The tile's cover (cropped as there) turns into that page on the way (a cover opened: the whole cover).
    const into = page.thumb !== c.thumb || c.crop?.length === 4 ? { src: page.thumb, aspect: page.w / page.h } : undefined;
    await hero({ ...coverPicture(c), from, to, into, backdrop: [0, 1], fade: 380, hold: ready, above: true });
    heroing = false;
    heroId = undefined;
  }
  /** A comic's cover as its tile shows it (cropped there: that part). */
  function coverPicture(c: Comic): HeroPicture {
    const [, , cw, ch] = c.crop?.length === 4 ? c.crop : [0, 0, 1, 1];
    return { src: c.thumb!, aspect: (c.cover_w! * cw) / (c.cover_h! * ch), crop: c.crop?.length === 4 ? c.crop : undefined };
  }
  /** A still copy of the page where the reader left it, over a backdrop as faded as the reader's was: shown while
   *  closing waits a moment before its flight (see closeReader). */
  function standIn(shot: { src: string; rect: Rect }, fade: number): HTMLElement {
    const layer = document.createElement('div');
    layer.className = 'hero-layer';
    layer.setAttribute('aria-hidden', 'true');
    const shade = document.createElement('div');
    shade.className = 'hero-shade';
    shade.style.opacity = String(fade);
    const img = document.createElement('img');
    img.src = shot.src;
    img.alt = '';
    Object.assign(img.style, { position: 'absolute', left: `${shot.rect.x}px`, top: `${shot.rect.y}px`, width: `${shot.rect.w}px`, height: `${shot.rect.h}px` });
    layer.append(shade, img);
    (document.getElementById('app') ?? document.body).append(layer);
    return layer;
  }
  /** The page the reader shows now: its picture and its place (for closing). */
  // (the reader's middle: with the details column open, left of it)
  function readerMiddle(): [number, number] {
    const r = document.querySelector('.reader .frame')?.getBoundingClientRect();
    return r ? [r.left + r.width / 2, r.top + r.height / 2] : [innerWidth / 2, innerHeight / 2];
  }
  function shownPage(): { src: string; rect: Rect } | null {
    const el = document
      .elementsFromPoint(...readerMiddle())
      .find((e) => e.matches('.reader .page, .reader .strip')) as HTMLElement | undefined;
    const img = el?.querySelector<HTMLImageElement>('img:not(.stand-in)') ?? el?.querySelector<HTMLImageElement>('img');
    if (!el || !img || !(img.currentSrc || img.src)) return null;
    const r = el.getBoundingClientRect();
    return { src: img.currentSrc || img.src, rect: { x: r.left, y: r.top, w: r.width, h: r.height } };
  }

  // Reading changed while the reader was open: the list again on closing (being read: first).
  let readChanged = false;
  let closing = false; // (the reader saves where it stopped as it goes)
  let lastSave: Promise<unknown> = Promise.resolve();
  let refreshTimer = 0;
  /** The list again once the reader has gone and its last save is in (being read: first). */
  function refreshAfterReading() {
    readChanged = false;
    clearTimeout(refreshTimer);
    refreshTimer = window.setTimeout(() => lastSave.finally(() => results.refresh()), 400); // (lastSave as it is then)
  }
  function closeReader(tries = 0) {
    // A menu or the details just closed (touch: they step back in history a moment later): after that
    // step, or the reader's own step back would only undo theirs.
    if (history.state?.layer && tries < 20) {
      if (tries >= 4) history.back(); // (one left behind - two closed at once: stepped back here)
      setTimeout(() => closeReader(tries + 1), 30);
      return;
    }
    const id = openID;
    details = null; // (its details go with it, as the viewer's do)
    closing = true;
    const shot = id !== null && !reducedMotion() ? shownPage() : null;
    // (mid swipe-down the reader's background has faded already: the animation goes on from there - no flash)
    const reader = document.querySelector<HTMLElement>('.reader');
    const fade = reader ? Number(getComputedStyle(reader).getPropertyValue('--fade')) || 1 : 1;
    if (shot && id !== null) {
      heroing = true; // (the reader hides at once; it goes when the address has changed)
      heroId = id; // (its tile stays hidden from the reader going until the cover lands)
      // Reading moved on: the comic may move in the list (being read: first) - the cover flies to where it will be,
      // not where it was. Meanwhile the page stays where it was let go (a still copy): until the reader has gone
      // (its last place is saved then), and if reading changed, until the list is in again - at most 0.6 s.
      const still = standIn(shot, fade);
      const changed = readChanged; // (cleared below: refreshAfterReading)
      const saveBefore = lastSave;
      queueMicrotask(async () => {
        const settled = (async () => {
          for (let k = 0; k < 30 && openID !== null; k++) await new Promise((r) => setTimeout(r, 16));
          if (!changed && lastSave === saveBefore) return; // (nor saved as it went)
          await lastSave.catch(() => {});
          await results.refresh();
        })();
        await Promise.race([settled, new Promise((r) => setTimeout(r, 600))]);
        const i = results.indexOf(id);
        if (i >= 0) {
          grid?.reveal(i);
          await tick();
          const to = grid?.tileRect(i);
          // (the page turns back into the tile's cover on the way)
          const c = results.file(id)?.comic;
          const into = c?.thumb && c.cover_w && c.cover_h ? coverPicture(c) : undefined;
          // (the flight is in place at once: the still copy goes then)
          const flight = to ? hero({ src: shot.src, aspect: shot.rect.w / shot.rect.h, from: shot.rect, to, into, backdrop: [fade, 0], duration: 260 }) : null;
          still.remove();
          await flight;
        }
        still.remove();
        heroing = false;
        heroId = undefined;
      });
    }
    refreshAfterReading();
    if (openedHere) history.back();
    else router.go('comics');
    openedHere = false;
  }
  // Where reading stopped (from the reader itself: closing it needs no reload).
  function onRead(id: number, page: number, saving: Promise<unknown>) {
    const f = results.file(id);
    if (f?.comic) results.patch(id, { comic: { ...f.comic, read_page: page, read_at: Date.now() } });
    lastSave = saving;
    readChanged = true;
    if (closing || openID === null) refreshAfterReading(); // (its last save, as it closed)
  }
  // The reader's heart (and L).
  async function favoriteComic(c: Comic, on: boolean) {
    if (details?.id === c.id) details = { ...details, favorite: on };
    await setFavorite([c.id], on, true, results);
  }
  // Deleted from its details (the panel's header): asked first, undo in the notice.
  async function removeComic(c: Comic) {
    const ok = await confirmAction({
      title: t('comics.deleteTitle', { title: c.title }),
      body: t('comics.deleteBody'),
      action: t('common.delete'),
      thumbs: c.thumb ? [c.thumb] : [],
      undoable: true,
    });
    if (!ok) return;
    try {
      await results.remove([c.id]);
    } catch (e) {
      toasts.error((e as Error).message);
      return;
    }
    if (openID === c.id) closeReader();
    details = null;
    toasts.show(t('common.deletedOne', { what: itemName(c.id, true) }), { cat: 'deleted',
      kind: 'removed',
      action: {
        label: t('common.undo'),
        run: async () => {
          await results.restore([c.id]);
          await results.refresh();
        },
      },
    });
    await results.refresh();
  }

  function open(i: number, info = false) {
    const c = results.fileAt(i)?.comic;
    if (!c) return;
    if (c.stack) {
      // A collection's tile: its page.
      cameFromGrid = true;
      router.go('comics', 'collection/' + c.stack.id);
      return;
    }
    // Details open with the comic's reader, over it - like a file's with the viewer (never over the grid).
    openAnimated(i, c);
    if (info) details = c;
  }

  // Crop cover (a tile's menu): the crop editor over the grid.
  let cropping = $state<Comic | null>(null);
  let panelBusy = $state(false); // (a dialog of the details is open)
  async function saveCrop(c: Comic, crop: number[] | null) {
    cropping = null;
    try {
      await api.updateComic(c.id, { crop: crop ?? [] });
      results.forget(c.id);
      await results.refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  let pendingSearch: string | null = null;
  $effect(() => {
    if (openID === null && pendingSearch !== null) {
      const q = pendingSearch;
      pendingSearch = null;
      untrack(() => search(q));
    }
  });

  // ---- comics that arrive or change (downloads): taken in without moving what's on screen.
  let merging = false;
  let lastTake = 0;
  async function takeChanges() {
    if (merging || document.visibilityState !== 'visible') return;
    merging = true;
    try {
      await results.merge(null, (shift) => {
        if (shift <= 0) return;
        if (selection.anchor !== null) selection.anchor += shift;
        grid?.addAtTop(shift);
      });
      await results.reloadDetails(); // (pages and covers change while a gallery comes in: in place)
      lastTake = Date.now();
    } finally {
      merging = false;
    }
  }
  $effect(() => {
    let soon = 0;
    // A gallery coming in says so with every page: taken in a moment after, at most every 5 s.
    const off = live.onDownloads(() => {
      if (soon) return;
      soon = window.setTimeout(() => {
        soon = 0;
        takeChanges();
      }, Math.max(1500, lastTake + 5000 - Date.now()));
    });
    return () => {
      off();
      clearTimeout(soon);
    };
  });

  // Back here from another screen: its URL again (the page stays mounted).
  $effect(() => {
    if (here()) untrack(setURL);
  });

  onMount(() => {
    collections.load();
    collections.onchange = () => {
      // (a collection taken apart or emptied while its page is open: back to all comics)
      if (inCollection && !collections.list.some((k) => k.id === inCollection)) leaveCollection();
      else results.refresh().then(() => results.reloadDetails()); // (a collection's tile keeps its ID: its count anew)
    };
    topbar.comics = countControl;
    namespaces.loadComics();
    if (results.searches === 0) search(query);
  });
</script>

<!-- Shown in the app's top bar: how many comics the search found. -->
{#snippet countControl()}
  <span class="count" aria-live="polite"
    >{#if (results.busy || !results.searches) && !results.total}{t('library.searching')}{:else if !results.error}{t('comics.count', { n: results.comics })}{/if}</span>
{/snippet}

<div class="comics" style:--lib-pad="{pad}px">
  <header bind:offsetHeight={headH}>
    {#if inCollection}
      <!-- A collection's page: its name, Back to all comics, and its menu (rename, take apart, delete). -->
      <div class="coll-head" in:glide={{ y: -8, duration: 260, easing: cubicOut }}>
        <button class="icon" aria-label={t('collections.back')} title={t('collections.back')} onclick={leaveCollection}><Icon name="left" /></button>
        <span class="coll-icon" aria-hidden="true"><Icon name="stack" size={18} /></span>
        <h2>{current?.name ?? ''}</h2>
        {#if current}
          {@const k = current}
          <span class="coll-read">{t('collections.readOf', { read: num(k.read), n: num(k.count) })}</span>
          <ActionMenu
            label={t('collections.moreFor', { name: k.name })}
            items={[
              { label: t('collections.rename'), icon: 'pencil', run: () => collections.rename(k) },
              { label: t('collections.reorder'), icon: 'sort', run: () => (collections.ordering = k) },
              { label: t('collections.takeApart'), icon: 'stack', hint: t('collections.takeApartHint'), run: () => collections.takeApart(k) },
              { label: t('collections.delete'), icon: 'trash', danger: true, hint: t('collections.deleteHint'), run: () => collections.deleteWithComics(k) },
            ]} />
        {/if}
      </div>
    {/if}
    {#if !inCollection}<div in:glide={{ y: -8, duration: 260, easing: cubicOut }}><div class="search-row">
      {#if inTrash && trash.comics > 0}
        <button class="empty-trash" aria-label={t('trash.empty')} title={t('trash.empty')} onclick={() => emptyTrash(results)} transition:reveal={{ axis: 'x' }}><Icon name="trash" size={20} /></button>
      {/if}
      <div class="search-grow"><SearchBar {query} {sort} placeholder={fav ? t('comics.favSearch') : inTrash ? t('comics.trashSearch') : t('comics.searchPlaceholder')} defaultSort={inTrash ? 'modified' : DEFAULT_SORT} onsearch={search} onsaved={(q, s) => ((sort = s), search(q))} onsort={setSort} /></div>
    </div>
    {#if inTrash && trash.comics > 0}
      <p class="trash-facts" transition:reveal>{t('comics.count', { n: trash.comics })} · {trash.comicSize} · {trash.days ? t('trash.keepDays', { n: trash.days }) : t('trash.keep')}</p>
    {/if}</div>{/if}
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
    {#if results.total === 0 && !results.busy && !results.error && results.searches > 0}
      {#if query}
        <div class="empty" in:swap><p>{t('comics.noMatch')}</p></div>
      {:else if fav}
        <div class="empty" in:swap><p>{t('comics.noFavorites')}</p></div>
      {:else if inTrash}
        <div class="empty" in:swap><p>{t('comics.trashEmpty')}</p></div>
      {:else}
        <div class="empty-shelf" in:swap>
          <span class="badge"><Icon name="book" size={28} /></span>
          <h2>{t('comics.none')}</h2>
          <p class="muted"><Rich text={t('comics.noneHint')} /></p>
        </div>
      {/if}
    {:else}
      <Grid bind:this={grid} noHover={heroing} hideId={heroId ?? openID ?? undefined} {pad} inset={topbar.h + headH - topbar.off} onopen={open} oncrop={(i) => (cropping = results.fileAt(i)?.comic ?? null)} />
    {/if}
  </div>
  {#if selection.active}<SelectionBar />{/if}
  <CollectionOrderDialog />
</div>

{#snippet panelActions()}
  {#if details}
    {@const c = details}
    <button class="icon delete" aria-label={t('comics.deleteLabel')} title={t('common.delete')} onclick={() => removeComic(c)} transition:nudge><Icon name="trash" /></button>
  {/if}
{/snippet}

{#if cropping?.cover_url}
  {@const c = cropping}
  <CropDialog src={c.cover_url!} crop={c.crop} onsave={(crop) => saveCrop(c, crop)} onclose={() => (cropping = null)} />
{/if}

{#if details && openID !== null}
  <DetailsPanel title={t('fileMenu.details')} onclose={() => (details = null)} actions={panelActions} over escapeOff={panelBusy}>
    <ComicPanel
      bind:busy={panelBusy}
      comic={details}
      onread={(fromStart) => {
        const id = details!.id;
        details = null;
        read(id, fromStart);
      }}
      onsearch={(q) => {
        // As from the viewer: the reader closes (its step back in history), then the search runs. (A moment
        // first: the tag's menu and the details take their own history steps back as they close.)
        pendingSearch = q;
        details = null;
        setTimeout(() => closeReader(), 60);
      }}
      onchanged={() => {
        if (details) results.forget(details.id);
        results.refresh();
      }} />
  </DetailsPanel>
{/if}

{#if openID !== null}
  {#key readerKey}
  <ComicReader id={openID} preload={preload?.id === openID ? preload.p : undefined} hidden={heroing} info={!!details} keysOff={!!cropping || panelBusy} startPage={pageParam() ?? startAt.get(openID)} onclose={() => closeReader()} onread={(p, saving) => onRead(openID!, p, saving)} ondetails={(c) => (details = details ? null : c)} onfavorite={(c, on) => favoriteComic(c, on)} ondelete={(c) => removeComic(c)} onnext={readNext} onready={() => {
      preload = null;
      readerReady?.();
    }} />
  {/key}
{/if}

<style>
  .comics {
    height: 100%;
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr) auto; /* grid, selection bar (the search bar floats) */
  }
  .grid {
    position: relative;
    min-height: 0;
  }
  /* The search bar floats over the grid under the top bar, sliding away with it (as in the library). */
  header {
    padding: 8px 10px 8px;
    position: absolute;
    top: calc(var(--bar-h, 0px) + var(--bar-off, 0px));
    left: 0;
    right: 0;
    z-index: 5;
    background: var(--base);
    /* (and 2px up under the top bar: on screens with fractional pixels - Android - the two edges were rounded apart
       and the scrolling grid showed through a 1px line between them) */
    box-shadow: 0 -2px 0 var(--base);
  }
  :global(.bar-sliding) .comics header {
    transition: top 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .coll-head {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: -2px 0 6px -6px;
  }
  .coll-icon {
    display: grid;
    color: var(--mauve);
  }
  .coll-head h2 {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: 17px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .coll-read {
    color: var(--subtext);
    font-size: var(--font-small);
    white-space: nowrap;
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.bar-sliding) .comics header {
      transition: none;
    }
  }
  @media (hover: hover) {
    .delete:hover {
      color: var(--red);
      background: color-mix(in srgb, var(--red) 12%, transparent);
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
    padding: 0 6px;
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .status {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0 4px;
    margin: 6px 4px 0;
    font-size: var(--font-small);
  }
  .offline {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--yellow);
  }

  .empty {
    margin: 0 10px;
    margin-top: calc(var(--lib-pad) + 12px); /* (its border below the search bar, not under it) */
    padding: 40px 16px;
    text-align: center;
  }
  .empty-shelf {
    display: grid;
    justify-items: center;
    text-align: center;
    gap: 8px;
    padding: calc(var(--lib-pad) + 40px) 16px 40px;
  }
  .empty-shelf h2 {
    color: var(--text);
    font-size: 18px;
    margin: 4px 0 0;
  }
  .empty-shelf p {
    margin: 0;
    max-width: 420px;
  }
  .empty-shelf :global(code) {
    font-size: 0.95em;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--surface0);
    color: var(--text);
  }
  .badge {
    display: grid;
    place-items: center;
    width: 56px;
    height: 56px;
    border-radius: 16px;
    background: color-mix(in srgb, var(--mauve) 16%, var(--mantle));
    color: var(--mauve);
  }
  /* The comics trash: a red Empty trash button before the search bar, its totals under it (as Media's). */
  .search-row {
    display: flex;
    gap: 8px;
  }
  .search-grow {
    flex: 1;
    min-width: 0;
  }
  .empty-trash {
    flex: none;
    align-self: stretch;
    width: max(var(--control-h), 40px);
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
