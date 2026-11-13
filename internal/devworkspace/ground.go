package devworkspace

// ground.go — the ONE ground-state resolver: "is this directory a LIVE ground,
// and WHICH model of ground is it?"
//
// WHY IT EXISTS (measured 2026-10-19, row 01a150b3): the SAME directory was read
// by two faces of this system and answered differently in the same minute.
//
//	$ gopi task show 01a150b3-9894-…        # the ledger's advisory line
//	ground (resolved now): repo=/home/nick/work/gopi worktree=/home/nick/workspace/01a150b3-… (exists) branch=master (exists) — recorded ground — verified live
//
//	$ gopi worker --resume 01a150b3-9894-…
//	gopi worker: resume: row … records worktree /home/nick/workspace/01a150b3-…, which is NOT a live linked worktree of /home/nick/work/gopi — the ground is gone …
//
// Both statements were produced by honest, independent readers of the same
// facts, and both were WRONG about the directory: it exists, it is a git work
// tree, and it is the CLONE channel's workspace — a standalone `git clone`
// (`devworkspace.Open`), which is neither a linked worktree of the row's repo
// nor a directory whose liveness may be inferred from `os.Stat`.
//
// THE MISSING VOCABULARY, not a missing check: this system has TWO ground
// models —
//
//	CLONE           the directory holds its OWN repository (.git is a DIRECTORY).
//	                It is alive whenever that repository is there; it is
//	                registered nowhere and needs no registration.
//	LINKED WORKTREE the directory is a guest of another repository (.git is a
//	                FILE naming a gitdir under <main>/.git/worktrees/…). It is
//	                alive only while that repository still lists it in
//	                `git worktree list`.
//
// — and every reader that knew only one of the two models reported the other as
// absent. The resume reader knew only the linked model (so a clone workspace
// was "the ground is gone"); the advisory knew only "does the path exist" (so a
// swept registration still read "verified live").
//
// WHAT THIS FUNCTION IS: a pure READ of facts about the DIRECTORY ITSELF — the
// kind of its `.git` entry, the gitdir that entry names, and whether the
// repository that gitdir belongs to still registers the directory. It never
// reads a settings file to decide the model (a ground is a fact about a
// directory, not about the configuration of the day), and it never creates,
// enters, repairs or removes anything (Rule 15b — a resolver that mints ground
// is not a resolver).
//
// EVERY reader of "is this ground alive?" goes through THIS function: the
// `gopi worker --resume` refusal and the ledger's `ground (resolved now)` line
// are two renderings of one answer, so they cannot disagree again. The Detail
// string is the resolver's evidence: it names exactly what was looked at, which
// is what makes the two faces' agreement checkable by a human reading one line.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// GroundModel names WHICH kind of ground a directory is. The two models are
// mutually exclusive by construction (the `.git` entry is either a directory or
// a file), and each has its own liveness rule — that is the whole reason this
// type exists (see the file header).
type GroundModel string

const (
	// GroundModelMissing: the directory has no `.git` entry at all, so it is not
	// a git work tree and there is no ground to resolve. Reported rather than
	// guessed: the model is UNDECIDABLE here, not "unknown".
	GroundModelMissing GroundModel = "missing"
	// GroundModelClone: `.git` is a DIRECTORY — the directory holds its own
	// repository (the clone channel's workspace, or an ordinary clone).
	GroundModelClone GroundModel = "clone"
	// GroundModelLinkedWorktree: `.git` is a FILE naming a gitdir — the directory
	// is a guest checkout of another repository.
	GroundModelLinkedWorktree GroundModel = "linked-worktree"
	// GroundModelBrokenLink: `.git` is a FILE whose gitdir cannot be resolved to
	// a live repository, or which names no gitdir at all. The directory LOOKS
	// like a linked worktree and its registration is gone — the shape a removed
	// worktree or a hand-deleted `<main>/.git/worktrees/<name>` leaves behind.
	GroundModelBrokenLink GroundModel = "broken-link"
)

// GroundGitEntryName is the ONE spelling of the entry whose KIND decides which
// ground model a directory is (Rules 1b/13/14: a literal `.git` typed at each
// call site is the drift this constant exists to remove — the resolver, the
// publish guard and the evidence reader all ask this one name).
const GroundGitEntryName = ".git"

// GroundModelOf answers WHICH kind of ground dir is, from the KIND of its
// `GroundGitEntryName` entry alone — the CHEAP half of ResolveGroundState, for
// callers that need the discriminator and not the liveness verdict.
//
// It exists because "is this a clone or a linked worktree?" became a question
// prose had to answer: PROJECT.md Rule 6c and the spawn seam's ground
// announcement both state DIFFERENT things about a clone and a worktree (a
// clone's commit is owed by the clone channel's delivery loop; a linked
// worktree's commit is already on its branch in the shared repo), and the
// 2026-10-27 measurement found this very run — a clone-channel ground — being
// told by its own briefing "it is a linked worktree of your task's repo".
// A sentence whose truth depends on the shape must be rendered FROM the
// measured shape, never from the channel that happens to be asking.
//
// The two models are mutually exclusive by construction (the entry is either a
// directory or a file), so this is a total function with one undecidable case:
// no entry at all reports GroundModelMissing rather than guessing.
func GroundModelOf(dir string) GroundModel {
	d := strings.TrimSpace(dir)
	if d == "" {
		return GroundModelMissing
	}
	info, err := os.Stat(filepath.Join(filepath.Clean(d), GroundGitEntryName))
	if err != nil {
		return GroundModelMissing
	}
	if info.IsDir() {
		return GroundModelClone
	}
	return GroundModelLinkedWorktree
}

// GroundState is the answer for one directory: whether a run may use it, which
// model it is, the branch it carries, the repository it belongs to, and the
// evidence the verdict was taken from.
type GroundState struct {
	// Dir is the directory the question was asked about, cleaned. It is echoed
	// rather than re-derived, so a caller can print the verdict beside the
	// question (an answer nobody can pair with its question is not evidence).
	Dir string
	// Alive is the ONE verdict both readers act on: a run may work in Dir.
	Alive bool
	// Model is WHICH kind of ground Dir is (see GroundModel).
	Model GroundModel
	// Branch is the branch checked out in Dir ("" when git cannot say).
	Branch string
	// Repo is the repository Dir belongs to: for a clone, Dir itself; for a
	// linked worktree, the main checkout the registration lives in; "" when the
	// model is missing/broken.
	Repo string
	// Rebase is the rebase state of THIS working tree, measured by asking git
	// where its rebase markers live (`git rev-parse --git-path`, ground_rebase.go)
	// — never by joining `.git/rebase-merge` onto Dir, which silently stops
	// being a question the moment `.git` is a FILE (measured 2026-10-27, row
	// 01a17a48: one sentence claimed the LINKED shape AND that the clone-shaped
	// path showed no rebase).
	//
	// It is populated for every directory git can answer about, INCLUDING a
	// directory whose ground is not alive: "the registration is gone" and "a
	// rebase is half-done" are different facts and a reader may need both.
	Rebase RebaseProbe
	// Detail is the one-line account of WHAT WAS CHECKED and what was found —
	// never a hedged "A, B, or C": each model and each failure has its own
	// sentence, because the repair differs (Rule 15/17).
	Detail string
}

// ResolveGroundState answers "may a run use this directory, and what kind of
// ground is it?" from the DIRECTORY's own facts.
//
// It is a PURE READ: no settings, no ledger, no write. The git call it makes is
// `git worktree list` on the repository a linked worktree names — the same
// listing the worktree sweep reads — and only when the directory says it is a
// linked worktree.
func ResolveGroundState(dir string) GroundState {
	st := GroundState{Dir: strings.TrimSpace(dir), Model: GroundModelMissing}
	if st.Dir == "" {
		st.Detail = "no ground: no directory was named — checked nothing"
		return st
	}
	st.Dir = filepath.Clean(st.Dir)
	gitEntry := filepath.Join(st.Dir, GroundGitEntryName)
	info, err := os.Stat(gitEntry)
	// THE SHAPE COMES FROM THE ONE DISCRIMINATOR (Rules 13/14): the switch below
	// reads the SAME fact `GroundModelOf` reads — the KIND of the `.git` entry —
	// and the rebase question is asked of GIT afterwards, so no clause of the
	// answer is a hand-joined path that only holds for one of the two shapes.
	switch {
	case err != nil:
		// THE FIRST MEASURED FACT: no `.git` entry. The sentence names the entry
		// and the two models it could have been, so the reader knows the answer
		// was taken from the directory rather than assumed.
		st.Model = GroundModelMissing
		st.Detail = "no ground: " + st.Dir + " has no .git entry (" + groundErrText(err) + ") — " +
			"checked " + gitEntry + ": a CLONE has a .git DIRECTORY and a LINKED worktree has a .git FILE, " +
			"and this directory has neither"
		// No `.git` entry: git may still answer from a PARENT repository (this
		// directory could be a subdirectory of a work tree), so the rebase
		// question is still asked and its own verdict stands on its own.
		st.Rebase = GroundRebaseProbe(st.Dir)
		return st
	case info.IsDir():
		st = groundCloneState(st, gitEntry)
	default:
		st = groundLinkedState(st, gitEntry)
	}
	st.Rebase = GroundRebaseProbe(st.Dir)
	return st
}

// groundCloneState reads the CLONE model: `.git` is a directory, so the
// directory's own repository is the ground. Liveness is a fact about that
// repository alone — nothing registers a clone, so nothing can un-register it.
func groundCloneState(st GroundState, gitEntry string) GroundState {
	st.Model = GroundModelClone
	head := filepath.Join(gitEntry, "HEAD")
	if _, err := os.Stat(head); err != nil {
		st.Alive = false
		st.Detail = "no ground: " + st.Dir + "/.git is a DIRECTORY (the CLONE model) but " + head +
			" does not exist (" + groundErrText(err) + ") — the repository in this directory is incomplete"
		return st
	}
	st.Alive = true
	st.Repo = st.Dir
	if b, err := currentBranch(context.Background(), st.Dir); err == nil {
		st.Branch = b
	}
	branch := st.Branch
	if branch == "" {
		branch = "(git could not report a branch)"
	}
	st.Detail = "alive: " + st.Dir + "/.git is a DIRECTORY → CLONE model (the directory holds its own " +
		"repository) — checked the .git kind, " + head + ", and the checked-out branch " + branch +
		"; a clone is registered nowhere, so no worktree list decides it"
	return st
}

// groundLinkedState reads the LINKED WORKTREE model: `.git` is a FILE naming a
// gitdir under another repository, and the directory is alive only while that
// repository still lists it.
func groundLinkedState(st GroundState, gitEntry string) GroundState {
	st.Model = GroundModelBrokenLink
	body, err := os.ReadFile(gitEntry)
	if err != nil {
		st.Detail = "no ground: " + st.Dir + "/.git is a FILE (the LINKED-worktree model) but it cannot be " +
			"read (" + groundErrText(err) + ") — nothing says which repository this directory belongs to"
		return st
	}
	gitdir := groundGitdirTarget(string(body), st.Dir)
	if gitdir == "" {
		st.Detail = "no ground: " + st.Dir + "/.git is a FILE that names no gitdir (" +
			groundFirstLine(string(body)) + ") — the LINKED-worktree model needs a `gitdir: <path>` target, " +
			"and without one no repository can be asked whether it registers this directory"
		return st
	}
	if info, err := os.Stat(gitdir); err != nil || !info.IsDir() {
		reason := groundErrText(err)
		if err == nil {
			reason = "not a directory"
		}
		st.Detail = "no ground: " + st.Dir + "/.git is a FILE → gitdir " + gitdir + ", which does not exist (" +
			reason + ") — the registration was removed (merged and swept, or removed by hand); " +
			"the LINKED-worktree model is dead the moment its gitdir is"
		return st
	}
	main := groundMainCheckoutOf(gitdir)
	if main == "" {
		st.Detail = "no ground: " + st.Dir + "/.git is a FILE → gitdir " + gitdir + " (which exists), but that " +
			"gitdir does not sit in a checkout (<checkout>/.git/worktrees/<name>), so the repository that " +
			"would register this directory cannot be named — refusing rather than guessing"
		return st
	}
	st.Repo = main
	registered, why := groundRegisteredWorktree(main, st.Dir)
	if !registered {
		st.Detail = "no ground: " + st.Dir + "/.git is a FILE → gitdir " + gitdir + " → repository " + main +
			", but " + main + "'s `git worktree list` does not name " + st.Dir + " (" + why + ") — " +
			"the LINKED-worktree model is alive only while the repository still registers the directory"
		return st
	}
	st.Model = GroundModelLinkedWorktree
	st.Alive = true
	if b, err := currentBranch(context.Background(), st.Dir); err == nil {
		st.Branch = b
	}
	branch := st.Branch
	if branch == "" {
		branch = "(git could not report a branch)"
	}
	st.Detail = "alive: " + st.Dir + "/.git is a FILE → LINKED-worktree model → gitdir " + gitdir + " → repository " + main +
		", whose `git worktree list` registers this directory — checked the .git kind, the gitdir target, " +
		"and the registration; branch " + branch
	return st
}

// groundGitdirTarget reads the target out of a linked worktree's `.git` file
// ("gitdir: <path>", possibly relative). "" when the line is not a gitdir
// line — the caller reports that as a shape it cannot name a repository from.
func groundGitdirTarget(body, dir string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "gitdir:") {
			continue
		}
		target := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
		if target == "" {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		return filepath.Clean(target)
	}
	return ""
}

// groundMainCheckoutOf derives the MAIN CHECKOUT from a worktree's gitdir
// (<checkout>/.git/worktrees/<name> → <checkout>). "" when the gitdir does not
// have that shape: a bare/mirror common dir names no checkout, and a caller
// must not be handed a guess.
func groundMainCheckoutOf(gitdir string) string {
	worktrees := filepath.Dir(gitdir) // …/.git/worktrees
	common := filepath.Dir(worktrees) // …/.git
	if filepath.Base(worktrees) != "worktrees" {
		return ""
	}
	if filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}

// groundRegisteredWorktree answers "does <main>'s `git worktree list` name
// dir?" and, beside the verdict, WHY it could not be read (so a git failure is
// never rendered as "not registered" — the two are different facts, Rule 15).
func groundRegisteredWorktree(main, dir string) (bool, string) {
	out, err := runCommand(context.Background(), main, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return false, "the listing failed: " + tail(err.Error()+" "+out)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		if sameDirGround(strings.TrimSpace(strings.TrimPrefix(line, "worktree ")), dir) {
			return true, ""
		}
	}
	return false, "listed " + main + "'s registered worktrees, none of them this directory"
}

// sameDirGround compares two directory spellings by their RESOLVED identity, so
// a symlinked path and a recorded path that name one directory are one
// directory (an answer of "not registered" for a registration that IS there
// would be a lie about the filesystem).
func sameDirGround(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	return ea == nil && eb == nil && ra == rb
}

// groundErrText renders a stat/read failure for a Detail line without the
// wrapping noise of os.PathError's own String (the path is already named by the
// caller's sentence).
func groundErrText(err error) string {
	if err == nil {
		return ""
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// groundFirstLine keeps a `.git` file's own words to one line for a Detail.
func groundFirstLine(body string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimRight(body, "\n"), "\n", 2)[0])
	if line == "" {
		line = "(empty)"
	}
	return line
}

// Report renders the state as the ONE line both readers print, so the resume
// refusal and the ledger's advisory line are byte-comparable when they agree.
func (s GroundState) Report() string {
	return "ground: dir=" + s.Dir + " alive=" + groundBoolText(s.Alive) + " model=" + string(s.Model) +
		" repo=" + groundAxisOrNone(s.Repo) + " branch=" + groundAxisOrNone(s.Branch) + " — " + s.Detail
}

func groundBoolText(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func groundAxisOrNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(none)"
	}
	return strings.TrimSpace(v)
}
