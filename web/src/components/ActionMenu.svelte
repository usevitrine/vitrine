<script lang="ts" module>
  import { settle, sizeOf } from '../lib/motion';
  import type { ComponentProps } from 'svelte';
  import { place } from '../lib/place';
  import type IconType from './Icon.svelte';

  export interface MenuChoice {
    heading: string; // the page's title
    value: string; // the current choice (checked)
    options: { value: string; label: string }[];
    pick: (value: string) => void;
  }
  export interface MenuItem {
    label: string;
    icon?: ComponentProps<typeof IconType>['name'];
    hint?: string; // a muted note on the right (e.g. the current schedule)
    danger?: boolean;
    disabled?: boolean;
    on?: boolean; // the current choice (lit, like a picked option)
    run?: () => void;
    choices?: MenuChoice; // opens a page of its own to pick from
  }
</script>

<script lang="ts">
  // A ⋯ button with the actions of a row (e.g. a subscription): a menu at the
  // button, or an action sheet along the bottom on phones. An item with choices
  // (a schedule, a group) opens them as a page of the menu, with a way back.
  import { backClose } from '../lib/backClose';
  import { tick } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import Icon from './Icon.svelte';
  import { sheetDrag } from '../lib/sheetDrag';
  import { sheetIn, sheetOut } from '../lib/sheet';
  import { Pages } from '../lib/pages.svelte';
  import { t } from '../lib/i18n.svelte';

  // With `current` it's a picker instead: a field showing the current choice
  // (like a dropdown) opening the same menu, lined up under the field.
  // icon: the button's own icon (else ⋯), with quiet: styled as a quiet button (a toolbar's, e.g. the selection bar's).
  let {
    label,
    title = '',
    items,
    current,
    icon,
    quiet = false,
    disabled = false,
    text,
  }: { label: string; title?: string; items: MenuItem[]; current?: string; icon?: ComponentProps<typeof IconType>['name']; quiet?: boolean; disabled?: boolean; text?: string } = $props();

  let open = $state(false);
  let page = $state<MenuChoice | null>(null);
  let trigger = $state<HTMLButtonElement>();
  let menu = $state<HTMLDivElement>();
  // Below the button (top), or above it (bottom: a taller page then grows upwards).
  // width: as measured on opening (its widest item), then kept - its pages don't make it jump.
  let pos = $state<{ left: number; top: number | null; bottom: number | null; ready: boolean; width?: number }>({ left: 0, top: 0, bottom: null, ready: false });
  let sheet = $state(false);

  function show() {
    sheet = matchMedia('(max-width: 639px)').matches;
    page = null;
    pos = { left: 0, top: 0, bottom: null, ready: false };
    open = true;
  }
  function close(refocus = false) {
    open = false;
    page = null;
    if (refocus) trigger?.focus({ preventScroll: true });
  }

  // At the button, right edges lined up (above it if there's no room below), inside the window.
  $effect(() => {
    if (!open || !menu || !trigger) return;
    const r = menu.getBoundingClientRect();
    const p = place(r, trigger.getBoundingClientRect(), { align: current !== undefined ? 'start' : 'end' });
    const width = Math.ceil(r.width);
    pos = p.above ? { left: p.left, top: null, bottom: p.bottom, ready: true, width } : { left: p.left, top: p.top, bottom: null, ready: true, width };
    const m = menu;
    tick().then(() => m.querySelector<HTMLButtonElement>('[role^=menuitem]:not(:disabled)')?.focus({ preventScroll: true }));
  });

  // ---- pages: a page of choices slides in (lib/pages, like every menu with pages).
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  const pages = new Pages();
  function go(next: MenuChoice | null) {
    pages
      .go(!!next, () => (page = next))
      .then((el) => el?.querySelector<HTMLButtonElement>('[role^=menuitem]:not(:disabled)')?.focus({ preventScroll: true }));
  }

  // ---- opening and closing: the menu drops in and fades out (as the other
  // menus do); the sheet slides up from the bottom and back down.
  function menuIn(node: Element) {
    // Reopened while still fading out, the same element comes back: usable again.
    node.removeAttribute('inert');
    node.removeAttribute('aria-hidden');
    const el = node as HTMLElement; // (and not where a drag left it)
    delete el.dataset.dragged;
    el.style.transform = '';
    el.style.transition = '';
    if (reduced()) return { duration: 0 };
    if (sheet) return sheetIn(node);
    return { duration: 180, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: translateY(${(1 - t) * -8}px) scale(${settle(0.98 + 0.02 * t, sizeOf(node))})` };
  }
  function menuOut(node: Element) {
    node.setAttribute('inert', ''); // a closing menu can't be pressed or read while it goes
    node.setAttribute('aria-hidden', 'true');
    if (reduced()) return { duration: 0 };
    if (sheet) return sheetOut(node); // from where a drag left it
    return { duration: 180, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: translateY(${(1 - t) * -8}px) scale(${settle(0.96 + 0.04 * t, sizeOf(node))})` };
  }
  function shade(node: HTMLElement, { out = false } = {}) {
    if (out) node.style.pointerEvents = 'none'; // the page is usable again at once
    return reduced() ? { duration: 0 } : { duration: out ? 160 : 180, css: (t: number) => `opacity: ${t}` };
  }

  function pick(item: MenuItem) {
    if (item.choices) {
      go(item.choices);
      return;
    }
    close();
    item.run?.();
  }
  function choose(c: MenuChoice, value: string) {
    close();
    c.pick(value);
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      if (page) go(null);
      else close(true);
      return;
    }
    if (e.key === 'ArrowLeft' && page) {
      e.preventDefault();
      go(null);
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    const els = [...menu!.querySelectorAll<HTMLButtonElement>('[role^=menuitem]:not(:disabled), .back')];
    const i = els.indexOf(document.activeElement as HTMLButtonElement);
    els[(i + (e.key === 'ArrowDown' ? 1 : els.length - 1)) % els.length]?.focus();
  }

  // The open menu's elements. Not bind:this: a menu still fading out after
  // closing would clear the reference when it goes - after the next menu has
  // set it - and that menu would never be placed (it stays hidden).
  function own<T extends HTMLElement>(set: (el: T | undefined) => void, get: () => T | undefined) {
    return (el: T) => {
      set(el);
      return () => {
        if (get() === el) set(undefined);
      };
    };
  }

  // On top of everything (rows scroll and clip).
  function toBody(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }
</script>

{#if current !== undefined}
  <button
    bind:this={trigger}
    type="button"
    class="picker"
    aria-label={label}
    aria-haspopup="menu"
    aria-expanded={open}
    onclick={() => (open ? close() : show())}><span class="grow">{current}</span><Icon name="chevron" size={16} /></button>
{:else}
  <button
    bind:this={trigger}
    class={(quiet ? 'quiet more' : icon ? 'icon more' : 'icon more dots') + (text ? ' texted' : '')}
    aria-label={label}
    title={icon ? label : t('common.more')}
    aria-haspopup="menu"
    aria-expanded={open}
    {disabled}
    onclick={() => (open ? close() : show())}><Icon name={icon ?? 'morev'} size={quiet ? 18 : 20} />{#if text}<span class="text">{text}</span>{/if}</button>
{/if}

{#if open}
  <div use:toBody>
    <div
      class="backdrop"
      class:dim={sheet}
      role="presentation"
      in:shade
      out:shade={{ out: true }}
      onpointerdown={(e) => {
        e.preventDefault();
        close();
      }}>
    </div>
    <div
      class="menu"
      class:sheet
      class:wide={current !== undefined}
      {@attach own((el) => (menu = el), () => menu)}
      role="menu"
      tabindex="-1"
      aria-label={page ? page.heading : label}
      style:left={sheet ? undefined : `${pos.left}px`}
      style:width={sheet || !pos.width ? undefined : `${pos.width}px`}
      style:top={sheet || pos.top === null ? undefined : `${pos.top}px`}
      style:bottom={sheet || pos.bottom === null ? undefined : `${pos.bottom}px`}
      data-sheet={sheet ? '' : undefined}
    {@attach sheet && sheetDrag(() => close())}
    {@attach backClose(() => close())}
      in:menuIn
      out:menuOut
      style:visibility={sheet || pos.ready ? undefined : 'hidden'}
      onkeydown={keydown}>
      {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
      <div class="mpages" class:animating={pages.animating} {@attach pages.box} style:height={pages.height !== null ? `${pages.height}px` : undefined}>
      {#key pages.nav}
      <div class="mpage" {@attach pages.page} in:pages.pageIn out:pages.pageOut>
      {#if page}
        <div class="head">
          <button class="back" aria-label={t('common.back')} onclick={() => go(null)}><Icon name="left" size={18} /></button>
          <span class="title">{page.heading}</span>
        </div>
        <div class="group">
          {#each page.options as o (o.value)}
            {@const on = o.value === page.value}
            <button role="menuitemradio" aria-checked={on} class:on onclick={() => choose(page!, o.value)}>
              <span class="grow">{o.label}</span>
              {#if on}<Icon name="check" size={16} />{/if}
            </button>
          {/each}
        </div>
      {:else}
        {#if title}<div class="head"><span class="title">{title}</span></div>{/if}
        <div class="group" class:first={!title}>
          {#each items as it, i (it.label)}
            {#if it.danger && i > 0 && !items[i - 1].danger}<div class="sep" role="separator"></div>{/if}
            <button role={it.on === undefined ? 'menuitem' : 'menuitemradio'} aria-checked={it.on} class:on={it.on} class:danger={it.danger} disabled={it.disabled} onclick={() => pick(it)}>
              {#if it.icon}<Icon name={it.icon} size={18} />{/if}
              <span class="grow">{it.label}</span>
              {#if it.hint}<span class="hint">{it.hint}</span>{/if}
              {#if it.choices}<Icon name="right" size={16} />{:else if it.on}<Icon name="check" size={16} />{/if}
            </button>
          {/each}
        </div>
      {/if}
      </div>
      {/key}
      </div>
    </div>
  </div>
{/if}

<style>
  .text {
    font-size: var(--font-small);
    font-weight: 600;
  }
  /* (vertical dots in a narrow button: less room beside what it's for) */
  .more {
    flex: none;
  }
  .icon.more.dots {
    width: 24px;
    min-width: 24px;
    padding: 0;
  }
  /* The picker's field looks like the other dropdowns. */
  .picker {
    width: 100%;
    justify-content: space-between;
    gap: 8px;
    padding: 0 10px 0 11px;
    background: var(--crust);
    color: var(--text);
    border: 1px solid var(--surface1);
    font-weight: 400;
    text-align: left;
  }
  .picker[aria-expanded='true'] {
    border-color: var(--mauve);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--mauve) 18%, transparent);
  }
  .picker > :global(svg) {
    flex: none;
    color: var(--subtext);
    transition: transform 0.15s;
  }
  .picker[aria-expanded='true'] > :global(svg) {
    transform: rotate(180deg);
  }
  @media (hover: hover) {
    .picker:hover:not(:disabled) {
      filter: none;
      border-color: var(--surface2);
    }
  }
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
  }
  .backdrop.dim {
    background: rgb(17 17 27 / 0.5);
  }
  .menu {
    position: fixed;
    z-index: 61;
    /* As wide as its longest item (a value on the right included), within limits: long ones shorten. */
    width: max-content;
    min-width: min(240px, calc(100vw - 16px));
    max-width: min(380px, calc(100vw - 16px));
    display: grid;
    padding: 4px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.55);
  }
  .menu.wide {
    min-width: min(300px, calc(100vw - 16px));
  }
  /* Phones: a sheet like the details panel - edge to edge along the bottom,
     rounded top corners, a grab bar (drag down to close), and room at the
     bottom for the home bar and the screen's rounded corners. */
  .menu.sheet {
    left: 0;
    right: 0;
    top: auto;
    bottom: 0;
    width: auto;
    min-width: 0;
    max-width: none;
    max-height: var(--sheet-max-h);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 0 12px calc(30px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
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
  /* Pages stack in one grid cell, so the old and the new can cross. */
  .head {
    display: flex;
    align-items: center;
    gap: 4px;
    min-height: var(--control-h);
    padding: 2px 8px 2px 12px;
  }
  .head:has(.back) {
    padding-left: 2px;
  }
  .title {
    font-weight: 700;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .group {
    display: grid;
    padding-top: 4px;
    margin-top: 2px;
    border-top: 1px solid var(--surface0);
  }
  .group.first {
    padding-top: 0;
    margin-top: 0;
    border-top: 0;
  }
  .sep {
    height: 1px;
    margin: 4px 0;
    background: var(--surface0);
  }
  .menu button {
    display: flex;
    align-items: center;
    gap: 12px;
    justify-content: flex-start;
    width: 100%;
    min-height: var(--control-h);
    padding: 6px 12px;
    background: none;
    color: var(--text);
    font-weight: 500;
    text-align: left;
    white-space: nowrap;
    border-radius: 9px;
  }
  .menu button > :global(svg) {
    flex: none;
    color: var(--subtext);
  }
  .menu button.back {
    width: var(--control-h);
    padding: 0;
    justify-content: center;
  }
  .grow {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .hint {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--subtext);
    font-size: 13px;
    font-weight: 400;
  }
  .menu button.on,
  .menu button.on > :global(svg),
  .menu button.on .hint {
    color: var(--mauve);
  }
  .menu button.danger,
  .menu button.danger > :global(svg) {
    color: var(--red);
  }
  @media (hover: hover) {
    .menu button:hover:not(:disabled) {
      background: var(--surface0);
      filter: none;
    }
  }
  .menu button:focus-visible {
    background: var(--surface0);
  }
</style>
