package build_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestCommandStartErrorsNamePathAndAreNotProcessErrors(t *testing.T) {
	// R-70T2-JC34
	for _, path := range []string{"go", "binary", "tar"} {
		t.Run(path, func(t *testing.T) {
			f := newSuite(t)
			startErr := errors.New("could not start")
			exec := f.deps.Exec
			f.deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				if c.Path == path || path == "binary" && filepath.IsAbs(c.Path) {
					return seam.Result{}, startErr
				}
				return exec(ctx, c)
			}
			out, err := f.run(context.Background(), "r1")
			var processError *build.ProcessError
			if err == nil || errors.As(err, &processError) || !errors.Is(err, startErr) || !strings.Contains(err.Error(), path) && path != "binary" || out != "" {
				t.Fatalf("output %q error %v", out, err)
			}
		})
	}
}
