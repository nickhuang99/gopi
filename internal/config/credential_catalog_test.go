package config

import (
	"strings"
	"testing"
)

// credential_catalog_test.go — AudioAPIKeyEnv is the environment CHANNEL of the
// inline recording-engine credential.
//
// It IS read (cmd/agent's voiceDoubaoKeySource) because a catalog row an
// operator can export while gopi ignores it is the silent failure Rule 15 bans.
// It is deliberately NOT republished by ApplyEnv — see the constant's own doc.
// This test pins the half that lives in this package: the row exists, it backs
// the settings key, and it TELLS the operator the one thing that makes this knob
// different from every other doubao one.

func TestDoubaoAPIKeyCatalogRowCarriesItsVerdict(t *testing.T) {
	var row *envKey
	for i := range EnvVars {
		if EnvVars[i].Env == AudioAPIKeyEnv {
			row = &EnvVars[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("the catalog carries no row for %s — the knob is undiscoverable from `gopi -h`", AudioAPIKeyEnv)
	}
	// The nested surface has no TOP-LEVEL settings key to name in the catalog's
	// short cell: the row's value is addressed by the documented PATH
	// (audio_engine_param.<block>.<knob>), which the help prose spells — the
	// cell itself stays empty so the help table's column budget is not blown by
	// a dotted path (see settings.go's catalog comment).
	wantPath := AudioParamSettingsPath(AudioEngineRecording, AudioParamKeyAPIKey)
	if row.Key != "" {
		t.Errorf("catalog row backs %q, want empty (the documented path is %q)", row.Key, wantPath)
	}
	if !strings.Contains(row.Help, wantPath) {
		t.Errorf("catalog row %q does not name %q — the knob is undiscoverable from `gopi -h`", row.Help, wantPath)
	}
	if !strings.Contains(row.Help, DoubaoAPIKeyNotPublishedClaim) {
		t.Errorf("catalog row %q does not state %q — an operator cannot learn that exporting this value is not the same as storing it",
			row.Help, DoubaoAPIKeyNotPublishedClaim)
	}
	if row.CLIFlag != "" {
		t.Errorf("catalog row names CLI flag %q — a credential has no command-line channel (it would land in the process table)", row.CLIFlag)
	}
}
