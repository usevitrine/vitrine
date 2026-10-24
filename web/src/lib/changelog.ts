// CHANGELOG.md read into versions: the installed one's (built into the app) and a newer release's (Settings > Updates).

export type ChangelogPart = { text: string; bold: boolean };
export type ChangelogVersion = { title: string; intro: string[]; items: ChangelogPart[][] };

/** "**A** b" -> [{A, bold}, {b}]. */
const parts = (s: string): ChangelogPart[] => s.split(/\*\*(.+?)\*\*/).map((text, i) => ({ text, bold: i % 2 === 1 })).filter((p) => p.text);

/** The changelog's "## version" sections: their plain lines and their "- " items (a wrapped item's lines joined). */
export function readChangelog(md: string): ChangelogVersion[] {
  const out: ChangelogVersion[] = [];
  let item: string | null = null;
  const flush = () => {
    if (item !== null && out.length) out[out.length - 1].items.push(parts(item));
    item = null;
  };
  for (const raw of md.split('\n')) {
    const line = raw.trimEnd();
    if (line.startsWith('## ')) {
      flush();
      out.push({ title: line.slice(3), intro: [], items: [] });
    } else if (!out.length) continue;
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

/** A section title's version ("1.0.2 - 2026-11-01" -> [1, 0, 2]); none for "Unreleased". */
function versionOf(s: string): number[] | null {
  const m = s.trim().match(/^v?(\d+)\.(\d+)\.(\d+)/);
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

/** The released versions in a changelog newer than the installed one (newest first, as written). */
export function newerThan(versions: ChangelogVersion[], installed: string): ChangelogVersion[] {
  const now = versionOf(installed);
  if (!now) return [];
  return versions.filter((v) => {
    const n = versionOf(v.title);
    if (!n) return false;
    for (let i = 0; i < 3; i++) if (n[i] !== now[i]) return n[i] > now[i];
    return false;
  });
}
