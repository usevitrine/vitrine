package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The account in Settings: the name and the password change with the current password only; a new password signs the
// other devices out (not this one); passkey-only sign-in refuses every password - right or wrong, the same answer -
// needs a passkey to turn on, and goes off with the last passkey.
func TestAccountChanges(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	// A second device, signed in too.
	jar, _ := cookiejar.New(nil)
	other := &testEnv{t: t, srv: e.srv, c: &http.Client{Jar: jar}}
	if code := other.call("POST", "/api/login", map[string]string{"name": "me", "password": "correct horse"}, nil); code != 200 {
		t.Fatalf("second device: %d", code)
	}

	// The password: the current one first.
	if code := e.call("POST", "/api/account/password", map[string]string{"current": "nope nope", "password": "battery staple"}, nil); code != 403 {
		t.Fatalf("wrong current password: %d", code)
	}
	if code := e.call("POST", "/api/account/password", map[string]string{"current": "correct horse", "password": "short"}, nil); code != 400 {
		t.Fatalf("too short: %d", code)
	}
	if code := e.call("POST", "/api/account/password", map[string]string{"current": "correct horse", "password": "battery staple"}, nil); code != 200 {
		t.Fatalf("change password: %d", code)
	}
	if code := e.call("GET", "/api/stats", nil, nil); code != 200 {
		t.Fatalf("this device after the change: %d", code)
	}
	if code := other.call("GET", "/api/stats", nil, nil); code != 401 {
		t.Fatalf("the other device after the change: %d", code)
	}
	if code := other.call("POST", "/api/login", map[string]string{"name": "me", "password": "correct horse"}, nil); code != 401 {
		t.Fatalf("the old password: %d", code)
	}
	if code := other.call("POST", "/api/login", map[string]string{"name": "me", "password": "battery staple"}, nil); code != 200 {
		t.Fatalf("the new password: %d", code)
	}

	// The name.
	var got struct {
		User string `json:"user"`
	}
	if code := e.call("POST", "/api/account/name", map[string]string{"current": "battery staple", "name": " spaced "}, nil); code != 400 {
		t.Fatalf("a name with spaces round it: %d", code)
	}
	if code := e.call("POST", "/api/account/name", map[string]string{"current": "battery staple", "name": "someone"}, &got); code != 200 || got.User != "someone" {
		t.Fatalf("rename: %d %+v", code, got)
	}
	var sess map[string]any
	e.call("GET", "/api/session", nil, &sess)
	if sess["user"] != "someone" {
		t.Fatalf("session after renaming: %v", sess)
	}

	// Other devices out, on request.
	var ended struct {
		Ended int `json:"ended"`
	}
	if code := e.call("POST", "/api/account/logout-others", struct{}{}, &ended); code != 200 || ended.Ended != 1 {
		t.Fatalf("logout others: %d %+v", code, ended)
	}
	if code := other.call("GET", "/api/stats", nil, nil); code != 401 {
		t.Fatalf("the other device: %d", code)
	}

	// Passkey only: not without a passkey.
	if code := e.call("POST", "/api/passkeys/only", map[string]bool{"on": true}, nil); code != 400 {
		t.Fatalf("passkey only without a passkey: %d", code)
	}
	// (a passkey as stored - making a real one needs an authenticator: the e2e tests use the browser's virtual one)
	if _, err := e.db.W.ExecContext(context.Background(),
		"INSERT INTO passkeys (user_id, credential_id, credential, origin, name, created_at) VALUES ((SELECT id FROM users), x'01', '{}', 'https://example.test', 'Phone', 1)"); err != nil {
		t.Fatal(err)
	}
	var keys struct {
		Passkeys []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"passkeys"`
		Only bool `json:"passkey_only"`
	}
	if code := e.call("POST", "/api/passkeys/only", map[string]bool{"on": true}, &keys); code != 200 || !keys.Only || len(keys.Passkeys) != 1 {
		t.Fatalf("passkey only: %d %+v", code, keys)
	}
	e.call("GET", "/api/session", nil, &sess)
	if sess["passkey_only"] != true {
		t.Fatalf("session says passkey only: %v", sess)
	}
	// Every password refused, the right one as the wrong one (nothing to learn from the answer).
	for _, pw := range []string{"battery staple", "wrong wrong"} {
		if code := other.call("POST", "/api/login", map[string]string{"name": "someone", "password": pw}, nil); code != 403 {
			t.Fatalf("password %q with passkey only: %d", pw, code)
		}
	}
	// The last passkey gone: passwords again.
	if code := e.call("POST", "/api/passkeys/"+strconv.FormatInt(keys.Passkeys[0].ID, 10)+"/remove", struct{}{}, &keys); code != 200 || keys.Only || len(keys.Passkeys) != 0 {
		t.Fatalf("remove the last passkey: %d %+v", code, keys)
	}
	if code := other.call("POST", "/api/login", map[string]string{"name": "someone", "password": "battery staple"}, nil); code != 200 {
		t.Fatalf("password after the last passkey went: %d", code)
	}
	// Signing in with a passkey: a step started elsewhere, or none, is refused.
	if code := other.call("POST", "/api/passkeys/login/finish", map[string]string{"id": "x"}, nil); code != 400 && code != 401 {
		t.Fatalf("finish without a begun step: %d", code)
	}
	// Begun on a named host (passkeys never belong to a bare IP address): its options, for that host.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/passkeys/login/begin", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "vitrine.test"
	resp, err := other.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var opts struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&opts)
	if resp.StatusCode != 200 || opts.PublicKey.Challenge == "" || opts.PublicKey.RPID != "vitrine.test" {
		t.Fatalf("begin: %d %+v", resp.StatusCode, opts)
	}
}

// Many password tries at once: never more than two hashed together (64 MiB each), all answered.
func TestLoginsAtOnce(t *testing.T) {
	e := newEnv(t)
	e.loginAs("me", "correct horse")
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			jar, _ := cookiejar.New(nil)
			o := &testEnv{t: t, srv: e.srv, c: &http.Client{Jar: jar}}
			codes <- o.call("POST", "/api/login", map[string]string{"name": "me", "password": "wrong wrong"}, nil)
		}()
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 401 && c != 429 {
			t.Fatalf("a try at once: %d", c)
		}
	}
}
