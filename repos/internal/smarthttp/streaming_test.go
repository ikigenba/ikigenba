package smarthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
)

const zeroSHA = "0000000000000000000000000000000000000000"

type streamReply struct {
	response *http.Response
	err      error
}

type streamHTTP struct {
	server    *httptest.Server
	done      chan string
	cancels   chan context.CancelFunc
	transport *http.Transport
	received  atomic.Int64
}

type smallListener struct{ net.Listener }

type streamObservedConn struct {
	net.Conn
	received *atomic.Int64
}

func (c streamObservedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.received.Add(int64(n))
	return n, err
}

type streamServerOption struct {
	closeGate <-chan struct{}
	context   func(context.Context) context.Context
	header    func(int)
	hold      *streamHoldObserver
}

type streamHoldObserver struct {
	eof, early atomic.Bool
	t          *testing.T
	trace      string
}

type streamHeldBody struct {
	io.ReadCloser
	hold *streamHoldObserver
}

func (b streamHeldBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.hold.eof.Store(true)
	}
	return n, err
}

type streamHeldWriter struct {
	http.ResponseWriter
	hold *streamHoldObserver
}

func (w streamHeldWriter) observe() {
	if !w.hold.eof.Load() {
		events := streamTrace(w.hold.t, w.hold.trace)
		_, exited := streamExit(events, streamBackend(w.hold.t, events))
		if !exited {
			w.hold.early.Store(true)
		}
	}
}

func (w streamHeldWriter) WriteHeader(status int) {
	w.observe()
	w.ResponseWriter.WriteHeader(status)
}

func (w streamHeldWriter) Write(p []byte) (int, error) {
	w.observe()
	return w.ResponseWriter.Write(p)
}

func (w streamHeldWriter) FlushError() error {
	w.observe()
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w streamHeldWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type streamResponseObserver struct {
	http.ResponseWriter
	header func(int)
}

func (w streamResponseObserver) WriteHeader(status int) {
	w.ResponseWriter.WriteHeader(status)
	w.header(status)
}

func (w streamResponseObserver) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type streamCloseGate struct {
	io.ReadCloser
	closed <-chan struct{}
	ctx    context.Context
}

func (b streamCloseGate) Close() error {
	select {
	case <-b.closed:
	case <-b.ctx.Done():
	}
	return b.ReadCloser.Close()
}

func (l smallListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		if tcp, ok := c.(*net.TCPConn); ok {
			err = tcp.SetWriteBuffer(4096)
			if err != nil {
				_ = c.Close()
				return nil, err
			}
		}
	}
	return c, err
}

func streamServer(f *fixture, small bool, options ...streamServerOption) *streamHTTP {
	f.t.Helper()
	x := &streamHTTP{done: make(chan string, 32), cancels: make(chan context.CancelFunc, 32)}
	h := f.handler()
	x.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		defer func() { x.done <- r.Header.Get("X-Request-Id") }()
		x.cancels <- cancel
		if len(options) != 0 {
			if options[0].hold != nil {
				r.Body = streamHeldBody{r.Body, options[0].hold}
				w = streamHeldWriter{w, options[0].hold}
			}
			if options[0].closeGate != nil && r.Header.Get("X-Request-Id") == "first" {
				r.Body = streamCloseGate{r.Body, options[0].closeGate, ctx}
			}
			if options[0].context != nil {
				ctx = options[0].context(ctx)
			}
			if options[0].header != nil {
				w = streamResponseObserver{w, options[0].header}
			}
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	if small {
		x.server.Listener = smallListener{x.server.Listener}
	}
	x.server.Start()
	x.transport = &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err == nil && small {
			if tcp, ok := c.(*net.TCPConn); ok {
				err = tcp.SetReadBuffer(65536)
				if err != nil {
					_ = c.Close()
					return nil, err
				}
			}
		}
		if err != nil {
			return nil, err
		}
		return streamObservedConn{c, &x.received}, nil
	}}
	f.t.Cleanup(func() { x.transport.CloseIdleConnections(); x.server.Close() })
	return x
}

func streamTake[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-deadline(t).Done():
		t.Fatal("stream synchronization did not complete")
		var zero T
		return zero
	}
}

func (x *streamHTTP) pipe(t *testing.T, route, requestID string) (*io.PipeWriter, <-chan streamReply) {
	t.Helper()
	rd, wr := io.Pipe()
	t.Cleanup(func() { _ = wr.Close(); _ = rd.Close() })
	r, err := http.NewRequestWithContext(deadline(t), "POST", x.server.URL+"/notes.git/"+route, rd)
	must(t, err)
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-Request-Id", requestID)
	r.Header.Set("Content-Type", "application/x-"+route+"-request")
	done := make(chan streamReply, 1)
	go func() {
		response, err := (&http.Client{Transport: x.transport}).Do(r)
		done <- streamReply{response, err}
	}()
	return wr, done
}

func streamWrite(t *testing.T, wr *io.PipeWriter, data []byte) {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := wr.Write(data); done <- err }()
	must(t, streamTake(t, done))
}

func streamHeaders(t *testing.T, reply <-chan streamReply, kind string) *http.Response {
	t.Helper()
	r := streamTake(t, reply)
	must(t, r.err)
	t.Cleanup(func() { _ = r.response.Body.Close() })
	same(t, r.response.StatusCode, http.StatusOK)
	same(t, r.response.Header.Get("Content-Type"), "application/x-"+kind+"-result")
	return r.response
}

func streamBody(t *testing.T, response *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(response.Body)
	must(t, err)
	must(t, response.Body.Close())
	return b
}

func streamAborted(t *testing.T, reply <-chan streamReply) {
	t.Helper()
	r := streamTake(t, reply)
	if r.err != nil {
		return
	}
	defer func() { _ = r.response.Body.Close() }()
	if _, err := io.ReadAll(r.response.Body); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("aborted response was complete: %v", err)
	}
}

func streamPack(f *fixture, work, sha string) []byte {
	f.t.Helper()
	cmd := exec.CommandContext(deadline(f.t), f.executable, "pack-objects", "--stdout", "--revs")
	cmd.Dir, cmd.Env, cmd.Stdin = work, append([]string(nil), f.env...), strings.NewReader(sha+"\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("pack fixture: %v: %s", err, stderr.String())
	}
	if !bytes.HasPrefix(b, []byte("PACK")) {
		f.t.Fatal("git did not produce a pack")
	}
	return b
}

func streamPush(old, sha, ref string, pack []byte) []byte {
	command := packet(old+" "+sha+" "+ref+"\x00report-status side-band-64k quiet\n") + "0000"
	return append([]byte(command), pack...)
}

func streamObjectsAbsent(f *fixture, id, work, sha string) {
	f.t.Helper()
	for line := range strings.SplitSeq(strings.TrimSpace(f.gitRun(work, "rev-list", "--objects", sha)), "\n") {
		object := strings.Fields(line)[0]
		if output, err := f.gitTry("", "--git-dir="+f.store.Dir(id), "cat-file", "-e", object); err == nil {
			f.t.Fatalf("aborted push object %s is readable: %s", object, output)
		}
	}
}

func streamPushed(f *fixture, requestID, id string, want map[string][2]string) {
	f.t.Helper()
	seen := make(map[string][2]string)
	for _, e := range f.events() {
		if e.RequestID == requestID && e.Name == "repo.pushed" {
			ref := e.Attrs["ref"].(string)
			if _, duplicate := seen[ref]; duplicate {
				f.t.Fatalf("duplicate push event for %s", ref)
			}
			seen[ref] = [2]string{e.Attrs["old"].(string), e.Attrs["new"].(string)}
			same(f.t, e.Attrs["repo"], id)
		}
	}
	same(f.t, seen, want)
}

func TestStreamCompletePushBeforeInputEOF(t *testing.T) {
	// R-2BTM-M9KJ R-D750-DRE3 R-3QZ3-P5MW
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	sha := f.commit(work, "open input")
	trace := streamTraceConfig(f, "complete")
	x := streamServer(f, false)
	wr, reply := x.pipe(t, "git-receive-pack", "complete")
	streamWrite(t, wr, streamPush(zeroSHA, sha, "refs/heads/main", streamPack(f, work, sha)))
	r := streamHeaders(t, reply, "git-receive-pack")
	b := streamBody(t, r)
	if !bytes.Contains(b, []byte("unpack ok")) || !bytes.Contains(b, []byte("ok refs/heads/main")) {
		t.Fatalf("missing successful ref report: %q", b)
	}
	same(t, streamTake(t, x.done), "complete")
	sid := streamBackend(t, streamTrace(t, trace))
	code, exited := streamExit(streamTrace(t, trace), sid)
	same(t, exited, true)
	same(t, code, 0)
	same(t, f.refs(repo.ID), "refs/heads/main "+sha+"\n")
	streamPushed(f, "complete", repo.ID, map[string][2]string{"refs/heads/main": {zeroSHA, sha}})
	// The writer has remained open through receipt of the whole answer and refs.
	must(t, wr.Close())
}

func TestStreamPartialPushHeadersAndRename(t *testing.T) {
	// R-2BTM-M9KJ R-3TEW-GP4A R-3QZ3-P5MW
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	sha := f.commit(work, "renamed in flight")
	request := streamPush(zeroSHA, sha, "refs/heads/main", streamPack(f, work, sha))
	trace := streamTraceConfig(f, "rename")
	hold := &streamHoldObserver{t: t, trace: trace}
	x := streamServer(f, false, streamServerOption{hold: hold})
	wr, reply := x.pipe(t, "git-receive-pack", "rename")
	streamWrite(t, wr, request[:len(request)-16])
	streamAwaitChild(t, trace, "receive-pack")
	same(t, f.refs(repo.ID), "")
	same(t, x.received.Load(), int64(0))
	call := f.clock.take(t)
	same(t, call.duration, time.Duration(f.settings.OperationSeconds)*time.Second)
	_, err := f.store.Rename(deadline(t), repo.ID, "renamed")
	must(t, err)
	same(t, x.received.Load(), int64(0))
	streamWrite(t, wr, request[len(request)-16:])
	must(t, wr.Close())
	r := streamHeaders(t, reply, "git-receive-pack")
	b := streamBody(t, r)
	if !bytes.Contains(b, []byte("ok refs/heads/main")) {
		t.Fatalf("renamed push failed: %q", b)
	}
	same(t, streamTake(t, x.done), "rename")
	same(t, hold.early.Load(), false)
	same(t, f.refs(repo.ID), "refs/heads/main "+sha+"\n")
	streamPushed(f, "rename", repo.ID, map[string][2]string{"refs/heads/main": {zeroSHA, sha}})
	for _, e := range f.events() {
		if e.Name == "repo.pushed" {
			same(t, e.Attrs["repo"], repo.ID)
		}
	}
}

func TestStreamFetchHoldsResponseUntilInputEOF(t *testing.T) {
	// R-2BTM-M9KJ
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	sha := f.commit(work, "held fetch")
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	trace := streamTraceConfig(f, "held-fetch")
	hold := &streamHoldObserver{t: t, trace: trace}
	x := streamServer(f, false, streamServerOption{hold: hold})
	wr, reply := x.pipe(t, "git-upload-pack", "held-fetch")
	streamWrite(t, wr, []byte(packet("want "+sha+" side-band-64k ofs-delta no-progress\n")+"0000"+packet("done\n")))
	streamAwaitChild(t, trace, "upload-pack")
	same(t, f.refs(repo.ID), "refs/heads/main "+sha+"\n")
	same(t, x.received.Load(), int64(0))
	must(t, wr.Close())
	body := streamBody(t, streamHeaders(t, reply, "git-upload-pack"))
	pack := streamSideband(t, body)
	received := f.working("received")
	cmd := exec.CommandContext(deadline(t), f.executable, "index-pack", "--stdin")
	cmd.Dir, cmd.Env, cmd.Stdin = received, append([]string(nil), f.env...), bytes.NewReader(pack)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("received fetch is incomplete: %v: %s", err, output)
	}
	f.gitRun(received, "cat-file", "-e", sha)
	same(t, streamTake(t, x.done), "held-fetch")
	same(t, hold.early.Load(), false)
}

func TestStreamPushReleasesFullOutputBufferBeforeInputEOF(t *testing.T) {
	// R-2FHB-RKSM R-2BTM-M9KJ
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	sha := f.commit(work, "bounded report")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "receive.keepAlive", "0")
	trace := streamTraceConfig(f, "bounded-report")
	var commands, report strings.Builder
	report.WriteString(packet("unpack ok\n"))
	const refs = 8000
	for i := range refs {
		ref := fmt.Sprintf("refs/heads/branch-%05d-with-long-fixture-name", i)
		line := zeroSHA + " " + sha + " " + ref
		if i == 0 {
			line += "\x00report-status quiet"
		}
		commands.WriteString(packet(line + "\n"))
		report.WriteString(packet("ok " + ref + "\n"))
	}
	commands.WriteString("0000")
	report.WriteString("0000")
	x := streamServer(f, true)
	wr, reply := x.pipe(t, "git-receive-pack", "bounded-report")
	streamWrite(t, wr, append([]byte(commands.String()), streamPack(f, work, sha)...))
	r := streamHeaders(t, reply, "git-receive-pack")
	first := make([]byte, 16)
	_, err := io.ReadFull(r.Body, first)
	must(t, err)
	same(t, string(first), report.String()[:len(first)])
	sid := streamBackend(t, streamTrace(t, trace))
	if _, exited := streamExit(streamTrace(t, trace), sid); exited {
		t.Fatal("http-backend exited before the client resumed reading its report")
	}
	if len(report.String()) <= 2*65536+65536+git.CopyBufferSize {
		t.Fatal("report does not exceed the socket, pipe and copy buffers")
	}
	// Input is still open and git has not exited: only the bounded hold's
	// release can let these report bytes reach the client.
	first = append(first, streamBody(t, r)...)
	body := first
	same(t, string(body), report.String())
	must(t, wr.Close())
	same(t, streamTake(t, x.done), "bounded-report")
	code, exited := streamExit(streamTrace(t, trace), sid)
	same(t, exited, true)
	same(t, code, 0)
	same(t, len(strings.Split(strings.TrimSpace(f.refs(repo.ID)), "\n")), refs)
}

func TestStreamPushEventsMatchRefChanges(t *testing.T) {
	// R-3QZ3-P5MW
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	first := f.commit(work, "before")
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "config", "receive.denyDeleteCurrent", "ignore")
	second := f.commit(work, "after")
	pack := streamPack(f, work, second)
	x := streamServer(f, false)
	wr, reply := x.pipe(t, "git-receive-pack", "update")
	streamWrite(t, wr, streamPush(first, second, "refs/heads/main", pack))
	must(t, wr.Close())
	b := streamBody(t, streamHeaders(t, reply, "git-receive-pack"))
	if !bytes.Contains(b, []byte("ok refs/heads/main")) {
		t.Fatalf("ref update failed: %q", b)
	}
	same(t, streamTake(t, x.done), "update")
	streamPushed(f, "update", repo.ID, map[string][2]string{"refs/heads/main": {first, second}})
	wr, reply = x.pipe(t, "git-receive-pack", "delete")
	streamWrite(t, wr, []byte(packet(second+" "+zeroSHA+" refs/heads/main\x00report-status side-band-64k quiet\n")+"0000"))
	must(t, wr.Close())
	b = streamBody(t, streamHeaders(t, reply, "git-receive-pack"))
	if !bytes.Contains(b, []byte("ok refs/heads/main")) {
		t.Fatalf("ref deletion failed: %q", b)
	}
	same(t, streamTake(t, x.done), "delete")
	same(t, f.refs(repo.ID), "")
	streamPushed(f, "delete", repo.ID, map[string][2]string{"refs/heads/main": {second, zeroSHA}})
	wr, reply = x.pipe(t, "git-receive-pack", "two-refs")
	commands := packet(zeroSHA+" "+first+" refs/heads/topic\x00report-status side-band-64k quiet\n") + packet(zeroSHA+" "+second+" refs/tags/mark\n") + "0000"
	streamWrite(t, wr, append([]byte(commands), pack...))
	must(t, wr.Close())
	b = streamBody(t, streamHeaders(t, reply, "git-receive-pack"))
	for _, ref := range []string{"refs/heads/topic", "refs/tags/mark"} {
		if !bytes.Contains(b, []byte("ok "+ref)) {
			t.Fatalf("multi-ref push failed: %q", b)
		}
	}
	same(t, streamTake(t, x.done), "two-refs")
	streamPushed(f, "two-refs", repo.ID, map[string][2]string{"refs/heads/topic": {zeroSHA, first}, "refs/tags/mark": {zeroSHA, second}})
	// An advertisement starts git but cannot produce pushed events.
	before := len(f.events())
	f.request("GET", "/notes.git/info/refs?service=git-receive-pack", nil)
	same(t, len(ownEvents(f.events()[before:])), 0)
}

func TestStreamForcedUpdateAndRefusalEvents(t *testing.T) {
	// R-3QZ3-P5MW
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	first := f.commit(work, "force target")
	second := f.commit(work, "force source")
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	f.gitRun(work, "reset", "--hard", first)
	x := streamServer(f, false)
	before := strings.TrimSpace(f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "rev-parse", "refs/heads/main"))
	same(t, before, second)
	args := []string{"-c", "http.extraHeader=X-User-Id: alice", "-c", "http.extraHeader=X-Request-Id: forced", "push", "--force", x.server.URL + "/notes.git", "main"}
	f.gitRun(work, args...)
	after := strings.TrimSpace(f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "rev-parse", "refs/heads/main"))
	same(t, after, first)
	streamPushed(f, "forced", repo.ID, map[string][2]string{"refs/heads/main": {before, after}})
	for range 2 {
		same(t, f.clock.take(t).duration, time.Duration(f.settings.OperationSeconds)*time.Second)
	}
	same(t, len(f.clock.timers), 0)
	// This is a server refusal: --force gets past the client's fast-forward
	// check, but receive-pack itself refuses the configured non-fast-forward.
	f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "update-ref", "refs/heads/main", second)
	f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "config", "receive.denyNonFastForwards", "true")
	refsBefore := f.refs(repo.ID)
	args[3] = "http.extraHeader=X-Request-Id: refused"
	output, err := f.gitTry(work, args...)
	if err == nil || !bytes.Contains(output, []byte("non-fast-forward")) {
		t.Fatalf("receive-pack did not refuse the push: %v: %s", err, output)
	}
	same(t, f.refs(repo.ID), refsBefore)
	// Both advertisement and POST took an operation deadline, proving that
	// real http-backend ran the refused push rather than only the client's GET.
	for range 2 {
		same(t, f.clock.take(t).duration, time.Duration(f.settings.OperationSeconds)*time.Second)
	}
	same(t, len(f.clock.timers), 0)
	streamPushed(f, "refused", repo.ID, map[string][2]string{})
}

func TestStreamClientDisconnectWithOpenInput(t *testing.T) {
	// R-0FTA-SO1I R-3QZ3-P5MW R-0H17-6FS7
	for _, route := range []string{"git-receive-pack", "git-upload-pack"} {
		t.Run(route, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			work := f.working("source")
			sha := f.commit(work, "disconnect")
			trace := streamTraceConfig(f, "disconnect")
			x := streamServer(f, false)
			conn, err := net.Dial("tcp", x.server.Listener.Addr().String())
			must(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			ioDeadline, ok := deadline(t).Deadline()
			if !ok {
				t.Fatal("test failure budget has no deadline")
			}
			must(t, conn.SetDeadline(ioDeadline))
			_, err = fmt.Fprintf(conn, "POST /notes.git/%s HTTP/1.1\r\nHost: fixture\r\nX-User-Id: alice\r\nX-Request-Id: disconnect\r\nContent-Type: application/x-%s-request\r\nTransfer-Encoding: chunked\r\n\r\n", route, route)
			must(t, err)
			input := []byte(packet("want " + sha + " side-band-64k\n"))[:12]
			if route == "git-receive-pack" {
				full := streamPush(zeroSHA, sha, "refs/heads/main", streamPack(f, work, sha))
				input = full[:len(full)-16]
			}
			// An io.Pipe is the open request-body source; no terminating chunk is sent.
			rd, wr := io.Pipe()
			defer func() { _ = wr.Close(); _ = rd.Close() }()
			written := make(chan error, 1)
			go func() {
				b := make([]byte, len(input))
				_, readErr := io.ReadFull(rd, b)
				if readErr == nil {
					_, readErr = fmt.Fprintf(conn, "%x\r\n%s\r\n", len(b), b)
				}
				written <- readErr
			}()
			streamWrite(t, wr, input)
			must(t, streamTake(t, written))
			streamAwaitChild(t, trace, strings.TrimPrefix(route, "git-"))
			f.clock.take(t) // Merely observes the deadline; no timer is fired.
			must(t, conn.Close())
			same(t, streamTake(t, x.done), "disconnect")
			same(t, f.refs(repo.ID), "")
			if route == "git-receive-pack" {
				streamObjectsAbsent(f, repo.ID, work, sha)
			}
			same(t, len(ownEvents(f.events())), 0)
			assertIdle(t, f, repo.ID)
		})
	}
}

func TestStreamDeadlineAndRequestCancellation(t *testing.T) {
	// R-5ESE-4K93 R-5G0A-IBZS R-3UMS-UGUZ R-3QZ3-P5MW R-0H17-6FS7
	for _, cause := range []string{"deadline", "context"} {
		for _, route := range []string{"git-receive-pack", "git-upload-pack"} {
			t.Run(cause+route, func(t *testing.T) {
				f := setup(t)
				repo := f.create("notes")
				work := f.working("source")
				sha := f.commit(work, "cancelled")
				trace := streamTraceConfig(f, "cancel")
				x := streamServer(f, false)
				wr, reply := x.pipe(t, route, "cancel")
				input := []byte(packet("want " + sha + " side-band-64k\n"))[:12]
				if route == "git-receive-pack" {
					full := streamPush(zeroSHA, sha, "refs/heads/main", streamPack(f, work, sha))
					input = full[:len(full)-16]
				}
				streamWrite(t, wr, input)
				streamAwaitChild(t, trace, strings.TrimPrefix(route, "git-"))
				timer := f.clock.take(t)
				cancel := streamTake(t, x.cancels)
				if cause == "deadline" {
					timer.fire <- epoch
				} else {
					cancel()
				}
				same(t, streamTake(t, x.done), "cancel")
				if cause == "context" {
					timer.fire <- epoch // A later timeout cannot relabel context cancellation.
				}
				same(t, f.refs(repo.ID), "")
				if route == "git-receive-pack" {
					streamObjectsAbsent(f, repo.ID, work, sha)
					f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "fsck")
				}
				events := ownEvents(f.events())
				if cause == "deadline" {
					same(t, len(events), 1)
					same(t, events[0].Name, "operation.timed_out")
					op := "fetch"
					if route == "git-receive-pack" {
						op = "push"
					}
					same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": op, "limit": "operation_seconds"})
				} else {
					same(t, len(events), 0)
				}
				assertIdle(t, f, repo.ID)
				// Input is closed only after all cancellation observations.
				must(t, wr.Close())
				streamAborted(t, reply)
			})
		}
	}
}

func TestStreamAlreadyDeliveredDeadline(t *testing.T) {
	// R-5ESE-4K93
	f := setup(t)
	repo := f.create("notes")
	f.limits = limits.New(f.settings, limits.Clock{Now: f.clock.read, After: func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- epoch
		return ch
	}})
	x := streamServer(f, false)
	wr, reply := x.pipe(t, "git-receive-pack", "already")
	streamWrite(t, wr, []byte("0000"))
	same(t, streamTake(t, x.done), "already")
	r := streamTake(t, reply)
	if r.err == nil {
		defer func() { _ = r.response.Body.Close() }()
		if _, err := io.ReadAll(r.response.Body); err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("expired operation returned complete response: %v", err)
		}
	}
	same(t, f.refs(repo.ID), "")
	events := ownEvents(f.events())
	same(t, len(events), 1)
	same(t, events[0].Name, "operation.timed_out")
	assertIdle(t, f, repo.ID)
	must(t, wr.Close())
}

type streamDeadlineGate struct {
	context.Context
	headers atomic.Bool
	resume  chan struct{}
	once    sync.Once
}

func (g *streamDeadlineGate) Done() <-chan struct{} {
	ch := g.Context.Done()
	if g.headers.Load() {
		select {
		case <-g.resume:
		case <-ch:
		}
	}
	return ch
}

func (g *streamDeadlineGate) release() { g.once.Do(func() { close(g.resume) }) }

func TestStreamDeadlineWithOptionalObserverDelay(t *testing.T) {
	// R-5ESE-4K93 R-3QZ3-P5MW
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	sha := f.commit(work, "deadline arbitration")
	trace := filepath.Join(f.root, "deadline-trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	gate := &streamDeadlineGate{resume: make(chan struct{})}
	x := streamServer(f, false, streamServerOption{context: func(ctx context.Context) context.Context {
		gate.Context = ctx
		return gate
	}, header: func(status int) {
		if status == http.StatusOK {
			gate.headers.Store(true)
		}
	}})
	t.Cleanup(gate.release)
	wr, reply := x.pipe(t, "git-receive-pack", "incomplete-deadline")
	input := streamPush(zeroSHA, sha, "refs/heads/main", streamPack(f, work, sha))
	streamWrite(t, wr, input[:len(input)-16])
	streamAwaitChild(t, trace, "receive-pack")
	timer := f.clock.take(t)
	events := streamTrace(t, trace)
	sid := streamBackend(t, events)
	if _, exited := streamExit(events, sid); exited {
		t.Fatal("git exited before deadline delivery")
	}
	same(t, f.refs(repo.ID), "")
	// A context getter after public headers is an optional scheduling seam.
	// With the incomplete pack's headers held, timeout must also work before
	// that optional seam is ever reached.
	timer.fire <- epoch
	gate.release()
	same(t, streamTake(t, x.done), "incomplete-deadline")
	var timedOut []telemetry.Event
	for _, event := range f.events() {
		if event.RequestID == "incomplete-deadline" && event.Name == "operation.timed_out" {
			timedOut = append(timedOut, event)
		}
	}
	same(t, len(timedOut), 1)
	same(t, timedOut[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "push", "limit": "operation_seconds"})
	streamPushed(f, "incomplete-deadline", repo.ID, map[string][2]string{})
	same(t, f.refs(repo.ID), "")
	streamObjectsAbsent(f, repo.ID, work, sha)
	assertIdle(t, f, repo.ID)
	must(t, wr.Close())
	streamAborted(t, reply)
}

func streamAwaitNaturalExit(t *testing.T, trace, sid string) {
	t.Helper()
	pidStart := strings.LastIndex(sid, "-P")
	if pidStart < 0 {
		t.Fatalf("Git trace session lacks process identity: %s", sid)
	}
	pid, err := strconv.ParseInt(sid[pidStart+2:], 16, 32)
	must(t, err)
	ctx := deadline(t)
	for {
		code, exited := streamExit(streamTrace(t, trace), sid)
		if exited && errors.Is(syscall.Kill(int(pid), 0), syscall.ESRCH) {
			same(t, code, 0)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("real Git did not exit naturally")
		default:
			runtime.Gosched()
		}
	}
}

func TestStreamFetchTimerAfterNaturalExit(t *testing.T) {
	// R-0H17-6FS7
	f := setup(t)
	repo := f.create("notes")
	trace := filepath.Join(f.root, "late-fetch-trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	x := streamServer(f, false)
	body := packet("command=ls-refs\n") + "0001" + packet("peel\n") + packet("symrefs\n") + "0000"
	r, err := http.NewRequestWithContext(deadline(t), "POST", x.server.URL+"/notes.git/git-upload-pack", strings.NewReader(body))
	must(t, err)
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-Request-Id", "late-fetch")
	r.Header.Set("Git-Protocol", "version=2")
	r.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	response, err := (&http.Client{Transport: x.transport}).Do(r)
	must(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	same(t, response.StatusCode, http.StatusOK)
	// Empty ls-refs writes precisely a flush packet, so these four received
	// bytes are the complete Git body, without requiring handler completion.
	answer := make([]byte, 4)
	_, err = io.ReadFull(response.Body, answer)
	must(t, err)
	same(t, string(answer), "0000")
	timer := f.clock.take(t)
	sid := streamBackend(t, streamTrace(t, trace))
	streamAwaitNaturalExit(t, trace, sid)
	// Delivery is after actual natural exit and all stdout reached the client.
	// It is equally valid for ServeHTTP to have already returned here.
	timer.fire <- epoch
	same(t, string(streamBody(t, response)), "")
	same(t, streamTake(t, x.done), "late-fetch")
	var fetched []telemetry.Event
	for _, event := range f.events() {
		if event.RequestID == "late-fetch" && event.Name == "repo.fetched" {
			fetched = append(fetched, event)
		}
	}
	same(t, len(fetched), 1)
	same(t, fetched[0].Attrs, telemetry.Attrs{"repo": repo.ID, "bytes": int64(0)})
}

func streamTrace(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	must(t, err)
	var events []map[string]any
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		var event map[string]any
		if len(line) != 0 && json.Unmarshal(line, &event) == nil {
			events = append(events, event)
		}
	}
	return events
}

func streamTraceConfig(f *fixture, name string) string {
	f.t.Helper()
	trace := filepath.Join(f.root, name+"-trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	must(f.t, os.WriteFile(trace, nil, 0600))
	return trace
}

func streamAwaitChild(t *testing.T, trace, service string) {
	t.Helper()
	ctx := deadline(t)
	for {
		for _, event := range streamTrace(t, trace) {
			argv, _ := event["argv"].([]any)
			if event["event"] == "child_start" && len(argv) == 4 && argv[0] == "git" && argv[1] == service && argv[2] == "--stateless-rpc" && argv[3] == "." {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("git did not start %s", service)
		default:
			runtime.Gosched()
		}
	}
}

func streamBackend(t *testing.T, events []map[string]any) string {
	t.Helper()
	for _, e := range events {
		if e["event"] == "start" {
			argv, _ := e["argv"].([]any)
			for _, arg := range argv {
				if arg == "http-backend" {
					return e["sid"].(string)
				}
			}
		}
	}
	t.Fatal("http-backend start missing from trace")
	return ""
}

func streamExit(events []map[string]any, sid string) (int, bool) {
	for _, e := range events {
		if e["sid"] == sid && e["event"] == "exit" {
			return int(e["code"].(float64)), true
		}
	}
	return 0, false
}

func streamSideband(t *testing.T, body []byte) []byte {
	t.Helper()
	var pack bytes.Buffer
	for len(body) > 0 {
		if len(body) < 4 {
			t.Fatal("truncated packet prefix")
		}
		n, err := strconv.ParseInt(string(body[:4]), 16, 32)
		must(t, err)
		body = body[4:]
		if n <= 2 {
			continue
		}
		if n < 4 || int(n)-4 > len(body) {
			t.Fatal("truncated packet payload")
		}
		payload := body[:int(n)-4]
		if len(payload) > 0 && payload[0] == 1 {
			_, err := pack.Write(payload[1:])
			must(t, err)
		}
		body = body[int(n)-4:]
	}
	return pack.Bytes()
}

func TestStreamFetchBeforeGitExits(t *testing.T) {
	// R-L3QO-KSYG R-0H17-6FS7
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	write(t, filepath.Join(work, "large"), noise(3*1024*1024))
	f.gitRun(work, "add", "large")
	f.gitRun(work, "commit", "-m", "stream fixture")
	sha := strings.TrimSpace(f.gitRun(work, "rev-parse", "HEAD"))
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	trace := filepath.Join(f.root, "stream-trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	x := streamServer(f, true)
	wr, reply := x.pipe(t, "git-upload-pack", "large-fetch")
	streamWrite(t, wr, []byte(packet("want "+sha+" side-band-64k ofs-delta no-progress\n")+"0000"+packet("done\n")))
	must(t, wr.Close())
	r := streamHeaders(t, reply, "git-upload-pack")
	first := make([]byte, 128)
	_, err := io.ReadFull(r.Body, first)
	must(t, err)
	events := streamTrace(t, trace)
	sid := streamBackend(t, events)
	if _, exited := streamExit(events, sid); exited {
		t.Fatal("http-backend exited before client consumed the pack")
	}
	// Resume only after observing git still in flight behind the small sockets.
	rest := streamBody(t, r)
	first = append(first, rest...)
	body := first
	if len(body) < 2*1024*1024+git.CopyBufferSize {
		t.Fatalf("fetch answer too small to exercise backpressure: %d", len(body))
	}
	same(t, streamTake(t, x.done), "large-fetch")
	code, exited := streamExit(streamTrace(t, trace), sid)
	same(t, exited, true)
	same(t, code, 0)
	pack := streamSideband(t, body)
	// Git verifies the pack's object count and trailing checksum, so dropped or
	// altered body bytes cannot pass merely by leaving complete pkt-lines.
	received := f.working("received")
	cmd := exec.CommandContext(deadline(t), f.executable, "index-pack", "--stdin")
	cmd.Dir, cmd.Env, cmd.Stdin = received, append([]string(nil), f.env...), bytes.NewReader(pack)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("received pack is incomplete: %v: %s", err, output)
	}
	f.gitRun(received, "cat-file", "-e", sha)
	var fetched []telemetry.Event
	for _, e := range f.events() {
		if e.RequestID == "large-fetch" && e.Name == "repo.fetched" {
			fetched = append(fetched, e)
		}
	}
	same(t, len(fetched), 1)
	same(t, fetched[0].Attrs, telemetry.Attrs{"repo": repo.ID, "bytes": int64(len(pack))})
}

func TestStreamFetchByteCountsWithRealClients(t *testing.T) {
	// R-0H17-6FS7
	for _, version := range []string{"0", "2"} {
		t.Run(version, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			work := f.working("source")
			f.commit(work, "pack count")
			f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
			x := streamServer(f, false)
			trace := filepath.Join(f.root, "client.pack")
			cmd := exec.CommandContext(deadline(t), f.executable, "-c", "protocol.version="+version, "-c", "http.extraHeader=X-User-Id: alice", "-c", "http.extraHeader=X-Request-Id: counted", "clone", x.server.URL+"/notes.git", filepath.Join(f.root, "clone"))
			cmd.Env = append(append([]string(nil), f.env...), "GIT_TRACE_PACKFILE="+trace)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("clone: %v: %s", err, output)
			}
			pack, err := os.ReadFile(filepath.Clean(trace))
			must(t, err)
			if !bytes.HasPrefix(pack, []byte("PACK")) {
				t.Fatal("client pack trace is not a pack")
			}
			var total int64
			withPack, fetchCount := 0, 0
			for _, e := range f.events() {
				if e.RequestID == "counted" && e.Name == "repo.fetched" {
					fetchCount++
					n := e.Attrs["bytes"].(int64)
					total += n
					if n > 0 {
						withPack++
					}
				}
			}
			same(t, withPack, 1)
			wantCount := 1
			if version == "2" {
				wantCount = 2 // One ls-refs, then one fetch.
			}
			same(t, fetchCount, wantCount)
			same(t, total, int64(len(pack)))
		})
	}
	f := setup(t)
	f.create("notes")
	x := streamServer(f, false)
	// Protocol v2 ls-refs carries no pack but is still a successful upload-pack.
	r, err := http.NewRequestWithContext(deadline(t), "POST", x.server.URL+"/notes.git/git-upload-pack", strings.NewReader(packet("command=ls-refs\n")+"0001"+packet("peel\n")+packet("symrefs\n")+"0000"))
	must(t, err)
	r.Header.Set("X-User-Id", "alice")
	r.Header.Set("X-Request-Id", "no-pack")
	r.Header.Set("Git-Protocol", "version=2")
	r.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	response, err := (&http.Client{Transport: x.transport}).Do(r)
	must(t, err)
	streamBody(t, response)
	same(t, streamTake(t, x.done), "no-pack")
	events := ownEvents(f.events())
	same(t, len(events), 1)
	same(t, events[0].Name, "repo.fetched")
	same(t, events[0].Attrs["bytes"], int64(0))
	before := len(f.events())
	f.request("POST", "/notes.git/git-upload-pack", strings.NewReader("not a pkt-line"))
	same(t, len(ownEvents(f.events()[before:])), 0)
}

func TestStreamPushEventsBeforeLockHandover(t *testing.T) {
	// R-7W8P-EFFQ
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	sha := f.commit(work, "lock handover")
	pack := streamPack(f, work, sha)
	request := streamPush(zeroSHA, sha, "refs/heads/main", pack)
	blocked := make(chan struct{})
	resume := make(chan struct{})
	var handover atomic.Bool
	f.limits = limits.New(f.settings, limits.Clock{Now: func() time.Time {
		if handover.CompareAndSwap(true, false) {
			close(blocked)
			<-resume
		}
		return epoch
	}, After: f.clock.after})
	firstClosed := make(chan struct{})
	x := streamServer(f, false, streamServerOption{closeGate: firstClosed})
	first, firstReply := x.pipe(t, "git-receive-pack", "first")
	streamWrite(t, first, request[:len(request)-16])
	f.clock.take(t)
	second, secondReply := x.pipe(t, "git-receive-pack", "second")
	queue := f.clock.take(t)
	same(t, queue.duration, time.Duration(f.settings.QueueSeconds)*time.Second)
	same(t, f.limits.Pressure().Write.Queued, int64(1))
	same(t, f.limits.Busy(repo.ID), true)
	// No other limits Now call can occur until the first grant is released.
	handover.Store(true)
	streamWrite(t, first, request[len(request)-16:])
	must(t, first.Close())
	firstResponse := streamHeaders(t, firstReply, "git-receive-pack")
	close(firstClosed)
	streamTake(t, blocked)
	must(t, f.writer.Flush(deadline(t)))
	streamPushed(f, "first", repo.ID, map[string][2]string{"refs/heads/main": {zeroSHA, sha}})
	close(resume)
	streamBody(t, firstResponse)
	same(t, streamTake(t, x.done), "first")
	streamWrite(t, second, streamPush(zeroSHA, sha, "refs/heads/topic", pack))
	must(t, second.Close())
	streamBody(t, streamHeaders(t, secondReply, "git-receive-pack"))
	same(t, streamTake(t, x.done), "second")
	streamPushed(f, "second", repo.ID, map[string][2]string{"refs/heads/topic": {zeroSHA, sha}})
}

func TestStreamAbortedReportRecordsMovedRefs(t *testing.T) {
	// R-45SO-WFGI R-3QZ3-P5MW
	for _, ending := range []string{"context", "deadline"} {
		t.Run(ending, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			work := f.working("source")
			sha := f.commit(work, "many refs")
			pack := streamPack(f, work, sha)
			f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "receive.keepAlive", "0")
			trace := filepath.Join(f.root, "refs-trace.json")
			f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
			// Pace the injected writer clock by its own sink. This prevents incidental
			// delivery-queue overflow from hiding whether every ref event was emitted.
			f.writer.Shutdown(deadline(t), "replace")
			f.capture = new(telemetry.Capture)
			ctx := deadline(t)
			var writer *telemetry.Writer
			var pacing atomic.Bool
			pacing.Store(true)
			writer = telemetry.New(telemetry.Config{Service: "repos", Sink: f.capture, Stderr: io.Discard, Rand: new(sequence), Now: func() time.Time {
				if pacing.Load() && writer != nil {
					if err := writer.Flush(ctx); err != nil {
						panic(err)
					}
				}
				return epoch
			}})
			f.writer = writer
			t.Cleanup(func() { pacing.Store(false); writer.Shutdown(deadline(t), "test") })
			var commands strings.Builder
			const refs = 8000
			for i := range refs {
				line := zeroSHA + " " + sha + fmt.Sprintf(" refs/heads/branch-%05d-with-long-fixture-name", i)
				if i == 0 {
					line += "\x00report-status quiet"
				}
				commands.WriteString(packet(line + "\n"))
			}
			commands.WriteString("0000")
			input := append([]byte(commands.String()), pack...)
			x := streamServer(f, true)
			wr, reply := x.pipe(t, "git-receive-pack", "many")
			streamWrite(t, wr, input)
			r := streamHeaders(t, reply, "git-receive-pack")
			prefix := make([]byte, 16)
			_, err := io.ReadFull(r.Body, prefix)
			must(t, err)
			// receive.keepAlive=0 makes these bytes the ref report, after updates.
			beforeAbort := f.refs(repo.ID)
			if beforeAbort == "" {
				t.Fatal("report arrived before any ref moved")
			}
			traceEvents := streamTrace(t, trace)
			backend := streamBackend(t, traceEvents)
			if _, exited := streamExit(traceEvents, backend); exited {
				t.Fatal("git completed its report before the client resumed reading")
			}
			select {
			case id := <-x.done:
				t.Fatalf("report fit buffers; handler already returned for %s", id)
			default:
			}
			timer := f.clock.take(t)
			cancel := streamTake(t, x.cancels)
			if ending == "context" {
				cancel()
			} else {
				timer.fire <- epoch
			}
			same(t, streamTake(t, x.done), "many")
			want := make(map[string][2]string)
			for line := range strings.SplitSeq(strings.TrimSpace(f.refs(repo.ID)), "\n") {
				fields := strings.Fields(line)
				same(t, fields[1], sha)
				want[fields[0]] = [2]string{zeroSHA, sha}
			}
			if len(want) < 5000 {
				t.Fatalf("only %d refs moved", len(want))
			}
			streamPushed(f, "many", repo.ID, want)
			assertIdle(t, f, repo.ID)
			must(t, wr.Close())
		})
	}
}
