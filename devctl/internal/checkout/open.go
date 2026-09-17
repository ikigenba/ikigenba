package checkout

import (
	"context"
	"fmt"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// Open discovers the checkout containing deps.Dir.
func Open(ctx context.Context, deps seam.Deps) (*Checkout, error) {
	const operation = "git rev-parse --show-toplevel"
	result, err := deps.Exec(ctx, seam.Cmd{
		Path: "git",
		Args: []string{"rev-parse", "--show-toplevel"},
		Dir:  deps.Dir,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}

	root := trimTrailingNewlines(string(result.Stdout))
	if result.ExitCode != 0 || root == "" {
		return nil, &NotInCheckoutError{Dir: deps.Dir}
	}
	return &Checkout{Root: root, Deps: deps}, nil
}

func trimTrailingNewlines(value string) string {
	return strings.TrimRight(value, "\r\n")
}
