<script lang="ts">
  // Duplicates: pairs of files that look alike (found in the background, see
  // internal/dupes), one at a time. Flip between the two in place (the best
  // way to spot a difference) or see them side by side; the better one is
  // suggested. Keeping one gives it the other's tags and sources and deletes
  // the other (undo from the notice, or within the usual 7 days).
  import { onMount } from 'svelte';
  import { reveal, pop, swap } from '../lib/motion';
  import { SvelteSet } from 'svelte/reactivity';
  import { api, type DupeFile, type DupePair } from '../lib/api';
  import { dupes, levels } from '../lib/dupes.svelte';
  import { formatDuration, formatSize, viewURL } from '../lib/results.svelte';
  import { toasts } from '../lib/toast.svelte';
  import Dropdown from '../components/Dropdown.svelte';
  import Icon from '../components/Icon.svelte';
  import { t, num, date } from '../lib/i18n.svelte';

  type Side = 'a' | 'b';

  let pairs = $state<DupePair[]>([]);
  let total = $state(0);
  let progress = $state({ checked: 0, total: 0 });
  let loaded = $state(false);
  const skipped = new SvelteSet<string>();
  const key = (p: DupePair) => `${p.a.id}-${p.b.id}`;

  let cur = $derived(pairs[0] as DupePair | undefined);
  let remaining = $derived(Math.max(0, total - skipped.size));
  let checking = $derived(progress.checked < progress.total);
  let percent = $derived(progress.total ? Math.floor((progress.checked / progress.total) * 100) : 100);

  // ---- view: flip (one at a time, in place) or side by side
  const wide = typeof matchMedia === 'function' && matchMedia('(min-width: 900px)').matches;
  let mode = $state<'flip' | 'side'>(stored('vitrine.dupeView', wide ? 'side' : 'flip') as 'flip' | 'side');
  let shown = $state<Side>('a');
  function stored(k: string, fallback: string) {
    try {
      return localStorage.getItem(k) ?? fallback;
    } catch {
      return fallback;
    }
  }
  function setMode(m: 'flip' | 'side') {
    mode = m;
    try {
      localStorage.setItem('vitrine.dupeView', m);
    } catch {
      /* this session only */
    }
  }

  // ---- loading
  let loading = false;
  async function load() {
    if (loading) return;
    loading = true;
    try {
      const r = await api.dupes(dupes.level, Math.min(500, 30 + skipped.size));
      const fresh = r.pairs.filter((p) => !skipped.has(key(p)));
      // Keep the pair on screen first (no jump while it's being looked at).
      pairs = cur && fresh.some((p) => key(p) === key(cur!)) ? [cur, ...fresh.filter((p) => key(p) !== key(cur!))] : fresh;
      total = r.total;
      progress = r.progress;
      dupes.count = r.total;
    } catch (e) {
      toasts.error((e as Error).message);
    } finally {
      loading = false;
      loaded = true;
    }
  }
  onMount(() => {
    load();
    // While the library is still being looked through, the counts follow.
    const t = setInterval(() => {
      if (checking || !pairs.length) load();
    }, 8000);
    return () => clearInterval(t);
  });
  function changeLevel(v: number) {
    dupes.setLevel(v);
    skipped.clear();
    pairs = [];
    load();
  }

  // ---- deciding
  // The last choice, undone from the Undo button (no notice on every choice).
  let last = $state<{ pair: DupePair; keep: Side | 'both' } | null>(null);
  let lastText = $derived(
    !last ? '' : last.keep === 'both' ? t('dupes.keptBoth') : t('dupes.kept', { keep: last.keep.toUpperCase(), drop: last.keep === 'a' ? 'B' : 'A' }),
  );
  async function decide(keep: Side | 'both') {
    const p = cur;
    if (!p) return;
    const dropped = keep === 'a' ? p.b.id : keep === 'b' ? p.a.id : 0;
    // Gone from the list at once (with any other pair of the deleted file).
    pairs = pairs.filter((x) => x !== p && x.a.id !== dropped && x.b.id !== dropped);
    total = Math.max(0, total - 1);
    dupes.count = total;
    shown = 'a';
    try {
      await api.decideDupe(p.a.id, p.b.id, keep);
      last = { pair: p, keep };
    } catch (e) {
      toasts.error((e as Error).message);
    }
    if (pairs.length < 5 && remaining > pairs.length) load();
    else if (dropped) load(); // the deleted file's other pairs leave the count too
  }
  async function undo(p: DupePair) {
    try {
      await api.undoDupe(p.a.id, p.b.id);
      if (last?.pair === p) last = null;
      skipped.delete(key(p));
      pairs = [p, ...pairs.filter((x) => key(x) !== key(p))];
      shown = 'a';
      await load();
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
  function skip() {
    if (!cur) return;
    skipped.add(key(cur));
    pairs = pairs.slice(1);
    shown = 'a';
    if (pairs.length < 5 && remaining > pairs.length) load();
  }

  // ---- comparing: which is better at what, and which to keep
  type Fact = { label: string; a: string; b: string; win: Side | null };
  const pick = (a: number, b: number, margin = 0.01): Side | null =>
    Math.abs(a - b) <= Math.max(a, b) * margin ? null : a > b ? 'a' : 'b';
  function facts(p: DupePair): Fact[] {
    const { a, b } = p;
    const out: Fact[] = [
      { label: t('dupes.fact.size'), a: `${num(a.width)} × ${num(a.height)}`, b: `${num(b.width)} × ${num(b.height)}`, win: pick(a.width * a.height, b.width * b.height) },
      { label: t('dupes.fact.file'), a: `${formatSize(a.size)} ${a.type}`, b: `${formatSize(b.size)} ${b.type}`, win: pick(a.size, b.size, 0.02) },
    ];
    if (a.duration_ms || b.duration_ms) {
      out.push({ label: t('filter.duration.label'), a: formatDuration(a.duration_ms), b: formatDuration(b.duration_ms), win: pick(a.duration_ms, b.duration_ms) });
    }
    out.push({ label: t('tags.title'), a: num(a.tags), b: num(b.tags), win: pick(a.tags, b.tags, 0) });
    if (a.sites?.length || b.sites?.length) {
      out.push({ label: t('filterMenu.fromShort'), a: sites(a), b: sites(b), win: pick(a.sites?.length ?? 0, b.sites?.length ?? 0, 0) });
    }
    out.push({ label: t('dupes.fact.added'), a: day(a.imported_at), b: day(b.imported_at), win: null });
    return out;
  }
  const sites = (f: DupeFile) => (f.sites?.length ? f.sites.join(', ') : '—');
  const day = (ms: number) => date(ms, { day: 'numeric', month: 'short', year: 'numeric' });

  /** The one to keep, and why: the bigger picture, else the bigger file, else more tags, else the older. */
  function suggest(p: DupePair): { side: Side; why: string } {
    const px = pick(p.a.width * p.a.height, p.b.width * p.b.height);
    if (px) return { side: px, why: t('dupes.why.bigger') };
    if (p.a.duration_ms || p.b.duration_ms) {
      const len = pick(p.a.duration_ms, p.b.duration_ms, 0.02);
      if (len) return { side: len, why: t('dupes.why.longer') };
    }
    const size = p.a.type === p.b.type ? pick(p.a.size, p.b.size, 0.02) : null;
    if (size) return { side: size, why: t('dupes.why.quality') };
    const tags = pick(p.a.tags, p.b.tags, 0);
    if (tags) return { side: tags, why: t('dupes.why.tags') };
    return { side: p.a.imported_at <= p.b.imported_at ? 'a' : 'b', why: t('dupes.why.first') };
  }
  /** Different colors (the hash only sees light and dark): likely a variant, not a copy.
   *  Compared on the 4x4 color previews every file has. */
  function colorsDiffer(p: DupePair): boolean {
    if (!p.a.placeholder || !p.b.placeholder) return false;
    const x = atob(p.a.placeholder);
    const y = atob(p.b.placeholder);
    if (x.length !== y.length || !x.length) return false;
    let sum = 0;
    for (let i = 0; i < x.length; i++) sum += Math.abs(x.charCodeAt(i) - y.charCodeAt(i));
    return sum / x.length > 28;
  }
  let rows = $derived(cur ? facts(cur) : []);
  let variant = $derived(cur ? colorsDiffer(cur) : false);
  let best = $derived(cur && !variant ? suggest(cur) : null);
  let same = $derived(cur?.distance === 0 && !variant);

  // ---- videos play muted and always in step: each is asked to play at once (an iPhone loads nothing until then -
  // waiting for both to be ready first left them still until a pause and play), then the other one follows the
  // leading one's clock (small drift: a touch faster or slower; a jump: one seek, then time to load - seeking
  // every frame while it loaded kept a slower video frozen for good). While the leader waits for data the other
  // waits too. One bar scrubs both; in flip mode the hidden one keeps going, so a flip shows the same moment.
  const videos: Partial<Record<Side, HTMLVideoElement>> = {};
  let vt = $state(0); // the leading video's time and length (s)
  let vdur = $state(0);
  let vplaying = $state(true);
  let scrubbing = false;
  const asked = new WeakSet<HTMLVideoElement>(); // a play() on its way
  let holdUntil = 0; // the ready one waits until then (the other is loading)
  const HOLD_MAX = 4000; // ms: the longest wait (a safety net: on a home network a wait is a few hundred ms)
  let heldSince = 0;
  let buffering = $state(false); // held a while: a spinner says so
  const lastSeek = new WeakMap<HTMLVideoElement, number>();
  /** Plays a video (once at a time); a browser that refuses (no autoplay) shows Play on the bar instead. */
  function play(v: HTMLVideoElement) {
    if (asked.has(v)) return;
    asked.add(v);
    v.play()
      .catch((e: Error) => {
        if (e.name === 'NotAllowedError') vplaying = false;
      })
      .finally(() => asked.delete(v));
  }
  const hasVideo = $derived(!!cur && (cur.a.mime.startsWith('video/') || cur.b.mime.startsWith('video/')));
  // (not one the browser can't play - an error: it never gets ready, and nothing should wait for it)
  const allVideos = () => (['a', 'b'] as const).map((x) => videos[x]).filter((v): v is HTMLVideoElement => !!v && v.isConnected && !v.error);
  /** The video the others follow: the one on show in flip mode, else the longer one. */
  const leader = () => {
    const vs = allVideos();
    const shownOne = videos[shown];
    if (mode === 'flip' && shownOne && vs.includes(shownOne)) return shownOne;
    return vs.reduce<HTMLVideoElement | undefined>((best, v) => (!best || (v.duration || 0) > (best.duration || 0) ? v : best), undefined);
  };
  // Each element joins in at the moment on show once it knows its length (side by side ↔ flip makes new ones;
  // a new pair starts at 0). Done here, not on loadedmetadata: WebKit sometimes had that before it was listened to.
  const joined = new WeakSet<HTMLVideoElement>();
  const joining = new WeakMap<HTMLVideoElement, number>(); // (since when)
  $effect(() => {
    if (!hasVideo) return;
    void cur;
    vplaying = true;
    vt = 0;
    let raf = 0;
    const tick = () => {
      raf = requestAnimationFrame(tick);
      const lead = leader();
      if (!lead) return;
      for (const v of allVideos()) {
        if (v.readyState >= 1 && !joined.has(v) && !v.seeking) {
          // (until it's there - WebKit sometimes drops a seek this early - or a second has gone)
          const at = Math.min(vt, Math.max(0, (v.duration || Infinity) - 0.05));
          const since = joining.get(v) ?? joining.set(v, performance.now()).get(v)!;
          if (Math.abs(v.currentTime - at) < 0.3 || performance.now() - since > 1000) joined.add(v);
          else v.currentTime = at;
        }
      }
      if (joined.has(lead)) {
        // (not a new element's 0 before it has joined)
        vt = lead.currentTime;
        // The bar: the leader's length (in flip mode the one on show; side by side the longer one).
        vdur = Number.isFinite(lead.duration) ? lead.duration : 0;
      }
      if (scrubbing) return;
      // The ready one waits while the other loads (a moment more: no play-pause flutter), then they go on together -
      // the loading one keeps playing meanwhile (a paused video may load nothing more: paused, both waited for good),
      // and never longer than a moment in all (then both play; the slow one catches up as it can).
      const vs = allVideos();
      const now = performance.now();
      if (vs.some((v) => v.seeking || v.readyState < 3)) holdUntil = now + 250;
      if (now >= holdUntil) heldSince = 0;
      else heldSince ||= now;
      const hold = now < holdUntil && now - heldSince < HOLD_MAX;
      buffering = vplaying && hold && now - heldSince > 400;
      for (const v of vs) {
        if (!vplaying) {
          if (!v.paused) v.pause();
        } else if (!hold || v.readyState < 3) play(v); // (also an iPhone: it loads nothing until asked to play)
        else if (!v.paused) v.pause(); // (waits for the other)
      }
      // (not before the leader knows where it is - a new element's 0 would pull the other back to the start - nor
      // while it seeks there)
      if (!joined.has(lead) || lead.seeking) return;
      for (const v of vs) {
        if (v === lead) continue;
        if (v.readyState < 3 || v.seeking) continue; // (still loading: a seek would only start it again)
        const end = Math.max(0, (v.duration || Infinity) - 0.05);
        const target = Math.min(lead.currentTime, end);
        const drift = v.currentTime - target;
        if (Math.abs(drift) > 0.3) {
          // One seek (both wait while it lands), then a second to settle.
          if (now - (lastSeek.get(v) ?? 0) > 1000) {
            lastSeek.set(v, now);
            v.currentTime = target;
          }
          v.playbackRate = 1;
        } else {
          v.playbackRate = Math.abs(drift) < 0.02 ? 1 : 1 - Math.max(-0.1, Math.min(0.1, drift * 0.8));
        }
      }
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  });
  function togglePlay() {
    vplaying = !vplaying;
    for (const v of allVideos()) {
      if (vplaying) play(v); // (in the tap itself: a browser that refused autoplay allows this)
      else v.pause();
    }
  }
  function scrub(to: number) {
    scrubbing = true;
    vt = to;
    for (const v of allVideos()) v.currentTime = Math.min(to, Math.max(0, (v.duration || Infinity) - 0.05));
  }
  function scrubEnd() {
    scrubbing = false;
  }
  function flip(to?: Side) {
    const next = to ?? (shown === 'a' ? 'b' : 'a');
    if (next === shown) return;
    // The hidden one kept in step already: a seek only if it fell behind (a seek stalls the picture just shown).
    const from = videos[shown];
    const into = videos[next];
    if (from && into && into.readyState >= 1 && Math.abs(into.currentTime - from.currentTime) > 0.3) into.currentTime = from.currentTime;
    shown = next;
  }

  // The next pair's pictures load meanwhile.
  $effect(() => {
    for (const f of [pairs[1]?.a, pairs[1]?.b]) {
      if (f && f.mime.startsWith('image/')) new Image().src = viewURL(f);
    }
  });

  function onKey(e: KeyboardEvent) {
    const t = e.target as HTMLElement;
    if (e.metaKey || e.ctrlKey || e.altKey || t.closest('input, textarea, [role="listbox"]')) return;
    const k = e.key.toLowerCase();
    if (k === 'z' && last) undo(last.pair);
    if (!cur) return;
    const act: Record<string, () => void> = {
      ' ': () => flip(),
      arrowleft: () => flip('a'),
      arrowright: () => flip('b'),
      '1': () => decide('a'),
      a: () => decide('a'),
      '2': () => decide('b'),
      b: () => decide('b'),
      '3': () => decide('both'),
      k: () => decide('both'),
      s: skip,
    };
    if (act[k]) {
      e.preventDefault();
      act[k]();
    }
  }
</script>

<svelte:window onkeydown={onKey} />

{#snippet media(f: DupeFile, side: Side)}
  {#if f.mime.startsWith('video/')}
    <video bind:this={videos[side]} src={f.url} poster={f.thumb} muted loop playsinline preload="auto"></video>
  {:else}
    <img
      src={viewURL(f)}
      alt={side === 'a' ? 'File A' : 'File B'}
      draggable="false"
      decoding="async"
      style:background-image={f.thumb ? `url(${f.thumb})` : undefined} />
  {/if}
{/snippet}

{#snippet undoButton()}
  <button
    class="quiet undo"
    transition:pop
    disabled={!last}
    onclick={() => last && undo(last.pair)}
    aria-label={last ? t('dupes.undoWhat', { what: lastText }) : t('common.undo')}
    title={last ? t('dupes.undoKey', { what: lastText }) : t('dupes.nothingToUndo')}><Icon name="undo" size={16} /> <span class="undo-label">{t('common.undo')}</span></button
  >
{/snippet}

{#snippet badge(side: Side)}
  <span class="badge {side}">{side.toUpperCase()}</span>
  {#if best?.side === side}<span class="suggested" transition:pop><Icon name="star" size={12} /> {t('dupes.suggested')}</span>{/if}
{/snippet}

<div class="dupes">
  <header class="head">
    <h1>{t('menu.duplicates')}</h1>
    {#if loaded && remaining > 0}<span class="count" transition:pop>{t('dupes.toCheck', { n: remaining })}</span>{/if}
    <span class="spacer"></span>
    <Dropdown
      value={String(dupes.level)}
      options={levels.map((l) => ({ ...l, value: String(l.value) }))}
      onchange={(v) => changeLevel(Number(v))} label={t('dupes.howAlike')} heading={t('dupes.showPairs')} align="end" />
  </header>

  {#if checking && progress.total > 0}
    <div class="progress" role="status" transition:reveal>
      <div class="bar"><span style:width="{percent}%"></span></div>
      <span class="muted">{t('dupes.looking', { checked: progress.checked, n: progress.total })}</span>
    </div>
  {/if}

  {#if !loaded}
    <div class="nothing" in:swap></div>
  {:else if !cur}
    <div class="nothing" in:swap>
      <div class="empty-card">
        <span class="empty-icon"><Icon name={checking ? 'clock' : 'check'} size={28} /></span>
        {#if checking}
          <h2>{t('dupes.nothingYet')}</h2>
          <p class="muted">{t('dupes.nothingYetHint')}</p>
        {:else}
          <h2>{t('dupes.none')}</h2>
          <p class="muted">{t('dupes.noneHint')}</p>
          {#if last}{@render undoButton()}{/if}
          {#if dupes.level < 10}
            <button class="quiet" onclick={() => changeLevel(levels.find((l) => l.value > dupes.level)!.value)} transition:pop>{t('dupes.lessAlike')}</button>
          {/if}
        {/if}
      </div>
    </div>
  {:else}
    {#key key(cur)}
      <div class="stage" class:side={mode === 'side'}>
        {#if buffering}<span class="buffering spinner" role="status" aria-label={t('common.loading')} transition:pop></span>{/if}
        {#if mode === 'side'}
          {#each ['a', 'b'] as const as s (s)}
            <div class="pane">
              {@render media(cur[s], s)}
              <div class="tags-row">{@render badge(s)}</div>
            </div>
          {/each}
        {:else}
          <button class="pane flip" aria-label={t('dupes.switch')} onclick={() => flip()}>
            {#each ['a', 'b'] as const as s (s)}
              <div class="layer" class:hidden={shown !== s} aria-hidden={shown !== s}>{@render media(cur[s], s)}</div>
            {/each}
            <!-- (a pop on each flip: that it switched shows even when the two look alike) -->
            {#key shown}<div class="tags-row swapped">{@render badge(shown)}</div>{/key}
            <span class="hint">{t('dupes.tapToSwitch')}</span>
          </button>
        {/if}
      </div>
    {/key}

    {#if hasVideo}
      <!-- One bar for both videos: play/pause, where they are, scrub both. -->
      <!-- (fades in place: sliding open would make the pictures above shrink while it did) -->
      <div class="vbar" in:swap>
        <button class="icon" aria-label={vplaying ? t('dupes.pauseBoth') : t('dupes.playBoth')} title={vplaying ? t('video.pause') : t('video.play')} onclick={togglePlay}
          ><Icon name={vplaying ? 'pause' : 'play'} size={18} /></button>
        <input
          type="range"
          min="0"
          max={vdur || 1}
          step="0.01"
          value={vt}
          aria-label={t('dupes.position')}
          oninput={(e) => scrub(Number((e.currentTarget as HTMLInputElement).value))}
          onchange={scrubEnd}
          onpointerup={scrubEnd} />
        <span class="vtime">{formatDuration(vt * 1000)} / {formatDuration(vdur * 1000)}</span>
      </div>
    {/if}

    <div class="bottom">
      <div class="facts" class:flipping={mode === 'flip'}>
        {#each ['a', 'b'] as const as s (s)}
          <button
            class="card-fact {s}"
            class:on={mode === 'flip' && shown === s}
            aria-pressed={mode === 'flip' ? shown === s : undefined}
            tabindex={mode === 'flip' ? 0 : -1}
            onclick={() => mode === 'flip' && flip(s)}>
            <span class="fact-head">{@render badge(s)}</span>
            {#each rows as r (r.label)}
              <span class="fact" class:win={r.win === s} title={r.label}>
                <span class="fact-label">{r.label}</span><span class="fact-value">{r[s]}</span>
              </span>
            {/each}
          </button>
        {/each}
      </div>
      <p class="why muted">
        {#if variant}{t('dupes.variant')}{/if}
        {#if same}{t('dupes.same')}{/if}
        {#if best}{t('dupes.suggestKeep', { side: best.side.toUpperCase(), why: best.why })}{/if}
        {t('dupes.merge')}
      </p>
      <div class="actions">
        <button class:quiet={best?.side !== 'a'} onclick={() => decide('a')} title={t('dupes.keepATitle')}>{t('dupes.keepA')}</button>
        <button class:quiet={!variant} onclick={() => decide('both')} title={t('dupes.keepBothTitle')}>{t('dupes.keepBoth')}</button>
        <button class:quiet={best?.side !== 'b'} onclick={() => decide('b')} title={t('dupes.keepBTitle')}>{t('dupes.keepB')}</button>
      </div>
      <div class="sub-actions">
        <div class="segmented view" role="tablist" aria-label={t('dupes.howCompare')}>
          <button role="tab" aria-selected={mode === 'flip'} onclick={() => setMode('flip')}>{t('dupes.flip')}</button>
          <button role="tab" aria-selected={mode === 'side'} onclick={() => setMode('side')}>{t('dupes.side')}</button>
        </div>
        <span class="spacer"></span>
        {@render undoButton()}
        <button class="quiet skip" onclick={skip} title={t('dupes.skipTitle')}>{t('setup.skip')} <span class="skip-icon"><Icon name="right" size={16} /></span></button>
      </div>
    </div>
  {/if}
</div>

<style>
  .dupes {
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: calc(var(--bar-h, 0px) + 12px) calc(12px + var(--safe-corner, 0px)) calc(12px + var(--bottom-room)); /* (room for the top bar; clear of the screen's corners) */
    max-width: 1200px;
    margin: 0 auto;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: var(--control-h);
  }
  .head h1 {
    font-size: 24px;
    font-weight: 700;
    margin: 0 2px;
    letter-spacing: -0.01em;
  }
  .count {
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .spacer {
    flex: 1;
  }
  .progress {
    display: grid;
    gap: 6px;
    font-size: 13px;
  }
  .bar {
    height: 4px;
    border-radius: 2px;
    background: var(--surface0);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--mauve);
    transition: width 0.4s ease-out;
  }

  .nothing {
    flex: 1;
    display: grid;
    place-items: center;
  }
  .empty-card {
    display: grid;
    justify-items: center;
    gap: 8px;
    text-align: center;
    max-width: 340px;
    padding: 24px;
  }
  .empty-card h2 {
    margin: 4px 0 0;
    font-size: 18px;
  }
  .empty-card p {
    margin: 0 0 8px;
  }
  .empty-icon {
    display: grid;
    place-items: center;
    width: 56px;
    height: 56px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--mauve) 14%, transparent);
    color: var(--mauve);
  }

  /* ---- the pictures */
  .stage {
    flex: 1;
    min-height: 0;
    display: grid;
    gap: 8px;
    position: relative;
  }
  .stage.side {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }
  .pane {
    position: relative;
    min-height: 0;
    min-width: 0;
    border-radius: 12px;
    background: var(--crust);
    overflow: hidden;
  }
  button.pane {
    display: block;
    width: 100%;
    height: 100%;
    padding: 0;
    border: 0;
    cursor: pointer;
    font-weight: inherit;
    color: inherit;
  }
  @media (hover: hover) {
    button.pane:hover {
      filter: none !important;
    }
  }
  .layer {
    position: absolute;
    inset: 0;
  }
  .buffering {
    position: absolute;
    z-index: 3;
    left: 50%;
    top: 50%;
    width: 30px;
    height: 30px;
    border-width: 3px;
    margin: -15px 0 0 -15px;
    filter: drop-shadow(0 1px 4px rgb(0 0 0 / 0.6));
    pointer-events: none;
  }
  /* (still drawn, under the one on show: a hidden video may stop being drawn, and show an old frame on a flip) */
  .layer.hidden {
    opacity: 0;
  }
  .pane img,
  .pane video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    background-size: contain;
    background-position: center;
    background-repeat: no-repeat;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
  }
  .tags-row {
    position: absolute;
    top: 10px;
    left: 10px;
    display: flex;
    gap: 6px;
    align-items: center;
    pointer-events: none;
  }
  .badge {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: 8px;
    font-weight: 800;
    font-size: 14px;
    color: var(--crust);
    box-shadow: 0 1px 4px rgb(0 0 0 / 0.4);
  }
  .badge.a {
    background: var(--blue);
  }
  .swapped .badge {
    animation: badge-pop 0.3s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  @keyframes badge-pop {
    from {
      transform: scale(1.6);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .swapped .badge {
      animation: none;
    }
  }
  .badge.b {
    background: var(--peach);
  }
  .suggested {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 8px;
    border-radius: 999px;
    background: rgb(17 17 27 / 0.75);
    color: var(--yellow);
    font-size: 12px;
    font-weight: 600;
  }
  .hint {
    position: absolute;
    bottom: 10px;
    left: 50%;
    transform: translateX(-50%);
    padding: 3px 10px;
    border-radius: 999px;
    background: rgb(17 17 27 / 0.7);
    color: var(--subtext);
    font-size: 12px;
    pointer-events: none;
  }

  /* ---- facts and actions */
  .vbar {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .vbar input {
    flex: 1;
    min-width: 0;
  }
  .vtime {
    flex: none;
    font-size: var(--font-small);
    color: var(--subtext);
    font-variant-numeric: tabular-nums;
  }
  .bottom {
    display: grid;
    min-width: 0;
    gap: 10px;
  }
  .facts {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 8px;
  }
  .card-fact {
    display: grid;
    align-content: start;
    justify-content: stretch;
    gap: 3px;
    padding: 8px 10px;
    min-height: 0;
    border-radius: 10px;
    border: 1px solid var(--surface0);
    background: var(--mantle);
    color: var(--text);
    font-weight: 400;
    text-align: left;
    white-space: normal;
    cursor: default;
  }
  .flipping .card-fact {
    cursor: pointer;
  }
  .card-fact.on.a {
    border-color: color-mix(in srgb, var(--blue) 60%, transparent);
  }
  .card-fact.on.b {
    border-color: color-mix(in srgb, var(--peach) 60%, transparent);
  }
  @media (hover: hover) {
    .card-fact:hover {
      filter: none !important;
    }
  }
  .fact-head {
    display: flex;
    gap: 6px;
    align-items: center;
    margin-bottom: 3px;
  }
  .fact-head .badge {
    width: 22px;
    height: 22px;
    font-size: 12px;
    border-radius: 6px;
    box-shadow: none;
  }
  .fact-head .suggested {
    background: color-mix(in srgb, var(--yellow) 12%, transparent);
    padding: 2px 7px;
  }
  .fact {
    display: flex;
    gap: 8px;
    font-size: 13px;
    line-height: 1.35;
    min-width: 0;
  }
  .fact-label {
    flex: none;
    width: 46px;
    color: var(--overlay0);
  }
  .fact-value {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .fact.win .fact-value {
    color: var(--green);
    font-weight: 600;
  }
  .why {
    margin: 0;
    font-size: 12.5px;
    text-align: center;
  }
  .actions {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 8px;
  }
  .sub-actions {
    display: flex;
    min-width: 0;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  /* Never narrower than its labels: Undo gives up its word on small screens instead. */
  .view {
    flex: none;
  }
  .view button {
    flex: none;
    white-space: nowrap;
    padding: 4px 14px;
  }
  .skip-icon {
    display: contents;
  }
  @media (max-width: 359px) {
    .skip-icon {
      display: none;
    }
  }
  @media (max-width: 599px) {
    .undo-label {
      display: none;
    }
    .undo {
      width: var(--control-h);
      padding: 0;
    }
  }
  .sub-actions .spacer {
    flex: 1;
  }
  .undo:disabled {
    opacity: 0.35;
  }
  .undo,
  .skip {
    flex: none;
    padding: 0 8px;
    border-color: transparent;
    color: var(--subtext);
  }
  @media (min-width: 900px) {
    .dupes {
      padding-top: calc(var(--bar-h, 0px) + 24px); /* (room for the top bar too) */
    }
    .bottom {
      grid-template-columns: 1fr auto;
      grid-template-areas: 'facts facts' 'why why' 'actions sub';
      align-items: center;
    }
    .facts {
      grid-area: facts;
    }
    .why {
      grid-area: why;
    }
    .actions {
      grid-area: actions;
      max-width: 520px;
    }
    .sub-actions {
      grid-area: sub;
    }
  }
</style>
