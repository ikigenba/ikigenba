package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appEvents "github.com/ikigenba/ikigenba/appkit/events"
)

// R-X661-3MC4
func TestDatabaseRestore(t *testing.T) {
	sockets := shortDir(t)
	socket := filepath.Join(sockets, "restore")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan appEvents.Event, 16)
	var finish atomic.Bool
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/declarations" {
			_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":["private_attr"]}],"accepts":["repo.pushed"]}`)
			return
		}
		var data struct {
			ID       string    `json:"id"`
			Seq      int64     `json:"seq"`
			Received time.Time `json:"received"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Error(err)
			return
		}
		received <- appEvents.Event{ID: data.ID, Seq: data.Seq, Received: data.Received}
		if data.Seq == 2 && !finish.Load() {
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"outcome":"ok"}`)
	})}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })
	path := filepath.Join(t.TempDir(), "services")
	writeServices(t, path, "", []map[string]any{{"name": "repos", "description": "repos", "url": "https://repos.test", "socket": socket, "enabled": true, "mcp": false}})
	dir := t.TempDir()
	env := func() map[string]string { return map[string]string{"IKIGENBA_SERVICES": path, "DRAIN_SECONDS": "1"} }
	receive := func() appEvents.Event {
		t.Helper()
		select {
		case e := <-received:
			return e
		case <-time.After(3 * time.Second):
			t.Fatal("no expected delivery")
			return appEvents.Event{}
		}
	}
	cursor := func(f *runFixture, n int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			r := call(t, f, "subscribers", map[string]any{}, "cursor")
			data, _ := json.Marshal(r)
			if bytes.Contains(data, []byte(`"cursor":`+strconv.Itoa(n))) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("cursor not advanced", string(data))
			}
		}
	}
	first := startRun(t, dir, env())
	one := emitted(first, 1, 0)
	two := emitted(first, 2, 0)
	emit(t, first, one, 204)
	if e := receive(); e.ID != one.ID || e.Seq != 1 {
		t.Fatal(e)
	}
	cursor(first, 1)
	emit(t, first, two, 204)
	original := receive()
	if original.ID != two.ID || original.Seq != 2 {
		t.Fatal(original)
	}
	first.stop(t)
	dirRoot, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dirRoot.Close() }()
	snapshot, err := dirRoot.ReadFile("state/events.db")
	if err != nil {
		t.Fatal(err)
	}
	finish.Store(true)
	second := startRun(t, dir, env())
	again := receive()
	if again.ID != original.ID || again.Seq != original.Seq || !again.Received.Equal(original.Received) {
		t.Fatal(again, original)
	}
	cursor(second, 2)
	three := emitted(second, 3, 0)
	emit(t, second, three, 204)
	if e := receive(); e.ID != three.ID || e.Seq != 3 {
		t.Fatal(e)
	}
	cursor(second, 3)
	second.stop(t)
	if err := dirRoot.WriteFile("state/events.db", snapshot, 0600); err != nil {
		t.Fatal(err)
	}
	restored := startRun(t, dir, env())
	redelivery := receive()
	if redelivery.ID != original.ID || redelivery.Seq != 2 || !redelivery.Received.Equal(original.Received) {
		t.Fatal(redelivery, original)
	}
	cursor(restored, 2)
	result := call(t, restored, "search", map[string]any{}, "restored-search")
	data, _ := json.Marshal(result)
	if !bytes.Contains(data, []byte(one.ID)) || !bytes.Contains(data, []byte(two.ID)) || bytes.Contains(data, []byte(three.ID)) {
		t.Fatal(string(data))
	}
	var decoded struct {
		Structured struct {
			Records []struct {
				ID       string    `json:"id"`
				Seq      int64     `json:"seq"`
				Received time.Time `json:"received"`
			}
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, record := range decoded.Structured.Records {
		want := int64(1)
		if record.ID == two.ID {
			want = 2
		}
		if record.Seq != want || !record.Received.Equal(original.Received) {
			t.Fatal(record)
		}
	}
	if len(decoded.Structured.Records) != 2 {
		t.Fatal(string(data))
	}
	four := emitted(restored, 4, 0)
	emit(t, restored, four, 204)
	if e := receive(); e.ID != four.ID || e.Seq != 3 {
		t.Fatal(e)
	}
	cursor(restored, 3)
	restored.stop(t)
	if strings.Contains(restored.errw.String(), "cannot") {
		t.Fatal(restored.errw.String())
	}
}
