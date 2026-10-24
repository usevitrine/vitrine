<script lang="ts">
  import { overlayScroll } from '../lib/overlayScroll';
  import { onMount, tick, untrack, type ComponentProps } from 'svelte';
  import { slide } from 'svelte/transition';
  import { live, bytes } from '../lib/live.svelte';
  import { galleryNow as galleryOf, measurable as measurableOf, progress as progressOf, goalOf } from '../lib/downloadProgress';
  import { reveal, moved, glide, nudge } from '../lib/motion';
  import Stack from '../components/Stack.svelte';
  import { api, type DownloadSearch, type RefreshStatus, type Subscription, type SubGroup, type FailedPost, type RetryJob, type SubLook, type SubPic } from '../lib/api';
  import SiteIcon from '../components/SiteIcon.svelte';
  import { subNames } from '../lib/subNames.svelte';
  import { tagColor, splitTag } from '../lib/namespaces.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { settings } from '../lib/settings.svelte';
  import { confirmAction } from '../lib/confirm.svelte';
  import Icon from '../components/Icon.svelte';
  import Dropdown from '../components/Dropdown.svelte';
  import ActionMenu, { type MenuItem } from '../components/ActionMenu.svelte';
  import TagInput from '../components/TagInput.svelte';
  import { router } from '../lib/router.svelte';
  import { depthChoice, depthLabel, siteDepth } from '../lib/depth';
  import { siteLabel, engineLabel, tagQuery, tagsQuery, canOr, sites as siteInfo } from '../lib/sites.svelte';
  import { t, num, time } from '../lib/i18n.svelte';
  import { intervals, DEFAULT_INTERVAL, cap, every } from '../lib/schedule';
  import type { SitePause } from '../lib/api';
  import { siteOn } from '../lib/sections';

  let sites = $state<string[]>([]); // sites to download from (on)
  let sitesOff = $state<string[]>([]); // switched off in Settings: their queues and subscriptions wait
  let sitesPaused = $state<Record<string, SitePause>>({}); // paused by themselves: they kept failing to answer
  async function tryNow(id: string) {
    try {
      await api.trySite(id);
      toasts.show(t('downloads.tryingNow', { site: siteLabel(id) }), { cat: 'downloadsMisc' });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  let searches = $state<DownloadSearch[]>([]);
  let subs = $state<Subscription[]>([]);
  let refreshStatus = $state<RefreshStatus[]>([]);
  let repeat = $state('0'); // seconds between checks; '0' = download once
  let now = $state(Date.now());
  let site = $state('');
  let query = $state('');
  let maxPosts = $state<number | undefined>(undefined);
  let error = $state('');
  let busy = $state(false);
  // Max files stays as typed (empty = every post) whatever the schedule.
  function setRepeat(v: string) {
    repeat = v;
  }

  // Subscriptions list: a filter and an order once there are enough of them
  // (the order is remembered on this device).
  const SUB_TOOLS_FROM = 6;
  type SubSort = 'next' | 'new' | 'files' | 'checked' | 'name';
  const subSortOptions: { value: SubSort; label: string }[] = $derived(
    (['next', 'new', 'files', 'checked', 'name'] as SubSort[]).map((value) => ({ value, label: t(`downloads.subSort.${value}`) })),
  );
  let subFilter = $state('');
  let attentionOnly = $state(false); // only the subscriptions that need a look (the summary's "need a look")
  const filtering = $derived(subFilter.trim() !== '' || attentionOnly);
  let subSort = $state<SubSort>(loadSubSort());
  function loadSubSort(): SubSort {
    try {
      const v = localStorage.getItem('vitrine.subSort');
      return subSortOptions.some((o) => o.value === v) ? (v as SubSort) : 'next';
    } catch {
      return 'next';
    }
  }
  function setSubSort(v: SubSort) {
    subSort = v;
    try {
      localStorage.setItem('vitrine.subSort', v);
    } catch {
      /* not remembered - fine */
    }
  }
  const shownSubs = $derived.by(() => {
    const f = subFilter.trim().toLowerCase();
    let list = f ? subs.filter((x) => x.query.toLowerCase().includes(f) || (x.label ?? '').toLowerCase().includes(f) || siteLabel(x.site).toLowerCase().includes(f)) : [...subs];
    if (attentionOnly) list = list.filter((x) => attentionOf(x));
    const by: Record<SubSort, (a: Subscription, b: Subscription) => number> = {
      // Paused ones last; then the soonest check first.
      next: (a, b) => Number(a.paused) - Number(b.paused) || a.next_check_at - b.next_check_at,
      new: (a, b) => b.last_new - a.last_new || b.downloaded - a.downloaded,
      files: (a, b) => b.downloaded - a.downloaded,
      checked: (a, b) => (b.last_check_at ?? 0) - (a.last_check_at ?? 0),
      name: (a, b) => named(a).localeCompare(named(b)),
    };
    return list.sort((a, b) => by[subSort](a, b) || a.query.localeCompare(b.query));
  });

  // ---- Subscription groups: named per site; the card shows site > group >
  // subscriptions, then the site's subscriptions in no group.
  let groups = $state<SubGroup[]>([]);
  let expanded = $state<number[]>(loadExpanded()); // group IDs opened on this device (groups start folded)
  let formGroup = $state(''); // New download form: '' = no group, a group ID, or 'new'
  let newGroupName = $state('');
  let moving = $state<{ sub: number; name: string } | null>(null); // naming a new group for a subscription
  let renaming = $state<{ id: number; name: string } | null>(null);

  function loadExpanded(): number[] {
    try {
      return JSON.parse(localStorage.getItem('vitrine.openGroups') ?? '[]');
    } catch {
      return [];
    }
  }
  function toggleGroup(id: number, open = !expanded.includes(id)) {
    expanded = open ? [...new Set([...expanded, id])] : expanded.filter((x) => x !== id);
    try {
      localStorage.setItem('vitrine.openGroups', JSON.stringify(expanded));
    } catch {
      /* not remembered - fine */
    }
  }

  // Whole sites fold too (their queue, or their groups and subscriptions);
  // remembered on this device. Keys: "queue:<site>" and "subs:<site>".
  let foldedSites = $state<string[]>(loadFolded());
  function loadFolded(): string[] {
    try {
      return JSON.parse(localStorage.getItem('vitrine.foldedSites') ?? '[]');
    } catch {
      return [];
    }
  }
  // The subscriptions' sites start folded (like their groups: the user), open only where opened on this device.
  let openSubSites = $state<string[]>(loadList('vitrine.openSubSites'));
  function loadList(key: string): string[] {
    try {
      return JSON.parse(localStorage.getItem(key) ?? '[]');
    } catch {
      return [];
    }
  }
  const siteFolded = (key: string) => (key.startsWith('subs:') ? !openSubSites.includes(key) : foldedSites.includes(key));
  function toggleSite(key: string) {
    const flip = (list: string[]) => (list.includes(key) ? list.filter((x) => x !== key) : [...list, key]);
    try {
      if (key.startsWith('subs:')) {
        openSubSites = flip(openSubSites);
        localStorage.setItem('vitrine.openSubSites', JSON.stringify(openSubSites));
      } else {
        foldedSites = flip(foldedSites);
        localStorage.setItem('vitrine.foldedSites', JSON.stringify(foldedSites));
      }
    } catch {
      /* not remembered - fine */
    }
  }

  // The new download's site: a list, or - with many sites on several engines -
  // the engines, each opening a page of its sites (one site alone is listed as itself).
  function siteMenu(list: string[], current: string): MenuItem[] {
    const byName = [...list].sort((a, b) => siteLabel(a).localeCompare(siteLabel(b)));
    const pick = (id: string): MenuItem => ({ label: siteLabel(id), on: id === current, run: () => (site = id) });
    const engines = [...new Set(byName.map((id) => siteInfo.engines[id] ?? ''))];
    if (byName.length <= 8 || engines.length < 2) return byName.map(pick);
    return engines
      .map((e) => ({ e, ids: byName.filter((id) => (siteInfo.engines[id] ?? '') === e) }))
      .sort((a, b) => engineLabel(a.e).localeCompare(engineLabel(b.e)))
      .map(({ e, ids }): MenuItem => {
        if (ids.length === 1) return { ...pick(ids[0]), hint: engineLabel(e) };
        const has = ids.includes(current);
        return {
          label: engineLabel(e),
          on: has,
          hint: has ? siteLabel(current) : t('filterMenu.nSites', { n: ids.length }),
          choices: { heading: engineLabel(e), value: current, options: ids.map((id) => ({ value: id, label: siteLabel(id) })), pick: (v) => (site = v) },
        };
      });
  }

  /** A queued or finished search's ⋯ menu. */
  function searchItems(s: DownloadSearch, finishedRow: boolean): MenuItem[] {
    const items: MenuItem[] = [];
    if (finishedRow) {
      if (s.state === 'failed') items.push({ label: t('downloads.retry'), icon: 'retry', hint: t('downloads.whereStopped'), run: () => act(s, 'resume') });
    } else if (s.state === 'paused') {
      items.push({ label: t('downloads.resume'), icon: 'play', run: () => act(s, 'resume') });
    } else {
      items.push({ label: t('video.pause'), icon: 'pause', run: () => act(s, 'pause') });
    }
    if (!subscribed(s)) items.push({ label: t('downloads.subscribe'), icon: 'bell', hint: every(DEFAULT_INTERVAL), run: () => subscribeSearch(s) });
    // A gallery site's downloads are comics: on the Comics page, not in the library.
    if (siteInfo.engines[s.site] === 'ehentai') items.push({ label: t('downloads.openComics'), icon: 'book', run: () => router.go('comics') });
    items.push(
      s.subscription_id
        ? { label: t('downloads.skipCheck'), icon: 'close', danger: true, run: () => act(s, 'remove') }
        : { label: finishedRow ? t('downloads.removeFromList') : t('sites.remove'), icon: 'close', danger: true, run: () => act(s, 'remove') },
    );
    return items;
  }

  // A line that scrolls sideways (a long search, many counts): soft edges
  // where more is out of view.
  function edgeFades(el: HTMLElement) {
    const update = () => {
      el.classList.toggle('fade-l', el.scrollLeft > 1);
      el.classList.toggle('fade-r', el.scrollLeft + el.clientWidth < el.scrollWidth - 1);
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    el.addEventListener('scroll', update, { passive: true });
    return () => {
      ro.disconnect();
      el.removeEventListener('scroll', update);
    };
  }

  const siteGroups = (siteId: string) => groups.filter((g) => g.site === siteId);
  /** The card's layout: each site with its groups (and their subscriptions), then its other subscriptions. */
  const subTree = $derived.by(() => {
    const ids = new Set([...shownSubs.map((s) => s.site), ...(filtering ? [] : groups.map((g) => g.site))]);
    return [...ids]
      .sort((a, b) => siteLabel(a).localeCompare(siteLabel(b)))
      .map((siteId) => {
        const inSite = shownSubs.filter((s) => s.site === siteId);
        const gs = siteGroups(siteId)
          .map((g) => ({ g, subs: inSite.filter((s) => s.group_id === g.id) }))
          .filter((x) => !filtering || x.subs.length);
        return { site: siteId, groups: gs, loose: inSite.filter((s) => !s.group_id || !groups.some((g) => g.id === s.group_id)) };
      });
  });
  const groupStats = (g: SubGroup) => {
    const members = subs.filter((s) => s.group_id === g.id);
    return {
      count: members.length,
      files: members.reduce((n, s) => n + s.downloaded, 0),
      newLast: members.reduce((n, s) => n + s.last_new, 0),
      allPaused: members.length > 0 && members.every((s) => s.paused),
    };
  };

  async function groupAct(g: SubGroup, action: 'check' | 'pause' | 'resume') {
    try {
      await api.groupAction(g.id, action);
      toasts.show(action === 'check' ? t('downloads.checking', { name: g.name }) : t(action === 'pause' ? 'downloads.paused' : 'downloads.resumed', { name: g.name }), { cat: 'downloadsControl',
        kind: action === 'check' ? 'info' : 'done',
      });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  async function groupInterval(g: SubGroup, v: string) {
    try {
      await api.updateGroup(g.id, { interval: +v });
      toasts.show(t('downloads.groupInterval', { name: g.name, every: every(+v) }), { cat: 'groups' });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  async function groupDepth(g: SubGroup, n: number) {
    try {
      await api.updateGroup(g.id, { check_depth: n });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  async function renameGroup(e: SubmitEvent) {
    e.preventDefault();
    if (!renaming) return;
    try {
      await api.updateGroup(renaming.id, { name: renaming.name });
      renaming = null;
      refresh();
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }
  async function deleteGroup(g: SubGroup) {
    const n = subs.filter((x) => x.group_id === g.id).length;
    const ok = await confirmAction({
      title: t('downloads.deleteGroupTitle', { name: g.name }),
      body: n ? t('downloads.deleteGroupBody', { n }) : undefined,
      action: t('downloads.deleteGroup'),
    });
    if (!ok) return;
    try {
      await api.groupAction(g.id, 'remove');
      toasts.show(t('downloads.groupDeleted', { name: g.name }), { cat: 'groups', kind: 'removed' });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  function groupMenu(g: SubGroup, v: string) {
    if (v === 'rename') renaming = { id: g.id, name: g.name };
    if (v === 'delete') deleteGroup(g);
  }
  async function moveTo(s: Subscription, v: string) {
    if (v === 'new') {
      moving = { sub: s.id, name: '' };
      return;
    }
    try {
      await api.updateSubscription(s.id, { group_id: +v });
      const g = groups.find((x) => x.id === +v);
      toasts.show(g ? t('downloads.moved', { query: named(s), name: g.name }) : t('downloads.noGroupNow', { query: named(s) }), { cat: 'groups' });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  async function moveToNew(e: SubmitEvent, s: Subscription) {
    e.preventDefault();
    if (!moving?.name.trim()) return;
    try {
      const g = await api.addGroup(s.site, moving.name);
      await api.updateSubscription(s.id, { group_id: g.id });
      toasts.show(t('downloads.movedNew', { query: named(s), name: g.name }), { cat: 'groups' });
      moving = null;
      refresh();
    } catch (err) {
      toasts.error((err as Error).message);
    }
  }
  function pickFormGroup(v: string) {
    formGroup = v;
    const g = groups.find((x) => String(x.id) === v);
    if (g?.interval) setRepeat(String(g.interval)); // the group's usual schedule
  }
  // A different site: its own groups.
  $effect(() => {
    site;
    formGroup = '';
  });

  // The New download form starts on the site used last (this device).
  function lastSite(): string {
    try {
      return localStorage.getItem('vitrine.lastSite') ?? '';
    } catch {
      return '';
    }
  }
  function rememberSite(id: string) {
    try {
      localStorage.setItem('vitrine.lastSite', id);
    } catch {
      /* not remembered - fine */
    }
  }

  async function refresh() {
    if (drag) return; // don't move rows under the finger
    try {
      const res = await api.downloads();
      sites = res.sites;
      sitesOff = res.sites_off ?? [];
      sitesPaused = res.sites_paused ?? {};
      if (site && !sites.includes(site)) site = ''; // switched off meanwhile
      if (!site && sites.length) site = sites.includes(lastSite()) ? lastSite() : sites[0];
      searches = res.searches; // each site's queue in order
      subs = res.subscriptions;
      subNames.set(subs); // (a "found by" chip's words in the library)
      groups = res.groups ?? [];
      refreshStatus = res.refresh;
      failedPosts = (await api.failedPosts()).posts;
      retries = res.retries ?? [];
      announce();
      now = lastRefresh = Date.now();
      error = '';
    } catch (e) {
      error = (e as Error).message;
    }
  }

  // Live: the server says when the queue changes; polling only as a fallback
  // (every 2.5 s without the live stream, every 20 s with it).
  let lastRefresh = 0;
  onMount(() => {
    siteInfo.load(); // sites installed meanwhile get their names and search hints
    refresh();
    const off = live.onDownloads(refresh);
    const timer = setInterval(() => {
      if (!live.connected || Date.now() - lastRefresh > 20_000) refresh();
      else now = Date.now();
    }, 2500);
    return () => {
      off();
      clearInterval(timer);
    };
  });

  // A running search's download speed: kept a few seconds between files (the
  // stream has no files in flight while the next post is looked up), so it doesn't flicker.
  let speeds = $state<Record<number, { v: number; at: number }>>({});
  $effect(() => {
    const sp = live.speed;
    const at = Date.now();
    const next = { ...untrack(() => speeds) };
    for (const [id, v] of Object.entries(sp)) if (v > 0) next[+id] = { v, at };
    speeds = next;
  });
  function speedOf(id: number): string {
    const sp = speeds[id];
    return sp && now - sp.at < 4000 ? t('downloads.speed', { size: bytes(sp.v) }) : '';
  }

  // Your own favorites on the site: they can become favorites here too (on unless unticked).
  let ownFavs = $derived(/^fav:me$/i.test(query.trim()));
  // Anyone's favorites: a subscription goes in the site's own Favorites group (no group to pick).
  let favSearch = $derived(/^fav:\S+$|^favorited_by=/i.test(query.trim()) || /[?&]page=favorites(&|&amp;)s=view/.test(query));
  let addToFavs = $state(true);

  async function add(e: SubmitEvent) {
    e.preventDefault();
    if (!query.trim()) return;
    busy = true;
    try {
      if (repeat !== '0') {
        const group = favSearch ? {} : formGroup === 'new' ? { group_name: newGroupName.trim() } : formGroup ? { group_id: +formGroup } : {};
        if (!favSearch && formGroup === 'new' && !newGroupName.trim()) throw new Error(t('downloads.nameGroupFirst'));
        await api.subscribe(site, query.trim(), +repeat, maxPosts ?? 0, group, ownFavs && addToFavs);
        const gName = favSearch ? t('menu.favorites') : formGroup === 'new' ? newGroupName.trim() : groups.find((x) => String(x.id) === formGroup)?.name;
        toasts.show(gName ? t('downloads.subscribedIn', { query: query.trim(), name: gName, every: every(+repeat) }) : t('downloads.subscribed', { query: query.trim(), every: every(+repeat) }), { cat: 'downloadsStarted' });
        if (formGroup === 'new') {
          newGroupName = '';
          formGroup = '';
        }
      } else {
        await api.addDownload(site, query.trim(), maxPosts ?? 0, ownFavs && addToFavs);
        toasts.show(t('downloads.downloading', { query: query.trim(), site: siteLabel(site) }), { cat: 'downloadsStarted', kind: 'info' });
      }
      rememberSite(site);
      query = '';
      refresh();
    } catch (err) {
      toasts.error((err as Error).message);
    } finally {
      busy = false;
    }
  }

  // The queue (running, waiting, paused) grouped by site in queue order;
  // finished ones (done, failed) newest first.
  const active = (s: DownloadSearch) => s.state === 'running' || s.state === 'queued' || s.state === 'paused';
  let queues = $derived.by(() => {
    const m = new Map<string, DownloadSearch[]>();
    for (const s of searches.filter(active)) m.set(s.site, [...(m.get(s.site) ?? []), s]);
    // Subscription checks go first (Settings > Sites > Queue order): shown in the order they run. (A first check is
    // a bulk download: it waits its turn.)
    const check = (x: DownloadSearch) => Number(!!x.subscription_id && !x.first_check);
    const order = (list: DownloadSearch[]) => (settings.checksFirst ? [...list].sort((a, b) => check(b) - check(a)) : list);
    return [...m.entries()].map(([site, list]) => ({ site, list: order(list) }));
  });
  let finished = $derived(
    searches.filter((s) => !active(s)).sort((a, b) => (b.finished_at ?? b.created_at) - (a.finished_at ?? a.created_at)),
  );
  let doneCount = $derived(finished.filter((s) => s.state === 'done').length);
  let failedCount = $derived(finished.filter((s) => s.state === 'failed').length);

  async function act(s: DownloadSearch, action: 'pause' | 'resume' | 'remove') {
    try {
      if (action === 'pause') await api.pauseDownload(s.id);
      if (action === 'resume') await api.resumeDownload(s.id);
      if (action === 'remove') {
        await api.removeDownload(s.id);
        searches = searches.filter((x) => x.id !== s.id);
        toasts.show(t('downloads.removed', { query: named(s) }), { cat: 'downloadsStarted', kind: 'removed' });
      }
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  /** Every failed download tries again, each from where it stopped. */
  // ---- Failed files: posts whose file couldn't be brought in, each tried again on its own.
  let failedPosts = $state<FailedPost[]>([]);
  const postKey = (f: { site: string; post: string }) => f.site + ':' + f.post;
  const outcomes = new Set(['downloaded', 'already', 'deleted', 'skipped', 'blacklisted', 'too big']);
  const outcomeWord = (o: string) => (outcomes.has(o) ? t(`downloads.outcome.${o.replace(' ', '_')}`) : o);

  // ---- Retries: a failed post tried again runs in the background - in its site's queue, with its progress, then
  // its result for a moment (back in Failed files with why, if it failed again).
  let retries = $state<RetryJob[]>([]);
  const RESULT_MS = 6000;
  let retriesShown = $derived(retries.filter((r) => !r.finished || now - r.finished < RESULT_MS));
  let retryingKeys = $derived(new Set(retries.filter((r) => !r.finished).map(postKey)));
  const mine = new Set<string>(); // started from this page: its result gets a notice
  const announced = new Set<string>();
  function announce() {
    for (const r of retries) {
      const k = postKey(r);
      if (!r.finished || !mine.has(k) || announced.has(k + '@' + r.finished)) continue;
      announced.add(k + '@' + r.finished);
      mine.delete(k);
      if (r.outcome === 'failed') toasts.error(r.error ? t('downloads.postFailedWhy', { post: r.post, error: r.error }) : t('downloads.postFailed', { post: r.post }));
      else toasts.show(t('downloads.retriedPost', { post: r.post, outcome: outcomeWord(r.outcome ?? '') }), { cat: 'retryResult', kind: r.outcome === 'downloaded' ? 'done' : 'info' });
    }
  }
  async function retryPost(f: FailedPost) {
    mine.add(postKey(f));
    try {
      await api.retryPost(f.site, f.post);
    } catch (e) {
      mine.delete(postKey(f));
      toasts.error((e as Error).message);
    }
    await refresh();
  }
  /** A retry's file downloading now (its bytes), and how fast (smoothed between progress messages). */
  const retryFile = (r: RetryJob) => live.files.find((x) => x.key === r.site + '/' + r.post);
  let retrySpeed = $state<Record<string, number>>({});
  let lastRead: Record<string, { read: number; at: number }> = {};
  $effect(() => {
    const at = performance.now();
    const next: Record<string, number> = {};
    const seen: typeof lastRead = {};
    for (const x of live.files) {
      if (!x.key) continue;
      const before = lastRead[x.key];
      seen[x.key] = { read: x.read, at };
      if (before && at > before.at && x.read >= before.read) {
        const now = ((x.read - before.read) / (at - before.at)) * 1000;
        next[x.key] = (untrack(() => retrySpeed[x.key!]) ?? now) * 0.6 + now * 0.4;
      }
    }
    lastRead = seen;
    retrySpeed = next;
  });

  // ---- the stacks: each site's queue, finished downloads and failed files fold into a stack (the active one in
  // front); unfolded on a tap.
  let unfolded = $state<Record<string, boolean>>({});
  // (wi: its place among the site's waiting downloads - those reorder; the one running stays in front)
  type QueueEntry = { kind: 'search'; s: DownloadSearch; wi: number } | { kind: 'retry'; r: RetryJob };
  const entryKey = (e: QueueEntry) => (e.kind === 'search' ? 's' + e.s.id : 'r' + postKey(e.r));
  let queueSites = $derived.by(() => {
    const order = [...new Set([...queues.map((q) => q.site), ...retriesShown.map((r) => r.site)])];
    return order.map((site) => {
      const list = queues.find((q) => q.site === site)?.list ?? [];
      const rs = retriesShown.filter((r) => r.site === site);
      const waiting = list.filter((s) => s.state !== 'running');
      const entries: QueueEntry[] = [
        ...list.filter((s) => s.state === 'running').map((s) => ({ kind: 'search' as const, s, wi: -1 })),
        ...rs.map((r) => ({ kind: 'retry' as const, r })),
        ...waiting.map((s, wi) => ({ kind: 'search' as const, s, wi })),
      ];
      return { site, list, waiting, entries };
    });
  });
  /** A list grouped by site, the sites in the order their first item comes. */
  function bySite<X extends { site: string }>(list: X[]): { site: string; list: X[] }[] {
    const m = new Map<string, X[]>();
    for (const x of list) m.set(x.site, [...(m.get(x.site) ?? []), x]);
    return [...m.entries()].map(([site, list]) => ({ site, list }));
  }

  /** "5 min ago", "3 h ago", "2 days ago". */
  function ago(ms: number): string {
    const s = Math.max(0, (Date.now() - ms) / 1000);
    if (s < 3600) return t('downloads.ago.min', { n: Math.max(1, Math.round(s / 60)) });
    if (s < 86400) return t('downloads.ago.h', { n: Math.round(s / 3600) });
    return t('downloads.ago.days', { n: Math.round(s / 86400) });
  }

  async function retryFailed() {
    const failed = finished.filter((s) => s.state === 'failed');
    const results = await Promise.allSettled(failed.map((s) => api.resumeDownload(s.id)));
    const ok = results.filter((r) => r.status === 'fulfilled').length;
    refresh();
    if (ok) toasts.show(t('downloads.retrying', { n: ok }), { cat: 'retrying', kind: 'info' });
    const bad = results.find((r) => r.status === 'rejected');
    if (bad) toasts.error((bad.reason as Error).message);
  }

  async function clearFinished() {
    try {
      const { removed } = await api.clearDownloads();
      searches = searches.filter((s) => s.state !== 'done');
      toasts.show(t('downloads.cleared', { n: removed }), { cat: 'downloadsMisc', kind: 'removed' });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  // ---- Tabs: Queue (with the tag refresh), Subscriptions, History (failed files, finished downloads); the last one
  // looked at is remembered on this device.
  type Tab = 'queue' | 'subs' | 'history';
  let tab = $state<Tab>(loadTab());
  function loadTab(): Tab {
    try {
      const v = localStorage.getItem('vitrine.downloadsTab');
      return v === 'subs' || v === 'history' ? v : 'queue';
    } catch {
      return 'queue';
    }
  }
  const tabOrder: Tab[] = ['queue', 'subs', 'history'];
  let tabDir = 1; // the way the panels slide: to a tab on the right, from the right
  function setTab(v: Tab) {
    if (v === tab) return;
    tabDir = tabOrder.indexOf(v) > tabOrder.indexOf(tab) ? 1 : -1;
    tab = v;
    try {
      localStorage.setItem('vitrine.downloadsTab', v);
    } catch {
      /* not remembered - fine */
    }
  }
  // The panels slide like Sort and filters' pages: the one leaving goes quickly, the new one comes in after it from
  // the side of its tab, both in one grid cell (no jump). Reduced motion: at once.
  const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  const ease = (t: number) => 1 - Math.pow(1 - t, 3);
  const panelIn = (node: Element) => glide(node, { x: reducedMotion() ? 0 : 24 * tabDir, duration: reducedMotion() ? 0 : 300, delay: reducedMotion() ? 0 : 90, easing: ease, opacity: 0 });
  const panelOut = (node: Element) => glide(node, { x: reducedMotion() ? 0 : -16 * tabDir, duration: reducedMotion() ? 0 : 160, easing: (t) => t * t, opacity: 0 });
  // A tab's count: a new number glides to its width (the tab's name, centered with it, moves smoothly too).
  function easeWidth(node: HTMLElement) {
    let w = node.offsetWidth;
    const mo = new MutationObserver(() => {
      const from = w;
      w = node.offsetWidth;
      if (from && from !== w && !reducedMotion()) node.animate([{ width: `${from}px` }, { width: `${w}px` }], { duration: 220, easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)' });
    });
    mo.observe(node, { childList: true, characterData: true, subtree: true });
    return { destroy: () => mo.disconnect() };
  }
  // A picture that has to load fades in (no pop) - one the browser has already shows at once.
  function fadeImg(node: HTMLImageElement) {
    if (node.complete && node.naturalWidth) return;
    node.style.opacity = '0';
    const show = () => {
      node.style.transition = 'opacity 0.25s';
      node.style.opacity = '1';
    };
    node.addEventListener('load', show, { once: true });
    node.addEventListener('error', show, { once: true });
  }

  // The lit tab's pill glides from tab to tab (measured: the tabs are as wide as their names and counts).
  let tabsEl = $state<HTMLElement>();
  let pill = $state<{ x: number; w: number } | null>(null);
  let pillReady = $state(false); // (no glide in from nowhere at the start)
  function placePill() {
    const b = tabsEl?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]');
    if (!b) return;
    pill = { x: b.offsetLeft, w: b.offsetWidth };
    if (!pillReady) requestAnimationFrame(() => (pillReady = true));
  }
  $effect(() => {
    void tab;
    void subs.length;
    void finished.length;
    void failedFileCount;
    void waitingCount;
    void runningCount;
    tick().then(placePill);
  });
  $effect(() => {
    if (!tabsEl) return;
    // (each tab too: a count coming, going or growing - "99+" - widens its tab, and the pill must cover it)
    const ro = new ResizeObserver(() => placePill());
    ro.observe(tabsEl);
    for (const b of tabsEl.querySelectorAll('[role="tab"]')) ro.observe(b);
    return () => ro.disconnect();
  });

  /** The summary's links: to a tab, then (if given) a part of it brought into view. */
  async function goTo(v: Tab, id = '') {
    setTab(v);
    await tick();
    const el = id ? document.getElementById(id) : document.getElementById('dl-tabs');
    el?.scrollIntoView({ block: 'start', behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  }

  // ---- The summary at the top: what's going on, each part a link to where it is.
  let runningCount = $derived(searches.filter((s) => s.state === 'running' && !sitesPaused[s.site]).length + retriesShown.filter((r) => !r.finished).length);
  let waitingCount = $derived(searches.filter((s) => s.state === 'queued' || s.state === 'paused' || (s.state === 'running' && !!sitesPaused[s.site])).length);
  let failedFileCount = $derived(failedPosts.filter((f) => !retryingKeys.has(postKey(f))).length);
  let nextCheckAt = $derived(subs.filter((s) => !s.paused && !s.check && !sitesOff.includes(s.site)).reduce((m, s) => Math.min(m, s.next_check_at), Infinity));
  let tagsDue = $derived(refreshStatus.reduce((n, r) => n + r.due, 0));
  let pausedSiteIds = $derived(Object.keys(sitesPaused).sort((a, b) => siteLabel(a).localeCompare(siteLabel(b))));

  // ---- Subscriptions that need a look: failing checks, or nothing new for months (a dead tag, a renamed artist).
  const QUIET_MS = 90 * 86400_000;
  function attentionOf(s: Subscription): 'failing' | 'quiet' | null {
    if (s.error || s.check?.state === 'failed') return 'failing';
    if (!s.paused && s.checks > 0 && now - s.created_at > QUIET_MS && now - (s.last_found_at ?? 0) > QUIET_MS) return 'quiet';
    return null;
  }
  let attentionCount = $derived(subs.filter((s) => attentionOf(s)).length);
  $effect(() => {
    if (attentionOnly && attentionCount === 0) attentionOnly = false; // (all seen to)
  });

  // ---- Failed files grouped by why (from the error's words): each reason its own stack, Retry all and Dismiss all.
  type Reason = 'notFound' | 'blocked' | 'connection' | 'file' | 'other';
  const reasonOrder: Reason[] = ['notFound', 'blocked', 'connection', 'file', 'other'];
  function reasonOf(error = ''): Reason {
    if (/\b(404|410)\b|not found|no longer|gone from|was deleted|removed/i.test(error)) return 'notFound';
    if (/cloudflare|\b(401|403|429)\b|forbidden|too many|captcha|rate.?limit|denied|login/i.test(error)) return 'blocked';
    if (/timeout|timed out|deadline|connection|reset|refused|\beof\b|tls|no such host|dns|unreachable|\b5\d\d\b|i\/o|network|broken pipe/i.test(error)) return 'connection';
    if (/size|checksum|md5|hash|decode|corrupt|damaged|unsupported|probe|ffmpeg|vips|truncated|empty file/i.test(error)) return 'file';
    return 'other';
  }
  let failedReasons = $derived.by(() => {
    const m = new Map<Reason, FailedPost[]>();
    for (const f of failedPosts) if (!retryingKeys.has(postKey(f))) m.set(reasonOf(f.error), [...(m.get(reasonOf(f.error)) ?? []), f]);
    return reasonOrder.filter((r) => m.has(r)).map((reason) => ({ reason, list: m.get(reason)! }));
  });
  async function retryPosts(list: FailedPost[]) {
    for (const f of list) mine.add(postKey(f));
    const results = await Promise.allSettled(list.map((f) => api.retryPost(f.site, f.post)));
    const ok = results.filter((r) => r.status === 'fulfilled').length;
    if (ok) toasts.show(t('downloads.retryingPosts', { n: ok }), { cat: 'retrying', kind: 'info' });
    await refresh();
  }
  async function dismissPosts(list: FailedPost[]) {
    try {
      const { dismissed } = await api.dismissPosts(list.map((f) => ({ site: f.site, post: f.post })));
      const gone = new Set(list.map(postKey));
      failedPosts = failedPosts.filter((f) => !gone.has(postKey(f)));
      toasts.show(t('downloads.dismissed', { n: dismissed }), { cat: 'downloadsMisc', kind: 'removed' });
    } catch (e) {
      toasts.error((e as Error).message);
    }
    refresh();
  }

  // ---- Finished: today's, then the earlier ones folded away (remembered on this device).
  const startOfToday = $derived(new Date(new Date(now).setHours(0, 0, 0, 0)).getTime());
  let finishedToday = $derived(finished.filter((s) => (s.finished_at ?? s.created_at) >= startOfToday));
  let finishedEarlier = $derived(finished.filter((s) => (s.finished_at ?? s.created_at) < startOfToday));
  let todaySites = $derived(bySite(finishedToday));
  let earlierSites = $derived(bySite(finishedEarlier));
  let earlierOpen = $state(loadEarlier());
  function loadEarlier(): boolean {
    try {
      return localStorage.getItem('vitrine.finishedEarlier') === '1';
    } catch {
      return false;
    }
  }
  function toggleEarlier() {
    earlierOpen = !earlierOpen;
    try {
      localStorage.setItem('vitrine.finishedEarlier', earlierOpen ? '1' : '0');
    } catch {
      /* not remembered - fine */
    }
  }

  // Opened from a file's "Found by": bring that subscription into view, briefly highlighted.
  let flashSub = $state(0);
  // From the empty library's "Start a download": the search box, ready to type.
  let queryInput = $state<HTMLInputElement>();
  let addForm = $state<HTMLFormElement>();
  $effect(() => {
    if (router.focus !== 'new-download' || !queryInput) return;
    router.focus = null;
    queryInput.focus();
  });
  $effect(() => {
    const f = router.focus;
    if (!f?.startsWith('sub:') || !subs.length) return;
    router.focus = null;
    const id = Number(f.slice(4));
    subFilter = ''; // the subscription must be in the list to scroll to it
    attentionOnly = false;
    setTab('subs');
    const it = subs.find((x) => x.id === id);
    if (it && siteFolded('subs:' + it.site)) toggleSite('subs:' + it.site); // its site open (they start folded)
    if (it?.group_id) toggleGroup(it.group_id, true); // and its group
    tick().then(() => {
      document.querySelector(`li[data-sub="${id}"]`)?.scrollIntoView({ block: 'center', behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
      flashSub = id;
      setTimeout(() => (flashSub = 0), 2200);
    });
  });

  // ---- subscriptions: a search checked again on a schedule for new posts.

  const repeatOptions = $derived([{ value: '0', label: t('downloads.once') }, ...intervals.map((v) => ({ value: String(v), label: cap(every(v)) }))]);
  const intervalOptions = $derived(intervals.map((v) => ({ value: String(v), label: cap(every(v)) })));


  /** "5 min", "3 h", "2 d" for a span in ms. */
  function span(ms: number): string {
    const m = Math.round(Math.abs(ms) / 60000);
    if (m < 60) return t('downloads.span.min', { n: Math.max(1, m) });
    const h = Math.round(m / 60);
    return h < 48 ? t('downloads.span.h', { n: h }) : t('downloads.span.d', { n: Math.round(h / 24) });
  }

  // ---- how each subscription looks (2026-10-22, the user: "beautify the subscriptions"): its newest files (a
  // gallery: its comic's cover), how many came since this device last looked, its health, when it's next checked.
  let looks = $state<Record<number, SubLook>>({});
  let freshSince = $state(0); // (the "+N" files: found after this)
  const SEEN_KEY = 'vitrine.subsSeen';
  let looksFor = ''; // (the totals they were asked for: asked again when files come in)
  async function loadLooks(since: number) {
    try {
      const res = await api.subscriptionLooks(since);
      looks = Object.fromEntries(Object.entries(res.looks).map(([k, v]) => [Number(k), v]));
    } catch {
      /* the cards show without them */
    }
  }
  $effect(() => {
    if (tab !== 'subs') return;
    untrack(() => {
      let seen = 0;
      try {
        seen = Number(localStorage.getItem(SEEN_KEY)) || 0;
        localStorage.setItem(SEEN_KEY, String(Date.now())); // (this visit: the next one counts from here)
      } catch {
        /* no "new" counts then */
      }
      freshSince = seen;
      looksFor = '';
    });
  });
  $effect(() => {
    if (tab !== 'subs') return;
    const key = subs.map((x) => `${x.id}:${x.downloaded}`).join(',');
    if (key === looksFor) return;
    looksFor = key;
    loadLooks(freshSince);
  });

  type Health = 'ok' | 'failing' | 'quiet' | 'paused' | 'checking';
  function health(s: Subscription): Health {
    const a = attentionOf(s);
    if (a) return a;
    if (s.check && s.check.state === 'running') return 'checking';
    if (s.paused) return 'paused';
    return 'ok';
  }
  /** How far along to the next check (0-1; the ring). */
  function untilNext(s: Subscription): number {
    if (s.paused || !s.interval) return 0;
    return Math.max(0, Math.min(1, 1 - (s.next_check_at - now) / (s.interval * 1000)));
  }
  type QPart = { text: string; color?: string };
  /** The search as chips: each term, excluded ones marked, a namespace's color where the library knows it; an OR
   *  search one chip with its tags (2026-10-23: they were a chip each, brackets and ~ too) - "( a ~ b )" (rule34's
   *  way, the default), or "~a ~b" (Danbooru, Mastodon: all the ~ terms are one group). */
  function queryChips(q: string): { parts: QPart[]; neg: boolean }[] {
    const part = (w: string): QPart => {
      const body = w.replace(/"/g, '').replace(/\$$/, ''); // (e-hentai's artist:"name$": the name)
      const p = splitTag(body.replace(/ /g, '_')); // (a namespace the library knows: its color, the name alone)
      return p.ns ? { text: body.slice(body.indexOf(':') + 1), color: tagColor(p.ns + ':' + p.name) } : { text: body };
    };
    const words = (q.match(/-?[^\s"]*(?:"[^"]*")?[^\s"]*/g) ?? []).filter((w) => w && w !== '-');
    const out: { parts: QPart[]; neg: boolean }[] = [];
    let tilde: QPart[] | null = null; // (the ~ terms' chip)
    for (let i = 0; i < words.length; i++) {
      const w = words[i];
      if (w === '(') {
        const parts: QPart[] = [];
        for (i++; i < words.length && words[i] !== ')'; i++) if (words[i] !== '~') parts.push(part(words[i]));
        if (parts.length) out.push({ parts, neg: false });
      } else if (w.startsWith('~') && w.length > 1) {
        if (!tilde) out.push({ parts: (tilde = []), neg: false });
        tilde.push(part(w.slice(1)));
      } else {
        const neg = w.startsWith('-') && w.length > 1;
        out.push({ parts: [part(neg ? w.slice(1) : w)], neg });
      }
    }
    return out;
  }
  /** A group's mosaic: the newest file of up to four of its subscriptions (a gallery: its cover). */
  function groupPics(members: Subscription[]): SubPic[] {
    return members
      .map((m) => looks[m.id]?.cover ?? looks[m.id]?.recent?.[0])
      .filter((p): p is SubPic => !!p)
      .slice(0, 4);
  }
  function openFresh(s: Subscription) {
    const l = looks[s.id];
    if (l?.comic) return router.go('comics', String(l.comic));
    const hours = Math.max(1, Math.ceil((Date.now() - freshSince) / 3600_000));
    router.openSearch(`system:found_by=${s.id} system:imported<${hours}h`);
  }

  // ---- phones: a card swiped to the left shows Check now and Pause/Resume behind it
  let swiped = $state<number | null>(null); // (the one open)
  let swipeX = $state(0); // (the one being dragged: how far)
  let dragging = $state<number | null>(null);
  const SWIPE_OPEN = 148;
  function swipe(s: Subscription) {
    return (el: HTMLElement) => {
      let x0 = 0;
      let y0 = 0;
      let base = 0;
      let engaged = false;
      let id = -1;
      const down = (e: PointerEvent) => {
        if (e.pointerType === 'mouse' || (e.target as HTMLElement).closest('button, a, input')) return;
        const chips = (e.target as HTMLElement).closest<HTMLElement>('.qchips');
        if (chips && chips.scrollWidth > chips.clientWidth) return; // (sideways there scrolls the chips)
        id = e.pointerId;
        x0 = e.clientX;
        y0 = e.clientY;
        base = swiped === s.id ? -SWIPE_OPEN : 0;
        engaged = false;
      };
      const move = (e: PointerEvent) => {
        if (e.pointerId !== id) return;
        const dx = e.clientX - x0;
        const dy = e.clientY - y0;
        if (!engaged) {
          if (Math.abs(dy) > 10) return void (id = -1); // (scrolling the page)
          if (Math.abs(dx) < 10) return;
          engaged = true;
          dragging = s.id;
          if (swiped !== s.id) swiped = null;
        }
        swipeX = Math.max(-SWIPE_OPEN - 24, Math.min(0, base + dx));
      };
      const up = (e: PointerEvent) => {
        if (e.pointerId !== id) return;
        id = -1;
        if (!engaged) return;
        swiped = swipeX < -SWIPE_OPEN / 2 ? s.id : null;
        dragging = null;
      };
      el.addEventListener('pointerdown', down);
      el.addEventListener('pointermove', move);
      el.addEventListener('pointerup', up);
      el.addEventListener('pointercancel', up);
      return () => {
        el.removeEventListener('pointerdown', down);
        el.removeEventListener('pointermove', move);
        el.removeEventListener('pointerup', up);
        el.removeEventListener('pointercancel', up);
      };
    };
  }
  // (a press anywhere else closes an open one)
  $effect(() => {
    if (swiped === null) return;
    const close = (e: PointerEvent) => {
      if (!(e.target as HTMLElement).closest(`li[data-sub="${swiped}"]`)) swiped = null;
    };
    document.addEventListener('pointerdown', close, true);
    return () => document.removeEventListener('pointerdown', close, true);
  });
  function swipeAct(s: Subscription, what: 'check' | 'pause' | 'resume') {
    swiped = null;
    subAct(s, what);
  }

  function subState(s: Subscription): DownloadSearch['state'] | null {
    if (s.check) return s.check.state;
    return s.paused ? 'paused' : null;
  }

  function lastCheck(s: Subscription): string {
    if (!s.checks) return t('downloads.notChecked');
    return t('downloads.checkedAgo', { span: span(now - (s.last_check_at ?? now)) });
  }

  function nextCheck(s: Subscription): string {
    if (s.paused) return '';
    if (s.check) return s.check.state === 'failed' ? t('downloads.retryIn', { span: span(s.next_check_at - now) }) : '';
    return s.next_check_at <= now ? t('downloads.nextSoon') : t('downloads.nextIn', { span: span(s.next_check_at - now) });
  }

  /** How a search is named on its buttons (a check: "the check of x"). */
  /** A download's or subscription's name: its label (a gallery's title), else its search. */
  const named = (x: { label?: string; query: string }) => x.label || x.query;
  const nameOf = (x: DownloadSearch) => (x.subscription_id ? t('downloads.checkOf', { query: named(x) }) : named(x));

  // Is this search a subscription's check, or subscribed already?
  const subscribed = (x: DownloadSearch) =>
    !!x.subscription_id || subs.some((s) => s.site === x.site && s.query === x.query.trim());

  async function subscribeSearch(x: DownloadSearch) {
    try {
      await api.subscribeSearch(x.id, DEFAULT_INTERVAL);
      toasts.show(t('downloads.subscribedSearch', { query: x.query, every: every(DEFAULT_INTERVAL) }), { cat: 'downloadsStarted' });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  // Files no longer in the site's favorites (a favorites subscription noticed): delete them, or keep them.
  async function favGone(s: Subscription, how: 'delete' | 'keep') {
    try {
      if (how === 'keep') {
        await api.favGoneKeep(s.id);
        toasts.show(t('downloads.kept', { n: s.fav_gone ?? 0 }), { cat: 'downloadsMisc' });
      } else {
        const ids = (await api.favGone(s.id)).files;
        if (!ids.length) return refresh();
        const thumbs = (await api.files(ids.slice(0, 3))).files.map((f) => f.thumb).filter((t): t is string => !!t);
        const n = ids.length;
        const ok = await confirmAction({
          title: t('selection.deleteTitle', { n }),
          body: t('downloads.favGoneBody', { site: siteLabel(s.site) }),
          action: n === 1 ? t('common.delete') : t('common.deleteN', { n }),
          thumbs,
          undoable: true,
        });
        if (!ok) return;
        const res = await api.favGoneDelete(s.id);
        toasts.show(t('common.deletedFiles', { n: res.deleted }), { cat: 'deleted',
          kind: 'removed',
          action: { label: t('common.undo'), run: () => api.restore(ids).then(() => undefined) },
        });
      }
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  async function subAct(s: Subscription, action: 'check' | 'pause' | 'resume' | 'remove') {
    try {
      if (action === 'check') {
        await api.checkSubscription(s.id);
        toasts.show(t('downloads.checking', { name: named(s) }), { cat: 'downloadsControl', kind: 'info' });
      }
      if (action === 'pause' || action === 'resume') await api.updateSubscription(s.id, { paused: action === 'pause' });
      if (action === 'remove') {
        const ok = await confirmAction({ title: t('downloads.unsubTitle', { query: named(s) }), body: t('downloads.unsubBody'), action: t('downloads.unsubscribe'), icon: 'close' });
        if (!ok) return;
        await api.removeSubscription(s.id);
        subs = subs.filter((x) => x.id !== s.id);
        searches = searches.filter((x) => x.subscription_id !== s.id);
        toasts.show(t('downloads.unsubscribed', { query: named(s) }), { cat: 'downloadsStarted', kind: 'removed' });
      }
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  async function changeInterval(s: Subscription, v: string) {
    try {
      await api.updateSubscription(s.id, { interval: +v });
      toasts.show(t('downloads.interval', { query: named(s), every: every(+v) }), { cat: 'downloadsControl' });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }

  async function changeDepth(s: Subscription, n: number) {
    try {
      await api.updateSubscription(s.id, { check_depth: n });
      refresh();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  /** The depth a subscription gets unless it sets one: its group's, else its site's or the one for all. */
  function inheritedDepth(s: Subscription): number {
    const g = groups.find((g) => g.id === s.group_id);
    return g ? g.depth : siteDepth(s.site);
  }

  // ---- reordering a site's queue: drag the handle, or arrow keys on it.

  let drag = $state<{ site: string; id: number; from: number; to: number; dy: number } | null>(null);
  let rows: { top: number; height: number }[] = [];
  let startY = 0;
  const gap = 8; // (the stack's gap between cards)

  function dragStart(e: PointerEvent, site: string, i: number, id: number) {
    if (e.button !== 0) return;
    const stack = (e.currentTarget as HTMLElement).closest('.queue-site')!;
    rows = [...stack.querySelectorAll<HTMLElement>('[data-wi]')]
      .sort((a, b) => Number(a.dataset.wi) - Number(b.dataset.wi))
      .map((el) => {
        const r = el.getBoundingClientRect();
        return { top: r.top, height: r.height };
      });
    startY = e.clientY;
    drag = { site, id, from: i, to: i, dy: 0 };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function dragMove(e: PointerEvent) {
    if (!drag) return;
    const dy = e.clientY - startY;
    const r = rows[drag.from];
    const center = r.top + r.height / 2 + dy;
    let to = 0;
    for (let k = 0; k < rows.length; k++) if (k !== drag.from && rows[k].top + rows[k].height / 2 < center) to++;
    drag = { ...drag, dy, to };
  }

  function dragEnd() {
    if (!drag) return;
    const { site, from, to } = drag;
    drag = null;
    if (from !== to) move(site, from, to);
  }

  /** How far a row shifts while another row is dragged past it. */
  function shift(site: string, i: number): number {
    if (!drag || drag.site !== site) return 0;
    if (i === drag.from) return drag.dy;
    const h = rows[drag.from].height + gap;
    if (drag.from < i && i <= drag.to) return -h;
    if (drag.to <= i && i < drag.from) return h;
    return 0;
  }

  /** Moves a site's waiting download (from/to: places among the waiting ones; the running one stays in front). */
  async function move(site: string, from: number, to: number) {
    const list = queues.find((q) => q.site === site)?.list ?? [];
    const waiting = list.filter((s) => s.state !== 'running').map((s) => s.id);
    const [id] = waiting.splice(from, 1);
    waiting.splice(to, 0, id);
    const ids = [...list.filter((s) => s.state === 'running').map((s) => s.id), ...waiting];
    // Show the new order at once, then save it.
    const pos = new Map(ids.map((x, k) => [x, k]));
    searches = [...searches].sort((a, b) =>
      a.site === site && b.site === site && pos.has(a.id) && pos.has(b.id) ? pos.get(a.id)! - pos.get(b.id)! : 0,
    );
    try {
      await api.reorderDownloads(site, ids);
    } catch (e) {
      toasts.error((e as Error).message);
    }
    refresh();
  }

  function handleKey(e: KeyboardEvent, site: string, i: number, n: number) {
    if (e.key === 'ArrowUp' && i > 0) move(site, i, i - 1);
    else if (e.key === 'ArrowDown' && i < n - 1) move(site, i, i + 1);
    else return;
    e.preventDefault();
  }

  const stateLabel = (s: DownloadSearch['state']) => t(`downloads.state.${s}`);
  // (a running one on a paused site waits for it: shown as waiting, not downloading)
  const shownState = (s: DownloadSearch): DownloadSearch['state'] => (s.state === 'running' && sitesPaused[s.site] ? 'queued' : s.state);

  /** A gallery site's search (its posts are whole galleries - comics - of many pages). */
  const isGallery = (s: DownloadSearch) => siteInfo.engines[s.site] === 'ehentai';
  /** One gallery's download (its link): counted by page - a search of many, by gallery. */
  const oneGallery = (x: { query: string }) => /\/g\/\d+\/[0-9a-f]+/i.test(x.query);
  // How far a search is (lib/downloadProgress, shared with the top bar's downloads menu).
  const galleryNow = (s: DownloadSearch) => galleryOf(s, live.files);
  const measurable = (s: DownloadSearch) => measurableOf(s, live.files);
  const progress = (s: DownloadSearch) => progressOf(s, live.files, isGallery(s));
</script>

{#snippet siteHead(key: string, siteId: string, count: number, offWhat: string)}
  {@const open = !siteFolded(key) || (key.startsWith('subs:') && subFilter.trim() !== '')}
  <h3 class="site-head">
    <button class="site-fold" class:open aria-expanded={open} aria-label={t(open ? 'downloads.foldName' : 'downloads.unfoldName', { name: siteLabel(siteId) })} onclick={() => toggleSite(key)}>
      <Icon name="right" size={16} />
      <SiteIcon host={siteInfo.hosts[siteId] ?? ''} />
      <span class="site-name">{siteLabel(siteId)}</span>
      <span class="count">{count}</span>
    </button>
    {#if sitesOff.includes(siteId)}<span class="off-chip" title={t('downloads.offTitle', { what: offWhat })} transition:nudge>{t('notify.status.off')}</span>{/if}
    {@render pausedChip(siteId)}
  </h3>
{/snippet}

<!-- A site paused by itself (it kept failing to answer): a chip by its name. -->
{#snippet pausedChip(siteId: string)}
  {#if sitesPaused[siteId]}<span class="paused-chip" title={t('downloads.pausedTitle')} transition:nudge>{t('downloads.paused')}</span>{/if}
{/snippet}

<!-- ... and at the top of the page: which site, why, when it tries again, and Try now. -->
{#snippet pausedLine(siteId: string)}
  {@const p = sitesPaused[siteId]}
  {#if p}
    <div class="paused-line" role="status" transition:reveal>
      <Icon name="alert" size={16} />
      <span class="paused-text"
        ><span><b>{siteLabel(siteId)}</b> {t('downloads.pausedLine', { time: time(new Date(p.until)) })}</span><span class="paused-why">{p.reason}</span></span>
      <button class="quiet" onclick={() => tryNow(siteId)}>{t('downloads.tryNow')}</button>
    </div>
  {/if}
{/snippet}

<!-- A count in the summary: a link to where those are. -->
{#snippet sumItem(n: number, label: string, go: () => void, cls = '')}
  <button class="sum-item {cls}" onclick={go}><b>{num(n)}</b> {label}</button>
{/snippet}

<!-- A site's stack heading: its name and how many; with more than one, it unfolds the stack too. -->
{#snippet stackHead(key: string, siteId: string, count: number, folds: boolean, offWhat: string)}
  {@const open = !!unfolded[key]}
  <h3 class="site-head">
    {#if folds}
      <button class="site-fold" class:open aria-expanded={open} aria-label={t(open ? 'downloads.foldName' : 'downloads.unfoldName', { name: siteLabel(siteId) })} onclick={() => (unfolded[key] = !open)}>
        <Icon name="right" size={16} />
        <span class="site-name">{siteLabel(siteId)}</span>
        <span class="count">{count}</span>
      </button>
    {:else}
      <span class="site-label"><span class="site-name">{siteLabel(siteId)}</span></span>
    {/if}
    {#if offWhat && sitesOff.includes(siteId)}<span class="off-chip" title={t('downloads.offTitle', { what: offWhat })} transition:nudge>{t('notify.status.off')}</span>{/if}
    {@render pausedChip(siteId)}
  </h3>
{/snippet}

<!-- A failed post tried again: its file's progress (bytes, speed), then its result for a moment. -->
{#snippet retryCard(r: RetryJob)}
  {@const file = retryFile(r)}
  {@const done = !!r.finished}
  {@const ok = done && r.outcome !== 'failed'}
  {@const speed = retrySpeed[r.site + '/' + r.post]}
  <div class="row">
    <div class="main">
      <div class="head">
        <span class="query one-line" {@attach edgeFades}><span>{t('downloads.post', { post: r.post })}</span></span>
        <span class="state {done ? (ok ? 'done' : 'failed') : 'running'}">{done ? (ok ? outcomeWord(r.outcome ?? '') : t('downloads.state.failed')) : t('downloads.state.retrying')}</span>
      </div>
      {#if !done && !(file && file.total > 0)}
        <div class="bar striped" role="progressbar" aria-label={t('downloads.noEstimate')}></div>
      {:else}
        <div class="bar"><div class="fill {done ? (ok ? 'done' : 'failed') : 'running'}" style:width="{done ? 100 : ((file?.read ?? 0) / Math.max(1, file?.total ?? 1)) * 100}%"></div></div>
      {/if}
      <div class="line-stats">
        <div class="counts" {@attach edgeFades}>
          <span class="stat lead"><Icon name="retry" size={12} /> {t('downloads.retryKind')}</span>
          {#if file && !done}<span class="stat">{file.total > 0 ? t('downloads.sizeOf', { read: bytes(file.read), total: bytes(file.total) }) : bytes(file.read)}</span>{/if}
          {#if r.query}<span class="stat quiet">{r.query}</span>{/if}
        </div>
        {#if !done && speed}<span class="stat speed">{t('downloads.speed', { size: bytes(speed) })}</span>{/if}
      </div>
      {#if done && r.error}<p class="error small" transition:reveal>{r.error}</p>{/if}
    </div>
  </div>
{/snippet}

{#snippet stat(n: number | string, label: string, cls = '')}
  <span class="stat {cls}"><b>{typeof n === 'number' ? num(n) : n}</b> {label}</span>
{/snippet}

<!-- The lead count: the posts (galleries) gone through, out of the goal - the limit picked (the site's count when
     that's smaller), else the site's count; none known: just the count (the bar is striped). -->
{#snippet goalCount(s: DownloadSearch, galleries: boolean)}
  {@const goal = goalOf(s)}
  {@const words = goal
    ? t(galleries ? 'downloads.stat.galleriesOf' : 'downloads.stat.postsOf', { done: num(s.handled), n: goal })
    : t(galleries ? 'downloads.stat.galleries' : 'downloads.stat.posts', { n: s.handled })}
  <span class="stat lead" title={goal ? words : undefined}><b>{num(s.handled)}</b>{#if goal}<span class="of">/{num(goal)}</span>{/if}<span class="vh">{goal ? words : ' ' + words}</span></span>
{/snippet}

<!-- A kind of post, as an icon and its count (its name when pointed at, and for screen readers). -->
{#snippet iconStat(n: number, icon: ComponentProps<typeof Icon>['name'], label: string, cls = '')}
  <span class="stat icon-stat {cls}" title="{num(n)} {label}"><Icon name={icon} size={14} /><b>{num(n)}</b><span class="vh"> {label}</span></span>
{/snippet}


<!-- Progress: how far through the estimate; without one (no total from the site,
     no file limit) a running search shows a moving striped bar. -->
{#snippet bar(s: DownloadSearch)}
  {#if shownState(s) === 'running' && !measurable(s)}
    <div class="bar striped" role="progressbar" aria-label={t('downloads.noEstimate')}></div>
  {:else}
    <div class="bar"><div class="fill {shownState(s)}" style:width="{progress(s) * 100}%"></div></div>
  {/if}
{/snippet}

{#snippet searchInfo(s: DownloadSearch)}
  {@const gal = isGallery(s)}
  {@const g = s.state === 'running' && gal ? galleryNow(s) : null}
  <div class="head">
    <span class="query one-line" title={s.query || t('push.everything')} {@attach edgeFades}><span>{named(s) || t('push.everything')}</span></span>
    <span class="state {shownState(s)}">{stateLabel(shownState(s))}</span>
  </div>
  {@render bar(s)}
  <!-- One line: the counts (scrolling sideways when they don't fit), the speed at the end. -->
  <div class="line-stats">
    <div class="counts" {@attach edgeFades}>
      {#if gal && oneGallery(s)}
        <!-- One gallery: counted by page. -->
        {#if g}<span class="stat lead"><b>{num(g.done)}</b> {t('downloads.stat.ofPages', { n: g.pages })}</span>
        {:else if s.pages}{@render stat(s.pages, t('downloads.stat.newPages', { n: s.pages }), 'lead good')}
        {:else if s.already}<span class="stat">{t('downloads.stat.hadAll')}</span>
        {:else if s.state === 'queued'}<span class="stat quiet">{t('downloads.notStarted')}</span>{/if}
      {:else if gal}
        <!-- A search of many galleries: counted by gallery (the one downloading now: which page). -->
        {#if s.handled || s.site_total > 1}
          {@render goalCount(s, true)}
        {:else if s.state === 'queued'}<span class="stat quiet">{t('downloads.notStarted')}</span>{/if}
        {#if g}<span class="stat quiet">{t('downloads.stat.pageOf', { done: num(g.done), pages: num(g.pages) })}</span>{/if}
      {:else if s.handled || goalOf(s)}
        {@render goalCount(s, false)}
      {:else if s.state === 'queued'}
        <span class="stat quiet">{t('downloads.notStarted')}</span>
      {/if}
      {#if !(gal && oneGallery(s))}
        {#if s.downloaded}{@render iconStat(s.downloaded, 'sparkles', t('downloads.stat.new'), 'good')}{/if}
        {#if s.already}{@render iconStat(s.already, 'check', t('downloads.stat.had'))}{/if}
      {/if}
      {#if s.deleted}{@render iconStat(s.deleted, 'trash', t('downloads.stat.deletedBefore'))}{/if}
      {#if s.blacklisted}{@render iconStat(s.blacklisted, 'eyeoff', t('downloads.stat.blacklisted'), 'bad')}{/if}
      {#if s.skipped}{@render iconStat(s.skipped, 'filter', t('downloads.stat.skipped'))}{/if}
      {#if s.failed}{@render iconStat(s.failed, 'alert', t('downloads.stat.failed'), 'bad')}{/if}
    </div>
    {#if s.state === 'running' && speedOf(s.id)}<span class="stat speed" title={t('downloads.speedTitle')}>{speedOf(s.id)}</span>{/if}
  </div>
  {#if s.error}<p class="error small" transition:reveal>{s.error}</p>{/if}
  {#if s.state === 'failed' && !s.subscription_id && s.retry_at}
    <p class="retry small" transition:reveal>
      <Icon name="retry" size={13} /> {s.retry_at <= now ? t('downloads.tryingNow') : t('downloads.tryingIn', { span: span(s.retry_at - now) })}{(s.attempts ?? 0) > 1 ? ' ' + t('downloads.attempt', { n: (s.attempts ?? 0) + 1 }) : ''}
    </p>
  {:else if s.state === 'failed' && !s.subscription_id && (s.attempts ?? 0) > 1}
    <p class="retry small" transition:reveal>{t('downloads.gaveUp', { n: s.attempts ?? 0 })}</p>
  {/if}
{/snippet}

{#snippet searchActs(s: DownloadSearch, finishedRow: boolean)}
  <ActionMenu label={s.subscription_id ? t('common.moreFor', { name: nameOf(s) }) : t('downloads.moreForDownload', { query: named(s) || t('push.everything') })} title={s.query || t('push.everything')} items={searchItems(s, finishedRow)} />
{/snippet}


{#snippet subRow(s: Subscription)}
  {@const st = subState(s)}
  {@const h = health(s)}
  {@const look = looks[s.id]}
  {@const fresh = look?.fresh ?? 0}
  {@const gallery = siteInfo.engines[s.site] === 'ehentai'}
  {@const gNow = s.check && gallery ? galleryNow(s.check) : null}
  <li data-sub={s.id} class:flash={flashSub === s.id} class:swiping={dragging === s.id} {@attach swipe(s)} transition:reveal>
    <!-- Phones: behind the card, what a swipe to the left shows. -->
    <div class="swipe-acts" aria-hidden={swiped !== s.id} inert={swiped !== s.id}>
      <button class="swipe-act" onclick={() => swipeAct(s, 'check')}><Icon name="retry" size={18} />{t('downloads.checkNow')}</button>
      {#if s.paused}
        <button class="swipe-act" onclick={() => swipeAct(s, 'resume')}><Icon name="play" size={18} />{t('downloads.resume')}</button>
      {:else}
        <button class="swipe-act" onclick={() => swipeAct(s, 'pause')}><Icon name="pause" size={18} />{t('video.pause')}</button>
      {/if}
    </div>
    <div class="card-body" style:translate={dragging === s.id ? `${swipeX}px 0` : swiped === s.id ? `-${SWIPE_OPEN}px 0` : undefined}>
    <div class="row">
      <div class="main">
        <div class="head">
          <!-- Its health at a glance (working, checking, paused, failing, quiet for months) in the middle of a ring
               filling up to the next check (turning while it checks). -->
          <span class="status" title={t(`downloads.health.${h}`) + (s.paused ? '' : ' · ' + cap(nextCheck(s) || lastCheck(s)))}>
            {#if !s.paused}
              <span class="ring" class:checking={st === 'running'} role="img" aria-label={nextCheck(s) ? t('downloads.nextRing', { when: nextCheck(s) }) : lastCheck(s)}>
                <svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="10" cy="10" r="8" class="track" /><circle cx="10" cy="10" r="8" class="fill" style:stroke-dashoffset={50.27 * (1 - (st === 'running' ? 0.25 : untilNext(s)))} /></svg>
              </span>
            {/if}
            <span class="health {h}" role="img" aria-label={t(`downloads.health.${h}`)}></span>
          </span>
          {#if s.label}
            <span class="query" title={s.query}>{s.label}</span>
          {:else}
            <!-- The search as chips (excluded terms marked, a namespace's color). -->
            <span class="query qchips" title={s.query} {@attach edgeFades}
              >{#each queryChips(s.query) as c, k (k)}{#if k}{' '}{/if}{#if c.parts.length > 1}<span class="qchip any"
                  >{#each c.parts as p, j (j)}{#if j}<span class="or">{t('searchBar.or')}</span>{/if}<span style:color={p.color}>{p.text}</span>{/each}</span
                >{:else}<span class="qchip" class:neg={c.neg} style:color={c.parts[0].color}>{c.parts[0].text}</span>{/if}{:else}<span class="qchip">{t('push.everything')}</span>{/each}</span>
          {/if}
          {#if st === 'paused' || st === 'failed'}<span class="state {st}" transition:nudge>{stateLabel(st)}</span>{/if}
          {#if fresh}
            <button class="fresh" onclick={() => openFresh(s)} title={t('downloads.freshTitle', { n: fresh })} aria-label={t('downloads.freshTitle', { n: fresh })}>+{num(fresh)}</button>
          {/if}
        </div>
        {#if s.check && st !== 'paused'}
          {@render bar(s.check)}
        {/if}
        {#if look?.cover}
          <!-- A gallery: its comic's cover (it opens the comic). -->
          <button class="pics cover-pic" onclick={() => router.go('comics', String(look.comic))} aria-label={t('downloads.openComic')} title={t('downloads.openComic')}>
            <img src={look.cover.thumb} alt="" loading="lazy" use:fadeImg />
          </button>
        {:else if look?.recent?.length}
          <!-- The newest files it found. -->
          <div class="pics" role="img" aria-label={t('downloads.newestFiles')}>
            {#each look.recent as p (p.id)}<span class="pic"><img src={p.thumb} alt="" loading="lazy" use:fadeImg /></span>{/each}
          </div>
        {/if}
        <!-- One line of facts: how often, how many, when it last found something (zeros left out). -->
        <div class="facts">
          {#if gallery && oneGallery(s) && gNow}
            <!-- One gallery: its pages as they come. -->
            <span class="stat"><b>{num(gNow.done)}</b> {t('downloads.stat.ofPages', { n: gNow.pages })}</span>
          {:else if s.check && s.check.handled && !(gallery && oneGallery(s))}
            {@render stat(s.check.handled, t(gallery ? 'downloads.stat.galleriesThisCheck' : 'downloads.stat.postsThisCheck', { n: s.check.handled }))}
            {@render stat(s.check.downloaded, t('downloads.stat.new'), s.check.downloaded ? 'good' : '')}
          {:else}
            <span>{cap(every(s.interval))}</span>
            {#if gallery && oneGallery(s)}
              <!-- One gallery: counted by page (the comic's, else what its checks brought in). -->
              {@const pages = look?.pages || s.pages || 0}
              {#if pages}{@render stat(pages, t('downloads.stat.pages', { n: pages }))}{/if}
            {:else if s.downloaded}{@render stat(s.downloaded, t(gallery ? 'downloads.stat.comics' : 'downloads.stat.files', { n: s.downloaded }))}{/if}
            {#if s.check?.state === 'failed' && nextCheck(s)}<span>{cap(nextCheck(s))}</span>{/if}
            {#if s.last_found_at}<span>{t('downloads.foundAgo', { span: span(now - s.last_found_at) })}</span>
            {:else if !s.checks && s.check?.state !== 'failed'}<span>{cap(lastCheck(s))}</span>{/if}
          {/if}
        </div>
        {#if s.error}<p class="error small" transition:reveal>{s.error}</p>{/if}
        {#if s.fav_gone}
          <div class="fav-gone" role="status" transition:reveal>
            <span>{t('downloads.favGone', { n: s.fav_gone })}</span>
            <span class="fav-gone-actions">
              <button class="quiet small" onclick={() => favGone(s, 'keep')}>{t('downloads.keep')}</button>
              <button class="quiet small danger" onclick={() => favGone(s, 'delete')}>{t('common.delete')}</button>
            </span>
          </div>
        {/if}
      </div>
      <ActionMenu
        label={t('common.moreFor', { name: named(s) })}
        title={s.query}
        items={[
          { label: t('downloads.checkNow'), icon: 'retry', run: () => subAct(s, 'check') },
          s.paused
            ? { label: t('downloads.resume'), icon: 'play', run: () => subAct(s, 'resume') }
            : { label: t('video.pause'), icon: 'pause', run: () => subAct(s, 'pause') },
          {
            label: t('downloads.schedule'),
            icon: 'clock',
            hint: cap(every(s.interval)),
            choices: {
              heading: t('downloads.check'),
              value: String(s.interval),
              options: intervals.includes(s.interval) ? intervalOptions : [...intervalOptions, { value: String(s.interval), label: cap(every(s.interval)) }],
              pick: (v) => changeInterval(s, v),
            },
          },
          {
            label: t('depth.label'),
            icon: 'depth',
            hint: depthLabel(s.check_depth ?? inheritedDepth(s)),
            choices: depthChoice(s.check_depth, inheritedDepth(s), (n) => changeDepth(s, n)),
          },
          {
            label: t('downloads.moveToGroup'),
            icon: 'folder',
            hint: groups.find((g) => g.id === s.group_id)?.name ?? t('downloads.noGroup'),
            choices: {
              heading: t('downloads.moveToGroup'),
              value: String(s.group_id ?? 0),
              options: [{ value: '0', label: t('downloads.noGroup') }, ...siteGroups(s.site).map((g) => ({ value: String(g.id), label: g.name })), { value: 'new', label: t('downloads.newGroup') }],
              pick: (v) => moveTo(s, v),
            },
          },
          { label: t('downloads.unsubscribe'), icon: 'close', hint: t('downloads.filesStay'), danger: true, run: () => subAct(s, 'remove') },
        ]} />
    </div>
    {#if moving?.sub === s.id}
      <form class="inline-name" onsubmit={(e) => moveToNew(e, s)} transition:reveal>
        <input bind:value={moving.name} placeholder={t('downloads.newGroupName')} aria-label={t('downloads.newGroupNameFor', { query: named(s) })} maxlength="60" />
        <button type="submit" disabled={!moving.name.trim()}>{t('downloads.createMove')}</button>
        <button type="button" class="quiet" onclick={() => (moving = null)}>{t('common.cancel')}</button>
      </form>
    {/if}
    </div>
  </li>
{/snippet}

<div class="page" {@attach overlayScroll}>
  <div class="page-inner wide">
    <h1>{t('stats.downloads')}</h1>

    <!-- Sites that stopped answering: first thing on the page. -->
    {#each pausedSiteIds as id (id)}{@render pausedLine(id)}{/each}

    <section class="card">
      <div class="card-head">
        <div class="title-row">
          <h2>{t('tagMenu.newDownload')}</h2>
          <button type="button" class="icon manage" aria-label={t('downloads.manageSites')} title={t('downloads.manageSites')} onclick={() => router.go('settings', 'sites')}
            ><Icon name="settings" size={18} /></button>
        </div>
        <p class="hint">{t('downloads.newHint')}</p>
      </div>
      <form class="add" onsubmit={add} bind:this={addForm}>
        <div class="line1">
          <div class="site">
            <ActionMenu label={t('filter.site.label')} title={t('filter.site.label')} current={site ? siteLabel(site) : t('downloads.pickSite')} items={siteMenu(sites.filter(siteOn), site)} />
          </div>
          <!-- Suggestions from the tags of the library's files from that site (a gallery site: its comics' tags); a pick is written the site's way. -->
          <div class="q" class:with-fav={!!site && siteInfo.favMe[site]}>
            <TagInput
              bind:value={query}
              bind:inputEl={queryInput}
              multi
              comics={siteInfo.engines[site] === 'ehentai' ? site : undefined}
              sites={site}
              format={(tg) => tagQuery(site, tg)}
              anyOf={(tags) => (canOr(site, tags) ? tagsQuery(site, tags, true) : null)}
              label={t('downloads.searchOnSite')}
              placeholder={site ? (siteInfo.hints[site] ?? t('downloads.searchSite', { site: siteLabel(site) })) : t('downloads.searchAny')}
              onenter={() => addForm?.requestSubmit()} />
            {#if site && siteInfo.favMe[site]}
              <!-- Your favorites on the site (fav:me), in one tap. -->
              <button type="button" class="icon fav-me" aria-label={t('downloads.myFavorites')} title={t('downloads.myFavoritesTitle')} onclick={() => (query = 'fav:me')}
                ><Icon name="heart" size={16} /></button>
            {/if}
          </div>
        </div>
        <div class="line2">
          <label class="opt">
            <span class="opt-label">{repeat === '0' ? t('downloads.files') : t('downloads.firstCheck')}</span>
            <input
              class="max"
              bind:value={maxPosts}
              type="number"
              min="1"
              inputmode="numeric"
              placeholder={t('downloads.all')}
              aria-label={repeat === '0' ? t('downloads.maxFiles') : t('downloads.maxFirst')}
              title={repeat === '0' ? t('downloads.maxFilesTitle') : t('downloads.maxFirstTitle')} />
          </label>
          <div class="opt">
            <span class="opt-label">{t('downloads.repeat')}</span>
            <Dropdown value={repeat} options={repeatOptions} onchange={setRepeat} label={t('downloads.repeat')} heading={t('downloads.repeat')} />
          </div>
          {#if repeat !== '0' && !favSearch}
            <div class="opt group-opt" transition:reveal>
              <span class="opt-label">{t('downloads.group')}</span>
              <Dropdown
                value={formGroup}
                options={[{ value: '', label: t('downloads.noGroup') }, ...siteGroups(site).map((g) => ({ value: String(g.id), label: g.name })), { value: 'new', label: t('downloads.newGroup') }]}
                onchange={pickFormGroup}
                label={t('downloads.group')}
                heading={t('downloads.addToGroup')} />
            </div>
          {/if}
          {#if repeat !== '0' && !favSearch && formGroup === 'new'}
            <input class="new-group" bind:value={newGroupName} placeholder={t('downloads.newGroupExample')} aria-label={t('downloads.newGroupLabel')} maxlength="60" />
          {/if}
          <button class="go" type="submit" disabled={busy || !site}>
            {#if repeat === '0'}<Icon name="download" size={18} /> {t('backups.download')}{:else}<Icon name="bell" size={18} /> {t('downloads.subscribe')}{/if}
          </button>
        </div>
        {#if ownFavs}
          <label class="add-favs" transition:reveal>
            <input type="checkbox" bind:checked={addToFavs} />
            <span>{t('downloads.addToFavs')}</span>
          </label>
        {/if}
        {#if repeat === '0' && maxPosts}
          <p class="note hint" transition:reveal>{t('downloads.noteOnce', { n: Number(maxPosts) })}</p>
        {/if}
        {#if repeat !== '0'}
          <p class="note hint" transition:reveal>
            {t('downloads.noteSub', { every: every(+repeat), first: maxPosts ? t('downloads.newestN', { n: Number(maxPosts) }) : t('downloads.everyPost') })}{site ? ' ' + t('downloads.fromSite', { site: siteLabel(site) }) : ''}.
          </p>
        {/if}
      </form>
    </section>

    {#if error}<p class="error small" transition:reveal>{error}</p>{/if}

    <!-- What's going on, at a glance: each count a link to where those are (zeros left out). -->
    <div class="summary" role="status">
      {#if runningCount}{@render sumItem(runningCount, t('downloads.sum.running'), () => goTo('queue'), 'running')}{/if}
      {#if waitingCount}{@render sumItem(waitingCount, t('downloads.sum.waiting'), () => goTo('queue'))}{/if}
      {#if failedFileCount}{@render sumItem(failedFileCount, t('downloads.sum.failed', { n: failedFileCount }), () => goTo('history', 'dl-failed'), 'bad')}{/if}
      {#if attentionCount}{@render sumItem(attentionCount, t('downloads.sum.attention', { n: attentionCount }), () => { attentionOnly = true; goTo('subs'); }, 'warn')}{/if}
      {#if nextCheckAt !== Infinity}
        <button class="sum-item" onclick={() => goTo('subs')}><Icon name="clock" size={13} /> {nextCheckAt <= now ? t('downloads.nextSoon') : t('downloads.nextIn', { span: span(nextCheckAt - now) })}</button>
      {/if}
      {#if tagsDue}{@render sumItem(tagsDue, t('downloads.sum.tagsDue', { n: tagsDue }), () => goTo('queue', 'dl-refresh'))}{/if}
      {#if !runningCount && !waitingCount && !failedFileCount && !attentionCount && nextCheckAt === Infinity && !tagsDue}
        <span class="sum-quiet">{t('downloads.sum.quiet')}</span>
      {/if}
    </div>

    <!-- Queue, Subscriptions, History: one at a time (the last one looked at is remembered). -->
    <div class="tabs" id="dl-tabs" role="tablist" aria-label={t('stats.downloads')} bind:this={tabsEl}>
      {#if pill}<span class="tab-pill" class:ready={pillReady} style:width="{pill.w}px" style:transform="translateX({pill.x}px)" aria-hidden="true"></span>{/if}
      {#each [{ id: 'queue', label: t('downloads.queue'), n: waitingCount + runningCount }, { id: 'subs', label: t('stats.subscriptions'), short: t('downloads.subsShort'), n: subs.length }, { id: 'history', label: t('downloads.history'), n: failedFileCount + finished.length }] as x (x.id)}
        <button role="tab" id="tab-{x.id}" aria-selected={tab === x.id} aria-controls="panel-{x.id}" class:on={tab === x.id} onclick={() => setTab(x.id as Tab)}
          ><span class="tab-label">{#if x.short}<span class="long">{x.label}</span><span class="short" aria-hidden="true">{x.short}</span>{:else}{x.label}{/if}</span>{#if x.n}<span class="tab-num" transition:slide={{ axis: 'x', duration: reducedMotion() ? 0 : 220, easing: ease }}
              ><span class="tab-count" use:easeWidth>{x.n > 99 ? '99+' : num(x.n)}</span></span
            >{/if}</button>
      {/each}
    </div>

    <div class="panels">
    {#if tab === 'queue'}
      <div class="panel" id="panel-queue" role="tabpanel" aria-labelledby="tab-queue" in:panelIn out:panelOut>
        <section class="card">
          <div class="card-head">
            <h2>{t('downloads.queue')}</h2>
            {#if queueSites.length}<p class="hint" transition:reveal>{t('downloads.queueHint')}</p>{/if}
          </div>
          {#each queueSites as q (q.site)}
            {@const key = 'queue:' + q.site}
            <div class="site-block queue-site" animate:moved transition:reveal>
              {@render stackHead(key, q.site, q.entries.length, q.entries.length > 1, t('downloads.queueWaits'))}
              <Stack
                items={q.entries}
                key={entryKey}
                bind:open={() => !!unfolded[key], (v) => (unfolded[key] = v)}
                label={t('downloads.inQueue', { site: siteLabel(q.site) })}
                shift={(i) => {
                  const e = q.entries[i];
                  return e?.kind === 'search' && e.wi >= 0 ? shift(q.site, e.wi) : 0;
                }}
                lifted={(i) => {
                  const e = q.entries[i];
                  return e?.kind === 'search' && drag?.id === e.s.id;
                }}>
                {#snippet card(e: QueueEntry, c)}
                  {#if e.kind === 'retry'}
                    {@render retryCard(e.r)}
                  {:else}
                    {@const grips = c.open && q.waiting.length > 1}
                    <div class="row" class:has-grip={grips} data-wi={e.wi >= 0 ? e.wi : undefined}>
                      {#if grips && e.wi >= 0}
                        <button
                          class="grip"
                          aria-label={t('saved.reorder', { name: nameOf(e.s) })}
                          title={t('saved.dragToReorder')}
                          onpointerdown={(ev) => dragStart(ev, q.site, e.wi, e.s.id)}
                          onpointermove={dragMove}
                          onpointerup={dragEnd}
                          onpointercancel={dragEnd}
                          onkeydown={(ev) => handleKey(ev, q.site, e.wi, q.waiting.length)}><Icon name="grip" size={16} /></button>
                      {:else if grips}
                        <span class="grip-space" aria-hidden="true"></span>
                      {/if}
                      <div class="main">{@render searchInfo(e.s)}</div>
                      <div class="acts">{@render searchActs(e.s, false)}</div>
                    </div>
                  {/if}
                {/snippet}
              </Stack>
            </div>
          {:else}
            <p class="quiet-empty" transition:reveal>{t('downloads.nothingWaiting')}</p>
          {/each}
        </section>

        <!-- The tag refresh: a line per site (posts read again now and then: sites keep editing tags). -->
        {#if refreshStatus.length}
          <section class="card slim" id="dl-refresh" transition:reveal>
            <div class="card-head">
              <h2>{t('downloads.refresh.title')}</h2>
              <p class="hint">{t('downloads.refresh.hint')}</p>
            </div>
            <ul class="list refresh-list">
              {#each refreshStatus as r (r.site)}
                <li class="refresh-line" animate:moved transition:reveal>
                  <span class="refresh-site">{siteLabel(r.site)}</span>
                  <span class="refresh-nums">
                    {#if r.due}{@render stat(r.due, t('downloads.stat.toRefresh'), 'lead')}{/if}
                    {@render stat(r.posts, t('downloads.stat.posts', { n: r.posts }))}
                  </span>
                  {#if r.paused}
                    <!-- (its site isn't answering: what's due waits, more can still be queued) -->
                    <span class="state paused" title={t('downloads.pausedTitle')}>{t('downloads.paused')}</span>
                  {:else}
                    <span class="state {r.due ? 'running' : 'done'}">{r.due ? t('downloads.refresh.refreshing') : t('downloads.refresh.upToDate')}</span>
                  {/if}
                </li>
              {/each}
            </ul>
          </section>
        {/if}
      </div>
    {:else if tab === 'subs'}
      <div class="panel" id="panel-subs" role="tabpanel" aria-labelledby="tab-subs" in:panelIn out:panelOut>
        <section class="card">
          <div class="card-head">
            <h2>{t('stats.subscriptions')}</h2>
            {#if subs.length}<p class="hint" transition:reveal>{t('downloads.subsHint')}</p>{/if}
          </div>
          {#if subs.length >= SUB_TOOLS_FROM || attentionCount}
            <div class="sub-tools" transition:reveal>
              {#if subs.length >= SUB_TOOLS_FROM}
                <label class="sub-filter">
                  <Icon name="search" size={16} />
                  <input
                    type="search"
                    bind:value={subFilter}
                    placeholder={t('downloads.filterSubs')}
                    aria-label={t('downloads.filterSubs')}
                    autocapitalize="off"
                    autocomplete="off"
                    spellcheck="false" />
                </label>
              {/if}
              {#if attentionCount}
                <button class="attention-toggle" class:on={attentionOnly} aria-pressed={attentionOnly} onclick={() => (attentionOnly = !attentionOnly)}
                   transition:nudge><Icon name="alert" size={14} /> {t('downloads.needsLook')}<span class="tab-count">{num(attentionCount)}</span></button>
              {/if}
              {#if subs.length >= SUB_TOOLS_FROM}
                <Dropdown value={subSort} options={subSortOptions} onchange={setSubSort} label={t('downloads.orderSubs')} heading={t('downloads.orderBy')} align="end" />
              {/if}
            </div>
            {#if subFilter.trim()}
              <p class="hint small sub-count" transition:reveal>
                {shownSubs.length ? t('downloads.showing', { count: shownSubs.length, n: subs.length }) : t('downloads.noMatch', { filter: subFilter.trim() })}
              </p>
            {/if}
          {/if}
          {#if subs.length}
            {#each subTree as site (site.site)}
              {@const sFolded = siteFolded('subs:' + site.site) && !filtering && (subTree.length > 1 || sitesOff.includes(site.site))}
              {#if subTree.length > 1 || sitesOff.includes(site.site)}
                {@render siteHead('subs:' + site.site, site.site, site.loose.length + site.groups.reduce((n, x) => n + x.subs.length, 0), t('downloads.notChecked2'))}
              {/if}
              {#if !sFolded}
                {#each site.groups as { g, subs: members } (g.id)}
                  {@const gs = groupStats(g)}
                  {@const open = expanded.includes(g.id) || filtering}
                  <section class="group" aria-label={t('downloads.groupLabel', { name: g.name })} animate:moved transition:reveal>
                    {#if renaming?.id === g.id}
                      <form class="inline-name rename" onsubmit={renameGroup} transition:reveal>
                        <input bind:value={renaming.name} aria-label={t('downloads.newNameFor', { name: g.name })} maxlength="60" />
                        <button type="submit" disabled={!renaming.name.trim()}>{t('tagBrowser.rename')}</button>
                        <button type="button" class="quiet" onclick={() => (renaming = null)}>{t('common.cancel')}</button>
                      </form>
                    {:else}
                      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (the name button is the keyboard way) -->
                      <div
                        class="row group-head"
                        class:with-mosaic={groupPics(members).length > 0}
                        onclick={(e) => {
                          if (!(e.target as HTMLElement).closest('button, a, input')) toggleGroup(g.id);
                        }}>
                        {#if groupPics(members).length}
                          <!-- Its subscriptions' newest finds, as a cover. -->
                          <span class="mosaic n{groupPics(members).length}" aria-hidden="true">{#each groupPics(members) as p (p.id)}<img src={p.thumb} alt="" loading="lazy" use:fadeImg />{/each}</span>
                        {/if}
                        <div class="main">
                          <div class="head">
                            <button class="group-title" class:open aria-expanded={open} aria-label={t(open ? 'downloads.foldName' : 'downloads.unfoldName', { name: g.name })} onclick={() => toggleGroup(g.id)}>
                              <Icon name="right" size={16} />
                              <span class="group-name">{g.name}</span>
                              <span class="count">{gs.count}</span>
                            </button>
                            {#if gs.allPaused && gs.count}<span class="state paused" transition:nudge>{t('downloads.state.paused')}</span>{/if}
                          </div>
                          {#if g.interval}
                            <div class="stats" transition:reveal><span class="stat meta"><Icon name="clock" size={13} /> {cap(every(g.interval))}</span></div>
                          {/if}
                          <div class="stats">
                            {#if gs.files}{@render stat(gs.files, t('downloads.stat.files', { n: gs.files }))}{/if}
                            {#if gs.newLast}{@render stat(gs.newLast, t('downloads.stat.newLastChecks'), 'good')}{/if}
                          </div>
                        </div>
                        <ActionMenu
                          label={t('common.moreFor', { name: g.name })}
                          title={g.name}
                          items={[
                            { label: t('downloads.checkAllNow'), icon: 'retry', disabled: !gs.count, run: () => groupAct(g, 'check') },
                            gs.allPaused
                              ? { label: t('downloads.resumeAll'), icon: 'play', run: () => groupAct(g, 'resume') }
                              : { label: t('downloads.pauseAll'), icon: 'pause', disabled: !gs.count, run: () => groupAct(g, 'pause') },
                            {
                              label: t('downloads.scheduleAll'),
                              icon: 'clock',
                              hint: g.interval ? cap(every(g.interval)) : '',
                              choices: { heading: t('downloads.checkAll'), value: String(g.interval ?? ''), options: intervalOptions, pick: (v) => groupInterval(g, v) },
                            },
                            {
                              label: t('depth.label'),
                              icon: 'depth',
                              hint: depthLabel(g.check_depth ?? siteDepth(g.site)),
                              choices: depthChoice(g.check_depth, siteDepth(g.site), (n) => groupDepth(g, n)),
                            },
                            { label: t('tagBrowser.rename'), icon: 'pencil', run: () => groupMenu(g, 'rename') },
                            { label: t('downloads.deleteGroup'), icon: 'trash', danger: true, run: () => groupMenu(g, 'delete') },
                          ]} />
                      </div>
                    {/if}
                    {#if open}
                      {#if members.length}
                        <ul class="list in-group sub-cards" transition:reveal>
                          {#each members as s (s.id)}{@render subRow(s)}{/each}
                        </ul>
                      {:else}
                        <p class="hint small group-empty" transition:reveal>{t('downloads.noSubs')}</p>
                      {/if}
                    {/if}
                  </section>
                {/each}
                {#if site.loose.length}
                  {#if site.groups.length}<h4 class="loose-head" transition:reveal>{t('downloads.notInGroup')}</h4>{/if}
                  <ul class="list sub-cards">
                    {#each site.loose as s (s.id)}{@render subRow(s)}{/each}
                  </ul>
                {/if}
              {/if}
            {/each}
          {:else}
            <p class="quiet-empty" transition:reveal>{t('downloads.noSubs')}</p>
          {/if}
        </section>
      </div>
    {:else}
      <div class="panel" id="panel-history" role="tabpanel" aria-labelledby="tab-history" in:panelIn out:panelOut>
        <!-- Failed files, grouped by why: each reason a stack of its own, with Retry all and Dismiss all. -->
        {#if failedPosts.length}
          <section class="card" id="dl-failed" transition:reveal>
            <div class="card-head">
              <h2>{t('downloads.failedFiles')}</h2>
              <p class="hint">{t('downloads.failedFilesHint')}</p>
            </div>
            {#each failedReasons as g (g.reason)}
              {@const key = 'failed:' + g.reason}
              <div class="site-block reason-block" animate:moved transition:reveal>
                <div class="reason-head">
                  <div class="reason-title">
                  <h3 class="site-head">
                    {#if g.list.length > 1}
                      <button class="site-fold" class:open={!!unfolded[key]} aria-expanded={!!unfolded[key]} aria-label={t(unfolded[key] ? 'downloads.foldName' : 'downloads.unfoldName', { name: t(`downloads.reason.${g.reason}`) })} onclick={() => (unfolded[key] = !unfolded[key])}>
                        <Icon name="right" size={16} />
                        <span class="site-name">{t(`downloads.reason.${g.reason}`)}</span>
                        <span class="count">{g.list.length}</span>
                      </button>
                    {:else}
                      <span class="site-label"><span class="site-name">{t(`downloads.reason.${g.reason}`)}</span></span>
                    {/if}
                  </h3>
                  <p class="hint small reason-hint">{t(`downloads.reasonHint.${g.reason}`)}</p>
                  </div>
                  <div class="reason-acts">
                    <button class="quiet small" onclick={() => retryPosts(g.list)}><Icon name="retry" size={14} /> {g.list.length > 1 ? t('downloads.retryAll') : t('downloads.retry')}</button>
                    <button class="quiet small" onclick={() => dismissPosts(g.list)} title={t('downloads.dismissTitle')}>{g.list.length > 1 ? t('downloads.dismissAll') : t('downloads.dismiss')}</button>
                  </div>
                </div>
                <Stack items={g.list} key={postKey} bind:open={() => !!unfolded[key], (v) => (unfolded[key] = v)} label={t('downloads.failedWhy', { reason: t(`downloads.reason.${g.reason}`) })}>
                  {#snippet card(f: FailedPost)}
                    <div class="row">
                      <div class="main">
                        <div class="fp-top">
                          {#if f.url}
                            <a class="fp-post" href={f.url} target="_blank" rel="noopener noreferrer">{t('downloads.post', { post: String(f.post) })}</a>
                          {:else}
                            <span class="fp-post">{t('downloads.post', { post: String(f.post) })}</span>
                          {/if}
                          <span class="fp-where">{siteLabel(f.site)} · {f.query ? f.query + ' · ' : ''}{ago(f.at)}</span>
                        </div>
                        {#if f.error}<p class="error small fp-err" transition:reveal>{f.error}</p>{/if}
                      </div>
                      <div class="acts">
                        <button class="icon" onclick={() => retryPost(f)} aria-label={t('downloads.retryPost', { post: String(f.post) })} title={t('downloads.retry')}
                          ><Icon name="retry" size={18} /></button>
                        <button class="icon" onclick={() => dismissPosts([f])} aria-label={t('downloads.dismissPost', { post: String(f.post) })} title={t('downloads.dismissTitle')}
                          ><Icon name="close" size={18} /></button>
                      </div>
                    </div>
                  {/snippet}
                </Stack>
              </div>
            {:else}
              <p class="quiet-empty" transition:reveal>{t('downloads.allRetrying')}</p>
            {/each}
          </section>
        {/if}

        <!-- Finished: today's by site, the earlier ones folded away. -->
        <section class="card">
          <div class="card-head with-action">
            <h2>{t('downloads.finished')}</h2>
            {#if failedCount || doneCount}
              <div class="head-acts" transition:reveal>
                {#if failedCount}
                  <button class="quiet small" onclick={retryFailed} transition:nudge><Icon name="retry" size={15} /> {t('downloads.retryFailed')}</button>
                {/if}
                {#if doneCount}
                  <button class="quiet small" onclick={clearFinished} transition:nudge>{t('downloads.clearFinished')}</button>
                {/if}
              </div>
            {/if}
          </div>
          {#if finished.length}
            {#if finishedEarlier.length}<h3 class="when-head" transition:reveal>{t('downloads.today')}</h3>{/if}
            {#each todaySites as g (g.site)}
              {@render finishedSite(g, 'done:')}
            {:else}
              <p class="quiet-empty" transition:reveal>{t('downloads.noneToday')}</p>
            {/each}
            {#if finishedEarlier.length}
              <button class="earlier" class:open={earlierOpen} aria-expanded={earlierOpen} onclick={toggleEarlier} transition:reveal>
                <Icon name="right" size={16} />
                <span>{t('downloads.earlier')}</span>
                <span class="count">{num(finishedEarlier.length)}</span>
              </button>
              {#if earlierOpen}
                {#each earlierSites as g (g.site)}
                  {@render finishedSite(g, 'earlier:')}
                {/each}
              {/if}
            {/if}
          {:else}
            <p class="quiet-empty">{t('downloads.noFinished')}</p>
          {/if}
        </section>
      </div>
    {/if}
    </div>
  </div>
</div>

{#snippet finishedSite(g: { site: string; list: DownloadSearch[] }, prefix: string)}
  {@const key = prefix + g.site}
  <div class="site-block" transition:reveal>
    {@render stackHead(key, g.site, g.list.length, g.list.length > 1, '')}
    <Stack items={g.list} key={(s) => s.id} bind:open={() => !!unfolded[key], (v) => (unfolded[key] = v)} label={t('downloads.finishedOn', { site: siteLabel(g.site) })}>
      {#snippet card(s: DownloadSearch)}
        <div class="row">
          <div class="main">{@render searchInfo(s)}</div>
          <div class="acts">{@render searchActs(s, true)}</div>
        </div>
      {/snippet}
    </Stack>
  </div>
{/snippet}

<style>
  /* ---- rows: one framed list per section, rows split by hairlines */
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    border-radius: 12px;
    border: 1px solid var(--surface0);
    background: var(--crust);
  }
  li {
    position: relative;
    background: var(--crust);
    transition: transform 0.15s;
  }
  li:first-child {
    border-radius: 11px 11px 0 0;
  }
  li:last-child {
    border-radius: 0 0 11px 11px;
  }
  li:only-child {
    border-radius: 11px;
  }
  li + li {
    border-top: 1px solid var(--surface0);
  }
  li.flash {
    animation: flash 2.2s ease-out;
  }
  @keyframes flash {
    0%,
    40% {
      background: color-mix(in srgb, var(--mauve) 14%, var(--crust));
      box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--mauve) 60%, transparent);
    }
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
  }
  /* A row's ⋮ in its top right corner (not floating halfway down a tall card). */
  .row > :global(button.more) {
    align-self: start;
    margin-top: -4px;
  }
  .row {
    column-gap: 6px;
    padding: 10px 6px 10px 14px;
  }
  .row.has-grip {
    grid-template-columns: 20px minmax(0, 1fr) auto;
    padding-left: 2px;
  }
  .grip-space {
    width: 20px;
  }
  .grip {
    align-self: stretch;
    width: 20px;
    min-width: 0;
    min-height: 0;
    height: auto;
    padding: 0;
    background: none;
    border-radius: 6px;
    color: var(--overlay0);
    cursor: grab;
    touch-action: none;
  }
  :global(.lifted) .grip {
    cursor: grabbing;
  }
  .main {
    min-width: 0;
    display: grid;
    gap: 6px;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .query {
    flex: 1;
    min-width: 0;
    font-weight: 600;
    overflow-wrap: anywhere;
    line-height: 1.3;
  }
  .query.one-line {
    overflow-wrap: normal;
  }
  /* Numbers that say what they are: the number bold, its meaning beside it. */
  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 14px;
    font-size: var(--font-small);
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .stat {
    white-space: nowrap;
  }
  .stat b {
    color: var(--text);
    font-weight: 600;
  }
  .stat.good b {
    color: var(--green);
  }
  .stat.bad b,
  .stat.bad {
    color: var(--red);
  }
  .stat.quiet {
    color: var(--overlay1);
  }
  /* The lead count's goal ("/50,000"), and a kind of post as an icon and its count. */
  .stat .of {
    color: var(--subtext);
  }
  /* (as chips - the user: new green, blacklisted and failed red, the rest plain) */
  .stat.icon-stat {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 1px 9px 1px 7px;
    border-radius: 999px;
    background: var(--surface0);
    line-height: 20px;
  }
  .stat.icon-stat :global(svg) {
    color: var(--overlay1);
  }
  .stat.icon-stat.good {
    background: color-mix(in srgb, var(--green) 14%, transparent);
  }
  .stat.icon-stat.good :global(svg) {
    color: var(--green);
  }
  .stat.icon-stat.bad {
    background: color-mix(in srgb, var(--red) 14%, transparent);
  }
  .stat.icon-stat.bad :global(svg) {
    color: var(--red);
  }
  /* (chips closer together than the line's other items: 6 px of its 14) */
  .stat.icon-stat + .stat.icon-stat {
    margin-left: -8px;
  }
  /* (only for screen readers: what a count is) */
  .vh {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  .stat.meta {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .stat.meta :global(svg) {
    color: var(--overlay1);
  }

  /* A line that never wraps: it scrolls sideways (no scrollbar), with soft
     edges where more is out of view. */
  .one-line,
  .counts {
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: none;
    white-space: nowrap;
  }
  .one-line::-webkit-scrollbar,
  .counts::-webkit-scrollbar {
    display: none;
  }
  .fade-l.fade-r {
    mask-image: linear-gradient(to right, transparent, #000 18px, #000 calc(100% - 18px), transparent);
  }
  .fade-l:not(.fade-r) {
    mask-image: linear-gradient(to right, transparent, #000 18px);
  }
  .fade-r:not(.fade-l) {
    mask-image: linear-gradient(to right, #000 calc(100% - 18px), transparent);
  }
  .line-stats {
    display: flex;
    align-items: baseline;
    gap: 12px;
    min-width: 0;
    font-size: var(--font-small);
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .counts {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center; /* (the lead count level with the chips) */
    gap: 14px;
  }
  .line-stats .speed {
    flex: none;
    margin-left: auto;
  }
  @media (max-width: 639px) {
    .row {
      align-items: start;
    }
  }

  .state {
    flex: none;
    font-size: 12px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--surface0);
    color: var(--subtext);
  }
  .state.running {
    color: var(--peach);
    background: color-mix(in srgb, var(--peach) 14%, transparent);
  }
  .state.done {
    color: var(--green);
    background: color-mix(in srgb, var(--green) 14%, transparent);
  }
  .state.failed {
    color: var(--red);
    background: color-mix(in srgb, var(--red) 14%, transparent);
  }
  .state.paused {
    color: var(--yellow);
    background: color-mix(in srgb, var(--yellow) 12%, transparent);
  }
  .kind {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-right: 6px;
    padding: 1px 6px 1px 4px;
    border-radius: 6px;
    background: color-mix(in srgb, var(--mauve) 14%, transparent);
    color: var(--mauve);
    font-size: 12px;
    font-weight: 600;
    vertical-align: 1px;
  }
  .bar {
    height: 4px;
    border-radius: 2px;
    background: var(--surface0);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    background: var(--peach);
    transition: width 0.4s;
  }
  .fill.done {
    background: var(--green);
  }
  .fill.failed {
    background: var(--red);
  }
  .fill.paused,
  .fill.queued {
    background: var(--overlay0);
  }
  .bar.striped {
    background: repeating-linear-gradient(-45deg, var(--peach) 0 6px, color-mix(in srgb, var(--peach) 45%, var(--surface0)) 6px 12px);
    background-size: 17px 17px; /* one stripe period along the bar (12 px / cos 45deg) */
    animation: stripes 0.8s linear infinite;
  }
  @keyframes stripes {
    to {
      background-position: 17px 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bar.striped {
      animation: none;
    }
  }
  .stat.speed {
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .retry {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--subtext);
  }

  /* ---- subscription groups: a framed block, its subscriptions inside (no second frame) */
  .paused-chip {
    flex: none;
    padding: 1px 8px;
    border-radius: 999px;
    font-size: var(--font-small);
    font-weight: 600;
    color: var(--peach);
    background: color-mix(in srgb, var(--peach) 14%, transparent);
  }
  .paused-line {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 8px;
    padding: 8px 8px 8px 12px;
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--peach) 10%, var(--mantle));
    border: 1px solid color-mix(in srgb, var(--peach) 30%, var(--surface0));
    color: var(--peach);
  }
  .paused-text {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 2px;
    color: var(--text);
    font-size: var(--font-small);
  }
  .paused-why {
    color: var(--subtext);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .paused-line button {
    flex: none;
  }
  .off-chip {
    margin-left: 8px;
    padding: 1px 8px;
    border-radius: 999px;
    box-shadow: inset 0 0 0 1px var(--surface1);
    color: var(--overlay1);
    font-size: 12px;
    font-weight: 600;
    vertical-align: 2px;
  }
  .site-head {
    display: flex;
    align-items: center;
    margin: 6px 0 0;
    text-transform: none; /* (a site's name, a reason: as written, like the fold buttons) */
    letter-spacing: normal;
  }
  .site-block {
    display: grid;
    gap: 6px;
  }
  .site-block + .site-block {
    margin-top: 8px;
  }
  .site-label {
    display: inline-flex;
    align-items: center;
    min-height: 28px;
    padding: 0 8px 0 4px;
    font: inherit;
  }
  /* A site's heading folds the whole site (like a group's). */
  .site-fold {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 0;
    height: auto;
    padding: 4px 8px 4px 2px;
    margin-left: -2px;
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
  }
  .site-fold > :global(svg) {
    flex: none;
    color: var(--subtext);
    transition: rotate 0.15s;
  }
  .site-fold.open > :global(svg) {
    rotate: 90deg;
  }
  .site-name {
    overflow-wrap: anywhere;
  }
  .group {
    display: grid;
    border-radius: 12px;
    border: 1px solid var(--surface0);
    background: color-mix(in srgb, var(--crust) 55%, transparent);
    overflow: hidden;
  }
  .group-head {
    padding: 8px 6px 8px 6px;
  }
  .group-title {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: 6px;
    min-height: 0;
    height: auto;
    padding: 2px 4px 2px 2px;
    background: none;
    color: var(--text);
    text-align: left;
    white-space: normal;
  }
  .group-title > :global(svg) {
    flex: none;
    color: var(--subtext);
    transition: rotate 0.15s;
  }
  .group-title.open > :global(svg) {
    rotate: 90deg;
  }
  .group-name {
    min-width: 0;
    font-weight: 700;
    overflow-wrap: anywhere;
  }
  .count {
    flex: none;
    min-width: 20px;
    padding: 0 6px;
    border-radius: 10px;
    background: var(--surface0);
    color: var(--subtext);
    font-size: 12px;
    font-weight: 600;
    text-align: center;
  }
  .group-head .stats {
    padding-left: 24px; /* under the name, past the fold arrow */
  }
  .list.in-group {
    border: none;
    border-top: 1px solid var(--surface0);
    border-radius: 0;
  }
  .group-empty {
    margin: 0;
    padding: 10px 14px 12px;
    border-top: 1px solid var(--surface0);
  }
  .loose-head {
    margin: 4px 2px 0;
    font-size: var(--font-small);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--overlay1);
  }
  .inline-name {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    padding: 0 12px 12px;
  }
  .inline-name.rename {
    padding: 8px;
  }
  .inline-name input {
    flex: 1 1 12em;
    min-width: 0;
  }
  .confirm {
    display: grid;
    gap: 10px;
    margin: 0 8px 8px;
    padding: 12px;
    border-radius: 10px;
    font-size: var(--font-small);
    background: color-mix(in srgb, var(--red) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--red) 35%, transparent);
  }
  .row-actions {
    display: flex;
    gap: 8px;
  }
  .danger-solid {
    background: var(--red);
    color: var(--crust);
  }

  /* Subscriptions: filter + order, one line (the filter takes the room). */
  .sub-tools {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .sub-filter {
    flex: 1;
    min-width: 0;
    position: relative;
    display: flex;
    align-items: center;
  }
  .sub-filter :global(svg) {
    position: absolute;
    left: 11px;
    color: var(--overlay0);
    pointer-events: none;
  }
  .sub-filter input {
    width: 100%;
    padding-left: 34px;
  }
  .sub-count {
    margin: 0;
  }

  /* ---- New download. Phone: site, search, then Files + Repeat side by side, then the button.
     Wider: site and search on one line; options and the button on the next. */
  .add {
    display: grid;
    gap: 10px;
  }
  .line1 {
    display: grid;
    gap: 8px;
  }
  .line1 .site {
    display: flex;
    gap: 4px;
  }
  .line1 .site :global(.picker) {
    flex: 1;
    min-width: 0;
  }
  .line1 .site :global(.dropdown),
  .opt :global(.dropdown) {
    display: grid;
  }
  /* The card's title with a quiet way to the sites on the right. */
  .title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin: -6px -6px -6px 0; /* the button's own room, not the title's */
  }
  .title-row button.manage {
    flex: none;
    color: var(--overlay1);
  }
  @media (hover: hover) {
    .title-row button.manage:hover {
      color: var(--subtext);
    }
  }
  .q {
    position: relative;
  }
  .q.with-fav :global(input) {
    padding-right: 40px;
  }
  .fav-me {
    position: absolute;
    right: 4px;
    top: 50%;
    transform: translateY(-50%);
    color: var(--overlay1);
    background: none;
  }
  /* Inside the box: no square behind it - the heart itself lights up. */
  @media (hover: hover) {
    .q .fav-me:hover:not(:disabled) {
      background: none;
      color: var(--red);
    }
  }
  .q .fav-me:active {
    color: var(--red);
  }
  .line2 {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 8px 10px;
    align-items: end;
  }
  .opt {
    display: grid;
    gap: 4px;
    min-width: 0;
  }
  .opt-label {
    font-size: var(--font-small);
    font-weight: 600;
    color: var(--subtext);
  }
  .max {
    width: 100%;
  }
  .go {
    grid-column: 1 / -1;
  }
  .group-opt,
  .new-group {
    grid-column: 1 / -1;
  }
  .note {
    margin: 0;
  }
  /* A favorites subscription: files that left the site's favorites, waiting for a choice. */
  .fav-gone {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 6px 10px;
    margin-top: 6px;
    padding: 6px 6px 6px 10px;
    border-radius: 10px;
    background: color-mix(in srgb, var(--yellow) 10%, transparent);
    color: var(--yellow);
    font-size: var(--font-small);
  }
  .fav-gone-actions {
    display: flex;
    gap: 6px;
  }
  .add-favs {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: var(--font-small);
    color: var(--subtext);
  }
  @media (min-width: 640px) {
    .line1 {
      display: flex;
    }
    .line1 .site {
      flex: none; /* as wide as the name of the site */
    }
    .line1 .q {
      flex: 1;
      min-width: 0;
    }
    /* Each option's label above it (as on phones); the button at the end of the row. */
    .line2 {
      display: flex;
      flex-wrap: wrap;
      align-items: flex-end;
      gap: 10px 14px;
    }
    .max {
      width: 80px;
    }
    .opt :global(.dropdown) {
      width: 140px;
    }
    /* A group's name: as wide as it needs up to a limit, then cut short (never over the button). */
    .group-opt {
      flex: 0 1 auto;
      min-width: 140px;
      max-width: 300px;
    }
    .group-opt :global(.dropdown) {
      width: auto;
      max-width: 100%;
    }
    .go {
      flex: none;
      margin-left: auto;
      padding: 0 18px;
    }
    .new-group {
      order: 10;
      flex: 1 1 100%;
    }
  }

  .head-acts {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 6px;
  }
  .with-action {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: var(--control-h);
  }
  .fp-top {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 4px 10px;
    min-width: 0;
  }
  .fp-post {
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .fp-where {
    min-width: 0;
    color: var(--subtext);
    font-size: var(--font-small);
    overflow-wrap: anywhere;
  }
  /* Trying again: the icon turns. */
  .spinning :global(svg) {
    animation: fp-spin 0.9s linear infinite;
  }
  @keyframes fp-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .fp-err {
    margin: 4px 0 0;
    overflow-wrap: anywhere;
  }
  h3 {
    margin: 4px 2px -6px;
    font-size: var(--font-small);
    font-weight: 600;
    color: var(--subtext);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  p {
    margin: 0;
  }
  /* ---- Sites that stopped answering: banners at the top of the page */
  .page-inner > .paused-line {
    margin: 0;
  }
  .paused-text b {
    font-weight: 700;
  }
  /* ---- The summary: counts that link to where they are */
  .summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-top: -4px;
  }
  .sum-item {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-height: 30px;
    height: auto;
    padding: 4px 11px;
    border-radius: 999px;
    border: 1px solid var(--surface0);
    background: var(--mantle);
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 500;
  }
  .sum-item b {
    color: var(--text);
    font-weight: 700;
  }
  .sum-item.running b {
    color: var(--peach);
  }
  .sum-item.bad b {
    color: var(--red);
  }
  .sum-item.warn b {
    color: var(--yellow);
  }
  .sum-item :global(svg) {
    color: var(--overlay1);
  }
  @media (hover: hover) {
    .sum-item:hover {
      border-color: var(--surface1);
      color: var(--text);
    }
  }
  .sum-quiet {
    color: var(--overlay1);
    font-size: var(--font-small);
  }
  /* ---- Tabs: Queue, Subscriptions, History */
  /* (desktop: not the page's whole width - three tabs of the same size) */
  @media (min-width: 640px) {
    .tabs {
      width: 480px;
    }
  }
  .tabs {
    position: relative;
    display: flex;
    gap: 2px;
    padding: 3px;
    border-radius: 999px;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    scroll-margin-top: 12px;
  }
  .tabs button {
    position: relative; /* (over the pill) */
    flex: 1 1 0; /* all the same width: nothing moves when a count comes or goes */
    min-width: max-content; /* (a phone too narrow for a name and its count: that tab takes what it needs) */
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: calc(var(--control-h) - 6px);
    padding: 0 10px;
    border-radius: 999px;
    border: 0;
    background: transparent;
    color: var(--subtext);
    font-weight: 600;
    white-space: nowrap;
    transition: color 0.2s;
  }
  /* Phones: "Subs" for Subscriptions - three tabs and their counts fit (the user); screen readers still hear the whole
     word (the long one only hidden from sight). */
  .tab-label .short {
    display: none;
  }
  @media (max-width: 639px) {
    .tab-label .short {
      display: inline;
    }
    .tab-label .long {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
      white-space: nowrap;
    }
  }
  .tabs button.on {
    color: var(--text);
  }
  .tab-pill {
    position: absolute;
    top: 3px;
    bottom: 3px;
    left: 0;
    border-radius: 999px;
    background: var(--surface0);
    pointer-events: none;
  }
  .tab-pill.ready {
    transition:
      transform 0.32s cubic-bezier(0.2, 0.8, 0.2, 1),
      width 0.32s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  @media (prefers-reduced-motion: reduce) {
    .tab-pill.ready {
      transition: none;
    }
  }
  .panels {
    display: grid;
    min-width: 0;
  }
  .panels > .panel {
    grid-area: 1 / 1; /* (the one leaving and the one coming in, over each other) */
  }
  @media (hover: hover) {
    /* (no lighter layer like other buttons: the pill is the only highlight) */
    .tabs button:hover,
    .sum-item:hover {
      background-image: none;
    }
    .tabs button:not(.on):hover {
      color: var(--text);
    }
  }
  /* The name and its count in the middle of the tab; a count coming or going slides open (the gap with it),
     a new number glides to its width - the name moves along smoothly. */
  .tab-num {
    display: inline-flex;
    padding-left: 6px;
    overflow: hidden;
  }
  .tab-count {
    display: inline-block;
    flex: none;
    box-sizing: border-box;
    overflow: hidden;
    white-space: nowrap;
    transition:
      background-color 0.2s,
      color 0.2s;
    min-width: 18px;
    padding: 0 6px;
    border-radius: 9px;
    background: color-mix(in srgb, var(--overlay0) 22%, transparent);
    color: var(--subtext);
    font-size: 11px;
    font-weight: 700;
    line-height: 18px;
    text-align: center;
  }
  .tabs button.on .tab-count {
    background: color-mix(in srgb, var(--mauve) 22%, transparent);
    color: var(--mauve);
  }
  .panel {
    display: grid;
    gap: 16px;
    min-width: 0;
    scroll-margin-top: 12px;
  }
  .panel > .card {
    scroll-margin-top: 12px;
  }
  /* ---- An empty part: a quiet line, no frame */
  .quiet-empty {
    margin: 0;
    padding: 2px 0;
    color: var(--overlay1);
    font-size: var(--font-small);
  }
  /* ---- The tag refresh: a line per site */
  .refresh-line {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 12px;
  }
  .refresh-site {
    flex: none;
    font-weight: 600;
  }
  .refresh-nums {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 2px 12px;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .refresh-line + .refresh-line {
    border-top: 1px solid var(--surface0);
  }
  /* ---- Failed files by reason */
  .reason-block + .reason-block {
    margin-top: 14px;
  }
  .reason-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }
  .reason-head .site-head {
    margin: 0;
  }
  .reason-acts {
    display: flex;
    gap: 4px;
  }
  .reason-title {
    flex: 1 1 220px;
    min-width: 0;
    display: grid;
    gap: 0;
  }
  .reason-hint {
    margin: 0 0 2px 24px; /* under the name, past the fold arrow */
    color: var(--overlay1);
  }
  .reason-title:has(.site-label) .reason-hint {
    margin-left: 4px;
  }
  .acts .icon + .icon {
    margin-left: 2px;
  }
  /* ---- Subscriptions: the "needs a look" filter, and the quiet ones' chip */
  .attention-toggle {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--surface0);
    background: var(--base);
    color: var(--subtext);
    font-weight: 600;
  }
  .attention-toggle :global(svg) {
    color: var(--yellow);
  }
  .attention-toggle.on {
    border-color: color-mix(in srgb, var(--yellow) 45%, var(--surface0));
    background: color-mix(in srgb, var(--yellow) 12%, var(--base));
    color: var(--text);
  }
  .state.quiet-sub {
    color: var(--overlay1);
    font-weight: 500;
  }
  /* ---- Finished: today, then the earlier ones folded */
  .when-head {
    margin: 0;
    color: var(--subtext);
    font-size: var(--font-small);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .earlier {
    justify-self: start;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 0;
    height: auto;
    margin-top: 4px;
    padding: 4px 8px 4px 2px;
    background: none;
    color: var(--subtext);
    font-weight: 600;
  }
  .earlier > :global(svg) {
    transition: rotate 0.15s;
  }
  .earlier.open > :global(svg) {
    rotate: 90deg;
  }
  @media (hover: hover) {
    .earlier:hover {
      color: var(--text);
    }
  }
  @media (max-width: 480px) {
    .tabs button {
      padding: 0 6px;
      font-size: 14px;
    }
    .tab-num {
      padding-left: 5px;
    }
    .tab-count {
      min-width: 16px;
      padding: 0 5px;
      font-size: 10.5px;
      line-height: 16px;
    }
    .sub-tools {
      flex-wrap: wrap;
    }
    .sub-filter {
      flex-basis: 100%;
    }
  }

  /* ---- Subscriptions as cards (2026-10-22): columns on wide screens, each card its health, its search as chips,
     its newest files, one line of facts; a group a mosaic of its subscriptions' finds; phones swipe for actions. */
  @media (min-width: 1100px) {
    .page-inner.wide {
      max-width: 1200px;
    }
    .sub-cards {
      grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
    }
  }
  .list.sub-cards {
    gap: 8px;
    border: none;
    border-radius: 0;
    background: none;
  }
  .list.in-group.sub-cards {
    padding: 8px;
    border-top: 1px solid var(--surface0);
  }
  .sub-cards li,
  .sub-cards li + li {
    border: 1px solid var(--surface0);
    border-radius: 12px;
    overflow: hidden;
    touch-action: pan-y; /* (a sideways swipe is the card's) */
  }
  .card-body {
    position: relative;
    z-index: 1;
    height: 100%;
    background: var(--crust);
    transition: translate 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .swiping .card-body {
    transition: none;
  }
  .swipe-acts {
    position: absolute;
    inset: 0 0 0 auto;
    width: 148px;
    display: flex;
  }
  .swipe-act {
    flex: 1;
    display: grid;
    place-items: center;
    align-content: center;
    gap: 4px;
    height: auto;
    min-height: 0;
    padding: 6px;
    border-radius: 0;
    background: var(--surface0);
    color: var(--text);
    font-size: 12px;
    font-weight: 600;
  }
  .swipe-act:first-child {
    background: color-mix(in srgb, var(--mauve) 30%, var(--surface0));
  }
  @media (hover: hover) and (pointer: fine) {
    .swipe-acts {
      display: none;
    }
  }
  .health {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--green);
    box-shadow: 0 0 0 3px color-mix(in srgb, currentColor 0%, transparent);
  }
  .health.checking {
    background: var(--blue);
    animation: beat 1.2s ease-in-out infinite;
  }
  .health.paused {
    background: var(--overlay0);
  }
  .health.quiet {
    background: var(--yellow);
  }
  .health.failing {
    background: var(--red);
  }
  @keyframes beat {
    50% {
      box-shadow: 0 0 0 4px color-mix(in srgb, var(--blue) 30%, transparent);
    }
  }
  /* One line, scrolling sideways (soft edges where more is out of view). */
  .qchips {
    flex: 1;
    min-width: 0;
    display: flex;
    gap: 4px;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: none;
    touch-action: pan-x pan-y;
  }
  .qchips::-webkit-scrollbar {
    display: none;
  }
  .qchip {
    flex: none;
    white-space: nowrap;
    padding: 1px 9px;
    border-radius: 999px;
    background: color-mix(in srgb, currentColor 14%, transparent);
    color: var(--tag-general);
    font-size: 13px;
    font-weight: 600;
    line-height: 20px;
  }
  /* An OR search's chip: its tags (each its own color) with a small OR between them. */
  .qchip.any {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    background: color-mix(in srgb, var(--tag-general) 14%, transparent);
  }
  .qchip.any > span:not(.or) {
    color: var(--tag-general);
  }
  .qchip .or {
    font-size: 10px;
    font-weight: 700;
    color: var(--mauve);
  }
  .qchip.neg {
    color: var(--red);
    text-decoration: line-through;
    text-decoration-thickness: 1.5px;
  }
  .fresh {
    flex: none;
    min-height: 0;
    height: 22px;
    padding: 0 9px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--green) 18%, transparent);
    color: var(--green);
    font-size: 12px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  /* (even sizes, the ring as big as its box: the dot exactly in its middle) */
  .status {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
  }
  .ring {
    position: absolute;
    inset: 0;
  }
  .ring svg {
    display: block;
    width: 100%;
    height: 100%;
  }
  .ring svg {
    rotate: -90deg;
  }
  .ring circle {
    fill: none;
    stroke-width: 2.5;
  }
  .ring .track {
    stroke: var(--surface1);
  }
  .ring .fill {
    stroke: var(--mauve);
    stroke-linecap: round;
    stroke-dasharray: 50.27;
    transition: stroke-dashoffset 0.6s;
  }
  .ring.checking svg {
    animation: spin 1s linear infinite;
  }
  @keyframes spin {
    to {
      rotate: 270deg;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .health.checking,
    .ring.checking svg {
      animation: none;
    }
  }
  .pics {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 4px;
    max-width: 300px;
  }
  .pic {
    aspect-ratio: 1;
    overflow: hidden;
    border-radius: 7px;
    background: var(--surface0);
  }
  .pic img,
  .cover-pic img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .cover-pic {
    display: block;
    width: 66px;
    height: auto;
    min-height: 0;
    aspect-ratio: 0.7;
    padding: 0;
    overflow: hidden;
    border-radius: 8px;
    background: var(--surface0);
  }
  .facts {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 0;
    font-size: var(--font-small);
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .facts > :global(*) + :global(*)::before {
    content: '·';
    margin: 0 7px;
    color: var(--overlay0);
  }
  .row.group-head.with-mosaic {
    grid-template-columns: auto minmax(0, 1fr) auto;
    column-gap: 10px;
  }
  .with-mosaic .stats {
    padding-left: 4px;
  }
  .mosaic {
    display: grid;
    grid-template-columns: 1fr 1fr;
    grid-template-rows: 1fr 1fr;
    gap: 2px;
    width: 56px;
    height: 56px;
    overflow: hidden;
    border-radius: 10px;
    background: var(--surface0);
  }
  .mosaic img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    min-width: 0;
    min-height: 0;
  }
  .mosaic.n1 img {
    grid-area: 1 / 1 / 3 / 3;
  }
  .mosaic.n2 img {
    grid-row: 1 / 3;
  }
  .mosaic.n3 img:first-child {
    grid-row: 1 / 3;
  }
  .site-fold :global(.site-icon),
  .site-fold > :global(span:has(img)) {
    flex: none;
  }
</style>
