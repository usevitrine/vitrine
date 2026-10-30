<script lang="ts">
  import { px, onPixels } from '../lib/motion';
  import { away } from '../lib/sheet';
  // The details panel's frame outside the viewer (the Comics page): the same
  // as the viewer's - a column on the right on wide screens (below the top
  // bar; the page makes room for it), a sheet over the bottom three quarters
  // on phones (dragged down from anywhere to close). A header with the title, the
  // page's buttons and close; the body scrolls.
  import type { Snippet } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { backClose } from '../lib/backClose';
  import { dragDown } from '../lib/sheetDrag';
  import { overlayScroll } from '../lib/overlayScroll';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n.svelte';

  // over: above the comic reader (it covers the whole screen, the top bar too).
  let {
    title,
    onclose,
    actions,
    children,
    over = false,
    escapeOff = false,
  }: {
    title: string;
    onclose: () => void;
    actions?: Snippet;
    children: Snippet;
    over?: boolean;
    escapeOff?: boolean; // (a dialog of its content is open: Escape closes that first)
  } = $props();

  // Once closing it's done (it still slides away a moment): its keys and buttons do nothing more.
  let closing = false;
  function close() {
    if (closing) return;
    closing = true;
    onclose();
  }

  const isSheet = () => !matchMedia('(min-width: 900px)').matches;
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  let gone = 0; // dragged down this far when let go (it slides on from there)
  function slide(node: Element, { out = false } = {}) {
    if (out) closing = true; // (closed from outside)
    if (reduced()) return { duration: 0 };
    if (!out) onPixels(node);
    if (isSheet()) {
      if (out && gone > 0) {
        const from = gone;
        gone = 0;
        return { duration: 200, easing: cubicOut, css: (k: number) => `transform: translateY(${px(from * k + (1 - k) * away(node))})` };
      }
      return { duration: 240, easing: cubicOut, css: (k: number) => `transform: translateY(${px((1 - k) * away(node))})` };
    }
    return { duration: 240, easing: cubicOut, css: (k: number) => `transform: translateX(${px((1 - k) * (node as HTMLElement).offsetWidth)})` };
  }

  // Phones: dragged down from anywhere on it closes it (lib/sheetDrag: a list scrolled down scrolls first).
  let dragging = $state(false);
  let dy = $state(0);
  const drag = dragDown({
    active: isSheet,
    onstart: () => (dragging = true),
    onmove: (d) => (dy = d),
    onend: (d, fast) => {
      dragging = false;
      if (d > 90 || (fast && d > 20)) {
        gone = d;
        close();
        return;
      }
      dy = 0;
    },
  });
</script>

<!-- Escape closes it - not while a dialog of its content is open (escapeOff), nor a question: those close first. -->
<svelte:window
  onkeydown={(e) => {
    if (e.key !== 'Escape' || e.defaultPrevented || closing || escapeOff || document.querySelector('[role=alertdialog]')) return;
    e.preventDefault(); // (handled: the reader under it stays)
    close();
  }} />

<aside
  class="panel"
  class:over
  data-details-panel
  class:dragging
  aria-label={title}
  {@attach backClose(() => close())}
  {@attach drag}
  style:transform={dy ? `translateY(${dy}px)` : undefined}
  in:slide
  out:slide={{ out: true }}>
  <header class="panel-head">
    <span class="grab" aria-hidden="true"></span>
    <h2>{title}</h2>
    {@render actions?.()}
    <button class="icon" aria-label={t('viewer.closeDetails')} title={t('viewer.closeKey')} onclick={close}><Icon name="close" /></button>
  </header>
  <div class="panel-body" {@attach overlayScroll}>
    {@render children()}
  </div>
</aside>

<style>
  /* As the viewer's details: a column on the right (wide screens), a sheet (phones). */
  .panel {
    position: fixed;
    z-index: 30;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--mantle);
    top: var(--bar-h, 0px);
    right: 0;
    bottom: 0;
    width: 340px;
    border-left: 1px solid var(--surface0);
    box-shadow: -12px 0 32px rgb(0 0 0 / 0.35);
  }
  .panel.over {
    top: 0;
    z-index: 60;
  }
  .panel-head {
    flex: none;
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 8px 8px 16px;
    border-bottom: 1px solid var(--surface0);
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
      top: auto;
      left: 0;
      width: auto;
      height: 75%;
      max-height: var(--sheet-max-h);
      border-left: none;
      border-radius: 20px 20px 0 0;
      box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
      z-index: 50;
      transition: transform 0.22s ease-out;
    }
    .panel.over {
      top: auto;
      z-index: 60;
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
