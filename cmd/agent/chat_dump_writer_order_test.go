package main

// chat_dump_writer_order_test.go — the regression for the dump sink's ORDER
// defect (measured 2026-09-26, `-count=120` stress runs of this package).
//
// THE DEFECT. The chat paint stream has two painters — the turn goroutine's
// answer renderer (Begin/Delta/Finish → writeRow → region.commitScroll) and the
// editor's read loop (Tick's status repaint, the menu) — and with a dump armed
// their writes are fanned out by an io.MultiWriter:
//
//	consumer 1: the terminal (test: the harness's recording writer)
//	consumer 2: the dump sink (its grid is the interpreted EFFECT of the bytes)
//
// io.MultiWriter hands one chunk to each consumer SEQUENTIALLY, so two
// concurrent painters can reach the two consumers in DIFFERENT orders:
//
//	A → terminal(pA) … B → terminal(pB) … B → sink(pB) … A → sink(pA)
//
// Each consumer then interprets a stream the other never saw. The recorded
// failure: the sink froze `▌ a b` / `▌ a   c` while the harness's replayed
// screen — the SAME writes, replayed in their RECORDED order — held
// `▌ a b c`, so `TestChatDump_FenceAndDiffMarkersStayLiteral` reported "the
// dumped grid does not carry \`a b c\` — the answer never reached the artifact"
// on an artifact that HAD received every byte.
//
// THE FIX under test is cmd/agent/chat_dump.go's chatDumpWriter: with a sink
// installed the fan-out runs under ONE lock, so the two consumers always see
// the same order (the no-sink path returns the caller's writer unchanged, so
// nothing at all changes when no dump was asked for).
//
// WHY THE SAMPLE PARKS A CONSUMER. The window between the fan-out's two
// consumer calls is a few instructions wide: a test that merely starts two
// writers at once SAMPLES it (measured: 2 corrupted artifacts per 120
// iterations). Here the terminal consumer parks INSIDE that window on a
// designated chunk, so the interleaving is CONSTRUCTED and the test is
// deterministic — the shape internal/lineedit's parkgate.go already uses for
// its own hard flake.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The park sample's own vocabulary (Rule 1: named, never inline).
const (
	// orderSampleParkChunk is the chunk whose arrival parks the terminal
	// consumer: the settled answer row an assertion looks for.
	orderSampleParkChunk = "\x1b[2;1Ha b c"
	// orderSampleBlankChunk is the overtaking chunk: it blanks two cells of the
	// parked row, which is what "▌   b c" is on the artifact.
	orderSampleBlankChunk = "\x1b[2;3H  "
	// orderSampleReleaseWait bounds the wait for the parked consumer to report
	// itself parked (a park, not a sleep).
	orderSampleReleaseWait = 5 * time.Second
	// orderSampleOvertakeWindow bounds the wait for the overtaking writer to
	// report itself DONE. Post-fix it cannot get past the parked fan-out (it
	// blocks on the paint writer's lock), so the window expiring is itself the
	// observation — a bounded wait on another writer's progress, never a sleep
	// the assertion depends on.
	orderSampleOvertakeWindow = 50 * time.Millisecond
	// orderSampleRow is the screen row both chunks write to (0-based).
	orderSampleRow = 1
)

// parkOnChunk is the TERMINAL in the sample: it writes ONE designated chunk to
// the recorder and then STOPS — the terminal consumer holds that chunk while the
// dump sink has not been handed it yet. That is the fan-out's own window, made
// wide enough to construct the interleaving deterministically instead of
// sampling it.
//
// It parks AFTER delegating to the recorder, deliberately: parking before would
// let both consumers see the same (reversed) order and the sample would prove
// nothing. The gate holds NO lock the other writer needs, so the fan-out itself
// stays unsynchronized — which is the thing under test.
type parkOnChunk struct {
	out    *recordingWriter
	chunk  string
	parked chan struct{}
	resume chan struct{}
	mu     sync.Mutex
	fired  bool
}

func newParkOnChunk(out *recordingWriter, chunk string) *parkOnChunk {
	return &parkOnChunk{out: out, chunk: chunk, parked: make(chan struct{}), resume: make(chan struct{})}
}

func (g *parkOnChunk) Write(p []byte) (int, error) {
	n, err := g.out.Write(p)
	if string(p) == g.chunk {
		g.mu.Lock()
		fire := !g.fired
		g.fired = true
		g.mu.Unlock()
		if fire {
			close(g.parked)
			<-g.resume
		}
	}
	return n, err
}

// TestChatDumpSinkSeesThePaintStreamInTheTerminalsOrder pins the ONE invariant
// the artifact can be trusted on: the dump's grid is what the TERMINAL received,
// byte for byte and in the terminal's order. Two painters, one parked inside the
// fan-out window; the sink's grid and the replay of the recorded stream must
// describe the same screen.
func TestChatDumpSinkSeesThePaintStreamInTheTerminalsOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path, Cols: completionCols, Rows: completionRows,
		Identity: uiclient.DumpIdentity{
			Surface: uiclient.SurfaceChat, PID: os.Getpid(), Target: chatDumpTarget(false),
			ColsRows:         [2]int{completionCols, completionRows},
			TerminalColsRows: [2]int{completionCols, completionRows},
			Started:          time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("arm the chat dump sink: %v", err)
	}

	rec := &recordingWriter{}
	park := newParkOnChunk(rec, orderSampleParkChunk)
	// The PRODUCTION wiring under test: the same chatDumpWriter the REPL installs,
	// with the terminal's own writer in the terminal's position (a slow terminal
	// is a terminal).
	paint := chatDumpWriter(park, d)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := io.WriteString(paint, orderSampleParkChunk); err != nil {
			t.Errorf("the parked painter's write failed: %v", err)
		}
	}()
	select {
	case <-park.parked:
	case <-time.After(orderSampleReleaseWait):
		t.Fatal("the terminal consumer never parked — the sample did not take its shape")
	}
	// The overtaking painter: the editor's read loop repainting while the turn
	// goroutine's row is still between the fan-out's two consumers. The wait below
	// is on THAT writer's own completion — post-fix it cannot complete (it is
	// serialized behind the parked fan-out), so the window expiring is the fact
	// being observed, not a sleep the assertion leans on.
	overtook := make(chan struct{})
	go func() {
		defer wg.Done()
		defer close(overtook)
		if _, err := io.WriteString(paint, orderSampleBlankChunk); err != nil {
			t.Errorf("the overtaking painter's write failed: %v", err)
		}
	}()
	select {
	case <-overtook: // it got past the parked writer: the divergence window was open
	case <-time.After(orderSampleOvertakeWindow): // it is blocked behind the parked fan-out
	}
	close(park.resume)
	wg.Wait()
	if err := d.Close(); err != nil {
		t.Fatalf("freeze the chat dump sink: %v", err)
	}

	// The terminal's own screen: the recorded writes replayed in RECORDED order.
	terminal := lineedit.NewScreen(completionCols, completionRows)
	rec.ReplayInto(terminal)
	terminalRow := terminal.Line(orderSampleRow)
	snap := d.Snapshot()
	if len(snap.Grid) <= orderSampleRow {
		t.Fatalf("the frozen artifact has %d rows, want at least %d:\n%s", len(snap.Grid), orderSampleRow+1, snap.Text())
	}
	sinkRow := snap.Grid[orderSampleRow]

	if strings.TrimRight(terminalRow, " ") != strings.TrimRight(sinkRow, " ") {
		t.Fatalf("the dump sink saw a DIFFERENT byte order than the terminal it observes:\n"+
			"  terminal row %d = %q\n  artifact row %d = %q\n"+
			"The two consumers of the paint stream's fan-out were reached in different orders, so the artifact describes a screen no run drew (measured 2026-09-26: `▌ a b` / `▌ a   c` in the artifact while the replayed stream held `▌ a b c`).",
			orderSampleRow, terminalRow, orderSampleRow, sinkRow)
	}
}
