<script lang="ts" generics="T extends string">
  import { overlayScroll } from '../lib/overlayScroll';
  import { glide } from '../lib/motion';
  // A dropdown in Vitrine's own look (the native one doesn't match the design,
  // least of all on desktop): a trigger button and a floating list - on phones a sheet along the bottom, like every
  // menu there (2026-10-25, the user: "on mobile all dropdowns should be the sheet"). Keyboard:
  // Enter/Space/arrows open it, arrows move, Enter picks, Esc closes.
  import { backClose } from '../lib/backClose';
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import { swallowPress } from '../lib/outside';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { sheetDrag } from '../lib/sheetDrag';

  let {
    value,
    options,
    onchange,
    label,
    heading = '',
    align = 'start',
    trigger,
    triggerClass = '',
    floating = false,
  }: {
    value: T;
    options: { value: T; label: string; meta?: string }[]; // meta: a muted note on the right (e.g. a count)
    onchange: (v: T) => void;
    label: string; // accessible name of the trigger
    heading?: string; // small title above the options
    align?: 'start' | 'end';
    trigger?: Snippet; // custom trigger content (e.g. an icon); default: the current label + chevron
    triggerClass?: string;
    floating?: boolean; // the menu floats over the page (the trigger sits in a scrolling or clipping row)
  } = $props();

  let open = $state(false);
  let sheet = $state(false); // phones: a sheet (decided on opening)
  let active = $state(0);
  let root = $state<HTMLElement>();
  let button = $state<HTMLButtonElement>();
  let list = $state<HTMLElement>();
  const id = 'dd-' + Math.random().toString(36).slice(2);

  let current = $derived(options.find((o) => o.value === value));

  // Floating: at the top of the page (no dialog or sheet crops it, nor - moved with transform - misplaces it), over
  // every layer.
  function toBody(node: HTMLElement) {
    document.body.appendChild(node);
    return () => node.remove();
  }
  // Floating: placed from the trigger's place on screen, below it (above when there's no room).
  let place = $state<Record<string, string> | null>(null);
  function placeMenu() {
    if (!floating || !button || sheet) return (place = null);
    const r = button.getBoundingClientRect();
    const h = list?.scrollHeight ?? 0; // known once shown (all of it, not the room it was first given); until then placed below
    const p: Record<string, string> = { 'min-width': `${Math.max(r.width, 200)}px` };
    // Below if it fits, else on the side with more room, never taller than that room (it scrolls).
    const below = innerHeight - r.bottom - 14;
    const above = r.top - 14;
    if (h <= below || below >= above) {
      p.top = `${r.bottom + 6}px`;
      p['max-height'] = `${below}px`;
    } else {
      p.bottom = `${innerHeight - r.top + 6}px`;
      p['max-height'] = `${above}px`;
    }
    if (align === 'end') p.right = `${Math.max(8, innerWidth - r.right)}px`;
    else p.left = `${Math.max(8, r.left)}px`;
    place = p;
  }
  // Placed again once the menu is there and its height is known.
  $effect(() => {
    if (open && list && floating) placeMenu();
  });
  // Never past the window's sides (a menu wider than its trigger, near the edge on a phone): moved in.
  $effect(() => {
    if (!open || !list || sheet) return;
    void place;
    const el = list;
    el.style.translate = '';
    const r = el.getBoundingClientRect();
    const pad = 8;
    let dx = Math.min(0, innerWidth - pad - r.right);
    if (r.left + dx < pad) dx = pad - r.left;
    if (dx) el.style.translate = `${dx}px 0`;
  });

  function show() {
    sheet = matchMedia('(max-width: 639px)').matches;
    active = Math.max(0, options.findIndex((o) => o.value === value));
    placeMenu();
    open = true;
    queueMicrotask(() => list?.focus());
  }

  function hide(refocus = true) {
    open = false;
    if (refocus) button?.focus();
  }

  function pick(i: number) {
    const o = options[i];
    hide();
    if (o && o.value !== value) onchange(o.value);
  }

  function onTriggerKey(e: KeyboardEvent) {
    if (['ArrowDown', 'ArrowUp'].includes(e.key)) {
      e.preventDefault();
      show();
    }
  }

  function onListKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') active = (active + 1) % options.length;
    else if (e.key === 'ArrowUp') active = (active - 1 + options.length) % options.length;
    else if (e.key === 'Home') active = 0;
    else if (e.key === 'End') active = options.length - 1;
    else if (e.key === 'Enter' || e.key === ' ') pick(active);
    else if (e.key === 'Escape' || e.key === 'Tab') hide(e.key === 'Escape');
    else return;
    e.preventDefault();
    e.stopPropagation(); // Esc closes only the dropdown, not a dialog behind it
  }

  // Close on a press outside.
  $effect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (root && !root.contains(e.target as Node) && !list?.contains(e.target as Node)) {
        swallowPress(e);
        hide(false);
      }
    };
    document.addEventListener('pointerdown', down, true);
    // Floating: scrolling what's behind it closes it (it would be left behind).
    const scrolled = (e: Event) => {
      if (floating && !list?.contains(e.target as Node)) hide(false);
    };
    document.addEventListener('scroll', scrolled, true);
    return () => {
      document.removeEventListener('pointerdown', down, true);
      document.removeEventListener('scroll', scrolled, true);
    };
  });
</script>

{#snippet items(title: string)}
  {#if title}<div class="heading">{title}</div>{/if}
  {#each options as o, i (o.value)}
    <div
      id="{id}-{i}"
      class="option"
      class:active={i === active}
      role="option"
      tabindex="-1"
      aria-selected={o.value === value}
      onpointerenter={(e) => e.pointerType === 'mouse' && (active = i)}
      onclick={() => pick(i)}
      onkeydown={() => {}}>
      <span>{o.label}</span>
      <span class="option-end">
        {#if o.meta}<span class="meta">{o.meta}</span>{/if}
        {#if o.value === value}<Icon name="check" size={16} />{:else if options.some((x) => x.meta)}<span class="check-space"></span>{/if}
      </span>
    </div>
  {/each}
{/snippet}

<div class="dropdown" bind:this={root}>
  <button
    bind:this={button}
    type="button"
    class={trigger ? triggerClass : `select ${triggerClass}`}
    aria-label={label}
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls={id}
    data-value={value}
    title={trigger ? `${label}: ${current?.label ?? value}` : (current?.label ?? value)}
    onclick={() => (open ? hide() : show())}
    onkeydown={onTriggerKey}>
    {#if trigger}
      {@render trigger()}
    {:else}
      <span class="current">{current?.label ?? value}</span>
      <Icon name="chevron" size={16} />
    {/if}
  </button>

  {#if open && sheet}
    <div {@attach toBody} data-floating=""><!-- (a menu under it - the filters sheet - takes a press here as its own) -->
      <div
        class="backdrop"
        role="presentation"
        in:shade
        out:shade={{ out: true }}
        onpointerdown={(e) => {
          e.preventDefault();
          hide(false);
        }}>
      </div>
      <div
        bind:this={list}
        {@attach backClose(() => hide(false))}
        {@attach sheetDrag(() => hide(false))}
        {id}
        class="menu sheet" {@attach overlayScroll}
        data-sheet=""
        role="listbox"
        tabindex="-1"
        aria-label={label}
        aria-activedescendant="{id}-{active}"
        onkeydown={onListKey}
        in:sheetIn
        out:sheetOut>
        <div class="grab-zone" aria-hidden="true"><span></span></div>
        {@render items(heading || label)}
      </div>
    </div>
  {:else if open}
    <div
      bind:this={list}
      {@attach backClose(() => hide(false))}
      {@attach floating && toBody}
      data-floating={floating ? '' : undefined}
      {id}
      class="menu {align}" {@attach overlayScroll}
      class:floating={!!place}
      style={place ? Object.entries(place).map(([k, v]) => `${k}: ${v}`).join('; ') : undefined}
      role="listbox"
      tabindex="-1"
      aria-label={label}
      aria-activedescendant="{id}-{active}"
      onkeydown={onListKey}
      transition:glide={{ y: -4, duration: 120 }}>
      {@render items(heading)}
    </div>
  {/if}
</div>

<style>
  .dropdown {
    position: relative;
    display: inline-block;
    min-width: 0;
  }
  /* The default trigger looks like an input. */
  .select {
    width: 100%;
    justify-content: space-between;
    gap: 8px;
    padding: 0 10px 0 11px;
    background: var(--crust);
    color: var(--text);
    border: 1px solid var(--surface1);
    font-weight: 400;
  }
  .select[aria-expanded='true'] {
    border-color: var(--mauve);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--mauve) 18%, transparent);
  }
  .select :global(svg) {
    color: var(--subtext);
    flex: none;
    transition: transform 0.15s;
  }
  .select[aria-expanded='true'] :global(svg) {
    transform: rotate(180deg);
  }
  @media (hover: hover) {
    .select:hover:not(:disabled) {
      filter: none;
      border-color: var(--surface2);
    }
  }
  /* A long choice (e.g. a group's name) is cut short with … rather than widening the field (the full
     name shows on hover). */
  .select {
    min-width: 0;
    max-width: 100%;
  }
  .current {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .menu {
    position: absolute;
    z-index: 40;
    top: calc(100% + 6px);
    min-width: max(100%, 200px);
    max-width: calc(100vw - 16px);
    max-height: min(60vh, 460px); /* a long list scrolls */
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 5px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 12px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.5);
    outline: none;
  }
  /* Its place comes from the style attribute (top or bottom, left or right). */
  .menu.floating {
    position: fixed;
    z-index: 90; /* (over the dialogs too) */
    top: auto;
    overflow-y: auto;
  }
  .menu.start {
    left: 0;
  }
  .menu.end {
    right: 0;
  }
  /* Phones: a sheet like every menu there - edge to edge along the bottom, a grab bar (drag down to close), room for
     the home bar; the page behind dims. */
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 89; /* (over the dialogs too, as the floating list) */
    background: rgb(17 17 27 / 0.5);
  }
  .menu.sheet {
    position: fixed;
    z-index: 90;
    left: 0;
    right: 0;
    top: auto;
    bottom: 0;
    min-width: 0;
    max-width: none;
    max-height: var(--sheet-max-h);
    padding: 0 12px calc(30px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  .menu.sheet .option {
    min-height: 48px;
  }
  /* (a touch isn't a cursor: the chosen one shows by its check and color) */
  .menu.sheet .option.active {
    background: none;
  }
  .grab-zone {
    position: sticky;
    top: 0;
    z-index: 1;
    display: grid;
    place-items: center;
    height: 22px;
    margin: 0 -12px;
    background: var(--mantle);
    touch-action: none;
    cursor: grab;
  }
  .grab-zone span {
    width: 38px;
    height: 5px;
    border-radius: 3px;
    background: var(--surface2);
  }
  .heading {
    padding: 6px 10px 4px;
    font-size: var(--font-small);
    font-weight: 600;
    color: var(--subtext);
  }
  .option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    min-height: calc(var(--control-h) - 2px);
    padding: 0 10px;
    border-radius: 8px;
    cursor: pointer;
    white-space: nowrap;
    color: var(--text);
  }
  .option-end {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .meta {
    font-size: var(--font-small);
    font-weight: 500;
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .check-space {
    width: 16px;
  }
  .option.active {
    background: var(--surface0);
  }
  .option[aria-selected='true'] {
    color: var(--mauve);
    font-weight: 600;
  }
</style>
