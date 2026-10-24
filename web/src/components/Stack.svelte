<script lang="ts" generics="T">
  // A stack of cards (a site's downloads, the notices showing at once): folded, the first card shows whole and the
  // next two peek out behind it, a little smaller; their edges unfold it. Unfolded, the cards fan out into a list
  // - each glides from its place in the stack to its place in the list, its content fading in - and fold back
  // the same way, each under the one before it (the front card stays on top, folded or not: with one level for all
  // unfolded, the later cards slid out over the front one - the user: they should "slide out from underneath").
  // Cards coming and going fade, the others glide to make room. Every card is absolutely placed
  // (transforms only: smooth on a phone); the stack's height follows them.
  import type { Snippet } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { t } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';

  let {
    items,
    key,
    card,
    open = $bindable(false),
    up = false,
    gap = 8,
    peek = 9,
    label,
    shift,
    lifted,
    hover = false,
    framed = true,
  }: {
    items: T[];
    key: (item: T) => string | number;
    card: Snippet<[T, { index: number; front: boolean; open: boolean }]>;
    open?: boolean;
    /** The stack builds upwards (notices at the bottom of the screen: the newest last, in front). */
    up?: boolean;
    gap?: number;
    peek?: number;
    /** What the stack is (its unfold button: "Show all 4 ..."). */
    label: string;
    /** Unfolded: how far a card is moved meanwhile (dragged, or making room for one dragged). */
    shift?: (index: number) => number;
    /** A card being dragged: it follows the pointer (no gliding). */
    lifted?: (index: number) => boolean;
    /** Unfolds while pointed at (notices), folds again after. */
    hover?: boolean;
    /** Each card on a card of its own (a framed background); off: the card snippet frames itself. */
    framed?: boolean;
  } = $props();

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  // A mouse on the peeking edges: the cards behind fan out a little - a hint of the unfold (2026-10-24; the user found
  // the "+N" chip turning the accent and dropping "weird"). The stack keeps its height (nothing below moves).
  const TEASE = 5;
  let teasing = $state(false);
  const many = $derived(items.length > 1);
  const isOpen = $derived(open || !many);
  // The card in front: the first (a list read top-down), or the last when building upwards.
  const order = $derived(up ? [...items].reverse() : items);

  let heights = $state<Record<string, number>>({});
  function measure(k: string) {
    return (node: HTMLElement) => {
      const ro = new ResizeObserver(() => (heights[k] = node.offsetHeight));
      ro.observe(node);
      heights[k] = node.offsetHeight;
      return () => ro.disconnect();
    };
  }
  const h = (item: T | undefined) => (item === undefined ? 0 : (heights[String(key(item))] ?? 0));
  const front = $derived(order[0]);

  // Where each card goes: unfolded, one under another (building upwards: from the bottom up); folded, behind the
  // front card - the next two peeking out by `peek` each, the rest hidden right behind them.
  type Place = { y: number; scale: number; height: number; hidden: boolean; depth: number };
  const places = $derived.by(() => {
    const out: Place[] = [];
    if (isOpen) {
      // (list order: top to bottom; building upwards the front card is the bottom one)
      const list = items;
      let y = 0;
      list.forEach((it, i) => {
        out.push({ y: y + (shift?.(i) ?? 0), scale: 1, height: h(it), hidden: false, depth: 0 });
        y += h(it) + gap;
      });
      return out;
    }
    const fh = h(front);
    const behind = Math.min(2, items.length - 1);
    items.forEach((_, i) => {
      const depth = up ? items.length - 1 - i : i; // (0 = in front)
      const d = Math.min(depth, 2);
      const fan = teasing ? d * TEASE : 0;
      const y = up ? (behind - d) * peek - fan : d * peek + fan;
      out.push({ y, scale: 1 - d * 0.045, height: fh, hidden: depth > 2, depth });
    });
    return out;
  });
  const total = $derived.by(() => {
    if (isOpen) return items.reduce((sum, it) => sum + h(it), 0) + gap * Math.max(0, items.length - 1);
    return h(front) + peek * Math.min(2, items.length - 1);
  });

  // No gliding on the first paint (the cards would fly in from the top).
  let ready = $state(false);
  $effect(() => {
    const id = requestAnimationFrame(() => requestAnimationFrame(() => (ready = true)));
    return () => cancelAnimationFrame(id);
  });

  function fadeIn(node: Element) {
    if (!ready || reduced()) return { duration: 0 };
    return { duration: 260, easing: cubicOut, css: (k: number) => `opacity: ${k}` };
  }
  function fadeOut(node: Element) {
    if (reduced()) return { duration: 0 };
    return { duration: 200, easing: cubicOut, css: (k: number) => `opacity: ${k}` };
  }

  let leaveTimer = 0;
</script>

<!-- svelte-ignore a11y_no_static_element_interactions (pointing at it only unfolds notices: the edges are the button) -->
<div
  class="stack"
  class:ready
  class:open={isOpen}
  class:many
  class:up
  class:teasing
  style:--tease="{TEASE * 2}px"
  style:height="{total}px"
  onpointerenter={(e) => {
    if (!hover || e.pointerType !== 'mouse') return;
    clearTimeout(leaveTimer);
    open = true;
  }}
  onpointerleave={(e) => {
    if (!hover || e.pointerType !== 'mouse') return;
    leaveTimer = window.setTimeout(() => (open = false), 250);
  }}>
  {#each items as item, i (key(item))}
    {@const p = places[i] ?? { y: 0, scale: 1, height: 0, hidden: false, depth: 0 }}
    {@const isFront = !isOpen && p.depth === 0}
    {@const behindFront = !isOpen && p.depth > 0}
    <div
      class="item"
      class:framed
      class:behind={behindFront}
      class:lifted={lifted?.(i)}
      style:transform="translate3d(0, {p.y}px, 0) scale({p.scale})"
      style:height={behindFront ? `${p.height}px` : undefined}
      style:z-index={lifted?.(i) ? 20 : 10 - Math.min(up ? items.length - 1 - i : i, 9)}
      style:opacity={p.hidden ? 0 : 1}
      aria-hidden={behindFront ? 'true' : undefined}
      inert={behindFront}
      in:fadeIn
      out:fadeOut>
      <div class="content" {@attach measure(String(key(item)))}>
        {@render card(item, { index: i, front: isFront || (isOpen && i === 0), open: isOpen })}
      </div>
    </div>
  {/each}
  {#if many && !isOpen}
    <!-- The peeking edges unfold it. -->
    <button
      class="unfold"
      style:height="{peek * Math.min(2, items.length - 1) + 14}px"
      aria-expanded="false"
      aria-label={t('stack.showAll', { n: items.length, what: label })}
      title={t('stack.showAll', { n: items.length, what: label })}
      onpointerenter={(e) => e.pointerType === 'mouse' && !reduced() && (teasing = true)}
      onpointerleave={() => (teasing = false)}
      onclick={() => {
        teasing = false;
        open = true;
      }}>
      <span class="more">+{items.length - 1}</span>
    </button>
  {/if}
</div>
{#if many && open && !hover}
  <div class="fold-row">
    <button class="fold" aria-expanded="true" onclick={() => (open = false)}><Icon name="chevron" size={15} /> {t('stack.showLess')}</button>
  </div>
{/if}

<style>
  .stack {
    position: relative;
  }
  .stack.ready {
    transition: height 0.42s cubic-bezier(0.2, 0.9, 0.25, 1);
  }
  .item {
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    transform-origin: 50% 100%; /* (folded: the cards behind narrow towards their peeking edge) */
    overflow: hidden;
  }
  .stack.up .item {
    transform-origin: 50% 0%;
  }
  .item.framed {
    border-radius: 12px;
    border: 1px solid var(--surface0);
    background: var(--crust);
  }
  .stack.ready .item {
    transition:
      transform 0.42s cubic-bezier(0.2, 0.9, 0.25, 1),
      height 0.42s cubic-bezier(0.2, 0.9, 0.25, 1),
      opacity 0.3s ease,
      box-shadow 0.3s ease,
      border-color 0.3s ease;
  }
  .stack.ready .item.lifted {
    transition: none;
    box-shadow:
      0 0 0 1px var(--mauve),
      0 10px 28px rgb(0 0 0 / 0.45);
  }
  /* Folded: a card behind shows only its edge - its content fades away, it darkens a little the deeper it is. */
  .content {
    transition: opacity 0.28s ease;
  }
  .item.framed.behind .content {
    opacity: 0;
  }
  /* (unframed - a notice is its own card: it keeps its look behind, only its edge shows; its shadow isn't cut off) */
  .item:not(.framed) {
    overflow: visible;
  }
  .item:not(.framed).behind {
    overflow: hidden;
    border-radius: 12px;
    filter: brightness(0.82);
  }
  .item.behind.framed {
    background: color-mix(in srgb, var(--surface0) 55%, var(--crust));
    border-color: var(--surface1);
  }
  .stack.many:not(.open) .item:not(.behind) {
    box-shadow: 0 6px 18px rgb(0 0 0 / 0.35);
  }
  .unfold {
    position: absolute;
    left: 6%;
    right: 6%;
    bottom: -4px;
    z-index: 11;
    min-height: 0;
    padding: 0 10px 2px 0;
    display: flex;
    align-items: flex-end;
    justify-content: flex-end;
    background: none;
    border: 0;
    border-radius: 0 0 12px 12px;
    color: var(--subtext);
    cursor: pointer;
  }
  .stack.up .unfold {
    bottom: auto;
    top: -4px;
    align-items: flex-start;
    border-radius: 12px 12px 0 0;
  }
  .more {
    font-size: 11px;
    font-weight: 700;
    line-height: 1;
    padding: 2px 6px;
    border-radius: 999px;
    background: var(--surface1);
    color: var(--text);
    transform: translateY(2px);
    transition:
      background 0.15s,
      transform 0.42s cubic-bezier(0.2, 0.9, 0.25, 1);
  }
  /* (fanned out: the chip rides on the last edge) */
  .stack.teasing .more {
    transform: translateY(calc(2px + var(--tease)));
  }
  .stack.up.teasing .more {
    transform: translateY(calc(2px - var(--tease)));
  }
  @media (hover: hover) {
    .stack .unfold:hover:not(:disabled) {
      background-image: none; /* (not the filled buttons' light layer: a pale box over the edges) */
    }
    .unfold:hover .more {
      background: var(--surface2);
    }
  }
  .fold-row {
    display: flex;
    justify-content: center;
    margin-top: 6px;
  }
  .fold {
    min-height: 0;
    height: 28px;
    padding: 0 12px;
    gap: 4px;
    font-size: var(--font-small);
    background: none;
    border: 0;
    color: var(--subtext);
  }
  .fold :global(svg) {
    rotate: 180deg;
  }
  @media (prefers-reduced-motion: reduce) {
    .stack.ready,
    .stack.ready .item,
    .content {
      transition: none;
    }
  }
</style>
