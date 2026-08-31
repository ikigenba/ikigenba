// Package browser defines the browser-launching dependency used by the CLI.
package browser

// Launcher opens a URL in the user's browser.
type Launcher interface {
	Open(url string) error
}
