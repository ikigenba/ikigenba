package paging_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/paging"
)

// R-QOMG-GI11: The consumer imports the declared path and package name.
// R-QPUC-U9RQ: A positional literal constructs the single-field Token.
// R-QR29-81IF: Encode is usable as the declared variadic value-receiver method expression.
// R-QSA5-LT94: Decode is usable as the declared variadic value-receiver method expression.
// R-QTI1-ZKZT: ErrToken is an exported, non-nil error.
// R-R9CQ-YLMU: Its Error method returns the named copy constant.
func TestPublicAPI(t *testing.T) {
	token := paging.Token{"consumer_tool"}
	var encode func(paging.Token, ...any) string
	var decode func(paging.Token, string, ...any) error
	var err error
	encode, decode, err = paging.Token.Encode, paging.Token.Decode, paging.ErrToken
	var got int64
	if err := decode(token, encode(token, int64(8)), &got); err != nil || got != 8 {
		t.Fatalf("method values: value %d, error %v", got, err)
	}
	if err == nil || err.Error() != paging.BadPage {
		t.Fatalf("ErrToken = %v", err)
	}
}

// R-QVXU-R4H7: DefaultLimit is usable as an untyped integer constant.
// R-QX5R-4W7W: MaxLimit is usable as an untyped integer constant.
func TestLimitConstants(t *testing.T) {
	const defaultByte byte = paging.DefaultLimit
	const maxUnsigned uint16 = paging.MaxLimit
	const defaultString = string(rune(paging.DefaultLimit))
	const maxString = string(rune(paging.MaxLimit))
	if defaultByte != 25 || maxUnsigned != 500 || defaultString != string(rune(25)) || maxString != string(rune(500)) {
		t.Fatal("limit constants have incorrect values")
	}
}

// R-QYDN-INYL: Each exported copy name is usable as a string constant.
// R-RFG8-VGCB: All copy constants are nonempty and safe in literal tags.
func TestCopyConstants(t *testing.T) {
	const badPage string = paging.BadPage
	const badLimit string = paging.BadLimit
	const pageDescription string = paging.PageDescription
	const limitDescription string = paging.LimitDescription
	const nextDescription string = paging.NextDescription
	for i, value := range []string{badPage, badLimit, pageDescription, limitDescription, nextDescription} {
		if value == "" {
			t.Fatalf("copy constant %d is empty", i)
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f || strings.ContainsRune("\"`\\", r) {
				t.Fatalf("copy constant %d has forbidden rune %U", i, r)
			}
		}
	}
}

func requirePanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("programmer error did not panic")
		}
	}()
	fn()
}

// R-QZLJ-WFPA: Only nonempty keys of the exact supported types are accepted.
func TestEncodeProgrammerErrors(t *testing.T) {
	type namedInt int64
	type namedString string
	var integer int64
	cases := [][]any{nil, {int(1)}, {int32(1)}, {namedInt(1)}, {namedString("key")}, {&integer}, {nil}, {float64(1)}, {int64(2), nil}}
	for i, fields := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			requirePanic(t, func() { paging.Token{Tool: "test"}.Encode(fields...) })
		})
	}
}

// R-R21C-NZ6O: Destination validation precedes token validation.
func TestDecodeProgrammerErrors(t *testing.T) {
	type namedInt int64
	type namedString string
	var integer int64
	var namedInteger namedInt
	var namedText namedString
	var nilInteger *int64
	var nilText *string
	cases := [][]any{nil, {nil}, {nilInteger}, {nilText}, {integer}, {new(int)}, {new(int32)}, {&namedInteger}, {&namedText}, {&integer, nil}}
	token := paging.Token{Tool: "test"}
	for i, into := range cases {
		for j, encoded := range []string{"", "invalid!", token.Encode(int64(1))} {
			t.Run(fmt.Sprintf("%d/%d", i, j), func(t *testing.T) {
				requirePanic(t, func() { _ = token.Decode(encoded, into...) })
			})
		}
	}
}

// frame constructs the specified wire checksum independently of Token.Encode.
func frame(tool string, payload []byte) string {
	length := uint64(len(tool))
	if length > math.MaxUint32 {
		panic("fixture tool too long")
	}
	input := binary.BigEndian.AppendUint32(nil, uint32(length))
	input = append(input, tool...)
	input = append(input, payload...)
	raw := append([]byte(nil), payload...)
	raw = binary.BigEndian.AppendUint32(raw, crc32.ChecksumIEEE(input))
	return base64.RawURLEncoding.EncodeToString(raw)
}

// R-R399-1QXD: The payload is the specified ordered typed byte framing.
// R-R4H5-FIO2: The checksum includes the length-prefixed tool and wire encoding.
func TestWireFormat(t *testing.T) {
	cases := []struct {
		tool    string
		fields  []any
		payload []byte
	}{
		{"", []any{int64(0)}, []byte{1, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"list_λ", []any{int64(-2), "é\x00", int64(math.MaxInt64), ""}, []byte{1, 255, 255, 255, 255, 255, 255, 255, 254, 2, 0, 0, 0, 3, 195, 169, 0, 1, 127, 255, 255, 255, 255, 255, 255, 255, 2, 0, 0, 0, 0}},
		{"other", []any{int64(math.MinInt64), "\xff"}, []byte{1, 128, 0, 0, 0, 0, 0, 0, 0, 2, 0, 0, 0, 1, 255}},
	}
	for _, tc := range cases {
		got := paging.Token{Tool: tc.tool}.Encode(tc.fields...)
		if want := frame(tc.tool, tc.payload); got != want {
			t.Fatalf("tool %q encoded %q, want %q", tc.tool, got, want)
		}
	}
}

// R-R5P1-TAER: Canonical tokens recover every corresponding key value.
func TestDecodeRoundTrip(t *testing.T) {
	token := paging.Token{Tool: "read_rows"}
	for _, integer := range []int64{math.MinInt64, -1, 0, 1, math.MaxInt64} {
		for _, text := range []string{"", "東京", "\x00\xff", strings.Repeat("x", 260)} {
			var gotInt, gotLast int64
			var gotText string
			encoded := token.Encode(integer, text, -integer)
			if err := token.Decode(encoded, &gotInt, &gotText, &gotLast); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if gotInt != integer || gotText != text || gotLast != -integer {
				t.Fatalf("round trip = %d, %q, %d", gotInt, gotText, gotLast)
			}
			if token.Encode(gotInt, gotText, gotLast) != encoded {
				t.Fatal("decoded key does not reencode canonically")
			}
		}
	}
	var onlyInt int64
	var onlyText string
	if err := token.Decode(token.Encode(int64(4)), &onlyInt); err != nil || onlyInt != 4 {
		t.Fatalf("single integer: %d, %v", onlyInt, err)
	}
	if err := token.Decode(token.Encode("abc"), &onlyText); err != nil || onlyText != "abc" {
		t.Fatalf("single string: %q, %v", onlyText, err)
	}
}

type invalidCase struct {
	name  string
	token string
}

func invalidTokens(t *testing.T, token paging.Token) []invalidCase {
	t.Helper()
	valid := token.Encode(int64(7), "tail")
	raw, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := append([]byte(nil), raw...)
	corrupted[len(corrupted)-1] ^= 1
	one := token.Encode(int64(7), "a") // 19 bytes: last base64 character has four unused bits.
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, one[len(one)-1])
	trailing := one[:len(one)-1] + string(alphabet[last+1])
	return []invalidCase{
		{"empty", ""}, {"padding", valid + "="}, {"plus", "+" + valid[1:]}, {"slash", "/" + valid[1:]},
		{"trailing_bits", trailing}, {"carriage_return", valid[:2] + "\r" + valid[2:]}, {"line_feed", valid[:2] + "\n" + valid[2:]}, {"outside_alphabet", valid + "!"},
		{"short_zero", base64.RawURLEncoding.EncodeToString(nil)}, {"short_one", base64.RawURLEncoding.EncodeToString([]byte{1})}, {"short_two", base64.RawURLEncoding.EncodeToString([]byte{1, 2})}, {"short_three", base64.RawURLEncoding.EncodeToString([]byte{1, 2, 3})},
		{"other_tool", paging.Token{Tool: "different_tool"}.Encode(int64(7), "tail")},
		{"checksum", base64.RawURLEncoding.EncodeToString(corrupted)},
		{"fewer", token.Encode(int64(7))}, {"more", token.Encode(int64(7), "tail", int64(1))},
		{"wrong_first_kind", token.Encode("head", "tail")}, {"wrong_later_kind", token.Encode(int64(7), int64(8))},
		{"appended", base64.RawURLEncoding.EncodeToString(append(append([]byte(nil), raw...), 0))},
		{"removed", base64.RawURLEncoding.EncodeToString(raw[:len(raw)-1])},
		{"unknown_kind", frame(token.Tool, []byte{3})},
		{"short_integer", frame(token.Tool, []byte{1, 0, 0, 0, 0, 0, 0, 0})},
		{"huge_string", frame(token.Tool, []byte{1, 0, 0, 0, 0, 0, 0, 0, 7, 2, 255, 255, 255, 255})},
		{"short_string_length", frame(token.Tool, []byte{1, 0, 0, 0, 0, 0, 0, 0, 7, 2, 0, 0, 0})},
		{"short_string", frame(token.Tool, []byte{1, 0, 0, 0, 0, 0, 0, 0, 7, 2, 0, 0, 0, 2, 'a'})},
		{"empty_payload", frame(token.Tool, nil)},
	}
}

// R-R6WY-725G: Every refusal returns the sentinel itself and preserves all targets.
// R-J11A-W7NQ: The complete specified malformed-token classes are refused.
// R-J3H3-NR54: Even malformed tokens return without panicking for valid targets.
func TestDecodeRefusals(t *testing.T) {
	token := paging.Token{Tool: "read_rows"}
	for _, tc := range invalidTokens(t, token) {
		t.Run(tc.name, func(t *testing.T) {
			integer, text := int64(99), "unchanged"
			err := token.Decode(tc.token, &integer, &text)
			if any(err) != any(paging.ErrToken) || !errors.Is(err, paging.ErrToken) {
				t.Fatalf("Decode error = %v, want ErrToken itself", err)
			}
			if integer != 99 || text != "unchanged" {
				t.Fatalf("refusal changed targets: %d, %q", integer, text)
			}
		})
	}
}

// R-QUPY-DCQI: Window can be called with its declared function signature.
// R-RAKN-CDDJ: An absent limit uses the default.
// R-RBSJ-Q548: Every valid requested limit is returned unchanged.
// R-RD0G-3WUX: Invalid values return zero and the formatted copy constant.
// R-J4P0-1IVT: The error includes both decimal values without formatting failures.
func TestWindow(t *testing.T) {
	windows := []func(*int) (int, error){paging.Window}
	window := windows[0]
	if got, err := window(nil); got != paging.DefaultLimit || err != nil {
		t.Fatalf("Window(nil) = %d, %v", got, err)
	}
	for limit := 1; limit <= paging.MaxLimit; limit++ {
		if got, err := window(&limit); got != limit || err != nil {
			t.Fatalf("Window(%d) = %d, %v", limit, got, err)
		}
	}
	for _, limit := range []int{math.MinInt, -1, 0, paging.MaxLimit + 1, math.MaxInt} {
		got, err := window(&limit)
		if got != 0 || err == nil {
			t.Fatalf("Window(%d) = %d, %v", limit, got, err)
		}
		want := fmt.Sprintf(paging.BadLimit, 1, paging.MaxLimit, limit)
		if err.Error() != want || !strings.Contains(err.Error(), strconv.Itoa(limit)) || !strings.Contains(err.Error(), strconv.Itoa(paging.MaxLimit)) || strings.Contains(err.Error(), "%!") {
			t.Fatalf("Window(%d) error = %q", limit, err.Error())
		}
	}
}

// exercise calls every exported function/method on success, refusal and panic paths.
func exercise(t *testing.T) string {
	t.Helper()
	token := paging.Token{Tool: "exercise"}
	encoded := token.Encode(int64(-5), "key")
	var integer int64
	var text string
	if err := token.Decode(encoded, &integer, &text); err != nil {
		t.Fatal(err)
	}
	if integer != -5 || text != "key" {
		t.Fatal("incorrect exercise key")
	}
	if err := token.Decode("invalid!", &integer, &text); any(err) != any(paging.ErrToken) {
		t.Fatal(err)
	}
	if got, err := paging.Window(nil); got != paging.DefaultLimit || err != nil {
		t.Fatalf("exercise default = %d, %v", got, err)
	}
	limit := 1
	if got, err := paging.Window(&limit); got != limit || err != nil {
		t.Fatalf("exercise window = %d, %v", got, err)
	}
	limit = 0
	if got, err := paging.Window(&limit); got != 0 || err == nil || err.Error() != fmt.Sprintf(paging.BadLimit, 1, paging.MaxLimit, limit) {
		t.Fatalf("exercise refusal = %d, %v", got, err)
	}
	if paging.ErrToken.Error() != paging.BadPage {
		t.Fatal("exercise sentinel text differs")
	}
	requirePanic(t, func() { token.Encode() })
	requirePanic(t, func() { _ = token.Decode("") })
	return encoded
}

// R-RGO5-9830: API calls never emit to process streams or the default logger.
func TestNoProcessOutput(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "captured"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	oldOut, oldErr, oldLog := os.Stdout, os.Stderr, log.Writer()
	var logged bytes.Buffer
	os.Stdout, os.Stderr = file, file
	log.SetOutput(&logged)
	t.Cleanup(func() { os.Stdout, os.Stderr = oldOut, oldErr; log.SetOutput(oldLog) })
	exercise(t)
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 || logged.Len() != 0 {
		t.Fatalf("process output: %d bytes, logger %q", info.Size(), logged.String())
	}
}

// R-RHW1-MZTP: The full API behaves identically in unrelated working directories.
func TestWorkingDirectoryIndependence(t *testing.T) {
	var want string
	for i := 0; i < 2; i++ {
		directory := t.TempDir()
		t.Chdir(directory)
		got := exercise(t)
		if i == 0 {
			want = got
		} else if got != want {
			t.Fatalf("working directory changed token from %q to %q", want, got)
		}
	}
}

// R-RJ3Y-0RKE: Concurrent shared and copied Tokens match serial calls.
func TestConcurrentUse(t *testing.T) {
	token := paging.Token{Tool: "concurrent"}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		integer, text := int64(i)-32, strconv.Itoa(i)
		want := token.Encode(integer, text)
		limit := i % (paging.MaxLimit + 2)
		wantWindow, wantErr := paging.Window(&limit)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				current := &token
				copyToken := token
				if j%2 == 1 {
					current = &copyToken
				}
				if got := current.Encode(integer, text); got != want {
					t.Errorf("concurrent Encode = %q, want %q", got, want)
				}
				var gotInt int64
				var gotText string
				if err := current.Decode(want, &gotInt, &gotText); err != nil || gotInt != integer || gotText != text {
					t.Errorf("concurrent Decode = %d, %q, %v", gotInt, gotText, err)
				}
				if err := current.Decode("bad!", &gotInt, &gotText); any(err) != any(paging.ErrToken) || gotInt != integer || gotText != text {
					t.Errorf("concurrent refusal = %d, %q, %v", gotInt, gotText, err)
				}
				gotWindow, gotErr := paging.Window(&limit)
				if gotWindow != wantWindow || (gotErr == nil) != (wantErr == nil) {
					t.Errorf("concurrent Window = %d, %v", gotWindow, gotErr)
				}
				if gotErr != nil && wantErr != nil && gotErr.Error() != wantErr.Error() {
					t.Errorf("concurrent Window error = %v, want %v", gotErr, wantErr)
				}
				if got, err := paging.Window(nil); got != paging.DefaultLimit || err != nil {
					t.Errorf("concurrent default = %d, %v", got, err)
				}
			}
		}()
	}
	wg.Wait()
}

// R-J3H3-NR54: Arbitrary input bytes are safe with valid destinations.
// R-R5P1-TAER: Accepted tokens reencode to the original canonical string.
// R-R6WY-725G: All other strings return the sentinel and preserve targets.
func FuzzDecode(f *testing.F) {
	token := paging.Token{Tool: "fuzz"}
	for _, seed := range []string{"", "abc", "\xff\x00", token.Encode(int64(1), ""), token.Encode(int64(-1), "λ")} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		integer, text := int64(314), "original"
		err := token.Decode(encoded, &integer, &text)
		if err == nil {
			if token.Encode(integer, text) != encoded {
				t.Fatal("noncanonical accepted token")
			}
		} else if any(err) != any(paging.ErrToken) || !errors.Is(err, paging.ErrToken) || integer != 314 || text != "original" {
			t.Fatalf("refusal = %d, %q, %v", integer, text, err)
		}
	})
}
