package main

// The guards for the START-PATH legibility defect this branch fixes
// (epic cmd-agent-ledger-busy-dsn, 2026-09-15) and for the PER-PROFILE unit
// identity (epic daemon-scope-per-profile, 2026-09-16).
//
// DEFECT 1 — the scope unit name was FIXED per face, so systemd refused the
// second start. Measured on this host: the resident `gopi merger --daemon`
// holds `gopi-daemon-merger.scope`, and every later start (in particular every
// TEST-started merger, which passes the escape probe and then asks systemd-run
// for that same unit) died with "Failed to start transient scope unit: Unit
// gopi-daemon-merger.scope already exists." — burning the handshake budget each
// time, which is how two concurrent full-suite runs both ended in
// `panic: test timed out after 10m0s`.
//
// DEFECT 1b — the first fix made the name a function of the LAUNCHER'S PID.
// That removed the collision with the operator's resident daemon but put the
// uniqueness in the wrong dimension: `go test` runs every case of a package
// inside ONE process, so every daemon that package started carried the SAME
// pid, and the operator could not NAME the unit (it changed on every start).
//
// DEFECT 2 — the failure was ILLEGIBLE: the parent reported only
// "(see the log): EOF", a transport event, while the cause sat in the daemon's
// own log. Rule 15: an error names its cause, never just its symptom.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// probeHoldProcessName is the program the existing-unit probe runs to HOLD a
// transient scope open: it must be alive while the question is asked (an exited
// scope releases its name immediately — measured).
const probeHoldProcessName = "/bin/sleep"

// probeHoldSecondsArg keeps that holder alive for the whole test. It must
// comfortably OUTLAST the probe's own observations: the holder is the unit's
// process (not the systemd-run that stays in the foreground for its lifetime),
// so a holder that expires mid-test reads as a released name, not as a bug.
const probeHoldSecondsArg = "60"

// controlStopVerb is the systemctl verb that ends a unit this test created.
const controlStopVerb = "stop"

// daemonProbePollInterval is the wait between two attempts to observe a unit
// systemd has just been asked to create. systemd creates the unit before it
// records its cgroup, so a single immediate read can miss it; the wait is
// bounded by systemctlQueryBudget.
const (
	daemonProbePollInterval = 20 * time.Millisecond
)

// daemonProbeLauncherLogName is the file, inside the test's own temp dir, that
// catches the probe launcher's output: a failed creation must be REPORTED with
// systemd's own words, never as a bare timeout.
const daemonProbeLauncherLogName = "systemd-run.log"

// daemonProbeLauncherLogMode is that file's mode (owner-only: it is this test's
// private evidence).
const daemonProbeLauncherLogMode = 0o600

// daemonProbeReapBudget bounds the wait for the launcher to exit after its unit
// has been stopped — it exits with its child (measured).
const daemonProbeReapBudget = 2 * time.Second

// daemonProbeEnvCommand is the program the profile-stamped probe holder runs:
// `/usr/bin/env <NAME=VALUE> /bin/sleep 60`, so the holder carries the profile
// stamp in its REAL environment (what daemonProcessEnvVar reads back).
const daemonProbeEnvCommand = "/usr/bin/env"

// daemonProbeStartScope creates a transient scope named unitName holding argv,
// and returns as soon as the creation has been handed to systemd. The creation
// is DELIBERATELY NOT WAITED FOR: `systemd-run --scope` stays in the foreground
// for its child's whole lifetime and exits with the CHILD's status (measured), so
// waiting here would tie a 60s holder to this call and any caller-side deadline
// would kill the creation itself (recorded as `signal: killed` — the shape that
// made the original test SKIP instead of fail).
//
// The launcher's output goes to a log inside the test's temp dir, and the
// launcher is DISOWNED (its own session): the scope outlives the launcher's
// usefulness to this test, and the unit is stopped by its exact NAME in cleanup
// (never a pattern kill — the operator has live gopi daemons here).
func daemonProbeStartScope(t *testing.T, unitName string, argv ...string) *exec.Cmd {
	t.Helper()
	args := []string{
		systemdRunFlagUser, systemdRunFlagScope, systemdRunFlagQuiet,
		systemdRunFlagUnit + daemonFlagValueSep + unitName,
	}
	args = append(args, argv...)
	logPath := filepath.Join(t.TempDir(), daemonProbeLauncherLogName)
	//stylegate:ignore probe launcher log: this test's own file, removed with its TempDir
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, daemonProbeLauncherLogMode)
	if err != nil {
		t.Fatalf("create %s: %v", logPath, err)
	}
	defer logFile.Close()
	cmd := exec.Command(systemdRunPath, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s --unit=%s: %v", systemdRunPath, unitName, err)
	}
	t.Cleanup(func() {
		stop := exec.Command(systemctlPath, systemdRunFlagUser, controlStopVerb, unitName+daemonUnitFileSuffix)
		//stylegate:ignore best-effort cleanup of the probe unit this test created; its exit status changes no assertion
		_ = stop.Run()
		// stylegate:ignore best-effort cleanup of the probe unit this test created; its exit status changes no assertion
		_ = stop.Run() //stylegate:ignore best-effort stop of the probe unit this test created; a failure changes no assertion
		// The launcher exits with its child (measured); it is reaped so this
		// test leaves no zombie behind, and killed if the stop did not take.
		reaped := make(chan struct{})
		go func() {
			//stylegate:ignore reaping the launcher this helper started; its status is not an assertion (the unit's own state is)
			_ = cmd.Wait()
			// stylegate:ignore reaping the launcher this helper started; its status is not an assertion (the unit's own state is)
			_ = cmd.Wait() //stylegate:ignore reaping a process this test spawned; the case asserts through the fixture's own files, not through this status
			close(reaped)
		}()
		select {
		case <-reaped:
		case <-time.After(daemonProbeReapBudget):
			//stylegate:ignore launcher that outlived its own unit: best-effort kill of a process this test started
			_ = cmd.Process.Kill()
		}
	})
	return cmd
}

// daemonProbeAwaitUnit waits, within ONE query budget, for unitName to answer
// "taken" — the state the assertions need. On timeout it FAILS with systemd's
// own words, never with a bare "timed out" and never with a skip: the caller has
// already proved a user bus works here, so a unit that never appears is this
// test's bug (Rule 15).
func daemonProbeAwaitUnit(t *testing.T, unitName string, launcher *exec.Cmd) {
	t.Helper()
	deadline := time.Now().Add(systemctlQueryBudget)
	for {
		taken, err := daemonScopeUnitExists(unitName)
		if err != nil {
			t.Fatalf("the existing-unit question must be answerable: %v", err)
		}
		if taken {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("systemd never reported %s as taken%s — a LIVE transient scope must answer as taken; "+
				"this is the condition the existing-unit decision exists for",
				unitName, daemonProbeLauncherSuffix(launcher))
		}
		time.Sleep(daemonProbePollInterval) //stylegate:allow-sleep bounded wait for systemd to report the unit it just created
	}
}

// daemonProbeLauncherSuffix adds the launcher's exit state to a failure message
// when it has already died — the usual cause of a unit that never appeared.
func daemonProbeLauncherSuffix(launcher *exec.Cmd) string {
	if launcher.ProcessState == nil {
		return fmt.Sprintf(" (the launcher is still running; pid %d)", launcher.Process.Pid)
	}
	return fmt.Sprintf(" (the launcher exited: %v)", launcher.ProcessState)
}

// daemonProbeHoldComm is the name the holder must report, proving the holder was
// found through the unit's CGROUP.
const daemonProbeHoldComm = "sleep"

// The two profile tags every name assertion is built from: same shape as a real
// one (readable tail + hash), different identities.
const (
	dsTagAlpha = "alpha-0123456789ab"
	dsTagBeta  = "beta-0123456789ab"
)

// TestScopeUnitNameIsPerProfile pins the uniqueness property the name needs —
// PER PROFILE, not per face and not per launch (the two earlier spellings and
// why each was measured wrong: see daemonScopeUnitPrefix).
//
// The property has two halves and both matter: two profiles must never share a
// name (the collision that killed a suite), and ONE profile must always produce
// the same name (an operator has to be able to name the unit, and a second
// start of the same profile is the FLOCK's refusal, not a systemd name clash).
func TestScopeUnitNameIsPerProfile(t *testing.T) {
	first := scopeUnitNameFor(CliVerbMerger, dsTagAlpha)
	again := scopeUnitNameFor(CliVerbMerger, dsTagBeta)
	if first == again {
		t.Fatalf("scopeUnitNameFor(%q, %q) == scopeUnitNameFor(%q, %q) = %q — two PROFILES would "+
			"ask systemd for ONE unit, which systemd refuses while it is alive ('Unit … already "+
			"exists')", CliVerbMerger, dsTagAlpha, CliVerbMerger, dsTagBeta, first)
	}
	if want := daemonScopeUnitPrefix + CliVerbMerger + daemonScopeUnitPidSep + dsTagAlpha; first != want {
		t.Errorf("scopeUnitNameFor(%q, %q) = %q, want %q", CliVerbMerger, dsTagAlpha, first, want)
	}
	// SAME profile, SAME name — the other half, and the one the pid suffix
	// could not provide. Rendering it twice is not a strong enough statement
	// (a name carrying os.Getpid() would also repeat within one process), so the
	// property is asserted where it is actually observable: the pure function
	// takes NO pid, so nothing about the caller can enter the name.
	if got, want := scopeUnitName(CliVerbWorker), scopeUnitNameFor(CliVerbWorker, daemonProfileUnitTagNow()); got != want {
		t.Errorf("scopeUnitName(%q) = %q, want %q (this process's own profile tag)", CliVerbWorker, got, want)
	}
	// The name must stay under the ONE prefix an operator greps, and must not
	// silently become the fixed per-face name again.
	if !strings.HasPrefix(first, daemonScopeUnitPrefix) {
		t.Errorf("%q lost the %q prefix — an operator's `systemctl --user list-units | grep gopi-daemon-` "+
			"would stop finding gopi's daemons", first, daemonScopeUnitPrefix)
	}
	if strings.HasSuffix(first, CliVerbMerger) {
		t.Errorf("%q is the FIXED per-face name — the collision defect is back", first)
	}
	// Different faces never collide, whatever the profile.
	if scopeUnitNameFor(CliVerbMerger, dsTagAlpha) == scopeUnitNameFor(CliVerbWorker, dsTagAlpha) {
		t.Errorf("the merger and the worker share the unit name %q", scopeUnitNameFor(CliVerbMerger, dsTagAlpha))
	}
	// The cgroupfs scope DIRECTORY shares the unit name (one visible node
	// whichever mechanism ran) — and therefore shares its uniqueness. A
	// directory named for the face alone would make cgroupscope.Enter join the
	// FIRST daemon's cgroup. The comparison is against the UNIT NAME itself (not
	// a re-rendered format string): the identity is the property, and a second
	// suffix expression here would be free to drift from it.
	if got, want := scopeDirName(CliVerbMerger), scopeUnitName(CliVerbMerger); got != want {
		t.Errorf("scopeDirName(%q) = %q, want the unit name %q — the cgroupfs scope directory must be the "+
			"ONE node carrying the unit name, not a per-face name an operator cannot correlate",
			CliVerbMerger, got, want)
	}
	if strings.Contains(scopeDirName(CliVerbMerger), daemonScopeUnitPrefix+daemonScopeUnitPrefix) {
		t.Errorf("scopeDirName(%q) = %q double-prefixes the unit name", CliVerbMerger, scopeDirName(CliVerbMerger))
	}
}

// TestScopeUnitNameCarriesNoLauncherPid is the RED PROOF for DEFECT 1b, stated
// as the property the previous fix would fail: the name must not contain the
// launching process's pid. It is asserted on the RENDERED name (not on the
// function's signature) so that re-introducing the pid ANYWHERE — a suffix, an
// infix, a fallback — fails here.
func TestScopeUnitNameCarriesNoLauncherPid(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	name := scopeUnitName(CliVerbWorker)
	if strings.Contains(name, pid) {
		t.Errorf("scopeUnitName(%q) = %q carries this process's pid (%s) — a name that changes with the "+
			"LAUNCHER is not a function of the profile: every case of one `go test` package starts its "+
			"daemons from the SAME pid, so they would all ask systemd for one unit", CliVerbWorker, name, pid)
	}
	// And the tag used to build it is this process's profile, not a constant.
	tag := daemonProfileUnitTagNow()
	if tag == "" || !strings.Contains(name, tag) {
		t.Errorf("scopeUnitName(%q) = %q does not carry this process's profile tag %q", CliVerbWorker, name, tag)
	}
}

// TestDaemonProfileUnitTagIsUniqueAndReadable pins the tag's two halves: it is
// READABLE (an operator recognises the profile) and UNIQUE (the readable half
// alone is not — two profiles may share a directory name).
func TestDaemonProfileUnitTagIsUniqueAndReadable(t *testing.T) {
	alpha := daemonProfileUnitTag("/home/nick/.gopi")
	// The DEFAULT profile's directory is ".gopi" for every such profile, so the
	// readable half comes from the HOME it lives in — otherwise the tag would
	// read "/(garbage)-<hash>" and tell the operator nothing.
	if !strings.HasPrefix(alpha, "nick") {
		t.Errorf("daemonProfileUnitTag(/home/nick/.gopi) = %q, want it to lead with the profile's readable "+
			"half (the HOME for a default profile, not the shared %q)", alpha, config.ProfileDirName)
	}
	// Two DIFFERENT profiles with the SAME directory name must not collide: the
	// hash is what separates them.
	sameNameA := daemonProfileUnitTag("/opt/a/gopi")
	sameNameB := daemonProfileUnitTag("/opt/b/gopi")
	if sameNameA == sameNameB {
		t.Fatalf("two profiles both named %q produced ONE tag %q — the name must be a function of the "+
			"whole identity, not of its last path element", "gopi", sameNameA)
	}
	if !strings.Contains(sameNameA, "gopi") || !strings.Contains(sameNameB, "gopi") {
		t.Errorf("the tags must still be READABLE (%q, %q)", sameNameA, sameNameB)
	}
	// One identity → one tag, always (the property the unit name inherits).
	if daemonProfileUnitTag("/opt/a/gopi") != daemonProfileUnitTag("/opt/a/gopi") {
		t.Error("the same identity rendered two different tags")
	}
	// The tag is always a usable unit-name token: systemd refuses a name that
	// STARTS with a separator, and a tag whose readable half was entirely
	// rejected must still produce one.
	for _, identity := range []string{"/home/nick/.gopi", "/opt/a/gopi", "/tmp/??-.-weird", "/", "profile"} {
		tag := daemonProfileUnitTag(identity)
		if tag == "" {
			t.Fatalf("daemonProfileUnitTag(%q) rendered an EMPTY tag", identity)
		}
		if strings.HasPrefix(tag, daemonUnitPartSep) {
			t.Errorf("daemonProfileUnitTag(%q) = %q starts with %q — systemd refuses such a unit name",
				identity, tag, daemonUnitPartSep)
		}
		if strings.ContainsAny(tag, "/ :@") {
			t.Errorf("daemonProfileUnitTag(%q) = %q carries a character systemd refuses in a unit name",
				identity, tag)
		}
		if len(daemonScopeUnitPrefix+CliVerbWorker+daemonScopeUnitPidSep+tag) > daemonUnitNameMaxLen {
			t.Errorf("daemonProfileUnitTag(%q) = %q makes an over-long unit name", identity, tag)
		}
	}
	// The cap is real: an absurd directory name is truncated rather than
	// allowed to push the unit name toward systemd's limit.
	long := daemonProfileUnitTag("/opt/" + strings.Repeat("z", 200))
	if len(long) > daemonProfileTailMaxLen+1+2*daemonProfileHashBytes {
		t.Errorf("daemonProfileUnitTag of a 200-character directory = %q (%d bytes), want the readable half "+
			"capped at %d", long, len(long), daemonProfileTailMaxLen)
	}
}

// TestDaemonProfileCanonicalPathResolvesWhatExists pins the canonicalisation
// that keeps two SPELLINGS of one profile from becoming two units: the deepest
// existing ancestor is resolved and the (usually not-yet-existing) remainder is
// re-appended, because EvalSymlinks on the whole path would simply fail.
func TestDaemonProfileCanonicalPathResolvesWhatExists(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real-home")
	if err := os.MkdirAll(filepath.Join(real, config.ProfileDirName), 0o755); err != nil {
		t.Fatalf("create the profile: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link-home")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	viaLink, err := filepath.Abs(filepath.Join(link, config.ProfileDirName))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	direct, err := filepath.Abs(filepath.Join(real, config.ProfileDirName))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if got := daemonProfileCanonicalPath(viaLink); got != direct {
		t.Errorf("daemonProfileCanonicalPath(%q) = %q, want %q — two spellings of one profile must produce "+
			"ONE unit name", viaLink, got, direct)
	}
	// A path whose leaf does not exist yet (the normal case at START time: the
	// profile directory is created later) still resolves its existing ancestor.
	missing := filepath.Join(link, config.ProfileDirName, "does", "not", "exist")
	wantMissing := filepath.Join(real, config.ProfileDirName, "does", "not", "exist")
	if got := daemonProfileCanonicalPath(missing); got != wantMissing {
		t.Errorf("daemonProfileCanonicalPath(%q) = %q, want %q", missing, got, wantMissing)
	}
}

// TestDaemonProfileIdentityFollowsGopiHome is the identity's own contract: it
// IS the profile's data root, so two GOPI_HOMEs are two identities and one
// GOPI_HOME is one identity (the property the operator's acceptance runs on).
func TestDaemonProfileIdentityFollowsGopiHome(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	t.Setenv(config.GopiDirEnv, homeA)
	identityA, ok := daemonProfileIdentity()
	if !ok {
		t.Fatalf("no identity for GOPI_HOME=%s", homeA)
	}
	if want := filepath.Join(homeA, config.ProfileDirName); identityA != want {
		t.Errorf("identity = %q, want the profile data root %q", identityA, want)
	}
	t.Setenv(config.GopiDirEnv, homeB)
	identityB, ok := daemonProfileIdentity()
	if !ok {
		t.Fatalf("no identity for GOPI_HOME=%s", homeB)
	}
	if identityA == identityB {
		t.Fatalf("two GOPI_HOMEs produced ONE identity %q — every unit-name assertion above rests on "+
			"this being a function of the profile", identityA)
	}
	if daemonProfileUnitTag(identityA) == daemonProfileUnitTag(identityB) {
		t.Errorf("two GOPI_HOMEs produced ONE profile tag")
	}
	// A blank GOPI_HOME means UNSET (the documented reading in config.GopiDir:
	// TrimSpace first), so it must resolve to the same profile as no GOPI_HOME
	// at all — never to an empty or invented identity.
	t.Setenv(config.GopiDirEnv, "   ")
	if got, ok := daemonProfileIdentity(); !ok || got != identityA {
		// identityA here is the HOME-based profile (GopiDirEnv was unset above
		// for the first two readings is NOT the case, so compare with the
		// user-home profile explicitly).
		userHome, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("no user home here: %v", err)
		}
		if want := filepath.Join(userHome, config.ProfileDirName); got != want || !ok {
			t.Errorf("a blank GOPI_HOME resolved to (%q, ok=%v), want the user-home profile %q (whitespace "+
				"means UNSET, per config.GopiDir)", got, ok, want)
		}
	}
	// An identity that CANNOT be resolved (no GOPI_HOME and no HOME to fall back
	// to) must be reported as unresolvable, never invented: two profiles sharing
	// an invented identity would collide — the defect this file exists to close.
	t.Setenv(config.GopiDirEnv, "")
	t.Setenv("HOME", "")
	if _, ok := daemonProfileIdentity(); ok {
		t.Error("with neither GOPI_HOME nor HOME, no identity can be resolved — reporting one would invent " +
			"a name two profiles could share")
	}
	if tag := daemonProfileUnitTagNow(); !strings.HasPrefix(tag, daemonProfileUnknownMark) {
		t.Errorf("the unresolvable-profile tag = %q, want it to say so with the %q mark", tag, daemonProfileUnknownMark)
	}
}

// TestDaemonScopeResolveExistingUnitAsksNothingWhenNotWrapped is the decision's
// first guard: mechanism ② and the degraded path never ask systemd for a unit
// name, so the decision must be a no-op for them (no systemctl call, no
// refusal).
func TestDaemonScopeResolveExistingUnitAsksNothingWhenNotWrapped(t *testing.T) {
	t.Setenv(config.GopiDirEnv, t.TempDir())
	for _, mode := range []string{daemonScopeModeCgfs, daemonScopeModeNone} {
		reused, detail, err := daemonScopeResolveExistingUnit(workerDaemonSpec(), CliVerbWorker,
			daemonScopeDecision{mode: mode, detail: "test"})
		if err != nil || reused || detail != "" {
			t.Errorf("mode %s: (reused=%v detail=%q err=%v), want (false, \"\", nil)", mode, reused, detail, err)
		}
	}
}

// TestDaemonAwaitReadyQuotesTheLogTail is the red proof for DEFECT 2. It drives
// the real handshake helper with a pipe whose writer is already closed (exactly
// what a child that died before reporting produces) and a log file holding the
// measured systemd refusal. The returned error must NAME that refusal —
// "(see the log): EOF" alone is the shape that made a 10-minute suite timeout
// read like a transport accident.
func TestDaemonAwaitReadyQuotesTheLogTail(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "merger.log")
	refusal := "Failed to start transient scope unit: Unit " + scopeUnitNameFor(CliVerbMerger, dsTagAlpha) + ".scope already exists."
	if err := os.WriteFile(logPath, []byte("banner line\n"+refusal+"\n"), 0o644); err != nil {
		t.Fatalf("write the daemon log: %v", err)
	}
	readyR, readyW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	// Closing the write end makes the read return EOF — the child died silently.
	if err := readyW.Close(); err != nil {
		t.Fatalf("close the write end: %v", err)
	}
	defer readyR.Close()

	verdict := daemonAwaitReady(readyR, logPath)
	if verdict == nil {
		t.Fatal("daemonAwaitReady reported success for a child that never wrote the ready byte")
	}
	got := verdict.Error()
	if !strings.Contains(got, daemonNoReportNote) {
		t.Errorf("verdict %q does not carry %q", got, daemonNoReportNote)
	}
	if !strings.Contains(got, refusal) {
		t.Errorf("verdict %q does not quote the log's cause %q — a start failure must NAME its cause "+
			"(Rule 15), not just report EOF", got, refusal)
	}
	// It must still carry the underlying error, so a caller can still tell EOF
	// from a read error.
	if !strings.Contains(got, "EOF") {
		t.Errorf("verdict %q dropped the underlying read error", got)
	}
}

// TestDaemonLogTailNoteReportsAnUnreadableLog is the no-silent-failure half
// (Rule 15b): a log that cannot be read must say so, never render as an empty
// tail that reads like "the log was read and held nothing".
func TestDaemonLogTailNoteReportsAnUnreadableLog(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-there.log")
	note := daemonLogTailNote(missing)
	if !strings.Contains(note, missing) || !strings.Contains(note, "could not be read") {
		t.Errorf("daemonLogTailNote(%q) = %q, want it to name the path and the read failure", missing, note)
	}
	empty := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write the empty log: %v", err)
	}
	if note := daemonLogTailNote(empty); !strings.Contains(note, "EMPTY") {
		t.Errorf("daemonLogTailNote(empty) = %q, want it to say the log is empty", note)
	}
	// A real tail keeps the END of the file (the cause), not the head.
	tailed := filepath.Join(t.TempDir(), "tailed.log")
	body := "head\n" + strings.Repeat("x", daemonLogTailBytes) + "\n" + tailString("THE-CAUSE", 9)
	if err := os.WriteFile(tailed, []byte(body), 0o644); err != nil {
		t.Fatalf("write the long log: %v", err)
	}
	note = daemonLogTailNote(tailed)
	if !strings.Contains(note, "THE-CAUSE") || strings.Contains(note, "head") {
		t.Errorf("daemonLogTailNote did not keep the log's TAIL (the cause): %q", note)
	}
	if !strings.Contains(note, tailed) {
		t.Errorf("the tail note must name the log it quotes, got %q", note)
	}
}

// TestDaemonScopeExistingUnitDecisionIsMeasured is the REAL-SYSTEMD half of the
// existing-unit decision: the two questions it asks (is the name taken? who
// holds it?) are MEASURED against a unit this test creates, because every one
// of them was got wrong once by reasoning instead of asking —
//
//   - `list-unit-files` prints a HEADER, so a line-count test reads a MISSING
//     unit as taken (measured: every start refused);
//   - `systemctl show -p X <unit>` without --property treats X as a UNIT NAME
//     and dumps every property of the real unit, so the answer is non-empty for
//     a name systemd has never heard of;
//   - the HOLDER of a transient SCOPE is NOT reachable through
//     `--property=MainPID` (measured empty on this host, systemd 249), which is
//     why this test asserts the holder through the unit's CGROUP. The
//     MainPID-based read it replaces did not fail — it could only ever answer
//     "not ours", a silent no-op, and the test that pinned it SKIPPED instead
//     of failing.
//
// It also pins the property the reuse half rests on: a holder that is NOT this
// profile's daemon is never claimed as ours.
func TestDaemonScopeExistingUnitDecisionIsMeasured(t *testing.T) {
	if err := probeSystemdRun(); err != nil {
		t.Skipf("no usable systemd user bus here: %v", err)
	}
	t.Setenv(config.GopiDirEnv, t.TempDir())
	// A name of this test's OWN (never a profile-derived one): what is under
	// test is the QUESTION, not the naming.
	unitName := daemonScopeUnitPrefix + CliVerbWorker + daemonScopeUnitPidSep +
		"probe" + daemonUnitPartSep + strconv.Itoa(os.Getpid())
	taken, err := daemonScopeUnitExists(unitName)
	if err != nil {
		t.Fatalf("the existing-unit question must be answerable: %v", err)
	}
	if taken {
		t.Fatalf("systemd reports %s as taken before anything created it — a unit name nobody holds "+
			"would refuse every start", unitName)
	}
	launcher := daemonProbeStartScope(t, unitName, probeHoldProcessName, probeHoldSecondsArg)
	daemonProbeAwaitUnit(t, unitName, launcher)
	// THE BUS IS PART OF THE QUESTION (measured, and the defect that made this
	// decision inert): the unit is a TRANSIENT USER scope, so only the USER
	// manager knows it — the SYSTEM bus answers "not-found" for it even while
	// it is alive. THE ASSERTION BELOW IS THE RED PROOF for that defect: if
	// production drops `--user` again, daemonScopeUnitLoadState asks the system
	// manager, gets "not-found" for this LIVE unit, and this test fails here
	// instead of the decision quietly reading "the name is free".
	userAnswer, err := daemonScopeUnitLoadState(unitName)
	if err != nil {
		t.Fatalf("the user manager must answer for %s: %v", unitName, err)
	}
	if userAnswer == daemonLoadStateNotFound {
		t.Fatalf("the user manager reports %s as %q while systemd has just started it — "+
			"the existing-unit question was asked on the WRONG BUS (the system manager answers "+
			"not-found for every transient user scope, a live one included), which makes the "+
			"whole decision inert", unitName, userAnswer)
	}
	systemOut, _ := exec.Command(systemctlPath, systemctlShowArg,
		systemctlPropertyFlag, systemctlPropertyLoadState, systemctlValueFlag,
		unitName+daemonUnitFileSuffix).Output() //stylegate:ignore probe reading of the unit's LoadState; a failure leaves it empty and the comparison below reports it
	// The two buses are compared as INFORMATION (the system manager's answer for
	// a user scope is not this repo's contract — a future systemd could change
	// it), but the MEANING is asserted: whatever the system bus says, it is not
	// the answer the user manager gave for a unit only the user manager knows.
	if got := strings.TrimSpace(string(systemOut)); got == userAnswer {
		t.Logf("note: the SYSTEM bus also reports %q for this live user scope on this host — "+
			"on such a host the --user flag would not be observable through this pair", got)
	}

	// THE HOLDER: found through the unit's cgroup (the measured-correct route),
	// and it is the process systemd placed there — not the `systemd-run` that
	// created the scope, which waits OUTSIDE it.
	holderPids, err := daemonUnitHolderPids(unitName)
	if err != nil {
		t.Fatalf("a live LIVE scope must have an identifiable holder: %v", err)
	}
	if len(holderPids) == 0 {
		t.Fatalf("the cgroup of the live scope %s listed no process", unitName)
	}
	if comm := daemonProcessComm(holderPids[0]); comm != daemonProbeHoldComm {
		t.Errorf("the holder of %s reports comm %q, want %q — the pid read was not the process systemd placed in the scope",
			unitName, comm, daemonProbeHoldComm)
	}
	// AND systemd's OWN pid property is NOT that holder, on this host: asserting
	// it keeps the two routes distinguishable, so a future systemd that starts
	// reporting a scope's MainPID is a MEASURED change rather than a silent
	// behaviour swap. It is reported, not failed: the host's systemd version is
	// not this repo's contract.
	t.Logf("measured on this host: MainPID of a live scope %s = %q (the holder is pid %d, from cgroup.procs)",
		unitName, daemonScopeUnitMainPIDText(unitName), holderPids[0])

	// The holder is NOT this profile's daemon (%s runs /bin/sleep, which carries
	// no profile stamp at all), so the decision must refuse rather than reuse.
	if pid, ok := daemonScopeSameProfileDaemon(unitName); ok {
		t.Errorf("a FOREIGN holder (pid %d) was claimed as this profile's own daemon — that is how a start "+
			"becomes a silent no-op", pid)
	}
}

// TestDaemonScopeReuseRecognisesThisProfilesOwnHolder is the OTHER half, and the
// one that must never be over-eager: the holder IS this profile's own daemon
// (it carries the profile stamp daemonScopeWrap writes), so the decision reuses
// instead of refusing. The holder here is a plain `sleep` wearing exactly the
// stamp the wrap writes — the decision reads the daemon's OWN statement of its
// profile, which is what makes it exact and is what makes the DEFAULT profile
// (whose processes have no GOPI_HOME) work at all.
func TestDaemonScopeReuseRecognisesThisProfilesOwnHolder(t *testing.T) {
	if err := probeSystemdRun(); err != nil {
		t.Skipf("no usable systemd user bus here: %v", err)
	}
	home := t.TempDir()
	t.Setenv(config.GopiDirEnv, home)
	identity, ok := daemonProfileIdentity()
	if !ok {
		t.Fatalf("no identity for GOPI_HOME=%s", home)
	}
	unitName := daemonScopeUnitPrefix + CliVerbWorker + daemonScopeUnitPidSep +
		"reuse" + daemonUnitPartSep + strconv.Itoa(os.Getpid())
	// The stamp goes to `env` as an ARGUMENT — a real environment on the holder
	// process, which is what daemonProcessEnvVar reads back. This test is about
	// READING the stamp, not about how it got there.
	launcher := daemonProbeStartScope(t, unitName,
		daemonProbeEnvCommand, daemonScopeProfileEnv+"="+identity,
		probeHoldProcessName, probeHoldSecondsArg)
	daemonProbeAwaitUnit(t, unitName, launcher)
	deadline := time.Now().Add(systemctlQueryBudget)
	for {
		if pid, ok := daemonScopeSameProfileDaemon(unitName); ok {
			if pid <= 0 {
				t.Errorf("the reuse must name the holder's pid, got %d", pid)
			}
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("a holder carrying THIS profile's stamp (%s=%s) must be recognised as this profile's "+
				"own daemon — otherwise a start refuses a daemon that is already there (%s)",
				daemonScopeProfileEnv, identity, daemonProbeHolderEvidence(t, unitName))
		}
		time.Sleep(daemonProbePollInterval) //stylegate:allow-sleep bounded wait for systemd to report the probe unit
	}
}

// daemonProbeHolderEvidence describes what the unit actually holds, for a
// failure message that names the cause instead of asserting a bare "not found"
// (Rule 15). It never fails the test by itself: it is the evidence a real
// failure needs.
func daemonProbeHolderEvidence(t *testing.T, unitName string) string {
	t.Helper()
	pids, err := daemonUnitHolderPids(unitName)
	if err != nil {
		return fmt.Sprintf("the holder itself could not be identified: %v", err)
	}
	report := fmt.Sprintf("holders=%v", pids)
	for _, pid := range pids {
		stamp, err := daemonProcessEnvVar(pid, daemonScopeProfileEnv)
		report += fmt.Sprintf(" pid %d: %s=%q (%v)", pid, daemonScopeProfileEnv, stamp, err)
	}
	return report
}
