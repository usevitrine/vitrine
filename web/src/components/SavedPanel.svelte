<script lang="ts">
  // Saved searches: save the current search (and its order), run one with a
  // tap; changed after running one, update it in one tap (or save it as a new
  // one); in Edit mode rename, reorder or delete them. Opened from the star in
  // the search bar; the list lives on the server.
  import { backClose } from '../lib/backClose';
  import { swallowPress, leaving } from '../lib/outside';
  import { onMount, tick } from 'svelte';
  import { useScope } from '../lib/scope.svelte';
  import { settings } from '../lib/settings.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { kindOf, label, parse } from '../lib/query';
  import { tagColor } from '../lib/namespaces.svelte';
  import { sortOptions } from './SortMenu.svelte';
  import type { SavedSearch } from '../lib/api';
  import Icon from './Icon.svelte';
  import { ListDrag } from '../lib/listDrag.svelte';
  import Rich from './Rich.svelte';
  import { t, num } from '../lib/i18n.svelte';

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
  let name = $state('');
  let busy = $state(false);
  let panel = $state<HTMLElement>();

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

  onMount(async () => {
    saved.refreshNews();
    await saved.load(); // fresh counts every time it opens
    await tick();
    (panel?.querySelector<HTMLElement>('[data-autofocus]') ?? panel)?.focus();
  });

  const sortLabel = (s: string) =>
    s && s !== defaultSort ? (sortOptions(comics).find((o) => o.value === s)?.label ?? s) : '';

  async function saveCurrent(e?: SubmitEvent) {
    e?.preventDefault();
    if (!query.trim() || busy) return;
    busy = true;
    try {
      const n = name.trim() || autoName;
      saved.lastRun = await saved.add(n, query, sort === defaultSort ? '' : sort);
      name = '';
      toasts.show(t('saved.saved', { name: n }), { cat: 'savedSearches' });
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
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }

  async function remove(s: SavedSearch) {
    const order = saved.list.map((x) => x.id);
    try {
      await saved.remove(s.id);
      toasts.show(t('saved.deleted', { name: s.name }), { cat: 'savedSearches',
        kind: 'removed',
        action: {
          label: t('common.undo'),
          run: async () => {
            const id = await saved.add(s.name, s.query, s.sort);
            await saved.reorder(order.map((x) => (x === s.id ? id : x)));
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

  // ---- drag to reorder (Edit mode), like the download queue.
  const lists = new ListDrag(move);

  // Arrow keys move between rows.
  function onKey(e: KeyboardEvent) {
    if (editing || (e.key !== 'ArrowDown' && e.key !== 'ArrowUp')) return;
    const items = [...(panel?.querySelectorAll<HTMLElement>('.run') ?? [])];
    const i = items.indexOf(document.activeElement as HTMLElement);
    const next = items[e.key === 'ArrowDown' ? Math.min(i + 1, items.length - 1) : Math.max(i - 1, 0)];
    if (next) {
      e.preventDefault();
      next.focus();
    }
  }

  // A search's chips past two lines are hidden: "+N" says how many.
  function clampChips(node: HTMLElement) {
    const measure = () => {
      const h = node.clientHeight;
      let hidden = 0;
      for (const c of node.children) {
        const el = c as HTMLElement;
        if (el.offsetTop + el.offsetHeight > h + 1) hidden++;
      }
      node.dataset.more = hidden ? '+' + num(hidden) : '';
      node.classList.toggle('clipped', hidden > 0);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(node);
    return () => ro.disconnect();
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
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions (Esc and arrow keys for the whole panel) -->
<div class="panel" bind:this={panel} {@attach backClose(() => onclose(false))} role="dialog" aria-label={t('saved.title')} tabindex="-1" onkeydown={onKey}>
  <div class="head">
    <h2><Icon name="star" size={16} filled /> {t('saved.title')}</h2>
    {#if saved.list.length}
      <button class="quiet small-btn" aria-pressed={editing} onclick={() => (editing = !editing)}>{editing ? t('saved.done') : t('saved.edit')}</button>
    {/if}
  </div>

  {#if query.trim() && !editing}
    {#if current}
      <p class="is-saved"><Icon name="star" size={16} filled /> <span><Rich text={t('saved.isSaved', { name: current.name })} /></span></p>
    {:else}
      {#if changedFrom}
        {@const s = changedFrom}
        <!-- Run, then changed: update it in one tap (or save it as a new one below). -->
        <div class="changed">
          <span class="changed-text"><Rich text={t('saved.changedFrom', { name: s.name })} /></span>
          <button onclick={() => useCurrent(s)} data-autofocus><Icon name="star" size={16} filled /> {t('saved.update')}</button>
        </div>
      {/if}
      <form class="save" onsubmit={saveCurrent}>
        <input
          bind:value={name}
          placeholder={autoName}
          aria-label={t('saved.nameLabel')}
          maxlength="80"
          autocomplete="off"
          enterkeyhint="done" />
        <button type="submit" class:quiet={!!changedFrom} disabled={busy} data-autofocus={changedFrom ? undefined : ''}><Icon name="star" size={16} /> {changedFrom ? t('saved.saveNew') : t('common.save')}</button>
      </form>
    {/if}
  {/if}

  {#if saved.list.length}
    <ul class="list" class:dragging={!!lists.drag}>
      {#each saved.list as s, i (s.id)}
        {@const sl = sortLabel(s.sort)}
        <li
          class:active={current?.id === s.id}
          class:lifted={lists.drag?.from === i}
          style:transform={lists.shift(i) ? `translateY(${lists.shift(i)}px)` : undefined}>
          {#if editing}
            <div class="edit-row">
              {#if saved.list.length > 1}
                <button
                  class="icon grip"
                  aria-label={t('saved.reorder', { name: s.name })}
                  title={t('saved.dragToReorder')}
                  onpointerdown={(e) => lists.start(e, i)}
                  onpointermove={lists.moveTo}
                  onpointerup={lists.end}
                  onpointercancel={lists.end}
                  onkeydown={(e) => {
                    if (e.key === 'ArrowUp' && i > 0) move(i, i - 1);
                    else if (e.key === 'ArrowDown' && i < saved.list.length - 1) move(i, i + 1);
                    else return;
                    e.preventDefault();
                  }}><Icon name="grip" size={18} /></button>
              {/if}
              <input
                class="rename"
                value={s.name}
                aria-label={t('saved.nameOf', { name: s.name })}
                maxlength="80"
                enterkeyhint="done"
                onkeydown={(e) => e.key === 'Enter' && (e.currentTarget as HTMLInputElement).blur()}
                onblur={(e) => rename(s, (e.currentTarget as HTMLInputElement).value)} />
              <button class="icon del" aria-label={t('saved.deleteLabel', { name: s.name })} title={t('common.delete')} onclick={() => remove(s)}><Icon name="trash" size={18} /></button>
            </div>
          {:else}
            <button class="run" onclick={() => run(s)} data-autofocus={i === 0 && (current || !query.trim()) ? '' : undefined}>
              <span class="top-line">
                <span class="name">{s.name}</span>
                {#if sl}<span class="sortl"><Icon name="sort" size={13} /> {sl}</span>{/if}
                {#if saved.newOf[s.id]}<span class="new-pill" title={t('saved.newTitle')}>{t('library.new', { n: saved.newOf[s.id] })}</span>{/if}
                <span class="count">{num(s.count)}</span>
              </span>
              <!-- Its chips: two lines at most ("+N" for the rest - a long search took the whole panel). -->
              <span class="q" {@attach clampChips}>
                {#each parse(s.query) as c, ci (ci)}
                  <span class="qc"
                    >{#each c.atoms as a, j (j)}{#if j > 0}<span class="or">{t('chip.or')}</span>{/if}<span
                          class="atom {kindOf(a.text)}"
                          class:neg={a.neg}
                          style:--c={tagColor(a.text)}>{a.neg ? '−' : ''}{label(a, comics)}</span
                        >{/each}</span
                  >
                {:else}
                  <span class="qc muted">{t('push.everything')}</span>
                {/each}
              </span>
            </button>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  <!-- Group (parent) tags: one tap away like a saved search, with their own NEW pills. -->
  {#if saved.groups.length && !editing}
    <h3 class="sub">{t('saved.groups')}</h3>
    <ul class="list groups">
      {#each saved.groups as g (g.tag)}
        <li>
          <button class="run" onclick={() => runGroup(g.tag)}>
            <span class="top-line">
              <span class="name"><span class="atom tag" style:--c={tagColor(g.tag)}>{label({ text: g.tag, neg: false })}</span></span>
              {#if g.new}<span class="new-pill" title={t('saved.newTitle')}>{t('library.new', { n: g.new })}</span>{/if}
              <span class="count">{t('common.tags', { n: g.children })}</span>
            </span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}

  {#if !saved.list.length && saved.loaded && !saved.groups.length}
    <div class="empty-saved">
      <Icon name="star" size={28} />
      <p><strong>{t('saved.none')}</strong></p>
      <p class="muted">
        {query.trim() ? t('saved.noneSave') : t('saved.noneSearch')}
      </p>
    </div>
  {/if}
</div>

<style>
  .panel {
    display: grid;
    grid-template-columns: minmax(0, 1fr); /* long searches wrap instead of widening it */
    gap: 10px;
    padding: 12px;
    max-height: min(70dvh, 620px);
    overflow-y: auto;
    overscroll-behavior: contain;
    background: var(--mantle);
    border: 1px solid var(--surface1);
    border-radius: 14px;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.5);
    outline: none;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: 28px;
  }
  h2 {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: var(--font);
    font-weight: 700;
  }
  h2 :global(svg),
  .is-saved :global(svg) {
    color: var(--mauve);
  }
  .small-btn {
    min-height: 0;
    height: 28px;
    padding: 0 12px;
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
  /* Run, then changed: update it (the main button), or save it as a new one below. */
  .changed {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 6px 6px 10px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--mauve) 10%, var(--crust));
  }
  .changed-text {
    flex: 1;
    min-width: 0;
    color: var(--subtext);
    font-size: var(--font-small);
    overflow-wrap: anywhere;
  }
  .changed-text :global(b) {
    color: var(--text);
  }
  .changed button {
    flex: none;
  }
  .is-saved {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    padding: 8px 10px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--mauve) 10%, var(--crust));
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .is-saved :global(strong) {
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
  }
  li {
    border-radius: 10px;
    background: var(--crust);
    border: 1px solid var(--surface0);
    transition: transform 0.15s;
  }
  li.active {
    border-color: color-mix(in srgb, var(--mauve) 60%, transparent);
    background: color-mix(in srgb, var(--mauve) 8%, var(--crust));
  }
  li.lifted {
    transition: none;
    position: relative;
    z-index: 2;
    border-color: var(--mauve);
    box-shadow: 0 10px 28px rgb(0 0 0 / 0.45);
  }
  .run {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    justify-content: flex-start;
    gap: 4px;
    width: 100%;
    min-height: 0;
    height: auto;
    padding: 9px 12px;
    background: none;
    border-radius: 10px;
    text-align: left;
    color: var(--text);
    font-weight: 400;
  }
  @media (hover: hover) {
    .run:hover {
      filter: none;
      background: var(--surface0);
    }
  }
  .top-line {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .name {
    min-width: 0;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  /* New since it was last opened from here (like a thumbnail's NEW pill). */
  .new-pill {
    flex: none;
    align-self: center;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--mauve);
    color: var(--crust);
    font-size: 11px;
    font-weight: 800;
    letter-spacing: 0.02em;
    font-variant-numeric: tabular-nums;
  }
  h3.sub {
    margin: 4px 2px -2px;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--overlay1);
  }
  .count {
    margin-left: auto; /* (at the end; the NEW pill stays by the name) */
    flex: none;
    font-size: var(--font-small);
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .q {
    --line: 1.45em;
    position: relative;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 3px 8px;
    font-size: var(--font-small);
    line-height: var(--line);
    max-height: calc(var(--line) * 2 + 3px);
    overflow: hidden;
  }
  .q.clipped::after {
    content: attr(data-more);
    position: absolute;
    right: 0;
    bottom: 0;
    height: var(--line);
    padding-left: 22px;
    color: var(--subtext);
    font-weight: 600;
    background: linear-gradient(to right, transparent, var(--row) 18px);
  }
  li {
    --row: var(--crust);
  }
  li.active {
    --row: color-mix(in srgb, var(--mauve) 8%, var(--crust));
  }
  .qc {
    display: inline-flex;
    flex-wrap: wrap;
    min-width: 0;
    gap: 0 4px;
    align-items: baseline;
    overflow-wrap: anywhere;
  }
  .atom {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .atom.tag,
  .atom.wildcard {
    color: var(--c);
  }
  .atom.neg {
    text-decoration: none;
  }
  .atom.system {
    color: var(--teal);
  }
  .atom.limit {
    color: var(--peach);
  }
  .atom.wildcard {
    color: var(--yellow);
  }
  .or {
    font-size: 10px;
    font-weight: 700;
    color: var(--mauve);
    text-transform: uppercase;
  }
  .sortl {
    flex: none;
    font-size: var(--font-small);
    display: inline-flex;
    align-items: center;
    gap: 3px;
    color: var(--overlay0);
  }
  .edit-row {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px;
  }
  .rename {
    flex: 1;
    min-width: 0;
  }
  .grip {
    flex: none;
    cursor: grab;
    touch-action: none;
    color: var(--overlay0);
  }
  .update {
    flex: none;
    min-height: 0;
    height: calc(var(--control-h) - 4px);
    padding: 0 10px;
    font-size: var(--font-small);
  }
  .del {
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
</style>
