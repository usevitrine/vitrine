package i18n

import "testing"

func TestT(t *testing.T) {
	en := For("en-US")
	if got := en.T("push.newFiles", "n", 1); got != "1 new file" {
		t.Fatalf("one: %q", got)
	}
	if got := en.T("push.newFiles", "n", 1234); got != "1,234 new files" {
		t.Fatalf("other: %q", got)
	}
	if got := en.T("push.on", "query", "blue_sky", "site", "safebooru.org"); got != "blue_sky on safebooru.org" {
		t.Fatalf("placeholders: %q", got)
	}
	if got := en.T("no.such.key"); got != "no.such.key" {
		t.Fatalf("missing: %q", got)
	}
	// A language without a text falls back to en-US; an unknown one is the default.
	if got := For("en-GB").T("push.testTitle"); got != "Notifications work" {
		t.Fatalf("fallback: %q", got)
	}
	if For("xx-XX").Tag() != Default() {
		t.Fatal("an unknown language isn't the default")
	}
	if err := SetDefault("xx-XX"); err == nil {
		t.Fatal("an unknown default was accepted")
	}
	if err := SetDefault("en-GB"); err != nil || For("").Tag() != "en-GB" {
		t.Fatalf("default: %v", err)
	}
	SetDefault("")
}
