package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/usevitrine/vitrine/internal/config"
	"github.com/usevitrine/vitrine/internal/db"
	"github.com/usevitrine/vitrine/internal/library"
	"github.com/usevitrine/vitrine/internal/media"
	"github.com/usevitrine/vitrine/internal/store"
	"github.com/usevitrine/vitrine/internal/thumbs"
)

// openLibrary opens the store, meta.db, thumbs.db and the index described
// by c. The returned function closes everything.
func openLibrary(ctx context.Context, c config.Config) (*library.Library, func(), error) {
	lib, _, closeAll, err := openApp(ctx, c)
	return lib, closeAll, err
}

// openApp is openLibrary that also returns meta.db (for the login).
func openApp(ctx context.Context, c config.Config) (*library.Library, *db.DB, func(), error) {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return nil, nil, nil, err
	}
	s, err := store.Open(c.FilesDir)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := s.CleanTemp(); err != nil {
		return nil, nil, nil, err
	}
	d, err := db.Open(ctx, filepath.Join(c.DataDir, "meta.db"))
	if err != nil {
		return nil, nil, nil, err
	}
	th, err := thumbs.Open(ctx, filepath.Join(c.DataDir, "thumbs.db"))
	if err != nil {
		d.Close()
		return nil, nil, nil, err
	}
	closeAll := func() { errors.Join(th.Close(), d.Close()) }
	tools := media.FindTools(c.FFmpeg, c.FFprobe, c.VipsThumbnail)
	tools.UseHW(c.HWAccel) // (the GPU is looked for when a video first needs it - serve: at start-up)
	lib, err := library.Open(ctx, library.Deps{DB: d, Store: s, Thumbs: th, Tools: tools})
	if err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	return lib, d, closeAll, nil
}
