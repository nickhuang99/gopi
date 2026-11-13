package config

// devrepo_workspace_root.go — THE WORKSPACE ROOT'S (PATH, LABEL) PAIR, AND THE
// ONE-KEY-TWO-VALUES REPORT.
//
// WHY THIS FILE EXISTS (bug 01a1945d, measured 2026-11-01). The clone channel's
// workspace root is answered by TWO different functions, and they used to
// disagree about WHICH CHANNEL decided it:
//
//	config.WorkspaceRootConfigured  read GOPI_WORKSPACE_ROOT / settings
//	                                `workspace_root` and pinned the SOURCE label
//	                                to whichever of those it had just read;
//	worktreeroots.Resolve           answered the PATH, and consults
//	                                GOPI_WORKTREE_ROOT FIRST — before the value
//	                                it is handed.
//
// So with both variables set, the PATH came from GOPI_WORKTREE_ROOT while the
// SOURCE still read "GOPI_WORKSPACE_ROOT". Nothing failed, so nothing said so
// (Rule 15). Measured before the fix, a probe test with both set:
//
//	both-set-different: path="/tmp/probe-worktree-root" source="GOPI_WORKSPACE_ROOT" err=<nil>
//	both-set-same:      path="/tmp/probe-workspace-root"  source="GOPI_WORKSPACE_ROOT" err=<nil>
//	only-workspace:     path="/tmp/probe-workspace-root"  source="GOPI_WORKSPACE_ROOT" err=<nil>
//
// The cost is not cosmetic: a reader who follows the label investigates the
// wrong variable, and an e2e fixture that pins GOPI_WORKSPACE_ROOT alone
// actually measures the worktree root instead — `testutil.MockEnv` ALWAYS pins
// GOPI_WORKTREE_ROOT, so that collision is the ordinary case in a suite, not an
// exotic one.
//
// THE FIX IS ONE RULE IN THREE CLAUSES, and every clause comes from ONE fact:
// the root resolver returns ITS OWN Source for the answer it gave. The pair is
// therefore read off that single Root value and cannot disagree by construction.
//
//	① the resolver answered from its DEFAULT   → "default".
//	② the resolver answered from the ambient override
//	  (GOPI_WORKTREE_ROOT)                    → that channel's own label is the
//	                                            label, and when a declared
//	                                            workspace_root exists and names a
//	                                            DIFFERENT directory the label
//	                                            says so, naming the value that is
//	                                            not in force.
//	③ otherwise                                → the resolver's answer IS the
//	                                            declared value, so the label
//	                                            names the workspace_root channel
//	                                            that declared it.
//
// WHY THE OVERRIDE STILL WINS INSTEAD OF BEING REFUSED. Refusing was the
// tempting reading of "handle the conflict", and it is wrong in the one place it
// would fire on a real host: a SANDBOX (and `testutil.MockEnv`) redirects work
// out of the operator's ~/workspace by setting GOPI_WORKTREE_ROOT while the
// operator's own settings.json still declares workspace_root — so a refusal
// would stop the clone channel everywhere a sandbox or a suite runs, which is the
// opposite of what the override exists for. The override is a deliberate,
// documented redirect (worktreeroots.Resolve's first branch); what was broken is
// that the run then REPORTED a different channel, and that is what clause ②
// repairs. A reader who follows the label now reaches the variable that actually
// decided the path, and one that also learns its own declaration was outranked.
//
// Rule 15's line is "no silent failure", not "refuse everything": clause ② makes
// the disagreement VISIBLE in the string every caller already logs
// (cmd/agent's clone channel prints "workspace_root=<path> via <source>"), without
// taking a decision the operator did not ask for.
//
// NOT A NEW MECHANISM (Rule 35): no key, no file, no hook, no script. Two
// constants and one pure decision function, called by the ONE existing reader.
//
// Rule 1/13/14: both constants are COMPARED and RENDERED, and the override's
// label is an ALIAS of the resolver's own constant — never a second copy of the
// text, because a retyped literal is the mirror-literal defect Rule 13 bans.

import (
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// WorkspaceRootSettingsLabel names the settings channel in a returned
	// Source label. Spelled once because a label is both compared and rendered:
	// a caller and its log line must name one channel with one string.
	WorkspaceRootSettingsLabel = "settings " + WorkspaceRootSettingsKey

	// WorkspaceRootSourceDefault labels a workspace root that NOTHING declared
	// — the worktreeroots.DefaultRoot() answer. Constant because it is compared
	// (workspaceRootAnswer asks whether the answer it got was the default one)
	// and rendered ("a default is reported as a default").
	WorkspaceRootSourceDefault = "default"

	// WorkspaceRootSourceWorktreeEnv is the label of the ONE ambient channel the
	// root resolver consults before the value it is handed. It is an ALIAS of
	// the resolver's own constant, never a second copy of the text: the
	// comparison below must name the very string the resolver produces.
	WorkspaceRootSourceWorktreeEnv = worktreeroots.SourceSettingsEnv
)

// workspaceRootAnswer turns the ONE root resolver's answer into this package's
// (path, source) pair.
//
// root is worktreeroots.Resolve(declared)'s answer — the ONLY producer of the
// path — so the clauses below describe THAT answer rather than re-deciding
// precedence. declared is the value this package's own key carries ("" when
// neither the env channel nor the settings key set one), and label names which
// of those two carried it.
func workspaceRootAnswer(root worktreeroots.Root, declared, label string) (string, string, error) {
	switch {
	case root.Source == worktreeroots.SourceDefault:
		// Nothing declared and nothing overridden: the default IS the answer,
		// and the label says so (Rule 15's positive half).
		return root.Path, WorkspaceRootSourceDefault, nil
	case root.Source != WorkspaceRootSourceWorktreeEnv:
		// The ambient override did not decide it, so the resolver's answer is
		// this package's declared value — clause ③.
		return root.Path, label, nil
	case declared == "":
		// Only the ambient override is in play; the label names it.
		return root.Path, root.Source, nil
	}
	// The override decided the path AND a workspace_root value exists. ONE
	// DIRECTORY THROUGH TWO NAMES IS NOT A CONFLICT — the run is measured
	// against that directory either way, so the label names both channels and
	// claims nothing was outranked.
	if sameWorkspaceRoot(root.Path, declared) {
		return root.Path, root.Source + " and " + label, nil
	}
	// ONE KEY, TWO VALUES: the override is in force (its own branch, read
	// first) and the declaration is not. The label carries BOTH, because either
	// alone sends the reader to the wrong variable.
	return root.Path, root.Source + " (overrides " + label + "=" + declared + ")", nil
}

// sameWorkspaceRoot reports whether a declared value names the directory the
// resolver already returned. Both sides are reduced the same way —
// worktreeroots.ExpandTilde (the ONE tilde reader) then one lexical clean — so
// "~/ws" and "/home/x/ws" compare equal and no second normalization rule is
// invented here. The resolver has already refused a relative root, so both sides
// are absolute.
func sameWorkspaceRoot(resolved, declared string) bool {
	expanded, err := worktreeroots.ExpandTilde(strings.TrimSpace(declared))
	if err != nil {
		// A value the tilde reader cannot expand is not the resolved path; the
		// caller reports it as outranked rather than guessing it equal.
		return false
	}
	return filepath.Clean(expanded) == filepath.Clean(strings.TrimSpace(resolved))
}
