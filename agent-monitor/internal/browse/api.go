// Package browse provides the interactive, read-only session browser.
package browse

import "io/fs"

// MinCols is the minimum width for a normal browser screen.
const MinCols = 40

// MinRows is the minimum height for a normal browser screen.
const MinRows = 5

// Config supplies the browser's filesystem and event sources.
type Config struct {
	Home     string
	Root     fs.FS
	Color    bool
	Watcher  Watcher
	Terminal Terminal
}

// Watcher replaces the directories watched for filesystem changes.
type Watcher interface {
	Watch(names []string)
	Changes() <-chan struct{}
}

// Terminal supplies input events and current terminal dimensions.
type Terminal interface {
	Keys() <-chan []byte
	Size() (cols, rows int)
	Resized() <-chan struct{}
}
