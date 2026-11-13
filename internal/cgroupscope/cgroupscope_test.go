package cgroupscope

// cgroupscope_test.go — the unit half of the epic daemon-cgroup-detach proof.
//
// The EPIC'S OWN VERDICT IS MADE BY THE LIVE HOST, not here (see
// cmd/agent/daemon_cgroup_e2e_test.go, which asserts /proc/<daemon pid>/cgroup
// against a frozen committed path). What this file pins is the part that must
// hold on EVERY host, in particular:
//
//   - the ONLY accepted evidence is a fresh /proc/<pid>/cgroup read: the
//     classification helpers that decide "is this a login session scope?" are
//     pure and are tested against the exact strings measured on this host,
//   - a failed escape is NEVER a silent success (ErrNoEscape), and the error
//     says what was attempted,
//   - the off-switch is explicit and reported, not an accident.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// The measured cgroup shapes this host really produces. They are FIXTURES, not
// guesses: every path below was read out of this host's own cgroup tree (see
// the report's Evidence section for the commands).
const (
	// sessionScopeOfUD is an ssh login's session scope — the epic's 2026-09-13
	// incident shape, a *.scope directly under the per-uid slice.
	sessionScopeOfUD = "/user.slice/user-1000.slice/session-18200.scope"
	// desktopSessionScopeOfUD is what a GNOME desktop session really is on this
	// host: an app scope inside app.slice, under the MANAGER. systemd created it
	// AS a session, so it is a session scope like any other.
	desktopSessionScopeOfUD = "/user.slice/user-1000.slice/user@1000.service/app.slice/" +
		"app-gnome-gnome\x2dsession\x2dquit-34290.scope"
	// terminalScopeOfUD is where THIS agent shell lives — a terminal spawned by
	// the desktop's gnome-terminal server (read from /proc/self/cgroup on this
	// host). It is the path the off-switch case of epic daemon-cgroup-detach
	// measured a correctly-un-escaped daemon in, and the path the OLD predicate
	// got wrong: the terminal is not a direct child of the per-uid slice, so a
	// depth-based rule answered "not a session" while the shell the operator
	// types in — and everything it starts — dies with the session's teardown.
	terminalScopeOfUD = "/user.slice/user-1000.slice/user@1000.service/app.slice/" +
		"app-org.gnome.Terminal.slice/vte-spawn-b4078f3c-7005-4309-80b9-9ed8ea10f798.scope"
	// containerSessionScope is a session `machinectl shell` opened into a running
	// container: systemd names its scope after the machine, and its teardown
	// ends that session exactly as session-N.scope's does for a host login.
	containerSessionScope = "/machine.slice/machine-lxc\\x2dpayload.slice/payload/" +
		"system.slice/lxc-payload-scope-1.scope"
)

func TestInSessionScope_RecognizesASessionScope(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		{sessionScopeOfUD, true, "the measured incident shape: an ssh login's session scope"},
		{"/user.slice/user-1000.slice/session-4.scope", true, "another session number"},
		{"/user.slice/user-1000.slice/session-4.scope/nested", true,
			"a child cgroup of a session scope IS a member of it: the nearest OWNING ancestor is the scope, so its teardown takes the child with it"},
		{"/user.slice/user-1001.slice/session-4.scope", true, "another user's session scope is still a session scope"},
		{containerSessionScope, true,
			"a container session's scope (`machinectl shell` into a running machine) is the session the payload process dies with"},
		{terminalScopeOfUD, true,
			"THE HOST SHAPE — a terminal scope under the desktop: this is where this host's agent shell really lives, and the off-switch case measured a correctly-un-escaped daemon here"},
		{terminalScopeOfUD + "/nested", true,
			"a process one level below a terminal scope is still a member of that terminal's session"},
		{desktopSessionScopeOfUD, true,
			"a GNOME desktop session scope: the desktop session is what a terminal's processes descend from"},
		{"/user.slice/user-1000.slice/user@1000.service/app.slice/app-gnome-wechat-2756616.scope", true,
			"a GUI application scope: systemd created it as a member of the desktop session, which is what tears it down"},
		{"/user.slice/user-1000.slice/user@1000.service", false, "the user manager is not a login session"},
		{"/user.slice/user-1000.slice/user@1000.service/gopi-daemon", false,
			"our own escape target, directly under the manager"},
		{"/user.slice/user-1000.slice/user@1000.service/app.slice/gopi-daemon-worker-001-abcd.scope", false,
			"the escape target as mechanism ① leaves it: a transient scope under app.slice — a scope, but OURS, and the whole point of the escape"},
		{"/user.slice/user-1000.slice/user@1000.service/app.slice", false,
			"app.slice under the manager holds no scope of its own: the walk climbs through it to the boundary"},
		{"/user.slice/user-1000.slice/user@1000.service/session.slice", false,
			"the manager's own session.slice, for the same reason"},
		{"/user.slice/user-1000.slice/user@1000.service/init.scope", false,
			"systemd's own manager scope: it hangs directly off the manager, so the walk steps over it and stops at the per-uid slice above"},
		{"/system.slice/ssh.service", false, "a system service"},
		{"/user.slice/user-1000.slice/session-4.slice", false, "a .slice is not a .scope"},
		{"/user.slice/user-1000.slice", false, "the per-uid slice is the login machinery itself, never a session"},
		{"/user.slice", false, "a slice is not a scope"},
		{"/", false, "the cgroup root is nobody's session"},
		{"user.slice/user-1000.slice/session-1.scope", false, "a RELATIVE path is not a cgroup membership line"},
		{"", false, "empty input is never a hit"},
	}
	for _, c := range cases {
		if got := InSessionScope(c.rel); got != c.want {
			t.Errorf("InSessionScope(%q) = %v, want %v (%s)", c.rel, got, c.want, c.why)
		}
	}
}

// TestInSessionScope_StopsAtTheLoginMachinery pins the ONE boundary the walk must
// not climb past: the per-uid slice, above which no login session of this user
// lives. Everything the login machinery holds — the manager itself, its own
// `init.scope`, and OUR escape target under it — is OUTSIDE the login sessions.
// That is the property every verdict in this package rests on, and the shape a
// correct systemd escape always produces.
//
// WHY IT IS A TEST OF ITS OWN: a walk that lost its stop condition would still
// classify every session scope correctly and would betray itself on the one
// shape that matters most — a daemon that HAS escaped, dragged up the slice
// above it and reported as "still in a session". The compound table above would
// catch that too; this case names the boundary directly, so a failure says WHICH
// half moved.
func TestInSessionScope_StopsAtTheLoginMachinery(t *testing.T) {
	for _, rel := range []string{
		"/user.slice/user-1000.slice/user@1000.service",
		"/user.slice/user-1000.slice/user@1000.service/gopi-daemon",
		"/user.slice/user-1000.slice/user@1000.service/init.scope",
		"/user.slice/user-1000.slice",
		"/user.slice",
	} {
		if InSessionScope(rel) {
			t.Errorf("InSessionScope(%q) = true: the login machinery — and our own escape target — are OUTSIDE the login session scopes", rel)
		}
	}
	// The non-regression half: a login session scope is still found at every
	// depth a host spells it at — including this host's own.
	for _, rel := range []string{
		sessionScopeOfUD,
		"/user.slice/user-1001.slice/session-4.scope",
		containerSessionScope,
		terminalScopeOfUD,
		desktopSessionScopeOfUD,
	} {
		if !InSessionScope(rel) {
			t.Errorf("InSessionScope(%q) = false: a login session scope must be recognised at any depth", rel)
		}
	}
}

func TestSelfRel_ReadsTheUnifiedHierarchyLine(t *testing.T) {
	rel, err := SelfRel()
	if err != nil {
		t.Fatalf("SelfRel on this host: %v", err)
	}
	if !strings.HasPrefix(rel, "/") {
		t.Errorf("SelfRel = %q, want an absolute cgroup path", rel)
	}
	// The value has to be the SAME string a direct read yields: SelfRel is the
	// only accepted verdict source, so a divergence between it and procfs would
	// make every verdict a fiction.
	raw, err := os.ReadFile(filepath.Join(ProcRoot, "self", ProcSelfCgroup))
	if err != nil {
		t.Fatalf("read /proc/self/cgroup: %v", err)
	}
	if !strings.Contains(string(raw), SelfCgroupPrefix+rel) {
		t.Errorf("SelfRel() = %q but /proc/self/cgroup reads %q", rel, string(raw))
	}
}

func TestRelOfPID_MissingPidIsAnError(t *testing.T) {
	// pid 0 is never a readable /proc entry — the probe must report the read
	// failure rather than fabricating a path.
	if _, err := RelOfPID(0); err == nil {
		t.Fatal("RelOfPID(0) must fail: a fabricated cgroup path would be a fake verdict")
	}
	var re *errReadError
	if _, err := RelOfPID(0); !errors.As(err, &re) {
		t.Errorf("RelOfPID error must be a read error, got %T", err)
	}
}

func TestManagerRel_IsTheUserManagerUnderTheUidSlice(t *testing.T) {
	got := ManagerRel(1000)
	want := "/user.slice/user-1000.slice/user@1000.service"
	if got != want {
		t.Errorf("ManagerRel(1000) = %q, want %q", got, want)
	}
	if InSessionScope(got) {
		t.Error("the escape target must NOT be classified as a session scope")
	}
}

func TestOff_ReadsTheOffSwitch(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes"} {
		testutil.SnapshotEnv(t, EnvOff)()
		t.Setenv(EnvOff, v)
		if !Off() {
			t.Errorf("Off() = false with %s=%s", EnvOff, v)
		}
	}
	testutil.SnapshotEnv(t, EnvOff)()
	t.Setenv(EnvOff, "0")
	if Off() {
		t.Errorf("Off() = true with %s=0", EnvOff)
	}
}

func TestEnter_OffSwitchIsAHardRefusal(t *testing.T) {
	testutil.SnapshotEnv(t, EnvOff)()
	t.Setenv(EnvOff, "1")
	_, err := Enter(os.Getuid(), "gopi-probe-off")
	if err == nil {
		t.Fatal("Enter must refuse when the escape is disarmed")
	}
	if !errors.Is(err, ErrNoEscape) {
		t.Errorf("Enter's refusal must wrap ErrNoEscape, got %v", err)
	}
}

// TestEnter_VerdictComesFromProcNotFromTheWrite is the anti-fake-success guard:
// whether the move succeeds or fails on this host, the returned verdict must
// AGREE with a fresh /proc/self/cgroup read. A success is accepted only when
// procfs says so; a failure is reported only when procfs still names the old
// cgroup.
func TestEnter_VerdictComesFromProcNotFromTheWrite(t *testing.T) {
	if Off() {
		t.Skipf("%s is set in this environment", EnvOff)
	}
	before, err := SelfRel()
	if err != nil {
		t.Skipf("cannot read this process's cgroup: %v", err)
	}
	sc, escErr := Enter(os.Getuid(), "gopi-probe-verdict")
	after, readErr := SelfRel()
	if readErr != nil {
		t.Fatalf("reading the cgroup after a migration attempt must work: %v", readErr)
	}
	if escErr == nil {
		if after != sc.Rel {
			t.Fatalf("Enter reported success at %q but /proc/self/cgroup reads %q — a write() return is never the verdict", sc.Rel, after)
		}
		if InSessionScope(after) {
			t.Fatalf("a successful escape left this process in a session scope (%s)", after)
		}
		return
	}
	// The escape failed — which is the honest outcome on a host without the
	// delegation. The FAILURE must be proven by procfs, not assumed.
	if !errors.Is(escErr, ErrNoEscape) {
		t.Fatalf("a failed escape must wrap ErrNoEscape, got %v", escErr)
	}
	if after != before {
		t.Fatalf("Enter failed (%v) yet the cgroup changed %q → %q: the two disagree", escErr, before, after)
	}
}

// TestWritable_AccessesInsteadOfOpeningTheDirectory is the RED regression guard
// for the fabricated verdict (epic "merger restart giving some warning/error
// message", 2026-09-13).
//
// THE DEFECT: the permission probe opened the target O_WRONLY, which for a
// DIRECTORY can only ever fail with EISDIR. Wired into Available() it printed
// "cgroup scope escape unavailable: … open …: is a directory" while mkdir in
// the very same subtree SUCCEEDED — a host-independent LIE, since the probe
// could not return nil for any directory on any host.
//
// The test is deliberately host-independent: t.TempDir() is a directory this
// process owns and can write, so access(2) MUST say yes — and the old probe's
// shape (open it O_WRONLY) MUST still fail, which is what pins the fix to the
// probe rather than to the host's delegation state.
func TestWritable_AccessesInsteadOfOpeningTheDirectory(t *testing.T) {
	dir := t.TempDir()

	if err := Writable(dir); err != nil {
		t.Fatalf("Writable(%s) = %v, want nil: a writable directory must be reported writable", dir, err)
	}
	// The exact shape that produced the defect — kept here as the counter-proof
	// that this test really discriminates the two probes.
	f, openErr := os.OpenFile(dir, os.O_WRONLY, 0)
	if openErr == nil {
		_ = f.Close() // deliberately ignored: best-effort close of a probe handle we already refuse to use
		t.Fatalf("os.OpenFile(%s, O_WRONLY) succeeded — this host cannot discriminate the two probes, the guard proves nothing", dir)
	}
	if !errors.Is(openErr, syscall.EISDIR) {
		t.Fatalf("os.OpenFile(%s, O_WRONLY) = %v, want EISDIR (the fabricated reason's carrier)", dir, openErr)
	}
}

// TestWritable_RefusesWhatTheProcessCannotWrite is the other half: a directory
// mode 0 is not writable to anyone, and the probe must say so with the path in
// the message (the operator's only handle on the problem).
func TestWritable_RefusesWhatTheProcessCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("uid 0 may write a 0-mode directory — the refusal cannot be exercised as root")
	}
	dir := filepath.Join(t.TempDir(), "sealed")
	if err := os.Mkdir(dir, 0o000); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	err := Writable(dir)
	if err == nil {
		t.Fatalf("Writable(%s) = nil, want a refusal", dir)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("the refusal must name the path it refused, got %v", err)
	}
}

// TestAvailable_ReasonIsNotTheFabricatedEISDIR pins the OUTCOME the operator
// reads. On a host where no escape is possible the reason must describe the
// permission that is actually missing, and it must never be the open-a-
// directory artefact — that string is the bug report this epic started from.
func TestAvailable_ReasonIsNotTheFabricatedEISDIR(t *testing.T) {
	if Off() {
		t.Skipf("%s is set in this environment", EnvOff)
	}
	err := Available(os.Getuid())
	if err == nil {
		t.Skip("this host can escape the session scope — there is no unavailable reason to inspect")
	}
	msg := err.Error()
	if strings.Contains(msg, "is a directory") {
		t.Errorf("the unavailable reason carries the fabricated open-a-directory artefact: %v", err)
	}
	if !errors.Is(err, ErrNoEscape) {
		t.Errorf("an unavailable verdict must wrap ErrNoEscape, got %v", err)
	}
	if !strings.Contains(msg, cgroupscopeRootErrFragment()) {
		t.Errorf("the reason must name the path whose permission is missing (%s), got %v", cgroupscopeRootErrFragment(), err)
	}
}

// cgroupscopeRootErrFragment is the one fragment every unavailable reason on
// this host must carry: the cgroup root the paths are resolved under. It is a
// helper (not a literal at the assertion) so the expectation is written once.
func cgroupscopeRootErrFragment() string { return Root }

// TestAvailable_AgreesWithEnter pins the diagnostic against the action: when
// Available says the escape works, Enter must succeed, and when it says it does
// not, Enter must fail. A diagnostic that disagrees with the action is how an
// operator ends up chasing a host problem that is not there.
func TestAvailable_AgreesWithEnter(t *testing.T) {
	if Off() {
		t.Skipf("%s is set in this environment", EnvOff)
	}
	availErr := Available(os.Getuid())
	sc, enterErr := Enter(os.Getuid(), "gopi-probe-agree")
	if availErr == nil && enterErr != nil {
		t.Errorf("Available said the escape works but Enter failed: %v", enterErr)
	}
	if availErr != nil && enterErr == nil {
		t.Errorf("Available said the escape is unavailable (%v) but Enter succeeded at %q", availErr, sc.Rel)
	}
}

// TestAvailable_DoesNotMoveTheCaller is the safety contract of the oracle
// (Rule: a successful migration is IRREVERSIBLE without a second migration).
// Available is a PREDICTION, so asking it must leave the caller exactly where it
// was — otherwise a diagnostic would be a side effect, and a probe that reported
// "success then failure" would have already spent the only migration it had.
//
// The check is a before/after read of the ONLY accepted evidence, on both
// verdicts: whether Available says yes or no, /proc/self/cgroup must be
// byte-identical afterwards, and no escape directory may have appeared from it.
func TestAvailable_DoesNotMoveTheCaller(t *testing.T) {
	if Off() {
		t.Skipf("%s is set in this environment", EnvOff)
	}
	before, err := SelfRel()
	if err != nil {
		t.Skipf("cannot read this process's cgroup: %v", err)
	}
	target, err := escapeTarget(os.Getuid(), "")
	if err != nil {
		t.Fatalf("escapeTarget: %v", err)
	}
	_, statErrBefore := os.Stat(target.Dir)

	availErr := Available(os.Getuid())

	after, err := SelfRel()
	if err != nil {
		t.Fatalf("reading the cgroup after Available must work: %v", err)
	}
	if after != before {
		t.Fatalf("Available moved the caller: %q → %q (verdict %v) — a prediction must not perform the migration", before, after, availErr)
	}
	_, statErrAfter := os.Stat(target.Dir)
	if (statErrBefore == nil) != (statErrAfter == nil) {
		t.Errorf("Available changed the filesystem: %s went from (%v) to (%v) — the probe must not create the target either", target.Dir, statErrBefore, statErrAfter)
	}
}

// TestCommonAncestor_IsTheLowestSharedCgroup pins the value the kernel's
// migration permission is actually checked against. The two rows that matter on
// this host are the SAME target reached from two different positions: from
// inside the user manager the shared cgroup is the manager (writable), from a
// sibling session scope it is the per-uid slice (root-owned).
func TestCommonAncestor_IsTheLowestSharedCgroup(t *testing.T) {
	manager := ManagerRel(1000)
	target := manager + "/gopi-daemon"
	session := "/user.slice/user-1000.slice/session-3452.scope"
	cases := []struct {
		a, b, want, why string
	}{
		{manager + "/app.slice/vte-spawn-x.scope", target, manager,
			"the measured in-manager case: the manager is shared, the slice is NOT reached"},
		{session, target, "/user.slice/user-1000.slice",
			"the measured ssh case: the two are siblings, so the slice is the lowest shared cgroup"},
		{target, target, target, "a migration to itself shares everything"},
		{session, "/user.slice/user-1001.slice/session-1.scope", "/user.slice",
			"another uid's session shares only the slice root of user.slice"},
		{"/system.slice/sshd.service", target, "/", "unrelated subtrees share the cgroup root"},
		{"", target, "/", "an unreadable position resolves to the root, never to the empty path"},
	}
	for _, c := range cases {
		if got := commonAncestor(c.a, c.b); got != c.want {
			t.Errorf("commonAncestor(%q, %q) = %q, want %q (%s)", c.a, c.b, got, c.want, c.why)
		}
	}
}

// TestTargetMayAdopt_ReadsSubtreeControlNotTheChildTree pins the kernel veto
// that sits BEHIND the permission gate, on a fixture whose shape is the one
// measured on this host — so the expectation is host-independent:
//
//	empty dir                            → admitted (fresh target)
//	controller enabled, child empty      → refused (measured EBUSY)
//	child POPULATED, no controller       → admitted (measured; the naive reading
//	                                       of "no internal process" gets this
//	                                       backwards)
func TestTargetMayAdopt_ReadsSubtreeControlNotTheChildTree(t *testing.T) {
	fresh := t.TempDir()
	if ok, why, err := targetMayAdopt(fresh); err != nil || !ok {
		t.Errorf("targetMayAdopt(fresh) = (%v, %q, %v), want admitted", ok, why, err)
	}

	delegating := t.TempDir()
	if err := os.Mkdir(filepath.Join(delegating, "kid"), dirMode); err != nil {
		t.Fatalf("mkdir kid: %v", err)
	}
	if err := os.WriteFile(filepath.Join(delegating, SubtreeControlFileName), []byte("memory pids\n"), 0o644); err != nil {
		t.Fatalf("write subtree_control: %v", err)
	}
	ok, why, err := targetMayAdopt(delegating)
	if err != nil {
		t.Fatalf("targetMayAdopt(delegating) error: %v", err)
	}
	if ok {
		t.Errorf("a target with %q enabled must be refused, got admitted", why)
	}
	if !strings.Contains(why, "memory") {
		t.Errorf("the refusal must name the measured controllers, got %q", why)
	}

	populated := t.TempDir()
	kid := filepath.Join(populated, "kid")
	if err := os.Mkdir(kid, dirMode); err != nil {
		t.Fatalf("mkdir kid: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kid, ProcsFileName), []byte("4242\n"), 0o644); err != nil {
		t.Fatalf("write kid cgroup.procs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(populated, SubtreeControlFileName), nil, 0o644); err != nil {
		t.Fatalf("write empty subtree_control: %v", err)
	}
	if ok, why, err := targetMayAdopt(populated); err != nil || !ok {
		t.Errorf("a populated CHILD must not be the veto (measured: the adoption succeeded), got (%v, %q, %v)", ok, why, err)
	}
}

// TestTargetMayAdopt_MissingTargetIsAdmitted is the one not-exist case that is
// NOT an error: the escape target is created by the operation itself, so its
// absence is the normal state of a first daemon start.
func TestTargetMayAdopt_MissingTargetIsAdmitted(t *testing.T) {
	ok, _, err := targetMayAdopt(filepath.Join(t.TempDir(), "not-created-yet"))
	if err != nil {
		t.Fatalf("a target that does not exist yet must not be an error, got %v", err)
	}
	if !ok {
		t.Error("a target that does not exist cannot be holding a controller — it must be admitted")
	}
}

// TestMigrationProtocol_NamesTheGateTheVerdictWasMadeOn pins the sentence an
// operator reads next to a verdict: it must carry the shared ancestor, because
// that — not the target and not the slice — is the cgroup whose mode decided.
// A verdict without its protocol is a number without a unit.
func TestMigrationProtocol_NamesTheGateTheVerdictWasMadeOn(t *testing.T) {
	src, err := SelfRel()
	if err != nil {
		t.Skipf("cannot read this process's cgroup: %v", err)
	}
	got, err := MigrationProtocol(os.Getuid(), "gopi-probe-protocol")
	if err != nil {
		t.Fatalf("MigrationProtocol: %v", err)
	}
	want := commonAncestor(src, ManagerRel(os.Getuid())+"/gopi-probe-protocol")
	if !strings.Contains(got, want) {
		t.Errorf("the protocol must name the shared ancestor %s, got %q", want, got)
	}
	if !strings.Contains(got, src) {
		t.Errorf("the protocol must name the source position %s, got %q", src, got)
	}
}

// TestNaiveSliceCheckIsNotTheMigrationGate is the RED PROOF, kept as a guard.
//
// THE DEFECT (epic 01a12c31, measured 2026-10-12): Available asked
// access(per-uid slice) unconditionally. From this run's position — inside
// user@1000.service — the slice is root:root 0755, so the check returned
// EACCES while the migration to a sibling directory under the manager SUCCEEDED
// and verified. Restoring that check makes TestAvailable_AgreesWithEnter fail
// again (measured: "Available said the escape is unavailable … but Enter
// succeeded").
//
// This case pins the discrimination itself: on a host where the migration works
// from the caller's position but the slice is not writable, Available MUST
// follow the migration. On a host where the two questions coincide (the ssh
// world: the caller is a sibling of the manager, so the slice IS the real gate)
// there is nothing to discriminate and the case skips with a printed reason
// rather than passing vacuously.
func TestNaiveSliceCheckIsNotTheMigrationGate(t *testing.T) {
	if Off() {
		t.Skipf("%s is set in this environment", EnvOff)
	}
	sliceErr := Writable(SliceDir(os.Getuid()))
	sc, enterErr := Enter(os.Getuid(), "gopi-probe-naive")
	if enterErr != nil || sliceErr == nil {
		t.Skipf("this host does not discriminate the two questions (slice=%v, Enter=%v): the guard needs a caller INSIDE user@<uid>.service whose slice mode is not writable — the position measured on 2026-10-12",
			sliceErr, enterErr)
	}
	if err := Available(os.Getuid()); err != nil {
		t.Errorf("the naive slice check refuses here (%v) yet the migration works — Available must follow the migration, not the slice: %v", sliceErr, err)
	}
	t.Logf("red proof: access(%s) = %v (refuses) while Enter migrated and verified at %s", SliceDir(os.Getuid()), sliceErr, sc.Rel)
}
