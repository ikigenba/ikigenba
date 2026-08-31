// Package browser defines the browser-launching dependency used by the CLI.
package browser

// Launcher opens a URL in the user's browser.
type Launcher interface {
	Open(url string) error
}

type command interface {
	Start() error
}

type commandFactory func(name string, args ...string) command

type platformLauncher struct {
	commandName string
}

func (l platformLauncher) Open(url string) error {
	return newCommand(l.commandName, url).Start()
}
