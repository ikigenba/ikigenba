// Package callback defines the callback listener dependency used by the CLI.
package callback

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// ListenFunc is the network dependency. net.Listen satisfies it.
type ListenFunc func(network, address string) (net.Listener, error)

// Server is a callback server bound to the IPv4 and IPv6 loopback interfaces.
type Server struct {
	ipv4        net.Listener
	ipv6        net.Listener
	port        int
	bindWarning error
}

// Listen binds IPv4 loopback, then IPv6 loopback on the same port. A returned
// Server is bound; there is no unbound state.
func Listen(listen ListenFunc, port int) (*Server, error) {
	ipv4, err := listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("bind IPv4 loopback: %w", err)
	}

	_, boundPortText, err := net.SplitHostPort(ipv4.Addr().String())
	if err != nil {
		return nil, errors.Join(fmt.Errorf("read IPv4 listener port: %w", err), ipv4.Close())
	}

	boundPort, err := strconv.Atoi(boundPortText)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("read IPv4 listener port: %w", err), ipv4.Close())
	}

	ipv6, err := listen("tcp6", net.JoinHostPort("::1", boundPortText))
	if err != nil {
		return &Server{
			ipv4:        ipv4,
			port:        boundPort,
			bindWarning: fmt.Errorf("bind IPv6 loopback: %w", err),
		}, nil
	}

	return &Server{ipv4: ipv4, ipv6: ipv6, port: boundPort}, nil
}

// Port reports the port actually bound.
func (s *Server) Port() int {
	return s.port
}

// BindWarning returns the IPv6 bind failure, or nil when both families bound.
func (s *Server) BindWarning() error {
	return s.bindWarning
}

// Close releases every bound listener.
func (s *Server) Close() error {
	var ipv6Err error
	if s.ipv6 != nil {
		ipv6Err = s.ipv6.Close()
	}

	return errors.Join(s.ipv4.Close(), ipv6Err)
}
