// Package hostauth signs and verifies the daemon's instance identity header.
package hostauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

// Header names, freshness bounds and STS admission limits.
const (
	HeaderName     = "X-Accountsd-Identity"
	BindingHeader  = cloud.BindingHeader
	MaxHeaderBytes = 8192
	PresignExpires = 60 * time.Second
	MaxExpires     = 300
	Freshness      = 5 * time.Minute
	MaxSkew        = time.Minute
	ReplayCapacity = 10000
	VerifyRate     = 20
	VerifyBurst    = 40
	STSTimeout     = cloud.CallerTimeout
)

// Errors distinguish refusals from unavailable verification.
var (
	ErrMalformed   = errors.New("malformed daemon identity")
	ErrStale       = errors.New("stale daemon identity")
	ErrReplayed    = errors.New("replayed daemon identity")
	ErrRejected    = errors.New("daemon identity rejected")
	ErrUnavailable = errors.New("daemon identity verification unavailable")
	ErrBusy        = errors.New("daemon identity verifier busy")
)

// VerifierConfig supplies the deployment identity, STS client and clock.
type VerifierConfig struct {
	Region  string
	Role    string
	Binding func() (string, error)
	STS     cloud.STS
	Now     func() time.Time
}

// Verifier checks signed host requests and tracks replay and admission.
type Verifier struct {
	cfg VerifierConfig

	mu       sync.Mutex
	accepted map[string]time.Time
	pruned   time.Time
	inFlight map[string]*flight
	refilled time.Time
	credit   time.Duration

	accountMu    sync.Mutex
	account      string
	accountKnown bool
}

type flight struct {
	done     chan struct{}
	accepted bool
}

// Credit is measured in time so token refill needs no floating-point rounding.
const tokenPeriod = time.Second / VerifyRate

// NewVerifier starts with a full admission bucket and an empty replay set.
func NewVerifier(cfg VerifierConfig) *Verifier {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Verifier{
		cfg:      cfg,
		accepted: make(map[string]time.Time),
		inFlight: make(map[string]*flight),
		credit:   VerifyBurst * tokenPeriod,
	}
}

// Header encodes one presigned STS request for transport.
func Header(ctx context.Context, p cloud.Presigner, binding string) (string, error) {
	raw, err := p.PresignCallerIdentity(ctx, binding, PresignExpires)
	if err != nil {
		return "", fmt.Errorf("presign daemon identity: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

// Verify returns the authenticated EC2 instance ID.
func (v *Verifier) Verify(ctx context.Context, header string) (string, error) {
	raw, signature, date, err := v.parse(header)
	if err != nil {
		return "", err
	}
	now := v.cfg.Now()
	if date.Before(now.Add(-Freshness)) || date.After(now.Add(MaxSkew)) {
		return "", ErrStale
	}

	var binding string
	for {
		v.mu.Lock()
		v.prune(now)
		_, seen := v.accepted[signature]
		pending := v.inFlight[signature]
		switch {
		case seen:
			v.mu.Unlock()
			return "", ErrReplayed
		case pending != nil:
			v.mu.Unlock()
			select {
			case <-pending.done:
				if pending.accepted {
					return "", ErrReplayed
				}
			case <-ctx.Done():
				return "", fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
			}
			now = v.cfg.Now()
		case binding == "":
			v.mu.Unlock()
			binding, err = v.cfg.Binding()
			if err != nil {
				return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
			}
			if binding == "" {
				return "", ErrUnavailable
			}
			now = v.cfg.Now()
		default:
			pending, room := v.admit(signature, now)
			v.mu.Unlock()
			if room != nil {
				select {
				case <-room:
					now = v.cfg.Now()
					continue
				case <-ctx.Done():
					return "", fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
				}
			}
			if pending == nil {
				return "", ErrBusy
			}
			return v.verifyCaller(ctx, raw, binding, signature, date, pending)
		}
	}
}

func (v *Verifier) prune(now time.Time) {
	cutoff := now.Add(-Freshness)
	if !cutoff.After(v.pruned) {
		return
	}
	for signature, date := range v.accepted {
		if date.Before(cutoff) {
			delete(v.accepted, signature)
		}
	}
	v.pruned = cutoff
}

func (v *Verifier) admit(signature string, now time.Time) (*flight, <-chan struct{}) {
	if len(v.inFlight) >= VerifyBurst || len(v.accepted) >= ReplayCapacity {
		return nil, nil
	}

	const full = VerifyBurst * tokenPeriod
	if now.After(v.refilled) {
		v.credit += min(full-v.credit, now.Sub(v.refilled))
		v.refilled = now
	}
	if v.credit < tokenPeriod {
		return nil, nil
	}
	if len(v.accepted)+len(v.inFlight) >= ReplayCapacity {
		// Only accepted signatures fill the set. Let an existing call settle
		// before deciding whether another success could cross the ceiling.
		for _, pending := range v.inFlight {
			return nil, pending.done
		}
	}
	v.credit -= tokenPeriod
	pending := &flight{done: make(chan struct{})}
	v.inFlight[signature] = pending
	return pending, nil
}

func (v *Verifier) verifyCaller(ctx context.Context, raw, binding, signature string, date time.Time, pending *flight) (string, error) {
	caller, err := v.cfg.STS.CallerOf(ctx, raw, binding)
	v.mu.Lock()
	delete(v.inFlight, signature)
	pending.accepted = err == nil
	if pending.accepted && !date.Before(v.pruned) {
		v.accepted[signature] = date
	}
	close(pending.done)
	v.mu.Unlock()

	if err != nil {
		if cloud.ClassOf(err) == cloud.ClassTransient {
			return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return "", fmt.Errorf("%w: %w", ErrRejected, err)
	}
	account, err := v.ownAccount(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if caller.Account != account {
		return "", ErrRejected
	}
	prefix := "arn:aws:sts::" + account + ":assumed-role/" + v.cfg.Role + "/"
	session, ok := strings.CutPrefix(caller.Arn, prefix)
	roleID, userSession, validUser := strings.Cut(caller.UserID, ":")
	if !ok || !instancePattern.MatchString(session) || !validUser || roleID == "" || session != userSession {
		return "", ErrRejected
	}
	return session, nil
}

func (v *Verifier) ownAccount(ctx context.Context) (string, error) {
	v.accountMu.Lock()
	defer v.accountMu.Unlock()
	if !v.accountKnown {
		account, err := v.cfg.STS.Account(ctx)
		if err != nil {
			return "", err
		}
		v.account = account
		v.accountKnown = true
	}
	return v.account, nil
}

var (
	signaturePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	instancePattern  = regexp.MustCompile(`^i-[0-9a-f]{8,17}$`)
	expiresPattern   = regexp.MustCompile(`^[0-9]+$`)
)

func (v *Verifier) parse(header string) (raw, signature string, date time.Time, err error) {
	if header == "" || len(header) > MaxHeaderBytes {
		return "", "", time.Time{}, ErrMalformed
	}
	decoded, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil {
		return "", "", time.Time{}, ErrMalformed
	}
	raw = string(decoded)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || strings.Contains(raw, "#") ||
		u.Host != "sts."+v.cfg.Region+".amazonaws.com" || u.EscapedPath() != "/" {
		return "", "", time.Time{}, ErrMalformed
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", "", time.Time{}, ErrMalformed
	}
	for _, name := range []string{"Action", "Version", "X-Amz-Algorithm", "X-Amz-SignedHeaders", "X-Amz-Expires", "X-Amz-Date", "X-Amz-Signature"} {
		if len(query[name]) != 1 {
			return "", "", time.Time{}, ErrMalformed
		}
	}
	signed := strings.Split(query.Get("X-Amz-SignedHeaders"), ";")
	expires := query.Get("X-Amz-Expires")
	expiration, err := strconv.Atoi(expires)
	signature = query.Get("X-Amz-Signature")
	if query.Get("Action") != "GetCallerIdentity" || query.Get("Version") != "2011-06-15" ||
		query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" ||
		!slices.Contains(signed, "host") || !slices.Contains(signed, strings.ToLower(BindingHeader)) ||
		err != nil || !expiresPattern.MatchString(expires) || expiration < 1 || expiration > MaxExpires ||
		!signaturePattern.MatchString(signature) {
		return "", "", time.Time{}, ErrMalformed
	}
	date, err = time.Parse("20060102T150405Z", query.Get("X-Amz-Date"))
	if err != nil {
		return "", "", time.Time{}, ErrMalformed
	}
	return raw, signature, date, nil
}
