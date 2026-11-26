package sim

import (
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestSeedListing(t *testing.T) {
	// R-JD9R-GCEW
	path := filepath.Join(t.TempDir(), "listing")
	b := newSandbox(t, "", path, 2, nil)
	first := b.hosts[0]
	// Reverse instance order by tag and make the first machine's desired hostname distinct from its tag.
	must(t, b.cloud.TagResource(t.Context(), first.ID, cloud.Tags{cloud.TagHost: "host03"}))
	applySeedState(b, 1, "yard", "barn")
	b.look()
	previous := seedListing(t, "host03", "host01", "barn", "yard")
	assertSeedListing(t, path, previous)

	reader, err := os.Open(filepath.Clean(path))
	must(t, err)
	defer func() { must(t, reader.Close()) }()
	before, err := reader.Stat()
	must(t, err)
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	must(t, err)
	t.Cleanup(func() { must(t, syscall.Close(watch)) })
	_, err = syscall.InotifyAddWatch(watch, filepath.Dir(path), syscall.IN_CREATE|syscall.IN_DELETE|syscall.IN_MODIFY|syscall.IN_MOVED_FROM|syscall.IN_MOVED_TO)
	must(t, err)
	applySeedState(b, 2, "acme")
	b.look()
	assertSeedRename(t, watch, filepath.Base(path))
	assertSeedListing(t, path, seedListing(t, "host02", "host02", "acme")+previous)
	after, err := os.Lstat(path)
	must(t, err)
	if os.SameFile(before, after) {
		t.Fatal("a look rewrote the listing in place")
	}
	body, err := io.ReadAll(reader)
	must(t, err)
	if string(body) != previous {
		t.Fatalf("reader of the replaced file got %q, want %q", body, previous)
	}

	must(t, b.cloud.StopInstance(t.Context(), first.ID))
	b.clock.advance(memory.StopDelay)
	b.look()
	assertSeedListing(t, path, seedListing(t, "host02", "host02", "acme"))
}

func assertSeedRename(t *testing.T, watch int, destination string) {
	t.Helper()
	var buf [4096]byte
	temps := make(map[string]bool)
	moves := make(map[uint32]bool)
	var published bool
	for {
		n, err := syscall.Read(watch, buf[:])
		if errors.Is(err, syscall.EAGAIN) {
			break
		}
		must(t, err)
		for pending := buf[:n]; len(pending) != 0; {
			var event syscall.InotifyEvent
			size, err := binary.Decode(pending, binary.NativeEndian, &event)
			if err != nil || int(event.Len) > len(pending)-size || event.Mask&syscall.IN_Q_OVERFLOW != 0 {
				t.Fatalf("invalid inotify event: %+v, %v", event, err)
			}
			end := size + int(event.Len)
			name := strings.TrimRight(string(pending[size:end]), "\x00")
			pending = pending[end:]
			if name == destination {
				if published || event.Mask != syscall.IN_MOVED_TO || !moves[event.Cookie] {
					t.Fatalf("unexpected seed listing event %+v for %q", event, destination)
				}
				published = true
				continue
			}
			switch event.Mask {
			case syscall.IN_CREATE:
				temps[name] = false
			case syscall.IN_MODIFY:
				if _, found := temps[name]; found {
					temps[name] = true
				}
			case syscall.IN_DELETE:
				delete(temps, name)
			case syscall.IN_MOVED_FROM:
				if temps[name] && event.Cookie != 0 {
					moves[event.Cookie] = true
				}
				delete(temps, name)
			}
		}
	}
	if !published {
		t.Fatalf("seed listing %q was not published by renaming a populated temporary file", destination)
	}
}

func TestSeedListingStreamsOneHostAndLineAtATime(t *testing.T) {
	// R-JD9R-GCEW
	type hostSeed struct {
		id, tag string
		seed    machine.Seed
		lines   []string
	}
	hosts := []hostSeed{
		{
			id: "second-instance", tag: "host01",
			seed:  machine.Seed{Account: "acme", Files: []string{"a", "b"}, Author: "acme", Email: "acme@example.test", Message: machine.SeedMessage},
			lines: []string{"host01 acme file a\n", "host01 acme file b\n", "host01 acme commit acme acme@example.test " + machine.SeedMessage + "\n"},
		},
		{
			id: "first-instance", tag: "host02",
			seed:  machine.Seed{Account: "barn", Files: []string{"c"}, Author: "barn", Email: "barn@example.test", Message: machine.SeedMessage},
			lines: []string{"host02 barn file c\n", "host02 barn commit barn barn@example.test " + machine.SeedMessage + "\n"},
		},
	}
	running := make(map[string]string, len(hosts))
	for _, host := range hosts {
		running[host.id] = host.tag
	}
	nextHost := 0
	var pending []string
	snapshot := func(id string) machine.Snapshot {
		if len(pending) != 0 {
			t.Fatalf("requested snapshot for %s before writing lines for %s: %q", id, hosts[nextHost-1].tag, pending)
		}
		if nextHost == len(hosts) || hosts[nextHost].id != id {
			t.Fatalf("unexpected snapshot request for %s at position %d", id, nextHost)
		}
		host := hosts[nextHost]
		nextHost++
		pending = host.lines
		return machine.Snapshot{Seeds: []machine.Seed{host.seed}}
	}
	write := seedWriterFunc(func(line []byte) (int, error) {
		if len(pending) == 0 || string(line) != pending[0] {
			t.Fatalf("write = %q, remaining expected lines = %q", line, pending)
		}
		pending = pending[1:]
		return len(line), nil
	})
	must(t, streamSeedListing(write, running, snapshot))
	if nextHost != len(hosts) || len(pending) != 0 {
		t.Fatalf("stream requested %d of %d snapshots with %d lines still pending", nextHost, len(hosts), len(pending))
	}
}

func TestSeedListingDisabled(t *testing.T) {
	// R-R4Q6-2ILZ
	b := newSandbox(t, "", "", 1, nil)
	applySeedState(b, 1, "acme")
	b.look()
	assertSeedDirectory(t, filepath.Dir(b.banFile))
}

func TestSeedListingAbsentWithoutSeeds(t *testing.T) {
	// R-R4Q6-2ILZ
	dir := t.TempDir()
	path := filepath.Join(dir, "listing")
	must(t, os.WriteFile(path, []byte("previous listing\n"), 0o600))
	b := newSandbox(t, "", path, 1, nil)
	expectAbsent(t, path)
	assertSeedDirectory(t, dir)

	applySeedState(b, 1, "acme")
	b.look()
	assertSeedListing(t, path, seedListing(t, "host01", "host01", "acme"))
	must(t, b.cloud.StopInstance(t.Context(), b.hosts[0].ID))
	b.clock.advance(memory.StopDelay)
	b.look()
	expectAbsent(t, path)
	assertSeedDirectory(t, dir)
}

func TestSeedListingLeavesNonregularPath(t *testing.T) {
	// R-R4Q6-2ILZ
	dir := t.TempDir()
	path := filepath.Join(dir, "listing")
	target := filepath.Join(dir, "target")
	const original = "keep"
	must(t, os.WriteFile(target, []byte(original), 0o600))
	must(t, os.Symlink(target, path))
	b := newSandbox(t, "", path, 1, nil)
	assertLink := func() {
		t.Helper()
		got, err := os.Readlink(path)
		must(t, err)
		if got != target {
			t.Fatalf("listing symlink points to %q, want %q", got, target)
		}
		body, err := os.ReadFile(filepath.Clean(target))
		must(t, err)
		if string(body) != original {
			t.Fatalf("listing symlink target now holds %q", body)
		}
		assertSeedDirectory(t, dir, "listing", "target")
	}
	assertLink()
	applySeedState(b, 1, "acme")
	b.look()
	assertLink()
}

const seedDomain = "example.test"

func applySeedState(b *sandboxBox, host int, slugs ...string) {
	b.t.Helper()
	state := protocol.DesiredState{
		Protocol: protocol.Version, Host: b.hosts[host-1].Tags[cloud.TagHost], Domain: seedDomain,
		Version: 1, Daemon: "same", DataVolume: "vol-data", DataGB: 100, RootGB: 20,
	}
	for i, slug := range slugs {
		state.Accounts = append(state.Accounts, protocol.Account{
			Slug: slug, GID: int64(4040 + i), State: protocol.AccountActive,
		})
	}
	b.applyState(host, state)
}

func seedListing(t *testing.T, tag, host string, slugs ...string) string {
	t.Helper()
	var files []string
	must(t, fs.WalkDir(accounts.Seed(), ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			files = append(files, path)
		}
		return err
	}))
	slices.Sort(files)
	var want strings.Builder
	for _, slug := range slugs {
		for _, path := range files {
			want.WriteString(tag + " " + slug + " file " + path + "\n")
		}
		want.WriteString(tag + " " + slug + " commit " + slug + " " + slug + "@" + host + "." + seedDomain + " " + machine.SeedMessage + "\n")
	}
	return want.String()
}

func assertSeedListing(t *testing.T, path, want string) {
	t.Helper()
	info, err := os.Lstat(path)
	must(t, err)
	if !info.Mode().IsRegular() {
		t.Fatalf("seed listing has mode %v, want regular file", info.Mode())
	}
	body, err := os.ReadFile(filepath.Clean(path))
	must(t, err)
	if string(body) != want {
		t.Fatalf("seed listing = %q, want %q", body, want)
	}
	assertSeedDirectory(t, filepath.Dir(path), filepath.Base(path))
}

func assertSeedDirectory(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	must(t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, want) {
		t.Fatalf("seed listing directory contains %v, want %v", names, want)
	}
}
