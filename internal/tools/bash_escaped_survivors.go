package tools

// bash_escaped_survivors.go — WHAT A KILL LEFT BEHIND, measured.
//
// THE DEFECT THIS CLOSES (bug row 01a1d278, operator's own report). A bash call
// that ran `make test-budget` in the FOREGROUND was killed by the idle net and
// the result said, in full:
//
//	[command killed: no output for 2m0s]
//
// — "killed, failed, and over". The SYSTEM's state was different: the command
// had backgrounded its work through `setsid` (scripts/test-budget.sh does that
// deliberately, Rule 42's containment: the suite leads its own session so a
// group-addressed signal a test sends reaches the suite and nothing else), so
// the `go test` process was NOT in the killed tree. It kept running for up to
// 45 minutes, holding the per-package suite lock, and the NEXT run of that
// package was refused with a holder pid the operator had never started.
//
// Two facts were true at once and the tool reported only one of them. That is
// the Rule 4.10 family — the observation had a mediator — and the fix is not a
// new mechanism, it is the missing MEASUREMENT: after a kill, ask the kernel
// which processes still hold the output pipe THIS CALL opened.
//
// WHY THE OUTPUT PIPE IS THE RIGHT EVIDENCE, and not a process-tree walk.
// killChildTree already walks /proc/<pid>/stat's ppid field for the child's
// descendants — and it can never find a `setsid … &` survivor, because
// setsid(2) both starts a NEW session and REPARENTS the process, so the ancestry
// link the walk follows is gone by the time the walk runs (measured; the same
// fact bashPipeGraceAfterKill's comment records). What a survivor CANNOT shed is
// the descriptor it inherited: an un-redirected child keeps our pipe's WRITE end,
// and /proc/<pid>/fd/<n> names the pipe by INODE (`pipe:[479692653]`). Fstat on
// the read end this call owns yields the same inode number, so "who still holds
// my pipe" is an equality the kernel answers, not an inference. Measured on this
// host while writing this file:
//
//	read-end inode=479692653 mode=10600        (syscall.Fstat on StdoutPipe)
//	pid=3036039 fd=1 -> pipe:[479692653]       (a setsid-detached `sleep 60`,
//	pid=3036039 fd=2 -> pipe:[479692654]        reparented, still running)
//
// The WRITE-ACCESS half is what makes it a survivor rather than this tool's own
// read end: /proc/<pid>/fdinfo/<n>'s `flags:` word carries the access mode in
// its low two bits, so a descriptor that is not O_RDONLY is the far end of the
// pipe. Without that the scan would "find" the tool reporting about itself.
//
// HONEST LIMIT, stated because the report must not read as complete. A survivor
// that redirected its own stdio (`>/dev/null 2>&1`) holds no descriptor of ours
// and is INVISIBLE to this scan — nothing in /proc links it to this call. The
// report therefore says what it measured (these pids hold this call's pipe) and,
// in the pipe-held-but-unnameable case, says that a survivor exists and could
// not be measured, instead of printing nothing (Rule 15: the state is stated,
// never implied).
//
// RULE 42 IS WHY THIS FILE ONLY REPORTS. These processes were STARTED by the
// command, but this call no longer HOLDS them: they left the process group and
// the ancestry, so it has no handle to signal through. The report names each one
// by pid AND by the kernel's start ticks (the incarnation, so a recycled pid
// cannot be mistaken for it) and says plainly that the pid alone is not an
// identity — it never invites a `kill` on a number.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/nick/gopi/internal/procname"
)

const (
	// bashEscapedSurvivorNoticeFragment is the stable phrase the result carries
	// when a kill left processes running outside the killed tree. A named
	// constant because the regression test asserts on it (Rule 1/13: one
	// definition site; a test reading a bare literal cannot tell a wording
	// change from a behaviour change).
	bashEscapedSurvivorNoticeFragment = "left running outside the killed tree"

	// bashEscapedSurvivorNoticeFmt is the full sentence (Rule 15: a kill that
	// did not stop the work is a FACT about the run, and the reader — often the
	// model that will decide what to do next — must be able to act on it).
	// Three verbs, each pointing at its own measured field.
	bashEscapedSurvivorNoticeFmt = "\n[%d process(es) %s when the command was killed: a signal reached the command's process tree, and these were measured OUTSIDE it — each still holds the stdout/stderr pipe THIS call opened (by inode), which is what proves the link]\n" +
		"%s" +
		"  these are NOT signalable from here (Rule 42: this call started them but no longer holds them — they left its process group and its ancestry, which is exactly why the tree kill missed them). The pid above is transient; the startticks value is the identity, so compare it before touching anything.\n" +
		"  if one of them is still doing the work you thought the kill ended, it will keep holding whatever lock it took (for a suite of one package: the per-package suite lock) until it exits.\n"

	// bashEscapedUnnameableFmt is the second, honest shape: a descendant held the
	// pipe after the kill (that is why the tool released it) and the scan could
	// not measure WHO — restricted procfs, or a process this user may not read.
	// Saying nothing here would be the silence this whole file removes.
	bashEscapedUnnameableFmt = "\n[a descendant %s when the command was killed and still held the output pipe: the tool released the pipe after %s, but WHICH process holds it could not be measured from here (/proc is restricted for it, or it belongs to another user) — so this result cannot name it. Something is still running; that much is measured.]\n"

	// bashEscapedReportMax bounds how many holders are named. A cap is stated
	// rather than silent: the remainder is counted in the sentence.
	bashEscapedReportMax = 5

	// bashEscapedFdScanMax bounds the descriptors looked at per process, so a
	// process with tens of thousands of them cannot make this scan expensive
	// (Rule 27: a bounded loop carries its own cap).
	bashEscapedFdScanMax = 256

	// bashEscapedCmdlineMax bounds the command line echoed per holder: a
	// diagnostic, not the record (the record is the pid + startticks).
	bashEscapedCmdlineMax = 120

	// bashEscapedPipeFieldPrefix is what /proc/<pid>/fdinfo/<n> calls the access
	// word, and bashEscapedAccessMask is the low two bits of it: O_RDONLY 0 /
	// O_WRONLY 1 / O_RDWR 2. A holder that is NOT O_RDONLY is the far end.
	bashEscapedPipeFieldPrefix = "flags:"
	bashEscapedAccessMask      = 3
	bashEscapedAccessRead      = 0

	// bashEscapedPipeLinkPrefix is what readlink on /proc/<pid>/fd/<n> yields for
	// an anonymous pipe ("pipe:[<inode>]").
	bashEscapedPipeLinkPrefix = "pipe:["
	bashEscapedPipeLinkSuffix = "]"
)

// bashEscapedHolder is ONE process measured to hold a pipe this call opened,
// with every field read from /proc at the moment of the scan (Rule 24: identity
// is /proc evidence — a pid number alone names nothing, because the kernel
// recycles it).
type bashEscapedHolder struct {
	Pid     int
	Start   string // stat field 22 — the incarnation
	Session int    // its session id; different from the killed child's is WHY it escaped
	HasSess bool
	Cwd     string
	Cmd     string
}

// bashPipeInodeOf reads the kernel's inode number for one open end of a pipe.
// 0 means "could not be read", which the caller must treat as "no channel" —
// never as "no holders" (Rule 15a: an unmeasured value never reads as a value).
func bashPipeInodeOf(f interface{ Fd() uintptr }) uint64 {
	if f == nil {
		return 0
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return 0
	}
	return st.Ino
}

// bashEscapedPipeHolders scans the process table for every live process holding
// a NON-READ descriptor on one of inodes, excluding self (this tool's own read
// ends). The result is sorted by pid so two identical runs report identically.
//
// Bounded by bashEscapedFdScanMax per process; an unreadable /proc entry is
// skipped (a process that exits mid-scan is not a finding, and a permission
// denial is reported by the caller through the unnameable branch when nothing
// at all could be named).
func bashEscapedPipeHolders(inodes map[uint64]bool, self int) []bashEscapedHolder {
	if len(inodes) == 0 {
		return nil
	}
	entries, err := os.ReadDir(procRootName)
	if err != nil {
		return nil
	}
	var out []bashEscapedHolder
	for _, e := range entries {
		pid, perr := strconv.Atoi(e.Name())
		if perr != nil || pid <= 0 || pid == self {
			continue
		}
		if !bashPidHoldsPipeEnd(pid, inodes) {
			continue
		}
		out = append(out, bashHolderFacts(pid))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pid < out[j].Pid })
	return out
}

// bashPidHoldsPipeEnd reports whether pid holds a WRITE-side descriptor on any
// of inodes. Both halves matter: the inode match says "this is one of my pipes",
// the access mode says "this is the far end of it" (this tool's own ends are
// O_RDONLY, so skipping the mode would make the scan find itself).
func bashPidHoldsPipeEnd(pid int, inodes map[uint64]bool) bool {
	fdDir := filepath.Join(procRootName, strconv.Itoa(pid), procFdDirName)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return false
	}
	for i, entry := range entries {
		if i >= bashEscapedFdScanMax {
			return false
		}
		target, lerr := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if lerr != nil {
			continue
		}
		ino, ok := bashPipeInodeFromLink(target)
		if !ok || !inodes[ino] {
			continue
		}
		mode, mok := bashFdAccessMode(pid, entry.Name())
		if !mok || mode == bashEscapedAccessRead {
			continue
		}
		return true
	}
	return false
}

// bashPipeInodeFromLink parses readlink's pipe spelling ("pipe:[<inode>]").
func bashPipeInodeFromLink(target string) (uint64, bool) {
	if !strings.HasPrefix(target, bashEscapedPipeLinkPrefix) || !strings.HasSuffix(target, bashEscapedPipeLinkSuffix) {
		return 0, false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(target, bashEscapedPipeLinkPrefix), bashEscapedPipeLinkSuffix)
	ino, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || ino == 0 {
		return 0, false
	}
	return ino, true
}

// bashFdAccessMode reads one descriptor's access mode from its fdinfo flags
// word (the low two bits). Unreadable/unparsable answers false — the caller then
// makes NO statement about that descriptor.
func bashFdAccessMode(pid int, fd string) (int, bool) {
	raw, err := os.ReadFile(filepath.Join(procRootName, strconv.Itoa(pid), procFdInfoDirName, fd))
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, bashEscapedPipeFieldPrefix) {
			continue
		}
		flags, perr := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, bashEscapedPipeFieldPrefix)), 8, 64)
		if perr != nil {
			return 0, false
		}
		return int(flags & bashEscapedAccessMask), true
	}
	return 0, false
}

// bashHolderFacts reads a holder's identity from /proc: its start ticks (the
// incarnation), its session (different from the killed child's — that is WHY the
// tree kill missed it), the directory it works in, and a bounded command line.
// Every field that cannot be read stays EMPTY, which the renderer names as
// unmeasured rather than printing a zero (Rule 15a).
func bashHolderFacts(pid int) bashEscapedHolder {
	h := bashEscapedHolder{Pid: pid}
	pidStr := strconv.Itoa(pid)
	if ticks, ok := procname.StartTicks(procRootName, pidStr); ok {
		h.Start = ticks
	}
	if sid, ok := procname.ProcSessionKey(procRootName, pidStr); ok {
		h.Session, h.HasSess = sid, true
	}
	if target, err := os.Readlink(filepath.Join(procRootName, pidStr, procname.CwdName)); err == nil {
		h.Cwd = target
	}
	if argv, ok := procname.Cmdline(procRootName, pidStr); ok {
		h.Cmd = bashBoundedCmdline(argv)
	}
	return h
}

// bashBoundedCmdline joins argv into one bounded line, so a process with a huge
// argument vector cannot flood the result it is being reported in.
func bashBoundedCmdline(argv []string) string {
	line := strings.Join(argv, " ")
	if len(line) <= bashEscapedCmdlineMax {
		return line
	}
	return line[:bashEscapedCmdlineMax] + bashCmdlineElision
}

// bashCmdlineElision marks a command line the report cut short, so a reader
// never mistakes a truncated argv for the whole one.
const bashCmdlineElision = "…(truncated)"

// bashEscapedSurvivorNotice renders the survivor report, or "" when nothing was
// left running (the ordinary kill: the tree kill reached everything, and the
// result keeps exactly the wording it had).
//
// childSid is the killed child's session id, when it could be read BEFORE the
// kill: it is what turns "this pid holds my pipe" into "this pid left the tree",
// because a holder in a DIFFERENT session than the child is a process the group
// kill and the ancestry walk both could not have reached.
func bashEscapedSurvivorNotice(holders []bashEscapedHolder, childSid int, haveChildSid bool, pipeReleased bool) string {
	if len(holders) == 0 {
		if !pipeReleased {
			return ""
		}
		return fmt.Sprintf(bashEscapedUnnameableFmt, bashEscapedSurvivorNoticeFragment, bashPipeGraceAfterKill)
	}
	var b strings.Builder
	shown := holders
	if len(shown) > bashEscapedReportMax {
		shown = shown[:bashEscapedReportMax]
	}
	for _, h := range shown {
		b.WriteString("  " + bashEscapedHolderLine(h, childSid, haveChildSid) + "\n")
	}
	if extra := len(holders) - len(shown); extra > 0 {
		fmt.Fprintf(&b, "  …and %d more (the scan is bounded; re-run the check to see them: /proc/<pid>/fd points at the same pipe inode)\n", extra)
	}
	return fmt.Sprintf(bashEscapedSurvivorNoticeFmt, len(holders), bashEscapedSurvivorNoticeFragment, b.String())
}

// bashEscapedHolderLine renders ONE holder as a single, copy-pasteable line.
// The field ORDER is fixed (pid, incarnation, session, cwd, command) so a reader
// — and a grep — can compare two reports of the same process.
func bashEscapedHolderLine(h bashEscapedHolder, childSid int, haveChildSid bool) string {
	ticks := h.Start
	if ticks == "" {
		ticks = "(unmeasured)"
	}
	sess := "session (unmeasured)"
	if h.HasSess {
		sess = fmt.Sprintf("session %d", h.Session)
		if haveChildSid && h.Session != childSid {
			sess += fmt.Sprintf(" — the killed child was in session %d, so this one left the tree", childSid)
		}
	}
	cwd := h.Cwd
	if cwd == "" {
		cwd = "(unmeasured)"
	}
	cmd := h.Cmd
	if cmd == "" {
		cmd = "(unmeasured)"
	}
	return fmt.Sprintf("pid %d startticks %s %s cwd %s cmd %q", h.Pid, ticks, sess, cwd, cmd)
}
