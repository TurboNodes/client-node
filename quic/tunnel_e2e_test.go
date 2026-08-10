package quic

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

func generateTestTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"turbo-proxy"},
	}
}

// TestHandleTunnelEndToEnd exercises the real handleTunnel against a live
// QUIC connection: one side opens a stream and speaks the server's half of
// the tunnel protocol (write header, read status byte), the other side runs
// the actual client handleTunnel against a real TCP echo server.
func TestHandleTunnelEndToEnd(t *testing.T) {
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go io.Copy(c, c)
		}
	}()

	tlsConf := generateTestTLSConfig()
	listener, err := quic.ListenAddr("127.0.0.1:0", tlsConf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan *quic.Stream, 1)
	go func() {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			t.Error(err)
			return
		}
		go handleTunnel(stream)
		serverDone <- stream
	}()

	clientTLSConf := &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"turbo-proxy"}}
	conn, err := quic.DialAddr(context.Background(), listener.Addr().String(), clientTLSConf, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "test done")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := writeTunnelHeader(stream, echoLn.Addr().String(), []byte("hello")); err != nil {
		t.Fatal(err)
	}

	stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	var status [1]byte
	if _, err := io.ReadFull(stream, status[:]); err != nil {
		t.Fatalf("reading status byte: %v", err)
	}
	if status[0] != tunnelOK {
		t.Fatalf("status = %d, want tunnelOK", status[0])
	}

	// The initial "hello" payload should already be echoed back.
	got := make([]byte, 5)
	if _, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("reading echoed initial payload: %v", err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("initial payload echo = %q, want %q", got, "hello")
	}

	// Now relay more data both ways over the raw tunnel.
	if _, err := stream.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	got2 := make([]byte, 5)
	if _, err := io.ReadFull(stream, got2); err != nil {
		t.Fatalf("reading second echo: %v", err)
	}
	if !bytes.Equal(got2, []byte("world")) {
		t.Fatalf("second echo = %q, want %q", got2, "world")
	}

	<-serverDone
}
