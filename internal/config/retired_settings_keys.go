package config

// retired_settings_keys.go — the ONE registry of RETIRED configuration knobs,
// and the answer to "what was this key for?".
//
// WHY A TABLE (the defect this file closes, 2026-11-06). Each retirement used to
// carry its own report FUNCTION in cmd/agent, each called from one global
// startup hook (tmpdir_inject.go's tmpDirBootstrapFromArgs). The effect was that
// EVERY command — `gopi task list`, `gopi config -h`, `gopi notify`, a bare
// one-shot prompt — printed two long lines about a settings key before its own
// output, because the pre-chain moment is the one point every verb passes
// through. Measured before this file existed, on this host:
//
//	$ gopi task list --status in_progress 2>/tmp/e
//	$ cat /tmp/e
//	worker: the settings key `task_pull` is RETIRED: the worker-pull switch is
//	gone: ... (two lines, ~700 bytes, before a single task row)
//
// The operator's ruling is the shape of the fix: 你们的 config 这个命令就是专门去
// 检查这些配置文件的，那个时候你可以指出来哪些是过时，哪些是什么用，其用途是什么?
// ……其他时候不要去发出这些噪音。 A prompt belongs to the command whose JOB it is:
// a stale key is a fact about the CONFIGURATION, so `gopi config show` reports
// it — and no other command does.
//
// WHY THE NAMES STAY, AND WHY THE ENTRY IS A TABLE ROW RATHER THAN A FUNCTION.
// The settings decoder tolerates an undeclared key SILENTLY (`json.Unmarshal`
// into a struct ignores what the struct does not model), so a settings.json
// still carrying a retired key is a knob NOBODY ever mentions unless something
// deliberately speaks it. That speaker is now this table, read by the ONE verb
// that reports configuration. Adding the sixth retirement is adding a ROW: no
// new report function, no new hook, and therefore no new per-start noise
// (Rule 35: a dependency — and a start-path hook is one — is not created
// casually).
//
// THE FOURTH COLUMN IS THE POINT OF THE MOVE. The startup hook said only "this
// is retired"; an operator reading it had no idea what the knob had done. Each
// row answers four questions: WHAT it was called (both faces), WHAT IT DID
// (Purpose), WHY IT WENT AND WITH WHAT (Retired), and WHAT TO DO INSTEAD
// (Replacement — the same constant every other surface renders, Rule 1b/14, so
// the sentence cannot drift between this table and a help text).

import (
	"sort"
	"strings"
)

// RetiredSettingsKey is ONE retired knob. The zero SettingsKey means the knob
// was ENV-ONLY for its whole life (no settings spelling is invented at
// retirement time — naming one would put a key in an operator's file that no
// version of this program ever wrote).
type RetiredSettingsKey struct {
	// SettingsKey is the settings.json key, or "" when the knob never had one.
	SettingsKey string
	// EnvName is the environment spelling, or "" when the knob had none.
	EnvName string
	// Purpose is WHAT THE KNOB CONTROLLED, in one sentence — the half the
	// startup hook never said.
	Purpose string
	// Retired is WHEN and WITH WHAT the knob went.
	Retired string
	// Replacement is what to do instead. It names an existing constant wherever
	// one exists, so a replacement cannot be re-typed (and cannot drift).
	Replacement string
}

// retiredSettingsKeys is the registry. It is UNEXPORTED and reached through the
// accessors below so no caller can mutate it and no caller has to know its
// order (the accessors sort by key for a stable report).
//
// Rule 13/14: every KEY NAME and ENV NAME is the existing exported constant,
// never a re-typed literal — the constants stay the single definition of each
// spelling (they are also what keeps the retirement's vocabulary available to
// the gates that assert a retired knob is never consumed).
var retiredSettingsKeys = []RetiredSettingsKey{
	{
		SettingsKey: ComputeNodesRetiredSettingsKey,
		EnvName:     ComputeNodesRetiredEnvName,
		Purpose:     "the explicit DISPATCH TOPOLOGY: a whitelist of compute nodes, each with its ssh alias, its capacity (max_concurrent), the alias the remote reported back through (report_to) and the git PUSH-BACK target for the remote's finished branch (`repo`, i.e. `git push <repo> HEAD` — never a default repo for `task_submit`)",
		Retired:     "deleted 2026-10-22 (commit bf87a5d03) with its only would-be consumer, cmd/agent/dispatch.go's runDispatchOnce, which had ZERO call sites: `gopi task serve` is a retired-daemon refusal and `gopi task run --nodes` went with that verb, so the whitelist was parsed, validated and read by nothing while printing a ~400-char \"NO CONSUMER\" essay onto every worker start (devdoc/compute-nodes-knob-deleted-20261022.md)",
		Replacement: ComputeNodesRetiredReplacement,
	},
	{
		SettingsKey: MergeTickRetiredSettingsKey,
		EnvName:     MergeTickRetiredEnvName,
		Purpose:     "the cadence, in minutes, of the resident MERGE TICK: a worker round spawned `gopi merge --fast` as a BLOCKING child and stayed offline for the duration",
		Retired:     "deleted 2026-09-27 together with the tick, its publisher (config.PublishMergeTickMin) and the worker-round call site — a merge is `gopi task merge`'s job, and two live workers running merges contended for the merge flock (devdoc/worker-merge-tick-retired.md)",
		Replacement: MergeTickRetiredReplacement,
	},
	{
		SettingsKey: MemorySyncTickRetiredSettingsKey,
		EnvName:     MemorySyncTickRetiredEnvName,
		Purpose:     "the cadence, in minutes, of the resident MEMORY-SYNC TICK: every tick spawned one cross-node ledger-sync child per declared peer",
		Retired:     "deleted 2026-10-25 together with its publisher and the worker-round call site; each fired run held a worker round for 315-534s measured in ~/.gopi/logs/worker.log, and the `gopi memory sync` verb it drove is itself DELETED (devdoc/memory-sync-tick-retired.md)",
		Replacement: LedgerCopyForwardReplacement,
	},
	{
		SettingsKey: TaskPullRetiredSettingsKey,
		EnvName:     TaskPullRetiredEnvName,
		Purpose:     "a boolean switch: when a worker's LOCAL queue was empty, ask each declared peer for ONE transferable row and run it here (`gopi task receive --takeover`)",
		Retired:     "deleted 2026-11-06 (batch 5, commit d7d836e5) once it had zero readers, after the whole cross-node row family and its only consumer `gopi task receive --takeover` went in batch 1 (2026-11-05)",
		Replacement: TaskPullRetiredReplacement,
	},
	{
		SettingsKey: MemorySyncOnStoreSettingsKey,
		EnvName:     MemorySyncOnStoreEnv,
		Purpose:     "the direction (\"push\" | \"pull\" | \"off\") of the memory-asset sync that a SUCCESSFUL `memory store` fired: push sent this node's rows out, pull applied the peer's rows here",
		Retired:     "RETIRED 2026-10-27 (epic memory-store-sync-decouple): the key is still DECLARED, so a typo in it is still an error, but every well-formed value now resolves to off — a `memory store` writes this node's ledger and reaches no peer (devdoc/memory-store-sync-decouple.md)",
		Replacement: LedgerCopyForwardReplacement,
	},
	{
		// ENV-ONLY: this cadence never had a settings.json spelling, and none is
		// invented here (see this file's header).
		EnvName:     MemorySyncEveryRetiredEnvName,
		Purpose:     "the cadence of the periodic memory-sync leg: `gopi memory sync --peer X --push --every <dur>` repeated one push session until SIGINT/SIGTERM",
		Retired:     "deleted 2026-11-06 (batch 5) together with its reader, its 300s default and the `--every` loop, after the `gopi memory sync` verb itself went in batch 1 (2026-11-05)",
		Replacement: MemorySyncEveryRetiredReplacement,
	},
}

// RetiredSettingsKeys returns the registry sorted by the settings key (env-only
// rows last, ordered by env name), so a report rendering it is stable across
// runs and a test can assert it without depending on declaration order.
//
// The returned slice is a COPY: a caller may sort, filter or edit it freely
// without changing what the next caller sees.
func RetiredSettingsKeys() []RetiredSettingsKey {
	out := make([]RetiredSettingsKey, len(retiredSettingsKeys))
	copy(out, retiredSettingsKeys)
	sortRetired(out)
	return out
}

// RetiredSettingsKeyInfo answers "is this settings key a retired knob, and what
// did it do?" — the ONE query `gopi config show` makes per printed row. A key
// that is not in the registry is not retired (it is live, unmodelled, or a
// typo), and that is the caller's business to render.
func RetiredSettingsKeyInfo(settingsKey string) (RetiredSettingsKey, bool) {
	key := strings.TrimSpace(settingsKey)
	if key == "" {
		return RetiredSettingsKey{}, false
	}
	for _, record := range retiredSettingsKeys {
		if record.SettingsKey != "" && record.SettingsKey == key {
			return record, true
		}
	}
	return RetiredSettingsKey{}, false
}

// RetiredKnobsWithEnvFace returns every registry row that HAS an environment
// face — the set `gopi config show` checks against this process's environment,
// so an exported-retired-knob is reported on the configuration verb rather than
// on every start.
func RetiredKnobsWithEnvFace() []RetiredSettingsKey {
	out := make([]RetiredSettingsKey, 0, len(retiredSettingsKeys))
	for _, record := range retiredSettingsKeys {
		if record.EnvName != "" {
			out = append(out, record)
		}
	}
	sortRetired(out)
	return out
}

// sortRetired orders a registry copy by its SETTINGS key, with the env-only
// rows (no settings key) after them ordered by env name: one stable total order,
// derived here rather than repeated at every renderer.
func sortRetired(rows []RetiredSettingsKey) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.SettingsKey == "") != (b.SettingsKey == "") {
			return a.SettingsKey != ""
		}
		if a.SettingsKey != b.SettingsKey {
			return a.SettingsKey < b.SettingsKey
		}
		return a.EnvName < b.EnvName
	})
}
