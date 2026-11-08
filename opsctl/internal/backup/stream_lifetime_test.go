package backup_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestHostBackupJoinsExecuteAfterCollision(t *testing.T) {
	// R-PLR0-LKUQ
	root := t.TempDir()
	store := configuredHostStore(t, root)
	emptyBackupAccounts(t, root)
	failedWrite, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	client := &lifetimeCloud{Client: newFileCloud(), putError: cloud.ErrAlreadyExists}
	env := fileHostEnv(root, func(_ context.Context, command host.Command) (host.Result, error) {
		defer close(completed)
		if command.Name != "zstd" || command.Stdout == nil {
			return host.Result{}, errors.New("expected streaming compressor")
		}
		_, err := command.Stdout.Write([]byte{0x28, 0xb5, 0x2f, 0xfd, 0})
		if err == nil {
			_, err = io.Copy(command.Stdout, command.Stdin)
		}
		close(failedWrite)
		<-release
		return host.Result{}, err
	})
	outcome := make(chan lifetimeOutcome, 1)
	go func() {
		result, err := backup.HostBackup(context.Background(), env, cloud.Env{Open: client.open}, store)
		outcome <- lifetimeOutcome{joined: channelClosed(completed), err: errors.Join(err, result.Accounts.Err)}
	}()
	awaitLifetime(t, failedWrite)
	select {
	case got := <-outcome:
		close(release)
		t.Fatalf("HostBackup returned before Execute: %+v", got)
	default:
	}
	close(release)
	got := receiveLifetime(t, outcome)
	if !got.joined || !errors.Is(got.err, cloud.ErrAlreadyExists) {
		t.Fatalf("HostBackup completed = %+v", got)
	}
}

func TestHostRestoreJoinsExecuteAfterMidstreamFailure(t *testing.T) {
	// R-PLR0-LKUQ
	root := t.TempDir()
	store := configuredHostStore(t, root)
	fixture := newRestoreAccountExecutor(t, root)
	client := &lifetimeCloud{Client: restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, accounts.Manifest{})))}
	failedWrite, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var passes atomic.Int32
	env := fixture.env()
	execute := env.Execute
	env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
		if command.Name != "zstd" || passes.Add(1) != 3 {
			return execute(ctx, command)
		}
		defer close(completed)
		if _, err := io.Copy(io.Discard, command.Stdin); err != nil {
			return host.Result{}, err
		}
		// Invalid first tar header makes the consumer fail while this write is active.
		_, err := command.Stdout.Write(bytes.Repeat([]byte{'!'}, 2*1024*1024))
		close(failedWrite)
		<-release
		return host.Result{}, err
	}
	outcome := make(chan lifetimeOutcome, 1)
	go func() {
		result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
		outcome <- lifetimeOutcome{joined: channelClosed(completed), err: err, step: result.FailedStep}
	}()
	awaitLifetime(t, failedWrite)
	select {
	case got := <-outcome:
		close(release)
		t.Fatalf("HostRestore returned before Execute: %+v", got)
	default:
	}
	close(release)
	got := receiveLifetime(t, outcome)
	if !got.joined || got.err == nil || got.step != "groups" {
		t.Fatalf("HostRestore completed = %+v", got)
	}
	client.assertClosed(t, 3)
}

func TestHostRestoreClosesEveryObjectReader(t *testing.T) {
	// R-PLR0-LKUQ
	for _, test := range []struct {
		name              string
		failGet, failRead int
	}{
		{name: "success"},
		{name: "source get", failGet: 1}, {name: "accounts get", failGet: 2},
		{name: "groups get", failGet: 3}, {name: "users get", failGet: 4},
		{name: "source read", failRead: 1}, {name: "accounts read", failRead: 2},
		{name: "groups read", failRead: 3}, {name: "users read", failRead: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store := configuredHostStore(t, root)
			fixture := newRestoreAccountExecutor(t, root)
			client := &lifetimeCloud{Client: restoreAccountsCloud(t, hostRestoreArchive(t, restoreManifestMember(t, accounts.Manifest{}))), failGet: test.failGet, failRead: test.failRead}
			var active atomic.Int32
			env := fixture.env()
			execute := env.Execute
			env.Execute = func(ctx context.Context, command host.Command) (host.Result, error) {
				active.Add(1)
				defer active.Add(-1)
				return execute(ctx, command)
			}
			result, err := backup.HostRestore(context.Background(), env, cloud.Env{Open: client.open}, store)
			want := max(test.failGet, test.failRead)
			if want == 0 {
				want = 4
				if err != nil {
					t.Fatalf("HostRestore = %+v, %v", result, err)
				}
			} else if err == nil {
				t.Fatalf("HostRestore accepted failure: %+v", result)
			}
			if active.Load() != 0 {
				t.Fatalf("HostRestore returned with %d active Execute calls", active.Load())
			}
			client.assertClosed(t, want)
		})
	}
}

type lifetimeOutcome struct {
	joined bool
	err    error
	step   string
}

func channelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
func awaitLifetime(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("stream handshake did not complete")
	}
}
func receiveLifetime(t *testing.T, ch <-chan lifetimeOutcome) lifetimeOutcome {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("host operation did not finish")
		return lifetimeOutcome{}
	}
}

type lifetimeCloud struct {
	cloud.Client
	putError          error
	readers           []*lifetimeReader
	failGet, failRead int
}

func (client *lifetimeCloud) open(context.Context, string) (cloud.Client, error) { return client, nil }
func (client *lifetimeCloud) PutObject(ctx context.Context, uri string, body io.Reader) error {
	if client.putError != nil {
		return client.putError
	}
	return client.Client.PutObject(ctx, uri, body)
}
func (client *lifetimeCloud) GetObject(ctx context.Context, uri string) (io.ReadCloser, error) {
	reader, err := client.Client.GetObject(ctx, uri)
	if reader == nil {
		return nil, err
	}
	tracked := &lifetimeReader{ReadCloser: reader}
	client.readers = append(client.readers, tracked)
	n := len(client.readers)
	tracked.readFailure = n == client.failRead
	if n == client.failGet {
		err = errors.New("download failed with reader")
	}
	return tracked, err
}
func (client *lifetimeCloud) assertClosed(t *testing.T, want int) {
	t.Helper()
	if len(client.readers) != want {
		t.Fatalf("GetObject readers = %d, want %d", len(client.readers), want)
	}
	for i, reader := range client.readers {
		if reader.closed.Load() < 1 {
			t.Errorf("reader %d closed %d times", i+1, reader.closed.Load())
		}
	}
}

type lifetimeReader struct {
	io.ReadCloser
	closed      atomic.Int32
	readFailure bool
}

func (reader *lifetimeReader) Read(p []byte) (int, error) {
	if reader.readFailure {
		return 0, errors.New("object read failed")
	}
	return reader.ReadCloser.Read(p)
}
func (reader *lifetimeReader) Close() error {
	reader.closed.Add(1)
	err := reader.ReadCloser.Close()
	return err
}
