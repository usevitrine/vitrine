package diskfree

import "testing"

func TestOf(t *testing.T) {
	u, err := Of(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if u.Total == 0 || u.Free > u.Total {
		t.Fatalf("odd usage: %+v", u)
	}
}
