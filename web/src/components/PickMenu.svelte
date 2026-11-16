<script lang="ts">
  // Several things picked (tags in the details, subscriptions on the Downloads page): a round button with their count
  // floats at the bottom right; it opens them as chips (each can be taken off; one line a finger scrolls sideways) and
  // what to do with them in cards, each row saying what it does - a sheet along the bottom on phones, a card above the
  // button with a mouse. Clear ends picking. The rows are the caller's (`actions`, given `use`: closes, then runs).
  // (2026-11-16: made for the tags - the user picked it from mockups - then shared with the subscriptions, the user:
  // "change the multi select in subs to look feel and act like the tag multi select in the details panel".)
  import { tick, type Snippet } from 'svelte';
  import GrabBar from './GrabBar.svelte';
  import Icon from './Icon.svelte';
  import { place } from '../lib/place';
  import { swallowPress } from '../lib/outside';
  import { menuOpened } from '../lib/menuOpen.svelte';
  import { backClose } from '../lib/backClose';
  import { sheetDrag } from '../lib/sheetDrag';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { phone } from '../lib/media';
  import { pop, nudge } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';

  let {
    title,
    fabLabel,
    chips,
    chipsLabel,
    unpickLabel,
    onunpick,
    onclear,
    actions,
    within,
  }: {
    title: string; // "3 tags picked"
    fabLabel: string; // the round button's name
    chips: { key: string | number; label: string }[];
    chipsLabel: string; // the chips' list's name
    unpickLabel: (label: string) => string;
    onunpick: (key: string | number) => void;
    onclear: () => void;
    actions: Snippet<[(run: () => void) => void]>;
    within?: () => Element | null | undefined; // with a mouse, the card stays inside it (the details panel)
  } = $props();

  let open = $state(false);
  $effect(() => {
    if (open) return menuOpened();
  });
  let sheet = $state(false);
  let fab = $state<HTMLButtonElement>();
  let card = $state<HTMLDivElement>();
  let pos = $state<{ left: number; bottom: number; ready: boolean }>({ left: 0, bottom: 0, ready: false });

  function show() {
    sheet = phone();
    pos = { left: 0, bottom: 0, ready: false };
    open = true;
  }
  function close(refocus = false) {
    open = false;
    if (refocus) fab?.focus({ preventScroll: true });
  }
  /** An action's row: the card closes, then it runs. */
  function use(run: () => void) {
    close();
    run();
  }
  // With a mouse: above the button, right edges lined up - inside `within` (the details panel) when given: no wider
  // than it, its left edge not past it (the user: "On desktop the multi select menu on the details panel hangs over the
  // edge of the details panel").
  let width = $state<number | undefined>(undefined);
  $effect(() => {
    if (!open || sheet || !card || !fab) return;
    const box = within?.()?.getBoundingClientRect();
    if (box) card.style.width = `${Math.min(340, box.width - 16)}px`;
    const r = card.getBoundingClientRect();
    const p = place(r, fab.getBoundingClientRect(), { align: 'end', gap: 10 });
    const left = box ? Math.max(box.left + 8, Math.min(p.left, box.right - 8 - r.width)) : p.left;
    width = box ? r.width : undefined;
    pos = { left, bottom: p.above ? p.bottom : innerHeight - p.top - card.offsetHeight, ready: true };
    const c = card;
    tick().then(() => c.querySelector<HTMLButtonElement>('.act')?.focus({ preventScroll: true }));
  });
  // The chips: one line a finger (or a wheel) scrolls sideways when they don't fit, as the search bar's (the user:
  // "The tag chips here should be a scrollable carousel" - "I wanna be able to scroll with my finger"); soft edges
  // only where more are out of view (the app's rule), checked again when one goes.
  let chipsEl = $state<HTMLElement>();
  let fadeL = $state(false);
  let fadeR = $state(false);
  function edges() {
    if (!chipsEl) return;
    fadeL = chipsEl.scrollLeft > 2;
    fadeR = chipsEl.scrollLeft + chipsEl.clientWidth < chipsEl.scrollWidth - 2;
  }
  $effect(() => {
    void chips.length;
    requestAnimationFrame(edges);
  });
  const watchChips = (el: HTMLElement) => {
    const ro = new ResizeObserver(edges);
    ro.observe(el);
    return () => ro.disconnect();
  };

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close(true);
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    const els = [...card!.querySelectorAll<HTMLButtonElement>('.act:not(:disabled)')];
    const i = els.indexOf(document.activeElement as HTMLButtonElement);
    els[(i + (e.key === 'ArrowDown' ? 1 : els.length - 1)) % els.length]?.focus();
  }
  function toBody(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }
</script>

<div use:toBody>
  <button
    bind:this={fab}
    class="fab"
    class:open
    aria-label={fabLabel}
    title={fabLabel}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={() => (open ? close() : show())}
    transition:pop
    ><Icon name="checks" size={24} />{#key chips.length}<span class="badge" in:nudge>{chips.length}</span>{/key}</button>

  {#if open}
    <div
      class="backdrop"
      class:dim={sheet}
      role="presentation"
      in:shade
      out:shade={{ out: true }}
      onpointerdown={(e) => {
        swallowPress(e);
        close();
      }}>
    </div>
    <div
      class="card"
      class:sheet
      bind:this={card}
      role="dialog"
      aria-label={title}
      tabindex="-1"
      data-sheet={sheet ? '' : undefined}
      style:left={sheet ? undefined : `${pos.left}px`}
      style:width={sheet || width === undefined ? undefined : `${width}px`}
      style:bottom={sheet ? undefined : `${pos.bottom}px`}
      style:visibility={sheet || pos.ready ? undefined : 'hidden'}
      {@attach sheet && sheetDrag(() => close())}
      {@attach backClose(() => close())}
      in:sheetIn={{ on: sheet, desk: 'drop' }}
      out:sheetOut={{ on: sheet, desk: 'drop' }}
      onkeydown={keydown}>
      {#if sheet}<GrabBar stuck bleed={14} />{/if}
      <div class="top">
        <span class="title">{title}</span>
        <button class="clear" onclick={() => (close(), onclear())}>{t('details.clearPicked')}</button>
      </div>
      <div class="chips-box" class:fade-l={fadeL} class:fade-r={fadeR}>
      <ul class="chips" aria-label={chipsLabel} bind:this={chipsEl} onscroll={edges} {@attach watchChips}>
        {#each chips as c (c.key)}
          <li transition:pop>
            <span>{c.label}</span>
            <button class="unpick" aria-label={unpickLabel(c.label)} title={unpickLabel(c.label)} onclick={() => onunpick(c.key)}><Icon name="close" size={14} /></button>
          </li>
        {/each}
      </ul>
      </div>

      {@render actions(use)}
    </div>
  {/if}
</div>

<style>
  .fab,
  .fab:hover:not(:disabled) {
    position: fixed;
    z-index: 55;
    right: calc(24px + var(--safe-right, 0px)); /* (its count clear of the page's scrollbar at the edge) */
    bottom: calc(18px + var(--bottom-room, 0px) + var(--kb, 0px));
    width: 58px;
    height: 58px;
    min-height: 0;
    padding: 0;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 50%;
    background: var(--mauve);
    color: var(--crust);
    box-shadow: 0 10px 26px rgb(0 0 0 / 0.5);
    transition:
      transform 0.18s cubic-bezier(0.2, 0.8, 0.2, 1),
      box-shadow 0.18s;
  }
  .fab:active {
    transform: scale(0.94);
  }
  .fab.open {
    transform: scale(0.92);
    box-shadow: 0 4px 12px rgb(0 0 0 / 0.4);
  }
  .badge {
    position: absolute;
    top: -4px;
    right: -4px;
    min-width: 24px;
    height: 24px;
    padding: 0 6px;
    display: grid;
    place-items: center;
    border-radius: 999px;
    border: 3px solid var(--base);
    background: var(--blue);
    color: var(--crust);
    font-size: 12px;
    font-weight: 800;
    font-variant-numeric: tabular-nums;
  }
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
  }
  .backdrop.dim {
    background: rgb(17 17 27 / 0.5);
  }
  .card {
    position: fixed;
    z-index: 61;
    width: min(340px, calc(100vw - 16px));
    max-height: calc(var(--view-h, 100dvh) - 100px);
    overflow-y: auto;
    overscroll-behavior: contain;
    display: grid;
    align-content: start;
    gap: 6px;
    padding: 12px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 18px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.55);
    outline: none;
  }
  .card.sheet {
    left: 0;
    right: 0;
    bottom: var(--kb, 0px);
    width: auto;
    max-height: var(--sheet-max-h);
    padding: 0 16px calc(24px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-height: 36px;
  }
  .title {
    font-size: 16px;
    font-weight: 700;
  }
  .clear,
  .clear:hover:not(:disabled) {
    min-height: 32px;
    height: 32px;
    padding: 0 12px;
    border: 0;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--subtext);
    font-weight: 600;
  }
  /* Soft edges where more chips are out of view: overlays in the card's color, eased in and out. */
  .chips-box {
    position: relative;
    margin: 2px -16px 4px; /* (to the card's edges: chips scroll out under its padding, not cut short of it) */
  }
  .card:not(.sheet) .chips-box {
    margin-inline: -12px;
  }
  .chips-box::before,
  .chips-box::after {
    content: '';
    position: absolute;
    z-index: 1;
    top: 0;
    bottom: 0;
    width: 28px;
    pointer-events: none;
    opacity: 0;
    transition: opacity 0.2s;
  }
  .chips-box::before {
    left: 0;
    background: linear-gradient(to right, var(--mantle), transparent);
  }
  .chips-box::after {
    right: 0;
    background: linear-gradient(to left, var(--mantle), transparent);
  }
  .chips-box.fade-l::before,
  .chips-box.fade-r::after {
    opacity: 1;
  }
  .chips {
    display: flex;
    gap: 6px;
    margin: 0;
    padding: 0 16px;
    list-style: none;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: none;
    scroll-padding-inline: 16px;
    touch-action: pan-x; /* (a sideways swipe is the row's; the sheet's drag-to-close stays vertical) */
  }
  .card:not(.sheet) .chips {
    padding-inline: 12px;
  }
  .chips::-webkit-scrollbar {
    display: none;
  }
  .chips li {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 0 4px 0 12px;
    min-height: 32px;
    border-radius: 999px;
    background: var(--blue);
    color: var(--crust);
    font-weight: 500;
  }
  .chips li span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .unpick,
  .unpick:hover:not(:disabled) {
    flex: none;
    width: 26px;
    height: 26px;
    min-height: 0;
    padding: 0;
    display: grid;
    place-items: center;
    border: 0;
    border-radius: 50%;
    background: transparent;
    color: inherit;
  }
  @media (hover: hover) {
    .unpick:hover:not(:disabled) {
      background: rgb(17 17 27 / 0.15);
    }
  }
  .card :global(h4) {
    margin: 8px 2px 2px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--overlay1);
  }
  .card :global(.group) {
    display: grid;
    border-radius: 14px;
    background: var(--crust);
    overflow: hidden;
  }
  .card :global(.act),
  .card :global(.act:hover:not(:disabled)) {
    display: flex;
    align-items: center;
    gap: 14px;
    justify-content: flex-start;
    width: 100%;
    min-height: 52px;
    height: auto;
    padding: 9px 14px;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--text);
    text-align: left;
    font-weight: 500;
  }
  /* (a line drawn inside, not a border: the hover look takes the border off, and a phone keeps the row that opened
     under the finger "hovered" - its line was missing) */
  .card :global(.act:not(:first-child)) {
    box-shadow: inset 0 1px 0 var(--surface0);
  }
  @media (hover: hover) {
    .card :global(.act:hover:not(:disabled)) {
      background: var(--surface0);
    }
  }
  .card :global(.act:focus-visible) {
    background: var(--surface0);
    outline: none;
  }
  .card :global(.act) > :global(svg) {
    flex: none;
    color: var(--mauve);
  }
  .card :global(.act.danger),
  .card :global(.act.danger) > :global(svg) {
    color: var(--red);
  }
  .card :global(.act:disabled) {
    opacity: 0.45;
  }
  .card :global(.txt) {
    display: grid;
    min-width: 0;
  }
  .card :global(.lbl) {
    overflow-wrap: anywhere;
  }
  .card :global(.sub) {
    color: var(--overlay1);
    font-size: 12px;
    font-weight: 400;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
