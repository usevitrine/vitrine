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
  import { pop, reduced } from '../lib/motion';
  import { fade } from 'svelte/transition';
  import { fineMouse, touch } from '../lib/media';
  import { measure as measureMarquee } from '../lib/marquee';
  import { cubicBezier } from '../lib/hero';
  import { on } from 'svelte/events';
  import { api, type FileInfo } from '../lib/api';
  import { placeholderURL, formatDuration, previewURL } from '../lib/results.svelte';
  import { useScope } from '../lib/scope.svelte';
  import { live } from '../lib/live.svelte';
  import RenameDialog from './RenameDialog.svelte';
  import { pagesText, progress, isRead, isReading, cropStyle, markComicsRead } from '../lib/comics';
  import { collections } from '../lib/collections.svelte';
  import { toasts, attempt } from '../lib/toast.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import { restoreFiles, purgeFiles } from '../lib/trash.svelte';
  import { settings } from '../lib/settings.svelte';
  import FileMenu from './FileMenu.svelte';
  import { setFavorite } from '../lib/favorites';
  import Icon from './Icon.svelte';
  import FastScroller from './FastScroller.svelte';
  import { layOut, visible, indexAt as masonryIndexAt } from '../lib/masonry';
  import { handoff, release } from '../lib/videoHandoff';
  import { itemName, t, num } from '../lib/i18n.svelte';

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
    stale = false,
    ondownload,
  }: {
    onopen: (index: number, details?: boolean) => void;
    pad?: number;
    inset?: number;
    oncrop?: (index: number) => void;
    noHover?: boolean; // (an open/close animation is playing over the grid: no cards meanwhile)
    hideId?: number; // (the file whose open/close animation is playing: that picture is its tile - not shown twice)
    stale?: boolean; // (the list is another page's - the library on the way to its trash: see cleared)
    ondownload?: (index: number) => void; // (Browse: a gallery's menu item)
  } = $props();
  // The library's files, or (on the Comics page) comics - always in masonry, each cover in its own shape.
  const { results, selection, kind } = useScope();
  const browsing = kind === 'browse'; // (a gallery site's search: drawn like Comics; a tile opens its details)
  const comics = kind === 'comics' || browsing;
  const scrollerId = browsing ? 'browse-scroller' : comics ? 'comics-scroller' : 'grid-scroller';
  // The Comics page: the comics downloading now, each with its pages so far (the stream: twice a second while one runs).
  const downloading = $derived(kind === 'comics' ? new Map(live.files.filter((f) => f.comic).map((f) => [f.comic!, f])) : null);
  // One that has ended, whole: its mark turns into a tick a moment, then goes. (Stopped or failed part way: none. The
  // stream's last word can be a page or two short, so the comic itself is asked.)
  let finishing = $state(new Set<number>());
  let wasComing: number[] = [];
  $effect(() => {
    const now = downloading;
    if (!now) return;
    untrack(() => {
      for (const id of wasComing) {
        if (now.has(id)) continue;
        api.comicInfo([id]).then(
          ({ comics }) => {
            const c = comics[0];
            if (!c || c.have < c.pages) return;
            finishing = new Set(finishing).add(id);
            window.setTimeout(() => {
              const next = new Set(finishing);
              next.delete(id);
              finishing = next;
            }, 1500);
          },
          () => {},
        );
      }
      wasComing = [...now.keys()];
    });
  });
  let isMasonry = $derived(comics || settings.gridLayout === 'masonry');

  let el = $state<HTMLDivElement>();
  let width = $state(0);
  let height = $state(0);
  let scrollTop = $state(0);
  const coarse = touch();
  // The space between tiles: hairline squares; masonry roomier (with rounded corners).
  let GAP = $derived(isMasonry ? (coarse ? 6 : 10) : 2);

  // ---- Hover previews (mouse only): the mouse resting still on a tile zooms
  // the tile itself into a larger card in the file's own shape - a video plays
  // in it (muted; a thin line shows how far), a picture shows bigger, a GIF
  // animates. It opens at once (2026-10-21, user: no delay - it waited 200 ms for the mouse to rest) and stays while
  // the mouse is anywhere on it; onto another tile, that one opens instead.
  const canHover = fineMouse();
  // x, y, w, h: the card, in the list (where the tile goes while zoomed; it
  // grows there from its place, unfolding to the file's shape).
  type Hover = { i: number; id: number; x: number; y: number; w: number; h: number; lead: number };
  let hover = $state<Hover | null>(null);
  let shrinking = $state<Hover[]>([]); // tiles shrinking back (above their neighbors until home; laid out as their card till then)
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
  let openTimer = 0;
  let restAt = { x: 0, y: 0 };
  let hoverReady = $state(false); // the preview has a frame to show (fades in then)
  let opened = $state<Hover | null>(null); // the zoom that has finished growing (its media loads then)
  let hoverProgress = $state(0);
  let scrubbing = $state(false); // the hover card's video being scrubbed (see scrubTo)
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
    if (noHover || unstacking || press || selection.active || menu) return;
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
    if (!canHover || e.pointerType !== 'mouse' || noHover || scrubbing) return; // (scrubbing: the card stays)
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

  /** A comic's title too long for its tile: how far to slide it, and for how long (lib/marquee). */
  function marquee(e: PointerEvent) {
    const box = (e.currentTarget as HTMLElement).querySelector<HTMLElement>('.comic-title');
    if (box) measureMarquee(box);
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
    // (whole pixels, as the tiles: no snap as it lifts off or lands)
    const h0 = { i, id: f.id, x: Math.round(p.x + left - tileLeft), y: Math.round(p.y + top - tileTop), w: Math.round(w), h: Math.round(h), lead };
    const from = cardNow(tileEl) ?? p; // (shrinking back: from where it is)
    shrinking = shrinking.filter((s) => s.i !== i);
    hover = h0;
    tick().then(() => {
      if (hover?.i === i && hover.id === h0.id) fly(tileEl, from, h0, h0, p, GROW); // (hover is a proxy of h0)
    });
    clearTimeout(openTimer);
    openTimer = window.setTimeout(() => {
      if (hover?.i === i) opened = hover;
    }, ZOOM_MS);
  }
  const ZOOM_MS = 340;

  // The card's motion (2026-10-24): laid out at once as the card - its size and place - and drawn from the tile to it
  // by a transform alone, on the compositor: a scale of the card (to the tile's shape: it unfolds to the file's), and
  // inside it (.fit) the opposite of its stretch, so the picture only ever scales evenly and stays centered - as the
  // tile's own crop at first. Growing by width, height and margins (until then) laid it out every frame, each edge
  // snapped to whole pixels on its own, and the picture inside shook by a pixel either way (the user: "a really fast
  // wobble"); a transform isn't snapped, and the picture and video are laid out once. (The easing is baked into the
  // keyframes: the stretch and its opposite don't interpolate as each other's inverse.)
  type Rect = { x: number; y: number; w: number; h: number };
  type Motion = { ms: number; ease: [number, number, number, number] };
  const GROW: Motion = { ms: ZOOM_MS, ease: [0.22, 1, 0.36, 1] };
  const SHRINK: Motion = { ms: 200, ease: [0.33, 1, 0.68, 1] };
  const flights = new Map<HTMLElement, { outer: Animation | Steps; inside: Animation[]; from: Rect; to: Rect; m: Motion }>();
  const lerp = (a: Rect, b: Rect, e: number): Rect => ({ x: a.x + (b.x - a.x) * e, y: a.y + (b.y - a.y) * e, w: a.w + (b.w - a.w) * e, h: a.h + (b.h - a.h) * e });
  /** Where a card in motion is drawn now (in the list), if it's moving. */
  function cardNow(t: HTMLElement): Rect | null {
    const f = flights.get(t);
    if (!f) return null;
    const u = Math.min(1, Math.max(0, Number(f.outer.currentTime ?? 0) / f.m.ms));
    return lerp(f.from, f.to, cubicBezier(...f.m.ease, u));
  }
  function land(t: HTMLElement) {
    const f = flights.get(t);
    flights.delete(t);
    f?.outer.cancel();
    for (const a of f?.inside ?? []) a.cancel();
  }

  /** cubic-bezier(a, b, c, d) as its numbers. */
  const bezier = (css: string) => (css.match(/[\d.]+/g) ?? ['0.25', '0.1', '0.25', '1']).slice(0, 4).map(Number) as [number, number, number, number];

  /**
   * What's inside a tile drawn by a transform that changes its shape (the hover card, a row's resize, a collection's
   * comics flying out of or into it) keeps its own: the picture scales evenly, covering the tile as at rest, and the
   * overlays (marks, badges, the selection's check) keep their size, each held about the tile's corner it's nearest - a
   * comic's foot (its gradient, title and chips) about the bottom left. Stretched with the tile, the picture was distorted and the
   * overlays shrank and grew back, the pages chip drifting from the title (the user: "the downloading/downloaded
   * indicator changes sizes briefly", "the pages icon shifts" - "this shouldnt happen anywhere").
   * scales: the tile's (x, y) scale at each keyframe, timed as its own animation (o).
   */
  function holdInside(t: HTMLElement, scales: [number, number][], o: KeyframeAnimationOptions): Animation[] {
    const out: Animation[] = [];
    const { fit, parts } = insides(t);
    if (fit) {
      out.push(fit.animate(scales.map(([sx, sy]) => {
        const s = Math.max(sx, sy); // (covering the shape it's in, as the tile's picture does)
        return { transform: `scale(${s / sx}, ${s / sy})` };
      }), o));
    }
    const inverse = scales.map(([sx, sy]) => ({ transform: `scale(${1 / sx}, ${1 / sy})` }));
    for (const { el: part, origin } of parts) {
      part.style.transformOrigin = origin;
      const a = part.animate(inverse, o);
      a.finished.then(() => (part.style.transformOrigin = ''), () => (part.style.transformOrigin = ''));
      out.push(a);
    }
    return out;
  }

  /** What holdInside holds in tile t: its picture, and its overlays each with the point in its own box of the tile's
   *  corner it's held about. */
  function insides(t: HTMLElement): { fit: HTMLElement | null; parts: { el: HTMLElement; origin: string }[] } {
    const parts: { el: HTMLElement; origin: string }[] = [];
    const hold = (el: HTMLElement, origin: string) => parts.push({ el, origin });
    // Each is held about the tile's own corner (not its own, nor the foot's - those sit a few pixels in, or a pixel
    // out: drawn smaller with the tile, that gap came back as it landed, a nudge of the chip). Places in the tile's
    // own pixels (a transform on it now: undone by its drawn-to-laid-out ratio).
    const w = t.offsetWidth;
    const h = t.offsetHeight;
    const tr = t.getBoundingClientRect();
    const k = tr.width / w || 1;
    const at = (part: HTMLElement, cornerX: number, cornerY: number) => {
      const r = part.getBoundingClientRect();
      hold(part, `${cornerX - (r.left - tr.left) / k}px ${cornerY - (r.top - tr.top) / k}px`);
    };
    for (const mark of t.querySelectorAll<HTMLElement>(':scope > .face > :is(.comic-status, .fav-badge, .new-badge, .badge, .check)')) {
      const r = mark.getBoundingClientRect();
      // (its corner: the side of the tile it's nearer)
      at(mark, (r.left + r.width / 2 - tr.left) / k > w / 2 ? w : 0, (r.top + r.height / 2 - tr.top) / k > h / 2 ? h : 0);
    }
    const foot = t.querySelector<HTMLElement>(':scope > .face > .comic-foot');
    // (the foot whole - its gradient too, which drawn shorter with the card grew back as it landed; wider than the
    // card while it's drawn smaller, it's clipped by the face)
    if (foot) at(foot, 0, h);
    // (a collection's edges too, about its top left: stretched with the card they stepped - the user: "pixel shift")
    const deck = t.querySelector<HTMLElement>(':scope > .face > .deck');
    if (deck) at(deck, 0, 0);
    return { fit: t.querySelector<HTMLElement>(':scope > .face > .fit'), parts };
  }

  /**
   * An animation played frame by frame on the page (requestAnimationFrame): the hover card's. Played by the compositor,
   * a scale draws its element as an image rendered once - a comic's title and chips, held at their size inside the
   * card, came out soft while it moved and stepped as it landed (the user: "it still shifts"); redrawn every frame they
   * stay as sharp as at rest. currentTime, finished and cancel as an Animation's; kept at its end until cancelled.
   */
  class Steps {
    currentTime = 0;
    readonly finished: Promise<void>;
    private raf = 0;
    private end!: () => void;
    private fail!: (e: unknown) => void;
    private clear: () => void;
    constructor(ms: number, frame: (u: number) => void, clear: () => void) {
      this.clear = clear;
      this.finished = new Promise<void>((resolve, reject) => {
        this.end = resolve;
        this.fail = reject;
      });
      this.finished.catch(() => {});
      frame(0);
      const t0 = performance.now();
      const step = (now: number) => {
        this.currentTime = Math.min(ms, Math.max(0, now - t0));
        frame(this.currentTime / ms);
        if (this.currentTime >= ms) this.end();
        else this.raf = requestAnimationFrame(step);
      };
      this.raf = requestAnimationFrame(step);
    }
    cancel() {
      cancelAnimationFrame(this.raf);
      this.clear();
      this.fail(new DOMException('cancelled', 'AbortError'));
    }
  }

  /** Keyframes (N + 1, for a linear timing: the easing baked in - a shape's scale and the inside's inverse don't ease
   *  as each other's inverse) from one place and size to another: the tile's transforms and its (x, y) scales. */
  function baked(from: { x: number; y: number; sx: number; sy: number }, to: { x: number; y: number; sx: number; sy: number }, ease: [number, number, number, number], extra?: (e: number) => Keyframe) {
    const N = 20;
    const frames: Keyframe[] = [];
    const scales: [number, number][] = [];
    for (let k = 0; k <= N; k++) {
      const e = cubicBezier(...ease, k / N);
      const x = from.x + (to.x - from.x) * e;
      const y = from.y + (to.y - from.y) * e;
      const sx = from.sx + (to.sx - from.sx) * e;
      const sy = from.sy + (to.sy - from.sy) * e;
      frames.push({ ...extra?.(e), transform: `translate(${x}px, ${y}px) scale(${sx}, ${sy})` });
      scales.push([sx, sy]);
    }
    return { frames, scales };
  }
  /** Draws tile t (laid out as card, placed by base) from one place to another; done when it's there. */
  function fly(t: HTMLElement, from: Rect, to: Rect, card: Rect, base: { x: number; y: number }, m: Motion, done?: () => void) {
    land(t);
    if (reduced()) return done?.();
    const { fit, parts } = insides(t);
    const foot = t.querySelector<HTMLElement>(':scope > .face > .comic-foot');
    const title = foot?.querySelector<HTMLElement>('.comic-title');
    const deck = t.querySelector<HTMLElement>(':scope > .face > .deck');
    for (const { el, origin } of parts) el.style.transformOrigin = origin;
    // By the translate and scale properties (the tile's own transform - its place - is left alone): they come before
    // it, so the scale scales its translate(base) too - the translate takes that back.
    const frame = (u: number) => {
      const r = lerp(from, to, cubicBezier(...m.ease, u));
      const sx = r.w / card.w;
      const sy = r.h / card.h;
      t.style.translate = `${base.x + r.x - card.x - sx * base.x}px ${base.y + r.y - card.y - sy * base.y}px`;
      t.style.scale = `${sx} ${sy}`;
      if (fit) {
        const s = Math.max(sx, sy); // (covering the shape it's in, as the tile's picture does)
        fit.style.scale = `${s / sx} ${s / sy}`;
      }
      for (const { el } of parts) el.style.scale = `${1 / sx} ${1 / sy}`;
      // (a comic's foot as wide as the card is drawn: laid out as the card, its title's soft edge stayed out of view
      // past the tile's side and came back at once as it landed - the user: "the gradient ... jumps back")
      if (foot) {
        foot.style.right = 'auto';
        foot.style.width = `${r.w + 2}px`;
      }
      if (deck) {
        deck.style.right = 'auto';
        deck.style.width = `${r.w}px`;
      }
    };
    const clear = () => {
      t.style.translate = t.style.scale = '';
      if (fit) fit.style.scale = '';
      for (const { el } of parts) el.style.scale = el.style.transformOrigin = '';
      if (foot) foot.style.right = foot.style.width = '';
      if (deck) deck.style.right = deck.style.width = '';
    };
    // (kept at the end until the layout follows - a shrink's card is laid out as the tile only then)
    const outer = new Steps(m.ms, frame, clear);
    flights.set(t, { outer, inside: [], from, to, m });
    outer.finished.then(
      () => {
        if (flights.get(t)?.outer !== outer) return;
        if (done) done();
        if (title) measureMarquee(title); // (does its title still not fit, at this width?)
        tick().then(() => flights.get(t)?.outer === outer && land(t));
      },
      () => {},
    );
  }

  /** The tiles' own slides still running (the first row's gaps closing after a scroll to the top, files arriving, a
   *  re-order): a card waits for them - opened mid-slide it started from the tile's new place while the tile was
   *  still drawn on its way there (the picture jumped to the side, then grew). */
  function slides(): Animation[] {
    if (!el) return [];
    return el.getAnimations({ subtree: true }).filter((a) => (a.playState === 'running' || a.playState === 'paused') && a.id !== 'card' && !(a instanceof CSSTransition) && !(a instanceof CSSAnimation) && ((a.effect as KeyframeEffect | null)?.target as Element | null)?.classList.contains('tile'));
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
      const h = hover;
      const t = el?.querySelector<HTMLElement>(`.tile[data-i="${h.i}"]`);
      if (t && !snapBack) {
        // Back to its place from where it's drawn (mid-grow: from there), laid out as the card until it's home.
        const from = cardNow(t) ?? h;
        shrinking = [...shrinking.filter((s) => s.i !== h.i), h];
        const home = place(h.i + lead);
        fly(t, from, home, h, home, SHRINK, () => (shrinking = shrinking.filter((s) => s.i !== h.i)));
      } else if (t) land(t);
    }
    opened = null;
    hover = null;
    hoverReady = false;
    hoverProgress = 0;
    held = null;
  }

  function leaveGrid() {
    if (scrubbing) return; // (the card stays while its video is scrubbed, wherever the pointer goes)
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
  // A list taller than a browser can make an element (33.5M px in Chromium and Safari, 17.9M in Firefox; a million files
  // at 3 a row is 43M - the user: "I can't scroll past 2024"): the page stays at most MAX_H tall and shows a window of the
  // list, base being where in the list the page starts. Nearing the window's edge it moves, the page's scroll with it, so
  // nothing on screen does. place() says where a tile is on the page (the transforms stay small and exact: a browser
  // keeps them in 32 bits); topNow() and moveTo() are positions in the list. (A shorter list: base 0.)
  const MAX_H = 16_000_000;
  const EDGE = 4_000_000;
  let base = $state(0);
  let listH = $derived(Math.max(0, contentH - GAP));
  let pageH = $derived(Math.min(listH, MAX_H));
  const topNow = () => (el?.scrollTop ?? 0) + base;
  function moveTo(top: number) {
    if (!el) return;
    const spare = listH - pageH;
    const at = top - base;
    if (base > spare || (at < EDGE && base > 0) || (at > MAX_H - EDGE && base < spare)) base = Math.max(0, Math.min(spare, Math.round(top - MAX_H / 2)));
    el.scrollTop = top - base;
    scrollTop = topNow(); // (the page's scroll may land where it was: then no scroll event says the list moved)
  }
  // Room under the last row: a gap, and on an iPhone clear of the home bar and the screen's curved corners (the last
  // tiles sat under them - the user). In pixels for the fast scroller.
  let padBottom = $state(0);
  $effect(() => {
    void width;
    void height;
    void GAP;
    if (el) padBottom = parseFloat(getComputedStyle(el).paddingBottom) || 0;
  });
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

  // ---- The tiles' shapes, drawn under them ahead of time (2026-10-28, the user's recording: flicking fast on an
  // iPhone showed the dark background - a whole screen of it for over a second). iOS moves the page itself, faster
  // than any page can make tiles for it (WebKit: ~35-50 ms a step with tiles coming in). So the page's own background,
  // in bands, is a picture of where the tiles go - rounded boxes in the tiles' color, exactly at their places: a flick
  // that outruns the tiles shows the boxes they'll fill, never nothing. A few pictures (one per band, made once each),
  // kept from four screens above to five below, moving with the page without the page doing anything.
  const BAND = 2048;
  let bandColor = $state('');
  let holeColor = $state(''); // the page's own background (the first one under the grid that isn't see-through)
  $effect(() => {
    void width;
    if (!el) return;
    bandColor = getComputedStyle(el).getPropertyValue('--surface0').trim() || '#313244';
    for (let n: Element | null = el; n; n = n.parentElement) {
      const bg = getComputedStyle(n).backgroundColor;
      if (bg && bg !== 'transparent' && !/rgba\(.*,\s*0\)$/.test(bg)) {
        holeColor = bg;
        break;
      }
    }
  });
  const bandCache = new Map<number, string>();
  let bandKey = '';
  /** A band's picture: the boxes of the tiles in it (CSS background-image). */
  function bandURL(b: number): string {
    const y0 = b * BAND;
    const y1 = y0 + BAND;
    const slotsIn: number[] = [];
    if (layout) slotsIn.push(...visible(layout, y0 - 1, y1));
    else if (rowH) {
      for (let r = Math.floor(y0 / rowH); r * rowH < y1 && r < rows; r++) {
        for (let c = Math.max(r * cols, lead); c < Math.min((r + 1) * cols, results.total + lead); c++) slotsIn.push(c);
      }
    }
    const radius = layout ? (coarse ? 6 : 8) : 0;
    let rects = '';
    for (const c of slotsIn) {
      const p = place(c);
      const py = p.y + base;
      if (py + p.h < y0 || py > y1) continue;
      rects += `<rect x="${p.x}" y="${py - y0}" width="${p.w}" height="${p.h}" rx="${radius}"/>`;
    }
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${Math.ceil(width)}" height="${BAND}"><g fill="${bandColor}">${rects}</g></svg>`;
    return `url("data:image/svg+xml,${encodeURIComponent(svg)}")`;
  }
  let bands = $derived.by(() => {
    if (!size || !contentH || !bandColor || !results.total) return [];
    // (the cache holds while the boxes would be the same: same list, sizes and color)
    const key = `${results.searches} ${results.total} ${lead} ${cols} ${size} ${width} ${bandColor} ${layout ? layout.height : 0}`;
    if (key !== bandKey) {
      bandCache.clear();
      bandKey = key;
    }
    const from = Math.max(0, Math.floor((listTop - 4 * height) / BAND));
    const to = Math.min(Math.ceil(contentH / BAND), Math.ceil((listTop + 6 * height) / BAND));
    const out: { b: number; url: string }[] = [];
    for (let b = from; b < to; b++) {
      let url = bandCache.get(b);
      if (url === undefined) bandCache.set(b, (url = bandURL(b)));
      out.push({ b, url });
    }
    return out;
  });

  /** Where a slot's tile goes in the list - on whole pixels: the columns' width is the grid's divided up (237.33...
   *  px), and a tile between pixels was snapped onto them the moment it zoomed (becoming a layer of its own): it
   *  jumped up and to the left before growing (the user, more than once). Its edges are rounded, not its size, so the
   *  gaps still add up and the last column still meets the edge. */
  function place(c: number): { x: number; y: number; w: number; h: number } {
    const r =
      layout && c < layout.y.length
        ? { x: side + layout.x[c], y: layout.y[c], w: layout.w[c], h: layout.h[c] }
        : { x: side + (c % cols) * (size + GAP), y: Math.floor(c / cols) * rowH, w: size, h: size };
    const x = Math.round(r.x);
    const y = Math.round(r.y);
    return { x, y: y - base, w: Math.round(r.x + r.w) - x, h: Math.round(r.y + r.h) - y };
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
  // A tile away from its place - its file open (hideId: the picture is its tile), zoomed by hovering, or shrinking
  // back - leaves the page there, not its box under it (bands; the user: "there shouldn't be anything, just empty space").
  const holes = $derived(
    tiles
      .filter((t) => hover?.i === t.i || shrinking.some((s) => s.i === t.i) || (hideId !== undefined && results.idAt(t.i) === hideId))
      .map((t) => t.p),
  );

  // Phones: the fast scroller goes to the side the thumb scrolls on (each
  // touch on the grid decides; not while the scroller itself is held).
  let fastSide = $state<'left' | 'right'>('right');
  $effect(() => {
    if (!el || !touch()) return;
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
  // A thumbnail that failed is asked for again a few times, waiting longer each time (a site's pictures - Browse - can
  // fail in bursts); still failing, it stops spinning (`failed`) - shown again when the tile comes back.
  const thumbWaits = [1000, 2000, 4000, 8000];
  function retryThumb(e: Event) {
    const img = e.currentTarget as HTMLImageElement;
    const tries = Number(img.dataset.tries ?? 0);
    if (tries >= thumbWaits.length) {
      img.classList.add('failed');
      return;
    }
    img.dataset.tries = String(tries + 1);
    const src = img.getAttribute('src');
    window.setTimeout(() => {
      if (!img.isConnected || !src) return;
      img.removeAttribute('src');
      img.setAttribute('src', src);
    }, thumbWaits[tries]);
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
  // A thumbnail comes in once it's decoded: faded in over a picture still decoding, it popped at the end of the fade
  // (WebKit: one in seven). The ones decoded in the same moment fade in together (one frame, one style pass); one seen
  // before shows at once (`now`: a tile made again on the way back up faded in from its preview a second time).
  const fading = new Set<Element>();
  let fadeFrame = 0;
  function thumbIn(node: Element, u: string) {
    const img = node as HTMLImageElement;
    if (seenThumbs.has(u)) {
      img.classList.add('in', 'now');
      return;
    }
    seenThumbs.add(u);
    img.decode().then(
      () => {
        // (its tile is waiting for the screen's one motion: the picture is simply there when that starts)
        if (gate?.tiles.has(img.closest('.tile') as HTMLElement)) {
          img.classList.add('in', 'now');
          return;
        }
        fading.add(img);
        fadeFrame ||= requestAnimationFrame(() => {
          fadeFrame = 0;
          for (const i of fading) i.classList.add('in');
          fading.clear();
        });
      },
      () => img.classList.add('in'), // (can't be decoded ahead - e.g. let go of: shown as before)
    );
  }
  function trackSpeed() {
    if (!el) return;
    const now = performance.now();
    const top = topNow();
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
      if (m?.kind === 'open') topOutside = topNow();
      const top = m?.kind === 'close' ? topOutside : 0;
      moveTo(top);
      scrollTop = top;
      lead = 0;
      selection.clear();
    }
  });

  /** The grid moving itself (following the viewer, keeping arrivals still): not
   *  the user scrolling - not a flick (trackSpeed reads data-quiet-until). */
  function scrollSelf(top: number) {
    if (!el) return;
    el.dataset.quietUntil = String(performance.now() + 300);
    moveTo(top);
  }

  /** Scrolls so the tile at index is visible (used when the viewer closes or swipes). */
  export function reveal(index: number) {
    if (!el || !rowH) return;
    const p = place(index + lead);
    const top = el.scrollTop - pad + inset; // the list's part not under the bars
    if (p.y < top || p.y + p.h + GAP > el.scrollTop - pad + height) {
      scrollSelf(p.y + base + pad - inset - (height - inset - p.h) / 2);
    }
  }

  /** The position of the first file on screen (not the rows kept ready above it): what arrivals are counted against -
   *  new files ahead of it go in as rows above, so it and everything after it stay where they are on screen. */
  export function firstOnScreen(): number {
    if (!el || !rowH) return 0;
    if (layout) return visible(layout, Math.max(0, listTop), Math.max(0, listTop) + 1)[0] ?? 0;
    return Math.max(0, Math.floor(Math.max(0, listTop) / rowH) * cols - lead);
  }

  /** Whether the grid is scrolled to the very top (new files can be added there without moving anything). */
  export function atTop() {
    return !el || topNow() < (rowH || 100) / 2;
  }

  // The last press, release or pointer move on the grid (performance.now()): someone aiming at a tile.
  let handledAt = -Infinity;
  const QUIET = 1000;
  /** Whether arriving files can go in now with nothing landing on the wrong tile (2026-10-28, the user: "clicking on a
   *  thumbnail while files are being added results in the wrong file opening" -> only when it's safe): at the very
   *  top, nothing pressed, no menu, hover card, scrub or fast scroller, and no scroll, press or pointer move on the
   *  grid in the last second. Else they wait (the library and comics pages look again). */
  export function quiet(): boolean {
    const now = performance.now();
    return atTop() && !press && !menu && !hover && !scrubbing && !fastDrag && !flinging && now - scrolledAt > QUIET && now - handledAt > QUIET;
  }

  /** n files came in at the top of the list: they go in as whole rows (filling
   *  the first row's gaps first) and the view moves with them, so nothing on
   *  screen moves. */
  export async function addAtTop(n: number) {
    if (n <= 0 || !cols) return;
    if (layout) return masonryAddAtTop(n);
    // At the very top: it stays there and the new files come into view - the list-change animation (the same motion
    // as every other change: they appear, the others slide along to make room).
    if (el && topNow() < (rowH || 100) / 2) {
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
      scrollSelf(topNow() + added * rowH);
      scrollTop = topNow();
    }
  }

  /** Masonry: at the top, the new files come into view (the list-change
   *  animation slides the others along); further down, the first tile on
   *  screen keeps its place on screen (every file below the new ones moves in
   *  the new layout, so the view follows it). */
  async function masonryAddAtTop(n: number) {
    lead = 0;
    if (!el || topNow() < (rowH || 100) / 2) return;
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
    scrollSelf(place(anchor.i + n).y + base - anchor.off);
    scrollTop = topNow();
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
  // (another page's list on screen - stale: see below; declared here, the effect just after reads it at once)
  let cleared = false;
  let clearedView = $state(false);
  let wasHidden = false; // (and the first showing's: see "Shown again" below)
  let firstShown = false;
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
      if (skip || cleared || !el || !size || reduced()) return;
      flip = snapshot();
      if (morphing && hover) {
        // (its copy takes over from the card: no card shrinking back under the comics)
        snapBack = true;
        leaveGrid();
        requestAnimationFrame(() => requestAnimationFrame(() => (snapBack = false)));
      }
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
  // ---- One motion for everything that changes what's on screen (2026-11-02, the user: "multiple animations interfering
  // with each other", "the first line animates differently than the rest"): a stagger capped at 12 tiles rippled through
  // the first row only, and each picture ran a fade of its own on top of its tile's whenever it finished loading. Now
  // there is one moment and one length: nothing moves until the new tiles' pictures are ready (at most MOTION.gate ms),
  // then new tiles fade in, tiles that stay slide and removed ones fade out, all together. Opacity only, no scaling, no
  // stagger. A picture still loading after that fades in over its color preview, as when scrolling. Reduced motion: none.
  const MOTION = {
    ease: 'cubic-bezier(0.2, 0.8, 0.2, 1)',
    fade: 'ease-out',
    time: 260,
    gate: 350,
  };
  let unstacking = $state(false); // (a collection opening or closing)
  /** The motion being held back until the pictures are ready. */
  let gate: { anims: Animation[]; tiles: Set<HTMLElement>; timer: number } | null = null;
  function hold(anims: Animation[], a: Animation) {
    a.pause();
    anims.push(a);
    return a;
  }
  /** A tile's picture is in (or it has none to wait for). */
  function pictureReady(t: HTMLElement) {
    const imgs = t.querySelectorAll('.fit img:not(.zoom-media)');
    if (imgs.length) return [...imgs].every((i) => i.classList.contains('in') || i.classList.contains('failed'));
    const f = results.fileAt(Number(t.dataset.i));
    return !!f && !f.thumb;
  }
  function openGate(anims: Animation[], tiles: HTMLElement[]) {
    if (gate) {
      gate.anims.push(...anims);
      for (const t of tiles) gate.tiles.add(t);
      return;
    }
    if (!tiles.length) {
      for (const a of anims) a.play();
      return;
    }
    const g = { anims, tiles: new Set(tiles), timer: 0 };
    const go = () => {
      if (gate !== g) return;
      clearTimeout(g.timer);
      gate = null;
      for (const a of g.anims) a.play();
    };
    gate = g;
    g.timer = window.setTimeout(go, MOTION.gate);
    const check = () => {
      if (gate !== g) return;
      if ([...g.tiles].every(pictureReady)) go();
      else requestAnimationFrame(check);
    };
    requestAnimationFrame(check);
  }
  /** The motion held back goes straight to its end: another change is about to be measured. */
  function closeGate() {
    if (!gate) return;
    clearTimeout(gate.timer);
    const anims = gate.anims;
    gate = null;
    for (const a of anims) a.finish();
  }
  /** A new tile's one animation (its place stays its own: only the opacity). */
  function enter(anims: Animation[], t: HTMLElement) {
    const a = t.animate([{ opacity: 0 }, { opacity: 1 }], { duration: MOTION.time, easing: MOTION.fade });
    a.id = 'enter';
    hold(anims, a);
  }
  const entering = (t: HTMLElement) => t.getAnimations().some((a) => a.id === 'enter');
  /** Where every tile on screen is now (by file), with a copy to fade out if it goes. */
  function snapshot(): Map<number, Snap> {
    closeGate();
    const snap = new Map<number, Snap>();
    if (!el) return snap;
    const box = el.getBoundingClientRect();
    for (const t of el.querySelectorAll<HTMLElement>('.sizer > .tile')) {
      const id = Number(t.dataset.id);
      if (!t.dataset.id || Number.isNaN(id)) continue;
      if (isCard(t)) {
        // (the hover card has its own motion - but a collection clicked is one, with a mouse: the pile starts from the
        // card, where it's drawn - from its tile's place it jumped, the user: "the hover zoom thing looks weird")
        if (id !== morphing?.id) continue;
        const rc = t.getBoundingClientRect();
        const ghost = still(t.cloneNode(true) as HTMLElement);
        ghost.querySelectorAll('.zoom-media, .preview, .preview-line').forEach((n) => n.remove());
        ghost.classList.remove('zoomed', 'shrinking');
        for (const n of [ghost, ...ghost.querySelectorAll<HTMLElement>('.fit, .comic-foot, .deck, .comic-status')]) n.style.translate = n.style.scale = n.style.right = n.style.width = '';
        Object.assign(ghost.style, { width: rc.width + 'px', height: rc.height + 'px', margin: '0', borderRadius: '10px' });
        ghost.style.setProperty('--r', '10px');
        snap.set(id, { x: rc.left, y: rc.top, w: rc.width, h: rc.height, ghost });
        continue;
      }
      const rc = t.getBoundingClientRect();
      if (rc.bottom < box.top || rc.top > box.bottom) continue; // (the rows kept ready off screen)
      snap.set(id, { x: rc.left, y: rc.top, w: rc.width, h: rc.height, ghost: still(t.cloneNode(true) as HTMLElement) });
    }
    return snap;
  }
  /** The tiles on screen now against before: slides, appearances, departures (MOTION). keep(id): a file not on
   *  screen any more that is still in the list (scrolled out of sight: no departure). */
  function animateFrom(before: Map<number, Snap>, keep: (id: number) => boolean = () => false) {
    if (!el || reduced()) return;
    const sizer = el.querySelector<HTMLElement>('.sizer');
    if (!sizer) return;
    const sr = sizer.getBoundingClientRect();
    const box = el.getBoundingClientRect();
    const tr = (x: number, y: number) => `translate(${x}px, ${y}px)`;
    const anims: Animation[] = [];
    const newTiles: HTMLElement[] = [];
    const comeIn = (t: HTMLElement) => {
      if (entering(t)) return;
      enter(anims, t);
      newTiles.push(t);
    };
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
      // (the rows kept ready off screen: nothing to see - no motion)
      const onScreen = sr.top + p.y + p.h > box.top && sr.top + p.y < box.bottom;
      if (!b) {
        if (onScreen) comeIn(t);
        continue;
      }
      const ox = b.x - sr.left;
      const oy = b.y - sr.top;
      if (Math.abs(ox - p.x) <= 0.5 && Math.abs(oy - p.y) <= 0.5 && Math.abs(b.w - p.w) <= 0.5) continue;
      // From far away (more than a screen and a half): it leaves and appears, rather than sweeping across the screen.
      // (Into the next row it slides, diagonally - the reshuffle reads as one.)
      if (Math.abs(oy - p.y) > box.height * 1.5) {
        hold(anims, depart(sizer, b, sr));
        comeIn(t);
        continue;
      }
      const sizeChange = Math.abs(b.w - p.w) > 0.5 || Math.abs(b.h - p.h) > 0.5;
      if (sizeChange) {
        // (rows: a tile's size changes with its row - it grows or shrinks from where it was)
        t.style.transformOrigin = '0 0';
        const { frames, scales } = baked({ x: ox, y: oy, sx: b.w / p.w, sy: b.h / p.h }, { x: p.x, y: p.y, sx: 1, sy: 1 }, bezier(MOTION.ease));
        const o: KeyframeAnimationOptions = { duration: MOTION.time, easing: 'linear' };
        for (const a of holdInside(t, scales, o)) hold(anims, a);
        hold(anims, t.animate(frames, o)).finished.finally(() => (t.style.transformOrigin = ''));
      } else {
        hold(anims, t.animate([{ transform: tr(ox, oy) }, { transform: tr(p.x, p.y) }], { duration: MOTION.time, easing: MOTION.ease }));
      }
    }
    for (const [id, b] of before) if (!keep(id)) hold(anims, depart(sizer, b, sr));
    openGate(anims, newTiles);
  }
  /** Every tile on screen comes in (a grid shown again or for the first time, and its first results). */
  function appearAll() {
    if (!el || reduced()) return;
    const box = el.getBoundingClientRect();
    const anims: Animation[] = [];
    const list: HTMLElement[] = [];
    for (const t of el.querySelectorAll<HTMLElement>('.sizer > .tile')) {
      const r = t.getBoundingClientRect();
      if (r.bottom <= box.top || r.top >= box.bottom || !t.dataset.c || isCard(t) || entering(t)) continue;
      enter(anims, t);
      list.push(t);
    }
    openGate(anims, list);
  }
  // Another page's list (stale: the library on the way to its trash, or opened from another screen with the library's
  // list still in the store): never shown here. What was on screen fades out where it is, all of it at once, and only
  // then do this page's tiles come in; if they come sooner, they wait hidden (2026-10-28, the user's recording: copies of the library's tiles leaving came back
  // a moment later at full strength, moved down by the trash's header, over the trash's own tiles appearing).
  let leaving: Animation | null = null;
  $effect.pre(() => {
    const s = stale;
    untrack(() => {
      const sizer = el?.querySelector<HTMLElement>('.sizer');
      if (s && !cleared) {
        cleared = true;
        closeGate();
        if (!sizer || reduced() || !tiles.length) {
          clearedView = true;
          return;
        }
        leaving = sizer.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 200, easing: MOTION.fade, fill: 'forwards' }); // (a little quicker: nothing comes in until it's gone)
        leaving.finished.then(() => cleared && (clearedView = true), () => {});
      } else if (!s && cleared) {
        cleared = false;
        firstShown = true; // (the appearing below is this list's: not again as a first showing)
        wasHidden = false;
        clearedView = true; // (this page's tiles hidden until the old ones have gone)
        const show = () => {
          if (cleared) return; // (stale again meanwhile)
          leaving?.cancel();
          leaving = null;
          clearedView = false;
          tick().then(appearAll);
        };
        if (leaving?.playState === 'running') leaving.finished.then(show, show);
        else show();
      }
    });
  });
  // Shown again (the other page was on screen: this one hidden, no size), or shown with its first results. Where it
  // was scrolled comes back with it: a hidden element forgets (the user: Browse "resets every time you go to it").
  let keptTop = 0;
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
        tick().then(appearAll);
      } else if (wasHidden) {
        wasHidden = false;
        if (el && keptTop) {
          moveTo(keptTop);
          scrollTop = topNow();
        }
        tick().then(appearAll);
      }
    });
  });
  const isCard = (t: HTMLElement) => t.classList.contains('zoomed') || t.classList.contains('shrinking');
  /** A tile leaving: its copy fades out where it was - under the tiles, and late, so one coming in over it never shows
   *  the empty place between the two. */
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
    const a = ghost.animate(
      [{ opacity: 1, transform: at }, { opacity: 1, transform: at, offset: 0.4 }, { opacity: 0, transform: at }],
      { duration: MOTION.time, easing: 'linear', fill: 'forwards' },
    );
    a.finished.finally(() => ghost.remove());
    return a;
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

  /** A collection opening unstacks (2026-11-02, the user: "it should have an unstacking effect", quick, no tilt): its
   *  comics start as the pile its tile stands for - the first on top - and are dealt out one after another to their
   *  places, the other tiles fading away. Closing stacks them back the same way, the other tiles coming back. Held until
   *  the pictures are in, as every change (openGate); no empty boxes under them meanwhile (unstacking). False when its
   *  tile isn't on screen (the usual animation then). */
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
    const anims: Animation[] = [];
    const COVER_FADE = 110; // (the collection's own tile, coming or going)
    const DECK = 110; // (the edges above it, sliding behind it or out)
    const settled = (list: Animation[]) => {
      unstacking = true;
      Promise.allSettled(list.map((a) => a.finished)).then(() => (unstacking = false));
    };
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
    const fadeOut = (g: Snap) => {
      const at = tr(g.x - sr.left, g.y - sr.top);
      const a = hold(anims, ghostOf(g).animate([{ opacity: 1, transform: at }, { opacity: 0, transform: at }], { duration: MOTION.time, easing: MOTION.fade, fill: 'forwards' }));
      a.finished.finally(() => g.ghost.remove());
    };
    if (morph.kind === 'open') {
      const from = before.get(morph.id);
      if (!from) return false;
      const ox = from.x - sr.left;
      const oy = from.y - sr.top;
      for (const [id, g] of before) if (id !== morph.id) fadeOut(g);
      // Its tile - the covers side by side - is in front: its edges slide behind it, then it fades as the first comic
      // leaves from under it (kept until the last had gone, it lingered - the user).
      const list = [...now].sort((p, q) => p[1].c - q[1].c);
      const tile = ghostOf(from);
      tile.style.zIndex = String(list.length + 2);
      const at = tr(ox, oy);
      for (const edge of tile.querySelectorAll('.deck > span')) hold(anims, edge.animate([{ translate: '0 0' }, { translate: '0 9px' }], { duration: DECK, easing: 'ease-in', fill: 'forwards' }));
      hold(anims, tile.animate([{ opacity: 1, transform: at }, { opacity: 0, transform: at }], { duration: COVER_FADE, delay: DECK * 0.6, easing: 'ease-out', fill: 'both' })).finished.finally(() => tile.remove());
      list.forEach(([, a], n) => {
        const [x, y] = xy(a.c);
        const w = a.el.offsetWidth || 1;
        const h = a.el.offsetHeight || 1;
        a.el.style.transformOrigin = '0 0';
        a.el.style.zIndex = String(list.length - n + 1); // (the first on top of the pile)
        const { frames, scales } = baked({ x: ox, y: oy, sx: from.w / w, sy: from.h / h }, { x, y, sx: 1, sy: 1 }, bezier(ease));
        const o: KeyframeAnimationOptions = { duration: 240, delay: DECK * 0.6 + Math.min(n, 10) * 16, easing: 'linear', fill: 'backwards' };
        for (const part of holdInside(a.el, scales, o)) hold(anims, part);
        hold(anims, a.el.animate(frames, o)).finished.finally(() => {
          a.el.style.transformOrigin = '';
          a.el.style.zIndex = '';
        });
      });
      settled(anims);
      openGate(anims, list.map(([, a]) => a.el));
      return true;
    }
    const into = now.get(morph.id);
    if (!into) return false;
    const [tx, ty] = xy(into.c);
    const tw = into.el.offsetWidth;
    const th = into.el.offsetHeight;
    // The last ones first, so the first lands on top.
    const going = [...before].sort((p, q) => q[1].y - p[1].y || q[1].x - p[1].x);
    going.forEach(([, g], n) => {
      const ghost = ghostOf(g);
      ghost.style.zIndex = String(n + 2);
      const { frames, scales } = baked({ x: g.x - sr.left, y: g.y - sr.top, sx: 1, sy: 1 }, { x: tx, y: ty, sx: tw / g.w, sy: th / g.h }, bezier(ease));
      const o: KeyframeAnimationOptions = { duration: 220, delay: Math.min(n, 10) * 12, easing: 'linear', fill: 'both' };
      for (const part of holdInside(ghost, scales, o)) hold(anims, part);
      hold(anims, ghost.animate(frames, o)).finished.finally(() => ghost.remove());
    });
    // The collection's tile is in front from the start (the user: "should always be in front"), the comics going in
    // under it; once they're in, its edges slide out from behind it. The others come back.
    const last = Math.min(going.length - 1, 10) * 12 + 220;
    const tileAt = tr(tx, ty);
    into.el.style.zIndex = String(going.length + 3);
    hold(anims, into.el.animate([{ opacity: 0, transform: tileAt }, { opacity: 1, transform: tileAt }], { duration: COVER_FADE, easing: 'ease-out', fill: 'backwards' }));
    for (const edge of into.el.querySelectorAll('.deck > span')) hold(anims, edge.animate([{ translate: '0 9px' }, { translate: '0 0' }], { duration: DECK, delay: Math.max(0, last - 40), easing: 'ease-out', fill: 'backwards' }));
    const lifted = into.el;
    Promise.allSettled(anims.map((a) => a.finished)).then(() => (lifted.style.zIndex = ''));
    const others: HTMLElement[] = [into.el];
    for (const [id, a] of now) {
      if (id === morph.id) continue;
      enter(anims, a.el);
      others.push(a.el);
    }
    settled(anims);
    openGate(anims, others);
    return true;
  }

  // The first row's gaps are only there to keep what's on screen still; once
  // that row comes into view they close, the tiles sliding into place (a tile
  // that changes rows fades in at its new place).
  $effect(() => {
    if (lead > 0 && rowH && scrollTop < rowH - 1) closeGaps();
  });
  function closeGaps() {
    const before = reduced() ? null : snapshot();
    lead = 0;
    if (before) tick().then(() => animateFrom(before, (id) => results.indexOf(id) >= 0));
  }

  /** Back to the top (smoothly, unless reduced motion). */
  export function toTop(instant = false) {
    if (base) moveTo(0); // (a window further down: straight there)
    else el?.scrollTo({ top: 0, behavior: instant || reduced() ? 'auto' : 'smooth' });
  }

  /** The rounded corners of the tile at index (px; 0 when square or not on screen) - a picture flying to or from it
   *  rounds or squares its corners on the way. */
  export function tileRadius(index: number): number {
    const t = el?.querySelector<HTMLElement>(`.tile[data-i="${index}"]`);
    return t ? parseFloat(getComputedStyle(t).borderTopLeftRadius) || 0 : 0;
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
    if (layout) return Math.max(0, Math.min(results.total - 1, masonryIndexAt(layout, clientX - r.left - side, clientY - r.top - pad + topNow())));
    const col = Math.min(cols - 1, Math.max(0, Math.floor((clientX - r.left) / (size + GAP))));
    const row = Math.max(0, Math.floor((clientY - r.top - pad + topNow()) / rowH));
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
    // scrubbing the hover card's video (see scrubTo): the place (f), and where the pointer last was (x)
    scrub?: { video: HTMLVideoElement; resume: boolean; x: number; f: number };
  };
  let press: Press | null = null;
  let longTimer: number | undefined;
  let dragBase: number[] = [];
  let dragTo = -1;
  let lastPoint = { x: 0, y: 0 };
  // (fid: the file it was opened on - its place is looked up again when it's used, in case the list changed)
  let menu = $state<{ i: number; fid?: number; x: number; y: number } | null>(null);

  function tileIndex(e: Event): number | null {
    const t = (e.target as HTMLElement).closest<HTMLElement>('[data-i]');
    return t ? Number(t.dataset.i) : null;
  }

  function down(e: PointerEvent) {
    handledAt = performance.now();
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
    if (press.touch) {
      longTimer = window.setTimeout(() => {
        if (!press) return;
        press.long = true;
        navigator.vibrate?.(10);
      }, 450);
    }
    // With a mouse, held still a moment: picked up, to drop on a collection (moved at once: selecting, as ever).
    clearTimeout(holdTimer);
    if (!press.touch && kind === 'comics' && !results.collection && results.scope !== 'trash' && collections.list.length && results.file(press.fid)?.comic && !results.file(press.fid)?.comic?.stack) {
      holdTimer = window.setTimeout(startLift, HOLD_MS);
    }
  }

  // ---- A comic picked up (2026-11-02, the user: "click and hold ... the same tilted effect with a border"): a copy of
  // its tile follows the pointer, tilted, its place left faint; let go on a collection's tile it goes in (with the rest
  // of the selection, if it's one of them); anywhere else, or Escape, it goes back.
  const HOLD_MS = 300;
  let holdTimer = 0;
  let lift = $state<{ id: number; ids: number[]; over: number | null } | null>(null);
  let landing = $state<number | null>(null); // (going back: its copy on the way, the tile itself not there until it lands - both showed)
  let liftGhost: HTMLElement | null = null;
  let liftFrom = { x: 0, y: 0 };
  function startLift() {
    const p = press;
    if (!p || p.dragging || !el || p.fid === undefined) return;
    const tile = el.querySelector<HTMLElement>(`.sizer > .tile[data-id="${p.fid}"]`);
    const sizer = el.querySelector<HTMLElement>('.sizer');
    if (!tile || !sizer) return;
    leaveGrid();
    const home = place(p.i + lead);
    const sr = sizer.getBoundingClientRect();
    const ghost = still(tile.cloneNode(true) as HTMLElement);
    ghost.querySelectorAll('.zoom-media, .preview, .preview-line').forEach((n) => n.remove());
    ghost.classList.remove('zoomed', 'shrinking');
    ghost.classList.add('lift-ghost');
    ghost.removeAttribute('data-i');
    ghost.removeAttribute('data-id');
    ghost.setAttribute('aria-hidden', 'true');
    ghost.tabIndex = -1;
    Object.assign(ghost.style, { left: sr.left + home.x + 'px', top: sr.top + home.y + 'px', width: home.w + 'px', height: home.h + 'px', margin: '0', transform: 'none', visibility: 'visible' });
    document.body.appendChild(ghost);
    if (!reduced()) ghost.animate([{ rotate: '0deg', scale: '1' }, { rotate: '-2deg', scale: '1.04' }], { duration: 150, easing: MOTION.ease, fill: 'forwards' });
    liftGhost = ghost;
    liftFrom = { x: p.x, y: p.y };
    const ids = selection.has(p.fid) ? selection.list().filter((id) => !results.file(id)?.comic?.stack) : [p.fid];
    lift = { id: p.fid, ids, over: null };
    moveLift(lastPoint.x, lastPoint.y);
    try {
      el.setPointerCapture(p.id);
    } catch {
      /* fine */
    }
    autoScroll();
  }
  function moveLift(x: number, y: number) {
    if (!lift || !liftGhost) return;
    liftGhost.style.translate = `${x - liftFrom.x}px ${y - liftFrom.y}px`;
    const under = document.elementFromPoint(x, y)?.closest<HTMLElement>('.sizer > .tile[data-i]');
    lift.over = under ? (results.fileAt(Number(under.dataset.i))?.comic?.stack?.id ?? null) : null;
  }
  /** Let go: into the collection under it (it shrinks into its tile), else back to its place. */
  function endLift(drop: boolean) {
    const l = lift;
    const ghost = liftGhost;
    lift = null;
    liftGhost = null;
    if (!l || !ghost) return;
    const into = drop ? l.over : null;
    const to = el?.querySelector<HTMLElement>(into !== null ? `.sizer > .tile[data-id="${results.idAt(tileOfStack(into))}"]` : `.sizer > .tile[data-id="${l.id}"]`)?.getBoundingClientRect();
    const box = ghost.getBoundingClientRect();
    ghost.getAnimations().forEach((a) => a.cancel());
    const end = () => {
      ghost.remove();
      if (landing === l.id) landing = null;
    };
    if (to && !reduced()) {
      if (into === null) landing = l.id;
      const left = parseFloat(ghost.style.left);
      const top = parseFloat(ghost.style.top);
      const shrink = into !== null ? 0.35 : 1;
      const at = (x: number, y: number) => `${x}px ${y}px`;
      ghost
        .animate(
          [
            { translate: ghost.style.translate, scale: '1.04', rotate: '-2deg', opacity: 1 },
            { translate: at(to.left + to.width / 2 - left - box.width / 2 + (box.width - parseFloat(ghost.style.width)) / 2, to.top + to.height / 2 - top - parseFloat(ghost.style.height) / 2), scale: String(shrink), rotate: '0deg', opacity: into !== null ? 0 : 1 },
          ],
          { duration: MOTION.time, easing: MOTION.ease, fill: 'forwards' },
        )
        .finished.finally(end);
    } else end();
    if (into !== null) {
      collections.add(l.ids, into).then((ok) => {
        if (ok && l.ids.length > 1) selection.clear();
      });
    }
  }
  /** Where a collection's tile is in the list (-1: not loaded). */
  function tileOfStack(k: number) {
    for (const t of tiles) if (t.f?.comic?.stack?.id === k) return t.i;
    return -1;
  }
  $effect(() => {
    if (!lift) return;
    return on(window, 'keydown', (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      press = null;
      endLift(false);
    });
  });

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
    if (lift) {
      moveLift(e.clientX, e.clientY);
      autoScroll();
      return;
    }
    if (press.scrub) {
      scrubTo(e);
      return;
    }
    const d = Math.hypot(e.clientX - press.x, e.clientY - press.y);
    if (d > 8) clearTimeout(holdTimer);
    // Pressed on the hover card's video: moved sideways, it scrubs (never a drag to select).
    const video = !press.touch && hover?.i === press.i ? cardVideo() : null;
    if (video && !press.dragging) {
      if (Math.abs(e.clientX - press.x) > 4 && video.duration > 0) {
        const at = held ?? video.currentTime / video.duration;
        press.scrub = { video, resume: !video.paused, x: press.x, f: at };
        seekPending = null;
        video.addEventListener('seeked', scrubSeeked);
        video.pause();
        scrubbing = true;
        try {
          el?.setPointerCapture(press.id);
          // The cursor kept on the card (hidden until let go): see scrubTo. (Refused: it just stays free.)
          lockPending = true;
          Promise.resolve(el?.requestPointerLock()).catch(() => {});
        } catch {
          /* fine */
        }
        scrubTo(e);
      }
      return;
    }
    if (!press.dragging) {
      if (press.touch && !press.long) {
        if (d > 10) {
          // A scroll, not a press.
          clearTimeout(longTimer);
          press = null;
        }
        return;
      }
      if (d > 8) startDrag();
      return;
    }
    const i = indexAt(e.clientX, e.clientY);
    if (i !== dragTo) {
      dragTo = i;
      applyDrag();
    }
    autoScroll();
  }

  // ---- Scrubbing the hover card's video (2026-10-28, the user): press on it and move sideways - the card's width is
  // the whole video (the line along its foot shows where); let go and it plays on from there. It moves on from where
  // the video was, by how far the pointer goes (2026-10-31, the user: "it should start from its current position in
  // the video, not where my mouse is"); pushed past an end, it moves back from that end at once. A click that doesn't
  // move still opens the viewer; the card stays while scrubbing, wherever the pointer goes.
  // The cursor can't leave the card while scrubbing (2026-11-02, the user: "the cursor gets trapped inside the thumbnail
  // so when i let go it doesnt stop playing"): pointer lock - hidden, only its movement reported, back where it was on
  // the card when let go. Where the browser refuses the lock, the cursor stays free and letting go off the card closes it.
  // The line shows where the hand is, not where the video's seeks have got to: their time updates, lagging behind,
  // pulled it back and forth while scrubbing fast (the user), and after letting go the exact place is held until the
  // video is there (held).
  /** The hover card's video, once it has a frame. */
  const cardVideo = () => el?.querySelector<HTMLVideoElement>('.tile.zoomed video.zoom-media') ?? null;
  let seekPending: number | null = null;
  let held: number | null = null; // let go: the place shown until the video gets there
  const MAX_MOVE = 200; // px in one pointer event
  let lockPending = false; // the cursor's lock asked for, not there (or refused) yet
  // A lock granted after letting go (a quick scrub): given straight back - the cursor would stay hidden until Esc.
  $effect(() => {
    const answered = () => {
      lockPending = false;
      if (document.pointerLockElement === el && !press?.scrub) document.exitPointerLock();
    };
    document.addEventListener('pointerlockchange', answered);
    document.addEventListener('pointerlockerror', answered);
    return () => {
      document.removeEventListener('pointerlockchange', answered);
      document.removeEventListener('pointerlockerror', answered);
    };
  });
  function scrubTo(e: PointerEvent) {
    const s = press?.scrub;
    if (!s) return;
    const card = s.video.closest<HTMLElement>('.tile');
    if (!card) return;
    const r = card.getBoundingClientRect();
    // (locked: the pointer's position stands still, only its movement is told)
    const moved = document.pointerLockElement ? e.movementX : e.clientX - s.x;
    s.x = e.clientX;
    // As the lock starts, Chrome moves the pointer to the middle of the window - one event, reported as a leap just
    // before the lock is: it sent the video to its end whenever the card was far from the middle (seen in a log of a
    // real mouse: 121 to 1281). Any leap in that moment isn't the hand; nor one no hand could make in one event.
    const by = Math.abs(moved) > (lockPending ? 30 : MAX_MOVE) ? 0 : moved;
    const f = Math.min(1, Math.max(0, s.f + by / r.width)); // (pushed past an end: back from that end at once)
    s.f = f;
    hoverProgress = f;
    // One seek at a time, only the latest place kept (a seek on top of another queues up and lags behind the hand):
    // the scrub's own 'seeked' listener takes the next (scrubSeeked).
    const t = f * s.video.duration;
    if (s.video.seeking) seekPending = t;
    else seekVideo(s.video, t);
  }
  function seekVideo(v: HTMLVideoElement & { fastSeek?: (t: number) => void }, at: number) {
    if (v.fastSeek) v.fastSeek(at);
    else v.currentTime = at;
  }
  function scrubSeeked(e: Event) {
    if (seekPending === null) return;
    const next = seekPending;
    seekPending = null;
    seekVideo(e.currentTarget as HTMLVideoElement, next);
  }

  // While dragging near the top or bottom edge, scroll and keep selecting.
  let scrollRaf = 0;
  function autoScroll() {
    if (scrollRaf || !el || !(press?.dragging || lift)) return;
    const step = () => {
      scrollRaf = 0;
      if (!el || !(press?.dragging || lift)) return;
      const r = el.getBoundingClientRect();
      const edge = 60;
      let v = 0;
      const top = r.top + inset;
      if (lastPoint.y < top + edge) v = -((top + edge - lastPoint.y) / edge) * 18;
      else if (lastPoint.y > r.bottom - edge) v = ((lastPoint.y - (r.bottom - edge)) / edge) * 18;
      if (!v) return;
      el.scrollTop += v;
      if (lift) {
        moveLift(lastPoint.x, lastPoint.y);
        scrollRaf = requestAnimationFrame(step);
        return;
      }
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
    handledAt = performance.now();
    if (!press || e.pointerId !== press.id) return;
    clearTimeout(longTimer);
    clearTimeout(holdTimer);
    const p = press;
    press = null;
    if (lift) {
      endLift(e.type !== 'pointercancel');
      return;
    }
    if (p.scrub) {
      // (let go: it plays on from there if it was playing - and the viewer doesn't open; the card held a frame longer:
      // WebKit sends the grid a leave as it lets the pointer go, which closed the card and opened it again from the start)
      const free = () => requestAnimationFrame(() => (scrubbing = false));
      if (document.pointerLockElement) {
        // (the cursor back, on the card - and the card held until it is: that leave comes once the lock has gone)
        document.addEventListener('pointerlockchange', free, { once: true });
        setTimeout(free, 500); // (never stuck)
        document.exitPointerLock();
      } else free();
      const v = p.scrub.video;
      v.removeEventListener('seeked', scrubSeeked);
      seekPending = null;
      // The exact place (a quick seek lands on the nearest keyframe - before or after the hand), held on the line until
      // the video is there: a seek still on its way finishing first doesn't count (the video is seeking again then).
      const f = p.scrub.f;
      held = f;
      const landed = () => {
        if (v.seeking) return;
        v.removeEventListener('seeked', landed);
        if (held === f) held = null;
      };
      v.addEventListener('seeked', landed);
      setTimeout(() => {
        v.removeEventListener('seeked', landed);
        if (held === f) held = null;
      }, 1500); // (never stuck)
      v.currentTime = f * v.duration;
      if (p.scrub.resume) v.play().catch(() => {});
      return;
    }
    if (p.dragging) {
      // Fill in any part of the range whose IDs weren't loaded during the drag.
      const ids = await results.idsInRange(p.i, dragTo);
      selection.add(ids.filter((x) => !unpickable(results.file(x))));
      return;
    }
    if (p.long) {
      menu = { i: p.i, fid: p.fid, x: p.x, y: p.y };
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
    const picking = selection.active || e.ctrlKey || e.metaKey || (e.shiftKey && selection.anchor !== null);
    if (picking && unpickable(results.file(id))) return;
    if (e.shiftKey && selection.anchor !== null) {
      selection.add((await results.idsInRange(selection.anchor, i)).filter((x) => !unpickable(results.file(x))));
      selection.anchor = i;
    } else if (e.ctrlKey || e.metaKey || selection.active) {
      selection.toggle(id, i);
    } else {
      onopen(i);
    }
  }

  function contextmenu(e: MouseEvent) {
    handledAt = performance.now();
    const i = tileIndex(e);
    if (i === null) return;
    e.preventDefault();
    if (press?.touch) return; // touch long-press opens the menu itself (Android also sends this)
    menu = { i, fid: results.idAt(i), x: e.clientX, y: e.clientY };
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
    if (results.scope !== 'trash') items.push({ label: t('comics.rename'), icon: 'pencil', run: () => (renaming = c) });
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
  // Renaming a comic from its menu (the same dialog as its details'): the tile follows at once.
  let renaming = $state<NonNullable<FileInfo['comic']> | null>(null);
  async function rename(c: NonNullable<FileInfo['comic']>, value: string) {
    renaming = null;
    const original = c.site_title ?? c.title;
    const title = value || original;
    if (title === c.title && !!value === !!c.site_title) return;
    await attempt(async () => {
      await api.updateComic(c.id, { title: value });
      results.patch(c.id, { comic: { ...c, title, site_title: value && value !== original ? original : undefined } });
      toasts.show(value ? t('comics.renamed', { what: itemName(c.id, true) }) : t('comics.titleBack'), {
        cat: 'comicEdits',
        action: {
          label: t('common.undo'),
          run: async () => {
            await api.updateComic(c.id, { title: c.site_title ? c.title : '' });
            results.patch(c.id, { comic: c });
          },
        },
      });
    });
  }
  /** A gallery on Browse: its menu (details, download, select). */
  function galleryItems(i: number, id: number | undefined): ComponentProps<typeof FileMenu>['extra'] {
    const c = results.file(id)?.comic;
    if (!c || id === undefined) return [];
    const items: NonNullable<ComponentProps<typeof FileMenu>['extra']> = [];
    if (!c.mark) {
      if (ondownload) items.push({ label: t('backups.download'), icon: 'download', run: () => ondownload(i) });
      items.push({ label: selection.has(id) ? t('common.deselect') : t('common.select'), icon: 'checks', run: () => selection.toggle(id, i) });
    }
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
    const removed = await attempt(async () => {
      await results.remove([id]);
      return true;
    });
    if (!removed) return;
    selection.ids.delete(id);
    toasts.show(t('common.deletedOne', { what: itemName(id, comics) }), { cat: 'deleted',
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

<!-- A comic downloading: the arrow keeps falling into its tray; done, the two give way to a tick drawn in green. -->
{#snippet downloadMark(done: boolean)}
  <span class="comic-status downloading" class:done role="img" aria-label={done ? t('comics.downloaded') : t('browse.queued')} title={done ? t('comics.downloaded') : t('browse.queued')} transition:pop>
    <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <g class="arrow"><path d="M12 3v11" /><path d="M7.5 9.5 12 14l4.5-4.5" /></g>
      <path class="tray" d="M5 20h14" />
      <path class="tick" pathLength="1" d="M5 12.5l4.5 4.5L19 7" />
    </svg>
  </span>
{/snippet}

<div class="wrap">
<div
  class="scroller grid-scroller"
  id={scrollerId}
  class:fast-on={showFast}
  class:comics
  style:padding-top="{pad}px"
  style:padding-bottom="calc({GAP}px + var(--bottom-room))"
  class:selecting={selection.active}
  class:unstacking
  class:scrubbing
  class:cleared={clearedView}
  bind:this={el}
  bind:clientWidth={width}
  bind:clientHeight={height}
  role="presentation"
  onscroll={() => {
    if (listH > MAX_H) moveTo(topNow()); // (nearing the window's edge: it moves)
    scrollTop = topNow();
    if (el!.clientHeight > 0 && !wasHidden) keptTop = scrollTop; // (not the browser's own 0 as it's hidden or shown again)
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
    handledAt = performance.now();
    hoverTile(e);
    move(e);
  }}
  onpointerleave={leaveGrid}
  onpointerup={up}
  onpointercancel={up}
  oncontextmenu={contextmenu}>
  <div class="sizer" style:height="{pageH}px">
    <!-- (the tiles' shapes, under them: what a flick that outruns them shows - see bands; the last one ends with the
         list - a whole band's height went past it, and the page scrolled further than its end) -->
    {#each clearedView ? [] : bands as band (band.b)}
      <div class="band" aria-hidden="true" style:top="{band.b * BAND - base}px" style:height="{Math.max(0, Math.min(BAND, contentH - GAP - band.b * BAND))}px" style:background-image={band.url}></div>
    {/each}
    {#if !clearedView && holeColor}
      {#each holes as p (`${p.x} ${p.y}`)}
        <div class="hole" aria-hidden="true" style:transform="translate({p.x}px, {p.y}px)" style:width="{p.w}px" style:height="{p.h}px" style:background={holeColor}></div>
      {/each}
    {/if}
    {#each tiles as tile (tile.i)}
      {@const z = hover?.i === tile.i ? hover : null}
      {@const back = z ? undefined : shrinking.find((s) => s.i === tile.i)}
      {@const c = z ?? back}
      <button
        class="tile"
        class:selected={selection.has(tile.f?.id)}
        class:unpickable={selection.active && unpickable(tile.f)}
        aria-disabled={selection.active && unpickable(tile.f) ? true : undefined}
        class:rounded={!!layout}
        class:zoomed={!!z}
        class:landed={landedId !== undefined && results.idAt(tile.i) === landedId}
        class:shrinking={!!back}
        class:lift-source={lift?.id === results.idAt(tile.i)}
        class:lift-home={landing !== null && landing === results.idAt(tile.i)}
        class:drop-target={lift !== null && lift.over !== null && lift.over === tile.f?.comic?.stack?.id}
        class:snap={snapBack}
        data-i={tile.i}
        data-c={tile.c}
        data-id={results.idAt(tile.i)}
        style:visibility={hideId !== undefined && results.idAt(tile.i) === hideId ? 'hidden' : undefined}
        style:width="{c ? c.w : tile.p.w}px"
        style:height="{c ? c.h : tile.p.h}px"
        style:transform="translate({tile.p.x}px, {tile.p.y}px)"
        style:margin-left={c ? `${c.x - tile.p.x}px` : undefined}
        style:margin-top={c ? `${c.y - tile.p.y}px` : undefined}
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
        <!-- (the picture: scaled evenly as the card unfolds - see fly) -->
        <span class="fit">
        {#if tile.f?.comic?.mosaic && tile.f.comic.mosaic.length > 1}
          {@const m = tile.f.comic.mosaic}
          <!-- A collection: its first comics' covers side by side (two), one big and two small (three), or four, the tile in
               the tallest cover's shape (the server shapes it); each cover - its crop when it has one - filling its cell. -->
          <span class="mosaic n{m.length}" aria-hidden="true">
            {#each m as cover, ci (ci + ' ' + cover.thumb)}
              {@const shape = cover.w && cover.h ? (cover.w * (cover.crop?.[2] ?? 1)) / (cover.h * (cover.crop?.[3] ?? 1)) : 0}
              <span class="cell"
                ><span class="frame" style:--shape={shape || undefined}
                  ><img src={cover.thumb} alt="" style={shape ? cropStyle(cover.crop) : undefined} decoding="async" draggable="false" onload={(e) => thumbIn(e.currentTarget, cover.thumb)} onerror={retryThumb} /></span
                ></span>
            {/each}
          </span>
        {:else if tile.f?.thumb}
          <!-- (flicking past: the color preview only, unless this thumbnail has been seen - see trackSpeed) -->
          {#if !flinging || seenThumbs.has(tile.f.thumb)}
            {@const u = tile.f.thumb}
            <!-- (a new element for each picture: a tile given another file - a new search - shows none of the last one's,
                 which Chrome kept showing until the new one came, for good if it failed) -->
            {#key u}
            <img
              src={u}
              alt=""
              style={cropStyle(tile.f.comic?.crop)}
              decoding="async"
              draggable="false"
              onload={(e) => thumbIn(e.currentTarget, u)}
              onerror={retryThumb} />
            <!-- Still on its way after a moment (e.g. after a long jump): a small spinner over the preview. -->
            <span class="waiting" aria-hidden="true"></span>
            {/key}
          {/if}
        {:else if tile.f}
          <!-- Just downloaded: its thumbnail is being made (checked again every few seconds). -->
          <span class="making" role="img" aria-label={t('grid.making')}></span>
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
                if (press?.scrub || held !== null) return; // (the hand's place, not the seeks')
                const v = e.currentTarget;
                // (WebKit, playing on after a seek: one stray update at 0 - the line flashed back to the start)
                if (v.currentTime < 0.01 && hoverProgress > 0.05) return;
                hoverProgress = v.duration ? v.currentTime / v.duration : 0;
              }}></video>
          {:else}
            <img class="zoom-media" class:ready={hoverReady} src={previewURL(tile.f)} style={cropStyle(tile.f.comic?.crop)} alt="" draggable="false" onload={() => (hoverReady = true)} />
          {/if}
        {/if}
        </span>
        {#if z && opened === z && tile.f && !tile.f.comic?.stack && tile.f.mime.startsWith('video/') && settings.hoverPreview}
          <span class="preview-line" style:--p={hoverProgress}></span>
        {/if}
        {#if browsing && tile.f?.comic?.mark === 'have'}<span class="had-veil" aria-hidden="true" transition:fade={{ duration: reduced() ? 0 : 200 }}></span>{/if}
        {#if tile.f?.comic}
          {@const c = tile.f.comic}
          {@const coming = c.stack ? undefined : downloading?.get(c.id)}
          {@const done = !coming && !c.stack && (finishing.has(c.id) || results.justDone.has(c.id))}
          <!-- A comic: read (a book with a check) or being read (a book turning) in a corner; its title and
               pages over the cover's foot; how far it's read along the bottom edge. Nothing here to press. -->
          {#if coming || done}
            {@render downloadMark(done)}
          {:else if c.stack}
            <!-- A collection: the edges of two more covers above its own (a deck); read to the end when all of it is. -->
            <span class="deck" aria-hidden="true"><span></span><span></span></span>
            {#if c.stack.count > 0 && c.stack.read >= c.stack.count}<span class="comic-status read" role="img" aria-label={t('comics.finished')} title={t('comics.finished')}><Icon name="bookcheck" size={13} /></span>{/if}
          {:else if c.mark === 'have'}
            <!-- (Comics > Browse: in the library already, or its download waits or runs) -->
            <span class="comic-status read" role="img" aria-label={t('browse.have')} title={t('browse.have')}><Icon name="check" size={13} /></span>
          {:else if c.mark === 'queued'}
            {@render downloadMark(false)}
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
              {:else if coming}
                <span class="comic-chip" title={pagesText(c)}><Icon name="pages" size={12} />{num(coming.done ?? 0)}/{num(Math.max(coming.pages ?? 0, c.pages))}</span>
              {:else}
                <span class="comic-chip" title={pagesText(c)}><Icon name="pages" size={12} />{c.have < c.pages ? `${num(c.have)}/${num(c.pages)}` : num(c.pages)}</span>
              {/if}
            </span>
            <span class="comic-title marquee"><span>{c.title}</span></span>
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
        {#if tile.f && results.isNew(tile.i, tile.f.id)}
          <span class="new-badge" transition:pop>{t('grid.new')}</span>
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
    <FastScroller {el} {scrollTop} {moveTo} contentHeight={contentH - GAP + pad + padBottom} {height} top={pad} total={results.total} dates={results.dates} bind:dragging={fastDrag} side={fastSide} controls={scrollerId} />
  {/if}
</div>

{#if menu}
  {@const m = menu}
  <!-- (where its file is now; -1: gone - then its actions do nothing) -->
  {@const mi = m.fid !== undefined && results.idAt(m.i) !== m.fid ? results.indexOf(m.fid) : m.i}
  {@const id = mi >= 0 ? results.idAt(mi) : undefined}
  <FileMenu
    x={m.x}
    y={m.y}
    selected={selection.has(id)}
    favorite={!!results.file(id)?.favorite}
    onselect={() => id !== undefined && selection.toggle(id, mi)}
    onfavorite={() => id !== undefined && setFavorite([id], !results.file(id)?.favorite, false, results)}
    viewLabel={browsing ? t('fileMenu.details') : results.file(id)?.comic?.stack ? t('fileMenu.open') : comics ? t('comics.read') : undefined}
    viewIcon={results.file(id)?.comic?.stack ? 'stack' : undefined}
    basic={browsing || !!results.file(id)?.comic?.stack}
    extra={browsing ? galleryItems(mi, id) : results.file(id)?.comic?.stack ? stackItems(id) : comicItems(mi, id)}
    onview={() => mi >= 0 && onopen(mi)}
    ondetails={() => mi >= 0 && onopen(mi, true)}
    ondelete={() => mi >= 0 && deleteOne(mi)}
    trashed={results.scope === 'trash'}
    onrestore={() => id !== undefined && restoreFiles(results, [id])}
    onclose={() => (menu = null)} />
{/if}
{#if renaming}
  {@const c = renaming}
  <RenameDialog title={c.title} original={c.site_title} onsave={(v) => rename(c, v)} onclose={() => (renaming = null)} />
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
  /* The keyboard's ring inside the tile's edge, over the picture: outside it (the shared style), the scroller cut it off
     above the top row. */
  .tile:focus-visible {
    outline: none;
  }
  .tile:focus-visible::after {
    content: '';
    position: absolute;
    inset: 0;
    z-index: 10;
    border-radius: inherit;
    box-shadow: inset 0 0 0 2px var(--mauve);
    pointer-events: none;
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
  .fit {
    position: absolute;
    inset: 0;
    display: block;
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
      opacity 0.26s ease-out,
      transform 0.15s;
  }
  img:global(.in) {
    opacity: 1;
  }
  /* (seen before: there at once - see thumbIn) */
  img:global(.now) {
    transition: transform 0.15s;
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
  img:global(.in) + .waiting,
  img:global(.failed) + .waiting {
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
  .tile:not(.selected):has(.fit > :global(img.in), .mosaic :global(img.in)) {
    background: none !important;
  }
  /* Back after the viewer or reader closed (the picture landed on it): its overlays fade in. (An animation of its own
     name: a comic's overlays had run overlay-in when their cover came, and the same animation again doesn't play
     again - they appeared at once as the reader's cover landed, seen in a motion recording.) */
  .tile.landed :is(.comic-foot, .comic-status, .comic-progress, .deck, .badge, .fav-badge, .new-badge) {
    animation: overlay-land 0.3s ease-out both;
  }
  @keyframes overlay-in {
    from {
      opacity: 0;
    }
  }
  @keyframes overlay-land {
    from {
      opacity: 0;
    }
  }
  /* A comic's cover: its overlays (title and chips, marks, progress) wait for its picture and fade in with it - a
     picture the browser had shows at once, and the overlay used to be there before any fade. */
  /* (the cover only - not a hover card's bigger picture, which never gets .in: the title and chips vanished at once as it came) */
  .tile:has(.fit > :global(img:not(.in):not(.zoom-media))) :is(.comic-foot, .comic-status, .comic-progress) {
    opacity: 0;
  }
  .tile:has(.fit > :global(img.in)) :is(.comic-foot, .comic-status, .comic-progress) {
    animation: overlay-in 0.3s ease-out both;
  }
  /* (a cover seen before is there at once: so are its overlays) */
  .tile:has(.fit > :global(img.now)) :is(.comic-foot, .comic-status, .comic-progress) {
    animation: none;
  }
  /* (landed, after the rule above and as weighty: it lost to it - a comic's overlays came at once. overlay-in stays
     in the list, done: when landed goes, an animation list without it started it again - the overlays blinked out
     and faded in a second time) */
  .tile.landed:has(.fit > :global(img.in)) :is(.comic-foot, .comic-status, .comic-progress) {
    animation:
      overlay-in 0.3s ease-out both,
      overlay-land 0.3s ease-out both;
  }
  @media (prefers-reduced-motion: reduce) {
    .tile:has(.fit > :global(img.in)) :is(.comic-foot, .comic-status, .comic-progress),
    .tile.landed:has(.fit > :global(img.in)) :is(.comic-foot, .comic-status, .comic-progress) {
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
     has a frame), lifting off the grid; it shrinks back when the mouse moves.
     It's laid out as the card at once and drawn there by a transform (fly): place
     and size in one go, on the compositor, never snapped to pixels. */
  .tile.zoomed,
  .tile.shrinking {
    z-index: 5;
    transform-origin: 0 0;
    transition:
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
  /* The badges: gone while it's a card, and until it's home - drawn by the card's stretch, a badge shrinking back
     changed shape (the user: the NEW chip "resizes when i mouse off"); they fade in once it's a tile again. */
  .tile :is(.badge, .fav-badge, .new-badge) {
    transition: opacity 0.18s;
  }
  .tile:is(.zoomed, .shrinking) :is(.badge, .fav-badge, .new-badge) {
    opacity: 0;
    transition: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .tile :is(.badge, .fav-badge, .new-badge) {
      transition: none;
    }
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
    overflow: hidden; /* (its edges slide behind the cover: playMorph) */
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
    font-size: 13px;
    font-weight: 650;
    /* (whole pixels, the chip's too: the foot's parts then sit on whole pixels - at 1.3 (16.9 px) they sat between
       pixels, drawn one way at rest and another while the card moved, so the icon stepped as a hover zoom ended) */
    line-height: 17px;
  }
  /* (one line that slides while the mouse is on the tile - app.css's .marquee, the same everywhere) */
  @media (hover: hover) {
    .tile:hover .comic-title:global(.long) > span {
      animation: marquee-line var(--dur, 6s) 0.3s infinite;
    }
    .tile:hover .comic-title:global(.long) {
      animation: marquee-edges var(--dur, 6s) linear 0.3s infinite;
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
    .tile:hover .comic-title:global(.long),
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
    line-height: 16px; /* (20 px tall: its 12 px icon centered on whole pixels too) */
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
  /* A comic downloading (see downloadMark). */
  .comic-status.downloading {
    background: var(--peach);
    transition:
      background-color 0.25s,
      color 0.25s;
  }
  .downloading .arrow {
    animation: arrow-falls 1.1s cubic-bezier(0.4, 0, 0.6, 1) infinite;
  }
  @keyframes arrow-falls {
    0% {
      translate: 0 -7px;
      opacity: 0;
    }
    25%,
    70% {
      opacity: 1;
    }
    100% {
      translate: 0 3px;
      opacity: 0;
    }
  }
  .downloading .tick {
    stroke-dasharray: 1;
    stroke-dashoffset: 1;
    transition: stroke-dashoffset 0.32s cubic-bezier(0.2, 0.8, 0.2, 1) 0.08s;
  }
  .downloading .tray {
    transition: opacity 0.15s;
  }
  .comic-status.downloading.done {
    background: var(--green);
    color: var(--crust);
  }
  .downloading.done .arrow {
    animation: none;
    opacity: 0;
  }
  .downloading.done .tray {
    opacity: 0;
  }
  .downloading.done .tick {
    stroke-dashoffset: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .downloading .arrow {
      animation: none;
    }
    .downloading .tick {
      transition: none;
    }
  }
  /* Browse: a gallery the library has - a grey veil over its picture (its hover card: the veil fades, the real colors
     to look at). A layer's opacity, not a filter on the picture: that was drawn again as the tile moved, and flickered. */
  .had-veil {
    position: absolute;
    inset: 0;
    background: rgb(40 40 52 / 0.72);
    pointer-events: none;
    transition: opacity 0.2s ease-out;
  }
  .tile.zoomed .had-veil {
    opacity: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .had-veil {
      transition: none;
    }
  }
  /* A comic picked up: its place left faint, a copy following the pointer; the collection under it lit. */
  .tile.lift-source {
    opacity: 0.3;
  }
  .tile.lift-home {
    visibility: hidden;
  }
  /* (inside its edge: a tile clips what's outside it - contain - the border was cut off) */
  .tile.drop-target::after {
    content: '';
    position: absolute;
    inset: 0;
    z-index: 10;
    border-radius: inherit;
    box-shadow: inset 0 0 0 3px var(--mauve);
    pointer-events: none;
  }
  :global(.lift-ghost) {
    position: fixed !important;
    z-index: 60;
    pointer-events: none;
    contain: none !important;
    overflow: visible !important;
    outline: 2px solid var(--mauve);
    box-shadow: 0 18px 36px rgb(0 0 0 / 0.55);
  }
  /* A collection unstacking: only the comics - no empty boxes where they're going, no color behind them on the way. */
  .unstacking .band,
  .unstacking .hole {
    visibility: hidden;
  }
  .unstacking :global(.sizer > .tile) {
    background: none !important;
  }
  /* Another page's tiles (stale): hidden while they leave as ghosts and this page's list comes. */
  .cleared :global(.sizer > .tile:not(.ghost)) {
    visibility: hidden;
  }
  /* The tiles' shapes ahead of time (bands): under every tile, never taking a tap. */
  .band {
    position: absolute;
    left: 0;
    right: 0;
    z-index: -2; /* (under removed tiles fading out too: -1) */
    pointer-events: none;
    background-repeat: no-repeat;
    background-position: 0 0;
  }
  /* A tile's place while it's away (holes): over its box, under every tile. */
  .hole {
    position: absolute;
    top: 0;
    left: 0;
    z-index: -2;
    pointer-events: none;
  }
  /* Scrubbing the hover card's video: a sideways cursor wherever the pointer goes. */
  .scrubbing,
  .scrubbing :global(*) {
    cursor: ew-resize !important;
  }
</style>
