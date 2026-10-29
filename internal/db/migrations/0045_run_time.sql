-- How long a search has been working (2026-10-29, the user: "Downloads should have how long they've been running for
-- somewhere. That pauses when the download is paused for any reason"): ms in its runs so far - counted only while a
-- worker runs it (paused, waiting in the queue, stepped aside or its site not answering: the clock stands).
ALTER TABLE searches ADD COLUMN run_ms INTEGER NOT NULL DEFAULT 0;
