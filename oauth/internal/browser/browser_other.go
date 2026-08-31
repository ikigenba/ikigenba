//go:build !linux && !darwin

package browser

import "errors"

var errUnsupportedPlatform = errors.New("browser launch is unsupported on this platform")

var newCommand commandFactory = func(string, ...string) command {
	panic("unsupported browser launcher attempted to construct a command")
}

type unsupportedLauncher struct{}

// New returns a launcher that reports the unsupported platform.
func New() Launcher {
	return unsupportedLauncher{}
}

func (unsupportedLauncher) Open(string) error {
	return errUnsupportedPlatform
}
