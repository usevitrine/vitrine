<script lang="ts">
  // The menu for one file (long-press on touch screens, right-click with a
  // mouse): select, view, details, favorite, delete. Shown next to the finger/cursor,
  // kept on screen. An extra item can open a small page of choices in its place (Mark as > Read / Unread;
  // back with the arrow, Left or Escape).
  import { backClose } from '../lib/backClose';
  import { place, atPoint } from '../lib/place';
  import Icon from './Icon.svelte';
  import type { ComponentProps } from 'svelte';
  import { t } from '../lib/i18n.svelte';
  import { Pages } from '../lib/pages.svelte';

  let {
    x,
    y,
    selected,
    favorite = false,
    viewLabel,
    viewIcon,
    basic = false,
    trashed = false,
    onrestore,
    extra = [],
    onselect,
    onfavorite,
    onview,
    ondetails,
    ondelete,
    onclose,
  }: {
    x: number;
    y: number;
    selected: boolean;
    favorite?: boolean;
    viewLabel?: string; // (a comic: "Read")
    viewIcon?: ComponentProps<typeof Icon>['name'];
    basic?: boolean; // only the view and the extra items (a collection's tile: Open, Rename, Take apart)
    trashed?: boolean; // in the trash: Restore, and Delete forever (no favorite)
    onrestore?: () => void;
    extra?: Extra[]; // (a comic: Mark as, Crop cover)
    onselect: () => void;
    onfavorite: () => void;
    onview: () => void;
    ondetails: () => void;
    ondelete: () => void;
    onclose: () => void;
  } = $props();

  type Extra = {
    label: string;
    icon: ComponentProps<typeof Icon>['name'];
    hint?: string; // (its current value, on the right)
    run?: () => void;
    danger?: boolean; // (deletes: red)
    choices?: { label: string; on: boolean; run: () => void }[];
  };
  let menu = $state<HTMLDivElement>();
  let page = $state<Extra | null>(null); // the choices shown in its place
  // A page of choices slides in, the height following (lib/pages, like every menu with pages); the width stays.
  const pages = new Pages();
  let width = $state<number | null>(null);
  function go(next: Extra | null) {
    if (width === null && menu) width = menu.offsetWidth;
    pages.go(!!next, () => (page = next)).then((el) => el?.querySelector<HTMLButtonElement>(next ? '[role=menuitemradio]' : 'button')?.focus());
  }
  let pos = $state({ left: 0, top: 0 });

  // At the point (to its left near the right edge, above it near the bottom), inside the window.
  $effect(() => {
    if (!menu) return;
    const p = place(menu.getBoundingClientRect(), atPoint(x, y), { gap: 0, flip: true });
    pos = { left: p.left, top: p.top };
  });
  // The first item takes the focus (a page of choices: go() moves it).
  $effect(() => {
    menu?.querySelector<HTMLButtonElement>('button')?.focus();
  });

  function pick(fn: () => void) {
    fn();
    onclose();
  }
</script>

<!-- A transparent backdrop closes the menu on any tap outside it. -->
<div
  class="backdrop"
  role="presentation"
  onpointerdown={(e) => {
    e.preventDefault();
    onclose();
  }}
  oncontextmenu={(e) => e.preventDefault()}>
</div>
<div
  class="menu"
  bind:this={menu}
  {@attach backClose(() => onclose())}
  role="menu"
  tabindex="-1"
  style:left="{pos.left}px"
  style:top="{pos.top}px"
  style:width={width !== null ? `${width}px` : undefined}
  onkeydown={(e) => {
    if (page && (e.key === 'Escape' || e.key === 'ArrowLeft')) {
      e.preventDefault();
      e.stopPropagation();
      go(null);
    } else if (e.key === 'Escape') onclose();
  }}>
  <div class="mpages" class:animating={pages.animating} {@attach pages.box} style:height={pages.height !== null ? `${pages.height}px` : undefined}>
  {#key pages.nav}
  <div class="mpage" {@attach pages.page} in:pages.pageIn out:pages.pageOut>
  {#if page}
    {@const p = page}
    <div class="page-head">
      <button class="back" aria-label={t('common.back')} onclick={() => go(null)}><Icon name="left" size={18} /></button>
      <span>{p.label}</span>
    </div>
    {#each p.choices ?? [] as c, ci (ci)}
      <button role="menuitemradio" aria-checked={c.on} class:on={c.on} onclick={() => pick(c.run)}>
        <span class="tick">{#if c.on}<Icon name="check" size={18} />{/if}</span>
        {c.label}
      </button>
    {/each}
  {:else}
  {#if !basic}<button role="menuitem" onclick={() => pick(onselect)}><Icon name="check" size={18} /> {selected ? t('common.deselect') : t('common.select')}</button>{/if}
  <button role="menuitem" onclick={() => pick(onview)}><Icon name={viewIcon ?? (viewLabel ? 'book' : 'expand')} size={18} /> {viewLabel ?? t('fileMenu.view')}</button>
  {#if !basic}
    <button role="menuitem" onclick={() => pick(ondetails)}><Icon name="info" size={18} /> {t('fileMenu.details')}</button>
    {#if trashed}
      <button role="menuitem" onclick={() => onrestore && pick(onrestore)}><Icon name="undo" size={18} /> {t('trash.restore')}</button>
    {:else}
      <button role="menuitem" onclick={() => pick(onfavorite)}><Icon name="heart" size={18} filled={favorite} /> {favorite ? t('fileMenu.unfavorite') : t('fileMenu.favorite')}</button>
    {/if}
  {/if}
  {#each extra as x (x.label)}
    {#if x.choices}
      <button role="menuitem" aria-haspopup="menu" onclick={() => go(x)}>
        <Icon name={x.icon} size={18} />
        <span class="grow">{x.label}</span>
        {#if x.hint}<span class="hint">{x.hint}</span>{/if}
        <Icon name="right" size={16} />
      </button>
    {:else}
      <button role="menuitem" class:danger={x.danger} onclick={() => x.run && pick(x.run)}><Icon name={x.icon} size={18} /> {x.label}</button>
    {/if}
  {/each}
  {#if !basic}<button role="menuitem" class="danger" onclick={() => pick(ondelete)}><Icon name="trash" size={18} /> {trashed ? t('trash.purge') : t('common.delete')}</button>{/if}
  {/if}
  </div>
  {/key}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 45;
  }
  .menu {
    position: fixed;
    z-index: 46;
    min-width: 180px;
    display: grid;
    padding: 4px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 12px;
    box-shadow: 0 12px 32px rgb(0 0 0 / 0.5);
  }
  button {
    display: flex;
    align-items: center;
    gap: 10px;
    justify-content: flex-start;
    background: none;
    color: var(--text);
    font-weight: 500;
    border-radius: 8px;
    padding: 0 12px;
  }
  button:hover,
  button:focus-visible {
    background: var(--surface0);
  }
  .danger {
    color: var(--red);
  }
  .grow {
    flex: 1;
    text-align: left;
  }
  .hint {
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 400;
  }
  .page-head {
    display: flex;
    align-items: center;
    gap: 4px;
    padding-right: 12px;
    font-weight: 700;
    border-bottom: 1px solid var(--surface0);
    margin-bottom: 4px;
  }
  .page-head .back {
    padding: 0;
    width: var(--control-h);
    justify-content: center;
  }
  .tick {
    width: 18px;
    display: inline-grid;
    color: var(--mauve);
  }
  button.on {
    color: var(--mauve);
  }
</style>
