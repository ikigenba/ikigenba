package scrapes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func writer(t *testing.T) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := &telemetry.Capture{}
	var diagnostics bytes.Buffer
	w := telemetry.New(telemetry.Config{Service: "reader", Sink: capture, Stderr: &diagnostics, Now: func() time.Time { return time.Unix(100, 0) }, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 256)), Sleep: func(context.Context, time.Duration) { t.Error("unexpected retry") }})
	t.Cleanup(func() {
		w.Shutdown(context.Background(), "test done")
		if diagnostics.Len() != 0 {
			t.Errorf("diagnostics: %s", diagnostics.String())
		}
	})
	return w, capture
}

func server(t *testing.T, handler http.HandlerFunc) services.Entry {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close(); <-done })
	return services.Entry{Name: "scrapes-42", Socket: socket, URL: "invalid public URL"}
}

func answer(t *testing.T, status int, body string) *scrapes.Client {
	t.Helper()
	entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	w, _ := writer(t)
	return scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
}

const validRun = `{"id":"id","scraper":"collector","status":"future-status","started":"2030-01-02T03:04:05+02:00","rows":9223372036854775807,"new":-1,"changed":2,"unchanged":3,"gone":4}`
const validRow = `{"scraper":"collector","run":"id","source_id":"source","collected":"2030-01-02T03:04:05-03:00","digest":"digest","name":"Name","names":[],"people":[],"facts":{}}`

func classify(t *testing.T, err, sentinel error, unexpected bool) {
	t.Helper()
	if err == nil {
		t.Fatal("missing error")
	}
	for _, candidate := range []error{scrapes.ErrNotFound, scrapes.ErrUnavailable} {
		if errors.Is(err, candidate) != errors.Is(sentinel, candidate) {
			t.Fatalf("classification %v as %v", err, candidate)
		}
	}
	var he *scrapes.HTTPError
	if errors.As(err, &he) != unexpected {
		t.Fatal("HTTP classification", err)
	}
}

func malformed(t *testing.T, err error) {
	t.Helper()
	classify(t, err, nil, false)
	if !strings.HasPrefix(err.Error(), "scrapes: ") {
		t.Fatal(err)
	}
}

func runCalls(c *scrapes.Client) []func(context.Context, string) (scrapes.Run, error) {
	return []func(context.Context, string) (scrapes.Run, error){c.Current, c.Record}
}

// R-DYKJ-8TZ7 R-CO8C-PJST R-CPG9-3BJI R-CQO5-H3A7 R-CRW1-UV0W R-CT3Y-8MRL R-CUBU-MEIA R-CVJR-068Z R-CXZJ-RPQD
func TestPublicTypes(t *testing.T) {
	w, _ := writer(t)
	constructors := []func(scrapes.ClientConfig) *scrapes.Client{scrapes.NewClient}
	cfg := scrapes.ClientConfig{Service: services.Entry{Name: "scrapes"}, Telemetry: w}
	var configShape struct {
		Service   services.Entry
		Telemetry *telemetry.Writer
	} = cfg
	client := constructors[0](cfg)
	methods := []func(context.Context, string) (scrapes.Run, error){client.Current, client.Record}
	rowMethods := []func(context.Context, string, func(scrapes.Row) error) error{client.Rows}
	run := scrapes.Run{"id", "scraper", "status", "reason", time.Unix(1, 0), time.Unix(2, 0), 1, 2, 3, 4, 5, "user", "email", "trigger"}
	var runShape struct {
		ID, Scraper, Status, Reason         string
		Started, Finished                   time.Time
		Rows, New, Changed, Unchanged, Gone int64
		User, Email, Trigger                string
	} = run
	address := scrapes.Address{"street", "city", "state", "zip", "county"}
	var addressShape struct{ Street, City, State, Zip, County string } = address
	person := scrapes.Person{"name", "role"}
	var personShape struct{ Name, Role string } = person
	lat, lon := 1.5, 2.5
	row := scrapes.Row{"s", "r", "src", time.Unix(3, 0), "d", "n", []string{"alias"}, address, &lat, &lon, []scrapes.Person{person}, map[string]json.RawMessage{"f": json.RawMessage(`1`)}, json.RawMessage(`null`)}
	var rowShape struct {
		Scraper, Run, SourceID string
		Collected              time.Time
		Digest, Name           string
		Names                  []string
		Address                scrapes.Address
		Lat, Lon               *float64
		People                 []scrapes.Person
		Facts                  map[string]json.RawMessage
		Raw                    json.RawMessage
	} = row
	he := scrapes.HTTPError{499, "body"}
	var httpShape struct {
		StatusCode int
		Body       string
	} = he
	errorMethods := []func() string{he.Error}
	if configShape.Telemetry != w || client == nil || methods[0] == nil || methods[1] == nil || rowMethods[0] == nil || !reflect.DeepEqual(scrapes.Run(runShape), run) || !reflect.DeepEqual(scrapes.Row(rowShape), row) || !reflect.DeepEqual(scrapes.Address(addressShape), address) || !reflect.DeepEqual(scrapes.Person(personShape), person) || httpShape.StatusCode != 499 || httpShape.Body != "body" || errorMethods[0]() == "" {
		t.Fatal("public shape")
	}
}

// R-CWRN-DXZO R-DR94-Y7J1
func TestErrors(t *testing.T) {
	for i, err := range []error{scrapes.ErrNotFound, scrapes.ErrUnavailable} {
		if err == nil || !strings.HasPrefix(err.Error(), "scrapes: ") {
			t.Fatal(err)
		}
		for j, other := range []error{scrapes.ErrNotFound, scrapes.ErrUnavailable} {
			if errors.Is(err, other) != (i == j) {
				t.Fatal("overlapping sentinels")
			}
		}
	}
	for _, code := range []int{0, -1, 200, 599} {
		if (&scrapes.HTTPError{StatusCode: code, Body: "ignored"}).Error() != fmt.Sprintf("scrapes: unexpected HTTP response (status %d)", code) {
			t.Fatal(code)
		}
	}
}

// R-CZ7G-5HH2
func TestConstruction(t *testing.T) {
	func() {
		defer func() {
			if value := recover(); !strings.Contains(fmt.Sprint(value), "telemetry writer is nil") {
				t.Fatalf("panic=%v", value)
			}
		}()
		scrapes.NewClient(scrapes.ClientConfig{})
		t.Error("no panic")
	}()
	w, capture := writer(t)
	contacted := make(chan struct{}, 1)
	live := server(t, func(out http.ResponseWriter, _ *http.Request) { contacted <- struct{}{}; out.WriteHeader(500) })
	for _, entry := range []services.Entry{{}, {Name: "invalid host /", Socket: filepath.Join(t.TempDir(), "missing")}, live} {
		if scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w}) == nil {
			t.Fatal("nil")
		}
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(capture.Events()) != 0 {
		t.Fatal("construction emitted event")
	}
	select {
	case <-contacted:
		t.Fatal("construction contacted service")
	default:
	}
}

// R-D0FC-J97R R-D1N8-X0YG R-D2V5-ASP5 R-D5AY-2C6J R-D6IU-G3X8
func TestRequests(t *testing.T) {
	for _, operation := range []string{"current", "record", "rows"} {
		for _, value := range []string{"", "fmcsa", "a?b", "r?x", "a/b", "goo%67le", "a b", "日本"} {
			for _, caller := range []*identity.Caller{nil, {UserID: "user", Email: "email", RequestID: "request"}, {RequestID: "partial"}} {
				t.Run(operation+"/"+value+"/"+fmt.Sprint(caller), func(t *testing.T) {
					path := "/runs/" + value + "/" + operation
					if operation == "current" {
						path = "/scrapers/" + value + "/current"
					}
					var calls atomic.Int32
					entry := server(t, func(out http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						body, err := io.ReadAll(r.Body)
						if err != nil || len(body) != 0 || r.Method != "GET" || r.Host != "scrapes-42" || r.URL.Path != path || r.URL.RawQuery != "" {
							t.Error("request changed", r, err)
						}
						expected := &http.Request{Header: make(http.Header)}
						if caller != nil {
							identity.Forward(*caller, expected)
						}
						for _, name := range []string{"X-User-Id", "X-User-Email", "X-Request-Id", "X-Event-Cause", "X-Event-Depth"} {
							if !reflect.DeepEqual(r.Header.Values(name), expected.Header.Values(name)) {
								t.Error(name, r.Header.Values(name))
							}
						}
						out.WriteHeader(404)
					})
					w, capture := writer(t)
					ctx := events.NewContext(context.Background(), events.Cause{ID: "cause", Depth: 3})
					if caller != nil {
						ctx = identity.NewContext(ctx, *caller)
					}
					c := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
					var err error
					switch operation {
					case "current":
						_, err = c.Current(ctx, value)
					case "record":
						_, err = c.Record(ctx, value)
					case "rows":
						err = c.Rows(ctx, value, func(scrapes.Row) error { t.Error("callback on refusal"); return nil })
					}
					classify(t, err, scrapes.ErrNotFound, false)
					if err := w.Flush(context.Background()); err != nil {
						t.Fatal(err)
					}
					trail := capture.Events()
					if calls.Load() != 1 || len(trail) != 1 {
						t.Fatal(calls.Load(), trail)
					}
					user, request := "", ""
					if caller != nil {
						user, request = caller.UserID, caller.RequestID
					}
					event := trail[0]
					if event.Name != "sibling.called" || event.Service != "reader" || event.User != user || event.RequestID != request || !event.Time.Equal(time.Unix(100, 0)) || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"target": entry.Name, "method": "GET", "path": path, "status": int64(404), "duration_us": int64(0)}) {
						t.Fatal(event)
					}
				})
			}
		}
	}
}

// R-8Q3B-XHKW R-8RB8-B9BL
func TestRuns(t *testing.T) {
	for _, body := range []string{validRun, strings.TrimSuffix(validRun, "}") + `,"reason":"arbitrary","finished":"2030-01-02T08:09:10-03:00","user":"u","email":"e","trigger":"t","extra":true}`, strings.TrimSuffix(validRun, "}") + `,"ID":"ignored","id":null,"id":"id","STATUS":null,"unknown":[1]}`} {
		for _, call := range runCalls(answer(t, 200, body)) {
			run, err := call(context.Background(), "id")
			started, _ := time.Parse(time.RFC3339, "2030-01-02T03:04:05+02:00")
			expected := scrapes.Run{ID: "id", Scraper: "collector", Status: "future-status", Started: started.UTC(), Rows: 9223372036854775807, New: -1, Changed: 2, Unchanged: 3, Gone: 4}
			if strings.Contains(body, `"finished"`) {
				finished, _ := time.Parse(time.RFC3339, "2030-01-02T08:09:10-03:00")
				expected.Finished = finished.UTC()
				expected.Reason = "arbitrary"
				expected.User = "u"
				expected.Email = "e"
				expected.Trigger = "t"
			}
			if err != nil || !reflect.DeepEqual(run, expected) {
				t.Fatal(run, expected, err)
			}
		}
	}
	empty := `{"id":"","scraper":"","status":"","started":"2030-01-02T03:04:05Z","rows":0,"new":0,"changed":0,"unchanged":0,"gone":0}`
	for _, call := range runCalls(answer(t, 200, empty)) {
		if _, err := call(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
	}
}

func mutate(t *testing.T, body, key string, value json.RawMessage, remove bool) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	if remove {
		delete(fields, key)
	} else {
		fields[key] = value
	}
	result, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(result)
}

// R-8Q3B-XHKW R-8SJ4-P12A R-DBEF-Z6W0
func TestMalformedRuns(t *testing.T) {
	bodies := []string{"", "null", "[]", "true", "1", "{}", "{", validRun + " {}", validRun + " trailing", string([]byte{123, 34, 120, 34, 58, 34, 255, 34, 125}), strings.Replace(validRun, `"id"`, `"ID"`, 1), strings.TrimSuffix(validRun, "}") + `,"id":null}`, strings.TrimSuffix(validRun, "}") + `,"id":1}`}
	for _, key := range []string{"id", "scraper", "status", "reason", "started", "finished", "user", "email", "trigger"} {
		for _, bad := range []string{"null", "1", "true", "[]", "{}"} {
			bodies = append(bodies, mutate(t, validRun, key, json.RawMessage(bad), false))
		}
		if key == "id" || key == "scraper" || key == "status" || key == "started" {
			bodies = append(bodies, mutate(t, validRun, key, nil, true))
		}
	}
	for _, key := range []string{"started", "finished"} {
		bodies = append(bodies, mutate(t, validRun, key, json.RawMessage(`"bad time"`), false))
	}
	for _, key := range []string{"rows", "new", "changed", "unchanged", "gone"} {
		for _, bad := range []string{"null", "1.5", "1e0", "9223372036854775808", `"1"`, "true", "[]", "{}"} {
			bodies = append(bodies, mutate(t, validRun, key, json.RawMessage(bad), false))
		}
		bodies = append(bodies, mutate(t, validRun, key, nil, true))
	}
	for i, body := range bodies {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			for _, call := range runCalls(answer(t, 200, body)) {
				run, err := call(context.Background(), "id")
				malformed(t, err)
				if !reflect.DeepEqual(run, scrapes.Run{}) {
					t.Fatal(run)
				}
			}
		})
	}
}

// R-8Q3B-XHKW R-8NNJ-5Y3I R-8TR1-2SSZ
func TestRows(t *testing.T) {
	full := strings.TrimSuffix(validRow, "}") + `,"names":["alias","other"],"address":{"street":"st","city":"city","state":"state","zip":"zip","county":"county","CITY":"ignore","city":null,"city":"city","extra":false},"lat":1.25,"lon":-2.5,"people":[{"name":"Person","role":"Role","NAME":null},{"role":"secondary"},{}],"facts":{"f":null,"f": { "v" : [1, 2] },"n":1.00},"raw": { "source" : [true, null] },"unknown":1,"NAME":"ignored","name":null,"name":"Name"}`
	for _, body := range []string{"", validRow + "\n", full + "\n" + validRow + "\n", strings.TrimSuffix(validRow, "}") + `,"lat":null,"lon":null,"raw":null}` + "\n"} {
		var rows []scrapes.Row
		err := answer(t, 200, body).Rows(context.Background(), "id", func(row scrapes.Row) error { rows = append(rows, row); return nil })
		if err != nil || len(rows) != strings.Count(body, "\n") {
			t.Fatal(err, len(rows))
		}
		for i, row := range rows {
			instant, _ := time.Parse(time.RFC3339, "2030-01-02T03:04:05-03:00")
			expected := scrapes.Row{Scraper: "collector", Run: "id", SourceID: "source", Collected: instant.UTC(), Digest: "digest", Name: "Name", Names: []string{}, People: []scrapes.Person{}, Facts: map[string]json.RawMessage{}}
			if strings.HasPrefix(body, full) && i == 0 {
				lat, lon := 1.25, -2.5
				expected.Names = []string{"alias", "other"}
				expected.Address = scrapes.Address{Street: "st", City: "city", State: "state", Zip: "zip", County: "county"}
				expected.Lat = &lat
				expected.Lon = &lon
				expected.People = []scrapes.Person{{Name: "Person", Role: "Role"}, {Role: "secondary"}, {}}
				expected.Facts = map[string]json.RawMessage{"f": json.RawMessage(`{ "v" : [1, 2] }`), "n": json.RawMessage(`1.00`)}
				expected.Raw = json.RawMessage(`{ "source" : [true, null] }`)
			}
			if strings.Contains(body, `"raw":null`) {
				expected.Raw = json.RawMessage(`null`)
			}
			if !reflect.DeepEqual(row, expected) {
				t.Fatalf("row=%+v want=%+v", row, expected)
			}
		}
	}
	for _, raw := range []string{`1`, `"text"`, `true`, `[]`, `{}`} {
		body := mutate(t, validRow, "raw", json.RawMessage(raw), false) + "\n"
		if err := answer(t, 200, body).Rows(context.Background(), "id", func(row scrapes.Row) error {
			if string(row.Raw) != raw {
				t.Fatal(row.Raw)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// R-8Q3B-XHKW R-8NNJ-5Y3I R-DHHX-W1LH
func TestMalformedRows(t *testing.T) {
	bodies := []string{"", "null", "[]", "true", "1", "{}", "{", validRow + " {}", validRow + " trailing", strings.Replace(validRow, `"scraper"`, `"SCRAPER"`, 1), strings.TrimSuffix(validRow, "}") + `,"name":null}`, string([]byte{123, 34, 120, 34, 58, 34, 255, 34, 125})}
	for _, key := range []string{"scraper", "run", "source_id", "digest", "name", "collected"} {
		for _, bad := range []string{"null", "1", "true", "[]", "{}"} {
			bodies = append(bodies, mutate(t, validRow, key, json.RawMessage(bad), false))
		}
		bodies = append(bodies, mutate(t, validRow, key, nil, true))
	}
	bodies = append(bodies, mutate(t, validRow, "collected", json.RawMessage(`"bad time"`), false))
	for _, key := range []string{"names", "people", "facts"} {
		bodies = append(bodies, mutate(t, validRow, key, nil, true))
		for _, bad := range []string{"null", "1", "true", `"text"`} {
			bodies = append(bodies, mutate(t, validRow, key, json.RawMessage(bad), false))
		}
	}
	for _, bad := range []string{`{}`, `[null]`, `["ok",1]`, `[{}]`} {
		bodies = append(bodies, mutate(t, validRow, "names", json.RawMessage(bad), false))
	}
	for _, bad := range []string{`{}`, `[null]`, `[1]`, `[[]]`, `["person"]`, `[{"name":null}]`, `[{"role":1}]`, `[{"role":null}]`, `[{"name":false}]`} {
		bodies = append(bodies, mutate(t, validRow, "people", json.RawMessage(bad), false))
	}
	bodies = append(bodies, mutate(t, validRow, "facts", json.RawMessage(`[]`), false))
	for _, bad := range []string{`null`, `1`, `[]`, `"address"`, `false`} {
		bodies = append(bodies, mutate(t, validRow, "address", json.RawMessage(bad), false))
	}
	for _, key := range []string{"street", "city", "state", "zip", "county"} {
		for _, bad := range []string{`null`, `1`, `true`, `[]`, `{}`} {
			bodies = append(bodies, mutate(t, validRow, "address", json.RawMessage(`{"`+key+`":`+bad+`}`), false))
		}
	}
	for _, key := range []string{"lat", "lon"} {
		for _, bad := range []string{`"1"`, `true`, `[]`, `{}`, `1e9999`} {
			bodies = append(bodies, mutate(t, validRow, key, json.RawMessage(bad), false))
		}
	}
	for i, body := range bodies {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			count := 0
			err := answer(t, 200, validRow+"\n"+body+"\n"+validRow+"\n").Rows(context.Background(), "id", func(row scrapes.Row) error {
				count++
				if row.SourceID != "source" {
					t.Fatal(row)
				}
				return nil
			})
			malformed(t, err)
			if count != 1 {
				t.Fatal(count)
			}
		})
	}
	count := 0
	err := answer(t, 200, "\n\n\n").Rows(context.Background(), "id", func(scrapes.Row) error { count++; return nil })
	malformed(t, err)
	if count != 0 {
		t.Fatal(count)
	}
}

// R-DIPU-9TC6
func TestIncompleteTail(t *testing.T) {
	for _, body := range []string{validRow, validRow + "\n" + validRow, validRow + "\n" + "partial", " "} {
		count := 0
		err := answer(t, 200, body).Rows(context.Background(), "id", func(scrapes.Row) error { count++; return nil })
		malformed(t, err)
		if count != strings.Count(body, "\n") {
			t.Fatal(count)
		}
	}
}

// R-DF25-4I43
func TestRowsArriveBeforeEnd(t *testing.T) {
	delivered := make(chan struct{})
	release := make(chan struct{})
	entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, validRow+"\n"+validRow+"\n")
		w.(http.Flusher).Flush()
		<-delivered
		<-release
		_, _ = io.WriteString(w, validRow+"\n")
	})
	w, _ := writer(t)
	c := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
	result := make(chan error, 1)
	go func() {
		count := 0
		result <- c.Rows(context.Background(), "id", func(scrapes.Row) error {
			count++
			if count == 2 {
				close(delivered)
			}
			return nil
		})
	}()
	<-delivered
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

// R-DGA1-I9US
func TestUnboundedLine(t *testing.T) {
	name := strings.Repeat("x", 16*1024*1024)
	body := mutate(t, validRow, "name", json.RawMessage(`"`+name+`"`), false) + "\n"
	count := 0
	err := answer(t, 200, body).Rows(context.Background(), "id", func(row scrapes.Row) error {
		count++
		if row.Name != name {
			t.Fatal("line truncated")
		}
		return nil
	})
	if err != nil || count != 1 {
		t.Fatal(err, count)
	}
}

// R-DOTC-6O1N R-DQ18-KFSC R-DMDJ-F4K9 R-DBEF-Z6W0 R-DW4Q-HAHT
func TestStatuses(t *testing.T) {
	for _, status := range []int{201, 300, 301, 302, 303, 304, 305, 307, 308, 400, 401, 404, 500, 503} {
		for _, body := range []string{"", validRun, "not JSON", strings.Repeat("x", 8000)} {
			for _, location := range []string{"", "/redirected"} {
				t.Run(fmt.Sprint(status)+"/"+fmt.Sprint(len(body))+location, func(t *testing.T) {
					var calls atomic.Int32
					entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
						calls.Add(1)
						w.Header().Set("Content-Type", "application/octet-stream")
						if location != "" {
							w.Header().Set("Location", location)
						}
						w.WriteHeader(status)
						_, _ = io.WriteString(w, body)
					})
					w, _ := writer(t)
					c := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
					check := func(err error) {
						var sentinel error
						if status == 404 {
							sentinel = scrapes.ErrNotFound
						}
						if status == 503 {
							sentinel = scrapes.ErrUnavailable
						}
						classify(t, err, sentinel, sentinel == nil)
						if sentinel == nil {
							var he *scrapes.HTTPError
							if !errors.As(err, &he) {
								t.Fatal(err)
							}
							prefix := body
							if status == 304 {
								prefix = ""
							}
							if len(prefix) > 4096 {
								prefix = prefix[:4096]
							}
							if he.StatusCode != status || he.Body != prefix {
								t.Fatal(he)
							}
						}
					}
					for _, call := range runCalls(c) {
						run, err := call(context.Background(), "id")
						check(err)
						if !reflect.DeepEqual(run, scrapes.Run{}) {
							t.Fatal(run)
						}
					}
					check(c.Rows(context.Background(), "id", func(scrapes.Row) error { t.Error("callback on status"); return nil }))
					if calls.Load() != 3 {
						t.Fatal("redirect followed", calls.Load())
					}
				})
			}
		}
	}
}

// R-DTOX-PR0F R-DMDJ-F4K9 R-DBEF-Z6W0 R-D5AY-2C6J
func TestTransportFailures(t *testing.T) {
	w, capture := writer(t)
	c := scrapes.NewClient(scrapes.ClientConfig{Service: services.Entry{Name: "scrapes", Socket: filepath.Join(t.TempDir(), "missing")}, Telemetry: w})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	for _, tc := range []struct {
		ctx      context.Context
		expected error
	}{{context.Background(), nil}, {canceled, context.Canceled}, {expired, context.DeadlineExceeded}} {
		check := func(err error) {
			classify(t, err, nil, false)
			if tc.expected != nil {
				if !errors.Is(err, tc.expected) {
					t.Fatal(err)
				}
			} else {
				var op *net.OpError
				if !errors.As(err, &op) || op.Op != "dial" {
					t.Fatal(err)
				}
			}
		}
		for _, call := range runCalls(c) {
			run, err := call(tc.ctx, "id")
			check(err)
			if !reflect.DeepEqual(run, scrapes.Run{}) {
				t.Fatal(run)
			}
		}
		check(c.Rows(tc.ctx, "id", func(scrapes.Row) error { t.Error("callback on transport failure"); return nil }))
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 9 {
		t.Fatal(trail)
	}
	for _, e := range trail {
		if e.Name != "sibling.called" || e.Attrs["status"] != int64(0) {
			t.Fatal(e)
		}
	}
}

// R-DJXQ-NL2V
func TestStreamFailures(t *testing.T) {
	t.Run("chunk closed", func(t *testing.T) {
		entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
			conn, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = conn.Close() }()
			line := validRow + "\n"
			_, _ = fmt.Fprintf(buffer, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n100\r\npartial", len(line), line)
			_ = buffer.Flush()
		})
		w, _ := writer(t)
		count := 0
		err := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w}).Rows(context.Background(), "id", func(scrapes.Row) error { count++; return nil })
		classify(t, err, nil, false)
		if !errors.Is(err, io.ErrUnexpectedEOF) || count != 1 {
			t.Fatal(err, count)
		}
	})
	t.Run("cancel read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ended := make(chan struct{})
		entry := server(t, func(w http.ResponseWriter, r *http.Request) {
			defer close(ended)
			_, _ = io.WriteString(w, validRow+"\npartial")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		w, _ := writer(t)
		count := 0
		err := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w}).Rows(ctx, "id", func(scrapes.Row) error { count++; cancel(); return nil })
		classify(t, err, nil, false)
		if !errors.Is(err, context.Canceled) || count != 1 {
			t.Fatal(err, count)
		}
		<-ended
	})
}

// R-DL5N-1CTK R-8UYX-GKJO
func TestClosesLargeBodies(t *testing.T) {
	stop := errors.New("consumer stop")
	for _, mode := range []string{"current", "record", "rows-status", "rows-invalid", "rows-stop"} {
		t.Run(mode, func(t *testing.T) {
			failed := make(chan error, 1)
			entry := server(t, func(w http.ResponseWriter, _ *http.Request) {
				if mode == "rows-invalid" || mode == "rows-stop" {
					w.WriteHeader(200)
					first := "invalid\n"
					if mode == "rows-stop" {
						first = validRow + "\n"
					}
					_, _ = io.WriteString(w, first)
					w.(http.Flusher).Flush()
				} else {
					w.WriteHeader(500)
				}
				block := bytes.Repeat([]byte{'x'}, 64*1024)
				if mode == "rows-stop" {
					block = []byte(strings.Repeat(validRow+"\n", 400))
				}
				remaining := 64 * 1024 * 1024
				for remaining > 0 {
					n, err := w.Write(block)
					if err != nil {
						failed <- err
						return
					}
					remaining -= n
				}
				failed <- nil
			})
			w, _ := writer(t)
			c := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
			count := 0
			var err error
			switch mode {
			case "current":
				_, err = c.Current(context.Background(), "id")
			case "record":
				_, err = c.Record(context.Background(), "id")
			default:
				err = c.Rows(context.Background(), "id", func(scrapes.Row) error { count++; return stop })
			}
			switch mode {
			case "rows-stop":
				if reflect.ValueOf(err) != reflect.ValueOf(stop) || count != 1 {
					t.Fatal(err, count)
				}
			case "rows-invalid":
				malformed(t, err)
				if count != 0 {
					t.Fatal(count)
				}
			default:
				classify(t, err, nil, true)
			}
			if err := <-failed; err == nil {
				t.Fatal("server wrote whole body")
			}
		})
	}
}

// R-DXCM-V28I
func TestConcurrentCalls(t *testing.T) {
	entry := server(t, func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		id := parts[2]
		if r.Header.Get("X-User-Id") != id {
			t.Error("caller mixed")
		}
		if parts[3] == "rows" {
			_, _ = io.WriteString(w, mutate(t, validRow, "source_id", json.RawMessage(`"`+id+`"`), false)+"\n")
		} else {
			_, _ = io.WriteString(w, mutate(t, validRun, "id", json.RawMessage(`"`+id+`"`), false))
		}
	})
	w, capture := writer(t)
	c := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			id := fmt.Sprint(i)
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: id, RequestID: "request" + id})
			for _, call := range runCalls(c) {
				run, err := call(ctx, id)
				if err != nil || run.ID != id {
					t.Error(run, err)
				}
			}
			count := 0
			err := c.Rows(ctx, id, func(row scrapes.Row) error {
				count++
				if row.SourceID != id {
					t.Error(row)
				}
				return nil
			})
			if err != nil || count != 1 {
				t.Error(err, count)
			}
		})
	}
	wg.Wait()
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	trail := capture.Events()
	if len(trail) != 96 {
		t.Fatal(len(trail))
	}
	for _, e := range trail {
		if e.RequestID != "request"+e.User {
			t.Fatal(e)
		}
	}
}

// R-DZSF-MLPW R-E10C-0DGL
func TestProcessIndependence(t *testing.T) {
	stdout, stderr := os.Stdout, os.Stderr
	readOut, writeOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	readErr, writeErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldLog := log.Writer()
	var logOutput bytes.Buffer
	os.Stdout, os.Stderr = writeOut, writeErr
	log.SetOutput(&logOutput)
	defer func() {
		os.Stdout, os.Stderr = stdout, stderr
		log.SetOutput(oldLog)
		_ = readOut.Close()
		_ = readErr.Close()
		_ = writeOut.Close()
		_ = writeErr.Close()
	}()
	entry := server(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/rows") {
			_, _ = io.WriteString(w, validRow+"\n")
		} else {
			_, _ = io.WriteString(w, validRun)
		}
	})
	w, _ := writer(t)
	var previousRuns []scrapes.Run
	var previousRows []scrapes.Row
	var previousErrors []string
	for i := range 2 {
		t.Chdir(t.TempDir())
		c := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
		var runs []scrapes.Run
		for _, call := range runCalls(c) {
			run, err := call(context.Background(), "id")
			if err != nil {
				t.Fatal(err)
			}
			runs = append(runs, run)
		}
		var rows []scrapes.Row
		if err := c.Rows(context.Background(), "id", func(row scrapes.Row) error { rows = append(rows, row); return nil }); err != nil {
			t.Fatal(err)
		}
		var errs []string
		for _, status := range []int{404, 503, 500, 200} {
			failing := answer(t, status, "invalid\n")
			for _, call := range runCalls(failing) {
				_, err := call(context.Background(), "id")
				if err == nil {
					t.Fatal("nil error")
				}
				errs = append(errs, err.Error())
			}
			err := failing.Rows(context.Background(), "id", func(scrapes.Row) error { t.Error("unexpected row"); return nil })
			if err == nil {
				t.Fatal("nil error")
			}
			errs = append(errs, err.Error())
		}
		he := &scrapes.HTTPError{StatusCode: 599}
		errs = append(errs, he.Error(), scrapes.ErrNotFound.Error(), scrapes.ErrUnavailable.Error())
		if i > 0 && (!reflect.DeepEqual(runs, previousRuns) || !reflect.DeepEqual(rows, previousRows) || !reflect.DeepEqual(errs, previousErrors)) {
			t.Fatal("working directory changed outcome")
		}
		previousRuns, previousRows, previousErrors = runs, rows, errs
	}
	_ = writeOut.Close()
	_ = writeErr.Close()
	out, outErr := io.ReadAll(readOut)
	diagnostics, diagErr := io.ReadAll(readErr)
	if outErr != nil || diagErr != nil || len(out) != 0 || len(diagnostics) != 0 || logOutput.Len() != 0 {
		t.Fatalf("output=%q stderr=%q log=%q errors=%v %v", out, diagnostics, logOutput.String(), outErr, diagErr)
	}
}

// R-D5AY-2C6J
func TestTelemetryDuration(t *testing.T) {
	for _, operation := range []string{"current", "record", "rows"} {
		for _, elapsed := range []time.Duration{789*time.Microsecond + 900*time.Nanosecond, -time.Microsecond} {
			t.Run(operation+elapsed.String(), func(t *testing.T) {
				var clockReads atomic.Int32
				capture := &telemetry.Capture{}
				var diagnostics bytes.Buffer
				start := time.Unix(100, 0)
				w := telemetry.New(telemetry.Config{Service: "reader", Sink: capture, Stderr: &diagnostics, Now: func() time.Time {
					if clockReads.Add(1) == 1 {
						return start
					}
					return start.Add(elapsed)
				}, Rand: bytes.NewReader(make([]byte, 16)), Sleep: func(context.Context, time.Duration) { t.Error("unexpected pause") }})
				t.Cleanup(func() {
					w.Shutdown(context.Background(), "done")
					if diagnostics.Len() != 0 {
						t.Error(diagnostics.String())
					}
				})
				entry := server(t, func(out http.ResponseWriter, _ *http.Request) { out.WriteHeader(500) })
				c := scrapes.NewClient(scrapes.ClientConfig{Service: entry, Telemetry: w})
				switch operation {
				case "current":
					_, _ = c.Current(context.Background(), "id")
				case "record":
					_, _ = c.Record(context.Background(), "id")
				case "rows":
					_ = c.Rows(context.Background(), "id", func(scrapes.Row) error { t.Error("callback on refusal"); return nil })
				}
				if err := w.Flush(context.Background()); err != nil {
					t.Fatal(err)
				}
				expected := int64(elapsed / time.Microsecond)
				if expected < 0 {
					expected = 0
				}
				trail := capture.Events()
				if len(trail) != 1 || trail[0].Attrs["duration_us"] != expected {
					t.Fatal(trail)
				}
			})
		}
	}
}
