<script lang="ts">
  // Settings, in sections (like the iPhone's): on phones a list of sections,
  // each opening as its own page with a back link; on wide screens the list
  // stays on the left and the section shows beside it. Each section has its
  // own address (/settings/backups), so the navigation menu can list them.
  // Phones and tablets (top spacing is for the home-screen app there).
  const touchScreen = matchMedia('(pointer: coarse)').matches;
  import { onMount } from 'svelte';
  import { reveal, pop } from '../lib/motion';
  import { api, toPlainSignIn } from '../lib/api';
  import { settings } from '../lib/settings.svelte';
  import { switchOff } from '../lib/sections';
  import { displayInfo } from '../lib/viewport';
  import ActionMenu, { type MenuItem } from '../components/ActionMenu.svelte';
  import { sortOptions } from '../components/SortMenu.svelte';
  import { sortFields, sortInfo, splitSort, joinSort } from '../lib/system';
  import InstallHelp from '../components/InstallHelp.svelte';
  import SitesCard from '../components/SitesCard.svelte';
  import DownloadRules from '../components/DownloadRules.svelte';
  import Icon from '../components/Icon.svelte';
  import { canSave, savedStats, clearSaved, helperState } from '../lib/offline.svelte';
  import NotificationsCard from '../components/NotificationsCard.svelte';
  import NoticeKindsCard from '../components/NoticeKindsCard.svelte';
  import BackupsCard from '../components/BackupsCard.svelte';
  import StatsCard from '../components/StatsCard.svelte';
  import { settingsSections } from '../lib/settingsSections';
  import Sections from '../components/Sections.svelte';
  import SearchBar from '../components/SearchBar.svelte';
  import Rich from '../components/Rich.svelte';
  import ChangelogCard from '../components/ChangelogCard.svelte';
  import UpdateStatus from '../components/UpdateStatus.svelte';
  import { router } from '../lib/router.svelte';
  import { t, num, i18n, languages } from '../lib/i18n.svelte';

  let { user, onLoggedOut }: { user: string; onLoggedOut: () => void } = $props();
  let stats = $state<{ files: number; tags: number; server?: { version: string } } | null>(null);
  // Shown to diagnose layout problems on phones (e.g. iOS home-screen app gaps).
  let display = $state(displayInfo());

  // The default sort as an action menu: the fields, each opening its two
  // directions (the current one lit, its direction beside it).
  const sortLabel = (v: string) => sortOptions().find((o) => o.value === v)?.label ?? v;
  function sortItems(value: string, set: (v: string) => void = (v) => (settings.defaultSort = v)): MenuItem[] {
    const cur = splitSort(value);
    return sortFields.filter((f) => f !== 'came' || settings.came).map((f) => {
      const i = sortInfo(f);
      const on = cur.field === f;
      if (!i.desc) return { label: i.label, on, run: () => set(f) };
      const ways = [
        { value: joinSort(f, false), label: t('settings.sortWay', { way: i.desc }) },
        { value: joinSort(f, true), label: t('settings.sortWay', { way: i.asc }) },
      ];
      return {
        label: i.label,
        on,
        hint: on ? (cur.asc ? ways[1] : ways[0]).label : undefined,
        choices: { heading: i.label, value: on ? value : '', options: ways, pick: set },
      };
    });
  }
  const newOrders = $derived([
    { value: 'imported', label: t('settings.newOrder.imported') },
    { value: 'imported_asc', label: t('settings.newOrder.importedAsc') },
    { value: 'uploaded', label: t('settings.newOrder.uploaded') },
  ]);
  // The language: Automatic (the browser's, named in brackets) or one of the language files.
  const languageName = (code: string) => languages.find((l) => l.code === code)?.name ?? code;
  const languageItems = $derived<MenuItem[]>([
    { label: t('settings.language.auto', { language: languageName(i18n.lang) }), on: settings.language === 'auto', run: () => (settings.language = 'auto') },
    ...languages.map((l) => ({ label: l.name, on: settings.language === l.code, run: () => (settings.language = l.code) })),
  ]);

  onMount(async () => {
    loadSaved();
    try {
      stats = await api.stats();
    } catch {
      /* shown as unknown */
    }
  });

  async function logout() {
    await api.logout().catch(() => {});
    await clearSaved(true); // nothing of the library stays on the device
    if (settings.anonymousLogin) return toPlainSignIn();
    onLoggedOut();
  }

  // Saved on this device (thumbnails kept by the service worker).
  let saved = $state<{ thumbs: number; bytes: number } | null>(null);
  let helper = $state<Awaited<ReturnType<typeof helperState>> | null>(null);
  const loadSaved = async () => {
    saved = await savedStats();
    helper = await helperState();
  };
  async function clearThumbs() {
    await clearSaved();
    await loadSaved();
  }
  const mb = (n: number) => (n < 1024 * 1024 ? t('unit.size.KB', { x: num(Math.round(n / 1024)) }) : t('unit.size.MB', { x: num(Math.round(n / (1024 * 1024))) }));
</script>

<Sections
  title={t('settings.title')}
  screen="settings"
  sections={settingsSections}
  fallback="library"
  hintOf={(sec) => (sec.id === 'account' ? t('settings.loggedInAs', { user }) : sec.hint)}>
  {#snippet foot()}
    <p class="hint foot">{t('settings.shared')}</p>
  {/snippet}
  {#snippet children(current: string)}
          {#if current === 'library'}
          <section class="card">
            <div class="card-head"><h2>{t('settings.section.library.label')}</h2></div>
            <!-- A div, not a label: the search bar has buttons and menus of its own. -->
            <div class="field">
              <span class="field-label">{t('settings.defaultSearch')}</span>
              <span class="hint">{t('settings.defaultSearchHint')}</span>
              <SearchBar
                plain
                label={t('settings.defaultSearch')}
                placeholder={t('settings.defaultSearchPlaceholder')}
                query={settings.defaultSearch}
                onsearch={(q) => (settings.defaultSearch = q)} />
            </div>
            <!-- A div, not a label: a click in a dropdown's list would count for the label and reopen it. -->
            <div class="field">
              <span class="field-label">{t('settings.defaultSort')}</span>
              <ActionMenu label={t('settings.defaultSort')} title={t('settings.defaultSort')} current={sortLabel(settings.defaultSort)} items={sortItems(settings.defaultSort)} />
            </div>
            <div class="field">
              <span class="field-label">{t('settings.favoritesSort')}</span>
              <ActionMenu
                label={t('settings.favoritesSort')}
                title={t('settings.favoritesSort')}
                current={sortLabel(settings.favoritesSort)}
                items={sortItems(settings.favoritesSort, (v) => (settings.favoritesSort = v))} />
            </div>
            <div class="field">
              <span class="field-label">{t('settings.newOrder.label')}</span>
              <span class="hint">{t('settings.newOrder.hint')}</span>
              <ActionMenu
                label={t('settings.newOrder.label')}
                title={t('settings.newOrder.label')}
                current={newOrders.find((o) => o.value === settings.newOrder)?.label ?? settings.newOrder}
                items={newOrders.map((o) => ({ label: o.label, on: o.value === settings.newOrder, run: () => (settings.newOrder = o.value) }))} />
            </div>
            <label class="check">
              <input type="checkbox" bind:checked={settings.newFromBulk} />
              <span class="field">
                <span class="field-label">{t('settings.bulkNew')}</span>
                <span class="hint">{t('settings.bulkNewHint')}</span>
              </span>
            </label>
            <div class="field">
              <span class="field-label">{t('settings.trashDays.label')}</span>
              <span class="hint">{t('settings.trashDays.hint')}</span>
              <ActionMenu
                label={t('settings.trashDays.label')}
                title={t('settings.trashDays.label')}
                current={settings.trashDays ? t('settings.trashDays.after', { n: settings.trashDays }) : t('settings.trashDays.never')}
                items={[0, 7, 30, 90, 365].map((n) => ({ label: n ? t('settings.trashDays.after', { n }) : t('settings.trashDays.never'), on: n === settings.trashDays, run: () => (settings.trashDays = n) }))} />
            </div>
            {#if stats}
              <p class="hint" transition:reveal>{t('common.files', { n: stats.files })} · {t('settings.tagsInUse', { n: stats.tags })}</p>
            {/if}
          </section>
          {:else if current === 'sites'}
            <SitesCard />
          {:else if current === 'rules'}
          <section class="card">
            <div class="card-head"><h2>{t('settings.section.rules.label')}</h2></div>
            <div class="rules">
              <DownloadRules />
              <label class="rule">
                <span class="field-label">{t('depth.label')}</span>
                <span class="unit-input">
                  <input
                    type="number"
                    min="0"
                    max="100000"
                    inputmode="numeric"
                    placeholder={t('depth.none')}
                    aria-label={t('settings.rules.depthLabel')}
                    value={settings.checkDepth || ''}
                    onchange={(e) => (settings.checkDepth = Math.min(100000, Math.max(0, Math.round(Number((e.currentTarget as HTMLInputElement).value) || 0))))} />
                  <span class="unit">{t('settings.rules.posts')}</span>
                </span>
                <span class="rule-hint">{t('settings.rules.depthHint')}</span>
              </label>
            </div>
          </section>
          {:else if current === 'notifications'}
            <NotificationsCard />
            <NoticeKindsCard />
          {:else if current === 'viewing'}
          <section class="card">
            <div class="card-head"><h2>{t('settings.sfw.title')}</h2></div>
            <!-- The sections: switching one off asks first (and whether to delete all of it); one stays on. -->
            {#each [{ id: 'media' as const, off: settings.mediaOff, label: t('menu.library'), hint: t('appSections.mediaHint') }, { id: 'comics' as const, off: settings.comicsOff, label: t('comics.title'), hint: t('appSections.comicsHint') }] as sec (sec.id)}
              {@const last = !sec.off && (sec.id === 'media' ? settings.comicsOff : settings.mediaOff)}
              <label class="check">
                <input
                  type="checkbox"
                  checked={!sec.off}
                  disabled={last}
                  onclick={(e) => {
                    if (sec.off) {
                      // (on again: no question)
                      if (sec.id === 'media') settings.mediaOff = false;
                      else settings.comicsOff = false;
                    } else {
                      e.preventDefault(); // (off once confirmed)
                      switchOff(sec.id);
                    }
                  }} />
                <span class="field">
                  <span class="field-label">{sec.label}</span>
                  <span class="hint">{last ? t('appSections.lastOn') : sec.hint}</span>
                </span>
              </label>
            {/each}
            <label class="check">
              <input type="checkbox" bind:checked={settings.sfwMode} />
              <span class="field">
                <span class="field-label">{t('settings.sfw.label')}</span>
                <span class="hint">{t('settings.sfw.hint')}</span>
              </span>
            </label>
          </section>
          <section class="card">
            <div class="card-head"><h2>{t('settings.language.title')}</h2></div>
            <div class="field">
              <span class="hint">{t('settings.language.hint')}</span>
              <ActionMenu
                label={t('settings.language.title')}
                title={t('settings.language.title')}
                current={settings.language === 'auto' ? t('settings.language.auto', { language: languageName(i18n.lang) }) : languageName(settings.language)}
                items={languageItems} />
            </div>
          </section>
          <section class="card">
            <div class="card-head"><h2>{t('settings.videos.title')}</h2></div>
            <label class="check">
              <input type="checkbox" bind:checked={settings.startMuted} />
              <span class="field">
                <span class="field-label">{t('settings.videos.muted')}</span>
                <span class="hint">{t('settings.videos.mutedHint')}</span>
              </span>
            </label>
            {#if !settings.sfwMode}
            <label class="check" transition:reveal>
              <input type="checkbox" bind:checked={settings.cameButton} />
              <span class="field">
                <span class="field-label">{t('settings.videos.came')}</span>
                <span class="hint">{t('settings.videos.cameHint')}</span>
              </span>
            </label>
            {/if}
            <label class="check">
              <input type="checkbox" bind:checked={settings.hoverPreview} />
              <span class="field">
                <span class="field-label">{t('settings.videos.hover')}</span>
                <span class="hint">{t('settings.videos.hoverHint')}</span>
              </span>
            </label>
          </section>
          <section class="card">
            <div class="card-head"><h2>{t('settings.grid.title')}</h2></div>
            <div class="grid-row">
              <span class="field">
                <span class="field-label" id="grid-layout">{t('settings.grid.tiles')}</span>
                <span class="hint">{t('settings.thisDevice')}</span>
              </span>
              <div class="seg" role="radiogroup" aria-labelledby="grid-layout">
                {#each [['masonry', t('settings.grid.masonry')], ['squares', t('settings.grid.squares')]] as [id, label] (id)}
                  <button role="radio" aria-checked={settings.gridLayout === id} class:on={settings.gridLayout === id} onclick={() => (settings.gridLayout = id as 'squares' | 'masonry')}>{label}</button>
                {/each}
              </div>
            </div>
          </section>
          {#if touchScreen}
          <section class="card" transition:reveal>
            <div class="card-head"><h2>{t('settings.display.title')}</h2></div>
            <label class="field">
              <span class="field-label">{t('settings.display.top')} <span class="value">{t('settings.display.pt', { n: settings.topSpacing })}</span></span>
              <span class="hint">{t('settings.display.topHint')}</span>
              <input type="range" min="0" max="60" step="2" bind:value={settings.topSpacing} aria-label={t('settings.display.top')} />
            </label>
          </section>
          {/if}
          {:else if current === 'device'}
          <section class="card install-card">
            <div class="card-head"><h2>{t('settings.device.app')}</h2></div>
            <InstallHelp />
          </section>
          <section class="card">
            <div class="card-head">
              <h2>{t('settings.section.device.label')}</h2>
              <p class="hint">{t('settings.device.only')}</p>
            </div>
            {#if canSave()}
              <div class="saved" transition:reveal>
                <div class="field">
                  <span class="field-label">{t('settings.device.saved')}</span>
                  <span class="hint">
                    {#if saved?.thumbs}{t('settings.device.thumbs', { n: saved.thumbs })}{saved.bytes ? ` · ${t('settings.device.inAll', { size: mb(saved.bytes) })}` : ''}{:else}{t('settings.device.nothing')}{/if}.
                    {t('settings.device.offline')}
                  </span>
                  {#if helper && helper.state !== 'running'}
                    <span class="hint helper" role="status" transition:pop>
                      {#if helper.state === 'starting'}
                        {t('settings.device.starting')}
                      {:else}
                        {t('settings.device.notSaving', { why: helper.why ?? '' })}
                      {/if}
                    </span>
                  {/if}
                </div>
                <button class="quiet" onclick={clearThumbs} disabled={!saved?.thumbs}>{t('settings.device.clear')}</button>
              </div>
            {/if}
            <details>
              <summary class="hint">{t('settings.device.screen')}</summary>
              <p class="hint mono">
                window {display.window} · screen {display.screen} · visible {display.visual} · app {display.app}<br />
                safe areas: {display.safe} · pixel ratio {display.ratio} · {display.standalone ? t('settings.device.homeScreenApp') : t('settings.device.browser')}
              </p>
              <button class="quiet" onclick={() => (display = displayInfo())}>{t('settings.device.refresh')}</button>
            </details>
          </section>
          {:else if current === 'backups'}
            <BackupsCard />
          {:else if current === 'stats'}
            <StatsCard />
          {:else if current === 'changelog'}
            <ChangelogCard />
          {:else if current === 'account'}
            <section class="card">
              <div class="card-head"><h2>{t('settings.section.account.label')}</h2></div>
              <div class="account">
                <p><Rich text={t('settings.loggedInAsBold', { user })} /></p>
                <button class="quiet" onclick={logout}>{t('settings.section.account.hint')}</button>
              </div>
              <label class="check">
                <input type="checkbox" bind:checked={settings.anonymousLogin} />
                <span class="field">
                  <span class="field-label">{t('settings.anonymous.label')}</span>
                  <span class="hint">{t('settings.anonymous.hint')}</span>
                </span>
              </label>
            </section>
            <section class="card">
              <div class="card-head"><h2>{t('settings.firstSteps.title')}</h2></div>
              <div class="account">
                <p class="hint">{t('settings.firstSteps.hint')}</p>
                <button class="quiet" onclick={() => (settings.welcome = 'sections')}>{t('settings.firstSteps.again')}</button>
              </div>
            </section>
            <!-- About: the version and its updates, the license, the source (the AGPL), the third-party notices. -->
            <section class="card about">
              <div class="card-head"><h2>{t('settings.about.title')}</h2></div>
              <p class="about-line"><strong>Vitrine</strong>{#if stats?.server?.version}&nbsp;<span class="about-ver">{stats.server.version}</span>{/if}</p>
              <UpdateStatus />
              <p class="hint">{t('settings.about.license')}</p>
              <div class="about-links">
                <button class="link" onclick={() => router.go('settings', 'changelog')}>{t('settings.about.whatsNew')}</button>
                <a href="https://github.com/usevitrine/vitrine" target="_blank" rel="noopener noreferrer">{t('settings.about.source')} <Icon name="external" size={14} /></a>
                <a href="/licenses" target="_blank" rel="noopener">{t('settings.about.thirdParty')} <Icon name="external" size={14} /></a>
              </div>
            </section>
          {/if}
  {/snippet}
</Sections>

<style>
  p {
    margin: 0;
  }
  .check {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    cursor: pointer;
  }
  .check input {
    margin-top: 2px;
  }
  .install-card {
    display: grid;
    gap: 12px;
  }
  .grid-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
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
    min-height: calc(var(--control-h) - 8px);
    padding: 0 12px;
    border-radius: 999px;
    background: transparent;
    color: var(--subtext);
    font-weight: 600;
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
  .check .field {
    gap: 2px;
  }
  .value {
    margin-left: 6px;
    color: var(--mauve);
    font-variant-numeric: tabular-nums;
  }
  input[type='range'] {
    width: 100%;
    max-width: 360px;
  }
  details {
    display: grid;
    gap: 10px;
  }
  details[open] summary {
    margin-bottom: 10px;
  }
  details button {
    margin-top: 10px;
  }
  summary {
    cursor: pointer;
    width: fit-content;
  }
  .mono {
    font-variant-numeric: tabular-nums;
    line-height: 1.7;
  }
  .about-line {
    margin: 0;
  }
  .about-ver {
    color: var(--subtext);
  }
  .about-links {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 18px;
  }
  .about-links a,
  .about-links .link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--mauve);
    font-weight: 600;
    text-decoration: none;
  }
  /* What's new: a link's look (it opens a dialog, not a page). */
  .about-links .link {
    min-height: 0;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .rules {
    display: grid;
    gap: 12px;
  }
  @media (min-width: 640px) {
    .rules {
      grid-template-columns: 1fr 1fr;
    }
  }
  .rule-hint {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .helper {
    display: block;
    margin-top: 4px;
    color: var(--yellow);
  }
  .saved {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .saved .field {
    gap: 2px;
  }
  .account {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .foot {
    text-align: center;
  }
</style>
