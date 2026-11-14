package main

// cli_scratch_gc.go — the `gopi tool scratch-gc` verb: the scratch home's ONE-SHOT,
// synchronous sweep, for a host-level watchdog (systemd --user timer / cron).
//
// WHY A CLI FACE AND NOT A NEW DAEMON (Rule 3): the tick it exposes is a PASS
// (scratch_gc_tick.go), and the only thing that can fire it on a host with
// nothing resident is the OS scheduler — a build box, a CI runner, a
// workstation between suites. CORRECTED 2026-10-30 (task 01a18ef7): this
// paragraph used to say "the resident sweep already rides the worker/merger
// tick, which bounds the tree on a host that keeps a resident gopi alive". No
// resident process calls that tick any more (worker round 187d05c; merger tick
// deleted 2026-09-19), so this verb is not the FALLBACK for a daemon-less host
// — it is the door, and its `--tick` arm is the one-shot face of the tick.
// A timer needs something to EXECUTE, and the executing thing must be an
// idempotent, self-terminating one-shot: not a wire tool (the LLM tool
// surface is frozen — Rule 3), and not a resident process (the resident half
// already exists). A CLI verb is the only shape that fits, and the epic's
// hard constraint explicitly allows one under cmd/agent.
//
// It is a THIN face: the sweep itself is internal/scratch.SweepHome — the
// same function scratchGCTick calls and the same hard rule applies
// (a directory held by a live process is never deleted: the /proc cwd+fd scan
// plus the .holder registration inside the sweep). No sweep logic is
// duplicated here, so a fix to the rule lands in one place.
//
// Exit status is the watchdog's signal, so it distinguishes three outcomes:
//
//	nothing to do / swept N   → 0 (a timer must not raise an alarm for the
//	                               normal case, including "home absent")
//	sweep could not run       → non-zero, with the reason on stderr
//
// The disabled-by-operator case (GOPI_TEST_SCRATCH_GC_MIN=0 / settings
// scratch_gc=false) still honours the shared knob — but note the ONE READER
// rule (Rule 2): the knob's environment reader lives in scratch_gc_tick.go
// (scratchGCInterval), and this verb calls THAT function rather than reading
// the env a second time.

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/shot"
)

// scratchGCLogFmt is the sweep's one-line report. It is a named constant
// because the watchdog's log is the epic's PRIMARY ACCEPTANCE EVIDENCE: an
// operator (and the acceptance check) greps for the scratch home and the
// removed count in exactly this shape. `logfmt` key=value spelling so the
// line is both readable and machine-parseable.
//
// Fields: verb, role (which face swept), home (the tree swept), removed
// (entries deleted), stale (the age threshold applied), disabled (true when
// the operator's off switch skipped the sweep).
const scratchGCLogFmt = "scratch-gc role=%s home=%s removed=%d stale=%s disabled=%t"

// scratchGCLockLogFmt is the LOCK EVIDENCE line, printed ABOVE the count line
// whenever this pass did not simply acquire a free lock — deferred to a live
// owner, broke an abandoned one, or could not measure the owner at all. The
// evidence text comes from internal/scratch's ONE renderer
// (scratch.GCLockEvidence), so the wording cannot drift from the decision.
//
// It is a line of its own rather than extra fields on the count line because the
// decision it narrates is the one that decides whether the count line means
// anything (a `removed=0` under `deferred-live-owner` is not "nothing to
// collect": it is "this pass did not sweep"). Rule 15 — the failure was
// decidable and was being swallowed.
const scratchGCLockLogFmt = "scratch-gc role=%s home=%s %s"

// scratchGCLockLine appends the lock verdict's FIELDS to an arm's count line, so
// the number is always stated together with the decision that produced it.
func scratchGCLockLine(line string, v scratch.GCLockVerdict) string {
	return line + scratch.GCLockFields(v)
}

// reportScratchGCLock prints the lock evidence line when there is something to
// say — see scratchGCLockLogFmt. A plain acquire prints nothing extra: the count
// line's own `lock=acquired owner=none` already says it, and a line added to
// every ordinary pass would be noise that hides the exceptional ones.
func reportScratchGCLock(role, home string, v scratch.GCLockVerdict) {
	if !scratch.GCLockNeedsReporting(v) {
		return
	}
	fmt.Printf(scratchGCLockLogFmt+"\n", role, home, scratch.GCLockEvidence(v))
}

// scratchGCBaseLogFmt is the memory-BASE sweep's report — the same shape with
// the base named by its own key (the two arms are distinguished by what
// follows `gopi tool scratch-gc`). ONE format per arm, so a reader never has to
// guess which tree a line refers to.
const scratchGCBaseLogFmt = "scratch-gc role=%s base=%s removed=%d stale=%s disabled=%t"

// scratchGCShotUnitsLogFmt is the CAPTURE-UNIT arm's OWN count line: how many
// orphaned transient capture units this invocation renewed.
//
// IT IS A SEPARATE LINE ON PURPOSE (epic shot-reclaim-trigger, acceptance 4).
// The scratch line above it counts REMOVED DIRECTORY ENTRIES — a different arm,
// a different unit of work — and the measured defect was exactly this
// conflation: the verb stopped six `gopi-shot-*` units and printed `removed=4`,
// so the capture reclamation was invisible in its own output. A count that
// cannot be seen cannot be checked, and an operator had no way to tell the
// defence had run.
//
// It prints on the ZERO path too: "the capture arm ran and renewed nothing" is
// the fact that distinguishes a wired-but-quiet defence from one that is not
// wired at all (Rule 15 — an unmeasured fact must never render as a silent one).
const scratchGCShotUnitsLogFmt = "scratch-gc role=%s shot-units-renewed=%d"

// scratchGCDryRunFlag is the vetting flag (Rule 1b: a flag spelling is a
// value with meaning — named once, referenced by the parser, the usage text
// and the tests).
const scratchGCDryRunFlag = "--dry-run"

// scratchGCExplainFlag is the PER-CANDIDATE VERDICT face: with it the sweep
// reports, for every candidate it looked at, whether it may be collected and the
// sentence that decided it — the owner record it judged (pid + kernel start ticks)
// and the outcome (gone / recycled / live / unmeasurable / unregistered).
//
// WHY THIS EXISTS: a removal count alone cannot distinguish "the tree is clean"
// from "the predicate refused everything" — the measured failure that motivated it
// (2026-09-18: removed=0 while 137 entries sat in the home, 172 of 175 candidates
// vetoed or window-bounded). With the reasons on the line, a zero becomes a
// readable statement about WHY, and a non-zero names the writer's root that went.
//
// IT IS A JUDGEMENT, NOT A PASS — and that had to be made TRUE rather than
// documented (2026-11-01, task 01a1957a). The flag used to be purely additive:
// `--explain` fell straight through into the live sweep, so a reader who typed
// exactly the form the help page demonstrated as "the VETTING face" deleted the
// very entries the verdicts were describing. Measured on the reporting row: the
// operator's own `scratch-gc --explain` removed
// `/home/user/workspace/_tmp/go-build3659052550` (11,270,405 bytes) while printing
// `verdict=collectable` for it (devdoc/tmpdir-lifecycle-forensics.md §5). The
// verdict report's own comment said "it deletes nothing", which was true of the
// REPORT and false of the COMMAND — the exact ambiguity this flag now closes.
//
// THE RULE (enforced by scratchGCExplainImpliesDryRun, stated in the help page):
// `--explain` IMPLIES `--dry-run`. The ONE exception is the arm's own explicit
// consent word in the same invocation — `--yes` on profile/stubs/roots — because
// spelling THAT word asks for the deletion, and turning a deliberate deletion into
// a report would be the same class of lie in the other direction. So:
//
//	gopi tool scratch-gc --explain                 → judgement only (deletes nothing)
//	gopi tool scratch-gc --explain --dry-run       → the same, spelled out
//	gopi tool scratch-gc profile --explain --yes   → report, THEN the confirmed reclaim
//	gopi tool scratch-gc                           → the sweep (unchanged, no flag needed)
const scratchGCExplainFlag = "--explain"

// scratchGCConsentFlagsByArm is the "I mean the deletion" vocabulary, PER ARM.
//
// WHY NOT VERB-WIDE (Rule 1b/13, and the reason this map is keyed): an arm has one
// word that says "go ahead", and `--yes` on the HOME arm is an argument that arm
// does not read. Treating it as consent there would let a stray word turn a
// judgement into a sweep — the defect this whole change removes, reintroduced by
// the fix. So an arm absent from this map can never answer yes, which is exactly
// the promise the help page makes for the faces whose bare form sweeps (home,
// base, trees, sandboxes, tick). The gocache arm is absent for the same reason at
// a different level: it deletes only with `--trim`, which is its own consent word
// and its own deletion gate, so `--explain` on that arm changes nothing at all.
//
// The spellings are the existing constants, never a second literal (Rule 13): a
// second spelling would be a second definition of the word "delete".
var scratchGCConsentFlagsByArm = map[string][]string{
	scratchGCProfileRole: {scratchGCStubsYesFlag},
	scratchGCStubsRole:   {scratchGCStubsYesFlag},
	scratchGCRootsRole:   {scratchGCStubsYesFlag},
}

// scratchGCConsentGiven reports whether args spell the consent word of the arm
// that is about to run.
func scratchGCConsentGiven(arm string, args []string) bool {
	for _, word := range scratchGCConsentFlagsByArm[arm] {
		for _, a := range args {
			if a == word {
				return true
			}
		}
	}
	return false
}

// scratchGCExplainImpliesDryRun is the ONE place the rule above is decided:
// `--explain` without this arm's consent word is a REPORT, so the pass runs on the
// dry-run face.
//
// WHY AT THE DISPATCH AND NOT INSIDE EACH ARM (Rule 13/14): six faces delete on their
// own default (home, base, trees, sandboxes, tick, and the three --yes arms), and a
// rule re-stated in nine places is a rule that will disagree with itself in one of
// them. Normalizing once, before any arm reads the flag, means every arm inherits
// the same promise — including an arm added later, which is the property the help
// page now claims.
func scratchGCExplainImpliesDryRun(arm string, explain bool, args []string) bool {
	if !explain {
		return false
	}
	return !scratchGCConsentGiven(arm, args)
}

// scratchGCBaseFlag is the MEMORY-BASE arm selector: `scratch-gc base` sweeps
// the memory base (the ad-hoc residue the home sweep cannot see). Spelled here
// ONCE (Rule 1b/13) and it HAS the three readers this file's arms need: the
// parser's arm switch (runScratchGCSubcommand), the usage text
// (scratchGCUsage, "the HOME arm and the `base` arm"), and the resident tick's
// reclaim hint (scratch_gc_tick.go, "reclaim with: gopi scratch-gc base").
//
// RETIRED / NEVER WIRED — there used to be a second selector here,
// scratchGCX11Flag = "x11", described as reaping this system's own stale
// X11-connection temp dirs. It NEVER had a reader: no case in the arm switch
// above, no line in the usage text, and no sweeper in internal/scratch (that
// package holds no X11 code at all), so `scratch-gc x11` could only ever fail
// as an unknown argument. The constant was removed by task 01a18f80 (the defect
// it was: a comment asserting three readers for a constant with none, beside an
// unreachable arm constant that implied a capability existed). This paragraph is
// kept ON PURPOSE — it is the only remaining record that X11-connection cleanup
// was ever intended here. Reviving it is a FEATURE, not a rename, and it needs
// all four of the following wired in the same change:
//
//  1. cmd/agent/cli_scratch_gc.go — a `case scratchGCX11Flag:` arm in the
//     subcommand's argument switch, assigning its own role label;
//  2. cmd/agent/cli_scratch_gc.go — a `gopi tool scratch-gc x11` entry in
//     scratchGCUsage (the help text is the promise, CONFIG.md's rule);
//  3. internal/scratch — the actual SWEEPER (a plan/verdict/sweep triple beside
//     SweepBase, with its own age constant and holder scan), which does not
//     exist; without it the arm has nothing to call;
//  4. cmd/agent/scratch_gc_tick.go — the resident tick's arm, if the pass is
//     meant to run unattended rather than on request.
//
// The regression guard `TestScratchGCArmConstantsHaveReaders` (same package)
// fails the build if an arm-named `scratchGC…Flag` constant comes back without a
// reader outside its own definition.
const scratchGCBaseFlag = "base"

// scratchGCTreesFlag is the DECLARED-TREES arm selector: `scratch-gc trees`// sweeps the trees this repository's own commands create and never clean — the
// GOTMPDIR tree's leftover `go-build*` directories, the flat raw-log directory,
// and the session tree's per-run `*.raw.log`. They are DECLARED in
// internal/scratch (declared_trees.go: path resolver + one age constant per
// tree); this file only drives them, so the GC's scope lives in one place a
// machine check can read (TestDeclaredTreesCoverRepoWriters).
//
// A separate ARM rather than more work for the default arm on purpose: the
// default arm's contract (the suite scratch home) is pinned by its own tests and
// by the installed watchdog, so a caller must be able to sweep one set without
// the other, and the two sweeps report on their own lines.
const scratchGCTreesFlag = "trees"

// scratchGCTreesRole labels the declared-trees arm, so an operator (and the
// watchdog log) tells the arms apart by the `role=` field alone.
const scratchGCTreesRole = "cli-trees"

// scratchGCFamiliesFlag is the FAMILY-CENSUS arm selector: `scratch-gc
// families` walks the sessions tree and reports, one line per SHAPE, which
// family it belongs to and what the registry decided about it — the SCOPE half
// of the declared trees, where `trees` is the BOUND half.
//
// WHY IT LIVES ON THIS VERB: the two arms answer the same question ("what does
// our tree hold, and what bounds it?") about the same tree, and the declared
// trees arm already reports per-tree bytes on this verb's logfmt line. A second
// verb would be a second schedule, a second help surface and a second place for
// the answer to drift (Rule 3).
const scratchGCFamiliesFlag = "families"

// scratchGCFamiliesRole labels the family-census arm.
const scratchGCFamiliesRole = "cli-families"

// scratchGCFamiliesLogFmt is the census line, one per shape: the shared
// scratchGCLogFmt field vocabulary EXTENDED with the five fields this arm exists
// to report — WHICH tree the shape was found in (the census covers the sessions
// tree and the profile's sibling trees), the shape itself, its family, the
// decision taken about it, how many entries carry it and how many bytes.
// `removed` is deliberately NOT reused for the count: the shared line's
// `removed=` field means "what this sweep deleted", and this face deletes
// nothing — so the census spells its own field names rather than describing a
// count of entries as removals.
const scratchGCFamiliesLogFmt = "scratch-gc role=%s tree=%s root=%s shape=%s family=%s decision=%s count=%d dir=%t bytes=%d"

// scratchGCFamiliesUnclassifiedDecision / scratchGCFamiliesUnknownFamily render
// a shape NO family claims. The decision is sessionlayout's own
// DecisionUnclassified (referenced, never re-spelled) and the family field says
// so in words, because the one thing this line must not do is look like a
// classified row.
const scratchGCFamiliesUnknownFamily = "NONE"

// scratchGCFamiliesListFlag is the census's enumeration flag: `families --list`
// prints every tree the arm can census and exits. It is the answer a reader
// needs BEFORE asking for one tree by name — the same "the verb names its own
// surface" duty the usage text carries, executable instead of prose.
const scratchGCFamiliesListFlag = "--list"

// scratchGCFamiliesTreeFlag is the census's TREE SELECTOR: `families --tree NAME`
// censuses one tree. Without it the arm censuses the SESSIONS TREE — its subject
// before the sibling trees existed, and the default on purpose: a default
// silently widened to five trees would multiply every existing reader's output.
//
// THE SPELLING LIVES IN ONE PLACE (Rules 13/14): it is the SAME option the
// `trees` arm now accepts, so the value is internal/scratch's
// DeclaredTreeFlagSpelling — referenced here, never re-typed. This name survives
// as the census arm's reader of it (the existing test face spells it), and a
// second literal `"--tree"` anywhere is what the mirror gates exist to catch.
const scratchGCFamiliesTreeFlag = scratch.DeclaredTreeFlagSpelling

// scratchGCFamiliesAllTrees is the selector value that asks for both groups: the
// sessions tree AND every sibling tree the registry declares (five today: see
// sessionlayout.SiblingTrees). Its SPELLING is the declared set's own `all`
// (Rule 14 — the literal lives once): the two selector vocabularies name
// different tree SETS (this one covers the sessions tree as well; the declared
// set does not contain it), but a reader types one word either way.
const scratchGCFamiliesAllTrees = scratch.DeclaredTreeSelectorAll

// scratchGCFamiliesSessionsTree is the SESSIONS TREE's name in the selector
// vocabulary. It is a READER-FACING name, not a declared-tree name: the census
// roots at scratch.SessionRawLogRoot, and the sessions tree is not itself a row
// of the declared set (its `session-raw` row bounds one family INSIDE it). The
// name is the profile's own directory name (Rule 13) — a second literal here is
// what TestSessionsDirNameNoHardcodedLiterals refuses, and it caught this one.
const scratchGCFamiliesSessionsTree = config.SessionsDirName

// scratchGCFamiliesNoWindow is the census line's `stale=` field: this arm
// applies NO age window (it removes nothing), and printing one would imply a
// bound the face does not apply. A NAMED value, not an empty string, so a
// reader can tell "no window by design" from "the field failed to render".
const scratchGCFamiliesNoWindow = "none"

// scratchGCFamiliesSummaryFmt is the census's per-tree closing line: how many
// DISTINCT shapes the tree holds, how many entries are unclassified, and the
// totals the census measured for THAT tree.
const scratchGCFamiliesSummaryFmt = "scratch-gc role=%s tree=%s root=%s shapes=%d unclassified=%d files=%d dirs=%d bytes=%d"

// scratchGCFamiliesReportFmt is the whole-run closing line: how many trees were
// censused (a tree that could not be walked is NOT counted — it is reported on
// stderr and the exit status says so) and what they held together. It is the one
// line a reader diffs between two runs.
const scratchGCFamiliesReportFmt = "scratch-gc role=%s trees=%d shapes=%d unclassified=%d files=%d dirs=%d bytes=%d"

// scratchGCTreesLogFmt is the declared-trees line: the EXISTING scratchGCLogFmt
// plus the two figures a reader needs to tell "this tree is empty" from "this
// tree is full and nothing in it has aged out yet": bytes= is what the plan
// WOULD reclaim, holding= is what the tree HOLDS right now and holding_n= how
// many entries that is. The pair is why the 2026-10-12 incident is readable in
// one line instead of sending its reader to `du` — measured then:
// `tree=budget-runs bytes=0` on a tree holding 10.27 GB.
// (same fields, same order — the same logfmt parsers keep working) EXTENDED with
// the two fields this arm exists to report: WHICH tree, and how many BYTES came
// back. The epic requires exactly this — report reclaimed bytes on the existing
// watchdog log line, not on a new channel, because a bound nobody can see is a
// bound nobody can check.
//
// Composed from scratchGCLogFmt rather than re-spelled (Rules 13/14): a field
// added to the shared format lands here automatically, so the two faces cannot
// drift.
//
// `removed=` IS ONLY EVER A MEASUREMENT ON THIS LINE. Every face that prints this
// format has MEASURED the figure — a live sweep's deletions, or the zero of an
// arm that deleted nothing (disabled, failed, unconfigured). The PLAN face does
// not print this format at all: it prints scratchGCTreesPlanLogFmt, because the
// two quantities are different facts and used to share one field name (see that
// constant).
const scratchGCTreesLogFmt = scratchGCLogFmt + " tree=%s present=%t entries=%d bytes=%d holding=%d holding_n=%d"

// scratchGCTreesPlanLogFmt is the DRY-RUN line: the same line as
// scratchGCTreesLogFmt with the ONE field whose meaning differs renamed —
// `removed=%d` becomes `plan=%d`.
//
// WHY IT IS A DERIVED FORMAT AND NOT ITS OWN LITERAL (Rules 13/14/43): the two
// lines must stay the same line, field for field, or an operator's logfmt diff
// between "what the pass would do" and "what it did" stops lining up. Deriving it
// from the live format means a field added there lands here automatically, and
// the ONE rename is stated in one place (a pin test asserts the substitution
// actually happened — TestScratchGCTrees_DryRunPlanIsNotARemovedField).
//
// THE BUG THIS EXISTS FOR (measured 2026-10-27, this arm): the dry-run line
// passed len(plan) into the shared `removed=` slot, so `--dry-run` printed
// `removed=1` on a pass that deleted NOTHING — a claim of a deletion, on the
// report-only face, that no reader could tell from a real one. The plan count is
// still worth printing (it is the whole answer the vetting face gives); it is the
// FIELD NAME that was wrong.
//
// `bytes=` is NOT renamed, and that is deliberate: the help text defines it as
// "what the plan WOULD reclaim (or, without --dry-run, did)", so on this face it
// is already documented as a plan figure rather than a measurement. `removed=`
// had no such definition — it reads as "what I deleted" everywhere else on the
// verb, which is exactly the ambiguity that misled a reader.
var scratchGCTreesPlanLogFmt = strings.Replace(scratchGCTreesLogFmt, scratchGCTreesRemovedField, scratchGCTreesPlanField, 1)

// scratchGCTreesRemovedField / scratchGCTreesPlanField are the two spellings the
// substitution above turns on. Named (Rule 1) so the pin test can assert the plan
// format contains the second and NOT the first without re-typing either.
//
// The names carry the `=` but NOT the value verb: the value's %d comes from the
// shared format and survives the substitution, which is why this pair is also the
// pair the HELP TEXT reads (it documents the fields by name; a constant carrying
// a scanf verb would print `plan=%d` to a human — observed on this change's
// first build, which is why the verb is not part of the name).
const scratchGCTreesRemovedField = "removed="
const scratchGCTreesPlanField = "plan="

// scratchGCDeclaredTreesLogFmt is the resident DECLARED-TREES arm's per-tree
// line: the shared scratchGCLogFmt plus the tree's identity and the bytes that
// came back. It is COMPOSED from scratchGCLogFmt for the same reason
// scratchGCTreesLogFmt is (Rules 13/14): a field added to the shared line lands
// on both faces at once, so a reader's logfmt filter keeps working across them.
//
// WHY IT IS A SECOND LINE AND NOT scratchGCTreesLogFmt ITSELF: the CLI arm's
// line carries `present=/entries=/holding=/holding_n=` because its job is to
// answer "what does this tree HOLD" (the figure the 2026-10-12 incident could
// not get). The resident arm's job is narrower — it deleted something on a tick
// nobody asked for, so it says what and how much, and the probe fields would
// cost two extra walks of every tree on every tick to answer a question the
// operator can ask of the CLI face.
const scratchGCDeclaredTreesLogFmt = scratchGCLogFmt + " tree=%s bytes=%d"

// scratchGCTreesSummaryLogFmt is the trees arm's UNCONDITIONAL per-tree SUMMARY
// line — the line that makes an empty plan impossible to misread as proof of
// protection.
//
// WHY IT IS A SEPARATE LINE AND NOT THREE MORE FIELDS ON THE LINE ABOVE
// (measured, this arm): the shared line shape is parsed by existing tests and by
// an operator's log filters, so its fields must not MOVE — a figure appended to
// it would also be invisible to anyone grepping for the count, and the count is
// the whole point. It is an ADDITIVE line, printed for every SELECTED tree on
// every pass (dry-run and live, `--explain` or not), carrying what the arm
// actually looked at. Four states print as empty output on the plan face and are
// separated here: the arm never ran, the tree does not exist on this host, the
// tree was walked and every entry was protected, and the tree was walked and had
// no entry at all.
const scratchGCTreesSummaryLogFmt = "tree-summary tree=%s examined=%d not_ours=%d collectable=%d kept=%d truncated=%t"

// scratchGCKeepReasonsLabel is the prefix of the trees arm's one-line reason
// DISTRIBUTION — WHICH rule is doing the protecting, which a kept total cannot
// say ("12 kept" does not tell an operator whether the clock or a live holder
// saved them). Composed from the package's own reason strings, so the label and
// the sentences it counts cannot drift.
const scratchGCKeepReasonsLabel = "kept-by-reason:"

// scratchGCSandboxesFlag is the SANDBOX arm selector: `scratch-gc sandboxes`
// sweeps the configured agent SANDBOX root (config.SandboxSettingsKey /
// $GOPI_SANDBOX_DIR / --sandbox-dir), removing only AGED, UNOWNED sandboxes.
//
// WHY IT IS AN ARM OF THIS VERB RATHER THAN SOMETHING NEW: the epic's hard
// constraint is no new daemon and no new tick. This verb IS the host-level
// one-shot (the systemd watchdog runs it) and the tick it exposes runs the
// same sweeps — so adding the sandbox predicate to internal/scratch and an arm
// here gives the sandbox tree the SAME existing reclamation machinery, with
// zero new schedules. (Corrected 2026-10-30: this named the "resident
// worker/merger tick" as the second caller, and there is none.)
//
// It is a separate ARM (not folded into the default) because the sandbox root is
// an ORDINARY DIRECTORY the operator may share, so its sweep must be scoped by
// name marker and must never be implied by a sweep of the suite scratch home.
const scratchGCSandboxesFlag = "sandboxes"

// scratchGCSandboxesRole labels the sandbox arm, so an operator tells it apart
// by the `role=` field alone (the same discipline as cli-trees).
const scratchGCSandboxesRole = "cli-sandboxes"

// scratchGCSandboxesLogFmt is the sandbox line: the shared scratchGCLogFmt
// extended with the reclaimed BYTES, composed rather than re-spelled (Rules
// 13/14) so a field added to the shared format lands here automatically. The
// `home=` field carries the resolved SANDBOX ROOT, so the line names the tree it
// swept — and the channel that decided it is reported on stderr beside it,
// because a default presented as a discovery is how a sweep ends up pointed at
// the wrong directory.
const scratchGCSandboxesLogFmt = scratchGCLogFmt + " bytes=%d"

// scratchGCStubsFlag is the WORKTREE-STUB arm selector: `scratch-gc stubs`
// reclaims the degenerate `gopi-<row stem>` directories a TEST-PROCESS worktree
// leak left in a REAL worktree root (each holding nothing but a `.git` pointer
// whose gitdir died with the /dev/shm suite run that created it).
//
// WHY IT IS AN ARM OF THIS VERB AND NOT SOMETHING NEW: Rule 35. This verb is
// already the host-level one-shot and its tick runs the same sweeps, so the arm
// adds a PREDICATE (internal/scratch/worktree_stub.go) and no
// schedule, no daemon and no state of its own.
//
// It is a SEPARATE arm rather than part of the default arm because its target is
// a directory the OPERATOR owns and shares with real worktrees: an operator (and
// the installed watchdog) must be able to run the suite-home sweep without ever
// touching a worktree root, and each root's verdicts are reported on their own
// lines.
const scratchGCStubsFlag = "stubs"

// scratchGCStubsRole labels the worktree-stub arm, so an operator tells it apart
// by the `role=` field alone (the same discipline as cli-trees/cli-sandboxes).
const scratchGCStubsRole = "cli-stubs"

// scratchGCStubsLogFmt is the stub arm's line: the EXISTING scratchGCLogFmt
// extended with the reclaimed BYTES, composed rather than re-spelled (Rules
// 13/14) so a field added to the shared format lands here automatically. The
// `home=` field carries the ROOT this line reports (one line per root), so a
// reader never has to guess which tree was swept.
const scratchGCStubsLogFmt = scratchGCLogFmt + " bytes=%d"

// scratchGCStubsYesFlag is the stub arm's CONFIRMATION flag. The arm REMOVES
// nothing without it: its target is a directory the operator shares with real
// worktrees, so the destructive step must be asked for explicitly (`--yes`), and
// every other spelling — including no flag at all — is a dry run. The rule is
// the sweep family's own (scratch-gc's default arm deletes, but the sandbox arm
// and this one are the two whose roots are ORDINARY directories).
const scratchGCStubsYesFlag = "--yes"

// scratchGCStubsRootFlag pins ONE worktree root instead of enumerating the
// configured ones. It exists because a dry run and the sweep that follows it
// must be able to name the same tree explicitly (the acceptance criterion: the
// deletion count equals the candidate count the dry run listed).
const scratchGCStubsRootFlag = "--root"

// scratchGCStaleMinFlag is the stale-window flag as the usage text spells it —
// the ONE definition of the spelling (Rule 1b), referenced by the parser's help
// text here and asserted by the CLI contract tests; config owns the authoritative
// constant (config.ScratchGCStaleMinFlagName) that the parser matches.
const scratchGCStaleMinFlag = "--" + config.ScratchGCStaleMinFlagName

// scratchGCParseGiB reads one of the gocache arm's two GiB-valued flags and
// returns it in whole GiB, or exits with a named refusal. A bad value is REFUSED
// rather than ignored: a bound or a floor the operator mistyped would silently
// become the default, and a threshold nobody can see is a threshold nobody can
// check (Rule 15). Zero is refused for the same reason (a zero bound means
// "evict everything", which is the opposite of a configured bound).
func scratchGCParseGiB(flagName, raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "gopi %s: %s requires a positive integer number of GiB, got %q\n",
			CliVerbScratchGC, flagName, raw)
		os.Exit(scratchGCExitError)
	}
	return n
}

// scratchGCParseStaleMin reads the --scratch-gc-stale-min value and returns it
// as minutes, or 0 when the value is unusable. It REPORTS a bad value and exits
// rather than continuing with the default: a watchdog's one-shot sweep with a
// mistyped window would silently reap nothing, which is the failure a threshold
// knob exists to prevent (Rule 15 — the sweep's off switch is the cadence knob,
// never a threshold that can never be exceeded).
func scratchGCParseStaleMin(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "gopi %s: %s requires a positive integer number of minutes, got %q\n",
			CliVerbScratchGC, scratchGCStaleMinFlag, raw)
		os.Exit(scratchGCExitError)
	}
	return n
}

// scratchGCUsageFingerprint is the usage text's first line, the string that
// proves a `-h` invocation reached THIS handler (the CLI contract suite and
// the unit tests both assert it — one definition, so a reworded usage cannot
// leave a stale fingerprint behind in a test).
const scratchGCUsageFingerprint = "gopi tool scratch-gc — sweep the suite scratch home ONCE and exit"

// scratchGCRoleCLI is the role label this face reports (the tick's labels are
// workerRoleLabel / mergerRoleLabel in scratch_gc_tick.go). One word, no
// spaces — it rides a logfmt line.
const scratchGCRoleCLI = "cli"

// scratchGCTickFlag selects the RESIDENT-TICK arm: it runs scratchGCTick once,
// under the CLI role label, and is the explicit face of the pass whose resident
// carrier was the worker's claim loop (operator ruling, 2026-10-26 — a worker
// dispatches, it does not run maintenance passes). Named `tick` because that is
// what it is: the function an operator can now ask for by name.
const scratchGCTickFlag = "tick"

// scratchGCBaseRole labels the memory-base arm of the same verb, so an
// operator reading a log tells the two arms apart by the `role=` field alone
// (both print a `scratch-gc …` line).
const scratchGCBaseRole = "cli-base"

// scratchGCPlanAgePrecision rounds the dry-run's reported age to the second —
// a human-readable age, not a nanosecond wall (time.Duration is nanoseconds).
const scratchGCPlanAgePrecision = time.Second

// scratchGCUsage is the -h text. It is a VAR, not a const, for ONE reason: the
// family-census section enumerates the censused trees by READING the registry
// (sessionlayout.SiblingTrees' own order), so the help text cannot drift from
// the trees the arm actually walks. A hand-spelled copy of the four names in
// this file is exactly the drift Rules 1b/13 ban — a fifth sibling tree landed
// in the registry would leave the help text describing four (Rule 1: the value
// comes from its ONE definition, not from a literal typed here).
var scratchGCUsage = scratchGCUsageFingerprint + ` (watchdog face).

This verb is the one-shot face a host-level watchdog (systemd --user timer /
cron) executes. CORRECTED 2026-10-30: this used to add "the resident sweep
(worker/merger tick) bounds the scratch tree only while a resident gopi is
alive", and no resident gopi fires that sweep — this verb (and its --tick arm)
is the only door:

  gopi tool scratch-gc            # sweep now; prints the removed count
  gopi tool scratch-gc --dry-run  # report what WOULD be removed; deletes nothing
  gopi tool scratch-gc --explain  # the VETTING face: the plan AND a verdict line
                             #   per candidate — why each may be collected, or why
                             #   it is kept — naming the recorded owner (pid +
                             #   kernel start ticks) the decision was measured
                             #   against. A FRESH root whose recorded owner is
                             #   gone, or whose pid was recycled, is collectable
                             #   NOW: the writer-owned verdict does not wait out
                             #   the age window.
                             #   ` + scratchGCExplainFlag + ` IMPLIES ` + scratchGCDryRunFlag + `: IT DELETES
                             #   NOTHING on every arm that deletes by default.
                             #   The only exception is the arm's own consent word
                             #   (` + scratchGCStubsYesFlag + ` on ` + scratchGCProfileFlag + `/` + scratchGCStubsFlag + `/` + scratchGCRootsFlag + `): spelling
                             #   that asks for the pass, so the report prints and
                             #   THEN the confirmed reclaim runs
  gopi tool scratch-gc --explain --dry-run
                             #   the same thing, spelled out (kept as a form
                             #   because a reader who wants to be explicit about
                             #   the read-only face should not have to know the
                             #   implication)
  gopi tool scratch-gc tick       # run the RESIDENT TICK once, by hand: the
                             #   same pass the worker's claim loop used to fire
                             #   (declared trees + sandbox sweep + the tick's own
                             #   staleness). The worker no longer carries it, so
                             #   this is the face an operator invokes by name
  gopi tool scratch-gc base       # also sweep the MEMORY BASE for this system's
                             #   own ad-hoc residue (gopi-*/gocache-*/probe-*)
  gopi tool scratch-gc base --dry-run   # the same, reporting only
  gopi tool scratch-gc trees      # ALSO sweep the DECLARED TREES this repository's
                             #   own commands create and never clean: the
                             #   GOTMPDIR tree's leftover go-build* dirs, the
                             #   flat raw-log dir, the session-tree *.raw.log,
                             #   and the shared build cache the merge/build
                             #   gate fills per checkout
  gopi tool scratch-gc trees --dry-run  # the same, reporting only
  gopi tool scratch-gc families   # CENSUS the sessions tree by FAMILY: one line per
                             #   SHAPE found (raw logs, records, metadata,
                             #   crash dumps, attachments, a probe's checkout),
                             #   naming the family that claims it and the
                             #   decision taken about it (bound:<tree> |
                             #   keep:<reason> | legacy). A shape NO family
                             #   claims prints family=NONE on stderr — the
                             #   report-only face of the range gate. Read-only:
                             #   it removes nothing and needs no --dry-run
  gopi tool scratch-gc profile    # REPORT the DIRECT CHILDREN of the gopi profile
                             #   root (<GOPI_HOME>/.gopi): each one's class
                             #   (state / declared / residue) and the sentence
                             #   that decided it, plus a count of the children
                             #   NOBODY declared. The profile root is a STATE
                             #   directory (ledger, settings, sessions, plugins,
                             #   locks) — this arm OBSERVES it; the bare form
                             #   deletes nothing. The --tick arm reports the
                             #   same way and has no deleting face at all
  gopi tool scratch-gc profile --dry-run    # the same, spelled out
  gopi tool scratch-gc profile --explain    # every child, not only the actionable ones
  gopi tool scratch-gc profile --yes        # ALSO reclaim the hand-made per-run
                             #   evidence family (evid-<token> /
                             #   sigrec-<token>): stale, held by no live
                             #   process, and nothing else — the root and every
                             #   declared child stay
  gopi tool scratch-gc sandboxes  # sweep the AGENT SANDBOX root: remove the
                             #   sandboxes whose recorded owner is gone and
                             #   that have aged out (never a live one, never
                             #   the root itself, never a directory that is
                             #   not a sandbox of this system's)
  gopi tool scratch-gc sandboxes --dry-run  # the same, reporting only
  gopi tool scratch-gc stubs      # reclaim the WORKTREE STUBS a test-process worktree
                             #   leak left in a REAL worktree root: directories
                             #   holding nothing but a .git pointer whose gitdir
                             #   died with the suite run that created it. The
                             #   roots are the settings repos table's worktree
                             #   roots (and $GOPI_WORKTREE_ROOT when set), because
                             #   the leak lands wherever the producer resolved a
                             #   root. Defaults to --dry-run: run it without that
                             #   flag ONLY after reading the dry run, and with the
                             #   same flags (a dry run with a different root is a
                             #   different set)
  gopi tool scratch-gc stubs --dry-run
                             #   list the candidates and, per candidate, WHY it
                             #   would go or WHY it is kept; deletes nothing
  gopi tool scratch-gc stubs --yes  # actually remove the candidates the same predicate
                             #   names (an explicit confirmation, because the root
                             #   is a directory the operator shares with real
                             #   worktrees)
  gopi tool scratch-gc roots      # CENSUS a worktree root: one line per direct child
                             #   naming its class — worktree (a live registration),
                             #   stub (the dead leak shape the stubs arm collects),
                             #   residue (an ad-hoc directory this system's own
                             #   tooling minted and which is NOT a worktree), or
                             #   foreign (a name this system does not mint). The
                             #   residue class is what no other arm reports, and it
                             #   is printed with the sentence that decided it.
                             #   Read-only: the bare form deletes nothing
  gopi tool scratch-gc roots --explain   # every child, worktree and foreign included
  gopi tool scratch-gc roots --yes       # ALSO reclaim the stub class, delegating to
                             #   the stubs arm's own predicate and sweep — this arm
                             #   has no removable set of its own
  gopi tool scratch-gc roots --root DIR  # census DIR instead (same refusal as stubs)
  gopi tool scratch-gc gocache    # MEASURE the AGENT-side Go toolchain cache
                             #   (the directory resolved with the toolchain's own
                             #   precedence: --path > $GOCACHE > GOCACHE in the go
                             #   env file ~/.config/go/env > <user cache dir>/go-build)
                             #   and judge it: its
                             #   size against the bound, and the FREE SPACE OF THE
                             #   FILESYSTEM THAT HOLDS IT against the headroom
                             #   floor. The line names the path, the device, the
                             #   filesystem type and both byte counts — the
                             #   measurement Rule 22's off-/ test cannot make when
                             #   the partition that filled is /home rather than /.
                             #   It also names its GRAIN (grain=cache-entry): the
                             #   unit reclaimable= is counted in, which is the
                             #   toolchain's own (<hex>-a index / <hex>-d data pair),
                             #   not the whole two-hex shard. When a cache is over its
                             #   bound
                             #   and nothing is past the grace window, the arm SAYS
                             #   so in words instead of printing a bare 0 that reads
                             #   as "your cache is fine".
                             #   DELETES NOTHING: the toolchain cache is the
                             #   operator's shared, cross-project asset, so the
                             #   finder of an exhaustion proposes and the operator
                             #   decides
  gopi tool scratch-gc gocache --path DIR
                             #   measure DIR instead of the resolved cache (a second
                             #   toolchain, a controlled fixture)
  gopi tool scratch-gc gocache ` + scratchGCGocacheMaxGiBFlag + ` GiB
                             #   judge the cache against GiB INSTEAD of the
                             #   resolved bound — the invocation's own bound,
                             #   outranking the ` + scratch.ToolchainCacheMaxEnvName + `
                             #   variable and the default. This is what makes the
                             #   bound exercisable on a cache that is not 25 GiB
  gopi tool scratch-gc gocache ` + scratchGCGocacheHeadroomGiBFlag + ` GiB
                             #   require GiB of FREE SPACE on the filesystem holding
                             #   the cache — the invocation's own floor, instead of the
                             #   default. A cache under its bound still fails this arm
                             #   when the partition it lives on is nearly full
  gopi tool scratch-gc gocache --trim
                             #   ALSO reclaim: remove whole cache ENTRIES at the
                             #   toolchain's own grain (the <hex>-a / <hex>-d file
                             #   pair inside a shard), OLDEST FIRST, down to the bound.
                             #   The toolchain's own bookkeeping (README, trim.txt,
                             #   log.txt, CACHEDIR.TAG) is never touched, no shard
                             #   DIRECTORY is ever removed, the cache ROOT is never
                             #   removed, and an entry whose newest file is inside the
                             #   24h grace window is kept — so a shard holding one
                             #   fresh entry still gives up the cold entries beside it.
                             #   The before/after byte counts are printed verbatim.
                             #   Withheld (and said so) when the sweeps are disabled
                             #   on this host
  gopi tool scratch-gc deps       # REPORT what is scheduled and resident on this host:
                             #   the installed crontab, the user/system unit files
                             #   that can schedule or reside, and the resident
                             #   gopi-shaped processes with each one's detachment
                             #   verdict measured from /proc/<pid>/cgroup (Rule 24).
                             #   READ-ONLY: it writes no crontab, touches no unit,
                             #   signals no process. This is the HOST half of Rule 35;
                             #   repo half (self-scheduling scripts, scheduler
                             #   install recipes, tracked state/lock artifacts) is
                             #   tested by: go test ./internal/testutil/ -run Rule35
  gopi tool scratch-gc -h         # this help (zero side-effects)

The declared trees are the ones no bound used to reach (measured 2026-09-20:
gopi-go-tmp 54.5 GB, ~/.gopi/raw 27 GB, sessions/**/*.raw.log 51.9 GB,
gopi-build-cache 16.6 GB). Each carries its OWN age constant
(internal/scratch/declared_trees.go: GoTmpStaleAge 24h, RawLogStaleAge 72h,
SessionRawLogStaleAge 336h, BuildCacheStaleAge 24h) and every line of this arm
reports that tree's reclaimed BYTES (the bytes= field).

Read the OTHER fields with their own definitions, because two of them look
like a bounded/unbounded verdict and neither is one:

  bytes=      what the plan WOULD reclaim (or, without --dry-run, did).
  removed=    how many entries this pass DELETED. It is a measurement, never a
              plan: under ` + scratchGCDryRunFlag + ` the line reports the plan count as
              ` + scratchGCTreesPlanField + ` instead, so a report-only pass cannot print a
              deletion it did not perform. A face that deleted nothing (disabled,
              failed, unconfigured) prints removed=0 with its own reason beside it.
  plan=       the DRY-RUN line's count of entries the pass WOULD remove. It appears
              only there — a live line never prints it, because the honest figure on
              a live pass is removed=.
  holding=    the bytes of every direct entry this tree's OWN namespace rule
              matches, at ANY age. It is NOT a du(1) of the tree: material the
              rule refuses (a flat evidence file in a tree of directories) is
              not counted, so holding= can be far below the tree's real size.
  holding_n=  how many entries that is — the same walk the probe counts, so it
              equals entries= on every tree (it is NOT a count of entries held
              by a live process; the live-holder veto is applied per candidate
              by the sweep, and appears only in the verdict sentences).

A tree can therefore print collectable=0 / kept=<n> / holding= all day while most
of its BYTES sit in a class its own rule never collects. The measured case is
budget-runs on this host (2026-10-27): ~8.7 GiB of run directories (the only
collectable class) beside ~7.0 GiB of flat per-run evidence that is never a
candidate at ANY age — the oldest 15 days past every window — because a run's
stream is the evidence a reader needs when a run is missing its completion line.
The bound for that half belongs to the writer (scripts/test-budget.sh), not to
this sweep; do not read a kept= count as "nothing here needs attention".
 The build cache is
bounded by AGE because its writer's own per-key bound cannot bound it: measured,
no key reaches 7.5% of that bound, so the tree grows by accumulating one
throwaway key per worktree per task. Its keys carry no useful mtime of their
own (the gate creates the key and the toolchain fills the cache below it), so a
key is collected only when every file inside it is older than the window. A
declared tree's ROOT is never removed at any age — gopi-go-tmp IS GOTMPDIR,
and a sweep that deleted it broke the next build — so only aged, live-unheld
CHILDREN go; an empty session directory is removed on the longer container
window.

What it does: removes entries under the scratch home that this system created
(suite run roots, legacy roots, queue entries) AND are older than the stale
age AND are held by no live process. A directory held by a live process is
NEVER removed.

With the base flag, the SAME rules are applied to the direct children of the
resolved memory base (` + scratch.BaseEnvName + `, default ` + scratch.DefaultBasePath + `):
only this system's own ad-hoc name families are candidates, each justified as
a name this repository's own commands produce — a build cache an agent
pointed there (` + scratch.BaseResiduePrefixGocache + `), a session's ad-hoc
entry (` + scratch.BaseResiduePrefixGopi + `), a probe directory
(` + scratch.BaseResiduePrefixProbe + `). A foreign name is not a candidate at
all, and neither is a symlink; an entry carrying such a name is residue whether
it is a DIRECTORY or a REGULAR FILE (a session that builds with
-o <base>/gopi-<what> leaves a binary there, and a name-only rule that could
not collect it left the file forever). With ` + scratchGCExplainFlag + ` every
in-namespace entry gets a verdict line: collectable, or KEPT with the sentence
that decided it (a symlink, a foreign kind, too fresh, held by a live process) —
so a removed=0 names its reason instead of being silent. The base itself is
never created; a base on the same filesystem as the boot partition is
refused (measured device ids, not spelling).

Arms:
  ` + scratchGCTreesFlag + `    the declared trees this repository creates and never cleans
              (one logfmt line per tree, with its own stale window and the
              bytes reclaimed).
  gopi ` + CliVerbTool + ` ` + CliVerbScratchGC + ` ` + scratchGCTreesFlag + ` ` + scratch.DeclaredTreeFlagSpelling + ` NAME
              Sweep ONLY that tree — an operator can bound the GOTMPDIR tree
              alone without reclaiming the build cache beside it. Accepts
              both ` + scratch.DeclaredTreeFlagSpelling + ` NAME and ` + scratch.DeclaredTreeFlagSpelling + `=NAME. The value
              ` + scratch.DeclaredTreeSelectorAll + ` means every declared tree, which is what omitting the
              selector has always done. An UNKNOWN name is refused, with the
              valid names listed, in ` + scratchGCDryRunFlag + ` too — a typo must never read
              as "the tree was already clean". Current names:
              ` + strings.Join(scratch.DeclaredTreeNames(), ", ") + `
  ` + scratchGCFamiliesFlag + ` the FAMILY CENSUS: one line per SHAPE found,
              naming the family that claims it and the decision taken about it
              (bound:<tree> | keep:<reason> | legacy). A shape NO family claims
              is reported with family=NONE — the report-only face of the range
              gate (GOPI.md Rule 15). It removes nothing.
              Two groups of trees are censused: the SESSIONS TREE itself, and the
              trees that hold its siblings — the gopi profile's own children
              (` + strings.Join(sessionlayout.SiblingTreeNames(), ", ") + `), plus the
              scratch home's dump class trees
              (` + strings.Join(sessionlayout.ScratchDumpsTreeNames(), ", ") + `), which are
              where the dump writers land when no dump knob is exported (their
              documented default) and therefore the pair that exists on a host
              that has configured nothing. The sessions tree is censused first; a
              tree that does not exist on this host is REPORTED as absent, never
              silently skipped.
  gopi ` + CliVerbTool + ` ` + CliVerbScratchGC + ` ` + scratchGCFamiliesFlag + ` ` + scratchGCFamiliesListFlag + `
              List the trees this arm censuses, one per line, and exit — the
              enumeration a reader needs before asking for one by name.
  gopi ` + CliVerbTool + ` ` + CliVerbScratchGC + ` ` + scratchGCFamiliesFlag + ` ` + scratchGCFamiliesTreeFlag + ` NAME
              Census ONLY that tree (a sessions-tree name, a sibling tree name,
              or ` + scratchGCFamiliesAllTrees + ` for every tree). An unknown
              name is refused with the list of known ones — a typo must never
              read as "that tree holds nothing".

Flags:
  --dry-run   Report only — no delete, no lock taken.
  ` + scratchGCExplainFlag + `  A verdict line per candidate in addition to the plan:
              whether it may be collected and the sentence that decided it, with
              the recorded owner it was measured against. IT IS A JUDGEMENT, NOT
              A PASS: ` + scratchGCExplainFlag + ` IMPLIES ` + scratchGCDryRunFlag + `, so this flag
              alone deletes nothing on every arm whose bare form sweeps — the
              implied report is printed with dry-run=true on its count line,
              because it IS one. The ONE exception is the arm's own consent word
              (` + scratchGCStubsYesFlag + ` on ` + scratchGCProfileFlag + `/` + scratchGCStubsFlag + `/` + scratchGCRootsFlag + `): that word is how this
              family says "I mean the deletion", so the report prints first and the
              confirmed reclaim runs after it. To delete on the arms that sweep by
              default, spell the sweep without ` + scratchGCExplainFlag + `.
              It changes nothing on ` + scratchGCGocacheFlag + `: that arm deletes only with
              its own ` + scratchGCGocacheTrimFlag + ` and carries no verdict report at all.
  ` + scratch.DeclaredTreeFlagSpelling + ` NAME
              Restrict ` + scratchGCTreesFlag + ` to ONE declared tree (both
              ` + scratch.DeclaredTreeFlagSpelling + ` NAME and ` + scratch.DeclaredTreeFlagSpelling + `=NAME). The census arm
              accepts the same option for its own tree vocabulary.
  ` + scratchGCStaleMinFlag + ` N
              The stale window in minutes for the HOME arm and the ` + scratchGCBaseFlag + ` arm
              (same as the env below). It does NOT set the window of the
              ` + scratchGCTreesFlag + ` arm: each declared tree carries its OWN age constant
              (see the table below), because the trees differ in what losing
              them costs and one global window would either over-protect the
              cheap ones or under-protect the cache. The ` + scratchGCTreesFlag + ` arm
              REPORTS the window it applied as its own stale= field on each
              tree's line, so the answer is read there, never assumed.
              THE TWO ARMS NOW AGREE BY CONSTRUCTION, and they agree on the
              TREE's window, never on this one. This flag used to leak: the HOME
              arm reached a declared tree's root through its queue walk and
              passed this value along as an override that WON over the tree's own
              constant, so a 1-minute window collected entries out of a 6h tree
              (measured 2026-10-27 on the budget-runs tree: the ` + scratchGCTreesFlag + ` arm
              reported stale=6h collectable=0 while the home arm planned all 401
              of the same entries). The override channel is gone — the sweep, the
              plan and the verdict faces take no window parameter at all — so the
              window printed as a tree's stale= field is the window applied to it
              by every arm, this flag or no flag.
  -h|--help   This text.

Env:
  ` + scratch.GCStaleAgeEnvName + `=N  the STALE WINDOW in minutes: an unheld
              scratch entry older than N minutes is garbage (default 360 = 6h).
              A held entry is NEVER collected, whatever its age. Set it through
              --scratch-gc-stale-min N, this variable, or settings.json
              "` + scratch.GCStaleAgeSettingsKey + `"; a non-positive or
              non-integer value is refused/reported and the default applies.
  ` + scratch.GCIntervalEnvName + `=0 (or a negative value, or settings.json
              "scratch_gc": false) disables the sweep: the verb reports
              disabled=true and exits 0 without touching the tree.
  ` + scratch.BaseEnvName + `  the memory base (default ` + scratch.DefaultBasePath + `).

Exit status:
  0   swept (or nothing to do, or disabled by the operator)
  1   the sweep could not run — reason on stderr
`

// isScratchGCSubcommand reports whether argv begins with the scratch-gc verb.
func isScratchGCSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbScratchGC {
		runScratchGCSubcommand(args[1:])
		return true
	}
	return false
}

// runScratchGCSubcommand runs the one-shot sweep. It returns nothing: the
// caller (main's dispatch chain) expects the verb to handle its own exit.
//
// The verb has TWO arms, selected by the optional first argument: the default
// (home) arm and the `base` arm. They share the parser, the disabled switch and
// the log shape, and differ only in WHICH tree is swept and which predicate
// internal/scratch applies — no sweep logic lives here.
func runScratchGCSubcommand(args []string) {
	dryRun := false
	explain := false
	arm := scratchGCRoleCLI
	if len(args) > 0 {
		switch args[0] {
		case scratchGCBaseFlag:
			arm = scratchGCBaseRole
			args = args[1:]
		case scratchGCTreesFlag:
			arm = scratchGCTreesRole
			args = args[1:]
		case scratchGCFamiliesFlag:
			arm = scratchGCFamiliesRole
			args = args[1:]
		case scratchGCProfileFlag:
			arm = scratchGCProfileRole
			args = args[1:]
		case scratchGCSandboxesFlag:
			arm = scratchGCSandboxesRole
			args = args[1:]
		case scratchGCStubsFlag:
			arm = scratchGCStubsRole
			args = args[1:]
		case scratchGCGocacheFlag:
			arm = scratchGCGocacheRole
			args = args[1:]
		case scratchGCRootsFlag:
			arm = scratchGCRootsRole
			args = args[1:]
		case scratchGCTickFlag:
			// THE RESIDENT TICK, ON DEMAND. `scratch-gc` in its other arms is a
			// THIN face over internal/scratch's own functions; THIS arm is the
			// one-shot face of the RESIDENT tick (scratchGCTick) — the pass the
			// WORKER's claim loop used to fire every round. Taking it off that
			// loop without this arm would have left a duty whose implementation
			// no production command reached (Rule 15), and "some other arm
			// cleans scratch too" is not coverage: the tick carries the declared
			// trees, the sandbox sweep and the tick's own two-position staleness
			// that the arms do not share.
			//
			// `now` is passed in rather than read inside, exactly as the resident
			// callers do, so the throttle is the tick's own and NOT this face's:
			// an operator invoking it explicitly gets the pass, while the tick
			// still refuses to re-run inside its own interval (the last-run clock
			// is process state, and a fresh process always sees a zero clock —
			// the same reasoning the deleted `task list --sweep` recorded for
			// its persisted stamp).
			scratchGCSweep(scratchGCRoleCLI)
			return
		case scratchGCDepsFlag:
			// The DISCOVERY arm: it reports what is scheduled and resident on
			// this host and returns. It sweeps nothing, so it takes no flags,
			// and it is deliberately NOT gated by the sweep's off switch — an
			// operator who disabled the sweeps still needs to see what their
			// host is running.
			runScratchGCDeps()
			return
		}
	}
	// staleMin is this invocation's FLAG value for the stale window (0 = not
	// given). It is published through the knob's ONE publisher AFTER parsing,
	// so the flag outranks an exported variable exactly as it does on the
	// agent path (config.PublishScratchGCStaleMin owns that ranking — the verb
	// does not re-decide it, Rule 2/13).
	staleMin := 0
	// The stub arm's own two flags (they are meaningless for the other arms, so
	// they are parsed here and IGNORED by them rather than plumbed through every
	// arm's signature).
	stubRoot := ""
	stubYes := false
	// treeSelector is the TREE SELECTOR's value — the flag BOTH the census arm
	// and the declared-trees arm read (`--tree NAME`), parsed once here because
	// one option has one parse (a second parse block is a second place for the
	// two arms to disagree about `--tree=X`). An ABSENT selector is the empty
	// string and means every declared tree; an EXPLICITLY EMPTY one
	// (`--tree=`, the shape a truncated argument leaves) is refused at the
	// parser rather than read as "every tree", because widening a bounded
	// destructive pass by accident is the footgun this arm's sibling
	// introduced (devdoc/scratch-gc-declared-trees.md).
	treeSelector := ""
	// familiesList is the census arm's `--list` flag. It is its own variable
	// rather than a magic selector value: `--list` is an ACTION (enumerate and
	// exit), not a tree name, and folding it into the selector is how a tree
	// that happens to be called "list" would become unselectable.
	familiesList := false
	// The gocache arm's own two flags, parsed here for the same reason: `--path`
	// measures a cache other than the resolved one, `--trim` is the arm's ONLY
	// deleting face (the bare form reports — the operator's shared toolchain cache
	// is not brought under a bound by a report, Rule 41).
	gocachePath := ""
	gocacheTrim := false
	// The bed the gocache arm is JUDGED against, in GiB, for THIS invocation: the
	// bound and the free-space floor. 0 = not given, so the knob's own reader
	// applies its ranking (env, then default) — one reader, one precedence.
	gocacheMaxGiB := int64(0)
	gocacheHeadroomGiB := int64(0)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Print(scratchGCUsage)
			os.Exit(0)
		case a == scratchGCDryRunFlag:
			dryRun = true
		case a == scratchGCExplainFlag:
			explain = true
		case a == scratchGCStubsYesFlag:
			stubYes = true
		case a == scratchGCGocacheTrimFlag:
			gocacheTrim = true
		case a == scratchGCGocachePathFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s: %s requires a value (a cache directory path)\n", CliVerbScratchGC, a)
				os.Exit(scratchGCExitError)
			}
			gocachePath = args[i+1]
			i++
		case strings.HasPrefix(a, scratchGCGocachePathFlag+"="):
			gocachePath = strings.TrimPrefix(a, scratchGCGocachePathFlag+"=")
		case a == scratchGCGocacheMaxGiBFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s: %s requires a value (GiB)\n", CliVerbScratchGC, a)
				os.Exit(scratchGCExitError)
			}
			gocacheMaxGiB = scratchGCParseGiB(a, args[i+1])
			i++
		case strings.HasPrefix(a, scratchGCGocacheMaxGiBFlag+"="):
			gocacheMaxGiB = scratchGCParseGiB(scratchGCGocacheMaxGiBFlag, strings.TrimPrefix(a, scratchGCGocacheMaxGiBFlag+"="))
		case a == scratchGCGocacheHeadroomGiBFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s: %s requires a value (GiB)\n", CliVerbScratchGC, a)
				os.Exit(scratchGCExitError)
			}
			gocacheHeadroomGiB = scratchGCParseGiB(a, args[i+1])
			i++
		case strings.HasPrefix(a, scratchGCGocacheHeadroomGiBFlag+"="):
			gocacheHeadroomGiB = scratchGCParseGiB(scratchGCGocacheHeadroomGiBFlag, strings.TrimPrefix(a, scratchGCGocacheHeadroomGiBFlag+"="))
		case a == scratchGCFamiliesListFlag:
			familiesList = true
		case a == scratchGCFamiliesTreeFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s: %s requires a value (the name of ONE declared tree, or %s for all of them; valid names: %s)\n",
					CliVerbScratchGC, a, scratch.DeclaredTreeSelectorAll, strings.Join(scratch.DeclaredTreeNames(), ", "))
				os.Exit(scratchGCExitError)
			}
			treeSelector = args[i+1]
			i++
		case strings.HasPrefix(a, scratchGCFamiliesTreeFlag+"="):
			// An EXPLICITLY EMPTY value is refused, not read as "every tree":
			// the selector bounds a pass that DELETES, so a truncated argument
			// must fail loudly rather than widen the pass to the whole set.
			value := strings.TrimPrefix(a, scratchGCFamiliesTreeFlag+"=")
			if strings.TrimSpace(value) == "" {
				fmt.Fprintf(os.Stderr, "gopi %s: %s= names no tree — pass a declared tree's name, or %s to sweep every one (valid names: %s)\n",
					CliVerbScratchGC, scratchGCFamiliesTreeFlag, scratch.DeclaredTreeSelectorAll, strings.Join(scratch.DeclaredTreeNames(), ", "))
				os.Exit(scratchGCExitError)
			}
			treeSelector = value
		case a == scratchGCStubsRootFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s: %s requires a value (a worktree root path)\n", CliVerbScratchGC, a)
				os.Exit(scratchGCExitError)
			}
			stubRoot = args[i+1]
			i++
		case strings.HasPrefix(a, scratchGCStubsRootFlag+"="):
			stubRoot = strings.TrimPrefix(a, scratchGCStubsRootFlag+"=")
		case a == scratchGCStaleMinFlag:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "gopi %s: %s requires a value (minutes)\n", CliVerbScratchGC, a)
				os.Exit(scratchGCExitError)
			}
			staleMin = scratchGCParseStaleMin(args[i+1])
			i++
		case strings.HasPrefix(a, scratchGCStaleMinFlag+"="):
			staleMin = scratchGCParseStaleMin(strings.TrimPrefix(a, scratchGCStaleMinFlag+"="))
		default:
			fmt.Fprintf(os.Stderr, "gopi %s: unexpected argument %q (want %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s or -h)\n",
				CliVerbScratchGC, a, scratchGCBaseFlag, scratchGCTreesFlag, scratchGCProfileFlag, scratchGCSandboxesFlag, scratchGCStubsFlag,
				scratchGCRootsFlag, scratchGCDepsFlag, scratchGCGocacheFlag, scratchGCDryRunFlag, scratchGCExplainFlag, scratchGCStubsYesFlag, scratchGCStubsRootFlag,
				scratchGCStaleMinFlag, scratchGCGocachePathFlag, scratchGCGocacheTrimFlag, scratchGCGocacheMaxGiBFlag, scratchGCGocacheHeadroomGiBFlag)
			os.Exit(scratchGCExitError)
		}
	}
	// --explain IS A JUDGEMENT, SO IT IMPLIES --dry-run — decided ONCE here, after
	// the parse and before any arm reads the flag, so the promise is identical on
	// every face (scratchGCExplainImpliesDryRun carries the rule and the measured
	// failure that produced it). Applied to the PARSED VARIABLE rather than by
	// rewriting args, and after the loop, so `--explain --dry-run --explain` and
	// every other ordering normalise to the same thing.
	if scratchGCExplainImpliesDryRun(arm, explain, args) {
		dryRun = true
	}
	if staleMin > 0 {
		config.PublishScratchGCStaleMin(nil, &config.Config{ScratchGCStaleMin: staleMin})
	}
	// The knob's value is validated ONCE, through config's own predicate, i.e.
	// the same refusal the agent path gives: this verb's flag was already hoisted
	// into the variable by config.BootstrapEnvFromArgs before the verb ran, so
	// validating the VARIABLE covers the flag and the env face alike (Rule 14:
	// one definition of "is this window usable").
	if err := config.ValidateScratchGCStaleMinEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbScratchGC, err)
		os.Exit(scratchGCExitError)
	}

	// The operator's off switch is shared with the tick (ONE reader, Rule 2):
	// an explicitly disabled sweep reports itself and exits 0 — the
	// watchdog must never alarm on a deliberate configuration.
	if _, enabled := scratchGCInterval(); !enabled {
		switch arm {
		case scratchGCBaseRole:
			fmt.Printf(scratchGCBaseLogFmt+"\n", arm, scratch.ResolveBase(), 0, scratch.ResolveStaleAge(), true)
		case scratchGCTreesRole:
			// ONE line per SELECTED declared tree, so a disabled watchdog
			// cannot be misread as "those trees are bounded": each line names
			// its tree (with the tree's own age) and says disabled=true. The
			// SELECTOR is honoured here too — an unknown name is refused in
			// this path exactly as it is on the live one, because "the sweep
			// is off" must not become a second, more permissive parser.
			trees, terr := scratch.DeclaredTreesForName(treeSelector)
			if terr != nil {
				refuseDeclaredTreeSelector(scratchGCTreesFlag, terr)
			}
			for _, tree := range trees {
				// holding= is measured even here, and that is the point: a
				// DISABLED sweep is exactly when a reader most needs to know
				// how much the trees are holding while nothing bounds them
				// (bytes= is 0 because a disabled arm reclaims nothing).
				holding, holdingN := scratch.TreeHeldTotal(tree)
				// present=/entries= are measured even here, for the same
				// reason: "the watchdog is disabled" and "there is no such
				// tree" are different facts, and a row that could not tell
				// them apart was the defect this arm's line was fixed for.
				probe := scratch.ProbeDeclaredTree(tree)
				fmt.Printf(scratchGCTreesLogFmt+"\n", arm, "", 0, tree.StaleAge, true, tree.Name,
					probe.Present, probe.Entries, int64(0), holding, holdingN)
			}
		case scratchGCFamiliesRole:
			// The census is READ-ONLY: it is not gated by the sweep's off
			// switch (an operator who disabled the SWEEPS still needs to see
			// what the tree holds), so it runs and returns before the switch
			// below can decide anything.
			runScratchGCFamilies(treeSelector, familiesList)
			return
		case scratchGCSandboxesRole:
			// The sandbox arm's own line: the root it WOULD have swept and the
			// window it would have applied. The stale field carries the SANDBOX
			// window (not the scratch home's) so a disabled sweep still reports
			// the bound the operator configured.
			root, _ := sandboxRootForSweep()
			fmt.Printf(scratchGCSandboxesLogFmt+"\n", arm, root, 0, scratch.SandboxStaleAge, true, int64(0))
		case scratchGCStubsRole:
			// ONE line per root the arm WOULD have swept, so a disabled switch
			// cannot be misread as "those roots were checked and are clean": this
			// arm's candidates are permanent residue, not an age-bounded tree.
			for _, r := range stubWorktreeRoots(true) {
				fmt.Printf(scratchGCStubsLogFmt+"\n", arm, r.root, 0, scratch.SandboxStaleAge, true, int64(0))
			}
		case scratchGCRootsRole:
			// The CENSUS is read-only, so an operator who disabled the SWEEPS
			// still sees what their worktree root holds — and the arm's removing
			// half is forced OFF here, because a disabled sweep must not delete
			// anything even when the operator's flag asks (the stubs case above
			// makes the opposite choice only because it has no read-only face).
			runScratchGCRoots(true, false, explain, stubRoot)
			return
		case scratchGCGocacheRole:
			// The toolchain cache's MEASUREMENT is read-only and is therefore
			// still taken (an operator who disabled the sweeps still needs to see
			// how close their /home is); the arm's deleting half is forced OFF
			// here whatever the flags say, and the arm SAYS SO on stderr rather
			// than silently skipping the reclaim it was asked for.
			runScratchGCGocache(gocachePath, gocacheTrim, false, gocacheMaxGiB, gocacheHeadroomGiB)
			return
		default:
			fmt.Printf(scratchGCLogFmt+"\n", arm, scratch.HomeDir(), 0, scratch.ResolveStaleAge(), true)
		}
		return
	}

	if arm == scratchGCBaseRole {
		runScratchGCBase(dryRun, explain)
		return
	}
	if arm == scratchGCTreesRole {
		runScratchGCTrees(dryRun, explain, treeSelector)
		return
	}
	if arm == scratchGCFamiliesRole {
		// Reached when the sweep is ENABLED (the disabled path above returns
		// first). Same read-only census either way.
		runScratchGCFamilies(treeSelector, familiesList)
		return
	}
	if arm == scratchGCProfileRole {
		runScratchGCProfile(dryRun, explain, stubYes)
		return
	}
	if arm == scratchGCSandboxesRole {
		runScratchGCSandboxes(dryRun)
		return
	}
	if arm == scratchGCStubsRole {
		runScratchGCStubs(dryRun, stubYes, stubRoot, explain)
		return
	}
	if arm == scratchGCRootsRole {
		runScratchGCRoots(dryRun, stubYes, explain, stubRoot)
		return
	}
	if arm == scratchGCGocacheRole {
		// The ENABLED path: `--trim` is honoured here (and only here), so the
		// reclaim the operator asked for actually runs.
		runScratchGCGocache(gocachePath, gocacheTrim, true, gocacheMaxGiB, gocacheHeadroomGiB)
		return
	}

	// The dry-run face inspects WITHOUT creating the home: a read-only
	// report must not be able to bring a tree into existence.
	if dryRun {
		reportScratchGCDryRun(scratch.HomeDir(), explain)
		return
	}

	home, err := scratch.EnsureHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbScratchGC, err)
		os.Exit(scratchGCExitError)
	}
	// The verdict face: the judgement BEFORE the delete, so an operator reading a
	// zero removal count can see which candidates were looked at and why each
	// survived. The REPORT deletes nothing and takes no lock; the COMMAND reaches
	// this line only with an explicit consent word, because `--explain` alone is
	// normalized onto the dry-run face above (scratchGCExplainImpliesDryRun). It is
	// the consent form — `profile --explain --yes`'s shape on this arm — where the
	// report runs first and the confirmed reclaim follows, so the set the verdicts
	// name is exactly the set the sweep is about to judge.
	if explain {
		reportScratchGCVerdicts(home)
	}
	removed, lockVerdict, err := scratch.SweepHomeDetailed(home, scratch.ResolveStaleAge())
	if err != nil {
		// A sweep that could not complete is a watchdog-worthy event: report
		// what was removed so far (Rule 15 — no silent failure) and exit
		// non-zero so the timer's status surfaces it.
		fmt.Println(scratchGCLockLine(fmt.Sprintf(scratchGCLogFmt, arm, home, removed, scratch.ResolveStaleAge(), false), lockVerdict))
		fmt.Fprintf(os.Stderr, "gopi %s: sweep of %s failed: %v\n", CliVerbScratchGC, home, err)
		os.Exit(scratchGCExitError)
	}
	// THE LOCK EVIDENCE (Rule 15, epic 01a1300b). `removed=0` used to be the WHOLE
	// story: a `.gc.lock` left behind by a CRASHED sweep deferred every pass
	// silently, so "nothing to collect" and "the sweep never ran" printed the same
	// line. The count line now carries the lock decision it was produced under,
	// and a pass that did anything other than plainly acquire the lock gets its
	// OWN line first (who held it, and why it was or was not broken).
	reportScratchGCLock(arm, home, lockVerdict)
	fmt.Println(scratchGCLockLine(fmt.Sprintf(scratchGCLogFmt, arm, home, removed, scratch.ResolveStaleAge(), false), lockVerdict))
	// The CAPTURE-UNIT arm reports on its OWN count line, separate from the
	// scratch line above. That separation is the epic's 4th acceptance clause and
	// it closes a measured Rule-15 defect: this verb stopped six `gopi-shot-*`
	// units while the only count on its output was `removed=4` — the scratch
	// QUEUE DIRECTORIES (queue-runtime-job-*, -merge-resolve-*, …). Not one line
	// named a unit, so an operator could not tell that the capture defence had
	// run at all, let alone how much it reclaimed.
	//
	// `renewed` is the arm's own word (see internal/scratch's
	// ShotUnitReclaimLogFmt): the units whose stop this pass completed. The count
	// is printed on the zero path too — "the defence ran and renewed nothing" is
	// a fact, and a line that only appears when it fired cannot be distinguished
	// from a defence that is not wired.
	renewed := reclaimShotUnits(arm)
	fmt.Printf(scratchGCShotUnitsLogFmt+"\n", arm, renewed)
}

// reclaimShotUnits stops the registered capture units whose creating shot
// process is provably gone — the SECOND thing this one-shot verb does, on the
// same invocation and the same clock. It returns the number of units RENEWED, so
// the verb can report the capture arm's work on its own count line (the scratch
// count above it is the queue-directory arm's, and the two must never be read as
// one number).
//
// WHY IT RIDES THIS VERB (and adds no schedule of its own): `gopi tool shot` starts
// each headless chrome as a transient systemd USER unit, and its teardown is a
// `defer` — which a SIGKILLed shot (a test timeout, a cancelled run) never
// reaches, so the unit and its chrome descendants stay ACTIVE forever
// (measured 2026-09-23: 13 units, 4h55m). The unit's lifetime therefore has to
// be held somewhere that outlives the writer, and this verb is exactly that
// place: the host watchdog already runs it and its tick runs the same sweeps,
// so the fix needs no new daemon, no new tick and no new schedule. (Corrected
// 2026-10-30: this named the "resident worker/merger tick" as the second
// caller; there is none.)
//
// The policy lives in internal/scratch (which verdict keeps a unit; an
// unmeasurable creator is KEPT) and the teardown in internal/shot (the one
// systemctl argv): this call site decides nothing.
//
// A reclamation failure is REPORTED and leaves the exit status to the sweep
// above: the capture reclamation is a hygiene arm, and a registry this process
// cannot read must not turn a healthy sweep into a watchdog alarm.
func reclaimShotUnits(role string) int {
	return shot.ReclaimOrphanedUnitsReporting(role)
}

// runScratchGCTrees sweeps (or plans) the DECLARED TREES: the trees this
// repository's own commands create, fill and never clean.
//
// `selector` is the per-tree SELECTOR (`--tree NAME`), empty meaning every
// declared tree — the arm's behaviour before the selector existed. It is NOT
// resolved here: scratch.DeclaredTreesForName owns the name→rows mapping (the
// table is that package's), and this face only renders the refusal, so a name
// added to the table is selectable without editing the CLI (Rules 13/14). An
// unknown name is refused BEFORE anything is swept, in --dry-run and in a live
// pass alike — a typo must never read as "you swept nothing and it was clean".
//
// ONE line per SELECTED tree, on the shared scratchGCLogFmt shape extended with
// `tree=` and `bytes=` — the epic's "report the reclaimed bytes on the existing
// watchdog log line". A tree that is not configured on this host (GOTMPDIR
// unset, no raw directory yet) is reported with bytes=0 and its own reason on
// stderr rather than being silently dropped (Rule 15: a missing declared tree
// must LOOK missing, because the whole defect this epic closes was a tree nobody
// noticed).
//
// The root invariant is enforced inside internal/scratch
// (SweepDeclaredTree/removeCollectableOfTree refuse a declared root at any age);
// this face adds nothing to that rule, it only reports it.
//
// TWO FACES OF ONE DECISION, and the `explain` flag is now READ here (it used to
// be ignored on this arm — the flag reached the parser and nothing else). The
// plan face names what WOULD BE REMOVED; under `--explain` this arm ALSO names
// every entry it JUDGED and KEPT, with the sentence the decision point produced
// (scratch.DeclaredTreeVerdicts — the reporting twin of the same walk). Before
// that, a protected entry and an entry the arm never looked at printed the SAME
// thing: nothing.
//
// AND THE SUMMARY LINE IS UNCONDITIONAL, flag or not: "how much did this arm
// actually examine" is asked by a reader who did not pass `--explain`, and a
// report whose only output is a plan cannot answer the question "did you look at
// my tree at all?".
func runScratchGCTrees(dryRun, explain bool, selector string) {
	trees, err := scratch.DeclaredTreesForName(selector)
	if err != nil {
		refuseDeclaredTreeSelector(scratchGCTreesFlag, err)
	}
	for _, tree := range trees {
		// The window is the ROW's own constant — the same one every other arm
		// applies to this tree (see scratch.DeclaredTree's window()). It is read
		// here only to RENDER the stale= field; it is not passed to the plan, the
		// sweep or the verdict twin, because none of them accepts one any more.
		treeAge := tree.StaleAge
		if dryRun {
			plan, err := scratch.DeclaredTreePlan(tree)
			if err != nil {
				reportDeclaredTreeFailure(tree, err)
				continue
			}
			bytes := scratch.TreePlanTotal(plan)
			holding, holdingN := scratch.TreeHeldTotal(tree)
			probe := scratch.ProbeDeclaredTree(tree)
			// THE PLAN LINE IS THE PLAN FORMAT, not the live one (see
			// scratchGCTreesPlanLogFmt): len(plan) is what the pass WOULD remove
			// and is reported as `plan=`, because `removed=` on this verb means
			// "what this sweep deleted" — and this face deletes nothing.
			fmt.Printf(scratchGCTreesPlanLogFmt+" dry-run=true\n", scratchGCTreesRole, "", len(plan), treeAge, false, tree.Name, probe.Present, probe.Entries, bytes, holding, holdingN)
			for _, e := range plan {
				fmt.Printf("would-remove %s tree=%s age=%s bytes=%d\n",
					e.Path, tree.Name, e.Age.Truncate(scratchGCPlanAgePrecision), e.Size)
			}
			reportDeclaredTreeKeepVerdicts(tree, explain)
			continue
		}
		removed, bytes, err := scratch.SweepDeclaredTree(tree)
		if err != nil {
			reportDeclaredTreeFailure(tree, err)
			continue
		}
		// ONE line per tree, on the shared format: the tree's OWN age is the
		// threshold actually applied to it (stale=), and bytes= is what came
		// back. A reader never has to guess which window or which tree a line
		// refers to.
		//
		// present= and entries= are read AFTER the sweep on purpose: they state
		// what the tree IS now that the pass has finished, which is what says
		// the ROOT SURVIVED (present=true) and that the namespace predicate
		// matched something (entries>0). A tree the pass could not resolve
		// prints present=false, and the failure itself went to stderr — never
		// silently as a clean row (Rule 15).
		holding, holdingN := scratch.TreeHeldTotal(tree)
		probe := scratch.ProbeDeclaredTree(tree)
		fmt.Printf(scratchGCTreesLogFmt+"\n", scratchGCTreesRole, "", removed, treeAge, false, tree.Name, probe.Present, probe.Entries, bytes, holding, holdingN)
		reportDeclaredTreeKeepVerdicts(tree, explain)
	}
}

// reportDeclaredTreeKeepVerdicts is the trees arm's reporting face: one
// `scratch-verdict`-shaped KEEP line per judged entry under `--explain`, and the
// per-tree SUMMARY line ALWAYS.
//
// The verdicts come from internal/scratch's DeclaredTreeVerdicts — the SAME walk
// and the SAME predicate the plan and the sweep use, so a line the operator reads
// cannot promise (or protect) something the sweep would decide differently. This
// function renders; it decides nothing.
//
// A verdict is printed ONLY when `--explain` asked: on a host whose declared
// trees hold hundreds of thousands of entries (measured: 74,994 in the sessions
// tree alone), printing every keep by default would bury the plan the reader
// came for. The SUMMARY is not gated by the flag, because the question it
// answers — "was this tree looked at, and how much of it" — is exactly the one a
// reader with an empty plan cannot otherwise settle.
//
// THE VERDICT LINE IS THE SHARED ONE (scratchGCVerdictLabel, the same shape the
// default arm's `--explain` prints), so an operator's filter for
// `^scratch-verdict ` finds every arm's judgement rather than one arm's dialect.
func reportDeclaredTreeKeepVerdicts(tree scratch.DeclaredTree, explain bool) {
	verdicts, summary := scratch.DeclaredTreeVerdicts(tree)
	if explain {
		for _, v := range verdicts {
			verdict := scratchGCKeepVerdict
			keptMark := scratchGCKeepMark
			if v.Collectable {
				verdict = scratchGCCollectableVerdict
				keptMark = ""
			}
			fmt.Printf("%s %s verdict=%s age=%s %s %s%s\n",
				scratchGCVerdictLabel, v.Path, verdict, v.Age.Truncate(scratchGCPlanAgePrecision),
				scratch.OwnerEvidence(v), v.Reason, keptMark)
		}
		if len(summary.KeptReasons) > 0 {
			fmt.Printf("%s %s\n", scratchGCKeepReasonsLabel, scratchGCKeepReasonTally(summary.KeptReasons))
		}
	}
	fmt.Printf(scratchGCTreesSummaryLogFmt+"\n",
		summary.Name, summary.Examined, summary.NotOurs, summary.Collectable, summary.KeptTotal(), summary.Truncated)
}

// scratchGCKeepReasonTally renders a tree's keep-reason DISTRIBUTION in a stable
// order — the reasons sorted, each with its count, so two runs of the same tree
// print the same line and an operator can diff them. The map's iteration order is
// deliberately not relied on (a report whose lines reorder between runs cannot be
// compared).
func scratchGCKeepReasonTally(counts map[string]int) string {
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf("%d\u00d7 %s", counts[reason], reason))
	}
	return strings.Join(parts, "; ")
}

// runScratchGCFamilies reports the SESSIONS TREE's family census: one line per
// shape, naming the family and the decision, and a summary line — plus one
// report line per UNCLASSIFIED shape (Rule 15: a family the registry does not
// know is reported, never ignored).
//
// It is READ-ONLY by construction: it calls the scan, prints, and returns. The
// tree is never opened file-by-file, never written to, and the arm is not gated
// by the sweep's off switch — an operator who disabled the sweeps still needs to
// see what the tree holds.
//
// Exit status: 0 when every selected tree was censused, 1 when one could not be
// walked or the selector named no tree (a census that could not look must never
// read as a clean tree).
func runScratchGCFamilies(selector string, list bool) {
	if list {
		printCensusListsTree()
		return
	}
	trees, ok := resolveCensusTrees(selector)
	if !ok {
		fmt.Fprintf(os.Stderr, "gopi %s %s: no censused tree is named %q — known: %s (or %s)\n",
			CliVerbScratchGC, scratchGCFamiliesFlag, selector, strings.Join(censusTreeNames(), ", "), scratchGCFamiliesAllTrees)
		os.Exit(scratchGCExitError)
	}
	var (
		censused     int
		shapes       int
		unclassified int
		files        int
		dirs         int
		bytes        int64
		failed       bool
	)
	for _, tree := range trees {
		root, rerr := tree.root()
		if rerr != nil || root == "" {
			fmt.Fprintf(os.Stderr, "gopi %s %s: tree %s has no root on this host — nothing to census: %v\n",
				CliVerbScratchGC, scratchGCFamiliesFlag, tree.name, rerr)
			failed = true
			continue
		}
		scan, serr := tree.scan()
		if serr != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: could not walk tree %s at %s: %v\n",
				CliVerbScratchGC, scratchGCFamiliesFlag, tree.name, root, serr)
			failed = true
			continue
		}
		censused++
		shapes += len(scan.ByShape)
		unclassified += len(scan.Unclassified)
		files += scan.Files
		dirs += scan.Dirs
		bytes += scan.Bytes
		for _, shape := range sortedShapeKeys(scan) {
			entry := scan.ByShape[shape]
			family := entry.Family
			if family == "" {
				family = scratchGCFamiliesUnknownFamily
			}
			fmt.Printf(scratchGCFamiliesLogFmt+"\n",
				scratchGCFamiliesRole, tree.name, scan.Root, shape, family, string(entry.Decision), entry.Count, entry.Dir, entry.Bytes)
		}
		fmt.Printf(scratchGCFamiliesSummaryFmt+"\n",
			scratchGCFamiliesRole, tree.name, scan.Root, len(scan.ByShape), len(scan.Unclassified), scan.Files, scan.Dirs, scan.Bytes)
		for shape, n := range scan.UnclassifiedShapes() {
			fmt.Fprintf(os.Stderr, "gopi %s %s: UNCLASSIFIED shape %q — %d entries in %s (tree %s) have no family decision; register the family in internal/sessionlayout (bound/keep/legacy)\n",
				CliVerbScratchGC, scratchGCFamiliesFlag, shape, n, scan.Root, tree.name)
		}
	}
	fmt.Printf(scratchGCFamiliesReportFmt+"\n",
		scratchGCFamiliesRole, censused, shapes, unclassified, files, dirs, bytes)
	if failed {
		os.Exit(scratchGCExitError)
	}
}

// censusTree is ONE tree the census face can walk: the name a reader selects it
// by, its root, and a closure that performs the walk.
//
// It exists so the sessions tree and the profile's sibling trees are ONE table
// rather than two code paths: `--list` enumerates the same slice `--tree`
// selects from, and the printer reads ONE scan shape for both groups (a sibling
// scan is widened into the sessions scan's type next to the closure that
// produced it, never in the printer).
type censusTree struct {
	name string
	root func() (string, error)
	scan func() (sessionlayout.FamilyScan, error)
}

// censusTrees is the ONE table of censused trees: the sessions tree FIRST (the
// default arm, its subject before the sibling trees existed), then the gopi
// profile's sibling trees in the SIBLING REGISTRY's own order
// (sessionlayout.SiblingTrees — the reader of that order, not a copy of it: a
// fifth sibling tree landed there is censused here with no change to this file),
// and then the SCRATCH home's dump class trees (sessionlayout.ScratchDumpsTrees),
// whose family list is the SAME `dumpsFamilies` the profile dump rows carry.
//
// The two groups are enumerated from their own registries rather than merged
// into one, because they answer to different roots (the profile vs the scratch
// home) and the census must be able to report WHICH root it read: a name that
// resolved to "whichever happens to be configured" answers a question the caller
// did not ask.
func censusTrees() []censusTree {
	out := []censusTree{{
		name: scratchGCFamiliesSessionsTree,
		root: scratch.SessionRawLogRoot,
		scan: func() (sessionlayout.FamilyScan, error) {
			root, err := scratch.SessionRawLogRoot()
			if err != nil {
				return sessionlayout.FamilyScan{}, err
			}
			return sessionlayout.ScanFamilies(root)
		},
	}}
	for _, group := range [][]sessionlayout.SiblingTree{
		sessionlayout.SiblingTrees(),
		sessionlayout.ScratchDumpsTrees(),
	} {
		for _, tree := range group {
			tree := tree
			out = append(out, censusTree{
				name: tree.Name,
				root: tree.Resolve,
				scan: func() (sessionlayout.FamilyScan, error) {
					ss, err := sessionlayout.ScanSiblingTree(tree)
					if err != nil {
						return sessionlayout.FamilyScan{}, err
					}
					return widenSiblingScan(ss), nil
				},
			})
		}
	}
	return out
}

// widenSiblingScan adapts a sibling tree's census to the sessions tree's scan
// shape: the rollups and the totals are the same facts, so the printer reads one
// type for both groups.
func widenSiblingScan(ss sessionlayout.SiblingScan) sessionlayout.FamilyScan {
	return sessionlayout.FamilyScan{
		Root:         ss.Root,
		ByShape:      ss.ByShape,
		Unclassified: ss.Unclassified,
		Dirs:         ss.Dirs,
		Files:        ss.Files,
		Bytes:        ss.Bytes,
	}
}

// resolveCensusTrees turns the selector into the trees to walk: empty = the
// sessions tree alone (the unchanged default), `all` = every tree, a name = that
// tree. An UNKNOWN name returns ok=false, so the caller refuses it with the list
// of known ones — "no such tree" and "that tree holds nothing" must never render
// the same (Rule 15).
func resolveCensusTrees(selector string) ([]censusTree, bool) {
	all := censusTrees()
	switch selector {
	case "":
		return all[:1], true
	case scratchGCFamiliesAllTrees:
		return all, true
	}
	for _, t := range all {
		if t.name == selector {
			return []censusTree{t}, true
		}
	}
	return nil, false
}

// printCensusListsTree enumerates the trees this arm can census: the name a
// reader selects each one by, and where its root is — including the trees whose
// root cannot be resolved on this host, because a name a reader may select must
// be listed either way (what is absent is the TREE, not the name).
func printCensusListsTree() {
	for _, t := range censusTrees() {
		root, err := t.root()
		switch {
		case err != nil:
			fmt.Printf("%s root=<unresolved: %v>\n", t.name, err)
		case root == "":
			fmt.Printf("%s root=<none on this host>\n", t.name)
		default:
			fmt.Printf("%s root=%s\n", t.name, root)
		}
	}
}

// censusTreeNames returns the selector names in the table's own order — the list
// an unknown selector is refused with, and the same list `--list` prints.
func censusTreeNames() []string {
	trees := censusTrees()
	out := make([]string, 0, len(trees))
	for _, t := range trees {
		out = append(out, t.name)
	}
	return out
}

// sortedShapeKeys returns a scan's shape keys in a stable order, so one census
// is diffable against the next (a map iteration order is not).
func sortedShapeKeys(scan sessionlayout.FamilyScan) []string {
	out := make([]string, 0, len(scan.ByShape))
	for shape := range scan.ByShape {
		out = append(out, shape)
	}
	sort.Strings(out)
	return out
}

// refuseDeclaredTreeSelector renders the FAIL-CLOSED refusal of a `--tree`
// value that names no declared tree, and exits non-zero.
//
// WHY THE CLIENT REFUSES RATHER THAN WARNING (Rule 15). The selector bounds a
// pass that DELETES. A warning would let the pass proceed — with the selection
// silently degraded to "nothing" (a clean-looking no-op the operator reads as
// "that tree was already clean") or, worse if a future default widens, to
// "everything". Refusing before any tree is touched, in --dry-run too, is what
// makes the mistake visible without a destructive run to notice it with.
//
// The message names the FLAG SPELLING, the value, and every valid name in the
// table's own order — the enumeration is scratch.DeclaredTreeNames(), so it
// cannot drift from the set the resolver accepts.
func refuseDeclaredTreeSelector(arm string, err error) {
	fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, arm, err)
	fmt.Fprintf(os.Stderr, "  pass %s NAME — valid names: %s (or %s for every declared tree)\n",
		scratch.DeclaredTreeFlagSpelling, strings.Join(scratch.DeclaredTreeNames(), ", "), scratch.DeclaredTreeSelectorAll)
	fmt.Fprintf(os.Stderr, "  `gopi %s %s %s` reports what each declared tree holds right now\n",
		CliVerbTool, CliVerbScratchGC, scratchGCTreesFlag+dryRunRefusalHint)
	os.Exit(scratchGCExitError)
}

// dryRunRefusalHint is the tail the refusal appends to the tree-selector hint
// above: the arm spelling that reports without deleting. It is a NAMED constant
// because it is a CLI SPELLING (Rule 1), and it is composed from the flag the
// parser actually accepts rather than re-typed.
const dryRunRefusalHint = " " + scratchGCDryRunFlag

// reportDeclaredTreeFailure renders one tree's outcome when the sweep could not
// run for it, and decides whether that is an ALARM.
//
// TWO DIFFERENT FACTS, deliberately not collapsed (Rule 15: an unmeasured fact
// must never render as a measured negative):
//
//   - the tree is NOT CONFIGURED on this host (GOTMPDIR unset, no raw directory
//     yet): there is nothing to bound, so the line says so and the exit status
//     stays 0. Alarming every 30 minutes for a tree that was never created would
//     train the operator to ignore the watchdog — the one failure mode this
//     family of scripts exists to avoid.
//   - the tree EXISTS but could not be measured/read: that IS an alarm (exit
//     non-zero), because silence would mean an unbounded tree nobody reports.
func reportDeclaredTreeFailure(tree scratch.DeclaredTree, err error) {
	// holding= stays 0 HERE and the zero is honest rather than convenient: this
	// arm is reached precisely because the tree could not be read or measured,
	// so any figure printed would be invented. Rule 15's other half — an
	// unmeasurable entry is reported as unmeasurable, never as empty.
	//
	// entries= is 0 for the same reason, and present= is the MEASURED probe
	// rather than a constant: a tree that could not be SWEPT may still exist,
	// and saying so is exactly the distinction this arm exists to make (a row
	// that printed nothing at all was how "not configured" came to read as
	// "clean and empty").
	probe := scratch.ProbeDeclaredTree(tree)
	fmt.Printf(scratchGCTreesLogFmt+"\n", scratchGCTreesRole, "", 0, tree.StaleAge, false, tree.Name,
		probe.Present, 0, int64(0), int64(0), 0)
	if errors.Is(err, scratch.ErrDeclaredTreeUnconfigured) {
		fmt.Fprintf(os.Stderr, "gopi %s %s: declared tree %s is not configured on this host — nothing to sweep: %v\n",
			CliVerbScratchGC, scratchGCTreesFlag, tree.Name, err)
		return
	}
	fmt.Fprintf(os.Stderr, "gopi %s %s: declared tree %s could not be swept: %v\n",
		CliVerbScratchGC, scratchGCTreesFlag, tree.Name, err)
	os.Exit(scratchGCExitError)
}

// runScratchGCBase is the memory-base arm: the sweep (or the plan) of the
// resolved base for THIS SYSTEM'S OWN ad-hoc residue.
//
// Every refusal inside scratch.SweepBase / scratch.BaseResiduePlan is reported
// with a non-zero exit (Rule 15): a base that shares the filesystem of `/`, a
// base that could not be read, a base with more entries than the scan limit —
// the watchdog must see each of them rather than a silent "removed 0".
func runScratchGCBase(dryRun, explain bool) {
	base := scratch.ResolveBase()
	if dryRun {
		report, err := scratch.BaseResiduePlan(base, scratch.ResolveStaleAge())
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: plan for %s failed: %v\n", CliVerbScratchGC, scratchGCBaseFlag, base, err)
			os.Exit(scratchGCExitError)
		}
		fmt.Printf(scratchGCBaseLogFmt+" dry-run=true\n", scratchGCBaseRole, base, len(report), scratch.ResolveStaleAge(), false)
		for _, p := range report {
			fmt.Printf("would-remove %s age=%s\n", p.Path, p.Age.Truncate(scratchGCPlanAgePrecision))
		}
		if explain {
			reportScratchGCBaseVerdicts(base)
		}
		return
	}
	// The verdict face runs BEFORE the delete, on the same clock and the same
	// predicate, so a removal count can be read against the set it was taken
	// from (mirroring the home arm — see reportScratchGCVerdicts).
	if explain {
		reportScratchGCBaseVerdicts(base)
	}
	removed, err := scratch.SweepBase(base, scratch.ResolveStaleAge())
	if err != nil {
		fmt.Printf(scratchGCBaseLogFmt+"\n", scratchGCBaseRole, base, removed, scratch.ResolveStaleAge(), false)
		fmt.Fprintf(os.Stderr, "gopi %s %s: sweep of %s failed: %v\n", CliVerbScratchGC, scratchGCBaseFlag, base, err)
		os.Exit(scratchGCExitError)
	}
	fmt.Printf(scratchGCBaseLogFmt+"\n", scratchGCBaseRole, base, removed, scratch.ResolveStaleAge(), false)

}

// reportScratchGCBaseVerdicts is the base arm's --explain face: ONE line per
// entry of the resolved base whose NAME is in this system's namespace, saying
// whether it may be collected and the sentence that decided it.
//
// WHY THIS FACE WAS MISSING AND HAD TO EXIST (measured 2026-10-01): the arm's
// candidates were once directories only, so a 28 MB compiler output sitting in
// /dev/shm under a name this system wrote (`gopi-red2`, `gopi-after-01a0e8c7`)
// was neither swept NOR mentioned: the verb printed `removed=0` and --explain
// printed one line — the summary. A zero it could not explain, on exactly the
// residue the operator was looking at. The line this function prints turns that
// silence into the sentence "in the namespace, refused because X", which is the
// difference Rule 15 asks for.
func reportScratchGCBaseVerdicts(base string) {
	verdicts, err := scratch.BaseResidueVerdicts(base, scratch.ResolveStaleAge())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s %s: verdicts for %s failed: %v\n", CliVerbScratchGC, scratchGCBaseFlag, base, err)
		os.Exit(scratchGCExitError)
	}
	// The LEGACY BARE-KEY build-cache family's verdicts (`<base>/<16-hex>/gocache`)
	// ride the same face. They have to: their names carry neither a family prefix
	// nor a dash, so the generic namespace walk above never reaches them — and a
	// family the sweep collects while --explain stays silent about it is exactly
	// the "a zero it could not explain" defect this function exists to remove.
	legacy, lerr := scratch.BaseResidueLegacyGoCacheVerdicts(base, scratch.ResolveStaleAge())
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "gopi %s %s: legacy build-cache verdicts for %s failed: %v\n",
			CliVerbScratchGC, scratchGCBaseFlag, base, lerr)
		os.Exit(scratchGCExitError)
	}
	verdicts = append(verdicts, legacy...)
	collectable := 0
	for _, v := range verdicts {
		verdict := scratchGCKeepVerdict
		keptMark := scratchGCKeepMark
		if v.Collectable {
			verdict = scratchGCCollectableVerdict
			keptMark = ""
			collectable++
		}
		fmt.Printf("%s %s verdict=%s age=%s %s\n",
			scratchGCVerdictLabel, v.Path, verdict, v.Age.Truncate(scratchGCPlanAgePrecision),
			v.Reason+keptMark)
	}
	fmt.Fprintf(os.Stderr, "gopi %s %s: %d candidate(s) judged, %d collectable\n",
		CliVerbScratchGC, scratchGCBaseFlag, len(verdicts), collectable)
}

// reportScratchGCDryRun lists the entries the real sweep would collect —
// same predicates (ownership + age + holder), same order of checks, no
// delete and no lock. A missing home is reported as such, not as an error.
func reportScratchGCDryRun(home string, explain bool) {
	report, err := scratch.SweepHomePlan(home, scratch.ResolveStaleAge())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: plan for %s failed: %v\n", CliVerbScratchGC, home, err)
		os.Exit(scratchGCExitError)
	}
	fmt.Printf(scratchGCLogFmt+" dry-run=true\n", scratchGCRoleCLI, home, len(report), scratch.ResolveStaleAge(), false)
	for _, p := range report {
		fmt.Printf("would-remove %s age=%s\n", p.Path, p.Age.Truncate(scratchGCPlanAgePrecision))
	}
	if explain {
		reportScratchGCVerdicts(home)
	}
	reportShotUnitPlan()
}

// reportScratchGCVerdicts prints ONE line per candidate: whether it may be
// collected and why. It shares the sweep's own predicate
// (scratch.SweepHomeVerdicts), so the report can never promise something the real
// sweep refuses — and it deletes nothing (a report must have no side effects).
//
// The line carries the facts an operator needs to act:
//
//	scratch-verdict <path> verdict=collectable|keep
//	                       age=<d>              how old the entry already is
//	                       outcome=<...> owner=pid:<n>/start:<ticks>
//	                                            the recorded owner the decision was
//	                                            measured against (unregistered when
//	                                            no writer recorded one)
//	                       <reason>             the sentence that decided it
func reportScratchGCVerdicts(home string) {
	verdicts, err := scratch.SweepHomeVerdicts(home, scratch.ResolveStaleAge())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: verdicts for %s failed: %v\n", CliVerbScratchGC, home, err)
		os.Exit(scratchGCExitError)
	}
	collectable := 0
	for _, v := range verdicts {
		verdict := scratchGCKeepVerdict
		keptMark := scratchGCKeepMark
		if v.Collectable {
			verdict = scratchGCCollectableVerdict
			keptMark = ""
			collectable++
		}
		fmt.Printf("%s %s verdict=%s age=%s %s %s%s\n",
			scratchGCVerdictLabel, v.Path, verdict, v.Age.Truncate(scratchGCPlanAgePrecision),
			scratch.OwnerEvidence(v), v.Reason, keptMark)
	}
	fmt.Fprintf(os.Stderr, "gopi %s: %d candidate(s) judged, %d collectable\n",
		CliVerbScratchGC, len(verdicts), collectable)
}

// The verdict line's fixed vocabulary: the label a reader greps for, and the two
// decisions a candidate can get. Constants (Rule 1b) because the tests, an
// operator's log filters and the usage text all quote them.
const (
	scratchGCVerdictLabel       = "scratch-verdict"
	scratchGCCollectableVerdict = "collectable"
	scratchGCKeepVerdict        = "keep"
	scratchGCKeepMark           = " [kept]"
)

// reportShotUnitPlan is the dry-run face of the capture-unit reclamation: the
// SAME predicate (internal/scratch.PlanShotUnits) reported instead of acted on,
// so an operator vetting the watchdog sees the orphaned captures before anything
// is stopped. A registry this process cannot read is REPORTED and adds no line
// beyond the report's own (Rule 15: an unmeasured fact must not render as a
// measured negative).
func reportShotUnitPlan() {
	plans, err := shot.PlanOrphanedUnits(shot.DefaultProcRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: capture unit registry unavailable: %v\n", CliVerbScratchGC, err)
		return
	}
	for _, p := range plans {
		verdict := scratch.ShotUnitWouldStopWord
		if p.Keep {
			verdict = scratch.ShotUnitKeepWord
		}
		fmt.Printf("%s %s verdict=%s reason=%q\n", verdict, p.Unit, p.Verdict, p.Reason)
	}
}

// scratchGCExitError is the watchdog-visible failure status (a named constant
// so the timer, the tests and the usage text cannot drift apart).
const scratchGCExitError = 1

// --- the sandbox arm ---------------------------------------------------------

// sandboxRootForSweep resolves the sandbox root through the ONE resolver, so the
// sweep, the initializer and `gopi tool sandbox show` can never disagree about where
// the sandbox tree is (Rule 2/14). The settings read is the CLI layer's job (it
// owns the file read, exactly as the sandbox verb does); a settings file that
// cannot be parsed is reported, because a `sandbox_dir` key we did not see must
// not silently become "sweep the default".
func sandboxRootForSweep() (root, source string) {
	var cfg *config.Config
	if path := config.ResolveSettingsPath(); path != "" {
		if loaded, err := config.FromSettingsFile(path); err == nil {
			cfg = loaded
		} else if config.SandboxRootFromEnv() == "" {
			fmt.Fprintf(os.Stderr, "gopi %s: warning: could not read %s (%v) — the settings key %q is not in force\n",
				CliVerbScratchGC, path, err, config.SandboxSettingsKey)
		}
	}
	return config.ResolveSandboxRoot(cfg, nil)
}

// runScratchGCSandboxes sweeps (or plans) the agent sandbox root.
//
// Bounded and fail-open like every other arm: an unreadable root, a root that is
// refused (the filesystem root, the user's home, the gopi profile), an
// unverifiable owner — each is REPORTED and resolves to "nothing removed", never
// to a delete. The sandbox predicate lives in internal/scratch (SweepSandboxes /
// PlanSandboxes), so this face decides nothing about who may be collected.
func runScratchGCSandboxes(dryRun bool) {
	root, source := sandboxRootForSweep()
	fmt.Fprintf(os.Stderr, "gopi %s %s: root=%s source=%s stale=%s\n",
		CliVerbScratchGC, scratchGCSandboxesFlag, root, source, scratch.SandboxStaleAge)

	if dryRun {
		plans, err := scratch.PlanSandboxes(root, scratch.SandboxStaleAge)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi %s %s: plan for %s failed: %v\n",
				CliVerbScratchGC, scratchGCSandboxesFlag, root, err)
			os.Exit(scratchGCExitError)
		}
		removable := 0
		for _, p := range plans {
			if !p.Keep {
				removable++
			}
		}
		fmt.Printf(scratchGCSandboxesLogFmt+" dry-run=true\n",
			scratchGCSandboxesRole, root, removable, scratch.SandboxStaleAge, false, int64(0))
		for _, p := range plans {
			verdict := "keep"
			if !p.Keep {
				verdict = "would-remove"
			}
			fmt.Printf("%s %s reason=%q\n", verdict, p.Path, p.Reason)
		}
		return
	}

	// The refusal guard runs BEFORE the sweep: a mistyped root must be named,
	// not swept. A refused root is a configuration error worth a non-zero exit
	// (the watchdog's log is the only place an operator would see it).
	if err := scratch.SandboxRootRefusal(root); err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s %s: %v\n", CliVerbScratchGC, scratchGCSandboxesFlag, err)
		os.Exit(scratchGCExitError)
	}
	removed, bytes, lockVerdict, err := scratch.SweepSandboxesDetailed(root, scratch.SandboxStaleAge)
	if err != nil {
		fmt.Println(scratchGCLockLine(fmt.Sprintf(scratchGCSandboxesLogFmt, scratchGCSandboxesRole, root, removed, scratch.SandboxStaleAge, false, bytes), lockVerdict))
		fmt.Fprintf(os.Stderr, "gopi %s %s: sweep of %s failed: %v\n",
			CliVerbScratchGC, scratchGCSandboxesFlag, root, err)
		os.Exit(scratchGCExitError)
	}
	reportScratchGCLock(scratchGCSandboxesRole, root, lockVerdict)
	fmt.Println(scratchGCLockLine(fmt.Sprintf(scratchGCSandboxesLogFmt, scratchGCSandboxesRole, root, removed, scratch.SandboxStaleAge, false, bytes), lockVerdict))
}
