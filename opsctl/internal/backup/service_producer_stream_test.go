package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type serviceStreamResult struct {
	size    int64
	failure error
}

func runServiceStream(ctx context.Context, t *testing.T, root, kind string, execute func(context.Context, host.Command) (host.Result, error), client cloud.Client) ([]serviceStreamResult, error) {
	t.Helper()
	store := configuredFileStore(t, root)
	env := fileHostEnv(root, execute)
	cloudEnv := cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }}
	var results []serviceStreamResult
	if kind == "files" {
		rows, err := backup.Files(ctx, env, cloudEnv, store, "")
		for _, row := range rows {
			results = append(results, serviceStreamResult{row.Size, row.Err})
		}
		return results, err
	}
	rows, err := backup.Snapshot(ctx, env, cloudEnv, store, "")
	for _, row := range rows {
		results = append(results, serviceStreamResult{row.Size, row.Err})
	}
	return results, err
}

func TestServiceArchivesStreamWithBoundedLeadAndLateReads(t *testing.T) {
	// R-0MRJ-B98V R-8XMV-MX66 R-3Y09-344L R-0HVX-S6A3
	for _, kind := range []string{"files", "snapshot", "snapshot-db"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "var/opt/ikigenba/crm/state/a-large", strings.Repeat("a", 5*1048576), 0o600)
			writeFile(t, root, "var/opt/ikigenba/crm/state/z-later", "before", 0o600)
			if kind == "snapshot-db" {
				writeFile(t, root, "opt/crm/etc/manifest.toml", "[database]\nengine = \"sqlite\"\npath = \"state/app.db\"\n", 0o600)
			}
			executor := &fileExecutor{unmapped: true}
			var active atomic.Bool
			var yielded, written atomic.Int64
			var temporary string
			var expected []byte
			var uploads int
			filesystem, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = filesystem.Close() }()
			mutate := func(name string, offset int64) error {
				relative, err := filepath.Rel(root, name)
				if err != nil {
					return err
				}
				file, err := filesystem.OpenFile(relative, os.O_WRONLY, 0)
				if err != nil {
					return err
				}
				_, err = file.WriteAt([]byte("after!"), offset)
				return errors.Join(err, file.Close())
			}
			execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
				if cmd.Name == "litestream" {
					if cmd.Args[0] == "ltx" {
						return host.Result{Stdout: []byte("[{}]")}, nil
					}
					temporary = cmd.Args[2]
					return host.Result{}, os.WriteFile(temporary, bytes.Repeat([]byte("d"), 5*1048576), 0o600)
				}
				if cmd.Name != "zstd" {
					return executor.execute(ctx, cmd)
				}
				if strings.Join(cmd.Args, " ") != "--quiet --stdout" || cmd.Stdin == nil || cmd.Stdout == nil {
					return host.Result{}, errors.New("wrong compression command")
				}
				active.Store(true)
				defer active.Store(false)
				var emitted bytes.Buffer
				write := func(p []byte) error {
					n, err := cmd.Stdout.Write(p)
					emitted.Write(p[:n])
					if lead := written.Add(int64(n)) - yielded.Load(); lead > 1048576 {
						return fmt.Errorf("output lead %d exceeds bound", lead)
					}
					return err
				}
				if err := write([]byte{0x28, 0xb5, 0x2f, 0xfd}); err != nil {
					return host.Result{}, err
				}
				prefix := make([]byte, 4096)
				if _, err := io.ReadFull(cmd.Stdin, prefix); err != nil {
					return host.Result{}, err
				}
				if err := mutate(filepath.Join(root, "var/opt/ikigenba/crm/state/a-large"), 2*1048576); err != nil {
					return host.Result{}, err
				}
				if err := mutate(filepath.Join(root, "var/opt/ikigenba/crm/state/z-later"), 0); err != nil {
					return host.Result{}, err
				}
				if temporary != "" {
					if err := mutate(temporary, 2*1048576); err != nil {
						return host.Result{}, err
					}
				}
				if err := write(prefix); err != nil {
					return host.Result{}, err
				}
				buf := make([]byte, 4*1048576)
				chunks := 0
				for {
					n, err := io.ReadFull(cmd.Stdin, buf)
					chunks++
					if temporary != "" && chunks == 2 {
						// zstd is now inside the database member. Its tail is
						// still more than 1 MiB beyond the input consumed.
						if changeErr := mutate(temporary, 5*1048576-6); changeErr != nil {
							return host.Result{}, changeErr
						}
					}
					if n > 0 {
						if writeErr := write(buf[:n]); writeErr != nil {
							return host.Result{}, writeErr
						}
					}
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
						expected = emitted.Bytes()
						return host.Result{}, nil
					}
					if err != nil {
						return host.Result{}, err
					}
				}
			}
			client := producerCloud{put: func(_ context.Context, _ string, body io.Reader) error {
				uploads++
				if !active.Load() {
					return errors.New("upload did not overlap zstd")
				}
				var received bytes.Buffer
				buf := make([]byte, 4096)
				for {
					n, err := body.Read(buf)
					yielded.Add(int64(n))
					received.Write(buf[:n])
					if errors.Is(err, io.EOF) {
						if active.Load() {
							return errors.New("EOF before Execute returned")
						}
						break
					}
					if err != nil {
						return err
					}
				}
				if !bytes.Equal(received.Bytes(), expected) {
					return errors.New("compressed output changed")
				}
				reader := tar.NewReader(bytes.NewReader(received.Bytes()[4:]))
				found := map[string]bool{}
				for {
					header, err := reader.Next()
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						return err
					}
					data, err := io.ReadAll(reader)
					if err != nil {
						return err
					}
					offset := 2 * 1048576
					switch header.Name {
					case "state/z-later":
						offset = 0
					case "state/a-large", "state/app.db":
					default:
						continue
					}
					if len(data) < offset+6 || string(data[offset:offset+6]) != "after!" {
						return fmt.Errorf("member %s was read early", header.Name)
					}
					if header.Name == "state/app.db" && string(data[len(data)-6:]) != "after!" {
						return errors.New("database tail was read before zstd reached it")
					}
					found[header.Name] = true
				}
				if !found["state/a-large"] || !found["state/z-later"] || (kind == "snapshot-db" && !found["state/app.db"]) {
					return fmt.Errorf("missing members: %v", found)
				}
				return nil
			}}
			results, err := runServiceStream(context.Background(), t, root, kind, execute, client)
			if err != nil || len(results) != 1 || results[0].failure != nil || uploads != 1 || results[0].size != int64(len(expected)) {
				t.Fatalf("results %+v, %v; uploads %d, bytes %d", results, err, uploads, len(expected))
			}
			if temporary != "" {
				if _, err := os.Stat(filepath.Dir(temporary)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("temporary remains: %v", err)
				}
			}
		})
	}
}

func TestServiceArchiveFailureNeverCommitsStreamingBody(t *testing.T) {
	// R-902O-EGNK R-0MRJ-B98V
	for _, kind := range []string{"files", "snapshot"} {
		for _, failure := range []string{"read", "zstd", "zstd-exit", "tar", "magic", "short", "interrupted", "upload", "collision"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				writeFile(t, root, "var/opt/ikigenba/alpha/state/a-large", strings.Repeat("a", 2*1048576), 0o600)
				writeFile(t, root, "var/opt/ikigenba/alpha/state/z-later", "later", 0o600)
				writeFile(t, root, "var/opt/ikigenba/beta/state/value", "beta", 0o600)
				executor := &fileExecutor{unmapped: true}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				primary := errors.New("process failed")
				uploadFailure := errors.New("upload refused")
				if failure == "collision" {
					uploadFailure = cloud.ErrAlreadyExists
				}
				var calls, uploads int
				var terminal error
				execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
					if cmd.Name != "zstd" {
						return executor.execute(ctx, cmd)
					}
					calls++
					magic := []byte{0x28, 0xb5, 0x2f, 0xfd}
					if calls == 1 && failure == "magic" {
						magic = []byte("bad!")
					}
					if calls == 1 && failure == "short" {
						magic = magic[:3]
					}
					if _, err := cmd.Stdout.Write(magic); err != nil {
						return host.Result{}, err
					}
					if calls == 1 {
						switch failure {
						case "zstd":
							return host.Result{ExitCode: 9}, primary
						case "zstd-exit":
							return host.Result{ExitCode: 9}, nil
						case "tar":
							return host.Result{}, nil
						case "interrupted":
							cancel()
						case "read":
							prefix := make([]byte, 4096)
							if _, err := io.ReadFull(cmd.Stdin, prefix); err != nil {
								return host.Result{}, err
							}
							if _, err := cmd.Stdout.Write(prefix); err != nil {
								return host.Result{}, err
							}
							if err := os.Rename(filepath.Join(root, "var/opt/ikigenba/alpha/state/z-later"), filepath.Join(root, "removed")); err != nil {
								return host.Result{}, err
							}
						}
					}
					_, err := io.Copy(cmd.Stdout, cmd.Stdin)
					return host.Result{}, err
				}
				client := producerCloud{put: func(_ context.Context, _ string, body io.Reader) error {
					uploads++
					if uploads == 1 && (failure == "upload" || failure == "collision") {
						return uploadFailure
					}
					_, err := io.Copy(io.Discard, body)
					if uploads == 1 {
						terminal = err
					}
					if err != nil {
						return errors.New("uploader hides producer failure")
					}
					return nil
				}}
				results, err := runServiceStream(ctx, t, root, kind, execute, client)
				if len(results) == 0 || results[0].failure == nil || results[0].size != 0 {
					t.Fatalf("results %+v, %v", results, err)
				}
				got := results[0].failure
				if failure == "interrupted" {
					if !errors.Is(err, context.Canceled) || len(results) != 1 {
						t.Fatalf("interruption results %+v, %v", results, err)
					}
				} else if err != nil || len(results) != 2 || results[1].failure != nil {
					t.Fatalf("continuation results %+v, %v", results, err)
				}
				if failure == "upload" || failure == "collision" {
					if !errors.Is(got, uploadFailure) {
						t.Fatalf("lost upload error: %v", got)
					}
				} else if terminal == nil || errors.Is(terminal, io.EOF) || strings.Contains(got.Error(), "uploader hides") {
					t.Fatalf("terminal %v, result %v", terminal, got)
				}
				if failure == "zstd" && !errors.Is(got, primary) {
					t.Fatalf("lost process failure: %v", got)
				}
				if failure == "zstd-exit" {
					var ce *host.CommandError
					if !errors.As(got, &ce) || ce.Result.ExitCode != 9 {
						t.Fatalf("lost exit: %v", got)
					}
				}
				if failure == "read" && !strings.Contains(got.Error(), "/var/opt/ikigenba/alpha/state/z-later: no such file") {
					t.Fatalf("lost canonical read failure: %v", got)
				}
			})
		}
	}
}

func TestServiceArchiveJoinsProcessesBeforeContinuing(t *testing.T) {
	// R-3Z85-GVVA R-902O-EGNK
	for _, kind := range []string{"files", "snapshot"} {
		for _, failure := range []string{"collision", "read"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				root := t.TempDir()
				writeFile(t, root, "var/opt/ikigenba/alpha/state/a-large", strings.Repeat("a", 2*1048576), 0o600)
				writeFile(t, root, "var/opt/ikigenba/alpha/state/z-later", "later", 0o600)
				writeFile(t, root, "var/opt/ikigenba/beta/state/value", "beta", 0o600)
				executor := &fileExecutor{unmapped: true}
				held, release := make(chan struct{}), make(chan struct{})
				var calls atomic.Int64
				execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
					if cmd.Name != "zstd" {
						return executor.execute(ctx, cmd)
					}
					first := calls.Add(1) == 1
					_, err := cmd.Stdout.Write([]byte{0x28, 0xb5, 0x2f, 0xfd})
					if err == nil && first && failure == "read" {
						prefix := make([]byte, 4096)
						_, err = io.ReadFull(cmd.Stdin, prefix)
						if err == nil {
							err = os.Rename(filepath.Join(root, "var/opt/ikigenba/alpha/state/z-later"), filepath.Join(root, "removed"))
						}
					}
					if err == nil {
						_, err = io.Copy(cmd.Stdout, cmd.Stdin)
					}
					if first {
						close(held)
						<-release
					}
					return host.Result{}, err
				}
				var uploads atomic.Int64
				client := producerCloud{put: func(_ context.Context, _ string, body io.Reader) error {
					if uploads.Add(1) == 1 && failure == "collision" {
						return cloud.ErrAlreadyExists
					}
					_, err := io.Copy(io.Discard, body)
					return err
				}}
				type outcome struct {
					rows []serviceStreamResult
					err  error
				}
				finished := make(chan outcome, 1)
				go func() {
					rows, err := runServiceStream(context.Background(), t, root, kind, execute, client)
					finished <- outcome{rows, err}
				}()
				<-held
				select {
				case got := <-finished:
					t.Fatalf("returned with process held: %+v", got)
				default:
				}
				if calls.Load() != 1 || uploads.Load() != 1 {
					t.Fatalf("later service started while held: processes %d, uploads %d", calls.Load(), uploads.Load())
				}
				close(release)
				got := <-finished
				if got.err != nil || len(got.rows) != 2 || got.rows[0].failure == nil || got.rows[1].failure != nil {
					t.Fatalf("result %+v", got)
				}
			})
		}
	}
}
