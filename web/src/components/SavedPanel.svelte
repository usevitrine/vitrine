<script lang="ts">
  import { overlayScroll } from '../lib/overlayScroll';
  import { reveal, moved, nudge, pop, glide } from '../lib/motion';
  // Saved searches: save the current search (and its order), run one with a tap; changed after running one, update
  // it in one tap (or save it as a new one); Edit renames or deletes them. Each is a tile with its first files; press
  // and hold one (a mouse: click and hold) to move it. A second tab has the group (parent) tags, as tiles too, in an
  // order of their own (shared setting groupOrder). Opened from the floppy disk in the search bar; on a phone it's a
  // sheet along the bottom, on a wide screen a panel floating at the right (like the viewer's details). The list lives
  // on the server. (Redesigned 2026-10-31 from the user's prototypes, "B · Tiles".)
  import { backClose } from '../lib/backClose';
  import { swallowPress, leaving } from '../lib/outside';
  import { onMount, tick } from 'svelte';
  import { useScope } from '../lib/scope.svelte';
  import { settings } from '../lib/settings.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { label, parse } from '../lib/query';
  import { tagColor } from '../lib/namespaces.svelte';
  import { sortOptions } from './SortMenu.svelte';
  import { api, type SavedSearch, type SavedLook } from '../lib/api';
  import Icon from './Icon.svelte';
  import { HoldDrag } from '../lib/listDrag.svelte';
  import { sheetDrag } from '../lib/sheetDrag';
  import { sheetIn, sheetOut, shade } from '../lib/sheet';
  import { Pages } from '../lib/pages.svelte';
  import { marquee } from '../lib/marquee';
  import { router } from '../lib/router.svelte';
  import Rich from './Rich.svelte';
  import { t, num, short } from '../lib/i18n.svelte';

  let {
    query,
    sort,
    onrun,
    onclose,
    defaultSort = settings.defaultSort,
  }: {
    query: string;
    sort: string;
    /** The page's default order (a saved search in it keeps no order). */
    defaultSort?: string;
    /** key: the menu entry ("saved:<id>", "tag:<tag>") - its NEW files are those since it was last opened. */
    onrun: (query: string, sort: string, key?: string) => void;
    onclose: (refocus?: boolean) => void;
  } = $props();

  const { saved, kind } = useScope();
  const comics = kind === 'comics';
  let editing = $state(false);
  let saving = $state(false); // the name box for the current search is open
  let name = $state('');
  let busy = $state(false);
  let panel = $state<HTMLElement>();
  let tab = $state<'saved' | 'groups'>('saved');

  let current = $derived(saved.find(query, sort, defaultSort));
  // (the search is one of them: a change from here offers to update it)
  $effect(() => {
    if (current) saved.lastRun = current.id;
  });
  /** The saved search this one was changed from (the last run), offered to update. */
  let changedFrom = $derived(!current && query.trim() ? saved.list.find((s) => s.id === saved.lastRun) : undefined);
  /** A name for the current search when none is typed: its chips, short. */
  let autoName = $derived(
    parse(query)
      .map((c) => c.atoms.map((a) => (a.neg ? '-' : '') + label(a, comics)).join(t('common.orSep')))
      .join(' '),
  );

  // ---- each tile's pictures (the library's list only) and the groups' facts
  let looks = $state<Record<string, SavedLook>>({});
  let groupLooks = $state<SavedLook[]>([]);
  async function loadLooks() {
    if (comics) return;
    try {
      const l = await api.savedLooks(defaultSort);
      looks = l.saved;
      groupLooks = l.groups;
    } catch {
      /* offline: tiles without pictures */
    }
  }

  /** The groups in the user's order (groupOrder), the rest after them by name, each with its NEW count and its facts. */
  let groups = $derived.by(() => {
    const order = settings.groupOrder;
    const at = (tag: string) => {
      const i = order.indexOf(tag);
      return i < 0 ? Infinity : i;
    };
    const look = new Map(groupLooks.map((g) => [g.tag, g]));
    return [...saved.groups]
      .sort((a, b) => at(a.tag) - at(b.tag) || a.tag.localeCompare(b.tag))
      .map((g) => ({ ...g, look: look.get(g.tag) }));
  });
  /** The tabs show when there are groups (the Comics page has none). */
  let tabs = $derived(!comics && groups.length > 0);

  onMount(async () => {
    saved.refreshNews();
    loadLooks();
    await saved.load(); // fresh counts every time it opens
    await tick();
    (panel?.querySelector<HTMLElement>('[data-autofocus]') ?? panel)?.focus();
  });

  const sortLabel = (s: string) =>
    s && s !== defaultSort ? (sortOptions(comics).find((o) => o.value === s)?.label ?? s) : '';

  async function openSave() {
    saving = true;
    await tick();
    panel?.querySelector<HTMLInputElement>('.save input')?.focus();
  }

  async function saveCurrent(e?: SubmitEvent) {
    e?.preventDefault();
    if (!query.trim() || busy) return;
    busy = true;
    try {
      const n = name.trim() || autoName;
      saved.lastRun = await saved.add(n, query, sort === defaultSort ? '' : sort);
      name = '';
      saving = false;
      if (tab !== 'saved') switchTab('saved');
      toasts.show(t('saved.saved', { name: n }), { cat: 'savedSearches' });
      loadLooks();
    } catch (err) {
      toasts.error((err as Error).message);
    } finally {
      busy = false;
    }
  }

  function run(s: SavedSearch) {
    saved.lastRun = s.id;
    onrun(s.query, s.sort || defaultSort, 'saved:' + s.id);
    onclose(false);
  }
  function runGroup(tag: string) {
    onrun(tag, defaultSort, 'tag:' + tag);
    onclose(false);
  }

  async function rename(s: SavedSearch, value: string) {
    const v = value.trim();
    if (!v || v === s.name) return;
    try {
      await saved.update(s.id, { name: v });
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }

  async function useCurrent(s: SavedSearch) {
    try {
      await saved.update(s.id, { query, sort: sort === defaultSort ? '' : sort });
      toasts.show(t('saved.updated', { name: s.name }), { cat: 'savedSearches' });
      loadLooks();
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }

  async function remove(s: SavedSearch) {
    const order = saved.list.map((x) => x.id);
    try {
      await saved.remove(s.id);
      toasts.show(t('saved.deleted', { name: s.name }), {
        cat: 'savedSearches',
        kind: 'removed',
        action: {
          label: t('common.undo'),
          run: async () => {
            const id = await saved.add(s.name, s.query, s.sort);
            await saved.reorder(order.map((x) => (x === s.id ? id : x)));
            loadLooks();
          },
        },
      });
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }

  async function move(from: number, to: number) {
    const ids = saved.list.map((s) => s.id);
    const [id] = ids.splice(from, 1);
    ids.splice(to, 0, id);
    try {
      await saved.reorder(ids);
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }
  function moveGroup(from: number, to: number) {
    const tags = groups.map((g) => g.tag);
    const [tag] = tags.splice(from, 1);
    tags.splice(to, 0, tag);
    settings.groupOrder = tags; // (shared: every device has the same order)
  }

  // ---- press and hold a tile to move it (lib/listDrag)
  const drags = new HoldDrag(move);
  const groupDrags = new HoldDrag(moveGroup);
  const dragOf = (d: HoldDrag, i: number) => {
    const o = d.offset(i);
    return o.x || o.y ? `translate(${o.x}px, ${o.y}px)` : undefined;
  };

  // ---- the tabs: Saved and Groups, moving like a menu's pages (lib/pages) - Groups is the deeper one
  const pages = new Pages((h) => {
    const cs = panel ? getComputedStyle(panel) : null;
    const room = cs && sheet ? parseFloat(cs.maxHeight) - 120 : Infinity;
    return Number.isFinite(room) && room > 0 ? Math.min(h, room) : h;
  });
  function switchTab(to: 'saved' | 'groups') {
    if (to === tab) return;
    editing = false;
    pages.go(to === 'groups', () => (tab = to));
  }

  // Arrow keys move between the tiles; with Edit on, the arrow keys on a tile's handle move the tile.
  function onKey(e: KeyboardEvent) {
    if (editing || !['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return;
    const items = [...(panel?.querySelectorAll<HTMLElement>('.run') ?? [])];
    const i = items.indexOf(document.activeElement as HTMLElement);
    if (i < 0) return;
    const cols = Math.max(1, Math.round((panel?.querySelector<HTMLElement>('.tiles')?.clientWidth ?? 1) / (items[0].offsetWidth || 1)));
    const by = e.key === 'ArrowLeft' ? -1 : e.key === 'ArrowRight' ? 1 : e.key === 'ArrowUp' ? -cols : cols;
    const next = items[Math.min(items.length - 1, Math.max(0, i + by))];
    if (next) {
      e.preventDefault();
      next.focus();
    }
  }
  function keyMove(e: KeyboardEvent, i: number) {
    const by = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : 0;
    if (!by || i + by < 0 || i + by >= saved.list.length) return;
    e.preventDefault();
    move(i, i + by);
    const id = saved.list[i]?.id;
    tick().then(() => panel?.querySelector<HTMLElement>(`[data-grip="${id}"]`)?.focus());
  }

  // Esc closes it wherever the focus is (also when nothing in it has focus).
  $effect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      e.stopPropagation();
      onclose(true);
    };
    window.addEventListener('keydown', key, true);
    return () => window.removeEventListener('keydown', key, true);
  });

  // A press outside the panel and the search bar closes it.
  $effect(() => {
    const down = (e: PointerEvent) => {
      if (leaving(panel)) return; // closed already, fading out
      const t = e.target as HTMLElement;
      if (!panel?.contains(t) && !t.closest('.bar, .toasts')) {
        swallowPress(e); // Undo in a toast keeps it open
        onclose(false);
      }
    };
    document.addEventListener('pointerdown', down, true);
    return () => document.removeEventListener('pointerdown', down, true);
  });

  // Phones: a sheet along the bottom (like the other menus there). Wider: a panel floating at the right, under the
  // search bar - it glides in from the right as it fades in.
  const sheet = matchMedia('(max-width: 639px)').matches;
  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    if (!sheet) {
      const bar = document.querySelector<HTMLElement>('.bar');
      node.style.setProperty('--side-top', `${Math.round((bar?.getBoundingClientRect().bottom ?? 56) + 8)}px`);
    }
    return { destroy: () => node.remove() };
  }
  const comeIn = (node: Element) => (sheet ? sheetIn(node) : glide(node, { x: 28, duration: 240 }));
  const goOut = (node: Element) => (sheet ? sheetOut(node) : glide(node, { x: 28, duration: 200 }));
</script>

<div use:portal>
  {#if sheet}<div class="backdrop" role="presentation" in:shade|global out:shade|global={{ out: true }}></div>{/if}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions (Esc and arrow keys for the whole panel) -->
  <div
    class="panel"
    class:sheet
    class:side={!sheet}
    data-sheet={sheet ? '' : undefined}
    bind:this={panel}
    {@attach overlayScroll}
    {@attach sheet && sheetDrag(() => onclose(false))}
    {@attach backClose(() => onclose(false))}
    in:comeIn|global
    out:goOut|global
    role="dialog"
    aria-label={t('saved.title')}
    tabindex="-1"
    onkeydown={onKey}>
    {#if sheet}<div class="grab-zone" aria-hidden="true"><span></span></div>{/if}
    <div class="head">
      <h2><Icon name="save" size={18} /> {t('saved.title')}</h2>
      <button class="icon" aria-label={t('saved.close')} title={t('saved.close')} onclick={() => onclose(true)}><Icon name="close" size={20} /></button>
    </div>

    <div class="bar-row">
      {#if tabs}
        <div class="segmented" role="tablist" aria-label={t('saved.title')} transition:nudge>
          <button type="button" role="tab" aria-selected={tab === 'saved'} onclick={() => switchTab('saved')}>{t('saved.tabSaved')}</button>
          <button type="button" role="tab" aria-selected={tab === 'groups'} onclick={() => switchTab('groups')}>{t('saved.tabGroups')}</button>
        </div>
      {/if}
      <span class="grow"></span>
      {#if tab === 'saved'}
        {#if saved.list.length}
          <button class="quiet small-btn" aria-pressed={editing} onclick={() => (editing = !editing)} transition:nudge>{editing ? t('saved.done') : t('saved.edit')}</button>
        {/if}
        {#if !editing}
          <button
            class="small-btn add"
            disabled={!query.trim() || !!current}
            aria-expanded={saving}
            title={current ? t('searchBar.savedAsTitle', { name: current.name }) : undefined}
            onclick={() => (saving ? (saving = false) : openSave())}
            data-autofocus={!current && query.trim() && !changedFrom ? '' : undefined}
            transition:nudge><Icon name="plus" size={16} /> {t('common.save')}</button>
        {/if}
      {:else}
        <button class="small-btn add" onclick={() => (router.go('tags', 'rules'), onclose(false))} title={t('saved.groupHint')} transition:nudge
          ><Icon name="plus" size={16} /> {t('saved.newGroup')}</button>
      {/if}
    </div>

    {#if tab === 'saved' && !editing && query.trim()}
      {#if current}
        <p class="note" transition:reveal><Icon name="save" size={16} /> <span><Rich text={t('saved.isSaved', { name: current.name })} /></span></p>
      {:else if changedFrom}
        {@const s = changedFrom}
        <!-- Run, then changed: update it in one tap (or save it as a new one). -->
        <div class="note changed" transition:reveal>
          <span class="changed-text"><Rich text={t('saved.changedFrom', { name: s.name })} /></span>
          <button class="small-btn" onclick={() => useCurrent(s)} data-autofocus>{t('saved.update')}</button>
          {#if !saving}<button class="quiet small-btn" onclick={openSave} transition:nudge>{t('saved.saveNew')}</button>{/if}
        </div>
      {/if}
      {#if saving && !current}
        <form class="save" onsubmit={saveCurrent} transition:reveal>
          <input bind:value={name} placeholder={autoName} aria-label={t('saved.nameLabel')} maxlength="80" autocomplete="off" enterkeyhint="done" />
          <button type="submit" disabled={busy}><Icon name="save" size={16} /> {t('common.save')}</button>
        </form>
      {/if}
    {/if}

    <div class="mpages" class:animating={pages.animating} {@attach pages.box} style:height={pages.height !== null ? `${pages.height}px` : undefined}>
      {#key pages.nav}
        <div class="mpage" {@attach pages.page} in:pages.pageIn out:pages.pageOut>
          {#if tab === 'saved'}
            {#if saved.list.length}
              <ul class="tiles" class:dragging={!!drags.drag}>
                {#each saved.list as s, i (s.id)}
                  {@const sl = sortLabel(s.sort)}
                  {@const pics = looks[s.id]?.pics ?? []}
                  {@const lifted = drags.drag?.from === i}
                  <li
                    class="tile"
                    class:active={current?.id === s.id}
                    class:lifted
                    style:transform={dragOf(drags, i)}
                    {@attach drags.tile(i)}
                    animate:moved
                    transition:pop>
                    {#if editing}
                      <div class="pics" aria-hidden="true">{@render mosaic(pics)}</div>
                      {#if saved.list.length > 1}
                        <button
                          class="icon corner grip"
                          data-grip={s.id}
                          aria-label={t('saved.reorder', { name: s.name })}
                          title={t('saved.holdToMove')}
                          onkeydown={(e) => keyMove(e, i)}
                          transition:pop><Icon name="grip" size={16} /></button>
                      {/if}
                      <button class="icon corner del" data-nodrag aria-label={t('saved.deleteLabel', { name: s.name })} title={t('common.delete')} onclick={() => remove(s)} transition:pop
                        ><Icon name="trash" size={16} /></button>
                      <div class="foot">
                        <input
                          class="rename"
                          value={s.name}
                          aria-label={t('saved.nameOf', { name: s.name })}
                          maxlength="80"
                          enterkeyhint="done"
                          onkeydown={(e) => e.key === 'Enter' && (e.currentTarget as HTMLInputElement).blur()}
                          onblur={(e) => rename(s, (e.currentTarget as HTMLInputElement).value)} />
                      </div>
                    {:else}
                      <button class="run" onclick={() => run(s)} title={s.query || t('push.everything')} data-autofocus={i === 0 && (current || !query.trim()) ? '' : undefined}>
                        <span class="pics" aria-hidden="true">{@render mosaic(pics)}</span>
                        {#if saved.newOf[s.id]}<span class="new-pill" title={t('saved.newTitle')} transition:pop>{t('library.new', { n: saved.newOf[s.id] })}</span>{/if}
                        <span class="foot">
                          <span class="name marquee sliding" {@attach marquee}><span>{s.name}</span></span>
                          <span class="facts"
                            >{t('common.filesShort', { n: s.count, x: short(s.count) })}{#if sl}<span class="sortl"> · {sl}</span>{/if}</span>
                        </span>
                      </button>
                    {/if}
                  </li>
                {/each}
              </ul>
            {:else if saved.loaded}
              <div class="empty-saved" transition:reveal>
                <Icon name="save" size={28} />
                <p><strong>{t('saved.none')}</strong></p>
                <p class="muted">{query.trim() ? t('saved.noneSave') : t('saved.noneSearch')}</p>
              </div>
            {/if}
          {:else}
            <!-- Group (parent) tags: one tap away like a saved search, with their own NEW pills and their own order. -->
            <ul class="tiles" class:dragging={!!groupDrags.drag}>
              {#each groups as g, i (g.tag)}
                {@const pics = g.look?.pics ?? []}
                {@const kids = g.look?.kids ?? []}
                <li class="tile" class:lifted={groupDrags.drag?.from === i} style:transform={dragOf(groupDrags, i)} {@attach groupDrags.tile(i)} animate:moved transition:pop>
                  <button class="run" onclick={() => runGroup(g.tag)} data-autofocus={i === 0 ? '' : undefined}>
                    <span class="pics" aria-hidden="true">{@render mosaic(pics)}</span>
                    {#if g.new}<span class="new-pill" title={t('saved.newTitle')} transition:pop>{t('library.new', { n: g.new })}</span>{/if}
                    <span class="foot">
                      <span class="name marquee sliding" style:color={tagColor(g.tag)} {@attach marquee}><span>{label({ text: g.tag, neg: false })}</span></span>
                      <span class="facts"
                        >{t('saved.groupFacts', {
                          tags: t('common.tags', { n: g.children }),
                          files: t('common.filesShort', { n: g.look?.count ?? 0, x: short(g.look?.count ?? 0) }),
                        })}</span>
                      {#if kids.length}<span class="kids"
                          >{g.children > kids.length ? t('saved.andMore', { list: kids.join(', '), n: num(g.children - kids.length) }) : kids.join(', ')}</span>{/if}
                    </span>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/key}
    </div>
  </div>
</div>

<!-- A tile's pictures: its first files, one big and two small (fewer: they share the room); none yet: a quiet box. -->
{#snippet mosaic(pics: SavedLook['pics'])}
  {#if pics.length}
    <span class="mosaic n{pics.length}">{#each pics as p (p.id)}<img src={p.thumb} alt="" draggable="false" loading="lazy" {@attach fadeIn} />{/each}</span>
  {:else}
    <span class="mosaic none"><Icon name="library" size={22} /></span>
  {/if}
{/snippet}

<script module lang="ts">
  /** A picture that has to load fades in (no pop) - one the browser has already shows at once. */
  function fadeIn(img: HTMLImageElement) {
    if (img.complete && img.naturalWidth) return;
    img.style.opacity = '0';
    const show = () => {
      img.style.transition = 'opacity 0.25s';
      img.style.opacity = '1';
    };
    img.addEventListener('load', show, { once: true });
    img.addEventListener('error', show, { once: true });
  }
</script>

<style>
  .panel {
    display: grid;
    grid-template-columns: minmax(0, 1fr); /* long names slide instead of widening it */
    align-content: start;
    gap: 10px;
    padding: 0 12px 14px;
    overflow-y: auto;
    overscroll-behavior: contain;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    outline: none;
  }
  /* Wide screens: floating at the right under the search bar, like the viewer's details panel. */
  .panel.side {
    position: fixed;
    z-index: 61;
    top: var(--side-top, 64px);
    right: 12px;
    bottom: calc(12px + var(--bottom-room));
    width: min(404px, calc(100vw - 24px));
    border-radius: 16px;
    box-shadow: 0 20px 50px rgb(0 0 0 / 0.55);
  }
  /* Phones: a sheet like the details panel - edge to edge along the bottom, a grab bar (drag down to close). */
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgb(17 17 27 / 0.5);
  }
  .panel.sheet {
    position: fixed;
    z-index: 61;
    left: 0;
    right: 0;
    bottom: 0;
    max-height: var(--sheet-max-h);
    padding-bottom: calc(24px + var(--bottom-room));
    border-width: 1px 0 0;
    border-radius: 20px 20px 0 0;
    box-shadow: 0 -12px 40px rgb(0 0 0 / 0.55);
  }
  .grab-zone {
    position: sticky;
    top: 0;
    z-index: 2;
    display: grid;
    place-items: center;
    height: 22px;
    margin: 0 -12px -12px;
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
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 -12px;
    padding: 8px 6px 8px 16px;
    border-bottom: 1px solid var(--surface0);
  }
  h2 {
    flex: 1;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 17px;
    font-weight: 700;
  }
  h2 :global(svg),
  .note :global(svg) {
    color: var(--mauve);
    flex: none;
  }
  .bar-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
  }
  .grow {
    flex: 1;
  }
  .bar-row .segmented {
    width: auto;
  }
  .bar-row .segmented button {
    flex: none;
    min-width: 0;
    min-height: 0;
    height: 30px;
    padding: 0 14px;
  }
  .small-btn {
    min-height: 0;
    height: 32px;
    padding: 0 12px;
    font-size: var(--font-small);
  }
  .add {
    padding: 0 12px 0 9px;
    gap: 4px;
  }
  .save {
    display: flex;
    gap: 8px;
  }
  .save input {
    flex: 1;
    min-width: 0;
  }
  .save button {
    flex: none;
  }
  /* "This search is saved as..." / "Changed from ...": a tinted line above the tiles. */
  .note {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    padding: 6px 6px 6px 10px;
    min-height: 32px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--mauve) 10%, var(--crust));
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .note :global(b),
  .note :global(strong) {
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .changed-text {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .changed .small-btn {
    flex: none;
  }

  /* ---- the tiles: two to a row, each its first files above its name and facts. */
  .tiles {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px;
  }
  .tile {
    position: relative;
    min-width: 0;
    border-radius: 12px;
    background: var(--crust);
    border: 1px solid var(--surface0);
    -webkit-user-select: none;
    user-select: none;
    -webkit-touch-callout: none;
    touch-action: manipulation;
  }
  .dragging .tile {
    transition: transform 0.2s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .tile.active {
    border-color: color-mix(in srgb, var(--mauve) 60%, transparent);
  }
  /* Picked up: above the others, a little bigger and tilted, following the finger. */
  .tile.lifted {
    z-index: 3;
    transition: none !important;
    border-color: var(--mauve);
    box-shadow: 0 18px 36px rgb(0 0 0 / 0.55);
  }
  .tile.lifted > :global(*) {
    transform: scale(1.04) rotate(-1.5deg);
    transition: transform 0.15s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .run {
    display: grid;
    width: 100%;
    min-height: 0;
    height: auto;
    padding: 0;
    gap: 0;
    background: none;
    border-radius: 11px;
    overflow: hidden;
    text-align: left;
    color: var(--text);
    font-weight: 400;
    white-space: normal;
  }
  @media (hover: hover) {
    .run:hover {
      filter: none;
      background: var(--surface0);
    }
  }
  .pics {
    display: block;
    border-radius: 11px 11px 0 0;
    overflow: hidden;
  }
  /* (one big, two small: like a subscription group's mosaic on the Downloads page) */
  .mosaic {
    display: grid;
    grid-template-columns: 2fr 1fr;
    grid-template-rows: 1fr 1fr;
    gap: 2px;
    height: 96px;
    background: var(--surface0);
  }
  .mosaic img {
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    object-fit: cover;
    -webkit-user-drag: none;
  }
  .mosaic.n1 img {
    grid-area: 1 / 1 / 3 / 3;
  }
  .mosaic.n2 img {
    grid-row: 1 / 3;
  }
  .mosaic.n3 img:first-child {
    grid-row: 1 / 3;
  }
  .mosaic.none {
    display: grid;
    grid-template-columns: 1fr;
    place-items: center;
    color: var(--overlay0);
  }
  .foot {
    display: grid;
    gap: 1px;
    padding: 7px 10px 9px;
    min-width: 0;
  }
  .name {
    font-weight: 700;
    font-size: 14px;
    line-height: 1.35;
  }
  .facts,
  .kids {
    font-size: 12px;
    color: var(--subtext);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-variant-numeric: tabular-nums;
  }
  .kids {
    font-size: 11px;
    color: var(--overlay1);
  }
  .sortl {
    color: var(--overlay1);
  }
  /* New since it was last opened from here (like a thumbnail's NEW pill). */
  .new-pill {
    position: absolute;
    top: 6px;
    left: 6px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--mauve);
    color: var(--crust);
    font-size: 11px;
    font-weight: 800;
    letter-spacing: 0.02em;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    box-shadow: 0 2px 6px rgb(0 0 0 / 0.35);
  }
  /* Edit: the name in a box, a handle (arrow keys) and Delete on the pictures. */
  .rename {
    width: 100%;
    min-width: 0;
    min-height: 0;
    height: 32px;
    font-size: 14px;
    font-weight: 600;
  }
  .corner {
    position: absolute;
    top: 6px;
    width: 30px;
    min-width: 30px;
    height: 30px;
    min-height: 0;
    border-radius: 8px;
    background: rgb(17 17 27 / 0.7);
    color: var(--text);
    backdrop-filter: blur(6px);
  }
  .grip {
    left: 6px;
    cursor: grab;
  }
  .del {
    right: 6px;
    color: var(--red) !important;
  }
  .empty-saved {
    display: grid;
    justify-items: center;
    gap: 2px;
    padding: 14px 8px 8px;
    text-align: center;
    color: var(--overlay0);
  }
  .empty-saved p {
    margin: 0;
    max-width: 34ch;
    font-size: var(--font-small);
  }
  .empty-saved strong {
    color: var(--text);
    font-size: var(--font);
  }
  @media (prefers-reduced-motion: reduce) {
    .dragging .tile,
    .tile.lifted > :global(*) {
      transition: none;
    }
  }
</style>
