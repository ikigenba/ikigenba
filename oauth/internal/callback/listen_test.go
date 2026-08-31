package callback_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/callback"
)

type listenCall struct {
	network string
	address string
}

type fakeListener struct {
	address net.Addr
}

func (l *fakeListener) Accept() (net.Conn, error) {
	return nil, errors.New("fake listener does not accept connections")
}

func (l *fakeListener) Close() error {
	return nil
}

func (l *fakeListener) Addr() net.Addr {
	return l.address
}

// R-G434-3Z7M
func TestListenRequestsIPv4LoopbackFirst(t *testing.T) {
	var calls []listenCall
	listen := func(network, address string) (net.Listener, error) {
		calls = append(calls, listenCall{network: network, address: address})
		return &fakeListener{address: tcpAddress(t, address)}, nil
	}

	server, err := callback.Listen(listen, 8391)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	if server == nil {
		t.Fatal("Listen() server = nil, want bound server")
	}
	if len(calls) != 2 {
		t.Fatalf("listen calls = %v, want exactly IPv4 then IPv6", calls)
	}
	if calls[0] != (listenCall{network: "tcp4", address: "127.0.0.1:8391"}) {
		t.Errorf("first listen call = %+v, want tcp4 on 127.0.0.1:8391", calls[0])
	}
	if calls[1].network != "tcp6" {
		t.Errorf("second listen network = %q, want tcp6", calls[1].network)
	}
}

// R-G5B0-HQYB
func TestListenReportsOperatingSystemAssignedIPv4Port(t *testing.T) {
	var ipv4 net.Listener
	var listenConfig net.ListenConfig
	listen := func(network, address string) (net.Listener, error) {
		if network == "tcp4" {
			var err error
			ipv4, err = listenConfig.Listen(context.Background(), network, address)
			return ipv4, err
		}
		return &fakeListener{address: tcpAddress(t, address)}, nil
	}
	t.Cleanup(func() {
		if ipv4 != nil {
			_ = ipv4.Close()
		}
	})

	server, err := callback.Listen(listen, 0)
	if err != nil {
		t.Fatalf("Listen(..., 0) error = %v", err)
	}
	if server == nil {
		t.Fatal("Listen(..., 0) server = nil, want bound server")
	}
	_, actualPortText, err := net.SplitHostPort(ipv4.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", ipv4.Addr(), err)
	}
	actualPort, err := strconv.Atoi(actualPortText)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", actualPortText, err)
	}
	if server.Port() == 0 {
		t.Fatal("server.Port() = 0, want operating-system-assigned non-zero port")
	}
	if server.Port() != actualPort {
		t.Errorf("server.Port() = %d, want IPv4 listener port %d", server.Port(), actualPort)
	}
}

// R-G6IW-VIP0
func TestListenRequestsIPv6OnAssignedIPv4Port(t *testing.T) {
	const assignedPort = 43827
	var calls []listenCall
	listen := func(network, address string) (net.Listener, error) {
		calls = append(calls, listenCall{network: network, address: address})
		if network == "tcp4" {
			return &fakeListener{address: tcpAddress(t, "127.0.0.1:"+strconv.Itoa(assignedPort))}, nil
		}
		return &fakeListener{address: tcpAddress(t, address)}, nil
	}

	server, err := callback.Listen(listen, 0)
	if err != nil {
		t.Fatalf("Listen(..., 0) error = %v", err)
	}
	if server == nil {
		t.Fatal("Listen(..., 0) server = nil, want bound server")
	}
	if len(calls) != 2 {
		t.Fatalf("listen calls = %v, want exactly IPv4 then IPv6", calls)
	}
	want := listenCall{network: "tcp6", address: "[::1]:43827"}
	if calls[1] != want {
		t.Errorf("IPv6 listen call = %+v, want %+v (and not [::1]:0 or a separate port)", calls[1], want)
	}
}

func tcpAddress(t *testing.T, address string) net.Addr {
	t.Helper()
	resolved, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		t.Fatalf("ResolveTCPAddr(%q) error = %v", address, err)
	}
	return resolved
}
