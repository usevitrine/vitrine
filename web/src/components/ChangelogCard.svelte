<script lang="ts">
  // A changelog as a card per version: the installed one's (the repository's CHANGELOG.md, built into the app, read
  // when it shows) - "Unreleased" first while changes wait for the next release - or given versions (a newer
  // release's, Settings > Updates: marked new). Each version's lines as a list.
  import { reveal } from '../lib/motion';
  import { readChangelog, type ChangelogVersion } from '../lib/changelog';
  import { t } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';

  let { given = null }: { given?: ChangelogVersion[] | null } = $props();
  let builtIn = $state<ChangelogVersion[] | null>(null);
  let failed = $state(false);
  let versions = $derived(given ?? builtIn);

  $effect(() => {
    if (given) return;
    import('../../../CHANGELOG.md?raw')
      .then((m) => (builtIn = readChangelog(m.default)))
      .catch(() => (failed = true));
  });
</script>

<div class="log">
  {#if failed}
    <p class="error small" transition:reveal>{t('changelog.failed')}</p>
  {:else if versions}
    {#each versions as v (v.title)}
      <section class="card" transition:reveal>
        <h2>{#if given}<span class="new"><Icon name="download" size={14} /> {t('updates.newIn')}</span>{/if}{v.title === 'Unreleased' ? t('changelog.unreleased') : v.title}</h2>
        {#each v.intro as p, i (i)}<p class="intro">{p}</p>{/each}
        <ul>
          {#each v.items as it, i (i)}
            <li>{#each it as p, j (j)}{#if p.bold}<strong>{p.text}</strong>{:else}{p.text}{/if}{/each}</li>
          {/each}
        </ul>
      </section>
    {/each}
  {/if}
</div>

<style>
  .log {
    display: grid;
    gap: 16px;
  }
  section {
    display: grid;
    gap: 8px;
  }
  h2 {
    margin: 0;
    color: var(--mauve);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 10px;
  }
  /* (a newer release's: not installed yet) */
  .new {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 9px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 600;
    color: var(--green);
    background: color-mix(in srgb, var(--green) 14%, transparent);
  }
  .intro {
    margin: 0;
    color: var(--subtext);
  }
  ul {
    margin: 0;
    padding-left: 18px;
    display: grid;
    gap: 6px;
    line-height: 1.45;
  }
  strong {
    color: var(--text);
  }
  li {
    color: var(--subtext);
  }
</style>
