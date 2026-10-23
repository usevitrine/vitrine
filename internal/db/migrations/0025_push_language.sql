-- Languages (2026-10-20): each device's push notifications come in its own
-- language (the web app's, sent when it turns them on or its language
-- changes); '' = the server's default (VITRINE_LANGUAGE, else en-US).
ALTER TABLE push_subscriptions ADD COLUMN language TEXT NOT NULL DEFAULT '';
