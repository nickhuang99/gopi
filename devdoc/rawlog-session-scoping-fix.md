<!-- doc-class: historical -->
# Task Report: raw log session-scoping — `gopi raw <id> 1 request` returning another session's request

Trigger: `~/work/claude/nick/what_is_request.txt` — "during the 1st request, why
user's input or prompt is not included? … see this command output: `gopi raw
c6985118 1 request`. what do we expect? should user's epic task or prompt being
included?"

## 1. What "request" means, from the HTTP protocol's point of view

An HTTP transaction is **one request message** (method + target + version +
headers + body) and **one response message** (status line + headers + body).
Nothing else. GoPi's raw logger records exactly the body half of those two
messages, with directional markers standing in for the parts HTTP would put in
the start line and headers:

| HTTP | raw log |
|---|---|
| `POST /v1/messages` + headers | `→ REQUEST` |
| request body (the full JSON) | the single line after the marker |
| `200 OK`, `Content-Type: text/event-stream` | `← RESPONSE` / `← CHUNK` / `HTTP-ERROR` |

So "a request" in an agent session **is** the whole JSON body the client sent —
`model`, `system`, `messages`, `tools`, `stream`, all of it. There is no smaller
or larger unit that is still "the request".

The user's own prompt is **not a separate protocol artifact**. It is
`messages[0]` of that body (a `role:"user"` content block). Two consequences
follow, and both are the honest answer to the trigger's questions:

1. "Is user's input + system prompt together the request?" — yes, plus tools and
   the model parameters. They are all fields of one HTTP request body.
2. "Is the user's request considered a 'prompt' then?" — no. GoPi's `--sysprompt`
   flag is named after the *system* prompt specifically. The user's input is a
   different field and was simply not surfaced by a dedicated view; the fix adds
   `--userprompt`.

One caveat that matters for the second question: GoPi appends its own directives
(a task-start checkpoint, an epic briefing) **into the same user content block**
as the user's text. So even a perfect tool cannot recover "just what the human
typed" from a raw log — that distinction only exists before the request is
built. `--userprompt` therefore prints the block *as sent*, suffixes included,
and says so in `gopi raw -h`.

## 2. Was the reported symptom a bug? Yes — but not where it looked

`gopi raw c6985118 1 request` on this machine returns the full 54 648-byte
round-1 body, and `messages[0]` starts with `[EPIC GOAL]`. The user input **is**
in the request, verbatim. That part is not a bug.

The bug is **attribution**. The raw log name is `gopi-raw-YYYYMMDD-HHMMSS.log` —
second-granularity wall clock — and `NewRawLogger` opened it with
`O_CREATE|O_APPEND`. Two gopi processes started inside the same second resolve
the *same* name and both append. `O_APPEND` makes both writers "succeed", so
nothing ever complains, and one file ends up holding two sessions' HTTP traffic
with no boundary. Round numbering is file-global, so `gopi raw <id> N` stops
meaning "session N's Nth send".

Measured against `~/.gopi/sessions` (1202 sessions, 657 recording `rawLogPath`):

- 582 distinct raw logs referenced; **56 referenced by more than one session**,
  2 of them with real traffic:
  - `gopi-raw-20260827-231907.log` → `044938be` + `77d67d9b`
  - `gopi-raw-20260829-092131.log` → `0cac8286` + `e7b5b5a9`
- Of 487 sessions with a non-empty first user event and a resolvable non-empty
  raw log, **2 (0.4%) had their user input absent from round 1** and present
  only in a later round of the shared file.

Reproduced exactly as described:

```
$ gopi log 77d67d9b | sed -n '/^TASK:/p'          # the session's own epic
TASK:
  [EPIC GOAL]
  NewAgent 函数定义在哪个文件哪几行？…

$ gopi raw 77d67d9b 1 request                     # what raw round 1 returns
[EPIC GOAL] 找到 assembleSystemPrompt 函数的定义，报告它在哪个文件哪一行…
```

The reading session's epic is nowhere in that round — it is in round 2 of the
shared file, because `044938be` (the sibling process that won the filename race)
sent first. This is precisely "the raw command misses user input, or epic task":
the tool did not drop anything, the file it was reading belonged partly to
someone else.

### Not the cause (checked and ruled out)

- **Request construction** — `MinimalCore` keeps `Messages`, so even a 1214-guard
  probe round carries the user's text.
- **A derived view filtering the body** — `RawRoundRequest` returns
  `RequestRaw` verbatim; `gopi log --http`'s digest also prints a preview.
- **Guard probes / compaction as round 1** — compaction cannot be round 1
  (there is no history yet); probes carry messages.
- **Timestamp pairing** — the primary resolver prefers the `rawLogPath` recorded
  in `session_start`; both failing sessions had the recorded path.

## 3. Fix

### `internal/agent/rawlog.go` — exclusive creation (root cause)

`createRawLogFile` opens the raw log `O_CREATE|O_EXCL|O_WRONLY|O_APPEND` and, on
`EEXIST`, walks a candidate ladder:

1. `gopi-raw-<ts>.log` — the bare, unchanged historical name.
2. `gopi-raw-<ts>-<n>.log` for n = 1..8 — readable names for the realistic
   handful-of-agents case.
3. `gopi-raw-<ts>-u<pid>-<n>.log` — seeded by an in-process `atomic` counter
   plus the pid, so it is unique by construction among an agent's siblings and
   across concurrent processes.

Ladder 3 exists so creation can **never** fail. That constraint is the design
point: without it, a busy second forces a choice between "share a file" (silent
wrong attribution) and "lose the raw log" (worse). A 40-goroutine probe confirms
40 agents in one second produce 40 distinct files and 0 shared.

One file now belongs to one session, which is what makes "round N of my raw log"
a meaningful statement at all.

### `internal/gopilog/rawlog.go` — accept the new names

`rawLogNameRe` matches the suffixed forms (`(?:-\d+|-u\d+-\d+)?`), so timestamp
pairing (`FindRawLogForSession`), `listRawLogFiles` and `rawLogStart` keep
working for logs created under the fix. Siblings share an embedded second, which
previously could not happen — `listRawLogFiles` now breaks ties by name, and
`FindRawLogForSession`'s ambiguity error names the collision instead of
returning an arbitrary pick.

### `cmd/agent/cli_raw.go` — say so when a log is shared

Historical shared logs cannot be un-interleaved. `warnSharedRawLog` counts the
sessions whose `session_start` records the resolved path
(`gopilog.SessionsRecordingRawLog`) and, when that is more than one, prints one
stderr line: the file name, how many sessions, the other session's short id, and
the consequence (round numbers are NOT session-scoped). stdout stays byte-pure —
verified byte-identical to the pre-fix build for all three sessions above. The
scan reads one bounded line per session file (~16 ms over 1202 sessions), which
is far below the cost of parsing the raw log the view prints anyway.

`--file` mode does not warn: it explicitly bypasses session pairing, and its
round numbering was always file-global.

## 4. Tests

| Test | Pins |
|---|---|
| `agent/TestNewRawLogger_SameSecondProcessesGetDistinctFiles` | two loggers in one second get distinct paths; the second's bytes never land in the first's file (failed before the fix) |
| `agent_test/TestRawLogger_RacingAgentsKeepSeparateRounds` | end-to-end: two racing agents, each file's round 1 via `gopilog.ParseHTTPRounds` carries **its own** request |
| `gopilog/TestRawLogStartParsingCollisionSuffix` | `-<n>` and `-u<pid>-<n>` names parse to the same embedded second; malformed suffixes are rejected |
| `gopilog/TestFindRawLogForSessionCollisionSibling` / `…Ambiguous` | suffixed logs stay reachable by timestamp pairing; same-second siblings error rather than guess |
| `gopilog/TestSessionsRecordingRawLogSingle` / `…Shared` / `…IgnoresNoise` | attribution scan: one owner, two owners, unparseable siblings skipped |
| `gopilog/TestFirstUserMessage` / `TestFormatRequestUserPromptVerbatim` | both wire shapes (string content, block array); absence reported, not "" |
| `cmd/agent/TestWarnSharedRawLogNamesTheOtherSessions` / `…SilentForSingleOwner` / `…ToleratesUnreadableSessions` | one stderr line naming the other session, stdout empty; silent in the normal case |
| `cmd/agent/TestRawUserpromptOverFixture` | `--userprompt` output equals the first request's user message, no decoration |
| `cmd/agent/TestParseRawArgs*` | `--userprompt` parses; mode conflicts rejected |

## 5. Not done

- `gopi stats <id> --rounds` does not emit the shared-log diagnostic. It shares
  raw round numbering with `raw` and would benefit, but `buildStats` has no
  stderr convention and is called from golden tests; left for a follow-up.
- `--userprompt` is first-request only, mirroring `--sysprompt`. Per-round user
  input stays `gopi raw <id> N request | jq '.messages[-1]'`.
- No backfill/repair of existing shared logs: interleaved bytes cannot be
  re-attributed after the fact, so the diagnostic is the honest answer.
