package backup_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestHostRestoreStreamsSourceIntoPublishedFiles(t *testing.T) {
	// R-9B1R-UEBT R-9C9O-862I R-9DHK-LXT7 R-1521-1TDA
	root := t.TempDir()
	store := configuredHostStore(t, root)
	body := bytes.Repeat([]byte("compressed input"), 400000)
	archive := decodeRawZstandardFrame(t, hostRestoreArchive(t, restoreMember{name: "etc/ikigenba/large", data: bytes.Repeat([]byte{'x'}, 5*1024*1024)}))
	client := &hostStreamCloud{Client: newFileCloud(), body: body}
	var active bool
	client.active = &active
	var stagedInfo os.FileInfo
	var progress int
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		active = true
		defer func() { active = false }()
		if command.Name != "zstd" || !reflect.DeepEqual(command.Args, []string{"--quiet", "--decompress", "--stdout"}) || command.Stdout == nil {
			return host.Result{}, errors.New("wrong decompression command")
		}
		var buffer [4096]byte
		var read int64
		for {
			n, err := command.Stdin.Read(buffer[:])
			if !bytes.Equal(buffer[:n], body[read:read+int64(n)]) {
				return host.Result{}, errors.New("changed input bytes")
			}
			read += int64(n)
			if client.read-read > 1048576 {
				return host.Result{}, errors.New("object read ahead exceeds bound")
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return host.Result{}, err
			}
		}
		for written := 0; written < len(archive); {
			end := min(written+128*1024, len(archive))
			n, err := command.Stdout.Write(archive[written:end])
			written += n
			if err != nil {
				return host.Result{}, err
			}
			stages, err := filepath.Glob(filepath.Join(root, ".opsctl-host-restore-*"))
			if err != nil {
				return host.Result{}, fmt.Errorf("find stage: %w", err)
			}
			if len(stages) != 1 {
				return host.Result{}, fmt.Errorf("stage = %v", stages)
			}
			info, err := os.Stat(stages[0])
			if err != nil {
				return host.Result{}, fmt.Errorf("stat stage: %w", err)
			}
			if info.Mode().Perm() != 0o700 {
				return host.Result{}, fmt.Errorf("stage permissions: %v", info)
			}
			fileInfo, err := os.Stat(filepath.Join(stages[0], "etc/ikigenba/large"))
			if err == nil {
				if int64(written-1048576-512) > fileInfo.Size() {
					return host.Result{}, fmt.Errorf("staged size %d after output %d", fileInfo.Size(), written)
				}
				if fileInfo.Size() > 0 && fileInfo.Size() < 5*1024*1024 {
					stagedInfo = fileInfo
					progress++
				}
			} else if written > 1048576 {
				return host.Result{}, err
			}
		}
		return host.Result{}, nil
	}}
	result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
	if err != nil || !result.SourceReady || !result.FilesRestored || result.Size != int64(len(body)) || client.gets != 1 || client.closed != 1 || client.outside {
		t.Fatalf("restore = %+v, %v; client %+v", result, err, client)
	}
	info, err := os.Stat(filepath.Join(root, "etc/ikigenba/large"))
	if err != nil || progress == 0 || !os.SameFile(stagedInfo, info) || info.Size() != 5*1024*1024 {
		t.Fatalf("published file differs from growing staged file: %v, %v, progress %d", info, err, progress)
	}
	assertHostStageGone(t, root)
}

func TestHostRestoreRejectsHeadersWithoutDrainingOutput(t *testing.T) {
	// R-13U4-O1ML
	for _, test := range []struct {
		name    string
		members []restoreMember
		nonzero bool
		want    string
	}{
		{"absolute", []restoreMember{{name: "/etc/ikigenba/file"}}, false, "unsafe archive entry"},
		{"dotdot", []restoreMember{{name: "etc/ikigenba/../file"}}, false, "unsafe archive entry"},
		{"outside", []restoreMember{{name: "etc/other/file"}}, false, "outside host restore trees"},
		{"duplicate", []restoreMember{{name: "etc/ikigenba/file"}, {name: "etc/ikigenba/file"}}, false, "duplicate archive entry"},
		{"unsupported", []restoreMember{{name: "etc/ikigenba/file", typeflag: tar.TypeLink, linkname: "other"}}, false, "unsupported type"},
		{"through regular", []restoreMember{{name: "etc/ikigenba/parent"}, {name: "etc/ikigenba/parent/file"}}, false, "traverses non-directory"},
		{"through symlink", []restoreMember{{name: "etc/ikigenba/parent", typeflag: tar.TypeSymlink, linkname: "elsewhere"}, {name: "etc/ikigenba/parent/file"}}, false, "traverses symlink"},
		{"ancestor regular", []restoreMember{{name: "etc/ikigenba/parent/file"}, {name: "etc/ikigenba/parent"}}, false, "non-directory ancestor"},
		{"ancestor symlink", []restoreMember{{name: "etc/ikigenba/parent/file"}, {name: "etc/ikigenba/parent", typeflag: tar.TypeSymlink, linkname: "elsewhere"}}, false, "non-directory ancestor"},
		{"extended name", []restoreMember{{name: "etc/other/" + strings.Repeat("long", 100)}}, false, "outside host restore trees"},
		{"directory data", []restoreMember{{name: "etc/ikigenba/directory", typeflag: tar.TypeDir}}, true, "unexpected data"},
		{"symlink data", []restoreMember{{name: "etc/ikigenba/link", typeflag: tar.TypeSymlink, linkname: "target"}}, true, "unexpected data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			writeFile(t, root, "etc/ikigenba/old", "keep", 0o600)
			before := fileTreeSnapshot(t, root)
			archive := decodeRawZstandardFrame(t, hostRestoreArchive(t, test.members...))
			headerEnd := len(archive) - 1024
			if test.nonzero {
				header := archive[headerEnd-512 : headerEnd]
				copy(header[124:136], []byte("00000000001\x00"))
				copy(header[148:156], []byte("        "))
				var sum int
				for _, value := range header {
					sum += int(value)
				}
				copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", sum)))
			}
			output := make([]byte, headerEnd+8*1024*1024)
			copy(output, archive[:headerEnd])
			client := &hostStreamCloud{Client: newFileCloud(), body: []byte("input")}
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				if _, err := io.Copy(io.Discard, command.Stdin); err != nil {
					return host.Result{}, err
				}
				n, err := command.Stdout.Write(output)
				if err == nil || n > headerEnd+1048576 {
					t.Errorf("rejected output accepted %d bytes, error %v", n, err)
				}
				if _, laterErr := command.Stdout.Write([]byte("later")); laterErr == nil {
					t.Error("write after rejection succeeded")
				}
				return host.Result{}, err
			}}
			result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
			var commandErr *host.CommandError
			if err == nil || !strings.Contains(err.Error(), test.want) || errors.As(err, &commandErr) || result.SourceReady || result.FailedStep != "source" {
				t.Fatalf("restore = %+v, %v", result, err)
			}
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatalf("rejection changed host: %v -> %v", before, after)
			}
			assertHostStageGone(t, root)
		})
	}
}

func TestHostRestoreStageWriteFailureIsSourceFailure(t *testing.T) {
	// R-9C9O-862I
	root := t.TempDir()
	store := configuredHostStore(t, root)
	writeFile(t, root, "etc/ikigenba/old", "keep", 0o600)
	before := fileTreeSnapshot(t, root)
	client := &hostStreamCloud{Client: newFileCloud(), body: []byte("input")}
	archive := decodeRawZstandardFrame(t, hostRestoreArchive(t, restoreMember{name: "etc/ikigenba/file", data: []byte("contents")}))
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		stages, err := filepath.Glob(filepath.Join(root, ".opsctl-host-restore-*"))
		if err != nil || len(stages) != 1 {
			return host.Result{}, errors.New("missing stage")
		}
		if err := os.MkdirAll(filepath.Join(stages[0], "etc/ikigenba/file"), 0o700); err != nil {
			return host.Result{}, err
		}
		_, err = command.Stdout.Write(archive)
		return host.Result{}, err
	}}
	result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
	if err == nil || result.SourceReady || result.FailedStep != "source" {
		t.Fatalf("restore = %+v, %v", result, err)
	}
	if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("stage failure changed host: %v -> %v", before, after)
	}
	assertHostStageGone(t, root)
}

func TestHostRestoreStageModeIgnoresRestrictiveUmask(t *testing.T) {
	// R-9C9O-862I
	const childEnv = "OPSCTL_TEST_HOST_STAGE_UMASK"
	if os.Getenv(childEnv) != "1" {
		// The Linux process link executes this test image without accepting an
		// executable path from arguments or the environment.
		command := exec.Command("/proc/self/exe", "-test.run=^TestHostRestoreStageModeIgnoresRestrictiveUmask$", "-test.count=1")
		command.Env = append(os.Environ(), childEnv+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated umask test: %v\n%s", err, output)
		}
		return
	}
	root := t.TempDir()
	store := configuredHostStore(t, root)
	archive := decodeRawZstandardFrame(t, hostRestoreArchive(t, restoreMember{name: "etc/ikigenba/file", data: []byte("contents")}))
	client := &hostStreamCloud{Client: newFileCloud(), body: []byte("input")}
	observed := false
	env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
		stages, err := filepath.Glob(filepath.Join(root, ".opsctl-host-restore-*"))
		if err != nil {
			return host.Result{}, err
		}
		if len(stages) != 1 {
			return host.Result{}, fmt.Errorf("stage count %d", len(stages))
		}
		info, err := os.Stat(stages[0])
		if err != nil {
			return host.Result{}, err
		}
		if info.Mode().Perm() != 0o700 {
			return host.Result{}, fmt.Errorf("stage mode %o, want 0700", info.Mode().Perm())
		}
		observed = true
		if _, err := io.Copy(io.Discard, command.Stdin); err != nil {
			return host.Result{}, err
		}
		_, err = command.Stdout.Write(archive)
		return host.Result{}, err
	}}
	previous := syscall.Umask(0o777)
	defer syscall.Umask(previous)
	result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
	if err != nil || !observed || !result.FilesRestored {
		t.Fatalf("restore under restrictive umask = %+v, %v; observed %v", result, err, observed)
	}
	assertHostStageGone(t, root)
}

func TestHostRestoreSourceCommandFailureNeverBecomesReady(t *testing.T) {
	// R-9B1R-UEBT R-UL3Q-XT1B
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprint(started), func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			before := fileTreeSnapshot(t, root)
			client := &hostStreamCloud{Client: newFileCloud(), body: []byte("input")}
			archive := decodeRawZstandardFrame(t, hostRestoreArchive(t, restoreMember{name: "etc/ikigenba/file", data: []byte("contents")}))
			captured := host.Result{ExitCode: 7, Stderr: []byte("decoder detail")}
			env := host.Env{Root: root, Execute: func(_ context.Context, command host.Command) (host.Result, error) {
				if !started {
					return captured, errors.New("cannot start")
				}
				if _, err := command.Stdout.Write(archive); err != nil {
					return captured, err
				}
				return captured, nil
			}}
			result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
			var commandErr *host.CommandError
			if !errors.As(err, &commandErr) || !reflect.DeepEqual(commandErr.Result, captured) || result.SourceReady || result.FailedStep != "source" {
				t.Fatalf("restore = %+v, %v", result, err)
			}
			if after := fileTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatalf("command failure changed host: %v -> %v", before, after)
			}
			if client.closed != 1 {
				t.Fatalf("closed reader %d times", client.closed)
			}
			assertHostStageGone(t, root)
		})
	}
}

func assertHostStageGone(t *testing.T, root string) {
	t.Helper()
	stages, err := filepath.Glob(filepath.Join(root, ".opsctl-host-restore-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("stages after restore: %v, %v", stages, err)
	}
}

type hostStreamCloud struct {
	cloud.Client
	body         []byte
	active       *bool
	outside      bool
	gets, closed int
	read         int64
}

func (client *hostStreamCloud) open(context.Context, string) (cloud.Client, error) {
	return client, nil
}
func (*hostStreamCloud) ListObjects(context.Context, string) ([]cloud.Object, error) {
	return []cloud.Object{{URI: restoreConfigurationURI}}, nil
}
func (client *hostStreamCloud) GetObject(context.Context, string) (io.ReadCloser, error) {
	client.gets++
	return &hostStreamReader{Reader: bytes.NewReader(client.body), client: client}, nil
}

type hostStreamReader struct {
	*bytes.Reader
	client *hostStreamCloud
}

func (reader *hostStreamReader) Read(p []byte) (int, error) {
	if reader.client.active != nil && !*reader.client.active {
		reader.client.outside = true
	}
	n, err := reader.Reader.Read(p)
	reader.client.read += int64(n)
	return n, err
}
func (reader *hostStreamReader) Close() error { reader.client.closed++; return nil }
