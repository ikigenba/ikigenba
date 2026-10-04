package visitor_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ikigenba/ikigenba/sites/internal/visitor"
)

// R-EGX9-4T9E R-EI55-IL03 R-EKKY-A4HH R-ELSU-NW86 R-EN0R-1NYV R-EO8N-FFPK R-EPGJ-T7G9
func TestDeclarations(t *testing.T) {
	// These assignments also require the constants to be untyped.
	var (
		name   namedString
		prefix namedString
		age    namedInt
	)
	name = visitor.CookieName
	prefix = visitor.IDPrefix
	age = visitor.MaxAge
	if name != "ikigenba_visitor" || prefix != "vis_" || age != 34560000 {
		t.Fatal(name, prefix, age)
	}
	var (
		valid    func(string) bool
		mint     func(io.Reader) (string, error)
		from     func(*http.Request) (string, bool)
		secure   func(*http.Request) bool
		cookie   func(string, bool) string
		referrer func(*http.Request) string
	)
	valid = visitor.ValidID
	mint = visitor.Mint
	from = visitor.FromRequest
	secure = visitor.Secure
	cookie = visitor.Cookie
	referrer = visitor.ReferrerHost
	id, err := mint(bytes.NewReader(make([]byte, 8)))
	if err != nil || !valid(id) || cookie(id, false) == "" {
		t.Fatal(id, err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	if got, ok := from(r); got != "" || ok || secure(r) || referrer(r) != "" {
		t.Fatal(got, ok)
	}
}

type namedString string
type namedInt int

// R-EQOG-6Z6Y
func TestValidID(t *testing.T) {
	for _, s := range []string{"vis_1a2b3c4d5e6f7081", "vis_0000000000000000", "vis_ffffffffffffffff"} {
		if !visitor.ValidID(s) {
			t.Errorf("rejected %q", s)
		}
	}
	for _, s := range []string{"hello", "vis_1A2B3C4D5E6F7081", "vis_1a2b3c4d5e6f708", "vis_1a2b3c4d5e6f70812", "sit_1a2b3c4d5e6f7081", "vis_1a2b3c4d5e6f708g", "", "vis_000000000000000é"} {
		if visitor.ValidID(s) {
			t.Errorf("accepted %q", s)
		}
	}
	for i := 4; i < 20; i++ {
		for _, c := range []byte{'/', 'g', 'A', ':', 0, 255} {
			b := []byte("vis_0123456789abcdef")
			b[i] = c
			if visitor.ValidID(string(b)) {
				t.Errorf("accepted invalid byte at %d", i)
			}
		}
	}
}

// R-ERWC-KQXN
func TestMintExactlyEightBytes(t *testing.T) {
	r := bytes.NewReader([]byte{0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f, 0x70, 0x81, 0xff})
	id, err := visitor.Mint(r)
	if err != nil || id != "vis_1a2b3c4d5e6f7081" {
		t.Fatal(id, err)
	}
	rest, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(rest, []byte{0xff}) {
		t.Fatal(rest, err)
	}
	// A short-read reader must be filled rather than treated as exhausted.
	id, err = visitor.Mint(&oneByteReader{data: []byte{0, 1, 2, 3, 4, 5, 6, 7}})
	if err != nil || id != "vis_0001020304050607" {
		t.Fatal(id, err)
	}
}

type oneByteReader struct{ data []byte }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("random source failed") }

// R-ET48-YIOC
func TestMintFailure(t *testing.T) {
	for n := 0; n < 8; n++ {
		if id, err := visitor.Mint(bytes.NewReader(make([]byte, n))); id != "" || err == nil {
			t.Fatal(n, id, err)
		}
	}
	if id, err := visitor.Mint(io.MultiReader(bytes.NewReader([]byte{1, 2, 3}), brokenReader{})); id != "" || err == nil {
		t.Fatal(id, err)
	}
}

// R-EUC5-CAF1
func TestFirstVisitorCookie(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"theme=dark; ikigenba_visitor=vis_1a2b3c4d5e6f7081", "vis_1a2b3c4d5e6f7081"},
		{`ikigenba_visitor="vis_1a2b3c4d5e6f7081"`, "vis_1a2b3c4d5e6f7081"},
		{"", ""}, {"ikigenba_visitor=", ""}, {"ikigenba_visitor=hello", ""},
		{"ikigenba_visitor=x; ikigenba_visitor=vis_1a2b3c4d5e6f7081", ""},
		{"ikigenba_visitor=vis_1a2b3c4d5e6f7081; ikigenba_visitor=x", "vis_1a2b3c4d5e6f7081"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.header != "" {
			r.Header.Set("Cookie", tc.header)
		}
		got, ok := visitor.FromRequest(r)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("%q: %q %v", tc.header, got, ok)
		}
	}
}

// R-EVK1-Q25Q
func TestSecure(t *testing.T) {
	for _, s := range []string{"https", "HTTPS", "HtTpS", "", "http", "ftp", "https, http", " https", "https ", "hKtps"} {
		r := httptest.NewRequest("GET", "/", nil)
		if s != "" {
			r.Header.Set("X-Forwarded-Proto", s)
		}
		want := s == "https" || s == "HTTPS" || s == "HtTpS"
		r.Header.Add("X-Forwarded-Proto", "http")
		if got := visitor.Secure(r); got != want {
			t.Errorf("Secure(%q)=%v", s, got)
		}
	}
}

// R-EWRY-3TWF
func TestCookieExactText(t *testing.T) {
	for _, id := range []string{"vis_1a2b3c4d5e6f7081", "", "arbitrary"} {
		for _, secure := range []bool{false, true} {
			want := "ikigenba_visitor=" + id + "; Path=/; Max-Age=34560000; HttpOnly; SameSite=Lax"
			if secure {
				want += "; Secure"
			}
			if got := visitor.Cookie(id, secure); got != want {
				t.Errorf("Cookie=%q want %q", got, want)
			}
		}
	}
}

// R-EXZU-HLN4
func TestReferrerHost(t *testing.T) {
	for _, tc := range []struct{ ref, want string }{{"https://example.org/post?x=1", "example.org"}, {"http://localhost:8080/x", "localhost:8080"}, {"https://user:pw@example.org/", "example.org"}, {"http://[example.org/", ""}, {"about:blank", ""}, {"not a url", ""}, {"", ""}, {"//Example.org:8080/x", "Example.org:8080"}, {"https://[::1]:8080/x", "[::1]:8080"}} {
		r := httptest.NewRequest("GET", "/", nil)
		if tc.ref != "" {
			r.Header.Set("Referer", tc.ref)
		}
		if got := visitor.ReferrerHost(r); got != tc.want {
			t.Errorf("ReferrerHost(%q)=%q", tc.ref, got)
		}
	}
}
