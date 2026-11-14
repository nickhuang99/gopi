<!-- doc-class: historical -->
# The chat screen dump is ON by default (opt-out) — the decision, the off switch, and the cost basis

Task: `01a0f720-99b5-75e4-b3ff-b4349249c2d4` (epic ③/3 of the self-observation
default family). Branch: `task/01a0f72099b575e4b3ffb4349249c2d4`.
Date: 2026-10-01. Supersedes the "DECISION OPEN / behaviour UNCHANGED (opt-in)"
state recorded in `devdoc/ui-state-dump-default-decision.md` **for the chat
surface**.

---

## 0. What changed, in one paragraph

`gopi chat` wrote its own interpreted screen state (`chat-screen.dump` + its
`.json` twin) only when asked — `--dump-screen[=PATH]` or `GOPI_CHAT_DUMP`. It
now writes it **unless declined**. The decline is spelled twice and both are
documented in `gopi chat -h`: the flag `--no-dump-screen` (takes no value) and a
falsey `GOPI_CHAT_DUMP` (`0`/`false`/`off`/`no`/`disabled`). The flag wins over
the environment. A declined run is **byte-identical** to a run from before the
capability existed: `chatDumpWriter` returns the caller's writer *unchanged* when
there is no sink, so the bytes reaching the terminal are the same object, not a
wrapper.

**Scope.** The `gopi ui` (BubbleTea) surface is NOT in this change, and neither
is the tool-wire vocabulary: `internal/uiclient`'s `GOPI_UI_DUMP` and
`gopi ui --dump-screen` keep their opt-in default. The epic named the chat
surface; flipping both in one change would have made the measured evidence
ambiguous about which surface it described.

## 1. Why default-on — the measured basis

The question was already recorded, with its evidence, in
`devdoc/ui-state-dump-default-decision.md` (task `01a0e070`):

> on this host `~/.gopi/sessions` holds 191 chat dumps and 0 ui dumps — i.e. the
> flag is effectively never armed, so the audit path is blind unless someone
> armed it in advance.

An observation capability that is off until someone remembers a flag is a
capability nobody has. The specific casualty was the one screen an agent most
needs to read — its own answer band — and the whole `ui_state --compare` /
`chat_surface` loop that exists to read it.

**The cost, measured** (so the default is an honest one, and the numbers live in
the help text too):

| quantity | measured value | where it comes from |
|---|---|---|
| one artifact write | 5793 bytes | 117x26 frame, sink re-interpretation |
| write cadence | at most once per `dumpMinInterval` = 100 ms | `internal/uiclient/dump.go` |
| frames vs file writes | 18408 frames / 2718 dumps (~1 write per 7 frames) | long live run, epic 01a0f66a |
| idle behaviour | freezes at `dumps=1`, then writes nothing | the layout paints only on CHANGE |
| dependency cost | **zero** — one file inside the run's OWN session directory (Rules 35, 22) | `chatResolveDumpPath` |

So the default is a **throttled sample**, not a firehose: ~1/7 of frames reach
the disk, and an idle chat stops writing entirely. The privacy cost is real and
is the operator's to weigh: the artifact holds the cells the operator's own
terminal showed. That is exactly why the switch OFF exists as a documented flag
rather than as an environment variable only.

## 2. The shape of the change

- **`cmd/agent/chat_dump.go`** — `chatDumpOptions` gained `disabled` and `stated`
  (and the `dumped()` predicate, the ONE place "off" is defined);
  `chatDumpDefault()` is the default decision; `chatDumpFromEnv()` maps unset →
  default, falsey → off, truthy → arm, anything else → path;
  `parseChatDumpArg` gained the `--no-dump-screen` arm (a value beside it is
  refused BY NAME); `chatResolveDumpPath` keys on `dumped()`.
- **`internal/cliverbs`** — `NoDumpScreen` (`--no-dump-screen`) and `ChatDumpEnv`
  (`GOPI_CHAT_DUMP`) are declared once, as leaf constants. `cmd/agent`,
  `internal/agent` and `internal/uiclient` all reference them (Rule 14) — no
  second literal of either spelling exists.
- **Text** — the `--dump-screen` help block (now "ON BY DEFAULT", with the cost,
  the reason and the off switch), `chatSurfaceArmedNotice` /
  `chatSurfaceUnarmedNotice` (`cmd/agent/chat_surface_surface.go`),
  `chatSurfaceNotArmedNotice` and the tool definition
  (`internal/agent/chat_surface.go`), and `uiclient.DumpNotArmedNotice` (which
  now says the CHAT dump is defaulted and names its decline, since its old text
  instructed a reader to arm a surface that is already armed).

## 3. Two traps this change had to walk around (both measured the hard way)

**(a) The default must not masquerade as a command-line statement.** The resume
chain adopts a recorded dump destination only when this run stated no dump
choice — otherwise every resumed session would silently lose the destination the
crashed run had recorded. With the dump armed by default, "requested" is TRUE on
every run, so the old `dumpStated = dumpOpts.requested` would have made every
resumed session supersede its own record. The new `stated` field is what the
chain keys on:

```go
chatApplyResumeFlags(..., dumpOpts, flags, dumpOpts.stated)   // NOT .requested
```

**(b) The help-text gate sliced the block at the first following flag.** The dump
now has two flag lines in one section; the gate's extractor
(`cli_chat_dump_screen_self_serving_help_test.go:chatDumpHelpBlock`) must step
over `--no-dump-screen` and end at the first flag that is NOT part of the family,
or every claim about "the page that creates the artifact" is made about a page
fragment.

## 4. What was NOT done (honest)

- `gopi ui`'s dump stays opt-in (see §0 — scope).
- No new knob, process, file type or sweep (Rule 35). The artifact lives in the
  run's own session directory, which the existing `gopi scratch-gc` tick already
  collects; nothing new needed to own its lifecycle.

## 5. Verification

See `task-report.md` in the run's session directory for the Rule-5 record
(build, tests, the pty evidence, the `ui_state --target chat` read-back, and the
declined run's absent artifact).
