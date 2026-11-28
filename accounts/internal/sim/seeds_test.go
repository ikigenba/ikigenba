package sim

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func TestSeedListing(t *testing.T) {
	// R-GRHH-2631
	path := filepath.Join(t.TempDir(), "listing")
	b := newSandbox(t, "", path, 2, nil)
	first := b.hosts[0]
	// Reverse instance order by tag and make the first machine's desired hostname distinct from its tag.
	must(t, b.cloud.TagResource(t.Context(), first.ID, cloud.Tags{cloud.TagHost: "host03"}))
	state := seedState(b, 1, "yard", "barn")
	state.Users = []protocol.User{
		{Name: "bob", Account: "barn", UID: 4242, State: protocol.UserActive},
		{Name: "alice", Account: "yard", UID: 4343, State: protocol.UserActive},
	}
	b.applyState(1, state)
	b.look()
	previous := seedListing("host03", "host01", "barn", "yard") +
		homeListing("host03", "yard", "alice") + homeListing("host03", "barn", "bob")
	assertSeedListing(t, path, previous)

	reader, err := os.Open(filepath.Clean(path))
	must(t, err)
	defer func() { must(t, reader.Close()) }()
	before, err := reader.Stat()
	must(t, err)
	watch := watchDirectory(t, filepath.Dir(path))
	applySeedState(b, 2, "acme")
	b.look()
	assertListingRename(t, watch, filepath.Base(path), false)
	assertSeedListing(t, path, seedListing("host02", "host02", "acme")+previous)
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
	assertSeedListing(t, path, seedListing("host02", "host02", "acme"))
}

func watchDirectory(t *testing.T, path string) int {
	t.Helper()
	watch, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	must(t, err)
	t.Cleanup(func() { must(t, syscall.Close(watch)) })
	_, err = syscall.InotifyAddWatch(watch, path, syscall.IN_CREATE|syscall.IN_DELETE|syscall.IN_MODIFY|syscall.IN_MOVED_FROM|syscall.IN_MOVED_TO)
	must(t, err)
	return watch
}

func readDirectoryEvents(t *testing.T, watch int, visit func(event syscall.InotifyEvent, name string)) {
	t.Helper()
	var buf [4096]byte
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
			visit(event, name)
		}
	}
}

func assertListingRename(t *testing.T, watch int, destination string, dotFile bool) {
	t.Helper()
	temps := make(map[string]bool)
	moves := make(map[uint32]bool)
	var published bool
	readDirectoryEvents(t, watch, func(event syscall.InotifyEvent, name string) {
		if name == destination {
			if published || event.Mask != syscall.IN_MOVED_TO || !moves[event.Cookie] {
				t.Fatalf("unexpected listing event %+v for %q", event, destination)
			}
			published = true
			return
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
			if temps[name] && (!dotFile || strings.HasPrefix(name, ".")) && event.Cookie != 0 {
				moves[event.Cookie] = true
			}
			delete(temps, name)
		}
	})
	if !published {
		t.Fatalf("listing %q was not published by renaming a populated temporary file", destination)
	}
}

func TestSeedListingStreamsOneHostAndLineAtATime(t *testing.T) {
	// R-GRHH-2631
	type hostListing struct {
		id, tag  string
		snapshot machine.Snapshot
		lines    []string
	}
	hosts := []hostListing{
		{
			id: "second-instance", tag: "host01",
			snapshot: machine.Snapshot{
				Seeds: []machine.Seed{{Account: "acme", Files: []string{"a", "b"}, Author: "acme", Email: "acme@example.test", Message: machine.SeedMessage}},
				Homes: []machine.Home{
					{Account: "acme", User: "alice", Files: []string{"notes", "notes/config"}},
					{Account: "acme", User: "bob", Files: []string{"notes"}},
				},
			},
			lines: []string{
				"host01 acme file a\n", "host01 acme file b\n", "host01 acme commit acme acme@example.test " + machine.SeedMessage + "\n",
				"host01 acme home alice notes\n", "host01 acme home alice notes/config\n", "host01 acme home bob notes\n",
			},
		},
		{
			id: "first-instance", tag: "host02",
			snapshot: machine.Snapshot{
				Homes: []machine.Home{{Account: "barn", User: "carol", Files: []string{"config"}}},
			},
			lines: []string{"host02 barn home carol config\n"},
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
		return host.snapshot
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
	// R-GSPD-FXTQ
	b := newSandbox(t, "", "", 1, nil)
	applySeedStateWithHome(b)
	b.look()
	assertSeedDirectory(t, filepath.Dir(b.banFile))
}

func TestSeedListingAbsentWithoutSeedsOrHomes(t *testing.T) {
	// R-GRHH-2631 R-GSPD-FXTQ
	dir := t.TempDir()
	path := filepath.Join(dir, "listing")
	must(t, os.WriteFile(path, []byte("previous listing\n"), 0o600))
	b := newSandbox(t, "", path, 1, nil)
	expectAbsent(t, path)
	assertSeedDirectory(t, dir)

	state := seedState(b, 1)
	state.Users = []protocol.User{{Name: "alice", Account: "acme", UID: 4242, State: protocol.UserActive}}
	b.applyState(1, state)
	b.look()
	assertSeedListing(t, path, homeListing("host01", "acme", "alice"))
	must(t, b.cloud.StopInstance(t.Context(), b.hosts[0].ID))
	b.clock.advance(memory.StopDelay)
	b.look()
	expectAbsent(t, path)
	assertSeedDirectory(t, dir)
}

func TestSeedListingLeavesNonregularPath(t *testing.T) {
	// R-GSPD-FXTQ
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
	applySeedStateWithHome(b)
	b.look()
	assertLink()
}

const seedDomain = "example.test"

func TestLoopsShareDisks(t *testing.T) {
	// R-D60E-MFUA
	path := filepath.Join(t.TempDir(), "listing")
	b := newSandbox(t, "", path, 2, nil)
	applySeedState(b, 1, "acme")
	b.look()
	assertSeedListing(t, path, seedListing("host01", "host01", "acme"))
	must(t, b.cloud.StopInstance(t.Context(), b.hosts[0].ID))
	b.look()
	expectAbsent(t, path)
	// The second loop applies a state with the original disk and its account.
	// A fresh disk would seed it with the second host's commit email instead.
	b.applyState(2, protocol.DesiredState{
		Protocol: protocol.Version, Host: "host02", Domain: seedDomain, Version: 1, Daemon: "same",
		DataVolume: "vol-" + b.hosts[0].ID, DataGB: 100, RootGB: 20,
		Accounts: []protocol.Account{{Slug: "acme", GID: 4040, State: protocol.AccountActive}},
	})
	b.look()
	assertSeedListing(t, path, seedListing("host02", "host01", "acme"))
}

func applySeedState(b *sandboxBox, host int, slugs ...string) {
	b.t.Helper()
	b.applyState(host, seedState(b, host, slugs...))
}

func applySeedStateWithHome(b *sandboxBox) {
	b.t.Helper()
	state := seedState(b, 1, "acme")
	state.Users = []protocol.User{{Name: "alice", Account: "acme", UID: 4242, State: protocol.UserActive}}
	b.applyState(1, state)
}

func seedState(b *sandboxBox, host int, slugs ...string) protocol.DesiredState {
	state := protocol.DesiredState{
		Protocol: protocol.Version, Host: b.hosts[host-1].Tags[cloud.TagHost], Domain: seedDomain,
		Version: 1, Daemon: "same", DataVolume: "vol-" + b.hosts[host-1].ID, DataGB: 100, RootGB: 20,
	}
	for i, slug := range slugs {
		state.Accounts = append(state.Accounts, protocol.Account{
			Slug: slug, GID: int64(4040 + i), State: protocol.AccountActive,
		})
	}
	return state
}

func seedListing(tag, host string, slugs ...string) string {
	files := machine.MemorySeedFiles()
	var want strings.Builder
	for _, slug := range slugs {
		for _, path := range files {
			want.WriteString(tag + " " + slug + " file " + path + "\n")
		}
		want.WriteString(tag + " " + slug + " commit " + slug + " " + slug + "@" + host + "." + seedDomain + " " + machine.SeedMessage + "\n")
	}
	return want.String()
}

func homeListing(tag, account, user string) string {
	var want strings.Builder
	for _, path := range machine.MemoryHomeFiles() {
		want.WriteString(tag + " " + account + " home " + user + " " + path + "\n")
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
