package gmail

// client_test.go drives the Gmail REST client fully offline via an injected fake
// http.RoundTripper. It exercises: token refresh + cache, 401 -> force-refresh
// -> retry-once, invalid_grant -> loud failure (no spin), and a representative
// method per side (GetProfile, HistoryList, MessagesSend). No network.

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAttachmentGet(t *testing.T) {
	// R-WVZH-M0IY
	payload := []byte("attachment bytes\x00\xff")
	for _, encoded := range []string{
		base64.URLEncoding.EncodeToString(payload),
		base64.RawURLEncoding.EncodeToString(payload),
	} {
		t.Run(encoded, func(t *testing.T) {
			c := newTestClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == hostOAuth {
					return resp(http.StatusOK, okToken), nil
				}
				if r.Method != http.MethodGet || r.URL.EscapedPath() != "/gmail/v1/users/me/messages/message%2Fid/attachments/attachment%2Fid" {
					t.Errorf("method/path = %s %q", r.Method, r.URL.EscapedPath())
				}
				if got := r.Header.Get("Authorization"); got != "Bearer AT-1" {
					t.Errorf("Authorization = %q", got)
				}
				return resp(http.StatusOK, `{"size":18,"data":"`+encoded+`"}`), nil
			})
			got, err := c.AttachmentGet(context.Background(), "message/id", "attachment/id")
			if err != nil || string(got) != string(payload) {
				t.Fatalf("AttachmentGet = %q, %v; want %q, nil", got, err, payload)
			}
		})
	}

	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			return resp(http.StatusOK, okToken), nil
		}
		return resp(http.StatusNotFound, `{"error":{"code":404,"message":"missing"}}`), nil
	})
	if _, err := c.AttachmentGet(context.Background(), "m", "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404 error = %v, want ErrNotFound", err)
	}

	for _, ids := range [][2]string{{"", "a"}, {"m", ""}} {
		c := newTestClient(func(*http.Request) (*http.Response, error) {
			t.Fatal("validation must not issue an HTTP request")
			return nil, nil
		})
		if _, err := c.AttachmentGet(context.Background(), ids[0], ids[1]); !errors.Is(err, ErrValidation) {
			t.Fatalf("AttachmentGet(%q, %q) error = %v, want ErrValidation", ids[0], ids[1], err)
		}
	}
}

// roundTripFunc adapts a func to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func resp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// newTestClient wires a Client over a fake RoundTripper.
func newTestClient(rt roundTripFunc) *Client {
	return NewClient(
		Config{ClientID: "cid", ClientSecret: "csec", RefreshToken: "rtok"},
		&http.Client{Transport: rt},
	)
}

const okToken = `{"access_token":"AT-1","expires_in":3600,"token_type":"Bearer"}`

// TestTokenRefreshAndCache: the first REST call triggers a refresh; a second
// call reuses the cached token without a second refresh.
func TestTokenRefreshAndCache(t *testing.T) {
	var refreshes, profiles int32
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			atomic.AddInt32(&refreshes, 1)
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Errorf("token refresh content-type = %q", got)
			}
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), "grant_type=refresh_token") {
				t.Errorf("token body missing grant_type: %s", b)
			}
			return resp(200, okToken), nil
		}
		// REST call: must carry the bearer.
		if got := r.Header.Get("Authorization"); got != "Bearer AT-1" {
			t.Errorf("missing/wrong bearer: %q", got)
		}
		atomic.AddInt32(&profiles, 1)
		return resp(200, `{"emailAddress":"a@b.com","historyId":"42"}`), nil
	})

	ctx := context.Background()
	if _, err := c.GetProfile(ctx); err != nil {
		t.Fatalf("GetProfile 1: %v", err)
	}
	p, err := c.GetProfile(ctx)
	if err != nil {
		t.Fatalf("GetProfile 2: %v", err)
	}
	if p.HistoryID != "42" || p.EmailAddress != "a@b.com" {
		t.Fatalf("unexpected profile: %+v", p)
	}
	if refreshes != 1 {
		t.Fatalf("expected exactly 1 refresh, got %d", refreshes)
	}
	if profiles != 2 {
		t.Fatalf("expected 2 profile calls, got %d", profiles)
	}
}

// Test401RefreshRetryOnce: a 401 on the REST call invalidates the token, forces
// one refresh, and retries exactly once (then succeeds).
func Test401RefreshRetryOnce(t *testing.T) {
	var refreshes, rest int32
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			n := atomic.AddInt32(&refreshes, 1)
			// hand out distinct tokens so we can prove the retry used the new one
			if n == 1 {
				return resp(200, `{"access_token":"AT-old","expires_in":3600}`), nil
			}
			return resp(200, `{"access_token":"AT-new","expires_in":3600}`), nil
		}
		n := atomic.AddInt32(&rest, 1)
		if n == 1 {
			if r.Header.Get("Authorization") != "Bearer AT-old" {
				t.Errorf("first REST call bearer = %q", r.Header.Get("Authorization"))
			}
			return resp(401, `{"error":{"code":401,"message":"invalid auth"}}`), nil
		}
		if r.Header.Get("Authorization") != "Bearer AT-new" {
			t.Errorf("retry bearer = %q, want refreshed token", r.Header.Get("Authorization"))
		}
		return resp(200, `{"emailAddress":"a@b.com","historyId":"99"}`), nil
	})

	p, err := c.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if p.HistoryID != "99" {
		t.Fatalf("historyId = %q", p.HistoryID)
	}
	if refreshes != 2 {
		t.Fatalf("expected 2 refreshes (initial + forced), got %d", refreshes)
	}
	if rest != 2 {
		t.Fatalf("expected 2 REST attempts (401 + retry), got %d", rest)
	}
}

// Test401RetriesOnlyOnce: a persistent 401 retries exactly once then returns a
// descriptive error — it does NOT loop.
func Test401RetriesOnlyOnce(t *testing.T) {
	var rest int32
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			return resp(200, okToken), nil
		}
		atomic.AddInt32(&rest, 1)
		return resp(401, `{"error":{"code":401,"message":"still bad"}}`), nil
	})
	_, err := c.GetProfile(context.Background())
	if err == nil {
		t.Fatal("expected error on persistent 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401: %v", err)
	}
	if rest != 2 {
		t.Fatalf("expected exactly 2 REST attempts, got %d", rest)
	}
}

// TestInvalidGrantFailsLoud: a dead refresh token (invalid_grant) surfaces as
// ErrInvalidGrant and does NOT spin (exactly one refresh attempt, no REST call).
func TestInvalidGrantFailsLoud(t *testing.T) {
	var refreshes, rest int32
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			atomic.AddInt32(&refreshes, 1)
			return resp(400, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`), nil
		}
		atomic.AddInt32(&rest, 1)
		return resp(200, `{}`), nil
	})
	_, err := c.GetProfile(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid_grant")
	}
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expected ErrInvalidGrant, got %v", err)
	}
	if refreshes != 1 {
		t.Fatalf("expected exactly 1 refresh attempt (no spin), got %d", refreshes)
	}
	if rest != 0 {
		t.Fatalf("expected 0 REST calls (refresh failed first), got %d", rest)
	}
}

// TestHistoryList: query params and decode (producer side).
func TestHistoryList(t *testing.T) {
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			return resp(200, okToken), nil
		}
		if !strings.HasSuffix(r.URL.Path, "/history") {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("startHistoryId") != "100" {
			t.Errorf("startHistoryId = %q", q.Get("startHistoryId"))
		}
		if q.Get("pageToken") != "PT" {
			t.Errorf("pageToken = %q", q.Get("pageToken"))
		}
		return resp(200, `{"history":[{"id":"101","messagesAdded":[{"message":{"id":"m1","threadId":"t1"}}]}],"historyId":"110","nextPageToken":"PT2"}`), nil
	})
	r, err := c.HistoryList(context.Background(), "100", "PT")
	if err != nil {
		t.Fatalf("HistoryList: %v", err)
	}
	if r.HistoryID != "110" || r.NextPageToken != "PT2" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(r.History) != 1 || len(r.History[0].MessagesAdded) != 1 ||
		r.History[0].MessagesAdded[0].Message.ID != "m1" {
		t.Fatalf("unexpected history: %+v", r.History)
	}
}

// TestHistoryListStaleCursor: a 404 maps to ErrNotFound (stale-cursor resync).
func TestHistoryListStaleCursor(t *testing.T) {
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			return resp(200, okToken), nil
		}
		return resp(404, `{"error":{"code":404,"message":"Requested entity was not found."}}`), nil
	})
	_, err := c.HistoryList(context.Background(), "1", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on 404, got %v", err)
	}
}

// TestMessagesSend: POST body shape + decode (MCP side).
func TestMessagesSend(t *testing.T) {
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			return resp(200, okToken), nil
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/messages/send") {
			t.Errorf("method/path = %s %q", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"raw":"UkFXMTIz"`) {
			t.Errorf("send body = %s", b)
		}
		return resp(200, `{"id":"sent1","threadId":"t1","labelIds":["SENT"]}`), nil
	})
	m, err := c.MessagesSend(context.Background(), "UkFXMTIz")
	if err != nil {
		t.Fatalf("MessagesSend: %v", err)
	}
	if m.ID != "sent1" || len(m.LabelIDs) != 1 || m.LabelIDs[0] != LabelSent {
		t.Fatalf("unexpected message: %+v", m)
	}
}

// TestMessagesSendValidation: empty raw is rejected before any HTTP call.
func TestMessagesSendValidation(t *testing.T) {
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		t.Fatal("no HTTP call expected on validation failure")
		return nil, nil
	})
	if _, err := c.MessagesSend(context.Background(), ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// TestMessageDelete: DELETE with no body, success on 204/200.
func TestMessageDelete(t *testing.T) {
	c := newTestClient(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == hostOAuth {
			return resp(200, okToken), nil
		}
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		return resp(204, ``), nil
	})
	if err := c.MessageDelete(context.Background(), "m9"); err != nil {
		t.Fatalf("MessageDelete: %v", err)
	}
}
