<script lang="ts">
  // The search: typed booru-style text turns into chips; every chip can be
  // edited in place. Each change runs the search straight away.
  import { parse, format, chipText, atomText, label, kindOf, balanced, isTitleText, titleTerm, type Chip } from '../lib/query';
  import { pop as grow, moved, settle, sizeOf, nudge } from '../lib/motion';
  import type { Suggestion } from '../lib/api';
  import TagInput from './TagInput.svelte';
  import { tagColor } from '../lib/namespaces.svelte';
  import ChipEditor from './ChipEditor.svelte';
  import Icon from './Icon.svelte';
  import SavedPanel from './SavedPanel.svelte';
  import SystemMenu from './SystemMenu.svelte';
  import { read as readFilter, isFavorite, type Form } from '../lib/system';
  import { useScope } from '../lib/scope.svelte';
  import { settings } from '../lib/settings.svelte';
  import { onMount } from 'svelte';
  import { on } from 'svelte/events';
  import { cubicOut } from 'svelte/easing';
  import { newDownload } from '../lib/newDownload.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    query,
    sort = settings.defaultSort,
    onsearch,
    onsaved = (q: string) => onsearch(q),
    onsort = () => {},
    plain = false,
    sortable = false,
    label: barLabel = t('tagMenu.search'),
    placeholder = t('library.search'),
    defaultSort = undefined,
    sorts = [],
    suggest,
    browseSites = [],
    site = '',
    onsite = () => {},
  }: {
    query: string;
    sort?: string;
    onsearch: (q: string) => void;
    onsaved?: (q: string, sort: string, key?: string) => void; // a saved search or group tag picked: its search, order and menu entry
    onsort?: (sort: string) => void; // an order picked in the filter menu
    plain?: boolean; // only the search (e.g. in Settings): no saved searches, no order
    sortable?: boolean; // ... with the order still in the filter menu (Settings' Browse search: as on the page)
    label?: string;
    placeholder?: string;
    /** The page's default order (the sort button is marked when the order differs); the library's by default. */
    defaultSort?: string;
    /** Comics > Browse: the gallery site's other orders (the filter menu's sort). */
    sorts?: string[];
    /** Comics > Browse: the gallery site's own tags as suggestions. */
    suggest?: (word: string, signal: AbortSignal) => Promise<{ suggestions: Suggestion[] }>;
    /** Comics > Browse: the sites it can browse and the one browsed (the filter menu's From row). */
    browseSites?: string[];
    site?: string;
    onsite?: (id: string) => void;
  } = $props();
  /** The order this page counts as its default (the sort mark, the menu's "default" and Reset). */
  // (in Settings - plain, sortable - the order picked is the default being set: nothing there is "not the default")
  const pageDefault = $derived(plain && sortable ? sort : (defaultSort ?? settings.defaultSort));
  const { saved, kind } = useScope();
  const comics = kind === 'comics' || kind === 'browse'; // (the Comics page and Browse: suggestions from the comics' tags)
  const browse = kind === 'browse'; // (a site's search: its own filters and orders, no saved searches)

  let chips = $state<Chip[]>([]);
  // Each chip's key, kept across changes: the same text keeps its chip; a changed one in the same place (an edit)
  // keeps its place's; others are new. So chips glide when moved, pop in and out when added or removed, and an
  // edited chip changes in place.
  let prevKeys: { text: string; key: string }[] = [];
  let nextKey = 0;
  const chipKeys = $derived.by(() => {
    const texts = chips.map(chipText);
    const used = new Set<string>();
    const keys: (string | null)[] = texts.map((tx) => {
      const m = prevKeys.find((p) => p.text === tx && !used.has(p.key));
      if (!m) return null;
      used.add(m.key);
      return m.key;
    });
    const out = keys.map((k, i) => {
      if (k) return k;
      const old = prevKeys[i];
      if (old && !used.has(old.key) && !texts.includes(old.text)) {
        used.add(old.key);
        return old.key;
      }
      return 'c' + nextKey++;
    });
    prevKeys = texts.map((text, i) => ({ text, key: out[i] }));
    return out;
  });
  // The sites picked in the search (system:site=a,b chips): suggestions only from their files' tags.
  let sites = $derived(
    chips
      .flatMap((c) => (c.atoms.length === 1 && !c.atoms[0].neg ? [c.atoms[0].text.match(/^system:site\s*=\s*(.+)$/i)?.[1] ?? ''] : []))
      .filter(Boolean)
      .join(','),
  );
  let draft = $state('');
  let editing = $state<number | null>(null);
  let input = $state<HTMLInputElement>();
  let savedOpen = $state(false);
  // The filter menu: adding a filter, or editing the filter chip at `index`.
  let filterMenu = $state<{ index: number | null; form: Form | null } | null>(null);
  let filterBtn = $state<HTMLButtonElement>();

  /** A chip the filter menu can edit (one filter it makes). */
  const filterForm = (c: Chip) => (c.atoms.length === 1 ? readFilter(atomText(c.atoms[0])) : null);

  function addFilter(text: string) {
    const added = parse(text);
    if (filterMenu?.index != null) split(filterMenu.index, added);
    else run([...chips, ...added]);
  }
  let starBtn = $state<HTMLButtonElement>();
  // The star is filled when the current search (and order) is a saved one.
  let current = $derived(saved.list.length ? saved.find(format(chips), sort, pageDefault) : undefined);
  onMount(() => {
    if (!saved.loaded) saved.load();
  });

  // The menus under the search bar drop in a little and fade (not with reduced motion).
  function pop(node: Element, { out = false } = {}) {
    // A closing menu can't be pressed (Svelte makes it inert) or read while it
    // fades; opened again before it has gone, it is both again.
    if (out) node.setAttribute('aria-hidden', 'true');
    else node.removeAttribute('aria-hidden');
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return { duration: 0 };
    return {
      duration: 180,
      easing: cubicOut,
      css: (t: number) => `opacity: ${t}; transform: translateY(${(1 - t) * -8}px) scale(${settle(0.96 + 0.04 * t, sizeOf(node))})`,
    };
  }

  // The chips scroll sideways: keep the typing box in view while typing and
  // when a chip is added.
  let fieldEl = $state<HTMLElement>();
  let chipsEl = $state<HTMLElement>();
  // A chip added from elsewhere too (a tag in the details: "add to the search"):
  // the end comes into view, where the new chip and the typing box are.
  let chipCount = 0; // (a search filled in from nothing - the page opening - reads from its start)
  $effect(() => {
    const n = chips.length;
    void draft;
    const added = chipCount > 0 && n > chipCount;
    chipCount = n;
    if (chipsEl && (added || document.activeElement === input)) {
      // (and while a new chip opens to its width: the end keeps in view)
      const el = chipsEl;
      const until = performance.now() + (added ? 340 : 0);
      const follow = () => {
        el.scrollLeft = el.scrollWidth;
        if (performance.now() < until) requestAnimationFrame(follow);
      };
      follow();
    }
    queueMicrotask(edges);
  });
  // Soft edges where more chips are scrolled out of view.
  let fadeL = $state(false);
  let fadeR = $state(false);
  function edges() {
    if (!chipsEl) return;
    fadeL = chipsEl.scrollLeft > 2;
    fadeR = chipsEl.scrollLeft + chipsEl.clientWidth < chipsEl.scrollWidth - 2;
  }

  // Follow the query when it changes from outside (URL, a tag tapped in the viewer).
  $effect(() => {
    chips = parse(query);
  });

  function run(next: Chip[]) {
    // A typed sort: goes to the sort menu (the parent takes it out of the
    // query), so it never shows as a chip.
    chips = next.filter((c) => !(c.atoms.length === 1 && kindOf(c.atoms[0].text) === 'sort'));
    onsearch(format(next));
  }

  function commitDraft() {
    // Comics: "/some title" is one chip (a title search), spaces and all.
    const added = parse(comics && isTitleText(draft) ? titleTerm(draft) : draft);
    draft = '';
    run([...chips, ...added]);
  }

  // In Settings there's no "search now": a word left in the box counts when
  // you leave it (a moment later, so picking a suggestion goes first).
  function left() {
    if (!plain) return;
    setTimeout(() => {
      if (document.activeElement !== input && draft.trim() && balanced(draft)) commitDraft();
    }, 200);
  }

  function changed(text: string) {
    // A finished word (space typed, no open group) becomes a chip at once - not in a title search (Enter ends it).
    if (comics && isTitleText(text)) return;
    if (/\s$/.test(text) && text.trim() && balanced(text)) commitDraft();
  }

  function backspaceEmpty() {
    if (!chips.length) return;
    const last = chips[chips.length - 1];
    chips = chips.slice(0, -1);
    draft = chipText(last);
  }

  function edit(i: number, c: Chip | null) {
    const next = chips.slice();
    if (c) next[i] = c;
    else next.splice(i, 1);
    run(next);
  }

  // The × on a chip: out of the search at once (an open editor follows its chip).
  function removeChip(i: number) {
    if (filterMenu?.index != null) filterMenu = null;
    if (editing === i) editing = null;
    else if (editing !== null && editing > i) editing--;
    edit(i, null);
  }

  // ---- Drag a chip onto another: they become one OR group (in the target's
  // place). Mouse: drag past a few pixels. Touch: press and hold, then slide (a
  // quick tap edits, a swipe scrolls). Limit and sort chips don't combine.
  const HOLD_MS = 350;
  type Drag = {
    i: number;
    id: number; // pointer
    x0: number;
    y0: number;
    x: number;
    y: number;
    dx: number; // where the chip was grabbed, from its top-left corner
    dy: number;
    touch: boolean;
    active: boolean;
    over: number | null;
    el: HTMLElement;
  };
  let drag = $state<Drag | null>(null);
  let holdTimer = 0;
  let suppressClick = false; // the click that ends a drag doesn't open the editor
  let stopTouchScroll: (() => void) | null = null;

  // The floating chip lives at the top of the page (above the top bar and every layer).
  function toBody(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  const combinable = (c: Chip) => c.atoms.every((a) => kindOf(a.text) !== 'limit' && kindOf(a.text) !== 'sort');

  function chipDown(e: PointerEvent, i: number) {
    if (e.button !== 0 || !combinable(chips[i]) || chips.length < 2) return;
    const el = e.currentTarget as HTMLElement;
    const r = el.closest('.chip')!.getBoundingClientRect();
    drag = { i, id: e.pointerId, x0: e.clientX, y0: e.clientY, x: e.clientX, y: e.clientY, dx: e.clientX - r.left, dy: e.clientY - r.top, touch: e.pointerType !== 'mouse', active: false, over: null, el };
    if (drag.touch) holdTimer = setTimeout(startDrag, HOLD_MS);
  }

  function startDrag() {
    if (!drag || drag.active) return;
    drag.active = true;
    savedOpen = false;
    editing = null;
    filterMenu = null;
    try {
      drag.el.setPointerCapture(drag.id);
    } catch {
      /* the pointer is gone */
    }
    // Touch: the page mustn't scroll under the finger while a chip is held.
    if (drag.touch) stopTouchScroll = on(window, 'touchmove', (ev) => ev.preventDefault(), { passive: false });
  }

  function chipMove(e: PointerEvent) {
    if (!drag || e.pointerId !== drag.id) return;
    const moved = Math.hypot(e.clientX - drag.x0, e.clientY - drag.y0);
    if (!drag.active) {
      if (drag.touch) {
        if (moved > 8) cancelDrag(); // a scroll, not a hold
        return;
      }
      if (moved < 6) return;
      startDrag();
    }
    drag.x = e.clientX;
    drag.y = e.clientY;
    const t = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('[data-chip]');
    const k = t ? Number(t.dataset.chip) : null;
    drag.over = k !== null && k !== drag.i && combinable(chips[k]) ? k : null;
  }

  function chipUp(e: PointerEvent) {
    if (!drag || e.pointerId !== drag.id) return;
    const d = drag;
    cancelDrag();
    if (!d.active) return; // a tap: the click handler edits
    suppressClick = true;
    setTimeout(() => (suppressClick = false), 0);
    if (d.over !== null) combine(d.i, d.over);
  }

  function cancelDrag() {
    clearTimeout(holdTimer);
    stopTouchScroll?.();
    stopTouchScroll = null;
    drag = null;
  }

  /** Puts chip `from`'s alternatives into chip `to` (an OR group) and removes `from`. */
  function combine(from: number, to: number) {
    const seen = new Set(chips[to].atoms.map((a) => (a.neg ? '-' : '') + a.text.toLowerCase()));
    const atoms = [...chips[to].atoms];
    for (const a of chips[from].atoms) {
      const k = (a.neg ? '-' : '') + a.text.toLowerCase();
      if (!seen.has(k)) {
        seen.add(k);
        atoms.push({ ...a });
      }
    }
    const next = chips.map((c, k) => (k === to ? { atoms } : c)).filter((_, k) => k !== from);
    run(next);
  }

  /** Where chip i starts, from the search bar's left edge (its menu opens under it). */
  function chipLeft(i: number) {
    const el = fieldEl?.querySelector<HTMLElement>(`[data-chip="${i}"]`);
    if (!el || !fieldEl) return 0;
    return Math.max(0, el.getBoundingClientRect().left - fieldEl.getBoundingClientRect().left);
  }

  function split(i: number, parts: Chip[]) {
    const next = chips.slice();
    next.splice(i, 1, ...parts);
    run(next);
  }
</script>

{#snippet atomsOf(c: Chip)}
  {#each c.atoms as a, j}
    {#if j > 0}<span class="or">{t('searchBar.or')}</span>{/if}
    <span class="atom {kindOf(a.text)}" class:neg={a.neg} style:--c={tagColor(a.text)}
      >{#if a.neg && !(a.text === 'system:has_audio' || isFavorite(a.text))}<span class="minus">−</span>{/if}{label(a, comics)}</span
    >
  {/each}
{/snippet}

<div class="bar" class:plain>
  <div class="field" role="group" aria-label={barLabel} bind:this={fieldEl}>
    {#if chips.length || draft}
      <!-- Something to clear: the magnifier becomes the clear button. -->
      <button
        class="icon lead clear"
        aria-label={t('searchBar.clear')}
        title={t('searchBar.clear')}
        onclick={() => {
          draft = '';
          editing = null;
          run([]);
          input?.focus();
        }} transition:nudge><Icon name="close" size={18} /></button>
    {:else}
      <span class="lead"><Icon name="search" size={18} /></span>
    {/if}
    <!-- The chips and the typing box: one line, scrolling sideways; a tap on the
         empty space after them starts typing. -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (a bigger target for the input, which has its own keys) -->
    <div class="chips-wrap" class:fade-l={fadeL} class:fade-r={fadeR}>
    <div class="chips" style:--typed={draft.length} bind:this={chipsEl} onscroll={edges} onclick={(e) => e.target === e.currentTarget && input?.focus()}>
    {#each chips as c, i (chipKeys[i])}
      <span
        animate:moved
        transition:nudge
        class="chip"
        data-chip={i}
        class:editing={editing === i}
        class:group={c.atoms.length > 1}
        class:lifted={drag?.active && drag.i === i}
        class:target={drag?.active && drag.over === i}>
        <button
          class="chip-main"
          onpointerdown={(e) => chipDown(e, i)}
          onpointermove={chipMove}
          onpointerup={chipUp}
          onpointercancel={cancelDrag}
          oncontextmenu={(e) => drag && e.preventDefault()}
          onclick={() => {
            if (suppressClick) return;
            savedOpen = false;
            const form = filterForm(c);
            if (form) {
              editing = null;
              filterMenu = filterMenu?.index === i ? null : { index: i, form };
            } else {
              filterMenu = null;
              editing = editing === i ? null : i;
            }
          }}
          title={combinable(c) && chips.length > 1 ? t('searchBar.chipTitleDrag', { chip: chipText(c) }) : t('searchBar.chipTitle', { chip: chipText(c) })}>
          {@render atomsOf(c)}
        </button>
        <button class="chip-x" aria-label={t('searchBar.removeChip', { chip: comics && c.atoms.length === 1 && kindOf(c.atoms[0].text) === 'title' ? (c.atoms[0].neg ? '-' : '') + label(c.atoms[0], comics) : chipText(c) })} title={t('sites.remove')} onclick={() => removeChip(i)}
          ><Icon name="close" size={14} /></button>
      </span>
    {/each}
    <TagInput
      bind:value={draft}
      bind:inputEl={input}
      {comics}
      {sites}
      {suggest}
      multi
      exclude={chips.flatMap((c) => c.atoms.map((a) => a.text))}
      anyOf={browse ? () => null : (tags) => `( ${tags.join(' ~ ')} )`}
      label={barLabel}
      placeholder={chips.length ? '' : placeholder}
      oninputchange={changed}
      onblur={left}
      onenter={commitDraft}
      onbackspaceempty={backspaceEmpty}
      anchor={fieldEl} />
    </div>
    </div>
    <span class="tail">
      <button
        bind:this={filterBtn}
        class="icon filt"
        aria-label={t('searchBar.filters')}
        class:changed={!plain && sort !== pageDefault}
        aria-expanded={filterMenu !== null && filterMenu.index === null}
        title={plain && !sortable ? t('searchBar.filters') : !plain && sort !== pageDefault ? t('searchBar.sortFiltersChanged') : t('searchBar.sortFilters')}
        onclick={() => {
          editing = null;
          savedOpen = false;
          filterMenu = filterMenu && filterMenu.index === null ? null : { index: null, form: null };
        }}><Icon name="filter" size={19} />{#if !plain && sort !== pageDefault}<span class="dot" transition:grow></span>{/if}</button>
      {#if !plain && !browse}
      <button
        bind:this={starBtn}
        class="icon star"
        class:on={!!current}
        aria-label={(current ? t('searchBar.savedAs', { name: current.name }) : t('saved.title')) + (saved.anyNew ? t('searchBar.newFiles') : '')}
        aria-expanded={savedOpen}
        title={current ? t('searchBar.savedAsTitle', { name: current.name }) : t('saved.title')}
        onclick={() => {
          editing = null;
          filterMenu = null;
          savedOpen = !savedOpen;
        }}><Icon name="star" size={19} filled={!!current} />{#if saved.anyNew}<span class="dot" aria-hidden="true" transition:grow></span>{/if}</button>
      {/if}
    </span>
  </div>
  {#if drag?.active}
    <div use:toBody class="drag-chip" class:group={chips[drag.i]?.atoms.length > 1} style:transform="translate({drag.x - drag.dx}px, {drag.y - drag.dy - (drag.touch ? 52 : 0)}px)" aria-hidden="true">
      {#if chips[drag.i]}{@render atomsOf(chips[drag.i])}{/if}
    </div>
  {/if}
  {#if savedOpen}
    <div class="editor saved-pos" in:pop|global out:pop|global={{ out: true }}>
      <SavedPanel
        query={format(chips)}
        {sort}
        defaultSort={pageDefault}
        onrun={(q, s, key) => onsaved(q, s, key)}
        onclose={(refocus) => {
          savedOpen = false;
          if (refocus) starBtn?.focus();
        }} />
    </div>
  {/if}
  <!-- {#key} outside {#if}: each opening is a new menu, the closing one fades out on its own. -->
  {#key filterMenu}
    {#if filterMenu}
      <div class="editor" class:saved-pos={filterMenu.index === null} class:wide={filterMenu.index === null} in:pop|global out:pop|global={{ out: true }}>
        <SystemMenu
          initial={filterMenu.form}
          noSort={plain && !sortable}
          {sorts}
          {browseSites}
          {site}
          {onsite}
          {sort}
          defaultSort={pageDefault}
          onsort={(s) => onsort(s)}
          query={format(chips)}
          onquery={(q) => run(parse(q))}
          onadd={addFilter}
          onclose={(refocus) => {
            const adding = filterMenu?.index === null;
            filterMenu = null;
            if (refocus && adding) filterBtn?.focus();
          }} />
      </div>
    {/if}
  {/key}
  {#key editing}
    {#if editing !== null && chips[editing]}
      <div class="editor chip-pos" style:--chip-left="{chipLeft(editing)}px" in:pop|global out:pop|global={{ out: true }}>
        <ChipEditor
          chip={chips[editing]}
          onchange={(c) => edit(editing!, c)}
          onsplit={(parts) => split(editing!, parts)}
          ononly={(c) => run([c])}
          ondownload={(tags) => {
            // The Downloads form on the site they came from (asked when that's more than one), each tag
            // written that site's way (#tag, @name, ...); an OR group as the site's OR search.
            newDownload(tags, { any: tags.length > 1 });
          }}
          onclose={() => (editing = null)} />
      </div>
    {/if}
  {/key}
</div>

<style>
  .bar {
    position: relative;
  }
  .field {
    display: flex;
    align-items: center;
    gap: 2px;
    /* Just tall enough for a chip: a control's height on phones, a little more
       with a mouse (chips have a floor there). */
    min-height: max(var(--control-h), 36px);
    padding: 1px 2px 1px 4px;
    background: var(--crust);
    border: 1px solid var(--surface1);
    border-radius: 12px;
    transition:
      border-color 0.15s,
      box-shadow 0.15s;
  }
  /* In a form (Settings): as tall as the other fields. */
  .plain .field {
    min-height: var(--control-h);
    padding-block: 0;
  }
  .plain .chips {
    padding-block: 2px;
  }
  /* Only while typing: pressing the star or a chip must not flash the border. */
  .field:has(:global(input:focus)) {
    border-color: var(--mauve);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--mauve) 18%, transparent);
  }
  .lead {
    flex: none;
    color: var(--overlay0);
    display: inline-grid;
    place-items: center;
    width: 30px;
  }
  .lead.clear {
    min-width: 0;
    height: calc(var(--control-h) - 6px);
    min-height: 0;
    padding: 0;
    color: var(--subtext);
  }
  /* Chips and the typing box: one line that scrolls sideways (no scrollbar);
     the typing box keeps some width at the end, an easy place to tap. */
  .chips {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 4px;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: none;
    /* Room for a chip's outline (editing / drop target): the sideways scroll clips at its edges. */
    padding: 4px 4px;
  }
  /* Soft edges where more chips are out of view: overlays (a mask would also hide
     the suggestions list inside). */
  .chips-wrap {
    position: relative;
    flex: 1;
    min-width: 0;
    display: flex;
  }
  .chips-wrap::before,
  .chips-wrap::after {
    content: '';
    position: absolute;
    z-index: 1;
    top: 0;
    bottom: 0;
    width: 20px;
    pointer-events: none;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .chips-wrap::before {
    left: 0;
    background: linear-gradient(to right, var(--crust), transparent);
  }
  .chips-wrap::after {
    right: 0;
    background: linear-gradient(to left, var(--crust), transparent);
  }
  .chips-wrap.fade-l::before,
  .chips-wrap.fade-r::after {
    opacity: 1;
  }
  .chips::-webkit-scrollbar {
    display: none;
  }
  .chips > :global(*) {
    flex: none;
  }
  /* The typing box: a small target after the chips that grows with what is typed
     (and fills the line when the chips leave room). */
  .chips > :global(.wrap) {
    flex: 1 0 0; /* not the input's default width: only what is left, or the minimum */
    min-width: calc(2.5em + var(--typed, 0) * 1ch);
  }
  .field :global(input) {
    border: 0;
    background: transparent;
    min-width: 0;
    /* A chip's height, not a whole control's: it sits in the row of chips. */
    min-height: 0;
    height: max(calc(var(--control-h) - 12px), 26px);
    padding: 0 4px;
  }
  .field :global(input:focus) {
    outline: none;
    box-shadow: none;
  }
  .chip {
    display: inline-flex;
    align-items: stretch;
    min-height: max(calc(var(--control-h) - 12px), 26px);
    border-radius: 999px;
    background: var(--surface0);
    flex: none; /* always the whole tag: the row scrolls instead */
  }
  .chip button {
    min-height: 0;
    height: auto;
    background: none;
    border-radius: 0;
    font-weight: 500;
  }
  /* Pointing at a chip: the whole round pill reacts (the buttons inside are
     square: no highlight of their own). The ×: a red tint - this goes. */
  @media (hover: hover) {
    .chip button:hover {
      background-image: none;
    }
    .chip:has(.chip-main:hover) {
      background: var(--surface1);
    }
    .chip:has(.chip-x:hover) {
      background: color-mix(in srgb, var(--red) 14%, var(--surface0));
    }
  }
  .chip-main {
    display: inline-flex;
    align-items: center;
    justify-content: flex-start;
    gap: 4px;
    min-width: 0;
    overflow: hidden;
    padding: 0 4px 0 10px;
    color: var(--text);
  }
  .chip-x {
    flex: none;
    padding: 0 8px 0 4px;
    color: var(--subtext);
  }
  @media (hover: hover) {
    .chip-x:hover {
      color: var(--red);
      filter: none;
    }
  }
  .chip.group {
    background: color-mix(in srgb, var(--mauve) 18%, var(--surface0));
  }
  /* Dragging a chip: it lifts out (the floating copy follows the pointer), the
     chip under it shows it will become an OR group. */
  .chip-main {
    /* Swipes on a chip still scroll the row (sideways) or the page; holding it still drags it. */
    touch-action: pan-x pan-y;
    -webkit-touch-callout: none;
    user-select: none;
    -webkit-user-select: none;
  }
  .chip.lifted {
    opacity: 0.35;
  }
  .chip.target {
    outline: 2px dashed var(--mauve);
    outline-offset: 1px;
    background: color-mix(in srgb, var(--mauve) 22%, var(--surface0));
  }
  .chip.target .chip-main::after {
    content: 'OR';
    margin-left: 2px;
    font-size: 10px;
    font-weight: 700;
    color: var(--mauve);
  }
  /* On touch it floats above the finger, so the chip it is dropped on stays visible. */
  .drag-chip {
    position: fixed;
    left: 0;
    top: 0;
    z-index: 60;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: calc(var(--control-h) - 6px);
    padding: 0 12px;
    border-radius: 999px;
    background: var(--surface1);
    box-shadow: 0 10px 28px rgb(0 0 0 / 0.5);
    pointer-events: none;
    font-weight: 500;
    scale: 1.05;
  }
  .drag-chip.group {
    background: color-mix(in srgb, var(--mauve) 22%, var(--surface1));
  }
  .chip.editing {
    outline: 2px solid var(--mauve);
  }
  .atom {
    white-space: nowrap;
    text-align: left;
  }
  .atom.tag,
  .atom.wildcard {
    color: var(--c); /* the namespace color */
  }
  .atom.neg:not(.tag, .wildcard) {
    color: var(--red);
  }
  .minus {
    color: var(--red);
    font-weight: 700;
  }
  .atom.system {
    color: var(--teal);
  }
  .atom.sort,
  .atom.limit {
    color: var(--peach);
  }
  .atom.wildcard {
    color: var(--yellow);
  }
  .atom.title {
    color: var(--text);
  }
  .or {
    font-size: 10px;
    font-weight: 700;
    color: var(--mauve);
  }
  /* Filters and saved searches: slim buttons (a touch target is still tall). */
  .tail {
    flex: none;
    display: inline-flex;
  }
  .tail > :global(button) {
    width: 34px;
    min-width: 0;
    min-height: 0;
    height: calc(var(--control-h) - 6px);
    padding: 0;
  }
  .star {
    position: relative;
    color: var(--overlay0);
  }
  .filt {
    position: relative;
    color: var(--overlay0);
  }
  /* The order isn't the default one (filters); new files in a saved search or group tag (star). */
  .dot {
    position: absolute;
    top: calc(50% - 11px);
    right: 4px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--mauve);
  }
  .filt[aria-expanded='true'] {
    color: var(--teal);
  }
  .star.on,
  .star[aria-expanded='true'] {
    color: var(--mauve);
  }
  .editor {
    transform-origin: top center;
    position: absolute;
    z-index: 25;
    left: 0;
    right: 0;
    top: calc(100% + 6px);
  }
  /* A chip's menu: under the chip (kept inside the search bar's width); a sheet on phones. */
  @media (min-width: 640px) {
    .editor.chip-pos {
      right: auto;
      width: 340px;
      left: max(0px, min(var(--chip-left), calc(100% - 340px)));
    }
  }
  @media (min-width: 900px) {
    .editor {
      right: auto;
      width: 520px;
    }
    /* Under the star, at the right end of the search bar. */
    .editor.saved-pos {
      left: auto;
      right: 0;
      width: 460px;
    }
    /* The filter list: two columns, so every filter shows without scrolling. */
    .editor.wide {
      width: auto; /* the menu sizes itself: wide for the list, narrower for a form */
    }
  }
</style>
