# Long-horizon by design (not a memory plugin)

Most agents bolt memory on as a plugin and start a fresh session when context
runs out. gopi treats continuity as a runtime property. Below, every number was
measured on this tree or in a real running session — no claims from docs.

## 1. One binary; a new build is live in one restart

There is no install step and no interpreter to update. Swapping to a new build
is `/quit`, start `gopi chat`, resume the session — seconds, not a migration.

```console
$ stat -c '%s' bin/gopi
76432065
```

A single 76 MB static file is the whole deployment. (The release binary is
`statically linked`; see innovation-showcase §7.) An interpreter-based tool
updates a dependency tree; this replaces one file.

**Boundary:** updating means replacing the binary and restarting. In-flight
tool calls in a running turn do not hot-swap; they finish on the old build.

## 2. Hundreds of sessions, one continuous history

Continuity is measured across thousands of recorded sessions, not one chat.

```console
$ find ~/.gopi/sessions -mindepth 1 -maxdepth 1 -type d | wc -l
4890
$ for f in ~/.gopi/sessions/*/session.jsonl; do wc -l < "$f"; done | sort -rn | head -1
25636
```

One session log reached 25,636 lines and kept working. The largest runs are not
exceptions — they are the mode this harness is built for.

## 3. The 200K window is a deployment choice, not a model ceiling

This is the most misread number, so here it is in full. The underlying model
advertises a 1M-token context. gopi deliberately runs a ~200K window because
prefill cost, not context size, is the binding constraint.

```console
$ grep -nE 'DefaultContextWindow = |DefaultCompactTriggerRatio = ' internal/agent/context.go
18:const DefaultContextWindow = 200000
32:const DefaultCompactTriggerRatio = 0.75
```

The source states the reasoning: a 290K-token prefill measured ~16s, so single
tasks run in a ~200K effective window; at 0.75, compression runs once at 150K
instead of letting history grow to ~810K and re-prefilling a huge tail each
late round.

**This is a judgment we made, not a limit imposed on us.** Writing "200K" alone
would misrepresent it as our ceiling; the model supports more. The choice is
about interactive round time.

## 4. Memory is queried from SQLite, not loaded from files

There is no memory plugin to install and no `memory.md` to rewrite on exit.
Memory lives in one SQLite database and is reached by full-text search.

```console
$ stat -c '%s' ~/.gopi/memory.db
1122205696
$ grep -nE 'EMBEDDED dictionary|go-ego/gse' internal/cjkseg/cjkseg.go
2:// wrapper around the pure-Go `github.com/go-ego/gse` engine, loaded from its
3:// EMBEDDED dictionary so no data file ever has to exist beside the binary.
31:	"github.com/go-ego/gse"
```

Chinese word segmentation is `gse` with a dictionary compiled into the binary —
no data file beside it, no separate service. The same segmenter feeds both the
memory writer's index column and the query side, so index and query cannot drift.

**Session continuity, as a design difference:** some harnesses carry a session
across restarts by writing memory files on exit (`memory.md`, "important notes")
and reading them back into a new context. gopi writes a one-line self-record each
round into SQLite and recalls relevant entries by FTS5 at the start of the next
turn. Two different designs — neither is asserted here to be a defect in the other.

## 5. Every round records a decision that survives compaction

```console
$ wc -l ~/.gopi/sessions/<SESSION-ID>/self-record.md
60
```

This file is written by the model itself: at each round boundary the agent appends
one `[IMPORTANT]` line — a distilled conclusion worth keeping. It is not produced
by a daemon or a hook outside the model; the acting model writes it as part of its
own turn.

On resume the file is the truth source. The in-memory note list is rebuilt from it
once at initialisation (`restoreSelfRecordNotes` in
`internal/agent/selfrecord_reload.go`), so a session continued by a different
process does not start with an empty list while the file still holds every earlier
note. When compaction drops the raw conversation, those notes are carried back to
the head of the context — which is why a decision from a hundred rounds ago still
changes behaviour now.
