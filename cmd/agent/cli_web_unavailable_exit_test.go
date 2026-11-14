package main

// cli_web_unavailable_exit_test.go — the EXIT-STATUS contract of the two web
// faces, driven through the REAL binary.
//
// THE DEFECT THIS PINS (measured 2026-09-27, inside a Rule-34 sandbox):
//
//	$ SB=$(gopi tool sandbox create --label websearch-repro | tail -1)
//	$ GOPI_HOME=$SB gopi web search x; echo $?
//	{"status":"unavailable","method":"merged",…,"message":"websearch scripts dir not found: …"}
//	0
//
// The payload said the tool could not run and the process said success, so a
// scripted caller (the thing a CLI is FOR) could not branch on it: the only
// signal was prose inside a JSON line. The fix is the mapping this file tests —
// status=unavailable ⇒ exit 3, usage errors ⇒ exit 1, and an ANSWER (ok, or the
// natural empty) ⇒ 0.
//
// RED PROOF (same argv, same env, pre-fix binary built from HEAD's source in a
// detached throwaway worktree):
//
//	pre-fix:  {"status":"unavailable",…} exit=0
//	post-fix: {"status":"unavailable",…} exit=3   (stderr names the status)
//
//nolint:lll // the transcript above is evidence, kept verbatim

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// webEmptyStatusStub answers with the plugin's OTHER natural status: a search
// that ran and found nothing. It is the negative control — "empty" is an answer,
// so it must NOT be turned into a failure by the exit-status mapping.
const webEmptyStatusStub = `#!/usr/bin/env python3
import json, sys
a = sys.argv[1:]
count = int(a[a.index("--count")+1]) if "--count" in a else 0
print(json.dumps({"status":"empty","method":"wiki","elapsed_ms":1,"count":0,
                  "results":[]}))
`

// TestWebUnavailablePayloadExitsNonZero is the fix's contract, asserted on both
// faces: the payload is still printed (it carries the reason), and the process
// status says the tool did not run.
func TestWebUnavailablePayloadExitsNonZero(t *testing.T) {
	// A directory that does not exist at all: the EXACT sandbox repro (the tool
	// resolves a profile path nobody created) — and the message must name both
	// the path and the knob.
	empty := filepath.Join(t.TempDir(), config.PluginsDirName)
	p := g5NewProbe(t).with(config.WebSearchScriptsDirEnvName + "=" + empty)

	res := p.run(t, "", "web", "search", "x")
	if res.exit != webPluginExitUnavailable {
		t.Fatalf("gopi web search with no plugin scripts: exit %d, want %d (the tool could not run)\n%s",
			res.exit, webPluginExitUnavailable, res.out)
	}
	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(lastJSONLine(res.out)), &payload); err != nil {
		t.Fatalf("the non-zero exit must still print the tool's payload: %v\n%s", err, res.out)
	}
	if payload.Status != "unavailable" {
		t.Errorf("payload status = %q, want %q", payload.Status, "unavailable")
	}
	// The message must be ACTIONABLE: it names the directory and the knob that
	// points somewhere else (the help documents the same knob).
	if !strings.Contains(payload.Message, empty) {
		t.Errorf("the message does not name the directory the tool looked in (%s):\n%s", empty, payload.Message)
	}
	if !strings.Contains(payload.Message, config.WebSearchScriptsDirEnvName) {
		t.Errorf("the message does not name %s, so a caller learns the path but not the knob:\n%s",
			config.WebSearchScriptsDirEnvName, payload.Message)
	}

	// The sibling face reports the same failure class with the same status.
	res = p.run(t, "", "web", "fetch", "https://example.org/x")
	if res.exit != webFetchExitUnavailable {
		t.Fatalf("gopi web fetch with no plugin scripts: exit %d, want %d\n%s",
			res.exit, webFetchExitUnavailable, res.out)
	}
}

// TestWebSearchHelpDocumentsTheExitStatus asserts the help tells the truth
// about the new contract — the place a scripted caller reads before writing the
// branch. The `-h` page must name the unavailable status and its code.
func TestWebSearchHelpDocumentsTheExitStatus(t *testing.T) {
	p := g5NewProbe(t)
	res := p.run(t, "", "web", "search", "-h")
	if res.exit != 0 {
		t.Fatalf("gopi web search -h: exit %d (want 0)\n%s", res.exit, res.out)
	}
	for _, want := range []string{"Exit status", "status=unavailable", "  3 "} {
		if !strings.Contains(res.out, want) {
			t.Errorf("the help does not document %q — a contract a caller cannot read is not a contract:\n%s", want, res.out)
		}
	}
}

// TestWebEmptyStatusIsNotAFailure is the negative control: status=empty means
// the search RAN and found nothing, which is an answer. A mapping that turned
// every non-ok payload into a failure would be right about the tool and wrong
// about the world.
func TestWebEmptyStatusIsNotAFailure(t *testing.T) {
	p := g5NewProbe(t).with(config.WebSearchScriptsDirEnvName + "=" +
		g5WriteStubScripts(t, webEmptyStatusStub, g5StubFetchEcho))
	res := p.run(t, "", "web", "search", "nothing-matches-this", "--method", "wiki")
	if res.exit != 0 {
		t.Fatalf("status=empty exited %d — an empty result is an answer, not a failure\n%s", res.exit, res.out)
	}
	if !strings.Contains(res.out, `"status": "empty"`) {
		t.Errorf("the payload's own status is not what the help documents:\n%s", res.out)
	}
}
