package shot

// displayalloc.go — the ADDRESS RESOLUTION and SERVER SPAWN for a private X
// display: can this run get one, WHICH number, and if not, WHY.
//
// WHY THIS EXISTS. Filming a terminal used to be a shell pipeline whose display
// logic lived in scripts/replay/display.sh, and its failure path was a GUESS
// written as a conclusion ("no free X display in :62..:73 — a leaked server is
// holding them"). Three defects were measured in that one sentence (peerbox
// 2026-10-02): the range was the AUTHOR'S MACHINE written as a constant (this
// host held servers at :0 :1 :62 :85 :97 :240); Xvfb's OWN diagnostic for every
// attempt was already on disk at <scratch>/xvfb-<n>.log and was never read, so
// "not installed", "the number is taken" and "the scratch is unwritable" were
// reported as the same sentence; and nothing checked the scratch directory
// before the loop, so an unwritable scratch produced twelve dead servers and a
// message about leaked servers.
//
// So the three questions are asked in the only order that works — each one's
// precondition is the previous one's conclusion:
//
//	CheckFilmScratch   can this run write where it must?
//	CheckFilmServer    is the display server present and RUNNABLE?
//	StartFilmDisplay   which number — and when none, the CLASSIFICATION
//
// ONE DEFINITION (Rule 14): the number space is DERIVED from the sockets this
// machine actually holds under the protocol's fixed socket directory, never a
// hard-coded window; the classification is built by READING each attempt's own
// log. `gopi chat --film` (cmd/agent/chat_film.go) is the only caller.
//
// ZERO NEW DEPENDENCIES (Rule 35): Xvfb is the existing bare-system dependency,
// and this file only makes a MISSING one say so — it installs nothing, starts no
// daemon of its own and leaves nothing behind beyond the server it owns (which
// it kills again in Close).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

// The search shape and the probe cadence. Every one of these is a NAMED
// constant (Rule 1) and the search shape is a POLICY, not a measured value.
const (
	// displayPreferredNumber is tried FIRST, so a normal run is comparable
	// between invocations; the search then extends past it and finally wraps
	// BELOW it, so no machine shape is out of reach (this host holds a server at
	// :240, which a fixed twelve-number window starting at :62 cannot see).
	displayPreferredNumber = 62
	// displayScanMax is the only hard bound: the number space we are willing to
	// probe. It is the top of the ascending arm.
	displayScanMax = 300
	// displaySettleTimeout is how long a server may take to REGISTER ITS OWN
	// SOCKET before that attempt is judged failed. The attempt is verified
	// against the socket, never trusted after a delay.
	displaySettleTimeout = 3 * time.Second
	// displayPollStep is the granularity at which the socket is observed.
	displayPollStep = 100 * time.Millisecond
	// displayMaxTries bounds how many candidate numbers ONE run may attempt. It
	// bounds wall time (tries × settle), never the number space itself.
	displayMaxTries = 12
	// displayScreenW/H/Depth are the private screen's geometry. 24-bit is the
	// depth the X11 GetImage path reads back without a palette conversion.
	displayScreenW = 1280
	displayScreenH = 1024
	displayScreenD = 24
	// displayServerDefault is the virtual framebuffer, spelled once.
	displayServerDefault = "Xvfb"
	// displayScreenArg is Xvfb's geometry request; the value is composed from the
	// three geometry constants, so the flag spelling and the numbers cannot
	// drift apart.
	displayScreenArg   = "-screen"
	displayNoListenArg = "-nolisten"
	displayNoListenVal = "tcp"
	// displayServerProbeArg asks the server for its own usage. It MUST be -help:
	// Xvfb has no -version (measured 2026-10-02 — `Xvfb -version` exits 1 with
	// "Unrecognized option", so probing with it declares a WORKING server
	// broken). -help exits 0 and needs no display.
	displayServerProbeArg = "-help"
	// displayServerProbeTimeout bounds that probe: a hung binary must not hang
	// the preflight.
	displayServerProbeTimeout = 5 * time.Second
	// displayLogPrefix/displayLogSuffix name one attempt's log under the scratch
	// directory ("xvfb-62.log"). The path is PRINTED on failure, so a reader can
	// read the whole thing.
	displayLogPrefix = "xvfb-"
	displayLogSuffix = ".log"
	// displayLogQuoteLines is how many lines of an attempt's log the failure
	// report quotes.
	displayLogQuoteLines = 4
)

// The install hint is printed ONLY when the server is genuinely absent (Rule 35:
// we do not install it, we say so).
const displayServerInstallHint = "install it: apt-get install -y xvfb (Debian/Ubuntu), dnf install xorg-x11-server-Xvfb (Fedora/RHEL), apk add xvfb (Alpine)"

// The write probe's name: a file created and removed again, so the check is a
// real WRITE rather than a permission-bit answer (`[ -w ]` answers a question
// about the mode; a read-only mount, a full filesystem and an ACL all pass it or
// are irrelevant to it).
const displayWriteProbePrefix = ".film-write-probe."

// DisplayCause classifies WHY one attempt at a display number failed. It is
// DERIVED from the server's own log, so every value below is a measurement
// rather than a hypothesis about the machine.
type DisplayCause string

const (
	// DisplayCauseOccupied: the number is held by a running server (Xvfb:
	// "already active" / "already in use").
	DisplayCauseOccupied DisplayCause = "occupied"
	// DisplayCauseNotInstalled: the binary did not run (the shell's own
	// "command not found" is written to that very log).
	DisplayCauseNotInstalled DisplayCause = "not-installed"
	// DisplayCauseUnwritable: the socket or lock file could not be created —
	// a scratch or socket directory that cannot be written.
	DisplayCauseUnwritable DisplayCause = "unwritable"
	// DisplayCauseOther: the server exited for a reason this file does not name.
	// Its log is quoted UNCLASSIFIED rather than forced into a category.
	DisplayCauseOther DisplayCause = "other"
)

// The server's own words, quoted so the classification is testable and a future
// reader can re-derive it. These are Xvfb's/Xorg's messages, not ours.
var (
	displayLogOccupiedRE     = regexp.MustCompile(`already active|already in use|Server is already active`)
	displayLogNotInstalledRE = regexp.MustCompile(`command not found|not found|No such file or directory`)
	displayLogUnwritableRE   = regexp.MustCompile(`Could not create server lock file|Cannot establish any listening sockets|Permission denied|Read-only file system|Address already in use`)
)

// classifyDisplayLog maps one attempt's log text to a cause. It is PURE, so the
// classification is decided by a test rather than by a live machine — and the
// ORDER matters: "already active" is checked before the unwritable arm, because
// Xvfb's occupied message also mentions the lock file.
func classifyDisplayLog(log string) DisplayCause {
	switch {
	case displayLogOccupiedRE.MatchString(log):
		return DisplayCauseOccupied
	case displayLogNotInstalledRE.MatchString(log):
		return DisplayCauseNotInstalled
	case displayLogUnwritableRE.MatchString(log):
		return DisplayCauseUnwritable
	default:
		return DisplayCauseOther
	}
}

// DisplayAttempt is ONE tried number and what its own log said about it.
type DisplayAttempt struct {
	Number int
	Log    string
	Cause  DisplayCause
	Quote  []string
	// Empty says the log held nothing at all — the server did not reach its own
	// diagnostics, which is what a missing or unrunnable binary looks like. It is
	// reported separately so an empty log is never read as "no reason given".
	Empty bool
}

// DisplayUnavailableError is the classification: every statement in it is read
// back from a log on disk, and the per-attempt evidence travels with it.
type DisplayUnavailableError struct {
	Tried int
	Total int
	// SocketDir is the directory the candidate list was derived FROM, printed so
	// a reader can check the derivation on their own machine.
	SocketDir string
	// Counts is per-cause attempt counts (occupancy, not-installed, unwritable,
	// other) — the arithmetic the report is built from.
	Counts map[DisplayCause]int
	// Attempts is one entry per tried number, in the order they were tried.
	Attempts []DisplayAttempt
}

// Error renders the report. Every sentence names its evidence: the counts come
// from the attempts above, and the quotes are the server's own words.
func (e *DisplayUnavailableError) Error() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "no usable X display: tried %d of %d candidate number(s) (preferred :%d, then ascending, then below; socket dir %s)",
		e.Tried, e.Total, displayPreferredNumber, e.SocketDir)
	if n := e.Counts[DisplayCauseOccupied]; n > 0 {
		fmt.Fprintf(&b, "\n  %d refused because the NUMBER IS TAKEN (the server said \"already active\") — those numbers are held by a running server; a run killed before its cleanup leaks exactly this", n)
	}
	if n := e.Counts[DisplayCauseNotInstalled]; n > 0 {
		fmt.Fprintf(&b, "\n  %d could not RUN the server: the binary is not on PATH (the preflight above says whether it is present)", n)
		fmt.Fprintf(&b, "\n  %s", displayServerInstallHint)
	}
	if n := e.Counts[DisplayCauseUnwritable]; n > 0 {
		fmt.Fprintf(&b, "\n  %d could not CREATE their socket/lock file — a socket directory or scratch that cannot be written; check the permissions of %s and of the scratch directory behind the logs below", n, e.SocketDir)
	}
	if n := e.Counts[DisplayCauseOther]; n > 0 {
		fmt.Fprintf(&b, "\n  %d exited for a reason this run does not name — their logs are quoted below, unclassified", n)
	}
	b.WriteString("\n  per-attempt evidence (the server's own words, read back — not a hypothesis):")
	for _, a := range e.Attempts {
		fmt.Fprintf(&b, "\n    ── :%d [%s] %s", a.Number, a.Cause, a.Log)
		if a.Empty {
			b.WriteString("\n       (the log is EMPTY: the server wrote nothing — it did not reach its own diagnostics, which is what a missing or unrunnable binary looks like)")
			continue
		}
		for _, line := range a.Quote {
			fmt.Fprintf(&b, "\n       %s", line)
		}
	}
	return b.String()
}

// CheckFilmScratch answers question 1: can this run write where it must?
//
// The check is a real WRITE, not a permission bit, and the probe file is removed
// again — a directory that cannot hold a marker cannot hold the server's log.
// label is the name the CALLER knows the directory by (e.g. "--film"), so the
// refusal reads in the caller's vocabulary.
func CheckFilmScratch(dir, label string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("%s names no directory: this run has nowhere to put the display server's logs", label)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%s %s cannot be created: %w", label, dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%s %s: %w", label, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s %s is not a directory", label, dir)
	}
	probe := filepath.Join(dir, displayWriteProbePrefix+strconv.Itoa(os.Getpid()))
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%s %s is NOT WRITABLE by uid %d: %w — the display server's log and every frame go here, so this run cannot start", label, dir, os.Getuid(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s %s: closing the write probe: %w", label, dir, err)
	}
	if err := os.Remove(probe); err != nil {
		return fmt.Errorf("%s %s: removing the write probe: %w", label, dir, err)
	}
	return nil
}

// CheckFilmServer answers question 2: is the display server present and
// RUNNABLE?
//
// The presence check is a PATH lookup — never a trial start: a trial start is
// what StartFilmDisplay does, and a missing binary detected there is
// indistinguishable from a taken number, which is the defect this file removes.
// The RUNNABILITY check asks the server for its own usage, so "present but
// cannot execute" (a wrong-architecture binary, a broken interpreter) is
// reported instead of being discovered as a failed attempt.
func CheckFilmServer(server string) error {
	if strings.TrimSpace(server) == "" {
		server = displayServerDefault
	}
	path, err := exec.LookPath(server)
	if err != nil {
		return fmt.Errorf("the display server %s is NOT INSTALLED: no executable named %s on PATH (%s)\nthis films a real terminal on a PRIVATE X display, so the X virtual framebuffer is required.\n%s\nnothing was started — a server that is not installed cannot be worked around, and no display number is the reason",
			server, server, os.Getenv("PATH"), displayServerInstallHint)
	}
	ctx, cancel := context.WithTimeout(context.Background(), displayServerProbeTimeout)
	defer cancel()
	if out, rerr := exec.CommandContext(ctx, path, displayServerProbeArg).CombinedOutput(); rerr != nil {
		return fmt.Errorf("%s is present at %s but did not run (`%s %s` failed: %w)\nlast output: %s\na private display cannot be created with it — this is NOT a missing package, and reinstalling will not fix it",
			server, path, server, displayServerProbeArg, rerr, strings.TrimSpace(string(out)))
	}
	return nil
}

// DisplayCandidateNumbers derives the candidate list from what this machine
// ACTUALLY holds: one number per registration socket under dir, skipped, in the
// order preferred → ascending → below.
//
// The occupancy record is the socket the server itself creates (<dir>/X<n>) —
// that file IS the registration, so reading it is how the list describes THIS
// machine instead of a range that happened to fit its author's.
func DisplayCandidateNumbers(dir string, used []int) []int {
	taken := make(map[int]bool, len(used))
	for _, n := range used {
		taken[n] = true
	}
	out := make([]int, 0, displayScanMax-displayPreferredNumber+displayPreferredNumber)
	for n := displayPreferredNumber; n <= displayScanMax; n++ {
		if !taken[n] {
			out = append(out, n)
		}
	}
	for n := 0; n < displayPreferredNumber; n++ {
		if !taken[n] {
			out = append(out, n)
		}
	}
	return out
}

// DisplayUsedNumbers lists the display numbers this machine has REGISTERED
// under dir — one number per socket `<dir>/X<n>`. A name that is not `X` followed
// by digits is not a registration and is skipped.
//
// An unreadable or absent directory is reported as an ERROR, never as "nothing
// is used": an empty answer would make every candidate look free and turn a
// broken socket directory into twelve failed starts (Rule 15b).
func DisplayUsedNumbers(dir string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the X11 socket directory %s: %w", dir, err)
	}
	var out []int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, x11UnixSocketPrefix) {
			continue
		}
		digits := name[len(x11UnixSocketPrefix):]
		if digits == "" || strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			continue
		}
		n, cerr := strconv.Atoi(digits)
		if cerr != nil {
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

// DisplaySocketPath is the registration socket of display number n. It is the
// protocol's fixed path — the ONE place outside the scratch tree that GoPi reads
// (see x11UnixSocketDir) — and it is read, never written by this file.
func DisplaySocketPath(dir string, number int) string {
	return filepath.Join(dir, x11UnixSocketPrefix+strconv.Itoa(number))
}

// filmServerCommand is the seam at the SPAWN boundary: a test replaces it to
// drive the whole allocation path with no server at all. Production never
// assigns it.
var filmServerCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// FilmDisplay is a private display THIS RUN owns: its number, the server's pid,
// the socket that proves it is up, and the log it writes.
type FilmDisplay struct {
	Number int
	PID    int
	// StartTicks is the server's INCARNATION (starttime, /proc stat field 22),
	// read at the spawn. `PID` alone is a recyclable number: this struct
	// outlives the spawn (a caller closes a display it allocated much earlier),
	// so a pid that has been recycled would be signalled in place of the server
	// — measured class, cmd/agent/ground_kill_identity_test.go, where exactly
	// that shape put SIGINT into `systemd --user` and stopped the host's GNOME
	// session. An EMPTY value is not usable identity and is treated as one
	// (Close then refuses rather than guessing).
	StartTicks string
	Socket     string
	Log        string
	serverExe  string
}

// Name is the display spelling every X client takes (":62"). It is derived from
// the number, so the number and the spelling cannot disagree.
func (d *FilmDisplay) Name() string { return ":" + strconv.Itoa(d.Number) }

// displayStartTicks reads the incarnation of a just-spawned server. An
// unreadable value returns "" — the empty identity `displayKillIncarnation`
// refuses, so a racing read can never turn into a signal at a recycled pid.
func displayStartTicks(pid int) string {
	ticks, ok := procname.StartTicks(procname.DefaultProcRoot, strconv.Itoa(pid))
	if !ok {
		return ""
	}
	return strings.TrimSpace(ticks)
}

// displayKillIncarnation delivers sig to a display server ONLY while the pid
// still names the process this run spawned, compared on the INCARNATION
// (pid + starttime) rather than the pid alone.
//
// It exists because a pid is a NUMBER THE KERNEL RECYCLES and `os.FindProcess`
// on Unix is not a lookup (Go's doc: it "is not a valid test for the existence
// of the process") — the previously shipped Close signalled whatever process
// owned the number at close time. A refused delivery is reported as an error so
// a live server is never silently abandoned; an ALREADY-GONE process is the
// state Close wants and is returned as success (ESRCH, and the "process already
// finished" spelling exec.Cmd uses).
func displayKillIncarnation(pid int, startTicks string, sig syscall.Signal) error {
	if pid <= 0 || startTicks == "" {
		return fmt.Errorf("display server pid %d has no recorded incarnation — refusing to signal a bare pid number", pid)
	}
	nowTicks, ok := procname.StartTicks(procname.DefaultProcRoot, strconv.Itoa(pid))
	if !ok {
		return nil // the entry is gone: the server has already exited, which is Close's goal state
	}
	if strings.TrimSpace(nowTicks) != strings.TrimSpace(startTicks) {
		return fmt.Errorf("refusing to signal display server pid %d: its incarnation changed (started at %s, the record says %s) — a recycled pid must never be signalled",
			pid, strings.TrimSpace(nowTicks), strings.TrimSpace(startTicks))
	}
	if err := syscall.Kill(pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) || strings.Contains(err.Error(), "process already finished") {
			return nil
		}
		return fmt.Errorf("stop display server pid %d: %w", pid, err)
	}
	return nil
}

// Close kills the display server and reaps it. A process that already exited is
// not an error (the caller may close after a failure path killed it).
func (d *FilmDisplay) Close() error {
	if d == nil || d.PID <= 0 {
		return nil
	}
	return displayKillIncarnation(d.PID, d.StartTicks, syscall.SIGTERM)
}

// StartFilmDisplay answers question 3: it starts a server THIS RUN owns on a
// number it can have, and on failure returns a *DisplayUnavailableError built
// from the attempts' own logs.
//
// scratch is where the logs live (CheckFilmScratch ran first). socketDir is the
// registration directory (x11UnixSocketDir in production, a fixture in a test).
func StartFilmDisplay(scratch, socketDir string, maxTries int) (*FilmDisplay, error) {
	server := displayServerDefault
	if maxTries <= 0 {
		maxTries = displayMaxTries
	}
	used, err := DisplayUsedNumbers(socketDir)
	if err != nil {
		return nil, err
	}
	candidates := DisplayCandidateNumbers(socketDir, used)
	serverExe, err := exec.LookPath(server)
	if err != nil {
		// Reported in the caller's own vocabulary rather than as a per-attempt
		// cause: nothing was started, and twelve identical "command not found"
		// logs would be twelve copies of one fact.
		return nil, fmt.Errorf("the display server %s is NOT INSTALLED: no executable named %s on PATH (%s)\n%s",
			server, server, os.Getenv("PATH"), displayServerInstallHint)
	}
	report := &DisplayUnavailableError{Total: len(candidates), SocketDir: socketDir, Counts: map[DisplayCause]int{}}
	for _, num := range candidates {
		if report.Tried >= maxTries {
			break
		}
		report.Tried++
		logPath := filepath.Join(scratch, displayLogPrefix+strconv.Itoa(num)+displayLogSuffix)
		d, aerr := tryFilmDisplay(serverExe, num, socketDir, logPath)
		if aerr == nil {
			return d, nil
		}
		attempt := readDisplayAttempt(num, logPath)
		report.Counts[attempt.Cause]++
		report.Attempts = append(report.Attempts, attempt)
	}
	if len(report.Attempts) == 0 {
		return nil, fmt.Errorf("no X display could be tried: the candidate list derived from %s is empty", socketDir)
	}
	return nil, report
}

// tryFilmDisplay starts ONE server and verifies it against its OWN socket while
// the process is still alive: two facts, neither of them a guess about startup
// speed. A server that refused the number writes why into its log and exits.
func tryFilmDisplay(serverExe string, number int, socketDir, logPath string) (*FilmDisplay, error) {
	sock := DisplaySocketPath(socketDir, number)
	// STALE REGISTRATION: the socket file outlives a server killed with SIGKILL
	// (`rm` of the socket is the server's own graceful path). A file left behind
	// would make the settle loop below return a success for a server that never
	// came up, so it is removed first — and if it cannot be removed, the attempt
	// fails HERE with that reason rather than as a mysterious timeout.
	if _, err := os.Stat(sock); err == nil {
		if rerr := os.Remove(sock); rerr != nil {
			return nil, fmt.Errorf("display :%d: a stale socket %s exists and cannot be removed: %w", number, sock, rerr)
		}
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("display :%d: the attempt log %s cannot be created: %w", number, logPath, err)
	}
	defer logFile.Close()
	geom := fmt.Sprintf("%dx%dx%d", displayScreenW, displayScreenH, displayScreenD)
	args := []string{":" + strconv.Itoa(number), displayScreenArg, "0", geom, displayNoListenArg, displayNoListenVal}
	cmd := filmServerCommand(context.Background(), serverExe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// IT DIES WITH THIS RUN, KERNEL-ENFORCED. Pdeathsig is the whole mitigation
	// for the leak this pipeline is famous for: a driver killed with SIGKILL runs
	// no cleanup code, so `Close` never happens — and the leaked servers are not
	// hypothetical, they are why the candidate list is DERIVED rather than assumed
	// (this host held :240 :62 :77 :97 from exactly that). The kernel delivers the
	// signal when the process that started the server dies, so no signal handler,
	// no supervisor and no new dependency is involved. Setpgid keeps the server out
	// of this run's own group, so a signal aimed at the group does not race the
	// kernel's.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("display :%d: starting %s: %w", number, serverExe, err)
	}
	d := &FilmDisplay{Number: number, PID: cmd.Process.Pid, StartTicks: displayStartTicks(cmd.Process.Pid), Socket: sock, Log: logPath, serverExe: serverExe}
	deadline := time.Now().Add(displaySettleTimeout)
	for time.Now().Before(deadline) {
		if info, serr := os.Stat(sock); serr == nil && info.Mode()&os.ModeSocket != 0 {
			return d, nil
		}
		// A server that already exited will never register: stop early so the
		// classification reads ITS log rather than waiting out the settle.
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			break
		}
		time.Sleep(displayPollStep) //stylegate:allow-sleep polling the server's own registration socket
	}
	if cerr := d.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "shot: display :%d cleanup: %v\n", number, cerr)
	}
	if werr := cmd.Wait(); werr != nil {
		return nil, fmt.Errorf("display :%d: %s exited: %w", number, serverExe, werr)
	}
	return nil, fmt.Errorf("display :%d: %s did not register %s within %s", number, serverExe, sock, displaySettleTimeout)
}

// readDisplayAttempt reads back ONE attempt's evidence. A log that cannot be
// read is still an attempt, and it says so in the entry rather than being
// dropped: an unreadable log and an empty one are different failures.
func readDisplayAttempt(number int, logPath string) DisplayAttempt {
	a := DisplayAttempt{Number: number, Log: logPath}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		a.Cause = DisplayCauseOther
		a.Quote = []string{"(the log could not be read: " + err.Error() + ")"}
		a.Empty = true
		return a
	}
	text := string(raw)
	a.Cause = classifyDisplayLog(text)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		a.Quote = append(a.Quote, line)
		if len(a.Quote) >= displayLogQuoteLines {
			break
		}
	}
	a.Empty = strings.TrimSpace(text) == ""
	return a
}

// X11SocketDir is the directory the candidate list is DERIVED from: the
// protocol's fixed location for a server's registration socket. It is the ONE
// place outside the scratch tree that a display allocation reads, and it is
// exported because "which directory did the derivation look at?" is part of the
// failure report a caller has to be able to reproduce.
func X11SocketDir() string { return x11UnixSocketDir }

// FilmGrabDefaultTries is the settled-grab bound (see SettledGrab). It is a
// function so the number has ONE definition (Rule 13/14) that a caller can read
// instead of re-typing.
func FilmGrabDefaultTries() int { return filmGrabDefaultTries }

// FilmGrabDefaultSettle is the pause between two grabs of the same frame.
func FilmGrabDefaultSettle() time.Duration { return filmGrabDefaultSettle }
