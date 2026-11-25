package boxmcp_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

type socketStep struct {
	line  string
	reply func(*net.UnixConn) error
}

func socketScript(t *testing.T, listener *net.UnixListener, steps ...socketStep) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for _, step := range steps {
			c, err := listener.AcceptUnix()
			if err != nil {
				done <- err
				return
			}
			if step.line != "" {
				var line string
				line, err = readLine(c)
				if err == nil && line != step.line+"\n" {
					t.Errorf("daemon received %q, want %q with LF", line, step.line)
				}
			}
			if err == nil {
				err = step.reply(c)
			}
			_ = c.Close()
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	return done
}

// The environment override and variable lookup in Run are observable. The
// production timeout cannot be measured under the offline test discipline.
func TestRunSocketOverride(t *testing.T) {
	l := unixListener(t)
	list := []byte(`{"jsonrpc":"2.0","id":7,"result":{"tools":[]}}`)
	done := socketScript(t, l, socketStep{listRequest, func(c *net.UnixConn) error {
		return boxmcp.WriteAnswer(c, boxmcp.Answer{Message: list})
	}})
	var out bytes.Buffer
	var lookedUp []string
	err := boxmcp.Run(t.Context(), strings.NewReader(listRequest), &out, func(key string) string {
		lookedUp = append(lookedUp, key)
		return l.Addr().String()
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lookedUp, []string{boxmcp.EnvSocket}) {
		t.Fatalf("environment lookups = %v", lookedUp)
	}
	if want := string(list) + "\n"; out.String() != want {
		t.Fatalf("output %q, want %q", out.String(), want)
	}
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}

// R-PSBY-J567, R-7RKC-MGUS, R-PNGD-027F
func TestForwardAndRefusal(t *testing.T) {
	l := unixListener(t)
	message := `{ "x":"<&>", "id":99 }`
	refusal := "the daemon said <&>\nand continued"
	done := socketScript(t, l,
		socketStep{listRequest, func(c *net.UnixConn) error {
			_, err := io.WriteString(c, `{"futureField":[1],"message":`+message+"}\n")
			return err
		}},
		socketStep{callRequest, func(c *net.UnixConn) error {
			_, err := io.WriteString(c, `{"refusal":"fallback","message":null}`+"\n")
			return err
		}},
		socketStep{listRequest, func(c *net.UnixConn) error {
			return boxmcp.WriteAnswer(c, boxmcp.Answer{Refusal: refusal})
		}},
	)
	var out bytes.Buffer
	input := strings.NewReader(strings.Join([]string{listRequest, callRequest, listRequest}, "\n"))
	cfg := boxmcp.Config{Socket: l.Addr().String(), Timeout: time.Second}
	if err := boxmcp.Serve(t.Context(), input, &out, cfg); err != nil {
		t.Fatal(err)
	}
	first, err := out.ReadString('\n')
	if err != nil || first != message+"\n" {
		t.Fatalf("relayed message %q, want %q with LF, error %v", first, message, err)
	}
	got := responses(t, out.String())
	if len(got) != 2 {
		t.Fatalf("got %d responses after the relay, want 2", len(got))
	}
	expectCallRefusal(t, got[0], "fallback")
	expectError(t, got[1], "7", boxmcp.CodeRefused, refusal)
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}

// R-PSBY-J567
func TestRefusalWhenDaemonNeverReadsRequest(t *testing.T) {
	l := unixListener(t)
	refusal := "busy"
	done := socketScript(t, l, socketStep{reply: func(c *net.UnixConn) error {
		if err := c.CloseRead(); err != nil {
			return err
		}
		return boxmcp.WriteAnswer(c, boxmcp.Answer{Refusal: refusal})
	}})
	call := strings.TrimSuffix(callRequest, "}") + `,"params":{"arg":"` + strings.Repeat("x", 60<<10) + `"}}`
	got := serve(t, strings.NewReader(call), boxmcp.Config{Socket: l.Addr().String(), Timeout: time.Second})
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1", len(got))
	}
	expectCallRefusal(t, got[0], refusal)
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}

// R-POO9-DTY4
func TestUnavailableAndMalformedAnswers(t *testing.T) {
	invalid := []string{
		"",
		`{"refusal":"no terminating LF"}`,
		"{\n",
		"\n",
		"null\n",
		"[]\n",
		"{}\n",
		`{"message":null}` + "\n",
		`{"message":{},"refusal":"both"}` + "\n",
		`{"message":[]}` + "\n",
		`{"message":"not object"}` + "\n",
		`{"refusal":1}` + "\n",
	}
	var steps []socketStep
	var requests []string
	for _, reply := range invalid {
		steps = append(steps, socketStep{callRequest, func(c *net.UnixConn) error {
			_, err := io.WriteString(c, reply)
			return err
		}})
		requests = append(requests, callRequest)
	}

	l := unixListener(t)
	done := socketScript(t, l, steps...)
	got := serve(t, strings.NewReader(strings.Join(requests, "\n")), boxmcp.Config{Socket: l.Addr().String(), Timeout: time.Second})
	if len(got) != len(requests) {
		t.Fatalf("got %d responses, want %d", len(got), len(requests))
	}
	for _, response := range got {
		expectCallRefusal(t, response, boxmcp.NotAnswering)
	}
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	got = serve(t, strings.NewReader(listRequest), boxmcp.Config{Socket: missing, Timeout: time.Second})
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1", len(got))
	}
	expectError(t, got[0], "7", boxmcp.CodeRefused, boxmcp.NotAnswering)
}

// R-POO9-DTY4
func TestAnswerDeadline(t *testing.T) {
	l := unixListener(t)
	done := socketScript(t, l, socketStep{callRequest, func(c *net.UnixConn) error {
		// The daemon keeps the unanswered connection open until the client leaves.
		_, err := c.Read(make([]byte, 1))
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}})
	got := serve(t, strings.NewReader(callRequest), boxmcp.Config{Socket: l.Addr().String(), Timeout: 10 * time.Millisecond})
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1", len(got))
	}
	expectCallRefusal(t, got[0], boxmcp.NotAnswering)
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}

// R-POO9-DTY4, R-PR42-5DFI
func TestLongAnswerIsAbandonedWithoutAllocation(t *testing.T) {
	l := unixListener(t)
	left := make(chan int64, 1)
	const offered = 256 << 20
	done := socketScript(t, l, socketStep{callRequest, func(c *net.UnixConn) error {
		written, err := io.CopyN(c, spaces{}, offered)
		left <- written
		if err == nil {
			return errors.New("oversized answer was fully written")
		}
		return nil
	}})
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := serve(t, strings.NewReader(callRequest), boxmcp.Config{Socket: l.Addr().String(), Timeout: 5 * time.Second})
	runtime.ReadMemStats(&after)
	if used := after.TotalAlloc - before.TotalAlloc; used >= 16<<20 {
		t.Errorf("allocated %d bytes over an oversized answer", used)
	}
	if len(got) != 1 {
		t.Fatalf("got %d responses, want 1", len(got))
	}
	expectCallRefusal(t, got[0], boxmcp.NotAnswering)
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	if n := await(t, left); n >= offered {
		t.Fatalf("peer wrote %d bytes before closure", n)
	}
}

// R-POO9-DTY4
func TestAnswerLineBoundary(t *testing.T) {
	l := unixListener(t)
	keepOpen := make(chan struct{})
	edge := `{"message":{"at":"limit"}}`
	done := socketScript(t, l,
		socketStep{callRequest, func(c *net.UnixConn) error {
			_, err := io.WriteString(c, strings.Repeat(" ", boxmcp.MaxAnswerLine-len(edge))+edge+"\n")
			return err
		}},
		socketStep{callRequest, func(c *net.UnixConn) error {
			if _, err := io.CopyN(c, spaces{}, boxmcp.MaxAnswerLine+1); err != nil {
				return err
			}
			<-keepOpen
			return nil
		}},
	)
	defer func() {
		close(keepOpen)
		if err := await(t, done); err != nil {
			t.Error(err)
		}
	}()
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cfg := boxmcp.Config{Socket: l.Addr().String(), Timeout: time.Minute}
	if err := boxmcp.Serve(ctx, strings.NewReader(callRequest+"\n"+callRequest), &out, cfg); err != nil {
		t.Fatal(err)
	}
	line, err := out.ReadString('\n')
	if err != nil || line != `{"at":"limit"}`+"\n" {
		t.Fatalf("answer at limit was %q, error %v", line, err)
	}
	got := responses(t, out.String())
	if len(got) != 1 {
		t.Fatalf("got %d responses to oversized answer, want 1", len(got))
	}
	expectCallRefusal(t, got[0], boxmcp.NotAnswering)
}

// R-PPW5-RLOT, R-PR42-5DFI
func TestSequentialStdio(t *testing.T) {
	for _, later := range []struct {
		name, line string
	}{
		{"local ping", pingRequest},
		{"second forward", listRequest},
	} {
		t.Run(later.name, func(t *testing.T) {
			testSequentialStdio(t, later.line)
		})
	}
}

func testSequentialStdio(t *testing.T, later string) {
	t.Helper()
	l := unixListener(t)
	firstReceived := make(chan struct{})
	laterStarted := make(chan struct{})
	firstAnswering := make(chan struct{})
	peer := socketScript(t, l, socketStep{callRequest, func(c *net.UnixConn) error {
		close(firstReceived)
		select {
		case <-laterStarted:
		case <-t.Context().Done():
			return t.Context().Err()
		}
		close(firstAnswering)
		return boxmcp.WriteAnswer(c, boxmcp.Answer{Refusal: "first"})
	}})
	in, input := io.Pipe()
	output, out := io.Pipe()
	defer func() { _ = in.Close() }()
	defer func() { _ = input.Close() }()
	defer func() { _ = output.Close() }()
	defer func() { _ = out.Close() }()

	stdout := &sequentialOutput{Writer: out, started: make(chan struct{})}
	stdin := &sequentialInput{Reader: in, completed: &stdout.completed}
	done := make(chan error, 1)
	go func() {
		done <- boxmcp.Serve(t.Context(), stdin, stdout, boxmcp.Config{Socket: l.Addr().String(), Timeout: 3 * time.Second})
	}()
	first := make(chan error, 1)
	go func() {
		_, err := io.WriteString(input, callRequest+"\n")
		first <- err
	}()
	if err := await(t, first); err != nil {
		t.Fatal(err)
	}
	await(t, firstReceived)

	// Watch for a second accepted connection independently of the first.
	// The local case uses a probe to detect any unexpected extra dial.
	const probe = "end"
	secondPeer := socketScript(t, l, socketStep{reply: func(c *net.UnixConn) error {
		select {
		case <-firstAnswering:
			if stdout.completed.Load() == 0 {
				t.Error("second connection accepted before the first response was fully written")
			}
		default:
			t.Error("second connection accepted while the first was unanswered")
		}
		line, err := readLine(c)
		if err != nil {
			return err
		}
		if later == pingRequest {
			if line != probe+"\n" {
				return errors.New("local ping dialed the daemon")
			}
			return nil
		}
		if line != later+"\n" {
			return errors.New("second socket did not receive the second request")
		}
		return boxmcp.WriteAnswer(c, boxmcp.Answer{Refusal: "second"})
	}})

	next := make(chan error, 1)
	go func() {
		close(laterStarted)
		_, err := io.WriteString(input, later+"\n")
		next <- err
	}()
	// The first frame has started writing, but without a reader it cannot
	// finish. A speculative input read during this interval is recorded.
	await(t, stdout.started)
	read := make(chan string, 2)
	go func() {
		defer close(read)
		scan := bufio.NewScanner(output)
		for range 2 {
			if !scan.Scan() {
				break
			}
			read <- scan.Text() + "\n"
		}
		if err := scan.Err(); err != nil {
			t.Error(err)
		}
	}()
	firstResponse := responses(t, await(t, read))
	if len(firstResponse) != 1 {
		t.Fatalf("got %d first responses, want one", len(firstResponse))
	}
	expectCallRefusal(t, firstResponse[0], "first")
	secondResponse := responses(t, await(t, read))
	if len(secondResponse) != 1 {
		t.Fatalf("got %d second responses, want one", len(secondResponse))
	}
	if later == pingRequest {
		expectResult(t, secondResponse[0], "42", "{}")
		c, err := (&net.Dialer{}).DialContext(t.Context(), "unix", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.WriteString(c, probe+"\n")
		_ = c.Close()
		if err != nil {
			t.Fatal(err)
		}
	} else {
		expectError(t, secondResponse[0], "7", boxmcp.CodeRefused, "second")
	}
	if err := await(t, next); err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
	if stdin.early.Load() {
		t.Error("read the next request before its predecessor's response was fully written")
	}
	if err := await(t, peer); err != nil {
		t.Fatal(err)
	}
	if err := await(t, secondPeer); err != nil {
		t.Fatal(err)
	}
}

type sequentialInput struct {
	io.Reader
	completed *atomic.Int32
	lines     atomic.Int32
	early     atomic.Bool
}

func (r *sequentialInput) Read(p []byte) (int, error) {
	if r.lines.Load() > r.completed.Load() {
		r.early.Store(true)
	}
	n, err := r.Reader.Read(p)
	if bytes.IndexByte(p[:n], '\n') >= 0 {
		r.lines.Add(1)
	}
	return n, err
}

type sequentialOutput struct {
	io.Writer
	started   chan struct{}
	once      sync.Once
	completed atomic.Int32
}

func (w *sequentialOutput) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	n, err := w.Writer.Write(p)
	if bytes.IndexByte(p[:n], '\n') >= 0 {
		w.completed.Add(1)
	}
	return n, err
}

// R-7YVQ-X3AY
func TestCancellationInterruptsInputAndSocket(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		in, writer := io.Pipe()
		defer func() { _ = in.Close() }()
		defer func() { _ = writer.Close() }()
		started := make(chan struct{})
		cancelWhileBlocked(t, signaledRead{Reader: in, started: started}, "unused", started)
	})
	t.Run("socket", func(t *testing.T) {
		l := unixListener(t)
		started := make(chan struct{})
		peer := socketScript(t, l, socketStep{callRequest, func(c *net.UnixConn) error {
			close(started)
			_, err := c.Read(make([]byte, 1))
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}})
		cancelWhileBlocked(t, strings.NewReader(callRequest+"\n"), l.Addr().String(), started)
		if err := await(t, peer); err != nil {
			t.Fatal(err)
		}
	})
}

// R-7YVQ-X3AY
func TestCancellationFromOutputWriter(t *testing.T) {
	for _, input := range []string{pingRequest, pingRequest + "\n" + listRequest + "\n"} {
		ctx, cancel := context.WithCancel(t.Context())
		var out cancelingWriter
		out.cancel = cancel
		cfg := boxmcp.Config{Socket: "unused", Timeout: time.Second}
		err := boxmcp.Serve(ctx, strings.NewReader(input), &out, cfg)
		cancel()
		if !errors.Is(err, context.Canceled) || out.afterCancel {
			t.Fatalf("Serve returned %v, write after cancellation = %v", err, out.afterCancel)
		}
		got := responses(t, out.String())
		if len(got) != 1 {
			t.Fatalf("got %d complete responses when writer canceled, want one", len(got))
		}
		expectResult(t, got[0], "42", "{}")
	}
}

type cancelingWriter struct {
	bytes.Buffer
	cancel      context.CancelFunc
	afterCancel bool
}

func (w *cancelingWriter) Write(p []byte) (int, error) {
	if w.Len() != 0 {
		w.afterCancel = true
	}
	n, err := w.Buffer.Write(p)
	w.cancel()
	return n, err
}

type signaledRead struct {
	io.Reader
	started chan struct{}
}

func (r signaledRead) Read(p []byte) (int, error) {
	close(r.started)
	return r.Reader.Read(p)
}

func cancelWhileBlocked(t *testing.T, reader io.Reader, socket string, started <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- boxmcp.Serve(ctx, reader, &out, boxmcp.Config{Socket: socket, Timeout: time.Minute})
	}()
	await(t, started)
	cancel()
	if err := await(t, done); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("cancellation returned %v and wrote %q", err, out.String())
	}
}

// R-7FDC-SRFU
func TestOversizedInputAnswersBeforeSkippingItsTail(t *testing.T) {
	input, writer := io.Pipe()
	defer func() { _ = input.Close() }()
	defer func() { _ = writer.Close() }()

	lineSent := make(chan error, 1)
	go func() {
		_, err := io.CopyN(writer, spaces{}, protocol.MaxBoxRequest+1)
		lineSent <- err
	}()

	output, out := io.Pipe()
	defer func() { _ = output.Close() }()
	defer func() { _ = out.Close() }()
	done := make(chan error, 1)
	go func() {
		cfg := boxmcp.Config{Socket: "unused", Timeout: time.Second}
		done <- boxmcp.Serve(t.Context(), input, out, cfg)
	}()

	if err := await(t, lineSent); err != nil {
		t.Fatal(err)
	}
	reply := make(chan string, 1)
	go func() {
		line, err := bufio.NewReader(output).ReadString('\n')
		if err != nil {
			t.Error(err)
		}
		reply <- line
	}()
	got := responses(t, await(t, reply))
	if len(got) != 1 {
		t.Fatalf("got %d responses before the tail, want 1", len(got))
	}
	expectError(t, got[0], "null", boxmcp.CodeInvalidRequest, boxmcp.MsgTooLarge)
	go func() {
		_, err := io.WriteString(writer, "ignored tail\n")
		_ = writer.CloseWithError(err)
	}()
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}
