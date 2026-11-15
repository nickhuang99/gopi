<!-- doc-class: normative -->
# `gopi session`: one flag-order criterion for the whole verb family

**The rule this page states, in one sentence:** for every `gopi session` verb that
takes a positional operand (`<session-id>` for `show`/`log`/`stats`/`raw`/`replay`/
`notes`, the resume TARGET for `resume`), a flag may stand on **either side** of
that operand, and the operand is the same token in both spellings.

A verb that instead refuses one order is a defect of the same cost as a wrong
result, because the refusal it prints is usually the *wrong complaint*: it says
the operand is missing while the operand is on the command line.

## The measured defect (2026-10-27)

```
$ gopi session show  --json 098967ca-…   exit 0    ← worked
$ gopi session show  098967ca-… --json   exit 0
$ gopi session log   --json 098967ca-…   exit 1    ← "gopi session log: missing <session-id> argument"
$ gopi session log   098967ca-… --json   exit 0
$ gopi session stats --json 098967ca-…   exit 1    ← "pick one input: <session-id>, …"
$ gopi session stats 098967ca-… --json   exit 0
```

The split was not arbitrary: `show` / `replay` / `notes` parse their own argv and
classify any non-flag token as a positional, wherever it stands. `log` / `stats` /
`raw` share the hand-rolled `memoryFlagSet` scanner (`cli_memory.go`), whose
`parse()` has ONE default for an **undeclared** flag: a bare `--flag` takes the
**next token as its value**. That default is correct for `--grep foo` and wrong
for `--json`, which is a complete instruction — and the very next token was the
session id, so the flag claimed it, no positional survived, and the verb reported
the id absent.

**Where a flag's value ENDS is a property of the FLAG, not of the verb that
admits it.** It was undeclared, so the scanner guessed, and it guessed "value".

## The mechanism (`cmd/agent/cli_session_flag_arity.go`)

One `sessionFlagArityTable` per scanner-based verb — `map[string]bool` keyed by
flag name, `true` meaning "claims the following token":

```go
var sessionLogFlagArity = sessionFlagArityTable{
	"json": false, "response": false, "thinking": false, /* … valueless views */
	"grep": true, "filter": true, "last": true, "toolcall": true, /* … */
}
```

Three functions read it and nothing mirrors it:

* `logFlagArity` / `statsFlagArity` / `rawFlagArity` — `(takesValue, admitted)`,
  one answer from one table, so a flag **cannot** be admitted in an admission
  `switch` while its arity is unstated;
* `logFlagMember` / `statsFlagMember` / `rawFlagMember` — now thin readers of the
  table, so admission and arity can never disagree (Rules 13/14);
* `sessionFlagDeclare(fs, args, arity)` — run **before** `fs.parse(args)`: it
  refuses an unknown flag **by name**, on the set actually typed, and declares
  every admitted valueless flag `fs.boolean(name)`.

The declaration MUST precede the scan: `memoryFlagSet.boolean` is consulted as the
token is consumed, so a valueless flag declared afterwards has already eaten its
neighbour — that is precisely how the session id disappeared into `--json`.

Refusing **before** the scan is also a correctness fix on its own: the post-scan
loop it replaced walked the flags the *scanner had recorded*, which is a different
set from the flags that were *typed* whenever a value happened to look like a flag.

## The fourth verb: `resume`

`runResume` read `args[0]` as the resume target unconditionally, so

```
$ gopi session resume --task t-123 <id>
gopi session resume: resume target "--task" is neither a raw log path nor an existing session id
```

— the operand was on the command line and the verb named the flag as it.
`resumeTargetAndTail(args)` now splits the argv with the same criterion:
`--task` (and its value, and any other `-flag`) belongs to the tail
`parseResumeArgs` owns, and the **first non-flag token** is the target. A `-flag`
is never a candidate for the target; an unknown flag is refused **by name**
(`unknown option "--bogus"`) in preference to the vaguer "no target" message, and
a line with no target at all is refused with `resumeNoTargetMessage` — ONE wording
for the empty line and the flags-only line, since two spellings of one refusal let
a reader think they faced two different problems.

## What this page does NOT change

* **Value-taking flags keep their value.** `--grep foo`, `--last 3`, `--filter
  read`, `--http 2`, `--file PATH`, `--branch main`, `--task <id>`.
* **A bare trailing `--last` / `--http`** still means "1" / "all": the scanner
  leaves the value empty when the next token is another flag or there is none.
* **`--json` on `session raw`** is still refused **by name** (raw prints verbatim
  bytes) — `rejectedRawFlags`, unchanged, and it fires before the scan.
* **Moved flags** `log --raw` / `log --http` stay *admitted* and are refused at run
  time with the pointer to their replacement (`logMovedPointer`); dropping them
  from the table would have turned a documented refusal into "unknown flag".

## The check

`cmd/agent/cli_session_flag_order_test.go` — three arms, because a parse-level
green can still be unreachable (PROJECT.md Rule 31):

1. `TestSessionFlagOrderIsTheSameOnBothSides` — the **real binary**
   (`gopi session <verb> <tail>`) over every verb+flag pair in BOTH orders: same
   exit code, byte-identical stdout, and an explicit failure if a verb claims an
   input is missing while the id is on the command line. The case table has a
   non-vacuity floor, so a broken table cannot pass as a clean run.
2. `TestSessionParsersAgreeOnFlagOrder` — `parseLogArgs` / `parseStatsArgs` /
   `parseRawArgs` at argv level, both orders, one token apart.
3. `TestSessionResumeTargetIsTheSameInBothOrders` — `resumeTargetAndTail` plus the
   real binary: both orders must **name the same operand** (that verb refuses a
   nonexistent one, so "both exit 0" would be the wrong assertion).

Red proof: with the three `sessionFlagDeclare` calls reverted to the pre-fix
post-scan loop, 11 subtests fail — every scanner-family case (log/stats/raw,
both flags each) plus the argv-level arm — while `show` / `replay` stay green,
which is what distinguishes "the family criterion regressed" from "the test is
broken".

The G4 help-surface gate (`cli_session_help_surface_basic_test.go`,
`scripts/g4-session-help-surface.sh`, `devdoc/gopi-session-help-surface-inventory.md`)
is kept in step: its `resume --bogus` row pinned the OLD sentence ("is neither a
raw log path nor an existing session id"), which the fix makes false — a flag-only
line now refuses by name — so the row asserts `unknown option "--bogus"` and a new
row asserts the empty-line refusal.
