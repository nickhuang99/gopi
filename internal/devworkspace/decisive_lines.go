package devworkspace

// decisive_lines.go — the blocker's DECISIVE LINES: the lines of a tool's output
// that NAME the decision, as opposed to the lines that merely announce that
// something was refused.
//
// THE DEFECT THIS FILE CLOSES (row 01a18124, measured on row 01a1810a): the
// delivery-retry briefing (cmd/agent's deliveryRetryBriefing) put a fixed
// ~180-character prefix IN FRONT of the tool's words, and the ledger summary is
// `[auto] ` + normalizeTaskSummary(briefing) — folded to one line and cut at
// 200 RUNES FROM THE START (internal/agent autoTaskSummaryMaxChars). The result
// was that the discriminating line
//
//	CONFLICT (content): Merge conflict in internal/tools/task_holder_resume.go
//
// never survived while the prefix did. "被 pull 挡下了" (the pull step refused)
// and "task_holder_resume.go is a CONTENT CONFLICT" are two completely
// different next moves for the agent that reads the row, and the ledger was
// only ever telling it the first one.
//
// The cure is NOT a longer cap (the same 200-rune budget has to hold: a ledger
// summary is one readable row) and NOT a content guess inside the summary
// normaliser (a fold function must not decide what matters). The cure is to put
// the DECISIVE LINE FIRST in the briefing, which is what this extractor selects
// and what cmd/agent's briefing assembler orders.
//
// WORDING PER KIND, FROM THE TOOL'S OWN VOCABULARY: each BlockerKind is refused
// by a different tool, so each kind gets its own pattern table (below). Patterns
// are matched case-insensitively as SUBSTRINGS — git's `error:` prefix, a
// porcelain STATUS column and a compiler's `undefined:` all sit mid-line by
// design — and the matched text is returned VERBATIM, never reworded: a reworded
// failure is one the reader has to re-derive.
//
// SEGMENTS, NOT NEWLINE LINES, and that is deliberate. This package's own Words
// builders COMPOSE several tool lines into one before the blocker is made —
// `pullFailureWords` joins git's lines with pullBodyLineJoiner inside a part and
// the parts with pullPartJoiner, and `tail` joins the last lines of a build/push
// output with tailLineJoiner. Splitting on "\n" alone would hand back a
// 600-rune composed string as one "line", i.e. exactly the thing being cut. So
// the extractor splits on the SAME joiners those builders use (they are named
// constants, referenced from both sides — Rule 1b/13); every piece it returns is
// still a verbatim fragment of what the tool printed.

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/textguard"
)

// decisiveEllipsis is the ONE marker for a line this file shortened: the
// ledger's own (internal/textguard.Ellipsis, Rule 14 — the value lives once, so
// a cut decisive line and a cut summary cannot look different to a reader).
const decisiveEllipsis = textguard.Ellipsis

const (
	// decisiveLineMaxPerBlocker bounds how many decisive lines one blocker hands
	// back: the FIRST one is the one that reaches a 200-rune summary, so the cap
	// keeps the briefing readable rather than narrowing the selection.
	decisiveLineMaxPerBlocker = 3
	// decisiveLineMaxRunes bounds ONE decisive line, and it is chosen against the
	// LEDGER budget, not by taste: `[auto] ` (AutoTaskSummaryPrefix, 7 runes) +
	// this cap must stay inside autoTaskSummaryMaxChars (200) so the FIRST
	// decisive line survives whole. A longer line is cut at the head (where the
	// deciding token lives) and says so with the shared ellipsis — never silently.
	decisiveLineMaxRunes = 160

	// The announcement a refusal opens with, dropped from the selection: every
	// blocker says it, so keeping it crowded out the line that differs. `git
	// pull --rebase origin master refused (exit: 1)` is the measured instance.
	pullRefusedAnnouncementMarker = "refused (exit:"
	// The plumbing the fetch half of `pull --rebase` prints before the answer:
	// `From <url>` and its `remote:` lines. Measured on the row-01a1810a
	// transcript — they were the first thing a summary reader saw.
	fetchSourceLinePrefix = "From "
	gitRemoteLinePrefix   = "remote:"
)

// kindDecisivePatterns is the PER-KIND vocabulary of a decision: for each kind,
// the tokens (lower-case, matched case-insensitively) whose presence makes a
// line decisive. An empty table means "this kind's words ARE the decision" (see
// decisiveByConstruction).
var kindDecisivePatterns = map[BlockerKind][]string{
	// `git pull --rebase` refuses with these; the first two name WHICH FILE and
	// WHICH COMMIT, which is the difference between "resolve a conflict" and
	// "the remote moved, pull again".
	BlockerKindPull: {
		"conflict (content)",
		"conflict (",
		"conflict:",                    // git's short form: "CONFLICT (modify/delete):"
		"could not apply ",             // the commit the rebase stopped on
		"would be overwritten",         // untracked file in the way
		"cannot pull with rebase",      // uncommitted work (also BlockerKindDirty's git wording)
		"not possible to fast-forward", // divergent branches
		"you have not concluded",       // a rebase/merge already in progress
		"error: ",
		"fatal: ",
	},
	// The DECLARED build command, run through /bin/sh: a Go/Vet/Make failure in
	// its own spelling. `position:` is the compiler's `file.go:line:col:` shape
	// and it is the strongest signal there is (it names the file).
	BlockerKindBuild: {
		"position:", // synthetic marker, replaced by the shape test below
		"undefined:",
		"cannot use ",
		"declared and not used",
		"syntax error",
		"missing return",
		"not enough arguments",
		"too many arguments",
		"vet: ",
		"make: *** ",
		"fail",
		"error",
	},
	// The clean gate is `git status --porcelain`; its LISTING is the decision and
	// it has no vocabulary — every line is a two-column status plus a path, so
	// this kind is matched by SHAPE (porcelainStatusLine), and the table only
	// carries what git says when it refuses the pull over that listing.
	BlockerKindDirty: {
		"cannot pull with rebase",
		"you have unstaged changes",
	},
	// `git push` refused: the two bracketed rejections are git's own words for
	// the two different causes (the remote moved; a hook said no).
	BlockerKindPush: {
		"! [rejected]",
		"! [remote rejected]",
		"non-fast-forward",
		"failed to push",
		"protected branch",
		"error: ",
		"fatal: ",
	},
	// The disk kind's vocabulary IS the shared marker list
	// (scratch.DiskExhaustionMarkers), referenced rather than re-spelled
	// (Rule 43): the decisive line is the one that says the FILESYSTEM refused
	// the write, and that is exactly the phrase the classifier matched on.
	BlockerKindDiskExhaustion: diskExhaustionDecisivePatterns(),
}

// decisiveByConstruction reports whether a kind's Words are the decision ITSELF
// rather than a tool's output to search: the empty, already-delivered,
// inherited-delivery and man kinds are statements this channel composed
// ("carries no commit to deliver", the man gate's stale-page sentence), so
// their FIRST segment is the answer by construction. It is a distinct function,
// not an empty pattern table, so a reader can tell "there is nothing to search"
// from "the pattern table is missing".
func decisiveByConstruction(k BlockerKind) bool {
	return k == BlockerKindEmpty || k == BlockerKindAlreadyDelivered || k == BlockerKindInheritedDelivery ||
		k == BlockerKindMan
}

// porcelainStatusLine reports whether s is a `git status --porcelain` entry: a
// two-character status (X=index, Y=worktree, each one of  MADRCU?!) followed by
// a space and a path. It exists because the dirty kind's decision has no
// vocabulary — the SHAPE is the statement, and `" M <path>"` vs `"M  <path>"`
// (unstaged vs staged) is a distinction a trimmed line would destroy.
func porcelainStatusLine(s string) bool {
	if len(s) < 4 || s[2] != ' ' {
		return false
	}
	// AN UNTRACKED ENTRY IS NOT A DECISION HERE: the clean gate itself ignores
	// `??` (git's rebase tolerates it — see cleanGate's contract), so a briefing
	// that led with `?? scratch.log` would point the agent at a path that is not
	// blocking anything.
	if s[0] == '?' && s[1] == '?' {
		return false
	}
	const statusSpace = " MADRCU?!"
	return strings.IndexByte(statusSpace, s[0]) >= 0 && strings.IndexByte(statusSpace, s[1]) >= 0
}

// compilerPositionLine reports whether s carries a `<file>:<line>:<col>:` prefix
// — the compiler's own way of naming WHERE, which is the one thing a build
// refusal must not lose.
func compilerPositionLine(s string) bool {
	rest := s
	for i := 0; i < 2; i++ {
		_, after, ok := strings.Cut(rest, ":")
		if !ok {
			return false
		}
		digits := 0
		for _, r := range after {
			if r < '0' || r > '9' {
				break
			}
			digits++
		}
		if digits == 0 {
			return false
		}
		rest = after[digits:]
	}
	_, _, ok := strings.Cut(rest, ":")
	return ok
}

// BlockerDecisiveNote says, in the briefing, either WHAT was extracted or why
// nothing could be: it is the "NOT silent" half of this extractor (Rule 15).
type BlockerDecisiveNote struct {
	// Lines are the decisive lines, verbatim, deduped, at most
	// decisiveLineMaxPerBlocker of them, each at most decisiveLineMaxRunes runes.
	Lines []string
	// Why is empty when Lines is non-empty, and a complete sentence otherwise,
	// naming the wording that was looked for (or the fact that the output was
	// empty / plumbing only).
	Why string
	// Dropped counts the segments skipped because they only ANNOUNCE the refusal
	// (the exit-code line, the fetch's `From`/`remote:` plumbing). It is measured
	// so the briefing can say "the output was not empty, it was plumbing" instead
	// of implying the tool said nothing.
	Dropped int
}

// Found reports whether the extractor recognised anything.
func (n BlockerDecisiveNote) Found() bool { return len(n.Lines) > 0 }

// DecisiveLines returns the lines of the tool's output that name the DECISION
// this blocker carries — per-kind vocabulary, verbatim, deduped, bounded.
//
// It never returns silence for output it could not read: a selection that found
// nothing says so in BlockerDecisiveNote.Why, naming what it looked for and how
// many segments it looked at.
func (b *Blocker) DecisiveLines() BlockerDecisiveNote {
	if b == nil {
		return BlockerDecisiveNote{Why: "没有可读的 blocker：这次交付尝试没有返回任何阻挡记录"}
	}
	segments := b.decisiveSegments()
	note := BlockerDecisiveNote{Lines: make([]string, 0, decisiveLineMaxPerBlocker)}
	for _, seg := range segments {
		if b.announcementSegment(seg) {
			note.Dropped++
			continue
		}
		if !b.decisiveSegment(seg) {
			continue
		}
		shown := truncateRunes(seg, decisiveLineMaxRunes)
		if !containsExact(note.Lines, shown) {
			note.Lines = append(note.Lines, shown)
		}
		if len(note.Lines) == decisiveLineMaxPerBlocker {
			break
		}
	}
	if note.Found() {
		return note
	}
	return b.explainNoDecisiveLine(note, len(segments))
}

// decisiveSegments splits Words on every joiner this package's own builders use,
// keeping order. See the file header: the builders COMPOSE tool lines, so a
// newline split alone would return a composed string as one "line".
func (b *Blocker) decisiveSegments() []string {
	words := strings.TrimRight(b.Words, "\n")
	if strings.TrimSpace(words) == "" {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(words, "\n") {
		for _, part := range strings.Split(raw, pullPartJoiner) {
			for _, seg := range strings.Split(part, pullBodyLineJoiner) {
				seg = strings.TrimRight(seg, "\r")
				if strings.TrimSpace(seg) == "" {
					continue
				}
				out = append(out, seg)
			}
		}
	}
	return out
}

// announcementSegment reports whether a segment only says that SOMETHING was
// refused: the `git pull --rebase origin <base> refused (exit: N)` line and the
// fetch's own `From <url>` / `remote: …` plumbing. Every blocker starts with
// these; none of them decides anything.
func (b *Blocker) announcementSegment(seg string) bool {
	if b.Kind != BlockerKindPull {
		return false
	}
	trimmed := strings.TrimSpace(seg)
	if strings.HasPrefix(trimmed, fetchSourceLinePrefix) || strings.HasPrefix(trimmed, gitRemoteLinePrefix) {
		return true
	}
	return strings.Contains(trimmed, pullRefusedAnnouncementMarker)
}

// decisiveSegment applies the per-kind test to one segment.
func (b *Blocker) decisiveSegment(seg string) bool {
	if decisiveByConstruction(b.Kind) {
		// The FIRST segment IS the decision; the caller's cap keeps the rest out.
		return true
	}
	if b.Kind == BlockerKindDirty && porcelainStatusLine(seg) {
		return true
	}
	if b.Kind == BlockerKindBuild && compilerPositionLine(seg) {
		return true
	}
	lower := strings.ToLower(seg)
	for _, pat := range kindDecisivePatterns[b.Kind] {
		if pat == "position:" { // a shape, not a token — see compilerPositionLine
			continue
		}
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// explainNoDecisiveLine is the HONEST non-answer (Rule 15): it names what was
// looked for, how many segments were read, and — when the output was plumbing
// only — that the tool did say something, just not the deciding line.
func (b *Blocker) explainNoDecisiveLine(note BlockerDecisiveNote, segments int) BlockerDecisiveNote {
	switch {
	case segments == 0:
		note.Why = fmt.Sprintf("没能抽出判定行：%s 这一步的阻挡记录里没有任何工具输出（0 行），不是没有判定行",
			b.Kind)
	case note.Dropped > 0:
		note.Why = fmt.Sprintf("没能抽出判定行：读了 %d 段输出，其中 %d 段只是「被拒了」这类通告（退出码、fetch 的 From/remote: 行），"+
			"剩下的没有出现 %s 这一步的判定措辞（%s）", segments, note.Dropped, b.Kind,
			strings.Join(kindDecisivePatterns[b.Kind], " / "))
	default:
		note.Why = fmt.Sprintf("没能抽出判定行：读了 %d 段输出，没有一段出现 %s 这一步的判定措辞（%s）",
			segments, b.Kind, strings.Join(kindDecisivePatterns[b.Kind], " / "))
	}
	return note
}

// truncateRunes cuts s to at most n runes and marks the cut, so a shortened
// line is never mistaken for the whole one. Rule 1/13: the marker is the shared
// ellipsis, referenced, not typed.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + decisiveEllipsis
}

// containsExact reports whether xs holds s exactly (the dedupe predicate: two
// different tool lines that differ only in case are two different facts).
func containsExact(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
