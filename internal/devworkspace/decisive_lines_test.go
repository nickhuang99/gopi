package devworkspace

// decisive_lines_test.go — the extractor's own arms. They are here, not in
// cmd/agent, because the per-kind VOCABULARY lives here: a build failure and a
// dirty listing are recognised by different tokens, and a test that only ever
// exercises the pull case would let the other two tables rot.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// pullConflictWords is the PRODUCTION composition of a real `pull --rebase`
// refusal (devworkspace.pullFailureWords on row 01a1810a's transcript): the exit
// announcement, then git's own lines joined with pullBodyLineJoiner, then the
// conflicted paths — parts joined with pullPartJoiner.
const pullConflictWords = "git pull --rebase origin master refused (exit: 1) | " +
	"From peer1:/home/user/work/gopi ; * branch            master     -> FETCH_HEAD ; " +
	"138f611..f2d6d39  master     -> origin/master ; Rebasing (1/1)Auto-merging cmd/agent/cli_resume.go ; " +
	"Auto-merging internal/tools/task_holder_resume.go ; " +
	"CONFLICT (content): Merge conflict in internal/tools/task_holder_resume.go ; " +
	"error: could not apply db17b50... fix(resume): resolve a task-id target through its row's run directory | " +
	"conflicted paths (git diff --name-only --diff-filter=U): internal/tools/task_holder_resume.go"

// TestBlockerDecisiveLines_PullNamesTheConflict is the epic's core assertion at
// the extractor level: the FIRST decisive line is git's CONFLICT line, naming the
// file, and it is a VERBATIM fragment of what git printed.
func TestBlockerDecisiveLines_PullNamesTheConflict(t *testing.T) {
	blk := &Blocker{Kind: BlockerKindPull, Words: pullConflictWords,
		Paths: []string{"internal/tools/task_holder_resume.go"}}
	note := blk.DecisiveLines()
	if !note.Found() {
		t.Fatalf("the extractor recognised nothing in a real conflict transcript: %s", note.Why)
	}
	first := note.Lines[0]
	for _, want := range []string{"CONFLICT (content)", "internal/tools/task_holder_resume.go"} {
		if !strings.Contains(first, want) {
			t.Fatalf("the first decisive line must carry %q, got %q (all: %v)", want, first, note.Lines)
		}
	}
	// VERBATIM: the returned line must be findable in the words it came from —
	// a reworded failure is one the reader has to re-derive (Rule 15).
	if !strings.Contains(pullConflictWords, first) {
		t.Fatalf("the decisive line was REWORDED: %q is not a fragment of the tool's output", first)
	}
	// THE ANNOUNCEMENTS ARE MEASURED, NOT SILENTLY DROPPED: the exit line and the
	// fetch plumbing are counted so the briefing can say the output was not empty.
	if note.Dropped != 2 {
		t.Fatalf("dropped announcements = %d, want 2 (the refused(exit:) line and the `From <url>` line): %v",
			note.Dropped, note.Lines)
	}
	if len(note.Lines) > decisiveLineMaxPerBlocker {
		t.Fatalf("kept %d lines, over the named cap %d", len(note.Lines), decisiveLineMaxPerBlocker)
	}
	for _, line := range note.Lines {
		if n := utf8.RuneCountInString(line); n > decisiveLineMaxRunes+1 { // +1 = the ellipsis
			t.Fatalf("a decisive line is %d runes, over the named cap %d: %q", n, decisiveLineMaxRunes, line)
		}
	}
}

// TestBlockerDecisiveLines_BuildNamesTheCompilerFailure pins the build table:
// the compiler's `file.go:line:col: undefined: X` is the decision, and a bare
// `make: *** … Error 1` is not (it says THAT it failed, not WHAT failed).
func TestBlockerDecisiveLines_BuildNamesTheCompilerFailure(t *testing.T) {
	blk := &Blocker{Kind: BlockerKindBuild,
		Words: "ok  \tgithub.com/nickhuang99/gopi/internal/agent\t1.2s | " +
			"# github.com/nickhuang99/gopi/internal/devworkspace | " +
			"./decisive_lines.go:118:14: undefined: decisiveLineJoiner | " +
			"make: *** [Makefile:42: build] Error 1"}
	note := blk.DecisiveLines()
	if !note.Found() {
		t.Fatalf("the build extractor recognised nothing: %s", note.Why)
	}
	if !strings.Contains(note.Lines[0], "undefined: decisiveLineJoiner") {
		t.Fatalf("the first decisive build line must be the compiler's, got %q (all: %v)", note.Lines[0], note.Lines)
	}
	if !strings.Contains(note.Lines[0], "decisive_lines.go:118:14") {
		t.Fatalf("the decisive build line must carry the compiler's position: %q", note.Lines[0])
	}
}

// TestBlockerDecisiveLines_DirtyKeepsThePorcelainColumns pins the dirty table:
// this kind's tool has no vocabulary, so the SHAPE decides — and the two-column
// status must survive verbatim, because `" M <path>"` (unstaged) and
// `"M  <path>"` (staged) are different git facts.
func TestBlockerDecisiveLines_DirtyKeepsThePorcelainColumns(t *testing.T) {
	blk := &Blocker{Kind: BlockerKindDirty,
		Words: " M internal/tools/task_holder_resume.go\nM  internal/tools/tasks.go\n?? scratch.log\n"}
	note := blk.DecisiveLines()
	if !note.Found() {
		t.Fatalf("the dirty extractor recognised nothing: %s", note.Why)
	}
	if !strings.HasPrefix(note.Lines[0], " M ") {
		t.Fatalf("the unstaged entry lost its leading column: %q", note.Lines[0])
	}
	// UNTRACKED LINES ARE NOT THE DECISION here (the gate itself ignores `??`),
	// so the kept set is the two tracked entries and nothing else.
	for _, line := range note.Lines {
		if strings.HasPrefix(line, "??") {
			t.Fatalf("an untracked entry must not be a decisive line: %v", note.Lines)
		}
	}
}

// TestBlockerDecisiveLines_NoRecognisedLineSaysWhy is Rule 15's arm at the
// extractor: silence is never the answer — the note explains what was looked for
// and how much output was read, distinguishing "plumbing only" from "no output".
func TestBlockerDecisiveLines_NoRecognisedLineSaysWhy(t *testing.T) {
	plumbingOnly := &Blocker{Kind: BlockerKindPull,
		Words: "git pull --rebase origin master refused (exit: 1) | From peer1:/home/user/work/gopi"}
	note := plumbingOnly.DecisiveLines()
	if note.Found() {
		t.Fatalf("plumbing must not be a decisive line: %v", note.Lines)
	}
	if !strings.Contains(note.Why, "没能抽出判定行") || !strings.Contains(note.Why, "段") {
		t.Fatalf("the non-answer must say it could not extract AND how much it read: %q", note.Why)
	}
	empty := &Blocker{Kind: BlockerKindPush}
	emptyNote := empty.DecisiveLines()
	if emptyNote.Why == "" || !strings.Contains(emptyNote.Why, "没有任何工具输出") {
		t.Fatalf("an empty Words must be reported as an empty output, not as a missing decision: %q", emptyNote.Why)
	}
	var nilBlk *Blocker
	if got := nilBlk.DecisiveLines(); got.Why == "" {
		t.Fatal("a nil blocker must explain itself rather than return an empty note")
	}
}

// TestBlockerDecisiveLines_EmptyKindIsItsOwnDecision pins decisiveByConstruction:
// the empty kind carries this channel's OWN sentence, so its first
// segment IS the decision — searching them for a tool's vocabulary would find
// nothing and report a false "could not extract".
func TestBlockerDecisiveLines_EmptyKindIsItsOwnDecision(t *testing.T) {
	blk := &Blocker{Kind: BlockerKindEmpty,
		Words: "carries no commit to deliver: `git rev-list --count origin/master..HEAD` printed 0"}
	note := blk.DecisiveLines()
	if !note.Found() {
		t.Fatalf("the empty kind's own sentence IS the decision, but nothing was kept: %s", note.Why)
	}
	if !strings.Contains(note.Lines[0], "carries no commit to deliver") {
		t.Fatalf("the kept line must be the channel's own measurement: %q", note.Lines[0])
	}
}

// TestBlockerDecisiveLines_AlreadyDeliveredKindIsItsOwnDecision is the same
// contract for the FIFTH kind (row 01a182df): its words are composed by this
// channel too ("carries no commit to deliver — but this run's delivery already
// landed (…): … reachable from master"), so its first segment IS the decision.
// Without the entry in decisiveByConstruction a reader searching it for a tool's
// vocabulary would find nothing and report a false "could not extract" — the
// exact false negative the empty kind's arm exists to prevent, one kind over.
func TestBlockerDecisiveLines_AlreadyDeliveredKindIsItsOwnDecision(t *testing.T) {
	blk := &Blocker{Kind: BlockerKindAlreadyDelivered,
		Words: "carries no commit to deliver — but this run's delivery already landed (abc1234): " +
			"the tip this run pushed is reachable from master"}
	note := blk.DecisiveLines()
	if !note.Found() {
		t.Fatalf("the already-delivered kind's own sentence IS the decision, but nothing was kept: %s", note.Why)
	}
	if !strings.Contains(note.Lines[0], deliveryAlreadyDeliveredMarker) {
		t.Fatalf("the kept line must carry the channel's own measurement (%q): %q",
			deliveryAlreadyDeliveredMarker, note.Lines[0])
	}
}
