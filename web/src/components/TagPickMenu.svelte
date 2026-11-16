<script lang="ts" module>
  export type PickUse = 'all' | 'any' | 'add' | 'download' | 'download-any' | 'blacklist';
</script>

<script lang="ts">
  // The picked tags' round button and what it opens (2026-11-16, the user picked these from mockups after "the multi
  // select tags in the details panel I just don't like the experience of... how cramped the popup menu is with the
  // buttons"): while tags are picked, a round button with their count floats at the bottom right; it opens the
  // picked tags (each can be taken off) and what to do with them in cards, each line saying what it does - Search
  // (all of them, any of them, added to the search), Download (all, any) and the blacklist. A sheet along the bottom
  // on phones, a card above the button with a mouse. Clear ends picking.
  import { tick } from 'svelte';
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
  import { splitTag } from '../lib/namespaces.svelte';
  import { marquee, measure } from '../lib/marquee';
  import { t } from '../lib/i18n.svelte';


  let {
    chosen,
    blacklisted,
    onuse,
    onunpick,
    onclear,
  }: {
    chosen: string[];
    blacklisted: (tag: string) => boolean;
    onuse: (how: PickUse) => void;
    onunpick: (tag: string) => void;
    onclear: () => void;
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
  function use(how: PickUse) {
    close();
    onuse(how);
  }
  // With a mouse: above the button, right edges lined up.
  $effect(() => {
    if (!open || sheet || !card || !fab) return;
    const p = place(card.getBoundingClientRect(), fab.getBoundingClientRect(), { align: 'end', gap: 10 });
    pos = { left: p.left, bottom: p.above ? p.bottom : innerHeight - p.top - card.offsetHeight, ready: true };
    const c = card;
    tick().then(() => c.querySelector<HTMLButtonElement>('.act')?.focus({ preventScroll: true }));
  });
  // (the last one taken off ends picking - the component goes with it)
  const names = (sep: string) => chosen.map((x) => splitTag(x).name).join(sep);
  const many = $derived(chosen.length > 1);
  // The chips: one line sliding to its end and back when they don't fit (the app's marquee, as a download's search -
  // the user: "The tag chips here should be a scrollable carousel"); held still under a finger or the pointer, so
  // an × is easy to hit; measured again when one goes.
  let chipsBox = $state<HTMLElement>();
  $effect(() => {
    void chosen.length;
    if (chipsBox) requestAnimationFrame(() => chipsBox && measure(chipsBox));
  });
  const allOut = $derived(chosen.length > 0 && chosen.every(blacklisted));

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
    aria-label={t('details.useThem', { n: chosen.length })}
    title={t('details.useThem', { n: chosen.length })}
    aria-haspopup="dialog"
    aria-expanded={open}
    onclick={() => (open ? close() : show())}
    transition:pop
    ><Icon name="checks" size={24} />{#key chosen.length}<span class="badge" in:nudge>{chosen.length}</span>{/key}</button>

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
      aria-label={t('details.tagsPicked', { n: chosen.length })}
      tabindex="-1"
      data-sheet={sheet ? '' : undefined}
      style:left={sheet ? undefined : `${pos.left}px`}
      style:bottom={sheet ? undefined : `${pos.bottom}px`}
      style:visibility={sheet || pos.ready ? undefined : 'hidden'}
      {@attach sheet && sheetDrag(() => close())}
      {@attach backClose(() => close())}
      in:sheetIn={{ on: sheet, desk: 'drop' }}
      out:sheetOut={{ on: sheet, desk: 'drop' }}
      onkeydown={keydown}>
      {#if sheet}<GrabBar stuck bleed={14} />{/if}
      <div class="top">
        <span class="title">{t('details.tagsPicked', { n: chosen.length })}</span>
        <button class="clear" onclick={() => (close(), onclear())}>{t('details.clearPicked')}</button>
      </div>
      <div class="chips-box marquee sliding" bind:this={chipsBox} {@attach marquee}>
      <ul class="chips" aria-label={t('details.picked')}>
        {#each chosen as tag (tag)}
          <li transition:pop>
            <span>{splitTag(tag).name}</span>
            <button class="unpick" aria-label={t('details.unpick', { tag })} title={t('details.unpick', { tag })} onclick={() => onunpick(tag)}><Icon name="close" size={14} /></button>
          </li>
        {/each}
      </ul>
      </div>

      <h4>{t('tagMenu.search')}</h4>
      <div class="group">
        <button class="act" onclick={() => use('all')}>
          <Icon name="search" size={20} />
          <span class="txt"><span class="lbl">{many ? t('details.filesAll') : t('details.filesWith')}</span><span class="sub">{names(' ')}</span></span>
        </button>
        {#if many}
          <button class="act" onclick={() => use('any')} transition:nudge>
            <Icon name="or" size={20} />
            <span class="txt"><span class="lbl">{t('details.filesAny')}</span><span class="sub">{names(' OR ')}</span></span>
          </button>
        {/if}
        <button class="act" onclick={() => use('add')}>
          <Icon name="plus" size={20} />
          <span class="txt"><span class="lbl">{t('details.addToSearch')}</span></span>
        </button>
      </div>

      <h4>{t('backups.download')}</h4>
      <div class="group">
        <button class="act" onclick={() => use('download')}>
          <Icon name="download" size={20} />
          <span class="txt"><span class="lbl">{many ? t('details.dlAll') : t('details.dlIt')}</span><span class="sub">{t('details.dlHint', { n: chosen.length })}</span></span>
        </button>
        {#if many}
          <button class="act" onclick={() => use('download-any')} transition:nudge>
            <Icon name="or" size={20} />
            <span class="txt"><span class="lbl">{t('details.dlAny')}</span><span class="sub">{t('details.dlAnyHint')}</span></span>
          </button>
        {/if}
        <button class="act danger" disabled={allOut} onclick={() => use('blacklist')}>
          <Icon name="eyeoff" size={20} />
          <span class="txt"><span class="lbl">{allOut ? t('details.allBlacklisted') : many ? t('details.blacklistThem') : t('details.blacklistIt')}</span><span class="sub">{t('details.blHint', { n: chosen.length })}</span></span>
        </button>
      </div>
    </div>
  {/if}
</div>

<style>
  .fab,
  .fab:hover:not(:disabled) {
    position: fixed;
    z-index: 55;
    right: calc(18px + var(--safe-right, 0px));
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
  .chips-box {
    margin: 2px 0 4px;
  }
  .chips-box:is(:hover, :active, :focus-within),
  .chips-box:is(:hover, :active, :focus-within) > :global(*) {
    animation-play-state: paused;
  }
  .chips {
    display: inline-flex !important; /* (the marquee's one line: as wide as its chips) */
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
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
  h4 {
    margin: 8px 2px 2px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--overlay1);
  }
  .group {
    display: grid;
    border-radius: 14px;
    background: var(--crust);
    overflow: hidden;
  }
  .act,
  .act:hover:not(:disabled) {
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
  .act:not(:first-child) {
    box-shadow: inset 0 1px 0 var(--surface0);
  }
  @media (hover: hover) {
    .act:hover:not(:disabled) {
      background: var(--surface0);
    }
  }
  .act:focus-visible {
    background: var(--surface0);
    outline: none;
  }
  .act > :global(svg) {
    flex: none;
    color: var(--mauve);
  }
  .act.danger,
  .act.danger > :global(svg) {
    color: var(--red);
  }
  .act:disabled {
    opacity: 0.45;
  }
  .txt {
    display: grid;
    min-width: 0;
  }
  .lbl {
    overflow-wrap: anywhere;
  }
  .sub {
    color: var(--overlay1);
    font-size: 12px;
    font-weight: 400;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
