package replay

// session_ground.go — WHERE a session's own artifacts live, resolved by the SAME
// code every other session reader uses.
//
// WHY THIS FILE EXISTS (Rule 14). The film pipeline used to resolve its input
// twice: a SHELL driver re-implemented the join
// `<GOPI_HOME|$HOME>/<.gopi>/sessions/<id>` in shell (gopi_sessions_dir) and then
// scanned that one level with a shell prefix match, while `gopi session log`, `gopi
// stats`, `gopi session raw` and the agent all resolve through
// internal/gopilog.ResolveSessionDir + FindSession. Two answers to "where is this
// session's directory" is exactly the drift the single-source rule closes — and
// it had already drifted OBSERVABLY: the shell scan only accepted a directory
// holding `session.jsonl`, so a WORKER session (task-run layout,
// `<sessions>/<task_id>/<run-uuid>.jsonl` — 80,985 such containers on this host,
// measured 2026-10-03) was invisible to that shell scan, even though the record is
// the same session.jsonl-shaped log.
//
// WHAT A SESSION'S ARTIFACTS ARE, AND WHY THE DIRECTORY IS THE ONE FROM THE
// RECORD'S OWN LAYOUT. A recording's film is `artifacts/replay/<stamp>/` under
// the SESSION DIRECTORY — the same subdirectory a chat's hand-written artifacts
// already land in (internal/tools.ChatSessionArtifactsDirName, which is an alias
// of sessionlayout.ArtifactsDirName: ONE definition, two writers). The session
// DIRECTORY is the container the record lives in, and it is the one thing all
// three layouts have: the bare layout's `<uuid>/session.jsonl` gives it directly,
// the flat layout's `<uuid>.jsonl` sits at the sessions root (whose directory is
// the tree itself, so files there would be litter among 80k siblings — see
// inSessionsTree), and a task-run layout's per-run file sits in the task's own
// container, which is where that run's artifacts belong.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

const (
	// replayDirName is the subdirectory of a session's artifacts area that holds
	// filmed recordings. A session may be replayed many times, so this is a
	// NAMESPACE and not the artifact itself: see artifactStamp.
	replayDirName = "replay"

	// stampLayout is a film directory's name — a UTC timestamp, sortable and
	// unique to the second. Two runs of the same session therefore land in two
	// directories and neither overwrites the other's replay.json or GIF.
	stampLayout = "20060102-150405"

	// stampAttempts bounds the "-N" disambiguation of two recordings started in
	// the SAME second (a loop, a test). It is a bound on a genuinely absurd
	// input, not a cadence: exhausting it means 256 films of one session inside
	// one second, which is a diagnosis to write, never a number to raise.
	stampAttempts = 256
)

// SessionGround is a session resolved to the two paths its readers need: the
// RECORD (the session.jsonl-shaped log) and the session DIRECTORY that holds it,
// which is the parent of that session's artifacts area.
type SessionGround struct {
	// Record is the session record FILE — the log the replay renders.
	Record string
	// Dir is the SESSION DIRECTORY: the container the record lives in, and the
	// directory whose `artifacts/` subdirectory belongs to this session.
	Dir string
}

// ArtifactsDir is the directory this session's own artifacts live under (the
// name is sessionlayout.ArtifactsDirName — never re-spelled here).
func (g SessionGround) ArtifactsDir() string {
	return filepath.Join(g.Dir, sessionlayout.ArtifactsDirName)
}

// FilmDir is where a film STARTED AT now lands: `<session>/artifacts/replay/<stamp>`,
// disambiguated with a "-N" suffix when a film of this second already exists.
// It never returns a directory that exists, so a second replay of the same
// session cannot overwrite the first one's record — the collision is resolved by
// naming a NEW directory, never by writing over an old one.
func (g SessionGround) FilmDir(now time.Time) (string, error) {
	if err := os.MkdirAll(filepath.Join(g.ArtifactsDir(), replayDirName), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Join(g.ArtifactsDir(), replayDirName), err)
	}
	stamp := now.Format(stampLayout)
	for i := 0; i < stampAttempts; i++ {
		name := stamp
		if i > 0 {
			name = fmt.Sprintf("%s-%d", stamp, i+1)
		}
		dir := filepath.Join(g.ArtifactsDir(), replayDirName, name)
		// One stat, two outcomes, each its own guard clause: a directory that
		// EXISTS is "taken" (keep looking), and any error that is not "does not
		// exist" is a real failure to report rather than a name to skip past.
		_, statErr := os.Stat(dir)
		if statErr == nil {
			continue
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat %s: %w", dir, statErr)
		}
		if err := os.Mkdir(dir, 0o755); err != nil {
			if os.IsExist(err) {
				continue // another run took it between the stat and the mkdir
			}
			return "", fmt.Errorf("create %s: %w", dir, err)
		}
		return dir, nil
	}
	return "", fmt.Errorf("no free film directory under %s after %d attempts at stamp %s",
		filepath.Join(g.ArtifactsDir(), replayDirName), stampAttempts, stamp)
}

// resolveSessionGround resolves an OPERAND — a session id (full, or the leading
// characters of one) or a path to a session directory / record file — to its
// record and its session directory.
//
// The ID path is internal/gopilog's: FindSession, over the sessions directory
// ResolveSessionDir computes, across all three layouts. This function does not
// search the tree itself and does not compose a sessions path of its own: it
// asks the resolver every other reader asks, so `gopi chat --replay <id>` and
// `gopi session log <id>` cannot disagree about which recording an id names.
//
// A PATH is taken as given (the operator spelled it). A path under the sessions
// root names a session like any other and gets that session's own artifacts
// directory; a path OUTSIDE the tree (a checkout, a fixture) has no session
// container to write into, so its film directory cannot be derived — see
// inSessionsTree, and the caller refuses rather than depositing into a tree the
// ledger does not own.
//
// ONE RESOLVER, TWO ROUTES (2026-10-03). This is the resolver behind the ONE
// remaining replay route, `gopi chat --replay <session>`: the chat target is
// resolved by gopilog.ResolveSessionDir + FindSession too
// (cmd/agent/chat_replay.go's resolveReplayRecordingPath, which reads the same
// two calls rather than composing a sessions path of its own), so an id that
// names a recording names the SAME file for both. The `gopi replay session`
// PRE-STEP that used to print record=/dir=/artifacts=/film= for a shell driver
// is gone with the verb: there is no shell driver left to ask it, and a session
// is named by the flag, not by a subcommand.
func resolveSessionGround(operand string) (SessionGround, error) {
	if operand == "" {
		return SessionGround{}, fmt.Errorf("empty session operand")
	}
	if isPathOperand(operand) {
		record, dir, err := recordForPath(operand)
		if err != nil {
			return SessionGround{}, err
		}
		if !inSessionsTree(dir) {
			return SessionGround{}, fmt.Errorf("%s is outside the sessions tree (%s), so it has no session container to hold its artifacts — replay a session of this profile, or pass a path under the sessions root",
				dir, sessionsRootOrEmpty())
		}
		return SessionGround{Record: record, Dir: dir}, nil
	}
	root, err := gopilog.ResolveSessionDir()
	if err != nil {
		return SessionGround{}, fmt.Errorf("resolve sessions dir: %w", err)
	}
	record, err := gopilog.FindSession(root, operand)
	if err != nil {
		return SessionGround{}, fmt.Errorf("%s: %w", operand, err)
	}
	if record == "" {
		return SessionGround{}, fmt.Errorf("%s names no session under %s — a session id is the FULL id (the one `gopi session log --last` prints); a truncated id is refused rather than guessed",
			operand, root)
	}
	dir := sessionlayout.RecordContainer(record)
	if !inSessionsTree(dir) {
		// The flat layout's record sits AT the root: the tree itself is its
		// container, and writing artifacts/ there would put a session's film
		// among the tree's own 80k members.
		return SessionGround{}, fmt.Errorf("%s is recorded in the FLAT layout (%s lives directly under the sessions root), so it has no session container of its own to hold artifacts — this session cannot be replayed to its own directory",
			operand, record)
	}
	return SessionGround{Record: record, Dir: dir}, nil
}

// isPathOperand reports whether the operand is a PATH rather than an id: it
// contains a separator, or it names something that exists. A bare id never does
// either, and an id is never a valid path.
func isPathOperand(operand string) bool {
	if strings.ContainsRune(operand, filepath.Separator) {
		return true
	}
	_, err := os.Stat(operand)
	return err == nil
}

// recordForPath resolves a PATH operand to (record file, session directory),
// accepting a session directory, a record file (session.jsonl or a task-run
// `<uuid>.jsonl`), or the sessions root itself.
func recordForPath(p string) (string, string, error) {
	st, err := os.Stat(p)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", p, err)
	}
	if !st.IsDir() {
		// A record FILE: the layout's own accounting decides which file in its
		// directory is THE record (sessionlayout.RecordInDir), so a path to a
		// side file is not silently accepted as one.
		dir := sessionlayout.RecordContainer(p)
		if rec, ok := sessionlayout.RecordInDir(dir); ok && rec.Path == p {
			return rec.Path, dir, nil
		}
		return "", "", fmt.Errorf("%s is not a session record — no session is recorded in %s (expected %s, or a per-run <uuid>.jsonl)",
			p, dir, sessionlayout.RecordName)
	}
	if rec, ok := sessionlayout.RecordInDir(p); ok {
		return rec.Path, p, nil
	}
	return "", "", fmt.Errorf("%s holds no session record (no %s, and no per-run <uuid>.jsonl)", p, sessionlayout.RecordName)
}

// inSessionsTree reports whether dir is a session CONTAINER: a directory inside
// the gopi sessions tree, one level below its root. The root itself is NOT a
// container (the flat layout's records live there, and the root is a shared
// tree, not a session).
//
// An unresolvable sessions root answers false: without a tree there is no
// container to name, and the caller refuses by name rather than inventing a
// destination.
func inSessionsTree(dir string) bool {
	root, err := gopilog.ResolveSessionDir()
	if err != nil {
		return false
	}
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	if dir == root || !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return false
	}
	// Exactly ONE level below the root: anything deeper is a directory INSIDE a
	// session container (its own scratch), not a container.
	rel := strings.TrimPrefix(dir, root+string(filepath.Separator))
	return rel != "" && !strings.ContainsRune(rel, filepath.Separator)
}

// sessionsRootOrEmpty is the sessions root for a message, or "" when it cannot
// be resolved — a refusal that cannot name the tree says so rather than printing
// a guess.
func sessionsRootOrEmpty() string {
	root, err := gopilog.ResolveSessionDir()
	if err != nil {
		return "(sessions tree unresolved)"
	}
	return root
}
