package backup_test

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type serviceStreamCloud struct {
	*restoreCloud
	object io.ReadCloser
	gets   int
}

func (c *serviceStreamCloud) GetObject(context.Context, string) (io.ReadCloser, error) {
	c.gets++
	return c.object, nil
}
func (c *serviceStreamCloud) open(context.Context, string) (cloud.Client, error) { return c, nil }

type serviceStreamInput struct {
	active    atomic.Bool
	closed    atomic.Bool
	read      atomic.Int64
	remaining int64
}

func (r *serviceStreamInput) Read(p []byte) (int, error) {
	if !r.active.Load() {
		return 0, errors.New("object read outside Execute")
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), int(r.remaining))
	clear(p[:n])
	r.remaining -= int64(n)
	r.read.Add(int64(n))
	return n, nil
}
func (r *serviceStreamInput) Close() error { r.closed.Store(true); return nil }

type serviceObservedWriter struct {
	output io.Writer
	after  func(int64) error
	size   int64
}

func (w *serviceObservedWriter) Write(p []byte) (int, error) {
	n, err := w.output.Write(p)
	w.size += int64(n)
	if err == nil {
		err = w.after(w.size)
	}
	return n, err
}

type serviceZeroReader struct{}

func (serviceZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
func assertServiceStageGone(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".opsctl-service-restore-") {
			t.Fatalf("stage remains: %s", entry.Name())
		}
	}
}
func TestServiceRestoreStreamsAndPublishesSameFiles(t *testing.T) {
	// R-92IH-604Y R-0YYJ-4YNT R-94Y9-XJMC R-11EB-WI57 R-99TV-GML4 R-12M8-A9VW
	for _, snapshot := range []bool{false, true} {
		t.Run(fmt.Sprint(snapshot), func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, false)
			store := restoreConfiguredStore(t, root)
			if snapshot {
				writeFile(t, root, "opt/notes/etc/manifest.toml", "app='notes'\n[database]\nengine='sqlite'\npath='state/large'\n", 0600)
			}
			const payload = 4 << 20
			input := &serviceStreamInput{remaining: 8 << 20}
			client := &serviceStreamCloud{restoreCloud: restoreClientFor(t, nil), object: input}
			executor := &restoreStageExecutor{t: t, root: root}
			var staged os.FileInfo
			returned := atomic.Bool{}
			execute := func(ctx context.Context, command host.Command) (host.Result, error) {
				if command.Name != "zstd" {
					return executor.execute(ctx, command)
				}
				input.active.Store(true)
				defer input.active.Store(false)
				defer returned.Store(true)
				if strings.Join(command.Args, " ") != "--quiet --decompress --stdout" || command.Stdout == nil {
					return host.Result{}, errors.New("wrong zstd command")
				}
				var buf [4096]byte
				var yielded int64
				for {
					n, err := command.Stdin.Read(buf[:])
					yielded += int64(n)
					if input.read.Load()-yielded > 1048576 {
						return host.Result{}, errors.New("compressed read-ahead")
					}
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						return host.Result{}, err
					}
				}
				observed := &serviceObservedWriter{output: command.Stdout, after: func(size int64) error {
					var info os.FileInfo
					err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if filepath.Base(name) == "large" {
							info, err = entry.Info()
							return err
						}
						if strings.HasPrefix(entry.Name(), ".opsctl-service-restore-") {
							stage, err := entry.Info()
							if err != nil {
								return err
							}
							if stage.Mode().Perm() != 0700 {
								return errors.New("stage not private")
							}
						}
						return nil
					})
					if err != nil {
						return err
					}
					minimum := min(int64(payload), size-1024-1048576)
					if minimum > 0 && (info == nil || info.Size() < minimum) {
						return fmt.Errorf("staged size %v at output %d", info, size)
					}
					if info != nil {
						staged = info
					}
					return nil
				}}
				tw := tar.NewWriter(observed)
				for _, h := range []*tar.Header{{Name: "state/", Typeflag: tar.TypeDir, Mode: 0755, Uid: os.Getuid(), Gid: os.Getgid()}, {Name: "state/large", Typeflag: tar.TypeReg, Mode: 0644, Size: payload, Uid: os.Getuid(), Gid: os.Getgid()}} {
					if err := tw.WriteHeader(h); err != nil {
						return host.Result{}, err
					}
				}
				remaining := int64(payload)
				if snapshot {
					var sqlite [512]byte
					copy(sqlite[:], "SQLite format 3\x00")
					sqlite[18] = 1
					sqlite[19] = 1
					if _, err := tw.Write(sqlite[:]); err != nil {
						return host.Result{}, err
					}
					remaining -= 512
				}
				if _, err := io.CopyN(tw, serviceZeroReader{}, remaining); err != nil {
					return host.Result{}, err
				}
				return host.Result{}, tw.Close()
			}
			from := ""
			if snapshot {
				from = "s3://elsewhere/snapshot"
			}
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: execute}, cloud.Env{Open: client.open}, store, "notes", nil, from, nil, func(context.Context) error { return nil })
			if err != nil {
				t.Fatalf("restore %+v: %v", report, err)
			}
			final, err := os.Lstat(filepath.Join(root, "var/opt/ikigenba/notes/state/large"))
			if err != nil || staged == nil || !os.SameFile(staged, final) || final.Size() != payload || final.Mode().Perm() != 0640 {
				t.Fatalf("publication staged=%v final=%v err=%v", staged, final, err)
			}
			if !returned.Load() || !input.closed.Load() || client.gets != 1 {
				t.Fatalf("lifetime returned=%v closed=%v gets=%d", returned.Load(), input.closed.Load(), client.gets)
			}
			assertServiceStageGone(t, root)
		})
	}
}

func TestServiceRestoreRejectsHeadersWithoutDrainingOutput(t *testing.T) {
	// R-106F-IQEI R-99TV-GML4 R-11EB-WI57
	for _, kind := range []string{"absolute", "traversal", "outside", "extended outside", "duplicate", "ancestor link", "ancestor file", "late non-directory", "device", "unsupported", "directory data", "symlink data"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, false)
			store := restoreConfiguredStore(t, root)
			input := &serviceStreamInput{}
			client := &serviceStreamCloud{restoreCloud: restoreClientFor(t, nil), object: input}
			returned := false
			var accepted int
			report, err := backup.Restore(context.Background(), host.Env{Root: root, Execute: func(_ context.Context, c host.Command) (host.Result, error) {
				if c.Name != "zstd" {
					return host.Result{}, errors.New("unit action before source rejection")
				}
				defer func() { returned = true }()
				tw := tar.NewWriter(c.Stdout)
				base := &tar.Header{Name: "state/bad", Typeflag: tar.TypeReg, Mode: 0600}
				switch kind {
				case "absolute":
					base.Name = "/state/bad"
				case "traversal":
					base.Name = "state/../bad"
				case "outside":
					base.Name = "outside/bad"
				case "extended outside":
					base.Name = strings.Repeat("outside/", 60) + "bad"
				case "duplicate":
					if err := tw.WriteHeader(base); err != nil {
						return host.Result{}, err
					}
				case "ancestor link", "ancestor file":
					h := &tar.Header{Name: "state/parent", Typeflag: tar.TypeReg}
					if kind == "ancestor link" {
						h.Typeflag = tar.TypeSymlink
						h.Linkname = "../../outside"
					}
					if err := tw.WriteHeader(h); err != nil {
						return host.Result{}, err
					}
					base.Name = "state/parent/bad"
				case "late non-directory":
					if err := tw.WriteHeader(&tar.Header{Name: "state/bad/child", Typeflag: tar.TypeReg}); err != nil {
						return host.Result{}, err
					}
				case "device":
					base.Typeflag = tar.TypeChar
				case "unsupported":
					base.Typeflag = tar.TypeLink
					base.Linkname = "state/else"
				case "directory data":
					base.Typeflag = tar.TypeDir
				case "symlink data":
					base.Typeflag = tar.TypeSymlink
				}
				if kind == "directory data" || kind == "symlink data" {
					// tar.Writer refuses malformed non-file sizes; alter the encoded header
					// through a temporary header writer and recompute its tar checksum.
					var header [512]byte
					capture := &serviceHeaderCapture{header: &header}
					writer := tar.NewWriter(capture)
					if err := writer.WriteHeader(base); err != nil {
						return host.Result{}, err
					}
					copy(header[124:136], []byte("00000000001\x00"))
					for i := 148; i < 156; i++ {
						header[i] = ' '
					}
					sum := 0
					for _, v := range header {
						sum += int(v)
					}
					copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
					if _, err := c.Stdout.Write(header[:]); err != nil {
						return host.Result{}, err
					}
				} else if err := tw.WriteHeader(base); err != nil {
					return host.Result{}, err
				}
				// A bad header can end exactly at a Write boundary, so the next write
				// must still observe the parser's rejection and accept a bounded prefix.
				tail := make([]byte, 8<<20)
				n, writeErr := c.Stdout.Write(tail)
				accepted = n
				if writeErr == nil {
					return host.Result{}, errors.New("bad header accepted")
				}
				if n, err := c.Stdout.Write([]byte("later")); n != 0 || err == nil {
					return host.Result{}, errors.New("later write accepted")
				}
				return host.Result{}, writeErr
			}}, cloud.Env{Open: client.open}, store, "notes", nil, "", nil, func(context.Context) error { return nil })
			var commandErr *host.CommandError
			if err == nil || errors.As(err, &commandErr) || len(report.Steps) != 1 || report.Steps[0].Name != "source" || accepted > 1048576 || !returned || !input.closed.Load() {
				t.Fatalf("report=%+v err=%v accepted=%d returned=%v closed=%v", report, err, accepted, returned, input.closed.Load())
			}
			assertServiceStageGone(t, root)
		})
	}
}

type serviceHeaderCapture struct{ header *[512]byte }

func (w *serviceHeaderCapture) Write(p []byte) (int, error) { copy(w.header[:], p); return len(p), nil }

func TestServiceRestoreSourceFailureCleansStageAndJoinsExecutor(t *testing.T) {
	// R-0YYJ-4YNT R-11EB-WI57 R-99TV-GML4 R-92IH-604Y
	for _, kind := range []string{"stage creation", "stage write", "cancellation", "zstd start", "zstd exit", "secrets", "activation marker", "stop", "files", "environment", "db", "litestream", "start", "callback"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			restoreInstalled(t, root, kind == "db" || kind == "litestream")
			store := restoreConfiguredStore(t, root)
			input := &serviceStreamInput{}
			client := &serviceStreamCloud{restoreCloud: restoreClientFor(t, nil), object: input}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			base := &restoreStageExecutor{t: t, root: root, installed: true, active: kind == "start"}
			expectedStep := map[string]string{"stop": "stop", "environment": "files", "db": "db", "litestream": "litestream", "start": "start"}[kind]
			switch kind {
			case "activation marker":
				if err := os.MkdirAll(filepath.Join(root, "run/opsctl/restore/notes.active"), 0700); err != nil {
					t.Fatal(err)
				}
			case "stop":
				base.failCommand = "systemctl stop ikigenba-notes.socket"
			case "db":
				base.failCommand = "litestream ltx -level all -json s3://bucket/host/notes/?region=us-east-2"
			case "start":
				base.failCommand = "systemctl start ikigenba-notes.socket"
			case "litestream":
				if err := store.Set("backup.service_wal_seconds", "invalid"); err != nil {
					t.Fatal(err)
				}
			case "environment":
				if err := serviceStreamChmod(root, "etc/opt/ikigenba/notes", 0500); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = serviceStreamChmod(root, "etc/opt/ikigenba/notes", 0700) }()
			}
			if base.failCommand != "" {
				base.failErr = errors.New("injected " + kind + " failure")
			}
			if kind == "files" {
				base.failCommand = "id --user ikigenba"
				base.failErr = errors.New("account failed")
			}
			if kind == "secrets" {
				writeFile(t, root, "opt/notes/etc/manifest.toml", "app='notes'\nsecrets=['TOKEN']\n", 0600)
			}
			if kind == "stage creation" {
				if err := serviceStreamChmod(root, ".", 0500); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = serviceStreamChmod(root, ".", 0700) }()
			}
			before := restoreTreeSnapshot(t, root)
			var executed, returned bool
			report, err := backup.Restore(ctx, host.Env{Root: root, Execute: func(ctx context.Context, c host.Command) (host.Result, error) {
				if c.Name != "zstd" {
					return base.execute(ctx, c)
				}
				executed = true
				defer func() { returned = true }()
				if kind == "zstd start" {
					return host.Result{}, errors.New("cannot start zstd")
				}
				if kind == "zstd exit" {
					return host.Result{ExitCode: 2}, nil
				}
				if kind == "cancellation" {
					cancel()
					_, err := c.Stdout.Write(make([]byte, 512))
					return host.Result{}, err
				}
				tw := tar.NewWriter(c.Stdout)
				if err := tw.WriteHeader(&tar.Header{Name: "state/", Typeflag: tar.TypeDir, Mode: 0750, Uid: os.Getuid(), Gid: os.Getgid()}); err != nil {
					return host.Result{}, err
				}
				if kind == "stage write" {
					entries, err := os.ReadDir(root)
					if err != nil {
						return host.Result{}, err
					}
					for _, entry := range entries {
						if strings.HasPrefix(entry.Name(), ".opsctl-service-restore-") {
							// The parent may be in flight just after the header Write returns.
							// Making the private stage unwritable also blocks creation of state.
							if err := serviceStreamChmod(root, entry.Name(), 0500); err != nil {
								return host.Result{}, err
							}
							defer func() {
								_ = serviceStreamChmod(root, entry.Name(), 0700)
								_ = serviceStreamChmod(root, filepath.Join(entry.Name(), "state"), 0700)
							}()
							if err := serviceStreamChmod(root, filepath.Join(entry.Name(), "state"), 0500); err != nil && !errors.Is(err, os.ErrNotExist) {
								return host.Result{}, err
							}
						}
					}
				}
				if err := tw.WriteHeader(&tar.Header{Name: "state/value", Typeflag: tar.TypeReg, Size: 1, Mode: 0600, Uid: os.Getuid(), Gid: os.Getgid()}); err != nil {
					return host.Result{}, err
				}
				if _, err := tw.Write([]byte("x")); err != nil {
					return host.Result{}, err
				}
				return host.Result{}, tw.Close()
			}}, cloud.Env{Open: client.open}, store, "notes", nil, "", nil, func(context.Context) error {
				if kind == "callback" {
					return errors.New("callback failed")
				}
				return nil
			})
			if err == nil || len(report.Steps) == 0 || executed && !returned || !input.closed.Load() {
				t.Fatalf("failure %s report=%+v err=%v execute=%v return=%v closed=%v", kind, report, err, executed, returned, input.closed.Load())
			}
			if expectedStep != "" {
				last := report.Steps[len(report.Steps)-1]
				if last.Name != expectedStep || last.Err == nil {
					t.Fatalf("%s did not reach its failure: %+v %v", kind, report, err)
				}
				var failure *backup.RestoreError
				expectedStage := map[string]string{"stop": "stop", "environment": "environment", "db": "litestream restore", "litestream": "litestream regeneration", "start": "start"}[kind]
				if !errors.As(err, &failure) || failure.Stage != expectedStage {
					t.Fatalf("%s failed at wrong stage: %v", kind, err)
				}
			}
			var commandErr *host.CommandError
			if (kind == "zstd start" || kind == "zstd exit") && !errors.As(err, &commandErr) {
				t.Fatalf("zstd error %T %v", err, err)
			}
			if kind == "stage creation" || kind == "stage write" {
				var failure *backup.RestoreError
				if len(report.Steps) != 1 || report.Steps[0].Name != "source" || report.Steps[0].Detail != "" || report.Steps[0].Err == nil || !errors.As(err, &failure) || failure.Stage != "source" || len(failure.Stopped) != 0 || len(base.commands) != 0 {
					t.Fatalf("source-stage failure crossed workflow boundary: %+v %v commands=%v", report, err, base.commands)
				}
				after := restoreTreeSnapshot(t, root)
				if !slices.Equal(before, after) {
					t.Fatalf("source-stage failure changed targets: before=%v after=%v", before, after)
				}
			}
			if kind == "activation marker" {
				var failure *backup.RestoreError
				last := report.Steps[len(report.Steps)-1]
				if !errors.As(err, &failure) || failure.Stage != "activation marker" || last.Name != "secrets" || last.Err != nil {
					t.Fatalf("activation marker injection failed at wrong stage: %+v %v", report, err)
				}
			}
			assertServiceStageGone(t, root)
		})
	}
}

func serviceStreamChmod(rootName, name string, mode os.FileMode) error {
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.Chmod(name, mode)
}
