<script lang="ts">
  // The download rules' size limits: videos over so many MB and images over so many megapixels are skipped (shared
  // settings). In Settings > Download rules and in the first steps.
  import { settings } from '../lib/settings.svelte';
  import { t } from '../lib/i18n.svelte';

  const thisYear = new Date().getFullYear();
  // (a typed "15" or a year to come isn't a limit: the box goes back to what's set; empty: none)
  function setYear(e: Event) {
    const box = e.currentTarget as HTMLInputElement;
    const y = Math.round(Number(box.value) || 0);
    if (!box.value || (y >= 1970 && y <= thisYear)) settings.skipUploadedYear = box.value ? y : 0;
    box.value = settings.skipUploadedYear ? String(settings.skipUploadedYear) : '';
  }
</script>

<label class="rule">
  <span class="field-label">{t('settings.rules.video')}</span>
  <span class="unit-input">
    <input
      type="number"
      min="1"
      inputmode="numeric"
      placeholder={t('settings.rules.noLimit')}
      aria-label={t('settings.rules.videoLabel')}
      value={settings.skipVideoMB || ''}
      onchange={(e) => (settings.skipVideoMB = Math.max(0, Number((e.currentTarget as HTMLInputElement).value) || 0))} />
    <span class="unit">MB</span>
  </span>
</label>
<label class="rule">
  <span class="field-label">{t('settings.rules.image')}</span>
  <span class="unit-input">
    <input
      type="number"
      min="1"
      step="any"
      inputmode="decimal"
      placeholder={t('settings.rules.noLimit')}
      aria-label={t('settings.rules.imageLabel')}
      value={settings.skipImageMP || ''}
      onchange={(e) => (settings.skipImageMP = Math.max(0, Number((e.currentTarget as HTMLInputElement).value) || 0))} />
    <span class="unit">{t('settings.rules.megapixels')}</span>
  </span>
</label>
<label class="rule">
  <span class="field-label">{t('settings.rules.year')}</span>
  <span class="unit-input">
    <input
      type="number"
      min="1970"
      max={thisYear}
      inputmode="numeric"
      placeholder={t('settings.rules.noLimit')}
      aria-label={t('settings.rules.yearLabel')}
      value={settings.skipUploadedYear || ''}
      onchange={setYear} />
    <span class="unit">{t('settings.rules.orEarlier')}</span>
  </span>
  <span class="rule-hint">{t('settings.rules.yearHint')}</span>
</label>

<style>
  .rule-hint {
    display: block;
    margin-top: 4px;
    color: var(--subtext);
    font-size: var(--font-small);
  }
</style>
