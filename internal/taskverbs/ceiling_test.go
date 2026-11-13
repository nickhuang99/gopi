package taskverbs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// taskUsageFile / taskUsageDecl are where the `gopi task` help page physically
// lives (Rule 14: one definition, referenced by name — never a literal path
// re-typed across the tree). The page is a raw string constant in the family
// dispatcher's file; this test reads THAT constant, not a copy of it.
const (
	taskUsageFile = "../../cmd/agent/cli_tasks.go"
	taskUsageDecl = "const tasksUsage = `"
)

// taskUsageTerminator ends the raw string literal that carries the help page.
const taskUsageTerminator = "`"

// taskHelpEntryRe matches one `gopi task <verb>` entry line the family help
// renders — the synopsis form `  gopi task show …` and the sync table's
// `  send       gopi task send …` form both carry it, which is why the gate
// keys on the invocation substring rather than on a column.
var taskHelpEntryRe = regexp.MustCompile(`gopi task ([a-z][a-z-]*)`)

// taskHelpPage extracts the raw-string help constant by REGION, not by a
// substring search: the opening line is known exactly, so the page runs from it
// to the first line consisting of nothing but the terminator. An extraction
// that found nothing is a failure of the gate, never a green run.
func taskHelpPage(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(taskUsageFile)
	if err != nil {
		t.Fatalf("read the family help source %s: %v", taskUsageFile, err)
	}
	start := strings.Index(string(src), taskUsageDecl)
	if start < 0 {
		t.Fatalf("%s carries no %q declaration — the gate would be vacuous", taskUsageFile, taskUsageDecl)
	}
	body := string(src)[start+len(taskUsageDecl):]
	end := strings.Index(body, "\n"+taskUsageTerminator+"\n")
	if end < 0 {
		t.Fatalf("%s: the %q literal is not terminated as expected — refusing to scan a guess", taskUsageFile, taskUsageDecl)
	}
	return body[:end]
}

// TestTaskVerbCeiling is THE gate on the `gopi task` verb surface — one gate,
// in the package that owns the vocabulary:
//
//   - the advertised surface (FamilyVerbs ∪ SyncVerbs) never exceeds
//     MaxTaskVerbs, so it cannot grow back one convenient verb at a time;
//   - every advertised verb is REACHABLE: `gopi task <verb>` appears on the
//     family help page, so a verb added to the vocabulary without teaching it
//     fails here rather than silently becoming a spelling nobody can find.
//
// It replaces the 3670-line per-form help-surface case table: the claim those
// cases pinned in 90 hand-written forms is the same claim one line of counting
// makes here.
func TestTaskVerbCeiling(t *testing.T) {
	advertised := append(append([]string{}, FamilyVerbs...), SyncVerbs...)
	if n := len(advertised); n > MaxTaskVerbs {
		t.Fatalf("the advertised `gopi task` surface is %d verbs, ceiling MaxTaskVerbs=%d — "+
			"either lower the surface or raise the ceiling in a commit that says why "+
			"(FamilyVerbs=%d, SyncVerbs=%d)", n, MaxTaskVerbs, len(FamilyVerbs), len(SyncVerbs))
	}

	page := taskHelpPage(t)
	documented := map[string]bool{}
	for _, m := range taskHelpEntryRe.FindAllStringSubmatch(page, -1) {
		documented[m[1]] = true
	}
	for _, verb := range advertised {
		if !documented[verb] {
			t.Errorf("`%s` is in FamilyVerbs/SyncVerbs but the `gopi task` help page carries no "+
				"`gopi task %s` entry — an advertised verb nobody can find is the drift this gate exists for",
				verb, verb)
		}
	}
	t.Logf("advertised=%d ceiling=%d help entries=%d", len(advertised), MaxTaskVerbs, len(documented))
}
