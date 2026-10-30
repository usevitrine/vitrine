<script lang="ts">
  // A bell on a tag a subscription follows (lib/subTags): filled when one is the tag alone, an outline when the tag
  // is one of several in its search, dimmed when every one is paused. Nothing for other tags.
  import Icon from './Icon.svelte';
  import { subTags } from '../lib/subTags.svelte';
  import { siteLabel } from '../lib/sites.svelte';
  import { t } from '../lib/i18n.svelte';

  let { tag, size = 16 }: { tag: string; size?: number } = $props();

  const s = $derived(subTags.of(tag));
  const title = $derived(
    s ? `${t(s.own ? 'subMark.own' : 'subMark.part')} · ${[...new Set(s.subs.map((x) => siteLabel(x.site)))].join(', ')}${s.paused ? ` (${t('subMark.paused')})` : ''}` : '',
  );
</script>

{#if s}
  <!-- (the bell 4 px smaller than the disc: the same odd/even size, so it sits exactly in the middle - a 9 px bell in
       a 14 px disc was half a pixel off, the user: "make sure ... the icon inside is centered") -->
  <span class="sub-mark" class:own={s.own} class:paused={s.paused} style:--s="{size}px" role="img" aria-label={title} {title}
    ><Icon name="bell" size={size - 4} /></span>
{/if}

<style>
  .sub-mark {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: var(--s);
    height: var(--s);
    border-radius: 50%;
    box-sizing: border-box;
    border: 1px solid var(--mauve);
    color: var(--mauve);
    vertical-align: middle;
  }
  .sub-mark.own {
    background: var(--mauve);
    color: var(--base);
  }
  .sub-mark.paused {
    opacity: 0.45;
  }
  .sub-mark :global(svg) {
    display: block; /* (no baseline gap under it) */
    stroke-width: 2.4;
  }
</style>
