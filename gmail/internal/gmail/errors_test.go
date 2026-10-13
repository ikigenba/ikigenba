package gmail_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
)

type invocation struct {
	name string
	call func(*gmail.Client, context.Context) (any, error)
	zero any
	pre  []reply
}

func invocations() []invocation {
	aid := "attachment"
	return []invocation{
		{"labels", func(c *gmail.Client, ctx context.Context) (any, error) { return c.Labels(ctx, syntheticToken) }, []gmail.Label(nil), nil},
		{"search", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.Search(ctx, syntheticToken, gmail.Query{Limit: gmail.PageSize, Page: "opaque"})
		}, gmail.Page{}, []reply{labelsJSON()}},
		{"thread", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.Thread(ctx, syntheticToken, "thread", "")
		}, gmail.Page{}, []reply{labelsJSON()}},
		{"show", func(c *gmail.Client, ctx context.Context) (any, error) { return c.Show(ctx, syntheticToken, "message") }, gmail.Message{}, []reply{labelsJSON()}},
		{"stat", func(c *gmail.Client, ctx context.Context) (any, error) { return c.Stat(ctx, syntheticToken, "message") }, gmail.Stat{}, nil},
		{"threadstats", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.ThreadStats(ctx, syntheticToken, "thread")
		}, []gmail.Stat(nil), nil},
		{"attachment", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.Attachment(ctx, syntheticToken, "message", "1", 4)
		}, gmail.Content{}, []reply{okJSON(attachmentJSON(4, nil, &aid))}},
		{"raw", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.Raw(ctx, syntheticToken, "message", 4)
		}, gmail.Content{}, nil},
		{"create", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.CreateDraft(ctx, syntheticToken, "", gmail.Build(composition()))
		}, gmail.Draft{}, nil},
		{"getdraft", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.GetDraft(ctx, syntheticToken, "draft")
		}, gmail.Draft{}, nil},
		{"update", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.UpdateDraft(ctx, syntheticToken, "draft", "", gmail.Build(composition()))
		}, gmail.Draft{}, nil},
		{"send", func(c *gmail.Client, ctx context.Context) (any, error) {
			return c.SendDraft(ctx, syntheticToken, "draft")
		}, gmail.Draft{}, nil},
		{"delete", func(c *gmail.Client, ctx context.Context) (any, error) {
			return nil, c.DeleteDraft(ctx, syntheticToken, "draft")
		}, nil, nil},
	}
}

func TestStatusCategories(t *testing.T) {
	// R-26ZX-T2EL R-I8C7-0IGX R-5TRX-70ZD
	for _, in := range invocations() {
		for _, status := range []int{302, 400, 401, 403, 404, 413, 429, 500} {
			t.Run(in.name+"/"+http.StatusText(status), func(t *testing.T) {
				replies := append(append([]reply(nil), in.pre...), reply{status: status, body: `{"error":{"message":"synthetic echoed secret"}}`})
				f, c := newFake(t, replies...)
				value, err := in.call(c, context.Background())
				want := gmail.ErrProvider
				if status == 401 {
					want = gmail.ErrUnauthorized
				}
				if status == 413 {
					want = gmail.ErrTooLarge
				}
				if status == 400 && in.name == "search" {
					want = gmail.ErrBadPage
				}
				if (status == 400 || status == 404) && (in.name == "thread" || in.name == "show" || in.name == "stat" || in.name == "threadstats" || in.name == "attachment" || in.name == "raw" || in.name == "getdraft" || in.name == "delete") {
					want = gmail.ErrNotFound
				}
				if status == 404 && (in.name == "send" || in.name == "update") {
					want = gmail.ErrNotFound
				}
				requireError(t, err, want)
				if !reflect.DeepEqual(value, in.zero) || len(f.calls()) != len(replies) {
					t.Fatal(value, f.calls())
				}
			})
		}
	}
	// A messages.list rejection without an input page remains a provider error.
	_, c := newFake(t, labelsJSON(), reply{status: 400})
	_, err := c.Search(context.Background(), syntheticToken, gmail.Query{Limit: gmail.PageSize})
	requireError(t, err, gmail.ErrProvider)
}

func TestEmptyIDsAndEscaping(t *testing.T) {
	// R-5RC4-FHHZ
	f, c := newFake(t)
	ctx := context.Background()
	var errs []error
	_, err := c.Thread(ctx, syntheticToken, "", "")
	errs = append(errs, err)
	_, err = c.ThreadStats(ctx, syntheticToken, "")
	errs = append(errs, err)
	_, err = c.Show(ctx, syntheticToken, "")
	errs = append(errs, err)
	_, err = c.Stat(ctx, syntheticToken, "")
	errs = append(errs, err)
	_, err = c.Attachment(ctx, syntheticToken, "", "1", 1)
	errs = append(errs, err)
	_, err = c.Attachment(ctx, syntheticToken, "message", "", 1)
	errs = append(errs, err)
	_, err = c.Raw(ctx, syntheticToken, "", 1)
	errs = append(errs, err)
	_, err = c.GetDraft(ctx, syntheticToken, "")
	errs = append(errs, err)
	_, err = c.UpdateDraft(ctx, syntheticToken, "", "", gmail.Build(composition()))
	errs = append(errs, err)
	_, err = c.SendDraft(ctx, syntheticToken, "")
	errs = append(errs, err)
	err = c.DeleteDraft(ctx, syntheticToken, "")
	errs = append(errs, err)
	for _, err := range errs {
		requireError(t, err, gmail.ErrNotFound)
	}
	if len(f.calls()) != 0 {
		t.Fatal(f.calls())
	}
	for _, in := range []struct {
		call func(*gmail.Client)
		pre  []reply
		path string
	}{
		{func(c *gmail.Client) { _, _ = c.Show(ctx, syntheticToken, "one/two ?#%") }, []reply{labelsJSON()}, "/gmail/v1/users/me/messages/one%2Ftwo%20%3F%23%25"},
		{func(c *gmail.Client) { _, _ = c.Thread(ctx, syntheticToken, "one/two ?#%", "") }, []reply{labelsJSON()}, "/gmail/v1/users/me/threads/one%2Ftwo%20%3F%23%25"},
		{func(c *gmail.Client) { _ = c.DeleteDraft(ctx, syntheticToken, "one/two ?#%") }, nil, "/gmail/v1/users/me/drafts/one%2Ftwo%20%3F%23%25"},
	} {
		f, c := newFake(t, append(in.pre, reply{status: 404})...)
		in.call(c)
		if got := f.calls()[len(in.pre)].path; got != in.path {
			t.Fatal(got, in.path)
		}
	}
}

func TestMalformedAnswers(t *testing.T) {
	// R-IARZ-S1YB
	for _, in := range invocations() {
		if in.name == "delete" || in.name == "update" {
			continue
		}
		for _, body := range []string{`null`, `[]`, `invalid`, `{"id":5}`, `{"internalDate":"bad-date"}`, `{"labels":{}}`, `{"messages":null}`, `{"parts":3}`} {
			replies := append(append([]reply(nil), in.pre...), okJSON(body))
			_, c := newFake(t, replies...)
			v, err := in.call(c, context.Background())
			if in.name == "labels" && (body == `{"internalDate":"bad-date"}` || body == `{"id":5}` || body == `{"messages":null}` || body == `{"parts":3}`) {
				continue
			}
			if (in.name == "search" || in.name == "thread" || in.name == "threadstats") && (body == `{"internalDate":"bad-date"}` || body == `{"id":5}`) {
				continue
			}
			requireError(t, err, gmail.ErrProvider)
			if !reflect.DeepEqual(v, in.zero) {
				t.Fatal(in.name, v)
			}
		}
	}
	for _, body := range []string{`{"id":"message","internalDate":"not-decimal"}`, `{"id":"message","payload":{"parts":null}}`, `{"id":"message","payload":{"parts":[false]}}`, `{"id":"message","payload":{"body":{"data":"not@base64"}}}`} {
		_, c := newFake(t, labelsJSON(), okJSON(body))
		_, err := c.Show(context.Background(), syntheticToken, "message")
		requireError(t, err, gmail.ErrProvider)
	}
	for _, body := range []string{`{"raw":"not@base64"}`, `{"raw":null}`} {
		_, c := newFake(t, okJSON(body))
		_, err := c.Raw(context.Background(), syntheticToken, "message", 100)
		requireError(t, err, gmail.ErrProvider)
	}
}

type observedBody struct {
	reader io.Reader
	closed bool
}

func TestPresentNullMessageFields(t *testing.T) {
	// R-IARZ-S1YB R-5TRX-70ZD
	for _, in := range invocations() {
		if in.name != "show" && in.name != "stat" && in.name != "search" && in.name != "thread" && in.name != "threadstats" {
			continue
		}
		for _, message := range []string{
			`{"id":"message","internalDate":null}`,
			`{"id":"message","payload":{"mimeType":"text/plain","body":{"data":null}}}`,
			`{"id":"message","payload":{"parts":[{"parts":[{"body":{"data":null}}]}]}}`,
		} {
			t.Run(in.name+"/"+message, func(t *testing.T) {
				body := message
				pre := append([]reply(nil), in.pre...)
				if in.name == "thread" || in.name == "threadstats" {
					body = `{"messages":[` + message + `]}`
				}
				if in.name == "search" {
					pre = append(pre, okJSON(`{"messages":[{"id":"message"}]}`))
				}
				_, c := newFake(t, append(pre, okJSON(body))...)
				v, err := in.call(c, context.Background())
				requireError(t, err, gmail.ErrProvider)
				if !reflect.DeepEqual(v, in.zero) {
					t.Fatal(v)
				}
			})
		}
	}
	// Absent date/data and a present empty base64url string remain valid.
	for _, body := range []string{`{"id":"message"}`, `{"id":"message","payload":{"mimeType":"text/plain","body":{}}}`, `{"id":"message","payload":{"parts":[{"mimeType":"text/plain","body":{"data":""}}]}}`} {
		_, c := newFake(t, labelsJSON(), okJSON(body))
		m, err := c.Show(context.Background(), syntheticToken, "message")
		if err != nil || m.Body != "" || m.Date.UnixMilli() != 0 {
			t.Fatal(m, err)
		}
	}
}

func TestIntermediateUploadBodyFailure(t *testing.T) {
	// R-2FJ8-HGLG R-5TRX-70ZD
	for _, tc := range []struct{ canceled, invalidRange bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var bodies []*observedBody
			calls := 0
			c := gmail.NewClient(gmail.Config{HTTP: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				calls++
				h := make(http.Header)
				status := 200
				var reader io.Reader = strings.NewReader("")
				if calls == 1 {
					h.Set("Location", "https://session.invalid/chunks")
				} else {
					status = 308
					h.Set("Range", fmt.Sprintf("bytes=0-%d", gmail.PushChunk-1))
					if tc.invalidRange {
						h.Set("Range", "bytes=0-1")
					}
					reader = failedReader{errors.New("synthetic body read failure")}
					if tc.canceled {
						reader = cancelingFailedReader{cancel: cancel}
					}
				}
				b := &observedBody{reader: reader}
				bodies = append(bodies, b)
				return &http.Response{StatusCode: status, Header: h, Body: b}, nil
			})}})
			d, err := c.UpdateDraft(ctx, syntheticToken, "draft", "", gmail.Build(largeComposition(7<<20)))
			requireError(t, err, gmail.ErrProvider)
			if tc.canceled && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if d != (gmail.Draft{}) || calls != 2 {
				t.Fatal(d, calls)
			}
			for _, b := range bodies {
				if !b.closed {
					t.Fatal("unclosed response body")
				}
			}
		})
	}
}

type cancelingFailedReader struct{ cancel context.CancelFunc }

func (r cancelingFailedReader) Read([]byte) (int, error) {
	r.cancel()
	return 0, errors.New("synthetic canceled body read failure")
}

func (b *observedBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *observedBody) Close() error               { b.closed = true; return nil }

func TestTransportFailuresAndBodyClosure(t *testing.T) {
	// R-2FJ8-HGLG R-5TRX-70ZD R-GN0X-1AMA
	for _, in := range invocations() {
		for _, kind := range []string{"no-answer", "read-failure", "cancel-before", "cancel-body"} {
			ctx, cancel := context.WithCancel(context.Background())
			if kind == "cancel-before" {
				cancel()
			}
			var bodies []*observedBody
			attempt := 0
			c := gmail.NewClient(gmail.Config{HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				attempt++
				if !strings.HasPrefix(r.URL.String(), gmail.DefaultBaseURL+"/") {
					t.Error(r.URL)
				}
				if attempt <= len(in.pre) {
					b := &observedBody{reader: strings.NewReader(in.pre[attempt-1].body)}
					bodies = append(bodies, b)
					return &http.Response{StatusCode: 200, Body: b, Header: make(http.Header)}, nil
				}
				if kind == "no-answer" || kind == "cancel-before" {
					return nil, errors.New("synthetic transport failure")
				}
				if kind == "cancel-body" {
					cancel()
				}
				b := &observedBody{reader: failedReader{errors.New("synthetic read failure")}}
				bodies = append(bodies, b)
				return &http.Response{StatusCode: 200, Body: b, Header: make(http.Header)}, nil
			})}})
			v, err := in.call(c, ctx)
			requireError(t, err, gmail.ErrProvider)
			if strings.HasPrefix(kind, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal(in.name, kind, err)
			}
			if !reflect.DeepEqual(v, in.zero) {
				t.Fatal(in.name, v)
			}
			for _, b := range bodies {
				if !b.closed {
					t.Fatal(in.name, kind, "unclosed response")
				}
			}
			cancel()
		}
	}
}
