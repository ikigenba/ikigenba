package cli

import (
	"io"
	"slices"
)

type followRenderer interface {
	render(first bool) (string, error)
	interrupt() string
}

func followCommand(command parsedCommand, sys System, stdout, stderr io.Writer) ExitCode {
	root := newFollowRoot(sys.Root)
	sys.Root = root
	var renderer followRenderer = &tableFollowRenderer{command: command, sys: sys}
	if command.kind == "chat" {
		renderer = newChatFollowRenderer(command, sys)
	}
	var watched []string
	var changes <-chan struct{}
	for first := true; ; first = false {
		beginFollowRender(root)
		output, err := renderer.render(first)
		if err != nil && first {
			return reportHarnessError(command, stderr, err)
		}
		if err == nil && output != "" {
			if code := writeProduct(stdout, stderr, output); code != ExitSuccess {
				return code
			}
		}
		names := watchedFollowNames(root)
		if sys.Watcher != nil && (first || !slices.Equal(names, watched)) {
			sys.Watcher.Watch(names)
			watched = names
		}
		if first && sys.Watcher != nil {
			changes = sys.Watcher.Changes()
		}
		// Test the interrupt separately so a ready change cannot win over an
		// interrupt that was already closed on entry to this wait.
		select {
		case <-sys.Interrupt:
			return writeFollowInterrupt(renderer, stdout, stderr)
		default:
		}
		select {
		case <-sys.Interrupt:
			return writeFollowInterrupt(renderer, stdout, stderr)
		case _, open := <-changes:
			if !open {
				<-sys.Interrupt
				return writeFollowInterrupt(renderer, stdout, stderr)
			}
		}
	}
}

func writeFollowInterrupt(renderer followRenderer, stdout, stderr io.Writer) ExitCode {
	output := renderer.interrupt()
	if output == "" {
		return ExitSuccess
	}
	return writeProduct(stdout, stderr, output)
}

type tableFollowRenderer struct {
	command  parsedCommand
	sys      System
	lastView string
}

func (r *tableFollowRenderer) render(first bool) (string, error) {
	result, err := renderCommand(r.command, r.sys)
	if err != nil {
		return "", err
	}
	if !first && result.view == r.lastView {
		return "", nil
	}
	r.lastView = result.view
	if r.sys.Terminal {
		if first {
			return "\x1b[?25l\x1b[H\x1b[2J" + result.view, nil
		}
		return "\x1b[H\x1b[2J" + result.view, nil
	}
	if first {
		return result.view, nil
	}
	return "\n" + result.view, nil
}

func (r *tableFollowRenderer) interrupt() string {
	if r.sys.Terminal {
		return "\x1b[?25h"
	}
	return ""
}
