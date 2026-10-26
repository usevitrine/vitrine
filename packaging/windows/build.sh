#!/usr/bin/env bash
# Builds Vitrine's Windows installer (Git Bash on Windows, with Inno Setup 6):
#   packaging/windows/build.sh <version> <Vitrine.exe> <vitrine.exe> <out dir>
# Vitrine.exe is the notification-area build (-H windowsgui -X main.gui=1), vitrine.exe the command line. The
# bundled tools are fetched from the pinned builds below (checked by their SHA-256; VITRINE_DL_CACHE keeps them).
set -euo pipefail

# ffmpeg: BtbN's LGPL build (shared libraries) - only its WebP, H.264-decoding and probing parts are used.
FFMPEG_URL=https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-09-30-13-08/ffmpeg-n8.1.3-9-g29e619e767-win64-lgpl-shared-8.1.zip
FFMPEG_SHA256=3e47bda1607740550141e37c0e49d1e5182b34699f15adfd137ee266d346811a
# libvips: the official "web" build (LGPL), static - vipsthumbnail.exe and one DLL.
VIPS_URL=https://github.com/libvips/build-win64-mxe/releases/download/v8.18.7/vips-dev-x64-web-8.18.7-static.zip
VIPS_SHA256=a736af61d3b46499760efa606e469cb14e060445b1f6fd36a1fa2e75009e1197

version=${1#v}; gui=$2; cli=$3; out=$4
# (the same file twice - e.g. one replaced by the other on Windows, which doesn't tell their names apart: Vitrine.exe
# would be the command line, which shuts at once)
if cmp -s "$gui" "$cli"; then echo "$gui and $cli are the same program: Vitrine.exe must be the notification-area build" >&2; exit 1; fi
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../.." && pwd)
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
cache=${VITRINE_DL_CACHE:-$work/dl}; mkdir -p "$cache" "$out"

fetch() { # url sha256 -> path
  local f="$cache/$(basename "$1")"
  [ -f "$f" ] || curl -fsSL --retry 3 -o "$f" "$1"
  echo "$2  $f" | sha256sum -c --quiet - >&2
  echo "$f"
}

stage="$work/stage"
mkdir -p "$stage/bin" "$stage/tools/ffmpeg" "$stage/tools/vips" "$stage/licenses"
cp "$gui" "$stage/Vitrine.exe"
cp "$cli" "$stage/bin/vitrine.exe"
cp "$root/README.md" "$root/config.example.toml" "$stage/"
cp "$root/LICENSE" "$stage/licenses/vitrine-LICENSE.txt"
cp "$here/THIRD-PARTY.txt" "$stage/licenses/"

ff=$(fetch "$FFMPEG_URL" "$FFMPEG_SHA256")
unzip -q "$ff" -d "$work/ff"
ffdir=$(echo "$work"/ff/*)
cp "$ffdir"/bin/ffmpeg.exe "$ffdir"/bin/ffprobe.exe "$ffdir"/bin/*.dll "$stage/tools/ffmpeg/"
cp "$ffdir/LICENSE.txt" "$stage/licenses/ffmpeg-LICENSE.txt"

vips=$(fetch "$VIPS_URL" "$VIPS_SHA256")
unzip -q "$vips" -d "$work/vips"
vdir=$(echo "$work"/vips/*)
cp "$vdir/bin/vipsthumbnail.exe" "$vdir/bin/libvips-42.dll" "$stage/tools/vips/"
cp "$vdir/LICENSE" "$stage/licenses/libvips-LICENSE.txt"
cp "$vdir/versions.json" "$stage/licenses/libvips-versions.json"

iscc=${ISCC:-}
for c in "$iscc" "${LOCALAPPDATA:-}/Programs/Inno Setup 6/ISCC.exe" "/c/Program Files (x86)/Inno Setup 6/ISCC.exe"; do
  if [ -n "$c" ] && [ -f "$c" ]; then iscc=$c; break; fi
done
[ -f "$iscc" ] || { echo "Inno Setup 6 (ISCC.exe) not found; set ISCC" >&2; exit 1; }
# (the file's version is numbers only: 1.2.3-hotfix -> 1.2.3.1)
num=${version%%-*}.0; [[ "$version" != *-hotfix ]] || num=${version%%-*}.1
"$iscc" -Q "-DVersion=$version" "-DNumVersion=$num" "-DStage=$(cygpath -w "$stage")" "-O$(cygpath -w "$out")" "$(cygpath -w "$here/vitrine.iss")"
echo "$out/Vitrine-Setup-$version-x64.exe"
