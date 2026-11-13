package config

// devrepo.go — THE CLONE CHANNEL'S SWITCH AND ITS WORKSPACE ROOT.
//
// Two settings keys, one decision each (Rule 1: a decision has ONE source):
//
//	dev_repo        (settings) / GOPI_DEV_REPO        — host:path of the
//	                  repository a task workspace is CLONED from. NON-EMPTY IS
//	                  THE SWITCH: empty/absent = today's worktree channel,
//	                  unchanged; non-empty = `gopi worker` clones from here.
//	                  There is deliberately NO third boolean: a second switch
//	                  would be a second decision-maker for the same question.
//	workspace_root  (settings) / GOPI_WORKSPACE_ROOT  — the RECLAIMABLE parent
//	                  directory the clone channel opens a per-task subdirectory
//	                  in. Default <home>/workspace (worktreeroots.DefaultRoot),
//	                  the same place and meaning the worktree channel uses, so
//	                  the two channels coexist without a second root policy.
//
// WHY THESE TWO AND NOT A NEW VERB (operator ruling, 2026-10-18): the local
// master was a single point — every agent's baseline was however stale the
// last hand-run pull left it, and N agents cannot pull one local master at
// once. The fix is the GitHub shape: one truth at origin, each agent cloning
// its own workspace and running pull --rebase → build → push. `gopi worker`'s
// spelling, arguments and exit codes stay EXACTLY as they are; this file is
// the whole user-visible surface of the change.
//
// BACKWARD COMPATIBILITY IS THE CONTRACT: an older binary does not know these
// keys and ignores them (encoding/json's unknown-field tolerance), and this
// revision changes NO existing key's meaning — an empty dev_repo reproduces
// today's behaviour byte for byte.
//
// Rule 2 (one reader): DevRepoFromSettingsFile and WorkspaceRootFromSettingsFile
// are the ONLY functions that read these two keys out of a settings file, and
// the env names are read here and nowhere else.

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// DevRepoEnvName is the environment channel for the clone channel's
	// source repository (host:path, the same spelling RepoConfig.Repo uses).
	DevRepoEnvName = "GOPI_DEV_REPO"
	// DevRepoSettingsKey is the settings.json key carrying the same value.
	DevRepoSettingsKey = "dev_repo"

	// WorkspaceRootEnvName is the environment channel for the reclaimable
	// workspace parent directory.
	WorkspaceRootEnvName = "GOPI_WORKSPACE_ROOT"
	// WorkspaceRootSettingsKey is the settings.json key carrying the same
	// value.
	WorkspaceRootSettingsKey = "workspace_root"
)

// DevRepo is the settings.json key both the struct field and the catalog row
// name — spelled once here so a rename is one line (Rule 1b).
//
// (It is deliberately NOT a `Config` field: the value's ONLY reader is
// DevRepoFromSettingsFile, which reads the document's own top level, so a
// struct field would be a second copy nothing consumes — the dead-knob defect
// the config catalog gate exists to catch.)
const DevRepo = DevRepoSettingsKey

// DevRepoConfigured returns the source repository a task workspace is cloned
// from: the env channel wins over the settings channel, and "" means the
// clone channel is OFF (today's worktree channel).
//
// The error is returned ONLY for a settings file that cannot be read or
// parsed, and the caller must treat it as a REFUSAL rather than a fallback to
// the worktree channel: silently choosing a different channel because the file
// is broken would start a run in a workspace the operator did not name
// (Rule 15 — the safe direction is to say what is wrong).
func DevRepoConfigured() (string, string, error) {
	if v := strings.TrimSpace(os.Getenv(DevRepoEnvName)); v != "" {
		return v, DevRepoEnvName, nil
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		return "", "", err
	}
	if v := strings.TrimSpace(cfg.topLevelOnlyString(DevRepoSettingsKey)); v != "" {
		return v, "settings " + DevRepoSettingsKey, nil
	}
	return "", "", nil
}

// WorkspaceRootConfigured returns the reclaimable parent directory the clone
// channel opens per-task subdirectories in, and the label of the channel that
// decided it (Rule 15's positive half: a default is reported as a default).
//
// Precedence is env > settings > worktreeroots.DefaultRoot() — the same
// ranking every other root knob here uses (one policy, not two). A declared
// value is canonicalized by the ONE tilde reader, and a value that cannot be
// made absolute is an ERROR rather than a relative root, which would authorise
// different paths in different working directories.
//
// The LABEL comes from the resolver's own Root: see below.
func WorkspaceRootConfigured() (string, string, error) {
	declared, label := strings.TrimSpace(os.Getenv(WorkspaceRootEnvName)), WorkspaceRootEnvName
	if declared == "" {
		cfg, err := FromSettingsFile(ResolveSettingsPath())
		if err != nil {
			return "", "", err
		}
		declared, label = strings.TrimSpace(cfg.topLevelOnlyString(WorkspaceRootSettingsKey)), WorkspaceRootSettingsLabel
	}
	root, err := worktreeroots.Resolve(declared)
	if err != nil {
		return "", "", err
	}
	return workspaceRootAnswer(root, declared, label)
}

// THE LABEL IS THE RESOLVER'S, NOT THIS FUNCTION'S (bug 01a1945d, measured
// 2026-11-01): worktreeroots.Resolve consults GOPI_WORKTREE_ROOT BEFORE the
// value handed to it, so a label pinned above named a channel that decided
// nothing — path from one variable, source from another, and no error (Rule 15).
// workspaceRootAnswer (devrepo_workspace_root.go) returns the resolver's own
// pair, so the two cannot disagree, and it NAMES an outranked declaration.

// CloneChannelConfigured is the ONE predicate that decides which workspace
// channel a `gopi worker` run takes: non-empty dev_repo = the clone channel.
// Every caller asks THIS question, so "is the clone channel on?" cannot be
// answered two different ways (Rule 1/14).
func CloneChannelConfigured() (bool, string, error) {
	repo, source, err := DevRepoConfigured()
	if err != nil {
		return false, "", err
	}
	return repo != "", source, nil
}

// topLevelOnlyReader is ONE settings key this build MODELS — but by reading the
// document's own TOP LEVEL, not by decoding into a Config field.
//
// WHY THIS REGISTRY EXISTS (bug measured 2026-11-13). `gopi config show` marks a
// key it does not model with `(unrecognized key — … so it is IGNORED)`, and it
// asked `ModelledSettingsKeys()` — which reflects the Config STRUCT's json tags.
// These two keys are deliberately not struct fields (see DevRepo's note above),
// so the verb called two of its own live knobs IGNORED while `gopi worker`
// printed, in the same breath, `clone channel ON (dev_repo=… via settings
// dev_repo; workspace_root=… via settings workspace_root)`. A report that
// contradicts the runtime is worse than no report (Rules 15/13).
//
// THE READER IS CARRIED WITH THE KEY, and that is the whole point: the value in
// this table IS the production read path (topLevelOnlyString routes through it),
// so "modelled" can never be a separate hand-kept list that drifts from what the
// decoder honours (Rules 1b/13/14). A key added here without a reader cannot
// compile; a reader added without a row falls outside topLevelOnlyString.
type topLevelOnlyReader struct {
	// Key is the settings.json key, always the exported constant (Rule 1).
	Key string
	// read answers the key's raw top-level string ("" when absent or not a
	// string). It is the ONLY shape allowed to call rawTopLevelString.
	read func(*Config) string
}

// topLevelOnlySettingsKeys is the registry. It lives HERE, beside the readers
// that own it, rather than in the renderer's package: a list in cmd/agent would
// be the second copy this repo's Rule 13 exists to forbid.
var topLevelOnlySettingsKeys = []topLevelOnlyReader{
	{Key: DevRepoSettingsKey, read: func(c *Config) string { return c.rawTopLevelString(DevRepoSettingsKey) }},
	{Key: WorkspaceRootSettingsKey, read: func(c *Config) string { return c.rawTopLevelString(WorkspaceRootSettingsKey) }},
}

// ModelledTopLevelOnlySettingsKeys returns the keys registered above, sorted, so
// the settings-report's "is this key modelled?" question is answered from ONE
// definition (ModelledSettingsKeys folds this list in).
func ModelledTopLevelOnlySettingsKeys() []string {
	out := make([]string, 0, len(topLevelOnlySettingsKeys))
	for _, row := range topLevelOnlySettingsKeys {
		out = append(out, row.Key)
	}
	sort.Strings(out)
	return out
}

// topLevelOnlyString answers ONE registered top-level key. An unregistered key
// answers "" — it is not a silent fallback but the absence of a reader, and
// registration is asserted (not assumed) by the package's own test.
func (c *Config) topLevelOnlyString(key string) string {
	for _, row := range topLevelOnlySettingsKeys {
		if row.Key == key {
			return row.read(c)
		}
	}
	return ""
}

// rawTopLevelString reads one TOP-LEVEL string key out of the settings
// document that was decoded ("" when the file is absent or the key is not a
// string).
//
// It exists because the keys here are read-only knobs with exactly one
// consumer, and FromSettingsFile keeps the document's own top level for
// precisely this kind of question — the alternative (a Config struct field)
// is a field nothing else reads, i.e. the dead-knob shape this package
// already has a gate against.
//
// CALL IT ONLY THROUGH topLevelOnlySettingsKeys (every production call is inside
// that registry; the test asserts it): a direct call elsewhere would be a reader
// the "is it modelled?" answer cannot see, which is exactly the bug above.
func (c *Config) rawTopLevelString(key string) string {
	if c == nil || c.rawTopLevel == nil {
		return ""
	}
	raw, ok := c.rawTopLevel[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
