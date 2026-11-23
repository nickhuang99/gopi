// Memory-base roots for the worktree sweep — the implicit declared root that
// closes the `gopi-verify-<task-id>` leak.
//
// THE DEFECT THIS FILE CLOSES (measured 2026-09-15, epic
// fix/worktree-sweep-memory-base-policy): verification and scratch worktrees are
// created directly under the MEMORY BASE (<home>/workspace/gopi-test-scratch is
// a symlink onto /dev/shm, scripts/setup-scratch-home.sh) — the `gopi-verify-*`
// family with its -wt / -base / -prefix / -merged / -master-base variants. Those
// are git worktree REGISTRATIONS, and every path-policy root the sweep consults
// (the repo's declared worktree_root, the scratch home, os.TempDir(), the repo
// tree) named a DIFFERENT directory, so rung ① reported each of them "outside
// every declared root — not swept" and no sweeper ever reclaimed them. 3/9
// residue cases, ~163 MB of tmpfs RAM
// (devdoc/worktree-sweep-memory-base-host-fixture.md).
// /dev/shm hit 100% on 2026-09-15 and broke suites with
// `database or disk is full (13)`.
//
// WHY A MEASUREMENT AND NOT A DECLARATION (Rule 22): the fix is NOT "add
// /dev/shm to the root list". /dev/shm is a WORLD-WRITABLE tmpfs shared with the
// rest of the host — a root spelled from a constant would authorise the REMOVAL
// of any registration under it, including one a neighbour made, and would still
// be wrong on a host whose memory base is somewhere else entirely. So the base
// is DERIVED from the scratch home by MEASURING the filesystem it resolves onto:
// walk up from the resolved scratch home while each ancestor shares the home's
// own device id AND that device is a tmpfs — never `/`. A foreign directory in
// the base is inside the ROOT but is not a registration, so it can never be a
// candidate (blast-radius constraint: only git registrations are judged).
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// memoryBaseSourcePrefix labels the role/source a DERIVED memory-base root
	// carries, so a finding's reason can NAME where the root came from (the
	// positive half of Rule 15 — the same way a refusal names the roots it
	// consulted). The role value itself comes from the shared registry
	// (worktreeroots), never a literal here (Rule 1b/14).
	memoryBaseSourcePrefix = worktreeroots.SourceMemoryBaseMeasured
	// memoryBaseDeviceProbePath is `/` — the partition a memory base must NOT be
	// on (Rule 22's criterion). Named so no call site spells the bare path.
	memoryBaseDeviceProbePath = "/"
	// memoryBaseMaxProbeDepth bounds the ancestor walk: a memory base is the
	// MOUNT POINT of a tmpfs and git registrations sit directly in it, so the
	// answer is at most a couple of levels above the scratch home. The bound is
	// what keeps a pathological depth from turning one policy lookup into a long
	// walk.
	memoryBaseMaxProbeDepth = 8
)

// memoryBaseRoot resolves the implicit root a scratch home implies: the nearest
// tmpfs ancestor the home resolves onto, EXCLUDING `/` (a root of `/` claims
// everything, which is the opposite of a policy).
//
// The result is a worktreeroots.Root with the MEASURED role
// (worktreeroots.RoleMemoryBase) and a source that names the measurement, so a
// reader of a sweep finding can tell a derived root from a declared one. ok is
// false when the home resolves onto no memory filesystem at all (a disk-backed
// scratch home — the shape where this root must NOT exist, because the sweep
// would otherwise claim a directory nobody chose).
func memoryBaseRoot() (worktreeroots.Root, bool) {
	home := scratch.CanonicalPath(scratch.HomeDir())
	if home == "" {
		return worktreeroots.Root{}, false
	}
	device, err := scratch.DeviceID(home)
	if err != nil {
		// An unmeasurable home implies NO root: "cannot tell" is never "authorise"
		// (Rule 15). The sweep then behaves exactly as it did before this file.
		return worktreeroots.Root{}, false
	}
	rootDevice, err := scratch.DeviceID(memoryBaseDeviceProbePath)
	if err != nil {
		return worktreeroots.Root{}, false
	}
	// A home ON `/` is not a memory base; /tmp on a default host is exactly this
	// shape and is already covered (or refused) by its own root.
	if device == rootDevice {
		return worktreeroots.Root{}, false
	}
	base, ok := memoryBaseAncestor(home, device)
	if !ok {
		return worktreeroots.Root{}, false
	}
	return worktreeroots.Root{
		Path:   base,
		Role:   worktreeroots.RoleMemoryBase,
		Source: memoryBaseSourcePrefix,
	}, true
}

// memoryBaseAncestor walks up from dir while every ancestor is on the SAME
// device (device). It returns the highest ancestor that still shares the device
// — the mount point a tmpfs-backed tree hangs off — or ok=false when that
// ancestor is `/` (which would claim the whole filesystem).
func memoryBaseAncestor(dir string, device uint64) (string, bool) {
	base := filepath.Clean(dir)
	for depth := 0; depth < memoryBaseMaxProbeDepth; depth++ {
		parent := filepath.Dir(base)
		if parent == base {
			break // the filesystem root
		}
		parentDevice, err := scratch.DeviceID(parent)
		if err != nil || parentDevice != device {
			break // a different filesystem: base is this device's top
		}
		base = parent
	}
	if base == memoryBaseDeviceProbePath {
		return "", false
	}
	if _, ok := memoryFilesystemKind(base); !ok {
		// The candidate is not a memory filesystem, so it is not a memory base:
		// a home that lives on the same non-root device as its parent (a
		// separate disk mount) must NOT be swept on the strength of device
		// equality alone.
		return "", false
	}
	return base, true
}

// memoryFilesystemKind classifies the filesystem at path by the statfs magic
// number, using the ONE definition of those magics in internal/scratch
// (Rule 14 — a second copy here would be drift waiting to happen).
//
// IT CALLS scratch.FilesystemTypeAt RATHER THAN SPELLING STATFS ITSELF, and that
// is a portability fix, not only a de-duplication: this call site read
// `st.Type` directly, whose type is int64 on linux and uint32 on darwin, so the
// bare form did not compile off linux ("cannot use st.Type (variable of type
// uint32) as int64 value"). scratch owns the per-platform statfs binding
// (internal/scratch/tmproot_statfs_{linux,darwin,other}.go), and going through
// it means the NEXT platform needs a change in exactly one place.
func memoryFilesystemKind(path string) (string, bool) {
	return scratch.FilesystemTypeAt(path)
}

// memoryBaseDeclaredBase returns the base a DECLARATION names, when the scratch
// home resolves onto it: `GOPI_SCRATCH_MEMORY_BASE` (host configuration — the
// variable the retired scripts/setup-scratch-home.sh mounted, still read as the
// LEGACY sweep's base knob), or the settings.json "tmpdir"
// key (Rule 22's explicit layer). "" when neither applies.
//
// It exists so the reported root can carry the operator's OWN spelling whenever
// one is knowable — a finding that says "/dev/shm (declared by
// GOPI_SCRATCH_MEMORY_BASE)" is actionable in a way "the tmpfs your scratch home
// resolves onto" is not.
func memoryBaseDeclaredBase(home string) string {
	candidates := []string{strings.TrimSpace(os.Getenv(scratch.MemoryBaseEnvName))}
	if settingsTmpDir() != "" {
		candidates = append(candidates, settingsTmpDir())
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		abs := scratch.CanonicalPath(candidate)
		if !filepath.IsAbs(abs) {
			continue
		}
		if worktreeroots.Within(home, abs) {
			return abs
		}
	}
	return ""
}

// settingsTmpDir reads the settings.json temp-workspace key — the same key
// cmd/agent injects into gopi's own environment (Rule 22). A settings file that
// cannot be read yields "" (no declaration), never an error: the DERIVED
// measurement above is the authority, the declaration only supplies a name.
func settingsTmpDir() string {
	return scratch.DeclaredTmpDir()
}

// memoryBaseRootSkipped reports the reason a scratch home did NOT yield an
// implicit root, for the operator's report. It exists because the sweep's whole
// value is that its findings are actionable: an ungovernable registration is
// reported WITH the measurement that decided it.
func memoryBaseRootSkipped(home string) string {
	device, err := scratch.DeviceID(home)
	if err != nil {
		return fmt.Sprintf("the scratch home %s could not be measured (%v)", home, err)
	}
	rootDevice, err := scratch.DeviceID(memoryBaseDeviceProbePath)
	if err != nil {
		return fmt.Sprintf("%s could not be measured (%v)", memoryBaseDeviceProbePath, err)
	}
	if device == rootDevice {
		return fmt.Sprintf("the scratch home %s is on the same filesystem as %s", home, memoryBaseDeviceProbePath)
	}
	base, ok := memoryBaseAncestor(home, device)
	if !ok {
		return fmt.Sprintf("the scratch home %s resolves onto no memory filesystem", home)
	}
	return fmt.Sprintf("the scratch home %s resolves onto %s, which is not a memory filesystem", home, base)
}

// fsFileMode is a tiny alias so the probe below reads as a mode check, not as a
// bare integer comparison (Rule 1b).
// memoryBaseDeclaredRoot returns base labeled with the operator's own
// declaration when one exists: GOSCRATCH base env or the settings.json "tmpdir"
// key, whichever the scratch home was measured to resolve onto. The path is
// always the MEASURED one — a declaration only ever supplies the label
// (Rule 22: the declaration is a convenience, the measurement is the authority).
func memoryBaseDeclaredRoot(base worktreeroots.Root) worktreeroots.Root {
	home := scratch.CanonicalPath(scratch.HomeDir())
	declared := memoryBaseDeclaredBase(home)
	if declared == "" || scratch.CanonicalPath(declared) != base.Path {
		return base
	}
	base.Source = worktreeroots.SourceMemoryBaseDeclared
	return base
}
