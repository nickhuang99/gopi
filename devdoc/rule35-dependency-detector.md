<!-- doc-class: normative -->
# Rule 35, made checkable — a dependency detector, with the half no test can see named honestly

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

PROJECT.md Rule 35 ("Zero Dependency Is a Discipline — Nothing New Is Created
Casually") was prose with no check. This is the check, and — the part that
matters more — the statement of **what it does not check**.

## The rule's own post-mortem, in one line

A gopi session asked to fix two worker launchers **installed a cron line**:
`*/10 * * * * /home/user/bin/peer-worker-autostart.sh`. Nobody knew it was running.
Its log reached 36,398 lines / 1,214 delegations, and it was **covering a bug** —
workers kept dying and the line kept replacing them, so "always a worker alive"
hid "workers keep dying". The artifact outlived its author's context by two weeks,
and nothing in the system could tell anyone why it existed.

Rule 35's answer is not "no dependencies". It is: **before creating a persistent
dependency, say four things** — (1) what breaks without it, (2) why the failure
cannot be fixed in-process, (3) who owns its lifecycle and how it is reclaimed,
(4) what the next person who finds it running will see. A detector cannot read an
author's mind, so it does the only honest thing available: it catches the
ARTIFACTS and forces the four statements into the review.

## Two halves — and neither substitutes for the other

| half | question | where | who runs it |
|---|---|---|---|
| **repo-side** | does this TREE carry the shapes of a casually created dependency? | `internal/testutil/rule35_dependency_gate_test.go` | the build: `go test ./internal/testutil/ -run Rule35` |
| **host-side** | what is INSTALLED and RUNNING on this machine right now? | `gopi scratch-gc deps` (`cmd/agent/cli_scratch_gc_deps.go`) | a human or the operator's own watchdog |

The split is not a design choice, it is where the information lives. **A gate can
read a tree; it cannot read a crontab.** A crontab line that was installed and
then deleted from the repo is invisible to every test in this repository and
visible in exactly one place — `crontab -l`. Conversely the discovery verb sees
nothing of a script that is about to install a scheduler at 3am.

### What the repo-side gate CAN see (three shapes, zero tolerance)

1. **Self-scheduling script** — an audited script (`scripts/`, `devdoc/`,
   `testdata/`) that writes a scheduler: a crontab INSTALL at command position
   (`… | crontab -`), or a literal cron EXPRESSION (five time fields followed by a
   command, plus the `@reboot`-family shorthand).
2. **Host-level mutation** — a script writing into the operator's host
   configuration: `>>`/`>`/`tee -a` into `.bashrc`, `.profile`,
   `environment.d`, `.config/autostart`, `tmpfiles.d`. Rule 22 already forbids the
   host-level fix in prose; this is the machine check behind that sentence.
3. **Tracked runtime state** — a git-TRACKED path that is runtime state by shape:
   `*.lock`, `*.pid`, a `.gopi/memory.db` / `.gopi/tasks.db`, a scratch-queue
   entry. The path is judged, not its content: a source file named like a lock is
   the same defect as a lock whose bytes are committed.

### What it CANNOT see (the honest half)

These are not "not yet implemented" — they are **not decidable from a tree**, and
they are exactly the four questions the verb exists to answer:

* a crontab line **already installed** for this user;
* a systemd unit/timer **already installed** under `~/.config/systemd/user`;
* a **resident process** already running on this host;
* any dependency that leaves **no artifact in this tree at all** (an entry in
  someone else's crontab, a file in `/etc`, a thing an installer put there).

The retired autostart is the proof that this half is the one that cost us: the
script was in the tree (so gate shape 1 would have flagged it), but the **cron
line** — the part that actually ran every ten minutes — was in a crontab.

## The command a human runs

```bash
# the repo half — part of the build, run it whenever a script changes
go test ./internal/testutil/ -run Rule35 -v

# the host half — what is scheduled and resident on THIS machine
gopi scratch-gc deps              # gopi-shaped findings on stdout
gopi scratch-gc deps 2>/dev/null  # the same, without the foreign neighbours
gopi scratch-gc deps 2>&1 | grep -E 'origin=gopi|summary|note'
```

Measured on peer1, 2026-09-26 (the run that landed this):

> **Retired mechanism (merge daemon loop removal, 2026-11-01).** The `gopi merge
> --every` loop quoted below existed when this run was measured and is gone now —
> see `devdoc/merge-verb-removal-inventory.md`. The row is evidence of the
> DETECTOR's format, not of a process running today; the output is kept verbatim.

```
deps kind=resident name="gopi worker" where="pid=3782211" origin=gopi detached=no cgroup="/user.slice/user-1000.slice/session-8.scope"
deps kind=resident name="gopi merge --every 3m" where="pid=578261" origin=gopi detached=no cgroup="/user.slice/user-1000.slice/session-16.scope"
deps kind=resident name="./bin/gopi web --addr 0.0.0.0:8123" where="pid=221331" origin=gopi detached=no cgroup="/user.slice/user-1000.slice/session-13.scope"
deps summary scheduled=0 units_gopi=0 units_foreign=33 resident=8 gopi=8 foreign=33 total=41
deps note="crontab: 9 entry(ies), 0 active schedule(s) — every line is commented or an assignment"
deps note="units: 33 unit file(s) installed here are not gopi-shaped — listed on stderr, so this report keeps only gopi's own"
deps note="host half only — the repo half (self-scheduling scripts, scheduler-install recipes, tracked state/lock artifacts) is checked by: go test ./internal/testutil/ -run Rule35"
```

Three things that run is worth reading for, because each is a design decision the
output makes visible:

* **`detached=no`, everywhere.** Every resident gopi process on this host is still
  in a `session-*.scope` — none has its own cgroup. That is Rule 24's criterion,
  read from `/proc/<pid>/cgroup` and from nothing else (never PPID, never
  `setsid`), through the same helper the daemon's own startup announcement uses,
  so the two can never disagree about what "detached" means.
* **`resident=26 gopi=26` was WRONG, and the run that produced it is the reason
  the classifier reads `/proc/<pid>/exe`.** Chromium rewrites its own argv, so
  `/proc/<pid>/cmdline`'s first NUL-separated field is the WHOLE command line —
  `chrome --type=gpu-process … --user-data-dir=…/queue-runtime-shot-…/gopi-shot-profile-…`.
  Classifying `filepath.Base(argv[0])` therefore read a scratch **directory
  name**, found the word `gopi`, and reported 22 browsers as dependencies of
  ours; the same browser under `--user-data-dir=/tmp/prof` classified `foreign`.
  A verdict that flips on a directory name is not a measurement. The arm now
  judges the PROGRAM (`/proc/<pid>/exe`, `chrome` in both spellings), going one
  argument deeper only for an interpreter (`sh -c …/gopi-worker-up` is really
  the script); an unreadable `exe` is `unknown`, never a silent `foreign`.
  After the fix the same host reads **`resident=8 gopi=8`** — all eight
  genuinely gopi. The regression guard plants a live process whose argv[0]
  carries the scratch-path shape and asserts its verdict is `foreign`; against
  the old rule that test fails with `= "gopi"`.
* **The SAME defect, a second sighting — and the first fix did not cover it.** A
  day later the same arm on the same host read `resident=9 gopi=9`, the ninth
  being a **generic shell**: `bash -c "cd
  /home/user/workspace/gopi-01a0ddfa… && sleep 60"`. The argv[0] fix had closed
  the trap for NON-interpreters only; the interpreter branch still did
  `filepath.Base(<first non-flag arg>)`, and for `sh -c "<line>"` that argument
  is a command **string**, not a path — so `filepath.Base("cd …/gopi-01a0… && …")`
  returned the last slash segment, and the word `gopi` came from a **worktree
  directory name**. Measured live, two such shells at once. Worse, and the
  reason this is worth a whole page: one of them was **the arm's own
  invocation**, so the number moved with how the report was run —
  `resident=7` plainly, `8` wrapped in `bash -c`, `9` with the plant, same host,
  same minute. The one-line change that closes it is `depsOriginFromProgram`:
  an operand of `-c` is a command string and is NEVER judged (`unknown`), and a
  non-flag argument containing a space is not a name either; a direct script
  operand (`bash …/gopi-worker-up.sh`) is still judged, which is the case the
  rule exists for. The fix moved the count by **one transient shell** and zero
  real findings: with the plant live, plain / wrapped / planted all read
  `resident=8 gopi=8`. Nothing is lost by refusing to judge a command string —
  the program such a line starts is its own process with its own `exe`, which is
  exactly how `bash -c "./bin/gopi web"` is reported. The guard is
  `TestScratchGCDepsInterpreterCommandStringIsNotAName`; it drives the pure
  decision (no planted process, so it cannot be skipped on a busy host) and each
  of its two rules is independently red-able (disabling the `-c` rule fails the
  no-space case; disabling the space rule fails the quoted-name case — both
  measured, both restored).
* **`scheduled=0` is a MEASURED zero, not an absent answer.** The crontab is 9
  lines of **commented-out history** (Evennia / hermes / glow-moon entries,
  disabled in the 2026-08-21 token sweep). The arm says `9 entry(ies), 0 active
  schedule(s)` — "empty" for a file with nine lines would be the imprecision Rule
  15 forbids, and it is exactly the shape a reader needs to trust the count.
* **`units_gopi=0` against `units_foreign=33`**, with the neighbours named on
  stderr. A report that quietly filtered to what it was looking for would leave
  the operator unable to tell "nothing of ours is installed" from "the scan never
  looked". (The scratch-GC watchdog timer is **not** installed on this host — the
  units present are the operator's desktop services, tigervnc and x11vnc.)

## Design decisions worth knowing (Rule 7)

**The gate rides `internal/testutil/scripts_gate_test.go`'s idiom, not a new
one.** It is the same family: a walk of `scripts/`+`devdoc/`+`testdata/`, comments
stripped before matching (the `isCommentLine` lesson — this repo's own evidence
files QUOTE the retired cron line, and a detector that fires on a quotation gets
relaxed the first time a reviewer meets it), a non-vacuity floor (240 script files
scanned; fewer than 40 is a red gate, not a green one), and a total stated
alongside a capped listing.

**Four measured false positives shaped the detector, and each is recorded in the
code where it bit.**

| shape | why it fired | fix |
|---|---|---|
| `for i in 0 1 2 3 4 5 6 7 8 9; do …` | five single digits ARE five valid cron fields | a schedule must begin the line, a quote, an assignment or `(` |
| `for d in (0, 1, 7, 30, 90, 180, 365):` | a trailing comma is a list separator, not a cron field | a field must START and END with a digit or `*` |
| `sec "root crontab / cron.d"` | a substring test finds the word inside prose | the check is on the COMMAND POSITION (segment-split, the baseline gate's own helper shape) |
| `echo "--- crontab (user) ---"` | as above | as above |

**The reviewed exemptions are named, in the code, with a sentence each** — never a
relaxed pattern. Two scheduler INSTALLERS are exempt: `install-scratch-gc-watchdog.sh`
and `gopi-home-gc.sh`, both installing the operator's own disk-hygiene upkeep
(Rule 34's timer, whose reasoning lives in the unit's own header), and both falling
back to a crontab only when no user bus answers. That is precisely the line the
gate is for: **the gate cannot tell a reviewed dependency from the retired
autostart**, so a file that installs a schedule may not be silently grandfathered —
it is listed, one sentence per entry, and the sentence is the review.
Five tracked paths are likewise exempt with reasons — the committed
`sandbox/*/.gopi/*` trees (`abtest`, `abtest2`, `sbx` ledgers, `sbx`'s task ledger
and its singleton lock). The sandboxes are themselves the finding
the rule was written for — agent profiles inside the source tree, rescued into the
index by `6a6aef6d` (2026-09-17) — and Rule 34 says they belong under the sandbox
root. The gate flags them; the review answers.

**The verb is an ARM of `scratch-gc`, not a new verb and not a new process.** Rule
35 would be self-refuting if it were enforced by a fresh daemon, so the discovery
surface is a pure addition to the host's existing one-shot upkeep face — and it is
deliberately placed **before** the sweep's off switch, because an operator who
disabled the sweeps still needs to see what their host is running. `gopi -h`'s
verb list is untouched (Rule 3: the CLI surface did not grow).

**A gopi-shaped finding is not an error; a measurement gap is.** The arm exits 0
with gopi-shaped residents listed (the operator's own watchdog is one of them, and
the arm is read-only — the call is the operator's). It exits non-zero only when a
source could not be read at all, because a report that quietly says "nothing
found" on a machine it could not read is the silent failure Rule 15 forbids.

## What is NOT delivered

* **No cron/systemd unit is installed by this change.** The discovery is on
  demand, and the repo-side gate runs in the test suite. Nothing new schedules
  itself; that constraint is the whole point.
* The gate is a **shape detector, not an intent reader.** A persistent dependency
  that leaves no artifact in this tree, or one whose artifact is a filename the
  three shapes do not cover, is not caught. The state-shape list is the reviewed
  boundary of that claim, and the wording "the shapes a gate CAN see" is the
  scoping, not a hedge.
* `cgroupscope`'s relativity means a cgroup can only be reported for a **live,
  readable** pid; a process that exited between the enumeration and the read shows
  no `cgroup=` field rather than a guessed one.
