<!-- doc-class: historical -->
# devdoc: the LOCAL ONLY `## 本机策略（hosta）` section in GOPI.md

**Class of the change**: documentation only — one appended section at the end of
`GOPI.md`. No Go file changed.

## What was appended, and what it deliberately is NOT

An appended `## 本机策略（hosta）— LOCAL ONLY` section carrying four
operator-authorised local notes for this client host:

1. bugs are filed on peerbox (`gopi task submit …`), including the measured
   spelling trap (`--payload @FILE`; the attachment flag is **`--attachment`**,
   singular and repeatable — a wrong spelling prints usage and exits 0, so the
   `Task queued: <id>` line must be read back);
2. the provenance criterion for this host's `memory.db` — **file existence**,
   not timestamps and not paths (the two hosts spell `$HOME` identically);
3. known environment facts (xterm has no CJK font; the chat screen dump path;
   no `sqlite3` CLI; check the task ledger for a behaviour's design source
   before calling it a bug);
4. the authorisation itself, and the acceptance that an upstream refresh of
   `GOPI.md` may drop the section.

It is **not** a `Rule`: it carries no rule number, it is absent from the rule
index, and it claims no machine check. Rules in `GOPI.md` are Tier A — they are
upstream and gated; this section is host-local policy written by the operator's
decision (2026-10-04) for this machine only, and it says so in its own first
line. Nothing was removed or reworded: the diff is `43 insertions(+)` with zero
deletions, so no existing Rule text was touched (Rule 4).

## Why a section instead of a Rule

The two shapes have different costs. A Rule costs an upstream review, a
mechanical gate and a claim of generality the section cannot honestly make
("file bugs on peerbox" is true only for a client host). The section costs a few
thousand bytes re-injected every round — bounded, and the reason it is kept to
~1.5k runes of prose.

## Where it lands, and what it costs when it does not

If this branch is merged, the section is visible to any agent running the
checkout that contains it, including on peerbox. If a future upstream refresh of
`GOPI.md` replaces the file, the section disappears — which the section itself
declares acceptable, because it exists to save this host's repeated discovery,
not to bind anyone else.
