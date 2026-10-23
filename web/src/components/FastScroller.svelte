<script lang="ts">
  // The grid's fast scroller (like Google Photos): a tab tucked against the right
  // edge (a sliver shows) that slides in while scrolling; drag it to jump anywhere (only
  // the handle: a press beside it is the grid's), with a bubble saying where you are - the date of the
  // files there (from a sample of the result's dates, so it's instant even
  // where nothing is loaded yet) - and year marks along the track.
  import Icon from './Icon.svelte';
  import { t, date } from '../lib/i18n.svelte';

  let {
    el,
    scrollTop,
    contentHeight,
    height,
    total,
    dates,
    dragging = $bindable(false),
    side = 'right',
    top = 0,
    controls = 'grid-scroller',
  }: {
    controls?: string; // the scroller's element ID
    el: HTMLElement | undefined;
    scrollTop: number;
    contentHeight: number;
    height: number;
    total: number;
    dates: number[] | null; // evenly spaced samples of the result's dates (ms), or null: show positions
    dragging?: boolean;
    side?: 'left' | 'right'; // phones: the side the thumb scrolls on
    top?: number; // the part of the scroller at the top that's covered (the track starts below it)
  } = $props();

  const PAD = 8;
  const THUMB = 48;
  let track = $derived(Math.max(0, height - top - PAD * 2));
  let maxScroll = $derived(Math.max(1, contentHeight - height));
  let ratio = $derived(Math.min(1, Math.max(0, scrollTop / maxScroll)));
  let thumbY = $derived(top + PAD + ratio * Math.max(0, track - THUMB));

  // Grows into the tab while scrolling, then shrinks back after a moment.
  let awake = $state(false);
  let hover = $state(false);
  let sleepTimer = 0;
  let lastTop = -1;
  $effect(() => {
    if (lastTop >= 0 && Math.abs(scrollTop - lastTop) > 1) wake();
    lastTop = scrollTop;
  });
  function wake() {
    awake = true;
    clearTimeout(sleepTimer);
    sleepTimer = window.setTimeout(() => {
      if (!dragging && !hover) awake = false;
    }, 1400);
  }

  // ---- dragging
  let grab = 0; // where on the thumb it was taken (px from its top)
  let root = $state<HTMLElement>();

  function scrollTo(clientY: number) {
    if (!el || !root) return;
    const rootTop = root.getBoundingClientRect().top;
    const r = (clientY - rootTop - grab - top - PAD) / Math.max(1, track - THUMB);
    el.scrollTop = Math.min(1, Math.max(0, r)) * maxScroll;
  }

  function down(e: PointerEvent) {
    if (e.button !== 0 || !root) return;
    e.preventDefault();
    e.stopPropagation();
    const rootTop = root.getBoundingClientRect().top;
    // Keep the grab point.
    grab = e.clientY - rootTop - thumbY;
    dragging = true;
    awake = true;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }
  function move(e: PointerEvent) {
    if (dragging) scrollTo(e.clientY);
  }
  function up() {
    if (!dragging) return;
    dragging = false;
    wake();
  }

  // ---- what the bubble says
  let span = $derived(dates && dates.length > 1 ? Math.abs(dates[0] - dates[dates.length - 1]) : 0);
  const DAY = 86_400_000;
  function dateLabel(t: number): string {
    const d = new Date(t);
    // A short span: the day; otherwise month and year.
    return span < 62 * DAY
      ? date(d, { day: 'numeric', month: 'short', year: span < 300 * DAY && d.getFullYear() === new Date().getFullYear() ? undefined : 'numeric' })
      : date(d, { month: 'long', year: 'numeric' });
  }
  let label = $derived.by(() => {
    if (dates && dates.length) {
      const t = dates[Math.round(ratio * (dates.length - 1))];
      if (t > 0) return dateLabel(t);
    }
    const pos = Math.min(total, Math.max(1, Math.round(ratio * (total - 1)) + 1));
    return t('common.posOf', { pos, total });
  });

  // Year marks where the year changes (spaced out so they never overlap).
  let marks = $derived.by(() => {
    if (!dates || dates.length < 2 || span < 400 * DAY) return [];
    const out: { y: number; text: string }[] = [];
    let last = new Date(dates[0]).getFullYear();
    let lastY = -100;
    for (let k = 1; k < dates.length; k++) {
      const year = new Date(dates[k]).getFullYear();
      if (year === last || !dates[k]) continue;
      last = year;
      const y = top + PAD + (k / (dates.length - 1)) * Math.max(0, track - THUMB) + THUMB / 2;
      if (y - lastY < 22) continue;
      lastY = y;
      out.push({ y, text: String(year) });
    }
    return out;
  });
</script>

<div
  class="fast"
  class:left={side === 'left'}
  class:awake={awake || dragging || hover}
  class:dragging
  bind:this={root}
  role="presentation"
  onpointermove={move}
  onpointerup={up}
  onpointercancel={up}
  onpointerenter={(e) => {
    if (e.pointerType === 'mouse') {
      hover = true;
      awake = true;
    }
  }}
  onpointerleave={() => {
    hover = false;
    wake();
  }}>
  {#if dragging}
    {#each marks as m (m.text)}
      <span class="mark" style:top="{m.y}px">{m.text}</span>
    {/each}
  {/if}
  <div
    class="thumb"
    role="scrollbar"
    aria-label={t('grid.fastScroll')}
    aria-controls={controls}
    aria-orientation="vertical"
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={Math.round(ratio * 100)}
    aria-valuetext={label}
    tabindex="-1"
    style:transform="translateY({thumbY}px)"
    onpointerdown={down}>
    <span class="tab"><Icon name="grip" size={16} /></span>
    {#if dragging}
      <span class="bubble">{label}</span>
    {/if}
  </div>
</div>

<style>
  /* Against the page's edge: past the grid's side gap (--grid-gutter, set by the library). */
  .fast {
    position: absolute;
    top: 0;
    right: calc(-1 * var(--grid-gutter, 0px));
    bottom: 0;
    width: 34px; /* the touch area; the visible handle is narrower */
    z-index: 3;
    pointer-events: none; /* only the handle takes presses */
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
  }
  .thumb {
    position: absolute;
    top: 0;
    right: 0;
    width: 34px;
    height: 48px;
    outline: none;
    cursor: grab;
    pointer-events: auto;
  }
  .dragging .thumb {
    cursor: grabbing;
  }
  /* A tab with a grip, easy to catch with a thumb. Asleep it's slid off the edge but for a
     sliver (in the grid's side gap: not over the thumbnails), and only the sliver takes presses;
     awake (scrolling, the mouse on it, dragged) it slides in. */
  .tab {
    position: absolute;
    top: 0;
    bottom: 0;
    right: 0;
    width: 30px; /* (its own width: the press area narrows while it sleeps) */
    display: grid;
    place-items: center;
    border-radius: 24px 0 0 24px;
    background: var(--surface1);
    color: var(--text);
    box-shadow: -2px 4px 14px rgb(0 0 0 / 0.45);
    transform: translateX(calc(100% - 9px));
    transition:
      transform 0.2s cubic-bezier(0.2, 0.8, 0.2, 1),
      background 0.15s;
  }
  .fast:not(.awake) .thumb {
    width: 14px; /* (the sliver, not the tab's whole width: a press beside it is the grid's) */
  }
  .awake .tab {
    transform: none;
  }
  .dragging .tab {
    background: var(--mauve);
    color: var(--crust);
  }
  .bubble {
    position: absolute;
    right: 44px;
    top: 50%;
    transform: translateY(-50%);
    padding: 7px 14px;
    border-radius: 12px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    color: var(--text);
    font-weight: 600;
    white-space: nowrap;
    box-shadow: 0 10px 28px rgb(0 0 0 / 0.5);
    pointer-events: none;
  }
  .mark {
    position: absolute;
    right: 40px;
    transform: translateY(-50%);
    padding: 1px 7px;
    border-radius: 8px;
    background: rgb(17 17 27 / 0.72);
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    pointer-events: none;
  }
  /* On the left (a left-handed thumb): the same, mirrored. */
  .fast.left {
    right: auto;
    left: calc(-1 * var(--grid-gutter, 0px));
  }
  .left .thumb {
    right: auto;
    left: 0;
  }
  .left .tab {
    right: auto;
    left: 0;
    border-radius: 0 24px 24px 0;
    box-shadow: 2px 4px 14px rgb(0 0 0 / 0.45);
    transform: translateX(calc(-100% + 9px));
  }
  .left.awake .tab {
    transform: none;
  }
  .left .bubble {
    right: auto;
    left: 44px;
  }
  .left .mark {
    right: auto;
    left: 40px;
  }
  @media (prefers-reduced-motion: reduce) {
    .tab {
      transition: none;
    }
  }
</style>
