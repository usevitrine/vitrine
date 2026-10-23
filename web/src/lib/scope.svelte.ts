// The library's search, grid, selection and saved searches are shared by two
// pages: the library (files) and Comics (comics, shown as files with their
// cover). A page hands its own set to the components below it (provideScope);
// without one they work on the library's.
import { getContext, setContext } from 'svelte';
import { results, Results } from './results.svelte';
import { selection, Selection } from './selection.svelte';
import { saved, Saved } from './saved.svelte';
import { BrowseResults } from './browse.svelte';

export interface Scope {
  kind: 'library' | 'comics' | 'browse';
  results: Results;
  selection: Selection;
  saved: Saved;
}

const libraryScope: Scope = { kind: 'library', results, selection, saved };

/** The Comics page's set (one for the app: its search stays when you come back). */
export const comicsScope: Scope = { kind: 'comics', results: new Results('comics'), selection: new Selection(), saved: new Saved('comics') };

/** Comics > Browse's set: a gallery site's search, drawn like Comics (no selection, saved searches or filters). */
export const browseScope: Scope = { kind: 'browse', results: new BrowseResults(), selection: new Selection(), saved: new Saved('comics') };

const key = Symbol('scope');

export function provideScope(s: Scope) {
  setContext(key, s);
}

/** The page's set (call while a component starts). */
export function useScope(): Scope {
  return getContext<Scope | undefined>(key) ?? libraryScope;
}
