<script lang="ts">
  // The Tags section of the details panels (a library file's, a comic's): tags
  // grouped by namespace; a tap opens the tag's menu (search with it, exclude
  // it, download it, blacklist it, move it to another namespace, copy, remove); several can
  // be picked at once and searched, downloaded or blacklisted together (the usual menu); the pencil edits
  // them (nothing is saved until the check), the arrows read them again from
  // the site. The panel loads the tags and says how to change them.
  import { onDestroy, untrack } from 'svelte';
  import { reveal, pop, moved } from '../lib/motion';
  import { api, type FileTag } from '../lib/api';
  import { format, parse } from '../lib/query';
  import { toasts } from '../lib/toast.svelte';
  import { copyText } from '../lib/clipboard';
  import { useScope } from '../lib/scope.svelte';
  import TagInput from './TagInput.svelte';
  import TagMenu from './TagMenu.svelte';
  import ActionMenu, { type MenuItem } from './ActionMenu.svelte';
  import Icon from './Icon.svelte';
  import { moveToNamespace, nsColor, nsLabel, nsRank, splitTag } from '../lib/namespaces.svelte';
  import { newDownload } from '../lib/newDownload.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    item,
    tags,
    onsearch,
    save,
    reload,
    refresh,
  }: {
    item: number; // whose tags (another one: editing and menus end)
    tags: FileTag[];
    onsearch: (q: string) => void;
    /** Adds and removes tags (the user's own; a removed site tag goes from this item). None: the tags can't be
     *  changed here (a site's gallery being browsed) - no editing, no removing, no namespaces. */
    save?: (add: string[], remove: string[]) => Promise<void>;
    /** Loads the tags again (after a change). */
    reload: () => void;
    /** Reads the tags from the site again (no button without it). */
    refresh?: () => Promise<void>;
  } = $props();

  const { results, kind } = useScope();
  const comics = kind === 'comics'; // (suggestions from the comics' tags)

  // ---- editing: the pencil shows × on every tag and the add box; nothing is
  // saved until the pencil (now a check) is pressed.
  let editing = $state(false);
  let adding = $state('');
  let toAdd = $state<string[]>([]);
  let toRemove = $state<string[]>([]);
  const changed = $derived(toAdd.length > 0 || toRemove.length > 0);
  const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

  let shownFor = 0;
  $effect(() => {
    if (item !== shownFor) {
      untrack(() => {
        if (shownFor && editing) saveEdits(); // moved on while editing: the changes are kept
        editing = false;
        picked = null;
        endSelecting();
      });
      shownFor = item;
    }
  });

  function startEditing() {
    toAdd = [];
    toRemove = [];
    adding = '';
    editing = true;
  }

  async function saveEdits() {
    const add = toAdd.slice();
    const remove = toRemove.slice();
    editing = false;
    toAdd = [];
    toRemove = [];
    adding = '';
    if (!add.length && !remove.length) return;
    try {
      await save?.(add, remove);
      const parts = [add.length ? t('details.added', { n: add.length }) : '', remove.length ? t('selection.removedTags', { n: remove.length }) : ''].filter(Boolean);
      toasts.show(t('details.tagsSaved', { changes: parts.join(', ') }), { cat: 'tags',
        action: {
          label: t('common.undo'),
          run: async () => {
            await save?.(remove, add);
            reload();
          },
        },
      });
    } catch (e) {
      toasts.error((e as Error).message);
    }
    reload();
  }
  onDestroy(() => {
    if (editing && changed) saveEdits(); // the details closed while editing
  });

  function add(text: string) {
    const names = text.split(/\s+/).filter(Boolean);
    if (!names.length) return;
    adding = '';
    for (const n of names) {
      const back = toRemove.find((x) => same(x, n));
      if (back) toRemove = toRemove.filter((x) => x !== back); // removed then added again: just keep it
      else if (!tags.some((x) => same(x.tag, n)) && !toAdd.some((x) => same(x, n))) toAdd = [...toAdd, n];
    }
  }

  /** × in edit mode: a new tag goes away; an existing one is marked removed (× again puts it back). */
  function toggleRemove(tag: string) {
    if (toAdd.includes(tag)) toAdd = toAdd.filter((x) => x !== tag);
    else toRemove = toRemove.includes(tag) ? toRemove.filter((x) => x !== tag) : [...toRemove, tag];
  }

  async function remove(tag: string) {
    try {
      await save?.([], [tag]);
      toasts.show(t('details.removedTag', { tag }), { cat: 'tags',
        kind: 'removed',
        action: {
          label: t('common.undo'),
          run: async () => {
            await save?.([tag], []);
            reload();
          },
        },
      });
    } catch (e) {
      toasts.error((e as Error).message);
    }
    reload();
  }

  // Grouped by namespace (creator, series, character, ..., general, meta), A-Z within.
  let groups = $derived.by(() => {
    const m = new Map<string, FileTag[]>();
    for (const x of [...tags, ...toAdd.map((tag) => ({ tag, origins: ['new'] }))]) {
      const ns = splitTag(x.tag).ns;
      m.set(ns, [...(m.get(ns) ?? []), x]);
    }
    return [...m.entries()]
      .sort(([a], [b]) => nsRank(a).localeCompare(nsRank(b)))
      .map(([ns, list]) => ({ ns, tags: list.sort((a, b) => splitTag(a.tag).name.localeCompare(splitTag(b.tag).name)) }));
  });

  // ---- a tapped tag: its menu.
  let picked = $state<{ tag: string; rect: DOMRect; el: HTMLElement } | null>(null);

  /** Whether the current search has the tag ('in'), excludes it ('out') or neither. */
  function inSearch(tag: string): 'in' | 'out' | null {
    for (const c of parse(results.query)) {
      if (c.atoms.length === 1 && same(c.atoms[0].text, tag)) return c.atoms[0].neg ? 'out' : 'in';
    }
    return null;
  }

  function searchWith(tag: string, how: 'add' | 'exclude' | 'drop' | 'only') {
    if (how === 'only') return onsearch(tag);
    // The tag's own chip (included or excluded) is replaced; OR groups stay as they are.
    const chips = parse(results.query).filter((c) => !(c.atoms.length === 1 && same(c.atoms[0].text, tag)));
    if (how !== 'drop') chips.push({ atoms: [{ neg: how === 'exclude', text: tag }] });
    onsearch(format(chips));
  }

  // ---- Several tags at once: picked (a mode in the header), then searched for
  // all together, any of them (an OR group), added to the search, or downloaded
  // (the form writes each the site's way).
  let selecting = $state(false);
  let chosen = $state<string[]>([]);
  function toggleChosen(tag: string) {
    chosen = chosen.includes(tag) ? chosen.filter((x) => x !== tag) : [...chosen, tag];
  }
  function endSelecting() {
    selecting = false;
    chosen = [];
  }
  const siteOf = (tag: string) => tags.find((x) => x.tag === tag)?.origins.find((o) => o !== 'user' && o !== 'new');

  // ---- The download blacklist (Tags > Tag rules): a tag or the picked ones added in a tap, or one taken off; Undo.
  let blacklist = $state<string[] | null>(null); // read the first time a menu shows it
  function loadBlacklist() {
    if (blacklist === null) api.blacklist().then((r) => (blacklist = r.tags)).catch(() => {});
  }
  async function setBlacklist(next: string[], said: string) {
    const before = blacklist ?? [];
    try {
      blacklist = (await api.setBlacklist(next)).tags;
      toasts.show(said, {
        cat: 'tags',
        action: { label: t('common.undo'), run: async () => (blacklist = (await api.setBlacklist(before)).tags) },
      });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  const blacklisted = (tag: string) => !!blacklist?.includes(tag);
  function blacklistAdd(list: string[]) {
    const add = list.filter((x) => !blacklisted(x));
    if (!add.length) return;
    setBlacklist([...(blacklist ?? []), ...add], add.length === 1 ? t('details.blacklistedOne', { tag: add[0] }) : t('details.blacklistedN', { n: add.length }));
  }
  function blacklistRemove(tag: string) {
    setBlacklist((blacklist ?? []).filter((x) => x !== tag), t('details.unblacklisted', { tag }));
  }
  $effect(() => {
    if (selecting || picked) loadBlacklist();
  });

  /** What the picked tags can be used for: the same menu as a row's ⋯ (a sheet on phones). */
  const pickItems = $derived<MenuItem[]>([
    { label: t('details.searchAll'), icon: 'search', run: () => useChosen('all') },
    { label: t('details.searchAny'), icon: 'or', disabled: chosen.length < 2, run: () => useChosen('any') },
    { label: t('details.addThem'), icon: 'plus', run: () => useChosen('add') },
    { label: t('details.downloadAll'), icon: 'download', run: () => useChosen('download') },
    { label: t('details.downloadAny'), icon: 'download', disabled: chosen.length < 2, run: () => useChosen('download-any') },
    {
      label: t('details.blacklistThem'),
      icon: 'eyeoff',
      disabled: chosen.every(blacklisted),
      run: () => {
        const list = [...chosen];
        endSelecting();
        blacklistAdd(list);
      },
    },
  ]);

  function useChosen(how: 'all' | 'any' | 'add' | 'download' | 'download-any') {
    const list = [...chosen];
    if (!list.length) return;
    endSelecting();
    if (how === 'download' || how === 'download-any') {
      // On the site they came from (asked when that's more than one).
      newDownload(list, { any: how === 'download-any', fallback: list.map(siteOf).find(Boolean) });
      return;
    }
    const pickedChips = how === 'any' ? [{ atoms: list.map((text) => ({ neg: false, text })) }] : list.map((text) => ({ atoms: [{ neg: false, text }] }));
    if (how === 'add') {
      // Added to the current search (a tag already in it as a chip of its own is replaced).
      const keep = parse(results.query).filter((c) => !(c.atoms.length === 1 && list.some((x) => same(c.atoms[0].text, x))));
      onsearch(format([...keep, ...pickedChips]));
    } else onsearch(format(pickedChips));
  }

  let refreshing = $state(false);
  async function doRefresh() {
    if (!refresh) return;
    refreshing = true;
    try {
      await refresh();
    } finally {
      refreshing = false;
    }
  }
</script>

<section aria-label={t('tags.title')}>
  <div class="head">
    <h3>{t('tags.title')}{#if tags.length}<span class="n" transition:pop>{tags.length}</span>{/if}</h3>
    {#if refresh}
      <button
        class="icon head-btn"
        class:spinning={refreshing}
        disabled={refreshing}
        onclick={doRefresh}
        aria-label={t('details.refreshLabel')}
        title={t('details.refreshTitle')} transition:pop><Icon name="retry" size={17} /></button>
    {/if}
    {#if editing}
      <button class="icon head-btn save" onclick={() => saveEdits()} aria-label={t('details.saveTags')} title={changed ? t('details.saveChanges') : t('saved.done')}><Icon name="check" size={18} /></button>
    {:else}
      {#if tags.length > 1}
        <button
          class="icon head-btn"
          class:on={selecting}
          aria-pressed={selecting}
          onclick={() => (selecting ? endSelecting() : ((selecting = true), (picked = null)))}
          aria-label={t('details.pick')}
          title={selecting ? t('details.donePicking') : t('details.pickTitle')} transition:pop><Icon name="checks" size={18} /></button>
      {/if}
      {#if !selecting && save}
        <button class="icon head-btn" onclick={startEditing} aria-label={t('details.editTags')} title={t('details.editTitle')} transition:pop><Icon name="pencil" size={17} /></button>
      {/if}
    {/if}
  </div>
  {#if editing}
    <TagInput bind:value={adding} {comics} multi label={t('details.addTags')} placeholder={t('blacklist.addPlaceholder')} onenter={add} autofocus />
    {#if changed}
      <p class="pending small" transition:reveal>{t('details.pending', { changes: [toAdd.length ? t('selection.toAdd', { n: toAdd.length }) : '', toRemove.length ? t('selection.toRemove', { n: toRemove.length }) : ''].filter(Boolean).join(', ') })}</p>
    {/if}
  {/if}
  {#each groups as g (g.ns)}
    <div class="group" style:--c={nsColor(g.ns)} animate:moved transition:reveal>
      <h4>{nsLabel(g.ns)} <span class="n">{g.tags.length}</span></h4>
      <ul class="tags">
        {#each g.tags as tg (tg.tag)}
          <li
            class:user={tg.origins.includes('user')}
            class:picked={picked?.tag === tg.tag}
            class:adding={tg.origins.includes('new')}
            class:removing={toRemove.includes(tg.tag)}
            class:chosen={selecting && chosen.includes(tg.tag)}
            title={tg.origins.includes('new') ? t('details.notSaved', { tag: tg.tag }) : t('details.tagFrom', { tag: tg.tag, from: tg.origins.join(', ') })} animate:moved transition:pop>
            <button
              class="name"
              aria-label={splitTag(tg.tag).name}
              aria-haspopup={selecting ? undefined : 'menu'}
              aria-expanded={selecting ? undefined : picked?.tag === tg.tag}
              aria-pressed={selecting ? chosen.includes(tg.tag) : undefined}
              onclick={(e) => {
                if (selecting) return toggleChosen(tg.tag);
                // Desktop shortcuts: Ctrl/Cmd-click adds to the search, Alt-click excludes.
                if (e.ctrlKey || e.metaKey) searchWith(tg.tag, 'add');
                else if (e.altKey) searchWith(tg.tag, 'exclude');
                else {
                  const li = (e.currentTarget as HTMLElement).closest('li')!;
                  picked = picked?.tag === tg.tag ? null : { tag: tg.tag, rect: li.getBoundingClientRect(), el: e.currentTarget as HTMLElement };
                }
              }}
              >{#each splitTag(tg.tag).name.split('_') as part, k}{#if k}_<wbr />{/if}{part}{/each}</button
            >
            {#if editing}
              {#if toRemove.includes(tg.tag)}
                <button class="x" aria-label={t('selection.keep', { tag: tg.tag })} title={t('selection.keepIt')} onclick={() => toggleRemove(tg.tag)} transition:pop><Icon name="undo" size={14} /></button>
              {:else}
                <button class="x" aria-label={t('details.removeTag', { tag: tg.tag })} title={t('sites.remove')} onclick={() => toggleRemove(tg.tag)}><Icon name="close" size={14} /></button>
              {/if}
            {/if}
          </li>
        {/each}
      </ul>
    </div>
  {:else}
    <p class="none muted" transition:reveal>{t('details.noTags')}</p>
  {/each}
  {#if selecting}
    <div class="pick-bar" role="toolbar" aria-label={t('details.picked')} transition:reveal>
      <span class="pick-n">{chosen.length ? t('details.nPicked', { n: chosen.length }) : t('details.tapToPick')}</span>
      <ActionMenu label={t('details.useThem')} title={t('details.nPicked', { n: chosen.length })} icon="more" quiet text={t('details.use')} items={pickItems} disabled={!chosen.length} />
      <button class="icon pick-x" aria-label={t('details.stopPicking')} title={t('saved.done')} onclick={endSelecting}><Icon name="close" size={18} /></button>
    </div>
  {/if}
</section>
{#if picked}
  {@const p = picked}
  <TagMenu
    tag={p.tag}
    anchor={p.rect}
    inSearch={inSearch(p.tag)}
    onsearch={(how) => searchWith(p.tag, how)}
    ondownload={() => newDownload([p.tag], { fallback: siteOf(p.tag) })}
    onnamespace={save
      ? async (ns) => {
          if (await moveToNamespace(p.tag, ns)) reload();
        }
      : undefined}
    oncopy={copyText}
    blacklisted={blacklist === null ? undefined : blacklisted(p.tag)}
    onblacklist={() => (blacklisted(p.tag) ? blacklistRemove(p.tag) : blacklistAdd([p.tag]))}
    onremove={save ? () => remove(p.tag) : undefined}
    removeText={comics ? t('tagMenu.removeFromComic') : undefined}
    onclose={(refocus) => {
      const el = p.el; // read before closing: p follows picked
      picked = null;
      if (refocus) el.focus();
    }} />
{/if}

<style>
  section {
    display: grid;
    gap: 10px;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
  }
  h3 {
    flex: 1;
    margin: 0;
    font-size: var(--font-small);
    font-weight: 600;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--subtext);
  }
  h3 .n,
  h4 .n {
    margin-left: 6px;
    color: var(--overlay0);
    font-weight: 500;
  }
  .head-btn {
    flex: none;
    width: 32px;
    min-width: 0;
    height: 32px;
    min-height: 0;
    margin: -4px 0;
    padding: 0;
    color: var(--subtext);
  }
  .head-btn.save {
    background: var(--mauve);
    color: var(--crust);
    border-radius: 50%;
  }
  .head-btn.spinning :global(svg) {
    animation: spin 0.9s linear infinite;
  }
  .head-btn.on {
    color: var(--mauve);
    background: color-mix(in srgb, var(--mauve) 14%, transparent);
  }
  .pending {
    margin: 0;
    color: var(--subtext);
  }
  /* Edit mode: a tag to be added (dashed), one to be removed (struck through). */
  .tags li.adding {
    outline: 1px dashed color-mix(in srgb, var(--c) 70%, transparent);
    outline-offset: -1px;
  }
  .tags li.removing {
    opacity: 0.55;
  }
  .tags li.removing .name {
    text-decoration: line-through;
  }
  /* Picking: one row like the selection bar - how many, Use (the usual menu: search, download, blacklist), ×. */
  .pick-bar {
    position: sticky;
    bottom: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 12px;
    padding: 6px 6px 6px 14px;
    border-radius: 12px;
    background: var(--crust);
    border: 1px solid var(--surface0);
    box-shadow: 0 -6px 18px rgb(0 0 0 / 0.35);
  }
  .pick-n {
    flex: 1;
    min-width: 0;
    font-weight: 700;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pick-x {
    flex: none;
  }
  /* Without the × (not editing), the name keeps even padding. */
  .tags li:not(:has(.x)) .name {
    padding-right: 12px;
  }
  @keyframes spin {
    to {
      transform: rotate(-360deg);
    }
  }

  /* Tags, by namespace. */
  .group {
    display: grid;
    gap: 6px;
    margin-top: 4px;
  }
  h4 {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--c);
  }
  .tags {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .tags li {
    display: inline-flex;
    align-items: center;
    max-width: 100%; /* a very long tag wraps inside its pill instead of running off */
    background: color-mix(in srgb, var(--c) 13%, var(--surface0));
    border-radius: 16px; /* a pill on one line, rounded corners when it wraps */
    overflow: hidden;
  }
  .tags li.user {
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--c) 55%, transparent);
  }
  .tags li.picked {
    outline: 2px solid var(--c);
    outline-offset: 1px;
  }
  /* Picked (several tags at once): filled with its namespace's color. */
  .tags li.chosen {
    background: var(--c);
    box-shadow: none;
  }
  .tags li.chosen .name {
    color: var(--crust);
    font-weight: 600;
  }
  .tags button {
    background: none;
    color: var(--c);
    min-height: calc(var(--control-h) - 8px);
    font-weight: 500;
    border-radius: 0;
  }
  .name {
    min-width: 0;
    padding: 4px 4px 4px 12px;
    text-align: left;
    white-space: normal;
    overflow-wrap: anywhere;
    line-height: 1.3;
  }
  .x {
    flex: none;
    align-self: stretch;
    padding: 0 10px 0 6px;
    color: var(--subtext) !important;
  }
  .none {
    margin: 0;
    padding: 4px 0;
  }
</style>
