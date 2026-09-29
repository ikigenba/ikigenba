package browse

import (
	"context"
	"io"
	"slices"
)

// Run browses the supplied root synchronously until input, context, or a write
// ends browsing. It leaves ownership of the terminal with its caller.
func Run(ctx context.Context, cfg Config, stdout io.Writer) error {
	root := &browseRoot{source: cfg.Root}
	state := newBrowserState(cfg, root)
	read := func() { resetRoot(root); state.read() }
	read()
	var cols, rows int
	draw := func() error {
		cols, rows = 0, 0
		if cfg.Terminal != nil {
			cols, rows = cfg.Terminal.Size()
		}
		var view screenView
		if cols >= MinCols && rows >= MinRows {
			view = state.view(cols, rows)
		}
		_, err := stdout.Write(renderScreen(view, cols, rows))
		return err
	}
	if err := draw(); err != nil {
		return err
	}
	watched := watchedRoot(root)
	if cfg.Watcher != nil {
		cfg.Watcher.Watch(watched)
	}
	var keys <-chan []byte
	var resized, changes <-chan struct{}
	if cfg.Terminal != nil {
		keys = cfg.Terminal.Keys()
		resized = cfg.Terminal.Resized()
	}
	if cfg.Watcher != nil {
		changes = cfg.Watcher.Changes()
	}
	for {
		// Cancellation already visible at the beginning of a wait takes priority
		// over all ready event channels.
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		madeRead := false
		select {
		case <-ctx.Done():
			return nil
		case value, ok := <-keys:
			if !ok {
				return nil
			}
			for _, k := range decodeKeys(value) {
				if k == keyQuit {
					return nil
				}
				if cols < MinCols || rows < MinRows {
					continue
				}
				if state.apply(k, cols, rows) {
					read()
					madeRead = true
				}
			}
		case _, ok := <-resized:
			if !ok {
				resized = nil
				continue
			}
		case _, ok := <-changes:
			if !ok {
				changes = nil
				continue
			}
			read()
			madeRead = true
		}
		if err := draw(); err != nil {
			return err
		}
		if madeRead && cfg.Watcher != nil {
			names := watchedRoot(root)
			if !slices.Equal(names, watched) {
				cfg.Watcher.Watch(names)
				watched = names
			}
		}
	}
}
