package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/usevitrine/vitrine/internal/db"
)

func newTest(t *testing.T) *Auth {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d)
}

func TestSetupLoginLogout(t *testing.T) {
	a := newTest(t)
	ctx := context.Background()

	if need, err := a.SetupNeeded(ctx); err != nil || !need {
		t.Fatalf("SetupNeeded = %v, %v", need, err)
	}
	if err := a.Setup(ctx, "me", "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := a.Setup(ctx, " me", "long enough"); err == nil {
		t.Fatal("name with spaces accepted")
	}
	if err := a.Setup(ctx, "me", "correct horse"); err != nil {
		t.Fatal(err)
	}
	if err := a.Setup(ctx, "other", "correct horse"); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("second setup: %v", err)
	}

	if _, err := a.Login(ctx, "me", "wrong password", ""); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := a.Login(ctx, "nobody", "correct horse", ""); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("unknown user: %v", err)
	}
	token, err := a.Login(ctx, "ME", "correct horse", "test agent") // names ignore case
	if err != nil {
		t.Fatal(err)
	}
	u, err := a.Session(ctx, token)
	if err != nil || u.Name != "me" {
		t.Fatalf("Session = %+v, %v", u, err)
	}
	if _, err := a.Session(ctx, token+"x"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("bad token: %v", err)
	}

	// Only the token's hash is stored.
	var stored []byte
	a.db.R.QueryRow("SELECT token_hash FROM sessions").Scan(&stored)
	if strings.Contains(string(stored), token) || len(stored) != 32 {
		t.Fatal("session token stored in the clear")
	}

	if err := a.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Session(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("after logout: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	a := newTest(t)
	ctx := context.Background()
	a.Setup(ctx, "me", "correct horse")
	clock := time.Now()
	a.now = func() time.Time { return clock }
	token, err := a.Login(ctx, "me", "correct horse", "")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(SessionIdle - time.Hour)
	if _, err := a.Session(ctx, token); err != nil {
		t.Fatalf("session expired early: %v", err)
	}
	// Use refreshed it, so it lasts another SessionIdle from here.
	clock = clock.Add(SessionIdle - time.Hour)
	if _, err := a.Session(ctx, token); err != nil {
		t.Fatalf("refreshed session expired: %v", err)
	}
	clock = clock.Add(SessionIdle + time.Hour)
	if _, err := a.Session(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("idle session still valid: %v", err)
	}
}

func TestSetPasswordEndsSessions(t *testing.T) {
	a := newTest(t)
	ctx := context.Background()
	a.Setup(ctx, "me", "correct horse")
	token, _ := a.Login(ctx, "me", "correct horse", "")
	if err := a.SetPassword(ctx, "me", "battery staple"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Session(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatal("old session survived a password change")
	}
	if _, err := a.Login(ctx, "me", "battery staple", ""); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimit(t *testing.T) {
	a := newTest(t)
	ctx := context.Background()
	a.Setup(ctx, "me", "correct horse")
	clock := time.Now()
	a.now = func() time.Time { return clock }
	for i := 0; i < maxFailures; i++ {
		a.Login(ctx, "me", "nope nope", "")
	}
	if _, err := a.Login(ctx, "me", "correct horse", ""); !errors.Is(err, ErrTooMany) {
		t.Fatalf("got %v, want ErrTooMany", err)
	}
	clock = clock.Add(failureWindow + time.Second)
	if _, err := a.Login(ctx, "me", "correct horse", ""); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := hashPassword("pässwörd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("unexpected format %q", h)
	}
	if !checkPassword(h, "pässwörd") || checkPassword(h, "passwörd") || checkPassword("garbage", "x") {
		t.Fatal("checkPassword wrong")
	}
	h2, _ := hashPassword("pässwörd")
	if h == h2 {
		t.Fatal("same salt twice")
	}
}
