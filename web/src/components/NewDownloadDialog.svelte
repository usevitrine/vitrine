<script lang="ts">
  import GrabBar from './GrabBar.svelte';
  import { overlayScroll } from '../lib/overlayScroll';
  // A new download from the library (2026-10-22: the Library's download button), a page at a time: the library's
  // search or another one (only when the search has tags), the site (where the search's tags came from first), the
  // search on that site (the library's written the site's way: lib/siteSearch - an OR group on a site without an OR
  // search: one search for each combination), then once or a subscription (how often, its group). A card in the middle on wider screens, a sheet along the bottom on phones - both above the
  // on-screen keyboard (--kb).
  import { onMount, tick } from 'svelte';
  import { reveal, swap } from '../lib/motion';
  import { phone, touch } from '../lib/media';
  import { read, write } from '../lib/storage';
  import { api, type SubGroup } from '../lib/api';
  import { siteLabel, tagQuery, tagsQuery, canOr, sites as siteInfo } from '../lib/sites.svelte';
  import { searchTags, siteSearch, splitSearches, separateSearches, separable, searchWords, ownFavorites, favoritesSearch } from '../lib/siteSearch';
  import { marquee } from '../lib/marquee';
  import { parse, type Chip } from '../lib/query';
  import { tagOrigins, type SiteChoice } from '../lib/newDownload.svelte';
  import { siteOn } from '../lib/sections';
  import { intervals, DEFAULT_INTERVAL, cap, every } from '../lib/schedule';
  import { router } from '../lib/router.svelte';
  import { toasts, attempt } from '../lib/toast.svelte';
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
    subscribe = false,
    ondone,
    onclose,
  }: {
    query: string; // a library search
    fromTags?: boolean; // tags picked (a tag's menu, the details): straight to the site
    fallback?: string; // offered first when the tags came from no site
    presetSite?: string; // a search on a site already (a Browse gallery's link): straight to "once or subscribe"
    presetText?: string;
    label?: string; // shown for it instead of the search (the gallery's title)
    subscribe?: boolean; // Subscribe picked at first (a comic's menu)
    ondone?: (subscribed: boolean, id?: number) => void; // (id: the new subscription's)
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
  let subscribed = $state<Record<string, string[]>>({}); // each site's subscribed searches
  const subscribedTo = (siteId: string, q: string) => !!subscribed[siteId]?.includes(q.trim());
  let origins = $state<SiteChoice[]>([]);
  let loaded = $state(false);
  let shown = $state(false); // (the dialog waits for its sites, a moment at most)
  let from: Promise<SiteChoice[]> = Promise.resolve([]); // the sites the search's tags came from (asked for at once)
  const usable = (list: SiteChoice[], sites = downloadSites) => list.filter((c) => sites.includes(c.site));
  onMount(async () => {
    siteInfo.load();
    // (all asked at once - the tags' sites were asked for only after the rest came back; for "This search" too, so
    // it's there when picked)
    if (tags.length) from = tagOrigins(tags);
    const slow = setTimeout(() => (shown = true), 300); // (a slow server: open with "Loading…", then grow)
    let sites: string[] = [];
    await attempt(async () => {
      const res = await api.downloadSites();
      await siteInfo.whenLoaded(); // (each site's OR search)
      sites = res.sites.filter(siteOn);
      groups = res.groups ?? [];
      subscribed = res.subscribed ?? {};
      if (fromTags) origins = usable(await from, sites);
    });
    clearTimeout(slow);
    const fill = () => {
      downloadSites = sites;
      loaded = true;
    };
    // (shown already: the list comes in place of "Loading…", the dialog's height follows instead of jumping)
    if (shown) await pages.grow(fill);
    else {
      fill();
      shown = true;
    }
    focusFirst();
  });
  function lastSite(): string {
    return read('vitrine.lastSite') ?? '';
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
  const sheet = phone();
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
    if (el instanceof HTMLInputElement && touch()) return;
    // (not scrolled to: while the height grows the box is still short, and it scrolled the page's top out of view)
    (el ?? panel)?.focus({ preventScroll: true });
  }

  // ---- the steps
  async function pickWhat(search: boolean) {
    useSearch = search;
    if (search) origins = usable(await from);
    go('site');
  }
  // Each site picked, with its search written its way (2026-11-16, the user: "when i start a download and it asks me to
  // pick a site, I wanna be able to select multiple sites"): a tap on a site goes on with it alone; its check picks
  // it among others (then a tap on a row picks or unpicks it), and Next goes on with all of them - a search box for
  // each, then for each site its own "once or subscribe" - how often, its group, how many (2026-11-16, the user: "if I
  // wanna subscribe to the downloads it's not as expected. I should be able to set a subscription/group/frequency per
  // site"). split: OR groups a site has no OR search for - a search for each combination.
  type How = { repeat: string; max: number | undefined; group: string; groupName: string }; // repeat: seconds, '0' = once
  // svelte-ignore state_referenced_locally
  const newHow = (): How => ({ repeat: subscribe ? String(DEFAULT_INTERVAL) : '0', max: undefined, group: '', groupName: '' });
  // sep: "Separately" - the site's search for each tag (each can be unticked; had: subscribed to it already).
  type Sep = { q: string; on: boolean; had: boolean };
  type Entry = { site: string; text: string; left: Chip[]; split: Chip[]; sep: Sep[] | null; how: How };
  // svelte-ignore state_referenced_locally
  let entries = $state<Entry[]>(presetSite ? [{ site: presetSite, text: presetText, left: [], split: [], sep: null, how: newHow() }] : []);
  // Together (one search with every tag) or Separately (a search for each - 2026-11-17, the user: "if i have more than
  // 1 selected, i want the option to download them all in separate queries"): picked on the site page, so each site's
  // searches are made from it.
  let separate = $state(false);
  // svelte-ignore state_referenced_locally
  const canSeparate = separable(query);
  const site = $derived(entries[0]?.site ?? '');
  const multi = $derived(entries.length > 1);
  let boxes = $state<HTMLInputElement[]>([]); // (the suggestions sit under each, over the dialog)
  const searchesOf = (e: Entry) => (e.sep ? e.sep.filter((x) => x.on).map((x) => x.q) : splitSearches(e.site, e.text, e.split));
  const jobs = $derived(entries.flatMap((e) => searchesOf(e).map((q) => ({ site: e.site, q, text: e.text }))));
  const searches = $derived(jobs.map((j) => j.q));
  const ready = $derived(entries.length > 0 && entries.every((e) => searchesOf(e).length > 0) && jobs.every((j) => j.q !== ''));
  let sitePicks = $state<string[]>([]); // checked on the site page (several at once)
  function togglePick(s: string) {
    sitePicks = sitePicks.includes(s) ? sitePicks.filter((x) => x !== s) : [...sitePicks, s];
  }
  function siteTap(s: string) {
    if (sitePicks.length) togglePick(s);
    else pickSites([s]);
  }
  function pickSites(list: string[]) {
    entries = list.map((s) => {
      if (!useSearch) return { site: s, text: '', left: [], split: [], sep: null, how: newHow() };
      const how = newHow();
      const name = searchWords(parse(query)).slice(0, 60);
      if (separate && canSeparate) {
        // (the ones subscribed to already: unticked, and said)
        const r = separateSearches(s, query);
        const sep = r.searches.map((q) => ({ q, had: subscribedTo(s, q), on: !subscribedTo(s, q) }));
        if (sep.length > 1) Object.assign(how, { group: 'new', groupName: name });
        return { site: s, text: '', left: r.left, split: [], sep, how };
      }
      const w = siteSearch(s, query);
      // (several subscriptions from one search: together in a group named after it, to start with)
      if (w.split.length) Object.assign(how, { group: 'new', groupName: name });
      return { site: s, text: w.query, left: w.left, split: w.split, sep: null, how };
    });
    go('search');
  }
  // One post (its link, or id:N): asked about first - the site says whether it has it, here in the dialog - and then
  // there's nothing to choose (a one-time download of that post).
  let single = $state<{ query: string; files: number } | null>(null);
  let refError = $state('');
  let checking = $state(false);
  const mayBeSingle = (q: string) => /^(id:\S+|https?:\/\/\S+)$/i.test(q.trim());
  async function toHow(e?: SubmitEvent) {
    e?.preventDefault();
    if (!ready || checking) return;
    single = null;
    refError = '';
    if (jobs.length === 1 && mayBeSingle(searches[0])) {
      checking = true;
      try {
        const res = await api.checkSingle(site, searches[0]);
        if (res.single) {
          single = { query: res.query ?? searches[0], files: res.files ?? 1 };
          entries[0].how.repeat = '0';
        }
      } catch (err) {
        refError = err instanceof Error ? err.message : String(err);
        return;
      } finally {
        checking = false;
      }
    }
    go('how');
  }

  let busy = $state(false);
  const cur = $derived(entries[0]?.how ?? newHow()); // (one site: its way)
  const favSearch = $derived(favoritesSearch(entries[0]?.text ?? '')); // (its files favorites: no group)
  const groupOptionsFor = (siteId: string) => [
    { value: '', label: t('downloads.noGroup') },
    ...groups.filter((g) => g.site === siteId).map((g) => ({ value: String(g.id), label: g.name })),
    { value: 'new', label: t('downloads.newGroup') },
  ];
  const intervalOptions = $derived(intervals.map((v) => ({ value: String(v), label: cap(every(v)) })));
  function pickGroup(h: How, v: string) {
    h.group = v;
    const g = groups.find((x) => String(x.id) === v);
    if (g?.interval) h.repeat = String(g.interval); // the group's usual schedule
  }
  // The submit button: Download (all once), Subscribe (all subscriptions), Start (some of each).
  const allOnce = $derived(entries.every((e) => e.how.repeat === '0'));
  const allSubs = $derived(entries.every((e) => e.how.repeat !== '0'));

  async function start(e: SubmitEvent) {
    e.preventDefault();
    if (!ready || busy) return;
    await attempt(async () => {
      for (const x of entries) {
        if (x.how.repeat !== '0' && !favoritesSearch(x.text) && x.how.group === 'new' && !x.how.groupName.trim()) throw new Error(t('downloads.nameGroupFirst'));
      }
      let subId: number | undefined;
      let subscribedNow = false;
      let skipped = 0; // (subscribed to already: left as they are - one existing stopped the rest, ErrExists)
      for (const x of entries) {
        const h = x.how;
        const fav = favoritesSearch(x.text);
        if (h.repeat !== '0') {
          const group = fav ? {} : h.group === 'new' ? { group_name: h.groupName.trim() } : h.group ? { group_id: +h.group } : {};
          // (a split search: each combination its own subscription - a new group's name finds the group the first made)
          for (const s of searchesOf(x)) {
            if (searchesOf(x).length > 1 && subscribedTo(x.site, s)) {
              skipped++;
              continue;
            }
            subId = (await api.subscribe(x.site, s, +h.repeat, h.max ?? 0, group, ownFavorites(x.text), label)).id; // (label: a gallery's title - its name in the lists)
            subscribedNow = true;
          }
        } else {
          for (const s of searchesOf(x)) await api.addDownload(x.site, s, h.max ?? 0, ownFavorites(x.text), label);
        }
      }
      const made = searches.length - skipped;
      const uniq = [...new Set(searches)]; // (the same search on several sites: named once)
      const q = uniq.length > 1 ? t('newDl.searches', { n: made }) : uniq[0];
      if (!made) {
        // (all of them subscribed to already: only that said)
      } else if (multi) {
        toasts.show(t('newDl.startedOn', { query: label || q, sites: entries.map((x) => siteLabel(x.site)).join(', ') }), { cat: 'downloadsStarted', kind: 'info' });
      } else if (cur.repeat !== '0') {
        const gName = favSearch ? t('menu.favorites') : cur.group === 'new' ? cur.groupName.trim() : groups.find((x) => String(x.id) === cur.group)?.name;
        toasts.show(gName ? t('downloads.subscribedIn', { query: label || q, name: gName, every: every(+cur.repeat) }) : t('downloads.subscribed', { query: label || q, every: every(+cur.repeat) }), {
          cat: 'downloadsStarted',
        });
      } else {
        toasts.show(t('downloads.downloading', { query: label || q, site: siteLabel(site) }), { cat: 'downloadsStarted', kind: 'info' });
      }
      if (skipped) toasts.show(t('newDl.skipped', { n: skipped }), { cat: 'downloadsStarted', kind: 'info' });
      write('vitrine.lastSite', site);
      ondone?.(subscribedNow, subId);
      onclose();
    }, (b) => (busy = b));
  }

  const titles: Record<Page, () => string> = {
    what: () => t('tagMenu.newDownload'),
    site: () => t('newDl.site'),
    search: () => (multi ? t('newDl.searchSites', { n: entries.length }) : t('newDl.search', { site: siteLabel(site) })),
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
  <!-- (Back closes it: from the moment it's asked for, here - on the dialog, which waits for its sites, the entry came
       after the menu it was opened from had started going back: that Back closed the dialog as it appeared, on phones) -->
  <div class="backdrop" role="presentation" in:shade|global out:shade|global={{ out: true }} onpointerdown={onclose} {@attach backClose(onclose)}></div>
  <!-- (once its sites are in, or after a moment: opened at its height, not grown from "Loading…" - a centered dialog that grew moved up) -->
  {#if shown}
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
      in:sheetIn|global={{ on: sheet, desk: 'pop' }}
      out:sheetOut|global={{ on: sheet, desk: 'pop' }}>
      {#if sheet}<GrabBar bleed={14} after={-6} />{/if}
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
              {#if useSearch && canSeparate}
                <div class="segmented apart" role="tablist" aria-label={t('newDl.apart')}>
                  <button type="button" role="tab" aria-selected={!separate} onclick={() => (separate = false)}><Icon name="search" size={16} /> {t('newDl.together')}</button>
                  <button type="button" role="tab" aria-selected={separate} onclick={() => (separate = true)}><Icon name="split" size={16} /> {t('newDl.separately')}</button>
                </div>
                <p class="hint left apart-hint">{separate ? t('newDl.separatelyHint') : t('newDl.togetherHint')}</p>
              {/if}
              <!-- (Loading… goes at once and the list fades in as the height follows - Loading… closing after the list
                   had come moved the dialog again; Loading… itself only shows if the wait is long enough to see) -->
              {#if !loaded || !siteInfo.loaded}
                <p class="hint waiting">{t('common.loading')}</p>
              {:else if !downloadSites.length}
                <div class="came" in:swap>
                  <p class="hint">{t('newDl.noSites')}</p>
                  <button class="quiet" onclick={() => (router.go('settings', 'sites'), onclose())}>{t('downloads.manageSites')}</button>
                </div>
              {:else}
                <div class="came" in:swap>
                {#if useSearch && origins.length}
                  <h3 transition:reveal>{t('sitePicker.from')}</h3>
                  <div class="items">
                    {#each origins as c, k (c.site)}
                    {@const on = sitePicks.includes(c.site)}
                    <div class="pick-row" class:on>
                      <button class="item site" aria-pressed={sitePicks.length ? on : undefined} onclick={() => siteTap(c.site)} data-autofocus={k === 0 ? true : undefined}>
                        <SiteIcon host={siteInfo.hosts[c.site] ?? ''} />
                        <span class="item-text"><span class="item-label">{siteLabel(c.site)}</span></span>
                        <span class="meta">{c.tags < tags.length ? t('sitePicker.someTags', { count: c.tags, n: tags.length }) : t('common.files', { n: c.files })}</span>
                      </button>
                      <button class="site-check" class:on aria-pressed={on} aria-label={t('newDl.severalSites')} onclick={() => togglePick(c.site)}
                        ><Icon name="check" size={14} /></button>
                    </div>
                    {/each}
                  </div>
                {/if}
                {#if others.length}
                  {#if useSearch && origins.length}<h3 transition:reveal>{t('sitePicker.others')}</h3>{/if}
                  <div class="items">
                    {#each others as s, k (s)}
                    {@const on = sitePicks.includes(s)}
                    <div class="pick-row" class:on>
                      <button class="item site" aria-pressed={sitePicks.length ? on : undefined} onclick={() => siteTap(s)} data-autofocus={k === 0 && !(useSearch && origins.length) ? true : undefined}>
                        <SiteIcon host={siteInfo.hosts[s] ?? ''} />
                        <span class="item-text"><span class="item-label">{siteLabel(s)}</span></span>
                      </button>
                      <button class="site-check" class:on aria-pressed={on} aria-label={t('newDl.severalSites')} onclick={() => togglePick(s)}
                        ><Icon name="check" size={14} /></button>
                    </div>
                    {/each}
                  </div>
                {/if}
                {#if sitePicks.length}
                  <div class="foot picks-foot" transition:reveal>
                    <span class="hint">{t('newDl.nSites', { n: sitePicks.length })}</span>
                    <button type="button" onclick={() => pickSites(sitePicks)}>{t('newDl.next')} <Icon name="right" size={16} /></button>
                  </div>
                {/if}
                </div>
              {/if}
            {:else if page === 'search'}
              <!-- What to search for on the site (the library's search written its way, or a new one). -->
              <form class="form" onsubmit={toHow}>
                {#each entries as e, i (e.site)}
                  <div class="site-search">
                    {#if multi}<p class="site-name"><SiteIcon host={siteInfo.hosts[e.site] ?? ''} /> {siteLabel(e.site)}</p>{/if}
                    {#if e.sep}
                      <!-- (each its own search: unticked ones left out; one subscribed to already starts unticked) -->
                      <ul class="sep-list" aria-label={multi ? t('newDl.searchesOn', { site: siteLabel(e.site) }) : t('newDl.theSearches')} {@attach overlayScroll}>
                        {#each e.sep as x, k (x.q)}
                          <li>
                            <button type="button" class="sep-row" class:on={x.on} aria-pressed={x.on} onclick={() => (x.on = !x.on)} data-autofocus={i === 0 && k === 0 ? true : undefined}>
                              <span class="site-check" class:on={x.on} aria-hidden="true"><Icon name="check" size={14} /></span>
                              <span class="sep-text">
                                <span class="marquee sliding" {@attach marquee}><span>{x.q}</span></span>
                                {#if x.had}<span class="sep-had">{t('newDl.subscribedAlready')}</span>{/if}
                              </span>
                            </button>
                          </li>
                        {/each}
                      </ul>
                      <p class="hint left">{searchesOf(e).length ? t('newDl.nPicked', { n: searchesOf(e).length, of: e.sep.length }) : t('newDl.nonePicked')}</p>
                    {:else}
                    <TagInput
                      anchor={sheet ? undefined : boxes[i]}
                      above={sheet}
                      bind:inputEl={boxes[i]}
                      bind:value={e.text}
                      multi
                      comics={siteInfo.engines[e.site] === 'ehentai' ? e.site : undefined}
                      sites={e.site}
                      format={(tg) => tagQuery(e.site, tg)}
                      anyOf={(tags) => (canOr(e.site, tags) ? tagsQuery(e.site, tags, true) : null)}
                      label={multi ? t('newDl.searchOn', { site: siteLabel(e.site) }) : t('downloads.searchOnSite')}
                      placeholder={siteInfo.hints[e.site] ?? t('downloads.searchSite', { site: siteLabel(e.site) })}
                      autofocus={i === 0 && !touch()}
                      onenter={() => toHow()} />
                    {/if}
                    {#if e.split.length}<p class="hint left" transition:reveal>{t('newDl.split', { site: siteLabel(e.site), n: searchesOf(e).length })}</p>{/if}
                    {#if e.left.length}<p class="hint left" transition:reveal>{t('newDl.leftOut', { what: searchWords(e.left) })}</p>{/if}
                  </div>
                {/each}
                {#if refError}<p class="hint left ref-error" role="alert" transition:reveal>{refError}</p>
                {:else if !multi && siteInfo.single[site]}<p class="hint left">{t('newDl.singleTip')}</p>{/if}
                <div class="foot">
                  <button type="submit" disabled={!ready || checking}>{t('newDl.next')} <Icon name="right" size={16} /></button>
                </div>
              </form>
            {:else}
              <!-- Once, or a subscription - for each site its own when there are several. -->
              <form class="form" onsubmit={start}>
                {#if multi}
                  {#each entries as x, i (x.site)}
                    <section class="site-how" aria-label={siteLabel(x.site)}>
                      <p class="what" title={label || searchesOf(x).join('\n')}>
                        <SiteIcon host={siteInfo.hosts[x.site] ?? ''} />
                        <span class="site-how-name">{siteLabel(x.site)}</span>
                        <span class="site-how-q">{label || (searchesOf(x).length > 1 ? t('newDl.searches', { n: searchesOf(x).length }) : searchesOf(x)[0])}</span>
                      </p>
                      {@render howControls(x.how, x.site, favoritesSearch(x.text), i === 0)}
                    </section>
                  {/each}
                {:else}
                  <p class="what" title={label || searches.join('\n')}>
                    <SiteIcon host={siteInfo.hosts[site] ?? ''} /> <span>{label || (searches.length > 1 ? t('newDl.searches', { n: searches.length }) : searches[0])}</span>
                  </p>
                  {#if searches.length > 1}
                    <ul class="split-list" {@attach overlayScroll} transition:reveal>
                      {#each searches as s (s)}<li>{s}</li>{/each}
                    </ul>
                  {/if}
                  {#if !single}{@render howControls(cur, site, favSearch, true)}{/if}
                  <p class="hint">
                    {#if single}{t('newDl.singleHint', { n: single.files })}
                    {:else if cur.repeat === '0'}{cur.max ? t('downloads.noteOnce', { n: Number(cur.max) }) : t('newDl.onceHint')}
                    {:else}{t('downloads.noteSub', { every: every(+cur.repeat), first: cur.max ? t('downloads.newestN', { n: Number(cur.max) }) : t('downloads.everyPost') })}.{/if}
                  </p>
                {/if}
                <div class="foot">
                  <button type="submit" class="go" disabled={busy}>
                    {#if allOnce}<Icon name="download" size={18} /> {searches.length > 1 ? t('newDl.downloadN', { n: searches.length }) : t('backups.download')}{:else if allSubs}<Icon name="bell" size={18} /> {searches.length > 1 ? t('newDl.subscribeN', { n: searches.length }) : t('downloads.subscribe')}{:else}<Icon name="right" size={18} /> {t('newDl.startN', { n: searches.length })}{/if}
                  </button>
                </div>
              </form>
            {/if}
          </div>
        {/key}
      </div>
    </div>
  {/if}
</div>

{#snippet howControls(h: How, siteId: string, fav: boolean, first: boolean)}
  <div class="segmented" role="tablist" aria-label={multi ? t('newDl.howOn', { site: siteLabel(siteId) }) : t('newDl.how')}>
    <button type="button" role="tab" aria-selected={h.repeat === '0'} onclick={() => (h.repeat = '0')} data-autofocus={first ? true : undefined}
      ><Icon name="download" size={16} /> {t('downloads.once')}</button>
    <button type="button" role="tab" aria-selected={h.repeat !== '0'} onclick={() => h.repeat === '0' && (h.repeat = String(DEFAULT_INTERVAL))}
      ><Icon name="bell" size={16} /> {t('downloads.subscribe')}</button>
  </div>
  <div class="rows">
    {#if h.repeat !== '0'}
      <div class="row" transition:reveal>
        <span class="row-label">{t('downloads.repeat')}</span>
        <Dropdown floating value={h.repeat} options={intervalOptions} onchange={(v) => (h.repeat = v)} label={multi ? t('newDl.repeatOn', { site: siteLabel(siteId) }) : t('downloads.repeat')} heading={t('downloads.repeat')} />
      </div>
      {#if !fav}
        <div class="row" transition:reveal>
          <span class="row-label">{t('downloads.group')}</span>
          <Dropdown floating value={h.group} options={groupOptionsFor(siteId)} onchange={(v) => pickGroup(h, v)} label={multi ? t('newDl.groupOn', { site: siteLabel(siteId) }) : t('downloads.group')} heading={t('downloads.addToGroup')} />
        </div>
        {#if h.group === 'new'}
          <input class="new-group" value={h.groupName} oninput={(e) => (h.groupName = e.currentTarget.value)} placeholder={t('downloads.newGroupExample')}
            aria-label={multi ? t('newDl.newGroupOn', { site: siteLabel(siteId) }) : t('downloads.newGroupLabel')} maxlength="60" transition:reveal />
        {/if}
      {/if}
    {/if}
    <label class="row">
      <span class="row-label">{h.repeat === '0' ? t('downloads.files') : t('downloads.firstCheck')}</span>
      <input
        class="max"
        value={h.max ?? ''}
        oninput={(e) => (h.max = e.currentTarget.value ? Number(e.currentTarget.value) : undefined)}
        type="number"
        min="1"
        inputmode="numeric"
        placeholder={t('downloads.all')}
        aria-label={h.repeat === '0' ? t('downloads.maxFiles') : t('downloads.maxFirst')} />
    </label>
  </div>
{/snippet}

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
  .came {
    display: grid;
    gap: 10px;
  }
  /* (a short wait shows nothing: "Loading…" appearing and going at once only flickered) */
  .waiting {
    animation: waiting 0.2s 0.3s both;
  }
  @keyframes waiting {
    from {
      opacity: 0;
    }
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
  .ref-error {
    color: var(--red);
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
  /* Together / Separately, over the sites */
  .apart {
    margin-bottom: -2px;
  }
  .apart-hint {
    margin: 0 0 4px;
  }
  /* "Separately": a site's searches, each with its check (unticked: left out); the list scrolls past a few */
  .sep-list {
    margin: 0;
    padding: 4px;
    max-height: 212px;
    overflow: auto;
    list-style: none;
    display: grid;
    grid-template-columns: minmax(0, 1fr); /* (a long search slides inside its row, not past the list) */
    gap: 2px;
    border-radius: 12px;
    background: var(--crust);
  }
  .sep-row,
  .sep-row:hover:not(:disabled) {
    width: 100%;
    min-height: 0;
    display: flex;
    align-items: center;
    padding: 6px 8px 6px 0;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--subtext);
    text-align: left;
    font-weight: 400;
  }
  .sep-row.on {
    color: var(--text);
  }
  @media (hover: hover) {
    .sep-row:hover:not(:disabled) {
      background: var(--surface0);
    }
  }
  .sep-text {
    min-width: 0;
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .sep-text .marquee {
    flex: 1;
    min-width: 0;
  }
  .sep-had {
    flex: none;
    font-size: var(--font-small);
    color: var(--overlay1);
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

  /* Several sites (2026-11-16): a check beside each site; Next once some are checked. */
  .pick-row {
    display: flex;
    align-items: center;
    gap: 4px;
    border-radius: 10px;
    transition: background-color 0.15s;
  }
  .pick-row.on {
    background: color-mix(in srgb, var(--mauve) 12%, transparent);
  }
  .pick-row .item {
    flex: 1;
    min-width: 0;
  }
  .site-check,
  .site-check:hover:not(:disabled) {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    min-height: 0;
    margin: 0 10px 0 4px;
    padding: 0;
    border-radius: 50%;
    border: 2px solid var(--overlay0);
    background: transparent;
    color: transparent;
    transition: background-color 0.15s, border-color 0.15s, color 0.15s;
  }
  /* (its hover look too: a phone keeps a tapped button "hovered", and the check only showed after the next tap) */
  .site-check.on,
  .site-check.on:hover:not(:disabled) {
    border-color: var(--mauve);
    background: var(--mauve);
    color: var(--crust);
  }
  .picks-foot {
    align-items: center;
    justify-content: space-between;
    padding-top: 4px;
  }
  .site-search {
    display: grid;
    gap: 6px;
  }
  .site-name {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-weight: 600;
    color: var(--subtext);
  }


  /* Several sites: each its own way, in a framed section. */
  .site-how {
    display: grid;
    gap: 10px;
    padding: 12px;
    border-radius: 12px;
    border: 1px solid var(--surface0);
    background: var(--crust);
  }
  .site-how .what {
    min-width: 0;
  }
  .site-how-name {
    flex: none;
  }
  .site-how-q {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--subtext);
    font-weight: 500;
  }
</style>
