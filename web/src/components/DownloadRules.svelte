<script lang="ts">
  // The download rules' size limits: videos over so many MB and images over so many megapixels are skipped (shared
  // settings). In Settings > Download rules and in the first steps.
  import { settings } from '../lib/settings.svelte';
  import { t } from '../lib/i18n.svelte';
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
