package release

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const sha = "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"
const remote = "/tmp/tmp.Ab12Cd34Ef"
const temp = ReleasesDir + "/.unpack.Ab12Cd34Ef"
const file = "/w/dist/" + sha + ".tar.xz"

func TestIdentityAndResolution(t *testing.T) {
	// R-WKI3-E9K7 R-WLPZ-S1AW R-WMXW-5T1L R-WO5S-JKSA R-WQLL-B49O R-WRTH-OW0D
	if Folder(sha) != "/opt/ikigenba/releases/"+sha || Opsctl(sha) != Folder(sha)+"/opsctl/bin/opsctl" {
		t.Fatal("paths")
	}
	_ = NotCommitError(struct{ Rev string }{})
	e := &NotCommitError{Rev: "r9"}
	if e.Error() != "'r9' is not a commit" || e.ExitCode() != 2 {
		t.Fatal(e)
	}
	for _, rev := range []string{"r1", "r3-rc1", "auth/v0.18.2", "4B22285", "abc", sha + "0", "4b22", "4b22285", "deadbeef", sha} {
		want := rev
		if rev == "4b22" || rev == "4b22285" || rev == "deadbeef" || rev == sha {
			want = ""
		}
		if Label(rev) != want {
			t.Fatal(rev)
		}
	}
	for _, rev := range []string{"r1", "4b22285", "r9", "main", "HEAD"} {
		calls := 0
		c := &checkout.Checkout{Root: "/root", Deps: seam.Deps{Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
			calls++
			object := rev
			if Label(rev) != "" {
				object = "refs/tags/" + rev
			}
			if !reflect.DeepEqual(cmd.Args, []string{"rev-parse", "--verify", "--quiet", "--end-of-options", object + "^{commit}"}) {
				t.Fatal(cmd)
			}
			if rev == "r9" || rev == "main" || rev == "HEAD" {
				return seam.Result{ExitCode: 1}, nil
			}
			return seam.Result{Stdout: []byte(sha + "\n")}, nil
		}}}
		got, label, err := Resolve(context.Background(), c, rev)
		if rev == "r1" || rev == "4b22285" {
			if err != nil || got != sha || label != Label(rev) {
				t.Fatalf("%s %s %s %v", rev, got, label, err)
			}
		} else {
			var nc *NotCommitError
			if !errors.As(err, &nc) || nc.Rev != rev || got != "" || label != "" {
				t.Fatal(err)
			}
		}
		if calls != 1 {
			t.Fatal(calls)
		}
	}
	cause := errors.New("runner")
	c := &checkout.Checkout{Deps: seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) { return seam.Result{}, cause }}}
	_, _, err := Resolve(context.Background(), c, "r1")
	if !errors.Is(err, cause) || err.Error() != "git rev-parse: runner" {
		t.Fatal(err)
	}
}

type putFake struct {
	t        *testing.T
	commands []string
	fail     int
	present  bool
	invalid  string
}

func (f *putFake) host() host.Host {
	return host.Host{Address: "18.118.7.42", Deps: seam.Deps{Dir: "/w", Exec: func(_ context.Context, c seam.Cmd) (seam.Result, error) {
		logical := c.Args[len(c.Args)-1]
		if c.Path == "scp" {
			logical = "scp"
		}
		f.commands = append(f.commands, logical)
		if f.fail == len(f.commands) {
			return seam.Result{ExitCode: 2, Stderr: []byte("failed\n")}, nil
		}
		if logical == "'mktemp'" {
			return seam.Result{Stdout: []byte(remote + "\n")}, nil
		}
		if strings.Contains(logical, "'test'") {
			if !f.present {
				return seam.Result{ExitCode: 1}, nil
			}
		}
		if strings.Contains(logical, "'mktemp' '-d'") {
			value := temp
			if f.invalid != "" {
				value = f.invalid
			}
			return seam.Result{Stdout: []byte(value + "\n")}, nil
		}
		return seam.Result{}, nil
	}, Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		f.t.Fatal("unexpected stream")
		return seam.Result{}, nil
	}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
		f.t.Fatal("unexpected cloud")
		return cloud.Clients{}, nil
	}}}
}
func TestPutCommands(t *testing.T) {
	// R-WT1E-2NR2 R-WU9A-GFHR R-WVH6-U78G R-WZ4V-ZIGJ
	for _, present := range []bool{false, true} {
		f := putFake{t: t, present: present}
		var out bytes.Buffer
		if err := Put(context.Background(), f.host(), &out, file, sha); err != nil {
			t.Fatal(err)
		}
		want := []string{"'mktemp'", "scp", "'sudo' 'mkdir' '-p' '-m' '0755' '" + ReleasesDir + "'", "'sudo' 'test' '-e' '" + Folder(sha) + "'"}
		if !present {
			want = append(want, "'sudo' 'mktemp' '-d' '"+ReleasesDir+"/.unpack.XXXXXXXXXX'", "'sudo' 'tar' '-x' '-J' '--no-same-owner' '-f' '"+remote+"' '-C' '"+temp+"'", "'sudo' 'mv' '-T' '"+temp+"/"+sha+"' '"+Folder(sha)+"'", "'sudo' 'rmdir' '"+temp+"'")
		}
		want = append(want, "'rm' '-f' '"+remote+"'")
		if !reflect.DeepEqual(f.commands, want) {
			t.Fatalf("commands %#v want %#v", f.commands, want)
		}
		detail := Folder(sha)
		if present {
			detail += " already present, kept"
		}
		if out.String() != "copy: ok ("+sha+".tar.xz -> 18.118.7.42)\nunpack: ok ("+detail+")\n" {
			t.Fatal(out.String())
		}
	}
}
func TestPutFailureCleanup(t *testing.T) {
	// R-WU9A-GFHR R-WWP3-7YZ5 R-WZ4V-ZIGJ
	for fail := 1; fail <= 9; fail++ {
		f := putFake{t: t, fail: fail}
		var out bytes.Buffer
		err := Put(context.Background(), f.host(), &out, file, sha)
		var ce *host.CommandError
		if !errors.As(err, &ce) || ce.Status != 2 || strings.Contains(out.String(), "unpack:") {
			t.Fatalf("fail %d: %v %q", fail, err, out.String())
		}
		if fail <= 2 && out.Len() != 0 {
			t.Fatal(out.String())
		}
		if fail == 1 && len(f.commands) != 1 {
			t.Fatal(f.commands)
		}
		if fail >= 2 && f.commands[len(f.commands)-1] != "'rm' '-f' '"+remote+"'" {
			t.Fatal(f.commands)
		}
		if fail >= 6 && fail <= 8 && f.commands[len(f.commands)-2] != "'sudo' 'rm' '-rf' '"+temp+"'" {
			t.Fatal(f.commands)
		}
	}
	for _, invalid := range []string{"/", ReleasesDir + "/.unpack.", ReleasesDir + "/.unpack.x/y", "/tmp/other"} {
		f := putFake{t: t, invalid: invalid}
		var out bytes.Buffer
		err := Put(context.Background(), f.host(), &out, file, sha)
		if err == nil || !strings.Contains(err.Error(), invalid) {
			t.Fatal(err)
		}
		for _, cmd := range f.commands {
			if strings.Contains(cmd, "'tar'") || strings.Contains(cmd, "'-rf'") {
				t.Fatal(cmd)
			}
		}
		if f.commands[len(f.commands)-1] != "'rm' '-f' '"+remote+"'" {
			t.Fatal(f.commands)
		}
	}
}
func TestActivateStreamsVerbatim(t *testing.T) {
	// R-WT1E-2NR2 R-WXWZ-LQPU R-WZ4V-ZIGJ
	for _, label := range []string{"", "r1"} {
		for _, status := range []int{0, 1} {
			calls := 0
			var out bytes.Buffer
			h := host.Host{Address: "18.118.7.42", Deps: seam.Deps{Dir: "/w", Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
				t.Fatal("unexpected exec")
				return seam.Result{}, nil
			}, Stream: func(_ context.Context, c seam.Cmd, w io.Writer) (seam.Result, error) {
				calls++
				want := "'sudo' '" + Opsctl(sha) + "' 'activate' '" + sha + "'"
				if label != "" {
					want += " '" + label + "'"
				}
				if c.Path != "ssh" || c.Args[len(c.Args)-1] != want {
					t.Fatal(c)
				}
				_, _ = io.WriteString(w, "arbitrary\n\x00bytes")
				return seam.Result{ExitCode: status, Stderr: []byte("opsctl failed\n")}, nil
			}, Cloud: func(context.Context, string, string) (cloud.Clients, error) {
				t.Fatal("cloud")
				return cloud.Clients{}, nil
			}}}
			err := Activate(context.Background(), h, &out, sha, label)
			if calls != 1 || out.String() != "arbitrary\n\x00bytes" {
				t.Fatal(out.String())
			}
			if status == 0 && err != nil {
				t.Fatal(err)
			}
			if status == 1 {
				var ce *host.CommandError
				if !errors.As(err, &ce) || ce.Step != "activate" || ce.Stderr != "opsctl failed\n" {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestCopyEmptyTempAndCleanupFailure(t *testing.T) {
	// R-WU9A-GFHR R-WWP3-7YZ5
	calls := 0
	h := host.Host{Deps: seam.Deps{Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		calls++
		return seam.Result{Stdout: []byte("\r\n")}, nil
	}}}
	var out bytes.Buffer
	err := Put(context.Background(), h, &out, file, sha)
	if err == nil || !strings.Contains(err.Error(), "mktemp") || calls != 1 || out.Len() != 0 {
		t.Fatalf("%v calls %d stdout %q", err, calls, out.String())
	}
	for _, failure := range []string{"copy", "tar"} {
		calls = 0
		f := putFake{t: t}
		h = f.host()
		original := h.Deps.Exec
		h.Deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
			result, err := original(ctx, c)
			calls++
			logical := c.Args[len(c.Args)-1]
			if failure == "copy" && c.Path == "scp" || failure == "tar" && strings.Contains(logical, "'tar'") {
				return seam.Result{ExitCode: 7, Stderr: []byte("first failure")}, nil
			}
			if strings.Contains(logical, "'rm'") {
				return seam.Result{ExitCode: 9, Stderr: []byte("cleanup failure")}, nil
			}
			return result, err
		}
		out.Reset()
		err = Put(context.Background(), h, &out, file, sha)
		var ce *host.CommandError
		if !errors.As(err, &ce) || ce.Status != 7 || ce.Stderr != "first failure" {
			t.Fatalf("%s: %v", failure, err)
		}
	}
}
