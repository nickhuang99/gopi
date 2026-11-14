<!-- doc-class: historical -->
# The compare header's authority provenance (`session_file=` | `not-read`)

**Task** `01a0de1a-d241-7cbc-a9cc-a882f7635161` · branch
`task/01a0de1ad2417cbca9cca882f7635161`.

## The defect

`gopi ui_state --compare`'s TEXT header printed the authority as the caller typed
it, and the count as `transcript_lines=0`:

```
#gopi-ui-state-compare surface=chat state=exited dump=… session=<the typed path> transcript_lines=0 match=false
```

Two entirely different facts produced that same line:

| fact | what the header printed |
|---|---|
| the transcript WAS read and holds zero assistant lines | `transcript_lines=0` |
| the transcript was NEVER read (the grid refused first) | `transcript_lines=0` |

The one field that told them apart — `session_file`, the path **actually
resolved and read**, `""` when nothing was — existed only in the `--json`
envelope. An agent or operator reading the text form (the channel a human reads)
could not tell "the log is empty" from "nobody looked at it", and the risk is
asymmetric: the never-read case invites the false conclusion *"the transcript is
empty, so there is nothing to find"* about a check that never ran.

Measured on the pre-change binary, same sandboxes, same command:

```
--- (1) READ AND EMPTY ---
session=…/demo/session.jsonl  transcript_lines=0 match=false      exit=3
--- (2) NEVER READ (grid refused first) ---
session=                                      transcript_lines=0 match=false      exit=1
```

Both differing ONLY in `session=`, and only because case (2)'s refusal happens
before `rec.Session` is reassigned — a difference of accident, not of design: the
instant the authority is derived from the dump or merely typed, the two lines are
byte-identical apart from nothing at all.

## The change

`uiStateSessionProvenance(sessionFile)` renders `session_file=<path>` — or
`session_file=not-read` when nothing was read — and both TEXT headers print it
beside `session=`:

```
# … session=<the typed/derived path> session_file=<the path read> transcript_lines=N match=…
# … session=<the typed/derived path> session_file=not-read    transcript_lines=0 match=false
```

Design points:

- **`session_file` is the JSON envelope's own field name** (`rule 1b`/14: the name
  lives in `uiStateFieldSessionFile`, the marker in `uiStateSessionNotRead`), so
  the text form and the machine form can be read as one fact and a script greps
  one word in both.
- **Both renderings, one helper.** The whole-answer audit (`--all` / `--last`)
  printed its header through the same `%s` slot for `session_file` that the compare
  did; it now prints `session=` + `session_file=` too. The divergence that produced
  this bug was one channel losing a field the other kept, so the provenance is
  computed in ONE function both call.
- **No behaviour change.** The comparison, the resolution order, the refusals and
  every exit code are untouched — the refuse-before-reading order is deliberately
  preserved, and it is now *visible* rather than merely true.

## Exit codes are unchanged (measured, both binaries, `/dev/null`-redirected so no SIGPIPE)

| case | pre | post |
|---|---|---|
| read-and-empty | 3 | 3 |
| never-read (refused grid) | 1 | 1 |
| `--session nosuch-id` | 1 | 1 |

## Regression guard

`TestUIStateCompare_TextHeaderSeparatesNeverReadFromReadAndEmpty`
(`cmd/agent/cli_uistate_compare_test.go`) — RED before the change with

```
the compare header carries no session_file field:
  #gopi-ui-state-compare … session= transcript_lines=0 match=false
```

and GREEN after. It pins three things: the never-read marker on a fallback-geometry
refusal, the resolved path for a read-and-empty authority (asserting the fixture
still yields `transcript_lines=0`, so the case cannot quietly stop exercising the
empty shape), and that the two headers are **not the same line** — the defect was
equality, not a wrong value. It also asserts the header agrees with the JSON
envelope's `session_file`. The vocabulary is spelled literally in the test so it
does not agree with the implementation by construction.

## Note on older devdocs

`devdoc/end-user-audit-protocol.md` (§CW-4) and the three other devdocs quoting
`#gopi-ui-state-compare …` lines are transcriptions of runs against the PRE-change
binary; their headers lack `session_file=`. They are historical evidence and were
left as written. The header shape after this change is the one the test above
pins.
