// sibling_dumps.go — the declared tree that BOUNDS the gopi PROFILE's `dumps/`
// directory: one of the SIBLING trees that live beside the sessions tree (see
// sessionlayout.SiblingTrees for the whole set).
//
// THE TREE, AND WHY IT HAS NO WRITER IN THIS REPOSITORY. The dump writers
// (internal/deepseek's writeDumpFile, reached from minimalCoreDumpDir and
// guardDumpDir) document their default as the SCRATCH dumps class tree
// (`internal/scratch.DumpDir(<class>)` — `<scratch home>/dumps/…`, deliberately
// never on `/`: devdoc/tmp-ban-scratch-root.md). That default is honoured. The
// PROFILE's `dumps/` nevertheless grows, because the writers' FIRST-ranked
// source is an environment variable — `GOPI_ERROR_DUMP_DIR`, read in exactly ONE
// place (minimalCoreDumpDir) — and an operator may export it from a shell
// profile, where every live gopi and every child it spawns inherits it. Measured
// on this host (2026-10-01): `~/.bashrc:139` exports
// `GOPI_ERROR_DUMP_DIR=/home/user/.gopi/dumps`, and the tree holds 4,345 dumps /
// 102 MiB. THE PATH THAT WRITES THIS TREE IS THE ENV OVERRIDE, NOT A CODE PATH.
//
// That fact decides the SHAPE of the fix. Changing the writer would override the
// operator's own declared choice about where their diagnostics live — the
// variable exists so they can make that choice — so the repository's job is to
// (a) state the precedence correctly where it documents the knob (CONFIG.md) and
// (b) BOUND the tree it is given, through machinery that already ticks. This
// file is (b): one declared-tree row in the set the existing sweep reads
// (`gopi tool scratch-gc trees`, the host watchdog and the resident worker/merger
// tick already drive it). No new script, cron, daemon or state file (Rule 35).
//
// WHY DUMPS ARE COLLECTABLE AT ALL. A dump is written BEFORE a request fails, is
// never read back by the writer, and exists to be replayed while the failure is
// being diagnosed. The family registry therefore decides it `bound:` — and the
// namespace predicate is the whole safety story: only a name that IS a request
// dump's own timestamp name (`20261001-104445.dump.json`, the collision form
// `20261001-104441-2.dump.json`) is ever a candidate. Anything else the operator
// parks in that directory — a `.txt` note, an unpacked tarball, a directory — is
// not ours and is never a candidate, whatever its age.
//
// The window is generous ON PURPOSE (SessionDumpsStaleAge, file-scoped like the
// sessions tree's raw logs rather than the 24h the toolchain work directories
// get): a dump is the only record of a failure somebody may still be reading.
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// SiblingDumpsTreeName is the declared tree's identity: the logfmt `tree=`
	// field on the watchdog line, the name the declared-set gate diagnoses with,
	// and the tree a sessionlayout `bound:` family names. The family registry
	// cannot import this package (importing it pulls in internal/tools, whose own
	// client of this package makes that a cycle), so this constant's VALUE is
	// held against the registry's (sessionlayout.SiblingDumpsTree) by a pin that
	// reads BOTH sources — a pair of literals with nothing joining them is
	// exactly the drift Rules 13/14 ban.
	SiblingDumpsTreeName = "sibling-dumps"

	// SiblingDumps1214TreeName is the SECOND dump tree's identity: the same
	// shape for the provider's 1214 error class, whose own knob
	// (GOPI_1214_DUMP_DIR, read once in internal/deepseek.guardDumpDir) can point
	// a second directory under the profile. Measured on this host: exported from
	// the same shell file, EMPTY today — which is exactly why it is registered:
	// the registry describes the RANGE of locations these knobs can create, and
	// an empty tree on one host is not a reason to leave the knob's target
	// uncensused. It is a SEPARATE row rather than a second root on the first,
	// because the two knobs answer to different guards and an operator may point
	// one at the profile and not the other.
	SiblingDumps1214TreeName = "sibling-dumps-1214"

	// SiblingDumpsDirName is the tree's own leaf name under the gopi profile
	// root — the directory the tree roots at, and the directory the env dump
	// override points at when an operator configures it (measured:
	// GOPI_ERROR_DUMP_DIR=/home/user/.gopi/dumps).
	SiblingDumpsDirName = "dumps"

	// SiblingDumps1214DirName is the second tree's leaf name (measured:
	// GOPI_1214_DUMP_DIR=/home/user/.gopi/dumps-1214).
	SiblingDumps1214DirName = "dumps-1214"
)

// SiblingDumpsStaleAge is how long a request dump may sit in the profile's
// `dumps/` tree before the sweep may collect it.
//
// It is FILE-scoped and LONG (14 days, the same window the sessions tree gives
// its own per-run raw HTTP logs, and for the same reason): a dump is replay
// material for a failure that has already been diagnosed, so the cost of
// keeping one an extra fortnight is a few kilobytes, while the cost of having
// collected the one dump an investigation needed is that the investigation
// cannot finish. The bash-profile knob that writes this tree also means the
// operator sees the directory in their own file browser — a window short enough
// to make yesterday's dump vanish would be a surprise, not a service.
const SiblingDumpsStaleAge = 14 * 24 * time.Hour

// siblingDumpsTree is the declared row: the profile's `dumps/` directory, whose
// only candidates are request dumps, whose window is SiblingDumpsStaleAge.
//
// It is an ordinary generic-walk row (no List/Sweep override): its Kind is
// EntryKindFileSuffix, so the sweep's own walk applies the hard rule — the
// entry's own mtime, the live-holder veto from the ONE /proc pass, the
// `.holder` registration, the "a declared root is never a candidate" refusal and
// the removal re-check — exactly as it does for the flat raw-log tree. A second
// implementation of any of those would be a second answer.
func siblingDumpsTree() DeclaredTree {
	return siblingDumpTree(SiblingDumpsTreeName, SiblingDumpsDirName)
}

// siblingDumps1214Tree is the SECOND dump tree's declared row: the profile's
// `dumps-1214/` directory — the same shape, the same family, the same window,
// pointed at by the other dump knob (GOPI_1214_DUMP_DIR). It shares this
// implementation rather than copying it, because the two trees differ in exactly
// two values (a name and a leaf) and in nothing else: a second walk, a second
// predicate or a second age would be a second answer to the same question.
func siblingDumps1214Tree() DeclaredTree {
	return siblingDumpTree(SiblingDumps1214TreeName, SiblingDumps1214DirName)
}

// siblingDumpTree builds one dump tree's declared row from its two distinguishing
// values. Everything else about the row is shared on purpose (see the caller).
func siblingDumpTree(name, dirName string) DeclaredTree {
	return DeclaredTree{
		Name:       name,
		Class:      TreeClassErrorDump,
		Kind:       EntryKindFileSuffix,
		Suffix:     siblingDumpFileSuffix,
		StaleAge:   SiblingDumpsStaleAge,
		EntryMatch: isSiblingDumpName,
		Resolve:    siblingDumpRootResolver(dirName),
	}
}

// siblingDumpRootResolver returns ONE dump tree's root resolver: the gopi
// PROFILE root (through the seam internal/config wires) plus that tree's own leaf
// name. The leaf is a PARAMETER rather than a second function, so the two rows
// cannot resolve to the same directory by a copy-paste slip.
func siblingDumpRootResolver(dirName string) func() (string, error) {
	return func() (string, error) {
		dir, err := siblingDataRootResolver()
		if err != nil {
			return "", err
		}
		if dir == "" {
			return "", nil
		}
		return CanonicalPath(filepath.Join(dir, dirName)), nil
	}
}

// siblingDumpFileSuffix is the suffix every request dump carries — the ONE name
// family this tree may take. Its value is internal/deepseek's dumpFileSuffix,
// re-spelled here because that package imports THIS one (through its own
// scratch.DumpDir call), so the constant cannot be imported back; the equality
// of the two is asserted by this package's own boundary test, which reads both
// sources.
const siblingDumpFileSuffix = ".dump.json"

// SiblingDumpsRoot returns the `dumps` dump tree's root. It is the row's own
// resolver, exposed so a caller (the fingerprint preflight below, and a test)
// asks the SAME function the sweep walks rather than re-joining the path.
//
// The directory is NOT created here. A sweep that materialised the tree it
// bounds would make "the operator never pointed the dump knob at their profile"
// and "the knob points there and the tree is empty" render the same; the walk
// reports the missing root as UNCONFIGURED instead, and the CLI reports that
// without raising an alarm (a declared tree need not exist on this host).
func SiblingDumpsRoot() (string, error) {
	return siblingDumpRootResolver(SiblingDumpsDirName)()
}

// RegisterSiblingDataRootResolver installs the gopi-profile-root resolver this
// package's sibling tree hangs off. internal/config calls it once from its own
// init, passing config.GopiDir — the ONE resolution of the profile root, so the
// sibling trees of the profile are never spelled twice (Rule 14).
//
// It is the SAME resolution the family registry's own seam takes
// (sessionlayout.RegisterSiblingDataRootResolver): internal/config owns the
// profile root, both registries declare the trees they hang off it, and neither
// imports the other.
func RegisterSiblingDataRootResolver(f func() (string, error)) {
	if f != nil {
		siblingDataRootResolver = f
	}
}

// siblingDataRootResolver is the function seam above. Unwired, it returns a
// NAMED error rather than an empty path: a sweep that resolved "" would walk the
// process's own working directory (the failure mode the raw-log trees'
// TestTreeResolversAreWired exists to refuse), and the row's resolver check in
// TestDeclaredTreesAreWiredAndNamed reads this error by name.
var siblingDataRootResolver = func() (string, error) {
	return "", fmt.Errorf("scratch: profile-root resolver is not wired (import internal/config)")
}

// isSiblingDumpName is the tree's NAMESPACE PREDICATE: a request dump's own
// timestamp name and nothing else. It is the same rule the family registry
// classifies the shape with (sessionlayout's isDumpTimestampName), spelled here
// because the registry is reachable from this package only through the
// cross-package pin that reads both sources — and a predicate that admitted a
// name the registry calls unclassified, or the reverse, is exactly the drift
// that pin exists to catch.
//
// The name is `<stamp>.dump.json` or `<stamp>-<n>.dump.json`: the stamp is eight
// digits, a dash, six digits (internal/deepseek's dumpNameTimeLayout), and the
// optional `-n` is the collision suffix writeDumpFile appends when two dumps
// land in the same second.
func isSiblingDumpName(name string) bool {
	stem, ok := strings.CutSuffix(name, siblingDumpFileSuffix)
	if !ok || stem == "" {
		return false
	}
	return strings.Trim(stem, siblingDumpNameDigits) == ""
}

// siblingDumpNameDigits is the alphabet of a dump name's stamp part: the ten
// digits of the timestamp and the dash between them.
const siblingDumpNameDigits = "0123456789-"

// SiblingDumpsFingerprint is the preflight for the `dumps` tree: how many entries
// of it are request dumps, and how many bytes they hold, WITHOUT planning or
// deleting anything.
//
// WHY IT EXISTS AS ITS OWN FUNCTION: `gopi tool scratch-gc families --tree dumps`
// reports the tree's SCOPE (one line per shape, with the family and the
// decision), while `gopi tool scratch-gc trees --dry-run` reports the BOUND (which
// entries the sweep would collect). A third question — "does the bound actually
// see this tree at all?" — is what a reader asks when the census says 4,345
// entries and the dry run says 0, and it is answered by the tree's OWN predicate
// and the tree's OWN root, not by a second one. A predicate that stopped matching
// the writer's names shows up here as 0.
func SiblingDumpsFingerprint() (files int, bytes int64, err error) {
	return siblingDumpFingerprint(siblingDumpsTree())
}

// siblingDumpFingerprint counts one dump tree's own candidates at its own root.
func siblingDumpFingerprint(tree DeclaredTree) (files int, bytes int64, err error) {
	root, rerr := tree.Resolve()
	if rerr != nil {
		return 0, 0, rerr
	}
	if root == "" {
		return 0, 0, fmt.Errorf("%w: the gopi profile root could not be resolved", ErrDeclaredTreeUnconfigured)
	}
	entries, derr := os.ReadDir(root)
	if derr != nil {
		if os.IsNotExist(derr) {
			return 0, 0, fmt.Errorf("%w: no %s tree at %s", ErrDeclaredTreeUnconfigured, tree.Name, root)
		}
		return 0, 0, fmt.Errorf("read %s (%s): %w", tree.Name, root, derr)
	}
	for _, e := range entries {
		if e.IsDir() || !tree.EntryMatch(e.Name()) {
			continue
		}
		fi, ierr := e.Info()
		if ierr != nil {
			continue // unmeasurable: never guessed at (Rule 15)
		}
		files++
		bytes += fi.Size()
	}
	return files, bytes, nil
}
