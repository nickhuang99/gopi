// task_resumed_session.go — the SESSION-EXISTENCE check behind
// tools.TaskTagResumed (F16.1, task 01a0ed37-4092-7ec5-a050-21da34cef04c).
//
// ITS CALLER is the resume path (cmd/agent/cli_resume.go, a direct in-process
// call). The `gopi task tag-resumed` CLI spelling that used to expose the same
// function was DELETED 2026-10-16 (verb-surface retirements: an internal hook
// exposed as a verb, 0 executions) — the CHECK below is untouched by that
// deletion, and it still guards every stamp the resume path writes.
//
// WHY IT EXISTS. TaskTagResumed used to check only that both positionals were
// non-empty, so it stamped `resumed:<anything>` into the row's tags: the
// all-zero uuid, a typo, and another task's session were all accepted, and
// `gopi task show` then summarised the row as `resumed: ×1` while the row's
// own `session_id:` column stayed empty — the row contradicting itself, and
// the pollution propagating into rows minted from it. An audit stamp that
// any string satisfies is not an audit stamp (Rule 15: bad input is refused,
// never silently accepted).
//
// WHAT IS CHECKED — a real artifact this profile left on disk, in any of the
// shapes a session writes (all under the gopi data root, resolved by the ONE
// resolver, config.GopiDir):
//
//	sessions/<task-id>/<session-id>.jsonl          a task run's log
//	sessions/<task-id>/<session-id>.raw.log        … its co-located raw log
//	sessions/<session-id>/session.jsonl            a bare prompt/chat session
//	sessions/<session-id>.jsonl                    the legacy flat layout
//	sessions/<session-id>/                         its evidence directory
//
// The session id is the run uuid, and every one of those names the id in its
// FILE NAME — so the check is a name probe per directory, never a pass over
// every record (the tree holds 10^5 entries here; the cost of getting this
// wrong is `internal/testutil/suiteprofile.go`'s measured 4,000 → 272s scan).
// The task-run directory is named for the SESSION'S OWN row group, which the
// ledger names (taskSessionDirHints — the same hint the row-lookup resolver
// uses), so the probe reaches it even when the caller passes another task id.
//
// AN EXISTING ARTIFACT IS THE ONLY ADMISSION (never a fallback). The one
// caller inside gopi (cli_resume.go) stamps a session id it has just written
// a JSONL for, so its artifact is on disk before the call; an id that matches
// nothing is refused BY NAME, and nothing is written to the row's tags.

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/fullid"
)

const (
	// taskSessionRawLogSuffix is the suffix of the raw HTTP log a run writes
	// BESIDE its session JSONL (internal/agent.newRawLoggerInDir, the writer
	// that owns this value). Declared here with the writer named, because
	// importing it would close an import cycle (internal/agent imports
	// internal/tools). It is a NON-standard spelling of the uuid shape (a
	// `.raw.log` tail is not a hex digit), so a session id can never be
	// confused with one of these names when a directory is listed.
	taskSessionRawLogSuffix = ".raw.log"
)

// taskSessionArtifactNames returns the candidate artifact names for sessionID
// in ONE directory of the sessions tree — every shape a real session leaves,
// as a NAME (no directory listing, no record parse). Ordered so the cheapest
// and most specific candidate is probed first.
func taskSessionArtifactNames(sessionID string) []string {
	return []string{
		sessionID + taskSessionJSONLExt,     // the run's session log
		sessionID + taskSessionRawLogSuffix, // its co-located raw log
		sessionID,                           // its bare-session directory
		taskBareSessionRecordName,           // …holding <session-id>/session.jsonl
	}
}

// taskSessionArtifactIn reports whether dir holds ANY artifact of sessionID.
// It is the ONE per-directory probe: every candidate is a name lookup, so a
// "not found" costs a handful of failed stats and never a readdir of the tree.
//
// THE DIRECTORY NAME IS A CANDIDATE ONLY WHEN IT IS A DIRECTORY, and only in
// the LAYOUTS a bare session uses — never any directory. The sessions tree is
// also organised BY TASK (sessions/<task-id>/…), so accepting a bare directory
// name would make a task id pass the check as if it were a session, i.e. the
// very pollution this function exists to stop. The two bare layouts are
// sessions/<session-id>/ holding session.jsonl, and a run directory whose own
// log is named for the id (a task id has no such log).
func taskSessionArtifactIn(dir, sessionID string) bool {
	for _, name := range taskSessionArtifactNames(sessionID) {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if name != sessionID {
			return true // a name stamped with the id: the log, the raw log, session.jsonl
		}
		// The bare directory: session dirs are shaped exactly like a UUID, and
		// so is a TASK directory (sessions/<task-id>/…), a wrapper's claim
		// directory or a run directory — so the name ALONE decides nothing.
		// What a session directory must hold is the record of a session (the
		// task-run layout needs no record here: its own log is named for the
		// id and the caller's task directory is probed too).
		if !fi.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name, taskBareSessionRecordName)); err == nil {
			return true
		}
	}
	return false
}

// taskResumedSessionArtifact resolves sessionID against THIS profile's
// session artifacts and returns the candidate id that names one. Callers hold
// either a full uuid (the resume path writes one) or the short chunk a
// rendered listing printed, so the resolution has two arms:
//
//   - a FULL id: the probe above, over the sessions root, the row's own task
//     directory and the ledger's hint directories. Cost is a readdir per
//     hinted directory (the ledger read is the one taskSessionDirHints
//     already memoises per process).
//   - a SHORT id (the shape internal/fullid refuses to resolve — 8..35
//     characters of a uuid): the ROOT-LEVEL directory names are the candidate
//     set (which is also where the legacy flat records live), and the ONE
//     full id that starts with the chunk is returned. gen-uuid-v4 uses
//     crypto/rand, so an 8-character chunk names exactly one local session;
//     zero or several matching ids are refused with the ids NAMED
//     (fullid.Refusal), never silently picked.
//
// The returned id is the CANONICAL identity the caller holds: one full id, or
// one short id that names a real artifact (a pre-uuid-v4 name, which no
// expansion could improve). It is "" when no artifact of the operand exists
// at all.
func taskResumedSessionArtifact(taskID, sessionID string) (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Errorf("task %s: cannot resolve the gopi data root: %w", taskResumedLabel, err)
	}
	dir := filepath.Join(gd, config.SessionsDirName)
	if !fullid.CanBePrefix(sessionID) {
		if taskResumedSessionDirsContaining(dir, taskID, sessionID) {
			return sessionID, nil
		}
		return "", nil
	}
	// Short id: expand over the ROOT's own candidate names first (the cheapest
	// set and the one the bare/legacy layouts live in), then over the
	// ledger-hinted task directories (a task run files its log as
	// sessions/<task-id>/<session-id>.jsonl, so the id appears one level down).
	// Both scans are readdirs of ONE directory each — never a walk of a tree
	// holding 10^5 entries.
	cands := taskResumedShortCandidates(dir, sessionID)
	for _, hint := range taskResumedSessionHints(taskID, sessionID) {
		cands = append(cands, taskResumedShortCandidates(filepath.Join(dir, hint), sessionID)...)
	}
	switch len(cands) {
	case 0:
		return "", nil
	case 1:
		return cands[0], nil
	default:
		return "", fullid.Refusal(fullid.KindSession, sessionID, cands)
	}
}

// taskResumedShortCandidates lists the ids ONE directory holds that begin with
// the short operand: the session DIRECTORY names and the JSONL log names at
// that level (the bare-session and legacy-flat layouts), each reduced to the
// id it carries. Only canonical uuid shapes are candidates — the operand is a
// chunk OF a uuid, so a name of any other shape cannot be its expansion.
//
// A directory that does not exist contributes NOTHING (a profile with no
// sessions yet, or a hinted task directory that was pruned) — never an error:
// the caller's answer for "no candidate anywhere" is the named refusal, which
// is the caller's to build.
func taskResumedShortCandidates(dir, short string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, taskSessionJSONLExt) {
			name = strings.TrimSuffix(name, taskSessionJSONLExt)
		}
		if !strings.HasPrefix(name, short) || !fullid.IsUUID(name) {
			continue
		}
		if slicesContainsString(out, name) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// taskResumedSessionDirsContaining reports whether sessionID names an artifact
// ANYWHERE in this profile: the sessions root itself, the given task's own run
// directory, and the run directories the LEDGER names for that session.
func taskResumedSessionDirsContaining(root, taskID, sessionID string) bool {
	if taskSessionArtifactIn(root, sessionID) {
		return true
	}
	for _, dir := range taskResumedSessionHints(taskID, sessionID) {
		if taskSessionArtifactIn(filepath.Join(root, dir), sessionID) {
			return true
		}
	}
	return false
}

// taskResumedSessionHints returns the session-DIR names to probe for a session
// id: the row's own task id (the TASK IS the directory name of its run,
// session-dir-unify S1) plus the ledger's hint directories (a session that
// submitted rows is filed under ITS OWN task id — taskSessionDirHints). The
// list is deduplicated so one directory is probed once.
//
// WHY THE ROW'S OWN DIRECTORY CANNOT BE ASSUMED: the caller's taskID is the
// TASK side of the command, and the ordinary case — `gopi session resume --task T`
// stamping the fresh session's id — has the fresh session in T's own
// directory. The audit case (another task's session) is exactly what the
// ledger hints are for; a hit that is in neither is an unknown session and is
// refused.
func taskResumedSessionHints(taskID, sessionID string) []string {
	var out []string
	// The row's OWN directory first: it is the address of the ordinary case
	// (a task's own run), and one stat either way.
	if id := taskIDNormalize(taskID); id != "" {
		out = append(out, id)
	}
	// The resume linkage is a task-runs-a-session record, so the session's own
	// row is normally the strongest address. The ledger hint query returns the
	// rows carrying this session id, newest first, and — for a session that
	// submitted rows — the SUBMITTER's task id, which is where the log lives.
	for _, hint := range taskSessionDirHints(sessionID) {
		id := taskIDNormalize(hint)
		if id == "" {
			continue
		}
		if slicesContainsString(out, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// slicesContainsString is the small membership test the hint de-duplication
// needs — the ONE place in this file that asks it (a second spelling of the
// dedupe rule is how two directories get probed twice).
func slicesContainsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// taskResumedSessionLabel is the noun the refusals name — a bare literal would
// be a Rule 1 violation at each of the three sites.
const taskResumedLabel = "session id"
