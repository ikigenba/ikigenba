package smarthttp

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
)

func (cfg Config) stream(w http.ResponseWriter, r *http.Request, q route, id string, grant *limits.Grant) {
	rc := http.NewResponseController(w)
	_ = rc.EnableFullDuplex()
	// Commit Emit and Start together before forwarding cancellation: an
	// event already recorded during startup cannot be withdrawn.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		answer(w, r, 500, "cannot run git; try again later")
		return
	}
	defer func() { _ = inRead.Close(); _ = inWrite.Close() }()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		answer(w, r, 500, "cannot run git; try again later")
		return
	}
	defer func() { _ = outRead.Close(); _ = outWrite.Close() }()
	cmd := cfg.Git.Command(ctx, "", cfg.environment(r, q, id),
		"-c", "http.getanyfile=false", "-c", "http.uploadpack=true", "-c", "http.receivepack=true",
		"-c", "receive.maxInputSize="+strconv.FormatInt(cfg.Limits.Settings().PushMaxBytes, 10),
		"-c", "receive.autogc=false", "-c", "gc.auto=0", "http-backend")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inRead, outWrite, io.Discard
	var before map[string]string
	if q.push {
		before, err = cfg.refs(r.Context(), id)
		if err != nil {
			storeAnswer(w, r, err)
			return
		}
	}
	executable, original, err := retainExecutable(cmd.Path)
	if err != nil {
		answer(w, r, 500, "cannot run git; try again later")
		return
	}
	defer func() { _ = executable.Close() }()
	cmd.Path = "/proc/self/fd/" + strconv.Itoa(3+len(cmd.ExtraFiles))
	cmd.ExtraFiles = append(cmd.ExtraFiles, executable)
	requestDone := r.Context().Done()
	deadline := grant.Deadline()
	select {
	case <-requestDone:
		return
	case <-deadline:
		cfg.operation(r.Context(), "operation.timed_out", id, q.op, "operation_seconds", 0)
		// An HTTP client may wait for its request writer before reporting a
		// connection error. Give it headers, then truncate the response, so
		// its body read observes the abort while its input remains open.
		w.WriteHeader(http.StatusOK)
		_ = rc.Flush()
		_ = rc.SetReadDeadline(time.Now())
		_ = rc.SetWriteDeadline(time.Now())
		panic(http.ErrAbortHandler)
	default:
	}
	if grant.Waited() > 0 {
		cfg.operation(r.Context(), "operation.waited", id, q.op, "", grant.Waited())
	}
	if err = startExecutable(cmd, executable, original); err != nil {
		// A post-Start mode-restoration failure still owns a real child.
		// End and reap it before returning its grant or closing its pipes.
		if cmd.Process != nil {
			cancel()
			_ = cmd.Wait()
		}
		answer(w, r, 500, "cannot run git; try again later")
		return
	}
	_ = inRead.Close()
	_ = outWrite.Close()
	var mu sync.Mutex
	ended, interrupted, timedOut := false, false, false
	bodyDone := make(chan struct{})
	processDone := make(chan struct{})
	watchDone := make(chan struct{})
	interrupt := func(timeout bool) {
		mu.Lock()
		if ended {
			mu.Unlock()
			return
		}
		interrupted = true
		timedOut = timedOut || (timeout && r.Context().Err() == nil)
		mu.Unlock()
		cancel()
		_ = rc.SetReadDeadline(time.Now())
		_ = rc.SetWriteDeadline(time.Now())
		_ = inWrite.Close()
		// Killing the command's group closes every producer of stdout. Keep
		// its reader open so bytes already produced can still reach EOF;
		// closing it here would manufacture a relay failure after a natural
		// exit whose complete output was already written to the client.
	}
	// Finish the startup decision before handing any client bytes to git.
	select {
	case <-requestDone:
		interrupt(false)
	case <-deadline:
		w.WriteHeader(http.StatusOK)
		_ = rc.Flush()
		interrupt(true)
	default:
	}
	waitResult := make(chan struct{})
	go func() { _ = cmd.Wait(); close(waitResult) }()
	go func() {
		defer close(watchDone)
		// One arbiter alone receives the deadline and commits completion.
		// A received deadline cannot disappear into a second consumer before
		// its interruption has been committed.
		select {
		case <-waitResult:
			select {
			case <-deadline:
				interrupt(true)
			default:
			}
		case <-requestDone:
			interrupt(false)
			<-waitResult
		case <-deadline:
			interrupt(true)
			<-waitResult
		}
		mu.Lock()
		ended = true
		stop := interrupted
		mu.Unlock()
		if stop {
			_ = rc.SetReadDeadline(time.Now())
			_ = rc.SetWriteDeadline(time.Now())
		}
		select {
		case <-bodyDone:
		default:
			if r.ContentLength != 0 {
				_ = rc.SetReadDeadline(time.Now())
			}
		}
		_ = inWrite.Close()
		close(processDone)
	}()
	mu.Lock()
	feed := !interrupted
	mu.Unlock()
	if feed {
		go func() {
			defer close(bodyDone)
			_, _ = io.CopyBuffer(inWrite, r.Body, make([]byte, git.CopyBufferSize))
			_ = inWrite.Close()
		}()
	} else {
		close(bodyDone)
	}
	status, header, body, headerErr := git.ReadHeader(outRead)
	var counter wireCounter
	var copyErr error
	var unwritten bool
	if headerErr == nil {
		for name, values := range header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(status)
		copyErr = rc.Flush()
		if copyErr == nil {
			unwritten, copyErr = relay(w, rc, body, &counter)
		}
	}
	if headerErr != nil || copyErr != nil {
		interrupt(false)
	}
	<-processDone
	<-watchDone
	// Deadline wakes an HTTP body read. Direct handler callers' bodies must
	// also be closed, since their reads have no connection deadline.
	_ = r.Body.Close()
	<-bodyDone
	mu.Lock()
	stopped, timeout := interrupted, timedOut
	mu.Unlock()
	if q.push {
		cfg.pushed(r.Context(), id, before)
		if counter.rejected {
			cfg.operation(r.Context(), "operation.rejected", id, q.op, "push_max_bytes", 0)
		}
	} else if r.Method == http.MethodPost && cmd.ProcessState != nil && cmd.ProcessState.Success() && headerErr == nil && !unwritten {
		// An attempted interruption is not evidence that it ended Git:
		// cancellation may arrive after the successful exit and full relay.
		// Wait can also report its context error despite an exit status of 0.
		// A final full-count Write or Flush can fail after transferring all
		// bytes. Once Git is reaped, EOF distinguishes that terminal error
		// from output still waiting to be written, without buffering a body.
		complete := copyErr == nil
		if !complete {
			var remaining [1]byte
			n, tailErr := body.Read(remaining[:])
			complete = n == 0 && tailErr == io.EOF
		}
		if complete {
			cfg.Telemetry.Emit(r.Context(), "repo.fetched", telemetry.Attrs{"repo": id, "bytes": counter.pack})
		}
	}
	if timeout {
		cfg.operation(r.Context(), "operation.timed_out", id, q.op, "operation_seconds", 0)
	}
	if stopped || headerErr != nil || copyErr != nil {
		panic(http.ErrAbortHandler)
	}
}

func relay(w http.ResponseWriter, rc *http.ResponseController, body io.Reader, counter *wireCounter) (unwritten bool, err error) {
	buffer := make([]byte, git.CopyBufferSize)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			written, writeErr := w.Write(buffer[:n])
			counter.observe(buffer[:written])
			if written != n {
				if writeErr == nil {
					writeErr = io.ErrShortWrite
				}
				return true, writeErr
			}
			if writeErr != nil {
				return false, writeErr
			}
			if flushErr := rc.Flush(); flushErr != nil {
				return false, flushErr
			}
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}
