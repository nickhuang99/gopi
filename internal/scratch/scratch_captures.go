package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// scratch_captures.go — WHERE a capture with no `--out` lands, and WHO collects
// it.
//
// THE DEFECT THIS CLOSES (measured 2026-11-07, task 01a1b337 follow-up
// 01a1b3b1). Every capture face — `gopi tool capture`, `gopi tool
// screen_capture`, `gopi tool shot`, and the model's own `screen_capture` tool
// (all four reach ONE writer per PNG family) — defaulted `--out` to a BARE
// RELATIVE NAME (`screen-<unix>.png` / `shot-<unix>.png`). `os.WriteFile` was
// handed the un-absolutised name (`filepath.Abs` only fed the reported JSON), so
// the file landed in the PROCESS'S CWD — and a task run's cwd is its own clone
// ground. Every capture therefore left an untracked PNG in a git work tree,
// which is PROJECT.md Rule 30's exact defect: the close-out records
// `completed-unclean`, and the debris is picked up by no merge path.
//
// WHY NOT THE SESSION DIRECTORY (the obvious first answer, and the reason it is
// REFUSED here). `<gopi dir>/sessions/<task-id>/` would be off every git tree,
// but it is bounded by NOTHING that would collect a PNG: the declared rows that
// root there take `*.raw.log` (session-raw), the legacy residue shapes, and the
// per-task `attachments/` archives — a fleet of `<stamp>.png` files is a NEW
// family with no collector, in a tree measured at 124 GB on this host. Moving
// the residue there would trade "pollutes a git tree" for "pollutes a directory
// nothing reclaims", which is worse: the second is invisible to every check.
//
// WHY NOT "THE FILE NEVER EXISTS" (base64 in the return value). The model-facing
// contract is that `screen_capture` answers a PATH and the next step is
// `vision_analyze <path>` (the tool's own hint says so, and vision_analyze takes
// a local path). A capture that never hit the disk could not be read back by the
// very tool it exists for. So the file MUST survive at least the next turn.
//
// THE ANSWER, THEREFORE: land it in a DUMP CLASS TREE that an existing GC arm
// already walks. `DeclaredTrees()` is the ONE declared set the sweep
// (`gopi tool scratch-gc trees`, and scratchGCTick's resident pass) visits; a
// dump class tree is exactly the shape of "a file a writer drops and never reads
// back, collected once it is older than the window". This file adds the capture
// class beside the two dump classes that already exist, with the SAME window
// (ScratchDumpsStaleAge) and the same collect path (removeCollectableOfTree
// through the generic walk) — NOT a new sweep, NOT a new script, NOT a crontab
// (Rule 35: the reclaim owner is named, and it is one that already ticks).
//
// WHAT A FUTURE READER MUST NOT DO: give this class its own reclaim leg. The row
// below is enough because it is appended to DeclaredTrees(); a second mechanism
// would be the shape Rule 35 exists to stop (an artifact that outlives its
// reason, with nobody able to say who collects it).

const (
	// DumpClassCapture is the class directory captures land in —
	// `<scratch home>/dumps/gopi-captures` (the join `DumpDir` makes, exactly
	// like the two dump classes). Named once: the writer below, the declared
	// row's root resolver and the sweep's report line all reference it.
	DumpClassCapture = "gopi-captures"

	// CaptureTreeName is this tree's identity on the watchdog log line
	// (`tree=`), in the declared-set gate's diagnostics and in `--tree`'s
	// selector. One definition (Rules 13/14).
	CaptureTreeName = "scratch-captures"

	// CaptureDumpFileSuffix is the extension a default-named capture carries.
	//
	// IT IS NOT `.png`, AND THAT IS THE SAFETY PROPERTY. The reclaim predicate
	// must be able to say "this file is OURS to collect" from the NAME alone,
	// and `dumps/gopi-captures/` is a directory a human may also drop a PNG into
	// (it is on the operator's own filesystem, and an operator tidying up is not
	// going to read this comment first). `<stem>.dump.png` is a namespace only
	// this writer produces, so a hand-placed `diagram.png` beside it is NEVER a
	// candidate — the same reasoning `siblingDumpFileSuffix` (`.dump.json`)
	// carries for the dump classes.
	CaptureDumpFileSuffix = ".dump.png"
)

// CaptureStaleAge is how long an unread default-named capture may sit before the
// sweep may collect it.
//
// IT IS THE DUMP WINDOW ON PURPOSE (its own NAME, the same VALUE, the same
// reason `ScratchDumpsStaleAge` is a separate constant from
// `SiblingDumpsStaleAge`): a capture is replay material for a step that has
// already happened — the model reads it on the next turn and never again — and
// two weeks is thousands of times the longest turn. The window is generous
// because the FAILURE it guards against (collecting a capture a live run is
// about to read) costs a run, while the failure it accepts (a stale capture
// costing a few KB until the next pass) costs nothing.
const CaptureStaleAge = ScratchDumpsStaleAge

// captureStemPattern is the alphabet a default-named capture's stem may use: the
// `screen`/`shot` prefix, an optional `-<pid>`-shaped discriminator, and the
// unix stamp. It is a PATTERN rather than a prefix test because the reclaim's
// whole safety argument is "only names this writer produces are candidates".
var captureStemPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*[0-9]$`)

// IsCaptureDumpName reports whether a file NAME is a default-named capture this
// writer created, i.e. whether the capture class may collect it. It is the row's
// namespace predicate AND the writer's own name check (Rule 14: one definition,
// so the writer cannot produce a name its collector refuses).
func IsCaptureDumpName(name string) bool {
	stem, ok := strings.CutSuffix(name, CaptureDumpFileSuffix)
	if !ok || stem == "" {
		return false
	}
	return captureStemPattern.MatchString(stem)
}

// CaptureDumpRoot resolves the class directory a captured PNG lands in, WITHOUT
// creating it — the contract the other class-tree resolvers honour (a sweep that
// materialised the tree it bounds would make "captures have never been taken on
// this host" and "the tree is empty" render the same).
func CaptureDumpRoot() (string, error) {
	if HomeDir() == "" {
		return "", nil
	}
	return ScratchDumpsClassRoot(DumpClassCapture)
}

// CaptureDumpFile resolves and CREATES the destination of a capture taken with no
// `--out`, returning the ABSOLUTE path.
//
// prefix names the family (`screen-`, `shot-`) so a reader of the directory can
// tell which face wrote a file; the unix stamp keeps two captures in one second
// apart by the caller's own collision handling below.
//
// FAILURE IS REPORTED, NEVER SWALLOWED (Rule 15). A scratch home that cannot be
// created for a capture is worth an error: the caller is mid-conversation about
// pixels and needs to know its picture is not where the tool claims.
func CaptureDumpFile(prefix string) (string, error) {
	dir, err := CaptureDumpRoot()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("capture: no scratch home to write into (scratch.HomeDir is empty)")
	}
	if err := MkdirAllWithSuiteRunOwnerPerm(dir, DirPerm); err != nil {
		return "", fmt.Errorf("capture: create %s: %w", dir, err)
	}
	stem := prefix + strconv.FormatInt(time.Now().Unix(), 10)
	path := filepath.Join(dir, stem+CaptureDumpFileSuffix)
	// Two captures inside the same second (a --window then an --area sweep) must
	// not overwrite each other: the second gets a numeric discriminator, which
	// IsCaptureDumpName still accepts (its pattern ends in a digit).
	for n := 2; ; n++ {
		if _, serr := os.Lstat(path); os.IsNotExist(serr) {
			return path, nil
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, n, CaptureDumpFileSuffix))
		if n > captureNameCollisionLimit {
			return "", fmt.Errorf("capture: no free file name in %s after %d attempts", dir, captureNameCollisionLimit)
		}
	}
}

// captureNameCollisionLimit bounds the collision loop above. It is a named
// constant (Rule 1) and it is SMALL on purpose: more than a handful of captures
// sharing one second is a sign of something else wrong, and an unbounded loop
// would spin forever on a directory the process cannot write.
const captureNameCollisionLimit = 64

// captureDumpTree is the class tree's declared row: the SHAPE the dump classes
// already use (files carrying ONE suffix, judged by the shared age rule), rooted
// at the class directory the writer creates.
//
// The class is TreeClassErrorDump because the FAMILY is the same one — "a file a
// writer drops into a class directory and nobody reads back" — and a reader of
// `scratch-gc trees` output must be able to see this row beside the two dump
// rows it is a sibling of. The row's NAME is what distinguishes it.
func captureDumpTree() DeclaredTree {
	return DeclaredTree{
		Name:       CaptureTreeName,
		Class:      TreeClassErrorDump,
		Kind:       EntryKindFileSuffix,
		Suffix:     CaptureDumpFileSuffix,
		StaleAge:   CaptureStaleAge,
		EntryMatch: IsCaptureDumpName,
		Resolve:    CaptureDumpRoot,
	}
}

// CaptureDumpTrees returns the capture class rows. It is the ONE place they are
// assembled (DeclaredTrees appends this), so a caller that needs the set reads
// the same construction the sweep walks rather than a second spelling of it.
func CaptureDumpTrees() []DeclaredTree {
	return []DeclaredTree{captureDumpTree()}
}
