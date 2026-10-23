package library

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestSettings(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	if s, err := l.Settings(ctx); err != nil || len(s) != 0 {
		t.Fatalf("empty: %v %v", s, err)
	}
	set := map[string]json.RawMessage{
		"defaultSearch": json.RawMessage(`"system:imported<5d"`),
		"defaultSort":   json.RawMessage(`"uploaded"`),
		"startMuted":    json.RawMessage(`false`),
		"speed":         json.RawMessage(`1.5`),
	}
	if err := l.SetSettings(ctx, set); err != nil {
		t.Fatal(err)
	}
	s, _ := l.Settings(ctx)
	if string(s["defaultSort"]) != `"uploaded"` || string(s["startMuted"]) != "false" || len(s) != 4 {
		t.Fatalf("settings: %v", s)
	}
	for _, bad := range []map[string]json.RawMessage{
		{"topSpacing": json.RawMessage(`20`)},       // per device: not shared
		{"speed": json.RawMessage(`9`)},             // out of range
		{"startMuted": json.RawMessage(`"yes"`)},    // not a bool
		{"defaultSearch": json.RawMessage(`"( x"`)}, // not a search
		{"loop": json.RawMessage(`true`), "speed": json.RawMessage(`"fast"`)},
	} {
		if err := l.SetSettings(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v accepted: %v", bad, err)
		}
	}
	if s, _ := l.Settings(ctx); s["loop"] != nil {
		t.Fatal("a rejected batch was partly stored")
	}
}

// Every value a device can hold is accepted - an empty welcome (never started) too:
// one refused value would refuse the whole save, every other setting with it.
func TestSettingsWelcome(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	for _, v := range []string{`""`, `"storage"`, `"done"`, `"finished"`} {
		if err := l.SetSettings(ctx, map[string]json.RawMessage{"welcome": json.RawMessage(v), "loop": json.RawMessage(`false`)}); err != nil {
			t.Errorf("welcome %s: %v", v, err)
		}
	}
	if err := l.SetSettings(ctx, map[string]json.RawMessage{"welcome": json.RawMessage(`"nope"`)}); !errors.Is(err, ErrInvalid) {
		t.Errorf("welcome nope accepted: %v", err)
	}
}
