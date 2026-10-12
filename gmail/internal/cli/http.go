package cli

import (
	"net"
	"net/http"
	"time"
)

// Bounds for the fast phases of a streamed Gmail exchange.
const (
	DialTimeout           = 10 * time.Second
	TLSHandshakeTimeout   = 10 * time.Second
	ResponseHeaderTimeout = 60 * time.Second
)

// DefaultHTTP returns a fresh client suitable for streamed mail transfers.
func DefaultHTTP() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: DialTimeout, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, TLSHandshakeTimeout: TLSHandshakeTimeout, ResponseHeaderTimeout: ResponseHeaderTimeout}}
}
