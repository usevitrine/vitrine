<script lang="ts">
  // Settings > Library > Comics: the search the Comics page opens with, in the Comics page's own search bar (its
  // filters - read state, pages, category, language - and the comics' tags).
  import { comicsScope, provideScope } from '../lib/scope.svelte';
  import { settings } from '../lib/settings.svelte';
  import { t } from '../lib/i18n.svelte';
  import SearchBar from './SearchBar.svelte';

  provideScope(comicsScope);
</script>

<SearchBar
  plain
  label={t('settings.comicsSearch')}
  placeholder={t('settings.defaultSearchPlaceholder')}
  query={settings.comicsSearch}
  onsearch={(q) => (settings.comicsSearch = q)} />
