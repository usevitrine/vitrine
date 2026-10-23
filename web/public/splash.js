// The home-screen app on iPhone: the splash's icon exactly where the launch
// image drew it, so nothing moves when the page takes over from it. The
// launch image (index.html's apple-touch-startup-image, drawn by the server)
// centers the icon below a status bar of the height in its `top`; the page
// starts below the real status bar, whose height may differ. Runs before the
// first paint (a plain script in <head>, after those links).
(function () {
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
