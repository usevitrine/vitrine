<script lang="ts">
  // Settings > Library > Browse: the search and order Comics > Browse opens with. The search in Browse's own search bar - its filters
  // (category, language, pages, stars...) and the site's own tags while typing (the user: "the filter menu ... should be
  // the same one thats on the page"; "so should the sort menu"): its orders too - newest, or one of the sites' own lists
  // (picking one clears the search, a search goes back to newest: as on the page). No site: the setting is for every
  // site (an order a site hasn't: its newest there).
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import { browseScope, provideScope } from '../lib/scope.svelte';
  import { namespaces } from '../lib/namespaces.svelte';
  import { settings } from '../lib/settings.svelte';
  import { t } from '../lib/i18n.svelte';
  import SearchBar from './SearchBar.svelte';
  import ActionMenu from './ActionMenu.svelte';

  provideScope(browseScope);
  let site = $state('');
  let sorts = $state<string[]>([]);
  onMount(async () => {
    try {
      const res = await api.browseSites();
      site = res.sites[0] ?? '';
      sorts = [...new Set(res.sites.flatMap((id) => res.sorts?.[id] ?? []))];
    } catch {
      /* the comics' own tags then */
    }
    namespaces.loadComics();
  });

  // The order as a field too (newest, or one of the sites' own lists) - the same setting as the search bar's menu.
  function pickSort(s: string) {
    settings.browseSort = s;
    if (s !== 'newest') settings.browseSearch = ''; // (a site's list has no search)
  }
  const sortLabel = (s: string) => t(`browse.sort.${s}`); // i18n-keys: browse.sort.*

  // The site's tags while typing (as the page suggests them), else the comics' own.
  const suggest = $derived(
    site
      ? async (word: string, signal: AbortSignal) => {
          const res = await api.browseTags(site, word, signal);
          namespaces.addComics(res.suggestions.map((s) => (s.tag.includes(':') ? s.tag.slice(0, s.tag.indexOf(':')) : '')));
          return res;
        }
      : undefined,
  );
</script>

<!-- A div, not a label: the search bar has buttons and menus of its own. -->
<div class="field">
  <span class="field-label">{t('settings.defaultSearch')}</span>
  <SearchBar
    plain
    label={t('settings.browseSearch')}
    placeholder={t('settings.browseSearchPlaceholder')}
    query={settings.browseSearch}
    {suggest}
    sortable
    sort={settings.browseSort || 'newest'}
    defaultSort="newest"
    {sorts}
    onsort={pickSort}
    onsearch={(q) => {
      settings.browseSearch = q;
      if (q.trim()) settings.browseSort = 'newest'; // (a search is the site's newest first)
    }} />
</div>
<div class="field">
  <span class="field-label">{t('settings.defaultSort')}</span>
  <ActionMenu
    label={t('settings.browseSort')}
    title={t('settings.browseSort')}
    current={sortLabel(settings.browseSort || 'newest')}
    items={['newest', ...sorts].map((s) => ({ label: sortLabel(s), on: s === (settings.browseSort || 'newest'), run: () => pickSort(s) }))} />
</div>
