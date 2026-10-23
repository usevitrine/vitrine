<script lang="ts">
  import { backClose } from './lib/backClose';
  import { settingsSections, tagSections } from './lib/settingsSections';
  import { live } from './lib/live.svelte';
  import { onMount, untrack } from 'svelte';
  import { fade, slide } from 'svelte/transition';
  import { reveal, pop, glide } from './lib/motion';
  import { api, setLoggedOutHandler, type Session, toPlainSignIn } from './lib/api';
  import { router, type Screen } from './lib/router.svelte';
  import Icon from './components/Icon.svelte';
  import Logo from './components/Logo.svelte';
  import { topbar } from './lib/topbar.svelte';
  import { namespaces } from './lib/namespaces.svelte';
  import { settings } from './lib/settings.svelte';
  import { i18n, t, num } from './lib/i18n.svelte';
  import { push } from './lib/push.svelte';
  import { sites } from './lib/sites.svelte';
  import Toasts from './components/Toasts.svelte';
  import Bell from './components/Bell.svelte';
  import { notices } from './lib/notices.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import PromptDialog from './components/PromptDialog.svelte';
  import NewDownloadDialog from './components/NewDownloadDialog.svelte';
  import { newDownloadDialog } from './lib/newDownload.svelte';
  import Login from './screens/Login.svelte';
  import Library from './screens/Library.svelte';
  import Comics from './screens/Comics.svelte';
  import { dupes } from './lib/dupes.svelte';
  import Tour from './components/Tour.svelte';
  import { tour } from './lib/tour.svelte';
  import type { Component } from 'svelte';

  // The screens used now and then load when first opened (a smaller first load: Downloads alone is the biggest part of
  // the app), and all of them a moment after the start - so opening one later is instant, offline too.
  const loaders = {
    downloads: () => import('./screens/Downloads.svelte'),
    browse: () => import('./screens/Browse.svelte'),
    tags: () => import('./screens/Tags.svelte'),
    duplicates: () => import('./screens/Duplicates.svelte'),
    rankings: () => import('./screens/Rankings.svelte'),
    settings: () => import('./screens/Settings.svelte'),
    firstSteps: () => import('./screens/FirstSteps.svelte'),
  };
  type Lazy = keyof typeof loaders;
  let lazy = $state<Partial<Record<Lazy, Component<any>>>>({});
  const asked = new Set<Lazy>();
  function want(name: Lazy) {
    if (asked.has(name)) return;
    asked.add(name);
    loaders[name]()
      .then((m) => (lazy[name] = m.default))
      .catch(() => asked.delete(name)); // (offline and not kept: asked again next time)
  }
  $effect(() => {
    const s = router.screen;
    if (s in loaders) want(s as Lazy);
    if (settings.welcome && settings.welcome !== 'finished') want('firstSteps');
  });
  $effect(() => {
    const t = setTimeout(() => (Object.keys(loaders) as Lazy[]).forEach(want), 3000);
    return () => clearTimeout(t);
  });

  let session = $state<Session | null>(null);
  let failed = $state('');
  // The navigation lives in a drawer behind the menu button in the top bar (on
  // every device, like hydrus-web), so the screens get the whole width.
  let drawer = $state(false);

  // The last known session (this device): the app opens on it at once and asks
  // the server in the background (logged out meanwhile: the login screen).
  const known = (() => {
    try {
      return JSON.parse(localStorage.getItem('vitrine.session') ?? 'null') as Session | null;
    } catch {
      return null;
    }
  })();
  onMount(async () => {
    setLoggedOutHandler(() => {
      if (session?.anonymous || settings.anonymousLogin) return toPlainSignIn();
      if (session) session = { ...session, logged_in: false };
      try {
        localStorage.removeItem('vitrine.session');
      } catch {
        /* nothing kept */
      }
    });
    if (known?.logged_in) session = known;
    try {
      const s = await api.session();
      if (!s.logged_in && !s.setup_needed && s.anonymous) return toPlainSignIn();
      session = s;
      i18n.setServerDefault(s.language);
      try {
        if (s.logged_in) localStorage.setItem('vitrine.session', JSON.stringify(s));
        else localStorage.removeItem('vitrine.session');
      } catch {
        /* not kept: asked again next time */
      }
    } catch (e) {
      if (!session) failed = (e as Error).message; // offline with a known session: carry on
    }
  });

  // Once logged in: the shared settings (the library's first search uses the
  // default search and order - waited for, but never more than a moment) and
  // the namespace list for tag colors.
  let ready = $state(false);
  // The first account was just made: the first steps start once the settings are read.
  let freshSetup = $state(false);
  $effect(() => {
    if (!ready || !freshSetup) return;
    freshSetup = false;
    settings.welcome = 'sections';
  });
  // Only logging in or out re-runs this (not a fresh copy of the same session from the server).
  const loggedIn = $derived(!!session?.logged_in);
  // The page's language (screen readers, hyphenation), and this device's notifications follow it.
  $effect(() => {
    document.documentElement.lang = i18n.lang;
  });
  $effect(() => {
    const lang = i18n.lang;
    if (ready) untrack(() => push.syncLanguage(lang));
  });
  $effect(() => {
    if (!loggedIn) {
      ready = false;
      live.stop();
      return;
    }
    live.start();
    notices.catchUp(); // (what the downloader did while no page was open)
    namespaces.load();
    sites.load();
    // Settings read before on this device: start with them, refresh in the background.
    if (settings.syncedBefore) ready = true;
    Promise.race([settings.sync(), new Promise((r) => setTimeout(r, 1500))]).then(() => (ready = true));
  });

  const tabs: {
    screen: Screen;
    label: string;
    icon: 'library' | 'book' | 'download' | 'tag' | 'copy' | 'drop' | 'trash' | 'settings';
    subs?: { sub: string; label: string; screen?: Screen }[]; // sub-pages, listed under it (another screen's, too)
    first?: string; // the sub-page the screen opens on from the menu
    id?: string; // (an entry of its own over another screen's pages: Trash)
    on?: () => boolean; // it's the current entry (else: its screen is showing)
  }[] = $derived([
    // Everything (/) and Favorites (/favorites, only favorites; see Library.svelte).
    { screen: 'library', label: t('menu.library'), icon: 'library', subs: [{ sub: '', label: t('menu.everything') }, { sub: 'favorites', label: t('menu.favorites') }], on: () => router.screen === 'library' && router.sub !== 'trash' },
    // (their own page: never in the library) All comics and Favorites (/comics/favorites).
    { screen: 'comics', label: t('comics.title'), icon: 'book', subs: [{ sub: '', label: t('comics.everything') }, { sub: 'favorites', label: t('menu.favorites') }, { sub: 'browse', label: t('browse.title'), screen: 'browse' as const }], on: () => router.screen === 'comics' && router.sub !== 'trash' },
    { screen: 'downloads', label: t('stats.downloads'), icon: 'download' },
    { screen: 'rankings', label: t('rankings.title'), icon: 'drop' }, // (only with the "I came" button on)
    { screen: 'duplicates', label: t('menu.duplicates'), icon: 'copy' },
    // The trash: deleted files and comics (Media's at /trash, Comics' at /comics/trash; lib/trash).
    { id: 'trash', screen: 'library', first: 'trash', label: t('menu.trash'), icon: 'trash', subs: [{ sub: 'trash', label: t('menu.library') }, { sub: 'trash', label: t('comics.title'), screen: 'comics' as const }], on: () => router.sub === 'trash' && (router.screen === 'library' || router.screen === 'comics') },
    {
      screen: 'tags',
      label: t('tags.title'),
      icon: 'tag',
      first: 'rules', // Tags opens on its rules
      subs: tagSections.map((x) => ({ sub: x.id, label: x.label })),
    },
    { screen: 'settings', label: t('settings.title'), icon: 'settings', subs: settingsSections.map((x) => ({ sub: x.id, label: x.label })) },
  ]);

  // The menu shows how many duplicates wait (asked each time it opens).
  $effect(() => {
    if (drawer) dupes.refreshCount();
  });

  // Menu items with sub-pages fold (folded to begin with); what's open is
  // remembered on this device.
  let unfolded = $state<string[]>(
    (() => {
      try {
        return JSON.parse(localStorage.getItem('vitrine.menuOpen') ?? '[]') as string[];
      } catch {
        return [];
      }
    })(),
  );
  function toggleFold(screen: string) {
    unfolded = unfolded.includes(screen) ? unfolded.filter((x) => x !== screen) : [...unfolded, screen];
    try {
      localStorage.setItem('vitrine.menuOpen', JSON.stringify(unfolded));
    } catch {
      /* not remembered */
    }
  }

  let comicsOpened = $state(false);
  let browseOpened = $state(false);
  // Safe for work: the I-came page isn't there - its address opens Media.
  $effect(() => {
    if (router.screen === 'rankings' && settings.sfwMode) router.go('library');
  });
  // A section that's off (Settings > Viewing > Content): its pages open the other one.
  $effect(() => {
    if (settings.mediaOff && (router.screen === 'library' || router.screen === 'duplicates')) router.replace('comics', router.sub === 'trash' ? 'trash' : '');
    else if (settings.comicsOff && (router.screen === 'comics' || router.screen === 'browse')) router.replace('library', router.sub === 'trash' ? 'trash' : '');
  });
  /** A menu entry that's there: the I-came page with its button on; a section's entries while it's on. */
  function shown(tab: (typeof tabs)[number]) {
    if (tab.screen === 'rankings') return settings.came;
    if (tab.id === 'trash') return true;
    if (settings.mediaOff && (tab.screen === 'library' || tab.screen === 'duplicates')) return false;
    if (settings.comicsOff && tab.screen === 'comics') return false;
    return true;
  }
  $effect(() => {
    if (router.screen === 'comics') comicsOpened = true;
    if (router.screen === 'browse') browseOpened = true;
  });

  function goTo(s: Screen, sub = '') {
    router.go(s, sub);
    drawer = false;
  }

  // ---- The top bar slides away while the page scrolls down and comes back when
  // it scrolls up (or reaches the top) - the library's grid and the other
  // screens' pages, not lists inside menus.
  let barAway = $state(false);
  let barH = $state(0);
  let barEl = $state<HTMLElement>();
  // How far it slides: all but the strip under the status bar (its top padding).
  let barHide = $derived.by(() => {
    void barH;
    return barEl ? Math.max(0, barH - (parseFloat(getComputedStyle(barEl).paddingTop) || 0)) : 0;
  });
  let barOff = $derived(barAway && !drawer ? barHide : 0);
  $effect(() => {
    topbar.h = barH;
    topbar.off = barOff;
  });
  const lastTop = new WeakMap<Element, number>();
  // Just after it moved: a scroll then (e.g. the browser settling) isn't taken as the user's.
  let quietUntil = 0;
  // While it slides (only then: its height measured at the start mustn't animate).
  let barSliding = $state(false);
  let slideTimer = 0;
  const setAway = (v: boolean) => {
    if (v === barAway) return;
    barSliding = true;
    clearTimeout(slideTimer);
    slideTimer = window.setTimeout(() => (barSliding = false), 300);
    barAway = v;
    quietUntil = performance.now() + 350;
  };
  $effect(() => {
    const onScroll = (e: Event) => {
      const t = e.target as HTMLElement;
      if (!(t instanceof HTMLElement) || !(t.classList.contains('grid-scroller') || t.classList.contains('page'))) return;
      const top = t.scrollTop;
      const prev = lastTop.get(t) ?? top;
      lastTop.set(t, top);
      if (top < 8) return setAway(false); // at the top: always there
      if (performance.now() < quietUntil || performance.now() < Number(t.dataset.quietUntil ?? 0)) return; // (see setAway; the grid moving itself)
      if (top - prev > 6 && top > barH * 2) setAway(true);
      else if (prev - top > 6) setAway(false);
    };
    document.addEventListener('scroll', onScroll, { capture: true, passive: true });
    return () => document.removeEventListener('scroll', onScroll, { capture: true });
  });
  // Another screen: the bar is back.
  $effect(() => {
    void router.screen;
    void router.sub;
    untrack(() => setAway(false));
  });
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && drawer && (drawer = false)} />

{#if failed}
  <p class="center error" transition:reveal>{t('app.unreachable', { error: failed })}</p>
{:else if !session}
  <!-- loading: a blank, themed screen for the split second it takes -->
{:else if !session.logged_in}
  <Login
    setup={session.setup_needed}
    onDone={(s) => {
      if (session?.setup_needed) freshSetup = true; // the first account: the first steps follow
      session = s;
    }} />
{:else if !ready}
  <!-- reading the shared settings: a split second -->
{:else if settings.welcome && settings.welcome !== 'finished'}
  {#if lazy.firstSteps}{@const FirstSteps = lazy.firstSteps}<FirstSteps />{/if}
{:else}
  <div class="shell" class:bar-sliding={barSliding} style:--bar-h="{barH}px" style:--bar-off="{-barOff}px">
    <header class="topbar" class:away={barAway && !drawer} bind:this={barEl} bind:offsetHeight={barH}>
      <button
        class="menu-button"
        aria-label={t('menu.menu')}
        aria-expanded={drawer}
        aria-controls="nav-drawer"
        onclick={() => (drawer = !drawer)}><Icon name="menu" size={22} /></button>
      <button class="home" aria-label={settings.mediaOff ? t('comics.title') : t('menu.library')} onclick={() => goTo(settings.mediaOff ? 'comics' : 'library')}><Logo /></button>
      <span class="spacer"></span>
      {#if router.screen === 'library' && topbar.library}{@render topbar.library()}{/if}
      {#if router.screen === 'comics' && topbar.comics}{@render topbar.comics()}{/if}
      {#if router.screen === 'browse' && topbar.browse}{@render topbar.browse()}{/if}
      <Bell />
    </header>

    {#if drawer}
      <div class="backdrop" role="presentation" onclick={() => (drawer = false)} transition:fade={{ duration: 150 }}></div>
      <nav id="nav-drawer" class="drawer" aria-label={t('menu.main')} {@attach backClose(() => (drawer = false))} transition:glide={{ x: -300, duration: 200, opacity: 1 }}>
        <div class="brand"><Logo size={30} /></div>
        {#each tabs.filter(shown) as tab (tab.id ?? tab.screen)}
          {@const key = tab.id ?? tab.screen}
          {@const open = !!tab.subs && unfolded.includes(key)}
          {@const current = tab.on ? tab.on() : router.screen === tab.screen}
          <div class="tab-row" class:folds={!!tab.subs}>
            <button
              class="tab"
              class:active={current}
              class:lit-below={open && current}
              aria-current={current ? 'page' : undefined}
              onclick={() => (tab.id === 'trash' && settings.mediaOff ? goTo('comics', 'trash') : goTo(tab.screen, tab.first))}>
              <Icon name={tab.icon} size={22} />
              <span id="menu-{key}">{tab.label}</span>
              {#if tab.screen === 'duplicates' && dupes.count}<span class="count-badge" transition:pop>{num(dupes.count)}</span>{/if}
            </button>
            {#if tab.subs}
              <button
                class="fold"
                class:open
                aria-expanded={open}
                aria-label={open ? t('menu.fold') : t('menu.unfold')}
                aria-describedby="menu-{key}"
                title={open ? t('menu.fold') : t('menu.unfold')}
                onclick={() => toggleFold(key)}><Icon name="chevron" size={18} /></button>
            {/if}
          </div>
          {#if open}
            <div class="subs" transition:slide|local={{ duration: 160 }}>
              {#each (tab.subs ?? []).filter((s) => (s.screen ?? tab.screen) !== (settings.mediaOff ? 'library' : '') && !(settings.comicsOff && (s.screen === 'comics' || s.screen === 'browse'))) as s (s.label)}
                <!-- (a sub-entry may be another screen's page: Comics > Browse, Trash > Comics) -->
                {@const own = s.screen}
                {@const here = own ? router.screen === own && router.sub === s.sub : router.screen === tab.screen && router.sub === s.sub}
                <button class="tab sub" class:active={here} aria-current={here ? 'page' : undefined} onclick={() => (own ? goTo(own, s.sub) : goTo(tab.screen, s.sub))}>
                  <span>{s.label}</span>
                </button>
              {/each}
            </div>
          {/if}
        {/each}
      </nav>
    {/if}

    <main class="main">
      <!-- The library stays mounted, so switching screens keeps its scroll position and results. -->
      <div class="screen" hidden={router.screen !== 'library'}><Library /></div>
      <!-- Comics too, once opened (its search, scroll position and grid stay). -->
      {#if comicsOpened}<div class="screen" hidden={router.screen !== 'comics'}><Comics /></div>{/if}
      <!-- Browse too, once opened (its search and results stay). -->
      {#if browseOpened && lazy.browse}{@const Browse = lazy.browse}<div class="screen" hidden={router.screen !== 'browse'}><Browse /></div>{/if}
      {#if router.screen === 'downloads' && lazy.downloads}{@const Downloads = lazy.downloads}<div class="screen"><Downloads /></div>{/if}
      {#if router.screen === 'tags' && lazy.tags}{@const Tags = lazy.tags}<div class="screen"><Tags /></div>{/if}
      {#if router.screen === 'duplicates' && lazy.duplicates}{@const Duplicates = lazy.duplicates}<div class="screen"><Duplicates /></div>{/if}
      {#if router.screen === 'rankings' && !settings.sfwMode && lazy.rankings}{@const Rankings = lazy.rankings}<div class="screen"><Rankings /></div>{/if}
      {#if router.screen === 'settings' && lazy.settings}
        {@const Settings = lazy.settings}
        <div class="screen"><Settings user={session.user ?? ''} onLoggedOut={() => (session = { ...session!, logged_in: false })} /></div>
      {/if}
    </main>
  </div>
  <!-- The tour after the first steps: on the library or Comics, nothing else open over it. -->
  {#if tour.active && !drawer && !newDownloadDialog.current && (router.screen === 'library' || router.screen === 'comics') && !/^\d+$/.test(router.sub)}<Tour />{/if}
{/if}
<Toasts />
<ConfirmDialog />
<PromptDialog />
<!-- A new download (the Library's download button, a tag's menu, the details' picked tags). -->
{#if newDownloadDialog.current}
  {#key newDownloadDialog.current}
    {@const d = newDownloadDialog.current}
    <NewDownloadDialog
      query={d.query}
      fromTags={d.fromTags}
      fallback={d.fallback}
      presetSite={d.site}
      presetText={d.text}
      label={d.label}
      ondone={d.ondone}
      onclose={() => (newDownloadDialog.current = null)} />
  {/key}
{/if}

<style>
  .center {
    padding: 24px;
    text-align: center;
  }
  .shell {
    height: 100%;
    position: relative;
    display: flex;
    flex-direction: column;
    /* landscape iPhone: keep clear of the notch */
    padding-left: var(--safe-left);
    padding-right: var(--safe-right);
  }
  .main {
    flex: 1;
    min-height: 0;
    position: relative;
  }
  .screen {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }

  /* The top bar: menu button, logo, and the shown screen's controls. It
     starts below the status bar (safe area + the iOS 27 blur allowance). */
  .topbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
    /* Just the menu button's height (a little more with a mouse), under the status bar. */
    padding: var(--safe-top) 10px 0;
    min-height: calc(max(var(--control-h), 40px) + var(--safe-top));
    background: var(--mantle);
    border-bottom: 1px solid var(--surface0);
    /* Over the screens (they keep room for it): sliding away moves nothing under it. */
    position: absolute;
    top: var(--bar-off, 0px);
    left: var(--safe-left);
    right: var(--safe-right);
    z-index: 6;
    /* Scrolled down: slid up out of sight - all but the strip under the status bar
       (content never shows under the status bar). By its top, not a transform: a transform
       would make it the frame of the menus inside it that are fixed to the screen. */
  }
  .bar-sliding .topbar {
    transition: top 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  @media (prefers-reduced-motion: reduce) {
    .bar-sliding .topbar {
      transition: none;
    }
  }
  @media (min-width: 900px) {
    .topbar {
      padding-left: 16px;
      padding-right: 16px;
    }
  }
  .spacer {
    flex: 1;
  }
  /* Phones: just the mark (the V) - the bar is crowded with the screen's controls, downloads and the bell. */
  @media (max-width: 639px) {
    .home :global(.name) {
      display: none;
    }
  }
  @media (max-width: 379px) {
    .topbar {
      gap: 2px;
      padding-left: 6px;
      padding-right: 6px;
    }
  }
  .menu-button {
    width: var(--control-h);
    height: var(--control-h);
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: var(--radius);
    background: none;
    color: var(--text);
    border: none;
  }
  .home {
    padding: 0 6px;
    height: var(--control-h);
    background: none;
    border: none;
  }
  @media (hover: hover) {
    .menu-button:hover,
    .home:hover {
      background: var(--surface0);
    }
  }

  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 34;
    background: rgb(17 17 27 / 0.55);
  }
  .drawer {
    position: fixed;
    z-index: 35;
    top: 0;
    bottom: 0;
    left: 0;
    width: min(280px, 82vw);
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: calc(14px + var(--safe-top)) 8px calc(14px + var(--bottom-room)) calc(8px + var(--safe-left) + var(--safe-corner, 0px));
    background: var(--mantle);
    border-right: 1px solid var(--surface0);
    box-shadow: 12px 0 32px rgb(0 0 0 / 0.45);
    /* Taller than the window (a short screen, a landscape phone): it scrolls. */
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: var(--surface1) transparent;
  }
  .drawer > * {
    flex: none; /* (items keep their size; the list scrolls instead of squashing them) */
  }
  .brand {
    padding: 4px 8px 18px;
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: calc(var(--control-h) + 4px);
    padding: 0 12px;
    background: transparent;
    color: var(--subtext);
    font-weight: 500;
    border-radius: var(--radius);
    justify-content: flex-start;
    /* The hover moves at once from item to item (no fade): side by side, the one
       fading out and the one fading in flickered between each other. */
    transition: none;
  }
  .count-badge {
    margin-left: auto;
    min-width: 22px;
    padding: 1px 7px;
    border-radius: 11px;
    background: color-mix(in srgb, var(--mauve) 18%, transparent);
    color: var(--mauve);
    font-size: 12px;
    font-weight: 700;
    text-align: center;
    font-variant-numeric: tabular-nums;
  }
  .tab.active {
    background: var(--surface0);
    color: var(--mauve);
  }
  /* A screen with sub-pages, unfolded: the screen is highlighted by its sub-page instead. */
  .tab.active.lit-below {
    background: none;
  }
  /* Its fold arrow: inside the row at the right (the row stays one target to open the screen). */
  .tab-row {
    position: relative;
    display: flex;
  }
  .tab-row > .tab {
    flex: 1;
    min-width: 0;
  }
  .tab-row.folds > .tab {
    padding-right: calc(var(--control-h) + 4px);
  }
  .fold {
    position: absolute;
    right: 2px;
    top: 50%;
    width: var(--control-h);
    height: var(--control-h);
    margin-top: calc(var(--control-h) / -2);
    padding: 0;
    display: grid;
    place-items: center;
    background: transparent;
    border: none;
    border-radius: var(--radius);
    color: var(--subtext);
    transition: none;
  }
  .fold :global(svg) {
    transform: rotate(-90deg); /* folded: pointing right */
    transition: transform 0.16s ease;
  }
  .fold.open :global(svg) {
    transform: none; /* unfolded: pointing down */
  }
  @media (prefers-reduced-motion: reduce) {
    .fold :global(svg) {
      transition: none;
    }
  }
  .subs {
    display: flex;
    flex-direction: column;
    gap: inherit;
  }
  /* Sub-pages: indented under their screen, on a thin rail (the current one lit). */
  .tab.sub {
    position: relative;
    min-height: calc(var(--control-h) - 4px);
    margin-left: 22px;
    padding-left: 24px;
    font-size: var(--font-small);
    border-radius: 0 var(--radius) var(--radius) 0;
  }
  .tab.sub::before {
    content: "";
    position: absolute;
    left: 0;
    top: -4px; /* across the gaps between items: one unbroken line */
    bottom: -4px;
    width: 2px;
    background: var(--surface0);
  }
  /* ... ending with the last sub-page (not running on to the next item: they looked joined). */
  .tab.sub:last-child::before {
    bottom: 0;
  }
  .tab.sub.active::before {
    z-index: 1;
    top: 0;
    bottom: 0;
    background: var(--mauve);
  }
  @media (hover: hover) {
    /* surface0 at half: a plain color (not a color-mix with transparent). */
    .tab:hover:not(.active),
    .fold:hover {
      background: rgb(49 50 68 / 0.5);
      color: var(--text);
    }
  }
</style>
