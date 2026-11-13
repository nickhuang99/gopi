package agent

// reframe_gate_test.go — the grep gates that lock the reframe+evaluate merge
// in CI (Rule 13: a retired constant bypassed by a bare literal is a defect).
//
// Two retirements must stay retired:
//
//   - the TOOL NAME task_evaluate (Rule 3: it is gone from claudeToolDefs, so
//     any surviving reference is dead code or a re-introduction), and
//   - the NEEDS_RESEARCH verdict (dropped 4→3: research IS reframing, so a
//     research "exit" is a loophole — a genuine unknown becomes a roadmap
//     item).
//
// The gate scans the package's PRODUCTION sources (test files are exempt:
// this file must be free to NAME the retired strings to assert their absence).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskverdict"
	"github.com/nick/gopi/internal/toolnames"
)

// prodGoSources returns (name, content) for every non-test .go file in dir
// (non-recursive — the agent package is flat).
func prodGoSources(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		out[e.Name()] = b
	}
	return out
}

// TestTaskEvaluateRetiredGate: the retired tool name must not reappear as a
// live reference anywhere in the tree.
//
// Two forms are gated:
//   - the IDENTIFIER toolnames.ToolTaskEvaluate (it is deleted; a use is a
//     compile error waiting for someone to "fix" it by re-adding the constant), and
//   - the quoted WIRE name "task_evaluate" in every production source EXCEPT
//     the single archival comment site at its former definition
//     (internal/toolnames/names.go — the Rule 13 "one allowed holder"
//     pattern: the audit trail may name what was deleted, nothing else may
//     reference it).
func TestTaskEvaluateRetiredGate(t *testing.T) {
	const (
		retiredWire = `"task_evaluate"`
		retiredID   = "ToolTaskEvaluate"
	)
	// relDir -> the single file allowed to archive the wire literal.
	dirs := []struct{ dir, archiveOK string }{
		{".", ""},
		{filepath.Join("..", "toolnames"), "names.go"},
		{filepath.Join("..", "..", "cmd", "agent"), ""},
	}
	for _, d := range dirs {
		for name, b := range prodGoSources(t, d.dir) {
			if name == d.archiveOK {
				continue // the ONE archival site: it records what was retired
			}
			if bytes.Contains(b, []byte(retiredID)) {
				t.Errorf("%s/%s still references the deleted constant %s — task_evaluate was absorbed by task(action=reframe)",
					d.dir, name, retiredID)
			}
			if !bytes.Contains(b, []byte(retiredWire)) {
				continue
			}
			t.Errorf("%s/%s still carries the retired wire name %s — task_evaluate was absorbed by task(action=reframe); reference the action constant, never the retired name",
				d.dir, name, retiredWire)
		}
	}
}

// TestNoNeedsResearchGate: the dropped verdict appears in no production
// source of internal/agent or internal/toolnames (the schema must not offer
// it) and — the load-bearing half — it is NOT in the verdict vocabulary.
func TestNoNeedsResearchGate(t *testing.T) {
	const dropped = "NEEDS_RESEARCH"
	dirs := []string{".", filepath.Join("..", "toolnames")}
	for _, dir := range dirs {
		for name, b := range prodGoSources(t, dir) {
			if bytes.Contains(b, []byte(dropped)) {
				t.Errorf("%s/%s mentions %s — the verdict was dropped (research is a roadmap item), so the schema and the prompt must never offer it",
					dir, name, dropped)
			}
		}
	}
}

// TestReframeSchemaCarriesTheReframeContract pins the schema half of the
// merge: the task tool must document the reframe action with all three
// verdicts and every conditional parameter, and must not offer the dropped
// research verdict.
//
// The `verdict` property deliberately carries NO JSON enum: it is SHARED with
// action=review (A-E), so the vocabulary lives in its description and the
// harness (validateReframe) is what actually rejects an invalid value — the
// pattern the review action already established (dispatch-validated, not
// schema-validated; the schema's job is to teach, the gate's job is to
// enforce).
func TestReframeSchemaCarriesTheReframeContract(t *testing.T) {
	doc := taskToolDescription(t) + " " + taskToolPropertyDescriptions(t)
	if !strings.Contains(doc, "action="+toolnames.ToolTaskReframe) {
		t.Fatalf("task tool description must document action=%s", toolnames.ToolTaskReframe)
	}
	for _, v := range taskverdict.All() {
		if !strings.Contains(doc, v) {
			t.Errorf("task schema is missing reframe verdict %s", v)
		}
	}
	if strings.Contains(doc, "NEEDS_RESEARCH") {
		t.Error("task schema offers the dropped NEEDS_RESEARCH verdict")
	}
	// The conditional payload parameters must exist in the schema so the model
	// can supply them (the HARNESS rejection is what enforces them).
	for _, p := range []string{"roadmap", "reason", "epic_summary", "constraints", "deliverables", "acceptance"} {
		if !schemaHasProperty(t, toolnames.ToolTask, p) {
			t.Errorf("task schema is missing the reframe parameter %q", p)
		}
	}
	// A roadmap item carries BOTH goal and acceptance (the child briefing).
	for _, p := range []string{"goal", "acceptance"} {
		if !roadmapItemHasProperty(t, p) {
			t.Errorf("roadmap item schema is missing %q", p)
		}
	}
}

// --- schema readers (one place; the merge left the task def one big literal) ---

func taskToolDef(t *testing.T) map[string]interface{} {
	t.Helper()
	for _, td := range claudeToolDefs {
		if td.Name == toolnames.ToolTask {
			schema, _ := td.InputSchema.(map[string]interface{})
			return schema
		}
	}
	t.Fatal("task tool def not found in claudeToolDefs")
	return nil
}

func taskToolSchemaProperties(t *testing.T) map[string]interface{} {
	t.Helper()
	props, ok := taskToolDef(t)["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("task schema has no properties")
	}
	return props
}

func taskToolDescription(t *testing.T) string {
	t.Helper()
	for _, td := range claudeToolDefs {
		if td.Name == toolnames.ToolTask {
			return td.Description
		}
	}
	t.Fatal("task tool def not found in claudeToolDefs")
	return ""
}

// taskToolPropertyDescriptions concatenates every property's description (the
// LLM-facing documentation of the task tool's arguments).
func taskToolPropertyDescriptions(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, v := range taskToolSchemaProperties(t) {
		prop, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if d, ok := prop["description"].(string); ok {
			b.WriteString(d + " ")
		}
	}
	return b.String()
}

func schemaHasProperty(t *testing.T, tool, prop string) bool {
	t.Helper()
	_, ok := taskToolSchemaProperties(t)[prop]
	return ok
}

// roadmapItemHasProperty reports whether the roadmap array's item object
// documents prop (the child-task briefing fields).
func roadmapItemHasProperty(t *testing.T, prop string) bool {
	t.Helper()
	rm, ok := taskToolSchemaProperties(t)["roadmap"].(map[string]interface{})
	if !ok {
		return false
	}
	items, ok := rm["items"].(map[string]interface{})
	if !ok {
		return false
	}
	props, ok := items["properties"].(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = props[prop]
	return ok
}
