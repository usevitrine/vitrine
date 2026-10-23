package library

import (
	"context"
	"testing"
)

func TestStats(t *testing.T) {
	l := newTest(t)
	ctx := context.Background()
	ids := importN(t, l, 4)
	if err := l.AddTags(ctx, ids[:2], tags("blue_sky", "landscape")); err != nil {
		t.Fatal(err)
	}
	if err := l.AddParent(ctx, tags("blue_sky")[0], tags("favorite_artists")[0]); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, ids[3:], ""); err != nil {
		t.Fatal(err)
	}
	st, err := l.Stats(ctx, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 3 || len(st.Kinds) != 1 || st.Kinds[0].Kind != "image" || st.Kinds[0].Files != 3 {
		t.Fatalf("files: %+v", st)
	}
	if st.Bytes != 3*5008 || st.Trash.Files != 1 || st.Trash.Bytes != 5008 {
		t.Fatalf("sizes: %d, trash %+v", st.Bytes, st.Trash)
	}
	if st.UserLinks != st.TagLinks || st.TagLinks < 4 || st.Parents != 1 || st.Untagged != 1 {
		t.Fatalf("tags: links %d (user %d), parents %d, untagged %d", st.TagLinks, st.UserLinks, st.Parents, st.Untagged)
	}
	if len(st.Imports) != 7 || st.Imports[6] != 3 || st.Since == 0 {
		t.Fatalf("imports: %v since %d", st.Imports, st.Since)
	}
}
