<script lang="ts">
  // Crops a comic's cover: the cover with a frame over it - drag inside the
  // frame to move it, a corner to resize it (any shape) - then Save. Reset
  // shows all of the cover again. The crop is fractions of the cover (x, y,
  // width, height); the grid shows only that part, in its shape.
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n.svelte';

  let {
    src,
    crop,
    onsave,
    onclose,
  }: { src: string; crop?: number[]; onsave: (crop: number[] | null) => void; onclose: () => void } = $props();

  // svelte-ignore state_referenced_locally
  let box = $state({ x: crop?.[0] ?? 0, y: crop?.[1] ?? 0, w: crop?.[2] ?? 1, h: crop?.[3] ?? 1 });
  let img = $state<HTMLImageElement>();
  const MIN = 0.08; // (a crop at least this much of the cover each way)

  type Grip = 'move' | 'nw' | 'ne' | 'sw' | 'se';
  let drag: { grip: Grip; x: number; y: number; start: typeof box; id: number } | null = null;
  function down(e: PointerEvent, grip: Grip) {
    e.preventDefault();
    e.stopPropagation();
    drag = { grip, x: e.clientX, y: e.clientY, start: { ...box }, id: e.pointerId };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }
  function move(e: PointerEvent) {
    if (!drag || drag.id !== e.pointerId || !img) return;
    const r = img.getBoundingClientRect();
    const dx = (e.clientX - drag.x) / r.width;
    const dy = (e.clientY - drag.y) / r.height;
    const s = drag.start;
    const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));
    if (drag.grip === 'move') {
      box = { ...s, x: clamp(s.x + dx, 0, 1 - s.w), y: clamp(s.y + dy, 0, 1 - s.h) };
      return;
    }
    let { x, y, w, h } = s;
    if (drag.grip.includes('w')) {
      const nx = clamp(s.x + dx, 0, s.x + s.w - MIN);
      w = s.w + (s.x - nx);
      x = nx;
    } else w = clamp(s.w + dx, MIN, 1 - s.x);
    if (drag.grip.includes('n')) {
      const ny = clamp(s.y + dy, 0, s.y + s.h - MIN);
      h = s.h + (s.y - ny);
      y = ny;
    } else h = clamp(s.h + dy, MIN, 1 - s.y);
    box = { x, y, w, h };
  }
  function up(e: PointerEvent) {
    if (drag?.id === e.pointerId) drag = null;
  }

  const whole = $derived(box.x < 0.001 && box.y < 0.001 && box.w > 0.999 && box.h > 0.999);
  function save() {
    onsave(whole ? null : [box.x, box.y, box.w, box.h].map((v) => Math.round(v * 10000) / 10000));
  }

  const pct = (v: number) => `${v * 100}%`;
</script>

<!-- (Enter saves; Escape, Back and a press outside close it: Modal) -->
<svelte:window onkeydown={(e) => e.key === 'Enter' && save()} />

<Modal title={t('comics.crop.title')} width={560} {onclose}>
  <p class="hint">{t('comics.crop.hint')}</p>
  <div class="stage">
    <div class="frame-wrap" role="presentation" onpointermove={move} onpointerup={up} onpointercancel={up}>
      <img bind:this={img} {src} alt="" draggable="false" />
      <!-- The part left out, dimmed; the crop framed, with a grip at each corner. -->
      <div
        class="crop"
        role="presentation"
        style:left={pct(box.x)}
        style:top={pct(box.y)}
        style:width={pct(box.w)}
        style:height={pct(box.h)}
        onpointerdown={(e) => down(e, 'move')}>
        {#each ['nw', 'ne', 'sw', 'se'] as const as g (g)}
          <span class="grip {g}" role="presentation" onpointerdown={(e) => down(e, g)}></span>
        {/each}
      </div>
    </div>
  </div>
  <div class="foot">
    <button class="quiet" onclick={() => (box = { x: 0, y: 0, w: 1, h: 1 })} disabled={whole}><Icon name="undo" size={16} /> {t('comics.crop.reset')}</button>
    <span class="spacer"></span>
    <button class="quiet" onclick={onclose}>{t('common.cancel')}</button>
    <button onclick={save}>{t('common.save')}</button>
  </div>
</Modal>

<style>
  .hint {
    margin: 0;
    color: var(--subtext);
    font-size: var(--font-small);
  }
  .stage {
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    padding: 6px 0;
  }
  .frame-wrap {
    position: relative;
    touch-action: none;
    user-select: none;
  }
  img {
    display: block;
    max-width: 100%;
    max-height: calc(100dvh - 200px);
    border-radius: 6px;
  }
  .crop {
    position: absolute;
    outline: 2px solid var(--mauve);
    box-shadow: 0 0 0 9999px rgb(17 17 27 / 0.62); /* everything outside, dimmed */
    cursor: move;
  }
  .grip {
    position: absolute;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--mauve);
    border: 3px solid var(--crust);
  }
  @media (pointer: coarse) {
    .grip {
      width: 30px;
      height: 30px;
    }
  }
  /* (inside the corners: the picture's edge clips anything past it) */
  .grip.nw {
    left: -3px;
    top: -3px;
    cursor: nwse-resize;
  }
  .grip.ne {
    right: -3px;
    top: -3px;
    cursor: nesw-resize;
  }
  .grip.sw {
    left: -3px;
    bottom: -3px;
    cursor: nesw-resize;
  }
  .grip.se {
    right: -3px;
    bottom: -3px;
    cursor: nwse-resize;
  }
  .frame-wrap {
    overflow: hidden; /* (the dimming stays on the picture) */
    border-radius: 6px;
  }
</style>
