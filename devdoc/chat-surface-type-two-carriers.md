<!-- doc-class: historical -->
# `chat_surface action=type`: the two carriers, and the two answers — measured, not read

Task `01a1071e-6a76-7535-b104-5768d3585e47`. Epic: produce the "no seam vs seam"
acceptance evidence for `chat_surface action=type` — the CARRIER refusal and the
BOUND refusal are two different carriers' answers, and the bound sentence is
STRUCTURALLY UNREACHABLE on a run that has no seam.

This document is a RECORD of one measurement run on the real binary. Every
theorem-shaped claim below has a verbatim answer, a command and a session id
behind it; the claim that `typed` stays 0 on the no-seam run is supported by the
run's own dump header and stderr, not by reading `chatSurfaceType`'s source.

## The two answers (verbatim, as the session recorded them)

**A — the CARRIER refusal** (run `6558b3e6-5ca2-4524-b1c8-9e5f1d929ecb`, a
`--once` run whose stdout is a pipe):

```
chat_surface action=type refused: this run has no next-turn seam: the input line editor is not active (a piped, one-shot or --no-editor run reads its lines from the caller). The tool can still SEE this surface, but nothing can be typed into it.
```

**B — the two seam answers** (run `93167c72-93b6-44d1-aa3d-89c70f903f80`, a real
pty with the line editor armed, driven by a scripted model, period bound 2):

```
chat_surface action=type typed=true bound_in_force=2 turns_used=1/2 pending=true — this line becomes the NEXT turn when the current one ends. It is your own input: write it as the user would (a question or an instruction), not as a note to yourself.
```

```
chat_surface action=type typed=true bound_in_force=2 turns_used=2/2 pending=true — this line becomes the NEXT turn when the current one ends. It is your own input: write it as the user would (a question or an instruction), not as a note to yourself.
```

and, on the ask AFTER the bound was spent:

```
chat_surface action=type refused: this period's self-driven loop is at its bound (2 of 2 turns used) — it does not extend itself, and only a line the OPERATOR submits opens a fresh period. Continue the work with an ordinary turn (your answer is itself a turn) and say in it what the operator should type to let you continue autonomously; the bound also rises to a larger value in a NEW session.
```

**The same arm at the DECLARED bound** (run `9a6269e9-0171-4b5e-81ba-654742c7640a`,
`--max-turns 8`): eight `typed=true bound_in_force=8` answers
(`turns_used=1/8` … `8/8`) and then the refusal with the number in force
substituted —

```
chat_surface action=type refused: this period's self-driven loop is at its bound (8 of 8 turns used) — it does not extend itself, and only a line the OPERATOR submits opens a fresh period. Continue the work with an ordinary turn (your answer is itself a turn) and say in it what the operator should type to let you continue autonomously; the bound also rises to a larger value in a NEW session.
```

Both bounds were RUN, not one inferred from the other (`runs3/` = floor 2,
`runs8/` = declared 8).

## Completed

- A collector that reproduces BOTH carriers in one command
  (`chat_type_seam_arms.py --all --out <dir>`; per-arm `--arm A|B`, `--bin` to
  point at another build) — the scripted SSE model, the pty leg and the verdict
  extraction are all in it.
- Arm A's verbatim answer and the evidence that it is a CARRIER answer and not a
  count: three asks, `typed` never appears, the dump header reads `terminal=0x0 …
  frames=0`, and the run's own stderr says no reserved layout was installed.
- Arm B's verbatim answers on a real pty: two `typed=true bound_in_force=2 …`
  returns (`turns_used=1/2`, then `2/2`) and the bound refusal on the third ask,
  with the charged turn boundary visible on the console.
- The exact argv, session ids, session records, console/stderr streams, dump
  headers and mock decision logs, under
  `/home/user/.gopi/sessions/01a1071e-6a76-7535-b104-5768d3585e47/evidence/`
  (`runs3/` is the canonical set; `README.md` says which run is which).
- This devdoc, classified in `devdoc/DOC-INDEX.tsv` in the same commit.

## Why A's answer is a carrier's answer, not a count

Three asks, three times the SAME carrier refusal, and `typed` never moves:

| run | asks made | `typed=true` answers | carrier refusals | bound refusals |
|---|---|---|---|---|
| A (no seam) | 3 | **0** | 3 | **0** |
| B (seam) | 3 | 2 | 0 | 1 |

The counting is done by the collector from the session record, and printed in
`arm-X/result.json` (`carrier_refusal_count` / `typed_true_count` /
`bound_refusal_count`) — never by reading the source.

The run's own artifacts say WHY:

- A's dump header (the first line of `arm-A/chat-screen.dump`):
  `#gopi-chat-dump pid=181609 target=self:chat-tools tools=on terminal=0x0
  model=0x0 grid=80x24 geometry=fallback mode=ansi frames=0 dumps=1 …` —
  `terminal=0x0`, `frames=0`: the console's stdout is not a terminal (it is the
  pipe), no reserved layout was ever armed, so there is no line editor.
- A's stderr says it in the run's own words:
  `gopi chat: no reserved layout is installed on this run, so the screen dump
  will stay an unpainted grid (frames=0) — the dump observes the answer band, and
  there is none here.`
- B's dump header, same slot, same binary:
  `#gopi-chat-dump pid=182456 … terminal=100x30 model=100x30 grid=100x30
  geometry=layout mode=ansi frames=267 dumps=35 …` — the terminal was measured,
  the layout was installed, frames were interpreted.
- B's console carried the charged boundary, once per self-driven turn:
  `[gopi] self-driven turn 1/2 of this period (typed by the chat_surface tool) — this loop stops at its bound` then `… turn 2/2 …`.

So A's refusal is decided by the CARRIER (there is nothing to type into), before
anything counts, and the count it would take to reach the bound sentence can
never start. Three asks, not one, because the claim is "the same refusal however
many times you ask" — re-asking is a measurement here, not advice.

## How to re-run it

One command collects both arms, on this machine, from the built binary:

```bash
python3 /home/user/.gopi/sessions/01a1071e-6a76-7535-b104-5768d3585e47/evidence/chat_type_seam_arms.py \
    --all --out runs3
```

Per-arm spellings: `--arm A` / `--arm B`, `--bin <gopi>` to point at another
build, `--max-turns N` to run arm B's period at another bound (`2` = the floor,
the default; `8` = the declared `chatSurfaceLoopBound`). The default run was made
against
`/home/user/.gopi/sessions/01a1071e-6a76-7535-b104-5768d3585e47/evidence/bin/gopi`
(built by `CGO_ENABLED=0 go build -o … ./cmd/agent/`).

What it does, in the order the two carriers need:

- **A arm** — `gopi chat --tools --dump-screen=<path> --once -p start`, stdout
  redirected to a file (a pipe, so stdin is not a tty). The scripted model asks
  for a `type` on every round and then answers with text, so the run ends by
  itself. Exit 0, 4 mock requests.
- **B arm** — the same binary under `pty.fork()` with the geometry installed by
  `TIOCSWINSZ` (100x30), so the reserved layout and the line editor exist; the
  driver types the bootstrap line and the Enter as TWO writes (in one write the
  editor reads them as a paste and no turn starts), then `/quit`. The scripted
  model alternates ask / plain answer, which is what makes each ask cross the
  TURN BOUNDARY the bound is charged at.

Both arms use a private `GOPI_HOME` and a loopback SSE stub
(`ANTHROPIC_BASE_URL=http://127.0.0.1:<port>`), so no provider is contacted and
no credential is needed. Every path the script writes is absolute and lives under
the session directory named in the briefing — the worktree holds only this
document.

**Measured pitfall, worth keeping:** the bound is charged at the TURN BOUNDARY,
so a stub that asks three times inside one turn gets `turns_used=1/2` on all
three answers and never reaches the bound sentence (observed on the first B run:
`typed=true … turns_used=1/2` three times, with the third answer REPLACING an
earlier pending line). The mock must END a turn between two asks.

**A second measured pitfall:** a relative `GOPI_HOME` is resolved against the
child's cwd, so the driver then looks for the session in the wrong tree
(`runs/runs/home-B/.gopi/…`); the script resolves its paths to absolute first.

**A third measured pitfall, and it bit the collector itself:** the answer reader
originally de-duplicated on the WHOLE answer string. Two answers from the same arm
differ only in their leading `[t+Ns]` stamp, so whenever the run was fast enough
for all three asks to land in the same second, three identical refusals collapsed
into **one** and `arm-A/result.json` read `carrier_refusal_count=1` while the
session record held three (`answers_recorded=3`). The count of "how many answers
did this arm receive" was a function of WALL TIME — the same defect Rule 15 names,
a scan that found less than the tree holds, reported as a fact. The reader now
keeps every answer and reports `answers_recorded` beside the three counts, so a
shrinking count is visible instead of silent. Evidence: `runs-recheck/arm-A/`
(3/3 after the fix) against the same run before it (`1` recorded answers).

## Where the raw evidence lives

Under `/home/user/.gopi/sessions/01a1071e-6a76-7535-b104-5768d3585e47/evidence/`:

- `chat_type_seam_arms.py` — the collector;
- `runs3/arm-{A,B}/session.jsonl` — the child's own session record (the answers
  above are quoted from it, `session.path` names the original);
- `runs3/arm-{A,B}/stdout.raw`, `stderr.raw` (A), `console.raw` and
  `console.stripped.txt` (the pty byte stream, ANSI removed) (B);
- `runs3/arm-{A,B}/chat-screen.dump` — the dump headers quoted above;
- `runs3/arm-{A,B}/mock-decisions.jsonl` — what the stub answered, per request,
  with the counts it saw in the replay;
- `runs3/arm-{A,B}/command.txt`, `driver.log`, `result.json` — the exact argv, the
  driver's timeline, and the machine-readable verdict.
- `runs8/arm-B/` — the same B arm at the DECLARED bound (`--max-turns 8`): its
  `result.json` (`typed_true_count=8`, `bound_refusal_count=1`) and its
  `session.jsonl` are where the `(8 of 8 turns used)` refusal is quoted from.
- `runs-recheck/` — a later re-collection with the FIXED counter (see the next
  pitfall): both arms from one command, and its `arm-A/result.json` reads
  `"carrier_refusal_count": 3` with `"answers_recorded": 3`. Treat `runs3/` and
  `runs-recheck/` as the same measurement made twice; `runs-recheck/` is the one
  whose counts came from the corrected reader.

One line is worth reading on both surfaces, because it is the SAME line doing the
same job: each run announces where its own screen can be read. Arm A's stderr
says the tool's `see`/`compare` have a dump to read while `type` cannot be
satisfied; arm B's console prints the operator's arm renewing the budget
(`budget RENEWED by your own submitted line`) when `/quit`… — that sentence is
what makes the bound refusal's remedy ("a line the OPERATOR submits") a path that
exists rather than a slogan, and it is emitted on the arm a model-written line can
never take.

## Not Done (honest)

- The B arm was measured at BOTH bounds: the floor `max_turns: 2`
  (`chatSurfaceRestartFloor`) and the DECLARED `max_turns: 8`
  (`chatSurfaceLoopBound`) — the second run is `runs8/`, and its refusal reads
  `(8 of 8 turns used)`. The two runs' refusals are the same string with the two
  numbers substituted, which is what makes the floor run a valid stand-in for the
  declared one; the declared spelling was run anyway rather than asserted.
- `action=key`'s carrier refusal ("this run has no line editor …") was not part
  of this epic and was not exercised.
- The no-seam arm is the `--once` (piped) spelling. The `--no-editor` spelling is
  the same predicate (`s.input.Mode() != chatInputEditor`) but was not run
  separately.
- `make test-budget PKGS='./internal/standards ./internal/docindex ./cmd/agent'`
  finished at 657s wall with **exit 1**: `internal/standards` and
  `internal/docindex` pass, `cmd/agent` FAILS on one test —
  `TestWorkerDaemon_RestartReplacesTheDaemon` (`the flock outlived its owner…`),
  under the stream's own recorded load `10.90 10.76 8.76` on 4 cores. That test
  was then re-run ALONE and passed in BOTH trees (`ok … 14.875s` here, `ok …
  24.321s` in the main checkout at master's tip `29ea0bb1`), so the failure is
  load-sensitive rather than diff-sensitive — but the suite's own verdict is
  FAIL, and it is reported as such. Separately,
  `go test ./internal/agent -run TestDocAnchorsResolveInNormativeDocs`
  fails — 4 new + 1 stale baseline entries — and the SAME failure reproduces in the
  main checkout at master's tip (`/home/user/work/GoPi`, commit `29ea0bb1`), a tree
  that does not contain this document at all (checked: the failure output does not
  mention it). So it is pre-existing drift in the doc-anchor baseline
  (`internal/agent/testdata/docanchor-baseline.tsv`), not something this change
  introduced. Named rather than hidden: the branch therefore does not carry a green
  `internal/agent` suite, and the only suite verdict this report claims is the
  devdoc-corpus equality (`gopi doccheck` → 0 gaps, 2009 rows).

## Verification

- Build: `CGO_ENABLED=0 go build -o …/evidence/bin/gopi ./cmd/agent/` → OK;
  binary reports `v0.1.0`, `go1.24.5`, `linux/amd64`.
- Tests: `gopi doccheck` → exit 0, corpus consistent. `make test-budget
  PKGS='./internal/standards ./internal/docindex ./cmd/agent'` → exit 1 (657s):
  standards + docindex pass, `cmd/agent` fails ONE test
  (`TestWorkerDaemon_RestartReplacesTheDaemon`, a load-sensitive daemon-singleton
  flake) which re-run alone passes in both this worktree and the main checkout.
  No Go code changes here, so no test is added or modified; the surfaces this
  document describes are covered by `cmd/agent/chat_surface_bound_e2e_test.go` and
  `cmd/agent/chat_inject_pty_test.go`.
- Ran: see "How to re-run it" — `python3 chat_type_seam_arms.py --all --out runs3`
  (the collector's own JSON, `runs3/arm-A/result.json` + `runs3/arm-B/result.json`,
  was printed by that run),
  `python3 chat_type_seam_arms.py --arm B --out runs8 --max-turns 8`, and — after
  the answer-reader fix — `python3 chat_type_seam_arms.py --all --out runs-recheck`.
- Result: arm A — 3 asks, 3 carrier refusals, `typed_true_count=0`,
  `bound_refusal_count=0`, dump header `terminal=0x0 frames=0`; arm B — 2
  `typed=true bound_in_force=2` answers (`turns_used=1/2`, then `2/2`) and 1
  bound refusal, dump header `terminal=100x30 frames=267`; arm B at the declared
  bound — 8 `typed=true bound_in_force=8` answers (`1/8` … `8/8`) and 1 refusal
  reading `(8 of 8 turns used)`, dump header `terminal=100x30 frames=547`.

## Evidence (self-filed)

- `runs3/arm-A/result.json` → `"carrier_refusal_count": 3`, `"typed_true_count": 0`,
  `"bound_refusal_count": 0`, `"dump_header": "…terminal=0x0 … frames=0 dumps=1…"`.
- `runs3/arm-B/result.json` → `"carrier_refusal_count": 0`, `"typed_true_count": 2`,
  `"bound_refusal_count": 1`, `"dump_header": "…terminal=100x30 … frames=267 dumps=35…"`.
- `runs3/arm-A/stderr.raw` → the three `⚙ result chat_surface chat_surface
  action=type refused: this run has no next-turn seam: …` lines, and the closing
  `gopi chat: no reserved layout is installed on this run, so the screen dump
  will stay an unpainted grid (frames=0)`.
- `runs3/arm-B/console.stripped.txt` → `[gopi] self-driven turn 1/2 of this
  period …` and `[gopi] self-driven turn 2/2 of this period …`.
- `runs8/arm-B/result.json` → `"max_turns": 8`, `"typed_true_count": 8`,
  `"bound_refusal_count": 1`, and a `tool_answers` entry reading
  `… at its bound (8 of 8 turns used) …`.
- `runs-recheck/arm-A/result.json` → `"answers_recorded": 3`,
  `"carrier_refusal_count": 3` (the same run read `1` before the answer-reader
  fix — the pitfall above).
- Both collectors exit 0; each arm's `driver.log` ends with `exit=0`.
