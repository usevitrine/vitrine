<script lang="ts">
  // Settings > Notifications: where each kind of notice goes - the bell, on screen (a moment), or off. For every
  // device (push notifications are set per device, above). Ones with a button (Undo, Reload) show on screen in any
  // case, as errors do.
  import { noticeKinds, modeOf, type NoticeMode } from '../lib/noticeKinds';
  import { pop } from '../lib/motion';
  import { settings } from '../lib/settings.svelte';
  import { t } from '../lib/i18n.svelte';

  const groups = ['downloader', 'library', 'comics', 'downloads', 'settings', 'app'] as const;
  const modes: NoticeMode[] = ['bell', 'screen', 'off'];

  function set(id: string, mode: NoticeMode) {
    const def = noticeKinds.find((k) => k.id === id)?.mode;
    const next = { ...settings.noticeModes };
    if (mode === def) delete next[id];
    else next[id] = mode;
    settings.noticeModes = next;
  }
  const changed = $derived(Object.keys(settings.noticeModes).length > 0);
</script>

<section class="card">
  <div class="card-head with-action">
    <div>
      <h2>{t('noticeKinds.title')}</h2>
      <p class="hint">{t('noticeKinds.hint')}</p>
    </div>
    {#if changed}<button class="quiet" onclick={() => (settings.noticeModes = {})} transition:pop>{t('noticeKinds.defaults')}</button>{/if}
  </div>
  {#each groups as g (g)}
    <h3 class="group">{t(`noticeKinds.groups.${g}`)}</h3>
    <ul class="kinds">
      {#each noticeKinds.filter((k) => k.group === g && (k.id !== 'came' || settings.came)) as k (k.id)}
        {@const mode = modeOf(k.id, settings.noticeModes)}
        <li>
          <span class="name" id="nk-{k.id}">
            {t(`noticeKinds.kinds.${k.id}`)}
            {#if k.undo}<span class="note">{t(k.id === 'update' ? 'noticeKinds.withReload' : 'noticeKinds.withUndo')}</span>{/if}
          </span>
          <div class="seg" role="radiogroup" aria-labelledby="nk-{k.id}">
            {#each modes as m (m)}
              <!-- (an error can't be off: it always shows on screen) -->
              {#if !(k.id === 'errors' && m === 'off')}
                <button role="radio" aria-checked={mode === m} class:on={mode === m} onclick={() => set(k.id, m)}>{t(`noticeKinds.modes.${m}`)}</button>
              {/if}
            {/each}
          </div>
        </li>
      {/each}
    </ul>
  {/each}
</section>

<style>
  .with-action {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }
  .with-action button {
    flex: none;
  }
  .group {
    margin: 14px 0 4px;
    font-size: var(--font-small);
    font-weight: 600;
    color: var(--subtext);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .kinds {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .kinds li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 7px 0;
    border-top: 1px solid var(--surface0);
  }
  .kinds li:first-child {
    border-top: 0;
  }
  .name {
    min-width: 0;
    display: grid;
    gap: 1px;
  }
  .note {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .seg {
    display: inline-flex;
    flex-shrink: 0;
    padding: 3px;
    gap: 2px;
    border-radius: 999px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
  }
  .seg button {
    min-height: calc(var(--control-h) - 10px);
    padding: 0 10px;
    border-radius: 999px;
    background: transparent;
    border: 0;
    color: var(--subtext);
    font-weight: 600;
    font-size: var(--font-small);
  }
  .seg button.on {
    background: var(--surface0);
    color: var(--text);
  }
  @media (hover: hover) {
    .seg button:not(.on):hover {
      color: var(--text);
    }
  }
</style>
