# NixOS

The flake and module (`flake.nix`, `nix/`): running Vitrine natively on NixOS, with no containers. How to install it: the
README. Decided 2026-11-03 (the user: "i wouldnt want it to be docker based, i'd want it to be native", a public option).

## The package (`nix/package.nix`)
- The web app with `buildNpmPackage` (its source is the repository, as it reads `../locales` and `../CHANGELOG.md`), the
  program with `buildGo127Module`, pure Go as everywhere. ffmpeg-headless (has VA-API and libwebp) and vipsthumbnail by
  store path, through a wrapper setting `VITRINE_FFMPEG` and so on.
- **The version comes from the changelog's newest heading**, so the commit "Vitrine X.Y.Z" sets it; `## Unreleased` on
  top gives `dev`, which the update check never calls out of date.
- **Keeping it building**: a change in `go.sum` or `web/package-lock.json` needs a new `vendorHash` or `npmDepsHash`. The
  `nix` job in CI fails and prints the hash it got; put it in `nix/package.nix`. A release's `stable` branch only moves
  when the flake builds that release.
- `flake.lock` pins nixpkgs, and with it the tools (ffmpeg's security fixes come with moving it). It's nixos-unstable because
  stable releases don't have Go 1.27 yet.

## The module (`nix/module.nix`)
- `services.vitrine`: `dataDir`, `filesDir`, `listen`, `openFirewall`, `hwaccel`, `settings` (TOML), `environmentFile`.
  A system user, `Restart=always` (Settings > Backups' Restart now counts on it), the sandboxing options, and `render` +
  `video` groups for the GPU. Intel's driver goes in `hardware.graphics.extraPackages`; AMD's comes with Mesa (its
  `radeonsi_drv_video`); Nvidia's is `services.xserver.videoDrivers = [ "nvidia" ]`, and ffmpeg finds its libraries through
  its RUNPATH (`/run/opengl-driver/lib`). Only Intel has been run on real hardware; the other two were checked against nixpkgs.
- The `vitrine` command it installs knows the library and, run as root, runs as the service's user, so nothing in the
  library ends up root's.

## Updates
A running NixOS program can't replace itself (the store is read-only, the system's configuration says what runs). Three
ways, all moving only Vitrine's input:

- **By hand**: `nix flake update vitrine`, `nixos-rebuild switch`. Settings > Updates says so (`managed_by`: `nixos`).
- **Automatic** (`updates.automatic.enable`): a timer starts `vitrine-update.service`. Settings says it comes by itself
  (`nixos-auto`).
- **The Update button** (`updates.button`): the same job, with the full-screen updater.

The job (`vitrine-update.service`, as root, not restarted by its own switch: as `system.autoUpgrade`) moves the input; if
the lock didn't change it ends (`unchanged`), otherwise it rebuilds, and on a failure puts the lock back. It writes a word
to `/run/vitrine-update/status`: running, unchanged, failed, done.

**Why a path unit for the button**: a rebuild needs root and Vitrine has no rights. It can create one empty file in
`/run/vitrine-update-request`, which starts that fixed job through `vitrine-update.path`; nothing it writes reaches the job.
It works the same inside a network namespace, where a VPN would have it. `internal/server/updates.go` waits for the status
to change (none within a minute: the path unit isn't there), for up to 3 hours as without a binary cache it compiles. Its
success is this program being restarted, so any last word it reads is a failure. The page waits as long as the server says
it's going.

**A system flake in a user's Git repository** (2026-11-03, found testing): Nix won't open it as root ("not owned by current
user"), so the job has a `HOME` of its own with `safe.directory` for that one folder, and gives the changed lock back to the
folder's owner. The lock is left for them to commit.

**The backup**: the unit's `ExecStartPre` makes one when the version changes, whatever brought it; no backup, no start, as
with the button. The button makes its own first (it can still refuse the update then) and leaves the file `backed-up` beside
the request: the unit then doesn't make a second.

**Going back** (`nixos-rebuild switch --rollback`): an older program refuses a database a newer one changed, and says so
(migrations only go forward). `backup -unless-newer` doesn't stop the start for that (found testing: the backup failed, which
stopped the start, which also stopped `vitrine restore` from taking effect). The way out: `vitrine restore <the backup from
before the update>`, which the restarting service then picks up.

## What's been tested
`nix/test.nix` (a VM, in CI): starts sandboxed, the command, WebP and libvips, one backup per new version, restarted when it
ends. By hand on 2026-11-03, in an unprivileged LXC with an Intel GPU: VA-API works under the sandboxing; all three
ways with a local Git repository as the channel; the button with nothing new, with a release that doesn't build; a rollback
across a migration and its restore; a Git-kept flake owned by another user. Not tested by hand: a real `stable` branch and
a binary cache (they come with the first release).

## Prebuilt copies (optional)
The release workflow pushes to the Cachix cache `vitrine` when the secret `CACHIX_AUTH_TOKEN` is there. Without it, every
system compiles each new version (under a minute on four cores, once the tools are downloaded).

**FlareSolverr next to it** (nixpkgs' `services.flaresolverr`; 2026-11-12): its unit denies `@setuid`, which its browser's
virtual display (Xvfb) needs to start ("Failed to activate virtual core keyboard"), so it never passes a Cloudflare check.
Allow it again: `systemd.services.flaresolverr.serviceConfig.SystemCallFilter = lib.mkAfter [ "@setuid" ];`. The README's
NixOS example has it (optional, with `services.flaresolverr.enable`; the website's example too - the user: "There should
be a flaresolverr example for nixos somewhere"). Vitrine and FlareSolverr on one host share its address, as Cloudflare
needs; FlareSolverr's port stays closed (its module opens the firewall only when asked).
