// A plain sign-in form: on success the page loads again (signed in, the real page comes).
const f = document.getElementById('f');
const e = document.getElementById('e');
f.addEventListener('submit', async (ev) => {
  ev.preventDefault();
  const b = f.querySelector('button');
  b.disabled = true;
  e.textContent = '';
  try {
    const r = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: f.elements['name'].value.trim(), password: f.elements['password'].value }),
    });
    if (r.ok) {
      location.replace('/');
      return;
    }
    e.textContent = r.status === 429 ? 'Too many tries. Wait a minute.' : 'Wrong name or password.';
  } catch {
    e.textContent = 'Can\'t reach the server.';
  }
  b.disabled = false;
});
