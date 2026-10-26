# The website (vitrine.software)

The plan for the project's website at https://vitrine.software (the domain is registered; its DNS is on Cloudflare,
nothing is served yet). Features first, then how it's built and deployed (not decided yet).

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
- A browser test checks the demo build loads and searches.
