<script lang="ts">
  import { reveal, pop, moved } from '../lib/motion';
  // Actions for the selected files: edit their tags (all their tags with how
  // many have each; add, remove, give a tag to all), refresh tags, delete
  // (with undo), select all, clear. Shown while anything is selected.
  import { tick } from 'svelte';
  import { backClose } from '../lib/backClose';
  import { api } from '../lib/api';
  import { useScope } from '../lib/scope.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import { restoreFiles, purgeFiles } from '../lib/trash.svelte';
  import TagInput from './TagInput.svelte';
  import Icon from './Icon.svelte';
  import { setFavorite } from '../lib/favorites';
  import { nsColor, nsLabel, nsRank, splitTag } from '../lib/namespaces.svelte';
  import { t, num } from '../lib/i18n.svelte';
  import { isRead, markComicsRead } from '../lib/comics';
  import { collections } from '../lib/collections.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import type { BrowseResults } from '../lib/browse.svelte';
  import { siteLabel } from '../lib/sites.svelte';

  const { results, selection, kind } = useScope();
  const comics = kind === 'comics'; // (comics: their own tags, and read / unread)
  const browsing = kind === 'browse'; // (a gallery site's search: only Download)
  // Texts that say "files" have a comics version.
  // i18n-keys: comicSelection.* selection.*
  const comicWords = new Set(['saved', 'noSite', 'label', 'editorLabel', 'tagsOf', 'addLabel', 'addAll', 'backToSome', 'onSome', 'removeTitle', 'noTags']);
  const sk = (key: string) => (comics && comicWords.has(key) ? 'comicSelection.' : 'selection.') + key;
  let busy = $state(false);

  // ---- the tag editor: the selection's tags, changed together, saved at once.
  let editing = $state(false);
  let loading = $state(false);
  let files = $state(0);
  let have = $state<{ tag: string; count: number }[]>([]);
  let toAdd = $state<string[]>([]); // new tags, or partial ones given to all
  let toRemove = $state<string[]>([]);
  let text = $state('');
  const changed = $derived(toAdd.length > 0 || toRemove.length > 0);
  const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();

  async function openEditor() {
    editing = true;
    loading = true;
    toAdd = [];
    toRemove = [];
    text = '';
    try {
      const ids = selection.list();
      const r = comics ? await api.comicsTagsOf(ids) : await api.tagsOf(ids);
      files = r.files;
      have = r.tags;
    } catch (e) {
      toasts.error((e as Error).message);
      editing = false;
    } finally {
      loading = false;
    }
  }

  function add(value: string) {
    text = '';
    for (const n of value.split(/\s+/).filter(Boolean)) {
      const back = toRemove.find((t) => same(t, n));
      if (back) toRemove = toRemove.filter((t) => t !== back);
      const had = have.find((t) => same(t.tag, n));
      if (had && had.count === files) continue; // every file has it already
      if (!toAdd.some((t) => same(t, n))) toAdd = [...toAdd, had ? had.tag : n];
    }
  }
  /** × : removed from every selected file (× again keeps it). */
  function toggleRemove(tag: string) {
    if (toAdd.includes(tag) && !have.some((t) => t.tag === tag)) return void (toAdd = toAdd.filter((t) => t !== tag));
    toAdd = toAdd.filter((t) => t !== tag);
    toRemove = toRemove.includes(tag) ? toRemove.filter((t) => t !== tag) : [...toRemove, tag];
  }
  /** + on a tag only some files have: every selected file gets it. */
  function toggleAll(tag: string) {
    toRemove = toRemove.filter((t) => t !== tag);
    toAdd = toAdd.includes(tag) ? toAdd.filter((t) => t !== tag) : [...toAdd, tag];
  }

  // Grouped by namespace like the details (creator, series, character, general, meta).
  const groups = $derived.by(() => {
    const all = [...have, ...toAdd.filter((t) => !have.some((h) => h.tag === t)).map((tag) => ({ tag, count: 0 }))];
    const m = new Map<string, { tag: string; count: number }[]>();
    for (const t of all) {
      const ns = splitTag(t.tag).ns;
      m.set(ns, [...(m.get(ns) ?? []), t]);
    }
    return [...m.entries()]
      .sort(([a], [b]) => nsRank(a).localeCompare(nsRank(b)))
      .map(([ns, list]) => ({ ns, tags: list.sort((a, b) => splitTag(a.tag).name.localeCompare(splitTag(b.tag).name)) }));
  });

  async function save() {
    if (!changed) return void (editing = false);
    const ids = selection.list();
    const add = toAdd.slice();
    const remove = toRemove.slice();
    busy = true;
    try {
      if (comics) await api.comicsTags(ids, add, remove);
      else await api.editTags(ids, add, remove);
      const parts = [add.length ? t('selection.addedTags', { n: add.length }) : '', remove.length ? t('selection.removedTags', { n: remove.length }) : ''].filter(Boolean);
      toasts.show(t(sk('saved'), { n: ids.length, changes: parts.join(', ') }), { cat: 'tags' });
      editing = false;
      ids.forEach((id) => results.forget(id));
      await results.refresh(); // the search may match differently now
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      busy = false;
    }
  }

  // All the selected files already favorites (as far as the loaded details tell): the heart removes them.
  let allFavorites = $derived.by(() => {
    void results.version;
    const ids = [...selection.ids];
    return ids.length > 0 && ids.every((id) => results.file(id)?.favorite);
  });
  async function favoriteSelected() {
    const ids = selection.list();
    if (!ids.length) return;
    busy = true;
    try {
      await setFavorite(ids, !allFavorites, false, results);
    } finally {
      busy = false;
    }
  }

  const inTrash = $derived(results.scope === 'trash');
  async function restoreSelected() {
    const ids = selection.list();
    if (!ids.length) return;
    busy = true;
    if (await restoreFiles(results, ids)) selection.clear();
    busy = false;
  }
  async function remove() {
    const ids = selection.list();
    if (!ids.length) return;
    const n = ids.length;
    const thumbs = ids
      .slice(0, 3)
      .map((id) => results.fileAt(results.indexOf(id))?.thumb)
      .filter((t): t is string => !!t);
    if (inTrash) {
      busy = true;
      if (await purgeFiles(results, ids, thumbs)) selection.clear();
      busy = false;
      return;
    }
    const ok = await confirmAction({
      title: t(comics ? 'comics.deleteTitleN' : 'selection.deleteTitle', { n }),
      body: t(comics ? 'comics.deleteBodyN' : 'selection.deleteBody'),
      action: n === 1 ? t('common.delete') : t('common.deleteN', { n }),
      thumbs,
      undoable: true,
    });
    if (!ok) return;
    busy = true;
    try {
      await results.remove(ids);
      selection.clear();
      toasts.show(t(comics ? 'comics.deletedN' : 'common.deletedFiles', { n: ids.length }), { cat: 'deleted',
        kind: 'removed',
        action: {
          label: t('common.undo'),
          run: async () => {
            await results.restore(ids);
            await results.refresh();
          },
        },
      });
      await results.refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      busy = false;
    }
  }

  // Re-reads the files' posts from their sites when each site is next idle.
  async function refreshTags() {
    const ids = selection.list();
    busy = true;
    try {
      if (comics) {
        // Read from their sites now (one request each).
        const { found } = await api.comicsRefresh(ids);
        toasts.show(found ? t('comics.refreshed', { n: found }) : t(sk('noSite')), { cat: 'refreshed', kind: found ? 'done' : 'info' });
        ids.forEach((id) => results.forget(id));
        await results.refresh();
        return;
      }
      const { posts } = await api.scheduleRefresh(ids);
      toasts.show(
        posts
          ? t('selection.refreshing', { n: posts })
          : t(sk('noSite')),
        { cat: 'refreshed', kind: 'info' },
      );
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      busy = false;
    }
  }

  async function selectAll() {
    const ids = await results.idsInRange(0, results.total - 1);
    // (not collections' tiles; Browse: not the galleries the library has or that wait already)
    selection.add(ids.filter((id) => !results.file(id)?.comic?.stack && !(browsing && results.file(id)?.comic?.mark)));
  }

  // Comics: all the selected ones read (as far as the loaded details tell) - the button marks them unread.
  let allRead = $derived.by(() => {
    void results.version;
    const ids = [...selection.ids];
    return ids.length > 0 && ids.every((id) => isRead(results.file(id)?.comic));
  });
  // The actions' strip (each button with its word, phones too - the user: "the buttons need labels on mobile too"):
  // soft edges where more buttons are scrolled out of view.
  let strip = $state<HTMLElement>();
  const wide = true;
  $effect(() => {
    void selection.count; // (the labels can change length: Read / Unread, Favorite / Unfavorite)
    void allRead;
    void allFavorites;
    void inTrash;
    void results.collection;
    tick().then(edges);
  });
  let fadeL = $state(false);
  let fadeR = $state(false);
  function edges() {
    if (!strip) return;
    fadeL = strip.scrollLeft > 2;
    fadeR = strip.scrollLeft + strip.clientWidth < strip.scrollWidth - 2;
  }
  $effect(() => {
    if (!strip) return;
    const ro = new ResizeObserver(edges);
    ro.observe(strip);
    return () => ro.disconnect();
  });

  // Comics: the selected ones into a collection (one of them, or a new one), or - on a collection's page - out of it.
  // (A collection's own tile among them is left out: it's no comic.)
  const comicIDs = () => selection.list().filter((id) => id < 2 ** 31);
  async function toCollection(k: number | 'new') {
    const ids = comicIDs();
    if (ids.length && (await collections.add(ids, k))) selection.clear();
  }
  async function outOfCollection() {
    const ids = comicIDs();
    if (ids.length && (await collections.remove(ids))) selection.clear();
  }

  // Browse: the selected galleries downloaded, each once (as its own download, named by its title); those the library
  // has or that wait already are left out.
  async function downloadSelected() {
    const br = results as BrowseResults;
    const picked = selection.list().map((id) => br.galleries.get(id)).filter((g) => g !== undefined);
    const todo = picked.filter((g) => !g.comic && !g.queued);
    busy = true;
    let n = 0;
    try {
      for (const g of todo) {
        await api.addDownload(br.site, g.url, 0, false, g.title);
        br.queued(g.id);
        n++;
      }
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      busy = false;
    }
    const had = picked.length - todo.length;
    if (n > 0 || had > 0) {
      toasts.show([n > 0 ? t('browse.downloadingMany', { n: num(n), site: siteLabel(br.site) }) : '', had > 0 ? t('browse.hadSome', { n: num(had) }) : ''].filter(Boolean).join(' '), { cat: 'downloadsStarted', kind: 'info' });
    }
    if (n === todo.length) selection.clear();
  }

  async function markRead() {
    const ids = selection.list();
    if (!ids.length) return;
    const read = !allRead;
    busy = true;
    await markComicsRead(ids, read, results);
    busy = false;
  }
</script>

{#snippet lbl(text: string)}{#if wide}<span class="lbl">{text}</span>{/if}{/snippet}

<div class="bar" role="toolbar" aria-label={browsing ? t('browse.selected') : t(sk('label'))} transition:reveal>
  {#if editing}
    <div class="editor" role="dialog" aria-label={t(sk('editorLabel'))} {@attach backClose(() => (editing = false))} transition:reveal>
      <div class="ed-head">
        <h2>{t(sk('tagsOf'), { n: selection.count })}</h2>
        <button class="icon" aria-label={t('selection.closeEditor')} title={t('selection.closeTitle')} onclick={() => (editing = false)}><Icon name="close" /></button>
      </div>
      <TagInput bind:value={text} {comics} multi autofocus label={t(sk('addLabel'))} placeholder={t('blacklist.addPlaceholder')} onenter={add} onescape={() => (editing = false)} />
      <div class="ed-tags">
        {#if loading}
          <p class="muted small" transition:reveal>{t('common.loading')}</p>
        {:else}
          {#each groups as g (g.ns)}
            <div class="group" style:--c={nsColor(g.ns)} animate:moved transition:reveal>
              <h3>{nsLabel(g.ns)}</h3>
              <ul class="tags">
                {#each g.tags as tg (tg.tag)}
                  {@const partial = tg.count > 0 && tg.count < files}
                  {@const adding = toAdd.includes(tg.tag)}
                  {@const removing = toRemove.includes(tg.tag)}
                  <li class:adding class:removing class:partial={partial && !adding}>
                    <span class="name">{#each splitTag(tg.tag).name.split('_') as part, k}{#if k}_<wbr />{/if}{part}{/each}</span>
                    {#if partial && !removing}
                      <button
                        class="cnt"
                        class:on={adding}
                        aria-label={adding ? t('selection.dontAddAll', { tag: tg.tag }) : t(sk('addAll'), { tag: tg.tag, n: files })}
                        title={adding ? t(sk('backToSome')) : t(sk('onSome'), { count: tg.count, n: files })}
                        onclick={() => toggleAll(tg.tag)} transition:pop>{adding ? t('selection.all') : `${num(tg.count)}/${num(files)}`}<Icon name={adding ? 'check' : 'plus'} size={12} /></button>
                    {/if}
                    {#if removing}
                      <button class="x" aria-label={t('selection.keep', { tag: tg.tag })} title={t('selection.keepIt')} onclick={() => toggleRemove(tg.tag)}><Icon name="undo" size={14} /></button>
                    {:else}
                      <button class="x" aria-label={t('selection.removeFromAll', { tag: tg.tag })} title={t(sk('removeTitle'))} onclick={() => toggleRemove(tg.tag)}><Icon name="close" size={14} /></button>
                    {/if}
                  </li>
                {/each}
              </ul>
            </div>
          {:else}
            <p class="muted small" transition:reveal>{t(sk('noTags'))}</p>
          {/each}
        {/if}
      </div>
      <div class="ed-foot">
        <span class="small muted">
          {#if changed}{[toAdd.length ? t('selection.toAdd', { n: toAdd.length }) : '', toRemove.length ? t('selection.toRemove', { n: toRemove.length }) : ''].filter(Boolean).join(', ')}{:else}{t('selection.editorHint')}{/if}
        </span>
        <button class="quiet" onclick={() => (editing = false)} transition:pop>{t('common.cancel')}</button>
        <button class="go" onclick={save} disabled={busy || !changed}>{t('common.save')}</button>
      </div>
    </div>
  {/if}
    <div class="row">
      <button class="icon" aria-label={t('selection.clear')} onclick={() => selection.clear()}><Icon name="close" /></button>
      <span class="count">{num(selection.count)}<span class="word">{' ' + t('selection.selected')}</span></span>
      <!-- The actions: square buttons in a strip that scrolls sideways when it doesn't fit (soft edges where more is). -->
      <div class="actions" class:fade-l={fadeL} class:fade-r={fadeR} bind:this={strip} onscroll={edges}>
        <button class="sq" class:wide aria-label={t('selection.selectAllLabel')} title={t('selection.selectAllLabel')} onclick={selectAll} disabled={busy}><Icon name="checks" size={18} />{@render lbl(t('selection.short.selectAll'))}</button>
        {#if browsing}
          <button class="go dl" onclick={downloadSelected} disabled={busy || selection.count === 0}><Icon name="download" size={18} /> {t('backups.download')}</button>
        {:else if inTrash}
          <button class="sq" class:wide aria-label={t('trash.restore')} title={t('trash.restore')} onclick={restoreSelected} disabled={busy}><Icon name="undo" size={18} />{@render lbl(t('trash.restore'))}</button>
        {:else}
        <button class="sq" class:wide aria-label={t('tags.title')} title={t('tags.title')} onclick={() => (editing ? (editing = false) : openEditor())} disabled={busy} aria-expanded={editing}><Icon name="tag" size={18} />{@render lbl(t('tags.title'))}</button>
        <button class="sq" class:wide aria-label={t('selection.refreshLabel')} title={t('selection.refreshTitle')} onclick={refreshTags} disabled={busy}><Icon name="retry" size={18} />{@render lbl(t('selection.short.refresh'))}</button>
        {#if comics}
          {#if results.collection}
            <button class="sq" class:wide aria-label={t('collections.remove')} title={t('collections.remove')} onclick={outOfCollection} disabled={busy}><Icon name="stack" size={18} />{@render lbl(t('selection.short.removeCollection'))}</button>
          {:else}
            <ActionMenu
              label={t('collections.addTo')}
              icon="stack"
              quiet
              text={wide ? t('selection.short.collection') : undefined}
              disabled={busy}
              items={[...collections.list.map((k) => ({ label: k.name, icon: 'stack' as const, run: () => toCollection(k.id) })), { label: t('collections.newItem'), icon: 'plus' as const, run: () => toCollection('new') }]} />
          {/if}
          <button class="sq" class:wide aria-label={allRead ? t('comics.markUnread') : t('comics.markRead')} title={allRead ? t('comics.markUnread') : t('comics.markRead')} onclick={markRead} disabled={busy}
            ><Icon name={allRead ? 'book' : 'check'} size={18} />{@render lbl(allRead ? t('selection.short.unread') : t('selection.short.read'))}</button>
        {/if}
        <button class="sq" class:wide class:fav-on={allFavorites} aria-label={allFavorites ? t('selection.unfavorite') : t('selection.favorite')} title={allFavorites ? t('fileMenu.unfavorite') : t('fileMenu.favorite')} onclick={favoriteSelected} disabled={busy}
          ><Icon name="heart" size={18} filled={allFavorites} />{@render lbl(allFavorites ? t('selection.short.unfavorite') : t('selection.short.favorite'))}</button>
        {/if}
        {#if !browsing}<button class="sq danger" class:wide aria-label={inTrash ? t('trash.purge') : t('selection.delete')} title={inTrash ? t('trash.purge') : t('selection.delete')} onclick={remove} disabled={busy} transition:pop><Icon name="trash" size={18} />{@render lbl(inTrash ? t('trash.purge') : t('common.delete'))}</button>{/if}
      </div>
    </div>
</div>

<style>
  .fav-on {
    color: var(--red);
  }
  .dl {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: var(--control-h);
  }
  .bar {
    position: relative;
    padding: 8px calc(10px + var(--safe-corner, 0px)) calc(8px + var(--safe-bottom) + var(--home-bar, 0px)); /* clear of the iPhone home indicator and corners */
    background: var(--mantle);
    border-top: 1px solid var(--surface0);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .count {
    font-weight: 600;
    white-space: nowrap;
    flex: none;
  }
  .row button.icon {
    flex: none;
  }
  /* The actions' strip: right-aligned when it fits, scrolling sideways (snapping to buttons) when it doesn't. */
  .actions {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scroll-snap-type: x proximity;
    scrollbar-width: none;
    padding: 2px 0;
    --fl: 0px;
    --fr: 0px;
    -webkit-mask-image: linear-gradient(to right, transparent, #000 var(--fl), #000 calc(100% - var(--fr)), transparent);
    mask-image: linear-gradient(to right, transparent, #000 var(--fl), #000 calc(100% - var(--fr)), transparent);
  }
  .actions::-webkit-scrollbar {
    display: none;
  }
  .actions.fade-l {
    --fl: 24px;
  }
  .actions.fade-r {
    --fr: 24px;
  }
  /* Right-aligned when they fit; scrolled from the first when they don't (an auto margin is 0 then - a spacer before
     them made the strip settle a few pixels in, the first button under the soft edge). */
  .actions > :first-child {
    margin-left: auto;
  }
  /* Each button says what it does. */
  .actions .sq.wide,
  .actions :global(button.quiet.more.texted) {
    width: auto;
    padding: 0 12px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }
  .lbl {
    font-size: var(--font-small);
    font-weight: 600;
  }
  /* Square buttons, all the same size. */
  .actions .sq,
  .actions :global(button.quiet.more) {
    flex: none;
    width: var(--control-h);
    height: var(--control-h);
    min-width: 0;
    padding: 0;
    display: grid;
    place-items: center;
    scroll-snap-align: start;
    background: transparent;
    border: 1px solid var(--surface1);
    border-radius: var(--radius);
    color: var(--text);
  }
  .actions .sq.danger {
    color: var(--red);
    border-color: color-mix(in srgb, var(--red) 40%, var(--surface1));
  }
  @media (max-width: 430px) {
    .word {
      display: none; /* just the number: room for the buttons */
    }
  }
  /* The tag editor: a panel above the bar (the whole width on phones, at the
     right on wider screens); only its tag list scrolls. */
  .editor {
    position: absolute;
    z-index: 5;
    left: 0;
    right: 0;
    bottom: 100%;
    display: grid;
    gap: 10px;
    max-height: min(70vh, 620px);
    grid-template-rows: auto auto minmax(0, 1fr) auto;
    padding: 12px 12px 10px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    border-bottom: none;
    border-radius: 16px 16px 0 0;
    box-shadow: 0 -12px 32px rgb(0 0 0 / 0.45);
  }
  @media (min-width: 700px) {
    .editor {
      left: auto;
      right: 10px;
      width: 520px;
    }
  }
  .ed-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .ed-head h2 {
    flex: 1;
    margin: 0 0 0 2px;
    font-size: 16px;
  }
  .ed-tags {
    overflow-y: auto;
    overscroll-behavior: contain;
    display: grid;
    gap: 10px;
    align-content: start;
    min-height: 60px;
  }
  .ed-foot {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .ed-foot .small {
    flex: 1;
    min-width: 0;
  }
  .ed-foot .go {
    padding: 0 20px;
  }
  .group {
    display: grid;
    gap: 6px;
  }
  .group h3 {
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
    max-width: 100%;
    min-height: calc(var(--control-h) - 8px);
    border-radius: 16px;
    background: color-mix(in srgb, var(--c) 13%, var(--surface0));
    overflow: hidden;
  }
  .name {
    min-width: 0;
    padding: 4px 4px 4px 12px;
    color: var(--c);
    font-weight: 500;
    overflow-wrap: anywhere;
    line-height: 1.3;
  }
  /* On only some of the files: a dimmer pill with "12/30". */
  .tags li.partial {
    background: color-mix(in srgb, var(--c) 6%, var(--crust));
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--c) 25%, transparent);
  }
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
  .tags button {
    flex: none;
    min-height: 0;
    height: auto;
    align-self: stretch;
    border-radius: 0;
    background: none;
  }
  .cnt {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 0 6px;
    font-size: 12px;
    font-weight: 600;
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .cnt.on {
    color: var(--c);
  }
  .x {
    padding: 0 10px 0 6px;
    color: var(--subtext);
  }
  .editor :global(.wrap .list) {
    max-height: min(40vh, 320px);
  }
</style>
