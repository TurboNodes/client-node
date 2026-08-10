package quic

import (
	"encoding/binary"
	"errors"
	"io"
)

// Each proxied connection gets its own QUIC stream instead of being
// multiplexed as JSON/base64 messages over the single control stream. The
// server opens the stream and writes this header before the stream becomes
// a raw, bidirectional byte pipe to the target:
//
//	1 byte    addr length
//	N bytes   addr ("host:port")
//	2 bytes   initial payload length (big-endian)
//	M bytes   initial payload
//
// The client replies with a single status byte before any relayed data.
const (
	tunnelOK     byte = 1
	tunnelFailed byte = 0
)

var errAddrTooLong = errors.New("tunnel: address too long")

func readTunnelHeader(r io.Reader) (addr string, payload []byte, err error) {
	var addrLen [1]byte
	if _, err = io.ReadFull(r, addrLen[:]); err != nil {
		return "", nil, err
	}
	addrBuf := make([]byte, addrLen[0])
	if _, err = io.ReadFull(r, addrBuf); err != nil {
		return "", nil, err
	}
	var payloadLen [2]byte
	if _, err = io.ReadFull(r, payloadLen[:]); err != nil {
		return "", nil, err
	}
	n := binary.BigEndian.Uint16(payloadLen[:])
	if n > 0 {
		payload = make([]byte, n)
		if _, err = io.ReadFull(r, payload); err != nil {
			return "", nil, err
		}
	}
	return string(addrBuf), payload, nil
}

func writeTunnelHeader(w io.Writer, addr string, payload []byte) error {
	if len(addr) > 255 {
		return errAddrTooLong
	}
	buf := make([]byte, 0, 1+len(addr)+2+len(payload))
	buf = append(buf, byte(len(addr)))
	buf = append(buf, addr...)
	var payloadLen [2]byte
	binary.BigEndian.PutUint16(payloadLen[:], uint16(len(payload)))
	buf = append(buf, payloadLen[:]...)
	buf = append(buf, payload...)
	_, err := w.Write(buf)
	return err
}
