package standardgate

import (
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/tools"
)

// removal_audit_shell_gate.go — the TEXT half of the removal-audit census.
//
// WHY THIS HALF EXISTS. removal_audit_gate_test.go walks the production Go tree
// with an AST and makes every `worktree + remove` / `branch + -d|-D` call owe an
// audit record. That predicate is language-bound, so the SAME destructive verb
// written in a TRACKED SHELL SCRIPT is invisible to it — and it was: measured
// 2026-10-15, scripts/post-merge-cleanup.sh ran `git branch $DEL_FLAG "$BRANCH"`
// against a private fixture repo and the audit sink was never created (the ref
// was gone, and no line named the arm that removed it). The Go half was green
// the whole time. A census whose reach stops at the file extension is a census
// an author steps around by typing the verb into a shell script — which is
// exactly what happened, and one language over from the 2026-10-12 sequel
// (`update-ref -d`, a deletion the `branch -d|-D` predicate could not see).
//
// THE PREDICATE IS THE GO HALF'S, SPELLED FOR TEXT. A line is a REF REMOVAL SITE
// when it invokes one of git's three deleting verbs on a ref:
//
//	git branch  -d|-D|--delete            remove a local branch
//	git update-ref -d|--delete <ref>      rewrite a ref directly
//	git push --delete | <remote> :refs/heads/<b>   remove a REMOTE branch
//
// The third form is new with this half and is not a widening for its own sake:
// a remote branch deletion is the same destruction one host away, the Go half
// cannot see it (it is a push vector, not a `branch` vector), and the tree
// already routes its remote deletions through the same arms. The second form is
// the shape the 2026-10-12 sequel measured.
//
// THE REMEDY IS THE ONE WRITER. `gopi task archive <branch> --reason <code>`
// writes the archive tag AND its companion reason ref AND (through its own
// reclaim leg) the ref removal — and the reclaim leg is the arm that emits the
// `kind=branch-delete` record. So a script that wishes a ref gone does not drop
// to `git branch -d`: it calls the writer, which is the only path that leaves
// the removal attributable (MEASURED on this host: the funnel archives the
// branch, removes the ref and appends an `arm=archive-reclaim-branch` record to
// GOPI_REMOVAL_AUDIT_LOG, while the raw delete removes the ref and writes
// nothing). The funnel is named here by its VERB; the companion ref's path is
// deliberately not spelled (the writer owns that spelling, and a second copy in
// a gate could only drift — Rule 13/14).
//
// SCOPE — the carriers are the trees a HAND-RUN or a PROSE instruction reaches
// the shell from, and the narrowness is the design (it mirrors the archive-mint
// gate, which was funnelled through the same four carriers):
//
//   - scripts/ — the tracked hand-run shell tree;
//   - GOPI.md — the instruction surface every agent reads (Rule 9 is where the
//     bare deletion was TAUGHT);
//   - the PRODUCTION Go trees internal/ + cmd/ — a Go literal can render a shell
//     command for an operator to run;
//   - the CARRIED devdocs — see removalShellDevdocCarriers below, which is the
//     arm this corpus was widened for and the one whose floor is a NAMED LIST
//     rather than a tree.
//
// tasks/ and briefings/ are OUT: they are HISTORY and must stay able to QUOTE
// the pre-fix command (a record that cannot print its own subject is not a
// record), and the remainder of devdoc/ is out for the same reason. Test
// fixtures are out for the same reason. A `#`/`//` comment is
// prose, never a removal site: scripts/safe-merge.sh documents `git branch -d`
// (as a CAUSE of the branch it is asked about) and must not be reported for it,
// so the shell half strips a `#` line and the Go half strips a `//` suffix. The
// Go half reads comments out of the picture with the archive-mint gate's own
// helper, so the two text gates agree on what "a comment" is.
//
// WHAT THIS HALF DOES NOT COVER (Rule 15: stated, not implied, and MEASURED
// against this tree rather than asserted):
//
//   - an `eval`-assembled or string-split command is invisible to any text scan;
//   - the devdoc corpus is NOT scanned as a tree, and the split is a PRINCIPLE
//     rather than an accident: a doc that RECORDS is not a doc that PRESCRIBES.
//     A record must stay able to print the command it ran — otherwise the
//     census's own document (whose subject IS the raw verb), a run's refusal
//     transcript (`error: Cannot delete branch … checked out at`), and a
//     past measurement of git's own behaviour could not exist. So what is IN is
//     the INSTRUCTION SURFACE — the text a child is handed and told to act on —
//     and it is the NAMED list removalShellDevdocCarriers spells, not a
//     class-derived filter (a class filter is vacuous here, see that block).
//
//     THE NUMBERS, MEASURED AT THIS TREE (so the sentence cannot go stale
//     silently). The counting command is
//
//         git grep -nE 'git branch -[dD] ' -- 'devdoc/*.md' 'devdoc/**/*.md'
//
//     and it returns 69 hits across 28 devdocs (this change's own devdoc is
//     among them, quoting the verb it is about): 8 of those docs carry a
//     `normative` row in devdoc/DOC-INDEX.tsv and 20 are `historical`. Not one
//     of the hits is a disposition a reader is told to take — every one quotes
//     (a run's transcript, a refusal, a `$ ` prompt in a record, the census of
//     the defect) — and the four files whose lines DID read as a disposition,
//     i.e. a table or an ordered step list a child follows, are the carriers
//     this half reads: they are clean as they ship (0 hits each), and the red
//     proof drives one of them red on the real file.
//
//     WHY THE LIST IS NAMED AND NOT INHERITED. The obvious widening — "every
//     devdoc whose DOC-INDEX class is normative" — was measured and REJECTED: it
//     admits 700+ files, including the census's own record (whose subject IS the
//     raw verb) and a 900-line orphan-disposition log, and a gate that fires on
//     the document that describes it gets switched off. It is also VACUOUS as a
//     corpus filter: DOC-INDEX.tsv classifies a doc from a reference the gate
//     would have to re-implement (Rule 14 — one definition), and a filter that
//     silently reads an empty file set still reports green. A named list cannot
//     widen by accident, cannot be vacuous (the floor AND the sentinel by name
//     are both checked), and each member is IN for a stated reason.
//
//     THE REMEDY FOR A HIT HERE IS THE FUNNEL, NOT AN EDIT OF A MEASUREMENT.
//     Two carriers were a LIVE prescription and this change funnelled them
//     (devdoc/rules-reference.md's Commit Path step 6 and its Rule 9 step 2).
//     The others are a run's record of a ruling — devdoc/branch-lifecycle-
//     ruling.md is cited by name in cmd/agent/merge_prompt.md:742 as standing
//     authority for the merger's lifecycle, which is why it is a carrier — and
//     funnelling those means rewriting the row while KEEPING the measurement it
//     carries, which the red proof and the gate's file:line make visible.
//
// The LLM INSTRUCTION files were the other gap and are now a carrier rather than
// a paragraph here — see removalShellInstructionRoot; that defect was measured
// (2026-10-16: a merge run's child read cmd/agent/merge_prompt.md's disposition
// table, ran a raw branch deletion, and no host recorded a branch-delete).
//
// NO BASELINE, BY CONSTRUCTION: a permitted silent ref removal is the defect
// this half exists to stop. The fix for a hit is the FUNNEL, never a permit; the
// only valve is the reviewed inline escape, which keeps the site visible in the
// data report.

// The gate's identity. The id names the RULE, and the probe is the red proof
// (a gate with no probe is a gate nobody has driven RED — see gate.go's
// TestStandardGates).
func removalShellGate() gate {
	return gate{
		id:             "removal-audit (a ref removal written in a shell script or an instruction must route through the ONE archive writer)",
		checklistRow:   "5.3",
		probe:          "TestRemovalAuditGateShellHalfRedProof",
		scanTextRoot:   textScanRoot,
		textCorpus:     removalShellTextCorpus,
		escape:         removalAuditGateEscape,
		baselineReason: "EMPTY by construction: a ref removal that leaves no audit record is the defect this half exists to stop, so there is nothing to permit. The three measured carriers were FUNNELLED through the one writer rather than permitted — scripts/post-merge-cleanup.sh and GOPI.md Rule 9.",
		baseline:       removalShellBaseline,
		find:           removalShellFind,
	}
}

// removalShellBaseline is empty BY CONSTRUCTION. An entry here would be a
// permanent exemption for an unaudited destruction, so the fix for a hit is the
// funnel (`gopi task archive`), never a permit.
var removalShellBaseline []string

// The vocabulary the predicate reads. These are git's own spellings and the
// shells' quoting, not our identifiers: they are data, matched as data (the same
// posture archiveMintFind takes for `git` + `tag`).
const (
	removalShellVerbBranch     = "branch"
	removalShellVerbUpdateRef  = "update-ref"
	removalShellVerbPush       = "push"
	removalShellFlagPrefix     = "-"
	removalShellLongFlagPrefix = "--"
	removalShellCommentPrefix  = "#"
	removalShellMarkdownQuote  = "`"
	removalShellRefspecPrefix  = ":"
	removalShellHeadsRef       = "refs/heads/"

	// The shell ASSIGNMENT syntax, read for one reason: the measured site wrote
	// the flag into a VARIABLE (`DEL_FLAG="-d"`, upgraded to `-D` under --force)
	// and then ran `git branch $DEL_FLAG "$BRANCH"` — so a predicate that only
	// accepted a literal flag token would have read the very line this half was
	// written from as clean. It is the text half's counterpart of the Go half's
	// constant resolution (removalAuditConsts): a spelling carried by a name is
	// still that spelling.
	removalShellAssignOp = "="
	removalShellVarSigil = "$"
	removalShellVarOpen  = "{"
	removalShellVarClose = "}"
	removalShellQuote    = "\""
	removalShellQuoteAlt = "'"
)

// The carriers, spelled as named prefixes (Rule 1) because the corpus filter and
// the non-vacuity floor must agree about them. The scripts prefix KEEPS its
// trailing separator (it is archiveMintScriptsRoot): a bare-"scripts" prefix
// would also match a sibling tree named `scripts-extra`, and a carrier set is
// exactly the kind of list that must not widen by accident.
const (
	removalShellScriptsPrefix = archiveMintScriptsRoot
	removalShellScriptsDir    = archiveMintScanRoot
	removalShellGoRoot        = archiveMintGoRoot
	removalShellCmdRoot       = archiveMintCmdRoot
	removalShellGoSuffix      = archiveMintGoSuffix
	removalShellTestSuffix    = archiveMintTestFileSuffix

	// removalShellInstructionRoot / removalShellInstructionSuffix select the
	// LLM INSTRUCTION files a gopi process hands to a CHILD: markdown under
	// cmd/agent/ that tells a spawned gopi what to run. They are carriers for
	// the same reason GOPI.md is — a command written in prose is a command a
	// child will run — and they were OUT of this half only because the census
	// predated the measured case: MEASURED 2026-10-16, a merge run's child read
	// cmd/agent/merge_prompt.md's Merger disposition table, ran the raw
	// deletion its action column prescribed, and no host recorded a
	// `kind=branch-delete` for that ref (the merge `41679b36e` was in master;
	// the sink held 0 branch-delete records for the branch on all three hosts,
	// while a same-family row that went through a production close-out arm had
	// two sha-bearing records). A prose carrier is exactly as load-bearing as a
	// script one, so the scope is stated as a tree+suffix pair (Rule 1) and the
	// floor below counts it.
	removalShellInstructionRoot   = "cmd/agent/"
	removalShellInstructionSuffix = ".md"

	// removalShellPADutyRel is the production Go file that RENDERS the console's
	// own refusal of the destructive ref move into the prompt a chat run is
	// given. It is a carrier — a command written in prose is a command a child
	// will run — and it is named here because it is the SECOND measured false
	// positive of 2026-11-05: the sentence refusing the family lists the family,
	// and a reading that takes one member of a list for an instruction reports
	// the refusal itself.
	removalShellPADutyRel = "internal/agent/chat_pa_duty.go"

	// The devdoc CARRIER SET — the arm this corpus was widened for, and the list
	// is NAMED for the reason above: a normative-class filter would be vacuous
	// (Rule 14) and a tree-shaped prefix would admit the census's own record.
	//
	// WHAT IS IN, and why each member is not incidental:
	//
	//   - mergerTakeoverPromptDoc is cited BY SECTION (§3.6.1) from
	//     cmd/agent/merge_prompt.md. The merger's prompt delegates its whole
	//     branch-lifecycle disposition table to that section, so the doc's rows
	//     ARE the merger's instructions — a child that reads `git branch -d
	//     <branch>` in a doc its prompt names by section runs it, which is the
	//     2026-10-16 defect one level of indirection deeper;
	//   - mergeBacklogTerminalGuardDoc / branchLifecycleRulingDoc are the two
	//     docs the tree's own merger machinery points at: the guard's
	//     disposition sentence and merge_prompt.md:742's "is quoted in …" cite
	//     them as standing authority;
	//   - rulesReferenceDoc is the DOC-INDEX-classified home of the Commit Path
	//     list that PROJECT.md Rule 5's Tier A/B split moved out of GOPI.md — i.e.
	//     the exact text this change funnelled, living one file over from the
	//     carrier that teaches it.
	removalShellDevdocPrefix = "devdoc/"
	removalShellDevdocSuffix = ".md"
)

// removalShellDevdocCarriers is the devdoc carrier set, spelled ONCE here so the
// corpus filter, the non-vacuity floor and the red-proof splice cannot disagree
// about the file set (Rule 14).
//
// It is a LIST rather than a predicate because the honest predicate is not
// decidable by a text scan: devdoc/DOC-INDEX.tsv is the only place a doc's class
// is recorded, and re-deriving it here would be a second definition of the
// corpus (Rule 14) and a second definition of "normative" (Rule 13). A list
// cannot widen by accident; the floor below is what keeps it from being vacuous.
var removalShellDevdocCarriers = []string{
	removalShellDevdocPrefix + "merger-takeover-branch-lifecycle.md",
	removalShellDevdocPrefix + "merge-backlog-terminal-guard.md",
	removalShellDevdocPrefix + "branch-lifecycle-ruling.md",
	removalShellDevdocPrefix + "rules-reference.md",
}

// removalShellDevdocSentinel is the carrier the epic names FIRST — the merger's
// own lifecycle rule set, which cmd/agent/merge_prompt.md cites by section
// (§3.6.1). It is a CONSTANT (Rule 1b) so the file the corpus is supposed to
// reach and the file the red proof splices cannot be two different names.
const removalShellDevdocSentinel = removalShellDevdocPrefix + "merger-takeover-branch-lifecycle.md"

// removalShellDevdocRoutedRow / removalShellDevdocRawRow are the same
// disposition-table line in its two states. The routed row is what the sentinel
// must ship (the ONE writer, in the action cell the merger's child reads); the
// raw row is what it shipped before and what the proof splices back. Both are
// spelled once, and both are used — the green assertion reads the shipped text
// and the red splice puts the other in its place, so a proof cannot pass by
// comparing a file to itself.
//
// The REASON CODE comes from the archive reason vocabulary's own constant, not
// from a literal here (Rule 1b/13/14): merge_prompt.md's action cells name the
// same code, so a child following the doc and a reader following the prompt
// arrive at the same verb with the same reason, and a rename of the code cannot
// leave this half teaching the old spelling.
// The RAW row is assembled rather than typed, because this file is itself a
// CARRIER: the gate reads the production Go trees, so a literal raw deletion
// written here would be reported as a site in the very gate that defines it
// (MEASURED by TestStandardGates on the first draft of this change, at this
// file's own line). The pieces come from the gate's own vocabulary constants
// (Rule 13/14), so the splice string cannot drift from what the predicate
// detects — if the predicate ever stops reporting this shape, the red proof
// fails rather than passing vacuously.
const (
	removalShellDevdocRoutedRow = "| `gopi task archive <branch> --reason " +
		tools.ArchiveReasonSuperseded + "` |"
	removalShellDevdocRawRow = "| `" + archiveMintGitToken + " " + removalShellVerbBranch + " " +
		removalShellFlagPrefix + removalShellBranchDeleteFlagShort + " <branch>` |"
)

// removalShellMinDevdocCarriers is the non-vacuity FLOOR for the devdoc arm
// (Rule 15: a corpus that widened to nothing must not read as a green). It is
// the size of the set as shipped and the test additionally asserts the SENTINEL
// by name, because a count cannot express "the file this half was widened for"
// — the same posture removalShellMinInstructionFiles states.
const removalShellMinDevdocCarriers = 4

// removalAuditGateEscape is the inline marker that releases ONE site, with its
// reason in the same comment block (Rule 13's reviewed exception, kept visible
// in the data report).
//
// IT LIVES IN A PRODUCTION FILE BECAUSE BOTH HALVES SHARE IT. The Go half
// (removal_audit_gate_test.go) reads it through the framework's `escape` field
// and this half's `find` is reached from a production path, so a marker declared
// in a `_test.go` file is unreachable to one of its two consumers — the compiler
// says so (`undefined: removalAuditGateEscape` in `go build ./...`) while the
// test build, which sees both files, stays quiet. ONE definition, both halves
// (Rule 14).
const removalAuditGateEscape = "removal-audit-ok:"

// removalShellMinScannedFiles is the non-vacuity FLOOR for the text walk: a
// broken root or a corpus filter that matches nothing would otherwise report a
// clean green while reading nothing (the failure mode archiveMintMinScripts
// names). Set well below the measured corpus (>=200 files under scripts/ alone,
// plus GOPI.md and the production Go trees) so ordinary churn cannot trip it.
const removalShellMinScannedFiles = 50

// removalShellShippedInstructionFiles is the number of INSTRUCTION carriers this
// tree ships — and it is ZERO, measured, since 2026-11-01.
//
// The arm this constant now feeds was written FOR a measured defect (2026-10-16:
// a merge child read cmd/agent/merge_prompt.md's disposition table and ran the
// raw deletion its action column prescribed, leaving no branch-delete record on
// any host). That file was the one-shot merge verb's own prompt, and it was
// deleted with the verb (epic merge-verb-removal, item 3): the child it spoke to
// no longer exists in any spelling.
//
// WHY A DECLARED COUNT AND NOT A FLOOR OF 0: a floor of 0 passes when the walk
// reads nothing, which is precisely the vacuity this half was widened to close.
// The test asserts EQUALITY in BOTH directions, so (a) a future instruction file
// re-opens the surface and turns the assertion into the live arm it was written
// as — the number to raise is this one, and the comment here says so — while (b)
// a tree whose carriers the walk cannot see disagrees with the constant and goes
// red rather than quietly covering nothing.
const removalShellShippedInstructionFiles = 0

// removalShellBranchDeleteFlags end a `git branch` command by REMOVING a ref:
// `-d`/`-D` and the long `--delete`. The deletion is detected by the flag being
// PRESENT, not by where it sits, because a shell writes `git branch -d "$b"` and
// `git branch "$b" -d` alike.
//
// `-r`/`--remotes` and `-a`/`--all` are DELIBERATELY ABSENT, and the omission is
// MEASURED: on their own they set the LIST SCOPE, and this tree reads
// `git branch -r --format=…` in scripts/gopi-stats.sh and
// `git branch --merged …` in the merge scans. A predicate that treated `-r` as a
// deletion would have reported a census line — the failure archiveMintFind's
// header records for `git tag | cnt`, one namespace over. A fused `-rd` still
// matches, because the cluster names `-d` (see removalShellFlagWords).
//
// `-f`/`--force` are absent for the same reason: on their own they MOVE a ref
// (`git branch -f <name> <base>`), which is the mutation this half leaves to
// shared_integration_ref_guard.go's own predicate — reporting it would put a
// removal gate in front of a create.
//
// NARROWING (2026-10-16, the devdoc-corpus epic) — why `branch` is NOT a FLAG
// WORD. The predicate still reads the FLAG the way git reads it (a cluster is
// several flags, `-rd` names `-d`), but the word `branch` itself is excluded, so
// `git branch -d $BRANCH` is a site while a MANUAL-PAGE CITATION of the same form
// — `git branch -d`, `git branch -D`, `git branch --delete`, the metavariable
// spelling in `devdoc/branch-lifecycle-ruling.md` and `devdoc/merge-command.md` —
// is not. That is not a hole opening: the deliberate phrase `-d $BRANCH` (an
// UPPERCASE shell variable, no braces) IS still a site, and it is the shape this
// half was written from (post-merge-cleanup.sh). What the exclusion buys is that
// a doc can NAME the verb it is discussing without the gate reporting the
// sentence rather than the act — the same rule that already keeps a `#` comment
// and a doc's `git push --force` (a MOVE) out of the report. Measured cost: an
// `eval`-ed `git branch $DEL_FLAG` whose variable name is lowercase resolves to
// UNKNOWN and is not reported — the same honest gap removalShellVars already
// states.
var removalShellBranchDeleteFlags = []string{
	removalShellBranchDeleteFlagShort, removalShellBranchDeleteFlagForce,
	removalShellBranchDeleteFlagLong, removalShellVerbBranch,
}

// The three spellings of `git branch`'s deletion, named because TWO readers need
// them: the vocabulary above, and the red proof's raw-row splice, which must
// spell the shape the predicate reports WITHOUT this carrier file carrying a
// literal site (Rule 13/14 — one definition, referenced by every consumer).
const (
	removalShellBranchDeleteFlagShort = "d"
	removalShellBranchDeleteFlagForce = "D"
	removalShellBranchDeleteFlagLong  = "delete"
)

// removalShellRefDeleteFlags end a `git update-ref` command by removing a ref.
// Only `-d`/`--delete` qualify: the three-argument form MOVES a ref, which is a
// different mutation with its own guard (internal/tools/shared_integration_ref_guard.go).
var removalShellRefDeleteFlags = []string{"d", "delete"}

// removalShellPushDeleteFlags are `git push`'s two deletion spellings. The short
// one is git's own alias for `--delete` (measured: `git push --help` lists
// `-d, --delete`), which is why it is here and not in the refspec rule.
var removalShellPushDeleteFlags = []string{"delete", "d"}

// removalShellRefToken marks the REF argument of `git update-ref -d <ref>`: the
// deletion must NAME a ref to be a ref removal. Without it the rule would fire
// on a `-d` flag that belongs to some other command on the line.
const removalShellRefToken = "refs/"

// removalShellDesc is the one sentence every hit carries. It names the remedy,
// and the remedy is the funnel.
var removalShellDesc = "removes a ref with git directly instead of going through the ONE writer, so the removal leaves no audit record — " +
	"`gopi task archive <branch> --reason <code>` (run `gopi task archive -h` for the reason vocabulary) writes " +
	tools.ArchiveBranchPrefix + "<branch>, its companion reason ref, AND the ref removal, and its reclaim leg is the arm that records the " +
	"`kind=branch-delete` line. measured 2026-10-15: the raw delete removed the ref and wrote nothing to the sink."

// removalShellTextCorpus narrows the module-wide text walk to this half's
// carriers. It is the `textCorpus` hook rather than a check inside the predicate
// so the file set a reader sees is the file set the gate walks (the hook exists
// for exactly this: an INSTRUCTION SURFACE is not the same thing as every text
// file in the module).
func removalShellTextCorpus(rel string) bool {
	slashed := filepath.ToSlash(rel)
	if slashed == archiveMintInstructionRel {
		return true
	}
	if removalShellIsInstruction(slashed) || removalShellIsDevdocCarrier(slashed) {
		return true
	}
	if strings.HasPrefix(slashed, removalShellScriptsPrefix) {
		return true
	}
	if !strings.HasPrefix(slashed, removalShellGoRoot) && !strings.HasPrefix(slashed, removalShellCmdRoot) {
		return false
	}
	// The production Go trees only: a `_test.go` fixture exists to CONSTRUCT the
	// shape (and a non-Go file under cmd/ is not production Go).
	return strings.HasSuffix(slashed, removalShellGoSuffix) && !strings.HasSuffix(slashed, removalShellTestSuffix)
}

// removalShellIsInstruction reports whether a repo-relative path is an LLM
// INSTRUCTION carrier — a markdown file under cmd/agent/ whose prose a spawned
// gopi child is told to execute. It is ONE function so the corpus hook and the
// red proof cannot disagree about the file set (Rule 14).
func removalShellIsInstruction(slashed string) bool {
	return strings.HasPrefix(slashed, removalShellInstructionRoot) &&
		strings.HasSuffix(slashed, removalShellInstructionSuffix)
}

// removalShellIsDevdocCarrier reports whether a repo-relative path is one of the
// named devdoc carriers. It is the list lookup, in ONE place, so the corpus hook,
// the floor and the red proof read the same set (Rule 14).
func removalShellIsDevdocCarrier(slashed string) bool {
	for _, rel := range removalShellDevdocCarriers {
		if slashed == rel {
			return true
		}
	}
	return false
}

// removalShellFindInScope reports whether one repo-relative path is a carrier of
// this half — the same rule as the corpus hook, stated once for the red proof,
// which feeds the predicate a synthetic path (Rule 14: one definition, so a
// probe cannot be green against a file set the scan does not use).
func removalShellFindInScope(rel string) bool { return removalShellTextCorpus(rel) }

// removalShellFind reports the raw ref-removal sites in one in-scope carrier.
//
// EVERY CARRIER IS READ AS TEXT, and that is a MEASURED choice rather than a
// simplification — the archive-mint gate learned it the hard way: a text gate's
// traversal builds `parsed{rel, src}` with NO file set, so an ast.Inspect over a
// nil file returns nothing and the unit test passes while the production scan
// reports zero hits (a gate correct in isolation and unreachable in production).
func removalShellFind(g gate, p *parsed) []hit {
	if !removalShellFindInScope(p.rel) {
		return nil // history, a fixture and every unrelated tree may quote the shape
	}
	isGo := strings.HasSuffix(p.rel, removalShellGoSuffix)
	vars := removalShellVars(p.src)
	var out []hit
	for i, line := range strings.Split(string(p.src), "\n") {
		if isGo {
			line = archiveMintStripLineComment(line)
		}
		if !removalShellLineRemovesRef(line, vars) {
			continue
		}
		out = append(out, hit{rel: p.rel, line: i + 1, desc: removalShellDesc})
	}
	return out
}

// removalShellVars reads the file's VARIABLE definitions: name → the values it
// may hold. It exists for the one measured spelling that would otherwise read as
// clean — post-merge-cleanup.sh writes the flag into a variable in one place
// and runs the delete in another — and it is the text half's counterpart of the
// Go half's constant resolution (removalAuditConsts), restricted to what a text
// scan can honestly see.
//
// A value the scan cannot resolve (a command substitution, a concatenation, a
// variable defined by another variable that is itself dynamic) is stored as
// UNKNOWN — empty — rather than guessed: an invented flag would report a site
// that is not there, and a gate that fires on the wrong line is a gate that gets
// switched off. The predicate therefore never reports a site whose flag it could
// not read, which is why this half ALSO names the one shape it cannot see in its
// header's Rule 15 paragraph.
func removalShellVars(src []byte) map[string][]string {
	vars := map[string][]string{}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), removalShellCommentPrefix) {
			continue
		}
		for _, tok := range removalShellFields(line) {
			name, values, ok := removalShellAssignVar(tok)
			if !ok {
				continue
			}
			vars[name] = removalShellResolveValues(values, vars, removalShellVarHopBudget)
		}
	}
	return vars
}

// removalShellVarHopBudget bounds the variable-to-variable chase (`B="$A"`,
// `C="$B"`). Two hops cover every measured chain in this tree; a longer one is a
// generated script, where an honest "unknown" beats a guess.
const removalShellVarHopBudget = 3

// removalShellResolveValues expands the values that are themselves variable
// references, up to the hop budget, so a flag carried through two names is still
// read as the flag.
func removalShellResolveValues(values []string, vars map[string][]string, hops int) []string {
	if hops <= 0 {
		return nil
	}
	var out []string
	for _, v := range values {
		name, ok := removalShellVarRef(v)
		if !ok {
			out = append(out, v)
			continue
		}
		out = append(out, removalShellResolveValues(vars[name], vars, hops-1)...)
	}
	return out
}

// removalShellFields tokenises one line the way the predicate reads it: markdown
// quoting removed, then whitespace split. It is ONE function because the
// variable scan and the predicate must agree about what a token is — a second
// spelling of the split would let a definition be read one way and a use
// another (Rule 14).
func removalShellFields(line string) []string {
	return strings.Fields(strings.ReplaceAll(line, removalShellMarkdownQuote, " "))
}

// removalShellAssignVar parses one token as a variable definition, returning the
// name and the values it may hold. `ok` is false for anything that is not a
// plain `NAME=…` assignment: a command's own `--opt=value`, a comparison, and an
// assignment whose left side is itself a substitution are all out of reach of a
// text scan and must not be read as definitions.
func removalShellAssignVar(tok string) (string, []string, bool) {
	eq := strings.Index(tok, removalShellAssignOp)
	if eq <= 0 {
		return "", nil, false
	}
	name := tok[:eq]
	if !removalShellPlainName(name) {
		return "", nil, false
	}
	return name, removalShellTokenValues(tok[eq+len(removalShellAssignOp):]), true
}

// removalShellPlainName reports whether a token is a bare shell identifier.
func removalShellPlainName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !removalShellWordRune(r) {
			return false
		}
	}
	return true
}

// removalShellWordRune reports whether a rune continues a shell identifier.
func removalShellWordRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// removalShellTokenValues splits the right-hand side of an assignment into the
// values it may hold: quotes stripped, whitespace separated. A value the scan
// cannot resolve literally (it contains a substitution) yields UNKNOWN (nil)
// rather than a guess.
func removalShellTokenValues(rhs string) []string {
	rhs = strings.Trim(rhs, removalShellQuote+removalShellQuoteAlt)
	if rhs == "" {
		return nil
	}
	if strings.Contains(rhs, removalShellVarSigil) {
		return nil // a substitution or a reference: the value is not readable here
	}
	return strings.Fields(rhs)
}

// removalShellVarRef parses one token as a variable REFERENCE — `$NAME`,
// `${NAME}`, `"${NAME}"` — and returns the name. Anything else (a literal, a
// word containing other text, a command substitution) is not a reference the
// scan can resolve.
func removalShellVarRef(tok string) (string, bool) {
	body := strings.Trim(tok, removalShellQuote+removalShellQuoteAlt)
	if !strings.HasPrefix(body, removalShellVarSigil) {
		return "", false
	}
	body = body[len(removalShellVarSigil):]
	if strings.HasPrefix(body, removalShellVarOpen) {
		body = strings.TrimPrefix(body, removalShellVarOpen)
		body = strings.TrimSuffix(body, removalShellVarClose)
	}
	body = strings.TrimSuffix(body, ";")
	if !removalShellPlainName(body) {
		return "", false
	}
	return body, true
}

// removalShellLineRemovesRef reports whether one line carries a ref-removing git
// invocation.
//
// The three decisions it makes, each of them measured against this tree:
//
//   - a `#`-prefixed line is PROSE. safe-merge.sh's "already deleted its ref
//     (`git branch -d`)" note and post-merge-cleanup.sh's own
//     "branch -d 会拒绝" comments are documentation OF the verb, never the
//     invocation — reporting them would make the gate fire on the explanation
//     rather than the act, and a gate that does that gets switched off.
//   - the backtick is QUOTING, not a separator: it is replaced by a space before
//     tokenisation so a quoted command (`git branch -D <name>`) tokenises the
//     same way the shell would read it. Markdown prose quotes the command it
//     teaches, and the quoting must not be what hides it.
//   - the verb pair is looked for ANYWHERE on the line, not at its head: a shell
//     deletes inside a command substitution (`if git branch -d "$b"; then`) or
//     after a conditional, and anchoring at the head would leave both unreported.
func removalShellLineRemovesRef(line string, vars map[string][]string) bool {
	if strings.HasPrefix(strings.TrimSpace(line), removalShellCommentPrefix) {
		return false
	}
	fields := removalShellFields(line)
	idx := removalShellRefVerbIndex(fields)
	if idx < 0 {
		return false
	}
	operands := removalShellExpandVars(fields[idx+1:], vars)
	switch fields[idx] {
	case removalShellVerbBranch:
		return removalShellFlagIn(operands, removalShellBranchDeleteFlags)
	case removalShellVerbUpdateRef:
		return removalShellFlagIn(operands, removalShellRefDeleteFlags) && removalShellNamesRef(operands)
	case removalShellVerbPush:
		return removalShellPushRemovesRef(operands)
	}
	return false
}

// removalShellExpandVars replaces every variable REFERENCE in the operands with
// the values the file's definitions gave it, so a flag carried by a name reaches
// the flag predicate. A reference the scan could not resolve expands to nothing:
// the operand list shrinks rather than acquiring an invented flag.
func removalShellExpandVars(operands []string, vars map[string][]string) []string {
	var out []string
	for _, tok := range operands {
		name, ok := removalShellVarRef(tok)
		if !ok {
			out = append(out, tok)
			continue
		}
		out = append(out, vars[name]...)
	}
	return out
}

// removalShellRefVerbIndex returns the index of the ref-mutating verb in a
// `<git> <verb>` pair, or -1 for a line with no such pair.
//
// It looks for an ADJACENT command-word + verb pair, and the command word is
// matched with the punctuation a shell and prose wrap it in (`$(git`, “ `git “,
// `(git`, an assignment's `g=$(git`) — the same measured rule archiveMintGitWord
// states, reused rather than restated so the two text gates cannot drift on what
// counts as the command word.
func removalShellRefVerbIndex(fields []string) int {
	for i := 0; i+1 < len(fields); i++ {
		if !archiveMintGitWord(fields[i]) {
			continue
		}
		switch fields[i+1] {
		case removalShellVerbBranch, removalShellVerbUpdateRef, removalShellVerbPush:
			return i + 1
		}
	}
	return -1
}

// removalShellFlagIn reports whether any token is one of the named flags.
//
// Three spellings are accepted, and each is a shape this tree actually writes:
//
//   - the flag as its own token (`-d`, `--delete`);
//   - a SHORT CLUSTER (`-fD`, `-rf`): every character must be an allowed one, so
//     `-m <msg>` (the create/move form) stays out while `-Df` is reported — the
//     rule reads the cluster, not the first letter;
//   - an ALTERNATION (`-d|-D`, `-d/-D`): prose writes the flag twice, and the
//     token is split on `/` and `|` so the alternation cannot hide the verb.
func removalShellFlagIn(operands, allowed []string) bool {
	for _, tok := range operands {
		if strings.Contains(tok, removalShellAssignOp) {
			continue // an assignment's own `NAME=value` is not the command's operand
		}
		if removalShellTokenIsFlag(tok, allowed) {
			return true
		}
	}
	return false
}

// removalShellShortFlagSep is the alternation separator: prose writes
// `-d|-D`, a shell writes `-d` — both name the same flag.
const removalShellShortFlagSep = "|"

// removalShellTokenIsFlag reports whether one token is a USE of an allowed flag.
//
// A token that names ONE flag is a use of it: the flag on its own (`-d`), a
// SHORT CLUSTER the shell reads back as several flags (`-fD`, `-rd`) and the
// LONG form (`--delete`) all qualify, and removalShellFlagWords decides which
// flags the token names.
//
// A token that carries an ALTERNATION (`-d/-D`, `-d|-D`) is read by the shape
// the alternation has, and that shape is what separates a USE from a MENTION —
// the distinction this half's header draws, applied one level below the verb:
//
//   - EVERY alternative names an allowed flag (`-d/-D`, `-D/-d`) — the token is
//     still saying "delete it one way or the other", so it is reported;
//   - SOME alternative names something else (`-f/-D/-B`, `-d|-f`) — the token is
//     a VOCABULARY LIST: a rule, a refusal or a classifier naming the FAMILY a
//     command is classified into, not an instruction to run one member of it.
//     It is NOT reported.
//
// MEASURED 2026-11-05, and this is the false positive the rule exists for. The
// two surfaces that REFUSE the destructive ref move spell the family exactly
// that way — PROJECT.md Rule 37's "NO DESTRUCTIVE REF MOVE … `git branch
// -f/-D/-B`" bullet and internal/agent/chat_pa_duty.go's "(2b) … the ref move
// is operator-only" paragraph. The old rule split the token on `/` and took ANY
// allowed flag it found, so the `-D` inside the family list was read as a
// deletion and two rule statements were reported as instructions to delete a
// ref. The only answer the gate left was a per-site inline escape (commit
// 9ccb8f8a added two), which is a suppression sitting on the two lines whose
// WHOLE MEANING is the refusal — exactly backwards.
//
// WHAT THE NARROWING DOES NOT OPEN: every deliberate deletion this tree spells
// is still reported, and the arms in TestRemovalAuditGateShellHalfRedProof pin
// each one — `git branch -D <b>`, `git branch -d/-D <b>` (an all-deletion
// alternation), the `-fD` cluster, `git branch $DEL_FLAG <b>` and
// `git update-ref -d <ref>`. What is gone is the reading of a family listing as
// one of its own members.
func removalShellTokenIsFlag(tok string, allowed []string) bool {
	parts := strings.FieldsFunc(tok, removalShellFlagSplit)
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !removalShellPartIsFlag(part, allowed) {
			return false
		}
	}
	return true
}

// removalShellPartIsFlag reports whether ONE alternative of a token names an
// allowed flag. It is the single-alternative half of removalShellTokenIsFlag,
// split out so the two readings cannot drift (Rule 14).
func removalShellPartIsFlag(part string, allowed []string) bool {
	for _, word := range removalShellFlagWords(part) {
		if removalShellNames(word, allowed) {
			return true
		}
	}
	return false
}

// removalShellFlagWords returns the flag names one token carries.
//
// A token is read the way git reads it, and the three shapes are the whole
// reason this is a function rather than a string comparison:
//
//   - the SHORT form names one flag per letter, so a CLUSTER is several flags
//     (`-rd` = `-r -d`, and the `-d` is the deletion);
//   - the LONG form is ONE name, and `--opt=value` names the flag before the
//     `=` (`--delete=…` is still a deletion, `--format=…` is still not);
//   - anything without a leading `-` names nothing.
func removalShellFlagWords(tok string) []string {
	if strings.HasPrefix(tok, removalShellLongFlagPrefix) {
		word := strings.TrimPrefix(tok, removalShellLongFlagPrefix)
		if i := strings.Index(word, removalShellAssignOp); i >= 0 {
			word = word[:i]
		}
		if word == "" {
			return nil
		}
		return []string{word}
	}
	if !strings.HasPrefix(tok, removalShellFlagPrefix) {
		return nil
	}
	var out []string
	for _, r := range strings.TrimPrefix(tok, removalShellFlagPrefix) {
		out = append(out, string(r))
	}
	return out
}

// removalShellFlagSplit splits an alternation written in prose or a shell
// (`-d/-D`, `-d|-D`) into the flags it names.
func removalShellFlagSplit(r rune) bool {
	return r == '/' || strings.ContainsRune(removalShellShortFlagSep, r)
}

// removalShellFlagWord is gone: the flag reading lives in removalShellFlagWords
// (one definition of "which flags does this token carry" — Rule 14), and the
// deletion predicates ask removalShellFlagIn.

// removalShellNames reports whether a spelling is one of the allowed flags.
func removalShellNames(spelling string, allowed []string) bool {
	for _, a := range allowed {
		if spelling == a {
			return true
		}
	}
	return false
}

// removalShellNamesRef reports whether the operands name a REF — the argument
// `git update-ref -d` needs before the line is a ref removal rather than some
// other command's `-d`.
func removalShellNamesRef(operands []string) bool {
	for _, f := range operands {
		if strings.Contains(f, removalShellRefToken) {
			return true
		}
	}
	return false
}

// removalShellPushRemovesRef reports whether a `git push` deletes a branch.
//
// Two spellings, both shipped in this tree's vocabulary:
//
//   - `--delete` / `-d`: the explicit form;
//   - a refspec whose SOURCE is empty (`:refs/heads/<b>`): the same deletion
//     written as a push, which is how a shell that already has the remote and
//     the branch in variables tends to spell it.
//
// A `+`/`--force` push is deliberately NOT here: it moves a remote ref, it does
// not remove one, and reporting it would put a gate in front of the push lane's
// ordinary delivery (measured: cmd/agent/merge_prompt.md and the pushback lane
// both push with force semantics on purpose).
func removalShellPushRemovesRef(operands []string) bool {
	if removalShellFlagIn(operands, removalShellPushDeleteFlags) {
		return true
	}
	for _, f := range operands {
		if strings.HasPrefix(f, removalShellRefspecPrefix) && strings.Contains(f, removalShellHeadsRef) {
			return true
		}
	}
	return false
}
