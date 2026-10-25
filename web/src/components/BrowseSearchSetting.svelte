<script lang="ts">
  // Settings > Library > Comics: the search Comics > Browse opens with, in Browse's own search bar - its filters
  // (category, language, pages, stars...) and the site's own tags while typing (the user: "the filter menu ... should be
  // the same one thats on the page"). No site or order: the setting is for every site.
  import { onMount } from 'svelte';
  import { api } from '../lib/api';
  import { browseScope, provideScope } from '../lib/scope.svelte';
  import { namespaces } from '../lib/namespaces.svelte';
  import { settings } from '../lib/settings.svelte';
  import { t } from '../lib/i18n.svelte';
  import SearchBar from './SearchBar.svelte';

  provideScope(browseScope);
  let site = $state('');
  onMount(async () => {
    try {
      site = (await api.browseSites()).sites[0] ?? '';
    } catch {
      /* the comics' own tags then */
    }
    namespaces.loadComics();
  });

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

<SearchBar
  plain
  label={t('settings.browseSearch')}
  placeholder={t('settings.browseSearchPlaceholder')}
  query={settings.browseSearch}
  {suggest}
  onsearch={(q) => (settings.browseSearch = q)} />
