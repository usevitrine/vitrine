# The website (vitrine.software)

**Live since 2026-10-26** at https://vitrine.software (and www): the repository `usevitrine/vitrine.software`
(public), deployed by its GitHub Actions workflow on every push to main. **The demo follows the app by itself**
(2026-10-26, the user: "when we update the app the demo updates with it?" - it didn't): every deploy builds it from
the `app/` submodule (`npm run build:demo` with Go and Node in CI), only its library - `demo-library.tar.gz`, demosnap's
`snapshot.json` and `media/` - from the site's `demo` release; `update-app.yml` checks daily for a new Vitrine release,
moves the submodule to its tag and starts a deploy (a push by the workflow's token starts no workflow). A release
reaches the site within a day; a new library is uploaded by hand (`gh release upload demo ... --clobber`). The
snapshot format (`internal/demo.Snapshot`) must stay readable by newer engines - a change to it means a new library.

The plan for the project's website at https://vitrine.software (its DNS is on Cloudflare). Features first, then how it's built and deployed (not decided yet).

## Look, tone, pages (decided 2026-10-26)
The user: "i want it beautiful, same color scheme as the app, lots of fancy animations, images of the media page,
comics page, the downloader, etc, a mix of serious and unserious."

- **The app's look**: Catppuccin Mocha (mauve accent, peach and green highlights), Plus Jakarta Sans, the app's
  rounded cards - the site and the demo feel like one thing. Big screenshots in laptop and phone frames, a soft
  mauve glow behind them.
- **Lots of animation, all smooth** (reduced motion respected): the hero's wall of thumbnails settling into Rows
  and the laptop rising out of it; sections sliding in on scroll, screenshots tilting a little and settling flat;
  the app's own moments (new files dropping into the grid with NEW pills, the Downloads stacks fanning out, a comic
  cover zooming into the reader, a chip typed and the grid reshuffling); a faint glow following the pointer on
  cards and buttons; counting numbers.
- **Screenshots of every part, at desktop and phone widths** (the user): media (Rows, squares), the viewer with
  details, comics and the reader, Downloads and subscriptions, Duplicates, Tags, Settings > Updates. Taken
  automatically (Playwright) from a library of the demo's CC0 art and Blender clips plus public-domain comics
  (e.g. Winsor McCay's Little Nemo, golden-age titles from the Digital Comic Museum) - SFW, legal, consistent, and
  retaken after a release. The downloader with the safebooru plugin and neutral searches.
- **Serious and unserious**: straight, confident headlines and facts; the jokes in the small print (e.g. "No cloud.
  No account. No one judging your 40,000 bookmarks."; a 404 that's an empty grid: "No results for this page. Try
  fewer tags."). The user: about that much.
- **Contact and GitHub on every page** (the user, 2026-10-26): a link to https://github.com/usevitrine/vitrine, and the
  contact email hello@vitrine.software **hidden from bots** - never in the HTML as text or a `mailto:`: put together
  by a script only when a person presses "Email us" (copied, or the mail app opened), with a plain hint for people
  without scripts that doesn't spell the address out either. (The address is already in Vitrine's User-Agent and Web
  Push subject in the public repository, so this cuts spam from the site's side only.)
- **Plugins** (the user, 2026-10-26: "also mention plugins somewhere"): a section in the features - the downloader
  learns a site from a small plugin file; installed and updated in Settings > Sites from the plugin list, or from a
  file; the kinds of site it speaks (booru-style image boards - Gelbooru, Danbooru, Shimmie - gallery sites, and
  Mastodon); "write your own" (a YAML file, no code) linking to the plugins repository. Only SFW sites are named
  (safebooru, built in; Danbooru's safebooru). The link to the plugins repository says what's there (decided with
  the user, 2026-10-26): "Plugins for more sites, some of them adult, are in the plugins repository." - no surprise
  for anyone clicking through (linking it is fine: the repository hosts no content, and a page is judged by its own
  content, not its links).
- **Never on the site: the "I came" button** (the user: "no i came button"), nor adult sites or content.
- **Pages**: the landing page (hero, features, the iPhone app, install, FAQ, footer) and **the demo on a page of its
  own**, opened by a button in the hero (the user).

## SEO (the user, 2026-10-26: "make sure it has excellent SEO")
- **Every page is real HTML, rendered ahead** (static, not a single-page app): search engines and link previews read
  the text without running scripts. The animations go on top and never hide content from them.
- **Fast pages**: good Core Web Vitals - images in AVIF/WebP with their sizes set (nothing jumps as they load),
  lazy loading below the fold, the hero's image preloaded, the font self-hosted and preloaded, little JavaScript.
  The animations must not cost loading speed or move the layout.
- **On every page**: its own title and description, a canonical URL, Open Graph and Twitter cards (a social image per
  page, from `docs/brand/`), one `h1` and headings in order, alt text on every image, `lang`.
- **For search engines**: `sitemap.xml`, `robots.txt`, structured data (JSON-LD `SoftwareApplication` with the
  version, platforms and license; `FAQPage` on the FAQ).
- **Words people search for**, in the headings and text, naturally: self-hosted media library, image and video
  library, tag-based / booru-style library, gallery downloader, Hydrus alternative, gallery-dl and Hydrus in one,
  Docker, Windows. Pages that answer a search on their own (e.g. "Vitrine vs Hydrus", installing with Docker) bring
  visitors best.
- The demo page has its own title and description, and a short text about it that search engines can read.

## Building and deploying (decided 2026-10-26)
- **The app's own code, not copies** (the user, 2026-10-26: "why isnt the demo reusing code from the project?" -> a
  git submodule): the site repository has this one as a submodule at `app/` and uses its colors
  (`web/src/lib/tokens.css`), its logo (`Logo.svelte`, through Astro's Svelte support) and its floating scrollbar
  (`lib/overlayScroll.ts`: element mode for the app, `'page'` and sideways modes for the site); the demo is built from
  the same checkout. The icons come from the same Lucide package (the site's own component, its own names). A change
  here reaches the site when its submodule is moved on.
- **The demo's library and the screenshots**: `tools/demolib` builds the library (cached downloads in
  %LOCALAPPDATA%VitrineDemo; every artwork, comic and clip looked at by eye - nudes, a shunga-style print and a
  propaganda poster left out by ID, Little Nemo only from 1905: from 1906 it has a racist caricature); `tools/siteshots`
  takes every screen at desktop (1920x1080) and phone (390x792 @2x) widths into the site's `src/assets/shots`.
- **Its own repository** (the user): `usevitrine/vitrine.software`, public, with the same git identity as the app.
- **Astro 6, fully static** (no adapter): pages rendered to HTML at build time; `astro:assets` `<Picture>` for
  AVIF/WebP in several sizes; `@astrojs/sitemap`; Plus Jakarta Sans self-hosted; animations in CSS and the browser's
  animation APIs, Svelte only for an interactive piece that needs it.
- **The demo comes from this repository**: the app's web client built in demo mode (a pretend server in the
  browser, the media screen only), published as a release file (`vitrine-demo.tar.gz`) that the site's build puts
  at `/demo/`. Its media (~100 MB) is a release file of the site repository, never in git.
- **Screenshots**: a script runs Vitrine on the screenshot library and Playwright captures every page at desktop and
  phone widths into the site's images - run when fresh ones are wanted (after a release).
- **Hosting: Cloudflare Workers with static assets** (Cloudflare's choice for new sites over Pages, which Astro's
  Cloudflare adapter no longer supports; a static site needs no adapter): `wrangler.jsonc` with `assets.directory:
  ./dist` and `not_found_handling: 404-page`; vitrine.software as a custom domain, www redirecting to it (the email
  records on the zone untouched).
- **Deploys by GitHub Actions** with `wrangler deploy` on every push to main, a preview version per branch - an API
  token, not the dashboard's Git connection (that needs the Cloudflare GitHub app authorized in the dashboard). The
  user chose an API token: limited to the account's Workers and the vitrine.software zone, kept by the user - in the
  site repository's Actions secrets (`CLOUDFLARE_API_TOKEN`, with `CLOUDFLARE_ACCOUNT_ID`) and, for local use, an
  environment variable on the PC; never printed, never in a file in a repository.

## The demo (decided 2026-10-26)
The user: "one thing i definitely want is a demo of vitrine on the site".

- **The real web app with a pretend server in the browser**, not a live public Vitrine (a public server that
  downloads on command is an open proxy, and visitors would share one library). A demo build of the same Svelte
  app, so it stays in step with Vitrine; static files, nothing runs on a server.
- **Only the media screen** (the user: "no features other than the media screen ... no sidebar, no downloads, no
  notifications"): the top bar with the logo and the search bar - no menu, Downloads button or bell. Working: the
  grid (Rows and squares, zoom, hover previews, the fast scroller), search (chips, suggestions, OR groups, negation,
  `system:` filters), sort and filters, the viewer (gestures, zoom, video controls, full screen) and the details
  panel with its tags (a tap searches) and sources.
- **Editable, per visit**: favorites, adding and removing tags, delete with undo. Edits live only in that tab's
  memory - nothing stored on the device, nothing sent anywhere: a reload starts fresh and no one else sees them
  (the user: "their edits reset when they reload and no one else can see the edits").
- **Content: CC0 / public-domain art and Blender clips** (the user's choice over safebooru art, which would be
  redistributing artists' work): about 300 artworks from museums' open-access collections (the Art Institute of
  Chicago, the Metropolitan Museum - CC0 images with data through public APIs) and about 25 short clips from the
  Blender open movies (Big Buck Bunny, Sintel, Spring...; CC-BY, credited on the site). Tags from their data
  (`artist:...`, medium, century, subjects, classification; `blender`, the film), the museum's or film's page as
  the source.
- **Made with Vitrine**: a script fetches them, imports them into a fresh library with their tags and sources and
  exports a snapshot (thumbnails, screen-sized copies, the data) that the demo reads - rebuilt with one run.

### As built (2026-10-26)
- **The search is Vitrine's own, as WebAssembly**: `cmd/demowasm` (GOOS=js GOARCH=wasm, ~6 MB) builds the index
  from the snapshot with its ordinary calls (`AddFile`, `AddTags`, `AddFileSite`...) and answers what the server's
  handlers would: search and its slices (with `find` and `shapes`), the fast scroller's dates, suggestions, tag
  edits (into the index and the details, added ones as the user's), tags-of, favorites, delete and undelete. Every
  search syntax and order works exactly as in the app - nothing rewritten in TypeScript.
- **The app in demo mode** (`npm run build:demo <snapshot folder>` -> `web/dist-demo`; vite `--mode demo`, base
  `/demo/app/`): `src/demo/backend.ts` takes the app's `/api/` fetches and answers from the engine, or with what a
  library of only those files says (no sites' downloads, notices, relations; saved searches kept for the visit;
  videos with their sound - `startMuted` off, the user: "make sure videos arent muted by default on the demo"); no
  service worker, install offer, version check or live events. `lib/demo.ts`'s `DEMO` hides the menu, Downloads and
  the bell. The router knows the base (`import.meta.env.BASE_URL`), so the app works in a folder.
- **The snapshot**: `tools/demosnap` from demolib's library -> `snapshot.json` (`internal/demo.Snapshot`: namespaces,
  tags, sites, each file's index columns, tags, sites, its `/api/files` info and its details) and `media/`: the
  library's own thumbnails, pictures at 1600 px (the demo can't make screen-sized copies on request), the clips as
  they are, the links' site icons (fetched once with `internal/favicon`). ~111 MB of media, 416 KB of data.
- **On the site**: `npm run demo [folder]` copies a built demo to `public/demo/app` (not in git); `/demo` is a page
  with a short text (for people and search engines) and the app in a frame filling the window, with Full screen
  (`/demo/app/`). Cloudflare serves the folder's index.html (its default HTML handling).
- **Tested**: `e2e/demo.spec.ts` with `DEMO_URL` set (a served build): it loads with no server, no menu or
  Downloads, a search from the suggestions, the details' tags and site, an edit found by a search and gone after a
  fresh load, and no warnings in the console.
