<script lang="ts">
  // Settings > Sites: each site plugin, its credentials (written to the
  // server, never shown again - only whether each is set) and a connection
  // test.
  import { onMount, untrack } from 'svelte';
  import { slide, fade } from 'svelte/transition';
  import { flip } from 'svelte/animate';
  import { cubicOut } from 'svelte/easing';
  import { api, type SiteInfo, type SiteTest, type PluginInfo, type Installed } from '../lib/api';
  import { sites as siteNames } from '../lib/sites.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import Icon from './Icon.svelte';
  import SiteIcon from './SiteIcon.svelte';
  import ActionMenu, { type MenuItem } from './ActionMenu.svelte';
  import { settings } from '../lib/settings.svelte';
  import { depthChoice, depthLabel } from '../lib/depth';
  import { t } from '../lib/i18n.svelte';

  // welcome: the first-start steps' version - the sites to add open from the start, no Cloudflare.
  let { welcome = false }: { welcome?: boolean } = $props();

  let sites = $state<SiteInfo[]>([]);
  let error = $state('');
  let loaded = $state(false);
  // Per site: what's typed (never pre-filled), busy flags, last test result.
  let typed = $state<Record<string, Record<string, string>>>({});
  let reveal = $state<Record<string, boolean>>({});
  let busy = $state<Record<string, 'save' | 'test' | 'remove' | 'off' | undefined>>({});
  let tests = $state<Record<string, SiteTest | undefined>>({});
  // Plugins: the list Vitrine offers (to install new sites and update installed ones).
  let plugins = $state<PluginInfo[]>([]);
  let pluginsError = $state('');
  let adding = $state(untrack(() => welcome)); // the Add site panel is open (from the start in the welcome)
  let showAdult = $state(false); // adult sites in the list to add (hidden until asked for)
  let installing = $state<string | null>(null); // a plugin ID, or 'file'
  let find = $state(''); // Add site's search
  let kind = $state(''); // Add site's filter: a plugin kind, '' = all
  let showAll = $state(false); // past the first few sites
  let fileInput = $state<HTMLInputElement>();
  let editing = $state<Record<string, boolean>>({}); // saved credentials being replaced

  /** What stays paused if a site goes ("2 subscriptions and 1 download"). */
  function usage(s: SiteInfo): string {
    const parts = [];
    if (s.subscriptions) parts.push(t('sites.usage.subscriptions', { n: s.subscriptions }));
    if (s.downloads) parts.push(t('sites.usage.downloads', { n: s.downloads }));
    return parts.join(t('sites.usage.and'));
  }

  // Off: nothing runs for the site - its downloads and subscription checks
  // wait, and it isn't offered for new downloads; on again, it carries on.
  async function setOff(s: SiteInfo, off: boolean) {
    busy[s.id] = 'off';
    try {
      sites = (await api.setSiteOff(s.id, off)).sites;
      const waiting = usage(s);
      toasts.show(off ? (waiting ? t('sites.offWaiting', { site: s.site, waiting }) : t('sites.off', { site: s.site })) : t('sites.on', { site: s.site }), { cat: 'sites', kind: 'info' });
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      busy[s.id] = undefined;
    }
  }

  async function removeSite(s: SiteInfo) {
    const waiting = usage(s);
    const ok = await confirmAction(
      s.overrides
        ? { title: t('sites.builtInTitle', { site: s.site }), body: t('sites.builtInBody'), action: t('sites.goBack'), icon: 'undo' }
        : {
            title: t('sites.removeTitle', { site: s.site }),
            body: s.built_in ? t('sites.removeBuiltIn') : waiting ? t('sites.removeBodyWaiting', { waiting }) : t('sites.removeBody'),
            action: t('sites.remove'),
          },
    );
    if (!ok) return;
    busy[s.id] = 'remove';
    try {
      const res = await api.removePlugin(s.id);
      sites = res.sites;
      toasts.show(res.removed.restored ? t('sites.restored', { site: s.site }) : t('sites.removed', { site: s.site }), { cat: 'sites', kind: 'removed' });
      siteNames.load();
      loadPlugins();
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      busy[s.id] = undefined;
    }
  }
  // Add site: the sites not installed, A to Z (SFW mode: adult sites never offered - installed ones keep working).
  // A long list shows its first few with "Show all"; a search for it and filters by kind once there are kinds to pick.
  const FIRST = 8;
  const KINDS = ['booru', 'gallery', 'social'];
  const offerable = $derived(plugins.filter((p) => !p.installed && !(p.nsfw && settings.sfwMode)).sort((a, b) => a.site.localeCompare(b.site)));
  const available = $derived(offerable.filter((p) => showAdult || !p.nsfw));
  const adultCount = $derived(offerable.filter((p) => p.nsfw).length);
  const kinds = $derived(KINDS.filter((k) => available.some((p) => p.kind === k)));
  const pickedKind = $derived(kinds.length > 1 && kinds.includes(kind) ? kind : ''); // (a kind that's gone: All)
  const searchable = $derived(offerable.length > FIRST); // the same with or without adult sites
  const matches = $derived.by(() => {
    const q = searchable ? find.trim().toLowerCase() : '';
    return available.filter((p) => (!pickedKind || p.kind === pickedKind) && (!q || [p.site, p.name, p.description ?? ''].some((s) => s.toLowerCase().includes(q))));
  });
  const searching = $derived(searchable && !!find.trim());
  const shown = $derived(searching || showAll ? matches : matches.slice(0, FIRST));
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  const ms = (n: number) => (reduced() ? 0 : n);
  const updateFor = (id: string) => plugins.find((p) => p.id === id && p.update);

  async function loadPlugins(refresh = false) {
    try {
      const res = await api.plugins(refresh);
      plugins = res.plugins;
      pluginsError = res.error ?? '';
    } catch (e) {
      pluginsError = (e as Error).message;
    }
  }

  function done(res: { installed: Installed; sites: SiteInfo[] }) {
    sites = res.sites;
    const i = res.installed;
    toasts.show(i.new ? t('sites.installed', { site: i.version ? `${i.site} ${i.version}` : i.site }) : i.version ? t('sites.updated', { site: i.site, version: i.version }) : t('sites.updatedNew', { site: i.site }), { cat: 'sites' });
    siteNames.load(); // the site's address everywhere else
    loadPlugins();
  }

  async function install(p: PluginInfo) {
    installing = p.id;
    try {
      done(await api.installPlugin(p.id));
      if (!p.installed && !welcome) adding = false;
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      installing = null;
    }
  }

  async function installFile(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = ''; // the same file can be picked again
    if (!file) return;
    if (file.size > 512 * 1024) {
      toasts.error(t('sites.tooBig'));
      return;
    }
    installing = 'file';
    try {
      done(await api.uploadPlugin(await file.text()));
      adding = false;
    } catch (err) {
      toasts.error((err as Error).message);
    } finally {
      installing = null;
    }
  }

  // ---- FlareSolverr: passes Cloudflare's "Checking your browser" pages.
  let flareSaved = $state(''); // the address in use ("" = none)
  let flareURL = $state('');
  let flareBusy = $state(false);
  let flareState = $state<{ ok: boolean; text: string } | null>(null);
  // "For sites ... {link} must reach them ..." around the link.
  const flareHint = $derived(t('sites.flare.hint').split('{link}'));
  async function saveFlare(e: SubmitEvent) {
    e.preventDefault();
    flareBusy = true;
    try {
      const res = await api.setFlaresolverr(flareURL.trim());
      flareSaved = flareURL = res.url;
      if (!res.url) flareState = null;
      else if (res.error) flareState = { ok: false, text: res.error };
      else flareState = { ok: true, text: t('sites.flare.connected', { version: res.version ?? '' }) };
    } catch (err) {
      flareState = { ok: false, text: (err as Error).message };
    } finally {
      flareBusy = false;
    }
  }

  onMount(async () => {
    loadPlugins();
    api
      .flaresolverr()
      .then((r) => (flareSaved = flareURL = r.url))
      .catch(() => {});
    try {
      sites = (await api.sites()).sites;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loaded = true;
    }
  });

  /** api_key -> "API key", user_id -> "User ID". */
  function fieldLabel(f: string): string {
    if (f === 'username' || f === 'password' || f === 'api_key' || f === 'user_id' || f === 'ipb_member_id' || f === 'ipb_pass_hash')
      return t(`sites.field.${f}`);
    const words = f.split(/[_-]+/).map((w) => (w === 'api' || w === 'id' || w === 'url' ? w.toUpperCase() : w));
    const s = words.join(' ');
    return s[0].toUpperCase() + s.slice(1);
  }

  /** A box's hint: what to type (a sign-in) or paste (a key). */
  function placeholder(f: string): string {
    if (f === 'username') return t('sites.placeholder.username');
    if (f === 'password') return t('sites.placeholder.password');
    if (f.startsWith('ipb_')) return t('sites.placeholder.cookie');
    return t('sites.placeholder.paste', { field: lowerFirst(fieldLabel(f)) });
  }

  /** "User ID" -> "user ID", "API key" stays (an acronym). */
  const lowerFirst = (w: string) => (/^[A-Z]{2}/.test(w) ? w : w[0].toLowerCase() + w.slice(1));

  // (A gallery site's login: a user name and password, or its two cookies - either pair.)
  const allSet = (s: SiteInfo) =>
    s.fields.includes('ipb_member_id')
      ? (!!s.set.username && !!s.set.password) || (!!s.set.ipb_member_id && !!s.set.ipb_pass_hash)
      : s.fields.every((f) => s.set[f]);
  const anySet = (s: SiteInfo) => s.fields.some((f) => s.set[f]);
  const hasTyped = (s: SiteInfo) => s.fields.some((f) => (typed[s.id]?.[f] ?? '').trim());

  function status(s: SiteInfo): { text: string; cls: string } {
    if (s.off) return { text: t('notify.status.off'), cls: 'off' };
    const res = tests[s.id];
    if (res) return res.ok ? { text: t('sites.status.connected'), cls: 'ok' } : { text: t('sites.status.notConnected'), cls: 'bad' };
    if (s.required && !allSet(s)) return { text: t('sites.status.needsAccount'), cls: 'warn' };
    return { text: t('sites.status.ready'), cls: 'neutral' }; // saved credentials show in the card itself
  }

  /** A site's ⋯ menu: test, update, its account, remove. */
  function siteItems(s: SiteInfo): MenuItem[] {
    const items: MenuItem[] = [
      s.off
        ? { label: t('sites.switchOn'), icon: 'play', disabled: !!busy[s.id], run: () => setOff(s, false) }
        : { label: t('sites.switchOff'), icon: 'pause', disabled: !!busy[s.id], run: () => setOff(s, true) },
      { label: t('sites.test'), icon: 'retry', disabled: !!busy[s.id], run: () => test(s) },
      {
        label: t('depth.label'),
        icon: 'depth',
        hint: depthLabel(settings.siteCheckDepths[s.id] ?? settings.checkDepth),
        choices: depthChoice(settings.siteCheckDepths[s.id], settings.checkDepth, (n) => setDepth(s.id, n)),
      },
    ];
    const u = updateFor(s.id);
    if (u) items.push({ label: t('sites.updateTo', { version: u.version ?? '' }), icon: 'download', disabled: installing !== null, run: () => install(u) });
    if (s.fields.length && allSet(s)) items.push({ label: t('sites.changeCreds'), icon: 'pencil', run: () => (editing[s.id] = true) });
    if (anySet(s)) items.push({ label: t('sites.removeCreds'), icon: 'close', danger: true, disabled: !!busy[s.id], run: () => remove(s) });
    if (s.removable) {
      items.push({ label: s.overrides ? t('sites.useBuiltIn') : t('sites.removeSite'), icon: 'trash', danger: true, run: () => removeSite(s) });
    }
    return items;
  }

  /** A site's check depth for its subscriptions (n < 0: the one for all). */
  function setDepth(id: string, n: number) {
    const next = { ...settings.siteCheckDepths };
    if (n < 0) delete next[id];
    else next[id] = n;
    settings.siteCheckDepths = next;
  }

  async function test(s: SiteInfo) {
    busy[s.id] = 'test';
    try {
      tests[s.id] = await api.testSite(s.id);
    } catch (e) {
      tests[s.id] = { ok: false, posts: 0, message: (e as Error).message };
    } finally {
      busy[s.id] = undefined;
    }
  }

  async function save(e: SubmitEvent, s: SiteInfo) {
    e.preventDefault();
    const values: Record<string, string> = {};
    for (const f of s.fields) {
      const v = (typed[s.id]?.[f] ?? '').trim();
      if (v) values[f] = v; // empty boxes keep what's saved
    }
    if (!Object.keys(values).length) return;
    busy[s.id] = 'save';
    try {
      sites = (await api.setCredentials(s.id, values)).sites;
      typed[s.id] = {};
      reveal[s.id] = false;
      editing[s.id] = false;
      busy[s.id] = undefined;
      toasts.show(t('sites.credsSaved', { site: s.site }), { cat: 'sites' });
      const fresh = sites.find((x) => x.id === s.id);
      if (fresh && allSet(fresh)) await test(fresh); // check them straight away
    } catch (err) {
      toasts.error((err as Error).message);
      busy[s.id] = undefined;
    }
  }

  async function remove(s: SiteInfo) {
    const ok = await confirmAction({ title: t('sites.removeLoginTitle', { site: s.site }), body: s.required ? t('sites.removeLoginBody') : undefined, action: t('sites.remove') });
    if (!ok) return;
    busy[s.id] = 'remove';
    try {
      sites = (await api.setCredentials(s.id, Object.fromEntries(s.fields.map((f) => [f, ''])))).sites;
      tests[s.id] = undefined;
      editing[s.id] = false;
      toasts.show(t('sites.credsRemoved', { site: s.site }), { cat: 'sites', kind: 'removed' });
    } catch (err) {
      toasts.error((err as Error).message);
    } finally {
      busy[s.id] = undefined;
    }
  }
</script>

<section class="card" data-card="sites">
  {#if !welcome}
    <div class="card-head">
      <h2>{t('settings.section.sites.label')}</h2>
      <p class="hint">{t('sites.hint')}</p>
    </div>
  {/if}

  {#if error}
    <p class="error small">{error}</p>
  {:else if loaded && !sites.length}
    <p class="empty">{t('sites.none')}</p>
  {/if}

  <div class="sites">
    {#each sites as s (s.id)}
      {@const st = status(s)}
      {@const res = tests[s.id]}
      <article class="site" class:off={s.off}>
        <header>
          <div class="title">
            <h3>{s.site}</h3>
            <p class="meta">
              {[s.name !== s.site && s.name !== s.id ? s.name : '', s.version ? `v${s.version}` : '', s.built_in ? t('sites.builtIn') : t('sites.installedTag'), s.tag_types ? t('sites.tagTypes') : ''].filter(Boolean).join(' · ')}
            </p>
          </div>
          <span class="status {st.cls}">{busy[s.id] === 'test' ? t('sites.testing') : st.text}</span>
          <ActionMenu label={t('common.moreFor', { name: s.site })} title={s.site} items={siteItems(s)} />
        </header>
        {#if updateFor(s.id)}
          {@const u = updateFor(s.id)!}
          <div class="update" role="status">
            <span>{s.version ? t('sites.availableHave', { version: u.version ?? '', have: s.version }) : t('sites.available', { version: u.version ?? '' })}</span>
            <button disabled={installing !== null} onclick={() => install(u)} aria-label={t('sites.updateSite', { site: s.site })}>
              {installing === u.id ? t('sites.updating') : t('saved.update')}
            </button>
          </div>
        {/if}

        {#if s.fields.length && allSet(s) && !editing[s.id]}
          <div class="account">
            <Icon name="check" size={16} />
            <span class="grow">{t('sites.credsSavedShort')}</span>
          </div>
        {:else if s.fields.length}
          <form class="creds" onsubmit={(e) => save(e, s)} autocomplete="off">
            {#if s.help}<p class="hint">{s.help}</p>{/if}
            {#each s.fields as f (f)}
              <label class="field">
                <span class="field-label">
                  {fieldLabel(f)}
                  {#if s.set[f]}<span class="saved"><Icon name="check" size={13} /> {t('sites.savedField')}</span>{/if}
                </span>
                <span class="secret">
                  <input
                    type={reveal[s.id] || f === 'username' ? 'text' : 'password'}
                    value={typed[s.id]?.[f] ?? ''}
                    oninput={(e) => (typed[s.id] = { ...typed[s.id], [f]: (e.currentTarget as HTMLInputElement).value })}
                    placeholder={s.set[f] ? t('sites.placeholder.replace') : placeholder(f)}
                    aria-label={t('sites.fieldLabel', { site: s.site, field: fieldLabel(f) })}
                    autocomplete="off"
                    autocapitalize="off"
                    spellcheck="false"
                    data-1p-ignore
                    data-lpignore="true" />
                  {#if f !== 'username'}
                  <button
                    type="button"
                    class="icon eye"
                    aria-label={reveal[s.id] ? t('sites.hideTyped') : t('sites.showTyped')}
                    title={reveal[s.id] ? t('sites.hideTyped') : t('sites.showTyped')}
                    aria-pressed={!!reveal[s.id]}
                    onclick={() => (reveal[s.id] = !reveal[s.id])}><Icon name={reveal[s.id] ? 'eyeoff' : 'eye'} size={18} /></button>
                  {/if}
                </span>
              </label>
            {/each}
            <div class="form-actions">
              {#if editing[s.id]}
                <button
                  type="button"
                  class="quiet"
                  onclick={() => {
                    editing[s.id] = false;
                    typed[s.id] = {};
                  }}>{t('common.cancel')}</button>
              {/if}
              <button type="submit" disabled={!hasTyped(s) || !!busy[s.id]}>
                {busy[s.id] === 'save' ? t('sites.saving') : t('common.save')}
              </button>
            </div>
          </form>
        {/if}
        {#if res}
          <p class="result" class:ok={res.ok} role="status">
            <Icon name={res.ok ? 'check' : 'close'} size={16} />
            <span>{res.message}{#if res.detail}<span class="detail">{res.detail}</span>{/if}</span>
          </p>
        {/if}
      </article>
    {/each}
  </div>

  {#if !welcome}
    <button class="quiet add" aria-expanded={adding} onclick={() => (adding = !adding)}
      ><Icon name={adding ? 'close' : 'plus'} size={16} /> {adding ? t('sites.close') : t('sites.add')}</button>
  {/if}
  {#if adding}
    <div class="adding" role="region" aria-label={t('sites.addRegion')} transition:slide={{ duration: ms(260), easing: cubicOut }}>
      {#snippet adultSwitch()}
        <label class="chip adult" class:on={showAdult}>
          <input type="checkbox" bind:checked={showAdult} />
          <span>{t('sites.showAdult')}</span>
        </label>
      {/snippet}
      {#if searchable}
        <div class="top">
          <label class="find">
            <Icon name="search" size={16} />
            <input
              type="search"
              bind:value={find}
              placeholder={t('sites.search')}
              aria-label={t('sites.search')}
              autocapitalize="off"
              autocomplete="off"
              spellcheck="false" />
          </label>
          {#if adultCount}{@render adultSwitch()}{/if}
        </div>
      {/if}
      {#if kinds.length > 1 || (adultCount && !searchable)}
        <div class="filters">
          {#if kinds.length > 1}
            <div class="kinds" role="radiogroup" aria-label={t('sites.kind')}>
              {#each ['', ...kinds] as k (k)}
                <button class="chip" role="radio" aria-checked={pickedKind === k} onclick={() => (kind = k)}>{t(k ? `sites.kinds.${k}` : 'sites.kinds.all')}</button>
              {/each}
            </div>
          {/if}
          {#if adultCount && !searchable}{@render adultSwitch()}{/if}
        </div>
      {/if}
      {#if shown.length}
        <ul class="available">
          {#each shown as p (p.id)}
            <li animate:flip={{ duration: ms(220), easing: cubicOut }} in:fade={{ duration: ms(180), delay: ms(60) }} out:slide={{ duration: ms(200), easing: cubicOut }}>
              <SiteIcon host={p.site} />
              <div class="title">
                <h3><span class="host">{p.site}</span>{#if p.nsfw}<span class="badge">{t('sites.adult')}</span>{/if}</h3>
                <p class="meta">{[p.name !== p.site ? p.name : '', p.version ? 'v' + p.version : ''].filter(Boolean).join(' · ')}</p>
                {#if p.description}<p class="desc">{p.description}</p>{/if}
              </div>
              <button disabled={installing !== null} onclick={() => install(p)} aria-label={t('sites.installSite', { site: p.site })}>
                {installing === p.id ? t('sites.installing') : t('sites.install')}
              </button>
            </li>
          {/each}
        </ul>
        {#if shown.length < matches.length}
          <button class="quiet more" onclick={() => (showAll = true)}>{t('sites.showAll', { n: matches.length })}</button>
        {/if}
      {:else if searching || pickedKind}
        <p class="hint">{adultCount && !showAdult && offerable.some((p) => p.nsfw && (!pickedKind || p.kind === pickedKind) && [p.site, p.name, p.description ?? ''].some((s) => s.toLowerCase().includes(find.trim().toLowerCase()))) ? t('sites.noMatchAdult') : t('sites.noMatch')}</p>
      {:else if pluginsError}
        <p class="hint">{t('sites.listFailed', { error: pluginsError })}</p>
      {:else if adultCount && !showAdult}
        <p class="hint">{t('sites.allOthers')}</p>
      {:else}
        <p class="hint">{t('sites.all')}</p>
      {/if}
      <div class="from-file">
        <p class="hint">{t('sites.fromFileHint')}</p>
        <button class="quiet" disabled={installing !== null} onclick={() => fileInput?.click()}>
          {installing === 'file' ? t('sites.installing') : t('sites.fromFile')}
        </button>
        <input bind:this={fileInput} type="file" accept=".yaml,.yml,text/yaml,application/x-yaml" hidden onchange={installFile} aria-label={t('sites.pluginFile')} />
      </div>
    </div>
  {/if}


  {#if !welcome}
  <form class="flare" onsubmit={saveFlare}>
    <h3>Cloudflare</h3>
    <p class="hint">
      {flareHint[0]}<a href="https://github.com/FlareSolverr/FlareSolverr" target="_blank" rel="noreferrer">FlareSolverr</a>{flareHint[1]}
    </p>
    <div class="flare-row">
      <input
        bind:value={flareURL}
        placeholder="http://flaresolverr:8191"
        aria-label={t('sites.flare.address')}
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        inputmode="url" />
      <button type="submit" class:quiet={flareURL.trim() === flareSaved} disabled={flareBusy || (!flareURL.trim() && !flareSaved)}>
        {flareBusy ? t('notify.checking') : flareURL.trim() !== flareSaved ? t('common.save') : t('sites.flare.test')}
      </button>
    </div>
    {#if flareState}
      <p class="result" class:ok={flareState.ok} role="status">
        <Icon name={flareState.ok ? 'check' : 'close'} size={16} /><span>{flareState.text}</span>
      </p>
    {:else if !flareSaved}
      <p class="hint small">{t('sites.flare.notSet')}</p>
    {/if}
  </form>
  <!-- A site that keeps failing to answer: its downloads pause by themselves until it does. -->
  <div class="flare">
    <h3>{t('sites.pause.title')}</h3>
    <p class="hint">{t('sites.pause.hint')}</p>
    <div class="pause-row">
      <ActionMenu
        label={t('sites.pause.title')}
        current={settings.pauseAfter > 0 ? t('sites.pause.after', { n: settings.pauseAfter }) : t('sites.pause.never')}
        items={[3, 5, 10, 20, 0].map((n) => ({ label: n > 0 ? t('sites.pause.after', { n }) : t('sites.pause.never'), on: settings.pauseAfter === n, run: () => (settings.pauseAfter = n) }))} />
    </div>
  </div>
  <!-- A subscription check waiting while a big download runs: it goes first (the download carries on after). -->
  <div class="flare">
    <h3>{t('sites.order.title')}</h3>
    <label class="check">
      <input type="checkbox" bind:checked={settings.checksFirst} />
      <span class="field">
        <span class="field-label">{t('sites.order.checksFirst')}</span>
        <span class="hint">{t('sites.order.checksFirstHint')}</span>
      </span>
    </label>
  </div>
  {/if}
</section>

<style>
  .check {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    cursor: pointer;
  }
  .check input {
    margin-top: 2px;
  }
  .add {
    width: 100%;
    border-style: dashed;
    color: var(--subtext);
  }
  .add[aria-expanded='true'] {
    border-style: solid;
  }
  .flare {
    display: grid;
    gap: 10px;
    margin-top: 6px;
    padding-top: 16px;
    border-top: 1px solid var(--surface0);
  }
  .pause-row {
    max-width: 320px;
  }
  .flare h3 {
    margin: 0;
  }
  .flare .hint {
    margin: 0;
  }
  .flare-row {
    display: flex;
    gap: 8px;
  }
  .flare-row input {
    flex: 1;
    min-width: 0;
  }
  .flare-row button {
    flex: none;
    min-width: 84px;
  }
  .account {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--font-small);
    color: var(--green);
  }
  .account .grow {
    color: var(--text);
  }
  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .form-actions button {
    min-width: 96px;
  }
  /* Phones: Save (and Cancel) across the card, easy to reach. */
  @media (max-width: 639px) {
    .form-actions button {
      flex: 1;
    }
  }
  .adding {
    display: grid;
    gap: 12px;
    padding: 14px;
    border-radius: 12px;
    border: 1px dashed var(--surface2);
  }
  /* The search with the adult switch beside it. */
  .top {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .top .chip.adult {
    height: var(--control-h);
  }
  .find {
    flex: 1;
    min-width: 0;
    position: relative;
    display: flex;
    align-items: center;
  }
  .find :global(svg) {
    position: absolute;
    left: 11px;
    color: var(--overlay0);
    pointer-events: none;
  }
  .find input {
    width: 100%;
    padding-left: 34px;
  }
  /* Kinds on the left (the adult switch at the end when there's no search). */
  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .kinds {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .chip {
    flex: none;
    display: inline-flex;
    align-items: center;
    min-height: 0;
    height: calc(var(--control-h) - 6px);
    padding: 0 12px;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
    transition:
      background-color 0.15s,
      color 0.15s,
      box-shadow 0.15s;
  }
  .chip[aria-checked='true'],
  .chip.on {
    background: color-mix(in srgb, var(--mauve) 22%, var(--surface0));
    color: var(--text);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--mauve) 60%, transparent);
  }
  .chip.adult {
    position: relative;
    margin-left: auto;
  }
  .chip.adult.on {
    background: color-mix(in srgb, var(--peach) 18%, var(--surface0));
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--peach) 55%, transparent);
  }
  /* The switch is the chip itself: an unseen checkbox over all of it (keyboards and screen readers too). */
  .chip.adult input {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    opacity: 0;
    cursor: pointer;
  }
  .chip.adult:has(input:focus-visible) {
    outline: 2px solid var(--mauve);
    outline-offset: 2px;
  }
  @media (hover: hover) {
    .chip:hover:not([aria-checked='true']):not(.on) {
      filter: none;
      color: var(--text);
    }
  }
  .available {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    border-radius: 12px;
    border: 1px solid var(--surface0);
    background: var(--crust);
    overflow: hidden;
  }
  .available li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: var(--crust);
  }
  .available li + li {
    border-top: 1px solid var(--surface0);
  }
  .available .title {
    flex: 1;
    min-width: 0;
  }
  /* The address on one line (the odd very long one ends in "…"), its adult badge after it. */
  .available h3 {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .available .host {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .available .badge {
    flex: none;
    margin-left: 0;
  }
  .available .desc {
    margin: 3px 0 0;
    color: var(--subtext);
    font-size: var(--font-small);
    line-height: 1.35;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .available li > button {
    flex: none;
  }
  .more {
    justify-self: center;
  }
  /* Phones: the panel without its own frame - the list has one - so the rows get the width. */
  @media (max-width: 639px) {
    .adding {
      padding: 4px 0 0;
      border: none;
      border-radius: 0;
    }
    .available li {
      gap: 10px;
      padding: 10px;
    }
    .available li > button {
      padding-inline: 14px;
    }
  }
  .badge {
    margin-left: 4px;
    padding: 1px 7px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 600;
    color: var(--peach);
    background: color-mix(in srgb, var(--peach) 14%, transparent);
    vertical-align: middle;
  }
  .from-file {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px 12px;
    padding-top: 10px;
    border-top: 1px solid var(--surface0);
  }
  .from-file .hint {
    margin: 0;
  }
  .confirm {
    display: grid;
    gap: 10px;
    padding: 12px;
    border-radius: 10px;
    font-size: var(--font-small);
    background: color-mix(in srgb, var(--red) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--red) 35%, transparent);
  }
  .danger-solid {
    background: var(--red);
    color: var(--crust);
  }
  .update {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px 12px;
    padding: 9px 12px;
    border-radius: 10px;
    font-size: var(--font-small);
    color: var(--blue);
    background: color-mix(in srgb, var(--blue) 10%, transparent);
  }
  .sites {
    display: grid;
    gap: 10px;
  }
  .site {
    display: grid;
    gap: 12px;
    padding: 14px;
    border-radius: 12px;
    background: var(--crust);
    border: 1px solid var(--surface0);
  }
  header {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }
  header .status {
    margin-top: 1px;
  }
  /* Centered on the status chip (about 22 px tall), reaching into the card's padding. */
  header :global(.more) {
    margin: calc((22px - var(--control-h)) / 2) -8px 0 -2px;
  }
  .title {
    flex: 1;
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--font);
    font-weight: 700;
    overflow-wrap: anywhere; /* a long address wraps instead of running under the status */
  }
  .meta {
    margin: 2px 0 0;
    font-size: var(--font-small);
    color: var(--subtext);
    overflow-wrap: anywhere;
  }
  .status {
    flex: none;
    font-size: var(--font-small);
    font-weight: 600;
    padding: 2px 9px;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--subtext);
  }
  .status.ok {
    color: var(--green);
    background: color-mix(in srgb, var(--green) 14%, transparent);
  }
  .status.bad {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 14%, transparent);
  }
  .status.off {
    color: var(--overlay1);
    background: none;
    box-shadow: inset 0 0 0 1px var(--surface1);
  }
  .site.off h3 {
    color: var(--subtext);
  }
  .status.warn {
    color: var(--yellow);
    background: color-mix(in srgb, var(--yellow) 12%, transparent);
  }
  .creds {
    display: grid;
    gap: 12px;
  }
  .creds .hint {
    margin: 0;
  }
  .field-label {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .saved {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: var(--font-small);
    font-weight: 500;
    color: var(--green);
  }
  .secret {
    position: relative;
    display: block;
  }
  .secret input {
    width: 100%;
    padding-right: 44px;
  }
  /* What's typed in a code font (easy to check a pasted key); the hint in the normal one. */
  .secret input:not(:placeholder-shown) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }
  .eye {
    position: absolute;
    right: 2px;
    top: 50%;
    translate: 0 -50%;
    color: var(--subtext);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .grow {
    flex: 1;
    margin: 0;
  }
  .result {
    display: flex;
    gap: 8px;
    align-items: flex-start;
    margin: 0;
    padding: 9px 12px;
    border-radius: 10px;
    font-size: var(--font-small);
    color: var(--red);
    background: color-mix(in srgb, var(--red) 10%, transparent);
  }
  .result.ok {
    color: var(--green);
    background: color-mix(in srgb, var(--green) 10%, transparent);
  }
  .result :global(svg) {
    flex: none;
    margin-top: 1px;
  }
  .detail {
    display: block;
    margin-top: 3px;
    color: var(--subtext);
    overflow-wrap: anywhere;
  }
</style>
