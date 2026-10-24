// The splash's icon exactly where the system's own splash drew it, so nothing
// moves when the page takes over from it. Runs before the first paint (a plain
// script in <head>, after the launch image links).
(function () {
  var root = document.documentElement.style;
  // The home-screen app on Android (Chrome, Brave, Edge...): Chromium's splash
  // covers the whole screen, status bar and navigation bar included, and shows
  // the manifest's maskable icon, flat, at 128dp (the mark at 0.56 of it,
  // icon-maskable.svg) 16dp above the middle (24dp on tablets), and the app's
  // name 48dp from the bottom (56dp): its webapp_splash_screen_large layout.
  // The page only gets the screen between the bars, and nothing tells it how
  // tall each one is (only both together) - except a touch: its screenY less
  // its clientY is the status bar. So the first touch keeps that (per
  // orientation); until then the navigation bar is taken as a gesture bar's
  // 24dp and the status bar as the rest.
  if (/Android/.test(navigator.userAgent) && matchMedia('(display-mode: standalone)').matches) {
    var tablet = Math.min(screen.width, screen.height) >= 600;
    var bars = Math.max(0, screen.height - innerHeight); // status bar + navigation bar
    var key = 'vitrine.statusBar.' + (screen.width < screen.height ? 'portrait' : 'landscape');
    var top = NaN;
    try {
      top = parseFloat(localStorage.getItem(key) || '');
    } catch (e) {
      /* no storage: the guess */
    }
    if (!(top >= 0 && top <= bars)) top = Math.max(0, bars - 24);
    var bottom = bars - top;
    root.setProperty('--splash-size', 128 * 0.56 + 'px');
    root.setProperty('--splash-opacity', '1');
    root.setProperty('--splash-shift', (bottom - top) / 2 - (tablet ? 24 : 16) + 'px');
    root.setProperty('--splash-name', 'block');
    root.setProperty('--splash-name-bottom', Math.max(0, (tablet ? 56 : 48) - bottom) + 'px');
    addEventListener(
      'pointerdown',
      function (e) {
        var bar = Math.round(e.screenY - e.clientY);
        if (bar >= 0 && bar <= bars) {
          try {
            localStorage.setItem(key, String(bar));
          } catch (err) {
            /* no storage: the guess next time too */
          }
        }
      },
      { capture: true, passive: true, once: true },
    );
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
