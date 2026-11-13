package scratch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

// The self-reap arm's judgement: the run roots a suite may REMOVE are the ones
// whose recorded owner the kernel PROVES dead. Age is deliberately not part of it
// (the family's other collector requires DefaultStaleAge = 6h, so a day's residue
// waited a full window), and a LIVE registration is never an authorisation —
// whoever it names.
//
// MEASURED context (this host, 2026-10-22): 19 `/dev/shm/suite-*-run-*` roots /
// 1.7G, all minted the same day, every `.holder` naming a pid with no /proc entry
// — i.e. every one a run that ended without the clean cleanup hook.
//
// THE DEFECT THIS FILE NOW PINS (measured 2026-10-23): the arm used to treat a
// record naming THIS process as an authorisation too, and its caller is a signal
// handler that can fire while the process is alive and about to continue
// (`cmd/agent`'s TestWorkerStopReleasesOwnWorktree self-raises SIGTERM). The live
// suite root was hollowed mid-run; four tests then failed with
// `TempDir: stat /dev/shm/suite-cmd-agent-run-755918610/…: no such file or
// directory` and all four passed when run alone.

// selfReapRoot mints a candidate in the suite-run namespace under a throwaway
// base and optionally writes an owner record for pid.
func selfReapRoot(t *testing.T, name string) string {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, name)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("mint %s: %v", root, err)
	}
	return root
}

// TestSuiteRunSelfReapNeverTakesALiveOwnersRoot is THE regression arm for the
// 2026-10-23 defect, and it is written against the EXACT root the run lost.
//
// The two shapes below are the two ways a LIVE registered owner appears:
//
//	(1) the observed run root — name `suite-cmd-agent-run-<N>` (the shape the pin
//	    mints under /dev/shm), registration naming THIS process, which is alive;
//	(2) the same, minted a moment ago under an arbitrary base.
//
// Neither may be judged collectable, and neither may be removed, moved or
// otherwise touched by this arm — the earlier spelling returned
// (true, SelfReapReasonOwn) for both, which is what hollowed the live run.
func TestSuiteRunSelfReapNeverTakesALiveOwnersRoot(t *testing.T) {
	cases := []struct {
		name string
		root string
	}{
		// The measured shape, verbatim: "suite-" + suite + "-run-" + random tail.
		{name: "observed /dev/shm run root", root: filepath.Join(t.TempDir(), "suite-cmd-agent-run-755918610")},
		{name: "the namespace helper's own spelling", root: selfReapRoot(t, scratchRunName())},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if _, err := os.Lstat(c.root); err != nil {
				if err := os.Mkdir(c.root, 0o755); err != nil {
					t.Fatalf("mint %s: %v", c.root, err)
				}
			}
			// A file INSIDE the root, so "the root was hollowed" is observable the
			// way the run observed it (a path that used to resolve, now ENOENT).
			inside := filepath.Join(c.root, "TestSomething1234", "002")
			if err := os.MkdirAll(inside, 0o755); err != nil {
				t.Fatalf("mint %s: %v", inside, err)
			}
			if err := WriteOwnerRecord(c.root, os.Getpid()); err != nil {
				t.Fatalf("register THIS (live) process in %s: %v", c.root, err)
			}
			if v := OwnerVerdictFor(c.root); v.Outcome == procname.OutcomeUnmeasurable {
				t.Fatalf("the fixture's own registration is unmeasurable (%s) — the arm would then be "+
					"refusing for the wrong reason and this test would prove nothing", v.Reason)
			}
			collectable, reason := SuiteRunSelfReapVerdict(c.root)
			if collectable {
				t.Fatalf("a root whose registered owner is THIS LIVE PROCESS was judged collectable "+
					"(%q) — that is the defect: it hollows a run that is still using the root", reason)
			}
			if !strings.Contains(reason, SelfReapReasonOwn) {
				t.Errorf("reason = %q, want it to name the live owner (%q)", reason, SelfReapReasonOwn)
			}
			removed, why, err := ReapOwnSuiteRunRoot(c.root)
			if err != nil {
				t.Fatalf("a refusal must not error: %v", err)
			}
			if removed {
				t.Fatalf("%s was removed while its registered owner (pid %d) is alive", c.root, os.Getpid())
			}
			if why == "" {
				t.Error("a refusal must explain itself (Rule 15)")
			}
			// The tree is intact, at its ORIGINAL name, with its contents: the
			// defect's second half was a move, and a move is a loss for the run.
			if _, err := os.Lstat(filepath.Join(c.root, HolderFileName)); err != nil {
				t.Fatalf("the root lost its registration: %v", err)
			}
			if _, err := os.Lstat(inside); err != nil {
				t.Fatalf("the run's own file %s is gone after the judgement: %v", inside, err)
			}
		})
	}
}

func TestSuiteRunSelfReapRefusesAnotherLiveProcess(t *testing.T) {
	root := selfReapRoot(t, scratchRunName())
	// The parent of this test binary is alive and is not us — the shape of a
	// concurrent run's root.
	other := os.Getppid()
	if other <= 1 || other == os.Getpid() {
		t.Skipf("no live other process to register (ppid=%d)", other)
	}
	if err := WriteOwnerRecord(root, other); err != nil {
		t.Fatalf("register pid %d in %s: %v", other, root, err)
	}
	collectable, reason := SuiteRunSelfReapVerdict(root)
	if collectable {
		t.Fatalf("a root registered by ANOTHER live pid was judged collectable: %s", reason)
	}
	if reason[:len(SelfReapReasonOtherLive)] != SelfReapReasonOtherLive {
		t.Errorf("reason = %q, want it to name %q", reason, SelfReapReasonOtherLive)
	}
	removed, _, err := ReapOwnSuiteRunRoot(root)
	if err != nil {
		t.Fatalf("a refusal must not error: %v", err)
	}
	if removed {
		t.Fatalf("%s was removed despite another live pid holding it — this is the data-loss direction", root)
	}
	if _, statErr := os.Lstat(root); statErr != nil {
		t.Fatalf("the refused root is gone: %v", statErr)
	}
}

func TestSuiteRunSelfReapTakesARootWhoseOwnerIsGone(t *testing.T) {
	root := selfReapRoot(t, scratchRunName())
	// A pid that cannot be live: the kernel's pid space starts above 0, and a
	// record whose incarnation no longer matches is the "recycled" arm.
	body := "v=2\npid=1\nstart=1\n"
	if err := os.WriteFile(filepath.Join(root, HolderFileName), []byte(body), 0o600); err != nil {
		t.Fatalf("write the record: %v", err)
	}
	collectable, reason := SuiteRunSelfReapVerdict(root)
	if !collectable {
		t.Fatalf("a root whose recorded owner is gone was refused: %s", reason)
	}
	if reason != SelfReapReasonOwnerDead {
		t.Errorf("reason = %q, want %q", reason, SelfReapReasonOwnerDead)
	}
}

func TestSuiteRunSelfReapRefusesAForeignPath(t *testing.T) {
	// The namespace predicate is the arm's whole safety argument: pointed at a
	// directory that is not one of this system's per-run suite roots (a task
	// worktree, another program's directory, the base itself), it must not
	// authorise a delete — even when the registration names this process.
	for _, name := range []string{"someones-worktree", "gopi-worktree-root", "suite-env"} {
		root := selfReapRoot(t, name)
		if err := WriteOwnerRecord(root, os.Getpid()); err != nil {
			t.Fatalf("register %s: %v", root, err)
		}
		collectable, reason := SuiteRunSelfReapVerdict(root)
		if collectable {
			t.Fatalf("%q was judged a suite run root: %s", name, reason)
		}
		if reason != SelfReapReasonNotOurs {
			t.Errorf("%q: reason = %q, want %q", name, reason, SelfReapReasonNotOurs)
		}
	}
}

func TestSuiteRunSelfReapRefusesAnUnregisteredRoot(t *testing.T) {
	root := selfReapRoot(t, scratchRunName())
	collectable, _ := SuiteRunSelfReapVerdict(root)
	if collectable {
		t.Fatalf("an unregistered suite root was judged collectable — the record is the evidence this arm cites")
	}
}

// scratchRunName is the namespace a suite's pin produces, spelled through the
// production markers rather than a literal (Rule 13): "suite-<suite>-run-<rand>",
// with the random tail the shape os.MkdirTemp produces.
func scratchRunName() string {
	return SuiteRunMarker + "selfreap-" + SuiteRunToken + "-" + strconv.Itoa(os.Getpid()) + "0000"
}

// The SUITE-START arm: the roots an EARLIER run of this suite left behind, whose
// owner the kernel confirms dead. Its scope is the suite's own prefix under one
// parent, so it can never reach another suite's roots, and a root naming a LIVE
// pid is kept and NAMED (the reason a reader must be able to tell "still in use"
// from "the sweep does not work").
func TestReapDeadSuiteRunRootsUnderIsScopedAndNamesItsKeeps(t *testing.T) {
	base := t.TempDir()
	pattern := SuiteRunMarker + "somelib-" + SuiteRunToken + "-"
	dead := filepath.Join(base, pattern+"1"+strconv.Itoa(os.Getpid())+"0000")
	foreign := filepath.Join(base, SuiteRunMarker+"otherlib-"+SuiteRunToken+"-22"+strconv.Itoa(os.Getpid())+"0000")
	livePid := os.Getppid()
	held := filepath.Join(base, pattern+"3"+strconv.Itoa(os.Getpid())+"0000")
	for _, dir := range []string{dead, foreign, held} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("mint %s: %v", dir, err)
		}
	}
	// dead: a pid the kernel has certainly ended, with a stale incarnation.
	if err := os.WriteFile(filepath.Join(dead, HolderFileName), []byte("v=2\npid=1\nstart=1\n"), 0o600); err != nil {
		t.Fatalf("record %s: %v", dead, err)
	}
	if err := os.WriteFile(filepath.Join(foreign, HolderFileName), []byte("v=2\npid=1\nstart=1\n"), 0o600); err != nil {
		t.Fatalf("record %s: %v", foreign, err)
	}
	if err := WriteOwnerRecord(held, livePid); err != nil {
		t.Fatalf("record %s: %v", held, err)
	}

	removed, kept, err := ReapDeadSuiteRunRootsUnder(base, pattern)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (the dead root of THIS suite)", removed)
	}
	if _, err := os.Lstat(dead); !os.IsNotExist(err) {
		t.Errorf("the dead root survived: %v", err)
	}
	// A sibling suite's root is out of scope even when its owner is dead.
	if _, err := os.Lstat(foreign); err != nil {
		t.Errorf("a SIBLING suite's root was touched: %v", err)
	}
	// A root naming another LIVE pid is kept, and the keep names why.
	if _, err := os.Lstat(held); err != nil {
		t.Fatalf("a root naming a live pid was removed: %v", err)
	}
	named := false
	for _, row := range kept {
		if strings.Contains(row, held) && strings.Contains(row, SelfReapReasonOtherLive) {
			named = true
		}
	}
	if !named {
		t.Errorf("the kept live root is not named with its reason: %v", kept)
	}
}
