<!-- doc-class: roadmap -->

# innovation-showcase.md (7 sections complete; mirrors the Chinese edition section-for-section)

> Target path: `/home/nick/work/github/gopi/docs/innovation-showcase.md`
> Section-for-section with the Chinese edition; output blocks byte-identical
> (same placeholders, same field order). See the Chinese file's notes on why
> section 2 shows the shape rather than the full byte stream.

# What gopi can do that most harnesses do not

Every command below was run on a real machine; the output is pasted unchanged.
Where a command has not been run, the entry says so — this page is a record, not
a brochure.

## 1. Token accounting that reconciles against the bill

gopi counts every request locally and compares it against the number the provider
actually reported. The `diff` column is the argument: it is `+0` on every request,
so the cost you see is the cost you are charged.

```console
$ gopi tool tokens --session <SESSION-ID>
line     msgs   tools   ours     server    diff
2        1      0       52881    52881     +0
```

Exit code 0; `requests: 1 exactly equal: 1`.

**Boundaries (found by running it):** the session must be a *complete* one — it
needs both `session.jsonl` and a `raw.log` carrying `input_tokens`. An empty
session, or one with no recorded requests, prints nothing useful. Size only
decides *speed*, not correctness: a 190 MB `raw.log` ran past a two-minute cap
and was killed by timeout.

## 2. Two independent recordings of a run

`screen-replay` writes back the bytes a session actually rendered; `--verify`
compares two recordings frame by frame and reports the first place they differ.
The other half is `raw.log`: every request and response verbatim, with the token
fields broken out.

```console
$ gopi screen-replay <A> | head -c 400 | cat -v
^[?2004h^[?u^[6n^[c^[]11;?^\P+q636F6C6F7273^\P+q524742^[1;21r^[1;1H^[2J
^[22;1H^[2K^[1m^[38;2;255;248;220myou>^[0m^[5C^[21;1H^[2K^[21;1H
```

The output is the raw bytes recorded from a real session (the 70 MB one), written
to stdout in recorded order: no canvas is built, no line is re-wrapped, no colour
is rewritten. So it can be piped, hashed, or asserted byte by byte. Diagnostics go
to stderr and never pollute stdout.

```console
$ gopi screen-replay <A> --verify <B>
gopi screen-replay <SESSION-ID|FILE> --verify
  A: ~/.gopi/sessions/<A>/<A>.screen.stream — frames=826951 bytes=68333537
  B: ~/.gopi/sessions/<B>/<B>.screen.stream — frames=1025200 bytes=47377413
differ: frame 4 — payload bytes differ (7 byte(s) each; first differing byte at 4)
  A: frame 4 at byte offset 188 — "\x1b[1;21r"
  B: frame 4 at byte offset 188 — "\x1b[1;19r"
```

It localizes the difference to **frame 4, byte offset 188** (two recordings of
two different sessions have different scroll-region row counts: 21 vs 19). This
turns "supports diff" from a claim into a recomputable coordinate.

**Boundaries:** a `.screen.stream` with `frames=0` is an empty recording — comparing
one yields nothing. Use a recording from a real session.

## 3. Two agents, one repository — a mailbox, not a chat room

Each session has its own inbox file, addressed by session id. There is no shared
context and no shared memory, so one agent's mistake cannot silently become the
other's premise. **The receipt is written by the reader**, which is why "delivered"
cannot be forged by the sender. Messages land at a round boundary, so a turn in
flight is never interrupted. `--peer <alias>` carries the same mechanism to
another machine.

```console
$ gopi notify --session <SESSION-ID> --message "$(cat /tmp/msg.md)"
session dir: ~/.gopi/sessions/<SESSION-ID> (resolved as its session directory name)
the session takes it at its next round boundary (an IDLE console wakes for it and
opens the next turn by itself; a turn already in flight is never interrupted)
```

This is not a feature nobody uses. Two agents working on this repository each
overturned one of the other's conclusions in a single evening: one reported a
dirty worktree of 7485 lines that measured 0 when checked; the other found 33
places where a machine's internal host name had been written into code the first
had passed as clean. Neither was a courtesy review — both changed what got shipped.

See [the worked example](../examples/session-collaboration/).

## 4. Three tiers of memory, prefetched before you ask

Distilled lessons, reusable procedures, and one searchable note per round of every
past run. Retrieval is automatic — the relevant few are already in your context
before you ask.

```console
$ gopi memory search "clone 通道 push 被拒"
now=<TIMESTAMP>
[distilled]
#<ID> clone-channel 报「carries no commit to deliver」时如何判「已落地」还是「从没提交」
      tier=warm conf=1.00 lesson=lesson stored=2026-10-28 age=26d
      sources=row <TASK-ID> tokens=3 fb=0.97(32)
Q: gopi worker 的交付腿说 "X carries no commit to deliver..." —— 我该补提交，还是本来就没什么可交？
```

(the stored lesson is in Chinese; left as-is)

The hit's `Q` is exactly the question that really came up at the time. The three
fields `tier` / `conf` / `fb` are **vote-driven**: `fb=0.97(32)` means 32 pieces of
feedback pushed it to a confidence of 0.97. Bad entries sink, good ones rise.

> In the block above, `<ID>` `<TASK-ID>` `<TIMESTAMP>` are placeholders: the raw
> output carried a host name and a real task id, replaced here by placeholders.
> Field names, field values and the hit's content are unchanged.

**Boundaries, and this one matters:** a prefetched pair survives only if it is
voted on. Measured: eight pairs that received no vote each survived exactly one
round; a pair voted `useful` was still present five rounds later. Automatic
retrieval is not a free lunch — it is a queue that a vote keeps alive.

## 5. A note to yourself that survives compaction

`self-record.md` is written every round and reloaded after the context is
compacted, so a decision made 500 rounds ago is still there when the window that
held it is gone.

```console
$ wc -l ~/.gopi/sessions/<SESSION-ID>/self-record.md
87
$ head -c 200 ~/.gopi/sessions/<SESSION-ID>/self-record.md
t1 <第一条自记的正文 — 那一轮做完了什么>
t2 <第二条 — 这一轮推翻了上一轮的哪个结论>
```

**One line per round**, prefixed by the round number. This one is 87 lines and
70 KB — everything a real session decided over dozens of rounds. (The stored
lessons are in Chinese; left as-is.)

This is not a log: **the next round's prompt reads it back**, so it changes
behaviour. Compaction deletes the raw conversation, not this line-by-line record of
conclusions.

> The two lines' bodies are summarized placeholders: the originals were internal
> working notes from that run. The line count, byte count and format
> (`<round> <body>`) are real.

## 6. A UI, and a cell grid underneath it

`gopi web` serves a local browser chat. `gopi ui_state` reads a live screen back as
a **cell grid** carrying colour and formatting per cell range — no pixels, no
vision model, so what the agent "sees" costs tokens proportional to text.

```console
$ gopi ui_state --target chat
grid_begin count=25
000| … the answer band, wrapped as it is drawn …
023|you>
024| … status line: turn, tool, context usage, cache hit rate …
lines_end
```

The returned JSON carries its own one-line definition, which is this entry's
whole point:

```json
{"tool":"ui_state",
 "what_this_is":"an interpreted cell grid (not bytes, not pixels)",
 "state":"live"}
```

**25 lines, 60 cell ranges**, each with its own attributes. The same data can be
read by a program (`--json`) or drawn for a human (`--target chat`).

> The grid's content above is a summarized placeholder: the raw output was the
> real conversation on screen. Structure and field names are unchanged.

## 7. One static binary, nothing to install

No Node, no Python, no Docker, no CGO. Run `file` on a **release** binary:

```console
$ curl -sL -o gopi https://github.com/nickhuang99/gopi/releases/download/v0.5.3/gopi_v0.5.3_linux_amd64
$ stat -c%s gopi
65409208
$ sha256sum gopi
9fa046ce6045948971a5632a115ca8ede824782daaa4f4400d9fa0f364bf342a
$ file gopi
gopi: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, BuildID[sha1]=ba846563bd697a92c2f81beb245fea549515d2e0, stripped
```

`statically linked` is the word. The sha256 matches the release's `.sha256` file
character for character.

**A trap you will really hit:** with a wrong URL, GitHub does **not** fail loudly —
`curl` still exits 0 and the body is a 9-byte `Not Found`. Check the size or the
sha256 before trusting the download.

> The macOS build is cross-compiled and has **never been run**. "It compiles" is
> not "it works".
