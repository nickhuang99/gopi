package config

import (
	"os"
	"path/filepath"
	"strings"
)

// sessions_root.go — WHERE A SESSION RUN WRITES, resolved in exactly ONE place.
//
// THE DEFECT THIS CLOSES (task 01a12662, epic: replay sessions flood the main
// sessions root). Every session-writing path used to spell
// `filepath.Join(config.GopiDir(), config.SessionsDirName)` for itself, so
// "where does a run's directory live" had as many answers as it had callers.
// That was fine while the answer was a constant. It stopped being fine the
// moment one KIND of run — a REPLAY, the thing `gopi chat --replay` and the
// `--film` driver behind it run, sometimes dozens of times an hour — had to land
// somewhere else: with N call sites, relocating it would have meant N edits and
// a silent split the day one of them was missed (the JSONL in one root, its
// `.raw.log` in another — the exact drift session-dir-unify S1 was written to
// end).
//
// THE CHANNEL IS ONE ENVIRONMENT VARIABLE, and it is the SAME kind of channel
// `GOPI_HOME` already is: a value that lives only inside the process tree that
// set it, consumed by the process's own resolution, installing nothing and
// outliving nothing (Rule 35 — no file, no daemon, no config key). A run that
// relocates its own sessions calls config.SessionsRoot's own contract by
// exporting this before any session is minted; a process that exports nothing
// reads the documented default, so a normal `gopi chat` is untouched by
// construction rather than by a branch.
//
// WHY NOT `GOPI_HOME` FOR THIS. `GOPI_HOME` relocates the WHOLE profile —
// settings.json, the ledger, plugins, the logs — and a replay must keep reading
// the operator's real configuration and answer `gopi session list` off the real
// ledger. One profile, one relocated tree: that is what this variable is.
const SessionsRootEnv = "GOPI_SESSIONS_ROOT"

// SessionsRoot resolves the directory a session RUN writes its directory into:
//
//	$GOPI_SESSIONS_ROOT            (the process's own relocation, if any)
//	<gopi dir>/<SessionsDirName>   (the documented default)
//
// The relocated value is returned VERBATIM (not joined with the sessions dir
// name): a caller that exports a path means that path is the root, so
// `<sink>/sessions` would make the reader and the writer disagree about what was
// asked for. The empty value is the absent one — os.Getenv renders both as "",
// and a root of "" would address the process's own working directory, which is
// the failure mode the whole declared-tree registry exists to prevent.
func SessionsRoot() (string, error) {
	if v := strings.TrimSpace(os.Getenv(SessionsRootEnv)); v != "" {
		return v, nil
	}
	gd, err := GopiDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(gd, SessionsDirName), nil
}

// SessionsRootIn is SessionsRoot for an EXPLICIT gopi data dir — the join the
// default branch above performs, named so a caller that already holds the data
// root (a fixture, the crash-dump sweep) spells it the one way instead of
// re-typing it (Rules 13/14).
func SessionsRootIn(gopiDir string) string {
	return filepath.Join(gopiDir, SessionsDirName)
}

// ChatSlotsDir is WHERE THE CHAT SLOT REGISTRY LIVES (the files
// `chat_session_id_<0..9>.txt` a chat run writes at startup and the sink
// command, scripts/notify/notify.sh, scans — cmd/agent/chat_session_slots.go).
//
// THE DEFECT THIS CLOSES (task 01a12d05). A run that RELOCATES its own session
// tree — `gopi chat --replay`/`--film`, which publishes SessionsRootEnv
// (chat_sessions_root.go) — kept filing its slot into the OPERATOR's profile
// root, because the registry resolved through GopiDir while the session resolved
// through SessionsRoot: two paths, and the relocation moved only one of them.
// Measured on the operator's own profile 2026-10-12: nine of the ten slots held
// `<scratch>/queue/queue-runtime-ui-session-*/chatreplay-sessions-*/sessions/<uuid>`
// — sessions that had long since been swept — with the machine's one real chat
// squeezed beside them. A run's own tree is the tree that holds ITS registry,
// which is the same rule the sandbox already states (Rule 34: a private profile
// answers for itself).
//
// ONE DERIVATION, NOT A SECOND CHANNEL: the registry is the directory the
// session tree lives in. The default branch of SessionsRoot is
// `filepath.Join(GopiDir(), SessionsDirName)`, so its parent IS that GopiDir and
// an ordinary run's path is UNCHANGED — character for character — while a
// relocated run gets the directory the caller named. There is deliberately no
// `GOPI_CHAT_SLOTS` variable and no flag: a second knob would be a second
// spelling of "which tree is mine", and the two could be set to disagree.
//
// The relocation is a property of the PROCESS (`SessionsRootEnv`), published
// before any session is minted, so a reader inside this run resolves it exactly
// as the writer did — that is what makes the registry findable rather than a
// write-only file in a scratch nobody scans.
func ChatSlotsDir() (string, error) {
	root, err := SessionsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Dir(root), nil
}

// ChatSlotsDirIn is ChatSlotsDir for an EXPLICIT gopi data dir — the same
// derivation, so a caller that already holds the data root (a fixture, a
// diagnostic) spells it the one way instead of re-typing it (Rules 13/14).
func ChatSlotsDirIn(gopiDir string) string {
	return filepath.Dir(SessionsRootIn(gopiDir))
}
