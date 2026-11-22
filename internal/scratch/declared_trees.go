// declared_trees.go — THE DECLARED SET of trees this repository's own commands
// create, fill and never clean, together with the ONE age constant each of them
// is bounded by.
//
// WHY THIS FILE EXISTS (the defect it closes, measured 2026-09-20): the host's
// GC covered exactly ONE tree out of four. `gopi tool scratch-gc` swept the suite
// scratch home (<home>/workspace/gopi-test-scratch) and nothing else, so three
// trees this same repository writes were unprotected BY CONSTRUCTION:
//
//	~/workspace/gopi-go-tmp   (= GOTMPDIR)      54.5 GB, 1520 go-build* dirs
//	~/.gopi/raw/*.log                           27 GB,   1433 files
//	~/.gopi/sessions/**/*.raw.log               51.9 GB, 1185 files
//	~/workspace/gopi-build-cache                16.6 GB, 865 dirs
//
// All four were swept BY HAND into a one-off script (~105 GB freed, /home
// 73 G → 196 G free) and the growth returns within two weeks because nothing
// enforced the bound. The scope was the defect, and it was invisible because
// there was no place that SAID what the GC's scope was — a tree could be added
// by any producer and no check noticed.
//
// WHAT A DECLARED TREE IS, AND WHAT IT IS NOT:
//
//   - It is a tree this system OWNS, that its own commands FILL, and that
//     nothing bounds. Each one carries the age constant that bounds its
//     ENTRIES (never the tree itself).
//   - It is NOT a permission to delete the tree. THE ROOT OF EVERY DECLARED
//     TREE SURVIVES EVERY SWEEP, unconditionally — that is the invariant the
//     sibling brief (briefings/brief-declared-paths-survive-sweep.md) exists
//     for: `gopi-go-tmp` IS `GOTMPDIR`, a hand sweep deleted it (it was idle,
//     not a worktree, so it passed both guards) and the next build died with
//     `go: creating work dir: stat /home/user/workspace/gopi-go-tmp: no such
//     file or directory`. "Nothing is using it right now" is not "it is not
//     needed." Only a declared tree's AGED, UNHELD CHILDREN are ever
//     candidates, and removeCollectableOfTree re-checks the root identity
//     before every single delete.
//   - It is NOT a name-marker family. The scratch home is SHARED with
//     neighbours, so entries there must carry their namespace in their NAME
//     (IsScratchEntry). A declared tree is not shared: the TREE is the
//     namespace declaration (the queue's rule, one level up), so its direct
//     children are in-namespace by construction and are judged on age +
//     liveness. A `go-build*` directory needs no marker to be ours — `go` made
//     it, in the directory we pointed `go` at.
//
// HOW THE DECLARATION IS CHECKED (Rule: 原则无效，检查才有效): the declared set
// is machine-checked against the trees the repository's own writers actually
// resolve — TestDeclaredTreesCoverRepoWriters (declared_trees_test.go) reads
// each WRITER's own source and fails when a writer's tree is not declared, and
// TestGCTreesPlanCoversEveryDeclaredTree fails when the CLI's plan face stops
// reaching a declared tree. The red-proof for both is recorded in
// devdoc/scratch-gc-declared-trees.md.
package scratch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tree classes name the tree TYPES the declared set can hold. A class is what
// the gate inspects (the scratch home is deliberately absent — see the gate).
const (
	// TreeClassGoTmp is the Go toolchain's own work-directory tree: `go build`
	// creates `go-build*` children under GOTMPDIR and removes them when it is
	// asked to; a KILLED `go` (a timeout, a killed suite, a dead merger) leaves
	// its child behind forever, because nothing else knows the tree exists.
	TreeClassGoTmp = "go-tmp"
	// TreeClassErrorDump is a tree of ERROR-PATH REQUEST DUMPS: the body of a
	// request that failed, written before the failure is reported so it can be
	// replayed. It is its own kind because the candidates are neither raw logs
	// nor cache keys, and because its tree exists only when an operator points
	// the dump knob at their profile (sibling_dumps.go carries the measurement).
	TreeClassErrorDump = "error-dump"
	// TreeClassRawLog is a tree of BYTE-LEVEL HTTP logs — one directory of flat
	// files, or one file per session nested in the session tree.
	TreeClassRawLog = "raw-log"
	// The declared trees' NAMES — the identity that appears on the watchdog log
	// line (`tree=<name>`), in the gate's diagnostics and in the family
	// registry's `bound:` decision (internal/sessionlayout, which cannot import
	// this package). Each is spelled ONCE here and referenced (Rules 13/14):
	// before this block the session tree's row spelled "session-raw" inline and
	// the gate's red-proof spelled it again, so a family bound to a tree that
	// had been renamed would have read green forever.
	//
	// TreeNameBuildCache is the fourth tree's name and lives beside its own
	// declaration (declared_build_cache.go, BuildCacheTreeName): one value, one
	// home.
	//
	// TreeNameGoTmp is the Go toolchain work-directory tree.
	TreeNameGoTmp = "go-tmp"
	// TreeNameRaw is the legacy flat raw-log directory.
	TreeNameRaw = "raw"
	// TreeNameSessionRaw is the sessions tree's per-run raw HTTP log tree — the
	// ONE declared tree that covers part of the sessions tree, and the tree the
	// family registry's raw-http-log family is bound to.
	TreeNameSessionRaw = "session-raw"

	// TreeClassLegacy is the class of the sessions tree's LEGACY-RESIDUE rows
	// (declared_session_legacy.go): families that live inside the sessions tree
	// and that NO writer in this repository mints any more — the task-named
	// containers and root residue of the pre-session layout, a probe's
	// hand-made clone, a private profile materialised by hand. They are a class
	// of their own rather than `raw-log` because their SHAPES (a directory
	// removed whole; a file whose name is an exact shape table rather than one
	// suffix), their age window and their safety argument all differ: the
	// argument for taking one is that its WRITER is gone, not that it is an old
	// log.
	//
	// ONE name, not one per shape: the three rows are one decision about one
	// thing, and the SHAPE lives in each row's Kind and its namespace predicate.
	// A second class spelling would force a reader to learn a vocabulary that
	// carries no decision.
	TreeClassLegacy = "legacy"

	// TreeClassBuildCache is the SHARED Go build cache the merge-time BUILD
	// gate creates per repository checkout
	// (`<worktree root>/<SharedBuildCacheDirName>/<repo-key>/gocache`). It is
	// the only class whose WRITER is the merge path, and the only one whose
	// entries are thrown away when their checkout is: a key is a digest of the
	// repo's ABSOLUTE path, so every `git worktree add` mints a new one and
	// nothing ever removes it (see declared_build_cache.go).
	TreeClassBuildCache = "build-cache"
	// TreeClassSessionArchive is the sessions tree's per-task `attachments/`
	// archive family: the copy area the submit face fills (internal/tools'
	// TaskCopyAttachments) and the LAST surviving copy of evidence a report
	// cites. Its row is the only one whose bound is not an age window but an
	// EVIDENCE predicate — an ordinary, empty, unheld directory holds no
	// evidence and no bytes, and everything else is kept (see
	// attachments_archive.go).
	TreeClassSessionArchive = "session-archive"

	// TreeClassBudgetRun is the class of the TEST-BUDGET entry point's own
	// artifact tree (<scratch home>/queue/test-budget): the per-run
	// built-binary directories `scripts/test-budget.sh` fills and never cleans,
	// one ~63 MB `gopi` binary per `make test-budget` run. It is a class of its
	// own rather than `build-cache` or a second `raw-log` because the WRITER is
	// a different component of this repository (the declared test entry point,
	// not the merge gate and not the agent), and the gate reads the class to
	// decide which writer's source it must find the declaration in
	// (declared_budget_runs.go).
	TreeClassBudgetRun = "budget-run"

	// TreeClassTmpDirResidue is the class of the DECLARED TEMP ROOT's own
	// residue (`<home>/workspace/_tmp`, what TMPDIR points at once gopi has
	// published its tmpdir): the directories a run mints there through
	// `os.MkdirTemp("", …)` / `mktemp -d` and abandons.
	//
	// It is a class of its own rather than `go-tmp` or `build-cache` because the
	// WRITER is different in kind: `go-tmp` is the toolchain's work directory
	// (`go-build*`, a name the toolchain generates), while this family is gopi's
	// own residue namespace transplanted into the same root — the base sweep's
	// `gopi-<token>` / `gocache-<token>` / `probe-<token>` shapes. Two rows over
	// one root are correct here only because their PREDICATES are disjoint
	// (`go-build*` vs the residue namespace), which is the same arrangement the
	// build-cache and session rows already use.
	TreeClassTmpDirResidue = "tmpdir-residue"
)

// EntryKind is what a tree counts as a removable entry.
type EntryKind int

const (
	// EntryKindDir: the tree's direct children are DIRECTORIES (go-build*,
	// gocache batch dirs). A flat child is never a candidate — an artifact is
	// not a run (the queue's rule, unchanged).
	EntryKindDir EntryKind = iota
	// EntryKindFileSuffix: the tree's direct children are FILES, and only
	// names carrying the tree's suffix are candidates at all (the legacy raw
	// log dir also holds a human's fmt-*.log dumps — not ours, never touched).
	EntryKindFileSuffix
	// EntryKindTreeFileSuffix: the candidates are FILES carrying the suffix,
	// found ONE directory below the tree (the session tree's per-run
	// `<uuid>.raw.log`). Swept one level down (the queue's rule) so a
	// container is never removed as a unit — a session directory that still
	// holds a live session.jsonl survives even when its raw log ages out.
	EntryKindTreeFileSuffix
	// EntryKindFileMatch: the candidates are FILES and the tree's NAMESPACE
	// PREDICATE is their WHOLE name rule — there is no single suffix to carry.
	//
	// WHY THE VOCABULARY NEEDED IT (measured 2026-10-01): the sessions root's
	// residue FILES are an exact SHAPE TABLE (a crash dump `<uuid>.dump.json`, a
	// probe's `run-full*.log`, `note.txt`, `.migration-manifest.json`, an
	// `<row-id>-seq.json`), which no one suffix spells. The alternative was
	// either a row per suffix (four rows for one family, and two of them would
	// never match anything) or a row with an EMPTY suffix — and an empty suffix
	// on a file kind means "every file in the tree", i.e. exactly the blunt
	// "delete everything older than N here" the EntryMatch field exists to
	// prevent. This kind keeps that guard: a FileMatch row is refused unless it
	// carries a predicate (TestDeclaredTreesAreWiredAndNamed).
	EntryKindFileMatch
)

// The AGE of each declared tree — ONE constant per tree (Rule 1: no magic
// numbers; Rules 13/14: defined once in this leaf package and referenced, never
// re-spelled).
//
// The three values differ ON PURPOSE, and each difference is a measurement, not
// a preference:
//
//   - GoTmpStaleAge is the SHORTEST because the tree is pure transient build
//     state and the toolchain's own lifecycle says so: `go` removes its work
//     dir at the end of the invocation, so ANY child still present is by
//     definition already abandoned by a process that died (a killed build).
//     24h is deliberately conservative, not tight — it is many times the
//     longest legitimate build on this host AND it leaves a full working day
//     in which a crashed build's cache can still be inspected by hand.
//   - RawLogStaleAge (flat `~/.gopi/raw/`) is SHORTER than the session-tree
//     age because those logs are UNPAIRED: a bare prompt / chat run writes
//     there, and the session-dir-unify layout (internal/agent.NewRawLoggerForRun)
//     puts every TASK run's raw log beside its session JSONL instead. An
//     unpaired HTTP log is the least referenced diagnostic material we keep.
//   - SessionRawLogStaleAge (session `*.raw.log`) is the LONGEST because a
//     real cost is attached: gopilog pairs a raw log to its session by
//     timestamp within maxRawLogMatchDelta (±2 min), so an aged-out file costs
//     the ability to re-read an old run's VERBATIM request body — the epic
//     brief's own wording for the acceptable loss — while `gopi session raw` /
//     `gopi session stats` / `gopi session resume` for anything current keep working. The
//     recorded rawLogPath in the session's session_start event becomes a dead
//     pointer, which is exactly why the window is 14 days and not 7.
const (
	// TreeEntryScanLimit caps how many entries ONE declared-tree walk visits,
	// so a pathological tree cannot turn the sweep into a hang.
	//
	// WHY IT IS NOT ScanLimit: ScanLimit (10,000) bounds ONE DIRECTORY of the
	// shared scratch home, where anything beyond that is pathological. A
	// declared tree is a DIFFERENT shape — measured on this host, the sessions
	// tree holds 74,994 top-level entries (75k per-run directories, 879 of them
	// carrying a raw log) — so reusing the scratch bound SKIPPED THE WHOLE TREE
	// and reported `removed=0`, i.e. the 40 GB of `*.raw.log` this arm exists to
	// bound would have stayed unbounded while the log said the tree was visited.
	// The value is a bound, not a target: a straight walk of 500k entries costs
	// seconds, and the per-candidate work (age, namespace, liveness) is what the
	// sweep is actually spending its time on.
	TreeEntryScanLimit = 500000

	// GoTmpStaleAge is the age past which an unheld `go-build*` directory in
	// the GOTMPDIR tree is garbage. The ROOT is never a candidate at any age.
	GoTmpStaleAge = 24 * time.Hour
	// RawLogStaleAge is the age past which an unpaired flat raw log under the
	// gopi data root's raw directory is garbage.
	RawLogStaleAge = 72 * time.Hour
	// SessionRawLogStaleAge is the age past which a session's own raw HTTP log
	// (and its container directory, once it is empty) is garbage.
	SessionRawLogStaleAge = 14 * 24 * time.Hour
	// SessionRawLogDirStaleAge is the extra age a session DIRECTORY must reach
	// before it is removed for being EMPTY of raw logs. It is strictly longer
	// than SessionRawLogStaleAge so the two arms can never race for the same
	// container: the file arm has to have finished with it long before the
	// directory arm looks at it.
	SessionRawLogDirStaleAge = SessionRawLogStaleAge + 24*time.Hour

	// SessionScratchDirStaleAge is the age past which a DIRECTORY inside a
	// session container — a run's own working area, whose NAME the run chose
	// (measured 2026-10-01: 277 directories / 165 shapes: evidence, evid, bin,
	// artifacts, __pycache__, crops…) — may be removed by the `session-raw`
	// row's container arm.
	//
	// IT IS THE LONGEST WINDOW IN THIS FILE ON PURPOSE, and the value is the
	// SECOND of two gates, not the only one. What actually makes such a
	// directory garbage is the CONTAINER's lifecycle: a container the ledger
	// still describes (a `metadata.json`, or any `*.jsonl` record a reader could
	// resume) keeps every directory inside it FOREVER, at any age, and so does a
	// container touched inside this window — see containerHasNoLedger in
	// sessions_scratch_dir.go.
	//
	// The 30 days is then the floor under the floor: it covers the measured
	// shape of the tree (the oldest such directory on this host is 14.8 days, so
	// every live container will long since have been archived or removed by the
	// time its dead sibling ages in) and it leaves a month in which a hand-made
	// evidence directory can still be read by a human before the sweep takes it.
	// A SHORTER window would be the reckless choice here: this family is
	// evidence a report cites by name, and the arm only ever touches a directory
	// whose container the ledger has already forgotten. It is deliberately NOT a
	// `StaleAge` on a tree row of its own: the container arm already walks this
	// tree, and a second row over one root would be a second answer to "what is
	// in this container".
	//	A FLOOR, NOT THE VALUE ITSELF: containerEntries hands its hook the row's
	//	`StaleAge` — the *.raw.log FILE arm's 14 days — and judging an evidence
	//	DIRECTORY by a file arm's window would delete evidence the documented
	//	decision forbids. scratchDirWindow applies this floor.
	SessionScratchDirStaleAge = 30 * 24 * time.Hour

	// SessionAttachmentsStaleAge is the window an EMPTY attachments ARCHIVE must
	// reach before it may be collected.
	//
	// IT IS THE LAST AND WEAKEST LEVER OF THAT TREE'S PREDICATE, and deliberately
	// long: what authorises the reclaim is the EVIDENCE predicate in
	// attachments_archive.go (an ordinary, empty, unheld directory holds nothing
	// a copy could survive in), never time. The window only keeps a run that is
	// mid-submission — a container whose archive was created a moment ago and is
	// about to receive its copies — out of the pass's way, so a submission is
	// never racing a sweep for the directory it is filling.
	//
	// 7 days, measured against the work it must not touch: the archive of a task
	// is filled at SUBMIT time and a task's runs happen within hours, so a
	// directory still empty after a week is one no submission is filling. Every
	// member of the family is a per-task artifact of a task that is over.
	SessionAttachmentsStaleAge = 7 * 24 * time.Hour
)

// Environment names the declared-tree resolvers read. Each is defined here or
// aliased to its ONE owner (Rules 13/14) and read in exactly one place
// (Rule 2 — the resolver below).
const (
	// GoTmpDirEnvName is the Go toolchain's own work-directory knob, read from
	// the go env file (`go env -w GOTMPDIR=…`, i.e. ~/.config/go/env). The
	// value is NOT re-spelled here: it is whatever the toolchain is configured
	// with, resolved through goToolEnvFile.
	GoTmpDirEnvName = "GOTMPDIR"

	// goEnvFileRelPath is the go env file's location under the home — the
	// toolchain's own path, go env -w writes it.
	goEnvFileRelPath = ".config/go/env"
	// goEnvFileKeySep is what separates key and value in the go env file.
	goEnvFileKeySep = "="
	// goEnvFileReadLimit bounds the read of the go env file: it is a small
	// key=value file a human writes, and a bound keeps a pathological one from
	// costing the sweep real memory.
	goEnvFileReadLimit = 1 << 20

	// rawLogDirName is the legacy flat raw-log directory's name under the gopi
	// data root. It is the value of internal/gopilog's own (unexported)
	// constant; the consumer, internal/config, cannot import gopilog (gopilog
	// imports config), so this package re-HOMES the value rather than
	// re-spelling it at each site (Rules 13/14) — internal/config's raw-log
	// resolver references THIS constant, and gopilog's own spelling is pinned
	// to it by TestRawLogDirNameMatchesGopilog.
	rawLogDirName = "raw"

	// GoBuildDirEntryPrefix is the NAME prefix of the only entries the GOTMPDIR
	// tree's sweep may take: the `go-build*` work directories the toolchain
	// creates (and removes itself when it exits cleanly). It is the tree's
	// NAMESPACE PREDICATE, and it matters: a declared tree's children are
	// judged by age + liveness, so without a name rule ANY aged directory in the
	// tree would be collectable — including something an operator parked there.
	GoBuildDirEntryPrefix = "go-build"

	// RawLogNamePrefix is the NAME prefix of a raw HTTP log this system writes
	// (`gopi-raw-YYYYMMDD-HHMMSS.log`). Together with RawLogFileSuffix it is the
	// flat raw directory's namespace predicate: the directory also holds a
	// human's own dumps (`fmt-detail.log`, `fmt-last.log` — measured on this
	// host) which carry the same suffix and must never be a candidate. The
	// writer (internal/agent.createRawLogFile) builds its names from these
	// constants, and internal/gopilog's TestRawLogDirNameMatchesScratchTree
	// asserts its reader REGEXP accepts exactly this shape.
	RawLogNamePrefix = "gopi-raw-"

	// RawLogFileSuffix is the suffix the legacy flat raw log's name carries
	// (`gopi-raw-YYYYMMDD-HHMMSS.log`). It is a NAMED value because it is the
	// only thing that separates OUR log from a human's `fmt-*.log` dump
	// sitting in the same directory. internal/agent's writer references it;
	// internal/gopilog's reader still spells it inside its own regexp (a
	// regexp cannot take the constant without being rewritten) — the two are
	// held together by gopilog's TestRawLogDirNameMatchesScratchTree and by
	// the gate's writer anchors.
	RawLogFileSuffix = ".log"

	// SessionRawLogFileSuffix is the suffix of a raw log co-located with
	// session/run files inside the sessions tree (session-dir-unify S1). The
	// WRITER (internal/agent.NewRawLoggerForRun → newRawLoggerInDir) builds the
	// file name from this constant, so writer and sweeper cannot disagree.
	SessionRawLogFileSuffix = ".raw.log"
)

// TreeClass is the type of one declared tree (see the class constants).
type TreeClass string

// DeclaredTree is one tree this repository's own commands create and never
// clean, named for the check to read and the sweep to walk.
//
// A field declared here that nothing reads is a lie: TestDeclaredTreeFieldsAreRead
// asserts every field-less name is read by ResolveDeclaredTrees/PlanDeclaredTrees,
// and the resolver itself is the ONE place a path is spelled (Rule 2).
type DeclaredTree struct {
	// Name is the tree's identity in reports and in the gate's diagnostics:
	// the logfmt key=value field on the watchdog line, and the name a red gate
	// prints when the tree is missing from the declared set.
	Name string
	// Class is the tree TYPE (the gate reads this to decide which writers it
	// must find a declaration for).
	Class TreeClass
	// Kind is what counts as a removable entry (directories, suffixed files,
	// or suffixed files one level down).
	Kind EntryKind
	// Suffix is the entry-name suffix, for the file kinds ("" for dirs).
	Suffix string
	// EntryMatch is the tree's NAMESPACE PREDICATE: a name that is not ours is
	// never a candidate, whatever its age. It is what keeps a declared tree from
	// becoming a blunt "delete everything older than N here" — a shared or
	// human-written tree may legitimately contain entries that are not ours.
	// nil means the Kind+Suffix rule is the whole namespace.
	EntryMatch func(name string) bool
	// StaleAge is THIS tree's age constant (see the block above). It is a
	// reference to one of the named constants, never a literal.
	StaleAge time.Duration
	// RootStaleAge is the age the tree has to reach before its EMPTY
	// directories may be removed; 0 means an empty directory is removed as
	// soon as it is stale under StaleAge. Only the session tree sets it.
	RootStaleAge time.Duration
	// EntryStale overrides how ONE entry's staleness is measured, for a tree
	// whose entries do not age by their own mtime. nil means the hard rule's
	// ScratchEntryStale (the entry's mtime), which is what every tree whose
	// entries are FILES wants. It is read through DeclaredTree.entryStale
	// (Rule 15: a declared field nothing reads is a lie).
	//
	// It exists for the build cache: the gate creates a key directory and
	// never touches it again, while the toolchain fills the cache BELOW it, so
	// the key's own mtime stops moving the moment the build starts (measured
	// 2026-09-20: every one of 172 keys older than its own `gocache`).
	EntryStale func(path string, now time.Time, olderThan time.Duration) (bool, error)
	// EntrySize overrides how ONE entry's reclaimable bytes are measured. nil
	// means the sweep's dirSize (regular files below the entry, symlinks not
	// followed) — correct for a cache tree too; a tree may name its own reader
	// when its body has a shape the generic walk would misreport. Read through
	// DeclaredTree.entrySize.
	EntrySize func(path string) (int64, error)
	// Sweep overrides how the tree's candidates are COLLECTED. nil means the
	// generic walk (declaredTreeEntries + removeCollectableOfTree), which plans
	// the whole tree and holds one liveness index across its candidates — the
	// right shape for every tree whose entries are one family of like objects.
	//
	// It exists for a tree whose candidates are a FOREIGN artifact: the shared
	// build cache's ROOT-LEVEL STRAY SHARDS (declared_build_cache_stray.go) are
	// the shards a GOCACHE parked at the cache root produced, so the safety
	// question is per shard (is a process inside it? is it a declared root?)
	// rather than per tree, and each one is re-checked at the moment of its own
	// delete. The PLAN face stays the tree's own listing (List), so `--dry-run`
	// cannot promise a different set from the one the sweep takes.
	Sweep func(age time.Duration) (int, int64, error)
	// List is the candidate listing of a tree that owns its collection: the same
	// candidates the tree's Sweep removes, measured the same way, with the
	// tree's own staleness rule applied — the ONLY thing the plan face of such a
	// tree reads. It is a field rather than the generic walk so a plan and a
	// sweep can never be two different answers (Rule 15: a declared field
	// nothing reads is a lie).
	List func(age time.Duration) ([]TreeEntry, error)
	// Resolve returns the tree's ROOT path. Every spelling of the tree lives
	// exclusively here.
	Resolve func() (string, error)
}

// entryStale applies the tree's staleness rule — its override when it declares
// one, else the hard rule itself. ONE place decides, so the predicate, the plan
// and the sweep cannot drift into three answers.
func (t DeclaredTree) entryStale(path string, now time.Time, olderThan time.Duration) (bool, error) {
	if t.EntryStale != nil {
		return t.EntryStale(path, now, olderThan)
	}
	return ScratchEntryStale(path, now, olderThan)
}

// entrySize applies the tree's size rule — its override when it declares one,
// else the sweep's generic subtree measurement.
func (t DeclaredTree) entrySize(path string) (int64, error) {
	if t.EntrySize != nil {
		return t.EntrySize(path)
	}
	return dirSize(path)
}

// window is the ONE window this tree's entries are judged by: the tree's own
// StaleAge constant, always, on every arm.
//
// WHY IT TAKES NO ARGUMENT (measured 2026-10-27, task 01a17b8b-581a). It used to
// be `staleAge(override)`, returning the caller's value whenever it was > 0 — and
// then two arms judged the SAME declared tree by two different clocks: the `trees`
// arm and the resident tick passed the tree's own constant, while the HOME arm
// reached the same tree through the queue walk (sweepDeclaredTreeFromQueue →
// SweepDeclaredTree) and passed its own GLOBAL window. So `--scratch-gc-stale-min
// 1` collected directories out of a tree declared at 24h/336h: measured on the
// budget-runs row, `scratch-gc trees` reported stale=6h0m0s collectable=0 while
// the home arm planned all 401 of them, and the two arms disagreed about the same
// bytes on the same tick.
//
// The repair is this signature, not a convention: a window that guards data must
// take no parameter from the arm that happens to find the tree, so a second answer
// is UNREPRESENTABLE rather than merely discouraged. A caller that wants a
// different window for a tree changes that tree's row (and its named constant),
// where the decision is attributable to the tree it protects.
func (t DeclaredTree) window() time.Duration { return t.StaleAge }

// DeclaredTrees is the ONE declared set: the trees the GC's scope must cover.
//
// Adding a tree the repository creates means adding a row here — and the gate
// (TestDeclaredTreesCoverRepoWriters) fails when a writer exists without one,
// so the set cannot silently fall behind the code again.
func DeclaredTrees() []DeclaredTree {
	trees := []DeclaredTree{
		{
			Name:       TreeNameGoTmp,
			Class:      TreeClassGoTmp,
			Kind:       EntryKindDir,
			StaleAge:   GoTmpStaleAge,
			EntryMatch: hasNamePrefix(GoBuildDirEntryPrefix),
			Resolve:    ResolveGoTmpDir,
		},
		{
			Name:       TreeNameRaw,
			Class:      TreeClassRawLog,
			Kind:       EntryKindFileSuffix,
			Suffix:     RawLogFileSuffix,
			StaleAge:   RawLogStaleAge,
			EntryMatch: hasNamePrefix(RawLogNamePrefix),
			Resolve:    RawLogDir,
		},
		{
			Name:         TreeNameSessionRaw,
			Class:        TreeClassRawLog,
			Kind:         EntryKindTreeFileSuffix,
			Suffix:       SessionRawLogFileSuffix,
			StaleAge:     SessionRawLogStaleAge,
			RootStaleAge: SessionRawLogDirStaleAge,
			Resolve:      SessionRawLogRoot,
		},
	}
	// The sessions tree's LEGACY-RESIDUE rows — the families the family registry
	// (internal/sessionlayout) decided to BOUND: the root-level residue, and the
	// probe checkout / private profile one level down inside a container. They
	// share the sessions root with the `session-raw` row above and never overlap
	// it (disjoint predicates, checked by the gate).
	trees = append(trees, SessionLegacyRootTrees()...)
	trees = append(trees, sessionLegacyContainerRow())

	// The shared Go build cache — the fourth tree, and the only one whose ROOT
	// is a function of the worktree-root resolution rather than one fixed path,
	// so it contributes one row per resolved root (declared_build_cache.go), plus
	// its second row per root for the ROOT-LEVEL STRAY SHARDS a foreign GOCACHE
	// parked above the keys (declared_build_cache_stray.go).
	trees = append(trees, BuildCacheTrees()...)
	// The sessions tree's per-task `attachments/` archives — the FIFTH row, and
	// the only one whose bound is an EVIDENCE predicate rather than an age
	// window (attachments_archive.go). It roots at the same sessions tree the
	// `session-raw` row roots at, and the two take disjoint shapes: that row
	// takes `*.raw.log` FILES one level down, this one takes `attachments/`
	// DIRECTORIES two levels down.
	trees = append(trees, attachmentsArchiveTree())
	// The gopi PROFILE's dump trees — the two rows whose existence depends on a
	// configuration an OPERATOR set rather than on a code path: the error-path
	// dump writers default to the scratch dumps class trees, while their
	// first-ranked sources (GOPI_ERROR_DUMP_DIR, GOPI_1214_DUMP_DIR, each read in
	// exactly one place in internal/deepseek) may point these. See
	// sibling_dumps.go for the measurement and the decision.
	trees = append(trees, siblingDumpsTree(), siblingDumps1214Tree())
	// The SCRATCH home's dump CLASS trees — the writers' DOCUMENTED default,
	// i.e. the two directories that exist on every host that has not exported a
	// dump knob (scratch_dumps.go carries the measurement: 393 dumps the census
	// could not see because no name rooted there).
	trees = append(trees, ScratchDumpsClassTrees()...)
	// The CAPTURE class tree — the PNGs a `gopi tool capture` / `screen_capture` /
	// `shot` run writes when no `--out` is given. It is the fifth SIBLING of the
	// two dump classes (same shape: one suffix, one class directory, the shared
	// age rule) and it exists so a capture's default destination is a tree a
	// sweep ALREADY walks (scratch_captures.go carries the measurement: the
	// previous default was the caller's cwd, i.e. the run's own git ground).
	trees = append(trees, CaptureDumpTrees()...)
	// The TEXT2IMG class tree — the PNGs `gopi tool text2img` and the model's
	// text2img tool write when no `out` is given. Sixth sibling of the dump and
	// capture classes, same shape (one suffix, one class directory, the shared
	// age rule), and it exists for the same measured reason: the previous default
	// was `<src 去扩展名>.png`, i.e. BESIDE THE SOURCE FILE — inside the run's own
	// git ground when src lives there (Rule 30's `completed-unclean`;
	// scratch_text2img.go carries the measurement).
	trees = append(trees, Text2ImgDumpTrees()...)
	// The TEST-BUDGET entry point's own artifact tree — the per-run built-binary
	// directories `scripts/test-budget.sh` fills and never cleans, the tree the
	// 2026-10-12 `/`-exhaustion found sitting outside every arm of this sweep
	// (measured: 320 directories / 10.27 GB, invisible to both faces).
	trees = append(trees, budgetRunsTree())
	// The SAME tree's FLAT half — the per-run `run-<tag>.json` STREAMS (17.7 GiB
	// of 21 GiB measured on this host, 2026-11-22). It is a SECOND row over the
	// same root because its window is a different decision (evidence kept for
	// weeks, not a binary kept for hours) and its candidates are a different
	// KIND; the two predicates are disjoint by construction and the gate checks
	// it. See declared_budget_evidence.go for why the directory row cannot be
	// widened to reach these.
	trees = append(trees, budgetEvidenceTree())
	// The DECLARED TEMP ROOT's own residue — the row that bounds `_tmp` itself,
	// the tree gopi publishes as TMPDIR for every child. It roots at the same
	// directory `go-tmp` may root at and takes a DISJOINT name shape (the base
	// sweep's residue namespace vs `go-build*`), so the two never collide.
	trees = append(trees, tmpDirResidueTree())
	// The suite's own GOCACHE sandbox shells — the SEVENTH row, and the one a
	// run that DIED leaves behind (declared_build_cache_suite.go carries the
	// measurement: 686 directories / 1.9 GiB, none of them claimed by any arm
	// before this row). It is rooted at the same directory as the two rows
	// above and takes a disjoint name shape, so the three never collide.
	trees = append(trees, BuildCacheSuiteSandboxTrees()...)
	return append(trees, BuildCacheStrayShardTrees()...)
}

// ResolveGoTmpDir returns the GOTMPDIR tree (the Go toolchain's work-directory
// root) through the SAME three ranks the toolchain itself uses:
//
//	GOTMPDIR ($GOTMPDIR)  >  GOTMPDIR (go env file)  >  the process temp root
//
// THE THIRD RANK IS THE ONE THAT WAS MISSING, AND ITS ABSENCE WAS A DEFECT
// (measured 2026-10-15, this host). `go` creates its `go-build*` work
// directories under GOTMPDIR when it is set and under the standard temp root
// (`$TMPDIR`, i.e. os.TempDir()) when it is NOT — so on a host where GOTMPDIR is
// set NOWHERE (this host's go env file carries only GOFLAGS and GOPROXY) every
// abandoned `go-build*` directory landed under `$TMPDIR` and NOTHING swept it.
// The row reported `not configured on this host — nothing to sweep` while the
// very tree it describes was being filled: measured the same day, three
// `go-build*` directories under /home/user/workspace/_tmp (this host's
// TMPDIR, 1.5 GB) at ~50 MB each, all three abandoned by killed builds.
//
// A DECLARED TREE THAT RESOLVES TO NOTHING IS WORSE THAN AN UNDECLARED ONE: the
// log line says the tree was considered, so a reader concludes the tree is
// bounded. The rank ladder is therefore the only honest reading — the tree the
// toolchain actually fills, resolved exactly as the toolchain resolves it.
//
// WHY THE FALLBACK IS READ AND NOT SPELLED (Rule 13/14): the last rank is
// whatever `go` would itself read — an exported `$TMPDIR`, or the Go runtime's
// own default when there is none. Re-spelling either here would be a SECOND
// definition of a rule that already has exactly one (EffectiveTempRoot, the same
// probe internal/testutil pins the suite with), and the two would drift.
//
// A temp root that cannot be measured is REPORTED, never rendered as
// "unconfigured" (Rule 15b: could-not-measure is not a value) — the caller must
// never read an unmeasurable host as a clean one.
func ResolveGoTmpDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(GoTmpDirEnvName)); v != "" {
		return CanonicalPath(v), nil
	}
	v, err := goToolEnvFileValue(GoTmpDirEnvName)
	if err != nil {
		return "", err
	}
	if v != "" {
		return CanonicalPath(v), nil
	}
	// THE THIRD RANK — the fallback the toolchain itself takes. See the
	// function's header: this rank was MISSING, and its absence is why the row
	// reported "not configured on this host" while `go` was filling the very
	// tree the row describes.
	root, _, err := EffectiveTempRoot()
	if err != nil {
		return "", fmt.Errorf("%s is set neither in the environment nor in %s, and the process temp root could not be measured: %w",
			GoTmpDirEnvName, goEnvFilePath(), err)
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%s is set neither in the environment nor in %s, and the process temp root resolved empty",
			GoTmpDirEnvName, goEnvFilePath())
	}
	return CanonicalPath(root), nil
}

// goEnvFilePath is the ONE spelling of the go env file's path
// (<home>/.config/go/env), the file `go env -w` writes.
func goEnvFilePath() string {
	return filepath.Join(homeBase(), goEnvFileRelPath)
}

// goToolEnvFileValue reads one key from the go env file. A missing file is not
// an error (an operator who never ran `go env -w`) — the caller reports the
// unset value itself. A file that cannot be READ is an error, because "could
// not measure" must never render as "not declared" (Rule 15).
func goToolEnvFileValue(key string) (string, error) {
	f, err := os.Open(goEnvFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read go env file %s: %w", goEnvFilePath(), err)
	}
	defer func() { _ = f.Close() }() //stylegate:ignore best-effort close of a read-only probe; the bounded read below already decided the value

	buf := make([]byte, goEnvFileReadLimit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", fmt.Errorf("read go env file %s: %w", goEnvFilePath(), err)
	}
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		name, value, found := strings.Cut(line, goEnvFileKeySep)
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`), nil
	}
	return "", nil
}

// RawLogDir returns the legacy flat raw-log directory under the gopi data root
// (<gopi dir>/raw). It resolves through config's ONE gopi-data-root resolution
// (config.RawLogDir — which references this package's rawLogDirName so the value
// has exactly one definition, Rule 14).
func RawLogDir() (string, error) {
	dir, err := rawLogDirResolver()
	if err != nil {
		return "", err
	}
	return CanonicalPath(dir), nil
}

// SessionRawLogRoot returns the sessions tree (<gopi dir>/sessions) — the tree
// whose per-run `<run-uuid>.raw.log` files are the third declared tree. The
// whole-tree `du` is deliberately never taken by the sweep (measured: it times
// out on this host, memory #2898); the sweep walks one level, stat'ing files.
func SessionRawLogRoot() (string, error) {
	dir, err := sessionsDirResolver()
	if err != nil {
		return "", err
	}
	return CanonicalPath(dir), nil
}

// rawLogDirResolver / sessionsDirResolver are function seams so this leaf
// package never imports internal/config (which imports THIS package —
// config.go/env.go/settings.go/flags.go/tmpdir.go all reference scratch
// constants, so a reverse import is a cycle). internal/config assigns them
// once, at init, from its own resolvers (config.GopiDir + the two dir names) —
// the same "leaf declares the shape, the owner supplies the value" pattern the
// rest of this package uses, and the assignment is checked by
// TestTreeResolversAreWired.
var (
	rawLogDirResolver   = func() (string, error) { return "", errTreeResolversUnwired }
	sessionsDirResolver = func() (string, error) { return "", errTreeResolversUnwired }
)

// ErrDeclaredTreeUnconfigured reports that a declared tree is declared but does
// not exist on THIS host (GOTMPDIR set nowhere, no raw-log directory yet, no
// sessions tree). It is a NAMED error so a caller can distinguish "nothing to
// bound here" (a reported no-op, exit 0 — a watchdog must not alarm every 30
// minutes for a tree that was never created) from "the tree is there and could
// not be read" (a real alarm, Rule 15: an unmeasured fact never renders as a
// measured negative).
var ErrDeclaredTreeUnconfigured = errors.New("declared tree is not configured on this host")

// errTreeResolversUnwired reports the one failure mode of the seam above: a
// caller that resolved a declared tree before internal/config wired the
// resolvers (only reachable from a test that builds the package standalone).
var errTreeResolversUnwired = fmt.Errorf("scratch: declared-tree resolvers are not wired (import internal/config)")

// RegisterDataRootResolvers installs the gopi-data-root-based resolvers for the
// raw-log trees. internal/config calls it once from its own init, passing its
// GopiDir plus the two directory names IT owns (config.SessionsDirName and this
// package's rawLogDirName) — so the paths have one spelling each and this
// package stays a leaf.
func RegisterDataRootResolvers(rawDir, sessionsDir func() (string, error)) {
	if rawDir != nil {
		rawLogDirResolver = rawDir
	}
	if sessionsDir != nil {
		sessionsDirResolver = sessionsDir
	}
}

// RawLogDirName is the legacy flat raw directory's leaf name, exported for the
// consumer in internal/config (Rule 14: one definition, every consumer imports
// it) — the value is this package's rawLogDirName.
func RawLogDirName() string { return rawLogDirName }

// IsDeclaredTreeRoot reports whether path IS the root of one declared tree —
// the question the invariant "the root of a declared tree is never a candidate"
// asks. It is a comparison of RESOLVED paths, so a tree spelled through a
// symlink is still recognised (and a root is never a prefix match: the root of
// one tree being nested inside another must not make it a candidate).
func IsDeclaredTreeRoot(path string) bool {
	want := CachedCanonicalPath(path)
	for _, tree := range DeclaredTrees() {
		root, err := tree.Resolve()
		if err != nil || root == "" {
			continue
		}
		if want == root {
			return true
		}
	}
	return false
}

// hasNamePrefix is the namespace predicate of a tree whose entries all begin
// with one fixed prefix (the toolchain's `go-build*`, our `gopi-raw-*`).
func hasNamePrefix(prefix string) func(string) bool {
	return func(name string) bool { return strings.HasPrefix(name, prefix) }
}

// entryName matches one tree's entry-naming rule: the KIND (directory or file),
// the SUFFIX when the tree declares one, and the tree's NAMESPACE predicate. All
// three must agree — a name that fails any of them is not ours and is never a
// candidate, whatever its age.
func (t DeclaredTree) entryName(name string, isDir bool) bool {
	switch t.Kind {
	case EntryKindDir:
		if !isDir {
			return false
		}
	case EntryKindFileSuffix, EntryKindTreeFileSuffix, EntryKindFileMatch:
		if isDir {
			return false
		}
	default:
		return false
	}
	if t.Suffix != "" && !strings.HasSuffix(name, t.Suffix) {
		return false
	}
	if t.EntryMatch != nil {
		return t.EntryMatch(name)
	}
	return true
}

// heldSet indexes the ONE /proc pass against ONE tree: every holder path inside
// the tree, plus every ancestor of it up to the tree root.
//
// WHY AN INDEX AND NOT HeldByProcesses PER CANDIDATE (measured 2026-09-20, this
// host): HeldByProcesses compares EVERY candidate against EVERY holder path —
// two resolved spellings each. On the sessions tree that is 74,994 containers ×
// the host's whole holder list, and the dry run did not finish inside two
// minutes. The two operations are equivalent (a candidate is held iff some
// holder lies inside it, i.e. iff the candidate is an ANCESTOR of a holder), so
// the same answer is had by walking each holder's path up to the root once and
// keeping the visited directories: O(holders × depth) instead of O(candidates ×
// holders). A candidate then costs one map lookup.
//
// It is the EXISTING liveness verdict, not a second one: the holders come from
// scratch.LiveHolders (the /proc cwd+exe+fd+maps scan) and the .holder
// registration is still checked per candidate by unheldRegistered. Only the
// comparison is indexed (memory #2632: one verdict, one implementation).
//
// SPELLING-AGNOSTIC (the fail-open this index had, measured 2026-10-14 on this
// host): the /proc channel reports what the KERNEL resolved, and a tree root may
// reach this walk through a LINK spelling (the retired
// scripts/setup-scratch-home.sh linked the scratch home elsewhere; a GOPI_HOME
// under a linked parent is the same shape). A lexical index built from ONE
// spelling of the root then never meets the holder — the holder's resolved path
// does not even START with the literal root — and heldIn answers false for the
// exact situation this veto exists for: a live process standing inside the
// candidate. MEASURED before the fix (probe, this host): root spelled through a
// link, holder path as the kernel reports it → heldIn(literal candidate)=false,
// heldIn(resolved candidate)=false, index keys=0 — the arm's ONLY liveness veto
// was a no-op, so the sweep fell through to age alone, in a package whose
// contract is "every unknown is a KEEP".
//
// THE FIX IS PER ROOT, NEVER PER CANDIDATE — and the distinction is the whole
// cost argument. The index exists because resolving EVERY CANDIDATE cost ~100s
// on a 75k-entry tree (see CachedCanonicalPath); the same trap is not sprung
// again from the holder side either. There are exactly TWO spellings of a root
// (holderSpellings), so each indexed chain is inserted under BOTH: a holder is
// first located under whichever root spelling it matches, its path RELATIVE to
// that root is taken, and the chain is then re-mounted onto every root spelling.
// That is what makes a candidate spelled through the link and a holder spelled
// as the kernel reports it — which are the same directory — land on one key.
// MEASURED on this host, for orientation: resolving every one of the 55,946
// holder paths of one /proc pass takes 387ms (70 of them change spelling), while
// this fix costs two resolutions PER TREE (the root's) and then only map
// inserts: a candidate remains one lookup on the spelling the walk handed it,
// with no resolution at all.
func heldSet(holders HolderScan, root string) map[string]struct{} {
	roots := rootSpellings(root)
	index := make(map[string]struct{}, len(holders.Paths))
	for _, holder := range holders.Paths {
		h := filepath.Clean(holder)
		// A holder outside the tree says nothing about it — indexing it would
		// let an unrelated path read as "held inside the tree" (the test that
		// pins this is TestHeldSetIndexesTreeAncestors). The test is LEXICAL on
		// purpose: it is the same rule the candidate side applies (heldIn), so
		// the two sides can only meet where the spellings already agree.
		rel, ok := relativeUnderAny(h, roots)
		if !ok {
			continue
		}
		// Insert the holder itself (a FILE holder holds exactly that file) and
		// every ancestor up to the tree root, under EVERY spelling of the root:
		// one directory, two names, and the candidate may be spelled either way.
		// The ancestor walk stays LEXICAL (filepath.Dir on the spelling) — no
		// lstat per level, because a chain re-mounted from a resolved path is
		// resolved all the way down.
		for _, r := range roots {
			insertChain(index, joinUnder(r, rel), r)
		}
	}
	return index
}

// rootSpellings lists the spellings ONE tree root is indexed under: the LITERAL
// spelling as the caller resolved (or spelled) it, plus the RESOLVED spelling the
// kernel reports for it. A root that is not a link (or that does not exist yet)
// yields the literal spelling alone, so this is a no-op — and byte-for-byte the
// previous behaviour — on every tree reached without a symlink.
//
// The resolution is memoized (CachedCanonicalPath) for the same reason the
// per-candidate question is: a root cannot move inside one run, and the arms that
// ask about it are concurrent sweeps.
func rootSpellings(root string) []string {
	clean := filepath.Clean(root)
	resolved := CachedCanonicalPath(clean)
	if resolved == clean || resolved == "" {
		return []string{clean}
	}
	return []string{clean, resolved}
}

// relativeUnder reports path's tail below root — whether path is root itself
// (empty tail, ok=true) or lies under it, compared LEXICALLY on the separator, so
// `/a/bc` is not "under" `/a/b`.
func relativeUnder(path, root string) (string, bool) {
	if path == root {
		return "", true
	}
	prefix := root + string(filepath.Separator)
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return path[len(prefix):], true
}

// relativeUnderAny is relativeUnder against the tree's root SPELLINGS, in order —
// the location step that lets a holder spelled resolved be re-mounted onto the
// literal root. It answers with the FIRST root that matches: a holder inside a
// linked tree matches one of the two, and a path matched by both would be a root
// nested inside its own resolution.
func relativeUnderAny(path string, roots []string) (string, bool) {
	for _, root := range roots {
		if rel, ok := relativeUnder(path, root); ok {
			return rel, true
		}
	}
	return "", false
}

// joinUnder re-mounts a relative tail onto one root spelling: "" is the root
// itself.
func joinUnder(root, rel string) string {
	if rel == "" {
		return root
	}
	return filepath.Join(root, rel)
}

// insertChain indexes path and every ancestor of it up to (and including) root —
// the walk heldSet performs for ONE holder, spelled for one root.
func insertChain(index map[string]struct{}, path, root string) {
	for p, ok := path, true; ok; p, ok = parentWithin(p, root) {
		index[p] = struct{}{}
		if p == root {
			break
		}
	}
}

// parentWithin returns path's parent and whether that parent still lies at or
// under root — the walk terminator of heldSet.
func parentWithin(path, root string) (string, bool) {
	if path == root {
		return "", false
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", false
	}
	if parent != root && !strings.HasPrefix(parent, root+string(filepath.Separator)) {
		return "", false
	}
	return parent, true
}

// heldIn reports whether the index holds path (the indexed form of
// HeldByProcesses for one tree).
func heldIn(path string, index map[string]struct{}) bool {
	_, ok := index[filepath.Clean(path)]
	return ok
}

// heldTreeScan is ONE declared-tree walk's holder verdict: the caller's SINGLE
// /proc pass in the two forms the walk needs.
type heldTreeScan struct {
	// index is the indexed ancestor set (heldSet): the per-candidate lookup, so
	// a candidate costs one map hit instead of a process-table walk (the
	// measured trap this package shipped once — memory #2632).
	index map[string]struct{}
	// scan is the same pass UNINDEXED, carried for the one fact the index
	// cannot express: whether the pass FINISHED. Truncated means the tail of
	// the process table was never looked at, so no candidate the index does not
	// hold may be deleted on it (see treeScanComplete).
	scan HolderScan
}

// treeScanComplete reports whether the pass this tree was judged by saw the
// WHOLE process table.
//
// WHY AN UNFINISHED PASS KEEPS EVERYTHING, stated once here so every caller
// reads the same rule: "no holder lies inside this path" is evidence only if
// every live process was examined. A pass that stopped at its bound examined
// some, so the answer is UNKNOWN — and unknown is never a delete (Rule 15).
// The same first clause is what unheldBatch applies; the difference is that the
// BATCH arms check it (unheldBatch, profileResidueReason, judgeSandbox,
// worktreeStubHeld) while the declared-tree walk read the scan without ever
// asking whether it finished — measured 2026-10-14: the container arm's hook
// returned a candidate for a truncated pass, i.e. the walk deleted on
// unmeasured evidence.
func treeScanComplete(held heldTreeScan) bool { return held.scan.Complete() }

// TreeEntry is one candidate a declared-tree sweep may collect.
type TreeEntry struct {
	// Path is the absolute path of the candidate.
	Path string
	// Size is its measured size in bytes (regular files only; 0 for a
	// directory, whose reclaim is measured after the fact).
	Size int64
	// Age is how long it has been untouched (mtime).
	Age time.Duration
	// IsDir is what the candidate is (dirs are removed whole).
	IsDir bool
}

// DeclaredTreePlan reports the entries of tree that a sweep would collect —
// deleting nothing. It is the SAME predicate the sweep uses (one predicate, two
// faces), so the plan can never promise something the sweep refuses.
//
// The window is the TREE's own (window()); a caller cannot supply one, so the
// plan and every sweep of this tree are judged by the same clock — see window's
// comment for the measured two-arm divergence this signature makes unrepresentable.
func DeclaredTreePlan(tree DeclaredTree) ([]TreeEntry, error) {
	root, err := tree.Resolve()
	if err != nil {
		return nil, err
	}
	if tree.List != nil {
		// A tree that owns its COLLECT face owns its candidate listing too, by
		// the same predicate and the same age rule — see the List field.
		// Returning an empty plan here would report a tree the sweep collects
		// while telling the operator nothing about it.
		return treeOwnedPlan(tree)
	}
	return declaredTreeEntries(tree, root)
}

// treeOwnedPlan is the PLAN face of a tree that owns its candidate listing
// (DeclaredTree.List): the listing itself, with the tree's window resolved first,
// so a plan can never promise a different set from the one the sweep takes.
func treeOwnedPlan(tree DeclaredTree) ([]TreeEntry, error) {
	return tree.List(tree.window())
}

// readTreeRoot reads a declared tree's root — the ONE place the two failures
// ("this tree does not exist on this host" vs "it is there and could not be
// read") are given their distinct, named shapes (Rule 15: an unmeasured fact
// never renders as a measured negative). Every walk of every tree root goes
// through here, so a second entry kind's walk cannot invent a third wording.
func readTreeRoot(tree DeclaredTree, root string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			// A tree that does not exist on this host has nothing to bound:
			// named as UNCONFIGURED so the CLI reports it without raising an
			// alarm every tick (and so the gate can still see that the tree IS
			// declared — declaration is not existence).
			return nil, fmt.Errorf("%w: declared tree %s has no root at %s",
				ErrDeclaredTreeUnconfigured, tree.Name, root)
		}
		return nil, fmt.Errorf("read declared tree %s (%s): %w", tree.Name, root, err)
	}
	return entries, nil
}

// errTreeScanLimit is the ONE shape of "this tree is pathological", shared by
// every walk so a truncated walk is never silent (Rule 15).
func errTreeScanLimit(tree DeclaredTree) error {
	return fmt.Errorf("declared tree %s has more than %d entries — stopped early (TreeEntryScanLimit)", tree.Name, TreeEntryScanLimit)
}

// reportTreeDeleteFailure renders one candidate's delete outcome, shared by
// every tree's collect face: a refusal is REPORTED with the tree that refused
// it, never swallowed (Rule 15).
func reportTreeDeleteFailure(tree DeclaredTree, path string, err error) {
	fmt.Fprintf(os.Stderr, "scratch: GC could not remove %s (%s): %v\n", path, tree.Name, err)
}

// reportTreeKeepUnmeasurable renders one candidate's UNMEASURABLE keep: a
// container the walk could not read is kept (Rule 15), and the keep is REPORTED
// with the tree it belongs to rather than left as silence — "kept because I
// could not look" and "not this tree's shape" must not read the same to an
// operator, and only one of them is a reason to go and look.
func reportTreeKeepUnmeasurable(tree DeclaredTree, path string, err error) {
	fmt.Fprintf(os.Stderr, "scratch: GC kept %s (%s) — %s: %v\n", path, tree.Name, EntryKeepUnmeasurable, err)
}

// declaredTreeEntries is the shared walk behind the plan and the sweep: it
// returns exactly the candidates, measured, with no delete. Its window is the
// tree's own, so the walk has no way to be told a second answer.
func declaredTreeEntries(tree DeclaredTree, root string) ([]TreeEntry, error) {
	age := tree.window()
	entries, err := readTreeRoot(tree, root)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	// ONE /proc pass for the whole tree, indexed once (heldSet): the
	// per-candidate form would cost a full process-table walk per entry (the
	// sessions tree has 74,994 top-level entries on the measured host — the
	// shape that HUNG the queue sweep, memory #2632).
	// ONE PASS, BOTH FACES: the index answers the walked spelling, the scan
	// itself answers the resolved one (heldTreePath), so the fail-closed half
	// costs no second /proc walk.
	scan := ScanHolders()
	held := heldTreeScan{index: heldSet(scan, root), scan: scan}
	var out []TreeEntry
	for i, entry := range entries {
		if i >= TreeEntryScanLimit {
			return out, fmt.Errorf("declared tree %s has more than %d entries — stopped early (TreeEntryScanLimit)", tree.Name, TreeEntryScanLimit)
		}
		path := filepath.Join(root, entry.Name())
		switch tree.Kind {
		case EntryKindTreeFileSuffix:
			// ONE LEVEL DOWN (the queue's rule): the candidates are the
			// per-run files inside each container directory, and the
			// container itself is only ever removed when it is EMPTY of
			// them — see containerEntries.
			out = append(out, containerEntries(tree, path, entry, now, age, held)...)
		default:
			if !tree.entryName(entry.Name(), entry.IsDir()) {
				continue
			}
			if !declaredEntryCollectable(tree, path, now, age, held) {
				continue
			}
			out = append(out, measuredEntry(tree, path, entry.IsDir(), now))
		}
	}
	return out, nil
}

// containerEntries plans the candidate files inside ONE container directory of
// a TreeFileSuffix tree, plus the container itself when it holds no candidate
// left (and is old enough under RootStaleAge).
func containerEntries(tree DeclaredTree, path string, entry os.DirEntry, now time.Time, age time.Duration, held heldTreeScan) []TreeEntry {
	if !entry.IsDir() {
		return nil // a flat entry where a container is expected: not ours
	}
	// AN UNFINISHED HOLDER PASS KEEPS THE WHOLE CONTAINER, read ONCE here before
	// any child is judged: a pass that stopped at its bound never looked at the
	// tail of the process table, so "nobody holds this container or anything in
	// it" was not measured — and an unmeasured fact authorises no delete
	// (Rule 15). This is the SAME predicate the rest of the walk asks
	// (treeScanComplete, one definition); what was missing is that the walk read
	// the scan without ever asking whether it had finished, while every other
	// arm of this package refuses on the flag (unheldBatch, profileResidueReason,
	// judgeSandbox, worktreeStubHeld). Measured 2026-10-14: a truncated pass
	// produced a candidate for the container arm.
	if !treeScanComplete(held) {
		return nil
	}
	if heldIn(path, held.index) {
		return nil // a live process inside the container: nothing below it is ours
	}
	children, err := os.ReadDir(path)
	if err != nil {
		// UNMEASURABLE: keep, and SAY SO. The keep is the existing rule (Rule 15);
		// the sentence is new, because a container this walk could not read
		// produced NO output at all — an operator reading the sweep's log could
		// not tell "kept because unreadable" from "not this tree's container",
		// which is the silent keep this package refuses everywhere else.
		reportTreeKeepUnmeasurable(tree, path, err)
		return nil
	}
	var out []TreeEntry
	keptFiles := 0
	for i, child := range children {
		if i >= TreeEntryScanLimit {
			break
		}
		childPath := filepath.Join(path, child.Name())
		if child.IsDir() {
			// A DIRECTORY inside the container: the run's own working area.
			// Its NAME is the run's choice (measured 2026-10-01: 165 shapes),
			// so the question is asked about the CONTAINER's lifecycle instead
			// — see sessions_scratch_dir.go for the predicate, and note that a
			// nil from the hook means KEEP, which is what the old blanket
			// keptFiles++ said for every directory and still says for every
			// one the container's ledger still describes.
			if cand := containerScratchDirEntries(tree, childPath, path, now, age, held); len(cand) > 0 {
				out = append(out, cand...)
				continue
			}
			keptFiles++ // unowned or young: the container is not emptied by it
			continue
		}
		if !tree.entryName(child.Name(), child.IsDir()) {
			keptFiles++ // the session record and side files: a container holding one is never emptied
			continue
		}
		if !declaredEntryCollectable(tree, childPath, now, age, held) {
			keptFiles++
			continue
		}
		out = append(out, measuredEntry(tree, childPath, false, now))
	}
	if keptFiles == 0 {
		// Every child is a candidate: the container is now a function of the
		// tree's own age (it is a per-run directory the writers created), so
		// it goes with them — but only once it has reached the LONGER
		// container window (RootStaleAge), which keeps the file arm and the
		// container arm from racing for the same directory.
		if rootAge := tree.RootStaleAge; rootAge > 0 {
			if stale, serr := ScratchEntryStale(path, now, rootAge); serr == nil && stale {
				out = append(out, measuredEntry(tree, path, true, now))
			}
		}
	}
	return out
}

// The hard rule applied to a declared tree's entry — stale, in the tree's
// namespace, and held by no live process; the root refused FIRST and
// unconditionally — used to live here as a bare bool, and that is exactly why
// the trees arm's `--explain` face could not answer its own question: a bool has
// no reason to print. It moved to declared_trees_verdict.go, where the SAME
// decision is made once as a REASON (declaredEntryKeepReason) and the bool is
// that reason's face (declaredEntryCollectable = reason == ""), so the sweep's
// delete decision and the report's sentence cannot drift.
//
// held is still the caller's ONE /proc pass, INDEXED once per tree (heldSet),
// and the .holder half is still read from inside the candidate — unheldBatch /
// unheldRegistered, reused rather than re-derived: "never delete a directory a
// live process holds" stays one rule with one verdict (memory #2632).

// measuredEntry measures one candidate for the report (size + age) using the
// TREE's own size rule. A tree that measures its body differently (the build
// cache's key holds a whole subtree) must be REPORTED by the same rule the
// sweep applies — a report that measured one way and collected another is a
// report nobody can act on.
//
// The AGE is the entry's own mtime age. For a tree whose staleness is decided
// by something else (the build cache reads the newest file below a key, because
// a key's own mtime stops moving once its build starts) the reported age is
// therefore the CONSERVATIVE direction: a key is only ever a candidate when the
// rule found all of its content older than the window, and its own mtime is
// older still.
func measuredEntry(tree DeclaredTree, path string, isDir bool, now time.Time) TreeEntry {
	e := TreeEntry{Path: path, IsDir: isDir}
	if size, err := tree.entrySize(path); err == nil {
		e.Size = size
	}
	if fi, err := os.Lstat(path); err == nil {
		e.Age = now.Sub(fi.ModTime())
	}
	return e
}

// TreePlanTotal is the byte total a plan would reclaim: the exact size of every
// planned file, plus the measured size of every planned directory (a directory
// is reclaimed whole, so its own subtree is what the sweep frees).
// TreeHeldTotal reports what one tree is HOLDING: the measured size of every
// child its OWN namespace rule claims, at ANY age — the plan's counterpart and
// the number the 2026-10-12 incident could not get anywhere.
//
// "HOLDING" IS NOT "HELD BY A LIVE PROCESS", and the two readings are ONE WORD
// APART on purpose-named fields (`holding=` / `holding_n=` vs a holder veto), so
// this function says which one it is: NO holder is consulted here (grep this
// body — `held`/`Holder` appear zero times). It is a WALK OF THE TREE, filtered
// by entryName and nothing else, which makes `holding_n` EQUAL to the probe's
// `entries` on every tree (measured 2026-10-27 on the host: 46 tree lines
// across four invocations, `entries != holding_n` on ZERO of them; the pair
// `budget-runs entries=431 holding=9175448847 holding_n=431` is that identity).
//
// Two consequences a reader must not have to re-derive:
//
//  1. `holding=0` is NOT "the tree is clean" and `holding_n=0` is NOT "nothing
//     held it" — a tree whose namespace rule matches no entry (an absent root, a
//     kind filter that fits nothing) reads (0, 0) beside a plan that can still
//     have bytes in it (measured: `tree=session-raw entries=0 bytes=81523226`,
//     an arm whose candidate is a FILE while the probe counts DIRECTORY-shaped
//     entries).
//  2. `holding=` is a NAMESPACE-FILTERED figure, never `du` of the tree. For a
//     tree that keeps flat evidence beside its containers it therefore
//     UNDER-REPORTS the tree's bytes by exactly the material its own rule
//     refuses: measured on `budget-runs` 2026-10-27, holding covered the 429
//     run DIRECTORIES (~8.1 GiB) while the tree's own streams and package lists
//     held a further ~7.0 GiB the same line never mentioned. Comparing that
//     figure with `du` and concluding "the sweep under-counts" is a MISREADING
//     of the field, not a defect in the walk (Rule 41).
//
// WHY IT IS A SECOND NUMBER AND NOT THE PLAN'S TOTAL (measured on this host).
// `scratch-gc trees` printed `tree=budget-runs bytes=0` on a tree that held
// 10.27 GB across 320 run directories: bytes= is what the plan WOULD reclaim,
// and every one of those entries was inside the age window, so the honest
// answer to "what would you reclaim" was 0 while the honest answer to "what is
// this tree holding" was 10 GB — and no face asked the second question. The
// same line is what made the incident read as "scratch-gc found nothing" when
// the sweep's own tree was the thing filling the disk (Rule 41: the figure the
// operator acts on has to be re-derivable, and this one is a walk of the tree).
//
// It applies the tree's own entryName rule (kind + namespace), so a FLAT
// artifact of the tree is never counted as a run — the number is what the tree
// could bound if everything in it aged out. A tree whose root is absent, or
// unreadable, reports (0, 0) and no error: a host that never ran the writer has
// no such tree, which is the same non-event the plan face reports.
// TreeProbe is ONE declared tree's PLAN READING: what the row RESOLVED to, and
// how many of the tree's direct entries its namespace predicate matched.
//
// WHY THE ROOT AND THE PRESENCE ARE APART (Rule 15: a failure must NAME itself).
// Before this type the row printed NOTHING for a tree it could not sweep: the
// failure went to stderr while the log line carried `removed=0 … bytes=0`, which
// is the SAME line a tree that was read, found clean, and left alone prints. A
// reader could not tell "the tree is bounded and empty" from "there is no tree
// here at all", and on this host that ambiguity was a live defect: the go-tmp row
// resolved to nothing while `go` was filling the very tree it describes.
//
// Present WITHOUT a root is the state the old code could not express, and it is
// NOT an error: it is the honest answer ("this tree does not exist here"), and it
// is kept distinct from an unmeasurable one.
type TreeProbe struct {
	// Root is the resolved tree root ("" when the tree is not configured).
	Root string `json:"root"`
	// Unconfigured is the resolver's own ErrDeclaredTreeUnconfigured verdict.
	Unconfigured bool `json:"unconfigured"`
	// Present reports whether the resolved root EXISTS as a directory.
	Present bool `json:"present"`
	// Entries is how many direct children the tree's namespace predicate
	// matched — the population the sweep had to judge. It is 0 for an absent or
	// unconfigured tree, so it NEVER substitutes for Present.
	Entries int `json:"entries"`
}

// ProbeDeclaredTree measures one declared tree WITHOUT collecting anything: the
// resolved root, whether it exists, and how many entries its predicate matches.
//
// It is the read-only half of the same resolution SweepDeclaredTree uses, and it
// exists so the sweep's report can state what it SAW rather than only what it
// took. A tree whose root cannot be read is Present=false with the entries left
// at zero — the caller reports the error it got from the resolver, and this
// function never turns "could not look" into "nothing there" (Rule 15).
func ProbeDeclaredTree(tree DeclaredTree) TreeProbe {
	root, err := tree.Resolve()
	if err != nil || root == "" {
		return TreeProbe{Unconfigured: err != nil && errors.Is(err, ErrDeclaredTreeUnconfigured)}
	}
	probe := TreeProbe{Root: root}
	info, statErr := os.Stat(root)
	if statErr != nil || !info.IsDir() {
		return probe
	}
	probe.Present = true
	entries, readErr := readTreeRoot(tree, root)
	if readErr != nil {
		return probe
	}
	for i, e := range entries {
		if i >= TreeEntryScanLimit {
			break
		}
		if tree.entryName(e.Name(), e.IsDir()) {
			probe.Entries++
		}
	}
	return probe
}

// TreeHeldTotal returns the bytes and count of the entries a tree's namespace
// predicate matches — at ANY age, and with NO holder filter (the same walk the
// probe counts; see this function's other comment above for why `holding_n`
// equals `entries` by construction, and what that does and does not say).
//
// A root that cannot be resolved, or that does not exist, is (0, 0) — the same
// reading an empty tree gives. It is therefore never a substitute for the
// presence the sweep's report states separately (see TreeProbe): "nothing is
// held here" and "there is nothing here" are different facts.
func TreeHeldTotal(tree DeclaredTree) (int64, int) {
	root, err := tree.Resolve()
	if err != nil || root == "" {
		return 0, 0
	}
	entries, err := readTreeRoot(tree, root)
	if err != nil {
		return 0, 0
	}
	var total int64
	count := 0
	for i, e := range entries {
		if i >= TreeEntryScanLimit {
			break
		}
		if !tree.entryName(e.Name(), e.IsDir()) {
			continue
		}
		if size, serr := tree.entrySize(filepath.Join(root, e.Name())); serr == nil {
			total += size
		}
		count++
	}
	return total, count
}

func TreePlanTotal(entries []TreeEntry) int64 {
	var total int64
	for _, e := range entries {
		// Every planned entry already carries the size its TREE's own rule
		// measured (measuredEntry): a directory's is its whole subtree, a
		// file's is its own bytes. Re-walking here would measure a second way.
		total += e.Size
	}
	return total
}

// SweepDeclaredTree removes tree's aged, unheld entries and reports (count,
// reclaimed bytes). The tree's ROOT is never removed, and every delete re-checks
// that identity (removeCollectableOfTree) — a sweep that could take a declared
// root would be the incident this file exists to prevent.
//
// The window is the tree's OWN (window()). It takes no age parameter ON PURPOSE:
// this function is the one call every arm funnels through — the `trees` arm, the
// resident tick, and the HOME arm's queue walk (sweepDeclaredTreeFromQueue) — so a
// parameter here is exactly how those arms came to disagree about one tree's
// window (see window's comment).
//
// A tree whose root does not exist is a no-op, not an error: a host that never
// ran `go` with a GOTMPDIR has no such tree.
func SweepDeclaredTree(tree DeclaredTree) (int, int64, error) {
	root, err := tree.Resolve()
	if err != nil {
		return 0, 0, err
	}
	if tree.Sweep != nil {
		// The tree collects its own candidates (they are not one family of like
		// objects — see the field), and it is handed ITS OWN window for the same
		// reason the generic walk is.
		return tree.Sweep(tree.window())
	}
	entries, err := declaredTreeEntries(tree, root)
	if err != nil {
		return 0, 0, err
	}
	removed := 0
	var freed int64
	for _, e := range entries {
		size := e.Size
		if err := removeCollectableOfTree(tree, e.Path); err != nil {
			fmt.Fprintf(os.Stderr, "scratch: GC could not remove %s (%s): %v\n", e.Path, tree.Name, err)
			continue
		}
		removed++
		freed += size
	}
	return removed, freed, nil
}

// removeCollectableOfTree deletes one declared-tree candidate that ALREADY
// passed the plan's predicate.
//
// THE INVARIANT, RE-CHECKED AT THE LAST POSSIBLE MOMENT: the path must not BE a
// declared root. The plan refused it already; this is the second half of the
// same rule, because a plan and a sweep can be separated by a symlink swap, a
// concurrent writer, or a future caller that builds a candidate list by hand.
// A refusal here is reported, never a crash (Rule 15).
//
// It also re-checks liveness exactly: between the plan and the delete a process
// may have entered the tree (a `go build` starting inside a `go-build*` the plan
// had called abandoned). The cost is one /proc walk per DELETE — i.e. per
// reclaimed entry, not per candidate — and the .holder half rides on top of it.
func removeCollectableOfTree(tree DeclaredTree, path string) error {
	if IsDeclaredTreeRoot(path) {
		return fmt.Errorf("refusing to remove the root of declared tree %s (%s)", tree.Name, path)
	}
	self := fmt.Sprintf("%d", os.Getpid())
	if holder, err := DirHolder(path); err == nil && holder != "" && holder != self {
		return fmt.Errorf("refusing to remove %s: held by live pid %s", path, holder)
	}
	if v := OwnerVerdictFor(path); v.Registered && !v.Collectable {
		return fmt.Errorf("refusing to remove %s: %s", path, v.Reason)
	}
	return removeCollectable(path)
}

// dirSize measures a directory subtree's byte size, following the sweep's own
// rules: symlinks are never followed (a symlink out of the tree must not make
// the sweep report — or delete — foreign bytes), and a directory that cannot be
// read reports its error so the caller can fall back to a measured 0 rather than
// a guess. It is the measurement the epic asks the log line to carry.
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += fi.Size()
		return nil
	})
	if err != nil {
		return total, err
	}
	return total, nil
}

// DeclaredTreeRoots returns every declared tree's resolved root, skipping the
// trees that are not configured on this host — the GATE's input (it asserts the
// CLI's plan face reaches exactly this set).
func DeclaredTreeRoots() map[string]string {
	out := make(map[string]string, len(DeclaredTrees()))
	for _, tree := range DeclaredTrees() {
		root, err := tree.Resolve()
		if err != nil || root == "" {
			continue
		}
		out[tree.Name] = root
	}
	return out
}

// declaredTreeForRoot answers "is this path ANOTHER declared tree's ROOT?" and
// returns that row — the ONE lookup that lets a container be handed to the row
// that owns it instead of being judged by whoever found it.
//
// WHY IT EXISTS (measured 2026-10-12). The queue arm judges the queue's direct
// children one by one — a good rule, and it is what stops one fresh child from
// shielding its stale siblings. But `test-budget` is itself a CONTAINER OF RUNS,
// so the arm's "one child, one verdict" landed on the container: a single live
// `make test-budget` kept it, and with it every run inside (320 directories /
// 10.27 GB, while `/` sat at 0 bytes free). A container that another tree's row
// already declares must therefore be handed to that row — the row knows its own
// namespace, its own agent window and its own candidates. This is the same rule
// the queue itself follows one level up: a namespace declaration is what makes
// its children judgeable, and the declaration lives with the owner.
//
// It is deliberately NOT a walk: the roots are already resolved by
// DeclaredTrees(), and the lookup refuses an EMPTY root (an unconfigured tree
// resolves to "" on a host that has no such directory, and an empty path would
// match everything).
// IT RETURNS EVERY ROW THAT ROOTS THERE, not the first one, and that is a
// correctness requirement rather than a generality: since 2026-11-22 two rows
// share the test-budget root (the binary directories and the flat streams, each
// with its OWN window — see declared_budget_evidence.go). A first-match lookup
// made the second row UNREACHABLE from the queue arm, so the home sweep and its
// dry-run silently judged half the tree while reporting the container as
// handed over — the same "a declared tree the arm never walks" defect this
// interposition was written to end, reintroduced one level down.
func declaredTreesForRoot(path string) []DeclaredTree {
	want := CachedCanonicalPath(path)
	var out []DeclaredTree
	for _, tree := range DeclaredTrees() {
		root, err := tree.Resolve()
		if err != nil || root == "" {
			continue
		}
		if want == root {
			out = append(out, tree)
		}
	}
	return out
}

// declaredTreeForRoot is the SINGLE-row form, kept for callers that need to
// answer "is this a declared root, and which row names it" (the gate, a report
// line). A caller that would SWEEP must use declaredTreesForRoot: taking the
// first row here is how a second row over the same root became invisible.
func declaredTreeForRoot(path string) (DeclaredTree, bool) {
	rows := declaredTreesForRoot(path)
	if len(rows) == 0 {
		return DeclaredTree{}, false
	}
	return rows[0], true
}

// sweepDeclaredTreeFromQueue collects one declared tree's candidates where the
// queue's own child walk found the tree's ROOT — the interposition
// queueSweepChildren makes. It reports the count, so the queue's total keeps
// meaning "entries removed", whatever arm removed them.
//
// It reuses the tree's own collect face and never touches the container itself:
// the container is the row's ROOT, and a declared root is refused at any age —
// the invariant that saved GOTMPDIR. Nothing about the protections changes
// (age, the indexed /proc holder verdict, the `.holder` registration); only the
// UNIT being judged is different.
//
// olderThan/holders/now are the QUEUE's own judgement of ITS children and are
// deliberately not forwarded: an interposed container is judged by the window on
// its own row (window()), never by the HOME arm's global one. Forwarding it was
// the 2026-10-27 defect — a 1-minute flag collected a 6h tree's entries through
// this call (task 01a17b8b-581a). The parameters stay in the signature because
// the caller has them in hand at that point in its walk and the call site reads
// as what it is (a queue child handed over), but nothing below consumes them; the
// tree's row decides.
func sweepDeclaredTreeFromQueue(tree DeclaredTree, _ time.Duration, _ HolderScan, _ time.Time) int {
	removed, _, err := SweepDeclaredTree(tree)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scratch: declared tree %s could not be swept from the queue: %v\n", tree.Name, err)
	}
	return removed
}
