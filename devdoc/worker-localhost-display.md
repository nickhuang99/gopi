<!-- doc-class: normative -->
# Constant record: `workerLocalDisplay` (`Xvfb :99`) and `workerForwardedDisplayShape` (`localhost:N`)

## The literal before the constant

The worker's DISPLAY announcement (epic worker-spawn-doors, GAP 1) printed its
remedy sentence with two inline display literals:

```
"durable LOCAL display (Xvfb :99 on this host), not an SSH forward (localhost:N)"
```

`"Xvfb :99"` and `"localhost:N"` are MEANING-CARRYING strings in the sense of
Rule 1b: they are a display NAMESPACE (the `:N` suffix is the display number the
X11 protocol resolves to a socket), not prose. They name the same thing the docs
name, the tests assert on, and `internal/shot`'s display grammar parses.
Deviations checked before declaring the constant (false-positive discipline):

- `Xvfb :99` in shell/Markdown — a COMMAND or prose, no execution path.
- `:99` in `internal/shot/*_test.go` — a display argument in a test, not a Go
  string literal of the constant's value.

## The constant

```go
const (
	workerLocalDisplay          = "Xvfb :99"
	workerForwardedDisplayShape = "localhost:N"
)
```

`cmd/agent/worker_display_preflight.go` is the ONE holder of both values; the
announcement and any test read the constants.

## `workerForwardedDisplayShape` — why it is a constant and not prose

`"localhost:N"` looks like prose, but it is the shape of a display STRING the
parser in `internal/shot` accepts: `localhost:11.0`, `localhost:15.0` and
`localhost:99.0` are all instances of it, they are what the operator's SSH
forwards put in `DISPLAY`, and the display-ownership docs describe exactly this
shape. Naming it once means the announcement text, the doc and any future
classifier (`isForwardedShape(display)`) agree by construction — the Rule 14
reason, applied inside a single package.

The `N` is deliberately the display-number placeholder rather than a number:
the dangerous property is "a `localhost` display reaching a forward that may
have died", not any particular display number. `localhost:99.0` is
INNOCENT in practice (it reaches the local Xvfb over TCP) while `localhost:15.0`
is DEAD — once the forward's listener exits, nothing listens on 6015 and the
socket file `/tmp/.X11-unix/X15` is gone, so the dial fails and the detached
worker fails every capture forever. The shape, not the number, is the unit.

## The measured paradox the constants encode

On this host `localhost:99.0` and `:99` answer to the SAME Xvfb display: the
`localhost` form goes over TCP (port 6000+99) while the local form goes through
`/tmp/.X11-unix/X99`, and both land on the same server. `shot.ProbeDisplay`
therefore reports

- `DISPLAY=:99` → reachable,
- `DISPLAY=localhost:99.0` → `X99` socket looked up in `$HOME/.X11-unix/…`, not
  found, dial falls back to TCP `localhost:6099` → reachable.

That is why the announcement's remedy (`Xvfb :99`) and its warning
(`localhost:N`) are the same display spelled two ways: the DANGER is not the
`localhost` spelling, it is a `localhost:N` forward whose forwarding process is
gone (the socket exists only while the forward lives). The decision record
`devdoc/worker-display-ownership.md` carries the measured table; this file is
only the constant's provenance.
