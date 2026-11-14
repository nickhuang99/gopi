package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// trimFixtureRoundBytes sizes one round's request body ABOVE the trim tail
// budget, which is what makes the fixture reproduce the measured defect.
const trimFixtureRoundBytes = 32 << 10

// buildResumeTrimFixture writes a raw log the REAL trim arm will rewrite, and
// returns its path plus the cap that gets it over the line. Model names encode
// the round, so the assertion can say WHICH round an answer came from.
func buildResumeTrimFixture(t *testing.T, rounds int) (string, int64) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "resume-fixture.raw.log")
	var b strings.Builder
	b.WriteString("# gopi raw log — created 2026-10-31T00:00:00Z pid=1 session=fixture\n")
	for i := 1; i <= rounds; i++ {
		b.WriteString("→ REQUEST\n")
		fmt.Fprintf(&b, `{"model":"round-%03d","max_tokens":%d,"messages":[{"role":"user","content":[{"type":"text","text":"%s"}]}]}`,
			i, 100, strings.Repeat("x", trimFixtureRoundBytes))
		b.WriteString("\n")
		b.WriteString("← CHUNK\n")
		b.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// THE DANGEROUS SHAPE, reproduced on purpose: the cap is barely over the
	// floor and each round is FAR bigger than the tail budget, so the rewrite's
	// tail starts in the MIDDLE of a round. The file's last `→ REQUEST` marker
	// is therefore the HEAD's — and that is the shape in which the head's
	// payload swallows the marker line (`invalid character '#'`, the measured
	// symptom).
	capBytes := scratch.SessionsRawLogTrimMinCapBytes * 2
	return path, capBytes
}

// TestResumeScanRefusesTrimmedLog is the RESUME half of the trim defect.
//
// PRE-FIX (measured): scanRawLogCheckpoint walked the file line by line, kept
// the last `→ REQUEST` payload, and knew nothing about the `#` header the trim
// arm writes — so on a trimmed log it answered with the HEAD's round-1 request
// while reporting success. A resume seeded from that answer silently discards
// every round the run completed after round one: the run looks resumed and is
// actually rewound.
//
// AFTER: the scan returns the shared refusal sentence, names the file, and
// hands back NO body at all — "empty" would be read as "no completed round"
// (the dump-fallback path), which is also a wrong answer here.
func TestResumeScanRefusesTrimmedLog(t *testing.T) {
	path, capBytes := buildResumeTrimFixture(t, 40)

	// BEFORE: the scan answers, with the LAST round's body.
	body, size, err := scanRawLogCheckpoint(path)
	if err != nil {
		t.Fatalf("pre-trim scan must succeed: %v", err)
	}
	if !strings.Contains(string(body), "round-040") {
		t.Fatalf("pre-trim scan did not answer from the last round: %.120s", string(body))
	}
	if size <= 0 {
		t.Fatalf("pre-trim scan size = %d, want the file's real size", size)
	}

	// The REAL rewrite the operator's verb performs.
	out, err := scratch.TrimRawLog(path, capBytes)
	if err != nil {
		t.Fatalf("TrimRawLog: %v", err)
	}
	if !out.Trimmed {
		t.Fatalf("TrimRawLog refused the fixture (before=%d cap=%d) — not over the cap", out.Before, capBytes)
	}

	// AFTER: refused by name, with no body.
	body, size, err = scanRawLogCheckpoint(path)
	if err == nil {
		t.Fatalf("post-trim scan returned a checkpoint (%d bytes, size=%d) from a log whose middle is gone — that is the head's round-1 request presented as the last state", len(body), size)
	}
	if len(body) != 0 {
		t.Errorf("refusal still handed back %d bytes of body", len(body))
	}
	if !strings.Contains(err.Error(), scratch.TrimmedLogResumeRefusal) {
		t.Errorf("refusal does not carry the shared sentence %q: %v", scratch.TrimmedLogResumeRefusal, err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("refusal does not name the file: %v", err)
	}
	if !strings.Contains(err.Error(), "crash dump") {
		t.Errorf("refusal does not name the alternative a reader can act on: %v", err)
	}

	// The whole resume face inherits the refusal rather than swallowing it: the
	// dump fallback must not be reached with an empty body (that path is for
	// "no completed round", not for "the middle was dropped").
	if _, _, _, _, lerr := LoadCheckpointWithSource(path); lerr == nil {
		t.Fatalf("LoadCheckpointWithSource answered from a trimmed log")
	} else if !strings.Contains(lerr.Error(), scratch.TrimmedLogResumeRefusal) {
		t.Fatalf("LoadCheckpointWithSource error lost the shared refusal sentence: %v", lerr)
	}
}
