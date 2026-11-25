package linux

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

func TestDownloadClaudeKeyReportsSizeError(t *testing.T) {
	// R-SSM0-A1YK
	h := New(Env{
		Root: t.TempDir(),
		Fetch: func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(strings.Repeat("a", MaxKeyBytes+1))), nil
		},
	})
	if err := h.downloadClaudeKey(t.Context()); !errors.Is(err, machine.ErrTooLarge) {
		t.Fatalf("downloadClaudeKey error = %v, want ErrTooLarge", err)
	}
}
