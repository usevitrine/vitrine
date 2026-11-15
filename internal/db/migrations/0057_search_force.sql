-- A one-post download "anyway" (2026-11-15, the user: "yes i want a download anyway" - a post the download rules,
-- the blacklist or a deletion record kept out): the search takes it whatever those say (download.AddForced).
ALTER TABLE searches ADD COLUMN force INTEGER NOT NULL DEFAULT 0;
