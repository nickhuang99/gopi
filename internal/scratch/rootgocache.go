// Go build cache placement probe — the ONE place that answers "does this shared
// build cache live on the `/` partition?", i.e. whether an unbounded cache may
// be created there.
//
// ROOT CAUSE THIS FILE EXISTS FOR (2026-09-15): the merge-time BUILD gate
// pointed GOCACHE at a per-PROCESS directory inside the tmpfs scratch root
// (<scratch>/queue/queue-runtime-merge-build-<pid>/gocache). Every gate call —
// the branch's build and, on a failure, master's — wrote 726M-1.14G there, the
// directory died with the PID, and /dev/shm reached 100% (measured 5.9G of a
// 7.8G tmpfs: 54 MB x 100+ dead cache copies). Moving that cache to disk is the
// fix; the SAME defect one level down is a "disk" path that resolves onto `/`,
// which is the 147G boot partition the 2026-09-11 exhaustion took from 19G to
// 1.5G free. So the placement is MEASURED, never inferred from a spelling
// (Rule 22: a criterion, not a value).
//
// WHY HERE (internal/scratch) AND NOT internal/tools: this package already owns
// the device-id measurement and its `/`-ban (tmproot.go /
// AssertTempRootOffRootFS) — the same statfs question, asked about a different
// path — and it is the LEAF both the gate and the suite can import (Rule 14:
// one definition, every consumer imports it; a second copy of the measurement is
// exactly the drift Rule 13 bans). The consumer is
// internal/tools/merge_build_gate_gocache.go.
package scratch

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	// GoCacheRootEnvName is the environment variable that overrides WHERE the
	// merge-time BUILD gate's shared Go build cache lives. It is declared in
	// this leaf package because TWO readers name it and must not drift: the gate
	// that reads it (merge_build_gate_gocache.go — the ONE reader, Rule 2) and
	// the refusal text below, which tells an operator which knob to turn.
	GoCacheRootEnvName = "GOPI_MERGE_BUILD_GATE_GOCACHE_ROOT"

	// sharedBuildCacheDirLeaf is the directory NAME under the worktree root that
	// holds the shared build caches. It is an ALIAS of
	// worktreeroots.SharedBuildCacheDirName — never a second definition
	// (Rules 13/14); it is spelled here because this package must not import
	// worktreeroots (that import would be a cycle: worktreeroots is a leaf and
	// imports nothing). The equality is machine-checked by
	// TestSharedBuildCacheLeafMatchesWorktreeRoots.
	//
	// worktree-root-ok: definition site of the leaf name; asserted equal to the
	// roots-package constant by the test named above.
	sharedBuildCacheDirLeaf = "gopi-build-cache"

	// goCacheOnRootReasonFmt / goCacheUnmeasurableReasonFmt render the two
	// refusals. They name the measured device, WHY an unbounded cache on the
	// boot partition is the incident this guards, and the lever that fixes it
	// (Rule 15: a refusal names its cause and its remedy).
	goCacheOnRootReasonFmt = "Go build cache root %q is on the SAME filesystem as %s (device 0x%x): an unbounded build " +
		"cache on the boot partition is the 2026-09-11 exhaustion (that partition went from 19G to 1.5G free), and the " +
		"tmpfs alternative is the 2026-09-15 one (/dev/shm at 100%%). Neither is acceptable. FIX: leave the cache where " +
		"it belongs by default (%s) by not pinning %s to a directory on %s"
	goCacheUnmeasurableReasonFmt = "Go build cache root %q: its filesystem cannot be measured — refusing to assume it is safe: %w"
)

// AssertGoCacheOffRootFS is the placement ban for a SHARED (cross-process,
// disk-backed) Go build cache: it measures the device of root against the device
// of `/` and refuses when they are the same filesystem.
//
// It is deliberately a MEASUREMENT and not a string check: the scratch home on
// this host is reached through a symlink, an operator may legitimately declare a
// root anywhere, and a spelling ("/home/...", "/tmp/...") is not evidence of a
// filesystem — DeviceID resolves symlinks and asks statfs, which is.
//
// root must ALREADY EXIST: creating the directory is the caller's job (the gate
// does it with MkdirAll and ABSTAINS when that fails), because a caller that has
// not created the tree has nothing to measure — and a cache the gate cannot
// measure is a cache it must not use (Rule 15b: ABSTAIN, never a silent
// fallback).
func AssertGoCacheOffRootFS(root string) error {
	clean := filepath.Clean(root)
	if strings.TrimSpace(root) == "" || clean == "." {
		return fmt.Errorf(goCacheUnmeasurableReasonFmt, root, errors.New("empty cache root"))
	}
	rootDev, err := RootDeviceID()
	if err != nil {
		return fmt.Errorf(goCacheUnmeasurableReasonFmt, clean, err)
	}
	// THE QUESTION IS ASKED ABOUT THE FILESYSTEM THAT WOULD HOLD THE PATH, not
	// about the path itself — so it is answerable BEFORE anything is created.
	// That ordering is the whole point: a caller that must not leave a refused
	// tree behind has to be able to refuse BEFORE its os.MkdirAll, and a
	// measurement that needs the directory to exist first makes that
	// impossible (measured 2026-10-01: the /dev/shm/<16-hex>/gocache family —
	// 171 trees — is exactly the residue a check-after-create leaves).
	//
	// This is the SAME rule IsTempRootOffRootFS already applies to a
	// not-yet-created sandbox (existingAncestor), reused rather than
	// re-derived. For a path that already exists it is the identity, so no
	// existing verdict changes.
	dev, err := DeviceID(existingAncestor(clean))
	if err != nil {
		return fmt.Errorf(goCacheUnmeasurableReasonFmt, clean, err)
	}
	if dev != rootDev {
		return nil
	}
	return fmt.Errorf(goCacheOnRootReasonFmt, clean, rootDeviceProbePath, dev,
		sharedBuildCacheHint(WorkspaceDirName), GoCacheRootEnvName, rootDeviceProbePath)
}

// MemoryFilesystem classifies a path by its MEASURED statfs type: true when the
// kernel reports a memory filesystem (tmpfs/ramfs) there, false with a
// description otherwise. internal/testutil.ScratchRootFilesystem is the
// suite-facing alias of the same classification (Rule 14) — this peer exists
// because a PRODUCTION caller (a placement assertion) must ask the question the
// suite asks without importing the test framework.
//
// An unreadable path is reported as an error, never guessed (Rule 15).
func MemoryFilesystem(path string) (bool, string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false, fmt.Sprintf("statfs(%s) failed: %v", path, err), err
	}
	switch uint64(st.Type) {
	case tmpfsMagic:
		return true, "tmpfs (memory filesystem)", nil
	case ramfsMagic:
		return true, "ramfs (memory filesystem)", nil
	}
	return false, fmt.Sprintf("block-backed filesystem type 0x%x", uint64(st.Type)), nil
}

// tmpfsMagic / ramfsMagic are the Linux statfs magic numbers of the two memory
// filesystems — the ONE definition of those values (Rule 14: internal/testutil
// carries the suite-facing aliases). Exported so a production caller in another
// package can classify without a second literal (Rules 1b/13).
const (
	TmpfsMagic = 0x01021994
	RamfsMagic = 0x858458f6

	tmpfsMagic = TmpfsMagic
	ramfsMagic = RamfsMagic
)

// AssertGoCacheOffShm refuses a SHARED build cache that lands on a memory
// filesystem, wherever that filesystem is mounted.
//
// It is separate from AssertGoCacheOffRootFS because the two failures are
// different incidents: a cache on `/` is the 2026-09-11 exhaustion, a cache on
// tmpfs is the 2026-09-15 one — and a memory filesystem is typically NOT on `/`
// (it is its own mount), so the device comparison alone cannot see it. The
// check is therefore by filesystem TYPE, measured, and it applies to ANY memory
// filesystem the cache may land on. The union of the two checks is what makes
// the shared cache's home unambiguous: on disk, off `/`, never RAM.
//
// LIKE ITS SIBLING, IT IS ANSWERABLE BEFORE THE CACHE EXISTS: the classification
// is taken of the filesystem that would HOLD the cache directory
// (existingAncestor), so a caller can refuse a RAM placement before its
// os.MkdirAll rather than after — the ordering the
// /dev/shm/<16-hex>/gocache family was minted by (see that check's comment).
// For a directory that already exists this changes nothing: the nearest
// existing ancestor of an existing path is the path itself.
func AssertGoCacheOffShm(cacheDir string) error {
	mem, description, err := MemoryFilesystem(existingAncestor(cacheDir))
	if err != nil {
		return fmt.Errorf(goCacheUnmeasurableReasonFmt, cacheDir, err)
	}
	if !mem {
		return nil
	}
	return fmt.Errorf(goCacheOnShmReasonFmt, cacheDir, description, GoCacheRootEnvName, sharedBuildCacheDirLeaf)
}

// goCacheOnShmReasonFmt renders the tmpfs refusal: the measured classification,
// WHY a reusable cache on RAM is the 2026-09-15 incident, and the lever.
const goCacheOnShmReasonFmt = "Go build cache root %q is on RAM-backed storage (%s): this is the 2026-09-15 " +
	"incident — the per-process cache wrote 726M-1.14G per build into /dev/shm and filled it (100%%, 5.9G of a " +
	"7.8G cap), while the filesystem could not be reclaimed from a cache that died with its process. FIX: point %s " +
	"at a DISK directory, or leave it unset so the cache uses <worktree root>/%s on disk"

// sharedBuildCacheHint is the path the cache uses by DEFAULT, as a
// reader-facing string for the refusal text above. It is built from the same
// values the gate builds its own default from (the workspace leaf and the cache
// leaf), so the message cannot drift from the behaviour (Rule 13). The leaf is
// rendered "~/<workspace>/<cache>" because the home is the reader's own and is
// not what the refusal is about.
func sharedBuildCacheHint(workspaceLeaf string) string {
	workspaceLeaf = strings.TrimSpace(workspaceLeaf)
	if workspaceLeaf == "" {
		return "~" + string(filepath.Separator) + sharedBuildCacheDirLeaf
	}
	return "~" + string(filepath.Separator) + filepath.Join(workspaceLeaf, sharedBuildCacheDirLeaf)
}

// SharedBuildCacheLeaf is the shared build cache directory name, exported for
// the equality test that pins it to worktreeroots.SharedBuildCacheDirName — the
// alias contract this package cannot express by import (see the constant).
func SharedBuildCacheLeaf() string { return sharedBuildCacheDirLeaf }
