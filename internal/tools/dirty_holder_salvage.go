package tools

// Dirty dead-holder salvage — the dispatch half of the reclaim's guard ②.
//
// THE STATE (measured on the live host 2026-09-15, three branches):
//
//	the holder is provably DEAD, the worktree still occupies the branch, and
//	the directory is DIRTY — so gate [c] (safe-merge.sh) and the merger's
//	protected arm both refuse the branch, and the reclaim arm's guard ②
//	reclaimDisposition refuses the directory.
//
// Everything up to here is deliberate: uncommitted work IS work, and no
// sweep may destroy it. What did not exist was the OTHER half — the state
// was rendered ("worktree is DIRTY (uncommitted work) — left for a human",
// reclaimSkipDirtyReason) and then FORGOTTEN: no task was filed, no
// notification fired, and the only copy of the work sat in a dead holder's
// directory. Two of those three live branches measure 0 commits ahead of
// master, i.e. the row's ENTIRE deliverable is that uncommitted tree.
//
// This arm closes the loop without weakening a single guard:
//
//	capture   — the tracked diff (`git diff HEAD --binary`) and the untracked
//	            files (stdlib tar.gz, relative paths preserved) are written
//	            into the scratch tree and attached to the salvage task, so the
//	            work has a DURABLE copy (TaskCopyAttachments archives it into
//	            the task's session dir) the moment the state is detected;
//	dispatch  — ONE salvage row per (repo, branch), deduped through the same
//	            live-summary probe the branch-rework lane uses;
//	never     — the tree is NOT committed, NOT cleaned, NOT removed. The
//	            operator keeps the worktree exactly as it was (this is the
//	            "left for a human" promise, kept literally).
//
// What the salvage row does NOT decide is whether the work is WANTED — that
// is the row's own job (it can close as `superseded` with a one-line reason,
// the ordinary way a row is closed). Detection is automatic, judgement stays
// human/agent, and a directory holding the only copy of a deliverable stops
// being invisible to everything but an operator reading a status line.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/boolvocab"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// DirtyHolderSalvageEnv is the kill switch for the salvage DISPATCH
	// (same convention as its siblings: "0"/"off"/"no"/"false"/"disabled"
	// turns it off). Off = no rows are filed; the reclaim arm's refusal and
	// its operator line are untouched (they are reporting, not dispatch).
	DirtyHolderSalvageEnv = "GOPI_DIRTY_HOLDER_SALVAGE"

	// dirtyHolderSalvageSummaryPrefix is the salvage lane's identity: ONE row
	// per branch, however often the merger ticks. It is deliberately NOT the
	// branch-rework prefix (resolveRowReworkSummaryPrefix): "the branch cannot
	// be merged" and "the branch's uncommitted work needs a carrier" are two
	// different interventions, and sharing a prefix would let either one
	// silence the other through the dedup probe.
	dirtyHolderSalvageSummaryPrefix = "salvage uncommitted work: "

	// dirtyHolderTrackedPatchName / dirtyHolderUntrackedArchiveName are the
	// capture filenames inside the salvage directory — the two names the
	// briefing and the report both quote (one spelling, Rule 14).
	dirtyHolderTrackedPatchName     = "tracked.patch"
	dirtyHolderUntrackedArchiveName = "untracked.tar.gz"

	// dirtyHolderSalvageTags classifies the row: it is a real, claimable work
	// item (a reworker must ACT on it), unlike the merger-owned resolve rows
	// whose exclusion is the auto-spawn tag.
	dirtyHolderSalvageTags = "salvage,worktree"

	// dirtyHolderCaptureDirName is the scratch class the captures land in
	// (Rule 22: the scratch tree, never a fixed /tmp path).
	dirtyHolderCaptureDirName = "dirty-holder-salvage"

	// dirtyHolderUntrackedMax bounds how many untracked entries one capture
	// archives — a runaway tree (a build output directory) must cost a
	// bounded amount of work, and the count is REPORTED, never silently cut.
	dirtyHolderUntrackedMax = 500
)

// dirtyHolderSalvageEnabled reads the kill switch (one reader, Rule 2); the OFF
// spellings come from the ONE vocabulary definition (internal/boolvocab, a
// leaf — PROJECT.md Rule 14), never from a list re-spelled here (Rule 13).
func dirtyHolderSalvageEnabled() bool {
	return !boolvocab.IsFalse(os.Getenv(DirtyHolderSalvageEnv))
}

// SalvageDirtyDeadHolderWorktrees files the salvage rows for the dead-holder
// branches whose worktree the reclaim arm refused because it is DIRTY (held).
// It returns one report line per branch it ACTED on (row filed) or attempted
// (capture failed), so the merger's summary can carry an honest count; a
// branch already carrying a live salvage row produces no line (dedup, not a
// failure).
//
// repo scopes the pass to one repository (the merger sweeps one repo per
// tick); held is the arm's own verdict map (RepoSweepResult.Held). Nothing
// here runs for a branch the reclaim arm would free: a clean directory has no
// uncommitted work to salvage.
func SalvageDirtyDeadHolderWorktrees(repo string, held map[string]string) []string {
	if len(held) == 0 {
		return nil
	}
	if !dirtyHolderSalvageEnabled() {
		// The kill switch is REPORTED, never silent (Rule 15): an operator who
		// set it off must still see that the state exists.
		return []string{dirtyHolderSalvageDisabledMarker + ": " + fmt.Sprint(len(held)) +
			" dirty dead-holder worktree(s) — dispatch disabled by " + DirtyHolderSalvageEnv}
	}
	rows, err := reclaimDeadHolderRows()
	if err != nil {
		// The ledger is the ONLY source of a row's task_id; without it the
		// salvage cannot name the row it carries, so it refuses loudly rather
		// than filing an anonymous row (Rule 15).
		return []string{dirtyHolderSalvageMarker + ": ledger scan failed: " + err.Error()}
	}
	byBranch := map[string]reclaimDeadHolderRow{}
	for _, r := range rows {
		if r.Repo == repo {
			byBranch[r.Branch] = r
		}
	}
	branches := make([]string, 0, len(held))
	for b := range held {
		branches = append(branches, b)
	}
	sort.Strings(branches)

	var out []string
	for _, branch := range branches {
		// The arm's OWN dirty refusals are the only ones in scope, and they are
		// two states of ONE guard. This merge resolved a genuine disagreement
		// about the second one:
		//
		//	master excluded the untracked-only state from salvage and emitted an
		//	  explanatory line instead ("nothing to capture — git tracks no
		//	  change"). That reasoning is FALSE for the case the arm exists for:
		//	  an untracked file's only copy IS that directory (nothing in git
		//	  holds it), which is exactly why the branch's predicate
		//	  reclaimDirtyHeld covers both shapes and why the keystone red proof
		//	  — present, UNCHANGED, on BOTH sides of this merge —
		//	  TestZZRedProof_DirtyDeadHolderWorkIsOrphaned is RED on master's own
		//	  tree for it (measured: `git archive 30450fc6` + run = FAIL
		//	  "ORPHANED WORK"). The merge therefore keeps the FILING arm and
		//	  keeps master's intent the other way: the hygiene state is still
		//	  NAMED (Rule 15) — the refusal wording says "untracked-only residue"
		//	  and the salvage row carries the untracked half as a tarball.
		//
		// Every other "held" reason (a live process inside, a re-attached
		// branch, a foreign path) is a different fact and must not be
		// salvaged behind a dirty-worktrees label.
		if !reclaimDirtyHeld(held[branch]) {
			continue
		}
		row, ok := byBranch[branch]
		if !ok {
			// The branch is held but no ledger row names it: git alone cannot
			// say whose work it is, so there is nobody to hand it to. Named,
			// never guessed.
			out = append(out, dirtyHolderSalvageMarker+": "+branch+" — no ledger row names this worktree; nothing to hand the work to")
			continue
		}
		summary := dirtyHolderSalvageSummaryPrefix + branch
		live, err := reworkSummaryLive(summary)
		if err != nil {
			out = append(out, dirtyHolderSalvageMarker+": "+branch+" — dedup probe failed: "+err.Error())
			continue
		}
		if live {
			continue // one row per branch, already filed
		}
		line, err := dirtyHolderSalvageOne(repo, row, summary)
		if err != nil {
			out = append(out, dirtyHolderSalvageMarker+": "+branch+" — "+err.Error())
			continue
		}
		out = append(out, line)
	}
	return out
}

// salvageLaneName is the dirty-holder salvage lane's name in the shared stamp's
// notify line (Rule 1b).
const salvageLaneName = "salvage"

// dirtyHolderSalvageMarker is the one phrase every salvage report line
// carries, so an operator greps one string in the merger log.
const dirtyHolderSalvageMarker = "dirty dead-holder salvage"

// dirtyHolderSalvageDisabledMarker is the same for the kill-switch line.
const dirtyHolderSalvageDisabledMarker = "dirty dead-holder salvage disabled"

// dirtyHolderSalvageOne captures row's uncommitted work and files the salvage
// task for it. The directory is read-only for this call (`git status`,
// `git diff`, `git ls-files` all read) — nothing is written inside it.
func dirtyHolderSalvageOne(repo string, row reclaimDeadHolderRow, summary string) (string, error) {
	ctx := context.Background()
	dir, err := scratch.MkdirTemp(dirtyHolderCaptureDirName, "salvage-*")
	if err != nil {
		return "", fmt.Errorf("capture dir: %w", err)
	}
	atts, err := dirtyHolderCapture(ctx, row.Worktree, dir)
	if err != nil {
		// A capture that did not happen must NOT become a row claiming it did
		// (Rule 15): no attachments, no task.
		return "", fmt.Errorf("capture: %w", err)
	}
	payload := dirtyHolderSalvagePayload(repo, row, atts, summary)
	_, reworkID, err := TaskSubmitReturningID(TaskSubmitOptions{
		Summary:     summary,
		Payload:     payload,
		Tags:        dirtyHolderSalvageTags,
		Source:      taskMergeSourceAuto,
		Repo:        repo,
		Attachments: atts.paths,
	})
	if err != nil {
		return "", fmt.Errorf("submit: %w", err)
	}
	// Dedup closure, the same one the branch-rework lane needs: TaskSubmit
	// stamps {"repo":…} only, while the dedup probes key on (repo, branch) —
	// without the stamp every later tick would file another copy of this row.
	if err := stampDeliverableBranchForSummary(salvageLaneName, summary, repo, row.Branch); err != nil {
		taskNotifyLog("merger: salvage deliverable.branch stamp skipped: %v", err)
	}
	// Rule 33's supersession edge (see rework_reparent_children.go): this lane
	// also supersedes the dead holder's work, so the rows depending on that
	// row are re-pointed at the salvage row — the branch-keyed bridge resolves
	// the original row through the existing branch→row lookup.
	reworkReparentFromBranch(repo, row.Branch, reworkID, salvageLaneName)
	taskNotifyLog("%s: %s (%s) — %s captured and handed to a task; the worktree is untouched",
		dirtyHolderSalvageMarker, row.Branch, row.TaskID, atts.describe())
	return dirtyHolderSalvageMarker + ": " + row.Branch + " (" + row.TaskID + ") — " +
		atts.describe() + " handed to a task; worktree untouched", nil
}

// dirtyHolderCapture is the ONE description of the captured material: the
// attachment paths (in the order the briefing quotes them) plus the
// human-readable listing with each file's sha256 — the tamper-evident half a
// reworker verifies before applying anything.
type dirtyHolderCaptureResult struct {
	paths []string
	names []string
	sizes []int64
	sums  []string
}

// describe renders the capture for the report line and the notification.
func (c dirtyHolderCaptureResult) describe() string {
	if len(c.names) == 0 {
		return "no uncommitted material"
	}
	parts := make([]string, 0, len(c.names))
	for i, n := range c.names {
		parts = append(parts, fmt.Sprintf("%s (%d bytes, sha256 %s)", n, c.sizes[i], c.sums[i]))
	}
	return strings.Join(parts, "; ")
}

// dirtyHolderCapture writes the tracked diff and the untracked archive for the
// worktree into dir, returning their paths and a listing. Both files are
// produced by READ-ONLY git commands plus a stdlib archive walk (zero external
// dependency, Rule 10.7).
func dirtyHolderCapture(ctx context.Context, worktree, dir string) (dirtyHolderCaptureResult, error) {
	var res dirtyHolderCaptureResult
	// ── tracked changes: a binary-safe patch against HEAD. `--binary` is what
	// makes the capture apply again for files git would otherwise treat as
	// "Binary files differ" (an image, a golden file).
	// The patch is APPLIED again later, so its body is parsed content: stdout
	// only (a stderr line would be spliced into the salvage patch).
	patch, err := finishCleanupGitParsedStdout(ctx, worktree, finishCleanupGitTimeout, "diff", "HEAD", "--binary")
	if err != nil {
		return res, fmt.Errorf("git diff: %w", err)
	}
	if strings.TrimSpace(patch) != "" {
		p := filepath.Join(dir, dirtyHolderTrackedPatchName)
		if err := os.WriteFile(p, []byte(patch+"\n"), 0o600); err != nil {
			return res, fmt.Errorf("write %s: %w", dirtyHolderTrackedPatchName, err)
		}
		if err := captureAppend(&res, p, dirtyHolderTrackedPatchName); err != nil {
			return res, err
		}
	}
	// ── untracked files (never in any diff — the live gopi-brew-fix case had
	// its whole NEW test file here). `--exclude-standard` honors .gitignore so
	// build residue is not archived.
	// One PATH per line, each handed to the archiver — parsed, so stdout only.
	listRaw, err := finishCleanupGitParsedStdout(ctx, worktree, finishCleanupGitTimeout,
		"ls-files", "--others", "--exclude-standard")
	if err != nil {
		return res, fmt.Errorf("git ls-files: %w", err)
	}
	untracked := dirtyHolderUntrackedList(listRaw)
	if len(untracked) > 0 {
		arc := filepath.Join(dir, dirtyHolderUntrackedArchiveName)
		n, err := dirtyHolderUntrackedArchive(worktree, untracked, arc)
		if err != nil {
			return res, err
		}
		if err := captureAppend(&res, arc, dirtyHolderUntrackedArchiveName); err != nil {
			return res, err
		}
		res.names[len(res.names)-1] = fmt.Sprintf("%s, %d file(s)", dirtyHolderUntrackedArchiveName, n)
	}
	return res, nil
}

// dirtyHolderUntrackedList parses `git ls-files --others --exclude-standard`
// output into a sorted, deduplicated, bounded list of relative paths.
func dirtyHolderUntrackedList(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		p := strings.TrimSpace(line)
		if p == "" || seen[p] || strings.Contains(p, "..") {
			continue // "" / duplicate / a path that could escape the archive
		}
		seen[p] = true
		out = append(out, p)
		if len(out) >= dirtyHolderUntrackedMax {
			break
		}
	}
	sort.Strings(out)
	return out
}

// dirtyHolderUntrackedArchive writes rels (relative to worktree) into a
// gzip+tar at out, preserving the paths. Returns the number of entries.
func dirtyHolderUntrackedArchive(worktree string, rels []string, out string) (int, error) {
	f, err := os.Create(out)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", dirtyHolderUntrackedArchiveName, err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	n := 0
	for _, rel := range rels {
		abs := filepath.Join(worktree, rel)
		info, err := os.Lstat(abs)
		if err != nil {
			// A file that vanished between the listing and the walk is not a
			// capture failure (the tree is a dead holder's, nobody should be
			// writing there) — it is recorded by being absent from the archive.
			continue
		}
		if info.IsDir() {
			continue
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return n, fmt.Errorf("tar header %s: %w", rel, err)
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return n, fmt.Errorf("tar write %s: %w", rel, err)
		}
		src, err := os.Open(abs)
		if err != nil {
			return n, fmt.Errorf("open %s: %w", rel, err)
		}
		if _, err := io.Copy(tw, src); err != nil {
			src.Close() //stylegate:ignore read-only handle closed on the error path; the copy error is what is returned
			return n, fmt.Errorf("archive %s: %w", rel, err)
		}
		src.Close() //stylegate:ignore read-only handle closed at end of iteration; the entry is complete
		n++
	}
	if err := tw.Close(); err != nil {
		return n, fmt.Errorf("tar close: %w", err)
	}
	if err := gz.Close(); err != nil {
		return n, fmt.Errorf("gzip close: %w", err)
	}
	return n, nil
}

// captureAppend records one captured file (path, size, sha256) in res.
func captureAppend(res *dirtyHolderCaptureResult, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open capture %s: %w", name, err)
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("hash capture %s: %w", name, err)
	}
	res.paths = append(res.paths, path)
	res.names = append(res.names, name)
	res.sizes = append(res.sizes, n)
	res.sums = append(res.sums, hex.EncodeToString(h.Sum(nil))[:16])
	return nil
}

// dirtyHolderSalvagePayload is the salvage briefing: everything a fresh
// session needs to carry the uncommitted work onto the branch WITHOUT the dead
// holder's directory (which may be gone by the time the row is claimed — the
// attachments are the durable copy, the directory is a bonus).
func dirtyHolderSalvagePayload(repo string, row reclaimDeadHolderRow, atts dirtyHolderCaptureResult, summary string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the WORKTREE-SALVAGE agent for repo %q.\n\n", repo)
	fmt.Fprintf(&b, "STATE (measured, not reported by a human): branch %q is held by a linked worktree at %q whose "+
		"holder is provably DEAD (task row %s, claim stamp %s). The directory carries UNCOMMITTED WORK, so every "+
		"automatic path refuses it — the merger's protected arm (branch cannot be merged), scripts/safe-merge.sh "+
		"gate [c] (branch is checked out), and the dead-holder reclaim's guard ② (a dirty worktree is never "+
		"removed). That refusal is CORRECT: uncommitted work is work. It left the work with no carrier, which is "+
		"what this row is.\n\n", row.Branch, row.Worktree, row.TaskID, row.ClaimedBy)
	fmt.Fprintf(&b, "CAPTURED MATERIAL (attachments — a durable copy taken the moment the state was detected):\n")
	for i, n := range atts.names {
		fmt.Fprintf(&b, "  %s  (%d bytes, sha256 %s)\n", n, atts.sizes[i], atts.sums[i])
	}
	if len(atts.names) == 0 {
		fmt.Fprintf(&b, "  (none — the directory measured dirty but produced no diff; inspect it directly if it still exists)\n")
	}
	fmt.Fprintf(&b, "\nTASK:\n")
	fmt.Fprintf(&b, "1. Read the captured material. `%s` is `git diff HEAD --binary` from %q; `%s` holds the "+
		"UNTRACKED files (a new file never appears in a diff — check this one carefully, it has carried whole "+
		"deliverables before).\n", dirtyHolderTrackedPatchName, row.Worktree, dirtyHolderUntrackedArchiveName)
	fmt.Fprintf(&b, "2. Decide the work's fate FIRST, and say which you chose in your report:\n")
	fmt.Fprintf(&b, "   (a) WANTED — carry it onto the branch: check the branch out in your own worktree, "+
		"`git apply` the patch, extract the untracked files, verify, and COMMIT on branch %q. The merger then "+
		"lands it normally.\n", row.Branch)
	fmt.Fprintf(&b, "   (b) SUPERSEDED — the work is already in master, or the task it came from was closed by "+
		"other means: close THIS row as superseded with the one-line reason and commit nothing. Do not "+
		"re-implement for its own sake.\n")
	fmt.Fprintf(&b, "3. Verify with the project's own commands (`go build ./...` and the tests of the packages you "+
		"touched) from your branch checkout before committing.\n")
	fmt.Fprintf(&b, "4. NEVER delete branch %q and NEVER `git worktree remove` the dead holder's directory: it is the "+
		"original, and this task is not authorized to destroy anything. If your patch does not apply cleanly, "+
		"re-derive the change from the captured content by hand and report the drift.\n", row.Branch)
	fmt.Fprintf(&b, "\nDo not force-push over history another node may share.")
	return b.String()
}

// stampDeliverableBranchForSummary merges branch into the deliverable JSON of
// every row whose SUMMARY is summary ({"repo":…} → {"repo":…,"branch":…}) —
// the ONE stamp both lanes share (Rule 14): the branch-rework lane keys on
// resolveRowReworkSummaryPrefix+branch, the dirty-holder salvage lane on its
// own prefix, and both need the branch recorded because the dedup probes key
// on (repo, branch) while TaskSubmit stamps {"repo":…} alone. TaskSubmit
// returns a rendered confirmation rather than an id, so the row is identified
// by its deterministic summary. Never clobbers an existing branch stamp;
// best-effort at both call sites (the row is queued and claimable either way).
func stampDeliverableBranchForSummary(lane, summary, repo, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" || strings.TrimSpace(summary) == "" {
		return nil
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT deliverable FROM tasks WHERE summary = ?`, summary)
	if err != nil {
		return err
	}
	var raws []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return err
		}
		raws = append(raws, raw)
	}
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if err := rows.Err(); err != nil {
		return err
	}
	for _, raw := range raws {
		raw = strings.TrimSpace(raw)
		var d struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
		}
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			continue // not JSON — leave it
		}
		if strings.TrimSpace(d.Branch) != "" {
			continue // already branch-stamped — never overwrite
		}
		if strings.TrimSpace(d.Repo) == "" {
			continue // repo-less blob — nothing to pair
		}
		merged := resolveRowDeliverable(d.Repo, branch)
		if _, err := db.ExecContext(ctx,
			`UPDATE tasks SET deliverable = ?, updated_at = ? WHERE summary = ? AND deliverable = ?`,
			merged, float64(time.Now().Unix()), summary, raw); err != nil {
			return err
		}
		taskNotifyLog("merger: %s row %q deliverable.branch stamped to %q at submit", lane, summary, branch)
		return nil
	}
	return nil
}
