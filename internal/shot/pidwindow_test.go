// pidwindow_test.go — the tests for "capture the window that HOSTS this
// process" (pidwindow.go).
//
// The subject has two legs, and each is driven where it lives:
//
//   - the /proc leg (which tty is mine, who owns its master side, which
//     processes are in my ancestry) is driven over a FIXTURE process tree, so
//     the walk is exercised with no display, no Xvfb and no dependency on the
//     test runner's own tty;
//   - the DISPLAY leg (_NET_WM_PID matching, the frame rectangle, the
//     no-match error) is driven over the package's own fake X server, reached
//     through the real dial path — the same discipline the actuation tests use.
//
// The end-to-end leg — this process' window captured from a live display — is
// NOT here: it needs a display whose clients share this pid namespace, so it is
// run for real by `gopi tool capture --self` and reported in the devdoc (Rule 31).

package shot

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Fixture /proc tree.
// ---------------------------------------------------------------------------

const (
	// pdFixtureComm is the command name the fixture's emulator process carries.
	pdFixtureComm = "fixture-term"
	// pdFixturePTYInode is the inode BOTH faces of the fixture pty report —
	// the identity the master search matches on.
	pdFixturePTYInode = "4242"
)

// pdWriteProcTree materialises a miniature /proc: one DIRECTORY PER PID with a
// stat file, plus fd/<n> symlinks whose fdinfo files carry the inode that
// decides which process owns a pty master.
//
// The shape is deliberately the kernel's own: the fixture asserts nothing about
// the walker's implementation, it hands the walker the same three facts the
// kernel would (the stat line, the fd symlink, the fdinfo inode).
func pdWriteProcTree(t *testing.T, procs []pdFixtureProc) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range procs {
		dir := filepath.Join(root, fmt.Sprint(p.pid))
		if err := os.MkdirAll(filepath.Join(dir, procFDName), 0o755); err != nil {
			t.Fatalf("fixture: mkdir %s: %v", dir, err)
		}
		// /proc/<pid>/stat: "<pid> (<comm>) <state> <ppid> …". The comm field is
		// the one that may contain spaces, and the fixture exercises exactly
		// that with pdOddComm.
		stat := fmt.Sprintf("%d (%s) S %d 0 0 0 0 0\n", p.pid, p.comm, p.ppid)
		if err := os.WriteFile(filepath.Join(dir, procStatName), []byte(stat), 0o644); err != nil {
			t.Fatalf("fixture: write stat for %d: %v", p.pid, err)
		}
		for i, fd := range p.fds {
			pdWriteFixtureFD(t, dir, fmt.Sprint(i), fd)
		}
	}
	return root
}

// pdFixtureProc is one process in the fixture tree.
type pdFixtureProc struct {
	pid  int
	ppid int
	comm string
	// fds are the process' open file descriptors, in fd-number order starting
	// at 0.
	fds []pdFixtureFD
}

// pdFixtureFD is one file descriptor of a fixture process.
type pdFixtureFD struct {
	link    string // what readlink /proc/<pid>/fd/<n> reports
	inode   string // the fdinfo ino field
	fdinfo  bool   // whether an fdinfo file exists at all
	withTTY bool   // whether fdinfo carries a tty-index line
}

// pdWriteFixtureFD writes one descriptor's symlink and fdinfo file.
func pdWriteFixtureFD(t *testing.T, procDir, fd string, f pdFixtureFD) {
	t.Helper()
	if err := os.Symlink(f.link, filepath.Join(procDir, procFDName, fd)); err != nil {
		t.Fatalf("fixture: symlink %s/%s: %v", procDir, fd, err)
	}
	if !f.fdinfo {
		return
	}
	body := "pos:\t0\nflags:\t0104002\nmnt_id:\t26\nino:\t" + f.inode + "\n"
	if f.withTTY {
		body += "tty-index:\t6\n"
	}
	infoDir := filepath.Join(procDir, procFDName, procFDInfoName)
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		t.Fatalf("fixture: mkdir %s: %v", infoDir, err)
	}
	if err := os.WriteFile(filepath.Join(infoDir, fd), []byte(body), 0o644); err != nil {
		t.Fatalf("fixture: write fdinfo %s/%s: %v", infoDir, fd, err)
	}
}

// pdOddComm is the command name of the fixture's ROOT process: it carries
// spaces and parentheses, which is what makes the stat parser's last-")"
// anchor load-bearing (a whitespace split would shift the ppid and read a state
// letter instead of a number).
const pdOddComm = "fixture (odd) name"

// TestTTYChainWalksAncestryAndSurvivesSpacedComm drives the ancestry leg over a
// fixture tree: the chain is returned NEAREST FIRST, stops at init, and reads
// the parent pid of a process whose command name contains spaces and
// parentheses.
func TestTTYChainWalksAncestryAndSurvivesSpacedComm(t *testing.T) {
	root := pdWriteProcTree(t, []pdFixtureProc{
		{pid: 500, ppid: 400, comm: pdOddComm},
		{pid: 400, ppid: 300, comm: "gopi"},
		{pid: 300, ppid: 200, comm: "bash"},
		{pid: 200, ppid: 100, comm: "fixture-term"},
		{pid: 100, ppid: 0, comm: "init"},
	})
	chain, err := ttyChain(root, 500)
	if err != nil {
		t.Fatalf("ttyChain: %v", err)
	}
	want := []int{500, 400, 300, 200, 100}
	if len(chain) != len(want) {
		t.Fatalf("ttyChain returned %d entries (%v), want %d", len(chain), chain, len(want))
	}
	for i, pid := range want {
		if chain[i].PID != pid {
			t.Errorf("chain[%d].PID = %d, want %d — the walk must return ancestors NEAREST first (%v)", i, chain[i].PID, pid, chain)
		}
	}
	if chain[0].Comm != pdOddComm {
		t.Errorf("chain[0].Comm = %q, want %q — the stat parse must anchor on the last ')' or a command name with spaces shifts every later field", chain[0].Comm, pdOddComm)
	}
	if chain[3].Comm != pdFixtureComm {
		t.Errorf("chain[3].Comm = %q, want %q", chain[3].Comm, pdFixtureComm)
	}
}

// TestTTYChainStopsAtAMissingParent pins the bound: a tree whose ancestor
// directory does not exist (a process that died mid-walk) stops the walk with
// the entries read so far, and a missing START is an error rather than an
// empty chain (an empty chain would silently match nothing).
func TestTTYChainStopsAtAMissingParent(t *testing.T) {
	root := pdWriteProcTree(t, []pdFixtureProc{
		{pid: 42, ppid: 7, comm: "leaf"},
	})
	chain, err := ttyChain(root, 42)
	if err != nil {
		t.Fatalf("ttyChain over a dead parent: %v", err)
	}
	if len(chain) != 1 || chain[0].PID != 42 {
		t.Fatalf("ttyChain = %v, want just the leaf — a missing ancestor ends the walk, it does not fail it", chain)
	}
	if _, err := ttyChain(root, 999); err == nil {
		t.Fatal("ttyChain accepted a pid with no /proc entry — an unreadable START is an error, not an empty chain")
	}
}

// TestParseProcStatRejectsMalformedLines pins Rule 15 at the parser: a line
// without the documented shape is refused with the line quoted, never decoded
// into a plausible-but-wrong ppid.
func TestParseProcStatRejectsMalformedLines(t *testing.T) {
	for _, bad := range []string{"", "500 no parens", "500 (x) S", "500 (x) S abc"} {
		if _, _, err := parseProcStat(bad); err == nil {
			t.Errorf("parseProcStat(%q) was accepted, want a refusal", bad)
		}
	}
	ppid, comm, err := parseProcStat("500 (odd (deep) name) R 499 0 0")
	if err != nil {
		t.Fatalf("parseProcStat on a valid odd line: %v", err)
	}
	if ppid != 499 || comm != "odd (deep) name" {
		t.Fatalf("parseProcStat = (%d, %q), want (499, %q)", ppid, comm, "odd (deep) name")
	}
}

// TestControllingTTYOnlyAcceptsAPTY pins what the /proc leg treats as "a
// terminal": /dev/pts/N is one, a pipe (pipe:[123]) and /dev/null are not.
func TestControllingTTYOnlyAcceptsAPTY(t *testing.T) {
	root := pdWriteProcTree(t, []pdFixtureProc{
		{pid: 10, ppid: 1, comm: "on-pty", fds: []pdFixtureFD{{link: "/dev/pts/4"}}},
		{pid: 11, ppid: 1, comm: "on-null", fds: []pdFixtureFD{{link: "/dev/null"}}},
		{pid: 12, ppid: 1, comm: "on-pipe", fds: []pdFixtureFD{{link: "pipe:[123]"}}},
		{pid: 13, ppid: 1, comm: "no-fd0"},
	})
	cases := map[int]string{10: "/dev/pts/4", 11: "", 12: "", 13: ""}
	for pid, want := range cases {
		if got := controllingTTY(root, pid); got != want {
			t.Errorf("controllingTTY(pid %d) = %q, want %q", pid, got, want)
		}
	}
}

// TestTerminalEmulatorPIDMatchesByPTYInode is the heart of the /proc leg: the
// emulator is the process holding the MASTER of the tty's pty, identified by
// the pty INODE — not by who merely has the slave open (a shell, a child, a
// background job) and not by name.
func TestTerminalEmulatorPIDMatchesByPTYInode(t *testing.T) {
	root := pdWriteProcTree(t, []pdFixtureProc{
		{pid: 200, ppid: 1, comm: pdFixtureComm, fds: []pdFixtureFD{
			{link: "/dev/null"},
			{link: "/dev/ptmx", inode: pdFixturePTYInode, fdinfo: true, withTTY: true},
		}},
		{pid: 300, ppid: 200, comm: "bash", fds: []pdFixtureFD{
			{link: "/dev/pts/6", inode: pdFixturePTYInode, fdinfo: true},
		}},
		// A DIFFERENT pty: the search must not stop at the first /dev/ptmx it
		// meets, which is what makes the inode the identity rather than the
		// device name.
		{pid: 400, ppid: 1, comm: "other-term", fds: []pdFixtureFD{
			{link: "/dev/ptmx", inode: "9999", fdinfo: true, withTTY: true},
		}},
	})
	ttyPath := filepath.Join(t.TempDir(), "pts6")
	if err := os.WriteFile(ttyPath, nil, 0o644); err != nil {
		t.Fatalf("fixture tty: %v", err)
	}
	ino, err := ttyInode(ttyPath)
	if err != nil {
		t.Fatalf("ttyInode: %v", err)
	}
	if ino == "9999" {
		t.Skip("the fixture's own filesystem handed out inode 9999 — impossible to construct the collision; rerun")
	}
	// Point the fixture's master at the REAL inode of the tty file, so the
	// match is the same inode comparison production does.
	pdRewriteInode(t, root, 200, "1", ino)
	pid, comm, err := terminalEmulatorPID(root, ttyPath)
	if err != nil {
		t.Fatalf("terminalEmulatorPID: %v", err)
	}
	if pid != 200 {
		t.Errorf("terminalEmulatorPID = pid %d, want 200 (the process holding the MASTER for this inode)", pid)
	}
	if comm != pdFixtureComm {
		t.Errorf("terminalEmulatorPID comm = %q, want %q", comm, pdFixtureComm)
	}
}

// pdRewriteInode rewrites one fixture fd's fdinfo inode.
func pdRewriteInode(t *testing.T, root string, pid int, fd, ino string) {
	t.Helper()
	path := filepath.Join(root, fmt.Sprint(pid), procFDName, procFDInfoName, fd)
	if err := os.WriteFile(path, []byte("pos:\t0\nflags:\t0104002\nino:\t"+ino+"\n"), 0o644); err != nil {
		t.Fatalf("rewrite fdinfo %s: %v", path, err)
	}
}

// TestTerminalEmulatorPIDRefusesWhenNobodyHoldsTheMaster pins Rule 15 on the
// no-emulator case: the error names the tty and its inode and says WHY no
// window can follow from it — the alternative (returning 0 and letting the
// caller guess) is the silent failure this file exists to prevent.
func TestTerminalEmulatorPIDRefusesWhenNobodyHoldsTheMaster(t *testing.T) {
	root := pdWriteProcTree(t, []pdFixtureProc{
		{pid: 200, ppid: 1, comm: "bash", fds: []pdFixtureFD{{link: "/dev/pts/6", inode: "1", fdinfo: true}}},
	})
	ttyPath := filepath.Join(t.TempDir(), "pts6")
	if err := os.WriteFile(ttyPath, nil, 0o644); err != nil {
		t.Fatalf("fixture tty: %v", err)
	}
	_, _, err := terminalEmulatorPID(root, ttyPath)
	if err == nil {
		t.Fatal("terminalEmulatorPID accepted a tty whose master nobody holds")
	}
	for _, want := range []string{ttyPath, "master"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

// TestMatchWindowByPIDPrefersTheEmulatorThenTheNearestAncestor is the whole
// selection rule, asserted directly: the emulator's pid outranks everything,
// and without one the NEAREST ancestor that owns a window wins (a further
// ancestor merely contains this process; it is not the window it runs in).
func TestMatchWindowByPIDPrefersTheEmulatorThenTheNearestAncestor(t *testing.T) {
	chain := []ttyChainEntry{{PID: 500, Comm: "leaf"}, {PID: 400, Comm: "gopi"}, {PID: 300, Comm: "bash"}, {PID: 200, Comm: "term"}}
	owners := []windowOwner{
		{Window: 0xaa, PID: 200},
		{Window: 0xbb, PID: 300},
	}
	entry, owner, ok := matchWindowByPID(chain, 200, "term", owners)
	if !ok || entry.PID != 200 || owner.Window != 0xaa {
		t.Fatalf("with an emulator the match must be the EMULATOR (got entry=%v owner=%v ok=%v)", entry, owner, ok)
	}
	entry, owner, ok = matchWindowByPID(chain, 0, "", owners)
	if !ok || entry.PID != 300 || owner.Window != 0xbb {
		t.Fatalf("without an emulator the match must be the NEAREST ancestor owning a window (got entry=%v owner=%v ok=%v)", entry, owner, ok)
	}
	entry, owner, ok = matchWindowByPID(chain, 0, "", []windowOwner{{Window: 0xcc, PID: 999}})
	if ok {
		t.Fatalf("a window no candidate owns must not match (got entry=%v owner=%v)", entry, owner)
	}
	// The emulator pid is tried FIRST even when it appears nowhere in the
	// ancestry: an emulator that re-executed itself is still the right window.
	entry, owner, ok = matchWindowByPID(chain, 777, "term", []windowOwner{{Window: 0xdd, PID: 777}})
	if !ok || owner.Window != 0xdd {
		t.Fatalf("the emulator pid must be matchable without being an ancestor (got entry=%v owner=%v ok=%v)", entry, owner, ok)
	}
}

// ---------------------------------------------------------------------------
// Display leg, over the package's fake X server.
// ---------------------------------------------------------------------------

// Fake-server property/window ids for the pid-matching tests. covFakeRoot is
// the root of the synthetic setup reply; these are the windows hung off it.
const (
	pdRootChildFrame   = uint32(0x00000601) // top-level: the WM frame
	pdRootChildClient  = uint32(0x00000602) // top-level: a bare client (no WM)
	pdNestedClient     = uint32(0x00000611) // the client INSIDE the frame
	pdFrameExtentsAtom = 0x00000701
	pdOtherClient      = uint32(0x00000621) // a second bare top-level window
)

// pdFakeXServer is the fixture's server state: the properties each window
// carries, expressed the way the wire does (atom id → 32-bit items), so the
// test asserts on protocol facts rather than on a mock's API.
type pdFakeXServer struct {
	props    map[uint32]map[uint32][]uint32
	children map[uint32][]uint32
	geometry map[uint32]pdGeometry
	// owner is the fake server this fixture is installed on. It is needed by
	// the ADDRESSING tests (selfaddress_test.go), which assert on the recorded
	// request stream around a focus change — the fixture's own reply table has
	// no opinion about focus.
	owner *covFakeXServer
}

// pdGeometry is one window's rectangle as the fixture serves it.
type pdGeometry struct {
	x, y, w, h int
}

// pdInstallFakePIDWindows starts the package's fake X server and installs the
// pid-window fixture on it. The fixture answers the requests the pid-matching
// path emits — InternAtom, GetProperty, QueryTree, GetGeometry and
// TranslateCoordinates — through the REAL dial path, so the resolution under
// test talks the wire, not a mock's API.
//
// It returns the display and the fixture.
func pdInstallFakePIDWindows(t *testing.T) (string, *pdFakeXServer) {
	t.Helper()
	srv := startCovFakeXServer(t, covUsFakeLayout())
	fx := &pdFakeXServer{
		props:    map[uint32]map[uint32][]uint32{},
		children: map[uint32][]uint32{},
		geometry: map[uint32]pdGeometry{
			covFakeRoot: {0, 0, covFakeWidth, covFakeHeight},
		},
	}
	srv.pidFixture = fx
	fx.owner = srv
	return srv.display, fx
}

// reply answers one request of the pid-window fixture, or returns ok=false so
// the base fake's own table gets it (the connection setup, XTEST, the keyboard
// mapping: none of which this fixture is about).
func (fx *pdFakeXServer) reply(req []byte, seq uint16) ([]byte, bool) {
	switch req[0] {
	case x11OpInternAtom:
		nameLen := int(binary.LittleEndian.Uint16(req[4:6]))
		if 8+nameLen > len(req) {
			return nil, false
		}
		name := string(req[8 : 8+nameLen])
		atom, known := pdAtomID(name)
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		if !known {
			// 0 = "no such atom" — the honest answer for a name this fixture
			// does not serve (production reads it as "the display has no such
			// property" and says so in its label).
			return reply, true
		}
		binary.LittleEndian.PutUint32(reply[8:12], atom)
		return reply, true
	case x11OpGetProperty:
		return fx.pdWindowReply(req, seq), true
	case x11OpQueryTree:
		window := binary.LittleEndian.Uint32(req[4:8])
		parent := uint32(covFakeRoot)
		if window == covFakeRoot {
			parent = 0
		}
		return pdQueryTreeReply(seq, parent, fx.children[window]...), true
	case x11OpGetGeometry:
		window := binary.LittleEndian.Uint32(req[4:8])
		g, ok := fx.geometry[window]
		if !ok {
			g = pdGeometry{0, 0, 10, 10}
		}
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		reply[1] = covFakeDepth
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		binary.LittleEndian.PutUint32(reply[8:12], covFakeRoot)
		binary.LittleEndian.PutUint16(reply[16:18], uint16(g.w))
		binary.LittleEndian.PutUint16(reply[18:20], uint16(g.h))
		return reply, true
	case x11OpTranslateCoords:
		window := binary.LittleEndian.Uint32(req[4:8])
		g := fx.geometry[window]
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		reply[1] = 1 // same-screen
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		binary.LittleEndian.PutUint16(reply[12:14], uint16(int16(g.x)))
		binary.LittleEndian.PutUint16(reply[14:16], uint16(int16(g.y)))
		return reply, true
	}
	return nil, false
}

// setProp records one property on one window.
func (fx *pdFakeXServer) setProp(window, atom uint32, items ...uint32) {
	if fx.props[window] == nil {
		fx.props[window] = map[uint32][]uint32{}
	}
	fx.props[window][atom] = items
}

// pdAtomIDs are the atom ids this fixture hands out, in first-request order,
// for the three names the path interns: _NET_CLIENT_LIST, _NET_WM_PID and
// _NET_FRAME_EXTENTS. Interning is answered by NAME, so the ids are derived
// from a fixed table rather than from request order (a reordering of the
// path's interning must not change the answer).
func pdAtomID(name string) (uint32, bool) {
	switch name {
	case netClientListName:
		return 0x00000c01, true
	case netWMNameName:
		return 0x00000c02, true
	case netFrameExtentsName:
		return pdFrameExtentsAtom, true
	}
	return 0, false
}

// pdWindowReply builds a GetProperty reply for one window/atom.
func (fx *pdFakeXServer) pdWindowReply(req []byte, seq uint16) []byte {
	window := binary.LittleEndian.Uint32(req[4:8])
	atom := binary.LittleEndian.Uint32(req[8:12])
	items, ok := fx.props[window][atom]
	if !ok {
		// type None: the property is absent (what windowProperty reads as 0).
		reply := make([]byte, x11ReplyHeaderLen)
		reply[0] = x11ReplyType
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		return reply
	}
	body := make([]byte, 4*len(items))
	for i, v := range items {
		binary.LittleEndian.PutUint32(body[i*4:], v)
	}
	reply := make([]byte, x11ReplyHeaderLen, x11ReplyHeaderLen+len(body))
	reply[0] = x11ReplyType
	reply[1] = x11FormatBits32
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	binary.LittleEndian.PutUint32(reply[4:8], uint32(len(body)/4))
	binary.LittleEndian.PutUint32(reply[8:12], 0x00000d01) // the property's type atom: not None
	binary.LittleEndian.PutUint32(reply[16:20], uint32(len(items)))
	return append(reply, body...)
}

// pdQueryTreeReply builds a QueryTree reply with the given parent and children.
func pdQueryTreeReply(seq uint16, parent uint32, children ...uint32) []byte {
	body := make([]byte, 4*len(children))
	for i, c := range children {
		binary.LittleEndian.PutUint32(body[i*4:], c)
	}
	reply := make([]byte, x11ReplyHeaderLen, x11ReplyHeaderLen+len(body))
	reply[0] = x11ReplyType
	binary.LittleEndian.PutUint16(reply[2:4], seq)
	binary.LittleEndian.PutUint32(reply[4:8], uint32(len(body)/4)) // CHILD COUNT, not bytes
	binary.LittleEndian.PutUint32(reply[8:12], covFakeRoot)
	binary.LittleEndian.PutUint32(reply[12:16], parent)
	binary.LittleEndian.PutUint32(reply[16:20], uint32(len(children)))
	return append(reply, body...)
}

// ---------------------------------------------------------------------------
// The display leg, driven end to end over the fake X server.
// ---------------------------------------------------------------------------

// pdFixturePIDs are the pids the fixture's process chain and its windows share.
const (
	pdFixtureEmulatorPID = 3001 // the process holding the fixture pty's master
	pdFixtureShellPID    = 3002 // its child (this process, in the fixture)
	// pdUnrelatedPID owns a window in the fixture but appears in no chain, so a
	// match that ignored the chain would be visible as this pid.
	pdUnrelatedPID = 9999
)

// pdFixtureTTYLink is the tty SPELLING the fixture's shell reports on fd 0.
// It is the form production accepts (/dev/pts/N), and the inode the master
// search matches comes from a REAL file the test creates — the two facts the
// kernel would provide, provided separately, so the fixture needs no /dev/pts
// write access.
const pdFixtureTTYLink = "/dev/pts/9"

// pdFixtureProcTree writes the /proc tree the display-leg tests resolve
// through: a shell whose tty is a pty whose master is held by the emulator.
func pdFixtureProcTree(t *testing.T, ttyPath string) string {
	t.Helper()
	ino, err := ttyInode(ttyPath)
	if err != nil {
		t.Fatalf("ttyInode(%s): %v", ttyPath, err)
	}
	return pdWriteProcTree(t, []pdFixtureProc{
		{pid: pdFixtureShellPID, ppid: pdFixtureEmulatorPID, comm: "gopi",
			fds: []pdFixtureFD{{link: pdFixtureTTYLink}, {link: pdFixtureTTYLink}}},
		{pid: pdFixtureEmulatorPID, ppid: 1000, comm: pdFixtureComm,
			fds: []pdFixtureFD{{link: "/dev/null"}, {link: x11TtyMasterLink, inode: ino, fdinfo: true, withTTY: true}}},
		{pid: 1000, ppid: 0, comm: "init"},
	})
}

// pdInstallFixtureWindows hangs two top-level windows off the fixture's root:
// a bare client owned by the emulator's pid, and another owned by nobody's pid
// in the chain (so a wrong match would be visible).
func pdInstallFixtureWindows(fx *pdFakeXServer) {
	fx.children[covFakeRoot] = []uint32{pdRootChildClient, pdOtherClient}
	fx.props[pdRootChildClient] = map[uint32][]uint32{
		pdAtomIDMust(netWMNameName): {pdFixtureEmulatorPID},
	}
	fx.props[pdOtherClient] = map[uint32][]uint32{
		pdAtomIDMust(netWMNameName): {pdUnrelatedPID},
	}
	fx.geometry[pdRootChildClient] = pdGeometry{120, 80, 900, 560}
	fx.geometry[pdOtherClient] = pdGeometry{0, 0, 5, 5}
}

// pdAtomIDMust is pdAtomID for a name the fixture is known to serve.
func pdAtomIDMust(name string) uint32 {
	atom, ok := pdAtomID(name)
	if !ok {
		panic("fixture atom " + name + " is not served") //panic-ok: test fixture constant lookup, a programming error in the test itself
	}
	return atom
}

// pdPointSelfAtFixture redirects the tty chain at the fixture tree and
// restores both seams afterwards — the production values are the constants, so
// a leak would make later tests resolve the fixture's pids.
func pdPointSelfAtFixture(t *testing.T, procRoot string) {
	t.Helper()
	prevRoot, prevPID := x11SelfProcRoot, x11SelfStartPID
	x11SelfProcRoot, x11SelfStartPID = procRoot, pdFixtureShellPID
	t.Cleanup(func() { x11SelfProcRoot, x11SelfStartPID = prevRoot, prevPID })
}

// TestWindowRectForResolvesTheHostingWindowByPID is the whole path: the /proc
// chain names the emulator, the emulator's pid matches a window's
// _NET_WM_PID, and the returned rectangle is that window's geometry — with the
// chain reported in the label.
func TestWindowRectForResolvesTheHostingWindowByPID(t *testing.T) {
	display, fx := pdInstallFakePIDWindows(t)
	pdInstallFixtureWindows(fx)
	ttyPath := filepath.Join(t.TempDir(), "pts9")
	if err := os.WriteFile(ttyPath, nil, 0o644); err != nil {
		t.Fatalf("fixture tty: %v", err)
	}
	pdPointSelfAtFixture(t, pdFixtureProcTree(t, ttyPath))

	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("dial %s: %v", display, err)
	}
	defer c.close()
	r, window, label, err := c.windowRectFor(false)
	if err != nil {
		t.Fatalf("windowRectFor: %v", err)
	}
	if window != pdRootChildClient {
		t.Fatalf("resolved window 0x%x, want 0x%x (the window whose _NET_WM_PID is the emulator's)", window, pdRootChildClient)
	}
	if r.X != 120 || r.Y != 80 || r.W != 900 || r.H != 560 {
		t.Fatalf("resolved rect %s, want 900x560+120+80 (the window's own geometry)", r)
	}
	for _, want := range []string{x11RouteNetWMNamePID, fmt.Sprint(pdFixtureEmulatorPID), pdFixtureComm, pdFixtureTTYLink, "client-area"} {
		if !strings.Contains(label, want) {
			t.Errorf("the route label does not carry %q: %q", want, label)
		}
	}
}

// TestWindowRectForTakesTheFrameWhenAsked pins the decoration switch: the same
// window, asked WITH decorations, expands by _NET_FRAME_EXTENTS (left, right,
// top, bottom) and says so in the label; asked without, it stays the client
// area.
func TestWindowRectForTakesTheFrameWhenAsked(t *testing.T) {
	display, fx := pdInstallFakePIDWindows(t)
	pdInstallFixtureWindows(fx)
	fx.props[pdRootChildClient][pdFrameExtentsAtom] = []uint32{2, 3, 37, 5}
	ttyPath := filepath.Join(t.TempDir(), "pts9")
	if err := os.WriteFile(ttyPath, nil, 0o644); err != nil {
		t.Fatalf("fixture tty: %v", err)
	}
	pdPointSelfAtFixture(t, pdFixtureProcTree(t, ttyPath))

	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("dial %s: %v", display, err)
	}
	defer c.close()
	client, _, _, err := c.windowRectFor(false)
	if err != nil {
		t.Fatalf("windowRectFor(false): %v", err)
	}
	framed, _, label, err := c.windowRectFor(true)
	if err != nil {
		t.Fatalf("windowRectFor(true): %v", err)
	}
	want := Rect{X: client.X - 2, Y: client.Y - 37, W: client.W + 5, H: client.H + 42}
	if framed != want {
		t.Fatalf("framed rect %s, want %s (client %s expanded by _NET_FRAME_EXTENTS 2,3,37,5)", framed, want, client)
	}
	if !strings.Contains(label, netFrameExtentsName) {
		t.Errorf("the route label does not name %s: %q", netFrameExtentsName, label)
	}
}

// TestWindowRectForRefusesWithoutAMatchingPID pins Rule 15 on the no-match
// case: the refusal quotes BOTH the ancestry walked and the pids the display
// reported — the two lists are what makes the cause readable (a pid-namespace
// mismatch shows as two disjoint lists, not as a guess).
func TestWindowRectForRefusesWithoutAMatchingPID(t *testing.T) {
	display, fx := pdInstallFakePIDWindows(t)
	pdInstallFixtureWindows(fx)
	// Re-home the fixture window on a pid that is in no chain.
	fx.props[pdRootChildClient][pdAtomIDMust(netWMNameName)] = []uint32{pdUnrelatedPID}
	ttyPath := filepath.Join(t.TempDir(), "pts9")
	if err := os.WriteFile(ttyPath, nil, 0o644); err != nil {
		t.Fatalf("fixture tty: %v", err)
	}
	pdPointSelfAtFixture(t, pdFixtureProcTree(t, ttyPath))

	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("dial %s: %v", display, err)
	}
	defer c.close()
	_, _, label, err := c.windowRectFor(false)
	if err == nil {
		t.Fatalf("a display with no window for this process was resolved anyway (route %q)", label)
	}
	for _, want := range []string{
		fmt.Sprint(pdFixtureShellPID), fmt.Sprint(pdFixtureEmulatorPID), pdFixtureComm,
		pdFixtureTTYLink, fmt.Sprint(pdUnrelatedPID), netWMNameName,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not carry %q, so the reader cannot compare the two sides: %v", want, err)
		}
	}
}

// TestWindowPIDAndClientEnumerationReadTheWire asserts the two list-shaped
// reads the path depends on: _NET_CLIENT_LIST (a WM's own list, when present)
// and QueryTree of the root (when there is no WM — the fixture's own case).
func TestWindowPIDAndClientEnumerationReadTheWire(t *testing.T) {
	display, fx := pdInstallFakePIDWindows(t)
	pdInstallFixtureWindows(fx)
	c, err := dialX11(display)
	if err != nil {
		t.Fatalf("dial %s: %v", display, err)
	}
	defer c.close()
	pid, err := c.windowPID(pdRootChildClient)
	if err != nil {
		t.Fatalf("windowPID: %v", err)
	}
	if pid != pdFixtureEmulatorPID {
		t.Fatalf("windowPID = %d, want %d", pid, pdFixtureEmulatorPID)
	}
	if pid, err := c.windowPID(0x00000e99); err != nil || pid != 0 {
		t.Fatalf("windowPID of an unknown window = (%d, %v), want (0, nil) — an absent property is not an error", pid, err)
	}
	tops, err := c.topLevelWindows()
	if err != nil {
		t.Fatalf("topLevelWindows: %v", err)
	}
	if len(tops) != 2 || tops[0] != pdRootChildClient || tops[1] != pdOtherClient {
		t.Fatalf("topLevelWindows = %v, want [0x%x 0x%x] (the root's children, in server order)", tops, pdRootChildClient, pdOtherClient)
	}
	children, err := c.queryTreeChildren(covFakeRoot)
	if err != nil {
		t.Fatalf("queryTreeChildren: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("queryTreeChildren(root) = %v, want the 2 fixture children — the reply's LENGTH field is a child COUNT, and reading it as bytes is the trap", children)
	}
}
