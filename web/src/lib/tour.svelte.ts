// The short tour after the first steps: a few one-time hints on the library (Tour.svelte). Per device - a phone and a
// desktop are used differently - and remembered there; it waits until the library or Comics is on screen.
import { read, write } from './storage';

const KEY = 'vitrine.tour';

class Tour {
  active = $state(read(KEY) === 'pending'); // (no storage: no tour)

  start() {
    this.active = true;
    write(KEY, 'pending'); // (no storage: shown now, not remembered)
  }

  end() {
    this.active = false;
    write(KEY, 'done');
  }
}

export const tour = new Tour();
