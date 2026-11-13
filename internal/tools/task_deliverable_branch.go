// Deliverable.branch backfill at writeback (bugfix epic 2026-09-02:
// "adoption merge gate never fires"). The adoption gate (adoptionTaskCompleted)
// pairs a branch with a COMPLETED ledger task whose deliverable JSON carries
// "branch": "<name>". But the writeback path only ever updated status+result —
// the deliverable column kept whatever the submitter wrote ({"repo":...} with
// NO branch), so AdoptionSweep skipped every branch (skipped=11 forever).
//
// Fix (Option A — backfill at completion): when a task-run child completes,
// resolve the branch it worked on and merge it into the row's deliverable:
//
//	TaskRunWriteback(completed) → taskWritebackBranch() → deliverable merge
//
// The child session's session_start.workdir is the authoritative record of
// where the child STARTED (the child inherits the task-run wrapper's cwd —
// a linked worktree per PROJECT.md Rule 6a — and NewSessionLogger records that
// cwd as session_start.workdir). `git -C <workdir> branch --show-current`
// then names the branch — CORRECT for the worktree==launch-dir shape, but
// WRONG when the executor launches from the MAIN repo (master checked out)
// and creates its own linked worktree mid-session (PROJECT.md Rule 6a solo
// shape): the launch workdir stays the main repo, its branch is master
// (exempt), and the real delivery lives on the worktree branch. For that
// shape the writeback falls back to scanning the child session's bash
// tool_use commands for the LAST `git worktree add ... -b <branch>` — the
// creation moment that names the branch the child actually committed on
// (2026-09-09 bugfix: t-1788813641 delivered to feat/prompt-review yet its
// row stayed branch-less because only the master launch dir was consulted).
// Deliberately ENV-GATED: branch resolution only consults the child-session
// env seams (TaskChildSessionEnv set by the task run CLI /
// TaskExecutorSessionEnv set at gopi startup) — a bare os.Getwd() fallback
// would wrongly stamp branches into rows written back from arbitrary cwds
// (tests run inside git worktrees!). No child session = no backfill.
//
// Best-effort by contract: every failure (no session, non-repo workdir,
// detached/master branch, DB error) logs and returns — the terminal writeback
// already succeeded and must never be rolled back by a side-channel backfill.
package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// taskBranchBackfillTimeout caps the git invocation resolving the executed
// branch (a wedged repo must not stall the writeback close-out).
//
// NOTE: the session-scan fast path (taskDeliverableBranchFromSession)
// matches the wire bytes `"bash"` in a raw JSONL line — that is an
// intentional literal scanning serialized bytes, NOT Go identity; the
// parsed record's real tool-name check uses toolnames.ToolBash. Do not
// "constantize" the byte literal (it must stay the serialized form).
const taskBranchBackfillTimeout = 10 * time.Second

// taskWorkdirFromSession reads session_start.workdir from a session JSONL
// (mirror of taskModelFromSession — first line, lenient scan). Returns ""
// when the session file is missing/unreadable or carries no workdir.
func taskWorkdirFromSession(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(gd, config.SessionsDirName)
	path := taskFindSessionJSONL(dir, sessionID)
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)
	if !sc.Scan() {
		return ""
	}
	var ev struct {
		Workdir string `json:"workdir"`
	}
	if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
		return ""
	}
	return strings.TrimSpace(ev.Workdir)
}

// taskGroundToolNeedles are the SERIALIZED (JSON-quoted) wire names of the
// tools whose commands can create the run's ground — the cheap byte
// prefilters the session scans use before decoding a line. Derived from
// toolnames (Rule 13: the wire name has one source of truth; only its
// serialized spelling is built here, because matching raw JSONL bytes is
// deliberately not Go identity — see the note on taskBranchBackfillTimeout).
// The JOB tool belongs in the set for the same reason the bash tool does: a
// detached `job(action=start, command=… git worktree add … -b …)` creates the
// run's worktree/branch, and a scan that only looked at bash lines recorded
// nothing for that run.
var taskGroundToolNeedles = [][]byte{
	[]byte(`"` + toolnames.ToolBash + `"`),
	[]byte(`"` + toolnames.ToolJob + `"`),
}

// taskGroundCommandBlock reports whether one parsed tool_use content block is
// a command this package may read ground from — a bash call, or a job call's
// START action (output/kill carry no command) — and returns that command.
// ONE reader for both scans below (Rules 13/14): the session-level branch scan
// and any later reader agree on what "a command that can create ground" is.
func taskGroundCommandBlock(name, action, command string) (string, bool) {
	if strings.TrimSpace(command) == "" {
		return "", false
	}
	switch name {
	case toolnames.ToolBash:
		return command, true
	case toolnames.ToolJob:
		if action != toolnames.ToolJobActionStart {
			return "", false
		}
		return command, true
	}
	return "", false
}

// taskGroundFromSession reads a child session's JSONL ONCE and returns the two
// branch readings the terminal writeback can take from it:
//
//	addBranch     — the LAST `git worktree add ... -b <branch>` creation
//	                moment (see the contract below).
//	createdBranch — the LAST in-place branch creation (`git checkout -b <name>`
//	                / `git switch -c <name>`) that ran on repo's ground
//	                (taskBranchCreatedFromCommand). This is the reading a
//	                background JOB's command yields — `job(action=start,
//	                command="git worktree add … -b …")` or the `checkout -b`
//	                form — which the bash-only scans recorded nowhere
//	                (measured: 47 job-start ground-creating commands in 400
//	                recent sessions).
//
// repo "" disables the createdBranch reading (no ground reference ⇒ no
// "was it created HERE?" answer — never guess, Rule 15). One file read serves
// both readers: a second scan of the same session would cost a second pass
// over a multi-MB JSONL for no information.
//
// (addBranch contract, unchanged: the executor shape where the child launches
// from the MAIN repo (branch master, exempt) and creates its own linked
// worktree mid-session — the worktree-add command is the authoritative record
// of the branch the child then commits on (Rule 6a solo shape — t-1788813641 /
// 2ab6c869: `cd ~/work/GoPi && git worktree add ~/workspace/gopi-prompt-review
// -b feat/prompt-review ...`). Chronologically LAST add wins (a move mid-run
// re-stamps on the next add). Only the -b/-B form is scanned: a worktree added
// WITHOUT a new branch has no branch claim (its commits land on the shared
// branch, already captured by the launch-workdir path or master-exempt by
// design). The parsed command is a raw string; matching tolerates both
// `git worktree add <path> -b <branch>` and `git worktree add -b <branch>
// <path>` argument orders, and commands that embed the add in a `cd ... && git
// ...` chain (the observed executor shape). The caller exempt-checks and
// existence-verifies the returned name (taskWritebackBranch), so an odd name
// (nested ref, deleted branch) safely resolves to no backfill.)
func taskGroundFromSession(sessionID, repo string) (addBranch, createdBranch string) {
	if strings.TrimSpace(sessionID) == "" {
		return "", ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return "", ""
	}
	dir := filepath.Join(gd, config.SessionsDirName)
	path := taskFindSessionJSONL(dir, sessionID)
	if path == "" {
		return "", ""
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	addBranch = ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 20<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte("tool_use")) {
			continue
		}
		// Cheap byte pre-filter: only a line carrying a ground-bearing tool's
		// serialized name can yield a command (taskGroundToolNeedles — a
		// second spelling here would drift from the reader below, Rules
		// 13/14).
		ground := false
		for _, needle := range taskGroundToolNeedles {
			if bytes.Contains(line, needle) {
				ground = true
				break
			}
		}
		if !ground {
			continue
		}
		var ev struct {
			Message struct {
				Content []struct {
					Type  string `json:"type"`
					Name  string `json:"name"`
					Input struct {
						Action  string `json:"action"`
						Command string `json:"command"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		for _, b := range ev.Message.Content {
			if b.Type != "tool_use" {
				continue
			}
			command, ok := taskGroundCommandBlock(b.Name, b.Input.Action, b.Input.Command)
			if !ok {
				continue
			}
			if br := taskWorktreeAddBranchFromCommand(command); br != "" {
				addBranch = br // chronological LAST add wins
			}
			if repo != "" {
				if cb := taskBranchCreatedFromCommand(command, repo, ""); cb != "" {
					createdBranch = cb // chronological LAST creation wins
				}
			}
		}
	}
	return addBranch, createdBranch
}

// taskWorktreeAddBranchFromCommand parses ONE bash command string for a
// `git worktree add ... -b <branch>` (or -B) creation moment and returns
// the branch it names ("" when the command is not such a creation). Tolerates
// both argument orders (`add <path> -b <branch>` and `add -b <branch>
// <path>`) — the branch name is the token right after -b/-B regardless of
// where the path sits. `git -C <repo> worktree add` (the -C form) is also
// matched. When a command chains several worktree-adds, the LAST -b wins
// (consistent with taskGroundFromSession's last-add-is-master
// rule). Quoted branch names with spaces are out of scope (git branch names
// cannot contain spaces).
func taskWorktreeAddBranchFromCommand(command string) string {
	fields := strings.Fields(command)
	last := ""
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] != finishCleanupWorktreeVerb || fields[i+1] != "add" {
			continue
		}
		// After the add verb, find -b|-B and take the NEXT field as branch.
		for j := i + 2; j+1 < len(fields); j++ {
			if fields[j] == "-b" || fields[j] == "-B" {
				b := strings.TrimSpace(fields[j+1])
				// Strip any trailing shell glue (`;`, `&&`, `|`, `2>&1`)
				// that a naive Fields split would not separate from a glued
				// command like `add -b feat/x 2>&1`.
				b = strings.TrimRight(b, ";&|")
				if b != "" {
					last = b // LAST add in the command wins
				}
				break
			}
			// Stop scanning this add once we pass the branch/options region:
			// the -b must come before any && or | chain segment boundary that
			// starts a NEW command (the add's own args end there).
			if strings.ContainsAny(fields[j], "&|;") {
				break
			}
		}
	}
	return last
}

// taskWritebackBranch resolves the branch a just-completed task-run child
// executed on. Resolution is three-path:
//
//  1. Launch workdir (primary): the child session's recorded
//     session_start.workdir → `git -C <workdir> branch --show-current`.
//     Correct when the child ran where it started (the worktree==launch-dir
//     shape) — the pre-existing behavior, kept for regression.
//  2. Worktree-add fallback: when path 1 yields nothing or an EXEMPT branch
//     (master/main/archive — the shape where the child launched from the
//     MAIN repo and created its own linked worktree mid-session), scan the
//     child session for the LAST `git worktree add ... -b <branch>` and use
//     that branch, VERIFIED to exist in the launch repo. See
//     taskGroundFromSession.
//  3. In-place creation (the JOB half): the child created its branch WITHOUT
//     a worktree — `git checkout -b <name>` / `git switch -c <name>`, read
//     from bash AND job(action=start) commands, gated to creations that ran
//     on the launch repo's own ground. Same verification as path 2.
//
// Env-gated (see the file doc): returns "" when no child session is
// resolvable, the workdir is not inside a git work tree, HEAD is detached,
// both paths come up empty, or the resolved branch is an integration/exempt
// name (master/main/archive/* — nothing to adopt).
func taskWritebackBranch() string {
	sid := strings.TrimSpace(os.Getenv(TaskChildSessionEnv))
	if sid == "" {
		// Executor-session case: writebacks running inside the executing
		// session itself (the auto-register end hook path) resolve their own
		// session id — printLogPaths exported it at startup.
		sid = strings.TrimSpace(os.Getenv(TaskExecutorSessionEnv))
	}
	if sid == "" {
		return "" // no authoritative child session — never guess from cwd
	}
	workdir := taskWorkdirFromSession(sid)
	if workdir != "" {
		if branch := taskBranchFromWorkdir(workdir); branch != "" {
			return branch // path 1: launch dir names the branch (regression shape)
		}
	}
	// path 2: the launch dir resolved to nothing or an exempt branch (master
	// — the executor started in the MAIN repo). The child's real delivery may
	// live on a worktree branch it created mid-session. Scan for the LAST
	// `git worktree add ... -b <branch>` and verify the branch exists in the
	// launch repo before claiming it.
	if workdir == "" {
		return "" // no launch repo to verify against — never trust a bare name
	}
	repo := taskLaunchRepo(sid, workdir)
	addBranch, createdBranch := taskGroundFromSession(sid, repo)
	branch := addBranch
	if branch == "" {
		// Path 3 (the job half): no worktree-add names the branch, but the
		// session may have CREATED it in place — `git checkout -b` /
		// `git switch -c`, including inside a detached `job(action=start)`
		// command, whose output never came back through the bash tool. The
		// branch is the run's deliverable exactly as much as a worktree
		// branch is, and it is what the in-flight claim stamp already
		// records; without this the row stayed branch-less for the whole
		// terminal report (the merger then reads its commits as unowned).
		branch = createdBranch
	}
	if branch == "" || taskBranchExempt(branch) {
		return ""
	}
	if !taskBranchExists(workdir, branch) {
		return "" // branch not in the launch repo — no adoption target
	}
	return branch
}

// taskLaunchRepo names the repository the child launched in — the ground every
// command-creating reading is judged against. It is the worktree's ROOT (not
// the launch directory itself): a child that launched from a SUBDIRECTORY of
// the repo (`…/repo/internal/tools`) is still on that repo's ground, and
// comparing a command's directory against the subdirectory would read its own
// repo as foreign. Falls back to the launch directory when git cannot name a
// root (not a repo) — then nothing can be inside it either way, so the
// reading stays conservative.
func taskLaunchRepo(sid, workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskBranchBackfillTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", workdir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return workdir
	}
	if root := strings.TrimSpace(string(out)); root != "" {
		return root
	}
	return workdir
}

// taskBranchFromWorkdir runs `git -C workdir branch --show-current` and
// exempt-checks the result ("" when not a work tree, detached, or exempt).
func taskBranchFromWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskBranchBackfillTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", workdir, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		return "" // not a git work tree (or wedged) — nothing to backfill
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || taskBranchExempt(branch) {
		return ""
	}
	return branch
}

// taskBranchExists verifies a branch exists in the repo at workdir, using
// `git -C workdir rev-parse --verify refs/heads/<branch>` (exact full ref —
// a worktree branch in the SAME repo shares the ref namespace, so the
// launch repo itself can confirm). False on any git error (branch deleted
// between session and writeback, wedged repo, not a repo).
func taskBranchExists(workdir, branch string) bool {
	workdir = strings.TrimSpace(workdir)
	branch = strings.TrimSpace(branch)
	if workdir == "" || branch == "" || strings.Contains(branch, "..") ||
		strings.HasPrefix(branch, "-") {
		return false // refuse ref-traversal / flag injection before shelling out
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskBranchBackfillTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", workdir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// taskBranchExempt reports whether a branch must never be backfilled as a
// task's deliverable (integration branches carry no adoptable work; the
// archive ledger is the loser graveyard — same exemptions finishCleanup
// and MergedCleanupSweep apply, one spelling each for one rule).
func taskBranchExempt(branch string) bool {
	switch strings.ToLower(branch) {
	case "master", "main":
		return true
	}
	return strings.HasPrefix(branch, archiveBranchPrefix)
}

// deliverableRepoOnly reports whether a deliverable blob is a submit-time
// repo-only PLACEHOLDER: valid JSON carrying "repo" and no "branch"/"worktree"
// yet — exactly the shape TaskSubmit stamps at enqueue (M4.8; the executor
// backfills branch at completion, taskBackfillDeliverableBranch). The
// placeholder is WEAKER than an executor's completion blob (repo+branch):
// the sync/forward gates (deliverableForwardable, harvestUpdate) upgrade it
// freely, while a full blob stays first-truth-wins. The reverse (a
// repo-only remote blob overwriting a local full one) is never allowed.
func deliverableRepoOnly(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return false
	}
	var d TaskDeliverableSpec
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return false
	}
	return strings.TrimSpace(d.Repo) != "" &&
		strings.TrimSpace(d.Branch) == "" &&
		strings.TrimSpace(d.Worktree) == ""
}

// taskBackfillDeliverableBranch stamps branch into a task row's deliverable
// JSON — {"repo":...} → {"repo":...,"branch":...} — ONLY when the row is
// completed, its deliverable still carries a repo and NO branch yet, and
// the branch is non-empty (never overwrites a branch another writer stamped,
// never invents one). Guarded single UPDATE keyed on the read-back raw blob
// so a concurrent writer cannot be clobbered (CAS-by-value). Best-effort:
// every failure logs via taskNotifyLog and returns nil — the writeback's
// terminal outcome is already committed and stays authoritative.
func taskBackfillDeliverableBranch(taskID, branch string) error {
	taskID = taskIDNormalize(taskID)
	branch = strings.TrimSpace(branch)
	if taskID == "" || branch == "" {
		return nil
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return err
	}
	if t == nil || t.Status != taskStatusCompleted {
		return nil // row gone or not completed — nothing to backfill
	}
	raw := strings.TrimSpace(t.Deliverable)
	if raw == "" {
		return nil // no deliverable blob (auto mirror rows) — nothing to merge
	}
	parsed := taskDeliverableParse(raw)
	repo, branchExisting := parsed.Repo, parsed.Branch
	if repo == "" || branchExisting != "" {
		return nil // repo-less blob (nothing to pair) or already branch-stamped
	}
	merged := taskDeliverableRebuild(taskDeliverable{Repo: repo, Branch: branch, Grounds: parsed.Grounds, Attachments: parsed.Attachments})
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE task_id = ? AND deliverable = ?`,
		merged, float64(time.Now().Unix()), taskID, raw)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		taskNotifyLog("task %s: deliverable.branch backfilled to %q at writeback", taskID, branch)
	}
	return nil
}
