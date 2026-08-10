package quic

import (
	"crypto/tls"
	"net"
	"os"
	"strings"
)

// alpnProto is the ALPN token the server negotiates on.
const alpnProto = "turbo-proxy"

// insecureEnvVar force-disables certificate verification for every host. For
// development against a self-signed server on a LAN address or a staging box;
// localhost is already covered without it.
const insecureEnvVar = "TURBO_INSECURE_TLS"

// clientTLSConfig builds the TLS config for a QUIC dial to addr.
//
// One function for both the latency probes and the real connection: they used
// to be written out separately, and the probe copy was missing the verification
// setting the dial had. A dev server with a self-signed certificate then failed
// the probe, got marked down, and was never dialled at all — the connection
// looked broken even though the dial would have succeeded.
//
// Verification is skipped only where a self-signed certificate is expected: a
// loopback address, or an explicit opt-in through TURBO_INSECURE_TLS. Real
// hosts are always verified, so this cannot silently weaken production.
func clientTLSConfig(addr string) *tls.Config {
	return &tls.Config{
		NextProtos:         []string{alpnProto},
		InsecureSkipVerify: skipTLSVerify(addr),
	}
}

func skipTLSVerify(addr string) bool {
	if os.Getenv(insecureEnvVar) != "" {
		return true
	}
	return isLoopback(addr)
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
