package sim

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// MaxSocketPath leaves room for the terminating null in a Unix socket address.
const MaxSocketPath = 107

// The listing exposes socket paths and the outcome of each listen attempt.
const (
	SocketList    = "sockets.txt"
	StatusServed  = "served"
	StatusTooLong = "too-long"
	StatusFailed  = "failed"
)

// This reserved name identifies a listing abandoned before its rename.
const pendingSocketList = "." + SocketList + ".tmp"

type boxUser struct {
	name   string
	host   string
	uid    int64
	path   string
	status string
	box    *hostListener
}

func updateBoxSockets(dir string, running map[string]string, loops map[string]loop) {
	var users []boxUser
	for id, host := range running {
		l, ok := loops[id]
		if !ok || l.box == nil {
			continue
		}
		select {
		case <-l.box.closed:
			continue
		default:
		}
		l.machine.RangeUnlockedUsers(func(name string, uid int64) {
			users = append(users, boxUser{
				name: name, host: host, uid: uid,
				path: filepath.Join(dir, name), box: l.box,
			})
		})
	}
	slices.SortFunc(users, func(a, b boxUser) int {
		return cmp.Or(cmp.Compare(a.name, b.name), cmp.Compare(a.host, b.host))
	})

	want := make(map[string]*boxUser, len(users))
	available := boxDirectory(dir, len(users) > 0)
	if available {
		for i := range users {
			u := &users[i]
			switch {
			case len(u.path) > MaxSocketPath:
				u.status = StatusTooLong
			case (i > 0 && users[i-1].name == u.name) || (i+1 < len(users) && users[i+1].name == u.name):
				u.status = StatusFailed
			default:
				want[u.path] = u
			}
		}
	}
	for _, l := range loops {
		if l.box != nil {
			l.box.prune(want)
		}
	}
	if !available {
		return
	}
	removeStaleSockets(dir, want)
	if len(users) == 0 {
		removeBoxListing(dir)
		return
	}
	for path, u := range want {
		u.status = StatusFailed
		if u.box.listen(path, u.uid) {
			u.status = StatusServed
		}
	}
	writeBoxListing(dir, users)
}

func boxDirectory(dir string, create bool) bool {
	info, err := os.Lstat(dir)
	if err == nil {
		return info.IsDir()
	}
	return create && os.IsNotExist(err) && os.Mkdir(dir, 0o700) == nil
}

func removeStaleSockets(dir string, want map[string]*boxUser) {
	f, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	for {
		entries, err := f.ReadDir(1)
		if err != nil {
			return
		}
		path := filepath.Join(dir, entries[0].Name())
		u := want[path]
		if u != nil && u.box.listening(path) {
			continue
		}
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(path)
		}
	}
}

func writeBoxListing(dir string, users []boxUser) {
	f, err := openBoxListing(dir)
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(f.Name()) }()
	w := bufio.NewWriter(f)
	for _, u := range users {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", u.name, u.host, u.uid, u.status, u.path); err != nil {
			_ = f.Close()
			return
		}
	}
	writeErr := w.Flush()
	closeErr := f.Close()
	if writeErr == nil && closeErr == nil {
		_ = os.Rename(f.Name(), filepath.Join(dir, SocketList))
	}
}

func openBoxListing(dir string) (*os.File, error) {
	path := filepath.Join(dir, pendingSocketList)
	removeListingFile(path)
	return os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

func removeBoxListing(dir string) {
	if dir != "" && boxDirectory(dir, false) {
		removeListingFile(filepath.Join(dir, SocketList))
		removeListingFile(filepath.Join(dir, pendingSocketList))
		_ = os.Remove(dir)
	}
}

func removeListingFile(path string) {
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
		_ = os.Remove(path)
	}
}
