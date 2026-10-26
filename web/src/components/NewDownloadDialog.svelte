<script lang="ts">
  import { overlayScroll } from '../lib/overlayScroll';
  // A new download from the library (2026-10-22: the Library's download button), a page at a time: the library's
  // search or another one (only when the search has tags), the site (where the search's tags came from first), the
  // search on that site (the library's written the site's way: lib/siteSearch - an OR group on a site without an OR
  // search: one search for each combination), then once or a subscription (how often, its group). A card in the middle on wider screens, a sheet along the bottom on phones - both above the
  // on-screen keyboard (--kb).
  import { onMount, tick } from 'svelte';
  import { reveal } from '../lib/motion';
  import { api, type SubGroup } from '../lib/api';
  import { siteLabel, tagQuery, tagsQuery, canOr, sites as siteInfo } from '../lib/sites.svelte';
  import { searchTags, siteSearch, splitSearches, searchWords } from '../lib/siteSearch';
  import { parse, type Chip } from '../lib/query';
  import { tagOrigins, type SiteChoice } from '../lib/newDownload.svelte';
  import { siteOn } from '../lib/sections';
  import { intervals, DEFAULT_INTERVAL, cap, every } from '../lib/schedule';
  import { router } from '../lib/router.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { backClose } from '../lib/backClose';
  import { sheetDrag } from '../lib/sheetDrag';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { Pages } from '../lib/pages.svelte';
  import { t } from '../lib/i18n.svelte';
  import TagInput from './TagInput.svelte';
  import Dropdown from './Dropdown.svelte';
  import SiteIcon from './SiteIcon.svelte';
  import Icon from './Icon.svelte';

  let {
    query,
    fromTags = false,
    fallback = '',
    presetSite = '',
    presetText = '',
    label = '',
    ondone,
    onclose,
  }: {
    query: string; // a library search
    fromTags?: boolean; // tags picked (a tag's menu, the details): straight to the site
    fallback?: string; // offered first when the tags came from no site
    presetSite?: string; // a search on a site already (a Browse gallery's link): straight to "once or subscribe"
    presetText?: string;
    label?: string; // shown for it instead of the search (the gallery's title)
    ondone?: (subscribed: boolean) => void;
    onclose: () => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  const tags = searchTags(query); // (the search when the dialog opened)
  type Page = 'what' | 'site' | 'search' | 'how';
  // svelte-ignore state_referenced_locally
  let page = $state<Page>(presetSite ? 'how' : tags.length && !fromTags ? 'what' : 'site');
  let trail = $state<Page[]>([]); // the pages before this one (Back)
  // svelte-ignore state_referenced_locally
  let useSearch = $state(fromTags);

  // ---- the sites (those that download now) and their subscription groups
  let downloadSites = $state<string[]>([]);
  let groups = $state<SubGroup[]>([]);
  let origins = $state<SiteChoice[]>([]);
  let loaded = $state(false);
  onMount(async () => {
    siteInfo.load();
    try {
      const res = await api.downloads();
      await siteInfo.whenLoaded(); // (each site's OR search)
      downloadSites = res.sites.filter(siteOn);
      groups = res.groups ?? [];
      if (fromTags) origins = await tagOrigins(tags, (s) => downloadSites.includes(s));
    } catch (e) {
      toasts.error((e as Error).message);
    }
    loaded = true;
    focusFirst();
  });
  function lastSite(): string {
    try {
      return localStorage.getItem('vitrine.lastSite') ?? '';
    } catch {
      return '';
    }
  }
  // The search's: where its tags came from first (with how many files), then the file's own site; a new search: the
  // site used last first.
  const others = $derived(
    downloadSites
      .filter((s) => !(useSearch && origins.some((o) => o.site === s)))
      .sort((a, b) => {
        const first = useSearch ? fallback : lastSite();
        return Number(b === first) - Number(a === first);
      }),
  );

  // ---- moving between pages (lib/pages, like the other menus with pages)
  let panel = $state<HTMLElement>();
  const sheet = matchMedia('(max-width: 639px)').matches;
  const pages = new Pages((h) => {
    const cs = panel ? getComputedStyle(panel) : null;
    const room = cs ? parseFloat(cs.maxHeight) - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom) - 60 : Infinity;
    return Number.isFinite(room) && room > 0 ? Math.min(h, room) : h;
  });
  async function go(to: Page) {
    trail.push(page);
    await pages.go(true, () => (page = to));
    focusFirst();
  }
  async function back() {
    const to = trail.pop();
    if (!to) return;
    await pages.go(false, () => (page = to));
    focusFirst();
  }
  async function focusFirst() {
    await tick();
    const el = panel?.querySelector<HTMLElement>('[data-autofocus]');
    // (a text box: with a mouse, ready to type; on a touch screen its keyboard waits for a tap)
    if (el instanceof HTMLInputElement && matchMedia('(pointer: coarse)').matches) return;
    (el ?? panel)?.focus();
  }

  // ---- the steps
  async function pickWhat(search: boolean) {
    useSearch = search;
    if (search) origins = await tagOrigins(tags, (s) => downloadSites.includes(s));
    go('site');
  }
  // svelte-ignore state_referenced_locally
  let site = $state(presetSite);
  // svelte-ignore state_referenced_locally
  let text = $state(presetText);
  let searchBox = $state<HTMLInputElement>(); // (the suggestions sit under it, over the dialog)
  let left = $state<Chip[]>([]);
  let split = $state<Chip[]>([]); // (OR groups the site has no OR search for: a search for each combination)
  const searches = $derived(splitSearches(site, text, split));
  const ready = $derived(searches.every((q) => q !== ''));
  function pickSite(s: string) {
    site = s;
    if (useSearch) ({ query: text, left, split } = siteSearch(s, query));
    else {
      text = '';
      left = [];
      split = [];
    }
    // (several subscriptions from one search: together in a group named after it, to start with)
    formGroup = split.length ? 'new' : '';
    newGroupName = split.length ? searchWords(parse(query)).slice(0, 60) : '';
    go('search');
  }
  function toHow(e?: SubmitEvent) {
    e?.preventDefault();
    if (ready) go('how');
  }

  let repeat = $state('0'); // seconds between checks; '0' = download once
  let maxPosts = $state<number | undefined>(undefined);
  let formGroup = $state(''); // a group's ID, 'new' or '' (none)
  let newGroupName = $state('');
  let busy = $state(false);
  const siteGroups = $derived(groups.filter((g) => g.site === site));
  const ownFavs = $derived(/^fav:me$/i.test(text.trim())); // (its files become favorites here too, as on the Downloads page)
  const favSearch = $derived(/^fav:\S+$|^favorited_by=/i.test(text.trim()) || /[?&]page=favorites(&|&amp;)s=view/.test(text));
  const intervalOptions = $derived(intervals.map((v) => ({ value: String(v), label: cap(every(v)) })));
  function pickGroup(v: string) {
    formGroup = v;
    const g = groups.find((x) => String(x.id) === v);
    if (g?.interval) repeat = String(g.interval); // the group's usual schedule
  }

  async function start(e: SubmitEvent) {
    e.preventDefault();
    if (!ready || busy) return;
    busy = true;
    try {
      const q = searches.length > 1 ? t('newDl.searches', { n: searches.length }) : searches[0];
      if (repeat !== '0') {
        if (!favSearch && formGroup === 'new' && !newGroupName.trim()) throw new Error(t('downloads.nameGroupFirst'));
        const group = favSearch ? {} : formGroup === 'new' ? { group_name: newGroupName.trim() } : formGroup ? { group_id: +formGroup } : {};
        // (a split search: each combination its own subscription - a new group's name finds the group the first one made)
        for (const s of searches) await api.subscribe(site, s, +repeat, maxPosts ?? 0, group, ownFavs, label); // (label: a gallery's title - its name in the lists)
        const gName = favSearch ? t('menu.favorites') : formGroup === 'new' ? newGroupName.trim() : siteGroups.find((x) => String(x.id) === formGroup)?.name;
        toasts.show(gName ? t('downloads.subscribedIn', { query: label || q, name: gName, every: every(+repeat) }) : t('downloads.subscribed', { query: label || q, every: every(+repeat) }), {
          cat: 'downloadsStarted',
        });
      } else {
        for (const s of searches) await api.addDownload(site, s, maxPosts ?? 0, ownFavs, label);
        toasts.show(t('downloads.downloading', { query: label || q, site: siteLabel(site) }), { cat: 'downloadsStarted', kind: 'info' });
      }
      try {
        localStorage.setItem('vitrine.lastSite', site);
      } catch {
        /* not remembered - fine */
      }
      ondone?.(repeat !== '0');
      onclose();
    } catch (err) {
      toasts.error((err as Error).message);
    } finally {
      busy = false;
    }
  }

  const titles: Record<Page, () => string> = {
    what: () => t('tagMenu.newDownload'),
    site: () => t('newDl.site'),
    search: () => t('newDl.search', { site: siteLabel(site) }),
    how: () => t('newDl.how'),
  };

  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      onclose();
    }
  }} />

<div use:portal>
  <div class="backdrop" role="presentation" in:shade|global out:shade|global={{ out: true }} onpointerdown={onclose}></div>
  <div
    class="dialog"
    class:sheet
    data-sheet={sheet ? '' : undefined}
    bind:this={panel}
    role="dialog"
    aria-modal="true"
    aria-label={titles[page]()}
    tabindex="-1"
    onkeydown={(e) => {
      // (here first: from its search box too - an open suggestion list takes Escape before this)
      if (e.key === 'Escape' && !e.defaultPrevented) {
        e.preventDefault();
        onclose();
      }
    }}
    {@attach sheet && sheetDrag(onclose)}
    {@attach backClose(onclose)}
    in:sheetIn|global={{ on: sheet, desk: 'pop' }}
    out:sheetOut|global={{ on: sheet, desk: 'pop' }}>
    {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
    <div class="head">
      {#if trail.length && page !== 'what'}
        <!-- (its width slides, its gap with it: the title beside it glides instead of jumping) -->
        <span class="back-slot" transition:reveal={{ axis: 'x' }}
          ><button type="button" class="icon back" aria-label={t('common.back')} title={t('common.back')} onclick={back}><Icon name="left" size={18} /></button></span
        >
      {/if}
      <h2>{titles[page]()}</h2>
      <button type="button" class="icon" aria-label={t('sites.close')} title={t('sites.close')} onclick={onclose}><Icon name="close" /></button>
    </div>

    <div class="mpages" class:animating={pages.animating} {@attach pages.box} style:height={pages.height !== null ? `${pages.height}px` : undefined}>
      {#key pages.nav}
        <div class="mpage" {@attach pages.page} in:pages.pageIn out:pages.pageOut>
          {#if page === 'what'}
            <!-- The search in the bar, or a new one. -->
            <div class="items">
              <button class="item" onclick={() => pickWhat(true)} data-autofocus>
                <Icon name="search" size={18} />
                <span class="item-text"><span class="item-label">{t('newDl.thisSearch')}</span><span class="item-hint q" title={query}>{searchWords(parse(query))}</span></span>
                <Icon name="right" size={16} />
              </button>
              <button class="item" onclick={() => pickWhat(false)}>
                <Icon name="pencil" size={18} />
                <span class="item-text"><span class="item-label">{t('newDl.other')}</span><span class="item-hint">{t('newDl.otherHint')}</span></span>
                <Icon name="right" size={16} />
              </button>
            </div>
          {:else if page === 'site'}
            {#if fromTags}<p class="what-tags" title={query}>{searchWords(parse(query))}</p>{/if}
            {#if !loaded || !siteInfo.loaded}
              <p class="hint" transition:reveal>{t('common.loading')}</p>
            {:else if !downloadSites.length}
              <p class="hint" transition:reveal>{t('newDl.noSites')}</p>
              <button class="quiet" onclick={() => (router.go('settings', 'sites'), onclose())}>{t('downloads.manageSites')}</button>
            {:else}
              {#if useSearch && origins.length}
                <h3 transition:reveal>{t('sitePicker.from')}</h3>
                <div class="items">
                  {#each origins as c, k (c.site)}
                    <button class="item site" onclick={() => pickSite(c.site)} data-autofocus={k === 0 ? true : undefined}>
                      <SiteIcon host={siteInfo.hosts[c.site] ?? ''} />
                      <span class="item-text"><span class="item-label">{siteLabel(c.site)}</span></span>
                      <span class="meta">{c.tags < tags.length ? t('sitePicker.someTags', { count: c.tags, n: tags.length }) : t('common.files', { n: c.files })}</span>
                    </button>
                  {/each}
                </div>
              {/if}
              {#if others.length}
                {#if useSearch && origins.length}<h3 transition:reveal>{t('sitePicker.others')}</h3>{/if}
                <div class="items">
                  {#each others as s, k (s)}
                    <button class="item site" onclick={() => pickSite(s)} data-autofocus={k === 0 && !(useSearch && origins.length) ? true : undefined}>
                      <SiteIcon host={siteInfo.hosts[s] ?? ''} />
                      <span class="item-text"><span class="item-label">{siteLabel(s)}</span></span>
                    </button>
                  {/each}
                </div>
              {/if}
            {/if}
          {:else if page === 'search'}
            <!-- What to search for on the site (the library's search written its way, or a new one). -->
            <form class="form" onsubmit={toHow}>
              <TagInput
                anchor={sheet ? undefined : searchBox}
                above={sheet}
                bind:inputEl={searchBox}
                bind:value={text}
                multi
                comics={siteInfo.engines[site] === 'ehentai' ? site : undefined}
                sites={site}
                format={(tg) => tagQuery(site, tg)}
                anyOf={(tags) => (canOr(site, tags) ? tagsQuery(site, tags, true) : null)}
                label={t('downloads.searchOnSite')}
                placeholder={siteInfo.hints[site] ?? t('downloads.searchSite', { site: siteLabel(site) })}
                autofocus={!matchMedia('(pointer: coarse)').matches}
                onenter={() => toHow()} />
              {#if split.length}<p class="hint left" transition:reveal>{t('newDl.split', { site: siteLabel(site), n: searches.length })}</p>{/if}
              {#if left.length}<p class="hint left" transition:reveal>{t('newDl.leftOut', { what: searchWords(left) })}</p>{/if}
              <div class="foot">
                <button type="submit" disabled={!ready}>{t('newDl.next')} <Icon name="right" size={16} /></button>
              </div>
            </form>
          {:else}
            <!-- Once, or a subscription. -->
            <form class="form" onsubmit={start}>
              <p class="what" title={label || searches.join('\n')}>
                <SiteIcon host={siteInfo.hosts[site] ?? ''} /> <span>{label || (searches.length > 1 ? t('newDl.searches', { n: searches.length }) : searches[0])}</span>
              </p>
              {#if searches.length > 1}
                <ul class="split-list" {@attach overlayScroll} transition:reveal>
                  {#each searches as s (s)}<li>{s}</li>{/each}
                </ul>
              {/if}
              <div class="segmented" role="tablist" aria-label={t('newDl.how')}>
                <button type="button" role="tab" aria-selected={repeat === '0'} onclick={() => (repeat = '0')} data-autofocus><Icon name="download" size={16} /> {t('downloads.once')}</button>
                <button type="button" role="tab" aria-selected={repeat !== '0'} onclick={() => repeat === '0' && (repeat = String(DEFAULT_INTERVAL))}><Icon name="bell" size={16} /> {t('downloads.subscribe')}</button>
              </div>
              <div class="rows">
                {#if repeat !== '0'}
                  <div class="row" transition:reveal>
                    <span class="row-label">{t('downloads.repeat')}</span>
                    <Dropdown floating value={repeat} options={intervalOptions} onchange={(v) => (repeat = v)} label={t('downloads.repeat')} heading={t('downloads.repeat')} />
                  </div>
                  {#if !favSearch}
                    <div class="row" transition:reveal>
                      <span class="row-label">{t('downloads.group')}</span>
                      <Dropdown
                        floating
                        value={formGroup}
                        options={[{ value: '', label: t('downloads.noGroup') }, ...siteGroups.map((g) => ({ value: String(g.id), label: g.name })), { value: 'new', label: t('downloads.newGroup') }]}
                        onchange={pickGroup}
                        label={t('downloads.group')}
                        heading={t('downloads.addToGroup')} />
                    </div>
                    {#if formGroup === 'new'}
                      <input class="new-group" bind:value={newGroupName} placeholder={t('downloads.newGroupExample')} aria-label={t('downloads.newGroupLabel')} maxlength="60"  transition:reveal/>
                    {/if}
                  {/if}
                {/if}
                <label class="row">
                  <span class="row-label">{repeat === '0' ? t('downloads.files') : t('downloads.firstCheck')}</span>
                  <input
                    class="max"
                    bind:value={maxPosts}
                    type="number"
                    min="1"
                    inputmode="numeric"
                    placeholder={t('downloads.all')}
                    aria-label={repeat === '0' ? t('downloads.maxFiles') : t('downloads.maxFirst')} />
                </label>
              </div>
              <p class="hint">
                {#if repeat === '0'}{maxPosts ? t('downloads.noteOnce', { n: Number(maxPosts) }) : t('newDl.onceHint')}
                {:else}{t('downloads.noteSub', { every: every(+repeat), first: maxPosts ? t('downloads.newestN', { n: Number(maxPosts) }) : t('downloads.everyPost') })}.{/if}
              </p>
              <div class="foot">
                <button type="submit" class="go" disabled={busy}>
                  {#if repeat === '0'}<Icon name="download" size={18} /> {t('backups.download')}{:else}<Icon name="bell" size={18} /> {t('downloads.subscribe')}{/if}
                </button>
              </div>
            </form>
          {/if}
        </div>
      {/key}
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 80;
    background: rgb(17 17 27 / 0.6);
  }
  /* In the middle of what can be seen (above the keyboard: app.css). */
  .dialog {
    position: fixed;
    z-index: 81;
    /* In the middle of what can be seen (above the keyboard), placed by layout - whole pixels: a translate of -50%
       left it half a pixel off, and its text shifted as an animation ended. */
    inset: var(--view-top) 0 var(--kb, 0px);
    height: fit-content;
    margin: auto;
    translate: 0 0; /* (an identity: drawn the same at rest as while it animates in) */
    width: min(460px, calc(100vw - 24px));
    max-height: calc(var(--view-h) - 24px);
    overflow: hidden auto; /* (never sideways: a long search wraps or ends in …) */
    overscroll-behavior: contain;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-content: start;
    gap: 10px;
    padding: 12px 14px 16px;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 16px;
    box-shadow: 0 20px 50px rgb(0 0 0 / 0.6);
    outline: none;
  }
  /* Phones: a sheet along the bottom like the other menus - just above the keyboard when it's open. */
  .dialog.sheet {
    left: 0;
    right: 0;
    top: auto;
    bottom: var(--kb, 0px);
    width: auto;
    height: auto;
    margin: 0;
    max-height: calc(var(--view-h) - 12px);
    padding: 0 14px calc(18px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  .grab-zone {
    display: grid;
    place-items: center;
    height: 22px;
    margin: 0 -14px -6px;
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
    min-height: 36px;
  }
  /* Back: its room (and the space after it) in a slot that slides open and shut. */
  .back-slot {
    flex: none;
    display: flex;
    margin-left: -6px;
    padding-right: 6px;
  }
  .head > :last-child {
    margin-left: 6px;
  }
  h2 {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: 17px;
    font-weight: 700;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  h3 {
    margin: 4px 0 2px 2px;
    font-size: var(--font-small);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--overlay1);
  }
  .mpage {
    gap: 10px;
    align-content: start;
  }
  .items {
    display: grid;
    gap: 2px;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 48px;
    padding: 7px 10px;
    border-radius: 10px;
    background: none;
    color: var(--subtext);
    text-align: left;
    font-weight: 500;
  }
  @media (hover: hover) {
    .item:hover {
      background: var(--surface0);
      filter: none;
    }
  }
  .item:focus-visible {
    background: var(--surface0);
  }
  .item-text {
    flex: 1;
    display: grid;
    min-width: 0;
  }
  .item-label {
    color: var(--teal);
    font-weight: 600;
  }
  .site .item-label {
    color: var(--text);
  }
  .item-hint {
    font-size: var(--font-small);
    color: var(--overlay1);
  }
  .item-hint.q {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font-size: 12px;
    color: var(--overlay1);
    white-space: nowrap;
  }
  .form {
    display: grid;
    grid-template-columns: minmax(0, 1fr); /* (a long search can't widen it) */
    gap: 12px;
  }
  .hint {
    margin: 0;
    font-size: var(--font-small);
    color: var(--overlay1);
  }
  .what-tags {
    margin: -4px 0 2px;
    color: var(--subtext);
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .left {
    margin-top: -4px;
    overflow-wrap: anywhere;
  }
  .what {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    color: var(--text);
    font-weight: 600;
  }
  .what span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* (the searches a split OR search makes: one per line, the list scrolls past a few) */
  .split-list {
    margin: -4px 0 0;
    padding: 6px 10px;
    max-height: 132px;
    overflow: auto;
    list-style: none;
    display: grid;
    gap: 2px;
    border-radius: 10px;
    background: var(--crust);
    color: var(--subtext);
    font-size: var(--font-small);
    overflow-wrap: anywhere;
  }
  .segmented {
    width: 100%;
  }
  .segmented button {
    flex: 1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
  }
  .rows {
    display: grid;
    gap: 8px;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .row-label {
    color: var(--subtext);
  }
  .max {
    width: 8em;
  }
  .new-group {
    width: 100%;
  }
  .foot {
    display: flex;
    justify-content: flex-end;
  }
  .foot button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .go {
    background: var(--mauve);
    color: var(--crust);
    border-color: transparent;
  }
</style>
