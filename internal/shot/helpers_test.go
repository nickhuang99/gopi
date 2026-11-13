package shot

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
)

// small helpers kept out of the main test file for readability.

func newBufio(c net.Conn) *bufio.Reader { return bufio.NewReader(c) }

// portOnly strips a "127.0.0.1:PORT" addr to its port (captureOverCDP takes
// the bare port, mirroring what waitDevToolsPort extracts from chrome).
func portOnly(addr string) string {
	_, port, _ := net.SplitHostPort(addr) //stylegate:ignore splits the fixture listener's own address; the derived port is the assertion
	return port
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func contextBackground() context.Context { return context.Background() }
