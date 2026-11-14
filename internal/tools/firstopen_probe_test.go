package tools

// firstopen_probe_test.go — the measurement instrument for the first-open
// blocker ("which single syscall can park the ledger's schema batch for
// minutes when a bare fsync measures ~1s?").
//
// WHY a probe and not a hypothesis: the two candidate mechanisms look IDENTICAL
// from Go (both park inside tx.Commit() of execSchemaBatchOnce, both are
// attributed to memory.go by any goroutine-stack watchdog) but are opposites in
// the kernel:
//
//	A. a DURABLE sync — the committing thread sits in fsync(2)/fdatasync(2)
//	   (syscall 74/75) inside the filesystem's fsync, paying real device
//	   latency;
//	B. a DRIVER WAIT — no sync syscall at all: the thread spins/polls
//	   (nanosleep, futex, epoll_wait) while SQLite's busy handler retries a
//	   lock, i.e. the cost is contention, not durability.
//
// So the probe reads /proc/self/task/<tid>/syscall + .../wchan for EVERY thread
// while the batch is in flight and reports the histogram of kernel states. That
// distinguishes A from B directly, without guessing from wall time.
//
// It is gated behind an explicit output directory (GOPI_FIRSTOPEN_PROBE_DIR) so
// the ordinary suite never writes evidence files; the numbers it prints are the
// evidence the report quotes.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// firstOpenProbeDirEnv names the directory the probe writes its two
	// ledgers into and reads its report from. Unset ⇒ the probe skips (the
	// suite stays silent).
	firstOpenProbeDirEnv = "GOPI_FIRSTOPEN_PROBE_DIR"
	// firstOpenProbeBlockDirEnv optionally names a directory on a NON-memory
	// filesystem, so the same measurement runs on both sides of the
	// memory/block divide. Unset ⇒ the block arm is reported as not measured.
	firstOpenProbeBlockDirEnv = "GOPI_FIRSTOPEN_PROBE_BLOCK_DIR"
	// firstOpenProbeSampleInterval is how often the sampler reads the
	// kernel's own view of every thread.
	firstOpenProbeSampleInterval = 2 * time.Millisecond
	// firstOpenProbeRepeat is how many fresh ledgers each arm opens, so one
	// cold-start outlier cannot become the finding.
	firstOpenProbeRepeat = 3
)

// syscallNumber → the name the report uses. 74/75 are the ONLY two that mean
// "the commit is paying device latency"; everything else with a sync-shaped
// name is the driver's own bookkeeping (pwrite64 writes the WAL, it does not
// wait for it).
var firstOpenProbeSyscallNames = map[string]string{
	"74":  "fsync",
	"75":  "fdatasync",
	"35":  "nanosleep",
	"202": "futex",
	"7":   "poll",
	"232": "epoll_wait",
	"18":  "pwrite64",
	"0":   "read",
}

// firstOpenProbeSample is one thread observation.
type firstOpenProbeSample struct {
	syscall string
	wchan   string
	state   string
}

// TestZZFirstOpenProbe is the gated measurement instrument (see the file
// header). It writes a machine-readable report and prints it, so the report can
// quote numbers instead of adjectives.
//
// The first open is measured PHASE BY PHASE — the same sequence
// initKnowledgeBase runs — because "the first open costs 2s" is not an
// attribution: the question is which of the schema phases carries the durable
// sync. Samples are scoped to each phase, so a histogram is attributed to the
// phase it was recorded in.
func TestZZFirstOpenProbe(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv(firstOpenProbeDirEnv))
	if dir == "" {
		t.Skipf("probe disabled: set %s to a writable directory (tmpfs preferred)", firstOpenProbeDirEnv)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("probe dir %s: %v", dir, err)
	}
	arms := []struct {
		name     string
		dir      string
		syncMode string
	}{{name: "working-path", dir: dir, syncMode: ledgerSyncMode}}
	if bdir := strings.TrimSpace(os.Getenv(firstOpenProbeBlockDirEnv)); bdir != "" {
		arms = append(arms,
			struct {
				name     string
				dir      string
				syncMode string
			}{name: "block-device-durable", dir: bdir, syncMode: ledgerSyncMode},
			struct {
				name     string
				dir      string
				syncMode string
			}{name: "block-device-off", dir: bdir, syncMode: ledgerSyncModeWorkingFS},
		)
	}

	var report strings.Builder
	for _, arm := range arms {
		for i := 0; i < firstOpenProbeRepeat; i++ {
			path := filepath.Join(arm.dir, fmt.Sprintf("probe-%s-%d.db", arm.name, i))
			for _, suffix := range []string{"", "-wal", "-shm"} {
				_ = os.Remove(path + suffix) //stylegate:ignore a stale probe DB is being replaced; a failed remove is reported by the open below
			}
			fmt.Fprintf(&report, "arm=%s fs=%s sync=%s fresh=%d\n",
				arm.name, firstOpenProbeFSKind(arm.dir), arm.syncMode, i)
			if err := firstOpenProbePhases(context.Background(), path, arm.syncMode, &report, t); err != nil {
				fmt.Fprintf(&report, "    ERROR %v\n", err)
			}
		}
	}
	out := filepath.Join(dir, "firstopen-probe.txt")
	if err := os.WriteFile(out, []byte(report.String()), 0o644); err != nil {
		t.Fatalf("write probe report: %v", err)
	}
	t.Logf("probe report (%s):\n%s", out, report.String())
}

// firstOpenProbePhases runs the first open one phase at a time, sampling every
// thread's kernel state during each phase and reporting the phase's wall time
// together with the blocking histogram recorded inside it.
func firstOpenProbePhases(ctx context.Context, path, syncMode string, report *strings.Builder, t *testing.T) error {
	db, err := openDB(path, false)
	if err != nil {
		return err
	}
	phases := []struct {
		name string
		run  func() error
	}{
		{name: "openDB", run: func() error { return nil }},
		{name: "enableWAL", run: func() error { return enableWALWithSync(ctx, db, syncMode) }},
		{name: "ensureSchema", run: func() error { return ensureSchema(ctx, db) }},
		{name: "ensureFeedbackSchema", run: func() error { return ensureFeedbackSchema(ctx, db) }},
		{name: "ensureTasksSchema", run: func() error { return ensureTasksSchema(ctx, db) }},
	}
	for _, ph := range phases {
		hist, blocking, wall, err := firstOpenProbeMeasure(ph.run)
		fmt.Fprintf(report, "    phase=%-20s wall=%8.3fs err=%v db=%s wal=%s\n",
			ph.name, wall.Seconds(), err, firstOpenProbeSize(path), firstOpenProbeSize(path+"-wal"))
		for _, k := range firstOpenProbeSortedKeys(blocking) {
			fmt.Fprintf(report, "        blocked-in %-42s %d samples\n", k, blocking[k])
		}
		for _, k := range firstOpenProbeSortedKeys(hist) {
			if blocking[k] == 0 {
				fmt.Fprintf(report, "        observed   %-42s %d samples\n", k, hist[k])
			}
		}
		if err != nil {
			_ = db.Close() //stylegate:ignore the phase failure is already in the report; this close is best-effort cleanup
			return nil     // a phase failure is reported, not fatal: the point is the cost
		}
	}
	_, _, wall, _ := firstOpenProbeMeasure(func() error { return db.Close() }) //stylegate:ignore the close's error is not what this measurement is about; its wall time is
	fmt.Fprintf(report, "    phase=%-20s wall=%8.3fs\n", "close", wall.Seconds())
	return nil
}

// firstOpenProbeMeasure runs fn under a per-thread kernel-state sampler and
// returns the sample histogram, the subset that is genuinely blocked (state D
// or an fsync/fdatasync syscall) and the wall time.
func firstOpenProbeMeasure(fn func() error) (map[string]int, map[string]int, time.Duration, error) {
	samples := make(chan firstOpenProbeSample, 16384)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstOpenProbeSampleThreads(stop, samples)
	}()
	start := time.Now()
	err := fn()
	wall := time.Since(start)
	close(stop)
	wg.Wait()
	close(samples)

	hist := map[string]int{}
	blocking := map[string]int{}
	for s := range samples {
		name := firstOpenProbeSyscallNames[s.syscall]
		if name == "" {
			name = "syscall " + s.syscall
		}
		key := name + " wchan=" + s.wchan
		hist[key]++
		if s.state == "D" || s.syscall == "74" || s.syscall == "75" {
			blocking[key]++
		}
	}
	return hist, blocking, wall, err
}

// firstOpenProbeSize renders a file's size for the phase line (the WAL grows by
// one page per committed write transaction, so it is the phase's record of how
// much durable data the engine wrote).
func firstOpenProbeSize(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return strconv.Itoa(int(st.Size()))
}

// firstOpenProbeSampleThreads reads the kernel's own per-thread state until stop
// is closed. One goroutine per thread is impossible (the sampler cannot know
// which thread blocks), so it sweeps /proc/self/task each tick and emits one
// sample per thread — a blocked thread therefore appears in the same tick with
// the same state repeatedly, which is what the histogram counts.
func firstOpenProbeSampleThreads(stop <-chan struct{}, out chan<- firstOpenProbeSample) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		for _, s := range firstOpenProbeReadTaskStates() {
			select {
			case out <- s:
			default:
			}
		}
		//stylegate:allow-sleep the sampler's whole job is to poll the kernel's per-thread state at a fixed cadence; a longer interval would miss the block it exists to record
		time.Sleep(firstOpenProbeSampleInterval)
	}
}

// firstOpenProbeReadTaskStates reads the syscall, wchan and state of every
// thread of THIS process straight from procfs — the kernel's account, not Go's.
func firstOpenProbeReadTaskStates() []firstOpenProbeSample {
	tasks, err := filepath.Glob("/proc/self/task/*")
	if err != nil {
		return nil
	}
	var samples []firstOpenProbeSample
	for _, task := range tasks {
		raw, err := os.ReadFile(filepath.Join(task, "syscall"))
		if err != nil {
			continue // the thread finished between the glob and the read
		}
		fields := strings.Fields(string(raw))
		if len(fields) == 0 {
			continue
		}
		s := firstOpenProbeSample{syscall: fields[0]}
		if w, err := os.ReadFile(filepath.Join(task, "wchan")); err == nil {
			s.wchan = strings.TrimSpace(string(w))
		}
		if stat, err := os.ReadFile(filepath.Join(task, "stat")); err == nil {
			// state is the field after the (comm) parenthesised name
			if i := strings.LastIndex(string(stat), ")"); i >= 0 {
				rest := strings.Fields(string(stat)[i+1:])
				if len(rest) > 0 {
					s.state = rest[0]
				}
			}
		}
		samples = append(samples, s)
	}
	return samples
}

// firstOpenProbeFSKind names the filesystem backing dir for the report, using
// the shared classifier (internal/scratch.MemoryFilesystem) rather than a path
// spelling — the same measurement Rule 22's gates use.
func firstOpenProbeFSKind(dir string) string {
	mem, name, err := scratch.MemoryFilesystem(dir)
	if err != nil {
		return "unmeasurable (" + err.Error() + ")"
	}
	if mem {
		return name + " (memory-backed)"
	}
	return name + " (device-backed)"
}

// firstOpenProbeSortedKeys keeps the report deterministic.
func firstOpenProbeSortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] > m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}
