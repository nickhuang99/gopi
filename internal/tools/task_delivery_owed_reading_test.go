package tools

// THE FOUR ACCEPTANCE ARMS OF THE DELIVERY-OWED READING (epic 01a1a8df):
//
//	① POSITIVE  — a ground holding one commit no remote ref reaches renders
//	              `delivery owed: 1 commit(s)` on `task list` AND a full
//	              four-fact block on `task show`, with a readable sha;
//	② NEGATIVE  — a ground whose commit the remote ALREADY carries (a push that
//	              succeeded) renders NO tail and NO block;
//	③ RE-DRIVEN — after the peer really holds the tip, the owed reading
//	              DISAPPEARS by itself (the probe, not a stored flag);
//	④ NEVER-COMMITTED — a `failed` row whose ground has no commit ahead of the
//	              remote (and one with NO remote refs at all) renders nothing —
//	              the misjudgement the epic forbids.
//
// Every arm drives the REAL renderers (`TaskList`, `TaskShow`) against a REAL
// git repository and the REAL probe; nothing is stubbed, so a reader that only
// looks plausible cannot pass. The fixture's shape is the host's own: a CLONE
// (`git clone <origin> <ground>`) whose `origin` is the bare repo the row
// records, so `refs/remotes/origin/*` exists and the ahead count means what it
// says.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// dlOwedFixtureTask is the row id every arm seeds; it is a uuid-shaped id
	// because the renderers use it as the row's address.
	dlOwedFixtureTask = "01a20100-0000-7000-8000-000000000301"
	// dlOwedBranch is the branch the fixture's ground stands on — a clone
	// ground's own branch, not the integration branch (the shape a post-fix
	// claim records).
	dlOwedBranch = "task/" + "01a20100000070008000000000000301"
	// dlOwedSeedClock is the fixed created_at every seeded row uses, so two
	// renders of the same ledger differ only by what the code under test did.
	dlOwedSeedClock = 1_767_000_000.0
)

// dlOwedFixture builds the whole shape: a BARE origin, a clone ground cut from
// it, and (when commit is true) one commit in the ground that the origin has
// never seen. It returns the ground directory and the ledger path with the row
// already seeded.
func dlOwedFixture(t *testing.T, status string, commit bool) (ground, dbPath, head string) {
	t.Helper()
	covLifeGitEnv(t)
	origin := covLifeGitRepo(t)

	// A clone, exactly as the channel makes one: `origin` is the bare repo the
	// row records, so refs/remotes/origin/master exists from the first fetch.
	ground = filepath.Join(t.TempDir(), "ground")
	covLifeGit(t, filepath.Dir(ground), covLifeGitBinary, "clone", "-q", origin, ground)
	covLifeGit(t, ground, covLifeGitBinary, "checkout", "-q", "-b", dlOwedBranch)

	if commit {
		covLifeWriteFile(t, filepath.Join(ground, "deliverable.txt"), "the row's work\n")
		covLifeGit(t, ground, covLifeGitBinary, "add", "deliverable.txt")
		covLifeGit(t, ground, covLifeGitBinary, "-c", "user.email=owed@example.invalid",
			"-c", "user.name=owed", "commit", "-q", "-m", "the row's deliverable")
	}
	head = covLifeGit(t, ground, covLifeGitBinary, "rev-parse", "HEAD")

	dbPath = testutil.NewTestDBPath(t)
	dlOwedSeedRow(t, dbPath, origin, ground, status)
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	return ground, dbPath, head
}

// dlOwedSeedRow writes ONE terminal row carrying the recorded deliverable, which
// is what both renderers read (never the fixture's own state).
func dlOwedSeedRow(t *testing.T, dbPath, repo, ground, status string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	deliv := taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: dlOwedBranch, Worktree: ground})
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, payload, deliverable, tags, source, created_at, updated_at)
		 VALUES (?, ?, ?, '', ?, '', 'auto', ?, ?)`,
		dlOwedFixtureTask, "the work whose publish was refused", status, deliv,
		dlOwedSeedClock, dlOwedSeedClock); err != nil {
		t.Fatalf("seed row: %v", err)
	}
}

// dlOwedList renders `task list` for the CURRENT ledger through the real verb
// function and returns the row's own line (fails when the row is absent, so an
// arm cannot pass on an empty render).
func dlOwedList(t *testing.T) string {
	t.Helper()
	out, err := TaskList("")
	if err != nil {
		t.Fatalf("task list: %v", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, dlOwedFixtureTask) {
			return line
		}
	}
	t.Fatalf("`task list` rendered no line for %s — the render is vacuous:\n%s", dlOwedFixtureTask, out)
	return ""
}

// dlOwedShow renders `task show` through the real verb function.
func dlOwedShow(t *testing.T) string {
	t.Helper()
	out, err := TaskShow(dlOwedFixtureTask)
	if err != nil {
		t.Fatalf("task show: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("task show rendered nothing — a non-vacuity floor")
	}
	return out
}

// ① POSITIVE — one commit no remote ref reaches. The LIST row carries the
// counted tail with a readable sha; the SHOW render carries all four facts.
func TestDeliveryOwedRendersForAGroundWithUnreachableCommits(t *testing.T) {
	ground, _, head := dlOwedFixture(t, taskStatusFailed, true)

	line := dlOwedList(t)
	if !strings.Contains(line, TaskDeliveryOwedSuffixLabel) {
		t.Fatalf("the list row carries no %q tail for a ground holding one unreachable commit:\n%s",
			TaskDeliveryOwedSuffixLabel, line)
	}
	if !strings.Contains(line, "1 commit(s) ahead of every remote") {
		t.Errorf("the tail does not state the count it measured:\n%s", line)
	}
	short := head[:taskDeliveryOwedShortShaLen]
	if !strings.Contains(line, short) {
		t.Errorf("the tail does not name the commit (%s is unreadable on the row):\n%s", short, line)
	}

	out := dlOwedShow(t)
	for _, want := range []string{
		TaskDeliveryOwedCommitsLabel,
		TaskDeliveryOwedHeadLabel + strings.Repeat(" ", taskShowLabelWidth-len(TaskDeliveryOwedHeadLabel)) + head,
		ground,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("`task show` does not carry %q — the delivery debt is unreadable again:\n%s", want, out)
		}
	}
	// The rescue axis is stated POSITIVELY either way: no tag holds this tip, so
	// the reader must say the commits live only in that ground (the fact that
	// makes a sweep fatal).
	if !strings.Contains(out, TaskDeliveryOwedRescueLabel) ||
		!strings.Contains(out, taskDeliveryOwedRescueRefPrefix) {
		t.Errorf("`task show` does not state the rescue axis (Rule 15 — an unreadable axis is not a silence):\n%s", out)
	}
	if !strings.Contains(out, "live ONLY in that ground") {
		t.Errorf("the rescue line must name the consequence for a tip no tag holds:\n%s", out)
	}
}

// ② NEGATIVE — the remote already carries the commit (the shape of a run whose
// publish SUCCEEDED). Neither face may claim a debt.
func TestDeliveryOwedIsSilentForADeliveredGround(t *testing.T) {
	// The commit is pushed to the origin the ground was cloned from, so
	// refs/remotes/origin/<branch> reaches it: ahead == 0, measured.
	ground, _, head := dlOwedFixture(t, taskStatusCompleted, true)
	covLifeGit(t, ground, covLifeGitBinary, "push", "-q", deliveryProofOriginRemote, "HEAD:"+dlOwedBranch)
	covLifeGit(t, ground, covLifeGitBinary, "fetch", "-q", deliveryProofOriginRemote)

	if n, _, ok, why := taskDeliveryOwedProbe(ground); !ok || n != 0 {
		t.Fatalf("fixture is not in the measured state: probe = (%d, ok=%v, %s), want 0 commits ahead; head=%s",
			n, ok, why, head)
	}
	if line := dlOwedList(t); strings.Contains(line, TaskDeliveryOwedSuffixLabel) {
		t.Fatalf("a DELIVERED ground renders a delivery-owed tail — the render would cry wolf on every pushed run:\n%s", line)
	}
	if out := dlOwedShow(t); strings.Contains(out, TaskDeliveryOwedCommitsLabel) {
		t.Fatalf("`task show` claims a debt for a ground whose commit the remote carries:\n%s", out)
	}
}

// ③ RE-DRIVEN — the owed reading is MEASURED, not stored, so the same row stops
// owing the moment the peer really holds the tip. This is the property that
// makes the re-drive arm trustworthy: nothing has to clear a flag afterwards.
func TestDeliveryOwedDisappearsOnceThePeerHoldsTheTip(t *testing.T) {
	ground, _, head := dlOwedFixture(t, taskStatusFailed, true)

	if line := dlOwedList(t); !strings.Contains(line, TaskDeliveryOwedSuffixLabel) {
		t.Fatalf("fixture is not in the measured state — the debt must be visible BEFORE the re-drive:\n%s", line)
	}

	// THE RE-DRIVE, performed the way the leg performs it: the commits reach the
	// remote the row records. (A test that instead deleted a flag would prove
	// nothing about the reading.)
	covLifeGit(t, ground, covLifeGitBinary, "push", "-q", deliveryProofOriginRemote, "HEAD:"+dlOwedBranch)
	covLifeGit(t, ground, covLifeGitBinary, "fetch", "-q", deliveryProofOriginRemote)

	// THE PEER'S OWN ANSWER, not this process's opinion: the branch really is on
	// the remote now, at this row's own tip.
	origin := covLifeGit(t, ground, covLifeGitBinary, "ls-remote", "--heads", deliveryProofOriginRemote, dlOwedBranch)
	if !strings.Contains(origin, head) {
		t.Fatalf("the push did not land — `ls-remote --heads` shows %q, want the row's tip %s", origin, head)
	}

	if line := dlOwedList(t); strings.Contains(line, TaskDeliveryOwedSuffixLabel) {
		t.Fatalf("the debt SURVIVED the delivery — the reading is not measuring:\n%s", line)
	}
	if out := dlOwedShow(t); strings.Contains(out, TaskDeliveryOwedCommitsLabel) {
		t.Fatalf("`task show` still claims the debt after the peer holds the tip:\n%s", out)
	}
}

// ④ NO MISJUDGEMENT — the two shapes that must NOT be called a debt:
//
//	(a) a failed row whose ground exists but never committed anything of its
//	    own (the run did nothing): ahead == 0;
//	(b) a failed row whose ground holds NO remote-tracking ref at all: the ahead
//	    count would be meaningless, so it is reported UNREADABLE rather than
//	    counted as owed (and the list stays silent, because that is the ordinary
//	    collected-ground shape).
//
// (b) is the arm that would have been missed by a naive `rev-list --count`
// reading: on a repo with no refs/remotes/ the command answers the WHOLE history,
// which is indistinguishable from a real debt by the number alone.
func TestDeliveryOwedIsNotClaimedForANeverCommittedRow(t *testing.T) {
	t.Run("ground with no commit of its own", func(t *testing.T) {
		ground, _, _ := dlOwedFixture(t, taskStatusFailed, false)
		n, _, ok, why := taskDeliveryOwedProbe(ground)
		if !ok {
			t.Fatalf("the probe must ANSWER for this shape, it did not: %s", why)
		}
		if n != 0 {
			t.Fatalf("a ground that never committed anything of its own measured %d commit(s) ahead, want 0", n)
		}
		if line := dlOwedList(t); strings.Contains(line, TaskDeliveryOwedSuffixLabel) {
			t.Fatalf("a never-committed row renders a delivery-owed tail:\n%s", line)
		}
		if out := dlOwedShow(t); strings.Contains(out, TaskDeliveryOwedCommitsLabel) {
			t.Fatalf("`task show` claims a debt for a run that committed nothing:\n%s", out)
		}
	})

	t.Run("ground with no remote-tracking ref", func(t *testing.T) {
		covLifeGitEnv(t)
		// A repo that is NOT a clone: it holds no refs/remotes/ entry, which is
		// the shape whose ahead count cannot be trusted.
		bare := covLifeGitRepo(t)
		dbPath := testutil.NewTestDBPath(t)
		dlOwedSeedRow(t, dbPath, bare, bare, taskStatusFailed)
		t.Setenv("GOPI_MEMORY_DB", dbPath)

		n, _, ok, why := taskDeliveryOwedProbe(bare)
		if ok {
			t.Fatalf("the probe claimed to ANSWER (%d ahead) for a repo with no remote-tracking ref — "+
				"its count is the whole history and means nothing", n)
		}
		if !strings.Contains(why, "no remote-tracking ref") {
			t.Errorf("the refusal does not name what it could not measure: %q", why)
		}
		// The LIST must not claim a COUNTED debt (that is the misjudgement the
		// epic forbids), and on this shape it states the unreadable axis instead
		// of a number — the ground is still on disk, so a work site is not
		// excluded and Rule 15 says so. The SHOW states it as UNCHECKED; both
		// surfaces refuse to render a count they could not measure.
		line := dlOwedList(t)
		if strings.Contains(line, "commit(s) ahead of every remote") {
			t.Fatalf("the list claims a COUNTED debt for a repo with no remote — the number would be the whole history:\n%s", line)
		}
		out := dlOwedShow(t)
		if !strings.Contains(out, TaskShowDeliveryOwedLabel) || !strings.Contains(out, "UNCHECKED") {
			t.Fatalf("`task show` must state the unreadable axis rather than render nothing:\n%s", out)
		}
	})
}

// dlOwedGroundExists is a non-vacuity helper: it proves the fixture's directory
// is really on disk, so an arm that goes green on a missing ground cannot happen
// by accident.
func dlOwedGroundExists(t *testing.T, ground string) {
	t.Helper()
	if st, err := os.Stat(ground); err != nil || !st.IsDir() {
		t.Fatalf("fixture ground %s is not a directory: %v", ground, err)
	}
}
