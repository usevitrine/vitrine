<script lang="ts">
  // Vitrine's own video controls (never the iOS player). They live in a layer
  // above the swipeable slides, not inside them, so scrubbing can't turn into
  // a swipe on iOS.
  import { untrack } from 'svelte';
  import { settings } from '../lib/settings.svelte';
  import { formatDuration } from '../lib/results.svelte';
  import { api, type SpriteInfo } from '../lib/api';
  import Icon from './Icon.svelte';
  import { fullscreen } from '../lib/fullscreen.svelte';
  import { volume } from '../lib/volume.svelte';
  import { t } from '../lib/i18n.svelte';

  /** The preview frame to show in the player while scrubbing (the viewer draws it over the video). */
  export type ScrubFrame = { url: string; size: string; pos: string };

  let {
    video,
    fileId = 0,
    scrubFrame = $bindable(null),
  }: { video: HTMLVideoElement; fileId?: number; scrubFrame?: ScrubFrame | null } = $props();

  let paused = $state(true);
  let muted = $state(true);
  let level = $state(1); // this video's volume (shown on the slider)
  let time = $state(0);
  let duration = $state(0);
  let buffered = $state(0);
  let blockedSound = $state(false);
  let scrubbing = $state(false);
  let track = $state<HTMLDivElement>();

  // Wire up the current video: start it as the settings say.
  $effect(() => {
    const v = video;
    const sync = () => {
      paused = v.paused;
      muted = v.muted;
      level = v.volume;
      if (!scrubbing) time = v.currentTime;
      duration = Number.isFinite(v.duration) ? v.duration : 0;
      buffered = v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0;
    };
    const events = ['play', 'pause', 'timeupdate', 'durationchange', 'volumechange', 'progress', 'loadedmetadata'];
    events.forEach((e) => v.addEventListener(e, sync));
    // Settings are read untracked: toggling loop or speed must not restart
    // or re-mute the video (their own effects below apply them).
    untrack(() => {
      v.loop = settings.loop;
      v.playbackRate = settings.speed;
      v.muted = settings.startMuted;
      if (volume.settable) v.volume = volume.level; // this device's level
    });
    blockedSound = false;
    // Paused meanwhile (the space bar, a tap): the browser's refusal can come late - it mustn't start it again.
    let pausedMeanwhile = false;
    const onPause = () => (pausedMeanwhile = true);
    v.addEventListener('pause', onPause, { once: true });
    v.play().catch(() => {
      // iOS blocks autoplay with sound: play muted and offer sound.
      v.removeEventListener('pause', onPause);
      if (!v.muted && !pausedMeanwhile) {
        v.muted = true;
        blockedSound = true;
        v.play().catch(() => {});
      }
    });
    // Untracked: sync reads `scrubbing`, which must not re-run this setup (it
    // would restart and re-mute the video every time scrubbing starts or ends).
    untrack(sync);
    return () => events.forEach((e) => v.removeEventListener(e, sync));
  });

  $effect(() => {
    video.loop = settings.loop;
  });
  // A seamless loop: the browser's own loop lets the video end, then starts it
  // again (a pause - long in Safari). Just before the end (a tenth of a second,
  // at the speed it plays) it's sent back to the start instead, so it never
  // ends; the browser's loop stays on underneath as a fallback. Checked every
  // frame (requestVideoFrameCallback where there is one), not while scrubbing.
  $effect(() => {
    const v = video as HTMLVideoElement & { requestVideoFrameCallback?: (cb: () => void) => number; cancelVideoFrameCallback?: (h: number) => void };
    let handle = 0;
    let live = true;
    const frames = typeof v.requestVideoFrameCallback === 'function';
    const check = () => {
      if (!live) return;
      const d = v.duration;
      if (settings.loop && !v.paused && !v.seeking && !scrubbing && !finishing && Number.isFinite(d) && d > 0.5 &&
          d - v.currentTime < 0.1 * Math.max(1, v.playbackRate)) {
        v.currentTime = 0;
      }
      handle = frames ? v.requestVideoFrameCallback!(check) : requestAnimationFrame(check);
    };
    handle = frames ? v.requestVideoFrameCallback!(check) : requestAnimationFrame(check);
    return () => {
      live = false;
      if (frames) v.cancelVideoFrameCallback?.(handle);
      else cancelAnimationFrame(handle);
    };
  });
  $effect(() => {
    video.playbackRate = settings.speed;
  });

  function toggle() {
    if (video.paused) video.play().catch(() => {});
    else video.pause();
  }

  function toggleMute() {
    video.muted = !video.muted; // only this video; the setting decides how the next one starts
    if (!video.muted && volume.settable) video.volume = volume.level; // back to this device's level
    blockedSound = false;
  }
  // The volume slider (where the browser allows it): moving it unmutes; the
  // level is remembered on this device.
  function setVolume(v: number) {
    volume.set(v);
    video.volume = volume.level;
    video.muted = volume.level === 0;
    blockedSound = false;
  }
  function wheelVolume(e: WheelEvent) {
    if (!volume.settable) return;
    e.preventDefault();
    const base = video.muted ? 0 : video.volume;
    setVolume(Math.round((base + (e.deltaY < 0 ? 0.05 : -0.05)) * 100) / 100);
  }

  // ---- scrubbing (like hydrus-web: the picture itself follows the finger).
  // The bar and time follow at once; the player shows the preview frame for
  // that spot straight away, while the video seeks as fast as it can decode -
  // one seek at a time, only the latest position kept - and takes over (sharp)
  // whenever it shows the same moment. Playback pauses while scrubbing.
  let trackRect: DOMRect | null = null;
  let resume = false;
  let seeking = $state(false); // a jump of the video is on its way (it may show black until it lands)
  let pending: number | null = null;
  let finishing = $state(false); // released: the exact seek is on its way

  function startScrub(e: PointerEvent) {
    scrubbing = true;
    trackRect = track!.getBoundingClientRect(); // once, not on every move
    resume = !video.paused;
    video.pause();
    scrubTo(e.clientX);
  }

  let rail = $state<HTMLDivElement>();

  function scrubTo(clientX: number) {
    if (!trackRect || !duration) return;
    const f = Math.min(1, Math.max(0, (clientX - trackRect.left) / trackRect.width));
    rail?.style.setProperty('--p', String(f)); // this frame, before Svelte's update
    time = f * duration;
    quickSeek(time);
  }

  function quickSeek(t: number) {
    if (seeking) {
      pending = t; // only the latest position matters
      return;
    }
    seeking = true;
    // 'seeked' can be skipped (same spot): don't wait for it forever - but a
    // slow one (a busy phone, a slow connection) is still waited for: a seek
    // started on top of it would queue.
    let timer = 0;
    const check = () => {
      if (video.seeking) timer = window.setTimeout(check, 200);
      else done();
    };
    timer = window.setTimeout(check, 400);
    const done = () => {
      clearTimeout(timer);
      if (!seeking) return;
      seeking = false;
      if (pending !== null && scrubbing) {
        const next = pending;
        pending = null;
        quickSeek(next);
      }
    };
    video.addEventListener('seeked', done, { once: true });
    const v = video as HTMLVideoElement & { fastSeek?: (t: number) => void };
    if (v.fastSeek) v.fastSeek(t);
    else v.currentTime = t;
  }

  function endScrub() {
    if (!scrubbing) return;
    scrubbing = false;
    trackRect = null;
    pending = null;
    finishing = true;
    video.addEventListener(
      'seeked',
      () => {
        finishing = false;
      },
      { once: true },
    );
    setTimeout(() => (finishing = false), 1500); // never stuck showing the preview
    video.currentTime = time; // the exact spot
    if (resume) video.play().catch(() => {});
  }

  // ---- preview frames above the scrub bar (while scrubbing; on desktop also on hover)
  let sprite = $state<SpriteInfo | null>(null);
  let spriteReady = $state(false); // its picture is loaded (until then an overlay would be empty)
  $effect(() => {
    const id = fileId;
    sprite = null;
    spriteReady = false;
    if (!id) return;
    let live = true;
    api
      .sprite(id)
      .then((s) => {
        if (!live) return;
        const img = new Image(); // fetched now, so the first frame shows at once
        img.onload = () => live && (spriteReady = true);
        img.src = s.url;
        sprite = s;
      })
      .catch(() => {}); // not made yet: just the time
    return () => (live = false);
  });

  let hoverX = $state<number | null>(null); // px from the track's left edge (mouse over it)
  let trackW = $state(0);
  /** Where the preview points: the scrub position, or the hovered one. */
  // The preview frame in the player: while scrubbing (and until the exact seek
  // lands), unless the video already shows that moment.
  $effect(() => {
    const s = sprite;
    // One picture at a time: while the finger is on the bar, always the preview
    // frame (the video jumps silently underneath); after letting go it stays
    // until the exact jump lands, then the video takes over - once. (Switching
    // whenever the video happened to be near flickered between two frames.)
    if (!s || !spriteReady || !duration || !(scrubbing || finishing)) {
      scrubFrame = null;
      return;
    }
    const k = Math.min(s.frames - 1, Math.max(0, Math.floor((time * 1000) / s.interval_ms)));
    const rows = Math.ceil(s.frames / s.cols);
    const col = k % s.cols;
    const row = Math.floor(k / s.cols);
    scrubFrame = {
      url: s.url,
      size: `${s.cols * 100}% ${rows * 100}%`,
      pos: `${s.cols > 1 ? (col / (s.cols - 1)) * 100 : 0}% ${rows > 1 ? (row / (rows - 1)) * 100 : 0}%`,
    };
  });
  $effect(() => () => (scrubFrame = null)); // gone with the controls

  let previewAt = $derived.by(() => {
    if (!duration || !trackW) return null;
    if (scrubbing) return { x: (time / duration) * trackW, t: time, hover: false };
    if (hoverX !== null) return { x: hoverX, t: (hoverX / trackW) * duration, hover: true };
    return null;
  });
  // The small frame above the bar: only when hovering with a mouse (scrubbing shows it in the player).
  let frame = $derived.by(() => {
    if (!sprite || !previewAt?.hover) return null;
    const k = Math.min(sprite.frames - 1, Math.max(0, Math.floor((previewAt.t * 1000) / sprite.interval_ms)));
    const rows = Math.ceil(sprite.frames / sprite.cols);
    return {
      pos: `-${(k % sprite.cols) * sprite.tile_w}px -${Math.floor(k / sprite.cols) * sprite.tile_h}px`,
      size: `${sprite.cols * sprite.tile_w}px ${rows * sprite.tile_h}px`,
    };
  });
  // The bubble stays inside the bar.
  let bubbleW = $derived(frame && sprite ? sprite.tile_w + 4 : 56); // + its border
  let bubbleLeft = $derived(previewAt ? Math.min(Math.max(previewAt.x - bubbleW / 2, 0), Math.max(0, trackW - bubbleW)) : 0);

  const speeds = [0.5, 1, 1.5, 2];

  // Full screen: the viewer, our player in it (never Apple's) - where the browser allows (lib/fullscreen).
</script>

<div class="controls" role="group" aria-label={t('video.controls')} ondragstart={(e) => e.preventDefault()}>
  {#if blockedSound}
    <button class="sound" onclick={toggleMute}><Icon name="volume" size={18} /> {t('video.tapForSound')}</button>
  {/if}
  <div
    class="track"
    bind:this={track}
    bind:clientWidth={trackW}
    role="slider"
    tabindex="0"
    aria-label={t('video.position')}
    aria-valuemin={0}
    aria-valuemax={Math.round(duration)}
    aria-valuenow={Math.round(time)}
    onpointerdown={(e) => {
      e.stopPropagation();
      // No text selection or browser drag (of a picture or selected text) instead of scrubbing.
      e.preventDefault();
      getSelection()?.removeAllRanges();
      track!.dataset.pointer = ''; // pressed, not tabbed to: no focus outline
      track!.focus({ preventScroll: true }); // (the arrow keys still move 5 s)
      track!.setPointerCapture(e.pointerId);
      startScrub(e);
    }}
    onpointermove={(e) => {
      e.stopPropagation();
      if (scrubbing) scrubTo(e.clientX);
      else if (e.pointerType === 'mouse' && track) hoverX = Math.min(trackW, Math.max(0, e.clientX - track.getBoundingClientRect().left));
    }}
    onpointerleave={() => (hoverX = null)}
    onpointerup={(e) => {
      e.stopPropagation();
      endScrub();
    }}
    onpointercancel={endScrub}
    onblur={() => track && delete track.dataset.pointer}
    onkeydown={(e) => {
      if (e.key === 'ArrowRight') video.currentTime = Math.min(duration, video.currentTime + 5);
      if (e.key === 'ArrowLeft') video.currentTime = Math.max(0, video.currentTime - 5);
    }}>
    {#if previewAt}
      <div class="preview" style:left="{bubbleLeft}px" style:width="{bubbleW}px" aria-hidden="true">
        {#if sprite && frame}
          <div
            class="frame"
            style:width="{sprite.tile_w}px"
            style:height="{sprite.tile_h}px"
            style:background-image="url({sprite.url})"
            style:background-position={frame.pos}
            style:background-size={frame.size}>
          </div>
        {/if}
        <span class="at">{formatDuration(previewAt.t * 1000)}</span>
      </div>
    {/if}
    <!-- Position as a 0..1 fraction in --p, drawn with transforms (no layout work per move). -->
    <div class="rail" bind:this={rail} style:--p={duration ? Math.min(1, time / duration) : 0}>
      <div class="buffered" style:width="{duration ? (buffered / duration) * 100 : 0}%"></div>
      <div class="played"></div>
      <div class="knob-lane"><div class="knob"></div></div>
    </div>
  </div>
  <div class="row">
    <button class="icon" aria-label={paused ? t('video.play') : t('video.pause')} onclick={toggle}
      ><Icon name={paused ? 'play' : 'pause'} /></button>
    <!-- The volume beside play: its slider opens to the right, moving only the time. -->
    <div class="vol" class:settable={volume.settable}>
      <button class="icon" aria-label={muted ? t('video.unmute') : t('video.mute')} onclick={toggleMute} onwheel={wheelVolume}
        ><Icon name={muted || level === 0 ? 'mute' : 'volume'} /></button>
      {#if volume.settable}
        <input
          class="vol-slider"
          type="range"
          min="0"
          max="1"
          step="0.01"
          value={muted ? 0 : level}
          style:--p="{(muted ? 0 : level) * 100}%"
          aria-label={t('video.volume')}
          oninput={(e) => setVolume(Number((e.currentTarget as HTMLInputElement).value))} />
      {/if}
    </div>
    <span class="time">{formatDuration(time * 1000)} / {formatDuration(duration * 1000)}</span>
    <span class="spacer"></span>
    <button
      class="icon"
      class:on={settings.loop}
      aria-label={t('video.loop')}
      aria-pressed={settings.loop}
      onclick={() => (settings.loop = !settings.loop)}><Icon name="loop" /></button>
    <button
      class="speed"
      aria-label={t('video.speed')}
      onclick={() => (settings.speed = speeds[(speeds.indexOf(settings.speed) + 1) % speeds.length] ?? 1)}
      >{settings.speed}×</button>
    {#if fullscreen.supported}
      <button
        class="icon"
        aria-label={fullscreen.on ? t('video.exitFullscreen') : t('video.fullscreen')}
        title={fullscreen.on ? t('video.exitFullscreenKey') : t('video.fullscreenKey')}
        onclick={() => fullscreen.toggle(video.closest('.viewer'))}><Icon name={fullscreen.on ? 'shrink' : 'expand'} /></button>
    {/if}
  </div>
</div>

<style>
  .controls {
    display: grid;
    user-select: none;
    -webkit-user-select: none;
    gap: 2px;
    padding: 0 calc(12px + var(--safe-corner, 0px)) calc(8px + var(--safe-bottom) + var(--home-bar, 0px)); /* (clear of the iPhone corners) */
    background: linear-gradient(transparent, rgb(17 17 27 / 0.85));
    touch-action: none;
  }
  .sound {
    justify-self: center;
    display: inline-flex;
    gap: 6px;
    align-items: center;
    margin-bottom: 8px;
  }
  .track {
    position: relative;
    padding: 14px 0;
    cursor: pointer;
    touch-action: none;
  }
  /* Pressed with a finger or the mouse (Safari counts that focus as visible):
     no outline; reached with Tab: the usual one. */
  .track:global([data-pointer]):focus-visible {
    outline: none;
  }
  .preview {
    position: absolute;
    bottom: calc(100% - 4px);
    display: grid;
    justify-items: center;
    gap: 4px;
    pointer-events: none;
  }
  .frame {
    border-radius: 8px;
    background-color: var(--crust);
    background-repeat: no-repeat;
    border: 2px solid rgb(205 214 244 / 0.9);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.55);
    box-sizing: content-box;
  }
  .at {
    padding: 2px 8px;
    border-radius: 6px;
    background: rgb(17 17 27 / 0.85);
    color: var(--text);
    font-size: var(--font-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .rail {
    position: relative;
    height: 4px;
    border-radius: 2px;
    background: rgb(205 214 244 / 0.25);
  }
  .buffered,
  .played {
    position: absolute;
    inset: 0 auto 0 0;
    border-radius: 2px;
  }
  .buffered {
    background: rgb(205 214 244 / 0.35);
  }
  .played {
    width: 100%;
    background: var(--mauve);
    transform: scaleX(var(--p));
    transform-origin: left;
    will-change: transform;
  }
  /* A full-width lane moved by --p of its own width: the knob rides on it. */
  .knob-lane {
    position: absolute;
    inset: 0;
    transform: translateX(calc(var(--p) * 100%));
    will-change: transform;
    pointer-events: none;
  }
  .knob {
    position: absolute;
    left: 0;
    top: 50%;
    width: 14px;
    height: 14px;
    margin: -7px 0 0 -7px;
    border-radius: 50%;
    background: var(--mauve);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .row button {
    color: var(--text);
  }
  /* The volume: the speaker beside play, and a slider that opens to its right on
     hover or focus (where the browser lets a page set the volume - not iOS); only
     the time moves along, the buttons on the right stay put. */
  .vol {
    display: flex;
    align-items: center;
  }
  .vol-slider {
    width: 0;
    min-width: 0;
    opacity: 0;
    margin: 0;
    overflow: hidden; /* (its thumb: at a high level it stuck out over the speaker, unseen, and took its clicks) */
    pointer-events: none;
    transition:
      width 0.18s ease-out,
      opacity 0.18s ease-out,
      margin 0.18s ease-out;
    accent-color: var(--mauve);
  }
  .vol:hover .vol-slider,
  .vol:focus-within .vol-slider {
    width: 84px;
    opacity: 1;
    margin: 0 8px 0 2px;
    overflow: visible;
    pointer-events: auto;
  }
  /* Phones use their volume buttons: no slider there (the remembered level still applies). */
  @media (hover: none) {
    .vol-slider {
      display: none;
    }
  }
  .on {
    color: var(--mauve) !important;
  }
  .time {
    font-variant-numeric: tabular-nums;
    font-size: var(--font-small);
    color: var(--subtext);
  }
  .spacer {
    flex: 1;
  }
  .speed {
    background: transparent;
    color: var(--text);
    padding: 0 8px;
    font-variant-numeric: tabular-nums;
  }
</style>
