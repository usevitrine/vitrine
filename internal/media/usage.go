package media

import (
	"bytes"
	"context"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/usevitrine/vitrine/internal/proc"
)

// What the tools cost, for the load summary in the log (2026-10-30, the user: "expand logging to make diagnosing this
// stuff easier"): every ffmpeg, ffprobe and vipsthumbnail run adds its processor time (user + system, from the OS)
// and wall time to its job's totals.

// JobUse is one kind of job's totals.
type JobUse struct {
	Runs, OnGPU int
	CPU, Wall   time.Duration
}

type usage struct {
	mu   sync.Mutex
	jobs map[string]*JobUse
}

// Usage holds the totals since the last TakeUsage.
var Usage = &usage{jobs: map[string]*JobUse{}}

func (u *usage) add(job string, wall, cpu time.Duration, gpu bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	j := u.jobs[job]
	if j == nil {
		j = &JobUse{}
		u.jobs[job] = j
	}
	j.Runs++
	j.CPU += cpu
	j.Wall += wall
	if gpu {
		j.OnGPU++
	}
}

// Take returns the totals by job (thumbnail, preview frames, probe) and starts again.
func (u *usage) Take() map[string]JobUse {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make(map[string]JobUse, len(u.jobs))
	for k, v := range u.jobs {
		out[k] = *v
	}
	u.jobs = map[string]*JobUse{}
	return out
}

// Jobs lists totals' job names in order (for a stable log line).
func Jobs(m map[string]JobUse) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Run is how one tool run went.
type Run struct {
	Wall, CPU time.Duration
	GPU       bool
}

// run runs a tool for a job, counting what it took; stdout comes back, stderr's text goes into the error.
func run(ctx context.Context, job string, gpu bool, name string, args ...string) ([]byte, string, Run, error) {
	cmd := proc.Hide(exec.CommandContext(ctx, name, args...))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	r := Run{Wall: time.Since(start), GPU: gpu}
	if ps := cmd.ProcessState; ps != nil {
		r.CPU = ps.UserTime() + ps.SystemTime()
	}
	Usage.add(job, r.Wall, r.CPU, gpu)
	return stdout.Bytes(), stderr.String(), r, err
}
