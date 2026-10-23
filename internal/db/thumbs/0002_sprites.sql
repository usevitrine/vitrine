-- Videos' preview frames for the scrub bar (media.Sprite), regenerable like
-- the thumbnails.
CREATE TABLE sprites (
    file_id     INTEGER PRIMARY KEY,
    version     INTEGER NOT NULL,   -- media.SpriteVersion that made it
    frames      INTEGER NOT NULL,
    cols        INTEGER NOT NULL,
    tile_w      INTEGER NOT NULL,
    tile_h      INTEGER NOT NULL,
    interval_ms INTEGER NOT NULL,
    data        BLOB    NOT NULL    -- WebP
) STRICT;
