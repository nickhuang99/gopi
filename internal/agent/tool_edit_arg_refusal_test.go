package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// editRefusalFixtureContent is the minimal sample the silent-content-loss
// defect was measured on (2026-10-04, task
// 01a103c2-36e2-7808-bc42-d652762c4691): a unique anchor line between two
// neighbours, so "the replacement landed", "the anchor survived" and "the file
// grew a line" are three independently observable facts.
const editRefusalFixtureContent = "line-A\n| anchor-row | keep me |\nline-C\n"

func writeEditRefusalFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	return path
}

// TestExecuteTool_EditRefusesAnEmptyReplacement pins the fix for the measured
// silent-content-loss defect: an `edit` call whose replacement is empty must be
// REFUSED, never performed with a success message.
//
// The defect: the dispatcher read args["newStr"] as "" whenever the caller sent
// its replacement under another key (the raw log of the measuring session
// recorded 6 of 17 edit calls using "newText"), and then called
// tools.Edit(path, oldStr, "", …) — a DELETION — and returned "Successfully
// edited <path>". The matched line was destroyed and nothing reported it.
func TestExecuteTool_EditRefusesAnEmptyReplacement(t *testing.T) {
	ag := &Agent{}

	t.Run("empty newStr", func(t *testing.T) {
		path := writeEditRefusalFixture(t, editRefusalFixtureContent)
		res := ag.executeTool(deepseek.ToolCall{
			Name: toolnames.ToolEdit,
			Arguments: map[string]interface{}{
				toolnames.ToolArgPath:   path,
				toolnames.ToolArgOldStr: "| anchor-row | keep me |",
				toolnames.ToolArgNewStr: "",
			},
		})
		assertEditRefused(t, res, path)
	})

	t.Run("replacement sent under the misspelled key", func(t *testing.T) {
		path := writeEditRefusalFixture(t, editRefusalFixtureContent)
		res := ag.executeTool(deepseek.ToolCall{
			Name: toolnames.ToolEdit,
			Arguments: map[string]interface{}{
				toolnames.ToolArgPath:               path,
				toolnames.ToolArgOldStr:             "| anchor-row | keep me |",
				toolnames.ToolArgNewTextMisspelling: "| NEW-ROW | added |",
			},
		})
		assertEditRefused(t, res, path)
		// The refusal names BOTH keys: what the caller typed, and the schema's.
		for _, want := range []string{toolnames.ToolArgNewTextMisspelling, toolnames.ToolArgNewStr} {
			if !strings.Contains(res, want) {
				t.Errorf("refusal does not name %q: %s", want, res)
			}
		}
	})
}

// TestExecuteTool_EditMultilineReplacementKeepsTheAnchorLine is the positive
// half — the minimal regression sample from the defect report: a single-line
// oldStr with a MULTI-LINE newStr must land the new content AND keep the matched
// line, so the file grows by exactly one line.
func TestExecuteTool_EditMultilineReplacementKeepsTheAnchorLine(t *testing.T) {
	path := writeEditRefusalFixture(t, editRefusalFixtureContent)
	ag := &Agent{}

	res := ag.executeTool(deepseek.ToolCall{
		Name: toolnames.ToolEdit,
		Arguments: map[string]interface{}{
			toolnames.ToolArgPath:   path,
			toolnames.ToolArgOldStr: "| anchor-row | keep me |",
			toolnames.ToolArgNewStr: "| NEW-ROW | added |\n| anchor-row | keep me |",
		},
	})
	if !strings.Contains(res, "Successfully edited") {
		t.Fatalf("the correct shape was refused: %s", res)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// The file must GROW by exactly one line: the matched line survives and the
	// new one is inserted above it. An exact-string compare is the assertion —
	// "looks right" is what the defect looked like.
	want := "line-A\n| NEW-ROW | added |\n| anchor-row | keep me |\nline-C\n"
	if string(got) != want {
		t.Errorf("edited file =\n%q\nwant\n%q", got, want)
	}
}

// assertEditRefused fails unless res is a refusal that leaves the file exactly
// as it was — the two facts an empty replacement must produce together.
func assertEditRefused(t *testing.T, res, path string) {
	t.Helper()
	if strings.Contains(res, "Successfully edited") {
		t.Fatalf("an empty replacement reported success: %s", res)
	}
	if !strings.Contains(res, "refused") {
		t.Errorf("the result does not say the call was refused: %s", res)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != editRefusalFixtureContent {
		t.Errorf("a refused edit changed the file:\n%q\nwant\n%q", got, editRefusalFixtureContent)
	}
}
