<script lang="ts">
  // A phone sheet's grab bar (drag down to close - lib/sheetDrag starts a drag on .grab-zone at once). The same bar
  // on every sheet; what differs is where it sits: across the sheet's side padding (bleed), stuck to the top of a
  // sheet that scrolls (stuck: the sheet's background behind it), its gap to what follows (after) and its layer (z).
  let { bleed = 0, after = 0, stuck = false, z = 1 }: { bleed?: number; after?: number; stuck?: boolean; z?: number } = $props();
</script>

<div
  class="grab-zone"
  class:stuck
  aria-hidden="true"
  style:margin="0 {-bleed}px {after}px"
  style:z-index={stuck ? z : undefined}><span></span></div>

<style>
  .grab-zone {
    justify-self: stretch;
    flex: none;
    display: grid;
    place-items: center;
    height: 22px;
    touch-action: none;
    cursor: grab;
  }
  .grab-zone.stuck {
    position: sticky;
    top: 0;
    background: var(--mantle);
  }
  span {
    width: 38px;
    height: 5px;
    border-radius: 3px;
    background: var(--surface2);
  }
</style>
