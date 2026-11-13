package agent_test

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/rawlogfmt"
)

// TestRawLogger_RacingAgentsKeepSeparateRounds is the end-to-end statement of
// the same-second collision fix: two agents that start in one wall-clock second
// must each own a file whose round 1 is THEIR OWN request. Before the
// exclusive-create fix both attached to one file (O_APPEND) and
// gopilog.ParseHTTPRounds — the parser behind `gopi session raw <id> N request` — saw
// two rounds in one shared transcript, so round 1 was whichever agent sent
// first: the other session's user prompt, with the reading session's own
// prompt nowhere in that round.
//
// This file lives in package agent_test because gopilog imports internal/agent.
func TestRawLogger_RacingAgentsKeepSeparateRounds(t *testing.T) {
	t.Setenv("GOPI_RAW_LOG", "")
	dir := t.TempDir()
	t.Setenv("GOPI_RAW_LOG_DIR", dir)

	a, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("agent A: %v", err)
	}
	defer a.Close()
	b, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("agent B: %v", err)
	}
	defer b.Close()

	writeRound := func(rl *agent.RawLogger, prompt string) {
		t.Helper()
		body := `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"` + prompt + `"}]}]}`
		// ONE record through the ONE writer: the removed bare-bytes Write path used
		// to spell the direction as marker TEXT here, which is the ambiguity the
		// framed format removes (a body containing that text was a second marker).
		rl.LogFunc()(rawlogfmt.TagRequest, "", []byte(body))
		if err := rl.Sync(); err != nil {
			t.Fatalf("sync: %v", err)
		}
	}
	writeRound(a, "AGENT-A-EPIC")
	writeRound(b, "AGENT-B-EPIC")

	for _, tc := range []struct {
		name string
		rl   *agent.RawLogger
		want string
	}{
		{"A", a, "AGENT-A-EPIC"},
		{"B", b, "AGENT-B-EPIC"},
	} {
		rounds, err := gopilog.ParseHTTPRounds(tc.rl.Path())
		if err != nil {
			t.Fatalf("%s: ParseHTTPRounds: %v", tc.name, err)
		}
		if len(rounds) != 1 {
			t.Fatalf("%s: %d rounds in its own raw log, want exactly 1 (file: %s)",
				tc.name, len(rounds), tc.rl.Path())
		}
		if !strings.Contains(string(rounds[0].RequestRaw), tc.want) {
			t.Errorf("%s: round 1 is not this session's request (%q)", tc.name, rounds[0].RequestRaw)
		}
	}
}
