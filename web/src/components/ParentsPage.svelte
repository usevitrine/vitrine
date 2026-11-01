<script lang="ts">
  // A menu's Parent tags page (the details' tag menu, a search chip's menu): every parent with a check when the tag is
  // one of its children - a tap puts it in or takes it out, the menu stays open - and a box for a new parent by name.
  // (2026-11-01, the user: "Tap menus on tag chips should have an option to add a tag to a parent/group tag", then
  // "You should be able to add tags to a parent in the search bar too".)
  import Icon from './Icon.svelte';
  import { nudge } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';
  import type { TagParents } from '../lib/parents.svelte';

  let { parents, tag, onback }: { parents: TagParents; tag: string; onback: () => void } = $props();
  let name = $state('');

  async function addNew(e: Event) {
    e.preventDefault();
    if (await parents.set(name, true)) name = '';
  }
</script>

<div class="head">
  <button class="back" aria-label={t('common.back')} onclick={onback}><Icon name="left" size={18} /></button>
  <span class="title">{t('tagMenu.parents')}</span>
</div>
<div class="group" role="group" aria-label={t('tagMenu.parents')}>
  {#each parents.all as p (p)}
    {@const on = parents.mine.has(p)}
    <button role="menuitemcheckbox" aria-checked={on} class:on disabled={parents.busy} onclick={() => parents.set(p, !on)}>
      <Icon name="stack" size={18} />
      <span class="grow">{p}</span>
      {#if on}<span class="tick" transition:nudge><Icon name="check" size={16} /></span>{/if}
    </button>
  {:else}
    <p class="note">{t('tagMenu.noParents')}</p>
  {/each}
</div>
<form class="group new" onsubmit={addNew}>
  <input bind:value={name} placeholder={t('tagMenu.newParent')} aria-label={t('tagMenu.newParent')} autocapitalize="off" autocomplete="off" spellcheck="false" />
  <button type="submit" class="add" disabled={parents.busy || !name.trim() || name.trim() === tag} aria-label={t('tagMenu.addParent')} title={t('tagMenu.addParent')}
    ><Icon name="plus" size={18} /></button>
</form>

<style>
  .head {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px 4px 4px;
  }
  .title {
    font-weight: 700;
  }
  .back {
    width: var(--control-h);
    min-height: var(--control-h);
    padding: 0;
    justify-content: center;
    flex: none;
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
  button.on,
  button.on > :global(svg) {
    color: var(--mauve);
  }
  .grow {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .tick {
    display: inline-flex;
    color: var(--mauve);
  }
  .note {
    margin: 0;
    padding: 6px 12px 8px;
    font-size: 13px;
    color: var(--subtext);
  }
  .new {
    display: flex;
    gap: 6px;
    padding: 6px 4px 2px;
  }
  .new input {
    flex: 1;
    min-width: 0;
  }
  .new .add {
    flex: none;
    width: var(--control-h);
    padding: 0;
    justify-content: center;
    background: var(--surface0);
  }
</style>
