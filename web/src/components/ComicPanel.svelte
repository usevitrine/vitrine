<script lang="ts">
  // A comic's details (the Comics page's details panel): laid out like a
  // library file's (FilePanel) - its cover and titles with Read, a facts card,
  // where it's from (its gallery page, Download again) and its tags (the same
  // Tags section as a file's: a tap opens a tag's menu, pick several, edit,
  // read them again from the site). The page gives it the panel's frame.
  import { api, type Comic, type FileTag } from '../lib/api';
  import { siteLabel } from '../lib/sites.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import { copyText } from '../lib/clipboard';
  import { isRead, isReading, progress, cropStyle } from '../lib/comics';
  import CropDialog from './CropDialog.svelte';
  import RenameDialog from './RenameDialog.svelte';
  import { splitTag } from '../lib/namespaces.svelte';
  import TagsSection from './TagsSection.svelte';
  import Icon from './Icon.svelte';
  import SiteIcon from './SiteIcon.svelte';
  import '../lib/details.css';
  import { t, num } from '../lib/i18n.svelte';
  import { when, fullDate, host } from '../lib/when';

  let {
    comic,
    onsearch,
    onread,
    onchanged,
    busy = $bindable(false),
  }: {
    comic: Comic;
    onsearch: (q: string) => void;
    onread: (fromStart: boolean) => void;
    /** The comic changed (its tags, its pages): the grid shows it again. */
    onchanged: () => void;
    /** A dialog of its own is open (rename, crop): the panel leaves Escape to it. */
    busy?: boolean;
  } = $props();

  // The user's own title and the cover's crop, as changed here (the page shows them once it has the comic again).
  let title = $state<string | null>(null);
  let siteTitle = $state<string | undefined>(undefined);
  let crop = $state<number[] | undefined>(undefined);
  let renaming = $state(false);
  let cropping = $state(false);
  $effect(() => {
    void comic.id;
    title = null;
    siteTitle = comic.site_title;
    crop = comic.crop;
    renaming = false;
  });
  const shownTitle = $derived(title ?? comic.title);
  $effect(() => {
    busy = renaming || cropping;
  });

  async function rename(next: string | null) {
    const value = next === null ? '' : next.trim();
    renaming = false;
    if (next !== null && (!value || value === shownTitle)) return;
    try {
      const before = shownTitle;
      const hadOwn = !!siteTitle;
      await api.updateComic(comic.id, { title: value });
      const original = siteTitle ?? comic.title;
      title = value || original;
      siteTitle = value && value !== original ? original : undefined;
      // (Undo: the title it had - the user's own before, or the site's.)
      toasts.show(value ? t('comics.renamed') : t('comics.titleBack'), { cat: 'comicEdits',
        kind: 'done',
        action: { label: t('common.undo'), run: () => rename(hadOwn ? before : null) },
      });
      onchanged();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  async function saveCrop(next: number[] | null) {
    cropping = false;
    try {
      await api.updateComic(comic.id, { crop: next ?? [] });
      crop = next ?? undefined;
      onchanged();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  let tags = $state<FileTag[]>([]);
  let loadedFor = 0;
  async function load(id: number) {
    try {
      const res = await api.comic(id);
      if (id !== comic.id) return; // another one meanwhile
      tags = res.tags;
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  $effect(() => {
    if (comic.id !== loadedFor) {
      loadedFor = comic.id;
      tags = [];
      load(comic.id);
    }
  });

  // Tags read from the site again.
  async function refreshTags() {
    const id = comic.id;
    try {
      const { found } = await api.comicsRefresh([id]);
      toasts.show(found ? t('details.refreshed') : t('details.gone'), { cat: 'refreshed', kind: found ? 'done' : 'info' });
      if (id === comic.id) load(id);
      onchanged();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  // Its pages fetched again from the site (e.g. pages that came in wrong).
  async function downloadAgain() {
    const ok = await confirmAction({
      title: t('comics.againTitle', { title: comic.title }),
      body: t('comics.againBody'),
      action: t('comics.again'),
      thumbs: comic.thumb ? [comic.thumb] : [],
    });
    if (!ok) return;
    try {
      await api.comicsRedownload([comic.id]);
      toasts.show(t('comics.againStarted'), { cat: 'comicEdits', kind: 'done' });
      onchanged();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  const languages = $derived(
    tags
      .map((x) => splitTag(x.tag))
      .filter((x) => x.ns === 'language' && x.name !== 'translated' && x.name !== 'rewrite')
      .map((x) => x.name.replace(/_/g, ' ')),
  );
  const readText = $derived(
    isRead(comic) ? t('comics.finished') : isReading(comic) ? t('comics.pageOf', { page: comic.read_page + 1, n: comic.pages }) : t('comics.notStarted'),
  );
</script>

<div class="details">
  <!-- Its cover and titles, and Read. -->
  <div class="top">
    {#if comic.thumb}
      <div class="cover-col">
        <span class="cover" style:aspect-ratio={crop && comic.cover_w && comic.cover_h ? `${comic.cover_w * crop[2]} / ${comic.cover_h * crop[3]}` : undefined}
          ><img src={comic.thumb} alt="" style={comic.cover_w && comic.cover_h ? cropStyle(crop) : undefined} /></span>
        {#if comic.cover_url}
          <button class="quiet small-btn" onclick={() => (cropping = true)}><Icon name="crop" size={14} /> {t('comics.crop.button')}</button>
        {/if}
      </div>
    {/if}
    <div class="titles">
      <div class="title-row">
        <h3 class="title">{shownTitle}</h3>
        <button class="icon rename-btn" aria-label={t('comics.rename')} title={t('comics.rename')} onclick={() => (renaming = true)}
          ><Icon name="pencil" size={15} /></button>
      </div>
      {#if comic.title_alt}<p class="alt">{comic.title_alt}</p>{/if}
      <div class="reads">
        <button onclick={() => onread(false)}><Icon name="book" size={16} /> {isReading(comic) ? t('comics.continueAt', { page: comic.read_page + 1 }) : t('comics.read')}</button>
        {#if comic.read_page > 0}<button class="quiet" onclick={() => onread(true)} title={t('comics.fromStart')} aria-label={t('comics.fromStart')}><Icon name="undo" size={16} /></button>{/if}
      </div>
    </div>
  </div>

  <dl class="facts">
    <div>
      <dt>{t('filter.pages.label')}</dt>
      <dd>{comic.have < comic.pages ? t('comics.havePages', { have: comic.have, n: comic.pages }) : num(comic.pages)}</dd>
    </div>
    <div>
      <dt>{t('comics.progress')}</dt>
      <dd>
        {readText}
        {#if progress(comic) > 0 && !isRead(comic)}<span class="bar" style:--p={progress(comic)}></span>{/if}
      </dd>
    </div>
    {#if comic.category}
      <div>
        <dt>{t('filter.category.label')}</dt>
        <dd>{comic.category}</dd>
      </div>
    {/if}
    {#if languages.length}
      <div>
        <dt>{t('filter.language.label')}</dt>
        <dd class="cap">{languages.join(', ')}</dd>
      </div>
    {/if}
    {#if !comic.site}
      <div>
        <dt>{t('details.from')}</dt>
        <dd>{t('comics.imported')}</dd>
      </div>
    {/if}
    {#if comic.posted_at}
      <div>
        <dt>{t('filter.uploaded.label')}</dt>
        <dd title={fullDate(comic.posted_at)}>{when(comic.posted_at)}</dd>
      </div>
    {/if}
    <div>
      <dt>{t('filter.added.label')}</dt>
      <dd title={fullDate(comic.created_at)}>{when(comic.created_at)}</dd>
    </div>
    <div>
      <dt>{t('filter.updated.label')}</dt>
      <dd title={fullDate(comic.updated_at)}>{when(comic.updated_at)}</dd>
    </div>
    <div>
      <dt>{t('comics.id')}</dt>
      <dd class="id">
        <span>{comic.id}</span>
        <button class="icon copy-id" aria-label={t('comics.copyId')} title={t('tagMenu.copy')} onclick={() => copyText(String(comic.id), t('comics.what.id'))}><Icon name="copy" size={15} /></button>
      </dd>
    </div>
  </dl>

  {#if comic.url}
    <section aria-label={t('details.from')}>
      <div class="head"><h3>{t('details.from')}</h3></div>
      <ul class="rows">
        <li class="row">
          <SiteIcon host={host(comic.url)} />
          <span class="what"><span class="q">{t('comics.galleryPage')}</span><span class="sub">{comic.site ? siteLabel(comic.site) : host(comic.url)}</span></span>
          <a class="icon act" href={comic.url} target="_blank" rel="noreferrer noopener" aria-label={t('comics.onSite')} title={t('details.open')}><Icon name="external" size={17} /></a>
          <button class="icon act" aria-label={t('comics.copyLink')} title={t('details.copyLink')} onclick={() => copyText(comic.url!, t('comics.what.link'))}><Icon name="copy" size={17} /></button>
        </li>
        {#if comic.site}
          <li>
            <button class="row" onclick={downloadAgain}>
              <span class="ic"><Icon name="retry" size={17} /></span>
              <span class="what"><span class="q">{t('comics.again')}</span><span class="sub">{t('comics.againHint')}</span></span>
            </button>
          </li>
        {/if}
      </ul>
    </section>
  {/if}

  {#if renaming}
    <RenameDialog title={shownTitle} original={siteTitle} onsave={(v) => rename(v || null)} onclose={() => (renaming = false)} />
  {/if}

  {#if cropping && comic.cover_url}
    <CropDialog src={comic.cover_url} {crop} onsave={saveCrop} onclose={() => (cropping = false)} />
  {/if}

  <TagsSection
    item={comic.id}
    {tags}
    {onsearch}
    save={async (add, remove) => {
      await api.comicsTags([comic.id], add, remove);
      onchanged();
    }}
    reload={() => load(comic.id)}
    refresh={comic.site && comic.url ? refreshTags : undefined} />
</div>

<style>
  .top {
    display: flex;
    gap: 14px;
    align-items: flex-start;
  }
  .cover-col {
    flex: none;
    width: 92px;
    display: grid;
    gap: 6px;
  }
  /* The cover as the grid shows it (its crop, in the crop's shape). */
  .cover {
    position: relative;
    display: block;
    overflow: hidden;
    border-radius: 10px;
    background: var(--surface0);
    box-shadow: 0 6px 18px rgb(0 0 0 / 0.4);
  }
  .cover img {
    display: block;
    width: 100%;
  }
  .small-btn {
    min-height: 26px;
    padding: 0 6px;
    gap: 4px;
    font-size: 12px;
  }
  .title-row {
    display: flex;
    align-items: flex-start;
    gap: 2px;
  }
  .title-row .title {
    flex: 1;
    min-width: 0;
  }
  .rename-btn {
    flex: none;
    width: 28px;
    min-width: 0;
    height: 28px;
    min-height: 0;
    margin: -3px -4px 0 0;
    color: var(--subtext);
  }
  .titles {
    min-width: 0;
    display: grid;
    gap: 6px;
  }
  /* (an h3 for its place in the outline; it reads as the panel's title) */
  .details .title {
    margin: 0;
    font-size: 16px;
    font-weight: 700;
    letter-spacing: 0;
    text-transform: none;
    color: var(--text);
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  .alt {
    margin: 0;
    font-size: var(--font-small);
    color: var(--subtext);
    overflow-wrap: anywhere;
  }
  .reads {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 4px;
  }
  .cap {
    text-transform: capitalize;
  }
  /* How far it's read: a thin line under the words. */
  .bar {
    display: block;
    height: 3px;
    margin-top: 6px;
    border-radius: 2px;
    background: linear-gradient(to right, var(--mauve) calc(var(--p) * 100%), var(--surface1) 0);
  }
</style>
