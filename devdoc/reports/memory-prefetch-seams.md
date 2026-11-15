<!-- doc-class: normative -->

# Memory prefetch seams — three access points, studied (no product change)

**Task** `01a0e53e-d36d` (child of epic `01a0e481`). **Scope**: research only. This
document changes no product behaviour — it names *where* a typing-time memory
prefetch could attach, with ≥3 candidates per seam, the one selected, and why the
others are not.

**Tree**: branch `task/01a0e53ed36d71bf94b8a614b4b03710`, base master `9fb9f8e8`.
**Every `file:line` below was re-read out of THIS tree** (`sed -n "<line>p"`), not
copied from memory — see §4 for the drift that makes that mandatory.

---

## 1. Seam A — reading the in-progress line ("打字中") from the line editor

The question: while a person types, what path can read the *current* buffer
without racing the editor's read loop, and what triggers the read?

| # | candidate access point | file:line | what it gives | why not |
|---|---|---|---|---|
| A1 | `Value.Text()` — the buffer read | `internal/lineedit/value.go:124` (reached via `Editor.Value()` `internal/lineedit/editor.go:1172`) | the buffer's contents, from ANY goroutine: a foreign reader gets the **published snapshot**, not the live fields | **SELECTED read path** (see below) |
| A2 | `Editor.inputPending()` — the loop's readiness seam | `internal/lineedit/editor.go:459` (impl. `(*Terminal).InputPending` `internal/lineedit/raw.go:401`, optional interface `inputBacklog` `internal/lineedit/raw.go:165`) | "is a keystroke already waiting" — a level, zero-timeout `poll(2)` | tells you *that* someone typed, never *what* they typed; the buffer it implies is not readable from here |
| A3 | `Editor.Source()` — the raw event source | `internal/lineedit/editor.go:896` | the `Source` (`ReadEvent`/`Width`/`Height`, `internal/lineedit/raw.go:145`) | a **consuming** interface: reading it to peek steals bytes from the editor's decoder |
| A4 | the `Run()` read loop itself | `internal/lineedit/editor.go:1201`; the blocking read at `:1252` (`ev, err := e.src.ReadEvent(buf, timeout)`) | the only place that WRITES the model | the loop is parked in `ReadEvent` for an unbounded wait (`timeout` is 0 unless a lone ESC or a pending completion bounds it) — a sample cannot ride a loop that is blocked, and a tick added here would be a production behaviour change |
| A5 | `Editor.SetStatus(fn func() Status)` — the loop-driven callback | `internal/lineedit/editor.go:653` (invoked loop-side during repaint; the draw site is `internal/lineedit/editor.go:1302`/`:1345`) | a callback the loop itself calls, on its own goroutine, with the live buffer in hand | it is a **rendering** contract sampled at keystroke cadence: a status callback that suddenly costs a DB query would put I/O on the single-writer frame path (the exact hazard `internal/lineedit/valueowner.go` was written to remove) |
| A6 | `Injector.InputPending()` — the source wrapper | `internal/lineedit/inject.go:621` (wrapper read at `:524`) | the same readiness answer, through the injection seam | same as A2: readiness, not contents |

**Selected: A1 — `Editor.Value().Text()`, read from a FOREIGN goroutine.**

**Why not the others.** It is the one read that is safe *by construction* rather
than by discipline: `Value.tick()` (`internal/lineedit/value.go:166`) publishes a
snapshot on **every** mutation, so `Text`/`Len`/`Cursor` (`value.go:124`/`:143`)
serve every non-owner reader from that copy (`internal/lineedit/value.go:92–95`,
and the invariant is written down in `internal/lineedit/valueowner.go:5–19`:
"`Value` has ONE writer while a read loop runs… a read taken on the read loop's
own goroutine reads the live fields"). A prefetch goroutine is exactly such a
foreign reader, and the file already declares what it gets: *"a reader outside the
loop sees the buffer at the last mutation the loop finished."* So **the read needs
no new production seam at all** — the existing API is sufficient and race-free.

A2/A6 are not alternatives but a **trigger** input; A3 consumes; A4/A5 would put
I/O on the frame path. The remaining design question (not this task's product
decision) is the trigger's cadence — and note its cost is bounded by A2's own
design note (`raw.go:166`): a `false` answer is a hint, never a safety property.

## 2. Seam B — the single place chat's prompt is assembled

| # | candidate access point | file:line | what it is | why not |
|---|---|---|---|---|
| B1 | the turn's outbound-message boundary | `cmd/agent/cli_chat.go:1731` — `out, sealErr := chatSealMessage(sess, msg, sess.toolsEnabled)` | THE one place a turn's model input is produced: the comment at `:1721` states it — *"Everything that reaches ag.Chat/ChatOps below is `out`, so there is exactly one outbound message per turn and no path around it."* | **SELECTED** |
| B2 | `chatOutbound.Message` — the struct field | `cmd/agent/chat_boundary.go:112` | what actually goes to the model (byte-identical for an untouched line, by contract) | it is the *carrier*, not the decision; attaching here would put the rule inside the paste-spill boundary (`chat_boundary.go:178`) |
| B3 | the turn entry | `cmd/agent/cli_chat.go:1864`/`:1866` — `ag.ChatOps(out.Message, sess.history)` / `ag.Chat(...)` | the last crossing before the Agent | below B1 the message is already sealed; a second mutation site here is a second assembly point, which is exactly what B1's comment forbids |
| B4 | the Agent entry | `internal/agent/loop.go:4492` — `func (a *Agent) Chat(userInput string, history …)` | where chat turns start inside the agent | scope: the **same** entry family serves workers and one-shot runs (`Run` `:3465`, `RunPrompt` `:3490`); a chat-only prefetch placed here fires for every run |
| B5 | system-prompt assembly | `internal/agent/loop.go:3529` — `a.Ctx.SystemPrompt = a.SystemPrompt` (const `chatSystemPrompt` `internal/agent/loop.go:1147`) | the system half | the system prompt is built once per run, not per turn — a per-turn prefetch cannot ride it |
| B6 | the existing in-turn injection precedent | `internal/agent/loop.go:4703` — `func (a *Agent) maybeInjectTaskStartSearch() bool` (called before the first round, `:3642`) | the harness's own "inject a user message the operator did not type" mechanism | it is a **task-run** checkpoint (once per run, `taskStartSearchPrompted`), not a per-turn chat seam; it is the pattern to copy for *accounting*, not the place to attach |
| B7 | the one-shot face's own prompt | `cmd/agent/chat_once.go:563` — `prompt := opts.prompt` | the paste/one-shot path's prompt resolution | a different face of the same verb; wiring here would leave the interactive face without the feature |

**Selected: B1 — `cmd/agent/cli_chat.go:1731`.**

**Why not the others.** B1 is where the tree itself asserts uniqueness, it is shared
by every chat face (editor, cooked, one-shot), and it sits *above* the turn
renderer (`renderer := newChatTurnRenderer(input)`, `:1745`) so the injected text
can be separated from what the operator is shown — `chatOutbound.Display`
(`chat_boundary.go:114`) already carries the human label, so "what the model gets"
and "what the screen says" are distinct fields, not a new concept. B4 is rejected
on scope (worker/one-shot contamination) and B5/B6 on cadence (per-run, not
per-turn). B2/B3/B7 are not assembly points but carrier/echo/echo.

## 3. Seam C — read-only memory retrieval (the existing API)

| # | candidate access point | file:line | what it is | why not |
|---|---|---|---|---|
| C1 | the structured retrieval exit | `internal/tools/memory.go:1543` — `func MemorySearchHits(query string, limit int, intent string) ([]MemoryHit, error)` | hits as data: ids, tier, confidence — everything a gate needs | **SELECTED** |
| C2 | the rendered exit | `internal/tools/memory.go:1851` — `func MemorySearch(query string, limit int, intent string) (string, error)` | the same recall, rendered to text | parsing rendered text back to structure is the exact fragility `MemorySearchHits`'s doc comment (`:1536–1541`) says it exists to remove |
| C3 | the search opener | `internal/tools/memory.go:1421` — `func openSearchDB(ctx, path)` | resolves + opens the search handle | it is **not** read-only: it goes through `openDB` (`internal/tools/memory.go:653`), which on a cold/mismatched default DB **migrates** — running it on the operator's live ledger from a prefetch goroutine would make the prefetch a writer |
| C4 | the read-only DSN | `internal/tools/ledger_dsn.go:57` — `func LedgerReadOnlyDSN(path string) string` | `mode=ro` + `_pragma=busy_timeout(dbBusyTimeoutMSec)` + `_pragma=query_only(true)`, built from production's own constant (`internal/tools/memory.go:85`, `dbBusyTimeoutMSec = 15000`) | **SELECTED opener** — it is the existing, tested shape; precedent user `internal/tools/ledger_backup.go:483` |
| C5 | the immediate-refusal pragma | `internal/tools/ledger_dsn.go:91` — `func ledgerNoWaitPragma() string` (`ledgerNoWaitBusyTimeoutMSec = 0`, `:67`) | `PRAGMA busy_timeout = 0` = refuse at once, never queue | available as the give-up is sharpened from "bounded wait" to "don't wait at all" — see the measurement below (0 ms) |
| C6 | the hot-resident digest | `internal/tools/memory.go:4664` — `func MemoryHotResident() string` | a best-effort, error-swallowing digest read | it is a per-run hint, not a per-keystroke query, and it is deliberately silent on failure (the shape a prefetch must NOT copy — Rule 15) |

**Selected: C1 (`MemorySearchHits`) opened through C4 (`LedgerReadOnlyDSN`).**

**Why not the others.** C1 and C2 share every recall route by design (`:1539`) so
C2 adds only rendering; C1 returns the data. The opener is the load-bearing
choice: C3 is a *writer* on a cold DB (migration) and must not be reached from a
prefetch; C4 is the same DSN the artifact verifier already trusts and is
constant-sourced (Rule 13/14 — `internal/tools/ledger_dsn_test.go` pins the
no-re-typed-literal property). C5 is the escape hatch if 15 s is judged too long
for a keystroke path; the measurement below shows both arms.

## 4. Line numbers drift — memory #6425 vs this tree

`memory #6425` recorded candidate anchors from the reframe run. Re-read on
master `9fb9f8e8`, three of them moved by hundreds of lines:

| #6425 claimed | this tree | drift |
|---|---|---|
| `internal/lineedit/editor.go:606` `Source` | **`editor.go:896`** | +290 |
| `internal/lineedit/editor.go:245` `inputPending` | **`editor.go:459`** | +214 |
| `internal/lineedit/editor.go:886` `Run` | **`editor.go:1201`** | +315 |
| `internal/tools/memory.go:1521` `MemorySearchHits` | **`memory.go:1543`** | +22 |
| `internal/tools/memory.go:1818` `MemorySearch` | **`memory.go:1851`** | +33 |
| `internal/tools/ledger_dsn.go:57` `LedgerReadOnlyDSN` | **`ledger_dsn.go:57`** | exact |
| `internal/tools/ledger_dsn.go:92` `ledgerNoWaitPragma` | **`ledger_dsn.go:91`** | −1 |

This is the whole reason §1–§3 cite lines re-read from the tree: a prefetch
attached to `editor.go:886` would land on an unrelated line.

## 5. Measurement — does the read-only DSN give up per `busy_timeout`, or block?

Rule 31: a claim about the DSN's give-up semantics is worth nothing until it is
run. The probe below is a **throwaway**: it lives at
`internal/tools/probe_seam/measure_test.go` (build tag `probeignore`) because it
must import `internal/tools` to call the real `LedgerReadOnlyDSN`, it runs **no
load generator** (Rule 27 — three opens and three queries), and it is **deleted
before the commit**; its source is reproduced verbatim in the appendix so a
future reader can re-run it.

Scratch root: `$PWD/.probe` (device `2052`, the `/home` partition — **not** `/tmp`,
which is device `2051`, i.e. on `/`; Rule 22).

### 5a. Verbatim command

```bash
cd /home/user/workspace/gopi-01a0e53ed36d71bf94b8a614b4b03710
mkdir -p .probe
PROBE_BASE=$PWD/.probe go test -tags probeignore -count=1 -v ./internal/tools/probe_seam/
```

### 5b. Verbatim output

```
=== RUN   TestReadOnlyDSNGivesUpPerBusyTimeout
ARM_A_DSN=file:/home/user/workspace/gopi-01a0e53ed36d71bf94b8a614b4b03710/.probe/seam-2444536285/memory.db?mode=ro&_pragma=busy_timeout(15000)&_pragma=query_only(true)
ARM_A_MS=14539 ERR=database is locked (5) (SQLITE_BUSY)
ARM_A0_MS=0 ERR=database is locked (5) (SQLITE_BUSY)
--- PASS: TestReadOnlyDSNGivesUpPerBusyTimeout (16.31s)
=== RUN   TestWALReaderDoesNotBlock
ARM_B_MS=0 ERR=<nil>
--- PASS: TestWALReaderDoesNotBlock (0.37s)
=== RUN   TestReadOnlyDSNRefusesWrites
ARM_C_WRITE_ERR=attempt to write a readonly database (8)
--- PASS: TestReadOnlyDSNRefusesWrites (0.14s)
PASS
ok  	github.com/the operator/gopi/internal/tools/probe_seam	16.834s
```

### 5c. What each arm establishes

- **Arm A (the acceptance question).** Rollback journal, writer holding
  `BEGIN EXCLUSIVE` + an uncommitted INSERT. The read-only DSN's query returned
  **`SQLITE_BUSY` after 14 539 ms**, bounded by the DSN's own
  `busy_timeout(15000)` — it **gives up with an error; it does not block forever**.
  The 461 ms shortfall is the retry-loop granularity, not an unbounded wait.
- **Arm A0.** The same query on the same lock with `busy_timeout(0)` — the shape
  `ledgerNoWaitPragma()` renders — returned `SQLITE_BUSY` in **0 ms**. This is the
  "a keystroke must never wait 15 s" lever, measured rather than asserted.
- **Arm B.** With the ledger in **WAL** (production's own mode,
  `enableWAL` — `internal/tools/memory.go:690`), a reader took the **committed
  snapshot in 0 ms** while a writer's transaction was open: no block, no error.
  Measured supporting fact for the epic: a read-only prefetch against a live WAL
  ledger is not merely bounded, it is **non-blocking**.
- **Arm C.** The same DSN refused an `INSERT` — `attempt to write a readonly
  database (8)`. `mode=ro` + `query_only(true)` hold, so a prefetch cannot write
  the ledger it reads.

### 5d. Not measured (honest gaps)

- **`mode=ro` against a WAL ledger whose `-shm`/`-wal` the reader may not write.**
  Arm B ran with the side files writable. A reader with no write access to the
  `-shm` is reported by SQLite to require `immutable=1`; this was **not** tested
  here and is a real risk for a prefetch running under a different uid. No claim
  is made either way.
- **Contention under a *concurrent* writer**, as opposed to a single held lock.
  Rule 27 forbids manufacturing that load, so it stays a hypothesis.
- The probe does not use `MemorySearchHits` end to end (it needs a seeded ledger);
  it measures the **opener's** contract, which is what the acceptance asks for.

## 6. Appendix — the probe, verbatim (deleted before commit)

```go
//go:build probeignore
package probe_seam

// (arm A) rollback journal + BEGIN EXCLUSIVE -> SQLITE_BUSY, bounded by
// busy_timeout; (arm A0) the same with busy_timeout=0; (arm B) WAL + open txn ->
// snapshot, no block; (arm C) the DSN refuses a write.
const probeZeroBusyTimeoutMS = 0

func TestReadOnlyDSNGivesUpPerBusyTimeout(t *testing.T) {
	dir := scratchDir(t)                       // os.MkdirTemp($PROBE_BASE, "seam-")
	path := filepath.Join(dir, "memory.db")
	_, conn := seedWriter(t, path, false)      // journal_mode=DELETE; BEGIN EXCLUSIVE; INSERT
	dsn := tools.LedgerReadOnlyDSN(path)
	el, err := timedRead(t, dsn)               // start := time.Now(); QueryRow("SELECT count(*) FROM t").Scan(&n)
	fmt.Printf("ARM_A_MS=%d ERR=%v\n", el.Milliseconds(), err)
	if err == nil { t.Fatalf("read SUCCEEDED under a held EXCLUSIVE lock") }
	zero := "file:" + path + "?mode=ro&_pragma=busy_timeout(" +
		fmt.Sprint(probeZeroBusyTimeoutMS) + ")&_pragma=query_only(true)"
	el0, err0 := timedRead(t, zero)
	fmt.Printf("ARM_A0_MS=%d ERR=%v\n", el0.Milliseconds(), err0)
	conn.ExecContext(context.Background(), "ROLLBACK")
}
```

`seedWriter` opens through production's own pragma shape
(`path+"?_txlock=immediate"`, `PRAGMA journal_mode = WAL|DELETE`) and its
`busy_timeout` comes from `dbBusyTimeoutMSec` via `LedgerReadOnlyDSN` — the probe
re-types no number.

## 7. Summary for the epic

| seam | selected access point | file:line |
|---|---|---|
| A — read the in-progress line | `Editor.Value().Text()`, from a foreign goroutine (published snapshot) | `internal/lineedit/editor.go:1172` → `internal/lineedit/value.go:124` (publish: `value.go:166`; invariant: `internal/lineedit/valueowner.go:5`) |
| B — one prompt-assembly site | the turn's outbound boundary | `cmd/agent/cli_chat.go:1731` |
| C — read-only retrieval | `MemorySearchHits` opened via `LedgerReadOnlyDSN` | `internal/tools/memory.go:1543` + `internal/tools/ledger_dsn.go:57` |

Zero product behaviour changed: this commit adds one document and its
`devdoc/DOC-INDEX.tsv` row. No `.go` file outside the deleted probe was touched.
