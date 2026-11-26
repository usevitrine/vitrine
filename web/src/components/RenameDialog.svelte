<script lang="ts">
  // Renames a comic: its title in a box (Enter saves), and - once it has a
  // title of the user's own - "Original title" (an undo) to go back to the
  // site's. A small dialog over everything (Modal).
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    title,
    original,
    onsave,
    onclose,
  }: {
    title: string;
    /** The site's title, when the comic has one of the user's own (else no undo). */
    original?: string;
    /** The new title ("" = the original again). */
    onsave: (title: string) => void;
    onclose: () => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  let draft = $state(title);
  const changed = $derived(draft.trim() !== '' && draft.trim() !== title);

  function select(node: HTMLTextAreaElement) {
    node.focus();
    node.select();
  }
</script>

<Modal title={t('comics.rename')} {onclose}>
  <form
    onsubmit={(e) => {
      e.preventDefault();
      if (changed) onsave(draft.trim());
    }}>
    <textarea
      bind:value={draft}
      rows="3"
      aria-label={t('comics.titleLabel')}
      use:select
      onkeydown={(e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
          if (changed) onsave(draft.trim());
        } else if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          onclose();
        }
      }}></textarea>
    <div class="foot">
      {#if original}
        <button type="button" class="quiet" title={original} onclick={() => onsave('')}><Icon name="undo" size={16} /> {t('comics.originalTitle')}</button>
      {/if}
      <span class="spacer"></span>
      <button type="button" class="quiet" onclick={onclose}>{t('common.cancel')}</button>
      <button type="submit" disabled={!changed}>{t('common.save')}</button>
    </div>
  </form>
</Modal>

<style>
  form {
    display: grid;
    gap: 12px;
  }
  textarea {
    width: 100%;
    resize: vertical;
    font: inherit;
    font-weight: 600;
    line-height: 1.35;
  }
</style>
