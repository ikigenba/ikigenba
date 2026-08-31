// Package callback defines the callback listener dependency used by the CLI.
package callback

import "net"

// ListenFunc is the network dependency. net.Listen satisfies it.
type ListenFunc func(network, address string) (net.Listener, error)
