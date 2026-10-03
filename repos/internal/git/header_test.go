package git_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/git"
)

func TestReadHeaderValid(t *testing.T) {
	// R-CNMM-9FIZ R-CRAB-EQR2
	if git.ErrHeader == nil {
		t.Fatal("nil ErrHeader")
	}
	cases := []struct {
		name, input, body string
		status            int
		header            http.Header
	}{
		{"empty LF", "\nbody\x00\xff", "body\x00\xff", 200, http.Header{}},
		{"empty CRLF", "\r\nbody", "body", 200, http.Header{}},
		{"mixed", "sTaTuS:  207 Multi Status\r\nx-test:  first  \nx-TEST:second\r\nOther: a:b\n\r\nbody\r\n", "body\r\n", 207, http.Header{"X-Test": {"first", "second"}, "Other": {"a:b"}}},
		{"default", "Content-Type:application/octet-stream\n\nPACK", "PACK", 200, http.Header{"Content-Type": {"application/octet-stream"}}},
		{"largest status", "Status:999 anything\n\n", "", 999, http.Header{}},
		{"smallest status", "Status:100\n\n", "", 100, http.Header{}},
		{"exact limit", "X:" + strings.Repeat("a", git.MaxHeaderBytes-4) + "\n\nTAIL", "TAIL", 200, http.Header{"X": {strings.Repeat("a", git.MaxHeaderBytes-4)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, header, body, err := git.ReadHeader(bytes.NewBufferString(tc.input))
			if err != nil || status != tc.status || !reflect.DeepEqual(header, tc.header) {
				t.Fatalf("ReadHeader = %d, %v, %v", status, header, err)
			}
			data, err := io.ReadAll(body)
			if err != nil || string(data) != tc.body {
				t.Fatalf("body = %q, %v", data, err)
			}
			one := make([]byte, 1)
			if n, err := body.Read(one); n != 0 || !errors.Is(err, io.EOF) {
				t.Fatalf("after body = %d, %v", n, err)
			}
		})
	}
}

type endlessHeader struct{ count int }

func (r *endlessHeader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'X'
	}
	r.count += len(p)
	return len(p), nil
}

func TestReadHeaderRefusesInvalidBoundedly(t *testing.T) {
	// R-CSI7-SIHR
	for _, input := range []string{
		"", "X: a\n", "X: a", "not a header\n\n", ":value\n\n", "X Y:value\n\n", "X\tY:value\n\n", "X\x00Y:value\n\n", "X\x7f:value\n\n", "X\rY:value\n\n",
		"Status:099\n\n", "Status:0\n\n", "Status:12\n\n", "Status:a00\n\n", "Status:1a0\n\n", "Status:10a\n\n", "Status:  \n\n",
		"X:" + strings.Repeat("a", git.MaxHeaderBytes-3) + "\n\n",
	} {
		_, header, body, err := git.ReadHeader(strings.NewReader(input))
		if !errors.Is(err, git.ErrHeader) || header != nil || body != nil {
			t.Fatalf("input %.30q: header=%v body=%v err=%v", input, header, body, err)
		}
	}
	r := new(endlessHeader)
	_, header, body, err := git.ReadHeader(r)
	if !errors.Is(err, git.ErrHeader) || header != nil || body != nil || r.count > git.MaxHeaderBytes+git.CopyBufferSize {
		t.Fatalf("endless reader: read=%d header=%v body=%v err=%v", r.count, header, body, err)
	}
}
