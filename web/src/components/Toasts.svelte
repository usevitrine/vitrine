<script lang="ts">
  // The notices that pop up at the bottom of the screen (lib/toast). Notices of the same kind form a stack like the
  // Downloads page's (the user: "the same stack style as on the downloads page" - Stack, building upwards): the newest
  // in front, the next two peeking out behind it with a "+N"; pointing at it (or tapping its edges) fans it out, each
  // with its own button. The stacks sit one above another, the newest at the bottom; each is placed by the ones below
  // it (measured), not by the page's flow - one going leaves its place at once and the others glide down while it
  // fades where it was (in a column they waited for it, and new ones came in high up - the user).
  import { cubicOut } from 'svelte/easing';
  import { px, settle, sizeOf, reduced } from '../lib/motion';
  import { toasts, type Toast, type ToastKind } from '../lib/toast.svelte';
  import Icon from './Icon.svelte';
  import Stack from './Stack.svelte';

  // Each kind of notice: its icon (the color comes from the stylesheet).
  const icons: Record<ToastKind, 'check' | 'info' | 'trash' | 'alert'> = { done: 'check', info: 'info', removed: 'trash', error: 'alert' };
  const GAP = 8;

  // The stacks: notices of one kind together (oldest first), the stack with the newest notice last (at the bottom).
  const stacks = $derived.by(() => {
    const by = new Map<string, Toast[]>();
    for (const x of toasts.list) by.set(x.key, [...(by.get(x.key) ?? []), x]);
    return [...by.entries()].map(([key, items]) => ({ key, items })).sort((a, b) => a.items[a.items.length - 1].id - b.items[b.items.length - 1].id);
  });
  let open = $state<Record<string, boolean>>({});

  let heights = $state<Record<string, number>>({});
  /** How far up a stack sits: the heights of the newer stacks below it, and their gaps. */
  function lift(i: number): number {
    let y = 0;
    for (const s of stacks.slice(i + 1)) y += (heights[s.key] ?? 0) + GAP;
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

{#snippet cardOf(x: Toast)}
  <div class="toast {x.kind}" class:has-action={!!x.action} role={x.kind === 'error' ? 'alert' : undefined}>
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
{/snippet}

<div class="toasts" aria-live="polite">
  {#each stacks as s, i (s.key)}
    <div class="group" style:translate="0 {px(-lift(i))}" bind:offsetHeight={heights[s.key]} in:rise out:fadeOut>
      <Stack items={s.items} key={(x) => x.id} up hover framed={false} gap={GAP} label={s.items[s.items.length - 1].text} bind:open={() => !!open[s.key], (v) => (open[s.key] = v)}>
        {#snippet card(x, _)}{@render cardOf(x)}{/snippet}
      </Stack>
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
  .group {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    transition: translate 0.28s cubic-bezier(0.2, 0.8, 0.2, 1);
    will-change: opacity; /* (its own layer: no text shift as it lands - see app.css) */
  }
  /* (a stack's edges and "+N" can be tapped to fan it out; its cards let taps through unless they have a button) */
  .group :global(.unfold) {
    pointer-events: auto;
  }
  .toast {
    --kind: var(--green);
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
  @media (prefers-reduced-motion: reduce) {
    .group {
      transition: none;
    }
  }
</style>
