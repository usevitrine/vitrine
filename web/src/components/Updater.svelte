<script lang="ts">
  // The full-screen updater (lib/updater): the start-up splash's look - its background, its logo in its very place -
  // with the update's steps and a bar under the logo. At the end the steps fade and the page reloads behind the logo:
  // the splash takes over where it is and fades the app in, so the reload doesn't show.
  import { fade } from 'svelte/transition';
  import { updater, updateSteps } from '../lib/updater.svelte';
  import { pop, swap, reveal, reduced } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';

  const done = (i: number) => updater.progress >= 1 || updateSteps.indexOf(updater.step) > i;
  const current = (i: number) => !done(i) && updateSteps.indexOf(updater.step) === i;
  const focusOnMount = (el: HTMLElement) => el.focus();
</script>

<div class="updater" class:lifted={!!updater.error} role="dialog" aria-modal="true" aria-labelledby="updater-title" transition:fade={{ duration: reduced() ? 0 : 300 }}>
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

  <div class="panel" class:leaving={updater.leaving}>
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

<style>
  /* As the splash (index.html): the whole screen, its color, the logo in the middle (shifted as it is). */
  .updater {
    position: fixed;
    inset: 0;
    z-index: 300;
    display: grid;
    place-items: center;
    background: #1e1e2e;
    color: var(--text);
    /* (a short screen: what is under the logo scrolls - the failure's Close stays reachable) */
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .mark {
    border-radius: 14px;
    opacity: 0.9;
    transform: translateY(var(--splash-shift, 0px));
    transition: translate 0.45s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  /* A failure: no reload follows, so the logo can move - up, with the panel, so the reason and Close fit a phone. */
  .lifted .mark {
    translate: 0 calc(-1 * min(140px, 16vh));
  }
  .lifted .panel {
    translate: -50% calc(-1 * min(140px, 16vh));
  }
  /* Under the logo, without moving it. */
  .panel {
    position: absolute;
    top: calc(50% + 32px + 28px + var(--splash-shift, 0px));
    left: 50%;
    translate: -50% 0;
    width: min(340px, calc(100vw - 48px));
    display: grid;
    gap: 14px;
    padding-bottom: calc(24px + var(--bottom-room, 0px));
    transition:
      opacity 0.4s ease-out,
      translate 0.45s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .panel.leaving {
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
    .mark,
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
