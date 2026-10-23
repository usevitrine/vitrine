<script lang="ts">
  // A search chip's menu: its text, editable in place (with OR alternatives),
  // and what to do with it - exclude or include it, add an alternative, split
  // a group, search with only it, start a new download for it, copy it,
  // remove it. Like a tag's menu in the details: a menu under the chip, a
  // sheet along the bottom on phones. Edits apply on Enter and on closing.
  import { backClose } from '../lib/backClose';
  import { reveal, pop } from '../lib/motion';
  import { kindOf, titleTerm,type Atom, type Chip } from '../lib/query';
  import { nsColor, nsLabel, splitTag } from '../lib/namespaces.svelte';
  import { copyText } from '../lib/clipboard';
  import { sheetDrag } from '../lib/sheetDrag';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import TagInput from './TagInput.svelte';
  import Icon from './Icon.svelte';
  import type { ComponentProps } from 'svelte';
  import { on } from 'svelte/events';
  import { t } from '../lib/i18n.svelte';
  import { useScope } from '../lib/scope.svelte';

  let {
    chip,
    onchange, // the edited chip, or null to remove it
    onsplit,
    ononly,
    ondownload,
    onclose,
  }: {
    chip: Chip;
    onchange: (c: Chip | null) => void;
    onsplit: (chips: Chip[]) => void;
    ononly: (c: Chip) => void; // a new search with only this chip
    ondownload: (tags: string[]) => void; // a new download for the tag (several: an OR group - posts with any of them)
    onclose: () => void;
  } = $props();
  const comics = useScope().kind === 'comics';

  // A working copy, taken once when the menu opens (it is re-created for each
  // chip); changes are applied on Enter, toggles, removals and closing.
  // svelte-ignore state_referenced_locally
  let atoms = $state<Atom[]>(chip.atoms.map((a) => ({ ...a })));
  let focusLast = $state(false);
  // Phones: a sheet along the bottom; wider screens: a menu under the chip.
  const sheet = matchMedia('(max-width: 639px)').matches;
  // With a mouse the text is ready to type in; on touch screens no keyboard pops up.
  const mouse = matchMedia('(pointer: fine)').matches;

  const single = $derived(atoms.length === 1 ? atoms[0] : null);
  const parts = $derived(single ? splitTag(single.text.trim()) : null);
  // A tag, or an OR group of tags (its download: the site's OR search) - not a filter, wildcard or excluded tag.
  const downloadable = $derived(atoms.length > 0 && atoms.every((a) => !a.neg && a.text.trim() !== '' && kindOf(a.text) === 'tag'));

  const kept = () => atoms.filter((a) => a.text.trim() !== '').map((a) => ({ neg: a.neg, text: titleTerm(a.text) }));

  function apply() {
    const k = kept();
    onchange(k.length ? { atoms: k } : null);
  }

  /** Closing applies what was typed. */
  function close() {
    apply();
    onclose();
  }

  function toggle(i: number) {
    atoms[i].neg = !atoms[i].neg;
    apply();
  }

  function removeAtom(i: number) {
    atoms.splice(i, 1);
    apply();
    if (!atoms.length) onclose();
  }

  function addAlternative() {
    atoms.push({ neg: false, text: '' });
    focusLast = true;
  }

  function split() {
    onsplit(kept().map((a) => ({ atoms: [a] })));
    onclose();
  }

  function only() {
    const k = kept();
    if (k.length) ononly({ atoms: k });
    onclose();
  }

  function remove() {
    onchange(null);
    onclose();
  }

  // Escape closes it from anywhere (a sheet doesn't take the focus on touch
  // screens: no keyboard popping up). An open suggestion list goes first.
  $effect(() =>
    on(window, 'keydown', (e) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      e.preventDefault();
      close();
    }),
  );

  // On top of everything on phones (the search bar's box would clip a sheet).
  function portal(node: HTMLElement) {
    if (sheet) document.body.appendChild(node);
    return { destroy: () => sheet && node.remove() };
  }
</script>

{#snippet item(icon: ComponentProps<typeof Icon>['name'], text: string, run: () => void, cls = '')}
  <button class="item {cls}" onclick={run}><Icon name={icon} size={18} /> {text}</button>
{/snippet}

<div use:portal>
  <div
    class="backdrop"
    class:dim={sheet}
    in:shade|global
    out:shade|global={{ out: true }}
    role="presentation"
    onpointerdown={(e) => {
      e.preventDefault();
      close();
    }}>
  </div>
  <div
    class="menu"
    class:sheet
    data-sheet={sheet ? '' : undefined}
    {@attach sheet && sheetDrag(close)}
    {@attach backClose(close)}
    in:sheetIn|global={{ on: sheet }}
    out:sheetOut|global={{ on: sheet }}
    role="dialog"
    aria-label={t('chip.edit')}
    tabindex="-1"
    style:--c={parts ? nsColor(parts.ns) : 'var(--mauve)'}>
    {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
    <div class="head">
      {#if single && parts}
        <span class="name">{#each (parts.name || ' ').split('_') as part, k}{#if k}_<wbr />{/if}{part}{/each}</span>
        <span class="ns">{nsLabel(parts.ns)}{#if single.neg}<span class="excluded"> · {t('chip.excluded')}</span>{/if}</span>
      {:else}
        <span class="name">{t('chip.anyOf')}</span>
        <span class="ns">{t('chip.alternatives', { n: atoms.length })}</span>
      {/if}
    </div>

    <div class="edit">
      {#each atoms as a, i (i)}
        {#if i > 0}<span class="or" transition:pop>{t('chip.or')}</span>{/if}
        <div class="row">
          <!-- One tag: Exclude below says it; a group's alternatives each have their own. -->
          {#if atoms.length > 1}<button
            class="icon neg"
            class:on={a.neg}
            title={a.neg ? t('chip.excludedTap') : t('chip.includedTap')}
            aria-label={a.neg ? t('chip.excludedLabel') : t('chip.includedLabel')}
            aria-pressed={a.neg}
            onclick={() => toggle(i)} transition:pop><Icon name={a.neg ? 'minus' : 'plus'} size={18} /></button>{/if}
          <TagInput
            bind:value={a.text}
            comics={comics}
            label={t('chip.text')}
            autofocus={(mouse && i === 0) || (focusLast && i === atoms.length - 1)}
            onenter={apply} />
          {#if atoms.length > 1}
            <button class="icon" title={t('chip.removeAlt')} aria-label={t('chip.removeAlt')} onclick={() => removeAtom(i)}
               transition:pop><Icon name="close" size={18} /></button>
          {/if}
        </div>
      {/each}
    </div>

    <div class="group" role="group" aria-label={t('tagMenu.search')}>
      {#if single}
        {#if single.neg}
          {@render item('undo', t('tagMenu.includeAgain'), () => toggle(0))}
        {:else}
          {@render item('block', t('tagMenu.exclude'), () => toggle(0))}
        {/if}
      {/if}
      {@render item('plus', t('chip.addAlt'), addAlternative)}
      {#if atoms.length > 1}{@render item('split', t('chip.split'), split)}{/if}
      {@render item('search', t('tagMenu.newSearch'), only)}
      {#if downloadable}
        {@render item('download', single ? t('tagMenu.newDownload') : t('details.downloadAny'), () => {
          const tags = atoms.map((a) => a.text.trim());
          apply();
          onclose();
          ondownload(tags);
        })}
      {/if}
    </div>

    {#if single && parts && single.text.trim()}
      {@const text = single.text.trim()}
      <div class="group" role="group" aria-label={t('tagMenu.copy')} transition:reveal>
        <button class="item" onclick={() => copyText(text)}>
          <Icon name="copy" size={18} />
          <span class="two">{t('tagMenu.copy')}<span class="what">{text}</span></span>
        </button>
        {#if parts.ns}
          <button class="item" onclick={() => copyText(parts.name)}>
            <Icon name="copy" size={18} />
            <span class="two">{t('tagMenu.copyBare')}<span class="what">{parts.name}</span></span>
          </button>
        {/if}
      </div>
    {/if}

    <div class="group">
      {@render item('close', t('tagMenu.removeFromSearch'), remove, 'danger')}
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 0;
  }
  .backdrop.dim {
    z-index: 60;
    background: rgb(17 17 27 / 0.5);
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  .menu {
    position: relative;
    z-index: 1;
    display: grid;
    padding: 4px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.55);
  }
  /* Phones: a sheet like the details panel - edge to edge along the bottom,
     a grab bar (drag down to close), room for the home bar. */
  .menu.sheet {
    position: fixed;
    z-index: 61;
    left: 0;
    right: 0;
    bottom: 0;
    max-height: var(--sheet-max-h);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 0 12px calc(30px + var(--safe-bottom));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  @keyframes rise {
    from {
      transform: translateY(100%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .backdrop.dim,
    .menu.sheet {
      animation: none;
    }
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
  .head {
    display: grid;
    gap: 1px;
    padding: 8px 12px;
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
  .excluded {
    color: var(--red);
  }
  .edit {
    display: grid;
    gap: 4px;
    padding: 0 8px 8px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .row :global(.wrap) {
    flex: 1;
    min-width: 0;
  }
  .or {
    padding-left: 48px;
    font-size: 12px;
    font-weight: 600;
    color: var(--mauve);
  }
  .neg.on {
    color: var(--red);
  }
  .group {
    display: grid;
    padding-top: 4px;
    margin-top: 4px;
    border-top: 1px solid var(--surface0);
  }
  .item {
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
  .item > :global(svg) {
    flex: none;
    color: var(--subtext);
  }
  @media (hover: hover) {
    .item:hover {
      background: var(--surface0);
    }
  }
  .item:focus-visible {
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
</style>
