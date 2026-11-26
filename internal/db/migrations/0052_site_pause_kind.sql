-- A site's pause can be a ban it named itself (2026-11-09: ExHentai bans an address for a while for loading too many
-- pages too fast, and says for how long): kind 'ban'; '' a site that kept failing to answer.
ALTER TABLE site_pauses ADD COLUMN kind TEXT NOT NULL DEFAULT '';
