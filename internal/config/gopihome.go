package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GopiDirEnv is the environment variable that relocates GoPi's entire
// user-profile root (settings.json, sessions, raw logs, memory.db, plugins):
// when set, ~/.gopi becomes $GOPI_HOME/.gopi.
//
// One knob, one place: every gopi-data path resolution calls GopiDir instead
// of os.UserHomeDir, so a test (or a sandboxed run) sets ONE env var instead
// of patching HOME — and the real user profile is never touched. The CLI
// --home flag feeds this same variable via ApplyEnv (flags > env > settings).
const GopiDirEnv = "GOPI_HOME"

// SessionsDirName is the sessions directory name under the gopi data root
// (<gopi-dir>/sessions). ONE named constant (Rule 1/2 single source): every
// package that touches the sessions tree — internal/tools (task session
// containers sessions/<task_id>/, audit reads), internal/agent (session run
// logs), internal/gopilog (reader/scan), cmd/agent (task-run backfill) —
// joins this constant instead of spelling "sessions". The dir name is an
// infra contract: session-dir-unify S1 merged task containers + session logs
// + task-run children under one root, so a rename must hit every consumer —
// a literal miss silently points writes/reads at different roots.
const SessionsDirName = "sessions"

// ChatHistoryFileName is the chat line-editor's history file name under the
// gopi data root (<gopi-dir>/chat_history). ONE named constant (Rule 1/14):
// the interactive editor writes it and the next session reads it back, so the
// two sides must agree on the exact spelling — a second literal is the drift
// that silently yields an always-empty history.
const ChatHistoryFileName = "chat_history"

// ProfileDirName is the profile directory name under a home: the gopi data
// root is <home>/<ProfileDirName>. ONE named constant (Rule 1): the value is
// spelled by the resolution below AND by every reader that asks "which profile
// does this PROCESS belong to?" (cmd/agent's worker enumeration joins a
// process's own HOME with this name). A second spelling is the drift Rule 13
// closes — the two sides would stop agreeing and every worker would read as a
// foreign profile's.
const ProfileDirName = ".gopi"

// ProfileDirIn is THE spelling of the profile join: the gopi data root under a
// given HOME is <home>/<ProfileDirName>. It is exported because the join is
// needed by callers that already HOLD a home — the sandbox initializer, which
// must materialise the very directory GopiDir will later resolve when
// GOPI_HOME is that home (Rule 14: one definition, referenced by both halves;
// a second `filepath.Join(home, ".gopi")` is how the creator and the reader
// stop agreeing, which is the divergence this function exists to prevent).
//
// GopiDir and gopiDirFromEnv both route through it, so a rename of the profile
// leaf cannot leave one of the three spellings behind.
func ProfileDirIn(home string) string {
	return filepath.Join(home, ProfileDirName)
}

// ProfilePathIn is THE spelling of a path INSIDE a profile: a component (or a
// relative path) joined under the profile root of a given home. Every consumer
// that materialises or reads a profile-scoped entry — the sandbox initializer
// (sessions/, raw/, logs/, settings.json, memory.db) and every reader that
// already holds a home — goes through this ONE join (Rule 2/13/14).
//
// It exists because "the profile root is <home>/.gopi" was previously known
// only to config.GopiDir, while the code that CREATED a profile (the sandbox
// initializer) spelled the join itself and therefore materialised its entries
// one directory too high: the docs named directories the reader could not find.
// One function, referenced by both sides, is the fix.
func ProfilePathIn(home string, component string) string {
	return filepath.Join(ProfileDirIn(home), component)
}

// GopiHomeMisuseErrorPrefix is the FIRST LINE of every refusal of a near-miss
// GOPI_HOME. It is a named constant because three surfaces must agree on it:
// the CLI prints the error this package renders, and the regression test
// asserts the refusal by matching this sentence — a reworded prefix that the
// test still spelled out by hand would be exactly the drift Rule 1b/14 exist
// to prevent, and a refusal nobody can grep is a silent failure in disguise.
//
// The wording is "a profile, not a home" rather than "a sandbox's profile"
// because the predicate below refuses a LEAF of <profile leaf> under ANY parent:
// the sandbox spelling (`<sandbox>/.gopi`) and an ordinary home's own profile
// root (`<home>/.gopi`) are the SAME near miss — both resolve one level deeper —
// and the sentence must describe both (measured 2026-10-13: the operator's own
// profile root was the form that actually put rows in a second ledger).
const GopiHomeMisuseErrorPrefix = "refusing " + GopiDirEnv + ": it names a profile, not a home"

// SandboxNestedProfileOptOutEnv is the operator's deliberate opt-out from the
// nested-profile refusal below.
//
// WHY AN OPT-OUT EXISTS AT ALL: the refusal is a POLICY about one spelling, not
// a claim that a nested profile is unusable — an operator who really wants
// <sandbox>/.gopi/gopi/ (a profile inside a sandbox's own profile) may say so
// once, in their own environment, and be obeyed. Rule 25's shape: the
// degradation is ANNOUNCED, never silent, and asking for it is the operator's
// own deliberate switch.
const (
	// SandboxNestedProfileOptOutEnv is the env NAME; it sits in a const block
	// (rather than a bare `const X = …` line) so the config-doc gate's catalog
	// scanner can resolve this row to its value — the same shape every other
	// constant-referencing catalog entry uses.
	SandboxNestedProfileOptOutEnv = "GOPI_ALLOW_NESTED_PROFILE"
)

// NestedProfileOptOut is the operator's deliberate opt-out switch, read LIVE
// from the environment on every call.
//
// Live rather than memoised ON PURPOSE: it is a per-process policy switch that
// tests must be able to flip in both directions (t.Setenv, Rule 17 — the
// harness restores it, which a sync.Once could not), and a config package that
// froze an env answer for the life of the process would make the two arms
// order-dependent. The cost is one os.Getenv per resolution, which is not a
// measurable price beside the ledger and session path work each caller does.
// The full rationale for the switch itself is on
// SandboxNestedProfileOptOutEnv above.
func NestedProfileOptOut() bool {
	return strings.TrimSpace(os.Getenv(SandboxNestedProfileOptOutEnv)) != ""
}

// sandboxOwnershipMarkerPresent reports whether dir carries a sandbox's
// ownership marker — the proof that dir is a directory `gopi tool sandbox
// create` materialised. It is deliberately the ONLY test used to identify a
// sandbox: the marker is written by the ONE initializer, so a name-shaped guess
// ("anything under the sandbox root", "anything called sandbox-*") can never
// refuse a directory the operator made by hand.
//
// It is asked for by the MESSAGE's wording, never by the predicate: the guard
// refuses a value on the arithmetic alone, while the sentence it prints may say
// which measured symptom applies (a profile nested inside a sandbox, with the
// sandbox's own ledger and sessions left empty) — see GopiHomeMisuseMessage.
func sandboxOwnershipMarkerPresent(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, SandboxOwnerFileName))
	return err == nil && st.Mode().IsRegular()
}

// LookupSandboxProfileMisuse reports whether home is a NEAR MISS — a value that
// names a PROFILE rather than the HOME that profile lives in — and the canonical
// value the operator meant (the profile's parent, which is the home).
//
// A PATH WHOSE LEAF IS THE PROFILE LEAF IS ALWAYS THE NEAR MISS, whatever its
// PARENT is. That is the whole predicate, and the parent-independence is
// measured rather than reasoned: the first version of this guard asked its
// questions about the PARENT (a sibling sandbox marker, or a second profile
// leaf), and the arithmetic it was written to stop is exactly one level above
// that window — the operator's own profile root.
//
//   - `<sandbox>/<profile leaf>` (the spelling `gopi tool sandbox -h` prints for
//     that sandbox, and the one `gopi tool sandbox run` does NOT set): the
//     documented arithmetic (<GOPI_HOME>/<leaf>) resolves one level deeper, to
//     <sandbox>/.gopi/.gopi — invisible to SweepSandboxes, which sweeps a sandbox
//     ROOT's children, while the sandbox's own ledger and sessions stay empty;
//   - `<home>/<profile leaf>` (no marker anywhere): the SAME arithmetic applies
//     to the operator's OWN profile root, and the first version of this guard
//     deliberately let it through. Measured 2026-10-13 on host-b:
//     `GOPI_HOME=/home/user/.gopi` resolved to /home/user/.gopi/.gopi, whose
//     memory.db held 2 rows while the real ledger held 1376 — and `gopi task
//     submit` printed a normal `Task queued: <id>` with exit 0, so a row was
//     written into a ledger nothing on the host sweeps, dispatches from or reads
//     back. Silent failure is the defect (Rule 15), not the inconvenience;
//   - `<x>/<profile leaf>/<profile leaf>`: a profile INSIDE a profile. Refused by
//     the same leaf test, so a tree a pre-fix binary left behind cannot be used
//     to keep nesting.
//
// Everything else is left alone ON PURPOSE, and the exclusions are the
// load-bearing half of the design:
//
//   - `GOPI_HOME=<sandbox>` (the CANONICAL spelling, and the one `gopi tool
//     sandbox run` sets) is a sandbox and must keep working — a guard that
//     refused it would break the documented path it is protecting (measured: the
//     first version of this check did exactly that, exit 2 on the correct value);
//   - an ordinary home, and a home whose leaf merely CONTAINS the profile word
//     (`<home>/.gopi-backup`): the test is the whole leaf, never a prefix.
//
// The correction is the parent, and deriving it is not a guess: GOPI_HOME is
// documented as a HOME (GopiDirEnv) and `<parent>/<leaf>` is by construction the
// profile of the home `<parent>`, so `<parent>` is the value that reaches the
// profile the operator named. An operator who really means the value as written
// wants the profile's own profile — the deliberate switch
// (SandboxNestedProfileOptOutEnv) is the door for that, and it says so.
func LookupSandboxProfileMisuse(home string) (profile, canonical string, ok bool) {
	if home == "" {
		return "", "", false
	}
	clean := filepath.Clean(home)
	if filepath.Base(clean) != ProfileDirName {
		return "", "", false
	}
	return ProfileDirIn(clean), filepath.Dir(clean), true
}

// GopiHomeMisuseMessage renders the refusal for a GOPI_HOME that names a
// PROFILE rather than the home that profile lives in. It is one function so the
// CLI refusal and the library sentinel cannot word the same problem differently
// (Rule 14), and it always carries the three facts an operator needs: the value,
// why it is wrong (the resolution arithmetic), and the exact correction.
//
// The consequence clause is chosen by what can be MEASURED about the value, not
// by the arm that refused it: a value whose parent carries a sandbox marker
// resolves into a profile nested inside a SANDBOX (no owner marker, the real
// sandbox's ledger and sessions left empty, SweepSandboxes blind to it); any
// other value resolves into the `$HOME/.gopi/.gopi` shape an ordinary shell typo
// produces. Asserting the sandbox consequence for the second shape would be a
// made-up symptom (Rule 41: inference never stands in as a criterion).
func GopiHomeMisuseMessage(home string) string {
	profile, canonical, ok := LookupSandboxProfileMisuse(home)
	if !ok {
		return ""
	}
	consequence := fmt.Sprintf(
		"a SECOND profile (`%s`) holding its own ledger, sessions and plugins, "+
			"which no reader of the profile you meant will ever see",
		profile)
	if sandboxOwnershipMarkerPresent(canonical) {
		consequence = fmt.Sprintf(
			"a SECOND, nested profile inside that sandbox that no reclaimer models "+
				"(it has no %s, and the sandbox's own ledger and %s stay empty)",
			SandboxOwnerFileName, SessionsDirName)
	}
	return fmt.Sprintf(
		"%s=%s names a profile, not a home: the profile root is <GOPI_HOME>/%s, "+
			"so this value resolves to %s — %s."+
			"\nSet %s=%s instead — the same profile, reached the documented way.",
		GopiDirEnv, home, ProfileDirName, profile, consequence, GopiDirEnv, canonical)
}

// CheckoutMarkerName is the ONE name that identifies a GIT WORK TREE (a code
// checkout) on disk: a directory carrying this entry is a checkout, not a home.
//
// It is a named constant (Rule 1/14) because both halves of the guard below —
// the predicate that decides, and the tests that pin the decision — must agree
// on the exact spelling; a second literal is the drift that would silently turn
// the guard into a no-op.
const CheckoutMarkerName = ".git"

// GopiHomeNamesCodeCheckout answers ONE narrow question about a GOPI_HOME value:
// does it NAME A CODE CHECKOUT (a git work tree) rather than a home?
//
// WHY THIS EXISTS (epic: a child must never resolve a throwaway profile). GOPI_HOME
// is documented as a HOME, and the profile root is `<GOPI_HOME>/.gopi`. A run
// standing in its own git worktree can hand that directory down as GOPI_HOME —
// and then every descendant resolves `<worktree>/.gopi` as its profile: a
// throwaway profile that nothing sweeps, nothing dispatches from and nothing
// reads back, while the worktree itself goes DIRTY (the profile lands inside the
// checkout). Notify deliveries into it vanish silently and the merge queue then
// parks on "tracked files modified". Measured 2026-10-15: this run's own
// bash-tool child carried GOPI_HOME=<the run worktree>.
//
// THE TEST IS THE MARKER, NOT THE PATH SHAPE — deliberately. `$GOPI_HOME/.git`
// existing is a FACT about the value, so:
//
//   - a SANDBOX home (`GOPI_HOME=<sandbox>`, a real directory holding `.gopi`)
//     carries no marker and therefore keeps inheriting: the documented path the
//     sandbox machinery depends on is never refused;
//   - an ordinary home is not a checkout and is never refused;
//   - a worktree's common dir, a `git init`-ed directory and a bare repo (both
//     of which carry `.git` as a FILE) are all "names a code checkout".
//
// Rule 41 discipline: `.git` EXISTING is the criterion. "It must be a gitdir
// POINTER file" would be an inference about how worktrees are laid out, and it
// is not needed — the question is whether the value names a checkout, and the
// marker answers exactly that. The marker is CONFIRMED with a real `git` call
// where the language allows it (internal/standards' checkout-tree proof); the
// predicate itself stays a leaf filesystem test on purpose, because it runs in
// every spawned shell's environment assembly and must not fork a process.
//
// A relative value is never a checkout claim (it is not a home either, but it is
// a different defect), and an empty value means "nothing was published".
func GopiHomeNamesCodeCheckout(home string) bool {
	home = strings.TrimSpace(home)
	if home == "" || !filepath.IsAbs(home) {
		return false
	}
	_, err := os.Lstat(filepath.Join(home, CheckoutMarkerName))
	return err == nil
}

// GopiDir returns the gopi data root directory: $GOPI_HOME/.gopi when
// GOPI_HOME is set (env var OR the --home CLI flag, which ApplyEnv injects
// into the process env), otherwise $HOME/.gopi via os.UserHomeDir.
//
// When GOPI_HOME is set, os.UserHomeDir is never consulted (a test sandbox
// may have no real HOME at all — os.UserHomeDir would then error).
func GopiDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(GopiDirEnv)); v != "" {
		// The near miss is refused HERE, at the ONE resolution every consumer
		// goes through — not at each caller, where a single miss would let the
		// nested profile back in (Rule 14: one definition, one enforcement
		// point). The opt-out is the operator's deliberate switch.
		if msg := GopiHomeMisuseMessage(v); msg != "" && !NestedProfileOptOut() {
			return "", errors.New(GopiHomeMisuseErrorPrefix + "\n" + msg)
		}
		return ProfileDirIn(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	return ProfileDirIn(home), nil
}

// GopiHome returns the HOME the gopi data paths resolve against: the value
// of GOPI_HOME when set, otherwise the OS user home. For tilde expansion and
// workdir-sandbox checks — the few places that need the HOME ITSELF rather
// than the .gopi root. Prefer GopiDir for anything under the gopi profile.
func GopiHome() (string, error) {
	if v := strings.TrimSpace(os.Getenv(GopiDirEnv)); v != "" {
		return v, nil
	}
	return os.UserHomeDir()
}

// gopiDirFromEnv reads the gopi data root OUT OF A PROCESS'S OWN ENVIRONMENT
// (the /proc/<pid>/environ contents) — the same resolution as GopiDir, but
// applied to a process OTHER than the caller.
//
// It exists for ONE question, asked by the worker-liveness code: "does this
// running worker belong to MY profile?" (cmd/agent's worker enumeration). That
// answer must not depend on the worker's command line: argv is the operator's
// own text and a flag naming a root is a contract that can drift, whereas the
// environment is the process's actual configuration — a sandboxed worker's
// environ carries its sandbox's GOPI_HOME by construction, so one worker
// process can never be mistaken for another profile's. Returns "" when the
// environ carries no GOPI_HOME (the reader then falls back to the HOME entry,
// exactly as GopiHome does).
func gopiDirFromEnv(environ []string) string {
	home := ""
	for _, kv := range environ {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch key {
		case GopiDirEnv:
			if v := strings.TrimSpace(value); v != "" {
				return ProfileDirIn(v)
			}
		case "HOME":
			home = value
		}
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return ProfileDirIn(home)
}

// GopiDirFromEnviron exposes gopiDirFromEnv: a caller that holds another
// process's environment (a /proc/<pid>/environ read) resolves the profile the
// SAME way this package resolves its own (Rule 2/14 — one spelling of the
// join, so the two sides cannot disagree).
func GopiDirFromEnviron(environ []string) string { return gopiDirFromEnv(environ) }
