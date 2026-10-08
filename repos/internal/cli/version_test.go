package cli_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/cli"
)

// R-KMH0-K217 R-KL94-6AAI
func TestInjectedDisplay(t *testing.T) {
	for _, display := range []string{"", "fixture display <&>", "first\nsecond"} {
		var out, diagnostic bytes.Buffer
		p := untouchedProcess([]string{"--version"}, &out, &diagnostic, t.TempDir())
		p.Version = display
		if code := cli.Run(context.Background(), p); code != cli.ExitSuccess || out.String() != display+"\n" || diagnostic.Len() != 0 {
			t.Fatalf("display %q: output %q stderr %q", display, out.String(), diagnostic.String())
		}
	}
}
