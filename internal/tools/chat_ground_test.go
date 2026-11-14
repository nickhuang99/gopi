package tools

// chat_ground_test.go — a chat session MINTED a ground until 2026-09-29
// (epic chat-session-ground); the operator retired the creation path (epic
// chat-session-ground-retired-when-not-a-row): a chat is not a ledger row, so a
// worktree it minted could be collected by no arm and its `chat/*` branch was
// residue nothing swept. The assertions below are the REVERSE of the ones this
// file carried then, and each one is the smallest sample that can separate the
// two shapes:
//
//	① NO BRANCH, NO WORKTREE, NO DIRECTORY — a chat started in a repository's
//	   MAIN checkout changes NOTHING on disk and in git (the regression guard:
//	   the old code created `<root>/gopi-<stem>` on `chat/<stem>`).
//	② THE RESOLUTION NAMES THE CALLER'S OWN DIRECTORY and reports that it is the
//	   main checkout — the surviving contract, which is what makes the start-up
//	   notice honest.
//	③ A DIRECTORY THAT IS NOT A MAIN CHECKOUT (a linked worktree, a nested repo)
//	   reports the same way and is likewise not moved.
//	④ A NON-git DIRECTORY is reported by name, and is not a degradation: a chat
//	   needs no repository, and "there is no checkout here" is an ANSWER.
//	⑤ ONLY an unresolvable starting directory DEGRADES — the one shape in which
//	   gopi cannot say where the session's writes will land.
//	⑥ the ANNOUNCEMENT names the directory (and the checkout when there is one);
//	   the DEGRADATION line names the reason. A degraded resolution announces no
//	   directory, and a resolved one prints no degradation.
//
// The fixture is a REAL repo (git init + commit) — the shape a chat is started
// in. The worktree root is pinned exactly as it was under the old epic, so the
// "no ground was minted HERE" claim is measured against the directory grounds
// used to be created in, not merely against a path nobody looks at (Rule 31's
// lesson: assert the surface, at the place it would show).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

// chatGroundFixture pins the worktree root and builds a real repo to start a
// chat in. The ledger is NOT pinned: this epic's ground touches no ledger, and
// a test that pinned one would hide a regression that started touching it.
func chatGroundFixture(t *testing.T, name string) (repo, root string) {
	t.Helper()
	root = t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	repo = groundTestRepo(t, name)
	return repo, root
}

// chatGroundRead is the resolution under test for one session id and directory.
func chatGroundRead(t *testing.T, sessionID, dir string) ChatGroundResolution {
	t.Helper()
	return ChatGroundFor(sessionID, dir)
}

// chatGroundEntries lists a directory's entry names, for the "nothing appeared"
// half of the assertions.
func chatGroundEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestChatGroundCreatesNothingAndReportsTheMainCheckout is the headline
// contract: a chat started in a repo's MAIN checkout RESOLVES to that directory
// — and mints no worktree, no branch, no directory anywhere under the root.
//
// The old assertion here demanded the opposite (a linked worktree on
// chat/<session-id>); it is REPLACED, not relaxed, because the behaviour it
// pinned is retired — there is no code left that could satisfy it, and keeping a
// test "for the shape" would be a green husk.
func TestChatGroundCreatesNothingAndReportsTheMainCheckout(t *testing.T) {
	repo, root := chatGroundFixture(t, "chat-no-ground-repo")
	const sid = "3f2a91c4-1d77-4a0e-9b6c-5e18d2ab7f30"

	before := chatGroundEntries(t, repo)
	branchesBefore := groundBranchList(t, repo)
	worktreesBefore := groundWorktreeList(t, repo)

	got := chatGroundRead(t, sid, repo)

	if got.Degraded {
		t.Fatalf("a chat started in a repo's main checkout has a directory to report; got a degradation: %s", got.Detail)
	}
	if !SameDir(got.Dir, repo) {
		t.Fatalf("the resolution must name the directory the session stands in (%s); got %q", repo, got.Dir)
	}
	if !got.InMainCheckout {
		t.Errorf("the resolution must report that %s is a main checkout — that is the fact the start-up notice exists to say: %+v", got.Dir, got)
	}
	if !SameDir(got.Repo, repo) {
		t.Errorf("the resolution named repo %q, want the caller's checkout %q", got.Repo, repo)
	}
	// ① NOTHING WAS MINTED — the whole point of the retirement.
	if after := chatGroundEntries(t, repo); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("the session's directory gained entries — a chat must create nothing:\nbefore: %v\nafter:  %v", before, after)
	}
	if got, want := groundBranchList(t, repo), branchesBefore; got != want {
		t.Fatalf("a branch was created for the chat:\nbefore:\n%s\nafter:\n%s", want, got)
	}
	if got, want := groundWorktreeList(t, repo), worktreesBefore; got != want {
		t.Fatalf("a worktree was created for the chat:\nbefore:\n%s\nafter:\n%s", want, got)
	}
	// The old producer's namespace is the one to check by name: a new chat/*
	// ref is exactly the residue this epic exists to stop producing.
	if refs := groundBranchList(t, repo); strings.Contains(refs, ChatGroundBranchPrefix) {
		t.Fatalf("the chat namespace %q was used again — the retired creation path is back:\n%s", ChatGroundBranchPrefix, refs)
	}
	// The place grounds used to be minted, named explicitly: an EMPTY root is
	// the measurement (a test that only looked at the repo could miss a worktree
	// created outside it).
	if entries := chatGroundEntries(t, root); len(entries) != 0 {
		t.Fatalf("the worktree root %s gained %d entries (%v) — the retired epic minted grounds here and this session must mint none", root, len(entries), entries)
	}
}

// TestChatGroundOutsideAMainCheckoutIsReportedNotMoved is the second directory
// shape: a chat started in a linked worktree (or any directory that is not the
// main checkout) is reported as such and left where it is. Nothing is needed
// here either — there is no "already grounded" state to preserve, only a
// directory to name.
func TestChatGroundOutsideAMainCheckoutIsReportedNotMoved(t *testing.T) {
	repo, root := chatGroundFixture(t, "chat-no-ground-outside-repo")

	// A linked worktree of the fixture repo — the shape the operator uses when
	// they DO want a branch, taken deliberately, not by this session.
	linked := filepath.Join(t.TempDir(), "manual-worktree")
	if out, err := groundTestGit(repo, "worktree", "add", linked, "-b", "manual/branch"); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	before := groundWorktreeList(t, repo)

	got := chatGroundRead(t, "5d0f8a23-6e41-4b77-b0c9-3ea7f2d61854", linked)

	if got.Degraded {
		t.Fatalf("a directory inside a git work tree has a checkout to report; got a degradation: %s", got.Detail)
	}
	if !SameDir(got.Dir, linked) {
		t.Fatalf("the session must not be moved: got %q, want %q", got.Dir, linked)
	}
	if got.InMainCheckout {
		t.Errorf("%s is not the main checkout of %s, and the report must not claim it is: %+v", linked, repo, got)
	}
	if !SameDir(got.Repo, repo) {
		t.Errorf("the resolution named repo %q, want %q", got.Repo, repo)
	}
	if got, want := groundWorktreeList(t, repo), before; got != want {
		t.Fatalf("a worktree was added for the chat:\nbefore:\n%s\nafter:\n%s", want, got)
	}
	if entries := chatGroundEntries(t, root); len(entries) != 0 {
		t.Fatalf("the worktree root %s gained %d entries (%v)", root, len(entries), entries)
	}
}

// TestChatGroundReportsANonRepoDirectoryByAnswer pins ④: outside a git work
// tree there is no checkout to name, and that is a REPORTED ANSWER, not a
// degradation — a chat needs no repository to be useful, and the line the
// operator sees must not read like a failure.
func TestChatGroundReportsANonRepoDirectoryByAnswer(t *testing.T) {
	root := t.TempDir()
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	plain := t.TempDir() // NOT a repo: no git init

	got := chatGroundRead(t, "b17e6d40-2c95-4f31-8a08-71ce4d5b9002", plain)

	if got.Degraded {
		t.Fatalf("a non-git directory is an answer (\"no checkout here\"), not a degradation: %s", got.Detail)
	}
	if !SameDir(got.Dir, plain) {
		t.Fatalf("the session's directory = %q, want %q", got.Dir, plain)
	}
	if got.InMainCheckout {
		t.Errorf("a non-git directory is not a main checkout: %+v", got)
	}
	if !strings.Contains(got.Detail, plain) {
		t.Errorf("the report must name the directory it is about: %q does not mention %s", got.Detail, plain)
	}
	if !strings.Contains(got.Detail, "not inside a git work tree") {
		t.Errorf("the report must say WHY there is no checkout: %q", got.Detail)
	}
	// Nothing was created anywhere (there is no producer left to create it).
	// why: correct — the set is the ENTRY LIST of a temp directory that was not
	// a git repo and is empty, and the claim is "reporting writes nothing": evidence of
	// absence over a directory, not a step list. No idempotent member can exist in it,
	// because ChatGroundFor's non-repo arm returns ChatGroundResolution{Dir: base, Detail: …}
	// (internal/tools/chat_ground.go) without calling any producer — the creation path was
	// deleted, so there is no step to run at all. The positive halves are pinned in the same
	// test (got.Degraded false, got.Dir names the directory, the Detail carries the reason),
	// and the RESOLVED arm is pinned on a live repo fixture by
	// TestChatGroundCreatesNothingAndReportsTheMainCheckout.
	if entries := chatGroundEntries(t, plain); len(entries) != 0 {
		t.Errorf("the non-repo directory gained %d entries — reporting must write nothing", len(entries))
	}
}

// TestChatGroundDegradesOnlyWhenTheDirectoryIsUnresolvable pins ⑤: the one
// remaining degradation is "gopi cannot tell what directory this session is
// in" — exercised directly, because every other shape now resolves.
func TestChatGroundDegradesOnlyWhenTheDirectoryIsUnresolvable(t *testing.T) {
	t.Setenv(worktreeroots.WorktreeRootEnvName, t.TempDir())

	// An explicit empty directory with an unresolvable CWD is the shape: from a
	// directory that has been removed, os.Getwd fails and no base can be named.
	gone := filepath.Join(t.TempDir(), "removed")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(gone); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) }) //stylegate:ignore cleanup restores the cwd the ground fixture moved away from
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	got := chatGroundRead(t, "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d", "")

	if !got.Degraded {
		t.Fatalf("an unresolvable starting directory must degrade; got %+v", got)
	}
	if got.Dir != "" {
		t.Errorf("a degraded report must name no directory; got %q", got.Dir)
	}
	if !strings.Contains(got.Detail, ChatGroundDetailDegradedLabel) {
		t.Errorf("the degradation detail %q must carry %q", got.Detail, ChatGroundDetailDegradedLabel)
	}
}

// TestChatGroundNoticesNameTheDirectoryAndTheDegradation is the TEXT contract
// (acceptance ③'s human half): the start-up line names the directory (and the
// checkout, when there is one); a degraded resolution announces no directory and
// prints the reason instead.
func TestChatGroundNoticesNameTheDirectoryAndTheDegradation(t *testing.T) {
	repo, _ := chatGroundFixture(t, "chat-no-ground-announce-repo")
	const sid = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

	inCheckout := chatGroundRead(t, sid, repo)
	ann := ChatGroundAnnouncement(inCheckout)
	for _, want := range []string{inCheckout.Dir, inCheckout.Repo, "MAIN CHECKOUT"} {
		if !strings.Contains(ann, want) {
			t.Errorf("the start-up line must carry %q:\n%s", want, ann)
		}
	}
	// The retired promise must not survive in the text: a chat owns no worktree,
	// so a line that said it did would be a claim about nothing.
	for _, unwanted := range []string{"worktree", ChatGroundBranchPrefix} {
		if strings.Contains(ann, "own "+unwanted) {
			t.Errorf("the start-up line still promises a %s of its own:\n%s", unwanted, ann)
		}
	}
	// A directory with no checkout at all: the same line, WITHOUT the checkout
	// clause — there is no repository to name, and claiming one would be a fact
	// the code does not have.
	outside := ChatGroundResolution{Dir: t.TempDir(), Detail: "no ground: fixture"}
	plainAnn := ChatGroundAnnouncement(outside)
	if !strings.Contains(plainAnn, outside.Dir) {
		t.Errorf("the start-up line must name the directory:\n%s", plainAnn)
	}
	for _, unwanted := range []string{"MAIN CHECKOUT", "that is not its main checkout"} {
		if strings.Contains(plainAnn, unwanted) {
			t.Errorf("no checkout is known for %s, so the line must not claim %q:\n%s", outside.Dir, unwanted, plainAnn)
		}
	}
	// The degraded shape announces nothing and prints its reason instead — a
	// clause naming no directory is worse than no clause (Rule 15).
	degraded := chatGroundDegrade("no ground: fixture")
	if s := ChatGroundAnnouncement(degraded); s != "" {
		t.Errorf("a degraded report must announce no directory; got %q", s)
	}
	if s := ChatGroundDegradedNotice(degraded); !strings.Contains(s, degraded.Detail) {
		t.Errorf("the degradation notice must carry the reason %q; got %q", degraded.Detail, s)
	}
	if s := ChatGroundDegradedNotice(inCheckout); s != "" {
		t.Errorf("a resolved session must not print a degradation notice; got %q", s)
	}
	// The session id decides NOTHING: two different ids in the same directory
	// report identically (it used to name the ground and the branch).
	other := chatGroundRead(t, "ffffffff-0000-4000-8000-00000000000f", repo)
	if !SameDir(other.Dir, inCheckout.Dir) || other.Detail != inCheckout.Detail {
		t.Errorf("the session id still decides something: %+v vs %+v", other, inCheckout)
	}
}

// groundBranchList is `git branch --list` in repo, newline-joined.
func groundBranchList(t *testing.T, repo string) string {
	t.Helper()
	out, err := groundTestGit(repo, "branch", "--list")
	if err != nil {
		t.Fatalf("git branch --list in %s: %v\n%s", repo, err, out)
	}
	return out
}
