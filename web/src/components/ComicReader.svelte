<script lang="ts">
  // The comic reader, full screen over everything. Three ways to read: one page
  // at a time, two-page spreads (the cover alone, a wide page alone), or one long
  // scroll (webtoons). Left to right, or right to left for manga (remembered per
  // comic). Pages turn with a swipe, a tap at the sides, the arrow keys or the
  // slider; a double tap or a pinch zooms (drag to look around). It looks and
  // handles like the library's viewer: the same floating controls, a swipe down
  // closes it (the page follows the finger, the background fades), a swipe up
  // shows the details (a column beside the page on wide screens). The place it
  // stops is saved, and the shelf opens there next time. Each page shows its
  // thumbnail at once while the full picture loads; the next pages load ahead.
  import { onDestroy, onMount, tick, untrack } from 'svelte';
  import { Glide, clampZoom as clampFit, zoomAround, pinchScale } from '../lib/zoom';
  import { on } from 'svelte/events';
  import { api, type Comic, type FileInfo } from '../lib/api';
  import { viewURL, fullURL } from '../lib/results.svelte';
  import { t, num } from '../lib/i18n.svelte';
  import { fullscreen } from '../lib/fullscreen.svelte';
  import { settings } from '../lib/settings.svelte';
  import { toasts } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';
  import ActionMenu from './ActionMenu.svelte';

  // onread: where reading is now (the shelf shows it at once; saving it takes a moment).
  let {
    id,
    startPage,
    onclose,
    onread,
    ondetails,
    onfavorite,
    ondelete,
    info = false,
    hidden = false,
    keysOff = false,
    onready,
    preload,
    onnext,
  }: {
    id: number;
    startPage?: number;
    onclose: () => void;
    onread?: (page: number, saving: Promise<unknown>) => void;
    ondetails?: (comic: Comic) => void; // (shows its details, or hides them when shown)
    onfavorite?: (comic: Comic, on: boolean) => void;
    ondelete?: (comic: Comic) => void; // (asks first)
    info?: boolean; // its details are open
    hidden?: boolean; // (the opening or closing animation plays over it: lib/hero)
    keysOff?: boolean; // (a dialog is open over it: the keys are its)
    onready?: () => void; // its page is there (the opening animation then fades into it)
    preload?: Promise<{ comic: Comic; pages: FileInfo[] }>; // (fetched already, by the opening animation)
    onnext?: (comic: Comic) => void; // (the end page's Up next: the collection's next comic, in its place)
  } = $props();

  type Mode = 'single' | 'spread' | 'scroll';
  let comic = $state<Comic | null>(null);
  let pages = $state<FileInfo[]>([]);
  let error = $state('');
  let index = $state(0); // the page shown (in a spread: its first page)
  let ended = $state(false); // past the last page: the end card
  let chrome = $state(true); // the controls over the page
  const mouse = matchMedia('(hover: hover) and (pointer: fine)').matches;
  const wideScreen = matchMedia('(min-width: 900px)').matches;
  const isSheet = () => !matchMedia('(min-width: 900px)').matches;
  let mode = $state<Mode>(loadMode());
  let rtl = $state(false);
  let box = $state<HTMLElement>();
  let reader = $state<HTMLElement>();

  // ---- settings this device remembers: the layout for all comics, the direction per comic
  function store<T>(key: string, fallback: T): T {
    try {
      const v = localStorage.getItem('vitrine.' + key);
      return v === null ? fallback : (JSON.parse(v) as T);
    } catch {
      return fallback;
    }
  }
  function keep(key: string, v: unknown) {
    try {
      localStorage.setItem('vitrine.' + key, JSON.stringify(v));
    } catch {
      /* not remembered */
    }
  }
  function loadMode(): Mode {
    const m = store<string>('readerMode', 'single');
    return m === 'spread' || m === 'scroll' ? m : 'single';
  }
  function setMode(m: Mode) {
    const at = index;
    mode = m;
    keep('readerMode', m);
    resetZoom();
    tick().then(() => goTo(at, false));
  }
  function setRtl(on: boolean) {
    rtl = on;
    const map = store<Record<string, boolean>>('readerRtl', {});
    if (on) map[id] = true;
    else delete map[id];
    keep('readerRtl', map);
  }

  onMount(async () => {
    rtl = !!store<Record<string, boolean>>('readerRtl', {})[id];
    try {
      const res = await (preload ?? api.comic(id));
      comic = res.comic;
      pages = res.pages;
      index = Math.min(Math.max(0, startPage ?? res.comic.read_page), Math.max(0, pages.length - 1));
      if (mode === 'spread') index = spreads[spreadOf(index)]?.[0] ?? 0;
      await tick();
      if (mode === 'scroll') scrollToPage(index, false);
      requestAnimationFrame(() => onready?.());
      findNext(res.comic);
      saved = lastShown; // (opening isn't reading: the place is saved once a page is turned - opening the cover keeps it)
    } catch (e) {
      error = (e as Error).message;
    }
    reader?.focus();
    hideSoon();
  });

  // ---- in a collection: the comic after this one (A to Z, as its page lists them), offered at the end
  let nextComic = $state<Comic | null>(null);
  async function findNext(c: Comic) {
    if (!c.collection) return;
    try {
      const res = await api.search('sort:title_asc', 1000, undefined, undefined, false, 'comics', c.collection);
      const i = res.ids.indexOf(id);
      const after = i >= 0 ? res.ids[i + 1] : undefined;
      if (after !== undefined) nextComic = (await api.comicInfo([after])).comics[0] ?? null;
    } catch {
      /* no Up next */
    }
  }

  // ---- spreads: the cover alone, then pairs of portrait pages; a wide page stands alone
  const wide = (f?: FileInfo) => !!f && f.width > f.height * 1.1;
  const spreads = $derived.by(() => {
    const out: number[][] = [];
    let i = 0;
    while (i < pages.length) {
      if (i === 0 || wide(pages[i]) || i + 1 >= pages.length || wide(pages[i + 1])) {
        out.push([i]);
        i++;
      } else {
        out.push([i, i + 1]);
        i += 2;
      }
    }
    return out;
  });
  const spreadOf = (p: number) => Math.max(0, spreads.findIndex((s) => s.includes(p)));
  // The unit pages turn by: a page, or a spread.
  const units = $derived(mode === 'spread' ? spreads : pages.map((_, i) => [i]));
  const unit = $derived(mode === 'spread' ? spreadOf(index) : index);
  // Where the track is: a page (or spread), or past the last one - the end page (one more page: swiped to and back from).
  const at = $derived(ended ? units.length : unit);
  const shown = $derived(units[unit] ?? []);
  const lastShown = $derived(shown.length ? shown[shown.length - 1] : index);

  function goTo(p: number, animate = true) {
    if (!pages.length) return;
    ended = false;
    p = Math.min(Math.max(0, p), pages.length - 1);
    if (mode === 'spread') p = spreads[spreadOf(p)][0];
    if (p !== index) resetZoom(false);
    index = p;
    if (mode === 'scroll') scrollToPage(p, animate);
  }
  // A turn that slides the page off (taps, keys and swipes alike); the end page is one more page; before the first and
  // after the end, nothing.
  let anim = $state(false); // the track animates (a turn, or snapping back after a short swipe)
  const TURN_MS = 220; // (a swipe finishing its slide; .track.animating in step)
  function turn(dir: 'next' | 'prev') {
    if (turning || mode === 'scroll' || (dir === 'next' && ended) || (dir === 'prev' && at <= 0)) {
      dx = 0;
      return dir === 'next' ? next() : prev();
    }
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return dir === 'next' ? next() : prev();
    // A tap or a key: the next page at once (as the library's viewer); a swipe: it slides on the way the finger took it.
    if (dx === 0) return dir === 'next' ? next() : prev();
    anim = true;
    turning = dir;
    window.setTimeout(() => {
      anim = false; // (the new page takes the middle without sliding back)
      turning = '';
      dx = 0;
      if (dir === 'next') next();
      else prev();
    }, TURN_MS);
  }
  function next() {
    if (ended) return;
    if (unit >= units.length - 1) {
      ended = true;
      chrome = true;
      return;
    }
    goTo(units[unit + 1][0]);
  }
  function prev() {
    if (ended) {
      ended = false;
      return;
    }
    if (unit > 0) goTo(units[unit - 1][0]);
  }

  // ---- where reading stopped: saved a moment after each turn, and on closing
  let saveTimer = 0;
  let saved = -1;
  function save() {
    clearTimeout(saveTimer);
    if (!comic || saved === lastShown) return;
    saved = lastShown;
    const saving = api.updateComic(id, { read_page: lastShown });
    onread?.(lastShown, saving);
    saving.catch(() => {
      /* tried again on the next turn */
      saved = -1;
    });
  }
  $effect(() => {
    void lastShown;
    if (!comic) return;
    clearTimeout(saveTimer);
    saveTimer = window.setTimeout(save, 800);
  });
  onDestroy(() => {
    save();
    fullscreen.exit();
  });

  // ---- the next pages load ahead (and the one before)
  const warmed = new Set<string>();
  $effect(() => {
    const from = index;
    untrack(() => {
      for (const p of [from + 1, from + 2, from + 3, from + 4, from - 1]) {
        const f = pages[p];
        if (!f) continue;
        const u = viewURL(f);
        if (warmed.has(u)) continue;
        warmed.add(u);
        const img = new Image();
        img.decoding = 'async';
        img.src = u;
      }
    });
  });
  // Pictures that have arrived: each fades in over its thumbnail stand-in (it swapped at once: the page snapped from
  // soft to sharp), which goes once it has (faded).
  let loaded = $state<Record<number, boolean>>({});
  let faded = $state<Record<number, boolean>>({});
  function arrived(p: number) {
    loaded[p] = true;
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) faded[p] = true; // (no fade: no transitionend)
  }

  // ---- the controls: shown at first, on a tap in the middle and when the mouse moves;
  // they fade after a while without anything happening (not while pointed at, nor with the details open)
  let hideTimer = 0;
  let overControls = false;
  function hideSoon() {
    clearTimeout(hideTimer);
    hideTimer = window.setTimeout(() => {
      if (ended || info || overControls || document.querySelector('.reader [aria-expanded="true"]')) hideSoon();
      else chrome = false;
    }, 3000);
  }
  function wake() {
    chrome = true;
    hideSoon();
  }
  function toggleFavorite() {
    if (!comic || !onfavorite) return;
    const on = !comic.favorite;
    comic = { ...comic, favorite: on };
    onfavorite(comic, on);
  }
  function toggleChrome() {
    chrome = !chrome;
    if (chrome) hideSoon();
  }

  // ---- "I came" (Settings > Viewing; not in SFW mode): counted on the page showing - the I-came page shows that page.
  async function countClimax() {
    const p = shown[0];
    if (!comic || p === undefined) return;
    const c = comic;
    const setCount = (n: number) => {
      if (pages[p]) pages[p] = { ...pages[p], came: n };
    };
    try {
      const res = await api.climaxPage(c.id, p);
      setCount(res.count);
      toasts.show(res.count === 1 ? t('viewer.countedFirst') : t('viewer.counted', { n: res.count }), { cat: 'came',
        action: {
          label: t('common.undo'),
          run: async () => {
            await api.undoClimax(res.id);
            setCount(res.count - 1);
          },
        },
      });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  const pageCame = $derived(shown.length ? (pages[shown[0]]?.came ?? 0) : 0);

  // ---- keys
  function key(e: KeyboardEvent) {
    if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
    if (keysOff || document.querySelector('[role=alertdialog]')) return; // (a dialog over it: its keys)
    const el = e.target as HTMLElement;
    if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable) return; // (typing in the details)
    if (e.key === 'Escape' && info) return; // (the details close first: theirs)
    const fwd = rtl ? 'ArrowLeft' : 'ArrowRight';
    const back = rtl ? 'ArrowRight' : 'ArrowLeft';
    if (mode === 'scroll' && (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === ' ')) return; // (the page scrolls)
    if (e.key === fwd || e.key === ' ' || e.key === 'PageDown') turn('next');
    else if (e.key === back || e.key === 'PageUp') turn('prev');
    else if (e.key === 'Home') goTo(0);
    else if (e.key === 'End') goTo(pages.length - 1);
    else if (e.key === 'Escape') {
      if (isZoomed()) resetZoom();
      else onclose();
    } else if (e.key === 'f' || e.key === 'F') fullscreen.toggle(reader ?? null);
    else if (e.key === 'i' && comic && ondetails) ondetails(comic);
    else if (e.key === 'l') toggleFavorite();
    else if (e.key === 'c' && settings.came) countClimax();
    else if (e.key === 'Delete' && comic && ondelete) ondelete(comic);
    else return;
    e.preventDefault();
  }

  // ---- swiping, tapping and zooming (one page or a spread) - as the library's viewer
  // One finger: sideways turns, down closes, up shows the details. Zoom: a double tap (a mouse: a click in the
  // middle) goes to the page's own pixels (100 %) and back; a pinch, Ctrl + wheel, or the wheel once zoomed.
  // Past the limits a pinch goes on with resistance and springs back; a pan keeps gliding after a flick.
  let zoom = $state({ s: 1, x: 0, y: 0 });
  let zoomAnimate = $state(false); // (the next zoom change animates)
  let zoomSpring = $state(false); // ... springing back from past a limit
  let dx = $state(0); // the page following a finger sideways
  let dy = $state(0); // ... or down (letting go far enough closes)
  let settling = $state(false); // (back up after a short swipe down)
  let axis: 'x' | 'down' | 'up' | 'pan' | null = null; // what a finger's move does (decided by its first 8 px)
  let turning = $state<'' | 'next' | 'prev'>(''); // sliding off to the next or previous page
  let boxW = $state(0);
  let boxH = $state(0);
  const isZoomed = () => zoom.s > 1.01;

  /** The page (or spread) as fitted at zoom 1: its size, and its scale against the first page's own pixels. */
  function fitted() {
    const ps = shown.map((p) => pages[p]).filter((f) => f?.width && f.height);
    // (the space the page has: its slide, inside the safe areas)
    const sp = box?.querySelector<HTMLElement>(".slide:nth-child(2) .spread");
    const W = sp?.clientWidth || boxW;
    const H = sp?.clientHeight || boxH;
    if (!ps.length || !W || !H) return { w: W, h: H, fit: 1 };
    // (as the CSS sizes them: each page the height, or the width - a spread: half each - at its shape)
    const each = ps.length > 1 ? W / 2 : W;
    const hs = ps.map((f) => Math.min(H, each / (f.width / f.height)));
    const w = ps.reduce((sum, f, i) => sum + hs[i] * (f.width / f.height), 0);
    return { w, h: Math.max(...hs), fit: hs[0] / ps[0].height };
  }
  /** The zoom that shows the page's own pixels (100 %); twice the fit when it's smaller than the screen anyway. */
  function actualSize() {
    const k = 1 / fitted().fit;
    return k > 1.2 ? k : 2;
  }
  /** At least 4x, or enough to see the page's own pixels. */
  function maxZoom() {
    return Math.min(16, Math.max(4, 1.5 / fitted().fit));
  }
  /** How big the page is shown, in % of its own pixels. */
  const percent = $derived.by(() => {
    void boxW;
    void boxH;
    return shown.length && pages[shown[0]]?.height ? Math.round(fitted().fit * zoom.s * 100) : 0;
  });
  /** Keeps a zoomed page from being dragged away from the screen edges. */
  function clampZoom(z: { s: number; x: number; y: number }) {
    return clampFit(z, fitted(), { w: boxW, h: boxH });
  }
  function fromCenter(clientX: number, clientY: number) {
    const r = box!.getBoundingClientRect();
    return { x: clientX - (r.left + r.width / 2), y: clientY - (r.top + r.height / 2) };
  }
  /** Zooms to scale s, keeping the point under (clientX, clientY) in place. */
  function zoomTo(s: number, clientX: number, clientY: number, animated = false) {
    if (!box) return;
    s = Math.min(maxZoom(), Math.max(1, s));
    zoomAnimate = animated;
    zoom = clampZoom(zoomAround(zoom, s, fromCenter(clientX, clientY)));
  }
  function resetZoom(animated = true) {
    stopGlide();
    zoomAnimate = animated;
    zoom = { s: 1, x: 0, y: 0 };
  }

  // Panning keeps going after the finger lets go (like Photos): the flick's speed, slowing down, stopping at the edges.
  const glider = new Glide(); // (a flicked pan keeps gliding: lib/zoom)
  const stopGlide = () => glider.stop();
  const glide = () =>
    glider.start(
      () => zoom,
      (z) => {
        zoomAnimate = false;
        zoom = z;
      },
      clampZoom,
    );


  const pointers = new Map<number, { x: number; y: number }>();
  let start: { x: number; y: number; t: number; id: number } | null = null;
  let pinch: { dist: number; mx: number; my: number; s: number; x: number; y: number } | null = null;
  let panFrom = { x: 0, y: 0 };
  let lastTap = { t: 0, x: 0, y: 0 };
  let tapTimer = 0;
  // Middle button: a click closes (as the viewer's); held on a zoomed page, it pans.
  let middlePan: { x: number; y: number; from: { x: number; y: number }; id: number } | null = null;
  let middleUsed = false;

  let onButton = false;
  function capture(id: number) {
    try {
      box?.setPointerCapture(id);
    } catch {
      /* synthetic pointers (tests) */
    }
  }
  function down(e: PointerEvent) {
    if (mode === 'scroll' || turning) return;
    // On a button (the end page's): a swipe still turns, a press is the button's - held only once it's a swipe.
    onButton = !!(e.target as HTMLElement).closest('button, a');
    if (e.button === 1) {
      e.preventDefault(); // (no auto-scroll)
      if (isZoomed()) {
        stopGlide();
        middlePan = { x: e.clientX, y: e.clientY, from: { x: zoom.x, y: zoom.y }, id: e.pointerId };
        middleUsed = true;
        box?.setPointerCapture(e.pointerId);
      }
      return;
    }
    if (e.button !== 0) return;
    stopGlide(); // (a touch catches a gliding page)
    zoomSpring = false;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (!onButton) capture(e.pointerId);
    if (pointers.size === 2) {
      // A second finger: a pinch instead of a swipe.
      clearTimeout(tapTimer);
      lastTap.t = 0;
      start = null;
      dx = 0;
      dy = 0;
      const [a, b] = [...pointers.values()];
      pinch = { dist: Math.hypot(a.x - b.x, a.y - b.y) || 1, mx: (a.x + b.x) / 2, my: (a.y + b.y) / 2, s: zoom.s, x: zoom.x, y: zoom.y };
      zoomAnimate = false;
      return;
    }
    if (pointers.size > 1) return;
    start = { x: e.clientX, y: e.clientY, t: performance.now(), id: e.pointerId };
    axis = null;
    zoomAnimate = false;
    panFrom = { x: zoom.x, y: zoom.y };
  }
  function move(e: PointerEvent) {
    if (middlePan && e.pointerId === middlePan.id) {
      zoomAnimate = false;
      zoom = clampZoom({ s: zoom.s, x: middlePan.from.x + e.clientX - middlePan.x, y: middlePan.from.y + e.clientY - middlePan.y });
      return;
    }
    if (!pointers.has(e.pointerId)) return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pinch) {
      if (pointers.size < 2) return;
      const [a, b] = [...pointers.values()];
      // Past the limits (smaller than fitted, bigger than the most) it goes on with resistance.
      const raw = (pinch.s * Math.hypot(a.x - b.x, a.y - b.y)) / pinch.dist;
      const max = maxZoom();
      const s = pinchScale(raw, max);
      // The point first under the fingers' middle stays under it (free while pinching: kept inside once let go).
      const m0 = fromCenter(pinch.mx, pinch.my);
      const m = fromCenter((a.x + b.x) / 2, (a.y + b.y) / 2);
      const px = (m0.x - pinch.x) / pinch.s;
      const py = (m0.y - pinch.y) / pinch.s;
      zoom = { s, x: m.x - px * s, y: m.y - py * s };
      return;
    }
    if (!start || e.pointerId !== start.id) return;
    const mx = e.clientX - start.x;
    const my = e.clientY - start.y;
    if (axis === null) {
      if (Math.hypot(mx, my) <= 8) return;
      axis = isZoomed() ? 'pan' : Math.abs(mx) > Math.abs(my) ? 'x' : my > 0 ? 'down' : 'up';
      if (onButton) capture(e.pointerId);
    }
    if (axis === 'pan') {
      zoom = clampZoom({ s: zoom.s, x: panFrom.x + mx, y: panFrom.y + my });
      glider.record(e.clientX, e.clientY);
    } else if (axis === 'x') {
      // Resists going back from the first page and on from the end page (there's nothing there).
      const back = rtl ? mx < 0 : mx > 0;
      dx = (back && at <= 0) || (!back && ended) ? mx / 4 : mx;
    } else if (axis === 'down') dy = Math.max(0, my);
  }
  function up(e: PointerEvent) {
    if (middlePan && e.pointerId === middlePan.id) {
      middlePan = null;
      return;
    }
    if (!pointers.delete(e.pointerId)) return;
    if (pinch) {
      if (pointers.size < 2) {
        pinch = null;
        // Back within the limits with a spring.
        if (zoom.s < 1.05) {
          zoomSpring = true;
          resetZoom();
        } else if (zoom.s > maxZoom()) {
          zoomSpring = true;
          const r = box!.getBoundingClientRect();
          zoomTo(maxZoom(), r.left + r.width / 2 + zoom.x / 2, r.top + r.height / 2 + zoom.y / 2, true);
        } else {
          const c = clampZoom(zoom);
          if (c.x !== zoom.x || c.y !== zoom.y) {
            zoomAnimate = true;
            zoom = c;
          }
        }
      }
      start = null;
      return;
    }
    if (!start || e.pointerId !== start.id) return;
    const s = start;
    start = null;
    if (axis === null) return onButton ? undefined : tap(e);
    if (axis === 'pan') return glide();
    if (axis === 'down') {
      // Far enough: closes (phones: the details sheet over the page goes first). Else back up.
      if (dy > 110) {
        if (!(info && isSheet())) return onclose();
        if (comic) ondetails?.(comic);
      }
      settling = true;
      dy = 0;
      window.setTimeout(() => (settling = false), TURN_MS);
      return;
    }
    if (axis === 'up') {
      if (s.y - e.clientY > 70 && comic && !info) ondetails?.(comic);
      return;
    }
    // A swipe: far enough, or quick enough, turns the page.
    const w = box?.clientWidth ?? 1;
    const fast = Math.abs(dx) / Math.max(1, performance.now() - s.t) > 0.5;
    const forward = rtl ? dx > 0 : dx < 0;
    if ((Math.abs(dx) > w * 0.22 || (fast && Math.abs(dx) > 30)) && (forward ? !ended : at > 0)) {
      turn(forward ? 'next' : 'prev');
    } else {
      // Not far enough: back to the middle.
      anim = true;
      dx = 0;
      window.setTimeout(() => (anim = false), TURN_MS);
    }
  }
  function tap(e: PointerEvent) {
    const { clientX: x, clientY: y } = e;
    // With the details open, a tap beside them only closes them (at once).
    if (info && comic) {
      clearTimeout(tapTimer);
      lastTap.t = 0;
      ondetails?.(comic);
      return;
    }
    const r = box!.getBoundingClientRect();
    const at = (x - r.left) / r.width;
    const side = at < 0.3 ? (rtl ? 'next' : 'prev') : at > 0.7 ? (rtl ? 'prev' : 'next') : null;
    if (ended) {
      // The end page: nothing to zoom - the sides turn (back), the middle shows the controls.
      if (side) turn(side);
      else toggleChrome();
      return;
    }
    // A mouse click: at once (no waiting for a double click) - the sides turn, the middle zooms to 100 % and back.
    if (mouse && e.pointerType === 'mouse') {
      if (isZoomed()) resetZoom();
      else if (side) turn(side);
      else zoomTo(actualSize(), x, y, true);
      return;
    }
    const now = performance.now();
    if (now - lastTap.t < 320 && Math.hypot(x - lastTap.x, y - lastTap.y) < 60) {
      // Double tap: to 100 % at the spot (or back out).
      clearTimeout(tapTimer);
      lastTap.t = 0;
      if (isZoomed()) resetZoom();
      else zoomTo(actualSize(), x, y, true);
      return;
    }
    lastTap = { t: now, x, y };
    // A single tap (once it's clear it isn't a double): the sides turn, the middle shows the controls.
    clearTimeout(tapTimer);
    tapTimer = window.setTimeout(() => {
      lastTap.t = 0;
      if (isZoomed() || !side) toggleChrome();
      else turn(side);
    }, 320);
  }
  // Ctrl + wheel (and a trackpad pinch) zooms; a zoomed page zooms with the wheel too (around the pointer, down to
  // the fit and no further - the same spin can't carry on into the next page: the wheel rests a moment first).
  // Otherwise the wheel turns pages as fast as it turns: a mouse wheel's click is a page, a trackpad's many small
  // steps add up to one (not in scroll mode). Attached non-passive so it can stop the browser's own zoom.
  let wheelSum = 0;
  let wheelAt = -Infinity;
  let lastZoomWheel = -Infinity;
  function wheel(e: WheelEvent) {
    if (mode === 'scroll') return;
    e.preventDefault();
    if (e.ctrlKey) {
      zoomTo(zoom.s * Math.exp(-e.deltaY * 0.01), e.clientX, e.clientY);
      return;
    }
    const now = performance.now();
    const lines = e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? 800 : 1;
    if (isZoomed()) {
      const s = zoom.s * Math.exp(-e.deltaY * lines * 0.0015);
      lastZoomWheel = now;
      if (s <= 1.01) resetZoom(false);
      else zoomTo(s, e.clientX, e.clientY);
      return;
    }
    if (now - lastZoomWheel < 400) {
      lastZoomWheel = now; // (still the spin that zoomed out)
      return;
    }
    const px = (Math.abs(e.deltaY) >= Math.abs(e.deltaX) ? e.deltaY : e.deltaX) * lines;
    if (Math.abs(px) >= 50) {
      wheelSum = 0;
      return px > 0 ? next() : prev();
    }
    if (now - wheelAt > 200 || Math.sign(px) !== Math.sign(wheelSum)) wheelSum = 0; // (a new spin)
    wheelAt = now;
    wheelSum += px;
    if (Math.abs(wheelSum) >= 120) {
      if (wheelSum > 0) next();
      else prev();
      wheelSum = 0;
    }
  }
  $effect(() => {
    if (box) return on(box, 'wheel', wheel, { passive: false });
  });

  // ---- zoomed in: the full-size pictures, loaded in the background first (the screen-size ones stay till then)
  const zoomedIn = $derived(zoom.s > 1.15);
  let full = $state<Record<number, boolean>>({}); // pages whose full-size picture has arrived
  const asked = new Set<number>();
  $effect(() => {
    if (!zoomedIn) return;
    for (const p of shown) {
      const f = pages[p];
      if (!f || asked.has(p) || fullURL(f) === viewURL(f)) continue;
      asked.add(p);
      const img = new Image();
      img.decoding = 'async';
      img.src = fullURL(f);
      img.decode().then(
        () => (full[p] = true),
        () => asked.delete(p),
      );
    }
  });
  const pageSrc = (p: number) => (zoomedIn && full[p] && shown.includes(p) ? fullURL(pages[p]) : viewURL(pages[p]));
  /** The picture shown isn't the one wanted yet (still loading). */
  const loading = $derived(shown.some((p) => !loaded[p] || (zoomedIn && fullURL(pages[p]) !== viewURL(pages[p]) && !full[p])));

  // ---- scroll mode: the page in the middle of the screen is the current one
  let scroller = $state<HTMLElement>();
  let ignoreScroll = 0;
  function scrollToPage(p: number, smooth: boolean) {
    const el = scroller?.querySelector<HTMLElement>(`[data-page="${p}"]`);
    if (!el || !scroller) return;
    ignoreScroll = performance.now() + (smooth ? 600 : 100);
    scroller.scrollTo({ top: el.offsetTop, behavior: smooth ? 'smooth' : 'auto' });
  }
  let recheck = 0;
  function scrolled() {
    if (!scroller) return;
    const wait = ignoreScroll - performance.now();
    if (wait > 0) {
      // (its own scroll is settling: looked at again once it has)
      clearTimeout(recheck);
      recheck = window.setTimeout(scrolled, wait + 20);
      return;
    }
    const mid = scroller.scrollTop + scroller.clientHeight / 2;
    for (const el of scroller.querySelectorAll<HTMLElement>('[data-page]')) {
      if (el.offsetTop <= mid && el.offsetTop + el.offsetHeight > mid) {
        index = Number(el.dataset.page);
        break;
      }
    }
    if (scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 4 && pages.length) index = pages.length - 1;
    if (chrome) chrome = false;
  }

  // The slider: page numbers from 1; right to left, its end is on the left.
  const counter = $derived(
    shown.length > 1 ? t('reader.pages', { a: num(shown[0] + 1), b: num(shown[1] + 1), n: pages.length }) : t('common.posOf', { pos: index + 1, total: pages.length }),
  );
  const slides = $derived(
    mode === 'scroll' ? [] : [{ k: 'prev', u: at - 1 }, { k: 'cur', u: at }, { k: 'next', u: at + 1 }],
  );
  const trackX = $derived.by(() => {
    if (turning) return `translateX(${(turning === 'next') !== rtl ? '-200%' : '0%'})`;
    return `translateX(calc(-100% + ${dx}px))`;
  });
</script>

<svelte:window onkeydown={key} />

<!-- svelte-ignore a11y_no_static_element_interactions (the mouse moving only shows the controls again) -->
<div
  class="reader"
  class:hidden
  class:info
  class:idle={!chrome && mouse}
  bind:this={reader}
  tabindex="-1"
  role="dialog"
  aria-modal="true"
  aria-label={comic?.title ?? t('comics.title')}
  style:--fade={1 - Math.min(dy / 400, 0.6)}
  style:--chrome-fade={1 - Math.min(dy / 160, 1)}
  onpointermove={(e) => e.pointerType === 'mouse' && wake()}>
  <!-- (the page and its controls; beside the details column on wide screens) -->
  <div class="frame">
  {#if error}
    <div class="message">
      <p>{error}</p>
      <button onclick={onclose}>{t('sites.close')}</button>
    </div>
  {:else if comic && !pages.length}
    <div class="message">
      <p>{t('reader.noPages')}</p>
      <button onclick={onclose}>{t('sites.close')}</button>
    </div>
  {:else if mode === 'scroll'}
    <div class="scroller" bind:this={scroller} onscroll={scrolled} onclick={(e) => !(e.target as HTMLElement).closest('a, button') && toggleChrome()} role="presentation">
      {#each pages as f, i (f.id)}
        <div class="strip" data-page={i} style:aspect-ratio="{f.width || 3} / {f.height || 4}">
          {#if f.thumb && !faded[i]}<img class="stand-in" src={f.thumb} alt="" draggable="false" />{/if}
          <img
            class="pic"
            class:in={loaded[i]}
            src={viewURL(f)}
            alt={t('reader.pageAlt', { n: i + 1 })}
            loading={Math.abs(i - index) < 3 ? 'eager' : 'lazy'}
            decoding="async"
            draggable="false"
            onload={() => arrived(i)}
            ontransitionend={() => (faded[i] = true)} />
        </div>
      {/each}
      <div class="strip-end">
        <p>{t('reader.end', { title: comic?.title ?? '' })}</p>
        {#if nextComic && onnext}
          {@const n = nextComic}
          <button class="up-next" onclick={() => onnext(n)}>
            {#if n.thumb}<img src={n.thumb} alt="" draggable="false" />{/if}
            <span class="up-next-text"><span class="up-next-label">{t('reader.upNext')}</span><span class="up-next-title">{n.title}</span></span>
            <Icon name="right" />
          </button>
        {/if}
        <button class="quiet" onclick={onclose}>{t('reader.backToShelf')}</button>
      </div>
    </div>
  {:else if comic}
    <div
      class="stage"
      class:settling
      class:zoom-in={mouse && percent > 0 && !isZoomed()}
      class:zoom-out={mouse && isZoomed()}
      bind:clientWidth={boxW}
      bind:clientHeight={boxH}
      onauxclick={(e) => {
        // A middle click closes - not when it was holding a zoomed page to pan it.
        if (e.button === 1 && !middleUsed) onclose();
        middleUsed = false;
      }}
      bind:this={box}
      style:transform={dy ? `translate3d(0, ${dy}px, 0) scale(${1 - Math.min(dy / 2000, 0.15)})` : undefined}
      onpointerdown={down}
      onpointermove={move}
      onpointerup={up}
      onpointercancel={up}
      role="presentation">
      <div class="track" class:rtl class:animating={anim} style:transform={trackX}>
        <!-- (keyed by page: after a turn the page that slid in stays the same element - nothing reloads or blinks) -->
        {#each slides as sl (sl.u)}
          <div class="slide">
            {#if units[sl.u]}
              <div
                class="spread"
                class:two={units[sl.u].length > 1}
                class:rtl
                class:zoom-anim={sl.k === 'cur' && zoomAnimate}
                class:spring={sl.k === 'cur' && zoomAnimate && zoomSpring}
                style:transform={sl.k === 'cur' && zoom.s !== 1 ? `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.s})` : undefined}>
                {#each units[sl.u] as p (p)}
                  {@const f = pages[p]}
                  <div class="page" style:aspect-ratio="{f.width || 3} / {f.height || 4}" style:--ar={(f.width || 3) / (f.height || 4)}>
                    {#if f.thumb && !faded[p]}<img class="stand-in" src={f.thumb} alt="" draggable="false" />{/if}
                    <img
                      class="pic"
                      class:in={loaded[p]}
                      src={pageSrc(p)}
                      alt={t('reader.pageAlt', { n: p + 1 })}
                      decoding="async"
                      draggable="false"
                      onload={() => arrived(p)}
                      ontransitionend={() => (faded[p] = true)} />
                  </div>
                {/each}
              </div>
            {:else if sl.u === units.length && units.length}
              <!-- The end page: what next - the collection's next comic, back to Comics, or read it again. -->
              <div class="end-page" role="status">
                <h2>{t('reader.end', { title: comic?.title ?? '' })}</h2>
                {#if nextComic && onnext}
                  {@const n = nextComic}
                  <button class="up-next" onclick={() => onnext(n)}>
                    {#if n.thumb}<img src={n.thumb} alt="" draggable="false" />{/if}
                    <span class="up-next-text"><span class="up-next-label">{t('reader.upNext')}</span><span class="up-next-title">{n.title}</span></span>
                    <Icon name="right" />
                  </button>
                {/if}
                <div class="end-actions">
                  <button class:quiet={!!nextComic} onclick={onclose}>{t('reader.backToShelf')}</button>
                  <button class="quiet" onclick={() => goTo(0)}>{t('reader.again')}</button>
                </div>
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}


  <!-- As the viewer's: no band across the page, each control on a small dark backing of its own. -->
  <div class="top float-bar" class:hide={!chrome} role="presentation" onpointerenter={() => (overControls = true)} onpointerleave={() => (overControls = false)}>
    <button class="icon" aria-label={t('sites.close')} title={t('reader.closeKey')} onclick={onclose}><Icon name="close" /></button>
    {#if pages.length}<span class="count chip">{counter}</span>{/if}
    {#if percent && mode !== 'scroll' && !ended}<span class="chip pct" title={t('viewer.percentTitle', { n: percent })}>{t('viewer.percent', { n: percent })}</span>{/if}
    {#if loading && mode !== 'scroll'}<span class="spinner" role="status" aria-label={t('viewer.loading')}></span>{/if}
    {#if comic}<span class="chip name" title={comic.title}>{comic.title}</span>{/if}
    <span class="spacer"></span>
    {#if comic && onfavorite}
      <button class="icon fav" class:on={!!comic.favorite} aria-label={t('filter.favorite.label')} aria-pressed={!!comic.favorite}
        title={comic.favorite ? t('viewer.unfavoriteKey') : t('viewer.favoriteKey')} onclick={toggleFavorite}><Icon name="heart" filled={!!comic.favorite} /></button>
    {/if}
    {#if comic && settings.came && pages.length}
      <button class="icon came" aria-label={pageCame ? t('viewer.cameSoFar', { n: pageCame }) : t('rankings.title')} title={t('viewer.cameKey')} onclick={countClimax}
        ><Icon name="drop" />{#if pageCame}<span class="came-n">{pageCame}</span>{/if}</button>
    {/if}
    {#if fullscreen.supported}
      <button class="icon" aria-label={fullscreen.on ? t('video.exitFullscreen') : t('video.fullscreen')} title={fullscreen.on ? t('video.exitFullscreenKey') : t('video.fullscreenKey')}
        onclick={() => fullscreen.toggle(reader ?? null)}><Icon name={fullscreen.on ? 'shrink' : 'expand'} /></button>
    {/if}
    {#if ondetails && comic}
      {@const c = comic}
      <button class="icon" class:on={info} aria-label={t('fileMenu.details')} title={t('viewer.detailsKey')} aria-pressed={info} onclick={() => ondetails(c)}><Icon name="info" /></button>
    {/if}
    <div class="menu-wrap">
      <ActionMenu
        label={t('reader.settings')}
        title={t('reader.settings')}
        items={[
          {
            label: t('reader.layout'),
            icon: 'book',
            hint: t(`reader.mode.${mode}`),
            choices: {
              heading: t('reader.layout'),
              value: mode,
              options: (['single', 'spread', 'scroll'] as const).map((m) => ({ value: m, label: t(`reader.mode.${m}`) })),
              pick: (v) => setMode(v as Mode),
            },
          },
          {
            label: t('reader.direction'),
            icon: 'sort',
            hint: rtl ? t('reader.rtl') : t('reader.ltr'),
            choices: {
              heading: t('reader.direction'),
              value: rtl ? 'rtl' : 'ltr',
              options: [
                { value: 'ltr', label: t('reader.ltr') },
                { value: 'rtl', label: t('reader.rtlManga') },
              ],
              pick: (v) => setRtl(v === 'rtl'),
            },
          },
        ]} />
    </div>
  </div>

  {#if wideScreen && mode !== 'scroll' && pages.length > 1}
    <!-- As the viewer's: arrows at the sides (right to left: the left one goes on). -->
    <button class="side-nav left" class:hide={!chrome} aria-label={rtl ? t('viewer.next') : t('viewer.previous')} onclick={() => turn(rtl ? 'next' : 'prev')} disabled={rtl ? ended : unit <= 0 && !ended}
      ><Icon name="left" size={28} /></button>
    <button class="side-nav right" class:hide={!chrome} aria-label={rtl ? t('viewer.previous') : t('viewer.next')} onclick={() => turn(rtl ? 'prev' : 'next')} disabled={rtl ? unit <= 0 && !ended : ended}
      ><Icon name="right" size={28} /></button>
  {/if}

  {#if pages.length > 1}
    <div class="bottom" class:hide={!chrome} role="presentation" onpointerenter={() => (overControls = true)} onpointerleave={() => (overControls = false)}>
      <input
        class="slider"
        type="range"
        min="0"
        max={pages.length - 1}
        value={index}
        dir={rtl ? 'rtl' : 'ltr'}
        style:--p="{(index / Math.max(1, pages.length - 1)) * 100}%"
        aria-label={t('reader.page')}
        aria-valuetext={counter}
        oninput={(e) => goTo(Number((e.currentTarget as HTMLInputElement).value), false)} />
    </div>
  {/if}
  </div>
</div>

<style>
  .reader {
    position: fixed;
    inset: 0;
    z-index: 45; /* (under notices and menus) */
    background: rgb(17 17 27 / var(--fade, 1)); /* (the viewer's; it fades as a swipe down closes it) */
    color: var(--text);
    overflow: hidden;
    outline: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
  }
  .reader.hidden {
    visibility: hidden;
  }
  .reader.idle,
  .reader.idle .stage {
    cursor: none; /* (idle with a mouse: the cursor goes with the controls) */
  }
  .frame {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }
  /* Wide screens: the details are a column on the right (DetailsPanel over it); the page makes room. */
  @media (min-width: 900px) {
    .frame {
      transition: right 0.26s cubic-bezier(0.2, 0.8, 0.2, 1);
    }
    .reader.info .frame {
      right: 340px;
    }
  }
  @media (min-width: 900px) and (prefers-reduced-motion: reduce) {
    .frame {
      transition: none;
    }
  }
  .message {
    position: absolute;
    inset: 0;
    display: grid;
    place-content: center;
    justify-items: center;
    gap: 12px;
    padding: 24px;
    text-align: center;
    color: var(--subtext);
  }
  /* ---- one page or a spread: a track of three (before, this, after) that slides */
  .stage {
    position: absolute;
    inset: 0;
    touch-action: none;
    overflow: hidden;
  }
  .stage.settling {
    transition: transform 0.22s cubic-bezier(0.25, 0.8, 0.3, 1); /* (TURN_MS) */
  }
  .track {
    display: flex;
    height: 100%;
    will-change: transform;
  }
  .track.rtl {
    flex-direction: row-reverse;
  }
  .track.animating {
    transition: transform 0.22s cubic-bezier(0.25, 0.8, 0.3, 1); /* (TURN_MS) */
  }
  .slide {
    flex: 0 0 100%;
    height: 100%;
    display: grid;
    place-items: center;
    overflow: hidden;
    padding: var(--safe-top) var(--safe-right, 0px) var(--safe-bottom) var(--safe-left, 0px);
  }
  .spread {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    transform-origin: center;
    container-type: size; /* (its pages are sized from it) */
  }
  .zoom-anim {
    transition: transform 0.22s ease-out;
  }
  /* Springing back from past a limit: a little overshoot, settling (as the viewer's). */
  .zoom-anim.spring {
    transition: transform 0.45s cubic-bezier(0.34, 1.45, 0.64, 1);
  }
  .stage.zoom-in {
    cursor: zoom-in;
  }
  .stage.zoom-out {
    cursor: zoom-out;
  }
  .spread.rtl {
    flex-direction: row-reverse;
  }
  /* A page as big as fits, at its own shape: the height, or the width (a spread: half each) - worked out here, not
     left to max-width (Safari ignored it with aspect-ratio: the page came out wider than the screen, cut off). */
  .page {
    position: relative;
    flex: none;
    height: min(100cqh, 100cqw / var(--ar));
  }
  .spread.two .page {
    height: min(100cqh, 50cqw / var(--ar));
  }
  .page img,
  .strip img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .stand-in {
    filter: blur(1px);
  }
  /* A page fades in over its stand-in once it has arrived. */
  .pic {
    opacity: 0;
    transition: opacity 0.25s ease-out;
  }
  .pic.in {
    opacity: 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .pic {
      transition: none;
    }
  }
  /* ---- scroll: the pages one under another, at most a comfortable width */
  .scroller {
    position: absolute;
    inset: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    touch-action: pan-y pinch-zoom;
    padding: var(--safe-top) 0 var(--safe-bottom);
  }
  .strip {
    position: relative;
    width: min(100%, 900px);
    margin: 0 auto;
  }
  .strip-end {
    display: grid;
    justify-items: center;
    gap: 10px;
    padding: 40px 16px 80px;
    color: var(--subtext);
  }
  /* ---- the end page (a page of its own after the last) */
  .end-page {
    width: min(380px, calc(100vw - 40px));
    display: grid;
    gap: 16px;
    justify-items: center;
    text-align: center;
  }
  .end-page h2 {
    margin: 0;
    font-size: 17px;
    color: var(--subtext);
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  /* The collection's next comic: its cover, its title, an arrow. */
  .up-next {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    padding: 10px 12px 10px 10px;
    text-align: left;
    background: var(--surface0);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    color: var(--text);
  }
  .up-next img {
    width: 56px;
    height: 78px;
    object-fit: cover;
    border-radius: 6px;
    flex: none;
  }
  .up-next-text {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 3px;
  }
  .up-next-label {
    font-size: var(--font-small);
    color: var(--mauve);
    font-weight: 600;
  }
  .up-next-title {
    font-weight: 600;
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .strip-end .up-next {
    width: min(380px, 100%);
  }
  .end-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: center;
  }
  /* ---- the controls over the page (as the viewer's; they fade, and with a swipe down) */
  /* (the bar, its buttons and chips, the spinner, the side arrows: the shared .float-bar styles - app.css) */
  .bottom {
    background: rgb(17 17 27 / 0.72);
    -webkit-backdrop-filter: blur(10px);
    backdrop-filter: blur(10px);
  }
  .name {
    min-width: 0;
    max-width: 420px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  @media (max-width: 480px) {
    .name {
      display: none; /* (no room beside the buttons) */
    }
  }
  .spacer {
    flex: 1;
  }
  .bottom {
    position: absolute;
    z-index: 2;
    left: 50%;
    translate: -50% 0;
    bottom: calc(var(--safe-bottom) + 12px);
    width: min(760px, calc(100% - 24px));
    display: flex;
    padding: 2px 14px;
    border-radius: 999px;
    transition: opacity 0.2s;
    opacity: var(--chrome-fade, 1);
  }
  /* Touch screens: well clear of a phone's home bar (its swipe up must not catch the slider). */
  @media (pointer: coarse) {
    .bottom {
      bottom: calc(var(--safe-bottom) + 28px);
    }
  }
  .hide {
    opacity: 0;
    pointer-events: none;
  }
  .side-nav:not(.hide, :disabled) {
    opacity: var(--chrome-fade, 1); /* (a swipe down: they fade with the rest) */
  }
  .slider {
    flex: 1;
    accent-color: var(--mauve);
    min-height: var(--control-h);
    background: transparent;
    border: 0;
    padding: 0;
  }
  /* "I came": its count on this page, a small number on the button (as in the viewer). */
  button.came {
    position: relative;
  }
  .came-n {
    position: absolute;
    right: -2px;
    bottom: -2px;
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    border-radius: 999px;
    background: var(--mauve);
    color: var(--crust);
    font-size: 10px;
    font-weight: 800;
    line-height: 16px;
    text-align: center;
  }
</style>
