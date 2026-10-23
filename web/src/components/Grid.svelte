<script module lang="ts">
  // A grid has been on screen already in this visit (the app's first results just show; after that, a grid shown
  // again or for the first time - Comics opened - has its tiles appear).
  let anyShown = false;
</script>

<script lang="ts">
  // The thumbnail grid. Only the rows on screen (plus a margin) exist in the
  // page, so scrolling stays smooth at any size; IDs and file details load in
  // windows around the visible part, ahead of the scroll.
  //
  // Touch: tap = open (or select, while selecting); long-press = file menu;
  // long-press then slide = select everything slid over. Mouse: click = open,
  // right-click = menu, drag = select, Ctrl+click = toggle, Shift+click =
  // select a range. Hovering a video or animated GIF with a mouse plays it in
  // its tile (muted), unless turned off in Settings.
  import { tick, untrack, type ComponentProps } from 'svelte';
  import { pop } from '../lib/motion';
  import { on } from 'svelte/events';
  import type { FileInfo } from '../lib/api';
  import { placeholderURL, formatDuration, previewURL } from '../lib/results.svelte';
  import { useScope } from '../lib/scope.svelte';
  import { pagesText, progress, isRead, isReading, cropStyle, markComicsRead } from '../lib/comics';
  import { collections } from '../lib/collections.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import { restoreFiles, purgeFiles } from '../lib/trash.svelte';
  import { settings } from '../lib/settings.svelte';
  import FileMenu from './FileMenu.svelte';
  import { setFavorite } from '../lib/favorites';
  import Icon from './Icon.svelte';
  import FastScroller from './FastScroller.svelte';
  import { layOut, visible, indexAt as masonryIndexAt } from '../lib/masonry';
  import { handoff, release } from '../lib/videoHandoff';
  import { t, num } from '../lib/i18n.svelte';

  // pad: room at the top of the list for the bars floating over it; inset: how much of
  // the grid they cover now (less once they've slid away).
  // oncrop: the Comics page's "Crop cover" in a tile's menu.
  let {
    onopen,
    pad = 0,
    inset = 0,
    oncrop,
    noHover = false,
    hideId,
  }: {
    onopen: (index: number, details?: boolean) => void;
    pad?: number;
    inset?: number;
    oncrop?: (index: number) => void;
    noHover?: boolean; // (an open/close animation is playing over the grid: no cards meanwhile)
    hideId?: number; // (the file whose open/close animation is playing: that picture is its tile - not shown twice)
  } = $props();
  // The library's files, or (on the Comics page) comics - always in masonry, each cover in its own shape.
  const { results, selection, kind } = useScope();
  const browsing = kind === 'browse'; // (a gallery site's search: drawn like Comics; a tile only opens its preview)
  const comics = kind === 'comics' || browsing;
  const scrollerId = browsing ? 'browse-scroller' : comics ? 'comics-scroller' : 'grid-scroller';
  let isMasonry = $derived(comics || settings.gridLayout === 'masonry');

  let el = $state<HTMLDivElement>();
  let width = $state(0);
  let height = $state(0);
  let scrollTop = $state(0);
  const coarse = matchMedia('(pointer: coarse)').matches;
  // The space between tiles: hairline squares; masonry roomier (with rounded corners).
  let GAP = $derived(isMasonry ? (coarse ? 6 : 10) : 2);

  // ---- Hover previews (mouse only): the mouse resting still on a tile zooms
  // the tile itself into a larger card in the file's own shape - a video plays
  // in it (muted; a thin line shows how far), a picture shows bigger, a GIF
  // animates. It opens at once (2026-10-21, user: no delay - it waited 200 ms for the mouse to rest) and stays while
  // the mouse is anywhere on it; onto another tile, that one opens instead.
  const canHover = matchMedia('(hover: hover) and (pointer: fine)').matches;
  // x, y, w, h: the card, in the list (where the tile goes while zoomed; it
  // grows there from its place, unfolding to the file's shape).
  type Hover = { i: number; id: number; x: number; y: number; w: number; h: number; lead: number };
  let hover = $state<Hover | null>(null);
  let shrinking = $state(-1); // the tile shrinking back (above its neighbors until it's home)
  let snapBack = $state(false); // the viewer took over from the zoomed tile: back without the animation
  // The tile that was hidden (its file open in the viewer or reader) and is back now that the picture landed on it:
  // its overlays (title, chips, badges) fade in rather than appear at once.
  let landedId = $state<number | undefined>();
  let hiddenWas: number | undefined;
  let landedTimer = 0;
  $effect(() => {
    const now = hideId;
    untrack(() => {
      if (hiddenWas !== undefined && hiddenWas !== now) {
        landedId = hiddenWas;
        clearTimeout(landedTimer);
        landedTimer = window.setTimeout(() => (landedId = undefined), 400);
      }
      hiddenWas = now;
    });
  });
  let shrinkTimer = 0;
  let openTimer = 0;
  let restAt = { x: 0, y: 0 };
  let hoverReady = $state(false); // the preview has a frame to show (fades in then)
  let opened = $state<Hover | null>(null); // the zoom that has finished growing (its media loads then)
  let hoverProgress = $state(0);
  let hoverTimer = 0;
  let hoverAt = -1; // the tile under the mouse
  // ... the file there and the first row's gaps then: new files arriving (everything moves
  // along) or the gaps closing slide the tiles sideways - the card mustn't open, or stay, there.
  let hoverWas = { id: -1, lead: 0 };

  // Hover intent: the card opens at once when the mouse slows over a tile (a hand arriving where it aims), not while
  // it sweeps across the grid - every tile passed over popped a card out and back ("shifting zoom animations"; worse
  // with Comics' bigger tiles). Moving fast, it opens once the mouse stays a moment (a move restarts the wait).
  const SWEEP = 0.35; // px per ms: faster than this is passing over, not arriving
  const SWEEP_WAIT = 90; // ms
  let lastMove = { x: 0, y: 0, t: 0, v: 0 };
  // Still scrolling (a wheel scroll glides on a moment after the last notch): no card opens until the grid has been
  // still a while - one opened in the glide was closed by the next scroll step and opened again, placed while the
  // grid still moved (user: the shift was "slightly more common after a scroll"). A move meanwhile opens the tile under
  // the mouse once it's still; a mouse resting through a scroll opens nothing.
  const SCROLL_QUIET = 150; // ms
  let scrolledAt = -Infinity;
  let afterScroll = false; // a move came while scrolling: the card waits for the stillness
  function hoverAfterScroll() {
    afterScroll = false;
    if (noHover || press || selection.active || menu) return;
    const wait = SCROLL_QUIET - (performance.now() - scrolledAt);
    if (wait > 0) {
      afterScroll = true;
      hoverTimer = setTimeout(hoverAfterScroll, wait);
      return;
    }
    const i = tileAt(lastMove.x, lastMove.y);
    hoverAt = i ?? -1;
    if (i === null) return;
    hoverWas = { id: results.idAt(i) ?? -1, lead };
    openHover(i, lastMove.x, lastMove.y);
  }
  function hoverTile(e: PointerEvent) {
    if (!canHover || e.pointerType !== 'mouse' || noHover) return;
    const now = e.timeStamp;
    const dt = now - lastMove.t;
    const v = dt > 0 && dt < 100 ? Math.hypot(e.clientX - lastMove.x, e.clientY - lastMove.y) / dt : 0;
    lastMove = { x: e.clientX, y: e.clientY, t: now, v: lastMove.v * 0.4 + v * 0.6 };
    // The card stays while the mouse is anywhere on it (it opens at once: no rest needed).
    if (hover && Number((e.target as HTMLElement).closest<HTMLElement>('.tile')?.dataset.i) === hover.i) return;
    // A twitch, not a move. While the card is still growing a hand drifting a little doesn't close it either: closing
    // mid-grow and opening again a moment later swung it to and fro.
    if (hover && Math.hypot(e.clientX - restAt.x, e.clientY - restAt.y) < (opened ? 4 : 16)) return;
    clearTimeout(hoverTimer);
    endHover();
    if (performance.now() - scrolledAt < SCROLL_QUIET) {
      hoverAt = -1;
      hoverAfterScroll(); // (it waits for the stillness, then opens what's under the mouse)
      return;
    }
    // (By position: the tile shrinking back may still cover others.)
    const i = press || selection.active || menu ? null : tileAt(e.clientX, e.clientY);
    hoverAt = i ?? -1;
    if (i === null) return;
    hoverWas = { id: results.idAt(i) ?? -1, lead };
    const x = e.clientX;
    const y = e.clientY;
    hoverTimer = setTimeout(() => openHover(i, x, y), lastMove.v > SWEEP ? SWEEP_WAIT : 0); // (else at once - after this move's other handlers)
  }

  /** A comic's title too long for its tile: how far to scroll it, and for how long (it goes there and back). */
  function marquee(e: PointerEvent) {
    const box = (e.currentTarget as HTMLElement).querySelector<HTMLElement>('.comic-title');
    const text = box?.firstElementChild as HTMLElement | null;
    if (!box || !text) return;
    const over = text.scrollWidth - box.clientWidth;
    box.classList.toggle('long', over > 2);
    box.style.setProperty('--shift', `${-over}px`);
    box.style.setProperty('--dur', `${Math.max(2.5, over / 30 + 1.5)}s`);
  }

  /** The card for tile i: about 1.5 tiles across (by area), centered on the tile, inside the grid. */
  function openHover(i: number, x: number, y: number) {
    const f = results.fileAt(i);
    const tileEl = el?.querySelector<HTMLElement>(`.tile[data-i="${i}"]`);
    const box = el?.parentElement;
    if (!f || !f.width || !f.height || !tileEl || !box || hoverAt !== i) return;
    if (f.id !== hoverWas.id || lead !== hoverWas.lead) return; // (moved meanwhile)
    const sliding = slides();
    if (sliding.length) {
      Promise.all(sliding.map((a) => a.finished.catch(() => {}))).then(() => openHover(i, x, y));
      return;
    }
    const M = 8;
    const W = box.clientWidth - 2 * M;
    const H = box.clientHeight - inset - 2 * M; // (below the bars over the grid)
    // Where the tile belongs (its place in the layout), not where it's drawn this
    // moment: a tile still shrinking back or moving (new files, a re-measure) is
    // drawn elsewhere, and the card then started from there - a jump.
    const p = place(i + lead);
    // The same area whatever the shape (a very tall or wide file gets length, not a sliver), about 1.5 tiles
    // across (bigger felt like too much). Comics: the tile itself, a little bigger (their tiles are large already).
    const side = Math.max(size * 1.5, 240);
    const a = f.width / f.height;
    let w = comics ? p.w * 1.15 : side * Math.sqrt(a);
    let h = comics ? p.h * 1.15 : side / Math.sqrt(a);
    // Never smaller than the tile either way: a wide picture's card came out shorter than its square tile (a tall
    // one's narrower), so while the card grew the picture filling it shrank - a zoom out inside a zoom in, a wobble.
    // Now the picture only grows (unless the screen is too small for that).
    const up = Math.max(1, p.w / w, p.h / h);
    w *= up;
    h *= up;
    const k = Math.min(1, W / w, H / h);
    w *= k;
    h *= k;
    const er = el!.getBoundingClientRect();
    const br = box.getBoundingClientRect();
    const tileLeft = er.left - br.left + p.x;
    const tileTop = er.top - br.top + pad - el!.scrollTop + p.y;
    const cx = tileLeft + p.w / 2;
    const cy = tileTop + p.h / 2;
    const left = Math.min(Math.max(cx - w / 2, M), M + W - w);
    const top = Math.min(Math.max(cy - h / 2, M + inset), M + inset + H - h);
    restAt = { x, y };
    // From the grid's box into the list's (where the tile is placed).
    const h0 = { i, id: f.id, x: p.x + left - tileLeft, y: p.y + top - tileTop, w, h, lead };
    hover = h0;
    clearTimeout(openTimer);
    openTimer = window.setTimeout(() => {
      if (hover?.i === i) opened = hover;
    }, ZOOM_MS);
  }
  const ZOOM_MS = 340;

  /** The tiles' own slides still running (the first row's gaps closing after a scroll to the top, files arriving, a
   *  re-order): a card waits for them - opened mid-slide it started from the tile's new place while the tile was
   *  still drawn on its way there (the picture jumped to the side, then grew). */
  function slides(): Animation[] {
    if (!el) return [];
    return el.getAnimations({ subtree: true }).filter((a) => a.playState === 'running' && !(a instanceof CSSTransition) && !(a instanceof CSSAnimation) && ((a.effect as KeyframeEffect | null)?.target as Element | null)?.classList.contains('tile'));
  }

  /** The tile under a point on screen, if any (not the gaps). */
  function tileAt(clientX: number, clientY: number): number | null {
    if (!el || !results.total) return null;
    const i = indexAt(clientX, clientY);
    const p = place(i + lead);
    const r = el.getBoundingClientRect();
    const x = clientX - r.left - p.x;
    const y = clientY - r.top - pad + el.scrollTop - p.y;
    return x >= 0 && x < p.w && y >= 0 && y < p.h ? i : null;
  }

  // The file under a card moved (new files came in at the top, the first row's gaps closed): the card closes
  // instead of sliding off with it.
  $effect(() => {
    void results.version;
    void lead;
    untrack(() => {
      const moved = (i: number, id: number, was: number) => lead !== was || (results.idAt(i) !== undefined && results.idAt(i) !== id);
      if (hover ? moved(hover.i, hover.id, hover.lead) : hoverAt >= 0 && moved(hoverAt, hoverWas.id, hoverWas.lead)) leaveGrid();
    });
  });

  function endHover() {
    clearTimeout(openTimer);
    if (hover) {
      shrinking = hover.i;
      clearTimeout(shrinkTimer);
      shrinkTimer = window.setTimeout(() => (shrinking = -1), 220);
    }
    opened = null;
    hover = null;
    hoverReady = false;
    hoverProgress = 0;
  }

  function leaveGrid() {
    hoverAt = -1;
    afterScroll = false;
    clearTimeout(hoverTimer);
    endHover();
  }
  // An animation over the grid: an open card goes back at once (not shrinking under it; the animation lands on the tile).
  $effect(() => {
    if (!noHover) return;
    untrack(() => {
      if (!hover && hoverAt < 0) return;
      snapBack = true;
      leaveGrid();
      requestAnimationFrame(() => requestAnimationFrame(() => (snapBack = false)));
    });
  });

  // Phones: at least 3 columns (masonry: 2 - its short tiles are a third as tall as
  // a square); wider screens fit as many ~170 px tiles as they can (masonry: ~236 px).
  let masonryOn = $derived(isMasonry);
  let cols = $derived(
    masonryOn
      ? comics
        ? width < 640
          ? 2 // (comics on a phone: two covers to a row)
          : Math.max(2, Math.floor((width + GAP) / (260 + GAP)))
        : Math.max(2, Math.floor((width + GAP) / ((coarse ? 180 : 236) + GAP)))
      : Math.max(3, Math.floor((width + GAP) / ((coarse ? 120 : 170) + GAP))),
  );
  // Masonry on phones: a margin at the sides as wide as the gaps (rounded tiles against the screen edge look cut off).
  let side = $derived(masonryOn && coarse ? GAP : 0);
  let size = $derived(width > 0 ? (width - 2 * side - GAP * (cols - 1)) / cols : 0);
  let rowH = $derived(size + GAP);
  // Files that arrive go in at the top as whole rows, so nothing already on
  // screen moves: the first row keeps empty cells (lead) that the next
  // arrivals fill, and the scroll position moves down by the rows added. A new
  // search or order closes the gaps.
  let lead = $state(0);
  let rows = $derived(Math.ceil((results.total + lead) / cols));
  // Masonry (Settings > Viewing > Grid): columns of tiles in their files' shapes,
  // laid out from the shapes the search sends (squares until they're there).
  let layout = $derived(
    isMasonry && size > 0 && results.shapes?.length === results.total
      ? layOut(results.shapes, results.total, cols, size, GAP)
      : null,
  );
  let contentH = $derived(layout ? layout.height + GAP : rows * rowH);
  let listTop = $derived(scrollTop - pad); // the list's own scroll position (below the room at the top)
  // The tiles that exist: the screen, and beyond it a screen and a half the way it scrolls (half a screen the other
  // way; never less than 3 rows). iOS moves the page itself, ahead of the app: with 3 rows the empty background
  // showed at the bottom while scrolling, even at an easy pace - tiles there show their color preview at least.
  let scrollDir = 1; // 1 down, -1 up (trackSpeed)
  let aheadPx = $derived(Math.max(3 * rowH, height * 1.5));
  let behindPx = $derived(Math.max(3 * rowH, height * 0.5));
  let abovePx = $derived(scrollTop >= 0 && scrollDir < 0 ? aheadPx : behindPx); // (scrollTop: read again as it moves)
  let belowPx = $derived(scrollTop >= 0 && scrollDir < 0 ? behindPx : aheadPx);
  let firstRow = $derived(rowH ? Math.max(0, Math.floor((listTop - abovePx) / rowH)) : 0);
  let lastRow = $derived(rowH ? Math.min(rows, Math.ceil((listTop + height + belowPx) / rowH)) : 0);
  // The slots in the page (on screen, plus a margin), in order; a slot is the
  // position plus the first row's gaps (lead; always 0 in masonry).
  let slots = $derived.by(() => {
    if (layout) return visible(layout, listTop - abovePx, listTop + height + belowPx);
    const out = [];
    for (let c = Math.max(firstRow * cols, lead); c < Math.min(results.total + lead, lastRow * cols); c++) out.push(c);
    return out;
  });
  // The positions in the page [from, to).
  let shownFrom = $derived(layout ? (slots[0] ?? 0) : firstRow * cols - lead);
  let shownTo = $derived(layout ? (slots.length ? slots[slots.length - 1] + 1 : 0) : lastRow * cols - lead);

  /** Where a slot's tile goes in the list. */
  function place(c: number): { x: number; y: number; w: number; h: number } {
    if (layout && c < layout.y.length) return { x: side + layout.x[c], y: layout.y[c], w: layout.w[c], h: layout.h[c] };
    return { x: side + (c % cols) * (size + GAP), y: Math.floor(c / cols) * rowH, w: size, h: size };
  }

  // Masonry turned on: the search again, with the shapes (the tiles slide to their new places).
  $effect(() => {
    if (isMasonry && !results.shapes && results.searches > 0 && !results.busy) untrack(() => results.refresh());
  });

  // A tile whose file and place haven't changed keeps its object: only the tiles coming in are drawn as it scrolls
  // (new objects for all of them re-drew every tile each step - on a big screen ~280, and a fast autoscroll kept the
  // page busy all the time).
  type Tile = { i: number; c: number; f: FileInfo | undefined; p: { x: number; y: number; w: number; h: number } };
  let tileKept = new Map<number, Tile>();
  let tiles = $derived.by(() => {
    results.version; // re-render when IDs or details arrive
    const kept = new Map<number, Tile>();
    const out = slots.map((c) => {
      const i = c - lead;
      const f = results.fileAt(i);
      const p = place(c);
      const o = tileKept.get(c);
      const t = o && o.i === i && o.f === f && o.p.x === p.x && o.p.y === p.y && o.p.w === p.w && o.p.h === p.h ? o : { i, c, f, p };
      kept.set(c, t);
      return t;
    });
    tileKept = kept;
    return out;
  });

  // Phones: the fast scroller goes to the side the thumb scrolls on (each
  // touch on the grid decides; not while the scroller itself is held).
  let fastSide = $state<'left' | 'right'>('right');
  $effect(() => {
    if (!el || !matchMedia('(pointer: coarse)').matches) return;
    const node = el;
    const touched = (e: TouchEvent) => {
      if (fastDrag || (e.target as HTMLElement).closest('.fast')) return;
      const r = node.getBoundingClientRect();
      fastSide = e.touches[0].clientX - r.left < r.width / 2 ? 'left' : 'right';
    };
    node.addEventListener('touchstart', touched, { passive: true });
    return () => node.removeEventListener('touchstart', touched);
  });

  // A thumbnail that failed to load (many asked at once after a long jump) is
  // asked again - a few times, a moment apart - instead of staying a blur.
  function retryThumb(e: Event) {
    const img = e.currentTarget as HTMLImageElement;
    const tries = Number(img.dataset.tries ?? 0);
    if (tries >= 3) return;
    img.dataset.tries = String(tries + 1);
    const src = img.getAttribute('src');
    window.setTimeout(() => {
      if (!img.isConnected || !src) return;
      img.removeAttribute('src');
      img.setAttribute('src', src);
    }, 1000 * (tries + 1));
  }

  // The fast scroller, whenever the results scroll at all (2026-10-22: it was from three screens on, the browser's own
  // scrollbar below that - which came and went with the list's length, the width changed by its room and every tile
  // shifted sideways after a long list gave way to a short one). The browser's scrollbar never shows.
  // It stays as it is while the library is hidden (another screen: no size): dropping it
  // there brought the browser's own scrollbar back for a moment, the grid measured its width
  // without the scrollbar's room, and the tiles fell short of the right edge on return.
  let showFast = $state(false);
  $effect(() => {
    // (touch screens overlay their scrollbars - no room taken: from three screens on, as before)
    if (height > 0) showFast = contentH > (coarse ? (height - pad) * 3 : height - pad + 1);
  });
  // The scrollbar comes or goes with it (the fast scroller replaces it): the width changes,
  // which the size watch doesn't always report - measured again.
  $effect(() => {
    void showFast;
    tick().then(() => {
      if (el && el.clientWidth > 0) width = el.clientWidth;
    });
  });
  let fastDrag = $state(false);

  // ---- Scrolling fast (a flick through a big library): tiles that come into view meanwhile show their color
  // preview only - their thumbnails load once it slows down (else hundreds of thumbnails asked for on the way held
  // up the ones where it stops). Thumbnails seen before still show. The details load further ahead, the way it goes.
  const FLING = 2.5; // px per ms: faster than this is a flick
  let flinging = $state(false);
  let speed = 0;
  let lastScroll = { top: 0, t: 0 };
  let flingTimer = 0;
  const seenThumbs = new Set<string>(); // thumbnails shown (or decoded ahead): they show at once, flinging or not
  function trackSpeed() {
    if (!el) return;
    const now = performance.now();
    const top = el.scrollTop;
    // (the grid moving itself - following the viewer, revealing a file: not a flick)
    if (now < Number(el.dataset.quietUntil ?? 0)) {
      lastScroll = { top, t: now };
      return;
    }
    const dt = now - lastScroll.t;
    if (dt > 0 && dt < 200) {
      const v = (top - lastScroll.top) / dt;
      speed = speed * 0.5 + v * 0.5;
      if (v) scrollDir = v > 0 ? 1 : -1;
    } else speed = 0;
    lastScroll = { top, t: now };
    if (Math.abs(speed) > FLING) flinging = true;
    else if (Math.abs(speed) < FLING / 2) flinging = false;
    clearTimeout(flingTimer);
    flingTimer = window.setTimeout(() => {
      flinging = false;
      speed = 0;
    }, 80);
  }

  // Load around the visible rows: a screen behind, two ahead (flicking: six ahead, the way it goes). While the
  // fast scroller is dragged, only where it pauses (not every screen it passes).
  let ensureTimer = 0;
  $effect(() => {
    // A new result (e.g. another sort order) needs loading even when the visible
    // rows are the same as before - it has the same length and starts at the top.
    void results.searches;
    const perScreen = layout ? shownTo - shownFrom : Math.ceil(height / (rowH || 1)) * cols;
    const ahead = (flinging ? 6 : 2) * perScreen;
    const from = scrollDir > 0 ? shownFrom - perScreen : shownFrom - ahead;
    const to = scrollDir > 0 ? shownTo + ahead : shownTo + perScreen;
    clearTimeout(ensureTimer);
    if (fastDrag) ensureTimer = window.setTimeout(() => results.ensure(from, to), 120);
    else results.ensure(from, to);
  });

  // Slowed down: the next half screen's thumbnails (the way it goes) are fetched and decoded before they come into
  // view - they then show at once, not a moment after.
  const ahead = new Map<string, HTMLImageElement>();
  $effect(() => {
    void results.version;
    if (flinging || fastDrag || !shownTo) return;
    const n = Math.max(cols, Math.ceil((shownTo - shownFrom) / 2));
    const from = scrollDir > 0 ? shownTo - lead : shownFrom - lead - n;
    untrack(() => {
      for (let i = Math.max(0, from); i < Math.min(results.total, from + n); i++) {
        const u = results.fileAt(i)?.thumb;
        if (!u || seenThumbs.has(u) || ahead.has(u)) continue;
        const img = new Image();
        img.decoding = 'async';
        img.src = u;
        ahead.set(u, img);
        img.decode().then(
          () => seenThumbs.add(u),
          () => {},
        );
        if (ahead.size > 120) ahead.delete(ahead.keys().next().value!); // (the oldest: let go)
      }
    });
  });

  // A new search starts at the top, with nothing selected. (Back from a collection's page: where the list was.)
  let lastQuery = '';
  let topOutside = 0; // (the scroll when a collection was opened)
  $effect(() => {
    results.version;
    const key = results.query + '\n' + results.sort + '\n' + results.collection; // a new search or order starts at the top
    if (key !== lastQuery) {
      lastQuery = key;
      const m = untrack(() => morphing);
      if (m?.kind === 'open') topOutside = el?.scrollTop ?? 0;
      const top = m?.kind === 'close' ? topOutside : 0;
      if (el) el.scrollTop = top;
      scrollTop = top;
      lead = 0;
      selection.clear();
    }
  });

  /** The grid moving itself (following the viewer, keeping arrivals still): not
   *  the user scrolling - the top bar stays as it is (App reads data-quiet-until). */
  function scrollSelf(top: number) {
    if (!el) return;
    el.dataset.quietUntil = String(performance.now() + 300);
    el.scrollTop = top;
  }

  /** Scrolls so the tile at index is visible (used when the viewer closes or swipes). */
  export function reveal(index: number) {
    if (!el || !rowH) return;
    const p = place(index + lead);
    const top = el.scrollTop - pad + inset; // the list's part not under the bars
    if (p.y < top || p.y + p.h + GAP > el.scrollTop - pad + height) {
      scrollSelf(p.y + pad - inset - (height - inset - p.h) / 2);
    }
  }

  /** Whether the grid is scrolled to the very top (new files can be added there without moving anything). */
  export function atTop() {
    return !el || el.scrollTop < (rowH || 100) / 2;
  }

  /** n files came in at the top of the list: they go in as whole rows (filling
   *  the first row's gaps first) and the view moves with them, so nothing on
   *  screen moves. */
  export async function addAtTop(n: number) {
    if (n <= 0 || !cols) return;
    if (layout) return masonryAddAtTop(n);
    // At the very top: it stays there and the new files come into view - the list-change animation (the same motion
    // as every other change: they appear, the others slide along to make room).
    if (el && el.scrollTop < (rowH || 100) / 2) {
      lead = 0;
      return;
    }
    skipFlip = true; // (further down nothing on screen moves: no animation; called before the new list renders)
    if (n <= lead) {
      lead -= n;
      return;
    }
    const extra = n - lead;
    const added = Math.ceil(extra / cols);
    lead = added * cols - extra;
    await tick(); // the list is taller first (else the browser clamps the scroll)
    if (el && rowH) {
      scrollSelf(el.scrollTop + added * rowH);
      scrollTop = el.scrollTop;
    }
  }

  /** Masonry: at the top, the new files come into view (the list-change
   *  animation slides the others along); further down, the first tile on
   *  screen keeps its place on screen (every file below the new ones moves in
   *  the new layout, so the view follows it). */
  async function masonryAddAtTop(n: number) {
    lead = 0;
    if (!el || el.scrollTop < (rowH || 100) / 2) return;
    skipFlip = true;
    // (Called with the new list in place but the old tiles still on screen.)
    const box = el.getBoundingClientRect();
    let anchor: { i: number; off: number } | null = null;
    for (const t of el.querySelectorAll<HTMLElement>('.sizer > .tile')) {
      const rc = t.getBoundingClientRect();
      if (rc.bottom <= box.top || rc.top >= box.bottom) continue;
      if (!anchor || rc.top - box.top < anchor.off) anchor = { i: Number(t.dataset.i), off: rc.top - box.top };
    }
    await tick();
    if (!anchor || !el) return;
    scrollSelf(place(anchor.i + n).y - anchor.off);
    scrollTop = el.scrollTop;
  }

  // ---- The list changing (a delete, a tag edit that drops or adds files, or
  // another search sharing files with the last - a chip removed or added):
  // what's on screen moves by file, not by position -
  // tiles slide to their new places, removed ones fade out where they were,
  // newcomers fade in. Positions are noted just before the new list renders.
  type Snap = { x: number; y: number; w: number; h: number; ghost: HTMLElement }; // x, y: where it was on screen
  let flip: Map<number, Snap> | null = null;
  // A collection opening (its comics fly out of its tile) or closing (they fly back into it): results.morph,
  // taken with the list it came with.
  let morphing: typeof results.morph = null;
  let lastKey = '';
  let skipFlip = false;
  let sameSearch = true; // the last new list was the same search again (else: another search)
  $effect(() => {
    results.focus = { from: shownFrom, to: shownTo };
  });
  $effect.pre(() => {
    void results.searches;
    untrack(() => {
      morphing = results.morph;
      results.morph = null;
      const key = `${results.query} sort:${results.sort} ${results.collection}`;
      sameSearch = key === lastKey;
      lastKey = key;
      flip = null;
      const skip = skipFlip;
      skipFlip = false;
      if (skip || !el || !size || reducedMotion()) return;
      flip = snapshot();
      tick().then(playFlip);
    });
  });
  /** A tile's copy without its hover video, taken out at once: a copied video keeps its address and autoplay but not
   *  its muting (a property, not an attribute) - the copies of tiles that stay are never put on the page, and one
   *  played on its own off the page, with sound, nothing to see or stop (files arriving under a resting mouse). */
  function still(ghost: HTMLElement): HTMLElement {
    for (const v of ghost.querySelectorAll('video')) {
      v.removeAttribute('autoplay');
      v.removeAttribute('src');
      v.remove();
    }
    return ghost;
  }
  // ---- One motion for every change of what's on screen (2026-10-21, user: "inconsistent and clunky" - each path had
  // its own timings, fades without scaling, pops): tiles that stay slide to their new places (into another row:
  // diagonally); one from far away leaves where it was like a removed tile and appears at its new place (no sweep
  // across the screen); new tiles appear - a fade with a slight grow - one after another in reading order; removed ones fade
  // with a slight shrink, a little quicker. Reduced motion: none.
  const MOTION = {
    ease: 'cubic-bezier(0.2, 0.8, 0.2, 1)',
    exitEase: 'cubic-bezier(0.4, 0, 1, 1)',
    move: 340,
    enter: 320,
    exit: 200,
    stagger: 18, // between new tiles, in reading order
    maxStagger: 12,
    scale: 0.92,
  };
  const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  /** Where every tile on screen is now (by file), with a copy to fade out if it goes. */
  function snapshot(): Map<number, Snap> {
    const snap = new Map<number, Snap>();
    if (!el) return snap;
    const box = el.getBoundingClientRect();
    for (const t of el.querySelectorAll<HTMLElement>('.sizer > .tile')) {
      const id = Number(t.dataset.id);
      if (!t.dataset.id || Number.isNaN(id)) continue;
      if (isCard(t)) continue; // (the hover card has its own motion)
      const rc = t.getBoundingClientRect();
      if (rc.bottom < box.top || rc.top > box.bottom) continue; // (the rows kept ready off screen)
      snap.set(id, { x: rc.left, y: rc.top, w: rc.width, h: rc.height, ghost: still(t.cloneNode(true) as HTMLElement) });
    }
    return snap;
  }
  /** The tiles on screen now against before: slides, appearances, departures (MOTION). keep(id): a file not on
   *  screen any more that is still in the list (scrolled out of sight: no departure). */
  function animateFrom(before: Map<number, Snap>, keep: (id: number) => boolean = () => false) {
    // (nothing on screen before - the first results: they just show, each thumbnail fading in as it loads)
    if (!el || reducedMotion() || !before.size) return;
    const sizer = el.querySelector<HTMLElement>('.sizer');
    if (!sizer) return;
    const sr = sizer.getBoundingClientRect();
    const box = el.getBoundingClientRect();
    const tr = (x: number, y: number) => `translate(${x}px, ${y}px)`;
    const entering: { el: HTMLElement; c: number }[] = [];
    for (const t of sizer.querySelectorAll<HTMLElement>(':scope > .tile')) {
      if (!t.dataset.id) continue;
      const id = Number(t.dataset.id);
      const c = Number(t.dataset.c);
      const p = place(c);
      const b = before.get(id);
      before.delete(id);
      // The hover card (open, or shrinking back) has its own motion: measured at its card size against its small tile,
      // a list change grew it by that much more, then back (user: it "suddenly zooms in even more then returns").
      if (isCard(t)) continue;
      // (the rows kept ready off screen: nothing to see - no motion, and no place in the line of new tiles)
      const onScreen = sr.top + p.y + p.h > box.top && sr.top + p.y < box.bottom;
      if (!b) {
        if (onScreen) entering.push({ el: t, c });
        continue;
      }
      const ox = b.x - sr.left;
      const oy = b.y - sr.top;
      if (Math.abs(ox - p.x) <= 0.5 && Math.abs(oy - p.y) <= 0.5 && Math.abs(b.w - p.w) <= 0.5) continue;
      // From far away (more than a screen and a half): it leaves and appears, rather than sweeping across the screen.
      // (Into the next row it slides, diagonally - the reshuffle reads as one.)
      if (Math.abs(oy - p.y) > box.height * 1.5) {
        depart(sizer, b, sr);
        entering.push({ el: t, c });
        continue;
      }
      const sizeChange = Math.abs(b.w - p.w) > 0.5 || Math.abs(b.h - p.h) > 0.5;
      if (sizeChange) {
        // (rows: a tile's size changes with its row - it grows or shrinks from where it was)
        t.style.transformOrigin = '0 0';
        t.animate([{ transform: `${tr(ox, oy)} scale(${b.w / p.w}, ${b.h / p.h})` }, { transform: tr(p.x, p.y) }], { duration: MOTION.move, easing: MOTION.ease })
          .finished.finally(() => (t.style.transformOrigin = ''));
      } else {
        t.animate([{ transform: tr(ox, oy) }, { transform: tr(p.x, p.y) }], { duration: MOTION.move, easing: MOTION.ease });
      }
    }
    appear(entering);
    for (const [id, b] of before) if (!keep(id)) depart(sizer, b, sr);
  }
  /** New tiles appearing (MOTION): a fade with a slight grow, one after another in reading order. */
  function appear(list: { el: HTMLElement; c: number }[]) {
    list.sort((a, b) => a.c - b.c);
    list.forEach((a, k) => {
      const p = place(a.c);
      const at = `translate(${p.x}px, ${p.y}px)`;
      a.el.animate(
        [
          { opacity: 0, transform: `${at} scale(${MOTION.scale})` },
          { opacity: 1, transform: `${at} scale(1)` },
        ],
        { duration: MOTION.enter, delay: Math.min(k, MOTION.maxStagger) * MOTION.stagger, easing: MOTION.ease, fill: 'backwards' },
      );
    });
  }
  /** Every tile on screen appears (a grid shown again - back from Comics or the library - or Comics' first results). */
  function appearAll() {
    if (!el || reducedMotion()) return;
    const box = el.getBoundingClientRect();
    const list: { el: HTMLElement; c: number }[] = [];
    for (const t of el.querySelectorAll<HTMLElement>('.sizer > .tile')) {
      const r = t.getBoundingClientRect();
      if (r.bottom > box.top && r.top < box.bottom && t.dataset.c) list.push({ el: t, c: Number(t.dataset.c) });
    }
    appear(list);
  }
  // Shown again (the other page was on screen: this one hidden, no size), or shown with its first results.
  let wasHidden = false;
  let firstShown = false;
  $effect(() => {
    const h = height;
    const ready = results.total > 0 && tiles.length > 0;
    untrack(() => {
      if (h === 0) {
        if (firstShown) wasHidden = true;
        return;
      }
      if (!ready) return;
      if (!firstShown) {
        firstShown = true;
        if (anyShown) tick().then(appearAll); // (a grid opened later in the visit: Comics' first time)
        anyShown = true;
      } else if (wasHidden) {
        wasHidden = false;
        tick().then(appearAll);
      }
    });
  });
  const isCard = (t: HTMLElement) => t.classList.contains('zoomed') || t.classList.contains('shrinking');
  /** A tile leaving: its copy fades out where it was, shrinking a little. */
  function depart(sizer: HTMLElement, g: Snap, sr: DOMRect) {
    const ghost = g.ghost;
    ghost.classList.add('ghost');
    ghost.querySelectorAll('video, .preview').forEach((v) => v.remove());
    ghost.removeAttribute('data-i');
    ghost.removeAttribute('data-id');
    ghost.setAttribute('aria-hidden', 'true');
    ghost.tabIndex = -1;
    sizer.appendChild(ghost);
    const at = `translate(${g.x - sr.left}px, ${g.y - sr.top}px)`;
    ghost
      .animate([{ opacity: 1, transform: `${at} scale(1)` }, { opacity: 0, transform: `${at} scale(${MOTION.scale})` }], { duration: MOTION.exit, easing: MOTION.exitEase, fill: 'forwards' })
      .finished.finally(() => ghost.remove());
  }

  function playFlip() {
    const before = flip;
    flip = null;
    if (!before || !el) return;
    const sizer = el.querySelector<HTMLElement>('.sizer');
    if (!sizer) return;
    // Positions inside the list (the tiles' own transforms); the old ones from
    // where they were on screen (the scroll may have changed: a shorter list).
    const morph = morphing;
    morphing = null;
    if (morph) {
      const sr = sizer.getBoundingClientRect();
      const xy = (c: number) => {
        const p = place(c);
        return [p.x, p.y];
      };
      const tr = (x: number, y: number) => `translate(${x}px, ${y}px)`;
      const now = new Map<number, { el: HTMLElement; c: number }>();
      for (const t of sizer.querySelectorAll<HTMLElement>(':scope > .tile')) {
        if (t.dataset.id) now.set(Number(t.dataset.id), { el: t, c: Number(t.dataset.c) });
      }
      if (playMorph(morph, before, now, sizer, sr, xy, tr, MOTION.ease)) return;
    }
    // (a file of the same search scrolled out of sight isn't leaving; another search with nothing in common on
    // screen: the old tiles leave and the new ones appear, like any change)
    animateFrom(before, (id) => sameSearch && results.indexOf(id) >= 0);
  }

  /** A collection opening: its comics fly out of its tile to their places (the rest fades away); closing: they fly
   *  back into it, the other tiles fading in. False when its tile isn't on screen (the usual animation then). */
  function playMorph(
    morph: NonNullable<typeof results.morph>,
    before: Map<number, Snap>,
    now: Map<number, { el: HTMLElement; c: number }>,
    sizer: HTMLElement,
    sr: DOMRect,
    xy: (c: number) => number[],
    tr: (x: number, y: number) => string,
    ease: string,
  ): boolean {
    const ghostOf = (g: Snap) => {
      const ghost = g.ghost;
      ghost.classList.add('ghost');
      ghost.querySelectorAll('video, .preview').forEach((v) => v.remove());
      ghost.removeAttribute('data-i');
      ghost.removeAttribute('data-id');
      ghost.setAttribute('aria-hidden', 'true');
      ghost.tabIndex = -1;
      ghost.style.transformOrigin = '0 0';
      sizer.appendChild(ghost);
      return ghost;
    };
    const fadeOut = (g: Snap, delay = 0) =>
      ghostOf(g)
        .animate([{ opacity: 1, transform: `${tr(g.x - sr.left, g.y - sr.top)} scale(1)` }, { opacity: 0, transform: `${tr(g.x - sr.left, g.y - sr.top)} scale(0.9)` }], { duration: 220, delay, easing: 'ease-in', fill: 'forwards' })
        .finished.finally(() => g.ghost.remove());
    if (morph.kind === 'open') {
      const from = before.get(morph.id);
      if (!from) return false;
      const ox = from.x - sr.left;
      const oy = from.y - sr.top;
      for (const [id, g] of before) if (id !== morph.id) fadeOut(g);
      // Its tile gives way as they come out of it.
      const tile = ghostOf(from);
      tile.style.zIndex = '1';
      tile
        .animate([{ opacity: 1, transform: `${tr(ox, oy)} scale(1)` }, { opacity: 0, transform: `${tr(ox, oy)} scale(1.04)` }], { duration: 260, easing: 'ease-out', fill: 'forwards' })
        .finished.finally(() => tile.remove());
      let n = 0;
      for (const [, a] of [...now].sort((p, q) => p[1].c - q[1].c)) {
        const [x, y] = xy(a.c);
        const w = a.el.offsetWidth || 1;
        const h = a.el.offsetHeight || 1;
        a.el.style.transformOrigin = '0 0';
        a.el
          .animate(
            [
              { opacity: 0.4, transform: `${tr(ox, oy)} scale(${from.w / w}, ${from.h / h})` },
              { opacity: 1, transform: `${tr(x, y)} scale(1)` },
            ],
            { duration: 460, delay: Math.min(n++, 12) * 28, easing: ease, fill: 'backwards' },
          )
          .finished.finally(() => (a.el.style.transformOrigin = ''));
      }
      return true;
    }
    const into = now.get(morph.id);
    if (!into) return false;
    const [tx, ty] = xy(into.c);
    const tw = into.el.offsetWidth;
    const th = into.el.offsetHeight;
    let n = 0;
    for (const [, g] of [...before].sort((p, q) => p[1].y - q[1].y || p[1].x - q[1].x)) {
      const ghost = ghostOf(g);
      ghost.style.zIndex = '1';
      ghost
        .animate(
          [
            { opacity: 1, transform: `${tr(g.x - sr.left, g.y - sr.top)} scale(1)` },
            { opacity: 0, transform: `${tr(tx, ty)} scale(${tw / g.w}, ${th / g.h})` },
          ],
          { duration: 420, delay: Math.min(n++, 12) * 22, easing: ease, fill: 'both' },
        )
        .finished.finally(() => ghost.remove());
    }
    // Its tile takes them in (a small bump); the others come back.
    into.el.animate([{ transform: `${tr(tx, ty)} scale(0.94)`, opacity: 0.5 }, { transform: `${tr(tx, ty)} scale(1.03)`, opacity: 1, offset: 0.7 }, { transform: `${tr(tx, ty)} scale(1)` }], {
      duration: 520,
      delay: 120,
      easing: 'ease-out',
      fill: 'backwards',
    });
    for (const [id, a] of now) {
      if (id === morph.id) continue;
      const [x, y] = xy(a.c);
      a.el.animate([{ opacity: 0, transform: `${tr(x, y)} scale(0.94)` }, { opacity: 1, transform: `${tr(x, y)} scale(1)` }], { duration: 360, delay: 160, easing: ease, fill: 'backwards' });
    }
    return true;
  }

  // The first row's gaps are only there to keep what's on screen still; once
  // that row comes into view they close, the tiles sliding into place (a tile
  // that changes rows fades in at its new place).
  $effect(() => {
    if (lead > 0 && rowH && scrollTop < rowH - 1) closeGaps();
  });
  function closeGaps() {
    const before = reducedMotion() ? null : snapshot();
    lead = 0;
    if (before) tick().then(() => animateFrom(before, (id) => results.indexOf(id) >= 0));
  }

  /** Back to the top (smoothly, unless reduced motion). */
  export function toTop(instant = false) {
    el?.scrollTo({ top: 0, behavior: instant || matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  }

  /** Where the tile at index is on screen, if it is (for the viewer's open/close animation). peek: only
   *  looking (a hover's card stays - else the animation takes over from it and it goes back). */
  export function tileRect(index: number, peek = false): { x: number; y: number; w: number; h: number } | null {
    if (!el || !rowH || index < 0 || index >= results.total) return null;
    // Zoomed by a hover: from there (it goes back at once, under the viewer, once the animation has taken over).
    const card = hover?.i === index ? el.querySelector<HTMLElement>(`.tile.zoomed[data-i="${index}"]`) : null;
    if (card) {
      const c = card.getBoundingClientRect();
      if (peek) return { x: c.left, y: c.top, w: c.width, h: c.height };
      // Its video goes to the viewer (it carries on from here) and is let go of now, not decoding beside the viewer's.
      const v = card.querySelector<HTMLVideoElement>('video.zoom-media');
      const id = results.idAt(index);
      if (v && id !== undefined) {
        handoff.give(id, v.currentTime);
        release(v)();
      }
      requestAnimationFrame(() =>
        requestAnimationFrame(() => {
          snapBack = true; // (no shrinking back under the animation)
          leaveGrid();
          requestAnimationFrame(() => requestAnimationFrame(() => (snapBack = false)));
        }),
      );
      return { x: c.left, y: c.top, w: c.width, h: c.height };
    }
    const r = el.getBoundingClientRect();
    const p = place(index + lead);
    const x = r.left + p.x;
    const y = r.top + pad + p.y - el.scrollTop;
    if (y + p.h <= r.top + inset || y >= r.bottom) return null;
    return { x, y, w: p.w, h: p.h };
  }

  // ---- taps, long-press, drag-select

  /** The result position under a point on screen (clamped to the grid). */
  function indexAt(clientX: number, clientY: number): number {
    const r = el!.getBoundingClientRect();
    if (layout) return Math.max(0, Math.min(results.total - 1, masonryIndexAt(layout, clientX - r.left - side, clientY - r.top - pad + el!.scrollTop)));
    const col = Math.min(cols - 1, Math.max(0, Math.floor((clientX - r.left) / (size + GAP))));
    const row = Math.max(0, Math.floor((clientY - r.top - pad + el!.scrollTop) / rowH));
    return Math.max(0, Math.min(results.total - 1, row * cols + col - lead));
  }

  type Press = {
    i: number;
    fid?: number; // the file on the tile when pressed
    id: number;
    x: number;
    y: number;
    touch: boolean;
    long: boolean; // held still long enough (touch)
    dragging: boolean;
  };
  let press: Press | null = null;
  let longTimer: number | undefined;
  let dragBase: number[] = [];
  let dragTo = -1;
  let lastPoint = { x: 0, y: 0 };
  let menu = $state<{ i: number; x: number; y: number } | null>(null);

  function tileIndex(e: Event): number | null {
    const t = (e.target as HTMLElement).closest<HTMLElement>('[data-i]');
    return t ? Number(t.dataset.i) : null;
  }

  function down(e: PointerEvent) {
    if (e.button !== 0) return; // right-click: the contextmenu handler
    const i = tileIndex(e);
    if (i === null) return;
    press = {
      i,
      fid: results.idAt(i),
      id: e.pointerId,
      x: e.clientX,
      y: e.clientY,
      touch: e.pointerType !== 'mouse',
      long: false,
      dragging: false,
    };
    lastPoint = { x: e.clientX, y: e.clientY };
    clearTimeout(longTimer);
    if (press.touch && !browsing) {
      // (Browse: no menu - a slow tap is a tap, not a long press that did nothing)
      longTimer = window.setTimeout(() => {
        if (!press) return;
        press.long = true;
        navigator.vibrate?.(10);
      }, 450);
    }
  }

  // What can't be picked (dimmed while selecting): a collection's tile (comics go into a collection, never a collection
  // into another - the user: "I'm able to add collections to other collections"), and on Browse a gallery the library
  // has or whose download waits.
  const unpickable = (f: { comic?: { mark?: string; stack?: unknown } } | undefined) => !!f?.comic?.stack || (browsing && !!f?.comic?.mark);

  function startDrag() {
    if (!press) return;
    press.dragging = true;
    dragBase = selection.list();
    dragTo = press.i;
    applyDrag();
    try {
      el?.setPointerCapture(press.id);
    } catch {
      /* fine */
    }
  }

  /** Selection = what was selected before the drag + the dragged range. */
  function applyDrag() {
    if (!press) return;
    const [a, b] = press.i <= dragTo ? [press.i, dragTo] : [dragTo, press.i];
    selection.ids.clear();
    selection.add(dragBase);
    for (let k = a; k <= b; k++) {
      const id = results.idAt(k);
      if (id !== undefined && !unpickable(results.file(id))) selection.ids.add(id);
    }
    selection.anchor = dragTo;
  }

  function move(e: PointerEvent) {
    if (!press || e.pointerId !== press.id) return;
    lastPoint = { x: e.clientX, y: e.clientY };
    const d = Math.hypot(e.clientX - press.x, e.clientY - press.y);
    if (!press.dragging) {
      if (press.touch && !press.long) {
        if (d > 10) {
          // A scroll, not a press.
          clearTimeout(longTimer);
          press = null;
        }
        return;
      }
      if (d > 8 && !browsing) startDrag(); // (Browse: nothing to select - a click that moved a little still opens)
      return;
    }
    const i = indexAt(e.clientX, e.clientY);
    if (i !== dragTo) {
      dragTo = i;
      applyDrag();
    }
    autoScroll();
  }

  // While dragging near the top or bottom edge, scroll and keep selecting.
  let scrollRaf = 0;
  function autoScroll() {
    if (scrollRaf || !el || !press?.dragging) return;
    const step = () => {
      scrollRaf = 0;
      if (!el || !press?.dragging) return;
      const r = el.getBoundingClientRect();
      const edge = 60;
      let v = 0;
      const top = r.top + inset;
      if (lastPoint.y < top + edge) v = -((top + edge - lastPoint.y) / edge) * 18;
      else if (lastPoint.y > r.bottom - edge) v = ((lastPoint.y - (r.bottom - edge)) / edge) * 18;
      if (!v) return;
      el.scrollTop += v;
      const i = indexAt(lastPoint.x, lastPoint.y);
      if (i !== dragTo) {
        dragTo = i;
        applyDrag();
      }
      scrollRaf = requestAnimationFrame(step);
    };
    scrollRaf = requestAnimationFrame(step);
  }

  async function up(e: PointerEvent) {
    if (!press || e.pointerId !== press.id) return;
    clearTimeout(longTimer);
    const p = press;
    press = null;
    if (p.dragging) {
      // Fill in any part of the range whose IDs weren't loaded during the drag.
      const ids = await results.idsInRange(p.i, dragTo);
      selection.add(ids.filter((x) => !unpickable(results.file(x))));
      return;
    }
    if (p.long) {
      if (!browsing) menu = { i: p.i, x: p.x, y: p.y };
      return;
    }
    if (e.type === 'pointercancel') return;
    tap(p.i, e, p.fid);
  }


  async function tap(i: number, e: { ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }, shown?: number) {
    // The file the tile showed, even if new files moved the list meanwhile.
    if (shown !== undefined && results.idAt(i) !== shown) {
      const at = results.indexOf(shown);
      if (at < 0) return;
      i = at;
    }
    const id = results.idAt(i);
    if (id === undefined) return;
    // (Browse too: Ctrl/Shift-click, or after its Select button - no drag or long press there)
    const picking = selection.active || e.ctrlKey || e.metaKey || (e.shiftKey && selection.anchor !== null);
    if (picking && unpickable(results.file(id))) return;
    if (e.shiftKey && selection.anchor !== null && (selection.active || !browsing)) {
      selection.add((await results.idsInRange(selection.anchor, i)).filter((x) => !unpickable(results.file(x))));
      selection.anchor = i;
    } else if (e.ctrlKey || e.metaKey || selection.active) {
      selection.toggle(id, i);
    } else {
      onopen(i);
    }
  }

  function contextmenu(e: MouseEvent) {
    const i = tileIndex(e);
    if (i === null) return;
    e.preventDefault();
    if (press?.touch || browsing) return; // touch long-press opens the menu itself (Android also sends this)
    menu = { i, x: e.clientX, y: e.clientY };
    leaveGrid();
  }

  // A finger that long-pressed and moves must select, not scroll the grid.
  $effect(() => {
    if (!el) return;
    return on(
      el,
      'touchmove',
      (e: TouchEvent) => {
        if (press?.long || press?.dragging) e.preventDefault();
      },
      { passive: false },
    );
  });

  // A comic's own menu items: Mark as > Read / Unread (more than one page: one page is read by opening it), Crop cover.
  function comicItems(i: number, id: number | undefined): ComponentProps<typeof FileMenu>['extra'] {
    const c = results.file(id)?.comic;
    if (!comics || !c || id === undefined) return [];
    const items: NonNullable<ComponentProps<typeof FileMenu>['extra']> = [];
    if (c.pages > 1) {
      const read = isRead(c);
      const unread = !read && !isReading(c);
      items.push({
        label: t('comics.markAs'),
        icon: 'bookcheck',
        hint: read ? t('comics.state.read') : unread ? t('comics.state.unread') : t('comics.state.reading'),
        choices: [
          { label: t('comics.state.read'), on: read, run: () => markComicsRead([id], true, results) },
          { label: t('comics.state.unread'), on: unread, run: () => markComicsRead([id], false, results) },
        ],
      });
    }
    if (oncrop && c.cover_url) items.push({ label: t('comics.crop.menu'), icon: 'crop', run: () => oncrop(i) });
    // Collections: on a collection's page, out of it; elsewhere into one (or a new one).
    if (results.collection) items.push({ label: t('collections.remove'), icon: 'stack', run: () => collections.remove([id]) });
    else
      items.push({
        label: t('collections.addTo'),
        icon: 'stack',
        hint: collections.list.find((k) => k.id === c.collection)?.name,
        choices: [
          ...collections.list.map((k) => ({ label: k.name, on: k.id === c.collection, run: () => collections.add([id], k.id) })),
          { label: t('collections.newItem'), on: false, run: () => collections.add([id], 'new') },
        ],
      });
    return items;
  }
  /** A collection's tile: its own menu (open it, rename it, take it apart, delete it with its comics). */
  function stackItems(id: number | undefined): ComponentProps<typeof FileMenu>['extra'] {
    const k = results.file(id)?.comic?.stack;
    if (!k) return [];
    return [
      { label: t('collections.rename'), icon: 'pencil', run: () => collections.rename(k) },
      { label: t('collections.reorder'), icon: 'sort', run: () => (collections.ordering = k) },
      { label: t('collections.takeApart'), icon: 'stack', hint: t('collections.takeApartHint'), run: () => collections.takeApart(k) },
      { label: t('collections.delete'), icon: 'trash', danger: true, hint: t('collections.deleteHint'), run: () => collections.deleteWithComics(k) },
    ];
  }

  async function deleteOne(i: number) {
    const id = results.idAt(i);
    if (id === undefined) return;
    const thumb = results.fileAt(i)?.thumb;
    if (results.scope === 'trash') {
      if (await purgeFiles(results, [id], thumb ? [thumb] : [])) selection.ids.delete(id);
      return;
    }
    const title = results.fileAt(i)?.comic?.title;
    const ok = await confirmAction({
      title: title !== undefined ? t('comics.deleteTitle', { title }) : t('selection.deleteTitle', { n: 1 }),
      body: t(title !== undefined ? 'comics.deleteBody' : 'selection.deleteBody'),
      action: t('common.delete'),
      thumbs: thumb ? [thumb] : [],
      undoable: true,
    });
    if (!ok) return;
    try {
      await results.remove([id]);
    } catch (err) {
      toasts.error((err as Error).message);
      return;
    }
    selection.ids.delete(id);
    toasts.show(t(comics ? 'comics.deletedOne' : 'grid.deleted'), { cat: 'deleted',
      kind: 'removed',
      action: {
        label: t('common.undo'),
        run: async () => {
          await results.restore([id]);
          await results.refresh();
        },
      },
    });
    await results.refresh();
  }
</script>

<div class="wrap">
<div
  class="scroller grid-scroller"
  id={scrollerId}
  class:fast-on={showFast}
  class:comics
  style:padding-top="{pad}px"
  class:selecting={selection.active}
  bind:this={el}
  bind:clientWidth={width}
  bind:clientHeight={height}
  role="presentation"
  onscroll={() => {
    scrollTop = el!.scrollTop;
    trackSpeed();
    scrolledAt = performance.now();
    const waiting = afterScroll;
    if (hover || hoverAt >= 0) leaveGrid(); // the tile moved away from the mouse
    if (waiting) {
      // (a move came during the scroll: once the grid is still, the tile under the mouse then)
      clearTimeout(hoverTimer);
      afterScroll = true;
      hoverTimer = setTimeout(hoverAfterScroll, SCROLL_QUIET);
    }
  }}
  onpointerdown={(e) => {
    // A plain click on the file under the card keeps it: the viewer opens from
    // the card (tileRect), not from the small tile.
    const onCard = hover && e.button === 0 && !e.ctrlKey && !e.metaKey && !e.shiftKey && !selection.active && tileIndex(e) === hover.i;
    if (onCard) clearTimeout(hoverTimer);
    else if (e.button !== 2) leaveGrid(); // (a right-click on the card: its menu - the card goes once that opens)
    down(e);
  }}
  onpointermove={(e) => {
    hoverTile(e);
    move(e);
  }}
  onpointerleave={leaveGrid}
  onpointerup={up}
  onpointercancel={up}
  oncontextmenu={contextmenu}>
  <div class="sizer" style:height="{Math.max(0, contentH - GAP)}px">
    {#each tiles as tile (tile.i)}
      {@const z = hover?.i === tile.i ? hover : null}
      <button
        class="tile"
        class:selected={selection.has(tile.f?.id)}
        class:unpickable={selection.active && unpickable(tile.f)}
        aria-disabled={selection.active && unpickable(tile.f) ? true : undefined}
        class:rounded={!!layout}
        class:zoomed={!!z}
        class:landed={landedId !== undefined && results.idAt(tile.i) === landedId}
        class:shrinking={!z && shrinking === tile.i}
        class:snap={snapBack}
        data-i={tile.i}
        data-c={tile.c}
        data-id={results.idAt(tile.i)}
        style:visibility={hideId !== undefined && results.idAt(tile.i) === hideId ? 'hidden' : undefined}
        style:width="{z ? z.w : tile.p.w}px"
        style:height="{z ? z.h : tile.p.h}px"
        style:transform="translate({z ? z.x : tile.p.x}px, {z ? z.y : tile.p.y}px)"
        style:background-image={tile.f?.placeholder ? `url(${placeholderURL(tile.f.placeholder)})` : undefined}
        aria-label={tile.f?.comic ? tile.f.comic.title : tile.f ? `${tile.f.type} ${tile.f.width}×${tile.f.height}` : t('common.loading')}
        aria-pressed={selection.active ? selection.has(tile.f?.id) : undefined}
        onpointerenter={comics ? marquee : undefined}
        onclick={(e) => {
          if (e.detail === 0) tap(tile.i, e); // keyboard (Enter/Space); pointers are handled above
        }}>
        <!-- Everything on the tile, clipped to its rounded shape as one (each piece clipped on its own let a sliver of
             the picture show around a comic's dark foot at the corners). -->
        <span class="face">
        {#if tile.f?.comic?.mosaic && tile.f.comic.mosaic.length > 1}
          {@const m = tile.f.comic.mosaic}
          <!-- A collection: its first comics' covers side by side (two), one big and two small (three), or four, the tile in
               the tallest cover's shape (the server shapes it); each cover - its crop when it has one - filling its cell. -->
          <span class="mosaic n{m.length}" aria-hidden="true">
            {#each m as cover, ci (ci)}
              {@const shape = cover.w && cover.h ? (cover.w * (cover.crop?.[2] ?? 1)) / (cover.h * (cover.crop?.[3] ?? 1)) : 0}
              <span class="cell"
                ><span class="frame" style:--shape={shape || undefined}
                  ><img src={cover.thumb} alt="" style={shape ? cropStyle(cover.crop) : undefined} decoding="async" draggable="false" onload={(e) => e.currentTarget.classList.add('in')} onerror={retryThumb} /></span
                ></span>
            {/each}
          </span>
        {:else if tile.f?.thumb}
          <!-- (flicking past: the color preview only, unless this thumbnail has been seen - see trackSpeed) -->
          {#if !flinging || seenThumbs.has(tile.f.thumb)}
            {@const u = tile.f.thumb}
            <img
              src={u}
              alt=""
              style={cropStyle(tile.f.comic?.crop)}
              decoding="async"
              draggable="false"
              onload={(e) => {
                e.currentTarget.classList.add('in');
                seenThumbs.add(u);
              }}
              onerror={retryThumb} />
            <!-- Still on its way after a moment (e.g. after a long jump): a small spinner over the preview. -->
            <span class="waiting" aria-hidden="true"></span>
          {/if}
        {:else if tile.f}
          <!-- Just downloaded: its thumbnail is being made (checked again every few seconds). -->
          <span class="making" role="img" aria-label={t('grid.making')}></span>
        {/if}
        {#if tile.f?.comic}
          {@const c = tile.f.comic}
          <!-- A comic: read (a book with a check) or being read (a book turning) in a corner; its title and
               pages over the cover's foot; how far it's read along the bottom edge. Nothing here to press. -->
          {#if c.stack}
            <!-- A collection: the edges of two more covers above its own (a deck); read to the end when all of it is. -->
            <span class="deck" aria-hidden="true"><span></span><span></span></span>
            {#if c.stack.count > 0 && c.stack.read >= c.stack.count}<span class="comic-status read" role="img" aria-label={t('comics.finished')} title={t('comics.finished')}><Icon name="bookcheck" size={13} /></span>{/if}
          {:else if c.mark === 'have'}
            <!-- (Comics > Browse: in the library already, or its download waits or runs) -->
            <span class="comic-status read" role="img" aria-label={t('browse.have')} title={t('browse.have')}><Icon name="check" size={13} /></span>
          {:else if c.mark === 'queued'}
            <span class="comic-status queued" role="img" aria-label={t('browse.queued')} title={t('browse.queued')}><Icon name="download" size={13} /></span>
          {:else if isRead(c)}
            <span class="comic-status read" role="img" aria-label={t('comics.finished')} title={t('comics.finished')}><Icon name="bookcheck" size={13} /></span>
          {:else if isReading(c)}
            <span class="comic-status reading" role="img" aria-label={t('comics.inProgress')} title={t('comics.inProgress')}><Icon name="bookreading" size={13} /></span>
          {/if}
          <span class="comic-foot">
            <!-- Its facts as chips (pages; more to come), above the title. -->
            <span class="comic-meta">
              {#if c.stack}
                <span class="comic-chip" title={t('collections.readOf', { read: num(c.stack.read), n: num(c.stack.count) })}><Icon name="stack" size={12} />{t('collections.count', { n: c.stack.count })}</span>
              {:else}
                <span class="comic-chip" title={pagesText(c)}><Icon name="pages" size={12} />{c.have < c.pages ? `${num(c.have)}/${num(c.pages)}` : num(c.pages)}</span>
              {/if}
            </span>
            <span class="comic-title"><span>{c.title}</span></span>
          </span>
          {#if !c.stack && progress(c) > 0 && !isRead(c)}<span class="comic-progress" style:--p={progress(c)}></span>{/if}
        {:else if tile.f && tile.f.type === 'gif'}
          <span class="badge">GIF</span><!-- an image (animated), not a video: no length -->
        {:else if tile.f && tile.f.duration_ms > 0}
          <span class="badge">{formatDuration(tile.f.duration_ms)}</span>
        {/if}
        {#if tile.f?.favorite}
          <span class="fav-badge" class:on-comic={!!tile.f.comic} role="img" aria-label={t('filter.favorite.label')} transition:pop><Icon name="heart" size={tile.f.comic ? 15 : 12} filled /></span>
        {/if}
        {#if tile.f && tile.i < results.pinned}
          <span class="new-badge" transition:pop>{t('grid.new')}</span>
        {/if}
        {#if z && opened === z && tile.f && !tile.f.comic?.stack}
          <!-- The video or bigger picture loads once it has grown: decoding a big one
               while it grows made the animation stutter (the thumbnail shows meanwhile). A collection's card
               keeps its mosaic (its covers, growing with it). -->
          {#if tile.f.mime.startsWith('video/') && !settings.hoverPreview}
            <!-- (videos don't play on hover: Settings; the card shows its thumbnail, grown) -->
          {:else if tile.f.mime.startsWith('video/')}
            <video
              {@attach release}
              class="zoom-media"
              class:ready={hoverReady}
              src={tile.f.url}
              muted
              autoplay
              loop
              playsinline
              disablepictureinpicture
              preload="auto"
              onplaying={() => (hoverReady = true)}
              ontimeupdate={(e) => {
                const v = e.currentTarget;
                hoverProgress = v.duration ? v.currentTime / v.duration : 0;
              }}></video>
            <span class="preview-line" style:--p={hoverProgress}></span>
          {:else}
            <img class="zoom-media" class:ready={hoverReady} src={previewURL(tile.f)} style={cropStyle(tile.f.comic?.crop)} alt="" draggable="false" onload={() => (hoverReady = true)} />
          {/if}
        {/if}
        {#if selection.active && !unpickable(tile.f)}
          <span class="check" aria-hidden="true" transition:pop>{#if selection.has(tile.f?.id)}<Icon name="check" size={14} />{/if}</span>
        {/if}
        </span>
      </button>
    {/each}
  </div>
</div>
  {#if showFast}
    <!-- Its track starts below the bars as they are when shown (pad), and stays there when they slide away:
         following them, the handle jumped up as the top bar went. -->
    <FastScroller {el} {scrollTop} contentHeight={contentH - GAP + pad} {height} top={pad} total={results.total} dates={results.dates} bind:dragging={fastDrag} side={fastSide} controls={scrollerId} />
  {/if}
</div>

{#if menu}
  {@const m = menu}
  {@const id = results.idAt(m.i)}
  <FileMenu
    x={m.x}
    y={m.y}
    selected={selection.has(id)}
    favorite={!!results.file(id)?.favorite}
    onselect={() => id !== undefined && selection.toggle(id, m.i)}
    onfavorite={() => id !== undefined && setFavorite([id], !results.file(id)?.favorite, false, results)}
    viewLabel={results.file(id)?.comic?.stack ? t('fileMenu.open') : comics ? t('comics.read') : undefined}
    viewIcon={results.file(id)?.comic?.stack ? 'stack' : undefined}
    basic={!!results.file(id)?.comic?.stack}
    extra={results.file(id)?.comic?.stack ? stackItems(id) : comicItems(m.i, id)}
    onview={() => onopen(m.i)}
    ondetails={() => onopen(m.i, true)}
    ondelete={() => deleteOne(m.i)}
    trashed={results.scope === 'trash'}
    onrestore={() => id !== undefined && restoreFiles(results, [id])}
    onclose={() => (menu = null)} />
{/if}

<style>
  .wrap {
    position: relative;
    height: 100%;
  }
  /* With a mouse never the browser's scrollbar: the fast scroller instead (it came and went with the list, the width
     with it). Touch screens overlay theirs (no room taken): hidden only behind the fast scroller. */
  .scroller.fast-on {
    scrollbar-width: none;
  }
  .scroller.fast-on::-webkit-scrollbar {
    display: none;
  }
  @media (pointer: fine) {
    .scroller {
      scrollbar-width: none;
    }
    .scroller::-webkit-scrollbar {
      display: none;
    }
  }
  .scroller {
    height: 100%;
    overflow-y: auto;
    overflow-x: hidden;
    overscroll-behavior: contain;
    -webkit-overflow-scrolling: touch;
  }
  .sizer {
    position: relative;
    isolation: isolate; /* (removed tiles fading out stay under the others: z-index -1) */
  }
  /* A removed file's tile fading out where it was (a copy; see playFlip). */
  .sizer > :global(.ghost) {
    pointer-events: none;
    z-index: -1;
  }
  .tile {
    position: absolute;
    left: 0;
    top: 0;
    padding: 0;
    border: 0;
    filter: none !important; /* no hover brightening from the shared button style */
    min-height: 0;
    border-radius: 0;
    background: var(--surface0) center / cover no-repeat;
    overflow: hidden;
    contain: strict;
    /* iOS: no long-press preview or text selection on tiles */
    -webkit-touch-callout: none;
    user-select: none;
    -webkit-user-select: none;
  }
  .tile.rounded {
    border-radius: 8px;
    --r: 8px;
  }
  .face {
    position: absolute;
    inset: 0;
    display: block;
    clip-path: inset(0 round var(--r, 0px));
  }
  /* Growing, open or shrinking back (or a comic - big, with a dark foot): a layer of its own, so the rounded clip is
     one mask over all of it (else the browser clips each piece and the edges show). */
  .tile:is(.zoomed, .shrinking) .face,
  .comics .tile .face {
    -webkit-mask-image: linear-gradient(#000, #000);
    mask-image: linear-gradient(#000, #000);
  }
  @media (pointer: coarse) {
    .tile.rounded {
      border-radius: 6px;
      --r: 6px;
    }
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
    opacity: 0;
    transition:
      opacity 0.18s,
      transform 0.15s;
  }
  img:global(.in) {
    opacity: 1;
  }
  /* Waiting for the thumbnail: a spinner shows only after half a second (a
     normal load never flashes it) and goes once the picture is in. */
  .waiting {
    position: absolute;
    top: 50%;
    left: 50%;
    width: 22px;
    height: 22px;
    margin: -11px 0 0 -11px;
    border-radius: 50%;
    /* The same spinner as everywhere: a faint ring, the accent going round. */
    border: 2.5px solid color-mix(in srgb, var(--text) 18%, transparent);
    border-top-color: var(--mauve);
    opacity: 0;
    pointer-events: none;
    animation:
      making 0.8s linear infinite,
      waiting-in 0.2s ease-out 0.5s forwards;
  }
  img:global(.in) + .waiting {
    display: none;
  }
  @keyframes waiting-in {
    to {
      opacity: 1;
    }
  }
  .selecting .tile:not(.selected) img {
    opacity: 0.75;
  }
  /* (Browse: in the library or downloading already - nothing to pick) */
  .selecting .tile.unpickable {
    opacity: 0.4;
    filter: saturate(0.6);
  }
  .tile.selected img {
    transform: scale(0.88);
    border-radius: 6px;
  }
  .tile.selected {
    background-color: color-mix(in srgb, var(--mauve) 30%, var(--crust));
    background-image: none !important;
  }
  /* Its picture in: no background of its own behind it (at the rounded edges, antialiased, it showed as a light
     hairline around a comic's dark foot - most on a hover card). */
  .tile:not(.selected):has(.face > :global(img.in), .mosaic :global(img.in)) {
    background: none !important;
  }
  /* Back after the viewer or reader closed (the picture landed on it): its overlays fade in. */
  .tile.landed :is(.comic-foot, .comic-status, .comic-progress, .deck, .badge, .fav-badge, .new-badge) {
    animation: overlay-in 0.3s ease-out both;
  }
  @keyframes overlay-in {
    from {
      opacity: 0;
    }
  }
  /* A comic's cover: its overlays (title and chips, marks, progress) wait for its picture and fade in with it - a
     picture the browser had shows at once, and the overlay used to be there before any fade. */
  .tile:has(.face > :global(img:not(.in))) :is(.comic-foot, .comic-status, .comic-progress) {
    opacity: 0;
  }
  .tile:has(.face > :global(img.in)) :is(.comic-foot, .comic-status, .comic-progress) {
    animation: overlay-in 0.3s ease-out both;
  }
  @media (prefers-reduced-motion: reduce) {
    .tile:has(.face > :global(img.in)) :is(.comic-foot, .comic-status, .comic-progress) {
      animation: none;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .tile.landed :is(.comic-foot, .comic-status, .comic-progress, .deck, .badge, .fav-badge, .new-badge) {
      animation: none;
    }
  }
  /* Zoomed by a hover: the tile itself grows over its neighbors into the file's
     own shape (the thumbnail at once, the video or bigger picture over it once it
     has a frame), lifting off the grid; it shrinks back when the mouse moves. */
  .tile.zoomed,
  .tile.shrinking {
    z-index: 5;
    transition:
      transform 0.34s cubic-bezier(0.22, 1, 0.36, 1),
      width 0.34s cubic-bezier(0.22, 1, 0.36, 1),
      height 0.34s cubic-bezier(0.22, 1, 0.36, 1),
      border-radius 0.34s,
      box-shadow 0.34s;
  }
  .tile.zoomed {
    border-radius: 10px;
    --r: 10px;
    box-shadow: 0 16px 22px rgb(0 0 0 / 0.6);
    contain: layout paint; /* (not size: it changes) */
  }
  .tile.shrinking {
    z-index: 4;
    pointer-events: none;
    transition-duration: 0.2s;
    transition-timing-function: cubic-bezier(0.33, 1, 0.68, 1);
  }
  .tile.snap {
    transition: none;
  }
  .tile.zoomed :is(.badge, .fav-badge, .new-badge) {
    opacity: 0;
  }
  .tile .zoom-media {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .tile .zoom-media.ready {
    opacity: 1;
  }
  .preview-line {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: 3px;
    /* mauve up to how far it has played, on a dark track (visible on any picture) */
    background: linear-gradient(to right, var(--mauve) calc(var(--p, 0) * 100%), rgb(17 17 27 / 0.55) 0);
    pointer-events: none;
  }
  /* A favorite: a small heart at the bottom left (the length is at the right). */
  /* A comic's foot: the title and its pages, over a shade (above the hover card's picture: nothing hides). */
  /* A collection's covers (2-4) in its tile, a hairline apart. */
  .mosaic {
    position: absolute;
    inset: 0;
    display: grid;
    gap: 2px;
    background: var(--base);
  }
  .mosaic.n2 {
    grid-template-columns: 1fr 1fr;
  }
  .mosaic.n3 {
    grid-template: 1fr 1fr / 2fr 1fr;
  }
  .mosaic.n3 .cell:first-child {
    grid-row: span 2;
  }
  .mosaic.n4 {
    grid-template: 1fr 1fr / 1fr 1fr;
  }
  .mosaic .cell {
    position: relative;
    overflow: hidden;
    background: var(--crust);
    container-type: size;
  }
  /* Each cover (its own crop when it has one) filling its cell, in its own shape - the overflow cropped evenly, never stretched. */
  .mosaic .frame {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%); /* (centered even when larger than its cell) */
    overflow: hidden;
    width: 100%;
    height: 100%;
  }
  .mosaic .frame[style*='--shape'] {
    width: max(100cqw, 100cqh * var(--shape));
    height: max(100cqh, 100cqw / var(--shape));
  }
  .mosaic img {
    object-fit: cover; /* (a cover of unknown size) */
  }
  /* A collection: the edges of two more covers above its own (a deck), inside the tile (tiles clip what's outside). */
  .deck {
    position: absolute;
    z-index: 1;
    top: 0;
    left: 0;
    right: 0;
    height: 9px;
    background: var(--base);
  }
  .deck > span {
    position: absolute;
    height: 3px;
    border-radius: 3px 3px 0 0;
  }
  .deck > span:first-child {
    top: 0;
    left: 14px;
    right: 14px;
    background: color-mix(in srgb, var(--overlay0) 45%, var(--base));
  }
  .deck > span:last-child {
    top: 4px;
    left: 7px;
    right: 7px;
    background: color-mix(in srgb, var(--overlay0) 80%, var(--base));
  }
  .comic-foot {
    position: absolute;
    z-index: 2;
    /* (a pixel past the edges - clipped there: at a size between whole pixels it stopped short of the picture's edge,
       which showed as a light line around it) */
    left: -1px;
    right: -1px;
    bottom: -1px;
    display: grid;
    justify-items: start;
    gap: 5px;
    padding: 28px 11px 17px; /* (the title well clear of the bottom edge and its progress line; +1 px: past the edges) */
    background: linear-gradient(to top, rgb(17 17 27 / 0.92), rgb(17 17 27 / 0.6) 55%, transparent);
    color: var(--text);
    text-align: left;
    pointer-events: none;
  }
  /* The title: one line, scrolling sideways while the mouse is on the tile when it doesn't fit. */
  .comic-title {
    width: 100%;
    overflow: hidden;
    white-space: nowrap;
    font-size: 13px;
    font-weight: 650;
    line-height: 1.3;
    mask-image: linear-gradient(to right, #000 calc(100% - 14px), transparent);
  }
  .comic-title > span {
    display: inline-block;
  }
  @media (hover: hover) {
    .tile:hover .comic-title:global(.long) > span {
      animation: marquee var(--dur, 4s) ease-in-out 0.3s infinite alternate;
    }
  }
  @keyframes marquee {
    0%,
    12% {
      transform: translateX(0);
    }
    88%,
    100% {
      transform: translateX(var(--shift, 0));
    }
  }
  /* Phones (no hovering): the title on two lines. */
  @media (hover: none) {
    .comic-title {
      white-space: normal;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow-wrap: anywhere;
      mask-image: none;
    }
    .comic-title > span {
      display: inline;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .tile:hover .comic-title:global(.long) > span {
      animation: none;
    }
  }
  /* Its facts: a row of chips (each an icon and a value). */
  .comic-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .comic-chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 8px 2px 6px;
    border-radius: 999px;
    background: rgb(255 255 255 / 0.12);
    font-size: 11px;
    font-weight: 600;
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  /* Read, or being read: a round badge in the top left corner. */
  .comic-status {
    position: absolute;
    z-index: 2;
    top: 5px;
    left: 5px;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 999px;
    color: var(--crust);
    box-shadow: 0 2px 6px rgb(0 0 0 / 0.4);
    pointer-events: none;
  }
  .comic-status.read {
    background: var(--green);
  }
  .comic-status.reading {
    background: var(--mauve);
  }
  .comic-status.queued {
    background: var(--peach);
  }
  /* How far it's read: a line along the cover's bottom edge. */
  .comic-progress {
    position: absolute;
    z-index: 3;
    left: 0;
    bottom: 0;
    height: 3px;
    width: calc(var(--p) * 100%);
    background: var(--mauve);
    pointer-events: none;
  }
  .fav-badge.on-comic {
    z-index: 2;
    top: 6px;
    bottom: auto;
    left: auto;
    right: 6px;
    width: 30px;
    height: 30px;
  }
  .fav-badge {
    position: absolute;
    left: 4px;
    bottom: 4px;
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border-radius: 999px;
    background: rgb(17 17 27 / 0.72);
    color: var(--red);
    pointer-events: none;
  }
  /* A file imported since "Mark all as seen". */
  /* Badges on a thumbnail: pills, like every chip in the app. */
  .new-badge {
    position: absolute;
    left: 4px;
    top: 4px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--mauve);
    color: var(--crust);
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 0.04em;
  }
  .making {
    position: absolute;
    top: 50%;
    left: 50%;
    width: 22px;
    height: 22px;
    margin: -11px 0 0 -11px;
    border-radius: 50%;
    border: 2.5px solid color-mix(in srgb, var(--text) 18%, transparent);
    border-top-color: var(--mauve);
    animation: making 0.8s linear infinite;
  }
  @keyframes making {
    to {
      rotate: 360deg;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .making {
      animation-duration: 3s;
    }
    .waiting {
      animation-duration: 3s, 0.2s;
    }
  }
  .badge {
    position: absolute;
    right: 4px;
    bottom: 4px;
    padding: 1px 7px;
    border-radius: 999px;
    background: rgb(17 17 27 / 0.72);
    color: var(--text);
    font-size: 11px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .check {
    position: absolute;
    top: 6px;
    left: 6px;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    border: 2px solid rgb(205 214 244 / 0.9);
    background: rgb(17 17 27 / 0.35);
    display: grid;
    place-items: center;
    color: var(--crust);
  }
  .tile.selected .check {
    background: var(--mauve);
    border-color: var(--mauve);
  }
</style>
