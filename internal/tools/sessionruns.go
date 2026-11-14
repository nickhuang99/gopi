package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickhuang99/gopi/internal/procstat"
	"github.com/nickhuang99/gopi/internal/rundir"
)

// sessionruns.go — THE RUN DIRECTORY AS A SESSION SOURCE.
//
// THE DEFECT THIS CLOSES. `gopi session list` read exactly ONE table: `chats`,
// which holds conversations (gopi chat, gopi web). A TASK RUN writes a session
// log too — every `gopi worker`, `gopi task` or one-shot prompt does — but it
// registers no chats row, so the index could not see it. Measured on this host
// 2026-10-28: the table answered 33 rows while the sessions tree held 2980
// directories and 1817 run logs. The operator's own debugging question ("what
// is this task doing right now, and where?") had no door at all: the run's pid,
// workdir, log and raw-log paths were on disk and nothing printed them.
//
// WHAT THIS ADDS, AND WHAT IT DOES NOT. It is a second SOURCE for the same
// listing, not a second listing: the kinds are named together (SessionKind*),
// filtered together and rendered by the same faces, so "all sessions" is one
// question with one answer. The chats table's semantics are untouched — its
// rows, its WHERE clause and its LocalOnly presence rule are exactly what they
// were; this file only ADDS the rows the table never held.
//
// THE ORDERING IS THE UNION'S OWN. A chat row's clock is chats.started_at; a
// run's is its session_start (falling back to the log's mtime). Both are unix
// seconds, so one sort orders the merged view — and a merged view that sorted
// each source separately and concatenated them would not be a listing.

const (
	// SessionKindChat is a conversation that registered a chats row.
	SessionKindChat = "chat"
	// SessionKindRun is a RUN that writes a session log without a chats row:
	// a task run, a one-shot prompt, a resumed session. Its identity, its
	// liveness stamp and its paths come from the log's own session_start.
	SessionKindRun = "run"
)

// SessionKinds is the `--kind` vocabulary, in DISPLAY order. It is the ONE
// definition (Rule 13/14): the flag's accepted values, the usage page's list
// and the filter's error message all read it.
var SessionKinds = []string{SessionKindChat, SessionKindRun}

// SessionSource is one row of the MERGED session index: a chats row or a run
// log, described in the fields both can answer. It is not a replacement for
// ChatSessionDetail (which carries the conversation-only columns: turns, the
// person-only subset, token totals); it is the shared HEAD the listing sorts
// and filters, and each kind's own detail is reached from its id.
type SessionSource struct {
	// Kind is SessionKindChat or SessionKindRun.
	Kind string
	// SessionID is the id the row is addressed by: the chats row's session_id,
	// or the run log's session_start sessionId (falling back to the run id).
	SessionID string
	// Mode is the chat's recorded mode ("chat"/"web") for a chat row; for a
	// run it is SessionModeRun — the run is neither, and saying "chat" would be
	// a lie and saying "" would look like an unrecorded chat.
	Mode string
	// Source is the recorded provenance (tasksource vocabulary) where the row
	// has one, "" for a run (whose provenance is the ledger's, not the log's).
	Source string
	// Model is the model the row or the run recorded ("" when it recorded none).
	Model string
	// Head is the human one-liner: the chat's title/first message, or the run
	// directory's name for a run (a run log carries no prompt summary).
	Head string
	// Started is the row's or the run's beginning, unix seconds (0 when
	// nothing recorded it — the merged ordering then falls back to LastWrite).
	Started float64
	// LastWrite is the run log's mtime, or the chat row's last message time.
	LastWrite float64
	// State is the lifecycle word (chat: tools.ChatStateRunning/ended; run:
	// SessionRunState*), from ONE derivation per kind so a kind cannot carry
	// two vocabulary words.
	State string
	// Workdir / LogPath are the run's own recorded facts (the chat row carries
	// them in its columns). For a run the workdir comes from session_start and
	// the log path IS the record file.
	Workdir string
	LogPath string
	// GitBranch / GitRepo are what git says about Workdir, when it could be
	// read (SessionRunGitFacts). Empty for chat rows: their branch is a
	// ledger column, not a measurement of the directory.
	GitBranch string
	GitRepo   string
	// RawLogPath is the request log beside the session log ("" when the run
	// recorded none — a fact, not a gap).
	RawLogPath string
	// PID is the liveness STAMP from session_start (0 when none was recorded).
	// It is deliberately not a verdict: Alive reports what the probe measured.
	PID int
	// Alive is the /proc verdict for PID, in procstat's three-valued
	// vocabulary — VerdictUnknown when nothing was stamped (Rule 15: an absent
	// stamp is missing evidence, never evidence of death).
	Alive procstat.Verdict
	// TaskID is the ledger row this run executed for ("" for a chat or a run
	// dispatched for nobody).
	TaskID string
	// RunID is the run's own id (rundir's spelling: the record's base name, or
	// the directory's name for the bare layout). "" for a chat row.
	RunID string
	// EndReason is the recorded ending (chat rows), "" for a run whose log
	// carries no session_end.
	EndReason string

	// chat is the chat INDEX row this source was built from, and hasChat says
	// whether there is one. They exist so the merged listing can render a chat
	// row in the chat index's OWN line (chatListRowLine) instead of a second
	// format for the same row — the facts a chat row carries that a run row has
	// no column for (turns, tokens, the presence marker) live there.
	chat    ChatSessionDetail
	hasChat bool
}

// ChatDetail returns the chat INDEX row this source was built from, and whether
// there is one. It is the accessor a consumer outside this package needs to
// render a chat row in the chat index's own shape (the fields a run log has no
// column for — turns, tokens, the presence marker — live only on that row).
func (s SessionSource) ChatDetail() (ChatSessionDetail, bool) { return s.chat, s.hasChat }

// SessionModeRun is the mode a RUN row carries. It lives with the kind constants
// because it is the same vocabulary question ("which species of session is
// this?"), and a caller filtering `--mode run` gets exactly the run rows.
const SessionModeRun = "run"

// SessionRunStates are the lifecycle words a run row can carry. They are NOT
// the chat words: a run never "ends" by a user quitting, and the fact the
// operator needs ("is anything still writing this?") is the liveness probe's.
const (
	// SessionRunStateRunning — a live process holds the run's stamp.
	SessionRunStateRunning = "running"
	// SessionRunStateEnded — the log carries a session_end verdict.
	SessionRunStateEnded = "ended"
	// SessionRunStateStopped — no session_end, and the stamped process is
	// gone: the run died without writing its verdict (a kill, a panic, a
	// power loss). Distinct from "ended" on purpose — a reader debugging an
	// interrupted run must not see it called finished.
	SessionRunStateStopped = "stopped"
	// SessionRunStateUnstamped — no session_end and no pid stamp: the run
	// predates the stamp, or its start line could not be read. Says WHY it
	// cannot be decided instead of picking "ended" or "stopped" (Rule 15).
	SessionRunStateUnstamped = "unstamped"
	// SessionRunStateLiveUnknown — a stamp exists but the probe could not
	// answer (permission, another namespace): neither running nor stopped is
	// established, and the row says so.
	SessionRunStateLiveUnknown = "live-unknown"
)

// sessionRunState decides a run row's word from the FACTS the record carries,
// and it is the ONE derivation (the table, the JSON face and `session show` all
// read it — Rule 13).
func sessionRunState(rec rundir.Record, alive procstat.Verdict) string {
	if alive == procstat.VerdictAlive {
		// A live writer outranks a recorded end: a resumed session appends a
		// second segment to the SAME log, so the previous segment's verdict is
		// on disk while the run is very much going.
		return SessionRunStateRunning
	}
	if rec.HasEnd {
		return SessionRunStateEnded
	}
	switch alive {
	case procstat.VerdictGone:
		return SessionRunStateStopped
	case procstat.VerdictUnknown:
		if rec.WriterPID <= 0 {
			return SessionRunStateUnstamped
		}
		return SessionRunStateLiveUnknown
	}
	return SessionRunStateUnstamped
}

// SessionSourcePage is what one merged listing MEASURED: the rows that matched,
// and — as numbers, never as silence — everything the view did NOT show. The
// four counts are separate because they have four different causes and four
// different remedies (Rule 15): a kind you filtered out, a chat row whose log is
// not on this machine, a directory that holds no run log, and the tail cut by
// --limit.
type SessionSourcePage struct {
	Rows []SessionSource
	// HiddenKind is how many rows the --kind filter dropped.
	HiddenKind int
	// HiddenPresence is how many chat rows the LOCAL-ONLY rule dropped (their
	// session log is not on this machine — the chats index records where a
	// conversation WAS).
	HiddenPresence int
	// RecordlessDirs is how many session directories held no run log at all.
	// They are not "hidden rows": they are directories with nothing to list,
	// and a reader who wonders why 2980 directories produced 1833 rows needs
	// this number to close the arithmetic.
	RecordlessDirs int
	// Dirs is how many directories the run scan entered.
	Dirs int
	// Truncated is how many matched rows the limit cut off the END of the
	// ordered view.
	Truncated int
	// Total is how many rows matched before the limit.
	Total int
	// ChatError is the chats-half read failure, when there was one. The RUN
	// half is still listed (a broken table must not hide the run logs), and the
	// caller PRINTS this rather than dropping it — a listing that silently
	// showed only runs would be the exact over-claim this field prevents.
	ChatError error
}

// SessionSourceFilter is the merged listing's filter. Every field is the
// caller's own typing; the SAME value narrows both kinds, so the two faces and
// the two sources cannot drift (Rule 13).
type SessionSourceFilter struct {
	// Chat is the chats-table half of the filter. Its Mode/Source/LocalOnly
	// semantics are unchanged (this file does not re-decide them); Kind and
	// Limit are the merged view's and are applied AFTER the merge.
	Chat ChatListFilter
	// Kind narrows to one species (SessionKinds). Empty means both; anything
	// else is a NAMED error (a typo'd filter that answers an empty list looks
	// exactly like an empty machine).
	Kind string
	// Mode narrows by the mode column of whichever kind: a chat's chat/web, or
	// SessionModeRun for a run. Empty means every mode. An unknown spelling is
	// a NAMED error, and the accepted set is the union of the two kinds' own
	// vocabularies.
	Mode string
	// Limit ≤ 0 means sessionSourceDefaultLimit.
	Limit int
}

const (
	// sessionSourceDefaultLimit is the merged listing's page size. It is the
	// chat index's cap rather than "everything": a task run's log list is
	// thousands of rows, and a default that dumps them is not a listing.
	sessionSourceDefaultLimit = chatListDefaultLimit
	// sessionSourceChatFetchCap bounds the chats half of the fetch. The chats
	// half cannot be cut before the merge (its rows interleave with the runs by
	// time), so it is fetched whole up to this cap — far above the measured 33
	// rows of a live profile, and a cap rather than "unlimited" so a runaway
	// table cannot turn one listing into an unbounded read.
	sessionSourceChatFetchCap = 1000
)

// sessionSourceValidMode reports whether m is a mode either species can carry.
func sessionSourceValidMode(m string) bool {
	return validChatMode(m) || m == SessionModeRun
}

// SessionSourceList answers the merged listing.
//
// IT IS A READ, and its two halves are read differently on purpose: the chats
// rows come from the ledger table (the ONLY place a conversation is indexed),
// and the run rows come from the sessions TREE (the ONLY place a run exists). A
// run has no index to read — that is the whole reason it was invisible — so the
// tree scan is not a fallback, it is the source.
func SessionSourceList(f SessionSourceFilter) (SessionSourcePage, error) {
	if k := strings.TrimSpace(f.Kind); k != "" && !sessionSourceKnownKind(k) {
		return SessionSourcePage{}, fmt.Errorf("session: list: invalid kind %q (want %s)", k, strings.Join(SessionKinds, "|"))
	}
	if m := strings.TrimSpace(f.Mode); m != "" && !sessionSourceValidMode(m) {
		return SessionSourcePage{}, fmt.Errorf("session: list: invalid mode %q (want %s|%s|%s)",
			m, ChatModeCLI, ChatModeWeb, SessionModeRun)
	}
	page := SessionSourcePage{}

	// THE CHATS HALF. Its own filter decides which rows exist (mode/source are
	// the table's columns); the merged view adds only the kind/mode narrowing,
	// applied below. A chats read failure is NOT fatal to the listing — a
	// profile whose ledger is unreadable still holds its run logs, and dropping
	// them because a table failed would hide the half that works. It is
	// recorded instead (page.HiddenPresence is not the place for that — the
	// error is returned with the page so the caller can print it).
	chatRows, hiddenPresence, chatErr := chatSessionRowsForSources(f.Chat)
	page.HiddenPresence = hiddenPresence
	for _, d := range chatRows {
		page.Rows = append(page.Rows, sessionSourceFromChat(d))
	}

	// THE RUNS HALF.
	root, err := runSessionsRoot()
	if err != nil {
		// The tree cannot be resolved: that is a fact about the scan, and it is
		// returned rather than rendering an empty run set as "no runs".
		return page, fmt.Errorf("session: list: runs: %w", err)
	}
	tree := rundir.HeadRecordsTree(root)
	page.Dirs = tree.Dirs
	page.RecordlessDirs = tree.RecordlessDirs
	for _, rec := range tree.Records {
		page.Rows = append(page.Rows, sessionSourceFromRun(rec))
	}

	// THE NARROWING IS THE MERGED VIEW'S, applied to both kinds with the same
	// predicate — so `--mode run` and `--kind run` select the same rows, and a
	// chat cannot survive a `--mode run` (its mode is chat/web by the schema).
	kind := strings.TrimSpace(f.Kind)
	mode := strings.TrimSpace(f.Mode)
	kept := page.Rows[:0:0]
	for _, r := range page.Rows {
		if kind != "" && r.Kind != kind {
			page.HiddenKind++
			continue
		}
		if mode != "" && r.Mode != mode {
			page.HiddenKind++
			continue
		}
		kept = append(kept, r)
	}
	page.Rows = kept
	page.Total = len(kept)
	page.ChatError = chatErr

	// ONE ORDER for the union: newest first, on the clock each row has.
	sort.SliceStable(page.Rows, func(i, j int) bool {
		ki, kj := page.Rows[i].orderKey(), page.Rows[j].orderKey()
		if ki != kj {
			return ki > kj
		}
		return page.Rows[i].SessionID < page.Rows[j].SessionID
	})

	limit := f.Limit
	if limit <= 0 {
		limit = sessionSourceDefaultLimit
	}
	if len(page.Rows) > limit {
		page.Truncated = len(page.Rows) - limit
		page.Rows = page.Rows[:limit]
	}
	return page, nil
}

// chatSessionRowsForSources is the chats HALF of the merged listing: the same
// filter, the same query, the same presence rule the chat index itself uses
// (ChatSessionPage — never a second SQL), with its page size raised to the
// fetch cap because the merge happens AFTER the read. The cut has to happen at
// the merge: a limit applied to each half would drop the newest runs of one
// kind in favour of the other's oldest rows, which is not an ordering.
//
// A read failure is returned WITH the empty slice rather than aborting the
// listing: the caller reports it and still renders the run half (a broken chats
// table must not hide run logs that were read successfully).
func chatSessionRowsForSources(f ChatListFilter) (rows []ChatSessionDetail, hidden int, err error) {
	f.Limit = sessionSourceChatFetchCap
	rows, hidden, err = ChatSessionPage(f)
	if err != nil {
		return nil, 0, err
	}
	return rows, hidden, nil
}

// sessionSourceKnownKind reports whether k is one of SessionKinds.
func sessionSourceKnownKind(k string) bool {
	for _, known := range SessionKinds {
		if k == known {
			return true
		}
	}
	return false
}

// orderKey is the row's place on the merged timeline: its start when something
// recorded one, else its last activity. It is a float64 unix second on BOTH
// kinds, which is what makes one sort possible.
func (s SessionSource) orderKey() float64 {
	if s.Started > 0 {
		return s.Started
	}
	return s.LastWrite
}

// sessionSourceFromChat adapts a chats row to the merged head. The conversion is
// mechanical and total: every field it can answer is filled, and the ones it
// cannot answer stay empty (a chat row has no pid stamp and no live probe —
// its process is not recorded anywhere).
func sessionSourceFromChat(d ChatSessionDetail) SessionSource {
	head := d.Title
	if head == "" {
		head = d.FirstMsg
	}
	if head == "" {
		head = chatNoTextWord
	}
	return SessionSource{
		Kind:      SessionKindChat,
		SessionID: d.SessionID,
		Mode:      d.Mode,
		Source:    d.Source,
		Model:     d.Model,
		Head:      head,
		Started:   d.StartedAt,
		LastWrite: d.LastMsgAt,
		State:     chatStateWord(d),
		Workdir:   d.Workdir,
		LogPath:   d.LogPath,
		EndReason: d.EndReason,
		chat:      d,
		hasChat:   true,
	}
}

// chatStateWord is the chats kind's ONE lifecycle derivation, reused by the
// table renderer (sessionStateWord) so the merged view and the chat index cannot
// describe one row in two words.
func chatStateWord(d ChatSessionDetail) string {
	state, _ := sessionStateWord(d.EndedAt, d.EndReason)
	return state
}

// sessionSourceFromRun adapts one run record to the merged head, probing the
// stamp it carries. The probe is the ONLY place a liveness claim is produced, and
// it produces procstat's three values — never a boolean.
func sessionSourceFromRun(rec rundir.Record) SessionSource {
	alive := procstat.Alive(rec.WriterPID)
	branch, repo := SessionRunGitFacts(rec.Workdir)
	started := float64(0)
	if rec.HasStart {
		started = float64(rec.Started.Unix())
	}
	last := float64(0)
	if !rec.LastWrite.IsZero() {
		last = float64(rec.LastWrite.Unix())
	}
	id := rec.SessionID
	if id == "" {
		id = rec.RunID
	}
	return SessionSource{
		Kind:       SessionKindRun,
		SessionID:  id,
		Mode:       SessionModeRun,
		Model:      rec.Model,
		Head:       filepath.Base(rec.Dir),
		Started:    started,
		LastWrite:  last,
		State:      sessionRunState(rec, alive),
		Workdir:    rec.Workdir,
		LogPath:    rec.Path,
		GitBranch:  branch,
		GitRepo:    repo,
		RawLogPath: rec.RawLogPath,
		PID:        rec.WriterPID,
		Alive:      alive,
		TaskID:     rec.TaskID,
		RunID:      rec.RunID,
	}
}

// runSessionsRoot is the sessions tree the run scan reads. It delegates to the
// ONE resolver (chatSessionsDir → config.SessionsRoot, which honours
// $GOPI_SESSIONS_ROOT) so a relocated tree is scanned where it was written —
// the same resolution the chat half's presence probe uses, so the two halves
// cannot disagree about which tree this machine has.
func runSessionsRoot() (string, error) {
	return chatSessionsDir()
}

// SessionRunGitFacts answers what git says about a run's directory: the branch
// checked out there and the repository it belongs to. Both are READ from disk —
// `.git/HEAD` and the `gitdir:` pointer — never by running git, because a
// listing may visit thousands of directories and a subprocess per row is not a
// listing.
//
// Every failure is reported as the pair ("", "") rather than an error: a workdir
// that was swept, was never a repository, or whose HEAD is unreadable is a
// COMMON case (a run's ground is reclaimed after the task ends), and a listing
// must not refuse to render because one row's directory is gone. The show page
// states the absence in words (Rule 15), which is where a reader can act on it.
func SessionRunGitFacts(workdir string) (branch, repo string) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return "", ""
	}
	gitEntry := filepath.Join(workdir, sessionGitEntryName)
	fi, err := os.Stat(gitEntry)
	if err != nil {
		return "", ""
	}
	if fi.IsDir() {
		// CLONE model: the directory holds its own repository.
		return readGitHeadBranch(filepath.Join(gitEntry, sessionGitHeadName)), workdir
	}
	// LINKED-WORKTREE model: `.git` is a FILE naming a gitdir under another
	// repository. The branch lives in that gitdir's HEAD, and the repository is
	// the checkout that owns it.
	gitdir, ok := readGitdirPointer(gitEntry, workdir)
	if !ok {
		return "", ""
	}
	return readGitHeadBranch(filepath.Join(gitdir, sessionGitHeadName)), linkedRepoOf(gitdir)
}

// readGitHeadBranch reads a HEAD file and answers the branch it names. A
// detached HEAD holds a bare object id, which is NOT a branch: it renders as the
// empty string (the caller states the absence) rather than as the first 40
// characters of a hash a reader would mistake for a branch name.
func readGitHeadBranch(headPath string) string {
	body, err := os.ReadFile(headPath)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(body))
	ref, ok := strings.CutPrefix(line, sessionGitRefPrefix)
	if !ok {
		return ""
	}
	branch, ok := strings.CutPrefix(ref, sessionGitHeadsPrefix)
	if !ok {
		return ""
	}
	return strings.TrimSpace(branch)
}

// readGitdirPointer reads a linked worktree's `.git` file. A relative target is
// resolved against the worktree (git writes both shapes), and a file that is not
// a `gitdir:` line is refused by returning ok=false.
func readGitdirPointer(gitEntry, workdir string) (string, bool) {
	body, err := os.ReadFile(gitEntry)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		target, ok := strings.CutPrefix(line, sessionGitdirKey)
		if !ok {
			continue
		}
		target = strings.TrimSpace(target)
		if target == "" {
			return "", false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(workdir, target)
		}
		return target, true
	}
	return "", false
}

// linkedRepoOf answers the checkout that owns a linked worktree's gitdir. Git
// writes it as `<checkout>/.git/worktrees/<name>`, so the checkout is three
// levels up — and ONLY when that shape holds: anything else returns "" rather
// than a guessed path (a wrong "repo" is worse than an absent one).
func linkedRepoOf(gitdir string) string {
	parent := filepath.Dir(gitdir)
	if filepath.Base(parent) != sessionGitWorktreesDirName {
		return ""
	}
	dotGit := filepath.Dir(parent)
	if filepath.Base(dotGit) != sessionGitEntryName {
		return ""
	}
	return filepath.Dir(dotGit)
}

// The git vocabulary this file reads, spelled ONCE (Rule 1b/13). The names are
// git's own on-disk layout, not a convention invented here: `.git` (a directory
// for a clone, a `gitdir:` file for a linked worktree), the `HEAD` file inside
// it, the `ref: refs/heads/` line a checked-out branch writes, and the
// `worktrees/` directory git keeps a linked worktree's private gitdir under.
const (
	sessionGitEntryName        = ".git"
	sessionGitHeadName         = "HEAD"
	sessionGitRefPrefix        = "ref: "
	sessionGitHeadsPrefix      = "refs/heads/"
	sessionGitdirKey           = "gitdir:"
	sessionGitWorktreesDirName = "worktrees"
)
