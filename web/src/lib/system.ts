import { t, date } from './i18n.svelte';
import { trash } from './trash.svelte';
import { subNames } from './subNames.svelte';
// System filters (the filter menu in the search bar): which there are, the
// form for each, turning a form into search text (internal/search's syntax)
// and back, and a plain-words label for a filter chip.

export type FilterId =
  | 'imported'
  | 'uploaded'
  | 'modified'
  | 'type'
  | 'duration'
  | 'size'
  | 'width'
  | 'height'
  | 'has_audio'
  | 'no_audio'
  | 'animated'
  | 'new'
  | 'favorite'
  | 'not_favorite'
  | 'tags'
  | 'rating'
  | 'site'
  | 'limit'
  // Comics:
  | 'pages'
  | 'unread'
  | 'reading'
  | 'read'
  | 'category'
  | 'language'
  // Comics > Browse (a gallery site's own filters):
  | 'stars'
  | 'expunged'
  | 'torrent';

/** A label (and value) whose words follow the app's language: read when shown. */
const choice = <V extends string>(value: V, key: string) => ({
  value,
  get label() {
    return t(key);
  },
});

const withHint = new Set<FilterId>(['imported', 'uploaded', 'modified', 'limit']);
// A hint only where the name alone could mislead (kept to a few words).
export const filters = Object.fromEntries(
  (['imported', 'uploaded', 'modified', 'type', 'duration', 'size', 'width', 'height', 'has_audio', 'no_audio', 'animated', 'new', 'favorite', 'not_favorite', 'tags', 'rating', 'site', 'limit', 'pages', 'unread', 'reading', 'read', 'category', 'language', 'stars', 'expunged', 'torrent'] as FilterId[]).map(
    (id) => [
      id,
      {
        get label() {
          return t(`filter.${id}.label`);
        },
        get hint() {
          return withHint.has(id) ? t(`filter.${id}.hint`) : undefined;
        },
      },
    ],
  ),
) as Record<FilterId, { label: string; hint?: string }>;

export const groups: { name: string; items: FilterId[] }[] = [
  { get name() { return t('filter.group.when'); }, items: ['imported', 'uploaded', 'modified'] },
  { get name() { return t('filter.group.file'); }, items: ['type', 'animated', 'duration', 'size', 'width', 'height', 'has_audio', 'no_audio'] },
  { get name() { return t('filter.group.other'); }, items: ['new', 'favorite', 'not_favorite', 'tags', 'rating', 'limit'] }, // Site: the From row at the top
];

/** The Comics page's filters (imported and modified read as added and updated there: see filterInfo). */
export const comicGroups: { name: string; items: FilterId[] }[] = [
  { get name() { return t('filter.group.when'); }, items: ['imported', 'uploaded', 'modified'] },
  { get name() { return t('filter.group.comic'); }, items: ['unread', 'reading', 'read', 'pages', 'category', 'language'] },
  { get name() { return t('filter.group.other'); }, items: ['favorite', 'not_favorite', 'tags', 'limit'] },
];

/** Comics > Browse's filters: what the gallery site's search can do (lib/browse turns them into its syntax). */
export const browseGroups: { name: string; items: FilterId[] }[] = [
  { get name() { return t('filter.group.comic'); }, items: ['category', 'language', 'pages', 'stars'] },
  { get name() { return t('filter.group.other'); }, items: ['expunged', 'torrent'] },
];
/** The gallery site's categories and its most common languages (Browse: the site's, not the library's). */
export const browseCategories = ['doujinshi', 'manga', 'artist_cg', 'game_cg', 'western', 'non-h', 'image_set', 'cosplay', 'asian_porn', 'misc'];
export const browseLanguages = ['english', 'japanese', 'chinese', 'korean', 'spanish', 'french', 'german', 'russian', 'portuguese', 'italian', 'thai', 'vietnamese'];
/** The least stars a gallery has (Browse). */
export const starChoices = ['2', '3', '4', '5'].map((n) => ({ value: n, get label() { return t('describe.stars', { n }); } }));

/** A filter's name and hint on a page: comics were added and updated, not imported and modified. */
export function filterInfo(id: FilterId, comics = false): { label: string; hint?: string } {
  if (comics && (id === 'imported' || id === 'modified')) {
    const c = id === 'imported' ? 'added' : 'updated';
    return { label: t(`filter.${c}.label`), hint: t(`filter.${c}.hint`) };
  }
  return filters[id];
}

/** Filters that add tag chips picked from the comics' own tags (category:..., language:...). */
export const tagFilter = (id: FilterId) => id === 'category' || id === 'language';

// ---- Sort: what to sort by (a list that can grow) and which way, with
// words that fit the field. Offered in the menu: sortFields; any other field
// the server knows (typed as sort:size, ...) still shows with its own words.

export const sortFields = ['imported', 'uploaded', 'modified', 'favorite', 'came', 'size', 'duration', 'width', 'height', 'tags', 'random'];
/** The Comics page's orders. */
export const comicSortFields = ['imported', 'uploaded', 'modified', 'read', 'title', 'pages', 'favorite', 'tags', 'random'];

const sortWords = new Set(['imported', 'uploaded', 'modified', 'favorite', 'came', 'size', 'duration', 'width', 'height', 'tags', 'read', 'title', 'pages']);

/** "imported_asc" -> { field: "imported", asc: true }. */
export function splitSort(sort: string): { field: string; asc: boolean } {
  const asc = sort.endsWith('_asc');
  return { field: asc ? sort.slice(0, -4) : sort, asc };
}

export const joinSort = (field: string, asc: boolean) => (asc && !field.startsWith('random') ? field + '_asc' : field);

/** A field's name and its two directions in words (no directions for random). */
export function sortInfo(field: string, comics = false): { label: string; desc: string; asc: string } | { label: string; desc: null; asc: null } {
  if (field.startsWith('random')) return { label: t('sort.random'), desc: null, asc: null };
  if (trash.on && field === 'modified') return { label: t('sort.deleted.label'), desc: t('sort.deleted.desc'), asc: t('sort.deleted.asc') };
  if (comics && (field === 'imported' || field === 'modified')) {
    const c = field === 'imported' ? 'added' : 'updated';
    return { label: t(`sort.${c}.label`), desc: t(`sort.${c}.desc`), asc: t(`sort.${c}.asc`) };
  }
  if (sortWords.has(field)) return { label: t(`sort.${field}.label`), desc: t(`sort.${field}.desc`), asc: t(`sort.${field}.asc`) };
  return { label: field.replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase()), desc: t('sort.descending'), asc: t('sort.ascending') };
}

/** Filters with nothing to fill in: picking one adds it straight away. */
export const instant = (id: FilterId) =>
  id === 'has_audio' || id === 'no_audio' || id === 'animated' || id === 'new' || id === 'favorite' || id === 'not_favorite' || id === 'unread' || id === 'reading' || id === 'read' || id === 'expunged' || id === 'torrent';

export type TimeMode = 'last' | 'ago' | 'since' | 'before' | 'on';
export type Cmp = '<' | '<=' | '=' | '>=' | '>' | '!=';

export interface Form {
  id: FilterId;
  mode: TimeMode; // imported / uploaded / modified
  num: string; // the number typed
  unit: string; // age: m h d w mo y; duration: s m; size: KB MB GB
  date: string; // YYYY-MM-DD
  cmp: Cmp;
  values: string[]; // type: several; rating, site: one
  not: boolean;
}

export const timeUnits = ['m', 'h', 'd', 'w', 'mo', 'y'].map((u) => choice(u, `unit.name.${u}`));
export const durationUnits = ['s', 'm'].map((u) => choice(u, `unit.name.${u}`));
export const sizeUnits = [
  { value: 'KB', label: 'KB' },
  { value: 'MB', label: 'MB' },
  { value: 'GB', label: 'GB' },
];

/** The comparisons offered for a number filter, in words. */
export function comparisons(id: FilterId): { value: Cmp; label: string }[] {
  switch (id) {
    case 'duration':
      return [choice<Cmp>('<', 'cmp.shorter'), choice<Cmp>('>', 'cmp.longer')];
    case 'size':
      return [choice<Cmp>('<', 'cmp.smaller'), choice<Cmp>('>', 'cmp.larger')];
    case 'tags':
    case 'pages':
      return [choice<Cmp>('<', 'cmp.fewer'), choice<Cmp>('>', 'cmp.more'), choice<Cmp>('=', 'cmp.exactly')];
    default: // width, height
      return [choice<Cmp>('>=', 'cmp.atLeast'), choice<Cmp>('<=', 'cmp.atMost'), choice<Cmp>('=', 'cmp.exactly')];
  }
}

export const typeChoices = [
  choice('video', 'filter.type.videos'),
  choice('image', 'filter.type.images'),
  { value: 'gif', label: 'GIF' },
  { value: 'mp4', label: 'MP4' },
  { value: 'webm', label: 'WebM' },
  { value: 'jpg', label: 'JPEG' },
  { value: 'png', label: 'PNG' },
];
export const ratingChoices = ['general', 'sensitive', 'questionable', 'explicit'].map((r) => choice(r, `filter.rating.${r}`));

const today = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};

export function defaults(id: FilterId): Form {
  const f: Form = { id, mode: 'last', num: '', unit: 'd', date: today(), cmp: '>', values: [], not: false };
  if (id === 'duration') Object.assign(f, { unit: 's', cmp: '>' });
  if (id === 'size') Object.assign(f, { unit: 'MB', cmp: '>' });
  if (id === 'width' || id === 'height') f.cmp = '>=';
  if (id === 'tags') f.cmp = '<';
  if (id === 'pages') f.cmp = '>';
  if (id === 'limit') f.num = '1000';
  return f;
}

const isTime = (id: FilterId) => id === 'imported' || id === 'uploaded' || id === 'modified';
const positive = (s: string) => /^\d+(\.\d+)?$/.test(s.trim()) && Number(s) > 0;
const whole = (s: string) => /^\d+$/.test(s.trim());

/** The search text for a filled-in form ('' while it isn't complete). */
export function build(f: Form): string {
  const n = f.num.trim();
  if (isTime(f.id)) {
    if (f.mode === 'last' || f.mode === 'ago') return positive(n) ? `system:${f.id}${f.mode === 'last' ? '<' : '>'}${n}${f.unit}` : '';
    if (!/^\d{4}-\d\d-\d\d$/.test(f.date)) return '';
    return `system:${f.id}${{ since: '>=', before: '<', on: '=' }[f.mode]}${f.date}`;
  }
  switch (f.id) {
    case 'has_audio':
      return 'system:has_audio';
    case 'no_audio':
      return '-system:has_audio';
    case 'favorite':
      return 'system:favorite';
    case 'not_favorite':
      return '-system:favorite';
    case 'animated':
    case 'new':
    case 'unread':
    case 'reading':
    case 'read':
    case 'expunged':
    case 'torrent':
      return 'system:' + f.id;
    case 'stars':
      return f.values.length ? `system:stars>=${f.values[0]}` : '';
    case 'pages':
      return whole(n) ? `system:pages${f.cmp}${n}` : '';
    case 'category':
    case 'language': {
      // Tags: one, or several as an OR group.
      const tags = f.values.map((v) => `${f.id}:${v}`);
      return tags.length > 1 ? `( ${tags.join(' ~ ')} )` : (tags[0] ?? '');
    }
    case 'duration':
    case 'size':
      return positive(n) ? `system:${f.id}${f.cmp}${n}${f.unit}` : '';
    case 'width':
    case 'height':
    case 'tags':
      return whole(n) ? `system:${f.id}${f.cmp}${n}` : '';
    case 'type':
    case 'rating':
    case 'site':
      return f.values.length ? `system:${f.id}${f.not ? '!=' : '='}${f.values.join(',')}` : '';
    case 'limit':
      return whole(n) && Number(n) > 0 ? `limit:${n}` : '';
  }
  return '';
}

/** Reads a filter chip's text back into its form (null: not one the menu makes). */
export function read(text: string): Form | null {
  const neg = text.startsWith('-');
  const t = (neg ? text.slice(1) : text).toLowerCase();
  if (t === 'system:has_audio') return defaults(neg ? 'no_audio' : 'has_audio');
  if (!neg && (t === 'system:expunged' || t === 'system:torrent' || t === 'system:animated' || t === 'system:new')) return defaults(t.slice(7) as FilterId);
  if (isFavorite(t)) return defaults(neg ? 'not_favorite' : 'favorite');
  if (neg) return null;
  const state = readState(t);
  if (state) return defaults(state);
  const lim = /^(?:limit:|system:limit=)(\d+)$/.exec(t);
  if (lim) return { ...defaults('limit'), num: lim[1] };
  const m = /^system:([a-z_]+)(<=|>=|!=|<|>|=)(.+)$/.exec(t);
  if (!m) return null;
  const [, field, op, val] = m;
  const id = ({ import: 'imported', posted: 'uploaded', upload: 'uploaded', filesize: 'size', length: 'duration', filetype: 'type', tag_count: 'tags' } as Record<string, string>)[field] ?? field;
  if (!(id in filters)) return null;
  const f = defaults(id as FilterId);
  if (isTime(f.id)) {
    const age = /^(\d+(?:\.\d+)?)(mo|min|[smhdwy])$/.exec(val);
    if (age && (op === '<' || op === '<=' || op === '>' || op === '>=')) {
      return { ...f, mode: op.startsWith('<') ? 'last' : 'ago', num: age[1], unit: age[2] === 'min' ? 'm' : age[2] === 's' ? 'm' : age[2] };
    }
    const date = /^(\d{4}-\d\d-\d\d)/.exec(val);
    const mode = ({ '>=': 'since', '>': 'since', '<': 'before', '<=': 'before', '=': 'on' } as Record<string, TimeMode>)[op];
    return date && mode ? { ...f, mode, date: date[1] } : null;
  }
  switch (f.id) {
    case 'duration': {
      const d = /^(\d+(?:\.\d+)?)(s|m)?$/.exec(val);
      if (!d || !comparisons('duration').some((c) => c.value === op)) return null;
      return { ...f, cmp: op as Cmp, num: d[1], unit: d[2] ?? 's' };
    }
    case 'size': {
      const s = /^(\d+(?:\.\d+)?)(kb|mb|gb|k|m|g)$/.exec(val);
      if (!s || !comparisons('size').some((c) => c.value === op)) return null;
      return { ...f, cmp: op as Cmp, num: s[1], unit: s[2][0].toUpperCase() + 'B' };
    }
    case 'width':
    case 'height':
    case 'tags':
    case 'pages':
      if (!/^\d+$/.test(val) || !comparisons(f.id).some((c) => c.value === op)) return null;
      return { ...f, cmp: op as Cmp, num: val };
    case 'stars':
      return op === '>=' && /^[2-5]$/.test(val) ? { ...f, values: [val] } : null;
    case 'type':
    case 'rating':
    case 'site':
      if (op !== '=' && op !== '!=') return null;
      return { ...f, not: op === '!=', values: val.split(',').filter(Boolean) };
  }
  return null;
}

const units = new Set(['s', 'm', 'h', 'd', 'w', 'mo', 'y']);
const amount = (n: string, unit: string) => {
  const u = unit === 'min' ? 'm' : unit;
  return units.has(u) ? t(`unit.amount.${u}`, { n: Number(n) }) : n + unit;
};
const dateWords = (d: string) => {
  const day = new Date(d.slice(0, 10) + 'T00:00');
  return isNaN(+day) ? d : date(day, { day: 'numeric', month: 'short', year: 'numeric' });
};
const symbol: Record<string, string> = { '<': '<', '<=': '≤', '=': '=', '>=': '≥', '>': '>', '!=': '≠' };

/**
 * A filter chip in plain words ("imported in the last 5 days", "longer than
 * 30 seconds"); null for text it doesn't know (shown as typed then).
 */
export function describe(text: string, comics = false): string | null {
  const low = text.toLowerCase();
  if (low === 'system:has_audio') return t('describe.hasAudio');
  if (low === 'system:expunged' || low === 'system:torrent' || low === 'system:animated' || low === 'system:new') return t(`describe.${low.slice(7)}`);
  const state = readState(low);
  if (state) return t(`describe.${state}`);
  if (isFavorite(low)) return t('describe.favorite');
  const m = /^system:([a-z_]+)(<=|>=|!=|<|>|=)(.+)$/.exec(low);
  if (!m) return null;
  const [, rawField, op, val] = m;
  if (rawField === 'found_by') {
    // (a subscription's new files, opened from the Downloads page)
    const name = subNames.of(Number(val));
    return t(op === '!=' ? 'describe.notFoundBy' : 'describe.foundBy', { name: name ? `“${name}”` : t('describe.aSubscription') });
  }
  const f = read(text);
  const field = f?.id ?? rawField;
  if (field === 'imported' || field === 'uploaded' || field === 'modified') {
    const w = comics && field !== 'uploaded' ? (field === 'imported' ? 'added' : 'updated') : field;
    const age = /^(\d+(?:\.\d+)?)(mo|min|[smhdwy])$/.exec(val);
    if (age) return t(op.startsWith('<') ? `describe.${w}.last` : `describe.${w}.ago`, { amount: amount(age[1], age[2]) });
    const words: Record<string, string> = { '>=': 'since', '>': 'after', '<': 'before', '<=': 'until', '=': 'on', '!=': 'notOn' };
    return t(`describe.${w}.${words[op]}`, { date: dateWords(val) });
  }
  switch (field) {
    case 'duration': {
      const d = /^(\d+(?:\.\d+)?)(s|m)?$/.exec(val);
      const x = d ? amount(d[1], d[2] ?? 's') : val;
      return op === '<' || op === '<=' ? t('describe.shorter', { x }) : op === '>' || op === '>=' ? t('describe.longer', { x }) : t('describe.length', { op: symbol[op], x });
    }
    case 'size': {
      const x = val.toUpperCase().replace(/^([\d.]+)([KMG])B?$/, '$1 $2B');
      return op === '<' || op === '<=' ? t('describe.smaller', { x }) : op === '>' || op === '>=' ? t('describe.larger', { x }) : t('describe.size', { op: symbol[op], x });
    }
    case 'width':
    case 'height':
      return t(`describe.${field}`, { op: symbol[op], x: val });
    case 'pages':
      return op === '<' ? t('describe.fewerPages', { n: Number(val) }) : op === '>' ? t('describe.morePages', { n: Number(val) }) : t('describe.pages', { op: symbol[op], n: Number(val) });
    case 'tags':
      return op === '<' ? t('describe.fewerTags', { n: Number(val) }) : op === '>' ? t('describe.moreTags', { n: Number(val) }) : t('describe.tags', { op: symbol[op], n: Number(val) });
    case 'type': {
      const names = val.split(',').map((v) => typeChoices.find((c) => c.value === v)?.label.toLowerCase().replace(/^(gif|mp4|webm|jpeg|png)$/, (s) => s.toUpperCase()) ?? v.toUpperCase());
      return t(op === '!=' ? 'describe.notType' : 'describe.type', { types: names.join(t('describe.or')) });
    }
    case 'rating':
      return t(op === '!=' ? 'describe.notRated' : 'describe.rated', { rating: val });
    case 'stars':
      return t('describe.stars', { n: val });
  }
  return null;
}

/** system:favorite in any of its spellings (favourite, fav, ...). */
export const isFavorite = (t: string) => /^system:(favou?rites?|fav|favou?rited)$/i.test(t);

/** A comics read-state filter (system:unread, system:reading, system:read and their other spellings). */
function readState(text: string): 'unread' | 'reading' | 'read' | null {
  const m = /^system:(unread|reading|in_progress|read|finished)$/i.exec(text);
  if (!m) return null;
  const w = m[1].toLowerCase();
  return w === 'in_progress' ? 'reading' : w === 'finished' ? 'read' : (w as 'unread' | 'reading' | 'read');
}
