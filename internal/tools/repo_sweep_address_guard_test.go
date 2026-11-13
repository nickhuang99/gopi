package tools

// repo_sweep_address_guard_test.go — repo_sweep.go's COMMENTS must not hand a
// reader an address that no longer dispatches.
//
// THE DEFECT THIS PINS (measured 2026-11-01, epic merge-verb-removal). The
// sweep's not-fast-forwardable arm said, in its own words:
//
//	Resolution is the merge command's job (the foreground `gopi merge`, whose
//	shipped prompt is cmd/agent/merge_prompt.md …
//
// Both halves of that address were gone: the top-level `gopi merge` verb was
// retired (the word now REFUSES and points at `gopi task merge`), and
// cmd/agent/merge_prompt.md was deleted with the one-shot implementation
// (commit 8cf39fb3). The BEHAVIOUR the comment describes was right the whole
// time — a non-ff branch must SURVIVE so the merge face can diff against it —
// so nothing had to move but the address.
//
// WHY A TEST AND NOT JUST A FIX: an address is the one thing a comment exists to
// give, and a wrong one costs every future reader the same round trip it cost
// this run (type the word, get a refusal). A one-off sweep of this file leaves
// the next paste of the old paragraph free to reintroduce it — which is exactly
// what happened across the corpus before 5ce259a6 triaged 47 files by hand.
//
// THE RULE, in the shape Rule 36 gives a retired-mechanism citation: a paragraph
// that NAMES a retired address must have said it is retired BEFORE the address
// appears. Naming it in the past tense is a record and is wanted; naming it as
// the live destination is the defect.
//
// FOUR ARMS: the real-tree verdict, the affirmative half (the LIVE address is
// still named — a guard that only forbids would be satisfied by silence), the
// red proof (the predicate fires on the verbatim pre-fix paragraph), and the
// green proof (a labelled paragraph passes, and so does the DIFFERENT word
// `gopi merger`).

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	// rsAddressGuardSourceRel is the file whose comments are judged. It is the
	// production file itself — not a copy — so the arm cannot drift from it.
	rsAddressGuardSourceRel = "repo_sweep.go"

	// rsCommentPrefix is the line-comment introducer of the judged file. The
	// scan does not try to strip string literals: judgement is per PARAGRAPH of
	// comment lines, and a `//`-prefixed line is one.
	rsCommentPrefix = "//"

	// rsRetiredAddressPattern matches the retired TOP-LEVEL verb as a whole
	// word. The boundary matters: `gopi merg`+`er` is a DIFFERENT word (the
	// retired resident daemon), and 5ce259a6 measured that a boundary-less grep
	// indicts 80 files where the real set is 47.
	rsRetiredAddressPattern = `\bgopi merge\b`

	// rsRetiredPromptToken is the deleted instruction file. It has no live
	// sibling: the one-shot merge child it instructed no longer exists in any
	// spelling, so ANY mention must carry the retirement label.
	rsRetiredPromptToken = "merge_prompt.md"

	// rsRetirementLabel matches the words that turn a mention into a RECORD.
	// Kept as one alternation because the label is prose, not a keyword: the
	// authors of this file write "RETIRED", "is gone", "were removed",
	// "went earlier with the daemon" — all of them label the same fact.
	rsRetirementLabel = `(?i)\b(retired|removed|gone|deleted|no longer|obsolete)\b`

	// rsLiveAddressPattern is the LIVE family spelling. Its presence is the
	// affirmative half: the delegation the paragraph describes still exists, so
	// the file must still say where it goes.
	rsLiveAddressPattern = `\bgopi task merge\b`

	// Non-vacuity floors: a scan that judged nothing must not read green. The
	// file is ~1500 lines of densely commented Go; these floors are far below
	// what it carries today and far above zero.
	rsAddressGuardMinJudgedLines  = 400
	rsAddressGuardMinJudgedBlocks = 60
)

// rsRetiredRe / rsLabelRe / rsLiveRe are the compiled forms, built once.
var (
	rsRetiredRe = regexp.MustCompile(rsRetiredAddressPattern)
	rsLabelRe   = regexp.MustCompile(rsRetirementLabel)
	rsLiveRe    = regexp.MustCompile(rsLiveAddressPattern)
)

// rsAddressViolation names one offending comment paragraph.
type rsAddressViolation struct {
	FirstLine int
	Text      string
}

// rsFirstRetiredOffset is the index of the FIRST retired address named in text,
// or -1 when the paragraph names none. Both retired spellings are considered —
// the verb (as a whole word) and the deleted prompt file (as a substring) — so a
// paragraph is not exempted by omitting one of them.
func rsFirstRetiredOffset(text string) int {
	first := -1
	if loc := rsRetiredRe.FindStringIndex(text); loc != nil {
		first = loc[0]
	}
	if at := strings.Index(text, rsRetiredPromptToken); at >= 0 && (first < 0 || at < first) {
		first = at
	}
	return first
}

// rsAddressViolations walks the source line by line, groups CONTIGUOUS comment
// lines into paragraphs, and reports every paragraph that presents a retired
// address WITHOUT having already said it is retired.
//
// THE RULE IS PREFIXED, NOT "anywhere in the paragraph", and that asymmetry is
// the whole design. A label anywhere in the paragraph is not enough — the
// MEASURED pre-fix paragraph carried the word "retired" three lines BELOW the
// offending address, where it applied to a DIFFERENT thing (the retired merger
// LAUNCHER's take-over prompt), and a whole-paragraph judge read that as a pass.
// A label BEFORE the address is the writer saying "what follows is history"; a
// label after it is usually about something else.
//
// Contiguity (not the whole file) is the unit on purpose: a label three hundred
// lines away does not help the reader standing on the sentence.
func rsAddressViolations(src string) (violations []rsAddressViolation, judgedLines, judgedBlocks int) {
	lines := strings.Split(src, "\n")
	var block []string
	blockStart := 0

	flush := func() {
		if len(block) == 0 {
			return
		}
		judgedBlocks++
		text := strings.Join(block, "\n")
		if first := rsFirstRetiredOffset(text); first >= 0 {
			if !rsLabelRe.MatchString(text[:first]) {
				violations = append(violations, rsAddressViolation{FirstLine: blockStart, Text: text})
			}
		}
		block = nil
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, rsCommentPrefix) {
			flush()
			continue
		}
		judgedLines++
		if len(block) == 0 {
			blockStart = i + 1 // 1-based, matching an editor's gutter
		}
		block = append(block, trimmed)
	}
	flush()
	return violations, judgedLines, judgedBlocks
}

// TestRepoSweepCommentsNameALiveMergeAddress is the real-tree arm: the shipped
// file's comments never present a retired address as the live destination, and
// they DO still name the address that works.
func TestRepoSweepCommentsNameALiveMergeAddress(t *testing.T) {
	raw, err := os.ReadFile(rsAddressGuardSourceRel)
	if err != nil {
		t.Fatalf("read the judged source %s: %v", rsAddressGuardSourceRel, err)
	}
	src := string(raw)

	violations, judgedLines, judgedBlocks := rsAddressViolations(src)
	if judgedLines < rsAddressGuardMinJudgedLines || judgedBlocks < rsAddressGuardMinJudgedBlocks {
		t.Fatalf("the scan judged only %d comment line(s) in %d paragraph(s) of %s — "+
			"floors are %d/%d, so this run did not read the file it thinks it read",
			judgedLines, judgedBlocks, rsAddressGuardSourceRel,
			rsAddressGuardMinJudgedLines, rsAddressGuardMinJudgedBlocks)
	}

	for _, v := range violations {
		t.Errorf("%s:%d names a RETIRED address as the live destination and does not say so before it:\n%s",
			rsAddressGuardSourceRel, v.FirstLine, v.Text)
	}

	// THE AFFIRMATIVE HALF. The paragraphs this file is judged on describe a
	// REAL hand-over, so removing every mention of the merge face would buy a
	// green by going silent about the behaviour. The live spelling must appear.
	if !rsLiveRe.MatchString(src) {
		t.Fatalf("%s no longer names the LIVE merge address `gopi task merge` anywhere — the not-ff "+
			"paragraph describes a hand-over that still exists, so silence is not the fix",
			rsAddressGuardSourceRel)
	}

	t.Logf("judged %d comment line(s) in %d paragraph(s) of %s: %d retired-address violation(s)",
		judgedLines, judgedBlocks, rsAddressGuardSourceRel, len(violations))
}

// TestRepoSweepAddressGuardRedProof drives the SAME predicate over the verbatim
// PRE-FIX paragraph this run repaired. A guard that has never been seen to fire
// is a guard nobody knows works.
func TestRepoSweepAddressGuardRedProof(t *testing.T) {
	const preFixParagraph = "// ③ not ff-able: LEAVE IT ALONE and say so. The tick has no mechanism left\n" +
		"// that ACTS on such a branch — no archive tag (which would permanently remove\n" +
		"// it from every candidate set), no ledger row, no worktree removal.\n" +
		"// Resolution is the merge command's job (the foreground `gopi merge`, whose\n" +
		"// shipped prompt is cmd/agent/merge_prompt.md — the retired merger\n" +
		"// launcher's take-over prompt was deleted with the daemon, epic\n" +
		"// merger-delete-dead-impl 2026-09-19), and the branch must SURVIVE for that\n" +
		"// hand-over to diff against. See repoSweepNotFFOutcome."

	violations, _, blocks := rsAddressViolations(preFixParagraph)
	if blocks != 1 {
		t.Fatalf("the fixture should be ONE paragraph, got %d", blocks)
	}
	if len(violations) != 1 {
		t.Fatalf("the pre-fix paragraph must be reported (it is the measured defect): got %d violation(s). "+
			"A guard that passes the defect verifies nothing.", len(violations))
	}
	if !strings.Contains(violations[0].Text, rsRetiredPromptToken) {
		t.Fatalf("the violation should quote the whole paragraph, including the deleted prompt file: %q",
			violations[0].Text)
	}

	// THE SECOND pre-fix shape: the retired verb with NO deleted file beside it.
	// Measured live at repo_sweep.go:438 before this run ("the same sentence
	// rides the `gopi merge` run's report") — so the predicate must fire on the
	// bare verb too, not only on the pair.
	if got, _, _ := rsAddressViolations("// and the same sentence rides the `gopi merge` run's report"); len(got) != 1 {
		t.Fatalf("the bare retired verb must be reported, got %d violation(s)", len(got))
	}

	// AND THE LABEL MUST COME FIRST: the pre-fix paragraph really does contain
	// the word "retired" (about the merger launcher) after the address, so a
	// whole-paragraph judge passes it. This control pins that ordering rule.
	if !rsLabelRe.MatchString(strings.TrimPrefix(preFixParagraph, rsCommentPrefix)) {
		t.Fatalf("the fixture is supposed to carry a LATE label — without one this arm proves nothing " +
			"about the ordering rule")
	}
}

// TestRepoSweepAddressGuardGreenProof is the other half: the repaired paragraph
// — and the shapes the file legitimately uses — must pass. Three controls, so a
// fix cannot be "label everything" or "delete the mention".
func TestRepoSweepAddressGuardGreenProof(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{
			"the repaired paragraph (labelled BEFORE the mention)",
			"// Resolution is the merge command's job (the live family spelling\n" +
				"// `gopi task merge` …). The retired one-shot `gopi merge` verb and its\n" +
				"// shipped prompt cmd/agent/merge_prompt.md are gone with the epic.",
		},
		{
			"a historical epic name (the pre-fix `--fast` arm, labelled before it)",
			"// THE MAIN CHECKOUT IS RESTORED FIRST (the epic that demanded it was\n" +
				"// named for the RETIRED `gopi merge --fast` arm — that headless\n" +
				"// spelling is gone, and the live merge face is `gopi task merge`).",
		},
		{
			"a DIFFERENT word: the retired resident daemon `gopi merger`",
			"// the `gopi merger --status` page this used to name went with the daemon",
		},
		{
			"a paragraph with no retired address at all",
			"// the sweep reports a not-ff branch and asks `gopi task merge` to hand it over",
		},
	} {
		if got, _, _ := rsAddressViolations(c.src); len(got) != 0 {
			t.Errorf("%s must not be reported, got %d violation(s): %+v", c.name, len(got), got)
		}
	}
}
