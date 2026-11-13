// Package worktreeroots — the ONE definition of every filesystem root a gopi
// agent may create, write or destroy work in.
//
// WHY THIS PACKAGE EXISTS (Rule 14, and the incident that motivated it): the
// 2026-09-13 /dev/shm worktree assessment measured 3.9 GB of the host's 7.8 GB
// tmpfs cap consumed by git worktrees and suite scratch, MemFree at 265 MB,
// and a swap cushion that is largely spent — while NO code in the repository
// could answer "where is an agent allowed to put a worktree?". The default was
// a sentence in GOPI.md (~/workspace/gopi-<task>) and the test harness pinned
// its own tmpfs root, so a `git worktree add /dev/shm/...` was a judgement call
// rather than a policy violation, and every root was a bare string literal at
// each site that spelled it (Rule 1b/13).
//
// This package is a LEAF: it imports nothing from the project (the toolnames
// precedent) so any consumer — internal/scratch, internal/config,
// internal/tools, cmd/agent — can import it with no cycle. It holds VALUES
// only; the policy that decides which of them authorises an operation lives
// with the operation (internal/tools/bash.go for shell commands,
// internal/tools/worktree_sweep.go for registrations).
//
// THE THREE ROLES a path can play, and why the distinction is load-bearing:
//
//	WORKTREE  — where a task's linked worktree belongs. DECLARED per repo in
//	            settings.json (Rule 12's `repos` table), defaulting to
//	            <home>/workspace. Persistent, backed by disk, never tmpfs.
//	SCRATCH   — the ephemeral tmpfs home (internal/scratch). Legitimate for
//	            suite fixtures and short-lived copies, and it has its own age
//	            GC. It is NOT a worktree root: everything in it is designed to
//	            be collected.
//	FIXTURE   — the throwaway area under the SYSTEM temp root (ambientTempRoot,
//	            i.e. the TMPDIR chain). Byte-for-byte test fixtures and
//	            one-off probes live here and are deleted by their creator.
//
// A declared WORKTREE root may coincide with a fixture root (an operator may
// point a sandbox at /tmp or at ~/workspace); that is why the policy checks
// ROLE MEMBERSHIP rather than one blessed path.
package worktreeroots

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	// WorkspaceDirName is the shared workspace directory under the home: the
	// default worktree root, and (Rule 14 hoist) the same value
	// internal/scratch.WorkspaceDirName carries for the scratch tree that
	// lives inside it. A rename is one line here.
	WorkspaceDirName = "workspace"
	// DefaultRootLeaf is the last path component of a default root: the
	// directory a worktree root is expected to live in. It exists so a reader
	// of a policy message can tell a defaulted root from a declared one.
	DefaultRootLeaf = WorkspaceDirName

	// ShmDir is the standard Linux shared-memory filesystem — a RECOGNITION
	// constant, never an authorisation to create anything.
	//
	// THE ROLE SPLIT (operator ruling, 2026-09-18: "just for a few ms
	// performance? no, no no! don't use tmpfs. change settings to workspace.
	// now"). The scratch home of record is a REAL DIRECTORY on the block device
	// (<home>/workspace/gopi-test-scratch — internal/scratch.HomeDir), so this
	// value now carries exactly ONE live role:
	//
	//   RECOGNISE AND CLEAN a legacy artifact — a worktree registration, a
	//   suite root, a per-process Go build cache, an ad-hoc session directory —
	//   that an earlier revision of this system created in the base. That role
	//   is real and is why the constant survives: internal/testutil.ShmDir and
	//   internal/testutil.MemoryBaseDefault are aliases of it
	//   (internal/scratch.DefaultBasePath is the base-residue sweep's target),
	//   and deleting the value would orphan 2.6 GB / 702 measured entries.
	//
	//   CREATE here is retired. No production path may create scratch, a
	//   worktree, or a cache under this value, and no refusal may PRESCRIBE it
	//   as the remedy — a tmpfs is no longer an acceptable scratch backing on
	//   this host at all. A worktree root may still BE this directory when an
	//   operator has explicitly declared it as a repo's worktree_root; that is
	//   the operator's own declaration, not this constant's authority.
	ShmDir = "/dev/shm"

	// AmbientTempRoot is the SYSTEM temp root — the last resort of the TMPDIR
	// chain, named once so the fixture-role test and the policy messages
	// cannot drift. internal/scratch.TmpDirEnvName decides the real one; this
	// constant only names the fallback the kernel/os package uses when nothing
	// is configured.
	AmbientTempRoot = "/tmp"

	// FixtureBannedRoot is the ambient temp root under its FIXTURE-harness
	// name: the directory a test names when the /tmp BAN itself is the subject
	// (a probe that must land on the ban's target, a saved/restored TMPDIR, a
	// prefix-containment contract) rather than a path a program chose.
	//
	// WHY A SECOND NAME FOR AN EXISTING VALUE (Rule 14): the value has ONE
	// definition — AmbientTempRoot, three lines up. This is an ALIAS, and the
	// alias exists because the two uses are different ROLES that the same
	// spelling served silently: "the system temp root" is what
	// internal/scratch resolves, while "the root the ban is about, spelled out
	// so the probe lands there even when the suite's own TMPDIR is elsewhere"
	// is a test target. Naming the second role is what lets a fixture say WHY
	// it must not be derived from the environment (deriving it there would
	// make the probe follow the very configuration it exists to prove is not
	// being followed). A rename stays one line: this constant has no literal.
	FixtureBannedRoot = AmbientTempRoot

	// WorktreeRootSettingsKey is the settings.json key inside one repo's entry
	// in the "repos" table that declares where that repo's worktrees belong
	// (Rule 12 precedent: the repos table is where a repo declares its
	// contract). Absent/blank = DefaultRoot.
	WorktreeRootSettingsKey = "worktree_root"

	// SharedBuildCacheDirName is the directory under a WORKTREE root that holds
	// the disk-backed build caches gopi SHARES across processes and builds —
	// today the merge-time BUILD gate's Go build cache
	// (internal/tools/merge_build_gate_gocache.go).
	//
	// WHY IT BELONGS BESIDE THE WORKTREE ROOT (and why the value is here, not
	// at the gate): a Go build cache is an inherently reusable cross-task asset
	// but it is NOT reusable across repos — a cache keyed by one repo's package
	// graph is dead weight for another — so its parent must be the same
	// declared-per-repo root a worktree uses, and it must be on DISK.
	//
	// The 2026-09-15 defect this constant exists for: the gate pointed GOCACHE
	// at a per-PROCESS directory inside the tmpfs scratch root
	// (<scratch>/queue/queue-runtime-merge-build-<pid>/gocache), so every
	// merger build wrote 726M-1.14G of RAM-backed tmpfs into a cache nobody
	// could reuse: measured 54 MB x 100+ dead per-PID copies = the 5.9G of
	// /dev/shm this sits in, with the filesystem at 100% on 2026-09-15. A
	// scratch class is designed to be COLLECTED, so a reusable cache can never
	// live in it (internal/scratch's own contract, Rule 22's criterion).
	SharedBuildCacheDirName = "gopi-build-cache"

	// SharedBuildCacheSuiteSandboxPrefix is the name prefix of the per-run GOCACHE
	// sandbox the tools test suite gives the merger BUILD gate for the length of a
	// run: `<prefix><pid>` directly under SharedBuildCacheDirName.
	//
	// WHY IT LIVES HERE, NEXT TO THE ROOT (Rules 13/14): the prefix names a child
	// of the directory this file already declares, and it has TWO readers that
	// must never disagree — the WRITER (internal/tools/testmain_test.go, whose own
	// constant is asserted equal to this one) and the SWEEP's namespace predicate
	// (internal/scratch's `build-cache/suite-sandbox` row, which claims the shells
	// a run did not reap). A prefix spelled in both places is a prefix that can
	// drift, and a drifted predicate reports an empty tree while the shells pile up
	// (the failure mode BuildCacheKeyHexLen's shared constant exists to prevent).
	//
	// The trailing separator is PART OF THE VALUE: the pid follows it directly, so
	// the predicate can require a digit run after the prefix rather than accepting
	// any name that merely starts with the prefix.
	//
	// THE VALUE IS NOT DERIVED FROM SharedBuildCacheDirName, and the first draft
	// that did was wrong (the assertion on the writer's side caught it): the name
	// the WRITER mints is its own constant plus the pid, so the shared spelling has
	// to be that same string.
	SharedBuildCacheSuiteSandboxPrefix = "gopi-tools-suite-build-cache-"

	// WorktreeRootEnvName overrides the declared root for ALL repos. It is the
	// sandbox knob: a suite or a probe points it at a fixture so its worktrees
	// land inside the fixture. Read in exactly ONE place (Resolve, Rule 2).
	WorktreeRootEnvName = "GOPI_WORKTREE_ROOT"
)

// Sentinel errors, exported so a caller can branch on the CAUSE instead of
// matching message text (Rule 15: every refusal is named, never a bare false).
var (
	// ErrNoHome is returned when no home directory can be resolved, so a
	// default root cannot be built. It is never silently replaced by a
	// fallback path: an unknown home means an unknown root, and an unknown
	// root must not authorise a write.
	ErrNoHome = errors.New("no home directory (HOME unset and os.UserHomeDir failed)")
)

// Root is one authorized filesystem root plus the ROLE it plays for this
// operation. A Root is always an absolute, cleaned path — never a spelling that
// depends on the process cwd (a cwd-relative root is a policy hole: the same
// command is allowed or refused depending on where it runs).
type Root struct {
	// Path is the absolute cleaned root.
	Path string
	// Role describes what operations the root authorises. Empty = no role, and
	// a root with no role authorises nothing.
	Role Role
	// Source says where the root came from, so a refusal can NAME it (Rule 15).
	// One of the Source* constants.
	Source string
}

// Role is what a root is FOR. The set is closed on purpose: a new authorising
// purpose must be added here, with its own justification, rather than by
// widening an existing role.
type Role string

const (
	// RoleWorktree: a task's linked worktree belongs here. Authorises worktree
	// creation and recursive cleanup INSIDE the root.
	RoleWorktree Role = "worktree"
	// RoleScratch: the ephemeral scratch home (age-GC'd). Authorises the
	// same operations as a worktree root — a suite's fixture worktree is a
	// legitimate, collected object.
	//
	// It is NOT "/dev/shm's role": since the 2026-09-18 ruling the scratch home
	// is a real directory on the block device (internal/scratch.HomeDir), and
	// no role authorises CREATING scratch on a memory filesystem any more. What
	// survives for a memory base is recognition — see RoleMemoryBase.
	RoleScratch Role = "scratch"
	// RoleFixture: the throwaway area under the system temp root.
	RoleFixture Role = "fixture"
	// RoleSession: the calling session's own working directory tree. It is the
	// historical beam of the bash guard (a command may clean up inside its own
	// cwd); it never authorises a WORKTREE to be created, because a worktree
	// belongs in a declared root, not wherever a session happens to stand.
	RoleSession Role = "session"
	// RoleMemoryBase: a memory filesystem (tmpfs) some scratch tree hangs off
	// — DERIVED by measurement rather than declared (Rule 22: the criterion is
	// the filesystem, never a spelled path), and EMPTY on a host whose scratch
	// home is on a block device, which is now the intended state (2026-09-18
	// ruling). It exists so a registration an earlier revision created in that
	// base (the measured gopi-verify-TASK family) stays governable: no declared
	// root named that directory, so the sweep's declared-root rung reported
	// every one of them "outside every declared root" while they held tmpfs RAM
	// nobody could reclaim (/dev/shm hit 100% on 2026-09-15 and broke suites
	// with a disk-full error). Recognition and cleanup — never creation.
	//
	// The role is SEPARATE from RoleScratch on purpose: the scratch HOME is a
	// directory this system owns end to end, while a memory base is a
	// world-writable tmpfs SHARED with the host — a root there may only ever
	// authorise an operation that the operation's own namespace rule already
	// admits (the sweep judges git REGISTRATIONS, so a foreign directory in the
	// base is inside the root and still never a candidate).
	RoleMemoryBase Role = "memory-base"
)

// Source values — where a root came from. They are constants because they are
// rendered into refusals (Rule 1b: a meaning-carrying string is never typed
// inline at the message site).
const (
	SourceSettingsEnv = "settings " + WorktreeRootSettingsKey + " / " + WorktreeRootEnvName
	SourceSettingsKey = "settings " + WorktreeRootSettingsKey
	SourceDefault     = "default <home>/" + DefaultRootLeaf
	SourceScratchHome = "scratch home"
	SourceTempRoot    = "system temp root"
	SourceSessionCwd  = "session working directory"
	SourceDevShm      = ShmDir + " (no declared root)"
	// SourceMemoryBaseMeasured: the memory filesystem the scratch home resolves
	// onto, measured by device id + statfs f_type. It names the MEASUREMENT
	// (not the path) so a reader can tell a derived root from a declared one.
	SourceMemoryBaseMeasured = "memory base measured from the scratch home"
	// SourceMemoryBaseDeclared: the same root, but an operator DECLARED it (the
	// scratch memory-base env or the settings.json tmpdir key) and the scratch
	// home was measured to resolve onto that declaration.
	SourceMemoryBaseDeclared = "memory base declared by the operator"
	// SourceCommandChangeDir: a directory the COMMAND ITSELF selected with a
	// leading `cd <path>` before the operation. It carries the same authority
	// as the session working directory (the operation runs there, so the tree
	// is the command's own), and it exists as its own source so a refusal can
	// tell the reader WHICH cd contributed the beam it allowed.
	SourceCommandChangeDir = "command cd directory"
)

// DefaultRoot returns the default worktree root, <home>/workspace. It is the
// root a repo gets when neither the settings table nor the environment declares
// one, and it is exactly the path GOPI.md has documented all along — so the
// default changes no behaviour, it only gives the sentence a single definition.
// HOME is resolved BEFORE the test pin is consulted, and that order is
// load-bearing: a process with no home has no operator root to leak into, so the
// pre-existing abstention (ErrNoHome) must stay reachable — the callers that
// assert `HOME="" ⇒ no root` keep their meaning (see the note in scratch_pin.go).
//
// The test pin (UnderTestRoot) is consulted on the DEFAULT branch alone, i.e.
// after the caller's own explicit choices have already had their turn (Rule 2:
// the explicit layer wins). See scratch_pin.go for why a test process must never
// resolve the operator's real workspace.
func DefaultRoot() (string, error) {
	home := os.Getenv(homeEnvName)
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", ErrNoHome
		}
		home = h
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return "", ErrNoHome
	}
	if pin := UnderTestRoot(); pin != "" {
		return pin, nil
	}
	return filepath.Join(filepath.Clean(home), WorkspaceDirName), nil
}

// homeEnvName is the variable os.UserHomeDir honours first, named so the
// reader is explicit about the precedence it depends on.
const homeEnvName = "HOME"

// Resolve returns the repo's worktree root: the ambient override when set (the
// sandbox hook — one reader, Rule 2), else the declared per-repo value, else
// DefaultRoot. declared is the settings value ("", when the repo's entry
// declares none).
//
// Precedence is ENV > declared > default: a test that must not touch a real
// workspace needs the override to win, and an operator declaring a per-repo
// path is choosing that repo's home. The returned Root carries the SOURCE so a
// refusal message can say which layer authorised the path.
func Resolve(declared string) (Root, error) {
	if raw := strings.TrimSpace(os.Getenv(WorktreeRootEnvName)); raw != "" {
		root, err := canonicalRoot(raw)
		if err != nil {
			return Root{}, err
		}
		return Root{Path: root, Role: RoleWorktree, Source: SourceSettingsEnv}, nil
	}
	if v := strings.TrimSpace(declared); v != "" {
		root, err := canonicalRoot(v)
		if err != nil {
			return Root{}, err
		}
		return Root{Path: root, Role: RoleWorktree, Source: SourceSettingsKey}, nil
	}
	root, err := DefaultRoot()
	if err != nil {
		return Root{}, err
	}
	return Root{Path: root, Role: RoleWorktree, Source: SourceDefault}, nil
}

// canonicalRoot normalizes a declared root: ~ expansion, abs, clean. A declared
// root that cannot be made absolute is an error, never a silently-relative
// root — a relative root would authorise different paths in different cwds.
func canonicalRoot(value string) (string, error) {
	expanded, err := ExpandTilde(value)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		return "", &RelativeRootError{Root: value}
	}
	return filepath.Clean(expanded), nil
}

// RelativeRootError reports a declared root that is not absolute.
type RelativeRootError struct{ Root string }

func (e *RelativeRootError) Error() string {
	return "worktree root " + e.Root + " is not an absolute path (a relative root authorises different paths in different working directories)"
}

// ExpandTilde resolves a leading "~" against the home directory; every other
// path is returned unchanged. It is the ONE tilde reader, so a declared root
// spelled "~/workspace" and the same root spelled absolutely compare equal.
func ExpandTilde(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ErrNoHome
	}
	if path == "~" {
		return filepath.Clean(home), nil
	}
	return filepath.Join(filepath.Clean(home), path[2:]), nil
}

// Permits reports whether path lies inside root (the root itself included).
// Both spellings of each side are compared — the literal one and the
// symlink-resolved one — because the kernel resolves paths (getcwd(2) returns
// the resolved form) while a declaration may be written through a link
// (<scratch-home> → a symlinked target). Comparing only one spelling is the
// classic false refusal (verified live on this host, where the scratch home was
// exactly such a symlink until the memory backing was retired on 2026-09-18).
//
// A missing target is not an error: a worktree path need not exist yet, and a
// nonexistent path has no symlink to resolve, so the literal comparison stands.
func Permits(root Root, path string) bool {
	if root.Path == "" || root.Role == "" {
		return false
	}
	if path == "" {
		return false
	}
	for _, candidate := range spellings(root.Path) {
		for _, target := range spellings(path) {
			if Within(target, candidate) {
				return true
			}
		}
	}
	return false
}

// PermitsAny reports whether any root permits path, returning the first match
// (so the caller can name WHICH root authorised it — Rule 15's positive half,
// the same way refusals name the roots that were consulted).
func PermitsAny(roots []Root, path string) (Root, bool) {
	for _, r := range roots {
		if Permits(r, path) {
			return r, true
		}
	}
	return Root{}, false
}

// spellings lists the spellings a holder or a declaration may carry for path:
// the cleaned literal and its symlink-resolved form when they differ.
func spellings(path string) []string {
	clean := filepath.Clean(path)
	out := []string{clean}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" && resolved != clean {
		out = append(out, resolved)
	}
	return out
}

// Within reports whether path is target itself or nested under it. It compares
// cleaned paths component-wise — a prefix STRING match would call
// "/workspace-old" a child of "/workspace". A root of "/" or "" never claims
// anything (the whole filesystem is not a root).
func Within(path, target string) bool {
	if target == "" || target == "." || target == string(filepath.Separator) {
		return false
	}
	path = filepath.Clean(path)
	if path == target {
		return true
	}
	return strings.HasPrefix(path, target+string(filepath.Separator))
}
