<!-- doc-class: normative -->
# The task-sync OFF banner: "off" is two facts wearing one word

## The defect

`TaskSyncLine()` (`internal/config/task_sync_switch.go`) had an ON branch that
consulted the resolved source (`taskSyncSource()`) and an OFF branch that did
not: every OFF printed the same hard-coded
`off (default — automatic task-row exchange disabled; task_sync/GOPI_TASK_SYNC enables it)`.

So an operator who deliberately wrote `"task_sync": false` into settings.json
got a banner that read **identically** to a node whose settings file never
mentions the knob. The question the banner exists to answer — *did my
declaration land?* — was unanswerable, and the two facts have the same VALUE
(off) and different meanings:

| fact | meaning |
|---|---|
| the file carries `false` | my declaration was read and honoured |
| the file is silent | nobody declared anything; the built-in default is in force |

## The fix

One resolution, two answers (`resolveTaskSync() (enabled bool, source string)`),
so the value and its provenance cannot diverge: the banner reports the source
the switch actually resolved on, rather than re-deriving one of its own.

```
task-sync: off (default — automatic task-row exchange disabled; task_sync/GOPI_TASK_SYNC enables it)
task-sync: off (declared in task_sync: false — automatic task-row exchange disabled)
task-sync: off (GOPI_TASK_SYNC: false — automatic task-row exchange disabled)
task-sync: on (task_sync)
task-sync: on (GOPI_TASK_SYNC)
```

The absent case keeps its old wording verbatim — it is the case that needs the
instructions, and `TestTaskSyncLineNamesTheEnablingKnobs` guards it.

## Why a raw-key presence probe, and why it is not a second reader (Rules 2/13)

`Config.TaskSync` is `bool json:"task_sync,omitempty"` (`internal/config/config.go`).
A struct decode maps **absent and false to the same zero value** — the typed
layer physically cannot answer the question the epic asks. The probe therefore
reads the raw key set (`map[string]json.RawMessage`, the same shape
`cmd/agent/cli_config.go:configSettingsRows` already uses to report "what does
the FILE say") and asks only `_, declared := keys[TaskSyncSettingsKey]`.

The Rule 2 question — "is this a second reader with its own precedence?" — is
answered by *when* it runs:

```
if cfg.TaskSync { return true, TaskSyncSettingsKey }   // value decided here
if taskSyncDeclaredInSettingsFile() { … }              // reporting only, AFTER
```

The probe runs strictly on the `false` tail, after the value is already
decided. It can change the WORDING and nothing else — a wrong probe cannot
invert the switch. Its failure modes (unreadable/absent/unparseable file) all
return `false`, i.e. "cannot tell is not a declaration", and the read error was
already announced on stderr by the resolution above (Rule 15).

## The choices that could have gone the other way

- **`taskSyncDeclaredHere = "declared in " + TaskSyncSettingsKey`** rather than
  a bare re-print of the key. The key name alone appears on every OFF line
  already (as "you could set this"), so spelling it again would have re-created
  the very ambiguity being fixed.
- **No fourth "unknown/unreadable" wording.** An unreadable settings file
  reports `taskSyncNotDeclared` and the banner says `default`. The stderr line
  already carries the path and the reason, and the epic asked for three cases.
- **The env channel owns the OFF label too** (CASE 5 in the report:
  `GOPI_TASK_SYNC=0` over a file declaring `true` prints `off (GOPI_TASK_SYNC: false …)`,
  not a settings credit). Crediting a declaration the env var just overrode
  would have been the same class of bug in the other direction.

## The test that would have caught it

`TestTaskSyncLineDistinguishesTheThreeOffSources` is written as a
**duplicate-line check**, not three independent substring assertions: after
collecting all three banners it fails if any two are equal. The original defect
was precisely two facts sharing one string, and three substring assertions on
three different strings would not have noticed a collapse back to the common
wording — only the pairwise comparison does.

The three SOURCES are compared; a fourth row (`env=0` over a file declaring
`true`) is deliberately excluded from the dup check, because it is the same
channel and the same value as the env-supplied row, and two env-decided OFFs
sharing one line is correct.

## Blast radius

`TaskSyncEnabled()` behaviour is unchanged — `resolveTaskSync` returns identical
values on every input path. Only the printed text of the OFF banner moved, and
`CONFIG.md` was updated in the same commit (two `task_sync_switch.go:NNN` line
references re-pointed, plus both the catalog row and the banner paragraph
document the three-channel wording) so `internal/standards/config_doc_gate_test.go`
stays green.