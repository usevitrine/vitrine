package auth

// Passkeys (WebAuthn, 2026-10-31, the user: "add passkey support with a passkey only login option"): an account's
// passkeys, signing in with one (no name typed: the passkey says whose it is), and signing in with passkeys only.
//
// A ceremony (adding a passkey, signing in) is two requests: its challenge is kept here, in memory, under a random ID
// the browser holds in a short-lived cookie, for 5 minutes, used once. Each passkey keeps the address it was made on
// (scheme and host); a sign-in counts only from that address - the server never trusts the request's Host for it (a
// page on another host that shares the passkey's domain could otherwise pass its own address off).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

var (
	// ErrNoPasskey means the passkey isn't one of this server's (or was removed).
	ErrNoPasskey = errors.New("this passkey isn't known here")
	// ErrCeremony means the passkey step took too long or was already used: start again.
	ErrCeremony = errors.New("that took too long - try again")
	// ErrNeedPasskey means passkey-only sign-in needs at least one passkey first.
	ErrNeedPasskey = errors.New("add a passkey first")
)

const ceremonyTTL = 5 * time.Minute

// Passkey is a passkey as the settings list it.
type Passkey struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	LastUsed  int64  `json:"last_used,omitempty"`
	Origin    string `json:"origin"`
}

// account is a user as the WebAuthn library sees it.
type account struct {
	id     int64
	name   string
	handle []byte
	creds  []webauthn.Credential
}

func (u *account) WebAuthnID() []byte                         { return u.handle }
func (u *account) WebAuthnName() string                       { return u.name }
func (u *account) WebAuthnDisplayName() string                { return u.name }
func (u *account) WebAuthnCredentials() []webauthn.Credential { return u.creds }

type ceremony struct {
	data    webauthn.SessionData
	user    int64 // adding a passkey: whose (0: signing in)
	origin  string
	expires time.Time
}

type ceremonies struct {
	mu  sync.Mutex
	all map[string]ceremony
}

var pending = ceremonies{all: map[string]ceremony{}}

func (c *ceremonies) put(cer ceremony) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.all {
		if now.After(v.expires) {
			delete(c.all, k)
		}
	}
	if len(c.all) >= 1000 { // (many begun, none finished: never more than this many kept)
		return "", ErrTooMany
	}
	c.all[id] = cer
	return id, nil
}

// take returns a ceremony once (a second try with it fails).
func (c *ceremonies) take(id string) (ceremony, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cer, ok := c.all[id]
	delete(c.all, id)
	if !ok || time.Now().After(cer.expires) {
		return ceremony{}, false
	}
	return cer, true
}

// Relying is the WebAuthn party for an address (origin: "https://host[:port]"): its RP ID is the host.
func Relying(origin string) (*webauthn.WebAuthn, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("auth: bad address %q", origin)
	}
	return webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "Vitrine",
		RPOrigins:     []string{origin},
	})
}

// loadAccount reads a user with their passkeys (and makes their passkey handle the first time).
func (a *Auth) loadAccount(ctx context.Context, id int64) (*account, error) {
	u := &account{id: id}
	err := a.db.R.QueryRowContext(ctx, "SELECT name, webauthn_id FROM users WHERE id = ?", id).Scan(&u.name, &u.handle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if len(u.handle) == 0 {
		h := make([]byte, 64)
		if _, err := rand.Read(h); err != nil {
			return nil, err
		}
		if _, err := a.db.W.ExecContext(ctx, "UPDATE users SET webauthn_id = ? WHERE id = ? AND webauthn_id IS NULL", h, id); err != nil {
			return nil, err
		}
		if err := a.db.R.QueryRowContext(ctx, "SELECT webauthn_id FROM users WHERE id = ?", id).Scan(&u.handle); err != nil {
			return nil, err
		}
	}
	rows, err := a.db.R.QueryContext(ctx, "SELECT credential FROM passkeys WHERE user_id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var js string
		if err := rows.Scan(&js); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(js), &c); err != nil {
			return nil, fmt.Errorf("auth: a stored passkey: %w", err)
		}
		u.creds = append(u.creds, c)
	}
	return u, rows.Err()
}

// BeginPasskey starts adding a passkey to a user's account on an address: the options for the browser, and the
// ceremony's ID.
func (a *Auth) BeginPasskey(ctx context.Context, userID int64, origin string) (*protocol.CredentialCreation, string, error) {
	wa, err := Relying(origin)
	if err != nil {
		return nil, "", err
	}
	u, err := a.loadAccount(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	exclude := make([]protocol.CredentialDescriptor, len(u.creds))
	for i := range u.creds {
		exclude[i] = u.creds[i].Descriptor() // (the same authenticator twice: refused by the browser)
	}
	creation, data, err := wa.BeginRegistration(u,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired), // (found without a name typed)
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired, // (the device's own lock: a passkey alone is enough)
		}),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return nil, "", err
	}
	id, err := pending.put(ceremony{data: *data, user: userID, origin: origin, expires: time.Now().Add(ceremonyTTL)})
	return creation, id, err
}

// FinishPasskey checks the browser's answer and keeps the new passkey under a name.
func (a *Auth) FinishPasskey(ctx context.Context, userID int64, ceremonyID, name string, body []byte) (Passkey, error) {
	cer, ok := pending.take(ceremonyID)
	if !ok || cer.user != userID {
		return Passkey{}, ErrCeremony
	}
	wa, err := Relying(cer.origin)
	if err != nil {
		return Passkey{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return Passkey{}, fmt.Errorf("the passkey's answer: %w", err)
	}
	u, err := a.loadAccount(ctx, userID)
	if err != nil {
		return Passkey{}, err
	}
	cred, err := wa.CreateCredential(u, cer.data, parsed)
	if err != nil {
		return Passkey{}, fmt.Errorf("the passkey wasn't accepted: %w", err)
	}
	js, err := json.Marshal(cred)
	if err != nil {
		return Passkey{}, err
	}
	name = cleanName(name)
	now := a.now().UnixMilli()
	res, err := a.db.W.ExecContext(ctx,
		"INSERT INTO passkeys (user_id, credential_id, credential, origin, name, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		userID, cred.ID, string(js), cer.origin, name, now)
	if err != nil {
		return Passkey{}, err
	}
	id, _ := res.LastInsertId()
	return Passkey{ID: id, Name: name, CreatedAt: now, Origin: cer.origin}, nil
}

func cleanName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	if utf8.RuneCountInString(name) > 60 {
		name = string([]rune(name)[:60])
	}
	return name
}

// BeginPasskeyLogin starts signing in with a passkey on an address (no name: the passkey says whose it is).
func (a *Auth) BeginPasskeyLogin(origin string) (*protocol.CredentialAssertion, string, error) {
	wa, err := Relying(origin)
	if err != nil {
		return nil, "", err
	}
	assertion, data, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	id, err := pending.put(ceremony{data: *data, origin: origin, expires: time.Now().Add(ceremonyTTL)})
	return assertion, id, err
}

// FinishPasskeyLogin checks the browser's answer and starts a session: the token, and whose it is.
func (a *Auth) FinishPasskeyLogin(ctx context.Context, ceremonyID string, body []byte, userAgent string) (string, User, error) {
	cer, ok := pending.take(ceremonyID)
	if !ok || cer.user != 0 {
		return "", User{}, ErrCeremony
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return "", User{}, fmt.Errorf("the passkey's answer: %w", err)
	}
	// The passkey: one of ours, made on this very address.
	var userID, keyID int64
	var origin string
	err = a.db.R.QueryRowContext(ctx, "SELECT id, user_id, origin FROM passkeys WHERE credential_id = ?", parsed.RawID).Scan(&keyID, &userID, &origin)
	if errors.Is(err, sql.ErrNoRows) {
		return "", User{}, ErrNoPasskey
	}
	if err != nil {
		return "", User{}, err
	}
	if origin != cer.origin {
		return "", User{}, ErrNoPasskey
	}
	wa, err := Relying(origin)
	if err != nil {
		return "", User{}, err
	}
	u, err := a.loadAccount(ctx, userID)
	if err != nil {
		return "", User{}, err
	}
	found := func(rawID, handle []byte) (webauthn.User, error) {
		if string(handle) != string(u.handle) {
			return nil, ErrNoPasskey
		}
		return u, nil
	}
	_, cred, err := wa.ValidatePasskeyLogin(found, cer.data, parsed)
	if err != nil {
		return "", User{}, fmt.Errorf("the passkey wasn't accepted: %w", err)
	}
	// (its signature counter and flags as they are now: a cloned authenticator shows as a counter going back)
	if cred.Authenticator.CloneWarning {
		return "", User{}, errors.New("the passkey wasn't accepted: its counter went back (a copied authenticator?)")
	}
	if js, err := json.Marshal(cred); err == nil {
		a.db.W.ExecContext(ctx, "UPDATE passkeys SET credential = ?, last_used = ? WHERE id = ?", string(js), a.now().UnixMilli(), keyID)
	}
	token, err := a.startSession(ctx, userID, userAgent)
	return token, User{ID: userID, Name: u.name}, err
}

// Passkeys lists a user's passkeys, oldest first.
func (a *Auth) Passkeys(ctx context.Context, userID int64) ([]Passkey, error) {
	rows, err := a.db.R.QueryContext(ctx,
		"SELECT id, name, created_at, coalesce(last_used, 0), origin FROM passkeys WHERE user_id = ? ORDER BY id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.LastUsed, &p.Origin); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RemovePasskey removes one of a user's passkeys. The last one gone, passkey-only sign-in goes too (else nothing could
// sign in).
func (a *Auth) RemovePasskey(ctx context.Context, userID, id int64) error {
	tx, err := a.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "DELETE FROM passkeys WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoPasskey
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE users SET passkey_only = 0 WHERE id = ? AND NOT EXISTS (SELECT 1 FROM passkeys WHERE user_id = ?)", userID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// RenamePasskey names one of a user's passkeys.
func (a *Auth) RenamePasskey(ctx context.Context, userID, id int64, name string) error {
	res, err := a.db.W.ExecContext(ctx, "UPDATE passkeys SET name = ? WHERE id = ? AND user_id = ?", cleanName(name), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoPasskey
	}
	return nil
}

// PasskeyOnly tells whether the account signs in with passkeys only (with at least one passkey to do it with).
func (a *Auth) PasskeyOnly(ctx context.Context) (bool, error) {
	var n int
	err := a.db.R.QueryRowContext(ctx,
		"SELECT count(*) FROM users u WHERE u.passkey_only = 1 AND EXISTS (SELECT 1 FROM passkeys p WHERE p.user_id = u.id)").Scan(&n)
	return n > 0, err
}

// SetPasskeyOnly turns passkey-only sign-in on (with a passkey to sign in with) or off.
func (a *Auth) SetPasskeyOnly(ctx context.Context, userID int64, on bool) error {
	if on {
		var n int
		if err := a.db.R.QueryRowContext(ctx, "SELECT count(*) FROM passkeys WHERE user_id = ?", userID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNeedPasskey
		}
	}
	_, err := a.db.W.ExecContext(ctx, "UPDATE users SET passkey_only = ? WHERE id = ?", on, userID)
	return err
}
