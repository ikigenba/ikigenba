package paging_test

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/paging"
)

// R-J3H3-NR54: Valid destinations cannot panic even for an oversized tool name.
// R-R6WY-725G: An unrepresentable tool binding refuses without changing targets.
func TestDecodeOversizedTool(t *testing.T) {
	if strconv.IntSize < 64 {
		// Such a string cannot exist on a process with 32-bit address lengths.
		return
	}
	length := uint64(math.MaxUint32) + 1
	if length > uint64(math.MaxInt) {
		t.Fatal("address length cannot represent fixture")
	}
	token := paging.Token{Tool: strings.Repeat("\x00", int(length))}
	encoded := paging.Token{Tool: "ordinary"}.Encode(int64(7), "key")
	integer, text := int64(99), "unchanged"
	err := token.Decode(encoded, &integer, &text)
	if any(err) != any(paging.ErrToken) || !errors.Is(err, paging.ErrToken) || integer != 99 || text != "unchanged" {
		t.Fatalf("oversized binding refusal = %d, %q, %v", integer, text, err)
	}
}
