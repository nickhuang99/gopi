<!-- doc-class: historical -->
# Proof: the pointer is part of the write path

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Round 1: this document was produced by the production writer (tools.Write —
the function behind gopi's `write` tool) and it teaches a worker-launch
shape: `gopi worker`.

After the write, `grep WORKER-OWNERSHIP` shows the canonical pointer line
was added by the writer itself — no hand editing.

Round 2: this paragraph arrived through the `edit` tool, which introduces a
second launch shape (`gopi task run <id>`) into an already-annotated file;
the pointer must stay exactly once.
