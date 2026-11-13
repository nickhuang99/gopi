package tools

// task_ref_history.go — THE REACHABLE-HISTORY READER: which task ids does a
// repository's reachable commit history NAME?
//
// WHY THIS FILE EXISTS (Rule 43, task 01a18799). Every symbol here answered
// "is this row's work named in reachable history?" from inside
// task_ref_reachability.go — the file the retired `gopi task orphans` verb
// owned. A reference census over that file showed the readers below are
// consumers OUTSIDE the retired mechanism: `task_completion_witness.go` and
// `task_deliverable_tip.go` both ask this question about rows that have
// nothing to do with the orphans readout. They are shared instruments, not
// parts of the retired verb, so they MOVE here in the same commit that deletes
// the verb's own file — never a symbol deleted out from under a live reader,
// and never a copy left behind in two places (Rule 14).
//
// WHAT IT IS, MEASURED. A terminal row's deliverable branch can disappear for
// two very different reasons: the work LANDED (the merger's commit subject
// names the row — `Merge … (re-lands 01a12ff2)`) or the ref was DELETED after
// a delivery that never happened. "Some reachable commit names this task id"
// is true of the first and false of the second, so it is the discriminator
// every consumer of this file rests on. It is asked of the OBJECT STORE with
// ONE `git log --all`, never per row.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// The reachable-history enumerator's git spellings (Rule 1b). `log --all` is
// what makes the set "reachable from SOME ref", which is the question — a commit
// present in the object store yet reachable from no ref (the incident's own
// state) must not count as carrying the work.
const (
	taskRefLogVerb    = "log"
	taskRefAllFlag    = "--all"
	taskRefFormatFlag = "--format"
)

// taskRefHistoryFormat is the ONE enumerator of reachable commit subjects (Rule
// 13): sha, a unit separator, and the subject. `--all` is what makes the set
// "reachable from some ref", which is the question — a commit present in the
// object store but reachable from no ref (the incident's own state) is precisely
// what must NOT count.
//
// THE FORMAT IS BOUND WITH `=`, AND THAT IS NOT COSMETIC (measured 2026-10-12).
// `git log --format <value>` (two argv entries) does NOT bind: git reads the
// value as a REVISION, exits 128 with "fatal: ambiguous argument", and
// `cmd.Output()` leaves that stderr unread — so the caller sees only an exit
// status, which is the silent failure the fail-closed arms exist to catch.
// `git log --format=<value>` (one argv entry) is the spelling git defines for
// this verb. (`for-each-ref` DOES accept the two-argument form, which is why the
// sibling constants beside the ref readout are used that way and this one is
// not.)
const (
	taskRefHistoryFormat = "%H%x1f%s"
	taskRefHistorySep    = "\x1f"
)

// taskRefGitOneShotTimeout bounds ONE git invocation issued by this reader. It
// is a network/disk budget, not a scan's: ONE probe answers the WHOLE ledger, so
// the cost does not grow with the number of rows — a scan whose result depended
// on how many rows a ledger happened to hold would not be a measurement.
//
// MOVED with `taskBranchRefsGit` (Rule 43): the timeout and the helper it bounds
// are one instrument, so they live together.
const taskRefGitOneShotTimeout = 30 * time.Second

// taskRefGit runs one git command in dir and returns stdout. It is the ONE
// invoker every reachable-history reader uses (Rule 13/14) — the completion
// witness, the deliverable-tip probe and the task-id index all read the object
// store through it, so a change to the timeout or the error shape reaches all
// three by construction.
func taskRefGit(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, taskRefGitOneShotTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// taskRefNamedTaskIDs indexes the task ids NAMED by the subject of any commit
// reachable from any ref. One `git log --all` (Rule 13), and the index is what
// makes the per-row question free.
//
// A commit SUBJECT is the field the merger writes the row id into (`Merge the
// disk-exhaustion abstention class into the build gate (re-lands 01a12ff2)`),
// so a substring scan for the id is the measurement. An id is a UUID, so the
// only ids a subject can accidentally contain are ids that were written there.
func taskRefNamedTaskIDs(ctx context.Context, repo string) (map[string]bool, string, error) {
	out := map[string]bool{}
	if strings.TrimSpace(repo) == "" {
		return out, "", errors.New("no repository was named")
	}
	raw, err := taskRefGit(ctx, repo, taskRefLogVerb, taskRefAllFlag,
		taskRefFormatFlag+"="+taskRefHistoryFormat)
	if err != nil {
		return out, "", fmt.Errorf("read the reachable history of %s: %w", repo, err)
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, taskRefHistorySep, 2)
		if len(parts) != 2 {
			continue
		}
		subject := parts[1]
		for _, id := range taskRefIDsInSubject(subject) {
			out[id] = true
		}
	}
	return out, raw, nil
}

// taskRefIDsInSubject extracts the hex runs a commit subject carries, keeping
// every run long enough to be a NAME (taskRefIDPrefixLen .. taskRefIDHexLen).
// Both spellings are indexed — the merger's short tail and the ledger's full or
// dashed id — so the two cannot disagree (Rule 14).
func taskRefIDsInSubject(subject string) []string {
	var out []string
	run := strings.Builder{}
	flush := func() {
		n := run.Len()
		switch {
		case n < taskRefIDPrefixLen:
		case n > taskRefIDHexLen:
			out = append(out, run.String()[:taskRefIDHexLen])
		default:
			out = append(out, run.String())
		}
		run.Reset()
	}
	for _, r := range subject {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			run.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	// A DASHED id is the ledger's own spelling and is indexed too, so a subject
	// that carries the full id is found even though its hex runs are split by
	// dashes (`01a12ff2-a6e0-…` — four runs, none of them the whole id).
	for i := 0; i+taskRefDashedIDLen <= len(subject); i++ {
		cand := subject[i : i+taskRefDashedIDLen]
		if taskRefLooksLikeDashedID(cand) {
			out = append(out, strings.ReplaceAll(cand, taskRefIDDash, ""))
		}
	}
	return out
}

// taskRefHistoryNames reports whether the reachable history NAMES this row by
// its task id — the FULL id, never a short tail.
//
// WHY THE FULL ID, MEASURED. A task id's first 8 hex digits are a TIMESTAMP
// PREFIX, not a random token: the two rows the orphans incident is about,
// 01a12ff2-a6e0-… and 01a12ff2-a6a8-…, share all eight. So `01a12ff2` appearing
// in the reachable history (`Merge … (re-lands 01a12ff2)`) is evidence that SOME
// row was re-landed, not that either of these was — a matcher accepting 8-hex
// runs therefore declared both incident rows "settled" (measured 2026-10-12,
// their own diagnostic run). Any prefix shorter than the whole id is ambiguous
// whenever two rows share a timestamp, which is the normal case for a batch
// submitted together; only the full id is unambiguous.
//
// The cost of that strictness is bounded and is REPORTED, not hidden: the short
// tails a subject carries (8..31 hex) match nothing here, so a row that was
// genuinely re-landed AND named only by a short tail reads as unaccounted. The
// direction is deliberate — a check that cannot prove a row was landed must not
// claim it was (Rule 41).
func taskRefHistoryNames(indexed map[string]bool, taskID string) bool {
	id := strings.ReplaceAll(strings.TrimSpace(taskID), taskRefIDDash, "")
	if len(id) < taskRefIDHexLen {
		return false
	}
	return indexed[id[:taskRefIDHexLen]]
}

const (
	// taskRefIDHexLen — a task id's hex payload, undashed.
	taskRefIDHexLen = 32
	// taskRefIDPrefixLen is the shortest hex run the index KEEPS: a task id's
	// leading 8 hex digits are a timestamp prefix, and the merger writes exactly
	// those (`(re-lands 01a12ff2)`), so they are worth indexing to see — but
	// they never MATCH a row (see taskRefHistoryNames: matching is full-id or
	// nothing, because two rows submitted in one batch share the prefix).
	taskRefIDPrefixLen = 8
	// taskRefDashedIDLen — the ledger's own spelling, `8-4-4-4-12` = 32 hex +
	// 4 dashes.
	taskRefDashedIDLen = 36
	taskRefIDDash      = "-"
)

// taskRefLooksLikeDashedID reports whether s has the ledger id's shape with a
// hex payload — the cheap positional test that keeps a prose word from being
// read as an id.
func taskRefLooksLikeDashedID(s string) bool {
	dashes := 0
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
			dashes++
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return dashes == 4
}
