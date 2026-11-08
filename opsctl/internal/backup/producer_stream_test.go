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
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

type producerCloud struct {
	cloud.Client
	put func(context.Context, string, io.Reader) error
}

func (c producerCloud) PutObject(ctx context.Context, uri string, body io.Reader) error {
	return c.put(ctx, uri, body)
}

func producerSources(t *testing.T) (string, *accountsMetadataFixture) {
	t.Helper()
	root := t.TempDir()
	emptyBackupAccounts(t, root)
	writeFile(t, root, "etc/passwd", "alice:x:1001:1001::/home/alice:/bin/bash\n", 0o600)
	writeFile(t, root, "etc/group", "shell:x:999:alice\nalice:x:1001:\n", 0o600)
	writeFile(t, root, "etc/shadow", "alice:!:0:0:99999:7:::\n", 0o600)
	for _, tree := range []string{"home/alice", "etc/ikigenba"} {
		writeFile(t, root, tree+"/a-large", strings.Repeat("a", 5*1048576), 0o600)
		writeFile(t, root, tree+"/z-later", "before", 0o600)
	}
	return root, &accountsMetadataFixture{t: t, root: root}
}

func runProducerBackup(ctx context.Context, t *testing.T, root string, execute func(context.Context, host.Command) (host.Result, error), client cloud.Client) (backup.HostBackupResult, error) {
	t.Helper()
	return backup.HostBackup(ctx, host.Env{Root: root, Now: func() time.Time { return time.Unix(10, 0) }, Execute: execute}, cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }}, configuredHostStore(t, t.TempDir()))
}

func TestHostBackupStreamsBothArchivesWithBoundedLead(t *testing.T) {
	// R-P28M-H8ZM R-P3GI-V0QB R-P4OF-8SH0
	root, metadata := producerSources(t)
	var active atomic.Bool
	var yielded atomic.Int64
	var written atomic.Int64
	var calls, uploads int
	var wantOutput []byte
	var sizes []int64
	execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
		if cmd.Name != "zstd" {
			return metadata.execute(ctx, cmd)
		}
		calls++
		if strings.Join(cmd.Args, " ") != "--quiet --stdout" || cmd.Stdin == nil || cmd.Stdout == nil {
			return host.Result{}, errors.New("wrong streaming compression command")
		}
		active.Store(true)
		defer active.Store(false)
		yielded.Store(0)
		written.Store(0)
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
		// Pause zstd after a small prefix and change a member whose contents
		// begin over 1 MiB beyond it. Reading it early would archive "before".
		prefix := make([]byte, 4096)
		n, err := io.ReadFull(cmd.Stdin, prefix)
		if err != nil {
			return host.Result{}, err
		}
		tree := "home/alice"
		if calls == 2 {
			tree = "etc/ikigenba"
		}
		if err := os.WriteFile(filepath.Join(root, tree, "z-later"), []byte("after!"), 0o600); err != nil {
			return host.Result{}, err
		}
		if err := write(prefix[:n]); err != nil {
			return host.Result{}, err
		}
		buf := make([]byte, 4*1048576)
		for {
			n, err := io.ReadFull(cmd.Stdin, buf)
			if n > 0 {
				if writeErr := write(buf[:n]); writeErr != nil {
					return host.Result{}, writeErr
				}
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				wantOutput = emitted.Bytes()
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
			return errors.New("upload did not overlap Execute")
		}
		var received bytes.Buffer
		buf := make([]byte, 1777)
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
		if !bytes.Equal(received.Bytes(), wantOutput) {
			return errors.New("upload changed compressed output")
		}
		sizes = append(sizes, int64(received.Len()))
		tr := tar.NewReader(bytes.NewReader(received.Bytes()[4:]))
		found := false
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			if strings.HasSuffix(h.Name, "/z-later") {
				data, err := io.ReadAll(tr)
				if err != nil {
					return fmt.Errorf("read late member: %w", err)
				}
				if string(data) != "after!" {
					return fmt.Errorf("late member = %q", data)
				}
				found = true
			}
		}
		if !found {
			return errors.New("late member missing")
		}
		return nil
	}}
	result, err := runProducerBackup(context.Background(), t, root, execute, client)
	if err != nil || result.Accounts.Err != nil || result.Host.Err != nil || calls != 2 || uploads != 2 {
		t.Fatalf("backup = %+v, %v; Execute %d, uploads %d", result, err, calls, uploads)
	}
	if result.Accounts.Size != sizes[0] || result.Host.Size != sizes[1] {
		t.Fatalf("sizes = %d, %d; yielded %v", result.Accounts.Size, result.Host.Size, sizes)
	}
}

func TestHostBackupFailureNeverCommitsStreamingBody(t *testing.T) {
	// R-P5WB-MK7P R-P28M-H8ZM
	for _, target := range []int{1, 2} {
		for _, failure := range []string{"read", "zstd", "zstd-exit", "tar", "magic", "short", "interrupted", "upload", "collision"} {
			t.Run(fmt.Sprintf("archive%d/%s", target, failure), func(t *testing.T) {
				root, metadata := producerSources(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				primary := errors.New("zstd process failure")
				uploadFailure := errors.New("upload refused")
				if failure == "collision" {
					uploadFailure = cloud.ErrAlreadyExists
				}
				var calls, uploads int
				var terminal error
				execute := func(ctx context.Context, cmd host.Command) (host.Result, error) {
					if cmd.Name != "zstd" {
						return metadata.execute(ctx, cmd)
					}
					calls++
					magic := []byte{0x28, 0xb5, 0x2f, 0xfd}
					if calls == target && failure == "magic" {
						magic = []byte("bad!")
					}
					if calls == target && failure == "short" {
						magic = magic[:3]
					}
					if _, err := cmd.Stdout.Write(magic); err != nil {
						return host.Result{}, err
					}
					if calls == target {
						switch failure {
						case "zstd":
							return host.Result{ExitCode: 9, Stderr: []byte("process detail")}, primary
						case "zstd-exit":
							return host.Result{ExitCode: 9, Stderr: []byte("process detail")}, nil
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
							tree := "home/alice"
							if target == 2 {
								tree = "etc/ikigenba"
							}
							if err := os.Rename(filepath.Join(root, tree, "z-later"), filepath.Join(root, "removed-member")); err != nil {
								return host.Result{}, err
							}
						}
					}
					_, err := io.Copy(cmd.Stdout, cmd.Stdin)
					return host.Result{}, err
				}
				client := producerCloud{put: func(_ context.Context, _ string, body io.Reader) error {
					uploads++
					if uploads == target && (failure == "upload" || failure == "collision") {
						return uploadFailure
					}
					_, err := io.Copy(io.Discard, body)
					if uploads == target {
						terminal = err
					}
					if err != nil {
						return errors.New("uploader hides body failure")
					}
					return nil
				}}
				result, err := runProducerBackup(ctx, t, root, execute, client)
				failed := result.Accounts
				if target == 2 {
					failed = result.Host
				}
				if failed.Err == nil || failed.Object != "" || failed.Size != 0 || uploads != target {
					t.Fatalf("backup = %+v, %v; uploads %d", result, err, uploads)
				}
				switch failure {
				case "upload", "collision":
					if !errors.Is(failed.Err, uploadFailure) {
						t.Fatalf("lost upload failure: %v", failed.Err)
					}
				default:
					if terminal == nil || errors.Is(terminal, io.EOF) || strings.Contains(failed.Err.Error(), "uploader hides") {
						t.Fatalf("terminal %v, archive error %v", terminal, failed.Err)
					}
				}
				if failure == "zstd" && !errors.Is(failed.Err, primary) {
					t.Fatalf("lost process failure: %v", failed.Err)
				}
				if failure == "zstd-exit" {
					var commandErr *host.CommandError
					if !errors.As(failed.Err, &commandErr) || commandErr.Result.ExitCode != 9 {
						t.Fatalf("lost exit status: %v", failed.Err)
					}
				}
				if failure == "read" && !strings.Contains(failed.Err.Error(), "/z-later: no such file") {
					t.Fatalf("lost member read failure: %v", failed.Err)
				}
				if failure == "interrupted" && (!errors.Is(err, context.Canceled) || !errors.Is(failed.Err, context.Canceled)) {
					t.Fatalf("lost cancellation: archive %v, operation %v", failed.Err, err)
				}
			})
		}
	}
}
