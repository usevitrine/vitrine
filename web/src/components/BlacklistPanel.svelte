<script lang="ts">
  // The download blacklist: tags whose posts are never downloaded, and a
  // clean-up for library files that already have them (an undoable delete).
  import { onMount } from 'svelte';
  import { reveal, pop, moved } from '../lib/motion';
  import { api, type BlacklistMatches } from '../lib/api';
  import { blacklist } from '../lib/blacklist.svelte';
  import { results } from '../lib/results.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import { router } from '../lib/router.svelte';
  import TagInput from './TagInput.svelte';
  import Icon from './Icon.svelte';
  import Rich from './Rich.svelte';
  import { bytes } from '../lib/live.svelte';
  import { t, num } from '../lib/i18n.svelte';

  /** Tags offered with a tap (the first steps: common ones), those not in the list yet. */
  let { suggest = [] }: { suggest?: string[] } = $props();

  // (the list is shared: lib/blacklist - the details add and take off tags too)
  const tags = $derived(blacklist.tags ?? []);
  let draft = $state('');
  let matches = $state<BlacklistMatches | null>(null);
  let busy = $state(false);

  async function loadMatches() {
    try {
      matches = await api.blacklistMatches();
    } catch {
      matches = null;
    }
  }

  onMount(async () => {
    await blacklist.load(true); // (fresh: another device may have changed it)
    loadMatches();
  });

  async function save(next: string[]) {
    try {
      await blacklist.set(next);
      loadMatches();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  // Several tags at once are fine (pasted lists: spaces, commas or new lines).
  function add(text: string) {
    const added = text.split(/[\s,]+/).filter(Boolean);
    draft = '';
    if (added.length) save([...tags, ...added]);
  }

  function remove(t: string) {
    save(tags.filter((x) => x !== t));
  }

  // "Never downloaded. {star} matches anything ({example})." around the two codes.
  const hint = $derived(t('blacklist.hint').split(/\{star\}|\{example\}/));

  /** The library search for all of them: one OR group. */
  let query = $derived(tags.length === 1 ? tags[0] : `( ${tags.join(' ~ ')} )`);

  function show() {
    router.openSearch(query);
  }

  async function purge() {
    if (!matches) return;
    const n = matches.total;
    const ok = await confirmAction({
      title: t('blacklist.deleteTitle', { n }),
      body: t('blacklist.deleteBody'),
      action: t('common.deleteN', { n }),
      undoable: true,
    });
    if (!ok) return;
    busy = true;
    try {
      const res = await api.deleteBlacklisted();
      toasts.show(t('common.deletedFiles', { n: res.deleted }), { cat: 'deleted',
        kind: 'removed',
        action: {
          label: t('common.undo'),
          run: async () => {
            await api.restore(res.ids);
            toasts.show(t('trash.restored', { n: res.ids.length }), { cat: 'deleted' });
            loadMatches();
            results.changedElsewhere();
          },
        },
        ms: 12000,
      });
      loadMatches();
      results.changedElsewhere(); // (the library behind: they go from it now, not at the next reload)
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      busy = false;
    }
  }

</script>

<section class="card blacklist">
  <div class="card-head">
    <h2>{t('blacklist.title')}</h2>
    <p class="hint">{hint[0]}<code>*</code>{hint[1]}<code>ai_*</code>{hint[2]}</p>
  </div>

  <div class="add-row">
    <TagInput bind:value={draft} multi label={t('blacklist.addLabel')} placeholder={t('blacklist.addPlaceholder')} onenter={add} />
    <button onclick={() => add(draft)} disabled={!draft.trim()}><Icon name="plus" size={18} /> {t('common.add')}</button>
  </div>
  {#if suggest.some((s) => !tags.includes(s))}
    <div class="suggest" transition:reveal>
      <span class="hint">{t('blacklist.common')}</span>
      {#each suggest.filter((s) => !tags.includes(s)) as s (s)}
        <button class="pill add" onclick={() => add(s)} animate:moved transition:pop><Icon name="plus" size={13} /> {s}</button>
      {/each}
    </div>
  {/if}
  {#if tags.length}
    <div class="tags" transition:reveal>
      {#each tags as tag (tag)}
        <span class="pill" animate:moved transition:pop>
          {tag}
          <button aria-label={t('blacklist.remove', { tag })} onclick={() => remove(tag)}><Icon name="close" size={14} /></button>
        </span>
      {/each}
    </div>
  {:else}
    <p class="empty" transition:reveal>{t('blacklist.empty')}</p>
  {/if}

  {#if matches && tags.length}
    <div class="matches" class:clean={matches.total === 0} transition:reveal>
      {#if matches.total === 0}
        <p class="small" transition:reveal><Icon name="check" size={16} /> {t('blacklist.clean')}</p>
      {:else}
        <div class="summary" transition:reveal>
          <p>
            <Rich text={t('blacklist.matches', { n: matches.total })} />
            <span class="muted">· {bytes(matches.size)}</span>
          </p>
          <p class="hint">
            {matches.tags
              .filter((c) => c.files > 0)
              .map((c) => `${c.tag} ${num(c.files)}`)
              .join(' · ')}
          </p>
        </div>
        <div class="actions">
          <button class="quiet" onclick={show}>{t('blacklist.show')}</button>
          <button class="quiet danger" disabled={busy} onclick={purge}>{t('blacklist.delete')}</button>
        </div>
      {/if}
    </div>
  {/if}
</section>

<style>
  .suggest {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .suggest .hint {
    margin-right: 2px;
  }
  .pill.add {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 0;
    padding: 4px 10px;
    border-radius: 999px;
    background: transparent;
    border: 1px dashed var(--surface2);
    color: var(--subtext);
    font-size: var(--font-small);
  }
  p {
    margin: 0;
  }
  code {
    font-size: 0.95em;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--surface0);
    color: var(--text);
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .pill {
    color: var(--red);
  }
  .matches {
    display: grid;
    gap: 12px;
    padding: 14px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--red) 7%, var(--crust));
    border: 1px solid color-mix(in srgb, var(--red) 22%, transparent);
  }
  .matches.clean {
    background: color-mix(in srgb, var(--green) 6%, var(--crust));
    border-color: color-mix(in srgb, var(--green) 20%, transparent);
    color: var(--green);
  }
  .matches.clean p {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .summary {
    display: grid;
    gap: 4px;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    justify-content: flex-end;
  }
</style>
