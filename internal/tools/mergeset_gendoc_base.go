package tools

// mergeset_gendoc_base.go — THE PRE-EXISTING HALF of the published-anchor edge,
// wired into the MERGED-RESULT measurement (rebuild_vet_mergeset.go).
//
// THE DEFECT this file closes, in the words of whoever next reads the branch.
// The union leg (mergeset_gendoc.go) asks its question over the LANDING and its
// TWO PARENTS, and its own header states the limit honestly: "a commit that
// moved a published anchor source WITHOUT touching the document is visible here
// only when it arrives IN the landing or in one of the landing's two parents. A
// commit further back on master is a pre-existing state, which the base arm
// owns." The base arm measured the MERGED-SET GATES on the base revision — the
// toolchain's `vet` and the fixed gate directories — and a DOCUMENT OF RECORD is
// DATA, so no leg of it ever read one: a stale block on master is green on
// `go build ./...`, green on `go vet ./...`, green on every fixed gate directory,
// and red only in the package the EDGE names (measured on this repo 2026-10-11:
// `TestCommandsDocGeneratedBlockMatchesSource` red at
// `internal/standards/commands_doc_gate_test.go:189` while master's truth was
// `cmd/agent/cli_archive.go:188`).
//
// WHY THE PROPERTY WAS INVISIBLE FOR SO LONG, stated so a reader does not read
// this as an oversight somewhere. Three measurements had to line up, and each
// one is right on its own:
//
//   - the post-pass (`go build ./...` + `go vet ./...`, since deleted with the
//     retired top-level `gopi merge` verb — it lived in merge_pass_verify.go) was
//     a COMPILE/TYPE measurement, and a generated document is data;
//   - ⓪e (`mergeAnchorGate`, merge_anchor_gate.go) measures the MERGE RESULT and
//     ABSTAINS when the branch's OWN document tests fail too
//     (mergeAnchorGateBranchFailText) — the shape EVERY COMMANDS.md drift in
//     this repo's history has had, because a branch cut from a stale master
//     carries the staleness itself;
//   - the union leg sees only what the landing or its two parents moved.
//
// So the tree keeps a drift nobody is asked about until it is fixed by hand.
//
// WHY HERE AND NOT A NEW HOOK (Rule 3/35 — no new tool, no new script, no new
// resident dependency). The merged-result step already runs on every landing and
// already materialises the BASE revision into a detached checkout for its
// attribution arm. This leg is the SAME measurement with the base checkout as
// its directory — one more member in the run set, one more `go test` of the
// package the EDGE names, no new process and no new tick.
//
// WHAT IT MEASURES, and the ONE question it asks: take the base revision's tree,
// read the published-anchor edge on it (the same ONE read every consumer makes,
// gendoc.GeneratedDocAnswer with a NIL change — "is this tree in step with
// itself?"), and run the gate packages the edge names ON THE BASE TREE. A red
// there is a drift that landed BEFORE this landing: reported, never attributed
// to the landing.
//
// WHY THE BASE'S OWN EDGE AND NOT MASTER'S GATE DIRECTORY LIST. Asking the
// repo's fixed gate directories would be a second opinion about which package
// blocks on a document (Rule 14), and it would miss the document's gate whenever
// it lives elsewhere — the same reason the union leg reads the edge.
//
// THE CHANGE ARGUMENT IS nil ON PURPOSE, and it is not an omission. The question
// here is not "did this landing oblige a document?" (the union leg asks that) but
// "does the base tree hold a document in step with its own sources?" — which is a
// property of the TREE, and a property of a tree is not a function of a path
// list. Reading it with nil is the same call the real-tree assertion in
// internal/standards makes for its precondition.
//
// NEVER A REPAIR, NEVER A ROLLBACK. It does not run the generator (rewriting the
// document is the author's act, not a gate's) and it never hand-edits the block.
// Its red is REPORTED through the merged-result step's own channels (notify +
// bug row, deduped by defect identity) with `res.OK` left alone — the sibling
// legs' contract, unchanged.
//
// IT REUSES, IT DOES NOT RE-DECLARE. The edge is the one definition of what a
// tree's documents oblige; the toolchain argv, the package-reference rendering
// and the run/parse machinery are the merged-result step's (Rule 14 — one
// rendering). What lives here is only what is genuinely new: the base-tree
// membership test.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/gendoc"
)

// gendocBaseGateName is how this leg identifies itself in every message. It is
// deliberately neither gendocUnionGateName NOR vetGateName: three measurements
// now speak on one close-out, and a reader has to be able to tell WHICH revision
// each of them was about (Rule 1b: compared and reported, so named once).
//
// The name says DOCUMENTS and the position says BASE, which is the whole
// distinction from the union leg: that leg's red is introduced by the landing,
// this leg's red is a state the landing FOUND.
const gendocBaseGateName = "base-documents"

// The abstentions, each a statement about the MEASUREMENT rather than about the
// tree (the gate's own abstention vocabulary, Rule 15): none of them may read as
// a clean pass, and none of them invents an error.
const (
	// gendocBaseNoCheckoutText is what a caller that asks the question without
	// the base checkout gets. The caller materialises the revision BEFORE it
	// asks anything (the step's own premise: the question is about the COMMIT,
	// never about whatever directory the merger stands in).
	gendocBaseNoCheckoutText = "the base-document question was asked without the base revision's checkout — the answer would be about a directory rather than about the commit (recorded, not measured)"

	// gendocBaseAbsentFmt is the abstention for a base tree that publishes no
	// generated document: it takes part in no edge, so there is no document of
	// record to be stale. The counts the SAME walk produced ride the sentence,
	// so "the scan looked and found nothing" is never inferred from silence.
	gendocBaseAbsentFmt = "the base revision publishes no generated document, so it takes part in no published-anchor edge (%d candidate(s), %d edge(s)): %s"

	// gendocBaseBrokenText opens the report for a base tree whose edge is
	// unusable (a document naming a generator that is not in the tree, an edge
	// with no gate package). It is REPORTED and the leg abstains: an unusable
	// edge is a defect in the tree that no run of this leg can measure, and
	// reading it as "nothing to run" is the silent short answer Rule 15 refuses.
	gendocBaseBrokenText = "the published-anchor edge is BROKEN on the base revision — "

	// gendocBaseNoGateText is the abstention for a base tree that publishes a
	// generated document no test package reads: nothing blocks on that document,
	// so no run of this leg could observe its drift. Stated, never passed over.
	gendocBaseNoGateText = "the base revision publishes a generated document, but no gate package reads it — nothing blocks on this document (recorded, not silently passed)"
)

// mergesetGendocBaseRuns resolves the base-document leg for one landing and
// returns the runs to append to the caller's run set, plus ONE log-ready note.
//
// A note is returned for every outcome that OBSERVED something, and its own
// words carry the distinction a reader needs (Rule 15): "the base has no
// document of record" and "the base could not be measured" are different facts,
// and the second must never read as a clean pass.
//
// baseDir is the base revision's own detached checkout, materialised by the
// caller and kept alive for the lifetime of this call; an empty baseDir is a
// caller error, not an abstention to hide.
func mergesetGendocBaseRuns(repo, mergeRev, baseDir string) ([]mergesetGateRun, string) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, gendocBaseNoCheckoutText
	}
	baseRev, ok := mergesetGateBaseRev(repo, mergeRev)
	if !ok {
		// The landing's first parent — the revision this leg is ABOUT — could
		// not be resolved. Without it there is no revision to name in the
		// report, and a finding attributed to an unknown revision is exactly
		// the "pre-existing state, nobody's business" shape this leg exists to
		// end.
		return nil, mergesetGateReasonNoDiffBase
	}
	// ONE READ OF THE TREE (Rule 14): the edge's own answer carries the
	// participation classification and the unusable-edge findings together, so
	// this leg cannot disagree with the delivery path about what a tree's
	// documents oblige.
	ans, err := gendoc.GeneratedDocAnswer(baseDir, nil)
	if err != nil {
		return nil, "the published-anchor edge could not be read on the base revision " + baseRev + " (" + mergeBuildGateTail(err.Error()) + ")"
	}
	if !ans.TakesPart() {
		reason := ans.Reason
		if reason == "" {
			reason = gendoc.ParticipationUnknown
		}
		return nil, fmt.Sprintf(gendocBaseAbsentFmt, ans.Candidates, ans.Docs, reason)
	}
	if len(ans.Findings) > 0 || len(ans.Errors) > 0 {
		// A BROKEN EDGE IS REPORTED, never read as "nothing to run": a document
		// whose generator is not in the tree, or an edge defect NO package could
		// catch (the ERROR class), is an instruction a reader would follow into a
		// command that runs nothing.
		//
		// Both channels are joined here because this leg's contract is the
		// REPORT (Rule 12), not a verdict: the class is carried to the operator
		// through this sentence, and the refusal on it belongs to the branch's
		// own arm.
		return nil, gendocBaseBrokenText +
			strings.Join(append(append([]string(nil), ans.Errors...), ans.Findings...), mergeCheckAbstainSeparator)
	}
	required := mergesetGendocParticipationGates(baseDir)
	if len(required) == 0 {
		return nil, gendocBaseNoGateText
	}
	refs := mergesetGendocPackageRefs(required)
	return []mergesetGateRun{{
		Gate:     gendocBaseGateName,
		Argv:     append(mergesetGateTestArgv(), refs...),
		Packages: refs,
	}}, ""
}
