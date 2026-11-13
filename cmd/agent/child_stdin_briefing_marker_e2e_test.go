package main

// child_stdin_briefing_marker_e2e_test.go — WHAT THE CHILD ACTUALLY RECEIVED
// (row 01a188da).
//
// THE DEFECT. The wrapper hands its child the briefing on stdin; the child
// records that text as its first user row and the record files it
// `source=human` (the stdin seam has no other honest face). Nothing downstream
// could tell it from something the operator typed, so the framework's
// instructions opened the run's turn list (`session log --turns`) and stood in
// for the person's first line as the row's title.
//
// WHY THIS ARM READS A REAL TRANSCRIPT. The unit arm in internal/sessionevent
// proves the WRAPPER's output satisfies the predicates; it says nothing about
// whether the wrapper is on the path a child is spawned through. A producer that
// still hands over the raw briefing would pass the unit arm and leave the defect
// live — so this walk drives the REAL binary, lets the REAL spawn seam write a
// REAL session log, and asserts on the row the child wrote about itself:
//
//	(1) the first user row OPENS WITH THE MARKER, and
//	(2) the reader's own predicate calls it a harness injection AND not a turn,
//	(3) while the task's INSTRUCTIONS are still all there — a marker that ate
//	    the briefing would satisfy (1) and (2) and deliver nothing.
//
// The briefing's own words are the control: `taskRunBriefing` renders the row's
// summary and payload, so the child's first row must still contain both.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

const (
	// childMarkerE2ETaskID is the row this walk dispatches.
	childMarkerE2ETaskID = "5c1d7a70-2f4e-4a0c-9a52-markerbriefing1"
	// childMarkerE2ESummary is the row's summary: the briefing must carry it.
	childMarkerE2ESummary = "child stdin briefing marker e2e"
	// childMarkerE2EPayload is the row's payload, likewise.
	childMarkerE2EPayload = "the payload sentence the child must still receive"
)

// TestWorkerChildBriefingCarriesTheHarnessMarkerE2E is the acceptance walk.
func TestWorkerChildBriefingCarriesTheHarnessMarkerE2E(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	// repo-context sandbox: same reason as the neighbouring walks — the wrapper
	// must not enumerate the developer's worktrees.
	SandboxRepoContext(t)
	mock := testutil.NewMockLLM(t, testutil.Resp{Text: "briefing received"})
	defer mock.Close()

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", dbPath) // parent-process tools.* calls below
	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SkipClaimBroadcastEnv+"=1",
	)

	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: childMarkerE2ESummary, Payload: childMarkerE2EPayload, TaskID: childMarkerE2ETaskID,
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	stdout, stderr, code := runGopiMapped(t, []string{"worker", childMarkerE2ETaskID}, "", env, "")
	if code != 0 {
		t.Fatalf("worker must succeed on a well-formed row: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if mock.RequestCount() == 0 {
		t.Fatalf("the child never reached the model, so no briefing was delivered:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}

	// THE CHILD'S OWN RECORD. One directory per run, named by the row id — the
	// artifact the operator reads when asking "what was this run told?".
	sessionDir := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, childMarkerE2ETaskID)
	rows := readSessionJSONLUserRows(t, sessionDir)
	if len(rows) == 0 {
		t.Fatalf("the run's session log records no user row under %s — nothing to judge", sessionDir)
	}

	// FIND the briefing row rather than assuming it is rows[0]: the harness also
	// records its own checkpoints on the same stdin seam, and an assertion that
	// pinned the ordinal would break the day one is added.
	idx := -1
	for i, r := range rows {
		if strings.Contains(r.Text, childMarkerE2ESummary) {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no user row carries the row's summary %q — the briefing was not the text the child received:\n%s",
			childMarkerE2ESummary, joinRowHeads(rows))
	}
	briefingRow := rows[idx]

	// THE SHAPE THE RECORD ACTUALLY CARRIES, measured on this walk's own run
	// rather than assumed: the child assembles its prompt as
	//
	//	[EPIC GOAL]\n[gopi briefing] [Task summary]\n…
	//
	// — the frame the child puts on a dispatched briefing, wrapped around the
	// marker the parent put on the text. Both halves are asserted: the marker is
	// present at the head region, and the frame (when present) encloses it. A
	// bare `HasPrefix(text, marker)` was this arm's first cut and it failed here
	// against CORRECT product behaviour — the measurement is what changed the
	// assertion, not the product (a walk that pins the wrong ordinal is
	// measuring its own fixture).
	head := headRunes(briefingRow.Text, 64)
	headAfterFrame := strings.TrimPrefix(briefingRow.Text, sessionevent.HarnessEpicGoalFrameMarker)
	headAfterFrame = strings.TrimLeft(headAfterFrame, " \t\r\n")
	if !strings.HasPrefix(headAfterFrame, sessionevent.HarnessBriefingOnStdinMarker) {
		t.Fatalf("the child's briefing row carries no harness marker after its frame (source=%q, first 64 runes: %q) — the spawn seam handed over an unmarked briefing, so `session log --turns` counts the framework's instructions as the operator's turn and the row's title is the framework's text",
			briefingRow.Source, head)
	}
	if !sessionevent.IsHarnessInjected(briefingRow.Text) {
		t.Fatalf("the child's briefing row is not recognised as a harness injection by the reader's own predicate")
	}
	// THE ROW IS AN INPUT, AND THAT IS THE 2026-11-01 RULING (it used to assert
	// the opposite here). A dispatched run records no operator row, so the
	// briefing it was dispatched with is the ONLY complete input it ever gets;
	// while this arm demanded that the row open nothing, NOTHING in a 136-row run
	// log opened a turn, internal/gopilog's request-row fallback fired, and
	// `session replay <run> --turns` printed one turn per exchange (measured on
	// 01a1944e-900f-70b7-81ac-24eda447b38b: 127 rounds reported as 127 turns).
	//
	// The two assertions are not in conflict — it is the pair a `gopi notify`
	// delivery has carried since 2026-10-27: harness-written AND an input, because
	// the rule is about what the harness must ANSWER, not about who wrote the row.
	if !sessionevent.IsTurnOpeningRow(briefingRow.Text, briefingRow.Source) {
		t.Fatalf("the child's briefing row OPENS NO TURN under the source the record gave it (%q) — a dispatched run then records no input at all and its turn count collapses onto its round count", briefingRow.Source)
	}
	// (3) THE INSTRUCTIONS SURVIVED. A marker that replaced the briefing would
	// pass every assertion above and deliver nothing.
	if !strings.Contains(briefingRow.Text, childMarkerE2EPayload) {
		t.Fatalf("the marked briefing no longer carries the row's payload %q — the wrapper replaced the text instead of prefixing it:\n%q",
			childMarkerE2EPayload, headRunes(briefingRow.Text, 400))
	}
}

// sessionUserRow is one `type=user` record, reduced to what a turn question
// needs: the text and the source the record filed it under.
type sessionUserRow struct {
	Text   string
	Source string
}

// readSessionJSONLUserRows decodes every user row of the run's session log.
//
// The file name is the session id (not the task id), so the directory is
// globbed rather than joined to a guessed name.
func readSessionJSONLUserRows(t *testing.T, dir string) []sessionUserRow {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+".jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no session JSONL under %s: %v", dir, err)
	}
	var out []sessionUserRow
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !strings.Contains(line, `"type":"user"`) {
				continue
			}
			var rec struct {
				Type    string `json:"type"`
				Source  string `json:"source"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue // a torn tail line is not a row this walk judges
			}
			if rec.Type != "user" {
				continue
			}
			out = append(out, sessionUserRow{Text: rec.Message.Content, Source: rec.Source})
		}
	}
	return out
}

// joinRowHeads renders the first rows' openings for a failure message — enough
// for a reader to see which text the child actually got, without pasting a
// whole transcript into the test output.
func joinRowHeads(rows []sessionUserRow) string {
	var b strings.Builder
	for i, r := range rows {
		if i >= 5 {
			b.WriteString("… (further rows omitted)\n")
			break
		}
		b.WriteString("  [" + r.Source + "] " + headRunes(r.Text, 100) + "\n")
	}
	return b.String()
}

// headRunes returns at most n runes of s (rune-safe: a briefing is mostly CJK).
func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// TestTheReaderPredicatesAgreeOnTheRecordedFace pins the OTHER direction of the
// same seam for the record's own vocabulary: for every face the stdin child can
// be recorded under, the MARKER decides — and the source cannot undo it.
//
// THE NOTIFY FACE IS DELIBERATELY NOT IN THIS LOOP, and the omission is the
// ruling, not an oversight: `IsTurnOpeningRow`'s FIRST leg makes every
// `source=notify` row an input whatever its text, because `gopi notify` handed
// the session one complete message from another process (operator ruling
// 2026-10-27). A stdin briefing is never written under that face — the seam
// writes `human` — so asserting the marker over it would be an assertion about a
// combination production cannot produce, and it went red exactly as written when
// this arm was first run.
func TestTheReaderPredicatesAgreeOnTheRecordedFace(t *testing.T) {
	body := childMarkerE2EPayload
	marked := sessionevent.BriefingOnStdin(body)
	// A BRIEFING OPENS A TURN UNDER EVERY FACE IT CAN ARRIVE UNDER, and this arm
	// asserted the OPPOSITE until 2026-11-01 — the day the opposite turned out to
	// be the defect. A dispatched run records no operator row, so the briefing it
	// was dispatched with is the only complete input it ever gets; while the
	// marker was read as "therefore it is machinery", NOTHING in a 136-row run log
	// opened a turn, internal/gopilog's request-row fallback fired, and
	// `session replay <run> --turns` printed one turn per exchange (127 rounds
	// reported as 127 turns on 01a1944e-900f-70b7-81ac-24eda447b38b).
	//
	// The marker is now the INPUT's identity, not a disqualifier — which is the
	// same pair `gopi notify` has carried since the 2026-10-27 ruling
	// (harness-written AND an input). What the marker must still NOT do is make
	// the row unreadable as machinery: that is the assertion below, and it is the
	// one that keeps a briefing off the operator's own transcript line.
	for _, source := range []string{inputsource.Human, inputsource.Queue, inputsource.LLM, inputsource.Notify} {
		if !sessionevent.IsTurnOpeningRow(marked, source) {
			t.Errorf("the marked briefing OPENS NO TURN under source=%q — a run recorded under that face then has no input at all and its turns collapse onto its rounds", source)
		}
	}
	// AND IT IS STILL FRAMEWORK TEXT. If this half goes, every surface that renders
	// a stored row prints the harness's instructions as something the person typed.
	if !sessionevent.IsFrameworkChatter(marked) {
		t.Errorf("the marked briefing is no longer framework chatter — a surface will render it as the operator's own line")
	}
	if !sessionevent.IsHarnessInjected(marked) {
		t.Errorf("the marked briefing is no longer recognised as harness-injected — the marker list and the predicate disagree")
	}
	if !sessionevent.IsTurnOpeningRow(body, inputsource.Human) {
		t.Errorf("the UNMARKED body does not open a turn under the human face — the counter-example is gone")
	}
	if !sessionevent.IsTurnOpeningRow(marked, inputsource.Notify) {
		t.Errorf("a marked row under the NOTIFY face is no longer a turn — a delivery is an input by the operator's ruling, and a marker must not silently repeal it")
	}
}
