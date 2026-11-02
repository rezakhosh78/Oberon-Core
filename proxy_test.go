package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

type proxyTestDialer struct {
	conn    net.Conn
	network string
	address string
}

func (d *proxyTestDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	d.network = network
	d.address = address
	return d.conn, nil
}

func TestValidateLocalProxyAddr(t *testing.T) {
	for _, address := range []string{"127.0.0.1:1717", "[::1]:1718", "localhost:9050"} {
		if err := validateLocalProxyAddr(address); err != nil {
			t.Errorf("validateLocalProxyAddr(%q): %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:1717", "192.0.2.5:1718", "127.0.0.1:0", "bad-address"} {
		if err := validateLocalProxyAddr(address); err == nil {
			t.Errorf("validateLocalProxyAddr(%q) unexpectedly succeeded", address)
		}
	}
}

func TestSOCKS5DomainConnectRelaysTCP(t *testing.T) {
	proxyClient, proxyServer := net.Pipe()
	upstreamServer, upstreamClient := net.Pipe()
	dialer := &proxyTestDialer{conn: upstreamServer}
	defer proxyClient.Close()
	defer upstreamClient.Close()
	_ = proxyClient.SetDeadline(time.Now().Add(3 * time.Second))
	go handleSOCKS5(proxyServer, dialer)

	if _, err := proxyClient.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	methodReply := make([]byte, 2)
	if _, err := io.ReadFull(proxyClient, methodReply); err != nil {
		t.Fatal(err)
	}
	if methodReply[0] != 5 || methodReply[1] != 0 {
		t.Fatalf("unexpected method reply: %v", methodReply)
	}

	request := []byte{5, 1, 0, 3, 11}
	request = append(request, []byte("example.com")...)
	request = append(request, 1, 187) // port 443
	if _, err := proxyClient.Write(request); err != nil {
		t.Fatal(err)
	}
	connectReply := make([]byte, 10)
	if _, err := io.ReadFull(proxyClient, connectReply); err != nil {
		t.Fatal(err)
	}
	if connectReply[1] != 0 {
		t.Fatalf("SOCKS5 CONNECT failed with reply %v", connectReply)
	}
	if dialer.network != "tcp" || dialer.address != "example.com:443" {
		t.Fatalf("dialed %q %q, want tcp example.com:443", dialer.network, dialer.address)
	}

	clientWrite := make(chan error, 1)
	go func() {
		_, err := proxyClient.Write([]byte("from-client"))
		clientWrite <- err
	}()
	gotFromClient := make([]byte, len("from-client"))
	if _, err := io.ReadFull(upstreamClient, gotFromClient); err != nil {
		t.Fatal(err)
	}
	if err := <-clientWrite; err != nil {
		t.Fatal(err)
	}
	if string(gotFromClient) != "from-client" {
		t.Fatalf("upstream received %q", gotFromClient)
	}

	upstreamWrite := make(chan error, 1)
	go func() {
		_, err := upstreamClient.Write([]byte("from-upstream"))
		upstreamWrite <- err
	}()
	gotFromUpstream := make([]byte, len("from-upstream"))
	_ = proxyClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(proxyClient, gotFromUpstream); err != nil {
		t.Fatal(err)
	}
	if err := <-upstreamWrite; err != nil {
		t.Fatal(err)
	}
	if string(gotFromUpstream) != "from-upstream" {
		t.Fatalf("client received %q", gotFromUpstream)
	}
}
