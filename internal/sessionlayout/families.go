// families.go — the SESSIONS TREE's family registry: every family of files and
// directories a gopi sessions tree holds, each with an EXPLICIT decision.
//
// WHY THIS FILE EXISTS. The declared-tree registry (internal/scratch) bounds
// the sessions tree by a NAMESPACE PREDICATE — `*.raw.log` — and a namespace
// predicate can only ever describe the families somebody already thought of.
// Measured on this host (2026-10-01): the sessions tree held 57 GiB, of which
// the declared `session-raw` tree covered 45.8 GiB / 1655 files, while
// `attachments/` (75,843 directories), 79,401 `metadata.json`, 3,146 records,
// 1,773 crash dumps, 1,191 reports, 950 worker logs and per-run working
// directories were bounded by NOTHING and named by NOTHING. The gate that
// should have caught it (TestDeclaredTreesCoverRepoWriters) is ANCHOR-driven:
// it checks that the trees on a HAND-WRITTEN writer list are declared, never
// that the list is complete.
//
// THIS PACKAGE supplies the missing half: the RANGE. `ClassifyFamily` answers
// "which family is this entry?" for ANY entry a real sessions tree yields, so a
// family nobody registered is REPORTED (Rule 15: an unclassified entry is
// never silently ignored) instead of falling outside every predicate.
//
// WHY HERE. This package already owns the SHAPE of a sessions tree (it is the
// leaf that says which `*.jsonl` is a record and which is a side file), it has
// no project imports, and internal/scratch — which owns the declared trees —
// already imports it. The registry therefore lives beside the shape it
// classifies and is reachable from the sweep without a cycle.
//
// WHAT A DECISION MEANS (exactly four values, no fifth):
//
//	bound:<tree>  a declared tree in internal/scratch.DeclaredTrees() collects
//	              this family — and the gate requires that tree to EXIST, so
//	              "declared but not swept" cannot pass.
//	keep:<reason> this family must NOT be aged out. The reason is the contract
//	              that keeps it alive (a record a reader resumes from, evidence
//	              no other copy holds).
//	legacy        a family with NO writer in this tree: residue from a layout
//	              or a mechanism that no longer exists. It is named here so it
//	              is visible; bounding it is a separate, deliberate decision
//	              (the epic dispatches it), not an age window invented here.
//	              The same value carries the measured verdict "no writer", so
//	              a future reader can tell "we chose to keep this" from "we
//	              never found who writes this".
//
// WHO KEEPS THE KEY. The two root-namespace predicates (the task-named
// container and the root-residue file) live in THIS package and the declared
// tree that bounds those families REFERENCES them (internal/scratch imports
// this one — the direction already exists). It is the same rule the raw-log
// family follows for `session-raw`: the family's membership test and the
// tree's namespace predicate must be ONE function, or the sweep collects a
// set the registry calls "not this family" (Rule 13/14 — a value with two
// spellings is a defect, and here the second spelling would delete files).
//
// WHAT `bound:` CLAIMS, EXACTLY. `bound:` says a declared tree's namespace
// predicate IS this family's membership test — never "every member of the
// family is collected". The container-file families below are matched by a
// PREFIX of their shape (a probe's artifact, `<id>-seq.json`), and the tree
// that bounds them carries the same predicate; a bound is a set equality, so a
// `bound:` row whose tree matched a different set would be refused by the gate
// (internal/scratch/sessions_tree_families_test.go cross-checks the tree the
// row names against DeclaredTrees(), and the tree's predicate is written in
// terms of this package's exported functions).
//
// A NEW FAMILY IS A GATE-VISIBLE OMISSION, NOT SILENCE: ClassifyFamily returns
// ok=false for anything the registry does not claim, and
// internal/scratch's TestSessionsTreeFamiliesAreClassified fails red on it.
package sessionlayout

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The family NAMES. They are constants (PROJECT.md Rules 1b/13/14) because the
// gate, the CLI's report line and the census document all name the same
// families: a second spelling anywhere would let one of the three disagree.
const (
	// FamilySessionContainer is a per-session container directly under the
	// sessions root (`<uuid>/`): the ledger's own directory for one session or
	// task run. Its members are classified one level deeper.
	FamilySessionContainer = "session-container"
	// FamilyTaskContainer is a container named by a TASK id rather than a
	// session uuid (`t-<epoch>`, a 32-hex row id, `process-<pid>`): the
	// pre-session and non-session containers a run leaves behind.
	FamilyTaskContainer = "task-container"
	// FamilyForeignCheckout is a container holding a `.git` directory: a
	// probe's clone of this repository (`smoke-clone`, `master-check`,
	// `tree-branch`, …). A whole CHECKOUT living inside a sessions tree is a
	// different shape from every other family here, and it is what makes the
	// tree grow by hundreds of megabytes per probe.
	FamilyForeignCheckout = "foreign-checkout"
	// FamilySandboxProfile is a container holding a `.gopi` profile
	// directory: a sandbox or a private profile a run materialised in place.
	FamilySandboxProfile = "sandbox-profile"
	// FamilySessionScratchDir is any other DIRECTORY inside a container: a
	// run's working directory, its own scratch, its evidence tree.
	FamilySessionScratchDir = "session-scratch-dir"
	// FamilyAttachmentsDir is the attachments area of a session container
	// (`attachments/`): the archive of files a task was submitted with. It
	// holds GB-scale unique bundles and is the family the epic refuses to age
	// out blindly (a branch's only surviving copy lives in one of them).
	FamilyAttachmentsDir = "attachments-archive"
	// FamilySessionRecord is a session RECORD — the `*.jsonl` this package's
	// own layout rules classify as one (RecordName or a per-run `<uuid>.jsonl`).
	FamilySessionRecord = "session-record"
	// FamilySideRecord is a registered side file of a session record
	// (signal-provenance.jsonl and any other JSONL filed beside a bare
	// record).
	FamilySideRecord = "session-side-record"
	// FamilyCrashDump is a crash dump (`<id>.dump.json`): the writer's own
	// artifact for a failed run.
	FamilyCrashDump = "session-crash-dump"
	// FamilyRawHTTPLog is the per-run raw HTTP log (`.raw.log`) — the ONE
	// family the declared `session-raw` tree bounds today.
	FamilyRawHTTPLog = "raw-http-log"
	// FamilySessionMetadata is `metadata.json`, the run's metadata record.
	FamilySessionMetadata = "session-metadata"
	// FamilyRunEvidence is any other FILE inside a container: a report, a
	// log, a dump, a script or a harness a run wrote as its own evidence.
	FamilyRunEvidence = "run-evidence"
	// FamilyRootStrayDir is a DIRECTORY directly under the sessions root that
	// is neither an id-named container nor a shape any writer mints: a
	// hand-run probe's directory, a harness's evidence tree.
	FamilyRootStrayDir = "root-stray-dir"
	// FamilySessionsRootResidue is a FILE directly under the sessions root
	// that is not one of the recognized families: operator residue from a
	// hand-run probe, a migration manifest, a log somebody parked there.
	FamilySessionsRootResidue = "sessions-root-residue"
	// FamilySessionsRootInboxDir is the `messages/` DIRECTORY directly under
	// the sessions root: an inbox whose session was never named.
	//
	// It is its own family rather than a member of the root-stray family
	// because the two answer different questions. A stray directory is a
	// probe's name (`zz-…`, `pick`); this one is a name the SHAPE of a session
	// tree already uses — `SessionMessagesDirName`, which is correct ONE LEVEL
	// DOWN inside a container — reached at the wrong DEPTH. Registering it
	// under the stray table would also hand it that family's `bound:` decision
	// and let an age window remove a directory that may be holding
	// undelivered messages; a `legacy` row adds no sweep arm at all (Rule 15:
	// report the shape, let a human decide).
	FamilySessionsRootInboxDir = "sessions-root-inbox"
)

// Decision is what the registry DECIDES about a family. Four values, and the
// fourth is the one that makes an omission visible.
type Decision string

const (
	// DecisionBound — a declared tree collects this family. Tree names it.
	DecisionBound Decision = "bound"
	// DecisionKeep — this family is deliberately NOT aged out; Reason is the
	// contract that keeps it.
	DecisionKeep Decision = "keep"
	// DecisionLegacy — a family with no writer in this tree (measured), kept
	// visible so bounding it is a decision somebody takes on purpose.
	DecisionLegacy Decision = "legacy"
	// DecisionUnclassified — the classification of an entry the registry does
	// not claim. It is never stored on a Family; it is what ClassifyFamily
	// reports, and what the gate fails red on.
	DecisionUnclassified Decision = "unclassified"
)

// Family is ONE family of the sessions tree: what it matches, and the decision
// taken about it.
type Family struct {
	// Name is the family's identity — one of the Family* constants. It is the
	// key the gate, the report line and the census all share.
	Name string
	// Class is the ROW TYPE, referenced from the scratch tree classes so the
	// two vocabularies cannot drift (Rule 13/14).
	Class Class
	// Where says at which depth of the sessions tree the family lives.
	Where Where
	// Decision is the explicit decision (DecisionBound/Keep/Legacy).
	Decision Decision
	// Tree names the declared tree that bounds this family. Non-empty IFF
	// Decision is DecisionBound — the gate checks both directions, so a
	// `bound:` row whose tree does not exist cannot pass, and a row that names
	// a tree without saying `bound:` cannot either.
	Tree string
	// Reason is WHY. For DecisionKeep it is the contract that keeps the family
	// alive; for DecisionLegacy it is the measured verdict (who was looked for
	// and not found); for DecisionBound it is what the tree actually collects
	// (a declared tree may bound only PART of a family — the reason says
	// which part, because a reader must never read "bound" as "all of it").
	Reason string
	// Match is the family's membership test.
	Match func(Entry2) bool
}

// Class is the row type of a family — the same coarse taxonomy the declared
// trees use, so a `bound:` family and its tree speak one vocabulary.
type Class string

const (
	// ClassSessionRecord — the ledger's own record files.
	ClassSessionRecord Class = "session-record"
	// ClassRunArtifact — an artifact a run wrote (a report, a log, a raw log).
	ClassRunArtifact Class = "run-artifact"
	// ClassRunWorkDir — a directory a run used as its working area.
	ClassRunWorkDir Class = "run-work-dir"
	// ClassImageArchive — an archive of unique files (attachments).
	ClassImageArchive Class = "archive"
	// ClassResidue — a family whose writer is not in this tree.
	ClassResidue Class = "residue"
)

// Where is the depth at which a family is found. It is part of the identity:
// `metadata.json` inside a container is a run's record, while a stray
// `metadata.json` AT THE ROOT is residue — and a match rule without the depth
// could not tell them apart.
type Where string

const (
	// WhereRoot — an entry directly under the sessions root.
	WhereRoot Where = "root"
	// WhereContainer — an entry directly inside a session/task container.
	WhereContainer Where = "container"
)

// Entry2 is one entry handed to a family's Match: the facts a classification
// may read. It is deliberately NOT an os.DirEntry (which cannot be built by a
// test) — a fixture tree and a live tree produce the same values.
//
// Hidden returns whether the entry's DIRECTORY holds a child of that name. It
// is a function, not a bool, because asking costs a stat and only two families
// need the answer: a walk over 200k entries must not pay for them.
type Entry2 struct {
	Path  string
	Name  string
	Shape string
	Dir   bool
	Where Where
	// Hidden reports whether a directory holds a child with that base name.
	// It is nil for a file entry, and it must never be called for one.
	Hidden func(name string) bool
}

// ShapeOf reduces a NAME to a stable shape token: ids and numbers are
// replaced so that `<uuid>.jsonl`, `01a0e67f….jsonl` and `session.jsonl` land
// on shapes a reader can enumerate, and so a fixture and the live tree produce
// the SAME token for the same family (the gate's redness depends on it).
//
// The order matters: a canonical uuid first, then a dashed task id (a full
// uuid's window is narrower, so it must be tried first), then a 16+ hex run,
// then any remaining digit run.
func ShapeOf(name string) string {
	s := uuidShapeRE.ReplaceAllString(name, shapeIDToken)
	s = taskIDShapeRE.ReplaceAllString(s, shapeIDToken)
	s = hexShapeRE.ReplaceAllString(s, shapeIDToken)
	return digitShapeRE.ReplaceAllString(s, shapeNumToken)
}

// The tokens a shape is built from, and the patterns that produce them.
const (
	// shapeIDToken replaces any identifier run (a uuid, a task id, a hash).
	shapeIDToken = "<id>"
	// shapeNumToken replaces any remaining number run.
	shapeNumToken = "N"
)

var (
	// uuidShapeRE matches a canonical 36-character uuid.
	uuidShapeRE = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	// taskIDShapeRE matches a dashed task id: eight hex characters followed by
	// one to four hex groups (`01a0ac30-b233`).
	taskIDShapeRE = regexp.MustCompile(`[0-9a-f]{8}(?:-[0-9a-f]{2,8}){1,4}`)
	// hexShapeRE matches a bare hex run long enough to be an id rather than a
	// word (`9f4a…`, 16+ characters).
	hexShapeRE = regexp.MustCompile(`[0-9a-f]{16,}`)
	// digitShapeRE matches any remaining number run.
	digitShapeRE = regexp.MustCompile(`[0-9]+`)
)

// The SHAPE constants of the families whose identity is a name SHAPE rather
// than a fixed name. They are spelled once here and referenced by the Match
// functions and by the gate's fixture, never re-typed.
const (
	// ShapeRecordName is the bare-session record's shape ("session.jsonl").
	ShapeRecordName = RecordName
	// ShapeRunRecord is a per-run record's shape ("<uuid>.jsonl").
	ShapeRunRecord = shapeIDToken + JSONLSuffix
	// ShapeRawHTTPLog is a raw HTTP log's shape ("<uuid>.raw.log").
	ShapeRawHTTPLog = shapeIDToken + ".raw.log"
	// ShapeCrashDump is a crash dump's shape ("<uuid>.dump.json").
	ShapeCrashDump = shapeIDToken + ".dump.json"
	// ShapeSessionCrashDump is the bare session's crash dump shape.
	ShapeSessionCrashDump = "session.dump.json"
	// ShapeContainer is a session container's shape (the uuid token alone).
	ShapeContainer = shapeIDToken
)

// The DECLARED TREE NAMES a `bound:` family may name. They live here, in the
// leaf package both the registry and internal/scratch can import, so the tree a
// family is bound to is spelled exactly once (Rules 13/14) — the cross-check
// that a `bound:` row names a tree that really exists is a comparison of two
// references to this ONE value, not of two spellings that could drift.
const (
	// TreeSessionRaw is internal/scratch's `session-raw` declared tree: the
	// tree that bounds `*.raw.log` inside the sessions tree.
	TreeSessionRaw = "session-raw"
	// TreeSessionLegacy is internal/scratch's `session-legacy` declared tree:
	// the tree that bounds the LEGACY residue families at the sessions ROOT —
	// the task-named containers (`t-<epoch>`, `process-<pid>`) and the
	// root-stray directories that no writer in this tree mints.
	//
	// It is ROOT-LEVEL ONLY, and that is the safety property rather than a
	// simplification: the sweep refuses the ROOT of a declared tree at any age
	// (internal/scratch.removeCollectableOfTree), so a row rooted AT the
	// sessions tree can NEVER remove the tree — only the aged, unheld CHILDREN
	// its namespace predicate claims. Recall a bounded family's reason: the
	// `bound:` value claims the tree's predicate IS this family's membership
	// test, and the two are written from these same functions.
	TreeSessionLegacy = "session-legacy"
	// TreeSessionLegacyContainers is the SECOND row of the same tree: the
	// legacy residue that is NOT at the root but ONE LEVEL DOWN inside a
	// session container — a probe's clone (a directory holding `.git`) and a
	// private profile (a directory holding `.gopi`). It shares the ROOT (the
	// sessions tree itself) with the row above and is a separate row because
	// its candidates are a different shape, answered by a different predicate:
	// the container must be AGED and UNHELD before anything inside it is even
	// considered — the one-level-down rule the raw-log container arm already
	// applies, and the reason a live run's checkout can never be a candidate.
	TreeSessionLegacyContainers = "session-legacy/containers"
	// TreeSessionAttachments is internal/scratch's `session-attachments`
	// declared tree: the tree that bounds the per-task `attachments/` archives
	// by the EVIDENCE predicate (an ordinary, empty, unheld archive holds
	// nothing a copy could survive in), never by an age window. The value is
	// internal/scratch.AttachmentsTreeName; it is RE-SPELLED here rather than
	// imported because internal/scratch imports internal/tools, which imports
	// this package — the import would be a cycle. The two spellings are held
	// together by internal/scratch's TestAttachmentsArchiveNameMatchesWriters,
	// which reads BOTH sources and compares the literals, so a rename on either
	// side fails there instead of leaving this row pointing at a tree nothing
	// bounds.
	TreeSessionAttachments = "session-attachments"
)

// The families whose identity is a fixed base NAME (never a shape): the
// attachments archive and the metadata record. Exported because the attach and
// metadata writers own the value elsewhere and the gate's fixture must produce
// the SAME name.
const (
	// AttachmentsDirName is the attachments area's leaf name. Its value is the
	// task layer's (internal/tools.taskAttachmentsDirName); it is re-homed
	// here rather than imported because internal/tools is not a leaf, and
	// TestAttachmentsDirNameMatchesWriter pins the two spellings together.
	AttachmentsDirName = "attachments"
	// MetadataFileName is the run metadata record's fixed name (the value of
	// internal/agent's and internal/tools' own constants; pinned by the same
	// test).
	MetadataFileName = "metadata.json"
	// SandboxProfileMarkerName is the profile directory a sandbox
	// materialises. Its value is internal/config.ProfileDirName (".gopi");
	// re-homed here for the same reason as the two above. It is EXPORTED
	// because a second reader needs the same spelling: the declared tree that
	// collects a container's abandoned profile directory (internal/scratch)
	// tests for it by this name, and a re-spelled ".gopi" there would be the
	// drift Rules 13/14 ban — it would also be a second answer to "is this a
	// profile directory?".
	SandboxProfileMarkerName = ".gopi"
	// GitMarkerName is git's own metadata directory — the marker of a
	// checkout, exported for the same reason (the collecting tree's predicate
	// is written from this value, never from a literal).
	GitMarkerName = ".git"
)

// families is THE REGISTRY: every family of a sessions tree, in match order
// (the first family that claims an entry owns it, so the ORDER is part of the
// meaning and the table is read top to bottom).
//
// Each row is one decision somebody made on purpose, with the reason attached.
// A new family in the tree that is not added here is reported as unclassified
// by ClassifyFamily — that is the whole point of the table.
var families = []Family{
	{
		Name:  FamilyForeignCheckout,
		Class: ClassResidue,
		Where: WhereContainer,
		// A whole repository checkout inside a session container: the probe
		// clone a run made to compare branches. Measured members: smoke-clone,
		// master-check, masterchk, tree-master, tree-branch, live, red-master-*.
		Decision: DecisionBound,
		Tree:     TreeSessionLegacyContainers,
		Reason:   "BOUND to the containers row of the session-legacy tree, which collects a `.git`-holding directory one level down inside an AGED, UNHELD session container. `smoke-clone` and its siblings were made by a probe's `git clone` run from a session by hand and no production writer mints one (grep for the names over the repository, measured 2026-10-01) — so nothing regenerates them and nothing else bounds them. THE LIMIT, which a reader must not read past: it collects a checkout whose CONTAINER is already abandoned; a container a run stands in is never a candidate, nor is anything below it",
		Match: func(e Entry2) bool {
			return e.Dir && e.Where == WhereContainer && e.Hidden != nil && e.Hidden(GitMarkerName)
		},
	},
	{
		Name:  FamilySandboxProfile,
		Class: ClassRunWorkDir,
		Where: WhereContainer,
		// A private profile materialised inside a session container
		// (`<sb>/.gopi`, the shape `gopi tool sandbox create` builds). Its reclaim
		// owner is the sandbox reclaimer, not the sessions-tree sweep.
		Decision: DecisionBound,
		Tree:     TreeSessionLegacyContainers,
		Reason:   "BOUND to the containers row, which collects a `.gopi`-holding directory one level down inside an AGED, UNHELD session container. The directory is materialised by a run invoking the sandbox face by hand inside its own container, and the sandbox reclaimer (scratch.SweepSandboxes) owns only the sandboxes ITS OWN resolver reports — these sit inside a session container, which the sandbox root does not contain (measured 2026-10-01: 21 such directories, none under the configured root), so the bound adds no second owner: it covers the case the reclaimer structurally cannot see",
		Match: func(e Entry2) bool {
			return e.Dir && e.Where == WhereContainer && e.Hidden != nil && e.Hidden(SandboxProfileMarkerName)
		},
	},
	{
		Name:  FamilyAttachmentsDir,
		Class: ClassImageArchive,
		Where: WhereContainer,
		// The attachments archive of one session container. The value is the
		// task layer's (internal/tools.taskAttachmentsDirName).
		//
		// THE DECISION CHANGED on 2026-10-02, from `keep:` to `bound:`, when the
		// measurement that authorises a bound arrived: of the 75,851 archive
		// directories in this host's sessions tree, 75,602 are EMPTY and 249
		// hold content. An empty archive holds no evidence and no bytes — it
		// cannot be the last copy of anything — while the non-empty ones are
		// exactly the family the operator's principle protects (measured:
		// 01a0f3de-…/attachments/…/branch-capture-list-windows.bundle, the only
		// copy of an archived branch).
		Decision: DecisionBound,
		Tree:     TreeSessionAttachments,
		Reason:   "the part that provably holds no unique evidence: an ORDINARY, EMPTY, unheld archive directory (internal/scratch/attachments_archive.go — its ONE predicate, with the mount-point write-probe that tells an empty-to-readdir mount root from a genuinely empty directory, and KEEP as the answer to every unknown). Measured 2026-10-02: 75,602 of 75,851 measured archives are empty. NOT bounded: the 249 archives that hold content — an archive with a byte in it is evidence, and it is KEPT whatever its age, because a deleted branch's only surviving copy is an attached bundle and a rule that could delete one is worse than no rule at all",
		Match: func(e Entry2) bool {
			return e.Dir && e.Where == WhereContainer && e.Name == AttachmentsDirName
		},
	},
	{
		Name:  FamilyRawHTTPLog,
		Class: ClassRunArtifact,
		Where: WhereContainer,
		// The per-run raw HTTP log — the ONE family the declared registry
		// bounds today (45.8 GiB / 1655 files measured).
		Decision: DecisionBound,
		Tree:     TreeSessionRaw,
		Reason:   "the whole family: the session-raw tree's namespace predicate IS this shape (EntryKindTreeFileSuffix + SessionRawLogFileSuffix)",
		Match: func(e Entry2) bool {
			return !e.Dir && e.Where == WhereContainer && strings.HasSuffix(e.Shape, ShapeRawHTTPLog)
		},
	},
	{
		Name:  FamilyCrashDump,
		Class: ClassRunArtifact,
		Where: WhereContainer,
		// The crash dump a failed run writes beside its record:
		// `<run-uuid>.dump.json`, or `session.dump.json` for a bare session.
		Decision: DecisionKeep,
		Reason:   "the writer's own record of a failed run (internal/agent.SessionLogger.WriteCrashDump): the last request body plus the error response. Nothing re-derives it, and a death whose dump was aged out is undiagnosable",
		Match: func(e Entry2) bool {
			return !e.Dir && e.Where == WhereContainer && (e.Shape == ShapeCrashDump || e.Shape == ShapeSessionCrashDump)
		},
	},
	{
		Name:  FamilySessionRecord,
		Class: ClassSessionRecord,
		Where: WhereContainer,
		// The session's own record — exactly what this package's layout rules
		// call a record (RecordName, or the per-run `<uuid>.jsonl`).
		Decision: DecisionKeep,
		Reason:   "the ledger's record (internal/agent.NewSessionLoggerForRun / NewSessionLogger): `gopi session resume`, `gopi session log` and the cost ledger all read it, and the raw log's pairing window is measured against its timestamp",
		Match: func(e Entry2) bool {
			return !e.Dir && e.Where == WhereContainer && IsJSONL(e.Shape) && (e.Shape == ShapeRecordName || e.Shape == ShapeRunRecord)
		},
	},
	{
		Name:  FamilySideRecord,
		Class: ClassSessionRecord,
		Where: WhereContainer,
		// A registered side file of a record: signal-provenance.jsonl is the
		// measured member; `sessionlayout.SideFileNames` is the registry of
		// them, so a new side file is a change in ONE place.
		Decision: DecisionKeep,
		Reason:   "a side file of the record beside it (signalrec's provenance record): it is not a session of its own and it dies with the record it annotates",
		Match: func(e Entry2) bool {
			return !e.Dir && e.Where == WhereContainer && IsJSONL(e.Shape) && IsSideFile(e.Name)
		},
	},
	{
		Name:  FamilySessionMetadata,
		Class: ClassSessionRecord,
		Where: WhereContainer,
		// The run's metadata record — one per container (79,401 measured), and
		// read by the session readers that map a task id to its artifacts.
		Decision: DecisionKeep,
		Reason:   "the run's metadata record (internal/agent.writeMetadataJSON merges it with the submit-time skeleton from internal/tools): `gopi task show` and the session readers resolve a container's identity through it",
		Match: func(e Entry2) bool {
			return !e.Dir && e.Where == WhereContainer && e.Name == MetadataFileName
		},
	},
	{
		Name:  FamilySessionScratchDir,
		Class: ClassRunWorkDir,
		Where: WhereContainer,
		// Any other directory inside a container: the run's own working area
		// (its scratch, its evidence tree, a `bin/` it built, a probe dir).
		//
		// THE DECISION MOVED (measured 2026-10-01, then again when this row
		// changed): the family was `keep` while the question being asked was
		// "which NAME is this?" — no name predicate can enumerate 165 run-chosen
		// shapes (evidence, evid, ev, bin, artifacts, crops, figs, __pycache__,
		// smoke-clone, master-check, probeN, acceptN, …). The question that CAN
		// be answered is the CONTAINER's, and it is answered by the
		// `session-raw` row's container arm (internal/scratch/
		// sessions_scratch_dir.go): `containerHasNoLedger(container)` is true
		// only when the container holds no `metadata.json` and no `*.jsonl`
		// record, i.e. the ledger can resolve nothing out of it. A container
		// `gopi task show` still resolves through keeps every directory inside
		// it FOREVER, and a container touched inside the window, one holding a
		// live process, one whose directory carries a `.holder` registration,
		// and every symlink are KEEPs as well.
		Decision: DecisionBound,
		Tree:     TreeSessionRaw,
		Reason:   "the part the session-raw row's container arm collects: the DIRECTORIES inside containers the ledger has finished with (no `metadata.json` AND no `*.jsonl` AND untouched for the arm's own window, `SessionScratchDirStaleAge` = 30d — a floor under the row's 14d file window, not that window). The same tree already bounds `raw-http-log` in full. A container `gopi task show` still resolves through is never touched, at any age; nor is a directory the registry claims for another family (attachments-archive / foreign-checkout / sandbox-profile), a held one, or a symlink. Measured 2026-10-01: 399 of the 430 directories live in ledger-carrying containers and stay KEEP, the 31 in containers with no record at all are the arm's candidates once they reach the window, and the oldest measured directory is 14.8d — so the live sweep is a no-op today, by design",
		Match:    func(e Entry2) bool { return e.Dir && e.Where == WhereContainer },
	},
	{
		Name:  FamilyRunEvidence,
		Class: ClassRunArtifact,
		Where: WhereContainer,
		// Every other FILE inside a container: the report, the worker log, the
		// console log, a probe's script, a screenshot. These are the artifacts
		// a report cites.
		Decision: DecisionKeep,
		Reason:   "run evidence (task-report.md, output.log, console.log, a probe's script/screenshot): a report cites these by name and nothing re-derives them",
		Match:    func(e Entry2) bool { return !e.Dir && e.Where == WhereContainer },
	},
	{
		Name:  FamilySessionContainer,
		Class: ClassSessionRecord,
		Where: WhereRoot,
		// A session container directly under the root.
		Decision: DecisionBound,
		Tree:     TreeSessionRaw,
		Reason:   "only the part the tree's RootStaleAge arm collects: a container EMPTY of raw logs (SessionRawLogDirStaleAge) is removed. Its contents are NOT bounded by that tree — every artifact family below carries its own decision",
		// ASKED BY NAME, through the ONE exported predicate the declared tree
		// asks too (Rules 13/14): the SHAPE cannot answer this question.
		// `ShapeOf` maps a bare 32-hex run onto `<id>` exactly as it maps a
		// canonical uuid, so a shape test called a run's own id-stem spelling a
		// session container — the second directory a run mints beside its own
		// (measured 2026-10-17: 25 of 25 such directories had a canonical twin,
		// one of them written 40 hours later). The name-level predicate refuses
		// it, and FamilyTaskContainer below claims it, so the directory lands on
		// a family instead of in the gap between two.
		Match: func(e Entry2) bool { return e.Dir && e.Where == WhereRoot && IsSessionContainerName(e.Name) },
	},
	{
		Name:  FamilyTaskContainer,
		Class: ClassRunWorkDir,
		Where: WhereRoot,
		// A container named by a task id rather than a session uuid:
		// `t-<epoch>`, a 32-hex row id (pre-session and non-session runs).
		Decision: DecisionBound,
		Tree:     TreeSessionLegacy,
		Reason:   "BOUND to the session-legacy tree, whose root-level directory predicate IS this family's membership test (sessionlayout.IsRootTaskContainerName, referenced from the tree — one function, two readers). What it collects: an AGED, UNHELD `t-`/`process-` container, removed whole. What it does NOT bound: an id-named session container, which the tree's predicate refuses explicitly. No writer in this tree: `t-` and `process-` directories are the PRE-session layout's (a task row's id, or a resident process's pid, named the directory before the session uuid did); the current writers mint `<session-uuid>` (internal/tools.taskSessionDir), which is why 79,310 of 79,656 root directories carry a uuid while 238 carry a `t-` name",
		Match:    func(e Entry2) bool { return e.Dir && e.Where == WhereRoot && IsRootTaskContainerName(e.Name) },
	},
	{
		Name:  FamilyRootStrayDir,
		Class: ClassResidue,
		Where: WhereRoot,
		// A root-level DIRECTORY that is neither an id-named container nor the
		// shape a writer mints: a hand-run probe's directory (`zz-…`,
		// `capdemo-N`, `union-proof`, `pick`) or a harness's evidence dir.
		Decision: DecisionBound,
		Tree:     TreeSessionLegacy,
		Reason:   "BOUND to the session-legacy tree, whose root-level directory predicate IS this family's membership test (sessionlayout.IsStrayRootDirName, referenced from the tree). What it collects: an AGED, UNHELD stray directory, removed whole. What it does NOT bound: any name the shape table does not carry — a NEW root-level directory stays unclassified and is REPORTED, never swept. No writer in this tree: the current writers mint `<session-uuid>/` (internal/tools.taskSessionDir) and `t-<epoch>/`; the measured members (capdemo-N, zz-cjk-test, zz-live-probe-<id>, union-proof, pick, evidence, merger-restart-evidence, gcsweep-keep-N, drain-claim-control-N) were made by probes and harnesses run by hand",
		Match:    isStrayRootDirShape,
	},
	{
		Name:  FamilySessionsRootInboxDir,
		Class: ClassResidue,
		Where: WhereRoot,
		// A `messages/` DIRECTORY directly under the sessions root: the inbox
		// name of a session, at the depth of the TREE instead of the depth of a
		// session.
		//
		// THE NAME IS NOT RESIDUE — the DEPTH IS. `SessionMessagesDirName` is
		// the durable inbound inbox ONE LEVEL DOWN inside a session container
		// (`internal/tools.sessionInboxPath` joins a SESSION DIR, and the
		// reader is `internal/agent.sessionInboxDir`, which takes
		// `RecordContainer(this run's log)`), so every container-level
		// `messages/` is the `attachments-archive`-style family's sibling and
		// is NOT this row's subject. This row claims only the copy that a
		// session can never read, because its own resolver would have to be the
		// sessions ROOT to produce it.
		//
		// MEASURED (this host, 2026-11-14). The live tree holds 388 `messages/`
		// directories one level down and EXACTLY ONE at the root, holding
		// `inbox.jsonl` — 4 records, 5079 bytes, all `"session_id":
		// "d1495e9e-c14b-4ab0-9b54-549e9000e623"`, written 2026-10-13 between
		// 02:13Z and 04:39Z.
		//
		// WHO WROTE IT — measured, and the answer is NOBODY ANY MORE. The only
		// way a production path can reach this depth is a `dir` operand equal to
		// the sessions root itself, and both resolvers refuse that by
		// construction:
		//   * the local DELIVER face (`cmd/agent.resolveNotifySessionDir`) takes
		//     `sessionlayout.RecordContainer(path)` — `filepath.Dir` of the
		//     record — and the record it finds is at least one level down;
		//   * the peer handler (`internal/tools.notifyPeerSessionDir`) does the
		//     same and additionally refuses a bare-root candidate with
		//     `DirHasRecord`.
		// The ONE path that used to produce the root was the SECOND `Dir()` that
		// peeled a level whenever the record's basename was not
		// `session.jsonl` — the defect fixed on the day this inbox was written
		// (see the comments in `cli_notify.go` and `notify_peer.go`, and
		// `TestNotifyPeerCLITaskRunRecordKeepsItsContainer`, whose second side
		// asserts this very directory does NOT come back). The 4 records are
		// that run's own repro probes, the last of them a "post-fix probe".
		//   * `gopilog.FindSession` CAN still return the root as `dir` when a
		//     LEGACY FLAT record (`<root>/<uuid>.jsonl`) is what matched — every
		//     container candidate is a directory, so a root-level file can only
		//     match through the flat-name spelling. That path is not reachable
		//     on this host today: measured, the sessions root holds NO `*.jsonl`
		//     at all (44 of the records the census counts there are
		//     `<id>.screen.stream` / `<id>.sgr.stream` / `<id>.dump.json`
		//     shapes), and the current writers mint `<session-uuid>/`. It stays a
		//     NAMED possibility here rather than a silent one, which is exactly
		//     what a `legacy` decision is for.
		//
		// THE DECISION IS `legacy`, NOT `bound:` — deliberately. Bounding it
		// would add a sweep arm over an inbox, and an inbox is the one artifact
		// whose whole value is that a message it holds has NOT been read yet: it
		// cannot be re-derived, and "aged and unheld" is precisely the state of
		// a message nobody has collected. So this row makes the shape VISIBLE
		// (the census prints it under its own family instead of UNCLASSIFIED)
		// and leaves the disposition to a person.
		Decision: DecisionLegacy,
		Reason:   "a session's inbox name at the TREE's depth instead of a session's: `messages/` directly under the sessions root, which no session can read (every resolver joins `SessionMessagesDirName` onto a session CONTAINER, one level down). Measured this host 2026-11-14: 1 such directory against 388 at the correct depth, holding `inbox.jsonl` — 4 records, 5079 bytes, all addressed to session d1495e9e-c14b-4ab0-9b54-549e9000e623 and written 2026-10-13 02:13Z–04:39Z, the day the one path that could reach this depth (a second `filepath.Dir()` peel in the two notify resolvers) was fixed. No writer in this tree: both resolvers now take `sessionlayout.RecordContainer`, and this host's sessions root holds no `*.jsonl` for a legacy-flat match to be made through. KEPT rather than bounded because an inbox's value IS its unread records — nothing re-derives one, and an age window would be deleting messages nobody has collected yet",
		Match: func(e Entry2) bool {
			return e.Dir && e.Where == WhereRoot && e.Name == SessionMessagesDirName
		},
	},
	{
		Name:  FamilySessionsRootResidue,
		Class: ClassResidue,
		Where: WhereRoot,
		// A file directly under the root that no other family claims: a stray
		// dump of a session whose container is gone, an operator's log, a
		// migration manifest.
		Decision: DecisionBound,
		Tree:     TreeSessionLegacy,
		Reason:   "BOUND to the session-legacy tree, whose root-level FILE predicate IS this family's membership test (sessionlayout.IsRootResidueFileName, referenced from the tree). What it collects: an AGED, UNHELD residue file (measured: 482 orphaned `<uuid>.dump.json` dumps, 4 `run-full*.log`). What it does NOT bound: a file whose shape the exact table does not carry. No writer in this tree: measured members are a hand-run probe's logs (run-full*.log, c4-trace.txt), a migration manifest (.migration-manifest.json) and orphaned `<uuid>.dump.json` dumps whose container is gone; the current writers place every artifact INSIDE a container, so nothing here is produced by the code in this tree. The match is deliberately EXACT shapes rather than a catch-all: a catch-all would make every new root-level FILE invisible to the gate, and an undiscovered family is exactly what this registry exists to report",
		Match:    isRootResidueFileShape,
	},
}

// The measured RESIDUE shapes of a sessions ROOT directory: a hand-run probe's
// directory, a harness's evidence tree. They are spelled as a small table
// rather than as a catch-all rule, because a catch-all is exactly what the
// registry must not do — a NEW root-level directory shape has to be classified
// on purpose, and the gate fails red when it is not (measured 2026-10-01: these
// rules covered every root-level directory in a 79,656-directory tree).
const (
	// strayDirEvidenceSuffix is the suffix of a harness's evidence directory
	// (`merger-restart-evidence`, `task-NaNeaca-evidence`).
	strayDirEvidenceSuffix = "-evidence"
)

var (
	// strayDirPrefixes are the measured name prefixes of a probe's directory.
	strayDirPrefixes = []string{"zz-", "capdemo-", "drain-claim-control-", "gcsweep-keep-"}
	// strayDirExactNames are the measured single-member residue names.
	strayDirExactNames = []string{"union-proof", "pick", "evidence"}
)

// isStrayRootDirShape reports whether a root-level directory's SHAPE is one of
// the measured residue shapes. Anything else at the root is UNCLASSIFIED — the
// report a reader must act on (Rule 15), never a silent catch-all.
func isStrayRootDirShape(e Entry2) bool {
	if !e.Dir || e.Where != WhereRoot {
		return false
	}
	if strings.HasSuffix(e.Shape, strayDirEvidenceSuffix) {
		return true
	}
	for _, p := range strayDirPrefixes {
		if strings.HasPrefix(e.Shape, p) {
			return true
		}
	}
	for _, n := range strayDirExactNames {
		if e.Shape == n {
			return true
		}
	}
	return false
}

// The measured ROOT-FILE residue shapes: the shape of a crashed run's dump
// whose container is gone (`<uuid>.dump.json`), the three log families a probe
// wrote by hand, the numbered run logs and the two operator text files.
//
// EXACT SHAPES, never a catch-all (see the family's own reason): the whole
// point of the registry is that a family nobody has decided about is REPORTED,
// and a catch-all would classify it silently.
var (
	// rootResidueFileSuffixes are the measured residue file suffixes.
	rootResidueFileSuffixes = []string{ShapeCrashDump, ".log", ".txt", rootResidueSeqSuffix}
	// rootResidueFileExact are the measured residue file names.
	rootResidueFileExact = []string{".migration-manifest.json", "README-placeholder"}
	// rootResidueFilePrefixes are the measured residue file prefixes (`run-full3.log`).
	rootResidueFilePrefixes = []string{"run-", "c4-", "c9-", "_acc"}
	// rootResidueSeqSuffix is the suffix of a sequence file a probe dumped into
	// the root (`<row-id>-seq.json`, measured once).
	rootResidueSeqSuffix = "-seq.json"
)

// isRootResidueFileShape reports whether a root-level FILE's shape is one of the
// measured residue shapes.
func isRootResidueFileShape(e Entry2) bool {
	if e.Dir || e.Where != WhereRoot {
		return false
	}
	for _, exact := range rootResidueFileExact {
		if e.Name == exact {
			return true
		}
	}
	for _, suf := range rootResidueFileSuffixes {
		if strings.HasSuffix(e.Shape, suf) {
			return true
		}
	}
	for _, pre := range rootResidueFilePrefixes {
		if strings.HasPrefix(e.Shape, pre) {
			return true
		}
	}
	return false
}

// isSessionContainerShape reports whether a root-level directory's shape is an
// id-named container: the shape STARTS with the identifier token. It is a
// PREFIX rule rather than an equality with the bare token because the tree
// holds mangled ids too (a 32-hex row id truncated mid-group shaped `<id>N`,
// measured: two of them), and a container whose name begins with an identifier
// run is the ledger's own directory either way. The rule is deliberately
// narrow: a name that does not begin with an id is NOT a container (that is
// what the stray-dir family below is for).
func isSessionContainerShape(shape string) bool {
	return strings.HasPrefix(shape, shapeIDToken)
}

// IsSessionContainerName answers the same question for a raw directory NAME:
// the container test a scanner applies before descending into a root-level
// directory. It goes through ShapeOf, so the name rule and the shape rule are
// ONE rule (a second spelling of "starts with an id" is exactly the drift
// Rules 13/14 ban).
// It REFUSES A NON-CANONICAL GROUPING FIRST, and that refusal is the WHOLE of
// this function's recent change. `ShapeOf` MAPS EVERY identifier run onto
// `<id>…` and drops the dashes with it, so a name that is a uuid's own 32 hex
// characters in a grouping this tree does not use shapes as a container — the
// container test above cannot tell it from the row's real directory, because
// the one thing that distinguishes them is exactly what the shape erased. That
// blindness is not a classifier's cosmetic defect here: this predicate IS the
// namespace rule of a row that REMOVES root-level directories
// (internal/scratch's session-legacy rows), and the near-miss it could not see
// is a name a run mints for itself by mis-transcribing its own id.
//
// MEASURED (2026-10-14, host nick-HP-Laptop, this package's sibling gate). A
// run whose row id is `01a13645-66c3-79ec-962a-5f8776e1f216` typed the four-dash
// form `01a13645-66c3-79ec-962a5f8776e1f216` — the tail group's leading dash
// moved one group left, i.e. the branch/worktree spelling a stem that strips
// non-alphanumerics produces. `mkdir -p <that>/artifacts` created an EMPTY
// container beside the row's own, and its `artifacts/` was removed by
// `gopi tool scratch-gc trees` after the container aged past the window — the
// directory the run had just written its evidence into, taken whole, silently
// (the row's own container was kept: it carries `metadata.json`, so the arm's
// unheldRegistered veto has something to read). The same predicate therefore
// lost an evidence directory while protecting its twin — the shape-erasure
// described above, one level up.
//
// WHAT IS REFUSED, EXACTLY: a name that is a uuid's 16 bytes in SOME DASHED
// group size — 32 hexadecimal characters plus at least one separator, spelled
// in a grouping that is not the canonical one `uuidShapeRE` already holds.
// That is the same set the uuid-shape gate refuses at the session-dir seams
// (internal/sessionid.IsAdmissible), asked here in this package's OWN
// vocabulary (the canonical pattern is already spelled here, once — Rules
// 13/14), so the classifier does not fall behind the gate the writers answer to.
//
// WHAT IS STILL ADMITTED, deliberately: a name carrying NO separator at all (a
// dashless identifier run — the branch/worktree spelling itself). That case is
// this tree's own documented decision (a container whose name begins with an
// identifier run is the ledger's own directory either way) and it is refused
// where it is minted, at the session-dir seams. A name that is not a uuid
// spelling at all (a plain name, a legacy `t-…`, an auxiliary-channel id) is
// untouched, because over-blocking a legitimate operand turns a visible failure
// into a silent one.
func IsSessionContainerName(name string) bool {
	if isDashlessUUIDName(name) {
		// NO SEPARATOR AT ALL: the second shape this predicate cannot see, and
		// the one this change refuses. It is the same blindness as the grouping
		// case above, one step further: `ShapeOf` maps a bare 32-hex run onto
		// `<id>` exactly as it maps a canonical uuid, so the container test
		// above cannot tell a run's OWN stem spelling from the row's directory —
		// and this predicate is the namespace rule of a row that removes
		// root-level directories.
		//
		// MEASURED (2026-10-17, host the host, task
		// 01a144001e4b7086b97d257497a832ed). The sessions root carried 25
		// root-level directories named as a bare 32-hex run. ALL 25 had a
		// canonical twin beside them (25 of 25), and one twin was WRITTEN 40
		// HOURS AFTER its dash-less copy — so the dash-less name is not "the
		// same container, differently spelled": it is a SECOND directory the run
		// minted for itself out of its own id (the branch/worktree stem, the
		// spelling `taskGroundIDStem` produces by stripping every non-alphanumeric
		// character). None of the 25 held a `metadata.json` or a `*.jsonl`; they
		// are the wrong-side twins of containers the ledger really describes.
		//
		// WHAT IS REFUSED, EXACTLY: a name that is exactly a uuid's 32
		// hexadecimal characters with no separator. The length test is what keeps
		// this from over-blocking: the ids this tree really carries that are NOT
		// id-shaped containers (`01a11548-tracked`, `0000-emptydir`, `01a0a392`)
		// are not 32 hex characters, so they are untouched — and the canonical
		// container itself is 36 characters and is answered below. See
		// isDashlessUUIDName for the whole set.
		return false
	}
	if isNonCanonicalGroupingName(name) {
		return false
	}
	return isSessionContainerShape(ShapeOf(name))
}

// isDashlessUUIDName reports whether name is a uuid's 16 bytes written with NO
// separator at all — the branch/worktree stem spelling a run types when it
// mis-transcribes its own row id (`01a1364566c379ec962a5f8776e1f216` for
// `01a13645-66c3-79ec-962a-5f8776e1f216`).
//
// It is deliberately the NARROWEST possible test, and the narrowness is the
// safety property: exactly 32 lower-case hexadecimal characters, nothing else.
// A name one character shorter or longer, one carrying any other byte, or one
// in ANY dash grouping is not this function's answer, so every id the tree
// really uses reaches the grouping test below (or the container test after it)
// with its verdict unchanged.
//
// This is NOT a second answer to "what does a canonical uuid look like": the
// canonical pattern is uuidShapeRE (Rules 13/14), and a canonical spelling is 36
// characters, so it can never satisfy the length test here. The two questions
// are disjoint by construction: "is this the grouping we use" and "is this the
// stem spelling that has no grouping at all".
func isDashlessUUIDName(name string) bool {
	if len(name) != uuidHexLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// uuidHexLen is the number of hexadecimal characters a uuid's 16 bytes are
// written in — the arithmetic half of the non-canonical-grouping test below.
// It is a named constant because the comparison it feeds is a rule, not an
// incidental bound (Rule 1).
const uuidHexLen = 32

// isNonCanonicalGroupingName reports whether name is a uuid's 16 bytes written
// in a DASH GROUPING this tree does not use — the near-miss a run produces when
// it mis-transcribes its own row id, and the ONE defect the container test
// above cannot see (ShapeOf erases the grouping).
//
// The canonical spelling is decided by uuidShapeRE — the pattern this package
// ALREADY holds for it — so this is not a second spelling of "what a canonical
// uuid looks like" (Rules 13/14): the question asked here is only ever "the
// bytes are a uuid's, and the grouping is the OTHER one".
//
// It reads the RAW NAME, never the shape, and that is the point: the shape is
// the coarser question asked after the grouping has already been thrown away.
func isNonCanonicalGroupingName(name string) bool {
	if uuidShapeRE.MatchString(name) {
		return false // the canonical grouping: a real container name
	}
	hex, sep := 0, false
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
			hex++
		case c == '-', c == '_', c == '.':
			sep = true
		default:
			return false // not a uuid spelling at all: a plain name, a legacy id
		}
	}
	return sep && hex == uuidHexLen
}

// The root-level container NAME prefixes that mark a task-named rather than a
// session-named directory: `t-` (the pre-session task container, minted as
// `t-<epoch>` and later variants `t-<epoch>-<n>`, `t-worker-stale`, …) and
// `process-` (a resident process's own directory).
//
// The test is a PREFIX on the SHAPE, not an equality with one spelling: the
// measured tree holds eleven different `t-…` shapes (t-N, t-N-N, t-worker-stale,
// t-live-decoy, t-eN-hard, t-zd-confirmed, t-de-resume, …), and a rule that
// enumerated them would go red the next time a probe named one differently —
// while the FAMILY question ("is this directory named after a task row rather
// than a session?") has one answer for all of them.
var rootTaskContainerPrefixes = []string{"t-", "process-"}

// isRootTaskContainer reports whether a root-level container's shape is one the
// task-container family owns.
func isRootTaskContainer(shape string) bool {
	for _, pre := range rootTaskContainerPrefixes {
		if strings.HasPrefix(shape, pre) {
			return true
		}
	}
	return false
}

// Families returns the registry (a copy: a caller must not be able to add a
// family at runtime, because the gate's verdict has to be reproducible from
// the source).
func Families() []Family {
	out := make([]Family, len(families))
	copy(out, families)
	return out
}

// FamilyByName returns the registered family with that name.
func FamilyByName(name string) (Family, bool) {
	for _, f := range families {
		if f.Name == name {
			return f, true
		}
	}
	return Family{}, false
}

// ClassifyFamily classifies ONE entry of a sessions tree. ok is false when no
// registered family claims it — the state a caller must REPORT, never ignore
// (Rule 15): an unclassified entry means the tree grew a family the registry
// does not know, which is exactly the defect this file exists to catch.
func ClassifyFamily(e Entry2) (Family, bool) { return classifyWith(families, e) }

// classifyWith is the ONE classifier body: the registry's own order walked
// against a given list. ClassifyFamily passes the real registry; the gate's
// red-proof passes a reduced one, so "the gate is red when a family is dropped"
// is proved by the SAME code the green run uses.
func classifyWith(fams []Family, e Entry2) (Family, bool) {
	for _, f := range fams {
		if f.Match(e) {
			return f, true
		}
	}
	return Family{}, false
}

// Unclassified is one entry no family claimed, with the shape a reader needs
// to register it (the shape, not the raw name: one report line must stand for
// the whole family).
type Unclassified struct {
	Path  string
	Name  string
	Shape string
	Dir   bool
	Where Where
}

// UnclassifiedIn returns every entry of the given slice that no family claims,
// in the order given. It is the ONE place the "unclassified" question is
// answered, so the gate, the report line and the red-proof cannot disagree.
func UnclassifiedIn(entries []Entry2) []Unclassified {
	var out []Unclassified
	for _, e := range entries {
		if _, ok := ClassifyFamily(e); ok {
			continue
		}
		out = append(out, Unclassified{Path: e.Path, Name: e.Name, Shape: e.Shape, Dir: e.Dir, Where: e.Where})
	}
	return out
}

// BoundTreeNames returns the names of the declared trees the registry marks
// `bound:` — the gate's cross-check input ("declared but not swept" cannot
// pass, and a `bound:` row naming a tree that does not exist is refused).
func BoundTreeNames() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range families {
		if f.Decision != DecisionBound || f.Tree == "" || seen[f.Tree] {
			continue
		}
		seen[f.Tree] = true
		out = append(out, f.Tree)
	}
	return out
}

// FamilyEntryFor builds the Entry handed to a Match for ONE filesystem entry:
// the shape is computed here (one definition of "the shape of this entry"),
// and the Hidden probe is attached only for a directory.
func FamilyEntryFor(path string, dir bool, where Where) Entry2 {
	name := filepath.Base(path)
	e := Entry2{Path: path, Name: name, Shape: ShapeOf(name), Dir: dir, Where: where}
	if dir {
		e.Hidden = func(child string) bool {
			_, err := os.Stat(filepath.Join(path, child))
			return err == nil
		}
	}
	return e
}

// The EXPORTED name-shape predicates. They are the ONE definition of "which
// root-level directory/file is legacy residue", shared by the registry's Match
// functions below and by the declared tree that collects them
// (internal/scratch, which imports this package): the sweep and the registry
// must reach the same verdict about the same name, so the value is spelled
// here and referenced there (Rules 13/14 — a second predicate would be a
// second answer, and the disagreement would delete a file the registry calls
// somebody else's).
//
// They take a NAME (not an Entry) because that is the shape a declared tree's
// namespace predicate is asked with (`EntryMatch func(name string) bool`).

// IsRootTaskContainerName reports whether a root-level directory named name is a
// TASK-NAMED container: the pre-session layout's `t-<epoch>` variant (and the
// `process-<pid>` directory a resident process minted for itself). It is the
// registry's task-container membership test, reached from the tasks-tree
// predicate.
func IsRootTaskContainerName(name string) bool {
	if IsSessionContainerName(name) {
		return false // an id-named container is the container family's, never this one's
	}
	if isNonCanonicalUUIDSpellingName(name) {
		// THE SPELLINGS THIS TREE DOES NOT USE: an id-named container the
		// CONTAINER test just refused (a non-canonical dash grouping, or the
		// dash-less branch/worktree stem). Refusing them in
		// IsSessionContainerName without claiming them here would leave root-level
		// directories that are containers by every other measure and belong to NO
		// family — the hole this pair closes. They are claimed by the family whose
		// writers minted them: both spellings are a run's own id
		// (`taskGroundIDStem` strips every non-alphanumeric character; the grouping
		// variant types the dash one group over), which is exactly this family's
		// "named after a task row rather than a session" test — asked here on the
		// NAME, because the shape test below cannot see either one (`ShapeOf` maps
		// both onto `<id>`, same as a canonical uuid).
		return true
	}
	return isRootTaskContainer(ShapeOf(name))
}

// isNonCanonicalUUIDSpellingName reports whether name is a uuid's 16 bytes
// written in a spelling THIS TREE DOES NOT USE — the union of the two shapes the
// canonical pattern cannot see:
//
//   - a DASH GROUPING that is not the canonical 8/13/18/23 (the tail group's
//     leading dash moved one group left), and
//   - NO SEPARATOR AT ALL (the branch/worktree stem).
//
// It is the union rather than either half because the two are ONE fact about a
// name — "these are a uuid's bytes, in a spelling the tree refuses" — and the
// refusal has ONE reason (internal/sessionid.Refuse states it: joining such an
// id mints a SECOND session directory beside the row's own). Splitting it into
// two predicates would let the two halves drift into two different families, and
// the second directory a run mints would land in whichever half was forgotten.
//
// It is NOT a second spelling of the canonical pattern (Rules 13/14): both halves
// here are spelled from the ONE rule this package already holds — the dash
// grouping is asked through isNonCanonicalGroupingName (which starts by
// consulting uuidShapeRE), and the dash-less half is the length + hex test of
// isDashlessUUIDName. Neither is a copy of the canonical regex, and a canonical
// spelling (36 characters, uuidShapeRE) can satisfy neither.
//
// The session-dir seams ask the same question in internal/sessionid
// (IsAdmissible -> isNonCanonicalUUIDSpelling) and REFUSE the id there. This
// package does not import it: the two answer about different things (that one
// about a ROW ID a caller hands to a writer, this one about a DIRECTORY NAME a
// walk finds on disk), and a root-level directory whose row id was refused is
// exactly the residue that still has to be classified.
func isNonCanonicalUUIDSpellingName(name string) bool {
	return isDashlessUUIDName(name) || isNonCanonicalGroupingName(name)
}

// IsStrayRootDirName reports whether a root-level directory named name carries
// one of the measured RESIDUE shapes — a hand-run probe's or harness's
// directory, never a container a writer mints.
func IsStrayRootDirName(name string) bool {
	return isStrayRootDirShape(FamilyEntryFor(name, true, WhereRoot))
}

// IsRootResidueFileName reports whether a root-level FILE named name carries one
// of the measured residue shapes — a hand-run probe's log, a migration manifest,
// the numbered run logs. The third member of the exported trio the collecting
// tree references instead of re-spelling the suffix table.
func IsRootResidueFileName(name string) bool {
	return isRootResidueFileShape(FamilyEntryFor(name, false, WhereRoot))
}
