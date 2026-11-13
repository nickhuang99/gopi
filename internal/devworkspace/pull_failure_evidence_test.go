package devworkspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pull_failure_evidence_test.go — A REFUSED PULL MUST NAME ITS CONFLICT.
//
// THE DEFECT THIS PINS (measured 2026-10-27, git 2.43.0, task
// 01a17a4f-014f-70e6-bd00-729c4925525a). One `git pull --rebase` that hits a
// content conflict prints, in this order:
//
//	From <remote>
//	 * branch            master     -> FETCH_HEAD
//	   c3aee53..3fa22e5  master     -> origin/master
//	Rebasing (1/1)Auto-merging f.txt
//	CONFLICT (content): Merge conflict in f.txt        ← WHICH conflict, and WHERE
//	error: could not apply d74060f... <subject>        ← WHICH commit
//	hint: Resolve all conflicts manually, …            ┐
//	hint: "git add/rm <conflicted_files>", …           │ four lines that say
//	hint: You can instead skip this commit: …          │ nothing about WHICH
//	hint: To abort and get back to the state before …  ┘ pull refused
//	Could not apply d74060f... <subject>
//
// `tail()` kept the LAST six lines — the `error:` line, all four hints and the
// closing `Could not apply` — so the two lines that carry the criteria
// (`CONFLICT … in <path>` and the file name itself) were the ones cut. The
// ledger row then carried a commit id and nothing to look at: the reader
// cannot tell which file conflicted, and an id is not a file. Measured on the
// shipped channel: a row quoted `could not apply 46a448d` while `git cat-file
// -t 46a448d` answered "Not a valid object name" in every repository — the
// only clue it left was unverifiable.
//
// WHAT IS ASSERTED, and why each arm is a criterion rather than a nicety:
//
//  1. the CONFLICTED FILE is in the words        — a path is actionable, an id
//     is not;
//  2. git's own conflict subject survives        — "CONFLICT (content)" is the
//     line that says WHAT kind;
//  3. the abort's hint lines are not the body    — they answer a question
//     nobody asked (how to proceed
//     by hand in a workspace this
//     channel already reset);
//  4. the sha is MEASURED, not quoted            — `git cat-file -t <sha>` runs
//     at the moment of refusal, so
//     an object that does not exist
//     is reported as such instead of
//     being left as a lead;
//  5. Paths names the conflicted path            — the same fact in structured
//     form, for a caller that does
//     not parse text;
//  6. Report() says WHICH STEP, WHICH COMMAND, WHICH FILES — the three things a
//     reader needs that a single
//     text fragment cannot give.
func TestPullConflictBlockerNamesTheConflictedPathsNotTheAbortHints(t *testing.T) {
	origin := newOrigin(t)
	cfg := cloneCfg(t, origin)
	if _, err := Open(context.Background(), cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	// THE SAME LINE, BOTH SIDES: ours in the workspace, theirs on origin.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "ours touches the shared first line")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "add", "README.md")
	gitIn(t, origin, "commit", "-q", "-m", "theirs touches the shared first line")

	_, blk, err := AttemptDelivery(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("a content conflict is a BLOCKER, not an error: %v", err)
	}
	if blk == nil || blk.Kind != BlockerKindPull {
		t.Fatalf("want a pull blocker, got %+v", blk)
	}

	// 1. THE PATH. Without it the words name no file to look at.
	if !strings.Contains(blk.Words, "README.md") {
		t.Fatalf("the pull blocker must name the conflicted path; an id is not a file. got %q", blk.Words)
	}
	// 2. GIT'S OWN CONFLICT SUBJECT, not our paraphrase of it.
	if !strings.Contains(blk.Words, "CONFLICT") {
		t.Fatalf("git's own conflict line must survive into the words, got %q", blk.Words)
	}
	// 3. THE ABORT'S HINTS ARE NOT THE BODY. `rebase --abort` is the channel's
	// own cleanup step; its usage text answers "how do I resolve this by hand",
	// which is not the question a refused delivery asks.
	for _, hint := range []string{
		"To abort and get back to the state before",
		"You can instead skip this commit",
		`"git add/rm <conflicted_files>"`,
	} {
		if strings.Contains(blk.Words, hint) {
			t.Fatalf("the aborted rebase's hint %q must not be part of the blocker's words (it is the channel's own cleanup step, not the refusal): %q", hint, blk.Words)
		}
	}
	// 4. THE COMMIT ID IS MEASURED AT THE MOMENT OF REFUSAL. The words must
	// carry the resolution verdict git itself gives, so an id that exists is
	// evidence and an id that does not is SAID to not exist.
	if !strings.Contains(blk.Words, catFileTypeVerb) || !strings.Contains(blk.Words, "commit") {
		t.Fatalf("the words must carry `%s <sha>`'s own answer for the replayed commit, got %q", catFileTypeVerb, blk.Words)
	}
	// 5. THE SAME FACT, STRUCTURED.
	if len(blk.Paths) == 0 {
		t.Fatalf("Blocker.Paths must carry the conflicted path(s), got none (words: %q)", blk.Words)
	}
	found := false
	for _, p := range blk.Paths {
		if strings.Contains(p, "README.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Blocker.Paths must name README.md, got %q", blk.Paths)
	}
	// 6. Report(): step, command, files — the three facts a reader needs.
	report := blk.Report()
	for _, want := range []string{
		string(BlockerKindPull),
		"git pull --rebase origin " + DefaultBranch,
		"README.md",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("Blocker.Report() must carry %q (which step, which command, which files), got:\n%s", want, report)
		}
	}
	// AND THE WORKSPACE IS STILL NOT LEFT MID-REBASE: the abort still runs, it
	// is only its TEXT that no longer masquerades as the refusal.
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "rebase-merge")); statErr == nil {
		t.Fatal("the aborted rebase must not leave rebase-merge behind")
	}
}

// TestPullFailureWordsCarryAnUnresolvableCommitIDAsSuch pins the OTHER half of
// arm 4: when the replayed commit is NOT in the workspace's object database,
// the words must say so in git's own voice rather than leaving the id as a
// lead. This is the shape the shipped row was in — `could not apply 46a448d`
// with nothing behind the number.
func TestPullFailureWordsCarryAnUnresolvableCommitIDAsSuch(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", DefaultBranch)
	// The pull's own account, verbatim, naming a commit this repository does not
	// carry — the row's measured shape.
	o := pullOutcome{
		Output: "From somewhere\n" +
			"Rebasing (1/1)Auto-merging f.txt\n" +
			"CONFLICT (content): Merge conflict in f.txt\n" +
			"error: could not apply 46a448d... fix(chat-replay): a subject\n" +
			"hint: Resolve all conflicts manually, mark them as resolved with\n" +
			"hint: To abort and get back to the state before \"git rebase\", run \"git rebase --abort\".\n" +
			"Could not apply 46a448d... fix(chat-replay): a subject\n",
		UnmergedPaths: []string{"f.txt"},
		PullCmd:       pullStepCommand + DefaultBranch,
		Err:           errPullRefused,
	}
	words := pullFailureWords(context.Background(), dir, o)
	if !strings.Contains(words, "Not a valid object name") {
		t.Fatalf("an unresolvable id must be reported as unresolvable, in git's own words; got %q", words)
	}
	if !strings.Contains(words, "f.txt") {
		t.Fatalf("the conflicted path must survive, got %q", words)
	}
	if strings.Contains(words, "To abort and get back to the state before") {
		t.Fatalf("the hint block must not be part of the words, got %q", words)
	}
}

// errPullRefused is the sentinel these tests hand a synthetic pullOutcome: the
// words' composition never reads it (it answers "did the pull fail", which the
// caller already knows), and a named value keeps the fixture honest (Rule 1).
var errPullRefused = errors.New("git pull --rebase refused")
