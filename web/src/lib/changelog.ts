// CHANGELOG.md read into versions: the installed one's (built into the app) and a newer release's (Settings > Updates).

export type ChangelogPart = { text: string; bold: boolean; code?: boolean };
/** A version's "### " section (New, Improved, Fixed; '' for items before any) and its items. */
export type ChangelogGroup = { title: string; items: ChangelogPart[][] };
export type ChangelogVersion = { title: string; intro: string[]; groups: ChangelogGroup[]; items: ChangelogPart[][] };

/** "**A** b `c`" -> [{A, bold}, {b}, {c, code}]. */
const parts = (s: string): ChangelogPart[] =>
  s
    .split(/\*\*(.+?)\*\*/)
    .flatMap((text, i) =>
      text.split(/`([^`]+)`/).map((t, j) => ({ text: t, bold: i % 2 === 1, code: j % 2 === 1 })),
    )
    .filter((p) => p.text);

/** The changelog's "## version" sections: their plain lines, their "### " sections (New, Improved, Fixed) and their
 *  "- " items (a wrapped item's lines joined; items: all of them, in order). */
export function readChangelog(md: string): ChangelogVersion[] {
  const out: ChangelogVersion[] = [];
  let item: string | null = null;
  const flush = () => {
    if (item !== null && out.length) {
      const v = out[out.length - 1];
      if (!v.groups.length) v.groups.push({ title: '', items: [] });
      const it = parts(item);
      v.groups[v.groups.length - 1].items.push(it);
      v.items.push(it);
    }
    item = null;
  };
  for (const raw of md.split('\n')) {
    const line = raw.trimEnd();
    if (line.startsWith('## ')) {
      flush();
      out.push({ title: line.slice(3), intro: [], groups: [], items: [] });
    } else if (!out.length) continue;
    else if (line.startsWith('### ')) {
      flush();
      out[out.length - 1].groups.push({ title: line.slice(4).trim(), items: [] });
    }
    else if (line.startsWith('- ')) {
      flush();
      item = line.slice(2);
    } else if (item !== null && line.startsWith('  ')) item += ' ' + line.trim();
    else if (line.trim()) {
      flush();
      out[out.length - 1].intro.push(line.trim());
    } else flush();
  }
  flush();
  return out;
}

/** A section title's version ("1.0.2 - 2026-11-01" -> [1, 0, 2, 0]; a hotfix, "1.0.2-hotfix", one past it:
 *  [1, 0, 2, 1]); none for "Unreleased". */
function versionOf(s: string): number[] | null {
  const m = s.trim().match(/^v?(\d+)\.(\d+)\.(\d+)(-hotfix)?/);
  return m ? [Number(m[1]), Number(m[2]), Number(m[3]), m[4] ? 1 : 0] : null;
}

/** The released versions in a changelog newer than the installed one (newest first, as written). */
export function newerThan(versions: ChangelogVersion[], installed: string): ChangelogVersion[] {
  const now = versionOf(installed);
  if (!now) return [];
  return versions.filter((v) => {
    const n = versionOf(v.title);
    if (!n) return false;
    for (let i = 0; i < 4; i++) if (n[i] !== now[i]) return n[i] > now[i];
    return false;
  });
}
