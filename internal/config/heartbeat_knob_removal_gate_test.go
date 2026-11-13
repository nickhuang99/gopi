package config

// The heartbeat-knob removal gate (Rule 21 / Rule 13, task 01a0ed9f2e): two
// env knobs were ADVERTISED, fully read and stored — and consumed by nothing.
//
// WHAT WAS WRONG (the measured chain, devdoc/heartbeat-knobs-removed.md):
//
//	GOPI_HEARTBEAT_SEC / GOPI_HEARTBEAT_STALE
//	  → loop.go's NewAgent read them into Agent.HeartbeatInterval /
//	    Agent.HeartbeatStaleRounds
//	  → nothing read those fields: the tracker they configured
//	    (heartbeat.go's HeartbeatState) had no production constructor at all,
//	    and its own `interval` field had ZERO readers
//	  → the live stalled-progress role was already occupied by
//	    maybeNudgeStaleTaskList, which compares against a DIFFERENT hardcoded
//	    constant (maxRoundsWithoutTaskList = 3, loop.go)
//
// So an operator could set either variable, see it in `gopi --help-env`, and
// change nothing. That is Rule 21's defect shape exactly: a knob that READS
// like a working knob. The fix was to remove the knobs and the dead tracker.
//
// WHAT THIS TEST FREEZES — three directions, because the defect could return
// through any of them:
//
//  1. the CATALOG must not carry the names (a row is what makes
//     `gopi --help-env` render one);
//  2. the RENDERED front door (`FormatUsage`, the text `gopi --help-env`
//     prints) must not carry them either — the catalog and the page are two
//     surfaces and a row could come back without the section, or vice versa;
//  3. no PRODUCTION file may read or declare them, and the dead tracker's
//     file must stay gone. This is the half that catches a re-wiring attempt
//     ("let us just read the env var again") rather than only a re-advertising.
//
// WHY LITERALS HERE ARE CORRECT (Rule 13, stated so a later reader does not
// "fix" it): Rule 13 bans a bare literal standing in for a constant that
// exists. Here no constant may exist — the constant's whole reason for being
// was the removed knob, so a `heartbeatSecEnv` constant would resurrect the
// spelling as a first-class knob name. The literal IS the artifact under test:
// these two constants are the gate's subject, and the assertion is that they
// occur NOWHERE else in production.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// removedHeartbeatSecEnv is the removed tick-interval knob's exact spelling.
	removedHeartbeatSecEnv = "GOPI_HEARTBEAT_SEC"
	// removedHeartbeatStaleEnv is the removed stale-round knob's exact spelling.
	removedHeartbeatStaleEnv = "GOPI_HEARTBEAT_STALE"
	// removedHeartbeatSettingsKey is the settings.json key that carried the
	// knob; the CLI flag is derived from it by the catalog's own convention.
	removedHeartbeatSettingsKey = "heartbeat_sec"
	// removedHeartbeatStaleSettingsKey is its stale-round twin.
	removedHeartbeatStaleSettingsKey = "heartbeat_stale"
	// removedHeartbeatTrackerFile is the dead module: no production constructor,
	// no reader of its timer field. It was deleted, not left to rot.
	removedHeartbeatTrackerFile = "internal/agent/heartbeat.go"
	// removedHeartbeatTrackerType is the type it declared, the one the two
	// fields above were assigned into.
	removedHeartbeatTrackerType = "HeartbeatState"
	// productionScanDirs are the trees a knob may legitimately be read from.
	productionScanDirs = "internal,cmd"
)

// productionFlagRE matches a CLI flag LOOKUP of the removed knob
// (`--heartbeat-sec`), which the settings key's own literal does not cover — the
// flag is spelled with a dash and is a separate surface (flags.go's case arm),
// so it needs its own predicate. A var, not a const: MustCompile is a call, and
// the pattern's own constant half is the spelling below.
var productionFlagRE = regexp.MustCompile(`(?m)"heartbeat-(sec|stale)"`)

// productionTrackerTypeDeclRE matches a production DECLARATION of the dead
// tracker — `type HeartbeatState struct`, or a constructor (`NewHeartbeatState`)
// or method whose receiver is that type. It is deliberately not a bare
// substring: `taskheartbeat.go` (a live, different module) mentions the old
// type's NAME in a comment explaining the precedent it set, and a gate that
// failed on prose would be a gate its owners route around.
var productionTrackerTypeDeclRE = regexp.MustCompile(
	`(?m)\btype\s+` + removedHeartbeatTrackerType + `\b|\b` + removedHeartbeatTrackerType + `\s*\)|\bfunc\s+\w*\(?\s*\*?` + removedHeartbeatTrackerType + `\b`)

// TestRemovedHeartbeatKnobsStayRemoved is the gate. It fails if the dead
// wiring or its advertising returns in any of the three directions above.
func TestRemovedHeartbeatKnobsStayRemoved(t *testing.T) {
	root := repoRootForConfigTest(t)
	names := []string{removedHeartbeatSecEnv, removedHeartbeatStaleEnv}
	keys := []string{removedHeartbeatSettingsKey, removedHeartbeatStaleSettingsKey}

	// Direction 1 — the catalog. The catalog is the source of truth the help
	// renders from, so a row here IS the advertisement.
	for _, e := range EnvVars {
		for i, n := range names {
			if e.Env == n {
				t.Errorf("catalog row %q is back: %s was removed because nothing consumed it — "+
					"the live stalled-progress mechanism is maybeNudgeStaleTaskList "+
					"(devdoc/heartbeat-knobs-removed.md)", e.Env, n)
			}
			if e.Key == keys[i] {
				t.Errorf("catalog row %q carries settings key %q — the removed knob's key is back", e.Env, e.Key)
			}
		}
	}

	// Direction 2 — the rendered front door. Asserted against the RENDERED text,
	// not the section table: that is the surface an operator actually reads.
	envTable := FormatEnvTable()
	for _, n := range names {
		if strings.Contains(envTable, n) {
			t.Errorf("`gopi --help-env` (FormatEnvTable) advertises %s again — "+
				"a knob that nothing consumes must not be documented as one", n)
		}
	}
	for _, k := range keys {
		if strings.Contains(envTable, k) {
			t.Errorf("`gopi --help-env` renders the removed settings key %q again", k)
		}
	}
	// The section must still render: the removal dropped two NAMES, not the
	// section, so a reader who knows where the knob lived still finds the rest.
	if !strings.Contains(envTable, usageAgentRuntimeSection) {
		t.Errorf("the Agent-runtime section vanished from `gopi --help-env` — the two names "+
			"were removed, the section was not (want it still rendering: %s)", usageAgentRuntimeSection)
	}

	// Direction 3 — production code. A re-read (or a re-declaration) of either
	// name, and the resurrection of the dead tracker, both fail here.
	for _, dir := range strings.Split(productionScanDirs, ",") {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			body, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			rel, _ := filepath.Rel(root, p) //stylegate:ignore relativises a path inside the test's own root; an error would only mislabel the finding this gate prints
			rel = filepath.ToSlash(rel)
			for _, n := range names {
				if strings.Contains(string(body), n) {
					t.Errorf("%s names %s — the removed knob's reader/declaration is back in "+
						"production; re-wiring a knob nothing consumes is the defect, not the fix", rel, n)
				}
			}
			for _, k := range keys {
				if strings.Contains(string(body), k) {
					t.Errorf("%s names the removed settings key %q — the whole knob chain must stay gone", rel, k)
				}
			}
			if productionTrackerTypeDeclRE.Match(body) {
				t.Errorf("%s references %s — the dead tracker was deleted (no production "+
					"constructor, no reader of its timer field); do not revive it", rel, removedHeartbeatTrackerType)
			}
			if productionFlagRE.Match(body) {
				t.Errorf("%s looks up a removed --heartbeat-* flag — the flag arm was removed with "+
					"its knob (Rule 21: an advertised knob must have a consumer)", rel)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("scan %s: %v", dir, walkErr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(removedHeartbeatTrackerFile))); err == nil {
		t.Errorf("%s exists again — it was wholly dead code; the live role belongs to "+
			"maybeNudgeStaleTaskList", removedHeartbeatTrackerFile)
	}
}

// usageAgentRuntimeSection is the section header the two removed names lived
// under. Named here so the assertion above and the section table cannot drift.
const usageAgentRuntimeSection = "Agent runtime"
