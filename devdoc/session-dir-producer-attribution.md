<!-- doc-class: historical -->
# The producer of the extra-digit session-dir name — attribution, and the provenance clause

**Bug row** `01a1472c-2919-7ac0-b9f8-edb08c65a88d` (filed 2026-10-17 by the run that
landed the refusal arm), branch `task/01a1472c29197ac0b9f8edb08c65a88d`, baseline
`cc846ee`, host `hosta`.

**Prior art**: the id→path seam already refuses the extra-digit spelling —
branch `task/01a14596573f7b59b8cec2645a2c80f6`, commit `3c479d342`
(`devdoc/session-dir-extra-digit-spelling.md`). **That arm is NOT in this
baseline**: `git merge-base --is-ancestor 3c479d342 HEAD` → exit 1, and a test
driving the seam on this tree shows the extra-digit operand is still accepted
here (measured below). So this document does not re-fix the seam; it settles
WHO produced the string.

## Verdict: the producer is the RUN's own transcription — the model's own command text

Not go code, not the framework's environment, not a human at a shell. Measured
on the one container this shape has on disk, by splitting every occurrence in
the run's own session log by the channel it travelled in.

### The evidence, in the order it decides

**(1) The environment never carried the bad spelling.** Four independent reads
of the env var's literal value, all printed by `tool_result` — i.e. by the
environment, not by the model's prose — are the CANONICAL 36-char id:

```
$ python3 <split tool_result literals of the printed env value>
line  172  [CANON] REPORT_PATH=/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md
line  443  [CANON] REPORT_PATH_ENV=[/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md]
line  470  [CANON] TASKRPT=/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md
line  701  [CANON] R=/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md
line  756  [CANON] HARNESS=/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md
line  766  [CANON] R=/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md
```

Those lines are the SHAPE that decides it: `REPORT_PATH_ENV=[…]` and
`TASKRPT=…` are the string the env handed the shell, expanded by the shell
itself. Every one is canonical. **The very re-read that the previous run
performed by hand — and concluded from — is already in the transcript, and it
says the environment was never the source.**

**(2) The bad spelling first appears in a `tool_use` input — text the MODEL
authored — 23 lines before any tool_result carries it.**

```
$ python3 <first occurrence per channel>
FIRST MISS: (41, 'ASSISTANT_tool_use:task', input={"acceptance": "… Non-branch artifacts (A/B logs,
archives) go ONLY to /home/user/.gopi/sessions/01a1452c-928d-7e0f9-9ef3-52c1482cfbc3. Baseline …"})
EARLIEST tool_result carrying MISS path: line 83
line2 has MISS: False | line2 has CANON: True
```

**(3) The directory was minted by an `mkdir -p` inside the model's own bash
command, whose variable was hand-typed.**

```
$ python3 <tool_use bash/job commands containing mkdir+MISS>
### line 74 tool bash — the command that MINTS the container:
SESS=/home/user/.gopi/sessions/01a1452c-928d-7e0f9-9ef3-52c1482cfbc3; mkdir -p "$SESS/evidence/prefix-tree" …
total tool_use bash commands containing mkdir+MISS: 10
```

`SESS=` is a literal in the command string — not an expansion of the env var.
Ten such commands, all spelling the container by hand.

**(4) The only tool_result anywhere that prints the bad form is the echo of a
command that hardcoded it.** In that command the env's value and the hardcoded
literal are printed SIDE BY SIDE and are DIFFERENT:

```
$ python3 <tool_result literals>
line  751  [MISS37-drop] R=/home/user/.gopi/sessions/01a1452c-928d-7e0f9-9ef3-52c1482cfbc3/task-report.md
line  756  [CANON      ] HARNESS=/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md
```

The same run's own narrative reaches the same conclusion after re-reading
(`output.log`), by accident of the copy it had made:

> `Actually hold on, that's odd: after my copy, $R exists. … $GOPI_TASK_REPORT_PATH=/home/user/.gopi/sessions/01a1452c-928d-7e0f-9ef3-52c1482cfbc3/task-report.md. Same spelling in my comma…`

### False leads, closed (Rule 41: inference never stood in for a judgment)

- **"The framework resolved the id into that path."** FALSIFIED by (1): every
  env-derived literal is canonical. (Inference-only version of the same idea:
  "the row's ground path is derivable from the reported artifactIdentity
  exePath" — the field's value in the transcript names the CANONICAL id and the
  MAIN repo's binary, `/home/user/work/GoPi/bin/gopi`, not a task ground; it
  does not carry the bad spelling and was therefore never evidence for this.)
- **"Another run / a human typed it."** FALSIFIED by (2)+(3): the string is in
  THIS run's own `tool_use` inputs with no earlier occurrence in any
  environment-produced channel.
- **A second container, same class?** Checked and it is a DIFFERENT class — see
  below. Do not merge the two counts.

## The class is two shapes, both model-typed, and the census must not merge them

```
$ ls -d /home/user/.gopi/sessions/01a14*  |  printf '%s\n' | wc -c  # name length
$ stat -c 'birth=%w %n' <each>
01a1452c-928d-7e0f-9ef3-52c1482cfbc3   len=37  birth=2026-10-16 22:45:11  has metadata.json=YES  <- gopi's own container
01a1452c-928d-7e0f9-9ef3-52c1482cfbc3  len=37  birth=2026-10-16 23:49:57  has metadata.json=NO   <- hand-mkdir'd
01a14212-e0d5-7aa5-8db3-0eca2b4e0969   len=36  birth=2026-10-16 08:18:15  has metadata.json=YES
01a14212e-e0d5-7aa5-8db3-0eca2b4e0969  len=37  birth=2026-10-16 09:30:50  has metadata.json=NO   <- hand-mkdir'd
$ gopi task show 01a1452c-928d-7e0f-9ef3-52c1482cfbc3  -> resolves the ROW (canonical)
$ gopi task show 01a1452c-928d-7e0f9-9ef3-52c1482cfbc3 -> no task with task_id
```

The 37-char siblings carry `task-report.md` / `evidence/` / `artifacts/` and NO
`metadata.json`: gopi's own container writes `metadata.json` at session START,
so a container without one is a container gopi never minted (the same
discriminator as memory `#10865`, reused here — and that memory's own incident
was a *different* shape: a 35-char last-dash-dropped spelling, not extra hex).
Their birth time is LATER than the canonical twin's and they hold hand-copied
report/evidence — the model, made unsure by its own typo two hours earlier,
believing the harness path "vanished", copied them back under the misspelling.

The 36-char twin `01a14212-e0d5-…` is the SAME class as the 37-char one and was
NOT in the briefing's count: its first appearance in that run's log is
`(22, 'tool_use', 'task')` — a model-authored `task` call argument, with line 1
canonical. So both instances are the run's transcription; only one of the two
is 37 characters.

## The deliverable: the refusal now says WHICH CHANNEL handed it the string

`internal/sessionid` is a LEAF and reads no environment (it refuses a shape, it
need not know a provenance). The caller-side clause therefore lives beside the
callers, in `internal/tools/sessionid_refusal.go`, and both seams wrap the
leaf's refusal with it:

- `internal/tools/task_session.go` (`taskSessionDir` — the id→path gate);
- `internal/tools/tasks.go` (`taskFindSessionJSONLByTask` — the read seam).

```
sessionidRefusal(id) = fmt.Errorf("%w (operand source: %s)", sessionid.Refuse(id), sourceClause(id))
```

Three honest verdicts, no fourth:

| this process was handed | clause says |
|---|---|
| nothing (`GOPI_SIGNALREC_TASK_ID` unset: a bare chat, a hand-run verb, a test) | the channel is unset — no source is invented |
| the operand itself | the spelling came THROUGH the environment; the producer is upstream |
| a different ledger id | the operand DIFFERS from the handed id; look at this process's own argument/command text |

One definition stays in the leaf (`sessionid.Refuse`), one reader of the wire
contract (`internal/signalrec`'s `TaskIDEnv`, the env the launcher exports at
spawn in `cmd/agent/cli_tasks.go`), and the clause never claims a source it did
not measure (Rule 15).

## The measurement that also re-priced the fix

Driving the seam on this baseline:

```
$ go test ./internal/tools/ -run TestTaskSessionDirRefusalNamesTheOperandSource -count=1
    sessionid_refusal_test.go:81: taskSessionDir("01a1452c-928d-7e0f9-9ef3-52c1482cfbc3") = nil error,
        want the shape gate to refuse it
```

**On `cc846ee` the extra-digit spelling is still ACCEPTED by the id→path seam,
so this defect is still live on master** — a session directory is still mintable
under the misspelling, and a report written to `$GOPI_TASK_REPORT_PATH`-style
paths is still filed where the canonical reader will not look. The prior run's
refusal arm is real but unmerged; the merge is what closes the class, and the
attribution above says the source it protects against is the run's own text —
which is the common case, not a hypothetical one.
