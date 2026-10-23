<script lang="ts">
  // A site's gallery being browsed (Comics > Browse): its details, laid out like a comic's (ComicPanel) in the same
  // details panel - its cover and titles, a facts card (its category a search), where it is (its gallery page), its
  // tags (the same Tags section: a tap opens a tag's menu - search it here, a new download - nothing to edit), and its
  // pages' thumbnails (the site's small ones: nothing against its image limit). The page gives the panel's frame.
  import { tick } from 'svelte';
  import { api, type BrowseGallery, type BrowseThumb, type FileTag } from '../lib/api';
  import { siteLabel } from '../lib/sites.svelte';
  import { namespaces } from '../lib/namespaces.svelte';
  import { copyText } from '../lib/clipboard';
  import TagsSection from './TagsSection.svelte';
  import Icon from './Icon.svelte';
  import SiteIcon from './SiteIcon.svelte';
  import '../lib/details.css';
  import { t, num } from '../lib/i18n.svelte';
  import { when, fullDate, host } from '../lib/when';

  let {
    gallery,
    site,
    image,
    onsearch,
  }: {
    gallery: BrowseGallery;
    site: string;
    /** A picture of the site's, through the server. */
    image: (u?: string) => string;
    onsearch: (q: string) => void;
  } = $props();

  // Its tags as the library keeps a downloaded comic's (the server gives each the namespace a download would), each
  // from this site.
  $effect(() => namespaces.addComics(gallery.tags.map((x) => x.ns ?? '')));
  // (once each: a repeated tag would stop the list - and the whole panel - from showing)
  const tags = $derived<FileTag[]>(
    [...new Set(gallery.tags.map((x) => (x.ns ? `${x.ns}:${x.name}` : x.name)))].map((tag) => ({ tag, origins: [site] })),
  );
  const languages = $derived(
    gallery.tags.filter((x) => x.ns === 'language' && x.name !== 'translated' && x.name !== 'rewrite').map((x) => x.name.replace(/_/g, ' ')),
  );

  // ---- its pages' thumbnails, a page of them at a time
  let thumbs = $state<BrowseThumb[]>([]);
  let pages = $state(0);
  let next = $state(0);
  let busy = $state(false);
  let error = $state('');
  let loadedFor = 0;
  async function more() {
    const g = gallery;
    if (busy) return;
    busy = true;
    error = '';
    try {
      const res = await api.browseThumbs(site, g.id, g.token, next);
      if (g.id !== gallery.id) return;
      thumbs = [...thumbs, ...res.thumbs];
      pages = res.pages;
      next++;
    } catch (e) {
      if (g.id === gallery.id) error = (e as Error).message;
    } finally {
      busy = false;
    }
  }
  $effect(() => {
    if (gallery.id !== loadedFor) {
      loadedFor = gallery.id;
      thumbs = [];
      pages = 0;
      next = 0;
      more();
    }
  });
  // The pages: one row to swipe through (a carousel), each thumbnail this tall; the next ones load as its end comes
  // near. With a mouse: arrows at its ends, and the wheel moves it sideways (until an end).
  const THUMB_H = 150;
  let strip = $state<HTMLElement>();
  let canBack = $state(false);
  let canFwd = $state(false);
  function onStrip() {
    const el = strip;
    if (!el) return;
    canBack = el.scrollLeft > 4;
    canFwd = el.scrollLeft + el.clientWidth < el.scrollWidth - 4;
    if (!busy && next < pages && el.scrollLeft + el.clientWidth > el.scrollWidth - 2 * THUMB_H) more();
  }
  $effect(() => {
    void thumbs.length;
    void busy;
    tick().then(onStrip); // (new thumbnails: the arrows - and maybe more, when they don't fill the row yet)
  });
  function step(dir: number) {
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    strip?.scrollBy({ left: dir * strip.clientWidth * 0.8, behavior: reduced ? 'auto' : 'smooth' });
  }
  function wheel(el: HTMLElement) {
    const on = (e: WheelEvent) => {
      if (Math.abs(e.deltaX) >= Math.abs(e.deltaY)) return; // (already sideways: a trackpad)
      const room = e.deltaY > 0 ? el.scrollWidth - el.clientWidth - el.scrollLeft : el.scrollLeft;
      if (room <= 1) return; // (at an end: the panel scrolls on)
      e.preventDefault();
      el.scrollLeft += e.deltaY;
    };
    el.addEventListener('wheel', on, { passive: false });
    return () => el.removeEventListener('wheel', on);
  }

</script>

<div class="details">
  <!-- Its cover and titles. -->
  <div class="top">
    {#if gallery.cover}
      <div class="cover-col"><span class="cover"><img src={image(gallery.cover)} alt="" /></span></div>
    {/if}
    <div class="titles">
      <h3 class="title">{gallery.title}</h3>
      {#if gallery.title_alt}<p class="alt">{gallery.title_alt}</p>{/if}
    </div>
  </div>

  <dl class="facts">
    <div>
      <dt>{t('filter.pages.label')}</dt>
      <dd>{num(gallery.pages)}</dd>
    </div>
    {#if gallery.category}
      <div>
        <dt>{t('filter.category.label')}</dt>
        <dd>
          <button class="link" onclick={() => onsearch('category:' + gallery.category!.toLowerCase().replace(/ /g, '_'))} title={t('browse.searchCategory')}
            >{gallery.category}</button>
        </dd>
      </div>
    {/if}
    {#if languages.length}
      <div>
        <dt>{t('filter.language.label')}</dt>
        <dd class="cap">{languages.join(', ')}</dd>
      </div>
    {/if}
    {#if gallery.posted}
      <div>
        <dt>{t('filter.uploaded.label')}</dt>
        <dd title={fullDate(gallery.posted)}>{when(gallery.posted)}</dd>
      </div>
    {/if}
    {#if gallery.rating}
      <div>
        <dt>{t('browse.rating')}</dt>
        <dd class="rating"><Icon name="star" size={14} /> {gallery.rating.toFixed(1)}</dd>
      </div>
    {/if}
    {#if gallery.uploader}
      <div>
        <dt>{t('browse.uploader')}</dt>
        <dd>{gallery.uploader}</dd>
      </div>
    {/if}
  </dl>

  <section aria-label={t('details.from')}>
    <div class="head"><h3>{t('details.from')}</h3></div>
    <ul class="rows">
      <li class="row">
        <SiteIcon host={host(gallery.url)} />
        <span class="what"><span class="q">{t('comics.galleryPage')}</span><span class="sub">{siteLabel(site)}</span></span>
        <a class="icon act" href={gallery.url} target="_blank" rel="noreferrer noopener" aria-label={t('comics.onSite')} title={t('details.open')}><Icon name="external" size={17} /></a>
        <button class="icon act" aria-label={t('comics.copyLink')} title={t('details.copyLink')} onclick={() => copyText(gallery.url, t('comics.what.link'))}><Icon name="copy" size={17} /></button>
      </li>
    </ul>
  </section>

  <TagsSection item={gallery.id} {tags} {onsearch} reload={() => {}} />

  <section class="pages" aria-label={t('browse.thumbs')}>
    <div class="head"><h3>{t('browse.thumbs')}</h3></div>
    <div class="carousel">
    <div class="thumbs" bind:this={strip} onscroll={onStrip} {@attach wheel} role="list">
      {#each thumbs as th (th.page)}
        {@const k = THUMB_H / th.h}
        <span class="thumb" style:width="{Math.round(th.w * k)}px" style:height="{THUMB_H}px" role="listitem" aria-label={t('browse.page', { n: th.page + 1 })}>
          <span
            class="sprite"
            style:width="{th.w}px"
            style:height="{th.h}px"
            style:background-image="url({image(th.url)})"
            style:background-position="-{th.x}px -{th.y}px"
            style:transform="scale({k})"></span>
          <span class="num">{th.page + 1}</span>
        </span>
      {/each}
      {#if busy}
        {#each { length: thumbs.length ? 3 : 6 } as _, k (k)}<span class="thumb placeholder" style:width="{Math.round(THUMB_H * 0.7)}px" style:height="{THUMB_H}px"></span>{/each}
      {/if}
    </div>
    {#if canBack}<button class="icon nav prev" aria-label={t('browse.prevThumbs')} title={t('browse.prevThumbs')} onclick={() => step(-1)}><Icon name="left" size={18} /></button>{/if}
    {#if canFwd}<button class="icon nav next" aria-label={t('browse.nextThumbs')} title={t('browse.nextThumbs')} onclick={() => step(1)}><Icon name="right" size={18} /></button>{/if}
    </div>
    {#if error}<p class="error small">{error}</p>{/if}
  </section>
</div>

<style>
  .top {
    display: flex;
    gap: 14px;
    align-items: flex-start;
  }
  .cover-col {
    flex: none;
    width: 92px;
  }
  .cover {
    display: block;
    overflow: hidden;
    border-radius: 10px;
    background: var(--surface0);
    box-shadow: 0 6px 18px rgb(0 0 0 / 0.4);
  }
  .cover img {
    display: block;
    width: 100%;
    aspect-ratio: 0.7;
    object-fit: cover;
  }
  .titles {
    min-width: 0;
    display: grid;
    gap: 6px;
  }
  .details .title {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    letter-spacing: 0;
    text-transform: none;
    color: var(--text);
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  .alt {
    margin: 0;
    font-size: var(--font-small);
    color: var(--subtext);
    overflow-wrap: anywhere;
  }
  .cap {
    text-transform: capitalize;
  }
  .rating {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .link {
    min-height: 0;
    height: auto;
    padding: 0;
    background: none;
    border: 0;
    color: var(--mauve);
    font: inherit;
    font-weight: 600;
  }
  @media (hover: hover) {
    .link:hover {
      background: none;
      text-decoration: underline;
    }
  }
  /* (never wider than the panel: the row scrolls inside it) */
  .pages {
    min-width: 0;
  }
  .carousel {
    position: relative;
    min-width: 0;
    margin: 0 -2px;
  }
  .thumbs {
    display: flex;
    gap: 8px;
    padding: 2px;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scroll-snap-type: x proximity;
    scrollbar-width: none; /* (the arrows and the swipe say there's more) */
  }
  .thumbs::-webkit-scrollbar {
    display: none;
  }
  .thumb {
    position: relative;
    flex: none;
    scroll-snap-align: start;
    overflow: hidden;
    border-radius: 8px;
    background: var(--surface0);
  }
  .thumb.placeholder {
    animation: pulse 1.2s ease-in-out infinite alternate;
  }
  @keyframes pulse {
    to {
      background: var(--surface1);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .thumb.placeholder {
      animation: none;
    }
  }
  .sprite {
    position: absolute;
    left: 0;
    top: 0;
    transform-origin: 0 0;
    background-repeat: no-repeat;
  }
  .num {
    position: absolute;
    right: 4px;
    bottom: 4px;
    padding: 0 6px;
    border-radius: 999px;
    background: rgb(17 17 27 / 0.75);
    font-size: 11px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  /* Arrows at the ends (with a mouse; a finger swipes). */
  .nav {
    position: absolute;
    top: 50%;
    translate: 0 -50%;
    width: 34px;
    height: 34px;
    min-height: 0;
    border-radius: 999px;
    background: rgb(17 17 27 / 0.82);
    border: 1px solid var(--surface1);
    box-shadow: 0 4px 14px rgb(0 0 0 / 0.45);
  }
  .prev {
    left: 6px;
  }
  .next {
    right: 6px;
  }
  @media (hover: none) {
    .nav {
      display: none;
    }
  }
</style>
