package build

import (
	"context"
	"io"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// run is replaced by the build core implementation in the next D08 phase.
func run(_ context.Context, _ string, _ io.Writer, _ seam.Deps) error {
	return nil
}
