<script lang="ts" module>
  export type PickUse = 'all' | 'any' | 'add' | 'download' | 'download-any' | 'blacklist';
</script>

<script lang="ts">
  // The picked tags (the details): the shared round button and card (PickMenu), with what tags can do - Search (all of
  // them, any of them, added to the search), Download (all, any) and the blacklist, each row saying what it does.
  import PickMenu from './PickMenu.svelte';
  import Icon from './Icon.svelte';
  import { nudge } from '../lib/motion';
  import { splitTag } from '../lib/namespaces.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    chosen,
    blacklisted,
    onuse,
    onunpick,
    onclear,
    within,
  }: {
    chosen: string[];
    blacklisted: (tag: string) => boolean;
    onuse: (how: PickUse) => void;
    onunpick: (tag: string) => void;
    onclear: () => void;
    within?: () => Element | null | undefined;
  } = $props();

  const names = (sep: string) => chosen.map((x) => splitTag(x).name).join(sep);
  const many = $derived(chosen.length > 1);
  const allOut = $derived(chosen.length > 0 && chosen.every(blacklisted));
  const chips = $derived(chosen.map((tag) => ({ key: tag, label: splitTag(tag).name })));
</script>

<PickMenu
  title={t('details.tagsPicked', { n: chosen.length })}
  fabLabel={t('details.useThem', { n: chosen.length })}
  {chips}
  chipsLabel={t('details.picked')}
  unpickLabel={(name) => t('details.unpick', { tag: chosen.find((x) => splitTag(x).name === name) ?? name })}
  onunpick={(k) => onunpick(String(k))}
  {onclear}
  {within}>
  {#snippet actions(use)}
    <h4>{t('tagMenu.search')}</h4>
    <div class="group">
      <button class="act" onclick={() => use(() => onuse('all'))}>
        <Icon name="search" size={20} />
        <span class="txt"><span class="lbl">{many ? t('details.filesAll') : t('details.filesWith')}</span><span class="sub">{names(' ')}</span></span>
      </button>
      {#if many}
        <button class="act" onclick={() => use(() => onuse('any'))} transition:nudge>
          <Icon name="or" size={20} />
          <span class="txt"><span class="lbl">{t('details.filesAny')}</span><span class="sub">{names(' OR ')}</span></span>
        </button>
      {/if}
      <button class="act" onclick={() => use(() => onuse('add'))}>
        <Icon name="plus" size={20} />
        <span class="txt"><span class="lbl">{t('details.addToSearch')}</span></span>
      </button>
    </div>

    <h4>{t('backups.download')}</h4>
    <div class="group">
      <button class="act" onclick={() => use(() => onuse('download'))}>
        <Icon name="download" size={20} />
        <span class="txt"><span class="lbl">{many ? t('details.dlAll') : t('details.dlIt')}</span><span class="sub">{t('details.dlHint', { n: chosen.length })}</span></span>
      </button>
      {#if many}
        <button class="act" onclick={() => use(() => onuse('download-any'))} transition:nudge>
          <Icon name="or" size={20} />
          <span class="txt"><span class="lbl">{t('details.dlAny')}</span><span class="sub">{t('details.dlAnyHint')}</span></span>
        </button>
      {/if}
      <button class="act danger" disabled={allOut} onclick={() => use(() => onuse('blacklist'))}>
        <Icon name="eyeoff" size={20} />
        <span class="txt"
          ><span class="lbl">{allOut ? t('details.allBlacklisted') : many ? t('details.blacklistThem') : t('details.blacklistIt')}</span><span class="sub"
            >{t('details.blHint', { n: chosen.length })}</span
          ></span>
      </button>
    </div>
  {/snippet}
</PickMenu>
