package deepseek

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildClaudeRequest_ReasoningEffortWire verifies the P0-1 wire contract:
// reasoning_effort is serialized only when thinking is enabled and the value
// is a real effort (never "" / "off").
func TestBuildClaudeRequest_ReasoningEffortWire(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}

	cases := []struct {
		name      string
		thinking  *Thinking
		effort    string
		wantWire  bool
		wantValue string
	}{
		{"enabled+high", &Thinking{Type: "enabled"}, "high", true, "high"},
		{"enabled+minimal", &Thinking{Type: "enabled"}, "minimal", true, "minimal"},
		{"enabled+empty", &Thinking{Type: "enabled"}, "", false, ""},
		{"enabled+off-not-wired", &Thinking{Type: "enabled"}, "off", false, ""},
		{"disabled+high-dropped", &Thinking{Type: "disabled"}, "high", false, ""},
		{"nil-thinking+high-dropped", nil, "high", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, tc.thinking, tc.effort)
			b, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			s := string(b)
			if tc.wantWire {
				if !strings.Contains(s, `"reasoning_effort":"`+tc.wantValue+`"`) {
					t.Errorf("wire missing reasoning_effort=%q: %s", tc.wantValue, s)
				}
			} else if strings.Contains(s, "reasoning_effort") {
				t.Errorf("wire must NOT contain reasoning_effort: %s", s)
			}
		})
	}
}

// TestBuildClaudeRequest_NoEffortArgBackwardCompat: callers that pass no
// effort argument (compact context) must see no reasoning_effort on the wire.
func TestBuildClaudeRequest_NoEffortArgBackwardCompat(t *testing.T) {
	req := BuildClaudeRequest("m", nil, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "enabled"})
	b, _ := json.Marshal(req) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	if strings.Contains(string(b), "reasoning_effort") {
		t.Errorf("no-effort-arg call must not wire reasoning_effort: %s", string(b))
	}
}
