// Package auth handles the login: the account, password hashes and sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/usevitrine/vitrine/internal/db"
)

var (
	// ErrBadLogin means the name or password is wrong (deliberately not
	// saying which).
	ErrBadLogin = errors.New("wrong name or password")
	// ErrTooMany means too many failed logins lately; try again shortly.
	ErrTooMany = errors.New("too many failed logins, wait a minute")
	// ErrNoSession means the session token is unknown or expired.
	ErrNoSession = errors.New("not logged in")
	// ErrSetupDone means an account already exists.
	ErrSetupDone = errors.New("an account already exists")
	// ErrPasskeyOnly means the account signs in with a passkey only (the password is refused).
	ErrPasskeyOnly = errors.New("this account signs in with a passkey only")
	// ErrBadPassword means the current password given to change the account was wrong.
	ErrBadPassword = errors.New("the current password is wrong")
)

// SessionIdle is how long a session lasts without use. Long, because the
// phone's home-screen app should stay logged in.
const SessionIdle = 180 * 24 * time.Hour

// MinPassword is the shortest accepted password, MaxPassword the longest (in bytes: a megabyte-long one was hashed on
// every try - work for nothing).
const (
	MinPassword = 8
	MaxPassword = 1024
)

// User is a logged-in account.
type User struct {
	ID   int64
	Name string
}

// Auth checks logins against meta.db.
type Auth struct {
	db  *db.DB
	now func() time.Time

	mu       sync.Mutex
	failures []time.Time // recent failed logins, for the rate limit
}

// New returns an Auth using meta.db.
func New(d *db.DB) *Auth {
	return &Auth{db: d, now: time.Now}
}

// Owner returns the account (the first one; there is one): requests through the server's local socket act as it.
// No account yet: ErrNoSession.
func (a *Auth) Owner(ctx context.Context) (User, error) {
	var u User
	err := a.db.R.QueryRowContext(ctx, "SELECT id, name FROM users ORDER BY id LIMIT 1").Scan(&u.ID, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoSession
	}
	return u, err
}

// SetupNeeded reports whether no account exists yet.
func (a *Auth) SetupNeeded(ctx context.Context) (bool, error) {
	var n int
	err := a.db.R.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n)
	return n == 0, err
}

func checkNew(name, password string) error {
	if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
		return fmt.Errorf("the name can't be empty or start/end with spaces")
	}
	if utf8.RuneCountInString(password) < MinPassword {
		return fmt.Errorf("the password needs at least %d characters", MinPassword)
	}
	if len(password) > MaxPassword {
		return fmt.Errorf("the password can't be longer than %d characters", MaxPassword)
	}
	if utf8.RuneCountInString(name) > 100 {
		return fmt.Errorf("the name can't be longer than 100 characters")
	}
	return nil
}

// Setup creates the first account. It fails once any account exists, so a
// stranger can't take over a running instance.
func (a *Auth) Setup(ctx context.Context, name, password string) error {
	if err := checkNew(name, password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := a.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrSetupDone
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO users (name, password_hash, created_at) VALUES (?, ?, ?)",
		name, hash, a.now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPassword sets a user's password, creating the user if needed, and ends
// their sessions. It is for the command line (password reset) - the way back in
// with every passkey lost, so it turns passkey-only sign-in off too.
func (a *Auth) SetPassword(ctx context.Context, name, password string) error {
	if err := checkNew(name, password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := a.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (name, password_hash, created_at) VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET password_hash = excluded.password_hash, passkey_only = 0`,
		name, hash, a.now().UnixMilli()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM sessions WHERE user_id = (SELECT id FROM users WHERE name = ?)", name); err != nil {
		return err
	}
	return tx.Commit()
}

// confirm checks the current password of a signed-in user (to change the account): rate-limited like a login.
func (a *Auth) confirm(ctx context.Context, userID int64, password string) error {
	var name string
	if err := a.db.R.QueryRowContext(ctx, "SELECT name FROM users WHERE id = ?", userID).Scan(&name); err != nil {
		return err
	}
	if _, err := a.checkLogin(ctx, name, password); errors.Is(err, ErrBadLogin) {
		return ErrBadPassword
	} else if err != nil {
		return err
	}
	return nil
}

// ChangePassword sets a signed-in user's password (the current one first) and ends their other sessions (keep: the
// token of the one making the change).
func (a *Auth) ChangePassword(ctx context.Context, userID int64, current, password, keep string) error {
	if err := a.confirm(ctx, userID, current); err != nil {
		return err
	}
	if err := checkNew("x", password); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	keepHash := sha256.Sum256([]byte(keep))
	tx, err := a.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash != ?", userID, keepHash[:]); err != nil {
		return err
	}
	return tx.Commit()
}

// Rename changes a signed-in user's name (the current password first).
func (a *Auth) Rename(ctx context.Context, userID int64, current, name string) error {
	if err := a.confirm(ctx, userID, current); err != nil {
		return err
	}
	if err := checkNew(name, strings.Repeat("x", MinPassword)); err != nil {
		return err
	}
	res, err := a.db.W.ExecContext(ctx, "UPDATE users SET name = ? WHERE id = ? AND NOT EXISTS (SELECT 1 FROM users WHERE name = ? AND id != ?)",
		name, userID, name, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("that name is taken")
	}
	return nil
}

// EndOtherSessions logs out every other device of a user (keep: this one's token).
func (a *Auth) EndOtherSessions(ctx context.Context, userID int64, keep string) (int, error) {
	keepHash := sha256.Sum256([]byte(keep))
	res, err := a.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND token_hash != ?", userID, keepHash[:])
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Login checks a name and password and starts a session. The returned token
// goes into the session cookie.
func (a *Auth) Login(ctx context.Context, name, password, userAgent string) (string, error) {
	// Passkey only: no password is even checked (a different answer for a right one would tell a guesser it was).
	if only, err := a.PasskeyOnly(ctx); err != nil {
		return "", err
	} else if only {
		return "", ErrPasskeyOnly
	}
	id, err := a.checkLogin(ctx, name, password)
	if err != nil {
		return "", err
	}
	return a.startSession(ctx, id, userAgent)
}

// checkLogin checks a name and password (rate-limited, one at a time); it returns the user's ID.
func (a *Auth) checkLogin(ctx context.Context, name, password string) (int64, error) {
	if !a.allowAttempt() {
		return 0, ErrTooMany
	}
	if len(password) > MaxPassword {
		a.recordFailure()
		return 0, ErrBadLogin
	}
	var id int64
	var hash string
	err := a.db.R.QueryRowContext(ctx, "SELECT id, password_hash FROM users WHERE name = ?", name).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		hash = dummyHash() // same work as a real check, so timing doesn't reveal names
	} else if err != nil {
		return 0, err
	}
	ok, err := a.checkPassword(ctx, hash, password)
	if err != nil {
		return 0, err
	}
	if !ok || id == 0 {
		a.recordFailure()
		return 0, ErrBadLogin
	}
	return id, nil
}

// checkPassword checks a password against its hash, at most two at a time: each check takes 64 MiB, and many tries
// at once (before any counted as failed) could take all the server's memory. A try that can't get its turn within a
// few seconds counts as too many.
func (a *Auth) checkPassword(ctx context.Context, phc, password string) (bool, error) {
	select {
	case hashing <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(5 * time.Second):
		return false, ErrTooMany
	}
	defer func() { <-hashing }()
	return checkPassword(phc, password), nil
}

var hashing = make(chan struct{}, 2)

// startSession makes a session for a user; its token goes into the session cookie.
func (a *Auth) startSession(ctx context.Context, id int64, userAgent string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	now := a.now().UnixMilli()
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	if _, err := a.db.W.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, last_seen, user_agent) VALUES (?, ?, ?, ?, ?)",
		sum[:], id, now, now, userAgent); err != nil {
		return "", err
	}
	return token, nil
}

// Session looks up a session token. Use keeps it alive (last_seen is
// written at most once an hour).
func (a *Auth) Session(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrNoSession
	}
	sum := sha256.Sum256([]byte(token))
	var u User
	var lastSeen int64
	err := a.db.R.QueryRowContext(ctx, `
		SELECT u.id, u.name, s.last_seen FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, sum[:]).Scan(&u.ID, &u.Name, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoSession
	}
	if err != nil {
		return User{}, err
	}
	now := a.now()
	if now.Sub(time.UnixMilli(lastSeen)) > SessionIdle {
		a.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", sum[:])
		return User{}, ErrNoSession
	}
	if now.Sub(time.UnixMilli(lastSeen)) > time.Hour {
		a.db.W.ExecContext(ctx, "UPDATE sessions SET last_seen = ? WHERE token_hash = ?", now.UnixMilli(), sum[:])
	}
	return u, nil
}

// Logout ends a session.
func (a *Auth) Logout(ctx context.Context, token string) error {
	sum := sha256.Sum256([]byte(token))
	_, err := a.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", sum[:])
	return err
}

// The rate limit: at most 10 failed logins a minute and 60 an hour, from anywhere (guessing a password: at most 1,440
// tries a day, not 14,400 - 2026-10-31). With a single account a global limit is simple and needs no trust in proxy
// headers; a passkey isn't held up by it (nothing to guess).
const (
	maxFailures     = 10
	failureWindow   = time.Minute
	maxFailuresHour = 60
)

func (a *Auth) allowAttempt() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	cutoff := now.Add(-time.Hour)
	i := 0
	for i < len(a.failures) && a.failures[i].Before(cutoff) {
		i++
	}
	a.failures = a.failures[i:]
	if len(a.failures) >= maxFailuresHour {
		return false
	}
	recent := 0
	for _, t := range a.failures {
		if !t.Before(now.Add(-failureWindow)) {
			recent++
		}
	}
	return recent < maxFailures
}

func (a *Auth) recordFailure() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures = append(a.failures, a.now())
}

// Password hashing: argon2id with RFC 9106's lower-memory recommendation
// (64 MiB, 3 passes, 4 lanes), stored in the usual PHC string format.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

func checkPassword(phc, password string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked against when the name doesn't exist (made on first
// use, not at start-up).
var dummyHash = sync.OnceValue(func() string {
	h, _ := hashPassword("not a real password")
	return h
})

// MediaKey returns the server's secret for signing file links (made on
// first use, kept in meta.db). Signed links let media load without the
// session cookie: some browsers' video loaders don't send it.
func (a *Auth) MediaKey(ctx context.Context) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if _, err := a.db.W.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES ('media_key', ?) ON CONFLICT (key) DO NOTHING", key); err != nil {
		return nil, err
	}
	var stored []byte
	if err := a.db.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'media_key'").Scan(&stored); err != nil {
		return nil, err
	}
	if len(stored) != 32 {
		return nil, fmt.Errorf("auth: bad media key in meta.db")
	}
	return stored, nil
}
