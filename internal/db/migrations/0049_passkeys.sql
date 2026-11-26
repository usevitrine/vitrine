-- Passkeys (WebAuthn): an account's passkeys, and whether it signs in with them only (the password refused).
-- webauthn_id: the account's random handle for passkeys (made with the first one; never its name or ID).
ALTER TABLE users ADD COLUMN webauthn_id BLOB;
ALTER TABLE users ADD COLUMN passkey_only INTEGER NOT NULL DEFAULT 0;

CREATE TABLE passkeys (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id BLOB    NOT NULL UNIQUE,
    credential    TEXT    NOT NULL, -- the library's whole record (public key, sign count, flags), JSON
    origin        TEXT    NOT NULL, -- the address it was made on (https://host): a sign-in counts only from there
    name          TEXT    NOT NULL,
    created_at    INTEGER NOT NULL,
    last_used     INTEGER
) STRICT;
CREATE INDEX passkeys_user ON passkeys (user_id);
