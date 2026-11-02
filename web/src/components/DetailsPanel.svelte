<script lang="ts">
  import { px, onPixels, reduced } from '../lib/motion';
  // The details panel's frame outside the viewer (the Comics page): the same
  // as the viewer's - a column on the right on wide screens (below the top
  // bar; the page makes room for it), a sheet over the bottom three quarters
  // on phones (dragged down from anywhere to close). A header with the title, the
  // page's buttons and close; the body scrolls. (Its look: .details-panel in app.css.)
  import type { Snippet } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { backClose } from '../lib/backClose';
  import { wide } from '../lib/media';
  import { DetailsSheet } from '../lib/detailsSheet.svelte';
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

  // A press outside closes it and does nothing else, as the viewer's details (the user: it opened the comic under the
  // cursor). Not a press in what it opened itself or above it: a menu, a dialog, a notice with its Undo.
  function pressedOff(e: PointerEvent) {
    const on = e.target as Element | null;
    if (closing || e.button !== 0 || !on || on.closest('[data-details-panel], [role=menu], [role=dialog], [role=alertdialog], [role=listbox], [role=status], .toast, .toasts')) return;
    if (document.querySelector('[role=menu], [role=alertdialog], [role=dialog][aria-modal="true"]')) return;
    e.stopPropagation();
    window.addEventListener('click', swallow, { capture: true, once: true });
    window.setTimeout(() => window.removeEventListener('click', swallow, { capture: true }), 600);
    close();
  }
  const swallow = (c: Event) => c.stopPropagation();

  const isSheet = () => !wide();
  const ds = new DetailsSheet(isSheet, close);
  function slide(node: Element, { out = false } = {}) {
    if (out) closing = true; // (closed from outside)
    if (reduced()) return { duration: 0 };
    if (isSheet()) return ds.slide(node, out);
    onPixels(node);
    return { duration: 240, easing: cubicOut, css: (k: number) => `transform: translateX(${px((1 - k) * (node as HTMLElement).offsetWidth)})` };
  }
</script>

<!-- Escape closes it - not while a dialog of its content is open (escapeOff), nor a question: those close first. -->
<svelte:window
  onpointerdowncapture={pressedOff}
  onkeydown={(e) => {
    if (e.key !== 'Escape' || e.defaultPrevented || closing || escapeOff || document.querySelector('[role=alertdialog]')) return;
    e.preventDefault(); // (handled: the reader under it stays)
    close();
  }} />

<aside
  class="panel details-panel"
  class:over
  data-details-panel
  class:dragging={ds.dragging}
  aria-label={title}
  {@attach backClose(() => close())}
  {@attach ds.drag}
  style:transform={ds.dy ? `translateY(${ds.dy}px)` : undefined}
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
  .panel {
    position: fixed;
    z-index: 30;
    top: var(--bar-h, 0px);
    right: 0;
    bottom: 0;
    width: 340px;
  }
  .panel.over {
    top: 0;
    z-index: 60;
  }
  @media (min-width: 900px) {
    .panel {
      box-shadow: -12px 0 32px rgb(0 0 0 / 0.35);
    }
  }
  @media (max-width: 899px) {
    .panel {
      top: auto;
      left: 0;
      bottom: var(--kb, 0px); /* above the on-screen keyboard */
      width: auto;
      max-height: var(--sheet-max-h);
      z-index: 50;
    }
    .panel.over {
      top: auto;
      z-index: 60;
    }
  }
</style>
