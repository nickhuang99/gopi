package scratch

// A PER-RUN SUITE ROOT THAT A LATER WRITER BRINGS BACK gets an owner record.
//
// THE MEASUREMENT (2026-10-22, this host, task 01a163ae). Every run of
//
//	./scripts/test-budget.sh -run '^TestResumeArmNeverTouchesALiveHolderWorktreeE2E$' ./internal/tools/
//
// left exactly ONE `/dev/shm/suite-tools-run-<N>` root containing a Test*
// directory, one numbered child (`002/memory.db`) and NO `.holder` file, and the
// collector correctly refused it — an unregistered root is not collectable, so
// only the 6h age window bounded it. `strace -f -tt -e
// trace=mkdir,mkdirat,rmdir,unlink,unlinkat,rename` over one run showed the SAME
// path created twice, 4.6ms apart:
//
//	2378753 22:36:50.692930 mkdirat(AT_FDCWD, "/dev/shm/suite-tools-run-2937975700", 0700) = 0
//	2378762 22:36:51.300338 unlinkat(7, ".holder", 0)                                  = 0
//	2378762 22:36:51.300378 unlinkat(6, "suite-tools-run-2937975700", AT_REMOVEDIR)     = 0
//	2380864 22:36:51.304962 mkdirat(AT_FDCWD, "/dev/shm/suite-tools-run-2937975700", 0755) = 0
//	2380864 22:36:51.305000 mkdirat(AT_FDCWD, ".../TestResume…E2E3589518047", 0755)      = 0
//	2380864 22:36:51.305026 mkdirat(AT_FDCWD, ".../TestResume…E2E3589518047/002", 0755)  = 0
//
// The 0700 creation is the suite's pin (`os.MkdirTemp` + `RegisterHolder`); the
// 0755 one, after the pin removed its own root, is the os.MkdirAll in
// `internal/tools`' initKnowledgeBase (`os.MkdirAll(filepath.Dir(GOPI_MEMORY_DB),
// 0o755)`) — thread 2380864 is a gopi child (pid 2380857) whose environment
// carried `GOPI_MEMORY_DB` into a `t.TempDir()` under the pin root, and it exits
// 1 immediately afterwards (SIGPIPE, the run it reported to is gone). MkdirAll
// creates the WHOLE missing chain in one call, which is why the root, its Test*
// child and `002` all appear with the same 0755 mode.
//
// WHY THE FIX IS AN OWNER RECORD AND NOT "STOP THE RECREATION": the creation is
// legitimate — a process told to open a ledger at a path creates its parent — and
// the collector must not be widened to delete an unregistered root (an unowned
// root is exactly what it must keep, Rule 15). What was missing is that the
// recreated root could never be JUDGED. With a record it is judged by the same
// ladder as every other root: its owner is the process that recreated it, and
// once that process is gone the next run of the same suite collects it at ANY
// age (ReapDeadSuiteRunRootsUnder, at suite start).
//
// SCOPE: only a name in the suite-run namespace (`suiteRunTokens`) is
// registered, and only a directory THIS CALL creates. A configured temp root or
// an operator's own directory never grows a `.holder` because gopi wrote a file
// in it, and a root somebody else created is never repossessed.

import (
	"fmt"
	"os"
	"path/filepath"
)

// MkdirAllWithSuiteRunOwner is os.MkdirAll for a directory whose creation may
// bring a per-run suite root back into existence: every suite-run-named ancestor
// THAT THIS CALL CREATES is given an owner record naming this process.
//
// It exists so a writer does not have to know the namespace rule: the check is
// here, next to `suiteRunTokens` and the one record writer, not copied into each
// caller (Rule 14 — a second copy of the predicate is a second policy).
//
// A registration failure is REPORTED and never fatal (Rule 15b): the directory
// is usable either way, and the age window remains the fallback collector.
// Creating the directory keeps os.MkdirAll's full contract, error included.
func MkdirAllWithSuiteRunOwner(dir string) error {
	created := suiteRunAncestorsToCreate(dir)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return err
	}
	for _, root := range created {
		if err := WriteOwnerRecord(root, os.Getpid()); err != nil {
			fmt.Fprintf(os.Stderr, "scratch: recreated suite run root %s: owner registration: %v\n", root, err)
			continue
		}
		// STATED, never silent: a per-run root appearing a second time is a fact
		// an operator should not have to rediscover from a tmpfs listing. It says
		// what happened and which collector now owns the consequence.
		fmt.Fprintf(os.Stderr,
			"scratch: recreated suite run root %s (written to after that run's pin had removed it) — "+
				"registered pid %d as its owner so the next run of the suite can collect it\n", root, os.Getpid())
	}
	return nil
}

// suiteRunAncestorsToCreate returns the suite-run-named ancestors of dir that do
// not exist yet, OUTERMOST FIRST — i.e. the roots os.MkdirAll is about to bring
// back. The walk stops at the first ancestor that already exists: nothing above
// it can be created, and an existing root belongs to whoever created it.
//
// The existence test is os.Lstat, not os.Stat: the answer decides whether this
// process may claim ownership, and a symlink is not a directory this call
// creates.
func suiteRunAncestorsToCreate(dir string) []string {
	var found []string
	for p := filepath.Clean(dir); ; {
		parent := filepath.Dir(p)
		if parent == p {
			break // reached the root of the path
		}
		if _, err := os.Lstat(p); err == nil {
			break // already there: not ours to claim, and neither is anything above
		}
		if suiteRunTokens(filepath.Base(p)) != nil {
			found = append(found, p)
		}
		p = parent
	}
	// Reverse: outermost first, so the reported lines read in creation order.
	for i, j := 0, len(found)-1; i < j; i, j = i+1, j-1 {
		found[i], found[j] = found[j], found[i]
	}
	return found
}
