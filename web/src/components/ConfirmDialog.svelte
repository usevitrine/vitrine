<script lang="ts">
  import { overlayScroll } from '../lib/overlayScroll';
  import { px, settle, sizeOf } from '../lib/motion';
  // The confirmation before a destructive action (lib/confirm): what goes (a
  // red badge, or the files' pictures), a question, one line, Cancel and the
  // action. A card in the middle on wider screens, a sheet on phones. Escape or
  // a press outside cancels; Tab stays inside. With a word to type (typed), the
  // action waits until it's typed.
  import { backClose } from '../lib/backClose';
  import { tick } from 'svelte';
  import { cubicOut } from 'svelte/easing';
  import { confirmer } from '../lib/confirm.svelte';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { sheetDrag } from '../lib/sheetDrag';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n.svelte';

  const c = $derived(confirmer.current);
  const sheet = matchMedia('(max-width: 639px)').matches;
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

  let box = $state<HTMLElement>();
  let opener: HTMLElement | null = null;
  let typedText = $state('');
  const ready = $derived(!c?.typed || typedText.trim().toLowerCase() === c.typed.toLowerCase());

  // Opened: the focus goes in (on the action when it can be undone, else on
  // Cancel); closed: back where it was.
  $effect(() => {
    if (!c) return;
    opener = document.activeElement as HTMLElement | null;
    const undoable = c.undoable;
    typedText = '';
    tick().then(() => box?.querySelector<HTMLElement>(c.typed ? '.typed' : undoable ? '.go' : '.cancel')?.focus());
    return () => opener?.focus?.({ preventScroll: true });
  });

  // Keys go to the dialog only (caught before the page sees them: the viewer's
  // arrows, a menu's Escape). Enter and Space still press the focused button.
  $effect(() => {
    if (!c) return;
    window.addEventListener('keydown', keydown, true);
    return () => window.removeEventListener('keydown', keydown, true);
  });
  function keydown(e: KeyboardEvent) {
    if (!c) return;
    e.stopPropagation();
    if (e.key === 'Escape') {
      e.preventDefault();
      confirmer.answer(false);
    } else if (e.key === 'Tab') {
      const b = [...(box?.querySelectorAll<HTMLElement>('input, button:not(:disabled)') ?? [])];
      const i = b.indexOf(document.activeElement as HTMLElement);
      e.preventDefault();
      b[(i + (e.shiftKey ? b.length - 1 : 1)) % b.length]?.focus();
    }
  }

  function pop(node: Element, { out = false } = {}) {
    if (sheet) return out ? sheetOut(node) : sheetIn(node);
    if (reduced()) return { duration: 0 };
    return {
      duration: out ? 200 : 220,
      easing: cubicOut,
      // (going: it shrinks and sinks a little as it fades, as plain as coming)
      css: (t: number) => `opacity: ${t}; transform: translateY(${px(out ? (1 - t) * 8 : 0)}) scale(${settle(0.94 + 0.06 * t, sizeOf(node))})`,
    };
  }
</script>

{#if c}
  <div class="backdrop" role="presentation" in:shade out:shade={{ out: true }} onpointerdown={() => confirmer.answer(false)}></div>
  <div
    class="dialog" {@attach overlayScroll}
    class:sheet
    data-sheet={sheet ? '' : undefined}
    data-drag
    {@attach sheet && sheetDrag(() => confirmer.answer(false))}
    {@attach backClose(() => confirmer.answer(false))}
    bind:this={box}
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="confirm-title"
    aria-describedby={c.body ? 'confirm-body' : undefined}
    in:pop
    out:pop={{ out: true }}>
    {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
    {#if c.thumbs?.length}
      <div class="thumbs" aria-hidden="true" style:--n={Math.min(3, c.thumbs.length)}>
        {#each c.thumbs.slice(0, 3) as t, i (t)}
          <img src={t} alt="" style:--i={i} />
        {/each}
        <span class="badge small"><Icon name={c.icon ?? 'trash'} size={14} /></span>
      </div>
    {:else}
      <span class="badge" aria-hidden="true"><Icon name={c.icon ?? 'trash'} size={22} /></span>
    {/if}
    <h2 id="confirm-title">{c.title}</h2>
    {#if c.body}<p id="confirm-body">{c.body}</p>{/if}
    {#if c.typed}
      <label class="typed-label">
        <span>{t('confirm.typeToConfirm', { word: c.typed })}</span>
        <input
          class="typed"
          bind:value={typedText}
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          aria-label={t('confirm.typeToConfirm', { word: c.typed })}
          onkeydown={(e) => e.key === 'Enter' && ready && confirmer.answer(true)} />
      </label>
    {/if}
    <div class="buttons">
      <button class="quiet cancel" onclick={() => confirmer.answer(false)}>{t('common.cancel')}</button>
      <button class="go" disabled={!ready} onclick={() => confirmer.answer(true)}>{c.action}</button>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 70; /* over the viewer and the sheets */
    background: rgb(17 17 27 / 0.6);
  }
  .dialog {
    position: fixed;
    z-index: 71;
    /* In the middle of what can be seen (above the keyboard), placed by layout - whole pixels: a translate of -50%
       left it half a pixel off, and its text shifted as an animation ended. */
    inset: var(--view-top) 0 var(--kb, 0px);
    height: fit-content;
    margin: auto;
    translate: 0 0; /* (an identity: drawn the same at rest as while it animates in) */
    width: min(380px, calc(100vw - 32px));
    max-height: calc(var(--view-h) - 24px);
    overflow-y: auto;
    display: grid;
    justify-items: center;
    gap: 6px;
    padding: 24px 20px 18px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 18px;
    box-shadow: 0 24px 60px rgb(0 0 0 / 0.6);
    text-align: center;
  }
  /* Phones: a sheet along the bottom like the others (drag down to cancel). */
  .dialog.sheet {
    left: 0;
    right: 0;
    top: auto;
    bottom: var(--kb, 0px); /* (just above the on-screen keyboard: a word to type) */
    width: auto;
    height: auto;
    margin: 0;
    padding: 0 20px calc(26px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  .grab-zone {
    justify-self: stretch;
    display: grid;
    place-items: center;
    height: 22px;
    margin: 0 -20px 10px;
    touch-action: none;
    cursor: grab;
  }
  .grab-zone span {
    width: 38px;
    height: 5px;
    border-radius: 3px;
    background: var(--surface2);
  }
  .badge {
    display: grid;
    place-items: center;
    width: 52px;
    height: 52px;
    margin-bottom: 8px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--red) 16%, transparent);
    color: var(--red);
  }
  /* The files' pictures, fanned out, with a small badge on them. */
  .thumbs {
    position: relative;
    width: calc(64px + (var(--n, 1) - 1) * 18px);
    height: 64px;
    margin: 0 auto 10px;
  }
  .thumbs img {
    position: absolute;
    top: 0;
    left: calc(var(--i) * 18px);
    width: 64px;
    height: 64px;
    object-fit: cover;
    border-radius: 12px;
    border: 2px solid var(--mantle);
    background: var(--surface0);
    transform: rotate(calc((var(--i) - (var(--n) - 1) / 2) * 6deg));
  }
  .badge.small {
    position: absolute;
    right: -8px;
    bottom: -8px;
    width: 26px;
    height: 26px;
    margin: 0;
    background: var(--red);
    color: var(--crust);
    border: 2px solid var(--mantle);
  }
  h2 {
    margin: 0;
    font-size: 17px;
    font-weight: 700;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  p {
    margin: 0;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .buttons {
    justify-self: stretch;
    display: flex;
    gap: 8px;
    margin-top: 14px;
  }
  .buttons button {
    flex: 1;
  }
  .go {
    background: var(--red);
    color: var(--crust);
  }
  /* The word to type: its label above a full-width box. */
  .typed-label {
    justify-self: stretch;
    display: grid;
    gap: 6px;
    margin-top: 8px;
    text-align: left;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .typed {
    width: 100%;
  }
</style>
