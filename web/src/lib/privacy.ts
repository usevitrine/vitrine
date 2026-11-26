// Privacy on a phone: the keyboard kept from learning what's typed. (Hiding the app in the app switcher was tried
// and dropped, 2026-10-21: iOS takes the switcher's picture before a web app hears it's being left.)

/**
 * Text fields: no suggestions, autocorrect, capitals or spellcheck - the phone's keyboard then doesn't offer (or,
 * on iOS, largely learn) what's typed in them. Set on every field as it appears; a field that says otherwise keeps
 * that (the login's name and password fields: password managers fill them).
 */
export function quietKeyboard() {
  const set = (el: Element) => {
    if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement)) return;
    if (el instanceof HTMLInputElement && !['text', 'search', 'url', 'email', 'number', ''].includes(el.type)) return;
    if (!el.hasAttribute('autocomplete')) el.setAttribute('autocomplete', 'off');
    if (!el.hasAttribute('autocorrect')) el.setAttribute('autocorrect', 'off');
    if (!el.hasAttribute('autocapitalize')) el.setAttribute('autocapitalize', 'off');
    if (!el.hasAttribute('spellcheck')) el.setAttribute('spellcheck', 'false');
  };
  const walk = (n: Node) => {
    if (!(n instanceof Element)) return;
    set(n);
    n.querySelectorAll('input, textarea').forEach(set);
  };
  walk(document.body);
  new MutationObserver((changes) => {
    for (const c of changes) c.addedNodes.forEach(walk);
  }).observe(document.body, { childList: true, subtree: true });
}
