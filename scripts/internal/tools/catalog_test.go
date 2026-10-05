package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
	"github.com/ikigenba/ikigenba/scripts/internal/tools"
)

func TestCatalogLifecycle(t *testing.T) {
	// R-J0PU-MDDF R-J1XR-0544 R-J4DJ-ROLI R-3LUG-27H8 R-52A4-ICD3 R-53I0-W43S R-54PX-9VUH R-55XT-NNL6 R-575Q-1FBV R-58DM-F72K R-59LI-SYT9 R-O7UW-EYEM R-5ATF-6QJY R-5C1B-KIAN R-5D97-YA1C R-C2RY-13RX
	h := setup(t, "print(1)\n")
	if got := string(object(t, h.call("list", tools.ListArgs{}))); got != `{"scripts":[]}` {
		t.Fatal(got)
	}
	a := h.create("zulu")
	b := h.create("alpha")
	if a.Created != "2025-01-02T02:04:05Z" || a.LastRun != nil || a.Ref != "main" || !store.ValidScriptID(a.ID) {
		t.Fatal(a)
	}
	ss := decode[tools.ScriptList](t, h.call("list", tools.ListArgs{}))
	if len(ss.Scripts) != 2 || ss.Scripts[0].Name != b.Name || ss.Scripts[1].ID != a.ID {
		t.Fatal(ss)
	}
	if got := decode[tools.Script](t, h.call("show", tools.ShowArgs{Name: a.Name})); !reflect.DeepEqual(got, a) {
		t.Fatal(got, a)
	}
	for _, n := range []string{"missing", a.ID, "Not valid"} {
		refusal(t, h.call("show", tools.ShowArgs{Name: n}), "no script named '"+n+"'")
	}
	other := identity.Caller{UserID: "bob", RequestID: "other"}
	refusal(t, h.raw("show", `{"name":"zulu"}`, other), "no script named 'zulu'")
	if got := string(object(t, h.raw("list", `{}`, other))); got != `{"scripts":[]}` {
		t.Fatal(got)
	}
	bad := "..bad"
	refusal(t, h.call("create", tools.CreateArgs{Name: "Not valid", Repo: "missing", Ref: &bad}), "invalid name 'Not valid'")
	refusal(t, h.call("create", tools.CreateArgs{Name: a.Name, Repo: "missing", Ref: &bad}), "a script named 'zulu' already exists")
	refusal(t, h.call("create", tools.CreateArgs{Name: "new", Repo: "missing", Ref: &bad}), "no repository 'missing'")
	refusal(t, h.call("create", tools.CreateArgs{Name: "new", Repo: "rep_1111111111111111", Ref: &bad}), "invalid ref '..bad'")
	trace, e := os.ReadFile(h.trace)
	must(t, e)
	if strings.Contains(string(trace), "rev-parse") || strings.Contains(string(trace), "archive") {
		t.Fatal(string(trace))
	}
	must(t, os.Remove(h.trace))
	refusal(t, h.call("update", tools.UpdateArgs{Name: "missing", Ref: bad}), "no script named 'missing'")
	refusal(t, h.call("update", tools.UpdateArgs{Name: a.Name, Ref: bad}), "invalid ref '..bad'")
	unchanged := decode[tools.Script](t, h.call("update", tools.UpdateArgs{Name: a.Name, Ref: "main"}))
	if !reflect.DeepEqual(a, unchanged) {
		t.Fatal(unchanged)
	}
	updated := decode[tools.Script](t, h.call("update", tools.UpdateArgs{Name: a.Name, Ref: "future"}))
	if updated.Ref != "future" || updated.ID != a.ID || updated.Repo != a.Repo {
		t.Fatal(updated)
	}
	d := decode[tools.Deleted](t, h.call("delete", tools.DeleteArgs{Name: a.Name}))
	if !d.Deleted || d.ID != a.ID {
		t.Fatal(d)
	}
	refusal(t, h.call("delete", tools.DeleteArgs{Name: a.Name}), "no script named 'zulu'")
	taken, e := h.st.Taken(context.Background(), a.Name)
	must(t, e)
	if taken {
		t.Fatal("name still taken")
	}
	// R-4YMF-D150 R-4XEI-Z9EB
	if _, e = os.Stat(h.trace); !os.IsNotExist(e) {
		t.Fatal("read/update/delete ran git", e)
	}
	entries, e := os.ReadDir(filepath.Join(h.root, "runs"))
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("catalog created folders")
	}
}

func TestConcurrentCreate(t *testing.T) {
	// R-575Q-1FBV
	h := setup(t, "print(1)\n")
	var wg, ready sync.WaitGroup
	ready.Add(8)
	start := make(chan struct{})
	answers := make(chan mcp.Result, 8)
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			ready.Done()
			<-start
			answers <- h.call("create", tools.CreateArgs{Name: "job", Repo: "rep_1111111111111111"})
		})
	}
	ready.Wait()
	close(start)
	wg.Wait()
	close(answers)
	n := 0
	for r := range answers {
		if r.IsError() {
			refusal(t, r, "a script named 'job' already exists")
		} else {
			n++
			_ = decode[tools.Script](t, r)
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
}

// catalogOwnerTrace checks every git start, rather than just banning two commands.
func catalogOwnerTrace(t *testing.T, h *fixture, count int) {
	t.Helper()
	if count == 0 {
		noGit(t, h)
		return
	}
	b, e := os.ReadFile(h.trace)
	must(t, e)
	starts := 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var event struct {
			Event string
			Argv  []string
		}
		must(t, json.Unmarshal([]byte(line), &event))
		if event.Event != "start" {
			continue
		}
		starts++
		if !reflect.DeepEqual(event.Argv[1:], []string{"config", "--file", filepath.Join(h.src.RepoDir("rep_1111111111111111"), "config"), "--get", "ikigenba.owner"}) {
			t.Fatal("unexpected git arguments", event.Argv)
		}
	}
	if starts != count {
		t.Fatalf("git starts = %d, want %d", starts, count)
	}
}

func catalogSeed(t *testing.T, h *fixture, name, owner, runID string) store.Script {
	t.Helper()
	ctx := context.Background()
	s, e := h.st.Create(ctx, store.Draft{Owner: owner, Name: name, Repo: "rep_1111111111111111", Ref: "main"})
	must(t, e)
	r, e := h.st.AddRun(ctx, store.Run{ID: runID, Script: s.ID, SHA: strings.Repeat("c", 40), Ref: "main", User: owner, RequestID: "fixture", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: s.Created})
	must(t, e)
	_, e = h.st.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, ExitCode: 9, Finished: s.Created.Add(time.Second), StdoutBytes: 6, StderrBytes: 5, Truncated: true})
	must(t, e)
	dir := h.core.Folder(r)
	must(t, os.MkdirAll(filepath.Join(dir, "out", "nested"), 0700))
	must(t, os.WriteFile(filepath.Join(dir, "stdout"), []byte("before"), 0600))
	must(t, os.WriteFile(filepath.Join(dir, "stderr"), []byte("error"), 0400))
	must(t, os.WriteFile(filepath.Join(dir, "out", "nested", "artifact"), []byte("preserve this"), 0600))
	must(t, os.Symlink("nested/artifact", filepath.Join(dir, "out", "link")))
	s, e = h.st.Find(ctx, owner, name)
	must(t, e)
	return s
}

func catalogOmit(s state, id string, folders bool) state {
	ss := []store.Script{}
	for _, sc := range s.Scripts {
		if sc.ID != id {
			ss = append(ss, sc)
		}
	}
	s.Scripts = ss
	rr := []store.Run{}
	for _, r := range s.Runs {
		if r.Script != id {
			rr = append(rr, r)
		}
	}
	s.Runs = rr
	if folders {
		delete(s.Files, ".") // the shared parent changes when its child is removed
		for p := range s.Files {
			if p == id || strings.HasPrefix(p, id+"/") {
				delete(s.Files, p)
			}
		}
	}
	return s
}

func TestCatalogCreateStoredRecordAndExistingEntries(t *testing.T) {
	// R-59LI-SYT9 R-O7UW-EYEM
	h := setup(t, "print(1)\n")
	catalogSeed(t, h, "survivor", "alice", "run_aaaaaaaaaaaaaaaa")
	catalogSeed(t, h, "foreign", "bob", "run_bbbbbbbbbbbbbbbb")
	for i, ref := range []*string{nil, new("future-branch")} {
		before := snapshot(t, h)
		removeTrace(t, h)
		name := fmt.Sprintf("created-%d", i)
		reply := h.call("create", tools.CreateArgs{Name: name, Repo: "rep_1111111111111111", Ref: ref})
		catalogOwnerTrace(t, h, 1)
		stored, e := h.st.Find(context.Background(), "alice", name)
		must(t, e)
		wantRef := "main"
		if ref != nil {
			wantRef = *ref
		}
		want := store.Script{ID: stored.ID, Name: name, Owner: "alice", Repo: "rep_1111111111111111", Ref: wantRef, Created: time.Date(2025, 1, 2, 2, 4, 5, 0, time.UTC)}
		if !store.ValidScriptID(stored.ID) || !reflect.DeepEqual(stored, want) {
			t.Fatal("stored script", stored, want)
		}
		assertWire(t, reply, expectedScript(t, want, true))
		after := snapshot(t, h)
		if len(after.Scripts) != len(before.Scripts)+1 || !reflect.DeepEqual(before, catalogOmit(after, stored.ID, false)) {
			t.Fatal("create changed existing catalog, runs or files", before, after)
		}
		if _, e = os.Lstat(filepath.Join(h.root, "runs", stored.ID)); !os.IsNotExist(e) {
			t.Fatal("create made script directory", e)
		}
	}
}

func TestCatalogOwnerBoundariesAndGitStarts(t *testing.T) {
	// R-575Q-1FBV R-O7UW-EYEM R-5ATF-6QJY R-5D97-YA1C R-55XT-NNL6
	h := setup(t, "print(1)\n")
	catalogSeed(t, h, "own", "alice", "run_cccccccccccccccc")
	catalogSeed(t, h, "foreign", "bob", "run_dddddddddddddddd")
	for i, tt := range []struct {
		tool, args, want string
		git              int
	}{
		{"create", `{"name":"own","repo":"missing","ref":"..bad"}`, "a script named 'own' already exists", 0},
		{"create", `{"name":"foreign","repo":"missing","ref":"..bad"}`, "a script named 'foreign' already exists", 0},
		{"create", `{"name":"Invalid","repo":"rep_1111111111111111","ref":"..bad"}`, "invalid name 'Invalid'", 0},
		{"create", `{"name":"new","repo":"missing","ref":"..bad"}`, "no repository 'missing'", 0},
		{"create", `{"name":"new","repo":"rep_1111111111111111","ref":"..bad"}`, "invalid ref '..bad'", 1},
		{"update", `{"name":"foreign","ref":"..bad"}`, "no script named 'foreign'", 0},
		{"update", `{"name":"absent","ref":"..bad"}`, "no script named 'absent'", 0},
		{"delete", `{"name":"foreign"}`, "no script named 'foreign'", 0},
	} {
		before := snapshot(t, h)
		removeTrace(t, h)
		u := caller()
		u.RequestID = fmt.Sprintf("catalog-boundary-%d", i)
		refusal(t, h.raw(tt.tool, tt.args, u), tt.want)
		unchanged(t, h, before)
		catalogOwnerTrace(t, h, tt.git)
		noDomain(t, h, u.RequestID)
	}
}

func TestCatalogUpdatePreservesLiveRun(t *testing.T) {
	// R-5C1B-KIAN
	h, sc, r, conn := paused(t)
	catalogSeed(t, h, "unrelated", "alice", "run_eeeeeeeeeeeeeeee")
	catalogSeed(t, h, "foreign", "bob", "run_ffffffffffffffff")
	for _, ref := range []string{"main", "future-branch"} {
		before := snapshot(t, h)
		original, e := h.st.Find(context.Background(), "alice", sc.Name)
		must(t, e)
		removeTrace(t, h)
		reply := h.call("update", tools.UpdateArgs{Name: sc.Name, Ref: ref})
		stored, e := h.st.Find(context.Background(), "alice", sc.Name)
		must(t, e)
		want := original
		want.Ref = ref
		if !reflect.DeepEqual(stored, want) {
			t.Fatal("update changed fields beyond Ref", stored, want)
		}
		assertWire(t, reply, expectedScript(t, want, true))
		for i := range before.Scripts {
			if before.Scripts[i].ID == sc.ID {
				before.Scripts[i].Ref = ref
			}
		}
		unchanged(t, h, before)
		noGit(t, h)
		stillLive(t, conn)
		live, e := h.st.RunByID(context.Background(), r.ID)
		must(t, e)
		if !reflect.DeepEqual(live, r) {
			t.Fatal("update changed running record", live, r)
		}
	}
	// The same process follows its original completion path after the update.
	_, e := conn.Write([]byte("finish"))
	must(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		ended, err := h.st.RunByID(ctx, r.ID)
		must(t, err)
		if ended.Status != store.StatusRunning {
			if ended.Status != store.StatusExited || ended.ExitCode != 7 || ended.Ref != r.Ref || ended.SHA != r.SHA {
				t.Fatal("updated live run ended differently", ended, r)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("live run never completed")
		default:
			runtime.Gosched()
		}
	}
}

func catalogDeleted(t *testing.T, h *fixture, s store.Script, records []store.Run, reply mcp.Result) {
	t.Helper()
	assertWire(t, reply, wire(t, member{"deleted", true}, member{"id", s.ID}))
	_, e := h.st.Find(context.Background(), s.Owner, s.Name)
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal("deleted script still found", e)
	}
	taken, e := h.st.Taken(context.Background(), s.Name)
	must(t, e)
	if taken {
		t.Fatal("deleted name still taken")
	}
	for _, r := range records {
		_, e = h.st.RunByID(context.Background(), r.ID)
		if !errors.Is(e, store.ErrNotFound) {
			t.Fatal("deleted run still found", r.ID, e)
		}
	}
	if _, e = os.Lstat(filepath.Join(h.root, "runs", s.ID)); !os.IsNotExist(e) {
		t.Fatal("deleted script path survives", e)
	}
}

func TestCatalogDeleteNeverRanAndRepositoryGone(t *testing.T) {
	// R-C2RY-13RX R-5D97-YA1C
	for _, gone := range []bool{false, true} {
		t.Run(fmt.Sprint(gone), func(t *testing.T) {
			h := setup(t, "print(1)\n")
			catalogSeed(t, h, "survivor", "alice", "run_1212121212121212")
			catalogSeed(t, h, "foreign", "bob", "run_1313131313131313")
			var victim store.Script
			if gone {
				victim = catalogSeed(t, h, "victim", "alice", "run_1414141414141414")
				must(t, os.RemoveAll(h.repo))
			} else {
				s := h.create("victim")
				var e error
				victim, e = h.st.Find(context.Background(), "alice", s.Name)
				must(t, e)
			}
			records, e := h.st.Runs(context.Background(), victim.ID)
			must(t, e)
			before := catalogOmit(snapshot(t, h), victim.ID, true)
			removeTrace(t, h)
			catalogDeleted(t, h, victim, records, h.call("delete", tools.DeleteArgs{Name: victim.Name}))
			if after := catalogOmit(snapshot(t, h), victim.ID, true); !reflect.DeepEqual(before, after) {
				t.Fatal("delete changed unrelated state", before, after)
			}
			noGit(t, h)
			refusedBefore := snapshot(t, h)
			refusal(t, h.call("delete", tools.DeleteArgs{Name: victim.Name}), "no script named 'victim'")
			unchanged(t, h, refusedBefore)
		})
	}
}

func TestCatalogDeleteStopsGroupsAndPreservesOtherProcesses(t *testing.T) {
	// R-C2RY-13RX
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	t.Cleanup(func() { stop(); cancel(); _ = listener.Close() })
	// Both Python processes own independent connections until killed. Closing
	// one connection cannot hide a surviving child in the other connection.
	program := fmt.Sprintf("import os,socket\npid=os.fork()\ns=socket.create_connection(('127.0.0.1',%d))\ns.sendall(b'C' if pid==0 else b'P')\nwhile True:\n c=s.recv(1)\n if c==b'p':\n  s.sendall(b'P')\n else:\n  break\n", listener.Addr().(*net.TCPAddr).Port)
	h := setup(t, program)
	victim := catalogSeed(t, h, "victim", "alice", "run_1515151515151515")
	other := h.create("unrelated")
	catalogSeed(t, h, "foreign", "bob", "run_1616161616161616")
	startGroup := func(name string) (store.Run, []net.Conn) {
		started := decode[tools.Started](t, h.call("run", tools.RunArgs{Name: name, Input: json.RawMessage(`{}`)}))
		r, err := h.st.RunByID(ctx, started.ID)
		must(t, err)
		conns := []net.Conn{}
		roles := map[byte]bool{}
		for range 2 {
			c, err := listener.Accept()
			must(t, err)
			closeOnDeadline := context.AfterFunc(ctx, func() { _ = c.Close() })
			t.Cleanup(func() { closeOnDeadline(); _ = c.Close() })
			b := make([]byte, 1)
			_, err = io.ReadFull(c, b)
			must(t, err)
			roles[b[0]] = true
			conns = append(conns, c)
		}
		if len(roles) != 2 || !roles['P'] || !roles['C'] {
			t.Fatal("parent/child handshakes", roles)
		}
		return r, conns
	}
	live, connections := startGroup(victim.Name)
	_, otherConnections := startGroup(other.Name)
	before := catalogOmit(snapshot(t, h), victim.ID, true)
	records, e := h.st.Runs(ctx, victim.ID)
	must(t, e)
	removeTrace(t, h)
	reply := h.call("delete", tools.DeleteArgs{Name: victim.Name})
	// Observe both sockets immediately after the tool answers. A living parent
	// or child responds to this probe; a killed process closes its connection.
	for _, c := range connections {
		_, _ = c.Write([]byte("p"))
		b := make([]byte, 1)
		n, err := c.Read(b)
		if n != 0 || !errors.Is(err, io.EOF) {
			t.Fatal("deleted group's process still responds", n, err, b)
		}
	}
	catalogDeleted(t, h, victim, records, reply)
	if live.Status != store.StatusRunning {
		t.Fatal("fixture run did not start", live)
	}
	if after := catalogOmit(snapshot(t, h), victim.ID, true); !reflect.DeepEqual(before, after) {
		t.Fatal("delete changed unrelated state", before, after)
	}
	for _, c := range otherConnections {
		stillLive(t, c)
	}
	noGit(t, h)
}
