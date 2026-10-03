package cli

import (
	"net"
	"os"
)

func inheritedListener(fd uintptr) (net.Listener, error) {
	file := os.NewFile(fd, "inherited listening socket")
	ln, err := net.FileListener(file)
	_ = file.Close()
	if unix, ok := ln.(*net.UnixListener); ok {
		unix.SetUnlinkOnClose(false)
	}
	return ln, err
}
