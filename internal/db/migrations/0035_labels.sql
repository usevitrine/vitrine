-- A name for a search or subscription where its search text says little (a gallery's link: its title, given when
-- it was added from Comics > Browse). NULL: the search text is the name.
ALTER TABLE searches ADD COLUMN label TEXT;
ALTER TABLE subscriptions ADD COLUMN label TEXT;
