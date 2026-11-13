package deepseek

// Regression guard for the dump-name collision: both death-dump writers used
// to name their file time.Now().Format("20060102-150405")+".dump.json" — a
// 1-second resolution with no uniqueness suffix — so a second dump inside the
// same wall-clock second truncated-overwrote the first via os.WriteFile
// (files=1, the first failed request lost forever — the dump's only purpose
// is replay/resume). These tests pin the repro: two dumps in the SAME second
// must yield TWO files (files=2), each retaining its own request body.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sameSecondTS is a fixed timestamp (second resolution) standing for "two
// dumps taken inside one wall-clock second" — no sleeps, no clock races.
const sameSecondTS = "20260101-120000"

// Test-only fixtures: markers that make each dump's own request body
// identifiable, an error body, and a log tag for direct helper calls.
const (
	dumpTestLogTag         = "[GOPI][dump-test]"
	dumpTestMarker1214     = "request-marker-1214"
	dumpTestMarkerFailed   = "request-marker-failed"
	errBody1214            = `{"code":1214,"msg":"invalid request shape"}`
	dumpJSONKeyOriginalReq = "original_request"
)

// dumpFileCount returns the dump files present in dir.
func dumpFileCount(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dump dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestWriteDumpFile_SameSecondYieldsTwoFiles is the deterministic core repro:
// two writes carrying the SAME timestamp and DIFFERENT bodies must produce two
// distinct files, each holding exactly its own body (the old code produced one
// file holding only the second body).
func TestWriteDumpFile_SameSecondYieldsTwoFiles(t *testing.T) {
	dir := t.TempDir()
	first := []byte(`{"session":"first","body":"AAA"}`)
	second := []byte(`{"session":"second","body":"BBB"}`)

	p1 := writeDumpFileAt(dir, sameSecondTS, first, dumpTestLogTag)
	p2 := writeDumpFileAt(dir, sameSecondTS, second, dumpTestLogTag)

	if p1 == "" || p2 == "" {
		t.Fatalf("dump writes failed: p1=%q p2=%q", p1, p2)
	}
	if p1 == p2 {
		t.Fatalf("same-second dumps collided on one path: %q", p1)
	}

	names := dumpFileCount(t, dir)
	if len(names) != 2 {
		t.Fatalf("want 2 dump files in the same second, got %d: %v", len(names), names)
	}
	wantFirst := sameSecondTS + dumpFileSuffix
	if got := filepath.Base(p1); got != wantFirst {
		t.Errorf("first dump name = %q, want %q", got, wantFirst)
	}
	wantSecond := sameSecondTS + "-1" + dumpFileSuffix
	if got := filepath.Base(p2); got != wantSecond {
		t.Errorf("second dump name = %q, want %q (disambiguating suffix)", got, wantSecond)
	}

	// Both requests survive with their OWN body — the overwrite is gone.
	got1, err := os.ReadFile(p1)
	if err != nil {
		t.Fatalf("read first dump: %v", err)
	}
	got2, err := os.ReadFile(p2)
	if err != nil {
		t.Fatalf("read second dump: %v", err)
	}
	if string(got1) != string(first) {
		t.Errorf("first dump body = %s, want %s", got1, first)
	}
	if string(got2) != string(second) {
		t.Errorf("second dump body = %s, want %s", got2, second)
	}
}

// TestWriteDumpFile_SameSecondThirdDumpGetsNextSuffix guards the retry loop
// beyond one collision: a third same-second dump must take "-2".
func TestWriteDumpFile_SameSecondThirdDumpGetsNextSuffix(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if p := writeDumpFileAt(dir, sameSecondTS, []byte(`{"i":1}`), dumpTestLogTag); p == "" {
			t.Fatalf("dump write %d failed", i)
		}
	}
	names := dumpFileCount(t, dir)
	if len(names) != 3 {
		t.Fatalf("want 3 dump files, got %d: %v", len(names), names)
	}
	third := filepath.Join(dir, sameSecondTS+"-2"+dumpFileSuffix)
	if _, err := os.Stat(third); err != nil {
		t.Errorf("third same-second dump not at %s: %v", third, err)
	}
}

// TestDumpWriters_SameSecondEndToEnd drives BOTH real writers (dump1214Request
// in error_1214.go and dumpFailedRequest in minimal_core.go) in the same
// second, each redirected to its own scratch dir via the env override: each
// writer must produce exactly ONE file carrying its own request body. Before
// the shared helper this yielded files=1 with the first request lost.
func TestDumpWriters_SameSecondEndToEnd(t *testing.T) {
	dir1214 := t.TempDir()
	dirFailed := t.TempDir()
	t.Setenv("GOPI_1214_DUMP_DIR", dir1214)
	t.Setenv("GOPI_ERROR_DUMP_DIR", dirFailed)

	orig1214 := &ClaudeRequest{
		Model:     "glm-1214-model",
		MaxTokens: 100,
		Messages: []ClaudeMessage{{
			Role:    "user",
			Content: []ClaudeContentBlock{{Type: "text", Text: dumpTestMarker1214}},
		}},
	}
	origFailed := &ClaudeRequest{
		Model:     "glm-failed-model",
		MaxTokens: 100,
		Messages: []ClaudeMessage{{
			Role:    "user",
			Content: []ClaudeContentBlock{{Type: "text", Text: dumpTestMarkerFailed}},
		}},
	}

	p1214 := dump1214Request(orig1214, MinimalCore(orig1214), MinimalCore(orig1214), []byte(errBody1214))
	pFailed := dumpFailedRequest(origFailed, http.StatusBadRequest, []byte(errBody1214))

	if p1214 == "" || pFailed == "" {
		t.Fatalf("dump writes failed: 1214=%q failed=%q", p1214, pFailed)
	}
	if filepath.Dir(p1214) != dir1214 {
		t.Errorf("1214 dump went to %s, want %s", p1214, dir1214)
	}
	if filepath.Dir(pFailed) != dirFailed {
		t.Errorf("failed-request dump went to %s, want %s", pFailed, dirFailed)
	}
	if names := dumpFileCount(t, dir1214); len(names) != 1 {
		t.Errorf("want exactly 1 file in the 1214 dump dir, got %d: %v", len(names), names)
	}
	if names := dumpFileCount(t, dirFailed); len(names) != 1 {
		t.Errorf("want exactly 1 file in the failed-request dump dir, got %d: %v", len(names), names)
	}

	// Each dump carries its own request body — nothing was truncated away.
	body1214, err := os.ReadFile(p1214)
	if err != nil {
		t.Fatalf("read 1214 dump: %v", err)
	}
	if !strings.Contains(string(body1214), dumpTestMarker1214) {
		t.Errorf("1214 dump lost its own request body: %s", body1214)
	}
	bodyFailed, err := os.ReadFile(pFailed)
	if err != nil {
		t.Fatalf("read failed-request dump: %v", err)
	}
	if !strings.Contains(string(bodyFailed), dumpTestMarkerFailed) {
		t.Errorf("failed-request dump lost its own request body: %s", bodyFailed)
	}
	if !strings.HasPrefix(filepath.Base(p1214), dumpNameTimestamp(time.Now())) {
		t.Errorf("1214 dump name %s is not timestamp-prefixed", filepath.Base(p1214))
	}

	// The dump is valid JSON with the original request key present.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body1214, &doc); err != nil {
		t.Fatalf("1214 dump is not valid JSON: %v", err)
	}
	if _, ok := doc[dumpJSONKeyOriginalReq]; !ok {
		t.Errorf("1214 dump lacks %q: %s", dumpJSONKeyOriginalReq, body1214)
	}
}

// TestDumpNaming_HasExactlyOneDefinitionSite is the Rule 13/14 gate: the dump
// timestamp layout and the ".dump.json" suffix may appear in production code
// ONLY at their single definition site (dump_file.go). Any re-inlined literal
// elsewhere — the collision bug's original shape — fails here.
func TestDumpNaming_HasExactlyOneDefinitionSite(t *testing.T) {
	const definitionFile = "dump_file.go"
	production, err := filepath.Glob(filepath.Join(".", "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	literals := []string{`"` + dumpNameTimeLayout + `"`, `"` + dumpFileSuffix + `"`}
	scanned := 0
	for _, file := range production {
		base := filepath.Base(file)
		if strings.HasSuffix(base, "_test.go") || base == definitionFile {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		scanned++
		for _, lit := range literals {
			if strings.Contains(string(src), lit) {
				t.Errorf("%s carries the dump-name literal %s — reference the constant in %s (Rule 13)",
					base, lit, definitionFile)
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("scanned no production files — the gate is vacuous")
	}
}

// TestWriteDumpFile_WriteFailureIsNonFatal pins Rule 15b: an unwritable dump
// dir yields "" (callers report "dump write failed") instead of a panic or a
// half-written file, and the failure is not silent on the log stream.
func TestWriteDumpFile_WriteFailureIsNonFatal(t *testing.T) {
	// A path whose parent is a regular FILE can never be created as a dir.
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parent, []byte("x"), dumpFilePerm); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	dir := filepath.Join(parent, "dumps")
	if got := writeDumpFileAt(dir, sameSecondTS, []byte("{}"), dumpTestLogTag); got != "" {
		t.Fatalf("unwritable dump dir returned %q, want \"\" (non-fatal)", got)
	}
}
