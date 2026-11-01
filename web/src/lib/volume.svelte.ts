// The video volume, remembered on this device (not shared: each device has its
// own speakers). Unmuting brings it back. iOS ignores a page setting a video's
// volume (the hardware buttons decide), so there the slider isn't offered.
import { read, write } from './storage';

const KEY = 'vitrine.volume';

function load(): number {
  const v = Number(read(KEY));
  return Number.isFinite(v) && v > 0 && v <= 1 ? v : 1;
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
    if (this.level > 0) write(KEY, String(this.level));
  }
}

export const volume = new Volume();
