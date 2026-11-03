<script lang="ts">
  // The top bar's wrench: there only while a fix from an update needs the user or is working (lib/repairs). A press
  // opens the fixes: what each does with Run and Skip, how far it is, and OK once it finished - that takes it away, and
  // the last one takes the button with it. Its ring shows how far the running ones are; a dot says something's waiting.
  import { backClose } from '../lib/backClose';
  import { grow, rowIn, rowOut, dismiss } from '../lib/popover';
  import { nudge, reveal } from '../lib/motion';
  import { flip } from 'svelte/animate';
  import { repairs } from '../lib/repairs.svelte';
  import type { Repair } from '../lib/api';
  import { repairName, repairWhat } from '../lib/repairWords';
  import { t, num } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';

  let open = $state(false);
  let button = $state<HTMLButtonElement>();
  let panel = $state<HTMLElement>();

  const waiting = $derived(repairs.list.some((r) => r.state !== 'running'));
  const failed = $derived(repairs.list.some((r) => r.state === 'failed'));
  const finished = $derived(repairs.list.length > 0 && repairs.list.every((r) => r.state === 'done'));

  $effect(() => {
    if (open) return dismiss(() => panel, () => button, () => (open = false));
  });
  // (the last one gone: nothing left to show)
  $effect(() => {
    if (!repairs.list.length) open = false;
  });

  // A fix holding off after a restart counts down, while the panel's open.
  let now = $state(Date.now());
  $effect(() => {
    if (!open || !repairs.list.some((r) => r.waiting)) return;
    now = Date.now();
    const tick = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(tick);
  });
  function waitWords(r: Repair): string {
    const left = Math.ceil((repairs.at + (r.resume_in ?? 0) * 1000 - Math.max(now, repairs.at)) / 1000);
    if (left <= 0) return t('repairs.waitingNow');
    return t('repairs.waiting', { time: `${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}` });
  }

  const R = 13;
  const C = 2 * Math.PI * R;

  function detail(r: Repair): string {
    if (r.state === 'done') return r.id === 'video_shapes' ? t('repairs.videoShapes.done', { n: r.done, changed: r.changed }) : t('repairs.generalTags.done', { n: r.changed });
    return t(r.id === 'video_shapes' ? 'repairs.videoShapes.count' : 'repairs.generalTags.count', { n: r.total });
  }
</script>

{#if repairs.list.length}
  <div class="repairs-wrap" in:nudge out:nudge>
    <button
      bind:this={button}
      class="icon repairs"
      class:finished
      aria-label={t('repairs.label')}
      title={t('repairs.label')}
      aria-haspopup="dialog"
      aria-expanded={open}
      onclick={() => (open = !open)}>
      {#if repairs.progress !== null}
        <svg class="ring" viewBox="0 0 30 30" aria-hidden="true">
          <circle cx="15" cy="15" r={R} />
          <circle class="on" cx="15" cy="15" r={R} stroke-dasharray={C} stroke-dashoffset={C * (1 - repairs.progress)} />
        </svg>
      {/if}
      <Icon name={finished ? 'check' : 'wrench'} size={finished ? 19 : 16} />
      {#if waiting && !finished}<span class="dot" class:alarm={failed} aria-hidden="true"></span>{/if}
    </button>

    {#if open}
      <div class="panel" bind:this={panel} role="dialog" aria-label={t('repairs.label')} {@attach backClose(() => (open = false))} in:grow out:grow={{ out: true }}>
        <header><h2>{t('repairs.label')}</h2></header>
        <ul>
          {#each repairs.list as r, i (r.id)}
            <li class="fix {r.state}" animate:flip={{ duration: 220 }} in:rowIn={{ i }} out:rowOut>
              <div class="head">
                <h3>{repairName(r.id)}</h3>
                {#if r.state === 'done'}<span class="badge" aria-hidden="true"><Icon name="check" size={15} /></span>{/if}
                {#if r.state === 'failed'}<span class="badge" aria-hidden="true"><Icon name="alert" size={15} /></span>{/if}
              </div>
              {#if r.state === 'offered' || r.state === 'failed'}
                <p class="what" transition:reveal>{repairWhat(r.id)}</p>
              {/if}
              {#if r.state === 'running'}
                <div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax={r.total} aria-valuenow={r.done} transition:reveal>
                  <span style:width="{r.total ? (r.done / r.total) * 100 : 0}%"></span>
                </div>
                <p class="count">{t('repairs.progress', { done: num(r.done), total: num(r.total) })}</p>
                {#if r.waiting}<p class="count waits" transition:reveal>{waitWords(r)}</p>{/if}
              {:else}
                <p class="count">{detail(r)}</p>
              {/if}
              {#if r.state === 'failed'}<p class="error">{r.error}</p>{/if}
              {#if r.state === 'offered'}
                <div class="actions">
                  <button class="quiet" onclick={() => repairs.act(r.id, 'skip')}>{t('repairs.skip')}</button>
                  <button onclick={() => repairs.act(r.id, 'run')}>{t('repairs.run')}</button>
                </div>
              {:else if r.state === 'failed'}
                <div class="actions">
                  <button class="quiet" onclick={() => repairs.act(r.id, 'skip')}>{t('repairs.skip')}</button>
                  <button onclick={() => repairs.act(r.id, 'run')}>{t('repairs.tryAgain')}</button>
                </div>
              {:else if r.state === 'done'}
                <div class="actions">
                  <button onclick={() => repairs.act(r.id, 'ack')}>{t('repairs.ok')}</button>
                </div>
              {/if}
            </li>
          {/each}
        </ul>
      </div>
    {/if}
  </div>
{/if}

<style>
  .repairs-wrap {
    position: relative;
  }
  .repairs {
    position: relative;
    color: var(--text);
  }
  .repairs.finished {
    color: var(--green);
  }
  .ring {
    position: absolute;
    top: 50%;
    left: 50%;
    width: 30px;
    height: 30px;
    margin: -15px 0 0 -15px;
    transform: rotate(-90deg);
    fill: none;
    stroke: var(--surface1);
    stroke-width: 2.5;
    pointer-events: none;
  }
  .ring .on {
    stroke: var(--mauve);
    stroke-linecap: round;
    transition: stroke-dashoffset 0.4s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .dot {
    position: absolute;
    top: calc(50% - 12px);
    left: calc(50% + 5px);
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--mauve);
    box-shadow: 0 0 0 2px var(--mantle);
  }
  .dot.alarm {
    background: var(--red);
  }
  .panel {
    position: absolute;
    top: calc(100% + 8px);
    right: -4px;
    z-index: 60;
    width: min(380px, calc(100vw - 16px));
    max-height: min(70vh, 560px);
    display: flex;
    flex-direction: column;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 18px 44px rgb(0 0 0 / 0.55);
    transform-origin: calc(100% - 22px) 0;
    overflow: hidden;
  }
  /* Phones: across the top bar, the same margin on both sides (the bar holds it, not the button). */
  @media (max-width: 639px) {
    .repairs-wrap {
      position: static;
    }
    .panel {
      left: 8px;
      right: 8px;
      width: auto;
    }
  }
  header {
    display: flex;
    align-items: center;
    min-height: 44px;
    padding: 4px 14px 3px;
    border-bottom: 1px solid var(--surface0);
  }
  h2 {
    margin: 0;
    font-size: 14px;
    font-weight: 700;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 6px;
    display: grid;
    gap: 6px;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* (not .card: the app's has a 14 px gap, on top of the margins here - the user: "so much white space") */
  .fix {
    padding: 10px 12px;
    border: 1px solid var(--surface0);
    border-radius: 12px;
    background: var(--crust);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  h3 {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: var(--font);
    font-weight: 600;
    line-height: round(1.4em, 1px);
  }
  .badge {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    color: var(--green);
    background: color-mix(in srgb, var(--green) 18%, transparent);
  }
  .failed .badge {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 18%, transparent);
  }
  /* (whole-pixel lines: else the buttons below land between pixels, their words snapped off their middle) */
  p {
    margin: 6px 0 0;
    line-height: round(1.4em, 1px);
  }
  .what {
    color: var(--text);
  }
  .count {
    font-size: var(--font-small);
    color: var(--subtext);
  }
  .waits {
    color: var(--peach);
  }
  .error {
    font-size: var(--font-small);
    color: var(--red);
    overflow-wrap: anywhere;
  }
  .bar {
    height: 6px;
    margin-top: 10px;
    border-radius: 999px;
    background: var(--surface0);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    border-radius: inherit;
    background: var(--mauve);
    transition: width 0.4s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--gap);
    margin-top: 10px;
  }
  @media (prefers-reduced-motion: reduce) {
    .ring .on,
    .bar span {
      transition: none;
    }
  }
</style>
