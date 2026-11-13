package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// task_sync — the TASK-ROW synchronization switch (one knob, one reader).
//
// WHY IT EXISTS (epic: five conflated axes): the node's IDENTITY (`role`),
// its COLLABORATION topology (`sync_peers`), its DELIVERY rule (branch
// push-back) and its KNOWLEDGE peers (`memory_peers`) are four different
// questions, and a node's answer to one used to decide the others. In
// particular "this node is a client" (identity — who arbitrates a conflict,
// whose branch push-back runs) was welded to "this node's task ledger is a
// mirror of a remote one" (collaboration), so a `role=client` node could not
// even submit a row locally: every `gopi task submit` was either FORWARDED to
// the peer or loudly refused (internal/tools/task_submit_forward.go).
//
// This switch answers the COLLABORATION question alone: does this node
// AUTOMATICALLY exchange task-ledger rows with declared peers? Nothing else
// moves with it — `role` still selects branch push-back and conflict
// arbitration, `compute_nodes` still whitelists dispatch targets,
// `memory_peers` still carries knowledge-asset sync. Neither delivery face
// consults this value: the finish-cleanup branch push-back chain does not read
// it (a client with task sync OFF still HANDS ITS BRANCH BACK), and neither did
// the serve-tick CODE-DELIVERY passes — the git/ref pass
// (cmd/agent/cli_tasks_sync_git.go) and the adoption sweep
// (cmd/agent/cli_tasks_adopt.go), both of which delivered and landed branch
// REFS and were decided by the declared topology (config.HasSyncPeers +
// tools.IsServerNode) exactly as the close-out push-back is. That git/ref pass
// file is GONE (the 2026-11-05 cross-node deletion), so of the three delivery
// faces only the close-out push-back and the adoption sweep still run — and the
// split it was drawn to keep holds for them: code delivery is on with ONE
// declared peer, while row exchange stays independently OFF.
//
// DEFAULT OFF, and OFF means OFF for automatic ROW crossings: the claim
// broadcast, the terminal-state push, the serve-tick DELTA pass (the row
// exchange, cmd/agent/cli_tasks_sync.go) and the submit forward all become
// no-ops. The MANUAL cross-node verb family (`gopi task
// sync|diff|harvest|send|receive|pull|push --peer <alias>`) was DELETED
// 2026-11-05 together with the ledger-to-ledger legs it drove, so there is no
// operator-driven crossing left to fall back on: this switch now gates the
// automatic legs and nothing else. `sync_peers` remains the topology list those
// legs AND the code-delivery faces resolve through.
//
// Default OFF is ALSO the upgrade posture (Rule 15a, and the reason this is
// not a repurpose of an existing key): a node that upgrades with a stale
// `sync_to` / `role=client` in its settings file keeps behaving locally until
// an operator deliberately turns the exchange back on. Turning a knob ON by
// omitting it is not a default, it is an ambush.

// TaskSyncSettingsKey is the settings.json key of the task-row-sync switch
// ("task_sync": true). Exported so the banner, the refusal texts and the doc
// name the key they read instead of re-typing it (Rule 1b/13).
const TaskSyncSettingsKey = "task_sync"

// TaskSyncEnv is the env override of the same knob (GOPI_TASK_SYNC=1). It is
// the second rung of the resolution the reader below implements (env >
// settings > default OFF) — Rule 2: exactly one place reads it.
const TaskSyncEnv = "GOPI_TASK_SYNC"

// --- ChatOpAuto: the chat console's standing posture -------------------------
//
// SECOND KNOB, SAME FILE, SAME SHAPE. `TaskSyncEnabled` above reports whether
// AUTOMATIC task-ROW exchange crosses the network; this one reports whether a
// `gopi chat` console may START an operation whose DURATION nobody declared.
// They are two axes of one question — "how much may a process do without a human
// in the loop?" — and they share this file for the same reason `task_sync` shares
// it with the node identity: ONE place resolves a switch of this shape, so the
// precedence, the truthy/falsey vocabulary and the "which channel decided"
// reporting cannot drift between them (Rule 2/13/14).
//
// The RESOLUTION lives in internal/tools (tools.ChatOpChannel) because that is
// where the criterion it feeds is enforced, and this file contributes its two
// NAMES and the settings KEY that carries it — the catalog row in settings.go
// binds them, and cmd/agent renders the banner from them.

// ChatOpAutoSettingsKey is the settings.json key of the posture ("chat_op_auto").
const ChatOpAutoSettingsKey = "chat_op_auto"

// ChatOpAutoEnv is the environment channel of the same posture.
const ChatOpAutoEnv = "GOPI_CHAT_OP_AUTO"

// taskSyncDefault is the switch's value when NEITHER channel declares it. It
// is a named constant, never a bare literal (Rule 1), because the default IS
// the epic's contract: OFF.
const taskSyncDefault = false

// taskSyncNotDeclared is the source label for the case where NEITHER channel
// mentions the knob. It is named (Rule 1) because the banner renders it
// verbatim and the test pins the three labels to three distinct strings.
const taskSyncNotDeclared = "not declared"

// taskSyncDeclaredHere is the source label for a settings file that CARRIES
// the key. Deliberately not the key name: the key's PRESENCE is the new fact
// this branch reports, and re-printing the key alone would read exactly like
// the OFF line's "you could set task_sync" advice.
const taskSyncDeclaredHere = "declared in " + TaskSyncSettingsKey

// TaskSyncEnabled reports whether AUTOMATIC task-row exchange is ON.
//
// Resolution: `GOPI_TASK_SYNC` (TaskSyncEnv) > settings.json "task_sync"
// (TaskSyncSettingsKey) > OFF. The env channel accepts the documented truthy
// vocabulary (EnvBool's spellings) and its documented falsey set
// ("0"/"off"/"false"/"no"), so an ApplyEnv-written "off" is understood rather
// than misread as enabled — an operator who exports GOPI_TASK_SYNC=0 on a node
// whose settings say `"task_sync": true` gets exchange OFF, which is what the
// precedence env > settings means everywhere else in this file's neighbours.
//
// A value that is NEITHER spelling (a typo like "ture") cannot answer the
// question: it is recorded on stderr and the settings channel decides — never
// silently read as "off", the exact defect Rule 15 bans for a switch whose off
// state is indistinguishable from its default.
//
// The setting lives on the Config struct (settings.json "task_sync") and is
// read here, in one place. There is no long-lived cache: a settings-file read
// per call is cheap relative to the ssh legs this switch guards, and a process
// whose settings file is edited mid-run must not keep an answer the operator
// has since revised (the memoisation trap ResetNodeIdentityForTest exists to
// patch, avoided here by not having it).
func TaskSyncEnabled() bool {
	enabled, _ := resolveTaskSync()
	return enabled
}

// resolveTaskSync is the ONE resolution (Rule 2): it answers two questions an
// operator reading the banner needs TOGETHER, so answering them in two places
// would let the banner disagree with the switch it reports. `enabled` is the
// switch itself and behaves exactly as TaskSyncEnabled always has; `source`
// names the channel that decided — TaskSyncEnv (env-supplied), the settings
// key (the file DECLARES the knob), or taskSyncNotDeclared (neither channel
// mentions it).
//
// Naming the source is not decoration: an OFF from a settings file that
// carries `"task_sync": false` and an OFF because the key is ABSENT are the
// same value and two different facts (did my declaration land?), and only the
// source distinguishes them.
func resolveTaskSync() (enabled bool, source string) {
	if raw := strings.TrimSpace(os.Getenv(TaskSyncEnv)); raw != "" {
		if IsTrueValue(raw) {
			return true, TaskSyncEnv
		}
		if IsFalseValue(raw) {
			return false, TaskSyncEnv
		}
		// Neither an enabling nor a disabling spelling: the value cannot
		// answer the question, so it must not be read as "off" silently.
		fmt.Fprintf(os.Stderr, "config: %s=%q is not a recognised boolean (%s/%s) — ignored; %s decides\n",
			TaskSyncEnv, raw, EnvBoolOn, "off", TaskSyncSettingsKey)
	}
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err != nil {
		// The settings file is the declaration of record; unreadable means the
		// answer is unknown, and unknown must not be reported as "enabled"
		// (Rule 15: the default is the safe posture, and it is announced).
		fmt.Fprintf(os.Stderr, "config: %s unreadable (%v) — %s resolves to its default (off)\n",
			ResolveSettingsPath(), err, TaskSyncSettingsKey)
		return taskSyncDefault, taskSyncNotDeclared
	}
	if cfg == nil {
		return taskSyncDefault, taskSyncNotDeclared
	}
	if cfg.TaskSync {
		return true, TaskSyncSettingsKey
	}
	// OFF is TWO states, and the file itself is the only witness: a settings
	// file that CARRIES the key has been read and answered, while one that
	// never mentions it left the built-in default in force.
	if taskSyncDeclaredInSettingsFile() {
		return false, taskSyncDeclaredHere
	}
	return false, taskSyncNotDeclared
}

// taskSyncDeclaredInSettingsFile reports whether the settings file at the
// resolved path CARRIES the switch's key — the difference between "off because
// you asked for off" and "off because nobody asked".
//
// It decodes the raw key set (map[string]json.RawMessage) rather than the
// typed Config, because the typed struct's `omitempty` bool cannot represent
// the distinction: absent and false are the same zero value there. This is a
// PRESENCE probe for a REPORTING branch only — the VALUE still comes from the
// one resolution above, so no precedence is duplicated (Rule 2) and the switch
// itself (TaskSyncEnabled) cannot move because of a probe result that is
// wrong. An unreadable or unparseable file yields false: the caller has
// already reported that failure on stderr, and "cannot tell" must not be
// dressed up as a declaration.
func taskSyncDeclaredInSettingsFile() bool {
	path := ResolveSettingsPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false // absent/unreadable is not a declaration; the caller reports it
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return false // same contract: unparseable is not a declaration
	}
	_, declared := keys[TaskSyncSettingsKey]
	return declared
}

// TaskSyncLine renders the startup banner's task-sync state, and it is
// DELIBERATELY never empty: the epic's fourth axis is the one an operator
// cannot infer from anything else on the banner (a client node with no peers,
// a server with task_sync off and a single-machine node all look alike from
// `role`/`sync_to`, and they behave differently). Empty-string suppression —
// the NodeIdentityLine convention — is right for a line that appears only
// when the identity is declared; it is wrong here, because "no line" would
// mean "unchanged" AND "off" AND "nobody asked".
func TaskSyncLine() string {
	enabled, source := resolveTaskSync()
	if enabled {
		return fmt.Sprintf("task-sync: on (%s)", source)
	}
	if source == taskSyncNotDeclared {
		return fmt.Sprintf("task-sync: off (default — automatic task-row exchange disabled; %s/%s enables it)",
			TaskSyncSettingsKey, TaskSyncEnv)
	}
	// OFF by DECLARATION. The line states the value the operator gave AND the
	// channel that carried it, so "my settings say false" and "nobody said
	// anything" stop reading alike — the two facts have the same VALUE (off)
	// and different meanings, and only this line can tell them apart.
	return fmt.Sprintf("task-sync: off (%s: false — automatic task-row exchange disabled)", source)
}
