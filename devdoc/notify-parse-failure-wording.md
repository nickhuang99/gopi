<!-- doc-class: normative -->
# A task event that names nothing is a PARSE FAILURE with its raw line — never `event=? task=? status=?`

Task `01a1a640-1c0e-707e-a4d2-73df25654559` (defect B). The console received two
notifications reading, in full:

```
task event: event=? task=? status=?
```

Three question marks naming no event, no task and no status. This page records what
produced them, why the obvious suspect (`{"event":"completed","task_id":"t-1",…}`)
is **not** the cause, and what the sink does instead.

## 1. The measurement, before anything was changed

The delivery chain is `scripts/notify/notify.sh` → `build_message` → `field` →
`json_string_body`. To see the text the sink would actually hand to a chat, a stub
`gopi` was put first on `PATH`, capturing the `--message` operand:

```bash
cat > bin/gopi <<'STUB'
#!/usr/bin/env bash
if [ "$1" = "notify" ] && [ "$2" = "--session" ]; then
  printf 'MESSAGE=<%s>\n' "$5"
fi
cat >/dev/null 2>&1
exit 0
STUB
```

Five payloads were pushed through the **baseline** sink (the copy that was deployed,
69784 bytes, byte-identical to `d4b06616:scripts/notify/notify.sh`):

| payload on stdin | delivered `--message` |
|---|---|
| `{"event":"completed","task_id":"t-1","status":"completed","summary":"local"}` | `task event: event=completed task=t-1 status=completed summary=local` |
| `{"event":"claimed","task_id":"01a1a686-…","status":"claimed","summary":"real"}` | `task event: event=claimed task=01a1a686-… status=claimed summary=real` |
| `{}` | `task event: event=? task=? status=?` |
| `not json at all` | `task event: event=? task=? status=?` |
| (zero bytes) / (a lone newline) | `task event: event=? task=? status=?` |

**So the `t-1` line was never rendered badly — it renders correctly, and always did.**
The `?` is the product of a payload from which `field()` can extract **nothing at
all**, not the product of a payload it read wrongly. The briefing's guess ("the chain
renders an existing event/status/task_id as `?`") is refuted by the first row of that
table; what the row does have in common with the console's two blank notifications is
the **wording the old placeholder produced**, which is what a reader would have seen
whether the payload was `t-1` or empty.

Two independent readings agree that the empty-payload shape is a real producer
failure, not a hypothetical one:

```bash
$ grep -c '^$' ~/.gopi/notify/task-events.jsonl      # 3 blank rows
$ awk 'NF==0{print NR}' ~/.gopi/notify/task-events.jsonl
361
6043
6044
```

A blank line in an append-only anchor is the fingerprint of `printf '%s\n' ""` — a
producer that handed the sink no bytes. And the `?` inbox messages are older than the
`t-1` anchor rows: the earliest is `2026-10-11T22:21:06Z`, three weeks before the
`t-1` events, so they cannot be caused by those events.

## 2. What the sink does now

`build_message` treats a payload that fails to yield **`event`, `task_id` and
`status`** as a parse failure, and says so with the raw line attached:

```
task event: PARSE FAILURE: no readable event,task_id,status; raw line: {}
task event: PARSE FAILURE: no readable task_id,status; raw line: {"event":"claimed"}
task event: PARSE FAILURE: no readable event,task_id,status; raw line: (empty — the producer sent no bytes at all)
task event: PARSE FAILURE: no readable event,task_id,status; raw line: (blank — bytes arrived, but they carry no characters)
```

Three properties are deliberate (Rule 15 — a bad event must say so, never be dropped
and never be laundered into a placeholder that reads like data):

- **It names the fields it could not read**, so a half-named payload (`{"event":"claimed"}`)
  reports exactly `task_id,status` and does not accuse the field that was present.
- **It carries the raw line**, flattened to one line (`build_message`'s contract is a
  one-line message), because the bytes are the only diagnostic such an event has.
- **An empty payload is distinguished from a whitespace-only one**: `(empty — the
  producer sent no bytes at all)` vs `(blank — bytes arrived, but they carry no
  characters)` — a lone newline is the shape a torn heredoc leaves, and calling that
  "empty" would be a false statement about the producer.

Why any missing field is a failure rather than only "all three missing": the producer
(`internal/tools/tasknotify.go`'s `taskNotifyPayload`) writes `event`, `task_id` and
`status` **without `omitempty`**, so a payload missing one of them was not produced by
that path at all — it is a hand-typed line, a truncated write, or a non-JSON body. A
message reading `task event: event=claimed task=? status=?` is the same defect in a
smaller costume.

## 3. The anchor stops accumulating blank rows

`printf '%s\n' "$payload" >>"$SINK_FILE"` appended a **blank line** for an empty
payload — indistinguishable from a torn write, and three of them are already live. The
sink now declines that write and **reports the refusal on stderr** (exit 0: the
delivery above already reported the parse failure, and the producer is the side to
fix):

```
notify.sh: ⚠ the payload on stdin was EMPTY — no bytes arrived, so nothing is appended to
/home/user/.gopi/notify/task-events.jsonl (the delivery above reported it as a parse
failure). This is the producer side to fix, not the anchor.
```

Nothing here deletes, rewrites or truncates: the anchor is append-only evidence, and
the three existing blank rows stay where they are.

## 4. Where the constants live

Rule 1/13/14: the wording is not spelled inline. Four `readonly` names sit with the
script's other message constants (`MESSAGE_EVENT_PREFIX`, `MESSAGE_PARSE_FAILURE`,
`MESSAGE_EMPTY_PAYLOAD`, `MESSAGE_WHITESPACE_PAYLOAD`), and the test reads the words
out of the sink rather than retyping them:

```bash
readonly MESSAGE_PARSE_FAILURE="PARSE FAILURE: no readable "
readonly MESSAGE_EVENT_PREFIX="task event: "
readonly MESSAGE_EMPTY_PAYLOAD="(empty — the producer sent no bytes at all)"
readonly MESSAGE_WHITESPACE_PAYLOAD="(blank — bytes arrived, but they carry no characters)"
```

No new dependency, no new knob, no toggle (Rule 35): the behaviour replaces a branch,
it does not add a configuration key.

## 5. The regression test, and its red state

`scripts/notify/notify_parse_failure_test.sh` — six cases, 26 assertions, driving the
**real** sink through a stub `gopi`, with its own `GOPI_HOME` so the live anchor is
never touched:

```bash
cd scripts/notify
bash notify_parse_failure_test.sh                     # green: 26 ok / 0 FAIL
BASE_REV=d4b06616 bash notify_parse_failure_test.sh --red-proof   # 16 FAIL
BASE_REV=3259f17  bash notify_parse_failure_test.sh --red-proof   # 16 FAIL
```

`--red-proof` reads the baseline sink **out of the object store**
(`git show "$BASE_REV:scripts/notify/notify.sh"`) and runs the same cases against it,
so the red state is produced by the same assertions that pass — not by a second,
weaker check. Both the parent (`d4b06616`, the utf8-truncation fix) and the branch's
own cut point (`3259f17`) are red, which is what "the test observes the defect
wherever the defect lived" means.

## 6. The delivered state, measured

The deployed copy `~/.gopi/notify/notify.sh` is **not** a symlink into this clone, so a
commit alone changes nothing about what the console receives. It was 69784 bytes
(`== d4b06616`), i.e. **still the defect**; after `bash scripts/notify/deploy.sh` it is
73980 bytes and byte-identical to this branch's copy:

```bash
$ cmp -s scripts/notify/notify.sh ~/.gopi/notify/notify.sh && echo same
same
$ gopi config show | grep -i notify     # the cmd line, parsed by gopi itself
  notify                                {"cmd":"bash /home/user/.gopi/notify/notify.sh","events":"all","async":false}
```

The previous copy is kept at
`~/.gopi/sessions/01a1a640-1c0e-707e-a4d2-73df25654559/ev/deployed-before-fix-notify.sh`
for a rollback that is a `cp`, not a rebuild.

## 7. What this page does NOT fix

A payload with **no readable fields** still reaches the anchor as a raw row (that is
the evidence) and still produces one console notification (now a parse failure rather
than three question marks). The defect *upstream* — whatever hands the sink a blank
line, and whatever writes the `{"event":"completed","task_id":"t-1",…}` rows into the
live anchor — is a producer-side question this page does not answer; it now **names
itself** in the message, which is the precondition for answering it.
