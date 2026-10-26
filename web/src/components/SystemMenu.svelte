<script lang="ts">
  import { overlayScroll } from '../lib/overlayScroll';
  // The filter menu (like hydrus-web's system predicates, but only what
  // Vitrine has): pick a filter, fill in its short form, and it's added to the
  // search as a chip. Opened from the search bar's filter button, or by
  // tapping a filter chip (then the form opens filled in, and updates it).
  import { backClose } from '../lib/backClose';
  import { reveal, pop, nudge } from '../lib/motion';
  import { swallowPress, leaving } from '../lib/outside';
  import { onMount, tick } from 'svelte';
  import Dropdown from './Dropdown.svelte';
  import { sheetDrag } from '../lib/sheetDrag';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { Pages } from '../lib/pages.svelte';
  import Icon from './Icon.svelte';
  import { sites, siteLabel } from '../lib/sites.svelte';
  import { api } from '../lib/api';
  import { parse, format } from '../lib/query';
  import { settings } from '../lib/settings.svelte';
  import { t, num } from '../lib/i18n.svelte';
  import { useScope } from '../lib/scope.svelte';
  import { splitTag } from '../lib/namespaces.svelte';
  import {
    groups,
    instant,
    defaults,
    build,
    describe,
    comparisons,
    timeUnits,
    durationUnits,
    sizeUnits,
    typeChoices,
    ratingChoices,
    sortFields,
    comicSortFields,
    comicGroups,
    browseGroups,
    browseCategories,
    browseLanguages,
    starChoices,
    filterInfo,
    tagFilter,
    splitSort,
    joinSort,
    sortInfo,
    type FilterId,
    type Form,
    type TimeMode,
    type Cmp,
    midSentence,
  } from '../lib/system';

  let {
    initial = null,
    noSort = false,
    sort = '',
    defaultSort = undefined,
    onsort = () => {},
    query = '',
    onquery = () => {},
    onadd,
    onclose,
    sorts = [],
    browseSites = [],
    site = '',
    onsite = () => {},
  }: {
    initial?: Form | null; // editing an existing filter chip
    noSort?: boolean; // filters only (e.g. the default search in Settings)
    sort?: string; // the current order (shown at the top of the list)
    defaultSort?: string; // the page's default order (the library's by default; Favorites has its own)
    onsort?: (sort: string) => void;
    query?: string; // the current search (the From row reads and changes its site chip)
    onquery?: (q: string) => void;
    onadd: (text: string) => void; // the filter's search text (adds it, or replaces the chip being edited)
    onclose: (refocus?: boolean) => void;
    sorts?: string[]; // Comics > Browse: the gallery site's other orders (after its newest first)
    browseSites?: string[]; // ... the sites it can browse (the From row: one of them)
    site?: string; // ... the one browsed
    onsite?: (id: string) => void;
  } = $props();
  const pageDefault = $derived(defaultSort ?? settings.defaultSort);
  // The Comics page: its own filters and orders (and its words: added, updated).
  // Comics > Browse: a gallery site's filters and orders (its newest first, then its own lists - popular, toplists).
  const browse = useScope().kind === 'browse';
  const comics = useScope().kind === 'comics';
  const fi = (id: FilterId) => filterInfo(id, comics);
  const si = (field: string) => (browse ? { label: t(`browse.sort.${field}`), desc: null, asc: null } : sortInfo(field, comics));
  const shownGroups = browse ? browseGroups : comics ? comicGroups : groups;
  // ("Times came" only with the "I came" button on)
  const offered = $derived(browse ? ['newest', ...sorts] : comics ? comicSortFields : sortFields.filter((f) => f !== 'came' || settings.came));

  // svelte-ignore state_referenced_locally
  let form = $state<Form | null>(initial ? { ...initial, values: [...initial.values] } : null);
  let panel = $state<HTMLElement>();
  // svelte-ignore state_referenced_locally
  const editing = !!initial; // the menu is re-created for each chip

  const text = $derived(form ? build(form) : '');
  const preview = $derived(text ? (describe(text.replace(/^-/, ''), comics) ?? text) : '');

  // Category and language (comics): picked from the comics' own tags, with how many have each.
  let tagChoices = $state<{ value: string; label: string; meta: string }[] | null>(null);
  $effect(() => {
    const id = form?.id;
    if (!id || !tagFilter(id)) return;
    if (browse) {
      // (the site's own: not the library's comics')
      tagChoices = (id === 'category' ? browseCategories : browseLanguages).map((v) => ({ value: v, label: v.replace(/_/g, ' '), meta: '' }));
      return;
    }
    tagChoices = null;
    api
      .autocomplete(id + ':', 100, undefined, 'comics')
      .then((res) => {
        tagChoices = res.suggestions
          .filter((x) => splitTag(x.tag).ns === id)
          .map((x) => {
            const name = splitTag(x.tag).name;
            return { value: name, label: name.replace(/_/g, ' '), meta: num(x.count) };
          });
      })
      .catch(() => (tagChoices = []));
  });

  const timeModes: { value: TimeMode; label: string }[] = $derived(
    (['last', 'ago', 'since', 'before', 'on'] as TimeMode[]).map((value) => ({ value, label: t(`filterMenu.when.${value}`) })),
  );
  const siteChoices = $derived(
    [...new Set(Object.values(sites.names))].sort().map((s) => ({ value: s, label: s })),
  );

  onMount(() => focusFirst());

  async function focusFirst() {
    await tick();
    (panel?.querySelector<HTMLElement>('[data-autofocus]') ?? panel)?.focus();
  }

  function choose(id: FilterId) {
    const f = defaults(id);
    if (instant(id)) {
      onadd(build(f));
      onclose(true);
      return;
    }
    form = f;
    focusFirst();
  }

  function back() {
    form = null;
    focusFirst();
  }

  function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!text) return;
    onadd(text);
    onclose(true);
  }

  function toggleValue(v: string, multi: boolean) {
    if (!form) return;
    if (multi) form.values = form.values.includes(v) ? form.values.filter((x) => x !== v) : [...form.values, v];
    else form.values = form.values[0] === v ? [] : [v];
  }

  // Sort: one full-width button showing the order; it opens a page of orders,
  // and an order with two directions opens a page for those (a back arrow on
  // each). Picking applies it and comes back here.
  let sortPage = $state<null | 'fields' | string>(null); // 'fields', or the field whose directions show
  function openSort(page: 'fields' | string) {
    sortPage = page;
    focusFirst();
  }
  function pickField(f: string) {
    if (!si(f).desc) return pickSort(f); // random: no direction
    openSort(f);
  }
  function pickSort(value: string) {
    onsort(value);
    sortPage = null;
    focusFirst();
  }
  const current = $derived(splitSort(sort || pageDefault));
  const info = $derived(si(current.field));
  const defaultField = $derived(splitSort(pageDefault).field);
  const fieldOptions = $derived(
    [...offered, ...(offered.includes(current.field) ? [] : [current.field])].map((f) => ({
      value: f,
      label: si(f).label,
      meta: f === defaultField ? t('filterMenu.default') : undefined,
    })),
  );
  const isDefault = $derived(joinSort(current.field, current.asc) === pageDefault);

  // From: which sites' files to show - one system:site= chip in the search
  // (several sites: system:site=a,b; "All sites" takes it out), with how many of
  // the search's files came from each site. Applies at once; the menu stays open.
  const siteOf = (text: string) => /^system:site=(.+)$/i.exec(text)?.[1].toLowerCase();
  const siteParts = $derived.by(() => {
    const chips = parse(query);
    const i = chips.findIndex((c) => c.atoms.length === 1 && !c.atoms[0].neg && siteOf(c.atoms[0].text));
    return { sites: i >= 0 ? siteOf(chips[i].atoms[0].text)!.split(',').filter(Boolean) : [], rest: format(chips.filter((_, k) => k !== i)) };
  });
  // A site chip may name a site by its address: matched to the plugin ID.
  const currentSites = $derived(
    siteParts.sites.map((s) => Object.entries(sites.names).find(([id, addr]) => id === s || addr === s)?.[0] ?? s),
  );
  const fromLabel = $derived(
    currentSites.length === 0 ? t('filterMenu.allSites') : currentSites.length <= 2 ? currentSites.map(siteLabel).join(', ') : t('filterMenu.nSites', { n: currentSites.length }),
  );
  let siteCounts = $state<Record<string, number>>({});
  let siteTotal = $state<number | null>(null);
  let countsGen = 0;
  $effect(() => {
    if (initial || browse) return; // editing a chip, or a site's search: no From row
    const q = siteParts.rest;
    const g = ++countsGen;
    api
      .siteCounts(q, comics ? 'comics' : '')
      .then((res) => {
        if (g !== countsGen) return;
        siteCounts = Object.fromEntries(res.sites.map((s) => [s.site, s.count]));
        siteTotal = res.total;
      })
      .catch(() => {
        /* the row works without numbers */
      });
  });
  // The sites, those with the most of the search's files first.
  const siteIds = $derived(
    Object.keys(sites.names)
      .filter((id) => !comics || (siteCounts[id] ?? 0) > 0 || currentSites.includes(id)) // (comics: only the sites they came from)
      .sort((a, b) => (siteCounts[b] ?? 0) - (siteCounts[a] ?? 0) || siteLabel(a).localeCompare(siteLabel(b))),
  );
  const setSites = (ids: string[]) => onquery(ids.length ? `${siteParts.rest} system:site=${ids.join(',')}`.trim() : siteParts.rest);
  const toggleSite = (id: string) =>
    setSites(currentSites.includes(id) ? currentSites.filter((x) => x !== id) : [...currentSites, id]);
  let fromPage = $state(false);
  function openFrom(open: boolean) {
    fromPage = open;
    focusFirst();
  }

  // ---- moving between pages (lib/pages, like every menu with pages). Only what
  // can be seen animates: a page taller than the panel scrolls inside it.
  const view = $derived(fromPage ? 'from' : sortPage === 'fields' ? 'fields' : sortPage ? 'dir:' + sortPage : form ? 'form:' + form.id : 'main');
  const depthOf = (v: string) => (v === 'main' ? 0 : v.startsWith('dir:') ? 2 : 1);
  const pages = new Pages((h) => {
    const cs = panel ? getComputedStyle(panel) : null;
    const room = cs ? parseFloat(cs.maxHeight) - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom) - 2 : Infinity;
    return Number.isFinite(room) && room > 0 ? Math.min(h, room) : h;
  });
  let lastView = '';
  $effect.pre(() => {
    const v = view;
    if (!lastView) {
      lastView = v;
      return;
    }
    if (v === lastView) return;
    const deeper = depthOf(v) >= depthOf(lastView);
    lastView = v;
    pages.go(deeper).then(() => panel && (panel.scrollTop = 0)); // (the view changed already: before it's drawn)
  });

  // Phones: a sheet along the bottom (like the other menus there).
  const sheet = matchMedia('(max-width: 639px)').matches;
  function portal(node: HTMLElement) {
    if (sheet) document.body.appendChild(node);
    return { destroy: () => sheet && node.remove() };
  }

  // A press outside the menu and the search bar closes it.
  $effect(() => {
    const down = (e: PointerEvent) => {
      if (leaving(panel)) return; // closed already, fading out
      const t = e.target as HTMLElement;
      if (!panel?.contains(t) && !t.closest('.bar') && !t.closest('[data-floating]')) {
        swallowPress(e);
        onclose(false);
      }
    };
    document.addEventListener('pointerdown', down, true);
    return () => document.removeEventListener('pointerdown', down, true);
  });

  const isTime = (id: FilterId) => id === 'imported' || id === 'uploaded' || id === 'modified';
</script>

<!-- Escape closes the menu even when nothing in it has focus (e.g. just after a page slid in). -->
<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape' && !e.defaultPrevented && !panel?.contains(document.activeElement)) {
      e.preventDefault();
      onclose(true);
    }
  }} />

<div use:portal>
{#if sheet}<div class="backdrop" role="presentation" in:shade|global out:shade|global={{ out: true }}></div>{/if}
<div
  class="panel" {@attach overlayScroll}
  class:sheet
  data-sheet={sheet ? '' : undefined}
    {@attach sheet && sheetDrag(() => onclose(false))}
    {@attach backClose(() => onclose(false))}
  in:sheetIn|global={{ on: sheet }}
  out:sheetOut|global={{ on: sheet }}
  class:form-view={!!form}
  bind:this={panel}
  role="dialog"
  aria-label={form ? t('filterMenu.filterLabel', { filter: fi(form.id).label }) : sortPage ? t('filterMenu.sort') : fromPage ? t('filterMenu.from') : t('searchBar.filters')}
  tabindex="-1"
  onkeydown={(e) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onclose(true);
    }
  }}>
  {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
  <div class="mpages" class:animating={pages.animating} {@attach pages.box} style:height={pages.height !== null ? `${pages.height}px` : undefined}>
  {#key pages.nav}
  <div class="mpage" {@attach pages.page} in:pages.pageIn out:pages.pageOut>
  {#if fromPage}
    <div class="head">
      <button type="button" class="icon back" aria-label={t('filterMenu.backToMain')} title={t('common.back')} onclick={() => openFrom(false)} data-autofocus
        ><Icon name="left" size={18} /></button>
      <h2>{t('filterMenu.from')}</h2>
    </div>
    {#if browse}
      <!-- (Browse: the site whose galleries show - one of them) -->
      <div class="items" role="group" aria-label={t('settings.section.sites.label')}>
        {#each browseSites as id (id)}
          <button class="item pick-item" class:on={id === site} aria-pressed={id === site} onclick={() => { if (id !== site) onsite(id); openFrom(false); }}>
            <span class="item-text"><span class="item-label">{siteLabel(id)}</span></span>
            <span class="tick">{#if id === site}<Icon name="check" size={16} />{/if}</span>
          </button>
        {/each}
      </div>
    {:else}
    <div class="items" role="group" aria-label={t('settings.section.sites.label')}>
      <button class="item pick-item" class:on={!currentSites.length} aria-pressed={!currentSites.length} onclick={() => setSites([])}>
        <span class="item-text"><span class="item-label">{t('filterMenu.allSites')}</span></span>
        {#if siteTotal !== null}<span class="meta" transition:nudge>{num(siteTotal)}</span>{/if}
        <span class="tick">{#if !currentSites.length}<Icon name="check" size={16} />{/if}</span>
      </button>
      {#each siteIds as id (id)}
        {@const on = currentSites.includes(id)}
        <button class="item pick-item" class:on aria-pressed={on} onclick={() => toggleSite(id)}>
          <span class="item-text"><span class="item-label">{siteLabel(id)}</span></span>
          <span class="meta">{num(siteCounts[id] ?? 0)}</span>
          <span class="tick">{#if on}<Icon name="check" size={16} />{/if}</span>
        </button>
      {/each}
    </div>
    {/if}
  {:else if sortPage === 'fields'}
    <div class="head">
      <button type="button" class="icon back" aria-label={t('filterMenu.backToMain')} title={t('common.back')} onclick={() => { sortPage = null; focusFirst(); }} data-autofocus
        ><Icon name="left" size={18} /></button>
      <h2>{t('sort.by')}</h2>
    </div>
    <div class="items" role="list">
      {#each fieldOptions as o (o.value)}
        {@const dirs = !!si(o.value).desc}
        <button class="item pick-item" class:on={o.value === current.field} onclick={() => pickField(o.value)}>
          <span class="item-text">
            <span class="item-label">{o.label}</span>
            {#if browse}
              <!-- (a site's own list has no search: said only when there's one to lose) -->
              {#if o.value !== 'newest' && o.value !== current.field && query.trim()}<span class="item-hint">{t('browse.sortHint')}</span>{/if}
            {:else if o.value === current.field}<span class="item-hint">{dirs ? t('settings.sortWay', { way: (current.asc ? info.asc : info.desc) ?? '' }) : t('filterMenu.now')}</span>{/if}
          </span>
          {#if o.meta}<span class="meta">{o.meta}</span>{/if}
          {#if dirs}<Icon name="right" size={16} />{:else if o.value === current.field}<Icon name="check" size={16} />{/if}
        </button>
      {/each}
    </div>
  {:else if sortPage}
    {@const sf = si(sortPage)}
    <div class="head">
      <button type="button" class="icon back" aria-label={t('filterMenu.backToOrders')} title={t('common.back')} onclick={() => openSort('fields')} data-autofocus
        ><Icon name="left" size={18} /></button>
      <h2>{sf.label}</h2>
    </div>
    <div class="items" role="list">
      {#each [false, true] as asc (asc)}
        {@const value = joinSort(sortPage, asc)}
        {@const on = sortPage === current.field && current.asc === asc}
        <button class="item pick-item" class:on onclick={() => pickSort(value)}>
          <span class="item-text"><span class="item-label">{t('settings.sortWay', { way: (asc ? sf.asc : sf.desc) ?? '' })}</span></span>
          {#if value === pageDefault}<span class="meta">{t('filterMenu.default')}</span>{/if}
          {#if on}<Icon name="check" size={16} />{/if}
        </button>
      {/each}
    </div>
  {:else if !form}
    <div class="head"><h2><Icon name="filter" size={16} /> {noSort ? t('searchBar.filters') : t('searchBar.sortFilters')}</h2></div>
    {#if !noSort}
    <section>
      <div class="sort-head">
        <h3>{t('filterMenu.sort')}</h3>
        {#if !isDefault}
          <button class="reset" onclick={() => onsort(pageDefault)} title={t('filterMenu.resetTitle')} transition:nudge>{t('filterMenu.reset')}</button>
        {/if}
      </div>
      <div class="sort-row">
      <button
        class="sort-pick"
        aria-label={info.desc ? t('filterMenu.sortByWay', { field: info.label, way: midSentence(current.asc ? info.asc : info.desc) }) : t('filterMenu.sortBy', { field: info.label })}
        data-value={current.field}
        data-dir={info.desc ? (current.asc ? info.asc : info.desc) : ''}
        onclick={() => openSort('fields')}>
        <span class="sp-label">{info.label}</span>
        {#if info.desc}<span class="sp-dir">{t('settings.sortWay', { way: current.asc ? info.asc : info.desc })}</span>{/if}
        <Icon name="right" size={16} />
      </button>
      </div>
    </section>
    {/if}
    {#if !browse || browseSites.length > 1}
    {@const from = browse ? siteLabel(site) : fromLabel}
    <section>
      <h3>{t('filterMenu.fromShort')}</h3>
      <div class="sort-row">
        <button class="sort-pick" aria-label={browse ? t('browse.siteLabel', { site: from }) : t('filterMenu.fromLabel', { from })} data-value={browse ? site : currentSites.join(',')} onclick={() => openFrom(true)}>
          <span class="sp-label sp-from">{from}</span>
          <Icon name="right" size={16} />
        </button>
      </div>
    </section>
    {/if}
    {#each shownGroups as g (g.name)}
      <section>
        <h3>{g.name}</h3>
        <div class="items">
          {#each g.items as id (id)}
            <button class="item" onclick={() => choose(id)}>
              <span class="item-text">
                <span class="item-label">{fi(id).label}</span>
                {#if fi(id).hint}<span class="item-hint">{fi(id).hint}</span>{/if}
              </span>
              {#if instant(id)}<Icon name="plus" size={16} />{:else}<Icon name="right" size={16} />{/if}
            </button>
          {/each}
        </div>
      </section>
    {/each}
  {:else}
    <form onsubmit={submit}>
      <div class="head">
        {#if !editing}
          <button type="button" class="icon back" aria-label={t('filterMenu.all')} title={t('filterMenu.all')} onclick={back}><Icon name="left" size={18} /></button>
        {/if}
        <h2>{fi(form.id).label}</h2>
      </div>
      {#if fi(form.id).hint}<p class="hint">{fi(form.id).hint}</p>{/if}

      {#if isTime(form.id)}
        <Dropdown floating={sheet} value={form.mode} options={timeModes} onchange={(v) => (form!.mode = v)} label={t('filter.group.when')} />
        {#if form.mode === 'last' || form.mode === 'ago'}
          <div class="row" transition:reveal>
            <input
              class="num"
              type="number"
              min="1"
              step="any"
              inputmode="decimal"
              value={form.num}
              oninput={(e) => (form!.num = e.currentTarget.value)}
              placeholder="5"
              aria-label={t('filterMenu.howMany')}
              data-autofocus />
            <Dropdown floating={sheet} value={form.unit} options={timeUnits} onchange={(v) => (form!.unit = v)} label={t('filterMenu.unit')} />
            {#if form.mode === 'ago'}<span class="word" transition:nudge>{t('filterMenu.ago')}</span>{/if}
          </div>
        {:else}
          <input type="date" bind:value={form.date} aria-label={t('filterMenu.date')} data-autofocus />
        {/if}
      {:else if form.id === 'duration' || form.id === 'size' || form.id === 'width' || form.id === 'height' || form.id === 'tags' || form.id === 'pages'}
        <div class="row">
          <Dropdown floating={sheet} value={form.cmp} options={comparisons(form.id)} onchange={(v: Cmp) => (form!.cmp = v)} label={t('filterMenu.comparison')} />
          <input
            class="num"
            type="number"
            min="0"
            step={form.id === 'duration' || form.id === 'size' ? 'any' : '1'}
            inputmode={form.id === 'duration' || form.id === 'size' ? 'decimal' : 'numeric'}
            value={form.num}
              oninput={(e) => (form!.num = e.currentTarget.value)}
            placeholder={{ duration: '30', size: '100', width: '1920', height: '1080', tags: '5', pages: '20' }[form.id]}
            aria-label={t('filterMenu.value')}
            data-autofocus />
          {#if form.id === 'duration'}
            <Dropdown floating={sheet} value={form.unit} options={durationUnits} onchange={(v) => (form!.unit = v)} label={t('filterMenu.unit')} />
          {:else if form.id === 'size'}
            <Dropdown floating={sheet} value={form.unit} options={sizeUnits} onchange={(v) => (form!.unit = v)} label={t('filterMenu.unit')} />
          {:else if form.id === 'tags'}
            <span class="word">{t('filterMenu.tagsWord')}</span>
          {:else if form.id === 'pages'}
            <span class="word">{t('filterMenu.pagesWord')}</span>
          {:else}
            <span class="word">{t('filterMenu.pixels')}</span>
          {/if}
        </div>
      {:else if form.id === 'type' || form.id === 'rating' || form.id === 'site'}
        <div class="segmented is" role="tablist" aria-label={t('filterMenu.isOrNot')}>
          <button type="button" role="tab" aria-selected={!form.not} onclick={() => (form!.not = false)}>{t('filterMenu.is')}</button>
          <button type="button" role="tab" aria-selected={form.not} onclick={() => (form!.not = true)}>{t('filterMenu.isNot')}</button>
        </div>
        {@const choices = form.id === 'type' ? typeChoices : form.id === 'rating' ? ratingChoices : siteChoices}
        <div class="choices" role="group" aria-label={fi(form.id).label}>
          {#each choices as c, k (c.value)}
            <button
              type="button"
              class="choice"
              aria-pressed={form.values.includes(c.value)}
              data-autofocus={k === 0 ? true : undefined}
              onclick={() => toggleValue(c.value, form!.id !== 'rating')}>
              {#if form.values.includes(c.value)}<Icon name="check" size={14} />{/if}{c.label}
            </button>
          {:else}
            <p class="hint">{t('filterMenu.noSites')}</p>
          {/each}
        </div>
      {:else if form.id === 'stars'}
        <div class="choices" role="group" aria-label={fi(form.id).label}>
          {#each starChoices as c, k (c.value)}
            <button type="button" class="choice" aria-pressed={form.values.includes(c.value)} data-autofocus={k === 0 ? true : undefined} onclick={() => toggleValue(c.value, false)}>
              {#if form.values.includes(c.value)}<Icon name="check" size={14} />{/if}{c.label}
            </button>
          {/each}
        </div>
      {:else if tagFilter(form.id)}
        <div class="choices" role="group" aria-label={fi(form.id).label}>
          {#if tagChoices === null}
            <p class="hint" transition:reveal>{t('common.loading')}</p>
          {:else}
            {#each tagChoices as c, k (c.value)}
              <button
                type="button"
                class="choice"
                aria-pressed={form.values.includes(c.value)}
                data-autofocus={k === 0 ? true : undefined}
                onclick={() => toggleValue(c.value, true)}>
                {#if form.values.includes(c.value)}<Icon name="check" size={14} />{/if}{c.label}{#if c.meta}<span class="choice-n">{c.meta}</span>{/if}
              </button>
            {:else}
              <p class="hint" transition:reveal>{t('filterMenu.noneYet')}</p>
            {/each}
          {/if}
        </div>
      {:else if form.id === 'limit'}
        <div class="row">
          <span class="word">{t('filterMenu.first')}</span>
          <input class="num" type="number" min="1" inputmode="numeric" value={form.num}
              oninput={(e) => (form!.num = e.currentTarget.value)} aria-label={t('filterMenu.howManyResults')} data-autofocus />
          <span class="word">{t('filterMenu.results')}</span>
        </div>
      {/if}

      <div class="foot">
        <span class="preview" aria-live="polite">{#if preview}<span class="chip-preview" transition:pop>{preview}</span>{/if}</span>
        <button type="submit" disabled={!text}>{editing ? t('saved.update') : t('filterMenu.add')}</button>
      </div>
    </form>
  {/if}
  </div>
  {/key}
  </div>
</div>
</div>

<style>
  .choice-n {
    margin-left: 6px;
    color: var(--overlay1);
    font-variant-numeric: tabular-nums;
  }
  /* Phones: a sheet like the details panel - edge to edge along the bottom, a
     grab bar (drag down to close), room for the home bar. */
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgb(17 17 27 / 0.5);
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  .panel.sheet {
    position: fixed;
    z-index: 61;
    left: 0;
    right: 0;
    bottom: 0;
    max-height: var(--sheet-max-h);
    overflow-y: auto;
    padding: 0 14px calc(30px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  /* A form is short: no scrolling, so an open dropdown floats over the page. */
  .panel.sheet.form-view {
    max-height: none;
    overflow: visible;
  }
  @media (prefers-reduced-motion: reduce) {
    .backdrop,
    .panel.sheet {
      animation: none;
    }
  }
  .grab-zone {
    position: sticky;
    top: 0;
    z-index: 2;
    display: grid;
    place-items: center;
    height: 22px;
    margin: 0 -14px -6px;
    background: var(--mantle);
    touch-action: none;
    cursor: grab;
  }
  .grab-zone span {
    width: 38px;
    height: 5px;
    border-radius: 3px;
    background: var(--surface2);
  }
  /* The order: one full-width button (it opens the Sort by page). */
  .sort-pick {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 0 12px;
    justify-content: flex-start;
    background: var(--crust);
    border: 1px solid var(--surface1);
    color: var(--text);
    font-weight: 500;
    text-align: left;
  }
  .sp-label {
    font-weight: 600;
  }
  .sp-from {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tick {
    display: grid;
    width: 16px;
    color: var(--mauve);
  }
  .sp-dir {
    flex: 1;
    color: var(--subtext);
  }
  .sort-pick > :global(svg) {
    flex: none;
    color: var(--subtext);
  }
  .pick-item.on .item-label {
    color: var(--mauve);
    font-weight: 600;
  }
  .meta {
    font-size: 12px;
    color: var(--overlay1);
  }
  /* Pages stack in one grid cell, so the old and the new can cross. */
  /* (.mpages and .mpage: app.css) */
  .mpage {
    gap: 10px;
    align-content: start;
  }
  .panel {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
    padding: 12px;
    max-height: min(70dvh, 640px);
    overflow-y: auto;
    overscroll-behavior: contain;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.5);
    outline: none;
  }
  /* A form is short and never scrolls, so an open dropdown floats over the
     page instead of being cut off inside the panel. */
  .panel.form-view {
    max-height: none;
    overflow: visible;
  }
  form {
    display: grid;
    gap: 12px;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 28px;
  }
  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: var(--font);
    font-weight: 700;
  }
  .back {
    margin-left: -6px;
  }
  h3 {
    margin: 4px 0 4px 2px;
    font-size: var(--font-small);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--overlay1);
  }
  /* Sort: [field ▾] [Newest | Oldest] Reset - one line, however many fields there are. */
  .sort-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 0 2px;
  }
  /* From: the site picker takes the whole width, like the order above it. */
  .sort-row > :global(.dropdown) {
    flex: 1;
    display: grid;
  }
  .sort-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .reset {
    min-height: 0;
    height: auto;
    padding: 2px 4px;
    background: none;
    color: var(--mauve);
    font-size: var(--font-small);
    font-weight: 600;
  }
  .items {
    display: grid;
    gap: 2px;
  }
  @media (min-width: 900px) {
    .panel {
      width: 660px; /* the four orders on one line */
      max-height: min(86dvh, 820px); /* every filter without scrolling */
    }
    .panel.form-view {
      width: 460px; /* a form needs less room than the list */
    }
    .items {
      grid-template-columns: 1fr 1fr;
    }
  }
  .item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    min-height: 0;
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
    flex: 1; /* a count or tick after it lines up at the right */
    display: grid;
    min-width: 0;
  }
  .item-label {
    color: var(--teal);
    font-weight: 600;
  }
  .item-hint {
    font-size: var(--font-small);
    color: var(--overlay1);
  }
  .hint {
    margin: -6px 0 0;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .num {
    width: 7.5em;
    flex: none;
  }
  .word {
    color: var(--subtext);
  }
  input[type='date'] {
    width: fit-content;
    color-scheme: dark;
  }
  .is {
    width: fit-content;
  }
  .is button {
    flex: none;
    padding: 0 16px;
  }
  .choices {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .choice {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-height: calc(var(--control-h) - 6px);
    padding: 0 12px;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--text);
    font-weight: 500;
  }
  .choice[aria-pressed='true'] {
    background: color-mix(in srgb, var(--teal) 22%, var(--surface0));
    color: var(--teal);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--teal) 60%, transparent);
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding-top: 4px;
  }
  .preview {
    min-width: 0;
  }
  .chip-preview {
    display: inline-block;
    max-width: 100%;
    padding: 3px 10px;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--teal);
    font-size: var(--font-small);
    font-weight: 500;
    overflow-wrap: anywhere;
  }
  .foot button[type='submit'] {
    flex: none;
  }
</style>
