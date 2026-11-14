package main

// chat_resume_single_session_e2e_test.go — the REAL-BINARY demonstration of the
// defect this epic exists for: `gopi chat --resume <id>` minted a SECOND session
// instead of continuing the one it was handed.
//
// THE MEASURED DEFECT (master 3430292a). The resume arm loaded the old
// conversation into the run's seed, read the old turn count as a DISPLAY
// baseline, and then built the agent like any other run — `NewAgent` minted a
// fresh uuid, so the resumed session got a fresh `sessions/<uuid>/` directory, a
// fresh `<uuid>.raw.log` opened O_EXCL and a fresh `session.jsonl`. The session
// the operator asked to continue never grew a byte, and every resume left one
// more stranded directory behind. Note that the turn baseline was already CORRECT
// (task 01a16fb3): the screen said turn N+1 while the log it named said N — two
// halves of one session, recorded in two places, and only one of them grew.
//
// WHY END TO END (Rule 31). The parts are provable in isolation (a constructor
// that takes an id, a raw logger that appends); what cannot be proved in
// isolation is that the ID THE OPERATOR TYPED reaches the mint through the
// shipped binary — the resolution that reads the session record, the agent
// constructor, and the two writers it owns. So this case runs the binary twice
// against a scripted mock provider (zero API calls), with a private profile, and
// measures the SESSION TREE afterwards: one directory, one raw log that GREW, one
// session record holding two session_start rows whose ids agree.
//
// THE FIRST RUN IS NOT PART OF THE SETUP — IT IS THE CONTROL. Its measurements
// (one directory, one creation header, N turns, size S) are asserted before the
// resume runs, so a failure afterwards names which fact moved.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/checkpoint"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// chatResumeE2EPrompt1 / …2 are the two operator turns: one per run, so "the
// second run appended a turn" is a fact about a prompt that only the second run
// could have carried.
const (
	chatResumeE2EPrompt1 = "first question of this session"
	chatResumeE2EPrompt2 = "second question, after the resume"
	chatResumeE2EReply1  = "the first answer"
	chatResumeE2EReply2  = "the second answer"
)

// chatResumeE2ERawRequestCount answers "how many request ROUNDS does this raw
// log hold" through the checkpoint layer's own counter rather than by counting a
// marker STRING in the file.
//
// IT USED TO COUNT `→ REQUEST` LITERALS (measured red, 2026-11-02, this run).
// A raw log has TWO framings and the writer now produces the LENGTH-PREFIXED one
// (`devdoc/rawlog-record-framing.md`): a framed record's header is `R 192474`,
// so a substring count of the legacy marker returns 0 on every log the current
// binary writes — and 0 is a confident wrong answer, not a failure (Rule 15).
// The defect surfaced here as "the creating run's raw log holds 0 request
// marker(s), want 1". checkpoint.CountRequests is the format-agnostic answer
// (it detects the framing and counts R/P records for a framed log), and it is
// the same function `gopi session checkpoint list` prints, so this case cannot
// disagree with the surface an operator reads.
//
// The bound is stated: CountRequests head-samples countScanBudgetBytes and a
// bigger file yields a LOWER BOUND. These fixtures hold a handful of KB, well
// inside the sample, so the count is exact here — and the assertion compares
// small integers (1 before, 2 after), which a lower bound could not fake upward.
func chatResumeE2ERawRequestCount(t *testing.T, rawPath string) int {
	t.Helper()
	n, truncated, err := checkpoint.CountRequests(rawPath)
	if err != nil {
		t.Fatalf("count the request rounds in %s: %v", rawPath, err)
	}
	if truncated {
		t.Fatalf("%s is larger than the counter's head sample, so its request count is a lower bound — this case asserts an exact number and the sample cannot give one", rawPath)
	}
	return n
}

// chatResumeE2EEnv is the sandboxed environment: the mock provider, a private
// profile and a private ledger, and the switches that keep a fixture run off the
// host (mirrors the sibling one-shot E2E cases in this package).
func chatResumeE2EEnv(t *testing.T, home, mockURL string) []string {
	t.Helper()
	return append(testutil.MockEnv(t, home, mockURL),
		"GOPI_MEMORY_DB="+filepath.Join(t.TempDir(), "memory.db"),
		"GOPI_SKIP_CLAIM_BROADCAST=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
	)
}

// chatResumeE2ESessionsRoot is the sessions tree a run under `home` writes into.
// It goes through config (the same two names the writers use: the profile
// directory and the sessions directory) rather than spelling "sessions" here — a
// second spelling would let this case pass while looking at a directory nothing
// writes to.
func chatResumeE2ESessionsRoot(t *testing.T, home string) string {
	t.Helper()
	return config.SessionsRootIn(filepath.Join(home, config.ProfileDirName))
}

// chatResumeE2ESessionDirs lists the session containers under root: the
// directories a bare chat session owns. A file at that level is not a session
// container and is reported rather than skipped, because the defect this case
// measures IS an extra container appearing.
func chatResumeE2ESessionDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read the sessions root %s: %v", root, err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs
}

// chatResumeE2ESessionStarts returns the sessionId of every session_start row in
// the record, plus the resumedFrom of each ("" for the run that started the
// session) — the witness the record itself carries.
func chatResumeE2ESessionStarts(t *testing.T, path string) (ids, resumedFrom []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the session record %s: %v", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev struct {
			Type        string `json:"type"`
			SessionID   string `json:"sessionId"`
			ResumedFrom string `json:"resumedFrom"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // a torn or foreign line is not a session_start
		}
		if ev.Type != sessionevent.EventTypeSessionStart {
			continue
		}
		ids = append(ids, ev.SessionID)
		resumedFrom = append(resumedFrom, ev.ResumedFrom)
	}
	return ids, resumedFrom
}

// TestChatResumeKeepsOneSessionDirectory is the epic's acceptance case, stated as
// the operator would: one `chat --once` creates a session, then `--resume <id>
// --once` continues it, and afterwards there is still ONE session directory, one
// raw log that GREW, and a record whose sessionId never changed.
func TestChatResumeKeepsOneSessionDirectory(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: chatResumeE2EReply1},
		testutil.Resp{Text: chatResumeE2EReply2},
	)
	defer llm.Close()
	env := chatResumeE2EEnv(t, home, llm.URL())

	// ── run 1: the session is CREATED ────────────────────────────────────────
	stdout1, stderr1, code1 := testutil.RunGopi(t,
		[]string{"chat", "--once", "-p", chatResumeE2EPrompt1}, "", env, work)
	if code1 != 0 {
		t.Fatalf("the creating run must complete: code=%d\n--- stdout ---\n%s\n--- stderr ---\n%s", code1, stdout1, stderr1)
	}
	root := chatResumeE2ESessionsRoot(t, home)
	dirs := chatResumeE2ESessionDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("after ONE chat run the sessions root holds %d director(ies) %v — the case's premise is one session, one directory\n--- stderr ---\n%s",
			len(dirs), dirs, stderr1)
	}
	sessionID := dirs[0]
	sessionDir := filepath.Join(root, sessionID)
	rawPath := filepath.Join(sessionDir, sessionID+".raw.log")
	recordPath := filepath.Join(sessionDir, "session.jsonl")

	rawBefore, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("the creating run left no raw log at %s: %v\n--- stderr ---\n%s", rawPath, err, stderr1)
	}
	if got, want := chatResumeE2ERawRequestCount(t, rawPath), 1; got != want {
		t.Fatalf("the creating run's raw log holds %d request round(s), want %d — the measurement of 'the resume appended a round' is not anchored\n%s",
			got, want, rawPath)
	}
	sess1, err := gopilog.ParseFile(recordPath)
	if err != nil {
		t.Fatalf("parse the created session %s: %v", recordPath, err)
	}
	turnsBefore := gopilog.SessionTurns(sess1)
	if turnsBefore < 1 {
		t.Fatalf("the created session holds %d turn(s) by gopilog.SessionTurns — the baseline this case compares against is not a conversation", turnsBefore)
	}

	// ── run 2: the session is CONTINUED ──────────────────────────────────────
	//
	// THE DOCUMENTED SPELLING, not the joined one. The help page advertises
	// `--resume SESSION` (the value in its own argv slot) and mentions the
	// joined `--resume=SESSION` as an accepted variant — and until now this case
	// ran only the variant, so the form a reader is taught was never executed
	// end to end. That gap is not hypothetical: the one-shot face refused the
	// spaced form with "more than one prompt given (<id> and <text>)" for as long
	// as it existed (task 01a1ae04), and this case stayed green throughout
	// because it typed the joined form.
	stdout2, stderr2, code2 := testutil.RunGopi(t,
		[]string{"chat", chatOnceFlag, chatFlagResume, sessionID, chatOncePromptFlag, chatResumeE2EPrompt2}, "", env, work)
	if code2 != 0 {
		t.Fatalf("the resuming run must complete: code=%d\n--- stdout ---\n%s\n--- stderr ---\n%s", code2, stdout2, stderr2)
	}

	// (1) ONE session directory, and it is the SAME one. This is the epic's
	// headline: the defect was a second directory, not a broken conversation.
	after := chatResumeE2ESessionDirs(t, root)
	if len(after) != 1 || after[0] != sessionID {
		t.Fatalf("after `chat --resume %s` the sessions root holds %d director(ies) %v — a resume must continue the session it was handed, not mint another\n--- stderr ---\n%s",
			sessionID, len(after), after, stderr2)
	}

	// (2) THE SAME RAW LOG WAS APPENDED — not recreated, not replaced. The
	// prefix is checked byte for byte: an append cannot rewrite the creation
	// header, so a changed prefix means the file is a different recording under
	// the same name.
	rawAfter, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("the resuming run lost the continued raw log %s: %v", rawPath, err)
	}
	if len(rawAfter) <= len(rawBefore) {
		t.Fatalf("the raw log did not grow: %d bytes before the resume, %d after — the resumed run's HTTP traffic went somewhere else\n--- stderr ---\n%s",
			len(rawBefore), len(rawAfter), stderr2)
	}
	if !strings.HasPrefix(string(rawAfter), string(rawBefore)) {
		t.Fatalf("the raw log's existing bytes CHANGED across the resume (it was rewritten, not appended)\n--- before (first 200 bytes) ---\n%s\n--- after (first 200 bytes) ---\n%s",
			firstBytes(rawBefore, 200), firstBytes(rawAfter, 200))
	}
	if got, want := chatResumeE2ERawRequestCount(t, rawPath), 2; got != want {
		t.Fatalf("the continued raw log holds %d request round(s), want %d (one per run) — the second run's round was not appended to this file\n%s",
			got, want, rawPath)
	}

	// (3) THE TURN COUNT GREW on the same record, through the ONE counter
	// `gopi session log --turns` prints.
	sess2, err := gopilog.ParseFile(recordPath)
	if err != nil {
		t.Fatalf("parse the continued session %s: %v", recordPath, err)
	}
	if got := gopilog.SessionTurns(sess2); got <= turnsBefore {
		t.Fatalf("the continued session still holds %d turn(s) (was %d before the resume) — the resumed turn landed in another record\n--- stderr ---\n%s",
			got, turnsBefore, stderr2)
	}

	// (4) THE SESSION ID NEVER CHANGED, and the record SAYS it was resumed: two
	// session_start rows, both naming the same id, the second carrying
	// resumedFrom. Without that field a file holding two headers is
	// indistinguishable from a corrupt one.
	ids, resumed := chatResumeE2ESessionStarts(t, recordPath)
	if len(ids) != 2 {
		t.Fatalf("the continued record holds %d session_start row(s) %v, want 2 (the creating run and the resuming run)\n--- stderr ---\n%s",
			len(ids), ids, stderr2)
	}
	for i, got := range ids {
		if got != sessionID {
			t.Fatalf("session_start row %d names sessionId=%q, want %q — the resumed run recorded itself under another id",
				i+1, got, sessionID)
		}
	}
	if resumed[0] != "" {
		t.Errorf("the CREATING run's session_start carries resumedFrom=%q — the field keeps its single meaning only if the run that started the session leaves it empty",
			resumed[0])
	}
	if resumed[1] != sessionID {
		t.Errorf("the RESUMING run's session_start carries resumedFrom=%q, want %q — the record must state which session it continues",
			resumed[1], sessionID)
	}

	// (5) BOTH ANSWERS ARE THE RUNS' OWN: the mock served two replies, so the
	// second run really ran a turn rather than replaying the first.
	if !strings.Contains(stdout1, chatResumeE2EReply1) || !strings.Contains(stdout2, chatResumeE2EReply2) {
		t.Errorf("the two runs' answers do not match their own turns:\n--- run 1 stdout ---\n%s\n--- run 2 stdout ---\n%s", stdout1, stdout2)
	}
}

// firstBytes is the head of a capture, for the diagnostic above. It exists so a
// failure prints the first divergence rather than a whole raw log.
func firstBytes(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
