<script lang="ts">
  // A text input that autocompletes the word being typed against the
  // library's tags (with counts). Used for searching, editing chips and
  // tagging files. Tags already there (in the box, or exclude: the search's chips) aren't suggested; with anyOf,
  // suggestions can be marked - from several words typed one after another - and go in together: all of them, or
  // any of them (an OR group). With recent: the page's recent searches over them (the search bar: all of them while the
  // box is empty, the ones matching the first word typed), each run with a tap.
  import { api, type Suggestion, type RecentSearch } from '../lib/api';
  import type { Snippet } from 'svelte';
  import { pop, moved, nudge, reduced, onPixels } from '../lib/motion';
  import { cubicOut } from 'svelte/easing';
  import { splitTag, tagColor, nsLabel } from '../lib/namespaces.svelte';
  import SubMark from './SubMark.svelte';
  import { t, num } from '../lib/i18n.svelte';
  import { isFilterWord } from '../lib/query';
  import Icon from './Icon.svelte';
  import { overlayScroll } from '../lib/overlayScroll';

  let {
    value = $bindable(''),
    placeholder = '',
    multi = false, // several words: complete the last one, add a space after picking
    autofocus = false,
    label = t('tagInput.label'),
    onenter, // Enter with no suggestion selected
    onbackspaceempty,
    onescape,
    oninputchange,
    onblur,
    inputEl = $bindable<HTMLInputElement | undefined>(undefined),
    anchor,
    span,
    above: inSheet = false,
    bare = false,
    comics,
    sites = '',
    format,
    suggest,
    exclude = [],
    anyOf,
    recent = [],
    recentRow,
    recentHead = false,
    onrecent,
    onrecentremove,
    onrecentclear,
    zone,
  }: {
    value?: string;
    placeholder?: string;
    multi?: boolean;
    autofocus?: boolean;
    label?: string;
    onenter?: (text: string) => void;
    onbackspaceempty?: () => void;
    onescape?: () => void;
    oninputchange?: (text: string) => void;
    onblur?: () => void;
    /** Pressing outside this (and the list) while the list is open only closes it - the press does nothing else (the
     *  search bar: its whole bar; 2026-11-13, the user: "clicking off of it should close it instead of interacting with
     *  the page"). None: a press elsewhere closes it and goes on (a form's Add button works at once). */
    zone?: HTMLElement;
    inputEl?: HTMLInputElement;
    anchor?: HTMLElement; // the suggestions take this element's width and sit under it
    span?: string; // the box's ancestor (a selector) the suggestions are as wide as, still right under the box
    /** In a phone sheet: the suggestions are part of it, above the box (the sheet grows up from the keyboard). */
    above?: boolean;
    bare?: boolean; // a pick inserts the tag without its namespace (a site's own search)
    // Suggestions from the comics' tags instead of the library's: true = all, a plugin ID = that site's galleries'.
    comics?: boolean | string;
    /** Suggestions only from the tags of files from these sites (comma-separated: system:site='s list). */
    sites?: string;
    format?: (tag: string) => string; // how a pick is written (e.g. the site's own search syntax)
    /** Suggestions from elsewhere (Comics > Browse: the gallery site's own tags), asked a little less eagerly. */
    suggest?: (word: string, signal: AbortSignal) => Promise<{ suggestions: Suggestion[] }>;
    /** Tags not to suggest (already in the search). */
    exclude?: string[];
    /** Marking suggestions (multi): how several marked tags are written as one "any of them" group (null: they can't be). */
    anyOf?: (tags: string[]) => string | null;
    /** Recent searches to offer while the box has the focus (the search bar decides which), drawn by recentRow. */
    recent?: RecentSearch[];
    recentRow?: Snippet<[RecentSearch]>;
    /** Their heading with Clear (the whole list, the box empty). */
    recentHead?: boolean;
    onrecent?: (r: RecentSearch) => void;
    onrecentremove?: (r: RecentSearch) => void;
    onrecentclear?: () => void;
  } = $props();

  // Under the anchor, as wide as it (fixed: the search box scrolls its chips sideways).
  // Upwards (a sheet's box near the keyboard): its bottom on the box's top, growing up to the top of what can be seen.
  // Placed in the sheet's own terms - the bottom the sheet itself was given (its CSS: above the keyboard, --kb) plus
  // the box's height over the sheet's bottom, a difference within the sheet; its room the visible height (--view-h)
  // less that. (The page's coordinates and innerHeight don't agree with the fixed layer's on an iPhone with the
  // keyboard up: the list hung far above the box - the user, twice: "the suggestions still float".)
  let place = $state<{ left: number; width: number; top?: number; bottom?: number; room?: number } | null>(null);
  /** The suggestions come and go as a menu does: dropping in a few px from the field (rising, when they open above
   *  it) as they fade. (They popped in and vanished at once - seen in a motion recording.) */
  function drop(node: Element, { out = false } = {}) {
    const el = node as HTMLElement;
    if (out) {
      el.dataset.leaving = ''; // (going: not the list a new word opens, nothing to press)
      el.style.pointerEvents = 'none';
    } else {
      // (back before it had gone: the same list comes in again)
      delete el.dataset.leaving;
      el.style.pointerEvents = '';
    }
    if (reduced()) return { duration: 0 };
    if (!out) onPixels(node);
    const from = place?.bottom !== undefined ? 6 : -6;
    return { duration: out ? 120 : 160, easing: cubicOut, css: (t: number) => `opacity: ${t}; transform: translateY(${(1 - t) * from}px)` };
  }
  // Never taller than the room left above the on-screen keyboard (and the bar
  // iOS puts on top of it): the visual viewport ends there.
  let maxHeight = $state<number | null>(null);
  /** The fixed layer the box sits in (a sheet or dialog), if any. */
  function fixedHost(el: HTMLElement | undefined): HTMLElement | null {
    for (let e = el?.parentElement; e && e !== document.body; e = e.parentElement) if (getComputedStyle(e).position === 'fixed') return e;
    return null;
  }
  // In a sheet (above): nothing floats - the list sits over the box in the sheet, which grows up from the keyboard
  // (floating over the page, placed by the page's coordinates, it hung far above the box on an iPhone with the
  // keyboard up - the user: "the suggestions still float"). Its height: what the sheet can still grow by, measured
  // within the sheet (its max-height less the rest of it).
  let listEl = $state<HTMLElement>();
  function fitInSheet() {
    const host = inputEl?.closest<HTMLElement>('[data-sheet]');
    if (!host) return;
    const max = parseFloat(getComputedStyle(host).maxHeight);
    if (!Number.isFinite(max)) return;
    const rest = host.scrollHeight - (listEl?.offsetHeight ?? 0);
    maxHeight = Math.max(96, Math.floor(max - rest - 8));
  }
  // Not anchored, with a span: the list stays in the box's own wrapper, moved out to the span's sides.
  let spread = $state<{ left: number; width: number } | null>(null);
  function placeList() {
    if (inSheet) return fitInSheet();
    const box = (anchor ?? inputEl)?.getBoundingClientRect();
    if (!box) return;
    const wide = (span ? inputEl?.closest(span)?.getBoundingClientRect() : null) ?? box;
    if (span && !anchor) {
      const from = inputEl?.parentElement?.getBoundingClientRect().left ?? wide.left;
      spread = { left: wide.left - from, width: wide.width };
    }
    const vv = window.visualViewport;
    // (the visible bottom: above the keyboard - --kb, as the sheets reckon it; else the visual viewport's)
    const kb = parseFloat(document.documentElement.style.getPropertyValue('--kb'));
    const bottom = Number.isFinite(kb) && kb > 0 ? innerHeight - kb : vv ? vv.offsetTop + vv.height : innerHeight;
    const below = bottom - (box.bottom + 4) - 8;
    const above = box.top - 4 - (vv ? vv.offsetTop : 0) - 8;
    // Anchored, with little room below (a sheet's box near the keyboard) and more above: it opens upwards.
    if (anchor && below < 200 && above > below) {
      const host = fixedHost(anchor);
      const hb = host ? parseFloat(getComputedStyle(host).bottom) : NaN;
      if (host && Number.isFinite(hb)) {
        const over = host.getBoundingClientRect().bottom - box.top + 4; // (the list's bottom over the sheet's)
        place = { left: wide.left, width: wide.width, bottom: hb + over, room: over + 8 };
        maxHeight = null;
        return;
      }
      place = { left: wide.left, width: wide.width, bottom: innerHeight - box.top + 4 };
      maxHeight = Math.min(420, above);
      return;
    }
    if (anchor) place = { left: wide.left, top: box.bottom + 4, width: wide.width };
    maxHeight = Math.max(120, Math.min(420, below));
  }
  $effect(() => {
    if (!listShown) return;
    placeList();
    const vv = window.visualViewport;
    addEventListener('resize', placeList);
    addEventListener('scroll', placeList, true);
    vv?.addEventListener('resize', placeList);
    vv?.addEventListener('scroll', placeList);
    // A box that moves without an event (a sheet gliding up with the keyboard, a dialog settling): followed each frame.
    let raf = 0;
    let last = '';
    const follow = () => {
      const r = (anchor ?? inputEl)?.getBoundingClientRect();
      const now = r ? `${r.left},${r.top},${r.width},${r.bottom}` : '';
      if (now !== last) {
        last = now;
        placeList();
      }
      raf = requestAnimationFrame(follow);
    };
    if (anchor) raf = requestAnimationFrame(follow);
    return () => {
      cancelAnimationFrame(raf);
      removeEventListener('resize', placeList);
      removeEventListener('scroll', placeList, true);
      vv?.removeEventListener('resize', placeList);
      vv?.removeEventListener('scroll', placeList);
    };
  });

  let suggestions = $state<Suggestion[]>([]);
  let shownFor = $state(''); // the word they were found for (marked in each)
  /** A suggestion's name split around what was typed ([before, typed, after]; nothing marked: a wildcard, or not in it). */
  function marked(name: string): [string, string, string] {
    let w = shownFor.toLowerCase();
    if (w.includes(':')) w = w.slice(w.indexOf(':') + 1); // (ns:text - the namespace is its chip)
    const i = w && !w.includes('*') ? name.toLowerCase().indexOf(w) : -1;
    return i < 0 ? [name, '', ''] : [name.slice(0, i), name.slice(i, i + w.length), name.slice(i + w.length)];
  }
  let selected = $state(-1);
  let open = $state(false);
  let timer: number | undefined;
  // Closing after a blur waits a moment, so a click on a suggestion lands first.
  let blurTimer: number | undefined;
  let left = false; // the focus went elsewhere (not just away and back at once)
  let abort: AbortController | undefined;
  const listId = 'sugg-' + Math.random().toString(36).slice(2);

  // Anchored: the list lives at the top of the page (a dialog would crop it - or, moved with transform, misplace it).
  function toBody(node: HTMLElement) {
    document.body.appendChild(node);
    return () => node.remove();
  }

  /** The word being completed: its start in the text, the leading "-" if any, and the word itself. */
  function currentWord() {
    // (the end of a highlighted part: from its start, a pick went in front of the highlighted word - "blue_sky blu" -
    // and never became a chip)
    const caret = inputEl?.selectionEnd ?? value.length;
    const before = value.slice(0, caret);
    const start = multi ? before.search(/\S*$/) : 0;
    let word = before.slice(start);
    const neg = word.startsWith('-') && word.length > 0;
    if (neg) word = word.slice(1);
    return { start, neg, word, caret };
  }

  function schedule() {
    clearTimeout(timer);
    timer = window.setTimeout(fetchSuggestions, suggest ? 250 : 70); // (a site: not a request per key)
  }

  async function fetchSuggestions() {
    const { word } = currentWord();
    // (comics: "/words" is a title search - no tags to suggest)
    if (!word || isFilterWord(word) || '()~'.includes(word) || (comics && /^-?\//.test(value.trimStart()))) {
      close();
      return;
    }
    abort?.abort();
    abort = new AbortController();
    try {
      // (all of them, as many as the server gives: the list scrolls; the ones already there are left out)
      const res =
        suggest ? await suggest(word, abort.signal) : typeof comics === 'string' ? await api.comicTags(word, comics, 100, abort.signal) : await api.autocomplete(word, 200, abort.signal, comics ? 'comics' : '', sites);
      const have = present();
      suggestions = res.suggestions.filter((s) => !have(s.tag) && !(s.ideal && have(s.ideal)));
      shownFor = word;
      selected = -1;
      open = suggestions.length > 0 && document.activeElement === inputEl;
    } catch {
      /* aborted or offline: keep quiet */
    }
  }

  /** Whether a tag is there already: in exclude, or one of the box's other words (written as a pick would write it - e.g. a site's #tag). */
  function present(): (tag: string) => boolean {
    const have = new Set(exclude.map((x) => x.toLowerCase()));
    const words = new Set<string>();
    if (multi) {
      const { start, caret } = currentWord();
      for (const w of (value.slice(0, start) + ' ' + value.slice(caret)).split(/\s+/)) {
        const bare = w.replace(/^-/, '').toLowerCase();
        if (bare && !'()~'.includes(bare)) words.add(bare);
      }
    }
    return (tag) => have.has(tag.toLowerCase()) || words.has(writeTag(tag).toLowerCase());
  }
  const writeTag = (full: string) => (format ? format(full) : bare ? splitTag(full).name : full);

  function close() {
    open = false;
    suggestions = [];
    selected = -1;
  }

  /** The box emptied from outside (the search bar's clear button): its list goes, and any answer still on its way. */
  export function reset() {
    clearTimeout(timer);
    abort?.abort();
    marks = [];
    close();
  }

  // ---- marked suggestions: they go in together (all of them, or any of them)
  let marks = $state<string[]>([]);
  let focused = $state(false);
  // The recent searches: shown while the box has the focus (Escape hides them until something is typed or it's left).
  let hush = $state(false);
  const shownRecent = $derived(focused && !hush && recentRow ? recent : []);
  const nR = $derived(shownRecent.length);
  const listShown = $derived(open || (focused && marks.length > 0) || nR > 0);
  // A press outside the zone and the list, while it's open: it closes, and the press (and its click) go no further.
  // Not over a menu, dialog or notice it may have opened itself.
  function pressedOff(e: PointerEvent) {
    const on = e.target as Element | null;
    if (!zone || !listShown || e.button !== 0 || !on || zone.contains(on) || listEl?.contains(on)) return;
    if (on.closest('[role=menu], [role=dialog], [role=alertdialog], .toast, .toasts')) return;
    e.stopPropagation();
    e.preventDefault(); // (no focus moving, no text selection: the box just lets go)
    window.addEventListener('click', swallow, { capture: true, once: true });
    window.setTimeout(() => window.removeEventListener('click', swallow, { capture: true }), 600);
    inputEl?.blur();
  }
  const swallow = (c: Event) => {
    c.stopPropagation();
    c.preventDefault();
  };
  /** A recent search picked: the search bar runs it, the box lets go (the keyboard goes, the results show). */
  function takeRecent(r: RecentSearch) {
    selected = -1;
    onrecent?.(r);
    inputEl?.blur();
  }
  const canMark = $derived(multi && !!anyOf);
  const anyText = $derived(marks.length > 1 && anyOf ? anyOf(marks.map(writeTag)) : null);
  // The strip of marked tags scrolls sideways: one just marked comes into view, at its end (and while it pops in) -
  // the user: it "should always do this when adding tags to search".
  let marksEl = $state<HTMLElement>();
  let markCount = 0;
  $effect(() => {
    const n = marks.length;
    const added = n > markCount;
    markCount = n;
    if (!added || !marksEl) return;
    const el = marksEl;
    const until = performance.now() + 240;
    const follow = () => {
      el.scrollLeft = el.scrollWidth;
      if (performance.now() < until) requestAnimationFrame(follow);
    };
    follow();
  });
  function toggleMark(s: Suggestion) {
    const full = s.ideal ?? s.tag;
    marks = marks.includes(full) ? marks.filter((m) => m !== full) : [...marks, full];
  }
  /** The marked tags in place of the word being typed: each one (all of them), or as one group (any of them). */
  function addMarked(any: boolean) {
    if (!marks.length) return;
    const text = any && anyText ? anyText : marks.map(writeTag).join(' ');
    marks = [];
    put(text, false);
  }
  /** Text in place of the word being typed (then a space, when several words go in the box). */
  function put(text: string, neg: boolean) {
    const { start, caret } = currentWord();
    const after = value.slice(caret);
    const insert = (neg ? '-' : '') + text + (multi ? ' ' : '');
    value = value.slice(0, start) + insert + (multi ? after.trimStart() : '');
    close();
    oninputchange?.(value);
    queueMicrotask(() => {
      const pos = start + insert.length;
      inputEl?.setSelectionRange(pos, pos);
      inputEl?.focus();
    });
  }
  // (the bar's buttons: pressed without taking the focus from the box - the keyboard stays)
  function press(e: Event, fn: () => void) {
    e.preventDefault();
    fn();
  }

  function pick(i: number) {
    choose(suggestions[i]);
  }
  function choose(s: Suggestion | undefined) {
    if (!s) return;
    put(writeTag(s.ideal ?? s.tag), currentWord().neg);
  }

  // Touch: a tap is handled here, not by the mouse events a phone fakes
  // afterwards - those don't come if the finger moved a little, and come after
  // the box lost focus (the keyboard starts closing and the list moves away
  // under the finger). The suggestion is the one touched, even if the list
  // changed meanwhile; moving the finger (scrolling the list) isn't a tap.
  let touch: { act: () => void; x: number; y: number } | null = null;
  function touchStart(e: TouchEvent, s: Suggestion) {
    const mark = !!(e.target as Element).closest('.mark');
    touchFor(e, mark ? () => toggleMark(s) : () => choose(s));
  }
  function touchFor(e: TouchEvent, act: () => void) {
    const t = e.touches[0];
    touch = { act, x: t.clientX, y: t.clientY };
  }
  function touchRecent(e: TouchEvent, r: RecentSearch) {
    touchFor(e, (e.target as Element).closest('.forget') ? () => onrecentremove?.(r) : () => takeRecent(r));
  }
  function touchMove(e: TouchEvent) {
    const t = e.touches[0];
    if (touch && Math.hypot(t.clientX - touch.x, t.clientY - touch.y) > 10) touch = null;
  }
  function touchEnd(e: TouchEvent) {
    if (!touch) return;
    e.preventDefault(); // no blur, no faked mouse events
    const { act } = touch;
    touch = null;
    act();
  }

  // The arrow keys keep the highlighted row in view (the list scrolls).
  $effect(() => {
    if (selected >= 0) listEl?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  });

  function keydown(e: KeyboardEvent) {
    if (listShown && (e.key === 'ArrowDown' || e.key === 'ArrowUp') && nR + suggestions.length > 0) {
      e.preventDefault();
      const n = nR + suggestions.length;
      selected = e.key === 'ArrowUp' && selected < 0 ? n - 1 : (selected + (e.key === 'ArrowDown' ? 1 : -1) + n) % n; // (up from none: the last)
      return;
    }
    // (the arrow keys move through the list; Space marks the highlighted suggestion - none highlighted: a space)
    if (open && selected >= nR && canMark && e.key === ' ') {
      e.preventDefault();
      toggleMark(suggestions[selected - nR]);
      return;
    }
    if (e.key === 'Enter' && marks.length) {
      e.preventDefault(); // (marked tags: Enter adds them in place of the word typed - all of them; Shift+Enter: any of them)
      addMarked(e.shiftKey);
      return;
    }
    if (selected >= 0 && selected < nR && (e.key === 'Enter' || e.key === 'Tab')) {
      e.preventDefault();
      takeRecent(shownRecent[selected]);
      return;
    }
    if (open && selected >= nR && (e.key === 'Enter' || e.key === 'Tab')) {
      e.preventDefault();
      pick(selected - nR);
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      close();
      onenter?.(value);
      return;
    }
    if (e.key === 'Escape') {
      if (open || marks.length || nR) {
        e.stopPropagation();
        close();
        marks = [];
        hush = true;
      } else {
        onescape?.();
      }
      return;
    }
    if (e.key === 'Backspace' && value === '') onbackspaceempty?.();
  }

  $effect(() => {
    if (autofocus) inputEl?.focus();
  });
</script>

<svelte:window onpointerdowncapture={pressedOff} />

<div class="wrap" class:above={inSheet}>
  <input
    bind:this={inputEl}
    bind:value
    {placeholder}
    aria-label={label}
    role="combobox"
    aria-expanded={listShown}
    aria-controls={listId}
    aria-autocomplete="list"
    autocomplete="off"
    autocapitalize="off"
    spellcheck="false"
    enterkeyhint="search"
    oninput={() => {
      hush = false;
      selected = -1;
      schedule();
      oninputchange?.(value);
    }}
    onkeydown={keydown}
    onfocus={() => {
      clearTimeout(blurTimer);
      focused = true;
      hush = false;
      // (back in a box left mid-word: its suggestions again. Only after leaving: a box opened with text in it stays quiet.)
      if (left) schedule();
      left = false;
    }}
    onblur={() => {
      blurTimer = window.setTimeout(() => {
        close();
        left = true;
        focused = false;
        marks = []; // (marks last while the box has the focus)
      }, 120);
      onblur?.();
    }} />
  {#if listShown}
    <!-- The box (placed; at the page's top level when anchored); the list scrolls inside it, under the app's
         floating scrollbar (overlayScroll: its thumb goes in the box). -->
    <div
      class="list"
      bind:this={listEl}
      in:drop
      out:drop={{ out: true }}
      class:anchored={!!place}
      {@attach anchor && toBody}
      data-floating={anchor ? '' : undefined}
      style:left={place ? `${place.left}px` : spread ? `${spread.left}px` : undefined}
      style:right={spread && !place ? 'auto' : undefined}
      style:top={place?.top !== undefined ? `${place.top}px` : undefined}
      style:bottom={place?.bottom !== undefined ? `${place.bottom}px` : undefined}
      style:width={place ? `${place.width}px` : spread ? `${spread.width}px` : undefined}>
    <ul class="scroll" id={listId} role="listbox" {@attach overlayScroll} style:max-height={place?.room !== undefined ? `calc(var(--view-h) - ${place.room + 10}px)` : maxHeight ? `${maxHeight}px` : undefined}>
      {#if nR && recentHead}
        <li class="head" role="presentation">
          <span>{t('searchBar.recent')}</span>
          <button type="button" class="clear-recent" onmousedown={(e) => press(e, () => onrecentclear?.())} ontouchend={(e) => press(e, () => onrecentclear?.())}
            >{t('searchBar.clearRecent')}</button>
        </li>
      {/if}
      {#each shownRecent as r, i (r.query + '|' + r.sort)}
        <li
          class="recent"
          role="option"
          aria-label={t('searchBar.recentOption', { search: r.query })}
          aria-selected={i === selected}
          animate:moved
          out:pop
          onmousedown={(e) => {
            e.preventDefault(); // (the box keeps the focus until the search is run)
            if ((e.target as Element).closest('.forget')) onrecentremove?.(r);
            else takeRecent(r);
          }}
          ontouchstart={(e) => touchRecent(e, r)}
          ontouchmove={touchMove}
          ontouchend={touchEnd}>
          <span class="r-icon" aria-hidden="true"><Icon name="recent" size={16} /></span>
          <span class="r-search">{@render recentRow!(r)}</span>
          <span class="forget" role="button" aria-label={t('searchBar.forgetRecent', { search: r.query })} title={t('searchBar.forgetRecentTitle')}
            ><Icon name="close" size={14} /></span>
        </li>
      {/each}
      {#if nR && suggestions.length}<li class="sep" role="presentation"></li>{/if}
      {#each suggestions as s, i (s.tag)}
        {@const p = splitTag(s.tag)}
        {@const m = marked(p.name)}
        {@const on = marks.includes(s.ideal ?? s.tag)}
        <li
          role="option"
          aria-label="{s.tag}{s.ideal ? ` → ${s.ideal}` : ''}{s.count ? `, ${num(s.count)}` : ''}"
          aria-selected={i + nR === selected}
          class:marked={on}
          onmousedown={(e) => {
            e.preventDefault(); // keeps the focus (and the keyboard) in the box
            if (canMark && ((e.target as Element).closest('.mark') || e.ctrlKey || e.metaKey)) toggleMark(s);
            else pick(i);
          }}
          ontouchstart={(e) => touchStart(e, s)}
          ontouchmove={touchMove}
          ontouchend={touchEnd}>
          <span class="name">
            <!-- Its namespace as a chip, then its name with what was typed marked. -->
            <span class="tag" style:color={tagColor(s.tag)}
              >{#if p.ns}<span class="ns">{nsLabel(p.ns)}</span>{/if}{m[0]}{#if m[1]}<mark>{m[1]}</mark>{/if}{m[2]}</span>
            {#if s.ideal}<span class="ideal">→ {s.ideal}</span>{/if}
          </span>
          <SubMark tag={s.ideal ?? s.tag} size={15} />
          {#if s.count}<span class="count">{num(s.count)}</span>{/if}
          {#if canMark}
            <!-- (marked: it goes in with the others marked - the bar below) -->
            <span class="mark" class:on role="checkbox" aria-checked={on} aria-label={t('tagInput.mark', { tag: s.ideal ?? s.tag })} title={t('tagInput.markTitle')}
              ><span class="box">{#if on}<Icon name="check" size={13} />{/if}</span></span>
          {/if}
        </li>
      {/each}
      {#if marks.length}
        <!-- The marked tags (a tap takes one out), then in they go: all of them, or any of them. -->
        <li class="marks" role="presentation">
          <span class="mark-tags" bind:this={marksEl}>
            {#each marks as m (m)}
              <button type="button" class="mtag" style:color={tagColor(m)} aria-label={t('tagInput.unmark', { tag: m })}
                onmousedown={(e) => press(e, () => (marks = marks.filter((x) => x !== m)))} ontouchend={(e) => press(e, () => (marks = marks.filter((x) => x !== m)))}
                 animate:moved transition:pop>{splitTag(m).name}<Icon name="close" size={11} /></button>
            {/each}
          </span>
          <span class="mark-go">
            <button type="button" class="go" onmousedown={(e) => press(e, () => addMarked(false))} ontouchend={(e) => press(e, () => addMarked(false))}>{t('details.allOfThem')}</button>
            {#if anyText}
              <button type="button" class="go" onmousedown={(e) => press(e, () => addMarked(true))} ontouchend={(e) => press(e, () => addMarked(true))}
                 transition:nudge>{t('details.anyOfThem')}</button>
            {/if}
          </span>
        </li>
      {/if}
    </ul>
    </div>
  {/if}
</div>

<style>
  /* In a sheet: the list above the box, in the flow (the box stays at the bottom, right above the keyboard). */
  .wrap.above {
    display: flex;
    flex-direction: column-reverse;
  }
  .wrap.above .list {
    position: static;
    margin-bottom: 6px;
    box-shadow: none;
  }
  .wrap {
    position: relative;
    flex: 1;
    min-width: 0;
  }
  input {
    width: 100%;
  }
  .list {
    position: absolute;
    z-index: 30;
    left: 0;
    right: 0;
    top: calc(100% + 4px);
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 10px;
    overflow: hidden;
    box-shadow: 0 12px 32px rgb(0 0 0 / 0.45);
  }
  .scroll {
    margin: 0;
    padding: 4px;
    list-style: none;
    max-height: min(50vh, 420px);
    overflow: auto;
    overscroll-behavior: contain;
  }
  .list.anchored {
    position: fixed;
    z-index: 90; /* (at the top of the page: over the dialogs too) */
    top: auto; /* (placed by placeList: under the box, or over it) */
    right: auto;
  }
  li {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 10px;
    border-radius: 6px;
    cursor: pointer;
    min-height: calc(var(--control-h) - 4px);
    align-items: center;
  }
  li[aria-selected='true'],
  li:hover {
    background: var(--surface0);
  }
  /* Recent searches: the clock, the search's chips (one line, faded at the end when long), an x that forgets it. */
  li.recent {
    justify-content: flex-start;
    gap: 8px;
    padding-right: 4px;
  }
  .r-icon {
    flex: none;
    display: grid;
    color: var(--overlay0);
  }
  .r-search {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 4px;
    overflow: hidden;
    mask-image: linear-gradient(to left, transparent, #000 16px);
  }
  .forget {
    flex: none;
    display: grid;
    place-items: center;
    width: 32px;
    align-self: stretch;
    margin: -6px 0;
    border-radius: 6px;
    color: var(--overlay0);
  }
  @media (hover: hover) {
    .forget:hover {
      color: var(--red);
    }
  }
  li.head,
  li.head:hover {
    min-height: 0;
    padding: 2px 2px 2px 10px;
    background: none;
    cursor: default;
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
  }
  .clear-recent {
    min-height: 28px;
    padding: 0 8px;
    border: 0;
    background: none;
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
  }
  @media (hover: hover) {
    .clear-recent:hover {
      color: var(--text);
    }
  }
  li.sep,
  li.sep:hover {
    min-height: 0;
    height: 1px;
    margin: 4px 6px;
    padding: 0;
    background: var(--surface0);
    cursor: default;
  }
  .name {
    overflow-wrap: anywhere;
  }
  .tag {
    overflow-wrap: anywhere;
  }
  .ns {
    display: inline-block;
    margin-right: 6px;
    padding: 0 7px;
    border-radius: 999px;
    background: color-mix(in srgb, currentColor 16%, transparent);
    font-size: 11px;
    font-weight: 600;
    line-height: 18px;
    vertical-align: 1px;
  }
  mark {
    padding: 0 1px;
    border-radius: 3px;
    background: color-mix(in srgb, currentColor 24%, transparent);
    color: inherit;
    font-weight: 700;
  }
  .ideal {
    color: var(--subtext);
    margin-left: 4px;
  }
  .count {
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .name {
    flex: 1;
    min-width: 0;
  }
  /* The mark: a round box at the row's end - a big target, the box itself small. */
  .mark {
    flex: none;
    display: grid;
    place-items: center;
    width: 32px;
    align-self: stretch;
    margin: -6px -10px -6px -4px;
  }
  .mark .box {
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 1.5px solid var(--overlay0);
    color: var(--base);
    transition:
      background-color 0.15s,
      border-color 0.15s;
  }
  .mark.on .box {
    background: var(--mauve);
    border-color: var(--mauve);
  }
  li.marked {
    background: color-mix(in srgb, var(--mauve) 10%, transparent);
  }
  /* The bar of marked tags: kept in sight at the list's bottom. */
  .marks {
    position: sticky;
    bottom: -4px;
    margin: 4px -4px -4px;
    padding: 8px 10px;
    border-radius: 0 0 10px 10px;
    border-top: 1px solid var(--surface1);
    background: var(--mantle);
    cursor: default;
    gap: 8px;
  }
  .marks:first-child {
    /* (no suggestions above: the bar alone) */
    margin-top: -4px;
    border-top: 0;
    border-radius: 10px;
  }
  .marks:hover {
    background: var(--mantle);
  }
  /* Phones: the marked tags on a line of their own, the buttons under them. */
  @media (max-width: 639px) {
    .marks {
      flex-wrap: wrap;
    }
    .marks .mark-tags {
      flex: 1 1 100%;
    }
    .marks .mark-go {
      flex: 1 1 100%;
    }
    .marks .go {
      flex: 1;
      justify-content: center;
      min-height: 36px;
    }
  }
  .mark-tags {
    display: flex;
    gap: 4px;
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .mtag {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    min-height: 26px;
    padding: 0 8px;
    border: 0;
    border-radius: 999px;
    background: color-mix(in srgb, currentColor 16%, transparent);
    font-size: var(--font-small);
    font-weight: 600;
    white-space: nowrap;
  }
  .mark-go {
    flex: none;
    display: flex;
    gap: 4px;
  }
  .go {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 30px;
    padding: 0 10px;
    border: 0;
    border-radius: 999px;
    background: var(--mauve);
    color: var(--base);
    font-size: var(--font-small);
    font-weight: 700;
    white-space: nowrap;
  }
</style>
