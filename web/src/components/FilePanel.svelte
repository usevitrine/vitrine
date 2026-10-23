<script lang="ts">
  // A file's details in the viewer: a facts card, where it's from (post page,
  // the searches that found it), its sources (the first one; the rest folded) and its tags (tap to search, × to
  // remove, add new ones). The viewer gives it its header and close button.
  import { api, type FileInfo, type FileLink, type FileSource, type FileTag } from '../lib/api';
  import { reveal, pop } from '../lib/motion';
  import { router } from '../lib/router.svelte';
  import { siteLabel } from '../lib/sites.svelte';
  import { formatDuration, formatSize, results } from '../lib/results.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { settings } from '../lib/settings.svelte';
  import { copyText } from '../lib/clipboard';
  import TagsSection from './TagsSection.svelte';
  import '../lib/details.css';
  import Icon from './Icon.svelte';
  import SiteIcon from './SiteIcon.svelte';
  import { t, num } from '../lib/i18n.svelte';
  import { when, shortDate, fullDate, host } from '../lib/when';
  import Stack from './Stack.svelte';

  let { file, onsearch }: { file: FileInfo; onsearch: (q: string) => void } = $props();

  // The "I came" count (Settings > Viewing): the latest press taken off, or all of them.
  async function removeCame(all: boolean) {
    const id = file.id;
    try {
      const { count } = await api.removeClimaxes(id, all);
      results.patch(id, { came: count });
      toasts.show(all ? t('details.came.reset') : count ? t('details.came.removedOne', { n: count }) : t('details.came.removed'), { cat: 'came', kind: 'removed' });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  let tags = $state<FileTag[]>([]);
  let links = $state<FileLink[]>([]);
  let foundBy = $state<FileSource[]>([]);
  let loadedFor = 0;

  async function load(id: number) {
    try {
      const res = await api.fileTags(id);
      if (id !== file.id) return; // swiped on meanwhile
      tags = res.tags;
      links = res.urls;
      foundBy = res.sources;
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  $effect(() => {
    if (file.id !== loadedFor) {
      loadedFor = file.id;
      tags = [];
      links = [];
      sourcesOpen = false;
      load(file.id);
    }
  });

  // Reads the file's posts from their sites again now.
  async function refreshTags() {
    const id = file.id;
    try {
      const { found, queued } = await api.refreshFile(id);
      // (a paused site - it kept failing to answer: queued for when it does)
      if (queued && !found) toasts.show(t('details.refreshQueued'), { cat: 'refreshed' });
      else toasts.show(found ? t('details.refreshed') : t('details.gone'), { cat: 'refreshed', kind: found ? 'done' : 'info' });
      if (id === file.id) load(id);
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  const copy = copyText;

  let posts = $derived(links.filter((l) => l.kind === 'post'));
  // Sources: grouped by site (the same one's links together), the first shown, the rest a tap away
  // (2026-10-23, the user: a file with "a ton of sources" - 32 - filled the panel).
  let sources = $derived.by(() => {
    const list = links.filter((l) => l.kind === 'source');
    const order = [...new Set(list.map((l) => host(l.url)))];
    return [...list].sort((a, b) => order.indexOf(host(a.url)) - order.indexOf(host(b.url)));
  });
  let sourcesOpen = $state(false);
  /** A link's path, short ("/video/123/some-title"), for under its site. */
  const path = (u: string) => {
    try {
      const p = new URL(u);
      return decodeURIComponent(p.pathname + p.search).replace(/\/$/, '') || '/';
    } catch {
      return '';
    }
  };
  // Only what downloaded the file (not every search that met it later).
  let downloadedBy = $derived(foundBy.filter((f) => f.downloaded));

  // ---- the facts card
  const kind = $derived(
    file.mime.startsWith('video/') ? 'video' : file.mime === 'image/gif' && file.duration_ms > 0 ? 'animation' : 'image',
  );
</script>

<div class="details">
  <dl class="facts">
    <div>
      <dt>{t('filter.type.label')}</dt>
      <dd>{t('details.typeKind', { type: file.type.toUpperCase(), kind: t(`details.kind.${kind}`) })}</dd>
    </div>
    <div>
      <dt>{t('filter.size.label')}</dt>
      <dd>{formatSize(file.size)}</dd>
    </div>
    <div>
      <dt>{t('details.dimensions')}</dt>
      <dd>{num(file.width)} × {num(file.height)}</dd>
    </div>
    {#if file.duration_ms}
      <div>
        <dt>{t('filter.duration.label')}</dt>
        <dd>{formatDuration(file.duration_ms)}{#if file.mime.startsWith('video/')}<span class="muted">{' · '}{file.has_audio ? t('details.sound') : t('details.silent')}</span>{/if}</dd>
      </div>
    {/if}
    {#if file.posted_at}
      <div>
        <dt>{t('filter.uploaded.label')}</dt>
        <dd title={fullDate(file.posted_at)}>{when(file.posted_at)}</dd>
      </div>
    {/if}
    <div>
      <dt>{t('filter.imported.label')}</dt>
      <dd title={fullDate(file.imported_at)}>{when(file.imported_at)}</dd>
    </div>
    <!-- Vitrine's own number for the file (also in the address as f=): handy to point at one file. -->
    <div>
      <dt>{t('details.fileId')}</dt>
      <dd class="id">
        <span>{file.id}</span>
        <button class="icon copy-id" aria-label={t('details.copyId')} title={t('tagMenu.copy')} onclick={() => copy(String(file.id), t('details.what.id'))}><Icon name="copy" size={15} /></button>
      </dd>
    </div>
    {#if settings.came && file.came}
      <div class="came-row wide">
        <dt>{t('rankings.title')}</dt>
        <dd class="came-dd">
          <span>{t('details.came.times', { n: file.came })}</span>
          <span class="came-acts">
            <button class="icon" aria-label={t('details.came.removeOne')} title={t('details.came.removeOne')} onclick={() => removeCame(false)}><Icon name="minus" size={16} /></button>
            <button class="icon" aria-label={t('details.came.resetButton')} title={t('details.came.resetButton')} onclick={() => removeCame(true)}><Icon name="undo" size={16} /></button>
          </span>
        </dd>
      </div>
    {/if}
  </dl>

  {#if posts.length || downloadedBy.length}
    <section aria-label={t('details.from')} transition:reveal>
      <div class="head">
        <h3>{t('details.from')}</h3>
      </div>
      <ul class="rows">
        {#each posts as l (l.url)}
          <li class="row">
            <SiteIcon host={host(l.url)} />
            <span class="what"><span class="q">{t('details.postPage')}</span><span class="sub">{siteLabel(l.site)}</span></span>
            <a class="icon act" href={l.url} target="_blank" rel="noreferrer noopener" aria-label={t('details.openPost')} title={t('details.open')}><Icon name="external" size={17} /></a>
            <button class="icon act" aria-label={t('details.copyPost')} title={t('details.copyLink')} onclick={() => copy(l.url, t('details.what.post'))}><Icon name="copy" size={17} /></button>
          </li>
        {/each}
        {#each downloadedBy as f (f.site + f.query)}
          <li class="row">
            <span class="ic"><Icon name={f.subscription_id ? 'bell' : 'download'} size={17} /></span>
            <span class="what">
              <span class="q">{f.query || t('push.everything')}</span>
              <span class="sub">{t(f.subscription_id ? 'details.subOn' : 'details.downloadOn', { site: siteLabel(f.site), date: shortDate(f.first_at) })}</span>
            </span>
            {#if f.subscription_id}
              <button
                class="icon act"
                aria-label={t('details.openSubNamed', { name: f.query || t('push.everything') })}
                title={t('details.openSub')}
                onclick={() => {
                  router.focus = 'sub:' + f.subscription_id;
                  router.go('downloads');
                }} transition:pop><Icon name="right" size={17} /></button>
            {/if}
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if sources.length}
    <section aria-label={t('details.sources')}>
      <div class="head">
        <h3>{t('details.sources')}</h3>
      </div>
      <!-- A stack like the Downloads page's: the first in front, the rest peeking out behind it (they fan out). -->
      <Stack items={sources} key={(l) => l.url} bind:open={sourcesOpen} label={t('details.sourcesWhat')}>
        {#snippet card(l: FileLink)}
          <div class="row">
            <SiteIcon host={host(l.url)} />
            <span class="what"><span class="q">{host(l.url)}</span><span class="sub path" title={l.url}>{path(l.url)}</span></span>
            <a class="icon act" href={l.url} target="_blank" rel="noreferrer noopener" aria-label={t('details.openSource')} title={t('details.open')}><Icon name="external" size={17} /></a>
            <button class="icon act" aria-label={t('details.copySource')} title={t('details.copyLink')} onclick={() => copy(l.url, t('details.what.source'))}><Icon name="copy" size={17} /></button>
          </div>
        {/snippet}
      </Stack>
    </section>
  {/if}

  <TagsSection
    item={file.id}
    {tags}
    {onsearch}
    save={(add, remove) => api.editTags([file.id], add, remove).then(() => {})}
    reload={() => load(file.id)}
    refresh={posts.length ? refreshTags : undefined} />
</div>

<style>
  /* (The facts card, rows and headings: lib/details.css; the tags: TagsSection.) */
  /* (a long link's path: one line, ending in …) */
  .path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .came-dd {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 6px 10px;
  }
  .came-acts {
    display: flex;
    gap: 2px;
    margin: -6px -6px -6px 0; /* (the buttons' own room, not the row's) */
  }
</style>
