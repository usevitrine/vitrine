<script lang="ts">
  // What's new after an update (lib/whatsNew): the new version's changes, "Don't show again" and Got it. A window in
  // the middle on wider screens, a sheet along the bottom on phones (drag it down to close - the user). Escape, Back
  // or a press outside close it too; closing marks the version seen.
  import { overlayScroll } from '../lib/overlayScroll';
  import { backClose } from '../lib/backClose';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { sheetDrag } from '../lib/sheetDrag';
  import { px, settle, sizeOf } from '../lib/motion';
  import { cubicOut } from 'svelte/easing';
  import { whatsNew } from '../lib/whatsNew.svelte';
  import ChangelogCard from './ChangelogCard.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n.svelte';

  const versions = $derived(whatsNew.shown);
  const sheet = matchMedia('(max-width: 639px)').matches;
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  let dontShow = $state(false);
  let box = $state<HTMLElement>();

  const close = () => whatsNew.close(dontShow);
  $effect(() => {
    if (versions) box?.focus({ preventScroll: true }); // (the window, not its button: no focus ring at first)
  });

  function pop(node: Element, { out = false } = {}) {
    if (sheet) return out ? sheetOut(node) : sheetIn(node);
    if (reduced()) return { duration: 0 };
    return {
      duration: out ? 200 : 220,
      easing: cubicOut,
      css: (k: number) => `opacity: ${k}; transform: translateY(${px(out ? (1 - k) * 8 : 0)}) scale(${settle(0.94 + 0.06 * k, sizeOf(node))})`,
    };
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (versions && e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      close();
    }
  }} />

{#if versions}
  <!-- (one version: named in the title; several - updates skipped - each under its own heading) -->
  {@const title = versions.length === 1 ? t('whatsNewDialog.title', { version: versions[0].title.split(' ')[0] }) : t('whatsNewDialog.titleMany')}
  <div class="backdrop" role="presentation" in:shade out:shade={{ out: true }} onpointerdown={close}></div>
  <div
    class="dialog"
    class:sheet
    role="dialog"
    aria-modal="true"
    aria-labelledby="whats-new-title"
    tabindex="-1"
    bind:this={box}
    data-sheet={sheet ? '' : undefined}
    {@attach sheet && sheetDrag(close)}
    {@attach backClose(close)}
    in:pop
    out:pop={{ out: true }}>
    {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
    <header class="head">
      <span class="badge" aria-hidden="true"><Icon name="sparkles" size={20} /></span>
      <h2 id="whats-new-title">{title}</h2>
    </header>
    <div class="body" {@attach overlayScroll}>
      <ChangelogCard given={versions} bare />
    </div>
    <footer>
      <label class="check">
        <input type="checkbox" bind:checked={dontShow} />
        <span>{t('whatsNewDialog.dontShow')}</span>
      </label>
      <button onclick={close}>{t('whatsNewDialog.ok')}</button>
    </footer>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 70;
    background: rgb(17 17 27 / 0.6);
  }
  .dialog {
    position: fixed;
    z-index: 71;
    /* In the middle of what can be seen, placed by layout (whole pixels - as ConfirmDialog). */
    inset: var(--view-top) 0 0;
    height: fit-content;
    margin: auto;
    translate: 0 0;
    width: min(520px, calc(100vw - 32px));
    max-height: calc(var(--view-h) - 48px);
    display: flex;
    flex-direction: column;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 18px;
    box-shadow: 0 24px 60px rgb(0 0 0 / 0.6);
    outline: none;
  }
  /* Phones: a sheet along the bottom (drag it down to close). */
  .dialog.sheet {
    inset: auto 0 0;
    width: auto;
    margin: 0;
    max-height: var(--sheet-max-h, 85vh);
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  .grab-zone {
    display: grid;
    place-items: center;
    height: 22px;
    flex: none;
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
    display: flex;
    align-items: center;
    gap: 12px;
    flex: none;
    padding: 20px 20px 12px;
  }
  .sheet .head {
    padding-top: 2px;
    touch-action: none; /* (dragged from here too) */
  }
  .badge {
    display: grid;
    place-items: center;
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--mauve) 16%, transparent);
    color: var(--mauve);
  }
  h2 {
    margin: 0;
    font-size: 18px;
    line-height: 1.3;
  }
  /* (several versions: their headings smaller than the window's title) */
  .body :global(h2) {
    font-size: 16px;
  }
  .body :global(section + section) {
    margin-top: 6px;
  }
  .body {
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 0 20px 4px;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 12px;
    flex: none;
    padding: 14px 20px 18px;
    border-top: 1px solid var(--surface0);
  }
  .sheet footer {
    padding-bottom: calc(18px + var(--bottom-room, 0px));
  }
  .check {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-right: auto;
    color: var(--subtext);
    font-size: var(--font-small);
    cursor: pointer;
  }
  footer button {
    min-width: 110px;
  }
</style>
