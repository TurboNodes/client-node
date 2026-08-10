package quic

import (
	"bytes"
	"testing"
)

func TestTunnelHeaderRoundTrip(t *testing.T) {
	cases := []struct {
		addr    string
		payload []byte
	}{
		{"example.com:443", nil},
		{"1.2.3.4:80", []byte("GET / HTTP/1.1\r\n\r\n")},
		{"", []byte("x")},
	}

	for _, c := range cases {
		var buf bytes.Buffer
		if err := writeTunnelHeader(&buf, c.addr, c.payload); err != nil {
			t.Fatalf("write(%q): %v", c.addr, err)
		}

		gotAddr, gotPayload, err := readTunnelHeader(&buf)
		if err != nil {
			t.Fatalf("read(%q): %v", c.addr, err)
		}
		if gotAddr != c.addr {
			t.Errorf("addr = %q, want %q", gotAddr, c.addr)
		}
		if !bytes.Equal(gotPayload, c.payload) {
			t.Errorf("payload = %q, want %q", gotPayload, c.payload)
		}
	}
}

func TestTunnelHeaderAddrTooLong(t *testing.T) {
	var buf bytes.Buffer
	long := make([]byte, 256)
	if err := writeTunnelHeader(&buf, string(long), nil); err == nil {
		t.Fatal("expected error for oversized address")
	}
}
