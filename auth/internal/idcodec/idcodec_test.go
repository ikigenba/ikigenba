package idcodec

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
)

var (
	_ func([]byte) string             = Encode
	_ func(io.Reader) (string, error) = NewID
	_ func(io.Reader) (string, error) = NewSecret
	_ func(string) string             = HashSecret
)

func TestConstants(t *testing.T) {
	// R-41J3-NIWG
	if Alphabet != "0123456789ABCDEFGHJKMNPQRSTVWXYZ" {
		t.Fatalf("Alphabet = %q", Alphabet)
	}
	if SecretPrefix != "ikp_" {
		t.Fatalf("SecretPrefix = %q", SecretPrefix)
	}
}

func TestEncodeReferenceVectors(t *testing.T) {
	// R-42R0-1AN5
	// R-5243-4N5A
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{name: "empty", in: nil, want: ""},
		{name: "zero", in: []byte{0x00}, want: "00"},
		{name: "ones", in: []byte{0xff}, want: "ZW"},
		{name: "leading zero bits", in: []byte{0x00, 0x01}, want: "000G"},
		{name: "foo", in: []byte("foo"), want: "CSQPY"},
		{
			name: "sixteen bytes",
			in:   sequentialBytes(16),
			want: "000G40R40M30E209185GR38E1W",
		},
		{
			name: "thirty-two bytes",
			in:   sequentialBytes(32),
			want: "000G40R40M30E209185GR38E1W8124GK2GAHC5RR34D1P70X3RFG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Encode(tt.in)
			if got != tt.want {
				t.Fatalf("Encode(%x) = %q, want %q", tt.in, got, tt.want)
			}
			if again := Encode(tt.in); again != got {
				t.Fatalf("second Encode(%x) = %q, first = %q", tt.in, again, got)
			}
			wantLen := (8*len(tt.in) + 4) / 5
			if len(got) != wantLen {
				t.Fatalf("len(Encode(%x)) = %d, want %d", tt.in, len(got), wantLen)
			}
			for _, char := range got {
				if !strings.ContainsRune(Alphabet, char) {
					t.Fatalf("Encode(%x) contains character %q outside Alphabet", tt.in, char)
				}
			}
		})
	}
}

func TestEncodeLengthAndAlphabetForEveryRemainder(t *testing.T) {
	for size := 0; size <= 64; size++ {
		in := sequentialBytes(size)
		got := Encode(in)
		wantLen := (8*size + 4) / 5
		if len(got) != wantLen {
			t.Fatalf("len(Encode(%d bytes)) = %d, want %d", size, len(got), wantLen)
		}
		assertEncodedShape(t, got, wantLen)
		if again := Encode(in); again != got {
			t.Fatalf("second Encode(%d bytes) = %q, first = %q", size, again, got)
		}
	}
}

func TestNewIDReadsExactlySixteenBytes(t *testing.T) {
	// R-43YW-F2DU
	// R-53BZ-IEVZ
	rand := &oneByteReader{data: sequentialBytes(16)}
	got, err := NewID(rand)
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	if got != "000G40R40M30E209185GR38E1W" {
		t.Fatalf("NewID() = %q", got)
	}
	if rand.read != 16 {
		t.Fatalf("NewID() read %d bytes, want 16", rand.read)
	}
	assertEncodedShape(t, got, 26)
}

func TestNewIDReadErrors(t *testing.T) {
	t.Run("short input", func(t *testing.T) {
		got, err := NewID(bytes.NewReader(make([]byte, 15)))
		if err == nil || got != "" {
			t.Fatalf("NewID() = %q, %v; want empty id and error", got, err)
		}
	})

	t.Run("non-EOF error after partial input", func(t *testing.T) {
		got, err := NewID(&partialErrorReader{data: make([]byte, 15)})
		if err == nil || got != "" {
			t.Fatalf("NewID() = %q, %v; want empty id and error", got, err)
		}
	})

	t.Run("error after full input", func(t *testing.T) {
		got, err := NewID(&terminalErrorReader{data: make([]byte, 16)})
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		if got != strings.Repeat("0", 26) {
			t.Fatalf("NewID() = %q", got)
		}
	})
}

func TestNewSecretReadsExactlyThirtyTwoBytes(t *testing.T) {
	// R-456S-SU4J
	// R-54JV-W6MO
	rand := &oneByteReader{data: sequentialBytes(32)}
	got, err := NewSecret(rand)
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	want := "ikp_000G40R40M30E209185GR38E1W8124GK2GAHC5RR34D1P70X3RFG"
	if got != want {
		t.Fatalf("NewSecret() = %q, want %q", got, want)
	}
	if rand.read != 32 {
		t.Fatalf("NewSecret() read %d bytes, want 32", rand.read)
	}
	if !strings.HasPrefix(got, SecretPrefix) {
		t.Fatalf("NewSecret() = %q, missing prefix %q", got, SecretPrefix)
	}
	assertEncodedShape(t, strings.TrimPrefix(got, SecretPrefix), 52)
}

func TestNewSecretReadErrors(t *testing.T) {
	t.Run("short input", func(t *testing.T) {
		got, err := NewSecret(bytes.NewReader(make([]byte, 31)))
		if err == nil || got != "" {
			t.Fatalf("NewSecret() = %q, %v; want empty secret and error", got, err)
		}
	})

	t.Run("non-EOF error after partial input", func(t *testing.T) {
		got, err := NewSecret(&partialErrorReader{data: make([]byte, 31)})
		if err == nil || got != "" {
			t.Fatalf("NewSecret() = %q, %v; want empty secret and error", got, err)
		}
	})

	t.Run("error after full input", func(t *testing.T) {
		got, err := NewSecret(&terminalErrorReader{data: make([]byte, 32)})
		if err != nil {
			t.Fatalf("NewSecret() error = %v", err)
		}
		if got != SecretPrefix+strings.Repeat("0", 52) {
			t.Fatalf("NewSecret() = %q", got)
		}
	})
}

func TestHashSecret(t *testing.T) {
	// R-46EP-6LV8
	// R-G99G-TBAH (HashSecret behavior only; persistence is owned by internal/store.)
	tests := []struct {
		secret string
		want   string
	}{
		{
			secret: "",
			want:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			secret: "ikp_test",
			want:   "90ef96e631e3364c8b559dba38d91d5557cdd418c8996b8108a0f3975291cd5b",
		},
	}
	hexPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)

	for _, tt := range tests {
		got := HashSecret(tt.secret)
		if got != tt.want {
			t.Errorf("HashSecret(%q) = %q, want %q", tt.secret, got, tt.want)
		}
		if !hexPattern.MatchString(got) {
			t.Errorf("HashSecret(%q) = %q, want 64 lowercase hex characters", tt.secret, got)
		}
		if again := HashSecret(tt.secret); again != got {
			t.Errorf("second HashSecret(%q) = %q, first = %q", tt.secret, again, got)
		}
	}
}

func sequentialBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}

	return b
}

func assertEncodedShape(t *testing.T, value string, wantLen int) {
	t.Helper()
	if len(value) != wantLen {
		t.Fatalf("encoded length = %d, want %d", len(value), wantLen)
	}
	for _, char := range value {
		if !strings.ContainsRune(Alphabet, char) {
			t.Fatalf("encoded value contains character %q outside Alphabet", char)
		}
	}
}

type oneByteReader struct {
	data []byte
	read int
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.read == len(r.data) {
		return 0, io.EOF
	}

	p[0] = r.data[r.read]
	r.read++

	return 1, nil
}

type terminalErrorReader struct {
	data []byte
}

func (r *terminalErrorReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]

	return n, errors.New("terminal error")
}

type partialErrorReader struct {
	data []byte
}

func (r *partialErrorReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = nil

	return n, errors.New("partial error")
}
