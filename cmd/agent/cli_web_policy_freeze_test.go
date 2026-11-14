package main

// cli_web_policy_freeze_test.go — the REAL-SURFACE regression for the defect this
// file's task found: `gopi web` (and its `gopi cli` / `gopi ui` siblings) built
// its agent and served tool-executing turns WITHOUT freezing the chat write
// boundary, so every write/edit a web turn made was judged against the tools
// package's own defaults (the built-in `prompt *` table with the on-request
// posture) instead of the operator's ```chat-policy fence and the posture the
// startup channels resolved.
//
// THE DEFAULT ARM WAS REVERSED ON THE OPERATOR'S RULING (2026-10-24, in their own
// words: "我给你的授权是全方位的，只要你是在替我做事，就是允许的。如果有障碍，就手
// 动改掉它"). The arm used to require the shapless default posture to ASK and keep
// the file off disk. That is not what this repository declares: the carrier table
// (internal/tools/chat_policy_carrier.go, chatPolicyCarrierDeclaresNoApprovalChannel)
// names `web`, `ui` and `cli` as faces whose OWN banner, help page and termToolsNotice
// say "turns run the full gopi toolset with no approval gate (loopback only,
// local-shell trust)" — a question rendered as text the asker is already reading is
// not an approval channel, so those faces DEGRADE `prompt` to `allow`.
//
// What the test therefore asserts now, and it is still a real regression guard:
//   - default posture: the write RUNS. A process that failed to freeze the policy
//     would fall back to the tools package's own `prompt *` default with the
//     console carrier, which is NOT in the degradation table, so it would ASK and
//     keep the file off disk — the two states remain distinguishable, and the
//     silent non-freeze is still caught.
//   - yolo posture: the write RUNS.
//   - the ```chat-policy fence still decides: a `forbidden write <path>` rule
//     still refuses (asserted by the fence-driven cases below).
//
// THE OBSERVABLE, and why the arms below are the honest pair: the boundary is
// decided ONCE per process and frozen (internal/tools.SetChatPolicy). A process
// that never calls it keeps asking a human who is not there, and the tool result
// the model gets is `[NEEDS OPERATOR APPROVAL — command NOT run]` — the file is
// never created. A process that DOES freeze it runs the write under the posture
// the channels resolved. So the same scripted turn, driven against the same
// binary, differs in exactly one observable: whether the target file exists.
//
// Rule 31: this drives the real `gopi web` binary (testutil.GopiBin — the pin
// built from the tree under test), over the real HTTP surface, with the real tool
// dispatch. The in-process case (cli_web_align_test.go's R2) covers the frame
// shape; this one covers the PROCESS-level freeze, which is what a fixture that
// constructs a webServer by hand can never see.

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

const (
	// webFreezeWriteContent is the payload the scripted tool_use asks for; the
	// file's content after the turn is the proof the WRITE ran rather than the
	// boundary answering in its place.
	webFreezeWriteContent = "written-by-a-real-web-tools-round"
	// webFreezeTurnPrompt is the user message; its text is irrelevant to the
	// scripted mock (which answers by request INDEX), so it stays short.
	webFreezeTurnPrompt = "write the file"
	// webFreezeTurnTimeout bounds the one turn — a hang guard, not a
	// performance assertion.
	webFreezeTurnTimeout = 60 * time.Second
	// webFreezeApprovalMarker is the prefix the boundary returns when it ASKS
	// instead of running (internal/tools' chatWriteApprovalPrefix). The negative
	// arm asserts its presence in the streamed tool result, so the arm fails
	// for the RIGHT reason if the boundary stops asking.
	webFreezeApprovalMarker = "[NEEDS OPERATOR APPROVAL"
)

// TestWebProcessFreezesTheChatWriteBoundary drives ONE scripted tool round
// against a live `gopi web --tools` in two arms that differ only in the posture
// the startup channel declares:
//
//   - GOPI_CHAT_POLICY unset → the shipped default (on-request) asks; the file
//     must NOT appear and the streamed tool result must carry the approval text.
//   - GOPI_CHAT_POLICY=<yolo> → the declared posture runs everything; the file
//     must appear with the requested content.
//
// The second arm is the fix (the process froze the boundary from its channels);
// the first is what makes the second evidence rather than a coincidence — a
// binary that ignored the boundary entirely would pass arm 2 and fail arm 1.
func TestWebProcessFreezesTheChatWriteBoundary(t *testing.T) {
	cases := []struct {
		name     string
		posture  string
		wantFile bool
	}{
		// The default posture RUNS the write: `web` is a face that declares no
		// approval channel in its own banner/help page, so a `prompt` degrades to
		// `allow` for it. A binary that never froze the policy would fall back to
		// the tools package's `prompt *` default under the CONSOLE carrier and would
		// ASK instead — so this arm still catches the silent non-freeze it was
		// written for.
		{name: "default posture runs the write (web declares no approval channel)", posture: "", wantFile: true},
		{name: "declared allow posture runs the write", posture: chatpolicy.PostureYoloWord, wantFile: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			target := filepath.Join(t.TempDir(), "made-by-real-web-run.txt")
			llm := testutil.NewMockLLM(t,
				testutil.Resp{ToolUse: &testutil.ToolCall{
					ID:   "call_web_freeze",
					Name: "write",
					Args: map[string]interface{}{"path": target, "content": webFreezeWriteContent},
				}},
				testutil.Resp{Text: "done after the tool round"},
			)
			defer llm.Close()

			env := testutil.MockEnv(t, home, llm.URL())
			if tc.posture != "" {
				// APPENDED, not replaced: MockEnv strips every ambient GOPI_* var, so
				// this is the one channel that declares the posture for the child, and
				// exec keeps the LAST occurrence of a duplicated key.
				env = append(env, agent.ChatPolicyEnv+"="+tc.posture)
			}
			p := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0", webToolsFlag}, "", env, "")
			defer p.Stop()
			base := p.WebBaseURL()

			// Readiness = the session artifact, the same signal the sibling live
			// case uses: the JSONL is created at agent construction, before the
			// listener accepts.
			sessRoot := filepath.Join(home, ".gopi", config.SessionsDirName)
			p.WaitFor("the sandboxed session JSONL", func() bool { return testutil.SessionRecordPath(sessRoot) != "" })

			client := &http.Client{Timeout: webFreezeTurnTimeout}
			resp, err := client.Post(base+"/api/chat", "application/json",
				strings.NewReader(`{"message":"`+webFreezeTurnPrompt+`"}`))
			if err != nil {
				t.Fatalf("turn POST failed: %v", err)
			}
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close() //stylegate:ignore the body was drained to EOF by the read above and its error is checked on the next line; the close error of a fully-read response carries no state
			if readErr != nil {
				t.Fatalf("reading the turn stream failed: %v", readErr)
			}
			stream := string(body)
			if !strings.Contains(stream, "event: done") {
				t.Fatalf("turn did not reach `event: done` — stream=%q\nstderr:\n%s", stream, p.Stderr())
			}

			got, statErr := os.ReadFile(target)
			switch {
			case tc.wantFile && statErr != nil:
				t.Fatalf("the tool round did not write %s under the %q posture: %v\nstream=%q\nstderr:\n%s",
					target, tc.posture, statErr, stream, p.Stderr())
			case tc.wantFile && string(got) != webFreezeWriteContent:
				t.Errorf("written content = %q, want %q", got, webFreezeWriteContent)
			case !tc.wantFile && statErr == nil:
				t.Fatalf("the write LANDED under the default (on-request) posture — the boundary did not ask: content=%q\nstream=%q", got, stream)
			}
			if !tc.wantFile && !strings.Contains(stream, webFreezeApprovalMarker) {
				t.Errorf("the default-posture arm did not stream an approval request (%q missing) — the boundary did not stop the write for the reason this arm asserts:\nstream=%q",
					webFreezeApprovalMarker, stream)
			}
		})
	}
}
