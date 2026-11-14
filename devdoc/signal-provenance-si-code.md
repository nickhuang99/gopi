<!-- doc-class: normative -->
# signal-provenance: the kernel's own word about who sent a signal

Status: implemented (epic `signal-provenance-si-code`).

## The question this closes

`signal-provenance.jsonl` answered two questions before this change:

1. **where was the victim** (`snapshot`: role, process tree, session dir);
2. **did a gopi stop path announce the kill** (`sourced: true|false`, with
   `intent_evidence` naming the announcement).

Neither separates **"a process called kill()/tgkill()"** from **"the kernel
raised it"** (a terminal hangup, a cgroup teardown, a POSIX timer, an OOM kill).
Every kernel-originated death therefore landed as `sourced:false`, which reads as
"nobody announced it" — i.e. it looks like an unexplained external kill. Measured
shape (2026-09-16): eight gopi processes SIGTERMed inside one second, zero intent
records, no way to tell whether a supervisor or the session teardown had done it.

The new fields turn that undecidable state into a two-way call:

| `si_code` | name | meaning |
|---|---|---|
| `0` | `SI_USER` | an ordinary process: `kill(2)`, `raise(3)`, `sigqueue` |
| `-6` | `SI_TKILL` | thread-directed: `tgkill(2)` / `tkill(2)` |
| `-1` | `SI_QUEUE` | `sigqueue(3)` (carries `si_value`) |
| `-2` | `SI_TIMER` | POSIX interval timer expired |
| `<= -1 downto -64` | `SI_RT<n>` | a queued realtime signal (`SIGRTMIN+n`) |
| `128` | `SI_KERNEL` | the kernel raised it itself (fault, execve thread teardown, cgroup) |
| other | `SI_UNKNOWN(<n>)` | a code outside this vocabulary — printed, never blank |

Two independent axes, deliberately not conflated:

- `si_code <= 0` (`SI_FROMUSER`) ⇒ a **process** sent it, and `sender_pid` is
  that process;
- `sourced` ⇒ a **gopi stop path announced** it.

"Sourced, SI_KERNEL" and "unsourced, SI_USER" are both meaningful states.

## Mechanism: SA_SIGINFO without CGO

Go already installs every watched signal with `SA_SIGINFO` and a `sigsend`-capable
ABI0 trampoline (`runtime/signal_unix.go`, `setsig`: `_SA_SIGINFO|_SA_ONSTACK|
_SA_RESTORER|_SA_RESTART`, `sa_handler = runtime.sigtramp`). **The kernel is
already handing this process a full `siginfo_t`; `os/signal` simply does not read
the sender fields out of it.**

`internal/sigsource` installs a thin `SA_SIGINFO` trampoline of its own —
`trampoline_amd64.s` — that:

1. copies `si_code` (offset 8), `si_pid` (16), `si_uid` (20) into a per-signal
   table, then
2. **tail-jumps into the handler it displaced** (`prevHandler[signo]`, i.e.
   `runtime.sigtramp`).

So the signal still reaches `signal.Notify` with the same disposition, on the
same goroutine, on the same alternate stack, one evidence record richer.
`internal/signalrec` installs it *after* `signal.Notify` (so the chain has a
target) and reads it *in the receiving goroutine* (the table holds the most recent
delivery per signal — reading it a goroutine later could describe another one).

Details that cost a prototype each:

- **`si_uid` is at offset 20, not 24.** The union members with a wider second
  field (`_sigchld`, `_timer`) pad differently; reading 24 returns padding
  (measured: `si_pid` correct, `si_uid` 0 for a uid-1000 sender).
- **Only caller-saved registers may be touched** (`R8 R9 R10 R11 RAX RCX`).
  `R12–R15/RBX/RBP` are callee-saved and `NOSPLIT|NOFRAME` gives no frame to save
  them in.
- **Flags are inherited, then `SA_SIGINFO` is OR-ed in.** Dropping the previous
  `SA_ONSTACK` would run Go's handler on a goroutine stack.
- **`installed[]` is a separate fact from `armed[]`.** `Release` stops the capture
  but leaves the chain in the kernel, so a later `Arm` must be able to tell "our
  trampoline is still installed above someone's handler" from "nothing was ever
  installed" — without it, re-arming read its own trampoline as the displaced
  handler (measured: the 2nd and 3rd test arms reported `Valid=false` while the 1st
  passed).
- **`rt_sigaction` takes an 8-byte sigset**, the kernel's own mask, not libc's
  128-byte `sigset_t`.

## Field shapes (union-compatible, Rule 15a)

`si_code` and `sender_pid` are **pointers**, **not** `int32` with `omitempty`:

- `si_code`'s most important value **is** the zero value (`SI_USER`), so a plain
  `int32` would report every ordinary `kill()` as an absence, and a reader that
  inverted that would accuse the wrong party;
- a nil field means **no capture** (non-Linux host, unarmed signal, refused
  `rt_sigaction`) and the record then keeps the exact pre-epic shape, byte for
  byte.

`sender_pid`/`sender_comm` are written **only** when `si_code <= 0`. For
`SI_KERNEL` (128) and the positive fault codes that union slot holds an unrelated
word (a fault address, a trap number) — writing it as a pid would invent a
suspect.

`sender_comm` is `/proc/<sender_pid>/comm` read **at signal time**, and is
best-effort by nature: the ordinary shape of an operator kill is a short-lived
`kill -INT <pid>` child that is already gone when the victim is scheduled. Absent
means "the sender was not readable then", never "the sender had no name".

## Degradation (non-Linux / non-amd64)

The trampoline is x86_64 assembly because `siginfo_t` offsets and the
signal-entry register convention are architecture facts, not portable ones.

| file | build tag | behaviour |
|---|---|---|
| `trampoline_amd64.s`, `install_linux_amd64.go` | `linux && amd64` | real capture |
| `install_linux_other.go` | `linux && !amd64` | `Delivered` ⇒ `Valid=false`, `UnsupportedReason` names it |
| `install_other.go` | `!linux` | same, naming GOOS/GOARCH |

`internal/signalrec`'s `signal.Notify` path is **unchanged** on those targets and
writes the old record shape. `Arm` failure is printed to stderr and recording goes
on (Rule 15: say it, never swallow it).

## Observation only

`sigsource` **never sends a signal** (the only senders in the package are tests),
never changes a disposition (`SIG_DFL` stays `SIG_DFL`, every installed handler
keeps being called), and `Release` restores nothing — putting a handler back (or a
`SIG_DFL` onto a signal another layer still owns) would be a behaviour change in a
package whose whole promise is to look.

## Verification

See `internal/sigsource/sigsource_capture_test.go` (three arms + the degradation
arm) and `internal/signalrec/signalrec_sicode_test.go` (record-level arms,
including an exited-sender arm that fails if a future change starts fabricating a
`comm`).

The reverse arms are the load-bearing ones: a `si_code` that is **constantly 0**
passes "self-kill is SI_USER" by construction and dies on
`TestDeliveryTKillIsNotSIUser`; a `sender_pid` filled with `os.Getpid()` passes the
self-kill arm and dies on `TestDeliveryFromAChildProcessNamesTheChild`.
