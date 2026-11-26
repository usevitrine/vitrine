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
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/usevitrine/vitrine/internal/db"
)

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 8291, Appendix A.
func TestEncryptRFCVector(t *testing.T) {
	plaintext := unb64(t, "V2hlbiBJIGdyb3cgdXAsIEkgd2FudCB0byBiZSBhIHdhdGVybWVsb24")
	as, err := ecdh.P256().NewPrivateKey(unb64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(as.PublicKey().Bytes(), unb64(t, "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8")) {
		t.Fatal("server public key")
	}
	ua, err := ecdh.P256().NewPublicKey(unb64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt(plaintext, ua, unb64(t, "BTBZMqHH6r4Tts7J_aSIgg"), as, unb64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	want := unb64(t, `DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml
		mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT
		pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN`)
	if !bytes.Equal(got, want) {
		t.Fatalf("message:\n got %s\nwant %s", b64.EncodeToString(got), b64.EncodeToString(want))
	}
}

// browser plays a browser's side: its keys, and decrypting a message.
type browser struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) *browser {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return &browser{k, auth}
}

func (b *browser) sub(endpoint string) Subscription {
	var s Subscription
	s.Endpoint = endpoint
	s.Keys.P256dh = b64.EncodeToString(b.key.PublicKey().Bytes())
	s.Keys.Auth = b64.EncodeToString(b.auth)
	return s
}

func (b *browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != 4096 || idlen != 65 {
		t.Fatalf("header: rs %d idlen %d", rs, idlen)
	}
	as, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := b.key.ECDH(as)
	prkKey, _ := hkdf.Extract(sha256.New, shared, b.auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(b.key.PublicKey().Bytes())+string(as.Bytes()), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatal("no last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func TestSend(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "meta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := Open(ctx, d, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	// The key is kept: opening again gives the same one.
	if s2, _ := Open(ctx, d, slog.New(slog.DiscardHandler)); s2.PublicKey() != s.PublicKey() {
		t.Fatal("the VAPID key changed")
	}

	phone, laptop := newBrowser(t), newBrowser(t)
	var mu sync.Mutex
	got := map[string][]byte{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusGone)
			return
		}
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("TTL") == "" {
			http.Error(w, "headers", 400)
			return
		}
		// VAPID: the token is signed by the key it names, for this origin.
		auth := r.Header.Get("Authorization")
		var tok, k string
		for _, part := range strings.Split(strings.TrimPrefix(auth, "vapid "), ",") {
			part = strings.TrimSpace(part)
			if v, ok := strings.CutPrefix(part, "t="); ok {
				tok = v
			} else if v, ok := strings.CutPrefix(part, "k="); ok {
				k = v
			}
		}
		pk, err := ecdsa.ParseUncompressedPublicKey(elliptic256(), mustB64(k))
		parts := strings.Split(tok, ".")
		if err != nil || len(parts) != 3 {
			http.Error(w, "vapid", http.StatusUnauthorized)
			return
		}
		h := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		sig := mustB64(parts[2])
		if len(sig) != 64 || !ecdsa.Verify(pk, h[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			http.Error(w, "signature", http.StatusUnauthorized)
			return
		}
		var claims struct {
			Aud string `json:"aud"`
			Sub string `json:"sub"`
		}
		json.Unmarshal(mustB64(parts[1]), &claims)
		if claims.Aud != "https://"+r.Host || claims.Sub != Subject {
			http.Error(w, "claims", http.StatusForbidden)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] = body
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	s.http = srv.Client()

	if err := s.Subscribe(ctx, phone.sub(srv.URL+"/phone"), "Safari on iPhone", "en-GB"); err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(ctx, laptop.sub(srv.URL+"/laptop"), "Brave on Windows", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(ctx, newBrowser(t).sub(srv.URL+"/gone"), "Old phone", "xx-XX"); err != nil {
		t.Fatal(err)
	}
	bad := phone.sub("http://insecure.test/x")
	if err := s.Subscribe(ctx, bad, "x", ""); err == nil {
		t.Fatal("an http endpoint was accepted")
	}

	m := Message{Title: "5 new files", Body: "safebooru.org · blue_sky", URL: "/"}
	n, err := s.Send(ctx, m)
	if err != nil || n != 2 {
		t.Fatalf("sent %d: %v", n, err)
	}
	for path, b := range map[string]*browser{"/phone": phone, "/laptop": laptop} {
		var back Message
		if err := json.Unmarshal(b.decrypt(t, got[path]), &back); err != nil || back != m {
			t.Fatalf("%s got %+v (%v)", path, back, err)
		}
	}
	// The gone one was removed; the others remain.
	devs, _ := s.Devices(ctx)
	if len(devs) != 2 || devs[0].Label != "Safari on iPhone" || devs[0].LastOKAt == 0 {
		t.Fatalf("devices: %+v", devs)
	}
	// Each in its own language: the phone said en-GB; the laptop none (the default, en-US).
	if langs, err := s.languages(ctx); err != nil || len(langs) != 2 {
		t.Fatalf("languages %v: %v", langs, err)
	}
	clear(got)
	if n, err := s.sendLang(ctx, "en-GB", m); err != nil || n != 1 || got["/phone"] == nil {
		t.Fatalf("en-GB: sent %d (%v)", n, err)
	}
	if n, err := s.sendLang(ctx, "en-US", m); err != nil || n != 1 || got["/laptop"] == nil {
		t.Fatalf("en-US: sent %d (%v)", n, err)
	}
	if l := s.Language(ctx, srv.URL+"/phone"); l != "en-GB" {
		t.Fatalf("the phone's language: %q", l)
	}
	if err := s.SendTo(ctx, srv.URL+"/laptop", Message{Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Unsubscribe(ctx, srv.URL+"/laptop"); err != nil {
		t.Fatal(err)
	}
	if err := s.SendTo(ctx, srv.URL+"/laptop", Message{Title: "Test"}); err == nil {
		t.Fatal("sent to a removed device")
	}
}

func elliptic256() elliptic.Curve { return elliptic.P256() }

func mustB64(s string) []byte {
	b, _ := b64.DecodeString(s)
	return b
}
