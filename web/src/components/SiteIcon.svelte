<script lang="ts" module>
  import { SvelteSet } from 'svelte/reactivity';
  // Sites without an icon (the server said so): not asked again while the app runs.
  const none = new SvelteSet<string>();
</script>

<script lang="ts">
  import Icon from './Icon.svelte';

  // A site's own icon (its favicon, fetched and kept by the server), on the
  // usual round-cornered badge; the link icon until it has loaded, or if it has none.
  // (v: raised when browsers must forget the icons they have, e.g. a cached "none".)
  let { host }: { host: string } = $props();
  let loaded = $state(false);
  let dark = $state(false); // a dark icon on a see-through background: it gets a light badge
  $effect(() => {
    host;
    loaded = false;
    dark = false;
  });

  /** Whether an icon is dark lines on nothing (like a black logo), which would
   *  vanish on the dark badge. */
  function isDark(img: HTMLImageElement) {
    try {
      const c = document.createElement('canvas');
      c.width = c.height = 16;
      const g = c.getContext('2d', { willReadFrequently: true })!;
      g.drawImage(img, 0, 0, 16, 16);
      const px = g.getImageData(0, 0, 16, 16).data;
      let seen = 0;
      let clear = 0;
      let light = 0;
      for (let i = 0; i < px.length; i += 4) {
        if (px[i + 3] < 128) {
          clear++;
          continue;
        }
        seen++;
        light += (0.2126 * px[i] + 0.7152 * px[i + 1] + 0.0722 * px[i + 2]) / 255;
      }
      return seen > 0 && clear > 256 * 0.25 && light / seen < 0.3;
    } catch {
      return false;
    }
  }
</script>

<span class="ic" class:site={loaded} class:dark>
  {#if !loaded}<Icon name="link" size={17} />{/if}
  {#if host && !none.has(host)}
    <img
      src="/api/favicon?host={encodeURIComponent(host)}&v=2"
      alt=""
      width="18"
      height="18"
      class:shown={loaded}
      onload={(e) => {
        dark = isDark(e.currentTarget as HTMLImageElement);
        loaded = true;
      }}
      onerror={() => none.add(host)} />
  {/if}
</span>

<style>
  .ic {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 9px;
    background: color-mix(in srgb, var(--mauve) 16%, transparent);
    color: var(--mauve);
    transition: background-color 0.15s;
  }
  /* A site's icon: on a neutral badge (its own colors, not the accent). */
  .ic.site {
    background: var(--surface0);
  }
  .ic.site.dark {
    background: var(--subtext);
  }
  img {
    position: absolute;
    inset: 7px;
    width: 18px;
    height: 18px;
    object-fit: contain;
    opacity: 0;
    transition: opacity 0.15s;
  }
  img.shown {
    opacity: 1;
  }
</style>
