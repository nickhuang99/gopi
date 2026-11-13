package tools

// Regressions for the two adjudications in task 01a0d625-095e-7a1d-a581-
// 828076248623 (epic: child-process + provenance fixtures). Both defects are of
// the same family — a fixture pinning a value that production had already
// stopped producing — and both are invisible to a test that only checks the
// happy path, because in each case the STALE expectation reads as "close
// enough". Each test below asserts the invariant the producer actually
// guarantees, so a future change to either side fails here by name.
//
//	A. provenance: the submit skeleton's source is the CANONICAL ledger value
//	   (internal/tasksource), never a retired alias and never the raw caller
//	   spelling (be17a9bd retired `auto`/`llm` as stored values).
//	B. identity:   the "#" column renders the id VERBATIM for every id form,
//	   including the legacy `t-<unix>` rows (c7de4ec8 retired truncation).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/tasksource"
)

// retiredAliases are the spellings the ledger still carries but must never be
// WRITTEN: `auto` (whose one recorded meaning is merger) and `llm` (whose one
// meaning is agent). Spelled here as data, not as a caller argument, because
// that is exactly what they are — history, not vocabulary.
var retiredAliases = []struct{ alias, canonical string }{
	{"auto", tasksource.Merger},
	{"llm", tasksource.Agent},
}

// TestSubmitMetadataSourceIsCanonicalNotTheCallerSpelling is the regression for
// A. The fixture that broke (TestUuidTaskIDSubmitCreatesSessionContainer)
// asserted `meta["source"] == "auto"` — the CALLER's spelling, and a retired
// alias. internal/tasksource.Normalize resolves `auto` to its one recorded
// meaning, merger, and TaskSubmit stores that; the row (the truth, read back
// from the ledger) and the container's metadata mirror (the file the fixture
// asserted on) must therefore agree on the same canonical value, and neither
// may carry an alias — a ledger holding three spellings for one role is the
// defect the canonicalization exists to close.
func TestSubmitMetadataSourceIsCanonicalNotTheCallerSpelling(t *testing.T) {
	for _, tc := range retiredAliases {
		t.Run("source="+tc.alias, func(t *testing.T) {
			home := phase1AttachEnv(t)
			out, err := TaskSubmit(TaskSubmitOptions{
				Summary: "provenance canonicalization",
				Payload: "body",
				Source:  tc.alias,
			})
			if err != nil {
				t.Fatalf("TaskSubmit(source=%q): %v", tc.alias, err)
			}
			taskID := extractTaskID(t, out)

			row := phase1TaskRow(t, taskID)
			if row.Source != tc.canonical {
				t.Errorf("ledger source = %q, want %q (the canonical value the caller's %q means)",
					row.Source, tc.canonical, tc.alias)
			}
			if value, ok := tasksource.Normalize(row.Source); !ok || value != row.Source {
				t.Errorf("ledger source %q is not canonical vocabulary (%s → %q, accepted=%v) — a row "+
					"written today never carries a retired alias", row.Source, tasksource.Policy(), value, ok)
			}

			metaB, err := os.ReadFile(filepath.Join(home, ".gopi", "sessions", taskID, "metadata.json"))
			if err != nil {
				t.Fatalf("submit skeleton metadata.json missing: %v", err)
			}
			var meta map[string]interface{}
			if err := json.Unmarshal(metaB, &meta); err != nil {
				t.Fatalf("metadata.json corrupt: %v", err)
			}
			metaSource, _ := meta["source"].(string)
			if metaSource != row.Source {
				t.Errorf("metadata source = %q, ledger source = %q — the container mirror must agree "+
					"with the row it belongs to", metaSource, row.Source)
			}
		})
	}
}

// TestSubmitDerivesProvenanceWhenTheCallerStatesNone is the OTHER half of the
// same rule, and the one a canonicalization-only fix would miss: a caller that
// states NO source gets this process's role, MEASURED (tasksource.DeriveNow
// over the spawn signature), not a hardcoded default. The assertion is the
// invariant — the stored value is ledger vocabulary and is identical in the row
// and the container mirror — since which role a machine session holds is
// evidence-based by design (tasksource's package doc: the signature proves the
// absence of an agent chain, never the presence of a person).
func TestSubmitDerivesProvenanceWhenTheCallerStatesNone(t *testing.T) {
	home := phase1AttachEnv(t)
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "derived provenance", Payload: "body"})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := extractTaskID(t, out)

	row := phase1TaskRow(t, taskID)
	if !tasksource.Known(row.Source) {
		t.Fatalf("derived ledger source %q is not ledger vocabulary (want %s)", row.Source, tasksource.Policy())
	}
	if value, ok := tasksource.Normalize(row.Source); !ok || value != row.Source {
		t.Errorf("derived ledger source %q is not canonical (%q, accepted=%v)", row.Source, value, ok)
	}
	metaB, err := os.ReadFile(filepath.Join(home, ".gopi", "sessions", taskID, "metadata.json"))
	if err != nil {
		t.Fatalf("submit skeleton metadata.json missing: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaB, &meta); err != nil {
		t.Fatalf("metadata.json corrupt: %v", err)
	}
	if metaSource, _ := meta["source"].(string); metaSource != row.Source {
		t.Errorf("metadata source = %q, ledger source = %q — a derived role must be mirrored verbatim",
			metaSource, row.Source)
	}
}

// TestTaskHashDisplayIsNeverATruncation is the regression for B. The fixture
// that broke asserted `taskHashDisplay("t-1788146419") == "1788146419"`, i.e. it
// pinned the TRUNCATION rather than the identity — and truncation is ambiguous
// by construction (an 8-character chip names more than one ledger row; the
// full-id-no-prefix epic measured `01a0b51d` naming 35 rows). The invariant is
// that the value is the id, verbatim, for every id form the ledger carries: a
// proper prefix of the id means a row's "#" column is not the token a resolution
// path accepts.
func TestTaskHashDisplayIsNeverATruncation(t *testing.T) {
	ids := []string{
		"t-1788146419",                         // legacy auto id (the form the stale assertion named)
		"t-1788146419-1",                       // legacy collision suffix
		"019c2f1e-4a1b-7f2c-9d3e-8f4a5b6c7d8e", // uuid auto id
		"pa-q-p3-wf",                           // custom (PA) id
	}
	for _, id := range ids {
		got := taskHashDisplay(id)
		if got != id {
			t.Errorf("taskHashDisplay(%q) = %q, want the id verbatim (a truncated identity is ambiguous)", id, got)
		}
		if len(got) < len(id) {
			t.Errorf("taskHashDisplay(%q) = %q is a TRUNCATION (%d of %d characters)", id, got, len(got), len(id))
		}
	}
	// The exact negative the stale expectation encoded: the legacy id's digits
	// are NOT its display — stripping the "t-" prefix was the truncation.
	if taskHashDisplay("t-1788146419") == "1788146419" {
		t.Error(`taskHashDisplay("t-1788146419") still strips the "t-" prefix — the truncation c7de4ec8 retired`)
	}
}
