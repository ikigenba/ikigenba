// Package seam provides the environmental dependencies of one sandbox run.
package seam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
)

// Cmd describes one program invocation.
type Cmd struct {
	Path string
	Args []string
	Dir  string
}

// Result holds captured output and the program's exit status.
type Result struct {
	Stdout   []byte
	Output   []byte
	ExitCode int
}

// Runner runs a program to completion and captures its output.
type Runner func(context.Context, Cmd) (Result, error)

// StreamRunner runs a program while delivering its standard output to a writer.
type StreamRunner func(context.Context, Cmd, io.Writer) (Result, error)

// Deps supplies the environmental dependencies of one CLI invocation.
type Deps struct {
	Dir    string
	EUID   int
	Getenv func(string) string
	Exec   Runner
	Stream StreamRunner
}

// Exec runs cmd and captures its standard output separately from combined output.
func Exec(ctx context.Context, cmd Cmd) (Result, error) { return run(ctx, cmd, nil) }

// Stream runs cmd, streams standard output, and captures standard error.
func Stream(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error) {
	return run(ctx, cmd, stdout)
}

type capture struct {
	mu            sync.Mutex
	out, combined bytes.Buffer
}

func (c *capture) write(p []byte, standard bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.combined.Write(p)
	if standard {
		c.out.Write(p)
	}
}

type captureWriter struct {
	capture  *capture
	standard bool
}

func (w captureWriter) Write(p []byte) (int, error) {
	w.capture.write(p, w.standard)
	return len(p), nil
}

type streamWriter struct {
	writer io.Writer
	cancel context.CancelFunc
	err    error
}

func (w *streamWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
		w.cancel()
	}
	return n, err
}

func run(ctx context.Context, cmd Cmd, stdout io.Writer) (Result, error) {
	if cmd.Dir == "" {
		return Result{}, errors.New("empty working directory")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	path, err := exec.LookPath(cmd.Path)
	if err != nil {
		return Result{}, err
	}
	process := &exec.Cmd{
		Path:        path,
		Args:        append([]string{cmd.Path}, cmd.Args...),
		Dir:         cmd.Dir,
		SysProcAttr: &syscall.SysProcAttr{Setpgid: true},
	}
	captured := new(capture)
	var streamed *streamWriter
	if stdout == nil {
		process.Stdout = captureWriter{capture: captured, standard: true}
		process.Stderr = captureWriter{capture: captured}
	} else {
		streamed = &streamWriter{writer: stdout, cancel: cancel}
		process.Stdout = streamed
		process.Stderr = captureWriter{capture: captured}
	}
	if err := process.Start(); err != nil {
		return Result{}, err
	}
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-childCtx.Done():
			_ = syscall.Kill(-process.Process.Pid, syscall.SIGKILL)
		case <-done:
		}
	}()
	err = process.Wait()
	close(done)
	<-watcherDone
	result := Result{Stdout: captured.out.Bytes(), Output: captured.combined.Bytes()}
	if streamed != nil && streamed.err != nil {
		return result, streamed.err
	}
	if errCtx := ctx.Err(); errCtx != nil {
		return result, errCtx
	}
	if err == nil {
		return result, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return result, err
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return result, fmt.Errorf("unknown exit status: %w", err)
	}
	if status.Signaled() {
		result.ExitCode = 128 + int(status.Signal())
	} else {
		result.ExitCode = status.ExitStatus()
	}
	return result, nil
}
