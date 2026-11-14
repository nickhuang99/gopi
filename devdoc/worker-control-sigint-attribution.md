<!-- doc-class: normative -->
# Where a `gopi worker --stop` process gets its process-wide SIGINT handler (attribution correction, task 01a1431c-5846-713c-83fe-9c9fecd45766)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** correction to `devdoc/worker-control-ctrl-c.md` and to task
`01a14316-b7cb-71b6-9426-48f788a85a9c`'s mechanism section. **Measured:**
2026-10-16, host hosta. No production behaviour is changed by this
document — the fix it corrects (`d1f88b5e5`) is on master and stays as it is.

## What is being corrected

Task `01a14316-b7cb` recorded the mechanism as:

> **`signal.Notify` 在 `cmd/agent/main.go`，是全进程级的**

The row's own citation was a bare line number (`main.go:895`) rather than the
anchor spelling; that citation is the half of the premise this document
corrects, and the positions the source carries now are re-derived further down.

and the correction row `01a1431c-5846` began from the same premise (it kept the
line number and only doubted the consequence). **Both are wrong about the line,
and the wrong line is impossible on this path** — which is worth writing down,
because a wrong attribution is what a later reader will reach for when they try
to change the behaviour.

## The impossibility, from the source (measured by reading, and pinned by a test)

`cmd/agent/main.go` dispatches its verbs at ONE place. The block below is
identified **by symbol, not by line**: `main.go`'s line numbers move with every
unrelated edit above `main()`, and four separate re-derivations of this document
have each chased them. To read the block at the source you are holding, run

```bash
grep -n 'signalrec.Install("")' cmd/agent/main.go
grep -n 'if isConfigSubcommand(dispatchArgs)' cmd/agent/main.go
grep -n 'signal.Notify(sigCh' cmd/agent/main.go
```

and the four anchors are, in order: the recorder's call, the chain's head (whose
line carries `isWorkerSubcommand(dispatchArgs)`), the bare `return` on the chain's
very next line, and the bare-prompt registration the chain must come before.

```go
    uninstallSignalRecord := signalrec.Install("")
    ...
    if isConfigSubcommand(dispatchArgs) || … || isWorkerSubcommand(dispatchArgs) || … {
        return
    }
    ...
    signal.Notify(sigCh, signalrec.GracefulStopSignals()...)   // the bare-prompt branch
```

> **THE LINE NUMBERS WERE RE-DERIVED (2026-10-16).** Every `main.go` position in
> this document was measured by reading on the branch that first wrote it, where
> the recorder's call sat on `613` and the chain on `631`. A later, unrelated
> commit inserted a tmp-dir bootstrap above both, and the source then read `635`
> / `653` / `654` / `917` — the same four anchors, 22 lines lower, with every
> DELTA between them unchanged (`main.go`'s own line count moved; the order the
> document is about did not).

> **RE-DERIVED AGAIN (2026-10-16, task `01a159ad-c07f`).** That re-derivation had
> itself gone stale: commit `732a4f3` (the `dev_repo`-empty deprecation notice)
> inserted six lines in `main()` ABOVE the recorder, and the source THEN read
> **`641` / `659` / `660` / `899`** — the positions as that commit left them: the same four anchors, six lines
> lower, again with every DELTA unchanged (18 recorder→chain, 239
> return→notify). Measured at this branch's tip (`a793f51`), where `732a4f3`
> is an ancestor. **Anchors are resolved here BY SYMBOL, never by a remembered
> number:** `signalrec.Install("")`, `if isConfigSubcommand(dispatchArgs)`, the
> `return` on the chain's next line, and `signal.Notify(sigCh,
> signalrec.GracefulStopSignals()...)` (`signal.Notify(` occurs exactly ONCE in
> `main.go`, so the bare-prompt registration cannot be confused with the control
> installer's).
> The control installer has its own four anchors, equally symbol-resolved and all
> in `cli_worker_control.go`: the `installWorkerControlSignals()` call inside
> `workerControlCommand`, that function's declaration, the
> `signal.Notify(ch, workerControlSignals...)` inside it, and the
> `signalrec.ClaimSignalHandlers()` beside it. (Their line numbers are
> deliberately NOT printed here — see the 2026-10-27 note below. The
> `cli_worker_control.go` line this document's history quoted for the control
> installer — a number under an older file layout (`d1f88b5e5`, where the
> declaration sat at `:1125` and its `signal.Notify(ch, …)` at `:1127`) — is
> NARRATIVE, read in a checkout this ground does not carry: `git cat-file -t
> d1f88b5e5` here answers `fatal: Not a valid object name`, so it is recorded,
> not re-measured.)
> The guard `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas` pins the
> symbols and checks any number that survives (see the 2026-10-27 note below).

> **RE-DERIVED AGAIN (2026-10-22, task `01a160a2-2cda-7c1e-8fa4-b925ba69bc1d`).** The
> worker-face block in `main()` was rewritten: the `compute_nodes` "NO CONSUMER"
> report was deleted WITH its knob, which took 17 lines out ABOVE the recorder and
> left one notice behind. The source now reads **`634` / `652` / `653` / `892`** —
> the same four anchors, seven lines higher, with every DELTA unchanged (18
> recorder→chain; **the `239 return→notify` figure in this note is SUPERSEDED**
> — see the 2026-10-26 re-derivation below, which measures it at 250). A
> line-number claim that is the SUBJECT of its own sentence is exactly what
> `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas` reads, so this
> re-derivation is not bookkeeping: that gate caught this drift on the first test
> run after the edit.

> **RE-DERIVED AGAIN (2026-10-26, task `01a17848-35ef`).** Three commits moved the
> anchors AGAIN and the numbers in the block above had gone stale: `6bf23e2` (the
> run-repository chain + withdrawal of the empty-`dev_repo` deprecation) put the
> recorder on `657` and the chain on `675`; `af34268` (signalrec: every
> handler-owning mode covers the recorder's terminating set) rewrote the
> bare-prompt registration to `signalrec.GracefulStopSignals()` and moved it to
> `924`; `82610a4` (session index rows report the operator-row turn count) then
> moved all four to **`667` / `685` / `686` / `936`** (re-measured 2026-10-27
> against `82610a4`'s own tree: the first draft of this note read `666` / `684` /
> `685` / `935`, one line too high on every anchor — the comment block ending on
> that tree's line 666 was read as the call below it, and `666` is not a line the
> recorder has occupied in ANY revision of `main.go` in this repository). Every DELTA the document
> argues from is unchanged (18 recorder→chain); the return→notify distance is
> **250**, not the 239 this document states, and that 239 was already stale before
> these three commits — the bare-prompt registration has been further from the
> chain since `af34268` rewrote it. Measured at this branch's tip.
>
> **All four dated notes above are HISTORICAL (read at the commit they name), and
> their absolute numbers are stale on today's tip.** They are kept because each
> records WHICH commit moved the anchors and by how much, which is the
> information a reader needs to know that the positions are not stable; they are
> NOT current claims, and the deltas they carry (18 recorder→chain, 287
> return→notify) still hold. The current positions are whatever the `grep -n`
> commands above print in the checkout you are holding, and this document
> deliberately does not restate them.

> **STOPPED RE-DERIVING NUMBERS (2026-10-27, task `01a17b52-dc62`).** This is the
> THIRD drift in eleven days: `657` → `667` → `673` for the recorder, and each of
> the two earlier repairs was "retype the number in the prose". That repair is
> itself the defect: it makes the document a mirror of a line count, so every
> unrelated insertion above `main()` turns the guard red and buys a commit that
> changes no claim. The doc now carries **no current absolute line number**: the
> four anchors are spelled, and the reader resolves them in their own checkout
> with the three `grep -n` commands above. The two DELTAS stay, because a
> distance between two anchors survives an insertion above both of them — and a
> distance is what the argument actually needs: "the recorder is armed 18 lines
> BEFORE the chain that returns" and "the bare-prompt registration is 287 lines
> BELOW that return" are the two facts the unreachability conclusion rests on.
>
> The guard was rebuilt at the same time and is what keeps this from rotting:
> `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas` now asserts (a) every one
> of the three attribution documents still SPELLS `signalrec.Install` and
> `signal.Notify(sigCh` — the durable half, invisible to any number check — with
> this document additionally required to keep the resolve commands and the chain's
> `isWorkerSubcommand(dispatchArgs)`; (b) any `main.go:N` a document still prints
> is checked against the source exactly as before, so the old habit cannot come
> back unnoticed; (c) both deltas, including the "287 lines earlier" spelling the
> first draft of the rebuild missed — found by the rebuild's own red proof.
> **A document that prints no line number is GREEN, and that is the intended
> outcome**: the failure this arm exists to prevent is a reader being handed a
> number that moved, not the absence of a number.
>
> `devdoc/signalrec-claim-vs-reraise.md` was added to the guard's document set at
> the same time: it carried `cmd/agent/main.go:447` and `main.go:666` with NO
> guard pointing at it, so it drifted silently through all three repairs above.

`gopi worker --stop` matches `isWorkerSubcommand` in the chain's head line, so the
process returns from `main()` on that head's **next** line. The bare-prompt
registration — `signal.Notify(sigCh, signalrec.GracefulStopSignals()...)`, the
only `signal.Notify(sigCh` in `main.go` — sits **287 lines below** that return and
on a different verb's path: **on `worker --stop` it is unreachable, and nothing
registered down there can be its mechanism.** (Both positions are resolved by
their spellings above, not by a remembered number; the 287 is a DELTA, which has
survived every insertion above the anchors — and it is the ONE insertion surface it
does not survive: code added BETWEEN the two anchors moves it, which is what the
2026-11-05 note below records.)

> **RE-MEASURED 250 → 279 (2026-11-05, row `01a1ac14`).** The bare-prompt
> screening guard (`config.GuardBarePrompt`, main.go's §1c) was inserted BETWEEN
> these two anchors on purpose — after both positional refusals, before
> `readInput`/`newAgent` — so the return→notify distance grew by the 29 lines of
> that block (chain `return` 705 → `signal.Notify(sigCh` 984). The RECORDER→chain
> delta is untouched at 18, because nothing was inserted above the recorder.
> **This is the trade the paragraph above states**: the 250 is not a constant of
> the argument, it is a distance between two anchors, and a change that lands
> between them must re-state it. Both spellings (this document's, and
> `worker-control-ctrl-c.md`'s) were updated in the same commit as the
> insertion, which is what
> `TestSigintDocsCiteTheLineNumbersTheSourceActuallyHas` requires — it reads the
> source, not the prose, and it is what failed first.

> **RE-MEASURED 279 → 287 (2026-11-13, row `01a1ce3e-240a-712a-a30b-361f8398cfdb`).**
> The run-start configuration dump (`writeDebugConfigDumpToStderr`, from the run
> path right after `printLogPaths`) was inserted BETWEEN the same two anchors —
> after the chain's `return`, before `signal.Notify(sigCh` — so the
> return→notify distance grew by the 8 lines of that block (chain `return` 700 →
> `signal.Notify(sigCh` 987). The RECORDER→chain delta is untouched at 18, because
> nothing was inserted above the recorder. Both spellings (this document's
> "287 lines below", `worker-control-ctrl-c.md`'s "287 lines earlier") were
> updated in the same commit as the insertion.
(`main.go`'s reachability story is unchanged between the pre-fix baseline
`01c1674` and this branch: `d1f88b5e5` touched only `cli_worker_control.go`, its
e2e test, `DOC-INDEX.tsv` and the devdoc, so the numbers differ there — `613` /
`631` / `632` / `895` — while the ORDER and every delta between those four
anchors are identical.)

## And then: WHY the process-wide SIGINT interception on that path is

`signalrec.Install("")` runs at the very top of `main()`, eighteen lines BEFORE the
chain that returns for `worker`.** Its `watchedSignals` are

```go
var watchedSignals = []os.Signal{
    syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP,
    syscall.SIGUSR1, syscall.SIGUSR2,
}
```

and its header states the discriminator that it THOUGHT made it identifiable at
runtime: **`SIGQUIT IS DELIBERATELY ABSENT`** from that list (registering it
would disable Go's goroutine stack dump). **That discriminator does not survive
measurement, and the epic that asked for it was wrong about the reading**: on
this host the SIGQUIT bit is set in the `SigCgt` of a two-line Go program that
calls no `signal.Notify` at all (`fffffffd7fc1fefd`), because the Go runtime
registers its own SIGQUIT handler for the stack dump. So bit 2 is set with or
without the recorder, and a mask cannot name the recorder's WATCHED list.

**The honest discriminator is therefore the source order plus the syscall log,
not the mask**: the registration lines, in order, are SIGTERM, SIGINT, SIGHUP,
SIGUSR1, SIGUSR2 with no SIGQUIT among them, they appear on this path in
`strace`, and the source pins that `signalrec.Install` runs eighteen lines before
the chain that returns for `worker`.

**The conclusion this supports**: on the control path the process-wide SIGINT
takeover comes from `signalrec.Install("")` at the top of `main()`, and the
bare-prompt registration the original row named cannot contribute at all — it is
unreachable on this verb.

### What the mask DOES settle, and what it never can

Three readings were taken on this host, and the discriminator all three carry is
**`SigCgt` bit 1 (SIGINT)** — the one bit that separates "some Go code called
`signal.Notify` for SIGINT" from "nothing did":

| process | `SigCgt` | bit1 SIGINT | bit2 SIGQUIT |
|---|---|---|---|
| trivial Go program, no `signal.Notify` anywhere | `fffffffd7fc1fefd` | **0** | 1 |
| the `gopi` hosting this agent run (pid 2518625) | `fffffffd7fc1eeff` | **1** | 1 |
| the operator's process on hp, task 01a1431c's reading | `fffffffd7fc1feff` | **1** | 1 |

Decoded with `python3`: bit *n* is `(int(mask,16) >> n) & 1`, and the classic
signal order is bit 0 `HUP`, bit 1 `INT`, bit 2 `QUIT`, bit 12 `PIPE`.

**A `SigCgt` bit-1 reading is set by ANY `signal.Notify` over SIGINT** — the
recorder (`signalrec.Install`, top of `main()`), the control installer
(`installWorkerControlSignals` in `cli_worker_control.go`), or the bare-prompt
branch (`signal.Notify(sigCh, signalrec.GracefulStopSignals()...)`). It therefore **cannot** distinguish the
before-fix binary from the after-fix one (both register, §1), **cannot** name
which `Notify` did it, and **cannot** say whether the handler acts. It is
evidence that a handler EXISTS, which is all the correction row needed — and
nothing more.

## The guard, and what it does and does not prove

`cmd/agent/worker_control_signal_origin_test.go` (this branch) pins both halves
by SOURCE POSITION:

- arm 1 — `signalrec.Install(` must appear **before** the subcommand chain's
  `return`, and the chain must still route `isWorkerSubcommand`;
- arm 2 — `workerControlCommand` must call `installWorkerControlSignals()`, and
  that function must contain both `signal.Notify(` and
  `signalrec.ClaimSignalHandlers()`.

That is a statement about the ORDER of statements, which is exactly the property
a runtime observation cannot carry: both statements are true at runtime and
neither is visible in the signal mask.

## Evidence (self-filed) — the commands and their raw output

Each reading below was taken on this host, on this branch. The verbatim
transcripts, scripts and binaries are in this run's session directory,
`/home/user/.gopi/sessions/01a1431c-5846-713c-83fe-9c9fecd45766/artifacts/`
(`evidence/`, `bin/gopi-fixed` = this branch, `bin/gopi-prefix` = the pre-fix
baseline `01c1674` built from `git archive`).

### 1. The registration is the recorder's, and it exists on both sides of the fix

```console
$ strace -f -e trace=signal -o prefix-sigint.txt \
      .../bin/gopi-prefix worker --stop --pid <live victim> --wait 25m </dev/null
$ grep SIGINT prefix-sigint.txt
2944867 rt_sigaction(SIGINT, NULL, {sa_handler=SIG_DFL, sa_mask=[], sa_flags=0}, 8) = 0
2944867 rt_sigaction(SIGINT, {sa_handler=0x479460, sa_mask=~[], sa_flags=SA_RESTORER|SA_ONSTACK|SA_RESTART|SA_SIGINFO, sa_restorer=0x4795a0}, NULL, 8) = 0
```

Two calls, both the runtime's, **on the pre-fix binary**. There is no third:
`01c1674`'s control code installs nothing, and the process never reaches the
bare-prompt branch's `signal.Notify(sigCh, signalrec.GracefulStopSignals()...)`.

> **This is also the acceptance criterion the correction row asked for and could
> not have: `SigCgt` cannot discriminate before from after the fix.** The pre-fix
> binary registers SIGINT on this path too, at the same eighteen-line recorder→
> chain delta it has now (that tree numbered the two anchors 613 and 631 — the
> tmp-dir bootstrap above them had not been inserted yet; verified: `git show
> 01c1674:cmd/agent/main.go | grep -n
> 'signalrec.Install("")'` → `613`), so a `SigCgt` bit-1 reading is set on both
> sides and measures the recorder, never the fix. The fix is discriminated by
> behaviour (exit 3 + abort line, below), not by any mask.

### 2. The fix's own registration, and the shape of it

```console
$ strace -f -tt -e trace=signal -o sig.txt .../bin/gopi-fixed worker --stop --pid <live victim> --wait 25m
$ grep SIGINT sig.txt
2930441 21:04:19.008412 rt_sigaction(SIGINT, {sa_handler=0x575ba659be40, sa_mask=[], …}, …) = 0
```

`sa_mask=[]` — no signals blocked — which is Go's `signal.Notify` shape for a
handler that must not mask a competing signal, and is NOT the shape at (1)
(`sa_mask=~[]`). The control installer is only reachable *after* the chain
(`workerControlCommand` calls `installWorkerControlSignals()`, whose declaration
is further down the same file), which is why it appears in the fixed binary and
not in the pre-fix one.

### 3. The behaviour, which is what the fix actually delivers, on a real pty

`evidence/ptyrun.py` starts the command on a pty (`openpty` + `setsid` +
`TIOCSCTTY` + `ISIG`), so `0x03` becomes SIGINT **through the tty discipline**,
and sends it once the stop request file exists. Same command, same bound, same
conditions, two binaries:

| binary | inherited SIGINT | exit | elapsed | abort line | `TERM_<pid>` after |
|---|---|---|---|---|---|
| `gopi-prefix` (`01c1674`) | `SIG_IGN` | **killed at the harness's ceiling** (`signaled, signal 9`, timed_out=true) | 9.63s | none | — |
| `gopi-fixed` (this branch) | `SIG_IGN` | **3** | 5.96s | `gopi worker: SIGINT — the stop request has been delivered; this command is no longer waiting for pid … (exit 3)` + `stop request file still standing` + `the request is NOT withdrawn` | standing |

Arm verbatim, `evidence/arm3-fixed-hold/pty.log`:

```text
^C
gopi worker: SIGINT — the stop request has been delivered; this command is no longer waiting for pid 2577324 to exit (exit 3)
gopi worker: stop request file still standing: …/worker/TERM_2577324
gopi worker: the request is NOT withdrawn — the worker still leaves at its own round boundary. Pressing Ctrl-C again does not escalate it; to interrupt the round use `gopi worker --stop --force`
```

and the same arm against the pre-fix binary produced **no abort line at all**
and had to be killed by the harness after its bound (recorded in
`evidence/arm2-prefix-hold/pty.json`: `"timed_out": true, "signaled": true`).

### 4. What is NOT settled, and is filed separately (Rule 41)

The `/proc/<pid>/status` `SigCgt` of these arms **contradicts** the syscall log
at the same instant, and the contradiction is reported rather than papered over:

* the syscall log (3.1) says the Go runtime's SIGINT handler is installed
  ~4 ms into the process, and the same log at 3.2 shows the same shape on this
  branch;
* the `/proc` samples say `SigCgt` bit 1 (SIGINT) is **clear** for the first
  ~4.8 s of every sampled arm (`fffffffd7fc1fefd`), flipping to
  `fffffffd7fc1feff` afterwards — in three separate runs, in the no-pty arm as
  well as the pty arms, and in the **no-Ctrl-C controls** (`shoths=0`) too,
  i.e. the flip is arm-independent;
* the syscall log of a later run shows a SIGINT `rt_sigaction` setting
  `SIG_IGN` with a real handler as its previous value, 25 µs after that handler
  was installed — so *something* re-ignores SIGINT right after the runtime
  installs it, and neither this document nor the fixing row can name what.

Both readings cannot be true of one instant, so at least one instrument is
misreading something not yet characterised. The honest statement is the one the
repo's own rule asks for: **the /proc mask sequence on this path is unexplained,
and no conclusion in this document rests on it.** The attribution argument above
rests on the source order (which a test pins) and on the syscall log (a record of
the calls themselves, not a derived reading). Filed as its own bug row with the
raw scripts and outputs as its payload.

## A coincidence worth naming, so it is not mistaken for support

`devdoc/DOC-INDEX.tsv`'s classifier picked this document's anchor as
`anchor cmd/agent/main.go at signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)`
— i.e. **the very line this document exists to disqualify.** The anchor is a
mechanical reading of "this doc quotes a line that resolves" and is true; it is
not a statement about the mechanism. Do not read the manifest cell as agreement
with the row this document corrects.
