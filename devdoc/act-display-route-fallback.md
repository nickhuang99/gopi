<!-- doc-class: historical -->
# Display route resolution — why `gopi act info` said "no such file or directory"

**Status: normative** — the standing claim is "an X11 display a caller names is
reached over its OWN transport first, and over this host's OTHER local transport
as a fallback; a display no route reaches fails with a message that names every
route tried". Threat/scope: `internal/shot`'s display resolution, and everything
above it (`gopi act`, `screen_capture`, `gopi shot`-live, the worker's display
preflight).

## 1. The defect, as reported and as measured

Report: `gopi act info` → no such file. Measured on **peerbox**, 2026-09-20, with
the session's real `$DISPLAY`:

```
$ DISPLAY=localhost:17.0 gopi act info
gopi act: connect to X display localhost:17.0 (unix /tmp/.X11-unix/X17):
          dial unix /tmp/.X11-unix/X17: connect: no such file or directory
```

The same host, same second, had **live** displays — `:1` (1600x900) and `:98`
(Xvfb 1024x768) — and TCP listeners on 6010–6018. So "no X11 display" was false;
what was missing was any willingness to look anywhere but one socket path.

### Root cause (5-why)

1. *Symptom*: ENOENT on `/tmp/.X11-unix/X17`.
2. *Mechanism*: `dialX11` mapped a `localhost` host **unconditionally** to the
   unix socket (`filepath.Join(x11UnixSocketDir, "X"+N)`), so `localhost:17`
   dialled a LOCAL socket — a path the string never named.
3. *Mechanism*: it dialled exactly ONE route. There was no second candidate, so
   one dead socket was the whole answer — no TCP attempt, and no attempt at the
   same display number over the transport this host actually serves.
4. *Why it read as a broken feature*: the failure surfaced as a bare dial error
   (`connect: … no such file or directory`), with nothing naming the remedy, so
   the operator could not tell "your DISPLAY is stale" from "gopi's GUI half is
   broken". The stale `DISPLAY` came from an SSH forward that no longer exists
   (`devdoc/worker-display-ownership.md`: a detached process can NEVER
   re-acquire a display it did not inherit a working source for).
5. *Norm violated*: Rule 15 — a degraded capability was reported as a raw OS
   error instead of an announced, actionable failure; and the documented
   `localhost:N` route (`devdoc/worker-localhost-display.md` records TCP
   6000+N as the *reachability* path for `localhost:99.0`) was never
   implemented in the dialler.

Two independent bugs, one error message: **(a)** the `localhost` → TCP rule was
never honoured; **(b)** a dead route was never followed by the other local one.

## 2. The fix — routes, in order, with one loud failure

`internal/shot/x11.go` grows one resolution concept (`x11Route`) and one source
of route order (`x11Routes`):

| DISPLAY | primary route (tried first) | fallback route |
|---|---|---|
| `:N` / `unix:N` | unix socket `/tmp/.X11-unix/XN` | TCP `127.0.0.1:(6000+N)` |
| `localhost:N` / `127.0.0.1:N` | TCP `localhost:(6000+N)` | unix socket `/tmp/.X11-unix/XN` |
| *host*`:N` (any other host) | TCP `host:(6000+N)` | **none** — a remote host's local socket is not ours to invent |

* The primary route is the transport the DISPLAY string NAMES — unchanged
  behaviour for the case that always worked (`:N` over the local socket).
* The fallback is a **local** address only, which is what makes its shorter
  bound (`x11FallbackDialTimeout`, 1 s) honest: a dead local listener refuses at
  once, so the error path stays fast instead of paying the full dial timeout
  per route.
* A display reached by fallback **says so**: the name echoed in results is
  `<display>@fallback` (`x11RouteLabelFallback`). A route substitution the
  caller cannot see is exactly the silent degradation Rule 15 forbids.
* When no route answers, the error lists every route with its OWN cause (a dead
  socket reads `no such file or directory`; a live server refusing reads
  `connection refused`) and appends this host's live displays — capped at
  `x11HintMaxDisplays` (4) and probed through the same resolver with the hint
  disabled, so a hint can never recurse into a hint:

```
$ DISPLAY=:0 gopi act info
gopi act: connect to X display :0: no route answered; unix /tmp/.X11-unix/X0:
dial unix /tmp/.X11-unix/X0: connect: connection refused; tcp 127.0.0.1:6000:
dial tcp 127.0.0.1:6000: connect: connection refused (live display(s) on this
host: :1, :98 — pass --display <D>, or export DISPLAY=<D>)
```

The same resolution is behind every entry point that matters: `act`/
`screen_capture` (`dialX11`), `shot.ProbeDisplay` (the worker's
`display preflight`), and `OpenXActuator` — so the worker's announcement cannot
disagree with the capability the tools actually have.

### The worker announcement, corrected with it

`cmd/agent/worker_display_preflight.go` measured "does the inherited DISPLAY
name resolve". After this change it measures "does ANY local route reach that
display number" — a strictly better question, because a dead `localhost:N` whose
number the host serves locally is now **reachable and announced reachable**
(true: every GUI tool call will reach it). The unreachable text states the
routes-tried fact instead of the (no longer true) promise that the process can
never reach a display by another route.

## 3. Why this is not "ignoring DISPLAY" or a new knob

* **No env contract** (Rule 2/21): `$DISPLAY` is still read in exactly one place
  (`captureDisplayName`), with `--display` ahead of it. The fallback is a
  *route* decision inside the resolver, never a second env read, and no caller
  can configure it.
* **No fabricated remote routes**: a remote host gets one route, as before.
* **The caller is still told which display answered** — with the route badge.
* **Rejected alternative: "just tell the user to fix DISPLAY".** The
  `worker-display-ownership` doctrine (announce, never refuse; the operator owns
  the launcher's env) is untouched — this is not a refusal. But the doctrine's
  own remedy line ("restart from a shell whose DISPLAY is the durable local
  display") is exactly what a *fallback* achieves without a restart: the same
  display NUMBER, reached over the transport that is alive. The measured host
  proves the case (localhost:17 dead, :1/:98 live).
* **Rejected alternative: try every display number on the host.** Silent
  substitution (surprising) and unbounded. The fallback stays at the SAME
  display number, and the hint only ever NAMES the alternatives.

## 4. Verification (measured, this host)

| check | result |
|---|---|
| `DISPLAY=localhost:17.0 gopi act info` (pre-fix) | exit 1, `connect: … no such file or directory` |
| `DISPLAY=localhost:17.0 gopi act info` (post-fix) | exit 0, `1920x1080`, pointer/focus read back |
| `DISPLAY=:1 gopi act info` | exit 0, `1600x900` — the previously-working path is unchanged |
| `DISPLAY=localhost:1.0` (no TCP listener for :1) | exit 0, `"display":"localhost:1@fallback"`, `1600x900` — the fallback is exercised AND badged |
| `DISPLAY=:0` (socket present, server refusing) | exit 1, both routes named + live displays |
| `gopi screen_capture` on the stale name | exit 0, PNG 1920x1080, `sha256_16 b706efa2128fbbbe`, read back as a real desktop |
| `go test ./internal/shot/` | ok (32 s) |
| red-proof (fallback removed, `x11Routes(d)[:1]`) | 4 new tests FAIL (`did not fall back …`), restored → ok |

## 5. Honest limits

* `localhost:N` is still tried over TCP FIRST (its named transport). A host that
  serves `:N` only locally pays one refused TCP connect (bounded at 1 s) before
  the socket route answers. A `--netpath`-style preference knob is deliberately
  NOT added: it would be a new contract for a rare case, and the fallback's cost
  is one refused local connect.
* The live-display hint probes at most `x11HintMaxDisplays` displays and stops at
  the first cap-ful — on a host with many X servers the list is a hint, not a
  complete inventory.
* The hint's directory scan is best-effort by design (an unreadable
  `/tmp/.X11-unix` contributes nothing); the failure it decorates is already
  decided and is never changed by the hint.
