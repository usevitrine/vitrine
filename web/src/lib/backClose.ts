// Back closes what's open on top - a menu, a sheet, a dialog - instead of
// leaving the page: Android's back button and gesture (and iOS's edge swipe in
// the home-screen app). Touch screens only (a mouse has Escape and clicks).
// Use as {@attach backClose(close)} on the element shown while it's open: it
// adds a history entry when it appears; Back pops it and closes it; closed any
// other way, its entry is popped too (unless the page moved on meanwhile).
let next = 1;
const touch = () => matchMedia('(pointer: coarse)').matches;

export function backClose(close: () => void) {
  return (node: Element) => {
    if (!touch()) return;
    const id = next++;
    history.pushState({ ...(history.state ?? {}), layer: id }, '');
    const at = location.href;
    let gone = false;
    const onPop = () => {
      // Our entry was popped (Back): close. (A layer opened after this one closes first.)
      if (gone || history.state?.layer === id || isAbove(id)) return;
      gone = true;
      stop();
      close();
    };
    // Closed some other way: our entry goes - after this task (a click that closes
    // a menu may also go to another screen: then it stays) and only if still on top.
    const closed = () => {
      if (gone) return;
      gone = true;
      stop();
      setTimeout(() => {
        if (history.state?.layer !== id) return;
        // Its action changed the address (e.g. a new search): going back would undo
        // that - the entry stays, as the page's (no longer a layer).
        if (location.href !== at) history.replaceState({ ...history.state, layer: undefined }, '');
        else history.back();
      }, 0);
    };
    // Closing starts its fade-out: the element is inert from then on (it goes
    // only after the fade) - our entry goes at once, not after it.
    const mo = new MutationObserver(() => (node as HTMLElement).inert && closed());
    mo.observe(node, { attributes: true, attributeFilter: ['inert'] });
    const stop = () => {
      removeEventListener('popstate', onPop);
      mo.disconnect();
    };
    addEventListener('popstate', onPop);
    return closed;
  };
}

/** Whether the history's current entry is a layer opened after this one (it closes first). */
function isAbove(id: number) {
  const top = history.state?.layer;
  return typeof top === 'number' && top > id;
}
