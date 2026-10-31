<script lang="ts">
  // Settings > Updates: the version installed and its updates (UpdateStatus: up to date, or a newer one and Update),
  // what's new in a newer release (its CHANGELOG.md, read by the server from GitHub: marked new), then the changes
  // in each installed version (the app's own changelog).
  import type { UpdateState } from '../lib/api';
  import { readChangelog, newerThan } from '../lib/changelog';
  import { reveal } from '../lib/motion';
  import { t } from '../lib/i18n.svelte';
  import UpdateStatus from './UpdateStatus.svelte';
  import ChangelogCard from './ChangelogCard.svelte';

  let info = $state<UpdateState | null>(null);
  let upcoming = $derived(info?.newer && info.changelog ? newerThan(readChangelog(info.changelog), info.current) : []);
</script>

<div class="updates">
  <section class="card">
    <div class="card-head"><h2>{info ? t('updates.installed', { version: info.current }) : t('settings.updatesTitle')}</h2></div>
    <UpdateStatus bind:info />
  </section>
  {#if upcoming.length}
    <div transition:reveal><ChangelogCard given={upcoming} /></div>
  {/if}
  <ChangelogCard />
</div>

<style>
  .updates {
    display: grid;
    gap: 16px;
  }
</style>
