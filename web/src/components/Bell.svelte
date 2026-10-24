<script lang="ts">
  import { untrack } from 'svelte';
  import { px, settle, sizeOf } from '../lib/motion';
  // The bell in the top bar: it rings when a notification comes, its counter says how many are unread (red with
  // an error among them). A press opens them - the newest first, the unread lit - with Clear; opening reads them.
  import { cubicOut } from 'svelte/easing';
  import { flip } from 'svelte/animate';
  import { backClose } from '../lib/backClose';
  import { swallowPress, leaving } from '../lib/outside';
  import { notices, type Notice } from '../lib/notices.svelte';
  import { router } from '../lib/router.svelte';
  import { t, num } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';
  import Stack from './Stack.svelte';

  const icons = { done: 'check', info: 'info', removed: 'trash', error: 'alert' } as const;

  let open = $state(false);
  let button = $state<HTMLButtonElement>();
  let panel = $state<HTMLElement>();
  let lit = $state<Set<string>>(new Set()); // unread when opened (they stay lit while it's open)

  function show() {
    lit = new Set(notices.list.filter((n) => !n.read).map((n) => n.key));
    open = true;
    notices.markRead();
  }
  function close() {
    open = false;
  }
  function go(n: Notice) {
    close();
    if (n.to) router.go(n.to, n.sub);
  }

  // Screen readers hear what was done that doesn't show (lib/toast), each for a few seconds.
  let said = $state<{ n: number; text: string }[]>([]);
  let saidN = 0;

  let seenSaid = notices.said;
  $effect(() => {
    if (notices.said === seenSaid) return;
    seenSaid = notices.said;
    const one = { n: ++saidN, text: notices.say };
    said = [...said.slice(-2), one];
    setTimeout(() => (said = said.filter((x) => x !== one)), 5000);
  });

  // Rings (a swing) each time one comes; the counter pops.
  let ringing = $state(false);
  let seenRings = notices.rings;
  $effect(() => {
    if (notices.rings === seenRings) return;
    seenRings = notices.rings;
    if (open) return notices.markRead(); // (open: read at once)
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    ringing = false;
    requestAnimationFrame(() => (ringing = true));
  });

  // A press outside closes it (the press goes no further: nothing opens by accident underneath).
  $effect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      const el = e.target as Node;
      if (panel?.contains(el) || button?.contains(el) || leaving(panel)) return;
      swallowPress(e);
      close();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) {
        e.preventDefault();
        close();
        button?.focus();
      }
    };
    window.addEventListener('pointerdown', down, true);
    window.addEventListener('keydown', key);
    return () => {
      window.removeEventListener('pointerdown', down, true);
      window.removeEventListener('keydown', key);
    };
  });

  /** "Just now", "5 min ago", "3 h ago", "2 days ago". */
  function ago(ms: number): string {
    const s = Math.max(0, (Date.now() - ms) / 1000);
    if (s < 60) return t('bell.justNow');
    if (s < 3600) return t('downloads.ago.min', { n: Math.round(s / 60) });
    if (s < 86400) return t('downloads.ago.h', { n: Math.round(s / 3600) });
    return t('downloads.ago.days', { n: Math.round(s / 86400) });
  }

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  // The panel grows out of the bell; its notifications slide in one after another.
  function grow(node: Element, { out = false } = {}) {
    if (reduced()) return { duration: 0 };
    return {
      duration: out ? 200 : 240,
      easing: cubicOut,
      css: (k: number) => `opacity: ${k}; transform: translateY(${(1 - k) * -8}px) scale(${settle(0.94 + 0.06 * k, sizeOf(node))})`,
    };
  }
  function slideIn(node: Element, { i = 0 } = {}) {
    if (reduced()) return { duration: 0 };
    return { delay: 60 + Math.min(i, 8) * 28, duration: 260, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateX(${px((1 - k) * 14)})` };
  }
  function slideOut(node: Element) {
    if (reduced()) return { duration: 0 };
    return { duration: 180, easing: cubicOut, css: (k: number) => `opacity: ${k}; transform: translateX(${px((1 - k) * 24)})` };
  }

  // Notices of the same kind (their kind of notice and tone) as one stack, like the pop-ups and the Downloads page
  // (the user: "they should be stacked in the notification bell too"): at the place of its newest, the newest in front,
  // the next two peeking out with "+N"; tapping its edges fans it out. One of a kind stays a plain row.
  const groups = $derived.by(() => {
    const by = new Map<string, Notice[]>();
    for (const n of notices.list) {
      const k = `${n.cat}|${n.kind}`;
      by.set(k, [...(by.get(k) ?? []), n]);
    }
    return [...by.entries()].map(([key, items]) => ({ key, items }));
  });
  let unfolded = $state<Record<string, boolean>>({});
  // Opened again: its stacks folded (the user: a stack left open stayed open the next time).
  $effect.pre(() => {
    if (open) untrack(() => (unfolded = {}));
  });
</script>

<div class="bell-wrap">
  <div class="sr-only" role="status" aria-live="polite">
    {#each said as x (x.n)}<p>{x.text}</p>{/each}
  </div>
  <button
    bind:this={button}
    class="icon bell"
    class:ringing
    class:has={notices.unread > 0}
    aria-label={notices.unread ? t('bell.labelUnread', { n: notices.unread }) : t('bell.label')}
    title={t('bell.label')}
    aria-haspopup="dialog"
    aria-expanded={open}
    onanimationend={() => (ringing = false)}
    onclick={() => (open ? close() : show())}>
    <span class="swing"><Icon name="bell" size={19} /></span>
    {#if notices.unread}
      {#key notices.unread}
        <span class="unread" class:alarm={notices.alarm} aria-hidden="true">{notices.unread > 99 ? '99+' : num(notices.unread)}</span>
      {/key}
    {/if}
  </button>

  {#if open}
    <div class="panel" bind:this={panel} role="dialog" aria-label={t('bell.label')} {@attach backClose(close)} in:grow out:grow={{ out: true }}>
      <header>
        <h2>{t('bell.label')}</h2>
        {#if notices.list.length}
          <button class="icon clear" aria-label={t('bell.clear')} title={t('bell.clear')} onclick={() => (notices.clear(), close())}><Icon name="broom" size={19} /></button>
        {/if}
      </header>
      {#if notices.list.length}
        <ul>
          {#each groups as g, i (g.key)}
            <li animate:flip={{ duration: 220 }} in:slideIn={{ i }} out:slideOut>
              {#if g.items.length === 1}
                <div class="solo">{@render row(g.items[0])}</div>
              {:else}
                <Stack items={g.items} key={(n) => n.key} label={g.items[0].text} bind:open={() => !!unfolded[g.key], (v) => (unfolded[g.key] = v)}>
                  {#snippet card(n, _)}{@render row(n)}{/snippet}
                </Stack>
              {/if}
            </li>
          {/each}
        </ul>
      {:else}
        <!-- (nothing: a quiet check, no words) -->
        <div class="empty" role="img" aria-label={t('bell.empty')} in:slideIn><Icon name="check" size={22} /></div>
      {/if}
    </div>
  {/if}
</div>

{#snippet row(n: Notice)}
  <div class="row {n.kind}" class:lit={lit.has(n.key)}>
    {#if n.to}
      <button class="item" onclick={() => go(n)}>{@render body(n)}</button>
    {:else}
      <div class="item">{@render body(n)}</div>
    {/if}
  </div>
{/snippet}

{#snippet body(n: Notice)}
  <span class="badge" aria-hidden="true"><Icon name={icons[n.kind]} size={15} /></span>
  <span class="text">{n.text}</span>
  <span class="when">{ago(n.at)}</span>
{/snippet}

<style>
  .bell-wrap {
    position: relative;
  }
  .bell {
    position: relative;
    color: var(--subtext);
  }
  .bell.has {
    color: var(--text);
  }
  .swing {
    display: grid;
    transform-origin: 50% 8%;
  }
  /* A ring: the bell swings, smaller each time. */
  .bell.ringing .swing {
    animation: ring 0.9s cubic-bezier(0.36, 0.07, 0.19, 0.97);
  }
  @keyframes ring {
    10% {
      transform: rotate(18deg);
    }
    25% {
      transform: rotate(-15deg);
    }
    40% {
      transform: rotate(11deg);
    }
    55% {
      transform: rotate(-7deg);
    }
    70% {
      transform: rotate(4deg);
    }
    85% {
      transform: rotate(-2deg);
    }
  }
  /* (on the icon's top right corner; a longer number grows to the right) */
  .unread {
    position: absolute;
    top: calc(50% - 15px);
    left: calc(50% + 2px);
    min-width: 14px;
    height: 14px;
    padding: 0 3px;
    border-radius: 999px;
    background: var(--mauve);
    color: var(--crust);
    font-size: 9px;
    font-weight: 800;
    line-height: 14px;
    text-align: center;
    box-shadow: 0 0 0 2px var(--mantle);
    animation: pop 0.38s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .unread.alarm {
    background: var(--red);
  }
  @keyframes pop {
    from {
      transform: scale(0.4);
    }
  }
  .panel {
    position: absolute;
    top: calc(100% + 8px);
    right: -4px;
    z-index: 60;
    width: min(380px, calc(100vw - 16px));
    max-height: min(70vh, 560px);
    display: flex;
    flex-direction: column;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 18px 44px rgb(0 0 0 / 0.55);
    transform-origin: calc(100% - 22px) 0;
    overflow: hidden;
  }
  /* Phones: across the top bar, the same margin on both sides (the bar holds it, not the button). */
  @media (max-width: 639px) {
    .bell-wrap {
      position: static;
    }
    .panel {
      left: 8px;
      right: 8px;
      width: auto;
    }
  }
  /* A modest heading: the title, the broom beside it. */
  header {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 44px;
    padding: 4px 6px 4px 14px;
    border-bottom: 1px solid var(--surface0);
  }
  h2 {
    flex: 1;
    margin: 0;
    font-size: 14px;
    font-weight: 700;
  }
  .clear {
    width: 36px;
    height: 36px;
    min-height: 0;
    padding: 0;
    color: var(--subtext);
  }
  @media (hover: hover) {
    .clear:hover {
      color: var(--mauve);
    }
    /* (a little sweep when pointed at) */
    .clear:hover :global(svg) {
      animation: sweep 0.5s ease-in-out;
    }
  }
  @keyframes sweep {
    30% {
      transform: rotate(-14deg) translateX(-1px);
    }
    65% {
      transform: rotate(10deg) translateX(1px);
    }
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 4px;
    overflow-y: auto;
    overscroll-behavior: contain;
    display: grid;
    gap: 2px;
  }
  .row {
    --kind: var(--green);
    border-radius: 10px;
  }
  .row.info {
    --kind: var(--blue);
  }
  .row.removed {
    --kind: var(--peach);
  }
  .row.error {
    --kind: var(--red);
  }
  .row.lit {
    background: color-mix(in srgb, var(--mauve) 9%, transparent);
  }
  /* (a stack's cards: a little room around each) */
  /* One on its own: a card like a stack's (Stack's framed cards - the user) */
  .solo {
    margin: 2px 0;
    border: 1px solid var(--surface0);
    border-radius: 12px;
    background: var(--crust);
    overflow: hidden;
  }
  li :global(.stack) {
    margin: 2px 0;
  }
  /* (folded: room under the peeking edges and their "+N", clear of the next row) */
  li :global(.stack.many:not(.open)) {
    margin-bottom: 10px;
  }
  .item {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: start;
    gap: 10px;
    width: 100%;
    min-height: 0;
    height: auto;
    padding: 8px 10px 8px 8px;
    background: none;
    border: 0;
    border-radius: 10px;
    color: var(--text);
    font: inherit;
    text-align: left;
    font-weight: 400;
    white-space: normal; /* (a button: its text wraps like the others, clear of the time) */
  }
  button.item:hover {
    background: var(--surface0);
  }
  .badge {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--kind) 18%, transparent);
    color: var(--kind);
  }
  .text {
    min-width: 0;
    overflow-wrap: anywhere;
    line-height: 1.35;
    padding-top: 3px;
  }
  .when {
    padding-top: 4px;
    font-size: 11px;
    color: var(--overlay1);
    white-space: nowrap;
  }
  .empty {
    display: grid;
    place-items: center;
    margin: 0;
    padding: 18px 16px;
    border: 0; /* (not the dashed box of an empty list elsewhere) */
    border-radius: 0;
    background: none;
    color: var(--overlay0);
  }
  @media (prefers-reduced-motion: reduce) {
    .unread {
      animation: none;
    }
  }
</style>
