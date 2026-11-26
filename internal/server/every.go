package server

import (
	"context"
	"time"
)

// Every runs fn first after the start delay, then every interval, until ctx
// ends - the shape of the background jobs that look now and then whether
// something is due (a backup, an update check, the health check). fn decides
// itself whether there's anything to do; a run that takes long pushes the
// next one back (the interval counts from its end).
func Every(ctx context.Context, first, interval time.Duration, fn func()) {
	wait := first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = interval
		fn()
	}
}
