<script lang="ts" module>
  import type { ComponentProps } from 'svelte';
  import type IconType from './Icon.svelte';

  export interface Section {
    id: string;
    label: string;
    icon: ComponentProps<typeof IconType>['name'];
    color: string; // a theme color's name (its tile)
    hint: string;
  }
</script>

<script lang="ts">
  // A screen in sections (like the iPhone's Settings), used by Settings and
  // Tags: on phones a list of sections, each opening as its own page with a
  // back link; on wide screens the list stays on the left and the section
  // shows beside it. Each section has its own address (/<screen>/<id>).
  import { overlayScroll } from '../lib/overlayScroll';
  import { onMount, type Snippet } from 'svelte';
  import { fly } from 'svelte/transition';
  import Icon from './Icon.svelte';
  import { router, type Screen } from '../lib/router.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    title,
    screen,
    sections,
    fallback,
    hintOf = (s: Section) => s.hint,
    foot,
    children,
  }: {
    title: string;
    screen: Screen;
    sections: readonly Section[];
    fallback: string; // the section shown on wide screens at the screen's own address
    hintOf?: (s: Section) => string;
    foot?: Snippet; // under the list
    children: Snippet<[string]>; // the section with that ID
  } = $props();

  const mq = matchMedia('(min-width: 900px)');
  let wide = $state(mq.matches);
  onMount(() => {
    const change = () => (wide = mq.matches);
    mq.addEventListener('change', change);
    return () => mq.removeEventListener('change', change);
  });
  const current = $derived(sections.some((s) => s.id === router.sub) ? router.sub : wide ? fallback : '');
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
</script>

<div class="page" {@attach overlayScroll}>
  <div class="layout" class:wide>
    {#if wide || !current}
      <nav class="side" aria-label={t('sections.nav', { title })} in:fly={{ x: reduced() || wide ? 0 : -24, duration: wide ? 0 : 200 }}>
        <h1>{title}</h1>
        <ul class="sections">
          {#each sections as s (s.id)}
            <li>
              <button class="section" class:on={current === s.id} aria-current={current === s.id ? 'page' : undefined} onclick={() => router.go(screen, s.id)}>
                <span class="tile" style:--c="var(--{s.color})"><Icon name={s.icon} size={18} /></span>
                <span class="what">
                  <span class="name">{s.label}</span>
                  <span class="sub">{hintOf(s)}</span>
                </span>
                {#if !wide}<Icon name="right" size={16} />{/if}
              </button>
            </li>
          {/each}
        </ul>
        {@render foot?.()}
      </nav>
    {/if}
    {#if current}
      {#key current}
        <div class="content" in:fly={{ x: reduced() ? 0 : wide ? 0 : 24, y: wide && !reduced() ? 6 : 0, duration: 200 }}>
          {#if !wide}
            <button class="back" onclick={() => router.go(screen)}><Icon name="left" size={18} /> {title}</button>
          {/if}
          {@render children(current)}
        </div>
      {/key}
    {/if}
  </div>
</div>

<style>
  /* Phones: the list, or one section. Wide: the list on the left (sticky), the
     section beside it. */
  .layout {
    max-width: 760px;
    margin: 0 auto;
  }
  .layout.wide {
    max-width: 1080px;
    display: grid;
    grid-template-columns: 280px minmax(0, 1fr);
    gap: 28px;
    align-items: start;
  }
  .side {
    display: grid;
    gap: 14px;
  }
  .layout.wide .side {
    position: sticky;
    top: 0;
  }
  .side h1 {
    margin: 0;
  }
  .sections {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    border-radius: 14px;
    border: 1px solid var(--surface0);
    background: var(--mantle);
    overflow: hidden;
  }
  .sections li + li {
    border-top: 1px solid var(--surface0);
  }
  .section {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 60px;
    padding: 8px 12px;
    border-radius: 0;
    background: none;
    color: var(--text);
    text-align: left;
    white-space: normal;
    font-weight: 400;
  }
  .section > :global(svg) {
    flex: none;
    color: var(--overlay1);
  }
  @media (hover: hover) {
    .section:hover {
      background: color-mix(in srgb, var(--surface0) 50%, transparent);
    }
  }
  .section.on {
    background: color-mix(in srgb, var(--mauve) 12%, transparent);
  }
  .section.on .name {
    color: var(--mauve);
  }
  /* A colored tile per section, like iOS Settings. */
  .tile {
    flex: none;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 9px;
    background: color-mix(in srgb, var(--c) 18%, transparent);
    color: var(--c);
  }
  .what {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 1px;
  }
  .name {
    font-weight: 600;
  }
  .sub {
    font-size: var(--font-small);
    color: var(--subtext);
  }
  .layout.wide .section {
    min-height: 52px;
  }
  .content {
    display: grid;
    gap: 16px;
    min-width: 0;
  }
  /* Wide: level with the list (below the title). */
  .layout.wide .content {
    margin-top: calc(1.2em + 22px);
  }
  .back {
    justify-self: start;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 0;
    height: 36px;
    padding: 0 10px 0 4px;
    margin: -4px 0 -6px -4px;
    background: none;
    color: var(--mauve);
    font-weight: 600;
  }
</style>
