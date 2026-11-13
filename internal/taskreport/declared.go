package taskreport

// declared.go — does the report DECLARE that its own task did not finish?
//
// THE MEASURED CASE (row 01a186b9-064c, 2026-10-29). The run's own report opens
// with the sentence
//
//	# Task Report: 删光 `gopi task reopen` —— 本轮裁决 BLOCKED（前置仍未落地），交付物 = 实测取证 + 可执行删除计划
//	**Verdict: BLOCKED** — 两条由 briefing 写死的硬前置在 master 上仍双双未落地 …
//
// and states, in its own words, "本轮没有删除任何代码、没有提交、没有移动 ref" —
// zero commits, zero deletions, `git status --porcelain` empty. The clone
// channel then measured the same ground and said so in the close-out's own
// digest ("carries no commit to deliver … nothing can be delivered or
// compared") — and the SAME close-out wrote `completed` over it. Two facts
// produced by one close-out, contradicting each other, with nothing aligning
// them: the row reads as finished work while its own report says the work was
// never done.
//
// WHY THIS IS NOT A SECOND CRITERION. The channel's `nothing to deliver` exit
// is a legitimate completion for a run that was ASKED to produce nothing (a
// counter-proof, a survey — row 01a18244 fixed exactly the defect of recording
// those as `failed`). What distinguishes that shape from this one is a fact
// the report already carries and nothing read: the run said, in its own
// verdict line, that it did NOT finish. So the predicate below is a READER,
// not a judge — it answers one question about the author's own words, and the
// close-out decides what to do with the answer (Rule 14: one spelling of "did
// this run declare itself unfinished", so no second vocabulary can drift).
//
// WHAT IT DOES NOT DO. It never edits a report, never infers incompleteness
// from prose (a mention of the word "blocked" in a paragraph is not a
// verdict — the token must open its own line), and never decides a status. It
// is deliberately CONSERVATIVE: a report with no `Verdict:` line returns
// "not declared", because inventing an admission the author did not make
// would accuse every pre-existing report in this repository.

import "strings"

const (
	// VerdictLinePrefix is the token a report opens its own judgement with.
	// Rule 5's skeleton does not emit it (the skeleton states facts, not a
	// verdict), so its presence is always an author's deliberate sentence.
	VerdictLinePrefix = "Verdict:"

	// declaredMarkupChars are stripped from BOTH ends of a token line before it
	// is compared: Markdown emphasis is a way of WRITING a line, not a
	// different line, and the case above spells its verdict `**Verdict: BLOCKED**`.
	// Whitespace is in this set because it is a BOUNDARY (it ends the token),
	// never content — declaredEmphasisChars is the subset that may be deleted
	// from the middle of a sentence without changing a word.
	declaredMarkupChars = "*_`#> \t"

	// declaredEmphasisChars are the characters declaredSentence removes
	// EVERYWHERE. Whitespace is deliberately absent: the quoted sentence is a
	// phrase, and collapsing its spaces would not be "stripping emphasis".
	declaredEmphasisChars = "*_`#>"
)

// incompleteVerdicts are the values that declare "this run did NOT finish".
// Matched against the WHOLE first word (or first two words), never as a string
// prefix, so `BLOCKED-BY-UPSTREAM` is not silently read as `BLOCKED` while a
// free-text tail ("BLOCKED — 前置 A 与 B 仍未落地") still matches.
var incompleteVerdicts = [][]string{
	{"BLOCKED"},
	{"NOT", "DONE"},
	{"NOT", "FINISHED"},
	{"INCOMPLETE"},
	{"UNFINISHED"},
	{"ABORTED"},
}

// incompleteVerdictMaxWords is how many leading words a verdict value may carry
// before the comparison stops: it is the LONGEST spelling in incompleteVerdicts
// (`NOT DONE` / `NOT FINISHED`), named rather than recomputed so the reader and
// the list cannot drift (Rule 1/13).
const incompleteVerdictMaxWords = 2

// DeclaredIncomplete answers (sentence, true) when the report's own `Verdict:`
// token line declares the run unfinished, with the sentence the author wrote —
// so the caller can quote it verbatim instead of paraphrasing an admission.
//
// The scan is line-anchored after stripping ONE list marker and the leading
// markup, through the same trimClaimBullet the run-check uses (Rule 14: one
// spelling of "where a token line starts").
func DeclaredIncomplete(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		s := declaredTokenLine(line)
		if s == "" || !strings.HasPrefix(s, VerdictLinePrefix) {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(s, VerdictLinePrefix)), declaredMarkupChars)
		if incompleteVerdictValue(value) {
			return declaredSentence(value), true
		}
	}
	return "", false
}

// declaredTokenLine normalises one line to the shape a token can open: no
// indentation, at most one list marker, no leading/trailing Markdown emphasis.
func declaredTokenLine(line string) string {
	s := trimClaimBullet(strings.TrimLeft(line, " \t"))
	return strings.Trim(s, declaredMarkupChars)
}

// incompleteVerdictValue reports whether a `Verdict:` value declares that the
// run did not finish. The comparison is on the leading WORDS of the value, cut
// at the first separator, so the author's tail ("— 前置仍未落地") is recognised
// as a tail rather than as part of the word, and a value that closes its own
// emphasis (`BLOCKED** — …`) still reads as `BLOCKED`.
func incompleteVerdictValue(value string) bool {
	words := verdictWords(value)
	for _, want := range incompleteVerdicts {
		if len(words) < len(want) {
			continue
		}
		matched := true
		for i, w := range want {
			if words[i] != w {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// verdictWords returns the UPPERCASED leading words of a verdict value, cut at
// the first separator or Markdown emphasis run — the only part of the value
// that names the verdict. A value opening with no word at all returns nothing
// (an empty verdict declares nothing).
func verdictWords(value string) []string {
	var words []string
	var cur []rune
	flush := func() bool {
		if len(cur) == 0 {
			return false
		}
		words = append(words, strings.ToUpper(string(cur)))
		cur = cur[:0]
		// Two words is the longest spelling in incompleteVerdicts (Rule 13: the
		// bound is this list's own shape, not a number chosen here).
		return len(words) >= incompleteVerdictMaxWords
	}
	for _, r := range value {
		if isVerdictSeparator(r) || isDeclaredMarkup(r) {
			if flush() {
				return words
			}
			continue
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

// declaredSentence renders the author's verdict value as TEXT: Markdown
// emphasis is stripped EVERYWHERE (not only at the ends), because the value is
// quoted verbatim into a ledger row and `BLOCKED** — …` reads as a typo there.
// Nothing else is changed: the words, their order and the author's tail are
// preserved exactly.
func declaredSentence(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if strings.ContainsRune(declaredEmphasisChars, r) {
			return -1
		}
		return r
	}, value))
}

// isDeclaredMarkup reports whether r is one of the Markdown characters an author
// wraps a token in — a way of WRITING the line, not part of the word.
func isDeclaredMarkup(r rune) bool {
	return strings.ContainsRune(declaredMarkupChars, r)
}

// isVerdictSeparator reports whether r separates a verdict word from its tail.
func isVerdictSeparator(r rune) bool {
	switch r {
	case ' ', '\t', '—', '-', ':', ',', '.', ';', '(', '[', '/', '|':
		return true
	}
	return false
}
