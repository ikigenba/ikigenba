// Package turns supplies the embedded user systemd units.
package turns

import (
	"embed"
	"io/fs"
)

const (
	// ServiceUnit names the user service unit.
	ServiceUnit = "turns.service"
	// TimerUnit names the user timer unit.
	TimerUnit = "turns.timer"
	// HostBinary is the binary path used by the host service unit.
	HostBinary = "/usr/local/bin/turns"
)

//go:embed share/systemd/turns.service share/systemd/turns.timer
var units embed.FS

// Units returns the embedded service and timer at the filesystem root.
func Units() fs.FS {
	root, err := fs.Sub(units, "share/systemd")
	if err != nil {
		panic(err)
	}
	return root
}
