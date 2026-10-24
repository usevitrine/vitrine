<script lang="ts">
  // The full-screen updater (lib/updater): the start-up splash's look - its background and logo - with the update's
  // steps and a bar under the logo, the two together in the middle of the screen. At the end the steps fade while the
  // logo glides to the splash's very place, and the page reloads behind it: the splash takes over where it is and
  // fades the app in, so the reload doesn't show.
  import { fade } from 'svelte/transition';
  import { updater, updateSteps } from '../lib/updater.svelte';
  import { pop, swap, reveal, reduced } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';

  const done = (i: number) => updater.progress >= 1 || updateSteps.indexOf(updater.step) > i;
  const current = (i: number) => !done(i) && updateSteps.indexOf(updater.step) === i;
  const focusOnMount = (el: HTMLElement) => el.focus();
  const GAP = 28; // (between the logo and the steps)
  let panelH = $state(0);
</script>

<div class="updater" role="dialog" aria-modal="true" aria-labelledby="updater-title" transition:fade={{ duration: reduced() ? 0 : 300 }}>
  <!-- The logo and the steps, in the middle; leaving, the steps fade and the group moves down by half their height (and
       the gap), which puts the logo in the middle - where the splash draws it (its shift too). -->
  <div class="group" class:leaving={updater.leaving} style:--gap="{GAP}px" style:--drop="{(panelH + GAP) / 2}px">
  <!-- (the splash's logo, as in index.html) -->
  <svg class="mark" width="64" height="64" viewBox="0 0 64 64" aria-hidden="true">
    <defs><linearGradient id="upd-g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#cba6f7" /><stop offset="1" stop-color="#f5c2e7" /></linearGradient></defs>
    <rect width="64" height="64" rx="14" fill="#1e1e2e" />
    <g transform="translate(0 -2)">
      <polygon points="11,14 23,14 32,40 32,54" fill="url(#upd-g)" /><polygon points="11,14 23,14 32,40 32,54" fill="#fff" opacity=".22" />
      <polygon points="11,14 32,54 21,40" fill="url(#upd-g)" /><polygon points="11,14 32,54 21,40" fill="#1e1e2e" opacity=".05" />
      <polygon points="41,14 53,14 32,54 32,40" fill="url(#upd-g)" /><polygon points="41,14 53,14 32,54 32,40" fill="#1e1e2e" opacity=".12" />
      <polygon points="53,14 43,40 32,54" fill="url(#upd-g)" /><polygon points="53,14 43,40 32,54" fill="#1e1e2e" opacity=".28" />
    </g>
  </svg>

  <div class="panel" bind:offsetHeight={panelH}>
    {#key !!updater.error}
      <h2 id="updater-title" in:swap>{updater.error ? t('updates.failedTitle') : t('updates.title', { version: updater.to })}</h2>
    {/key}
    <div class="bar" class:failed={!!updater.error} role="progressbar" aria-label={t('updates.title', { version: updater.to })} aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(updater.progress * 100)}>
      <span style:transform="scaleX({updater.progress})"></span>
    </div>
    <ol class="steps">
      {#each updateSteps as s, i (s)}
        <li class:done={done(i)} class:current={current(i)}>
          <span class="dot">
            {#if done(i)}<span class="ic" in:pop><Icon name="check" size={14} /></span>
            {:else if current(i) && !updater.error}<span class="spin" in:pop></span>
            {:else if current(i)}<span class="ic bad" in:pop><Icon name="close" size={14} /></span>{/if}
          </span>
          {t(`updates.steps.${s}`)}
        </li>
      {/each}
    </ol>
    {#if updater.error}
      <div class="failed-box" transition:reveal>
        <p class="error">{updater.error}</p>
        <p class="hint">{t('updates.failedHint')}</p>
        <button class="primary" onclick={() => updater.close()} {@attach focusOnMount}>{t('updates.close')}</button>
      </div>
    {:else}
      <p class="hint" transition:reveal>{t('updates.keepOpen')}</p>
    {/if}
  </div>
  </div>
</div>

<style>
  /* As the splash (index.html): the whole screen, its color. (margin: auto centers the group and still lets a short
     screen scroll to all of it - the failure's Close stays reachable.) */
  .updater {
    position: fixed;
    inset: 0;
    z-index: 300;
    display: flex;
    padding: 0 24px; /* (no top/bottom of its own: the middle stays the splash's middle) */
    background: #1e1e2e;
    color: var(--text);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .group {
    margin: auto;
    padding: calc(24px + var(--bottom-room, 0px)) 0; /* (the same above and below: clear of the home bar, still centered) */
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--gap);
    transition: translate 0.5s cubic-bezier(0.4, 0, 0.2, 1);
  }
  .group.leaving {
    translate: 0 calc(var(--drop) + var(--splash-shift, 0px));
  }
  .mark {
    flex: none;
    border-radius: 14px;
    opacity: 0.9;
  }
  .panel {
    width: min(340px, calc(100vw - 48px));
    display: grid;
    gap: 14px;
    transition: opacity 0.35s ease-out;
  }
  .leaving .panel {
    opacity: 0;
  }
  h2 {
    margin: 0;
    font-size: 17px;
    font-weight: 600;
    text-align: center;
  }
  .bar {
    height: 6px;
    border-radius: 3px;
    background: var(--surface0);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: linear-gradient(90deg, #cba6f7, #f5c2e7);
    transform-origin: left;
    transition: transform 0.25s linear;
  }
  .bar.failed span {
    background: var(--red);
  }
  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 9px;
  }
  .steps li {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--overlay1);
    transition: color 0.3s;
  }
  .steps li.current {
    color: var(--text);
  }
  .steps li.done {
    color: var(--subtext);
  }
  .dot {
    flex: none;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--surface0);
    transition: background-color 0.3s;
  }
  .done .dot {
    background: color-mix(in srgb, var(--green) 22%, transparent);
    color: var(--green);
  }
  .ic,
  .spin {
    display: grid;
    place-items: center;
  }
  .ic.bad {
    color: var(--red);
  }
  .spin {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 2px solid color-mix(in srgb, var(--text) 18%, transparent);
    border-top-color: var(--mauve);
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .hint {
    margin: 0;
    text-align: center;
  }
  .failed-box {
    display: grid;
    gap: 10px;
    justify-items: center;
    text-align: center;
  }
  .error {
    margin: 0;
    color: var(--red);
  }
  @media (prefers-reduced-motion: reduce) {
    .bar span,
    .group,
    .panel,
    .steps li,
    .dot {
      transition: none;
    }
    .spin {
      animation-duration: 2s;
    }
  }
</style>
