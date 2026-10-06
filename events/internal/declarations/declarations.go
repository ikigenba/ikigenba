// Package declarations discovers the services' event contracts.
package declarations

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

// AskTimeout bounds each declaration request independently of deliveries.
const AskTimeout = 2 * time.Second

// Config supplies discovery's store, transport recording, and scheduling seams.
type Config struct {
	Store     *store.Store
	Services  string
	Telemetry *telemetry.Writer
	AskAfter  func(d time.Duration) <-chan time.Time
	Joined    func(service string)
}

// Declarations discovers contracts and coalesces on-demand requests.
type Declarations struct {
	cfg      Config
	mu       sync.Mutex
	started  bool
	inflight map[string]chan struct{}
}

// New constructs a discovery manager.
func New(cfg Config) *Declarations {
	if cfg.AskAfter == nil {
		cfg.AskAfter = time.After
	}
	return &Declarations{cfg: cfg, inflight: make(map[string]chan struct{})}
}

func (d *Declarations) list() (map[string]string, bool) {
	list, err := services.Read(d.cfg.Services)
	if err != nil {
		return nil, false
	}
	askable := make(map[string]string)
	for _, entry := range list {
		if entry.Enabled && entry.Name != events.ServiceName {
			if _, exists := askable[entry.Name]; !exists {
				askable[entry.Name] = entry.Socket
			}
		}
	}
	return askable, true
}

// Refresh asks all enabled siblings concurrently and forgets removed services.
func (d *Declarations) Refresh(ctx context.Context) {
	d.mu.Lock()
	start := !d.started
	d.started = true
	d.mu.Unlock()
	list, valid := d.list()
	if !valid && !start {
		return
	}
	writeCtx := context.WithoutCancel(ctx)
	held, _ := d.cfg.Store.Declarations(writeCtx)
	for service := range held {
		if _, exists := list[service]; !exists {
			_ = d.cfg.Store.Forget(writeCtx, service)
		}
	}
	var group sync.WaitGroup
	for service, socket := range list {
		group.Go(func() { d.ask(ctx, service, socket) })
	}
	group.Wait()
}

// Ask refreshes one sibling, sharing an in-flight on-demand ask.
func (d *Declarations) Ask(ctx context.Context, service string) {
	list, _ := d.list()
	socket, ok := list[service]
	if ctx.Err() != nil || !ok {
		return
	}
	d.mu.Lock()
	done, joined := d.inflight[service]
	if !joined {
		done = make(chan struct{})
		d.inflight[service] = done
		go func() {
			d.ask(context.WithoutCancel(ctx), service, socket)
			d.mu.Lock()
			delete(d.inflight, service)
			close(done)
			d.mu.Unlock()
		}()
	}
	d.mu.Unlock()
	if joined && d.cfg.Joined != nil {
		d.cfg.Joined(service)
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (d *Declarations) ask(parent context.Context, service, socket string) {
	deadline := d.cfg.AskAfter(AskTimeout)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-deadline:
			cancel()
		case <-finished:
		}
	}()
	transport := telemetry.SocketTransport(socket)
	defer transport.CloseIdleConnections()
	client := telemetry.SiblingClient(d.cfg.Telemetry, service, bodyTransport{transport})
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sibling"+events.DeclarationsPath, nil)
	if err != nil {
		return
	}
	response, err := client.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK || ctx.Err() != nil {
		return
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return
	}
	declaration, ok := parse(body)
	if ok && ctx.Err() == nil {
		_ = d.cfg.Store.Declare(context.WithoutCancel(parent), service, declaration)
	}
}

// Read the bounded response before SiblingClient records its outcome, including
// a deadline while a sibling has sent headers but stalled its body.
type bodyTransport struct{ base http.RoundTripper }

func (t bodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(events.MaxEventBytes)+1))
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

var eventName = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
var attrName = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func parse(body []byte) (store.Declaration, bool) {
	var result store.Declaration
	if len(body) > events.MaxEventBytes {
		return result, false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return result, false
	}
	var emissions []json.RawMessage
	if !array(object["emits"], &emissions) || !array(object["accepts"], &result.Accepts) {
		return result, false
	}
	result.Emits = make([]events.Emission, 0, len(emissions))
	for _, raw := range emissions {
		var fields map[string]json.RawMessage
		var emission events.Emission
		if json.Unmarshal(raw, &fields) != nil || fields == nil || json.Unmarshal(fields["event"], &emission.Event) != nil || !eventName.MatchString(emission.Event) || !array(fields["attrs"], &emission.Attrs) {
			return store.Declaration{}, false
		}
		for _, attr := range emission.Attrs {
			if !attrName.MatchString(attr) {
				return store.Declaration{}, false
			}
		}
		result.Emits = append(result.Emits, emission)
	}
	for _, name := range result.Accepts {
		if name != "*" && !eventName.MatchString(name) {
			return store.Declaration{}, false
		}
	}
	return result, true
}

func array(raw json.RawMessage, dest any) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '[' && json.Unmarshal(trimmed, dest) == nil
}
