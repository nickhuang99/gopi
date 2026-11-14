// x11_xauthority_candidates_test.go — the regression guard for the "X11
// connection rejected because of wrong authentication" defect (task
// 01a0c51a): screen_capture worked in one ssh session and failed in another on
// the same host, permanently, and the difference was which Xauthority entry for
// that display NUMBER the client happened to offer.
//
// WHY A FAKE SERVER AND NOT A LIVE FORWARD: a live sshd forward can only prove
// "the client offered the cookie the forward holds" for the arrangement this
// session happens to be in. The fake server makes the ARRANGEMENT the subject:
// it holds ONE cookie, the Xauthority file lists several entries for the same
// display number, and the test asserts the client finds the accepted one. That
// is deterministic, needs no ssh, and red-proves the old "first matching entry
// wins" lookup.
//
// The fake server lives at /tmp/.X11-unix/XN (the protocol-mandated socket
// location, shared with cov_shot_x11_test.go's helpers, which are reused rather
// than re-implemented); the test skips VIBLY when the socket cannot be bound.
package shot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	// xauthRequiredCookie / xauthWrongCookie are the two MIT-MAGIC-COOKIE-1
	// values the fixtures use: the one the fake X server holds and a decoy that
	// appears EARLIER in the file for the same display number.
	xauthRequiredCookie = "00112233445566778899aabbccddeeff"
	xauthWrongCookie    = "ffeeddccbbaa99887766554433221100"

	// xauthForeignAddress is the Xauthority address spelling of a NON-local
	// client host — the shape that shipped the defect (measured on this host: a
	// `host-b:11` entry listed before the live `peerbox:11` one). It is a
	// name no Xauthority reader can mistake for this machine.
	xauthForeignAddress = "some-other-laptop"

	// xauthRefusalReason is the reason string the protocol's FAILED
	// connection-setup reply carries on a wrong cookie (the exact wording the
	// operator's ssh session reported).
	xauthRefusalReason = "wrong authentication"
)

// xauthFakeServer is an X11 server that holds ONE cookie and speaks just enough
// protocol for a capture: the connection-setup handshake, then GetImage.
//
// A connection whose offered cookie is not the one it holds is CLOSED without a
// reply — the measured behaviour of a live X11 forward with a wrong cookie — or
// answered with a protocol FAILED reply carrying a reason, depending on mode.
type xauthFakeServer struct {
	t       *testing.T
	ln      net.Listener
	display string

	// refuseFrame switches the rejection shape from "close silently" to "send a
	// protocol refusal carrying xauthRefusalReason" (the two shapes a real
	// server/forward pair produce).
	refuseFrame bool

	mu       sync.Mutex
	offered  []string
	attempts int

	shutdown chan struct{}
	wg       sync.WaitGroup
}

// startXauthFakeServer binds a socket for a swept display number and serves the
// handshake + GetImage until the test ends.
func startXauthFakeServer(t *testing.T, refuseFrame bool) *xauthFakeServer {
	t.Helper()
	if _, err := os.Stat(x11UnixSocketDir); err != nil {
		t.Skipf("no %s directory — cannot host a fake X server: %v", x11UnixSocketDir, err)
	}
	display, num, err := covClaimFakeDisplay()
	if err != nil {
		t.Skipf("cannot find a free display number for the fake X server: %v", err)
	}
	sockPath := filepath.Join(x11UnixSocketDir, x11UnixSocketPrefix+num)
	lockPath := covXLockPath(num)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Skipf("cannot bind %s (no permission to host a fake X server): %v", sockPath, err)
	}
	s := &xauthFakeServer{t: t, ln: ln, display: display, refuseFrame: refuseFrame, shutdown: make(chan struct{})}
	t.Cleanup(func() {
		close(s.shutdown)
		s.ln.Close() //stylegate:ignore test cleanup: the fake listener is closed after its shutdown channel, which is what unwinds the accept loop
		s.wg.Wait()
		os.Remove(sockPath)
		os.Remove(lockPath)
	})
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%10d\n", os.Getpid())), 0o644); err != nil {
		t.Errorf("write X lock file: %v", err)
	}
	s.wg.Add(1)
	go s.serve()
	return s
}

func (s *xauthFakeServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.shutdown:
				return
			default:
			}
			return
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			s.serveConn(c)
		}(conn)
	}
}

// offeredLabels returns the credentials the fake was handed, in order.
func (s *xauthFakeServer) offeredLabels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.offered))
	copy(out, s.offered)
	return out
}

func (s *xauthFakeServer) serveConn(c net.Conn) {
	head := make([]byte, x11SetupRequestLen)
	if _, err := readFullConn(c, head); err != nil {
		return
	}
	naLen := int(binary.LittleEndian.Uint16(head[6:8]))
	daLen := int(binary.LittleEndian.Uint16(head[8:10]))
	auth := make([]byte, pad4(naLen)+naLen+pad4(daLen)+daLen)
	if len(auth) > 0 {
		if _, err := readFullConn(c, auth); err != nil {
			return
		}
	}
	// Connection-setup request layout after the 12-byte fixed head: the
	// auth-protocol NAME (padded to 4), then the auth DATA (padded to 4).
	nameOff := 0
	dataOff := naLen + pad4(naLen)
	if dataOff+daLen > len(auth) {
		return
	}
	name := string(auth[nameOff : nameOff+naLen])
	data := auth[dataOff : dataOff+daLen]
	s.mu.Lock()
	s.attempts++
	s.offered = append(s.offered, fmt.Sprintf("%s=%x", name, data))
	s.mu.Unlock()

	if name != x11AuthCookieName || !bytes.Equal(data, mustHex(xauthRequiredCookie)) {
		if !s.refuseFrame {
			return // the forward's answer to a wrong cookie: close, no reply
		}
		_, _ = c.Write(xauthFailedSetupReply(xauthRefusalReason)) //stylegate:ignore test fixture: the refusal frame is the whole point of this branch, and the connection is closing either way
		return
	}
	if _, err := c.Write(xauthSuccessSetupReply()); err != nil {
		return
	}
	s.serveGetImage(c)
}

// serveGetImage answers GetImage requests (opcode 73) with a synthetic band of
// pixels, so a capture over this connection can complete. Any other request is
// answered with an X11 error frame, so a wrong interaction fails loudly rather
// than hanging the test.
func (s *xauthFakeServer) serveGetImage(c net.Conn) {
	var seq uint16
	for {
		rh := make([]byte, 4)
		if _, err := readFullConn(c, rh); err != nil {
			return
		}
		words := int(binary.LittleEndian.Uint16(rh[2:4]))
		if words < 1 {
			return
		}
		req := make([]byte, words*4)
		copy(req, rh)
		if _, err := readFullConn(c, req[4:]); err != nil {
			return
		}
		seq++
		if req[0] != x11OpGetImage {
			errFrame := make([]byte, x11ReplyHeaderLen)
			errFrame[0] = x11ErrorType
			errFrame[1] = 3 // BadWindow
			binary.LittleEndian.PutUint16(errFrame[2:4], seq)
			if _, err := c.Write(errFrame); err != nil {
				return
			}
			continue
		}
		h := int(binary.LittleEndian.Uint16(req[14:16]))
		w := int(binary.LittleEndian.Uint16(req[12:14]))
		payload := make([]byte, w*x11BitsPerPixel4*h)
		for i := range payload {
			payload[i] = byte(i % 251)
		}
		reply := make([]byte, x11ReplyHeaderLen, x11ReplyHeaderLen+len(payload))
		reply[0] = x11ReplyType
		reply[1] = covFakeDepth
		binary.LittleEndian.PutUint16(reply[2:4], seq)
		binary.LittleEndian.PutUint32(reply[4:8], uint32(len(payload)/4))
		binary.LittleEndian.PutUint32(reply[16:20], uint32(len(payload)))
		if _, err := c.Write(append(reply, payload...)); err != nil {
			return
		}
	}
}

// xauthSuccessSetupReply mirrors covFakeXServer.setupReply's byte layout (built
// by length so it can never desync) for the small screens this fake advertises.
func xauthSuccessSetupReply() []byte {
	vendor := []byte(covSetupVendor)
	formats := make([]byte, 8*covSetupFormats)
	screen := make([]byte, 40)
	binary.LittleEndian.PutUint32(screen[0:4], covFakeRoot)
	binary.LittleEndian.PutUint16(screen[20:22], covFakeWidth)
	binary.LittleEndian.PutUint16(screen[22:24], covFakeHeight)
	screen[38] = covFakeDepth

	body := make([]byte, 0, 32+pad4(len(vendor))+len(vendor)+len(formats)+len(screen))
	body = append(body, make([]byte, 32)...)
	binary.LittleEndian.PutUint16(body[16:18], uint16(len(vendor)))
	binary.LittleEndian.PutUint16(body[18:20], uint16(65535))
	body[20] = covSetupScreens
	body[21] = covSetupFormats
	body[22] = nativeByteOrder()
	body = append(body, vendor...)
	body = append(body, make([]byte, pad4(len(vendor)))...)
	body = append(body, formats...)
	body = append(body, screen...)

	head := make([]byte, 8)
	head[0] = x11SetupReplySuccess
	binary.LittleEndian.PutUint16(head[6:8], uint16(len(body)/4))
	return append(head, body...)
}

// xauthFailedSetupReply builds the connection-setup FAILED frame (status 0)
// carrying a reason string — the reply a real server sends instead of closing.
func xauthFailedSetupReply(reason string) []byte {
	head := make([]byte, 8)
	head[0] = 0
	head[x11SetupReplyReasonOffset] = byte(len(reason))
	head[6] = 0
	head[7] = 0
	body := make([]byte, pad4(len(reason))+len(reason))
	copy(body, reason)
	return append(head, body...)
}

// xauthEntry is one Xauthority row to write into the fixture file.
type xauthEntry struct {
	family int
	addr   string
	number string
	cookie string
}

// writeXauthFile writes the entries as a byte-exact Xauthority file. It is
// written directly rather than through xauth(1) because xauth REFUSES address
// spellings like "some-other-laptop:11" on a host that is not that laptop, and
// those foreign spellings are exactly the arrangement under test.
func writeXauthFile(t *testing.T, entries []xauthEntry) string {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		buf.Write(xauthEntryField(e.family))
		for _, s := range []string{e.addr, e.number, x11AuthCookieName} {
			writeXauthString(&buf, s)
		}
		// The cookie is RAW BYTES on the wire file format (xauth prints it as
		// hex); the fixture spells it as hex, so it is decoded here rather than
		// written as text.
		writeXauthBytes(&buf, mustHex(e.cookie))
	}
	path := filepath.Join(t.TempDir(), "Xauthority")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture Xauthority: %v", err)
	}
	return path
}

func xauthEntryField(v int) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(v))
	return b
}

func writeXauthString(buf *bytes.Buffer, s string) {
	writeXauthBytes(buf, []byte(s))
}

func writeXauthBytes(buf *bytes.Buffer, b []byte) {
	buf.Write(xauthEntryField(len(b)))
	buf.Write(b)
}

func mustHex(s string) []byte {
	out := make([]byte, len(s)/2)
	for i := range out {
		var v int
		if _, err := fmt.Sscanf(s[i*2:i*2+2], "%02x", &v); err != nil {
			panic("bad hex fixture: " + err.Error())
		}
		out[i] = byte(v)
	}
	return out
}

// xauthFixtureEntries is the arrangement that shipped the defect: for ONE
// display number the file holds a STALE entry under this host's own spelling
// (the highest-priority candidate, so it is offered first and refused), the
// entry sshd's xauth add wrote for the forward (`localhost:N`) which is the one
// the server actually holds, and a foreign client host's spelling last.
func xauthFixtureEntries(t *testing.T, num string) []xauthEntry {
	t.Helper()
	return []xauthEntry{
		{family: x11FamilyLocal, addr: localHostname(t), number: num, cookie: xauthWrongCookie},
		{family: x11FamilyLocal, addr: x11HostLocalhost, number: num, cookie: xauthRequiredCookie},
		{family: x11FamilyLocal, addr: xauthForeignAddress, number: num, cookie: xauthWrongCookie},
		{family: x11FamilyInternet, addr: "", number: num, cookie: xauthWrongCookie},
	}
}

// localHostname is this host's short name, i.e. the address spelling an
// Xauthority entry for a display on THIS machine carries.
func localHostname(t *testing.T) string {
	t.Helper()
	hn, err := os.Hostname()
	if err != nil {
		t.Skipf("cannot determine this host's name: %v", err)
	}
	if i := strings.IndexByte(hn, '.'); i > 0 {
		hn = hn[:i]
	}
	return hn
}

// TestX11XauthorityTriesEveryCandidateForTheDisplayNumber is the regression
// guard: with several Xauthority entries for one display number and the ACCEPTED
// cookie last, the capture must still succeed — and the display name it reports
// must say which credential was used, so a substitution is never silent.
func TestX11XauthorityTriesEveryCandidateForTheDisplayNumber(t *testing.T) {
	srv := startXauthFakeServer(t, false)
	num := strings.TrimPrefix(srv.display, ":")
	authPath := writeXauthFile(t, xauthFixtureEntries(t, num))
	t.Setenv(x11EnvAuthFile, authPath)

	png, geom, err := CaptureX11Root(srv.display)
	if err != nil {
		t.Fatalf("capture failed although the Xauthority file holds the accepted cookie for %s: %v\noffered: %v", srv.display, err, srv.offeredLabels())
	}
	if len(png) == 0 {
		t.Fatal("capture returned no PNG bytes")
	}
	if want := srv.display + x11AuthLabelSuffix + x11HostLocalhost; geom.Display != want {
		t.Errorf("geometry display = %q, want %q (the accepted credential must be named)", geom.Display, want)
	}
	if got := len(srv.offeredLabels()); got != 2 {
		t.Errorf("server saw %d connection attempt(s), want 2 (the stale top-priority entry refused, then the accepted one): %v", got, srv.offeredLabels())
	}
	if first := srv.offeredLabels()[0]; !strings.Contains(first, xauthWrongCookie) {
		t.Errorf("first attempt offered %s, want the top-priority entry (%s) — the priority order is what makes the search deterministic", first, xauthWrongCookie)
	}
	if second := srv.offeredLabels()[1]; !strings.Contains(second, xauthRequiredCookie) {
		t.Errorf("second attempt offered %s, want the accepted credential (%s)", second, xauthRequiredCookie)
	}
}

// TestX11XauthorityFirstCandidateKeepsThePlainDisplayName pins the common case
// the fix must not disturb: when the FIRST candidate is the accepted one, the
// display name carries no credential badge and the capture costs exactly one
// connection.
func TestX11XauthorityFirstCandidateKeepsThePlainDisplayName(t *testing.T) {
	srv := startXauthFakeServer(t, false)
	num := strings.TrimPrefix(srv.display, ":")
	authPath := writeXauthFile(t, []xauthEntry{
		{family: x11FamilyLocal, addr: localHostname(t), number: num, cookie: xauthRequiredCookie},
		{family: x11FamilyLocal, addr: xauthForeignAddress, number: num, cookie: xauthWrongCookie},
	})
	t.Setenv(x11EnvAuthFile, authPath)

	_, geom, err := CaptureX11Root(srv.display)
	if err != nil {
		t.Fatalf("capture failed with the accepted cookie FIRST: %v", err)
	}
	if geom.Display != srv.display {
		t.Errorf("geometry display = %q, want the plain %q (no badge when the first credential works)", geom.Display, srv.display)
	}
	if got := len(srv.offeredLabels()); got != 1 {
		t.Errorf("server saw %d attempt(s), want exactly 1 (no needless retry): %v", got, srv.offeredLabels())
	}
}

// TestX11XauthorityRefusalNamesTheServerReasonAndTheCredentialOffered is the
// diagnostic half: when NO candidate is accepted, the error must carry the
// SERVER's own reason and every credential that was offered — the operator's
// session reported "X11 connection rejected because of wrong authentication",
// and a failure that hides WHICH cookie was sent cannot be diagnosed.
func TestX11XauthorityRefusalNamesTheServerReasonAndTheCredentialOffered(t *testing.T) {
	srv := startXauthFakeServer(t, true)
	num := strings.TrimPrefix(srv.display, ":")
	authPath := writeXauthFile(t, []xauthEntry{
		{family: x11FamilyLocal, addr: xauthForeignAddress, number: num, cookie: xauthWrongCookie},
		{family: x11FamilyLocal, addr: xauthForeignAddress, number: num, cookie: "00112233445566778899aabbccddee00"},
		{family: x11FamilyLocal, addr: xauthForeignAddress, number: num, cookie: "00112233445566778899aabbccddee01"},
	})
	t.Setenv(x11EnvAuthFile, authPath)

	_, err := ProbeDisplay(srv.display)
	if err == nil {
		t.Fatal("probe succeeded against a server that refuses every offered cookie")
	}
	msg := err.Error()
	if !strings.Contains(msg, xauthRefusalReason) {
		t.Errorf("error does not carry the server's own reason %q: %v", xauthRefusalReason, err)
	}
	if !strings.Contains(msg, xauthForeignAddress+":"+num) {
		t.Errorf("error does not name the credential that was refused (%s:%s): %v", xauthForeignAddress, num, err)
	}
	if n := len(srv.offeredLabels()); n < 2 {
		t.Errorf("only %d credential(s) offered, want every candidate tried: %v", n, srv.offeredLabels())
	}
}

// TestReadXAuthorityEntriesPriorityOrder pins the lookup order itself, without
// any server: this host's own spelling first, then the localhost spelling sshd
// writes, then a foreign host's Local entries, then the Internet-family ones.
func TestReadXAuthorityEntriesPriorityOrder(t *testing.T) {
	num := "77"
	entries := []xauthEntry{
		{family: x11FamilyLocal, addr: xauthForeignAddress, number: num, cookie: "aa11"},
		{family: x11FamilyInternet, addr: "", number: num, cookie: "bb22"},
		{family: x11FamilyLocal, addr: x11HostLocalhost, number: num, cookie: "cc33"},
		{family: x11FamilyLocal, addr: localHostname(t), number: num, cookie: "dd44"},
	}
	authPath := writeXauthFile(t, entries)
	t.Setenv(x11EnvAuthFile, authPath)

	d, err := parseX11Display(x11HostLocalhost + ":" + num)
	if err != nil {
		t.Fatalf("parseX11Display: %v", err)
	}
	got := readXAuthorityEntries(d)
	want := []string{localHostname(t), x11HostLocalhost, xauthForeignAddress, ""}
	if len(got) != len(want) {
		t.Fatalf("got %d credentials, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].address != want[i] {
			t.Errorf("credential %d address = %q, want %q (order: %v)", i, got[i].address, want[i], addresses(got))
		}
	}
	// readXAuthority (the single-credential face) must agree with the head of
	// that order — one source of truth, never a second lookup.
	name, data := readXAuthority(d)
	if name != x11AuthCookieName || fmt.Sprintf("%x", data) != "dd44" {
		t.Errorf("readXAuthority = (%q,%x), want the head of the priority order (%q,%s)", name, data, x11AuthCookieName, "dd44")
	}
}

// TestReadXAuthorityEntriesSkipsOtherDisplayNumbers guards the other direction:
// an entry for a DIFFERENT display number must never be offered.
func TestReadXAuthorityEntriesSkipsOtherDisplayNumbers(t *testing.T) {
	authPath := writeXauthFile(t, []xauthEntry{
		{family: x11FamilyLocal, addr: localHostname(t), number: "78", cookie: "aa11"},
		{family: x11FamilyInternet, addr: "", number: "79", cookie: "bb22"},
	})
	t.Setenv(x11EnvAuthFile, authPath)
	d, err := parseX11Display(":77")
	if err != nil {
		t.Fatalf("parseX11Display: %v", err)
	}
	if got := readXAuthorityEntries(d); len(got) != 0 {
		t.Errorf("got %d credential(s) for display :77 from a file that has none: %+v", len(got), addresses(got))
	}
}

// TestReadXAuthorityEntriesCapsCandidates bounds the failure path: a
// hand-maintained file with many entries for one number must not turn a refused
// display into an unbounded number of round trips.
func TestReadXAuthorityEntriesCapsCandidates(t *testing.T) {
	num := "77"
	entries := make([]xauthEntry, 0, x11AuthMaxCandidates+5)
	for i := 0; i < x11AuthMaxCandidates+5; i++ {
		entries = append(entries, xauthEntry{family: x11FamilyLocal, addr: xauthForeignAddress, number: num, cookie: fmt.Sprintf("%04x", i)})
	}
	authPath := writeXauthFile(t, entries)
	t.Setenv(x11EnvAuthFile, authPath)
	d, err := parseX11Display(":" + num)
	if err != nil {
		t.Fatalf("parseX11Display: %v", err)
	}
	if got := readXAuthorityEntries(d); len(got) != x11AuthMaxCandidates {
		t.Errorf("got %d credentials, want the cap %d", len(got), x11AuthMaxCandidates)
	}
}

// TestX11AuthRetryableNamesTheRetryableShapes pins which failures justify
// offering another credential: a server refusal and the silent close a forward
// answers a wrong cookie with. A timeout must NOT (another cookie cannot fix a
// server that answers nothing, and retrying only multiplies the wait).
func TestX11AuthRetryableNamesTheRetryableShapes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "server refusal frame", err: &x11SetupRefusalError{display: ":1", status: 0, reason: xauthRefusalReason}, want: true},
		{name: "server demands another auth protocol", err: &x11SetupRefusalError{display: ":1", status: x11SetupReplyAuthenticate}, want: true},
		{name: "peer closed during setup (wrong cookie on a forward)", err: fmt.Errorf("setup reply: %w", errEOF), want: true},
		{name: "peer closed mid-auth-frame", err: fmt.Errorf("setup reply: %w", errUnexpectedEOF), want: true},
		{name: "server answered nothing (timeout)", err: &timeoutError{}, want: false},
		{name: "dial failure", err: fmt.Errorf("connect: connection refused"), want: false},
	}
	for _, tc := range cases {
		if got := x11AuthRetryable(tc.err); got != tc.want {
			t.Errorf("%s: x11AuthRetryable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// timeoutError is a net.Error whose Timeout() reports true, so the retry
// predicate can be pinned on the shape rather than on a real socket timing out.
type timeoutError struct{}

func (*timeoutError) Error() string   { return "i/o timeout" }
func (*timeoutError) Timeout() bool   { return true }
func (*timeoutError) Temporary() bool { return true }

// addresses renders a credential list's address spellings (test diagnostics).
func addresses(cs []x11Credential) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.address)
	}
	return out
}

// TestX11SetupRefusalReadsTheReasonString proves the reason is read off the wire
// rather than invented: the failure frame is produced by the fake server with a
// distinctive string, and the client's error must contain it verbatim.
func TestX11SetupRefusalReadsTheReasonString(t *testing.T) {
	const reason = "Server is already active for display 77"
	if got := xauthFailedSetupReply(reason); int(got[x11SetupReplyReasonOffset]) != len(reason) {
		t.Fatalf("fixture reason length byte = %d, want %d", got[x11SetupReplyReasonOffset], len(reason))
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, x11SetupRequestLen)
		if _, err := readFullConn(c, buf); err != nil {
			return
		}
		_, _ = c.Write(xauthFailedSetupReply(reason)) //stylegate:ignore test fixture: writing the refusal frame is this goroutine's entire job
		// The frame is flushed by the write; the deferred Close sends FIN after
		// it, so the client reads the reason and then sees EOF. No sleep: the
		// client is already blocked in its read.
	}()
	d, err := parseX11Display("localhost:77")
	if err != nil {
		t.Fatalf("parseX11Display: %v", err)
	}
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c := &x11Client{conn: raw, extOp: map[string]byte{}}
	defer c.close()
	if err := c.handshake("localhost:77", d, x11Credential{authName: x11AuthCookieName, data: []byte("deadbeef")}); err == nil {
		t.Fatal("handshake accepted a FAILED setup reply")
	} else if !strings.Contains(err.Error(), reason) {
		t.Errorf("handshake error = %v, want it to carry the server's reason %q", err, reason)
	}
}

// TestX11CredentialLabelMatchesXauthSpelling keeps the operator-facing label
// findable in their own `xauth list` output.
func TestX11CredentialLabelMatchesXauthSpelling(t *testing.T) {
	if got := x11CredentialLabel(x11Credential{address: "peerbox"}, 11); got != "peerbox:11" {
		t.Errorf("label = %q, want %q", got, "peerbox:11")
	}
	if got := x11CredentialLabel(x11Credential{}, 11); got != ":11" {
		t.Errorf("internet-family label = %q, want %q", got, ":11")
	}
	if got := x11CredentialBadge(x11Credential{}); got != x11CredentialBadgeNoAddress {
		t.Errorf("badge = %q, want %q", got, x11CredentialBadgeNoAddress)
	}
}

var (
	errEOF           = io.EOF
	errUnexpectedEOF = io.ErrUnexpectedEOF
)
