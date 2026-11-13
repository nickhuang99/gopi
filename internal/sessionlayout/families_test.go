// families_test.go — the family registry's own contracts, in the package that
// owns it.
//
// THREE ASSERTIONS, deliberately separated:
//
//  1. DRIFT (TestFamilyNamesMatchWriters): every name this registry re-homes
//     from a non-leaf writer (the attachments area, the metadata record, the
//     profile directory) EQUALS the writer's own constant, read out of the
//     writer's SOURCE. The registry cannot import internal/tools or
//     internal/agent (neither is a leaf), so the two spellings are held
//     together here — the same shape this package already uses for
//     signalrec.FileName in TestSideFileNamesMatchWriters.
//  2. SHAPE (TestFamilyRegistryShape): every row carries a decision, a reason,
//     and — for a `bound:` row — a tree name; and a `bound:` row is the ONLY
//     kind that names a tree. A row that breaks either direction is a decision
//     nobody can read.
//  3. THE DECISION TABLE (TestClassifyFamilyCoversAMeasuredTree): the family
//     each measured entry of a real-shaped fixture tree lands on, so a change
//     to the match ORDER that moved a family silently would fail here.
package sessionlayout_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/sessionlayout"
)

// repoFile reads one repository file, located from this test's own working
// directory (the package directory), and fails when it is missing: a missing
// writer means the pin has rotted, which must never read as "no drift".
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// TestFamilyNamesMatchWriters pins every re-homed name to its production
// writer. The value is read as a CONSTANT ASSIGNMENT (`name = "value"`) in the
// writer's file: a writer that reformats its constant still passes, while a
// writer whose VALUE changed fails — which is the drift this guards.
func TestFamilyNamesMatchWriters(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		rel      string
		declared string // the writer's own constant declaration
	}{
		{
			name:     "attachments area",
			value:    sessionlayout.AttachmentsDirName,
			rel:      "internal/tools/task_session.go",
			declared: "taskAttachmentsDirName",
		},
		{
			name:     "metadata record",
			value:    sessionlayout.MetadataFileName,
			rel:      "internal/tools/task_session.go",
			declared: "taskSubmitMetadataFileName",
		},
		{
			name:     "metadata record (agent's own spelling)",
			value:    sessionlayout.MetadataFileName,
			rel:      "internal/agent/session.go",
			declared: `"metadata.json"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := repoFile(t, tc.rel)
			if !strings.Contains(body, tc.declared) {
				t.Fatalf("%s no longer declares %s — re-point this pin at the writer's own constant", tc.rel, tc.declared)
			}
			want := tc.declared + ` = "` + tc.value + `"`
			if strings.HasPrefix(tc.declared, `"`) {
				// A bare literal writer (no named constant): the pin is the
				// literal itself, and the value must appear as a quoted string.
				want = tc.declared
			}
			if !strings.Contains(body, want) {
				t.Fatalf("family value drift: the registry spells %q while %s declares %q — one of the two moved (Rules 13/14): registry %s",
					tc.value, tc.rel, tc.declared, sessionlayout.AttachmentsDirName)
			}
		})
	}
}

// TestFamilyRegistryShape pins what a row must carry to be readable, and the
// two-way rule about the Tree field.
func TestFamilyRegistryShape(t *testing.T) {
	fams := sessionlayout.Families()
	if len(fams) == 0 {
		t.Fatal("the family registry is empty — every family would read as unclassified")
	}
	seen := map[string]bool{}
	for _, f := range fams {
		if f.Name == "" || f.Class == "" || f.Where == "" {
			t.Errorf("family %+v: Name/Class/Where are all read by the report and the gate", f)
		}
		if seen[f.Name] {
			t.Errorf("family name %q appears twice — a report line could not be attributed", f.Name)
		}
		seen[f.Name] = true
		if len(f.Reason) < familyReasonMinLen {
			t.Errorf("family %q carries a %d-character reason — a decision without a readable WHY is not a decision (%q)",
				f.Name, len(f.Reason), f.Reason)
		}
		switch f.Decision {
		case sessionlayout.DecisionBound:
			if f.Tree == "" {
				t.Errorf("family %q is `bound:` but names no tree — the cross-check could not tell which tree bounds it", f.Name)
			}
		case sessionlayout.DecisionKeep, sessionlayout.DecisionLegacy:
			if f.Tree != "" {
				t.Errorf("family %q is %q but names tree %q — a tree is only meaningful for a `bound:` family (a reader would take it for a declaration that does not exist)",
					f.Name, f.Decision, f.Tree)
			}
		default:
			t.Errorf("family %q carries decision %q, which is not one of bound/keep/legacy", f.Name, f.Decision)
		}
		if f.Match == nil {
			t.Errorf("family %q has no membership test — it could never claim an entry", f.Name)
		}
	}
	if len(sessionlayout.BoundTreeNames()) == 0 {
		t.Error("no family is `bound:` — the sessions tree's ONE bounded family (*.raw.log) has gone missing")
	}
}

// familyReasonMinLen is the shortest acceptable decision reason. A one-word
// reason is what a hasty classification looks like, and the reason is the only
// thing a later reader has to judge the decision by.
const familyReasonMinLen = 40

// TestClassifyFamilyCoversAMeasuredTree drives the classifier over the SHAPES
// measured in the live tree (2026-10-01: 79,401 metadata.json, 75,843
// attachments dirs, …), plus the families the epic names. It pins the family
// each shape lands on, so a match-order change that quietly reclassified a
// family fails here instead of in a census nobody runs.
func TestClassifyFamilyCoversAMeasuredTree(t *testing.T) {
	const (
		uuid     = "01a0f4f9-baf5-7f13-a707-d05a68ea12e4"
		hexName  = "01a0e67f7eb3788ab13dddba2ab5fc37"
		taskName = "t-1788897004"
	)
	fileCases := []struct {
		where sessionlayout.Where
		name  string
		want  string
	}{
		{sessionlayout.WhereContainer, "session.jsonl", sessionlayout.FamilySessionRecord},
		{sessionlayout.WhereContainer, uuid + ".jsonl", sessionlayout.FamilySessionRecord},
		{sessionlayout.WhereContainer, uuid + ".raw.log", sessionlayout.FamilyRawHTTPLog},
		{sessionlayout.WhereContainer, "signal-provenance.jsonl", sessionlayout.FamilySideRecord},
		{sessionlayout.WhereContainer, uuid + ".dump.json", sessionlayout.FamilyCrashDump},
		{sessionlayout.WhereContainer, "session.dump.json", sessionlayout.FamilyCrashDump},
		{sessionlayout.WhereContainer, sessionlayout.MetadataFileName, sessionlayout.FamilySessionMetadata},
		{sessionlayout.WhereContainer, "task-report.md", sessionlayout.FamilyRunEvidence},
		{sessionlayout.WhereContainer, "output.log", sessionlayout.FamilyRunEvidence},
		{sessionlayout.WhereContainer, "console.log", sessionlayout.FamilyRunEvidence},
		{sessionlayout.WhereContainer, "chat-screen.dump", sessionlayout.FamilyRunEvidence},
		// A new *.jsonl in a container that no family claims: it stays a
		// registered family (run evidence) — the registry claims ALL container
		// files, which is exactly why a new family shows up at the ROOT or as a
		// new directory shape, and why the gate's fixture tests both.
		{sessionlayout.WhereContainer, "brand-new-artifact.json", sessionlayout.FamilyRunEvidence},
		{sessionlayout.WhereRoot, uuid + ".dump.json", sessionlayout.FamilySessionsRootResidue},
		{sessionlayout.WhereRoot, "note.txt", sessionlayout.FamilySessionsRootResidue},
		{sessionlayout.WhereRoot, ".migration-manifest.json", sessionlayout.FamilySessionsRootResidue},
	}
	for _, tc := range fileCases {
		got, ok := lookupName(t, tc.where, tc.name)
		if !ok {
			t.Errorf("%q at %s is UNCLASSIFIED — the family that owns it went missing", tc.name, tc.where)
			continue
		}
		if got != tc.want {
			t.Errorf("%q at %s classified as %q, want %q", tc.name, tc.where, got, tc.want)
		}
	}

	dirCases := []struct {
		where sessionlayout.Where
		name  string
		kids  []string
		want  string
	}{
		{sessionlayout.WhereContainer, "attachments", nil, sessionlayout.FamilyAttachmentsDir},
		{sessionlayout.WhereContainer, "evidence", nil, sessionlayout.FamilySessionScratchDir},
		{sessionlayout.WhereContainer, "smoke-clone", []string{".git"}, sessionlayout.FamilyForeignCheckout},
		{sessionlayout.WhereContainer, "master-check", []string{".git", "cmd"}, sessionlayout.FamilyForeignCheckout},
		{sessionlayout.WhereContainer, "sandbox-home", []string{".gopi"}, sessionlayout.FamilySandboxProfile},
		{sessionlayout.WhereRoot, uuid, nil, sessionlayout.FamilySessionContainer},
		// A BARE 32-HEX RUN IS NOT A SESSION CONTAINER (measured 2026-10-17,
		// task 01a144001e4b7086b97d257497a832ed). It is the branch/worktree stem
		// a run types when it mis-transcribes its own row id, so the directory it
		// mints is a SECOND one beside the row's own — 25 of 25 on the measured
		// host had a canonical twin, one of them written 40 hours later. It lands
		// on the task-container family, whose own doc already says so ("a 32-hex
		// row id"): the name is a run's id, not a session uuid.
		{sessionlayout.WhereRoot, hexName, nil, sessionlayout.FamilyTaskContainer},
		{sessionlayout.WhereRoot, taskName, nil, sessionlayout.FamilyTaskContainer},
		{sessionlayout.WhereRoot, "process-12345", nil, sessionlayout.FamilyTaskContainer},
	}
	for _, tc := range dirCases {
		got, ok := lookupDir(t, tc.where, tc.name, tc.kids)
		if !ok {
			t.Errorf("directory %q at %s is UNCLASSIFIED — the family that owns it went missing", tc.name, tc.where)
			continue
		}
		if got != tc.want {
			t.Errorf("directory %q at %s classified as %q, want %q", tc.name, tc.where, got, tc.want)
		}
	}
}

// lookupName builds an Entry for a FILE with that name and returns the family
// name that claims it.
func lookupName(t *testing.T, where sessionlayout.Where, name string) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	f, ok := sessionlayout.ClassifyFamily(sessionlayout.FamilyEntryFor(path, false, where))
	return f.Name, ok
}

// lookupDir builds an Entry for a DIRECTORY with that name (and the given
// children, which the checkout/profile families test) and returns the family
// name that claims it.
func lookupDir(t *testing.T, where sessionlayout.Where, name string, kids []string) (string, bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	for _, kid := range kids {
		if err := os.MkdirAll(filepath.Join(path, kid), 0o755); err != nil {
			t.Fatalf("fixture child %s: %v", kid, err)
		}
	}
	f, ok := sessionlayout.ClassifyFamily(sessionlayout.FamilyEntryFor(path, true, where))
	return f.Name, ok
}
