<script lang="ts">
  import { px, settle, sizeOf, onPixels } from '../lib/motion';
  // A small dialog over everything (Rename, Crop the cover): a dimmed backdrop, a card in the middle that pops in,
  // a heading with a close button; Escape, Back and a press outside close it. Its content brings its own buttons
  // (a ".foot" row is styled here: buttons on the right, a ".spacer" pushing some to the left).
  import type { Snippet } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { backClose } from '../lib/backClose';
  import { shade } from '../lib/sheet';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    title,
    width = 460,
    onclose,
    children,
  }: {
    title: string;
    width?: number; // (px, at most the screen less a margin)
    onclose: () => void;
    children: Snippet;
  } = $props();

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  function pop(node: Element, { out = false } = {}) {
    if (reduced()) return { duration: 0 };
    if (!out) onPixels(node);
    // (going: as plain as coming - it shrinks and sinks a little as it fades, not just a blink)
    return {
      duration: out ? 200 : 220,
      easing: cubicOut,
      css: (k: number) => `opacity: ${k}; transform: translateY(${px(out ? (1 - k) * 8 : 0)}) scale(${settle(0.94 + 0.06 * k, sizeOf(node))})`,
    };
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      onclose();
    }
  }} />

<div class="backdrop" role="presentation" in:shade out:shade={{ out: true }} onpointerdown={onclose}></div>
<div class="dialog" role="dialog" aria-modal="true" aria-label={title} style:--w="{width}px" {@attach backClose(onclose)} in:pop out:pop={{ out: true }}>
  <header>
    <h2>{title}</h2>
    <button type="button" class="icon" aria-label={t('sites.close')} onclick={onclose}><Icon name="close" /></button>
  </header>
  {@render children()}
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 80;
    background: rgb(17 17 27 / 0.7);
  }
  .dialog {
    position: fixed;
    z-index: 81;
    /* In the middle of what can be seen (above the keyboard), placed by layout - whole pixels: a translate of -50%
       left it half a pixel off, and its text shifted as an animation ended. */
    inset: var(--view-top) 0 var(--kb, 0px);
    height: fit-content;
    margin: auto;
    translate: 0 0; /* (an identity: drawn the same at rest as while it animates in) */
    width: min(var(--w), calc(100vw - 24px));
    max-height: calc(var(--view-h) - 24px);
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 12px 14px 14px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 16px;
    box-shadow: 0 20px 50px rgb(0 0 0 / 0.6);
  }
  header {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  h2 {
    flex: 1;
    margin: 0;
    font-size: 17px;
  }
  .dialog :global(.foot) {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .dialog :global(.foot .spacer) {
    flex: 1;
  }
</style>
