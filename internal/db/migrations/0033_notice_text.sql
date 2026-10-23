-- The bell's notices for every device (2026-10-21): besides the downloader's events, what was done in the app and
-- set to go to the bell (Settings > Notifications) - its words (in the language it was done in), its tone (done,
-- info, removed, error) and its kind (the setting it follows).
ALTER TABLE notices ADD COLUMN text TEXT NOT NULL DEFAULT '';
ALTER TABLE notices ADD COLUMN tone TEXT NOT NULL DEFAULT '';
ALTER TABLE notices ADD COLUMN category TEXT NOT NULL DEFAULT '';
