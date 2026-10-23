-- Login. Vitrine has one user today, but accounts and sessions are keyed by
-- user so more users stay possible.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,    -- argon2id, PHC string format
    created_at    INTEGER NOT NULL
) STRICT;

-- A session is a random token in a cookie; only its SHA-256 is stored, so a
-- leaked database doesn't leak logins.
CREATE TABLE sessions (
    token_hash BLOB    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    user_agent TEXT
) STRICT, WITHOUT ROWID;
