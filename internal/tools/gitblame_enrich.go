package tools

// gitblame_enrich.go — the LEDGER half of git_blame (epic
// gitblame-task-enrichment). gitblame.go keeps the blame call itself and its
// parsing contract; this file joins each blamed commit into the task ledger.
//
// The join key is the `task:` trailer the commit seam writes
// (internal/tools/git_commit_task_trailer.go), read with
// `%(trailers:key=task,valueonly)` — never with `%(trailers:key=task)`, whose
// rendered form is `Task: <value>` / `task: <value>` and would make every
// reader re-parse git's rendering (measured: commit 90927414a carries the
// trailer TWICE, once as `Task:` and once as `task:`).
//
// COST SHAPE (the whole point of this file's structure): one blamed line is
// not one ledger question. A 200-line window routinely comes from three
// commits, so the hashes are DE-DUPLICATED first, then ONE `git log --stdin`
// call fetches every distinct commit's trailers, then the ledger is opened
// EXACTLY ONCE, read-only, and asked one question per distinct task id. The
// counts are reported in-band (`ledger_access`) so the shape is checkable from
// outside instead of taken on trust.

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// gitBlameCommitTrailers is what ONE commit says about itself: the task row it
// belongs to (the join key) and the machine that produced it.
type gitBlameCommitTrailers struct {
	TaskID string
	Host   string
}

// gitBlameEnrich attaches ledger attribution to every blamed line and RETURNS
// the finished result plus the access counters. It never fails the tool: every
// one of the three fallback layers degrades to "no attribution, said out loud".
func gitBlameEnrich(repoRoot, filePath string, items []GitBlameLineItem, ledgerPresent bool) (GitBlameResult, GitBlameLedgerAccess) {
	res := GitBlameResult{FilePath: filePath, LineItems: items}
	access := GitBlameLedgerAccess{}

	hashes := gitBlameUniqueCommits(items)
	access.Commits = len(hashes)
	if len(hashes) == 0 {
		res.TaskEnrichment = gitBlameEnrichmentNone
		res.TaskEnrichmentNote = "no blamed lines"
		res.LedgerAccess = access
		return res, access
	}

	// Layer ① is decided here: a commit with no `task:` trailer simply has no
	// attribution, and that is ordinary (another project's repo, a hand-made
	// commit, this repository's own initial commit).
	trailers, err := gitBlameCommitTrailersOf(repoRoot, hashes)
	access.TrailerLookups = len(hashes)
	if err != nil {
		access.Warning = fmt.Sprintf("trailer lookup failed: %v", err)
		res.TaskEnrichment = gitBlameEnrichmentNone
		res.TaskEnrichmentNote = "could not read commit trailers (" + access.Warning + ")"
		res.LedgerAccess = access
		return res, access
	}

	// Distinct task ids, in first-appearance order (a 200-line window from 3
	// commits is 3 ids at most, whatever the line count).
	var ids []string
	seen := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		id := trailers[h].TaskID
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}

	// Echo the trailer-derived attribution FIRST: task_id and task_host come
	// from git, so they are known even when the ledger cannot answer (layer ②
	// and ③). Anything ledger-derived is filled in below.
	byHash := make(map[string]GitBlameLineItem, len(hashes))
	for _, h := range hashes {
		tr := trailers[h]
		byHash[h] = GitBlameLineItem{TaskID: sanitizeTaskSummary(tr.TaskID), TaskHost: sanitizeTaskSummary(tr.Host)}
	}

	if len(ids) == 0 {
		res.TaskEnrichment = gitBlameEnrichmentNone
		res.TaskEnrichmentNote = fmt.Sprintf("no `%s:` trailer on any of the %d commit(s) in this window — attribution is not available for this repository",
			gitBlameTaskTrailerKey, len(hashes))
		res.LineItems = gitBlameApplyAttribution(items, byHash)
		res.LedgerAccess = access
		return res, access
	}

	// Layer ③: NO LEDGER AT ALL is the ordinary case for a repo that is not
	// this machine's gopi profile. Note the deliberate absence of an open: the
	// write-open fallback inside openTasksDBReadOnly creates a missing ledger,
	// and a blame READ must never create one (a read that writes is the defect,
	// and it would take the ledger's write lock to answer a read).
	ledgerPath := memoryDBPath()
	if !ledgerPresent {
		res.TaskEnrichment = gitBlameEnrichmentNone
		res.TaskEnrichmentNote = fmt.Sprintf("%d distinct task id(s) found in trailers, but this host has no task ledger at %s — task_id from the commit is still reported", len(ids), ledgerPath)
		res.LineItems = gitBlameApplyAttribution(items, byHash)
		res.LedgerAccess = access
		return res, access
	}

	rows, opens, queries, ledgerErr := gitBlameLookupTasks(ledgerPath, ids)
	access.LedgerOpens = opens
	access.LedgerQueries = queries
	if ledgerErr != nil {
		access.Warning = ledgerErr.Error()
	}

	// Merge: the ledger says what the row IS; git says which row.
	for _, h := range hashes {
		tr := trailers[h]
		row, ok := rows[tr.TaskID]
		if !ok {
			continue
		}
		item := byHash[h]
		item.TaskSummary = sanitizeTaskSummary(row.Summary)
		item.TaskStatus = row.Status
		byHash[h] = item
	}

	resolved := 0
	for _, h := range hashes {
		if byHash[h].TaskSummary != "" || byHash[h].TaskStatus != "" {
			resolved++
		}
	}
	res.LineItems = gitBlameApplyAttribution(items, byHash)
	switch {
	case resolved == len(hashes):
		res.TaskEnrichment = gitBlameEnrichmentFull
		res.TaskEnrichmentNote = fmt.Sprintf("all %d distinct commit(s) resolved to a task row in %s", len(hashes), ledgerPath)
	case resolved > 0:
		res.TaskEnrichment = gitBlameEnrichmentPartial
		res.TaskEnrichmentNote = fmt.Sprintf("%d of %d distinct commit(s) resolved to a task row; the rest name a task this ledger does not hold (or none at all)",
			resolved, len(hashes))
	default:
		// Layer ②: trailer present, row absent — a peer's row, a pruned row,
		// or a hand-written trailer. The task_id is still worth reporting.
		res.TaskEnrichment = gitBlameEnrichmentNone
		res.TaskEnrichmentNote = fmt.Sprintf("trailer(s) name %s, but no such task row exists in %s — task_id is reported from the commit, summary/status cannot be",
			gitBlameUnresolvedIDs(ids), ledgerPath)
	}
	if ledgerErr != nil {
		res.TaskEnrichmentNote += "; ledger read degraded: " + ledgerErr.Error()
	}
	res.LedgerAccess = access
	return res, access
}

// gitBlameUniqueCommits returns the DISTINCT commit hashes of a window in
// first-appearance order. This is the de-duplication the ledger cost depends
// on: without it a 200-line window from 3 commits would ask 200 questions.
func gitBlameUniqueCommits(items []GitBlameLineItem) []string {
	var out []string
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		if it.CommitHash == "" || seen[it.CommitHash] {
			continue
		}
		seen[it.CommitHash] = true
		out = append(out, it.CommitHash)
	}
	return out
}

// gitBlameCommitTrailersOf resolves every given commit's trailers in ONE
// `git log` call. Missing commits (a hash the one deduplicated list carries but
// this repository does not — impossible in practice, but `--no-walk` is silent
// about it) simply have no entry, which reads as "no trailer".
//
// Record framing: `-z` separates records by NUL, which a commit body cannot
// contain; inside a record, 0x1f separates the three fields and 0x1e separates
// repeated values of one key. None of those can appear literally in a body, so
// no message or trailer value can forge a boundary.
func gitBlameCommitTrailersOf(repoRoot string, hashes []string) (map[string]gitBlameCommitTrailers, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBlameTrailerTimeout)
	defer cancel()

	format := "%H" + gitBlameTrailerFieldSep +
		fmt.Sprintf("%%(trailers:key=%s,valueonly,separator=%%x1e)", gitBlameTaskTrailerKey) + gitBlameTrailerFieldSep +
		fmt.Sprintf("%%(trailers:key=%s,valueonly,separator=%%x1e)", gitBlameHostTrailerKey)
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "log", "--no-walk=unsorted", "--stdin", "-z",
		"--format="+strings.ReplaceAll(format, gitBlameTrailerFieldSep, "%x1f"))
	cmd.Stdin = strings.NewReader(strings.Join(hashes, "\n") + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// The trailer is supplementary: git already answered the blame. Report
		// the degradation upward instead of failing the tool (Rule 15: the
		// caller STATES it, never swallows it).
		return nil, fmt.Errorf("git log: %w (%s)", err, gitBlameFirstLine(stderr.String()))
	}

	out := make(map[string]gitBlameCommitTrailers, len(hashes))
	for _, rec := range strings.Split(stdout.String(), gitBlameLogRecordSep) {
		rec = strings.TrimPrefix(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, gitBlameTrailerFieldSep)
		if len(fields) < 3 {
			continue
		}
		hash := strings.TrimSpace(fields[0])
		if hash == "" {
			continue
		}
		out[hash] = gitBlameCommitTrailers{
			TaskID: firstTrailerValue(fields[1]),
			Host:   firstTrailerValue(fields[2]),
		}
	}
	return out, nil
}

// firstTrailerValue takes the FIRST value of a repeated trailer. A commit that
// carries the same trailer twice (measured: 90927414a writes both `Task:` and
// `task:`) yields both values here; the first is the one the seam wrote, and
// picking one deterministically beats joining them into a value that matches no
// row at all.
func firstTrailerValue(field string) string {
	if i := strings.Index(field, gitBlameTrailerValueSep); i >= 0 {
		field = field[:i]
	}
	return strings.TrimSpace(field)
}

// gitBlameLookupTasks opens the ledger ONCE, read-only, and asks one question
// per distinct task id. `opens`/`queries` are returned rather than inferred so
// the cost shape of a call is a reported fact.
func gitBlameLookupTasks(ledgerPath string, ids []string) (map[string]*Task, int, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitBlameLedgerTimeout)
	defer cancel()

	db, err := openTasksDBReadOnly(ctx, ledgerPath)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ledger open failed: %w", err)
	}
	defer db.Close() //stylegate:ignore read-only handle closed at return; its lookup errors are the ones that matter

	rows := make(map[string]*Task, len(ids))
	queries := 0
	for _, id := range ids {
		queries++
		row, lookErr := taskByID(ctx, db, id)
		if lookErr != nil {
			// One unreadable row must not cost the other ids their answer; the
			// caller turns this into the note's "ledger read degraded" clause.
			return rows, 1, queries, fmt.Errorf("ledger lookup %s: %w", id, lookErr)
		}
		if row != nil {
			rows[id] = row
		}
	}
	return rows, 1, queries, nil
}

// gitBlameApplyAttribution copies the per-commit attribution onto every line
// whose commit it belongs to.
func gitBlameApplyAttribution(items []GitBlameLineItem, byHash map[string]GitBlameLineItem) []GitBlameLineItem {
	if len(items) == 0 {
		return items
	}
	out := make([]GitBlameLineItem, len(items))
	copy(out, items)
	for i := range out {
		att, ok := byHash[out[i].CommitHash]
		if !ok {
			continue
		}
		out[i].TaskID = att.TaskID
		out[i].TaskSummary = att.TaskSummary
		out[i].TaskStatus = att.TaskStatus
		out[i].TaskHost = att.TaskHost
	}
	return out
}

// gitBlameUnresolvedIDs renders the task ids a ledger could not resolve, capped
// so a wide window cannot echo an unbounded list back at the model.
func gitBlameUnresolvedIDs(ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	shown := sorted
	more := 0
	if len(shown) > gitBlameUnresolvedNoteCap {
		more = len(shown) - gitBlameUnresolvedNoteCap
		shown = shown[:gitBlameUnresolvedNoteCap]
	}
	out := strings.Join(shown, ", ")
	if more > 0 {
		out += fmt.Sprintf(" (+%d more)", more)
	}
	return out
}

// gitBlameFirstLine keeps git's stderr to one line: the degradation note rides
// inside a JSON field and a multi-line git diagnostic would bloat the result.
func gitBlameFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
