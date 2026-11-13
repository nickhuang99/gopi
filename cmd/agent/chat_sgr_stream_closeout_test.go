package main

// chat_sgr_stream_closeout_test.go — the recording's close-out record, on the exit
// paths that do NOT reach the run's main teardown (bug 01a1b823-5070).
//
// THE DEFECT THIS PINS, measured 2026-11-08 on this host: `SgrStream.Close` is what
// writes `#gopi-sgr-stream-end`, and it used to be reachable only from the big
// teardown defer registered ~200 lines after the stream was opened. A return in
// between — `chatArmDump` failing is the cheapest one to reach — left a file with a
// header, 116 bytes, and NO close-out record. Five such files were found in the
// session tree; nine more had no trailer at all.
//
// THE FAILURE MODE IS NOT "THE FILE IS SHORT". A stream with a header and no trailer
// is indistinguishable from a run that died mid-write, which is exactly the
// misdiagnosis the replay verb used to print (`the stream ends mid-frame`) — so a
// recorder that does not close its own successful exits manufactures the defect the
// reader was just taught to name. The two halves of this bug are one story, which is
// why this test lives beside the replay tests rather than with the writer's unit
// tests.
//
// THE ASSERTION IS ON THE FILE, not on a return value: the run's status is the
// fixture's premise (it must be a FAILING run — that is the exit path under test),
// and the finding is what the file's own records say afterwards.

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/uiclient"
)

// TestChatSgrStreamWritesItsCloseoutOnAnEarlyExit arms the raw stream, forces the
// run to fail at the dump-arming step (a dump PATH that is a directory: `install`
// cannot rename onto it), and then reads the artifact's OWN ending.
func TestChatSgrStreamWritesItsCloseoutOnAnEarlyExit(t *testing.T) {
	dir := t.TempDir()
	sgrPath := filepath.Join(dir, "early-exit"+uiclient.ScreenStreamSuffix)
	// A DIRECTORY where the dump wants a file. `chatResolveDumpPath` accepts it (the
	// parent exists), so the failure lands in `chatArmDump` — AFTER the raw stream was
	// opened and BEFORE the teardown defer existed. That window is the defect.
	dumpAsDir := filepath.Join(dir, "dump-is-a-directory")
	if err := os.Mkdir(dumpAsDir, 0o755); err != nil {
		t.Fatalf("create the dump-as-directory fixture: %v", err)
	}

	ag := &agent.Agent{
		Ctx:     &agent.Context{},
		Model:   chatBoundaryTestModel,
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	in, closeIn := newChatInputCooked(strings.NewReader("quit\n"), io.Discard)
	code := runChatWithInput([]string{
		"--no-editor",
		chatSgrStreamFlag + chatFlagValueSep + sgrPath,
		chatDumpFlag + chatDumpFlagSep + dumpAsDir,
	}, in)
	closeIn()

	// THE PREMISE: this run FAILED at the arming step. A run that got past it would
	// be exercising a different exit path and this test would prove nothing.
	if code == chatExitOK {
		t.Fatalf("status = %d, want a failure: the fixture's whole point is the exit path "+
			"between opening the stream and the run's own teardown", code)
	}

	end, size, err := uiclient.ScanSgrStreamEndPath(sgrPath)
	if err != nil {
		t.Fatalf("scan the artifact at %s (%d byte(s)): %v", sgrPath, size, err)
	}
	if !end.Closed() {
		t.Fatalf("the stream %s (%d byte(s)) has state=%q and NO close-out record: a failing run that "+
			"left the artifact it opened unclosed manufactures exactly the shape the replay verb used to "+
			"misreport as a mid-frame truncation", sgrPath, size, end.State)
	}
	if end.Contradicts() {
		t.Fatalf("the closed stream %s contradicts itself: %+v", sgrPath, end.Findings)
	}
}
