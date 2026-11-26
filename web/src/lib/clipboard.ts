import { toasts } from './toast.svelte';
import { t } from './i18n.svelte';

/** Copies text, with a notice ("Copied <what>"). */
export async function copyText(text: string, what = text) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    // Older browsers / no permission: the classic way.
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    if (!ok) return toasts.error(t('copy.failed'));
  }
  toasts.show(t('copy.copied', { what }), { cat: 'copied' });
}
