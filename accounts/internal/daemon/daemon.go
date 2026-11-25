// Package daemon reconciles one host with the state served by accounts.
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Config provides the accounts endpoint and the dependencies used to reconcile a host.
type Config struct {
	BaseURL   string
	HTTP      *http.Client
	Presigner cloud.Presigner
	Metadata  cloud.Metadata
	Machine   machine.Machine
	Digest    string
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration)
	Stderr    io.Writer
	Box       net.Listener
	PeerUID   func(net.Conn) (int64, error)
	After     func(time.Duration) <-chan time.Time
}

// Request timing and the daemon's HTTP user agent.
const (
	MinBackoff      = 5 * time.Second
	MaxBackoff      = time.Minute
	RequestTimeout  = protocol.LongPoll + 15*time.Second
	DownloadTimeout = 5 * time.Minute
	UserAgent       = "accountsd"
)

// Diagnostic text written to the service journal.
const (
	DiagArguments = "unexpected command-line arguments"
	DiagURL       = "ACCOUNTS_URL must be an absolute HTTPS URL without credentials, query or fragment"
	DiagSetup     = "could not start daemon"
	DiagFetch     = "state fetch failed"
	DiagUpdate    = "daemon update failed"
	DiagApply     = "state apply failed"
	DiagReport    = "report delivery failed"
	invalidBase   = "expected an absolute HTTP or HTTPS URL without credentials, query or fragment"
	statusError   = "unexpected HTTP status %d"
)

type reconciler struct {
	cfg        Config
	binding    string
	signing    chan struct{}
	lastSigned time.Time
	haveSigned bool
	latest     atomic.Pointer[protocol.DesiredState]
	diagnostic sync.Mutex
	attempted  int64
	applied    int64
}

// Run pulls and applies desired states until cancellation or a successful self-update.
func Run(ctx context.Context, cfg Config) (result error) {
	u, err := parseBaseURL(cfg.BaseURL)
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	} else if cfg.Box != nil && (cfg.PeerUID == nil || cfg.After == nil) {
		err = errors.New("box listener requires peer credentials and a timer")
	}
	if err != nil {
		if cfg.Box != nil {
			_ = cfg.Box.Close()
		}
		return err
	}

	r := reconciler{cfg: cfg, binding: strings.ToLower(u.Host), signing: make(chan struct{}, 1)}
	if cfg.Box != nil {
		acceptCtx, stopAccept := context.WithCancel(ctx)
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			r.serveBox(acceptCtx, ctx)
		}()
		defer func() {
			stopAccept()
			_ = cfg.Box.Close()
			<-drained
			if ctx.Err() != nil {
				result = ctx.Err()
			}
		}()
	}
	fetchBackoff, updateBackoff := MinBackoff, MinBackoff
	for ctx.Err() == nil {
		state, err := r.fetch(ctx)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			r.diagnose(DiagFetch, err)
			cfg.Sleep(ctx, fetchBackoff)
			fetchBackoff = advanceBackoff(fetchBackoff)
			continue
		}
		r.attempted = state.Version
		r.latest.Store(&state)
		fetchBackoff = MinBackoff

		var updateErr error
		if state.Daemon != "" && state.Daemon != cfg.Digest {
			updateErr = r.update(ctx, state.Daemon)
			if ctx.Err() != nil {
				break
			}
			if updateErr == nil {
				return nil
			}
			r.diagnose(DiagUpdate, updateErr)
		}
		if err := r.applyAndReport(ctx, state, updateErr); err != nil {
			return err
		}
		if updateErr != nil {
			cfg.Sleep(ctx, updateBackoff)
			updateBackoff = advanceBackoff(updateBackoff)
		} else {
			updateBackoff = MinBackoff
		}
	}
	return ctx.Err()
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.ForceQuery || u.RawQuery != "" || strings.Contains(raw, "#") {
		return nil, errors.New(invalidBase)
	}
	return u, nil
}

func advanceBackoff(pause time.Duration) time.Duration {
	return min(pause*2, MaxBackoff)
}

func (r *reconciler) waitToSign(ctx context.Context) {
	now := r.cfg.Now().UTC()
	for r.haveSigned && !now.Truncate(time.Second).After(r.lastSigned) && ctx.Err() == nil {
		r.cfg.Sleep(ctx, r.lastSigned.Add(time.Second).Sub(now))
		now = r.cfg.Now().UTC()
	}
}

func (r *reconciler) identityHeader(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r.signing <- struct{}{}:
	}
	defer func() { <-r.signing }()
	r.waitToSign(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	header, err := hostauth.Header(ctx, r.cfg.Presigner, r.binding)
	// Credential refresh can cross a second boundary before the presigner
	// assigns X-Amz-Date. Wait from the second in which signing completed.
	r.lastSigned = r.cfg.Now().UTC().Truncate(time.Second)
	r.haveSigned = true
	return header, err
}

func (r *reconciler) request(ctx context.Context, client *http.Client, method, path string, body io.Reader, identify bool) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, r.cfg.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", protocol.ContentJSON)
	}
	if identify {
		header, err := r.identityHeader(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set(hostauth.HeaderName, header)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (r *reconciler) fetch(ctx context.Context) (protocol.DesiredState, error) {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	query := url.Values{
		protocol.QueryAfter:  {strconv.FormatInt(r.attempted, 10)},
		protocol.QueryDigest: {r.cfg.Digest},
	}
	resp, err := r.request(ctx, r.cfg.HTTP, http.MethodGet, protocol.StatePath+"?"+query.Encode(), nil, true)
	if err != nil {
		return protocol.DesiredState{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return protocol.DesiredState{}, err
	}
	return protocol.DecodeState(resp.Body)
}

func (r *reconciler) update(ctx context.Context, digest string) error {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	resp, err := r.request(ctx, r.cfg.HTTP, http.MethodGet, protocol.BinaryPath, nil, false)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := checkStatus(resp, http.StatusOK); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.cfg.Machine.InstallDaemon(ctx, resp.Body, digest)
}

func (r *reconciler) applyAndReport(ctx context.Context, state protocol.DesiredState, updateErr error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	applyErr := r.cfg.Machine.Apply(ctx, state)
	if err := ctx.Err(); err != nil {
		return err
	}
	if applyErr != nil {
		r.diagnose(DiagApply, applyErr)
	} else {
		r.applied = state.Version
	}
	facts, inspectErr := r.cfg.Machine.Inspect(ctx, state)
	if err := ctx.Err(); err != nil {
		return err
	}
	if inspectErr != nil {
		facts = protocol.Report{}
	}
	instance, metadataErr := r.cfg.Metadata.Instance(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if metadataErr != nil {
		instance = cloud.InstanceFacts{}
	}
	facts.Protocol = protocol.Version
	facts.Attempted, facts.Applied = state.Version, r.applied
	facts.Error = ""
	if applyErr != nil {
		facts.Error = applyErr.Error()
	} else if updateErr != nil {
		facts.Error = updateErr.Error()
	}
	facts.InstanceID, facts.Type, facts.AMI = instance.InstanceID, instance.Type, instance.AMI
	facts.DaemonVersion = r.cfg.Digest

	err := r.report(ctx, facts)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		r.diagnose(DiagReport, err)
	}
	return nil
}

func (r *reconciler) report(ctx context.Context, facts protocol.Report) error {
	ctx, cancel := context.WithTimeout(ctx, RequestTimeout)
	defer cancel()
	body, err := json.Marshal(facts.Bounded())
	if err != nil {
		return err
	}
	resp, err := r.request(ctx, r.cfg.HTTP, http.MethodPost, protocol.ReportPath, bytes.NewReader(body), true)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return checkStatus(resp, http.StatusNoContent)
}

func checkStatus(resp *http.Response, want int) error {
	if resp.StatusCode != want {
		return fmt.Errorf(statusError, resp.StatusCode)
	}
	return nil
}

func (r *reconciler) diagnose(message string, err error) {
	r.diagnostic.Lock()
	defer r.diagnostic.Unlock()
	diagnose(r.cfg.Stderr, message, err)
}

func diagnose(w io.Writer, message string, err error) {
	if err != nil {
		detail := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
		_, _ = fmt.Fprintf(w, "%s: %s: %s\n", UserAgent, message, detail)
		return
	}
	_, _ = fmt.Fprintf(w, "%s: %s\n", UserAgent, message)
}
