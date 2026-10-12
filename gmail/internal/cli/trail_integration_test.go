package cli_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/cli"
	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
	"github.com/ikigenba/ikigenba/gmail/internal/web"
)

func trailAnswer(t *testing.T, f *runFixture, c identity.Caller, name string, args any) map[string]any {
	t.Helper()
	r := f.call(c, name, args)
	if r.IsError() {
		t.Fatalf("%s refused: %s", name, resultText(t, r))
	}
	return resultJSON(t, r)
}
func trailID(t *testing.T, v map[string]any) string {
	t.Helper()
	id, ok := v["id"].(string)
	if !ok || id == "" {
		t.Fatal(v)
	}
	return id
}
func trailPage(t *testing.T, f *runFixture, c identity.Caller, path string) string {
	t.Helper()
	r, e := http.NewRequestWithContext(context.Background(), http.MethodGet, f.Base+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	identity.Forward(c, r)
	response, e := f.HTTP.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = response.Body.Close() }()
	var b strings.Builder
	if _, e = io.Copy(&b, response.Body); e != nil {
		t.Fatal(e)
	}
	return b.String()
}
func trailNames() map[string]bool {
	return map[string]bool{trail.TransferStarted: true, trail.TransferFinished: true, trail.DraftCreated: true, trail.DraftSent: true, trail.DraftDiscarded: true}
}
func trailPair(t *testing.T, f *runFixture) {
	t.Helper()
	names := trailNames()
	a := []telemetry.Event{}
	for _, e := range f.Telemetry.Events() {
		if names[e.Name] {
			a = append(a, e)
		}
	}
	b := f.Events.Events()
	if len(a) != len(b) {
		t.Fatalf("paired counts %d/%d", len(a), len(b))
	}
	used := make([]bool, len(b))
	for _, e := range a {
		count := 0
		for i, v := range b {
			if e.Name == v.Name && e.RequestID == v.RequestID && e.User == v.User && reflect.DeepEqual(e.Attrs, telemetry.Attrs(v.Attrs)) {
				if used[i] {
					t.Fatalf("duplicate event %#v", v)
				}
				used[i] = true
				count++
			}
		}
		if count != 1 {
			t.Fatalf("match count %d for %#v", count, e)
		}
	}
}
func trailDraftEvent(t *testing.T, f *runFixture, name string, c identity.Caller, v map[string]any) {
	t.Helper()
	want := telemetry.Attrs{"draft": v["id"], "message": v["message"], "thread": v["thread"], "attachments": int64(len(v["attachments"].([]any)))}
	countA, countB := 0, 0
	for _, e := range f.Telemetry.Events() {
		if e.Name == name && e.Attrs["draft"] == v["id"] {
			countA++
			if e.User != c.UserID || e.RequestID != c.RequestID || !reflect.DeepEqual(e.Attrs, want) {
				t.Fatalf("draft trail %#v want %#v", e, want)
			}
		}
	}
	for _, e := range f.Events.Events() {
		if e.Name == name && e.Attrs["draft"] == v["id"] {
			countB++
			if e.User != c.UserID || e.RequestID != c.RequestID || !reflect.DeepEqual(e.Attrs, events.Attrs(want)) {
				t.Fatalf("draft bus %#v", e)
			}
		}
	}
	if countA != 1 || countB != 1 {
		t.Fatalf("draft counts %d %d", countA, countB)
	}
}
func trailTransferEvent(t *testing.T, f *runFixture, c identity.Caller, v map[string]any, started bool, draining ...bool) {
	t.Helper()
	id := v["id"]
	want := telemetry.Attrs{"transfer": id}
	for _, key := range []string{"direction", "message", "attachment", "draft", "path", "status", "reason", "bytes"} {
		value, ok := v[key]
		if !ok {
			value = ""
		}
		if key == "bytes" {
			value = int64(value.(float64))
		}
		want[key] = value
	}
	for _, bus := range []bool{false, true} {
		names := []string{}
		attrs := []telemetry.Attrs{}
		if bus {
			for _, e := range f.Events.Events() {
				if e.Attrs["transfer"] == id {
					if e.User != c.UserID || e.RequestID != c.RequestID {
						t.Fatal(e)
					}
					names = append(names, e.Name)
					attrs = append(attrs, telemetry.Attrs(e.Attrs))
				}
			}
		} else {
			for _, e := range f.Telemetry.Events() {
				if e.Attrs["transfer"] == id {
					if e.User != c.UserID || e.RequestID != c.RequestID {
						t.Fatal(e)
					}
					names = append(names, e.Name)
					attrs = append(attrs, e.Attrs)
				}
			}
		}
		if len(draining) > 0 && draining[0] {
			expected := []string{}
			if started {
				expected = append(expected, trail.TransferStarted)
			}
			if len(names) > 0 && names[len(names)-1] == trail.TransferFinished {
				expected = append(expected, trail.TransferFinished)
			}
			if !reflect.DeepEqual(names, expected) {
				t.Fatalf("draining order %#v want %#v", names, expected)
			}
			if len(names) > 0 && names[len(names)-1] == trail.TransferFinished && !reflect.DeepEqual(attrs[len(attrs)-1], want) {
				t.Fatalf("draining attrs %#v want %#v", attrs, want)
			}
			continue
		}
		expected := []string{trail.TransferFinished}
		if started {
			expected = []string{trail.TransferStarted, trail.TransferFinished}
		}
		if !reflect.DeepEqual(names, expected) {
			t.Fatalf("transfer event order %#v want %#v", names, expected)
		}
		if !reflect.DeepEqual(attrs[len(attrs)-1], want) {
			t.Fatalf("transfer attrs %#v want %#v", attrs, want)
		}
	}
}

func TestTrailRunChangesAndNames(t *testing.T) {
	// R-X89N-3XNF R-XAPF-VH4T R-XBXC-98VI R-XD58-N0M7 R-XED5-0SCW
	f := startRun(t, runOptions{})
	c := fixtureCaller
	created := trailAnswer(t, f, c, "compose", map[string]any{"to": []string{"recipient@example.invalid"}, "subject": "synthetic-subject"})
	c.RequestID = "push-request"
	pushed := trailAnswer(t, f, c, "push", map[string]any{"draft": created["id"], "path": "push.bin"})
	pushDone := f.awaitTransfer(c, trailID(t, pushed))
	c.RequestID = "send-request"
	sent := trailAnswer(t, f, c, "send", map[string]any{"draft": created["id"]})
	c.RequestID = "compose-second"
	created2 := trailAnswer(t, f, c, "compose", map[string]any{"to": []string{"recipient@example.invalid"}})
	c.RequestID = "discard-request"
	discarded := trailAnswer(t, f, c, "discard", map[string]any{"draft": created2["id"]})
	c.RequestID = "pull-request"
	pulled := trailAnswer(t, f, c, "pull", map[string]any{"message": "message", "attachment": "part", "path": "pulled.bin"})
	pullDone := f.awaitTransfer(c, trailID(t, pulled))
	if code := f.stop(); code != cli.ExitSuccess {
		t.Fatalf("stop %d %s", code, f.Err.String())
	}
	trailPair(t, f)
	c = fixtureCaller
	trailDraftEvent(t, f, trail.DraftCreated, c, created)
	c.RequestID = "compose-second"
	trailDraftEvent(t, f, trail.DraftCreated, c, created2)
	c.RequestID = "send-request"
	trailDraftEvent(t, f, trail.DraftSent, c, sent)
	c.RequestID = "discard-request"
	trailDraftEvent(t, f, trail.DraftDiscarded, c, discarded)
	c.RequestID = "push-request"
	trailTransferEvent(t, f, c, pushDone, true)
	c.RequestID = "pull-request"
	trailTransferEvent(t, f, c, pullDone, true)
	allowed := trailNames()
	for _, name := range []string{"service.started", "service.stopping", "request.started", "request.finished", "tool.called", "sibling.called", "gmail.called", "event.lost"} {
		allowed[name] = true
	}
	for _, e := range f.Telemetry.Events() {
		if !allowed[e.Name] {
			t.Fatal(e)
		}
	}
	for _, e := range f.Events.Events() {
		if !trailNames()[e.Name] {
			t.Fatal(e)
		}
	}
}

func TestTrailReadAndRefusedCallsTellNothing(t *testing.T) {
	// R-6FQ4-2WBV
	f := startRun(t, runOptions{})
	c := fixtureCaller
	for _, tc := range []struct {
		name string
		args any
	}{{"compose", map[string]any{}}, {"pull", map[string]any{"message": "message", "path": "invalid.bin", "expires": "invalid"}}, {"push", map[string]any{"draft": "unknown", "path": "push.bin"}}, {"send", map[string]any{"draft": "unknown"}}, {"discard", map[string]any{"draft": "unknown"}}, {"search", map[string]any{"query": "synthetic"}}, {"thread", map[string]any{"thread": "thread"}}, {"show", map[string]any{"message": "message"}}, {"labels", map[string]any{}}, {"drafts", map[string]any{}}, {"transfers", map[string]any{}}, {"transfer", map[string]any{"transfer": "unknown"}}} {
		f.call(c, tc.name, tc.args)
	}
	for _, path := range []string{"/", "/tools", "/about", "/drafts/unknown", "/transfers/unknown", "/missing"} {
		trailPage(t, f, c, path)
	}
	if code := f.stop(); code != 0 {
		t.Fatal(code)
	}
	for _, e := range f.Telemetry.Events() {
		if trailNames()[e.Name] {
			t.Fatal(e)
		}
	}
	if len(f.Events.Events()) != 0 {
		t.Fatal(f.Events.Events())
	}
}

func TestTrailGmailAndSiblingCallCounts(t *testing.T) {
	// R-XJ8Q-JVBO R-XKGM-XN2D
	mail := &fakeMail{drafts: map[string]string{}}
	var mu sync.Mutex
	statuses := []int64{}
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response, e := mail.RoundTrip(r)
		mu.Lock()
		if strings.Contains(r.URL.RawQuery, "provider-failure") {
			statuses = append(statuses, 0)
			mu.Unlock()
			if response != nil {
				_ = response.Body.Close()
			}
			return nil, errors.New("synthetic transport failure")
		}
		statuses = append(statuses, int64(response.StatusCode))
		mu.Unlock()
		return response, e
	})
	f := startRun(t, runOptions{Transport: tr})
	c := fixtureCaller
	f.call(c, "search", map[string]any{"query": "provider-failure"})
	f.call(c, "show", map[string]any{"message": "message"})
	d := trailAnswer(t, f, c, "compose", map[string]any{"to": []string{"recipient@example.invalid"}})
	p := trailAnswer(t, f, c, "push", map[string]any{"draft": d["id"], "path": "push.bin"})
	f.awaitTransfer(c, trailID(t, p))
	f.stop()
	got := []int64{}
	for _, e := range f.Telemetry.Events() {
		if e.Name != "gmail.called" {
			continue
		}
		if e.User != c.UserID || e.RequestID != c.RequestID || len(e.Attrs) != 3 {
			t.Fatal(e)
		}
		endpoint, ok := e.Attrs["endpoint"].(string)
		if !ok || endpoint == "" {
			t.Fatal(e)
		}
		status, ok := e.Attrs["status"].(int64)
		if !ok {
			t.Fatal(e)
		}
		duration, ok := e.Attrs["duration_us"].(int64)
		if !ok || duration < 0 {
			t.Fatal(e)
		}
		got = append(got, status)
	}
	if len(got) != len(mail.Requests()) || len(got) != len(statuses) {
		t.Fatalf("gmail counts %d/%d/%d", len(got), len(mail.Requests()), len(statuses))
	}
	slices.Sort(got)
	slices.Sort(statuses)
	if !reflect.DeepEqual(got, statuses) {
		t.Fatal(got, statuses)
	}
	cc, aa := f.SiblingRequests()
	for target, want := range map[string]int{"connections": len(cc), "artifacts": len(aa)} {
		count := 0
		for _, e := range f.Telemetry.Events() {
			if e.Name == "sibling.called" && e.Attrs["target"] == target {
				count++
			}
		}
		if count != want || count == 0 {
			t.Fatalf("%s count %d want %d", target, count, want)
		}
	}
}

type trailRejectTelemetry struct{}

func (trailRejectTelemetry) Deliver(context.Context, telemetry.Event) error {
	return telemetry.ErrRejected
}

type trailRejectEvents struct{}

func (trailRejectEvents) Deliver(context.Context, events.Event) error { return events.ErrRejected }
func TestTrailRejectedSinksPreserveResults(t *testing.T) {
	// R-BACC-IC0J
	var answers [2][]map[string]any
	for i := 0; i < 2; i++ {
		o := runOptions{}
		if i == 1 {
			o.Sink = trailRejectTelemetry{}
			o.EventSink = trailRejectEvents{}
		}
		f := startRun(t, o)
		c := fixtureCaller
		created := trailAnswer(t, f, c, "compose", map[string]any{"to": []string{"recipient@example.invalid"}})
		answers[i] = append(answers[i], created)
		p := trailAnswer(t, f, c, "pull", map[string]any{"message": "message", "attachment": "part", "path": "pull.bin"})
		done := f.awaitTransfer(c, trailID(t, p))
		answers[i] = append(answers[i], done)
		refused := f.call(c, "compose", map[string]any{})
		if !refused.IsError() {
			t.Fatal("missing refusal")
		}
		answers[i] = append(answers[i], map[string]any{"error": resultText(t, refused)})
		call := func(name string, args any) map[string]any {
			r := f.call(c, name, args)
			v := map[string]any{"IsError": r.IsError()}
			if r.IsError() {
				v["answer"] = resultText(t, r)
			} else {
				v["answer"] = resultJSON(t, r)
			}
			answers[i] = append(answers[i], v)
			if !r.IsError() {
				return v["answer"].(map[string]any)
			}
			return nil
		}
		push := call("push", map[string]any{"draft": created["id"], "path": "push.bin"})
		pushDone := f.awaitTransfer(c, trailID(t, push))
		answers[i] = append(answers[i], pushDone)
		second := call("compose", map[string]any{"to": []string{"recipient@example.invalid"}})
		sent := call("send", map[string]any{"draft": second["id"]})
		third := call("compose", map[string]any{"to": []string{"recipient@example.invalid"}})
		discarded := call("discard", map[string]any{"draft": third["id"]})
		for _, tc := range []struct {
			name string
			args any
		}{{"search", map[string]any{"query": "synthetic"}}, {"thread", map[string]any{"thread": "thread"}}, {"show", map[string]any{"message": "message"}}, {"labels", map[string]any{}}, {"drafts", map[string]any{}}, {"draft", map[string]any{"draft": created["id"]}}, {"transfers", map[string]any{}}, {"transfer", map[string]any{"transfer": p["id"]}}, {"send", map[string]any{"draft": second["id"]}}} {
			call(tc.name, tc.args)
		}

		f.stop()
		held := f.reopen()
		d, e := held.GetDraft(context.Background(), c.UserID, trailID(t, created))
		if e != nil || d.Status != store.DraftOpen {
			t.Fatal(d, e)
		}
		transfer, e := held.GetTransfer(context.Background(), c.UserID, trailID(t, p))
		if e != nil || transfer.Status != done["status"] || transfer.Bytes != int64(done["bytes"].(float64)) {
			t.Fatal(transfer, e)
		}

		for _, v := range []map[string]any{sent, discarded} {
			d, e := held.GetDraft(context.Background(), c.UserID, trailID(t, v))
			if e != nil || d.Status != v["status"] {
				t.Fatal(d, e)
			}
		}
		storedPush, e := held.GetTransfer(context.Background(), c.UserID, trailID(t, push))
		if e != nil || storedPush.Status != pushDone["status"] || storedPush.Bytes != int64(pushDone["bytes"].(float64)) {
			t.Fatal(storedPush, e)
		}
		for _, v := range answers[i] {
			trailNormalizeAnswer(v)
		}

	}
	if !reflect.DeepEqual(answers[0], answers[1]) {
		t.Fatalf("sink affects answers %#v %#v", answers[0], answers[1])
	}
}

func TestTrailReconciledDraftExactlyOnce(t *testing.T) {
	// R-6EI7-P4L6
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprint(sent), func(t *testing.T) {
			mail := &fakeMail{drafts: map[string]string{}}
			var mu sync.Mutex
			missing := false
			tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				gone := missing
				mu.Unlock()
				if gone && strings.Contains(r.URL.Path, "/drafts/") {
					return jsonResponse(r, 404, map[string]any{}), nil
				}
				if strings.Contains(r.URL.Path, "/threads/") {
					messages := []any{}
					if sent {
						messages = append(messages, map[string]any{"id": "reconciled-message", "threadId": "draft-thread", "internalDate": "1791806401000", "labelIds": []string{"SENT"}})
					}
					return jsonResponse(r, 200, map[string]any{"id": "draft-thread", "messages": messages}), nil
				}
				return mail.RoundTrip(r)
			})
			f := startRun(t, runOptions{Transport: tr})
			c := fixtureCaller
			created := trailAnswer(t, f, c, "compose", map[string]any{"to": []string{"recipient@example.invalid"}})
			mu.Lock()
			missing = true
			mu.Unlock()
			c.RequestID = "reconciliation-request"
			d := trailAnswer(t, f, c, "draft", map[string]any{"draft": created["id"]})
			if d["status"] != map[bool]string{false: "discarded", true: "sent"}[sent] {
				t.Fatal(d)
			}
			trailAnswer(t, f, c, "draft", map[string]any{"draft": created["id"]})
			f.stop()
			name := trail.DraftDiscarded
			if sent {
				name = trail.DraftSent
			}
			trailDraftEvent(t, f, name, c, d)
			trailPair(t, f)
		})
	}
}

func TestTrailQueueAndDrainIdentity(t *testing.T) {
	// R-XBXC-98VI
	mail := &fakeMail{drafts: map[string]string{}}
	entered := make(chan struct{})
	var once sync.Once
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/attachments/") {
			once.Do(func() { close(entered) })
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return mail.RoundTrip(r)
	})
	f := startRun(t, runOptions{Transport: tr, Env: map[string]string{"TRANSFER_MAX_ACTIVE": "1", "DRAIN_SECONDS": "1"}})
	first := fixtureCaller
	first.RequestID = "first-request"
	a := trailAnswer(t, f, first, "pull", map[string]any{"message": "message", "attachment": "part", "path": "first.bin"})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not begin")
	}
	second := fixtureCaller
	second.RequestID = "queued-request"
	b := trailAnswer(t, f, second, "pull", map[string]any{"message": "message", "attachment": "part", "path": "second.bin"})
	f.stop()
	st := f.reopen()
	for _, tc := range []struct {
		c       identity.Caller
		v       map[string]any
		started bool
	}{{first, a, true}, {second, b, false}} {
		held, e := st.GetTransfer(context.Background(), tc.c.UserID, trailID(t, tc.v))
		if e != nil || held.Status != store.StatusFailed || held.Reason != store.ReasonInterrupted {
			t.Fatal(held, e)
		}
		v := map[string]any{"id": held.ID, "direction": held.Direction, "message": held.Message, "attachment": held.Attachment, "draft": held.Draft, "path": held.Path, "status": held.Status, "reason": held.Reason, "bytes": float64(held.Bytes)}
		trailTransferEvent(t, f, tc.c, v, tc.started, true)
	}
	trailPair(t, f)
}

type trailSecrets struct {
	token                string
	markers, texts, data []string
}

type secretRandom struct{ n uint64 }

func (r *secretRandom) Read(p []byte) (int, error) {
	for i := 0; i < len(p); {
		block := sha256.Sum256([]byte(fmt.Sprint("fixture-secret-", r.n)))
		r.n++
		i += copy(p[i:], block[:])
	}
	return len(p), nil
}
func secretText(r io.Reader, n int) string {
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	v := make([]byte, n)
	if _, e := io.ReadFull(r, v); e != nil {
		panic(e)
	}
	for i := range v {
		v[i] = alphabet[int(v[i])%len(alphabet)]
		if i%7 == 0 {
			v[i] = 'G'
		}
	}
	return string(v)
}
func newTrailSecrets(t *testing.T) trailSecrets {
	t.Helper()
	r := &secretRandom{}
	s := trailSecrets{token: secretText(r, 40)}
	for i := 0; i < 12; i++ {
		s.markers = append(s.markers, secretText(r, 24))
	}
	s.texts = []string{secretText(r, 4096), secretText(r, 4096)}
	s.data = []string{secretText(r, 4096), secretText(r, 4096)}
	all := append([]string{s.token}, s.markers...)
	all = append(all, s.texts...)
	all = append(all, s.data...)
	for i, a := range all {
		for j, b := range all {
			if i != j && strings.Contains(b, a) {
				t.Fatal("secret fixture overlap")
			}
		}
	}
	return s
}
func secretPieces(bodies []string) []string {
	out := []string{}
	for _, body := range bodies {
		for _, value := range []string{body, base64.StdEncoding.EncodeToString([]byte(body)), base64.URLEncoding.EncodeToString([]byte(body))} {
			for i := 0; i+16 <= len(value); i++ {
				out = append(out, value[i:i+16])
			}
		}
	}
	return out
}
func assertNoSecrets(t *testing.T, label, value string, needles []string) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			t.Fatalf("%s contains protected fixture content", label)
		}
	}
}
func trailEncoded(t *testing.T, value any) string {
	t.Helper()
	b, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

func TestTrailSecrecyAcrossWholeRun(t *testing.T) {
	// R-XLOJ-BET2 R-XMWF-P6JR R-XO4C-2YAG R-XPC8-GQ15 R-XQK4-UHRU R-XRS1-89IJ R-XU7T-ZSZX R-XVFQ-DKQM
	secrets := newTrailSecrets(t)
	mail := &fakeMail{drafts: map[string]string{}}
	blocked := make(chan struct{})
	var once sync.Once
	address := func(i int) string { return secrets.markers[i] + "@example.invalid" }
	failure := strings.Join(append(append(append([]string{secrets.token}, secrets.markers...), secrets.texts...), secrets.data...), " ")
	message := func(id string) map[string]any {
		return map[string]any{"id": id, "threadId": "thread", "labelIds": []string{"INBOX"}, "internalDate": "1791806400000", "sizeEstimate": 10000, "snippet": secrets.markers[6], "payload": map[string]any{"mimeType": "multipart/mixed", "headers": []any{map[string]any{"name": "Subject", "value": secrets.markers[0]}, map[string]any{"name": "From", "value": address(1)}, map[string]any{"name": "To", "value": address(2)}, map[string]any{"name": "Cc", "value": address(3)}, map[string]any{"name": "Reply-To", "value": address(4)}}, "parts": []any{map[string]any{"partId": "text", "mimeType": "text/plain", "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte(secrets.texts[0]))}}, map[string]any{"partId": "part", "filename": secrets.markers[5], "mimeType": "application/octet-stream", "body": map[string]any{"attachmentId": "blob", "size": len(secrets.data[0])}}}}}
	}
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/messages") && strings.Contains(r.URL.Query().Get("q"), "fail"):
			return jsonResponse(r, 500, map[string]any{"error": failure}), nil
		case strings.Contains(path, "/attachments/"):
			if strings.Contains(path, "/failure/") {
				return jsonResponse(r, 500, map[string]any{"error": failure}), nil
			}
			if strings.Contains(path, "/blocked/") {
				once.Do(func() { close(blocked) })
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return jsonResponse(r, 200, map[string]any{"size": len(secrets.data[0]), "data": base64.RawURLEncoding.EncodeToString([]byte(secrets.data[0]))}), nil
		case strings.Contains(path, "/messages/"):
			id := path[strings.LastIndex(path, "/")+1:]
			if r.URL.Query().Get("format") == "raw" {
				raw := "Subject: " + secrets.markers[0] + "\r\nFrom: " + address(1) + "\r\nContent-Type: multipart/mixed; boundary=fixture\r\n\r\n--fixture\r\nContent-Type: text/plain\r\n\r\n" + secrets.texts[0] + "\r\n--fixture\r\nContent-Type: application/octet-stream\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte(secrets.data[0])) + "\r\n--fixture--\r\n"
				return jsonResponse(r, 200, map[string]any{"raw": base64.RawURLEncoding.EncodeToString([]byte(raw))}), nil
			}
			return jsonResponse(r, 200, message(id)), nil
		case strings.Contains(path, "/threads/"):
			return jsonResponse(r, 200, map[string]any{"id": "thread", "messages": []any{message("message")}}), nil
		default:
			return mail.RoundTrip(r)
		}
	})
	broker := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": secrets.token, "account": address(7), "expires": "2099-01-01T00:00:00Z", "scopes": []string{}})
	})
	f := startRun(t, runOptions{Transport: tr, ConnectionsHandler: broker, Env: map[string]string{"DRAIN_SECONDS": "1"}})
	f.mu.Lock()
	f.ArtifactData["push.bin"] = []byte(secrets.data[1])
	f.mu.Unlock()
	c := fixtureCaller
	type observedAnswer struct {
		name   string
		result mcp.Result
	}
	answers := []observedAnswer{}
	call := func(name string, args any) map[string]any {
		r := f.call(c, name, args)
		answers = append(answers, observedAnswer{name, r})
		if r.IsError() {
			return nil
		}
		return resultJSON(t, r)
	}
	call("search", map[string]any{"query": secrets.markers[8]})
	call("search", map[string]any{"query": "fail " + secrets.markers[8]})
	call("thread", map[string]any{"thread": "thread"})
	call("show", map[string]any{"message": "message"})
	call("labels", map[string]any{})
	d := call("compose", map[string]any{"to": []string{address(9)}, "cc": []string{address(10)}, "bcc": []string{address(11)}, "subject": secrets.markers[9], "body": secrets.texts[1]})
	id := trailID(t, d)
	call("draft", map[string]any{"draft": id})
	call("drafts", map[string]any{})
	p := call("push", map[string]any{"draft": id, "path": "push.bin"})
	f.awaitTransfer(c, trailID(t, p))
	call("transfer", map[string]any{"transfer": p["id"]})
	call("send", map[string]any{"draft": id})
	call("push", map[string]any{"draft": id, "path": "push.bin"})
	call("send", map[string]any{"draft": id})
	call("discard", map[string]any{"draft": id})
	d2 := call("compose", map[string]any{"to": []string{address(9)}, "subject": secrets.markers[9], "body": secrets.texts[1]})
	call("discard", map[string]any{"draft": d2["id"]})
	transferIDs := []string{}
	for _, source := range []string{"message", "failure"} {
		v := call("pull", map[string]any{"message": source, "attachment": "part", "path": source + ".bin"})
		tid := trailID(t, v)
		transferIDs = append(transferIDs, tid)
		end := f.awaitTransfer(c, tid)
		wanted := "done"
		if source == "failure" {
			wanted = "failed"
		}
		if end["status"] != wanted {
			t.Fatal(end)
		}
	}
	raw := call("pull", map[string]any{"message": "message", "path": "raw.bin"})
	f.awaitTransfer(c, trailID(t, raw))
	call("transfers", map[string]any{})
	call("transfer", map[string]any{"transfer": transferIDs[0]})
	pages := []string{}
	for _, path := range []string{"/", "/tools", "/about", "/missing", "/drafts/" + id, "/drafts/" + trailID(t, d2), "/transfers/" + transferIDs[0], "/transfers/" + transferIDs[1]} {
		pages = append(pages, trailPage(t, f, c, path))
	}
	interrupted := call("pull", map[string]any{"message": "blocked", "attachment": "part", "path": "blocked.bin"})
	select {
	case <-blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("blocked transfer not running")
	}
	f.stop()
	held, e := f.reopen().GetTransfer(context.Background(), c.UserID, trailID(t, interrupted))
	if e != nil || held.Status != store.StatusFailed || held.Reason != store.ReasonInterrupted {
		t.Fatal(held, e)
	}
	dataPieces, textPieces, composePieces := secretPieces(secrets.data), secretPieces(secrets.texts), secretPieces([]string{secrets.texts[1]})
	captures := []string{trailEncoded(t, f.Telemetry.Events()), trailEncoded(t, f.Events.Events()), f.Out.String(), f.Err.String()}
	for _, v := range captures {
		assertNoSecrets(t, "sink/stream token", v, []string{secrets.token})
		assertNoSecrets(t, "sink/stream data", v, dataPieces)
		assertNoSecrets(t, "sink/stream text", v, textPieces)
		assertNoSecrets(t, "sink/stream marker", v, secrets.markers)
	}
	for _, a := range answers {
		value := trailEncoded(t, a.result)
		assertNoSecrets(t, a.name+" token", value, []string{secrets.token})
		assertNoSecrets(t, a.name+" data", value, dataPieces)
		if a.name != "draft" {
			assertNoSecrets(t, a.name+" composition", value, composePieces)
		}
		if a.result.IsError() {
			assertNoSecrets(t, a.name+" refusal marker", value, secrets.markers)
			assertNoSecrets(t, a.name+" refusal text", value, textPieces)
		}
	}
	for _, value := range pages {
		assertNoSecrets(t, "page token", value, []string{secrets.token})
		assertNoSecrets(t, "page data", value, dataPieces)
		assertNoSecrets(t, "page text", value, textPieces)
		assertNoSecrets(t, "page snippet", value, []string{secrets.markers[6]})
	}
	cc, aa := f.SiblingRequests()
	for _, r := range append(cc, aa...) {
		assertNoSecrets(t, "sibling request", trailEncoded(t, r), []string{secrets.token})
	}
}

func TestTrailFailingStoreTellsNoChange(t *testing.T) {
	// R-XI0U-63KZ
	f := startRun(t, runOptions{})
	ctx := identity.NewContext(context.Background(), fixtureCaller)
	dbHandle, e := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "gmail.db"), Migrations: app.Migrations(), Now: f.Process.Now, Service: "gmail", Stderr: io.Discard})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = dbHandle.Close() })
	st := store.New(dbHandle, store.Config{Now: f.Process.Now, Rand: &runRandom{}})
	d, e := st.AddDraft(ctx, store.Draft{UserID: fixtureCaller.UserID, GmailDraft: "google-draft", Message: "draft-message", Thread: "draft-thread", To: []string{"recipient@example.invalid"}})
	if e != nil {
		t.Fatal(e)
	}
	capture, bus := &telemetry.Capture{}, &events.Capture{}
	writer := telemetry.New(telemetry.Config{Service: "gmail", Version: "test", Sink: capture, Stderr: io.Discard, Now: f.Process.Now, Sleep: func(context.Context, time.Duration) {}, Rand: fixedRandom(1)})
	emitter := events.New(events.Config{Service: "gmail", Sink: bus, Stderr: io.Discard, Now: f.Process.Now, Sleep: func(context.Context, time.Duration) {}, Rand: fixedRandom(1), Emits: trail.Emits(), Telemetry: writer})
	t.Cleanup(func() { emitter.Shutdown(context.Background()); writer.Shutdown(context.Background(), "test") })
	servicePath, _ := f.Process.LookupEnv(services.Variable)
	list, e := services.Read(servicePath)
	if e != nil {
		t.Fatal(e)
	}
	connection, _ := list.Find("connections")
	artifact, _ := list.Find("artifacts")
	cc := connections.NewClient(connections.ClientConfig{Service: connection, Telemetry: writer})
	ac := artifacts.NewClient(artifacts.ClientConfig{Service: artifact, Telemetry: writer})
	gc := gmail.NewClient(gmail.Config{HTTP: f.Process.HTTP, Telemetry: writer})
	dr := drafts.New(drafts.Config{Store: st, Gmail: gc, Connections: cc, Telemetry: writer, Events: emitter, ConnectURL: "http://connections.example.invalid/google/connect"})
	engine := transfers.New(transfers.Config{Store: st, Gmail: gc, Artifacts: ac, Connections: cc, Drafts: dr, Telemetry: writer, Events: emitter, MaxBytes: 52428800, MaxActive: 2, MaxQueued: 10, Now: f.Process.Now})
	server := httptest.NewServer(web.Handler(web.Config{Store: st, Gmail: gc, Drafts: dr, Transfers: engine, Telemetry: writer, Events: emitter, Banner: f.Process.Banner, MCP: f.Process.MCP(writer), ServicesPath: servicePath}))
	t.Cleanup(server.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client(), Name: "test", Version: "test"})
	dbHandle.SetFailing(true)
	for _, tc := range []struct {
		name string
		args any
	}{{"compose", map[string]any{"to": []string{"recipient@example.invalid"}}}, {"send", map[string]any{"draft": d.ID}}, {"discard", map[string]any{"draft": d.ID}}, {"pull", map[string]any{"message": "message", "attachment": "part", "path": "new.bin"}}, {"push", map[string]any{"draft": d.ID, "path": "push.bin"}}} {
		args, e := json.Marshal(tc.args)
		if e != nil {
			t.Fatal(e)
		}
		r, e := client.CallTool(ctx, fixtureCaller, tc.name, args)
		if e != nil {
			t.Fatal(e)
		}
		if !r.IsError() {
			t.Fatalf("failing store allowed %s", tc.name)
		}
	}
	if e = emitter.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	if e = writer.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	for _, event := range capture.Events() {
		if trailNames()[event.Name] {
			t.Fatal(event)
		}
	}
	if len(bus.Events()) != 0 {
		t.Fatal(bus.Events())
	}
	f.stop()
}

func TestTrailQueuedWorkAndRecoveredCaller(t *testing.T) {
	// R-XBXC-98VI
	mail := &fakeMail{drafts: map[string]string{}}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/attachments/") {
			once.Do(func() { close(entered); <-release })
		}
		return mail.RoundTrip(r)
	})
	f := startRun(t, runOptions{Transport: tr, Env: map[string]string{"TRANSFER_MAX_ACTIVE": "1"}})
	c1 := fixtureCaller
	c1.RequestID = "immediate-request"
	a := trailAnswer(t, f, c1, "pull", map[string]any{"message": "message", "attachment": "part", "path": "first.bin"})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not start")
	}
	c2 := fixtureCaller
	c2.RequestID = "queued-request"
	b := trailAnswer(t, f, c2, "pull", map[string]any{"message": "message", "attachment": "part", "path": "second.bin"})
	close(release)
	doneA := f.awaitTransfer(c1, trailID(t, a))
	doneB := f.awaitTransfer(c2, trailID(t, b))
	f.stop()
	trailTransferEvent(t, f, c1, doneA, true)
	trailTransferEvent(t, f, c2, doneB, true)
	trailPair(t, f)
	// The previous process's unfinished rows are supplied as isolated persisted state.
	st := f.reopen()
	var pending []store.Transfer
	for _, status := range []string{store.StatusQueued, store.StatusRunning} {
		d, e := st.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: status, Message: "message", Attachment: "part", Path: status + ".bin", UserID: c1.UserID, UserEmail: c1.Email, RequestID: "prior-" + status})
		if e != nil {
			t.Fatal(e)
		}
		pending = append(pending, d)
	}
	next := startRun(t, runOptions{Dir: f.Dir})
	records := []map[string]any{}
	for _, p := range pending {
		c := fixtureCaller
		c.RequestID = "new-reader"
		v := trailAnswer(t, next, c, "transfer", map[string]any{"transfer": p.ID})
		if v["status"] != store.StatusFailed || v["reason"] != store.ReasonInterrupted {
			t.Fatal(v)
		}
		records = append(records, v)
	}
	next.stop()
	for i, p := range pending {
		c := identity.Caller{UserID: p.UserID, Email: p.UserEmail, RequestID: p.RequestID}
		trailTransferEvent(t, next, c, records[i], false)
	}
	trailPair(t, next)
}

func trailNormalizeAnswer(value any) {
	switch v := value.(type) {
	case map[string]any:
		for _, k := range []string{"id", "created", "modified", "started", "finished", "sent", "link", "url"} {
			delete(v, k)
		}
		if id, ok := v["draft"].(string); ok && strings.HasPrefix(id, "drf_") {
			delete(v, "draft")
		}
		for _, x := range v {
			trailNormalizeAnswer(x)
		}
	case []any:
		for _, x := range v {
			trailNormalizeAnswer(x)
		}
	}
}
