package awssdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

type listingReply struct {
	body   string
	status int
}
type listingTransport struct {
	t        *testing.T
	replies  []listingReply
	requests int
}

func (l *listingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	l.t.Helper()
	page := l.requests
	l.requests++
	if page >= len(l.replies) {
		l.t.Fatalf("unexpected request %d", l.requests)
	}
	query := request.URL.Query()
	wantToken := ""
	if page > 0 {
		wantToken = fmt.Sprintf("page-%d", page+1)
	}
	if request.Method != http.MethodGet || request.URL.Path != "/ikigenba.dev" || query.Get("prefix") != "sbx1/host/" || query.Get("max-keys") != fmt.Sprint(cloud.ObjectPageSize) || query.Get("continuation-token") != wantToken {
		l.t.Fatalf("request %d = %s %s; want listing prefix, page size and token %q", l.requests, request.Method, request.URL, wantToken)
	}
	reply := l.replies[page]
	return &http.Response{StatusCode: reply.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(reply.body)), Request: request}, nil
}

func listingPage(objects []cloud.Object, truncated bool, token string) listingReply {
	var body strings.Builder
	fmt.Fprintf(&body, `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><IsTruncated>%t</IsTruncated>`, truncated)
	if token != "" {
		fmt.Fprintf(&body, "<NextContinuationToken>%s</NextContinuationToken>", token)
	}
	for _, object := range objects {
		fmt.Fprintf(&body, "<Contents><Key>%s</Key><Size>%d</Size><LastModified>%s</LastModified></Contents>", object.Key, object.Size, object.Modified.Format(time.RFC3339))
	}
	body.WriteString("</ListBucketResult>")
	return listingReply{body: body.String(), status: http.StatusOK}
}

func listingClient(t *testing.T, transport *listingTransport) cloud.S3 {
	t.Helper()
	clients, err := OpenWithLoader(t.Context(), "profile", "us-test-1", func(context.Context, string, string) (aws.Config, error) {
		return aws.Config{Region: "us-test-1", RetryMaxAttempts: 1, Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "fake", SecretAccessKey: "fake"}, nil
		}), HTTPClient: &http.Client{Transport: transport}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return clients.S3
}

func TestListObjectsPagesAndCallbackOrdering(t *testing.T) {
	// R-7ILW-NBW8 R-7JTT-13MX
	modified := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]cloud.Object{
		{{Key: "sbx1/host/z", Size: 3, Modified: modified}, {Key: "sbx1/host/a", Size: 2, Modified: modified.Add(time.Hour)}},
		{{Key: "sbx1/host/y", Size: 6, Modified: modified.Add(2 * time.Hour)}, {Key: "sbx1/host/b", Size: 4, Modified: modified.Add(3 * time.Hour)}},
		{{Key: "sbx1/host/c", Size: 5, Modified: modified.Add(4 * time.Hour)}},
	}
	replies := []listingReply{listingPage(pages[0], true, "page-2"), listingPage(pages[1], true, "page-3"), listingPage(pages[2], false, "unused")}
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprintf("stop_%t", stop), func(t *testing.T) {
			transport := &listingTransport{t: t, replies: replies}
			calls := 0
			err := listingClient(t, transport).ListObjects(t.Context(), "ikigenba.dev", "sbx1/host/", func(page []cloud.Object) bool {
				calls++
				if transport.requests != calls {
					t.Fatalf("callback %d after %d requests", calls, transport.requests)
				}
				if !reflect.DeepEqual(page, pages[calls-1]) {
					t.Fatalf("callback %d = %#v; want %#v", calls, page, pages[calls-1])
				}
				return !stop
			})
			want := len(pages)
			if stop {
				want = 1
			}
			if err != nil || calls != want || transport.requests != want {
				t.Fatalf("requests %d, callbacks %d, error %v; want %d, %d, nil", transport.requests, calls, err, want, want)
			}
		})
	}
}

func TestListObjectsEmptyPages(t *testing.T) {
	// R-7ILW-NBW8
	for _, replies := range [][]listingReply{{listingPage(nil, false, "")}, {listingPage(nil, true, "page-2"), listingPage(nil, false, "")}} {
		transport := &listingTransport{t: t, replies: replies}
		err := listingClient(t, transport).ListObjects(t.Context(), "ikigenba.dev", "sbx1/host/", func([]cloud.Object) bool { t.Fatal("callback for empty page"); return false })
		if err != nil || transport.requests != len(replies) {
			t.Fatalf("requests %d, error %v; want %d, nil", transport.requests, err, len(replies))
		}
	}
}

func TestListObjectsPageFailures(t *testing.T) {
	// R-7L1P-EVDM R-GSOH-NDVV
	first := []cloud.Object{{Key: "sbx1/host/one", Size: 1, Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, {Key: "sbx1/host/two", Size: 2, Modified: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}}
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing_token_%t", malformed), func(t *testing.T) {
			failure := listingReply{status: http.StatusServiceUnavailable, body: `<Error><Code>SlowDown</Code><Message>retry later</Message></Error>`}
			if malformed {
				failure = listingPage(first[:1], true, "")
			}
			transport := &listingTransport{t: t, replies: []listingReply{listingPage(first, true, "page-2"), failure}}
			calls := 0
			err := listingClient(t, transport).ListObjects(t.Context(), "ikigenba.dev", "sbx1/host/", func(page []cloud.Object) bool {
				calls++
				if !reflect.DeepEqual(page, first) {
					t.Fatalf("page = %#v; want %#v", page, first)
				}
				return true
			})
			var got *cloud.Error
			if !errors.As(err, &got) || got.Service != "s3" || got.Operation != "ListObjectsV2" || got.Subject != "ikigenba.dev/sbx1/host/" || got.Err == nil {
				t.Fatalf("error = %#v; want listing cloud.Error", err)
			}
			if !malformed && got.Error() != "s3 ListObjectsV2 ikigenba.dev/sbx1/host/: SlowDown" {
				t.Fatalf("error = %v", err)
			}
			if transport.requests != 2 || calls != 1 {
				t.Fatalf("requests %d, callbacks %d; want 2, 1", transport.requests, calls)
			}
		})
	}
}
