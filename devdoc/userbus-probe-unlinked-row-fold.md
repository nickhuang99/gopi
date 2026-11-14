<!-- doc-class: historical -->
# The unlinked-yet-bound socket was real; the /proc/net/unix LOOKUP was first-match

Task `01a12ca867027aa58ff3fbc3deac7741` · branch `task/01a12ca867027aa58ff3fbc3deac7741`

## The failure

`TestUserBusProbe_RealUnlinkedSocketIsDetected` — the POSITIVE CONTROL of the
user-bus discriminator (`cmd/agent/daemon_userbus_probe_test.go`) — failed on
this host:

```
the kernel must still hold the socket after the path was unlinked: {… bound:true listen:false inode:0 connectErrno:2}
the bound socket's inode must be unchanged by the unlink: before 17599423, after 0
```

The epic named two candidate causes: (a) the probe's `/proc/net/unix` field
indices are off for the row shape actually emitted, or (b) the kernel genuinely
keeps no pathname row for an unlinked socket, making the test's premise wrong.

**Neither is what happened.** The row survives the unlink, and the indices are
right — the LOOKUP was wrong.

## The measurement

An isolated probe (`~/.gopi/sessions/01a12ca8-…/probe/main.go`, raw output in
`unlinked-socket-row-evidence.md`, both on `/dev/shm` and on a real block-device
directory) dumps **every** row carrying the path at each step. Kernel
`6.8.0-138-generic`:

```
1. right after bind
    row#1 St="01" inode="17620230" 0000000000000000: 00000002 00000000 00010000 0001 01 17620230 /dev/shm/…/bus
2. one client connect
3. after that client closed
    row#1 St="02" inode="0"        0000000000000000: 00000002 00000000 00000000 0001 02     0 /dev/shm/…/bus   <- FIRST MATCH
    row#2 St="01" inode="17620230" 0000000000000000: 00000002 00000000 00010000 0001 01 17620230 /dev/shm/…/bus   <- the binding
4. after unlink (listener still open)
    row#1 St="02" inode="0"        (unchanged)
    row#2 St="01" inode="17620230" (unchanged)
5. stat → ENOENT;  dial → ENOENT
```

**One pathname, two rows.** The kernel's `unix_mkname` copies the path into
every AF_UNIX socket's address struct — a client's included, via `connect(2)` —
so one client `connect()` adds a SECOND row for the same path (St=`02`,
Inode `0`, the connected socket object), and the kernel emits it **first**. Both
survive the unlink, because the listener's socket object keeps its address.

`userBusKernelBinding` scanned the table and **returned at the first row whose
path matched**. During `observeUserBusPath`'s own `net.DialTimeout`, the probe's
client row was created; the next observation read that client row and reported
`listen:false, inode:0` for a socket that was bound and listening. The
observation is `stat` → `binding` → `connect`, so the client row already existed
before the unlink: the raw dump shows **two** rows on both sides of it.

### The field indices (why (a) was a red herring)

`proc(5)`'s header — `Num RefCount Protocol Flags Type St Inode Path` — is
zero-based and omits `Num`, but the row's field `[0]` is NOT a column: it is the
row's address, a 16-hex-digit label ending in `:`. So the columns land one
position to the RIGHT of their names, and anything testing "`fields[5]` is not
`01`" is ASTONISHINGLY CLOSE to the truth — the trap is elsewhere. Many
`/proc/net/unix` parsers deliberately drop `field[0]` and are then shifted by
one; the tell is **where the path lands**: `[7]` means the label was kept,
`[6]` means it was dropped. This tree had it right (`[5]` St, `[6]` Inode,
`[7]` Path), and the raw dump proves it.

## The fix

`userBusKernelBinding` now scans the **whole** table into rows and folds them
(`userBusFoldUnixRows`, a pure function): `bound` is set by the LISTENing row
wherever it sits, that row's inode is the reported one, and the row **count** is
kept as evidence. A first-match lookup is no longer possible.

Two further honest-reporting repairs came with it, both from reading the same
function:

- **unreadable ≠ absent.** An open/scan failure used to return "no binding",
  which reads as "nothing is bound" — a state change signalled by a read error,
  turned into a possible fault verdict (Rule 15). It is now an `error`, carried
  as `bindingErr` and classified `unknown`.
- the fixture's socket path is checked against `sun_path` (108 bytes) and the
  bind's `EINVAL` is distinguished from the filesystem's, because on a host
  whose scratch root is itself a tmpfs the suite's nested names push the path
  over the limit (see below).

## A second, separate defect the acceptance run exposed

`GOPI_TEST_TMPDIR=/run/user/1000/gopi-ubprobe` (the "real /run/user path" the
acceptance names) failed the case at `bind`:

```
bind a unix socket: listen unix /run/user/1000/gopi-ubprobe/fixture-suite-cmd-agent-run-/suite-cmd-agent-run-3105791120/ubdiag2647389674/bus: bind: invalid argument
```

That path is **108 bytes** — exactly `sun_path` including its terminator — and
`bind(2)` answers `EINVAL`, not `ENAMETOOLONG`, when it does not fit. The
fixture now uses a short pinned directory name (`ubd`), asserts the path fits
before binding, and on a genuine `EINVAL` from bind **skips with the measurement
in the message** (a filesystem that cannot hold a socket node is a statement
about the filesystem, not about the predicate). On this host, on that root, it
now PASSES rather than skips.

## Verification

- **Build:** `go build ./...` → OK.
- **Tests:** `go test -run TestUserBus ./cmd/agent/` → `ok` (18 cases PASS,
  0 fail). The same set with the suite root on a real `/run/user/1000` tmpfs →
  `ok` (the regression that failed at baseline now passes there).
  `make test-budget PKGS='./cmd/agent'` → see the task report.
- **Ran:** the surface's own acceptance invocation, on both roots:

  ```
  GOPI_TEST_TMPDIR=/dev/shm/…  go test -run TestUserBusProbe_RealUnlinkedSocketIsDetected ./cmd/agent/   → PASS
  GOPI_TEST_TMPDIR=/run/user/1000/gopi-ubprobe go test -run TestUserBusProbe ./cmd/agent/               → PASS
  ```

  and the baseline `HEAD` (a detached worktree at `b7e5eddd6`, unmodified) on
  the `/run/user/1000` root → **FAIL** on the same case, which is what makes the
  pass attributable to this change rather than to the root.
- **Result:** the positive control detects the unlinked-yet-bound state for
  real: `bound:true listen:true inode:<unchanged>` with `bindingRows ≥ 2` after
  the unlink, and classify → `bus-path-unlinked`.
