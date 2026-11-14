// pidwindow.go — resolve "the window that HOSTS this process" by PID, and
// capture it.
//
// WHY THIS EXISTS (devdoc/screen-capture-own-window.md): `--window` asked the
// display "which window is ACTIVE?" (_NET_ACTIVE_WINDOW). That question has a
// different answer from the one a caller in a shell actually means: when the
// operator is looking at the terminal that runs gopi, "the current window" is
// that terminal — but the ACTIVE window is whatever the user last clicked, so a
// capture made from a terminal echoed into a screenshot of another application
// (the measured 2026-09-30 "captured hermes" incident). The question with a
// deterministic answer is not "what is focused" but "where am I RUNNING":
//
//	my controlling tty  →  the process that owns its master side (the terminal
//	                       emulator)  →  that pid's window (_NET_WM_PID)
//
// The FIRST leg is kernel ground truth, readable with no display and no WM:
// /proc/<pid>/fd/0 names the tty, and the emulator is the process holding the
// MASTER side. The SECOND leg is EWMH: _NET_WM_PID is the property a client
// sets to its own process id, so it is an identity — not a heuristic — and it
// does not depend on focus, on the pointer, or on which window is on top.
//
// WHAT IT CANNOT DO, stated here so the limit is not discovered as a bug: the
// pid on the display side only resolves when the display's clients live in the
// SAME PID NAMESPACE as this process. An X-forwarded display (ssh -X) or a
// container boundary makes _NET_WM_PID a number from another namespace, which
// matches nothing here — the capture then REFUSES and prints the chain it
// walked and the pids the display did report (see the error text), rather than
// falling back to the active window, which is exactly the mis-capture this file
// exists to remove.

package shot

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// request length of a GetProperty carrying up to N 32-bit items is
	// variable, so this is the length of the FIXED part (opcode..long-length).
	x11GetPropertyFixedLen = 6

	// x11ProcRoot is the kernel's process filesystem. It is a constant because
	// every /proc path in this file is derived from it and a test points the
	// walker at a fixture tree instead (x11SelfProcRoot).
	x11ProcRoot = "/proc"

	// The /proc/<pid> entry names this file reads. "stat" carries the parent
	// pid, "fd/0" the controlling tty — the two facts the chain needs.
	procStatName   = "stat"
	procFDName     = "fd"
	procFDInfoName = "info"
	procFDStdin    = "0"

	// x11TTYPrefix is the devpts mount point: a controlling tty of a shell on
	// a pseudo-terminal reads as /dev/pts/<n>. Any other spelling (a real
	// console, /dev/tty1) is not a terminal EMULATOR's slave and yields no
	// window, which the resolution reports rather than guesses around.
	x11TTYPrefix = "/dev/pts/"

	// x11TtyMasterLink is the device a terminal emulator holds for the pty it
	// created: opening /dev/ptmx yields the master side, and the pty's own
	// inode is shared between the two faces. Matching the SLAVE's inode against
	// the fdinfo of a process's pty master is therefore a device-level identity
	// (measured on this host: xterm's /dev/ptmx fd reports the inode of the
	// /dev/pts/N its child runs on).
	x11TtyMasterLink = "/dev/ptmx"

	// x11MaxTTYChainDepth bounds the walk from this process up to init. A
	// deeper ancestry than this is a broken /proc, not a chain.
	x11MaxTTYChainDepth = 32

	// x11MaxClientWindows bounds how many windows one resolution inspects: a
	// display with more top-level windows than this is not a desktop, and the
	// bound keeps a pathological server from turning a capture into a walk.
	x11MaxClientWindows = 512

	// x11MaxFrameChildren bounds the descent from a top-level window into its
	// children. A reparenting WM puts the client one level below the frame; the
	// bound stops a hostile/deep tree from being walked forever.
	x11MaxFrameChildren = 8

	// EWMH properties this file reads. _NET_CLIENT_LIST is the WM's list of
	// managed clients; _NET_WM_PID is a client's own pid; _NET_FRAME_EXTENTS is
	// the WM's decoration width on each side of the client.
	netClientListName    = "_NET_CLIENT_LIST"
	netWMNameName        = "_NET_WM_PID"
	netFrameExtentsName  = "_NET_FRAME_EXTENTS"
	x11RouteNetWMNamePID = netWMNameName

	// x11GTKFrameExtentsName is the CLIENT-SIDE-decoration extent property
	// (GTK's own shadow/resize border). It is read only to REPORT — a window
	// that sets it draws its own title bar INSIDE the client window (measured
	// 2026-09-30 on this host's GNOME Terminal: no _NET_FRAME_EXTENTS at all,
	// _GTK_FRAME_EXTENTS = 26,26,23,29, and the title bar's pixels are inside
	// the client rectangle), so expanding by it would capture drop shadow
	// instead of the frame the reader asked for.
	x11GTKFrameExtentsName = "_GTK_FRAME_EXTENTS"

	// x11FrameExtentsClientSide is the phrase the window route carries when the
	// display publishes no server-side frame for the window.
	x11FrameExtentsClientSide = "client-area (no " + netFrameExtentsName + ": this window is client-side decorated, so its title bar is already INSIDE this rectangle)"
)

// x11SelfProcRoot is the root the tty-chain walker reads. Production never
// assigns it (it is the constant x11ProcRoot, which is /proc); every assignment
// is a TEST's, so the walk — the leg that has no display and therefore no fake
// server — can be driven over a fixture process tree. It is the same seam
// discipline CaptureX11Provider follows, one level down.
var x11SelfProcRoot = x11ProcRoot

// x11SelfStartPID is the process whose chain is walked: 0 means "this
// process" (os.Getpid()). It is a variable only so a test can point the walk at
// a fixture tree's leaf.
var x11SelfStartPID = 0

// ttyChainEntry is one process in the ancestry walk, nearest first.
type ttyChainEntry struct {
	PID  int
	Comm string
}

// ttyChain walks from start up through its parents (bounded by
// x11MaxTTYChainDepth) and returns the entries nearest-first. It reads
// /proc/<pid>/stat only, so it works with no display, no WM and no elevated
// privilege.
func ttyChain(procRoot string, start int) ([]ttyChainEntry, error) {
	var chain []ttyChainEntry
	pid := start
	for depth := 0; depth < x11MaxTTYChainDepth; depth++ {
		raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), procStatName))
		if err != nil {
			if depth == 0 {
				return nil, fmt.Errorf("read %s/%d/%s: %w", procRoot, pid, procStatName, err)
			}
			break
		}
		ppid, comm, err := parseProcStat(string(raw))
		if err != nil {
			return nil, fmt.Errorf("pid %d: %w", pid, err)
		}
		chain = append(chain, ttyChainEntry{PID: pid, Comm: comm})
		if ppid <= 0 || ppid == pid {
			break
		}
		pid = ppid
	}
	if len(chain) == 0 {
		return nil, fmt.Errorf("no ancestry could be read from %s starting at pid %d", procRoot, start)
	}
	return chain, nil
}

// parseProcStat reads the parent pid and the command name out of a
// /proc/<pid>/stat line.
//
// The command name is the ONLY field that may contain spaces and parentheses
// (a process may be called "my (odd) name"), so the parse anchors on the LAST
// ')' — splitting the line on whitespace would shift every later field for
// such a process, and the parent pid would then be a state letter.
func parseProcStat(line string) (ppid int, comm string, err error) {
	open := strings.IndexByte(line, '(')
	closeIdx := strings.LastIndexByte(line, ')')
	if open < 0 || closeIdx < open {
		return 0, "", fmt.Errorf("%s line %q is not in the documented <pid> (<comm>) <state> <ppid> … shape", procStatName, strings.TrimSpace(line))
	}
	comm = line[open+1 : closeIdx]
	// The field split lives in ONE place — procname.StatField — instead of
	// re-inlining the count-from-the-last-')' body here (Rules 13/14): a second
	// copy of the comm trap is a second chance to read the wrong column
	// silently, which is exactly the failure this delegation removes.
	rawPpid, ok := procname.StatField(line, procname.StatFieldParent)
	if !ok {
		return 0, "", fmt.Errorf("%s line %q carries no parent-pid field (field %d) after the command name", procStatName, strings.TrimSpace(line), procname.StatFieldParent)
	}
	ppid, err = strconv.Atoi(rawPpid)
	if err != nil {
		return 0, "", fmt.Errorf("%s line %q: parent pid %q is not a number", procStatName, strings.TrimSpace(line), rawPpid)
	}
	return ppid, comm, nil
}

// controllingTTY returns the tty device path this process' stdin is connected
// to, or "" when stdin is not a terminal (a pipe, /dev/null, a socket).
func controllingTTY(procRoot string, pid int) string {
	link, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), procFDName, procFDStdin))
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(link, x11TTYPrefix) {
		return ""
	}
	return link
}

// terminalEmulatorPID returns the pid of the process that owns the MASTER side
// of the pty whose slave is `ttyPath`, or an error naming why no such process
// was found.
//
// The identity is the pty's own inode: /proc/<pid>/fdinfo/<fd> reports the
// inode of every fd, and a pty's master and slave faces share it. So the walk
// asks every process "do you hold this pty?" and reports the one whose fd links
// to /dev/ptmx — the terminal emulator — rather than the (many) processes that
// merely have the slave open as stdin/stdout.
func terminalEmulatorPID(procRoot, ttyPath string) (int, string, error) {
	ino, err := ttyInode(ttyPath)
	if err != nil {
		return 0, "", err
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return 0, "", fmt.Errorf("read %s: %w", procRoot, err)
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a pid directory
		}
		if !holdsPTYMaster(procRoot, pid, ino) {
			continue
		}
		comm := ""
		if raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), procStatName)); err == nil {
			if _, c, err := parseProcStat(string(raw)); err == nil {
				comm = c
			}
		}
		return pid, comm, nil
	}
	return 0, "", fmt.Errorf("no process holds the master side of %s (inode %s): this session's tty is not a terminal emulator's pty — an ssh session whose emulator runs on the CLIENT side has no local window at all", ttyPath, ino)
}

// holdsPTYMaster reports whether pid has an fd whose fdinfo inode equals ino
// and whose device link is the pty master.
func holdsPTYMaster(procRoot string, pid int, ino string) bool {
	fdDir := filepath.Join(procRoot, strconv.Itoa(pid), procFDName)
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false // another user's process: unreadable, and not ours to resolve
	}
	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil || !strings.HasPrefix(link, x11TtyMasterLink) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(fdDir, procFDInfoName, fd.Name()))
		if err != nil {
			continue
		}
		if fdinfoInode(string(raw)) == ino {
			return true
		}
	}
	return false
}

// statInode extracts the inode from the platform-specific Stat_t that
// os.FileInfo.Sys returns.
func statInode(sys any) uint64 {
	st, ok := sys.(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return st.Ino
}

// ttyInode returns the inode of a device path, as a decimal string.
func ttyInode(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	ino := statInode(fi.Sys())
	if ino == 0 {
		return "", fmt.Errorf("stat %s: reported inode 0", path)
	}
	return strconv.FormatUint(ino, 10), nil
}

// fdinfoInode pulls the inode out of a /proc/<pid>/fdinfo/<fd> file's
// "ino:\t<n>" line ("" when the line is absent).
func fdinfoInode(fdinfo string) string {
	for _, line := range strings.Split(fdinfo, "\n") {
		if !strings.HasPrefix(line, "ino:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "ino:"))
	}
	return ""
}

// windowOwner describes a window this client enumerated, with the pid its
// _NET_WM_PID property reports (0 = the window carries no pid: a WM frame, or a
// client that never set the property).
type windowOwner struct {
	Window uint32
	PID    uint32
	Frame  uint32 // the top-level window it sits in, when it is not top-level itself
}

// clientsWithPID enumerates the display's top-level windows and the client
// window inside each, with the pid each one reports.
//
// Route 1 is _NET_CLIENT_LIST (the WM's own list). A display with NO window
// manager has no such property — and a bare Xvfb in a test fixture is exactly
// that case — so Route 2 walks the root window's children instead (QueryTree),
// which is what "every top-level window" means with nobody managing them.
func (c *x11Client) clientsWithPID() ([]windowOwner, []uint32, error) {
	tops, err := c.topLevelWindows()
	if err != nil {
		return nil, nil, err
	}
	var owners []windowOwner
	for _, w := range tops {
		if pid, err := c.windowPID(w); err == nil && pid != 0 {
			owners = append(owners, windowOwner{Window: w, PID: pid})
		}
		children, err := c.queryTreeChildren(w)
		if err != nil {
			continue // a window that vanished mid-walk is not an error to report
		}
		for i, child := range children {
			if i >= x11MaxFrameChildren {
				break
			}
			pid, err := c.windowPID(child)
			if err != nil || pid == 0 {
				continue
			}
			owners = append(owners, windowOwner{Window: child, PID: pid, Frame: w})
		}
		if len(owners) >= x11MaxClientWindows {
			break
		}
	}
	return owners, tops, nil
}

// topLevelWindows returns the windows one level below the root: the WM's
// client list when it has one, otherwise the root's own children.
func (c *x11Client) topLevelWindows() ([]uint32, error) {
	if atom, err := c.internAtom(netClientListName, true); err == nil && atom != 0 {
		items, err := c.windowPropertyItems(c.root, atom, x11MaxClientWindows)
		if err == nil && len(items) > 0 {
			return items, nil
		}
	}
	children, err := c.queryTreeChildren(c.root)
	if err != nil {
		return nil, fmt.Errorf("no %s on the root window and the root's own children could not be listed: %w", netClientListName, err)
	}
	if len(children) > x11MaxClientWindows {
		children = children[:x11MaxClientWindows]
	}
	return children, nil
}

// windowPID reads a window's _NET_WM_PID (0 when it has none).
func (c *x11Client) windowPID(window uint32) (uint32, error) {
	atom, err := c.internAtom(netWMNameName, true)
	if err != nil {
		return 0, err
	}
	if atom == 0 {
		return 0, nil
	}
	return c.windowPIDForAtom(window, atom)
}

// windowPIDForAtom is windowPID with the property atom ALREADY resolved. The
// listing interning the atom once and reading it per window is what keeps a
// read-only enumeration's round trips linear in the window count instead of
// asking the server for the same atom N times.
func (c *x11Client) windowPIDForAtom(window, pidAtom uint32) (uint32, error) {
	if pidAtom == 0 {
		return 0, nil
	}
	return c.windowProperty(window, pidAtom)
}

// windowRectFor resolves the window of THIS process by pid and returns its
// rectangle in root coordinates together with a label naming every step of the
// resolution (Rule 15: the answer carries its provenance, so a caller can see
// WHICH pid and WHICH route produced the window).
//
// decorations == true asks for the window-manager frame instead of the client
// area: the frame is the client's rectangle EXPANDED by _NET_FRAME_EXTENTS
// (left, right, top, bottom), which is what a screenshot tool shows when it
// captures "a window" with its title bar and borders. The mode is reported in
// the label — a silent choice between the two rectangles would be the same
// class of defect as a silent window substitution.
func (c *x11Client) windowRectFor(decorations bool) (Rect, uint32, string, error) {
	chain, err := ttyChain(x11SelfProcRoot, x11SelfPID())
	if err != nil {
		return Rect{}, 0, "", err
	}
	tty := controllingTTY(x11SelfProcRoot, x11SelfPID())
	var emulator int
	var emulatorComm string
	if tty != "" {
		if pid, comm, err := terminalEmulatorPID(x11SelfProcRoot, tty); err == nil {
			emulator, emulatorComm = pid, comm
		}
	}
	owners, tops, err := c.clientsWithPID()
	if err != nil {
		return Rect{}, 0, "", err
	}
	// The emulator's pid is the answer the design is built on, so it is tried
	// first; when the chain has no emulator (an ssh session, a process started
	// by a daemon) the ancestry order is the honest fallback: the window of the
	// NEAREST ancestor that owns one is the window this process is running in.
	entry, owner, found := matchWindowByPID(chain, emulator, emulatorComm, owners)
	if !found {
		return Rect{}, 0, "", c.noOwnWindowError(chain, tty, emulator, owners, tops)
	}
	r, err := c.windowRect(owner.Window)
	if err != nil {
		return Rect{}, 0, "", err
	}
	label := fmt.Sprintf("%s pid=%d (%s) window=%s", x11RouteNetWMNamePID, entry.PID, entry.Comm, windowIDString(owner.Window))
	if tty != "" {
		label += " tty=" + tty
	}
	if !decorations {
		return r, owner.Window, label + " client-area", nil
	}
	framed, ok, err := c.frameRect(owner.Window, r)
	if err != nil {
		return Rect{}, 0, "", err
	}
	if !ok {
		return r, owner.Window, label + " client-area (no " + netFrameExtentsName + ": this display's window manager publishes no decoration width)", nil
	}
	return framed, owner.Window, label + " frame(" + netFrameExtentsName + ")", nil
}

// matchWindowByPID is the WHOLE window-selection rule, in one pure function:
// the terminal emulator's pid is tried first (it is the answer the /proc chain
// exists to produce), then the ancestry from this process upward, and the first
// candidate that owns a window wins. The order is the point — the nearest
// ancestor owning a window is the one this process is RUNNING IN, while a
// further ancestor (say the desktop's session leader) merely contains it.
//
// It is pure over (chain, emulator pid, enumerated windows) so the rule can be
// driven over a fixture without a display (Rule 31 asks for the surface to be
// executed; this is the part of it that has no wire).
func matchWindowByPID(chain []ttyChainEntry, emulator int, emulatorComm string, owners []windowOwner) (ttyChainEntry, windowOwner, bool) {
	candidates := chain
	if emulator != 0 {
		candidates = append([]ttyChainEntry{{PID: emulator, Comm: emulatorComm}}, chain...)
	}
	for _, entry := range candidates {
		for _, owner := range owners {
			if int(owner.PID) == entry.PID {
				return entry, owner, true
			}
		}
	}
	return ttyChainEntry{}, windowOwner{}, false
}

// noOwnWindowError explains a resolution that found no window, quoting BOTH
// sides of the comparison: the ancestry walked and the pids the display
// actually reports. Without that pairing the message would be a dead end for
// the reader — the two lists are what names the cause (a display whose clients
// live in another pid namespace reports pids that appear in neither).
func (c *x11Client) noOwnWindowError(chain []ttyChainEntry, tty string, emulator int, owners []windowOwner, tops []uint32) error {
	var b strings.Builder
	fmt.Fprintf(&b, "no window on display %s carries the pid of this process or of its ancestry, so the window that HOSTS this process could not be resolved", c.connectedDisplay)
	if tty == "" {
		b.WriteString("; stdin is not a pseudo-terminal (no /dev/pts/N), so no terminal emulator could be identified from it")
	} else {
		fmt.Fprintf(&b, "; tty=%s", tty)
	}
	if emulator != 0 {
		fmt.Fprintf(&b, " emulator-pid=%d", emulator)
	}
	b.WriteString("; ancestry walked (nearest first): ")
	for i, entry := range chain {
		if i > 0 {
			b.WriteString(" → ")
		}
		fmt.Fprintf(&b, "%d(%s)", entry.PID, entry.Comm)
	}
	fmt.Fprintf(&b, "; %d window(s) enumerated, %d carrying %s", len(tops), len(owners), netWMNameName)
	if len(owners) > 0 {
		b.WriteString(": ")
		for i, owner := range owners {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s=%d", windowIDString(owner.Window), owner.PID)
		}
	}
	b.WriteString(" — a match requires this process and the display's clients to share ONE pid namespace (an X-forwarded display reports the pids of the host that runs the emulator)")
	b.WriteString("; WHAT TO DO: (a) if this is an ssh -X / X-forwarded session, the emulator runs on the CLIENT machine, so run the command THERE with DISPLAY set to that machine's display — no local addressing can find a window this host cannot see; (b) if stdin is not a terminal (a pipe, a daemon, a scheduled job), start the process with a real tty inside the target window's terminal; (c) if the window exists but its client never set " + netWMNameName + " (some toolkits do not), address it explicitly with `gopi tool act move X Y --display ...` + `gopi tool act click`, or capture it with `gopi tool capture --area WxH+X+Y`")
	return fmt.Errorf("%s", b.String())
}

// x11SelfPID is the pid whose chain is walked (this process, unless a test
// points the walk at a fixture leaf).
func x11SelfPID() int {
	if x11SelfStartPID != 0 {
		return x11SelfStartPID
	}
	return os.Getpid()
}

// frameRect expands a client window's rectangle by _NET_FRAME_EXTENTS
// (left, right, top, bottom). ok == false means the display publishes no
// decoration width for that window — the caller then captures the client area
// and says so in the label rather than inventing a border.
func (c *x11Client) frameRect(client uint32, clientRect Rect) (Rect, bool, error) {
	atom, err := c.internAtom(netFrameExtentsName, true)
	if err != nil || atom == 0 {
		return clientRect, false, nil
	}
	items, err := c.windowPropertyItems(client, atom, x11FrameExtentsItems)
	if err != nil || len(items) < x11FrameExtentsItems {
		return clientRect, false, nil
	}
	left, right := int(items[0]), int(items[1])
	top, bottom := int(items[2]), int(items[3])
	framed := Rect{X: clientRect.X - left, Y: clientRect.Y - top, W: clientRect.W + left + right, H: clientRect.H + top + bottom}
	if framed.W <= 0 || framed.H <= 0 {
		return clientRect, false, nil
	}
	return framed, true, nil
}

// x11FrameExtentsItems is how many CARDINALs _NET_FRAME_EXTENTS carries.
const x11FrameExtentsItems = 4

// queryTreeChildren issues QueryTree (opcode 15) and returns a window's
// children. The reply's |length| field is the CHILD COUNT (not a byte length),
// with the children inline in the payload — see queryTree, which reads the
// parent instead.
func (c *x11Client) queryTreeChildren(window uint32) ([]uint32, error) {
	header, payload, err := c.queryTreeReply(window)
	if err != nil {
		return nil, err
	}
	n := int(binary.NativeEndian.Uint32(header[16:20]))
	if n > len(payload)/4 {
		n = len(payload) / 4
	}
	children := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		children = append(children, binary.NativeEndian.Uint32(payload[i*4:i*4+4]))
	}
	return children, nil
}

// windowPropertyItems reads up to maxItems 32-bit items of a property (a
// LIST-typed EWMH property such as _NET_CLIENT_LIST carries many). It is the
// plural sibling of windowProperty, which stops at one item.
func (c *x11Client) windowPropertyItems(window, property uint32, maxItems int) ([]uint32, error) {
	req := make([]byte, x11GetPropertyFixedLen*4)
	req[0] = x11OpGetProperty
	req[1] = 0 // do not delete the property
	binary.NativeEndian.PutUint16(req[2:], x11GetPropertyFixedLen)
	binary.NativeEndian.PutUint32(req[4:], window)
	binary.NativeEndian.PutUint32(req[8:], property)
	binary.NativeEndian.PutUint32(req[12:], x11PropertyTypeAny)
	binary.NativeEndian.PutUint32(req[16:], 0) // long-offset
	binary.NativeEndian.PutUint32(req[20:], uint32(maxItems))
	if err := c.request(req); err != nil {
		return nil, err
	}
	header, payload, err := c.readReply("GetProperty")
	if err != nil {
		return nil, err
	}
	if binary.NativeEndian.Uint32(header[8:12]) == 0 { // type None: absent
		return nil, nil
	}
	if int(header[1]) != x11FormatBits32 {
		return nil, fmt.Errorf("GetProperty returned format %d, want %d (these properties are 32-bit values)", header[1], x11FormatBits32)
	}
	n := int(binary.NativeEndian.Uint32(header[16:20]))
	if n > len(payload)/4 {
		n = len(payload) / 4
	}
	items := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, binary.NativeEndian.Uint32(payload[i*4:i*4+4]))
	}
	return items, nil
}

// CaptureX11Self captures the window that HOSTS the calling process — resolved
// by pid (see this file's header), never by focus. decorations selects the
// window-manager frame over the client area; the result's WindowLabel names
// which rectangle was taken and how the window was resolved.
func CaptureX11Self(display string, decorations bool) ([]byte, x11Geometry, error) {
	var geom x11Geometry
	name := captureDisplayName(display)
	if name == "" {
		return nil, geom, x11ErrNoDisplay()
	}
	c, err := dialX11(name)
	if err != nil {
		return nil, geom, err
	}
	defer c.close()
	r, window, label, err := c.windowRectFor(decorations)
	if err != nil {
		return nil, geom, err
	}
	if err := r.within(c.width, c.height); err != nil {
		return nil, geom, fmt.Errorf("window 0x%x at %s: %w", window, r, err)
	}
	return c.captureRect(name, r, CaptureModeSelf, label, window)
}
