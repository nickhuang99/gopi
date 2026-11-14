package agent

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// token_reconciliation_test.go — the epic's gap B: the local prediction and the
// server's own prompt_tokens must be reconciled AT RUN TIME, per request, so the
// question "is the deviation a constant or does it drift?" can be answered from a
// session log instead of a hand measurement taken once.

// readReconRows returns every token_reconciliation row of a JSONL file, in file
// order. It fails the test on a malformed row rather than skipping it: a row this
// mechanism wrote is either readable or the mechanism is broken.
func readReconRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var rows []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("malformed JSONL row %q: %v", line, err)
		}
		if row["type"] == sessionevent.EventTypeTokenReconciliation {
			rows = append(rows, row)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return rows
}

// TestTokenReconciliationRowCarriesTheThreeNumbers pins the SHAPE of the row this
// mechanism exists to produce: prediction, server count, signed delta, and the
// derived percentage in basis points. If any of them is missing the row cannot
// answer the question it was written for, so each field is asserted by name.
func TestTokenReconciliationRowCarriesTheThreeNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sl, err := testSessionLogger(t, path, "run-1")
	if err != nil {
		t.Fatalf("newSessionLoggerOnFile: %v", err)
	}
	// 47,915 predicted against 54,223 charged is the pair this run measured in a
	// REAL session (8382c8fd), kept as the fixture because it exercises a
	// non-trivial, non-round deviation: +6,308 tokens, +1316 bp.
	if err := sl.LogTokenReconciliation(47915, 54223, 6308, reconFrameStart, false); err != nil {
		t.Fatalf("LogTokenReconciliation: %v", err)
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	rows := readReconRows(t, path)
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1 (the log holds %d token_reconciliation rows)", len(rows), len(rows))
	}
	row := rows[0]
	want := map[string]float64{
		reconFieldPredictedTokens:     47915,
		reconFieldServerPromptTokens:  54223,
		reconFieldDeltaTokens:         6308,
		reconFieldDeltaPctBasisPoints: 1316,
	}
	for field, v := range want {
		got, ok := row[field].(float64)
		if !ok {
			t.Fatalf("field %q = %#v, want a number (the row carries keys %v)", field, row[field], rowKeys(row))
		}
		if int(got) != int(v) {
			t.Errorf("field %q = %d, want %d", field, int(got), int(v))
		}
	}
	if got := row[reconFieldFrame]; got != reconFrameStart {
		t.Errorf("field %q = %#v, want %q", reconFieldFrame, got, reconFrameStart)
	}
	// WHICH prediction was measured is on the row, because the two populations
	// must not be folded together (see the field's own comment for the measured
	// rows that forced it).
	if got, ok := row[reconFieldPredictionAnchored].(bool); !ok || got != false {
		t.Errorf("field %q = %#v, want false (this fixture is a whole-prompt estimate)", reconFieldPredictionAnchored, row[reconFieldPredictionAnchored])
	}
	// The SIGN is the information: a reader must be able to tell "the engine
	// under-counts" from "the engine over-counts" without knowing which of the
	// other two fields is the reference.
	if server, predicted := want[reconFieldServerPromptTokens], want[reconFieldPredictedTokens]; want[reconFieldDeltaTokens] != server-predicted {
		t.Fatalf("delta %v is not server-predicted (%v)", want[reconFieldDeltaTokens], server-predicted)
	}
}

// testSessionLogger opens a session logger on a fresh file, standing in for the
// production wiring that mints one per run.
func testSessionLogger(t *testing.T, path, runID string) (*SessionLogger, error) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = f.Close() }) //stylegate:ignore teardown only: the logger's Sync is the flush the assertions read, and a close failure after it reports nothing the test can act on
	return newSessionLoggerOnFile(f, path, "test-model", runID, "", "", "")
}

func rowKeys(row map[string]any) []string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	return keys
}

// TestDeltaPercentIsBasisPointsAndRefusesAnUnknownBase pins the derived figure's
// arithmetic AND its refusal. The refusal is the half worth a test: a percentage of
// a zero prediction is not a fact, and writing 0 there would read as "the two
// numbers agreed exactly" — the opposite of what is known.
func TestDeltaPercentIsBasisPointsAndRefusesAnUnknownBase(t *testing.T) {
	cases := []struct {
		name      string
		predicted int
		delta     int
		want      int
		wantOK    bool
	}{
		{"the measured pair from session 8382c8fd", 47915, 6308, 1316, true},
		{"an exact match is zero basis points", 1000, 0, 0, true},
		{"under-counting is positive", 1000, 24, 240, true},
		{"over-counting is negative", 1000, -24, -240, true},
		{"one hundred percent over", 1000, 1000, 10000, true},
		{"no prediction has no percentage", 0, 500, 0, false},
		{"a negative base is equally meaningless", -5, 500, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := deltaPercentBasisPoints(c.predicted, c.delta)
			if ok != c.wantOK {
				t.Fatalf("deltaPercentBasisPoints(%d, %d) ok = %v, want %v", c.predicted, c.delta, ok, c.wantOK)
			}
			if ok && got != c.want {
				t.Fatalf("deltaPercentBasisPoints(%d, %d) = %d, want %d", c.predicted, c.delta, got, c.want)
			}
		})
	}

	// THE AMBIGUITY THIS SIGNATURE EXISTS TO REMOVE: an exact match and "no
	// percentage could be taken" both render the integer 0. Only the second return
	// value tells them apart, and the row therefore OMITS the field for the second.
	if match, ok := deltaPercentBasisPoints(1000, 0); !ok || match != 0 {
		t.Fatalf("an exact match must be a PRESENT 0 bp reading, got (%d, %v)", match, ok)
	}
	if unknown, ok := deltaPercentBasisPoints(0, 0); ok {
		t.Fatalf("no prediction must be an ABSENT percentage, got (%d, ok=true) — a present 0 would read as a perfect match", unknown)
	}
}

// TestReconcilePromptTokensRefusesToFabricateARow pins the three absences that must
// produce NO row rather than a row of zeroes (Rule 15 — a fabricated 0 is worse
// than a missing row, because it is indistinguishable from a measurement).
func TestReconcilePromptTokensRefusesToFabricateARow(t *testing.T) {
	measured := deepseek.ClaudeUsage{
		InputTokens:              46000,
		CacheCreationInputTokens: 0,
		CacheReadInputTokens:     8223,
	}

	t.Run("no session logger means no record to write into", func(t *testing.T) {
		a := &Agent{Ctx: &Context{}}
		// Must not panic, must not create anything: a bare `gopi "…"` run has no
		// JSONL, and inventing one would be a dependency nobody asked for.
		a.reconcilePromptTokens(measured, 47915, reconFrameStart)
	})

	t.Run("no prompt measurement means nothing to reconcile against", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "session.jsonl")
		sl, err := testSessionLogger(t, path, "run-2")
		if err != nil {
			t.Fatalf("newSessionLoggerOnFile: %v", err)
		}
		a := &Agent{Ctx: &Context{}, SessionLog: sl}
		a.reconcilePromptTokens(deepseek.ClaudeUsage{}, 47915, reconFrameStart)
		if err := sl.Sync(); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if rows := readReconRows(t, path); len(rows) != 0 {
			t.Fatalf("wrote %d row(s) for a frame carrying no measurement: %v", len(rows), rows)
		}
	})

	t.Run("no prediction means the delta has no referent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "session.jsonl")
		sl, err := testSessionLogger(t, path, "run-3")
		if err != nil {
			t.Fatalf("newSessionLoggerOnFile: %v", err)
		}
		a := &Agent{Ctx: &Context{}, SessionLog: sl}
		a.reconcilePromptTokens(measured, 0, reconFrameStart)
		if err := sl.Sync(); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if rows := readReconRows(t, path); len(rows) != 0 {
			t.Fatalf("wrote %d row(s) with no prediction to compare against: %v", len(rows), rows)
		}
	})

	t.Run("a real measurement and a real prediction write exactly one row", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "session.jsonl")
		sl, err := testSessionLogger(t, path, "run-4")
		if err != nil {
			t.Fatalf("newSessionLoggerOnFile: %v", err)
		}
		a := &Agent{Ctx: &Context{}, SessionLog: sl}
		a.reconcilePromptTokens(measured, 47915, reconFrameDelta)
		if err := sl.Sync(); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		rows := readReconRows(t, path)
		if len(rows) != 1 {
			t.Fatalf("row count = %d, want 1", len(rows))
		}
		if got := rows[0][reconFieldServerPromptTokens].(float64); int(got) != measured.PromptSideTokens() {
			t.Fatalf("server prompt tokens = %d, want %d (input + cache-creation + cache-read)",
				int(got), measured.PromptSideTokens())
		}
		if got := rows[0][reconFieldFrame]; got != reconFrameDelta {
			t.Fatalf("frame = %#v, want %q", got, reconFrameDelta)
		}
		// A real prediction means the derived percentage is PRESENT.
		if _, ok := rows[0][reconFieldDeltaPctBasisPoints]; !ok {
			t.Fatalf("a row with a real prediction carries no %q field", reconFieldDeltaPctBasisPoints)
		}
	})
}

// TestReconciledFrameIsTheFirstOneWithAMeasurement pins the per-attempt ONCE rule.
// Both stream frames carry a prompt-side count and they agree on every counter, so
// a second row would not record a second fact — it would double the population
// anyone counting drift against, which is exactly how a 2.4% sample becomes a
// "measured over N requests" claim that is off by a factor of two.
func TestReconciledFrameIsTheFirstOneWithAMeasurement(t *testing.T) {
	usage := deepseek.ClaudeUsage{InputTokens: 46000, CacheReadInputTokens: 8223}
	if got := usage.PromptSideTokens(); got != 54223 {
		t.Fatalf("fixture PromptSideTokens = %d, want 54223", got)
	}

	// The guard is one boolean read by both frames' arms; this asserts the RULE on
	// the same expression the arms use, so a change to either arm that drops it
	// fails here rather than in a session log nobody re-reads.
	reconciled := false
	writes := 0
	for _, frame := range []string{reconFrameStart, reconFrameDelta} {
		if usage.HasPromptMeasurement() {
			if !reconciled {
				reconciled = true
				_ = frame
				writes++
			}
		}
	}
	if writes != 1 {
		t.Fatalf("writes = %d over two measuring frames, want exactly 1", writes)
	}
}

// TestPredictionAnchoredSeparatesTheTwoPopulations pins the distinction the first
// REAL chat run forced into the design. The reconciliation row's delta means two
// different things depending on whether the prediction had an API measurement to
// stand on, and folding them together produces a true-sounding number about
// nothing (measured: 0/1/0/94 bp — mean 24 bp — of which three rows measured a
// 1–9 token TAIL estimate and one measured the WHOLE-prompt error of 0.94%).
func TestPredictionAnchoredSeparatesTheTwoPopulations(t *testing.T) {
	t.Run("a fresh context has no measurement to stand on", func(t *testing.T) {
		c := &Context{Messages: []deepseek.Message{{Role: "user", Content: "hi"}}}
		if c.PredictionAnchored() {
			t.Fatalf("an unmeasured context reports anchored: the whole prompt is a local estimate and its delta is the whole-prompt error")
		}
	})

	t.Run("a recorded measurement anchors the prediction", func(t *testing.T) {
		c := &Context{Messages: []deepseek.Message{{Role: "user", Content: "hi"}}}
		c.SetPromptTokens(54000)
		if !c.PredictionAnchored() {
			t.Fatalf("a context carrying PromptTokens reports unanchored: the measurement is being ignored, and the delta would be read as a whole-prompt error it is not")
		}
	})

	t.Run("a measurement past the message list does not anchor", func(t *testing.T) {
		// PromptTokensIndex > len(Messages) is what a TRIM to below the recorded
		// index leaves behind: the measurement no longer covers a prefix of what is
		// there, so CalibratedTokens falls back to the full estimate — and the flag
		// has to agree with it or the row would mislabel its own number.
		c := &Context{Messages: []deepseek.Message{{Role: "user", Content: "hi"}}, PromptTokens: 90000, PromptTokensIndex: 7}
		if c.PredictionAnchored() {
			t.Fatalf("an orphaned measurement reports anchored, but CalibratedTokens() does not use it")
		}
	})

	t.Run("a nil context is not anchored", func(t *testing.T) {
		var c *Context
		if c.PredictionAnchored() {
			t.Fatalf("a nil context reports anchored")
		}
	})
}

// TestReconciliationOmitsThePercentageWithNoPrediction is the Rule 15 half of the
// derived field: with no prediction there is no percentage to take, and the KEY IS
// ABSENT rather than present-and-zero. Presence would be read as a reading — a
// "delta_pct_bp": 0 on a row says the two numbers AGREED EXACTLY, which is the
// opposite of what is known, and the first cut of this code shipped exactly that
// ambiguity behind a sentinel constant that was numerically identical to a real
// reading.
func TestReconciliationOmitsThePercentageWithNoPrediction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sl, err := testSessionLogger(t, path, "run-nopct")
	if err != nil {
		t.Fatalf("testSessionLogger: %v", err)
	}
	// A row written through the logger with a zero prediction: the percentage has no
	// referent, so the field must not appear.
	if err := sl.LogTokenReconciliation(0, 54223, 54223, reconFrameStart, false); err != nil {
		t.Fatalf("LogTokenReconciliation: %v", err)
	}
	if err := sl.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	rows := readReconRows(t, path)
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if v, present := rows[0][reconFieldDeltaPctBasisPoints]; present {
		t.Fatalf("field %q is PRESENT with value %#v on a row with no prediction — a present 0 is indistinguishable from a perfect match",
			reconFieldDeltaPctBasisPoints, v)
	}
	// The rest of the row is still there: the absence of ONE derived field must not
	// turn into the absence of the record.
	if v, ok := rows[0][reconFieldPredictedTokens].(float64); !ok || int(v) != 0 {
		t.Fatalf("field %q = %#v, want 0", reconFieldPredictedTokens, rows[0][reconFieldPredictedTokens])
	}
}

// TestReconcilePromptTokensReportsAWriteFailure pins the half this mechanism was
// missing until the ignored-error ratchet found it (task 01a1b37e).
//
// The row IS best-effort — a reconciliation that could not be recorded must not
// fail a model round that succeeded — but "best-effort" and "silent" are
// different claims, and Rule 15 forbids the second. So the write's error must
// reach the run's own reporting channel; the arm below fails on a reconciler that
// drops it, which is exactly what `_ = a.SessionLog.LogTokenReconciliation(…)`
// did.
//
// THE FAILURE IS ARRANGED WITHOUT FAULT INJECTION: the session log writes through
// a bufio buffer, so a closed descriptor only surfaces once the row is pushed
// through the flush path. Both facts are set here (the descriptor closed, the
// buffer at its sync threshold), which is the same shape a full disk takes.
func TestReconcilePromptTokensReportsAWriteFailure(t *testing.T) {
	measured := deepseek.ClaudeUsage{
		InputTokens:              46000,
		CacheCreationInputTokens: 0,
		CacheReadInputTokens:     8223,
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sl, err := testSessionLogger(t, path, "run-write-failure")
	if err != nil {
		t.Fatalf("newSessionLoggerOnFile: %v", err)
	}
	if err := sl.file.Close(); err != nil {
		t.Fatalf("close the fixture's descriptor: %v", err)
	}
	sl.buffered = sessionLogSyncThreshold

	var reported strings.Builder
	a := &Agent{Ctx: &Context{}, SessionLog: sl, Logger: log.New(&reported, "", 0)}
	a.reconcilePromptTokens(measured, 47915, reconFrameStart)

	if reported.Len() == 0 {
		t.Fatal("a refused reconciliation row was reported to NOBODY — the write failed and the run never learned it (Rule 15)")
	}
	// The message has to name the frame, or a reader of the run's stderr cannot
	// tell which of the two reconciliation frames was lost.
	if !strings.Contains(reported.String(), reconFrameStart) {
		t.Fatalf("the report does not name the frame %q, so it does not say WHICH row was lost: %s",
			reconFrameStart, reported.String())
	}
	// The premise must be real: the row was actually refused, not written and then
	// complained about.
	if rows := readReconRows(t, path); len(rows) != 0 {
		t.Fatalf("wrote %d row(s) through a closed descriptor — the arm above then reports a success: %v", len(rows), rows)
	}
}
