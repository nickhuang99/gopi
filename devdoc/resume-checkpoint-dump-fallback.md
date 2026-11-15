<!-- doc-class: normative -->
# The resume checkpoint has TWO homes — the raw log, and the crash dump beside it

**Row:** 01a183a8 · **Scope:** `gopi session resume` / `gopi --session <id>` checkpoint load.

## The defect, as measured

A run whose **first** HTTP request died at the transport layer leaves a session
directory like this:

```
$ wc -c <run>.raw.log
131          # or 167 with the pid=… tail — a header line and NOTHING else
$ python3 -c 'print(list(json.load(open("<run>.dump.json")).keys()))'
['category','elapsed_sec','error_body','failed_iteration','http_status','iterations',
 'last_request_digest','recent_tool_calls','recoverable_hint','request','session_id',
 'timestamp','total_tokens','type']
```

`failed_iteration: 0`, `category: "network"`, `http_status: 0`,
`error_body: … dial tcp … i/o timeout` — and `request` holds the **complete**
body (model / max_tokens / system / messages / tools).

`LoadCheckpoint` scanned `raw.log` for a `→ REQUEST` marker and, finding none,
answered:

```
resume: no → REQUEST checkpoint found in …/<run>.raw.log
```

That sentence is about the FILE, and it reads as a sentence about the CONTEXT.
The context was on disk in the same directory and was never opened. The reason
the raw log is empty is structural, not accidental: the raw logger writes its
`→ REQUEST` marker only once a request body is handed to the transport, so a
failure **before** the first write leaves the header alone.

## The fix — one decision point, two readers

```
LoadCheckpointWithSource(rawLogPath) → (req, history, systemTexts, CheckpointProvenance, err)
        │
        ├── scanRawLogCheckpoint: last "→ REQUEST" payload, plus the file's size
        │        └── non-empty  → checkpointFromBody  (source = raw log → REQUEST)
        │
        └── empty → loadCheckpointFromDump → checkpointFromRequest (source = crash dump request)
                        └── nothing replayable → resumeNoCheckpointError
```

* **`internal/agent/resume.go`** keeps the scan, gains the source decision and
  the shared validator `checkpointFromRequest` (message presence, the message
  bound, the tool_use/tool_result pairing invariant) — so a second SOURCE can
  never skip a CHECK.
* **`internal/agent/resume_dump_fallback.go`** owns the dump reader:
  `dumpCheckpointCandidates` names the three file shapes this host really
  carries (`<run>.dump.json`, the bare-session `session.dump.json`, then any
  other `*.dump.json` that parses), and `readDumpCheckpoint` refuses a document
  that is not `type: crash_dump`, has no request, or whose request was
  **head-trimmed** by `tailJSON` (a replay aid, deliberately not JSON).
* **`cmd/agent/cli_resume.go`** announces the fallback with the artifact, the
  file, `failed_iteration`, the category and the recovered body's size — a
  recovery from a second source is stated, never silent (Rule 15).

`CheckpointProvenance` is a struct rather than a bare enum because the operator
line needs the numbers, and rendering them is the FACE's job, not the loader's.

## `failed_iteration` is a report, not a switch

The persisted request already **is** the context the run died on. A dump with
`failed_iteration=0` (first request, transport failure) and one with
`failed_iteration>0` (a later round) recover identically — the recovered
`messages` seed the resumed context verbatim. The number is printed so the
recovery is *understood*, not to select a rebuild path nobody needed.

## Verification (this rev)

| check | result |
|---|---|
| `go build ./...` | clean |
| `go test ./internal/agent/ -run TestLoadCheckpoint` | ok |
| `go test ./cmd/agent/ -run TestSessionResume` | ok |
| red-before | pre-fix shim (same API, old behaviour): 4 internal + 2 CLI tests FAIL with the old sentence |
| real CLI, header-only log + dump | `holds no → REQUEST checkpoint … recovered from the crash dump request …/…dump.json (failed_iteration=0, category=network)` |
| real CLI, header-only log, **no** dump | `no → REQUEST checkpoint in … (131 bytes …) and no replayable crash dump beside it — tried …/…dump.json — unreadable (…); …/session.dump.json — unreadable (…)` |

## What this deliberately does NOT do

* It does not repair HP's network, nor retry the transport failure: the goal is
  that a *resume* re-issues the recovered request, which it does by seeding the
  context (the first resumed round rebuilds the wire body from that context).
* It does not read the `*.jsonl` tail. The dump already carries the complete
  body; a second reconstruction path would be a second answer to one question
  (Rules 13/14), and the jsonl's `request` events carry token counts only.
* It does not touch a dump whose request was head-trimmed at 1 MiB — replaying a
  truncated context would send a body the API has never seen.
