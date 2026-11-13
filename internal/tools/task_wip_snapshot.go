package tools

// WIP SNAPSHOT — the run's own uncommitted work, preserved somewhere that
// outlives its ground.
//
// THE MEASURED DEFECT (2026-10-26, row 01a17a8b's epic). A run that is
// interrupted by a signal, an OOM kill or a power cut leaves its uncommitted
// work in exactly one place: the run's own worktree. The moment the row is
// written `failed` that directory becomes RECLAIMABLE — the worktree sweep is
// entitled to remove it — and the only path that saved the 1097 uncommitted
// lines of the measured incident was a human who happened to be awake at that
// second and copied them out by hand (`wip-snapshot/` plus `tracked-0703.patch`
// in that session's own directory). Nothing in the ledger could do it, and
// nothing told the successor where the salvaged work was.
//
// THE SHAPE THIS FILE ADDS — three legs, one primitive:
//
//	① SnapshotUncommittedWork(taskID, trigger) is the PRIMITIVE: measure the
//	   row's OWN worktree through the ONE close-out measurement that already
//	   exists (ReclaimRunResidueCheck), and if it holds dirt, deposit it under
//	   the row's OWN session directory (<gopi-dir>/sessions/<id>/wip/).
//	② the `failed` WRITEBACK calls it FIRST (taskUpdateStampedCore, before any
//	   UPDATE) so the snapshot exists even when the writeback itself is the last
//	   thing that happens to the process.
//	③ (DELETED 2026-10-30, task 01a18ef7.) A PERIODIC leg used to ride the
//	   task-lifecycle pass (taskLifecyclePassOnce → SnapshotLiveRunsPeriodic),
//	   capped per pass, "so a run interrupted with no writeback at all still has
//	   a recent copy on disk". MEASURED: that pass had no production caller —
//	   the worker round's call went in 187d05c, the merger tick in 2026-09-19,
//	   `gopi task reconcile` in 01a18799 — so the periodic leg ran only from
//	   its own tests, and the "recent copy" it promised was never taken in
//	   production. It is gone with its per-row throttle and per-pass cap. The
//	   legs below are the ones with a carrier: ② runs on the `failed`
//	   WRITEBACK (taskUpdateStampedCore, internal/tools/tasks.go), ④ on the
//	   successor mint.
//	④ the successor mint (`gopi task clone`) names the artifact in the new row's
//	   payload, so the relayer reads the path from the briefing instead of being
//	   handed a file by a human (task_successor_wip_pointer.go).
//
// THREE BOUNDARIES, each of which is a rule and not a preference:
//
//	Rule 30 — the snapshot leg only READS the worktree. Nothing here commits,
//	  stashes, cleans, restores or deletes a byte of the run's tree; every git
//	  verb it runs is one of the READ vocabulary (status/diff/ls-files/rev-parse)
//	  and every file it writes goes under the session dir.
//	Rule 37 — the deposit lives in the run's OWN session directory, which is
//	  never a git-tracked path. `git status --porcelain` in the ground is
//	  therefore empty of this file's output by construction, not by cleanup.
//	Zero write when there is nothing to save: a clean worktree returns BEFORE
//	  any directory is created, so a tidy run leaves no `wip/` at all.
//
// WHAT IT IS NOT: a backup of committed work (git already holds that), a
// replacement for the run committing its own work, and a reason to relax
// Rule 30's dirty close-out gate. A snapshot makes an interruption survivable;
// it never makes an uncommitted delivery acceptable.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The snapshot's own vocabulary (Rule 1/13: no bare literals at a call site).
const (
	// taskWIPDirName is the directory under the row's session dir that holds the
	// deposit. One spelling for the writer and for the pointer the successor leg
	// renders into a successor's briefing (Rule 14).
	taskWIPDirName = "wip"

	// taskWIPStatusFile is the raw `git status --porcelain` body of the measured
	// instant — the inventory a reader compares against the copies beside it.
	taskWIPStatusFile = "status.porcelain"

	// taskWIPTrackedPatchFile is every TRACKED change at the measured instant,
	// binary included, as one applyable patch (`git diff HEAD --binary`).
	taskWIPTrackedPatchFile = "tracked.patch"

	// taskWIPUntrackedDirName holds one COPY per untracked file, at its
	// worktree-relative path — the half a patch cannot carry.
	taskWIPUntrackedDirName = "untracked"

	// taskWIPHandoffFile is the machine-generated hand-over note: what was
	// measured, when, and the exact commands that restore it. It exists so the
	// successor (or a human) needs no context that is not on disk.
	taskWIPHandoffFile = "HANDOFF-FIRST-READ-ME.md"

	// taskWIPUntrackedListFile is the newline-separated list of untracked paths
	// the snapshot COPIED — the index into taskWIPUntrackedDirName. A file too
	// large to copy is named in the hand-over note instead of silently dropped.
	taskWIPUntrackedListFile = "untracked.list"

	// The trigger, spelled once: it ends up in the hand-over note and in the
	// ledger event's detail. There is deliberately no second value — the only
	// leg left is the `failed` WRITEBACK (taskUpdateStampedCore), and a value
	// nothing writes is a vocabulary a later reader would have to disprove
	// (Rule 13). The periodic leg's own trigger went with the leg, 2026-10-30.
	taskWIPTriggerFailed = "failed-writeback"
)

const (
	// taskWIPMaxFileBytes bounds ONE untracked file's copy. A build artefact or
	// a capture can be hundreds of megabytes; the point of the deposit is the
	// run's SOURCE work, and a file at or above this bound is NAMED in the
	// hand-over note (its worktree-relative path and size) rather than copied —
	// never silently omitted.
	taskWIPMaxFileBytes = 8 << 20 // 8 MiB

	// taskWIPMaxTotalBytes bounds one whole snapshot's copied bytes, so a
	// pathological tree cannot fill the profile. Once the budget is spent the
	// remaining paths are NAMED, not dropped silently.
	taskWIPMaxTotalBytes = 64 << 20 // 64 MiB

	// taskWIPUntrackedListSep separates the recorded trigger words in an event
	// detail and the copied paths in untracked.list. One definition (Rule 14).
	taskWIPUntrackedListSep = "\n"

	// taskWIPMaxUntrackedEntries bounds the number of untracked paths one
	// snapshot will consider; the remainder is COUNTED in the hand-over note.
	taskWIPMaxUntrackedEntries = 5000
)

// Event values filed on the row's trail. A snapshot that happened and one that
// did not are different facts, and an operator grepping "which rows were
// preserved before this failure" must not have to parse a sentence (Rule 14).
const (
	// taskWIPSnapshotEvent — the deposit exists; the detail carries its path.
	taskWIPSnapshotEvent = "wip-snapshot"

	// (taskWIPSnapshotCleanEvent was DELETED 2026-10-30 with the periodic leg it
	// belonged to — task 01a18ef7. It was filed "on the PERIODIC leg only" so the
	// trail could distinguish "the tick looked" from "the tick never looked", and
	// once the leg went, `grep -rn taskWIPSnapshotCleanEvent --include=*.go .`
	// found the declaration and its own comment and nothing else: a vocabulary
	// value nothing writes (Rule 13). The `failed` writeback always has something
	// to say, so its two events are the whole set.)

	// taskWIPSnapshotFailedEvent — the deposit could NOT be written, or the
	// worktree could not be measured. Always paired with the reason, and it
	// never blocks the transition that triggered it.
	taskWIPSnapshotFailedEvent = "wip-snapshot-failed"
)

// TaskWIPSnapshot is the outcome of one measurement.
type TaskWIPSnapshot struct {
	// Path is the deposit directory, "" when nothing was written.
	Path string
	// Worktree is the directory that was measured ("" when the row's own ground
	// could not be attributed — nothing outside the row's own worktree is read).
	Worktree string
	// Entries is the number of porcelain entries at the measured instant.
	Entries int
	// TrackedFiles / UntrackedFiles count what the deposit captures.
	TrackedFiles   int
	UntrackedFiles int
	// Skipped names the entries the deposit could not carry (too large, over
	// budget, deleted-before-copied). Never empty without a reason.
	Skipped []string
	// untrackedPaths is the list written to taskWIPUntrackedListFile: exactly the
	// entries whose bytes are in taskWIPUntrackedDirName.
	untrackedPaths []string
	// Clean is true when the worktree held nothing to save.
	Clean bool
	// Trigger is the leg that asked (taskWIPTrigger*).
	Trigger string
	// Note is the one-line operator sentence for the ledger and stderr.
	Note string
}

// SnapshotUncommittedWork deposits the row's uncommitted work into its own
// session directory and returns what it did. See this file's header.
//
// A CLEAN worktree returns BEFORE any directory is created — that ordering is
// the whole of "zero write when there is nothing to save", and it is why the
// mkdir call sits after the measurement rather than before it.
//
// An error is returned only when the MEASUREMENT itself is unusable (an
// unreadable ledger or an unreadable ground). Every partial failure inside the
// deposit is REPORTED on the row's trail and in the returned Note, and never
// blocks the caller's transition (Rule 15: recorded, not swallowed, not fatal).
func SnapshotUncommittedWork(taskID, trigger string) (TaskWIPSnapshot, error) {
	taskID = taskIDNormalize(taskID)
	out := TaskWIPSnapshot{Trigger: trigger}
	if taskID == "" {
		return out, fmt.Errorf("wip snapshot: task_id must not be empty")
	}
	// THE ONE MEASUREMENT. ReclaimRunResidueCheck already answers "is this the
	// row's OWN worktree, and what does it hold?" — including the attribution
	// ladder that keeps this leg OFF the shared main checkout and off a sibling
	// run's tree (Rule 29/30's boundary). A second, parallel measurement here
	// would be a second opinion about ownership (Rule 14).
	res, err := ReclaimRunResidueCheck(taskID)
	if err != nil {
		return out, fmt.Errorf("wip snapshot: %s: measure: %w", taskID, err)
	}
	out.Worktree = res.Worktree
	if res.Worktree == "" {
		// No attributable ground of this row: nothing was measured, so nothing
		// is claimed to have been saved.
		return out, nil
	}
	out.Entries = len(taskWIPPorcelainEntries(res.Porcelain))
	if reclaimResidueClass(res.Porcelain) == reclaimResidueClean {
		out.Clean = true
		out.Note = fmt.Sprintf("wip snapshot (%s): %s holds no uncommitted work — nothing written", trigger, res.Worktree)
		return out, nil
	}
	dir, derr := taskWIPDeposit(taskID, res.Worktree, res.Porcelain, &out)
	if derr != nil {
		out.Note = fmt.Sprintf("wip snapshot (%s): %s: deposit FAILED — %v", trigger, res.Worktree, derr)
		return out, derr
	}
	out.Path = dir
	return out, nil
}

// taskWIPDeposit builds the deposit directory and every file inside it. Its
// first act is the ONE mkdir — reached only when the caller has already proved
// there is something to save.
func taskWIPDeposit(taskID, worktree, porcelain string, out *TaskWIPSnapshot) (string, error) {
	sessionDir, err := taskSessionDir(taskID)
	if err != nil {
		return "", err // the refusal names the operand; the caller reports it
	}
	if sessionDir == "" {
		return "", fmt.Errorf("no session directory for %s", taskID)
	}
	dir := filepath.Join(sessionDir, taskWIPDirName)
	if err := os.MkdirAll(filepath.Join(dir, taskWIPUntrackedDirName), 0o755); err != nil {
		return "", err
	}
	// ① the inventory, exactly as git printed it.
	if err := os.WriteFile(filepath.Join(dir, taskWIPStatusFile), []byte(porcelain), 0o644); err != nil {
		return "", err
	}
	// ② the tracked half: one applyable patch, binary content included.
	//
	// It is written STRAIGHT TO THE FILE, not captured into a string first. A
	// patch's bytes ARE the deliverable (`git apply` recomputes every hunk's
	// context from them), and the package's parsed-read helper trims its result —
	// measured here: a captured patch applied with `error: corrupt patch at line
	// 7`, because the trailing newline of the last context line was trimmed away.
	// So this one call does no parsing at all, which is exactly why it does not
	// go through the parsed-reader (that helper's contract is a PARSED value).
	if err := taskWIPGitToFile(worktree, filepath.Join(dir, taskWIPTrackedPatchFile), taskWIPDiffArgs...); err != nil {
		return "", err
	}
	out.TrackedFiles = taskWIPPatchFileCount(porcelain)
	// ③ the untracked half: one copy per file, at its worktree-relative path.
	copied, skipped := taskWIPCopyUntracked(worktree, porcelain, dir, out)
	out.UntrackedFiles = copied
	out.Skipped = skipped
	if err := os.WriteFile(filepath.Join(dir, taskWIPUntrackedListFile),
		[]byte(strings.Join(out.untrackedPaths, taskWIPUntrackedListSep)), 0o644); err != nil {
		return "", err
	}
	// ④ the hand-over note: the whole recipe, on disk, needing no context.
	if err := os.WriteFile(filepath.Join(dir, taskWIPHandoffFile),
		[]byte(taskWIPHandoffText(taskID, worktree, porcelain, *out)), 0o644); err != nil {
		return "", err
	}
	out.Note = fmt.Sprintf("wip snapshot (%s): %s preserved at %s — %d tracked file(s) in %s, %d untracked file(s) in %s/%s/%s",
		out.Trigger, worktree, dir, out.TrackedFiles, taskWIPTrackedPatchFile,
		out.UntrackedFiles, taskWIPDirName, taskWIPUntrackedDirName, taskWIPUntrackedListFile)
	return dir, nil
}

// taskWIPGitToFile runs one READ-ONLY git verb in dir with its stdout written
// BYTE-FOR-BYTE into path. Nothing is parsed, trimmed or re-encoded: the file IS
// the value. Its stderr is captured separately so a failure carries git's own
// sentence (Rule 15), and the verb list is the caller's — every one of them is a
// read (Rule 30: nothing here mutates the tree being measured).
//
// WHY IT EXISTS (the measurement that put it here): capturing `git diff --binary`
// through the package's parsed-read helper and re-writing the returned string
// produced a patch `git apply` refused with `error: corrupt patch at line 7` —
// the helper trims its result, and a patch's final context line is terminated BY
// that newline. A deliverable whose bytes are the value must never pass through a
// parser.
func taskWIPGitToFile(dir, path string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, gitBinaryName, args...)
	cmd.Dir = dir
	cmd.Stdout = f
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	// The close error is REPORTED when the command itself succeeded: a patch that
	// never reached the disk is the failure a caller most needs to hear about.
	if cerr := f.Close(); cerr != nil && runErr == nil {
		return cerr
	}
	if runErr != nil {
		diag := strings.TrimSpace(stderr.String())
		if diag == "" {
			diag = "git said nothing on stderr"
		}
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), runErr, diag)
	}
	return nil
}

// The read vocabulary, spelled once (Rule 1/13).
var (
	// taskWIPDiffArgs renders every TRACKED change against HEAD as one patch.
	// `--binary` is what makes a moved binary file restorable instead of a
	// "Binary files differ" placeholder — the deposit is worthless without it.
	taskWIPDiffArgs = []string{taskWIPDiffVerb, taskWIPHeadArg, taskWIPBinaryFlag}
)

// taskWIPPorcelainEntry is one line of `git status --porcelain`, split into the
// two codes and the path (renames carry "old -> new").
type taskWIPPorcelainEntry struct {
	XY   string
	Path string
}

// taskWIPPorcelainEntries parses the porcelain body into entries. The parse is
// deliberately literal: two status columns, one separator space, and the rest
// is the path. A rename's "old -> new" is reduced to its NEW side (the side
// that exists on disk and is therefore copyable).
func taskWIPPorcelainEntries(porcelain string) []taskWIPPorcelainEntry {
	var out []taskWIPPorcelainEntry
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		path := strings.TrimSpace(line[3:])
		if arrow := strings.LastIndex(path, taskWIPRenameArrow); arrow >= 0 {
			path = strings.TrimSpace(path[arrow+len(taskWIPRenameArrow):])
		}
		if path == "" {
			continue
		}
		out = append(out, taskWIPPorcelainEntry{XY: xy, Path: path})
	}
	return out
}

// taskWIPPatchFileCount counts the entries whose change is TRACKED (anything but
// the untracked marker) — the half the patch carries.
func taskWIPPatchFileCount(porcelain string) int {
	n := 0
	for _, e := range taskWIPPorcelainEntries(porcelain) {
		if !strings.HasPrefix(e.XY, reclaimPorcelainUntrackedMarker) {
			n++
		}
	}
	return n
}

// taskWIPCopyUntracked copies every untracked file in the porcelain body into
// <dir>/untracked/<its own relative path>. It answers how many it copied and
// which entries it could NOT carry — a size bound or a vanished file is NAMED,
// never a silent omission (Rule 15).
func taskWIPCopyUntracked(worktree, porcelain, dir string, out *TaskWIPSnapshot) (int, []string) {
	var copied int
	var skipped []string
	var budget int64 = taskWIPMaxTotalBytes
	seen := 0
	for _, e := range taskWIPPorcelainEntries(porcelain) {
		if !strings.HasPrefix(e.XY, reclaimPorcelainUntrackedMarker) {
			continue // the tracked half rides the patch
		}
		seen++
		if seen > taskWIPMaxUntrackedEntries {
			skipped = append(skipped, fmt.Sprintf("(stopped after %d untracked entries)", taskWIPMaxUntrackedEntries))
			break
		}
		src := filepath.Join(worktree, e.Path)
		fi, err := os.Lstat(src)
		if err != nil {
			skipped = append(skipped, e.Path+" (vanished before the copy)")
			continue
		}
		if fi.IsDir() {
			skipped = append(skipped, e.Path+" (directory)")
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			// A symlink is copied as the LINK itself, never followed: following
			// it would copy whatever it points at, which may be outside the tree.
			if _, lerr := taskWIPCopyTo(os.Readlink, src, filepath.Join(dir, taskWIPUntrackedDirName, e.Path)); lerr != nil {
				skipped = append(skipped, e.Path+" (symlink: "+lerr.Error()+")")
				continue
			}
			out.untrackedPaths = append(out.untrackedPaths, e.Path)
			copied++
			continue
		}
		if fi.Size() > taskWIPMaxFileBytes || fi.Size() > budget {
			skipped = append(skipped, fmt.Sprintf("%s (%d bytes, over the snapshot budget)", e.Path, fi.Size()))
			continue
		}
		budget -= fi.Size()
		if _, cerr := taskWIPCopyFile(src, filepath.Join(dir, taskWIPUntrackedDirName, e.Path), fi.Mode().Perm()); cerr != nil {
			skipped = append(skipped, e.Path+" ("+cerr.Error()+")")
			continue
		}
		out.untrackedPaths = append(out.untrackedPaths, e.Path)
		copied++
	}
	return copied, skipped
}

// taskWIPCopyFile copies one file, creating its parent directory. It is the ONE
// copy primitive (Rule 14) — ReadFile+WriteFile, bounded by the caller's
// budget, never a shell subprocess (Rule 35: no new external command).
func taskWIPCopyFile(src, dst string, perm os.FileMode) (int64, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(dst, data, perm); err != nil {
		return 0, err
	}
	return int64(len(data)), nil
}

// taskWIPCopyTo is taskWIPCopyFile for content the caller produces (a symlink's
// target): the reader is a parameter so the symlink case needs no second copy
// implementation with a different source type.
func taskWIPCopyTo(reader func(string) (string, error), src, dst string) (int64, error) {
	target, err := reader(src)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	if err := os.WriteFile(dst, []byte(target), 0o644); err != nil {
		return 0, err
	}
	return int64(len(target)), nil
}

// taskWIPHandoffText renders the note a successor reads first: what was
// measured, WHERE from (the branch and the tip, so a reader can tell whether
// the deposit still applies), and the exact restore recipe. Every command is
// complete and pasteable (the operator copies these out of the file).
func taskWIPHandoffText(taskID, worktree, porcelain string, out TaskWIPSnapshot) string {
	entries := taskWIPPorcelainEntries(porcelain)
	var b strings.Builder
	b.WriteString("# WIP snapshot — the uncommitted work of run " + taskID + "\n\n")
	b.WriteString("This directory is the automatic deposit of a run's uncommitted work,\n")
	b.WriteString("written when the run was still standing in its own worktree. It is the\n")
	b.WriteString("artifact a successor (or a human) restores from; the original worktree\n")
	b.WriteString("may already be gone.\n\n")
	b.WriteString("```\n")
	b.WriteString("measured at     : " + time.Now().Format(time.RFC3339) + "\n")
	b.WriteString("trigger         : " + out.Trigger + "\n")
	b.WriteString("source worktree : " + worktree + "\n")
	b.WriteString("porcelain lines : " + fmt.Sprintf("%d", len(entries)) + "\n")
	b.WriteString("tracked entries : " + fmt.Sprintf("%d", out.TrackedFiles) + "  (in " + taskWIPTrackedPatchFile + ")\n")
	b.WriteString("untracked files : " + fmt.Sprintf("%d", out.UntrackedFiles) + "  (copies in " + taskWIPUntrackedDirName + "/)\n")
	b.WriteString("```\n\n")
	if len(out.Skipped) > 0 {
		b.WriteString("## NOT carried by this deposit\n\n")
		for _, s := range out.Skipped {
			b.WriteString("- " + s + "\n")
		}
		b.WriteString("\nIf one of those is the work, it exists only in the source worktree\n")
		b.WriteString("(if that still exists) — copy it before the directory is reclaimed.\n\n")
	}
	b.WriteString("## Restore\n\n")
	b.WriteString("Run these from the ROOT of the branch you are restoring onto. The patch\n")
	b.WriteString("lists full paths from the source worktree's root.\n\n")
	b.WriteString("```\n")
	b.WriteString("cd <your worktree>\n")
	b.WriteString("git apply --binary --3way " + filepath.Join(taskSessionDirHintForHandoff(taskID), taskWIPTrackedPatchFile) + "\n")
	b.WriteString("cp -r " + filepath.Join(taskSessionDirHintForHandoff(taskID), taskWIPUntrackedDirName) + "/. .\n")
	b.WriteString("git status --porcelain    # compare with " + taskWIPStatusFile + " beside this file\n")
	b.WriteString("```\n\n")
	b.WriteString("`" + taskWIPTrackedPatchFile + "` carries every TRACKED change (binary files included).\n")
	b.WriteString("`" + taskWIPUntrackedDirName + "/` holds one copy per untracked file at its own\n")
	b.WriteString("worktree-relative path; `" + taskWIPUntrackedListFile + "` lists exactly which ones were\n")
	b.WriteString("copied. `" + taskWIPStatusFile + "` is the raw measurement both came from.\n\n")
	b.WriteString("## What this deposit is NOT\n\n")
	b.WriteString("- It is not a commit. Nothing was committed, staged, stashed or cleaned in\n")
	b.WriteString("  the source worktree by the run that wrote it.\n")
	b.WriteString("- It is not a guarantee. The work is only safe once it is on a branch.\n")
	return b.String()
}

// taskSessionDirHintForHandoff names the deposit directory in the hand-over
// note. It is a DISPLAY value inside a generated document, so an unresolvable
// session dir degrades to a placeholder rather than failing the snapshot (the
// deposit itself has already been written by the time this runs).
func taskSessionDirHintForHandoff(taskID string) string {
	if dir, err := taskSessionDir(taskID); err == nil && dir != "" {
		return filepath.Join(dir, taskWIPDirName)
	}
	return filepath.Join("<session-dir>", taskWIPDirName)
}

// taskWIPRenameArrow is git's own separator inside a porcelain rename entry
// ("old -> new").
const taskWIPRenameArrow = " -> "

// The read vocabulary this file's two git calls are spelled from (Rule 1: no
// bare literals). All three are global git spellings rather than this file's own
// invention, and BLAME for a change to them is the reason they are named here.
const (
	// taskWIPDiffVerb is the patch-producing read (never a mutation).
	taskWIPDiffVerb = "diff"
	// taskWIPHeadArg scopes that diff to the committed tree.
	taskWIPHeadArg = "HEAD"
	// taskWIPBinaryFlag makes `git diff` emit restorable binary hunks instead of
	// a "Binary files differ" placeholder — the deposit is worthless without it.
	taskWIPBinaryFlag = "--binary"
)

// taskWIPFile records one event on the row's trail. A trail that cannot be
// written is REPORTED to the operator log and to stderr, never swallowed
// (Rule 15) — the snapshot itself has already happened by the time this runs.
func taskWIPFile(event, taskID, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), reclaimGitTimeout)
	defer cancel()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		taskNotifyLog("wip snapshot event %s for %s: %v", event, taskID, err)
		fmt.Fprintln(os.Stderr, "[task] wip snapshot: "+detail)
		return
	}
	defer db.Close()
	taskEventWrite(ctx, db, taskID, event, detail)
	taskNotifyLog("%s", detail)
}

// SnapshotBeforeFailed is the `failed`-writeback leg: it snapshots the row's
// uncommitted work and REPORTS the outcome on the row's trail.
//
// It is called from taskUpdateStampedCore before the state write, and it is
// deliberately NON-BLOCKING in every direction: the transition the caller asked
// for is the ledger's record of what happened, and losing that record because a
// copy failed would be worse than losing the copy. What it must never do is
// fail SILENTLY — a refused or failed deposit is filed as an event and printed
// (Rule 15).
func SnapshotBeforeFailed(taskID string) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return
	}
	snap, err := SnapshotUncommittedWork(taskID, taskWIPTriggerFailed)
	if err != nil {
		taskWIPFile(taskWIPSnapshotFailedEvent, taskID, snap.Note+" — "+err.Error())
		return
	}
	if snap.Worktree == "" {
		return // no attributable ground: nothing measured, nothing claimed
	}
	if snap.Clean {
		return // a clean writeback writes nothing at all, not even a note
	}
	taskWIPFile(taskWIPSnapshotEvent, taskID, snap.Note)
}
