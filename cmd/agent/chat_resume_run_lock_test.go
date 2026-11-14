package main

// chat_resume_run_lock_test.go — the REAL-BINARY proof that `gopi chat --resume`
// cannot execute a session directory another process is executing.
//
// THE MEASURED DEFECT (session 1918704d-3823-40d4-b1bb-e928ac791f73, this host,
// 2026-11-06 08:05:10 … 08:06:12; evidence under
// ~/.gopi/sessions/01a1b347-3de5-718b-b002-f60a581249ee/evidence/). A long-lived
// interactive chat (pid 962224) held that session directory from 11-05 13:01.
// Eleven more `gopi chat --once --resume=<the same id>` processes were launched
// against it inside 60 seconds, and the record shows what that means: 12
// `session_start` rows, 12 `session_end` rows, and every pair of their lifetimes
// OVERLAPPING (36 overlapping pairs, measured) — twelve writers on one directory
// with nothing arbitrating. The other two resume faces (`gopi session resume`,
// `gopi web`) have taken the run-scope flock since 2026-11-05
// (run_lock_resume_seam.go); the chat face never asked.
//
// WHAT THIS CASE PROVES, and why it has to be the shipped binary (Rule 31): the
// seam's unit arms already prove runlock refuses a second descriptor. What they
// cannot prove is that the CHAT ARM reaches the seam at all, and reaches it
// BEFORE it writes — the position is the requirement, and a call placed one line
// too late would still pass a unit test while appending a `session_start` on the
// way to the refusal. So this runs `bin/gopi` twice against a scripted mock
// (zero API calls) and measures the run DIRECTORY: the refused run must exit
// non-zero, name the lock, and leave the session record with the same number of
// `session_start` rows it had before.
//
// THE THIRD RUN IS THE CONTROL. After the holder releases, the same resume must
// succeed — otherwise "refused" could mean anything, including a resume that is
// simply broken.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/runlock"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	chatResumeLockPrompt1 = "the run that creates the session"
	chatResumeLockPrompt2 = "a resume while another process holds the directory"
	chatResumeLockPrompt3 = "the same resume once the holder is gone"
	chatResumeLockReply1  = "created"
	chatResumeLockReply2  = "continued"
)

func TestChatResumeRefusesWhenAnotherProcessHoldsTheRunDirectory(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: chatResumeLockReply1},
		testutil.Resp{Text: chatResumeLockReply2},
	)
	defer llm.Close()
	env := chatResumeE2EEnv(t, home, llm.URL())

	// ── run 1: the session EXISTS ────────────────────────────────────────────
	_, stderr1, code1 := testutil.RunGopi(t,
		[]string{"chat", chatOnceFlag, chatOncePromptFlag, chatResumeLockPrompt1}, "", env, work)
	if code1 != 0 {
		t.Fatalf("the creating run must complete: code=%d\n--- stderr ---\n%s", code1, stderr1)
	}
	root := chatResumeE2ESessionsRoot(t, home)
	dirs := chatResumeE2ESessionDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("the premise is one session, one directory; the root holds %d %v", len(dirs), dirs)
	}
	sessionID := dirs[0]
	sessionDir := filepath.Join(root, sessionID)
	recordPath := filepath.Join(sessionDir, "session.jsonl")

	startsBefore, _ := chatResumeE2ESessionStarts(t, recordPath)
	if len(startsBefore) != 1 {
		t.Fatalf("the created record holds %d session_start row(s), want 1 — the count this case compares against is not anchored", len(startsBefore))
	}

	// ── run 2: a SECOND PROCESS holds the run directory ─────────────────────
	//
	// The holder is this test process — genuinely another process from the
	// `bin/gopi` the resume runs, which is the whole point: flock is arbitration
	// between processes (two open descriptions), not within one.
	holder, err := runlock.Lock(sessionDir)
	if err != nil {
		t.Fatalf("the fixture could not take the lock it is about to hold: %v", err)
	}
	lockPath := holder.Path()
	if lockPath != runlock.PathIn(sessionDir) {
		t.Fatalf("lock path = %q, want %q", lockPath, runlock.PathIn(sessionDir))
	}
	// THE BASELINE IS TAKEN AFTER THE HOLDER ARMS, and that is not sloppiness:
	// taking the lock CREATES run.lock (runlock's own rule — the lock file is
	// never deleted, its content is overwritten), so a snapshot taken before it
	// would report the fixture's own file as the resume's write.
	before := dirSnapshot(t, sessionDir)

	stdout2, stderr2, code2 := testutil.RunGopi(t,
		[]string{"chat", chatOnceFlag, chatFlagResume, sessionID, chatOncePromptFlag, chatResumeLockPrompt2}, "", env, work)

	if code2 == 0 {
		t.Fatalf("the resume SUCCEEDED while %s was held by pid %d — two processes would append to one session record"+
			"\n--- stdout ---\n%s\n--- stderr ---\n%s", lockPath, os.Getpid(), stdout2, stderr2)
	}
	if !strings.Contains(stderr2, lockPath) {
		t.Errorf("the refusal does not name the lock path %q — an operator cannot stat it:\n%s", lockPath, stderr2)
	}
	if !strings.Contains(stderr2, "pid "+itoaPID(os.Getpid())) {
		t.Errorf("the refusal does not name the holder's pid %d:\n%s", os.Getpid(), stderr2)
	}
	if !strings.Contains(stderr2, "startticks ") {
		t.Errorf("the refusal carries no start ticks — a pid alone is recyclable:\n%s", stderr2)
	}
	if !strings.Contains(stderr2, chatProgLabel) {
		t.Errorf("the refusal does not name the face the operator typed (%q):\n%s", chatProgLabel, stderr2)
	}

	// THE REFUSAL WROTE NOTHING. This is the half a unit test on the seam cannot
	// reach: the lock must be taken before LoadCheckpointWithSource /
	// newAgentResuming, both of which append to this directory.
	after := dirSnapshot(t, sessionDir)
	if before != after {
		t.Errorf("the REFUSED resume wrote into the run directory — the lock must be taken before any session file is created"+
			"\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	startsAfterRefusal, _ := chatResumeE2ESessionStarts(t, recordPath)
	if len(startsAfterRefusal) != 1 {
		t.Errorf("the refused resume appended a session_start: %d row(s) after the refusal, want 1", len(startsAfterRefusal))
	}

	// ── run 3: the control — the SAME resume now succeeds ───────────────────
	if rerr := holder.Release(); rerr != nil {
		t.Fatalf("release the fixture lock: %v", rerr)
	}
	stdout3, stderr3, code3 := testutil.RunGopi(t,
		[]string{"chat", chatOnceFlag, chatFlagResume, sessionID, chatOncePromptFlag, chatResumeLockPrompt3}, "", env, work)
	if code3 != 0 {
		t.Fatalf("with nobody holding the directory the resume must succeed: code=%d"+
			"\n--- stdout ---\n%s\n--- stderr ---\n%s", code3, stdout3, stderr3)
	}
	if !strings.Contains(stdout3, chatResumeLockReply2) {
		t.Errorf("the control run did not run its own turn:\n--- stdout ---\n%s", stdout3)
	}
	startsFinal, resumed := chatResumeE2ESessionStarts(t, recordPath)
	if len(startsFinal) != 2 {
		t.Fatalf("the control resume left %d session_start row(s) %v, want 2 (the creating run and this resume)", len(startsFinal), startsFinal)
	}
	if resumed[1] != sessionID {
		t.Errorf("the control resume's session_start carries resumedFrom=%q, want %q", resumed[1], sessionID)
	}
}
