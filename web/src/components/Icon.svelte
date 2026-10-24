<script lang="ts">
  // A small built-in icon set (24x24 strokes), so no icon font is needed.
  const paths = {
    library: 'M7 3h11a3 3 0 0 1 3 3v10M5.5 7h9a2.5 2.5 0 0 1 2.5 2.5v9a2.5 2.5 0 0 1-2.5 2.5h-9A2.5 2.5 0 0 1 3 18.5v-9A2.5 2.5 0 0 1 5.5 7zM3.5 18l4-4 3 3 2-2 4 4M11.3 11a1.2 1.2 0 1 0 2.4 0a1.2 1.2 0 1 0-2.4 0z', // stacked pictures
    download: 'M12 4v11m0 0-4-4m4 4 4-4M5 20h14',
    tag: 'M3 12V4h8l9 9-8 8-9-9zM7.5 7.5h.01',
    settings: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z',
    close: 'M6 6l12 12M18 6 6 18',
    search: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zm9 2-4.3-4.3',
    info: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5m0-8h.01',
    alert: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v6m0 4h.01',
    trash: 'M4 7h16M9 7V4h6v3m-8 0 1 13h8l1-13',
    play: 'M7 4v16l13-8z',
    pause: 'M9 5v14M15 5v14',
    volume: 'M4 9v6h4l5 4V5L8 9zM16 9a4 4 0 0 1 0 6m2.5-8.5a7.5 7.5 0 0 1 0 11',
    mute: 'M4 9v6h4l5 4V5L8 9zM17 9l5 6m0-6-5 6',
    loop: 'M17 2l3 3-3 3M4 11V9a4 4 0 0 1 4-4h12M7 22l-3-3 3-3m13-3v2a4 4 0 0 1-4 4H4',
    plus: 'M12 5v14M5 12h14',
    minus: 'M5 12h14',
    left: 'M15 5l-7 7 7 7',
    right: 'M9 5l7 7-7 7',
    link: 'M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1',
    pencil: 'M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16v4zM13.5 6.5l4 4',
    globe: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM3 12h18M12 3c2.5 2.7 3.8 5.7 3.8 9s-1.3 6.3-3.8 9c-2.5-2.7-3.8-5.7-3.8-9s1.3-6.3 3.8-9z',
    device: 'M8 3h8a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H8a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1zM11 18h2',
    archive: 'M3 5h18v4H3zM5 9v10a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V9M10 13h4',
    user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 21a8 8 0 0 1 16 0',
    copy: 'M8 8h11a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1zM16 8V5a1 1 0 0 0-1-1H4a1 1 0 0 0-1 1v11a1 1 0 0 0 1 1h4',
    block: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM5.6 5.6l12.8 12.8',
    external: 'M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5',
    expand: 'M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5',
    shrink: 'M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5',
    or: 'M5 14c1.5-2.6 3.6-3.3 6-1.8l2 1.3c2.4 1.5 4.5.8 6-1.8', // a tilde: the search's OR (a ~ b)
    split: 'M12 21v-7m0 0-6-6m6 6 6-6M4 4h4v4M20 4h-4v4',
    check: 'M5 12l5 5L20 7',
    undo: 'M9 14 4 9l5-5M4 9h11a5 5 0 0 1 0 10h-3',
    menu: 'M4 7h16M4 12h16M4 17h16',
    chevron: 'm6 9 6 6 6-6',
    sort: 'M4 7h16M4 12h10M4 17h5', // like Material's "sort" (hydrus-web's sort button)
    grip: 'M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01',
    retry: 'M4 12a8 8 0 1 0 2.3-5.7M4 4v4h4',
    star: 'M12 3.5l2.6 5.3 5.9.9-4.3 4.1 1 5.8-5.2-2.7-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z',
    up: 'M12 19V5m0 0-6 6m6-6 6 6',
    share: 'M12 3v12M12 3 8 7m4-4 4 4M8 10H6a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-9a1 1 0 0 0-1-1h-2',
    addbox: 'M5 4h14a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1zM12 8v8M8 12h8',
    eye: 'M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
    eyeoff: 'M3 3l18 18M10.6 5.1A10 10 0 0 1 12 5c6.4 0 10 7 10 7a17 17 0 0 1-3.2 4M6.6 6.6C3.8 8.4 2 12 2 12s3.6 7 10 7c1.9 0 3.5-.6 4.9-1.4M9.9 9.9a3 3 0 0 0 4.2 4.2',
    clock: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3 2',
    bell: 'M6 9a6 6 0 1 1 12 0c0 6 2.5 8 2.5 8h-17S6 15 6 9M10 21h4',
    filter: 'M4 6h16M7 12h10M10 18h4',
    stack: 'M7 4h10M5 7h14M4 10h16v10H4z', // a stack of covers (a collection)
    folder: 'M3 7.5A1.5 1.5 0 0 1 4.5 6H9l2 2h8.5A1.5 1.5 0 0 1 21 9.5v8a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5z',
    more: 'M5 12h.01M12 12h.01M19 12h.01',
    morev: 'M12 5h.01M12 12h.01M12 19h.01',
    drop: 'M10.31 22.09Q9.5 16.05 10.79 12.1A4.6 4.6 0 1 1 16.5 17.81Q12.36 18.91 10.31 22.09ZM3.75 9.91Q3.42 6.62 4.22 4.46A2.3 2.3 0 1 1 7.14 7.38Q4.88 8.08 3.75 9.91ZM17.67 7.04Q17.41 4.64 17.99 3.06A1.7 1.7 0 1 1 20.14 5.21Q18.49 5.71 17.67 7.04Z', // "I came": droplets as in the emoji, tails curling back
    checks: 'M4 12.5l3.5 3.5L14 9.5M10 16l1.5 1.5L20 9',
    heart: 'M12 20.5s-7.5-4.6-9.6-9.4A5.2 5.2 0 0 1 12 5.6a5.2 5.2 0 0 1 9.6 5.5c-2.1 4.8-9.6 9.4-9.6 9.4z',
    chart: 'M5 20v-8M12 20V5M19 20v-5M3 20h18',
    depth: 'M4 5h16M4 10h16M4 15h7M17 13v7M14 17l3 3 3-3',
    book: 'M3 5.5c2.7-1.5 6-1.5 9 .5 3-2 6.3-2 9-.5V19c-2.7-1.5-6-1.5-9 .5-3-2-6.3-2-9-.5zM12 6v13.5',
    bookcheck: 'M2 5c2-1.1 4.5-1.1 7 .4 2.5-1.5 5-1.5 7-.4v8.5M2 5v10c2-1.1 4.5-1.1 7 .4 1-.6 2-1 3-1.2M9 5.4v10M14 18.5l2.5 2.5 5-5', // a book, read (a check)
    bookreading: 'M2 5c2-1.1 4.5-1.1 7 .4 2.5-1.5 5-1.5 7-.4v6M2 5v10c2-1.1 4.5-1.1 7 .4 1-.6 2-1 3-1.2M9 5.4v10M21.5 18a3.5 3.5 0 1 1-1.1-2.5M21 13.5v2.5h-2.5', // a book being read (a turning arrow)
    pages: 'M8 3h8l4 4v11H8zM16 3v4h4M5 7v14h11', // pages: a sheet over another
    crop: 'M6 2v14a2 2 0 0 0 2 2h14M2 6h14a2 2 0 0 1 2 2v14',
    broom: 'M12 3v7M7.5 10h9l1.5 10H6zM10 14.5V20M14 14.5V20',
  } as const;

  let { name, size = 20, filled = false }: { name: keyof typeof paths; size?: number; filled?: boolean } = $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill={filled ? 'currentColor' : 'none'}
  stroke="currentColor"
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"><path d={paths[name]} /></svg>
