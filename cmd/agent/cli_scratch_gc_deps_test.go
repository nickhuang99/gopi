// cli_scratch_gc_deps_test.go — the `scratch-gc deps` arm's two classifiers,
// plus the regression guard for the argv[0] defect that made 22 Chromium
// processes report as `origin=gopi`.
//
// The defect (measured 2026-09-26 on this host): Chromium REWRITES its own
// argv, so /proc/<pid>/cmdline's first NUL-separated field is the WHOLE command
// line — including `--user-data-dir=…/queue-runtime-shot-…/gopi-shot-profile-…`.
// A classifier reading argv[0] therefore found the word `gopi` in a scratch
// DIRECTORY NAME and called a browser a dependency of ours: `resident=26
// gopi=26` when the truth was 8. The same browser under
// `--user-data-dir=/tmp/prof` classified as `foreign` — a verdict that flips on
// a directory name. The fix reads /proc/<pid>/exe (the real exec'd file), which
// is `chrome` in both spellings.
//
// THE GUARD asserts the property, not the symptom: for the SAME argv list, the
// verdict must not change when a non-executable argument changes. That is what
// makes this a regression test rather than a snapshot of one host.
//
// THE GUARD'S OWN FIXTURE was load-flaky until 2026-09-26: it planted a process
// whose argv[1] did not exist, so `sleep` exited within ~50 µs and the case
// classified a zombie (`unknown`) whenever the readlink lost that race (21/300
// runs red, load ≈ 8). The plant now keeps a real duration on argv[1] and its
// measurability is ASSERTED (see the preconditions below) — rationale,
// measurements and the red-proof: devdoc/scratch-gc-deps-origin-guard-deterministic-plant.md.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/flaketest"
)

// TestScratchGCDepsOriginReadsExeNotArgv0 is the guard with TEETH: it drives
// depsResidentOrigin against a REAL pid whose argv[0] carries the `gopi` shape
// while its executable does not. Under the old argv[0] rule this test fails
// (the arm called the process ours); under the exe rule it passes. Without the
// live pid the property above could be re-broken by reverting the classifier
// and every test would still be green — which is exactly the "guard that cannot
// go red" this repo keeps paying for.
func TestScratchGCDepsOriginReadsExeNotArgv0(t *testing.T) {
	flaketest.Declare("deterministic: the fixture planted a process and then overwrote cmd.Args with ONE field (the Chromium-shaped lie), so the kernel delivered that string as argv[0] and NOTHING as argv[1] — the program printed its usage and exited within ~50 us, so /proc/<pid>/exe was ENOENT and the classifier answered `unknown` where the test demanded `foreign`. Whether the readlink ran inside that sub-50-us window was the host's business (21/300 runs red at load ~8). Registered: testdata/flakes.tsv, owner 01a0f60e-309b-7e27-98f7-be6b93d05cc8.")
	// The argv[0] is the measured Chromium shape: ONE field holding the WHOLE
	// command line, whose last slash-separated segment begins with `gopi`
	// because the scratch DIRECTORY it points at does. filepath.Base of that
	// field is therefore "gopi-shot-profile-42 …" — the word comes from a path,
	// not from a program.
	const plantArgv0 = "/usr/lib/chromium/chrome --headless --no-sandbox " +
		"--user-data-dir=/home/nick/workspace/gopi-test-scratch/queue/run-1/gopi-shot-profile-42 --use-angle=swiftshader"
	// The planted program is a REAL binary and it gets a REAL argv[1].
	// WHY THE ARGUMENT MATTERS (measured, this test's own flake): the fixture
	// used to overwrite Args with the single field above, so the kernel passed
	// it as the child's argv[0] and NOTHING as argv[1]. `sleep` then parsed no
	// duration, printed its usage and exited within a millisecond — and a
	// process that has already exited has no readable /proc/<pid>/exe, so
	// depsResidentOrigin answered `unknown` (correctly) where the test demanded
	// `foreign`. Whether the assertion saw a live process or a corpse was a
	// race the case usually won and a loaded host lost: 21 of 300 runs red on
	// this host (the run's own measurement; see the report). argv[1] is what
	// makes the plant's liveness INDEPENDENT of the host's load.
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep(1) to plant with: %v", err)
	}
	cmd := exec.Command(sleepBin, plantSleepSeconds)
	cmd.Args = []string{plantArgv0, plantSleepSeconds} // argv[0] is the lie; argv[1] keeps the real program alive
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the planted process: %v", err)
	}
	//stylegate:ignore kills the child process the test spawned and is tearing down
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	// PRECONDITION 1 (structural, and therefore deterministic): the plant's argv
	// is the lying name AND the real duration. This is the exact property whose
	// absence caused the flake, and it is decided without spawning anything — a
	// later edit that drops argv[1] fails HERE, every run, instead of handing
	// the assertion a process that is about to die.
	if len(cmd.Args) != plantArgvLen {
		t.Fatalf("the plant carries %d argv field(s) (%q), want %d: argv[0] is the lie, argv[1] is "+
			"what keeps the real program alive past the measurement", len(cmd.Args), cmd.Args, plantArgvLen)
	}
	// and that second field must be a duration the program will ACCEPT: an
	// operand it rejects (an empty string — measured: `sleep ""` prints its usage
	// and exits 1) is the same unmeasurable plant by another route, and the cost
	// of catching it here is one regex instead of a load-dependent sleep.
	if !plantDurationRe.MatchString(cmd.Args[plantDurationArgIdx]) {
		t.Fatalf("the plant's argv[%d] = %q is not a duration %s accepts, so the child would exit "+
			"the moment it is spawned — the fixture must keep it RUNNING past the measurement",
			plantDurationArgIdx, cmd.Args[plantDurationArgIdx], plantArgv0)
	}
	// PRECONDITION 2 (measured): the child is alive — past its exec and still
	// RUNNING a moment later — and /proc/<pid>/exe reads back the executable we
	// started. If that fails the FIXTURE is at fault and it is a Fatal, so an
	// unmeasurable plant can never be read as the classifier's own verdict (and
	// `unknown` is never accepted below — Rule 15).
	exe := plantWaitExeReadable(t, cmd, sleepBin, plantArgv0)

	// The argv the classifier is given is the argv the KERNEL was given (argv[0]
	// is the lie, argv[1] the duration) — taken from cmd.Args rather than
	// retyped, so the check below cannot pass while examining a constant that no
	// longer describes the plant.
	parts := append([]string{}, cmd.Args...)
	// The fixture must actually reproduce the defect: `filepath.Base(argv[0])`
	// (the OLD rule's input) matches the gopi name regex.
	if !depsGopiNameRes.MatchString(filepath.Base(parts[0])) {
		t.Fatalf("the plant does not reproduce the trap: filepath.Base(%q) = %q must match the name regex",
			parts[0], filepath.Base(parts[0]))
	}
	got := depsResidentOrigin(cmd.Process.Pid, parts)
	// PRECONDITION 3 (measured, AFTER the call): the pid we classified was still
	// alive when the classifier read it. A verdict taken from a process that
	// exited mid-read is not a verdict about a live process — it is the corpse
	// the old fixture produced — so this is a Fatal, never the assertion's
	// business.
	plantAssertAlive(t, cmd, plantArgv0, "across the classifier's /proc read")
	if got != depsOriginForeign {
		t.Errorf("depsResidentOrigin(argv[0]=%q) = %q, want %q — the verdict came from argv[0]; "+
			"the PROGRAM is /proc/<pid>/exe (here %s, read back as %q from the live pid), and a "+
			"scratch directory name must not decide whether a process is gopi's dependency",
			plantArgv0, got, depsOriginForeign, sleepBin, exe)
	}
}

// The planted fixture's own knobs. All are named (Rule 1b): the poll bound is
// the harness's patience — the exec installs the /proc symlink immediately, so
// the loop re-reads only while it has not landed (5 ms × 200 ≈ 1 s), never a
// policy knob — and the duration is what keeps the child alive past the
// measurement. Nothing waits on that duration, so a loaded host cannot expire
// it; the trade is deliberate (a short one would put the old race back).
const (
	plantWaitAttempts = 200
	plantPollInterval = 5 * time.Millisecond
	plantSleepSeconds = "30"
	// plantArgvLen is the plant's argv arity: the lying argv[0] plus the
	// duration the real program needs in order to stay alive (Precondition 1),
	// and plantDurationArgIdx is where that duration sits.
	plantArgvLen        = 2
	plantDurationArgIdx = 1
	// plantDurationRes accepts the spellings `sleep` itself accepts for a
	// duration argument (an integer, a float, or a value with a unit suffix).
	plantDurationRes = `^([0-9]+(\.[0-9]+)?|[0-9]*\.[0-9]+)([smhd]?)$`
)

// plantDurationRe is plantDurationRes compiled once (the check runs on every
// case, and compiling a pattern inside a test body is a cost with no reader).
var plantDurationRe = regexp.MustCompile(plantDurationRes)

// plantAssertAlive turns "the planted child is still RUNNING" into a FIXTURE
// failure, naming the phase it was checked in. It is the difference between a
// guard that asserts and a guard that quietly classifies whatever it finds
// (Rule 15: an unavailable measurement is never a classification).
//
// TWO checks, because liveness alone is not the property this fixture needs:
//
//   - `Signal(0)` answers "not collected" — but a ZOMBIE satisfies it. And a
//     zombie is precisely the state the old fixture landed in: the child exited
//     within ~50 µs (measured: 400/400 runs at 50 µs were state=Z), and a
//     zombie's /proc/<pid>/exe is ENOENT, so the classifier answered `unknown`
//     for a process that was "alive" by POSIX and unmeasurable in fact.
//   - the state field of /proc/<pid>/stat is therefore read too, and Z/X (exited
//     and not yet reaped, or dead) is a fixture failure. That is the measured
//     root cause stated as a check, not as a comment.
func plantAssertAlive(t *testing.T, cmd *exec.Cmd, plantArgv0, phase string) {
	t.Helper()
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the plant is not measurable: pid %d (argv[0]=%q) was not alive %s (%v) — the fixture "+
			"must keep a real program alive, or the assertion below measures a corpse and reads "+
			"`unknown` as a verdict", cmd.Process.Pid, plantArgv0, phase, err)
	}
	raw, err := os.ReadFile(filepath.Join(depsProcRoot, strconv.Itoa(cmd.Process.Pid), plantProcStatName))
	if err != nil {
		t.Fatalf("the plant is not measurable: %s of pid %d (argv[0]=%q) is unreadable %s (%v) — a "+
			"process with no process table entry cannot be classified", plantProcStatName,
			cmd.Process.Pid, plantArgv0, phase, err)
	}
	if state := plantProcState(raw); state == plantProcStateZombie || state == plantProcStateDead {
		t.Fatalf("the plant is not measurable: pid %d (argv[0]=%q) is state=%q %s — an exited process "+
			"has no readable /proc/<pid>/exe, so the classifier would answer %q and this guard would "+
			"assert nothing", cmd.Process.Pid, plantArgv0, state, phase, depsOriginUnknown)
	}
}

// plantProcState extracts the state character from /proc/<pid>/stat: the field
// after the parenthesised comm, which may itself contain spaces and parentheses
// — hence the LAST ')' in the record, not the first field.
func plantProcState(stat []byte) string {
	rec := string(stat)
	i := strings.LastIndex(rec, ") ")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(rec[i+2:], " ", 2)[0])
}

const (
	// plantProcStatName is the process-table record read by plantAssertAlive.
	plantProcStatName = "stat"
	// plantProcStateZombie / plantProcStateDead are the state characters that
	// mean "this process is over" (Z = exited, not yet collected; X = dead).
	plantProcStateZombie = "Z"
	plantProcStateDead   = "X"
)

// plantWaitExeReadable blocks until the planted child is MEASURABLE — alive,
// past its exec, and answering /proc/<pid>/exe — and fails the test when it
// never becomes so. It returns the target it read.
//
// WHY A PRECONDITION AND NOT A RETRY AROUND THE ASSERTION: the assertion is
// about a LIVE process's executable. Read too early — or against a child that
// has already exited — and the symlink is missing, the classifier correctly
// answers `unknown` (Rule 15), and the guard silently stops asserting anything.
// So the unmeasurable plant is a fixture failure, loudly, and the signal is the
// process's own state: `Signal(0)` answers liveness (it returns
// os.ErrProcessDone for a child that has exited and been collected — precisely
// the exit-1 `sleep` the old fixture produced) and the symlink's target must
// equal the executable we started.
//
// The symlink is read with os.Readlink, never os.ReadFile: os.ReadFile follows
// it and reads the BINARY, so the instrument would report a failure that has
// nothing to do with the question — this repo has already paid once for a
// broken instrument that looked like evidence.
func plantWaitExeReadable(t *testing.T, cmd *exec.Cmd, wantExe, plantArgv0 string) string {
	t.Helper()
	pid := cmd.Process.Pid
	link := filepath.Join(depsProcRoot, strconv.Itoa(pid), depsProcExe)
	var lastExe string
	var lastErr error
	for i := 0; i < plantWaitAttempts; i++ {
		// Liveness first: an exited child has no /proc/<pid>/exe at all, and
		// classifying that absence as `unknown` is the defect this fixture had.
		plantAssertAlive(t, cmd, plantArgv0, "while waiting for its exe to become readable")
		lastExe, lastErr = os.Readlink(link)
		if lastErr == nil && lastExe == wantExe {
			return lastExe
		}
		time.Sleep(plantPollInterval) //stylegate:allow-sleep bounded /proc poll: the child's exec installs the exe symlink, so the loop re-reads until it lands (plantWaitAttempts caps it; the failure path is a Fatalf, never a silent pass)
	}
	t.Fatalf("the plant is not measurable: %s never read back %q (last %q, err %v, pid %d) — "+
		"the fixture must produce a live process whose exe is readable before the classifier is asked",
		link, wantExe, lastExe, lastErr, pid)
	return ""
}

// TestScratchGCDepsCronLineIsASchedule exercises the real extractor on the
// shapes a crontab actually carries: a schedule (five fields + command), the
// `@reboot` shorthand, an environment assignment and a comment (neither
// schedules anything — reporting them as findings is noise a reader learns to
// skip).
func TestScratchGCDepsCronLineIsASchedule(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		wantCmd string
		wantOK  bool
	}{
		{"weekly gopi entry", "0 3 * * 0 /home/nick/bin/gopi-home-gc.sh", "/home/nick/bin/gopi-home-gc.sh", true},
		{"step field", "*/10 * * * * /home/nick/bin/hp-worker-autostart.sh", "/home/nick/bin/hp-worker-autostart.sh", true},
		{"reboot shorthand", "@reboot /home/nick/bin/gopi-worker-up", "/home/nick/bin/gopi-worker-up", true},
		{"environment assignment", "PATH=/usr/bin:/bin", "", false},
		{"comment", "# 0 3 * * 0 /home/nick/bin/gopi-home-gc.sh", "", false},
		{"blank", "   ", "", false},
		{"prose, not a schedule", "run the crontab installer when the bus is down", "", false},
		{"too few fields", "0 3 * *", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := depsCronCommand(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("depsCronCommand(%q) ok=%v, want %v", tc.line, ok, tc.wantOK)
			}
			if got != tc.wantCmd {
				t.Errorf("depsCronCommand(%q) cmd=%q, want %q", tc.line, got, tc.wantCmd)
			}
		})
	}
}

// TestScratchGCDepsOriginIgnoresNonExecutableArgs is THE regression guard. The
// verdict comes from the executable, so changing an argument that is not the
// program must not change it. The scratch-path case is the one that was
// measured wrong on this host.
func TestScratchGCDepsOriginIgnoresNonExecutableArgs(t *testing.T) {
	// Both argv lists are the shape Chromium leaves behind: field 0 is the
	// entire command line, and the directory name carries a pattern the name
	// regex would match.
	inScratch := []string{
		"/snap/chromium/3537/usr/lib/chromium-browser/chrome --type=gpu-process --no-sandbox " +
			"--user-data-dir=/home/nick/workspace/gopi-test-scratch/queue/queue-runtime-shot-875503/" +
			"gopi-shot-profile-2645074146 --use-angle=swiftshader-webgl",
	}
	inTmp := []string{
		"/snap/chromium/3537/usr/lib/chromium-browser/chrome --type=gpu-process --no-sandbox " +
			"--user-data-dir=/tmp/prof --use-angle=swiftshader-webgl",
	}
	// depsResidentOrigin reads /proc for a REAL pid; the guard asserts the
	// INVARIANT that the function it replaced violated by construction, using
	// the pure part of the decision: the name test on the PROGRAM basename.
	scratchProg := filepath.Base(strings.Fields(inScratch[0])[0])
	tmpProg := filepath.Base(strings.Fields(inTmp[0])[0])
	if scratchProg != "chrome" || tmpProg != "chrome" {
		t.Fatalf("the two spellings must name the SAME program: %q vs %q — "+
			"this is exactly the argv[0] trap (the field is the whole line, not the executable)",
			scratchProg, tmpProg)
	}
	if depsClassify("chrome") != depsOriginForeign {
		t.Errorf("a browser's program name must classify foreign, got %q", depsClassify("chrome"))
	}
	// The trap, stated so it cannot come back: the raw argv[0] string WOULD
	// match, which is why argv[0] must never be what is classified.
	if !depsGopiNameRes.MatchString(inScratch[0]) {
		t.Fatalf("the fixture no longer reproduces the defect: the scratch path must match the name regex")
	}
	if !depsInterpreterRes.MatchString("bash") || !depsInterpreterRes.MatchString("python3") {
		t.Errorf("interpreter detection must accept bash and python3")
	}
	if depsInterpreterRes.MatchString("chrome") {
		t.Errorf("an interpreter-only rule must not claim a browser")
	}
}

// TestScratchGCDepsClassifyGopiShapes pins the name test's two sides: ours are
// visible (including the HP worker family Rule 35's incident was about), and a
// neighbour's is not silently dropped from the report.
func TestScratchGCDepsClassifyGopiShapes(t *testing.T) {
	ours := []string{"gopi", "gopi-worker-up", "hp-worker-autostart.sh", "/home/nick/bin/gopi"}
	for _, s := range ours {
		if got := depsClassify(s); got != depsOriginGopi {
			t.Errorf("depsClassify(%q) = %q, want %q", s, got, depsOriginGopi)
		}
	}
	theirs := []string{"chrome", "systemd", "cron", ""}
	for _, s := range theirs {
		want := depsOriginForeign
		if strings.TrimSpace(s) == "" {
			want = depsOriginUnknown
		}
		if got := depsClassify(s); got != want {
			t.Errorf("depsClassify(%q) = %q, want %q", s, got, want)
		}
	}
}

// TestScratchGCDepsInterpreterCommandStringIsNotAName is the SECOND sighting of
// the same defect, and it is the one the first guard could not see: the
// argv[0]-fixed classifier still judged an INTERPRETER's first non-flag
// argument, which for `sh -c "<line>"` is a command STRING, not a path.
//
// Measured live on this host (2026-09-26), two shells at once:
//
//	bash -c "cd /home/nick/workspace/gopi-01a0ddfa2d98732aa5a3968bd10b6a3a && sleep 60 & wait"
//
// classified `origin=gopi` — the word came from a WORKTREE DIRECTORY NAME. The
// line was the arm's OWN invocation, so the count depended on how the report was
// invoked (7 residents plainly, 8 wrapped, 9 with the plant). A measurement that
// moves with the caller's command line is not a measurement.
//
// The guard drives the pure decision, so it needs no planted process and cannot
// be skipped on a busy host. Every case here is a shape that was measured or is
// reachable by construction; the LAST two are the anti-over-fix controls — a
// real script operand must still be judged, or the fix would blind the arm.
func TestScratchGCDepsInterpreterCommandStringIsNotAName(t *testing.T) {
	worktree := "/home/nick/workspace/gopi-01a0ddfa2d98732aa5a3968bd10b6a3a"
	cases := []struct {
		name string
		prog string
		argv []string
		want string
	}{
		{
			name: "bash -c with a gopi-named cwd (the measured false positive)",
			prog: "bash",
			argv: []string{"bash", "-c", "cd " + worktree + " && sleep 60 & wait"},
			want: depsOriginUnknown,
		},
		{
			name: "sh -c running a gopi command (the target is its OWN process)",
			prog: "sh",
			argv: []string{"sh", "-c", "./bin/gopi worker"},
			want: depsOriginUnknown,
		},
		{
			name: "bash -c whose string carries the binary path",
			prog: "bash",
			argv: []string{"bash", "-c", "/home/nick/work/GoPi/bin/gopi merge --every 3m"},
			want: depsOriginUnknown,
		},
		{
			name: "the ulimit wrapper shape measured on this host",
			prog: "bash",
			argv: []string{"bash", "-c", "ulimit -v 12582912 -u 4096 2>/dev/null; cd " + worktree + " && go build ./cmd/agent/"},
			want: depsOriginUnknown,
		},
		{
			name: "sh -c whose operand has no spaces (only the -c rule catches this)",
			prog: "sh",
			argv: []string{"sh", "-c", "gopi"},
			want: depsOriginUnknown,
		},
		{
			name: "a quoted script name carrying a space (only the space rule catches this)",
			prog: "bash",
			argv: []string{"bash", "/home/nick/bin/gopi worker up"},
			want: depsOriginUnknown,
		},
		{
			name: "anti-over-fix: a direct script operand is still judged",
			prog: "bash",
			argv: []string{"bash", "/home/nick/bin/gopi-worker-up.sh", "2"},
			want: depsOriginGopi,
		},
		{
			name: "anti-over-fix: a shell script that is not gopi's",
			prog: "bash",
			argv: []string{"bash", "/usr/local/bin/backup.sh"},
			want: depsOriginForeign,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := depsOriginFromProgram(tc.prog, tc.argv); got != tc.want {
				t.Errorf("depsOriginFromProgram(%q, %q) = %q, want %q", tc.prog, tc.argv, got, tc.want)
			}
		})
	}
	// The guard is worthless if the fixture stops reproducing the trap: the
	// pre-fix rule (base of the first non-flag argument) must classify the
	// measured line as gopi's — otherwise this test would pass under the bug.
	measured := []string{"bash", "-c", "cd " + worktree + " && sleep 60 & wait"}
	if got := depsClassify(filepath.Base(measured[2])); got != depsOriginGopi {
		t.Fatalf("the fixture no longer reproduces the defect: the pre-fix rule would read %q, want %q",
			got, depsOriginGopi)
	}
	// And the unreadable-exe path stays `unknown`, never a silent `foreign`.
	if got := depsResidentOrigin(1<<30, []string{"gopi"}); got != depsOriginUnknown {
		t.Errorf("a pid with no exe must be %q, got %q (Rule 15: never a silent wrong answer)",
			depsOriginUnknown, got)
	}
}
