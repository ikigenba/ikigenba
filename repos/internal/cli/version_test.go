package cli_test

import (
	"bytes"
	"context"
	"regexp"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/cli"
)

// R-S7X1-EMW0 R-S94X-SEMP
func TestVersionVariableAndSyntax(t *testing.T) {
	api := struct{ Version *string }{Version: &cli.Version}
	pattern := `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`
	if !regexp.MustCompile(pattern).MatchString(*api.Version) {
		t.Fatalf("Version = %q is not v followed by a semantic version", *api.Version)
	}
	original := *api.Version
	t.Cleanup(func() { *api.Version = original })
	*api.Version = "v12.34.56-rc.7+build.8"
	var out, diagnostic bytes.Buffer
	if code := cli.Run(context.Background(), cli.Process{Args: []string{"--version"}, Stdout: &out, Stderr: &diagnostic, Dir: t.TempDir()}); code != cli.ExitSuccess {
		t.Fatalf("Run(--version) = %d", code)
	}
	if out.String() != *api.Version+"\n" || diagnostic.Len() != 0 {
		t.Fatalf("version output = %q, stderr = %q", out.String(), diagnostic.String())
	}
}
