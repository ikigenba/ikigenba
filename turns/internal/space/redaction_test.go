package space_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/turns/internal/space"
)

type finalErrorReader struct {
	io.Reader
	remaining int
}

func (r *finalErrorReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.remaining -= n
	if r.remaining == 0 {
		return n, errors.New("failure beside last bytes")
	}
	return n, err
}

func (*finalErrorReader) Close() error { return nil }

// R-FK43-K3SB
func TestReadErrorAlongsideLastRequiredBytes(t *testing.T) {
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, status := range []int{200, 400} {
				for _, delta := range []int{-1, 0} {
					length := 4096 + len(testToken) - 1 + delta
					hc := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
						res := response(status, "text/plain", "")
						res.Body = &finalErrorReader{Reader: strings.NewReader(strings.Repeat("z", length)), remaining: length}
						return res, nil
					})}
					held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
					if status == 200 || delta == -1 {
						assertFailure(t, held, err, "cannot reach "+space.Endpoint(testSpace), "failure beside last bytes", false)
					} else {
						assertFailure(t, held, err, "the space answered HTTP 400", strings.Repeat("z", 4096), true)
					}
				}
			}
		})
	}
}

// R-FHOA-SKAX
func TestRedactedPrefix(t *testing.T) {
	cases := []struct {
		name, token, input, want string
	}{
		{"short-repeated", "tok", "x tok ytok", "x <token> y<token>"},
		{"crossing-short", "tok", strings.Repeat("a", 4094) + "toktokz", strings.Repeat("a", 4094) + "<t"},
		{"no-sliding", "0123456789", "0123456789" + strings.Repeat("a", 4086) + "0123456789", "<token>" + strings.Repeat("a", 4086)},
		{"crossing-long", "0123456789", strings.Repeat("a", 4094) + "0123456789tail", strings.Repeat("a", 4094) + "<t"},
		{"starts-at-cut", "tok", strings.Repeat("a", 4096) + "tok", strings.Repeat("a", 4096)},
		{"nonoverlapping", "aba", "abababa", "<token>b<token>"},
		{"empty-short", "", "x tok ytok", "x tok ytok"},
		{"empty-long", "", strings.Repeat("a", 4096) + "tail", strings.Repeat("a", 4096)},
	}
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					hc := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
						return response(401, "text/plain", tc.input), nil
					})}
					held, err := m.call(space.New(testSpace, tc.token, hc), context.Background())
					assertFailure(t, held, err, "the space refused the token", tc.want, true)
				})
			}
		})
	}
}

// R-FHOA-SKAX R-FIW7-6C1M R-FK43-K3SB
func TestUnquotedErrorsRedactWholeText(t *testing.T) {
	cases := []struct {
		name, token, input, want string
	}{
		{"repeated", "tok", "x tok ytok", "x <token> y<token>"},
		{"nonoverlapping", "aba", "abababa", "<token>b<token>"},
		{"no-cap", "tok", strings.Repeat("a", 4094) + "toktokz", strings.Repeat("a", 4094) + "<token><token>z"},
		{"empty", "", "x tok ytok", "x tok ytok"},
	}
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					for _, failure := range []string{"transport", "read"} {
						t.Run(failure, func(t *testing.T) {
							hc := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
								if failure == "transport" {
									return nil, errors.New(tc.input)
								}
								res := response(200, "application/json", "")
								res.Body = &failingReader{failure: errors.New(tc.input)}
								return res, nil
							})}
							held, err := m.call(space.New(testSpace, tc.token, hc), context.Background())
							want := tc.want
							if failure == "transport" {
								want = `Post "` + space.Endpoint(testSpace) + `": ` + want
							}
							assertFailure(t, held, err, "cannot reach "+space.Endpoint(testSpace), want, false)
						})
					}
				})
			}
		})
	}
}

// R-MH6Z-4PMD R-FIW7-6C1M R-FK43-K3SB R-FLBZ-XVJ0 R-FMJW-BN9P
// R-FNRS-PF0E R-FOZP-36R3 R-FQ7L-GYHS R-FRFH-UQ8H
func TestDistinctiveTokenNeverAppearsInFailures(t *testing.T) {
	texts := []struct{ name, input, redacted, prefix string }{
		{"absent", "arbitrary upstream failure", "arbitrary upstream failure", "arbitrary upstream failure"},
		{"echo", "before " + testToken + " after " + testToken, "before <token> after <token>", "before <token> after <token>"},
		{"crossing", strings.Repeat("z", 4094) + testToken + "tail", strings.Repeat("z", 4094) + "<token>tail", strings.Repeat("z", 4094) + "<t"},
	}
	classes := []string{"transport", "read", "401", "400", "301", "302", "303", "307", "308", "unreadable", "gateway", "tool"}
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			for _, class := range classes {
				t.Run(class, func(t *testing.T) {
					for _, tc := range texts {
						t.Run(tc.name, func(t *testing.T) {
							calls := 0
							hc := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
								calls++
								if class == "transport" {
									return nil, errors.New(tc.input)
								}
								if class == "read" {
									res := response(200, "application/json", "")
									res.Body = &failingReader{failure: errors.New(tc.input)}
									return res, nil
								}
								if status, err := strconv.Atoi(class); err == nil {
									res := response(status, "text/plain", tc.input)
									res.Header.Set("Location", "https://other.invalid/target")
									return res, nil
								}
								if class == "unreadable" {
									return response(200, "text/plain", tc.input), nil
								}
								a := answer()
								a["id"] = decode(t, req.Body)["id"]
								if class == "gateway" {
									delete(a, "result")
									a["error"] = map[string]any{"message": tc.input}
								} else {
									result(a)["isError"] = true
									result(a)["content"] = []any{map[string]any{"type": "text", "text": tc.input}}
								}
								return response(200, "application/json", encode(t, a)), nil
							})}
							held, err := m.call(space.New(testSpace, testToken, hc), context.Background())
							reason, detail, quoted := "", tc.prefix, true
							switch class {
							case "transport", "read":
								reason, detail, quoted = "cannot reach "+space.Endpoint(testSpace), tc.redacted, false
								if class == "transport" {
									detail = `Post "` + space.Endpoint(testSpace) + `": ` + detail
								}
							case "401":
								reason = "the space refused the token"
							case "unreadable":
								reason = "the space's answer could not be read"
							case "gateway":
								reason = "the gateway refused the request"
							case "tool":
								reason = "sessions refused the query"
								if m.name == "push" {
									reason = "sessions refused a batch"
								}
							default:
								reason = "the space answered HTTP " + class
							}
							assertFailure(t, held, err, reason, detail, quoted)
							if calls != 1 {
								t.Fatalf("sent %d HTTP requests", calls)
							}
						})
					}
				})
			}
		})
	}
}
