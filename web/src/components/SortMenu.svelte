<script lang="ts" module>
  // The result order. In the top bar it's an icon button (like hydrus-web's
  // sort button); in Settings a normal dropdown. Both use Vitrine's own
  // dropdown, not the native one. The sort is kept apart from the search -
  // in the URL's ?sort= - never as a chip.
  import { sortFields, comicSortFields, sortInfo, midSentence } from '../lib/system';
  import { t } from '../lib/i18n.svelte';

  /** Every order the library offers, both ways ("File size - largest first"), in the current language. */
  export function sortOptions(comics = false) {
    return [
      { value: 'imported', label: t(comics ? 'sort.newestAdded' : 'sort.newestImported') },
      { value: 'imported_asc', label: t(comics ? 'sort.oldestAdded' : 'sort.oldestImported') },
      { value: 'uploaded', label: t('sort.newestUploaded') },
      { value: 'uploaded_asc', label: t('sort.oldestUploaded') },
      ...(comics ? comicSortFields : sortFields)
        .filter((f) => f !== 'imported' && f !== 'uploaded')
        .flatMap((f) => {
          const i = sortInfo(f, comics);
          if (!i.desc) return [{ value: f, label: i.label }];
          return [
            { value: f, label: t('sort.option', { field: i.label, way: midSentence(i.desc) }) },
            { value: f + '_asc', label: t('sort.option', { field: i.label, way: midSentence(i.asc) }) },
          ];
        }),
    ];
  }
</script>

<script lang="ts">
  import Dropdown from './Dropdown.svelte';
  import Icon from './Icon.svelte';

  let {
    value,
    onchange,
    label = t('sort.order'),
    compact = false,
    changed = false, // marks the button when the order isn't the default
  }: { value: string; onchange: (v: string) => void; label?: string; compact?: boolean; changed?: boolean } =
    $props();

  // A sort typed into the search (e.g. sort:size) that isn't one of the options.
  let options = $derived.by(() => {
    const all = sortOptions();
    return all.some((o) => o.value === value)
      ? all
      : [...all, { value, label: t(value.endsWith('_asc') ? 'sort.typedAsc' : 'sort.typed', { sort: value.replace('_asc', '') }) }];
  });
</script>

{#snippet icon()}
  <Icon name="sort" size={22} />
  {#if changed}<span class="dot"></span>{/if}
{/snippet}

{#if compact}
  <Dropdown {value} {options} {onchange} {label} heading={t('sort.by')} align="end" trigger={icon} triggerClass="icon sort-button" />
{:else}
  <Dropdown {value} {options} {onchange} {label} />
{/if}

<style>
  :global(.sort-button) {
    position: relative;
    color: var(--text) !important;
  }
  .dot {
    position: absolute;
    top: 5px;
    right: 5px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--mauve);
  }
</style>
