// The short tour after the first steps: a few one-time hints on the library (Tour.svelte). Per device - a phone and a
// desktop are used differently - and remembered there; it waits until the library or Comics is on screen.
const KEY = 'vitrine.tour';

class Tour {
  active = $state(false);

  constructor() {
    try {
      this.active = localStorage.getItem(KEY) === 'pending';
    } catch {
      /* no storage: no tour */
    }
  }

  start() {
    this.active = true;
    try {
      localStorage.setItem(KEY, 'pending');
    } catch {
      /* shown now, not remembered */
    }
  }

  end() {
    this.active = false;
    try {
      localStorage.setItem(KEY, 'done');
    } catch {
      /* fine */
    }
  }
}

export const tour = new Tour();
