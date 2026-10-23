// The app icon's count of new files (iPhone 16.4+ home-screen apps and
// installed desktop apps): files imported since "Mark all as seen". The
// service worker sets it too when a notification arrives (public/sw.js).
import { api } from './api';
import { settings } from './settings.svelte';

type BadgeNavigator = Navigator & { setAppBadge?: (n?: number) => Promise<void>; clearAppBadge?: () => Promise<void> };

export async function updateBadge() {
  const nav = navigator as BadgeNavigator;
  if (!nav.setAppBadge || !settings.seenAt) return;
  try {
    const { new: n } = await api.newCount('', settings.seenAt, settings.newFromBulk);
    if (n > 0) await nav.setAppBadge(n);
    else await nav.clearAppBadge?.();
  } catch {
    /* no badge this time */
  }
}
