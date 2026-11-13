package devworkspace

// trace.go — the SOURCE-REPOSITORY FALLBACK (bug 01a1515c, second half).
//
// THE SITUATION IT EXISTS FOR. The clone channel refuses to run a task whose
// repo declares no build command, and that refusal is CORRECT: the command is
// never guessed (Rule 12), because a guessed build can compile packages the
// change never touched and would let unverified code reach master. But a
// reframe's roadmap child can be born naming its parent's WORKSPACE
// (/home/nick/workspace/<parent-id>) instead of the repository that workspace
// was cloned from. A workspace is a repository of its own — a plain CLONE, not a
// linked worktree — so it is declared nowhere in settings and every child died
// on that check (three of three, measured 2026-10-18).
//
// WHAT THIS DOES. When the row's repo falls INSIDE the workspace root (a fact,
// not a guess: that is where this channel itself puts its checkouts) it asks the
// checkout ONE read-only question — `git remote get-url origin` — and looks THAT
// path up in the same settings repos table. A declared source repository answers
// with its own build command; an undeclared one refuses exactly as before.
//
// THE FOUR RED LINES, each of which is a test:
//   - a repo OUTSIDE the workspace root is not traced at all: its answer, its
//     log and its git invocations are literally unchanged (TraceToSource is not
//     reached);
//   - every trace that is ATTEMPTED prints TraceLineFormat — a fallback nobody
//     can read in the log is a silent repair (Rule 15), so the line is printed
//     at the decision point, before the outcome is known;
//   - a FAILED trace still REFUSES — the caller keeps its original error
//     verbatim and only appends the trace's own words;
//   - NOTHING is cached: every refusal re-asks the checkout, so a repository
//     that gains an origin mid-run is seen, and a stale answer from a previous
//     run can never authorize a push.
//
// It writes nothing: the question is `git remote get-url` and the answer is a
// string.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// TraceLineFormat is the ONE spelling of the fallback's log line (Rule 1b).
	// It is printed whenever a trace is ATTEMPTED — before the settings lookup,
	// so a trace that finds nothing is as visible as one that succeeds.
	//
	//	source trace: repo %q lies under workspace root %q (%s) — asking it for its origin to find the repository that declares a build
	TraceLineFormat = "source trace: repo %q lies under workspace root %q (%s) — " +
		"asking it for its origin to find the repository that declares a build"

	// TraceOutcomeHitFormat reports a successful trace and the declaration the
	// run will actually use.
	TraceOutcomeHitFormat = "source trace: %s → origin %s → build declared in settings %s"

	// TraceOutcomeMissFmt reports a trace that found no declaration. The caller
	// keeps its own refusal; this line says WHY the fallback did not rescue it.
	TraceOutcomeMissFmt = "source trace: %s (%s) declares no build command in settings %s either — " +
		"the refusal stands (a build command is never guessed)"
)

// SourceTrace is what one trace attempt answered, for the caller's log line.
type SourceTrace struct {
	// Repo is the workspace path the row named.
	Repo string
	// Origin is what `git remote get-url origin` answered ("" when it refused).
	Origin string
	// Display is the settings spelling of Origin — the path the declaration is
	// keyed on and the one an operator can paste into a repair.
	Display string
	// Err is the trace's own failure ("no origin remote declared", "not a
	// work tree", a settings read error). Non-nil means NOTHING was rescued.
	Err error
}

// TraceToSource answers "which repository does this workspace come from, and does
// it declare a build?" for a repo that lies inside the channel's workspace root.
//
// ok=false means: not traceable, or traced and nothing declared — the caller
// refuses with its OWN error, unchanged. The returned rc is the DECLARED
// configuration of the source repository, exactly as ResolveRepoBuild would have
// returned it for that path.
func TraceToSource(ctx context.Context, cfg Config, repo string) (config.RepoConfig, SourceTrace, bool) {
	repo = strings.TrimSpace(repo)
	tr := SourceTrace{Repo: repo}
	if repo == "" {
		tr.Err = fmt.Errorf("the row names no repository")
		return config.RepoConfig{}, tr, false
	}
	// The gate is a FACT (this channel's own checkout location), so a path that
	// is not under it is never touched — that is what keeps a normal repo's log
	// and git invocations byte-identical.
	if cfg.Root == "" || !worktreeroots.Within(repo, cfg.Root) {
		tr.Err = fmt.Errorf("%s is not under the workspace root %q", repo, cfg.Root)
		return config.RepoConfig{}, tr, false
	}
	// PRINTED BEFORE THE OUTCOME IS KNOWN: an attempted trace is evidence, a
	// silent one is a repair nobody can audit (Rule 15).
	tracef(cfg.Log, TraceLineFormat, repo, cfg.Root, rootSourceLabel(cfg))
	origin, err := originURL(ctx, repo)
	if err != nil {
		tr.Err = err
		return config.RepoConfig{}, tr, false
	}
	tr.Origin = origin
	display, err := SourceRepoDisplayPath(origin)
	if err != nil {
		tr.Err = err
		return config.RepoConfig{}, tr, false
	}
	tr.Display = display
	rc, ok, err := config.ResolveRepoBuild(display)
	if err != nil {
		tr.Err = fmt.Errorf("reading the declaration for %s: %w", display, err)
		return config.RepoConfig{}, tr, false
	}
	if !ok {
		tr.Err = fmt.Errorf("%s declares no build", display)
		return config.RepoConfig{}, tr, false
	}
	tracef(cfg.Log, TraceOutcomeHitFormat, repo, display, config.ReposSettingsKey)
	return rc, tr, true
}

// RepoPathState names WHAT IS ACTUALLY AT a row's repo path, which is the
// question the clone channel was never asking (epic 01a15629). A row can name a
// directory that a PREVIOUS run's workspace reclamation deleted; such a path
// declares nothing in settings for the trivial reason that nothing is there, and
// reporting that as "declares no build command" told every operator the wrong
// thing (three measured rows: 01a154de, 01a1510e, 01a1513b).
//
// The two reasons are NAMED here (Rule 1b) so the refusal sentence rendered by
// the caller is spelled once and cannot drift from the probe.
const (
	// RepoPathMissingReason is the FIRST dead shape: nothing is on disk.
	RepoPathMissingReason = "the directory no longer exists on disk"
	// RepoPathNotGitReason is the SECOND: something is there, but it is not a
	// git work tree, so no build declaration can describe it.
	RepoPathNotGitReason = "the directory exists but is not a git work tree (it carries no .git entry)"
)

// RepoPathReadable asks the ONE question that decides which refusal sentence is
// TRUE: is this path a usable git work tree? It is deliberately NOT a build
// question — the declaration lives in settings and is asked separately, by
// config.ResolveRepoBuild. A path that answers false here CANNOT have a
// meaningful "declares no build command" verdict.
//
// It stats and nothing else: no git process, no settings read. A caller that
// only asks about the workspace root therefore keeps a normal repository's run
// exactly as it was.
func RepoPathReadable(dir string) (bool, string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false, RepoPathMissingReason
	}
	if _, err := os.Stat(dir); err != nil {
		return false, RepoPathMissingReason
	}
	if !isGitWorkTree(dir) {
		return false, RepoPathNotGitReason
	}
	return true, ""
}

// originURL is the ONE git question this package asks a workspace about its
// provenance: where was it cloned from. Read-only, bounded, and answered by the
// same runner every other git call in this package uses.
func originURL(ctx context.Context, dir string) (string, error) {
	if !isGitWorkTree(dir) {
		return "", fmt.Errorf("%s is not a git checkout (no .git)", dir)
	}
	out, err := runCommand(ctx, dir, gitBinary, "remote", "get-url", "origin")
	origin := strings.TrimSpace(out)
	if err != nil {
		return "", fmt.Errorf("%s declares no origin remote: %w", dir, err)
	}
	if origin == "" {
		return "", fmt.Errorf("%s declares an empty origin remote", dir)
	}
	return origin, nil
}

// SourceRepoDisplayPath renders the settings spelling of an origin URL: the
// repos table is keyed on the LOCAL checkout path (a repo the rebuild list can
// name), so an ssh/file URL is reduced to its path, and a relative one is
// ANCHORED at the working checkout — which is where git resolved it for the
// clone and therefore what the operator declared.
func SourceRepoDisplayPath(origin string) (string, error) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "", fmt.Errorf("empty origin")
	}
	if strings.HasPrefix(origin, "file://") {
		origin = strings.TrimPrefix(origin, "file://")
	}
	// git's ssh spelling: [user@]host:path — the path is what follows the FIRST
	// colon, and a host is anything that is not a bare absolute path.
	if i := strings.Index(origin, ":"); i >= 0 && !strings.HasPrefix(origin, "/") &&
		!strings.Contains(origin[:i], "/") {
		origin = origin[i+1:]
	}
	if !filepath.IsAbs(origin) {
		return "", fmt.Errorf("origin %q names no local checkout path", origin)
	}
	return filepath.Clean(origin), nil
}

// rootSourceLabel names who declared the workspace root, so the trace line never
// implies a root the operator did not choose (Rule 1b).
func rootSourceLabel(cfg Config) string {
	if s := strings.TrimSpace(cfg.RootSource); s != "" {
		return s
	}
	return "workspace root"
}

// tracef writes one trace line when the run has a log.
func tracef(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", args...)
}
