<!-- doc-class: historical -->
# "Offline" means Go modules, not the network

Recorded from the superseded correction task `01a0a4d3` (a correction to
`01a0a4cf`, which had already completed before the correction could apply).

## The durable fact

When a GoPi task briefing says the box is "offline", it means **exactly one
thing**: `go.mod` resolves from the local module cache with no proxy, so a *new
third-party dependency cannot be fetched*. This is a dependency-vendoring
constraint.

It is **not** a network constraint. Web access is live and is a first-class
resource:

- `gopi web search "<query>"` and `gopi web fetch <url>` work, with per-source
  egress through the DO search-bot VPS (no VPN needed).
- `golang.org/x/term` is genuinely absent (not in `go.mod`/`go.sum`/module
  cache) — that is the module constraint, and it is why the CLI line-editor work
  uses `golang.org/x/sys/unix` instead (already a direct dependency).

## Why this is worth a file

Two different failures get conflated: "I cannot add a dependency" and "I cannot
reach the internet". Treating the first as the second makes agents skip web
research entirely and produce weaker work, while treating the second as the first
makes them try to `go get` something that will fail. Name which one is meant.
