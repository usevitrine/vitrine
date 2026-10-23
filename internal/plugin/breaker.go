package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// A site that keeps failing to answer (it's down, a network problem on the way, a Cloudflare block that can't be
// passed) is paused by itself: after PauseAfter connection failures in a row, its requests fail at once for a while
// (searches and checks wait instead of failing, files aren't recorded as failed - see the downloader), then one
// request tries again. It answering ends the pause; failing again pauses it longer (pauseSteps). Any answer from
// the site counts as it being there - a 404 too; only no answer, a 5xx or a Cloudflare block count against it.

// PauseAfter is how many connection failures in a row pause a site, unless the user set another number
// (Settings > Sites: SetPauseAfter; 0 = never).
const PauseAfter = 5

// pauseSteps are how long a site stays paused: the first time, then after each failed try.
var pauseSteps = []time.Duration{2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 30 * time.Minute, time.Hour}

// ErrPaused means the site is paused (it kept failing to answer): nothing was sent.
var ErrPaused = errors.New("paused: the site isn't answering")

// PausedError says why and until when.
type PausedError struct {
	Site   string
	Until  time.Time
	Reason string
}

func (e *PausedError) Error() string {
	return fmt.Sprintf("%s: paused until %s - it isn't answering (%s)", e.Site, e.Until.Format("15:04"), e.Reason)
}

func (e *PausedError) Unwrap() error { return ErrPaused }

// Pause is a site's pause, as shown (the zero value: not paused).
type Pause struct {
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`  // the next try
	Reason string    `json:"reason"` // the last failure
	Tries  int       `json:"tries"`  // tries that failed since it paused
}

// breaker counts a site's connection failures and pauses it (safe for concurrent use).
type breaker struct {
	mu      sync.Mutex
	now     func() time.Time
	after   int    // failures in a row that pause it (0: never)
	fails   int    // in a row
	last    string // the last failure
	pause   Pause  // Until zero: not paused
	probing bool   // the one request trying again is out
	// onChange hears when the site pauses (again) or answers again (paused false).
	onChange func(paused bool, p Pause)
}

// allow says whether a request may go out now: not paused, or the pause is over and no other request is trying.
func (b *breaker) allow(site string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pause.Until.IsZero() {
		return nil
	}
	if !b.probing && !b.now().Before(b.pause.Until) {
		b.probing = true // this one tries; the others wait for how it goes
		return nil
	}
	return &PausedError{Site: site, Until: b.pause.Until, Reason: b.pause.Reason}
}

// done records how a request went: err is a connection failure (no answer, 5xx, a Cloudflare block) or nil (the
// site answered). Requests that were canceled don't count either way.
func (b *breaker) done(err error) {
	b.mu.Lock()
	var notify func()
	if err == nil {
		was := !b.pause.Until.IsZero()
		b.fails, b.probing = 0, false
		if was {
			p := b.pause
			b.pause = Pause{}
			if f := b.onChange; f != nil {
				notify = func() { f(false, p) }
			}
		}
	} else {
		b.fails++
		b.last = err.Error()
		paused := !b.pause.Until.IsZero()
		switch {
		case paused && b.probing: // the try failed: longer
			b.probing = false
			b.pause.Tries++
			b.pause.Until = b.now().Add(pauseSteps[min(b.pause.Tries, len(pauseSteps)-1)])
			b.pause.Reason = b.last
		case !paused && b.after > 0 && b.fails >= b.after:
			b.pause = Pause{Since: b.now(), Until: b.now().Add(pauseSteps[0]), Reason: b.last}
		default:
			b.mu.Unlock()
			return
		}
		if f := b.onChange; f != nil {
			p := b.pause
			notify = func() { f(true, p) }
		}
	}
	b.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// release gives back a try that ended without saying anything (the request was canceled).
func (b *breaker) release() {
	b.mu.Lock()
	b.probing = false
	b.mu.Unlock()
}

// connectionFailure says whether a request's outcome counts against the site (nil: it answered).
func connectionFailure(ctx context.Context, resp *http.Response, err error) (failure error, counts bool) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return nil, false // (stopped here, not by the site)
	}
	if err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Code < 500 {
			return nil, true
		}
		return err, true
	}
	if resp != nil && resp.StatusCode >= 500 {
		return fmt.Errorf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)), true
	}
	return nil, true
}

// Paused is the site's pause now (the zero value: not paused).
func (c *Client) Paused() Pause {
	c.brk.mu.Lock()
	defer c.brk.mu.Unlock()
	return c.brk.pause
}

// RestorePause takes back a pause kept from before a restart (its next try as it was; a failed try pauses it longer).
// The site isn't told (it heard when it paused).
func (c *Client) RestorePause(p Pause) {
	if p.Until.IsZero() {
		return
	}
	c.brk.mu.Lock()
	c.brk.pause, c.brk.probing = p, false
	c.brk.fails = max(c.brk.fails, c.brk.after)
	c.brk.last = p.Reason
	c.brk.mu.Unlock()
}

// Waiting says whether the site is paused and its next try isn't due yet.
func (c *Client) Waiting() bool {
	c.brk.mu.Lock()
	defer c.brk.mu.Unlock()
	return !c.brk.pause.Until.IsZero() && (c.brk.probing || c.brk.now().Before(c.brk.pause.Until))
}

// SetClock sets the clock pauses are timed by (the downloader's: tests move it).
func (c *Client) SetClock(now func() time.Time) {
	c.brk.mu.Lock()
	c.brk.now = now
	c.brk.mu.Unlock()
}

// SetPauseAfter sets how many connection failures in a row pause the site (0: never - a pause now ends).
func (c *Client) SetPauseAfter(n int) {
	c.brk.mu.Lock()
	c.brk.after = max(0, n)
	var notify func()
	if n <= 0 && !c.brk.pause.Until.IsZero() {
		p := c.brk.pause
		c.brk.pause, c.brk.probing = Pause{}, false
		if f := c.brk.onChange; f != nil {
			notify = func() { f(false, p) }
		}
	}
	c.brk.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// PauseAfter is how many connection failures in a row pause the site (0: never).
func (c *Client) PauseAfter() int {
	c.brk.mu.Lock()
	defer c.brk.mu.Unlock()
	return c.brk.after
}

// Failing is how many connection failures in a row the site has had (0: its last request was answered).
func (c *Client) Failing() int {
	c.brk.mu.Lock()
	defer c.brk.mu.Unlock()
	return c.brk.fails
}

// TryNow ends the wait of a paused site: its next request tries again.
func (c *Client) TryNow() {
	c.brk.mu.Lock()
	if !c.brk.pause.Until.IsZero() && !c.brk.probing {
		c.brk.pause.Until = c.brk.now()
	}
	c.brk.mu.Unlock()
}

// OnPause sets who hears when the site pauses (again) or answers again.
func (c *Client) OnPause(f func(paused bool, p Pause)) {
	c.brk.mu.Lock()
	c.brk.onChange = f
	c.brk.mu.Unlock()
}
