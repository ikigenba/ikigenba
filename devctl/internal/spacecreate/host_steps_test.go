package spacecreate

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func testLaunch(f *createFake) Launch {
	return Launch{InstanceID: "i-new", SHA: createSHA, Label: "release-87", File: filepath.Join(f.root, "dist", createSHA+".tar.xz"), Manifests: []checkout.Manifest{{App: "auth"}, {App: "dummy"}, {App: "events"}, {App: "mcp"}, {App: "repos"}, {App: "scripts"}, {App: "sites"}, {App: "telemetry"}}, Config: hostsetup.Config{Root: "ikigenba.dev", Region: "us-east-2", ZoneID: "ZONE1", Space: resultSpace(), Opsctl: release.Opsctl(createSHA)}, KnownHosts: filepath.Join(f.tmp, "known_hosts.launch"), RequireHostBackup: true}
}

func TestSharedHostPathReadsEveryHostPageAndOnePagePerApp(t *testing.T) {
	// R-32Q2-YEH8 R-3OO9-U9TQ R-O101-XO9A R-O73J-UIYR R-43B2-FIQ2 R-O8BG-8APG R-4AMG-Q568 R-DYY1-CBDS
	_ = SetUpHost
	_ = FinishHost
	_ = Resolve
	for _, reverse := range []bool{false, true} {
		f := newCreateFake(t)
		l := testLaunch(f)
		var listed []string
		f.list = func(_ context.Context, bucket, prefix string, each func([]cloud.Object) bool) error {
			if bucket != l.Config.Root {
				t.Fatal(bucket)
			}
			listed = append(listed, prefix)
			switch {
			case strings.HasSuffix(prefix, "/host/"):
				pages := [][]cloud.Object{{{Key: prefix + "2026-09-12T14:22:51.865067548Z.tar.zst"}}, {{Key: prefix + "2026-09-11T14:22:51.48120933Z.tar.zst"}, {Key: prefix + "2026-09-12T14:22:51.8650675Z.tar.zst"}, {Key: prefix + "2026-09-12T14:22:51.865067548Z.accounts.tar.zst"}}}
				if reverse {
					pages[0], pages[1] = pages[1], pages[0]
				}
				for _, page := range pages {
					if !each(page) {
						t.Fatal("stopped host listing")
					}
				}
			case strings.Contains(prefix, "/sites/"):
				if each(nil) {
					t.Fatal("requested another app page")
				}
			default:
				if each([]cloud.Object{{Key: prefix + "first"}}) {
					t.Fatal("requested second app page")
				}
			}
			return nil
		}
		var out bytes.Buffer
		r, err := SetUpHost(t.Context(), &out, f.deps(), f, f, l)
		if err != nil || !r.Backup || r.Host.Domain != l.Config.Space.Domain || r.Host.KnownHosts != "" {
			t.Fatalf("%#v %v", r, err)
		}
		if err := FinishHost(t.Context(), &out, f.deps(), f, l, r); err != nil {
			t.Fatal(err)
		}
		want := "host: ok (status checks passed, cloud-init done)\ncopy: ok (" + createSHA + ".tar.xz -> sbx1.ikigenba.dev)\nunpack: ok (/opt/ikigenba/releases/" + createSHA + ")\nopsctl: ok (5 keys set)\nrestore: ok (host/2026-09-12T14:22:51.865067548Z.tar.zst, 5 keys set again)\ninit: ok\n"
		for _, m := range l.Manifests {
			detail := "opsctl restore " + m.App
			if m.App == "sites" {
				detail = "no backup of sites"
			}
			want += "restore: ok (" + detail + ")\n"
		}
		want += "activate output\n"
		if out.String() != want || len(listed) != 9 {
			t.Fatalf("out=%q lists=%v", out.String(), listed)
		}
		restored := false
		for _, c := range f.commands {
			if c.Path != "ssh" && c.Path != "scp" {
				t.Fatal(c)
			}
			args := strings.Join(c.Args, " ")
			if !strings.Contains(args, "ec2-user@sbx1.ikigenba.dev") || strings.Contains(args, "host.apex") {
				t.Fatal(c)
			}
			if strings.Contains(args, "UserKnownHostsFile=") == restored {
				t.Fatal(c)
			}
			if strings.Contains(args, "'host' 'restore'") {
				restored = true
			}
		}
	}
}

func TestSharedHostPathMissingBackupAndListingFailure(t *testing.T) {
	// R-O27Y-BFZZ R-355V-PXYM
	for _, key := range []string{"", "sbx1/host/2026-11-08T18:29:41.222933531Z.accounts.tar.zst"} {
		f := newCreateFake(t)
		l := testLaunch(f)
		if key != "" {
			f.objects = []cloud.Object{{Key: key}}
		}
		var out bytes.Buffer
		r, err := SetUpHost(t.Context(), &out, f.deps(), f, f, l)
		var missing *NoHostBackupError
		if !errors.As(err, &missing) || missing.Bucket != "ikigenba.dev" || missing.Prefix != "sbx1/host/" || missing.ExitCode() != 1 || missing.Error() != "restore: no host backup under s3://ikigenba.dev/sbx1/host/" || r.Backup || r.Host.Domain != "" || !strings.HasSuffix(out.String(), "opsctl: ok (5 keys set)\n") {
			t.Fatalf("%#v %q %v", r, out.String(), err)
		}
	}
	f := newCreateFake(t)
	sentinel := errors.New("listing failed")
	f.list = func(context.Context, string, string, func([]cloud.Object) bool) error { return sentinel }
	var out bytes.Buffer
	r, err := SetUpHost(t.Context(), &out, f.deps(), f, f, testLaunch(f))
	if !errors.Is(err, sentinel) || r.Host.Domain != "" || strings.Contains(out.String(), "restore:") {
		t.Fatalf("%#v %q %v", r, out.String(), err)
	}
}

func TestResolveIsOnlyDNSAndOneStep(t *testing.T) {
	// R-3JSO-B6UY
	for _, success := range []bool{true, false} {
		calls := 0
		deps := seam.Deps{After: immediateAfter, LookupHost: func(domain string) ([]string, error) {
			calls++
			if domain != "sbx1.ikigenba.dev" {
				t.Fatal(domain)
			}
			if success {
				return []string{"18.118.7.42"}, nil
			}
			return nil, errors.New("not resolved")
		}, Exec: func(context.Context, seam.Cmd) (seam.Result, error) { t.Fatal("Exec"); return seam.Result{}, nil }, Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			t.Fatal("Stream")
			return seam.Result{}, nil
		}}
		var out bytes.Buffer
		err := Resolve(t.Context(), &out, deps, "sbx1.ikigenba.dev", "18.118.7.42")
		if success {
			if err != nil || calls != 1 || out.String() != "resolve: ok (sbx1.ikigenba.dev -> 18.118.7.42)\n" {
				t.Fatalf("%q %v calls=%d", out.String(), err, calls)
			}
		} else {
			var resolve *space.ResolveError
			if !errors.As(err, &resolve) || resolve.Domain != "sbx1.ikigenba.dev" || resolve.Address != "18.118.7.42" || out.Len() != 0 || calls != space.PollAttempts {
				t.Fatalf("%q %v calls=%d", out.String(), err, calls)
			}
		}
	}
}

func TestKnownHostsUsesInjectedHomeAndStat(t *testing.T) {
	// R-ONQA-V0EN R-OOY7-8S5C R-3X7K-IO0L R-9JPE-20LM R-36DS-3PPB R-9H9L-AH48
	sentinel := errors.New("exec failed")
	for _, tc := range []struct {
		name      string
		backup    bool
		statErr   error
		status    int
		execErr   error
		wantCalls int
		wantError bool
	}{{name: "missing", statErr: fmtWrap(fs.ErrNotExist)}, {name: "present", wantCalls: 1}, {name: "restored", backup: true}, {name: "permission", statErr: fmtWrap(fs.ErrPermission), wantError: true}, {name: "failed", status: 255, wantCalls: 1, wantError: true}, {name: "exec", execErr: sentinel, wantCalls: 1, wantError: true}} {
		t.Run(tc.name, func(t *testing.T) {
			stats, calls := 0, 0
			var out bytes.Buffer
			deps := seam.Deps{Dir: "/checkout", Getenv: func(key string) string {
				if key != "HOME" {
					t.Fatal(key)
				}
				return "/home/dev"
			}, Stat: func(path string) (os.FileInfo, error) {
				stats++
				if path != "/home/dev/.ssh/known_hosts" {
					t.Fatal(path)
				}
				return nil, tc.statErr
			}, Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
				calls++
				if !reflect.DeepEqual(c, seam.Cmd{Path: "ssh-keygen", Args: []string{"-R", "sbx1.ikigenba.dev"}, Dir: "/checkout"}) {
					t.Fatal(c)
				}
				return seam.Result{ExitCode: tc.status, Stdout: []byte("discard"), Stderr: []byte("permission denied\n")}, tc.execErr
			}}
			err := knownHosts(t.Context(), &out, deps, "sbx1.ikigenba.dev", tc.backup)
			wantStats := 1
			if tc.backup {
				wantStats = 0
			}
			if calls != tc.wantCalls || stats != wantStats {
				t.Fatalf("calls=%d stats=%d", calls, stats)
			}
			if tc.wantError {
				if err == nil || out.Len() != 0 {
					t.Fatalf("%q %v", out.String(), err)
				}
				if tc.execErr != nil && !errors.Is(err, sentinel) {
					t.Fatal(err)
				}
				if tc.status != 0 {
					var kh *KnownHostsError
					if !errors.As(err, &kh) || kh.Status != 255 || kh.Domain != "sbx1.ikigenba.dev" || kh.Stderr != "permission denied\n" || kh.Error() != "known_hosts: ssh-keygen -R sbx1.ikigenba.dev: exit status 255" || kh.Detail() != seam.QuoteOutput(kh.Stderr) || kh.ExitCode() != 1 {
						t.Fatal(err)
					}
				}
				if tc.statErr != nil {
					var coded interface{ ExitCode() int }
					if !errors.Is(err, fs.ErrPermission) || !errors.As(err, &coded) || coded.ExitCode() != 1 {
						t.Fatal(err)
					}
				}
			} else {
				detail := "removed sbx1.ikigenba.dev"
				if tc.backup {
					detail = "kept"
				}
				if err != nil || out.String() != "known_hosts: ok ("+detail+")\n" {
					t.Fatalf("%q %v", out.String(), err)
				}
			}
		})
	}
}
func fmtWrap(err error) error { return &os.PathError{Op: "stat", Path: "injected", Err: err} }
