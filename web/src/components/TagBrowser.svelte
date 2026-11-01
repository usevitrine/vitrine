<script lang="ts">
  // All tags: filter, namespace, site (the tags of its files), most used or A-Z; tap a tag to search it,
  // rename it (your own tags) or alias it to another (a sibling - for tags from
  // sites, which a tag refresh would otherwise bring back under the old name).
  import { api, type BrowsedTag, type Relation } from '../lib/api';
  import { reveal, moved, swap, nudge } from '../lib/motion';
  import { router } from '../lib/router.svelte';
  import { toasts, attempt } from '../lib/toast.svelte';
  import { siteLabel } from '../lib/sites.svelte';
  import { namespaces, nsChoices, moveToNamespace, nsColor, nsLabel, nsRank, splitTag } from '../lib/namespaces.svelte';
  import Dropdown from './Dropdown.svelte';
  import TagInput from './TagInput.svelte';
  import Icon from './Icon.svelte';
  import SubMark from './SubMark.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import Rich from './Rich.svelte';
  import { t, num } from '../lib/i18n.svelte';

  let { onrelations }: { onrelations: (r: { parents: Relation[]; siblings: Relation[] }) => void } = $props();

  const PAGE = 100;
  let filter = $state('');
  let ns = $state(''); // '' = all, '-' = no namespace
  let site = $state(''); // '' = all sites; a site: only the tags of its files, counted by them
  let sort = $state<'count' | 'name'>('count');

  // The sites the library has files from (media and comics), the most files first.
  let siteList = $state<string[]>([]);
  // (either can fail alone - the other's sites still show; both failing: no site picker)
  Promise.allSettled([api.siteCounts(''), api.siteCounts('', 'comics')]).then((res) => {
    const n: Record<string, number> = {};
    for (const r of res) if (r.status === 'fulfilled') for (const s of r.value.sites) n[s.site] = (n[s.site] ?? 0) + s.count;
    siteList = Object.keys(n).sort((a, b) => n[b] - n[a] || siteLabel(a).localeCompare(siteLabel(b)));
  });
  let tags = $state<BrowsedTag[]>([]);
  let total = $state(0);
  let loading = $state(false);
  let error = $state('');
  let open = $state<string | null>(null); // the tag whose actions are shown
  let mode = $state<'rename' | 'alias' | null>(null);
  let target = $state('');
  let busy = $state(false);

  // Namespace chips: All, the namespaces in booru order, General.
  let chips = $derived([
    { value: '', label: t('tagBrowser.all'), color: 'var(--text)' },
    ...[...namespaces.names, '']
      .sort((a, b) => nsRank(a).localeCompare(nsRank(b)))
      .map((n) => ({ value: n || '-', label: nsLabel(n), color: nsColor(n) })),
  ]);

  let gen = 0;
  /** Loads the first page, the next one (more), or after an edit as many as are shown (keep: the page stays where it was scrolled). */
  async function load(more = false, keep = false) {
    const g = ++gen;
    loading = true;
    try {
      const res = await api.tagList(filter, ns, site, sort, more ? tags.length : 0, keep ? Math.max(PAGE, tags.length) : PAGE);
      if (g !== gen) return;
      tags = more ? [...tags, ...res.tags] : res.tags;
      total = res.total;
      error = '';
    } catch (e) {
      if (g === gen) error = (e as Error).message;
    } finally {
      if (g === gen) loading = false;
    }
  }

  // Reload when the filter, namespace, site or order changes (typing: a moment after the last key).
  let timer = 0;
  $effect(() => {
    void ns;
    void site;
    void sort;
    const f = filter;
    clearTimeout(timer);
    timer = window.setTimeout(() => load(), f ? 180 : 0);
    open = null;
  });

  /** Opens a tag's rename or alias form (under the tag). */
  function startEdit(bt: BrowsedTag, m: 'rename' | 'alias') {
    open = bt.tag;
    mode = m;
    target = '';
  }

  function search(tag: string) {
    router.openSearch(tag);
  }

  async function rename(bt: BrowsedTag) {
    const to = target.trim();
    if (!to || to === bt.tag) return;
    await attempt(async () => {
      const { files } = await api.renameTag(bt.tag, to);
      toasts.show(t('tagBrowser.renamed', { from: bt.tag, to, n: files }), { cat: 'tagEdits' });
      open = null;
      load(false, true);
    }, (b) => (busy = b));
  }

  async function alias(bt: BrowsedTag) {
    const to = target.trim();
    if (!to || to === bt.tag) return;
    await attempt(async () => {
      onrelations(await api.editRelation('sibling', 'add', bt.tag, to));
      toasts.show(t('tagBrowser.aliased', { from: bt.tag, to }), { cat: 'tagEdits' });
      open = null;
      load(false, true);
    }, (b) => (busy = b));
  }
</script>

<section class="card" aria-label={t('tags.section.all.label')}>
  <p class="hint">{t('tagBrowser.hint')}</p>

  <div class="controls">
    <span class="filter">
      <Icon name="search" size={16} />
      <input bind:value={filter} placeholder={t('tagBrowser.filter')} aria-label={t('tagBrowser.filter')} autocapitalize="off" autocomplete="off" spellcheck="false" />
    </span>
    <Dropdown
      value={sort}
      options={[
        { value: 'count', label: t('tagBrowser.mostUsed') },
        { value: 'name', label: t('tagBrowser.az') },
      ]}
      onchange={(v) => (sort = v as 'count' | 'name')}
      label={t('tagBrowser.order')}
      align="end" />
    {#if siteList.length > 1 || site}
      <span class="site">
        <Dropdown
          value={site}
          options={[{ value: '', label: t('tagBrowser.allSites') }, ...siteList.map((id) => ({ value: id, label: siteLabel(id) }))]}
          onchange={(v) => (site = v)}
          label={t('tagBrowser.site')}
          align="end" />
      </span>
    {/if}
  </div>

  <div class="chips" role="radiogroup" aria-label={t('tagMenu.namespace')}>
    {#each chips as c (c.value)}
      <button
        class="chip"
        role="radio"
        aria-checked={ns === c.value}
        style:--c={c.color}
        onclick={() => (ns = c.value)}>{c.label}</button>
    {/each}
  </div>

  {#if error}
    <p class="error small" transition:reveal>{error}</p>
  {/if}

  <p class="summary small muted" aria-live="polite">
    {#if loading && !tags.length}{t('common.loading')}{:else}{t('common.tags', { n: total })}{/if}
  </p>

  {#if tags.length}
    <ul class="list" transition:reveal>
      {#each tags as bt (bt.tag)}
        {@const st = splitTag(bt.tag)}
        <li class:open={open === bt.tag} style:--c={nsColor(bt.namespace)} animate:moved in:swap>
          <div class="line">
            <button class="row" title={t('tagBrowser.searchFor', { tag: bt.tag })} onclick={() => search(bt.tag)}>
              <span class="name">
                {#if bt.namespace}<span class="ns">{bt.namespace}:</span>{/if}{st.name}
                {#if bt.ideal}<span class="ideal" transition:nudge>→ {bt.ideal}</span>{/if}
              </span>
              <SubMark tag={bt.ideal ?? bt.tag} size={15} />
              <span class="count">{num(bt.count)}</span>
            </button>
            <ActionMenu
              label={t('common.moreFor', { name: bt.tag })}
              title={bt.tag}
              items={[
                { label: t('tagMenu.search'), icon: 'search', run: () => search(bt.tag) },
                bt.from_sites
                  ? { label: t('tagBrowser.rename'), icon: 'tag', disabled: true, hint: t('tagBrowser.siteTag') }
                  : { label: t('tagBrowser.renameMenu'), icon: 'tag', run: () => startEdit(bt, 'rename') },
                { label: t('tagBrowser.aliasMenu'), icon: 'link', hint: t('tagBrowser.aliasHint'), run: () => startEdit(bt, 'alias') },
                {
                  label: t('tagMenu.namespace'),
                  icon: 'folder',
                  hint: nsLabel(bt.namespace),
                  choices: {
                    heading: t('tagMenu.namespace'),
                    value: bt.namespace,
                    options: nsChoices().map((ns) => ({ value: ns, label: nsLabel(ns), dot: nsColor(ns) })),
                    pick: async (ns) => {
                      if (ns !== bt.namespace && (await moveToNamespace(bt.tag, ns))) load(false, true);
                    },
                  },
                },
              ]} />
          </div>
          {#if open === bt.tag && mode}
            <div class="actions" transition:reveal>
              {#if mode}
                <form
                  class="edit"
                  onsubmit={(e) => {
                    e.preventDefault();
                    if (mode === 'rename') rename(bt);
                    else alias(bt);
                  }} transition:reveal>
                  <p class="hint">
                    <Rich text={t(mode === 'rename' ? 'tagBrowser.renameHint' : 'tagBrowser.aliasFormHint', { tag: bt.tag })} />
                  </p>
                  <div class="edit-row">
                    <TagInput
                      bind:value={target}
                      autofocus
                      label={mode === 'rename' ? t('tagBrowser.newName') : t('tags.siblings.ideal')}
                      placeholder={mode === 'rename' ? t('tagBrowser.newNameExample') : t('tagBrowser.idealExample')}
                      onenter={() => (mode === 'rename' ? rename(bt) : alias(bt))}
                      onescape={() => (mode = null)} />
                    <button type="submit" disabled={busy || !target.trim()}>{mode === 'rename' ? t('tagBrowser.rename') : t('tagBrowser.alias')}</button>
                    <button type="button" class="quiet" onclick={() => (mode = null)}>{t('common.cancel')}</button>
                  </div>
                </form>
              {/if}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
    {#if tags.length < total}
      <button class="quiet more" disabled={loading} onclick={() => load(true)} transition:reveal>
        {loading ? t('common.loading') : t('tagBrowser.more', { n: total - tags.length })}
      </button>
    {/if}
  {:else if !loading && !error}
    <p class="empty" transition:reveal>{filter || ns || site ? t('tagBrowser.noMatch') : t('tagBrowser.none')}</p>
  {/if}
</section>

<style>
  .controls {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  /* (phones: the filter on a row of its own, the order and the site under it) */
  @media (max-width: 639px) {
    .filter {
      flex-basis: 100%;
    }
    .controls .site {
      flex: 1;
      min-width: 0;
    }
    .controls .site :global(.dropdown) {
      width: 100%;
    }
  }
  .controls .site {
    display: grid;
  }
  .controls .site :global(.dropdown) {
    width: 170px;
  }
  .filter {
    position: relative;
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
  }
  .filter :global(svg) {
    position: absolute;
    left: 11px;
    color: var(--overlay0);
    pointer-events: none;
  }
  .filter input {
    width: 100%;
    padding-left: 34px;
  }
  .controls :global(.dropdown) {
    flex: none;
    width: 128px;
    display: grid;
  }
  .chips {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    scrollbar-width: none;
    margin: 0 -2px;
    padding: 2px;
  }
  .chips::-webkit-scrollbar {
    display: none;
  }
  .chip {
    flex: none;
    min-height: 0;
    height: calc(var(--control-h) - 6px);
    padding: 0 12px;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--c);
    font-weight: 600;
  }
  .chip[aria-checked='true'] {
    background: color-mix(in srgb, var(--c) 22%, var(--surface0));
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--c) 60%, transparent);
  }
  .summary {
    margin: 0;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    border-radius: 12px;
    border: 1px solid var(--surface0);
    background: var(--crust);
    overflow: hidden;
  }
  li + li {
    border-top: 1px solid var(--surface0);
  }
  li.open {
    background: color-mix(in srgb, var(--c) 6%, var(--crust));
  }
  .line {
    display: flex;
    align-items: center;
    padding-right: 4px;
  }
  .line .row {
    flex: 1;
    min-width: 0;
  }
  .row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    min-height: 0;
    height: auto;
    padding: 9px 14px;
    background: none;
    border-radius: 0;
    text-align: left;
    font-weight: 500;
    color: var(--c);
  }
  /* (the whole line lights, its dots button included) */
  @media (hover: hover) {
    .row:hover {
      filter: none;
    }
    .line:hover {
      background: var(--surface0);
    }
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  /* (a subscription follows it: the bell before the count - lib/subTags) */
  .row :global(.sub-mark) {
    align-self: center;
  }
  .ns {
    opacity: 0.7;
  }
  .ideal {
    margin-left: 6px;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .count {
    flex: none;
    color: var(--subtext);
    font-size: var(--font-small);
    font-variant-numeric: tabular-nums;
  }
  .actions {
    padding: 0 14px 12px;
    display: grid;
    gap: 8px;
  }
  .edit .hint {
    margin: 0;
  }
  .edit {
    display: grid;
    gap: 8px;
  }
  .edit-row {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .edit-row :global(.wrap) {
    flex: 1;
    min-width: 0;
  }
  .more {
    justify-self: center;
  }
</style>
