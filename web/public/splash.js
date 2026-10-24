// The splash's icon exactly where the system's own splash drew it, so nothing
// moves when the page takes over from it. Runs before the first paint (a plain
// script in <head>, after the launch image links).
(function () {
  // The home-screen app on Android (Chrome, Brave, Edge...): no splash of ours.
  // Chromium shows its own until the page first draws something (a color alone
  // doesn't count), so it simply stays until the app appears. Lining ours up
  // with it can't be done reliably: it covers the whole screen, bars included,
  // and the page can't tell how tall each bar is (the user saw it jump).
  // (Except the load right after an update: the updater drew the splash's logo, and the splash takes over from it.)
  var updated = false;
  try {
    updated = !!sessionStorage.getItem('vitrine.updatedTo');
  } catch (e) {
    /* no storage */
  }
  if (!updated && /Android/.test(navigator.userAgent) && matchMedia('(display-mode: standalone)').matches) {
    document.documentElement.style.setProperty('--splash-display', 'none');
    return;
  }
  // The home-screen app on iPhone: the launch image (index.html's
  // apple-touch-startup-image, drawn by the server) centers the icon below a
  // status bar of the height in its `top`; the page starts below the real
  // status bar, whose height may differ.
  if (!navigator.standalone) return;
  var bar = screen.height - innerHeight; // the real status bar (the page starts below it)
  var links = document.querySelectorAll('link[rel="apple-touch-startup-image"]');
  for (var i = 0; i < links.length; i++) {
    if (!matchMedia(links[i].media).matches) continue;
    var u = new URL(links[i].href);
    var top = Number(u.searchParams.get('top')) / Number(u.searchParams.get('scale') || 1);
    document.documentElement.style.setProperty('--splash-shift', (top - bar) / 2 + 'px');
    return;
  }
})();
