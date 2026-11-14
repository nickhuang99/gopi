<!-- doc-class: normative -->
# Replay: port semantics and same-second artifact concurrency

**Class: `historical`** — the record of THIS run's measurements. The class is not
a self-assessment: it is the verdict of the repository's own classifier, and
`devdoc/DOC-INDEX.tsv` carries the same row (`gopi doccheck` reports the corpus
consistent). It is `historical` rather than `normative` for a reason worth stating,
because it is the boundary of this task: **the listener this document describes
belongs to item 1 (`01a0fea1-da84…`) and is not yet in this tree**, so a standing
claim about `master` here could not be gated by any check — there is no file to
resolve it against. What IS in the tree is the concurrency behaviour of
`internal/replay/session_ground.go`, and that half is pinned by the regression test
this document ships. The wording in §1 is nevertheless the standard the pipeline's
own text must follow once item 1 lands.

This document is the companion of task `01a0fe9f8be17f05b2997fa350bc71d1`, a
follow-up to the replay re-implementation row `01a0fe9b`. It adds **no
implementation**: the stub's listener belongs to item 1 (`01a0fea1-da84…`, the
stub + seam), and here we only fix the WORDS and pin the concurrency BEHAVIOUR
with a test. See `## Not Done` for the boundary.

---

## 1. The port: what the stub's listener IS, in accurate words

The wording this document exists to ban — that the port is one **no other process
can listen on / reach** ("别的进程侦听不到") — is **FALSE**, and it is the kind of
false that a reader acts on (it suggests a private channel where there is none).
It is not in this tree yet: it is the phrasing the follow-up row forbids before it
gets written into the stub's own text (item 1, `01a0fea1-da84…`). The measurement
below falsifies it with a real request from two independent processes, so the
wording has a verdict *before* it has a home.

### The wording table

| claim | verdict | why |
|---|---|---|
| "only bound to `127.0.0.1`" | **TRUE** | `listener_addr=127.0.0.1:<port>`, `listen_host=127.0.0.1`; `ss` shows the local address column as `127.0.0.1:<port>`, never `0.0.0.0:*` or `[::]` |
| "the port is allocated by the kernel (`:0`), not chosen by the stub" | **TRUE** | the stub asks for `:0`; the number appears only after the bind, and every run gave a DIFFERENT number — `44189`, `33837`, `46431`, `40377`, `34205` (five runs of the probe: `evidence/portprobe.out`, `portprobe2.out`, `portproof/portproof.out`, `portproof3.out`) |
| "no other process can **listen** on that port" | **TRUE, but only while the stub holds it** | a bound TCP listener owns the 4-tuple; `ss -ltnp` attributes it: `users:(("portproof",pid=2459785,fd=3))`. After `Close` the port is immediately bindable again, so this is not a lasting claim |
| "**no other process can reach it**" / "别的进程侦听不到" | **FALSE — do not write this** | two independent processes (bash `/dev/tcp`, `python3` socket) sent a real HTTP request to `127.0.0.1:<port>` and each received `HTTP/1.0 200 OK` from the stub |

### The wording to use

> The replay stub listens on **loopback only** (`127.0.0.1`), on a port the
> **kernel assigns** (the stub binds `:0`, it does not pick a number). It is not a
> private channel: **any process on this machine that knows the port can connect
> to it** — the isolation is that the port is not advertised and is not reachable
> off-host, not that other processes are excluded.

The `127.0.0.1` binding is what makes the last clause true of the machine as a
whole: the socket is unreachable from another host, so nothing outside peer1 can
find it. Off-host is the boundary the binding actually draws.

### Measurement (`## Verification` → `Ran:`)

From this run's session directory (`evidence/portproof/`: `main.go`, `portproof`,
`portproof.out`, `live-ss.txt`):

```
url=http://127.0.0.1:46431
pid=2459785
listener_addr=127.0.0.1:46431
listener_network=tcp
listen_host=127.0.0.1
assigned_port=46431
port_is_ephemeral_kernel_choice=true
crossproc_bash_devtcp=HTTP/1.0 200 OK Date: Fri, 02 Oct 2026 22:07:23 GMT Content-Length: 29
crossproc_python3=HTTP/1.0 200 OK Date: Fri, 02 Oct 2026 22:07:23 GMT
control_closed_port_1=bash: connect: Connection refused /dev/tcp/127.0.0.1/1: Connection refused CONNECT-FAILED
ss_listen_rows_for_port_after_close=0
rebind_after_close=ok (port 46431 re-acquired by this pid)
```

The kernel-allocated port, seen by an **outside** process while the stub was
still alive (`ss -ltnp | grep :46431`, `live-ss.txt`):

```
LISTEN 0      4096       127.0.0.1:46431      0.0.0.0:*    users:(("portproof",pid=2459785,fd=3))
```

Read the three columns together: `127.0.0.1` in the local-address column (bound
to loopback, not `0.0.0.0`), a `5`-digit ephemeral number in the port column (the
kernel's choice), and `users:(("portproof",pid=2459785,fd=3))` (the holder is the
probe process itself — not some pre-existing service that happened to be there).

**Re-measured while this report was being written** (two further, independent runs
of the same probe shape, `evidence/portproof3.out`): `assigned_port=40377` and
then `assigned_port=34205`, each `listen_host=127.0.0.1`, each held by
`users:(("stampprobe",pid=…,fd=3))`, and each
`ss_listen_rows_for_port_after_close=0` against a `ss -ltn` baseline of 26 rows —
the same three facts, again, on a port the kernel chose. Five runs, five different
numbers (`44189`, `33837`, `46431`, `40377`, `34205`): nothing here is a constant
someone picked.

**The control matters as much as the result.** The same request form to
`127.0.0.1:1`, where nothing listens, is refused — so "200 OK" is a property of
the stub's socket, not of the request tool. And the listener table is back to its
**26-line** baseline after `Close` (`evidence/portproof/ss-baseline.txt` and
`ss-after.txt` are 27 lines each — one header plus 26 `LISTEN` rows — and are
byte-identical after the header), with **zero** rows naming the assigned port:
`grep -c 46431 ss-after.txt` → 0. The allocation is released, not leaked, and the
same pid re-acquires the very port (`rebind_after_close=ok`).

---

## 2. The three isolation layers that already exist — reuse, never rebuild

These were verified as PRESENT, not re-implemented (Rule 13/14: one definition
each, referenced by its path). A future task that wants any of these must extend
the file named here.

| layer | what it isolates | where it is defined | the mechanism (read from the source) |
|---|---|---|---|
| **display** | the X server a run films on | `scripts/replay/display.sh:53` `DISPLAY_SOCKET_DIR="/tmp/.X11-unix"`, `:54` `DISPLAY_SOCKET_PREFIX="X"`, `:209` `display_candidates()`, `:246` `sock="$DISPLAY_SOCKET_DIR/$DISPLAY_SOCKET_PREFIX$num"` | the candidate numbers are **derived from this machine's actual X sockets**, not from a literal range: `display.sh:50` — *"the X server creates `<dir>/X<n>` … that socket IS the occupancy record"*. A run takes a number nothing holds; the failure of an attempt is classified from that attempt's own Xvfb log |
| **sandbox** | the run's gopi profile (ledger, sessions) | `scripts/replay/replay.sh:449` `SB="$("$GOPI" sandbox create --label "replay-$(date +%Y%m%d-%H%M%S)" --task-id … \| tail -1)"`, `:451` `[ -d "$SB/.gopi" ] \|\| die …` | a **fresh sandbox per run** (Rule 34): the private profile is materialised by the ONE initializer, so a re-run cannot read the previous run's state, and the operator's own ledger is untouched |
| **scratch** | every temp path the run writes | `scripts/replay/replay.sh:372` `SCRATCH="$(gopi_scratch_mkdir replay)"`, defined `scripts/lib/tmp-ban.sh:102` `gopi_scratch_mkdir` | off the repository **and off the `/` partition**, asserted rather than assumed (`tmp-ban.sh:53` `gopi_assert_off_root_fs`, `:46` `gopi_scratch_queue`); the directory is kept as the run's evidence and reclaimed by the age GC |

Each layer was already load-bearing before this task; nothing here changed them.

---

## 3. The artifact directory: same-second concurrency is already answered — (a)

**The question.** A film lands at
`<session dir>/artifacts/replay/<UTC stamp>` where the stamp is
`20060102-150405` (`internal/replay/session_ground.go:51` `stampLayout`) — a
**second**-resolution name. Two recordings of one session started inside the same
second therefore compute the SAME name. What happens?

**The verdict: (a) a unique suffix — never (b) refuse-if-exists.** The
implementation takes (a), and the choice is right, for three reasons that are all
about what a refusal would cost:

1. **A film is an artifact of a run that already succeeded.** By the time `FilmDir`
   is called the run has passed its dependency preflight, its display allocation
   and its sandbox creation. Refusing the destination at that point throws away
   all of it — and the caller has no remedy, because the collision is an artifact
   of a clock, not of anything the caller could change.
2. **A second-resolution name is a human LABEL, not an identifier.** "The film of
   14:10:25" is what a person looks for; that the 32nd caller in one second needs
   `-32` behind it costs nothing, while a refusal would make the pipeline's
   success depend on how fast the operator's shell loop is.
3. **(b) is not a policy that can be honestly implemented here.** It needs
   someone to decide what "already exists" means when the *directory* exists
   because a concurrent caller created it microseconds ago — the same-second case
   the question is about — and refuse there too. That turns a benign race into a
   spurious failure.

**How it is implemented** (`internal/replay/session_ground.go:82` `FilmDir`,
`:103–107`):

```go
if err := os.Mkdir(dir, 0o755); err != nil {
    if os.IsExist(err) {
        continue // another run took it between the stat and the mkdir
    }
```

The `stat`-then-`mkdir` pair is a **check followed by a claim**, and the claim is
the one that decides: a competitor that wins the window between the two is stepped
over by the `IsExist → continue`, never written into. So the function is
race-tolerant by construction, and its documented contract — *"It never returns a
directory that exists"* — holds under concurrency, not merely in sequence.

The bound is `stampAttempts = 256` (`:57`), and its comment is the policy: 256
films of one session inside one second is *"a diagnosis to write, never a number
to raise."*

**The gap this task closed.** The property was true and **untested**. The
existing script test pins only the SEQUENTIAL case —
`scripts/replay/test-session-operand.sh:176` "C5: two films of ONE session never
share a directory" — which calls the resolver twice, seconds apart, and would
pass against an implementation that collides under load. Nothing drove the
same-second window.

**Now pinned by** `internal/replay/session_ground_test.go`:
`TestFilmDirGivesEverySameSecondRunItsOwnDirectory` releases 32 goroutines
through one channel with the SAME `now`, and asserts four properties, each
reported independently so a failure names WHICH one broke:

1. the 32 returned names are pairwise DISTINCT;
2. every name is a directory that **really exists on disk**, under the session's
   own `artifacts/replay/`, carrying the stamp;
3. exactly **one** carries the BARE stamp (the suffix is the disambiguator, so it
   is not handed out when the name is free — the first film of a second is the one
   findable by stamp alone);
4. a further call in that same second still gets a NEW, created directory.

### Evidence that the test can fail (a mutation series, not a green)

Applied to `internal/replay/session_ground.go`, reverted after each:

| mutant | what it models | result (`evidence/filmdir-mutant*.txt`) |
|---|---|---|
| no `-N` suffix (one name per second) | the naive implementation the (a)/(b) question is about | **FAIL** — all 32 calls: `FilmDir(call N) failed: no free film directory … after 256 attempts at stamp 20261003-141025` (32 hits) |
| drop the `os.IsExist(err) → continue` | losing the race instead of stepping over it | **FAIL** — 23 × `mkdir …-8: file exists` (the losers of the window, each one a call that would have died mid-run) |
| `if statErr == nil { return dir, nil }` | "it exists, so use it" — the real overwrite bug | **FAIL** — 30 pairs, `<FAIL> calls 0 and 1 share the film directory …/20261003-141025 — two recordings of one second would overwrite each other's replay.json and GIF` |

Each mutant's file was reverted and the source verified byte-identical to its
pre-mutation copy (`diff -q`), so the green below is the shipped code:
`go test ./internal/replay/ -run TestFilmDirGivesEverySameSecondRunItsOwnDirectory -v` →
`--- PASS: … (0.00s)`; `-race -count=3` is clean on the real implementation.

The THIRD mutant is the point: it is the only one that reproduces the damage the
rule forbids — two recordings of one second sharing one directory — and the test
names that damage in those words rather than reporting a generic failure. The
first two fail differently (no name at all, and a lost race), which is why the
four assertions are reported independently.

---

## 4. Not re-created here

- **The stub's listener** (the `:0` bind, the seam, the handler) — item 1,
  `01a0fea1-da84…`. This document says what that listener MEANS; it does not
  implement it.
- **The three isolation layers** — §2 lists them by path; no second answer to any
  of them was added.
- **No unix socket** was introduced. "A local process that knows the port can
  reach it" is a fact to STATE accurately, not a defect to engineer around; a
  unix socket would be a new mechanism (Rule 35) answering a question nobody
  asked.

## 5. Verification material

Every artifact this document cites lives in this run's session directory
(`~/.gopi/sessions/01a0fe9f8be17f05b2997fa350bc71d1/evidence/`), never in the
checkout (Rule 30): `portprobe.{go,out}`, `portprobe2.out` (the first probe),
`portproof/{main.go,portproof.out,live-ss.txt,ss-baseline.txt,ss-after.txt}` (the
second, under `ss -ltnp`), `portproof3.out` (the two re-measurements), and
`filmdir-{green,mutant,mutantB,mutantC}.txt`.
The one artifact that IS a deliverable — the regression test — is committed here.
