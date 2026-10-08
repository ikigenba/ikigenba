package cli_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/cron/internal/tools"
)

type credentialTransport struct{ header string }

func (c credentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header = req.Header.Clone()
	if c.header != "" {
		req.Header.Set("Authorization", c.header)
	}
	return ecNginxTransport{}.RoundTrip(req)
}

type credentialRecord struct {
	trail         []telemetry.Event
	bus           []events.Event
	stderr        string
	files         map[string][]byte
	entries       map[string]bool
	before        map[string][]byte
	beforeEntries map[string]bool
}

// Eight-character substrings suffice: every longer substring contains one.
func credentialGrams(t *testing.T, values ...string) []string {
	t.Helper()
	var result []string
	for _, v := range values {
		if len(v) < 16 {
			t.Fatal("short marker")
		}
		for i := 0; i+8 <= len(v); i++ {
			gram := strings.ToLower(v[i : i+8])
			has := false
			for _, c := range gram {
				if c >= 'g' && c <= 'z' {
					has = true
				}
			}
			if !has {
				t.Fatalf("marker has ambiguous gram %q", gram)
			}
			result = append(result, gram)
		}
	}
	return result
}
func credentialHeld(s string, grams []string) string {
	s = strings.ToLower(s)
	for _, gram := range grams {
		if strings.Contains(s, gram) {
			return gram
		}
	}
	return ""
}
func credentialSnapshot(t *testing.T, root string) (map[string][]byte, map[string]bool) {
	t.Helper()
	files := map[string][]byte{}
	entries := map[string]bool{}
	handle, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = fs.WalkDir(handle.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries[path] = true
		if entry.Type().IsRegular() {
			b, err := handle.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = b
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files, entries
}
func credentialAttrsText(attrs map[string]any) string {
	var values []string
	for _, value := range attrs {
		switch v := value.(type) {
		case string:
			values = append(values, v)
		case float64:
			values = append(values, strconv.FormatFloat(v, 'f', -1, 64))
		case float32:
			values = append(values, strconv.FormatFloat(float64(v), 'f', -1, 32))
		default:
			values = append(values, fmt.Sprint(v))
		}
	}
	return strings.Join(values, " ")
}
func credentialEventText(e telemetry.Event) string {
	return strings.Join([]string{e.Name, e.RequestID, e.User, credentialAttrsText(e.Attrs)}, " ")
}
func credentialBusText(e events.Event) string {
	return strings.Join([]string{e.Name, e.ID, e.RequestID, e.User, e.Cause, credentialAttrsText(e.Attrs)}, " ")
}
func credentialRun(t *testing.T, authorized, fail bool, notifyPath string) credentialRecord {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "work")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sentinel"), []byte("unchanged fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	before, beforeEntries := credentialSnapshot(t, root)
	fired := make(chan struct{}, 4)
	var trail telemetry.Capture
	var bus events.Capture
	trailSink := ecTrailSink(func(ctx context.Context, e telemetry.Event) error {
		_ = trail.Deliver(ctx, e)
		if strings.HasSuffix(e.Name, ".fired") {
			fired <- struct{}{}
		}
		if fail {
			return telemetry.ErrRejected
		}
		return nil
	})
	busSink := ecBusSink(func(ctx context.Context, e events.Event) error {
		_ = bus.Deliver(ctx, e)
		if fail {
			return events.ErrRejected
		}
		return nil
	})
	r := ecStart(t, dir, nil, trailSink, busSink, func(context.Context, time.Duration) {}, notifyPath)
	email := "wwwwwwww@wwwwwwww"
	if authorized {
		email = "yyyyyyyy@yyyyyyyy"
	}
	r.caller.Email = email
	user := "zzzzzzzzzzzzzzzz"
	password := "qqqqqqqqqqqqqqqq"
	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	for pass := 0; pass < 2; pass++ {
		header := ""
		if authorized {
			header = "Bearer " + password
			if pass == 1 {
				header = "Basic " + encoded
			}
		}
		r.client = mcp.NewClient(mcp.ClientConfig{Endpoint: r.url + "/mcp", HTTPClient: &http.Client{Transport: credentialTransport{header: header}, Timeout: 5 * time.Second}})
		for _, request := range []struct {
			method, path string
			identified   bool
		}{
			{"GET", "/", false}, {"HEAD", "/", false}, {"POST", "/mcp", false}, {"POST", "/declarations", false},
			{"GET", "/", true}, {"GET", "/about", true}, {"GET", "/_appkit/theme.css", true}, {"GET", "/no-page", true}, {"GET", "/declarations", true}, {"POST", "/events", true},
		} {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			req, err := http.NewRequestWithContext(ctx, request.method, r.url+request.path, nil)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			req.Host = "cron.example.test"
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-User-Email", email)
			if request.identified {
				req.Header.Set("X-User-Id", r.caller.UserID)
			}
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			_, err = io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			cancel()
			if err != nil || closeErr != nil {
				t.Fatalf("body %v %v", err, closeErr)
			}
			if !request.identified && request.path != "/declarations" && response.StatusCode != http.StatusInternalServerError {
				t.Fatalf("missing identity %s %s: %d", request.method, request.path, response.StatusCode)
			}
		}
		slug := fmt.Sprintf("credential_pass%d", pass)
		r.ok("create", tools.CreateArgs{Slug: slug, When: "* * * * *"})
		r.ok("list", tools.ListArgs{})
		shown := r.ok("show", tools.ShowArgs{Slug: slug})
		object := credentialTrigger(t, shown)
		if object.Next == nil {
			t.Fatal("created trigger has no next")
		}
		slot, err := time.Parse(time.RFC3339, *object.Next)
		if err != nil {
			t.Fatal(err)
		}

		r.clock.wake(slot)
		ecAwait(t, fired)
		object = credentialTrigger(t, r.ok("show", tools.ShowArgs{Slug: slug}))
		r.clock.mu.Lock()
		r.clock.firing = false
		r.clock.mu.Unlock()
		if object.LastFired == nil || *object.LastFired != slot.Format(time.RFC3339) {
			t.Fatal("controlled fire did not finish")
		}

		r.ok("update", tools.UpdateArgs{Slug: slug, When: "@hourly"})
		r.ok("pause", tools.PauseArgs{Slug: slug})
		r.ok("resume", tools.ResumeArgs{Slug: slug})
		if !r.call("show", tools.ShowArgs{Slug: "absent_trigger"}).IsError() {
			t.Fatal("missing show succeeded")
		}
		r.ok("delete", tools.DeleteArgs{Slug: slug})
	}
	r.stop()
	files, entries := credentialSnapshot(t, root)
	return credentialRecord{trail: trail.Events(), bus: bus.Events(), stderr: r.stderr.text(), files: files, entries: entries, before: before, beforeEntries: beforeEntries}
}
func credentialTrigger(t *testing.T, result mcp.Result) tools.Trigger {
	t.Helper()
	raw, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		StructuredContent tools.Trigger `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record.StructuredContent
}

// R-HHUN-PFL8 R-HJ2K-37BX R-Q84O-CC6Z R-Q9CK-Q3XO R-QBSD-HNF2 R-HKAG-GZ2M
func TestRunCredentialConfidentiality(t *testing.T) {
	user := "zzzzzzzzzzzzzzzz"
	password := "qqqqqqqqqqqqqqqq"
	email := "yyyyyyyy@yyyyyyyy"
	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	secret := credentialGrams(t, user, password, encoded)
	all := append(append([]string{}, secret...), credentialGrams(t, email)...)
	for _, failing := range []bool{false, true} {
		t.Run(fmt.Sprint(failing), func(t *testing.T) {
			short, err := os.MkdirTemp("", "credential-ready-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(short); err != nil {
					t.Error(err)
				}
			})
			notifyPath := filepath.Join(short, "n")
			reference := credentialRun(t, false, failing, notifyPath)
			for _, e := range reference.trail {
				if gram := credentialHeld(credentialEventText(e), all); gram != "" {
					t.Fatalf("inadmissible reference trail %q", gram)
				}
			}
			for _, e := range reference.bus {
				if gram := credentialHeld(credentialBusText(e), all); gram != "" {
					t.Fatalf("inadmissible reference bus %q", gram)
				}
			}
			if gram := credentialHeld(reference.stderr, all); gram != "" {
				t.Fatalf("inadmissible reference stderr %q", gram)
			}
			for path, b := range reference.files {
				if gram := credentialHeld(string(b), all); gram != "" {
					t.Fatalf("inadmissible reference file %s %q", path, gram)
				}
			}
			for path := range reference.entries {
				if gram := credentialHeld(path, all); gram != "" {
					t.Fatalf("inadmissible reference path %s %q", path, gram)
				}
			}
			result := credentialRun(t, true, failing, notifyPath)
			for _, e := range result.trail {
				if gram := credentialHeld(credentialEventText(e), all); gram != "" {
					t.Fatalf("trail leaked %q: %+v", gram, e)
				}
			}
			for _, e := range result.bus {
				if gram := credentialHeld(credentialBusText(e), all); gram != "" {
					t.Fatalf("bus leaked %q: %+v", gram, e)
				}
			}
			if gram := credentialHeld(result.stderr, all); gram != "" {
				t.Fatalf("stderr leaked %q", gram)
			}
			if failing && (!strings.Contains(result.stderr, "cron: lost event:") || !strings.Contains(result.stderr, "cron: undelivered event:")) {
				t.Fatal("failed sinks did not exercise diagnostic records", result.stderr)
			}
			for path, b := range result.files {
				if string(b) == string(result.before[path]) {
					continue
				}
				if gram := credentialHeld(string(b), secret); gram != "" {
					t.Fatalf("file %s leaked %q", path, gram)
				}
			}
			for path := range result.entries {
				if !result.beforeEntries[path] {
					if gram := credentialHeld(path, secret); gram != "" {
						t.Fatalf("path %s leaked %q", path, gram)
					}
				}
			}
			var fired int
			for _, e := range result.bus {
				if strings.HasSuffix(e.Name, ".fired") {
					fired++
				}
			}
			if fired != 2 {
				t.Fatalf("credential sequence fired %d events", fired)
			}
		})
	}
}
