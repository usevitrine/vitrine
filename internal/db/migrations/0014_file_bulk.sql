-- Files that came in bulk: from a one-off download, a subscription's first
-- check or an import (not from a subscription's regular checks). The library
-- can leave them out of "new files" (setting newFromBulk).
ALTER TABLE files ADD COLUMN bulk INTEGER NOT NULL DEFAULT 0;
