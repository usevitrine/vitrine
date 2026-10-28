<script lang="ts">
  import { overlayScroll } from '../lib/overlayScroll';
  // The first steps after the first account: what you'll use (and safe for work), storage, sites, how the library
  // looks, the app on the home screen, notifications, what never to download (blacklist, size limits), a first
  // download, then a page of good-to-knows - and a short tour of the library once they're done (lib/tour).
  // Every step can be skipped; where it is is a shared setting (welcome), so it
  // carries on where it was left - on any device - until finished or skipped.
  import { onMount } from 'svelte';
  import { reveal, pop, glide } from '../lib/motion';
  import { fade } from 'svelte/transition';
  import { cubicOut } from 'svelte/easing';
  import { reducedMotion } from '../lib/hero';
  import { newDownloadDialog } from '../lib/newDownload.svelte';
  import { api, type LibraryStats, type UpdateState } from '../lib/api';
  import { updater } from '../lib/updater.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { settings } from '../lib/settings.svelte';
  import { whatsNew } from '../lib/whatsNew.svelte';
  import { router } from '../lib/router.svelte';
  import { sites } from '../lib/sites.svelte';
  import { bytes } from '../lib/live.svelte';
  import { pushSupport } from '../lib/push.svelte';
  import Logo from '../components/Logo.svelte';
  import Icon from '../components/Icon.svelte';
  import SitesCard from '../components/SitesCard.svelte';
  import NotificationsCard from '../components/NotificationsCard.svelte';
  import InstallHelp from '../components/InstallHelp.svelte';
  import Rich from '../components/Rich.svelte';
  import { t } from '../lib/i18n.svelte';
  import { tour } from '../lib/tour.svelte';
  import BlacklistPanel from '../components/BlacklistPanel.svelte';
  import DownloadRules from '../components/DownloadRules.svelte';

  // (the blacklist's taps: common things people never want - neutral examples)
  const commonBlacklist = ['watermark', 'text_only', 'ai_generated', 'lowres', 'jpeg_artifacts'];
  const fineMouse = matchMedia('(hover: hover) and (pointer: fine)').matches;

  type Step = 'sections' | 'storage' | 'sites' | 'look' | 'app' | 'notify' | 'rules' | 'library' | 'done';
  const steps: Step[] = ['sections', 'storage', 'sites', 'look', 'app', 'notify', 'rules', 'library', 'done'];

  const step = $derived<Step>(steps.includes(settings.welcome as Step) ? (settings.welcome as Step) : 'sections');
  const at = $derived(steps.indexOf(step));
  let page = $state<HTMLElement>();

  // Each step slides in from the side it's reached from (forward: the right, back: the left); the last one fades.
  let dir = $state(1);
  function go(s: Step | 'finished') {
    if (s !== 'finished') dir = steps.indexOf(s) >= at ? 1 : -1;
    settings.welcome = s;
    page?.scrollTo({ top: 0 });
  }
  const moving = () => !reducedMotion();
  // (the new one waits for the old one to fade: never both at once, text over text)
  const stepIn = (node: Element) => glide(node, { x: moving() ? 28 * dir : 0, duration: moving() ? 280 : 0, delay: moving() ? 120 : 0, easing: cubicOut, opacity: 0 });
  const stepOut = (node: Element) => fade(node, { duration: moving() ? 120 : 0 });
  const next = () => go(steps[at + 1] ?? 'finished');
  const back = () => at > 0 && go(steps[at - 1]);

  // ---- a newer Vitrine: updated from here, setup starts over on it (what it shows may have changed)
  let info = $state<UpdateState | null>(null);
  const update = $derived(info?.newer ? info : null);
  let checking = $state(false);
  let checked = $state(false); // (asked here: "Up to date" says so)
  onMount(async () => {
    try {
      info = await api.updateState();
      if (!info.latest && !info.error) info = await api.checkForUpdate(); // (just installed: not asked yet)
    } catch {
      /* no update API, or offline: nothing shown */
    }
  });
  async function checkNow() {
    checking = true;
    try {
      info = await api.checkForUpdate();
      checked = true;
    } catch (e) {
      toasts.error(t('updates.checkFailed', { error: (e as Error).message }));
    } finally {
      checking = false;
    }
  }
  async function updateNow() {
    if (!update) return;
    if (!(await confirmAction({ title: t('updates.confirmTitle', { version: update.latest }), body: t('setup.update.confirm'), action: t('updates.update'), icon: 'download' }))) return;
    dir = -1;
    settings.welcome = 'sections';
    updater.start(update);
  }

  // ---- storage
  let stats = $state<LibraryStats | null>(null);
  let statsError = $state('');
  onMount(async () => {
    try {
      stats = await api.stats(0);
    } catch (e) {
      statsError = (e as Error).message;
    }
  });
  const disk = (which: 'data' | 'files') => stats?.storage.disks.find((d) => d.for === which || d.for === 'both');
  /** Little room left: under 10 GB or 5 %. */
  const low = (d?: { total: number; free: number }) => !!d && (d.free < 10e9 || d.free < d.total * 0.05);

  // ---- first download: the New download dialog itself (the site, the search with tag suggestions, once - all or the
  // newest so many - or a subscription with its interval and group); done, the steps go on.
  const usable = $derived(Object.keys(sites.names).filter((s) => !sites.off.includes(s)));
  let started = $state<'once' | 'sub' | null>(null);
  function startDownload() {
    newDownloadDialog.current = {
      query: '',
      fromTags: false,
      ondone: (subscribed) => {
        started = subscribed ? 'sub' : 'once';
        next();
      },
    };
  }

  function finish(to: 'library' | 'comics' | 'downloads' | 'blacklist' | 'rules') {
    settings.welcome = 'finished';
    void whatsNew.markSeen(); // (just set up: this version's changes aren't news)
    tour.start(); // (a few hints on the library, once - on this device)
    if (to === 'downloads') router.go('downloads');
    else if (to === 'blacklist') router.go('tags', 'rules');
    else if (to === 'rules') router.go('settings', 'rules');
    else if (to === 'comics') router.go('comics');
    else router.go('library');
  }
</script>

<div class="first" bind:this={page} {@attach overlayScroll}>
  <div class="inner">
    <header>
      <Logo size={28} />
      {#if step !== 'done'}
        <button class="quiet skip-all" onclick={() => finish('library')}>{t('setup.skipAll')}</button>
      {/if}
    </header>
    <div class="progress" role="progressbar" aria-label={t('setup.label')} aria-valuemin={1} aria-valuemax={steps.length} aria-valuenow={at + 1}>
      {#each steps as s, i (s)}
        <span class:done={i < at} class:now={i === at}></span>
      {/each}
    </div>
    {#if update}
      <div class="update" transition:reveal>
        <Icon name="download" size={18} />
        <span class="field">
          <strong>{t('updates.notice', { version: update.latest })}</strong>
          <span class="hint">{update.can_update ? t('setup.update.hint') : t('updates.howTo')}</span>
        </span>
        {#if update.can_update}<button class="primary" onclick={updateNow}>{t('updates.update')}</button>{/if}
      </div>
    {/if}
    <div class="count">
      <span>{t('setup.stepOf', { n: at + 1, total: steps.length })}</span>
      {#if info && !update}
        {#key checked}
          <span class="check-update" in:fade={{ duration: 150 }}>
            {#if checked && !info.error}<Icon name="check" size={14} /> {t('updates.upToDate')}{:else}<button class="link" onclick={checkNow} disabled={checking}>{checking ? t('updates.checking') : t('setup.update.check')}</button>{/if}
          </span>
        {/key}
      {/if}
    </div>
    <div class="stage">
    {#key step}
    <div class="step" in:stepIn out:stepOut>
    <h1>{t(`setup.title.${step}`)}</h1>

    {#if step === 'sections'}
      <!-- What you'll use: Media, Comics or both (Settings > Viewing > Content later); one at least. -->
      <p class="lead">{t('setup.sections.lead')}</p>
      <div class="sections">
        {#each [{ id: 'media', icon: 'library' as const, label: t('menu.library'), hint: t('appSections.mediaHint'), on: !settings.mediaOff }, { id: 'comics', icon: 'book' as const, label: t('comics.title'), hint: t('appSections.comicsHint'), on: !settings.comicsOff }] as sec (sec.id)}
          <button
            class="section"
            class:on={sec.on}
            role="checkbox"
            aria-checked={sec.on}
            onclick={() => {
              if (sec.id === 'media') {
                if (sec.on && settings.comicsOff) settings.comicsOff = false; // (one stays on: the other comes on)
                settings.mediaOff = sec.on;
              } else {
                if (sec.on && settings.mediaOff) settings.mediaOff = false;
                settings.comicsOff = sec.on;
              }
            }}>
            <span class="section-icon"><Icon name={sec.icon} size={26} /></span>
            <span class="section-text"><strong>{sec.label}</strong><span class="hint">{sec.hint}</span></span>
            <span class="tick" aria-hidden="true">{#if sec.on}<Icon name="check" size={16} />{/if}</span>
          </button>
        {/each}
      </div>
      <label class="check sfw">
        <input type="checkbox" bind:checked={settings.sfwMode} />
        <span class="check-text"><strong>{t('settings.sfw.label')}</strong><span class="hint">{t('settings.sfw.hint')}</span></span>
      </label>
    {:else if step === 'storage'}
      <p class="lead">{t('setup.storage.lead')}</p>
      {#if statsError}
        <p class="problem" transition:reveal><Icon name="alert" size={16} /> {t('setup.storage.failed', { error: statsError })}</p>
      {:else if !stats}
        <p class="hint" transition:reveal>{t('notify.checking')}</p>
      {:else}
        {@const files = disk('files')}
        {@const data = disk('data')}
        <ul class="checks">
          <li class:warn={low(files)}>
            <Icon name={low(files) ? 'alert' : 'check'} size={18} />
            <div>
              <strong>{t('setup.storage.files')}</strong>
              <code>{stats.storage.files_dir}</code>
              {#if files}<span class="hint">{t('setup.storage.free', { free: bytes(files.free), total: bytes(files.total) })}</span>{/if}
              {#if low(files)}<span class="fix"><Rich text={t('setup.storage.filesLow')} /></span>{/if}
            </div>
          </li>
          <li class:warn={low(data)}>
            <Icon name={low(data) ? 'alert' : 'check'} size={18} />
            <div>
              <strong>{t('setup.storage.data')}</strong>
              <code>{stats.storage.data_dir}</code>
              {#if data && data !== files}<span class="hint">{t('setup.storage.free', { free: bytes(data.free), total: bytes(data.total) })}</span>{/if}
              {#if low(data)}<span class="fix">{t('setup.storage.dataLow')}</span>{/if}
            </div>
          </li>
          <li class:warn={!stats.server.ffmpeg}>
            <Icon name={stats.server.ffmpeg ? 'check' : 'alert'} size={18} />
            <div>
              <strong>{t('setup.storage.ffmpeg')}</strong>
              {#if stats.server.ffmpeg}
                <span class="hint">{stats.server.ffmpeg === 'found' ? t('setup.storage.found') : stats.server.ffmpeg}</span>
              {:else}
                <span class="fix">{t('setup.storage.noFfmpeg')}</span>
              {/if}
            </div>
          </li>
          <li class:soft={!stats.server.vips}>
            <Icon name={stats.server.vips ? 'check' : 'info'} size={18} />
            <div>
              <strong>{t('setup.storage.vips')}</strong>
              {#if stats.server.vips}
                <span class="hint">{stats.server.vips === 'found' ? t('setup.storage.found') : stats.server.vips}</span>
              {:else}
                <span class="hint">{t('setup.storage.noVips')}</span>
              {/if}
            </div>
          </li>
        </ul>
        <p class="note"><Icon name="archive" size={16} /> {t('setup.storage.backups')}</p>
      {/if}
    {:else if step === 'sites'}
      <p class="lead">{t('setup.sites.lead')}</p>
      <SitesCard welcome />
    {:else if step === 'look'}
      <p class="lead">{t('setup.look.lead')}</p>
      <div class="look" role="radiogroup" aria-label={t('settings.grid.tiles')}>
        {#each [{ id: 'masonry' as const, label: t('settings.grid.masonry'), hint: t('setup.look.masonry') }, { id: 'squares' as const, label: t('settings.grid.squares'), hint: t('setup.look.squares') }] as g (g.id)}
          <button class="look-pick" class:on={settings.gridLayout === g.id} role="radio" aria-checked={settings.gridLayout === g.id} onclick={() => (settings.gridLayout = g.id)}>
            <span class="mini {g.id}" aria-hidden="true"
              >{#each [[0.75, 1.3, 1], [0.6, 1.1, 0.85], [1.4, 0.7, 1]] as col, c (c)}<span class="col">{#each col as r, i (i)}<span class="tile" style:--r={r}></span>{/each}</span>{/each}</span>
            <span class="look-text"><strong>{g.label}</strong><span class="hint">{g.hint}</span></span>
          </button>
        {/each}
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={settings.startMuted} />
        <span class="check-text"><strong>{t('settings.videos.muted')}</strong><span class="hint">{t('settings.videos.mutedHint')}</span></span>
      </label>
      {#if fineMouse}
        <label class="check">
          <input type="checkbox" bind:checked={settings.hoverPreview} />
          <span class="check-text"><strong>{t('settings.videos.hover')}</strong><span class="hint">{t('settings.videos.hoverHint')}</span></span>
        </label>
      {/if}
      <p class="note"><Icon name="settings" size={16} /> {t('setup.look.later')}</p>
    {:else if step === 'rules'}
      <p class="lead">{t('setup.rules.lead')}</p>
      <BlacklistPanel suggest={commonBlacklist} />
      <section class="card limits">
        <div class="card-head"><h2>{t('settings.section.rules.label')}</h2><p class="hint">{t('setup.rules.limits')}</p></div>
        <div class="limit-fields"><DownloadRules /></div>
      </section>
    {:else if step === 'app'}
      <InstallHelp />
    {:else if step === 'notify'}
      {#if pushSupport() === 'needs-home-screen'}
        <p class="lead">{t('setup.notify.homeScreen')}</p>
      {:else}
        <p class="lead">{t('setup.notify.lead')}</p>
        <NotificationsCard />
      {/if}
    {:else if step === 'library'}
      {#if !usable.length}
        <p class="lead">{t('setup.library.noSites')}</p>
      {:else}
        <p class="lead">{t('setup.library.lead')}</p>
        <div class="start">
          <ul class="ways">
            <li><Icon name="download" size={18} /><span><strong>{t('setup.library.onceTitle')}</strong><span class="hint">{t('setup.library.onceHint')}</span></span></li>
            <li><Icon name="clock" size={18} /><span><strong>{t('setup.library.subTitle')}</strong><span class="hint">{t('setup.library.subHint')}</span></span></li>
          </ul>
          <button class="go-start" onclick={startDownload}><Icon name="download" size={18} /> {t('setup.library.start')}</button>
        </div>
      {/if}
    {:else}
      <p class="lead">
        {#if started === 'sub'}{t('setup.done.sub')}{:else if started}{t('setup.done.once')}{:else}{t('setup.done.ready')}{/if}
      </p>
      <ul class="good">
        <li>
          <Icon name="archive" size={18} />
          <div><strong>{t('setup.done.backups')}</strong><span class="hint">{t('setup.done.backupsHint')}</span></div>
        </li>
        <li>
          <Icon name="block" size={18} />
          <div>
            <strong>{t('setup.done.changeTitle')}</strong><span class="hint">{t('setup.done.changeHint')}</span>
            <button class="link" onclick={() => finish('blacklist')}>{t('setup.done.blacklistButton')}</button>
          </div>
        </li>
        <li>
          <Icon name="info" size={18} />
          <div><strong>{t('setup.done.tourTitle')}</strong><span class="hint">{t('setup.done.tourHint')}</span></div>
        </li>
      </ul>
    {/if}
    </div>
    {/key}
    </div>

    <footer>
      {#if step === 'done'}
        <span></span>
        <button onclick={() => finish(started ? 'downloads' : settings.mediaOff ? 'comics' : 'library')}>{started ? t('setup.seeDownload') : t('setup.openLibrary')}</button>
      {:else}
        {#if at > 0}<button class="quiet" onclick={back} transition:pop><Icon name="left" size={16} /> {t('common.back')}</button>{:else}<span></span>{/if}
        <span class="right">
          {#if step === 'library'}
            <button class="quiet" onclick={next}>{t('setup.skip')}</button>
          {:else}
            <button onclick={next}>{t('setup.continue')}</button>
          {/if}
        </span>
      {/if}
    </footer>
  </div>
</div>

<style>
  .first {
    height: 100%;
    overflow-y: auto;
    padding: calc(var(--safe-top) + 12px) calc(var(--safe-right) + 16px) calc(var(--bottom-room) + 24px) calc(var(--safe-left) + 16px);
  }
  .inner {
    max-width: 600px;
    margin: 0 auto;
    display: grid;
    gap: 14px;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: var(--control-h);
  }
  .skip-all {
    color: var(--subtext);
  }
  .progress {
    display: flex;
    gap: 6px;
  }
  .progress span {
    flex: 1;
    height: 4px;
    border-radius: 2px;
    background: var(--surface0);
    transition: background-color 0.35s ease;
  }
  @media (prefers-reduced-motion: reduce) {
    .progress span {
      transition: none;
    }
  }
  .progress span.done {
    background: color-mix(in srgb, var(--mauve) 55%, var(--surface0));
  }
  .progress span.now {
    background: var(--mauve);
  }
  .check-update {
    display: inline-flex;
    gap: 4px;
    align-items: center;
    font-weight: 500;
  }
  .check-update .link {
    min-height: 0;
    padding: 0;
    border: 0;
    background: none;
    color: var(--mauve);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .check-update .link:disabled {
    color: var(--subtext);
    cursor: default;
  }
  .update {
    display: flex;
    gap: 12px;
    align-items: center;
    padding: 12px 14px;
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--mauve) 12%, var(--mantle));
    color: var(--mauve);
  }
  .update .field {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    color: var(--text);
  }
  .update .hint {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .update button {
    flex: none;
  }
  .count {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    margin: 4px 0 -8px;
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
  }
  h1 {
    margin: 0;
    font-size: 26px;
    font-weight: 700;
    letter-spacing: -0.01em;
  }
  .lead {
    margin: 0;
    color: var(--subtext);
  }
  .hint {
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .first :global(code) {
    font-size: 0.92em;
    overflow-wrap: anywhere;
  }
  .checks,
  .good {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 2px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    border-radius: 14px;
    overflow: hidden;
  }
  .checks li,
  .good li {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 12px 14px;
  }
  .checks li + li,
  .good li + li {
    border-top: 1px solid var(--surface0);
  }
  .checks li > :global(svg),
  .good li > :global(svg) {
    flex: none;
    margin-top: 1px;
    color: var(--green);
  }
  .good li > :global(svg) {
    color: var(--mauve);
  }
  .checks li.warn > :global(svg) {
    color: var(--yellow);
  }
  .checks li.soft > :global(svg) {
    color: var(--subtext);
  }
  .checks div,
  .good div {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .checks :global(code) {
    color: var(--text);
  }
  .fix {
    color: var(--yellow);
    font-size: var(--font-small);
  }
  .problem {
    display: flex;
    gap: 8px;
    align-items: center;
    color: var(--yellow);
    margin: 0;
  }
  .note {
    display: flex;
    gap: 8px;
    align-items: center;
    margin: 0;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .check {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding: 12px 14px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    border-radius: 14px;
    cursor: pointer;
  }
  .check input {
    margin-top: 3px;
  }
  .check-text {
    display: grid;
    gap: 2px;
  }
  .look {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .look-pick {
    display: grid;
    gap: 10px;
    padding: 12px;
    text-align: left;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    border-radius: 14px;
    color: var(--text);
    transition: border-color 0.2s, background-color 0.2s;
  }
  .look-pick.on {
    border-color: var(--mauve);
    background: color-mix(in srgb, var(--mauve) 10%, var(--mantle));
  }
  .look-text {
    display: grid;
    gap: 2px;
  }
  /* A little picture of each kind of grid: three columns of tiles - squares, or each in its own shape (plain
     columns, not CSS multi-column: WebKit's made the card count as never settling). */
  .mini {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 4px;
    height: 96px;
    overflow: hidden;
  }
  .mini .col {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .mini .tile {
    flex: none;
    border-radius: 4px;
    background: color-mix(in srgb, var(--mauve) 30%, var(--surface0));
  }
  .mini.squares .tile {
    aspect-ratio: 1;
  }
  .mini.masonry .tile {
    height: calc(var(--r) * 34px);
  }
  .limits {
    display: grid;
    gap: 12px;
  }
  .limit-fields {
    display: grid;
    gap: 12px;
  }
  @media (min-width: 640px) {
    .limit-fields {
      grid-template-columns: 1fr 1fr;
    }
  }
  .start {
    display: grid;
    gap: 14px;
    padding: 16px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    border-radius: 14px;
  }
  .ways {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 12px;
  }
  .ways li {
    display: flex;
    gap: 10px;
    align-items: flex-start;
  }
  .ways li > :global(svg) {
    flex: none;
    margin-top: 2px;
    color: var(--mauve);
  }
  .ways li > span {
    display: grid;
    gap: 2px;
  }
  .go-start {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }
  /* The steps: the new one slides in over the old one as it fades (one grid cell, no jump in height). */
  .stage {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
  }
  .stage > .step {
    grid-area: 1 / 1;
    display: grid;
    gap: 14px;
    align-content: start;
    min-width: 0;
  }
  .link {
    justify-self: start;
    margin-top: 4px;
    padding: 0;
    min-height: 0;
    background: none;
    border: none;
    color: var(--mauve);
    font-weight: 600;
  }
  footer {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
  }
  footer .right {
    display: flex;
    gap: 8px;
  }
  footer button {
    min-width: 120px;
  }
  /* What you'll use: two big choices, ticked when on. */
  .sections {
    display: grid;
    gap: 10px;
    margin-top: 6px;
  }
  .section {
    display: flex;
    align-items: center;
    gap: 14px;
    width: 100%;
    min-height: 72px;
    padding: 12px 14px;
    border-radius: 14px;
    border: 1px solid var(--surface1);
    background: var(--mantle);
    color: var(--text);
    text-align: left;
    font-weight: 400;
    white-space: normal; /* (its words wrap: a phone is narrow) */
  }
  .section.on {
    border-color: var(--mauve);
    background: color-mix(in srgb, var(--mauve) 10%, var(--mantle));
  }
  .section-icon {
    flex: none;
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: 12px;
    background: color-mix(in srgb, var(--mauve) 16%, transparent);
    color: var(--mauve);
  }
  .section-text {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 2px;
  }
  .tick {
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border-radius: 50%;
    border: 1.5px solid var(--overlay0);
    color: var(--crust);
  }
  .section.on .tick {
    background: var(--mauve);
    border-color: var(--mauve);
  }
</style>
