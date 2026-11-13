package standardgate

// sandbox_settings_doc_gate_test.go — the red proof and the real-tree half of
// the Rule 34 shape gate.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSandboxSettingsDocGateRedProof proves the predicate reports the sentence
// that actually shipped wrong, and stays silent on the shapes a document is
// entitled to write.
func TestSandboxSettingsDocGateRedProof(t *testing.T) {
	// RED 1 — THE PRE-FIX SENTENCE, verbatim from PROJECT.md Rule 34 as it stood at
	// baseline 2c56271 (wrapped exactly as the file wrapped it). This is the
	// text the gate exists for: if the predicate cannot report THIS, the gate is
	// vacuous.
	assertSandboxDocReports(t, "probe.md",
		"`settings.json` — a **symlink**\n"+
			"to the REAL settings file, **read-only by contract**\n",
		1, "LINK")

	// RED 2 — the same claim without the emphasis, and with the em-dash on the
	// same line as the kind: a doc that reformats the sentence must not escape.
	assertSandboxDocReports(t, "probe.md",
		"`settings.json` — a symlink to the operator's document\n",
		1, "LINK")

	// RED 3 — a shape assertion that names the copy but NOT its mode: "a
	// read-only copy" is the same claim one refactor away from drift, so the
	// mode is part of the assertion.
	assertSandboxDocReports(t, "probe.md",
		"`settings.json` — a read-only copy of the operator's document\n",
		1, sandboxSettingsDocModeToken)

	// RED 4 — the same two failures inside a PRODUCTION GO comment: the help
	// pages and the package docs carry the shape too (cmd/agent/cli_sandbox.go,
	// internal/sandbox/sandbox.go), so the corpus is not markdown only.
	assertSandboxDocReports(t, "probe.go",
		"package p\n\n// `settings.json` — a **symlink** to the real file\nfunc f() {}\n",
		3, "LINK")

	// GREEN 1 — the SHIPPED shape: a 0444 copy, named with its mode.
	assertSandboxDocSilent(t, "probe.md",
		"`settings.json` — a **read-only COPY (mode `0444`) of the operator's real settings\n"+
			"document, NEVER a link** (measured, 2026-09-27).\n")

	// GREEN 2 — the REFUSAL form and the HISTORICAL form: a document must be able
	// to say what it replaced and why ("the entry used to be a symlink"), or the
	// record of the incident cannot be written at all.
	assertSandboxDocSilent(t, "probe.md",
		"The entry used to be a symlink to the real document; it is now a 0444 copy.\n")
	assertSandboxDocSilent(t, "probe.md",
		"`settings.json` — a **read-only COPY (mode `0444`)**, never a **symlink** again.\n")

	// GREEN 3 — a line that MENTIONS the entry without asserting its kind is prose,
	// not a shape assertion: the ledger's own explanation, the resolution chain,
	// and a sentence naming the plugins link all read like this.
	assertSandboxDocSilent(t, "probe.md",
		"3. `<gopi data root>/settings.json` — i.e. `$GOPI_HOME/.gopi/settings.json`,\n"+
			"the same failure settingsDocSource refuses for the settings copy.\n")
	assertSandboxDocSilent(t, "probe.md",
		"`plugins` — a **symlink** to the REAL plugin tree, read-only by contract\n")

	// GREEN 4 — the EM-DASH is the shape form, so a symlink claim written where
	// the entry token does not precede it (a link described in the ledger's
	// paragraph) is not this gate's subject.
	assertSandboxDocSilent(t, "probe.md",
		"a writer wrote a sibling and renamed it into place, replacing the symlink node\n")

	// The ESCAPE is applied by the classifier, not the predicate: a line whose
	// SUBJECT is the refused shape is still REPORTED and silenced visibily.
	escaped := "`settings.json` — a **symlink** " + sandboxSettingsDocEscape + " quoting the pre-fix shape\n"
	got := sandboxSettingsDocFind(gate{}, &parsed{rel: "probe.md", src: []byte(escaped)})
	if len(got) != 1 {
		t.Fatalf("the escape fixture must still be REPORTED by the predicate — the escape is applied by "+
			"the classifier so it stays visible in the data report: %+v", got)
	}
	if !markerOn(linesAbove([]byte(escaped), got[0].line), sandboxSettingsDocEscape) {
		t.Fatalf("the inline escape %q did not silence the reported line", sandboxSettingsDocEscape)
	}
}

// TestSandboxSettingsDocGateScansTheRealTree is the non-vacuity half: the gate
// must read the real surfaces, find them clean, and find the shape assertion
// itself present in GOPI.md's sandbox paragraph (a rule file that stopped
// stating the shape at all would pass a "no link claim" check by saying
// nothing).
func TestSandboxSettingsDocGateScansTheRealTree(t *testing.T) {
	root := repoRoot(t)
	findings, files := sandboxSettingsDocScanWithFloor(t, root)
	if files < sandboxSettingsDocMinFiles {
		t.Fatalf("the corpus read only %d file(s) (floor %d) — a mis-specified root or corpus "+
			"reports a clean green while reading nothing", files, sandboxSettingsDocMinFiles)
	}
	for _, h := range findings {
		t.Errorf("%s:%d disagrees with the shipped sandbox profile shape\n  %s\n"+
			"  fix the sentence (internal/sandbox/settings_doc.go is the behaviour of record), or mark "+
			"a line whose SUBJECT is the pre-fix shape with %q and a reason",
			h.rel, h.line, h.desc, sandboxSettingsDocEscape)
	}

	doc, err := os.ReadFile(filepath.Join(root, sandboxDocInstructionRel))
	if err != nil {
		t.Fatalf("read %s: %v — the rule file is the surface this gate covers", sandboxDocInstructionRel, err)
	}
	para := sandboxSettingsDocShapeParagraph(string(doc))
	if para == "" {
		t.Fatalf("%s no longer carries the sandbox-shape paragraph (anchor %q) — the instruction lost "+
			"the contents list this gate keeps honest", sandboxDocInstructionRel, sandboxSettingsDocShapeAnchor)
	}
	if !strings.Contains(para, sandboxSettingsDocModeToken) {
		t.Fatalf("%s's sandbox-shape paragraph does not name the mode %s of the settings copy — the rule "+
			"file must state the shipped shape, not merely avoid the wrong one:\n%s",
			sandboxDocInstructionRel, sandboxSettingsDocModeToken, para)
	}
	if !strings.Contains(para, "NEVER a link") {
		t.Fatalf("%s's sandbox-shape paragraph does not refuse the link spelling over the settings entry — "+
			"a reader of the rule file must learn WHY the entry is not a link:\n%s",
			sandboxDocInstructionRel, para)
	}
}

// sandboxDocInstructionRel is the agent-facing rule file the shape paragraph is
// read from.
const sandboxDocInstructionRel = ProjectDocName

// sandboxSettingsDocScanWithFloor runs the real-tree scan and reports how many
// files the corpus admitted (the non-vacuity figure).
func sandboxSettingsDocScanWithFloor(t *testing.T, root string) (findings []hit, files int) {
	t.Helper()
	for _, rel := range textFilesUnder(t, root, sandboxSettingsDocScanRoot) {
		if !sandboxSettingsDocCorpus(rel) {
			continue
		}
		files++
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		findings = append(findings, sandboxSettingsDocFind(gate{}, &parsed{rel: rel, src: src})...)
	}
	return findings, files
}

// assertSandboxDocReports fails when the planted text is NOT reported on the
// expected line, or when the report does not name what the reader must fix.
func assertSandboxDocReports(t *testing.T, name, src string, wantLine int, wantWord string) {
	t.Helper()
	got := sandboxSettingsDocFind(gate{}, &parsed{rel: name, src: []byte(src)})
	if len(got) == 0 {
		t.Fatalf("RED PROOF FAILED for %s: the planted sentence was not reported — the gate is vacuous\n  source: %s",
			name, src)
	}
	if got[0].line != wantLine {
		t.Fatalf("RED PROOF for %s: reported line %d, want %d (the line carrying the shape claim)",
			name, got[0].line, wantLine)
	}
	if !strings.Contains(got[0].desc, wantWord) {
		t.Fatalf("RED PROOF for %s: reported %q, which does not name %q", name, got[0].desc, wantWord)
	}
}

// assertSandboxDocSilent fails when a text that must NOT be reported is.
func assertSandboxDocSilent(t *testing.T, name, src string) {
	t.Helper()
	if got := sandboxSettingsDocFind(gate{}, &parsed{rel: name, src: []byte(src)}); len(got) != 0 {
		t.Fatalf("FALSE POSITIVE in %s: %+v\n  source: %s", name, got, src)
	}
}
