// The splash's icon exactly where the system's own splash drew it, so nothing
// moves when the page takes over from it. Runs before the first paint (a plain
// script in <head>, after the launch image links).
(function () {
  var root = document.documentElement.style;
  // The home-screen app on Android (Chrome, Brave, Edge...): Chromium's splash
  // shows the manifest icon at 128dp, 16dp above the middle (24dp on tablets),
  // and the app's name 48dp from the bottom (56dp) - its
  // webapp_splash_screen_large layout. The page fills the same area.
  if (/Android/.test(navigator.userAgent) && matchMedia('(display-mode: standalone)').matches) {
    var tablet = Math.min(screen.width, screen.height) >= 600;
    root.setProperty('--splash-size', '128px');
    root.setProperty('--splash-opacity', '1');
    root.setProperty('--splash-shift', tablet ? '-24px' : '-16px');
    root.setProperty('--splash-name', 'block');
    root.setProperty('--splash-name-bottom', tablet ? '56px' : '48px');
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
    root.setProperty('--splash-shift', (top - bar) / 2 + 'px');
    return;
  }
})();
