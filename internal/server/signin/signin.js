// A plain sign-in form: on success the page loads again (signed in, the real page comes). A passkey too, where the
// browser can (the account signing in with passkeys only: the passkey alone).
const f = document.getElementById('f');
const e = document.getElementById('e');
const pw = document.getElementById('pw');
const pk = document.getElementById('pk');

const post = (path, body) => fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });

f.addEventListener('submit', async (ev) => {
  ev.preventDefault();
  const b = f.querySelector('button');
  b.disabled = true;
  e.textContent = '';
  try {
    const r = await post('/api/login', { name: f.elements['name'].value.trim(), password: f.elements['password'].value });
    if (r.ok) {
      location.replace('/');
      return;
    }
    e.textContent = r.status === 429 ? 'Too many tries. Wait a minute.' : r.status === 403 ? 'Sign in with a passkey.' : 'Wrong name or password.';
  } catch {
    e.textContent = 'Can\'t reach the server.';
  }
  b.disabled = false;
});

// Passkeys: the server's options carry bytes as base64url; the browser's API wants them as bytes, and back.
const bytes = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4)), (c) => c.charCodeAt(0));
const text = (buf) => {
  if (!buf) return undefined;
  let s = '';
  for (const b of new Uint8Array(buf)) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
};

if (window.isSecureContext && 'PublicKeyCredential' in window) {
  pk.hidden = false;
  pk.addEventListener('click', async () => {
    pk.disabled = true;
    e.textContent = '';
    try {
      const { publicKey: o } = await (await post('/api/passkeys/login/begin', {})).json();
      const c = await navigator.credentials.get({
        publicKey: { ...o, challenge: bytes(o.challenge), allowCredentials: (o.allowCredentials || []).map((d) => ({ ...d, id: bytes(d.id) })) },
      });
      const r = await post('/api/passkeys/login/finish', {
        id: c.id,
        rawId: text(c.rawId),
        type: c.type,
        clientExtensionResults: c.getClientExtensionResults(),
        response: {
          clientDataJSON: text(c.response.clientDataJSON),
          authenticatorData: text(c.response.authenticatorData),
          signature: text(c.response.signature),
          userHandle: text(c.response.userHandle),
        },
      });
      if (r.ok) {
        location.replace('/');
        return;
      }
      e.textContent = 'That passkey didn\'t work.';
    } catch (err) {
      if (!(err instanceof DOMException && (err.name === 'NotAllowedError' || err.name === 'AbortError'))) e.textContent = 'That passkey didn\'t work.';
    }
    pk.disabled = false;
  });
}

// Passkeys only: no name and password to type.
fetch('/api/session').then((r) => r.json()).then((s) => {
  if (s.passkey_only) {
    pw.hidden = true;
    for (const i of pw.querySelectorAll('input')) i.required = false;
  }
}).catch(() => {});
