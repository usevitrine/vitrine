<script lang="ts">
  // What to do with a tag tapped in the details: search with it (add, exclude,
  // a new search), start a new download for it, copy it (with or without its namespace), put it in
  // another namespace (a page of its own), remove it from the file. A menu next to the tag, kept on screen; Escape or a tap outside closes it.
  import { backClose } from '../lib/backClose';
  import { place } from '../lib/place';
  import { tick } from 'svelte';
  import Icon from './Icon.svelte';
  import { sheetDrag } from '../lib/sheetDrag';
  import { Pages } from '../lib/pages.svelte';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { nsChoices, nsColor, nsLabel, splitTag } from '../lib/namespaces.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    tag,
    anchor,
    inSearch,
    onsearch,
    ondownload,
    onnamespace,
    oncopy,
    blacklistEntry,
    onblacklist,
    onremove,
    onclose,
    removeText = t('tagMenu.removeFromFile'),
  }: {
    tag: string;
    anchor: DOMRect; // the tag's place on screen
    inSearch: 'in' | 'out' | null; // whether the current search has it, excludes it, or neither
    onsearch: (how: 'add' | 'exclude' | 'drop' | 'only') => void;
    ondownload: () => void; // a new download for the tag
    onnamespace?: (ns: string) => void; // into another namespace ("" = general), on every file (none: not offered)
    oncopy: (text: string) => void;
    /** The blacklist entry it's on ("" none; undefined: not known yet - Add is offered). Its own entry can be taken
     *  off here; one of a pattern ("ai_*") is named instead (taking that off would let other tags through too). */
    blacklistEntry?: string;
    /** Adds the tag to the download blacklist, or takes it off (none: not offered). */
    onblacklist?: () => void;
    onremove?: () => void; // (none: not offered - tags that can't be changed here, e.g. a site's gallery being browsed)
    onclose: (refocus?: boolean) => void;
    removeText?: string; // (a comic's: "Remove from this comic")
  } = $props();

  const parts = $derived(splitTag(tag));
  let menu = $state<HTMLDivElement>();
  let nsPage = $state(false); // the Namespace page is on show
  // Pages move like every menu's (lib/pages).
  const pages = new Pages();
  function showNs(on: boolean) {
    pages
      .go(on, () => (nsPage = on))
      .then((el) => el?.querySelector<HTMLButtonElement>('[role^=menuitem]')?.focus({ preventScroll: true }));
  }
  let pos = $state({ left: 0, top: 0, ready: false });
  // Phones: an action sheet along the bottom; wider screens: a menu at the tag.
  const sheet = matchMedia('(max-width: 639px)').matches;

  // Under the tag (above it if there's no room), inside the window.
  $effect(() => {
    if (!menu) return;
    const p = place(menu.getBoundingClientRect(), anchor, { gap: 6 });
    pos = sheet ? { left: 0, top: 0, ready: true } : { left: p.left, top: p.top, ready: true };
    // Once shown (a hidden element can't take focus): keys go to the menu.
    const m = menu;
    tick().then(() => m.querySelector<HTMLButtonElement>('[role=menuitem]')?.focus({ preventScroll: true }));
  });

  function pick(fn: () => void) {
    fn();
    onclose();
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault(); // the menu only, not the details or the viewer (they skip handled keys)
      e.stopPropagation();
      onclose(true);
      return;
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    const items = [...menu!.querySelectorAll<HTMLButtonElement>('[role^=menuitem]')];
    const i = items.indexOf(document.activeElement as HTMLButtonElement);
    items[(i + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length]?.focus();
  }

  // On top of everything (the viewer and its sheet move and clip).
  function toBody(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }
</script>

<div use:toBody>
  <div
    class="backdrop"
    class:dim={sheet}
    in:shade
    out:shade={{ out: true }}
    role="presentation"
    onpointerdown={(e) => {
      e.preventDefault();
      onclose();
    }}>
  </div>
  <div
    class="menu"
    class:sheet
    data-sheet={sheet ? '' : undefined}
    {@attach sheet && sheetDrag(onclose)}
    {@attach backClose(onclose)}
    in:sheetIn={{ on: sheet, desk: 'drop' }}
    out:sheetOut={{ on: sheet, desk: 'drop' }}
    bind:this={menu}
    role="menu"
    tabindex="-1"
    aria-label={t('tagMenu.label', { tag: parts.name })}
    style:left={sheet ? undefined : `${pos.left}px`}
    style:top={sheet ? undefined : `${pos.top}px`}
    style:visibility={pos.ready ? undefined : 'hidden'}
    style:--c={nsColor(parts.ns)}
    onkeydown={keydown}>
    {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
    <div class="mpages" class:animating={pages.animating} {@attach pages.box} style:height={pages.height !== null ? `${pages.height}px` : undefined}>
    {#key pages.nav}
    <div class="mpage" {@attach pages.page} in:pages.pageIn out:pages.pageOut>
    {#if nsPage}
      <div class="head page">
        <button class="back" aria-label={t('common.back')} onclick={() => showNs(false)}><Icon name="left" size={18} /></button>
        <span class="name">{t('tagMenu.namespace')}</span>
      </div>
      <div class="group" role="group" aria-label={t('tagMenu.namespace')}>
        {#each nsChoices() as ns (ns)}
          <button role="menuitemradio" aria-checked={ns === parts.ns} class:on={ns === parts.ns} onclick={() => pick(() => ns !== parts.ns && onnamespace?.(ns))}>
            <span class="dot" style:background={nsColor(ns)}></span>
            <span class="grow">{nsLabel(ns)}</span>
            {#if ns === parts.ns}<Icon name="check" size={16} />{/if}
          </button>
        {/each}
      </div>
    {:else}
    <div class="head">
      <span class="name">{#each parts.name.split('_') as part, k}{#if k}_<wbr />{/if}{part}{/each}</span>
      {#if parts.ns}<span class="ns">{nsLabel(parts.ns)}</span>{/if}
    </div>

    <div class="group" role="group" aria-label={t('tagMenu.search')}>
      {#if inSearch === 'in'}
        <button role="menuitem" onclick={() => pick(() => onsearch('drop'))}><Icon name="minus" size={18} /> {t('tagMenu.removeFromSearch')}</button>
      {:else}
        <button role="menuitem" onclick={() => pick(() => onsearch('add'))}><Icon name="plus" size={18} /> {t('tagMenu.addToSearch')}</button>
      {/if}
      {#if inSearch === 'out'}
        <button role="menuitem" onclick={() => pick(() => onsearch('drop'))}><Icon name="undo" size={18} /> {t('tagMenu.includeAgain')}</button>
      {:else}
        <button role="menuitem" onclick={() => pick(() => onsearch('exclude'))}><Icon name="block" size={18} /> {t('tagMenu.exclude')}</button>
      {/if}
      <button role="menuitem" onclick={() => pick(() => onsearch('only'))}><Icon name="search" size={18} /> {t('tagMenu.newSearch')}</button>
      <button role="menuitem" onclick={() => pick(ondownload)}><Icon name="download" size={18} /> {t('tagMenu.newDownload')}</button>
      {#if onblacklist}
        {#if blacklistEntry && blacklistEntry !== tag.toLowerCase() && blacklistEntry !== parts.name.toLowerCase()}
          <button role="menuitem" disabled
            ><Icon name="eyeoff" size={18} /> <span class="two">{t('tagMenu.onBlacklist')}<span class="what">{blacklistEntry}</span></span></button>
        {:else}
          <button role="menuitem" onclick={() => pick(onblacklist)}
            ><Icon name="eyeoff" size={18} /> {blacklistEntry ? t('tagMenu.unblacklist') : t('tagMenu.blacklist')}</button>
        {/if}
      {/if}
    </div>

    <div class="group" role="group" aria-label={t('tagMenu.copy')}>
      <button role="menuitem" onclick={() => pick(() => oncopy(tag))}>
        <Icon name="copy" size={18} />
        <span class="two">{t('tagMenu.copy')}<span class="what">{tag}</span></span>
      </button>
      {#if parts.ns}
        <button role="menuitem" onclick={() => pick(() => oncopy(parts.name))}>
          <Icon name="copy" size={18} />
          <span class="two">{t('tagMenu.copyBare')}<span class="what">{parts.name}</span></span>
        </button>
      {/if}
    </div>

    {#if onnamespace}
      <div class="group">
        <button role="menuitem" onclick={() => showNs(true)}>
          <Icon name="folder" size={18} />
          <span class="grow">{t('tagMenu.namespace')}</span>
          <span class="now">{nsLabel(parts.ns)}</span>
          <Icon name="right" size={16} />
        </button>
      </div>
    {/if}

    {#if onremove}
      <div class="group">
        <button role="menuitem" class="danger" onclick={() => pick(onremove)}><Icon name="close" size={18} /> {removeText}</button>
      </div>
    {/if}
    {/if}
    </div>
    {/key}
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
  }
  .backdrop.dim {
    background: rgb(17 17 27 / 0.5);
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
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
    max-height: var(--sheet-max-h);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 0 12px calc(30px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
    animation: none; /* it slides (lib/sheet) */
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
  .menu {
    position: fixed;
    z-index: 61;
    width: min(290px, calc(100vw - 16px));
    display: grid;
    padding: 4px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.55);
    animation: pop 0.12s ease-out;
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-4px) scale(0.98);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .backdrop.dim,
    .menu.sheet,
    .menu {
      animation: none;
    }
  }
  .head {
    display: grid;
    gap: 1px;
    padding: 8px 12px 8px;
  }
  .head .name {
    font-weight: 700;
    color: var(--c);
    overflow-wrap: anywhere;
    line-height: 1.3;
  }
  .head .ns {
    font-size: 12px;
    color: var(--subtext);
  }
  .group {
    display: grid;
    padding-top: 4px;
    margin-top: 4px;
    border-top: 1px solid var(--surface0);
  }
  button {
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
    white-space: normal;
    border-radius: 9px;
  }
  button > :global(svg) {
    flex: none;
    color: var(--subtext);
  }
  @media (hover: hover) {
    button:hover {
      background: var(--surface0);
      filter: none;
    }
  }
  button:focus-visible {
    background: var(--surface0);
  }
  .two {
    min-width: 0;
    display: grid;
    gap: 1px;
  }
  .what {
    font-size: 12px;
    font-weight: 400;
    color: var(--subtext);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    overflow-wrap: anywhere;
  }
  .danger,
  .danger > :global(svg) {
    color: var(--red);
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .now {
    font-size: 12px;
    color: var(--subtext);
  }
  .head.page {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px 4px 4px;
  }
  .head.page .name {
    color: var(--text);
  }
  .head.page .back {
    width: var(--control-h);
    min-height: var(--control-h);
    padding: 0;
    justify-content: center;
    flex: none;
  }
  .dot {
    flex: none;
    width: 10px;
    height: 10px;
    margin: 0 4px;
    border-radius: 50%;
  }
  button.on {
    color: var(--mauve);
  }
  button.on > :global(svg) {
    color: var(--mauve);
  }
</style>
