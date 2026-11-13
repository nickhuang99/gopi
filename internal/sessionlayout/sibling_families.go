// sibling_families.go — the SIBLING TREES' family registry: the four trees that
// live BESIDE the sessions tree under the gopi profile root, each with an
// explicit decision.
//
// WHY THIS FILE EXISTS. families.go answers the RANGE question for ONE tree
// (`<profile>/sessions`): which family is this entry, and what did we decide
// about it. The profile root holds four more trees it never asked about —
// `archive/`, `backups/`, `notify/`, `dumps/` — and until this file existed they
// were measured by a `find` typed into a report, not by the registry the census
// face reads. Measured on this host (2026-10-01):
//
//	archive/   8 entries / 1.7 GiB   no writer in this tree (rescue tarballs)
//	backups/   510 entries / 925 MiB written by internal/tools.LedgerBackupDirName
//	notify/    4 entries / 100 MiB   written by the operator's settings.json `notify.cmd`
//	dumps/     4,321 entries / 102 MiB  written by internal/deepseek's error dump path
//
// A `find` in a report is an ANCHOR: it describes what somebody already noticed.
// The registry is the RANGE: `Classify` answers "which family is this entry?"
// for ANY entry these trees yield, so a family nobody registered is REPORTED
// (Rule 15) instead of falling outside every predicate.
//
// WHERE THE TREES ARE. Every tree here roots under the gopi PROFILE root
// (`GOPI_HOME`, i.e. `config.GopiDir()`), one level below it. That is exactly
// the resolution `internal/config` owns, so the leaf rule of this package holds:
// `siblingDataRootResolver` is a function SEAM, wired once by internal/config,
// and this file never imports it (the same pattern RawLogDir/SessionRawLogRoot
// use in internal/scratch, Rules 2/14).
//
// WHY dumps IS ALSO BOUNDED BY A DECLARED TREE. A decision is only worth what
// enforces it, and this package owns no machinery: the machinery that already
// ticks is the declared-tree sweep in internal/scratch (`gopi tool scratch-gc trees`,
// driven by the host watchdog and the scratch tick). So the ONE
// tree whose decision is `bound` names a declared tree row
// (internal/scratch/sibling_dumps.go, `sibling-dumps`), and the value below is
// the SAME value that row's `Tree.Name` carries — pinned to it by a test in
// internal/scratch that reads this source and compares the two literals, which
// is how the two packages share a name without an import cycle (internal/scratch
// imports internal/tools, so a reverse import is impossible).
//
// A decision of `keep` or `legacy` needs no machinery: the reason IS the
// contract, and a `legacy` row carries the measured verdict "no writer in this
// tree" so a future reader can tell "we chose to keep this" from "we never found
// who writes this".
//
// A NEW FAMILY IS A GATE-VISIBLE OMISSION, NOT SILENCE: ClassifySiblingFamily
// returns ok=false for anything the tree's registry does not claim, and the gate
// (TestSiblingTreeFamiliesAreClassified in internal/scratch, with its red-proof)
// fails on it.
//
// The decisions themselves — one row per family, with the measurement behind
// each, plus the `dumps/` discrepancy and why it is the env override rather than
// a code path — are the standing statement in
// devdoc/sessions-tree-siblings-census.md: read it before changing a Decision
// here, because a decision is a contract with whoever owns that tree.
package sessionlayout

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The SIBLING TREE NAMES. They are constants (Rules 1b/13/14) because the
// registry, the CLI's report line, the declared-tree row and the census
// document all name the same four trees: a second spelling anywhere would let
// one of them disagree with the rest.
const (
	// SiblingTreeArchive is the profile's `archive/` tree: rescue tarballs and
	// exported residue a run parked for a human.
	SiblingTreeArchive = "archive"
	// SiblingTreeBackups is the profile's `backups/` tree: the ledger's own
	// snapshots (internal/tools.LedgerBackupDirName mints its name).
	SiblingTreeBackups = "backups"
	// SiblingTreeNotify is the profile's `notify/` tree: the event streams an
	// operator's `notify.cmd` appends to.
	SiblingTreeNotify = "notify"
	// SiblingTreeDumps is the profile's `dumps/` tree: the request dumps the
	// error paths write when GOPI_ERROR_DUMP_DIR points at the profile instead
	// of the writer's default (the scratch dumps class tree).
	SiblingTreeDumps = "dumps"
	// SiblingTreeDumps1214 is the profile's `dumps-1214/` tree: the SAME shape
	// as `dumps/` for the SECOND dump knob, GOPI_1214_DUMP_DIR (the guard that
	// reacts to the provider's 1214 error class, internal/deepseek/error_1214.go).
	// It is registered although it holds nothing today, because the registry
	// describes the RANGE — which directory is ours to look at and what we
	// decided about it — and an empty tree is a fact about this host, not a
	// reason to leave the knob's target uncensused.
	SiblingTreeDumps1214 = "dumps-1214"
)

// The DECLARED TREE NAMES a `bound:` sibling family may name. They live here,
// in the leaf package both the registry and internal/scratch can import, so the
// tree a family is bound to is spelled exactly once (Rules 13/14).
const (
	// SiblingDumpsTree is internal/scratch's `sibling-dumps` declared tree: the
	// tree that bounds the profile's `dumps/` directory by the ONE namespace
	// predicate that is ours there — a request dump's own timestamp name
	// (`<ts>.dump.json`). The value is internal/scratch.SiblingDumpsTreeName; it
	// is RE-SPELLED here rather than imported because internal/scratch imports
	// internal/tools, which imports this package — the import would be a cycle.
	// The two spellings are held together by internal/scratch's
	// TestSiblingSiblingNamesMatchTheirWriters, which reads BOTH sources, so a
	// rename on either side fails there instead of leaving this row pointing at
	// a tree nothing bounds.
	SiblingDumpsTree = "sibling-dumps"
	// SiblingDumps1214Tree is internal/scratch's `sibling-dumps-1214` declared
	// tree: the same bound for the SECOND dump knob's directory. It has its own
	// tree rather than sharing `sibling-dumps` because the two knobs answer to
	// different guards (gopi-generic 400s vs the 1214 error class) and an
	// operator may point one at the profile and not the other — a shared row
	// would have to walk two roots and report one count. Re-spelled for the same
	// import-cycle reason, and pinned by the same test.
	SiblingDumps1214Tree = "sibling-dumps-1214"
)

// The measured SHAPE words of the sibling families. A shape token is what the
// census prints (one line per shape, not per file), so these are shared with
// the gate's fixture and the census document.
const (
	// SiblingShapeTarball is a probe's or a rescue's tarball (`*.tgz`, `*.tar.gz`).
	SiblingShapeTarball = "*.tgz"
	// SiblingShapeTarGz is the same artifact under the other suffix.
	SiblingShapeTarGz = "*.tar.gz"
	// SiblingShapeExport is an exported row set (`*.tsv`, `*.json`): a data
	// file a probe wrote for a human to read.
	SiblingShapeExport = "*.tsv"
	// SiblingShapeSidecarSuffix is the SQLite sidecar suffix (`-wal`, `-shm`).
	SiblingShapeSidecarSuffix = "-wal|-shm"
	// SiblingShapeEventStream is the notify feed's file name.
	SiblingShapeEventStream = "task-events.jsonl"
	// SiblingShapeUnstamped is the shape token of a timestamped dump name, the
	// ONE name family the profile dumps tree holds.
	SiblingShapeUnstamped = "<ts>.dump.json"
)

// SiblingFamily is ONE family of ONE sibling tree: what it matches, and the
// decision taken about it. It mirrors Family (families.go) on purpose — the two
// registries are read by the same census face, and a reader who has understood
// one has understood the other.
type SiblingFamily struct {
	// Name is the family's identity, unique within its tree (it is the key the
	// gate, the report line and the census document share).
	Name string
	// Decision is the explicit decision (DecisionBound/Keep/Legacy).
	Decision Decision
	// Tree names the declared tree that bounds this family. Non-empty IFF
	// Decision is DecisionBound — the gate checks both directions.
	Tree string
	// Reason is WHY, and it is the whole deliverable for a keep/legacy row: for
	// DecisionKeep it is the contract that keeps the family alive; for
	// DecisionLegacy it is the measured verdict (who was looked for and not
	// found); for DecisionBound it says which PART the tree collects, because a
	// reader must never read "bound" as "all of it".
	Reason string
	// Match is the family's membership test against one entry of the tree.
	Match func(SiblingEntry) bool
}

// SiblingEntry is one entry handed to a family's Match: the facts a
// classification may read. It is deliberately not an os.DirEntry, so a fixture
// tree and a live tree produce the same values.
type SiblingEntry struct {
	Path  string
	Name  string
	Shape string
	Dir   bool
	Where Where
}

// SiblingTree is ONE of the profile's sibling trees: its name, its registry and
// the one resolver that spells its root.
type SiblingTree struct {
	// Name is one of the SiblingTree* constants.
	Name string
	// Families is the tree's registry, in match order (the first family that
	// claims an entry owns it, so the ORDER is part of the meaning).
	Families []SiblingFamily
	// Resolve returns the tree's ROOT. Every spelling of the tree lives here.
	// An empty root with a nil error means the profile root could not be
	// resolved on this host (nothing to census), which a caller REPORTS.
	Resolve func() (string, error)
}

// SiblingTrees returns every sibling tree the registry declares, in a stable
// order. This is the
// ONE place a sibling tree is declared: the census face, the gate and the
// `--list` enumeration all read it.
func SiblingTrees() []SiblingTree {
	return []SiblingTree{
		{Name: SiblingTreeArchive, Families: archiveFamilies(), Resolve: func() (string, error) {
			return siblingTreeRoot(SiblingTreeArchive)
		}},
		{Name: SiblingTreeBackups, Families: backupsFamilies(), Resolve: func() (string, error) {
			return siblingTreeRoot(SiblingTreeBackups)
		}},
		{Name: SiblingTreeNotify, Families: notifyFamilies(), Resolve: func() (string, error) {
			return siblingTreeRoot(SiblingTreeNotify)
		}},
		{Name: SiblingTreeDumps, Families: dumpsFamilies(SiblingDumpsTree), Resolve: func() (string, error) {
			return siblingTreeRoot(SiblingTreeDumps)
		}},
		{Name: SiblingTreeDumps1214, Families: dumpsFamilies(SiblingDumps1214Tree), Resolve: func() (string, error) {
			return siblingTreeRoot(SiblingTreeDumps1214)
		}},
	}
}

// SiblingTreeByName returns the declared sibling tree with that name. ok is
// false for a name no tree carries — the state a CLI must report rather than
// silently census every tree instead.
func SiblingTreeByName(name string) (SiblingTree, bool) {
	for _, t := range SiblingTrees() {
		if t.Name == name {
			return t, true
		}
	}
	return SiblingTree{}, false
}

// SiblingTreeNames returns the four tree names in the same stable order.
func SiblingTreeNames() []string {
	trees := SiblingTrees()
	out := make([]string, 0, len(trees))
	for _, t := range trees {
		out = append(out, t.Name)
	}
	return out
}

// SiblingBoundTreeNames returns every declared tree a sibling family is bound
// to, deduplicated. It is what the cross-check in internal/scratch compares
// against the LIVE declared set — the mirror of BoundTreeNames for the
// sessions tree.
func SiblingBoundTreeNames() []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range SiblingTrees() {
		for _, f := range t.Families {
			if f.Decision != DecisionBound || f.Tree == "" || seen[f.Tree] {
				continue
			}
			seen[f.Tree] = true
			out = append(out, f.Tree)
		}
	}
	return out
}

// SiblingTreeOfFamily returns the tree a family belongs to. ok is false for a
// family name no tree declares.
func SiblingTreeOfFamily(name string) (string, bool) {
	for _, t := range SiblingTrees() {
		for _, f := range t.Families {
			if f.Name == name {
				return t.Name, true
			}
		}
	}
	return "", false
}

// ClassifySiblingFamily classifies ONE entry against ONE tree's registry. ok is
// false when no family of that tree claims it — the state a caller must REPORT,
// never ignore (Rule 15): an unclassified entry means the tree grew a family the
// registry does not know.
func ClassifySiblingFamily(tree SiblingTree, e SiblingEntry) (SiblingFamily, bool) {
	return classifySiblingWith(tree.Families, e)
}

// classifySiblingWith is the ONE classifier body: a tree's own order walked
// against a given list. ClassifySiblingFamily passes the real registry; the
// gate's red-proof passes a reduced one, so "the gate is red when a family is
// dropped" is proved by the SAME code the green run uses.
func classifySiblingWith(fams []SiblingFamily, e SiblingEntry) (SiblingFamily, bool) {
	for _, f := range fams {
		if f.Match != nil && f.Match(e) {
			return f, true
		}
	}
	return SiblingFamily{}, false
}

// SiblingScan is the census of ONE sibling tree.
type SiblingScan struct {
	// Tree is the tree's name, Root the tree that was walked.
	Tree string
	Root string
	// ByShape maps a shape token to what was measured for it (the same rollup
	// the sessions-tree census uses, so the two report faces agree about what a
	// "family" is).
	ByShape map[string]ShapeScan
	// Unclassified lists every entry no family of this tree claims.
	Unclassified []Unclassified
	// Dirs/files/bytes are the totals (they include every entry, classified or
	// not).
	Dirs  int
	Files int
	Bytes int64
}

// ScanSiblingTree walks ONE sibling tree and classifies every entry at its ROOT
// level.
//
// EXACTLY ONE LEVEL, and that boundary is deliberate (it mirrors
// ScanFamilies'): a directory the tree holds is classified by its OWN name and
// its contents are that container's business — descending into a rescue
// directory would turn a census of four trees into a walk of the operator's
// rescue data. A tree that does not exist is an ERROR, never an empty census:
// "nothing there" and "could not look" must not render the same (Rule 15).
func ScanSiblingTree(tree SiblingTree) (SiblingScan, error) {
	root, err := tree.Resolve()
	if err != nil {
		return SiblingScan{}, err
	}
	if root == "" {
		return SiblingScan{}, os.ErrNotExist
	}
	return ScanSiblingTreeWith(tree.Name, root, tree.Families)
}

// ScanSiblingTreeWith walks the tree rooted at root and classifies against the
// family list it is GIVEN. It exists for one reason: a gate must be able to
// drive the scan with a REDUCED registry and show that an entry the reduced list
// no longer claims comes back UNCLASSIFIED — a red-proof that wrote its own copy
// of the walk would prove nothing about the code that runs.
func ScanSiblingTreeWith(treeName, root string, fams []SiblingFamily) (SiblingScan, error) {
	scan := SiblingScan{Tree: treeName, Root: root, ByShape: map[string]ShapeScan{}}
	entries, err := os.ReadDir(root)
	if err != nil {
		return SiblingScan{}, err
	}
	for _, e := range entries {
		child := filepath.Join(root, e.Name())
		se := SiblingEntry{
			Path: child, Name: e.Name(), Shape: ShapeOf(e.Name()),
			Dir: e.IsDir(), Where: WhereRoot,
		}
		scan.add(fams, se, sizeOf(child, e.IsDir()))
	}
	return scan, nil
}

// add folds one classified entry into the scan's rollup.
func (s *SiblingScan) add(fams []SiblingFamily, e SiblingEntry, size int64) {
	if e.Dir {
		s.Dirs++
	} else {
		s.Files++
		s.Bytes += size
	}
	key := e.Shape
	if e.Dir {
		key = e.Shape + "/"
	}
	entry, ok := s.ByShape[key]
	if !ok {
		entry = ShapeScan{Dir: e.Dir}
	}
	entry.Count++
	entry.Bytes += size
	fam, ok := classifySiblingWith(fams, e)
	if !ok {
		entry.Family = ""
		entry.Decision = DecisionUnclassified
		s.Unclassified = append(s.Unclassified, Unclassified{
			Path: e.Path, Name: e.Name, Shape: e.Shape, Dir: e.Dir, Where: e.Where,
		})
	} else {
		entry.Family = fam.Name
		entry.Decision = fam.Decision
	}
	s.ByShape[key] = entry
}

// UnclassifiedShapes returns the DISTINCT shapes of the scan's unclassified
// entries with their counts, so one report line stands for a family rather than
// for ten thousand files.
func (s SiblingScan) UnclassifiedShapes() map[string]int {
	out := map[string]int{}
	for _, u := range s.Unclassified {
		key := u.Shape
		if u.Dir {
			key += "/"
		}
		out[key]++
	}
	return out
}

// SortedShapes returns the scan's shape tokens in a stable order, so a report
// is diffable between two runs (a map iteration order is not).
func (s SiblingScan) SortedShapes() []string {
	out := make([]string, 0, len(s.ByShape))
	for shape := range s.ByShape {
		out = append(out, shape)
	}
	sort.Strings(out)
	return out
}

// ---- the resolution seam -------------------------------------------------

// errSiblingResolverUnwired reports the one failure mode of the seam below: a
// caller that resolved a sibling tree before internal/config wired the resolver
// (only reachable from a test that builds this package standalone).
var errSiblingResolverUnwired = errors.New("sessionlayout: sibling-tree resolvers are not wired (import internal/config)")

// siblingDataRootResolver resolves the gopi PROFILE root (`GOPI_HOME`, i.e.
// `config.GopiDir()`). internal/config assigns it once from its own init, the
// same "leaf declares the shape, the owner supplies the value" pattern
// internal/scratch uses for the raw-log trees.
var siblingDataRootResolver = func() (string, error) { return "", errSiblingResolverUnwired }

// RegisterSiblingDataRootResolver installs the profile-root resolver. A nil
// argument is ignored, so a caller cannot un-wire it by accident.
func RegisterSiblingDataRootResolver(f func() (string, error)) {
	if f != nil {
		siblingDataRootResolver = f
	}
}

// siblingTreeRoot resolves one sibling tree's root: the profile root plus the
// tree's own leaf name. The name is the tree's identity, so the join is derived
// from it rather than from a second table (Rules 13/14).
func siblingTreeRoot(name string) (string, error) {
	root, err := siblingDataRootResolver()
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", nil
	}
	return filepath.Join(root, name), nil
}

// ---- the four registries -------------------------------------------------

// The measured name ingredients of the sibling families. Every one of them is a
// value a writer or the operator's own configuration produced, named once.
const (
	// backupSidecarWal / backupSidecarShm are SQLite's own sidecar suffixes.
	backupSidecarWal = "-wal"
	backupSidecarShm = "-shm"
	// backupFilePrefix is internal/tools' own artifact prefix
	// (LedgerBackupFilePrefix, "memory.db.backup"); re-spelled here for the
	// same reason the sessions registry re-spells its tree names — the two
	// spellings are pinned to each other by the boundary test.
	backupFilePrefix = "memory.db.backup"
	// backupStagingSuffix is the suffix of an artifact that is NOT yet proved
	// good (internal/tools' ledgerBackupStagingSuffix, ".partial").
	backupStagingSuffix = ".partial"
	// backupSnapshotSuffix is what the writer's own artifact ENDS with
	// (`memory.db.backup-<stamp>.db`) — the other half of its identity, so the
	// snapshot family matches the writer's artifact and nothing beside it (a
	// `-wal`/`-shm` beside it is the SIDECAR family's, never this one's).
	backupSnapshotSuffix = ".db"
)

// The measured FOREIGN backup prefixes: snapshots of the ledger a person or a
// one-off probe made by hand (`memory.db.bak-latprov-…`, an old host's copy). They
// are a TABLE rather than a catch-all for the same reason the sessions-root
// residue family is: a catch-all would make every new backup shape invisible to
// the gate, and an undiscovered family is exactly what this registry exists to
// report.
// backupForeignPrefixes are the measured prefixes of the hand-made snapshots.
//
// The writer's OWN prefix (`memory.db.backup`) is deliberately ABSENT: the exact
// snapshot and staging families above already claim it, and a catch-all here
// would SHADOW them — dropping either of those rows would then go on matching
// through this one, which is the difference between a gate and a decoration. The
// red-proof in sibling_tree_families_test.go caught exactly that when the
// prefix was present (2026-10-01), and it is why no prefix may overlap another
// family's names: every row here is load-bearing or it is not in the table.
//
// NO HOSTNAME IS SPELLED HERE. One snapshot on this host was named after the
// machine that copied it (`memory.db.<host>-backup`), and writing that host
// into the table would bind this registry to one machine: the same hand-copy
// made from any OTHER machine would reach the gate as UNCLASSIFIED. The
// `-backup` suffix is the MEASURED SHAPE and the shape is what the family is
// about, so the suffix is the prefix and the host stays out of the code.
const (
	// ledgerSnapshotStem is the ledger file a snapshot is taken OF.
	ledgerSnapshotStem = "memory.db."
	// backupHostCopiedSuffix is the suffix such a snapshot carries.
	backupHostCopiedSuffix = "-backup"
)

var backupForeignPrefixes = []string{
	"memory.db.bak-",
	"memory-glm-audit-",
}

// backupWorktreeResiduePrefixes are the measured prefixes of the ARCHIVED
// WORKTREE residue directories this tree accumulates: a hand-run script's dump
// of a worktree it was releasing (`wt-release-<date>/`, `wt-sweep-<date>/`).
// An EXACT table, never a catch-all (see the family's own reason).
var backupWorktreeResiduePrefixes = []string{"wt-release-", "wt-sweep-"}

// The measured shapes of an ARCHIVE tree entry: a rescue tarball, a raw data
// export, and the directory a rescue was unpacked into.
var (
	archiveTarballSuffixes = []string{".tgz", ".tar.gz"}
	archiveExportSuffixes  = []string{".tsv", ".json"}
	archiveDirPrefixes     = []string{"workspace-rescue-"}
)

// The measured shapes of a NOTIFY tree entry: the operator's feed command
// appends ONE event stream, and its backup beside it is the same family.
var notifyNamePrefixes = []string{"task-events.", "night-watch-"}

// The measured shapes of an error-path DUMP name: a timestamp, optionally with
// the collision suffix writeDumpFile appends ("…-1.dump.json").
const (
	// dumpNameSuffix is the suffix every request dump carries
	// (internal/deepseek's dumpFileSuffix).
	dumpNameSuffix = ".dump.json"
	// dumpNameDigits is the alphabet of a dump name's stamp part.
	dumpNameDigits = "0123456789-"
)

// isDumpTimestampName reports whether name is a request dump's own name
// (`20261001-104445.dump.json`, `20261001-104441-2.dump.json`). It is the
// tree's NAMESPACE PREDICATE: a `.dump.json` that is not stamped this way was
// not written by the dump path and is never a candidate.
func isDumpTimestampName(name string) bool {
	stem, ok := strings.CutSuffix(name, dumpNameSuffix)
	if !ok || stem == "" {
		return false
	}
	return strings.Trim(stem, dumpNameDigits) == ""
}

// archiveFamilies is the `archive/` tree's registry, in match order.
//
// MEASURED (this host, 2026-10-01): 8 entries / 1.7 GiB — two packed rescue
// tarballs (`sandbox-chat-shot-…tgz`, 692 KiB), a legacy row export
// (`legacy-t-rows-export.tsv`, 547 KiB), and one unpacked rescue directory
// (`workspace-rescue-20260920/`) holding 1.6 GiB of loose evidence.
func archiveFamilies() []SiblingFamily {
	return []SiblingFamily{
		{
			Name:     "archive-rescue-tarball",
			Decision: DecisionLegacy,
			Reason: "no writer in this tree: a packed rescue snapshot (`*.tgz`, `*.tar.gz`) " +
				"is evidence a person or a one-off probe packed by hand before clearing a tree — " +
				"the measured members are `sandbox-chat-shot-<id>-<stamp>.tgz` and the same artifact " +
				"as `.tar.gz`. Nothing in this repository unpacks or re-derives one, so an age window " +
				"would destroy a rescue whose only copy it is",
			Match: func(e SiblingEntry) bool {
				if e.Dir {
					return false
				}
				for _, s := range archiveTarballSuffixes {
					if strings.HasSuffix(e.Name, s) {
						return true
					}
				}
				return false
			},
		},
		{
			Name:     "archive-export",
			Decision: DecisionLegacy,
			Reason: "no writer in this tree: a raw export a probe wrote for a human to read " +
				"(`legacy-t-rows-export.tsv`, `*.json`). The repository's current exporters write " +
				"into a session container or print to stdout, so this is residue — kept because a " +
				"reader may still be its only reference",
			Match: func(e SiblingEntry) bool {
				if e.Dir {
					return false
				}
				for _, s := range archiveExportSuffixes {
					if strings.HasSuffix(e.Name, s) {
						return true
					}
				}
				return false
			},
		},
		{
			// A DIRECTORY at the archive root: an unpacked rescue. It is
			// classified by its own name and NOT descended into — the census
			// boundary of every tree here is one level.
			Name:     "archive-rescue-dir",
			Decision: DecisionLegacy,
			Reason: "no writer in this tree: `workspace-rescue-<date>/` is the directory a packed " +
				"rescue was unpacked into (measured: 1.6 GiB of loose evidence — the larger half of " +
				"this tree). Its contents are the rescue's business, not the registry's; a sweep that " +
				"bounded it would be bounding somebody's rescue",
			Match: func(e SiblingEntry) bool {
				if !e.Dir {
					return false
				}
				for _, p := range archiveDirPrefixes {
					if strings.HasPrefix(e.Name, p) {
						return true
					}
				}
				return false
			},
		},
	}
}

// backupsFamilies is the `backups/` tree's registry, in match order.
//
// MEASURED (this host, 2026-10-01): 510 entries / 925 MiB. The WRITER is
// internal/tools' ledger backup path (`LedgerBackupDirName` = "backups",
// resolved by ResolveLedgerBackupDir): `gopi memory backup` writes
// `memory.db.backup-<stamp>.db` through a `.partial` staging name.
func backupsFamilies() []SiblingFamily {
	return []SiblingFamily{
		{
			// The writer's own artifact — the ONE family here that is minted by
			// a code path in this repository.
			Name:     "ledger-backup-snapshot",
			Decision: DecisionKeep,
			Reason: "a ledger snapshot minted by internal/tools' backup path (`gopi memory backup`): " +
				"the file name is `memory.db.backup-<stamp>.db`, written through a `.partial` staging " +
				"name and verified before the rename. It is RESTORE material — the ledger's own " +
				"disaster-recovery artifact, and a peer's last good copy may be the one on this disk — " +
				"so nothing ages it out. Bounding it would need a retention decision with the " +
				"operator's disaster-recovery contract in hand, and it belongs to the memory subsystem, " +
				"not to this tree's family registry",
			Match: func(e SiblingEntry) bool {
				return !e.Dir && strings.HasPrefix(e.Name, backupFilePrefix) &&
					strings.HasSuffix(e.Name, backupSnapshotSuffix) &&
					!strings.HasSuffix(e.Name, backupStagingSuffix)
			},
		},
		{
			// The staging name of an in-flight snapshot: the artifact the
			// writer is building. A sweep that took it would break a running
			// backup, and the writer removes it itself on success.
			Name:     "ledger-backup-staging",
			Decision: DecisionKeep,
			Reason: "the `.partial` staging name of a snapshot being written: internal/tools renames " +
				"it to the final name only after the artifact is verified, and it clears it on a " +
				"failure it survives. Nothing else may touch it — an unverified artifact is not a " +
				"backup, and deleting one mid-write destroys the run that is producing it",
			Match: func(e SiblingEntry) bool {
				return !e.Dir && strings.HasSuffix(e.Name, backupStagingSuffix)
			},
		},
		{
			// SQLite's sidecars. They belong to the artifact whose name they
			// extend and are meaningless apart from it.
			Name:     "ledger-backup-sidecar",
			Decision: DecisionKeep,
			Reason: "SQLite's own sidecar of a backup artifact beside it (`-wal`, `-shm`): the pair " +
				"is one artifact — a reader restoring the `.db` reads them together, and a sweep that " +
				"collected them would leave a snapshot that cannot be opened. They are removed with " +
				"their artifact, by whoever removes the artifact, never on their own age",
			Match: func(e SiblingEntry) bool {
				return !e.Dir && (strings.HasSuffix(e.Name, backupSidecarWal) ||
					strings.HasSuffix(e.Name, backupSidecarShm))
			},
		},
		{
			// Everything else — the hand-made snapshots, as an EXACT table of
			// the measured prefixes (never a catch-all: an unseen shape must
			// reach the gate as UNCLASSIFIED).
			Name:     "ledger-backup-foreign",
			Decision: DecisionKeep,
			Reason: "a ledger snapshot a person or a one-off probe copied by hand " +
				"(`memory.db.bak-latprov-…`, `memory.db.bak-pre-legacy-purge-…`, " +
				"`memory.db.<host>-backup`, `memory-glm-audit-….db`, with their `-wal`/`-shm` " +
				"sidecars): measured 15 of this tree's 21 top-level entries, against 3 written by the " +
				"backup path itself. No writer in this repository mints these names — internal/tools' " +
				"own prefix is `memory.db.backup` — and each one is the only copy of a prior ledger " +
				"state, so they are kept on purpose and enumerated by PREFIX rather than by a " +
				"catch-all, so a new backup shape still reaches the gate",
			Match: func(e SiblingEntry) bool {
				if e.Dir {
					return false
				}
				for _, p := range backupForeignPrefixes {
					if strings.HasPrefix(e.Name, p) {
						return true
					}
				}
				// the host-copied snapshot shape: `memory.db.<anything>-backup…`
				// (a trailing date is part of the measured shape, so the mark is
				// looked for anywhere after the stem, not only at the very end)
				if !strings.HasPrefix(e.Name, ledgerSnapshotStem) {
					return false
				}
				return strings.Contains(e.Name[len(ledgerSnapshotStem):], backupHostCopiedSuffix)
			},
		},
		{
			// A DIRECTORY at the backups root: the worktree residue a hand-run
			// release script archived here (measured: 119 MiB across three such
			// directories, the second largest family in this tree).
			Name:     "worktree-residue-archive",
			Decision: DecisionLegacy,
			Reason: "no writer in this tree: `wt-release-<date>/` and `wt-sweep-<date>/` are what " +
				"the worktree-release scripts (which lived in the operator's home, measured " +
				"`~/wt-release*.sh`, and are no longer in this repository) copied aside " +
				"before reclaiming a worktree — devdoc/worktree-residue-release.md records the run. " +
				"No code path in this tree mints these names, so they are residue; they are KEPT " +
				"because one may be the last copy of a released worktree's dirty files, and the " +
				"measured size (119 MiB of the tree's 925 MiB) makes a sweep of them a decision " +
				"somebody takes on purpose, not an age window invented here",
			Match: func(e SiblingEntry) bool {
				if !e.Dir {
					return false
				}
				for _, p := range backupWorktreeResiduePrefixes {
					if strings.HasPrefix(e.Name, p) {
						return true
					}
				}
				return false
			},
		},
	}
}

// notifyFamilies is the `notify/` tree's registry, in match order.
//
// MEASURED (this host, 2026-10-01): 4 entries / 100 MiB. The WRITER is not code
// in this tree at all: it is the operator's own settings.json `notify.cmd`
// (measured: `awk '{print; print ""}' | tee -a /home/nick/.gopi/notify/task-events.jsonl`),
// run by internal/tools' notify path on every task transition. The path is
// therefore CONFIGURATION, and a keeper of this tree is the operator, not the
// repository.
func notifyFamilies() []SiblingFamily {
	return []SiblingFamily{
		{
			// The event stream the operator's command appends to, plus the
			// backup of it a person took beside it.
			Name:     "notify-event-stream",
			Decision: DecisionKeep,
			Reason: "the task-event stream the operator's own settings.json `notify.cmd` appends to " +
				"(measured: `tee -a <profile>/notify/task-events.jsonl`, ~100 MiB of one line per task " +
				"transition, plus `…jsonl.bak-pre-newline-<stamp>` beside it). The WRITER is operator " +
				"configuration, not a code path in this repository: the repository only runs the " +
				"command, so the retention contract belongs to whoever wrote it. gopi must never " +
				"collect a file its operator asked for by name — an append-only feed has no safe " +
				"truncation point, and the size is the operator's own reporting, not garbage",
			Match: func(e SiblingEntry) bool {
				if e.Dir {
					return false
				}
				for _, p := range notifyNamePrefixes {
					if strings.HasPrefix(e.Name, p) {
						return true
					}
				}
				return false
			},
		},
	}
}

// dumpsFamilies is a dump tree's registry: ONE family, and it is the ONE kind of
// sibling family that is BOUND.
//
// THE DISCREPANCY THIS ROW RESOLVES (measured 2026-10-01). internal/deepseek's
// dump writers document their default as the SCRATCH dumps class tree
// (`internal/scratch.DumpDir(<class>)` — `<scratch home>/dumps/…`, deliberately
// never on `/`), and that default IS honoured: nothing in this repository writes
// `<profile>/dumps`. The tree nevertheless grew THAT DAY (4,345 files / 102 MiB,
// 48 of them written the same morning) because the PROFILE's `dumps/` is what
// the writers' FIRST-ranked source names: `GOPI_ERROR_DUMP_DIR` (read in exactly
// one place, minimalCoreDumpDir), which this host's operator exports from
// `~/.bashrc:139`, so every live gopi — and every child it spawns — inherits it.
// The path that writes this tree is therefore the ENV OVERRIDE, not a code path,
// and the fix is a corrected statement (CONFIG.md + devdoc) plus a real bound,
// not a writer change: the operator asked for the profile path on purpose, and
// overriding their choice would be the repository deciding where their
// diagnostics live.
//
// THE SAME SHAPE GOVERNS THE SECOND KNOB, and it is why this function takes the
// tree it declares rather than naming one: `GOPI_1214_DUMP_DIR` (read once, in
// guardDumpDir) points the profile's `dumps-1214/` the same way — measured on
// this host, exported from the same shell file, EMPTY today. Registering only
// the tree that happens to hold files would be an anchor again; the parameter is
// what makes this registry describe the RANGE of the two durable locations these
// two knobs can create under the profile.
func dumpsFamilies(declaredTree string) []SiblingFamily {
	return []SiblingFamily{
		{
			Name:     "error-request-dump",
			Decision: DecisionBound,
			Tree:     declaredTree,
			Reason: "a request dump the error paths write before they fail " +
				"(`<stamp>.dump.json`, internal/deepseek.writeDumpFile -> minimalCoreDumpDir / " +
				"guardDumpDir). It lands HERE because the dump knobs point the profile's dump " +
				"directories at it (the writers' documented default is the scratch dumps class tree, " +
				"internal/scratch.DumpDir, which is honoured when the variable is unset). The declared " +
				"tree named above collects ONLY the entries whose name IS a dump's own timestamp name " +
				"(`<stamp>.dump.json`, the collision form `<stamp>-<n>.dump.json`), and only once they " +
				"are older than its named window: a dump is replay material for a failure that has " +
				"already been diagnosed, and the writer itself never reads one back",
			Match: func(e SiblingEntry) bool {
				return !e.Dir && isDumpTimestampName(e.Name)
			},
		},
	}
}
