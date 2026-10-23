<script lang="ts">
  // The short tour (lib/tour): one hint at a time beside the thing it's about, which is lit up (the rest of the page
  // dimmed around it, still usable). Hints whose thing isn't on screen are left out (an empty library has no file to
  // point at). Next / Done; Skip tour or Esc ends it - for good, on this device.
  import { onMount } from 'svelte';
  import { fly } from 'svelte/transition';
  import { cubicOut } from 'svelte/easing';
  import { tour } from '../lib/tour.svelte';
  import { reducedMotion } from '../lib/hero';
  import { t } from '../lib/i18n.svelte';

  const touch = matchMedia('(pointer: coarse)').matches;
  const all = [
    { sel: '.bar [role="combobox"]', title: t('tour.search.title'), text: t('tour.search.text') },
    { sel: 'button[aria-label="New download"]', title: t('tour.download.title'), text: t('tour.download.text') },
    { sel: 'button[aria-label^="Saved searches"]', title: t('tour.saved.title'), text: t('tour.saved.text') },
    { sel: '.tile[data-i="0"]', title: t('tour.view.title'), text: touch ? t('tour.view.touch') : t('tour.view.mouse') },
    { sel: '.menu-button', title: t('tour.menu.title'), text: t('tour.menu.text') },
  ];
  const visible = (sel: string) => {
    const el = document.querySelector<HTMLElement>(sel);
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < innerHeight ? el : null;
  };
  // (the hints there are, decided when it opens - the page has settled by then)
  let hints = $state<typeof all>([]);
  let i = $state(0);
  let rect = $state<DOMRect | null>(null);
  let cardH = $state(160);

  onMount(() => {
    const start = setTimeout(() => {
      hints = all.filter((h) => visible(h.sel));
      if (!hints.length) tour.end();
    }, 600);
    // Where the current thing is, every frame (the page scrolls, the top bar slides).
    let raf = 0;
    const follow = () => {
      const h = hints[i];
      const el = h && document.querySelector<HTMLElement>(h.sel);
      rect = el ? el.getBoundingClientRect() : null;
      raf = requestAnimationFrame(follow);
    };
    raf = requestAnimationFrame(follow);
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        tour.end();
      }
    };
    window.addEventListener('keydown', key, true);
    return () => {
      clearTimeout(start);
      cancelAnimationFrame(raf);
      window.removeEventListener('keydown', key, true);
    };
  });

  function next() {
    if (i < hints.length - 1) i++;
    else tour.end();
  }

  const PAD = 6;
  const W = 320;
  // The card: under the thing when there's room, else above it; inside the window.
  let place = $derived.by(() => {
    if (!rect) return null;
    const w = Math.min(W, innerWidth - 24);
    const left = Math.min(Math.max(12, rect.left + rect.width / 2 - w / 2), innerWidth - w - 12);
    const below = rect.bottom + PAD + 12;
    const top = below + cardH < innerHeight - 12 ? below : Math.max(12, rect.top - PAD - 12 - cardH);
    return { left, top, w };
  });
</script>

{#if hints.length && rect && place}
  {@const h = hints[i]}
  <!-- The thing, lit up: a frame around it whose shadow dims the rest. -->
  <div
    class="spot"
    aria-hidden="true"
    style:left="{rect.left - PAD}px"
    style:top="{rect.top - PAD}px"
    style:width="{rect.width + PAD * 2}px"
    style:height="{rect.height + PAD * 2}px">
  </div>
  {#key i}
    <div
      class="hint-card"
      role="dialog"
      aria-label={h.title}
      bind:offsetHeight={cardH}
      style:left="{place.left}px"
      style:top="{place.top}px"
      style:width="{place.w}px"
      in:fly={{ y: 8, duration: reducedMotion() ? 0 : 220, easing: cubicOut }}>
      <p class="step">{t('tour.of', { n: i + 1, total: hints.length })}</p>
      <h2>{h.title}</h2>
      <p class="text">{h.text}</p>
      <div class="buttons">
        <button class="quiet" onclick={() => tour.end()}>{t('tour.skip')}</button>
        <button onclick={next}>{i < hints.length - 1 ? t('tour.next') : t('tour.done')}</button>
      </div>
    </div>
  {/key}
{/if}

<style>
  .spot {
    position: fixed;
    z-index: 95;
    border-radius: 12px;
    box-shadow:
      0 0 0 2px var(--mauve),
      0 0 0 9999px rgb(17 17 27 / 0.55);
    pointer-events: none;
    transition:
      left 0.25s cubic-bezier(0.2, 0.8, 0.2, 1),
      top 0.25s cubic-bezier(0.2, 0.8, 0.2, 1),
      width 0.25s cubic-bezier(0.2, 0.8, 0.2, 1),
      height 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .hint-card {
    position: fixed;
    z-index: 96;
    display: grid;
    gap: 6px;
    padding: 14px 14px 12px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.5);
  }
  .step {
    margin: 0;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  h2 {
    margin: 0;
    font-size: 16px;
  }
  .text {
    margin: 0;
    line-height: 1.45;
  }
  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 6px;
  }
  @media (prefers-reduced-motion: reduce) {
    .spot {
      transition: none;
    }
  }
</style>
