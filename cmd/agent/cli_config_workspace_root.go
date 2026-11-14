package main

// cli_config_workspace_root.go — `gopi config show`'s answer to "where does a
// task's workspace actually land on THIS host?".
//
// WHY THIS VERB (bug measured 2026-11-13). The settings table printed
// `workspace_root` with `(unrecognized key — … so it is IGNORED)` while the very
// same run's worker banner read that key and built its ground under it:
//
//	task <id>: clone channel ON (dev_repo=… via settings dev_repo;
//	              workspace_root=/home/user/workspace via settings workspace_root)
//
// Two surfaces of one binary contradicting each other is worse than either being
// absent (Rule 15). The model half of that bug is fixed in internal/config
// (ModelledSettingsKeys now folds in the top-level-only readers), and this file
// is the REPORT half: the resolved answer, its channel, and — when an ambient
// variable outranks the declaration — the declaration it outranked.
//
// IT REPORTS, IT DOES NOT RE-DECIDE (Rules 2/13/14). The pair printed is the one
// `config.WorkspaceRootConfigured` returns, which is the SAME call the worker,
// the clone channel, the reclaim sweep, the submit-time ground test, the durable
// repo repair and the ground-adoption arms all make — one reader, every
// production caller (internal/agent + internal/tools). A report that re-ranked
// the channels itself could disagree with the run it is describing, which is
// exactly the defect above in the other direction.
//
// `worktreeroots.Resolve` is why the label is not simply the key's name: it
// consults GOPI_WORKTREE_ROOT-shaped ambient input BEFORE the value it is handed,
// so with both set the PATH comes from the variable and the declaration is
// outranked — the resolver says so in the label, and this row prints it verbatim.

import (
	"fmt"
	"os"

	"github.com/nick/gopi/internal/config"
)

const (
	// configWorkspaceRootLabel is the report's row label. It is deliberately
	// distinct from `run repo` (which repository a run works IN) and from the
	// `repos` table's `worktree_root` (a per-repo declaration): this row answers
	// where the per-task DIRECTORY is created.
	configWorkspaceRootLabel = "workspace root"
	// configWorkspaceRootFmt names the directory and the channel that decided
	// it (Rule 15's positive half: a decision is never anonymous).
	configWorkspaceRootFmt = "%s (from %s)"
	// configWorkspaceRootUnreadable is printed when the resolver refuses. The
	// refusal itself goes to stderr with its own reason, so the row says where
	// to look rather than repeating it.
	configWorkspaceRootUnreadable = "(unresolved — see the refusal below)"
)

// showWorkspaceRoot prints the reclaimable parent directory a task workspace is
// created under, with the channel that decided it.
//
// A resolver ERROR is reported on BOTH faces on purpose: the row on stdout keeps
// the report's shape readable, and stderr carries the exact reason (a relative
// value, an unresolvable home) — an operator scripting this verb reads stdout,
// and an operator debugging it reads stderr.
func showWorkspaceRoot() {
	root, source, err := config.WorkspaceRootConfigured()
	if err != nil {
		fmt.Printf("  %-22s %s\n", configWorkspaceRootLabel, configWorkspaceRootUnreadable)
		fmt.Fprintf(os.Stderr, "%s: %s: %v\n", runRepoProgName, config.WorkspaceRootSettingsKey, err)
		return
	}
	fmt.Printf("  %-22s %s\n", configWorkspaceRootLabel,
		fmt.Sprintf(configWorkspaceRootFmt, root, source))
}
