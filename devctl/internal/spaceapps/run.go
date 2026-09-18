package spaceapps

import (
	"context"
	"io"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// Run executes a space restart or logs command.
func Run(context.Context, []string, io.Writer, seam.Deps, string) error {
	return nil
}
