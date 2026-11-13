package agent

// reframe_ground_clauses.go — the reframe swap must not delete the clauses that
// say WHICH TREE this run writes.
//
// ── THE DEFECT (measured 2026-11-01, row 01a1957a-0e54) ─────────────────────
//
// applyReframe replaces the raw [EPIC GOAL] message with a compact body built
// from the model's own four fields. That is right for the briefing's PROSE — a
// giant payload re-injected every round buys nothing after it has been read once
// — and it was silently wrong for the briefing's HARNESS CLAUSES, because they
// are not prose: they are the runner's own statements of fact about this run,
// and the model cannot reconstruct a single one of them from its own summary.
//
// The measurement, from that run's own request log
// (`…/01a1957a-…raw.log`, one JSON request per line):
//
//	req#1   line 3     ground=True  publish=True  tree=True  evidence=True  cutpoint=True
//	req#2   line 489   ground=True  publish=True  tree=True  evidence=True  cutpoint=True
//	req#3   line 5335  ground=False publish=False tree=False evidence=False cutpoint=False   ← reframe applied
//	… every later request: all five False
//
// and 12 s after that switch the run ran `cd /home/nick/work/gopi && git
// checkout -b task/<id>` in the repo's MAIN checkout, then edited a file there.
// The briefing's authority clause names that directory by absolute path and
// says twice that it is read-only for this run; the publish clause says the
// clone channel pushes, never the run. All of it was present in req#1–#2 and
// absent from req#3 onwards.
//
// WHAT THIS FILE DOES. It re-attaches those clauses VERBATIM after the reframed
// body. They are facts about the RUN, not about the epic, so a summary of the
// epic must not be able to drop them; the labels themselves come from the ONE
// package that writes them (internal/tools), so a renamed clause is picked up
// here with no second spelling (Rules 13/14).
//
// WHAT IT DELIBERATELY DOES NOT DO. It does not re-attach the whole briefing
// (that is the context cost the reframe exists to remove), and it does not
// PARAPHRASE a clause — a paraphrase of "the ONLY tree you write" is a weaker
// sentence than the one the runner wrote, and the model would be reading our
// summary of a rule instead of the rule.

import (
	"strings"

	"github.com/nick/gopi/internal/tools"
)

// reframePreservedClauseLabels is the set of briefing clauses a reframe keeps.
// Each label is the CONSTANT the clause's writer uses, so the reader here and
// the writer there cannot drift (Rule 14).
//
// THIS LIST IS NOT SELF-ENFORCING, WHICH IS EXACTLY THE DEFECT IT WAS MEANT TO
// CLOSE. A new briefing clause added tomorrow is preserved only if somebody
// remembers to add its label here; forgetting is SILENT, and the clause is then
// dropped at the first reframe — the shape of the 01a1957a incident, one level
// down. `reframe_dropped_clause_labels_test.go` is the machine half: it
// enumerates every briefing-clause label constant under internal/tools, and each
// one must appear either here or in reframeDroppedClauseLabels below with a
// STATED reason. An unlisted label reddens it.
var reframePreservedClauseLabels = []string{
	tools.TaskGroundAnnouncementLabel,
	tools.TaskBriefingPublishLabel,
	tools.TaskBriefingAuthorityLabel,
	tools.TaskGroundEvidenceLabel,
	tools.GroundCutPointLabel,
}

// reframeDroppedClauseLabels is the OTHER half of that census: briefing-clause
// labels the reframe swap deliberately does NOT keep, each with the reason. It
// is a deny-list beside an allow-list on purpose (the .gitignore shape): a label
// has to be counted in one of the two, so "nobody decided" cannot pass for a
// decision and a new clause cannot quietly join the dropped set either.
//
// WHAT BELONGS HERE: a clause about the EPIC (its premises, its figures, its
// acceptance) rather than about the RUN — the reframe's whole job is to compress
// the epic, and re-attaching those would put back the context it just removed.
// What must NOT be here: anything that says which tree this run writes, who
// publishes its commits, where its evidence lands, or which ref a criterion is
// measured on — the model cannot reconstruct any of that from its own summary,
// and that is the measured failure mode this file exists for.
var reframeDroppedClauseLabels = map[string]string{
	tools.TaskBriefingPremiseLabel: "a PREMISE of the epic (a revision it claims is unlanded). " +
		"The reframe already restates the epic, and a stale premise is re-derived at dispatch, not preserved across a swap",
	tools.TaskBriefingPremiseBaseLabel: "the epic's BASE REVISION claim. " +
		"Same population as the premise clause: it describes what the briefing ASSERTS, not which tree this run writes",
	tools.TaskBriefingCountLabel: "a population FIGURE the epic quotes. " +
		"A figure is about the epic's subject, not about the run; the reframed body states the acceptance it belongs to",
	tools.TaskBriefingGateAssertLabel: "a GATE ASSERTION the epic makes. " +
		"The assertion is part of the epic's acceptance, which the reframe restates by construction",
	tools.TaskBriefingPathSetLabel: "a PINNED PATH SET the epic names. " +
		"The set belongs to the epic's subject matter; the reframed body carries the paths it still needs",
	tools.TaskBriefingRunAlternativesLabel: "an ACCEPTANCE detail (a `go test -run` alternative that reaches no test). " +
		"It judges the epic's acceptance line, not the run's tree",
	tools.TaskBriefingProseTestNamesLabel: "an ACCEPTANCE detail (a prose `Test…` name that reaches no test function). " +
		"Same population as the alternatives clause",
	tools.TaskBriefingRunAltPartialLabel: "an ACCEPTANCE detail on the SAME population as the alternatives clause above, from the SAME file " +
		"(task_briefing_run_alternatives_partial.go is that arm's sibling and says so in its own header: the dead-alternative clause sounds on the " +
		"all-dead pattern, this one on the partial pattern, and neither sounds on the other). It is a measurement of the BRIEFING's `-run` selector — " +
		"which alternatives reach a test in this ground — not a fact about which tree this run writes, who publishes its commits, where its evidence " +
		"lands, or which ref a criterion is measured on. Splitting that pair, keeping one arm of one measurement and dropping the other, would make the " +
		"two halves of a single finding classify differently. DROPPING IT LICENSES NO COMPLETION CLAIM, on the clause's own words: it closes by saying " +
		"\"This clause is NOT evidence that the work is done, and the green it reads is NOT a reading of the package\" — so an executor's done-judgement " +
		"never rested on it, and what it asks for (REPLACE THE DEAD ALTERNATIVES…, quoted against the briefing's own acceptance line) is an act performed " +
		"while the briefing is being read. HONEST RESIDUAL, stated rather than covered: unlike its siblings, NOTHING re-derives this measurement after " +
		"the swap — the close-out's declared entry point is a package-scoped `make test-budget PKGS='…'` and carries no `-run` at all, so it cannot " +
		"reproduce or refute this clause. A run that loses it can read a partial green as a full one; that risk is about the RUN's own acceptance " +
		"discipline, which is the reason it is the executor's to resolve at comprehension time",
	tools.TaskSubmitBaseStaleLabel: "a SUBMIT-TIME warning about a declared base revision. " +
		"It is stamped when the row is FILED and describes the payload, not the running run's tree",
	tools.MasterReadingLabel: "which `master` a criterion is measured against. " +
		"It IS run-relevant, and it is dropped on purpose: the announcement clause and the cut-point clause both name the integration ref the run actually stands on, so this one would be a third, older opinion about the same fact (the cut point is measured ON THIS GROUND, this one was measured where the row was authored)",
	tools.TaskAttachmentClaimNoneLabel: "a `task show` READ-FACE marker (the payload names attachments it does not carry). " +
		"It is printed by `task show`, never appended to a briefing",
}

// reframePreservedBriefingClauses returns the harness clauses present in raw,
// verbatim and in their original order of appearance, joined by a blank line.
// A briefing that carries none returns "" (a run with no announcement — a chat,
// a repo-less row — keeps exactly the behaviour it had).
func reframePreservedBriefingClauses(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	var kept []string
	for _, label := range reframePreservedClauseLabels {
		body := reframeClauseBody(lines, label)
		if body != "" {
			kept = append(kept, body)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "\n\n")
}

// reframeClauseBody returns the section that STARTS at label and runs until the
// next harness label (or the end of the briefing). A label is matched on its own
// line, at the head: a clause that merely MENTIONS another label's text in its
// prose does not start a new section.
func reframeClauseBody(lines []string, label string) string {
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), label) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if reframeLineStartsAnyClause(lines[i]) {
			end = i
			break
		}
	}
	body := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	return body
}

// reframeLineStartsAnyClause reports whether a line begins one of the preserved
// clauses — the boundary a section scan stops at.
func reframeLineStartsAnyClause(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, label := range reframePreservedClauseLabels {
		if strings.HasPrefix(trimmed, label) {
			return true
		}
	}
	return false
}
