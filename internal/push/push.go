// Package push sends Web Push notifications to the browsers that turned them
// on (RFC 8030 delivery, RFC 8291 message encryption, RFC 8292 VAPID). It
// needs no third-party service: each browser's push service (Apple, Google,
// Mozilla) delivers the message. The server's VAPID key is made on first use
// and kept in meta.db.
package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/i18n"
)

// Subject identifies the sender to push services (the VAPID "sub" claim).
const Subject = "mailto:hello@vitrine.software" // (the project's contact address: push services write there about problems)

var b64 = base64.RawURLEncoding

// Subscription is a browser's push subscription (from PushManager.subscribe).
type Subscription struct {
	Endpoint       string `json:"endpoint"`
	ExpirationTime *int64 `json:"expirationTime"` // sent by browsers (usually null); not used
	Keys           struct {
		P256dh string `json:"p256dh"` // base64url
		Auth   string `json:"auth"`   // base64url
	} `json:"keys"`
}

// Device is a browser that gets notifications.
type Device struct {
	Endpoint  string `json:"endpoint"`
	Label     string `json:"label"`
	CreatedAt int64  `json:"created_at"`
	LastOKAt  int64  `json:"last_ok_at,omitempty"`
}

// Message is what a notification shows. URL is opened when it's tapped.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

// ErrGone means the browser's subscription no longer exists (it's removed).
var ErrGone = errors.New("push: the subscription is gone")

// Service sends notifications.
type Service struct {
	db   *db.DB
	key  *ecdsa.PrivateKey
	pub  []byte // uncompressed public key, 65 bytes
	http *http.Client
	log  *slog.Logger
	now  func() time.Time
}

// Open loads the VAPID key (making it on first use).
func Open(ctx context.Context, d *db.DB, log *slog.Logger) (*Service, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	raw, err := k.Bytes()
	if err != nil {
		return nil, err
	}
	if _, err := d.W.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES ('vapid_key', ?) ON CONFLICT (key) DO NOTHING", raw); err != nil {
		return nil, err
	}
	var stored []byte
	if err := d.R.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'vapid_key'").Scan(&stored); err != nil {
		return nil, err
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), stored)
	if err != nil {
		return nil, fmt.Errorf("push: bad VAPID key in meta.db: %w", err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	return &Service{db: d, key: key, pub: pub, http: &http.Client{Timeout: 30 * time.Second}, log: log, now: time.Now}, nil
}

// PublicKey is the server's VAPID public key, base64url (the browser's
// applicationServerKey; not a secret).
func (s *Service) PublicKey() string { return b64.EncodeToString(s.pub) }

// Subscribe adds (or updates) a browser; language is its notifications' (a language
// tag; "" or unknown: the server's default).
func (s *Service) Subscribe(ctx context.Context, sub Subscription, label, language string) error {
	p256dh, auth, err := sub.keys()
	if err != nil {
		return err
	}
	u, err := url.Parse(sub.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("push: the endpoint must be an https URL")
	}
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 100 {
		label = "A browser"
	}
	if !slices.Contains(i18n.Available(), language) {
		language = ""
	}
	_, err = s.db.W.ExecContext(ctx, `INSERT INTO push_subscriptions (endpoint, p256dh, auth, label, created_at, language)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth,
		label = excluded.label, language = excluded.language`, sub.Endpoint, p256dh, auth, label, s.now().UnixMilli(), language)
	return err
}

// languages lists the languages devices get notifications in (a device
// without its own gets the server's default).
func (s *Service) languages(ctx context.Context) ([]string, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT DISTINCT language FROM push_subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		l = i18n.For(l).Tag() // ("" or a language no longer there: the default)
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out, rows.Err()
}

// Language is the language a device gets notifications in.
func (s *Service) Language(ctx context.Context, endpoint string) string {
	var l string
	s.db.R.QueryRowContext(ctx, "SELECT language FROM push_subscriptions WHERE endpoint = ?", endpoint).Scan(&l)
	return i18n.For(l).Tag()
}

// Unsubscribe removes a browser.
func (s *Service) Unsubscribe(ctx context.Context, endpoint string) error {
	_, err := s.db.W.ExecContext(ctx, "DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

// Devices lists the browsers that get notifications, oldest first.
func (s *Service) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.R.QueryContext(ctx,
		"SELECT endpoint, label, created_at, coalesce(last_ok_at, 0) FROM push_subscriptions ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.Endpoint, &d.Label, &d.CreatedAt, &d.LastOKAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Send sends a message to every browser; it returns how many accepted it.
// Browsers whose subscription is gone are removed.
func (s *Service) Send(ctx context.Context, m Message) (int, error) {
	return s.send(ctx, "", "", m)
}

// sendLang sends a message to the browsers that get notifications in a language.
func (s *Service) sendLang(ctx context.Context, lang string, m Message) (int, error) {
	return s.send(ctx, "", lang, m)
}

// SendTo sends a message to one browser.
func (s *Service) SendTo(ctx context.Context, endpoint string, m Message) error {
	n, err := s.send(ctx, endpoint, "", m)
	if err == nil && n == 0 {
		err = errors.New("push: no such device")
	}
	return err
}

func (s *Service) send(ctx context.Context, only, lang string, m Message) (int, error) {
	q := "SELECT endpoint, p256dh, auth FROM push_subscriptions"
	args := []any{}
	switch {
	case only != "":
		q += " WHERE endpoint = ?"
		args = append(args, only)
	case lang != "":
		// (devices without a language of their own get the default)
		q += " WHERE language = ? OR (? = ? AND language NOT IN (SELECT value FROM json_each(?)))"
		known, _ := json.Marshal(i18n.Available())
		args = append(args, lang, lang, i18n.Default(), string(known))
	}
	rows, err := s.db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	type target struct {
		endpoint     string
		p256dh, auth []byte
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.endpoint, &t.p256dh, &t.auth); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, t)
	}
	rows.Close()
	payload, err := json.Marshal(m)
	if err != nil {
		return 0, err
	}
	sent := 0
	var errs []error
	for _, t := range targets {
		err := s.deliver(ctx, t.endpoint, t.p256dh, t.auth, payload)
		switch {
		case errors.Is(err, ErrGone):
			s.log.Info("push: removing a device whose subscription ended")
			s.Unsubscribe(ctx, t.endpoint)
			if only != "" {
				errs = append(errs, err)
			}
		case err != nil:
			errs = append(errs, err)
		default:
			sent++
			s.db.W.ExecContext(ctx, "UPDATE push_subscriptions SET last_ok_at = ? WHERE endpoint = ?",
				s.now().UnixMilli(), t.endpoint)
		}
	}
	return sent, errors.Join(errs...)
}

// deliver encrypts a payload for one browser and posts it to its push service.
func (s *Service) deliver(ctx context.Context, endpoint string, p256dh, auth, payload []byte) error {
	ua, err := ecdh.P256().NewPublicKey(p256dh)
	if err != nil {
		return fmt.Errorf("push: bad browser key: %w", err)
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	body, err := encrypt(payload, ua, auth, as, salt)
	if err != nil {
		return err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	jwt, err := s.vapidJWT(u.Scheme + "://" + u.Host)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", "86400") // a phone that's off gets it within a day
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+s.PublicKey())
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("push: sending to %s: %w", u.Host, err)
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("push: %s refused the message: %s %s", u.Host, resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// vapidJWT signs the VAPID token for a push service (aud = its origin).
func (s *Service) vapidJWT(aud string) (string, error) {
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{"aud": aud, "exp": s.now().Add(12 * time.Hour).Unix(), "sub": Subject})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + b64.EncodeToString(claims)
	h := sha256.Sum256([]byte(unsigned))
	r, sv, err := ecdsa.Sign(rand.Reader, s.key, h[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64) // ES256: r and s, 32 bytes each
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return unsigned + "." + b64.EncodeToString(sig), nil
}

// encrypt makes an RFC 8291 message (aes128gcm, one record): the header
// (salt, record size, the server's one-off public key) and the ciphertext.
func encrypt(plaintext []byte, ua *ecdh.PublicKey, auth []byte, as *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	const recordSize = 4096
	if len(plaintext)+17 > recordSize-86 {
		return nil, errors.New("push: message too long")
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	uaPub, asPub := ua.Bytes(), as.PublicKey().Bytes()
	prkKey, err := hkdf.Extract(sha256.New, shared, auth)
	if err != nil {
		return nil, err
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(uaPub)+string(asPub), 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 86+len(plaintext)+17)
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	// The last (only) record ends with the delimiter 0x02, no padding.
	return gcm.Seal(out, nonce, append(append([]byte{}, plaintext...), 0x02), nil), nil
}

func (sub Subscription) keys() (p256dh, auth []byte, err error) {
	dec := func(s string) ([]byte, error) {
		return b64.DecodeString(strings.TrimRight(s, "="))
	}
	if p256dh, err = dec(sub.Keys.P256dh); err != nil || len(p256dh) != 65 {
		return nil, nil, errors.New("push: bad p256dh key")
	}
	if _, err := ecdh.P256().NewPublicKey(p256dh); err != nil {
		return nil, nil, errors.New("push: bad p256dh key")
	}
	if auth, err = dec(sub.Keys.Auth); err != nil || len(auth) != 16 {
		return nil, nil, errors.New("push: bad auth secret")
	}
	return p256dh, auth, nil
}
