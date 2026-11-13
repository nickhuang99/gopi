package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// git_blame — a strong-contract wrapper around `git blame` (GitTraceSkill
// atomic tool, doubao design 2026-08: devdoc/gitblame-motivate-gopi.md
// records the LLM-adoption evidence the tool was accepted on).
//
// Why a dedicated tool instead of a bash subcommand (design doc §判断):
// raw blame output is noisy (LLM pays tokens parsing it), the LLM passes
// wrong line numbers / paths, a whole-file blame on a big file explodes
// tokens, and raw git stderr derails the model. The tool instead validates,
// clips, parses and returns structured JSON — raw git output never reaches
// the LLM. It is an OPTIONAL tool: no git binary → it reports itself
// unavailable and the agent loop continues.

// gitBlameDefaultSpan is the default blame window (lines) when the model
// omits start_line/end_line. The tool FORCES a bounded range — a whole-file
// blame is never issued (design contract risk 1: token blast protection).
const gitBlameDefaultSpan = 50

// gitBlameMaxSpan caps a single git_blame call's line window. Wider ranges
// are refused with a structured error, however the model asked.
const gitBlameMaxSpan = 200

// ---------------------------------------------------------------------------
// Ledger enrichment (epic gitblame-task-enrichment)
//
// WHY: `author` carries no information for an agent — every run commits as
// `gopi <gopi@gopi>`, so "who wrote this line" is a constant. What DOES carry
// information is which TASK the change belongs to and what that task was
// trying to do, because that is what lets a later run decide whether its change
// conflicts with the intent behind the line (让步 or not).
//
// The chain: the bash tool's commit seam appends `task: <row id>` (and
// `host: <hostname>`) trailers to every commit a dispatched run makes
// (internal/tools/git_commit_task_trailer.go), so the TRAILER on the blamed
// commit is the join key into the ledger.
//
// THREE FALLBACK LAYERS, none of which may fail the tool (a repo with no
// trailers, a trailer naming a row this ledger does not hold, and a machine
// with no ledger at all are all ORDINARY — git_blame must work in any git
// repository):
//
//	① commit has no `task:` trailer   → task_* empty, blame unchanged
//	② trailer present, row absent      → task_id set, the rest empty
//	③ no ledger / unreadable ledger    → blame unchanged, task_* empty
//
// `task_enrichment` says WHICH of those happened, because an empty field with
// no stated reason reads as "that commit belongs to no task" when the truth may
// be "it belongs to a task this ledger cannot see" — two facts that lead to
// different decisions.
// ---------------------------------------------------------------------------

const (
	// gitBlameTaskTrailerKey / gitBlameHostTrailerKey are the two trailer axes
	// the commit seam writes (Rule 1: the values live once, here).
	gitBlameTaskTrailerKey = "task"
	gitBlameHostTrailerKey = "host"

	// gitBlameTrailerFieldSep separates the three fields of one log record;
	// gitBlameTrailerValueSep separates repeated values of ONE trailer key
	// inside that field. Both are non-printing bytes so no commit message or
	// trailer VALUE can forge a field boundary (git forbids NUL in a body; a
	// control character survives only as an escape in the pretty format).
	gitBlameTrailerFieldSep = "\x1f"
	gitBlameTrailerValueSep = "\x1e"

	// gitBlameLogRecordSep is `git log -z`'s record separator (NUL). A commit
	// body cannot contain NUL, so it is the one unambiguous record boundary.
	gitBlameLogRecordSep = "\x00"

	// gitBlameTrailerTimeout bounds the ONE `git log` call that resolves every
	// distinct commit's trailers (Rule 2: one place reads the deadline).
	gitBlameTrailerTimeout = 10 * time.Second

	// gitBlameLedgerTimeout bounds the ONE read-only ledger open plus its
	// point lookups. A blame answer must not hang behind a busy ledger.
	gitBlameLedgerTimeout = 10 * time.Second

	// The three values of the top-level `task_enrichment` field.
	gitBlameEnrichmentNone    = "none"
	gitBlameEnrichmentPartial = "partial"
	gitBlameEnrichmentFull    = "full"

	// gitBlameUnresolvedNoteCap bounds how many unresolved task ids the note
	// spells out — enough to identify the gap, never an unbounded echo of a
	// whole window's ids.
	gitBlameUnresolvedNoteCap = 3
)

// GitBlameLedgerAccess is the DE-DUPLICATION measurement, reported in-band so a
// caller can check the tool did not query the ledger once per blamed LINE: a
// 200-line window drawn from 3 commits must read 1 open + 3 lookups, and a
// window with no trailers must read 0 opens.
type GitBlameLedgerAccess struct {
	// Commits is the number of DISTINCT commit hashes in the window.
	Commits int `json:"commits"`
	// TrailerLookups is the number of DISTINCT hashes asked for trailers (one
	// `git log --no-walk --stdin` call covers all of them).
	TrailerLookups int `json:"trailer_lookups"`
	// LedgerOpens / LedgerQueries count the read-only ledger open and the
	// point lookups that followed it.
	LedgerOpens   int `json:"ledger_opens"`
	LedgerQueries int `json:"ledger_queries"`
	// Warning carries a non-fatal degradation (Rule 15: a fallback that fired
	// is stated, never silent). Empty means every step succeeded.
	Warning string `json:"warning,omitempty"`
}

// GitBlameLineItem is one blamed line, structured for the LLM.
type GitBlameLineItem struct {
	LineNo        int    `json:"line_no"`
	CommitHash    string `json:"commit_hash"`
	Author        string `json:"author"`
	Timestamp     string `json:"timestamp"`
	CommitMessage string `json:"commit_message"`
	CodeLine      string `json:"code_line"`

	// The ledger join, resolved from the commit's `task:` trailer. All four are
	// empty when the commit carries no trailer (fallback layer ①); task_id
	// alone is set when the trailer names a row this ledger does not hold
	// (layer ②). task_summary is passed through sanitizeTaskSummary — the same
	// guard the ledger's own render paths use, so a tool result can never leak
	// text the ledger would have escaped.
	TaskID      string `json:"task_id,omitempty"`
	TaskSummary string `json:"task_summary,omitempty"`
	TaskStatus  string `json:"task_status,omitempty"`
	TaskHost    string `json:"task_host,omitempty"`
}

// GitBlameResult is the successful structured response.
type GitBlameResult struct {
	FilePath  string             `json:"file_path"`
	LineItems []GitBlameLineItem `json:"line_items"`

	// TaskEnrichment: none | partial | full (see the block comment above).
	TaskEnrichment string `json:"task_enrichment"`
	// TaskEnrichmentNote is one sentence saying WHY it is partial or none — the
	// machine-readable enum alone cannot distinguish "no trailers here" from
	// "the ledger does not have these rows".
	TaskEnrichmentNote string               `json:"task_enrichment_note"`
	LedgerAccess       GitBlameLedgerAccess `json:"ledger_access"`
}

// GitBlameError is the structured failure response (no git binary, not a git
// repo, file missing/untracked, path escape, bad or too-wide range). Raw git
// stderr is never surfaced.
type GitBlameError struct {
	Error string `json:"error"`
}

// GitBlame resolves the git repo of the current working directory and
// returns the blame result for filePath's line range as a JSON string —
// either GitBlameResult (success) or GitBlameError (structured failure).
//
// Contract (design doc):
//   - filePath is repo-root-relative ("internal/memory/store.go"); absolute
//     paths, ".." escapes and symlink-outs are rejected.
//   - start_line/end_line are 1-based, validated (end >= start), clipped to
//     the file's actual line count; a missing range defaults to a bounded
//     window; ranges wider than gitBlameMaxSpan are refused.
//   - no git binary → tool reports itself unavailable (optional tool).
func GitBlame(filePath string, startLine, endLine int) string {
	if _, err := exec.LookPath("git"); err != nil {
		return mustJSON(GitBlameError{Error: "当前环境无 git 二进制，git_blame 不可用（optional tool，不影响其他工具）"})
	}
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return mustJSON(GitBlameError{Error: err.Error()})
	}
	return gitBlameAt(repoRoot, filePath, startLine, endLine)
}

// gitBlameAt is GitBlame's testable core: repoRoot is resolved (real
// execution uses the cwd's repo; tests inject a temp repo).
func gitBlameAt(repoRoot, filePath string, startLine, endLine int) string {
	// Path escape protection: lexical containment first, then symlink
	// resolution — a symlink inside the repo pointing outside must not read
	// outside the repo (design risk 2).
	full, err := resolveInRepo(repoRoot, filePath)
	if err != nil {
		return mustJSON(GitBlameError{Error: err.Error()})
	}
	info, err := os.Stat(full)
	if err != nil {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("file not found: %s", filePath)})
	}
	if info.IsDir() {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("not a file: %s", filePath)})
	}
	totalLines, err := blameFileLineCount(full)
	if err != nil {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("file read error: %v", err)})
	}

	// Line validation (design doc §内部行为): 1-based; an omitted range
	// (start/end < 1) defaults to a bounded window (gitBlameDefaultSpan);
	// end must be >= start; both clip to the file's actual line count.
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		// Omitted or inverted end_line → default window from startLine.
		endLine = startLine + gitBlameDefaultSpan - 1
	}
	if startLine > totalLines {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("line range %d-%d out of bounds: %s has %d lines", startLine, endLine, filePath, totalLines)})
	}
	if endLine > totalLines {
		endLine = totalLines
	}
	// Force a bounded range (design risk 1): refuse anything wider than the
	// cap even when the model explicitly asked — token blast protection.
	if endLine-startLine+1 > gitBlameMaxSpan {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("line range %d-%d too wide (max %d lines per call); narrow the range", startLine, endLine, gitBlameMaxSpan)})
	}

	// Git-tracked check (design doc §异常兜底): exit code only, stderr
	// suppressed — raw git output stays hidden.
	tracked := exec.Command("git", "-C", repoRoot, "ls-files", "--error-unmatch", "--", filePath)
	tracked.Stderr = nil
	if err := tracked.Run(); err != nil {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("file not tracked by git: %s", filePath)})
	}

	// git blame -L start,end --porcelain — bounded range, machine format.
	// Raw output never leaves this function.
	cmd := exec.Command("git", "-C", repoRoot, "blame", "-L", fmt.Sprintf("%d,%d", startLine, endLine), "--porcelain", "--", filePath)
	out, err := cmd.Output()
	if err != nil {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("git blame failed for %s lines %d-%d", filePath, startLine, endLine)})
	}
	items, err := parseGitBlamePorcelain(string(out))
	if err != nil {
		return mustJSON(GitBlameError{Error: fmt.Sprintf("git blame parse error: %v", err)})
	}

	// Best-effort memory linkage (design doc §记忆联动): commit metadata
	// lands in the knowledge base so a future memory_search can match
	// commit messages against past lessons. Best-effort on purpose — the
	// memory store has its own garbage/dedup gates, and a failure here must
	// never fail the tool itself (blame is the deliverable, memory a
	// byproduct).
	// Whether this host HAS a ledger is sampled BEFORE the memory linkage
	// below, because that linkage is best-effort and MemoryStore may CREATE the
	// file: "was there a ledger to consult" must describe the state at the
	// moment this read looked, not the state its own side effect left behind.
	ledgerPresent := fileExists(memoryDBPath())

	storeBlameMemory(filePath, startLine, endLine, items)

	// Ledger enrichment (epic gitblame-task-enrichment): join each distinct
	// commit's `task:` trailer into the task ledger. Never fatal — the blame
	// answer is the deliverable, the attribution a byproduct with an honest
	// `task_enrichment` verdict.
	res, _ := gitBlameEnrich(repoRoot, filePath, items, ledgerPresent)

	return mustJSON(res)
}

// gitRepoRoot returns the absolute repo root of the current working
// directory via `git rev-parse --show-toplevel`.
func gitRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not inside a git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveInRepo joins a repo-root-relative path onto repoRoot and verifies
// the result stays inside repoRoot: absolute paths, ".." escapes and
// symlink-outs are rejected (design risk 2 — 路径逃逸防护).
func resolveInRepo(repoRoot, filePath string) (string, error) {
	clean := filepath.Clean(filePath)
	if clean == "." || clean == "" || clean == string(filepath.Separator) {
		return "", fmt.Errorf("invalid file_path %q: expected a repo-relative path like internal/memory/store.go", filePath)
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("file_path must be repo-relative, got absolute path %q", filePath)
	}
	full := filepath.Join(repoRoot, clean)
	rel, err := filepath.Rel(repoRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file_path escapes the git repository: %q", filePath)
	}
	// Symlink resolution: a link inside the repo pointing outside must not
	// be followed for reads. A missing file is fine here (the caller
	// reports "file not found").
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		if os.IsNotExist(err) {
			return full, nil
		}
		return "", fmt.Errorf("file_path resolution error: %w", err)
	}
	rel2, err := filepath.Rel(repoRoot, resolved)
	if err != nil || rel2 == ".." || strings.HasPrefix(rel2, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file_path escapes the git repository via symlink: %q", filePath)
	}
	return full, nil
}

// blameFileLineCount returns the number of lines in a file (newline-aware:
// a file without a trailing newline still counts its final line).
func blameFileLineCount(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n := bytes.Count(data, []byte("\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n, nil
}

// parseGitBlamePorcelain parses `git blame --porcelain` output into
// structured line items. The porcelain format is stable and machine
// readable: each commit block is headed by "<40-hex-sha> <orig-line>
// <final-line> <count>", followed by header fields (author / author-time /
// summary / ...), then "	"-prefixed code lines — one per blamed line.
// After the first line of a run, git RE-EMITS a shortened header
// ("<sha> <orig> <final>", no count, no header fields) before each
// subsequent line — that incremental header must NOT reset the author /
// timestamp / summary state, only a genuinely different commit hash does.
// Line numbers are the FINAL file line numbers (what read/error output
// shows the model).
func parseGitBlamePorcelain(out string) ([]GitBlameLineItem, error) {
	var items []GitBlameLineItem
	var curHash, curAuthor, curTime, curSummary string
	nextFinal := 0
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
			// skip blank separator lines
		case strings.HasPrefix(line, "	"):
			items = append(items, GitBlameLineItem{
				LineNo:        nextFinal,
				CommitHash:    curHash,
				Author:        curAuthor,
				Timestamp:     curTime,
				CommitMessage: curSummary,
				CodeLine:      strings.TrimPrefix(line, "	"),
			})
			nextFinal++
		case isBlameHashHeader(line):
			fields := strings.Fields(line)
			if n, err := strconv.Atoi(fields[2]); err == nil {
				nextFinal = n
			}
			// Only a NEW commit block resets the header state; a repeated
			// header for the same hash is git's per-line incremental
			// re-emission and carries no fields.
			if fields[0] != curHash {
				curHash = fields[0]
				curAuthor, curTime, curSummary = "", "", ""
			}
		case strings.HasPrefix(line, "author "):
			curAuthor = strings.TrimSpace(strings.TrimPrefix(line, "author "))
		case strings.HasPrefix(line, "author-time "):
			if ts, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "author-time ")), 10, 64); err == nil {
				curTime = time.Unix(ts, 0).UTC().Format(time.RFC3339)
			}
		case strings.HasPrefix(line, "summary "):
			curSummary = strings.TrimSpace(strings.TrimPrefix(line, "summary "))
		}
		// All other header fields (author-mail, committer*, previous,
		// filename, boundary) are ignored — not part of the contract.
	}
	return items, nil
}

// isBlameHashHeader reports whether line is a porcelain commit-block header:
// a 40-hex sha followed by a space (boundary commits are 40 zeros).
func isBlameHashHeader(line string) bool {
	if len(line) < 41 || line[40] != ' ' {
		return false
	}
	for i := 0; i < 40; i++ {
		c := line[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// storeBlameMemory persists the blame result's commit metadata into the
// knowledge base (best-effort, design doc §记忆联动): memory records carry
// version context so a future memory_search can match commit messages
// against past lessons. Failures are swallowed — the memory store's
// garbage gate may reject, the DB may be absent, none of it affects the
// tool's own result.
func storeBlameMemory(filePath string, startLine, endLine int, items []GitBlameLineItem) {
	if len(items) == 0 {
		return
	}
	// Unique commits in file order, with their messages.
	var order []string
	msgs := make(map[string]string, len(items))
	for _, it := range items {
		if _, seen := msgs[it.CommitHash]; !seen {
			msgs[it.CommitHash] = it.CommitMessage
			order = append(order, it.CommitHash)
		}
	}
	var sb strings.Builder
	for i, h := range order {
		if i > 0 {
			sb.WriteString("; ")
		}
		if len(h) > 8 {
			h = h[:8]
		}
		sb.WriteString(h)
		if m := msgs[h]; m != "" {
			sb.WriteString(" " + m)
		}
	}
	question := fmt.Sprintf("git blame %s lines %d-%d — which commits last touched these lines", filePath, startLine, endLine)
	answer := fmt.Sprintf("%d lines blamed across %d commits: %s", len(items), len(order), sb.String())
	_, _ = MemoryStore(
		fmt.Sprintf("git blame %s", filePath),
		question,
		answer,
		"git_blame tool",
		[]string{"git blame", "commit history", filePath, "git"},
	) //stylegate:ignore the blame question is cached; a cache failure must not fail the blame answer
}

// mustJSON marshals a result/error struct. These are plain structs whose
// marshaling cannot fail; the "" fallback keeps the tool total (returns a
// structured error rather than panicking).
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"git_blame internal error"}`
	}
	return string(b)
}
