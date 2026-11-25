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

func TestEnsureClaudeKeyRejectsFullCapture(t *testing.T) {
	// R-FKNN-X12D
	const valid = "pub:fixture\nfpr:::::::::" + ClaudeKeyFingerprint + ":\n"
	h := New(Env{
		Root: t.TempDir(),
		Fetch: func(context.Context, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("fixture key")), nil
		},
		Execute: func(_ context.Context, command Command) (Result, error) {
			switch command.Name {
			case "rpm":
				return Result{ExitCode: 1}, nil
			case "chown":
				return Result{}, nil
			case "gpg":
				return Result{Stdout: []byte(valid + strings.Repeat("x", MaxCapture-len(valid)))}, nil
			default:
				t.Fatalf("unexpected command %s %v", command.Name, command.Args)
				return Result{}, nil
			}
		},
	})
	if err := h.ensureClaudeKey(t.Context()); !errors.Is(err, ErrClaudeKey) {
		t.Fatalf("ensureClaudeKey error = %v, want ErrClaudeKey", err)
	}
}
