<script lang="ts">
  // The notices that pop up at the bottom of the screen: errors, and those with an action (Undo) - the rest go to
  // the bell (Bell.svelte). A new one rises into place, one going sinks and fades; the others glide.
  import { cubicOut } from 'svelte/easing';
  import { flip } from 'svelte/animate';
  import { toasts, type ToastKind } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';

  // Each kind of notice: its icon (the color comes from the stylesheet).
  const icons: Record<ToastKind, 'check' | 'info' | 'trash' | 'alert'> = { done: 'check', info: 'info', removed: 'trash', error: 'alert' };

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  function rise(node: Element) {
    if (reduced()) return { duration: 0 };
    return { duration: 320, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateY(${(1 - k) * 18}px) scale(${0.96 + 0.04 * k})` };
  }
  function sink(node: Element) {
    if (reduced()) return { duration: 0 };
    return { duration: 200, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: scale(${0.96 + 0.04 * k})` };
  }
</script>

<div class="toasts" aria-live="polite">
  {#each toasts.list as x (x.id)}
    <div class="toast {x.kind}" class:has-action={!!x.action} role={x.kind === 'error' ? 'alert' : undefined} animate:flip={{ duration: 240 }} in:rise out:sink>
      <span class="badge" aria-hidden="true"><Icon name={icons[x.kind]} size={16} /></span>
      <span class="text">{x.text}</span>
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
  .toast.has-action {
    pointer-events: auto;
  }
  .toasts {
    position: fixed;
    left: 50%;
    bottom: calc(72px + var(--safe-bottom) + var(--home-bar, 0px)); /* above the selection bar */
    transform: translateX(-50%);
    z-index: 50;
    display: grid;
    gap: 8px;
    width: min(420px, calc(100% - 24px));
    pointer-events: none;
  }
  .toast {
    --kind: var(--green);
    /* (taps go through to what's under it - a phone sheet's items, say - unless it has a button: Undo, Reload) */
    pointer-events: none;
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 44px;
    padding: 7px 14px 7px 8px;
    background: var(--surface0);
    border: 1px solid var(--surface1);
    border-radius: 12px;
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.4);
  }
  .toast.has-action {
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
    flex: none;
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--kind) 18%, transparent);
    color: var(--kind);
  }
  .text {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .toast button {
    flex: none;
    color: var(--mauve);
    border-color: transparent;
  }
</style>
