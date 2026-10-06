package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/golden"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/seed"
)

const expectedGoldenUsage = `Usage: devctl golden <subcommand> [arguments]

Keep a space's data as a named golden set, one snapshot per app under
golden/<set>/ in the backup bucket, for 'devctl seed' to give to any space.
A golden set carries no secrets.

Subcommands:
  capture <space> <set>   snapshot every app on <space> and make the snapshots golden set <set>

Run 'devctl golden <subcommand> --help' for details.
`

const expectedSeedUsage = `Usage: devctl seed <space> <source>

Give <space> the data of <source>, a golden set or another space. Each app's
newest snapshot in <source> is copied under <space>'s own seed/ prefix and
put back there with 'opsctl restore <app> --from <uri>', which replaces the
app's etc/, state/ and database, and writes its etc/env from <space>'s own
secrets. An app on <space> that <source> holds no snapshot of is left alone.

<source> is a golden set when one has that name, and otherwise the space with
that label. A space's full domain always names the space.
`

type dispatchS3 struct {
	cloud.S3
	calls *[]string
}

func (s *dispatchS3) ListObjects(_ context.Context, bucket, prefix string) ([]cloud.Object, error) {
	*s.calls = append(*s.calls, "list "+bucket+" "+prefix)
	if prefix == "golden/demo/" {
		return []cloud.Object{{Key: prefix + "crm/snapshot.tar.zst"}}, nil
	}
	return nil, nil
}
func (s *dispatchS3) CopyObject(_ context.Context, bucket, source, key string) error {
	*s.calls = append(*s.calls, "copy "+bucket+" "+source+" "+key)
	return nil
}
func (s *dispatchS3) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	*s.calls = append(*s.calls, "delete "+bucket+" "+strings.Join(keys, " "))
	return nil
}

func TestGoldenAndSeedDispatchMatchesSuccessfulPackageRun(t *testing.T) {
	// R-STKP-UCJF R-TD33-YOEJ
	tests := []struct {
		command string
		args    []string
		run     func(context.Context, []string, io.Writer, seam.Deps) error
		want    string
	}{
		{command: "golden", args: []string{"capture", "sbx1", "demo"}, run: golden.Run,
			want: "snapshot: ok (opsctl snapshot, 1 apps)\ncopy: ok (crm -> example.test/golden/demo/crm/snapshot.tar.zst)\nprune: ok (nothing to delete)\n"},
		{command: "seed", args: []string{"sbx1", "demo"}, run: seed.Run,
			want: "source: ok (golden set demo, 1 apps)\ncopy: ok (crm -> example.test/sbx1/seed/crm/snapshot.tar.zst)\nrestore: ok (opsctl restore crm --from s3://example.test/sbx1/seed/crm/snapshot.tar.zst)\nclean: ok (nothing to delete)\n"},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			observe := func(viaCLI bool) (string, []string) {
				var calls []string
				deps := checkoutDeps(t, `{"domain":"example.test","region":"eu-west-1"}`)
				checkoutExec := deps.Exec
				deps.Exec = func(ctx context.Context, cmd seam.Cmd) (seam.Result, error) {
					if cmd.Path == "git" {
						calls = append(calls, "checkout")
						return checkoutExec(ctx, cmd)
					}
					calls = append(calls, "exec "+cmd.Path+" "+strings.Join(cmd.Args, " "))
					if cmd.Path != "ssh" {
						t.Fatalf("unexpected process %#v", cmd)
					}
					logical := strings.Join(cmd.Args, " ")
					if strings.Contains(logical, "'opsctl' 'snapshot'") {
						return seam.Result{Stdout: []byte("crm: ok (s3://example.test/sbx1/snapshots/crm/snapshot.tar.zst, captured)\n")}, nil
					}
					return seam.Result{Stdout: []byte("crm running\n")}, nil
				}
				deps.Cloud = func(_ context.Context, profile, region string) (cloud.Clients, error) {
					calls = append(calls, "cloud "+profile+" "+region)
					return cloud.Clients{STS: &cliSTS{}, EC2: &cliEC2{instances: []cloud.Instance{{ID: "i-target", Space: "sbx1.example.test", State: cloud.StateRunning, Address: "192.0.2.10"}}}, S3: &dispatchS3{calls: &calls}}, nil
				}
				deps.Stream = func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
					t.Fatal("unexpected stream")
					return seam.Result{}, nil
				}
				deps.Getenv = func(string) string { t.Fatal("unexpected environment read"); return "" }
				deps.Now = func() time.Time { t.Fatal("unexpected clock"); return time.Time{} }
				deps.After = func(time.Duration) <-chan time.Time { t.Fatal("unexpected wait"); return nil }
				var stdout, stderr bytes.Buffer
				if viaCLI {
					code := Run(t.Context(), append([]string{test.command}, test.args...), strings.NewReader(""), &stdout, &stderr, deps)
					if code != 0 || stderr.Len() != 0 {
						t.Fatalf("CLI returned %d, stderr %q", code, stderr.String())
					}
				} else if err := test.run(t.Context(), test.args, &stdout, deps); err != nil {
					t.Fatal(err)
				}
				if stdout.String() != test.want {
					t.Fatalf("output %q, want %q", stdout.String(), test.want)
				}
				return stdout.String(), calls
			}
			cliOutput, cliCalls := observe(true)
			directOutput, directCalls := observe(false)
			if cliOutput != directOutput || !reflect.DeepEqual(cliCalls, directCalls) {
				t.Fatalf("CLI %q %q; package %q %q", cliOutput, cliCalls, directOutput, directCalls)
			}
			if len(cliCalls) < 4 {
				t.Fatalf("dispatch made too few calls: %q", cliCalls)
			}
		})
	}
}

func TestCommandsOutsideRootFileSetIgnoreInvalidRootFile(t *testing.T) {
	// R-S2QX-FE85
	fixture := newCLIBuildFixture(t)
	if err := os.MkdirAll(filepath.Join(fixture.root, "infra"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.root, "infra", "terraform.tfvars.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := fixture.deps()
	cloudCalls := 0
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		cloudCalls++
		return cloud.Clients{}, nil
	}
	result := invokeWithDeps(deps, "build", "crm")
	if result.code != 0 || result.stderr != "" || !strings.HasPrefix(result.stdout, "crm/dist/crm-") {
		t.Fatalf("build with invalid root file: %#v", result)
	}
	assertResult(t, invokeWithDeps(deps, "version"), 0, version+"\n", "")
	if cloudCalls != 0 {
		t.Fatalf("non-root-file commands made %d cloud calls", cloudCalls)
	}
}
