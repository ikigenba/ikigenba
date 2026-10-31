package pages_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home/internal/bookmarks"
	"github.com/ikigenba/ikigenba/home/internal/pages"
)

func signedInteger(t *testing.T, value any) int64 {
	t.Helper()
	v := reflect.ValueOf(value)
	if v.IsValid() {
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return v.Int()
		}
	}
	t.Fatalf("attribute is not a signed integer: %v", value)
	return 0
}

type eventBody struct {
	io.Reader
	beforeRead func()
}

func (b *eventBody) Read(p []byte) (int, error) {
	if b.beforeRead != nil {
		f := b.beforeRead
		b.beforeRead = nil
		f()
	}
	return b.Reader.Read(p)
}

func (*eventBody) Close() error { return nil }

type eventResponse struct {
	*httptest.ResponseRecorder
	beforeHeader func()
}

func (w eventResponse) WriteHeader(status int) {
	w.beforeHeader()
	w.ResponseRecorder.WriteHeader(status)
}

// R-2YPR-Y8EC
func TestMoveEventShapeWhileRequestStateChanges(t *testing.T) {
	for _, direction := range []string{"left", "right", "", "up"} {
		for _, ending := range []string{"live", "canceled", "unreachable"} {
			t.Run(direction+"/"+ending, func(t *testing.T) {
				d, s := database(t)
				for _, name := range []string{"first", "second", "third"} {
					create(t, s, name, bookmarks.Icon{})
				}
				id := held(t, s)[2].ID
				capture := new(telemetry.Capture)
				w := writer(t, capture, new(bytes.Buffer))
				h := testHandler(t, pages.Config{Store: s, Telemetry: w})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				r := moveRequest(id, "", "application/x-www-form-urlencoded").WithContext(ctx)
				r.Header["X-User-Id"] = []string{"caller", "ignored-user"}
				r.Header["X-Request-Id"] = []string{"changing-request", "ignored-request"}
				r.Body = &eventBody{Reader: strings.NewReader("to=" + direction), beforeRead: func() {
					// A separate store operation changes the order during the request.
					if _, changed, err := s.Move(context.Background(), "caller", id, bookmarks.Left); err != nil || !changed {
						t.Fatalf("interleaved move: changed %v, error %v", changed, err)
					}
				}}
				out := httptest.NewRecorder()
				h.ServeHTTP(eventResponse{out, func() {
					switch ending {
					case "canceled":
						cancel()
					case "unreachable":
						d.SetFailing(true)
					}
				}}, r)
				if out.Code != http.StatusSeeOther {
					t.Fatalf("expected a 303 to exercise its event contract, got %d", out.Code)
				}
				flush(t, w)
				started, finished := -1, -1
				var moves []int
				events := capture.Events()
				for i, event := range events {
					switch event.Name {
					case "request.started":
						started = i
					case "request.finished":
						finished = i
					case "bookmark.moved":
						moves = append(moves, i)
					}
				}
				if len(moves) > 1 || ((direction != "left" && direction != "right") && len(moves) != 0) {
					t.Fatal(events)
				}
				for _, i := range moves {
					event := events[i]
					if started < 0 || started >= i || finished <= i || event.RequestID != "changing-request" || event.User != "caller" || len(event.Attrs) != 3 || event.Attrs["bookmark"] != id {
						t.Fatal(event, events)
					}
					from, to := signedInteger(t, event.Attrs["from"]), signedInteger(t, event.Attrs["to"])
					delta := int64(-1)
					if direction == "right" {
						delta = 1
					}
					if from < 0 || to != from+delta {
						t.Fatal(event)
					}
				}
			})
		}
	}
}
