package main

// daemon_userbus_probe_test.go — the DISCRIMINATOR's cases (epic: user-bus
// diagnosis, 2026-09-15).
//
// WHAT IS PINNED HERE, and why each case is worth a test of its own:
//
//  1. THREE SYNTHETIC STATES → THREE DISTINCT REASONS. The defect was that one
//     blended sentence covered three host states with three different repairs;
//     a test that only checked "a reason is printed" would pass on the blended
//     message. So the three observations are built by hand (no host in that
//     state is needed) and their reasons, verdicts and next steps are compared
//     for DISTINCTNESS.
//  2. THE RED PROOF: the unlinked state (②) must not classify as "no user
//     manager" (①). It is the state this host is in, and it is the one the
//     blended message hides — the operator reading "there is no user manager"
//     would try to create one, which does not fix an unlinked socket path.
//     The proof is written as a contradiction: the SAME path observation, once
//     with a live manager and once without, must produce two different reasons
//     — so a classifier that ignored liveness, or one that ignored the path,
//     fails it.
//  3. The measurement must be EVIDENCE, not prose: the unlinked line must carry
//     the kernel binding (inode + LISTEN), the connect errno (ENOENT) and the
//     confirming commands.
//  4. The POSITIVE CONTROL: the unlinked state is produced for real (bind a
//     socket, unlink its path, observe it) — a predicate that never fires on a
//     state it claims to detect is a blind gate.
//  5. The LIVE case: whatever state THIS host is in, the printed diagnosis must
//     agree with the state a direct probe measures (both worlds, never a
//     hardcoded expectation of this machine).

import (
	"bufio"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// ubTestManagerUnit is a manager unit name for the fixture uid.
	ubTestManagerUnit = "user@1000.service"
	// ubTestPrivatePath / ubTestBusPath are socket paths shaped like a real
	// session's ("/run/user/1000/systemd/private", "/run/user/1000/bus").
	ubTestPrivatePath = "/run/user/1000/systemd/private"
	ubTestBusPath     = "/run/user/1000/bus"
	// ubTestInode is the kernel inode of the fixture's bound socket — the number
	// an operator matches in /proc/<pid>/fd, so it must survive into the output.
	ubTestInode = 40136
	// ubTestRuntimeDir is the fixture's runtime directory.
	ubTestRuntimeDir = "/run/user/1000"
	// ubSocketPathMaxBytes caps a fixture's socket path: AF_UNIX's sun_path is
	// 108 bytes INCLUDING its terminator, and bind(2) answers EINVAL (not
	// ENAMETOOLONG) when the path does not fit — see ubScratchSocketPath.
	ubSocketPathMaxBytes = 108
	// ubScratchDirPrefix is the fixture directory's name. It is SHORT on
	// purpose: the suite's scratch root can itself be deeply nested on a host
	// that pins it into a tmpfs, and every byte of this name is one the socket
	// path cannot afford (measured: 108 bytes ⇒ bind: invalid argument).
	ubScratchDirPrefix = "ubd"
	// ubTestRowInode is the inode the LISTENing fixture row carries.
	ubTestRowInode = 17620230
	// ubListenRow / ubClientRow are two rows of the kernel's own table, copied
	// verbatim from /proc/net/unix on kernel 6.8.0-138-generic, both carrying
	// ONE path. The LISTEN row is the bound listener; the St=02 row is what a
	// single client connect() adds, and the kernel places it FIRST — the
	// interleaving a first-match lookup reads as "no listener here"
	// (userBusUnixBinding has the measurement).
	ubListenRow = "0000000000000000: 00000002 00000000 00010000 0001 01 " +
		"17620230 " + ubTestBusPath
	ubClientRow = "0000000000000000: 00000002 00000000 00000000 0001 02 " +
		"0 " + ubTestBusPath
)

// ubAbsentErr is a REAL stat(2) ENOENT (from the filesystem, not a synthesized
// error value), so the classifier is exercised against the same error SHAPE it
// meets on a host: a *fs.PathError wrapping syscall.ENOENT.
func ubAbsentErr(t *testing.T) error {
	t.Helper()
	_, err := os.Stat(filepath.Join(t.TempDir(), "absent-socket"))
	if err == nil {
		t.Fatal("stat of a nonexistent path returned nil — the fixture is wrong")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the fixture error is not ENOENT: %v", err)
	}
	return err
}

// ubUnlinkedPath is the fixture for state ②: the path is ABSENT from the VFS
// and the kernel still holds the socket (bound, LISTEN, with an inode), and a
// real client's connect returns ENOENT.
func ubUnlinkedPath(t *testing.T, path, kind string) userBusPathObservation {
	t.Helper()
	return userBusPathObservation{
		path:         path,
		kind:         kind,
		statErr:      ubAbsentErr(t),
		bound:        true,
		listen:       true,
		inode:        ubTestInode,
		connectErr:   &net.OpError{Op: "dial", Net: userBusUnixNetwork, Err: syscall.ENOENT},
		connectErrno: syscall.ENOENT,
	}
}

// ubStalePath is the fixture for state ③: a node EXISTS at the path and the
// connection is refused (a leftover socket nobody accepts on).
func ubStalePath(t *testing.T, path, kind string) userBusPathObservation {
	t.Helper()
	return userBusPathObservation{
		path:         path,
		kind:         kind,
		statErr:      nil,
		connectErr:   &net.OpError{Op: "dial", Net: userBusUnixNetwork, Err: syscall.ECONNREFUSED},
		connectErrno: syscall.ECONNREFUSED,
	}
}

// ubReadyPath is the fixture for a path that answers.
func ubReadyPath(path, kind string) userBusPathObservation {
	return userBusPathObservation{path: path, kind: kind}
}

// ubObs builds a whole observation from a uid, a liveness answer and the path
// observations.
func ubObs(t *testing.T, active bool, paths ...userBusPathObservation) userBusObservation {
	t.Helper()
	return userBusObservation{
		uid:           1000,
		unit:          ubTestManagerUnit,
		runtimeDir:    ubTestRuntimeDir,
		managerActive: active,
		paths:         paths,
	}
}

// TestUserBusDiscriminator_ThreeStatesThreeDistinctReasons is the epic's own
// acceptance case: one measurement per host state, three DIFFERENT reasons, and
// the three next steps must differ too (a discriminator whose repairs are the
// same has not discriminated anything).
func TestUserBusDiscriminator_ThreeStatesThreeDistinctReasons(t *testing.T) {
	cases := []struct {
		name string
		obs  userBusObservation
		want string
	}{
		{
			// ① No user manager at all: nothing answers, and no socket is bound
			// (the manager was never there or is long gone).
			name: "no user manager",
			obs:  ubObs(t, false, userBusPathObservation{path: ubTestPrivatePath, kind: userBusPathKindPrivate, statErr: ubAbsentErr(t)}),
			want: userBusReasonNoManager,
		},
		{
			// ② The manager is ALIVE, the path is gone, the kernel still holds
			// the socket (this host's state, 2026-09-15).
			name: "bus path unlinked while the kernel holds the socket",
			obs:  ubObs(t, true, ubUnlinkedPath(t, ubTestPrivatePath, userBusPathKindPrivate)),
			want: userBusReasonUnlinked,
		},
		{
			// ③ A node exists and refuses.
			name: "stale socket node",
			obs:  ubObs(t, true, ubStalePath(t, ubTestBusPath, userBusPathKindBus)),
			want: userBusReasonStale,
		},
	}

	seenReason := map[string]string{}
	seenVerdict := map[string]string{}
	seenNext := map[string]string{}
	for _, tc := range cases {
		got := classifyUserBus(tc.obs)
		if got.reason != tc.want {
			t.Errorf("%s: reason = %q, want %q (measured: %s)", tc.name, got.reason, tc.want, got.measured)
		}
		if got.verdict == "" {
			t.Errorf("%s: a named reason must carry an operator-facing verdict", tc.name)
		}
		if got.measured == "" {
			t.Errorf("%s: the verdict must carry the measurement it was read from", tc.name)
		}
		if strings.Contains(got.measured, "\n") || strings.Contains(got.verdict, "\n") {
			t.Errorf("%s: the verdict and its evidence must each be ONE line, got %q / %q", tc.name, got.verdict, got.measured)
		}
		if got.next == "" {
			t.Errorf("%s: every fault state must name a next step", tc.name)
		}
		if other, dup := seenReason[got.reason]; dup {
			t.Errorf("state %s and %s share the reason %q — the states were not discriminated", tc.name, other, got.reason)
		}
		if other, dup := seenVerdict[got.verdict]; dup {
			t.Errorf("state %s and %s share the verdict — two states, one sentence", tc.name, other)
		}
		if other, dup := seenNext[got.next]; dup {
			t.Errorf("state %s and %s share the next step — the repairs must differ", tc.name, other)
		}
		seenReason[got.reason], seenVerdict[got.verdict], seenNext[got.next] = tc.name, tc.name, tc.name
	}
	if len(seenReason) != 3 {
		t.Fatalf("three synthetic states must produce three reasons, got %d: %v", len(seenReason), seenReason)
	}
}

// TestUserBusDiscriminator_UnlinkedNeverClassifiesAsNoManager is the RED PROOF
// the epic asks for: the unlinked-socket state may not collapse into "no user
// manager". Without it, the code could pass every other case here while
// printing the same sentence for the state this host is actually in.
//
// The proof is a CONTRAST on a single measurement: one path observation (absent
// path, socket bound, connect ENOENT) is classified twice, once under a live
// manager and once under a dead one, and the two verdicts must differ.
func TestUserBusDiscriminator_UnlinkedNeverClassifiesAsNoManager(t *testing.T) {
	unlinked := ubObs(t, true, ubUnlinkedPath(t, ubTestPrivatePath, userBusPathKindPrivate))
	noManager := ubObs(t, false, ubUnlinkedPath(t, ubTestPrivatePath, userBusPathKindPrivate))

	gotUnlinked := classifyUserBus(unlinked)
	gotNoManager := classifyUserBus(noManager)

	if gotUnlinked.reason == userBusReasonNoManager {
		t.Fatalf("an unlinked bus path with a LIVE manager classified as %q — the exact collapse this epic removes (measured: %s)",
			gotUnlinked.reason, gotUnlinked.measured)
	}
	if gotUnlinked.reason != userBusReasonUnlinked {
		t.Errorf("an absent-and-bound path under a live manager must be %q, got %q", userBusReasonUnlinked, gotUnlinked.reason)
	}
	if gotNoManager.reason == userBusReasonUnlinked {
		t.Errorf("a DEAD manager classified as %q — liveness must decide, not the path alone (measured: %s)",
			gotNoManager.reason, gotNoManager.measured)
	}
	if gotUnlinked.reason == gotNoManager.reason {
		t.Errorf("the same path observation must not yield one reason for two liveness states: %q", gotUnlinked.reason)
	}
	if gotUnlinked.verdict == gotNoManager.verdict {
		t.Errorf("two states must not share one verdict sentence: %q", gotUnlinked.verdict)
	}

	// The discriminator's second half, from the same direction: an ABSENT path
	// with NO bound socket is not the unlinked state — absence alone is not the
	// signal, the absence-while-bound PAIR is.
	absentUnbound := ubObs(t, true, userBusPathObservation{
		path:         ubTestPrivatePath,
		kind:         userBusPathKindPrivate,
		statErr:      ubAbsentErr(t),
		connectErr:   &net.OpError{Op: "dial", Net: userBusUnixNetwork, Err: syscall.ENOENT},
		connectErrno: syscall.ENOENT,
	})
	if got := classifyUserBus(absentUnbound); got.reason == userBusReasonUnlinked {
		t.Errorf("an absent path with NO kernel binding classified as %q — the kernel table is the second half of the signal (measured: %s)",
			got.reason, got.measured)
	}
}

// TestUserBusDiscriminator_UnlinkedEvidenceNamesTheKernelHold is the "evidence,
// not prose" case: the operator must be able to go from the printed line to the
// socket's holder and to the path's absence, so the decisive line carries the
// inode, the kernel's LISTEN state, the ENOENT from the client's own connect,
// and the commands that reproduce all three facts.
func TestUserBusDiscriminator_UnlinkedEvidenceNamesTheKernelHold(t *testing.T) {
	d := classifyUserBus(ubObs(t, true, ubUnlinkedPath(t, ubTestPrivatePath, userBusPathKindPrivate)))
	for _, want := range []string{ubTestPrivatePath, "unlinked", "ENOENT", "LISTEN", "inode", "40136"} {
		if !strings.Contains(d.measured, want) {
			t.Errorf("the evidence line must carry %q:\n%s", want, d.measured)
		}
	}
	for _, want := range []string{systemctlPath, systemctlIsActiveArg, ubTestManagerUnit, "stat '" + ubTestPrivatePath + "'", procNetUnixPath} {
		if !strings.Contains(d.confirm, want) {
			t.Errorf("the confirming commands must carry %q:\n%s", want, d.confirm)
		}
	}
	if !strings.Contains(d.next, "user-runtime-dir@1000.service") || !strings.Contains(d.next, "user@1000.service") {
		t.Errorf("the repair for the unlinked state must name the units the operator restarts:\n%s", d.next)
	}
	// Rule 25: the correction fragment the no-privilege gate requires must be in
	// this state's operator-facing text too (it is the state the operator is
	// actually in when they read it).
	if !strings.Contains(d.next, dsNoPrivCorrectionFragment) {
		t.Errorf("the unlinked state's next step must answer the privilege question plainly (missing %q):\n%s",
			dsNoPrivCorrectionFragment, d.next)
	}
	// And the runtime-directory hazard must be named here rather than in a
	// devdoc only: a sweep that empties the runtime directory is what unlinks
	// these sockets (memory #2810/#2811, measured).
	if !strings.Contains(d.next, "runtime directory") {
		t.Errorf("the next step must name the runtime directory as the thing that must be preserved:\n%s", d.next)
	}
}

// TestUserBusDiscriminator_EvidenceAtEveryPathLevel pins the "no silent
// absence" rule for the OTHER observables: a path that cannot be examined, and
// a connect failure whose errno the probe could not extract, must be reported
// as what they are — never folded into a named fault and never rendered as a
// bare number.
func TestUserBusDiscriminator_UnreadableAndUnclassifiedAreNotFaults(t *testing.T) {
	unreadable := userBusPathObservation{
		path:    ubTestPrivatePath,
		kind:    userBusPathKindPrivate,
		statErr: fs.ErrPermission,
	}
	if got := userBusPathState(unreadable); !strings.Contains(got, "unreadable") {
		t.Errorf("a path that could not be examined must be reported as unreadable, got %q", got)
	}
	if got := userBusErrnoName(0); got != "no error" {
		t.Errorf("errno 0 must render as no error, got %q", got)
	}
	if got := userBusErrnoName(syscall.EIO); got != userBusUnclassifiedFailure {
		t.Errorf("an errno without a name of its own must render as %q (never a number), got %q",
			userBusUnclassifiedFailure, got)
	}
	// A present, bound path that is NEITHER accepting nor refusing (the client
	// died for another reason) is not a named fault: it falls to the honest
	// unknown, and the unknown must ask the host the three questions.
	odd := ubObs(t, true, userBusPathObservation{
		path:         ubTestPrivatePath,
		kind:         userBusPathKindPrivate,
		bound:        true,
		listen:       true,
		connectErr:   errors.New("dial unix: something else"),
		connectErrno: 0,
	})
	d := classifyUserBus(odd)
	if d.reason != userBusReasonUnknown {
		t.Fatalf("a state the measurement does not name must classify as %q, got %q", userBusReasonUnknown, d.reason)
	}
	for _, want := range []string{systemctlPath, procNetUnixPath} {
		if !strings.Contains(d.next, want) {
			t.Errorf("the unknown state must hand the reader the questions to ask (%q missing):\n%s", want, d.next)
		}
	}
}

// TestUserBusDiscriminator_LivenessUnavailableIsUnknownNotNoManager is the Rule
// 15 case at the top of the state machine: when the liveness QUESTION could not
// be asked (no systemctl, no system bus), the classifier must not answer "no
// user manager" — that is an unmeasured claim, and it is exactly the kind of
// fabricated verdict the degraded-note epic removed one layer up.
func TestUserBusDiscriminator_LivenessUnavailableIsUnknownNotNoManager(t *testing.T) {
	obs := ubObs(t, false, ubUnlinkedPath(t, ubTestPrivatePath, userBusPathKindPrivate))
	obs.livenessErr = errors.New("no /usr/bin/systemctl")
	d := classifyUserBus(obs)
	if d.reason != userBusReasonUnknown {
		t.Fatalf("an unaskable liveness question must classify as %q, got %q", userBusReasonUnknown, d.reason)
	}
	if strings.Contains(d.verdict, userBusVerdictNoManager) {
		t.Errorf("the unknown verdict must not claim there is no manager:\n%s", d.verdict)
	}
}

// TestUserBusDiscriminator_HealthyBusIsNotAFault is the other direction: a bus
// that answers must not be reported as a fault, and must carry no next step (a
// remedy for a healthy state invents a problem — the same rule the no-privilege
// correction follows).
func TestUserBusDiscriminator_HealthyBusIsNotAFault(t *testing.T) {
	d := classifyUserBus(ubObs(t, true,
		ubReadyPath(ubTestPrivatePath, userBusPathKindPrivate),
		ubReadyPath(ubTestBusPath, userBusPathKindBus)))
	if d.reason != userBusReasonHealthy {
		t.Fatalf("an answering bus classified as %q, want %q", d.reason, userBusReasonHealthy)
	}
	if d.next != "" {
		t.Errorf("a healthy bus must carry no next step, got %q", d.next)
	}
	if line := daemonUserBusBlockerNote("gopi worker", d); line != "" {
		t.Errorf("a healthy bus must print no blocker note, got %q", line)
	}
}

// TestUserBusDiscriminator_ReasonCodesAreStableAndDistinct is the cheap
// anti-drift guard: the reason is the discriminator's WIRE value (an operator
// greps it, a test compares it), so the codes must stay three distinct, stable
// strings — a rename that keeps two reasons equal is the collapse returning in
// another spelling.
func TestUserBusDiscriminator_ReasonCodesAreStableAndDistinct(t *testing.T) {
	codes := []string{
		userBusReasonNoManager, userBusReasonUnlinked, userBusReasonStale,
		userBusReasonHealthy, userBusReasonUnknown,
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if c == "" || strings.ContainsAny(c, " \t\n") {
			t.Errorf("a reason code must be a non-empty single word, got %q", c)
		}
		if seen[c] {
			t.Errorf("two reasons share the code %q", c)
		}
		seen[c] = true
	}
	if len(seen) != len(codes) {
		t.Fatalf("the reason codes must be distinct, got %d values from %d codes", len(seen), len(codes))
	}
}

// TestUserBusRender_IsFourBoundedLines pins the operator-facing shape: header
// (with the reason), measurement, confirming commands, next step — each one
// line, because the block is printed on every degraded start on this host.
func TestUserBusRender_IsFourBoundedLines(t *testing.T) {
	d := classifyUserBus(ubObs(t, true, ubUnlinkedPath(t, ubTestPrivatePath, userBusPathKindPrivate)))
	lines := daemonUserBusDiagnosisLines("gopi worker", d)
	if len(lines) != 4 {
		t.Fatalf("a fault diagnosis is four lines, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], d.reason) {
		t.Errorf("the first line must carry the reason code (an operator greps it), got %q", lines[0])
	}
	if !strings.Contains(lines[0], d.verdict) {
		t.Errorf("the first line must carry the verdict, got %q", lines[0])
	}
	for i, l := range lines {
		if strings.Count(l, "\n") != 1 || !strings.HasSuffix(l, "\n") {
			t.Errorf("line %d must be exactly one line, got %q", i, l)
		}
		if !strings.HasPrefix(l, "gopi worker: ") {
			t.Errorf("line %d must be attributed to the face, got %q", i, l)
		}
	}
	// Every emitted line obeys the no-privilege gate by construction (the same
	// predicate the source sweep uses), because this text goes to an operator.
	for _, l := range lines {
		if hit, offense := dsNoPrivilegeOffense(l); hit {
			t.Errorf("the diagnosis prescribes a privilege (%q): %q", offense, l)
		}
	}
}

// ubScratchSocketPath returns a directory whose name is SHORT and whose
// resulting socket path fits AF_UNIX's sun_path limit, inside the suite's
// scratch root.
//
// WHY THE NAME IS PINNED SHORT AND THE BIND IS CHECKED: a unix socket path is
// capped at 108 bytes INCLUDING the terminator, and bind(2) answers EINVAL —
// not ENAMETOOLONG — when it does not fit. On a host whose scratch root is
// itself a tmpfs (Rule 22's memory root, e.g. GOPI_TEST_TMPDIR=/run/user/1000/x)
// the suite's own nested names are long enough that an "ubdiag<random>" tail
// pushes the path over: measured, 108 bytes ⇒ bind: invalid argument. And an
// EINVAL from bind is not a statement about sockets — so the two real causes
// (a name that does not fit, a filesystem that cannot hold a socket node) are
// told apart from it rather than being swallowed as a failure.
func ubScratchSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(testutil.SuiteTempRoot(), ubScratchDirPrefix)
	if err != nil {
		t.Fatalf("scratch dir: %v", err)
	}
	t.Cleanup(func() {
		//stylegate:ignore best-effort scratch cleanup; the case's verdict is already decided
		_ = os.RemoveAll(dir)
	})
	path := filepath.Join(dir, userBusSocketName)
	if len(path) >= ubSocketPathMaxBytes {
		t.Fatalf("the scratch root is too deep for an AF_UNIX socket path: %q is %d bytes, and a unix path is capped at %d — set %s to a shorter root",
			path, len(path), ubSocketPathMaxBytes, testutil.SuiteTempRootEnvName)
	}
	return path
}

// TestUserBusProbe_RealUnlinkedSocketIsDetected is the POSITIVE CONTROL for the
// predicate: the state the epic is about is produced for real — bind a socket
// in the suite's own scratch root, unlink its path while the listener stays
// open, and observe it. A predicate that never fires on the state it claims to
// detect is a blind gate, and this case is what makes the synthetic fixtures
// credible.
//
// The scratch root is testutil.SuiteTempRoot (never a hardcoded /tmp): the
// socket and its path are per-run, off the `/` partition on a host that can
// provide one (Rule 22).
func TestUserBusProbe_RealUnlinkedSocketIsDetected(t *testing.T) {
	path := ubScratchSocketPath(t)

	lis, err := net.Listen(userBusUnixNetwork, path)
	if err != nil {
		// A filesystem that cannot hold a socket node (FAT, some overlay
		// mounts) answers EINVAL too, and that is a statement about the
		// FILESYSTEM, not about the predicate this case exists to exercise: the
		// measurement is reported and the case is skipped, which is not
		// silence — the reason and the errno are named.
		if errors.Is(err, syscall.EINVAL) {
			t.Skipf("the suite's scratch root cannot hold an AF_UNIX socket node (%v) — the unlinked-yet-bound predicate is not exercised here", err)
		}
		t.Fatalf("bind a unix socket: %v", err)
	}
	// The listener stays OPEN: this is the unlinked-yet-bound state, not a
	// closed socket.
	t.Cleanup(func() {
		//stylegate:ignore best-effort close of the fixture listener; the verdict is in hand
		_ = lis.Close()
	})

	before := observeUserBusPath(path, userBusPathKindBus)
	if before.statErr != nil || !before.bound || !before.listen || before.connectErr != nil {
		t.Fatalf("a freshly bound socket must be present, bound, LISTEN and accepting; measured %+v", before)
	}
	if before.inode == 0 {
		t.Fatalf("the kernel must report an inode for a bound socket; measured %+v", before)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("unlink the socket path: %v", err)
	}
	after := observeUserBusPath(path, userBusPathKindBus)
	if after.statErr == nil {
		t.Errorf("the path was unlinked but stat reported it present: %+v", after)
	}
	if !after.bound || !after.listen {
		t.Errorf("the kernel must still hold the socket after the path was unlinked: %+v", after)
	}
	if after.inode != before.inode {
		t.Errorf("the bound socket's inode must be unchanged by the unlink: before %d, after %d", before.inode, after.inode)
	}
	if !errors.Is(after.connectErrno, syscall.ENOENT) {
		t.Errorf("connect to an unlinked path must fail ENOENT (the discriminator's signal), got %v", after.connectErr)
	}
	if state := userBusPathState(after); !strings.Contains(state, "unlinked") {
		t.Errorf("the state word for an unlinked-and-bound path must say so, got %q", state)
	}

	// AND A LIVE CLIENT MUST NOT HIDE IT (the regression this case now pins).
	// ONE client connect() makes the kernel render a SECOND /proc/net/unix row
	// carrying the SAME path — St=02, Inode=0, the connected socket object —
	// and the kernel places that row FIRST, so a lookup that stopped at the
	// first matching row described the CLIENT and reported inode 0 with no
	// LISTEN for a socket that was bound and listening at that instant (the
	// measurement is in userBusUnixBinding). The client's connect FAILS here
	// (the path is already gone) and its row is left OPEN across the next
	// observation: the worst case, and the binding must survive it.
	client, dialErr := net.DialTimeout(userBusUnixNetwork, path, userBusConnectBudget)
	if client != nil {
		//stylegate:ignore the fixture client is closed below; a stray close error cannot change the verdict
		defer client.Close()
	}
	if dialErr == nil {
		t.Fatalf("a connect to an unlinked path succeeded — the fixture no longer produces state ②")
	}
	// PROVE the interleaving was produced HERE, independently of the fold's own
	// counter: more than one row now carries this one path.
	if n := ubClientRows(t, path); n <= 1 {
		t.Fatalf("only %d /proc/net/unix row(s) carry %s — the kernel produced no client row, so the interleaving this case pins does not exist here",
			n, path)
	}
	shared := observeUserBusPath(path, userBusPathKindBus)
	if !shared.bound || !shared.listen || shared.inode != before.inode {
		t.Errorf("a live client's row must not hide the binding: %+v", shared)
	}
	if state := userBusPathState(shared); !strings.Contains(state, "unlinked") {
		t.Errorf("the state word under a live client must still say unlinked, got %q", state)
	}
	// And the classifier reads the real observation as the unlinked state when
	// the manager is alive — the same verdict the synthetic case asserts.
	got := classifyUserBus(userBusObservation{
		uid: 1000, unit: ubTestManagerUnit, runtimeDir: filepath.Dir(path),
		managerActive: true,
		paths:         []userBusPathObservation{after},
	})
	if got.reason != userBusReasonUnlinked {
		t.Errorf("a real unlinked-yet-bound socket classified as %q, want %q (measured: %s)",
			got.reason, userBusReasonUnlinked, got.measured)
	}
}

// ubClientRows counts the /proc/net/unix rows carrying path, so a test that
// means to exercise the shared-path interleaving can PROVE the kernel produced
// it here rather than assuming it (Rule 15: a check that silently cannot fail is
// not a check).
func ubClientRows(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(procNetUnixPath)
	if err != nil {
		t.Fatalf("read %s: %v", procNetUnixPath, err)
	}
	defer f.Close() //stylegate:ignore the read is complete before the caller returns
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) <= procNetUnixPathFieldIdx0 {
			continue
		}
		if fields[procNetUnixPathFieldIdx0] == path {
			n++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", procNetUnixPath, err)
	}
	return n
}

// TestUserBusKernelBinding_FoldsEveryRowForThePath is the regression guard for
// the positive control's failure, as a PURE test: the fold over the kernel's
// rows takes the LISTENing row wherever it sits, in every order, and never lets
// a client's row (State 02, Inode 0) stand in for the binding.
//
// WHY A PURE CASE AND NOT ONLY THE LIVE ONE: a test cannot make the kernel place
// the client's row FIRST on demand — that is the kernel's ordering, not the
// test's choice — so the dangerous interleaving is exercised here as VALUES
// (the rows are verbatim /proc/net/unix lines, in both orders), while the live
// case above proves that rows really do share one path on a real kernel.
func TestUserBusKernelBinding_FoldsEveryRowForThePath(t *testing.T) {
	rows := func(lines ...string) [][]string {
		out := make([][]string, 0, len(lines))
		for _, l := range lines {
			out = append(out, strings.Fields(l))
		}
		return out
	}
	for _, tc := range []struct {
		name string
		rows [][]string
		want userBusUnixBinding
	}{
		{
			name: "the listener alone (nothing has connected yet)",
			rows: rows(ubListenRow),
			want: userBusUnixBinding{found: true, bound: true, listen: true, inode: ubTestRowInode, rows: 1},
		},
		{
			name: "the client's row FIRST — the order the kernel actually emits",
			rows: rows(ubClientRow, ubListenRow),
			want: userBusUnixBinding{found: true, bound: true, listen: true, inode: ubTestRowInode, rows: 2},
		},
		{
			name: "the listener first, the client's row after it",
			rows: rows(ubListenRow, ubClientRow),
			want: userBusUnixBinding{found: true, bound: true, listen: true, inode: ubTestRowInode, rows: 2},
		},
		{
			name: "only a client's row (nothing is bound at this path)",
			rows: rows(ubClientRow),
			want: userBusUnixBinding{found: true, rows: 1},
		},
	} {
		got := userBusFoldUnixRows(tc.rows)
		if !got.found {
			t.Fatalf("%s: a path carried by a row must be reported as found", tc.name)
		}
		if got != tc.want {
			t.Errorf("%s: fold = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if got := userBusFoldUnixRows(nil); got.found {
		t.Errorf("no row at all must report NO binding — an absence is not a binding")
	}
}

// TestUserBusKernelBinding_UnreadableTableIsUnknownNotAbsence pins the honest
// floor this fix added: a binding table that could not be READ is a different
// fact from a table that holds no row, and only the second one may be read as a
// fault. An unreadable table makes states ②③④ indistinguishable, so the
// classifier must say unknown — never "no socket is bound", which would report
// a healthy bus as a fault.
func TestUserBusKernelBinding_UnreadableTableIsUnknownNotAbsence(t *testing.T) {
	unreadable := userBusPathObservation{
		path:       ubTestPrivatePath,
		kind:       userBusPathKindPrivate,
		bindingErr: errors.New("read " + procNetUnixPath + ": permission denied"),
	}
	d := classifyUserBus(ubObs(t, true, unreadable))
	if d.reason != userBusReasonUnknown {
		t.Fatalf("an unreadable binding table classified as %q, want %q", d.reason, userBusReasonUnknown)
	}
	if !strings.Contains(d.measured, procNetUnixPath) {
		t.Errorf("the unknown state must name the table it could not read, got %q", d.measured)
	}
	if d.next == "" {
		t.Errorf("the unknown state must hand the reader the questions to ask")
	}
	// The same path with NO binding row at all is the genuine absence, and it
	// must NOT be classified as the unlinked state (absence alone is not the
	// signal — the absent-while-bound PAIR is).
	absent := classifyUserBus(ubObs(t, true, userBusPathObservation{
		path:    ubTestPrivatePath,
		kind:    userBusPathKindPrivate,
		statErr: ubAbsentErr(t),
	}))
	if absent.reason == userBusReasonUnlinked {
		t.Errorf("an absent path with no bound socket must not be the unlinked state: %+v", absent)
	}
}

// TestUserBusProbe_ThisHostStateIsMeasuredAndPrinted is the LIVE case, written
// in BOTH WORLDS: the probe runs against this host, and whatever it measures
// must be what the classifier reports — with the three fault states asserted
// from the MEASUREMENT (a bound socket + an absent path must be the unlinked
// verdict; no manager must be the no-manager verdict), never from a hardcoded
// expectation about this machine. On a host whose user bus is healthy the same
// case asserts the healthy verdict, so the test cannot pass by being skipped.
func TestUserBusProbe_ThisHostStateIsMeasuredAndPrinted(t *testing.T) {
	obs := observeUserBus(os.Getuid(), os.Getenv(userBusRuntimeDirEnv))
	d := classifyUserBus(obs)

	if obs.livenessErr == nil {
		// The liveness answer and the verdict must agree in both directions.
		if !obs.managerActive && d.reason != userBusReasonNoManager {
			t.Errorf("%s measured inactive but classified as %q", obs.unit, d.reason)
		}
		if obs.managerActive && d.reason == userBusReasonNoManager {
			t.Errorf("%s measured active but classified as no user manager", obs.unit)
		}
	}
	// A path measured absent-and-bound MUST be the unlinked state, whichever
	// path it was: this is the assertion that would have caught the blended
	// message on this host.
	for _, p := range obs.paths {
		if errors.Is(p.statErr, fs.ErrNotExist) && p.bound && d.reason != userBusReasonUnlinked {
			t.Errorf("%s is absent from the VFS but still bound in the kernel (inode %d), yet the diagnosis says %q:\n%s",
				p.path, p.inode, d.reason, d.measured)
		}
	}
	if d.reason == "" || d.verdict == "" || d.measured == "" || d.confirm == "" {
		t.Fatalf("every state must carry reason, verdict, measurement and confirming commands; got %+v", d)
	}
	lines := daemonUserBusDiagnosisLines("gopi worker", d)
	if len(lines) < 3 {
		t.Fatalf("the rendered diagnosis is at least three lines, got %q", lines)
	}
	// The block must be greppable by reason code on the operator's own stdout.
	if !strings.Contains(strings.Join(lines, ""), "user bus ("+d.reason+")") {
		t.Errorf("the rendered diagnosis must name the reason:\n%s", strings.Join(lines, ""))
	}
}

// TestUserBusDiagnosis_OnlyTheBusFailureIsDiagnosed pins the cost/benefit gate
// at the seam: the probe runs only when rung ① died of a BUS failure. A
// systemd-run failure about anything else (a unit clash, a missing binary) must
// not collect a user-bus verdict it did not earn — and the zero diagnosis must
// stay zero, so the seam prints nothing.
func TestUserBusDiagnosis_OnlyTheBusFailureIsDiagnosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"systemd's own bus failure", errors.New("Failed to connect to bus: No such file or directory"), true},
		{"a bus failure in another spelling", errors.New("Failed to connect to BUS"), true},
		{"a unit-name clash", errors.New("Unit gopi-daemon-merger.scope already exists"), false},
		{"no systemd-run at all", errors.New("no /usr/bin/systemd-run"), false},
		{"no failure", nil, false},
	} {
		if got := userBusFailureMentionsBus(tc.err); got != tc.want {
			t.Errorf("%s: mentionsBus = %v, want %v", tc.name, got, tc.want)
		}
		if tc.want {
			continue
		}
		if d := daemonUserBusDiagnosis(tc.err); daemonUserBusDiagnosed(d) {
			t.Errorf("%s: a non-bus failure must not be diagnosed as a user-bus state, got %+v", tc.name, d)
		}
	}
}

// TestUserBusLiveness_AnUnknownUnitIsAnAnswerNotAFailure pins the boundary of
// the liveness question in BOTH worlds: when the system bus can be asked, a
// unit systemd does not know must come back as "not active" with NO error (a
// status answer); when the question cannot be asked at all, the error must be
// the question's — and the classifier must then refuse to name a fault. The
// case never asserts this host's manager state, so it is meaningful on a host
// where the bus answers and on one where it does not.
func TestUserBusLiveness_AnUnknownUnitIsAnAnswerNotAFailure(t *testing.T) {
	const unknownUnit = "gopi-userbus-probe-nonexistent.service"
	active, err := userBusManagerActive(unknownUnit)
	if err != nil {
		// The question could not be asked (no system bus on this host): the
		// answer must be reported as UNKNOWN, never as "no user manager".
		d := classifyUserBus(userBusObservation{uid: 1000, unit: unknownUnit, livenessErr: err})
		if d.reason != userBusReasonUnknown {
			t.Fatalf("an unaskable liveness question classified as %q, want %q", d.reason, userBusReasonUnknown)
		}
		return
	}
	if active {
		t.Errorf("a unit systemd does not know answered %q=true — the exit status was misread", unknownUnit)
	}
	// And a REAL unit name that systemd knows must also come back with an
	// answer (not an error), whichever state it is in: the probe classifies the
	// answer, not the host.
	if _, err := userBusManagerActive(ubTestManagerUnit); err != nil {
		t.Errorf("the liveness question about %s failed while the bus was reachable: %v", ubTestManagerUnit, err)
	}
}
