// The browser's storage (localStorage; 'session' for sessionStorage) without the throwing: private modes and
// blocked site data make it throw or come back empty, and nothing kept there may break the app - a read gives
// nothing (or the fallback), a write or remove that can't be done is skipped.
type Area = 'local' | 'session';

const area = (where: Area) => (where === 'session' ? sessionStorage : localStorage);

/** The value kept under key (null: none, or no storage). */
export function read(key: string, where: Area = 'local'): string | null {
  try {
    return area(where).getItem(key);
  } catch {
    return null;
  }
}

export function write(key: string, value: string, where: Area = 'local') {
  try {
    area(where).setItem(key, value);
  } catch {
    /* not kept - fine */
  }
}

export function remove(key: string, where: Area = 'local') {
  try {
    area(where).removeItem(key);
  } catch {
    /* nothing to do */
  }
}

/** A value kept as JSON; fallback when there's none or it doesn't parse. */
export function readJSON<T>(key: string, fallback: T, where: Area = 'local'): T {
  const v = read(key, where);
  if (v === null) return fallback;
  try {
    return JSON.parse(v) as T;
  } catch {
    return fallback;
  }
}

export function writeJSON(key: string, value: unknown, where: Area = 'local') {
  write(key, JSON.stringify(value), where);
}
