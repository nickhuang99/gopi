// Package cgroupscope moves a process into its own cgroup v2 sub-cgroup, so the
// process is no longer a member of the LOGIN SESSION's scope.
//
// WHY IT EXISTS (epic: daemon-cgroup-detach, 2026-09-12 / 2026-09-13 incidents).
// `gopi worker --daemon` / `gopi merger --daemon` detach with fork/exec-self +
// setsid, which makes the child a session leader with no controlling terminal —
// but setsid does NOT change the cgroup. The daemon keeps living in
// /user.slice/user-1000.slice/session-N.scope, and when systemd tears that
// session scope down (ssh closed, terminal gone, PA killed) EVERY process in it
// receives SIGTERM. setsid is therefore NOT the guarantee it looks like: a
// detached daemon must leave the session scope itself.
//
// THE MECHANISM (measured on this host, kernel 5.15, cgroup v2 unified):
// the user manager (user@1000.service) delegates its own subtree to the user
// (the systemd "Delegate=" default for user managers), so the user can create
// cgroups under /sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/…
// and write pid files inside them. There is NO dbus/session bus requirement —
// a plain mkdir + a write into cgroup.procs is enough. systemd-run --user --scope
// would be the declarative equivalent but needs that bus (absent on this host);
// this is the zero-dependency path that always works when the delegation does.
//
// THE SESSION SCOPE ITSELF IS NOT WRITABLE — the kernel checks write access on
// the cgroup's PARENT, so moving a self-migrating process one level up is
// permitted while moving it sideways/into the session scope is not. The target
// is therefore a child of the user manager's cgroup, which is where the session
// scope is NOT.
//
// WHAT IS DELIBERATELY NOT DONE HERE:
//
//   - no silent fallback: if the escape cannot be performed AND verified, the
//     caller is told (ErrNoEscape) and decides what "unavailable" means. A
//     detach that silently leaves the process in the session scope is exactly
//     the fake guarantee this package exists to remove.
//   - no assumption that a successful write() means the move happened: the
//     only accepted proof is a FRESH read of /proc/self/cgroup (a probe on this
//     host returned success from a write that changed nothing).
package cgroupscope

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/nickhuang99/gopi/internal/boolvocab"
)

// cgroup v2 filesystem facts (one spelling each, Rule 1b).
const (
	// Root is the cgroup v2 unified mount point.
	Root = "/sys/fs/cgroup"
	// ProcRoot is the procfs mount point, where the authoritative
	// /proc/<pid>/cgroup reads come from.
	ProcRoot = "/proc"
	// ProcSelfCgroup is the file the CALLER's position is read from and the
	// only accepted proof that a move happened.
	ProcSelfCgroup = "cgroup"
	// ControllerFileName is the file that marks a directory as a cgroup.
	ControllerFileName = "cgroup.controllers"
	// ProcsFileName is the file a process is migrated through.
	ProcsFileName = "cgroup.procs"
	// SubtreeControlFileName is the file naming the controllers a cgroup has
	// delegated to its children. A cgroup with any controller enabled in it may
	// not hold processes of its own, so it cannot adopt a process migrating from
	// another subtree (measured EBUSY) — this file, not the child tree, is what
	// decides that veto.
	SubtreeControlFileName = "cgroup.subtree_control"
	// noControllerEnabled is the measurement targetMayAdopt reports when the
	// veto does not apply, so the caller's reason can always name what it read.
	noControllerEnabled = "(no controller enabled)"
	// CgroupfsMagic is the fs type reported for a cgroup v2 mount in
	// /proc/self/mountinfo (used to confirm the unified hierarchy, never a
	// synthesized path guess).
	CgroupfsMagic = "cgroup2"
	// SelfCgroupPrefix is the cgroup-v2 unified-hierarchy line prefix in
	// /proc/<pid>/cgroup ("0::/path"). Anything else is a v1 line and is not
	// what "which systemd scope owns this process" is answered from.
	SelfCgroupPrefix = "0::"
)

// Naming facts of the hierarchy this package targets.
const (
	// accessWrite is the access(2) MAY_WRITE bit. The stdlib syscall package
	// exposes Access() but no W_OK constant, so it is named here: the mode word
	// is a permission bit of the C library's ABI, not a tunable.
	accessWrite = 2
	// userSliceName is the slice containing every logged-in user's processes.
	userSliceName = "user.slice"
	// userSlicePrefix is the per-uid slice naming ("user-1000.slice").
	userSlicePrefix = "user-"
	// userSliceSuffix is that name's tail.
	userSliceSuffix = ".slice"
	// userManagerPrefix is the user manager unit's directory name
	// ("user@1000.service").
	userManagerPrefix = "user@"
	// userManagerSuffix is that name's tail.
	userManagerSuffix = ".service"
	// scopeSuffix names a systemd scope unit's cgroup directory.
	scopeSuffix = ".scope"
	// EscapeTargetPrefix names every cgroup (and every systemd scope unit) this
	// package and the daemon seam that uses it create to hold a DETACHED
	// process: `gopi-daemon-<component>-<profile>`. It is exported because the
	// seam names the systemd unit with it, and a second spelling would let the
	// two drift — the escape target would then be classified as a session scope
	// and every "did it escape?" verdict would read backwards.
	EscapeTargetPrefix = "gopi-daemon-"
	// defaultScopeDirName is the sub-cgroup a daemon is moved into when the
	// caller names none. A plain name (no leading dot) so an operator reading
	// `systemd-cgls` sees what it is.
	defaultScopeDirName = "gopi-daemon"
	// dirMode is the mode of a created cgroup directory: the cgroup filesystem
	// exposes its own control files, the mode only has to permit the owner's
	// traversal.
	dirMode = 0o755
	// zeroCPUs stands for "no limit" in cgroup.max.descendants.
	zeroCPUs = "0"
)

// Terminal/pty SCOPE markers — the names that say a session scope is backed by
// a TERMINAL, which is the fact TerminalScope answers (task 01a163f3, measured
// 2026-10-22 22:04:55: a foreground `gopi worker` inside a vte-spawn scope died
// of SIGHUP when the terminal tab was torn down, `sourced:false`).
//
// WHY A MARKER TABLE IS RIGHT HERE AND WRONG FOR InSessionScope: that predicate
// answers "does a session teardown reach this process", a question about the
// cgroup SHAPE, so its walk is deliberately name-agnostic (see its own comment).
// TerminalScope answers a different and NARROWER question — "is this process's
// lifetime tied to an interactive terminal?" — whose answer IS the name: a
// terminal tab, a tmux pane and an ssh login differ in nothing but the unit
// that carries them. A name missing from this table costs a MISSED HINT (the
// banner stays quiet), never a wrong verdict about scope membership, which is
// why an incomplete list is tolerable here and would not be in the walk above.
const (
	// terminalScopeMarkerVTE is GNOME Terminal's (VTE) per-tab scope,
	// `vte-spawn-<uuid>.scope` — this host's own shape, read from
	// /proc/self/cgroup.
	terminalScopeMarkerVTE = "vte-spawn-"
	// terminalScopeMarkerTmux is the tmux server's scope when a distribution
	// ships one (`tmux-spawn-*`, `app-tmux-*` all carry the word).
	terminalScopeMarkerTmux = "tmux"
	// sessionScopeMarker is systemd-logind's per-login scope, `session-<n>.scope`
	// — an ssh/console login, whose teardown hangs up the same way a tab's does.
	sessionScopeMarker = "session-"
)

// terminalScopePrefixMarkers are the markers a scope's name must START WITH. A
// PREFIX rule (not containment) is what keeps a GUI scope named after its own
// program out: this host really produces
// `app-gnome-gnome-session-quit-34290.scope`, which CONTAINS "session-" and is
// nothing of the kind (measured while red-proofing this predicate).
var terminalScopePrefixMarkers = []string{
	terminalScopeMarkerVTE,
	sessionScopeMarker,
}

// terminalScopeNameMarkers are the markers matched by CONTAINMENT, because the
// distributions that ship a tmux scope wrap the word in a unit name of their own
// (`app-tmux-1234.scope`, `tmux-spawn-abc.scope`) — no single prefix covers both.
var terminalScopeNameMarkers = []string{
	terminalScopeMarkerTmux,
}

// EnvOff names the environment variable that turns the escape off ("1"/"true").
// It exists so a host that must NOT be migrated (an operator's own debugging
// run, or a test asserting the pre-escape behaviour) has an explicit switch —
// and so the off-state is a documented, visible decision rather than an
// accident of a missing directory.
const EnvOff = "GOPI_CGROUP_ESCAPE_OFF"

// ErrNoEscape reports that this process could not be moved into its own cgroup
// and that the failure was VERIFIED (a fresh /proc/self/cgroup read still names
// the old cgroup). Callers must treat it as "cgroup independence unavailable",
// never as a successful detach.
var ErrNoEscape = errors.New("cgroup scope escape unavailable")

// errReadError is the sentinel error a failed read path returns. Errors are
// examined with errors.As so this value is never constructed at a call site.
type errReadError struct {
	path string
	err  error
}

func (e *errReadError) Error() string {
	return fmt.Sprintf("read %s: ", e.path) + e.err.Error()
}

func (e *errReadError) Unwrap() error { return e.err }

// errAccessError is the sentinel error an access(2) permission probe returns:
// the OPERATION was not attempted at all, so it must not be reported with a
// read/write verb (that spelling is how "read …: is a directory" was produced
// for an operation that never read anything).
type errAccessError struct {
	path string
	err  error
}

func (e *errAccessError) Error() string {
	return fmt.Sprintf("%s is not writable by this user (%v)", e.path, e.err)
}

func (e *errAccessError) Unwrap() error { return e.err }

// errWriteError is the sentinel error a failed write path returns.
type errWriteError struct {
	path string
	err  error
}

func (e *errWriteError) Error() string {
	return fmt.Sprintf("write %s: ", e.path) + e.err.Error()
}

func (e *errWriteError) Unwrap() error { return e.err }

// Off reports whether the escape was explicitly disarmed. One reader of EnvOff
// (Rule 2/21); the truthy spellings come from the ONE vocabulary definition
// (internal/boolvocab — a leaf, so this package imports it rather than
// re-spelling a narrower copy, Rules 13/14).
func Off() bool {
	return boolvocab.IsTrue(os.Getenv(EnvOff))
}

// ScopeName is a resolved escape target: an existing cgroup directory plus the
// path /proc/self/cgroup must show afterwards.
type ScopeName struct {
	// Dir is the absolute cgroup filesystem path ("/sys/fs/cgroup/…/gopi-daemon").
	Dir string
	// Rel is the path that must appear after "0::" in /proc/self/cgroup
	// ("/user.slice/user-1000.slice/user@1000.service/gopi-daemon").
	Rel string
}

// ManagerDir returns the user manager's cgroup directory
// (/sys/fs/cgroup/user.slice/user-<uid>.slice/user@<uid>.service) — the parent
// under which the escape target is created. It is resolved from the process's
// OWN current cgroup when that cgroup is already inside the manager (the normal
// case), so the location is measured rather than guessed.
func ManagerDir(uid int) (string, error) {
	cur, err := SelfRel()
	if err != nil {
		return "", err
	}
	managerRel := filepath.Join(userSliceName, userSliceDirName(uid), userManagerDirName(uid))
	// Both a process started by the user manager and one started by a session
	// scope are under user.slice/user-UID.slice; the escape target is always
	// the manager, so the path is derived from the uid and CONFIRMED against
	// the live hierarchy.
	dir := filepath.Join(Root, managerRel)
	if _, err := os.Stat(filepath.Join(dir, ControllerFileName)); err != nil {
		return "", &errReadError{path: filepath.Join(dir, ControllerFileName), err: err}
	}
	_ = strings.TrimSpace(cur)
	return dir, nil
}

// userSliceDirName is the per-uid slice directory name ("user-1000.slice").
func userSliceDirName(uid int) string {
	return userSlicePrefix + strconv.Itoa(uid) + userSliceSuffix
}

// userManagerDirName is the user manager's cgroup directory name
// ("user@1000.service").
func userManagerDirName(uid int) string {
	return userManagerPrefix + strconv.Itoa(uid) + userManagerSuffix
}

// ManagerRel is the cgroup-relative path of the user manager for uid.
func ManagerRel(uid int) string {
	return "/" + filepath.Join(userSliceName, userSliceDirName(uid), userManagerDirName(uid))
}

// SliceDir is the per-uid slice's cgroup directory
// (/sys/fs/cgroup/user.slice/user-<uid>.slice). It is NOT the migration gate:
// it is the common ancestor the kernel checks when the caller sits OUTSIDE the
// manager subtree (a login session scope, whose sibling relationship to
// user@<uid>.service makes the slice the lowest shared cgroup). Reading it as
// "the gate for every caller" is the defect epic 01a12c31 removed — the gate is
// whichever ancestor the actual source and target share, which commonAncestor
// computes and Available asks about.
func SliceDir(uid int) string {
	return filepath.Join(Root, userSliceName, userSliceDirName(uid))
}

// SelfRel returns the cgroup-relative path of THIS process (the "0::" line of
// /proc/self/cgroup), or an error when the unified hierarchy is not in use.
// This is the value every verdict is compared against — never a caller's hope.
func SelfRel() (string, error) {
	return RelOfPID(os.Getpid())
}

// RelOfPID reads the unified-hierarchy cgroup path of pid from procfs. It is
// the ONLY accepted evidence about where a process is: a write into a
// cgroup.procs that reports success proves nothing (measured on this host).
func RelOfPID(pid int) (string, error) {
	path := filepath.Join(ProcRoot, strconv.Itoa(pid), ProcSelfCgroup)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", &errReadError{path: path, err: err}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, SelfCgroupPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, SelfCgroupPrefix)), nil
		}
	}
	return "", fmt.Errorf("%w: %s carries no %q line (cgroup v1 or an unexpected shape)",
		ErrNoEscape, path, SelfCgroupPrefix)
}

// InSessionScope reports whether a cgroup-relative path is a LOGIN SESSION
// SCOPE — the membership whose teardown SIGTERMs everything in it, which is what
// every verdict in this package is about.
//
// WHAT IT HAD TO LEARN (the defect this replaces, measured 2026-10-11):
//
//	the first version accepted a *.scope DIRECTLY under the per-uid slice
//	("…/user-1000.slice/session-4.scope") — the shape of an ssh login, and the
//	only shape the 2026-09-13 incidents were diagnosed in. A DESKTOP session is
//	not shaped that way: this host's agent shell lives at
//	  /user.slice/user-1000.slice/user@1000.service/app.slice/
//	      app-org.gnome.Terminal.slice/vte-spawn-<uuid>.scope
//	and everything a shell there starts inherits that path. A daemon that
//	correctly did NOT escape (`GOPI_CGROUP_ESCAPE_OFF=1`) was therefore read as
//	having escaped: the epic's off-switch case failed on the HOST SHAPE, not on
//	the behaviour it asserted.
//
// SO THE CRITERION IS WHICH UNIT OWNS THE PROCESS, NOT HOW DEEP THE PATH IS. A
// cgroup's processes are a member of the nearest ANCESTOR unit that carries
// processes of its own — the cgroup v2 rule, and why a process standing in
// `…/app.slice/app-org.gnome.Terminal.slice/vte-spawn-<uuid>.scope` is a member
// of that terminal's scope: it is a terminal the GNOME session spawned, its
// shell descends from the session, and it dies when the session is torn down.
// So the walk climbs and asks ONE question per level, stopping at an answer:
//
//   - is this a SYSTEMD SCOPE (`*.scope`)? Any scope that is not the user
//     manager's own subtree is a session's lifetime — the manager's own scopes
//     (init.scope) and the manager itself are the only exceptions, and they are
//     handled by the boundary below. This is deliberately name-agnostic: a
//     session scope's name is `session-4.scope` for a login, but nothing in a
//     cgroup path says which scope is a session — and asking whether a path is
//     "a session's" by its NAME would have to guess at every spelling systemd
//     has ever used (desktop app scopes, container payload scopes) and would
//     misjudge each guess in one direction or the other.
//   - is this the LOGIN MACHINERY (the per-uid slice `user-<uid>.slice`, or the
//     user manager `user@<uid>.service`)? Then the process is OUTSIDE the login
//     sessions: the manager outlives every session of that user, and it is
//     exactly where the systemd escape mechanism puts a daemon. The walk ends
//     here, answering "not in a session scope".
//
// Slices are therefore TRANSPARENT: `app.slice`, `background.slice`,
// `session.slice` and the frames vendor slices are all units a session's
// processes are grouped in, not sessions themselves, so the walk passes through
// them and keeps asking — which is what makes the desktop shape work without a
// table of vendor names.
//
// TWO SCOPES ARE NOT SESSIONS EVEN THOUGH THEY ARE SCOPES, and each is answered
// by a rule of the walk rather than by a name list:
//
//   - a scope hanging DIRECTLY off the user manager is the manager's OWN
//     (`user@<uid>.service/init.scope`); per-user session scopes are siblings of
//     the manager, never its children, so the walk steps over it and reaches the
//     boundary above it;
//   - a scope named after the escape target (EscapeTargetPrefix) is our own
//     detach — the end state a successful escape produces, and the one thing in
//     this hierarchy that must never read as "still in a session".
//
// CHILDREN INHERIT: a cgroup UNDER a session scope is answered by the same rule
// (the nearest owning ancestor is that scope), and anything under the manager
// (`…/user@1000.service/gopi-daemon`) stops at the boundary.
func InSessionScope(rel string) bool {
	dir := filepath.Clean(rel)
	if dir == "." || dir == string(filepath.Separator) || !filepath.IsAbs(dir) {
		return false
	}
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		name := filepath.Base(dir)
		if isLoginMachineryName(name) {
			return false
		}
		if strings.HasSuffix(name, scopeSuffix) {
			// A scope that hangs DIRECTLY off the user MANAGER is the manager's
			// own scope (user@<uid>.service/init.scope), not a session: systemd's
			// per-user session scopes are siblings of the manager, never its
			// children. It is skipped so the walk reaches the boundary above it.
			if isUserManagerName(filepath.Base(parent)) {
				dir = parent
				continue
			}
			// Our OWN escape target is not a session's scope, whatever it hangs
			// under: a daemon this package or the daemon seam moved into a
			// fresh `gopi-daemon-*` scope is the end state the escape exists to
			// produce. It is recognised by the seam's own naming rule
			// (EscapeTargetPrefix), not by a list of hosts.
			return !strings.HasPrefix(name, EscapeTargetPrefix)
		}
		dir = parent
	}
}

// isLoginMachineryName reports whether a cgroup directory NAME is the LOGIN
// MACHINERY rather than a session: the per-uid slice ("user-1000.slice"), the
// parent of every session scope of that user. It outlives every session, so
// reaching it means no login session scope was on the way up.
//
// ONE name family, and only one: the per-uid slice is an ancestor of everything
// a user's sessions contain, INCLUDING the user manager, so it is the single
// place an ancestor walk must stop — a walk that also stopped at the manager
// would answer exactly the same verdicts (measured while red-proofing this
// predicate: removing the manager from the stop set turns no case red), and an
// untestable doubling of a boundary is the shape a later reader "simplifies"
// wrongly.
func isLoginMachineryName(name string) bool {
	return strings.HasPrefix(name, userSlicePrefix) && strings.HasSuffix(name, userSliceSuffix)
}

// isUserManagerName reports whether a directory name is a user manager unit
// ("user@1000.service") — the login machinery that outlives every session, and
// the parent the systemd escape mechanism creates its target under.
func isUserManagerName(name string) bool {
	return strings.HasPrefix(name, userManagerPrefix) && strings.HasSuffix(name, userManagerSuffix)
}

// TerminalScope reports whether a cgroup-relative path names a TERMINAL (or
// login) session scope — the membership whose teardown HANGS UP the process
// rather than merely signalling it, so an interactive foreground run dies with
// the tab / the ssh session it was started from.
//
// WHY IT IS NOT InSessionScope (which is true for BOTH): InSessionScope answers
// "does the scope teardown reach me" and is therefore also true for a desktop
// session scope, an app scope and a container payload scope — none of which a
// user is holding a terminal on. THIS predicate answers the narrower question a
// startup HINT needs: is there a terminal whose closing takes this process with
// it? Its verdict therefore ALSO requires InSessionScope — a `gopi-daemon-*`
// scope that happens to carry a marker word is still our own escape target, and
// reporting a daemon as terminal-bound would make the banner fire on the face
// that already solved the problem.
//
// THE EVIDENCE IS THE FRESH /proc/<pid>/cgroup READ (RelOfPID / SelfRel), never
// a caller's assumption: Rule 24 — only a fresh read is a verdict. An empty,
// relative or unreadable path is NOT a terminal scope: the hint is silent when
// the measurement could not be taken, because a hint that guesses is worse than
// no hint (it would teach an operator to skip the line that matters on a host
// where it does).
//
// It is a HINT's input, never a refusal: a foreground run inside a terminal is a
// legitimate, documented way to debug a worker.
func TerminalScope(rel string) bool {
	if !InSessionScope(rel) {
		return false
	}
	// THE OWNING SCOPE, not the leaf: a process one cgroup below a terminal tab
	// is still that tab's member, and the leaf there is a plain child directory.
	// The walk is cgroupscope's own (the same one InSessionScope uses), so the
	// two predicates cannot disagree about which unit owns a path.
	return terminalScopeName(owningScopeName(filepath.Clean(rel)))
}

// owningScopeName returns the NAME of the nearest ancestor-or-self directory
// that is a systemd scope, or "" when the walk reaches a boundary first. It
// mirrors InSessionScope's walk by construction: same climb, same stop.
func owningScopeName(rel string) string {
	dir := rel
	if dir == "." || dir == string(filepath.Separator) || !filepath.IsAbs(dir) {
		return ""
	}
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		name := filepath.Base(dir)
		if isLoginMachineryName(name) {
			return ""
		}
		if strings.HasSuffix(name, scopeSuffix) {
			if isUserManagerName(filepath.Base(parent)) {
				dir = parent
				continue
			}
			return name
		}
		dir = parent
	}
}

// terminalScopeName reports whether a scope's NAME is terminal-backed — a
// terminal tab, a tmux pane's server, or a login session.
//
// THE ESCAPE TARGET IS EXCLUDED FIRST, and by the same constant InSessionScope
// uses: `gopi-daemon-worker-<profile>.scope` is a scope the daemon seam created
// precisely to leave a terminal's lifetime, and a marker word inside it (a
// profile tag, a component name) must never resurrect the verdict.
func terminalScopeName(scopeName string) bool {
	if scopeName == "" || strings.HasPrefix(scopeName, EscapeTargetPrefix) {
		return false
	}
	for _, marker := range terminalScopePrefixMarkers {
		if strings.HasPrefix(scopeName, marker) {
			return true
		}
	}
	for _, marker := range terminalScopeNameMarkers {
		if strings.Contains(scopeName, marker) {
			return true
		}
	}
	return false
}

// Enter moves THIS process into its own cgroup under the user manager's
// delegated subtree and VERIFIES it with a fresh /proc/self/cgroup read.
//
// scopeDirName is the sub-cgroup to use ("" = defaultScopeDirName). The
// directory is reused when it already exists (so two daemons of the same face
// share one cgroup, exactly like a systemd scope would when the unit name
// repeats, and an operator sees ONE node per face in systemd-cgls).
//
// A verified move returns the resolved ScopeName. Anything else returns a
// wrapped ErrNoEscape: the caller then KNOWS it is still in the session scope
// and may choose a documented degradation — it may never assume independence.
func Enter(uid int, scopeDirName string) (ScopeName, error) {
	if Off() {
		return ScopeName{}, fmt.Errorf("%w: %s is set", ErrNoEscape, EnvOff)
	}
	if scopeDirName == "" {
		scopeDirName = defaultScopeDirName
	}
	manager, err := ManagerDir(uid)
	if err != nil {
		return ScopeName{}, fmt.Errorf("%w: user manager cgroup: %w", ErrNoEscape, err)
	}
	dir := filepath.Join(manager, scopeDirName)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return ScopeName{}, fmt.Errorf("%w: create %s: %w", ErrNoEscape, dir, err)
	}
	procs := filepath.Join(dir, ProcsFileName)
	if err := os.WriteFile(procs, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return ScopeName{}, fmt.Errorf("%w: migrate into %s: %w", ErrNoEscape, procs, err)
	}
	// THE VERDICT IS THE FRESH READ. A successful write above is not evidence:
	// a probe on this host reported success while /proc/self/cgroup still named
	// the old cgroup.
	rel, err := SelfRel()
	if err != nil {
		return ScopeName{}, fmt.Errorf("%w: verify after migrating into %s: %w", ErrNoEscape, procs, err)
	}
	want := ManagerRel(uid) + "/" + scopeDirName
	if filepath.Clean(rel) != filepath.Clean(want) {
		return ScopeName{}, fmt.Errorf("%w: wrote pid into %s but /proc/self/cgroup still reads %q (want %q)",
			ErrNoEscape, procs, rel, want)
	}
	return ScopeName{Dir: dir, Rel: rel}, nil
}

// Available reports whether an escape COULD succeed FROM WHERE THIS PROCESS
// IS, without performing one. It exists so a diagnostic can explain WHY a
// detach is degraded instead of reporting an opaque failure — and it must
// therefore ask the question the MIGRATION asks, never a neighbouring, easier
// one.
//
// THE PREDICATE IS POSITION-DEPENDENT (epic 01a12c31, measured 2026-10-12).
// cgroup v2's cross-subtree rule (kernel/cgroup/cgroup.c
// cgroup_migration_permission) does NOT ask "may I write the target's
// cgroup.procs", and it does NOT ask "may I write the per-uid slice". It walks
// the migration's SOURCE and TARGET to their lowest COMMON ANCESTOR cgroup and
// asks may-write on THAT ONE — then the low-level vet (any process may migrate
// a process; the target must not have a foreign child; no-internal-process).
// So the very same uid gets OPPOSITE answers depending on where the caller sits,
// and BOTH of these are true on one host at one instant:
//
//	this run's process:  vte-spawn-….scope under user@1000.service
//	  → target /user.slice/user-1000.slice/user@1000.service/<scope>
//	  → common ancestor = user@1000.service (nick:nick 0755) → PERMITTED
//	an ssh session:      session-3452.scope, a SIBLING of user@1000.service
//	  → the SAME target
//	  → common ancestor = user-1000.slice (root:root 0755) → EACCES
//
// A static check on the per-uid slice can therefore decide NEITHER case: it
// reports the in-manager (working) case as unavailable, which is exactly the
// disagreement this function was repaired for (Available said ErrNoEscape while
// Enter migrated and verified).
//
// THE GATES, in the order the operation meets them:
//
//	① THE MIGRATION GATE (cgroup_migration_permission) — as above, decided by
//	   the common ancestor's mode, computed from the caller's ACTUAL position
//	   (SelfRel) and the ACTUAL target. Only when the ancestor grants access may
//	   the kernel reach its vetoes: the no-internal-process rule (a cgroup
//	   holding processes may not adopt more, measured EBUSY) and the
//	   no-foreign-child rule. Both are vet with the SAME ancestor, so the
//	   ancestor verdict is reported first and the veto renames itself after it.
//	② THE CREATE GATE — MkdirAll of the scope directory under the manager.
//
// The verdict is the FIRST gate that fails (the operation stops there), and it
// names the path plus the consequence, so the operator can act on it. A probe
// that opens a cgroup DIRECTORY for writing can only ever answer EISDIR — see
// Writable — and that fabricated reason is precisely the defect this function
// was first repaired for.
//
// AN HONEST LIMIT (Rule 15): this is a PREDICTION from the same inputs the
// kernel uses, not the kernel's own answer. The one authoritative verdict stays
// Enter's fresh /proc/self/cgroup read, so a caller that needs certainty must
// still act and read — Available only decides whether acting is worth a try.
func Available(uid int) error {
	if Off() {
		return fmt.Errorf("%w: %s is set", ErrNoEscape, EnvOff)
	}
	manager, err := ManagerDir(uid)
	if err != nil {
		return fmt.Errorf("%w: user manager cgroup: %w", ErrNoEscape, err)
	}
	target, err := escapeTarget(uid, "")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoEscape, err)
	}
	// ① THE MIGRATION GATE — the mode of the migration's lowest common ancestor,
	// computed from where THIS process really is.
	src, err := SelfRel()
	if err != nil {
		return fmt.Errorf("%w: cannot read where this process is: %w", ErrNoEscape, err)
	}
	ancestor := commonAncestor(src, target.Rel)
	if err := Writable(filepath.Join(Root, ancestor)); err != nil {
		return fmt.Errorf("%w: a migration from %s to %s is gated on their shared ancestor cgroup: %w",
			ErrNoEscape, src, target.Rel, err)
	}
	// The ancestor granted access — now the kernel's OWN veto, which is not a
	// permission at all: a cgroup with a controller enabled in its own
	// subtree_control may not hold processes, so it cannot adopt one from
	// another subtree (EBUSY, measured).
	vacant, enabled, err := targetMayAdopt(filepath.Join(Root, target.Rel))
	if err != nil {
		return fmt.Errorf("%w: cannot tell whether %s may adopt a process: %w", ErrNoEscape, target.Rel, err)
	}
	if !vacant {
		return fmt.Errorf("%w: %s has %q enabled in its own %s, so the kernel refuses to let it hold a process and the adoption fails EBUSY (measured)",
			ErrNoEscape, filepath.Join(Root, target.Rel), enabled, SubtreeControlFileName)
	}
	// ② The create gate, reached only when ① passed.
	if err := Writable(manager); err != nil {
		return fmt.Errorf("%w: creating the scope directory: %w", ErrNoEscape, err)
	}
	return nil
}

// escapeTarget resolves the FULL migration target (the absolute path and the
// cgroup-relative path) without creating it — the single definition the
// protocol sentence below is also spelled from, so Available's prediction and
// Enter's execution cannot describe two different directories.
func escapeTarget(uid int, scopeDirName string) (ScopeName, error) {
	if scopeDirName == "" {
		scopeDirName = defaultScopeDirName
	}
	manager, err := ManagerDir(uid)
	if err != nil {
		return ScopeName{}, err
	}
	rel := filepath.Clean(ManagerRel(uid) + "/" + scopeDirName)
	return ScopeName{Dir: filepath.Join(manager, scopeDirName), Rel: rel}, nil
}

// commonAncestor returns the cgroup-relative path of the LOWEST cgroup that
// contains both a and b — the one cgroup whose mode decides whether the kernel
// permits a pid migration between them (cgroup_migration_permission). Inputs
// are the "0::" paths of /proc/<pid>/cgroup.
func commonAncestor(a, b string) string {
	ac := cgroupComponents(a)
	bc := cgroupComponents(b)
	var common []string
	for i := 0; i < len(ac) && i < len(bc) && ac[i] == bc[i]; i++ {
		common = append(common, ac[i])
	}
	if len(common) == 0 {
		return "/"
	}
	return "/" + strings.Join(common, "/")
}

// cgroupComponents splits a cgroup-relative path into its directory names: the
// cgroup root, "//" and "" all yield none, so the common ancestor of two
// unrelated subtrees is the root (and never an empty string that would resolve
// to the filesystem root by accident).
func cgroupComponents(rel string) []string {
	clean := filepath.Clean("/" + rel)
	if clean == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(clean, "/"), "/")
}

// targetMayAdopt reports whether the cgroup at rel may take in a process
// migrating from ANOTHER subtree, plus the measurement that decided it — the
// kernel's OWN veto, which sits BEHIND the permission gate and is not a
// permission at all.
//
// THE RULE, measured on this kernel (cgroup v2, 6.8) rather than quoted from
// documentation: a cgroup that has a controller enabled in its own
// cgroup.subtree_control may not hold processes of its own (the
// "no internal process" constraint), so a write into its cgroup.procs fails
// EBUSY ("Device or resource busy"):
//
//	target fresh, subtree_control empty                    → admitted
//	target with a child, a controller enabled, child EMPTY  → EBUSY
//	user@1000.service, subtree_control "memory pids"        → EBUSY
//	target, subtree_control EMPTY, child POPULATED          → admitted
//
// The last two rows are why this probe reads subtree_control and NOT the child
// tree: a populated child does NOT stop a cgroup from adopting a process, while
// an enabled controller DOES. The naive reading of "no internal process" gets
// both of those backwards, and each row above is a measurement.
//
// A directory that does not exist yet has no subtree_control file, hence no
// enabled controller: it is admitted, and the create gate below is what brings
// it into being (empty) for real.
func targetMayAdopt(dir string) (bool, string, error) {
	subtree := filepath.Join(dir, SubtreeControlFileName)
	b, err := os.ReadFile(subtree)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, noControllerEnabled, nil
		}
		return false, "", &errReadError{path: subtree, err: err}
	}
	enabled := strings.TrimSpace(string(b))
	if enabled == "" {
		return true, noControllerEnabled, nil
	}
	return false, enabled, nil
}

// MigrationProtocol renders the sentence Available's verdict is the PREDICTION
// of — the migration as it will actually be performed, with the cgroup whose
// mode the kernel checks. It exists so an operator reading a reason can see the
// question that was asked; a verdict without its protocol is a number without
// a unit.
func MigrationProtocol(uid int, scopeDirName string) (string, error) {
	src, err := SelfRel()
	if err != nil {
		return "", err
	}
	target, err := escapeTarget(uid, scopeDirName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("migrate pid %d from %s to %s; gated on the mode of their shared ancestor %s",
		os.Getpid(), src, target.Rel, filepath.Join(Root, commonAncestor(src, target.Rel))), nil
}

// Writable reports whether THIS process may write to path, as the kernel will
// decide it — syscall.Access(MAY_WRITE), i.e. access(2), which works for a
// DIRECTORY as well as a file.
//
// WHY NOT os.OpenFile(path, O_WRONLY, 0) (the shape this replaced, measured
// 2026-09-13): a directory cannot be opened for writing, so that probe answers
// EISDIR on every host. Wired into Available() it produced the fabricated
// verdict "cgroup scope escape unavailable: … open …: is a directory" while the
// very same subtree was writable (mkdir under it succeeded). A permission probe
// must be able to say "you may write here" about the thing the operation
// writes to — a cgroup directory.
func Writable(path string) error {
	if err := syscall.Access(path, accessWrite); err != nil {
		return &errAccessError{path: path, err: err}
	}
	return nil
}
