package main

// devworkspace_delivery_decisive_test.go — the LEDGER BUDGET test (row
// 01a18124, measured on row 01a1810a).
//
// The auto-registered row a run writes carries the summary
// `[auto] ` + normalizeTaskSummary(briefing): folded to ONE line and cut at
// agent.AutoTaskSummaryBudget (200) runes FROM THE START. The delivery-retry
// briefing used to put a ~180-character fixed prefix (the workspace path plus
// "第 N/M 轮交付尝试被 pull 这一步挡下了。工具原话（逐字，未改写）：") in front of the
// tool's words, so the ONE line that distinguishes a CONTENT CONFLICT in a named
// file from "the pull step refused" was always outside the budget, and the row
// reported only the second half. The operator's ruling was explicit: do not
// raise the cap, make the decisive line fit.
//
// These arms therefore assert on the FINAL STRING THE LEDGER STORES, not on the
// briefing: a briefing can look right while the summary it folds into does not.
// Each of the three blocker kinds gets its own arm — they are refused by three
// different tools with three different vocabularies, so one arm proving the pull
// case says nothing about the build or the dirty case.

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// pullConflictBlocker is the blocker a real `git pull --rebase` produced on row
// 01a1810a's workspace — the Words are the PRODUCTION composition
// (devworkspace.pullFailureWords: the exit line, git's own lines joined with
// " ; ", then the conflicted paths), copied from that row's transcript.
func pullConflictBlocker() *devworkspace.Blocker {
	return &devworkspace.Blocker{
		Kind: devworkspace.BlockerKindPull,
		Step: "git pull --rebase origin master",
		Words: "git pull --rebase origin master refused (exit: 1) | From peerbox:/home/user/work/gopi ; " +
			"* branch            master     -> FETCH_HEAD ; 138f611..f2d6d39  master     -> origin/master ; " +
			"Rebasing (1/1)Auto-merging cmd/agent/cli_resume.go ; " +
			"Auto-merging internal/tools/task_holder_resume.go ; " +
			"CONFLICT (content): Merge conflict in internal/tools/task_holder_resume.go ; " +
			"error: could not apply db17b50... fix(resume): resolve a task-id target through its row's run directory, " +
			"and refuse when the row names no checkpoint ; " +
			"Could not apply db17b50... fix(resume): resolve a task-id target through its row's run directory | " +
			"conflicted paths (git diff --name-only --diff-filter=U): internal/tools/task_holder_resume.go",
		Paths: []string{"internal/tools/task_holder_resume.go"},
	}
}

// buildFailureBlocker is a declared-build failure as `tail` hands it over: the
// compiler names the file and the token; `make` names the target it gave up on.
func buildFailureBlocker() *devworkspace.Blocker {
	return &devworkspace.Blocker{
		Kind: devworkspace.BlockerKindBuild,
		Words: "ok  	github.com/nickhuang99/gopi/internal/agent	1.2s | " +
			"# github.com/nickhuang99/gopi/internal/devworkspace | " +
			"./decisive_lines.go:118:14: undefined: decisiveLineJoiner | " +
			"make: *** [Makefile:42: build] Error 1",
	}
}

// TestDeliverySummaryCarriesThePullConflictDecisiveLine is the acceptance arm of
// the epic: the CONFLICT line and the conflicted PATH must both be inside the
// 200-rune ledger summary a run actually writes.
func TestDeliverySummaryCarriesThePullConflictDecisiveLine(t *testing.T) {
	blk := pullConflictBlocker()
	briefing := deliveryRetryBriefing(blk, "/home/user/workspace/01a17df9-824d-7a54-bd7e-fc8e84247ce4", 1)
	summary := agent.AutoTaskSummaryLine(briefing)

	if !strings.HasPrefix(summary, agent.AutoTaskSummaryPrefix) {
		t.Fatalf("the stored summary must carry the auto marker %q: %q", agent.AutoTaskSummaryPrefix, summary)
	}
	// TWO BUDGETS, AND THE STRICTER ONE IS THE LEDGER'S. normalizeTaskSummary
	// folds the BRIEFING to agent.AutoTaskSummaryBudget runes and the `[auto] `
	// marker goes in front of that — while the ledger's own store
	// (internal/tools sanitizeTaskSummary) caps the WHOLE stored row at the same
	// number (taskSummaryMaxRunes mirrors it). So the row a reader actually sees
	// is the first AutoTaskSummaryBudget runes of `summary`, and that is the
	// string the decisive line has to survive in. Asserting on it (rather than on
	// the untruncated fold) is what makes this test about the real row.
	stored := truncateRunesForTest(summary, agent.AutoTaskSummaryBudget)
	if n := utf8.RuneCountInString(summary); n > agent.AutoTaskSummaryBudget {
		t.Logf("the fold is %d runes and the store keeps %d — the assertions below are made on the STORED prefix",
			n, agent.AutoTaskSummaryBudget)
	}
	// THE TWO SUBSTRINGS THE ROW WAS MISSING. Either is enough per the
	// acceptance criterion; both are asserted because the conflict line carries
	// both facts and losing either one is a distinct regression.
	for _, want := range []string{"CONFLICT", "task_holder_resume.go"} {
		if !strings.Contains(stored, want) {
			t.Fatalf("the ledger summary must name the CONFLICT and the file (%q missing):\n%s", want, summary)
		}
	}
	// AND IT MUST STILL SAY WHICH STEP REFUSED — the old briefing's one true
	// fact is not traded away for the new one.
	if !strings.Contains(stored, string(devworkspace.BlockerKindPull)) {
		t.Fatalf("the summary must still name the refusing step: %q", summary)
	}
	// THE ANNOUNCEMENT IS NOT THE HEADLINE: the fetch plumbing that used to
	// crowd the line out must not be what the summary opens with after the
	// label.
	// THE MEASUREMENT ITSELF, printed so a run's transcript carries the row the
	// ledger would store (Rule 31: a surface reported ready is a surface that was
	// executed, with its real output pasted — not `-h`).
	t.Logf("deliveryRetryBriefing → agent.AutoTaskSummaryLine = %q (%d runes); STORED (first %d) = %q",
		summary, utf8.RuneCountInString(summary), agent.AutoTaskSummaryBudget, stored)
	if strings.Contains(stored, "From peerbox:") && strings.Index(stored, "From peerbox:") < strings.Index(stored, "CONFLICT") {
		t.Fatalf("the fetch plumbing must not precede the decisive line:\n%s", summary)
	}
}

// TestDeliverySummaryCarriesTheBuildFailureDecisiveLine is the build kind's arm:
// the compiler's own `file.go:line:col: undefined: X` is what a reader acts on,
// and `make: *** [Makefile:42: build] Error 1` (the last line, the one a tail
// ends on) is not.
func TestDeliverySummaryCarriesTheBuildFailureDecisiveLine(t *testing.T) {
	blk := buildFailureBlocker()
	summary := agent.AutoTaskSummaryLine(deliveryRetryBriefing(blk, "/ws/task", 2))

	for _, want := range []string{"undefined: decisiveLineJoiner", "decisive_lines.go:118:14"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("the build summary must name the compiler's own failure (%q missing):\n%s", want, summary)
		}
	}
	if !strings.Contains(summary, string(devworkspace.BlockerKindBuild)) {
		t.Fatalf("the summary must name the refusing step: %q", summary)
	}
}

// TestDeliverySummaryCarriesTheDirtyPathDecisiveLine is the dirty kind's arm.
// Its tool (`git status --porcelain`) has no failure vocabulary — the LISTING is
// the statement — so this arm also pins that the two-column status is preserved
// verbatim: `" M <path>"` (unstaged) and `"M  <path>"` (staged) are different
// facts, and a briefing that trims the columns has changed git's answer.
func TestDeliverySummaryCarriesTheDirtyPathDecisiveLine(t *testing.T) {
	blk := &devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindDirty,
		Words: " M internal/tools/task_holder_resume.go\n?? scratch.log\n",
		Paths: []string{" M internal/tools/task_holder_resume.go"},
	}
	summary := agent.AutoTaskSummaryLine(deliveryRetryBriefing(blk, "/ws/task", 1))

	if !strings.Contains(summary, " M internal/tools/task_holder_resume.go") {
		t.Fatalf("the dirty summary must carry git's porcelain line VERBATIM (leading space included):\n%s", summary)
	}
	if !strings.Contains(summary, string(devworkspace.BlockerKindDirty)) {
		t.Fatalf("the summary must name the refusing step: %q", summary)
	}
}

// TestDeliveryBriefingSaysWhenNoDecisiveLineCouldBeExtracted is Rule 15's half:
// when the extractor recognises nothing, the briefing SAYS SO and names what it
// looked for — the tool's words are still handed over in full underneath, so the
// reader is never left with an unexplained silence.
func TestDeliveryBriefingSaysWhenNoDecisiveLineCouldBeExtracted(t *testing.T) {
	blk := &devworkspace.Blocker{
		Kind:  devworkspace.BlockerKindPull,
		Words: "git pull --rebase origin master refused (exit: 1) | From peerbox:/home/user/work/gopi",
	}
	note := blk.DecisiveLines()
	if note.Found() {
		t.Fatalf("this blocker carries plumbing only — nothing should have been recognised: %+v", note)
	}
	if !strings.Contains(note.Why, "没能抽出判定行") {
		t.Fatalf("the non-answer must say it could not extract a line: %q", note.Why)
	}
	briefing := deliveryRetryBriefing(blk, "/ws/task", 1)
	if !strings.Contains(briefing, "没能抽出判定行") {
		t.Fatalf("the briefing must say the extraction failed rather than omit the section:\n%s", briefing)
	}
	// The plumbing-only explanation says the output was NOT empty — the tool did
	// speak, it just said nothing that decides.
	if !strings.Contains(briefing, fmt.Sprintf("%d 段", 2)) && !strings.Contains(briefing, "段只是") {
		t.Fatalf("the non-answer must report how much output it read:\n%s", briefing)
	}
	if !strings.Contains(briefing, "From peerbox:/home/user/work/gopi") {
		t.Fatalf("the tool's words must still be handed over verbatim:\n%s", briefing)
	}
}

// truncateRunesForTest reproduces the store's cap on the ASSEMBLED row (the
// ledger keeps the first N runes and folds the rest away), so an assertion can
// be made on what a reader of the ledger actually sees. It is deliberately a
// test-local cut and NOT a second implementation of the sanitiser: the point is
// the length, and the length is the one number agent.AutoTaskSummaryBudget
// exports.
func truncateRunesForTest(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
