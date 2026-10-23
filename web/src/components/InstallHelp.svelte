<script lang="ts">
  // How to install Vitrine as an app on this device: the browser's own install
  // dialog where it offers one (Chromium on Android and desktop), else steps
  // for this platform and browser - iPhone/iPad (Safari, Chrome, Firefox, Edge),
  // Android (Chrome, Brave, Edge, Samsung Internet, Firefox, Opera) and desktop
  // (Chrome, Brave, Edge, Safari; Firefox can't).
  import Icon from './Icon.svelte';
  import { reveal } from '../lib/motion';
  import Rich from './Rich.svelte';
  import { install, where } from '../lib/install.svelte';
  import { toasts } from '../lib/toast.svelte';
  import { t } from '../lib/i18n.svelte';

  type IconName = 'share' | 'addbox' | 'library' | 'morev' | 'more' | 'menu' | 'download' | 'plus' | 'external';
  // A step: an icon and its words (the bold part in <b>...</b>).
  type Step = { icon: IconName; text: string };

  const { platform, browser } = where();

  function steps(): { lead: string; steps: Step[] } | null {
    const open: Step = { icon: 'library', text: t(`install.open.${platform === 'desktop' ? 'desktop' : platform === 'ios' ? 'ios' : 'android'}`) };
    const menuDots: Step = { icon: 'morev', text: t('install.step.menuDots') };
    if (platform === 'ios') {
      const share: Step =
        browser === 'chrome'
          ? { icon: 'share', text: t('install.step.iosChromeShare') }
          : browser === 'firefox' || browser === 'edge'
            ? { icon: 'menu', text: t('install.step.menuThenShare') }
            : { icon: 'share', text: t('install.step.safariShare') };
      return {
        lead: t('install.lead.ios'),
        steps: [share, { icon: 'addbox', text: t('install.step.addToHomeScreen') }, open],
      };
    }
    if (platform === 'android') {
      const lead = t('install.lead.android');
      if (browser === 'samsung')
        return { lead, steps: [{ icon: 'menu', text: t('install.step.menuBottom') }, { icon: 'plus', text: t('install.step.samsungAdd') }, open] };
      if (browser === 'firefox') return { lead, steps: [menuDots, { icon: 'download', text: t('install.step.firefoxInstall') }, open] };
      if (browser === 'opera') return { lead, steps: [menuDots, { icon: 'plus', text: t('install.step.operaAdd') }, open] };
      return { lead, steps: [menuDots, { icon: 'download', text: t('install.step.installApp') }, open] };
    }
    const lead = t('install.lead.desktop');
    if (browser === 'safari') return { lead, steps: [{ icon: 'plus', text: t('install.step.safariDock') }, open] };
    if (browser === 'edge')
      return { lead, steps: [{ icon: 'more', text: t('install.step.edgeMenu') }, { icon: 'download', text: t('install.step.edgeInstall') }, open] };
    if (browser === 'firefox') return null;
    return {
      lead,
      steps: [{ icon: 'download', text: t('install.step.chromeIcon') }, { icon: 'morev', text: t('install.step.chromeMenu') }, open],
    };
  }
  const how = $derived(steps());

  async function run() {
    try {
      if (await install.run()) toasts.show(t('install.installed'), { cat: 'device' });
    } catch (e) {
      toasts.error((e as Error).message);
    }
  }
</script>

{#if install.installed}
  <p class="ok-line" transition:reveal><Icon name="check" size={18} /> {t('install.installedHere')}</p>
{:else if install.offer}
  <p class="lead" transition:reveal>{platform === 'desktop' ? t('install.lead.desktop') : t('install.lead.android')}</p>
  <button class="install" onclick={run}><Icon name="download" size={18} /> {t('install.button')}</button>
{:else if how}
  <p class="lead" transition:reveal>{how.lead}</p>
  <ol class="how">
    {#each how.steps as s, i (i)}
      <li><span class="key"><Icon name={s.icon} size={20} /></span><span><Rich text={s.text} /></span></li>
    {/each}
  </ol>
{:else}
  <p class="lead" transition:reveal>{t('install.firefoxDesktop')}</p>
{/if}

<style>
  .lead {
    margin: 0;
    color: var(--subtext);
  }
  .ok-line {
    display: flex;
    gap: 8px;
    align-items: center;
    margin: 0;
    color: var(--green);
  }
  .install {
    justify-self: start;
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .how {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    background: var(--mantle);
    border: 1px solid var(--surface0);
    border-radius: 14px;
    overflow: hidden;
  }
  .how li {
    display: flex;
    gap: 12px;
    align-items: center;
    padding: 12px 14px;
  }
  .how li + li {
    border-top: 1px solid var(--surface0);
  }
  .key {
    flex: none;
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    border-radius: 10px;
    background: var(--surface0);
    color: var(--blue);
  }
</style>
