-- The work still waiting - thumbnails, near-duplicate hashes, preview frames - was found by walking the whole files
-- table from the newest file down until enough were found. With only a few waiting that reads every row: on a
-- 579k-file library importing two files a second it was half of Vitrine's own processor time (a CPU profile,
-- 2026-10-30). These partial indexes hold only the files still waiting (each leaves its index when done), and the
-- queries (library: pendingMedia, PendingPHash, PendingSprites) repeat their conditions word for word so SQLite
-- can use them. Flags: 1 probed, 2 thumbnail, 4 media error, 8 preview frames, 16 preview frames failed, 32
-- perceptual hash, 64 comic only. Types 6-9: the videos (MP4, QuickTime, WebM, Matroska).
CREATE INDEX files_pending_media ON files (id) WHERE deleted_at IS NULL AND flags & 3 != 3;
CREATE INDEX files_pending_phash ON files (id) WHERE deleted_at IS NULL AND flags & 98 = 2;
CREATE INDEX files_pending_sprite ON files (id) WHERE deleted_at IS NULL AND type BETWEEN 6 AND 9 AND flags & 25 = 1;
