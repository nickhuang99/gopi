// launch.go — HOW `gopi tool shot` starts headless chrome, and the clamp it must
// escape first.
//
// WHY THIS FILE EXISTS (measured on peer1, 2026-09-19): the SPA capture path
// was dead in every gopi-worker context. `bash` children carry scratch's child
// clamp (`internal/scratch`: `ulimit -v 12582912 -u 4096`), and the hard limit
// is inherited, so chrome died at startup with a V8 CHECK failure
// (`traps: chrome[...] trap int3`) before printing a DevTools port.
//
// ROOT CAUSE (four controlled probes, identical chrome argv, only the systemd
// Limit* property differing):
//
//	LimitAS=12GiB + LimitNPROC=4096 → no CDP endpoint
//	LimitNPROC=4096 only            → CDP endpoint up
//	LimitAS=12GiB only              → no CDP endpoint
//	no limits                       → CDP endpoint up
//
// So RLIMIT_AS is the cause and RLIMIT_NPROC is harmless to chrome. The
// mechanism is a VIRTUAL reservation, not usage: chrome/V8 maps ~55 GiB of
// address space (measured VmPeak 5.49e7 kB per process — the pointer-
// compression cage/sandbox reservation) while its RSS is ~200 MiB and the
// unit's memory current was 36 MiB. RLIMIT_AS bounds the reservation, so the
// process dies before it ever uses memory it was never going to use.
//
// THE ESCAPE (verified): a process spawned by the systemd USER MANAGER starts
// outside the session's process limits. `systemd-run --user --unit=… --collect
// -q -- <chrome argv>` is a transient SERVICE unit under
// `user@1000.service/app.slice/…` with unclamped rlimits. Note that `--scope`
// does NOT work — a transient scope inherits the caller's process limits; only
// the service-unit form escapes.
//
// THE REPLACEMENT GUARD (the clamp is not simply dropped): the transient unit
// carries `MemoryMax=<defaultChromeMemoryMaxMB>M MemorySwapMax=0`, which bounds
// what actually threatens the host — resident memory — instead of a virtual
// reservation that is never touched. Measured: chrome renders fine under it.
//
// DEGRADATION IS ANNOUNCED, NEVER SILENT: where no user manager answers, the
// launch falls back to the attached path (which cannot escape the clamp) and
// says so on stderr, naming the limit that will kill chrome.
package shot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// Named constants (Rule 1) — no magic numbers below this comment block.
const (
	// chromeMemMaxEnvName overrides the transient unit's memory ceiling, in
	// MiB. Read in exactly ONE place (chromeMemoryMaxMB, Rule 2).
	chromeMemMaxEnvName = "GOPI_SHOT_MEM_MAX_MB"

	// ChromeMemMaxEnvName is chromeMemMaxEnvName's value for the caller that
	// must NAME the knob rather than read it — `gopi tool shot -h` documents it.
	// An ALIAS, not a second definition (Rule 14): the value has exactly one
	// holder, above.
	//
	// env-reader-exempt: an alias of chromeMemMaxEnvName; the reader is
	// os.Getenv(chromeMemMaxEnvName) in chromeMemoryMaxMB, and no code reads a
	// knob through this identifier.
	ChromeMemMaxEnvName = chromeMemMaxEnvName

	// DefaultChromeMemoryMaxMB exports defaultChromeMemoryMaxMB's value for the
	// usage text, which must state the default it would apply.
	DefaultChromeMemoryMaxMB = defaultChromeMemoryMaxMB

	// defaultChromeMemoryMaxMB is the replacement guard's default ceiling. It
	// bounds RSS — measured chrome RSS is ~200 MiB (unit memory current 36 MiB
	// while idle at about:blank) — so 512 MiB leaves room for a real page while
	// staying far below the host's 15.5 GB.
	defaultChromeMemoryMaxMB = 512

	// minChromeAddressSpaceBytes is the smallest soft RLIMIT_AS under which
	// chrome is expected to start. MEASURED: a working chrome reports VmPeak
	// 5.49e7 kB ≈ 51 GiB of virtual address space per process, so anything
	// below 64 GiB is treated as "will not start" and takes the unit path.
	minChromeAddressSpaceBytes = 64 << 30

	// rlimitInfinity is RLIM_INFINITY as the kernel reports it in a struct
	// rlimit (all bits set) — the "no limit" value. Declared here with its
	// meaning rather than inlined at the comparison (Rule 1a).
	rlimitInfinity = ^uint64(0)

	// systemdRunBin / systemctlBin are the host tools carrying the escape and
	// its teardown. They are constants because the launcher, its tests and the
	// operator-facing fallback message must agree on one spelling (Rule 1b).
	systemdRunBin = "systemd-run"
	systemctlBin  = "systemctl"

	// shotUnitPrefix names the transient unit. Prefixed so an operator can see
	// at a glance which units belong to the capture path. The VALUE lives in
	// internal/scratch (scratch.ShotUnitPrefix) because the sweeper must
	// recognise exactly the same names this launcher mints (Rules 13/14).
	shotUnitPrefix = scratch.ShotUnitPrefix

	// chromeErrFileName holds chrome's stderr INSIDE the throwaway profile dir
	// (already removed by Capture's defer). systemd-run redirects the unit's
	// stderr there because a unit's stderr has no caller-side pipe.
	chromeErrFileName = "chrome.err"

	// systemdRunStartTimeout bounds the systemd-run client call itself. It
	// returns as soon as the unit is started (Type=simple), so this is a
	// bound on the manager round-trip, not on chrome.
	systemdRunStartTimeout = 10 * time.Second

	// systemctlStopTimeout bounds the teardown call.
	systemctlStopTimeout = 10 * time.Second

	// systemctlExitNoSuchUnit is systemctl's "unit not loaded" status (5, the
	// documented EXIT_NOTINSTALLED/LSB code; measured on this host). For a
	// teardown it is the NORMAL outcome: the unit was started with --collect,
	// so a chrome that already exited has had its unit garbage-collected and
	// there is nothing left to stop. The exit status is the only reliable
	// signal (`systemctl stop` also exits 5 when the manager is unreachable),
	// so the message is gated on the status AND the unit's absence is what the
	// status means for a unit this process created moments ago.
	systemctlExitNoSuchUnit = 5

	// devtoolsFilePollInterval is the poll cadence for the DevTools line in
	// chrome's redirect file (paired with chromeStartTimeout).
	devtoolsFilePollInterval = 100 * time.Millisecond

	// systemdRunSetenvFlag is systemd-run's "put this variable into the unit's
	// environment" option (`-E/--setenv=NAME=VALUE`).
	systemdRunSetenvFlag = "--setenv="
)

// chromeProcess is a started headless chrome plus its teardown.
type chromeProcess struct {
	port string
	stop func()
}

// chromeArgv are the flags every headless capture launches with. Shared by both
// launch paths so the two can never drift (Rule 14: one definition).
func chromeArgv(profile string) []string {
	return []string{
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		"about:blank",
	}
}

// launchChrome starts headless chrome and returns its DevTools port plus a
// teardown func. When the inherited address-space limit forbids chrome's
// startup reservation the process is started as a transient systemd user unit;
// otherwise (and as an announced fallback) it is started as an ordinary child.
func launchChrome(ctx context.Context, bin, profile string) (*chromeProcess, error) {
	argv := chromeArgv(profile)
	if !addressSpaceClamped() {
		return launchChromeAttached(ctx, bin, argv)
	}
	proc, err := launchChromeUserUnit(bin, argv, profile)
	if err == nil {
		return proc, nil
	}
	// Honest degradation (Rule 25's spirit: announce, never pass off): the
	// attached launch inherits the clamp, so chrome will most likely die again.
	fmt.Fprintf(os.Stderr,
		"%v — falling back to an attached launch, which cannot escape "+
			"the inherited address-space limit (chrome reserves ~51 GiB of virtual "+
			"address space); chrome may fail to start under this clamp\n", err)
	return launchChromeAttached(ctx, bin, argv)
}

// launchChromeAttached starts chrome as a plain child of this process and reads
// the DevTools port from its stderr pipe (the original path).
func launchChromeAttached(ctx context.Context, bin string, argv []string) (*chromeProcess, error) {
	cmdCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, bin, argv...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("shot: start %s: %w", bin, err)
	}
	stop := func() {
		cancel()   // kills the whole chrome process group via CommandContext
		cmd.Wait() //nolint:errcheck // reaped here; the exit status is not needed
	}
	port, err := waitDevToolsPort(stderr)
	if err != nil {
		stop()
		return nil, err
	}
	return &chromeProcess{port: port, stop: stop}, nil
}

// launchChromeUserUnit starts chrome as a transient systemd USER service. The
// unit is created by the user manager, so it does not inherit this process's
// address-space limit, and it carries the memory ceiling that replaces it.
func launchChromeUserUnit(bin string, argv []string, profile string) (*chromeProcess, error) {
	errPath := filepath.Join(profile, chromeErrFileName)
	unit := shotUnitName()
	runArgv := systemdRunArgs(unit, errPath, chromeMemoryMaxMB(), unitInheritedEnv(), append([]string{bin}, argv...))

	startCtx, cancel := context.WithTimeout(context.Background(), systemdRunStartTimeout)
	defer cancel()
	out, err := exec.CommandContext(startCtx, systemdRunBin, runArgv...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("shot: %s --user failed (no user manager?): %w%s",
			systemdRunBin, err, formatCmdOutput(out))
	}

	// REGISTER NOW, not after the port appears: the unit exists from the line
	// above, and a kill during the (up to chromeStartTimeout) startup window
	// must not leave an unregistered unit behind. A registration failure is
	// ANNOUNCED and the capture continues — it degrades this unit's teardown to
	// the pre-fix behaviour (the defer below), which is not worth failing a
	// capture over (Rule 15b: recorded, never swallowed).
	deregister := registerShotUnit(unit)

	port, err := waitDevToolsPortInFile(errPath, chromeStartTimeout)
	if err != nil {
		stopUserUnit(unit)
		deregister()
		return nil, err
	}
	return &chromeProcess{port: port, stop: func() {
		stopUserUnit(unit)
		deregister()
	}}, nil
}

// registerShotUnit records the unit in the scratch unit registry and returns the
// deregistration to be run on the NORMAL stop path.
//
// WHY IT IS REGISTERED AT ALL: teardown is a defer, and a defer does not run for
// a SIGKILLed process (a test timeout, a cancelled run). The registration is the
// copy of the fact that survives the writer — `gopi tool scratch-gc` stops the units
// whose creating process is provably gone (internal/scratch.ReclaimShotUnits).
//
// The returned deregistration is a no-op that REPORTS on failure: an
// unremoved registration is not a leak (the next sweep stops an already-gone
// unit harmlessly), but it is a fact the operator should be able to see.
func registerShotUnit(unit string) func() {
	dir := scratch.ShotUnitDir()
	if _, err := scratch.RegisterShotUnit(dir, unit, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "shot: %s is not registered for reclamation (%v) — "+
			"a kill -9 of this process will leave its chrome running\n", unit, err)
		return func() {}
	}
	return func() {
		if err := scratch.DeregisterShotUnit(dir, unit); err != nil {
			fmt.Fprintf(os.Stderr, "shot: %s registration not removed: %v\n", unit, err)
		}
	}
}

// systemdRunArgs builds the argv that starts chrome as a transient unit. Pure,
// so the shape is unit-testable without touching systemd.
func systemdRunArgs(unit, errPath string, memMaxMB int, unitEnv []string, chromeArgv []string) []string {
	args := []string{
		"--user",
		"--unit=" + unit,
		"--collect", // garbage-collect the unit, so a dead run leaves no residue
		"--quiet",
		"-p", "MemoryMax=" + strconv.Itoa(memMaxMB) + "M",
		"-p", "MemorySwapMax=0",
		"-p", "StandardError=file:" + errPath,
	}
	// A TRANSIENT UNIT DOES NOT INHERIT THE CALLER'S ENVIRONMENT. Measured on
	// systemd 249, launching a stub through this exact argv: the unit's own
	// /proc/self/environ carried `PWD=/home/user` (the user's home, not the
	// caller's directory) and NO TMPDIR at all, while the same stub launched as
	// a plain child saw both. So a variable the launched program is entitled to
	// must be handed over EXPLICITLY, or the escape silently drops it — an
	// inheritance the caller believes it has and never got.
	for _, kv := range unitEnv {
		args = append(args, systemdRunSetenvFlag+kv)
	}
	args = append(args, "--")
	return append(args, chromeArgv...)
}

// unitInheritedEnv lists the caller variables a launched browser is entitled to
// receive inside its transient unit, as NAME=VALUE pairs. The unit environment
// is CLEAN (systemdRunArgs records the measurement), so this list is the WHOLE
// inheritance and it is stated in one place.
//
// scratch.TmpDirEnvName is on it because gopi's temp-root contract publishes
// exactly this variable for the child tree: a browser launched without it
// resolves the AMBIENT temp root instead, which is the fallback the tmpdir epic
// exists to remove — an inheritance the publisher believes it has and the unit
// never got.
//
// An UNSET caller variable contributes NOTHING: the unit keeps systemd's own
// default rather than being handed an empty string, because "" and "absent" are
// different answers to `os.LookupEnv` and only one of them is what the caller
// said.
//
// OTHER caller variables are still dropped by the unit (measured alongside:
// `$PWD` inside the unit is the user's home, not the caller's directory). That
// is pre-existing behaviour of the escape and is NOT covered here — a browser
// driven by absolute paths does not depend on it, and widening this list to
// "whatever the caller happens to export" would hand a systemd unit an
// environment nobody declared.
func unitInheritedEnv() []string {
	var env []string
	if v, ok := os.LookupEnv(scratch.TmpDirEnvName); ok {
		env = append(env, scratch.TmpDirEnvName+"="+v)
	}
	return env
}

// shotUnitName returns a unique transient unit name (concurrent captures must
// not collide, and a recycled name would attach to a stale unit).
func shotUnitName() string {
	return shotUnitPrefix + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// stopUserUnit stops (and, with --collect, reaps) the transient unit. A failure
// is RECORDED, never swallowed (Rule 15b) — unless the unit is already gone,
// which is the normal outcome when chrome exited on its own.
func stopUserUnit(unit string) {
	if err := stopUserUnitErr(unit); err != nil {
		fmt.Fprintf(os.Stderr, "shot: %s --user stop %s: %v\n", systemctlBin, unit, err)
	}
}

// stopUserUnitErr is stopUserUnit as a RETURNED error, so the reclamation sweep
// (internal/scratch's injected stop) can decide for itself whether a unit was
// stopped: a failed stop must leave the registration in place for the next
// sweep rather than be reported as reclaimed. ONE teardown definition, two
// callers (the defer and the sweep) — never a second systemctl command line.
//
// A unit that is already gone (EXIT_NOTINSTALLED, systemctlExitNoSuchUnit) is
// SUCCESS: it was started with --collect, so a chrome that exited on its own has
// had its unit garbage-collected and there is nothing left to stop.
func stopUserUnitErr(unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlStopTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemctlBin, "--user", "stop", unit).CombinedOutput()
	if err == nil || exitStatusIs(err, systemctlExitNoSuchUnit) {
		return nil
	}
	// %w, not %v: the caller (the sweep) asks whether the stop failed with a
	// reason the chain can describe, and errors.Is/As must keep working
	// (PROJECT.md Rule 16).
	return fmt.Errorf("%w%s", err, formatCmdOutput(out))
}

// exitStatusIs reports whether err is an *exec.ExitError with the given status.
func exitStatusIs(err error, status int) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	return ee.ExitCode() == status
}

// waitDevToolsPortInFile polls chrome's redirect file until the DevTools
// listening line appears, bounded by timeout (chromeStartTimeout at the call
// site; a caller-owned bound so a test can exercise the failure fast).
func waitDevToolsPortInFile(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if b, err := os.ReadFile(path); err == nil {
			if m := wsURLPattern.FindStringSubmatch(string(b)); m != nil {
				return m[1], nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("shot: chrome did not report a DevTools port within %s (stderr: %s)",
				timeout, firstLineOrEmpty(path))
		}
		time.Sleep(devtoolsFilePollInterval)
	}
}

// firstLineOrEmpty returns the first line of a file for an error message, or ""
// when the file cannot be read (the error message must not fail).
func firstLineOrEmpty(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(line)
}

// formatCmdOutput renders captured command output for a message, empty string
// when there is none.
func formatCmdOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	return ": " + s
}

// chromeMemoryMaxMB resolves the replacement guard's ceiling (Rule 2: the ONE
// reader of chromeMemMaxEnvName). A malformed value is announced and treated as
// unset, so a typo can never silently drop the bound.
func chromeMemoryMaxMB() int {
	raw := strings.TrimSpace(os.Getenv(chromeMemMaxEnvName))
	if raw == "" {
		return defaultChromeMemoryMaxMB
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "shot: %s=%q is not a positive integer — using %dMiB\n",
			chromeMemMaxEnvName, raw, defaultChromeMemoryMaxMB)
		return defaultChromeMemoryMaxMB
	}
	return n
}

// addressSpaceClamped reports whether THIS process's soft RLIMIT_AS is small
// enough to forbid chrome's startup reservation.
func addressSpaceClamped() bool {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &lim); err != nil {
		return false
	}
	return addressSpaceTooSmall(lim.Cur)
}

// addressSpaceTooSmall is the pure predicate behind addressSpaceClamped: a soft
// limit of RLIM_INFINITY never blocks chrome; anything below the measured
// reservation does.
func addressSpaceTooSmall(soft uint64) bool {
	return soft != rlimitInfinity && soft < minChromeAddressSpaceBytes
}
