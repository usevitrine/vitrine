package download

import (
	"context"
	"testing"
	"time"
)

func TestFoundBy(t *testing.T) {
	e := newEnv(t, 10)
	ctx := context.Background()
	e.clock()
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	e.dl.Add(ctx, "fake", " tag_7 ", 0)
	e.dl.RunQueue(ctx, "fake")
	sub, _ := e.dl.Subscribe(ctx, "fake", "tag_7", time.Hour, 0)

	ids := e.lib.Index().Search(mustParse(t, "tag_7"), e.dl.now()).IDs
	got, err := e.lib.FileSources(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Query != "common" || !got[0].Downloaded || got[0].Site != "fake" || got[0].SubscriptionID != 0 ||
		got[1].Query != "tag_7" || got[1].Downloaded || got[1].SubscriptionID != sub {
		t.Fatalf("sources: %+v", got)
	}
	// Running the same search again adds nothing.
	e.dl.Add(ctx, "fake", "common", 0)
	e.dl.RunQueue(ctx, "fake")
	if got, _ = e.lib.FileSources(ctx, ids[0]); len(got) != 2 {
		t.Fatalf("after a rerun: %+v", got)
	}
}
