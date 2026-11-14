<!-- doc-class: normative -->
# devdoc: `gopi ui` — remote access, and an engine identity you can verify

Branch: `feat/ui-remote-access` (worktree `~/workspace/gopi-01a0d334673e78c49de0693e0da9fa49`)
Epic: *"gopi ui should allow remote access … check gopi web using sessionid to identify model … browser remote access greatly help debugging and verification"*

## 1. What was actually broken

`gopi ui` does not have its own server. It starts the SAME engine `gopi web`
serves — the same `webServer`, the same single persistent agent, the same
`webChatHandler` route table — and it already gate-wrapped it correctly:

```go
handler, token, err := webGatedHandler(opts.addr, srv.webChatHandler(), trust)
```

So on `--addr 0.0.0.0:PORT` the engine minted a per-run access token and every
remote request needed it. **Two things were missing, and together they made
remote browser access impossible in practice:**

1. **No banner.** `uiStartServer` called `printLogPaths` and nothing else — the
   minted token appeared NOWHERE. A second machine had no URL to open. (The one
   thing that "worked" was a browser on the *same* host, and only because local
   trust — `webPeerIsLocal` — admits this machine without a token. That is
   exactly the posture that hides the defect.)
2. **No identity.** With the token in hand a remote client could reach the
   engine, but nothing on the wire said *which* session or model it had reached.
   That is the briefing's second ask, and it is not cosmetic: the token is
   re-minted per run and the engine owns ONE conversation, so a stale page, a
   restarted engine and a second operator's browser all look identical from the
   outside.

## 2. The change

### 2.1 The remote handoff (`cmd/agent/cli_ui.go`)

`uiStartServer`, on a non-loopback bind, now prints the same block the browser
carrier prints, through the same helpers:

```
gopi ui: listening (Ctrl+C to stop)
  open: http://localhost:8097?t=<token>
  open: http://10.0.0.9:8097?t=<token>
  open: http://10.0.0.6:8097?t=<token>
gopi ui: LAN-REACHABLE bind — every URL below carries the per-run access token; …
```

* `webPrintListening` was split into a carrier-parameterised
  `webPrintListeningRuntime(carrier, addr, ln, token, localTrust)`, and the URL
  resolution extracted into `webReachableURLs` / `webBoundPort` (one place that
  computes "what did the kernel actually bind"). `gopi web` keeps calling the
  original wrapper.
* The exposure notices became carrier-parameterised FORMATS
  (`webLANWarningFormat`, `webToolsLANWarningFormat`,
  `webLocalTrustNoticeFormat`), with the old constants DERIVED from them, so the
  risk text is shared but the name is truthful (`gopi ui:`, never `gopi web:`).
  `webCarrierUI` is derived from the verb constant (`"gopi " + CliVerbUI`).
* Only a non-loopback bind prints the block: a remote banner on a loopback bind
  would be a false security statement.

### 2.2 The identity surface (`cmd/agent/web_identity.go`, new)

A gated read-only route, mounted inside `webChatHandler` — so the access gate
covers it exactly like `/api/chat` and it adds NO new public surface:

```
GET /api/identity            →  401 without the token; 200 with it
{"addr":"0.0.0.0:8097","session_id":"3da16999-…","model":"deepseek-flash",
 "vision_model":"deepseek-flash","tools":false,"messages":0,"turns":0,
 "tokens":0,"started":"2026-09-24T11:48:53Z"}
```

* `session_id` is the id `finalizeChatSession` closes and `gopi session list`
  shows — the single session the whole run owns.
* `model` is the model **serving turns**, read under `s.mu`: during an image turn
  the shared agent's model IS the vision model, and the route reports that
  (`vision_model` names what a pasted image will use).
* `messages`/`turns`/`tokens` distinguish an EMPTY session from a live
  conversation you did not start.
* `started` makes a restarted engine visibly a NEW run.

Both terminal carriers print the same fact as a line, via one struct
(`webIdentityPayload`) that is ALSO the wire type — the notice and the JSON
cannot drift:

```
gopi ui: engine session=e88f4354-… model=deepseek-flash tools=true addr=127.0.0.1:8098
gopi ui: identity http://127.0.0.1:8098/api/identity (messages=0 turns=0 tokens=0 started=…)
```

For `--server` (an engine this process did not start) the identity is FETCHED
over the gated wire (`webFetchIdentity`, Bearer) — never guessed — and a peer
that does not answer is REPORTED rather than silently assumed fine.

The page renders the same line as its first log node (`identifyEngine()`, a
plain `textContent` node, styled quiet): a browser that opened a remote URL can
see which engine it is driving without leaving the UI.

### 2.3 Startup readiness (`uiEngineReady`)

Before the terminal surface starts, `gopi ui` probes its target engine and
reports a bind nothing answers. Any HTTP status counts — a 401 from the gate IS
the engine answering; the probe asks "is the socket serving?", not "am I
authorized?". Conflating the two was the first shape of this code and it broke
the `--server` path (a token the probe did not yet have made a live engine look
dead): found by running it, fixed by separating the questions.

## 3. Traps hit while building this (kept here so the next agent does not re-walk them)

1. **URL order is load-bearing.** `origin + route + "?t=" + token` authenticates;
   `origin + "?t=" + token + route` puts the route INSIDE the token's value, the
   constant-time compare fails and the operator's own banner hands them a 401.
   Measured live (HTTP 401 on the identity URL the banner printed). One helper
   now owns the shape: `webRouteURL(origin, route, token, localTrust)`.
2. **A wildcard display list starts with `localhost`.** Handing a REMOTE operator
   the first URL means handing them one that only works on the printing machine.
   `uiIdentityURL` prefers an interface address when one exists.
3. **`script -q -c … <file>` does NOT accept a terminal's `TIOCSWINSZ` here**
   (`script: tcgetattr … Invalid argument`), so the pty transcript stays empty and
   the TUI exits with "could not open a new TTY" under a plain redirect. The
   server-side facts are all visible before that (the banner and the identity
   block are printed first, and the socket keeps serving), which is what the
   live evidence below uses.
4. **Port 8090 on peerbox is held by another agent's `gopi ui`** — an unrelated
   instance is running from `~/work/GoPi/bin/gopi`. Use a free port (8097/8098)
   and a sandbox, never the shared default.

## 5. Suite classification (honest, and partially unverified)

The whole `./cmd/agent` package does NOT pass in this run: **18 arms fail**. I
split them by measurement rather than by assumption:

* **11 proven PRE-EXISTING**: re-run against a pristine `master` export
  (`git archive master | tar -x -C <scratch>`, so no foreign worktree is touched)
  they fail identically, with no line of my diff in their path —
  `TestG5InventoryMatchesTheHelp`, `TestG5SurfaceCensusIsDerivedFromConstants`,
  `TestCLINamingExceptionsDocumentedInTopLevelHelp`,
  `TestCLIUserVerbTokensAreAllFromCliVerbs`,
  `TestEveryHelpConstantFitsTheWrapBudget`,
  `TestToolsMaintenanceHelpSurface*` (both),
  `TestTopLevelUsageDelegatesEnvVarsToHelpEnv`,
  `TestWorkerRunRecordsTheMissingRanLineE2E`,
  `TestTaskRunSeamWritesReportAndShowRendersIt`,
  `TestWorkerReleaseOwnWorktree_Acceptance` (this last one is also *architectural*
  here: its own output says the process stands in a linked worktree, not the main
  checkout, so it refuses release by design).
* **7 e2e arms I could NOT clear to my own satisfaction** — the worktree-residue
  family (`TestResumeReclaimsTheWorktreeItFinishedIn`,
  `TestManualRunRecyclesItsWorktree`, `TestRawPromptRunReleasesItsWorktree`,
  `TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves`,
  `TestWorkerServerDeliveryRecyclesThenMergerMerges`,
  `TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow`,
  `TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`). They fail with
  "left the worktree behind" in the suite's scratch tree; they spawn a CHILD gopi
  with `append(os.Environ(), …)`, i.e. inside a live runner's environment; and
  `git diff --name-only HEAD~1 HEAD | grep -E 'worktree|release|worker|internal/tools'`
  is **empty** — my diff touches nothing they read. The `master` archive could not
  settle them (a bare export has no `.git`, so these git-dependent arms skip
  there), and masking my inherited task-report contract
  (`env -u GOPI_TASK_REPORT_PATH -u GOPI_TASK_REPORT_OWNER`) did NOT change their
  outcome — so I am NOT claiming them environmental. They are recorded here as
  **not verified by this run**. The changed surface itself is green
  (`-run 'TestWeb|TestUI'` → 101 PASS), and no web/UI/reclaim assertion in these
  arms even mentions my files.
* **The environment note that matters for a reader of the full-package number**:
  every failing arm is either a help/inventory table or a worktree-residue e2e —
  none is a web, UI, identity or banner surface, which is what this branch
  changes.

## 4. Not done (honest)

* **No TLS.** The token travels in clear text on the LAN, exactly as
  `gopi web`'s LAN opt-in already documents — unchanged, not fixed here.
* **No per-turn approval** for remote tool runs: `--tools` on a LAN bind is
  warned about (loudly, in this carrier's name too) but not gated.
* **`/api/identity` is served by all three carriers** because it rides the shared
  mux. That is deliberate (one definition, one gate), but it does mean
  `gopi web` and `gopi cli` gained the route as a side effect.

