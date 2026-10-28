package plugin

import (
	"sync"
	"sync/atomic"
	"time"
)

// Self-tuning connections (2026-10-28): a plugin whose limits.max_connections is above limits.connections lets
// Vitrine find how many files at once give the most from where it runs - through a VPN route each connection can be
// slow while more of them add up; a fast line gains nothing from more. It starts at connections; each minute the site
// was busy the whole time (every allowed connection downloading) it measures the total speed; with two such minutes
// at a number, it tries one more and keeps it only if the total rose by at least a tenth - else it goes back and
// leaves it for half an hour. A 429 goes back to connections at once (and the same wait).
//
// The client's slot channel has room for max_connections; the tuner keeps the slots not in use filled with its own
// tokens - taking one out allows one more download, putting one in (once a download ends) one fewer - so the code
// that waits for a slot doesn't change.
type tuner struct {
	c         *Client
	base, max int

	mu        sync.Mutex
	target    int
	rates     map[int]float64 // total bytes a second seen at each number (smoothed)
	seen      map[int]int     // full minutes measured at each number since it was last set
	probing   bool            // target is one more than the last good number, on trial
	holdUntil time.Time       // no trying more before this

	read    atomic.Int64 // file bytes read, all downloads
	active  atomic.Int32 // files downloading now
	running atomic.Bool

	now          func() time.Time
	tick, window time.Duration
}

const (
	tuneGain = 1.10             // one more connection must bring this much more in total
	tuneHold = 30 * time.Minute // after a try that didn't pay, or a 429
)

func newTuner(c *Client, base, max int) *tuner {
	t := &tuner{c: c, base: base, max: max, target: base, rates: map[int]float64{}, seen: map[int]int{},
		now: time.Now, tick: 5 * time.Second, window: time.Minute}
	for range max - base {
		c.files <- struct{}{} // (the slots above base: held to begin with)
	}
	return t
}

// Target is how many files may download at once now.
func (t *tuner) Target() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.target
}

// set changes the number (under mu): slots handed back or held again.
func (t *tuner) set(n int) {
	n = min(max(n, t.base), t.max)
	for ; t.target < n; t.target++ {
		<-t.c.files // (one of the tuner's own tokens)
	}
	for ; t.target > n; t.target-- {
		go func() { t.c.files <- struct{}{} }() // (taken back once a download ends)
	}
	t.seen[n] = 0
	if t.c.log != nil {
		t.c.log.Info("connections", "site", t.c.m.Site, "files_at_once", n)
	}
}

// started: a file download began (the tuner runs while there are downloads).
func (t *tuner) started() {
	t.active.Add(1)
	if t.running.CompareAndSwap(false, true) {
		go t.run()
	}
}

func (t *tuner) ended() { t.active.Add(-1) }

// tooMany: the site said 429 - back to the plugin's number for a while.
func (t *tuner) tooMany() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.probing = false
	t.holdUntil = t.now().Add(tuneHold)
	if t.target > t.base {
		t.set(t.base)
	}
}

// run measures minute by minute while files download (it ends after 10 idle minutes; the next download starts it).
func (t *tuner) run() {
	defer t.running.Store(false)
	tk := time.NewTicker(t.tick)
	defer tk.Stop()
	samples, full, last, idle := 0, 0, t.read.Load(), t.now()
	for range tk.C {
		a := int(t.active.Load())
		if a == 0 {
			if t.now().Sub(idle) > 10*time.Minute {
				return
			}
		} else {
			idle = t.now()
		}
		samples++
		if a >= t.Target() {
			full++
		}
		if time.Duration(samples)*t.tick < t.window {
			continue
		}
		n := t.read.Load()
		if full == samples {
			t.judge(float64(n-last) / t.window.Seconds())
		}
		samples, full, last = 0, 0, n
	}
}

// judge takes a full minute's total speed at the current number and decides whether to try more or go back.
func (t *tuner) judge(rate float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cur := t.target
	if old, ok := t.rates[cur]; ok && t.seen[cur] > 0 {
		rate = (old + rate) / 2
	}
	t.rates[cur] = rate
	t.seen[cur]++
	if t.seen[cur] < 2 {
		return
	}
	if t.probing {
		t.probing = false
		if rate < t.rates[cur-1]*tuneGain {
			t.holdUntil = t.now().Add(tuneHold)
			t.set(cur - 1)
		}
		return
	}
	if cur < t.max && !t.now().Before(t.holdUntil) {
		t.probing = true
		t.set(cur + 1)
	}
}
