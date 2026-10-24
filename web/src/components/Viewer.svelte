<script lang="ts">
  import { backClose } from '../lib/backClose';
  import { pop, px, nudge } from '../lib/motion';
  import { dragDown } from '../lib/sheetDrag';
  import { Glide, clampZoom as clampFit, zoomAround, pinchScale } from '../lib/zoom';
  import { reducedMotion } from '../lib/hero';
  import { volume } from '../lib/volume.svelte';
  import { cubicOut } from 'svelte/easing';
  // Full-screen viewer over the same result list as the grid: swipe or use
  // the arrow keys to move through every result (no pages), swipe down or
  // Esc to close, tap to show/hide the controls.
  import { onMount, untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { on } from 'svelte/events';
  import { api, type FileInfo } from '../lib/api';
  import { settings } from '../lib/settings.svelte';
  import { results, viewURL, fullURL } from '../lib/results.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { confirmAction, confirmer } from '../lib/confirm.svelte';
  import { restoreFiles, purgeFiles } from '../lib/trash.svelte';
  import { fullscreen } from '../lib/fullscreen.svelte';
  import { overlayScroll } from '../lib/overlayScroll';
  import VideoControls from './VideoControls.svelte';
  import { handoff } from '../lib/videoHandoff';
  import FilePanel from './FilePanel.svelte';
  import Icon from './Icon.svelte';
  import { setFavorite } from '../lib/favorites';
  import { itemName, t, num } from '../lib/i18n.svelte';

  let {
    index = $bindable(0),
    onclose,
    onsearch,
    startInfo = false,
    hidden = false,
  }: { index: number; onclose: () => void; onsearch: (q: string) => void; startInfo?: boolean; hidden?: boolean } = $props();

  const wide = matchMedia('(min-width: 900px)');
  // A mouse (desktop): moving between files is instant, a click zooms to 100 %
  // (or plays/pauses a video) and the controls always show. Touch: slides, a
  // tap shows/hides the controls, a double tap zooms.
  const mouse = matchMedia('(hover: hover) and (pointer: fine)').matches;
  let chrome = $state(true);
  let viewerEl = $state<HTMLElement>();
  // Full screen ends with the viewer.
  $effect(() => () => fullscreen.exit());
  // The controls fade out after a while without anything happening (a tap, the
  // mouse moving, a key) - not while a video is paused, the pointer is on
  // them, the details are open or a dialog asks something. With a mouse, moving
  // it brings them back (and the cursor hides with them).
  const IDLE_MS = 3000;
  let idleTimer = 0;
  let overControls = false;
  function armIdle() {
    clearTimeout(idleTimer);
    idleTimer = window.setTimeout(() => {
      const keep = info || overControls || confirmer.current || (isVideo && videoEl?.paused);
      if (keep) armIdle();
      else chrome = false;
    }, IDLE_MS);
  }
  /** Something happened: the controls show again (and the wait starts over). */
  function wake() {
    chrome = true;
    armIdle();
  }
  $effect(() => {
    if (chrome) untrack(armIdle);
    else clearTimeout(idleTimer);
    return () => clearTimeout(idleTimer);
  });
  // The controls fade in as the viewer opens: drawn hidden first, shown a frame
  // later (a CSS animation instead could paint them fully on before it began).
  let entered = $state(false);
  onMount(() => {
    let id = requestAnimationFrame(() => (id = requestAnimationFrame(() => (entered = true))));
    return () => cancelAnimationFrame(id);
  });
  // Details start hidden on every device (more room for the file), unless opened
  // from the file menu's "Details".
  // svelte-ignore state_referenced_locally
  let info = $state(startInfo);
  let width = $state(0);
  let dx = $state(0);
  let dy = $state(0);
  // While swiping down to close (and as it springs back), only the current file
  // shows: the shrinking track would otherwise reveal its neighbors.
  let hideSides = $state(false);
  let hideSidesTimer = 0;
  let animate = $state(false);
  // Video elements by file ID: the incoming video is created as soon as a move
  // starts (like PhotoSwipe in hydrus-web), so it loads while sliding in.
  let videoEls = $state<Record<number, HTMLVideoElement | null>>({});
  // A removed video stops downloading (like hydrus-web): WebKit may otherwise
  // keep fetching a detached element.
  // Opened from a hover card that was playing it: from where the card was (lib/videoHandoff).
  const resumeFrom = (id: number) => (v: HTMLVideoElement) => {
    const t = handoff.take(id);
    if (t < 0.3) return;
    const go = () => (v.currentTime = t);
    if (v.readyState >= 1) go();
    else v.addEventListener('loadedmetadata', go, { once: true });
  };
  function releaseOnRemove(v: HTMLVideoElement) {
    return () => {
      v.pause();
      v.removeAttribute('src');
      v.load();
    };
  }

  // A video fades in over its thumbnail once it has a frame on screen (not a
  // hard cut from the soft thumbnail to the sharp picture). Then the thumbnail
  // underneath is hidden (fitted on its own, its rounded size can differ a
  // little and peek out at the edges).
  let videoShown = $state<Record<number, boolean>>({}); // fading in / shown
  let thumbGone = $state<Record<number, boolean>>({}); // faded in: the thumbnail can go
  function trackShown(id: number) {
    return (v: HTMLVideoElement) => {
      let waiting = false;
      let raf = 0;
      let fallback = 0;
      let done = 0;
      const fadeIn = () => {
        if (videoShown[id] || raf) return;
        clearTimeout(fallback);
        raf = requestAnimationFrame(() => {
          videoShown[id] = true;
          done = window.setTimeout(() => (thumbGone[id] = true), reducedMotion() ? 0 : 260);
        });
      };
      // Only once a frame is really on screen: the browser says so with
      // requestVideoFrameCallback (iOS reports loadeddata/canplay before it
      // draws - fading in then showed a blank). A timer in case it never does.
      const shown = () => {
        if (waiting || videoShown[id]) return;
        waiting = true;
        if ('requestVideoFrameCallback' in v) v.requestVideoFrameCallback(fadeIn);
        fallback = window.setTimeout(fadeIn, 'requestVideoFrameCallback' in v ? 700 : 120);
      };
      v.addEventListener('loadeddata', shown);
      v.addEventListener('playing', shown);
      if (v.readyState >= 2) shown();
      return () => {
        cancelAnimationFrame(raf);
        clearTimeout(fallback);
        clearTimeout(done);
        v.removeEventListener('loadeddata', shown);
        v.removeEventListener('playing', shown);
        delete videoShown[id];
        delete thumbGone[id];
      };
    };
  }

  $effect(() => {
    results.ensure(index - 20, index + 60);
  });

  const at = (i: number) => {
    results.version;
    return results.fileAt(i);
  };
  let cur = $derived(at(index));
  /** The heart: the file in view becomes a favorite or stops being one (no notice: the heart shows it).
   *  On the Favorites page it leaves the list, like a deleted file: the next one shows (none left: closed). */
  /** The "I came" button (Settings > Viewing): counted, with Undo for a mis-tap. */
  async function countClimax() {
    const f = cur;
    if (!f) return;
    try {
      const res = await api.climax(f.id);
      results.patch(f.id, { came: res.count });
      toasts.show(t(res.count === 1 ? 'viewer.countedFirst' : 'viewer.counted', { what: itemName(f.id), n: res.count }), { cat: 'came',
        action: {
          label: t('common.undo'),
          run: async () => {
            const u = await api.undoClimax(res.id);
            results.patch(f.id, { came: u.count });
          },
        },
      });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  async function toggleFavorite() {
    if (!cur) return;
    await setFavorite([cur.id], !cur.favorite, true);
    if (!viewerEl?.isConnected) return; // closed meanwhile: setting the position would open it again
    if (results.total === 0) onclose();
    else index = Math.min(index, results.total - 1);
  }
  // The details keep the last file while the next one's info is on its way
  // (never closing and reopening - which would scroll them back to the top).
  let panelFile = $state<FileInfo>();
  $effect(() => {
    if (cur) panelFile = cur;
  });
  // While scrubbing: the preview frame for that spot, shown over the video (from VideoControls).
  let scrubFrame = $state<{ url: string; size: string; pos: string } | null>(null);
  let prev = $derived(at(index - 1));
  let next = $derived(at(index + 1));
  let isVideo = $derived(cur?.mime.startsWith('video/') ?? false);
  let videoEl = $derived(cur ? (videoEls[cur.id] ?? undefined) : undefined);

  // ---- moving

  // A move slides the next file in, then makes it current. A new swipe or key
  // press during the slide finishes it at once, so there is no speed limit.
  let moving = false;
  let moveTimer: number | undefined;
  let moveTarget = 0;
  /** The file a move is going to (its video starts loading at once). */
  let incomingId = $state<number | null>(null);

  function finishMove() {
    if (!moving) return;
    clearTimeout(moveTimer);
    animate = false;
    dx = 0;
    index = moveTarget;
    moving = false;
    incomingId = null;
  }

  /** dragged: the move finishes a drag (a finger, or a mouse held down), so it
   *  slides on from where the drag left it - with a mouse, other moves are instant. */
  function go(step: number, dragged = false) {
    finishMove();
    const target = index + step;
    if (mouse && !dragged) {
      if (target < 0 || target >= results.total) return;
      // Instant, no slide (a click before - a zoom - leaves the slide transition on).
      animate = false;
      dx = 0;
      index = target;
      return;
    }
    if (target < 0 || target >= results.total) {
      animate = true;
      dx = 0;
      return;
    }
    moving = true;
    moveTarget = target;
    incomingId = at(target)?.id ?? null;
    animate = true;
    dx = -step * width;
    moveTimer = window.setTimeout(finishMove, 200);
  }

  // ---- still images: decoded ahead, off the main thread.
  // Decoding a big image when it's first drawn blocks the thread that follows
  // the finger, so a swipe started meanwhile lags. The neighbors (two ahead,
  // one behind) are decoded in the background (img.decode()); a neighbor gets
  // its full picture only once that's done - until then its thumbnail shows.
  const decoded = new SvelteSet<string>();
  const warming = new Map<string, HTMLImageElement>(); // kept alive until done
  const kept: string[] = []; // the most recent, so the decoded set stays small
  function warm(url: string) {
    if (decoded.has(url) || warming.has(url)) return;
    const img = new Image();
    img.decoding = 'async';
    img.src = url;
    warming.set(url, img);
    const done = () => {
      warming.delete(url);
      decoded.add(url);
      kept.push(url);
      while (kept.length > 12) decoded.delete(kept.shift()!);
    };
    img.decode().then(done, done); // a failed decode: let the element try itself
  }
  // A transparent pixel while the picture is decoded: an <img> without a src
  // draws a border and a broken-image box (the thumbnail behind shows instead).
  const BLANK = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
  const isStill = (f: { mime: string; type: string }) => f.mime.startsWith('image/') && f.type !== 'gif';
  $effect(() => {
    results.version;
    // While the opening animation runs, only the open file (it has the thread to itself).
    for (const k of hidden ? [0] : [0, 1, -1, 2, 3]) {
      const f = results.fileAt(index + k);
      if (f && isStill(f)) warm(viewURL(f));
    }
  });

  // The slides are keyed by file, so an image that slides in keeps its element
  // (already decoded) when it becomes current: no reload, no flash.
  let slides = $derived(
    [prev, cur, next].flatMap((f, slot) => (f ? [{ f, slot }] : [])),
  );

  // ---- gestures
  // Pointer events cover finger, pen and mouse alike. One finger: swipe
  // left/right = next/previous, down = close, up = details, tap = controls.
  // Images: two fingers pinch-zoom, one finger pans while zoomed, double-tap
  // zooms in/out. Videos: press and hold = 2x, double-tap the left/right third
  // = -/+10 s, the middle = sound on/off (one-handed; play/pause is the button). A single tap waits a moment to be sure
  // it isn't the first half of a double tap.

  const pointers = new Map<number, { x: number; y: number }>();
  let start: { x: number; y: number; t: number; id: number } | null = null;
  let axis: 'x' | 'down' | 'up' | 'pan' | null = null;

  let holdTimer: number | undefined;
  let holding = false;
  let heldRate = 1;
  let lastTap: { t: number; x: number } | null = null;
  let tapTimer: number | undefined;
  let flash = $state<{ text: string; side: 'left' | 'mid' | 'right'; n: number; hold?: boolean; icon?: 'volume' | 'mute' } | null>(null);
  let flashTimer: number | undefined;

  function showFlash(text: string, side: 'left' | 'mid' | 'right', icon?: 'volume' | 'mute') {
    clearTimeout(flashTimer);
    flash = { text, side, icon, n: (flash?.n ?? 0) + 1 };
    flashTimer = window.setTimeout(() => (flash = null), 700);
  }

  // Zoom of the current image: scale and offset in px from the center.
  let zoom = $state({ s: 1, x: 0, y: 0 });
  let zoomAnimate = $state(false);
  let zoomSpring = $state(false); // the next zoom animation springs (back from past a limit)

  // Zoomed in: the full-size picture, decoded in the background first (the
  // screen-sized one stays until it's ready).
  let zoomedIn = $derived(zoom.s > 1.15);
  $effect(() => {
    if (cur && zoomedIn && isStill(cur) && fullURL(cur) !== viewURL(cur)) warm(fullURL(cur));
  });
  const imgSrc = (f: Parameters<typeof viewURL>[0], current: boolean) => {
    if (!isStill(f)) return viewURL(f);
    if (current && zoomedIn && decoded.has(fullURL(f))) return fullURL(f);
    // Even the open file: drawing an image not decoded yet stalls the page (the
    // thumbnail shows meanwhile, e.g. while the opening animation ends).
    return decoded.has(viewURL(f)) ? viewURL(f) : undefined;
  };

  let pinch: { dist: number; mx: number; my: number; s: number; x: number; y: number } | null = null;
  let panFrom = { x: 0, y: 0 };
  // Middle button held on a zoomed picture: panning.
  let middlePan: { x: number; y: number; from: { x: number; y: number }; id: number; moved: boolean } | null = null;
  let middleUsed = false; // the press panned (or began zoomed): its auxclick mustn't close
  // Panning keeps going after the finger lets go (like Photos): the flick's
  // speed, slowing down, stopping at the picture's edges.
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

  let stageEl = $state<HTMLDivElement>();
  let height = $state(0);

  // A new file starts unzoomed.
  $effect(() => {
    index;
    stopGlide();
    zoomAnimate = false;
    zoom = { s: 1, x: 0, y: 0 };
  });

  const isZoomed = () => zoom.s > 1.01;

  /** Size of the image as fitted on screen at zoom 1. */
  function fitted() {
    const f = cur;
    if (!f || !f.width || !f.height || !width || !height) return { w: width, h: height, fit: 1 };
    const fit = Math.min(width / f.width, height / f.height);
    return { w: f.width * fit, h: f.height * fit, fit };
  }

  /** The zoom that shows the original's own pixels (100 %); twice the fit when
   *  the picture is smaller than the screen anyway. */
  function actualSize() {
    const k = 1 / fitted().fit;
    return k > 1.2 ? k : 2;
  }
  /** How big the picture is shown, in % of its own pixels. */
  let percent = $derived.by(() => {
    void width;
    void height;
    return cur && isStill(cur) && cur.width ? Math.round(fitted().fit * zoom.s * 100) : 0;
  });
  /** The picture on screen isn't the one wanted yet (still decoding / loading). */
  let loading = $derived.by(() => {
    if (!cur || !isStill(cur)) return false;
    void decoded.size;
    const want = zoomedIn ? fullURL(cur) : viewURL(cur);
    return !decoded.has(want);
  });

  /** At least 4x, or enough to see the original's own pixels. */
  function maxZoom() {
    return Math.min(16, Math.max(4, 1.5 / fitted().fit));
  }

  /** Keeps a zoomed image from being dragged away from the screen edges. */
  function clampZoom(z: { s: number; x: number; y: number }) {
    return clampFit(z, fitted(), { w: width, h: height });
  }

  /** A point relative to the stage center. */
  function fromCenter(clientX: number, clientY: number) {
    const r = stageEl!.getBoundingClientRect();
    return { x: clientX - (r.left + r.width / 2), y: clientY - (r.top + r.height / 2) };
  }

  /** Zooms to scale s, keeping the point under (clientX, clientY) in place. */
  function zoomTo(s: number, clientX: number, clientY: number, animated = false) {
    if (isVideo || !stageEl) return;
    s = Math.min(maxZoom(), Math.max(1, s));
    zoomAnimate = animated;
    zoom = clampZoom(zoomAround(zoom, s, fromCenter(clientX, clientY)));
  }

  function resetZoom() {
    stopGlide();
    zoomAnimate = true;
    zoom = { s: 1, x: 0, y: 0 };
  }

  function down(e: PointerEvent) {
    if (e.button === 1) {
      e.preventDefault(); // middle button: no auto-scroll (auxclick closes)
      if (isZoomed()) {
        // Zoomed: hold it to pan (it then doesn't close the viewer).
        stopGlide();
        middlePan = { x: e.clientX, y: e.clientY, from: { x: zoom.x, y: zoom.y }, id: e.pointerId, moved: false };
        try {
          stageEl?.setPointerCapture(e.pointerId);
        } catch {
          /* synthetic pointers (tests) */
        }
      }
      return;
    }
    if (e.button !== 0) return;
    stopGlide(); // a touch catches a gliding picture
    zoomSpring = false;
    finishMove(); // a swipe during a slide finishes it first
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    try {
      stageEl?.setPointerCapture(e.pointerId);
    } catch {
      /* synthetic pointers (tests) can't be captured */
    }

    if (pointers.size === 2 && !isVideo && cur) {
      // A second finger: pinch instead of swipe.
      clearTimeout(holdTimer);
      clearTimeout(tapTimer);
      lastTap = null;
      start = null;
      animate = true;
      dx = 0;
      dy = 0;
      const [a, b] = [...pointers.values()];
      pinch = {
        dist: Math.hypot(a.x - b.x, a.y - b.y) || 1,
        mx: (a.x + b.x) / 2,
        my: (a.y + b.y) / 2,
        s: zoom.s,
        x: zoom.x,
        y: zoom.y,
      };
      zoomAnimate = false;
      return;
    }
    if (pointers.size > 1) return;

    start = { x: e.clientX, y: e.clientY, t: performance.now(), id: e.pointerId };
    axis = null;
    animate = false;
    zoomAnimate = false;
    panFrom = { x: zoom.x, y: zoom.y };
    clearTimeout(holdTimer);
    if (isVideo && videoEl) {
      holdTimer = window.setTimeout(() => {
        if (!start || axis !== null || !videoEl) return;
        holding = true;
        heldRate = videoEl.playbackRate;
        videoEl.playbackRate = 2;
        if (videoEl.paused) videoEl.play().catch(() => {});
        clearTimeout(flashTimer); // an earlier indicator's fade must not clear this one
        flash = { text: '2× ▸▸', side: 'mid', n: (flash?.n ?? 0) + 1, hold: true };
      }, 450);
    }
  }

  function tapped(e: PointerEvent) {
    // With the details open, a tap beside them only closes them - at once (not
    // after waiting to see whether it's a double tap).
    if (info) {
      clearTimeout(tapTimer);
      lastTap = null;
      info = false;
      return;
    }
    // A mouse click: at once (no waiting for a double click). A picture zooms
    // to 100 % at the pointer and back to fit; a video plays/pauses.
    if (mouse && e.pointerType === 'mouse') {
      if (isVideo && videoEl) {
        const v = videoEl;
        if (v.paused) v.play().catch(() => {});
        else v.pause();
        showFlash(v.paused ? '❚❚' : '▶', 'mid');
      } else if (isZoomed()) resetZoom();
      else zoomTo(actualSize(), e.clientX, e.clientY, true);
      return;
    }
    const now = performance.now();
    if (lastTap && now - lastTap.t < 320 && Math.abs(e.clientX - lastTap.x) < 60) {
      // Double tap.
      clearTimeout(tapTimer);
      lastTap = null;
      if (!isVideo || !videoEl) {
        if (isZoomed()) resetZoom();
        else zoomTo(actualSize(), e.clientX, e.clientY, true);
        return;
      }
      const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
      const zone = (e.clientX - r.left) / r.width;
      const v = videoEl;
      if (zone < 1 / 3) {
        v.currentTime = Math.max(0, v.currentTime - 10);
        showFlash(t('viewer.back10'), 'left');
      } else if (zone > 2 / 3) {
        v.currentTime = Math.min(v.duration || Infinity, v.currentTime + 10);
        showFlash(t('viewer.forward10'), 'right');
      } else {
        v.muted = !v.muted;
        if (!v.muted && volume.settable) v.volume = volume.level;
        showFlash(v.muted ? t('viewer.soundOff') : t('viewer.soundOn'), 'mid', v.muted ? 'mute' : 'volume');
      }
      return;
    }
    lastTap = { t: now, x: e.clientX };
    tapTimer = window.setTimeout(() => {
      lastTap = null;
      if (info) info = false; // a tap outside the details closes them
      else chrome = !chrome;
    }, 320);
  }

  function move(e: PointerEvent) {
    if (middlePan && e.pointerId === middlePan.id) {
      zoomAnimate = false;
      zoom = clampZoom({ s: zoom.s, x: middlePan.from.x + e.clientX - middlePan.x, y: middlePan.from.y + e.clientY - middlePan.y });
      middlePan.moved = true;
      return;
    }
    if (!pointers.has(e.pointerId)) return;
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });

    if (pinch) {
      if (pointers.size < 2) return;
      const [a, b] = [...pointers.values()];
      // Past the limits (smaller than fitted, bigger than the most) it goes on
      // with resistance, and springs back when let go.
      const raw = (pinch.s * Math.hypot(a.x - b.x, a.y - b.y)) / pinch.dist;
      const max = maxZoom();
      const s = pinchScale(raw, max);
      // Keep the image point that was first under the fingers' midpoint under it.
      const m0 = fromCenter(pinch.mx, pinch.my);
      const m = fromCenter((a.x + b.x) / 2, (a.y + b.y) / 2);
      const px = (m0.x - pinch.x) / pinch.s;
      const py = (m0.y - pinch.y) / pinch.s;
      // Free under the fingers: kept inside the screen edges only once they let
      // go (held at the edges while pinching, it stuck there, then jumped).
      zoom = { s, x: m.x - px * s, y: m.y - py * s };
      return;
    }

    if (!start || e.pointerId !== start.id) return;
    const mx = e.clientX - start.x;
    const my = e.clientY - start.y;
    if (axis === null && Math.hypot(mx, my) > 10) {
      if (holding) return; // a held finger may wobble; don't turn it into a swipe
      clearTimeout(holdTimer);
      axis = isZoomed() ? 'pan' : Math.abs(mx) > Math.abs(my) ? 'x' : my > 0 ? 'down' : 'up';
    }
    if (axis === 'pan') {
      zoom = clampZoom({ s: zoom.s, x: panFrom.x + mx, y: panFrom.y + my });
      glider.record(e.clientX, e.clientY);
    } else if (axis === 'x') {
      // Resist at the ends of the results.
      const edge = (mx > 0 && index === 0) || (mx < 0 && index === results.total - 1);
      dx = edge ? mx / 4 : mx;
    } else if (axis === 'down') {
      dy = Math.max(0, my);
      hideSides = true;
      clearTimeout(hideSidesTimer);
    }
  }

  function up(e: PointerEvent) {
    if (middlePan && e.pointerId === middlePan.id) {
      middlePan = null;
      middleUsed = true; // its auxclick comes next: not a close
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
          const r = stageEl!.getBoundingClientRect();
          zoomTo(maxZoom(), r.left + r.width / 2 + zoom.x / 2, r.top + r.height / 2 + zoom.y / 2, true);
        } else {
          // Pinched past an edge: it settles back against it.
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
    clearTimeout(holdTimer);
    if (holding) {
      holding = false;
      if (videoEl) videoEl.playbackRate = heldRate;
      flash = null;
      start = null;
      return;
    }
    const dt = performance.now() - start.t;
    const mx = e.clientX - start.x;
    const my = e.clientY - start.y;
    const fast = Math.abs(mx) / dt > 0.5;
    animate = true;
    if (axis === 'pan') {
      glide(); // let go: it keeps going a little
    } else if (axis === 'x' && (Math.abs(mx) > width * 0.2 || fast) && Math.abs(mx) > 30) {
      go(mx < 0 ? 1 : -1, true);
    } else if (axis === 'down' && dy > 110) {
      // Phones: the details sheet covers the picture - it goes first. Wide
      // screens: the details are a column beside it - the viewer closes.
      if (info && isSheet()) info = false;
      else onclose();
    } else if (axis === 'up' && -my > 70) {
      info = true;
    } else if (axis === null && dt < 350) {
      tapped(e);
      dx = 0;
    } else {
      dx = 0;
    }
    dy = 0;
    if (hideSides) hideSidesTimer = window.setTimeout(() => (hideSides = false), 250);
    start = null;
  }

  // ---- mouse wheel (desktop): next/previous; Ctrl + wheel (or a trackpad
  // pinch) zooms; a zoomed image scrolls instead. Attached non-passive so it
  // can stop the browser's own page zoom.
  // The wheel moves between files as fast as it turns: a mouse wheel's click
  // (one big step) is one file; a trackpad's many small steps add up to one.
  let wheelSum = 0;
  let wheelAt = -Infinity;
  let lastZoomWheel = -Infinity; // the wheel last zoomed (navigation waits until it rests)
  $effect(() => {
    if (!stageEl) return;
    return on(
      stageEl,
      'wheel',
      (e: WheelEvent) => {
        e.preventDefault();
        if (e.ctrlKey) {
          zoomTo(zoom.s * Math.exp(-e.deltaY * 0.01), e.clientX, e.clientY);
          return;
        }
        const now = performance.now();
        // Zoomed: the wheel zooms (around the pointer), down to the fit and no
        // further. The same spin then can't carry on into the next file: the
        // wheel has to rest a moment first.
        if (isZoomed()) {
          const lines = e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? 800 : 1;
          const s = zoom.s * Math.exp(-e.deltaY * lines * 0.0015);
          lastZoomWheel = now;
          if (s <= 1.01) resetZoom();
          else zoomTo(s, e.clientX, e.clientY);
          return;
        }
        if (now - lastZoomWheel < 400) {
          lastZoomWheel = now; // still the spin that zoomed out
          return;
        }
        const px = e.deltaY * (e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? 800 : 1);
        if (Math.abs(px) >= 50) {
          wheelSum = 0;
          go(px > 0 ? 1 : -1);
          return;
        }
        if (now - wheelAt > 200 || Math.sign(px) !== Math.sign(wheelSum)) wheelSum = 0; // a new spin
        wheelAt = now;
        wheelSum += px;
        if (Math.abs(wheelSum) >= 120) {
          go(wheelSum > 0 ? 1 : -1);
          wheelSum = 0;
        }
      },
      { passive: false },
    );
  });

  // ---- Back closes the viewer: opening it adds a history entry, Back (or the
  // iPhone's edge swipe) pops it and we close. Closing any other way pops it.
  onMount(() => {
    history.pushState({ ...(history.state ?? {}), viewer: true }, '');
    const onPop = () => {
      if (!history.state?.viewer) onclose();
    };
    addEventListener('popstate', onPop);
    return () => {
      removeEventListener('popstate', onPop);
      if (history.state?.viewer) history.back();
    };
  });

  // ---- the details: a sheet on phones (slides up; dragging its header down
  // closes it), a column on wide screens (the column grows and shrinks - CSS,
  // .viewer grid - so the panel slides in from the edge and the picture makes
  // room; on closing the panel stays while its column shrinks away).
  const isSheet = () => matchMedia('(max-width: 899px)').matches;
  let sheetGone = 0; // how far the sheet was dragged when it was let go (it leaves from there)
  // In and out separately (a two-way transition: plays its way in backwards to
  // leave - from where the sheet rests, so a dragged one jumped up first).
  function sheet(node: Element, { out = false } = {}) {
    if (reducedMotion()) return { duration: 0 };
    if (out && isSheet() && sheetGone > 0) {
      const from = sheetGone;
      sheetGone = 0;
      return { duration: 200, easing: cubicOut, css: (t: number) => `transform: translateY(${px(from * t + (1 - t) * (node as HTMLElement).offsetHeight)})` };
    }
    if (!isSheet()) return { duration: 260, css: () => '' };
    return { duration: 240, easing: cubicOut, css: (t: number) => `transform: translateY(${px((1 - t) * (node as HTMLElement).offsetHeight)})` };
  }
  // Phones: dragged down from anywhere on it closes it (lib/sheetDrag: a list scrolled down scrolls first).
  let sheetDragging = $state(false);
  let sheetDy = $state(0);
  const sheetDrag = dragDown({
    active: isSheet,
    onstart: () => (sheetDragging = true),
    onmove: (d) => (sheetDy = d),
    onend: (d, fast) => {
      sheetDragging = false;
      if (d > 90 || (fast && d > 20)) {
        // Closed: it stays where it was let go and slides on from there (put back
        // to its place first, it jumped up for a frame before sliding away).
        sheetGone = d;
        info = false;
        return;
      }
      sheetDy = 0;
    },
  });
  // Opened again: in its place.
  $effect(() => {
    if (info) untrack(() => (sheetDy = 0));
  });

  // ---- keys

  function key(e: KeyboardEvent) {
    if (e.defaultPrevented) return; // handled already (e.g. Escape in a menu)
    wake();
    const t = e.target as HTMLElement;
    if (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable) return;
    switch (e.key) {
      case 'ArrowRight':
        go(1);
        break;
      case 'ArrowLeft':
        go(-1);
        break;
      case 'Escape':
        if (info) info = false;
        else onclose();
        break;
      case 'i':
        info = !info;
        break;
      case 'l':
        toggleFavorite();
        break;
      case 'c':
        if (settings.came) countClimax();
        break;
      case 'f':
        if (!fullscreen.supported) return;
        fullscreen.toggle(viewerEl ?? null);
        break;
      case 'Delete':
        remove();
        break;
      case ' ':
        if (videoEl) {
          e.preventDefault();
          if (videoEl.paused) videoEl.play().catch(() => {});
          else videoEl.pause();
        }
        break;
      case 'm':
        if (videoEl) {
          videoEl.muted = !videoEl.muted;
          if (!videoEl.muted && volume.settable) videoEl.volume = volume.level;
        }
        break;
      default:
        return;
    }
  }

  // ---- delete with undo

  // In the trash: back into the library, or removed for good (then the next file, as after a delete).
  async function restoreCurrent() {
    const f = cur;
    if (!f || !(await restoreFiles(results, [f.id]))) return;
    afterGone();
  }
  function afterGone() {
    if (!viewerEl?.isConnected) return;
    if (results.total === 0) onclose();
    else index = Math.min(index, results.total - 1);
  }
  async function remove() {
    const f = cur;
    if (!f) return;
    if (results.scope === 'trash') {
      if (await purgeFiles(results, [f.id], f.thumb ? [f.thumb] : [])) afterGone();
      return;
    }
    const ok = await confirmAction({
      title: t('selection.deleteTitle', { n: 1 }),
      body: t('selection.deleteBody'),
      action: t('common.delete'),
      thumbs: f.thumb ? [f.thumb] : [],
      undoable: true,
    });
    if (!ok || cur?.id !== f.id) return;
    try {
      await api.remove([f.id]);
    } catch (e) {
      toasts.error((e as Error).message);
      return;
    }
    toasts.show(t('common.deletedOne', { what: itemName(f.id) }), { cat: 'deleted',
      kind: 'removed',
      action: {
        label: t('common.undo'),
        run: async () => {
          await api.restore([f.id]);
          await results.refresh();
        },
      },
    });
    await results.refresh();
    if (!viewerEl?.isConnected) return; // closed meanwhile (as above)
    if (results.total === 0) onclose();
    else index = Math.min(index, results.total - 1);
  }
</script>

<svelte:window onkeydown={key} />

<!-- svelte-ignore a11y_no_static_element_interactions (the mouse moving only shows the controls again; nothing to press) -->
<div
  class="viewer"
  bind:this={viewerEl}
  class:info
  class:hidden
  class:idle={!chrome && mouse}
  style:--fade={1 - Math.min(dy / 400, 0.6)}
  style:--chrome-fade={1 - Math.min(dy / 160, 1)}
  onpointermove={(e) => e.pointerType === 'mouse' && wake()}
  onpointerdowncapture={() => chrome && armIdle()}>
  <!-- The browser never drags a picture or text from here (it would steal swipes, scrubbing and zoom drags). -->
  <div class="stage-wrap" role="presentation" ondragstart={(e) => e.preventDefault()}>
    <div
      class="stage"
      class:zoom-in={mouse && percent > 0 && zoom.s <= 1.01}
      class:zoom-out={mouse && percent > 0 && zoom.s > 1.01}
      bind:this={stageEl}
      bind:clientWidth={width}
      bind:clientHeight={height}
      role="presentation"
      onpointerdown={down}
      onpointermove={move}
      onpointerup={up}
      onpointercancel={up}
      onauxclick={(e) => {
        // Middle-click closes - not when it was holding a zoomed picture to pan it.
        if (e.button === 1 && !middleUsed) onclose();
        middleUsed = false;
      }}>
      <div
        class="track"
        class:animate
        style:transform="translate3d(0, {dy}px, 0) scale({1 - Math.min(dy / 2000, 0.15)})">
        {#each slides as s (s.f.id)}
          {@const f = s.f}
          <div
            class="slide"
            data-slot={s.slot}
            class:animate
            style:visibility={hideSides && s.slot !== 1 ? 'hidden' : undefined}
            style:transform="translate3d(calc({(s.slot - 1) * 100}% + {dx}px), 0, 0)">
            {#if f.mime.startsWith('video/')}
              <!-- Like hydrus-web: the video gets the thumbnail as its poster (so it never
                   shows a blank frame) and is created when a move towards it starts. The
                   thumbnail also stays underneath, the same element that slid in. -->
              {#if f.thumb}<img class="under" src={f.thumb} alt="" draggable="false" style:visibility={thumbGone[f.id] ? 'hidden' : undefined} />{/if}
              {#if s.slot === 1 || f.id === incomingId}
                <!-- svelte-ignore a11y_media_has_caption (downloaded media has no caption tracks) -->
                <video bind:this={videoEls[f.id]} {@attach releaseOnRemove} {@attach trackShown(f.id)} {@attach resumeFrom(f.id)} class:shown={videoShown[f.id]} src={f.url} poster={f.thumb} playsinline preload="auto"></video>
              {/if}
              {#if s.slot === 1 && scrubFrame && f.width && f.height}
                <!-- Scrubbing: the frame for that spot at once, exactly where the picture is (contained). -->
                <div class="scrub-wrap" aria-hidden="true">
                  <div
                    class="scrub-frame"
                    style:--ar={f.width / f.height}
                    style:background-image="url({scrubFrame.url})"
                    style:background-size={scrubFrame.size}
                    style:background-position={scrubFrame.pos}>
                  </div>
                </div>
              {/if}
            {:else}
              <img
                src={imgSrc(f, s.slot === 1) ?? BLANK}
                alt=""
                draggable="false"
                decoding="async"
                class:zoom-anim={s.slot === 1 && zoomAnimate}
                class:spring={s.slot === 1 && zoomAnimate && zoomSpring}
                style:transform={s.slot === 1 && zoom.s !== 1
                  ? `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.s})`
                  : undefined}
                style:background-image={f.thumb ? `url(${f.thumb})` : undefined} />
            {/if}
          </div>
        {/each}
      </div>
    </div>

    {#if flash}
      {#key flash.n}
        <div class="flash {flash.side}" class:hold={flash.hold} class:icon-only={!!flash.icon} aria-live="polite">
          {#if flash.icon}<Icon name={flash.icon} size={30} /><span class="sr-only">{flash.text}</span>{:else}{flash.text}{/if}
        </div>
      {/key}
    {/if}

    <!-- Controls sit in their own layer above the slides (never inside the swipe area). -->
    <div class="top float-bar" class:hide={!chrome || !entered} role="presentation" onpointerenter={() => (overControls = true)} onpointerleave={() => (overControls = false)}>
      <button class="icon" aria-label={t('sites.close')} onclick={onclose}><Icon name="close" /></button>
      <span class="count chip">{num(index + 1)} / {num(results.total)}</span>
      {#if index < results.pinned}<span class="chip new" title={t('viewer.newTitle')} transition:nudge>{t('grid.new')}</span>{/if}
      {#if percent}<span class="chip pct" title={t('viewer.percentTitle', { n: percent })} transition:nudge>{t('viewer.percent', { n: percent })}</span>{/if}
      {#if loading}<span class="spinner" role="status" aria-label={t('viewer.loading')} transition:pop></span>{/if}
      <span class="spacer"></span>
      {#if cur}
        <button class="icon fav" class:on={cur.favorite} aria-label={t('filter.favorite.label')} aria-pressed={!!cur.favorite} title={cur.favorite ? t('viewer.unfavoriteKey') : t('viewer.favoriteKey')}
          onclick={toggleFavorite}><Icon name="heart" filled={!!cur.favorite} /></button>
        {#if settings.came}
          <button class="icon came" aria-label={cur.came ? t('viewer.cameSoFar', { n: cur.came }) : t('rankings.title')} title={t('viewer.cameKey')} onclick={countClimax}
            ><Icon name="drop" />{#if cur.came}<span class="came-n" transition:nudge>{cur.came}</span>{/if}</button>
        {/if}
      {/if}
      {#if fullscreen.supported && !isVideo}
        <!-- (a video has its own, in its controls) -->
        <button class="icon" aria-label={fullscreen.on ? t('video.exitFullscreen') : t('video.fullscreen')} title={fullscreen.on ? t('video.exitFullscreenKey') : t('video.fullscreenKey')}
          onclick={() => fullscreen.toggle(viewerEl ?? null)}><Icon name={fullscreen.on ? 'shrink' : 'expand'} /></button>
      {/if}
      <button class="icon" class:on={info} aria-label={t('fileMenu.details')} title={t('viewer.detailsKey')} aria-pressed={info} onclick={() => (info = !info)}
        ><Icon name="info" /></button>
    </div>
    {#if isVideo && videoEl}
      <div class="bottom" class:hide={!chrome || !entered} role="presentation" onpointerenter={() => (overControls = true)} onpointerleave={() => (overControls = false)}>
        <VideoControls video={videoEl} fileId={cur?.id ?? 0} bind:scrubFrame />
      </div>
    {/if}
    {#if wide.matches}
      <button class="side-nav left" class:hide={!chrome} aria-label={t('viewer.previous')} onclick={() => go(-1)} disabled={index === 0}><Icon name="left" size={28} /></button>
      <button class="side-nav right" class:hide={!chrome} aria-label={t('viewer.next')} onclick={() => go(1)} disabled={index >= results.total - 1}
        ><Icon name="right" size={28} /></button>
    {/if}
  </div>

  {#if info && panelFile}
    <aside
      class="panel"
      {@attach backClose(() => (info = false))}
      {@attach sheetDrag}
      class:dragging={sheetDragging}
      aria-label={t('fileMenu.details')}
      style:transform={sheetDy ? `translateY(${sheetDy}px)` : undefined}
      in:sheet
      out:sheet={{ out: true }}>
      <header class="panel-head">
        <span class="grab" aria-hidden="true"></span>
        <h2>{t('fileMenu.details')}</h2>
        <!-- Delete: a quiet icon here (red when pointed at); it asks first. -->
        {#if results.scope === 'trash'}
          <button class="icon" aria-label={t('trash.restore')} title={t('trash.restore')} onclick={restoreCurrent}><Icon name="undo" /></button>
        {/if}
        <button class="icon delete" aria-label={results.scope === 'trash' ? t('trash.purge') : t('viewer.deleteFile')} title={results.scope === 'trash' ? t('trash.purge') : t('viewer.deleteKey')} onclick={remove}><Icon name="trash" /></button>
        <button class="icon" aria-label={t('viewer.closeDetails')} title={t('viewer.closeKey')} onclick={() => (info = false)}><Icon name="close" /></button>
      </header>
      <div class="panel-body" {@attach overlayScroll}>
        <FilePanel file={panelFile} {onsearch} />
      </div>
    </aside>
  {/if}
</div>

<style>
  /* While the open animation (lib/hero) plays, the file is hidden (shown at
     its last frame), but the controls already fade in, above the animation
     (which runs just under the viewer: nothing changes layer when it ends). */
  .viewer.hidden {
    background: transparent;
  }
  .viewer.hidden > :not(.stage-wrap),
  .viewer.hidden .stage-wrap > :not(.top, .bottom) {
    opacity: 0;
  }
  .viewer {
    position: fixed;
    inset: 0;
    z-index: 40;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    background: rgb(17 17 27 / var(--fade));
  }
  /* Wide screens: the details column (0 when closed) grows and shrinks smoothly;
     the panel keeps its width, so it slides in from the edge (clipped here). */
  @media (min-width: 900px) {
    .viewer {
      grid-template-columns: minmax(0, 1fr) 0px;
      overflow: hidden;
      transition: grid-template-columns 0.26s cubic-bezier(0.2, 0.8, 0.2, 1);
    }
    .viewer.info {
      grid-template-columns: minmax(0, 1fr) 340px;
    }
    .panel {
      width: 340px;
      grid-column: 2;
      grid-row: 1;
    }
  }
  @media (min-width: 900px) and (prefers-reduced-motion: reduce) {
    .viewer {
      transition: none;
    }
  }
  /* Its own stacking context: the slides' layers (will-change, zoom, video) stay inside it - on an iPhone a promoted
     picture could otherwise be drawn over the details sheet (the user: "it appears behind the image with just the top
     10% of the panel showing"). */
  .stage-wrap {
    isolation: isolate;
    z-index: 0;
    position: relative;
    user-select: none;
    -webkit-user-select: none;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  .stage {
    position: absolute;
    inset: 0;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
  }
  /* A mouse: the pointer says what a click does. */
  .stage.zoom-in {
    cursor: zoom-in;
  }
  .stage.zoom-out {
    cursor: zoom-out;
  }
  .track {
    position: relative;
    height: 100%;
  }
  .slide {
    position: absolute;
    inset: 0;
    will-change: transform;
  }
  .track.animate,
  .slide.animate {
    transition: transform 0.2s ease-out;
  }
  .zoom-anim {
    transition: transform 0.22s ease-out;
  }
  /* Springing back from past a limit: a little overshoot, settling. */
  .zoom-anim.spring {
    transition: transform 0.45s cubic-bezier(0.34, 1.45, 0.64, 1);
  }
  .scrub-wrap {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    container-type: size;
    pointer-events: none;
  }
  /* The video's contained box: as wide as fits both the width and the height. */
  .scrub-frame {
    width: min(100cqw, calc(100cqh * var(--ar)));
    aspect-ratio: var(--ar);
    background-repeat: no-repeat;
  }
  .slide img,
  .slide video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    background: center / contain no-repeat;
  }
  /* The thumbnail under a video: fitted on its own, its rounded size can be a
     pixel bigger than the video's - trimmed, so it never peeks out around it. */
  .slide img.under {
    clip-path: inset(1px);
  }
  .slide video {
    opacity: 0;
    transition: opacity 0.24s ease-out;
  }
  .slide video.shown {
    opacity: 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .slide video {
      transition: none;
    }
  }
  .top button.came {
    position: relative;
  }
  /* How many times so far: a small count on the button. */
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
  /* A new file (as on its thumbnail). */
  .chip.new {
    padding: 3px 8px;
    background: var(--mauve);
    color: var(--crust);
    font-size: 11px;
    font-weight: 800;
    letter-spacing: 0.04em;
  }
  .spacer {
    flex: 1;
  }
  .bottom {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    transition: opacity 0.2s;
    opacity: var(--chrome-fade, 1);
  }
  .flash {
    position: absolute;
    top: 50%;
    transform: translate(-50%, -50%);
    padding: 10px 16px;
    border-radius: 999px;
    background: rgb(17 17 27 / 0.7);
    color: var(--text);
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    pointer-events: none;
    animation: flash 0.7s ease-out forwards;
  }
  /* An icon (sound on/off): a round badge. */
  .flash.icon-only {
    display: grid;
    place-items: center;
    padding: 14px;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  .flash.hold {
    animation: none;
  }
  .flash.left {
    left: 20%;
  }
  .flash.mid {
    left: 50%;
  }
  .flash.right {
    left: 80%;
  }
  @keyframes flash {
    0%,
    60% {
      opacity: 1;
    }
    100% {
      opacity: 0;
    }
  }
  .hide {
    opacity: 0;
    pointer-events: none;
  }
  /* Idle with a mouse: the cursor goes with the controls. */
  .viewer.idle,
  .viewer.idle .stage {
    cursor: none;
  }
  /* The details: a column on the right (wide screens), a sheet over the
     bottom three quarters (phones). A header with the close button; the body scrolls. */
  .panel {
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--mantle);
    border-left: 1px solid var(--surface0);
  }
  .panel-head {
    flex: none;
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: calc(8px + var(--safe-top)) 8px 8px 16px;
    border-bottom: 1px solid var(--surface0);
  }
  @media (hover: hover) {
    .panel-head .delete:hover {
      color: var(--red);
      background: color-mix(in srgb, var(--red) 12%, transparent);
    }
  }
  .panel-head h2 {
    flex: 1;
    margin: 0;
    font-size: 17px;
    font-weight: 700;
  }
  .grab {
    display: none;
  }
  .panel-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 16px 16px calc(24px + var(--bottom-room));
  }
  @media (max-width: 899px) {
    .panel {
      position: absolute;
      left: 0;
      right: 0;
      bottom: var(--kb, 0px); /* above the on-screen keyboard */
      height: 75%;
      max-height: min(var(--sheet-max-h), calc(100% - var(--kb, 0px) - 56px));
      border-left: none;
      border-radius: 20px 20px 0 0;
      box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
      z-index: 3;
      will-change: transform; /* (its own layer, always drawn over the picture's - iOS) */
      transition: transform 0.22s ease-out;
    }
    .panel.dragging {
      transition: none;
    }
    .panel-head {
      padding: 18px 8px 6px 20px;
      touch-action: none; /* drag it down to close */
      cursor: grab;
    }
    .grab {
      display: block;
      position: absolute;
      top: 7px;
      left: 50%;
      width: 38px;
      height: 5px;
      margin-left: -19px;
      border-radius: 3px;
      background: var(--surface2);
    }
    .panel-body {
      padding: 16px 20px calc(24px + var(--bottom-room));
    }
  }
</style>
