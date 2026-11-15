<!-- doc-class: normative -->
# End-user audit protocol — how to make gopi its own second user

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Status: protocol + first sweep (2026-09-26). Owner: the end-user-audit epic
(`01a0dced-ca8a-77f1-8820-8de8a6e023e8`, goal "let gopi be user").

The problem this answers: gopi has **no user audit** — the only human is the
single tester, and every agent that touches gopi reads its *source*. A
developer never uses their own feature; a programmer codes but never thinks as
the user. This document is the method that closes that gap, written so a fresh
task can run one slice of it and submit what it finds.

## The method — four steps, in this order

1. **Read the help, not the source.** `gopi --help`, then the slice's
   `<verb> -h`. Write down what you *expect* purely from the help text — this
   expectation is the test oracle. Reading the implementation first destroys
   the oracle: you would then be testing the code against itself.
2. **Run it for real, with real arguments, against real state** (PROJECT.md Rule
   31: a surface you did not execute is not delivered). In a sandbox — see
   the isolation section; a probe must never touch the operator's live ledger.
3. **Compare** the observed output / exit code / side effect against step 1's
   expectation. A divergence is the finding (this is what the help-vs-truth
   gates in `internal/standards/` cannot see: they check that help *exists*,
   not that it is *true to a user's experience*).
4. **File it** — `task_submit` (tags `bug`) with this exact shape:

   | step | what goes in the payload |
   |---|---|
   | expectation | the help line(s) you read, quoted verbatim |
   | invocation | the exact argv, verbatim, with real arguments |
   | observed | stdout/stderr + exit code, quoted |
   | contract | the source/report line that says what SHOULD happen |
   | acceptance | how a fix proves itself (the smallest repro + the check) |

   A finding with no quoted invocation is not a bug report, it is an opinion.

### Finding taxonomy (used below and expected in each slice's report)

- **F1 — false statement**: help text or an output message asserts something
  that is not true in the situation the user is in.
- **F2 — undiscoverable/silent divergence**: the user must read source to get
  the real behaviour; nothing on the surface says it.
- **F3 — vocabulary drift**: two parts of the same surface disagree on the
  accepted vocabulary of one knob.
- **F4 — error/exit-code fidelity**: the refusal, its message, or its exit
  status does not match the documented contract.
- **F5 — claim unverified**: a message that carries no citation to the fact it
  states (a user cannot tell "true" from "unchecked").

## Isolation — a user probe is not allowed to be a real user

| do | why |
|---|---|
| `gopi sandbox create --label <slice> --task-id "$GOPI_TASK_ID"` | the ONE initializer (Rule 34) |
| `GOPI_HOME="$SB"` / `gopi sandbox run …` | a private profile: sessions, ledger, logs stay inside |
| point `--db` at a scratch DB when the probe writes | never the operator's append-only ledger (Rule 32) |
| keep API keys out of reach (`--base-url http://127.0.0.1:9`) | **a probe that looks like a task becomes a run**: any positional argument is a real prompt, so `gopi frobnicate` starts a real agent session (measured: this epic's first probe spent 336 921 tokens on the unknown-verb string `frobnicate` and wrote a real ledger row). A user audit that burns tokens or pollutes the ledger *is itself* the bug it was looking for |

Recorded incident for the last row: the probe was issued from an **un-sandboxed**
shell, so `frobnicate` was read as a prompt, the agent resolved it as an epic,
returned `IMPOSSIBLE`, and registered row `01a0dcee-ee1d-702d-835b-1d01739b43bd`
in the live ledger. Nothing was damaged; the isolation probe that followed
(same input, sandboxed) wrote **only** to the sandbox DB. Treat the sandbox as
mandatory, not as a nicety.

**And mind WHERE you put the flag** (added by the fourth sweep, finding RS-1): a
probe written as `gopi --base-url http://127.0.0.1:9 resume <target>` does not
exercise `resume` at all — the flag prefix routes the whole line into the run
path, the verb becomes the PROMPT, and the probe spends a real run while proving
nothing about the verb. Probe the verb at `argv[1]`; pass run flags only when the
run path is what you are auditing, and check the dispatch actually happened
before you read the output.

## The sweep — slice map (each slice is one task)

Every verb the top-level help advertises, in the nine groups `gopi --help`
already renders (help is the index; each row is the slice's entry point).

| # | slice | verbs | state |
|---|---|---|---|
| 0 | **help & top-level surface** | `--help`, `--help-env`, `-h`, unknown-flag/verb, `version` | **swept 2026-09-26** (this document) |
| 1 | task ledger | `task <sub>`, `worker`, `merge`, `merger`, `archive` | **swept 2026-09-26** (§Fifth sweep; 141 probes, 5 findings filed `01a0dd4f-52ea`/`-531e`/`-5360`/`-b1fe`/`-b253`) |
| 2 | session inspection | `log`, `stats`, `raw`, `checkpoint`, `resume` | **swept 2026-09-26** (§Third + §Fourth sweep) |
| 3 | chat & web | `chat`, `web`, `ui`, `ui_state`, `cli`, `proxy` | **swept 2026-09-26** (§Sixth sweep; 88 probes, 7 findings filed `01a0dd87-8242`/`-8283`/`-82d5`/`-8315`/`-8351`/`-838f`, plus `01a0dd8f-c014` from the post-sweep check) |
| 4 | tools | `code-search`, `vision_analyze`, `text2img`, `capture`, `screen_capture`, `act`, `voice_input`, `shot` | partly probed (§Sweep) |
| 5 | memory & knowledge | `memory <sub>`, `memsync`, `brew` | **swept 2026-09-26** (§Seventh sweep; 142 probes, 4 findings filed `01a0de51-5959`/`-59ab`, `01a0de52-ff2c`/`-ff78`) |
| 6 | config & maintenance | `config`, `setup`, `completion`, `scratch-gc`, `sandbox` | **swept 2026-09-26** (§Sandbox) |
| 7 | environment table | `--help-env` (241 lines) vs `CONFIG.md` vs `config show` | TODO |


Per-slice acceptance: a report naming ≥1 finding in the taxonomy with the five
step-4 fields filled, or an explicit "no divergence found in these N probes"
listing every probe run (a clean slice is a result, not a failure).

## First sweep — help & top-level surface (2026-09-26)

Binary: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` at `79807932`,
run from the epic's worktree. Probes: 33 verbs × `-h` (all rc=0), 12 error
paths, `--help`/`--help-env`/`version` shapes.

### What held up (positive results, worth not re-testing)

- **Discovery completeness**: all 33 advertised verbs answer `-h` with rc=0;
  the dispatch list (27 `CliVerb*` constants) and the rendered index agree.
- **Error fidelity (F4)**: 22 of 24 malformed invocations exit 1 with a
  one-line reason *and* the verb's usage — `task list` on a bad status, an
  unknown `task` subcommand, `code-search --bogus`, `completion tcsh`,
  `act teleport`, `proxy --upstream telegram`, `capture --area 99999x99999`
  (refused against the real display geometry). `gopi <unknown-verb>` is not
  refused at all: an unknown word is a *prompt by design*.
- **Input-contract fidelity**: `gopi` with no prompt reads empty stdin and
  prints the help + exits 1, exactly as the Usage block claims.
- **Generators produce valid artifacts**: `completion bash|zsh|fish` emit
  scripts and `completion bash | bash -n` parses clean.

### Findings

**TS-1 (F1, medium) — `gopi version` states a falsehood in a linked worktree.**
Every agent runs in a linked worktree, so this is the *common* case, not an
edge case.

```
$ pwd                                  # a linked worktree of /home/user/work/GoPi
$ bin/gopi version
v0.1.0
go: go1.24.5
platform: linux/amd64
revision: unavailable — this build carries no VCS stamp (built outside a git checkout)
```

The build ran **inside** a git checkout (`git rev-parse --is-inside-work-tree`
→ true, gitdir = `/home/user/work/GoPi/.git/worktrees/…`). Go deliberately
embeds no `vcs.*` stamp for a linked worktree — reproducible with zero project
code: `git init` + one commit + one 1-line `cmd/agent/main.go` → main checkout **3**
`vcs.*` keys, its linked worktree **0**, same `go1.24.5`. Evidence:
`evidence-version-stamp.txt` in this run's session dir.

The message names the wrong cause, and the wrong cause is the one a user would
act on ("am I even in my repo?") — the real fact is "the toolchain skipped the
stamp because the build ran in a linked worktree".

Second-order, and the reason this is not cosmetic: `revision:` is the ONE key
the post-merge rebuild verifier recovers a revision from
(`cmd/agent/cli_version.go:25`, read by `gopiversion.RevisionOfDisplay` at
`internal/tools/rebuild.go:277`). A binary built anywhere but the declared main
checkout carries none, and the verifier cannot tell "honestly unstamped" from
"wrong binary". Known and deliberate (report `01a0d0c3…`): the close-loop
builds with `cmd.Dir` = the main checkout, so the verifier still resolves a
revision. Not re-litigated here — the string is the actionable defect.

Acceptance for a fix: `gopi version` distinguishes the two absences (not a git
checkout at all vs a linked worktree), the string is produced by a reader that
`go version -m` output can be tested against, and the red-proof case is a
linked worktree built from a scratch repo like the one above.

**TS-2 (F3, low) — one knob, two vocabularies in the same help screen.**

```
$ gopi --help | grep -A1 'thinking ON'
  --thinking ON|OFF         thinking mode (GOPI_THINKING)
...
$ gopi --help | grep GOPI_THINKING
  GOPI_THINKING=disabled gopi "summarize"      override one knob for one run
```

The option row says `ON|OFF`; the Examples block — the copy-paste-ready line —
uses `disabled`. Both work (`internal/config/config.go:82`: on/off/true/false/
1/0/yes/no/enabled/disabled), so this is discoverability, not breakage. An
`--help-env` row worth checking in slice 7: whether the 241-line table states
the full accepted set anywhere.

### Probes that produced no finding (recorded so they are not re-run)

| probe | result |
|---|---|
| `gopi task search "x" --limit 5` | rc=1 `unknown flag --limit` — correct, `--limit` belongs to `gopi memory search` (both documented in their own `-h`) |
| `gopi -v`, `gopi --version`, `gopi version` | identical, rc=0 |
| `gopi --settings /nonexistent version` | rc=0 — the version path needs no config (documented: "no git, no filesystem, no network") |
| `gopi --nope` | rc=1 `gopi: unknown flag --nope` |

## Second sweep — sandbox (config & maintenance, 2026-09-26)

**SB-1 (F1+F2, high) — `gopi sandbox create` from the shell produces a
writable private `memory.db`, silently.** Rule 34 and `gopi sandbox -h` both
promise `memory.db -> the shared read-only sample (symlink, from the test-DB
scaffold)`, unconditionally:

```
$ SB=$(gopi sandbox create --label build --task-id "$TASK_ID" | tail -1)
$ find "$SB" -maxdepth 2
drwx------  sessions   drwx------  raw   drwx------  logs
lrwxrwxrwx  settings.json -> /home/user/.gopi/settings.json
drwx------  .gopi          ← no memory.db at the top level at all
-rw-------  .gopi/memory.db   (139 264 bytes, REGULAR FILE, mode 600)
```

The documented symlink is materialised **only** when `GOPI_SANDBOX_SAMPLE_DB`
is set (`internal/sandbox/sandbox.go`: `sampleDBTarget` returns "" when neither
`Options.MemoryDBPath` nor the env var is set, and `Create` then simply skips
the link). No shell invocation sets it — `cli_sandbox.go` passes
`sandbox.Options` through and the help mentions the env var as an *option*,
not as a precondition. So a hand-made sandbox is a **second database
generator** (Rule 32's named anti-pattern) whose file is not the documented
shape: it is writable, unsealed, and asymmetric with the settings link.

Measured consequence: a probe run inside it wrote real rows to that private
DB —

```
$ sqlite3 "$SB/.gopi/memory.db" "select task_id,summary from tasks where summary like '%frobnicate%';"
01a0dcf2-a9ad-76c8-b381-b40d9ab106c6|[auto] frobnicate
01a0dcf2-fa95-742a-bacd-69dd4bf4ce0d|[auto] frobnicate-two
```

— which is exactly what the sample link exists to prevent. Isolation otherwise
held (the live ledger was 3092 rows before and after; evidence:
`evidence-sandbox-memorydb.txt`).

Acceptance for a fix: `gopi sandbox create` alone either materialises the
sample link (resolving the sample the way production can) or **fails loudly**
naming the reason and the channel — never a silent third shape (Rule 22's
"fails LOUDLY, never a silent fallback" clause applied to this initializer).

> **RESOLVED 2026-09-26 — and resolved the OTHER way (this finding was right, and its
> acceptance was overtaken).** The observation stands: a plain-shell
> `gopi sandbox create` produced a writable `.gopi/memory.db` while `gopi sandbox -h`
> (and this document's own oracle quote of it) promised a symlink to the shared
> read-only sample. But the fix chosen was **not** "materialise the sample link":
> commit `40035f03` decided the opposite — the ledger is a **WRITE TARGET**, so a
> symlink to a read-only artifact is the defect, and `<sandbox>/.gopi/memory.db` is now
> a REAL, private, `0600`, WRITABLE file by design (PROJECT.md Rule 34). The file you
> measured *is* the shape; what was wrong was the promise. Consequences for this
> finding's text: (a) the `gopi sandbox -h` line it quotes as the oracle has since been
> **deleted** — the usage now describes ONE shape; (b) `sampleDBTarget` /
> `GOPI_SANDBOX_SAMPLE_DB` no longer exist — the sample is passed as a **seed by value**
> (`GOPI_SANDBOX_LEDGER_SEED`, copied, verified as a usable SQLite DB); (c) point 2's
> "a hand-made sandbox is a second database generator" is still true and now has a
> named answer: the sandbox's own ledger is the ONE first-open authority, and a second
> generator remains the anti-pattern (Rule 32).

## Third sweep — session inspection (slice 2, 2026-09-26)

Oracle: `log -h`, `stats -h`, `raw -h`, `checkpoint -h`, `resume -h` (slice 2's
entry points), then run each against **real** state: read-only reads against the
operator's live profile, everything that writes or resolves a path inside a
sandbox from `gopi sandbox create`.

**SI-1 (F1+F2, high) — the sandbox's own shape and its readers disagree about
where a session lives; nothing ever reads the documented `sessions/`.**

`gopi sandbox -h` and Rule 34 both promise three private, writable directories —
`sessions/`, `raw/`, `logs/` — and `create` really does materialise them at the
sandbox top level:

```
$ SB=$(gopi sandbox create --label euaudit3 --task-id "$TASK_ID" | tail -1)
$ find "$SB" -maxdepth 1 -printf '%y %p\n'
d $SB        l $SB/settings.json      d $SB/logs     d $SB/raw     d $SB/sessions
```

But `--help-env` states the resolution rule for `GOPI_HOME` verbatim — *"Gopi
data root: ~/.gopi becomes $GOPI_HOME/.gopi (test/sandbox isolation)"* — and
every session command follows the rule, not the shape:

```
$ GOPI_HOME="$SB" ./bin/gopi log --last
Error listing sessions: open $SB/.gopi/sessions: no such file or directory   (rc=1)
$ GOPI_HOME="$SB" ./bin/gopi stats --all
gopi stats: open $SB/.gopi/sessions: no such file or directory               (rc=1)
$ GOPI_HOME="$SB" ./bin/gopi raw --last
Error: open $SB/.gopi/sessions: no such file or directory                    (rc=1)
$ GOPI_HOME="$SB" ./bin/gopi checkpoint list
gopi checkpoint list: checkpoint: list $SB/.gopi/raw: no such file or directory  (rc=1)
```

The run path is no better: a real `gopi … "<prompt>"` inside the sandbox writes
its session **below `.gopi/`**, leaving the documented directory empty forever:

```
$ GOPI_HOME="$SB" ./bin/gopi --base-url http://127.0.0.1:9 --api-key dummy \
    --timeout 10 "probe"                     # LLM refused, as intended
$ find "$SB" -maxdepth 3 -printf '%y %p\n'
d $SB/.gopi/sessions/b386ca70-354f-465f-8c37-a12a400c6530
d $SB/sessions                               ← still EMPTY, nothing reads or writes it
```

The divergence is not cosmetic, it costs the user their data: a raw log copied
into the directory the help names is invisible, and the exact same bytes one
level down are found —

```
$ cp "$REAL_RAW" "$SB/raw/gopi-raw-20260926-120000.log"
$ GOPI_HOME="$SB" ./bin/gopi checkpoint list
… /home/user/workspace/gopi-sandbox/sandbox-euaudit2-4e1a5e84/.gopi/raw: no such file …   (rc=1)
$ cp "$REAL_RAW" "$SB/.gopi/raw/gopi-raw-20260926-120000.log"
$ GOPI_HOME="$SB" ./bin/gopi checkpoint list
log                          reqs last-model   msgs  bytes sha256    last-message
gopi-raw-20260926-120000.log ≥152 glm-5.3-flash    3  54402 fc5ef6b2be2c284a {"type":"tool_result",…}   (rc=0)
```

Two readings of one environment cannot both be right, and the one a user
believes is the one the help prints. The escape hatch (`GOPI_HOME="$SB/.gopi"`,
so `$GOPI_HOME/.gopi` ≡ `$SB/.gopi`) is nowhere in the help, and it re-points the
profile the sandbox initializer created — a workaround the user must invent.

Acceptance for a fix: ONE of the two is made true — either `create` materialises
`.gopi/{sessions,raw,logs}` (the paths its own readers and the run path use, the
top-level dirs dropped from the documented shape) or the readers and the run
path resolve `$GOPI_HOME/{sessions,raw,logs}` (and `--help-env`'s `GOPI_HOME`
line is corrected with them). The check is a red-proof that runs a real
`gopi "<prompt>"` inside a fresh sandbox and asserts the session it writes is
found by `gopi log --last` under the same `GOPI_HOME` — the two surfaces agree,
so a fix cannot satisfy it by editing prose alone.

**Probes that produced no finding** (recorded so they are not re-run):

| probe | result |
|---|---|
| `gopi log --last 2` with no `GOPI_HOME` | two blocks, a blank line between, model/duration/iterations/tokens all present — rc=0 |
| `gopi stats` with no input | rc=1, `pick one input: <session-id>, --branch …, --last [N] or --all`, then the verb's usage — faithful |
| a refusal's exit status vs its wording | `log`/`stats`/`raw`/`checkpoint` all rc=1 with a one-line reason — F4 clean apart from SI-1's missing directory |
| `gopi raw -h`'s "no `--json`/`--text`/`--format`" rule | stated, and the selector grammar (`3`/`0`/`last`/`-2`, `all` refused as a round) is spelled out on the surface — no probe needed to disprove a claim it does not make |

## Fourth sweep — session inspection, the `resume` half (slice 2, 2026-09-26)

Oracle: `resume -h`, `checkpoint -h`, `log -h`, `stats -h` — then each read against
**real** state inside `gopi sandbox create` profiles.

**The instrument lesson that opened this sweep (recorded first because it invalidated
a whole transcript):** the first transcript of these probes prefixed every call with
run flags — `bin-gopi --base-url … --api-key … --timeout N resume <target>` — exactly
as this document's isolation section had been written. That form is itself broken
(RS-1 below): the flag prefix made the run path swallow `resume` as a *prompt*, so
none of those probes ever reached `gopi resume`. Every conclusion drawn from them was
an artefact of the instrument. **A probe's own invocation shape is part of what is
under audit — run each surface at least once in the bare form a user would type, and
detect "did the verb actually dispatch?" before reading any output.** The detector
used here: a session directory minted under `$GOPI_HOME/.gopi/sessions/` is written
only by the run path, so a growing count means the verb was swallowed.

**RS-1 (F1+F2, high) — a verb placed after a run flag is silently swallowed as a
prompt; the machine starts a real run.** The oracle is one help screen that states
both rules and never says which wins: `gopi [options] "<prompt>"` (Usage) and
`gopi --timeout 600 task.txt` (Examples), versus `gopi <command> -h`.

```
$ p() { d0=$(ls -1 "$SB/.gopi/sessions" 2>/dev/null|wc -l); GOPI_HOME="$SB" bin-gopi "$@"
        d1=$(ls -1 "$SB/.gopi/sessions"|wc -l); [ $d1 -gt $d0 ] && echo RUN-PATH || echo dispatched; }
version                                rc=0    dispatched
--settings /nonexistent version        rc=0    dispatched
--timeout 3 version                    rc=1    RUN-PATH     ← a real agent run
--base-url http://127.0.0.1:9 version  rc=127  dispatched
$ sqlite3 "$SB/.gopi/memory.db" "select substr(summary,1,40) from tasks"
[auto] version                                            ← a live ledger row for the string "version"
```

The same collapse with `resume` (sandbox `euaudit2d`): four unresolvable targets,
four agent runs, zero refusals, four `[auto] resume …` rows. A/B on ONE target in ONE
sandbox, differing only in argv order: verb-first → `cannot resolve session id`, rc=1,
no session minted; flag-first → a raw log, a new session, `task ledger: registered
01a0dcff-ff45-…`. This is the machine that already billed this epic 336 921 tokens
for the word `frobnicate`. Filed `01a0dd02-d0a2-…`.

**RS-2 (F1, high) — `gopi resume <task-id>` is unreachable in the form its own help
documents.** The help names the task-id target FIRST and calls it "the zombie-recovery
path" ("recovering a killed run needs no session-id hunting"):

```
$ GOPI_HOME=$SB bin-gopi task show 01a0dced-ca8a-77f1-8820-8de8a6e023e8
gopi task show error: task: no task with task_id="01a0dced-…"
$ GOPI_HOME=$SB bin-gopi --base-url … --timeout 8 resume 01a0dced-…   # the documented shape
[gopi] raw log: $SB/.gopi/sessions/2cffe158-…/….raw.log              # a NEW session: the id was a PROMPT
$ GOPI_HOME=$SB bin-gopi resume 0 --task t-1788048877                 # help's own example line
gopi: unknown flag --task                                             rc=1
```

So the row the help tells the user to recover is not in the profile the recovery runs
under (`no task with task_id=…`), the documented flag-prefixed invocation never reaches
the resolver at all (RS-1), and the `--task` example is refused as an unknown flag.
What *does* work is the undocumented combination — verb as argv[1], no run flags,
profile whose ledger holds the row. Filed `01a0dd02-e387-…`.

**RS-3 (F3+F4, low) — the same word is advertised as a subcommand of two verbs, and
only one accepts it.** `gopi checkpoint -h`'s own Usage block lists
`gopi resume <log> [continuation...]` under its title "checkpoint & resume
subcommands", while the top-level index says `gopi checkpoint list|export` and the
dispatcher answers:

```
$ bin-gopi checkpoint resume /nonexistent/foo.log
gopi checkpoint: unknown subcommand "resume"      rc=1
```

A user who reads `checkpoint -h` types one word too many. Filed `01a0dd02-f8d5-…`.

**Probes that produced no finding** (recorded so they are not re-run):

| probe | result |
|---|---|
| `resume` with no argument | rc=1 + the full usage on stderr — faithful |
| `resume <nonexistent session-id>` at argv[1] | rc=1, `cannot resolve session id "…"` naming the path it searched — faithful |
| `resume <raw-log path>` with the file really present | resolves, writes a session below `.gopi/sessions/`, runs — the checkpoint path works |
| `resume <bare timestamp prefix>` (the `checkpoint -h` example form) | accepted, same behaviour as the absolute path — faithful |
| `log -h` / `stats -h` / `checkpoint -h` view flags | every view named is accepted (no `unknown flag`); `raw`'s selector grammar holds |

Isolation for this sweep: every probe ran with `GOPI_HOME` pointing into
`gopi sandbox create` profile; the probe rows (`[auto] version`, `[auto] resume …`)
exist only in the sandbox ledgers (euaudit2d: 4 rows, euaudit2f: 1, euaudit2g: 1).
`ls -1 ~/.gopi/raw | wc -l` → 176, unchanged. **One breach of this run's own rule is
recorded rather than hidden**: a single warm-up call was issued without `GOPI_HOME` and
leaked row `01a0dd01-b008-70fa-9bf7-70103728e08a` (`[auto] frobprobe-y`) into the live
ledger; it was left in place (a ledger write is corrected by an event, never deleted)
and the isolation check below counts *probe* rows, so the breach is explicit instead of
invisible.

## Fifth sweep — task ledger (slice 1, 2026-09-26)

Slice 1: `task <sub>`, `worker`, `merge`, `merger`, `archive`. Binary
`CGO_ENABLED=0 go build -o bin-gopi ./cmd/agent/` at `f7216ef6`; **141 probe
invocations**, transcripts in the run's evidence dir
(`~/.gopi/sessions/01a0dcfa-d991-70a7-9c52-68b79abfdaed/evidence/`, files
`01-…`–`14-…`). Isolation: a `gopi sandbox create` profile plus a scratch
`--db`; destructive verbs ran in a throwaway git repo with its own
`settings.json`. Operator ledger `select count(*) from tasks` = **3107 before,
3107 after**, and `summary like '%euaudit%'` = 0 — never touched.

### Step 1 — what the help promised (the oracle)

Read before any source. Two facts about the surface itself:

- **`task <sub> -h` is group-scoped, not subcommand-scoped.** Ten invocations
  (`task -h`, and `-h` on submit/show/update/list/archive/verify/next/resume/
  finish-cleanup) all rendered the **same 20,354-byte group manual**, byte for
  byte (`md5` of every body identical). So a subcommand's own flags are
  discoverable only by finding its entry INSIDE one long page — `task submit -h`
  never says "this is submit".
- The manual's per-verb promises (verbatim, and this sweep's oracle):
  `task list` **"List tasks, newest first"**; `task submit` **"--source names the
  row's ROLE … human ONLY from a process with no agent chain above it"**;
  `task update` **"illegal transitions (e.g. completed → pending) are refused"**;
  `task archive` **"Archive a TERMINAL task"**; `worker` **"Exit codes: … 0 when
  it completed, 1 when it failed or could not be run"**; `merger` **"retired … a
  merge the operator cannot stop"**; `merge` **"its loop, sweep and daemon faces
  refuse at every spelling ('gopi merger', its deprecated aliases, and even
  '-h')"**; `archive` **"--repo PATH   Repo to inspect/act on. Default: the repo
  of the cwd"**.

### Step 3 — findings (F1–F5 taxonomy, five fields each)

Findings only; the per-probe table follows. Each is filed as its own `bug` row.

**TL-1 (F1+F2, medium) — `--source human` is silently replaced by `agent`.**
The help quotes five selectable values; four round-trip, the fifth is discarded
with `rc=0` and no message.
```
$ gopi task submit "source probe human" --payload sp --db $DB --repo $RIG --source human --task-id …004
Task queued: …004 (status=pending, source=agent, tags=—)                rc=0
$ sqlite3 $DB "select source from tasks where summary='source probe human'"    -> agent
   control, same shell/profile: --source worker|server|merger|agent -> stored as given
   the documented escape hatch really works: GOPI_HUMAN_TERMINAL=1 … --source human -> source=human
```
Contract: `cli_tasks.go:4783` `source := tasksource.DeriveNow(opts.source)` — the
intent is deliberate (no fake-human rows; `Known()` passes at :4770), but the
surface never says the value was dropped. Filed `01a0dd4f-52ea-7d7b-867e-c12843ab8cdd`.

**TL-2 (F1+F3, medium) — a removal message points at a RETIRED verb.**
```
$ gopi task next
gopi task next: removed (M4.5) — manual claiming was removed — use "gopi worker" (…) or "gopi task serve" (the daemon loop)   rc=1
$ gopi task serve
serve: retired — the resident merger daemon was retired …                                                                   rc=1
```
Contract: `cli_tasks.go:4544-4546` — `taskRemovedAdvice`'s own comment says the
pointer must name "`gopi worker`, never the retired verb (… a removal message that
points at another removed spelling sends the reader in a circle)"; the very next
line returns a literal `or "gopi task serve"`. Filed `01a0dd4f-531e-7e5b-b044-31c082a3e2b6`.

**TL-3 (F2+F4, medium) — `task merge`: advertised by the manual, absent from the
dispatcher's list, and `-h` runs it.**
```
$ gopi task frobnicate
gopi task frobnicate: unknown subcommand "frobnicate" (want archive|dedup-scan|…|merger|serve|run)   rc=1
      ← `task -h` advertises `task merge` as a documented atom; the dispatcher's own want-list does not carry `merge`
$ gopi task merge -h
GoPi task subcommands — … (the generic group manual, byte-identical to `task -h`)                    rc=0
$ gopi task merge
task merge (no-submit): merged=0 deleted=0 dispatched=0 … failed=0                                   rc=0   ← a REAL pass
```
Measured safety: against the live repo that pass walked every repo in the `repos`
table under the merge lock and decided nothing (`master` 79807932 and the branch
HEAD f7216ef6 unchanged, worktree registrations identical) — nothing was damaged,
but a mistyped `-h` is the difference between a help page and a merge run. Filed
`01a0dd4f-5360-74cf-b833-e9dc8181e946`.

**TL-4 (F1+F2, low) — one help line, two orders.** "List tasks, newest first", and
the unfiltered verb keeps it (`…00e → …00d → …00c → …00a → …` exact
`created_at DESC`), but `--status pending` renders priority-first:
```
priority 1 (18:38:10) → priority 2 → priority 5 (18:37:55 — the OLDEST) → … → priority 8
```
A user filtering the queue reads an order the same page promised it would not
see. Filed `01a0dd4f-b1fe-7642-807e-ecd6d128f1be`.

**TL-5 (F4, low) — the worker's refusal exit code is undocumented.**
`worker -h`: "Exit codes: … 0 when it completed, 1 when it failed or could not be
run." Measured at every worker face from inside an agent session:
```
$ gopi worker --once        rc=3   gopi worker: refusing to start: this process is inside an agent session …
$ gopi worker <task-id>     rc=3   (same)
$ gopi worker --daemon      rc=3   (same)
```
A refusal to run IS "could not be run" — documented as 1, measured 3, and 3
appears nowhere on the worker page (the same binary documents rc=3 for
`task status`'s "unverifiable"). Filed `01a0dd4f-b253-7361-8fd0-0e94cc80c5e2`.

### What held up (positive results, worth not re-testing)

- **`task submit`'s `--repo` gate (help: "verified … BEFORE the task row is
  written") is exactly true**: `--repo /nonexistent-dir` → rc=1
  `repo "/nonexistent-dir" is not a git work tree`, and the row count for that id
  is 0 — aborted before the write, not after.
- **Rule 33's park marker is truthful and never head-blocks**: a child of a
  pending parent printed `[queue-skip] parent 01a0…0001 not released
  (status=pending; releases on completed|verified)` while unrelated claimable rows
  still rendered; a child of a DANGLING parent id was refused at submit
  (`parent_task_id "…00ff" does not exist`, rc=1, no row written); a completed
  parent released its child (`…0008` renders plain, no marker).
- **The state machine refuses exactly what the help names**: `pending →
  completed` rc=1 naming the allowed set `claimed|in_progress|cancelled`;
  `completed → pending` rc=1 `allowed from completed: verified|failed`; the legal
  chain (`in_progress → completed`) rc=0; `reopened` is accepted as a filter
  value, as the retired-row clause says.
- **`--reason` / `--result` behave as documented**: a same-status no-op does NOT
  overwrite a stored reason (`completed → completed` left `probe: legal path
  finished` intact), and `--result` renders as its own `result:` line
  (`verdict: green, 12/12`) without touching the summary.
- **`task archive` + the `--status failed` hiding rule are exactly as stated**: a
  pending row is refused ("not terminal"); a failed row is archived
  (`tags=resolved`), then HIDDEN from `--status failed` and shown again by `--all`
  with a `[resolved]` marker; the tombstone stays `task show`-able; a second
  archive is an idempotent no-op.
- **`task finish-cleanup` is a guarded, dry-run-default chain**: the default run
  printed `would run; align-…` (nothing touched), `--apply` ran the real chain
  (`done=[align-before-delivery,pre-merge,worktree] worktree-freed=…`) in the
  throwaway repo, a pending row refused with `status=pending not cleanable
  (terminal or cancelled required)`, and the contradictory `--apply --dry-run`
  pair refused with rc=1 (`--dry-run wins`).
  > **Retired mechanism (worktree lifecycle, `58b5618b`, 2026-10-30).** This
  > bullet records what that verb printed on the audit day. It no longer runs:
  > the spelling is retired, answers exit **1** and names the paths that
  > survived (`gopi task sweep-worktrees`, `gopi task list`, `gopi merge`).
  > `gopi task list --sweep` was on that list until 2026-11-05 (commit
  > e4521fad), when the stale sweep was deleted end to end: the surviving
  > read-out is plain `gopi task list`, which resolves each row's holder and
  > reports it without acting. The paragraph is kept as the record of the
  > measurement, not as a step to repeat.
- **The retired-verb family refuses at every spelling** (`merger`,
  `task merger`, `task serve`, `merger --stop`, `merger --status`) with rc=1 and a
  pointer to `gopi task merge` — except `task serve -h`, which is rc=0 and prints
  that same pointer, so the guidance still lands. *(Measured 2026-11-01: the
  pointer is the family spelling `gopi task merge`. This bullet said `gopi
  merge` when the audit ran — that top-level verb was removed afterwards, and
  a refusal pointing at it would now point at another refusal.)*
- **The `task for-branch` exit contract is live**: an unowned branch → rc=4 with
  `(1 rows carry a branch; none names this branch in repo …)`; a named branch →
  rc=0. `task status` → `verdict: n/a` + `resumable: yes` for a non-`in_progress`
  row, rc=1 for a missing row.
- **`task verify` reports honestly**: a row with no delivered fix → rc=1
  `FAIL: child gopi run: … (ctx: context deadline exceeded)`; a nonexistent row →
  rc=1 `invalid: task "…" does not exist`.
- **`merge` never ignores a name**: `merge --fast feat/does-not-exist-xyz` rc=1
  "no repo … holds … — a named branch is never silently ignored"; `merge --fast
  feat/ff-target` landed and pruned it (the ref disappeared from `refs/heads`).

### The worker probe — what "ran for real" means here (Rule 31)

The briefing's hazard was a worker round running its worktree-release pass over
LIVE worktrees. Measured, and bounded by construction:

- Every worker face (`--once`, `<task-id>`, `--daemon`) first hit the agent-spawn
  guard from inside the agent session: **rc=3, nothing started**.
- The returning one-row face was then executed with the override the guard itself
  prints (`GOPI_ALLOW_AGENT_SPAWN=1`), under a **hard `timeout 120`**, from a
  scratch cwd, with `GOPI_SETTINGS` naming ONLY the throwaway repo (the live repo
  is not in its `repos` table, so no sweep can see it), a sandboxed `--db`, and
  the LLM endpoint refused (`ANTHROPIC_BASE_URL=http://127.0.0.1:9`).
- Result: the round really ran — a session was minted, the child's LLM call
  failed against the refused endpoint, the row went `failed`
  (`claimed_by=hostb-Laptop:…`, `reason=task failed`), the row's
  `own-worktree-release` pass fired, rc=1 — and the **live repo was untouched**:
  `git worktree list` 6 registrations before and after (`diff` empty), `master`
  79807932 and the branch HEAD f7216ef6 unchanged.
- `GOPI_ALLOW_AGENT_SPAWN=1` deliberately overrides a guard whose own words are
  "a deliberate rule violation, not an accident"; it was taken under the
  briefing's explicit sandbox/cwd/ledger constraints, the blast radius was the
  throwaway repo, and the hard `timeout` bound the child.
- **Aftermath, checked, not assumed**: `gopi worker --restart` (P88) did spawn a
  detached local worker for the sandbox (`gopi worker --status` in that profile
  then found it: `no workers running` + "a FOREGROUND worker takes no lock"). It
  never had the poll gating on, so it could not claim anything, and it ran under
  the sandbox profile + `GOPI_MEMORY_DB` (never the operator's ledger). It
  **self-exited** — `--restart`'s own message names "self-exits after 3 idle
  round(s)", ~90s at a 30s idle wait, and its lock (holding pid 3193059, 18:40)
  pointed at a pid that was gone by the time I looked. Three other `gopi worker`
  processes visible at the same time were NOT from this probe: their cwd was a
  peer run's session dir (`/home/user/.gopi/sessions/<other-task-id>`), so they were left
  alone. A probe that starts a daemon owes this check, and owes it BEFORE
  concluding "I must clean this up".

### Probes that produced no finding (tabulated so they are not re-run)

| probe (verbatim) | rc | result |
|---|---|---|
| `task submit <s> --payload <p> --db $DB --source human --task-id X` | 0 | queued; source stored `agent` → TL-1 |
| `task submit <s> --db $DB --task-id X` (NO `--payload`) | 0 | note "no --payload given — the positional summary is mirrored into the payload (a payload-less row is never claimed by worker/serve)"; the payload column holds the summary — faithful |
| `task submit <s> --db $DB` with no `--repo`, from a non-git cwd | 1 | `repo "$PWD" is not a git work tree` + no row — faithful |
| `task submit … --repo /nonexistent-dir` | 1 | aborted BEFORE the write (row count for that id = 0) — faithful |
| `task submit … --tags bug` / `--tags docs` / `--tags bug --priority 2` | 0 | priority derived 1 / 8, explicit 2 overrides — faithful |
| `task show <id>`, after update, after `--result`, after archive | 0 | every documented field renders (`reason:`, `result:`, `ended_at`, `deliverable.repo`) — faithful |
| `task list`, `--status pending`, `--sweep`, `--status reopened` | 0 | counts + `[next]` + `[queue-skip]` markers correct; order → TL-4 |
| `task search probe` / `task search "order"` | 0 | hits printed, no order claimed by the help — nothing to disprove |
| `task update` legal chain, same-status no-op, `--reason`/`--result` | 0 | as documented |
| `task update <completed> --status pending` | 1 | `illegal status transition completed → pending (allowed from completed: verified\|failed)` — faithful |
| `task list --status bogus` | 1 | names the whole vocabulary incl. `reopened` — faithful |
| `task archive <terminal>` / `<pending>` / twice | 0 / 1 / 0 | archive, refuse, idempotent no-op — faithful |
| `task list --status failed` before/after archive, and `--all` | 0 | hidden, then shown with `[resolved]` — faithful |
| `task for-branch <unowned>` / `<owned>` | 4 / 0 | refuses by name / names the owner — faithful |
| `task status <completed>` / `<missing>` | 0 / 1 | `n/a`, `resumable: yes` / `does not exist` — faithful |
| `task verify` (no delivered fix, `--timeout 1`) / (missing row) | 1 / 1 | honest FAIL and invalid — faithful |
| `task next` | 1 | removed, with a pointer → the pointer names a retired verb (TL-2) |
| `task frobnicate` | 1 | unknown subcommand + want-list + the full manual — faithful |
| `task merge` / `task merge -h` | 0 / 0 | a real pass / the generic manual → TL-3 |
| `task serve`, `task merger`, `merger --stop`, `merger --status`, `merger -h` | 1 | refuse with a `gopi task merge` pointer — faithful |
| `worker -h` / `--status` / `--stop` | 0 | help takes no lock; `--status` names the lock path — faithful |
| `worker --once` / `<task-id>` / `--daemon` (inside an agent session) | 3 | guard refusal → TL-5 |
| `worker --restart` (no daemon running) | 1 | `no worker daemon is running`, then a start that failed during startup — reported, not silent |
| `merge --status` | 0 | prints `<repo>: N branch(es) tagged out of the merge queue` + one line per archived branch with its reason — read-only, no lock |
| `merge --fast <no-such-name>` / `<ff-branch>` | 1 / 0 | refused by name / landed + pruned — faithful |
| `archive --check [--repo R]` | 0 | honours `--repo`, from a non-git cwd too — faithful |
| `archive <b>` (no `--reason`) / (bogus `--reason`) | 1 / 1 | requires the code and prints the closed set — faithful |
| `archive <b> --reason superseded` (completed-row owner, no override) | 1 | refuses and prints the exact `--override-completed` form to type — faithful |
| `archive <b> --reason superseded --override-completed` | 0 | archives + writes `refs/gopi/archive-reason/<branch>`, read back — faithful |
| `archive <b> --reason superseded --repo R` (cwd NOT git) | 1 | `git rev-parse --show-toplevel in .` — see the carried-over finding below |
| `task finish-cleanup` (dry-run default / `--apply` / pending row / contradictory pair) | 0 / 0 / 0 / 1 | as documented — faithful |

**Carried over, NOT filed here:** `gopi archive <branch> --repo PATH` ignores
`--repo` for the archive ACTION while `--check` honours it (re-measured this
sweep: `--check --repo` from a non-git cwd → rc=0; the action → rc=1
`git rev-parse --show-toplevel in .`). This is **already reported** in
`devdoc/gopi-tools-maintenance-help-surface-report.md` *Findings 1*, which also
records the open design question (is the archive action deliberately cwd-only and
the help line wrong?). Re-filing would duplicate an existing row, so this sweep
cites that report and adds only the second independent measurement.

### Isolation evidence (this sweep)

| fact | value |
|---|---|
| operator live ledger, `select count(*) from tasks` | **3107 → 3107** |
| operator ledger rows matching `%euaudit%` | 0 |
| sandbox | `gopi sandbox create --label euaudit5` → `<root>/sandbox-euaudit5-3dffbf2b`, device id 66309 vs `/` 66307 (off-root, Rule 22) |
| probe ledgers | scratch `--db` files under the run's evidence dir (`rig/`, `rig2/`), never `~/.gopi/memory.db` |
| destructive-verb target | throwaway repo + its own `settings.json` (`rig2/authored`) |
| live repo after every probe | `master` 79807932, branch HEAD f7216ef6, 6 worktree registrations, 4 branches — all unchanged |

## Sixth sweep — chat & web (slice 3, 2026-09-26)

Slice: `chat`, `web`, `ui`, `ui_state`, `cli`, `proxy`. Method per the protocol:
`-h` read as the oracle FIRST (byte counts + md5 in the run's `probes/help-index.txt`),
expectations written to `probes/expectations.md` before any probe, every probe run
for real inside a sandbox profile with the LLM endpoint refused
(`http://127.0.0.1:9`). 88 probes, 6 findings filed as `bug` rows.

| verb | bytes | md5 |
|---|---|---|
| chat | 12907 | 381b5f48edbbaa70cd858fe9b5cfce22 |
| web | 3471 | 7a43d0bf3b0d8082df903f038150f8b3 |
| ui | 4405 | 1e821fa8605fcfe822eb0b968b02ca85 |
| ui_state | 4217 | 28b6e1bbde1a638c794e8915bace92cf |
| cli | 3071 | 44e9606d0690db6370d0abf1354cfeb7 |
| proxy | 1149 | 738eb099e4a9cfadd6b03b6a25bf4441 |

**The one fact that governs everything below**: a run flag placed BEFORE a verb
does not configure the verb — it turns the verb into the PROMPT of a real agent
run (`BootstrapEnvFromArgs` hoists only `--settings`/`--home` before dispatch).
Six of this sweep's probes had to be discarded and re-run because of it, and the
blast radius was not limited to the probe (finding CW-2). Every probe in this
sweep that the first batch mis-routed was re-run with the endpoint arriving as
`ANTHROPIC_BASE_URL` in the environment, which is the only spelling that reaches
a verb's own face for that flag.

Findings only; the per-probe table follows. Each is filed as its own `bug` row.

**CW-1 (F1+F2, high) — a run flag before a verb silently makes the verb the prompt.**
Every verb's own page takes its face flags AFTER the verb, and the faces themselves
reject the run flag on their own face — so the prefix spelling is the only one a
user can reach for, and nothing says it disables the verb.
```
$ gopi --db <evidence>/scratch.db version      → session minted, rc=1, stdout empty
    session.jsonl: {"content":"[EPIC GOAL]\nversion\n\nBased on this Epic, [TASK_LIST_BEGIN] …"}
$ gopi --base-url http://127.0.0.1:9 chat --once --turns 5 -p hi   → rc=124, session minted
$ gopi --timeout 5 version / --raw-log off version / --model some-model version
                                                → same shape, each a real run
$ gopi chat --once -p hi --base-url http://127.0.0.1:9   → rc=2 "more than one prompt given"
    control: gopi cli --once -p hi --base-url http://127.0.0.1:9 → rc=2 'unknown flag "--base-url"'
```
Contract: `cmd/agent/main.go` `BootstrapEnvFromArgs`; `CONFIG.md:143`/`CONFIG.md:454` document
`--base-url` as a run option and say nothing about placement. Filed `01a0dd87-8242-73e7-b665-ebbffdc25e99`.

**CW-2 (data-loss mechanism, high) — a probe-started run's close-out took a LIVE run's worktree.**
Observed while this sweep was running: the worktree registration of the running
session vanished, the branch ref was fast-forwarded from under it, and the
working directory itself was deleted (every later `bash` call failed with
`getcwd: cannot access parent directories`, and `task_submit` itself failed with
`resolve default repo (cwd): getwd: no such file or directory`). Recovered from
the surviving ref (`git worktree add <path> task/01a0dcfa-…`;
`merge-base --is-ancestor 79807932 HEAD` → true, no master-lineage work lost).
Honest note: the ref move and the removal are observed; the actor is inferred —
two agent runs were alive and only the close-out chain does both. The invariant is
what failed, and it is the one Rule 30 exists to protect from the other side.
Filed `01a0dd87-8283-7a87-a936-4fc56142bb24`.

**CW-3 (F2+F4) — `chat` re-reads a bad flag as a resume target.**
```
$ gopi chat --bogus-flag     rc=1
gopi chat: resume target "--bogus-flag" is neither a raw log path nor an existing session id …
```
`-h` documents exit 2 for "a bad flag" and documents the positional as a raw-log
path or session UUID; nothing says an unrecognised `--word` is read as the
positional. Filed `01a0dd87-82d5-7690-b9d4-7f81ea92c15a`.

**CW-4 (F2+F4) — `ui_state --session` never resolves, so a bogus id and a real one read the same.**
```
$ gopi ui_state --compare hello --session nosuch-session-id   rc=1
#gopi-ui-state-compare surface=chat state=exited dump=<the dump on hand> session= transcript_lines=0 match=false
  refused … reason="the dump sink never interpreted a render frame (frames=0)…"
$ gopi ui_state --compare hello --session <real session dir>  → byte-identical line, session= still empty
$ gopi ui_state --list --session nosuch-session-id            rc=0 (flag not consulted)
```
The dump is discovered independently of `--session`, the resolved authority is
never named, and the verb exists precisely to stop a false reading about a paint
bug. Filed `01a0dd87-8315-7767-a5b6-ac0485ce30c5`.

**CW-5 (F5) — `proxy` announces "listening on" before the socket exists.**
```
$ gopi proxy --upstream openai … --listen 127.0.0.1:9
gopi proxy: listening on 127.0.0.1:9 (upstream=openai base=http://127.0.0.1:9 model=m)
gopi proxy: listen tcp 127.0.0.1:9: bind: permission denied                       rc=1
   busy port: "listening on 127.0.0.1:18093 …" then "bind: address already in use"
```
A readiness probe that greps the banner races the bind. Filed `01a0dd87-8351-70c8-a764-a10e50f2b93c`.

**CW-6 (F1+F3) — the REPL's refusal prints a vocabulary the help does not.**
```
$ printf '/frobnicate\n/quit\n' | gopi chat --once     rc=1
[gopi] unknown command "frobnicate" — available: code-search, log, memory, new, task, text2img
   /new and /clear both accept; the list names "new" bare, omits /quit /help /tools /clear
```
Filed `01a0dd87-838f-7788-91dd-0a167ecf1ed9`.

### What held up (positive results, worth not re-testing)

| probe | result |
|---|---|
| `chat --once -p hi` with a refused endpoint (`ANTHROPIC_BASE_URL`) | rc=1, no session minted, endpoint error surfaced — the once-path refuses cleanly |
| `cli --once` / `cli --once --tools` with a refused endpoint | rc=1, same shape — `--tools` does not open a second path |
| `chat` from a scripted pipe (no TTY), real commands | `/memory list` `/help` `/task list` all served from the pipe, rc=0 — a scripted line IS honoured |
| `ui_state --list`, `ui_state --target chat --pid <bogus>` | rc=0 / rc=1 with a named refusal — the surface-selection refusal is correct and names `--pid` |
| `web search` / `web fetch` with a refused endpoint | refuses by name, names the URL it tried |
| `proxy` on a FREE port, real `POST /v1/messages` | 502 with the upstream error translated into the OpenAI/Anthropic shape — the translation leg works |
| `cli` with no text browser | rc=1, names `GOPI_LINKS_BIN`, the looked-for path, the build script and the reason it is not on the Go build path — an exemplary refusal |
| `chat --once --turns 0` | refused, names the bound |

### Probes that produced no finding (tabulated so they are not re-run)

`chat -h`/`web -h`/`ui -h`/`ui_state -h`/`cli -h`/`proxy -h` (oracle capture, 6);
`chat --once` ×3 with a refused endpoint; `cli --once` ×2; `web search|fetch` ×2;
`ui --bogus-flag` refusal; `ui --server <bogus token>` refusal; `proxy` free-port
bind + live request; `proxy` busy-port refusal; `ui_state --list`/`--target`/`--pid`
×4; REPL `/help` `/memory` `/task list` `/new` `/clear` `/tools` `/quit` (7);
`chat` bad-flag, empty prompt, `--turns 0` (3). The refusals that named their
cause and their exit are the majority of this slice — this sweep's finding count
is low relative to the probe count for that reason.

### Isolation evidence (this sweep)

| fact | value |
|---|---|
| sandbox | `sandbox-euaudit3-56aba2a7`, device **66309** vs `/` **66307** (off-root, Rule 22) |
| operator live ledger | **3112 → 3114** rows across the whole sweep |
| operator ledger rows matching `%frobnicate%` / `[auto] version` | **0** / **0** |
| the mis-routed probes' rows | landed in the SANDBOX ledger: `[auto] version` ×8, `[auto] chat` ×2 |
| sandbox sessions minted by this sweep | 23 |
| worktree at sweep end | branch `task/01a0dcfa-…`, HEAD `5710f683` (= peer1/master tip); the worktree was rebuilt from the stored ref after CW-2, and this sweep's doc edit is the commit it adds on top |

The mis-routed probes are the one place isolation was *load-bearing* rather than
merely present: they ran a real agent loop, and every row they wrote landed in
the sandbox ledger — the operator's ledger never saw them.

**CW-7 (F5, medium) — a near-miss `GOPI_HOME` silently builds a second profile.**
`GOPI_HOME=<sandbox>/.gopi` (the profile path, not the home) resolves the profile
root to `<sandbox>/.gopi/.gopi`: nothing refuses, nothing warns, and a nested
tree with its own `memory.db` (4 rows) and its own `sessions/` (4 dirs) appears
beside the sandbox's real one, outside the sandbox reclaimer's model. Recovered by
hand this run, inventory kept in `probes/ab-nested-profile.txt`. The canonical
spelling — `GOPI_HOME=<sandbox> GOPI_SETTINGS=<sandbox>/settings.json` — is what
GOPI.md's sandbox rule already states; the help never names the near miss.
Filed `01a0dd8f-c014-7989-9d81-53620de4697f`.

### Post-sweep verification (same day, after the sweep closed)

Written because two of this sweep's six findings moved while it was being filed:
a later reader must not re-file CW-1 as an implementation, and must not copy the
sweep's own sandbox export line.

**1. CW-1's mechanism is FIXED and the fix is IN THIS BRANCH — the row is now a
verification task, not an implementation.** Commit `04a1262b` ("fix(cli): a
top-level verb after a run flag is refused, not run as a prompt",
`internal/config/cli_verb_guard.go`) is an ancestor of this branch (it arrived
with the merge at `5710f683`, = `peer1/master` at the time of writing) and was
NOT on the tree the sweep's binary was built from (local `master` `79807932`,
15:21, predates it). A/B, same sandbox, same six shapes:

| prefix shape | PRE-guard binary | guarded binary |
|---|---|---|
| `--db <evidence>/scratch.db version` | ran a real agent loop, sandbox row | `rc=1`, guard message, no session, no row |
| `--base-url http://127.0.0.1:9 version` | ran, sandbox row | `rc=1`, guard message |
| `--timeout 3 version` | ran, sandbox row | `rc=1`, guard message |
| `--raw-log off version` | ran, sandbox row | `rc=1`, guard message |
| `--model some-model version` | ran, sandbox row | `rc=1`, guard message |
| `--api-key k version` | ran, sandbox row | `rc=1`, guard message |

6/6 → 0/6 (measured: the sandbox ledger grew by one `[auto] version` row per
PRE-guard run and by nothing per guarded run), and the guard also refuses this
sweep's own verb faces (`--base-url … chat|ui_state`, `--timeout 3 cli|web` →
`"<verb>" is a gopi command, not prompt text …`). Both escapes the message names
still work: `gopi version` → `v0.1.0` `rc=0`; `--prompt version` → runs.
Acceptance for `01a0dd87-8242-73e7-b665-ebbffdc25e99` is therefore: reproduce the
A/B above on the delivered commit, not write the guard.

**2. Method correction — the sweep's sandbox export line was the wrong
spelling.** This sweep exported `GOPI_HOME="$SB/.gopi"`. The canonical form
(GOPI.md, the sandbox rule) is `GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json"`
— a sandbox IS a gopi home, and its profile root `<sandbox>/.gopi` is materialised
from it (`config.ProfileDirIn`; `CONFIG.md`'s `gopi_home` row states the join).
`$SB/.gopi` therefore resolves to `<sandbox>/.gopi/.gopi`: a nested profile that
is created silently, sits outside the reclaimer's knowledge, and had minted its
own 4 sessions and 4 `[auto]` rows before this run noticed and removed it. The
probes themselves are unaffected — their exports named the same directory as the
arg that consumed it, so the ABSOLUTE paths in `probes/*.argv` are the record of
where each row landed. Only a reader copying the line forward would be misled.

**3. CW-2's close-out evidence, now on disk.** The run that performed the
align/pre-merge/push leg left its own line in the sandbox's G4 transcript:
`finish-cleanup: 01a0dd7f-806e-… branch=task/01a0dcfa-… done=[align-before-delivery,pre-merge,push]
failed=[worktree: worktree in use by live process pid 3393506 (…"gopi proxy …")]`
— i.e. the mechanism that moved the branch ref is observed, while which of the
two concurrent runs invoked it still is not (CW-2's own row says so).

**4. The other four findings still stand on the guarded tree.** Re-run against
the guarded binary in the same sandbox, each with the spelling the row names:
CW-3 `chat --bogus-flag` → `rc=1 resume target "--bogus-flag" is neither a raw
log path nor an existing session id`; CW-4 `ui_state --compare hello --session
nosuch-session-id` → `rc=1`, `session= transcript_lines=0`; CW-5 `proxy … --listen
127.0.0.1:9` → `listening on 127.0.0.1:9 …` printed, then `bind: permission
denied`, `rc=1`; CW-6 `chat --once -p /frobnicate` → `unknown command
"frobnicate" — available: code-search, log, memory, new, task, text2img`. Only
CW-1's mechanism changed under the sweep's feet; the guard touches the run path's
argument order and nothing else in this slice.

## Seventh sweep — memory & knowledge (slice 5, 2026-09-26)

Slice 5: `memory <sub>` (`search|store|get|list|update|delete|feedback|sync|
provenance|backup`), `memsync`, `brew`. Binary
`CGO_ENABLED=0 go build -o bin-gopi ./cmd/agent/` at this branch's HEAD
`933c9a8c`, built and run from the run's evidence dir (never the worktree).
**142 recorded invocations** across 27 transcripts, 15 oracle pages, 98 probe
output files, in `~/.gopi/sessions/01a0dcfa-d9d8-7d55-bae5-de49deef3b58/evidence5/`.

Isolation: `SB=$(gopi sandbox create --label euaudit5m --task-id $ID | tail -1)`
(= `<sandbox root>/sandbox-euaudit5m-776ac449`, device id **66309** vs `/`
**66307** — off-root, Rule 22) with the canonical export `GOPI_HOME="$SB"`, plus a
**scratch `--db` / `GOPI_MEMORY_DB`** for every write. Operator ledger
fingerprint: `select count(*) from tasks` **3131 → 3137**, md5
`ec835b5d…` → `2f9ad2f4…`. The +6 is accounted for row by row: **4** are this
sweep's own `bug` rows (by design a live-ledger write), and 2 are foreign
(`01a0de4c-dede…` owned by another live run's pid 2938071, `01a0de4f-22aa…` a
standards-suite row) — neither carries a probe token of this sweep
(`payload like '%bogus-flag%' … → 0`). The knowledge base was never touched:
`select count(*) from distilled_qa` for every probe topic of this sweep → **0**.

### Step 1 — the oracle (bytes + md5, captured before any probe)

| page | rc | bytes | md5 |
|---|---|---|---|
| `memory -h` | 0 | 7777 | 0c2d0aba68ff0d2f41b77a422c0d5a1d |
| `memory search -h` | 0 | 7777 | 0c2d0aba68ff0d2f41b77a422c0d5a1d |
| `memory store -h` | 0 | 7777 | 0c2d0aba68ff0d2f41b77a422c0d5a1d |
| `memory update -h` | 0 | 7777 | 0c2d0aba68ff0d2f41b77a422c0d5a1d |
| `memory feedback -h` | 0 | 7777 | 0c2d0aba68ff0d2f41b77a422c0d5a1d |
| `memsync -h` | 0 | 3776 | 8d7b6b4106c3653ef98f4450225e5ec3 |
| `brew -h` | 0 | 709 | 2eb5b28b613edc18ec3b78fc287fb81e |
| `--help` (top level) | 0 | 10155 | 6546d3ee816b357b70b4c9f1b1e2e361 |

All five `memory *` pages are byte-identical to `memory -h` (the group-manual
shape slice 1 measured for `task`), and so are five more sub-verbs that page
never enumerates as separate pages — `memory get|list|delete|provenance -h` are
the same 7777 bytes, while `memory sync -h` (6524) and `memory backup -h` (4514)
are pages of their own. The page does carry each sub-verb's flags inline, so the
oracle is per-PAGE, not per-verb. Expectations were written to
`evidence5/expectations.md` before any probe was run.

### Findings (F1–F5 taxonomy, five fields each; each filed as its own `bug` row)

**F5-1 (F2+F4, medium) — `memsync --import` refuses a patch row with rc=2 and an
outcome no help page mentions.** `memsync -h` promises "dedup (exact +
similarity), sources merged" and "Re-importing the same patch is a no-op
(idempotent)". Measured, the SAME patch gives two different outcomes:
```
$ gopi memsync --import patch.json --db <db whose rows were human-stored>
imported: stored=0 deduped=0 refused=2                                       rc=2
  refused: row 0: question already owned by a local non-sync row — human wins
$ gopi memsync --import patch.json --db <fresh db>    → stored=2  rc=0
$ gopi memsync --import patch.json --db <fresh db>    → deduped=2 rc=0   (idempotent: 2 rows before/after)
```
The ownership rule and the partial-refusal exit code exist on no page a user
reads. Filed `01a0de51-5959-7eb7-96c4-e143a294c3f1`.

**F5-2 (F2+F3, medium) — the group prints pointers at spellings its own page
disowns.** Following the surface's OWN output verbatim:
```
$ gopi memory list --db $DB
1 most recent record(s):
#1 [cold] probe topic alpha renamed
    Q: How do I prove slice5 store works?
(details: memory_get <id>)                                                   rc=0
$ gopi memory search "nothing at all" --db $DB     # empty scratch DB
No distilled memory found for this query. … call memory(action=store) to save what you learned …   rc=0
$ gopi memory_get 1 --db $DB
#1 …                                                                         rc=0
note: gopi memory_get is deprecated — use gopi memory get                    (stderr)
$ gopi memory store action=store --db $DB
gopi memory store: unexpected positional argument "action=store"             rc=1
```
`memory -h` calls the grouped form canonical and names the legacy verb in its own
enumeration line, yet the runtime message sends the user to the legacy verb —
which then refuses to be quiet about it. The empty-result sentence, by contrast,
is a POSITIVE result for the briefing's question: rc=0, a sentence naming the
state and the next action, never an error. Filed
`01a0de51-59ab-7799-9c53-ef0d003dcdff`.

**F5-3 (F2+F3, high) — `brew` swallows an unknown flag and runs anyway.** A/B on
the same bounded work, endpoint refused, differing only in the mistyped token:
```
$ gopi brew --dry-run --limit 1   (ANTHROPIC_BASE_URL=http://127.0.0.1:9)
brew done in 7.3s: sessions=1 chunks=0 accepted=0 rejected=0 stored=0 deduped=0 errors=1   rc=0
$ gopi brew --dry-run --limit 1 --bogus-flag
brew done in 7.0s: sessions=1 … errors=1                                                    rc=0
$ gopi brew --dry-runn --limit 1
brew done in 6.6s: sessions=1 … errors=1                                                    rc=0
control, same binary/sandbox:  gopi memory search probe --bogus → unknown flag --bogus  rc=1
                               gopi memory list --bogus        → unknown flag --bogus  rc=1
                               gopi memsync --exprot --out …   → specify --export / …   rc=1
```
Every sibling verb refuses; `brew` performs the sweep. The same page's closed
flag list omits the `--db PATH` that `brew` in fact accepts (measured working),
so the write target is settable but undiscoverable — while the unflagged typo is
the dangerous case (`--limt 5` sweeps AND stores, reporting `rc=0`). Filed
`01a0de52-ff2c-7148-9ad5-f74f0b7d175d`.

**F5-4 (F4+F3, medium) — `memory search --intent X` eats its value as the query,
silently.** `--limit` and `--intent` sit on adjacent lines of the same flag block,
and the page's Usage puts `"<query>"` first — but only one of them tolerates
flag-before-positional:
```
$ gopi memory search --limit 3 prove --db $DB     → #1 probe topic alpha …                rc=0  (fine)
$ gopi memory search --intent prove --db $DB      → missing <query> argument              rc=1
$ gopi memory search --intent debug --db $DB      → "No distilled memory found for this query…"  rc=0   ← for a query the user never wrote
$ gopi memory search --intent debug --db $DB "prove"  → #1 … (ran on "debug"; "prove" discarded)  rc=0
$ gopi memory search prove --intent debug --db $DB    → #1 …                              rc=0  (the documented order)
```
Shape 4 is the sharp one: the user's real query is discarded and a wrong result
is returned as success, with no message. Filed
`01a0de52-ff78-753c-acf7-014290bccd22`.

### What held up (positive results, worth not re-testing)

- **The `--db` knob is exactly as documented, and `--db` beats the env var.**
  With `GOPI_MEMORY_DB=<env.db>` set and `--db <db>` also passed, the row landed
  in `<db>` (env.db stayed at 1 row) — precedence is real, not aspirational.
- **`store` → `search` round-trips, and the id is usable everywhere**: `Memory
  stored (id=1).` then `search` prints `#1 probe topic alpha tier=warm conf=0.85
  lesson=recipe when=… tokens=4` + Q/A + `keywords:`, and `memory get 1`,
  `memory update 1 --tier cold` (`Memory updated (id=1).`), `memory list`,
  `memory delete 1`, `memsync --export` all address it by that id.
- **The dedup signal is honest and names its similarity**: a second identical
  store printed `Memory stored (id=1, deduped into an existing entry,
  similarity=1.000).`
- **A nonexistent id is refused by EVERY verb that takes one**, rc=1, naming the
  id: `update`/`feedback`/`delete` → `no record with id 999`; `get` → `memory_get:
  sql: no rows in result set`.
- **Argument validation is real on the memory verbs**: `--confidence 2` →
  `--confidence must be a number in [0,1], got "2"`; `id must be a positive
  integer, got "abc"`; `--limit "0"|"-1"|"abc"` → `--limit must be a positive
  integer, got "…"`; `store` with no `--topic` → `Missing required parameter:
  topic (--topic)`; `search` with no query → `missing <query> argument`;
  `update` with no field → `nothing to update (set at least one field)`; a bad
  sub-verb → `unknown subcommand "frobnicate" — want search|store|get|list|
  update|delete|feedback|sync|provenance|backup`.
- **The one-vote-per-record-per-task rule the page states is enforced and says
  so**: a repeat vote in the same task printed `record 1 already voted useful in
  this task — one vote per record per task (ignored)`.
- **The audit branches work off `--db`**: `feedback --list` shows the vote it
  read (`#1 dbflag topic useful=1.00 useless=0.00 fb=0.67(1)`), and the same
  command WITHOUT `--db` correctly reported `No feedback recorded yet.` against
  the sandbox's own (empty) ledger; `--suggest-cold` names its threshold (`need
  useless ≥ 3 and useful = 0`); `--recompute` reports the population it rebuilt.
- **`memory sync`'s declared-never-discovered topology is honest**: `--peer
  no-such-peer` → `peer "no-such-peer" is not in the memory_peers whitelist
  (settings.json "memory_peers" / GOPI_MEMORY_PEERS)`.
- **`memsync` refuses short/absent arguments by name, rc=1**: no selector →
  the full want-list; `--export` without `--out` → `--export requires --out
  PATH`; `--import /nonexistent-patch.json` → the open error naming the path;
  `--export --out /nonexistent-dir/…` → the write error naming the path;
  `--import` of a real patch into a db where the question is free → `stored=N`,
  and re-import → `deduped=N` with the row count unchanged (idempotency holds).
- **`--dry-run` really is read-only**: `brew --dry-run --limit 1` (×3 spellings)
  left the sandbox KB at **0 rows before and after**.
- **`brew`'s own validations fire where a value is required**: `--limit 0|-3|abc`
  → `--limit must be a positive integer` rc=1; a positional → `unexpected
  argument "boguspositional"`; `--session /nonexistent` → an error line naming
  the path; `--json` emits the same numbers as a JSON object.
- **`memory provenance` reports an honest unknown rather than inventing one**:
  `addressable: 0 untraceable: 1` with `sources is empty — no origin recorded at
  all (honest unknown, never fabricated)`.
- **The `--intent` flag is not *ignored*** — it is mis-parsed (F5-4, filed);
  what it does NOT do is silently change ranking without effect.

### Probes that produced no finding (tabulated so they are not re-run)

| probe (verbatim, `$SB`/`$DB` as above) | rc | result |
|---|---|---|
| `memory search "nothing at all" --db <fresh empty db>` | 0 | "No distilled memory found for this query …" — the empty case is a sentence, not an error |
| `memory search "prove slice5 store works" --db $DB` | 0 | `#1 … tokens=4` + Q/A + keywords — the round-trip |
| `memory search prove --db $DB` / `--limit 3 prove --db $DB` | 0 | one hit, tokens=1 — both orders fine for `--limit` |
| `memory list --db $DB` / `--limit 2` / `--limit 3 --db` | 0 | `1 most recent record(s):` + id + tier + topic + Q |
| `memory list --limit 0 --db $DB` | 1 | `--limit must be a positive integer, got "0"` |
| `memory get 1 --db $DB` / `memory get 999 --db <empty>` | 0 / 1 | full record / `memory_get: sql: no rows in result set` |
| `memory store --topic … --question … --answer … --keywords … --db $DB` | 0 | `Memory stored (id=1).` |
| `memory store` (identical Q again) | 0 | dedup signal with `similarity=1.000` |
| `memory store --db $DB` (no fields) | 1 | `Missing required parameter: topic (--topic)` + usage |
| `memory update 1 --topic … --db $DB` | 0 | `Memory updated (id=1).` |
| `memory update 1 --tier=cold --db $DB` (equals form) | 0 | same |
| `memory update --db $DB --tier cold 1` (flags first) | 0 | same |
| `memory update --id 1 --tier cold --db $DB` | 0 | same (`--id` also accepted) |
| `memory update 1 --keywords "a,b" --db $DB` | 0 | same |
| `memory update 1 --tier cold --db $DB` (repeat) | 0 | idempotent — field already cold |
| `memory update 999 --tier cold --db $DB` | 1 | `no record with id 999` |
| `memory update 1 --confidence 2 --db $DB` | 1 | `--confidence must be a number in [0,1], got "2"` |
| `memory update 1 --confidence 0.5 --db $DB` | 0 | `Memory updated (id=1).` (conf 0.85 → 0.50 read back) |
| `memory update abc --tier cold --db $DB` | 1 | `id must be a positive integer, got "abc"` |
| `memory delete 2 --db $DB` (then `search` for its question) | 0 | deleted; the FTS hit is gone too (0 hits) |
| `memory delete 999 --db $DB` | 1 | `no record with id 999` |
| `memory feedback 1 --useful --db $DB` ×2 | 0 | vote recorded, mass/score/step printed |
| `memory feedback 1 --useful --task T1` then again | 0 / 0 | second is the ignored-duplicate note |
| `memory feedback 999 --useful --db $DB` | 1 | `no record with id 999` |
| `memory feedback 1 --helpful --db $DB` | 1 | `unknown flag --helpful` |
| `memory feedback 1 --db $DB` (no rating) | 1 | `exactly one of --useful / --useless / --unused is required` |
| `memory feedback 1 --rating useful --db $DB` | 1 | `unknown flag --rating` — the CLI shape is the three booleans |
| `memory feedback abc --useful --db $DB` | 1 | `id must be a positive integer, got "abc"` |
| `memory feedback --list --db $D` / `--suggest-cold` / `--recompute` | 0 | the audit branches, each naming what it read |
| `memory feedback --list` (no `--db`) | 0 | `No feedback recorded yet.` — the default ledger, correctly |
| `memory provenance --shape --db $DB` / `--counts` | 0 | distribution + persisted counts |
| `memory sync --peer no-such-peer --db $DB` | 1 | whitelist refusal, names the settings key and the env var |
| `memory sync --serve-assets` (empty stdin) | 1 | `parse stdin: unexpected end of JSON input` |
| `memory backup --no-ship --dir … --db $DB` | 0 | `ledger backup:` + source + snapshot size + sha256 |
| `memory backup -h` | 0 | its own 4514-byte page |
| `memsync` (no selector) / `memsync --verboten` / `--exprot` typo | 1 | the want-list, rc=1 |
| `memsync --export` (no `--out`) | 1 | `--export requires --out PATH` |
| `memsync --import /nonexistent-patch.json --db $DB` | 1 | the open error, naming the path |
| `memsync --export --out /nonexistent-dir/patch.json --db $DB` | 1 | the write error, naming the path |
| `memsync --export --out patch.json --db $DB` | 0 | `exported 2 rows (2 scanned, 0 skipped by --since) → … (node …)` |
| `memsync --export --out patch2.json --db <dup.db, node-tagged rows>` | 0 | `exported 0 rows` — anti-reflow holds (node:* never re-exported) |
| `memsync --import patch.json --db <dup.db>` twice | 0 / 0 | `stored=2` then `deduped=2`, 2 rows before and after |
| `brew --dry-run --limit 1` (×2 spellings) | 0 | sweep summary; 0 KB rows written |
| `brew --dry-run --limit 1 --db $DB` | 0 | same (the undocumented-but-accepted flag) |
| `brew --limit 0` / `-3` / `abc` (dry-run) | 1 | `--limit must be a positive integer` |
| `brew --session /nonexistent-session.jsonl` | 0 | honest `errors=1` + the open error line |
| `brew --dir /nonexistent-dir` | 0 | `sessions=0 errors=0` — nothing to sweep is not an error |
| `brew --json --limit 1` | 0 | the same numbers as JSON |
| `gopi memory` (no sub-verb) | 1 | the group page on stderr (slice 1's `task -h` shape, inverted) |
| `memory frobnicate --db $DB` | 1 | `unknown subcommand "frobnicate" — want …` — **not** the prompt fallback |
| `memory store` with 2 positional words and no `--db` in scope | — | not probed; the prompt fallback for `memory` was reachable only by omitting the verb itself |

### Instrument note — three batches were mis-written and re-run (recorded, not findings)

This sweep's own argv was wrong three times, and the error is instructive enough
to keep: a probe written as `gopi update 999 --tier cold --db $DB` (the `memory`
prefix dropped) has a **non-verb at `argv[1]`** — so `--tier` was refused as an
unknown TOP-LEVEL flag (rc=1), and, where only `--db` was in scope, the two
positionals became the run path's prompt and a **real agent run** started
(`rc=124` under the time bound, 7 sandbox sessions minted, each carrying a raw
log). The detector that caught it is slice 2's: a session directory minted under
`$SB/.gopi/sessions/` is written only by the run path. `brew`'s three "unknown
flag" probes were the same mistake (`b0`/`b0b`/`b0c`). All of it landed inside
the sandbox — the operator's ledger carries **no** probe row and the operator's
`distilled_qa` carries **no** probe topic — but a mis-routed probe costs a real
run, so: **check the sub-verb's own prefix is at argv[1] AND at argv[2] before
reading any output**.

### Isolation evidence (this sweep)

| fact | value |
|---|---|
| operator ledger `select count(*) from tasks` | **3131 → 3137** (+4 own `bug` rows, +2 foreign rows attributed) |
| operator ledger md5 | `ec835b5dec4fdbb808d308e744431320` → `2f9ad2f449d2ebf2fef8a2946c1caa3c` |
| operator `distilled_qa` rows matching any probe topic | **0** |
| operator ledger rows matching a probe token (`%bogus-flag%`, `%--exprot%`) | 0 |
| sandbox | `sandbox-euaudit5m-776ac449`, device **66309** vs `/` **66307** (off-root, Rule 22) |
| write probes' DBs | scratch `--db` files under the run's evidence dir: `scratch/scratch.db`, `scratch/env.db`, `scratch/dup.db`, `scratch/d3-*.db`, `scratch/d4-*.db`, `scratch/empty.db`; `GOPI_MEMORY_DB` measured to be honoured, and `--db` measured to beat it |
| sandbox sessions minted | 7 — all of them from the three mis-routed batches above, none from a correct probe |
| operator profile during the sweep | `/home/user/.gopi/memory.db` size 139 649 024 unchanged (only the ledger rows above were written) |
## Eighth sweep — the primary run path (slice 8, 2026-09-26)

**Scope**: `gopi "<prompt>"`, `gopi [options] input.txt`, stdin, `@PATH` / `@-` /
`\@text`, `.png` attach, `--prompt TEXT`, refusals. Oracle `gopi -h`
(10 092 B, md5 `e45a708aa8eb6856fe8a7355c96eb635`) read before any source; there is
no `gopi <command> -h` for the run path, so that page IS its whole contract.

**53 probes → 3 divergence rows (RP-1 F1, RP-2 F2, RP-3 F2) and 11 promises that
held.** Report: `devdoc/end-user-audit-slice8-runpath.md`.

### Method refinements this sweep adds (reusable)

1. **THE REQUEST BOUNDARY IS THE ONLY ORACLE FOR A "WHAT REACHED THE MODEL" CLAIM.**
   rc/stdout/stderr cannot see the payload. Stand up a **local echoing endpoint** —
   an Anthropic-shaped server that records every request body and answers with a
   minimal valid SSE stream — and point `--base-url` at it. Payload fidelity, image
   attachment and the vision auto-switch then become *measured* facts, at **0 real
   tokens**, with bodies left on disk (`evidence/echo/req-NNN.json`). Recipe:
   ~120 lines of `http.server` + a 7-event SSE body (`message_start`,
   `content_block_start`, `ping`, `content_block_delta`, `content_block_stop`,
   `message_delta`, `message_stop`); a `delta` of one text block is enough — the
   real client accepts it and exits 0.
2. **BUILD THE BINARY FROM YOUR OWN WORKTREE.** `gopi` on `$PATH` is a symlink into
   the main checkout (`/usr/local/bin/gopi → /home/user/work/GoPi/bin/gopi`, a *different*
   commit from this run's worktree). Auditing the installed binary audits somebody
   else's commit: `CGO_ENABLED=0 go build -o <evidence>/bin/gopi ./cmd/agent/`.
3. **`env -i` IS PART OF THE PROOF for "the run path does this".** A probe run inside
   a task inherits ~20 `GOPI_*` variables, and the run path's prompt framing was
   *reproduced with `env -i`* before it was believed. Without that, a finding about
   framing is indistinguishable from the parent harness's own environment.
4. **A SHELL HELPER'S SIGNATURE IS AN INSTRUMENT THAT CAN LIE — RECORD argv, DO NOT
   ASSUME IT.** Three probes reached the real API because a helper
   (`runsp(){ n=$1; sd=$2; shift 2; … }`) was called with one leading argument, which
   made it **consume `--base-url` as the label**. The recorded argv — not the intent —
   is what caught it. Corollary: when a probe's isolation fails, treat it as an
   instrument defect until the root cause is measured; the 4-cell A/B/C/D control
   (varying `--vision-base-url` / `--vision-model` / `--model`, counting echo requests
   vs real-API dumps) proved **no gopi fallback was involved** — filing it would have
   been a false finding. Cost of the accident: 0 tokens (HTTP 400 model rejection,
   `prompt=0 completion=0` in every trajectory summary).
5. **ORDER THE PROBE PASS SO ISOLATION EXISTS BEFORE THE FIRST RUN-CAPABLE PROBE.**
   This sweep's "purity first" batch ran un-sandboxed and one probe (a *nonexistent
   file* positional) started a real run in the operator profile — 2 session dirs and
   1 auto task row. The sandbox is cheap; create it first.
6. **A MISSING FILE IS NOT AN ERROR ON THIS PATH** — see RP-2: the two spellings of
   one input have opposite semantics, which is exactly the kind of class the
   file-input probes must cover.

### Findings (five-field rows filed via `task_submit`)

- **RP-1 (F1, high)** — a plain shell prompt is wrapped in the agent-harness frame:
  the wire's single user message is `[EPIC GOAL]\n<your text>\n\nBased on this Epic,
  [TASK_LIST_BEGIN] … [EPIC REFRAME] … THE BUDGET IS PART OF THE CRITERION …`
  (~1000 chars around the text) for every input form, while `gopi -h` says "The task
  text". Row `01a0dec6-3a8a-7a5d-986f-2d6c1a5d8dee`.
- **RP-2 (F2, high)** — a **nonexistent** file positional is not refused: rc=0, a run
  starts (session + ledger row), and the *path string* is sent to the model as the
  task text. The `@PATH` spelling of the identical defect refuses by name, rc=1.
  Row `01a0dec6-280c-71f8-adf4-90067106a49f`.
- **RP-3 (F2, high)** — with `--prompt TEXT`, a non-image file positional is
  **dropped in silence** (instruction reaches the wire, the file appears nowhere)
  although the option line says "positionals become files/images". The `.png` half of
  the same promise holds. Row `01a0dec6-2863-77ee-9051-54c313f67e89`.

### What held (no-divergence, so the next slice need not re-probe)

no-prompt+empty-stdin → help on **stdout**, rc=1, byte-identical to `gopi -h` (S1) ·
bare `--`, `-z`, `--wat` refusals with the documented `--name` vocabulary (S3, S10,
S14) · `@PATH` verbatim through quotes/`$VAR`/`$( )`/globs (E3) · `@-` from stdin
(S12) · `\@text` literal, also as `--prompt '\@x'` (S13, S20) · stdin as an input
form (S11) · `.png` positional attached NATIVELY with the wire model switching to
the vision model (E5, S29, S31 — `"model": "model-VISION"`) · `--prompt TEXT`
(S16) · a directory positional refused by name, rc=1 (S4) · empty file / empty stdin
→ `No input provided`, rc=1 (S18, S19, S22) · `@`-alone and `--prompt @` refused
identically (S7, S8) · a flag placed *before* a verb now gets a **named, actionable
refusal** rather than being swallowed as a prompt (S15 — the RS-1 class from the
third sweep, fixed here).

### Isolation evidence (this sweep)

| fact | value |
|---|---|
| sandbox | `sandbox-slice8-e94bb50a`, device **66309** vs `/` **66307** (off-root by device id, Rule 22) |
| audited binary | built from this worktree, sha256 `acb2f941708dba48149417dfd53a6157b92757f1f105d428fb0421d418a54059` |
| operator ledger — probe payload | `select count(*) from tasks where payload like '%SLICE8TOKEN%'` = **2**, and both rows are this audit's OWN filed bug rows (RP-1 id 3172, RP-3 id 3171, attributed by `created_at`); `distilled_qa` matching = **0**; size 140 853 248 unchanged |
| operator sessions | 7509 → 7513, of which 2 are this run's own `task_submit` sessions (named by task id) and the rest are foreign runs on a live machine. One un-sandboxed `P8` probe did create 2 dirs + 1 auto row — see method note 5 |
| sandbox cost | 33 session dirs; `tasks` 33 rows, `task_events` 33; `distilled_qa` 0; the echo endpoint recorded **56** request bodies |
| token cost | **0 real tokens** — echo endpoint for payload probes, `--base-url http://127.0.0.1:9` for the rest, HTTP 400 rejections (`prompt=0 completion=0`) for the 3 instrumentation slips |
## Roadmap — how this epic is decomposed

Each slice above is one task (60-minute budget each) and each is a valid
`task_submit` on its own: "read your slice's help, probe it as a user inside a
sandbox, submit every divergence in the step-4 shape". Findings filed by the
sweeps so far: TS-1 `01a0dcf5-27a2-…`, TS-2 (no row of its own — its home is
slice 7), SB-1 `01a0dcf5-27d3-…`, SI-1 `01a0dcfa-9818-…`, RS-1
`01a0dd02-d0a2-…`, RS-2 `01a0dd02-e387-…`, RS-3 `01a0dd02-f8d5-…`, slice 3's
`01a0dd87-8242`/`-8283`/`-82d5`/`-8315`/`-8351`/`-838f` and its post-sweep
`01a0dd8f-c014` (the near-miss `GOPI_HOME` — CW-7), and slice 5's
`01a0de51-5959`/`-59ab` + `01a0de52-ff2c`/`-ff78`. The
decomposition is enqueued as one child row per slice (the epic's own reframe,
2026-09-26):

| slice | child task row |
|---|---|
| 1 task ledger | `01a0dcfa-d991-70a7-9c52-68b79abfdaed` |
| 3 chat & web | `01a0dcfa-d9a6-71c1-9bb8-5a6b04e1fe23` |
| 4 tools (finish) | `01a0dcfa-d9bd-7673-9a3d-be64d59f60e4` |
| 5 memory & knowledge | `01a0dcfa-d9d8-7d55-bae5-de49deef3b58` |
| 7 environment table | `01a0dcfa-dae2-75a1-b3d2-761cc02674e6` |
| 8 the primary run path | `01a0dcfa-dafb-709d-808c-8bc93369aa63` |

Slice 8 (the primary run path) is the only one that needs an LLM endpoint; it is
doable with a mocked/refused endpoint plus the sandbox, and its findings are
about argv handling, not answer quality.

Two files a slice must update: this document (its findings row, and the state
column of its own row in the map above) and the session evidence directory of
its own run (raw outputs).

### Slice-8 closing note (the endpoint-mocking question, answered)

The slice-8 row above said "expensive — mock the endpoint". Measured this sweep:
**mocking is not a workaround, it is strictly better than a live endpoint.** A local
Anthropic-shaped echo server makes the *request body* an artifact, so claims about what
the model was told stop being inferences from stdout and become byte-level evidence —
at zero cost, and with no risk of a probe shipping the operator's payload to a
third party. A live endpoint can only ever confirm "something was sent". The refused
endpoint (`--base-url http://127.0.0.1:9`) remains the right instrument for the
*surfaces* (rc, refusals, side effects); the **echo** endpoint is the right instrument
for the *payload*. Both are 0-token; run the payload probes against the echo server and
keep the refusal for probes whose only question is "did it start a run".
