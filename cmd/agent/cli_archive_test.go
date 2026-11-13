package main

// cli_archive_test.go — the `gopi task archive` verb's routing/behaviour contract.
//
// Separate from cli_contracts_test.go on purpose: that suite pins ROUTING
// (`-h` reaches the handler, no agent-loop fallthrough). This file pins the
// GATE — the epic hand-archive-gate's rule that a branch whose task row is
// completed/verified cannot be buried by a bare `git tag`, and that the
// recorded reason reads back off the ref.
//
// Runs the REAL binary against a temp git repo + a temp ledger
// (GOPI_MEMORY_DB), because the subject is the interaction of three real
// systems: git refs, the SQLite ledger, and the argv parser.

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/taskverbs"
	"github.com/nick/gopi/internal/tools"
)

// archCLIRepo builds a temp git repo with one branch, returning its path.
func archCLIRepo(t *testing.T, branch string) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "master"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-q", "-m", "seed"},
		{"branch", branch},
	} {
		// A git read against a temp repo, bounded by the go-test VERBOSE
		// default rather than by a value this line owns (Rule 2): the failure
		// this closes was a `CombinedOutput` parked for 9 minutes with no
		// ceiling at all, which turned one hung child into the whole package's
		// 600s budget (measured 2026-09-22: cmd/agent's suite never finished).
		ctx, cancel := context.WithTimeout(context.Background(), archCLIGitTimeout)
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return repo
}

// archCLIGitTimeout bounds every git child the archive CLI tests spawn.
const archCLIGitTimeout = archCLIGitIdleTimeout

// archCLIGitIdleTimeout is the ceiling ARCHIVE CLI TESTS put on their own git
// children (Rule 27: a test that can park the whole package's budget must have
// a ceiling of its own; this number is a TEST limit, never a product timeout,
// so it lives here beside its user and not in CONFIG.md's knob table).
const archCLIGitIdleTimeout = 60 * time.Second

// archCLISeedRow writes one ledger row owning branch, in the given status,
// through the package's own writers (the legal transition path), then stamps
// the deliverable branch via the EXISTING test seam
// (tools.MergeVerifySetDeliverableBranchForTest, from verify_gate.go) — the
// same shape production's backfill writes, so the fixture cannot drift from
// what the gate detects on. No new exported API is added for a test.
func archCLISeedRow(t *testing.T, taskID, status, branch string) {
	t.Helper()
	if _, err := tools.TaskNew("archive cli fixture "+taskID, taskID, "", ""); err != nil {
		t.Fatalf("TaskNew(%s): %v", taskID, err)
	}
	steps := []string{status}
	if status == tools.TaskStatusVerified {
		steps = []string{tools.TaskStatusCompleted, tools.TaskStatusVerified}
	}
	if status == tools.TaskStatusInProgress {
		steps = nil // TaskNew's default
	}
	for _, s := range steps {
		if _, err := tools.TaskUpdate(taskID, s); err != nil {
			t.Fatalf("TaskUpdate(%s -> %s): %v", taskID, s, err)
		}
	}
	if err := tools.MergeVerifySetDeliverableBranchForTest(taskID, t.TempDir(), branch); err != nil {
		t.Fatalf("stamp deliverable %s/%s: %v", taskID, branch, err)
	}
}

// TestArchiveCLIRefusesACompletedOwnerThenRecordsTheReason is the end-to-end
// contract of the epic: the refusal blocks the write, and the override records
// a reason a later reader can query.
func TestArchiveCLIRefusesACompletedOwnerThenRecordsTheReason(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/done")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	// CONVERTED (epic archive-ref-reclaim, 2026-10-01): the tip is captured
	// BEFORE the archive because a successful archive now CARRIES THE DECISION
	// THROUGH — it removes refs/heads/<branch>, which is the whole point of the
	// reclaim arm. The old form read the tip back through the branch ref name
	// after the write, which is the state this epic exists to eliminate.
	doneTip := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "feat/done"))

	// Seed the ledger IN-PROCESS at the same GOPI_MEMORY_DB the probe binary
	// is handed, so both sides read one file. t.Setenv (never os.Setenv) —
	// the value is restored for sibling tests.
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	archCLISeedRow(t, "t-done", tools.TaskStatusCompleted, "feat/done")

	out, err := runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, taskverbs.Archive, "feat/done", "--reason", "superseded")
	if err == nil {
		t.Fatalf("expected a non-zero exit for a completed owner; got exit 0:\n%s", out)
	}
	if !strings.Contains(out, "t-done") || !strings.Contains(out, "override-completed") {
		t.Fatalf("refusal does not name the row and the override:\n%s", out)
	}
	if tags := archCLITags(t, repo); len(tags) != 0 {
		t.Fatalf("a REFUSED archive still wrote %v", tags)
	}

	// The override: a named reason + the acknowledgement.
	out, err = runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, taskverbs.Archive, "feat/done", "--reason", "superseded",
		"--detail", "landed as feat/other", "--override-completed")
	if err != nil {
		t.Fatalf("override failed: %v\n%s", err, out)
	}
	if tags := archCLITags(t, repo); len(tags) != 1 {
		t.Fatalf("override wrote %v, want exactly one archive tag", tags)
	}
	// Readback through git: the archive tag is UNCHANGED in shape (lightweight,
	// at the decided commit — what every reader treats it as), and the WHY is
	// readable from its companion ref.
	if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "archive/feat/done")); got != doneTip {
		t.Fatalf("archive ref resolves to %s, want the decided commit %s (the gate must not re-point the ref)", got, doneTip)
	}
	// AND THE DECIDED REF IS GONE — the carry-through half (epic
	// archive-ref-reclaim): a tag that leaves its branch ref behind is the
	// residue every other lane skips forever.
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet",
		"refs/heads/feat/done").Output(); err == nil {
		t.Fatalf("the decided branch ref survived the archive (%q)", strings.TrimSpace(string(out)))
	}
	if got := archCLIObjectType(t, repo, "archive/feat/done"); got != "commit" {
		t.Fatalf("archive ref object type = %q, want commit (a lightweight ref, as every lane writes)", got)
	}
	if rec := archCLIReasonRecord(t, repo, "feat/done"); !strings.Contains(rec, "reason: superseded") {
		t.Fatalf("reason record does not carry the code:\n%s", rec)
	}
}

// TestArchiveCLIRefusesALiveOwnerDespiteACancelledSibling is the REAL-VERB
// regression for the four-owner-row incident (zbook 2026-10-07, run 01a115c3,
// branch task/01a113e467767eb9a4c33cb1df1cdfbb): two rows named one branch, the
// `cancelled` one was the pick, and `gopi task archive <branch> --reason
// hand-mistake` exited 0 with `archive/<branch>` written over a run that was
// still building.
//
// It is driven through the BINARY, not through ArchiveDecide, because the
// incident was an exit code and a ref: a unit test of the pick would have
// passed while the operator's command still minted the tag. The assertions are
// therefore the operator's own three readings — exit status, the refusal text,
// and `git tag -l`.
//
// The decided-owner override (the path the epic keeps working) is exercised on
// a SEPARATE branch in the same ledger, so the refusal is shown to be about the
// live owner and not about the ledger being unusable.
func TestArchiveCLIRefusesALiveOwnerDespiteACancelledSibling(t *testing.T) {
	bin := gopiTestBinPath(t)
	const liveBranch = "task/01a113e467767eb9a4c33cb1df1cdfbb"
	repo := archCLIRepo(t, liveBranch)
	archCLIGit(t, repo, "branch", "feat/finished")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// The dead row is seeded FIRST — the measured order, and the one the
	// pre-fix scan kept.
	archCLISeedRow(t, "01a114aa-eb19-dead", tools.TaskStatusCancelled, liveBranch)
	archCLISeedRow(t, "01a113e4-6799-live", tools.TaskStatusInProgress, liveBranch)
	// The decided-owner arm below needs its own owner.
	archCLISeedRow(t, "t-finished", tools.TaskStatusCompleted, "feat/finished")

	out, err := runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, taskverbs.Archive, liveBranch, "--reason", "hand-mistake")
	if err == nil {
		t.Fatalf("a LIVE run's ground was archived by hand (exit 0):\n%s", out)
	}
	if !strings.Contains(out, "IN FLIGHT") {
		t.Fatalf("the refusal must say the work is in flight:\n%s", out)
	}
	if !strings.Contains(out, "01a113e4-6799-live") {
		t.Errorf("the refusal must name the LIVE owner, not the cancelled sibling:\n%s", out)
	}
	if tags := archCLITags(t, repo); len(tags) != 0 {
		t.Fatalf("a REFUSED archive still wrote %v", tags)
	}
	// No trace at all: the reason record is the archive's other half and must
	// not exist either, and the branch ref must still be there (the near miss
	// was not only the tag — a tag armed the reclaim arm that deletes this ref).
	if out, err := exec.Command("git", "-C", repo, "cat-file", "blob",
		"refs/gopi/archive-reason/"+liveBranch).CombinedOutput(); err == nil {
		t.Fatalf("the refusal wrote a reason record: %s", out)
	}
	if _, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet",
		"refs/heads/"+liveBranch).Output(); err != nil {
		t.Fatalf("the refusal removed the live branch ref: %v", err)
	}

	// The override arm still works (the epic's accepted operator flow): a
	// COMPLETED owner plus --override-completed writes the tag.
	finishedTip := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "feat/finished"))
	if out, err := runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, taskverbs.Archive, "feat/finished", "--reason", "superseded",
		"--override-completed"); err != nil {
		t.Fatalf("the decided-owner override was broken by this fix: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "archive/feat/finished")); got != finishedTip {
		t.Fatalf("override wrote archive/feat/finished at %s, want the decided commit %s", got, finishedTip)
	}
}

// TestArchiveCLICheckReproducesAndClearsTheBacklog is the operator's half: the
// --check verb must FIND a reason-less tag over a completed row (exit 1) and go
// clean (exit 0) once the reason is recorded.
func TestArchiveCLICheckReproducesAndClearsTheBacklog(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/buried")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	archCLISeedRow(t, "t-buried", tools.TaskStatusCompleted, "feat/buried")
	// The 21:04 shape, exactly: a bare lightweight tag.
	for _, args := range [][]string{
		{"tag", "archive/feat/buried", "feat/buried"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}

	out, err := runArchiveProbe(t, bin, home, repo, dbPath, CliVerbTask, taskverbs.Archive, "--check")
	if err == nil {
		t.Fatalf("--check exited 0 with a reason-less archive over a completed row:\n%s", out)
	}
	if !strings.Contains(out, "archive/feat/buried") || !strings.Contains(out, "t-buried") {
		t.Fatalf("--check report does not name the tag and its row:\n%s", out)
	}

	// Remediate, then the check must be green.
	if out, err := runArchiveProbe(t, bin, home, repo, dbPath, CliVerbTask, taskverbs.Archive, "feat/buried",
		"--reason", "hand-mistake", "--override-completed"); err != nil {
		t.Fatalf("remediation failed: %v\n%s", err, out)
	}
	out, err = runArchiveProbe(t, bin, home, repo, dbPath, CliVerbTask, taskverbs.Archive, "--check")
	if err != nil {
		t.Fatalf("--check still red after remediation: %v\n%s", err, out)
	}
	if !strings.Contains(out, "clean") {
		t.Fatalf("clean --check report does not say so:\n%s", out)
	}
}

// TestArchiveCLIRejectsAnUnknownReason — the vocabulary is closed at the argv
// boundary too (the parser refuses before any git call).
func TestArchiveCLIRejectsAnUnknownReason(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/x")
	out, err := runArchiveProbe(t, bin, t.TempDir(), repo, filepath.Join(t.TempDir(), "memory.db"),
		CliVerbTask, taskverbs.Archive, "feat/x", "--reason", "because-i-said-so")
	if err == nil {
		t.Fatalf("an unknown reason was accepted:\n%s", out)
	}
	if !strings.Contains(out, "unknown --reason") {
		t.Fatalf("rejection does not name the problem:\n%s", out)
	}
	if tags := archCLITags(t, repo); len(tags) != 0 {
		t.Fatalf("a rejected invocation wrote %v", tags)
	}
}

// TestTaskArchiveCarriesTheBranchOperandToTheSameEngine is the CONTRACT of the
// re-homed branch archive (epic verb-restructure, 2026-10-05): `gopi task
// archive <branch>` must be the SAME command as `gopi task archive <branch>` — one
// engine, one tag, one reason record — and the two operands of the verb must be
// told apart by what the operand RESOLVES TO.
//
// THE TWO ARMS, both driven through the real binary here:
//
//   - an operand that IS a ledger row retires the ROW (tags += resolved) and
//     writes NO ref — the pre-existing meaning, unchanged;
//   - an operand that is NOT a row is a BRANCH and gets the archive lane.
//
// The discrimination is the part worth pinning: if the resolver ever stopped
// asking the ledger, the row arm would become a branch archive (a tag written
// for a task id) or the branch arm would never be reached. The test therefore
// drives a REAL seeded row id AND a REAL branch in the SAME repo/ledger, and
// asserts each landed in its own shape.
func TestTaskArchiveCarriesTheBranchOperandToTheSameEngine(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/rehomed")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// Seed one TERMINAL row, so the row arm is legal and its operand resolves.
	archCLISeedRow(t, "t-rehomed-row", tools.TaskStatusCompleted, "feat/other")
	// A SECOND row, FAILED, owning feat/rehomed: the branch arm below must
	// PROCEED with the reason the operator named. Without an owner the gate
	// forces the reason to ArchiveReasonUnowned (a branch nothing tracked gets
	// no story told about it) — correct behaviour, but it would make this test
	// assert the wrong thing about what `--reason` does.
	archCLISeedRow(t, "t-rehomed-fail", tools.TaskStatusFailed, "feat/rehomed")

	// --- arm 1: the operand is a ROW ---------------------------------------
	out, err := runArchiveProbe(t, bin, t.TempDir(), repo, dbPath,
		CliVerbTask, taskverbs.Archive, "t-rehomed-row")
	if err != nil {
		t.Fatalf("`gopi task archive <row id>` failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "resolved") {
		t.Errorf("the row arm must stamp/resolve the row — its output names no resolution:\n%s", out)
	}
	if tags := archCLITags(t, repo); len(tags) != 0 {
		t.Errorf("a ROW operand wrote archive tags %v — the operand was mistaken for a branch", tags)
	}

	// --- arm 2: the operand is a BRANCH -------------------------------------
	tip := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "feat/rehomed"))
	out, err = runArchiveProbe(t, bin, t.TempDir(), repo, dbPath,
		CliVerbTask, taskverbs.Archive, "feat/rehomed", "--reason", "superseded")
	if err != nil {
		t.Fatalf("`gopi task archive <branch>` failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "archive/feat/rehomed")); got != tip {
		t.Fatalf("archive/feat/rehomed = %q, want the branch tip %q — the re-homed spelling wrote a different ref",
			got, tip)
	}
	if reason := archCLIReasonRecord(t, repo, "feat/rehomed"); !strings.Contains(reason, "superseded") {
		t.Errorf("the reason record must carry the decision through the re-homed spelling:\n%s", reason)
	}
	// --- arm 3: the OPERAND-LESS modes -------------------------------------
	// --check / --reclaim / --verify-commit name NO branch, and the parser must
	// not demand one: the top-level page advertises `gopi task archive --check`, so
	// the re-homed spelling owes the same three invocations. Before this arm
	// existed, `gopi task archive --check` answered "missing required
	// parameter: task_id" — a page advertising what the dispatcher refuses.
	chk, err := runArchiveProbe(t, bin, t.TempDir(), repo, dbPath, CliVerbTask, taskverbs.Archive, "--check")
	if err != nil {
		t.Fatalf("`gopi task archive --check` must run the backlog sweep: %v\n%s", err, chk)
	}
	if !strings.Contains(chk, "archive backlog") {
		t.Errorf("`gopi task archive --check` must print the sweep's report:\n%s", chk)
	}
	// Bare, with no operand and no mode, stays the historical refusal — the
	// modes above are the ONLY zero-operand shapes that are not a mistake.
	bare, err := runArchiveProbe(t, bin, t.TempDir(), repo, dbPath, CliVerbTask, taskverbs.Archive)
	if err == nil {
		t.Fatalf("`gopi task archive` with nothing at all must be refused:\n%s", bare)
	}
	if !strings.Contains(bare, "task_id") {
		t.Errorf("bare `gopi task archive` must keep the row-archive refusal (it names task_id), not the branch one:\n%s", bare)
	}

	// The refusal contract travels with the engine: an unknown reason is still
	// refused, and its error line names the spelling the OPERATOR typed.
	bad, err := runArchiveProbe(t, bin, t.TempDir(), repo, dbPath,
		CliVerbTask, taskverbs.Archive, "feat/decided", "--reason", "because-i-said-so")
	if err == nil {
		t.Fatalf("the re-homed spelling accepted an unknown reason:\n%s", bad)
	}
	if !strings.Contains(bad, "task archive: unknown --reason") {
		t.Errorf("the refusal must name the spelling the operator typed (`gopi task archive`), not the top-level one:\n%s", bad)
	}
}

// --- probes -----------------------------------------------------------------

// runArchiveProbe runs the real binary with an isolated GOPI_HOME and the given
// repo as cwd, returning combined output and the exec error (nil = exit 0).
// The env comes from the package's own cliContractEnv — the scrubbed-env
// discipline lives in ONE place, so a new probe cannot accidentally inherit an
// ambient GOPI_* var.
func runArchiveProbe(t *testing.T, bin, home, repo, dbPath string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, familyAddressArgs(args)...)
	cmd.Dir = repo
	cmd.Env = append(cliContractEnv(home), "GOPI_MEMORY_DB="+dbPath)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func archCLITags(t *testing.T, repo string) []string {
	t.Helper()
	return strings.Fields(archCLIGit(t, repo, "tag", "-l", "archive/*"))
}

func archCLIObjectType(t *testing.T, repo, ref string) string {
	t.Helper()
	return strings.TrimSpace(archCLIGit(t, repo, "cat-file", "-t", ref))
}

// archCLIReasonRecord reads a branch's reason record through the CLI's own
// documented path (`git cat-file blob refs/gopi/archive-reason/<branch>`).
func archCLIReasonRecord(t *testing.T, repo, branch string) string {
	t.Helper()
	return archCLIGit(t, repo, "cat-file", "blob", "refs/gopi/archive-reason/"+branch)
}

func archCLIGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// --- epic archive-ref-reclaim: the decided ref is CARRIED THROUGH ------------
//
// Before the fix every lane that honoured an archive tag SKIPPED the branch and
// none removed it, so refs/heads/feat/capture-self-window sat at the same commit
// as its own archive tag (measured e1701ac8) forever. These two probes pin the
// operator-visible half: the verb removes the decided ref on the way out, and
// `--reclaim` does the same for the residue that is already there.

// TestArchiveCLICarriesTheDecisionThroughToTheRef: after a successful archive
// the TAG exists and the BRANCH REF is gone, at the same commit.
func TestArchiveCLICarriesTheDecisionThroughToTheRef(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/decided-ref")
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	tip := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "feat/decided-ref"))
	out, err := runArchiveProbe(t, bin, t.TempDir(), repo, dbPath,
		CliVerbTask, taskverbs.Archive, "feat/decided-ref", "--reason", "superseded")
	if err != nil {
		t.Fatalf("archive failed: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet",
		"refs/heads/feat/decided-ref").Output(); err == nil {
		t.Fatalf("the decided branch ref still resolves to %q — the decision was not carried through",
			strings.TrimSpace(string(out)))
	}
	if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "archive/feat/decided-ref")); got != tip {
		t.Fatalf("archive tag = %s, want the branch tip %s (the tag is the durable half)", got, tip)
	}
}

// TestArchiveCLIReclaimRemovesTheResidueAndReportsRefusals: `--reclaim` cleans
// the branches whose tag and ref still agree, KEEPS one that moved after the
// decision (reporting both commits), and the operator sees both facts.
func TestArchiveCLIReclaimRemovesTheResidueAndReportsRefusals(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/residue")
	// A second branch that MOVES after its archive tag is written — the refusal
	// case, in the same sweep.
	for _, args := range [][]string{
		{"branch", "feat/moved"},
		{"tag", "archive/feat/residue", "feat/residue"},
		{"tag", "archive/feat/moved", "feat/moved"},
		{"checkout", "-q", "feat/moved"},
		{"commit", "--allow-empty", "-q", "-m", "work after the decision"},
		{"checkout", "-q", "master"},
	} {
		archCLIGit(t, repo, args...)
	}
	moved := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "feat/moved"))

	out, err := runArchiveProbe(t, bin, t.TempDir(), repo, filepath.Join(t.TempDir(), "memory.db"),
		CliVerbTask, taskverbs.Archive, "--reclaim")
	if err != nil {
		t.Fatalf("--reclaim failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "deleted=1 kept=1") {
		t.Fatalf("reclaim tally must show one deletion and one refusal:\n%s", out)
	}
	// The residue is gone …
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet",
		"refs/heads/feat/residue").Output(); err == nil {
		t.Fatalf("feat/residue survived the reclaim (%q)", strings.TrimSpace(string(out)))
	}
	// … the moved branch is KEPT, at its moved tip …
	if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "feat/moved")); got != moved {
		t.Fatalf("a divergent branch was moved: %s → %q", moved, got)
	}
	// … and BOTH tags survive (this arm never deletes a tag).
	for _, tag := range []string{"archive/feat/residue", "archive/feat/moved"} {
		if strings.TrimSpace(archCLIGit(t, repo, "rev-parse", tag)) == "" {
			t.Fatalf("tag %s vanished — the tag is never deleted", tag)
		}
	}
	// A SECOND sweep is IDEMPOTENT about acting: it deletes nothing further. The
	// divergent row is REPORTED AGAIN, deliberately — a standing refusal that
	// only spoke once would hide a branch a human still has to look at, the same
	// choice the sweep's not-ff reporting makes.
	out, err = runArchiveProbe(t, bin, t.TempDir(), repo, filepath.Join(t.TempDir(), "memory.db"),
		CliVerbTask, taskverbs.Archive, "--reclaim")
	if err != nil {
		t.Fatalf("second --reclaim failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "deleted=0 kept=1") {
		t.Fatalf("second sweep must delete nothing further and keep re-reporting the refusal:\n%s", out)
	}
}

// TestArchiveCLIReclaimAnswersATaskGroundFromTheFacts is the OPERATOR-FACING
// half of the task-ground repair (2026-10-12). Before it, `--reclaim` refused
// every `task/<id>` branch by name prefix with the sentence "is the integration
// branch or a task-run ground — never a reclaim candidate", so a ground whose
// ledger row was gone and which no worktree held survived every sweep forever.
//
// Every invocation below is the REAL binary against a REAL repo and a REAL
// (isolated) ledger, and the ledger is seeded through the project's own writers
// (tools.TaskNew / MergeVerifySetDeliverableBranchForTest), so the facts the arm
// reads are facts the ledger really holds (Rule 31 — the surface is run, not
// described).
func TestArchiveCLIReclaimAnswersATaskGroundFromTheFacts(t *testing.T) {
	bin := gopiTestBinPath(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	home := t.TempDir()

	const (
		orphanID     = "01a0f44c2d0f7a82bde8f76445daa302"
		liveID       = "01a0f44c2d0f7a82bde8f76445daa303"
		orphanBranch = "task/" + orphanID
		liveBranch   = "task/" + liveID
	)

	// ── the UNOWNED, unheld ground: the residue this repair is about ────────
	orphanRepo := archCLIRepo(t, orphanBranch)
	archCLIGit(t, orphanRepo, "tag", "archive/"+orphanBranch, orphanBranch)
	orphanTip := strings.TrimSpace(archCLIGit(t, orphanRepo, "rev-parse", orphanBranch))

	out, err := runArchiveProbe(t, bin, home, orphanRepo, dbPath,
		CliVerbTask, taskverbs.Archive, "--reclaim")
	if err != nil {
		t.Fatalf("--reclaim over an unowned task ground failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "deleted=1") {
		t.Fatalf("an unowned, unheld task ground must be reclaimed:\n%s", out)
	}
	if got := archCLIGitNoFatal(orphanRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+orphanBranch); got != "" {
		t.Fatalf("%s survived the reclaim: %q", orphanBranch, got)
	}
	// The tag is the durable half and is never touched.
	if got := strings.TrimSpace(archCLIGit(t, orphanRepo, "rev-parse", "archive/"+orphanBranch)); got != orphanTip {
		t.Fatalf("archive tag = %q, want %q (the tag must survive)", got, orphanTip)
	}

	// ── the ground a NON-TERMINAL ledger row still tracks ───────────────────
	liveRepo := archCLIRepo(t, liveBranch)
	archCLIGit(t, liveRepo, "tag", "archive/"+liveBranch, liveBranch)
	archCLISeedRow(t, liveID, tools.TaskStatusInProgress, liveBranch)
	liveTip := strings.TrimSpace(archCLIGit(t, liveRepo, "rev-parse", liveBranch))

	out, err = runArchiveProbe(t, bin, home, liveRepo, dbPath,
		CliVerbTask, taskverbs.Archive, "--reclaim")
	if err != nil {
		t.Fatalf("--reclaim over a live task ground failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "skipped=1") {
		t.Fatalf("a live ledger row must keep the ground:\n%s", out)
	}
	if !strings.Contains(out, liveID) || !strings.Contains(out, tools.TaskStatusInProgress) {
		t.Fatalf("the refusal must NAME the row and its status:\n%s", out)
	}
	if got := archCLIGitNoFatal(liveRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+liveBranch); strings.TrimSpace(got) != liveTip {
		t.Fatalf("%s was moved though its row is in flight: %q, want %q", liveBranch, got, liveTip)
	}

	// ── the ground a REGISTERED WORKTREE holds ─────────────────────────────
	heldRepo := archCLIRepo(t, "task/01a0f44c2d0f7a82bde8f76445daa304")
	heldBranch := "task/01a0f44c2d0f7a82bde8f76445daa304"
	heldTree := filepath.Join(t.TempDir(), "held-wt")
	archCLIGit(t, heldRepo, "tag", "archive/"+heldBranch, heldBranch)
	archCLIGit(t, heldRepo, "worktree", "add", "-q", heldTree, heldBranch)

	out, err = runArchiveProbe(t, bin, home, heldRepo, dbPath,
		CliVerbTask, taskverbs.Archive, "--reclaim")
	if err != nil {
		t.Fatalf("--reclaim over a held task ground failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "skipped=1") || !strings.Contains(out, heldTree) {
		t.Fatalf("the worktree refusal must name the tree %q:\n%s", heldTree, out)
	}
	if got := archCLIGitNoFatal(heldRepo, "rev-parse", "--verify", "--quiet", "refs/heads/"+heldBranch); strings.TrimSpace(got) == "" {
		t.Fatal("a branch a worktree holds was reclaimed — Rule 29 violated")
	}

	// ── master/main stay refused BY NAME, with no fact asked ───────────────
	masterRepo := archCLIRepo(t, "feat/ordinary")
	archCLIGit(t, masterRepo, "tag", "archive/master", "master")
	out, err = runArchiveProbe(t, bin, home, masterRepo, dbPath,
		CliVerbTask, taskverbs.Archive, "--reclaim")
	if err != nil {
		t.Fatalf("--reclaim with an archived master failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "is the integration branch") || !strings.Contains(out, "skipped=1") {
		t.Fatalf("master must stay refused by name:\n%s", out)
	}
	if archCLIGitNoFatal(masterRepo, "rev-parse", "--verify", "--quiet", "refs/heads/master") == "" {
		t.Fatal("master was reclaimed")
	}
}

// archCLIGitNoFatal runs a git read that is ALLOWED to fail (a probe for an
// absent ref), returning "" instead of failing the test.
func archCLIGitNoFatal(repo string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestArchiveCLIRefusesTheIntegrationBranchBeforeAnyWrite is the REAL-VERB
// regression for the measured defect 01a12f0d (sager, 2026-10-12): in a
// throwaway repo,
//
//	$ gopi task archive master --reason hand-mistake --detail probe
//	archived master as archive/master
//	  reason: unowned
//	  read back: git cat-file blob refs/gopi/archive-reason/master
//	  skipped          archive/master  branch master is the integration branch — never a reclaim candidate
//	rc=0
//
// — ONE invocation that says it archived master and that master was skipped,
// with BOTH refs really written (`git tag --list archive/master` non-empty;
// `refs/gopi/archive-reason/master` resolving). refs/tags/archive/<branch> is
// the project's decided-EXCLUSION namespace and every merge path reads it as "a
// loser was decided here" (PROJECT.md Rule 9), so those two refs marked the
// INTEGRATION HEAD as decided-against.
//
// Driven through the BINARY because the defect was an exit code and two refs — a
// unit test of the gate would have passed while the operator's command still
// minted the tag. The assertions are the operator's own readings:
//
//	(a) the refusal is printed and the exit status is NON-ZERO,
//	(b) `git tag --list 'archive/master'` is empty,
//	(c) `git rev-parse --verify refs/gopi/archive-reason/master` FAILS,
//	(d) the CONTROL — an ordinary unowned branch — still archives (tag written,
//	    exit 0), so a refuse-everything implementation cannot pass this test.
func TestArchiveCLIRefusesTheIntegrationBranchBeforeAnyWrite(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/x")
	archCLIGit(t, repo, "branch", "main")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()

	for _, branch := range []string{"master", "main"} {
		tip := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "refs/heads/"+branch))
		out, err := runArchiveProbe(t, bin, home, repo, dbPath,
			CliVerbTask, taskverbs.Archive, branch, "--reason", "hand-mistake", "--detail", "probe")
		if err == nil {
			t.Fatalf("archiving the integration branch %s exited 0:\n%s", branch, out)
		}
		if !strings.Contains(out, "refused") || !strings.Contains(out, "integration branch") {
			t.Fatalf("the refusal does not say what happened and why:\n%s", out)
		}
		// The pre-fix output's own sentence: it must not appear as the fate of a
		// branch this invocation claimed to have ARCHIVED (post-fix, a refusal
		// mentions no "archived <branch>" line at all).
		if strings.Contains(out, "archived "+branch) {
			t.Fatalf("the output still claims it archived %s while refusing:\n%s", branch, out)
		}
		// (b) no archive tag.
		if tags := archCLITags(t, repo); len(tags) != 0 {
			t.Fatalf("%s: a REFUSED archive wrote %v", branch, tags)
		}
		// (c) no reason ref.
		if got := archCLIGitNoFatal(repo, "rev-parse", "--verify", "--quiet", "refs/gopi/archive-reason/"+branch); got != "" {
			t.Fatalf("%s: a REFUSED archive wrote refs/gopi/archive-reason/%s = %s", branch, branch, got)
		}
		// The branch itself is untouched.
		if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "refs/heads/"+branch)); got != tip {
			t.Fatalf("%s: the branch moved on a refused archive (%s → %s)", branch, tip, got)
		}
	}
	if out := strings.TrimSpace(archCLIGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/tags/archive/")); out != "" {
		t.Fatalf("the archive namespace is not empty after the refusals:\n%s", out)
	}

	// (d) THE CONTROL: an ordinary unowned branch still archives, exit 0.
	ctrlTip := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "feat/x"))
	out, err := runArchiveProbe(t, bin, home, repo, dbPath,
		CliVerbTask, taskverbs.Archive, "feat/x", "--reason", "hand-mistake", "--detail", "probe")
	if err != nil {
		t.Fatalf("the control archive failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "archive/feat/x")); got != ctrlTip {
		t.Fatalf("archive/feat/x = %q, want the branch tip %q", got, ctrlTip)
	}
	if got := archCLIGitNoFatal(repo, "cat-file", "blob", "refs/gopi/archive-reason/feat/x"); !strings.Contains(got, "reason: unowned") {
		t.Fatalf("the control archive's reason record is missing or wrong: %q", got)
	}
}

// TestArchiveCLIBranchLaneHonoursRepoFlag is the REAL-VERB regression for the
// measured defect 01a1301b (sager, 2026-10-12): `--repo` was parsed, advertised
// in the usage text and HONOURED by every census mode (--check/--reclaim/
// --unreachable/--verify-commit), while the BRANCH lane called
// tools.ArchiveBranchCwd, which resolved the repo from the PROCESS CWD and never
// looked at opts.Repo. Three invocations — `--repo <fixture>`, no `--repo` at
// all, and `--repo /nonexistent-xyz` — produced ONE byte-identical output and
// one exit status, and the write died with git's own
//
//	fatal: Failed to resolve 'fix/some-failed-attempt' as a valid ref.
//
// a sentence about a MISSING BRANCH, for a branch the operator had asked to
// archive in ANOTHER repo.
//
// Driven through the BINARY, because the defect was an exit code and a ref: a
// unit test of the parser would have passed while the operator's command
// resolved the wrong repo. Three readings, all the operator's own:
//
//	(a) `--repo <target>` with the cwd in a DIFFERENT repo archives INTO the
//	    target (its `refs/tags/archive/<branch>` exists; the cwd's repo is
//	    untouched, and the branch ref the cwd's repo owns is still there),
//	(b) a `--repo` naming a path that is not a repo is REFUSED (non-zero exit,
//	    a message naming git's own failure) and writes NOTHING anywhere — never
//	    a silent fallback to the cwd (Rule 15), and
//	(c) THE CONTROL: with no `--repo` the cwd's own repo is still what gets the
//	    tag, so the fix cannot be "always use the last path seen".
func TestArchiveCLIBranchLaneHonoursRepoFlag(t *testing.T) {
	bin := gopiTestBinPath(t)

	// target = where --repo points; cwd = a DIFFERENT repo the process stands in.
	target := archCLIRepo(t, fixtureBranch)
	cwd := archCLIRepo(t, cwdBranch)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	targetTip := strings.TrimSpace(archCLIGit(t, target, "rev-parse", fixtureBranch))
	cwdTip := strings.TrimSpace(archCLIGit(t, cwd, "rev-parse", cwdBranch))

	// (a) THE FIX: the flag decides WHICH repo is written.
	out, err := runArchiveProbe(t, bin, home, cwd,
		CliVerbTask, taskverbs.Archive, fixtureBranch, "--reason", archiveReasonInline,
		"--repo", target)
	if err != nil {
		t.Fatalf("--repo pointing at the target repo failed: %v\n%s", err, out)
	}
	if got := archCLIGitNoFatal(target, "rev-parse", "--verify", "--quiet", tools.ArchiveTagRefPrefix+fixtureBranch); strings.TrimSpace(got) == "" {
		t.Fatalf("no archive tag in the --repo target after exit 0; tags=%v\n%s",
			archCLITags(t, target), out)
	}
	if got := strings.TrimSpace(archCLIGit(t, target, "rev-parse", "archive/"+fixtureBranch)); got != targetTip {
		t.Fatalf("archive/%s = %q in the target, want its branch tip %q", fixtureBranch, got, targetTip)
	}
	// The cwd's repo was not the subject of the invocation: no tag, and — the
	// loudest half — its own branch ref is untouched (a cwd-resolved archive
	// would have carried the decision through and deleted it).
	if tags := archCLITags(t, cwd); len(tags) != 0 {
		t.Fatalf("the CWD repo was written despite --repo: %v", tags)
	}
	if got := strings.TrimSpace(archCLIGit(t, cwd, "rev-parse", "refs/heads/"+cwdBranch)); got != cwdTip {
		t.Fatalf("the CWD repo's branch was touched (%s → %s) by an archive aimed elsewhere", cwdTip, got)
	}

	// (c) THE CONTROL — NO --repo at all: the cwd's own repo is the target, so
	// the fix cannot be "use whatever path was named last".
	out, err = runArchiveProbe(t, bin, home, cwd,
		CliVerbTask, taskverbs.Archive, cwdBranch, "--reason", archiveReasonInline)
	if err != nil {
		t.Fatalf("the cwd-repo control archive failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(archCLIGit(t, cwd, "rev-parse", "archive/"+cwdBranch)); got != cwdTip {
		t.Fatalf("archive/%s = %q, want the cwd branch tip %q", cwdBranch, got, cwdTip)
	}

	// (b) A --repo THAT IS NOT A REPO IS REFUSED, never silently the cwd.
	// Measured pre-fix: identical output to (a), exit 1, target untouched — i.e.
	// the flag was ignored in silence. The assertion is on the OBSERVATION, not
	// the sentence: a non-repo path must produce a non-zero exit and must leave
	// every repo in play untouched.
	// The state BOTH repos must be in afterwards is captured first: the control
	// above legitimately wrote into cwd, so the assertion is "nothing changed
	// during this invocation", not "the repo is empty".
	targetTagsBefore := strings.Join(archCLITags(t, target), " ")
	cwdTagsBefore := strings.Join(archCLITags(t, cwd), " ")
	targetBranchBefore := archCLIGitNoFatal(target, "rev-parse", "refs/heads/"+fixtureBranch)
	out, err = runArchiveProbe(t, bin, home, cwd,
		CliVerbTask, taskverbs.Archive, fixtureBranch, "--reason", archiveReasonInline,
		"--repo", filepath.Join(t.TempDir(), "not-a-repo"))
	if err == nil {
		t.Fatalf("--repo naming a non-repo exited 0 — the flag was ignored:\n%s", out)
	}
	if !strings.Contains(out, "--show-toplevel") {
		t.Errorf("the refusal must carry git's own failure for the path it was given:\n%s", out)
	}
	if got := strings.Join(archCLITags(t, target), " "); got != targetTagsBefore {
		t.Fatalf("a REFUSED --repo wrote the target repo: %q → %q", targetTagsBefore, got)
	}
	if got := strings.Join(archCLITags(t, cwd), " "); got != cwdTagsBefore {
		t.Fatalf("a REFUSED --repo fell back to the cwd repo: %q → %q", cwdTagsBefore, got)
	}
	if got := archCLIGitNoFatal(target, "rev-parse", "refs/heads/"+fixtureBranch); got != targetBranchBefore {
		t.Fatalf("a REFUSED --repo moved the target's branch (%s → %s)", targetBranchBefore, got)
	}
}

// The two branches this regression drives, named once (a reader of a failure
// message and the fixture setup must not be able to disagree about them).
const (
	fixtureBranch = "fix/some-failed-attempt"
	cwdBranch     = "feat/elsewhere"
)

// archiveReasonInline is the reason code these probes pass: the package's own
// constant, never the literal (Rule 1b) — a renamed code must break the caller,
// not a test that spells it itself.
const archiveReasonInline = tools.ArchiveReasonHandMistake

// TestArchiveCLICheckExitsNonZeroOnAMalformedReasonRef is the EXIT-STATUS arm of
// the 2026-10-12 defect. Two things were wrong at once, and they compound: a
// commit-typed ref in refs/gopi/archive-reason/ aborted the whole namespace scan
// (so every archive face died), and the surface printed its complaint on stderr
// while the shell saw a zero status (the earlier evidence file recorded `rc=0`
// beside a fatal `cat-file` line). A check whose finding is only a paragraph is
// not a check — the STATUS is the signal a script reads.
//
// The fixture is the minimal one: an EMPTY backlog (no tag, no reason-less row)
// plus exactly one wrong-typed companion ref. A clean namespace in the same
// shape must still exit 0, which is what keeps the finding honest — otherwise
// this test would pass on a verb that exits 1 unconditionally.
func TestArchiveCLICheckExitsNonZeroOnAMalformedReasonRef(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo := archCLIRepo(t, "feat/malformed")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	home := t.TempDir()
	t.Setenv("GOPI_MEMORY_DB", dbPath)

	// CLEAN CONTROL: nothing to report, so --check must be green. Without this
	// arm the malformed case could not tell a finding from a constant exit 1.
	out, err := runArchiveProbe(t, bin, home, repo, dbPath, CliVerbTask, taskverbs.Archive, "--check")
	if err != nil {
		t.Fatalf("--check on a CLEAN namespace exited non-zero: %v\n%s", err, out)
	}

	// The malformed entry: a companion ref pointing at a COMMIT.
	sha := strings.TrimSpace(archCLIGit(t, repo, "rev-parse", "HEAD"))
	archCLIGit(t, repo, "update-ref", "refs/gopi/archive-reason/feat/malformed", sha)

	out, err = runArchiveProbe(t, bin, home, repo, dbPath, CliVerbTask, taskverbs.Archive, "--check")
	if err == nil {
		t.Fatalf("--check exited 0 with a malformed entry in the reason namespace:\n%s", out)
	}
	if code := exitCodeOf(t, err); code != 1 {
		t.Fatalf("--check exit code = %d, want 1 (the finding must reach the shell):\n%s", code, out)
	}
	wantRef := "refs/gopi/archive-reason/feat/malformed"
	if !strings.Contains(out, wantRef) {
		t.Fatalf("--check report does not name the malformed ref %s:\n%s", wantRef, out)
	}
	// The report says WHAT is wrong, not just which ref: the wrong type and the
	// type a record must be.
	if !strings.Contains(out, "ref points at a commit, not a blob") {
		t.Errorf("--check report does not name the wrong type / the expected one:\n%s", out)
	}
}
