package tools_test

import (
	"bufio"
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
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
)

func TestRunRecordsAndFiles(t *testing.T) {
	// R-IZHY-8LMQ R-J5LG-5GC7 R-7HJB-831B R-7IR7-LUS0 R-T179-4S0M R-7MEW-R603 R-3PI5-7IPB R-3QQ1-LAG0 R-T9QJ-T67H R-5I4T-HD04 R-5KKM-8WHI R-5N0F-0FYW R-5O8B-E7PL R-YM61-H3W4
	h := setup(t, "print(1)\n")
	sc := h.create("job")
	if got := string(object(t, h.call("runs", tools.RunsArgs{Name: "job"}))); got != `{"runs":[]}` {
		t.Fatal(got)
	}
	override := "missing"
	wireInput := `{ "b" : [ ], "a":1 }`
	base := h.http.Transport
	h.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		b, e := io.ReadAll(r.Body)
		if e != nil {
			return nil, e
		}
		_ = r.Body.Close()
		b = bytes.Replace(b, []byte(`{"b":[],"a":1}`), []byte(wireInput), 1)
		r.Body = io.NopCloser(bytes.NewReader(b))
		r.ContentLength = int64(len(b))
		return base.RoundTrip(r)
	})
	start := decode[tools.Started](t, h.raw("run", `{"name":"job","ref":"missing","input":{ "b" : [ ], "a":1 }}`, caller()))
	h.http.Transport = base
	if start.Status != store.StatusFailed || start.SHA != nil || start.Reason == nil || *start.Reason != store.ReasonCommitMissing {
		t.Fatal(start)
	}
	r, e := h.st.RunByID(context.Background(), start.ID)
	must(t, e)
	if r.Script != sc.ID || r.User != "alice" || r.RequestID != caller().RequestID || r.Ref != override || r.Trigger != store.TriggerManual {
		t.Fatal(r)
	}
	folder := h.core.Folder(r)
	folderRoot, e := os.OpenRoot(folder)
	must(t, e)
	defer func() { _ = folderRoot.Close() }()
	input, e := folderRoot.ReadFile(runs.InputFile)
	must(t, e)
	if string(input) != `{ "b" : [ ], "a":1 }` {
		t.Fatal(string(input))
	}
	original, e := h.st.Find(context.Background(), "alice", "job")
	must(t, e)
	if original.Ref != "main" {
		t.Fatal(original)
	}
	write := func(name string, b []byte) {
		p := filepath.Join(folder, name)
		must(t, os.MkdirAll(filepath.Dir(p), 0700))
		must(t, os.WriteFile(p, b, 0600))
	}
	write("stdout", append([]byte("Zürich — 東京\n"), 0xff, 0xe2, 0x82))
	write("stderr", []byte("err\n"))
	write("out/report.csv", bytes.Repeat([]byte{'x'}, 13))
	write("out/charts/sales.svg", bytes.Repeat([]byte{'x'}, 42))
	write("out/.cache/state", []byte("ok"))
	must(t, os.Symlink("report.csv", filepath.Join(folder, "out/latest.csv")))
	must(t, os.Symlink("charts", filepath.Join(folder, "out/graphs")))
	v := decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: r.ID}))
	if v.Status != "failed" || v.Reason == nil || *v.Reason != "commit_missing" || v.Finished == nil || v.ExitCode != nil || v.SHA != nil || v.Stdout == nil || *v.Stdout != "Zürich — 東京\n���" || v.Stderr == nil || *v.Stderr != "err\n" || v.Files == nil || !reflect.DeepEqual(*v.Files, []tools.File{{Path: ".cache/state", Size: 2}, {Path: "charts/sales.svg", Size: 42}, {Path: "report.csv", Size: 13}}) {
		t.Fatal(v)
	}
	// A stream symlink is never read; directory symlinks never contribute files.
	target := filepath.Join(h.root, "target")
	must(t, os.WriteFile(target, []byte("secret"), 0600))
	must(t, os.Remove(filepath.Join(folder, "stdout")))
	must(t, os.Symlink(target, filepath.Join(folder, "stdout")))
	v = decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: r.ID}))
	if v.Stdout == nil || *v.Stdout != "" {
		t.Fatal(v)
	}
	listed := decode[tools.RunList](t, h.call("runs", tools.RunsArgs{Name: "job"}))
	if len(listed.Runs) != 1 || listed.Runs[0].ID != r.ID || listed.Runs[0].Finished == nil {
		t.Fatal(listed)
	}
	shown := decode[tools.Script](t, h.call("show", tools.ShowArgs{Name: "job"}))
	if shown.LastRun == nil || shown.LastRun.ID != r.ID || shown.LastRun.ExitCode != nil || shown.LastRun.Started != "2025-01-02T02:04:05Z" {
		t.Fatal(shown)
	}
	refusal(t, h.call("cancel", tools.CancelArgs{Run: r.ID}), fmt.Sprintf(tools.Ended, r.ID))
	for _, id := range []string{sc.ID, "job", "run_2222222222222222"} {
		refusal(t, h.call("result", tools.ResultArgs{Run: id}), fmt.Sprintf(tools.MissingRun, id))
		refusal(t, h.call("cancel", tools.CancelArgs{Run: id}), fmt.Sprintf(tools.MissingRun, id))
	}
	refusal(t, h.raw("result", fmt.Sprintf(`{"run":%q}`, r.ID), identity.Caller{UserID: "bob"}), fmt.Sprintf(tools.MissingRun, r.ID))
	must(t, os.RemoveAll(folder))
	v = decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: r.ID}))
	if v.FilesGone == nil || !*v.FilesGone || v.Stdout != nil || v.Stderr != nil || v.Files != nil {
		t.Fatal(v)
	}
	assertWire(t, h.call("runs", tools.RunsArgs{Name: "job"}), `{"runs":[`+expectedEntry(t, r)+`]}`)
}

func TestLiveRunCancelAndDelete(t *testing.T) {
	// R-5PG7-RZGA R-0VVB-3QKN R-C2RY-13RX R-54PX-9VUH R-T9QJ-T67H
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	defer func() { _ = listener.Close() }()
	address := listener.Addr().(*net.TCPAddr)
	script := fmt.Sprintf("import socket\nprint('hello',flush=True)\ns=socket.create_connection(('127.0.0.1',%d))\ns.sendall(b'ready')\ns.recv(1)\n", address.Port)
	h := setup(t, script)
	sc := h.create("job")
	start := decode[tools.Started](t, h.call("run", tools.RunArgs{Name: "job", Input: json.RawMessage(`{}`)}))
	if start.Status != "running" || start.SHA == nil || start.Reason != nil {
		t.Fatal(start)
	}
	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer deadlineCancel()
	stop := context.AfterFunc(deadlineCtx, func() { _ = listener.Close() })
	defer stop()
	conn, e := listener.Accept()
	must(t, e)
	defer func() { _ = conn.Close() }()
	stopConn := context.AfterFunc(deadlineCtx, func() { _ = conn.Close() })
	defer stopConn()
	ready := make([]byte, 5)
	_, e = io.ReadFull(conn, ready)
	must(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var v tools.RunResult
	for {
		v = decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: start.ID}))
		if v.Stdout != nil && *v.Stdout == "hello\n" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("stdout never arrived")
		default:
			runtime.Gosched()
		}
	}
	if v.Status != "running" || v.Finished != nil || v.ExitCode != nil || v.StdoutBytes != 6 {
		t.Fatal(v)
	}
	r, e := h.st.RunByID(context.Background(), start.ID)
	must(t, e)
	b, e := os.ReadFile(filepath.Join(h.core.Folder(r), "input.json"))
	must(t, e)
	if string(b) != "{}" {
		t.Fatal(string(b))
	}
	ended := decode[tools.RunEntry](t, h.call("cancel", tools.CancelArgs{Run: r.ID}))
	if ended.Status != "killed" || ended.Finished == nil || ended.ExitCode != nil {
		t.Fatal(ended)
	}
	if h.core.Gone(r) {
		t.Fatal("cancel removed files")
	}
	if _, e = h.st.RunByID(context.Background(), r.ID); e != nil {
		t.Fatal(e)
	}
	second := decode[tools.Started](t, h.raw("run", `{"name":"job"}`, caller()))
	secondConn, e := listener.Accept()
	must(t, e)
	defer func() { _ = secondConn.Close() }()
	stopSecond := context.AfterFunc(deadlineCtx, func() { _ = secondConn.Close() })
	defer stopSecond()
	_, e = io.ReadFull(secondConn, make([]byte, 5))
	must(t, e)
	d := decode[tools.Deleted](t, h.call("delete", tools.DeleteArgs{Name: "job"}))
	_, e = h.st.RunByID(context.Background(), second.ID)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if d.ID != sc.ID {
		t.Fatal(d)
	}
	_, e = h.st.RunByID(context.Background(), r.ID)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(h.root, "runs", sc.ID)); !os.IsNotExist(e) {
		t.Fatal("deleted folder survived", e)
	}
}

func TestDrainAndRunFolderFailure(t *testing.T) {
	// R-YJQ8-PKEQ R-YKY5-3C5F R-8FOH-XNSR
	h := setup(t, "print(1)\n")
	sc := h.create("job")
	must(t, os.MkdirAll(filepath.Join(h.root, "runs"), 0700))
	root, e := os.OpenRoot(h.root)
	must(t, e)
	defer func() { _ = root.Close() }()
	must(t, root.Chmod("runs", 0500))
	u := caller()
	u.RequestID = "folder-failure"
	refusal(t, h.raw("run", `{"name":"job"}`, u), store.Unreachable)
	rr, e := h.st.Runs(context.Background(), sc.ID)
	must(t, e)
	if len(rr) != 0 {
		t.Fatal(rr)
	}
	if _, e = os.Stat(filepath.Join(h.root, "runs", sc.ID)); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	must(t, root.Chmod("runs", 0700))
	h.core.Drain(context.Background())
	refusal(t, h.call("run", tools.RunArgs{Name: "job", Input: json.RawMessage(`{}`)}), runs.Stopping)
	refusal(t, h.call("run", tools.RunArgs{Name: "missing", Input: json.RawMessage(`{}`)}), fmt.Sprintf(tools.MissingScript, "missing"))
	h.flush()
	n := 0
	for _, e := range h.capture.Events() {
		if e.RequestID == u.RequestID {
			n++
			if e.Name != "tool.called" || e.Attrs["tool"] != "run" || e.Attrs["kind"] != "additive" || e.Attrs["outcome"] != "error" {
				t.Fatal(e)
			}
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
}

func TestDeleteDuringPendingRun(t *testing.T) {
	// R-5LSI-MO87
	h := setup(t, "print(1)\n")
	sc := h.create("job")
	fifo := filepath.Join(h.root, "held-archive")
	must(t, syscall.Mkfifo(fifo, 0600))
	n := 0
	h.after = func(time.Duration) <-chan time.Time {
		n++
		if n == 2 {
			h.env = append(h.env, "GIT_TRACE="+fifo)
		}
		return make(chan time.Time)
	}
	removeTrace(t, h)
	answer := make(chan mcp.Result, 1)
	go func() { answer <- h.raw("run", `{"name":"job"}`, caller()) }()
	lifecycleUntil(t, func() bool {
		b, err := os.ReadFile(h.trace)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			var e struct {
				Event string
				Argv  []string
			}
			if json.Unmarshal([]byte(line), &e) == nil && e.Event == "start" {
				for _, a := range e.Argv {
					if a == "archive" {
						return true
					}
				}
			}
		}
		return false
	})
	select {
	case a := <-answer:
		t.Fatal("archive was not held", a)
	default:
	}
	deleted := decode[tools.Deleted](t, h.call("delete", tools.DeleteArgs{Name: "job"}))
	if !deleted.Deleted || deleted.ID != sc.ID {
		t.Fatal(deleted)
	}
	// Release the held trace endpoint after public delete canceled the pending run.
	fd, err := syscall.Open(fifo, syscall.O_RDWR|syscall.O_NONBLOCK, 0600)
	must(t, err)
	must(t, syscall.Close(fd))
	select {
	case a := <-answer:
		refusal(t, a, fmt.Sprintf(tools.MissingScript, "job"))
	case <-time.After(10 * time.Second):
		t.Fatal("pending run did not answer")
	}
	rr, e := h.st.Runs(context.Background(), sc.ID)
	must(t, e)
	if len(rr) != 0 {
		t.Fatal(rr)
	}
}

func TestCancelRacesExit(t *testing.T) {
	// R-YNDX-UVMT
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	defer func() { _ = listener.Close() }()
	script := fmt.Sprintf("import socket\ns=socket.create_connection(('127.0.0.1',%d))\ns.recv(1)\nraise SystemExit(7)\n", listener.Addr().(*net.TCPAddr).Port)
	h := setup(t, script)
	h.create("job")
	start := decode[tools.Started](t, h.raw("run", `{"name":"job"}`, caller()))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	conn, e := listener.Accept()
	must(t, e)
	defer func() { _ = conn.Close() }()
	ready := make(chan struct{})
	answer := make(chan mcp.Result, 1)
	go func() { <-ready; answer <- h.call("cancel", tools.CancelArgs{Run: start.ID}) }()
	close(ready)
	_, e = conn.Write([]byte("x"))
	if e != nil && !errors.Is(e, net.ErrClosed) {
		t.Log(e)
	}
	select {
	case r := <-answer:
		record, e := h.st.RunByID(context.Background(), start.ID)
		must(t, e)
		if r.IsError() {
			refusal(t, r, fmt.Sprintf(tools.Ended, start.ID))
			if record.Status != "exited" || record.ExitCode != 7 {
				t.Fatal(record)
			}
		} else {
			v := decode[tools.RunEntry](t, r)
			if v.Status != "killed" || record.Status != "killed" {
				t.Fatal(v, record)
			}
		}
	case <-ctx.Done():
		t.Fatal("cancel race did not end")
	}
}

// toolCalledCount ignores unrelated telemetry while observing tools/list.

func lifecycleUntil(t *testing.T, predicate func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for !predicate() {
		select {
		case <-ctx.Done():
			t.Fatal("lifecycle condition did not arrive")
		default:
			runtime.Gosched()
		}
	}
}

func TestDrainRunVariantsAndArrival(t *testing.T) {
	// R-YJQ8-PKEQ
	h := setup(t, "print(1)\n")
	h.create("own")
	_, e := h.st.Create(context.Background(), store.Draft{Owner: "bob", Name: "foreign", Repo: "rep_1111111111111111", Ref: "main"})
	must(t, e)
	arrived, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-release
		identity.Require(h.server).ServeHTTP(w, r)
	}))
	defer srv.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL, HTTPClient: srv.Client(), Name: "arrival", Version: "test"})
	answer := make(chan mcp.Result, 1)
	go func() {
		r, e := client.CallTool(context.Background(), caller(), "run", json.RawMessage(`{"name":"own"}`))
		if e != nil {
			t.Error(e)
		}
		answer <- r
	}()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("request did not arrive")
	}
	h.core.Drain(context.Background())
	close(release)
	select {
	case r := <-answer:
		refusal(t, r, runs.Stopping)
	case <-time.After(10 * time.Second):
		t.Fatal("arrived run did not answer")
	}
	for _, name := range []string{"own", "foreign", "absent"} {
		for _, extras := range []string{"", `,"ref":"missing"`, `,"input":{"a":1}`, `,"ref":"missing","input":{"a":1}`} {
			before := snapshot(t, h)
			removeTrace(t, h)
			u := caller()
			u.RequestID = "drain-variant"
			want := fmt.Sprintf(tools.MissingScript, name)
			if name == "own" {
				want = runs.Stopping
			}
			refusal(t, h.raw("run", `{"name":"`+name+`"`+extras+`}`, u), want)
			unchanged(t, h, before)
			noGit(t, h)
			noDomain(t, h, u.RequestID)
		}
	}
}

func TestStartAnswersMetadataAndDefaultInput(t *testing.T) {
	// R-T9QJ-T67H R-5KKM-8WHI
	for _, mode := range []string{"running-omitted", "running-empty", "unresolved", "resolved-failure"} {
		t.Run(mode, func(t *testing.T) {
			h, sc, _, conn := paused(t)
			stillLive(t, conn)
			h.command(h.repo, "branch", "alternate", "main")
			_ = object(t, h.call("update", tools.UpdateArgs{Name: sc.Name, Ref: "alternate"}))
			before, e := h.st.Runs(context.Background(), sc.ID)
			must(t, e)
			args := `{"name":"live-job"}`
			ref := "alternate"
			if mode == "running-empty" {
				args = `{"name":"live-job","ref":""}`
			}
			if mode == "unresolved" {
				args = `{"name":"live-job","ref":"missing"}`
				ref = "missing"
			}
			if mode == "resolved-failure" {
				// Resolve succeeds; the test-owned git entry disappears before archive starts.
				n := 0
				h.after = func(time.Duration) <-chan time.Time {
					n++
					if n == 2 {
						must(t, os.Remove(h.gitExec))
					}
					return make(chan time.Time)
				}
			}
			u := caller()
			u.RequestID = "start-" + mode
			answer := h.raw("run", args, u)
			started := decode[tools.Started](t, answer)
			r, e := h.st.RunByID(context.Background(), started.ID)
			must(t, e)
			want := []member{{"id", r.ID}, {"status", started.Status}}
			if started.SHA != nil {
				want = append(want, member{"sha", *started.SHA})
			}
			if started.Reason != nil {
				want = append(want, member{"reason", *started.Reason})
			}
			assertWire(t, answer, wire(t, want...))
			if r.Script != sc.ID || r.User != u.UserID || r.RequestID != u.RequestID || r.Trigger != store.TriggerManual || r.Ref != ref {
				t.Fatal(r)
			}
			sha := ""
			if started.SHA != nil {
				sha = *started.SHA
			}
			if r.SHA != sha {
				t.Fatal(r, started)
			}
			if mode == "unresolved" || mode == "resolved-failure" {
				if started.Status != store.StatusFailed || started.Reason == nil || r.Status != store.StatusFailed || r.Reason != *started.Reason {
					t.Fatal(r, started)
				}
				if (mode == "unresolved") != (started.SHA == nil) {
					t.Fatal(started)
				}
			} else if started.Status != store.StatusRunning || started.SHA == nil || started.Reason != nil {
				t.Fatal(started)
			}
			after, e := h.st.Runs(context.Background(), sc.ID)
			must(t, e)
			if len(after) != len(before)+1 {
				t.Fatal("run count", before, after)
			}
			input, e := os.ReadFile(filepath.Join(h.core.Folder(r), runs.InputFile))
			must(t, e)
			if !bytes.Equal(input, []byte("{}")) {
				t.Fatal(input)
			}
		})
	}
}

func TestOwnEndedCancelPreservesRecords(t *testing.T) {
	// R-YM61-H3W4
	for i, status := range []string{store.StatusExited, store.StatusKilled, store.StatusTimedOut, store.StatusFailed} {
		t.Run(status, func(t *testing.T) {
			h, sc, live, conn := paused(t)
			r := store.Run{ID: fmt.Sprintf("run_%016x", 800+i), Script: sc.ID, SHA: strings.Repeat("b", 40), User: "alice", Ref: "main", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: live.Started}
			if status == store.StatusFailed {
				r.Status = status
				r.SHA = ""
				r.Reason = store.ReasonCommitMissing
				r.Finished = live.Started
			}
			r, e := h.st.AddRun(context.Background(), r)
			must(t, e)
			if status != store.StatusFailed {
				code := 0
				if status == store.StatusExited {
					code = 7
				}
				_, e = h.st.FinishRun(context.Background(), r.ID, store.Ending{Status: status, Finished: live.Started.Add(time.Second), ExitCode: code})
				must(t, e)
			}
			before := snapshot(t, h)
			removeTrace(t, h)
			u := caller()
			u.RequestID = "ended-" + status
			refusal(t, h.raw("cancel", fmt.Sprintf(`{"run":%q}`, r.ID), u), fmt.Sprintf(tools.Ended, r.ID))
			unchanged(t, h, before)
			noGit(t, h)
			noDomain(t, h, u.RequestID)
			stillLive(t, conn)
		})
	}
}

func TestCancelKillsGroupAndRetainsBytes(t *testing.T) {
	// R-0VVB-3QKN
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	defer func() { _ = listener.Close() }()
	script := fmt.Sprintf(`import os,socket
child=os.fork()==0
role=b'C' if child else b'R'
if not child:
 print('retained output',flush=True)
 open(os.path.join(os.environ['IKIGENBA_OUT_DIR'],'artifact'),'wb').write(b'kept\x00bytes')
s=socket.create_connection(('127.0.0.1',%d))
s.sendall(role+str(os.getpid()).encode()+b'\n')
while True:
 c=s.recv(1)
 if c==b'p': s.sendall(b'P')
 else: break
`, listener.Addr().(*net.TCPAddr).Port)
	h := setup(t, script)
	sc := h.create("group-job")
	other := h.create("other-job")
	type peer struct {
		conn net.Conn
		pid  string
	}
	acceptGroup := func() []peer {
		var peers []peer
		for range 2 {
			c, e := listener.Accept()
			must(t, e)
			deadline, _ := ctx.Deadline()
			must(t, c.SetDeadline(deadline))
			line, e := bufio.NewReader(c).ReadString('\n')
			must(t, e)
			peers = append(peers, peer{c, strings.TrimSpace(line[1:])})
			t.Cleanup(func() { _ = c.Close() })
		}
		return peers
	}
	start := decode[tools.Started](t, h.raw("run", fmt.Sprintf(`{"name":%q}`, sc.Name), caller()))
	group := acceptGroup()
	second := decode[tools.Started](t, h.raw("run", fmt.Sprintf(`{"name":%q}`, other.Name), caller()))
	unrelated := acceptGroup()
	third := decode[tools.Started](t, h.raw("run", fmt.Sprintf(`{"name":%q}`, sc.Name), caller()))
	unrelated = append(unrelated, acceptGroup()...)
	r, e := h.st.RunByID(context.Background(), start.ID)
	must(t, e)
	r2, e := h.st.RunByID(context.Background(), second.ID)
	must(t, e)
	r3, e := h.st.RunByID(context.Background(), third.ID)
	must(t, e)
	read := func(r store.Run, name string) []byte {
		root, e := os.OpenRoot(h.core.Folder(r))
		must(t, e)
		defer func() { _ = root.Close() }()
		b, e := root.ReadFile(name)
		must(t, e)
		return b
	}
	lifecycleUntil(t, func() bool {
		return string(read(r, "stdout")) == "retained output\n" && string(read(r2, "stdout")) == "retained output\n" && string(read(r3, "stdout")) == "retained output\n"
	})
	beforeOut := read(r, "stdout")
	beforeArtifact := read(r, "out/artifact")
	beforeOther := snapshot(t, h)
	answer := h.call("cancel", tools.CancelArgs{Run: r.ID})
	ended, e := h.st.RunByID(context.Background(), r.ID)
	must(t, e)
	assertWire(t, answer, expectedEntry(t, ended))
	if ended.Status != store.StatusKilled || ended.Finished.IsZero() {
		t.Fatal(ended)
	}
	for _, p := range group {
		b := make([]byte, 1)
		_, e := p.conn.Read(b)
		if !errors.Is(e, io.EOF) {
			t.Fatal("group connection remains", p.pid, e)
		}
		lifecycleUntil(t, func() bool { return lifecycleProcessGone(p.pid) })
	}
	if !bytes.Equal(read(r, "stdout"), beforeOut) || !bytes.Equal(read(r, "out/artifact"), beforeArtifact) || h.core.Gone(r) {
		t.Fatal("cancel changed retained files")
	}
	after := snapshot(t, h)
	for path, f := range beforeOther.Files {
		if (strings.HasPrefix(path, other.ID+"/") || strings.HasPrefix(path, sc.ID+"/"+r3.ID+"/")) && !reflect.DeepEqual(after.Files[path], f) {
			t.Fatal("unrelated file changed", path)
		}
	}
	r2after, e := h.st.RunByID(context.Background(), r2.ID)
	must(t, e)
	if !reflect.DeepEqual(r2, r2after) {
		t.Fatal("unrelated record changed", r2, r2after)
	}
	r3after, e := h.st.RunByID(context.Background(), r3.ID)
	must(t, e)
	if !reflect.DeepEqual(r3, r3after) {
		t.Fatal("same-script unrelated record changed", r3, r3after)
	}
	for _, p := range unrelated {
		stillLive(t, p.conn)
	}
}

func lifecycleProcessGone(pid string) bool {
	root, e := os.OpenRoot("/proc")
	if e != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	b, e := root.ReadFile(filepath.Join(pid, "stat"))
	if os.IsNotExist(e) {
		return true
	}
	if e != nil {
		return false
	}
	_, tail, ok := strings.Cut(string(b), ") ")
	return ok && strings.HasPrefix(tail, "Z ")
}

func TestQueuedToolsLifecycle(t *testing.T) {
	// R-T179-4S0M R-T9QJ-T67H R-YJQ8-PKEQ R-TH1Y-3SNN R-YM61-H3W4 R-0X37-HIBC R-TJHQ-VC51 R-YDMQ-SPP9
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	defer func() { _ = listener.Close() }()
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	closeListener := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer closeListener()
	script := fmt.Sprintf("import socket,os,json\nvalue=json.load(open(os.environ['IKIGENBA_INPUT']))\nif 'marker' in value: open(value['marker'],'w').write('started')\ns=socket.create_connection(('127.0.0.1',%d))\ns.sendall(b'ready')\ns.recv(1)\n", listener.Addr().(*net.TCPAddr).Port)
	h := setup(t, script, func(c *runs.Config) { c.MaxActive = 1; c.MaxQueued = 10; c.KeepCount = 100 })
	sc := h.create("job")
	active := decode[tools.Started](t, h.call("run", tools.RunArgs{Name: sc.Name, Input: json.RawMessage(`{}`)}))
	conn, e := listener.Accept()
	must(t, e)
	defer func() { _ = conn.Close() }()
	deadline, _ := ctx.Deadline()
	must(t, conn.SetDeadline(deadline))
	_, e = io.ReadFull(conn, make([]byte, 5))
	must(t, e)
	if active.Status != store.StatusRunning {
		t.Fatal(active)
	}
	var queued []store.Run
	var markers []string
	for i := range 10 {
		marker := filepath.Join(h.root, fmt.Sprintf("queued-marker-%d", i))
		markers = append(markers, marker)
		u := caller()
		u.RequestID = "queued-request"
		answer := h.raw("run", fmt.Sprintf(`{"name":"job","input":{"marker":%q}}`, marker), u)
		start := decode[tools.Started](t, answer)
		r, e := h.st.RunByID(context.Background(), start.ID)
		must(t, e)
		assertWire(t, answer, wire(t, member{"id", r.ID}, member{"status", store.StatusQueued}, member{"sha", r.SHA}))
		if r.Status != store.StatusQueued || r.Script != sc.ID || r.User != u.UserID || r.RequestID != u.RequestID || r.Trigger != store.TriggerManual || r.Ref != sc.Ref || r.SHA == "" {
			t.Fatal(r)
		}
		queued = append(queued, r)
		v := decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: r.ID}))
		if v.Status != store.StatusQueued || v.Finished != nil || v.ExitCode != nil || v.StdoutBytes != 0 || v.StderrBytes != 0 || v.Stdout == nil || *v.Stdout != "" || v.Stderr == nil || *v.Stderr != "" || v.Files == nil || len(*v.Files) != 0 {
			t.Fatal(v)
		}
	}
	removeTrace(t, h)
	refusal(t, h.call("run", tools.RunArgs{Name: "job", Input: json.RawMessage(`{}`)}), fmt.Sprintf(runs.QueueFull, int64(10)))
	refusal(t, h.call("run", tools.RunArgs{Name: "missing", Input: json.RawMessage(`{}`)}), fmt.Sprintf(tools.MissingScript, "missing"))
	noGit(t, h)
	q := queued[0]
	before, e := os.ReadDir(h.core.Folder(q))
	must(t, e)
	ended := decode[tools.RunEntry](t, h.call("cancel", tools.CancelArgs{Run: q.ID}))
	if ended.Status != store.StatusKilled || ended.Finished == nil || ended.ExitCode != nil {
		t.Fatal(ended)
	}
	after, e := os.ReadDir(h.core.Folder(q))
	must(t, e)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("queued cancellation changed folder")
	}
	result := decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: q.ID}))
	if result.Status != store.StatusKilled || result.StdoutBytes != 0 || result.StderrBytes != 0 || result.Stdout == nil || *result.Stdout != "" || result.Stderr == nil || *result.Stderr != "" || result.Files == nil || len(*result.Files) != 0 {
		t.Fatal(result)
	}
	_ = object(t, h.call("delete", tools.DeleteArgs{Name: sc.Name}))
	for _, r := range queued {
		refusal(t, h.call("result", tools.ResultArgs{Run: r.ID}), fmt.Sprintf(tools.MissingRun, r.ID))
		refusal(t, h.call("cancel", tools.CancelArgs{Run: r.ID}), fmt.Sprintf(tools.MissingRun, r.ID))
	}
	for _, marker := range markers {
		if _, e := os.Stat(marker); !os.IsNotExist(e) {
			t.Fatal("queued run started", marker, e)
		}
	}
	// Ending the active run cannot launch any deleted or cancelled queued run.
	_, e = conn.Read(make([]byte, 1))
	if !errors.Is(e, io.EOF) {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(h.root, "runs", sc.ID)); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	_ = object(t, h.call("list", tools.ListArgs{}))
	noGit(t, h)
}

// R-0X37-HIBC
func TestQueuedCancelStaysKilledAfterRunningRunEnds(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	deadline, _ := ctx.Deadline()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	defer func() { _ = listener.Close() }()
	must(t, listener.(*net.TCPListener).SetDeadline(deadline))
	script := fmt.Sprintf("import socket,os,json\nv=json.load(open(os.environ['IKIGENBA_INPUT']))\nif 'marker' in v: open(v['marker'],'w').write('started')\ns=socket.create_connection(('127.0.0.1',%d))\ns.sendall(b'ready')\ns.recv(1)\n", listener.Addr().(*net.TCPAddr).Port)
	h := setup(t, script, func(c *runs.Config) { c.MaxActive = 1; c.KeepCount = 100 })
	sc := h.create("job")
	active := decode[tools.Started](t, h.call("run", tools.RunArgs{Name: sc.Name, Input: json.RawMessage(`{}`)}))
	conn, e := listener.Accept()
	must(t, e)
	defer func() { _ = conn.Close() }()
	must(t, conn.SetDeadline(deadline))
	_, e = io.ReadFull(conn, make([]byte, 5))
	must(t, e)
	marker := filepath.Join(h.root, "canceled-marker")
	q := decode[tools.Started](t, h.call("run", tools.RunArgs{Name: sc.Name, Input: json.RawMessage(fmt.Sprintf(`{"marker":%q}`, marker))}))
	next := decode[tools.Started](t, h.call("run", tools.RunArgs{Name: sc.Name, Input: json.RawMessage(`{}`)}))
	aBefore, e := h.st.RunByID(context.Background(), active.ID)
	must(t, e)
	nBefore, e := h.st.RunByID(context.Background(), next.ID)
	must(t, e)
	_ = decode[tools.RunEntry](t, h.call("cancel", tools.CancelArgs{Run: q.ID}))
	for _, before := range []store.Run{aBefore, nBefore} {
		after, e := h.st.RunByID(context.Background(), before.ID)
		must(t, e)
		if after != before {
			t.Fatal("cancellation changed another record", before, after)
		}
	}
	_, e = conn.Write([]byte("x"))
	must(t, e)
	// The successor reaching its socket proves the earlier slot was released.
	successor, e := listener.Accept()
	must(t, e)
	defer func() { _ = successor.Close() }()
	must(t, successor.SetDeadline(deadline))
	_, e = io.ReadFull(successor, make([]byte, 5))
	must(t, e)
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatal("canceled process started", e)
	}
	result := decode[tools.RunResult](t, h.call("result", tools.ResultArgs{Run: q.ID}))
	if result.Status != store.StatusKilled || result.StdoutBytes != 0 || result.StderrBytes != 0 || result.Stdout == nil || *result.Stdout != "" || result.Stderr == nil || *result.Stderr != "" || result.Files == nil || len(*result.Files) != 0 {
		t.Fatal(result)
	}
	_ = object(t, h.call("delete", tools.DeleteArgs{Name: sc.Name}))
}

func TestUnavailableRunRefusal(t *testing.T) {
	// R-YJQ8-PKEQ R-YDMQ-SPP9
	h := setup(t, "print(1)\n", func(c *runs.Config) { c.Unavailable = "no delegation" })
	h.create("job")
	removeTrace(t, h)
	refusal(t, h.call("run", tools.RunArgs{Name: "job", Input: json.RawMessage(`{}`)}), fmt.Sprintf(runs.NoRuns, "no delegation"))
	refusal(t, h.call("run", tools.RunArgs{Name: "missing", Input: json.RawMessage(`{}`)}), fmt.Sprintf(tools.MissingScript, "missing"))
	h.core.Drain(context.Background())
	refusal(t, h.call("run", tools.RunArgs{Name: "job", Input: json.RawMessage(`{}`)}), runs.Stopping)
	noGit(t, h)
}
