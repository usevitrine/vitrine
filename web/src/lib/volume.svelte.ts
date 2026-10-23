// The video volume, remembered on this device (not shared: each device has its
// own speakers). Unmuting brings it back. iOS ignores a page setting a video's
// volume (the hardware buttons decide), so there the slider isn't offered.

const KEY = 'vitrine.volume';

function load(): number {
  try {
    const v = Number(localStorage.getItem(KEY));
    return Number.isFinite(v) && v > 0 && v <= 1 ? v : 1;
  } catch {
    return 1;
  }
}

/** Whether this browser lets a page set a video's volume (not iOS Safari). */
function volumeSettable(): boolean {
  try {
    const v = document.createElement('video');
    v.volume = 0.5;
    return v.volume === 0.5;
  } catch {
    return false;
  }
}

class Volume {
  level = $state(load());
  readonly settable = volumeSettable();

  set(v: number) {
    this.level = Math.min(1, Math.max(0, v));
    try {
      if (this.level > 0) localStorage.setItem(KEY, String(this.level));
    } catch {
      /* not remembered - fine */
    }
  }
}

export const volume = new Volume();
