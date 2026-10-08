package deploy_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/deploy"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const deployHelp = `Usage: devctl deploy <space> <sha|tag>

Build the suite at <sha|tag> as build does, check that the space holds every
secret the release's manifests declare, copy dist/<sha>.tar.xz to the space's
host, unpack it into /opt/ikigenba/releases/<sha>/, and have that release's
opsctl activate it. A tag is the release's label, exactly as typed; a sha
gives none.
`

type runSignature func(context.Context, []string, string, io.Writer, seam.Deps) error

var _ runSignature = deploy.Run

func TestDeployPublicContract(t *testing.T) {
	// R-VOSP-G8A5 R-YR44-RQHN R-O1SV-WGPG
	_ = deploy.UsageError(struct {
		Message string
		Help    string
	}{})
	_ = deploy.MissingSecretsError(struct {
		App   string
		Space string
		Names []string
	}{})
	usage := &deploy.UsageError{Message: "bad", Help: "devctl deploy --help"}
	if usage.Error() != "bad" || usage.Detail() != "see 'devctl deploy --help' for usage" || usage.ExitCode() != 2 || (&deploy.UsageError{}).Detail() != "" {
		t.Fatalf("UsageError contract failed: %#v", usage)
	}
	missing := &deploy.MissingSecretsError{App: "crm", Space: "sbx1", Names: []string{"A", "B"}}
	if missing.Error() != "crm: secrets missing A,B" || missing.Detail() != "run 'devctl secrets push sbx1 crm'" || missing.ExitCode() != 2 {
		t.Fatalf("MissingSecretsError contract failed: %#v", missing)
	}
}
func TestDeployHelpAndGrammarPrecedeExternalAccess(t *testing.T) {
	// R-R9DQ-Q1II R-RALN-3T97 R-RBTJ-HKZW R-RD1F-VCQL R-RE9C-94HA
	for _, args := range [][]string{{"--help"}, {"-h"}, {"sbx1", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz", "--help"}} {
		var stdout bytes.Buffer
		calls := 0
		deps := seam.Deps{Dir: t.TempDir(), Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			calls++
			return cloud.Clients{}, errors.New("called")
		}, Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			calls++
			return seam.Result{}, errors.New("called")
		}}
		if err := deploy.Run(context.Background(), args, "test-version", &stdout, deps); err != nil || stdout.String() != deployHelp || calls != 0 {
			t.Fatalf("help %q = stdout %q err %v calls %d", args, stdout.String(), err, calls)
		}
	}
	for _, test := range []struct {
		args    []string
		message string
	}{
		{nil, "deploy needs <space> and <sha|tag>"},
		{[]string{"sbx1"}, "deploy needs <space> and <sha|tag>"},
		{[]string{"sbx1", "a", "extra"}, "deploy takes only <space> and <sha|tag>"},
		{[]string{"sbx1", "--bad"}, "unknown option '--bad'"},
	} {
		err := deploy.Run(context.Background(), test.args, "test-version", io.Discard, seam.Deps{Dir: t.TempDir(), Exec: failRunner(t), Cloud: failCloud(t)})
		var usage *deploy.UsageError
		if !errors.As(err, &usage) || usage.Message != test.message || usage.Help != "devctl deploy --help" {
			t.Fatalf("Run(%q) = %#v", test.args, err)
		}
	}
}

func failRunner(t *testing.T) seam.Runner {
	t.Helper()
	return func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Fatal("unexpected process call")
		return seam.Result{}, nil
	}
}
func failCloud(t *testing.T) cloud.Opener {
	t.Helper()
	return func(context.Context, string, string) (cloud.Clients, error) {
		t.Fatal("unexpected cloud call")
		return cloud.Clients{}, nil
	}
}
