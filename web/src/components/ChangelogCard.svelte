<script lang="ts">
  // A changelog as a card per version: the installed one's (the repository's CHANGELOG.md, built into the app, read
  // when it shows) - "Unreleased" first while changes wait for the next release - or given versions (a newer
  // release's, Settings > Updates: marked new). Each version's sections (New, Improved, Fixed) as lists.
  import { reveal } from '../lib/motion';
  import { readChangelog, type ChangelogVersion } from '../lib/changelog';
  import { t } from '../lib/i18n.svelte';
  import Icon from './Icon.svelte';

  let { given = null }: { given?: ChangelogVersion[] | null } = $props();
  let builtIn = $state<ChangelogVersion[] | null>(null);
  let failed = $state(false);
  // (a version with nothing in it - "Unreleased" just after a release - isn't shown: an empty card)
  let versions = $derived((given ?? builtIn)?.filter((v) => v.intro.length || v.items.length || v.groups.some((g) => g.items.length)) ?? null);

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
        {#each v.groups as g, gi (gi)}
          {#if g.title}<h3>{g.title}</h3>{/if}
          <ul>
            {#each g.items as it, i (i)}
              <li>{#each it as p, j (j)}{#if p.code}<code>{p.text}</code>{:else if p.bold}<strong>{p.text}</strong>{:else}{p.text}{/if}{/each}</li>
            {/each}
          </ul>
        {/each}
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
  /* A version's section: New, Improved, Fixed. */
  h3 {
    margin: 6px 0 0;
    font-size: var(--font-small);
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--overlay1);
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
  code {
    padding: 1px 5px;
    border-radius: 5px;
    background: var(--surface0);
    color: var(--text);
    font-size: 0.92em;
  }
  li {
    color: var(--subtext);
  }
</style>
