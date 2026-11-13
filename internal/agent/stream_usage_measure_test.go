package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/testutil"
)

// The measured shape these tests are built on (this repo's own raw logs,
// session 8981132f, lines 6 and 310): a REAL stream announces the prompt-side
// count at message_start and repeats it at message_delta, differing only in
// output_tokens —
//
//	message_start … "usage":{"input_tokens":29456,"cache_creation_input_tokens":0,
//	                         "cache_read_input_tokens":2048,"output_tokens":0}
//	message_delta … same three prompt-side counters, "output_tokens":113
//
// so the context measurement is available one frame earlier than the code used
// to look for it, and the frames that never reach a delta (interrupted,
// over-budget re-issue, a dying stream) can be measured too.

const (
	// streamUsageInput / streamUsageCacheRead are the scripted prompt-side
	// counters; their sum is what must land in Context.PromptTokens.
	streamUsageInput     = 1200
	streamUsageCacheRead = 8000
	// streamUsageOutput is scripted LARGER than the prompt side would allow it
	// to be confused with: it must never enter the prompt-side measurement.
	streamUsageOutput = 77
)

func streamUsageStartJSON() string {
	return fmt.Sprintf(`{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d,"output_tokens":0}}}`,
		streamUsageInput, streamUsageCacheRead)
}

func streamUsageDeltaJSON() string {
	return fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d,"output_tokens":%d}}`,
		streamUsageInput, streamUsageCacheRead, streamUsageOutput)
}

// streamAgent builds an Agent whose Claude client answers every request with
// the scripted SSE body the caller writes.
func streamAgent(t *testing.T, script func(w http.ResponseWriter)) *Agent {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		script(w)
	}))
	t.Cleanup(srv.Close)
	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:     srv.URL,
			APIKey:      "test-key",
			HTTPClient:  srv.Client(),
			RetryConfig: deepseek.DefaultRetryConfig(),
		},
		Model:  "test-model",
		Logger: log.New(io.Discard, "", 0),
		Ctx:    &Context{},
	}
	ag.Ctx.AddUserMessage("hello")
	return ag
}

func (a *Agent) streamRound(t *testing.T) {
	t.Helper()
	req := deepseek.BuildClaudeRequest(a.Model, a.Ctx.WireMessages(false), a.SystemPrompt, "", nil, 1024, nil, "")
	if _, err := a.chatOnce(context.Background(), req, 0); err != nil {
		t.Fatalf("chatOnce: %v", err)
	}
}

// TestStreamMessageStartUsageReachesContext is the regression: the API's
// measured prompt-side count must reach Context on the STREAMING path. Both
// frames are scripted, and the assertion is on the SUM of the three prompt-side
// counters (input + cache_creation + cache_read) — the same total the
// non-streaming arm records — never on input_tokens alone.
func TestStreamMessageStartUsageReachesContext(t *testing.T) {
	ag := streamAgent(t, func(w http.ResponseWriter) {
		testutil.WriteClaudeSSE(w, "message_start", streamUsageStartJSON())
		testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		testutil.WriteClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`)
		testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		testutil.WriteClaudeSSE(w, "message_delta", streamUsageDeltaJSON())
		testutil.WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	})
	ag.streamRound(t)

	want := streamUsageInput + streamUsageCacheRead
	if ag.Ctx.PromptTokens != want {
		t.Fatalf("PromptTokens = %d, want %d (input %d + cache_read %d) — the API measurement did not reach the context",
			ag.Ctx.PromptTokens, want, streamUsageInput, streamUsageCacheRead)
	}
	if ag.Ctx.PromptTokensIndex != len(ag.Ctx.Messages) {
		t.Errorf("PromptTokensIndex = %d, want %d (the measurement covers every message sent)",
			ag.Ctx.PromptTokensIndex, len(ag.Ctx.Messages))
	}
}

// TestStreamUsageIsMeasuredWithoutAMessageDelta pins the arm that used to be
// missing: a round whose stream ends after message_start — no delta at all —
// must still calibrate the context. Under the old single-arm code this left
// PromptTokens at 0 and the estimate purely heuristic, which is the state every
// interrupted / over-budget-reissued / dying round produced.
func TestStreamUsageIsMeasuredWithoutAMessageDelta(t *testing.T) {
	ag := streamAgent(t, func(w http.ResponseWriter) {
		testutil.WriteClaudeSSE(w, "message_start", streamUsageStartJSON())
		testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		testutil.WriteClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"interrupted"}}`)
		// NO message_delta / message_stop: the stream simply ends.
	})
	ag.streamRound(t)

	if want := streamUsageInput + streamUsageCacheRead; ag.Ctx.PromptTokens != want {
		t.Fatalf("PromptTokens = %d after a delta-less stream, want %d — message_start's usage must calibrate the context on its own",
			ag.Ctx.PromptTokens, want)
	}
}

// TestStreamZeroUsageIsAbsenceNotMeasurement: a frame carrying no counters must
// NOT be written into the context. Recording 0 would replace a real count with a
// fabricated "measured zero", and EstimatedTokens would silently fall back to
// the heuristic while the log line still claimed to be measured (Rule 15).
func TestStreamZeroUsageIsAbsenceNotMeasurement(t *testing.T) {
	ag := streamAgent(t, func(w http.ResponseWriter) {
		testutil.WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0}}}`)
		testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		testutil.WriteClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`)
		testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
		testutil.WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`)
		testutil.WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
	})
	ag.streamRound(t)

	if ag.Ctx.PromptTokens != 0 || ag.Ctx.PromptTokensIndex != 0 {
		t.Fatalf("an all-zero usage frame wrote a measurement (%d/%d) — an ABSENCE of measurement must not become a measured 0",
			ag.Ctx.PromptTokens, ag.Ctx.PromptTokensIndex)
	}
}

// TestCompactionLogNamesTheMeasuredCountAtTrigger is the second half of the
// reported symptom, and it is a READING-POINT bug, not a missing measurement:
// the trigger fires ON the measured count, then compactContext RESETS
// PromptTokens (the measurement covered the pre-compaction context), and the log
// line read the field after that reset — so every compacted line in the field
// reads "(measured 0)" for a judgment that was made on a real value. The line
// must carry what the trigger judged.
//
// Field evidence the scripted numbers mirror: ~/.gopi/sessions/8981132f's own
// console.log reads "(measured 0)" while its raw log holds 144 message_start
// frames with a non-zero prompt side (the largest 151407).
func TestCompactionLogNamesTheMeasuredCountAtTrigger(t *testing.T) {
	ag, _ := newCompactTestAgent(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, compactTextResponse("SUMMARY"))
	}, 2000)
	var buf bytes.Buffer
	ag.Logger = log.New(&buf, "", 0)
	ag.CompactKeepRecent = 0
	fillHistory(ag.Ctx, 6, 500)
	const measuredAtTrigger = 9000
	ag.Ctx.SetPromptTokens(measuredAtTrigger)

	ag.trimContext(context.Background())

	// The reset really happened — this test would be vacuous without it, and it
	// is what makes the log line's source meaningful.
	if ag.Ctx.PromptTokens != 0 {
		t.Fatalf("test premise: compaction must reset the measurement, got PromptTokens=%d", ag.Ctx.PromptTokens)
	}
	logged := buf.String()
	if !strings.Contains(logged, fmt.Sprintf("(measured %d)", measuredAtTrigger)) {
		t.Fatalf("the compaction line does not carry the measured count the trigger judged; log:\n%s", logged)
	}
	if strings.Contains(logged, "(measured 0)") {
		t.Fatalf("the compaction line still reports the POST-RESET field instead of the trigger-time measurement; log:\n%s", logged)
	}
}
