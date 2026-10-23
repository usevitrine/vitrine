<script lang="ts">
  // Tag parents (group tags: searching the parent finds every child's files)
  // and siblings (one tag stands for another).
  import { onMount } from 'svelte';
  import { api, type Relation } from '../lib/api';
  import { toasts } from '../lib/toast.svelte';
  import TagInput from '../components/TagInput.svelte';
  import { tagColor } from '../lib/namespaces.svelte';
  import Icon from '../components/Icon.svelte';
  import BlacklistPanel from '../components/BlacklistPanel.svelte';
  import TagBrowser from '../components/TagBrowser.svelte';
  import Sections from '../components/Sections.svelte';
  import { tagSections } from '../lib/settingsSections';
  import { t } from '../lib/i18n.svelte';

  let parents = $state<Relation[]>([]);
  let siblings = $state<Relation[]>([]);
  let child = $state('');
  let parent = $state('');
  let from = $state('');
  let to = $state('');

  // What's typed in each parent's "add children" box.
  let childDraft = $state<Record<string, string>>({});
  async function addChildren(parent: string, text = childDraft[parent] ?? '') {
    const list = text.split(/\s+/).filter(Boolean);
    childDraft[parent] = '';
    for (const c of list) if (!(await edit('parent', 'add', c, parent))) return;
  }

  // Parents grouped by parent tag, children sorted.
  let groups = $derived.by(() => {
    const m = new Map<string, string[]>();
    for (const r of parents) m.set(r.to, [...(m.get(r.to) ?? []), r.from]);
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0])).map(([p, cs]) => ({ parent: p, children: cs.sort() }));
  });

  async function load() {
    try {
      const r = await api.relations();
      parents = r.parents;
      siblings = r.siblings;
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  onMount(load);

  async function edit(kind: 'parent' | 'sibling', action: 'add' | 'remove', a: string, b = '') {
    try {
      const r = await api.editRelation(kind, action, a.trim(), b.trim());
      parents = r.parents;
      siblings = r.siblings;
      return true;
    } catch (e) {
      toasts.error((e as Error).message);
      return false;
    }
  }

  async function addParent() {
    const children = child.split(/\s+/).filter(Boolean);
    if (!children.length || !parent.trim()) return;
    for (const c of children) if (!(await edit('parent', 'add', c, parent))) return;
    child = '';
  }

  async function addSibling() {
    if (!from.trim() || !to.trim()) return;
    if (await edit('sibling', 'add', from, to)) {
      from = '';
      to = '';
    }
  }
</script>

<Sections title={t('tags.title')} screen="tags" sections={tagSections} fallback="rules">
  {#snippet children(current: string)}
    {#if current === 'all'}
      <TagBrowser
        onrelations={(r) => {
          parents = r.parents;
          siblings = r.siblings;
        }} />
    {:else}

    <section class="card">
      <div class="card-head">
        <h2>{t('tags.parents.title')}</h2>
        <p class="hint">{t('tags.parents.hint')}</p>
      </div>
      <div class="form">
        <TagInput bind:value={child} multi label={t('tags.parents.childLabel')} placeholder={t('tags.parents.childPlaceholder')} onenter={addParent} />
        <span class="arrow" aria-hidden="true">→</span>
        <TagInput bind:value={parent} label={t('tags.parents.parent')} placeholder={t('tags.parents.parent')} onenter={addParent} />
        <button onclick={addParent}><Icon name="plus" size={18} /> {t('common.add')}</button>
      </div>
      {#each groups as g (g.parent)}
        <div class="group">
          <div class="gname"><span style:color={tagColor(g.parent)}>{g.parent}</span> <span class="muted">· {g.children.length}</span></div>
          <div class="add-row">
            <TagInput bind:value={() => childDraft[g.parent] ?? "", (v) => (childDraft[g.parent] = v)} multi label={t('tags.parents.addChildTo', { tag: g.parent })} placeholder={t('tags.parents.addChildren')} onenter={(text) => addChildren(g.parent, text)} />
            <button onclick={() => addChildren(g.parent)} disabled={!childDraft[g.parent]?.trim()}><Icon name="plus" size={18} /> {t('common.add')}</button>
          </div>
          <div class="chips">
            {#each g.children as c (c)}
              <span class="pill">
                <span style:color={tagColor(c)}>{c}</span>
                <button aria-label={t('tags.parents.remove', { tag: c, parent: g.parent })} onclick={() => edit('parent', 'remove', c, g.parent)}
                  ><Icon name="close" size={14} /></button>
              </span>
            {/each}
          </div>
        </div>
      {:else}
        <p class="empty">{t('tags.parents.empty')}</p>
      {/each}
    </section>

    <section class="card">
      <div class="card-head">
        <h2>{t('tags.siblings.title')}</h2>
        <p class="hint">{t('tags.siblings.hint')}</p>
      </div>
      <div class="form">
        <TagInput bind:value={from} label={t('tags.siblings.tag')} placeholder={t('tags.siblings.tag')} onenter={addSibling} />
        <span class="arrow" aria-hidden="true">→</span>
        <TagInput bind:value={to} label={t('tags.siblings.ideal')} placeholder={t('tags.siblings.ideal')} onenter={addSibling} />
        <button onclick={addSibling}><Icon name="plus" size={18} /> {t('common.add')}</button>
      </div>
      {#if siblings.length}
        <ul class="list">
          {#each siblings as s (s.from)}
            <li>
              <span><span style:color={tagColor(s.from)}>{s.from}</span> <span class="arrow">→</span> <span style:color={tagColor(s.to)}>{s.to}</span></span>
              <button class="icon" aria-label={t('tags.siblings.remove', { tag: s.from })} onclick={() => edit('sibling', 'remove', s.from)}
                ><Icon name="close" size={16} /></button>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="empty">{t('tags.siblings.empty')}</p>
      {/if}
    </section>

    <BlacklistPanel />
    {/if}
  {/snippet}
</Sections>

<style>
  /* Phone: the two tags on one row, the button below at full width. */
  .form {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
    align-items: center;
    gap: 8px;
  }
  .form button {
    grid-column: 1 / -1;
  }
  @media (min-width: 640px) {
    .form {
      grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) auto;
    }
    .form button {
      grid-column: auto;
    }
  }
  .arrow {
    color: var(--mauve);
  }
  .group {
    display: grid;
    gap: 10px;
    padding: 12px;
    border-radius: 10px;
    background: var(--crust);
    border: 1px solid var(--surface0);
  }
  .gname {
    font-weight: 600;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }
  .list li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 4px 0 4px 2px;
    border-top: 1px solid var(--surface0);
    overflow-wrap: anywhere;
  }
  .list li:first-child {
    border-top: 0;
  }
</style>
