package main

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/metrics"
	"strings"
	"time"

	"github.com/usevitrine/vitrine/internal/media"
)

// loadEvery is how often the load summary goes in the log (while there's load).
const loadEvery = 5 * time.Minute

// logLoad writes what the processor was busy with every few minutes (2026-10-30, the user: up to 80% of 8 cores
// while importing a big library - "expand logging to make diagnosing this stuff easier"): Vitrine's own share (Go's
// estimate: its code and its garbage collector), then each kind of tool job - runs, processor time, how many on the
// GPU. Quiet when there was next to nothing. For more: `vitrine profile` (what Vitrine's own time went on).
func logLoad(ctx context.Context, log *slog.Logger) {
	samples := []metrics.Sample{
		{Name: "/cpu/classes/user:cpu-seconds"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
		{Name: "/cpu/classes/scavenge/total:cpu-seconds"},
	}
	read := func() (user, gc float64) {
		metrics.Read(samples)
		f := func(s metrics.Sample) float64 {
			if s.Value.Kind() == metrics.KindFloat64 {
				return s.Value.Float64()
			}
			return 0
		}
		return f(samples[0]), f(samples[1]) + f(samples[2])
	}
	lastUser, lastGC := read()
	last := time.Now()
	media.Usage.Take() // (from now)
	tick := time.NewTicker(loadEvery)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
		now := time.Now()
		user, gc := read()
		secs := now.Sub(last).Seconds()
		own := (user - lastUser + gc - lastGC) / secs
		gcShare := 0.0
		if d := user - lastUser + gc - lastGC; d > 0 {
			gcShare = (gc - lastGC) / d
		}
		lastUser, lastGC, last = user, gc, now
		jobs := media.Usage.Take()
		var tools float64
		var parts []string
		for _, k := range media.Jobs(jobs) {
			j := jobs[k]
			tools += j.CPU.Seconds()
			p := fmt.Sprintf("%s: %d in %s cpu", k, j.Runs, j.CPU.Round(time.Second))
			if j.OnGPU > 0 {
				p += fmt.Sprintf(" (%d on the GPU)", j.OnGPU)
			}
			parts = append(parts, p)
		}
		if own < 0.05 && tools < 5 {
			continue
		}
		log.Info("load", "minutes", int(secs/60+0.5), "cores", runtime.NumCPU(),
			"vitrine_cores", fmt.Sprintf("%.2f", own), "vitrine_gc", fmt.Sprintf("%.0f%%", gcShare*100),
			"tools_cores", fmt.Sprintf("%.2f", tools/secs), "tools", strings.Join(parts, "; "))
	}
}
