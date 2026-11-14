<!-- doc-class: historical -->
# The X11 Xauthority Entry That Was Never Tried — `screen_capture` working in one ssh session and failing in another

Date: 2026-09-22 · Task: `01a0c51a-b329-7ea6-a215-6e64d092f37d` · Branch: `fix/x11-xauthority-candidates`

## The report, and why it was not a flake

The operator's observation, verbatim in substance:

- `screen_capture` **always worked** in one ssh session and **always failed** in
  another, on the SAME host (peerbox), with the same binary.
- Neither session used `ssh -X`, so the forwarding itself was not the variable.
- The behaviour was **stable across exit + reconnect** — same session, same
  outcome, every time. Not random.
- The same experiment on HP worked in BOTH sessions.

A stable per-session outcome is the signature of a value that is read from the
session's environment and never re-read. The failing session reported
`DISPLAY=localhost:11.0` and the tool answered:

```
X11 connection rejected because of wrong authentication
```

## Root cause (measured, not inferred)

`internal/shot/x11.go`'s `readXAuthority` returned the **first** Xauthority entry
whose number matched the display — and, failing that, the first Internet-family
entry. The comment even recorded the intent: *"a Local entry wins over an
Internet one"*. What it never did was consider that an Xauthority file holds
**several** entries for the same display **number**, distinguished only by the
address spelling they came from, and that only the server knows which one its
forward holds.

The failing session's file (measured on peerbox, `~/.Xauthority`, 16 entries) held:

```
fam=256 addr=hostb-Laptop num=11   e586a675…      ← offered FIRST, refused
fam=256 addr=peerbox         num=11   07b96b1b…      ← the cookie :11 actually holds
```

The foreign-client-host spelling sat first in the file, so it was offered first,
the live sshd forward on `localhost:11.0` rejected it, and the capture died — in
that session, every time, for as long as that file kept its order. The succeeding
session's `DISPLAY=localhost:16.0` had only one entry, which happened to be the
right one.

Protocol-level confirmation (byte-exact, no `xdpyinfo` hand-waving):

```
6011 cookie hostb-Laptop(#4)  -> EOF(closed)      ← refused
6011 cookie localhost(#6)       -> EOF(closed)      ← also refused
6011 cookie peerbox(#15)          -> SUCCESS(1)       ← the live cookie
```

The forward answers a wrong cookie by **closing the connection with no reply
byte at all** (EOF), while a refused *setup* is answered with a protocol FAILED
frame carrying the server's reason string. Both shapes had to be handled.

### Why HP looked better

HP is a **client**, not the server: sshd's own `xauth add` writes the localhost
spelling per session, and a client host does not accumulate a second, foreign
spelling for the same number the way peerbox (a host that has been *both* the ssh
client and the ssh server for years) does. Fewer candidate entries, no collision
— so both sessions worked. The defect is a collision, and HP had no collision to
have.

### Why this is not "ssh -X broken"

Everything downstream was correct: the socket, the route, the cookie the file
carried for `peerbox:11`. One credential of several was chosen by file order rather
than by offering them and letting the server say. All eight live forwarded
displays on peerbox (`:10`–`:17`) answer fine with the right cookie; before the fix
the **cwd-dependent `DISPLAY` alone** decided the outcome.

## The fix

`internal/shot/x11.go`:

1. **`readXAuthorityEntries`** replaces the single-entry lookup. It collects
   EVERY `MIT-MAGIC-COOKIE-1` entry for the display number and orders them by how
   likely each is the one the server holds:
   1. this host's own spellings (short hostname / FQDN label — the cookie of a
      display that lives on this machine), Local family;
   2. the `localhost:N` spelling sshd's own `xauth add` writes for every forward;
   3. every other Local entry with the same number (a foreign client host's
      spelling — tried last, because it usually belongs to someone else's
      session);
   4. the Internet-family entries (empty address) as a final fallback.
   The list is capped by `x11AuthMaxCandidates` (8) so a hand-maintained file
   cannot turn a refused display into an unbounded number of round trips.
   `readXAuthority` is kept as the head of that one order — never a second lookup.
2. **`dialX11Routes` tries the candidates in that order**, re-dialling only for
   the failures a different credential can fix (`x11AuthRetryable`): a server
   refusal frame, a "different auth protocol" demand, or the silent EOF a forward
   sends a wrong cookie. A dial error, a bad protocol version, a byte-order
   mismatch and a read timeout are NOT retried — another cookie cannot fix them,
   and retrying would only multiply the wait.
3. **A refusal is no longer a guess.** `x11SetupRefusalError` reads the reason
   string out of the FAILED setup reply and reports it with the credential that
   was offered:
   `X display localhost:11.0 refused the connection: wrong authentication (offered hostb-Laptop:11)`.
   The previous wording ("see the setup failure reason frame — check DISPLAY and
   Xauthority") told the operator to go read a frame the client never read.
4. **A substitution is visible** (Rule 15): when a capture succeeds on anything
   but the first candidate, the display name it echoes carries `@auth:<address>`
   — the same discipline as the existing `@fallback` route badge. A capture that
   needed the third cookie can never be mistaken for one that needed the first.

No new dependency, no new tool (Rule 3): stdlib only, the display/auth grammar
stays in `internal/shot` (Rule 2).

## Verification

- Real invocations, both binaries, on the operator's live `~/.Xauthority`
  (Rule 31 — the surface actually run, not a unit test):
  `gopi screen_capture --display localhost:11.0 --out …` → OLD: `connect to X
  display localhost:11.0: no route answered; tcp localhost:6011: setup reply:
  EOF`; NEW: PNG 1920x1080x24, `sha256_16` returned.
- All eight live forwarded displays `:10`–`:17` now capture (NEW binary).
- A real forward (:17) with the accepted cookie reachable only as the SECOND
  candidate: OLD fails, NEW returns
  `display":"localhost:17.0@auth:localhost"` — the retry loop exercised against a
  live sshd forward, not just the fake server.
- The regression suite `internal/shot` is green (40s).
- Red-proof: a temporary copy of the pre-fix lookup, run against the regression
  fixture, offers `ffeeddcc…` (refused) where the fixture's accepted cookie is
  `00112233…` — the fixture reproduces the defect, and the new lookup lists the
  accepted credential among its candidates.

## What is NOT claimed

- `ssh -X`/`ForwardX11` is not required for this path and was not changed
  (the operator's setup is plain ssh + sshd's own `X11Forwarding yes`).
- The HP-side "both sessions work" result is explained above as "no collision",
  not measured on HP in this run (this task ran on peerbox, where the defect
  reproduces).
- If NO candidate is accepted, the failure still names EOF for a forward-shaped
  refusal (the forward sends no protocol frame). The error now says how many
  credentials were offered in turn, which is the fact that makes it diagnosable;
  the reason string appears whenever the peer is a real server rather than a
  forwarder that drops the connection.
