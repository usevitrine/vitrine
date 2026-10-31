<script lang="ts">
  // Settings > Help & about: the problem report - made on the server, private values replaced; downloaded, copied or
  // shown. (In Stats until 2026-10-31.)
  import { overlayScroll } from '../lib/overlayScroll';
  import { reveal } from '../lib/motion';
  import { api } from '../lib/api';
  import { toasts } from '../lib/toast.svelte';
  import { copyText } from '../lib/clipboard';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n.svelte';

  let report = $state('');
  let showReport = $state(false);
  let making = $state(false);
  async function getReport(): Promise<string> {
    making = true;
    try {
      report = await api.report();
      return report;
    } finally {
      making = false;
    }
  }
  async function reportAction(what: 'show' | 'copy' | 'download') {
    if (what === 'show' && showReport) {
      showReport = false;
      return;
    }
    let text: string;
    try {
      text = await getReport();
    } catch (e) {
      toasts.error((e as Error).message);
      return;
    }
    if (what === 'show') showReport = true;
    else if (what === 'copy') copyText(text, t('stats.report.what'));
    else {
      const a = document.createElement('a');
      a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
      a.download = `vitrine-report-${new Date().toISOString().slice(0, 10)}.txt`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
    }
  }
</script>

<section class="card">
  <div class="card-head">
    <h2>{t('stats.report.title')}</h2>
    <p class="hint">{t('stats.report.hint')}</p>
  </div>
  <div class="actions">
    <button onclick={() => reportAction('download')} disabled={making}><Icon name="download" size={16} /> {t('backups.download')}</button>
    <button class="quiet" onclick={() => reportAction('copy')} disabled={making}><Icon name="copy" size={16} /> {t('tagMenu.copy')}</button>
    <button class="quiet" onclick={() => reportAction('show')} disabled={making} aria-expanded={showReport}><Icon name={showReport ? 'eyeoff' : 'eye'} size={16} /> {showReport ? t('stats.report.hide') : t('stats.report.show')}</button>
  </div>
  {#if showReport}
    <pre class="report" {@attach overlayScroll} aria-label={t('stats.report.label')} transition:reveal>{report}</pre>
  {/if}
</section>

<style>
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .report {
    margin: 0;
    max-height: min(60vh, 520px);
    overflow: auto;
    padding: 12px;
    border-radius: 10px;
    background: var(--crust);
    border: 1px solid var(--surface0);
    font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    color: var(--subtext);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    tab-size: 2;
  }
</style>
