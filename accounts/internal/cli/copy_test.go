package cli_test

import (
	"context"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cli"
)

// R-D08W-IJKU
func TestUsageCopy(t *testing.T) {
	const usage string = cli.Usage
	if usage == "" || usage[len(usage)-1] != '\n' {
		t.Fatalf("invalid Usage %q", usage)
	}
	p, out, _ := inert(t, []string{"--help"})
	cli.Run(context.Background(), p)
	if out.String() != usage {
		t.Fatalf("help = %q, want Usage", out.String())
	}
}
