package server

import (
	"context"
	"time"
)

// Screen-sized copies of big images are made ahead of time (newest files
// first, one at a time), so even the first swipe through new downloads gets
// a ready copy instead of waiting for one to be made. Only the sizes viewers
// have used to fill their screens in the last 30 days.

// RunRenditionsAhead checks for missing copies a few minutes after the start
// and then every 10 minutes.
func (s *Server) RunRenditionsAhead(ctx context.Context) {
	if s.rend == nil {
		return
	}
	wait := 2 * time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 10 * time.Minute
		if n := s.makeRenditionsAhead(ctx); n > 0 {
			s.log.Info("screen-sized copies made ahead", "made", n)
		}
	}
}

func (s *Server) makeRenditionsAhead(ctx context.Context) int {
	sizes := s.rend.ScreenSizes(30 * 24 * time.Hour)
	if len(sizes) == 0 {
		return 0
	}
	ids, err := s.lib.LargeStills(ctx, sizes[0])
	if err != nil {
		s.log.Warn("copies ahead: listing images", "err", err)
		return 0
	}
	made := 0
	for start := 0; start < len(ids); start += 200 {
		rows, err := s.lib.Files(ctx, ids[start:min(start+200, len(ids))])
		if err != nil {
			return made
		}
		for _, f := range rows {
			for _, size := range sizes {
				if max(f.Width, f.Height) <= size || s.rend.Has(f.SHA256.String(), size) {
					continue
				}
				if !s.rend.RoomAhead() || ctx.Err() != nil {
					return made
				}
				_, err := s.rend.Get(ctx, f.SHA256.String(), size, func(ctx context.Context) ([]byte, error) {
					return s.lib.Tools().Rendition(ctx, f.Path, f.Width, f.Height, size)
				})
				if err == nil {
					made++
				}
			}
		}
	}
	return made
}
