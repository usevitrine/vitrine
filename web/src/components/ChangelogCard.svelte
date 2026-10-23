<script lang="ts">
  // Settings > What's new (its own page): the repository's CHANGELOG.md (built into the app, read when the page
  // opens), a card per version - "Unreleased" first while changes wait for the next release - its lines as a list.
  import { reveal } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';

  type Part = { text: string; bold: boolean };
  type Version = { title: string; intro: string[]; items: Part[][] };
  let versions = $state<Version[] | null>(null);
  let failed = $state(false);

  /** "**A** b" -> [{A, bold}, {b}]. */
  const parts = (s: string): Part[] => s.split(/\*\*(.+?)\*\*/).map((text, i) => ({ text, bold: i % 2 === 1 })).filter((p) => p.text);

  /** The changelog's "## version" sections: their plain lines and their "- " items (a wrapped item's lines joined). */
  function read(md: string): Version[] {
    const out: Version[] = [];
    let item: string | null = null;
    const flush = () => {
      if (item !== null && out.length) out[out.length - 1].items.push(parts(item));
      item = null;
    };
    for (const raw of md.split('\n')) {
      const line = raw.trimEnd();
      if (line.startsWith('## ')) {
        flush();
        out.push({ title: line.slice(3), intro: [], items: [] });
      } else if (!out.length) continue;
      else if (line.startsWith('- ')) {
        flush();
        item = line.slice(2);
      } else if (item !== null && line.startsWith('  ')) item += ' ' + line.trim();
      else if (line.trim()) {
        flush();
        out[out.length - 1].intro.push(line.trim());
      } else flush();
    }
    flush();
    return out;
  }

  $effect(() => {
    import('../../../CHANGELOG.md?raw')
      .then((m) => (versions = read(m.default)))
      .catch(() => (failed = true));
  });
</script>

<div class="log">
  {#if failed}
    <p class="error small" transition:reveal>{t('changelog.failed')}</p>
  {:else if versions}
    {#each versions as v (v.title)}
      <section class="card" transition:reveal>
        <h2>{v.title === 'Unreleased' ? t('changelog.unreleased') : v.title}</h2>
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
