import type { Snippet } from 'svelte';

// Controls a screen shows in the app's top bar (e.g. the library's sort
// menu). The top bar renders them only while that screen is shown.
// h: the bar's height; off: how far it has slid up out of sight (0 when shown) -
// it floats over the screens, which keep room for it (and for what slides with it).
export const topbar = $state<{ library: Snippet | null; comics: Snippet | null; browse: Snippet | null; h: number; off: number }>({ library: null, comics: null, browse: null, h: 0, off: 0 });
