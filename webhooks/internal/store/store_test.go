package store_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
)

var instant = time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.FixedZone("offset", 3600))

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func openDB(t *testing.T, path string, now func() time.Time) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: webhooks.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func open(t *testing.T, path string, random io.Reader, c *clock) (*db.DB, *store.Store) {
	t.Helper()
	d := openDB(t, path, c.Now)
	t.Cleanup(func() { _ = d.Close() })
	// R-YIX5-RXG3
	return d, store.New(d, store.Config{Now: c.Now, Rand: random})
}

// counter yields an endless stream whose every 8-byte block differs.
type counter struct {
	mu sync.Mutex
	n  uint64
	b  []byte
}

func (c *counter) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range p {
		if len(c.b) == 0 {
			c.n++
			c.b = binary.BigEndian.AppendUint64(nil, c.n)
		}
		p[i] = c.b[0]
		c.b = c.b[1:]
	}
	return len(p), nil
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, errors.New("broken source") }

func fixture(t *testing.T) (string, *db.DB, *store.Store, *clock) {
	t.Helper()
	c := &clock{t: instant}
	path := filepath.Join(t.TempDir(), "state", "webhooks.db")
	d, s := open(t, path, &counter{}, c)
	return path, d, s, c
}

func same(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func draft(slug, scheme string) store.Draft {
	// R-YK52-5P6S
	return store.Draft{Slug: slug, Scheme: scheme, OwnerID: "owner", OwnerEmail: "Mixed+tag@example.test"}
}

func create(t *testing.T, s *store.Store, slug, scheme string) (store.Webhook, string) {
	t.Helper()
	w, secret, err := s.Create(context.Background(), draft(slug, scheme))
	if err != nil {
		t.Fatal(err)
	}
	return w, secret
}

func list(t *testing.T, s *store.Store) []store.Webhook {
	t.Helper()
	xs, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return xs
}

func validSecret(s string) bool {
	if !strings.HasPrefix(s, store.SecretPrefix) || len(s) != len(store.SecretPrefix)+52 {
		return false
	}
	for _, r := range s[len(store.SecretPrefix):] {
		if !strings.ContainsRune(crockford, r) {
			return false
		}
	}
	return true
}

func methods(
	func(context.Context) ([]store.Webhook, error),
	func(context.Context, string) (store.Webhook, error),
	func(context.Context, store.Draft) (store.Webhook, string, error),
	func(context.Context, string) (store.Webhook, string, error),
	func(context.Context, string) (store.Webhook, error),
	func(context.Context, string, store.Arrival) (store.Delivery, error),
	func(context.Context, string) (store.Delivery, store.Webhook, error),
	func(context.Context, time.Time) (int64, error),
) {
}

func TestDeclarations(t *testing.T) {
	// R-YLCY-JGXH
	same(t, store.IDPrefix, "whk_")
	same(t, store.DeliveryPrefix, "whd_")
	same(t, store.SecretPrefix, "whs_")
	same(t, store.Bearer, "bearer")
	same(t, store.GitHubHMAC, "github-hmac")
	same(t, store.MaxBody, 1048576)
	const unreachable string = store.Unreachable
	if unreachable == "" {
		t.Fatal("empty unreachable line")
	}
	// R-YNSR-B0EV
	errs := []error{store.ErrNotFound, store.ErrSlugTaken, store.ErrInvalid, store.ErrUnreachable}
	for i, a := range errs {
		if a == nil {
			t.Fatal("nil error")
		}
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Fatalf("errors overlap: %v %v", a, b)
			}
		}
	}
	// R-YQ8K-2JW9: the method set, by use.
	var s *store.Store
	methods(s.List, s.Get, s.Create, s.Rotate, s.Delete, s.Receive, s.Delivery, s.Sweep)
	// R-YK52-5P6S
	_ = store.Webhook{ID: "", Slug: "", Scheme: "", OwnerID: "", OwnerEmail: "", SecretSHA256: "", SecretPlain: "", Created: time.Time{}, LastReceived: time.Time{}}
	_ = store.Arrival{ContentType: "", GitHubEvent: "", GitHubDelivery: "", Body: nil}
	_ = store.Delivery{ID: "", HookID: "", Received: time.Time{}, ContentType: "", GitHubEvent: "", GitHubDelivery: "", Body: nil}
}

func TestValidation(t *testing.T) {
	// R-YMKU-X8O6 R-YRGG-GBMY
	for _, s := range []string{"whk_0123456789abcdef", "whk_0000000000000000"} {
		if !store.ValidID(s) || store.ValidDeliveryID(s) {
			t.Error(s)
		}
	}
	for _, s := range []string{"whd_0123456789abcdef", "whd_ffffffffffffffff"} {
		if !store.ValidDeliveryID(s) || store.ValidID(s) {
			t.Error(s)
		}
	}
	for _, s := range []string{"", "whk_0123456789abcde", "whk_0123456789abcdef0", "WHK_0123456789abcdef", "whk_0123456789abcdeF", "whk_0123456789abcdeg", "whs_0123456789abcdef"} {
		if store.ValidID(s) {
			t.Error(s)
		}
		if store.ValidDeliveryID(strings.Replace(s, "whk_", "whd_", 1)) {
			t.Error("delivery", s)
		}
	}
	// R-YSOC-U3DN
	for _, s := range []string{"gh_push", "a", "tick", "a1_b2", strings.Repeat("a", 64)} {
		if !store.ValidSlug(s) {
			t.Error(s)
		}
	}
	for _, s := range []string{"gh-push", "GH_push", "9am", "_gh", "gh__push", "gh_", "gh.push", "gh push", "", strings.Repeat("a", 65), "aé"} {
		if store.ValidSlug(s) {
			t.Error(s)
		}
	}
	// R-YTW9-7V4C
	for s, want := range map[string]bool{"bearer": true, "github-hmac": true, "": false, "Bearer": false, "hmac": false, "github_hmac": false} {
		if store.ValidScheme(s) != want {
			t.Error(s)
		}
	}
	for _, secret := range []string{"", "whs_abc", "some secret value"} {
		sum := sha256.Sum256([]byte(secret))
		same(t, store.HashSecret(secret), hex.EncodeToString(sum[:]))
	}
}

func TestCreateAndRestart(t *testing.T) {
	ctx := context.Background()
	c := &clock{t: instant}
	path := filepath.Join(t.TempDir(), "state", "webhooks.db")
	random := bytes.Repeat([]byte{0x5a}, 40)
	random = append(random, bytes.Repeat([]byte{0x11}, 40)...)
	d := openDB(t, path, c.Now)
	s := store.New(d, store.Config{Now: c.Now, Rand: bytes.NewReader(random)})
	// R-YYRU-QY34: empty store.
	empty := list(t, s)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list %#v", empty)
	}
	if _, err := s.Get(ctx, "gh_push"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	// R-YV45-LMV1 R-YWC1-ZELQ
	w, secret, err := s.Create(ctx, draft("gh_push", store.Bearer))
	if err != nil {
		t.Fatal(err)
	}
	same(t, w.ID, "whk_5a5a5a5a5a5a5a5a")
	wantSecret := store.SecretPrefix + base32.NewEncoding(crockford).WithPadding(base32.NoPadding).EncodeToString(bytes.Repeat([]byte{0x5a}, 32))
	same(t, secret, wantSecret)
	if !validSecret(secret) || !store.ValidID(w.ID) {
		t.Fatal(secret, w.ID)
	}
	same(t, w.Slug, "gh_push")
	same(t, w.Scheme, store.Bearer)
	same(t, w.OwnerID, "owner")
	same(t, w.OwnerEmail, "Mixed+tag@example.test")
	same(t, w.SecretSHA256, store.HashSecret(secret))
	same(t, w.SecretPlain, "")
	same(t, w.Created, instant.UTC().Truncate(time.Second))
	if !w.LastReceived.IsZero() {
		t.Fatal("received")
	}
	h, plain, err := s.Create(ctx, draft("gh_hmac", store.GitHubHMAC))
	if err != nil {
		t.Fatal(err)
	}
	same(t, h.ID, "whk_1111111111111111")
	same(t, h.SecretPlain, plain)
	same(t, h.SecretSHA256, "")
	if !validSecret(plain) {
		t.Fatal(plain)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	// R-YV45-LMV1: across a restart.
	d2 := openDB(t, path, c.Now)
	t.Cleanup(func() { _ = d2.Close() })
	s2 := store.New(d2, store.Config{Now: c.Now, Rand: &counter{}})
	got, err := s2.Get(ctx, "gh_push")
	if err != nil {
		t.Fatal(err)
	}
	same(t, got, w)
	got, err = s2.Get(ctx, "gh_hmac")
	if err != nil {
		t.Fatal(err)
	}
	same(t, got, h)
	// R-Z8J1-T40O
	for _, x := range list(t, s2) {
		if x.Created.Location() != time.UTC || x.Created.Nanosecond() != 0 {
			t.Fatal(x.Created)
		}
	}
}

func TestRedraw(t *testing.T) {
	c := &clock{t: instant}
	path := filepath.Join(t.TempDir(), "state", "webhooks.db")
	first := []byte{0x3f, 0x9a, 0x1c, 0x7e, 0x5b, 0x2d, 0x80, 0x46}
	random := append(append([]byte{}, first...), make([]byte, 32)...)
	random = append(random, first...)
	random = append(random, 0, 0, 0, 0, 0, 0, 0, 1)
	random = append(random, make([]byte, 32)...)
	r := &countingReader{r: bytes.NewReader(random)}
	_, s := open(t, path, r, c)
	a, _ := create(t, s, "alpha", store.Bearer)
	same(t, a.ID, "whk_3f9a1c7e5b2d8046")
	same(t, r.n, 40)
	// R-YWC1-ZELQ: a candidate that is taken is drawn again.
	b, _ := create(t, s, "beta", store.Bearer)
	same(t, b.ID, "whk_0000000000000001")
	same(t, r.n, 88)
}

func TestCreateRefusals(t *testing.T) {
	ctx := context.Background()
	_, d, s, c := fixture(t)
	create(t, s, "taken", store.Bearer)
	before := list(t, s)
	// R-YXJY-D6CF
	for _, tc := range []struct {
		d    store.Draft
		want error
	}{
		{store.Draft{Slug: "Bad", Scheme: store.Bearer, OwnerID: "owner"}, store.ErrInvalid},
		{store.Draft{Slug: "fine", Scheme: "basic", OwnerID: "owner"}, store.ErrInvalid},
		{store.Draft{Slug: "fine", Scheme: "", OwnerID: "owner"}, store.ErrInvalid},
		{store.Draft{Slug: "fine", Scheme: store.Bearer, OwnerID: ""}, store.ErrInvalid},
		{store.Draft{Slug: "taken", Scheme: store.GitHubHMAC, OwnerID: "other"}, store.ErrSlugTaken},
	} {
		r := &countingReader{r: &counter{}}
		w, secret, err := store.New(d, store.Config{Now: c.Now, Rand: r}).Create(ctx, tc.d)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%v: %v", tc.d, err)
		}
		same(t, w, store.Webhook{})
		same(t, secret, "")
		same(t, r.n, 0)
		same(t, list(t, s), before)
	}
	// R-YXJY-D6CF: concurrent creates of one slug.
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, results[i] = s.Create(ctx, draft("race", store.Bearer))
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, store.ErrSlugTaken):
			t.Fatal(err)
		}
	}
	same(t, ok, 1)
}

func TestGetAndList(t *testing.T) {
	ctx := context.Background()
	_, _, s, _ := fixture(t)
	for _, slug := range []string{"tick", "alpha", "gh_push", "zeta", "a1"} {
		x, _, err := s.Create(ctx, store.Draft{Slug: slug, Scheme: store.Bearer, OwnerID: "u_" + slug, OwnerEmail: slug + "@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		// R-YYRU-QY34: any owner's webhook by slug.
		got, err := s.Get(ctx, slug)
		if err != nil {
			t.Fatal(err)
		}
		same(t, got, x)
	}
	var slugs []string
	for _, x := range list(t, s) {
		slugs = append(slugs, x.Slug)
	}
	same(t, slugs, []string{"a1", "alpha", "gh_push", "tick", "zeta"})
	tick, _ := s.Get(ctx, "tick")
	for _, slug := range []string{"", "Tick", "tic", tick.ID} {
		if _, err := s.Get(ctx, slug); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(slug, err)
		}
	}
}

func TestRotate(t *testing.T) {
	ctx := context.Background()
	path, d, s, c := fixture(t)
	b, old := create(t, s, "bearer_one", store.Bearer)
	h, oldPlain := create(t, s, "hmac_one", store.GitHubHMAC)
	// R-YZZR-4PTT
	r := &countingReader{r: bytes.NewReader(bytes.Repeat([]byte{0x77}, 32))}
	rs := store.New(d, store.Config{Now: c.Now, Rand: r})
	nb, secret, err := rs.Rotate(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	same(t, r.n, 32)
	same(t, secret, store.SecretPrefix+base32.NewEncoding(crockford).WithPadding(base32.NoPadding).EncodeToString(bytes.Repeat([]byte{0x77}, 32)))
	if secret == old {
		t.Fatal("secret unchanged")
	}
	want := b
	want.SecretSHA256 = store.HashSecret(secret)
	same(t, nb, want)
	nh, plain, err := s.Rotate(ctx, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plain == oldPlain || !validSecret(plain) {
		t.Fatal(plain)
	}
	wantH := h
	wantH.SecretPlain = plain
	same(t, nh, wantH)
	before := list(t, s)
	r2 := &countingReader{r: &counter{}}
	w, sec, err := store.New(d, store.Config{Now: c.Now, Rand: r2}).Rotate(ctx, "whk_0000000000000999")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	same(t, w, store.Webhook{})
	same(t, sec, "")
	same(t, list(t, s), before)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_, s2 := open(t, path, &counter{}, c)
	same(t, list(t, s2), before)
}

func receive(t *testing.T, s *store.Store, id string, a store.Arrival) store.Delivery {
	t.Helper()
	got, err := s.Receive(context.Background(), id, a)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReceiveAndDelivery(t *testing.T) {
	ctx := context.Background()
	path, d, s, c := fixture(t)
	w, _ := create(t, s, "gh_push", store.GitHubHMAC)
	other, _ := create(t, s, "other", store.Bearer)
	at := time.Date(2026, 10, 9, 14, 12, 37, 987654321, time.FixedZone("x", -7200))
	c.Set(at)
	r := &countingReader{r: bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8})}
	body := []byte{0xff, 0x00, 'h', 'i', '\n'}
	// R-Z2FJ-W9B7
	a := store.Arrival{ContentType: "application/octet-stream; x=1", GitHubEvent: "push", GitHubDelivery: "abc-def", Body: body}
	got, err := store.New(d, store.Config{Now: c.Now, Rand: r}).Receive(ctx, w.ID, a)
	if err != nil {
		t.Fatal(err)
	}
	same(t, got.ID, "whd_0102030405060708")
	same(t, r.n, 8)
	same(t, got.HookID, w.ID)
	same(t, got.Received, at.UTC().Truncate(time.Second))
	same(t, got.Received.Location(), time.UTC)
	same(t, got.ContentType, a.ContentType)
	same(t, got.GitHubEvent, "push")
	same(t, got.GitHubDelivery, "abc-def")
	same(t, got.Body, body)
	updated, _ := s.Get(ctx, "gh_push")
	want := w
	want.LastReceived = got.Received
	same(t, updated, want)
	o, _ := s.Get(ctx, "other")
	same(t, o, other)
	// R-Z2FJ-W9B7: nil body reads as empty; ids are fresh.
	empty := receive(t, s, other.ID, store.Arrival{})
	if empty.Body == nil || len(empty.Body) != 0 || empty.ID == got.ID || !store.ValidDeliveryID(empty.ID) {
		t.Fatalf("%#v", empty)
	}
	// R-Z4VC-NSSL
	dl, hook, err := s.Delivery(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	same(t, dl, got)
	same(t, hook, updated)
	if _, _, err := s.Delivery(ctx, "whd_00000000000000ff"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	// R-Z3NG-A11W
	before := list(t, s)
	big := make([]byte, store.MaxBody+1)
	if _, err := s.Receive(ctx, w.ID, store.Arrival{Body: big}); !errors.Is(err, store.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.Receive(ctx, "whk_00000000000000ff", store.Arrival{Body: body}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	same(t, list(t, s), before)
	exact := receive(t, s, w.ID, store.Arrival{Body: make([]byte, store.MaxBody)})
	same(t, len(exact.Body), store.MaxBody)
	// R-Z4VC-NSSL: across a restart.
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_, s2 := open(t, path, &counter{}, c)
	dl, hook, err = s2.Delivery(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	same(t, dl, got)
	updated2, _ := s2.Get(ctx, "gh_push")
	same(t, hook, updated2)
	// R-Z8J1-T40O
	if dl.Received.Location() != time.UTC || dl.Received.Nanosecond() != 0 || hook.LastReceived.Location() != time.UTC {
		t.Fatal("times")
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	path, d, s, c := fixture(t)
	w, _ := create(t, s, "gone", store.Bearer)
	keep, _ := create(t, s, "kept", store.GitHubHMAC)
	d1 := receive(t, s, w.ID, store.Arrival{Body: []byte("one")})
	d2 := receive(t, s, w.ID, store.Arrival{Body: []byte("two")})
	k1 := receive(t, s, keep.ID, store.Arrival{Body: []byte("three")})
	keepNow, _ := s.Get(ctx, "kept")
	// R-Z17N-IHKI
	before, _ := s.Get(ctx, "gone")
	got, err := s.Delete(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	same(t, got, before)
	if _, err := s.Get(ctx, "gone"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	for _, id := range []string{d1.ID, d2.ID} {
		if _, _, err := s.Delivery(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	}
	kd, kh, err := s.Delivery(ctx, k1.ID)
	if err != nil {
		t.Fatal(err)
	}
	same(t, kd, k1)
	same(t, kh, keepNow)
	snapshot := list(t, s)
	if _, err := s.Delete(ctx, w.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	same(t, list(t, s), snapshot)
	again, _, err := s.Create(ctx, store.Draft{Slug: "gone", Scheme: store.GitHubHMAC, OwnerID: "someone"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == w.ID {
		t.Fatal("same id")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_, s2 := open(t, path, &counter{}, c)
	for _, id := range []string{d1.ID, d2.ID} {
		if _, _, err := s2.Delivery(ctx, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	}
}

func TestSweep(t *testing.T) {
	ctx := context.Background()
	_, _, s, c := fixture(t)
	w, _ := create(t, s, "tick", store.Bearer)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var ids []string
	for i := 0; i < 4; i++ {
		c.Set(base.Add(time.Duration(i) * time.Hour))
		ids = append(ids, receive(t, s, w.ID, store.Arrival{Body: []byte("x")}).ID)
	}
	hook, _ := s.Get(ctx, "tick")
	// R-Z639-1KJA: received earlier than before (truncated) goes; equal stays.
	n, err := s.Sweep(ctx, base.Add(2*time.Hour+500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	same(t, n, int64(2))
	for i, id := range ids {
		_, _, err := s.Delivery(ctx, id)
		if (i < 2) != errors.Is(err, store.ErrNotFound) {
			t.Fatal(i, err)
		}
	}
	after, _ := s.Get(ctx, "tick")
	same(t, after, hook)
	n, err = s.Sweep(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	same(t, n, int64(0))
}

func TestUnreachable(t *testing.T) {
	ctx := context.Background()
	_, d, s, c := fixture(t)
	w, _ := create(t, s, "tick", store.Bearer)
	dl := receive(t, s, w.ID, store.Arrival{Body: []byte("x")})
	snapshot := list(t, s)
	check := func(err error) {
		t.Helper()
		// R-Z7B5-FC9Z
		if !errors.Is(err, store.ErrUnreachable) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) || errors.Is(err, store.ErrSlugTaken) {
			t.Fatalf("error %v", err)
		}
	}
	calls := func(ctx context.Context, s *store.Store) {
		_, err := s.List(ctx)
		check(err)
		_, err = s.Get(ctx, "tick")
		check(err)
		_, _, err = s.Create(ctx, draft("fresh", store.Bearer))
		check(err)
		_, _, err = s.Rotate(ctx, w.ID)
		check(err)
		_, err = s.Delete(ctx, w.ID)
		check(err)
		_, err = s.Receive(ctx, w.ID, store.Arrival{})
		check(err)
		_, _, err = s.Delivery(ctx, dl.ID)
		check(err)
		_, err = s.Sweep(ctx, instant.Add(time.Hour))
		check(err)
	}
	d.SetFailing(true)
	calls(ctx, s)
	d.SetFailing(false)
	same(t, list(t, s), snapshot)
	done, cancel := context.WithCancel(ctx)
	cancel()
	calls(done, s)
	same(t, list(t, s), snapshot)
	bad := store.New(d, store.Config{Now: c.Now, Rand: failing{}})
	_, _, err := bad.Create(ctx, draft("fresh", store.Bearer))
	check(err)
	_, _, err = bad.Rotate(ctx, w.ID)
	check(err)
	_, err = bad.Receive(ctx, w.ID, store.Arrival{})
	check(err)
	same(t, list(t, s), snapshot)
	if _, _, err := s.Delivery(ctx, dl.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrent(t *testing.T) {
	ctx := context.Background()
	c := &clock{t: instant}
	path := filepath.Join(t.TempDir(), "state", "webhooks.db")
	// R-Z8J1-T40O: a reader unsafe for concurrent use.
	data := make([]byte, 0, 40*64*2)
	for i := 0; i < 128; i++ {
		block := make([]byte, 40)
		block[6], block[7] = byte(i>>8), byte(i+1)
		data = append(data, block...)
	}
	_, s := open(t, path, bytes.NewReader(data), c)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slug := "hook" + strconv.Itoa(i)
			w, _, err := s.Create(ctx, draft(slug, store.Bearer))
			if err != nil {
				errs <- err
				return
			}
			if _, err := s.Get(ctx, slug); err != nil {
				errs <- err
			}
			if _, err := s.List(ctx); err != nil {
				errs <- err
			}
			if w.Created.Location() != time.UTC || w.Created.Nanosecond() != 0 {
				errs <- errors.New("created time")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	same(t, len(list(t, s)), 16)
}
