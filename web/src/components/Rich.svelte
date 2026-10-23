<script lang="ts">
  // A translated text with <b>bold</b> and <c>code</c> parts (lib/i18n rich()),
  // shown as <strong> and <code> - no HTML from the language files is ever rendered.
  import { rich } from '../lib/i18n.svelte';

  let { text }: { text: string } = $props();
</script>

{#each rich(text) as part, i (i)}{#if part.as === 'b'}<strong>{part.text}</strong>{:else if part.as === 'c'}<code>{part.text}</code>{:else}{part.text}{/if}{/each}
