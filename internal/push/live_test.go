package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/usevitrine/vitrine/internal/db"
)

// TestLiveServices checks that Apple, Google and Mozilla accept the VAPID
// token (run by hand: VITRINE_LIVE_PUSH=1). The endpoints are made up, so the
// right answer is "bad token / gone" with our key and "bad JWT" with another.
func TestLiveServices(t *testing.T) {
	if os.Getenv("VITRINE_LIVE_PUSH") == "" {
		t.Skip()
	}
	ctx := context.Background()
	d, _ := db.Open(ctx, filepath.Join(t.TempDir(), "meta.db"))
	defer d.Close()
	s, _ := Open(ctx, d, slog.New(slog.DiscardHandler))
	b := newBrowser(t)
	for _, ep := range []string{
		"https://web.push.apple.com/QGmadeUpTokenForTestingOnly0000000000000000000000000000000000000000",
		"https://fcm.googleapis.com/fcm/send/dmadeup:APA91bMadeUpTokenForTestingOnly000000000000000000000000",
		"https://updates.push.services.mozilla.com/wpush/v2/gAAAAABmadeUpTokenForTestingOnly0000000000000000",
	} {
		err := s.deliver(ctx, ep, b.key.PublicKey().Bytes(), b.auth, []byte(`{"title":"x"}`))
		t.Logf("good JWT -> %v", err)
		good := s.key
		s.key, _ = ecdsaKey()
		err = s.deliver(ctx, ep, b.key.PublicKey().Bytes(), b.auth, []byte(`{"title":"x"}`))
		s.key = good
		t.Logf("JWT signed by another key -> %v", err)
	}
}

func ecdsaKey() (*ecdsa.PrivateKey, error) { return ecdsa.GenerateKey(elliptic.P256(), rand.Reader) }
