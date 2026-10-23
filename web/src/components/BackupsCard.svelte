<script lang="ts">
  // Settings > Backups: copies of the library database (tags, subscriptions,
  // settings, deletion records) - daily and on demand; download one, restore
  // one (it goes in place when Vitrine restarts), or restore from a file.
  import { onMount } from 'svelte';
  import { api, type BackupInfo } from '../lib/api';
  import { settings } from '../lib/settings.svelte';
  import { formatSize } from '../lib/results.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import Dropdown from './Dropdown.svelte';
  import Icon from './Icon.svelte';
  import ActionMenu from './ActionMenu.svelte';
  import Rich from './Rich.svelte';
  import { t } from '../lib/i18n.svelte';
  import { when } from '../lib/when';

  let list = $state<BackupInfo[]>([]);
  let dir = $state('');
  let pending = $state(false);
  let lastError = $state('');
  let loaded = $state(false);
  let making = $state(false);
  let staging = $state(false);
  let restarting = $state(false);
  let fileInput = $state<HTMLInputElement>();

  async function load() {
    try {
      const r = await api.backups();
      list = r.backups;
      dir = r.dir;
      pending = r.pending;
      lastError = r.last_error;
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      loaded = true;
    }
  }
  onMount(load);


  async function backUpNow() {
    making = true;
    try {
      const { backup } = await api.createBackup();
      toasts.show(t('backups.done', { size: formatSize(backup.size) }), { cat: 'backups' });
      await load();
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      making = false;
    }
  }

  /** Saves a backup file (the browser's own download). */
  function download(name: string) {
    const a = document.createElement('a');
    a.href = `/api/backups/${name}/download`;
    a.download = name;
    document.body.append(a);
    a.click();
    a.remove();
  }

  async function remove(b: BackupInfo) {
    const ok = await confirmAction({ title: t('backups.deleteTitle', { when: when(b.created_at) }), action: t('common.delete') });
    if (!ok) return;
    try {
      await api.deleteBackup(b.name);
      list = list.filter((x) => x.name !== b.name);
      toasts.show(t('backups.deleted', { when: when(b.created_at) }), { cat: 'backups', kind: 'removed' });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  async function restore(b: BackupInfo) {
    const ok = await confirmAction({
      title: t('backups.restoreTitle', { when: when(b.created_at) }),
      body: t('backups.restoreBody'),
      action: t('backups.restore'),
      icon: 'undo',
    });
    if (!ok) return;
    staging = true;
    try {
      await api.restoreBackup(b.name);
      pending = true;
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      staging = false;
    }
  }

  async function restoreFile(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0];
    (e.currentTarget as HTMLInputElement).value = '';
    if (!file) return;
    const ok = await confirmAction({
      title: t('backups.restoreFileTitle'),
      body: t('backups.restoreFileBody'),
      action: t('backups.restore'),
      icon: 'undo',
    });
    if (!ok) return;
    staging = true;
    try {
      await api.uploadBackup(file);
      pending = true;
    } catch (err) {
      toasts.error((err as Error).message);
    } finally {
      staging = false;
    }
  }

  async function cancelRestore() {
    try {
      await api.cancelRestore();
      pending = false;
      toasts.show(t('backups.restoreCanceled'), { cat: 'backups', kind: 'info' });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  // Restart: Vitrine stops; Docker or a service manager starts it again. The
  // page waits for it to answer, then reloads.
  async function restart() {
    restarting = true;
    try {
      await api.restart();
    } catch (e) {
      restarting = false;
      toasts.error((e as Error).message);
      return;
    }
    await new Promise((r) => setTimeout(r, 1500));
    for (let i = 0; i < 120; i++) {
      try {
        const r = await fetch('/api/health', { cache: 'no-store' });
        if (r.ok) {
          location.reload();
          return;
        }
      } catch {
        /* not back yet */
      }
      await new Promise((r) => setTimeout(r, 1000));
    }
    restarting = false;
    toasts.error(t('backups.notBack'));
  }

  const keepOptions = $derived([3, 7, 14, 30, 60].map((n) => ({ value: String(n), label: t('backups.keepN', { n }) })));
</script>

<section class="card">
  <div class="card-head">
    <h2>{t('backups.title')}</h2>
    <p class="hint">{t('backups.hint')}</p>
  </div>

  {#if pending}
    <div class="pending" role="status">
      <p><Rich text={t('backups.pending')} /></p>
      <div class="row-actions">
        <button class="go" onclick={restart} disabled={restarting}><Icon name="retry" size={17} /> {restarting ? t('backups.restarting') : t('backups.restartNow')}</button>
        <button class="quiet" onclick={cancelRestore} disabled={restarting}>{t('backups.cancelRestore')}</button>
      </div>
      <p class="hint small">{t('backups.restartHint')}</p>
    </div>
  {/if}

  <label class="check">
    <input type="checkbox" bind:checked={settings.backupAuto} />
    <span class="field">
      <span class="field-label">{t('backups.daily')}</span>
      <span class="hint">{t('backups.dailyHint')}</span>
    </span>
  </label>

  <div class="bar">
    <Dropdown value={String(settings.backupKeep)} options={keepOptions} onchange={(v) => (settings.backupKeep = Number(v))} label={t('backups.keepLabel')} heading={t('backups.keep')} />
    <button class="go now" onclick={backUpNow} disabled={making}><Icon name="download" size={17} /> {making ? t('backups.making') : t('backups.now')}</button>
  </div>

  {#if lastError}<p class="error small">{t('backups.failed', { error: lastError })}</p>{/if}

  {#if list.length}
    <ul class="list" aria-label={t('backups.title')}>
      {#each list as b, i (b.name)}
        <li>
          <div class="row">
            <span class="what">
              <span class="q">{when(b.created_at)}{#if i === 0}<span class="newest">{t('backups.newest')}</span>{/if}</span>
              <span class="sub">{formatSize(b.size)}</span>
            </span>
            <ActionMenu
              label={t('backups.more', { when: when(b.created_at) })}
              title={when(b.created_at)}
              items={[
                { label: t('backups.download'), icon: 'download', hint: formatSize(b.size), run: () => download(b.name) },
                { label: t('backups.restoreMenu'), icon: 'undo', disabled: staging, run: () => restore(b) },
                { label: t('common.delete'), icon: 'trash', danger: true, run: () => remove(b) },
              ]} />
          </div>
        </li>
      {/each}
    </ul>
  {:else if loaded}
    <p class="empty">{t('backups.empty')}</p>
  {/if}

  <div class="foot">
    <button class="quiet" onclick={() => fileInput?.click()} disabled={staging}><Icon name="undo" size={16} /> {t('backups.fromFile')}</button>
    <input bind:this={fileInput} type="file" accept=".gz,.db,application/gzip" hidden onchange={restoreFile} aria-label={t('backups.file')} />
    {#if dir}<span class="hint small">{t('backups.savedIn', { dir })}</span>{/if}
  </div>
</section>

<style>
  /* A checkbox beside its label and hint (like the other settings). */
  .check {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    cursor: pointer;
  }
  .check input {
    margin-top: 2px;
  }
  .check .field {
    gap: 2px;
  }
  .pending {
    display: grid;
    gap: 10px;
    padding: 12px;
    border-radius: 12px;
    background: color-mix(in srgb, var(--yellow) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--yellow) 40%, transparent);
  }
  .pending p {
    margin: 0;
  }
  .row-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .bar :global(.dropdown) {
    width: 140px;
  }
  .now {
    margin-left: auto;
    padding: 0 16px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    border-radius: 12px;
    border: 1px solid var(--surface0);
    background: var(--crust);
  }
  li + li {
    border-top: 1px solid var(--surface0);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 6px 6px 6px 14px;
  }
  .what {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 1px;
  }
  .q {
    font-weight: 600;
  }
  .newest {
    margin-left: 8px;
    padding: 1px 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--green) 14%, transparent);
    color: var(--green);
    font-size: 12px;
    font-weight: 600;
  }
  .sub {
    font-size: var(--font-small);
    color: var(--subtext);
  }
  .foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 12px;
  }
  .foot .hint {
    overflow-wrap: anywhere;
  }
  p {
    margin: 0;
  }
</style>
