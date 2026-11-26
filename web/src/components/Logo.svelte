<script lang="ts">
  // The Vitrine mark - the letter V cut like a crystal (each arm a lit and a
  // shaded face; public/icon.svg has the same) - and the wordmark, where the
  // mark is the V of "Vitrine". size: about the height of the mark.
  let { size = 26, wordmark = true }: { size?: number; wordmark?: boolean } = $props();
  const id = $props.id(); // (each logo's own gradient)
  const faces: [string, number][] = [
    ['11,14 23,14 32,40 32,54', 0.22],
    ['11,14 32,54 21,40', -0.05],
    ['41,14 53,14 32,54 32,40', -0.12],
    ['53,14 43,40 32,54', -0.28],
  ];
</script>

<span class="logo" style:--size="{size}px" role="img" aria-label="Vitrine">
  <svg class="mark" viewBox="10 13 44 42" aria-hidden="true">
    <defs>
      <linearGradient id="{id}-g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#cba6f7" /><stop offset="1" stop-color="#f5c2e7" /></linearGradient>
    </defs>
    {#each faces as [points, shade] (points)}
      <polygon {points} fill="url(#{id}-g)" />
      <polygon {points} fill={shade > 0 ? '#fff' : '#1e1e2e'} opacity={Math.abs(shade)} />
    {/each}
  </svg>
  {#if wordmark}<span class="name" aria-hidden="true">itrine</span>{/if}
</span>

<style>
  .logo {
    display: inline-flex;
    align-items: flex-end;
    line-height: 1;
  }
  .mark {
    width: calc(var(--size) * 0.8);
    height: calc(var(--size) * 0.77);
    flex: none;
  }
  .name {
    /* The mark is the word's V: the rest of it sits on the same line, close to it. */
    margin-left: calc(var(--size) * -0.04);
    color: var(--text);
    font-weight: 700;
    font-size: calc(var(--size) * 0.72);
    letter-spacing: -0.01em;
    transform: translateY(calc(var(--size) * 0.06));
  }
</style>
