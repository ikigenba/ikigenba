package cli_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Marked values: long, and every eight-character run holds a letter past f,
// so no hexadecimal id or run of digits can match them.
const (
	markAuth      = "AuthKqzwmrtplvxnsjyuhgkv"
	markBody      = "BodyRstuvwxyzGhijklmnopqRstuvw"
	markUnkept    = "UnkeptLmnopqGhzyxwvutsrq"
	markWrong     = "whs_WrongTsrqpoNmlkjihGzyxw"
	markType      = "application/x-hjklmnpqrtvwxz"
	markEvent     = "pushwvutsrqpnmlkjhgzyx"
	markDelivery  = "GgghhhjjjkkkmmmnnnpppQqqsss"
	markSignature = "sha256=" + "yyyzzzxxxwwwvvvuuutttsssrrrqqqpppooonnnmmmlllkkkjjjiiihhhgggzzz"
)

type authTransport struct{ header string }

func (a authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header = req.Header.Clone()
	req.Header.Set("Authorization", a.header)
	return http.DefaultTransport.RoundTrip(req)
}

// grams lists every eight-character run, lowercased; with strict, a run that
// holds only hexadecimal characters is refused, otherwise it is skipped.
func grams(t *testing.T, strict bool, values ...string) []string {
	t.Helper()
	var result []string
	for _, v := range values {
		for i := 0; i+8 <= len(v); i++ {
			gram := strings.ToLower(v[i : i+8])
			if strings.Trim(gram, "0123456789abcdef") == "" {
				if strict {
					t.Fatalf("ambiguous marker gram %q", gram)
				}
				continue
			}
			result = append(result, gram)
		}
	}
	if len(result) == 0 {
		t.Fatal("no grams")
	}
	return result
}

func held(s string, gs []string) string {
	s = strings.ToLower(s)
	for _, g := range gs {
		if strings.Contains(s, g) {
			return g
		}
	}
	return ""
}

func attrsText(attrs map[string]any) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v ", k, attrs[k])
	}
	return b.String()
}

type confidentialRecord struct {
	trail   []telemetry.Event
	bus     []events.Event
	stderr  string
	results map[string][]string
	pages   []string
	secrets []string
	sigs    []string
}

func confidentialRun(t *testing.T, failing bool) confidentialRecord {
	t.Helper()
	h := newRunHarness(t, t.TempDir())
	h.setEnv("DRAIN_SECONDS", "1")
	// Every event is seen; with failing, it is then refused, so the writer
	// and the emitter report it on stderr.
	tc, ec := h.tc, h.ec
	received := make(chan string, 16)
	h.p.Sink = runTelemetrySink(func(c context.Context, e telemetry.Event) error {
		_ = tc.Deliver(c, e)
		if e.Name == "webhook.plain_hook.received" {
			id, _ := e.Attrs["delivery"].(string)
			select {
			case received <- id:
			default:
			}
		}
		if failing {
			return fmt.Errorf("trail offline")
		}
		return nil
	})
	h.p.EventSink = runEventSink(func(c context.Context, e events.Event) error {
		_ = ec.Deliver(c, e)
		if failing {
			return fmt.Errorf("bus offline")
		}
		return nil
	})
	auth := "Bearer " + markAuth
	httpClient := &http.Client{Timeout: 5 * time.Second, Transport: authTransport{auth}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: h.url + "/mcp", HTTPClient: httpClient})
	rec := confidentialRecord{results: map[string][]string{}}
	call := func(tool string, args any) []byte {
		t.Helper()
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		r, err := client.CallTool(context.Background(), identity.Caller{UserID: "owner", Email: "owner@example.com"}, tool, b)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := r.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		rec.results[tool] = append(rec.results[tool], string(raw))
		return raw
	}
	secretOf := func(raw []byte) string {
		var v struct {
			StructuredContent struct{ Secret string } `json:"structuredContent"`
		}
		if err := json.Unmarshal(raw, &v); err != nil || v.StructuredContent.Secret == "" {
			t.Fatalf("no secret in %s", raw)
		}
		return v.StructuredContent.Secret
	}
	send := func(path string, body []byte, headers map[string]string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), "POST", h.url+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	page := func(path string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), "GET", h.url+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-User-Id", "owner")
		req.Header.Set("X-User-Email", "owner@example.com")
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		rec.pages = append(rec.pages, string(b))
	}
	h.start()
	plain := secretOf(call("create", map[string]string{"slug": "plain_hook"}))
	signedSecret := secretOf(call("create", map[string]string{"slug": "signed_hook", "scheme": "github-hmac"}))
	rec.secrets = append(rec.secrets, plain, signedSecret)
	kept := map[string]string{"Content-Type": markType, "X-GitHub-Event": markEvent, "X-GitHub-Delivery": markDelivery, "X-Custom-Marker": markUnkept}
	withSecret := map[string]string{"X-Webhook-Secret": plain}
	for k, v := range kept {
		withSecret[k] = v
	}
	if code := send("/in/plain_hook", []byte(markBody), withSecret); code != http.StatusAccepted {
		t.Fatalf("bearer accept %d", code)
	}
	wrong := map[string]string{"X-Webhook-Secret": markWrong, "X-Custom-Marker": markUnkept}
	if code := send("/in/plain_hook", []byte(markBody), wrong); code != http.StatusNotFound {
		t.Fatalf("bearer refuse %d", code)
	}
	mac := hmac.New(sha256.New, []byte(signedSecret))
	mac.Write([]byte(markBody))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	rec.sigs = append(rec.sigs, sig)
	signed := map[string]string{"X-Hub-Signature-256": sig}
	for k, v := range kept {
		signed[k] = v
	}
	if code := send("/in/signed_hook", []byte(markBody), signed); code != http.StatusAccepted {
		t.Fatalf("signed accept %d", code)
	}
	signed["X-Hub-Signature-256"] = markSignature
	if code := send("/in/signed_hook", []byte(markBody), signed); code != http.StatusNotFound {
		t.Fatalf("signed refuse %d", code)
	}
	rotated := secretOf(call("rotate", map[string]string{"slug": "plain_hook"}))
	rec.secrets = append(rec.secrets, rotated)
	var deliveryID string
	select {
	case deliveryID = <-received:
	case <-time.After(5 * time.Second):
	}
	if deliveryID == "" {
		t.Fatalf("no delivery id; stderr %q", h.err.String())
	}
	if raw := call("delivery", map[string]string{"id": deliveryID}); !strings.Contains(string(raw), markBody) {
		t.Fatalf("delivery body %s", raw)
	}
	call("list", map[string]string{})
	call("show", map[string]string{"slug": "plain_hook"})
	call("show", map[string]string{"slug": "signed_hook"})
	for _, p := range []string{"/", "/tools", "/about", "/nope"} {
		page(p)
	}
	call("delete", map[string]string{"slug": "signed_hook"})
	if code := h.stop("SIGTERM"); code != 0 {
		t.Fatalf("stop %d", code)
	}
	rec.trail = h.tc.Events()
	rec.bus = h.ec.Events()
	rec.stderr = h.err.String()
	if !failing && (len(rec.trail) == 0 || len(rec.bus) == 0) {
		t.Fatal("nothing captured")
	}
	if failing && !strings.Contains(rec.stderr, "webhooks: ") {
		t.Fatalf("failing sinks wrote nothing %q", rec.stderr)
	}
	return rec
}

// R-0ZXT-P6KS R-12DM-GQ26
func TestRunConfidentiality(t *testing.T) {
	for _, failing := range []bool{false, true} {
		t.Run(fmt.Sprint("failing=", failing), func(t *testing.T) {
			rec := confidentialRun(t, failing)
			secret := grams(t, true, markAuth, markBody, markUnkept, markWrong, markSignature)
			secret = append(secret, grams(t, false, rec.secrets...)...)
			secret = append(secret, grams(t, false, rec.sigs...)...)
			keptDelivery := grams(t, true, markDelivery)
			keptValues := grams(t, true, markType, markEvent)
			for _, e := range rec.trail {
				text := strings.Join([]string{e.Service, e.Name, e.RequestID, e.User, attrsText(e.Attrs)}, " ")
				if g := held(text, secret); g != "" {
					t.Fatalf("trail %s holds %q: %s", e.Name, g, text)
				}
				if g := held(text, keptDelivery); g != "" {
					t.Fatalf("trail holds the delivery header %s", text)
				}
				if held(text, keptValues) != "" {
					rest := strings.Join([]string{e.Service, e.Name, e.RequestID, e.User}, " ")
					other := map[string]any{}
					for k, v := range e.Attrs {
						if k != "content_type" && k != "type" {
							other[k] = v
						}
					}
					if !strings.HasSuffix(e.Name, ".received") || held(rest+attrsText(other), keptValues) != "" {
						t.Fatalf("kept header outside a received event's attrs: %s", text)
					}
				}
			}
			for _, e := range rec.bus {
				text := strings.Join([]string{e.ID, e.Service, e.Name, e.RequestID, e.User, e.Cause, attrsText(e.Attrs)}, " ")
				if g := held(text, secret); g != "" {
					t.Fatalf("bus %s holds %q", e.Name, g)
				}
				if held(text, keptDelivery) != "" {
					t.Fatalf("bus holds the delivery header %s", text)
				}
				if held(text, keptValues) != "" && !strings.HasSuffix(e.Name, ".received") {
					t.Fatalf("kept header outside a received event: %s", text)
				}
			}
			// Positive controls: the checks below see the events they judge.
			seen := false
			for _, e := range rec.trail {
				if e.Name == "webhook.signed_hook.received" && e.Attrs["content_type"] == markType && e.Attrs["type"] == markEvent {
					seen = true
				}
			}
			if !seen || (failing && !strings.Contains(rec.stderr, markType)) {
				t.Fatalf("received events not observed; stderr %q", rec.stderr)
			}
			if g := held(rec.stderr, secret); g != "" {
				t.Fatalf("stderr holds %q", g)
			}
			if held(rec.stderr, keptDelivery) != "" {
				t.Fatal("stderr holds the delivery header")
			}
			// R-12DM-GQ26: secrets only in create and rotate results; the body only in delivery's.
			secrets := grams(t, false, rec.secrets...)
			body := grams(t, true, markBody)
			for tool, results := range rec.results {
				for _, r := range results {
					if tool != "create" && tool != "rotate" && held(r, secrets) != "" {
						t.Fatalf("%s result shows a secret: %s", tool, r)
					}
					if tool != "delivery" && held(r, body) != "" {
						t.Fatalf("%s result shows the body: %s", tool, r)
					}
				}
			}
			for _, p := range rec.pages {
				if held(p, secrets) != "" || held(p, body) != "" {
					t.Fatal("a page shows a secret or a body")
				}
			}
		})
	}
}
