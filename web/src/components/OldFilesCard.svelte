<script lang="ts">
  // Settings > Library: every file uploaded in a chosen year or before, into the trash or out for good. Goes by the date
  // the site gives; files with no date are never included, and it says how many. The work runs on the server: this
  // shows how far it has come, and what such deletes have left in the trash - space that's only freed by deleting it
  // forever.
  import { onMount } from 'svelte';
  import { api, type OldFiles } from '../lib/api';
  import { confirmAction, confirmChecked } from '../lib/confirm.svelte';
  import { attempt } from '../lib/toast.svelte';
  import { formatSize } from '../lib/results.svelte';
  import { reveal } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import Icon from './Icon.svelte';

  let data = $state<OldFiles | null>(null);
  let year = $state(0);
  const job = $derived(data?.job);

  // Newest first: each year with everything before it - what picking it would delete.
  const options = $derived.by(() => {
    let files = 0;
    let bytes = 0;
    let favorites = 0;
    const out = (data?.years ?? []).map((y) => {
      files += y.files;
      bytes += y.bytes;
      favorites += y.favorites ?? 0;
      return { year: y.year, files, bytes, favorites };
    });
    return out.reverse();
  });
  const picked = $derived(options.find((o) => o.year === year));

  async function load() {
    const r = await attempt(() => api.oldFiles());
    if (r) data = r;
  }
  onMount(load);

  // While the server works: how far it has come, then the new counts.
  $effect(() => {
    if (!job) return;
    const timer = setInterval(async () => {
      const r = await api.oldFilesJob().catch(() => null);
      if (!r) return;
      if (r.job) data = { job: r.job };
      else load();
    }, 1500);
    return () => clearInterval(timer);
  });

  async function start(work: () => Promise<unknown>) {
    await attempt(work);
    await load();
  }

  async function deleteOld() {
    const o = picked;
    if (!o) return;
    const details = [t('oldFiles.confirmNever')];
    if (o.favorites) details.unshift(t('oldFiles.confirmFavorites', { n: o.favorites }));
    if (data?.undated?.items) details.push(t('oldFiles.undated', { n: data.undated.items, size: formatSize(data.undated.bytes) }));
    const { ok, checked } = await confirmChecked({
      title: t('oldFiles.confirmTitle', { n: o.files, year: String(o.year) }),
      body: t('oldFiles.confirmBody', { size: formatSize(o.bytes) }),
      details,
      action: t('oldFiles.confirmAction'),
      typed: t('trash.emptyWord'),
      check: t('oldFiles.confirmForever', { size: formatSize(o.bytes) }),
    });
    if (!ok) return;
    year = 0;
    await start(() => api.deleteOldFiles(o.year, checked));
  }

  async function purge(y: number, files: number, bytes: number) {
    const ok = await confirmAction({
      title: t('oldFiles.purgeTitle', { n: files }),
      body: t('oldFiles.purgeBody', { size: formatSize(bytes) }),
      action: t('trash.purge'),
      typed: t('trash.emptyWord'),
    });
    if (ok) await start(() => api.purgeOldFiles(y));
  }
</script>

<section class="card">
  <div class="card-head">
    <h2>{t('oldFiles.title')}</h2>
    <p class="hint">{t('oldFiles.hint')}</p>
  </div>
  {#if job}
    <div class="job" transition:reveal role="status">
      <span class="field-label">{t(`oldFiles.job.${job.kind}`)}</span>
      <div class="bar"><span style:width="{job.total ? (100 * job.done) / job.total : 0}%"></span></div>
      <div class="row">
        <span class="hint">{t('oldFiles.job.count', { done: job.done.toLocaleString(), total: job.total.toLocaleString() })}</span>
        <button class="quiet" onclick={() => attempt(() => api.stopOldFiles())}>{t('oldFiles.job.stop')}</button>
      </div>
    </div>
  {:else if data}
    {#if options.length}
      <div class="field">
        <span class="field-label">{t('oldFiles.label')}</span>
        <ActionMenu
          label={t('oldFiles.label')}
          title={t('oldFiles.label')}
          current={picked ? t('oldFiles.orEarlier', { year: String(picked.year) }) : t('oldFiles.pick')}
          items={options.map((o) => ({
            label: `${t('oldFiles.orEarlier', { year: String(o.year) })} · ${t('common.files', { n: o.files })} · ${formatSize(o.bytes)}`,
            on: o.year === year,
            run: () => (year = o.year),
          }))} />
      </div>
    {:else}
      <p class="hint">{t('oldFiles.none')}</p>
    {/if}
    {#if picked}
      <div class="go" transition:reveal>
        <button class="danger" onclick={deleteOld}><Icon name="trash" size={16} /> {t('oldFiles.delete', { n: picked.files, size: formatSize(picked.bytes) })}</button>
      </div>
    {/if}
    {#if data.undated?.items}
      <p class="hint" transition:reveal>{t('oldFiles.undated', { n: data.undated.items, size: formatSize(data.undated.bytes) })}</p>
    {/if}
    {#each data.in_trash ?? [] as b (b.year)}
      <div class="trashed" transition:reveal>
        <span>{t('oldFiles.inTrash', { n: b.files, year: String(b.year), size: formatSize(b.bytes) })}</span>
        <div class="row">
          <button class="quiet" onclick={() => start(() => api.restoreOldFiles(b.year))}>{t('trash.restore')}</button>
          <button class="quiet danger" onclick={() => purge(b.year, b.files, b.bytes)}>{t('trash.purge')}</button>
        </div>
      </div>
    {/each}
  {/if}
</section>

<style>
  .go {
    display: flex;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--gap);
  }
  .job {
    display: grid;
    gap: 8px;
  }
  .bar {
    height: 6px;
    border-radius: 3px;
    background: var(--surface0);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--mauve);
    transition: width 0.3s;
  }
  .trashed {
    display: grid;
    gap: 8px;
    padding: 10px 12px;
    border-radius: var(--radius);
    background: var(--surface0);
  }
  .trashed .row {
    justify-content: flex-start;
  }
</style>
