// Package pushfence — the ONE question every `git push` boundary in this
// repository asks before it hands a target to git:
//
//	"is this a TEST process about to push into something that is NOT its own
//	 temp space?"
//
// It returns the refusal line to record, or "" when the push may run.
//
// WHY IT IS A PACKAGE AND NOT A FUNCTION IN internal/tools (Rule 14, Rule 43):
// the predicate was born inside the close-out chain and shipped there for a
// year; the second boundary (the push-back lane) reached it only because it
// lives in the same package. Every other boundary — the CLONE CHANNEL's own
// `git push origin HEAD:<branch>` in internal/devworkspace, and the
// report-back push in cmd/agent — could not ask it at all, because importing
// internal/tools from those places is either a cycle or a dependency they must
// not carry. That is exactly the class this package removes: ONE predicate,
// ONE sentence, askable from anywhere a push happens.
//
// THE MEASURED RESIDUE THIS ANSWERS (2026-10-26, zbook's own repository):
//
//	$ git log --format='%H|%an <%ae>|%s' feat/exists
//	3043963598210ca6d6edb4075feb389a77de7c5e|t <t@t>|work
//	89ed62db66a125cde90939468c825d4f649aa789|t <t@t>|init
//	$ git merge-base master feat/exists   → no merge base (ORPHAN ROOT)
//	$ git reflog show feat/exists
//	30439635 feat/exists@{0}: push
//
// A fixture's throwaway root (`git init` + `a.txt` + `commit -m init`, then
// `work.txt` + `commit -m work`) arrived in the operator's real repository BY
// PUSH. The predicate refuses that shape at every boundary that asks it.
//
// The two instruments, both needed:
//   - testing.Testing() is true inside `go test` (the ordinary leak), false in
//     the shipped binary;
//   - the marker env var covers RE-EXEC'D test children: a helper spawned by a
//     test is a real subprocess, so Testing() reports false there while it is
//     still test code.
package pushfence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

const (
	// OptOutEnv is the opt-OUT of the fence (may this node be the recipient of
	// a test's deliverable push?). It exists so the fence is testable and
	// operable at all: `go test` sets it around the one arm that must push to a
	// local target while the HOST still looks like a real server node.
	//
	// The name still says FINISH_CLEANUP while the fence now guards the
	// push-back lane, the clone channel and the report-back push: renaming it
	// would break every pinned reading of it, so the name is kept and the debt
	// is recorded here (Rule 15: say where the spelling and the surface
	// disagree).
	OptOutEnv = "GOPI_FINISH_CLEANUP_TEST_PUSH"

	// Marker is the substring a reader (or an assertion) matches to identify
	// the fence's refusal WITHOUT repeating the sentence — Base is rendered
	// with the target and the opt-out appended, so the literal alone is not the
	// whole reason line.
	Marker = "in-process test fence"

	// Base is the fence's refusal line (%s = the resolved target). It is a
	// report an operator reads, so it names the condition outright instead of
	// borrowing another lane's wording — a test push refused under a sentence
	// that means "single-node deployment" would itself be a silent-ish failure
	// (Rule 15).
	Base = "in-process test fence: a test process must not push a deliverable to a real node (target=%s)"

	// reasonSeparator joins a refusal's cause to the appended opt-out name (the
	// same separator the close-out chain's reasons use).
	reasonSeparator = " — "

	// The spellings LocalSide compares on, each named once and referenced
	// (Rules 1b/13/14).
	tildePrefix       = "~/"
	pathRoot          = "/"
	dotPrefix         = "./"
	dotDotPrefix      = "../"
	pathSeparator     = ":"
	homeExpansionHead = "~/"
)

// Refusal is THE question, in one call: the refusal line to record, or "" when
// the push may run.
//
// WHY ONE WRAPPER AND NOT ONE CALL PER SITE: the predicate and its sentence are
// a PAIR, and a lane that asked the predicate while rendering its own wording
// would be a second, drifting answer to the same question (Rule 14). The count
// of call sites is the coverage of the class, so the wrapper also makes the
// boundary set countable — `pushfence.Refusal(` appears once per push boundary
// and nowhere else.
func Refusal(target string) string {
	if !LeavesTestSpace(target) {
		return ""
	}
	return Reason(target)
}

// Reason renders the refusal for a target. Split out so a caller that already
// knows the answer can report it in the SAME words (never a second sentence).
func Reason(target string) string {
	return fmt.Sprintf(Base, target) + reasonSeparator + OptOutEnv + "=1 to allow a local target"
}

// LeavesTestSpace reports whether this call is an IN-PROCESS test about to push
// a deliverable to a target OUTSIDE temp/scratch space — the condition the
// fence refuses.
//
// Two halves, both required:
//
//  1. ARE WE A TEST? testing.Testing() covers `go test` in-process; the marker
//     env var covers RE-EXEC'D test children, which are real subprocesses and
//     report Testing()==false.
//  2. IS THE RECIPIENT OUTSIDE TEST SPACE? The criterion is a PATH PROPERTY,
//     never an alias spelling: a target's local side is "inside test space"
//     when it resolves under the scratch temp root the suite pins (TMPDIR,
//     published around the run) or under the operator's configured tmpdir.
//     Anything else — a peer alias such as this host's own `zbook`, or a bare
//     path outside those roots — is a real node's checkout.
//
// The opt-out exists for the case the predicate cannot see: a run whose env
// carries no temp-root pin at all (a single-package run outside TestMain, a
// hand-invoked binary), where a legitimate local-target arm would otherwise be
// refused.
func LeavesTestSpace(target string) bool {
	if strings.TrimSpace(os.Getenv(OptOutEnv)) != "" {
		return false
	}
	if !testing.Testing() {
		return false
	}
	return !InTestSpace(target)
}

// InTestSpace decides the path half: true when the bytes the push would write
// land under a root this process may write as scratch. An EMPTY target is not a
// push at all (the callers skip on it earlier, on their own reason) — never
// claimed as test space here.
func InTestSpace(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	local := LocalSide(target)
	if local == "" {
		return false
	}
	// A side that is not an absolute-or-tilde path is expanded against the
	// PEER's cwd (its home) and so is never measurable from here — refused, not
	// guessed. Otherwise the local side is resolved (tilde included) and checked
	// against the roots in force.
	if !strings.HasPrefix(local, tildePrefix) && !strings.HasPrefix(local, pathRoot) {
		return false
	}
	return resolvedLocalSideInTestSpace(local)
}

// LocalSide returns the part of a push target that names a path on THIS
// filesystem — the bytes the push would actually write — or "" when the target
// names none.
//
// It answers with GIT's OWN rule, because this fence's question is about what
// git will do with the string, not about where a colon sits in it: a target
// that OPENS with a path (`/`, `~/`, `./`, `../`) is a local directory name in
// its ENTIRETY — git never looks for a host inside it, so a colon later in the
// string belongs to the DIRECTORY NAME, not to an ssh-side/path split.
// Otherwise (no path separator before the first colon) the string is
// `<host-or-alias>:<path>`, and the path after the colon is the side to
// measure.
//
// MEASURED DEFECT (2026-09-28, master's own run of internal/tools). The pre-fix
// code split on the FIRST colon in every case, so a fixture's own local bare
// repo
//
//	/dev/shm/suite-tools-run-<N>/Test…/peer.git:~/work/GoPi
//
// had its local side read as the literal `~/work/GoPi` — resolving to the
// operator's REAL <home>/work/GoPi — and the fence REFUSED a push whose bytes
// would land entirely inside the suite's own temp root. The refusal was a false
// positive on a target git treats as local, and it contradicted the fence's own
// contract. The fence is about WHERE THE BYTES LAND; it was measuring a string
// it had mis-parsed.
func LocalSide(target string) string {
	if strings.HasPrefix(target, pathRoot) || strings.HasPrefix(target, tildePrefix) ||
		strings.HasPrefix(target, dotPrefix) || strings.HasPrefix(target, dotDotPrefix) {
		// A local path spelling in its entirety — git stops looking for a host.
		return target
	}
	i := strings.Index(target, pathSeparator)
	if i < 0 {
		return target // no colon at all: the whole string is the path
	}
	// A path separator BEFORE the colon means the colon is inside a directory
	// name (`foo/bar:baz` is one local name, not host `foo/bar`), so the whole
	// string is the path.
	if strings.Contains(target[:i], pathRoot) {
		return target
	}
	return target[i+1:]
}

// resolvedLocalSideInTestSpace resolves a local side (tilde included) and asks
// whether it lands under one of the roots in force.
func resolvedLocalSideInTestSpace(local string) bool {
	if strings.HasPrefix(local, homeExpansionHead) {
		// The tilde belongs to the RECEIVING process's home. Accepted only when
		// that home is one of the roots in force — a test that repointed TMPDIR
		// at /tmp/... and pushes to a local ~/... is still writing outside it.
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		return PathUnderAny(filepath.Join(home, strings.TrimPrefix(local, homeExpansionHead)), TestSpaceRoots())
	}
	return PathUnderAny(local, TestSpaceRoots())
}

// TestSpaceRoots collects the directories a test may legitimately write into.
// Both spellings are read (Rule 2: the env names live in their owning packages,
// and this is a reader, not a second definition).
func TestSpaceRoots() []string {
	var roots []string
	// Both names come from their owning packages (Rule 14): GOPI_TMPDIR is the
	// knob gopi publishes for its children and TMPDIR is the ambient spelling a
	// caller exports. The suite's own pin is published into TMPDIR by the
	// harness, so these two cover the in-test case without this production file
	// importing the test scaffolding.
	for _, name := range []string{config.TmpDirEnvName, scratch.TmpDirEnvName} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			roots = append(roots, v)
		}
	}
	return roots
}

// PathUnderAny reports whether path lies at or under one of the roots, compared
// on cleaned absolute paths (a textual prefix match would accept /tmp-evil for
// /tmp).
func PathUnderAny(path string, roots []string) bool {
	clean := filepath.Clean(path)
	for _, r := range roots {
		r = filepath.Clean(r)
		if clean == r || strings.HasPrefix(clean, r+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
