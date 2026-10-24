<script lang="ts">
  // The notices that pop up at the bottom of the screen (lib/toast): the newest at the bottom, the others above it.
  // Each is placed by the ones below it (measured), not by the page's flow: one going leaves its place at once and
  // the others glide down into it while it fades where it was (in a column they waited for it, and new ones came in
  // high up - the user). A new one rises into place. One of the same kind takes the place of the one showing: its
  // words change, and a count on its icon says how many it stands for.
  import { cubicOut } from 'svelte/easing';
  import { px, settle, sizeOf, reduced } from '../lib/motion';
  import { toasts, type ToastKind } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';

  // Each kind of notice: its icon (the color comes from the stylesheet).
  const icons: Record<ToastKind, 'check' | 'info' | 'trash' | 'alert'> = { done: 'check', info: 'info', removed: 'trash', error: 'alert' };
  const GAP = 8;

  let heights = $state<Record<number, number>>({});
  /** How far up a notice sits: the heights of the newer ones below it, and their gaps. */
  function lift(i: number): number {
    let y = 0;
    for (const x of toasts.list.slice(i + 1)) y += (heights[x.id] ?? 0) + GAP;
    return y;
  }

  function rise(node: Element) {
    if (reduced()) return { duration: 0 };
    return { duration: 300, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateY(${px((1 - k) * 16)}) scale(${settle(0.96 + 0.04 * k, sizeOf(node))})` };
  }
  function fadeOut(node: Element) {
    if (reduced()) return { duration: 0 };
    return { duration: 200, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: scale(${settle(0.96 + 0.04 * k, sizeOf(node))})` };
  }
</script>

<div class="toasts" aria-live="polite">
  {#each toasts.list as x, i (x.id)}
    <div
      class="toast {x.kind}"
      class:has-action={!!x.action}
      role={x.kind === 'error' ? 'alert' : undefined}
      style:translate="0 {px(-lift(i))}"
      bind:offsetHeight={heights[x.id]}
      in:rise
      out:fadeOut>
      <span class="badge" aria-hidden="true">
        <Icon name={icons[x.kind]} size={16} />
        {#if x.count > 1}{#key x.count}<span class="count">{x.count}</span>{/key}{/if}
      </span>
      {#key x.text}<span class="text">{x.text}</span>{/key}
      {#if x.action}
        <button
          class="quiet"
          onclick={() => {
            x.action!.run();
            toasts.dismiss(x.id);
          }}>{x.action.label}</button>
      {/if}
    </div>
  {/each}
</div>

<style>
  /* Just above the bottom of the screen - above the selection bar while it shows (it says its height:
     --bottom-bar) - clear of the iPhone's home bar. */
  .toasts {
    position: fixed;
    left: 50%;
    bottom: calc(16px + max(var(--bottom-room, 0px), var(--bottom-bar, 0px)));
    translate: -50% 0;
    z-index: 50;
    width: min(420px, calc(100% - 24px));
    pointer-events: none;
  }
  .toast {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    transition: translate 0.28s cubic-bezier(0.2, 0.8, 0.2, 1);
    will-change: opacity; /* (its own layer: no text shift as it lands - see app.css) */
    --kind: var(--green);
    /* (taps go through to what's under it - a phone sheet's items, say - unless it has a button: Undo, Reload) */
    pointer-events: none;
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 44px;
    box-sizing: border-box;
    padding: 7px 14px 7px 8px;
    background: var(--surface0);
    border: 1px solid var(--surface1);
    border-radius: 12px;
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.4);
  }
  .toast.has-action {
    pointer-events: auto;
    padding-right: 6px;
  }
  .toast.info {
    --kind: var(--blue);
  }
  .toast.removed {
    --kind: var(--peach);
  }
  .toast.error {
    --kind: var(--red);
    border-color: color-mix(in srgb, var(--red) 55%, var(--surface1));
    color: var(--text); /* the badge says it is an error (not the app-wide .error text color) */
  }
  .badge {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--kind) 18%, transparent);
    color: var(--kind);
  }
  /* How many the notice stands for (the same kind, merged). */
  .count {
    position: absolute;
    top: -5px;
    right: -7px;
    min-width: 17px;
    height: 17px;
    padding: 0 4px;
    box-sizing: border-box;
    border-radius: 999px;
    background: var(--kind);
    color: var(--crust);
    font-size: 11px;
    font-weight: 700;
    line-height: 17px;
    text-align: center;
    animation: count-pop 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  @keyframes count-pop {
    from {
      transform: scale(0.5);
      opacity: 0;
    }
  }
  .text {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
    animation: text-in 0.2s ease-out;
  }
  @keyframes text-in {
    from {
      opacity: 0.2;
    }
  }
  .toast button {
    flex: none;
    color: var(--mauve);
    border-color: transparent;
  }
  @media (prefers-reduced-motion: reduce) {
    .toast {
      transition: none;
    }
    .count,
    .text {
      animation: none;
    }
  }
</style>
