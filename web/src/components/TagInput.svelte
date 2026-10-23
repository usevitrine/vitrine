<script lang="ts">
  // A text input that autocompletes the word being typed against the
  // library's tags (with counts). Used for searching, editing chips and
  // tagging files. Tags already there (in the box, or exclude: the search's chips) aren't suggested; with anyOf,
  // suggestions can be marked - from several words typed one after another - and go in together: all of them, or
  // any of them (an OR group).
  import { api, type Suggestion } from '../lib/api';
  import { pop, moved } from '../lib/motion';
  import { splitTag, tagColor, nsLabel } from '../lib/namespaces.svelte';
  import { t, num } from '../lib/i18n.svelte';
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
    above: inSheet = false,
    bare = false,
    comics,
    sites = '',
    format,
    suggest,
    exclude = [],
    anyOf,
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
    inputEl?: HTMLInputElement;
    anchor?: HTMLElement; // the suggestions take this element's width and sit under it
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
  } = $props();

  // Under the anchor, as wide as it (fixed: the search box scrolls its chips sideways).
  // Upwards (a sheet's box near the keyboard): its bottom on the box's top, growing up to the top of what can be seen.
  // Placed in the sheet's own terms - the bottom the sheet itself was given (its CSS: above the keyboard, --kb) plus
  // the box's height over the sheet's bottom, a difference within the sheet; its room the visible height (--view-h)
  // less that. (The page's coordinates and innerHeight don't agree with the fixed layer's on an iPhone with the
  // keyboard up: the list hung far above the box - the user, twice: "the suggestions still float".)
  let place = $state<{ left: number; width: number; top?: number; bottom?: number; room?: number } | null>(null);
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
  function placeList() {
    if (inSheet) return fitInSheet();
    const box = (anchor ?? inputEl)?.getBoundingClientRect();
    if (!box) return;
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
        place = { left: box.left, width: box.width, bottom: hb + over, room: over + 8 };
        maxHeight = null;
        return;
      }
      place = { left: box.left, width: box.width, bottom: innerHeight - box.top + 4 };
      maxHeight = Math.min(420, above);
      return;
    }
    if (anchor) place = { left: box.left, top: box.bottom + 4, width: box.width };
    maxHeight = Math.max(120, Math.min(420, below));
  }
  $effect(() => {
    if (!open) return;
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
    if (!word || /^(system:|sort:|order:|limit:)/i.test(word) || '()~'.includes(word) || (comics && /^-?\//.test(value.trimStart()))) {
      close();
      return;
    }
    abort?.abort();
    abort = new AbortController();
    try {
      // (a few more asked for: the ones already there are left out)
      const res =
        suggest ? await suggest(word, abort.signal) : typeof comics === 'string' ? await api.comicTags(word, comics, 18, abort.signal) : await api.autocomplete(word, 18, abort.signal, comics ? 'comics' : '', sites);
      const have = present();
      suggestions = res.suggestions.filter((s) => !have(s.tag) && !(s.ideal && have(s.ideal))).slice(0, 12);
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

  // ---- marked suggestions: they go in together (all of them, or any of them)
  let marks = $state<string[]>([]);
  let focused = $state(false);
  const canMark = $derived(multi && !!anyOf);
  const anyText = $derived(marks.length > 1 && anyOf ? anyOf(marks.map(writeTag)) : null);
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
  let touch: { s: Suggestion; x: number; y: number; mark: boolean } | null = null;
  function touchStart(e: TouchEvent, s: Suggestion) {
    const t = e.touches[0];
    touch = { s, x: t.clientX, y: t.clientY, mark: !!(e.target as Element).closest('.mark') };
  }
  function touchMove(e: TouchEvent) {
    const t = e.touches[0];
    if (touch && Math.hypot(t.clientX - touch.x, t.clientY - touch.y) > 10) touch = null;
  }
  function touchEnd(e: TouchEvent) {
    if (!touch) return;
    e.preventDefault(); // no blur, no faked mouse events
    const { s, mark } = touch;
    touch = null;
    if (mark) toggleMark(s);
    else choose(s);
  }

  function keydown(e: KeyboardEvent) {
    if (open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
      e.preventDefault();
      const n = suggestions.length;
      selected = (selected + (e.key === 'ArrowDown' ? 1 : -1) + n) % n;
      return;
    }
    // (the arrow keys move through the list; Space marks the highlighted suggestion - none highlighted: a space)
    if (open && selected >= 0 && canMark && e.key === ' ') {
      e.preventDefault();
      toggleMark(suggestions[selected]);
      return;
    }
    if (e.key === 'Enter' && marks.length) {
      e.preventDefault(); // (marked tags: Enter adds them in place of the word typed - all of them; Shift+Enter: any of them)
      addMarked(e.shiftKey);
      return;
    }
    if (open && selected >= 0 && (e.key === 'Enter' || e.key === 'Tab')) {
      e.preventDefault();
      pick(selected);
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      close();
      onenter?.(value);
      return;
    }
    if (e.key === 'Escape') {
      if (open || marks.length) {
        e.stopPropagation();
        close();
        marks = [];
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

<div class="wrap" class:above={inSheet}>
  <input
    bind:this={inputEl}
    bind:value
    {placeholder}
    aria-label={label}
    role="combobox"
    aria-expanded={open || (focused && marks.length > 0)}
    aria-controls={listId}
    aria-autocomplete="list"
    autocomplete="off"
    autocapitalize="off"
    spellcheck="false"
    enterkeyhint="search"
    oninput={() => {
      schedule();
      oninputchange?.(value);
    }}
    onkeydown={keydown}
    onfocus={() => {
      clearTimeout(blurTimer);
      focused = true;
    }}
    onblur={() => {
      blurTimer = window.setTimeout(() => {
        close();
        focused = false;
        marks = []; // (marks last while the box has the focus)
      }, 120);
      onblur?.();
    }} />
  {#if open || (focused && marks.length > 0)}
    <!-- The box (placed; at the page's top level when anchored); the list scrolls inside it, under the app's
         floating scrollbar (overlayScroll: its thumb goes in the box). -->
    <div
      class="list"
      bind:this={listEl}
      class:anchored={!!place}
      {@attach anchor && toBody}
      data-floating={anchor ? '' : undefined}
      style:left={place ? `${place.left}px` : undefined}
      style:top={place?.top !== undefined ? `${place.top}px` : undefined}
      style:bottom={place?.bottom !== undefined ? `${place.bottom}px` : undefined}
      style:width={place ? `${place.width}px` : undefined}>
    <ul class="scroll" id={listId} role="listbox" {@attach overlayScroll} style:max-height={place?.room !== undefined ? `calc(var(--view-h) - ${place.room + 10}px)` : maxHeight ? `${maxHeight}px` : undefined}>
      {#each suggestions as s, i (s.tag)}
        {@const p = splitTag(s.tag)}
        {@const m = marked(p.name)}
        {@const on = marks.includes(s.ideal ?? s.tag)}
        <li
          role="option"
          aria-label="{s.tag}{s.ideal ? ` → ${s.ideal}` : ''}{s.count ? `, ${num(s.count)}` : ''}"
          aria-selected={i === selected}
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
          <span class="mark-tags">
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
                 transition:pop>{t('details.anyOfThem')}</button>
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
