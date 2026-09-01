package callback_test

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strconv"
	"testing"

	"github.com/ikigenba/ikigenba/oauth/internal/callback"
)

type listenCall struct {
	network string
	address string
}

type fakeListener struct {
	address    net.Addr
	closeCalls int
	closeErr   error
}

// R-LLIA-SA7K
func TestListenFuncHasNetworkDependencySignature(t *testing.T) {
	var standardListen callback.ListenFunc = net.Listen
	_ = standardListen

	listenType := reflect.TypeOf((*callback.ListenFunc)(nil)).Elem()
	if listenType.Name() != "ListenFunc" {
		t.Errorf("ListenFunc type name = %q, want %q", listenType.Name(), "ListenFunc")
	}
	if listenType.PkgPath() != "github.com/ikigenba/ikigenba/oauth/internal/callback" {
		t.Errorf("ListenFunc package path = %q, want internal/callback package", listenType.PkgPath())
	}
	if listenType.Kind() != reflect.Func {
		t.Fatalf("ListenFunc kind = %v, want func", listenType.Kind())
	}

	stringType := reflect.TypeOf("")
	if listenType.NumIn() != 2 {
		t.Fatalf("ListenFunc input count = %d, want 2", listenType.NumIn())
	}
	for index := range 2 {
		if inputType := listenType.In(index); inputType != stringType {
			t.Errorf("ListenFunc input %d = %v, want string", index, inputType)
		}
	}

	listenerType := reflect.TypeOf((*net.Listener)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if listenType.NumOut() != 2 {
		t.Fatalf("ListenFunc result count = %d, want 2", listenType.NumOut())
	}
	if resultType := listenType.Out(0); resultType != listenerType {
		t.Errorf("ListenFunc result 0 = %v, want %v", resultType, listenerType)
	}
	if resultType := listenType.Out(1); resultType != errorType {
		t.Errorf("ListenFunc result 1 = %v, want %v", resultType, errorType)
	}
}

// R-LMQ7-61Y9
func TestListenHasCallbackListenerSignature(t *testing.T) {
	wantType := reflect.TypeOf((func(callback.ListenFunc, int) (*callback.Server, error))(nil))
	if gotType := reflect.TypeOf(callback.Listen); gotType != wantType {
		t.Errorf("Listen type = %v, want %v", gotType, wantType)
	}
}

// R-LNY3-JTOY
func TestListenServerProvidesListenerMethods(t *testing.T) {
	serverType := reflect.TypeOf((*callback.Server)(nil))
	wantMethods := map[string]reflect.Type{
		"Port":        reflect.TypeOf((func(*callback.Server) int)(nil)),
		"BindWarning": reflect.TypeOf((func(*callback.Server) error)(nil)),
		"Close":       reflect.TypeOf((func(*callback.Server) error)(nil)),
	}

	for name, wantType := range wantMethods {
		method, found := serverType.MethodByName(name)
		if !found {
			t.Errorf("*callback.Server is missing exported method %s with type %v", name, wantType)
			continue
		}
		if method.Type != wantType {
			t.Errorf("(*callback.Server).%s type = %v, want %v", name, method.Type, wantType)
		}
	}
}

func (l *fakeListener) Accept() (net.Conn, error) {
	return nil, errors.New("fake listener does not accept connections")
}

func (l *fakeListener) Close() error {
	l.closeCalls++
	return l.closeErr
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

// R-G7QT-9AFP
func TestListenToleratesIPv6BindFailureAndReportsWarning(t *testing.T) {
	ipv6Failure := errors.New("IPv6 unavailable")
	ipv4 := &fakeListener{address: tcpAddress(t, "127.0.0.1:8391")}
	listen := func(network, _ string) (net.Listener, error) {
		if network == "tcp4" {
			return ipv4, nil
		}
		return nil, ipv6Failure
	}

	server, err := callback.Listen(listen, 8391)
	if err != nil {
		t.Fatalf("Listen() error = %v, want nil", err)
	}
	if server == nil {
		t.Fatal("Listen() server = nil, want usable IPv4-backed server")
	}
	if server.Port() != 8391 {
		t.Errorf("server.Port() = %d, want 8391", server.Port())
	}
	if warning := server.BindWarning(); warning == nil {
		t.Fatal("server.BindWarning() = nil, want IPv6 bind failure")
	} else if !errors.Is(warning, ipv6Failure) {
		t.Errorf("server.BindWarning() = %v, want error wrapping %v", warning, ipv6Failure)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("server.Close() error = %v", err)
	}
	if ipv4.closeCalls != 1 {
		t.Errorf("IPv4 Close calls = %d, want 1", ipv4.closeCalls)
	}
}

// R-G8YP-N26E
func TestListenWithBothFamiliesHasNoBindWarning(t *testing.T) {
	ipv4CloseFailure := errors.New("IPv4 close failure")
	ipv4 := &fakeListener{
		address:  tcpAddress(t, "127.0.0.1:8391"),
		closeErr: ipv4CloseFailure,
	}
	ipv6 := &fakeListener{address: tcpAddress(t, "[::1]:8391")}
	listen := func(network, _ string) (net.Listener, error) {
		if network == "tcp4" {
			return ipv4, nil
		}
		return ipv6, nil
	}

	server, err := callback.Listen(listen, 8391)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	if server == nil {
		t.Fatal("Listen() server = nil, want bound server")
	}
	if warning := server.BindWarning(); warning != nil {
		t.Errorf("server.BindWarning() = %v, want nil", warning)
	}
	if err := server.Close(); !errors.Is(err, ipv4CloseFailure) {
		t.Errorf("server.Close() error = %v, want error wrapping %v", err, ipv4CloseFailure)
	}
	if ipv4.closeCalls != 1 || ipv6.closeCalls != 1 {
		t.Errorf("Close calls = IPv4 %d, IPv6 %d; want both 1", ipv4.closeCalls, ipv6.closeCalls)
	}
}

// R-GA6M-0TX3
func TestListenTreatsIPv4BindFailureAsFatal(t *testing.T) {
	ipv4Failure := errors.New("IPv4 unavailable")
	var calls []listenCall
	listen := func(network, address string) (net.Listener, error) {
		calls = append(calls, listenCall{network: network, address: address})
		if network == "tcp4" {
			return nil, ipv4Failure
		}
		return &fakeListener{address: tcpAddress(t, address)}, nil
	}

	server, err := callback.Listen(listen, 8391)
	if server != nil {
		t.Fatalf("Listen() server = %v, want nil", server)
	}
	if err == nil {
		t.Fatal("Listen() error = nil, want IPv4 bind failure")
	}
	if !errors.Is(err, ipv4Failure) {
		t.Errorf("Listen() error = %v, want error wrapping %v", err, ipv4Failure)
	}
	if len(calls) != 1 || calls[0].network != "tcp4" {
		t.Errorf("listen calls = %+v, want only the fatal IPv4 attempt", calls)
	}
}

// R-GBEI-ELNS
func TestCloseAllowsImmediateFixedPortRebind(t *testing.T) {
	server, err := callback.Listen(net.Listen, 0)
	if err != nil {
		t.Fatalf("initial Listen(..., 0) error = %v", err)
	}
	if server == nil {
		t.Fatal("initial Listen(..., 0) server = nil")
	}
	port := server.Port()
	if port == 0 {
		t.Fatal("initial server.Port() = 0, want fixed non-zero port")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("initial server.Close() error = %v", err)
	}

	rebound, err := callback.Listen(net.Listen, port)
	if err != nil {
		t.Fatalf("fresh Listen(..., %d) after Close error = %v", port, err)
	}
	if rebound == nil {
		t.Fatalf("fresh Listen(..., %d) server = nil", port)
	}
	t.Cleanup(func() {
		if err := rebound.Close(); err != nil {
			t.Errorf("rebound server.Close() error = %v", err)
		}
	})
	if rebound.Port() != port {
		t.Errorf("rebound server.Port() = %d, want %d", rebound.Port(), port)
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
