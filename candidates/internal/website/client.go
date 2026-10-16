package website

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var errRefused = errors.New(FailRefused)

// Client returns an environment-independent fetcher that refuses inward addresses.
func Client() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: publicDial}}
}

func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip, parseErr := netip.ParseAddr(strings.TrimPrefix(host, "www.")); parseErr == nil && inward(ip.Unmap()) {
		return nil, errRefused
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if inward(ip) {
			return nil, errRefused
		}
	}
	var last error
	for _, ip := range ips {
		conn, e := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return conn, nil
		}
		last = e
	}
	if last == nil {
		last = errRefused
	}
	return nil, last
}

func inward(ip netip.Addr) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
