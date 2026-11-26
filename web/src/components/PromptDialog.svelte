<script lang="ts">
  // Asks for a line of text (lib/prompt): a box, Cancel and the action (Enter does it).
  import Modal from './Modal.svelte';
  import { prompt } from '../lib/prompt.svelte';
  import { t } from '../lib/i18n.svelte';

  let draft = $state('');
  $effect(() => {
    draft = prompt.current?.value ?? '';
  });
  function select(node: HTMLInputElement) {
    node.focus();
    node.select();
  }
</script>

{#if prompt.current}
  {@const p = prompt.current}
  <Modal title={p.title} width={400} onclose={() => prompt.answer(null)}>
    <form
      onsubmit={(e) => {
        e.preventDefault();
        if (draft.trim()) prompt.answer(draft);
      }}>
      <input bind:value={draft} aria-label={p.label} placeholder={p.placeholder ?? ''} maxlength="200" use:select />
      <div class="foot">
        <span class="spacer"></span>
        <button type="button" class="quiet" onclick={() => prompt.answer(null)}>{t('common.cancel')}</button>
        <button type="submit" disabled={!draft.trim()}>{p.action}</button>
      </div>
    </form>
  </Modal>
{/if}

<style>
  form {
    display: grid;
    gap: 12px;
  }
  input {
    width: 100%;
  }
</style>
